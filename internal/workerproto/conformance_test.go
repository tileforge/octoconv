package workerproto

import (
	"bufio"
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
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRevokeOnLiveConnection(t *testing.T) {
	pool := testPool(t)
	resetWorkerState(t, pool)
	repo := NewRepo(pool)
	workerID := uuid.New()
	manifest := readPositiveFixture(t, "manifest-image.json")
	ctx := context.Background()
	if err := repo.Approve(ctx, workerID, manifest); err != nil {
		t.Fatalf("approve: %v", err)
	}
	digest := manifestDigest(manifest)
	ca, caKey := mintTestCA(t)
	addr := listenWorker(t, repo, ca, caKey)
	cert := mintClientCert(t, ca, caKey, workerID)

	dials := 0
	dial := func() *tls.Conn {
		dials++
		if dials != 1 {
			t.Fatal("dialed again")
		}
		return dialWorkerTLS(t, addr, ca, cert, "worker.octoconv.local")
	}
	conn := dial()
	local := conn.LocalAddr().String()
	client := &rawHTTP{t: t, conn: conn, reader: bufio.NewReader(conn)}
	body := helloBody(workerID, digest, 1)
	status, _, respBody := client.exchange(http.MethodPost, "/worker/v1/sessions", "", body, nil)
	if status != http.StatusOK {
		t.Fatalf("HELLO status %d", status)
	}
	sessionID, token := decodeHello(t, respBody)
	hb := []byte(`{"protocol_version":1}`)
	path := "/worker/v1/sessions/" + sessionID.String() + "/heartbeat"
	status, _, _ = client.exchange(http.MethodPost, path, token, hb, nil)
	if status != http.StatusOK {
		t.Fatalf("heartbeat status %d", status)
	}

	if err := repo.RevokeIdentity(ctx, workerID, cert.Leaf.SerialNumber, issuerSHA256(cert.Leaf)); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	var hdr http.Header
	status, hdr, respBody = client.exchange(http.MethodPost, path, token, hb, nil)
	assertProtocolError(t, status, hdr, respBody, http.StatusUnauthorized, "session_closed")

	var closed bool
	var exists bool
	if err := pool.QueryRow(ctx, `
		SELECT closed_at IS NOT NULL, true
		FROM worker_sessions WHERE id = $1`, sessionID).Scan(&closed, &exists); err != nil {
		t.Fatalf("read session: %v", err)
	}
	if !exists || !closed {
		t.Fatal("revoked session row did not stay closed")
	}
	status, hdr, respBody = client.exchange(http.MethodPost, "/worker/v1/sessions", "", body, nil)
	assertProtocolError(t, status, hdr, respBody, http.StatusUnauthorized, "session_closed")
	var openSessions, sessions int
	if err := pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE closed_at IS NULL),
			count(*)
		FROM worker_sessions`).Scan(&openSessions, &sessions); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if openSessions != 0 || sessions != 1 {
		t.Fatalf("open sessions = %d, sessions = %d", openSessions, sessions)
	}
	var attempts int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM worker_attempt_credentials`).Scan(&attempts); err != nil {
		t.Fatalf("count attempts: %v", err)
	}
	if attempts != 0 {
		t.Fatalf("attempts = %d, want 0", attempts)
	}
	if dials != 1 || conn.LocalAddr().String() != local {
		t.Fatal("dialed again")
	}
}

func TestDenylistSerialLeavesNewCertificate(t *testing.T) {
	pool := testPool(t)
	resetWorkerState(t, pool)
	repo := NewRepo(pool)
	workerID := uuid.New()
	manifest := readPositiveFixture(t, "manifest-image.json")
	ctx := context.Background()
	if err := repo.Approve(ctx, workerID, manifest); err != nil {
		t.Fatalf("approve: %v", err)
	}
	digest := manifestDigest(manifest)
	ca, caKey := mintTestCA(t)
	addr := listenWorker(t, repo, ca, caKey)
	oldCert := mintClientCert(t, ca, caKey, workerID)
	newCert := mintClientCert(t, ca, caKey, workerID)
	if oldCert.Leaf.SerialNumber.Cmp(newCert.Leaf.SerialNumber) == 0 {
		t.Fatal("certificate serials are not distinct")
	}
	oldConn := dialWorkerTLS(t, addr, ca, oldCert, "worker.octoconv.local")
	newConn := dialWorkerTLS(t, addr, ca, newCert, "worker.octoconv.local")
	oldClient := &rawHTTP{t: t, conn: oldConn, reader: bufio.NewReader(oldConn)}
	newClient := &rawHTTP{t: t, conn: newConn, reader: bufio.NewReader(newConn)}
	body := helloBody(workerID, digest, 1)
	oldID := helloOn(t, oldClient, body)
	newID := helloOn(t, newClient, body)
	hb := []byte(`{"protocol_version":1}`)

	if err := repo.DenylistSerial(ctx, workerID, oldCert.Leaf.SerialNumber, issuerSHA256(oldCert.Leaf)); err != nil {
		t.Fatalf("denylist: %v", err)
	}
	var oldClosed, newClosed bool
	if err := pool.QueryRow(ctx, `
		SELECT
			(SELECT closed_at IS NOT NULL FROM worker_sessions WHERE id = $1),
			(SELECT closed_at IS NOT NULL FROM worker_sessions WHERE id = $2)`,
		oldID.sessionID, newID.sessionID).Scan(&oldClosed, &newClosed); err != nil {
		t.Fatalf("read sessions: %v", err)
	}
	if !oldClosed || newClosed {
		t.Fatalf("old closed=%v new closed=%v", oldClosed, newClosed)
	}
	status, hdr, respBody := oldClient.exchange(http.MethodPost, oldID.heartbeatPath(), oldID.token, hb, nil)
	assertProtocolError(t, status, hdr, respBody, http.StatusUnauthorized, "session_closed")
	status, hdr, respBody = newClient.exchange(http.MethodPost, newID.heartbeatPath(), newID.token, hb, nil)
	if status != http.StatusOK {
		t.Fatalf("new certificate heartbeat status %d body %s", status, respBody)
	}
	assertNoStoreJSON(t, hdr)
}

func TestRevokeDoesNotZeroSlotHold(t *testing.T) {
	pool := testPool(t)
	resetWorkerState(t, pool)
	repo := NewRepo(pool)
	workerID := uuid.New()
	manifest := readPositiveFixture(t, "manifest-image.json")
	ctx := context.Background()
	if err := repo.Approve(ctx, workerID, manifest); err != nil {
		t.Fatalf("approve: %v", err)
	}
	ca, caKey := mintTestCA(t)
	addr := listenWorker(t, repo, ca, caKey)
	cert := mintClientCert(t, ca, caKey, workerID)
	conn := dialWorkerTLS(t, addr, ca, cert, "worker.octoconv.local")
	client := &rawHTTP{t: t, conn: conn, reader: bufio.NewReader(conn)}
	opened := helloOn(t, client, helloBody(workerID, manifestDigest(manifest), 1))

	holdID := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO worker_slot_holds (id, owner_key, session_id) VALUES ($1, $2, $3)`,
		holdID, "owner-image", opened.sessionID); err != nil {
		t.Fatalf("insert hold: %v", err)
	}
	before := readHold(t, pool, holdID)
	if err := repo.RevokeIdentity(ctx, workerID, cert.Leaf.SerialNumber, issuerSHA256(cert.Leaf)); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	after := readHold(t, pool, holdID)
	if after != before {
		t.Fatal("slot hold changed")
	}
	var closed bool
	if err := pool.QueryRow(ctx, `
		SELECT closed_at IS NOT NULL FROM worker_sessions WHERE id = $1`, opened.sessionID).Scan(&closed); err != nil {
		t.Fatalf("read session: %v", err)
	}
	if !closed {
		t.Fatal("revoke did not close the session")
	}
	var holds int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM worker_slot_holds`).Scan(&holds); err != nil {
		t.Fatalf("count holds: %v", err)
	}
	if holds != 1 {
		t.Fatalf("holds = %d", holds)
	}
}

type openedHello struct {
	sessionID uuid.UUID
	token     string
}

func (o openedHello) heartbeatPath() string {
	return "/worker/v1/sessions/" + o.sessionID.String() + "/heartbeat"
}

func helloOn(t *testing.T, client *rawHTTP, body []byte) openedHello {
	t.Helper()
	status, _, respBody := client.exchange(http.MethodPost, "/worker/v1/sessions", "", body, nil)
	if status != http.StatusOK {
		t.Fatalf("HELLO status %d", status)
	}
	sessionID, token := decodeHello(t, respBody)
	return openedHello{sessionID: sessionID, token: token}
}

