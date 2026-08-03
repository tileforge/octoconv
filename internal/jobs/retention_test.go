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

// TestPurgeJobsSparesParentWithLiveDescendant proves the cascade cannot remove
// a job that does not itself satisfy the retention predicate. jobs.parent_id
// self-references with ON DELETE CASCADE, so deleting an aged terminal parent
// would otherwise take a live child with it — bypassing both the status
// predicate and the batch bound, silently.
//
// The grandchild case is the reason the guard is recursive: the child here is
// itself eligible (aged, terminal), so a one-level check would clear the parent
// for deletion and the cascade would still reach the live grandchild.
func TestPurgeJobsSparesParentWithLiveDescendant(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	parentID := createAgedJob(t, r, clientID, StatusDone, 100*24*time.Hour)
	childID := createAgedJob(t, r, clientID, StatusDone, 100*24*time.Hour)
	grandchildID := createAgedJob(t, r, clientID, StatusActive, 100*24*time.Hour)

	// parent -> child -> grandchild, where only the grandchild is ineligible.
	if _, err := r.pool.Exec(ctx, `UPDATE jobs SET parent_id = $1 WHERE id = $2`, parentID, childID); err != nil {
		t.Fatalf("link child to parent: %v", err)
	}
	if _, err := r.pool.Exec(ctx, `UPDATE jobs SET parent_id = $1 WHERE id = $2`, childID, grandchildID); err != nil {
		t.Fatalf("link grandchild to child: %v", err)
	}
	t.Cleanup(func() {
		_, _ = r.pool.Exec(context.Background(), `DELETE FROM jobs WHERE id = $1`, parentID)
	})

	if _, err := r.PurgeJobs(ctx, 90*24*time.Hour, 1000); err != nil {
		t.Fatalf("PurgeJobs: %v", err)
	}

	if _, err := r.Get(ctx, grandchildID); err != nil {
		t.Fatalf("live grandchild must survive: the cascade deleted a job that never satisfied the retention predicate: %v", err)
	}
	if _, err := r.Get(ctx, parentID); err != nil {
		t.Fatalf("parent must be spared while any descendant is ineligible: %v", err)
	}
	if _, err := r.Get(ctx, childID); err != nil {
		t.Fatalf("child must be spared while its own descendant is ineligible: %v", err)
	}
}

