package reconciler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/apaderin/octoconv/internal/jobs"
)

// TestPurgeDeletesAgedRows proves the sweeper's purge() actually removes
// aged jobs/client_usage_daily rows through a live *jobs.Repo -- not the
// fakeStore, which never touches real Postgres. This is the RETN-01/D-13
// proof that retention is executable, not merely wired: aging here is done
// by backdating created_at/day directly (mirrors
// internal/jobs/retention_test.go's documented divergence from the soak
// tests' genuine-wait convention — no test can afford to wait 90/400 real
// days).
func TestPurgeDeletesAgedRows(t *testing.T) {
	pool := newSoakTestPool(t)
	repo := jobs.NewRepo(pool)
	clientID := createSoakTestClient(t, pool)
	ctx := context.Background()

	// Aged, terminal job — must be purged.
	agedID, err := repo.Create(ctx, jobs.CreateParams{
		ClientID:     clientID,
		Operation:    "convert",
		Engine:       "image",
		SourceFormat: "png",
		TargetFormat: "webp",
		Input:        jobs.Input{ObjectKey: "uploads/retention-soak/0-aged.png", Filename: "in.png", Format: "png"},
	})
	if err != nil {
		t.Fatalf("Create (aged): %v", err)
	}
	if err := repo.MarkActive(ctx, agedID); err != nil {
		t.Fatalf("MarkActive (aged): %v", err)
	}
	// D-13: MarkDone requires exactly one primary output. jobs.addPrimaryOutput
	// is not visible from this package, so replicate its effect directly via
	// the public Repo.AddOutput, relying on the DB DEFAULT for role (D-16) --
	// the same order every engine already follows in production.
	if err := repo.AddOutput(ctx, agedID, jobs.Output{
		Ordinal: 0, ObjectKey: "results/retention-soak/0-aged-out.webp", Filename: "out.webp",
		Format: "webp", SizeBytes: 1, ContentType: "image/webp",
	}); err != nil {
		t.Fatalf("AddOutput (aged): %v", err)
	}
	if err := repo.MarkDone(ctx, agedID, 0); err != nil {
		t.Fatalf("MarkDone (aged): %v", err)
	}
	backdatedAt := time.Now().Add(-100 * 24 * time.Hour)
	if _, err := pool.Exec(ctx, `UPDATE jobs SET created_at = $2 WHERE id = $1`, agedID, backdatedAt); err != nil {
		t.Fatalf("backdate aged job: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM jobs WHERE id = $1`, agedID) })

	// Fresh job in the same client — must survive.
	freshID, err := repo.Create(ctx, jobs.CreateParams{
		ClientID:     clientID,
		Operation:    "convert",
		Engine:       "image",
		SourceFormat: "png",
		TargetFormat: "webp",
		Input:        jobs.Input{ObjectKey: "uploads/retention-soak/0-fresh.png", Filename: "in.png", Format: "png"},
	})
	if err != nil {
		t.Fatalf("Create (fresh): %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM jobs WHERE id = $1`, freshID) })

	// Aged usage row, inserted directly (bypassing recordUsage, which always
	// writes "today") — mirrors internal/jobs/retention_test.go's
	// insertUsageRow helper.
	agedDay := time.Now().UTC().AddDate(0, 0, -401)
	if _, err := pool.Exec(ctx, `
		INSERT INTO client_usage_daily (client_id, engine, day, status, job_count, bytes_in, bytes_out, wall_seconds)
		VALUES ($1, 'image', $2, 'done', 1, 100, 10, 1.5)`, clientID, agedDay,
	); err != nil {
		t.Fatalf("insert aged usage row: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM client_usage_daily WHERE client_id = $1 AND day = $2`, clientID, agedDay)
	})

	enq := &fakeEnqueuer{}
	s := NewSweeper(repo, enq, Config{
		QueuedStaleAfter: time.Hour,
		ActiveStaleAfter: time.Hour,
		SweepInterval:    time.Hour, // irrelevant: purge() is called directly, not via a ticker
		MaxRecoveries:    3,
		JobsRetention:    90 * 24 * time.Hour,
		UsageRetention:   400 * 24 * time.Hour,
		PurgeBatchSize:   1000,
	})

	s.purge(ctx)

	if _, err := repo.Get(ctx, agedID); !errors.Is(err, jobs.ErrNotFound) {
		t.Fatalf("Get(agedID) after purge = %v, want ErrNotFound", err)
	}
	if _, err := repo.Get(ctx, freshID); err != nil {
		t.Fatalf("Get(freshID) after purge = %v, want nil (fresh job must survive)", err)
	}

	var usageCount int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM client_usage_daily WHERE client_id = $1 AND day = $2`, clientID, agedDay,
	).Scan(&usageCount); err != nil {
		t.Fatalf("count aged usage row: %v", err)
	}
	if usageCount != 0 {
		t.Fatalf("aged usage row survived the purge, count = %d", usageCount)
	}
}

// TestPurgeDisabledWhenWindowsUnset proves testConfig()'s literal — the
// EXACT shape every pre-existing test in this package uses, which never
// sets JobsRetention/UsageRetention/PurgeBatchSize — disables both purges
// entirely (T-51-12 applied at the reconciler-wiring level, on top of
// jobs.PurgeJobs/PurgeUsage's own guard). Asserted twice: against the fake
// (call arguments) and against live Postgres (a real aged row survives).
func TestPurgeDisabledWhenWindowsUnset(t *testing.T) {
	t.Run("fake", func(t *testing.T) {
		store := &fakeStore{recoveryCount: map[uuid.UUID]int{}}
		enq := &fakeEnqueuer{}
		s := NewSweeper(store, enq, testConfig())

		s.purge(context.Background())

		if len(store.purgeJobsCalls) != 1 || store.purgeJobsCalls[0].olderThan != 0 {
			t.Fatalf("PurgeJobs calls = %+v, want exactly one call with olderThan=0", store.purgeJobsCalls)
		}
		if len(store.purgeUsageCalls) != 1 || store.purgeUsageCalls[0].olderThan != 0 {
			t.Fatalf("PurgeUsage calls = %+v, want exactly one call with olderThan=0", store.purgeUsageCalls)
		}
		if store.purgeJobsCalls[0].batch != defaultPurgeBatchSize {
			t.Fatalf("PurgeJobs batch = %d, want the package default %d (PurgeBatchSize unset in testConfig())",
				store.purgeJobsCalls[0].batch, defaultPurgeBatchSize)
		}
	})

	t.Run("live", func(t *testing.T) {
		pool := newSoakTestPool(t)
		repo := jobs.NewRepo(pool)
		clientID := createSoakTestClient(t, pool)
		ctx := context.Background()

		agedID, err := repo.Create(ctx, jobs.CreateParams{
			ClientID:     clientID,
			Operation:    "convert",
			Engine:       "image",
			SourceFormat: "png",
			TargetFormat: "webp",
			Input:        jobs.Input{ObjectKey: "uploads/retention-soak/0-disabled.png", Filename: "in.png", Format: "png"},
		})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := repo.MarkActive(ctx, agedID); err != nil {
			t.Fatalf("MarkActive: %v", err)
		}
		if err := repo.AddOutput(ctx, agedID, jobs.Output{
			Ordinal: 0, ObjectKey: "results/retention-soak/0-disabled-out.webp", Filename: "out.webp",
			Format: "webp", SizeBytes: 1, ContentType: "image/webp",
		}); err != nil {
			t.Fatalf("AddOutput: %v", err)
		}
		if err := repo.MarkDone(ctx, agedID, 0); err != nil {
			t.Fatalf("MarkDone: %v", err)
		}
		backdatedAt := time.Now().Add(-100 * 24 * time.Hour)
		if _, err := pool.Exec(ctx, `UPDATE jobs SET created_at = $2 WHERE id = $1`, agedID, backdatedAt); err != nil {
			t.Fatalf("backdate job: %v", err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM jobs WHERE id = $1`, agedID) })

		enq := &fakeEnqueuer{}
		s := NewSweeper(repo, enq, testConfig()) // same literal shape — no retention fields set

		s.purge(ctx)

		if _, err := repo.Get(ctx, agedID); err != nil {
			t.Fatalf("aged job must survive when JobsRetention is unset: %v", err)
		}
	})
}