func decodeHello(t *testing.T, body []byte) (uuid.UUID, string) {
	t.Helper()
	var hello struct {
		SessionID    string `json:"session_id"`
		SessionToken string `json:"session_token"`
	}
	if err := json.Unmarshal(body, &hello); err != nil {
		t.Fatalf("decode HELLO: %v", err)
	}
	sessionID, err := uuid.Parse(hello.SessionID)
	if err != nil {
		t.Fatal("session_id is not a uuid")
	}
	return sessionID, hello.SessionToken
}

func helloBody(workerID uuid.UUID, digest string, slots int) []byte {
	return []byte(fmt.Sprintf(
		`{"protocol_version":1,"worker_id":"%s","manifest_sha256":"%s","free_slots":%d}`,
		workerID.String(), digest, slots,
	))
}

func manifestDigest(file []byte) string {
	sum := sha256.Sum256(file)
	return hex.EncodeToString(sum[:])
}

type rawHTTP struct {
	t      *testing.T
	conn   *tls.Conn
	reader *bufio.Reader
}

func (c *rawHTTP) exchange(method, path, token string, body []byte, extra http.Header) (int, http.Header, []byte) {
	c.t.Helper()
	if err := c.conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		c.t.Fatalf("deadline: %v", err)
	}
	var req bytes.Buffer
	fmt.Fprintf(&req, "%s %s HTTP/1.1\r\n", method, path)
	req.WriteString("Host: worker.octoconv.local\r\n")
	req.WriteString("Content-Type: application/json\r\n")
	fmt.Fprintf(&req, "Content-Length: %d\r\n", len(body))
	req.WriteString("Connection: keep-alive\r\n")
	if token != "" {
		fmt.Fprintf(&req, "Authorization: Bearer %s\r\n", token)
	}
	for key, values := range extra {
		for _, value := range values {
			fmt.Fprintf(&req, "%s: %s\r\n", key, value)
		}
	}
	req.WriteString("\r\n")
	req.Write(body)
	if _, err := c.conn.Write(req.Bytes()); err != nil {
		c.t.Fatalf("write: %v", err)
	}
	resp, err := http.ReadResponse(c.reader, nil)
	if err != nil {
		c.t.Fatalf("read: %v", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("body: %v", err)
	}
	return resp.StatusCode, resp.Header.Clone(), respBody
}

