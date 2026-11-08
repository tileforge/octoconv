package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"math/big"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/tileforge/octoconv/internal/api"
	"github.com/tileforge/octoconv/internal/clients"
	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/db"
	"github.com/tileforge/octoconv/internal/jobs"
	"github.com/tileforge/octoconv/internal/presets"
	"github.com/tileforge/octoconv/internal/queue"
	"github.com/tileforge/octoconv/internal/storage"
	"github.com/tileforge/octoconv/internal/workerproto"
)

func TestExifGPSJobCompletesThroughClientGet(t *testing.T) {
	requireVips(t)
	pool := protocolPool(t)
	resetProtocolTables(t, pool)

	mat := mintMaterial(t)
	manifestPath := testPath("../../docs/worker-protocol/examples/positive/manifest-image.json")
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal("manifest")
	}
	repo := workerproto.NewRepo(pool)
	if err := repo.Approve(context.Background(), mat.workerID, manifest); err != nil {
		fatalDB(t, err)
	}

	store := newObjectBytes()
	fileAddr := serveTLS(t, fileServerConfig(mat.server), store, true)
	store.setBase("https://" + fileAddr)

	tap := &coreTap{next: workerproto.NewHandler(repo, jobs.NewRepo(pool), store, 120*time.Second).Routes()}
	coreCfg, err := workerproto.ServerTLSConfig(repo)
	if err != nil {
		t.Fatal("server tls")
	}
	caPool := x509.NewCertPool()
	caPool.AddCert(mat.ca)
	coreCfg.ClientCAs = caPool
	coreCfg.Certificates = []tls.Certificate{mat.server}
	coreCfg.NextProtos = []string{"h2", "http/1.1"}
	coreAddr := serveTLS(t, coreCfg, tap, true)

	clientID := insertProtocolClient(t, pool)
	stubs := &apiStubs{}
	resolver := fixedResolver{client: &clients.Client{ID: clientID, Name: "protocol-tracer"}}
	srv := api.NewServer(jobs.NewRepo(pool), store, stubs, stubs, stubs, stubs, stubs, resolver, api.HealthDeps{}, api.Config{})
	srv.SetCapabilityCatalog(repo)
	apiSrv := httptestServer(t, srv.Routes())

	jpg := readFixture(t, "../../internal/convert/testdata/sanitize/exif_gps.jpg")
	jobID := postExifJob(t, apiSrv.Addr, jpg)
	if stubs.image.Load() != 0 {
		t.Fatal("image enqueue")
	}

	t.Setenv(envProtocolCoreURL, "https://"+coreAddr)
	t.Setenv(envProtocolCert, mat.certFile)
	t.Setenv(envProtocolKey, mat.keyFile)
	t.Setenv(envProtocolCA, mat.caFile)
	t.Setenv(envProtocolWorkerID, mat.workerID.String())
	t.Setenv(envProtocolManifest, manifestPath)
	t.Setenv("WORKER_CONCURRENCY", "4")
	t.Setenv("ENGINE_TIMEOUT", "120s")
	dial, err := loadProtocolDial()
	if err != nil {
		t.Fatal(err)
	}
	if !dial.ready {
		t.Fatal("dial")
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- runProtocol(ctx, dial)
	}()

	body := pollJob(t, apiSrv.Addr, jobID, errCh)
	if body["status"] != "done" {
		t.Fatal("status")
	}
	pct, ok := body["progress_pct"].(float64)
	if !ok || pct != 100 {
		t.Fatal("progress")
	}
	rawURL, _ := body["download_url"].(string)
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		t.Fatal("download_url")
	}
	png := downloadHTTPS(t, caPool, rawURL)
	if !bytes.HasPrefix(png, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		t.Fatal("png")
	}
	sum := sha256.Sum256(png)
	want := hex.EncodeToString(sum[:])
	tap.mu.Lock()
	completes := append([][]byte(nil), tap.completes...)
	tap.mu.Unlock()
	if len(completes) != 1 || !bytes.Contains(completes[0], []byte(want)) {
		t.Fatal("complete sha")
	}
	var engine, owner string
	if err := pool.QueryRow(context.Background(), `SELECT engine, dispatch_owner FROM jobs WHERE id = $1`, jobID).Scan(&engine, &owner); err != nil {
		fatalDB(t, err)
	}
	if engine != "image" || owner != "protocol" {
		t.Fatal("engine")
	}
	if stubs.image.Load() != 0 {
		t.Fatal("image enqueue")
	}
	if store.fileAuth.Load() || tap.helloAuth.Load() || tap.pullMissingAuth.Load() || tap.pullAttempt.Load() {
		t.Fatal("auth headers")
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(publicText(err.Error()))
		}
	case <-time.After(20 * time.Second):
		t.Fatal("runProtocol did not return")
	}
}

func TestProtocolTLSSuites(t *testing.T) {
	src := readFixture(t, "protocol.go")
	if bytes.Contains(src, []byte("CipherSuites:")) {
		t.Fatal("cipher list")
	}
	ok := []tls.ConnectionState{
		{Version: tls.VersionTLS13, CipherSuite: tls.TLS_AES_128_GCM_SHA256},
		{Version: tls.VersionTLS13, CipherSuite: tls.TLS_AES_256_GCM_SHA384},
		{Version: tls.VersionTLS13, CipherSuite: tls.TLS_CHACHA20_POLY1305_SHA256},
	}
	for _, state := range ok {
		if err := protocolSuiteOK(state); err != nil {
			t.Fatal(err)
		}
	}
	bad := []tls.ConnectionState{
		{Version: tls.VersionTLS12, CipherSuite: tls.TLS_AES_128_GCM_SHA256},
		{Version: tls.VersionTLS13, CipherSuite: tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256},
	}
	for _, state := range bad {
		if err := protocolSuiteOK(state); err == nil {
			t.Fatal("suite accepted")
		}
	}
}

func TestDeadlineMapsToTimeout(t *testing.T) {
	err := fmtError("premature end of jpeg file: %w", context.DeadlineExceeded)
	if imageEngineCode(err) != "timeout" {
		t.Fatal("wrapped deadline")
	}
	if imageEngineCode(context.DeadlineExceeded) != "timeout" {
		t.Fatal("deadline")
	}
}

