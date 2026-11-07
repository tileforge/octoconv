package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/tileforge/octoconv/internal/convert"
)

const (
	envProtocolCoreURL  = "IMAGE_PROTOCOL_CORE_URL"
	envProtocolCert     = "IMAGE_PROTOCOL_CLIENT_CERT"
	envProtocolKey      = "IMAGE_PROTOCOL_CLIENT_KEY"
	envProtocolCA       = "IMAGE_PROTOCOL_CLIENT_CA"
	envProtocolWorkerID = "IMAGE_PROTOCOL_WORKER_ID"
	envProtocolManifest = "IMAGE_PROTOCOL_MANIFEST_PATH"

	maxProtocolFileBytes = 100 << 20
	maxProtocolJSONBytes = 262144
)

var (
	errOriginRejected   = errors.New("origin_rejected")
	errSessionClosed    = errors.New("session_closed")
	errAttemptNotActive = errors.New("attempt_not_active")
)

var imageTerminalSignatures = []string{
	"is not a known file format",
	"premature end of jpeg file",
	"jpeg datastream contains no image",
}

// imageEngineCode maps an image conversion error onto the two engine codes
// handleFail already accepts. A canceled parent is not passed here.
func imageEngineCode(err error) string {
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "no converter for") {
		return "engine_error"
	}
	for _, sig := range imageTerminalSignatures {
		if strings.Contains(msg, sig) {
			return "engine_error"
		}
	}
	return "timeout"
}

// protocolSuiteOK accepts the three TLS 1.3 suites the published protocol allows.
func protocolSuiteOK(state tls.ConnectionState) error {
	if state.Version != tls.VersionTLS13 {
		return errors.New("tls version")
	}
	switch state.CipherSuite {
	case tls.TLS_AES_128_GCM_SHA256, tls.TLS_AES_256_GCM_SHA384, tls.TLS_CHACHA20_POLY1305_SHA256:
		return nil
	default:
		return errors.New("tls suite")
	}
}

type protocolDial struct {
	ready        bool
	core         *url.URL
	certFile     string
	keyFile      string
	caFile       string
	manifestFile string
	workerID     uuid.UUID
	freeSlots    int
	engineLimit  time.Duration
}

func loadProtocolDial() (protocolDial, error) {
	values := []string{
		os.Getenv(envProtocolCoreURL),
		os.Getenv(envProtocolCert),
		os.Getenv(envProtocolKey),
		os.Getenv(envProtocolCA),
		os.Getenv(envProtocolWorkerID),
		os.Getenv(envProtocolManifest),
	}
	present := 0
	for _, value := range values {
		if value != "" {
			present++
		}
	}
	if present == 0 {
		return protocolDial{}, nil
	}
	if present != len(values) {
		return protocolDial{}, errors.New("image protocol dial environment is incomplete")
	}
	core, err := url.Parse(values[0])
	if err != nil || !core.IsAbs() || core.Scheme != "https" || core.Hostname() == "" {
		return protocolDial{}, errors.New("IMAGE_PROTOCOL_CORE_URL must be an absolute https URL")
	}
	workerID, err := uuid.Parse(values[4])
	if err != nil || workerID.String() != strings.ToLower(values[4]) {
		return protocolDial{}, errors.New("IMAGE_PROTOCOL_WORKER_ID must be a UUID")
	}
	names := []string{envProtocolCert, envProtocolKey, envProtocolCA, envProtocolManifest}
	paths := []string{values[1], values[2], values[3], values[5]}
	for i, path := range paths {
		info, statErr := os.Stat(path)
		if statErr != nil || !info.Mode().IsRegular() {
			return protocolDial{}, fmt.Errorf("%s must be a file", names[i])
		}
	}
	slots := envInt("WORKER_CONCURRENCY", 4)
	if slots < 0 {
		slots = 4
	}
	limit := envDuration("ENGINE_TIMEOUT", 120*time.Second)
	if limit <= 0 {
		limit = 120 * time.Second
	}
	return protocolDial{
		ready:        true,
		core:         core,
		certFile:     values[1],
		keyFile:      values[2],
		caFile:       values[3],
		manifestFile: values[5],
		workerID:     workerID,
		freeSlots:    slots,
		engineLimit:  limit,
	}, nil
}

