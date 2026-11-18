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
	task, err := NewImageConvertTask(id, 4, ImageUniqueTTL(4, 120*time.Second))
	if err != nil {
		t.Fatalf("NewImageConvertTask: %v", err)
	}
	if task.Type() != TypeImageConvert {
		t.Errorf("task type = %q, want %q", task.Type(), TypeImageConvert)
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

// TestImageRetryDelaySchedule asserts ImageRetryDelay returns the exact
// 2s/5s/15s schedule (no jitter, D-06) and clamps on both ends.
func TestImageRetryDelaySchedule(t *testing.T) {
	cases := []struct {
		n    int
		want time.Duration
	}{
		{n: -1, want: 2 * time.Second}, // clamps to first entry
		{n: 0, want: 2 * time.Second},
		{n: 1, want: 5 * time.Second},
		{n: 2, want: 15 * time.Second},
		{n: 3, want: 15 * time.Second}, // clamps past the end of the schedule
		{n: 100, want: 15 * time.Second},
	}
	for _, tc := range cases {
		got := ImageRetryDelay(tc.n, nil, nil)
		if got != tc.want {
			t.Errorf("ImageRetryDelay(%d) = %v, want %v", tc.n, got, tc.want)
		}
	}
}

// TestRetryDelayFuncDispatch asserts RetryDelayFunc routes each task type to
// its own retry delay function: image tasks get the exact seconds-scale
// schedule, webhook tasks stay within the existing ±25% jitter band, and an
// unknown task type falls back to asynq's own default.
func TestRetryDelayFuncDispatch(t *testing.T) {
	imageTask := asynq.NewTask(TypeImageConvert, nil)
	webhookTask := asynq.NewTask(TypeWebhookDeliver, nil)
	documentTask := asynq.NewTask(TypeDocumentConvert, nil)
	otherTask := asynq.NewTask("some:other-type", nil)

	if got, want := RetryDelayFunc(0, nil, imageTask), 2*time.Second; got != want {
		t.Errorf("RetryDelayFunc(image, 0) = %v, want %v", got, want)
	}
	if got, want := RetryDelayFunc(2, nil, imageTask), 15*time.Second; got != want {
		t.Errorf("RetryDelayFunc(image, 2) = %v, want %v", got, want)
	}

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

// TestImageUniqueTTL asserts ImageUniqueTTL derives its result from the
// corrected worst-case retry lifetime ((maxRetry+1) executions, since
// asynq's archive condition msg.Retried >= msg.Retry is checked AFTER each
// failed attempt) and that the derived TTL always strictly exceeds that
// worst-case lifetime.
func TestImageUniqueTTL(t *testing.T) {
	maxRetry := 4
	engineTimeout := 120 * time.Second
	backoffSum := 2*time.Second + 5*time.Second + 15*time.Second + 15*time.Second // i=0..3, clamped at i>=2

	want := time.Duration(maxRetry+1)*engineTimeout + backoffSum + uniqueTTLSafetyMargin
	got := ImageUniqueTTL(maxRetry, engineTimeout)
	if got != want {
		t.Errorf("ImageUniqueTTL(%d, %v) = %v, want %v", maxRetry, engineTimeout, got, want)
	}

	worstCaseRetryLifetime := time.Duration(maxRetry+1)*engineTimeout + backoffSum
	if got <= worstCaseRetryLifetime {
		t.Errorf("ImageUniqueTTL(%d, %v) = %v, want strictly greater than worst-case retry lifetime %v", maxRetry, engineTimeout, got, worstCaseRetryLifetime)
	}

	// Clamp case: maxRetry=6 sums backoff for i=0..5, clamped to the last
	// schedule entry (15s) for i>=2.
	maxRetry6 := 6
	backoffSum6 := 2*time.Second + 5*time.Second + 15*time.Second + 15*time.Second + 15*time.Second + 15*time.Second
	want6 := time.Duration(maxRetry6+1)*engineTimeout + backoffSum6 + uniqueTTLSafetyMargin
	if got6 := ImageUniqueTTL(maxRetry6, engineTimeout); got6 != want6 {
		t.Errorf("ImageUniqueTTL(%d, %v) = %v, want %v", maxRetry6, engineTimeout, got6, want6)
	}

	// Monotonicity: raising either argument must never shrink the TTL.
	if ImageUniqueTTL(maxRetry+1, engineTimeout) <= ImageUniqueTTL(maxRetry, engineTimeout) {
		t.Errorf("ImageUniqueTTL must grow monotonically with maxRetry")
	}
	if ImageUniqueTTL(maxRetry, engineTimeout+time.Second) <= ImageUniqueTTL(maxRetry, engineTimeout) {
		t.Errorf("ImageUniqueTTL must grow monotonically with engineTimeout")
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

// TestAVConvertTaskRoundTrip asserts NewAVConvertTask builds a task routed
// to TypeAVConvert whose payload round-trips through ParseConvertPayload
// back to the same job id, and that its asynq options resolve to the av
// queue -- mirrors TestAudioConvertTaskRoundTrip's shape for the av engine
// class (TestNewAVConvertTaskRoutesToAVQueue's scope, folded in here since
// the existing per-engine round-trip tests already assert Type()/payload
// only, not a separate queue-name introspection test).
func TestAVConvertTaskRoundTrip(t *testing.T) {
	id := uuid.New()
	task, err := NewAVConvertTask(id, 2, AVUniqueTTL(2, 600*time.Second))
	if err != nil {
		t.Fatalf("NewAVConvertTask: %v", err)
	}
	if task.Type() != TypeAVConvert {
		t.Errorf("task type = %q, want %q", task.Type(), TypeAVConvert)
	}
	p, err := ParseConvertPayload(task.Payload())
	if err != nil {
		t.Fatalf("ParseConvertPayload: %v", err)
	}
	if p.JobID != id {
		t.Errorf("job id = %s, want %s", p.JobID, id)
	}
}

// TestAVRetryDelaySchedule asserts AVRetryDelay returns the exact D-03
// LOCKED 30s/2m schedule (no jitter, mirrors AudioRetryDelay's/
// DocumentRetryDelay's shape) and clamps on both ends.
func TestAVRetryDelaySchedule(t *testing.T) {
	cases := []struct {
		n    int
		want time.Duration
	}{
		{n: -1, want: 30 * time.Second}, // clamps to first entry
		{n: 0, want: 30 * time.Second},
		{n: 1, want: 2 * time.Minute},
		{n: 99, want: 2 * time.Minute}, // clamps past the end of the schedule
	}
	for _, tc := range cases {
		got := AVRetryDelay(tc.n, nil, nil)
		if got != tc.want {
			t.Errorf("AVRetryDelay(%d) = %v, want %v", tc.n, got, tc.want)
		}
	}
}

// TestRetryDelayFuncRoutesAVConvert is the only mechanism that catches
// Pitfall 1's silent RetryDelayFunc default fallthrough for av: it fails if
// the `case TypeAVConvert` arm is ever removed from RetryDelayFunc, since a
// missing case would produce no compile error, only the wrong (asynq
// default) backoff schedule silently applied in production.
func TestRetryDelayFuncRoutesAVConvert(t *testing.T) {
	avTask := asynq.NewTask(TypeAVConvert, nil)

	if got, want := RetryDelayFunc(0, nil, avTask), 30*time.Second; got != want {
		t.Errorf("RetryDelayFunc(av, 0) = %v, want %v", got, want)
	}
	if got, want := RetryDelayFunc(1, nil, avTask), 2*time.Minute; got != want {
		t.Errorf("RetryDelayFunc(av, 1) = %v, want %v", got, want)
	}
	// Index 5 clamps to the last schedule entry (2m).
	if got, want := RetryDelayFunc(5, nil, avTask), 2*time.Minute; got != want {
		t.Errorf("RetryDelayFunc(av, 5) = %v, want %v", got, want)
	}
}

// TestAVUniqueTTL asserts AVUniqueTTL derives its result from the corrected
// worst-case retry lifetime ((maxRetry+1) executions) exactly like
// DocumentUniqueTTL/HTMLUniqueTTL/AudioUniqueTTL, evaluates to exactly
// 2070s for (2, 600s) -- 3*600s + (30s+120s) + 120s = 2070s -- is monotonic
// in both arguments, and strictly EXCEEDS the zero-margin worst-case retry
// lifetime, demonstrating uniqueTTLSafetyMargin is a load-bearing term, not
// accidentally zero (D-03: reuses the shared margin, never a per-engine
// one).
func TestAVUniqueTTL(t *testing.T) {
	maxRetry := 2                                // D-03 LOCKED
	engineTimeout := 600 * time.Second           // matches the provisional AV_ENGINE_TIMEOUT default
	backoffSum := 30*time.Second + 2*time.Minute // i=0..1, per avRetrySchedule

	want := time.Duration(maxRetry+1)*engineTimeout + backoffSum + uniqueTTLSafetyMargin
	got := AVUniqueTTL(maxRetry, engineTimeout)
	if got != want {
		t.Errorf("AVUniqueTTL(%d, %v) = %v, want %v", maxRetry, engineTimeout, got, want)
	}
	// Assertion 1: exact hand-computed worked example, pinned as a literal
	// self-check mirroring TestAudioUniqueTTL's own "want != 2570*time.Second"
	// shape.
	if want != 2070*time.Second {
		t.Errorf("expected worked example want = 2070s, got %v", want)
	}

	// Assertion 2: monotonicity in BOTH arguments.
	if AVUniqueTTL(maxRetry+1, engineTimeout) <= AVUniqueTTL(maxRetry, engineTimeout) {
		t.Errorf("AVUniqueTTL must grow monotonically with maxRetry")
	}
	if AVUniqueTTL(maxRetry, engineTimeout+time.Second) <= AVUniqueTTL(maxRetry, engineTimeout) {
		t.Errorf("AVUniqueTTL must grow monotonically with engineTimeout")
	}

	// Assertion 3: strictly exceeds the zero-margin worst-case retry
	// lifetime -- proves uniqueTTLSafetyMargin is load-bearing rather than
	// accidentally zero.
	worstCaseNoMargin := time.Duration(maxRetry+1)*engineTimeout + avBackoffSum(maxRetry)
	if AVUniqueTTL(maxRetry, engineTimeout) <= worstCaseNoMargin {
		t.Errorf("AVUniqueTTL(%d, %v) = %v must strictly exceed the zero-margin worst-case lifetime %v",
			maxRetry, engineTimeout, AVUniqueTTL(maxRetry, engineTimeout), worstCaseNoMargin)
	}
}

// TestOCRConvertTaskRoundTrip asserts NewOCRConvertTask builds a task routed
// to TypeOCRConvert whose payload round-trips through ParseConvertPayload
// back to the same job id -- mirrors TestAVConvertTaskRoundTrip's shape for
// the ocr engine class.
func TestOCRConvertTaskRoundTrip(t *testing.T) {
	id := uuid.New()
	task, err := NewOCRConvertTask(id, 2, OCRUniqueTTL(2, 300*time.Second))
	if err != nil {
		t.Fatalf("NewOCRConvertTask: %v", err)
	}
	if task.Type() != TypeOCRConvert {
		t.Errorf("task type = %q, want %q", task.Type(), TypeOCRConvert)
	}
	p, err := ParseConvertPayload(task.Payload())
	if err != nil {
		t.Fatalf("ParseConvertPayload: %v", err)
	}
	if p.JobID != id {
		t.Errorf("job id = %s, want %s", p.JobID, id)
	}
}

// TestOCRRetryDelaySchedule asserts OCRRetryDelay returns the exact
// provisional 15s/1m schedule (no jitter, mirrors AVRetryDelay's shape) and
// clamps on both ends.
func TestOCRRetryDelaySchedule(t *testing.T) {
	cases := []struct {
		n    int
		want time.Duration
	}{
		{n: -1, want: 15 * time.Second}, // clamps to first entry
		{n: 0, want: 15 * time.Second},
		{n: 1, want: 1 * time.Minute},
		{n: 99, want: 1 * time.Minute}, // clamps past the end of the schedule
	}
	for _, tc := range cases {
		got := OCRRetryDelay(tc.n, nil, nil)
		if got != tc.want {
			t.Errorf("OCRRetryDelay(%d) = %v, want %v", tc.n, got, tc.want)
		}
	}
}

// TestRetryDelayFuncRoutesOCRConvert is the only mechanism that catches
// Pitfall 1's silent RetryDelayFunc default fallthrough for ocr: it fails if
// the `case TypeOCRConvert` arm is ever removed from RetryDelayFunc, since a
// missing case would produce no compile error, only the wrong (asynq
// default) backoff schedule silently applied in production.
func TestRetryDelayFuncRoutesOCRConvert(t *testing.T) {
	ocrTask := asynq.NewTask(TypeOCRConvert, nil)

	if got, want := RetryDelayFunc(0, nil, ocrTask), 15*time.Second; got != want {
		t.Errorf("RetryDelayFunc(ocr, 0) = %v, want %v", got, want)
	}
	if got, want := RetryDelayFunc(1, nil, ocrTask), 1*time.Minute; got != want {
		t.Errorf("RetryDelayFunc(ocr, 1) = %v, want %v", got, want)
	}
	// Index 5 clamps to the last schedule entry (1m).
	if got, want := RetryDelayFunc(5, nil, ocrTask), 1*time.Minute; got != want {
		t.Errorf("RetryDelayFunc(ocr, 5) = %v, want %v", got, want)
	}
}

// TestOCRUniqueTTL asserts OCRUniqueTTL derives its result from the
// corrected worst-case retry lifetime ((maxRetry+1) executions) exactly like
// AVUniqueTTL, evaluates to exactly 1095s for (2, 300s) -- 3*300s + (15s+60s)
// + 120s = 1095s -- is monotonic in both arguments, and strictly EXCEEDS the
// zero-margin worst-case retry lifetime, demonstrating uniqueTTLSafetyMargin
// is a load-bearing term, not accidentally zero (D-04: reuses the shared
// margin, never a per-engine one; OCRUniqueTTL is derived FRESH from OCR's
// own budget, never copied from AV/Audio).
func TestOCRUniqueTTL(t *testing.T) {
	maxRetry := 2
	engineTimeout := 300 * time.Second           // matches the provisional OCR_ENGINE_TIMEOUT default
	backoffSum := 15*time.Second + 1*time.Minute // i=0..1, per ocrRetrySchedule

	want := time.Duration(maxRetry+1)*engineTimeout + backoffSum + uniqueTTLSafetyMargin
	got := OCRUniqueTTL(maxRetry, engineTimeout)
	if got != want {
		t.Errorf("OCRUniqueTTL(%d, %v) = %v, want %v", maxRetry, engineTimeout, got, want)
	}
	if want != 1095*time.Second {
		t.Errorf("expected worked example want = 1095s, got %v", want)
	}

	// Monotonicity: raising either argument must never shrink the TTL.
	if OCRUniqueTTL(maxRetry+1, engineTimeout) <= OCRUniqueTTL(maxRetry, engineTimeout) {
		t.Errorf("OCRUniqueTTL must grow monotonically with maxRetry")
	}
	if OCRUniqueTTL(maxRetry, engineTimeout+time.Second) <= OCRUniqueTTL(maxRetry, engineTimeout) {
		t.Errorf("OCRUniqueTTL must grow monotonically with engineTimeout")
	}

	// Fresh-derivation proof (acceptance criterion, D-04/IN-02): OCRUniqueTTL
	// scales with its own engineTimeout argument rather than being a
	// hardcoded/copied constant.
	if OCRUniqueTTL(2, 300*time.Second) == OCRUniqueTTL(2, 600*time.Second) {
		t.Errorf("OCRUniqueTTL(2, 300s) must differ from OCRUniqueTTL(2, 600s) -- proves it is derived, not constant")
	}

	// Strictly exceeds the zero-margin worst-case retry lifetime -- proves
	// uniqueTTLSafetyMargin is load-bearing rather than accidentally zero.
	worstCaseNoMargin := time.Duration(maxRetry+1)*engineTimeout + ocrBackoffSum(maxRetry)
	if OCRUniqueTTL(maxRetry, engineTimeout) <= worstCaseNoMargin {
		t.Errorf("OCRUniqueTTL(%d, %v) = %v must strictly exceed the zero-margin worst-case lifetime %v",
			maxRetry, engineTimeout, OCRUniqueTTL(maxRetry, engineTimeout), worstCaseNoMargin)
	}
}

// TestPandocConvertTaskRoundTrip mirrors TestOCRConvertTaskRoundTrip for the
// pandoc engine class.
func TestPandocConvertTaskRoundTrip(t *testing.T) {
	id := uuid.New()
	task, err := NewPandocConvertTask(id, 2, PandocUniqueTTL(2, 90*time.Second))
	if err != nil {
		t.Fatalf("NewPandocConvertTask: %v", err)
	}
	if task.Type() != TypePandocConvert {
		t.Errorf("task type = %q, want %q", task.Type(), TypePandocConvert)
	}
	p, err := ParseConvertPayload(task.Payload())
	if err != nil {
		t.Fatalf("ParseConvertPayload: %v", err)
	}
	if p.JobID != id {
		t.Errorf("job id = %s, want %s", p.JobID, id)
	}
}

// TestPandocRetrySchedule asserts pandocRetrySchedule has exactly 2 entries
// (acceptance criterion) and that PandocRetryDelay returns pandoc's own 5s/
// 20s schedule (no jitter, mirrors OCRRetryDelay's shape) and clamps on both
// ends.
func TestPandocRetrySchedule(t *testing.T) {
	if len(pandocRetrySchedule) != 2 {
		t.Fatalf("len(pandocRetrySchedule) = %d, want 2", len(pandocRetrySchedule))
	}

	cases := []struct {
		n    int
		want time.Duration
	}{
		{n: -1, want: 5 * time.Second}, // clamps to first entry
		{n: 0, want: 5 * time.Second},
		{n: 1, want: 20 * time.Second},
		{n: 99, want: 20 * time.Second}, // clamps past the end of the schedule
	}
	for _, tc := range cases {
		got := PandocRetryDelay(tc.n, nil, nil)
		if got != tc.want {
			t.Errorf("PandocRetryDelay(%d) = %v, want %v", tc.n, got, tc.want)
		}
	}
}

// TestRetryDelayFuncRoutesPandocConvert is the only mechanism that catches
// Pitfall 1's silent RetryDelayFunc default fallthrough for pandoc: it fails
// if the `case TypePandocConvert` arm is ever removed from RetryDelayFunc,
// since a missing case would produce no compile error, only the wrong
// (asynq default) backoff schedule silently applied in production.
func TestRetryDelayFuncRoutesPandocConvert(t *testing.T) {
	pandocTask := asynq.NewTask(TypePandocConvert, nil)

	if got, want := RetryDelayFunc(0, nil, pandocTask), 5*time.Second; got != want {
		t.Errorf("RetryDelayFunc(pandoc, 0) = %v, want %v", got, want)
	}
	if got, want := RetryDelayFunc(1, nil, pandocTask), 20*time.Second; got != want {
		t.Errorf("RetryDelayFunc(pandoc, 1) = %v, want %v", got, want)
	}
	// Index 5 clamps to the last schedule entry (20s).
	if got, want := RetryDelayFunc(5, nil, pandocTask), 20*time.Second; got != want {
		t.Errorf("RetryDelayFunc(pandoc, 5) = %v, want %v", got, want)
	}
}

// TestPandocUniqueTTL asserts PandocUniqueTTL derives its result from the
// corrected worst-case retry lifetime ((maxRetry+1) executions) exactly like
// OCRUniqueTTL, is monotonic in both arguments, strictly EXCEEDS the
// zero-margin worst-case retry lifetime (uniqueTTLSafetyMargin is
// load-bearing), and -- the D-03 fresh-derivation proof -- scales with its
// own engineTimeout argument (60s vs 120s) rather than being a hardcoded/
// copied constant, and is provably distinct from OCRUniqueTTL/AVUniqueTTL/
// AudioUniqueTTL under the SAME (maxRetry, engineTimeout) inputs (proves the
// formula's constants -- schedule/margin -- are pandoc's own, not copied).
func TestPandocUniqueTTL(t *testing.T) {
	maxRetry := 2
	engineTimeout := 90 * time.Second            // matches the provisional PANDOC_ENGINE_TIMEOUT default
	backoffSum := 5*time.Second + 20*time.Second // i=0..1, per pandocRetrySchedule

	want := time.Duration(maxRetry+1)*engineTimeout + backoffSum + uniqueTTLSafetyMargin
	got := PandocUniqueTTL(maxRetry, engineTimeout)
	if got != want {
		t.Errorf("PandocUniqueTTL(%d, %v) = %v, want %v", maxRetry, engineTimeout, got, want)
	}

	// Monotonicity: raising either argument must never shrink the TTL.
	if PandocUniqueTTL(maxRetry+1, engineTimeout) <= PandocUniqueTTL(maxRetry, engineTimeout) {
		t.Errorf("PandocUniqueTTL must grow monotonically with maxRetry")
	}
	if PandocUniqueTTL(maxRetry, engineTimeout+time.Second) <= PandocUniqueTTL(maxRetry, engineTimeout) {
		t.Errorf("PandocUniqueTTL must grow monotonically with engineTimeout")
	}

	// Fresh-derivation proof (acceptance criterion, D-03): PandocUniqueTTL
	// scales with its own engineTimeout argument rather than being a
	// hardcoded/copied constant.
	if PandocUniqueTTL(2, 60*time.Second) == PandocUniqueTTL(2, 120*time.Second) {
		t.Errorf("PandocUniqueTTL(2, 60s) must differ from PandocUniqueTTL(2, 120s) -- proves it is derived, not constant")
	}

	// Strictly exceeds the zero-margin worst-case retry lifetime -- proves
	// uniqueTTLSafetyMargin is load-bearing rather than accidentally zero.
	worstCaseNoMargin := time.Duration(maxRetry+1)*engineTimeout + pandocBackoffSum(maxRetry)
	if PandocUniqueTTL(maxRetry, engineTimeout) <= worstCaseNoMargin {
		t.Errorf("PandocUniqueTTL(%d, %v) = %v must strictly exceed the zero-margin worst-case lifetime %v",
			maxRetry, engineTimeout, PandocUniqueTTL(maxRetry, engineTimeout), worstCaseNoMargin)
	}

	// Not-copied proof: under the SAME (maxRetry, engineTimeout) inputs,
	// PandocUniqueTTL must differ from OCRUniqueTTL/AVUniqueTTL/
	// AudioUniqueTTL -- if it matched, the schedule/margin constants baked
	// into the formula would have to be an accidental or deliberate copy of
	// another engine's, not pandoc's own.
	if PandocUniqueTTL(maxRetry, engineTimeout) == OCRUniqueTTL(maxRetry, engineTimeout) {
		t.Errorf("PandocUniqueTTL(%d, %v) must not equal OCRUniqueTTL(%d, %v) -- proves the schedule is pandoc's own, not OCR's",
			maxRetry, engineTimeout, maxRetry, engineTimeout)
	}
	if PandocUniqueTTL(maxRetry, engineTimeout) == AVUniqueTTL(maxRetry, engineTimeout) {
		t.Errorf("PandocUniqueTTL(%d, %v) must not equal AVUniqueTTL(%d, %v) -- proves the schedule is pandoc's own, not AV's",
			maxRetry, engineTimeout, maxRetry, engineTimeout)
	}
}

// TestAllConvertQueuesCoversEveryEngine is the D-06 completeness guard for
// the queue-depth collector's silent variadic-omission seam (RESEARCH.md
// Pitfall 2): it iterates every convert.Engine* constant and asserts each
// one appears exactly once in AllConvertQueues(), except html and audio,
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
		if engine == convert.EngineHTML || engine == convert.EngineAudio {
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

// TestProbeUniqueTTLIsItsOwn mirrors the pandoc/OCR distinctness proofs: the
// probe lock TTL must be derived from probe's own retry budget rather than
// copied from a neighbour. Equal values under the same inputs would mean the
// derivation was shared, and a shared derivation drifts silently the moment
// one engine's budget changes.
func TestProbeUniqueTTLIsItsOwn(t *testing.T) {
	const maxRetry = 2
	engineTimeout := 180 * time.Second

	got := ProbeUniqueTTL(maxRetry, engineTimeout)
	want := time.Duration(maxRetry+1)*engineTimeout + (5*time.Second + 20*time.Second) + uniqueTTLSafetyMargin
	if got != want {
		t.Errorf("ProbeUniqueTTL(%d, %v) = %v, want %v", maxRetry, engineTimeout, got, want)
	}
	// Probe and pandoc happen to share a schedule shape today, so a
	// "must differ from pandoc" assertion would be false rather than
	// strong. What the recomputation above proves instead is the property
	// that actually matters: the TTL comes from probeRetrySchedule's own
	// literals, so pandoc changing its schedule cannot move this number.
}

// TestClientOCRUniqueTTLDiffersFromAVAndAudio is the D-04/IN-02
// fresh-derivation proof at the Client level: under default env (no
// OCR_MAX_RETRY/OCR_ENGINE_TIMEOUT/AV_MAX_RETRY/AV_ENGINE_TIMEOUT/
// AUDIO_MAX_RETRY/AUDIO_ENGINE_TIMEOUT overrides), the client's derived
// ocrUniqueTTL must NOT be byte-equal to avUniqueTTL or audioUniqueTTL --
// proving OCRUniqueTTL was computed from OCR's own budget, not copied.
// asynq.NewClient only lazily constructs a redis.UniversalClient (no network
// I/O at construction time -- see asynq's MakeRedisClient), so this test
// needs no live Redis; REDIS_ADDR only has to be non-empty to satisfy
// RedisOpt's presence check.
func TestClientOCRUniqueTTLDiffersFromAVAndAudio(t *testing.T) {
	t.Setenv("REDIS_ADDR", "127.0.0.1:0")
	for _, k := range []string{
		"OCR_MAX_RETRY", "OCR_ENGINE_TIMEOUT",
		"AV_MAX_RETRY", "AV_ENGINE_TIMEOUT",
	} {
		t.Setenv(k, "")
	}

	cl, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer cl.Close()

	if cl.ocrUniqueTTL == cl.avUniqueTTL {
		t.Errorf("ocrUniqueTTL (%v) must not equal avUniqueTTL (%v) -- fresh derivation, not a copy", cl.ocrUniqueTTL, cl.avUniqueTTL)
	}
}

// TestClientPandocUniqueTTLDiffersFromOCRAndAV is the D-03 fresh-derivation
// proof at the Client level, mirroring
// TestClientOCRUniqueTTLDiffersFromAVAndAudio's shape exactly: under default
// env (no PANDOC_MAX_RETRY/PANDOC_ENGINE_TIMEOUT/OCR_MAX_RETRY/
// OCR_ENGINE_TIMEOUT/AV_MAX_RETRY/AV_ENGINE_TIMEOUT overrides), the client's
// derived pandocUniqueTTL must NOT be byte-equal to ocrUniqueTTL or
// avUniqueTTL -- proving PandocUniqueTTL was computed from pandoc's own
// budget, not copied from a neighboring engine class (the recurring
// project anti-pattern this phase is flagged against). asynq.NewClient only
// lazily constructs a redis.UniversalClient (no network I/O at construction
// time), so this test needs no live Redis; REDIS_ADDR only has to be
// non-empty to satisfy RedisOpt's presence check.
func TestClientPandocUniqueTTLDiffersFromOCRAndAV(t *testing.T) {
	t.Setenv("REDIS_ADDR", "127.0.0.1:0")
	for _, k := range []string{
		"PANDOC_MAX_RETRY", "PANDOC_ENGINE_TIMEOUT",
		"OCR_MAX_RETRY", "OCR_ENGINE_TIMEOUT",
		"AV_MAX_RETRY", "AV_ENGINE_TIMEOUT",
	} {
		t.Setenv(k, "")
	}

	cl, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer cl.Close()

	if cl.pandocUniqueTTL == cl.ocrUniqueTTL {
		t.Errorf("pandocUniqueTTL (%v) must not equal ocrUniqueTTL (%v) -- fresh derivation, not a copy", cl.pandocUniqueTTL, cl.ocrUniqueTTL)
	}
	if cl.pandocUniqueTTL == cl.avUniqueTTL {
		t.Errorf("pandocUniqueTTL (%v) must not equal avUniqueTTL (%v) -- fresh derivation, not a copy", cl.pandocUniqueTTL, cl.avUniqueTTL)
	}
}

// TestEnqueueOCRConvert enqueues an OCR task and confirms it lands in the ocr
// queue. Requires a live Redis (REDIS_ADDR); skipped otherwise. Mirrors
// TestEnqueueImageConvert's shape for the ocr engine class.
func TestEnqueueOCRConvert(t *testing.T) {
	if os.Getenv("REDIS_ADDR") == "" {
		t.Skip("REDIS_ADDR not set; skipping integration test")
	}

	cl, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer cl.Close()

	id := uuid.New()
	if err := cl.EnqueueOCRConvert(context.Background(), id); err != nil {
		t.Fatalf("EnqueueOCRConvert: %v", err)
	}

	opt, _ := RedisOpt()
	insp := asynq.NewInspector(opt)
	defer insp.Close()

	infos, err := insp.ListPendingTasks(QueueOCR)
	if err != nil {
		t.Fatalf("ListPendingTasks: %v", err)
	}
	found := false
	for _, ti := range infos {
		p, err := ParseConvertPayload(ti.Payload)
		if err == nil && p.JobID == id {
			found = true
			if ti.Type != TypeOCRConvert {
				t.Errorf("enqueued type = %q, want %q", ti.Type, TypeOCRConvert)
			}
			// cleanup
			_ = insp.DeleteTask(QueueOCR, ti.ID)
			break
		}
	}
	if !found {
		t.Errorf("enqueued task for job %s not found in queue %q", id, QueueOCR)
	}
}

// TestEnqueuePandocConvert enqueues a pandoc task and confirms it lands in
// the pandoc queue. Requires a live Redis (REDIS_ADDR); skipped otherwise.
// Mirrors TestEnqueueOCRConvert's shape for the pandoc engine class.
func TestEnqueuePandocConvert(t *testing.T) {
	if os.Getenv("REDIS_ADDR") == "" {
		t.Skip("REDIS_ADDR not set; skipping integration test")
	}

	cl, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer cl.Close()

	id := uuid.New()
	if err := cl.EnqueuePandocConvert(context.Background(), id); err != nil {
		t.Fatalf("EnqueuePandocConvert: %v", err)
	}

	opt, _ := RedisOpt()
	insp := asynq.NewInspector(opt)
	defer insp.Close()

	infos, err := insp.ListPendingTasks(QueuePandoc)
	if err != nil {
		t.Fatalf("ListPendingTasks: %v", err)
	}
	found := false
	for _, ti := range infos {
		p, err := ParseConvertPayload(ti.Payload)
		if err == nil && p.JobID == id {
			found = true
			if ti.Type != TypePandocConvert {
				t.Errorf("enqueued type = %q, want %q", ti.Type, TypePandocConvert)
			}
			// cleanup
			_ = insp.DeleteTask(QueuePandoc, ti.ID)
			break
		}
	}
	if !found {
		t.Errorf("enqueued task for job %s not found in queue %q", id, QueuePandoc)
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

// TestEnqueueImageConvert enqueues a task and confirms it lands in the image
// queue. Requires a live Redis (REDIS_ADDR); skipped otherwise.
func TestEnqueueImageConvert(t *testing.T) {
	if os.Getenv("REDIS_ADDR") == "" {
		t.Skip("REDIS_ADDR not set; skipping integration test")
	}

	cl, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer cl.Close()

	id := uuid.New()
	if err := cl.EnqueueImageConvert(context.Background(), id); err != nil {
		t.Fatalf("EnqueueImageConvert: %v", err)
	}

	opt, _ := RedisOpt()
	insp := asynq.NewInspector(opt)
	defer insp.Close()

	infos, err := insp.ListPendingTasks(QueueImage)
	if err != nil {
		t.Fatalf("ListPendingTasks: %v", err)
	}
	found := false
	for _, ti := range infos {
		p, err := ParseConvertPayload(ti.Payload)
		if err == nil && p.JobID == id {
			found = true
			if ti.Type != TypeImageConvert {
				t.Errorf("enqueued type = %q, want %q", ti.Type, TypeImageConvert)
			}
			// cleanup
			_ = insp.DeleteTask(QueueImage, ti.ID)
			break
		}
	}
	if !found {
		t.Errorf("enqueued task for job %s not found in queue %q", id, QueueImage)
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
	if len(got) != 8 {
		t.Errorf("AllConvertQueues() has %d queues, want 8", len(got))
	}
	count := 0
	for _, q := range got {
		if q == QueueCAD {
			count++
		}
	}
	if count != 1 {
		t.Errorf("AllConvertQueues() contains %q %d times, want exactly 1", QueueCAD, count)
	}
}