func TestTerminalVipsMapsToEngineError(t *testing.T) {
	workerSrc := string(readFixture(t, "../../internal/worker/worker.go"))
	protoSrc := string(readFixture(t, "protocol.go"))
	sigs := []string{
		"is not a known file format",
		"premature end of jpeg file",
		"jpeg datastream contains no image",
	}
	for _, sig := range sigs {
		if !strings.Contains(workerSrc, sig) || !strings.Contains(protoSrc, sig) {
			t.Fatalf("missing signature %s", sig)
		}
		if imageEngineCode(errors.New("vips copy: "+sig)) != "engine_error" {
			t.Fatalf("signature %s", sig)
		}
	}
	if imageEngineCode(errors.New("no converter for jpg to pdf")) != "engine_error" {
		t.Fatal("lookup")
	}
	if imageEngineCode(errors.New("exit status 1")) != "timeout" {
		t.Fatal("exit status")
	}
}

func TestRetryableVipsMapsToTimeout(t *testing.T) {
	if imageEngineCode(errors.New("vips copy failed: temporary failure")) != "timeout" {
		t.Fatal("retryable")
	}
}

func TestSymlinkOutputIsEngineError(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "secret")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatal("write")
	}
	link := filepath.Join(dir, "out.png")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal("symlink")
	}
	_, err := convert.ReadJobFile(link)
	if !errors.Is(err, convert.ErrJobFileIrregular) {
		t.Fatal("read")
	}
	if imageEngineCode(err) != "engine_error" {
		t.Fatal("code")
	}
	if imageEngineCode(fmt.Errorf("open output: %w", convert.ErrJobFileIrregular)) != "engine_error" {
		t.Fatal("wrapped")
	}
}

