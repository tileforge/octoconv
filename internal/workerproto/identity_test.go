package workerproto

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
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tileforge/octoconv/internal/db"
)

func TestSessionTokenIsHashed(t *testing.T) {
	wire1, hash1, err := NewSessionToken()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	wire2, hash2, err := NewSessionToken()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if wire1 == wire2 || hash1 == hash2 {
		t.Fatal("two mints returned the same token")
	}
	assertSessionToken(t, wire1, hash1)
	assertSessionToken(t, wire2, hash2)

	if _, err := HashToken(strings.Repeat("A", 64)); err == nil || !strings.Contains(err.Error(), "bad session token") || strings.Contains(err.Error(), strings.Repeat("A", 64)) {
		t.Fatal("uppercase token must be rejected as bad session token")
	}
	bad := "not-a-token"
	if _, err := HashToken(bad); err == nil || err.Error() != "bad session token" || strings.Contains(err.Error(), bad) {
		t.Fatal("bad session token error must not include token characters")
	}
}

func assertSessionToken(t *testing.T, wire, stored string) {
	t.Helper()
	if !isHex64(wire) {
		t.Fatal("wire token is not 64 lowercase hex")
	}
	if !isHex64(stored) {
		t.Fatal("stored token is not 64 lowercase hex")
	}
	if stored == wire {
		t.Fatal("stored value equals the wire token")
	}
	raw, err := hex.DecodeString(wire)
	if err != nil || len(raw) != 32 {
		t.Fatal("wire token did not decode to 32 bytes")
	}
	sum := sha256.Sum256(raw)
	if stored != hex.EncodeToString(sum[:]) {
		t.Fatal("stored value is not sha256 of the raw token bytes")
	}
	textSum := sha256.Sum256([]byte(wire))
	if stored == hex.EncodeToString(textSum[:]) {
		t.Fatal("stored value is sha256 of the hex text")
	}
	got, err := HashToken(wire)
	if err != nil {
		t.Fatalf("HashToken: %v", err)
	}
	if got != stored {
		t.Fatal("HashToken does not match the minted hash")
	}
}

func TestListenerTLSProfile(t *testing.T) {
	pool := x509.NewCertPool()
	cfg := ListenerConfig(pool, nil)
	if cfg.MinVersion != tls.VersionTLS13 || cfg.MaxVersion != tls.VersionTLS13 {
		t.Fatal("listener is not pinned to TLS 1.3")
	}
	if cfg.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Fatal("client certificate is not required")
	}
	if cfg.ClientCAs == nil || cfg.ClientCAs != pool {
		t.Fatal("ClientCAs is not the operator pool")
	}
	if len(cfg.CurvePreferences) != 2 || cfg.CurvePreferences[0] != tls.X25519 || cfg.CurvePreferences[1] != tls.CurveP256 {
		t.Fatal("curve preferences are not X25519 and P-256")
	}
	if cfg.CipherSuites != nil {
		t.Fatal("CipherSuites must stay nil")
	}
	if !cfg.SessionTicketsDisabled {
		t.Fatal("session tickets are enabled")
	}
	if cfg.Renegotiation != tls.RenegotiateNever {
		t.Fatal("renegotiation is allowed")
	}
}

