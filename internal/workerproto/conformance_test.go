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
	"strings"
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

func TestPullRouteIsUnregistered(t *testing.T) {
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
