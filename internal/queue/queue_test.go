package queue

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/tileforge/octoconv/internal/convert"
)

func TestConvertPayloadRoundTrip(t *testing.T) {
	id := uuid.New()
	task, err := NewDocumentConvertTask(id, 3, DocumentUniqueTTL(3, 300*time.Second))
	if err != nil {
		t.Fatalf("NewDocumentConvertTask: %v", err)
	}
	if task.Type() != TypeDocumentConvert {
		t.Errorf("task type = %q, want %q", task.Type(), TypeDocumentConvert)
	}
	p, err := ParseConvertPayload(task.Payload())
	if err != nil {
		t.Fatalf("ParseConvertPayload: %v", err)
	}
	if p.JobID != id {
		t.Errorf("job id = %s, want %s", p.JobID, id)
	}
}

// TestWebhookRetryDelaySchedule asserts WebhookRetryDelay indexes
// webhookRetrySchedule with asynq's 0-based retry count (msg.Retried),
// within the ±25% jitter band, and clamps at the schedule's last entry.
func TestWebhookRetryDelaySchedule(t *testing.T) {
	schedule := []time.Duration{
		30 * time.Second,
		1 * time.Minute,
		2 * time.Minute,
		4 * time.Minute,
		8 * time.Minute,
		15 * time.Minute,
	}
	cases := []struct {
		n    int
		base time.Duration
	}{
		{n: 0, base: schedule[0]},
		{n: 1, base: schedule[1]},
		{n: 5, base: schedule[5]},
		{n: 6, base: schedule[5]}, // clamps past the end of the schedule
		{n: 100, base: schedule[5]},
	}
	for _, tc := range cases {
		delay := WebhookRetryDelay(tc.n, nil, nil)
		lo := time.Duration(float64(tc.base) * 0.75)
		hi := time.Duration(float64(tc.base) * 1.25)
		if delay < lo || delay > hi {
			t.Errorf("WebhookRetryDelay(%d) = %v, want within [%v, %v] (base %v)", tc.n, delay, lo, hi, tc.base)
		}
	}
}

// TestRetryDelayFuncDispatch asserts RetryDelayFunc routes each task type to
// its own retry delay function: webhook tasks stay within the existing ±25%
// jitter band, and an unknown task type falls back to asynq's own default.
func TestRetryDelayFuncDispatch(t *testing.T) {
	webhookTask := asynq.NewTask(TypeWebhookDeliver, nil)
	documentTask := asynq.NewTask(TypeDocumentConvert, nil)
	otherTask := asynq.NewTask("some:other-type", nil)

	if got, want := RetryDelayFunc(0, nil, documentTask), 5*time.Second; got != want {
		t.Errorf("RetryDelayFunc(document, 0) = %v, want %v", got, want)
	}
	if got, want := RetryDelayFunc(2, nil, documentTask), 30*time.Second; got != want {
		t.Errorf("RetryDelayFunc(document, 2) = %v, want %v", got, want)
	}

	base := 30 * time.Second
	lo := time.Duration(float64(base) * 0.75)
	hi := time.Duration(float64(base) * 1.25)
	if got := RetryDelayFunc(0, nil, webhookTask); got < lo || got > hi {
		t.Errorf("RetryDelayFunc(webhook, 0) = %v, want within [%v, %v]", got, lo, hi)
	}

	// asynq.DefaultRetryDelayFunc(0, ...) = 0^4 + 15 + rand(30)*1 seconds
	// (Sidekiq formula, server.go:403) — random, so assert the dispatched
	// value falls in its known [15s, 45s) range rather than exact equality.
	got := RetryDelayFunc(0, nil, otherTask)
	if lo, hi := 15*time.Second, 45*time.Second; got < lo || got >= hi {
		t.Errorf("RetryDelayFunc(other, 0) = %v, want within [%v, %v) (asynq default)", got, lo, hi)
	}
}