type protocolClient struct {
	core         *url.URL
	coreHTTP     *http.Client
	fileHTTP     *http.Client
	workerID     uuid.UUID
	manifestSHA  string
	freeSlots    int
	engineLimit  time.Duration
	sessionID    string
	sessionToken string
	leaseSeconds int
	pullWait     int
}

func newProtocolClient(dial protocolDial) (*protocolClient, error) {
	cert, err := tls.LoadX509KeyPair(dial.certFile, dial.keyFile)
	if err != nil {
		return nil, errors.New("image protocol client certificate")
	}
	caPEM, err := os.ReadFile(dial.caFile)
	if err != nil {
		return nil, errors.New("image protocol client CA")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("image protocol client CA")
	}
	manifest, err := os.ReadFile(dial.manifestFile)
	if err != nil {
		return nil, errors.New("image protocol manifest")
	}
	sum := sha256.Sum256(manifest)
	coreTLS := protocolTLS(pool, cert, dial.core.Hostname())
	fileTLS := protocolTLS(pool, cert, "")
	return &protocolClient{
		core:        dial.core,
		coreHTTP:    protocolHTTP(coreTLS),
		fileHTTP:    protocolHTTP(fileTLS),
		workerID:    dial.workerID,
		manifestSHA: hex.EncodeToString(sum[:]),
		freeSlots:   dial.freeSlots,
		engineLimit: dial.engineLimit,
	}, nil
}

func protocolTLS(pool *x509.CertPool, cert tls.Certificate, serverName string) *tls.Config {
	cfg := &tls.Config{
		MinVersion:             tls.VersionTLS13,
		MaxVersion:             tls.VersionTLS13,
		RootCAs:                pool,
		Certificates:           []tls.Certificate{cert},
		CurvePreferences:       []tls.CurveID{tls.X25519, tls.CurveP256},
		SessionTicketsDisabled: true,
		Renegotiation:          tls.RenegotiateNever,
		VerifyConnection:       protocolSuiteOK,
	}
	if serverName != "" {
		cfg.ServerName = serverName
	}
	return cfg
}

func protocolHTTP(cfg *tls.Config) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: cfg,
			Proxy:           nil,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errOriginRejected
		},
	}
}

func (c *protocolClient) close() {
	c.coreHTTP.CloseIdleConnections()
	c.fileHTTP.CloseIdleConnections()
}

func runProtocol(ctx context.Context, dial protocolDial) error {
	client, err := newProtocolClient(dial)
	if err != nil {
		return err
	}
	defer client.close()
	err = client.loop(ctx)
	if err == nil || (ctx.Err() != nil && !errors.Is(err, errSessionClosed)) {
		return nil
	}
	return scrubURL(err)
}

func (c *protocolClient) loop(ctx context.Context) error {
	hello, err := c.hello(ctx)
	if err != nil {
		return err
	}
	if hello.Control.Terminate || hello.Control.Drain {
		return nil
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		pull, err := c.pull(ctx)
		if ctx.Err() != nil && !errors.Is(err, errSessionClosed) {
			return nil
		}
		if err != nil {
			return err
		}
		if len(pull.Assignments) == 0 {
			if pull.Control.Terminate || pull.Control.Drain {
				return nil
			}
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(200 * time.Millisecond):
			}
			continue
		}
		for _, asgn := range pull.Assignments {
			if err := c.runAssignment(ctx, asgn, pull.Control); err != nil {
				return err
			}
			if pull.Control.Terminate || pull.Control.Drain {
				return nil
			}
		}
	}
}

type controlJSON struct {
	CancelAttemptIDs []string `json:"cancel_attempt_ids"`
	Drain            bool     `json:"drain"`
	Terminate        bool     `json:"terminate"`
}

