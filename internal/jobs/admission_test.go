package jobs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// setClientLimits writes a client_limits row directly via SQL (a full-row
// upsert, mirroring internal/clients/limits.go's SetLimit semantics) rather
// than importing internal/clients -- this package must not depend on it.
// Any nil pointer round-trips as SQL NULL, matching D-02.
func setClientLimits(t *testing.T, r *Repo, clientID uuid.UUID, engine string, maxConcurrent *int, maxDailyBytes, maxDailyJobs *int64) {
	t.Helper()
	_, err := r.pool.Exec(context.Background(), `
		INSERT INTO client_limits (client_id, engine, max_concurrent, max_daily_bytes, max_daily_jobs)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (client_id, engine) DO UPDATE SET
			max_concurrent  = EXCLUDED.max_concurrent,
			max_daily_bytes = EXCLUDED.max_daily_bytes,
			max_daily_jobs  = EXCLUDED.max_daily_jobs`,
		clientID, engine, maxConcurrent, maxDailyBytes, maxDailyJobs)
	if err != nil {
		t.Fatalf("set client limits: %v", err)
	}
}

// setEnforcementMode writes clients.enforcement_mode directly via SQL.
func setEnforcementMode(t *testing.T, r *Repo, clientID uuid.UUID, mode string) {
	t.Helper()
	_, err := r.pool.Exec(context.Background(), `UPDATE clients SET enforcement_mode = $2 WHERE id = $1`, clientID, mode)
	if err != nil {
		t.Fatalf("set enforcement mode: %v", err)
	}
}

// seedDailyUsage inserts one client_usage_daily row for TODAY's UTC date
// with status 'done', matching how Repo.recordUsage (usage.go) writes the
// rollup -- so daily-limit tests read exactly the shape production writes.
func seedDailyUsage(t *testing.T, r *Repo, clientID uuid.UUID, engine string, bytesIn, jobCount int64) {
	t.Helper()
	_, err := r.pool.Exec(context.Background(), `
		INSERT INTO client_usage_daily (client_id, engine, day, status, job_count, bytes_in, bytes_out, wall_seconds)
		VALUES ($1, $2, (now() AT TIME ZONE 'utc')::date, 'done', $3, $4, 0, 0)`,
		clientID, engine, jobCount, bytesIn)
	if err != nil {
		t.Fatalf("seed daily usage: %v", err)
	}
}

// intPtr is already declared in repo_test.go; this file reuses it. Only
// int64Ptr is new here (no existing *int64 helper in this package's tests).
func int64Ptr(v int64) *int64 { return &v }

// createFixture is a minimal Create call for these tests: every field this
// package's admission checks actually read (client, engine, input size) is
// explicit; everything else is a fixed image png->webp conversion.
func createFixture(t *testing.T, r *Repo, clientID uuid.UUID, engine, objectKey string, sizeBytes int64) (uuid.UUID, error) {
	t.Helper()
	src, tgt := "png", "webp"
	if engine == "document" {
		src, tgt = "docx", "pdf"
	}
	return r.Create(context.Background(), CreateParams{
		ClientID: clientID, Operation: "convert", Engine: engine, SourceFormat: src, TargetFormat: tgt,
		Input: Input{ObjectKey: objectKey, Filename: "in." + src, Format: src, SizeBytes: sizeBytes},
	})
}

// TestAdmitUnlimitedWhenNoLimitsRow pins ROADMAP SC1: enforcement_mode
// 'enforce' with NO client_limits row at all must behave exactly as before
// this phase existed.
func TestAdmitUnlimitedWhenNoLimitsRow(t *testing.T) {
	r := newTestRepo(t)
	clientID := createTestClient(t, r)
	setEnforcementMode(t, r, clientID, enforcementModeEnforce)

	for i := 0; i < 5; i++ {
		if _, err := createFixture(t, r, clientID, "image", fmt.Sprintf("uploads/adm1/%d-in.png", i), 10); err != nil {
			t.Fatalf("create job %d: %v", i, err)
		}
	}
}

// TestAdmitUnlimitedWhenAllColumnsNull pins the other half of D-02: a
// client_limits row that EXISTS but has every column NULL is behaviorally
// identical to no row at all.
func TestAdmitUnlimitedWhenAllColumnsNull(t *testing.T) {
	r := newTestRepo(t)
	clientID := createTestClient(t, r)
	setEnforcementMode(t, r, clientID, enforcementModeEnforce)
	setClientLimits(t, r, clientID, "image", nil, nil, nil)

	for i := 0; i < 5; i++ {
		if _, err := createFixture(t, r, clientID, "image", fmt.Sprintf("uploads/adm2/%d-in.png", i), 10); err != nil {
			t.Fatalf("create job %d: %v", i, err)
		}
	}
}

