package jobs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// GROUP A — source-level SQL-shape assertions. No database needed; these run
// on every `go test` invocation as the always-on regression gate for the two
// critical correctness constraints D-05 depends on.
// ---------------------------------------------------------------------------

// TestUsageTerminalSQLHasNoStatusPredicate pins that usageTerminalSQL never
// filters client_usage_daily by status. A status filter would reintroduce
// the D-05 enforcement/visibility drift: migration 0014 admits 'canceled'
// rows into client_usage_daily, and evaluate() (admission.go:287-292) sums
// the table unfiltered. If this test fails, the operator-facing usage figure
// will disagree with what the admission gate actually enforces.
func TestUsageTerminalSQLHasNoStatusPredicate(t *testing.T) {
	if strings.Contains(usageTerminalSQL, "status") {
		t.Fatalf("usageTerminalSQL contains %q — a status filter on client_usage_daily reintroduces the D-05 enforcement/visibility drift: migration 0014 admits 'canceled' rows and evaluate() (admission.go:287-292) sums the table unfiltered; the operator-facing usage figure must match", "status")
	}
}

// TestUsageInFlightSQLUsesLiteralStatuses pins that both in-flight statements
// write 'queued'/'active' as SQL literals, never bound $n parameters.
// Binding them as parameters defeats jobs_client_engine_inflight_idx's
// partial predicate (admission.go:295-304), silently degrading the query
// plan to a sequential scan of jobs.
func TestUsageInFlightSQLUsesLiteralStatuses(t *testing.T) {
	const literal = "status IN ('queued', 'active')"
	const bound = "status IN ($"

	stmts := map[string]string{
		"usageInFlightJobsSQL":  usageInFlightJobsSQL,
		"usageInFlightBytesSQL": usageInFlightBytesSQL,
	}
	for name, sql := range stmts {
		if !strings.Contains(sql, literal) {
			t.Errorf("%s does not contain the literal predicate %q — binding the in-flight statuses as parameters defeats jobs_client_engine_inflight_idx's partial predicate (admission.go:295-304), silently degrading the plan to a sequential scan of jobs", name, literal)
		}
		if strings.Contains(sql, bound) {
			t.Errorf("%s contains a bound-parameter status predicate (%q) — the in-flight statuses must be SQL literals, matching evaluate()'s own in-flight subqueries (admission.go:295-304)", name, bound)
		}
	}
}

// TestUsageInFlightSQLExcludesNullClients pins that both in-flight
// statements guard against jobs.client_id's ON DELETE SET NULL nullability
// (0001_init.sql:43). Without the IS NOT NULL guard, a deleted client's
// still-queued/active jobs collapse into one NULL group that pgx scans into
// uuid.Nil without error, fabricating a phantom client row in the operator
// report carrying enforcement_mode: "" — a value docs/openapi.yaml declares
// as enum: [observe, enforce] and required (WR-01).
func TestUsageInFlightSQLExcludesNullClients(t *testing.T) {
	stmts := map[string]string{
		"usageInFlightJobsSQL":  usageInFlightJobsSQL,
		"usageInFlightBytesSQL": usageInFlightBytesSQL,
	}
	guards := map[string]string{
		"usageInFlightJobsSQL":  "client_id IS NOT NULL",
		"usageInFlightBytesSQL": "j.client_id IS NOT NULL",
	}
	for name, sql := range stmts {
		if !strings.Contains(sql, guards[name]) {
			t.Errorf("%s does not contain %q — without this guard, a job orphaned by ON DELETE SET NULL (0001_init.sql:43) collapses into a NULL group that pgx scans into uuid.Nil without error, fabricating a phantom client row in the operator report (WR-01)", name, guards[name])
		}
	}
}

// TestUsageInFlightJobCountDoesNotJoinInputs pins that the job-count query
// and the byte-sum query stay two SEPARATE grouped statements. Counting over
// a job_inputs join would multiply a job by its job_inputs row count — a job
// with N inputs must contribute 1 to the job count and N sizes to the byte
// sum, never N to the count.
func TestUsageInFlightJobCountDoesNotJoinInputs(t *testing.T) {
	if strings.Contains(usageInFlightJobsSQL, "job_inputs") {
		t.Fatalf("usageInFlightJobsSQL joins job_inputs — counting over that join would multiply a job by its job_inputs row count instead of counting it once; the job-count and byte-sum terms must stay separate statements, mirroring evaluate()'s own two-separate-subqueries structure (admission.go:289-294)")
	}
	if !strings.Contains(usageInFlightBytesSQL, "job_inputs") {
		t.Fatalf("usageInFlightBytesSQL does not reference job_inputs — the byte-sum term must join job_inputs to sum size_bytes per in-flight job")
	}
}