func dialWorkerTLS(t *testing.T, addr string, ca *x509.Certificate, cert tls.Certificate, serverName string) *tls.Conn {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	conn, err := tls.Dial("tcp", addr, &tls.Config{
		MinVersion:   tls.VersionTLS13,
		MaxVersion:   tls.VersionTLS13,
		RootCAs:      roots,
		Certificates: []tls.Certificate{cert},
		ServerName:   serverName,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestPlaintextDialCreatesNoSession(t *testing.T) {
	pool := testPool(t)
	resetWorkerState(t, pool)
	repo := NewRepo(pool)
	workerID := uuid.New()
	manifest := readPositiveFixture(t, "manifest-image.json")
	if err := repo.Approve(context.Background(), workerID, manifest); err != nil {
		t.Fatalf("approve: %v", err)
	}
	ca, caKey := mintTestCA(t)
	addr := listenWorker(t, repo, ca, caKey)
	cfg := ListenerConfig(x509.NewCertPool(), nil)
	if cfg.MinVersion != tls.VersionTLS13 || cfg.MaxVersion != tls.VersionTLS13 {
		t.Fatal("listener is not pinned to TLS 1.3")
	}
	serverCfg, err := ServerTLSConfig(repo)
	if err != nil {
		t.Fatalf("server tls: %v", err)
	}
	if serverCfg.MinVersion != tls.VersionTLS13 || serverCfg.ClientAuth != tls.RequireAndVerifyClientCert || serverCfg.CipherSuites != nil {
		t.Fatal("server tls config can leave TLS optional")
	}
	src, err := os.ReadFile("tlsconfig.go")
	if err != nil {
		t.Fatalf("read tls config: %v", err)
	}
	for _, forbidden := range []string{"InsecureSkipVerify", "SystemCertPool", "h2c"} {
		if bytes.Contains(src, []byte(forbidden)) {
			t.Fatalf("tls config contains %s", forbidden)
		}
	}

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	body := helloBody(workerID, manifestDigest(manifest), 1)
	req := fmt.Sprintf("POST /worker/v1/sessions HTTP/1.1\r\nHost: worker.octoconv.local\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s", len(body), body)
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 2048)
	n, _ := conn.Read(buf)
	reply := buf[:n]
	if bytes.Contains(reply, []byte("session_token")) || bytes.Contains(reply, []byte(" 200 ")) {
		t.Fatalf("plaintext HELLO succeeded: %q", reply)
	}
	if !bytes.Contains(reply, []byte("HTTPS server")) {
		t.Fatalf("plaintext dial was not refused: %q", reply)
	}
	if allSessions(t, pool) != 0 {
		t.Fatal("plaintext dial created a session")
	}
}

func TestBadClientCertificateCreatesNoSession(t *testing.T) {
	pool := testPool(t)
	resetWorkerState(t, pool)
	repo := NewRepo(pool)
	workerID := uuid.New()
	manifest := readPositiveFixture(t, "manifest-image.json")
	if err := repo.Approve(context.Background(), workerID, manifest); err != nil {
		t.Fatalf("approve: %v", err)
	}
	ca, caKey := mintTestCA(t)
	addr := listenWorker(t, repo, ca, caKey)
	good := mintClientCert(t, ca, caKey, workerID)
	if !applicationAccepted(t, addr, ca, &good, "worker.octoconv.local") {
		t.Fatal("approved certificate did not complete TLS")
	}
	if allSessions(t, pool) != 0 {
		t.Fatal("handshake alone created a session")
	}

	otherCA, otherKey := mintTestCA(t)
	other := mintClientCert(t, otherCA, otherKey, workerID)
	assertHandshakeRejected(t, pool, addr, ca, other, "worker.octoconv.local", "other ca")

	noURI := mintVariantCert(t, ca, caKey, nil)
	assertHandshakeRejected(t, pool, addr, ca, noURI, "worker.octoconv.local", "missing uri")

	upper := url.URL{Scheme: "urn", Opaque: "octoconv:worker:" + strings.ToUpper(workerID.String())}
	badURI := mintVariantCert(t, ca, caKey, []*url.URL{&upper})
	assertHandshakeRejected(t, pool, addr, ca, badURI, "worker.octoconv.local", "non-canonical uri")

	unknown := mintClientCert(t, ca, caKey, uuid.New())
	assertHandshakeRejected(t, pool, addr, ca, unknown, "worker.octoconv.local", "unknown worker")
}

func TestServerNameMismatchCreatesNoSession(t *testing.T) {
	pool := testPool(t)
	resetWorkerState(t, pool)
	repo := NewRepo(pool)
	workerID := uuid.New()
	manifest := readPositiveFixture(t, "manifest-image.json")
	if err := repo.Approve(context.Background(), workerID, manifest); err != nil {
		t.Fatalf("approve: %v", err)
	}
	ca, caKey := mintTestCA(t)
	addr := listenWorker(t, repo, ca, caKey)
	cert := mintClientCert(t, ca, caKey, workerID)
	assertHandshakeRejected(t, pool, addr, ca, cert, "not-worker.example", "server name")
	if !applicationAccepted(t, addr, ca, &cert, "worker.octoconv.local") {
		t.Fatal("matching server name did not complete TLS")
	}
	if allSessions(t, pool) != 0 {
		t.Fatal("server name mismatch created a session")
	}
}

func assertHandshakeRejected(t *testing.T, pool *pgxpool.Pool, addr string, ca *x509.Certificate, cert tls.Certificate, serverName, label string) {
	t.Helper()
	before := allSessions(t, pool)
	if applicationAccepted(t, addr, ca, &cert, serverName) {
		t.Fatalf("%s handshake succeeded", label)
	}
	if allSessions(t, pool) != before {
		t.Fatalf("%s created a session", label)
	}
}

func applicationAccepted(t *testing.T, addr string, ca *x509.Certificate, cert *tls.Certificate, serverName string) bool {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS13,
		MaxVersion: tls.VersionTLS13,
		RootCAs:    roots,
		ServerName: serverName,
	}
	if cert != nil {
		cfg.Certificates = []tls.Certificate{*cert}
	}
	conn, err := tls.Dial("tcp", addr, cfg)
	if err != nil {
		return false
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	if _, err := conn.Write([]byte("GET /worker/v1/sessions HTTP/1.1\r\nHost: worker.octoconv.local\r\n\r\n")); err != nil {
		return false
	}
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if n == 0 || err != nil && !bytes.HasPrefix(buf[:n], []byte("HTTP/")) {
		return false
	}
	return bytes.HasPrefix(buf[:n], []byte("HTTP/"))
}

func mintVariantCert(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, uris []*url.URL) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("variant key: %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	if err != nil {
		t.Fatalf("variant serial: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "variant"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		URIs:         uris,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("variant cert: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse variant: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der, ca.Raw}, PrivateKey: key, Leaf: leaf}
}

func allSessions(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM worker_sessions`).Scan(&n); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	return n
}

func TestAttemptTokenOnWrongAttempt(t *testing.T) {
	live := openHello(t)
	tokenA, hashA, _, hashB, attemptA, attemptB := insertAttemptPair(t, live)
	beforeA := readAttemptCredential(t, live.pool, attemptA)
	beforeB := readAttemptCredential(t, live.pool, attemptB)
	if beforeA.tokenHash != hashA || beforeB.tokenHash != hashB {
		t.Fatal("stored attempt hash is not the minted hash")
	}
	before := readGateState(t, live.pool)
	for _, path := range attemptPaths(live.addr, attemptB) {
		header := make(http.Header)
		header.Set("Octoconv-Attempt", tokenA)
		status, hdr, body := doJSON(t, live.client, http.MethodPost, path, live.token, []byte(`{"protocol_version":1}`), header)
		assertProtocolError(t, status, hdr, body, http.StatusForbidden, "capability_denied")
		if readAttemptCredential(t, live.pool, attemptA) != beforeA || readAttemptCredential(t, live.pool, attemptB) != beforeB {
			t.Fatal("wrong attempt token changed a credential")
		}
		if readGateState(t, live.pool) != before {
			t.Fatal("wrong attempt token wrote gate state")
		}
	}
}

func TestMatchingAttemptTokenWritesNothing(t *testing.T) {
	live := openHello(t)
	_, _, tokenB, _, _, attemptB := insertAttemptPair(t, live)
	beforeB := readAttemptCredential(t, live.pool, attemptB)
	before := readGateState(t, live.pool)
	for _, path := range attemptPaths(live.addr, attemptB) {
		header := make(http.Header)
		header.Set("Octoconv-Attempt", tokenB)
		status, hdr, body := doJSON(t, live.client, http.MethodPost, path, live.token, []byte(`{"protocol_version":1}`), header)
		assertProtocolError(t, status, hdr, body, http.StatusConflict, "attempt_not_active")
		if readAttemptCredential(t, live.pool, attemptB) != beforeB {
			t.Fatal("matching attempt token changed the credential")
		}
		if readGateState(t, live.pool) != before {
			t.Fatal("matching attempt token wrote a credential or a slot hold")
		}
	}
}

func TestMissingAttemptHeaderIsUnauthorized(t *testing.T) {
	live := openHello(t)
	_, _, _, _, _, attemptB := insertAttemptPair(t, live)
	beforeB := readAttemptCredential(t, live.pool, attemptB)
	path := attemptPaths(live.addr, attemptB)[0]
	status, hdr, body := doJSON(t, live.client, http.MethodPost, path, live.token, []byte(`{"protocol_version":1}`), nil)
	assertProtocolError(t, status, hdr, body, http.StatusUnauthorized, "unauthorized")
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(payload["code"]) == `"capability_denied"` {
		t.Fatal("missing attempt header was capability_denied")
	}
	if readAttemptCredential(t, live.pool, attemptB) != beforeB {
		t.Fatal("missing attempt header changed the credential")
	}
}

func TestPullIncompleteBodyWritesNothing(t *testing.T) {
	live := openHello(t)
	before := readGateState(t, live.pool)
	status, hdr, body := doJSON(t, live.client, http.MethodPost, live.sessionURL()+"/pull", live.token, []byte(`{"protocol_version":1}`), nil)
	assertProtocolError(t, status, hdr, body, http.StatusBadRequest, "invalid_message")
	if readGateState(t, live.pool) != before || before.attempts != 0 {
		t.Fatal("pull created an attempt row")
	}
}

func TestPullAssignsOneImageAttempt(t *testing.T) {
	live := openHello(t)
	ctx := context.Background()
	_, err := live.pool.Exec(ctx, `
		DELETE FROM jobs
		WHERE options @> '{"test":"TestPullAssignsOneImageAttempt"}'`)
	if err != nil {
		t.Fatalf("clear fixture jobs: %v", err)
	}
	jobID := uuid.New()
	_, err = live.pool.Exec(ctx, `
		INSERT INTO jobs (
			id, operation, engine, status, source_format, target_format,
			dispatch_owner, capability_id, capability_version, protocol_deadline, options
		) VALUES (
			$1, 'convert', 'image', 'queued', 'png', 'jpg',
			'protocol', 'octoconv.convert.image', 1, now() + interval '1 hour',
			'{"test":"TestPullAssignsOneImageAttempt"}'
		)`, jobID)
	if err != nil {
		t.Fatalf("insert job: %v", err)
	}
	t.Cleanup(func() {
		_, _ = live.pool.Exec(context.Background(), `DELETE FROM jobs WHERE id = $1`, jobID)
	})

	var stepBefore *uuid.UUID
	var deadline time.Time
	err = live.pool.QueryRow(ctx, `SELECT step_id, protocol_deadline FROM jobs WHERE id = $1`, jobID).Scan(&stepBefore, &deadline)
	if err != nil {
		t.Fatalf("read job: %v", err)
	}
	if stepBefore != nil {
		t.Fatal("step_id was set before the claim")
	}

	body := mustJSON(t, map[string]any{
		"protocol_version": 1,
		"pull_request_id":  uuid.New().String(),
		"free_slots":       1,
		"wait_seconds":     0,
	})
	status, hdr, resp := doJSON(t, live.client, http.MethodPost, live.sessionURL()+"/pull", live.token, body, nil)
	if status != http.StatusOK {
		t.Fatalf("pull status %d: %s", status, resp)
	}
	assertNoStoreJSON(t, hdr)
	assignment := decodeOneAssignment(t, resp)
	if _, ok := assignment["href"]; ok {
		t.Fatal("assignment has an href")
	}
	if string(assignment["inputs"]) != "[]" || string(assignment["outputs"]) != "[]" {
		t.Fatal("inputs or outputs are not empty arrays")
	}
	var limits struct {
		MaxOutputBytes int `json:"max_output_bytes"`
	}
	if err := json.Unmarshal(assignment["limits"], &limits); err != nil {
		t.Fatalf("limits: %v", err)
	}
	if limits.MaxOutputBytes != 100<<20 {
		t.Fatalf("max_output_bytes %d", limits.MaxOutputBytes)
	}
	token, err := jsonString(assignment["attempt_token"])
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 64 || token != strings.ToLower(token) {
		t.Fatalf("attempt_token %q", token)
	}
	if _, err := hex.DecodeString(token); err != nil {
		t.Fatalf("attempt_token is not hex: %v", err)
	}
	wantHash, err := HashToken(token)
	if err != nil {
		t.Fatalf("hash token: %v", err)
	}
	if wantHash == token {
		t.Fatal("token hash equals the wire token")
	}
	attemptID, err := jsonUUID(assignment["attempt_id"])
	if err != nil {
		t.Fatal(err)
	}
	stepID, err := jsonUUID(assignment["step_id"])
	if err != nil {
		t.Fatal(err)
	}
	jobGot, err := jsonUUID(assignment["job_id"])
	if err != nil {
		t.Fatal(err)
	}
	if jobGot != jobID {
		t.Fatalf("assigned job %s", jobGot)
	}
	cred := readAttemptCredential(t, live.pool, attemptID)
	if cred.tokenHash != wantHash {
		t.Fatal("credential hash is not HashToken of the wire token")
	}
	var hold struct {
		id       uuid.UUID
		ownerKey string
		state    string
		released *time.Time
		holdStep uuid.UUID
	}
	err = live.pool.QueryRow(ctx, `
		SELECT id, owner_key, state, released_at, step_id
		FROM worker_slot_holds WHERE id = $1`, attemptID).
		Scan(&hold.id, &hold.ownerKey, &hold.state, &hold.released, &hold.holdStep)
	if err != nil {
		t.Fatalf("read hold: %v", err)
	}
	if hold.id != attemptID || hold.state != "active" || hold.released != nil || hold.ownerKey != live.workerID.String() || hold.holdStep != stepID {
		t.Fatalf("hold %+v", hold)
	}
	var jobStatus, owner string
	var attempts int
	var progress *int16
	var storedStep uuid.UUID
	err = live.pool.QueryRow(ctx, `
		SELECT status, attempts, progress_pct, step_id, dispatch_owner
		FROM jobs WHERE id = $1`, jobID).Scan(&jobStatus, &attempts, &progress, &storedStep, &owner)
	if err != nil {
		t.Fatalf("read claimed job: %v", err)
	}
	if jobStatus != "active" || attempts != 0 || progress != nil || storedStep != stepID || owner != "protocol" {
		t.Fatalf("job status=%s attempts=%d progress=%v step=%s owner=%s", jobStatus, attempts, progress, storedStep, owner)
	}
	leaseAt, err := jsonTime(assignment["lease_expires_at"])
	if err != nil {
		t.Fatal(err)
	}
	attemptAt, err := jsonTime(assignment["attempt_deadline"])
	if err != nil {
		t.Fatal(err)
	}
	if leaseAt.After(attemptAt) || attemptAt.After(deadline) {
		t.Fatalf("lease %s attempt %s deadline %s", leaseAt, attemptAt, deadline)
	}

	second := mustJSON(t, map[string]any{
		"protocol_version": 1,
		"pull_request_id":  uuid.New().String(),
		"free_slots":       1,
		"wait_seconds":     0,
	})
	status, hdr, resp = doJSON(t, live.client, http.MethodPost, live.sessionURL()+"/pull", live.token, second, nil)
	if status != http.StatusOK {
		t.Fatalf("second pull status %d: %s", status, resp)
	}
	assertNoStoreJSON(t, hdr)
	var again struct {
		Assignments json.RawMessage `json:"assignments"`
	}
	if err := json.Unmarshal(resp, &again); err != nil {
		t.Fatalf("decode second pull: %v", err)
	}
	if string(again.Assignments) != "[]" {
		t.Fatalf("second pull assignments %s", again.Assignments)
	}
	var holds int
	err = live.pool.QueryRow(ctx, `SELECT count(*) FROM worker_slot_holds WHERE job_id = $1`, jobID).Scan(&holds)
	if err != nil {
		t.Fatalf("count holds: %v", err)
	}
	if holds != 1 {
		t.Fatalf("holds %d", holds)
	}
}

func decodeOneAssignment(t *testing.T, body []byte) map[string]json.RawMessage {
	t.Helper()
	var payload struct {
		Assignments []map[string]json.RawMessage `json:"assignments"`
		Control     struct {
			CancelAttemptIDs []string `json:"cancel_attempt_ids"`
			Drain            bool     `json:"drain"`
			Terminate        bool     `json:"terminate"`
		} `json:"control"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode pull: %v", err)
	}
	if len(payload.Assignments) != 1 {
		t.Fatalf("assignments %d", len(payload.Assignments))
	}
	if payload.Control.CancelAttemptIDs == nil || len(payload.Control.CancelAttemptIDs) != 0 || payload.Control.Drain || payload.Control.Terminate {
		t.Fatalf("control %+v", payload.Control)
	}
	return payload.Assignments[0]
}