// TestAdmitConcurrencyCapRejectsBeyondK pins the basic concurrency cap: the
// (K+1)th create is rejected, and the rejected transaction leaves nothing
// behind.
func TestAdmitConcurrencyCapRejectsBeyondK(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)
	setEnforcementMode(t, r, clientID, enforcementModeEnforce)
	setClientLimits(t, r, clientID, "image", intPtr(2), nil, nil)

	for i := 0; i < 2; i++ {
		if _, err := createFixture(t, r, clientID, "image", fmt.Sprintf("uploads/adm3/%d-in.png", i), 10); err != nil {
			t.Fatalf("create job %d: %v", i, err)
		}
	}

	if _, err := createFixture(t, r, clientID, "image", "uploads/adm3/2-in.png", 10); !errors.Is(err, ErrConcurrencyLimit) {
		t.Fatalf("3rd create error = %v, want ErrConcurrencyLimit", err)
	}

	var count int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE client_id = $1`, clientID).Scan(&count); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if count != 2 {
		t.Fatalf("job count = %d, want 2 (the rejected transaction must leave nothing behind)", count)
	}
}

// TestTerminalStatusFreesSlotWithNoReleaseCode is the ROADMAP SC3 / D-08
// proof: MarkDone, MarkFailed and Cancel each free a slot purely as a
// consequence of the status change they already make -- no dedicated
// release code exists anywhere in this package.
func TestTerminalStatusFreesSlotWithNoReleaseCode(t *testing.T) {
	tests := []struct {
		name   string
		finish func(t *testing.T, r *Repo, id uuid.UUID)
	}{
		{"MarkDone", func(t *testing.T, r *Repo, id uuid.UUID) {
			if err := r.MarkActive(context.Background(), id); err != nil {
				t.Fatalf("MarkActive: %v", err)
			}
			if err := r.MarkDone(context.Background(), id, 0); err != nil {
				t.Fatalf("MarkDone: %v", err)
			}
		}},
		{"MarkFailed", func(t *testing.T, r *Repo, id uuid.UUID) {
			if err := r.MarkFailed(context.Background(), id, "engine_error", "boom", nil, 0); err != nil {
				t.Fatalf("MarkFailed: %v", err)
			}
		}},
		{"Cancel", func(t *testing.T, r *Repo, id uuid.UUID) {
			if err := r.Cancel(context.Background(), id); err != nil {
				t.Fatalf("Cancel: %v", err)
			}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestRepo(t)
			clientID := createTestClient(t, r)
			setEnforcementMode(t, r, clientID, enforcementModeEnforce)
			setClientLimits(t, r, clientID, "image", intPtr(1), nil, nil)

			id, err := createFixture(t, r, clientID, "image", "uploads/adm4/0-in.png", 10)
			if err != nil {
				t.Fatalf("first create: %v", err)
			}

			if _, err := createFixture(t, r, clientID, "image", "uploads/adm4/1-in.png", 10); !errors.Is(err, ErrConcurrencyLimit) {
				t.Fatalf("second create (at cap) error = %v, want ErrConcurrencyLimit", err)
			}

			tt.finish(t, r, id)

			if _, err := createFixture(t, r, clientID, "image", "uploads/adm4/2-in.png", 10); err != nil {
				t.Fatalf("create after %s must succeed (slot freed with zero release code), got: %v", tt.name, err)
			}
		})
	}
}

// TestConcurrentBurstAdmitsExactlyK is the load-bearing race proof for
// T-52-12 / ROADMAP SC2a: 12 simultaneous Create calls against a cap of 3
// must admit EXACTLY 3, never more (a bare SELECT-then-INSERT would let
// more through under concurrency).
func TestConcurrentBurstAdmitsExactlyK(t *testing.T) {
	r := newTestRepo(t)
	clientID := createTestClient(t, r)
	setEnforcementMode(t, r, clientID, enforcementModeEnforce)
	setClientLimits(t, r, clientID, "image", intPtr(3), nil, nil)

	const n = 12
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = createFixture(t, r, clientID, "image", fmt.Sprintf("uploads/adm5/%d-in.png", i), 10)
		}(i)
	}
	close(start)
	wg.Wait()

	var okCount, rejectCount int
	for _, err := range errs {
		switch {
		case err == nil:
			okCount++
		case errors.Is(err, ErrConcurrencyLimit):
			rejectCount++
		default:
			t.Fatalf("unexpected error from concurrent create: %v", err)
		}
	}
	if okCount != 3 {
		t.Fatalf("okCount = %d, want exactly 3", okCount)
	}
	if rejectCount != 9 {
		t.Fatalf("rejectCount = %d, want exactly 9", rejectCount)
	}

	var count int
	if err := r.pool.QueryRow(context.Background(), `SELECT count(*) FROM jobs WHERE client_id = $1`, clientID).Scan(&count); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if count != 3 {
		t.Fatalf("job row count = %d, want exactly 3", count)
	}
}

// TestZeroConcurrencyDeniesEngine pins that an explicit max_concurrent = 0
// denies that engine entirely, while leaving a DIFFERENT engine for the
// same client (with no limit row of its own) unaffected -- the mechanism by
// which an "allowed engine set" is expressed without a second column.
func TestZeroConcurrencyDeniesEngine(t *testing.T) {
	r := newTestRepo(t)
	clientID := createTestClient(t, r)
	setEnforcementMode(t, r, clientID, enforcementModeEnforce)
	setClientLimits(t, r, clientID, "image", intPtr(0), nil, nil)

	if _, err := createFixture(t, r, clientID, "image", "uploads/adm6/0-in.png", 10); !errors.Is(err, ErrConcurrencyLimit) {
		t.Fatalf("image create error = %v, want ErrConcurrencyLimit", err)
	}

	if _, err := createFixture(t, r, clientID, "document", "uploads/adm6/0-in.docx", 10); err != nil {
		t.Fatalf("document create (different engine, no limit set) must succeed, got: %v", err)
	}
}

// TestDailyBytesLimit pins the "would exceed, not already exceeded"
// semantics at the exact boundary: existing+incoming == limit admits,
// existing+incoming > limit rejects with ErrDailyBytesLimit.
func TestDailyBytesLimit(t *testing.T) {
	r := newTestRepo(t)

	t.Run("at exact boundary admits", func(t *testing.T) {
		clientID := createTestClient(t, r)
		setEnforcementMode(t, r, clientID, enforcementModeEnforce)
		setClientLimits(t, r, clientID, "image", nil, int64Ptr(1000), nil)
		seedDailyUsage(t, r, clientID, "image", 990, 5)

		if _, err := createFixture(t, r, clientID, "image", "uploads/adm7a/0-in.png", 10); err != nil {
			t.Fatalf("create at exact byte boundary (990+10=1000) must succeed, got: %v", err)
		}
	})

	t.Run("over boundary rejects", func(t *testing.T) {
		clientID := createTestClient(t, r)
		setEnforcementMode(t, r, clientID, enforcementModeEnforce)
		setClientLimits(t, r, clientID, "image", nil, int64Ptr(1000), nil)
		seedDailyUsage(t, r, clientID, "image", 995, 5)

		if _, err := createFixture(t, r, clientID, "image", "uploads/adm7b/0-in.png", 10); !errors.Is(err, ErrDailyBytesLimit) {
			t.Fatalf("create exceeding daily bytes (995+10=1005) error = %v, want ErrDailyBytesLimit", err)
		}
	})
}

// TestDailyJobsLimit is TestDailyBytesLimit's counterpart for job count.
func TestDailyJobsLimit(t *testing.T) {
	r := newTestRepo(t)

	t.Run("at exact boundary admits", func(t *testing.T) {
		clientID := createTestClient(t, r)
		setEnforcementMode(t, r, clientID, enforcementModeEnforce)
		setClientLimits(t, r, clientID, "image", nil, nil, int64Ptr(5))
		seedDailyUsage(t, r, clientID, "image", 0, 4)

		if _, err := createFixture(t, r, clientID, "image", "uploads/adm8a/0-in.png", 10); err != nil {
			t.Fatalf("create at exact job-count boundary (4+1=5) must succeed, got: %v", err)
		}
	})

	t.Run("over boundary rejects", func(t *testing.T) {
		clientID := createTestClient(t, r)
		setEnforcementMode(t, r, clientID, enforcementModeEnforce)
		setClientLimits(t, r, clientID, "image", nil, nil, int64Ptr(5))
		seedDailyUsage(t, r, clientID, "image", 0, 5)

		if _, err := createFixture(t, r, clientID, "image", "uploads/adm8b/0-in.png", 10); !errors.Is(err, ErrDailyJobsLimit) {
			t.Fatalf("create exceeding daily job count (5+1=6) error = %v, want ErrDailyJobsLimit", err)
		}
	})
}

// TestDailyLimitIgnoresOtherDaysAndOtherEngines pins that the daily read is
// scoped to TODAY's UTC date and to the requested engine only -- neither
// yesterday's rollup, a different engine's rollup, nor a different engine's
// IN-FLIGHT work for the same client contributes to today's verdict. The
// in-flight-job leg (added in plan 52-06) pins that the new in-flight term
// is engine-scoped exactly like the pre-existing rollup term.
func TestDailyLimitIgnoresOtherDaysAndOtherEngines(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)
	setEnforcementMode(t, r, clientID, enforcementModeEnforce)
	setClientLimits(t, r, clientID, "image", nil, int64Ptr(100), nil)

	if _, err := r.pool.Exec(ctx, `
		INSERT INTO client_usage_daily (client_id, engine, day, status, job_count, bytes_in, bytes_out, wall_seconds)
		VALUES ($1, 'image', (now() AT TIME ZONE 'utc')::date - interval '1 day', 'done', 5, 5000, 0, 0)`,
		clientID); err != nil {
		t.Fatalf("seed yesterday usage: %v", err)
	}
	seedDailyUsage(t, r, clientID, "document", 5000, 5)

	// An in-flight (queued, never terminated) document job sized well past
	// the image engine's max_daily_bytes=100 -- this must NOT count toward
	// the image engine's in-flight term, or the create below would be
	// rejected.
	if _, err := createFixture(t, r, clientID, "document", "uploads/adm9/0-in.docx", 5000); err != nil {
		t.Fatalf("create in-flight document job: %v", err)
	}

	if _, err := createFixture(t, r, clientID, "image", "uploads/adm9/0-in.png", 10); err != nil {
		t.Fatalf("create must succeed -- neither yesterday's nor a different engine's rollup or in-flight usage should count toward today's image limit, got: %v", err)
	}
}

// quotaObserverCall records one SetQuotaObserver invocation for assertion.
type quotaObserverCall struct {
	clientID uuid.UUID
	engine   string
	reason   string
}

// TestObserveModeNeverRejects pins D-13/D-14: observe mode always admits,
// even against an explicit max_concurrent = 0, and reports exactly one
// observer call with the correct reason.
func TestObserveModeNeverRejects(t *testing.T) {
	r := newTestRepo(t)
	clientID := createTestClient(t, r)
	setEnforcementMode(t, r, clientID, enforcementModeObserve)
	setClientLimits(t, r, clientID, "image", intPtr(0), nil, nil)

	var calls []quotaObserverCall
	r.SetQuotaObserver(func(cid uuid.UUID, engine, reason string) {
		calls = append(calls, quotaObserverCall{clientID: cid, engine: engine, reason: reason})
	})
	t.Cleanup(func() { r.SetQuotaObserver(nil) })

	if _, err := createFixture(t, r, clientID, "image", "uploads/adm10/0-in.png", 10); err != nil {
		t.Fatalf("observe mode must admit despite max_concurrent=0, got: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("observer call count = %d, want exactly 1", len(calls))
	}
	if calls[0].reason != QuotaReasonConcurrency {
		t.Fatalf("observer reason = %q, want %q", calls[0].reason, QuotaReasonConcurrency)
	}
	if calls[0].clientID != clientID || calls[0].engine != "image" {
		t.Fatalf("observer call = %+v, want clientID=%s engine=image", calls[0], clientID)
	}
}

// TestGlobalKillSwitchNeverRejects pins the other half of D-13: the global
// kill-switch overrides even an 'enforce' per-client mode, and still
// reports.
func TestGlobalKillSwitchNeverRejects(t *testing.T) {
	r := newTestRepo(t)
	clientID := createTestClient(t, r)
	setEnforcementMode(t, r, clientID, enforcementModeEnforce)
	setClientLimits(t, r, clientID, "image", intPtr(0), nil, nil)
	r.SetQuotaEnforcementDisabled(true)
	t.Cleanup(func() { r.SetQuotaEnforcementDisabled(false) })

	var calls int
	r.SetQuotaObserver(func(uuid.UUID, string, string) { calls++ })
	t.Cleanup(func() { r.SetQuotaObserver(nil) })

	if _, err := createFixture(t, r, clientID, "image", "uploads/adm11/0-in.png", 10); err != nil {
		t.Fatalf("global kill-switch must admit despite enforce mode + max_concurrent=0, got: %v", err)
	}

	if calls != 1 {
		t.Fatalf("observer call count = %d, want exactly 1", calls)
	}
}

// TestPreAdmitMatchesAuthoritativeVerdict pins PreAdmit's three documented
// behaviors: it agrees with admitLocked's verdict on a real violation, it is
// a true no-op with no limits row, and it never double-fires the observer
// in observe mode (admitLocked alone fires it).
func TestPreAdmitMatchesAuthoritativeVerdict(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	t.Run("concurrency limit reached", func(t *testing.T) {
		clientID := createTestClient(t, r)
		setEnforcementMode(t, r, clientID, enforcementModeEnforce)
		setClientLimits(t, r, clientID, "image", intPtr(1), nil, nil)

		if _, err := createFixture(t, r, clientID, "image", "uploads/adm12a/0-in.png", 10); err != nil {
			t.Fatalf("first create: %v", err)
		}

		if err := r.PreAdmit(ctx, clientID, "image", 10); !errors.Is(err, ErrConcurrencyLimit) {
			t.Fatalf("PreAdmit with one in-flight job at cap=1 = %v, want ErrConcurrencyLimit", err)
		}
	})

	t.Run("no limits row", func(t *testing.T) {
		clientID := createTestClient(t, r)
		setEnforcementMode(t, r, clientID, enforcementModeEnforce)

		if err := r.PreAdmit(ctx, clientID, "image", 10); err != nil {
			t.Fatalf("PreAdmit with no limits row = %v, want nil", err)
		}
	})

	t.Run("observe mode fires no observer", func(t *testing.T) {
		clientID := createTestClient(t, r)
		setEnforcementMode(t, r, clientID, enforcementModeObserve)
		setClientLimits(t, r, clientID, "image", intPtr(0), nil, nil)

		var calls int
		r.SetQuotaObserver(func(uuid.UUID, string, string) { calls++ })
		t.Cleanup(func() { r.SetQuotaObserver(nil) })

		if err := r.PreAdmit(ctx, clientID, "image", 10); err != nil {
			t.Fatalf("PreAdmit in observe mode = %v, want nil", err)
		}
		if calls != 0 {
			t.Fatalf("observer call count = %d, want exactly 0 (PreAdmit must not double-count observe mode -- admitLocked fires it)", calls)
		}
	})
}

// TestDailyCapNoLongerOvershootsWhenConcurrencyIsHigher supersedes the D-15
// overshoot pin this test used to be (see the dated amendment to D-15 in
// 52-CONTEXT.md). The old fixture was constructed so the daily check
// could NEVER fire (max_daily_bytes sized to one job, but bytesUsed always
// read 0 because nothing here reaches a terminal transition) -- that
// premise is gone now that in-flight work is charged at admission. With
// max_concurrent = 3 (higher than 1) and max_daily_bytes sized to exactly
// one job's input, exactly ONE create is admitted and the SECOND is
// rejected with ErrDailyBytesLimit, NOT ErrConcurrencyLimit -- proving the
// daily term, not the concurrency cap, is what stops it. A daily cap sized
// for one job now admits one job in every configuration, whether or not a
// concurrency cap is also set, and the K x MAX_INPUT_BYTES overshoot D-15
// used to accept is gone.
func TestDailyCapNoLongerOvershootsWhenConcurrencyIsHigher(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)
	setEnforcementMode(t, r, clientID, enforcementModeEnforce)

	const k = 3
	const inputSize int64 = 1000
	setClientLimits(t, r, clientID, "image", intPtr(k), int64Ptr(inputSize), nil)

	if _, err := createFixture(t, r, clientID, "image", "uploads/adm13/0-in.png", inputSize); err != nil {
		t.Fatalf("1st create (0+inputSize<=inputSize): %v", err)
	}

	_, err := createFixture(t, r, clientID, "image", "uploads/adm13/1-in.png", inputSize)
	if !errors.Is(err, ErrDailyBytesLimit) {
		t.Fatalf("2nd create error = %v, want ErrDailyBytesLimit", err)
	}
	if errors.Is(err, ErrConcurrencyLimit) {
		t.Fatalf("2nd create error = %v, must NOT be ErrConcurrencyLimit -- the daily term, not the concurrency cap (still at 1 of 3), must be what stops it", err)
	}

	var totalBytes int64
	if err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(sum(ji.size_bytes), 0)
		FROM job_inputs ji
		JOIN jobs j ON j.id = ji.job_id
		WHERE j.client_id = $1 AND j.status IN ('queued', 'active')`,
		clientID,
	).Scan(&totalBytes); err != nil {
		t.Fatalf("sum in-flight input bytes: %v", err)
	}
	if totalBytes != inputSize {
		t.Fatalf("in-flight bytes = %d, want exactly %d (one job's worth, not K x inputSize)", totalBytes, inputSize)
	}
}