func TestApprovedImageHelloAndHeartbeat(t *testing.T) {
	pool := testPool(t)
	resetWorkerState(t, pool)
	repo := NewRepo(pool)
	workerID := uuid.New()
	fileBytes := readPositiveFixture(t, "manifest-image.json")
	ctx := context.Background()
	if err := repo.Approve(ctx, workerID, fileBytes); err != nil {
		t.Fatalf("approve: %v", err)
	}

	pool2 := testPool(t)
	var sessions int
	if err := pool2.QueryRow(ctx, `SELECT count(*) FROM worker_sessions WHERE worker_id = $1`, workerID).Scan(&sessions); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessions != 0 {
		t.Fatalf("sessions = %d, want 0 before HELLO", sessions)
	}
	var storedHash string
	if err := pool2.QueryRow(ctx, `SELECT manifest_sha256 FROM worker_identities WHERE worker_id = $1`, workerID).Scan(&storedHash); err != nil {
		t.Fatalf("read identity: %v", err)
	}
	sum := sha256.Sum256(fileBytes)
	if storedHash != hex.EncodeToString(sum[:]) {
		t.Fatal("approved digest is not sha256 of the manifest file bytes")
	}

	ca, caKey := mintTestCA(t)
	addr := listenWorker(t, NewRepo(pool2), ca, caKey)
	clientCert := mintClientCert(t, ca, caKey, workerID)
	client := workerHTTPClient(t, ca, clientCert)

	helloBody := []byte(`{"protocol_version":1,"worker_id":"` + workerID.String() + `","manifest_sha256":"` + storedHash + `","free_slots":3}`)
	status, hdr, respBody := doJSON(t, client, http.MethodPost, "https://"+addr+"/worker/v1/sessions", "", helloBody, nil)
	if status != http.StatusOK {
		t.Fatalf("HELLO status %d", status)
	}
	assertNoStoreJSON(t, hdr)
	var hello struct {
		ProtocolVersion           int             `json:"protocol_version"`
		SessionID                 string          `json:"session_id"`
		SessionToken              string          `json:"session_token"`
		PullWaitSeconds           int             `json:"pull_wait_seconds"`
		HeartbeatIntervalSeconds  int             `json:"heartbeat_interval_seconds"`
		LeaseSeconds              int             `json:"lease_seconds"`
		SessionMaxLifetimeSeconds int             `json:"session_max_lifetime_seconds"`
		MaxMessageBytes           int             `json:"max_message_bytes"`
		FileLinkMaxTTLSeconds     int             `json:"file_link_max_ttl_seconds"`
		ClockSkewMarginSeconds    int             `json:"clock_skew_margin_seconds"`
		PullRatePerSecond         int             `json:"pull_rate_per_second"`
		ProgressRatePerSecond     int             `json:"progress_rate_per_second"`
		FileAccess                string          `json:"file_access"`
		Control                   json.RawMessage `json:"control"`
	}
	if err := json.Unmarshal(respBody, &hello); err != nil {
		t.Fatalf("decode HELLO: %v", err)
	}
	if hello.ProtocolVersion != 1 || hello.PullWaitSeconds != 20 || hello.HeartbeatIntervalSeconds != 10 || hello.LeaseSeconds != 30 || hello.SessionMaxLifetimeSeconds != 86400 || hello.MaxMessageBytes != 262144 || hello.FileLinkMaxTTLSeconds != 300 || hello.ClockSkewMarginSeconds != 5 || hello.PullRatePerSecond != 2 || hello.ProgressRatePerSecond != 4 || hello.FileAccess != "direct" {
		t.Fatal("HELLO advertisement does not match the published starting numbers")
	}
	assertEmptyCancelList(t, hello.Control)
	sessionID, err := uuid.Parse(hello.SessionID)
	if err != nil || sessionID.String() != hello.SessionID {
		t.Fatal("session_id is not a canonical uuid")
	}
	mintedHash, err := HashToken(hello.SessionToken)
	if err != nil {
		t.Fatalf("HashToken response: %v", err)
	}

	var tokenHash string
	var freeSlots int
	var lifetime float64
	if err := pool2.QueryRow(ctx, `
		SELECT token_hash, free_slots, EXTRACT(EPOCH FROM (expires_at - opened_at))
		FROM worker_sessions WHERE id = $1`, sessionID).Scan(&tokenHash, &freeSlots, &lifetime); err != nil {
		t.Fatalf("read session: %v", err)
	}
	if tokenHash == hello.SessionToken || tokenHash != mintedHash || !isHex64(tokenHash) {
		t.Fatal("token_hash is not sha256 of the raw session token")
	}
	if freeSlots != 3 || lifetime != 86400 {
		t.Fatalf("session stored free_slots %d lifetime %v", freeSlots, lifetime)
	}
	var legacyEngine, legacyOperation string
	if err := pool2.QueryRow(ctx, `
		SELECT legacy_engine, legacy_operation FROM worker_capabilities WHERE worker_id = $1`, workerID).Scan(&legacyEngine, &legacyOperation); err != nil {
		t.Fatalf("read capability: %v", err)
	}
	if legacyEngine != "image" || legacyOperation != "convert" {
		t.Fatalf("legacy = %s/%s", legacyEngine, legacyOperation)
	}
	var formats int
	if err := pool2.QueryRow(ctx, `
		SELECT count(*)
		FROM worker_capability_formats f
		JOIN worker_capabilities c ON c.id = f.capability_id AND c.version = f.capability_version
		WHERE c.worker_id = $1`, workerID).Scan(&formats); err != nil {
		t.Fatalf("count formats: %v", err)
	}
	if formats != 20 {
		t.Fatalf("format rows = %d, want 20", formats)
	}
	var holds int
	if err := pool2.QueryRow(ctx, `SELECT count(*) FROM worker_slot_holds`).Scan(&holds); err != nil {
		t.Fatalf("count holds: %v", err)
	}
	if holds != 0 {
		t.Fatalf("slot holds = %d, want 0", holds)
	}

	status, hdr, respBody = doJSON(t, client, http.MethodPost, "https://"+addr+"/worker/v1/sessions/"+sessionID.String()+"/heartbeat", hello.SessionToken, []byte(`{"protocol_version":1}`), nil)
	if status != http.StatusOK {
		t.Fatalf("heartbeat status %d", status)
	}
	assertNoStoreJSON(t, hdr)
	var hb map[string]json.RawMessage
	if err := json.Unmarshal(respBody, &hb); err != nil {
		t.Fatalf("decode heartbeat: %v", err)
	}
	if string(hb["protocol_version"]) != "1" {
		t.Fatal("heartbeat protocol_version")
	}
	if _, ok := hb["session_id"]; ok {
		t.Fatal("heartbeat included session_id")
	}
	if _, ok := hb["lease_expires_at"]; ok {
		t.Fatal("heartbeat included lease_expires_at")
	}
	if _, ok := hb["assignments"]; ok {
		t.Fatal("heartbeat included assignments")
	}
	assertEmptyCancelList(t, hb["control"])
	if len(hb) != 2 {
		t.Fatal("heartbeat body is not protocol_version and control only")
	}
}