// ---------------------------------------------------------------------------
// GROUP B — DATABASE_URL-gated integration tests. Each proves, against a
// real Postgres, that UsageSummary's figures mirror evaluate()'s formula
// (D-05).
// ---------------------------------------------------------------------------

// findUsageRow returns the row for (clientID, engine), or nil if absent.
func findUsageRow(rows []UsageRow, clientID uuid.UUID, engine string) *UsageRow {
	for i := range rows {
		if rows[i].ClientID == clientID && rows[i].Engine == engine {
			return &rows[i]
		}
	}
	return nil
}

// utcToday returns today's UTC midnight as read from Postgres via
// r.UTCToday — the same clock recordUsage (usage.go) and evaluate()
// (admission.go) both stamp rows with. Deriving this from the API host's
// wall clock (the prior implementation) let a UTC-midnight crossing between
// a test's seed and its assertion flake the test (review WR-07); reading it
// from the database instead ties the test's notion of "today" to the exact
// clock the rows themselves were written against, so that crossing can no
// longer cause a mismatch.
func utcToday(t *testing.T, r *Repo) time.Time {
	t.Helper()
	today, err := r.UTCToday(context.Background())
	if err != nil {
		t.Fatalf("UTCToday: %v", err)
	}
	return today
}

// TestUTCTodayMatchesPostgresDay pins that Repo.UTCToday returns exactly
// what a direct `SELECT (now() AT TIME ZONE 'utc')::date` returns, and that
// the value is UTC-midnight-aligned. Plan 53-07's handler feeds this value
// straight into resolveUsagePeriod, which assumes a normalized day boundary,
// so both properties matter, not just equality with the direct query.
func TestUTCTodayMatchesPostgresDay(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	got, err := r.UTCToday(ctx)
	if err != nil {
		t.Fatalf("UTCToday: %v", err)
	}

	var want time.Time
	if err := r.pool.QueryRow(ctx, `SELECT (now() AT TIME ZONE 'utc')::date`).Scan(&want); err != nil {
		t.Fatalf("direct SELECT (now() AT TIME ZONE 'utc')::date: %v", err)
	}

	if !got.Equal(want) {
		t.Errorf("UTCToday() = %v, want %v (direct SELECT) — both must read the identical Postgres day expression", got, want)
	}
	if got.Hour() != 0 || got.Minute() != 0 || got.Second() != 0 || got.Nanosecond() != 0 {
		t.Errorf("UTCToday() = %v is not UTC-midnight-aligned — plan 53-07's handler assumes a normalized day boundary", got)
	}
	if got.Location() != time.UTC {
		t.Errorf("UTCToday().Location() = %v, want time.UTC", got.Location())
	}
}

// TestUsageSummaryMirrorsEvaluateDailyBytesBoundary pins the strongest
// available proof that UsageSummary's figure is byte-for-byte evaluate()'s
// figure: it seeds terminal + in-flight usage, computes the sum UsageSummary
// reports, then feeds that exact number back into max_daily_bytes and
// confirms evaluate() (via Repo.Create) flips from admit to reject at
// exactly that boundary (D-05).
func TestUsageSummaryMirrorsEvaluateDailyBytesBoundary(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	seedDailyUsage(t, r, clientID, "image", 500, 3)
	if _, err := createFixture(t, r, clientID, "image", "uploads/usq1/0-in.png", 250); err != nil {
		t.Fatalf("create in-flight fixture: %v", err)
	}

	today := utcToday(t, r)
	rows, err := r.UsageSummary(ctx, &clientID, today, today)
	if err != nil {
		t.Fatalf("UsageSummary: %v", err)
	}
	row := findUsageRow(rows, clientID, "image")
	if row == nil {
		t.Fatalf("UsageSummary did not return a row for (client=%s, engine=image)", clientID)
	}
	want := row.TerminalBytesIn + row.InFlightBytesIn

	setEnforcementMode(t, r, clientID, enforcementModeEnforce)
	setClientLimits(t, r, clientID, "image", nil, int64Ptr(want), nil)
	if _, err := createFixture(t, r, clientID, "image", "uploads/usq1/1-in.png", 0); err != nil {
		t.Fatalf("create at UsageSummary's exact byte boundary (bytesUsed=%d, inputBytes=0) must be admitted by evaluate() — D-05 says UsageSummary's figure IS evaluate()'s figure, got: %v", want, err)
	}

	// Guard against a zero-byte fixture writing a negative limit: migration
	// 0013_client_limits.sql:37 declares
	// CHECK (max_daily_bytes IS NULL OR max_daily_bytes >= 0), so if want were
	// ever 0 the write below would be -1 and die on a Postgres constraint
	// violation instead of asserting anything about ErrDailyBytesLimit
	// (CodeRabbit, PR #6).
	if want <= 0 {
		t.Fatalf("want = %d, must be > 0 — the seeded fixture (terminalBytesIn=%d, inFlightBytesIn=%d) produced a non-positive boundary, which would write an invalid negative max_daily_bytes below", want, row.TerminalBytesIn, row.InFlightBytesIn)
	}
	setClientLimits(t, r, clientID, "image", nil, int64Ptr(want-1), nil)
	if _, err := createFixture(t, r, clientID, "image", "uploads/usq1/2-in.png", 0); !errors.Is(err, ErrDailyBytesLimit) {
		t.Fatalf("create exceeding UsageSummary's byte figure by 1 (limit=%d, bytesUsed=%d) must be rejected with ErrDailyBytesLimit — D-05 says UsageSummary's figure IS evaluate()'s figure, error = %v", want-1, want, err)
	}
}

