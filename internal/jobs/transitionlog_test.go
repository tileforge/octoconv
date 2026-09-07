package jobs

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Every status change in this service goes through Repo.transition, and the
// lifecycle record is emitted there rather than at the callers.
//
// It was at the callers first, and that meant four places to remember. Three
// were already missing when review looked (PR #23): a cancellation of a job
// still QUEUED, an operator cancellation through manage-clients, and a job the
// reconciler marked failed after exhausting recoveries. Each ended with no
// record at all, so the chain that starts at admission simply stopped.
//
// These tests drive the real transitions against the real database, because
// the thing under test is what the LOCKED read sees -- a fake repository has
// no lock and no row.

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	// log.Writer(), never log.SetOutput(nil): nil is a nil io.Writer and the
	// next log call panics inside log.output.
	t.Cleanup(func() { log.SetOutput(prev) })
	return &buf
}

func recordFor(t *testing.T, buf *bytes.Buffer, event string) string {
	t.Helper()
	var line string
	for _, l := range strings.Split(buf.String(), "\n") {
		if strings.Contains(l, event) {
			line = l
		}
	}
	return line
}

// A cancellation of a job that never reaches a worker: the case that had no
// record at all when the logging lived in the worker.
func TestQueuedCancellationIsRecorded(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	id, err := r.Create(ctx, CreateParams{
		ClientID:  createTestClient(t, r),
		Operation: "convert", Engine: "image", SourceFormat: "png", TargetFormat: "webp",
		CorrelationID: "svc-x/req-queued-cancel",
		Input:         Input{ObjectKey: "uploads/qc/0-in.png", Filename: "in.png", Format: "png"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	buf := captureLog(t)
	if err := r.Cancel(ctx, id); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	line := recordFor(t, buf, "job canceled")
	if line == "" {
		t.Fatalf("a queued cancellation left no record; the log chain stops at admission.\nlog:\n%s",
			buf.String())
	}
	for _, want := range []string{
		id.String(),
		"correlation_id=svc-x/req-queued-cancel",
		"engine=image",
		"from_status=queued",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("the record lacks %s: %s", want, line)
		}
	}
}

// from_status is read UNDER THE LOCK, not from whatever the caller saw
// earlier. A queued job that a worker activates between the caller's read and
// the transition must be recorded as it actually was (codex, PR #23).
func TestFromStatusComesFromTheLockedRead(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	id, err := r.Create(ctx, CreateParams{
		ClientID:  createTestClient(t, r),
		Operation: "convert", Engine: "image", SourceFormat: "png", TargetFormat: "webp",
		Input: Input{ObjectKey: "uploads/lr/0-in.png", Filename: "in.png", Format: "png"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// The race, made deterministic: the job is ACTIVE by the time the
	// transition takes its lock, whatever any earlier read reported.
	if err := r.MarkActive(ctx, id); err != nil {
		t.Fatalf("MarkActive: %v", err)
	}

	buf := captureLog(t)
	if err := r.Cancel(ctx, id); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	line := recordFor(t, buf, "job canceled")
	if !strings.Contains(line, "from_status=active") {
		t.Fatalf("from_status is not what the transition locked. A caller that read the job while "+
			"it was queued would have this record claim queued.\ngot: %s", line)
	}
}

// And a failure recorded by anything that calls MarkFailed -- the worker, or
// the reconciler exhausting its recoveries -- produces the same record without
// that caller having to remember.
func TestMarkFailedIsRecordedWhoeverCallsIt(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	id, err := r.Create(ctx, CreateParams{
		ClientID:  createTestClient(t, r),
		Operation: "convert", Engine: "image", SourceFormat: "png", TargetFormat: "webp",
		Input: Input{ObjectKey: "uploads/mf/0-in.png", Filename: "in.png", Format: "png"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	buf := captureLog(t)
	// The reconciler's shape: MarkFailed straight from a queued job, never
	// touching Runner.finish.
	if err := r.MarkFailed(ctx, id, "reconciler_exhausted", "recovery attempts exhausted", nil, 0); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	line := recordFor(t, buf, "job failed")
	if line == "" {
		t.Fatalf("a job failed by the reconciler left no record.\nlog:\n%s", buf.String())
	}
	if !strings.Contains(line, "from_status=queued") || !strings.Contains(line, id.String()) {
		t.Errorf("the record is incomplete: %s", line)
	}
}

// Centralising the record dropped the two fields the phase existed to provide
// (codex, PR #23): without `code` an operator cannot tell engine_error from
// invalid_options from reconciler_exhausted, and without duration_ms
// completion latency means going back to Postgres.
//
// A refactor that keeps every test passing and silently narrows what the logs
// say is exactly the failure this table is here to catch.
func TestFailureRecordCarriesTheOutcome(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	id, err := r.Create(ctx, CreateParams{
		ClientID:  createTestClient(t, r),
		Operation: "convert", Engine: "image", SourceFormat: "png", TargetFormat: "webp",
		Input: Input{ObjectKey: "uploads/oc/0-in.png", Filename: "in.png", Format: "png"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	buf := captureLog(t)
	if err := r.MarkFailed(ctx, id, "engine_error", "boom", nil, 1234*time.Millisecond); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	line := recordFor(t, buf, "job failed")
	for _, want := range []string{"code=engine_error", "duration_ms=1234"} {
		if !strings.Contains(line, want) {
			t.Errorf("the failure record lacks %s, so the logs cannot distinguish one failure "+
				"class from another: %s", want, line)
		}
	}
}

// And the fields are OMITTED where they would be empty: a cancellation has no
// error code, and a field that is usually blank trains readers to ignore it.
func TestCancellationRecordHasNoEmptyOutcomeFields(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	id, err := r.Create(ctx, CreateParams{
		ClientID:  createTestClient(t, r),
		Operation: "convert", Engine: "image", SourceFormat: "png", TargetFormat: "webp",
		Input: Input{ObjectKey: "uploads/nc/0-in.png", Filename: "in.png", Format: "png"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	buf := captureLog(t)
	if err := r.Cancel(ctx, id); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	line := recordFor(t, buf, "job canceled")
	for _, unwanted := range []string{`code=""`, `duration_ms=0`} {
		if strings.Contains(line, unwanted) {
			t.Errorf("the cancellation record carries %s, which means nothing here: %s", unwanted, line)
		}
	}
}

// A redelivered task transitions active -> active (MarkActive allows it), so
// it writes a second record. That is correct and worth pinning rather than
// deduplicating: from_status distinguishes the two, so the log shows a
// REDELIVERY rather than a second job.
//
// Checked because centralising the record made it fire on every transition,
// including this one -- a change in volume is a change an operator lives with,
// and it should be one somebody decided rather than discovered.
func TestRedeliveryIsVisibleAsRedelivery(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	id, err := r.Create(ctx, CreateParams{
		ClientID:  createTestClient(t, r),
		Operation: "convert", Engine: "image", SourceFormat: "png", TargetFormat: "webp",
		Input: Input{ObjectKey: "uploads/rd/0-in.png", Filename: "in.png", Format: "png"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	buf := captureLog(t)
	if err := r.MarkActive(ctx, id); err != nil {
		t.Fatalf("first MarkActive: %v", err)
	}
	if err := r.MarkActive(ctx, id); err != nil {
		t.Fatalf("second MarkActive: %v", err)
	}

	var froms []string
	for _, l := range strings.Split(buf.String(), "\n") {
		if !strings.Contains(l, "job active") {
			continue
		}
		switch {
		case strings.Contains(l, "from_status=queued"):
			froms = append(froms, "queued")
		case strings.Contains(l, "from_status=active"):
			froms = append(froms, "active")
		}
	}
	if len(froms) != 2 || froms[0] != "queued" || froms[1] != "active" {
		t.Fatalf("want a queued->active record then an active->active one, so a redelivery reads "+
			"as a redelivery rather than as a second job; got %v\nlog:\n%s", froms, buf.String())
	}
}

// Every TERMINAL record carries a duration -- including the ones where it is
// genuinely zero, and including a cancellation, where the number exists only
// in the locked row (codex, PR #23).
//
// The first version of this treated zero as "not applicable" and omitted the
// field, so an instant invalid_options failure and a cancellation of a
// long-running job both reported nothing.
func TestTerminalRecordsAlwaysCarryADuration(t *testing.T) {
	newJob := func(t *testing.T, r *Repo, key string) uuid.UUID {
		t.Helper()
		id, err := r.Create(context.Background(), CreateParams{
			ClientID:  createTestClient(t, r),
			Operation: "convert", Engine: "image", SourceFormat: "png", TargetFormat: "webp",
			Input: Input{ObjectKey: "uploads/" + key + "/0-in.png", Filename: "in.png", Format: "png"},
		})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		return id
	}

	t.Run("a zero-duration failure still reports it", func(t *testing.T) {
		r := newTestRepo(t)
		id := newJob(t, r, "zd")
		buf := captureLog(t)
		// The reconciler's shape exactly: no attempt clock at all.
		if err := r.MarkFailed(context.Background(), id, "reconciler_exhausted", "exhausted", nil, 0); err != nil {
			t.Fatalf("MarkFailed: %v", err)
		}
		line := recordFor(t, buf, "job failed")
		if !strings.Contains(line, "duration_ms=0") {
			t.Fatalf("an instant failure reports no duration, so it cannot be told from a "+
				"transition that has no notion of one: %s", line)
		}
	})

	t.Run("cancelling an active job derives it from the row", func(t *testing.T) {
		r := newTestRepo(t)
		ctx := context.Background()
		id := newJob(t, r, "ca")
		if err := r.MarkActive(ctx, id); err != nil {
			t.Fatalf("MarkActive: %v", err)
		}
		buf := captureLog(t)
		if err := r.Cancel(ctx, id); err != nil {
			t.Fatalf("Cancel: %v", err)
		}
		line := recordFor(t, buf, "job canceled")
		if !strings.Contains(line, "duration_ms=") {
			t.Fatalf("a cancellation of a job that had been RUNNING reports no duration, though "+
				"started_at is right there in the locked row: %s", line)
		}
	})

	t.Run("cancelling a queued job has none to report", func(t *testing.T) {
		r := newTestRepo(t)
		id := newJob(t, r, "cq2")
		buf := captureLog(t)
		if err := r.Cancel(context.Background(), id); err != nil {
			t.Fatalf("Cancel: %v", err)
		}
		line := recordFor(t, buf, "job canceled")
		if strings.Contains(line, "duration_ms=") {
			t.Fatalf("a job that never started reports a duration, which is a number invented "+
				"rather than measured: %s", line)
		}
	})
}
