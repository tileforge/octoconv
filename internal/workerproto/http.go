package workerproto

import (
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

const maxMessageBytes = 262144

type numKind int

const (
	numMissing numKind = iota
	numBad
	numOK
)

// Handler serves worker session routes.
type Handler struct {
	repo *Repo
}

// NewHandler returns a handler bound to the catalog.
func NewHandler(repo *Repo) *Handler {
	return &Handler{repo: repo}
}

// Routes returns the worker router. It is not mounted on the client API.
func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Post("/worker/v1/sessions", h.handleHello)
	r.Post("/worker/v1/sessions/{id}/heartbeat", h.handleHeartbeat)
	r.Delete("/worker/v1/sessions/{id}", h.handleDelete)
	return r
}

type helloRequest struct {
	ProtocolVersion json.Number     `json:"protocol_version"`
	WorkerID        string          `json:"worker_id"`
	ManifestSHA256  string          `json:"manifest_sha256"`
	FreeSlots       json.Number     `json:"free_slots"`
	Extensions      json.RawMessage `json:"extensions"`
}

type protocolRequest struct {
	ProtocolVersion json.Number     `json:"protocol_version"`
	Extensions      json.RawMessage `json:"extensions"`
}

type controlWire struct {
	CancelAttemptIDs []string `json:"cancel_attempt_ids"`
	Drain            bool     `json:"drain"`
	Terminate        bool     `json:"terminate"`
}

type helloWire struct {
	ProtocolVersion           int         `json:"protocol_version"`
	SessionID                 string      `json:"session_id"`
	SessionToken              string      `json:"session_token"`
	PullWaitSeconds           int         `json:"pull_wait_seconds"`
	HeartbeatIntervalSeconds  int         `json:"heartbeat_interval_seconds"`
	LeaseSeconds              int         `json:"lease_seconds"`
	SessionMaxLifetimeSeconds int         `json:"session_max_lifetime_seconds"`
	MaxMessageBytes           int         `json:"max_message_bytes"`
	FileLinkMaxTTLSeconds     int         `json:"file_link_max_ttl_seconds"`
	ClockSkewMarginSeconds    int         `json:"clock_skew_margin_seconds"`
	PullRatePerSecond         int         `json:"pull_rate_per_second"`
	ProgressRatePerSecond     int         `json:"progress_rate_per_second"`
	FileAccess                string      `json:"file_access"`
	Control                   controlWire `json:"control"`
}

type heartbeatWire struct {
	ProtocolVersion int         `json:"protocol_version"`
	Control         controlWire `json:"control"`
}

type wireError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

