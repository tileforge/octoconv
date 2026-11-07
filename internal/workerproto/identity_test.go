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
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tileforge/octoconv/internal/db"
	"github.com/tileforge/octoconv/internal/jobs"
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
		Handler:           NewHandler(repo, jobs.NewRepo(repo.pool), 120*time.Second).Routes(),
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

func TestManifestMismatchInsertsNothing(t *testing.T) {
	pool := testPool(t)
	resetWorkerState(t, pool)
	repo := NewRepo(pool)
	workerID := uuid.New()
	fileBytes := readPositiveFixture(t, "manifest-image.json")
	if err := repo.Approve(context.Background(), workerID, fileBytes); err != nil {
		t.Fatalf("approve: %v", err)
	}
	before := catalogCounts(t, pool)
	ca, caKey := mintTestCA(t)
	addr := listenWorker(t, repo, ca, caKey)
	client := workerHTTPClient(t, ca, mintClientCert(t, ca, caKey, workerID))
	body := mustJSON(t, map[string]any{
		"protocol_version": 1,
		"worker_id":        workerID.String(),
		"manifest_sha256":  strings.Repeat("b", 64),
		"free_slots":       1,
	})
	status, hdr, respBody := doJSON(t, client, http.MethodPost, "https://"+addr+"/worker/v1/sessions", "", body, nil)
	assertProtocolError(t, status, hdr, respBody, http.StatusForbidden, "manifest_mismatch")
	after := catalogCounts(t, pool)
	if after != before || after.sessions != 0 {
		t.Fatalf("catalog changed from %+v to %+v", before, after)
	}
}

func TestHelloWorkerIDMismatch(t *testing.T) {
	pool := testPool(t)
	resetWorkerState(t, pool)
	repo := NewRepo(pool)
	workerID := uuid.New()
	fileBytes := readPositiveFixture(t, "manifest-image.json")
	if err := repo.Approve(context.Background(), workerID, fileBytes); err != nil {
		t.Fatalf("approve: %v", err)
	}
	sum := sha256.Sum256(fileBytes)
	ca, caKey := mintTestCA(t)
	addr := listenWorker(t, repo, ca, caKey)
	client := workerHTTPClient(t, ca, mintClientCert(t, ca, caKey, workerID))
	body := mustJSON(t, map[string]any{
		"protocol_version": 1,
		"worker_id":        uuid.New().String(),
		"manifest_sha256":  hex.EncodeToString(sum[:]),
		"free_slots":       1,
	})
	status, hdr, respBody := doJSON(t, client, http.MethodPost, "https://"+addr+"/worker/v1/sessions", "", body, nil)
	assertProtocolError(t, status, hdr, respBody, http.StatusUnauthorized, "unauthorized")
	if n := sessionCount(t, pool, workerID); n != 0 {
		t.Fatalf("sessions = %d", n)
	}
}

