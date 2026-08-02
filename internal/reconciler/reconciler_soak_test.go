package reconciler

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/apaderin/octoconv/internal/db"
	"github.com/apaderin/octoconv/internal/jobs"
)

// newSoakTestPool mirrors internal/jobs/repo_test.go's newTestRepo helper
// (same DATABASE_URL skip guard, same db.Connect/db.Migrate/t.Cleanup
// sequence). It lives here, duplicated rather than imported, because this
// file is in package reconciler and cannot reach jobs' unexported test
// helpers across package boundaries.
func newSoakTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set; skipping soak test")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx)
	if err != nil {
		t.Fatalf("db.Connect: %v", err)
	}
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// createSoakTestClient mirrors internal/jobs/repo_test.go's createTestClient
// helper: inserts a minimal clients row (name only) so a soak-test job can
// satisfy the jobs.client_id foreign key.
func createSoakTestClient(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(context.Background(),
		"INSERT INTO clients (name) VALUES ($1) RETURNING id", "reconciler-soak-test-client",
	).Scan(&id)
	if err != nil {
		t.Fatalf("create soak test client: %v", err)
	}
	return id
}

// TestSoakRecoversStrandedQueuedJob proves RECON-05 SC3 (D-07): a job
// genuinely stranded in queued past QueuedStaleAfter is recovered by a live,
// running Sweeper.Run process within the expected sweep cadence, using REAL
// elapsed wall-clock time. No SQL backdating of created_at is used — the
// staleness threshold must be crossed by genuinely waiting.
//
// This pairs a REAL jobs.Repo (live Postgres) with the existing in-memory
// fakeEnqueuer (NOT a real Redis-backed producer): a real asynq.Unique lock
// on the image queue has a hardcoded 2-minute uniqueTTLSafetyMargin floor
// (internal/queue/queue.go), which would blow this test's "well under a
// minute" time budget (06-RESEARCH.md Pitfall 3).
func TestSoakRecoversStrandedQueuedJob(t *testing.T) {
	pool := newSoakTestPool(t)
	repo := jobs.NewRepo(pool)
	clientID := createSoakTestClient(t, pool)

	jobID, err := repo.Create(context.Background(), jobs.CreateParams{
		ClientID:     clientID,
		Operation:    "convert",
		Engine:       "image",
		SourceFormat: "png",
		TargetFormat: "webp",
		Input:        jobs.Input{ObjectKey: "uploads/soak/0-in.png", Filename: "in.png", Format: "png"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	enq := &fakeEnqueuer{} // in-memory, no live Redis (reconciler_test.go)
	cfg := Config{
		QueuedStaleAfter: 1 * time.Second,
		ActiveStaleAfter: 1 * time.Second,
		SweepInterval:    300 * time.Millisecond,
		MaxRecoveries:    2,
	}
	s := NewSweeper(repo, enq, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		j, err := repo.Get(context.Background(), jobID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if j.Status == jobs.StatusQueued && len(enq.imageCallIDs()) >= 1 {
			return // recovered under genuine elapsed wall-clock time
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("job was not recovered within 10s of real elapsed time")
}

// TestSoakExhaustsAtCap proves RECON-05 SC4 (D-08): a job exceeding
// MaxRecoveries under real elapsed time is terminally failed, with the
// failure recorded in job_events. Same real-Repo + fakeEnqueuer pairing as
// above.
//
// Per 06-RESEARCH.md Pitfall 4, RequeueStale never resets created_at, so a
// job recovered via the active branch trips the queued-branch staleness
// check on the very next tick — the sequence of recorded recovery `reason`
// values is not fixed across runs. This test therefore asserts only the
// final terminal state (status=failed) and that at least one exhaustion
// job_events row exists, never a specific reason sequence.
func TestSoakExhaustsAtCap(t *testing.T) {
	pool := newSoakTestPool(t)
	repo := jobs.NewRepo(pool)
	clientID := createSoakTestClient(t, pool)

	jobID, err := repo.Create(context.Background(), jobs.CreateParams{
		ClientID:     clientID,
		Operation:    "convert",
		Engine:       "image",
		SourceFormat: "png",
		TargetFormat: "webp",
		Input:        jobs.Input{ObjectKey: "uploads/soak/0-exhaust.png", Filename: "in.png", Format: "png"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	enq := &fakeEnqueuer{}
	cfg := Config{
		QueuedStaleAfter: 1 * time.Second,
		ActiveStaleAfter: 1 * time.Second,
		SweepInterval:    300 * time.Millisecond,
		MaxRecoveries:    2,
	}
	s := NewSweeper(repo, enq, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		j, err := repo.Get(context.Background(), jobID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if j.Status == jobs.StatusFailed {
			// Terminal state reached under real elapsed time. Now confirm the
			// exhaustion was recorded in job_events (mirrors TestMarkFailed's
			// detail-round-trip query style, internal/jobs/repo_test.go).
			var count int
			if err := pool.QueryRow(context.Background(),
				`SELECT count(*) FROM job_events WHERE job_id = $1 AND to_status = 'failed' AND detail->>'action' = 'reconciler_exhausted'`,
				jobID,
			).Scan(&count); err != nil {
				t.Fatalf("query job_events: %v", err)
			}
			if count < 1 {
				t.Fatalf("expected at least one reconciler_exhausted job_events row, got %d", count)
			}
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatal("job did not reach terminal failed state within 15s of real elapsed time")
}

// TestSoakStaleRecoveryCountsUsageExactlyOnce is the criterion ROADMAP Phase
// 51 SC2 was tightened to require (D-06): a job dragged through the
// reconciler's documented stale-recovery race must be counted in
// client_usage_daily EXACTLY ONCE, not merely "recovery happened". It forces
// the race for real against live Postgres rather than arguing it from the
// design:
//
//  1. A real job is MarkActive'd (a genuine worker attempt started).
//  2. The active-staleness threshold is crossed by GENUINELY WAITING (no SQL
//     backdating of started_at, mirroring TestSoakRecoversStrandedQueuedJob's
//     discipline) and a direct s.sweep(ctx) call (deterministic, not
//     tick-timed) recovers the job back to queued via the in-memory
//     fakeEnqueuer.
//  3. The ORIGINAL "zombie" worker -- unaware it has been recovered -- then
//     tries to finish the job it was actually processing: MarkDone against a
//     job that is now `queued`, not `active`. This MUST be rejected by
//     transition's allow-list ({active} only for MarkDone) with an "illegal
//     transition" error -- this is the assertion the whole design rests on.
//  4. The RECOVERED attempt (a fresh worker picking up the requeued job)
//     completes normally: MarkActive -> AddOutput -> MarkDone, which must
//     succeed.
//  5. client_usage_daily for this client must show EXACTLY ONE row:
//     job_count = 1 (NOT 2), status = 'done', bytes_in/bytes_out matching the
//     single input/output actually recorded, wall_seconds > 0.
//
// What would break this test: any design where the client_usage_daily write
// (recordUsage, internal/jobs/usage.go) is a SEPARATE statement issued after
// the transition transaction commits, rather than inside
// Repo.transition's apply closure on the same locked row (D-05), would let
// the zombie worker's MarkDone land as an independent write once the
// allow-list rejects only the STATUS update but the usage write still runs
// -- or, worse, if the allow-list check were bypassed entirely for
// accounting, producing job_count = 2 for one real conversion. The current
// design closes this because recordUsage runs INSIDE the same guarded
// transition as the status UPDATE (Task 1/MarkDone doc comment,
// internal/jobs/repo.go): a rejected transition never reaches apply, so
// recordUsage never runs for the zombie's attempt at all.
func TestSoakStaleRecoveryCountsUsageExactlyOnce(t *testing.T) {
	pool := newSoakTestPool(t)
	repo := jobs.NewRepo(pool)
	clientID := createSoakTestClient(t, pool)

	const inputBytes = 500
	const outputBytes = 50

	jobID, err := repo.Create(context.Background(), jobs.CreateParams{
		ClientID:     clientID,
		Operation:    "convert",
		Engine:       "image",
		SourceFormat: "png",
		TargetFormat: "webp",
		Input:        jobs.Input{ObjectKey: "uploads/soak/0-race-in.png", Filename: "in.png", Format: "png", SizeBytes: inputBytes},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.MarkActive(context.Background(), jobID); err != nil {
		t.Fatalf("MarkActive (original attempt): %v", err)
	}

	enq := &fakeEnqueuer{} // in-memory, no live Redis (reconciler_test.go)
	cfg := Config{
		// QueuedStaleAfter deliberately long so the queued state this job
		// passes through mid-test is NEVER independently swept -- only the
		// ActiveStaleAfter branch matters here.
		QueuedStaleAfter: 1 * time.Hour,
		ActiveStaleAfter: 1 * time.Second,
		SweepInterval:    1 * time.Hour, // irrelevant: sweep() is called directly, not via Run
		MaxRecoveries:    3,
	}
	s := NewSweeper(repo, enq, cfg)

	// Cross ActiveStaleAfter by genuinely waiting -- no created_at/started_at
	// backdating (06-RESEARCH.md Pitfall 3 precedent, TestSoakRecoversStrandedQueuedJob).
	time.Sleep(cfg.ActiveStaleAfter + 300*time.Millisecond)

	// Deterministic recovery: call sweep directly (same package) rather than
	// racing a ticker.
	s.sweep(context.Background())

	j, err := repo.Get(context.Background(), jobID)
	if err != nil {
		t.Fatalf("Get after sweep: %v", err)
	}
	if j.Status != jobs.StatusQueued {
		t.Fatalf("status after sweep = %q, want %q (recovered)", j.Status, jobs.StatusQueued)
	}
	imageCalls := enq.imageCallIDs()
	if len(imageCalls) != 1 || imageCalls[0] != jobID {
		t.Fatalf("imageCalls = %+v, want exactly one enqueue for job %s", imageCalls, jobID)
	}

	// Step 3: the ORIGINAL zombie worker, unaware it was recovered, tries to
	// finish the attempt it was actually running. The job is now `queued`,
	// not `active` -- MarkDone's allow-list ({active} only) MUST reject this.
	zombieErr := repo.MarkDone(context.Background(), jobID, 250*time.Millisecond)
	if zombieErr == nil {
		t.Fatal("zombie MarkDone succeeded, want an illegal-transition error (queued -> done)")
	}
	if !strings.Contains(zombieErr.Error(), "illegal transition") {
		t.Fatalf("zombie MarkDone error = %v, want it to contain %q", zombieErr, "illegal transition")
	}

	// Step 4: the RECOVERED attempt completes normally.
	if err := repo.MarkActive(context.Background(), jobID); err != nil {
		t.Fatalf("MarkActive (recovered attempt): %v", err)
	}
	if err := repo.AddOutput(context.Background(), jobID, jobs.Output{
		Ordinal: 0, ObjectKey: "results/soak/0-race-out.webp", Filename: "out.webp",
		Format: "webp", SizeBytes: outputBytes, ContentType: "image/webp",
	}); err != nil {
		t.Fatalf("AddOutput (recovered attempt): %v", err)
	}
	if err := repo.MarkDone(context.Background(), jobID, 400*time.Millisecond); err != nil {
		t.Fatalf("MarkDone (recovered attempt) = %v, want nil", err)
	}

	// Step 5: exactly one usage row for this client, not two.
	rows, err := pool.Query(context.Background(),
		`SELECT job_count, bytes_in, bytes_out, wall_seconds, status FROM client_usage_daily WHERE client_id = $1`,
		clientID,
	)
	if err != nil {
		t.Fatalf("query client_usage_daily: %v", err)
	}
	defer rows.Close()

	var rowCount int
	var jobCount, bytesIn, bytesOut int64
	var wallSeconds float64
	var status string
	for rows.Next() {
		rowCount++
		if err := rows.Scan(&jobCount, &bytesIn, &bytesOut, &wallSeconds, &status); err != nil {
			t.Fatalf("scan client_usage_daily row: %v", err)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate client_usage_daily rows: %v", err)
	}

	if rowCount != 1 {
		t.Fatalf("client_usage_daily row count = %d, want exactly 1", rowCount)
	}
	if jobCount != 1 {
		t.Fatalf("job_count = %d, want exactly 1 (the zombie's rejected MarkDone must not double-count)", jobCount)
	}
	if status != jobs.StatusDone {
		t.Fatalf("status = %q, want %q", status, jobs.StatusDone)
	}
	if bytesIn != inputBytes {
		t.Fatalf("bytes_in = %d, want %d", bytesIn, inputBytes)
	}
	if bytesOut != outputBytes {
		t.Fatalf("bytes_out = %d, want %d", bytesOut, outputBytes)
	}
	if wallSeconds <= 0 {
		t.Fatalf("wall_seconds = %v, want > 0", wallSeconds)
	}
}

// TestSoakExhaustedJobIsAccounted proves the path Task 2 wired (reconciler
// exhaustion -> MarkFailed -> client_usage_daily) against live Postgres --
// it has no other coverage (reconciler_test.go's fakeStore never touches
// real Postgres). MaxRecoveries is deliberately 0 so the very first sweep
// after QueuedStaleAfter is crossed drives the job straight to
// reconciler_exhausted, keeping the test fast without needing multiple
// recovery round-trips.
func TestSoakExhaustedJobIsAccounted(t *testing.T) {
	pool := newSoakTestPool(t)
	repo := jobs.NewRepo(pool)
	clientID := createSoakTestClient(t, pool)

	jobID, err := repo.Create(context.Background(), jobs.CreateParams{
		ClientID:     clientID,
		Operation:    "convert",
		Engine:       "image",
		SourceFormat: "png",
		TargetFormat: "webp",
		Input:        jobs.Input{ObjectKey: "uploads/soak/0-exhaust-acct.png", Filename: "in.png", Format: "png", SizeBytes: 10},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	enq := &fakeEnqueuer{}
	cfg := Config{
		QueuedStaleAfter: 1 * time.Second,
		ActiveStaleAfter: 1 * time.Hour, // irrelevant: this job never goes active
		SweepInterval:    1 * time.Hour, // irrelevant: sweep() is called directly
		MaxRecoveries:    0,             // exhausts on the very first sweep past staleness
	}
	s := NewSweeper(repo, enq, cfg)

	// Cross QueuedStaleAfter by genuinely waiting (same discipline as every
	// other soak test in this file).
	time.Sleep(cfg.QueuedStaleAfter + 300*time.Millisecond)
	s.sweep(context.Background())

	j, err := repo.Get(context.Background(), jobID)
	if err != nil {
		t.Fatalf("Get after sweep: %v", err)
	}
	if j.Status != jobs.StatusFailed {
		t.Fatalf("status after sweep = %q, want %q (reconciler_exhausted)", j.Status, jobs.StatusFailed)
	}

	var jobCount int64
	var status string
	if err := pool.QueryRow(context.Background(),
		`SELECT job_count, status FROM client_usage_daily WHERE client_id = $1 AND status = 'failed'`,
		clientID,
	).Scan(&jobCount, &status); err != nil {
		t.Fatalf("query client_usage_daily: %v", err)
	}
	if jobCount != 1 {
		t.Fatalf("job_count = %d, want exactly 1", jobCount)
	}
	if status != jobs.StatusFailed {
		t.Fatalf("status = %q, want %q", status, jobs.StatusFailed)
	}
}