func assertEmptyCancelList(t *testing.T, raw json.RawMessage) {
	t.Helper()
	var control map[string]json.RawMessage
	if err := json.Unmarshal(raw, &control); err != nil {
		t.Fatalf("decode control: %v", err)
	}
	if string(control["cancel_attempt_ids"]) != "[]" || string(control["drain"]) != "false" || string(control["terminate"]) != "false" {
		t.Fatal("control is not an empty cancel list with drain and terminate false")
	}
}

func assertNoStoreJSON(t *testing.T, hdr http.Header) {
	t.Helper()
	if hdr.Get("Content-Type") != "application/json" {
		t.Fatal("Content-Type is not application/json")
	}
	if hdr.Get("Cache-Control") != "no-store" {
		t.Fatal("Cache-Control is not no-store")
	}
}

func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx)
	if err != nil {
		t.Fatalf("db.Connect: %v", err)
	}
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func resetWorkerState(t *testing.T, pool *pgxpool.Pool) {
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
	if err != nil {
		t.Fatalf("reset worker state: %v", err)
	}
}

func readPositiveFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "docs", "worker-protocol", "examples", "positive", name)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return body
}

func mintTestCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ca key: %v", err)
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
		t.Fatalf("ca cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse ca: %v", err)
	}
	return cert, key
}

func mintClientCert(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, workerID uuid.UUID) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("client key: %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	if err != nil {
		t.Fatalf("client serial: %v", err)
	}
	uri := urlWorker(workerID)
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
		t.Fatalf("client cert: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse client: %v", err)
	}
	if len(leaf.URIs) != 1 || leaf.URIs[0].String() != "urn:octoconv:worker:"+workerID.String() {
		t.Fatal("client certificate URI SAN is not the worker urn")
	}
	return tls.Certificate{Certificate: [][]byte{der, ca.Raw}, PrivateKey: key, Leaf: leaf}
}

func mintServerCert(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("server key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "worker.octoconv.local"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"worker.octoconv.local"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("server cert: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der, ca.Raw}, PrivateKey: key}
}

func listenWorker(t *testing.T, repo *Repo, ca *x509.Certificate, caKey *ecdsa.PrivateKey) string {
	t.Helper()
	cfg, err := ServerTLSConfig(repo)
	if err != nil {
		t.Fatalf("server tls: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	cfg.ClientCAs = pool
	cfg.Certificates = []tls.Certificate{mintServerCert(t, ca, caKey)}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{
		Handler:           NewHandler(repo).Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().String()
}

func workerHTTPClient(t *testing.T, ca *x509.Certificate, clientCert tls.Certificate) *http.Client {
	t.Helper()
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion:   tls.VersionTLS13,
				MaxVersion:   tls.VersionTLS13,
				RootCAs:      pool,
				Certificates: []tls.Certificate{clientCert},
				ServerName:   "worker.octoconv.local",
			},
		},
	}
}

func doJSON(t *testing.T, client *http.Client, method, url, token string, body []byte, extra http.Header) (int, http.Header, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for key, values := range extra {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, resp.Header.Clone(), respBody
}

func urlWorker(id uuid.UUID) url.URL {
	return url.URL{Scheme: "urn", Opaque: "octoconv:worker:" + id.String()}
}