func TestHelloUnknownField(t *testing.T) {
	pool := testPool(t)
	resetWorkerState(t, pool)
	repo := NewRepo(pool)
	workerID := uuid.New()
	fileBytes := readPositiveFixture(t, "manifest-image.json")
	if err := repo.Approve(context.Background(), workerID, fileBytes); err != nil {
		t.Fatalf("approve: %v", err)
	}
	sum := sha256.Sum256(fileBytes)
	digest := hex.EncodeToString(sum[:])
	ca, caKey := mintTestCA(t)
	addr := listenWorker(t, repo, ca, caKey)
	client := workerHTTPClient(t, ca, mintClientCert(t, ca, caKey, workerID))
	unknown := mustJSON(t, map[string]any{
		"protocol_version": 1,
		"worker_id":        workerID.String(),
		"manifest_sha256":  digest,
		"free_slots":       1,
		"priority":         1,
	})
	status, hdr, respBody := doJSON(t, client, http.MethodPost, "https://"+addr+"/worker/v1/sessions", "", unknown, nil)
	assertProtocolError(t, status, hdr, respBody, http.StatusBadRequest, "unknown_field")
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(respBody, &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(payload["message"]) != `"unknown field"` || string(payload["retryable"]) != "false" {
		t.Fatal("unknown field body does not match the published fixture")
	}
	if _, ok := payload["error"]; ok {
		t.Fatal("client error field is present")
	}
	if sessionCount(t, pool, workerID) != 0 {
		t.Fatal("unknown field opened a session")
	}

	extended := mustJSON(t, map[string]any{
		"protocol_version": 1,
		"worker_id":        workerID.String(),
		"manifest_sha256":  digest,
		"free_slots":       1,
		"extensions":       map[string]any{"priority": 1},
	})
	status, _, _ = doJSON(t, client, http.MethodPost, "https://"+addr+"/worker/v1/sessions", "", extended, nil)
	if status != http.StatusOK {
		t.Fatalf("extensions status %d", status)
	}
	if sessionCount(t, pool, workerID) != 1 {
		t.Fatal("extensions did not open a session")
	}
}

func TestHelloProtocolVersionTwo(t *testing.T) {
	pool := testPool(t)
	resetWorkerState(t, pool)
	repo := NewRepo(pool)
	workerID := uuid.New()
	fileBytes := readPositiveFixture(t, "manifest-image.json")
	if err := repo.Approve(context.Background(), workerID, fileBytes); err != nil {
		t.Fatalf("approve: %v", err)
	}
	sum := sha256.Sum256(fileBytes)
	digest := hex.EncodeToString(sum[:])
	ca, caKey := mintTestCA(t)
	addr := listenWorker(t, repo, ca, caKey)
	client := workerHTTPClient(t, ca, mintClientCert(t, ca, caKey, workerID))
	two := mustJSON(t, map[string]any{
		"protocol_version": 2,
		"worker_id":        workerID.String(),
		"manifest_sha256":  digest,
		"free_slots":       1,
	})
	status, hdr, respBody := doJSON(t, client, http.MethodPost, "https://"+addr+"/worker/v1/sessions", "", two, nil)
	assertProtocolError(t, status, hdr, respBody, http.StatusBadRequest, "incompatible_protocol")
	if sessionCount(t, pool, workerID) != 0 {
		t.Fatal("protocol 2 opened a session")
	}
	fractional := []byte(`{"protocol_version":1.5,"worker_id":"` + workerID.String() + `","manifest_sha256":"` + digest + `","free_slots":1}`)
	status, hdr, respBody = doJSON(t, client, http.MethodPost, "https://"+addr+"/worker/v1/sessions", "", fractional, nil)
	assertProtocolError(t, status, hdr, respBody, http.StatusBadRequest, "invalid_message")
	if sessionCount(t, pool, workerID) != 0 {
		t.Fatal("fractional protocol opened a session")
	}
}

func TestSecondFormatClaimRollsBack(t *testing.T) {
	pool := testPool(t)
	resetWorkerState(t, pool)
	repo := NewRepo(pool)
	ctx := context.Background()
	first := uuid.New()
	second := uuid.New()
	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errCh <- repo.Approve(ctx, first, claimManifest(t, "octoconv.convert.claima", "jpg", "png"))
	}()
	go func() {
		defer wg.Done()
		errCh <- repo.Approve(ctx, second, claimManifest(t, "octoconv.convert.claimb", "jpeg", "png"))
	}()
	wg.Wait()
	close(errCh)
	okN, conflictN := 0, 0
	for err := range errCh {
		switch {
		case err == nil:
			okN++
		case errors.Is(err, ErrCapabilityConflict):
			conflictN++
		default:
			t.Fatalf("approve: %v", err)
		}
	}
	if okN != 1 || conflictN != 1 {
		t.Fatalf("successes=%d conflicts=%d", okN, conflictN)
	}
	var rows int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM worker_capability_formats
		WHERE client_operation = 'convert' AND source_format = 'jpg' AND target_format = 'png'`,
	).Scan(&rows); err != nil {
		t.Fatalf("count formats: %v", err)
	}
	if rows != 1 {
		t.Fatalf("jpg/png rows = %d", rows)
	}
	var winner uuid.UUID
	var hash string
	if err := pool.QueryRow(ctx, `SELECT worker_id, manifest_sha256 FROM worker_identities`).Scan(&winner, &hash); err != nil {
		t.Fatalf("read winner: %v", err)
	}
	err := repo.Approve(ctx, uuid.New(), claimManifest(t, "octoconv.convert.claimc", "jpeg", "png"))
	if !errors.Is(err, ErrCapabilityConflict) {
		t.Fatalf("sequential jpeg claim: %v", err)
	}
	var hashAfter string
	if err := pool.QueryRow(ctx, `SELECT manifest_sha256 FROM worker_identities WHERE worker_id = $1`, winner).Scan(&hashAfter); err != nil {
		t.Fatalf("reread winner: %v", err)
	}
	if hashAfter != hash {
		t.Fatal("conflicting approve changed the first manifest hash")
	}
	var identities int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM worker_identities`).Scan(&identities); err != nil {
		t.Fatalf("count identities: %v", err)
	}
	if identities != 1 {
		t.Fatalf("identities = %d", identities)
	}
}

