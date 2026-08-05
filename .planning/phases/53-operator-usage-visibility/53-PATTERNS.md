# Phase 53: Operator Usage Visibility - Pattern Map

**Mapped:** 2026-08-05
**Files analyzed:** 8 (new: 4-5, modified: 3-4, depending on planner's interface split)
**Analogs found:** 8 / 8

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|--------------------|------|-----------|-----------------|----------------|
| `internal/api/system_usage_handlers.go` (new) | controller | request-response | `internal/api/system_presets_handlers.go` (operator gating) + `internal/api/jobs_list_handler.go` (query-param validation) | exact (composite of two governing precedents) |
| `internal/api/routes.go` (modify) | route | request-response | itself — add sibling `r.Route("/system/usage", ...)` next to the existing `/system/presets` block | exact — copy-shape template |
| `internal/api/api.go` (modify) | provider/interface | request-response | itself — `Repo`/`PresetRepo`/`PresetAdmin` interface-segregation precedent + `Server` struct + `NewServer`/`Config` defaulting idiom | exact — copy-shape template |
| `internal/jobs/usage_query.go` (new, name Claude's discretion) | model/service (repo method) | CRUD (aggregate read) | `internal/jobs/admission.go` `evaluate()`/`loadQuotaState` (SQL shape to mirror) | exact for SQL formula; role-match for file placement (`internal/jobs/usage.go` is the write-side sibling) |
| `internal/clients/limits.go` (modify — add `ListAllLimits` method) | model/service (repo method) | CRUD (read) | `internal/clients/limits.go` `ListLimits` (same file, single-client precedent to generalize) | exact — copy-shape template |
| `cmd/api/main.go` (modify) | config/wiring | request-response | itself — existing `clientRepo`/`api.NewServer(...)` call site | exact — copy-shape template |
| `internal/api/system_usage_handlers_test.go` (new) | test | request-response | `internal/api/system_presets_handlers_test.go` (no-leak-404 idiom) + `internal/api/handlers_test.go` (`fakeRepo`/`fakeResolver`/`newTestServer` idiom) | exact (composite) |
| `internal/jobs/usage_query_test.go` (new, or appended to `admission_test.go`) | test | CRUD (aggregate read) | `internal/jobs/admission_test.go`/`repo_test.go` (`DATABASE_URL`-gated integration test, `newTestRepo`, `createFixture`, `setClientLimits`, `seedDailyUsage` helpers) | exact — copy-shape template, helpers directly reusable |

## Pattern Assignments

### `internal/api/system_usage_handlers.go` (controller, request-response)

**Analogs:** `internal/api/system_presets_handlers.go` (copy-shape for the file's doc comment + operator-gating framing) and `internal/api/jobs_list_handler.go` (copy-shape for the fail-closed query-param validation body).

**Operator gating — nothing to write, only reuse** (`internal/api/system_presets_handlers.go:54-67`):
```go
func (s *Server) requireOperator(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client, ok := auth.ClientFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusNotFound, noSuchPreset)
			return
		}
		if _, isOperator := s.operators[client.ID]; !isOperator {
			writeError(w, http.StatusNotFound, noSuchPreset)
			return
		}
		next.ServeHTTP(w, r)
	})
}
```
No changes to this function. `noSuchPreset` is declared in `internal/api/presets_handlers.go:26` as `"preset not found"` — the new endpoint's non-operator 404 MUST reuse this exact constant, not a new phase-specific string (research already confirms this; do not invent `"usage not found"`).

**Fail-closed query-param validation pattern to copy** (`internal/api/jobs_list_handler.go:12-22,29-49`):
```go
var listJobsValidStatuses = map[string]struct{}{
	jobs.StatusAwaitingUpload: {},
	jobs.StatusQueued:         {},
	jobs.StatusActive:         {},
	jobs.StatusDone:           {},
	jobs.StatusFailed:         {},
	jobs.StatusCanceled:       {},
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	client, _ := auth.ClientFromContext(ctx)

	q := r.URL.Query()

	status := q.Get("status")
	if status != "" {
		if _, ok := listJobsValidStatuses[status]; !ok {
			writeError(w, http.StatusUnprocessableEntity, "invalid status filter")
			return
		}
	}
	// ... engine validated the same way against convert.Default.Classes()
```
Apply the identical shape for `?period`: declare `var validUsagePeriods = map[string]struct{}{"today": {}, "7d": {}, "30d": {}}`, default to `"today"` when absent (D-04), reject with `422` via `writeError(w, http.StatusUnprocessableEntity, "invalid period filter")` BEFORE any DB call — same order-of-operations as `handleListJobs` (validate everything, then call the repo once).

**`?client_id=` optional-narrowing parse** — no existing exact analog for an *optional* UUID query param, but `jobs.DecodeCursor`'s error-mapping shape in the same file (`jobs_list_handler.go:64-72`) is the pattern to copy for the parse-error branch:
```go
var cursor *jobs.Cursor
if v := q.Get("cursor"); v != "" {
	c, err := jobs.DecodeCursor(v)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cursor")
		return
	}
	cursor = &c
}
```
Mirror this exactly for `?client_id=`: `uuid.Parse(v)` → `400` on error, else `&id`. (`uuid.Parse` + 400 is also used identically in `cmd/manage-clients/main.go:166-169` and throughout `internal/api/handlers.go` for `chi.URLParam` ids — same idiom, different source.)

**Non-nil empty slice / response shape convention** (`internal/api/jobs_list_handler.go:80-84`):
```go
// Non-nil so an empty result marshals to [] rather than null (D-02).
items := make([]map[string]any, 0, len(summaries))
```
Apply the same to the usage response list.

**writeJSON/writeError — reuse as-is** (`internal/api/handlers.go:968-975,1031-1033`):
```go
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
```
No new response-writing helper is needed or wanted.

**UTC day-boundary convention to copy** (`internal/api/handlers.go:986-995`, `secondsUntilUTCMidnight`):
```go
func secondsUntilUTCMidnight(now time.Time) int {
	now = now.UTC()
	nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	...
}
```
The new handler's `period → [dayFrom, dayTo]` resolver must call `.UTC()` on `time.Now()` before deriving day boundaries, exactly like this function does — this is the established idiom for avoiding local-timezone drift against `client_usage_daily.day` (written via Postgres `(now() AT TIME ZONE 'utc')::date`).

---

### `internal/api/routes.go` (route registration, request-response)

**Analog:** itself, `internal/api/routes.go:58-70` (the `/v1/system/presets` block) — copy-shape template.

```go
// internal/api/routes.go:58-70 — the exact shape to replicate for /system/usage
r.Route("/system/presets", func(r chi.Router) {
	r.Use(s.requireOperator)
	r.Post("/", s.handleCreateSystemPreset)
	r.Get("/", s.handleListSystemPresets)
	r.Get("/{name}", s.handleShowSystemPreset)
	r.Put("/{name}", s.handleUpdateSystemPreset)
	r.Delete("/{name}", s.handleDeactivateSystemPreset)
})
```
New code (sibling block, same `r.Route("/v1", ...)` scope, lines 35-71):
```go
r.Route("/system/usage", func(r chi.Router) {
	r.Use(s.requireOperator)
	r.Get("/", s.handleSystemUsage)
})
```
Place it immediately after the `/system/presets` block so both operator-gated subtrees stay visually adjacent, matching the existing comment at line 58-62 explaining why `/system/...` is a distinct literal segment (no chi routing conflict with `/v1/presets/{name}`).

---

### `internal/api/api.go` (provider/interface, request-response)

**Analog:** itself — the `Repo`/`PresetRepo`/`PresetAdmin` narrow-interface pattern (`internal/api/api.go:18-38,51-72`) and the `Server` struct / `NewServer` zero-value-defaulting idiom (`internal/api/api.go:106-169,235-332`).

**Interface-segregation pattern to copy** (`internal/api/api.go:51-72`, `PresetRepo`/`PresetAdmin` — two narrow interfaces, same underlying concrete repo):
```go
type PresetRepo interface {
	Resolve(ctx context.Context, clientID uuid.UUID, name string) (*presets.Preset, error)
}

type PresetAdmin interface {
	Create(ctx context.Context, p presets.CreateParams) (uuid.UUID, int, error)
	Update(...) (int, error)
	Deactivate(...) error
	Get(...) (*presets.Preset, error)
	List(...) ([]presets.Preset, error)
	ListForClient(...) ([]presets.Preset, error)
	GetForClient(...) (*presets.Preset, error)
}
```
Research's recommendation (confirmed sound against this precedent): add two new narrow interfaces rather than widening the existing jobs-only `Repo` — e.g. `UsageRepo` (backed by `*jobs.Repo`, one new method) and `LimitsRepo` (backed by `*clients.Repo`, one new method). This is the SAME shape as `PresetRepo`+`PresetAdmin` both being backed by `*presets.Repo` — do not collapse into one interface, and do not add the new method(s) to the existing `Repo` interface (that would force `fakeRepo` in `handlers_test.go` to grow a method every existing job-handler test doesn't exercise, breaking interface segregation — see `api.go`'s own `Repo` interface at lines 18-38, which is deliberately jobs-scoped only).

**`Server` struct field + `NewServer` positional-param idiom to copy** (`internal/api/api.go:106-169,235-332`):
```go
type Server struct {
	repo          Repo
	storage       Storage
	queue         Enqueuer
	presets       PresetRepo
	presetAdmin   PresetAdmin
	resolver      auth.ClientResolver
	health        HealthDeps
	...
	operators map[uuid.UUID]struct{}
	...
}

func NewServer(repo Repo, storage Storage, queue Enqueuer, presets PresetRepo, presetAdmin PresetAdmin, resolver auth.ClientResolver, health HealthDeps, cfg Config) *Server {
	...
	operators := cfg.OperatorClientIDs
	if operators == nil {
		operators = map[uuid.UUID]struct{}{}
	}
	return &Server{ ... }
}
```
New fields (`usageRepo UsageRepo`, `limitsRepo LimitsRepo`) should be added as new POSITIONAL parameters to `NewServer` (matching how `presets`/`presetAdmin` were added as positional params, not folded into `Config` — `Config` is reserved for tunables/scalars per the existing doc comment convention, not for dependency interfaces). This changes `NewServer`'s signature and therefore every call site (`cmd/api/main.go:170`, `internal/api/handlers_test.go`'s `newTestServer` at line 676, and `internal/api/system_presets_handlers_test.go`'s `newOperatorGateTestServer` at line 66-71) — all must be updated to pass the two new args.

---

### `internal/jobs/usage_query.go` (new — repo read method, CRUD/aggregate)

**Analog:** `internal/jobs/admission.go` `evaluate()`/`loadQuotaState` (`internal/jobs/admission.go:190-364`) — this is the load-bearing SQL formula D-05 requires byte-for-byte replication of, not just a style reference.

**Terminal-usage aggregate — copy the exact no-status-filter shape** (`internal/jobs/admission.go:284-295`, condensed):
```go
COALESCE((SELECT sum(bytes_in) FROM client_usage_daily
    WHERE client_id = $1 AND engine = $2 AND day = (now() AT TIME ZONE 'utc')::date), 0)
+ COALESCE((SELECT sum(ji.size_bytes) FROM jobs j JOIN job_inputs ji ON ji.job_id = j.id
    WHERE j.client_id = $1 AND j.engine = $2 AND j.status IN ('queued', 'active')), 0),
COALESCE((SELECT sum(job_count) FROM client_usage_daily
    WHERE client_id = $1 AND engine = $2 AND day = (now() AT TIME ZONE 'utc')::date), 0)
+ COALESCE((SELECT count(*) FROM jobs j
    WHERE j.client_id = $1 AND j.engine = $2 AND j.status IN ('queued', 'active')), 0)
```
**CRITICAL — no `status IN (...)` filter on `client_usage_daily`** in this subquery (sums across `done`/`failed`/`canceled` unfiltered) — this is what D-05 requires the new query to also do. Do not reintroduce a `status IN ('done','failed')` filter (that was migration 0011's original constraint, superseded by `0014_client_usage_canceled.sql`).

**In-flight literal-vs-bound-param split — copy exactly** (`internal/jobs/admission.go:295-304`, comment explaining why):
```go
// The in-flight subqueries above write their two statuses as SQL
// literals, NOT as bound parameters: the literal form is what the
// planner can match against jobs_client_engine_inflight_idx's
// partial predicate (WHERE status IN ('queued', 'active'))
```
The new all-clients/`GROUP BY client_id, engine` query for in-flight counts must keep `'queued', 'active'` as literal SQL strings, binding only `client_id`/`engine`/date-range as real `$n` parameters — exactly as `evaluate()` does. This is Pitfall 2 from RESEARCH.md; verify with `EXPLAIN ANALYZE` during implementation that `jobs_client_engine_inflight_idx` is used, not a sequential scan.

**`quotaState`/NULL-safe limit fields — copy the type shape, not the exact SQL** (`internal/jobs/admission.go:190-203`):
```go
type quotaState struct {
	enforcementMode string
	maxConcurrent   *int
	maxDailyBytes   *int64
	maxDailyJobs    *int64
}
func (st quotaState) unlimited() bool {
	return st.maxConcurrent == nil && st.maxDailyBytes == nil && st.maxDailyJobs == nil
}
```
The new `UsageRow`/response type needs the equivalent NULL-safe pointer fields for the limits half — do not coerce to bare `int`/`int64` with a 0 default (Pitfall 5).

**`pgxQuerier` interface for dual pool/tx use** (`internal/jobs/admission.go:180-186`) — likely NOT needed for the new read (it runs outside any transaction/advisory lock per CONTEXT.md's Integration Points note: "this read does NOT need to run under `admitLocked`'s advisory lock"), but note its existence in case the planner wants the new method testable against both `*pgxpool.Pool` and a `pgx.Tx` for consistency with the rest of the package's style.

**File-placement precedent:** `internal/jobs/usage.go` (`recordUsage`, the WRITE-side sibling of this new READ) is the closer file-role analog for where to place the new file within the package — same package doc-comment convention (package-level comment on one file explaining the file's role, per CLAUDE.md "Comments" convention), same `func (r *Repo) MethodName(...)` receiver shape:
```go
// internal/jobs/usage.go:45 — the receiver/signature shape to mirror
func (r *Repo) recordUsage(ctx context.Context, tx pgx.Tx, id uuid.UUID, status string, attempt time.Duration) error {
```
New method signature per RESEARCH.md's recommendation: `func (r *Repo) UsageSummary(ctx context.Context, clientID *uuid.UUID, dayFrom, dayTo time.Time) ([]UsageRow, error)` — exported (unlike `recordUsage`), since it is called cross-package from `internal/api`.

---

### `internal/clients/limits.go` (modify — add `ListAllLimits`)

**Analog:** itself, `ListLimits` (`internal/clients/limits.go:85-110`) — copy-shape template, generalize the `WHERE client_id = $1` to an optional filter.

```go
// internal/clients/limits.go:87-110 — the exact shape to generalize
func (r *Repo) ListLimits(ctx context.Context, clientID uuid.UUID) ([]Limit, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT engine, max_concurrent, max_daily_bytes, max_daily_jobs
		FROM client_limits WHERE client_id = $1 ORDER BY engine`,
		clientID,
	)
	if err != nil {
		return nil, fmt.Errorf("list client limits: %w", err)
	}
	defer rows.Close()

	var out []Limit
	for rows.Next() {
		var l Limit
		if err := rows.Scan(&l.Engine, &l.MaxConcurrent, &l.MaxDailyBytes, &l.MaxDailyJobs); err != nil {
			return nil, fmt.Errorf("scan client limit: %w", err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list client limits: %w", err)
	}
	return out, nil
}
```
New `ListAllLimits(ctx context.Context, clientID *uuid.UUID) ([]LimitWithMeta, error)` should copy this row-scan loop verbatim, swapping the `WHERE` clause for the optional-narrowing idiom (`WHERE ($1::uuid IS NULL OR client_id = $1)`, per RESEARCH.md's Code Examples section) and adding a `JOIN clients c ON c.id = client_id` to also select `c.enforcement_mode` (Claude's Discretion, RESEARCH.md recommends including it). The `Limit` struct's pointer-field convention (`internal/clients/limits.go:20-28`) is the exact shape the new `LimitWithMeta` (or equivalent) type must reuse:
```go
type Limit struct {
	Engine        string
	MaxConcurrent *int
	MaxDailyBytes *int64
	MaxDailyJobs  *int64
}
```

**Error-wrap convention** — every method in this file wraps with `fmt.Errorf("<action>: %w", err)` (`internal/clients/limits.go:46,61,80,94,107`); the new method must do the same, e.g. `fmt.Errorf("list all client limits: %w", err)`.

---

### `cmd/api/main.go` (wiring, request-response)

**Analog:** itself, the existing `clientRepo`/`api.NewServer(...)` call site (`cmd/api/main.go:107-108,169-205`) — copy-shape template, minimal change.

```go
// cmd/api/main.go:107-108 — clientRepo already constructed, NOT yet passed to NewServer
clientRepo := clients.NewRepo(pool)
resolver := auth.NewResolver(clientRepo, salt)
...
// cmd/api/main.go:169-170 — jobsRepo constructed immediately before NewServer
jobsRepo := jobs.NewRepo(pool)
srv := api.NewServer(jobsRepo, store, qc, presetRepo, presetRepo, resolver, health, api.Config{...})
```
Change: pass `jobsRepo` (already satisfies the new `UsageRepo` interface once `UsageSummary` is added to `*jobs.Repo`) and `clientRepo` (already constructed at line 107, already satisfies the new `LimitsRepo` interface once `ListAllLimits` is added to `*clients.Repo`) as two new positional args to `api.NewServer(...)`. No new construction needed — both concrete repos already exist in this function; only the call site's arg list changes.

---

## Shared Patterns

### Operator-gated route + no-leak 404
**Source:** `internal/api/system_presets_handlers.go:54-67` (`requireOperator`), `internal/api/presets_handlers.go:26` (`noSuchPreset` constant)
**Apply to:** `internal/api/system_usage_handlers.go`'s new route registration in `routes.go`
**Reuse as-is — zero changes needed.** The endpoint's non-operator response body MUST be byte-identical to every other `/v1/system/...` 404, verified via `writeError(wantRec, http.StatusNotFound, noSuchPreset)` comparison in tests (see Test Patterns below).

### Fail-closed query-param validation before DB work
**Source:** `internal/api/jobs_list_handler.go:16-23,36-42` (`listJobsValidStatuses` allow-set pattern)
**Apply to:** `?period` validation in the new handler — reject 422 via `writeError(w, http.StatusUnprocessableEntity, "invalid period filter")` before any repo call, exact same idiom as `?status`/`?engine`.

### Response writing
**Source:** `internal/api/handlers.go:968-975` (`writeJSON`), `:1031-1033` (`writeError`)
**Apply to:** All new handler code — no new helper needed.

### UTC day-boundary derivation
**Source:** `internal/api/handlers.go:986-995` (`secondsUntilUTCMidnight`'s `.UTC()` convention)
**Apply to:** The new handler's `?period → [dayFrom, dayTo]` resolution logic — must call `.UTC()` before deriving day boundaries to stay consistent with `client_usage_daily.day` and `evaluate()`'s own UTC-day filter (Pitfall 4).

### NULL-safe nullable-limit fields, never a sentinel number
**Source:** `internal/clients/limits.go:20-28` (`Limit` struct doc comment + pointer fields), `internal/jobs/admission.go:190-203` (`quotaState`)
**Apply to:** The new response/row types for both the usage-query repo method and the limits repo method — every nullable limit column stays a Go pointer type end-to-end, only becoming JSON `null` at the final `writeJSON` marshal, never coerced to a bare `0`.

### Error wrapping
**Source:** `internal/clients/limits.go` (every method), `internal/jobs/repo.go`, `internal/storage/storage.go`
**Apply to:** New repo methods in both `internal/jobs` and `internal/clients` — `fmt.Errorf("<action>: %w", err)`.

## Test Patterns

### No-leak-404 byte-identity test
**Source:** `internal/api/system_presets_handlers_test.go:101-128` (`TestRequireOperator_NonOperatorNoLeak404`)
```go
req := httptest.NewRequest(http.MethodGet, "/v1/system/usage", nil)
req = req.WithContext(auth.WithClient(req.Context(), other))
rec := httptest.NewRecorder()
srv.Routes().ServeHTTP(rec, req)

if rec.Code != http.StatusNotFound {
	t.Fatalf("status = %d, want 404", rec.Code)
}
wantRec := httptest.NewRecorder()
writeError(wantRec, http.StatusNotFound, noSuchPreset)
if !bytes.Equal(rec.Body.Bytes(), wantRec.Body.Bytes()) {
	t.Errorf("body = %q, want byte-identical to noSuchPreset 404 %q", rec.Body.String(), wantRec.Body.String())
}
```
**Apply to:** The new endpoint's SC3 (no-leak 404) test — copy this pattern verbatim, swapping the path.

### Hand-written fake dependency idiom (no mocking library)
**Source:** `internal/api/handlers_test.go:34-101` (`fakeRepo`) — capture-args-and-configurable-returns shape
```go
type fakeRepo struct {
	listResult   []jobs.JobSummary
	listNext     *jobs.Cursor
	listErr      error
	listClientID uuid.UUID
	listFilter   jobs.ListFilter
	listCursor   *jobs.Cursor
	listLimit    int
}
func (f *fakeRepo) List(_ context.Context, clientID uuid.UUID, filter jobs.ListFilter, cursor *jobs.Cursor, limit int) ([]jobs.JobSummary, *jobs.Cursor, error) {
	f.listClientID = clientID
	f.listFilter = filter
	f.listCursor = cursor
	f.listLimit = limit
	return f.listResult, f.listNext, f.listErr
}
```
**Apply to:** New `fakeUsageRepo`/`fakeLimitsRepo` (or equivalent names) in `internal/api/handlers_test.go` or a new `_test.go` file — same capture-then-return shape, one field pair per constructor arg + one configurable-error field.

**`newTestServer` / `newOperatorGateTestServer` call-site update required** (`internal/api/handlers_test.go:676-690`, `internal/api/system_presets_handlers_test.go:66-71`) — both construct `Server` via `NewServer(...)` with a fixed positional-arg list; adding `UsageRepo`/`LimitsRepo` params to `NewServer` means BOTH these test helpers need the two new fake args threaded through, or a new helper (e.g. `newUsageTestServer`) if the existing helpers' signatures shouldn't grow for unrelated (non-usage) tests. Recommend threading through both existing helpers (matching how `presets`/`presetAdmin` were added originally) rather than adding a third parallel constructor.

### DB-backed integration test with reusable fixture helpers
**Source:** `internal/jobs/admission_test.go` + `internal/jobs/repo_test.go:15-30` — `newTestRepo`, `createFixture`, `setClientLimits`, `setEnforcementMode`, `seedDailyUsage` (all unexported, same-package, directly reusable by a new `usage_query_test.go` in `internal/jobs`)
```go
func newTestRepo(t *testing.T) *Repo {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
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
	return NewRepo(pool)
}
```
Existing helper signatures found in `internal/jobs/admission_test.go:23-83` — `setClientLimits(t, r, clientID, engine, maxConcurrent, maxDailyBytes, maxDailyJobs)`, `setEnforcementMode(t, r, clientID, mode)`, `seedDailyUsage(t, r, clientID, engine, bytesIn, jobCount)`, `createFixture(t, r, clientID, engine, objectKey, sizeBytes) (uuid.UUID, error)`, `int64Ptr(v int64) *int64` — all are unexported same-package helpers the new `UsageSummary` integration test can call directly without redefining them (place the new test file in `internal/jobs`, same package, to inherit them).
**Apply to:** `internal/jobs.UsageSummary`'s integration test — build fixtures with `seedDailyUsage`/`createFixture`, assert the aggregate matches what `evaluate()` would independently compute for the `today` case (cross-check against `evaluate()` itself is the strongest possible test for D-05's "mirrors exactly" requirement).

## No Analog Found

None. Every file this phase touches has at least a role-match analog in the existing codebase; no file requires falling back to RESEARCH.md's Code Examples as a sole source (though RESEARCH.md's condensed SQL in "Code Examples" is still a useful cross-check against the `evaluate()` excerpts extracted above).

## Metadata

**Analog search scope:** `internal/api/`, `internal/jobs/`, `internal/clients/`, `cmd/api/`, `cmd/manage-clients/` — the full set RESEARCH.md's Sources section already enumerated; no new directories needed to be searched.
**Files scanned (this pass):** `internal/api/system_presets_handlers.go`, `internal/api/system_presets_handlers_test.go`, `internal/api/jobs_list_handler.go`, `internal/api/api.go`, `internal/api/routes.go`, `internal/api/handlers.go` (lines 960-1034), `internal/api/handlers_test.go` (lines 34-150+), `internal/jobs/admission.go` (lines 180-364), `internal/jobs/usage.go`, `internal/jobs/jobs.go` (status constants), `internal/jobs/admission_test.go` (function index), `internal/jobs/repo_test.go` (lines 1-30), `internal/clients/limits.go`, `cmd/api/main.go` (lines 95-214), `cmd/manage-clients/main.go` (lines 159-293, `formatOptionalInt`/`formatOptionalInt64`, `show-limits`).
**Pattern extraction date:** 2026-08-05
