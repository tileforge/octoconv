// This file's package doc comment lives in findings.go; this file adds the
// D-06 mechanism-1 work-ceiling primitive.
package inspect

import (
	"sync"
	"time"
)

// Budget configures ONE Tracker: the byte and time ceiling a single
// inspect operation may spend across every family analyzer that shares it
// (58-CONTEXT.md D-06 mechanism 1). A ZERO field means "not configured,
// substitute the documented default" -- withDefaults performs that
// substitution, and NewTracker always calls it, so a caller can never end
// up with an accidentally-zero, always-exhausted Tracker. This mirrors the
// exact defaulting discipline api.NewServer already applies to
// PresignTTL/MaxUploadBytes (internal/api/api.go:305-372): treating a
// missing/zero-valued tuning knob as "zero allowed" would turn a skipped
// environment variable into a silent full-endpoint outage, which is the
// opposite of what a calibratable ceiling is for.
//
// Both defaults below are STARTING values for the live gate (Plan 58-08,
// SC6) to calibrate, not measured numbers -- this project treats copying a
// number from an unrelated engine class as a documented anti-pattern (see
// .env.example's AUDIO_ENGINE_TIMEOUT/OCR_ENGINE_TIMEOUT measured-
// provenance comments), and inventing one with no calibration plan is no
// better than copying. По-русски, как и в 58-CONTEXT.md: оба значения
// СТАРТОВЫЕ и подлежат калибровке живым гейтом SC6 (план 58-08), а не
// измерены заранее.
type Budget struct {
	// MaxBytes is the total number of bytes every family analyzer sharing
	// this Tracker may read across the whole inspect operation. Zero means
	// "use the default", never "zero bytes allowed".
	MaxBytes int64
	// MaxDuration is the wall-clock ceiling for the same operation. Zero
	// means "use the default". This is a COURTESY outer bound, not the
	// load-bearing mechanism -- see Tracker's own doc comment for why.
	MaxDuration time.Duration
}

const (
	// defaultMaxBytes is Budget's starting byte ceiling: 8 MiB. SC6-calibrated,
	// not measured.
	defaultMaxBytes int64 = 8 << 20
	// defaultMaxDuration is Budget's starting time ceiling: 2s. SC6-calibrated,
	// not measured.
	defaultMaxDuration = 2 * time.Second
)

// withDefaults returns a copy of b with every zero field replaced by its
// documented default. It never mutates b -- callers always use the
// returned value.
func (b Budget) withDefaults() Budget {
	if b.MaxBytes == 0 {
		b.MaxBytes = defaultMaxBytes
	}
	if b.MaxDuration == 0 {
		b.MaxDuration = defaultMaxDuration
	}
	return b
}

// Tracker is a thread-safe accountant for ONE inspect operation's Budget.
// It sits ABOVE the hard iteration/box/entry caps that already live INSIDE
// each family parser (this package's own window.go: maxTokenScanIterations,
// and the per-format ceilings 58-04/58-05 add on top of it): those caps
// bound ONE parser's own loop, and they exist because a context deadline
// does NOT interrupt a CPU-bound loop that never calls back into the
// scheduler or checks ctx.Err() (58-RESEARCH.md Q4 -- Go's async goroutine
// preemption keeps OTHER goroutines, including /healthz's own handler,
// running, but it does not make THIS loop stop). Tracker is the SUM across
// every family analyzer one inspect call invokes, so a dozen individually-
// bounded parsers cannot silently add up to one operation with no overall
// ceiling. Neither layer replaces the other; both ship together.
//
// context, wherever a caller wires one in on top of this (Plan 58-07),
// remains a courtesy OUTER bound only: it guarantees a held semaphore slot
// (internal/api/inspectgate.go, D-06 mechanism 2) eventually releases even
// if a bug in a Tracker caller or a family parser slips past both of the
// above, but it is never the primary mechanism, for the identical reason
// stated above -- a context deadline cannot stop a tight loop that never
// checks it.
//
// A Tracker may be shared across several family analyzers processing the
// SAME upload, sequentially or concurrently -- Consume is safe for
// concurrent callers because every field it touches is guarded by mu, not
// because of any single-goroutine assumption.
type Tracker struct {
	mu sync.Mutex

	budget    Budget
	start     time.Time
	consumed  int64
	exhausted Reason
	// ceilingHit is the quick/260811-r3s receptacle: set directly by a
	// scanner, from wherever it detects that its OWN local resource ceiling
	// (an iteration/box/entry-count cap living INSIDE a family parser --
	// window.go's maxTokenScanIterations, media.go's maxISOBMFFBoxes/
	// maxEBMLElements/maxRIFFChunks, container.go's maxRelsPartBytes, ...)
	// fired, NOT the byte/time budget Consume enforces above. See
	// MarkCeilingHit's own doc comment for why this is a SEPARATE field
	// from exhausted rather than reusing it.
	ceilingHit bool
	checkedAt  time.Time
	longestGap time.Duration
}

