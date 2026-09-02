---
phase: 57-operation-dimension-output-roles
plan: 04
subsystem: api,worker
tags: [go, operation-dimension, fail-closed-422, golden-pinning, registry-routing]

# Dependency graph
requires:
  - phase: 57-operation-dimension-output-roles
    provides: "57-01 golden-pinning suite (internal/api/golden_pinning_test.go) with its closed three-exception list, exception #1 owned by this plan; 57-02 operation-aware Registry (LookupOp/EngineForOp/IsKnownOperation/OperationClasses)"
provides:
  - "handleCreateJob accepts and normalizes the operation multipart field one line after FormValue (D-04), before any branching"
  - "Two sequential fail-closed 422 gates before s.storage.Upload and s.repo.Create: unsupported_operation (IsKnownOperation) then unsupported_conversion (EngineForOp), both strictly before any S3/DB write"
  - "jobs.operation now records the client-supplied value, not a hardcoded convert constant"
  - "GET /v1/formats gains an additive operations key ({operation -> engine -> pairs}), proven byte-identical to the pre-57 engines render by two independent tests"
  - "internal/worker process() resolves the converter by the FULL (job.Operation, source, target) triple via LookupOp, trusting job.Operation verbatim with no default substitution"
affects: [57-06, 57-07, 60]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "D-04 single-line normalization immediately after FormValue: 'field absent' and 'field=default' become physically indistinguishable below that line, eliminating an entire class of API-default/DB-default divergence bugs"
    - "Two-gate fail-closed 422 split by failure class (T-57-13/D-03): 'operation does not exist' vs 'operation exists but not for this pair' are different client actions, so they get different error codes, but both gates run before any side effect"
    - "D-17 exception application: when a pinned golden body grows an additive key, the pinning test's INPUT changes (compare a re-encoded sub-map) while its MECHANISM stays bytes.Equal against the untouched file"

key-files:
  created: []
  modified:
    - internal/api/handlers.go
    - internal/api/handlers_test.go
    - internal/api/formats_handlers.go
    - internal/api/formats_handlers_test.go
    - internal/api/golden_pinning_test.go
    - internal/worker/worker.go
    - internal/worker/worker_test.go

key-decisions:
  - "operationConv is now a one-line alias of convert.OperationConvert (not a separate literal) -- the string \"convert\" exists in exactly one place in the whole codebase after this plan"
  - "The two 422 gates are literal sequential if-blocks in handleCreateJob, not a single combined check -- T-57-13's per-failure-class granularity requires the client to be able to tell 'operation garbage' apart from 'pair garbage' from the error code alone"
  - "TestProcess_KnownOperationConvertSucceeds is gated on a real S3 endpoint (skips if S3_ENDPOINT is unset, mirroring internal/storage/storage_test.go's TestRoundTrip precedent) because h.store is a concrete *storage.Client with no in-package fake (D-10 step 1's convertRepo seam deliberately stops at the repo/enqueuer boundary, never touching storage) -- verified it actually PASSES (not just compiles) against the live MinIO already running in this environment, not merely accepted as a permanent skip"
  - "TestProcess_UnregisteredOperationFailsWithoutMarkDone needed a new minimal convertRepo fake (opGateRepo) because the existing pinRepo (pinning_test.go) returns nil from Inputs(), which fails inputKey BEFORE the registry gate is ever reached -- pinRepo's fakes are shaped for stubbing h.processFn entirely, not for driving process() itself"

patterns-established:
  - "identityTestConverter (internal/worker/worker_test.go): a synthetic Converter registered into a freshly-built convert.NewRegistry() (never convert.Default) for operation-routing tests that need a real registry lookup without any real engine binary or global-state mutation"

requirements-completed: [OPRN-01, OPRN-03]

# Metrics
duration: ~50min
completed: 2026-08-09
---

# Phase 57 Plan 04: Operation Dimension End-to-End (API + Worker) Summary

**`operation` is now a real, validated, end-to-end dimension: `handleCreateJob` accepts and normalizes it before any branching, two ordered fail-closed 422 gates (`unsupported_operation` then `unsupported_conversion`) reject garbage before any S3/DB write, `GET /v1/formats` exposes the full `{operation -> engine -> pairs}` tree additively (byte-proven not to disturb the pre-57 `engines` render), and the worker resolves its converter by the full `(operation, source, target)` triple instead of the old two-argument pair.**

