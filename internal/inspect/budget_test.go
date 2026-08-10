package inspect

import (
	"sync"
	"testing"
	"time"
)

// TestTrackerConsumeSequence covers the basic behavior block: Consume(60)
// then Consume(60) against a 100-byte budget.
func TestTrackerConsumeSequence(t *testing.T) {
	tr := NewTracker(Budget{MaxBytes: 100, MaxDuration: time.Minute})
	if !tr.Consume(60) {
		t.Fatal("first Consume(60) = false, want true")
	}
	if tr.Consume(60) {
		t.Fatal("second Consume(60) = true, want false (100-byte ceiling exceeded)")
	}
}

// TestTrackerExhaustedReasonBytes: after the byte ceiling trips,
// ExhaustedReason must report ReasonWorkCeilingBytes.
func TestTrackerExhaustedReasonBytes(t *testing.T) {
	tr := NewTracker(Budget{MaxBytes: 100, MaxDuration: time.Minute})
	tr.Consume(60)
	tr.Consume(60)
	if got := tr.ExhaustedReason(); got != ReasonWorkCeilingBytes {
		t.Fatalf("ExhaustedReason() = %q, want %q", got, ReasonWorkCeilingBytes)
	}
	if !tr.Exhausted() {
		t.Error("Exhausted() = false, want true")
	}
}

// TestTrackerExhaustedReasonTime: after MaxDuration elapses, the next
// Consume returns false and ExhaustedReason reports ReasonWorkCeilingTime.
func TestTrackerExhaustedReasonTime(t *testing.T) {
	tr := NewTracker(Budget{MaxBytes: 1 << 30, MaxDuration: 20 * time.Millisecond})
	time.Sleep(30 * time.Millisecond)
	if tr.Consume(1) {
		t.Fatal("Consume(1) = true after MaxDuration elapsed, want false")
	}
	if got := tr.ExhaustedReason(); got != ReasonWorkCeilingTime {
		t.Fatalf("ExhaustedReason() = %q, want %q", got, ReasonWorkCeilingTime)
	}
}

// TestTrackerKeepsFirstExhaustionReason: when both the byte AND time
// ceilings are (or become) exhausted, ExhaustedReason must report the
// FIRST one that actually occurred, never the last.
func TestTrackerKeepsFirstExhaustionReason(t *testing.T) {
	tr := NewTracker(Budget{MaxBytes: 10, MaxDuration: 30 * time.Millisecond})

	// Trip the byte ceiling first, well before MaxDuration elapses.
	if tr.Consume(20) {
		t.Fatal("Consume(20) against a 10-byte budget = true, want false")
	}
	if got := tr.ExhaustedReason(); got != ReasonWorkCeilingBytes {
		t.Fatalf("ExhaustedReason() after byte trip = %q, want %q", got, ReasonWorkCeilingBytes)
	}

	// Now let MaxDuration elapse too and call Consume again -- the reason
	// must NOT flip to ReasonWorkCeilingTime; the first cause is latched.
	time.Sleep(40 * time.Millisecond)
	if tr.Consume(1) {
		t.Fatal("Consume(1) on an already-exhausted Tracker = true, want false")
	}
	if got := tr.ExhaustedReason(); got != ReasonWorkCeilingBytes {
		t.Fatalf("ExhaustedReason() after time also elapsed = %q, want %q (first reason must stick)", got, ReasonWorkCeilingBytes)
	}
}

// TestTrackerRemainingNeverNegative: Remaining must never report a
// negative value, even right at or past the byte ceiling.
func TestTrackerRemainingNeverNegative(t *testing.T) {
	tr := NewTracker(Budget{MaxBytes: 10, MaxDuration: time.Minute})
	if got := tr.Remaining(); got != 10 {
		t.Fatalf("Remaining() before any Consume = %d, want 10", got)
	}
	tr.Consume(10)
	if got := tr.Remaining(); got != 0 {
		t.Fatalf("Remaining() at exactly the ceiling = %d, want 0", got)
	}
	tr.Consume(1) // exceeds; must not go negative
	if got := tr.Remaining(); got < 0 {
		t.Fatalf("Remaining() = %d, want >= 0", got)
	}
}

// TestBudgetZeroMeansDefaultNotZeroAllowed: a zero-valued Budget must NOT
// behave like "zero bytes allowed" -- it must substitute the documented
// defaults, so a Tracker built from it accepts a small Consume call.
func TestBudgetZeroMeansDefaultNotZeroAllowed(t *testing.T) {
	tr := NewTracker(Budget{})
	if !tr.Consume(1) {
		t.Fatal("Consume(1) on a zero-valued Budget = false, want true (zero must mean 'use the default', not 'zero bytes allowed')")
	}
	if tr.Exhausted() {
		t.Fatal("Exhausted() = true immediately after NewTracker(Budget{}), want false")
	}
}

// TestTrackerConsumeNonPositiveNoop: Consume(n) for n <= 0 is a no-op that
// returns true and does not mutate any state.
func TestTrackerConsumeNonPositiveNoop(t *testing.T) {
	tr := NewTracker(Budget{MaxBytes: 100, MaxDuration: time.Minute})
	if !tr.Consume(0) {
		t.Fatal("Consume(0) = false, want true")
	}
	if !tr.Consume(-5) {
		t.Fatal("Consume(-5) = false, want true")
	}
	if got := tr.Remaining(); got != 100 {
		t.Fatalf("Remaining() after only non-positive Consume calls = %d, want 100 (unchanged)", got)
	}
	if tr.Exhausted() {
		t.Fatal("Exhausted() = true after only non-positive Consume calls, want false")
	}
}

// TestTrackerConcurrent exercises Consume from multiple goroutines sharing
// ONE Tracker -- the shape a single inspect operation invoking several
// family analyzers concurrently would produce. Run under -race; must not
// panic or report a data race, and must never let consumed exceed
// MaxBytes by more than one goroutine's single increment.
func TestTrackerConcurrent(t *testing.T) {
	const (
		goroutines      = 20
		perGoroutine    = 50
		bytesPerConsume = 7
	)
	tr := NewTracker(Budget{MaxBytes: 1 << 20, MaxDuration: time.Minute})

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				tr.Consume(bytesPerConsume)
			}
		}()
	}
	wg.Wait()

	if tr.Exhausted() {
		t.Fatal("Tracker unexpectedly exhausted under a budget far larger than total consumption")
	}
	want := int64(goroutines * perGoroutine * bytesPerConsume)
	if got := tr.Remaining(); got != (1<<20)-want {
		t.Fatalf("Remaining() = %d, want %d (all %d concurrent Consume calls must be accounted for exactly once)", got, (1<<20)-want, goroutines*perGoroutine)
	}
}