// TestWebhookUniqueTTL asserts WebhookUniqueTTL derives its result from a
// jitter-inflated worst-case backoff sum (NOT from calling the jittered
// WebhookRetryDelay), evaluates to exactly 2477.5s for (6, 10s), and always
// strictly exceeds the worst-case retry lifetime, is monotonic, and is
// deterministic across repeated calls.
func TestWebhookUniqueTTL(t *testing.T) {
	maxRetry := 6
	perAttemptTimeout := 10 * time.Second
	// Jitter-inflated (x1.25) values computed directly from the raw
	// webhookRetrySchedule (30s,1m,2m,4m,8m,15m) — NOT via WebhookRetryDelay,
	// which would introduce non-determinism into this test.
	backoffSum := 37500*time.Millisecond + 75*time.Second + 150*time.Second + 300*time.Second + 600*time.Second + 1125*time.Second

	want := time.Duration(maxRetry+1)*perAttemptTimeout + backoffSum + uniqueTTLSafetyMargin
	got := WebhookUniqueTTL(maxRetry, perAttemptTimeout)
	if got != want {
		t.Errorf("WebhookUniqueTTL(%d, %v) = %v, want %v", maxRetry, perAttemptTimeout, got, want)
	}
	if want != 2477500*time.Millisecond {
		t.Errorf("expected worked example want = 2477.5s, got %v", want)
	}

	worstCaseRetryLifetime := time.Duration(maxRetry+1)*perAttemptTimeout + backoffSum
	if got <= worstCaseRetryLifetime {
		t.Errorf("WebhookUniqueTTL(%d, %v) = %v, want strictly greater than worst-case retry lifetime %v", maxRetry, perAttemptTimeout, got, worstCaseRetryLifetime)
	}

	// Monotonicity: raising either argument must never shrink the TTL.
	if WebhookUniqueTTL(maxRetry+1, perAttemptTimeout) <= WebhookUniqueTTL(maxRetry, perAttemptTimeout) {
		t.Errorf("WebhookUniqueTTL must grow monotonically with maxRetry")
	}
	if WebhookUniqueTTL(maxRetry, perAttemptTimeout+time.Second) <= WebhookUniqueTTL(maxRetry, perAttemptTimeout) {
		t.Errorf("WebhookUniqueTTL must grow monotonically with perAttemptTimeout")
	}

	// Determinism: no jittered call leaked into the derivation.
	if got2 := WebhookUniqueTTL(maxRetry, perAttemptTimeout); got2 != got {
		t.Errorf("WebhookUniqueTTL is not deterministic: %v != %v", got2, got)
	}
}

// TestDocumentConvertTaskRoundTrip asserts NewDocumentConvertTask builds a
// task routed to TypeDocumentConvert whose payload round-trips through
// ParseConvertPayload back to the same job id — mirrors
// TestConvertPayloadRoundTrip's shape for the document engine class.
func TestDocumentConvertTaskRoundTrip(t *testing.T) {
	id := uuid.New()
	task, err := NewDocumentConvertTask(id, 3, DocumentUniqueTTL(3, 300*time.Second))
	if err != nil {
		t.Fatalf("NewDocumentConvertTask: %v", err)
	}
	if task.Type() != TypeDocumentConvert {
		t.Errorf("task type = %q, want %q", task.Type(), TypeDocumentConvert)
	}
	p, err := ParseConvertPayload(task.Payload())
	if err != nil {
		t.Fatalf("ParseConvertPayload: %v", err)
	}
	if p.JobID != id {
		t.Errorf("job id = %s, want %s", p.JobID, id)
	}
}

// TestDocumentRetryDelaySchedule asserts DocumentRetryDelay returns the exact
// 5s/15s/30s schedule (no jitter, mirrors ImageRetryDelay's shape) and clamps
// on both ends.
func TestDocumentRetryDelaySchedule(t *testing.T) {
	cases := []struct {
		n    int
		want time.Duration
	}{
		{n: -1, want: 5 * time.Second}, // clamps to first entry
		{n: 0, want: 5 * time.Second},
		{n: 1, want: 15 * time.Second},
		{n: 2, want: 30 * time.Second},
		{n: 99, want: 30 * time.Second}, // clamps past the end of the schedule
	}
	for _, tc := range cases {
		got := DocumentRetryDelay(tc.n, nil, nil)
		if got != tc.want {
			t.Errorf("DocumentRetryDelay(%d) = %v, want %v", tc.n, got, tc.want)
		}
	}
}