// NewTracker builds a Tracker for one inspect operation. b's zero fields
// are replaced by Budget's documented defaults (withDefaults) before
// anything else happens -- a caller that leaves MaxBytes/MaxDuration unset
// gets the default ceiling, never a zero-byte, already-exhausted Tracker.
func NewTracker(b Budget) *Tracker {
	return &Tracker{
		budget: b.withDefaults(),
		start:  time.Now(),
	}
}

// Consume records that n more bytes were just read by some family analyzer
// sharing this Tracker. It returns true when the read fits inside both
// ceilings, false the instant either is (or was already) exceeded.
//
// Time is checked BEFORE bytes on every call: the time ceiling is the one
// most likely to be crossed BETWEEN two Consume calls (an analyzer doing
// expensive per-iteration work between reads), whereas the byte ceiling can
// only ever be crossed AT a Consume call itself -- checking time first
// means a call that arrives after MaxDuration has already elapsed is
// correctly attributed to the time ceiling even if it would also have
// fit under MaxBytes.
//
// The FIRST exhaustion reason observed is latched for the Tracker's
// lifetime: once exhausted is non-empty, every subsequent Consume call
// short-circuits to false without re-evaluating either ceiling, so a
// second family analyzer that has not yet observed Exhausted() cannot
// silently overwrite the original cause. This mirrors Coverage.
// MarkTruncated's own first-reason-wins rule in status.go -- the fact
// worth keeping when several analyzers are truncated by the SAME exhausted
// budget is where it actually ran out, not which analyzer happened to call
// Consume last.
//
// n <= 0 is a no-op that returns true without consulting or mutating any
// state: there is nothing to account for.
func (t *Tracker) Consume(n int64) bool {
	if n <= 0 {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.exhausted != "" {
		return false
	}
	if time.Since(t.start) > t.budget.MaxDuration {
		t.exhausted = ReasonWorkCeilingTime
		return false
	}
	if t.consumed+n > t.budget.MaxBytes {
		t.exhausted = ReasonWorkCeilingBytes
		return false
	}
	t.consumed += n
	return true
}

// MarkCeilingHit is quick/260811-r3s's receptacle write: it records,
// DIRECTLY on this operation's shared Tracker, that some scanner --
// possibly several call frames deep inside a family analyzer -- stopped
// work early because of its OWN local resource ceiling (see this type's
// own doc comment for the distinction from Consume's byte/time budget).
// Once marked, ExhaustedReason/Exhausted report a ceiling for the rest of
// this Tracker's lifetime, and BuildReport's Coverage claim can never come
// back Complete:true for this operation -- regardless of what any
// individual Check or characteristic's own Reason field ends up saying.
//
// This is the mechanism that makes the class of defect quick/260811-r3s
// exists to close structurally impossible: three review rounds each found
// a caller several layers up that forgot to thread a local ceilingHit
// signal into a Check.Reason or a characteristic's "<name>_reason" key,
// silently leaving Coverage.Complete:true on a selective report. Coverage
// no longer depends on any one caller getting that threading right --
// MarkCeilingHit is called at the exact point each scanner detects its own
// ceiling, and BuildReport reads the union of every scanner's marks
// directly, with nothing for an intermediate caller to drop.
//
// A SEPARATE field from exhausted (Consume's own byte/time signal) rather
// than reusing it: reusing exhausted would make Consume's own
// "time-checked-before-bytes, first-reason-wins" contract ambiguous about
// which of two entirely different ceilings actually fired first, and would
// also make a local ceiling hit short-circuit every SUBSEQUENT Consume call
// for the rest of the operation (Consume's own first line: "if
// t.exhausted != \"\" { return false }") -- a side effect this quick task's
// design explicitly does not require and that could change existing
// Consume-driven truncation behavior in ways no test here exercises.
// ExhaustedReason/Exhausted below read BOTH fields, so the observable
// contract (a report-wide "is this operation's own ceiling exhausted"
// answer) is a strict superset of what it was before, without touching
// Consume's own byte/time semantics at all.
//
// Every local ceiling this package currently detects maps to
// ReasonWorkCeilingBytes -- exactly the existing convention every scanner
// already uses when translating its own local ceilingHit into a
// Check/characteristic Reason (e.g. extractXLSXSheetCount, scanJPEGMetadata)
// -- so MarkCeilingHit does not take a Reason parameter; it always means
// "the same kind of thing Consume's own byte ceiling means", just detected
// somewhere Consume itself was never called.
//
// A nil receiver is a safe no-op: every family analyzer in this package
// accepts a possibly-nil *Tracker (a caller/test exercising one family in
// isolation, exactly like BuildReport's own nil-tolerant tr), and a scanner
// several calls deep must be free to call tr.MarkCeilingHit() unconditionally
// without every call site re-checking tr != nil first.
func (t *Tracker) MarkCeilingHit() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ceilingHit = true
}

