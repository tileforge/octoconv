package jobs

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestRecordUsageSumsAllOrdinals pins D-03 (the corrected reading): a job
// with two job_inputs and two job_outputs must yield a client_usage_daily
// row whose bytes_in/bytes_out are the SUM over every ordinal, never the
// ordinal-0 row alone. This is the primary RED case for Task 1: it fails to
// compile until recordUsage/the widened MarkDone signature exist, then fails
// on assertion until the correlated sum(size_bytes) subqueries are wired in.
func TestRecordUsageSumsAllOrdinals(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	id, err := r.Create(ctx, CreateParams{
		ClientID:  clientID,
		Operation: "convert", Engine: "image", SourceFormat: "png", TargetFormat: "webp",
		Input: Input{Ordinal: 0, ObjectKey: "uploads/ru1/0-in.png", Filename: "in.png", Format: "png", SizeBytes: 100},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// A second input row at ordinal 1 — the whole point of this test is that
	// the rollup must sum across it, not stop at ordinal 0 (SEED-017).
	if _, err := r.pool.Exec(ctx, `
		INSERT INTO job_inputs (job_id, ordinal, object_key, filename, format, size_bytes, content_type)
		VALUES ($1, 1, 'uploads/ru1/1-in.png', 'in2.png', 'png', 200, 'image/png')`, id,
	); err != nil {
		t.Fatalf("insert second job_input: %v", err)
	}

	if err := r.MarkActive(ctx, id); err != nil {
		t.Fatalf("MarkActive: %v", err)
	}
	if err := r.AddOutput(ctx, id, Output{Ordinal: 0, ObjectKey: "results/ru1/0-out.webp", Filename: "out.webp", Format: "webp", SizeBytes: 10, ContentType: "image/webp"}); err != nil {
		t.Fatalf("AddOutput (0): %v", err)
	}
	if err := r.AddOutput(ctx, id, Output{Ordinal: 1, ObjectKey: "results/ru1/1-out.webp", Filename: "out2.webp", Format: "webp", SizeBytes: 20, ContentType: "image/webp"}); err != nil {
		t.Fatalf("AddOutput (1): %v", err)
	}
	// D-13: MarkDone requires EXACTLY ONE primary output, and AddOutput never
	// writes role (D-16, it is DB-defaulted), so both rows above defaulted to
	// 'primary'. Demote ordinal 1 to 'report' via direct SQL -- AddOutput has
	// no write path for role -- so this test keeps proving what it is actually
	// about (recordUsage sums size_bytes over EVERY ordinal regardless of
	// role, SEED-017) without violating D-13's precondition.
	if _, err := r.pool.Exec(ctx, `UPDATE job_outputs SET role = 'report' WHERE job_id = $1 AND ordinal = 1`, id); err != nil {
		t.Fatalf("demote ordinal 1 to role=report: %v", err)
	}

	if err := r.MarkDone(ctx, id, 0); err != nil {
		t.Fatalf("MarkDone: %v", err)
	}

	var jobCount int64
	var bytesIn, bytesOut int64
	var status string
	if err := r.pool.QueryRow(ctx, `
		SELECT job_count, bytes_in, bytes_out, status FROM client_usage_daily
		WHERE client_id = $1 AND engine = 'image' AND status = 'done'`, clientID,
	).Scan(&jobCount, &bytesIn, &bytesOut, &status); err != nil {
		t.Fatalf("query client_usage_daily: %v", err)
	}
	if jobCount != 1 {
		t.Fatalf("job_count = %d, want 1", jobCount)
	}
	if bytesIn != 300 {
		t.Fatalf("bytes_in = %d, want 300 (100+200, summed over both ordinals)", bytesIn)
	}
	if bytesOut != 30 {
		t.Fatalf("bytes_out = %d, want 30 (10+20, summed over both ordinals)", bytesOut)
	}
	if status != StatusDone {
		t.Fatalf("status = %q, want %q", status, StatusDone)
	}
}

// TestRecordUsageIsNotInflatedByRetriedOutput reproduces the real retry
// sequence that made the rollup over-count, and pins the fix.
//
// AddOutput commits on its own connection, outside the terminal transition. If
// MarkDone then fails transiently, asynq retries the SAME task; MarkActive
// permits active -> active precisely so that re-entry is legal, the conversion
// runs again, and AddOutput is called a second time for the same logical
// output. With AddOutput as a plain INSERT and no uniqueness on
// (job_id, ordinal), that left two rows, and recordUsage -- which deliberately
// sums across every ordinal so multi-output jobs are not under-counted --
// summed both. The rollup is written inside the terminal transaction and never
// revisited, so the overstatement was permanent.
//
// The assertion is on bytes_out, not on the row count, because the row count
// is an implementation detail of the fix while the corrupted number is what
// actually reaches an operator.
func TestRecordUsageIsNotInflatedByRetriedOutput(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	id, err := r.Create(ctx, CreateParams{
		ClientID:  clientID,
		Operation: "convert", Engine: "image", SourceFormat: "png", TargetFormat: "webp",
		Input: Input{Ordinal: 0, ObjectKey: "uploads/rud/0-in.png", Filename: "in.png", Format: "png", SizeBytes: 100},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Attempt 1: activate, write the output, then have the terminal
	// transition never happen (the transient MarkDone failure).
	if err := r.MarkActive(ctx, id); err != nil {
		t.Fatalf("MarkActive (attempt 1): %v", err)
	}
	out := Output{Ordinal: 0, ObjectKey: "results/rud/0-out.webp", Filename: "out.webp", Format: "webp", SizeBytes: 40, ContentType: "image/webp"}
	if err := r.AddOutput(ctx, id, out); err != nil {
		t.Fatalf("AddOutput (attempt 1): %v", err)
	}

	// Attempt 2: asynq redelivers the same task. active -> active is allowed
	// by design, the conversion reruns, and the same logical output is
	// written again.
	if err := r.MarkActive(ctx, id); err != nil {
		t.Fatalf("MarkActive (attempt 2) must be legal — active -> active is how same-task retry re-enters: %v", err)
	}
	if err := r.AddOutput(ctx, id, out); err != nil {
		t.Fatalf("AddOutput (attempt 2): %v", err)
	}

	if err := r.MarkDone(ctx, id, 0); err != nil {
		t.Fatalf("MarkDone: %v", err)
	}

	var bytesOut int64
	if err := r.pool.QueryRow(ctx, `
		SELECT bytes_out FROM client_usage_daily
		WHERE client_id = $1 AND engine = 'image' AND status = 'done'`, clientID,
	).Scan(&bytesOut); err != nil {
		t.Fatalf("query client_usage_daily: %v", err)
	}
	if bytesOut != 40 {
		t.Fatalf("bytes_out = %d, want exactly 40: a retried write of the SAME logical output must not be summed twice", bytesOut)
	}
}

// TestRecordUsageFailedPathAndStatusSeparation pins D-04: done and failed
// jobs for the same client/engine/day land in two DISTINCT rows, each with
// job_count = 1, differing only by status.
func TestRecordUsageFailedPathAndStatusSeparation(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	doneID, err := r.Create(ctx, CreateParams{
		ClientID: clientID, Operation: "convert", Engine: "image", SourceFormat: "png", TargetFormat: "webp",
		Input: Input{ObjectKey: "uploads/ru2d/0-in.png", Filename: "in.png", Format: "png", SizeBytes: 50},
	})
	if err != nil {
		t.Fatalf("Create done job: %v", err)
	}
	if err := r.MarkActive(ctx, doneID); err != nil {
		t.Fatalf("MarkActive done job: %v", err)
	}
	addPrimaryOutput(t, r, doneID)
	if err := r.MarkDone(ctx, doneID, 0); err != nil {
		t.Fatalf("MarkDone: %v", err)
	}

	failedID, err := r.Create(ctx, CreateParams{
		ClientID: clientID, Operation: "convert", Engine: "image", SourceFormat: "png", TargetFormat: "webp",
		Input: Input{ObjectKey: "uploads/ru2f/0-in.png", Filename: "in.png", Format: "png", SizeBytes: 60},
	})
	if err != nil {
		t.Fatalf("Create failed job: %v", err)
	}
	if err := r.MarkActive(ctx, failedID); err != nil {
		t.Fatalf("MarkActive failed job: %v", err)
	}
	if err := r.MarkFailed(ctx, failedID, "engine_error", "boom", nil, 0); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT status, job_count FROM client_usage_daily
		WHERE client_id = $1 AND engine = 'image'`, clientID,
	)
	if err != nil {
		t.Fatalf("query client_usage_daily: %v", err)
	}
	defer rows.Close()

	byStatus := map[string]int64{}
	for rows.Next() {
		var status string
		var jobCount int64
		if err := rows.Scan(&status, &jobCount); err != nil {
			t.Fatalf("scan: %v", err)
		}
		byStatus[status] = jobCount
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}

	if len(byStatus) != 2 {
		t.Fatalf("expected 2 distinct status rows, got %+v", byStatus)
	}
	if byStatus[StatusDone] != 1 {
		t.Fatalf("done job_count = %d, want 1", byStatus[StatusDone])
	}
	if byStatus[StatusFailed] != 1 {
		t.Fatalf("failed job_count = %d, want 1", byStatus[StatusFailed])
	}
}

// TestRecordUsageSkipsOrphanedClient pins T-51-07: jobs.client_id is
// ON DELETE SET NULL, so a client can be deleted while its job is still
// in flight. That job must still reach done normally, and MUST NOT write a
// client_usage_daily row (there is no client to attribute it to).
func TestRecordUsageSkipsOrphanedClient(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	id, err := r.Create(ctx, CreateParams{
		ClientID: clientID, Operation: "convert", Engine: "image", SourceFormat: "png", TargetFormat: "webp",
		Input: Input{ObjectKey: "uploads/ru3/0-in.png", Filename: "in.png", Format: "png", SizeBytes: 10},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Simulate the client row being deleted (ON DELETE SET NULL fires) by
	// setting client_id to NULL directly, rather than deleting the clients
	// row -- the fixture's other tests may share the clients table state,
	// and this is functionally identical from jobs' point of view.
	if _, err := r.pool.Exec(ctx, `UPDATE jobs SET client_id = NULL WHERE id = $1`, id); err != nil {
		t.Fatalf("orphan job's client_id: %v", err)
	}

	if err := r.MarkActive(ctx, id); err != nil {
		t.Fatalf("MarkActive on orphaned-client job: %v", err)
	}

	before, err := r.pool.Query(ctx, `SELECT count(*) FROM client_usage_daily`)
	if err != nil {
		t.Fatalf("count before: %v", err)
	}
	var countBefore int64
	before.Next()
	if err := before.Scan(&countBefore); err != nil {
		t.Fatalf("scan count before: %v", err)
	}
	before.Close()

	addPrimaryOutput(t, r, id)
	if err := r.MarkDone(ctx, id, 0); err != nil {
		t.Fatalf("MarkDone on orphaned-client job must succeed (a deleted client must never wedge a job in active), got: %v", err)
	}

	got, err := r.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != StatusDone {
		t.Fatalf("status = %q, want done", got.Status)
	}

	after, err := r.pool.Query(ctx, `SELECT count(*) FROM client_usage_daily`)
	if err != nil {
		t.Fatalf("count after: %v", err)
	}
	var countAfter int64
	after.Next()
	if err := after.Scan(&countAfter); err != nil {
		t.Fatalf("scan count after: %v", err)
	}
	after.Close()

	if countAfter != countBefore {
		t.Fatalf("client_usage_daily row count changed from %d to %d — an orphaned-client job must write no usage row", countBefore, countAfter)
	}
}

// TestRecordUsageWallSeconds pins the three wall_seconds derivation branches
// from <behavior>: an explicit attempt duration, a derived duration from a
// backdated started_at when attempt=0, and an exact 0 when the job never
// started (attempt=0, started_at NULL — the reconciler exhaustion path).
func TestRecordUsageWallSeconds(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	t.Run("explicit attempt", func(t *testing.T) {
		id, err := r.Create(ctx, CreateParams{
			ClientID: clientID, Operation: "convert", Engine: "image", SourceFormat: "png", TargetFormat: "webp",
			Input: Input{ObjectKey: "uploads/ru4a/0-in.png", Filename: "in.png", Format: "png"},
		})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := r.MarkActive(ctx, id); err != nil {
			t.Fatalf("MarkActive: %v", err)
		}
		addPrimaryOutput(t, r, id)
		if err := r.MarkDone(ctx, id, 1500*time.Millisecond); err != nil {
			t.Fatalf("MarkDone: %v", err)
		}

		var wallSeconds float64
		if err := r.pool.QueryRow(ctx, `
			SELECT wall_seconds FROM client_usage_daily
			WHERE client_id = $1 AND engine = 'image' AND status = 'done'`, clientID,
		).Scan(&wallSeconds); err != nil {
			t.Fatalf("query wall_seconds: %v", err)
		}
		if diff := wallSeconds - 1.5; diff < -0.05 || diff > 0.05 {
			t.Fatalf("wall_seconds = %v, want ~1.5", wallSeconds)
		}
	})

	t.Run("derived from backdated started_at", func(t *testing.T) {
		id, err := r.Create(ctx, CreateParams{
			ClientID: clientID, Operation: "convert", Engine: "document", SourceFormat: "docx", TargetFormat: "pdf",
			Input: Input{ObjectKey: "uploads/ru4b/0-in.docx", Filename: "in.docx", Format: "docx"},
		})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := r.MarkActive(ctx, id); err != nil {
			t.Fatalf("MarkActive: %v", err)
		}
		if _, err := r.pool.Exec(ctx, `UPDATE jobs SET started_at = now() - interval '3 seconds' WHERE id = $1`, id); err != nil {
			t.Fatalf("backdate started_at: %v", err)
		}
		if err := r.MarkFailed(ctx, id, "engine_error", "boom", nil, 0); err != nil {
			t.Fatalf("MarkFailed: %v", err)
		}

		var wallSeconds float64
		if err := r.pool.QueryRow(ctx, `
			SELECT wall_seconds FROM client_usage_daily
			WHERE client_id = $1 AND engine = 'document' AND status = 'failed'`, clientID,
		).Scan(&wallSeconds); err != nil {
			t.Fatalf("query wall_seconds: %v", err)
		}
		if diff := wallSeconds - 3.0; diff < -0.5 || diff > 0.5 {
			t.Fatalf("wall_seconds = %v, want ~3.0", wallSeconds)
		}
	})

	t.Run("never started records zero", func(t *testing.T) {
		id, err := r.Create(ctx, CreateParams{
			ClientID: clientID, Operation: "convert", Engine: "ocr", SourceFormat: "png", TargetFormat: "txt",
			Input: Input{ObjectKey: "uploads/ru4c/0-in.png", Filename: "in.png", Format: "png"},
		})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		// Reconciler exhaustion path: MarkFailed called directly from
		// 'queued' with attempt=0 and no MarkActive ever having run, so
		// jobs.started_at stays NULL.
		if err := r.MarkFailed(ctx, id, "reconciler_exhausted", "recovery cap exceeded", nil, 0); err != nil {
			t.Fatalf("MarkFailed (never started): %v", err)
		}

		var wallSeconds float64
		if err := r.pool.QueryRow(ctx, `
			SELECT wall_seconds FROM client_usage_daily
			WHERE client_id = $1 AND engine = 'ocr' AND status = 'failed'`, clientID,
		).Scan(&wallSeconds); err != nil {
			t.Fatalf("query wall_seconds: %v", err)
		}
		if wallSeconds != 0 {
			t.Fatalf("wall_seconds = %v, want exactly 0", wallSeconds)
		}
	})
}

// TestRecordUsageAccumulatesSameBucket pins the ON CONFLICT arm: two jobs
// landing in the same (client, engine, day, status) bucket must accumulate
// into ONE row with job_count = 2 and summed byte totals, not two rows.
func TestRecordUsageAccumulatesSameBucket(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	for i, sizeIn := range []int64{100, 300} {
		id, err := r.Create(ctx, CreateParams{
			ClientID: clientID, Operation: "convert", Engine: "image", SourceFormat: "png", TargetFormat: "webp",
			Input: Input{ObjectKey: "uploads/ru5/0-in.png", Filename: "in.png", Format: "png", SizeBytes: sizeIn},
		})
		if err != nil {
			t.Fatalf("Create job %d: %v", i, err)
		}
		if err := r.MarkActive(ctx, id); err != nil {
			t.Fatalf("MarkActive job %d: %v", i, err)
		}
		if err := r.AddOutput(ctx, id, Output{Ordinal: 0, ObjectKey: "results/ru5/0-out.webp", Filename: "out.webp", Format: "webp", SizeBytes: 5, ContentType: "image/webp"}); err != nil {
			t.Fatalf("AddOutput job %d: %v", i, err)
		}
		if err := r.MarkDone(ctx, id, 0); err != nil {
			t.Fatalf("MarkDone job %d: %v", i, err)
		}
	}

	rows, err := r.pool.Query(ctx, `
		SELECT job_count, bytes_in, bytes_out FROM client_usage_daily
		WHERE client_id = $1 AND engine = 'image' AND status = 'done'`, clientID,
	)
	if err != nil {
		t.Fatalf("query client_usage_daily: %v", err)
	}
	defer rows.Close()

	var n int
	var jobCount, bytesIn, bytesOut int64
	for rows.Next() {
		n++
		if err := rows.Scan(&jobCount, &bytesIn, &bytesOut); err != nil {
			t.Fatalf("scan: %v", err)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected exactly one accumulated row, got %d", n)
	}
	if jobCount != 2 {
		t.Fatalf("job_count = %d, want 2", jobCount)
	}
	if bytesIn != 400 {
		t.Fatalf("bytes_in = %d, want 400 (100+300)", bytesIn)
	}
	if bytesOut != 10 {
		t.Fatalf("bytes_out = %d, want 10 (5+5)", bytesOut)
	}
}

// TestRecordUsageExactlyOnceUnderConcurrentMarkDone is the load-bearing
// concurrency proof this plan exists for: exactly-once accounting is a
// PROPERTY OF THE GUARDED TRANSITION (transition's SELECT ... FOR UPDATE +
// source-status allow-list), not of the rollup UPSERT, which is deliberately
// non-idempotent. Two goroutines race to MarkDone the same active job; the
// allow-list must let exactly one commit, and client_usage_daily must show
// exactly one job_count / one set of byte totals — not merely "an error
// occurred somewhere". A future refactor that moves recordUsage out of the
// apply closure (back onto a separate, non-transactional write) breaks this
// test, which is the point: see the inversion check recorded in the SUMMARY.
func TestRecordUsageExactlyOnceUnderConcurrentMarkDone(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	id, err := r.Create(ctx, CreateParams{
		ClientID: clientID, Operation: "convert", Engine: "image", SourceFormat: "png", TargetFormat: "webp",
		Input: Input{ObjectKey: "uploads/ru6/0-in.png", Filename: "in.png", Format: "png", SizeBytes: 111},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := r.MarkActive(ctx, id); err != nil {
		t.Fatalf("MarkActive: %v", err)
	}
	if err := r.AddOutput(ctx, id, Output{Ordinal: 0, ObjectKey: "results/ru6/0-out.webp", Filename: "out.webp", Format: "webp", SizeBytes: 22, ContentType: "image/webp"}); err != nil {
		t.Fatalf("AddOutput: %v", err)
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = r.MarkDone(ctx, id, 0)
		}(i)
	}
	close(start)
	wg.Wait()

	var okCount, errCount int
	for _, err := range errs {
		if err == nil {
			okCount++
			continue
		}
		errCount++
		if !strings.Contains(err.Error(), "illegal transition") {
			t.Fatalf("losing MarkDone error = %v, want it to contain %q", err, "illegal transition")
		}
	}
	if okCount != 1 || errCount != 1 {
		t.Fatalf("okCount=%d errCount=%d, want exactly one winner and one loser", okCount, errCount)
	}

	var jobCount, bytesIn, bytesOut int64
	if err := r.pool.QueryRow(ctx, `
		SELECT job_count, bytes_in, bytes_out FROM client_usage_daily
		WHERE client_id = $1 AND engine = 'image' AND status = 'done'`, clientID,
	).Scan(&jobCount, &bytesIn, &bytesOut); err != nil {
		t.Fatalf("query client_usage_daily: %v", err)
	}
	if jobCount != 1 {
		t.Fatalf("job_count = %d, want exactly 1 (double-counting under concurrent MarkDone)", jobCount)
	}
	if bytesIn != 111 {
		t.Fatalf("bytes_in = %d, want 111 (single job's total, not doubled)", bytesIn)
	}
	if bytesOut != 22 {
		t.Fatalf("bytes_out = %d, want 22 (single job's total, not doubled)", bytesOut)
	}
}

// TestRecordUsageSecondTerminalTransitionIsRejected is the sequential
// counterpart to the concurrency proof above: a second MarkDone on an
// already-done job, and a MarkDone followed by MarkFailed, must both be
// rejected by the allow-list before recordUsage ever runs — neither may add
// a client_usage_daily row.
func TestRecordUsageSecondTerminalTransitionIsRejected(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	t.Run("MarkDone then MarkDone", func(t *testing.T) {
		id, err := r.Create(ctx, CreateParams{
			ClientID: clientID, Operation: "convert", Engine: "image", SourceFormat: "png", TargetFormat: "webp",
			Input: Input{ObjectKey: "uploads/ru7a/0-in.png", Filename: "in.png", Format: "png"},
		})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := r.MarkActive(ctx, id); err != nil {
			t.Fatalf("MarkActive: %v", err)
		}
		addPrimaryOutput(t, r, id)
		if err := r.MarkDone(ctx, id, 0); err != nil {
			t.Fatalf("first MarkDone: %v", err)
		}
		// Second MarkDone must still be rejected by transition()'s allow-list
		// (status is already 'done', not in allowedFrom={active}) BEFORE the
		// D-13 primary-output check in MarkDone's apply closure ever runs --
		// no additional output fixture is needed for this call to keep
		// testing what it claims to test.
		if err := r.MarkDone(ctx, id, 0); err == nil {
			t.Fatalf("second MarkDone on an already-done job must error, got nil")
		}

		var jobCount int64
		if err := r.pool.QueryRow(ctx, `
			SELECT job_count FROM client_usage_daily
			WHERE client_id = $1 AND engine = 'image' AND status = 'done'`, clientID,
		).Scan(&jobCount); err != nil {
			t.Fatalf("query client_usage_daily: %v", err)
		}
		if jobCount != 1 {
			t.Fatalf("job_count = %d, want 1 (rejected second MarkDone must not add a row)", jobCount)
		}
	})

	t.Run("MarkDone then MarkFailed", func(t *testing.T) {
		id, err := r.Create(ctx, CreateParams{
			ClientID: clientID, Operation: "convert", Engine: "image", SourceFormat: "png", TargetFormat: "webp",
			Input: Input{ObjectKey: "uploads/ru7b/0-in.png", Filename: "in.png", Format: "png"},
		})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := r.MarkActive(ctx, id); err != nil {
			t.Fatalf("MarkActive: %v", err)
		}
		addPrimaryOutput(t, r, id)
		if err := r.MarkDone(ctx, id, 0); err != nil {
			t.Fatalf("MarkDone: %v", err)
		}
		if err := r.MarkFailed(ctx, id, "engine_error", "boom", nil, 0); err == nil {
			t.Fatalf("MarkFailed on an already-done job must error, got nil")
		}

		var failedCount int64
		if err := r.pool.QueryRow(ctx, `
			SELECT count(*) FROM client_usage_daily
			WHERE client_id = $1 AND engine = 'image' AND status = 'failed'`, clientID,
		).Scan(&failedCount); err != nil {
			t.Fatalf("query client_usage_daily: %v", err)
		}
		if failedCount != 0 {
			t.Fatalf("failed row count = %d, want 0 (rejected MarkFailed after MarkDone must not add a row)", failedCount)
		}
	})
}