type helloJSON struct {
	SessionID       string      `json:"session_id"`
	SessionToken    string      `json:"session_token"`
	LeaseSeconds    int         `json:"lease_seconds"`
	PullWaitSeconds int         `json:"pull_wait_seconds"`
	Control         controlJSON `json:"control"`
}

type helloRequestJSON struct {
	ProtocolVersion int    `json:"protocol_version"`
	WorkerID        string `json:"worker_id"`
	ManifestSHA256  string `json:"manifest_sha256"`
	FreeSlots       int    `json:"free_slots"`
}

func (c *protocolClient) hello(ctx context.Context) (helloJSON, error) {
	body, err := json.Marshal(helloRequestJSON{
		ProtocolVersion: 1,
		WorkerID:        c.workerID.String(),
		ManifestSHA256:  c.manifestSHA,
		FreeSlots:       c.freeSlots,
	})
	if err != nil {
		return helloJSON{}, err
	}
	raw, err := c.postCore(ctx, "/worker/v1/sessions", body, "", "", false)
	if err != nil {
		return helloJSON{}, err
	}
	var hello helloJSON
	if err := json.Unmarshal(raw, &hello); err != nil || hello.SessionID == "" || hello.SessionToken == "" {
		return helloJSON{}, errors.New("hello")
	}
	c.sessionID = hello.SessionID
	c.sessionToken = hello.SessionToken
	c.leaseSeconds = hello.LeaseSeconds
	c.pullWait = hello.PullWaitSeconds
	return hello, nil
}

type pullRequestJSON struct {
	ProtocolVersion int    `json:"protocol_version"`
	PullRequestID   string `json:"pull_request_id"`
	FreeSlots       int    `json:"free_slots"`
	WaitSeconds     int    `json:"wait_seconds"`
}

type fileLinkJSON struct {
	Role      string `json:"role"`
	Ordinal   int    `json:"ordinal"`
	Format    string `json:"format"`
	Href      string `json:"href"`
	ExpiresAt string `json:"expires_at"`
}

type assignmentJSON struct {
	JobID             string         `json:"job_id"`
	StepID            string         `json:"step_id"`
	AttemptID         string         `json:"attempt_id"`
	AttemptToken      string         `json:"attempt_token"`
	CapabilityID      string         `json:"capability_id"`
	CapabilityVersion int            `json:"capability_version"`
	AttemptDeadline   string         `json:"attempt_deadline"`
	LeaseExpiresAt    string         `json:"lease_expires_at"`
	Limits            limitsJSON     `json:"limits"`
	Inputs            []fileLinkJSON `json:"inputs"`
	Outputs           []fileLinkJSON `json:"outputs"`
}

type limitsJSON struct {
	MaxOutputBytes int64 `json:"max_output_bytes"`
}

type pullResponseJSON struct {
	Assignments []assignmentJSON `json:"assignments"`
	Control     controlJSON      `json:"control"`
}

func (c *protocolClient) pull(ctx context.Context) (pullResponseJSON, error) {
	wait := c.pullWait
	if wait < 0 {
		wait = 0
	}
	if wait > 20 {
		wait = 20
	}
	body, err := json.Marshal(pullRequestJSON{
		ProtocolVersion: 1,
		PullRequestID:   uuid.NewString(),
		FreeSlots:       c.freeSlots,
		WaitSeconds:     wait,
	})
	if err != nil {
		return pullResponseJSON{}, err
	}
	raw, err := c.postCore(ctx, "/worker/v1/sessions/"+url.PathEscape(c.sessionID)+"/pull", body, c.sessionToken, "", true)
	if err != nil {
		var status *statusError
		if errors.As(err, &status) && status.code == "rate_limited" {
			select {
			case <-ctx.Done():
				return pullResponseJSON{}, ctx.Err()
			case <-time.After(time.Second):
			}
			return c.pull(ctx)
		}
		return pullResponseJSON{}, err
	}
	var pull pullResponseJSON
	if err := json.Unmarshal(raw, &pull); err != nil {
		return pullResponseJSON{}, errors.New("pull")
	}
	if pull.Assignments == nil {
		pull.Assignments = []assignmentJSON{}
	}
	return pull, nil
}

