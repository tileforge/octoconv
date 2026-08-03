// Package metrics defines the Prometheus counters, histograms, and
// collectors this service exposes.
package metrics

import (
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	jobOutcomes = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "octoconv_job_outcomes_total",
		Help: "Total number of conversion jobs reaching a terminal state, labeled by engine and status.",
	}, []string{"engine", "status"})

	jobDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "octoconv_job_duration_seconds",
		Help:    "Wall-clock duration of a conversion attempt, labeled by engine and status.",
		Buckets: prometheus.DefBuckets,
	}, []string{"engine", "status"})

	webhookDeliveries = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "octoconv_webhook_deliveries_total",
		Help: "Total number of webhook delivery attempts, labeled by result (success/failure).",
	}, []string{"result"})

	reconcilerActions = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "octoconv_reconciler_actions_total",
		Help: "Total number of reconciler actions, labeled by action (recovered/exhausted/webhook_gap_recovered/unroutable_engine/purged_jobs/purged_usage).",
	}, []string{"action"})

	// clientJobOutcomes is a NEW, purpose-built CounterVec (D-07) -- client_id
	// is deliberately NOT added to jobOutcomes or jobDuration above. jobDuration
	// is a histogram already at ~250 series; adding client_id there would
	// multiply it. This counter's own client_id label is itself bounded by
	// clientLabel below, never fed a raw client id directly.
	clientJobOutcomes = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "octoconv_client_job_outcomes_total",
		Help: "Total number of conversion jobs reaching a terminal state, labeled by client, engine and status. client_id collapses to the literal \"overflow\" once ClientLabelCeiling distinct clients have been observed.",
	}, []string{"client_id", "engine", "status"})
)

// RecordJobOutcome increments the job-outcome counter and observes the
// conversion duration for the given engine and terminal status.
func RecordJobOutcome(engine, status string, d time.Duration) {
	jobOutcomes.WithLabelValues(engine, status).Inc()
	jobDuration.WithLabelValues(engine, status).Observe(d.Seconds())
}

// RecordWebhookDelivery increments the webhook-delivery counter for a
// success or failure result.
func RecordWebhookDelivery(success bool) {
	result := "failure"
	if success {
		result = "success"
	}
	webhookDeliveries.WithLabelValues(result).Inc()
}

// RecordReconcilerAction increments the reconciler-action counter for the
// given action ("recovered", "exhausted", "webhook_gap_recovered", or
// "unroutable_engine").
func RecordReconcilerAction(action string) {
	reconcilerActions.WithLabelValues(action).Inc()
}

// RecordReconcilerPurge adds n to the reconciler-action counter for a
// retention purge kind ("purged_jobs" or "purged_usage", RETN-01). It
// reuses reconcilerActions rather than adding a new metric -- RETN-01 only
// requires the purge to report what it deleted, and this vec is already the
// reconciler's reporting surface (T-51-15: an operator must be able to see
// whether retention is running at all).
func RecordReconcilerPurge(kind string, n int) {
	reconcilerActions.WithLabelValues(kind).Add(float64(n))
}

// overflowClientLabel is the single stable label value every client_id
// beyond ClientLabelCeiling, or any malformed/empty/nil-UUID id, collapses
// into (D-09). Data is not lost -- Postgres's client_usage_daily rollup
// remains exact regardless of this metric's ceiling -- only per-client
// resolution on this counter is.
const overflowClientLabel = "overflow"

// ClientLabelCeiling bounds the number of distinct client_id label values
// admitted onto octoconv_client_job_outcomes_total (ACCT-04, D-08). A
// client_id label multiplies series with the number of distinct clients;
// left unbounded it is a self-inflicted Prometheus DoS. Resolved once at
// package init from METRICS_CLIENT_ID_CEILING (default 200) using a small
// package-local env-int helper -- the codebase has no shared config
// package and duplicates such helpers per consumer (cmd/api/main.go,
// cmd/worker/main.go), so a local one here is in-convention; reading it in
// this package rather than in each cmd/*/main.go keeps the blast radius to
// here (the env var is documented and propagated in Plan 04).
var ClientLabelCeiling = envClientLabelCeiling()

