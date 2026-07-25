package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/apaderin/octoconv/internal/auth"
	"github.com/apaderin/octoconv/internal/jobs"
)

// sseUnset is the sentinel sseSnapshot uses for a nil *int (progress_pct /
// eta_seconds) field so the struct stays comparable with == -- comparing the
// *jobs.Job pointers directly would spuriously "change" every poll, since
// s.repo.Get returns a fresh *jobs.Job each call.
const sseUnset = -1

// sseSnapshot is the D-01 comparable summary of the fields that gate a new
// emit: status, progress_pct, eta_seconds. Two snapshots compare equal with
// == iff none of those three changed since the last emit.
type sseSnapshot struct {
	status string
	pct    int
	eta    int
}

func snapshotOf(job *jobs.Job) sseSnapshot {
	snap := sseSnapshot{status: job.Status, pct: sseUnset, eta: sseUnset}
	if job.ProgressPct != nil {
		snap.pct = *job.ProgressPct
	}
	if job.EtaSeconds != nil {
		snap.eta = *job.EtaSeconds
	}
	return snap
}

// sseProgressPayload is the `event: progress` JSON body -- deliberately the
// SAME null-aware shape handleGetJob renders (nil *int -> JSON null), so a
// client already parsing GET /v1/jobs/{id} responses needs no second parser.
type sseProgressPayload struct {
	Status      string `json:"status"`
	ProgressPct *int   `json:"progress_pct"`
	EtaSeconds  *int   `json:"eta_seconds"`
}

// emit writes one named SSE event carrying the null-aware progress payload
// and flushes immediately (no client-visible buffering delay).
func emit(rc *http.ResponseController, w http.ResponseWriter, event string, job *jobs.Job) error {
	body, err := json.Marshal(sseProgressPayload{
		Status:      job.Status,
		ProgressPct: job.ProgressPct,
		EtaSeconds:  job.EtaSeconds,
	})
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, "event: "+event+"\ndata: "+string(body)+"\n\n"); err != nil {
		return err
	}
	return rc.Flush()
}

// emitTerminal writes the ONE final event a terminal status produces (D-02):
// {"status": ...} only -- deliberately never a presigned result URL. The SSE
// handler never touches storage/presign; the client does a normal GET
// /v1/jobs/{id} for the result once it sees this event.
func emitTerminal(rc *http.ResponseController, w http.ResponseWriter, status string) error {
	body, err := json.Marshal(map[string]string{"status": status})
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, "event: progress\ndata: "+string(body)+"\n\n"); err != nil {
		return err
	}
	return rc.Flush()
}

// handleJobEvents streams a job's live progress over Server-Sent Events
// (PUSH-01, GET /v1/jobs/{id}/events). Auth + the 404-not-403 ownership
// guard are IDENTICAL to handleGetJob's (T-39-01/T-42-04): a cross-client or
// unknown job id is indistinguishable from not-found, and no stream ever
// opens for it.
//
// The stream is DB-poll-sourced (repeated s.repo.Get -- the SAME method
// handleGetJob calls; no in-memory pub/sub), so any stateless api replica
// can serve any client. It emits a named `progress` event ONLY when
// {status, progress_pct, eta_seconds} changed since the last emit, sends a
// `: keepalive` heartbeat comment on its own cadence to survive proxy
// idle-timeouts, and closes after one final event on a terminal status
// (done/failed/canceled, D-02).
//
// Every branch of the loop below runs in THIS goroutine -- the one net/http
// already allocated for this request. No additional goroutine is ever
// spawned here, so "zero leaked goroutines" on disconnect/terminal/max-cap
// is true by construction (T-42-06), not something a join/drain has to
// prove.
func (s *Server) handleJobEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return
	}

	job, err := s.repo.Get(ctx, id)
	if errors.Is(err, jobs.ErrNotFound) {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load job")
		return
	}

	// Ownership guard: IDENTICAL to handleGetJob's (T-39-01 precedent) -- a
	// cross-client job is reported exactly like a truly-missing one, never a
	// distinct 403 (T-42-04).
	client, _ := auth.ClientFromContext(ctx)
	if job.ClientID != client.ID {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Defensive against a future nginx/ingress buffering this stream; a
	// harmless no-op today (no reverse proxy sits in front of this service,
	// PROJECT.md).
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	rc := http.NewResponseController(w)

	// D-01/Pattern 1 (42-RESEARCH.md): emit the current state immediately on
	// connect -- a change from an unset baseline -- rather than making the
	// client wait up to ssePollInterval to learn where the job already is.
	if err := emit(rc, w, "progress", job); err != nil {
		return // client already gone
	}
	last := snapshotOf(job)
	if isTerminalStatus(job.Status) {
		_ = emitTerminal(rc, w, job.Status)
		return
	}

	pollTicker := time.NewTicker(s.ssePollInterval)
	defer pollTicker.Stop()
	heartbeatTicker := time.NewTicker(s.sseHeartbeatInterval)
	defer heartbeatTicker.Stop()
	maxDuration := time.NewTimer(s.sseMaxStreamDuration)
	defer maxDuration.Stop()

	for {
		select {
		case <-ctx.Done():
			// D-02: client disconnect. This IS the request goroutine; no
			// extra goroutine was ever spawned, so returning here ends
			// everything cleanly.
			return
		case <-maxDuration.C:
			// D-02: wedged/abandoned-stream safety cap.
			return
		case <-heartbeatTicker.C:
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return // write failed -- treat as disconnect
			}
			if err := rc.Flush(); err != nil {
				return
			}
		case <-pollTicker.C:
			job, err := s.repo.Get(ctx, id)
			if err != nil {
				// Transient DB error -- close rather than spin; the
				// client's own reconnect logic recovers.
				return
			}
			if snap := snapshotOf(job); snap != last {
				if err := emit(rc, w, "progress", job); err != nil {
					return
				}
				last = snap
			}
			if isTerminalStatus(job.Status) {
				_ = emitTerminal(rc, w, job.Status)
				return
			}
		}
	}
}
