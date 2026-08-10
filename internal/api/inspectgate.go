package api

import (
	"net/http"
	"strconv"

	"github.com/apaderin/octoconv/internal/errcode"
	"github.com/apaderin/octoconv/internal/ratelimit"
)

const (
	// defaultInspectMaxConcurrent is newInspectSemaphore's own safe fallback
	// for maxConcurrent <= 0 -- independent of api.Config's own defaulting
	// (Plan 58-06 Task 3 adds that on top, in NewServer, in the project's
	// usual `if cfg.X == 0 { cfg.X = ... }` idiom). Kept here too so a
	// directly-constructed gate -- exactly what every test in this file
	// does -- never silently admits zero requests just because a caller
	// forgot to pass a positive number.
	defaultInspectMaxConcurrent = 4

	// inspectBusyRetryAfterSeconds is inspectSemaphore's fixed Retry-After
	// hint. A slot frees in about the time ONE inspect parse takes --
	// single-digit seconds, bounded BY CONSTRUCTION by
	// internal/inspect.Budget's own default MaxDuration (2s,
	// internal/inspect/budget.go) -- a full order of magnitude below
	// quotaConcurrencyRetryAfterSeconds (handlers.go, 30s: a JOB
	// concurrency slot frees only when an in-flight CONVERSION reaches a
	// terminal state, minutes for some engine classes, not one bounded
	// parse). This value is derived from that existing, already-documented
	// ceiling, not copied from quotaConcurrencyRetryAfterSeconds itself --
	// copying a number from an unrelated mechanism is this project's own
	// documented anti-pattern (see quotaConcurrencyRetryAfterSeconds's own
	// comment, and .env.example's AUDIO_ENGINE_TIMEOUT/OCR_ENGINE_TIMEOUT
	// measured-provenance notes, for precedent on treating a copied number
	// as a real defect class).
	inspectBusyRetryAfterSeconds = 3

	// inspectBusyMessage is the single body text every inspectSemaphore
	// rejection returns. It names the SERVICE's state, never the client's,
	// and carries no number and no variable/env-var name -- the identical
	// non-leaking discipline the quota-concurrency 429 already applies
	// (handlers.go, T-52-24): a client that could read its own configured
	// ceiling off the rejection text could binary-search concurrent
	// requests to discover it.
	inspectBusyMessage = "service is at its concurrent inspect-parsing ceiling right now"
)

// inspectSemaphore bounds how many synchronous inspect requests may be
// PARSING untrusted bytes at once (58-CONTEXT.md D-06 mechanism 2,
// T-58-30). It is a plain buffered channel -- the semaphore package under
// golang.org's extended-sync module tree is deliberately NOT introduced
// (D-01/D-02): stdlib already expresses this bound in one line, and this
// file's go.mod/go.sum stay untouched.
type inspectSemaphore struct {
	sem chan struct{}
}

// newInspectSemaphore builds a semaphore with room for maxConcurrent
// concurrent holders. maxConcurrent <= 0 substitutes
// defaultInspectMaxConcurrent rather than building a semaphore that admits
// nothing -- the same "zero means unconfigured, never zero allowed"
// discipline internal/inspect.Budget.withDefaults already applies to the
// work ceiling (see this project's D-16 inversion discipline: skipping this
// substitution is exactly the kind of regression an inversion test is
// meant to catch).
func newInspectSemaphore(maxConcurrent int) *inspectSemaphore {
	if maxConcurrent <= 0 {
		maxConcurrent = defaultInspectMaxConcurrent
	}
	return &inspectSemaphore{sem: make(chan struct{}, maxConcurrent)}
}

// acquireOrReject attempts, WITHOUT BLOCKING, to take one slot. On success
// it returns a release func the caller must defer (or call directly on
// every exit path) and ok==true. On failure -- the semaphore already at
// capacity -- it writes the SAME 429 errcode.InspectBusy response
// middleware below writes (Retry-After header included) and returns
// ok==false; the caller must return immediately without writing anything
// further.
//
// This exists so the slot can be acquired at a point OTHER than "wraps an
// entire http.Handler" (middleware below still does exactly that, for
// every existing caller that wants it): T-SLF-02 (PR #9 finding at
// inspectgate.go:120, ORIGINALLY documented on middleware's own doc
// comment as a property that must not be refactored away) is that the
// synchronous inspect handler used to acquire this slot BEFORE reading its
// own request body, so four authenticated clients trickling a multipart
// body arbitrarily slowly could hold every slot without ever doing any
// parsing -- turning the defense against a burst into a burst vector
// itself. handleInspect (internal/api/inspect_handlers.go) now calls THIS
// method directly, after its own body intake completes, instead of being
// wrapped by middleware below.
//
// The D-06a property that made "wrap the whole handler" attractive in the
// first place -- a held slot bounds concurrent Repo.PreAdmit calls against
// the shared *pgxpool.Pool, protecting GET /healthz's own Postgres ping --
// is UNCHANGED by this move: PreAdmit still runs while whatever slot this
// method returned is held, exactly like before. Only the body-transfer
// portion of the request, which never touches the pool or any CPU-bound
// parser, is no longer covered -- and covering it bought nothing but the
// slowloris exposure this fix closes.
func (g *inspectSemaphore) acquireOrReject(w http.ResponseWriter) (release func(), ok bool) {
	select {
	case g.sem <- struct{}{}:
		return func() { <-g.sem }, true
	default:
		w.Header().Set("Retry-After", strconv.Itoa(inspectBusyRetryAfterSeconds))
		writeError(w, http.StatusTooManyRequests, errcode.InspectBusy, inspectBusyMessage)
		return nil, false
	}
}