func TestImagepdfApproveHasNoLegacyEngine(t *testing.T) {
	pool := testPool(t)
	resetWorkerState(t, pool)
	repo := NewRepo(pool)
	workerID := uuid.New()
	body := readPositiveFixture(t, "manifest-imagepdf.json")
	ctx := context.Background()
	if err := repo.Approve(ctx, workerID, body); err != nil {
		t.Fatalf("approve: %v", err)
	}
	var legacyOp, legacyEngine *string
	var formats int
	if err := pool.QueryRow(ctx, `
		SELECT c.legacy_operation, c.legacy_engine, count(f.source_format)
		FROM worker_capabilities c
		LEFT JOIN worker_capability_formats f
		  ON f.capability_id = c.id AND f.capability_version = c.version
		WHERE c.worker_id = $1
		GROUP BY c.legacy_operation, c.legacy_engine`, workerID).Scan(&legacyOp, &legacyEngine, &formats); err != nil {
		t.Fatalf("read capability: %v", err)
	}
	if legacyOp != nil || legacyEngine != nil || formats != 1 {
		t.Fatalf("legacy op=%v engine=%v formats=%d", legacyOp, legacyEngine, formats)
	}
	var source, target string
	if err := pool.QueryRow(ctx, `
		SELECT source_format, target_format FROM worker_capability_formats`).Scan(&source, &target); err != nil {
		t.Fatalf("read format: %v", err)
	}
	if source != "png" || target != "pdf" {
		t.Fatalf("format %s -> %s", source, target)
	}
	if AccountingKey("", "octoconv.convert.imagepdf") != "octoconv.convert.imagepdf" {
		t.Fatal("imagepdf accounting key is not the capability id")
	}
	if AccountingKey("image", "octoconv.convert.imagepdf") != "image" {
		t.Fatal("image accounting key is not the legacy engine")
	}
	if err := repo.Approve(ctx, workerID, body); err != nil {
		t.Fatalf("reapprove: %v", err)
	}
	var caps int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM worker_capabilities WHERE worker_id = $1`, workerID).Scan(&caps); err != nil {
		t.Fatalf("count capabilities: %v", err)
	}
	if caps != 1 {
		t.Fatalf("capabilities = %d", caps)
	}
}

func TestCompareInstalledDoesNotInsert(t *testing.T) {
	pool := testPool(t)
	resetWorkerState(t, pool)
	repo := NewRepo(pool)
	ctx := context.Background()
	workerID := uuid.New()
	body := readPositiveFixture(t, "manifest-imagepdf.json")
	if err := repo.Approve(ctx, workerID, body); err != nil {
		t.Fatalf("approve: %v", err)
	}
	var before int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM worker_identities`).Scan(&before); err != nil {
		t.Fatalf("count: %v", err)
	}
	empty, err := repo.CompareInstalled(ctx, t.TempDir())
	if err != nil || len(empty.Mismatched) != 0 {
		t.Fatalf("empty dir: %v %+v", err, empty)
	}
	var after int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM worker_identities`).Scan(&after); err != nil {
		t.Fatalf("count: %v", err)
	}
	if after != before {
		t.Fatalf("identities %d to %d", before, after)
	}

	dir := t.TempDir()
	sub := filepath.Join(dir, workerID.String())
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "manifest.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	var hashBefore string
	if err := pool.QueryRow(ctx, `SELECT manifest_sha256 FROM worker_identities WHERE worker_id = $1`, workerID).Scan(&hashBefore); err != nil {
		t.Fatalf("hash: %v", err)
	}
	mismatch, err := repo.CompareInstalled(ctx, dir)
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if len(mismatch.Mismatched) != 1 || mismatch.Mismatched[0] != workerID {
		t.Fatal("hash mismatch was not recorded")
	}
	var hashAfter string
	if err := pool.QueryRow(ctx, `SELECT manifest_sha256 FROM worker_identities WHERE worker_id = $1`, workerID).Scan(&hashAfter); err != nil {
		t.Fatalf("hash: %v", err)
	}
	if hashAfter != hashBefore {
		t.Fatal("compare updated manifest_sha256")
	}

	missingID := uuid.New()
	missingDir := t.TempDir()
	missingSub := filepath.Join(missingDir, missingID.String())
	if err := os.Mkdir(missingSub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(missingSub, "manifest.json"), body, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	none, err := repo.CompareInstalled(ctx, missingDir)
	if err != nil || len(none.Mismatched) != 0 {
		t.Fatalf("missing row: %v %+v", err, none)
	}
	var inserted bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM worker_identities WHERE worker_id = $1)`, missingID).Scan(&inserted); err != nil {
		t.Fatalf("exists: %v", err)
	}
	if inserted {
		t.Fatal("compare inserted a catalog row")
	}

	bad := t.TempDir()
	if err := os.Mkdir(filepath.Join(bad, "not-a-uuid"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := repo.CompareInstalled(ctx, bad); err == nil {
		t.Fatal("non-uuid child was accepted")
	}
}

type catalogSnapshot struct {
	identities int
	caps       int
	formats    int
	sessions   int
}

func catalogCounts(t *testing.T, pool *pgxpool.Pool) catalogSnapshot {
	t.Helper()
	var snap catalogSnapshot
	err := pool.QueryRow(context.Background(), `
		SELECT
			(SELECT count(*) FROM worker_identities),
			(SELECT count(*) FROM worker_capabilities),
			(SELECT count(*) FROM worker_capability_formats),
			(SELECT count(*) FROM worker_sessions)`).Scan(&snap.identities, &snap.caps, &snap.formats, &snap.sessions)
	if err != nil {
		t.Fatalf("catalog counts: %v", err)
	}
	return snap
}

func sessionCount(t *testing.T, pool *pgxpool.Pool, workerID uuid.UUID) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(), `SELECT count(*) FROM worker_sessions WHERE worker_id = $1`, workerID).Scan(&n)
	if err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	return n
}

