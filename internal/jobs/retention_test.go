package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Aging in this file is done by SQL-backdating jobs.created_at (and
// client_usage_daily.day) directly, unlike the reconciler soak tests
// (internal/reconciler/reconciler_soak_test.go), which age jobs by genuinely
// waiting out a real staleness threshold. This file's windows are 90/400
// DAYS — no test can afford to wait that long, so backdating is the
// deliberate, documented divergence from the soak-test convention, not an
// oversight.

// createAgedJob creates a job in status via the real Create/MarkActive/
// MarkDone/MarkFailed transitions (never a raw INSERT — the guarded
// transition path is what production code goes through too), then
// backdates its created_at by age via a direct UPDATE.
func createAgedJob(t *testing.T, r *Repo, clientID uuid.UUID, status string, age time.Duration) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	id, err := r.Create(ctx, CreateParams{
		ClientID:     clientID,
		Operation:    "convert",
		Engine:       "image",
		SourceFormat: "png",
		TargetFormat: "webp",
		Input:        Input{ObjectKey: "uploads/retention/0-in.png", Filename: "in.png", Format: "png"},
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	switch status {
	case StatusQueued:
		// Create already leaves the job in StatusQueued.
	case StatusActive:
		if err := r.MarkActive(ctx, id); err != nil {
			t.Fatalf("MarkActive: %v", err)
		}
	case StatusDone:
		if err := r.MarkActive(ctx, id); err != nil {
			t.Fatalf("MarkActive: %v", err)
		}
		if err := r.MarkDone(ctx, id, 0); err != nil {
			t.Fatalf("MarkDone: %v", err)
		}
	case StatusFailed:
		if err := r.MarkFailed(ctx, id, "engine_error", "boom", nil, 0); err != nil {
			t.Fatalf("MarkFailed: %v", err)
		}
	default:
		t.Fatalf("createAgedJob: unsupported status fixture %q", status)
	}

	// The backdated timestamp is computed in Go (time.Now().Add(-age)) and
	// bound directly, rather than issuing `now() - $2` in SQL: binding a Go
	// time.Duration into a bare `now() - $2` expression leaves Postgres to
	// infer $2's type from context, which resolves the whole expression to
	// `interval` (not `timestamptz`) and fails against the timestamptz
	// column -- computing the target instant in Go sidesteps the ambiguity
	// entirely.
	backdatedAt := time.Now().Add(-age)
	if _, err := r.pool.Exec(ctx,
		`UPDATE jobs SET created_at = $2 WHERE id = $1`, id, backdatedAt,
	); err != nil {
		t.Fatalf("backdate job %s created_at: %v", id, err)
	}

	// PurgeJobs deletes across the WHOLE table (never scoped to a client_id
	// — that scoping is deliberately absent, see retention.go), so a job a
	// test's own purge call deliberately leaves behind (e.g. a StatusQueued/
	// StatusActive fixture, or the survivor of a batch=1 call) would
	// otherwise persist as debris and corrupt an exact rows-affected
	// assertion in a LATER, independent `go test` invocation. Unconditional
	// cleanup here (idempotent — a no-op if the purge under test already
	// deleted it) keeps every test's effect on the table strictly
	// self-contained regardless of what the test under test actually did.
	t.Cleanup(func() {
		_, _ = r.pool.Exec(context.Background(), `DELETE FROM jobs WHERE id = $1`, id)
	})
	return id
}

// insertUsageRow inserts a client_usage_daily row directly (bypassing
// recordUsage, which always writes "today") so day can be pinned to an
// arbitrary aged or fresh value for the purge tests below.
func insertUsageRow(t *testing.T, r *Repo, clientID uuid.UUID, day time.Time) {
	t.Helper()
	if _, err := r.pool.Exec(context.Background(), `
		INSERT INTO client_usage_daily (client_id, engine, day, status, job_count, bytes_in, bytes_out, wall_seconds)
		VALUES ($1, 'image', $2, 'done', 1, 100, 10, 1.5)`,
		clientID, day,
	); err != nil {
		t.Fatalf("insert client_usage_daily row: %v", err)
	}

	// Same rationale as createAgedJob's t.Cleanup above: PurgeUsage is also
	// unscoped across the whole table, so a row a test deliberately leaves
	// behind (e.g. TestPurgeUsageDisabledWhenWindowZero's aged row) must not
	// survive as debris for a later, independent `go test` invocation.
	t.Cleanup(func() {
		_, _ = r.pool.Exec(context.Background(),
			`DELETE FROM client_usage_daily WHERE client_id = $1 AND day = $2`, clientID, day)
	})
}

// TestPurgeJobsDeletesAgedTerminalJobAndCascadesEvents proves the primary
// case: a 90-day window deletes a done job whose created_at is 100 days old,
// and proves the cascade (job_events gone) rather than assuming it — the
// deletion here is only against `jobs`; job_events has no explicit DELETE.
func TestPurgeJobsDeletesAgedTerminalJobAndCascadesEvents(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	id := createAgedJob(t, r, clientID, StatusDone, 100*24*time.Hour)

	var eventsBefore int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM job_events WHERE job_id = $1`, id).Scan(&eventsBefore); err != nil {
		t.Fatalf("count job_events before purge: %v", err)
	}
	if eventsBefore == 0 {
		t.Fatalf("expected job_events rows to exist before purge, got 0")
	}

	n, err := r.PurgeJobs(ctx, 90*24*time.Hour, 1000)
	if err != nil {
		t.Fatalf("PurgeJobs: %v", err)
	}
	if n != 1 {
		t.Fatalf("PurgeJobs rows affected = %d, want 1", n)
	}

	if _, err := r.Get(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after purge = %v, want ErrNotFound", err)
	}

	var eventsAfter int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM job_events WHERE job_id = $1`, id).Scan(&eventsAfter); err != nil {
		t.Fatalf("count job_events after purge: %v", err)
	}
	if eventsAfter != 0 {
		t.Fatalf("job_events rows after purge = %d, want 0 (cascade via ON DELETE CASCADE)", eventsAfter)
	}
}

// TestPurgeJobsSparesQueuedAndActiveRegardlessOfAge proves non-terminal jobs
// are never purged, no matter how old — the safety property the plan calls
// out as more important than an optimization.
func TestPurgeJobsSparesQueuedAndActiveRegardlessOfAge(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	queuedID := createAgedJob(t, r, clientID, StatusQueued, 100*24*time.Hour)
	activeID := createAgedJob(t, r, clientID, StatusActive, 100*24*time.Hour)

	n, err := r.PurgeJobs(ctx, 90*24*time.Hour, 1000)
	if err != nil {
		t.Fatalf("PurgeJobs: %v", err)
	}
	if n != 0 {
		t.Fatalf("PurgeJobs rows affected = %d, want 0 (StatusQueued/StatusActive jobs must never be purged)", n)
	}

	if _, err := r.Get(ctx, queuedID); err != nil {
		t.Fatalf("aged StatusQueued job must survive the purge: %v", err)
	}
	if _, err := r.Get(ctx, activeID); err != nil {
		t.Fatalf("aged StatusActive job must survive the purge: %v", err)
	}
}

// TestPurgeJobsSparesFreshJobInsideWindow proves a job created inside the
// window (here, 1 hour old against a 90-day window) is never deleted.
func TestPurgeJobsSparesFreshJobInsideWindow(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	freshID := createAgedJob(t, r, clientID, StatusDone, 1*time.Hour)

	n, err := r.PurgeJobs(ctx, 90*24*time.Hour, 1000)
	if err != nil {
		t.Fatalf("PurgeJobs: %v", err)
	}
	if n != 0 {
		t.Fatalf("PurgeJobs rows affected = %d, want 0 (fresh job inside the window must survive)", n)
	}
	if _, err := r.Get(ctx, freshID); err != nil {
		t.Fatalf("fresh job must survive the purge: %v", err)
	}
}

// TestPurgeJobsBatchLimitsRowsPerCall proves batch=1 against 3 eligible aged
// jobs deletes exactly 1 and leaves 2 behind — the per-call bound that
// makes a single sweep tick's purge work bounded.
func TestPurgeJobsBatchLimitsRowsPerCall(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	for i := 0; i < 3; i++ {
		createAgedJob(t, r, clientID, StatusDone, 100*24*time.Hour)
	}

	n, err := r.PurgeJobs(ctx, 90*24*time.Hour, 1)
	if err != nil {
		t.Fatalf("PurgeJobs: %v", err)
	}
	if n != 1 {
		t.Fatalf("PurgeJobs(batch=1) rows affected = %d, want 1", n)
	}

	var remaining int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE client_id = $1`, clientID).Scan(&remaining); err != nil {
		t.Fatalf("count remaining jobs: %v", err)
	}
	if remaining != 2 {
		t.Fatalf("remaining jobs = %d, want 2", remaining)
	}
}

// TestPurgeJobsDisabledWhenWindowZero proves PurgeJobs(ctx, 0, 1000) deletes
// nothing — the T-51-12 mitigation: an unset/zero window must disable
// purging, never mean "delete everything older than now".
func TestPurgeJobsDisabledWhenWindowZero(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	createAgedJob(t, r, clientID, StatusDone, 100*24*time.Hour)

	n, err := r.PurgeJobs(ctx, 0, 1000)
	if err != nil {
		t.Fatalf("PurgeJobs: %v", err)
	}
	if n != 0 {
		t.Fatalf("PurgeJobs(ctx, 0, 1000) rows affected = %d, want 0", n)
	}

	var remaining int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE client_id = $1`, clientID).Scan(&remaining); err != nil {
		t.Fatalf("count remaining jobs: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("remaining jobs = %d, want 1 (olderThan<=0 must disable purging entirely)", remaining)
	}
}

// TestPurgeUsageDeletesAgedRowKeepsFreshRow proves the 400-day usage window
// deletes a row dated 401 days ago and keeps one dated 10 days ago.
func TestPurgeUsageDeletesAgedRowKeepsFreshRow(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	agedDay := time.Now().UTC().AddDate(0, 0, -401)
	freshDay := time.Now().UTC().AddDate(0, 0, -10)
	insertUsageRow(t, r, clientID, agedDay)
	insertUsageRow(t, r, clientID, freshDay)

	n, err := r.PurgeUsage(ctx, 400*24*time.Hour, 1000)
	if err != nil {
		t.Fatalf("PurgeUsage: %v", err)
	}
	if n != 1 {
		t.Fatalf("PurgeUsage rows affected = %d, want 1", n)
	}

	var agedCount int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM client_usage_daily WHERE client_id = $1 AND day = $2`, clientID, agedDay).Scan(&agedCount); err != nil {
		t.Fatalf("count aged usage row: %v", err)
	}
	if agedCount != 0 {
		t.Fatalf("aged usage row survived the purge, count = %d", agedCount)
	}

	var freshCount int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM client_usage_daily WHERE client_id = $1 AND day = $2`, clientID, freshDay).Scan(&freshCount); err != nil {
		t.Fatalf("count fresh usage row: %v", err)
	}
	if freshCount != 1 {
		t.Fatalf("fresh usage row missing after the purge, count = %d", freshCount)
	}
}

// TestPurgeUsageDisabledWhenWindowZero mirrors
// TestPurgeJobsDisabledWhenWindowZero for the usage table.
func TestPurgeUsageDisabledWhenWindowZero(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	agedDay := time.Now().UTC().AddDate(0, 0, -401)
	insertUsageRow(t, r, clientID, agedDay)

	n, err := r.PurgeUsage(ctx, 0, 1000)
	if err != nil {
		t.Fatalf("PurgeUsage: %v", err)
	}
	if n != 0 {
		t.Fatalf("PurgeUsage(ctx, 0, 1000) rows affected = %d, want 0", n)
	}

	var remaining int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM client_usage_daily WHERE client_id = $1`, clientID).Scan(&remaining); err != nil {
		t.Fatalf("count remaining usage rows: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("remaining usage rows = %d, want 1 (olderThan<=0 must disable purging entirely)", remaining)
	}
}
