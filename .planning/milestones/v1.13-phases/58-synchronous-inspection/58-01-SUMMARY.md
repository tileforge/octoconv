---
phase: 58-synchronous-inspection
plan: 01
subsystem: api
tags: [golden-testing, errcode, openapi, error-codes, regression-pinning]

# Dependency graph
requires:
  - phase: 57-operation-dimension-output-roles
    provides: "D-17 pinning protocol pattern (golden_pinning_test.go) reused here as D-15"
  - phase: 56-jobrunner-consolidation-structured-error-codes
    provides: "internal/errcode vocabulary, docs/ERROR-CODES.md contract, doc_sync_test.go / openapi_sync_test.go parity tests"
provides:
  - "Byte-exact pre-Phase-58 snapshots of GET /healthz, GET /v1/formats, and all ten POST /v1/jobs content-validation rejections, committed as the phase's first commit"
  - "errcode.InspectBusy sync error code (16th code), synced across Go/docs/openapi"
affects: [58-02, 58-03, 58-04, 58-05, 58-06, 58-07, 58-08]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "D-15 golden pinning suite: distinct -update-golden-58 flag (avoids flag-redefined panic with golden_pinning_test.go's -update-golden), assertGolden58 helper reads testdata/pre58_* directly off disk, never an inline literal"
    - "Single golden file for a table of rejection cases: map[string]{status,body} encoded once with SetEscapeHTML(false)+SetIndent, relying on Go's automatic lexicographic map-key sort for a deterministic diff"

key-files:
  created:
    - internal/api/pre58_pinning_test.go
    - internal/api/testdata/pre58_healthz.golden.json
    - internal/api/testdata/pre58_formats.golden.json
    - internal/api/testdata/pre58_createjob_rejections.golden.json
  modified:
    - internal/errcode/errcode.go
    - docs/ERROR-CODES.md
    - docs/openapi.yaml

key-decisions:
  - "gofmt's const-block column alignment would have padded `InspectBusy   = ...` to align with the adjacent `InternalError` line, which conflicts with the plan's literal acceptance-criteria grep pattern `InspectBusy = \"inspect_busy\"` (single space) -- resolved by inserting a blank line between the two const specs so gofmt treats them as separate alignment groups, matching the existing UnsupportedOperation precedent in the same file."
  - "unrecognized_content golden case uses plain ASCII garbage bytes under a .png filename rather than a fixture-derived non-match, keeping the pinning suite free of any dependency on convert package internals beyond Sniff's public failure path."

requirements-completed: [INSP-05]

# Metrics
duration: 13min
completed: 2026-08-10
---

# Phase 58 Plan 01: Pre-Phase-58 Golden Pinning + inspect_busy Error Code Summary

**Byte-pinned pre-Phase-58 bodies of /healthz, /v1/formats, and all ten POST /v1/jobs content-validation rejections, plus a new `inspect_busy` 429 sync error code synced across Go constants, ERROR-CODES.md, and openapi.yaml.**

## Performance

- **Duration:** 13 min
- **Started:** 2026-08-10T00:49:01Z (worktree base commit)
- **Completed:** 2026-08-10T01:01:39Z
- **Tasks:** 2
- **Files modified:** 7 (4 created, 3 modified)

## Accomplishments
- Committed three byte-exact golden fixtures (`pre58_healthz.golden.json`, `pre58_formats.golden.json`, `pre58_createjob_rejections.golden.json`) in a commit containing ONLY the pinning suite and fixtures -- zero production-code lines, verified by `git show --name-only`
- Pinning suite (`pre58_pinning_test.go`) header declares a closed, currently-empty exceptions list and the `git rev-list --count` immutability check, mirroring the D-17/57 protocol under the D-15 name
- All ten distinguishable POST /v1/jobs content-validation rejections captured in one table-driven golden file, keyed by their `reason=` log token, with both status AND body pinned per case (engine_size_limit is the sole 413 among nine 422s)
- Added `errcode.InspectBusy = "inspect_busy"` as the service's third distinct 429 class, positioned directly after `RateLimited`; updated `all()`, the doc-comment code count, `docs/ERROR-CODES.md`'s sync table, and `docs/openapi.yaml`'s `Error.code` enum
- Proved the openapi parity test is alive (D-16 inversion): temporarily removed the `- inspect_busy` enum line, confirmed `TestOpenAPIErrorCodeEnumMatchesGoConstants` fails naming `inspect_busy` as missing, restored the line, confirmed green again

## Task Commits

Each task was committed atomically:

1. **Task 1: Снять golden-снимки /healthz, /v1/formats и всех content-validation отказов POST /v1/jobs** - `ea3f2c8` (test)
2. **Task 2: Добавить sync-код inspect_busy в errcode, docs/ERROR-CODES.md и enum openapi** - `db68dfa` (feat)