func assertProtocolError(t *testing.T, status int, hdr http.Header, body []byte, wantStatus int, code string) {
	t.Helper()
	if status != wantStatus {
		t.Fatalf("status %d, want %d", status, wantStatus)
	}
	assertNoStoreJSON(t, hdr)
	var got map[string]json.RawMessage
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if string(got["code"]) != `"`+code+`"` {
		t.Fatalf("code %s", got["code"])
	}
	if _, ok := got["error"]; ok {
		t.Fatal("response has an error field")
	}
	if string(got["retryable"]) != "false" {
		t.Fatal("retryable is not false")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return body
}

func TestClosedSessionStaysClosedAfterDedupDelete(t *testing.T) {
	live := openHello(t)
	ctx := context.Background()
	if _, err := live.pool.Exec(ctx, `
		INSERT INTO worker_request_dedup (session_id, dedup_key) VALUES ($1, 'pull-1')`, live.sessionID); err != nil {
		t.Fatalf("insert dedup: %v", err)
	}
	status, hdr, body := doJSON(t, live.client, http.MethodDelete, live.sessionURL(), live.token, []byte(`{"protocol_version":1}`), nil)
	if status != http.StatusOK {
		t.Fatalf("delete status %d", status)
	}
	assertNoStoreJSON(t, hdr)
	assertHeartbeatShape(t, body)
	var dedup int
	var closedAt *time.Time
	if err := live.pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM worker_request_dedup WHERE session_id = $1),
		       (SELECT closed_at FROM worker_sessions WHERE id = $1)`, live.sessionID).Scan(&dedup, &closedAt); err != nil {
		t.Fatalf("read close: %v", err)
	}
	if dedup != 0 || closedAt == nil {
		t.Fatal("dedup remains or the session row was not closed")
	}
	var exists bool
	if err := live.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM worker_sessions WHERE id = $1)`, live.sessionID).Scan(&exists); err != nil {
		t.Fatalf("exists: %v", err)
	}
	if !exists {
		t.Fatal("close deleted the session row")
	}
	status, hdr, body = doJSON(t, live.client, http.MethodPost, live.sessionURL()+"/heartbeat", live.token, []byte(`{"protocol_version":1}`), nil)
	assertProtocolError(t, status, hdr, body, http.StatusUnauthorized, "session_closed")
}