func jsonString(raw json.RawMessage) (string, error) {
	var value string
	err := json.Unmarshal(raw, &value)
	return value, err
}

func jsonUUID(raw json.RawMessage) (uuid.UUID, error) {
	text, err := jsonString(raw)
	if err != nil {
		return uuid.Nil, err
	}
	return uuid.Parse(text)
}

func jsonTime(raw json.RawMessage) (time.Time, error) {
	text, err := jsonString(raw)
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339Nano, text)
}

func TestOversizedAttemptBodyWritesNothing(t *testing.T) {
	live := openHello(t)
	_, _, tokenB, _, _, attemptB := insertAttemptPair(t, live)
	beforeB := readAttemptCredential(t, live.pool, attemptB)
	header := make(http.Header)
	header.Set("Octoconv-Attempt", tokenB)
	body := bytes.Repeat([]byte("x"), maxMessageBytes+1)
	status, hdr, respBody := doJSON(t, live.client, http.MethodPost, attemptPaths(live.addr, attemptB)[0], live.token, body, header)
	assertProtocolError(t, status, hdr, respBody, http.StatusRequestEntityTooLarge, "message_too_large")
	if readAttemptCredential(t, live.pool, attemptB) != beforeB {
		t.Fatal("oversized attempt body changed the credential")
	}
}

func attemptPaths(addr string, attemptID uuid.UUID) []string {
	base := "https://" + addr + "/worker/v1/attempts/" + attemptID.String() + "/"
	return []string{base + "lease", base + "progress", base + "fail", base + "complete", base + "stopped"}
}

func insertAttemptPair(t *testing.T, live liveSession) (string, string, string, string, uuid.UUID, uuid.UUID) {
	t.Helper()
	tokenA, hashA, err := NewSessionToken()
	if err != nil {
		t.Fatalf("mint attempt: %v", err)
	}
	tokenB, hashB, err := NewSessionToken()
	if err != nil {
		t.Fatalf("mint attempt: %v", err)
	}
	attemptA := uuid.New()
	attemptB := uuid.New()
	insertAttemptCredential(t, live.pool, attemptA, live.sessionID, hashA, "octoconv.convert.image", 1)
	insertAttemptCredential(t, live.pool, attemptB, live.sessionID, hashB, "octoconv.convert.image", 1)
	return tokenA, hashA, tokenB, hashB, attemptA, attemptB
}

type attemptCredential struct {
	tokenHash string
	sessionID uuid.UUID
	capID     string
	capVer    int
}

func insertAttemptCredential(t *testing.T, pool *pgxpool.Pool, attemptID, sessionID uuid.UUID, tokenHash, capabilityID string, version int) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO worker_attempt_credentials (
			attempt_id, token_hash, session_id, capability_id, capability_version
		) VALUES ($1, $2, $3, $4, $5)`,
		attemptID, tokenHash, sessionID, capabilityID, version)
	if err != nil {
		t.Fatalf("insert attempt: %v", err)
	}
}

func readAttemptCredential(t *testing.T, pool *pgxpool.Pool, attemptID uuid.UUID) attemptCredential {
	t.Helper()
	var row attemptCredential
	err := pool.QueryRow(context.Background(), `
		SELECT token_hash, session_id, capability_id, capability_version
		FROM worker_attempt_credentials WHERE attempt_id = $1`, attemptID).
		Scan(&row.tokenHash, &row.sessionID, &row.capID, &row.capVer)
	if err != nil {
		t.Fatalf("read attempt: %v", err)
	}
	return row
}

type gateState struct {
	attempts int
	holds    int
}

func readGateState(t *testing.T, pool *pgxpool.Pool) gateState {
	t.Helper()
	var state gateState
	err := pool.QueryRow(context.Background(), `
		SELECT
			(SELECT count(*) FROM worker_attempt_credentials),
			(SELECT count(*) FROM worker_slot_holds)`).Scan(&state.attempts, &state.holds)
	if err != nil {
		t.Fatalf("read gate state: %v", err)
	}
	return state
}

func TestScenario01PullReplayDoesNotExtendLease(t *testing.T) {
	live := openHello(t)
	jobID := insertProtocolImageJob(t, live.pool)
	pullID := uuid.New()
	body := pullBody(pullID, 1, 0)
	status, hdr, first := doJSON(t, live.client, http.MethodPost, live.sessionURL()+"/pull", live.token, body, nil)
	if status != http.StatusOK {
		t.Fatalf("pull status %d: %s", status, first)
	}
	assertNoStoreJSON(t, hdr)
	var lease time.Time
	if err := live.pool.QueryRow(context.Background(), `
		SELECT lease_expires_at FROM worker_slot_holds WHERE job_id = $1`, jobID).Scan(&lease); err != nil {
		t.Fatalf("read lease: %v", err)
	}
	var backdated time.Time
	if err := live.pool.QueryRow(context.Background(), `
		UPDATE worker_slot_holds
		SET lease_expires_at = now() - interval '1 minute'
		WHERE job_id = $1
		RETURNING lease_expires_at`, jobID).Scan(&backdated); err != nil {
		t.Fatalf("backdate lease: %v", err)
	}
	status, hdr, second := doJSON(t, live.client, http.MethodPost, live.sessionURL()+"/pull", live.token, body, nil)
	if status != http.StatusOK {
		t.Fatalf("replay status %d: %s", status, second)
	}
	assertNoStoreJSON(t, hdr)
	if !bytes.Equal(first, second) {
		t.Fatalf("replay body differs\n%s\n%s", first, second)
	}
	var after time.Time
	if err := live.pool.QueryRow(context.Background(), `
		SELECT lease_expires_at FROM worker_slot_holds WHERE job_id = $1`, jobID).Scan(&after); err != nil {
		t.Fatalf("reread lease: %v", err)
	}
	if !after.Equal(backdated) {
		t.Fatalf("lease moved from %s to %s", backdated, after)
	}
	if holds := countHolds(t, live.pool, jobID); holds != 1 {
		t.Fatalf("holds %d", holds)
	}
	if !lease.After(backdated) {
		t.Fatal("lease was not moved into the past")
	}
}

func TestScenario02ConcurrentPullOneAttempt(t *testing.T) {
	live := openHello(t)
	jobID := insertProtocolImageJob(t, live.pool)
	body := pullBody(uuid.New(), 1, 0)
	var wg sync.WaitGroup
	start := make(chan struct{})
	type pullResult struct {
		status int
		body   []byte
		err    error
	}
	results := make([]pullResult, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i].status, results[i].body, results[i].err = postBody(live.client, http.MethodPost, live.sessionURL()+"/pull", live.token, body)
		}(i)
	}
	close(start)
	wg.Wait()
	for i, result := range results {
		if result.err != nil {
			t.Fatalf("pull %d: %v", i, result.err)
		}
		if result.status != http.StatusOK {
			t.Fatalf("pull %d status %d: %s", i, result.status, result.body)
		}
		if !bytes.Equal(result.body, results[0].body) {
			t.Fatalf("pull %d body differs", i)
		}
	}
	if holds := countHolds(t, live.pool, jobID); holds != 1 {
		t.Fatalf("holds %d", holds)
	}
	var creds int
	if err := live.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM worker_attempt_credentials WHERE session_id = $1`, live.sessionID).Scan(&creds); err != nil {
		t.Fatalf("count credentials: %v", err)
	}
	if creds != 1 {
		t.Fatalf("credentials %d", creds)
	}
}

