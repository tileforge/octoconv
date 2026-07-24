// Package cancel is a per-process, best-effort speed layer for cancelling an
// in-flight conversion. It is deliberately NOT the source of correctness:
// the guarded-transition allow-list (internal/jobs/repo.go's transition
// helper, MarkActive/MarkDone/MarkFailed) is the sole authoritative
// mechanism that keeps a canceled job from being resurrected (D-05/D-12). A
// worker that misses the signal below simply runs to completion/timeout and
// has its terminal Mark* rejected -- wasted compute, never state
// corruption.
//
// The package has two halves: an in-memory Registry mapping a job id to the
// context.CancelFunc for the attempt this process is currently running
// (D-04), and (in pubsub.go) a Redis pub/sub Publisher/Subscriber over the
// job:cancel channel that lets the API process wake the exact worker
// process running a given job.
package cancel

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

// Channel is the Redis pub/sub channel name job ids are published on. Single
// source of truth, shared by Publisher and Subscriber (pubsub.go).
const Channel = "job:cancel"

// Registry maps a job id to the context.CancelFunc for the attempt this
// process is currently running that job under, mutex-guarded so
// Register/Deregister/Cancel are safe to call concurrently from the
// worker's handler goroutines and the Subscriber's receive loop.
type Registry struct {
	mu sync.Mutex
	m  map[uuid.UUID]context.CancelFunc
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{m: make(map[uuid.UUID]context.CancelFunc)}
}

// Default is the process-wide registry, populated by the worker around each
// conversion attempt -- mirrors convert.Default's global-at-init singleton
// style (internal/convert/convert.go).
var Default = NewRegistry()

// Register records the CancelFunc for id. Callers (the worker's process())
// MUST call this before starting the conversion attempt and Deregister on
// return (typically via defer), so Cancel only ever affects the attempt
// currently in flight.
func (r *Registry) Register(id uuid.UUID, cancel context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[id] = cancel
}

// Deregister removes id from the registry. Safe to call even if id was
// never registered (or was already removed).
func (r *Registry) Deregister(id uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.m, id)
}

// Cancel invokes the CancelFunc registered for id, if any. If this process
// is not currently running id (never registered, already deregistered, or
// running on a different worker process entirely), Cancel is a safe no-op
// -- D-04's explicit contract, since the cancel signal fans out to every
// worker process and only the one actually holding the job should act on
// it.
func (r *Registry) Cancel(id uuid.UUID) {
	r.mu.Lock()
	cancel, ok := r.m[id]
	r.mu.Unlock()
	if ok {
		cancel()
	}
}