func TestSlotHoldSurvivesCloseAndNewHello(t *testing.T) {
	live := openHello(t)
	ctx := context.Background()
	holdID := uuid.New()
	if _, err := live.pool.Exec(ctx, `
		INSERT INTO worker_slot_holds (id, owner_key, session_id) VALUES ($1, $2, $3)`,
		holdID, "owner-image", live.sessionID); err != nil {
		t.Fatalf("insert hold: %v", err)
	}
	before := readHold(t, live.pool, holdID)
	status, _, _ := doJSON(t, live.client, http.MethodDelete, live.sessionURL(), live.token, []byte(`{"protocol_version":1}`), nil)
	if status != http.StatusOK {
		t.Fatalf("delete status %d", status)
	}
	sum := sha256.Sum256(live.manifest)
	hello := mustJSON(t, map[string]any{
		"protocol_version": 1,
		"worker_id":        live.workerID.String(),
		"manifest_sha256":  hex.EncodeToString(sum[:]),
		"free_slots":       1,
	})
	status, _, _ = doJSON(t, live.client, http.MethodPost, "https://"+live.addr+"/worker/v1/sessions", "", hello, nil)
	if status != http.StatusOK {
		t.Fatalf("second HELLO status %d", status)
	}
	after := readHold(t, live.pool, holdID)
	if after != before {
		t.Fatal("slot hold changed across close and a new HELLO")
	}
	var holds int
	if err := live.pool.QueryRow(ctx, `SELECT count(*) FROM worker_slot_holds`).Scan(&holds); err != nil {
		t.Fatalf("count holds: %v", err)
	}
	if holds != 1 {
		t.Fatalf("holds = %d", holds)
	}
}

func TestExpiredSessionIsClosed(t *testing.T) {
	live := openHello(t)
	if _, err := live.pool.Exec(context.Background(), `
		UPDATE worker_sessions SET expires_at = now() - interval '1 second' WHERE id = $1`, live.sessionID); err != nil {
		t.Fatalf("expire: %v", err)
	}
	status, hdr, body := doJSON(t, live.client, http.MethodPost, live.sessionURL()+"/heartbeat", live.token, []byte(`{"protocol_version":1}`), nil)
	assertProtocolError(t, status, hdr, body, http.StatusUnauthorized, "session_closed")
}

func TestAttemptHeaderOnHeartbeatChangesNothing(t *testing.T) {
	live := openHello(t)
	ctx := context.Background()
	holdID := uuid.New()
	if _, err := live.pool.Exec(ctx, `
		INSERT INTO worker_slot_holds (id, owner_key, session_id) VALUES ($1, $2, $3)`,
		holdID, "owner-image", live.sessionID); err != nil {
		t.Fatalf("insert hold: %v", err)
	}
	before := readHold(t, live.pool, holdID)
	header := make(http.Header)
	header.Set("Octoconv-Attempt", strings.Repeat("c", 64))
	status, hdr, body := doJSON(t, live.client, http.MethodPost, live.sessionURL()+"/heartbeat", live.token, []byte(`{"protocol_version":1}`), header)
	assertProtocolError(t, status, hdr, body, http.StatusUnauthorized, "unauthorized")
	var closedAt *time.Time
	if err := live.pool.QueryRow(ctx, `SELECT closed_at FROM worker_sessions WHERE id = $1`, live.sessionID).Scan(&closedAt); err != nil {
		t.Fatalf("read session: %v", err)
	}
	if closedAt != nil {
		t.Fatal("attempt header closed the session")
	}
	after := readHold(t, live.pool, holdID)
	if after != before {
		t.Fatal("attempt header changed the slot hold")
	}
}