func TestScenario03PullConflict(t *testing.T) {
	live := openHello(t)
	jobID := insertProtocolImageJob(t, live.pool)
	pullID := uuid.New()
	firstBody := pullBody(pullID, 1, 0)
	status, _, first := doJSON(t, live.client, http.MethodPost, live.sessionURL()+"/pull", live.token, firstBody, nil)
	if status != http.StatusOK {
		t.Fatalf("pull status %d: %s", status, first)
	}
	var stored []byte
	if err := live.pool.QueryRow(context.Background(), `
		SELECT response_body FROM worker_request_dedup
		WHERE session_id = $1 AND dedup_key = $2`, live.sessionID, pullID.String()).Scan(&stored); err != nil {
		t.Fatalf("read stored pull: %v", err)
	}
	secondBody := []byte(strings.Replace(string(firstBody), "{", "{ ", 1))
	status, hdr, resp := doJSON(t, live.client, http.MethodPost, live.sessionURL()+"/pull", live.token, secondBody, nil)
	assertProtocolError(t, status, hdr, resp, http.StatusConflict, "pull_conflict")
	var message string
	if err := json.Unmarshal(resp, &struct {
		Message *string `json:"message"`
	}{&message}); err != nil {
		t.Fatalf("decode conflict: %v", err)
	}
	if message == "invalid message" {
		t.Fatalf("conflict message %q", message)
	}
	var after []byte
	if err := live.pool.QueryRow(context.Background(), `
		SELECT response_body FROM worker_request_dedup
		WHERE session_id = $1 AND dedup_key = $2`, live.sessionID, pullID.String()).Scan(&after); err != nil {
		t.Fatalf("reread stored pull: %v", err)
	}
	if !bytes.Equal(stored, after) {
		t.Fatal("conflict replaced the stored body")
	}
	if holds := countHolds(t, live.pool, jobID); holds != 1 {
		t.Fatalf("holds %d", holds)
	}
}

func TestScenario04EmptyPullReplay(t *testing.T) {
	live := openHello(t)
	deletePullFixtures(t, live.pool)
	pullID := uuid.New()
	body := pullBody(pullID, 1, 0)
	status, hdr, first := doJSON(t, live.client, http.MethodPost, live.sessionURL()+"/pull", live.token, body, nil)
	if status != http.StatusOK {
		t.Fatalf("pull status %d: %s", status, first)
	}
	assertNoStoreJSON(t, hdr)
	assertJSONEqual(t, first, readPositiveFixture(t, "pull-empty.json"))
	status, _, second := doJSON(t, live.client, http.MethodPost, live.sessionURL()+"/pull", live.token, body, nil)
	if status != http.StatusOK {
		t.Fatalf("replay status %d: %s", status, second)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("empty replay differs")
	}
	var holds int
	if err := live.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM worker_slot_holds WHERE session_id = $1`, live.sessionID).Scan(&holds); err != nil {
		t.Fatalf("count holds: %v", err)
	}
	if holds != 0 {
		t.Fatalf("holds %d", holds)
	}
}

func TestScenario05PullAfterClose(t *testing.T) {
	live := openHello(t)
	deletePullFixtures(t, live.pool)
	body := pullBody(uuid.New(), 1, 0)
	status, _, resp := doJSON(t, live.client, http.MethodPost, live.sessionURL()+"/pull", live.token, body, nil)
	if status != http.StatusOK {
		t.Fatalf("pull status %d: %s", status, resp)
	}
	status, _, resp = doJSON(t, live.client, http.MethodDelete, live.sessionURL(), live.token, []byte(`{"protocol_version":1}`), nil)
	if status != http.StatusOK {
		t.Fatalf("delete status %d: %s", status, resp)
	}
	var dedup int
	if err := live.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM worker_request_dedup WHERE session_id = $1`, live.sessionID).Scan(&dedup); err != nil {
		t.Fatalf("count dedup: %v", err)
	}
	if dedup != 0 {
		t.Fatalf("dedup rows %d", dedup)
	}
	var hdr http.Header
	status, hdr, resp = doJSON(t, live.client, http.MethodPost, live.sessionURL()+"/pull", live.token, body, nil)
	assertProtocolError(t, status, hdr, resp, http.StatusUnauthorized, "session_closed")
	var creds int
	if err := live.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM worker_attempt_credentials WHERE session_id = $1`, live.sessionID).Scan(&creds); err != nil {
		t.Fatalf("count credentials: %v", err)
	}
	if creds != 0 {
		t.Fatalf("credentials %d", creds)
	}
}

func TestScenario17PullUnknownField(t *testing.T) {
	live := openHello(t)
	deletePullFixtures(t, live.pool)
	unknown := []byte(`{"protocol_version":1,"pull_request_id":"` + uuid.New().String() + `","free_slots":1,"wait_seconds":0,"priority":1}`)
	status, hdr, resp := doJSON(t, live.client, http.MethodPost, live.sessionURL()+"/pull", live.token, unknown, nil)
	assertProtocolError(t, status, hdr, resp, http.StatusBadRequest, "unknown_field")
	withExt := []byte(`{"protocol_version":1,"pull_request_id":"` + uuid.New().String() + `","free_slots":1,"wait_seconds":0,"extensions":{"priority":1}}`)
	without := pullBody(uuid.New(), 1, 0)
	status, _, extended := doJSON(t, live.client, http.MethodPost, live.sessionURL()+"/pull", live.token, withExt, nil)
	if status != http.StatusOK {
		t.Fatalf("extensions pull status %d: %s", status, extended)
	}
	status, _, plain := doJSON(t, live.client, http.MethodPost, live.sessionURL()+"/pull", live.token, without, nil)
	if status != http.StatusOK {
		t.Fatalf("plain pull status %d: %s", status, plain)
	}
	if !bytes.Equal(extended, plain) {
		t.Fatalf("extensions changed the pull\n%s\n%s", extended, plain)
	}
	var holds int
	if err := live.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM worker_slot_holds WHERE session_id = $1`, live.sessionID).Scan(&holds); err != nil {
		t.Fatalf("count holds: %v", err)
	}
	if holds != 0 {
		t.Fatalf("holds %d", holds)
	}
}

func TestPullFreeSlotsZeroStoresEmpty(t *testing.T) {
	live := openHello(t)
	jobID := insertPullJob(t, live.pool, pullJob{})
	pullID := uuid.New()
	body := pullBody(pullID, 0, 0)
	status, hdr, resp := doJSON(t, live.client, http.MethodPost, live.sessionURL()+"/pull", live.token, body, nil)
	if status != http.StatusOK {
		t.Fatalf("pull status %d: %s", status, resp)
	}
	assertNoStoreJSON(t, hdr)
	assertEmptyPull(t, resp)
	if readGateState(t, live.pool) != (gateState{}) {
		t.Fatal("free_slots 0 inserted an attempt")
	}
	if jobStatusOf(t, live.pool, jobID) != "queued" {
		t.Fatal("job left queued")
	}
	status, hdr, again := doJSON(t, live.client, http.MethodPost, live.sessionURL()+"/pull", live.token, body, nil)
	if status != http.StatusOK {
		t.Fatalf("replay status %d: %s", status, again)
	}
	assertNoStoreJSON(t, hdr)
	if !bytes.Equal(resp, again) {
		t.Fatalf("replay changed the empty pull\n%s\n%s", resp, again)
	}
	if readGateState(t, live.pool) != (gateState{}) || jobStatusOf(t, live.pool, jobID) != "queued" {
		t.Fatal("replay stored an attempt")
	}
}