// TestDailyJobsLimitWithoutConcurrencyCap is the verifier's own reproduction
// (52-VERIFICATION.md truth #5 / 52-REVIEW.md CR-01), inverted into a
// permanent regression gate: max_concurrent = nil is the exact configuration
// `manage-clients set-limit <id> image - - 2` produces, and before this fix
// it admitted 50 of 50 sequential creates because nothing but a finished job
// could ever populate client_usage_daily. All three new tests in this file
// set max_concurrent to nil -- that is the entire point, since every
// existing daily test above either sets a concurrency cap or never exercises
// in-flight work, so none of them could have caught this class of defect.
func TestDailyJobsLimitWithoutConcurrencyCap(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)
	setEnforcementMode(t, r, clientID, enforcementModeEnforce)
	setClientLimits(t, r, clientID, "image", nil, nil, int64Ptr(2))

	var admitted, rejected int
	for i := 0; i < 50; i++ {
		_, err := createFixture(t, r, clientID, "image", fmt.Sprintf("uploads/adm14/%d-in.png", i), 10)
		switch {
		case err == nil:
			admitted++
		case errors.Is(err, ErrDailyJobsLimit):
			rejected++
		default:
			t.Fatalf("create %d: unexpected error %v", i, err)
		}
	}
	if admitted != 2 {
		t.Fatalf("admitted = %d, want exactly 2", admitted)
	}
	if rejected != 48 {
		t.Fatalf("rejected = %d, want exactly 48", rejected)
	}

	var count int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE client_id = $1`, clientID).Scan(&count); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if count != 2 {
		t.Fatalf("job count = %d, want exactly 2 (the rejected transactions must leave nothing behind)", count)
	}
}

// waitForAdvisoryLockWaiters polls conn every 10ms, up to timeout, for the
// number of UNGRANTED advisory-lock waiters on the exact (k1, k2) two-key
// pair, returning as soon as the count reaches min (or the last observed
// count if the deadline elapses first). Scoped to locktype='advisory' AND
// this exact (classid, objid) pair AND objsubid = 2 (the two-key advisory-
// lock form pg_advisory_lock(int,int)/pg_advisory_xact_lock(int,int) both
// use) -- NEVER a global advisory-lock count, because `go test ./...` runs
// packages in parallel against the same database and another package's test
// can hold unrelated advisory locks at the same time. classid/objid are
// oid (unsigned 32-bit) columns; k1/k2 are int32, so callers must pass
// their unsigned reinterpretation (int64(uint32(k))) -- verified live
// against octoconv-db (a two-key pg_advisory_lock held in one psql session,
// a second session blocked on it, pg_locks inspected: classid/objid match
// the values passed, objsubid=2, granted=false for the blocked session).
func waitForAdvisoryLockWaiters(t *testing.T, conn *pgxpool.Conn, k1, k2 int32, min int, timeout time.Duration) int {
	t.Helper()
	key1, key2 := int64(uint32(k1)), int64(uint32(k2))
	deadline := time.Now().Add(timeout)
	var last int
	for {
		if err := conn.QueryRow(context.Background(), `
			SELECT count(*) FROM pg_locks
			WHERE locktype = 'advisory' AND classid::bigint = $1 AND objid::bigint = $2
				AND objsubid = 2 AND NOT granted`,
			key1, key2,
		).Scan(&last); err != nil {
			t.Fatalf("count advisory-lock waiters: %v", err)
		}
		if last >= min || time.Now().After(deadline) {
			return last
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestConcurrentBurstDailyJobsCapAdmitsExactlyOne is the lock-coverage pin
// for the daily terms (Codex PR #5 review, P2b). It exists because neither
// of this package's two other concurrency-shaped tests can catch the daily
// subqueries leaving admitLocked's advisory-locked transaction:
//
//   - TestDailyCapNoLongerOvershootsWhenConcurrencyIsHigher performs only two
//     SEQUENTIAL creates, so it still passes even if the daily in-flight
//     subqueries were moved out of admitLocked's tx onto r.pool -- sequential
//     calls never race regardless of which connection/transaction runs the
//     read.
//   - TestConcurrentBurstAdmitsExactlyK configures only max_concurrent, so
//     the CONCURRENCY term -- not the daily term -- is what gets serialized
//     there; it says nothing about the daily terms' lock coverage.
//
// Setting max_concurrent to nil here is what makes the DAILY term the thing
// being serialized: with no concurrency cap, admitLocked's daily subqueries
// (both the byte and job-count sums, computed by the SAME single SQL
// statement under the SAME advisory lock -- so this one test pins the lock
// coverage of both) are the only mechanism standing between 12 concurrent
// creates and 12 concurrent admissions.
//
// Mechanism (corrected from the prior channel-release version, which was
// PROBABILISTIC, not deterministic -- measured while diagnosing this test:
// with admitLocked's `SELECT pg_advisory_xact_lock($1, $2)` commented out,
// the old close(start)-only version PASSED 1 of 5 runs, because a lock-free
// race sometimes still happens to interleave into "1 admitted" purely by
// scheduler luck; the 4 failing runs saw okCount 3, 7, 8, 11). This version
// instead takes a SESSION-scoped pg_advisory_lock, on a dedicated controller
// connection acquired from r.pool, on the exact same (k1, k2) key pair
// admitLocked's own transaction-scoped lock uses (quotaLockKeys), and
// structurally OBSERVES the 12 goroutines blocked behind it in pg_locks
// BEFORE ever releasing them -- the property under test is witnessed
// directly, not inferred from timing:
//
//  1. waitForAdvisoryLockWaiters asserts >= 2 ungranted waiters on this
//     exact (client, image) key pair within a 5s deadline. This is the leg
//     that fails when the lock is removed from admitLocked: with no lock to
//     block on, admission never reaches this checkpoint as a "waiter" at
//     all, so the count never reaches 2 and this call fatals. (Measured
//     10-of-10 below -- see the acceptance run this plan requires.)
//  2. While the controller still holds the lock, `jobs` for this client must
//     read exactly 0 rows, and still 0 after a 200ms settle. This is the
//     leg that fails when the lock is removed: with nothing serializing the
//     12 goroutines, some of them race straight through to a committed
//     INSERT while the controller connection is doing nothing to stop them.
//  3. The pre-existing okCount==1 / rejectCount==11 / jobs-count==1
//     assertions, kept verbatim after the controller releases the lock, are
//     the leg that fails when evaluate is moved AHEAD of the lock in
//     admitLocked: all 12 goroutines then carry a stale pre-lock verdict
//     past the (now purely decorative) lock, and all 12 are admitted.
func TestConcurrentBurstDailyJobsCapAdmitsExactlyOne(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)
	setEnforcementMode(t, r, clientID, enforcementModeEnforce)
	setClientLimits(t, r, clientID, "image", nil, nil, int64Ptr(1))

	k1, k2 := quotaLockKeys(clientID, "image")

	controller, err := r.pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire controller connection: %v", err)
	}
	t.Cleanup(controller.Release)

	if _, err := controller.Exec(ctx, `SELECT pg_advisory_lock($1, $2)`, k1, k2); err != nil {
		t.Fatalf("controller acquire session-scoped advisory lock: %v", err)
	}

	const n = 12
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = createFixture(t, r, clientID, "image", fmt.Sprintf("uploads/adm20/%d-in.png", i), 10)
		}(i)
	}
	close(start)

	if waiters := waitForAdvisoryLockWaiters(t, controller, k1, k2, 2, 5*time.Second); waiters < 2 {
		t.Fatalf("ungranted advisory-lock waiters on (client=%s, image) = %d after 5s, want >= 2 -- no admission ever reached admitLocked's advisory lock, i.e. the daily evaluation is not lock-covered", clientID, waiters)
	}

	var count int
	if err := controller.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE client_id = $1`, clientID).Scan(&count); err != nil {
		t.Fatalf("count jobs while (client, image) advisory lock is held: %v", err)
	}
	if count != 0 {
		t.Fatalf("job row count while the (client, image) advisory lock is held elsewhere = %d, want 0 -- no admission may COMPLETE while this lock is held", count)
	}
	time.Sleep(200 * time.Millisecond)
	if err := controller.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE client_id = $1`, clientID).Scan(&count); err != nil {
		t.Fatalf("count jobs after 200ms settle: %v", err)
	}
	if count != 0 {
		t.Fatalf("job row count after a 200ms settle, lock still held = %d, want still 0", count)
	}

	if _, err := controller.Exec(ctx, `SELECT pg_advisory_unlock($1, $2)`, k1, k2); err != nil {
		t.Fatalf("controller release session-scoped advisory lock: %v", err)
	}
	wg.Wait()

	var okCount, rejectCount int
	for _, err := range errs {
		switch {
		case err == nil:
			okCount++
		case errors.Is(err, ErrDailyJobsLimit):
			rejectCount++
		default:
			t.Fatalf("unexpected error from concurrent create: %v", err)
		}
	}
	if okCount != 1 {
		t.Fatalf("okCount = %d, want exactly 1", okCount)
	}
	if rejectCount != 11 {
		t.Fatalf("rejectCount = %d, want exactly 11", rejectCount)
	}

	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE client_id = $1`, clientID).Scan(&count); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if count != 1 {
		t.Fatalf("job row count = %d, want exactly 1 (the rejected transactions must leave nothing behind)", count)
	}
}