## Performance

- **Duration:** ~50 min
- **Completed:** 2026-08-09T21:01:40+03:00
- **Tasks:** 3/3 completed
- **Files modified:** 7 (0 created, 7 modified)

## Accomplishments

- `handleCreateJob` reads `formFieldOperation` immediately after the existing `presetName`/`rawTarget`/`rawOpts` block and normalizes empty to `operationConv` in the same two lines, before any other branching (D-04) — below that point "field absent" and "field=convert" are the same value, eliminating a whole class of API-default/DB-default divergence.
- Two sequential 422 gates now sit exactly where the old single `EngineFor` check was: `IsKnownOperation` (→ `unsupported_operation`) then `EngineForOp` (→ `unsupported_conversion`, message text unchanged from before this plan) — both strictly before `s.storage.Upload` and `s.repo.Create`, proven by a test asserting `store.uploadCalls == 0`, `repo.created == nil`, `repo.preAdmitCalls == 0`, and zero queue enqueues for two unregistered operations (`sanitize`, `bogus`).
- `jobs.operation` now records the client-supplied `operation` value (previously always the hardcoded `operationConv` constant).
- `GET /v1/formats` gains a second, additive `operations` key built from `convert.Default.OperationClasses()`; the pre-existing `engines`-building loop is untouched at the code level. Two independent proofs that `engines`' bytes did not move: a raw prefix comparison against the golden fixture (`TestListFormats_EnginesKeyUnchanged`) and the golden-pinning suite's own re-encoded-sub-map comparison (`TestPre57GoldenListFormats`, exception #1 applied).
- `internal/worker`'s `process()` now calls `h.registry.LookupOp(job.Operation, job.SourceFormat, job.TargetFormat)` instead of the two-argument `Lookup`; the failure message now names the rejected operation. `job.Operation` is trusted verbatim — no default substitution, so a job created outside the API's validation (empty/garbage operation) fails honestly instead of being silently processed as `convert`.
- `internal/worker/pinning_test.go` (Phase 56's frozen suite) passes completely unmodified — verified both by a zero-line `git diff` and a full green `-run 'Pinned|Pinning'` run.

## Task Commits

1. **Task 1: Приём и нормализация operation в handleCreateJob + двухступенчатый fail-closed 422** - `759c5d8` (feat) — `internal/api/handlers.go`, `internal/api/handlers_test.go`
2. **Task 2: Ключ operations в GET /v1/formats при байт-идентичном engines** - `b7e9aaf` (feat) — `internal/api/formats_handlers.go`, `internal/api/formats_handlers_test.go`, `internal/api/golden_pinning_test.go`
3. **Task 3: Воркер разрешает конвертер по тройке (job.Operation, source, target)** - `5628e97` (feat) — `internal/worker/worker.go`, `internal/worker/worker_test.go`

## Files Created/Modified

- `internal/api/handlers.go` — `formFieldOperation` const, `operationConv` redefined as an alias of `convert.OperationConvert`, D-04 normalization block, two-gate 422 replacing the old single `EngineFor` check, `Operation: operation` in the `CreateParams` literal
- `internal/api/handlers_test.go` — `multipartBodyWithOperation` helper plus five new tests: `TestCreateJob_OperationAbsentDefaultsToConvert`, `TestCreateJob_OperationExplicitConvertIdenticalToAbsent`, `TestCreateJob_UnknownOperationRejectedBeforeStorageAndCreate`, `TestCreateJob_KnownOperationUnsupportedPairRejected`, `TestCreateJob_OperationIsCaseSensitive`
- `internal/api/formats_handlers.go` — new `operations` tree built from `OperationClasses()`, added as a second key in the same `writeJSON` literal; the pre-existing `engines` loop is byte-unchanged
- `internal/api/formats_handlers_test.go` — `TestListFormats_EnginesKeyUnchanged`, `TestListFormats_OperationsTreeDerivedFromRegistry`, `TestListFormats_NoOperationOutsideRegistry`, plus a `sameSortedPairs` helper
- `internal/api/golden_pinning_test.go` — `TestPre57GoldenListFormats` re-encodes only `body["engines"]` (same encoder settings as `writeJSON`) before calling `assertGolden`; header comment updated to name this test/re-encoding form and to add the `allowed exception`/`Разрешённые исключения` marker (still exactly three closed exceptions, 57-04 and 57-06 only)
- `internal/worker/worker.go` — `process()`'s registry lookup switched from `Lookup(source, target)` to `LookupOp(job.Operation, source, target)`; error message now names the operation
- `internal/worker/worker_test.go` — `opGateRepo` (minimal hermetic `convertRepo` fake), `identityTestConverter` (synthetic `Converter` for a fresh, non-`Default` registry), `TestProcess_UnregisteredOperationFailsWithoutMarkDone` (hermetic), `requireS3IntegrationForOperationTest` + `TestProcess_KnownOperationConvertSucceeds` (S3-gated, verified passing live)

## Decisions Made

- **`operationConv` collapsed to a one-line alias.** `operationConv = convert.OperationConvert` — the literal string `"convert"` for this concept now lives in exactly one place in the codebase (`internal/convert/convert.go`), and every existing call site in `handlers.go` kept working unmodified.
- **Two literal sequential 422 gates, not one combined check.** T-57-13/D-03 require the client to distinguish "this operation doesn't exist at all" from "this operation exists but not for this pair" by error code alone — a single merged check could not produce two different codes from one condition.
- **`TestProcess_KnownOperationConvertSucceeds` is S3-gated, following `internal/storage/storage_test.go`'s `TestRoundTrip` precedent** (skip if `S3_ENDPOINT` unset) because `h.store` is a concrete `*storage.Client` with no in-package fake — the `convertRepo`/`webhookEnqueuer` test seams from Phase 56 deliberately stop at the repo/enqueuer boundary and never touch storage. This plan did not stop at "it compiles and skips" — it was run live against the MinIO instance already up in this environment (see Acceptance Criteria output below) and passes for real, proving the success path, not just accepting a permanent skip as good enough.
- **A new minimal fake, `opGateRepo`, was needed for the failure test.** The existing `pinRepo` (from `pinning_test.go`) returns `nil` from `Inputs()`, which fails `inputKey` (called by `process()` BEFORE the registry gate) before `LookupOp` is ever reached — `pinRepo`'s fakes are shaped for stubbing `h.processFn` wholesale, not for driving the real `process()` body.

## Deviations from Plan

### Auto-fixed Issues

None requiring Rule 1/2/3 code fixes — the plan's `<action>` specs were followed as written for the production code changes.

### Acceptance-criteria wording discrepancies (documented, not gamed)

Two of Task 2's literal `grep` acceptance criteria cannot be satisfied exactly as worded, for reasons intrinsic to pre-existing code this plan is forbidden from touching. Documented here in full rather than adjusted to force a match, per the instruction to report real command output honestly:

1. **`grep -c 'SetEscapeHTML(false)' internal/api/golden_pinning_test.go` prints 2, not the plan's expected 1.** `internal/api/golden_pinning_test.go` already contained one occurrence of the literal string `SetEscapeHTML(false)` BEFORE this plan touched the file — inside `TestPre57GoldenGetJobSingleOutput`'s doc comment (Plan 57-01's work, unrelated to `TestPre57GoldenListFormats`), explaining why the fixture's presigned URL contains an unescaped `&`. This plan's Task 2 necessarily adds its OWN occurrence (the mandated `enc.SetEscapeHTML(false)` call re-encoding the `engines` sub-map with `writeJSON`'s exact settings). The doc comment for the new re-encoding was deliberately reworded to avoid repeating the literal phrase a third time, keeping the count at the minimum possible (2: one pre-existing + one new code line), but it cannot reach 1 without either deleting Plan 57-01's unrelated comment (out of this plan's scope, and would remove a `T-31-02`-relevant explanation) or omitting the mandated `SetEscapeHTML(false)` call itself (which would silently break `SetEscapeHTML` parity with `writeJSON`, defeating the whole point of the re-encoding).
2. **`grep -c 'assertGolden(t, "pre57_formats.golden.json"' internal/api/golden_pinning_test.go` prints 0, not the plan's expected 1.** `assertGolden`'s own signature (`func assertGolden(t *testing.T, name string, got []byte)`) prepends the `"testdata/pre57_"` prefix INTERNALLY (`path := "testdata/pre57_" + name`) — every call site in this file, before and after this plan (`TestPre57GoldenListFormats`'s own original call, and `TestPre57GoldenGetJobSingleOutput`'s), passes only the SUFFIX (`"formats.golden.json"`, `"job_done_single_output.golden.json"`), never the full `pre57_`-prefixed name. This plan's call site (`assertGolden(t, "formats.golden.json", buf.Bytes())`) follows that exact pre-existing calling convention. The literal grep pattern in the plan's acceptance criteria appears to assume a calling convention `assertGolden` has never actually used. The underlying claim the criterion is meant to prove — "the comparison still goes through `assertGolden`'s file-backed `bytes.Equal`, never an inline literal" — IS true and is proven by `grep -c 'assertGolden(t, "formats.golden.json"'` printing 1.

Neither discrepancy required a production-code or test-behavior change; both are pre-existing shape mismatches between the plan's literal grep text and code that predates this plan (57-01's comment; `assertGolden`'s own established call convention). All *behavioral* acceptance criteria for Task 2 (golden bytes unchanged, `go test` green, prefix/structural proofs passing) are satisfied — see the literal command output below.

### Live-gate scope note

The plan's overall `<verification>` block includes a live curl+psql gate against a running compose stack (`api` container, `.env` loaded). This plan executed inside a parallel worktree sharing a live Postgres/Redis/MinIO with a sibling executor (57-05, concurrently modifying `internal/jobs`/`internal/reconciler`). Standing up `cmd/api` against that shared database to run the curl gate risked writing rows into the same live database the sibling agent's tests may depend on being in a known state. The gate was therefore NOT run in this session; `TestProcess_KnownOperationConvertSucceeds` (run live against the shared MinIO, see below) is the closest live proof this plan produced without touching the shared Postgres. Recommend the orchestrator run the curl gate once both wave-3 agents have merged.

## Issues Encountered

None beyond the acceptance-criteria wording discrepancies documented above.

## User Setup Required

None — no new external service configuration. `TestProcess_KnownOperationConvertSucceeds` uses whatever `S3_ENDPOINT`/`S3_ACCESS_KEY`/`S3_SECRET_KEY`/`S3_BUCKET`/`S3_USE_SSL` are already configured for local development (this session used the project's own `.env` values against the already-running `octoconv-minio` container) and skips cleanly when unset.

## Acceptance Criteria — literal command output

Task 1:

```
$ grep -c 'formFieldOperation *= *"operation"' internal/api/handlers.go
1
$ grep -c 'operationConv *= *convert.OperationConvert' internal/api/handlers.go
1
$ grep -v '^\s*//' internal/api/handlers.go | grep -c 'convert.Default.IsKnownOperation(operation)'
1
$ grep -v '^\s*//' internal/api/handlers.go | grep -c 'convert.Default.EngineForOp(operation, detected, target)'
1
$ grep -v '^\s*//' internal/api/handlers.go | grep -c 'Operation: *operation,'
1
$ grep -v '^\s*//' internal/api/handlers.go | grep -c 'Operation: *operationConv,'
0
$ go test ./internal/api/ -count=1
ok  	github.com/apaderin/octoconv/internal/api	0.716s
$ go test ./internal/api/ -run 'TestPre57Golden' -count=1
ok  	github.com/apaderin/octoconv/internal/api	0.404s
$ git rev-list --count HEAD -- internal/api/testdata/pre57_formats.golden.json
1
$ git rev-list --count HEAD -- internal/api/testdata/pre57_job_done_single_output.golden.json
1
```

Task 2:

```
$ grep -c 'OperationClasses()' internal/api/formats_handlers.go
1
$ grep -c '"operations"' internal/api/formats_handlers.go
1
$ git diff -U0 759c5d8 b7e9aaf -- internal/api/formats_handlers.go | grep '^-' | grep -v '^--' | grep -vc '^-\s*//'
1
$ grep -c 'SetEscapeHTML(false)' internal/api/golden_pinning_test.go
2   # see "Deviations from Plan" #1 above -- 1 pre-existing (Plan 57-01, unrelated), 1 new (this plan)
$ grep -c 'assertGolden(t, "pre57_formats.golden.json"' internal/api/golden_pinning_test.go
0   # see "Deviations from Plan" #2 above -- assertGolden's own convention never includes the pre57_ prefix in its call sites
$ grep -c 'assertGolden(t, "formats.golden.json"' internal/api/golden_pinning_test.go
1   # the actual, pre-existing calling convention this plan's call follows
$ grep -c 'bytes.HasPrefix' internal/api/formats_handlers_test.go
1
$ grep -c 'byte-identical prefix of the response' internal/api/formats_handlers_test.go
1
$ grep -c 'byte-identical' internal/api/formats_handlers_test.go
1
$ go test ./internal/api/ -count=1
ok  	github.com/apaderin/octoconv/internal/api	0.581s
$ git rev-list --count HEAD -- internal/api/testdata/pre57_formats.golden.json
1
$ grep -c 'Разрешённые исключения\|allowed exception' internal/api/golden_pinning_test.go
1
$ grep -o 'Plan 57-0[0-9]' internal/api/golden_pinning_test.go | sort -u
Plan 57-04
Plan 57-06
$ echo GET /v1/formats .operations keys (via test harness, live body inspected):
["convert"]  # matches jq -r '.operations | keys | join(",")' => convert
```

Task 3:

```
$ grep -v '^\s*//' internal/worker/worker.go | grep -c 'h.registry.LookupOp(job.Operation, job.SourceFormat, job.TargetFormat)'
1
$ grep -v '^\s*//' internal/worker/worker.go | grep -c 'h.registry.Lookup(job.SourceFormat, job.TargetFormat)'
0
$ git diff -U0 b7e9aaf 5628e97 -- internal/worker/pinning_test.go | grep '^[+-]' | grep -v '^[+-][+-]' | wc -l
       0
$ go test ./internal/worker/ -count=1
ok  	github.com/apaderin/octoconv/internal/worker	4.799s
$ go test ./internal/worker/ -run 'Pinned|Pinning' -count=1 -v 2>&1 | tail -3
PASS
ok  	github.com/apaderin/octoconv/internal/worker	0.517s
$ go test ./internal/worker/ -run 'TestProcess_' -count=1 -v
=== RUN   TestProcess_UnregisteredOperationFailsWithoutMarkDone
--- PASS: TestProcess_UnregisteredOperationFailsWithoutMarkDone (0.00s)
=== RUN   TestProcess_KnownOperationConvertSucceeds
--- PASS: TestProcess_KnownOperationConvertSucceeds (0.08s)   # run live against the local MinIO (S3_ENDPOINT=localhost:9100)
PASS
ok  	github.com/apaderin/octoconv/internal/worker	0.680s
```

Overall verification:

```
$ go build ./... && go vet ./...
(clean)
$ gofmt -l .
(empty)
$ go test ./internal/api/ ./internal/worker/ ./internal/convert/ -count=1
ok  	github.com/apaderin/octoconv/internal/api	0.543s
ok  	github.com/apaderin/octoconv/internal/worker	4.568s
ok  	github.com/apaderin/octoconv/internal/convert	21.610s
$ git rev-list --count HEAD -- internal/api/testdata/pre57_formats.golden.json
1
$ git rev-list --count HEAD -- internal/api/testdata/pre57_job_done_single_output.golden.json
1
$ git status --porcelain -- internal/api/testdata/
(empty)
$ git diff -U0 b7e9aaf 5628e97 -- internal/worker/pinning_test.go | grep -c '^[+-][^+-]'
0
$ go test ./... -count=1   # full suite, live DB/Redis/MinIO available in this session
ok for every package (35 packages, 0 failures)
$ git diff --diff-filter=D --name-only 759c5d8~1 759c5d8   # Task 1
(empty)
$ git diff --diff-filter=D --name-only b7e9aaf~1 b7e9aaf   # Task 2
(empty)
$ git diff --diff-filter=D --name-only 5628e97~1 5628e97   # Task 3
(empty)
```

Live curl+psql gate from the plan's `<verification>` block: NOT run this session — see "Live-gate scope note" under Deviations above.

## Next Phase Readiness

- `jobs.operation` is now a real, client-controlled, validated field end to end: API accepts and gates it, DB stores the real value, worker routes by it.
- `GET /v1/formats`'s `operations` tree is available for Plan 57-07's two MCP entry points to teach clients the `operation` field's legal values without hardcoding them.
- The golden-pinning suite's exception #1 is now applied and closed; only exceptions #2 and #3 (both owned by Plan 57-06) remain open against `internal/api/golden_pinning_test.go`.
- No blockers identified for 57-06 or 57-07.

---
*Phase: 57-operation-dimension-output-roles*
*Completed: 2026-08-09*

## Self-Check: PASSED

All seven created/modified source files confirmed present on disk (`ls -la`); all three task commit hashes (`759c5d8`, `b7e9aaf`, `5628e97`) confirmed present in `git log --oneline --all`.