func TestPullOwnerCapStoresEmpty(t *testing.T) {
	live := openHello(t)
	var slots int
	if err := live.pool.QueryRow(context.Background(), `
		SELECT max_slots FROM worker_identities WHERE worker_id = $1`, live.workerID).Scan(&slots); err != nil {
		t.Fatalf("read max_slots: %v", err)
	}
	if slots != 4 {
		t.Fatalf("HELLO free_slots changed max_slots to %d", slots)
	}
	if _, err := live.pool.Exec(context.Background(), `
		UPDATE worker_identities SET max_slots = 1 WHERE worker_id = $1`, live.workerID); err != nil {
		t.Fatalf("set max_slots: %v", err)
	}
	jobID := insertPullJob(t, live.pool, pullJob{})
	holdID := insertSlotHold(t, live.pool, slotHold{owner: live.workerID.String(), sessionID: live.sessionID})
	status, resp := pullOnce(t, live, 4)
	if status != http.StatusOK {
		t.Fatalf("pull status %d: %s", status, resp)
	}
	assertEmptyPull(t, resp)
	if jobStatusOf(t, live.pool, jobID) != "queued" || countHolds(t, live.pool, jobID) != 0 {
		t.Fatal("full owner cap claimed the job")
	}
	if unreleasedHolds(t, live.pool, live.workerID.String()) != 1 {
		t.Fatal("owner cap inserted a second hold")
	}
	if _, err := live.pool.Exec(context.Background(), `
		UPDATE worker_slot_holds SET owner_key = $2 WHERE id = $1`, holdID, uuid.New().String()); err != nil {
		t.Fatalf("move hold: %v", err)
	}
	status, resp = pullOnce(t, live, 4)
	if status != http.StatusOK {
		t.Fatalf("second pull status %d: %s", status, resp)
	}
	got, ok := pullAssignmentID(t, resp)
	if !ok || got != jobID {
		t.Fatalf("assignment %s", resp)
	}
}

func TestPullTwoSessionsShareOwnerCap(t *testing.T) {
	live := openHello(t)
	other := helloAgain(t, live)
	if _, err := live.pool.Exec(context.Background(), `
		UPDATE worker_identities SET max_slots = 1 WHERE worker_id = $1`, live.workerID); err != nil {
		t.Fatalf("set max_slots: %v", err)
	}
	older := insertPullJob(t, live.pool, pullJob{createdAt: time.Now().Add(-time.Minute)})
	younger := insertPullJob(t, live.pool, pullJob{})
	assertOneAssignment(t, live.pool, []uuid.UUID{older, younger}, pullTogether([]liveSession{live, other}, 4))
}

func TestPullSkipsClientAtMaxConcurrent(t *testing.T) {
	live := openHello(t)
	capped := insertClient(t, live.pool)
	other := insertClient(t, live.pool)
	setMaxConcurrent(t, live.pool, capped, 1)
	insertPullJob(t, live.pool, pullJob{clientID: &capped, status: "active", owner: "legacy"})
	older := insertPullJob(t, live.pool, pullJob{clientID: &capped, createdAt: time.Now().Add(-time.Minute)})
	newer := insertPullJob(t, live.pool, pullJob{clientID: &other})
	status, resp := pullOnce(t, live, 1)
	if status != http.StatusOK {
		t.Fatalf("pull status %d: %s", status, resp)
	}
	got, ok := pullAssignmentID(t, resp)
	if !ok || got != newer {
		t.Fatalf("assignment %s", resp)
	}
	if jobStatusOf(t, live.pool, older) != "queued" || jobFailed(t, live.pool, older) {
		t.Fatal("over-cap job was claimed or failed")
	}
}

func TestPullDrainsQueuedPairUnderClientCap(t *testing.T) {
	live := openHello(t)
	clientID := insertClient(t, live.pool)
	setMaxConcurrent(t, live.pool, clientID, 1)
	older := insertPullJob(t, live.pool, pullJob{clientID: &clientID, createdAt: time.Now().Add(-time.Minute)})
	younger := insertPullJob(t, live.pool, pullJob{clientID: &clientID})
	status, resp := pullOnce(t, live, 1)
	if status != http.StatusOK {
		t.Fatalf("first pull status %d: %s", status, resp)
	}
	got, ok := pullAssignmentID(t, resp)
	if !ok || got != older {
		t.Fatalf("first assignment %s", resp)
	}
	if jobStatusOf(t, live.pool, younger) != "queued" {
		t.Fatal("younger job was claimed with the older one")
	}
	status, resp = pullOnce(t, live, 1)
	if status != http.StatusOK {
		t.Fatalf("second pull status %d: %s", status, resp)
	}
	assertEmptyPull(t, resp)
	if jobStatusOf(t, live.pool, younger) != "queued" || jobFailed(t, live.pool, younger) {
		t.Fatal("second pull failed the younger job")
	}
	if _, err := live.pool.Exec(context.Background(), `UPDATE jobs SET status = 'done' WHERE id = $1`, older); err != nil {
		t.Fatalf("finish older: %v", err)
	}
	time.Sleep(1100 * time.Millisecond)
	status, resp = pullOnce(t, live, 1)
	if status != http.StatusOK {
		t.Fatalf("third pull status %d: %s", status, resp)
	}
	got, ok = pullAssignmentID(t, resp)
	if !ok || got != younger {
		t.Fatalf("third assignment %s", resp)
	}
	if usageRows(t, live.pool, clientID) != 0 {
		t.Fatal("pull inserted client_usage_daily")
	}
}

func TestPullTwoWorkersShareClientCap(t *testing.T) {
	// worker_capabilities is unique on (id, version) and formats are unique per pair,
	// so two approved image workers cannot both own octoconv.convert.image v1.
	ln := startWorkerListener(t)
	first := ln.hello(t, imageLegacyManifest(t, "octoconv.convert.image", "png", "jpg"))
	second := ln.hello(t, imageLegacyManifest(t, "octoconv.convert.imageb", "jpg", "png"))
	clientID := insertClient(t, ln.pool)
	setMaxConcurrent(t, ln.pool, clientID, 1)
	older := insertPullJob(t, ln.pool, pullJob{
		clientID:   &clientID,
		capability: "octoconv.convert.image",
		createdAt:  time.Now().Add(-time.Minute),
	})
	younger := insertPullJob(t, ln.pool, pullJob{
		clientID:   &clientID,
		capability: "octoconv.convert.imageb",
	})
	assertOneAssignment(t, ln.pool, []uuid.UUID{older, younger}, pullTogether([]liveSession{first, second}, 1))
}