// TestUsageSummaryMirrorsEvaluateDailyJobsBoundary is
// TestUsageSummaryMirrorsEvaluateDailyBytesBoundary's counterpart for the
// job-count dimension.
func TestUsageSummaryMirrorsEvaluateDailyJobsBoundary(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	seedDailyUsage(t, r, clientID, "image", 0, 3)
	if _, err := createFixture(t, r, clientID, "image", "uploads/usq2/0-in.png", 10); err != nil {
		t.Fatalf("create in-flight fixture: %v", err)
	}

	today := utcToday(t, r)
	rows, err := r.UsageSummary(ctx, &clientID, today, today)
	if err != nil {
		t.Fatalf("UsageSummary: %v", err)
	}
	row := findUsageRow(rows, clientID, "image")
	if row == nil {
		t.Fatalf("UsageSummary did not return a row for (client=%s, engine=image)", clientID)
	}
	want := row.TerminalJobCount + row.InFlightJobCount

	setEnforcementMode(t, r, clientID, enforcementModeEnforce)
	setClientLimits(t, r, clientID, "image", nil, nil, int64Ptr(want))
	if _, err := createFixture(t, r, clientID, "image", "uploads/usq2/1-in.png", 10); !errors.Is(err, ErrDailyJobsLimit) {
		t.Fatalf("create with max_daily_jobs=%d (== UsageSummary's jobsUsed) must be rejected with ErrDailyJobsLimit (evaluate compares jobsUsed+1 > limit) — D-05 says UsageSummary's figure IS evaluate()'s figure, error = %v", want, err)
	}

	setClientLimits(t, r, clientID, "image", nil, nil, int64Ptr(want+1))
	if _, err := createFixture(t, r, clientID, "image", "uploads/usq2/2-in.png", 10); err != nil {
		t.Fatalf("create with max_daily_jobs=%d (== UsageSummary's jobsUsed + 1) must be admitted — D-05 says UsageSummary's figure IS evaluate()'s figure, got: %v", want+1, err)
	}
}

// TestUsageSummaryCountsCanceledTerminalRows is the explicit guard against
// the stale `status IN ('done','failed')` reading (RESEARCH.md Pitfall 1):
// a canceled job's rollup row must be summed exactly like a done/failed row.
func TestUsageSummaryCountsCanceledTerminalRows(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	seedDailyUsage(t, r, clientID, "image", 500, 3)
	if _, err := r.pool.Exec(ctx, `
		INSERT INTO client_usage_daily (client_id, engine, day, status, job_count, bytes_in, bytes_out, wall_seconds)
		VALUES ($1, 'image', (now() AT TIME ZONE 'utc')::date, 'canceled', 2, 300, 0, 0)`,
		clientID); err != nil {
		t.Fatalf("seed canceled usage: %v", err)
	}

	today := utcToday(t, r)
	rows, err := r.UsageSummary(ctx, &clientID, today, today)
	if err != nil {
		t.Fatalf("UsageSummary: %v", err)
	}
	row := findUsageRow(rows, clientID, "image")
	if row == nil {
		t.Fatalf("UsageSummary did not return a row for (client=%s, engine=image)", clientID)
	}
	if row.TerminalJobCount != 5 {
		t.Errorf("TerminalJobCount = %d, want 5 (3 done + 2 canceled) — a canceled row's job_count must be summed exactly like a done row (migration 0014, D-05)", row.TerminalJobCount)
	}
	if row.TerminalBytesIn != 800 {
		t.Errorf("TerminalBytesIn = %d, want 800 (500 done + 300 canceled) — a canceled row's bytes_in must be summed exactly like a done row (migration 0014, D-05)", row.TerminalBytesIn)
	}
}