type liveSession struct {
	pool      *pgxpool.Pool
	workerID  uuid.UUID
	sessionID uuid.UUID
	token     string
	addr      string
	client    *http.Client
	manifest  []byte
}

func (s liveSession) sessionURL() string {
	return "https://" + s.addr + "/worker/v1/sessions/" + s.sessionID.String()
}

func openHello(t *testing.T) liveSession {
	t.Helper()
	pool := testPool(t)
	resetWorkerState(t, pool)
	repo := NewRepo(pool)
	workerID := uuid.New()
	manifest := readPositiveFixture(t, "manifest-image.json")
	if err := repo.Approve(context.Background(), workerID, manifest); err != nil {
		t.Fatalf("approve: %v", err)
	}
	sum := sha256.Sum256(manifest)
	ca, caKey := mintTestCA(t)
	addr := listenWorker(t, repo, ca, caKey)
	client := workerHTTPClient(t, ca, mintClientCert(t, ca, caKey, workerID))
	body := mustJSON(t, map[string]any{
		"protocol_version": 1,
		"worker_id":        workerID.String(),
		"manifest_sha256":  hex.EncodeToString(sum[:]),
		"free_slots":       1,
	})
	status, _, respBody := doJSON(t, client, http.MethodPost, "https://"+addr+"/worker/v1/sessions", "", body, nil)
	if status != http.StatusOK {
		t.Fatalf("HELLO status %d", status)
	}
	var hello struct {
		SessionID    string `json:"session_id"`
		SessionToken string `json:"session_token"`
	}
	if err := json.Unmarshal(respBody, &hello); err != nil {
		t.Fatalf("decode HELLO: %v", err)
	}
	sessionID, err := uuid.Parse(hello.SessionID)
	if err != nil {
		t.Fatal("session_id is not a uuid")
	}
	return liveSession{
		pool:      pool,
		workerID:  workerID,
		sessionID: sessionID,
		token:     hello.SessionToken,
		addr:      addr,
		client:    client,
		manifest:  manifest,
	}
}

type holdRow struct {
	id         uuid.UUID
	ownerKey   string
	sessionID  uuid.UUID
	releasedAt *time.Time
}

func readHold(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) holdRow {
	t.Helper()
	var row holdRow
	err := pool.QueryRow(context.Background(), `
		SELECT id, owner_key, session_id, released_at FROM worker_slot_holds WHERE id = $1`, id).
		Scan(&row.id, &row.ownerKey, &row.sessionID, &row.releasedAt)
	if err != nil {
		t.Fatalf("read hold: %v", err)
	}
	return row
}

func assertHeartbeatShape(t *testing.T, body []byte) {
	t.Helper()
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode heartbeat: %v", err)
	}
	if string(payload["protocol_version"]) != "1" || len(payload) != 2 {
		t.Fatal("success body is not protocol_version and control")
	}
	assertEmptyCancelList(t, payload["control"])
}

func claimManifest(t *testing.T, id, from, to string) []byte {
	t.Helper()
	return mustJSON(t, map[string]any{
		"capabilities": []any{
			map[string]any{
				"id":               id,
				"version":          1,
				"client_operation": "convert",
				"inputs":           []any{map[string]any{"role": "source", "min": 1, "max": 1}},
				"outputs":          []any{map[string]any{"role": "primary", "min": 1, "max": 1}},
				"parameter_schema": map[string]any{},
				"formats":          []any{map[string]any{"from": from, "to": to}},
			},
		},
	})
}