// ExhaustedReason returns the first ceiling this Tracker hit --
// ReasonWorkCeilingBytes or ReasonWorkCeilingTime from Consume's own
// byte/time budget, or ReasonWorkCeilingBytes if only MarkCeilingHit's
// receptacle was marked (see MarkCeilingHit's own doc comment for why a
// local ceiling always reports as the bytes reason) -- or "" if neither has
// ever fired. Consume's own exhausted field is checked first: if the shared
// byte/time budget already has a reason latched, that is definitionally the
// FIRST ceiling this Tracker hit in wall-clock terms among the two fields,
// consistent with Consume's own first-reason-wins contract.
func (t *Tracker) ExhaustedReason() Reason {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.exhausted != "" {
		return t.exhausted
	}
	if t.ceilingHit {
		return ReasonWorkCeilingBytes
	}
	return ""
}

func (t *Tracker) OutOfTime() bool {
	if t == nil || t.start.IsZero() {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	last := t.checkedAt
	if last.IsZero() {
		last = t.start
	}
	if gap := now.Sub(last); gap > t.longestGap {
		t.longestGap = gap
	}
	t.checkedAt = now
	if now.Sub(t.start) <= t.budget.MaxDuration {
		return false
	}
	if t.exhausted == "" {
		t.exhausted = ReasonWorkCeilingTime
	}
	return true
}

func (t *Tracker) ceilingReason() Reason {
	if t != nil && t.ExhaustedReason() == ReasonWorkCeilingTime {
		return ReasonWorkCeilingTime
	}
	return ReasonWorkCeilingBytes
}

const pdfTimeCheckEvery = 1024

// Exhausted reports whether either ceiling has been hit.
func (t *Tracker) Exhausted() bool {
	return t.ExhaustedReason() != ""
}

// Remaining returns the number of bytes still available under MaxBytes,
// never negative. Consume never increments consumed on the call that trips
// the byte ceiling (it returns false first), so consumed never actually
// exceeds MaxBytes in practice -- the < 0 clamp exists anyway so this
// method's contract ("never negative") holds unconditionally, without every
// caller needing to know that invariant to rely on it.
func (t *Tracker) Remaining() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	remaining := t.budget.MaxBytes - t.consumed
	if remaining < 0 {
		return 0
	}
	return remaining
}