func (c *protocolClient) runAssignment(ctx context.Context, asgn assignmentJSON, ctrl controlJSON) error {
	if ctrl.Terminate {
		return c.postStopped(ctx, asgn, "terminate")
	}
	if containsID(ctrl.CancelAttemptIDs, asgn.AttemptID) {
		return c.postStopped(ctx, asgn, "cancel")
	}
	if asgn.AttemptID == "" || asgn.AttemptToken == "" {
		return errors.New("assignment")
	}
	if !oneLink(asgn.Inputs, "source") || !oneLink(asgn.Outputs, "primary") {
		return c.fail(ctx, asgn, "engine_error", "assignment")
	}
	in := asgn.Inputs[0]
	out := asgn.Outputs[0]
	if _, err := requireHTTPS(in.Href); err != nil {
		return err
	}
	if _, err := requireHTTPS(out.Href); err != nil {
		return err
	}
	deadline, err := time.Parse(time.RFC3339Nano, asgn.AttemptDeadline)
	if err != nil {
		return c.fail(ctx, asgn, "engine_error", "assignment")
	}
	leaseUntil, err := time.Parse(time.RFC3339Nano, asgn.LeaseExpiresAt)
	if err != nil {
		return c.fail(ctx, asgn, "engine_error", "assignment")
	}

	attemptCtx, cancelAttempt := context.WithCancelCause(ctx)
	var flag stopFlag
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.renewLoop(ctx, attemptCtx, cancelAttempt, &flag, asgn, leaseUntil)
	}()
	defer func() {
		cancelAttempt(context.Canceled)
		wg.Wait()
	}()

	if done, err := c.alreadyStopped(ctx, &flag); done {
		return err
	}
	src, err := c.getFile(attemptCtx, in.Href)
	if done, stopErr := c.alreadyStopped(ctx, &flag); done {
		return stopErr
	}
	if err != nil {
		if errors.Is(err, errOriginRejected) {
			return errOriginRejected
		}
		return c.fail(ctx, asgn, imageEngineCode(err), safeMessage(err))
	}
	if err := c.postProgress(attemptCtx, asgn, 0); err != nil {
		if done, stopErr := c.alreadyStopped(ctx, &flag); done {
			return stopErr
		}
		if errors.Is(err, errAttemptNotActive) {
			return c.lostLease(ctx, &flag, asgn)
		}
		return err
	}

	produced, err := c.convert(attemptCtx, in.Format, out.Format, src, deadline)
	if done, stopErr := c.alreadyStopped(ctx, &flag); done {
		return stopErr
	}
	if err != nil {
		if errors.Is(err, errOriginRejected) {
			return errOriginRejected
		}
		return c.fail(ctx, asgn, imageEngineCode(err), safeMessage(err))
	}
	if asgn.Limits.MaxOutputBytes > 0 && int64(len(produced)) > asgn.Limits.MaxOutputBytes {
		return c.fail(ctx, asgn, "engine_error", "output too large")
	}
	if err := c.postProgress(attemptCtx, asgn, 99); err != nil {
		if done, stopErr := c.alreadyStopped(ctx, &flag); done {
			return stopErr
		}
		if errors.Is(err, errAttemptNotActive) {
			return c.lostLease(ctx, &flag, asgn)
		}
		return err
	}
	if err := c.putFile(attemptCtx, out.Href, produced); err != nil {
		if done, stopErr := c.alreadyStopped(ctx, &flag); done {
			return stopErr
		}
		if errors.Is(err, errOriginRejected) {
			return errOriginRejected
		}
		return c.fail(ctx, asgn, imageEngineCode(err), safeMessage(err))
	}
	if !flag.claim("complete", nil) {
		if errors.Is(flag.cause(), errSessionClosed) {
			return errSessionClosed
		}
		return nil
	}
	sum := sha256.Sum256(produced)
	completeBody, err := json.Marshal(completeJSON{
		ProtocolVersion: 1,
		Artifacts: []artifactJSON{{
			Role:        "primary",
			Ordinal:     0,
			Format:      out.Format,
			ContentType: convert.MIMEType(out.Format),
			SHA256:      hex.EncodeToString(sum[:]),
			ByteSize:    int64(len(produced)),
		}},
	})
	if err != nil {
		return err
	}
	_, err = c.postCore(
		ctx,
		"/worker/v1/attempts/"+url.PathEscape(asgn.AttemptID)+"/complete",
		completeBody,
		c.sessionToken,
		asgn.AttemptToken,
		true,
	)
	if errors.Is(err, errAttemptNotActive) {
		return c.lostLease(ctx, &flag, asgn)
	}
	return err
}