// TestDocumentUniqueTTL asserts DocumentUniqueTTL derives its result from the
// corrected worst-case retry lifetime ((maxRetry+1) executions) exactly like
// ImageUniqueTTL, evaluates to exactly 1370s for (3, 300s), and is
// monotonic in both arguments.
func TestDocumentUniqueTTL(t *testing.T) {
	maxRetry := 3
	engineTimeout := 300 * time.Second
	backoffSum := 5*time.Second + 15*time.Second + 30*time.Second // i=0..2

	want := time.Duration(maxRetry+1)*engineTimeout + backoffSum + uniqueTTLSafetyMargin
	got := DocumentUniqueTTL(maxRetry, engineTimeout)
	if got != want {
		t.Errorf("DocumentUniqueTTL(%d, %v) = %v, want %v", maxRetry, engineTimeout, got, want)
	}
	if want != 1370*time.Second {
		t.Errorf("expected worked example want = 1370s, got %v", want)
	}

	// Monotonicity: raising either argument must never shrink the TTL.
	if DocumentUniqueTTL(maxRetry+1, engineTimeout) <= DocumentUniqueTTL(maxRetry, engineTimeout) {
		t.Errorf("DocumentUniqueTTL must grow monotonically with maxRetry")
	}
	if DocumentUniqueTTL(maxRetry, engineTimeout+time.Second) <= DocumentUniqueTTL(maxRetry, engineTimeout) {
		t.Errorf("DocumentUniqueTTL must grow monotonically with engineTimeout")
	}
}

// TestAllConvertQueuesCoversEveryEngine is the D-06 completeness guard for
// the queue-depth collector's silent variadic-omission seam (RESEARCH.md
// Pitfall 2): it iterates every convert.Engine* constant and asserts each
// one appears exactly once in AllConvertQueues(), except image, html, audio, av, ocr, pandoc, tectonic, probe, document, and cad,
// which are admitted and have no queue. AllConvertQueues contains no extras
// (e.g. QueueWebhook, which is not an engine class).
func TestAllConvertQueuesCoversEveryEngine(t *testing.T) {
	// Derived from convert.AllEngines() rather than typed out. The
	// hand-written list here had to be edited by hand every time an engine
	// class was added, which made this completeness guard itself a thing that
	// could drift -- and it did, in Phase 60: adding the eighth class failed
	// this test not because the eighth queue was missing but because this
	// list had not been updated. A guard that needs updating in the same
	// commit as the thing it guards is checking that someone remembered
	// twice, not that the two agree.
	wantEngines := make(map[string]bool)
	for _, e := range convert.AllEngines() {
		wantEngines[e] = true
	}

	got := AllConvertQueues()
	seen := make(map[string]int, len(got))
	for _, q := range got {
		seen[q]++
	}

	for engine := range wantEngines {
		wantCount := 1
		if engine == convert.EngineImage || engine == convert.EngineHTML || engine == convert.EngineAudio || engine == convert.EngineAV || engine == convert.EngineOCR || engine == convert.EnginePandoc || engine == convert.EngineTectonic || engine == convert.EngineProbe || engine == convert.EngineDocument || engine == convert.EngineCAD {
			wantCount = 0
		}
		if seen[engine] != wantCount {
			t.Errorf("AllConvertQueues() contains engine %q %d times, want exactly %d", engine, seen[engine], wantCount)
		}
	}
	for q, count := range seen {
		if !wantEngines[q] {
			t.Errorf("AllConvertQueues() contains unexpected extra entry %q (count %d) -- not a registered engine class", q, count)
		}
	}
}

func TestEnqueueCADJob(t *testing.T) {
	if os.Getenv("REDIS_ADDR") == "" {
		t.Skip("REDIS_ADDR not set; skipping integration test")
	}

	cl, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer cl.Close()

	id := uuid.New()
	if err := cl.EnqueueCADJob(context.Background(), id); err != nil {
		t.Fatalf("EnqueueCADJob: %v", err)
	}

	opt, _ := RedisOpt()
	insp := asynq.NewInspector(opt)
	defer insp.Close()

	infos, err := insp.ListPendingTasks(QueueCAD)
	if err != nil {
		t.Fatalf("ListPendingTasks: %v", err)
	}
	found := false
	for _, ti := range infos {
		p, err := ParseConvertPayload(ti.Payload)
		if err == nil && p.JobID == id {
			found = true
			if ti.Type != TypeCADJob {
				t.Errorf("enqueued type = %q, want %q", ti.Type, TypeCADJob)
			}
			if ti.MaxRetry != cl.cadMaxRetry {
				t.Errorf("enqueued MaxRetry = %d, want %d", ti.MaxRetry, cl.cadMaxRetry)
			}
			_ = insp.DeleteTask(QueueCAD, ti.ID)
			break
		}
	}
	if !found {
		t.Errorf("enqueued task for job %s not found in queue %q", id, QueueCAD)
	}
}