// TestDailyBytesLimitWithoutConcurrencyCap is TestDailyJobsLimitWithoutConcurrencyCap's
// counterpart for the byte cap: with no concurrency cap set, the in-flight
// byte term must reject a create as soon as the running in-flight total
// would exceed max_daily_bytes, and the rejection must be provably caused by
// the in-flight term (nothing here ever reaches a terminal state, so
// client_usage_daily stays empty for the whole test).
func TestDailyBytesLimitWithoutConcurrencyCap(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)
	setEnforcementMode(t, r, clientID, enforcementModeEnforce)
	setClientLimits(t, r, clientID, "image", nil, int64Ptr(1000), nil)

	if _, err := createFixture(t, r, clientID, "image", "uploads/adm15/0-in.png", 400); err != nil {
		t.Fatalf("1st create (0+400<=1000): %v", err)
	}
	if _, err := createFixture(t, r, clientID, "image", "uploads/adm15/1-in.png", 400); err != nil {
		t.Fatalf("2nd create (400+400<=1000): %v", err)
	}
	if _, err := createFixture(t, r, clientID, "image", "uploads/adm15/2-in.png", 400); !errors.Is(err, ErrDailyBytesLimit) {
		t.Fatalf("3rd create (800+400=1200>1000) error = %v, want ErrDailyBytesLimit", err)
	}

	var count int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE client_id = $1`, clientID).Scan(&count); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if count != 2 {
		t.Fatalf("job count = %d, want exactly 2", count)
	}
}

// TestInFlightAndTerminalUsageCountedExactlyOnce is the no-double-count pin
// across a terminal transition, designed so a double count fails it: if the
// completed job A were charged twice -- once while in-flight and again in
// the client_usage_daily rollup -- the running total at create C below would
// be 400, and C would already have been rejected instead of admitted at the
// exact 300 boundary. This fails loudly on a double count rather than merely
// not detecting one.
func TestInFlightAndTerminalUsageCountedExactlyOnce(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)
	setEnforcementMode(t, r, clientID, enforcementModeEnforce)
	setClientLimits(t, r, clientID, "image", nil, int64Ptr(300), nil)

	idA, err := createFixture(t, r, clientID, "image", "uploads/adm16/a-in.png", 100)
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	if err := r.MarkActive(ctx, idA); err != nil {
		t.Fatalf("MarkActive(A): %v", err)
	}
	if err := r.MarkDone(ctx, idA, 0); err != nil {
		t.Fatalf("MarkDone(A): %v", err)
	}

	// B: rollup 100 (A, terminal) + in-flight 0 + own 100 = 200 <= 300.
	if _, err := createFixture(t, r, clientID, "image", "uploads/adm16/b-in.png", 100); err != nil {
		t.Fatalf("create B (rollup 100 + in-flight 0 + own 100 = 200<=300): %v", err)
	}
	// C: rollup 100 (A) + in-flight 100 (B) + own 100 = 300 <= 300, the exact
	// boundary. If A were double-counted, this would already read 400 and
	// be rejected.
	if _, err := createFixture(t, r, clientID, "image", "uploads/adm16/c-in.png", 100); err != nil {
		t.Fatalf("create C (rollup 100 + in-flight 100 + own 100 = 300<=300): %v", err)
	}
	// D: rollup 100 (A) + in-flight 200 (B, C) + own 100 = 400 > 300.
	if _, err := createFixture(t, r, clientID, "image", "uploads/adm16/d-in.png", 100); !errors.Is(err, ErrDailyBytesLimit) {
		t.Fatalf("create D (400>300) error = %v, want ErrDailyBytesLimit", err)
	}

	var bytesIn, jobCount int64
	if err := r.pool.QueryRow(ctx, `
		SELECT bytes_in, job_count FROM client_usage_daily
		WHERE client_id = $1 AND engine = 'image' AND day = (now() AT TIME ZONE 'utc')::date`,
		clientID,
	).Scan(&bytesIn, &jobCount); err != nil {
		t.Fatalf("read client_usage_daily row: %v", err)
	}
	if bytesIn != 100 || jobCount != 1 {
		t.Fatalf("client_usage_daily row = {bytes_in: %d, job_count: %d}, want {100, 1}", bytesIn, jobCount)
	}
}

// TestCanceledJobStillCountsAgainstDailyQuota is the WR-01 evasion path,
// inverted into a permanent regression gate. Before Cancel charged
// recordUsage, A's cost vanished on cancel and C was admitted -- an
// unbounded `create -> cancel` loop never advanced the cap. max_concurrent
// is nil so this exercises the daily path specifically (a concurrency cap
// would free A's slot for an unrelated reason and mask the evasion).
func TestCanceledJobStillCountsAgainstDailyQuota(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)
	setEnforcementMode(t, r, clientID, enforcementModeEnforce)
	setClientLimits(t, r, clientID, "image", nil, nil, int64Ptr(2))

	idA, err := createFixture(t, r, clientID, "image", "uploads/adm17/a-in.png", 10)
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	if err := r.Cancel(ctx, idA); err != nil {
		t.Fatalf("Cancel(A): %v", err)
	}

	if _, err := createFixture(t, r, clientID, "image", "uploads/adm17/b-in.png", 10); err != nil {
		t.Fatalf("create B (1 canceled + 1 own = 2<=2): %v", err)
	}

	if _, err := createFixture(t, r, clientID, "image", "uploads/adm17/c-in.png", 10); !errors.Is(err, ErrDailyJobsLimit) {
		t.Fatalf("create C (1 canceled + 1 in-flight B + 1 own = 3>2) error = %v, want ErrDailyJobsLimit", err)
	}

	var jobCount int64
	if err := r.pool.QueryRow(ctx, `
		SELECT job_count FROM client_usage_daily
		WHERE client_id = $1 AND engine = 'image' AND day = (now() AT TIME ZONE 'utc')::date AND status = 'canceled'`,
		clientID,
	).Scan(&jobCount); err != nil {
		t.Fatalf("read canceled client_usage_daily row: %v", err)
	}
	if jobCount != 1 {
		t.Fatalf("canceled row job_count = %d, want exactly 1", jobCount)
	}
}

// TestCancelDoesNotDoubleCountAcrossInFlightAndRollup pins that a canceled
// job's bytes are charged exactly once. If the cancel were charged twice --
// once while in-flight and again in the rollup -- create C below would
// already read 400 and be rejected one create early, instead of admitted
// at the exact 300 boundary. This fails loudly on a double count rather
// than merely not detecting one.
func TestCancelDoesNotDoubleCountAcrossInFlightAndRollup(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)
	setEnforcementMode(t, r, clientID, enforcementModeEnforce)
	setClientLimits(t, r, clientID, "image", nil, int64Ptr(300), nil)

	idA, err := createFixture(t, r, clientID, "image", "uploads/adm18/a-in.png", 100)
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	if err := r.Cancel(ctx, idA); err != nil {
		t.Fatalf("Cancel(A): %v", err)
	}

	// B: rollup 100 (A, canceled) + in-flight 0 + own 100 = 200 <= 300.
	if _, err := createFixture(t, r, clientID, "image", "uploads/adm18/b-in.png", 100); err != nil {
		t.Fatalf("create B (rollup 100 + in-flight 0 + own 100 = 200<=300): %v", err)
	}
	// C: rollup 100 (A) + in-flight 100 (B) + own 100 = 300 <= 300, the exact
	// boundary. If A were double-counted, this would already read 400 and
	// be rejected.
	if _, err := createFixture(t, r, clientID, "image", "uploads/adm18/c-in.png", 100); err != nil {
		t.Fatalf("create C (rollup 100 + in-flight 100 + own 100 = 300<=300): %v", err)
	}
	// D: rollup 100 (A) + in-flight 200 (B, C) + own 100 = 400 > 300.
	if _, err := createFixture(t, r, clientID, "image", "uploads/adm18/d-in.png", 100); !errors.Is(err, ErrDailyBytesLimit) {
		t.Fatalf("create D (400>300) error = %v, want ErrDailyBytesLimit", err)
	}
}

// TestAdmissionHoldsUnderRepeatableReadDefault induces the exact hostile
// configuration 52-REVIEW.md WR-02 describes -- a second pool whose
// server-side default_transaction_isolation is repeatable read -- and proves
// the concurrency cap still holds. With the isolation level pinned in
// Repo.Create/Repo.transition's pgx.BeginTxFunc calls, admission is correct
// regardless of the connection's inherited default; before that pin, this
// exact deployment configuration silently over-admitted, with no error and
// no serialization failure, because the post-lock re-check would have been
// served from the pre-lock snapshot instead of a fresh one.
func TestAdmissionHoldsUnderRepeatableReadDefault(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}

	// Set default_transaction_isolation as a startup RuntimeParam rather
	// than via a DSN `options=-c ...` string: the options GUC splits its
	// value on unescaped whitespace, so a two-word value like "repeatable
	// read" requires backslash-escaping the embedded space -- fragile and
	// easy to get silently wrong (confirmed live: an unescaped attempt was
	// truncated to "repeatable" by the server and rejected outright).
	// RuntimeParams is sent as its own discrete startup-packet parameter,
	// so the two-word value round-trips with no escaping at all, and this
	// pool otherwise still connects to the exact same DATABASE_URL target.
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("pgxpool.ParseConfig: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["default_transaction_isolation"] = "repeatable read"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("pgxpool.NewWithConfig (repeatable-read default): %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping repeatable-read pool: %v", err)
	}

	// Assert the harness actually induced the condition; a false failure
	// here would be worse than an honest skip, but this is the ONLY
	// condition that triggers a skip -- any other failure below is real.
	var observedIso string
	if err := pool.QueryRow(ctx, `SHOW default_transaction_isolation`).Scan(&observedIso); err != nil {
		t.Fatalf("SHOW default_transaction_isolation: %v", err)
	}
	if observedIso != "repeatable read" {
		t.Skipf("harness did not induce a repeatable read default (observed %q); skipping rather than risk a false failure", observedIso)
	}

	r := NewRepo(pool)
	clientID := createTestClient(t, r)
	setEnforcementMode(t, r, clientID, enforcementModeEnforce)
	setClientLimits(t, r, clientID, "image", intPtr(3), nil, nil)

	const n = 12
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = createFixture(t, r, clientID, "image", fmt.Sprintf("uploads/adm19/%d-in.png", i), 10)
		}(i)
	}
	close(start)
	wg.Wait()

	var okCount, rejectCount int
	for _, err := range errs {
		switch {
		case err == nil:
			okCount++
		case errors.Is(err, ErrConcurrencyLimit):
			rejectCount++
		default:
			t.Fatalf("unexpected error from concurrent create: %v", err)
		}
	}
	if okCount != 3 {
		t.Fatalf("okCount = %d, want exactly 3", okCount)
	}
	if rejectCount != 9 {
		t.Fatalf("rejectCount = %d, want exactly 9", rejectCount)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE client_id = $1`, clientID).Scan(&count); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if count != 3 {
		t.Fatalf("job row count = %d, want exactly 3", count)
	}
}

