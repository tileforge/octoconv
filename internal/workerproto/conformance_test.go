package workerproto

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
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