func (c *protocolClient) convert(ctx context.Context, from, to string, src []byte, deadline time.Time) ([]byte, error) {
	conv, ok := convert.Default.Lookup(from, to)
	if !ok {
		return nil, fmt.Errorf("no converter for %s to %s", from, to)
	}
	dir, err := os.MkdirTemp("", "octoconv-image-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	inPath := filepath.Join(dir, "in."+from)
	outPath := filepath.Join(dir, "out."+to)
	if err := os.WriteFile(inPath, src, 0o600); err != nil {
		return nil, err
	}
	budget := c.engineLimit
	if until := time.Until(deadline); until < budget {
		budget = until
	}
	convertCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	if err := conv.Convert(convertCtx, inPath, outPath, nil); err != nil {
		return nil, err
	}
	return os.ReadFile(outPath)
}

func (c *protocolClient) renewLoop(parent, attempt context.Context, cancelAttempt context.CancelCauseFunc, flag *stopFlag, asgn assignmentJSON, leaseUntil time.Time) {
	wait := renewEvery(c.leaseSeconds)
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		select {
		case <-parent.Done():
			return
		case <-attempt.Done():
			return
		case <-timer.C:
			if parent.Err() != nil {
				return
			}
			if !leaseUntil.IsZero() && !time.Now().Before(leaseUntil) {
				c.stopAttempt(parent, cancelAttempt, flag, asgn, "lease_lost", errAttemptNotActive)
				return
			}
			next, ctrl, err := c.renew(attempt, asgn)
			if parent.Err() != nil {
				return
			}
			if errors.Is(err, errSessionClosed) {
				flag.claim("session_closed", errSessionClosed)
				cancelAttempt(errSessionClosed)
				return
			}
			if errors.Is(err, errAttemptNotActive) {
				c.stopAttempt(parent, cancelAttempt, flag, asgn, "lease_lost", errAttemptNotActive)
				return
			}
			if err != nil {
				timer.Reset(wait)
				continue
			}
			if ctrl.Terminate {
				c.stopAttempt(parent, cancelAttempt, flag, asgn, "terminate", errors.New("terminate"))
				return
			}
			if containsID(ctrl.CancelAttemptIDs, asgn.AttemptID) {
				c.stopAttempt(parent, cancelAttempt, flag, asgn, "cancel", errors.New("cancel"))
				return
			}
			if !next.IsZero() {
				leaseUntil = next
			}
			timer.Reset(wait)
		}
	}
}

func renewEvery(seconds int) time.Duration {
	if seconds <= 0 {
		return time.Millisecond
	}
	half := time.Duration(seconds) * time.Second / 2
	if half <= 0 {
		return time.Millisecond
	}
	return half
}

func (c *protocolClient) stopAttempt(parent context.Context, cancelAttempt context.CancelCauseFunc, flag *stopFlag, asgn assignmentJSON, reason string, cause error) {
	if parent.Err() != nil {
		return
	}
	if flag.claim(reason, cause) {
		_ = c.postStopped(parent, asgn, reason)
	}
	cancelAttempt(cause)
}

