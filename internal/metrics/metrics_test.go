package metrics

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRecordJobOutcome(t *testing.T) {
	before := testutil.ToFloat64(jobOutcomes.WithLabelValues("image", "done"))
	RecordJobOutcome("image", "done", 50*time.Millisecond)
	after := testutil.ToFloat64(jobOutcomes.WithLabelValues("image", "done"))
	if after != before+1 {
		t.Fatalf("job outcome counter: want %v, got %v", before+1, after)
	}
}

func TestRecordWebhookDelivery(t *testing.T) {
	beforeSuccess := testutil.ToFloat64(webhookDeliveries.WithLabelValues("success"))
	beforeFailure := testutil.ToFloat64(webhookDeliveries.WithLabelValues("failure"))

	RecordWebhookDelivery(true)
	RecordWebhookDelivery(false)

	afterSuccess := testutil.ToFloat64(webhookDeliveries.WithLabelValues("success"))
	afterFailure := testutil.ToFloat64(webhookDeliveries.WithLabelValues("failure"))

	if afterSuccess != beforeSuccess+1 {
		t.Fatalf("success counter: want %v, got %v", beforeSuccess+1, afterSuccess)
	}
	if afterFailure != beforeFailure+1 {
		t.Fatalf("failure counter: want %v, got %v", beforeFailure+1, afterFailure)
	}
}

func TestRecordReconcilerAction(t *testing.T) {
	before := testutil.ToFloat64(reconcilerActions.WithLabelValues("recovered"))
	RecordReconcilerAction("recovered")
	after := testutil.ToFloat64(reconcilerActions.WithLabelValues("recovered"))
	if after != before+1 {
		t.Fatalf("recovered counter: want %v, got %v", before+1, after)
	}
}

func TestNewQueueDepthCollectorDescribe(t *testing.T) {
	inspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: "127.0.0.1:0"})
	c := NewQueueDepthCollector(inspector, "image", "webhook")

	ch := make(chan *prometheus.Desc, 10)
	c.Describe(ch)
	close(ch)

	count := 0
	for range ch {
		count++
	}
	if count != 1 {
		t.Fatalf("Describe: want exactly 1 descriptor, got %d", count)
	}
}

// TestNewQueueDepthCollectorDescribeAllFourQueues is the Redis-free half of
// D-03 (KEDA-01, Phase 27 Plan 01): constructing the collector with all four
// engine-class queue names — the exact set now registered once on the
// always-on api process (cmd/api/main.go), not per-worker — must not panic
// and must still Describe exactly one octoconv_queue_depth descriptor,
// regardless of how many queue names are passed in. The per-queue value
// proof (that Collect() actually reports a series for each queue) lives in
// the compose-E2E test (TestQueueDepthMetricRelocationE2E), since Collect
// needs a live Redis to query via the Inspector.
func TestRecordClientJobOutcome(t *testing.T) {
	clientID := uuid.NewString()
	before := testutil.ToFloat64(clientJobOutcomes.WithLabelValues(clientID, "image", "done"))
	RecordClientJobOutcome(clientID, "image", "done")
	after := testutil.ToFloat64(clientJobOutcomes.WithLabelValues(clientID, "image", "done"))
	if after != before+1 {
		t.Fatalf("client job outcome counter: want %v, got %v", before+1, after)
	}
}

// TestClientLabelCeilingCollapsesToOverflow exercises the allocator through
// a test-local instance (newClientLabelAllocator), NOT the package-level
// defaultClientAllocator that clientLabel/RecordClientJobOutcome use in
// production. Chosen approach: an isolated instance rather than a
// reset-the-shared-map helper, so this test's assertions hold regardless of
// which client ids sibling tests (or concurrent production calls) admit
// into the shared package state.
func TestClientLabelCeilingCollapsesToOverflow(t *testing.T) {
	const ceiling = 5
	a := newClientLabelAllocator(ceiling)

	ids := make([]string, ceiling)
	for i := range ids {
		ids[i] = uuid.NewString()
		if got := a.resolve(ids[i]); got != ids[i] {
			t.Fatalf("admit %d: want %s, got %s", i, ids[i], got)
		}
	}

	overflow1 := uuid.NewString()
	overflow2 := uuid.NewString()
	if got := a.resolve(overflow1); got != overflowClientLabel {
		t.Fatalf("overflow 1: want %q, got %q", overflowClientLabel, got)
	}
	if got := a.resolve(overflow2); got != overflowClientLabel {
		t.Fatalf("overflow 2: want %q, got %q", overflowClientLabel, got)
	}

	// An already-admitted id keeps its own label after the ceiling is
	// reached (first-seen-first-served, never evicted, D-10).
	if got := a.resolve(ids[0]); got != ids[0] {
		t.Fatalf("previously admitted id: want %s, got %s", ids[0], got)
	}
}

// TestClientLabelRejectsMalformedID uses clientLabel (the production
// package-level allocator) directly: none of these inputs ever consume a
// ceiling slot, so the assertion holds regardless of shared-state ordering.
func TestClientLabelRejectsMalformedID(t *testing.T) {
	cases := []string{"", "not-a-uuid", "00000000-0000-0000-0000-000000000000"}
	for _, c := range cases {
		if got := clientLabel(c); got != overflowClientLabel {
			t.Fatalf("clientLabel(%q): want %q, got %q", c, overflowClientLabel, got)
		}
	}
}

// TestJobDurationLabelsPinned exists to fail loudly on a future diff that
// adds client_id (or any other label) to octoconv_job_duration_seconds
// (ROADMAP SC1, D-07): the histogram is already at ~250 series and must not
// multiply.
func TestJobDurationLabelsPinned(t *testing.T) {
	RecordJobOutcome("image", "done", 10*time.Millisecond)

	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	var found bool
	for _, mf := range mfs {
		if mf.GetName() != "octoconv_job_duration_seconds" {
			continue
		}
		found = true
		for _, m := range mf.GetMetric() {
			var names []string
			for _, l := range m.GetLabel() {
				names = append(names, l.GetName())
			}
			sort.Strings(names)
			want := []string{"engine", "status"}
			if len(names) != len(want) {
				t.Fatalf("octoconv_job_duration_seconds labels: want %v, got %v", want, names)
			}
			for i := range want {
				if names[i] != want[i] {
					t.Fatalf("octoconv_job_duration_seconds labels: want %v, got %v", want, names)
				}
			}
		}
	}
	if !found {
		t.Fatal("octoconv_job_duration_seconds family not found in default gatherer")
	}
}

// TestQueueDepthLabelsPinned exists to fail loudly on a future diff that
// adds client_id to the KEDA-read octoconv_queue_depth metric (ROADMAP SC1).
func TestQueueDepthLabelsPinned(t *testing.T) {
	desc := queueDepthDesc.String()
	if !strings.Contains(desc, "queue") || !strings.Contains(desc, "state") {
		t.Fatalf("queueDepthDesc: want to mention queue and state, got %s", desc)
	}
	if strings.Contains(desc, "client_id") {
		t.Fatalf("queueDepthDesc: must NOT mention client_id, got %s", desc)
	}
}

func TestNewQueueDepthCollectorDescribeAllFourQueues(t *testing.T) {
	inspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: "127.0.0.1:0"})
	c := NewQueueDepthCollector(inspector, "image", "document", "html", "webhook")

	ch := make(chan *prometheus.Desc, 10)
	c.Describe(ch)
	close(ch)

	count := 0
	for range ch {
		count++
	}
	if count != 1 {
		t.Fatalf("Describe: want exactly 1 descriptor, got %d", count)
	}
}