**Plan metadata:** (this commit, following)

## Files Created/Modified
- `internal/api/pre58_pinning_test.go` - D-15 golden pinning suite for /healthz, /v1/formats, and 10 POST /v1/jobs rejection cases; `-update-golden-58` flag + `assertGolden58` helper
- `internal/api/testdata/pre58_healthz.golden.json` - pre-Phase-58 byte snapshot of GET /healthz (all deps healthy)
- `internal/api/testdata/pre58_formats.golden.json` - pre-Phase-58 byte snapshot of GET /v1/formats (full body, including the 57-04 `operations` key)
- `internal/api/testdata/pre58_createjob_rejections.golden.json` - pre-Phase-58 {status, body} snapshot of all 10 content-validation rejection cases
- `internal/errcode/errcode.go` - adds `InspectBusy` constant + `all()` entry + updated doc-comment code count (fifteen -> sixteen)
- `docs/ERROR-CODES.md` - adds the `inspect_busy` sync-table row; updates "Fifteen codes" -> "Sixteen codes"
- `docs/openapi.yaml` - adds `- inspect_busy` to the `Error.code` enum, 12-space indent, positioned after `rate_limited`

## Decisions Made
- Golden pinning test file placed as a NEW file (`pre58_pinning_test.go`) alongside, not replacing, `golden_pinning_test.go` (57's suite) -- both suites coexist permanently, each pinning a disjoint set of endpoints/bodies for its own phase.
- The `updateGolden58` flag is deliberately named to differ from `golden_pinning_test.go`'s `updateGolden` to avoid a `flag redefined` panic when both test files are compiled into the same `api` test binary -- verified by running `go test ./internal/api/...` (both suites present) without a panic.
- InspectBusy's doc comment explicitly contrasts all three 429 classes (`rate_limited`, `quota_exceeded`, `inspect_busy`) inline so a future reader never has to cross-reference three separate files to understand why three codes share one HTTP status.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Blank line inserted to break gofmt's const-block alignment group**
- **Found during:** Task 2 (adding `errcode.InspectBusy`)
- **Issue:** `gofmt -w` aligned `InspectBusy` and the adjacent `InternalError` constant into one alignment group, padding `InspectBusy`'s `=` with extra spaces (`InspectBusy   = "inspect_busy"`) to match `InternalError`'s longer identifier width. This does not match the plan's literal acceptance-criteria grep pattern `InspectBusy = "inspect_busy"` (single space) and would have made the acceptance check fail despite the code being logically correct.
- **Fix:** Inserted a blank line between the `InspectBusy` and `InternalError` const specs, matching the existing precedent in the same file where `UnsupportedOperation` (preceded by a multi-line doc comment, like `InspectBusy`) forms its own single-line alignment group rather than joining an adjacent group. Re-ran `gofmt -w` and `gofmt -l` to confirm the file is fully gofmt-clean.
- **Files modified:** internal/errcode/errcode.go
- **Verification:** `grep -c 'InspectBusy = "inspect_busy"' internal/errcode/errcode.go` -> 1; `gofmt -l internal/errcode` -> empty
- **Committed in:** db68dfa (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (1 bug/formatting)
**Impact on plan:** Purely cosmetic formatting fix required to satisfy the plan's own literal acceptance criterion; no behavioral change, no scope creep.

## Issues Encountered
- The header comment's required literal phrase "Исключений нет" was initially split across a line wrap (word-wrapped comment lines), so the acceptance check `grep -qF "Исключений нет"` failed on the first pass since `grep -F` does not match across a newline. Fixed by rewording the wrap so the two words sit on the same source line; verified with the same grep command before committing.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Golden fixtures are in place and immutability-checked (`git rev-list --count` == 1 for all three); every later Phase 58 plan that touches `/healthz` (58-03) or `POST /v1/jobs` content-validation (58-02/D-04) must leave `TestPre58Golden*` green without modifying this suite's Go code or the golden bytes, per the closed (empty) exceptions list in the header.
- `errcode.InspectBusy` is declared and synced across all three representations (Go, ERROR-CODES.md, openapi.yaml) and ready for Plan 58-0x to wire into the actual inspect-concurrency semaphore's 429 response.
- No blockers for the next wave.

## Self-Check: PASSED

All created files verified present on disk (internal/api/pre58_pinning_test.go, three testdata/pre58_*.golden.json files); both task commit hashes (ea3f2c8, db68dfa) verified present in git log.

---
*Phase: 58-synchronous-inspection*
*Completed: 2026-08-10*