func TestPullRateLimitStoresNoAttempt(t *testing.T) {
	live := openHello(t)
	jobID := insertPullJob(t, live.pool, pullJob{})
	third := uuid.New()
	for i := 0; i < 2; i++ {
		status, _, resp := doJSON(t, live.client, http.MethodPost, live.sessionURL()+"/pull", live.token, pullBody(uuid.New(), 0, 0), nil)
		if status != http.StatusOK {
			t.Fatalf("pull %d status %d: %s", i, status, resp)
		}
		assertEmptyPull(t, resp)
	}
	status, hdr, resp := doJSON(t, live.client, http.MethodPost, live.sessionURL()+"/pull", live.token, pullBody(third, 0, 0), nil)
	assertRateLimited(t, status, hdr, resp)
	var dedup int
	if err := live.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM worker_request_dedup WHERE session_id = $1 AND dedup_key = $2`,
		live.sessionID, third.String()).Scan(&dedup); err != nil {
		t.Fatalf("count dedup: %v", err)
	}
	if dedup != 0 || readGateState(t, live.pool) != (gateState{}) || jobStatusOf(t, live.pool, jobID) != "queued" {
		t.Fatal("rate limit stored a dedup row or an attempt")
	}
}

func TestPullDoesNotClaimLegacyOrImagepdf(t *testing.T) {
	ln := startWorkerListener(t)
	image := ln.hello(t, readPositiveFixture(t, "manifest-image.json"))
	imagepdf := ln.hello(t, readPositiveFixture(t, "manifest-imagepdf.json"))
	legacyID := insertPullJob(t, ln.pool, pullJob{owner: "legacy"})
	pdfID := insertPullJob(t, ln.pool, pullJob{capability: "octoconv.convert.imagepdf"})
	status, resp := pullOnce(t, image, 1)
	if status != http.StatusOK {
		t.Fatalf("image pull status %d: %s", status, resp)
	}
	assertEmptyPull(t, resp)
	if strings.Contains(string(resp), legacyID.String()) || strings.Contains(string(resp), pdfID.String()) {
		t.Fatalf("image pull returned %s", resp)
	}
	status, resp = pullOnce(t, imagepdf, 1)
	if status != http.StatusOK {
		t.Fatalf("imagepdf pull status %d: %s", status, resp)
	}
	assertEmptyPull(t, resp)
	if jobStatusOf(t, ln.pool, legacyID) != "queued" || dispatchOwner(t, ln.pool, legacyID) != "legacy" {
		t.Fatal("legacy job changed")
	}
	if jobStatusOf(t, ln.pool, pdfID) != "queued" || dispatchOwner(t, ln.pool, pdfID) != "protocol" {
		t.Fatal("imagepdf job changed")
	}
}

func TestPullDoesNotWriteUsage(t *testing.T) {
	live := openHello(t)
	clientID := insertClient(t, live.pool)
	if _, err := live.pool.Exec(context.Background(), `
		INSERT INTO client_usage_daily (client_id, engine, day, status, job_count)
		VALUES ($1, 'image', CURRENT_DATE, 'done', 3)`, clientID); err != nil {
		t.Fatalf("insert usage: %v", err)
	}
	jobID := insertPullJob(t, live.pool, pullJob{clientID: &clientID})
	body := pullBody(uuid.New(), 1, 0)
	status, _, resp := doJSON(t, live.client, http.MethodPost, live.sessionURL()+"/pull", live.token, body, nil)
	if status != http.StatusOK {
		t.Fatalf("pull status %d: %s", status, resp)
	}
	got, ok := pullAssignmentID(t, resp)
	if !ok || got != jobID {
		t.Fatalf("assignment %s", resp)
	}
	if rows, jobs := usageSnapshot(t, live.pool, clientID); rows != 1 || jobs != 3 {
		t.Fatalf("claim wrote usage rows=%d jobs=%d", rows, jobs)
	}
	status, _, again := doJSON(t, live.client, http.MethodPost, live.sessionURL()+"/pull", live.token, body, nil)
	if status != http.StatusOK || !bytes.Equal(resp, again) {
		t.Fatalf("replay status %d: %s", status, again)
	}
	if rows, jobs := usageSnapshot(t, live.pool, clientID); rows != 1 || jobs != 3 {
		t.Fatalf("replay wrote usage rows=%d jobs=%d", rows, jobs)
	}
}

func TestPullFiveFailedHoldsStayQueued(t *testing.T) {
	live := openHello(t)
	failedStep := uuid.New()
	stoppedStep := uuid.New()
	failedJob := insertPullJob(t, live.pool, pullJob{stepID: &failedStep, createdAt: time.Now().Add(-time.Minute)})
	stoppedJob := insertPullJob(t, live.pool, pullJob{stepID: &stoppedStep})
	for i := 0; i < 5; i++ {
		insertSlotHold(t, live.pool, slotHold{
			owner: uuid.New().String(), sessionID: live.sessionID, jobID: &failedJob, stepID: &failedStep, state: "failed", released: true,
		})
		insertSlotHold(t, live.pool, slotHold{
			owner: uuid.New().String(), sessionID: live.sessionID, jobID: &stoppedJob, stepID: &stoppedStep, state: "stopped", released: true,
		})
	}
	status, resp := pullOnce(t, live, 1)
	if status != http.StatusOK {
		t.Fatalf("pull status %d: %s", status, resp)
	}
	got, ok := pullAssignmentID(t, resp)
	if !ok || got != stoppedJob {
		t.Fatalf("assignment %s", resp)
	}
	if jobStatusOf(t, live.pool, failedJob) != "queued" {
		t.Fatal("five failed holds were claimed")
	}
}

func pullBody(id uuid.UUID, freeSlots, waitSeconds int) []byte {
	return []byte(`{"protocol_version":1,"pull_request_id":"` + id.String() + `","free_slots":` + strconv.Itoa(freeSlots) + `,"wait_seconds":` + strconv.Itoa(waitSeconds) + `}`)
}

func insertProtocolImageJob(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	deletePullFixtures(t, pool)
	jobID := uuid.New()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO jobs (
			id, operation, engine, status, source_format, target_format,
			dispatch_owner, capability_id, capability_version, protocol_deadline, options
		) VALUES (
			$1, 'convert', 'image', 'queued', 'png', 'jpg',
			'protocol', 'octoconv.convert.image', 1, now() + interval '1 hour',
			'{"test":"protocol-pull"}'
		)`, jobID)
	if err != nil {
		t.Fatalf("insert job: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM jobs WHERE id = $1`, jobID)
	})
	return jobID
}

func deletePullFixtures(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		DELETE FROM jobs
		WHERE options @> '{"test":"protocol-pull"}'
		   OR options @> '{"test":"TestPullAssignsOneImageAttempt"}'`)
	if err != nil {
		t.Fatalf("clear fixture jobs: %v", err)
	}
}

func countHolds(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM worker_slot_holds WHERE job_id = $1`, jobID).Scan(&n); err != nil {
		t.Fatalf("count holds: %v", err)
	}
	return n
}

func assertJSONEqual(t *testing.T, body, fixture []byte) {
	t.Helper()
	var got, want any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if err := json.Unmarshal(fixture, &want); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("json mismatch\n%s\n%s", body, fixture)
	}
}

func postBody(client *http.Client, method, rawURL, token string, body []byte) (int, []byte, error) {
	req, err := http.NewRequest(method, rawURL, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, respBody, nil
}

type workerListener struct {
	pool  *pgxpool.Pool
	repo  *Repo
	ca    *x509.Certificate
	caKey *ecdsa.PrivateKey
	addr  string
}

func startWorkerListener(t *testing.T) workerListener {
	t.Helper()
	pool := testPool(t)
	resetWorkerState(t, pool)
	repo := NewRepo(pool)
	ca, caKey := mintTestCA(t)
	return workerListener{
		pool: pool, repo: repo, ca: ca, caKey: caKey,
		addr: listenWorker(t, repo, ca, caKey),
	}
}

func (l workerListener) hello(t *testing.T, manifest []byte) liveSession {
	t.Helper()
	workerID := uuid.New()
	if err := l.repo.Approve(context.Background(), workerID, manifest); err != nil {
		t.Fatalf("approve: %v", err)
	}
	client := workerHTTPClient(t, l.ca, mintClientCert(t, l.ca, l.caKey, workerID))
	status, _, resp := doJSON(t, client, http.MethodPost, "https://"+l.addr+"/worker/v1/sessions", "", helloBody(workerID, manifestDigest(manifest), 1), nil)
	if status != http.StatusOK {
		t.Fatalf("HELLO status %d: %s", status, resp)
	}
	sessionID, token := decodeHello(t, resp)
	return liveSession{
		pool: l.pool, workerID: workerID, sessionID: sessionID,
		token: token, addr: l.addr, client: client, manifest: manifest,
	}
}

func helloAgain(t *testing.T, live liveSession) liveSession {
	t.Helper()
	status, _, resp := doJSON(t, live.client, http.MethodPost, "https://"+live.addr+"/worker/v1/sessions", "", helloBody(live.workerID, manifestDigest(live.manifest), 1), nil)
	if status != http.StatusOK {
		t.Fatalf("HELLO status %d: %s", status, resp)
	}
	sessionID, token := decodeHello(t, resp)
	next := live
	next.sessionID = sessionID
	next.token = token
	return next
}

func imageLegacyManifest(t *testing.T, id, from, to string) []byte {
	t.Helper()
	return mustJSON(t, map[string]any{
		"capabilities": []any{
			map[string]any{
				"id":               id,
				"version":          1,
				"legacy":           map[string]any{"operation": "convert", "engine": "image"},
				"client_operation": "convert",
				"inputs":           []any{map[string]any{"role": "source", "min": 1, "max": 1}},
				"outputs":          []any{map[string]any{"role": "primary", "min": 1, "max": 1}},
				"parameter_schema": map[string]any{},
				"formats":          []any{map[string]any{"from": from, "to": to}},
			},
		},
	})
}

type pullJob struct {
	clientID   *uuid.UUID
	capability string
	status     string
	owner      string
	stepID     *uuid.UUID
	createdAt  time.Time
}

func insertPullJob(t *testing.T, pool *pgxpool.Pool, job pullJob) uuid.UUID {
	t.Helper()
	if job.status == "" {
		job.status = "queued"
	}
	if job.owner == "" {
		job.owner = "protocol"
	}
	if job.createdAt.IsZero() {
		job.createdAt = time.Now()
	}
	var capability any
	var version any
	if job.owner == "protocol" {
		if job.capability == "" {
			job.capability = "octoconv.convert.image"
		}
		capability = job.capability
		version = 1
	}
	id := uuid.New()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO jobs (
			id, client_id, operation, engine, status, source_format, target_format,
			dispatch_owner, capability_id, capability_version, protocol_deadline,
			step_id, options, created_at
		) VALUES (
			$1, $2, 'convert', 'image', $3, 'png', 'jpg',
			$4, $5, $6, CASE WHEN $4 = 'protocol' THEN now() + interval '1 hour' ELSE NULL END,
			$7, '{"test":"protocol-pull"}', $8
		)`, id, job.clientID, job.status, job.owner, capability, version, job.stepID, job.createdAt)
	if err != nil {
		t.Fatalf("insert job: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM worker_slot_holds WHERE job_id = $1`, id)
		_, _ = pool.Exec(context.Background(), `DELETE FROM jobs WHERE id = $1`, id)
	})
	return id
}