func (c *protocolClient) lostLease(ctx context.Context, flag *stopFlag, asgn assignmentJSON) error {
	if flag.outcome() == "lease_lost" || flag.outcome() == "terminate" || flag.outcome() == "cancel" {
		return nil
	}
	if flag.claim("lease_lost", errAttemptNotActive) {
		return c.postStopped(ctx, asgn, "lease_lost")
	}
	if errors.Is(flag.cause(), errSessionClosed) {
		return errSessionClosed
	}
	return nil
}

func (c *protocolClient) alreadyStopped(ctx context.Context, flag *stopFlag) (bool, error) {
	if errors.Is(flag.cause(), errSessionClosed) {
		return true, errSessionClosed
	}
	if flag.outcome() != "" && flag.outcome() != "complete" {
		return true, nil
	}
	if ctx.Err() != nil {
		return true, nil
	}
	return false, nil
}

type leaseResponseJSON struct {
	LeaseExpiresAt string      `json:"lease_expires_at"`
	Control        controlJSON `json:"control"`
}

func (c *protocolClient) renew(ctx context.Context, asgn assignmentJSON) (time.Time, controlJSON, error) {
	body := []byte(`{"protocol_version":1}`)
	raw, err := c.postCore(
		ctx,
		"/worker/v1/attempts/"+url.PathEscape(asgn.AttemptID)+"/lease",
		body,
		c.sessionToken,
		asgn.AttemptToken,
		false,
	)
	if err != nil {
		return time.Time{}, controlJSON{}, err
	}
	var lease leaseResponseJSON
	if err := json.Unmarshal(raw, &lease); err != nil {
		return time.Time{}, controlJSON{}, errors.New("lease")
	}
	when, err := time.Parse(time.RFC3339Nano, lease.LeaseExpiresAt)
	if err != nil {
		return time.Time{}, lease.Control, nil
	}
	return when, lease.Control, nil
}

type progressJSON struct {
	ProtocolVersion int `json:"protocol_version"`
	Percent         int `json:"percent"`
}

func (c *protocolClient) postProgress(ctx context.Context, asgn assignmentJSON, percent int) error {
	if percent < 0 || percent > 99 {
		return errors.New("progress")
	}
	body, err := json.Marshal(progressJSON{ProtocolVersion: 1, Percent: percent})
	if err != nil {
		return err
	}
	_, err = c.postCore(
		ctx,
		"/worker/v1/attempts/"+url.PathEscape(asgn.AttemptID)+"/progress",
		body,
		c.sessionToken,
		asgn.AttemptToken,
		false,
	)
	return err
}

type failJSON struct {
	ProtocolVersion int    `json:"protocol_version"`
	EngineCode      string `json:"engine_code"`
	Message         string `json:"message"`
}

func (c *protocolClient) fail(ctx context.Context, asgn assignmentJSON, code, message string) error {
	if ctx.Err() != nil {
		return nil
	}
	if code != "timeout" && code != "engine_error" {
		code = "engine_error"
	}
	if message == "" {
		message = "conversion failed"
	}
	body, err := json.Marshal(failJSON{ProtocolVersion: 1, EngineCode: code, Message: message})
	if err != nil {
		return err
	}
	_, err = c.postCore(
		ctx,
		"/worker/v1/attempts/"+url.PathEscape(asgn.AttemptID)+"/fail",
		body,
		c.sessionToken,
		asgn.AttemptToken,
		false,
	)
	if errors.Is(err, errAttemptNotActive) || errors.Is(err, errSessionClosed) {
		return err
	}
	return err
}

type stoppedJSON struct {
	ProtocolVersion int    `json:"protocol_version"`
	Reason          string `json:"reason"`
}

func (c *protocolClient) postStopped(ctx context.Context, asgn assignmentJSON, reason string) error {
	if ctx.Err() != nil {
		return nil
	}
	body, err := json.Marshal(stoppedJSON{ProtocolVersion: 1, Reason: reason})
	if err != nil {
		return err
	}
	_, err = c.postCore(
		ctx,
		"/worker/v1/attempts/"+url.PathEscape(asgn.AttemptID)+"/stopped",
		body,
		c.sessionToken,
		asgn.AttemptToken,
		false,
	)
	if errors.Is(err, errAttemptNotActive) {
		return nil
	}
	return err
}