// TestUsageSummaryMultiDayRangeIsInclusive pins the inclusive day-range
// semantics: a range covering only today excludes an older row, and a wider
// range includes it.
func TestUsageSummaryMultiDayRangeIsInclusive(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	seedDailyUsage(t, r, clientID, "image", 500, 3)
	if _, err := r.pool.Exec(ctx, `
		INSERT INTO client_usage_daily (client_id, engine, day, status, job_count, bytes_in, bytes_out, wall_seconds)
		VALUES ($1, 'image', (now() AT TIME ZONE 'utc')::date - 3, 'done', 4, 700, 0, 0)`,
		clientID); err != nil {
		t.Fatalf("seed 3-days-ago usage: %v", err)
	}

	today := utcToday(t, r)

	todayOnly, err := r.UsageSummary(ctx, &clientID, today, today)
	if err != nil {
		t.Fatalf("UsageSummary(today..today): %v", err)
	}
	row := findUsageRow(todayOnly, clientID, "image")
	if row == nil {
		t.Fatalf("UsageSummary(today..today) did not return a row for (client=%s, engine=image)", clientID)
	}
	if row.TerminalJobCount != 3 || row.TerminalBytesIn != 500 {
		t.Errorf("UsageSummary(today..today) = {jobs=%d, bytesIn=%d}, want {jobs=3, bytesIn=500} — a today-only range must exclude the 3-days-ago row", row.TerminalJobCount, row.TerminalBytesIn)
	}

	wideRange, err := r.UsageSummary(ctx, &clientID, today.AddDate(0, 0, -6), today)
	if err != nil {
		t.Fatalf("UsageSummary(today-6..today): %v", err)
	}
	row = findUsageRow(wideRange, clientID, "image")
	if row == nil {
		t.Fatalf("UsageSummary(today-6..today) did not return a row for (client=%s, engine=image)", clientID)
	}
	if row.TerminalJobCount != 7 || row.TerminalBytesIn != 1200 {
		t.Errorf("UsageSummary(today-6..today) = {jobs=%d, bytesIn=%d}, want {jobs=7, bytesIn=1200} — an inclusive 7-day range must sum both the today row and the 3-days-ago row", row.TerminalJobCount, row.TerminalBytesIn)
	}
}

// TestUsageSummaryAllClientsAndNarrowedAgree pins D-01's primary shape:
// clientID == nil returns every client's rows, a non-nil clientID narrows to
// one, and the narrowed client's row is field-for-field identical between
// the two calls.
func TestUsageSummaryAllClientsAndNarrowedAgree(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientA := createTestClient(t, r)
	clientB := createTestClient(t, r)

	seedDailyUsage(t, r, clientA, "image", 100, 1)
	seedDailyUsage(t, r, clientB, "image", 200, 2)

	today := utcToday(t, r)

	all, err := r.UsageSummary(ctx, nil, today, today)
	if err != nil {
		t.Fatalf("UsageSummary(nil): %v", err)
	}
	rowA := findUsageRow(all, clientA, "image")
	rowB := findUsageRow(all, clientB, "image")
	if rowA == nil {
		t.Fatalf("UsageSummary(nil) did not return a row for client A (%s)", clientA)
	}
	if rowB == nil {
		t.Fatalf("UsageSummary(nil) did not return a row for client B (%s)", clientB)
	}

	narrowedA, err := r.UsageSummary(ctx, &clientA, today, today)
	if err != nil {
		t.Fatalf("UsageSummary(&clientA): %v", err)
	}
	for _, row := range narrowedA {
		if row.ClientID == clientB {
			t.Fatalf("UsageSummary(&clientA) leaked client B's row — narrowing by clientID must exclude every other client")
		}
	}
	narrowedRowA := findUsageRow(narrowedA, clientA, "image")
	if narrowedRowA == nil {
		t.Fatalf("UsageSummary(&clientA) did not return a row for client A (%s)", clientA)
	}
	if *narrowedRowA != *rowA {
		t.Errorf("client A's row differs between UsageSummary(nil) and UsageSummary(&clientA): %+v vs %+v — the two call shapes must agree field-for-field", *rowA, *narrowedRowA)
	}
}