func insertClient(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(context.Background(), `INSERT INTO clients (id, name) VALUES ($1, $2)`, id, "pull-cap-"+id.String()); err != nil {
		t.Fatalf("insert client: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM jobs WHERE client_id = $1`, id)
		_, _ = pool.Exec(context.Background(), `DELETE FROM client_usage_daily WHERE client_id = $1`, id)
		_, _ = pool.Exec(context.Background(), `DELETE FROM client_limits WHERE client_id = $1`, id)
		_, _ = pool.Exec(context.Background(), `DELETE FROM clients WHERE id = $1`, id)
	})
	return id
}

func setMaxConcurrent(t *testing.T, pool *pgxpool.Pool, clientID uuid.UUID, n int) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO client_limits (client_id, engine, max_concurrent)
		VALUES ($1, 'image', $2)`, clientID, n)
	if err != nil {
		t.Fatalf("insert client limit: %v", err)
	}
}

type slotHold struct {
	owner     string
	sessionID uuid.UUID
	jobID     *uuid.UUID
	stepID    *uuid.UUID
	state     string
	released  bool
}

func insertSlotHold(t *testing.T, pool *pgxpool.Pool, hold slotHold) uuid.UUID {
	t.Helper()
	id := uuid.New()
	var releasedAt any
	if hold.released {
		releasedAt = time.Now()
	}
	var state any
	if hold.state != "" {
		state = hold.state
	}
	_, err := pool.Exec(context.Background(), `
		INSERT INTO worker_slot_holds (
			id, owner_key, session_id, job_id, step_id, state, released_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		id, hold.owner, hold.sessionID, hold.jobID, hold.stepID, state, releasedAt)
	if err != nil {
		t.Fatalf("insert hold: %v", err)
	}
	return id
}

func pullOnce(t *testing.T, live liveSession, freeSlots int) (int, []byte) {
	t.Helper()
	status, _, resp := doJSON(t, live.client, http.MethodPost, live.sessionURL()+"/pull", live.token, pullBody(uuid.New(), freeSlots, 0), nil)
	return status, resp
}

type pullResp struct {
	status int
	body   []byte
	err    error
}

func pullTogether(sessions []liveSession, freeSlots int) []pullResp {
	out := make([]pullResp, len(sessions))
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range sessions {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			s := sessions[i]
			out[i].status, out[i].body, out[i].err = postBody(s.client, http.MethodPost, s.sessionURL()+"/pull", s.token, pullBody(uuid.New(), freeSlots, 0))
		}(i)
	}
	close(start)
	wg.Wait()
	return out
}

func assertOneAssignment(t *testing.T, pool *pgxpool.Pool, jobIDs []uuid.UUID, results []pullResp) {
	t.Helper()
	assigned := 0
	empty := 0
	for _, result := range results {
		if result.err != nil {
			t.Fatalf("pull: %v", result.err)
		}
		if result.status != http.StatusOK {
			t.Fatalf("pull status %d: %s", result.status, result.body)
		}
		if _, ok := pullAssignmentID(t, result.body); ok {
			assigned++
			continue
		}
		assertEmptyPull(t, result.body)
		empty++
	}
	if assigned != 1 || empty != 1 {
		t.Fatalf("assigned %d empty %d", assigned, empty)
	}
	if countJobStatus(t, pool, jobIDs, "queued") != 1 || countJobStatus(t, pool, jobIDs, "active") != 1 {
		t.Fatal("jobs were not one queued and one active")
	}
	var holds int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM worker_slot_holds
		WHERE job_id = ANY($1) AND released_at IS NULL`, jobIDs).Scan(&holds); err != nil {
		t.Fatalf("count holds: %v", err)
	}
	if holds != 1 {
		t.Fatalf("unreleased holds %d", holds)
	}
}

func assertEmptyPull(t *testing.T, body []byte) {
	t.Helper()
	var payload struct {
		ProtocolVersion int               `json:"protocol_version"`
		Assignments     []json.RawMessage `json:"assignments"`
		Control         struct {
			CancelAttemptIDs []string `json:"cancel_attempt_ids"`
			Drain            bool     `json:"drain"`
			Terminate        bool     `json:"terminate"`
		} `json:"control"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode pull: %v", err)
	}
	if payload.ProtocolVersion != 1 || len(payload.Assignments) != 0 {
		t.Fatalf("pull %s", body)
	}
	if payload.Control.CancelAttemptIDs == nil || len(payload.Control.CancelAttemptIDs) != 0 || payload.Control.Drain || payload.Control.Terminate {
		t.Fatalf("control %+v", payload.Control)
	}
}

func pullAssignmentID(t *testing.T, body []byte) (uuid.UUID, bool) {
	t.Helper()
	var payload struct {
		Assignments []map[string]json.RawMessage `json:"assignments"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode pull: %v", err)
	}
	if len(payload.Assignments) == 0 {
		return uuid.Nil, false
	}
	if len(payload.Assignments) != 1 {
		t.Fatalf("assignments %d: %s", len(payload.Assignments), body)
	}
	id, err := jsonUUID(payload.Assignments[0]["job_id"])
	if err != nil {
		t.Fatal(err)
	}
	return id, true
}

func assertRateLimited(t *testing.T, status int, hdr http.Header, body []byte) {
	t.Helper()
	if status != http.StatusTooManyRequests {
		t.Fatalf("status %d: %s", status, body)
	}
	assertNoStoreJSON(t, hdr)
	var got map[string]json.RawMessage
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if string(got["code"]) != `"rate_limited"` || string(got["retryable"]) != "true" {
		t.Fatalf("body %s", body)
	}
	if _, ok := got["error"]; ok {
		t.Fatal("response has an error field")
	}
}

func jobStatusOf(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) string {
	t.Helper()
	var status string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM jobs WHERE id = $1`, id).Scan(&status); err != nil {
		t.Fatalf("read job: %v", err)
	}
	return status
}

func jobFailed(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) bool {
	t.Helper()
	var code *string
	if err := pool.QueryRow(context.Background(), `SELECT error_code FROM jobs WHERE id = $1`, id).Scan(&code); err != nil {
		t.Fatalf("read job: %v", err)
	}
	return code != nil
}

func dispatchOwner(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) string {
	t.Helper()
	var owner string
	if err := pool.QueryRow(context.Background(), `SELECT dispatch_owner FROM jobs WHERE id = $1`, id).Scan(&owner); err != nil {
		t.Fatalf("read job: %v", err)
	}
	return owner
}

func countJobStatus(t *testing.T, pool *pgxpool.Pool, ids []uuid.UUID, status string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM jobs WHERE id = ANY($1) AND status = $2`, ids, status).Scan(&n); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	return n
}

func unreleasedHolds(t *testing.T, pool *pgxpool.Pool, owner string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM worker_slot_holds WHERE owner_key = $1 AND released_at IS NULL`, owner).Scan(&n); err != nil {
		t.Fatalf("count holds: %v", err)
	}
	return n
}

func usageRows(t *testing.T, pool *pgxpool.Pool, clientID uuid.UUID) int {
	t.Helper()
	rows, _ := usageSnapshot(t, pool, clientID)
	return rows
}

func usageSnapshot(t *testing.T, pool *pgxpool.Pool, clientID uuid.UUID) (int, int) {
	t.Helper()
	var rows int
	var jobs int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*)::int, COALESCE(sum(job_count), 0)::int
		FROM client_usage_daily WHERE client_id = $1`, clientID).Scan(&rows, &jobs); err != nil {
		t.Fatalf("read usage: %v", err)
	}
	return rows, jobs
}
