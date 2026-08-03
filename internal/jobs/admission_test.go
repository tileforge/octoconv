package jobs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
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