// TestQuotaLockKeysDeterministicAndDistinct is a pure Go unit test (no
// DATABASE_URL required): the lock-key derivation is deterministic across
// calls, distinct for different clients and different engines, and pinned
// against hard-coded values for one fixture pair so an accidental
// hash-function swap is caught even though it would still look
// "deterministic" and "distinct".
func TestQuotaLockKeysDeterministicAndDistinct(t *testing.T) {
	clientA := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	clientB := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	a1, a2 := quotaLockKeys(clientA, "image")
	repeat1, repeat2 := quotaLockKeys(clientA, "image")
	if a1 != repeat1 || a2 != repeat2 {
		t.Fatalf("quotaLockKeys not deterministic across calls: (%d,%d) != (%d,%d)", a1, a2, repeat1, repeat2)
	}

	// Hard-coded expected values for clientA+"image" -- catches an
	// accidental hash-function swap (e.g. FNV-1a -> FNV-1, or a change in
	// what bytes are hashed) that would otherwise still pass the
	// determinism/distinctness checks below while silently changing the
	// mapping every replica computes.
	const wantK1, wantK2 = int32(1031130113), int32(-1286523398)
	if a1 != wantK1 || a2 != wantK2 {
		t.Fatalf("quotaLockKeys(%s, image) = (%d, %d), want (%d, %d)", clientA, a1, a2, wantK1, wantK2)
	}

	b1, b2 := quotaLockKeys(clientB, "image")
	if a1 == b1 && a2 == b2 {
		t.Fatalf("quotaLockKeys must differ for different clients: clientA=(%d,%d) clientB=(%d,%d)", a1, a2, b1, b2)
	}

	c1, c2 := quotaLockKeys(clientA, "document")
	if a1 == c1 && a2 == c2 {
		t.Fatalf("quotaLockKeys must differ for different engines: image=(%d,%d) document=(%d,%d)", a1, a2, c1, c2)
	}
}