func (h *Handler) handleHello(w http.ResponseWriter, r *http.Request) {
	leaf, ok := peerLeaf(w, r)
	if !ok {
		return
	}
	var req helloRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if !protocolVersionOK(w, req.ProtocolVersion) {
		return
	}
	if !extensionsObject(req.Extensions) {
		writeCode(w, http.StatusBadRequest, "invalid_message")
		return
	}
	slots, kind := integerToken(req.FreeSlots)
	if kind != numOK || slots < 0 || slots > int64(^uint32(0)>>1) {
		writeCode(w, http.StatusBadRequest, "invalid_message")
		return
	}
	bodyID, err := uuid.Parse(req.WorkerID)
	if err != nil {
		writeCode(w, http.StatusBadRequest, "invalid_message")
		return
	}
	certID, err := workerIDFromLeaf(leaf)
	if err != nil || certID != bodyID {
		writeCode(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !hex64.MatchString(req.ManifestSHA256) {
		writeCode(w, http.StatusBadRequest, "invalid_message")
		return
	}
	approved, found, err := h.repo.approvedDigest(r.Context(), certID)
	if err != nil {
		writeCode(w, http.StatusInternalServerError, "invalid_message")
		return
	}
	if !found || approved != req.ManifestSHA256 {
		writeCode(w, http.StatusForbidden, "manifest_mismatch")
		return
	}
	serial, err := certSerial(leaf)
	if err != nil {
		writeCode(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	wire, tokenHash, err := NewSessionToken()
	if err != nil {
		writeCode(w, http.StatusInternalServerError, "invalid_message")
		return
	}
	sessionID := uuid.New()
	if err := h.repo.openSession(r.Context(), newSession{
		ID:        sessionID,
		WorkerID:  certID,
		TokenHash: tokenHash,
		Serial:    serial,
		IssuerSHA: issuerSHA256(leaf),
		FreeSlots: int(slots),
	}); err != nil {
		writeCode(w, http.StatusInternalServerError, "invalid_message")
		return
	}
	writeJSON(w, http.StatusOK, helloWire{
		ProtocolVersion:           1,
		SessionID:                 sessionID.String(),
		SessionToken:              wire,
		PullWaitSeconds:           20,
		HeartbeatIntervalSeconds:  10,
		LeaseSeconds:              30,
		SessionMaxLifetimeSeconds: 86400,
		MaxMessageBytes:           maxMessageBytes,
		FileLinkMaxTTLSeconds:     300,
		ClockSkewMarginSeconds:    5,
		PullRatePerSecond:         2,
		ProgressRatePerSecond:     4,
		FileAccess:                "direct",
		Control:                   freshControl(),
	})
}

func (h *Handler) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	h.finishSession(w, r, false)
}

func (h *Handler) handleDelete(w http.ResponseWriter, r *http.Request) {
	h.finishSession(w, r, true)
}

func (h *Handler) finishSession(w http.ResponseWriter, r *http.Request, closeSession bool) {
	if _, ok := r.Header["Octoconv-Attempt"]; ok {
		writeCode(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	sess, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	var req protocolRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if !protocolVersionOK(w, req.ProtocolVersion) {
		return
	}
	if !extensionsObject(req.Extensions) {
		writeCode(w, http.StatusBadRequest, "invalid_message")
		return
	}
	if closeSession {
		if err := h.repo.closeSession(r.Context(), sess.ID); err != nil {
			writeCode(w, http.StatusInternalServerError, "invalid_message")
			return
		}
	}
	writeJSON(w, http.StatusOK, heartbeatWire{ProtocolVersion: 1, Control: freshControl()})
}

func (h *Handler) authenticate(w http.ResponseWriter, r *http.Request) (storedSession, bool) {
	leaf, ok := peerLeaf(w, r)
	if !ok {
		return storedSession{}, false
	}
	serial, err := certSerial(leaf)
	if err != nil {
		writeCode(w, http.StatusUnauthorized, "unauthorized")
		return storedSession{}, false
	}
	denied, err := h.repo.serialDenied(r.Context(), issuerSHA256(leaf), serial)
	if err != nil {
		writeCode(w, http.StatusInternalServerError, "invalid_message")
		return storedSession{}, false
	}
	if denied {
		writeCode(w, http.StatusUnauthorized, "session_closed")
		return storedSession{}, false
	}
	tokenHash, err := bearerHash(r.Header.Get("Authorization"))
	if err != nil {
		writeCode(w, http.StatusUnauthorized, "unauthorized")
		return storedSession{}, false
	}
	sess, err := h.repo.sessionByHash(r.Context(), tokenHash)
	if errors.Is(err, errSessionNotFound) {
		writeCode(w, http.StatusUnauthorized, "unauthorized")
		return storedSession{}, false
	}
	if err != nil {
		writeCode(w, http.StatusInternalServerError, "invalid_message")
		return storedSession{}, false
	}
	if sess.Closed || sess.Expired {
		writeCode(w, http.StatusUnauthorized, "session_closed")
		return storedSession{}, false
	}
	certID, err := workerIDFromLeaf(leaf)
	if err != nil || certID != sess.WorkerID {
		writeCode(w, http.StatusUnauthorized, "unauthorized")
		return storedSession{}, false
	}
	urlID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil || urlID != sess.ID {
		writeCode(w, http.StatusUnauthorized, "unauthorized")
		return storedSession{}, false
	}
	return sess, true
}

func peerLeaf(w http.ResponseWriter, r *http.Request) (*x509.Certificate, bool) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		writeCode(w, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}
	return r.TLS.PeerCertificates[0], true
}

func bearerHash(header string) (string, error) {
	const prefix = "Bearer "
	if len(header) < len(prefix) || header[:len(prefix)] != prefix {
		return "", errors.New("bad session token")
	}
	return HashToken(header[len(prefix):])
}

func protocolVersionOK(w http.ResponseWriter, n json.Number) bool {
	version, kind := integerToken(n)
	if kind != numOK {
		writeCode(w, http.StatusBadRequest, "invalid_message")
		return false
	}
	if version != 1 {
		writeCode(w, http.StatusBadRequest, "incompatible_protocol")
		return false
	}
	return true
}

func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxMessageBytes)
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeDecodeError(w, err)
		return false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			writeCode(w, http.StatusBadRequest, "invalid_message")
			return false
		}
		writeDecodeError(w, err)
		return false
	}
	return true
}

func writeDecodeError(w http.ResponseWriter, err error) {
	var maxBytes *http.MaxBytesError
	if errors.As(err, &maxBytes) || strings.Contains(err.Error(), "request body too large") {
		writeCode(w, http.StatusRequestEntityTooLarge, "message_too_large")
		return
	}
	if strings.Contains(err.Error(), "unknown field") {
		writeCode(w, http.StatusBadRequest, "unknown_field")
		return
	}
	writeCode(w, http.StatusBadRequest, "invalid_message")
}

func extensionsObject(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return true
	}
	return json.Valid([]byte(trimmed)) && trimmed[0] == '{'
}

func integerToken(n json.Number) (int64, numKind) {
	text := string(n)
	if text == "" {
		return 0, numMissing
	}
	if strings.ContainsAny(text, ".eE") {
		return 0, numBad
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, numBad
	}
	return value, numOK
}

func freshControl() controlWire {
	return controlWire{CancelAttemptIDs: []string{}}
}

func writeCode(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, wireError{Code: code, Message: messageFor(code), Retryable: false})
}

func messageFor(code string) string {
	switch code {
	case "unknown_field":
		return "unknown field"
	case "manifest_mismatch":
		return "manifest mismatch"
	case "incompatible_protocol":
		return "incompatible protocol"
	case "message_too_large":
		return "message too large"
	case "unauthorized":
		return "unauthorized"
	case "session_closed":
		return "session closed"
	default:
		return "invalid message"
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