// TestPurgeIsBoundedPerTick proves a single purge() call issues at most
// maxPurgeBatchesPerSweep PurgeJobs calls (and, independently, at most
// maxPurgeBatchesPerSweep PurgeUsage calls) even when the store reports a
// full batch on every call — the T-51-13 DoS mitigation: the ceiling is
// what stops the loop, not the store ever reporting a partial batch.
func TestPurgeIsBoundedPerTick(t *testing.T) {
	store := &fakeStore{recoveryCount: map[uuid.UUID]int{}}
	// Script well beyond maxPurgeBatchesPerSweep calls to each report a full
	// batch (n == batch == 1), so the ceiling — not a partial-batch
	// early-exit — is what bounds the loop.
	for i := 0; i < maxPurgeBatchesPerSweep+5; i++ {
		store.purgeJobsResults = append(store.purgeJobsResults, purgeResult{n: 1})
		store.purgeUsageResults = append(store.purgeUsageResults, purgeResult{n: 1})
	}
	enq := &fakeEnqueuer{}
	s := NewSweeper(store, enq, Config{
		QueuedStaleAfter: time.Hour,
		ActiveStaleAfter: time.Hour,
		SweepInterval:    time.Hour,
		MaxRecoveries:    3,
		JobsRetention:    90 * 24 * time.Hour,
		UsageRetention:   400 * 24 * time.Hour,
		PurgeBatchSize:   1,
	})

	s.purge(context.Background())

	if len(store.purgeJobsCalls) != maxPurgeBatchesPerSweep {
		t.Fatalf("PurgeJobs calls = %d, want exactly %d (bounded by maxPurgeBatchesPerSweep)",
			len(store.purgeJobsCalls), maxPurgeBatchesPerSweep)
	}
	if len(store.purgeUsageCalls) != maxPurgeBatchesPerSweep {
		t.Fatalf("PurgeUsage calls = %d, want exactly %d (bounded by maxPurgeBatchesPerSweep)",
			len(store.purgeUsageCalls), maxPurgeBatchesPerSweep)
	}
}