// statusLiteralPattern extracts each `'<value>'::text` literal out of a
// pg_get_constraintdef/pg_get_indexdef string -- e.g. the
// `ARRAY['queued'::text, 'active'::text]` shape both functions return for a
// `status = ANY (ARRAY[...])` predicate. Never compared as raw SQL strings:
// every comparison in TestEveryJobStatusIsClassifiedForQuotaAccounting goes
// through this extraction first, then an order-independent set comparison.
var statusLiteralPattern = regexp.MustCompile(`'([a-z_]+)'::text`)

// extractStatusLiterals returns the sorted, deduplicated set of status
// literals found in def (a pg_get_constraintdef/pg_get_indexdef string).
func extractStatusLiterals(def string) []string {
	matches := statusLiteralPattern.FindAllStringSubmatch(def, -1)
	seen := make(map[string]struct{}, len(matches))
	for _, m := range matches {
		seen[m[1]] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// sortedSet returns a sorted, deduplicated copy of the union of one or more
// string slices, for order-independent comparison.
func sortedSet(slices ...[]string) []string {
	seen := make(map[string]struct{})
	for _, sl := range slices {
		for _, s := range sl {
			seen[s] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// setDiff returns (in got but not want, in want but not got) -- both
// order-independent -- for a precise "what's missing / what's extra"
// failure message rather than a raw slice dump.
func setDiff(got, want []string) (extra, missing []string) {
	gotSet := make(map[string]struct{}, len(got))
	for _, s := range got {
		gotSet[s] = struct{}{}
	}
	wantSet := make(map[string]struct{}, len(want))
	for _, s := range want {
		wantSet[s] = struct{}{}
	}
	for s := range gotSet {
		if _, ok := wantSet[s]; !ok {
			extra = append(extra, s)
		}
	}
	for s := range wantSet {
		if _, ok := gotSet[s]; !ok {
			missing = append(missing, s)
		}
	}
	sort.Strings(extra)
	sort.Strings(missing)
	return extra, missing
}

// TestEveryJobStatusIsClassifiedForQuotaAccounting is the live-schema
// invariant test for the quota status partition (Codex PR #5 review, P3):
// every value in the jobs.status CHECK domain must land in exactly one of
// quotaInFlightStatuses, quotaLedgerStatuses or quotaUnaccountedStatuses,
// and quotaInFlightStatuses must never drift from
// jobs_client_engine_inflight_idx's own partial predicate. Both reads go
// through the live schema (pg_get_constraintdef / pg_get_indexdef), never a
// hard-coded copy of either definition, so this test fails the moment the
// schema and the Go declaration disagree in either direction.
func TestEveryJobStatusIsClassifiedForQuotaAccounting(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	var checkDef string
	if err := r.pool.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid) FROM pg_constraint
		WHERE conrelid = 'jobs'::regclass AND contype = 'c'
			AND pg_get_constraintdef(oid) LIKE '%(status = ANY%'`,
	).Scan(&checkDef); err != nil {
		t.Fatalf("read jobs status CHECK definition: %v", err)
	}
	schemaStatuses := extractStatusLiterals(checkDef)
	declaredStatuses := sortedSet(quotaInFlightStatuses, quotaLedgerStatuses, quotaUnaccountedStatuses)

	if extra, missing := setDiff(schemaStatuses, declaredStatuses); len(extra) > 0 || len(missing) > 0 {
		t.Fatalf(
			"jobs.status CHECK domain %v and the declared quota partition %v disagree (schema has but partition doesn't: %v; partition has but schema doesn't: %v) -- "+
				"decide whether each newly added status consumes quota: move it into quotaInFlightStatuses (also requires changing "+
				"jobs_client_engine_inflight_idx's predicate in a new migration and the in-flight SQL literals in evaluate), "+
				"into quotaLedgerStatuses, or into quotaUnaccountedStatuses with a recorded reason",
			schemaStatuses, declaredStatuses, extra, missing,
		)
	}

	var indexDef string
	if err := r.pool.QueryRow(ctx, `SELECT pg_get_indexdef('jobs_client_engine_inflight_idx'::regclass)`).Scan(&indexDef); err != nil {
		t.Fatalf("read jobs_client_engine_inflight_idx definition: %v", err)
	}
	indexStatuses := extractStatusLiterals(indexDef)
	inFlightStatuses := sortedSet(quotaInFlightStatuses)

	if extra, missing := setDiff(indexStatuses, inFlightStatuses); len(extra) > 0 || len(missing) > 0 {
		t.Fatalf(
			"jobs_client_engine_inflight_idx's WHERE predicate %v and quotaInFlightStatuses %v disagree (index has but declaration doesn't: %v; declaration has but index doesn't: %v) -- "+
				"the index predicate and the Go declaration must never drift apart in either direction",
			indexStatuses, inFlightStatuses, extra, missing,
		)
	}

	unaccounted := sortedSet(quotaUnaccountedStatuses)
	wantUnaccounted := []string{StatusAwaitingUpload}
	if extra, missing := setDiff(unaccounted, wantUnaccounted); len(extra) > 0 || len(missing) > 0 {
		t.Fatalf(
			"quotaUnaccountedStatuses = %v, want exactly %v (extra: %v, missing: %v) -- "+
				"removing an entry from quotaUnaccountedStatuses is only correct together with moving that status into "+
				"quotaInFlightStatuses or quotaLedgerStatuses; for the in-flight set that also means changing "+
				"jobs_client_engine_inflight_idx's predicate in a new migration",
			unaccounted, wantUnaccounted, extra, missing,
		)
	}
}