func TestProgressPercentStaysBelow100(t *testing.T) {
	requireVips(t)
	state := &scriptState{helloLease: 30, helloWait: 0, jpg: readFixture(t, "../../internal/convert/testdata/sanitize/exif_gps.jpg")}
	dial, fileURL := bootScript(t, state, true)
	state.setAssignment(scriptAssignment(fileURL), controlJSON{})
	runProtocolUntil(t, dial, 60*time.Second, func() bool {
		state.mu.Lock()
		defer state.mu.Unlock()
		return len(state.progress) >= 2 && len(state.completes) >= 1
	})
	state.mu.Lock()
	progress := append([][]byte(nil), state.progress...)
	fails := len(state.fails)
	state.mu.Unlock()
	if fails != 0 {
		t.Fatal("fail")
	}
	saw0 := false
	saw99 := false
	for _, raw := range progress {
		if bytes.Contains(raw, []byte(".")) || bytes.Contains(raw, []byte("100")) {
			t.Fatal("progress value")
		}
		var body struct {
			Percent int `json:"percent"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal("progress json")
		}
		if body.Percent < 0 || body.Percent > 99 {
			t.Fatal("progress range")
		}
		if body.Percent == 0 {
			saw0 = true
		}
		if body.Percent == 99 {
			if !saw0 {
				t.Fatal("progress order")
			}
			saw99 = true
		}
	}
	if !saw0 || !saw99 {
		t.Fatal("progress sequence")
	}
}

func TestTerminateSendsStopped(t *testing.T) {
	state := &scriptState{helloLease: 30, helloWait: 0}
	dial, fileURL := bootScript(t, state, true)
	asgn := scriptAssignment(fileURL)
	state.setAssignment(asgn, controlJSON{Terminate: true})
	err := runProtocol(context.Background(), dial)
	if err != nil {
		t.Fatal(publicText(err.Error()))
	}
	reasons := state.stoppedReasons()
	state.mu.Lock()
	fails := len(state.fails)
	gets := state.fileGets
	state.mu.Unlock()
	if len(reasons) != 1 || reasons[0] != "terminate" || fails != 0 || gets != 0 {
		t.Fatal("terminate")
	}
}

func TestCancelSendsStopped(t *testing.T) {
	state := &scriptState{helloLease: 30, helloWait: 0}
	dial, fileURL := bootScript(t, state, true)
	asgn := scriptAssignment(fileURL)
	state.setAssignment(asgn, controlJSON{CancelAttemptIDs: []string{asgn.AttemptID}})
	runProtocolUntil(t, dial, 20*time.Second, func() bool {
		return len(state.stoppedReasons()) >= 1
	})
	reasons := state.stoppedReasons()
	state.mu.Lock()
	fails := len(state.fails)
	gets := state.fileGets
	state.mu.Unlock()
	if len(reasons) != 1 || reasons[0] != "cancel" || fails != 0 || gets != 0 {
		t.Fatal("cancel")
	}
}

func TestStalledRenewalStopsAtLeaseExpiry(t *testing.T) {
	state := &scriptState{helloLease: 0, helloWait: 0, stallLease: true, blockGet: true}
	dial, fileURL := bootScript(t, state, true)
	asgn := scriptAssignment(fileURL)
	expires := time.Now().Add(2 * time.Second)
	asgn.LeaseExpiresAt = expires.UTC().Format(time.RFC3339Nano)
	state.setAssignment(asgn, controlJSON{})
	runProtocolUntil(t, dial, 8*time.Second, func() bool {
		return len(state.stoppedReasons()) >= 1
	})
	if time.Since(expires) > 3*time.Second {
		t.Fatal("renewal outlived the lease")
	}
	reasons := state.stoppedReasons()
	state.mu.Lock()
	fails := len(state.fails)
	state.mu.Unlock()
	if len(reasons) != 1 || reasons[0] != "lease_lost" || fails != 0 {
		t.Fatal("lease")
	}
}

func TestStalledStoppedReleasesAttempt(t *testing.T) {
	state := &scriptState{helloLease: 0, helloWait: 0, stallLease: true, stallStopped: true, blockGet: true}
	dial, fileURL := bootScript(t, state, true)
	asgn := scriptAssignment(fileURL)
	expires := time.Now().Add(2 * time.Second)
	asgn.LeaseExpiresAt = expires.UTC().Format(time.RFC3339Nano)
	state.setAssignment(asgn, controlJSON{})
	runProtocolUntil(t, dial, stoppedNotifyTimeout+8*time.Second, func() bool {
		state.mu.Lock()
		defer state.mu.Unlock()
		return state.pulls >= 2 && state.getReleased >= 1 && len(state.stopped) >= 1
	})
	since := time.Since(expires)
	if since < stoppedNotifyTimeout-time.Second || since > stoppedNotifyTimeout+3*time.Second {
		t.Fatal("stopped bound")
	}
	reasons := state.stoppedReasons()
	state.mu.Lock()
	fails := len(state.fails)
	replies := state.stoppedReplies
	released := state.getReleased
	state.mu.Unlock()
	if len(reasons) != 1 || reasons[0] != "lease_lost" || fails != 0 || replies != 0 || released < 1 {
		t.Fatal("stopped")
	}
}

func TestLeaseLossSendsStopped(t *testing.T) {
	state := &scriptState{helloLease: 0, helloWait: 0, blockGet: true, leaseCode: "attempt_not_active"}
	dial, fileURL := bootScript(t, state, true)
	state.setAssignment(scriptAssignment(fileURL), controlJSON{})
	runProtocolUntil(t, dial, 20*time.Second, func() bool {
		return len(state.stoppedReasons()) >= 1
	})
	reasons := state.stoppedReasons()
	state.mu.Lock()
	fails := len(state.fails)
	state.mu.Unlock()
	if len(reasons) != 1 || reasons[0] != "lease_lost" || fails != 0 {
		t.Fatal("lease")
	}
}

func TestCompleteRetryUsesSameBytes(t *testing.T) {
	requireVips(t)
	state := &scriptState{
		helloLease:     30,
		helloWait:      0,
		jpg:            readFixture(t, "../../internal/convert/testdata/sanitize/exif_gps.jpg"),
		hijackComplete: true,
	}
	dial, fileURL := bootScript(t, state, false)
	state.setAssignment(scriptAssignment(fileURL), controlJSON{})
	runProtocolUntil(t, dial, 60*time.Second, func() bool {
		state.mu.Lock()
		defer state.mu.Unlock()
		return len(state.completes) >= 2
	})
	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.completes) != 2 || !bytes.Equal(state.completes[0], state.completes[1]) {
		t.Fatal("complete bytes")
	}
	sum := sha256.Sum256(state.putBody)
	if !bytes.Contains(state.completes[0], []byte(hex.EncodeToString(sum[:]))) {
		t.Fatal("artifact sha")
	}
	if len(state.fails) != 0 {
		t.Fatal("fail")
	}
}

func TestPlaintextCoreURLDoesNotDial(t *testing.T) {
	probe := installLegacyGuard(t)
	ln := countAccepts(t)
	mat := mintMaterial(t)
	setCompleteDial(t, "http://"+ln.addr, mat)
	err := dispatch(context.Background())
	if err == nil || probe.called() || ln.accepted() != 0 {
		t.Fatal("plaintext core")
	}
	if !strings.Contains(err.Error(), "https") {
		t.Fatal(publicText(err.Error()))
	}
}

func TestPlaintextHrefIsNotRequested(t *testing.T) {
	ln := countAccepts(t)
	state := &scriptState{helloLease: 30, helloWait: 0}
	dial, _ := bootScript(t, state, true)
	state.setAssignment(scriptAssignment("http://"+ln.addr), controlJSON{})
	err := runProtocol(context.Background(), dial)
	if !errors.Is(err, errOriginRejected) || err.Error() != "origin_rejected" {
		t.Fatal(publicText(err.Error()))
	}
	state.mu.Lock()
	fails := len(state.fails)
	gets := state.fileGets
	state.mu.Unlock()
	if fails != 0 || gets != 0 || ln.accepted() != 0 {
		t.Fatal("plaintext href")
	}
}

func TestRedirectIsOriginRejected(t *testing.T) {
	second := countAccepts(t)
	state := &scriptState{helloLease: 30, helloWait: 0, redirectTo: "http://" + second.addr + "/gone"}
	dial, fileURL := bootScript(t, state, true)
	state.setAssignment(scriptAssignment(fileURL), controlJSON{})
	err := runProtocol(context.Background(), dial)
	if !errors.Is(err, errOriginRejected) || err.Error() != "origin_rejected" {
		t.Fatal(publicText(err.Error()))
	}
	state.mu.Lock()
	fails := len(state.fails)
	gets := append([]headerSnap(nil), state.getSnaps...)
	auth := state.fileAuth
	state.mu.Unlock()
	if fails != 0 || auth || len(gets) == 0 || second.accepted() != 0 {
		t.Fatal("redirect")
	}
	for _, snap := range gets {
		if snap.auth || snap.attempt {
			t.Fatal("file header")
		}
	}
}

func TestFileRequestOmitsAuthorization(t *testing.T) {
	requireVips(t)
	state := &scriptState{helloLease: 30, helloWait: 0, jpg: readFixture(t, "../../internal/convert/testdata/sanitize/exif_gps.jpg")}
	dial, fileURL := bootScript(t, state, true)
	state.setAssignment(scriptAssignment(fileURL), controlJSON{})
	runProtocolUntil(t, dial, 60*time.Second, func() bool {
		state.mu.Lock()
		defer state.mu.Unlock()
		return len(state.completes) >= 1 && len(state.getSnaps) >= 1 && len(state.putSnaps) >= 1 && len(state.pullSnaps) >= 1
	})
	state.mu.Lock()
	gets := append([]headerSnap(nil), state.getSnaps...)
	puts := append([]headerSnap(nil), state.putSnaps...)
	pulls := append([]headerSnap(nil), state.pullSnaps...)
	state.mu.Unlock()
	for _, snap := range append(gets, puts...) {
		if snap.auth || snap.attempt {
			t.Fatal("file header")
		}
	}
	for _, snap := range pulls {
		if !snap.auth || snap.attempt {
			t.Fatal("pull header")
		}
	}
}

func TestProtocolModeDoesNotConstructLegacyClients(t *testing.T) {
	probe := installLegacyGuard(t)
	mat := mintMaterial(t)
	setCompleteDial(t, "https://127.0.0.1:1", mat)
	t.Setenv("WORKER_CONCURRENCY", "4")
	t.Setenv("ENGINE_TIMEOUT", "120s")
	t.Setenv("DATABASE_URL", "postgres://legacy.invalid/db")
	t.Setenv("REDIS_ADDR", "127.0.0.1:1")
	t.Setenv("S3_ACCESS_KEY", "legacy-key")
	t.Setenv("S3_SECRET_KEY", "legacy-secret")
	dial, err := loadProtocolDial()
	if err != nil || !dial.ready {
		t.Fatal("dial")
	}
	err = dispatch(context.Background())
	if err == nil || probe.called() {
		t.Fatal("legacy clients")
	}
}

func TestAbsentDialEnvKeepsImageQueue(t *testing.T) {
	probe := installLegacyCapture(t)
	clearDialEnv(t)
	err := dispatch(context.Background())
	if err != nil {
		t.Fatal(publicText(err.Error()))
	}
	if queue.TypeImageConvert != "image:convert" || probe.task != "image:convert" {
		t.Fatal("task")
	}
	if queue.QueueImage != "image" || probe.queues["image"] != 4 {
		t.Fatal("queue")
	}
	if probe.served.Load() != 1 {
		t.Fatal("serve")
	}
}

func TestPartialDialEnvDoesNotStartLegacy(t *testing.T) {
	probe := installLegacyGuard(t)
	clearDialEnv(t)
	t.Setenv(envProtocolCoreURL, "https://127.0.0.1/")
	err := dispatch(context.Background())
	if err == nil || probe.called() {
		t.Fatal("partial dial")
	}
}

func TestProtocolSourcesDoNotImportWorkerproto(t *testing.T) {
	mainFile := parseGo(t, "main.go")
	protocolFile := parseGo(t, "protocol.go")
	if importsPath(mainFile, "github.com/tileforge/octoconv/internal/workerproto") || importsPath(protocolFile, "github.com/tileforge/octoconv/internal/workerproto") {
		t.Fatal("workerproto import")
	}
	for _, path := range []string{
		"github.com/tileforge/octoconv/internal/worker",
		"github.com/tileforge/octoconv/internal/db",
		"github.com/tileforge/octoconv/internal/jobs",
		"github.com/tileforge/octoconv/internal/storage",
		"github.com/tileforge/octoconv/internal/queue",
	} {
		if importsPath(protocolFile, path) {
			t.Fatal("protocol import")
		}
	}
	calls := callNames(funcBody(t, mainFile, "main"))
	if !callOrder(calls, "RunExecModeIfRequested", "ResolveHelper", "LibvipsPreflight", "dispatch") {
		t.Fatal("main order")
	}
	for _, sel := range []struct{ pkg, name string }{
		{"db", "Connect"},
		{"storage", "New"},
		{"queue", "NewClient"},
		{"redis", "NewClient"},
	} {
		if selectorCount(funcBody(t, mainFile, "runLegacy"), sel.pkg, sel.name) == 0 {
			t.Fatal("missing " + sel.pkg + "." + sel.name)
		}
		for _, decl := range mainFile.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name == "runLegacy" || fn.Body == nil {
				continue
			}
			if selectorCount(fn, sel.pkg, sel.name) != 0 {
				t.Fatal(sel.pkg + "." + sel.name + " outside runLegacy")
			}
		}
	}
}

type legacyProbe struct {
	connects atomic.Int32
	storage  atomic.Int32
	queue    atomic.Int32
	redis    atomic.Int32
	redisOpt atomic.Int32
	served   atomic.Int32
	queues   map[string]int
	task     string
}

func (p *legacyProbe) called() bool {
	return p.connects.Load()+p.storage.Load()+p.queue.Load()+p.redis.Load()+p.redisOpt.Load()+p.served.Load() != 0
}

func clearLegacySeams(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		legacyConnect = nil
		legacyStorage = nil
		legacyQueue = nil
		legacyRedis = nil
		legacyRedisOpt = nil
		legacyServe = nil
	})
}

func installLegacyGuard(t *testing.T) *legacyProbe {
	t.Helper()
	clearLegacySeams(t)
	probe := &legacyProbe{}
	legacyConnect = func(context.Context) (*pgxpool.Pool, error) {
		probe.connects.Add(1)
		return nil, errors.New("legacy connect")
	}
	legacyStorage = func(context.Context) (*storage.Client, error) {
		probe.storage.Add(1)
		return nil, errors.New("legacy storage")
	}
	legacyQueue = func() (*queue.Client, error) {
		probe.queue.Add(1)
		return nil, errors.New("legacy queue")
	}
	legacyRedis = func(*redis.Options) *redis.Client {
		probe.redis.Add(1)
		return nil
	}
	legacyRedisOpt = func() (asynq.RedisClientOpt, error) {
		probe.redisOpt.Add(1)
		return asynq.RedisClientOpt{}, errors.New("legacy redis")
	}
	legacyServe = func(map[string]int, string, *asynq.ServeMux) error {
		probe.served.Add(1)
		return nil
	}
	return probe
}

func installLegacyCapture(t *testing.T) *legacyProbe {
	t.Helper()
	clearLegacySeams(t)
	probe := &legacyProbe{}
	legacyConnect = func(context.Context) (*pgxpool.Pool, error) {
		probe.connects.Add(1)
		return nil, nil
	}
	legacyStorage = func(context.Context) (*storage.Client, error) {
		probe.storage.Add(1)
		return nil, nil
	}
	legacyQueue = func() (*queue.Client, error) {
		probe.queue.Add(1)
		return nil, nil
	}
	legacyRedis = func(*redis.Options) *redis.Client {
		probe.redis.Add(1)
		return nil
	}
	legacyRedisOpt = func() (asynq.RedisClientOpt, error) {
		probe.redisOpt.Add(1)
		return asynq.RedisClientOpt{Addr: "127.0.0.1:1"}, nil
	}
	legacyServe = func(queues map[string]int, task string, _ *asynq.ServeMux) error {
		probe.served.Add(1)
		probe.queues = queues
		probe.task = task
		return nil
	}
	return probe
}

func clearDialEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		envProtocolCoreURL,
		envProtocolCert,
		envProtocolKey,
		envProtocolCA,
		envProtocolWorkerID,
		envProtocolManifest,
	} {
		t.Setenv(key, "")
	}
}

func setCompleteDial(t *testing.T, core string, mat tlsMaterial) {
	t.Helper()
	t.Setenv(envProtocolCoreURL, core)
	t.Setenv(envProtocolCert, mat.certFile)
	t.Setenv(envProtocolKey, mat.keyFile)
	t.Setenv(envProtocolCA, mat.caFile)
	t.Setenv(envProtocolWorkerID, mat.workerID.String())
	t.Setenv(envProtocolManifest, mat.manifestFile)
}

type acceptCounter struct {
	addr string
	n    atomic.Int32
}

func (c *acceptCounter) accepted() int32 { return c.n.Load() }

func countAccepts(t *testing.T) *acceptCounter {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("listen")
	}
	counter := &acceptCounter{addr: ln.Addr().String()}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			counter.n.Add(1)
			_ = conn.Close()
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return counter
}

func parseGo(t *testing.T, name string) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), testPath(name), nil, 0)
	if err != nil {
		t.Fatal("parse")
	}
	return f
}

func importsPath(f *ast.File, path string) bool {
	for _, imp := range f.Imports {
		got, err := strconv.Unquote(imp.Path.Value)
		if err == nil && got == path {
			return true
		}
	}
	return false
}

func funcBody(t *testing.T, f *ast.File, name string) *ast.FuncDecl {
	t.Helper()
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == name && fn.Body != nil {
			return fn
		}
	}
	t.Fatal("func")
	return nil
}

func callNames(fn *ast.FuncDecl) []string {
	var names []string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			names = append(names, fun.Name)
		case *ast.SelectorExpr:
			names = append(names, fun.Sel.Name)
		}
		return true
	})
	return names
}

func callOrder(names []string, want ...string) bool {
	at := 0
	for _, name := range names {
		if at < len(want) && name == want[at] {
			at++
		}
	}
	return at == len(want)
}

func selectorCount(fn *ast.FuncDecl, pkg, name string) int {
	n := 0
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		sel, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if ok && id.Name == pkg && sel.Sel.Name == name {
			n++
		}
		return true
	})
	return n
}

func runProtocolUntil(t *testing.T, dial protocolDial, limit time.Duration, ready func() bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- runProtocol(ctx, dial)
	}()
	deadline := time.Now().Add(limit)
	for !ready() {
		select {
		case err := <-errCh:
			cancel()
			if err != nil {
				t.Fatal(publicText(err.Error()))
			}
			t.Fatal("runProtocol returned")
		default:
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("timed out")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(publicText(err.Error()))
		}
	case <-time.After(20 * time.Second):
		t.Fatal("runProtocol did not return")
	}
}

type scriptState struct {
	mu             sync.Mutex
	helloLease     int
	helloWait      int
	asgn           assignmentJSON
	ctrl           controlJSON
	pulls          int
	progress       [][]byte
	fails          [][]byte
	stopped        [][]byte
	completes      [][]byte
	hijackComplete bool
	leaseCode      string
	stallLease     bool
	stallStopped   bool
	blockGet       bool
	getReleased    int
	stoppedReplies int
	jpg            []byte
	putBody        []byte
	fileGets       int
	fileAuth       bool
	pullSnaps      []headerSnap
	getSnaps       []headerSnap
	putSnaps       []headerSnap
	redirectTo     string
}

type headerSnap struct {
	auth    bool
	attempt bool
}

func (s *scriptState) setAssignment(asgn assignmentJSON, ctrl controlJSON) {
	s.mu.Lock()
	s.asgn = asgn
	s.ctrl = ctrl
	s.mu.Unlock()
}

func (s *scriptState) stoppedReasons() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.stopped))
	for _, raw := range s.stopped {
		var body struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(raw, &body)
		out = append(out, body.Reason)
	}
	return out
}

func (s *scriptState) serveCore(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxProtocolJSONBytes+1))
	if err != nil {
		http.Error(w, "read", http.StatusBadRequest)
		return
	}
	switch {
	case strings.HasSuffix(r.URL.Path, "/pull"):
		s.notePull(r)
		s.writePull(w)
	case strings.HasSuffix(r.URL.Path, "/sessions"):
		s.writeHello(w)
	case strings.HasSuffix(r.URL.Path, "/lease"):
		s.writeLease(w, r)
	case strings.HasSuffix(r.URL.Path, "/progress"):
		s.keep(&s.progress, body)
		writeJSONStatus(w, http.StatusOK, []byte(`{"protocol_version":1}`))
	case strings.HasSuffix(r.URL.Path, "/fail"):
		s.keep(&s.fails, body)
		writeJSONStatus(w, http.StatusOK, []byte(`{"protocol_version":1}`))
	case strings.HasSuffix(r.URL.Path, "/stopped"):
		s.keep(&s.stopped, body)
		s.mu.Lock()
		stall := s.stallStopped
		s.mu.Unlock()
		if stall {
			<-r.Context().Done()
			return
		}
		s.mu.Lock()
		s.stoppedReplies++
		s.mu.Unlock()
		writeJSONStatus(w, http.StatusOK, []byte(`{"protocol_version":1}`))
	case strings.HasSuffix(r.URL.Path, "/complete"):
		if s.maybeHijackComplete(w, body) {
			return
		}
		s.keep(&s.completes, body)
		writeJSONStatus(w, http.StatusOK, []byte(`{"protocol_version":1}`))
	default:
		http.NotFound(w, r)
	}
}

func (s *scriptState) notePull(r *http.Request) {
	s.mu.Lock()
	s.pullSnaps = append(s.pullSnaps, headerSnap{
		auth:    r.Header.Get("Authorization") != "",
		attempt: r.Header.Get("Octoconv-Attempt") != "",
	})
	s.mu.Unlock()
}

func (s *scriptState) serveFile(w http.ResponseWriter, r *http.Request) {
	snap := headerSnap{
		auth:    r.Header.Get("Authorization") != "",
		attempt: r.Header.Get("Octoconv-Attempt") != "",
	}
	if snap.auth || snap.attempt {
		s.mu.Lock()
		s.fileAuth = true
		s.mu.Unlock()
	}
	s.mu.Lock()
	block := s.blockGet
	jpg := append([]byte(nil), s.jpg...)
	s.mu.Unlock()
	if r.Method == http.MethodGet && block {
		<-r.Context().Done()
		s.mu.Lock()
		s.getReleased++
		s.mu.Unlock()
		panic(http.ErrAbortHandler)
	}
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		s.fileGets++
		s.getSnaps = append(s.getSnaps, snap)
		redirect := s.redirectTo
		s.mu.Unlock()
		if redirect != "" {
			w.Header().Set("Location", redirect)
			w.WriteHeader(http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(jpg)
	case http.MethodPut:
		raw, err := io.ReadAll(io.LimitReader(r.Body, maxProtocolFileBytes+1))
		if err != nil {
			http.Error(w, "read", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		s.putBody = append([]byte(nil), raw...)
		s.putSnaps = append(s.putSnaps, snap)
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	default:
		http.Error(w, "method", http.StatusMethodNotAllowed)
	}
}

func (s *scriptState) keep(dst *[][]byte, body []byte) {
	s.mu.Lock()
	*dst = append(*dst, append([]byte(nil), body...))
	s.mu.Unlock()
}

func (s *scriptState) writeHello(w http.ResponseWriter) {
	s.mu.Lock()
	hello := helloJSON{
		SessionID:       uuid.NewString(),
		SessionToken:    "session-token",
		LeaseSeconds:    s.helloLease,
		PullWaitSeconds: s.helloWait,
	}
	s.mu.Unlock()
	raw, err := json.Marshal(hello)
	if err != nil {
		http.Error(w, "json", http.StatusInternalServerError)
		return
	}
	writeJSONStatus(w, http.StatusOK, raw)
}

func (s *scriptState) writePull(w http.ResponseWriter) {
	s.mu.Lock()
	s.pulls++
	pull := pullResponseJSON{Assignments: []assignmentJSON{}, Control: controlJSON{CancelAttemptIDs: []string{}}}
	if s.pulls == 1 {
		pull.Assignments = []assignmentJSON{s.asgn}
		pull.Control = s.ctrl
	}
	s.mu.Unlock()
	raw, err := json.Marshal(pull)
	if err != nil {
		http.Error(w, "json", http.StatusInternalServerError)
		return
	}
	writeJSONStatus(w, http.StatusOK, raw)
}

func (s *scriptState) writeLease(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	code := s.leaseCode
	stall := s.stallLease
	s.mu.Unlock()
	if stall {
		<-r.Context().Done()
		return
	}
	if code == "attempt_not_active" {
		writeJSONStatus(w, http.StatusConflict, []byte(`{"code":"attempt_not_active","message":"attempt not active","retryable":false}`))
		return
	}
	when := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	raw, err := json.Marshal(leaseResponseJSON{LeaseExpiresAt: when})
	if err != nil {
		http.Error(w, "json", http.StatusInternalServerError)
		return
	}
	writeJSONStatus(w, http.StatusOK, raw)
}

func (s *scriptState) maybeHijackComplete(w http.ResponseWriter, body []byte) bool {
	s.mu.Lock()
	hijack := s.hijackComplete && len(s.completes) == 0
	if hijack {
		s.completes = append(s.completes, append([]byte(nil), body...))
	}
	s.mu.Unlock()
	if !hijack {
		return false
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack", http.StatusInternalServerError)
		return true
	}
	conn, _, err := hj.Hijack()
	if err != nil {
		return true
	}
	_ = conn.Close()
	return true
}

func scriptAssignment(fileBase string) assignmentJSON {
	when := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	return assignmentJSON{
		JobID:             uuid.NewString(),
		StepID:            uuid.NewString(),
		AttemptID:         uuid.NewString(),
		AttemptToken:      "attempt-token",
		CapabilityID:      "octoconv.convert.image",
		CapabilityVersion: 1,
		AttemptDeadline:   when,
		LeaseExpiresAt:    when,
		Limits:            limitsJSON{MaxOutputBytes: 100 << 20},
		Inputs: []fileLinkJSON{{
			Role: "source", Ordinal: 0, Format: "jpg", Href: fileBase + "/objects",
		}},
		Outputs: []fileLinkJSON{{
			Role: "primary", Ordinal: 0, Format: "png", Href: fileBase + "/objects",
		}},
	}
}

func bootScript(t *testing.T, state *scriptState, h2 bool) (protocolDial, string) {
	t.Helper()
	mat := mintMaterial(t)
	coreAddr := serveTLS(t, serverConfig(mat.server, h2), http.HandlerFunc(state.serveCore), h2)
	fileAddr := serveTLS(t, serverConfig(mat.server, h2), http.HandlerFunc(state.serveFile), h2)
	coreURL, err := url.Parse("https://" + coreAddr)
	if err != nil {
		t.Fatal("core url")
	}
	return protocolDial{
		ready:        true,
		core:         coreURL,
		certFile:     mat.certFile,
		keyFile:      mat.keyFile,
		caFile:       mat.caFile,
		manifestFile: mat.manifestFile,
		workerID:     mat.workerID,
		freeSlots:    4,
		engineLimit:  120 * time.Second,
	}, "https://" + fileAddr
}

type tlsMaterial struct {
	workerID     uuid.UUID
	ca           *x509.Certificate
	server       tls.Certificate
	certFile     string
	keyFile      string
	caFile       string
	manifestFile string
}

func mintMaterial(t *testing.T) tlsMaterial {
	t.Helper()
	ca, caKey := mintCA(t)
	workerID := uuid.New()
	client := mintClient(t, ca, caKey, workerID)
	server := mintIPServer(t, ca, caKey)
	dir := t.TempDir()
	certFile := filepath.Join(dir, "client.pem")
	keyFile := filepath.Join(dir, "client.key")
	caFile := filepath.Join(dir, "ca.pem")
	manifestFile := filepath.Join(dir, "manifest.json")
	writeCertPEM(t, certFile, client.Certificate)
	writeKeyPEM(t, keyFile, client.PrivateKey)
	writeCertPEM(t, caFile, [][]byte{ca.Raw})
	if err := os.WriteFile(manifestFile, []byte("manifest\n"), 0o600); err != nil {
		t.Fatal("manifest file")
	}
	return tlsMaterial{
		workerID:     workerID,
		ca:           ca,
		server:       server,
		certFile:     certFile,
		keyFile:      keyFile,
		caFile:       caFile,
		manifestFile: manifestFile,
	}
}

func mintCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal("ca key")
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "octoconv-test-operator"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal("ca cert")
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal("parse ca")
	}
	return cert, key
}

func mintClient(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, workerID uuid.UUID) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal("client key")
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	if err != nil {
		t.Fatal("client serial")
	}
	uri := url.URL{Scheme: "urn", Opaque: "octoconv:worker:" + workerID.String()}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: workerID.String()},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		URIs:         []*url.URL{&uri},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal("client cert")
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal("parse client")
	}
	return tls.Certificate{Certificate: [][]byte{der, ca.Raw}, PrivateKey: key, Leaf: leaf}
}

func mintIPServer(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal("server key")
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	if err != nil {
		t.Fatal("server serial")
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal("server cert")
	}
	return tls.Certificate{Certificate: [][]byte{der, ca.Raw}, PrivateKey: key}
}

func serverConfig(cert tls.Certificate, h2 bool) *tls.Config {
	next := []string{"http/1.1"}
	if h2 {
		next = []string{"h2", "http/1.1"}
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		MaxVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.NoClientCert,
		NextProtos:   next,
	}
}

func fileServerConfig(cert tls.Certificate) *tls.Config {
	cfg := serverConfig(cert, true)
	cfg.ClientAuth = tls.NoClientCert
	return cfg
}

func serveTLS(t *testing.T, cfg *tls.Config, h http.Handler, h2 bool) string {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		t.Fatal("listen")
	}
	srv := &http.Server{
		Handler:           h,
		TLSConfig:         cfg,
		ReadHeaderTimeout: 5 * time.Second,
	}
	if !h2 {
		srv.TLSNextProto = map[string]func(*http.Server, *tls.Conn, http.Handler){}
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().String()
}

func writeCertPEM(t *testing.T, path string, ders [][]byte) {
	t.Helper()
	var buf bytes.Buffer
	for _, der := range ders {
		if err := pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
			t.Fatal("pem")
		}
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal("pem file")
	}
}

func writeKeyPEM(t *testing.T, path string, key any) {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal("key")
	}
	var buf bytes.Buffer
	if err := pem.Encode(&buf, &pem.Block{Type: "PRIVATE KEY", Bytes: der}); err != nil {
		t.Fatal("pem")
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal("pem file")
	}
}

func writeJSONStatus(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

type coreTap struct {
	next            http.Handler
	mu              sync.Mutex
	completes       [][]byte
	helloAuth       atomic.Bool
	pullMissingAuth atomic.Bool
	pullAttempt     atomic.Bool
}

func (c *coreTap) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/pull"):
		if r.Header.Get("Authorization") == "" {
			c.pullMissingAuth.Store(true)
		}
		if r.Header.Get("Octoconv-Attempt") != "" {
			c.pullAttempt.Store(true)
		}
	case strings.HasSuffix(r.URL.Path, "/sessions"):
		if r.Header.Get("Authorization") != "" {
			c.helloAuth.Store(true)
		}
	}
	if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/complete") {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		c.mu.Lock()
		c.completes = append(c.completes, append([]byte(nil), raw...))
		c.mu.Unlock()
	}
	c.next.ServeHTTP(w, r)
}

type objectBytes struct {
	mu       sync.Mutex
	objects  map[string][]byte
	base     string
	fileAuth atomic.Bool
}

func newObjectBytes() *objectBytes {
	return &objectBytes{objects: map[string][]byte{}}
}

func (s *objectBytes) setBase(base string) {
	s.mu.Lock()
	s.base = base
	s.mu.Unlock()
}

func (s *objectBytes) link(key string) (string, error) {
	s.mu.Lock()
	base := s.base
	s.mu.Unlock()
	if base == "" {
		return "", errors.New("file base")
	}
	return base + "/objects?key=" + url.QueryEscape(key), nil
}

func (s *objectBytes) Upload(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	body, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.objects[key] = body
	s.mu.Unlock()
	return nil
}

func (s *objectBytes) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	return s.link(key)
}

func (s *objectBytes) PresignPut(_ context.Context, key string, _ time.Duration) (string, error) {
	return s.link(key)
}

func (s *objectBytes) Copy(_ context.Context, srcKey, dstKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, ok := s.objects[srcKey]
	if !ok {
		return storage.ErrNotFound
	}
	s.objects[dstKey] = append([]byte(nil), payload...)
	return nil
}

func (s *objectBytes) ObjectSize(_ context.Context, key string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, ok := s.objects[key]
	if !ok {
		return 0, storage.ErrNotFound
	}
	return int64(len(payload)), nil
}

func (s *objectBytes) Download(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, ok := s.objects[key]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), payload...))), nil
}

func (s *objectBytes) Remove(_ context.Context, key string) error {
	s.mu.Lock()
	delete(s.objects, key)
	s.mu.Unlock()
	return nil
}

func (s *objectBytes) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "" || r.Header.Get("Octoconv-Attempt") != "" {
		s.fileAuth.Store(true)
	}
	key := r.URL.Query().Get("key")
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		payload, ok := s.objects[key]
		if ok {
			payload = append([]byte(nil), payload...)
		}
		s.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	case http.MethodPut:
		body, err := io.ReadAll(io.LimitReader(r.Body, maxProtocolFileBytes+1))
		if err != nil {
			http.Error(w, "read", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		s.objects[key] = body
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	default:
		http.Error(w, "method", http.StatusMethodNotAllowed)
	}
}

type apiStubs struct {
	image atomic.Int32
}

func (s *apiStubs) EnqueueImageConvert(context.Context, uuid.UUID) error {
	s.image.Add(1)
	return errors.New("enqueue")
}

func (s *apiStubs) EnqueueDocumentConvert(context.Context, uuid.UUID) error { return nil }
func (s *apiStubs) EnqueueHTMLConvert(context.Context, uuid.UUID) error     { return nil }
func (s *apiStubs) EnqueueAudioConvert(context.Context, uuid.UUID) error    { return nil }
func (s *apiStubs) EnqueueAVConvert(context.Context, uuid.UUID) error       { return nil }
func (s *apiStubs) EnqueueOCRConvert(context.Context, uuid.UUID) error      { return nil }
func (s *apiStubs) EnqueuePandocConvert(context.Context, uuid.UUID) error   { return nil }
func (s *apiStubs) EnqueueProbeSanitize(context.Context, uuid.UUID) error   { return nil }
func (s *apiStubs) EnqueueTectonicConvert(context.Context, uuid.UUID) error { return nil }
func (s *apiStubs) EnqueueCADJob(context.Context, uuid.UUID) error          { return nil }
func (s *apiStubs) Resolve(context.Context, uuid.UUID, string) (*presets.Preset, error) {
	return nil, presets.ErrNotFound
}
func (s *apiStubs) Create(context.Context, presets.CreateParams) (uuid.UUID, int, error) {
	return uuid.Nil, 0, nil
}
func (s *apiStubs) Update(context.Context, string, *uuid.UUID, string, string, map[string]any, string) (int, error) {
	return 0, nil
}
func (s *apiStubs) Deactivate(context.Context, string, *uuid.UUID, string) error { return nil }
func (s *apiStubs) Get(context.Context, string, *uuid.UUID, string) (*presets.Preset, error) {
	return nil, presets.ErrNotFound
}
func (s *apiStubs) List(context.Context, string, *uuid.UUID, bool) ([]presets.Preset, error) {
	return nil, nil
}
func (s *apiStubs) ListForClient(context.Context, *uuid.UUID, bool) ([]presets.Preset, error) {
	return nil, nil
}
func (s *apiStubs) GetForClient(context.Context, *uuid.UUID, string) (*presets.Preset, error) {
	return nil, presets.ErrNotFound
}
func (s *apiStubs) UsageSummary(context.Context, *uuid.UUID, time.Time, time.Time) ([]jobs.UsageRow, error) {
	return nil, nil
}
func (s *apiStubs) UTCToday(context.Context) (time.Time, error) { return time.Time{}, nil }
func (s *apiStubs) ListAllLimits(context.Context, *uuid.UUID) ([]clients.LimitWithMeta, error) {
	return nil, nil
}

type fixedResolver struct {
	client *clients.Client
}

func (f fixedResolver) ResolveClient(context.Context, string) (*clients.Client, error) {
	return f.client, nil
}

func httptestServer(t *testing.T, h http.Handler) *http.Server {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("listen")
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	srv.Addr = ln.Addr().String()
	return srv
}

func postExifJob(t *testing.T, base string, jpg []byte) uuid.UUID {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", "exif_gps.jpg")
	if err != nil {
		t.Fatal("form")
	}
	if _, err := fw.Write(jpg); err != nil {
		t.Fatal("form")
	}
	if err := w.WriteField("target", "png"); err != nil {
		t.Fatal("form")
	}
	if err := w.Close(); err != nil {
		t.Fatal("form")
	}
	req, err := http.NewRequest(http.MethodPost, "http://"+base+"/v1/jobs", &buf)
	if err != nil {
		t.Fatal("request")
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "ApiKey tracer")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal("create")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal("create body")
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("create status %d: %s", resp.StatusCode, publicText(string(raw)))
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal("create json")
	}
	if body["status"] != "queued" {
		t.Fatal("create status field")
	}
	if _, ok := body["capability_id"]; ok {
		t.Fatal("capability_id")
	}
	id, err := uuid.Parse(fmtString(body["job_id"]))
	if err != nil {
		t.Fatal("job id")
	}
	return id
}

func pollJob(t *testing.T, base string, id uuid.UUID, errCh <-chan error) map[string]any {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for {
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatal(publicText(err.Error()))
			}
			t.Fatal("runProtocol returned")
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("job did not finish")
		}
		req, err := http.NewRequest(http.MethodGet, "http://"+base+"/v1/jobs/"+id.String(), nil)
		if err != nil {
			t.Fatal("request")
		}
		req.Header.Set("Authorization", "ApiKey tracer")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal("get job")
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal("get body")
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("get status %d: %s", resp.StatusCode, publicText(string(raw)))
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal("get json")
		}
		switch body["status"] {
		case "done":
			return body
		case "failed":
			t.Fatal(publicText(fmtString(body["error_code"]) + " " + fmtString(body["error_message"])))
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func downloadHTTPS(t *testing.T, pool *x509.CertPool, rawURL string) []byte {
	t.Helper()
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS13,
				RootCAs:    pool,
			},
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("redirect")
		},
	}
	resp, err := client.Get(rawURL)
	if err != nil {
		t.Fatal("download")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxProtocolFileBytes+1))
	if err != nil {
		t.Fatal("download body")
	}
	return body
}

func protocolPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	requireDatabase(t)
	ctx := context.Background()
	pool, err := db.Connect(ctx)
	if err != nil {
		t.Fatal("postgres")
	}
	if err := db.Migrate(ctx, pool); err != nil {
		pool.Close()
		fatalDB(t, err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func resetProtocolTables(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		DELETE FROM worker_request_dedup;
		DELETE FROM worker_attempt_credentials;
		DELETE FROM worker_slot_holds;
		DELETE FROM worker_sessions;
		DELETE FROM worker_certificate_denylist;
		DELETE FROM worker_capability_formats;
		DELETE FROM worker_capabilities;
		DELETE FROM worker_identities`)
	fatalDB(t, err)
}

func insertProtocolClient(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(context.Background(), `INSERT INTO clients (name) VALUES ($1) RETURNING id`, "protocol-tracer").Scan(&id)
	fatalDB(t, err)
	return id
}

func requireDatabase(t *testing.T) {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" {
		loadDotEnvDatabase(t)
	}
	if os.Getenv("DATABASE_URL") == "" {
		t.Fatal("DATABASE_URL is empty")
	}
}

func loadDotEnvDatabase(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(testPath("../../.env"))
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "DATABASE_URL" {
			continue
		}
		value = strings.Trim(firstField(strings.TrimSpace(value)), `"'`)
		if value != "" {
			t.Setenv("DATABASE_URL", value)
		}
		return
	}
}

func requireVips(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("vips"); err != nil {
		t.Fatal("vips is not on PATH")
	}
}

func fatalDB(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	t.Fatal(publicText(err.Error()))
}

func publicText(msg string) string {
	if strings.Contains(msg, "://") {
		return "redacted"
	}
	return msg
}

func fmtString(v any) string {
	s, _ := v.(string)
	return s
}

func fmtError(format string, err error) error {
	return fmt.Errorf(format, err)
}

func readFixture(t *testing.T, rel string) []byte {
	t.Helper()
	body, err := os.ReadFile(testPath(rel))
	if err != nil {
		t.Fatal("fixture")
	}
	return body
}

func testPath(rel string) string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return rel
	}
	return filepath.Join(filepath.Dir(file), rel)
}