// TestUsageSummaryExcludesOrphanedNullClientJobs pins the WR-01 fix against
// the real production path: a client with one in-flight job is deleted (the
// real ON DELETE SET NULL path, 0001_init.sql:43), and UsageSummary's
// un-narrowed shape (clientID == nil — the only shape where the bug
// manifests, since a non-nil clientID's ($1::uuid IS NULL OR client_id = $1)
// bind already excludes NULLs) must not attribute that orphaned job to a
// phantom uuid.Nil client.
func TestUsageSummaryExcludesOrphanedNullClientJobs(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	jobID, err := createFixture(t, r, clientID, "image", "uploads/orphan/0-in.png", 111)
	if err != nil {
		t.Fatalf("create in-flight fixture: %v", err)
	}
	t.Cleanup(func() {
		if _, err := r.pool.Exec(context.Background(), `DELETE FROM jobs WHERE id = $1`, jobID); err != nil {
			t.Errorf("cleanup orphaned job %s: %v", jobID, err)
		}
	})

	if _, err := r.pool.Exec(ctx, `DELETE FROM clients WHERE id = $1`, clientID); err != nil {
		t.Fatalf("delete owning client (the real ON DELETE SET NULL path): %v", err)
	}

	var orphanCount int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE id = $1 AND client_id IS NULL`, jobID).Scan(&orphanCount); err != nil {
		t.Fatalf("sanity-check orphan state: %v", err)
	}
	if orphanCount != 1 {
		t.Fatalf("orphanCount = %d, want 1 — deleting the owning client must SET NULL jobs.client_id (0001_init.sql:43); if this fails the schema no longer nullifies on delete and this test is vacuous", orphanCount)
	}

	today := utcToday(t, r)
	rows, err := r.UsageSummary(ctx, nil, today, today)
	if err != nil {
		t.Fatalf("UsageSummary(nil): %v", err)
	}
	for _, row := range rows {
		if row.ClientID == uuid.Nil {
			t.Fatalf("UsageSummary(nil) returned a phantom row with ClientID == uuid.Nil (engine=%s, InFlightJobCount=%d) — an orphaned NULL-client job must be excluded, not attributed to a fabricated client identity (WR-01): %+v", row.Engine, row.InFlightJobCount, row)
		}
	}
}

// TestUsageSummaryMultiInputJobCountedOnce pins the two-separate-queries
// decision: a job with N job_inputs rows must contribute 1 to
// InFlightJobCount and N sizes to InFlightBytesIn — never N to the count.
func TestUsageSummaryMultiInputJobCountedOnce(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	clientID := createTestClient(t, r)

	jobID, err := createFixture(t, r, clientID, "image", "uploads/usq3/0-in.png", 100)
	if err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	if _, err := r.pool.Exec(ctx, `
		INSERT INTO job_inputs (job_id, ordinal, object_key, filename, format, size_bytes, content_type)
		VALUES ($1, 1, 'uploads/usq3/1-in.png', 'in-1.png', 'png', 50, 'image/png')`,
		jobID); err != nil {
		t.Fatalf("insert second job_inputs row: %v", err)
	}

	today := utcToday(t, r)
	rows, err := r.UsageSummary(ctx, &clientID, today, today)
	if err != nil {
		t.Fatalf("UsageSummary: %v", err)
	}
	row := findUsageRow(rows, clientID, "image")
	if row == nil {
		t.Fatalf("UsageSummary did not return a row for (client=%s, engine=image)", clientID)
	}
	if row.InFlightJobCount != 1 {
		t.Errorf("InFlightJobCount = %d, want 1 — a job with 2 job_inputs rows must contribute exactly 1 to the job count, never counting over the job_inputs join (evaluate()'s two-separate-subqueries structure, admission.go:289-294)", row.InFlightJobCount)
	}
	if row.InFlightBytesIn != 150 {
		t.Errorf("InFlightBytesIn = %d, want 150 (100 + 50) — the byte sum must total every job_inputs row for the job", row.InFlightBytesIn)
	}
}