// TestEnqueueWebhookDeliverDuplicate asserts that a second
// EnqueueWebhookDeliver for the same job id, while the first task/lock is
// still live, returns asynq.ErrDuplicateTask instead of creating a second
// concurrent webhook task (D-01). Requires a live Redis (REDIS_ADDR);
// skipped otherwise.
func TestEnqueueWebhookDeliverDuplicate(t *testing.T) {
	if os.Getenv("REDIS_ADDR") == "" {
		t.Skip("REDIS_ADDR not set; skipping integration test")
	}

	cl, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer cl.Close()

	id := uuid.New()
	if err := cl.EnqueueWebhookDeliver(context.Background(), id); err != nil {
		t.Fatalf("first EnqueueWebhookDeliver: %v", err)
	}
	err = cl.EnqueueWebhookDeliver(context.Background(), id)
	if !errors.Is(err, asynq.ErrDuplicateTask) {
		t.Fatalf("second EnqueueWebhookDeliver = %v, want asynq.ErrDuplicateTask", err)
	}

	opt, _ := RedisOpt()
	insp := asynq.NewInspector(opt)
	defer insp.Close()

	infos, err := insp.ListPendingTasks(QueueWebhook)
	if err != nil {
		t.Fatalf("ListPendingTasks: %v", err)
	}
	for _, ti := range infos {
		p, perr := ParseWebhookPayload(ti.Payload)
		if perr == nil && p.JobID == id {
			_ = insp.DeleteTask(QueueWebhook, ti.ID)
			break
		}
	}
}

func TestCADJobTaskRoundTrip(t *testing.T) {
	id := uuid.New()
	ttl := CADUniqueTTL(2, convert.CADDefaultEngineTimeout)
	task, err := NewCADJobTask(id, 2, ttl)
	if err != nil {
		t.Fatalf("NewCADJobTask: %v", err)
	}
	if task.Type() != "cad:job" || task.Type() != TypeCADJob {
		t.Errorf("task type = %q, want cad:job", task.Type())
	}
	if QueueCAD != "cad" {
		t.Errorf("QueueCAD = %q, want cad", QueueCAD)
	}
	p, err := ParseConvertPayload(task.Payload())
	if err != nil {
		t.Fatalf("ParseConvertPayload: %v", err)
	}
	if p.JobID != id {
		t.Errorf("job id = %s, want %s", p.JobID, id)
	}
}

func TestRetryDelayFuncRoutesCADJob(t *testing.T) {
	cadTask := asynq.NewTask(TypeCADJob, nil)
	for _, n := range []int{-1, 0, 1, 5, 99} {
		if got, want := RetryDelayFunc(n, nil, cadTask), CADRetryDelay(n, nil, nil); got != want {
			t.Errorf("RetryDelayFunc(cad, %d) = %v, want %v", n, got, want)
		}
	}
	if got, want := RetryDelayFunc(0, nil, cadTask), 5*time.Second; got != want {
		t.Errorf("RetryDelayFunc(cad, 0) = %v, want %v, the shared schedule rather than asynq's default", got, want)
	}
}

func TestCADUniqueTTL(t *testing.T) {
	const maxRetry = 2
	timeout := convert.CADDefaultEngineTimeout
	want := 3*timeout + cadBackoffSum(maxRetry) + uniqueTTLSafetyMargin
	if got := CADUniqueTTL(maxRetry, timeout); got != want {
		t.Errorf("CADUniqueTTL(%d, %v) = %v, want %v", maxRetry, timeout, got, want)
	}
	if got, want := cadBackoffSum(maxRetry), CADRetryDelay(0, nil, nil)+CADRetryDelay(1, nil, nil); got != want {
		t.Errorf("cadBackoffSum(%d) = %v, want %v", maxRetry, got, want)
	}
	if CADUniqueTTL(maxRetry, 2*timeout) <= CADUniqueTTL(maxRetry, timeout) {
		t.Error("CADUniqueTTL does not grow with the engine timeout")
	}
	if CADUniqueTTL(maxRetry+1, timeout) <= CADUniqueTTL(maxRetry, timeout) {
		t.Error("CADUniqueTTL does not grow with the retry budget")
	}
}

func TestCADQueueIsExposed(t *testing.T) {
	got := AllConvertQueues()
	if len(got) != 0 {
		t.Errorf("AllConvertQueues() has %d queues, want 0", len(got))
	}
	count := 0
	for _, q := range got {
		if q == QueueCAD {
			count++
		}
	}
	if count != 0 {
		t.Errorf("AllConvertQueues() contains %q %d times, want 0", QueueCAD, count)
	}
}