// middleware wraps next with the semaphore, via acquireOrReject above.
//
// Capture is a NON-BLOCKING select with a default branch: a full semaphore
// returns 429 immediately -- in the tens of milliseconds it takes to run
// this select and write the response -- it never queues the request.
// Queuing a pathological burst behind a full semaphore would turn the
// defense itself into an additional DoS vector (accumulating goroutines
// and open connections while callers wait); an immediate, honest 429 tells
// the caller the truth right away instead (T-58-30, 58-CONTEXT.md D-06).
//
// This method is no longer mounted ahead of POST /v1/inspect (routes.go) --
// see acquireOrReject's own doc comment above for why the acquisition
// point moved INSIDE handleInspect, after body intake. middleware is kept,
// unchanged in behavior, because it remains the simplest correct shape for
// a caller that genuinely wants "wrap this whole handler" (every test in
// inspectgate_test.go exercises exactly that shape, in isolation from
// handleInspect, and continues to pass unmodified).
//
// Release is via `defer`, placed BEFORE next.ServeHTTP runs, so the slot
// frees on every exit path from next.ServeHTTP: a normal return, a
// recovered panic (chi's middleware.Recoverer runs OUTSIDE this middleware
// in Routes(), ahead of it in the chain, exactly like the general per-
// client rate limiter today), or the underlying handler returning early
// because it observed the client disconnect via r.Context() being
// canceled. In every one of those cases next.ServeHTTP eventually returns
// (however it returns), and `defer` guarantees this middleware's own
// release code runs before this call frame unwinds further -- release
// never depends on any SPECIFIC one of those exit paths, only on
// next.ServeHTTP returning at all.
func (g *inspectSemaphore) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		release, ok := g.acquireOrReject(w)
		if !ok {
			return
		}
		defer release()
		next.ServeHTTP(w, r)
	})
}

// newInspectRateLimit returns chi middleware enforcing rpm requests per
// minute against inspect traffic ONLY, keyed by the exact same
// authenticated-client identity ratelimit.PerClient already uses
// (internal/ratelimit's unexported clientKey, reached ONLY through
// ratelimit.PerClient -- this file never re-reads the resolved client from
// the request context a second time, so there remains exactly one place
// in this codebase that does so for rate-limiting purposes).
//
// Calling ratelimit.PerClient(rpm) here, with an rpm value that can differ
// from the general per-client limiter's own, is what gives inspect its
// OWN, independent rate budget (58-CONTEXT.md D-06 mechanism 4):
// httprate.Limit -- which ratelimit.PerClient wraps -- allocates a
// brand-new *httprate.RateLimiter, and therefore a brand-new internal
// counter, on EVERY call
// (github.com/go-chi/httprate@v0.15.0's Limit -> NewRateLimiter). Two
// separate calls to ratelimit.PerClient -- one in Routes() for the general
// limit, one here for inspect -- are therefore two genuinely independent
// pieces of state from the moment each is constructed: exhausting one
// budget structurally cannot touch the other's counter, no coordination
// code required. This is why no new export (PerClientNamed or otherwise)
// was added to internal/ratelimit: the existing exported constructor,
// called twice with two different rpm values, already produces two
// independent limiters.
//
// D-07 CAVEAT (58-CONTEXT.md) -- recorded HERE, at the mechanism, not only
// in planning docs, because that is the entire point of D-07: this budget
// is in-process and in-memory, exactly like the rest of internal/ratelimit
// (see that package's own doc comment, ratelimit.go:8-13). While
// api.replicas == 1 (deploy/chart/octoconv/values.yaml:33, true as of this
// writing) this budget is a real, service-wide limit. The moment
// api.replicas > 1, this state stops being shared across replicas and the
// EFFECTIVE budget silently becomes rpm times the replica count -- with no
// compile error and no deploy-time warning. This is the DIRECT OPPOSITE of
// the quota mechanism in internal/jobs/admission.go, which Phase 55 proved
// multireplica-safe with a live gate; the asymmetry is accepted
// deliberately here, not inherited silently. The upgrade path, already
// named by internal/ratelimit's own doc comment, is httprate's
// WithLimitCounter option backed by a Redis-based counter -- not a
// rewrite of this call site. The live gate this phase ships (Plan 58-08)
// is required to assert api.replicas == 1 explicitly, so that a future
// change to that value forces a reader back to this exact paragraph.
func newInspectRateLimit(rpm int) func(http.Handler) http.Handler {
	return ratelimit.PerClient(rpm)
}