type completeJSON struct {
	ProtocolVersion int            `json:"protocol_version"`
	Artifacts       []artifactJSON `json:"artifacts"`
}

type artifactJSON struct {
	Role        string `json:"role"`
	Ordinal     int    `json:"ordinal"`
	Format      string `json:"format"`
	ContentType string `json:"content_type"`
	SHA256      string `json:"sha256"`
	ByteSize    int64  `json:"byte_size"`
}

func (c *protocolClient) getFile(ctx context.Context, href string) ([]byte, error) {
	if _, err := requireHTTPS(href); err != nil {
		return nil, err
	}
	return c.do(ctx, c.fileHTTP, http.MethodGet, href, nil, "", "", maxProtocolFileBytes)
}

func (c *protocolClient) putFile(ctx context.Context, href string, body []byte) error {
	if _, err := requireHTTPS(href); err != nil {
		return err
	}
	_, err := c.do(ctx, c.fileHTTP, http.MethodPut, href, body, "", "", maxProtocolJSONBytes)
	return err
}

func (c *protocolClient) postCore(ctx context.Context, path string, body []byte, bearer, attempt string, retry bool) ([]byte, error) {
	target, err := url.JoinPath(c.core.String(), strings.TrimPrefix(path, "/"))
	if err != nil {
		return nil, err
	}
	attempts := 1
	if retry {
		attempts = 2
	}
	var last error
	for try := 0; try < attempts; try++ {
		raw, err := c.do(ctx, c.coreHTTP, http.MethodPost, target, body, bearer, attempt, maxProtocolJSONBytes)
		if err == nil {
			return raw, nil
		}
		last = err
		if ctx.Err() != nil || errors.Is(err, errSessionClosed) || errors.Is(err, errAttemptNotActive) || errors.Is(err, errOriginRejected) {
			return nil, err
		}
		var status *statusError
		if errors.As(err, &status) {
			return nil, err
		}
	}
	return nil, last
}

type statusError struct {
	status int
	code   string
}

func (e *statusError) Error() string {
	if e.code != "" {
		return e.code
	}
	return fmt.Sprintf("status %d", e.status)
}

func (c *protocolClient) do(ctx context.Context, client *http.Client, method, rawURL string, body []byte, bearer, attempt string, limit int64) ([]byte, error) {
	if _, err := requireHTTPS(rawURL); err != nil {
		return nil, err
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
	if err != nil {
		return nil, err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if attempt != "" {
		req.Header.Set("Octoconv-Attempt", attempt)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > limit {
		return nil, errors.New("response too large")
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return payload, nil
	}
	code := wireCode(payload)
	if resp.StatusCode == http.StatusUnauthorized && code == "session_closed" {
		return nil, errSessionClosed
	}
	if resp.StatusCode == http.StatusConflict && code == "attempt_not_active" {
		return nil, errAttemptNotActive
	}
	return nil, &statusError{status: resp.StatusCode, code: code}
}

func requireHTTPS(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, errOriginRejected
	}
	return parsed, nil
}

func wireCode(payload []byte) string {
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return ""
	}
	return body.Code
}

func containsID(ids []string, id string) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}

func oneLink(links []fileLinkJSON, role string) bool {
	return len(links) == 1 && links[0].Role == role && links[0].Ordinal == 0 && links[0].Href != ""
}

func scrubURL(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return scrubURL(urlErr.Err)
	}
	return err
}

func safeMessage(err error) string {
	err = scrubURL(err)
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	if msg == "" {
		return "conversion failed"
	}
	if len(msg) > 400 {
		return msg[:400]
	}
	return msg
}

type stopFlag struct {
	mu     sync.Mutex
	reason string
	err    error
}

func (s *stopFlag) claim(reason string, err error) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reason != "" || s.err != nil {
		return false
	}
	s.reason = reason
	s.err = err
	return true
}

func (s *stopFlag) outcome() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reason
}

func (s *stopFlag) cause() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}