func envClientLabelCeiling() int {
	const defaultCeiling = 200
	v := os.Getenv("METRICS_CLIENT_ID_CEILING")
	if v == "" {
		return defaultCeiling
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return defaultCeiling
	}
	return n
}

// clientLabelAllocator is the first-seen-first-served, in-process admission
// map behind clientLabel. It is a struct (rather than bare package-level
// vars) so tests can construct isolated instances via
// newClientLabelAllocator without perturbing or depending on the shared
// defaultClientAllocator that production code uses -- see
// TestClientLabelCeilingCollapsesToOverflow.
type clientLabelAllocator struct {
	mu       sync.Mutex
	ceiling  int
	admitted map[string]struct{}
}

func newClientLabelAllocator(ceiling int) *clientLabelAllocator {
	return &clientLabelAllocator{ceiling: ceiling, admitted: make(map[string]struct{})}
}

// resolve returns clientID unchanged if it is a well-formed, non-nil UUID
// that either has already been admitted or still fits under the ceiling;
// otherwise it returns overflowClientLabel and, for a would-be-new id at
// the ceiling, admits nothing (an overflowing id never consumes a slot).
//
// Three non-obvious properties, in the codebase's why-not-what comment
// style:
//
//  1. Why the cap exists at all: an unbounded client_id label multiplies
//     Prometheus series with the number of distinct clients ever seen --
//     a self-inflicted DoS on the metrics backend (ACCT-04).
//  2. Why first-seen-first-served and never evicted: it is deterministic
//     and state-free -- no LRU bookkeeping, no clock, no extra dependency
//     (D-10).
//  3. The consequence an operator must not discover in production: the
//     admitted set lives only in this process's memory and resets on
//     restart, so which clients get individual resolution on this counter
//     can change after a redeploy and a dashboard may attribute
//     differently across that boundary. This is acceptable ONLY because
//     client_usage_daily (Plan 02) is the authoritative, exact record and
//     this counter exists for alerting/observation, not accounting (D-10).
//     Unlike the Postgres rollup, this counter is deliberately
//     fire-and-forget: a lost increment (e.g. a scrape between process
//     restart and the next job) is not retried or reconciled.
func (a *clientLabelAllocator) resolve(clientID string) string {
	if clientID == "" {
		return overflowClientLabel
	}
	id, err := uuid.Parse(clientID)
	if err != nil || id == uuid.Nil {
		return overflowClientLabel
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.admitted[clientID]; ok {
		return clientID
	}
	if len(a.admitted) >= a.ceiling {
		return overflowClientLabel
	}
	a.admitted[clientID] = struct{}{}
	return clientID
}

// defaultClientAllocator backs every production call to clientLabel /
// RecordClientJobOutcome.
var defaultClientAllocator = newClientLabelAllocator(ClientLabelCeiling)

// clientLabel resolves a raw client id to the label value that must be used
// on clientJobOutcomes. Every path to that CounterVec goes through this
// function -- never WithLabelValues with a raw id.
func clientLabel(clientID string) string {
	return defaultClientAllocator.resolve(clientID)
}

// RecordClientJobOutcome increments the per-client job-outcome counter for
// the given client, engine and terminal status. clientID is resolved through
// the bounded allocator before use (ACCT-04).
//
// Scope of that bound, stated precisely: the allocator caps the distinct
// client_id label values ONE PROCESS emits over ONE lifetime. It is not a
// global bound on distinct label values in Prometheus. Each replica admits its
// own set, and a restarted process starts from an empty map and may admit a
// different set while previously scraped series still live out their retention
// in the TSDB. So the count of distinct client_id values Prometheus has ever
// seen can exceed the ceiling.
//
// That is accepted rather than fixed, deliberately. A stronger bound needs a
// shared or persisted admission set, which would put coordination -- and a new
// failure mode -- on a path that is explicitly fire-and-forget and explicitly
// NOT the accounting record (client_usage_daily is, D-10). The ceiling exists
// as a backstop against pathological cardinality, not as a load-bearing limit:
// OctoConv's clients are internal company services, so the real population is
// orders of magnitude below 200, and a deployment where it is not is a signal
// to revisit the design rather than to raise the number.
func RecordClientJobOutcome(clientID, engine, status string) {
	clientJobOutcomes.WithLabelValues(clientLabel(clientID), engine, status).Inc()
}