// TestPurgeJobsBatchBoundSurvivesCascade pins that the per-tick work ceiling
// cannot be defeated through the ON DELETE CASCADE on jobs.parent_id.
//
// A LIMIT bounds the ids selected for deletion, not what the cascade takes
// with them, so deleting one parent with N eligible descendants removed N+1
// rows from a batch of 1. Worse, a self-referential cascade does not count
// toward the statement's rows-affected, so the caller was told it deleted one
// row while the whole subtree vanished -- which also corrupts the `n < batch`
// signal the sweep loop uses to decide the backlog is drained.
//
// Deleting only leaves makes the bound structural: a leaf cascades to nothing,
// so rows-affected is exactly what was removed.
func TestPurgeJobsBatchBoundSurvivesCascade(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	// One aged terminal parent with three aged terminal children — the whole
	// subtree is eligible, so nothing here is protected by status or age.
	parentID := createAgedJob(t, r, clientID, StatusDone, 100*24*time.Hour)
	var childIDs []uuid.UUID
	for i := 0; i < 3; i++ {
		childID := createAgedJob(t, r, clientID, StatusDone, 100*24*time.Hour)
		if _, err := r.pool.Exec(ctx, `UPDATE jobs SET parent_id = $1 WHERE id = $2`, parentID, childID); err != nil {
			t.Fatalf("link child %d to parent: %v", i, err)
		}
		childIDs = append(childIDs, childID)
	}
	t.Cleanup(func() {
		_, _ = r.pool.Exec(context.Background(), `DELETE FROM jobs WHERE id = $1`, parentID)
	})

	// batch=2 against a 4-job subtree: selecting the parent would cascade all
	// four away in a single statement.
	n, err := r.PurgeJobs(ctx, 90*24*time.Hour, 2)
	if err != nil {
		t.Fatalf("PurgeJobs: %v", err)
	}
	if n != 2 {
		t.Fatalf("rows affected = %d, want exactly 2 (the batch bound)", n)
	}

	var remaining int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM jobs WHERE id = $1 OR parent_id = $1`, parentID,
	).Scan(&remaining); err != nil {
		t.Fatalf("count remaining subtree rows: %v", err)
	}
	if remaining != 2 {
		t.Fatalf("subtree rows remaining = %d, want 2: the cascade deleted more than the batch bound allowed", remaining)
	}
	if _, err := r.Get(ctx, parentID); err != nil {
		t.Fatalf("parent must survive while it still has children — deleting it would cascade past the batch bound: %v", err)
	}

	// Drain the rest bottom-up: the last child goes, which makes the parent a
	// leaf, and only then may the parent be deleted.
	if _, err := r.PurgeJobs(ctx, 90*24*time.Hour, 2); err != nil {
		t.Fatalf("PurgeJobs (second tick): %v", err)
	}
	for _, id := range childIDs {
		if _, err := r.Get(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("all children must be purged after two ticks, got err=%v", err)
		}
	}
	if _, err := r.PurgeJobs(ctx, 90*24*time.Hour, 2); err != nil {
		t.Fatalf("PurgeJobs (third tick): %v", err)
	}
	if _, err := r.Get(ctx, parentID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("parent must be purged once it became a leaf, got err=%v", err)
	}
}

// TestPurgeJobsAdvancesPastBlockedRoots pins the liveness property that the
// subtree guard must not cost: a blocked root at the head of the creation
// order must not stop the sweep from reaching eligible jobs behind it.
//
// With the guard applied AFTER the LIMIT, the batch is filled with the oldest
// roots, the guard then removes all of them, the DELETE affects zero rows, and
// the caller's `n < batch` loop-exit fires -- so the next tick re-selects the
// same blocked head and the eligible job behind it is never purged, forever.
// batch=1 makes that head-of-line block exact rather than probabilistic.
func TestPurgeJobsAdvancesPastBlockedRoots(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	// Oldest, and permanently blocked: terminal parent with a live child.
	blockedParentID := createAgedJob(t, r, clientID, StatusDone, 120*24*time.Hour)
	liveChildID := createAgedJob(t, r, clientID, StatusActive, 120*24*time.Hour)
	if _, err := r.pool.Exec(ctx, `UPDATE jobs SET parent_id = $1 WHERE id = $2`, blockedParentID, liveChildID); err != nil {
		t.Fatalf("link live child to blocked parent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = r.pool.Exec(context.Background(), `DELETE FROM jobs WHERE id = $1`, blockedParentID)
	})

	// Newer than the blocked root, and fully eligible.
	eligibleID := createAgedJob(t, r, clientID, StatusDone, 100*24*time.Hour)

	// batch=1: without the fix the single slot is consumed by the blocked
	// root and the DELETE affects nothing.
	n, err := r.PurgeJobs(ctx, 90*24*time.Hour, 1)
	if err != nil {
		t.Fatalf("PurgeJobs: %v", err)
	}
	if n != 1 {
		t.Fatalf("purge must advance past the blocked root and delete the eligible job behind it: rows affected = %d, want 1", n)
	}
	if _, err := r.Get(ctx, eligibleID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("eligible job behind a blocked root must be purged, got err=%v", err)
	}
	if _, err := r.Get(ctx, blockedParentID); err != nil {
		t.Fatalf("blocked root must still be spared: %v", err)
	}
	if _, err := r.Get(ctx, liveChildID); err != nil {
		t.Fatalf("live child must still survive: %v", err)
	}
}
