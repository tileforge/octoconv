---
phase: 57-operation-dimension-output-roles
plan: 01
subsystem: api
tags: [go, testing, golden-pinning, errcode, chi]

# Dependency graph
requires:
  - phase: 56-jobrunner-consolidation-structured-error-codes
    provides: "internal/errcode vocabulary + docs/ERROR-CODES.md Go↔doc parity test, D-10 golden-pinning protocol precedent"
provides:
  - "internal/api/golden_pinning_test.go — D-17 evidentiary base: byte-pins pre-Phase-57 GET /v1/formats and GET /v1/jobs/{id} (single-output done job)"
  - "internal/api/testdata/pre57_formats.golden.json and pre57_job_done_single_output.golden.json — the two frozen fixtures"
  - "errcode.UnsupportedOperation (\"unsupported_operation\") — new sync error code, 15th in the closed vocabulary"
  - "docs/ERROR-CODES.md sync table row for unsupported_operation, kept in parity by TestErrorCodesDocMatchesGoConstants"
affects: [57-04, 57-06]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "D-17 golden-pinning: byte snapshot committed BEFORE any production code changes, compared via os.ReadFile + bytes.Equal against testdata, never against an inline literal"
    - "Closed exception list for a pinning suite lives in ONE header comment (this file), not scattered across future plans' PLAN.md files"

key-files:
  created:
    - internal/api/golden_pinning_test.go
    - internal/api/testdata/pre57_formats.golden.json
    - internal/api/testdata/pre57_job_done_single_output.golden.json
  modified:
    - internal/errcode/errcode.go
    - docs/ERROR-CODES.md

key-decisions:
  - "assertGolden's read path uses a literal os.ReadFile(\"testdata/pre57_\" + name) call (not a fully-generic path variable) so the acceptance-criteria grep for a literal-file comparison (not an inline expectation) is mechanically true, not just true in spirit"
  - "unsupported_operation docs row avoids naming the not-yet-existing /v1/formats \"operations\" key (that lands in 57-04); it just says \"check against GET /v1/formats\" so the doc doesn't forward-reference a field this plan didn't add"

patterns-established:
  - "Golden-pinning suites must physically read testdata/ at compare time (grep-verifiable literal os.ReadFile(\"testdata/pre57_...\" prefix), never hold the expectation as an in-test literal"

requirements-completed: [OPRN-01, OPRN-03, ROLE-02]

# Metrics
duration: ~35min
completed: 2026-08-09
---

# Phase 57 Plan 01: Golden Pinning & unsupported_operation Error Code Summary

**Byte-pinned GET /v1/formats and GET /v1/jobs/{id} (single-output done job) against pre-Phase-57 code in the phase's first commit, plus a new `unsupported_operation` sync error code (15th in the closed vocabulary) kept in Go↔doc parity by the existing `TestErrorCodesDocMatchesGoConstants`.**

## Performance

- **Duration:** ~35 min
- **Completed:** 2026-08-09T17:18:09Z
- **Tasks:** 2/2 completed
- **Files modified:** 5 (3 created, 2 modified)

## Accomplishments

- Created `internal/api/golden_pinning_test.go`, an internal-package (`package api`) pinning suite with a header comment that states the D-17 protocol, the CLOSED list of exactly three permitted future exceptions (57-04 Task 2, 57-06 Task 1, 57-06 Task 2), the standing prohibition on ever changing the golden bytes, and the `git rev-list --count HEAD -- <file>` immutability check with rationale for why a ref-less `git diff` would be blind to the exact scenario D-17 guards against.
- Generated both golden fixtures from TODAY's (pre-Phase-57) handler output via `-update-golden`, then re-ran without the flag to confirm the byte comparison passes.
- Committed the golden-pinning suite as the phase's FIRST commit, containing exactly three files (test file + two `.golden.json` fixtures) — verified via `git show --name-only --pretty=format: HEAD`.
- Added `errcode.UnsupportedOperation = "unsupported_operation"` right after `UnsupportedConversion` in both the const block and the `all` slice (preserving declaration-order invariant), bumped the doc comment to "closed at fifteen codes".
- Added the corresponding row to `docs/ERROR-CODES.md`'s synchronous table and bumped its "Fifteen codes" header line, leaving the untouched async "Fourteen codes" line alone (D-04).

## Task Commits

1. **Task 1: Golden-snapshot GET /v1/formats and GET /v1/jobs/{id}** - `0346eee` (test) — exactly 3 files: `internal/api/golden_pinning_test.go`, `internal/api/testdata/pre57_formats.golden.json`, `internal/api/testdata/pre57_job_done_single_output.golden.json`
2. **Task 2: Add unsupported_operation sync error code** - `be1b9c2` (feat) — `internal/errcode/errcode.go`, `docs/ERROR-CODES.md`

## Files Created/Modified

- `internal/api/golden_pinning_test.go` - D-17 pinning suite: `updateGolden` flag, `assertGolden` helper (reads/writes `testdata/pre57_<name>`), `TestPre57GoldenListFormats`, `TestPre57GoldenGetJobSingleOutput`
- `internal/api/testdata/pre57_formats.golden.json` - byte snapshot of `GET /v1/formats` pre-Phase-57 (single top-level key `engines`)
- `internal/api/testdata/pre57_job_done_single_output.golden.json` - byte snapshot of `GET /v1/jobs/{id}` for a done job with one output (keys: `download_url,eta_seconds,job_id,progress_pct,status`; no `outputs` key; presigned URL's unescaped `&` proves `SetEscapeHTML(false)`)
- `internal/errcode/errcode.go` - new `UnsupportedOperation` const + `all` entry, doc comment bumped to "fifteen codes"
- `docs/ERROR-CODES.md` - new `unsupported_operation` table row, header bumped to "Fifteen codes"

## Decisions Made

- **assertGolden's literal-path read.** Built the helper's read call as `os.ReadFile("testdata/pre57_" + name)` (literal prefix, not a pre-built path variable passed to `os.ReadFile`) so the plan's acceptance-criteria grep (`os.ReadFile("testdata/pre57_`) mechanically confirms the comparison hits a real file, not an inline literal — the grep is a proxy for "this test cannot be gamed by editing an in-file expectation," and the implementation satisfies that proxy literally, not just in spirit.
- **docs row wording avoids forward-referencing the `operations` key.** `GET /v1/formats` does not gain the `operations` tree until plan 57-04 (D-10); the new error-code doc row therefore tells the client to check "GET /v1/formats" generically rather than naming a field this plan did not add, avoiding a doc claim that would be false until 57-04 lands.

## Deviations from Plan

None - plan executed exactly as written. Both tasks matched their `<action>` specs; all listed `<acceptance_criteria>` commands were run literally with the outputs below.

## Issues Encountered

None.

## Acceptance Criteria — literal command output

Task 1:

```
$ jq -r 'keys | join(",")' internal/api/testdata/pre57_formats.golden.json
engines

$ jq -r 'keys | join(",")' internal/api/testdata/pre57_job_done_single_output.golden.json
download_url,eta_seconds,job_id,progress_pct,status

$ jq -e 'has("outputs") | not' internal/api/testdata/pre57_job_done_single_output.golden.json
true   # exit 0

$ grep -c 'sig=abc&x=1' internal/api/testdata/pre57_job_done_single_output.golden.json
1

$ go test ./internal/api/ -run 'TestPre57Golden' -count=1
ok  	github.com/apaderin/octoconv/internal/api	1.332s

$ grep -v '^//' internal/api/golden_pinning_test.go | grep -c 'os.ReadFile("testdata/pre57_'
1

$ for m in D-17 57-CONTEXT.md 57-04 57-06 D-18 'git rev-list --count'; do grep -qF "$m" internal/api/golden_pinning_test.go || echo "MISSING $m"; done
(empty output — all markers present)

$ grep -c '57-06' internal/api/golden_pinning_test.go
6

$ git rev-list --count HEAD -- internal/api/testdata/pre57_formats.golden.json
1
$ git rev-list --count HEAD -- internal/api/testdata/pre57_job_done_single_output.golden.json
1

$ git show --name-only --pretty=format: HEAD   # (HEAD at the time = 0346eee)
internal/api/golden_pinning_test.go
internal/api/testdata/pre57_formats.golden.json
internal/api/testdata/pre57_job_done_single_output.golden.json
```

Task 2:

```
$ grep -c 'UnsupportedOperation = "unsupported_operation"' internal/errcode/errcode.go
1
$ grep -v '^//' internal/errcode/errcode.go | grep -c 'UnsupportedOperation,'
1
$ grep -c 'closed at fifteen codes' internal/errcode/errcode.go
1
$ grep -c 'closed at fourteen codes' internal/errcode/errcode.go
0
$ grep -c '^| `unsupported_operation` |' docs/ERROR-CODES.md
1
$ grep -c '^Fourteen codes' docs/ERROR-CODES.md
1
$ go test ./internal/errcode/... -count=1
ok  	github.com/apaderin/octoconv/internal/errcode	1.067s
$ go vet ./internal/errcode/...
(clean)
$ git rev-list --count HEAD -- internal/api/testdata/pre57_formats.golden.json
1
```

Overall verification:

```
$ go test ./internal/api/ -run 'TestPre57Golden' -count=1   -> ok
$ go test ./internal/errcode/... -count=1                   -> ok
$ go vet ./...                                               -> clean
$ gofmt -l internal/api internal/errcode                     -> (empty)
$ go build ./...                                              -> clean
$ go test ./internal/api/... -count=1                         -> ok
$ git diff --diff-filter=D --name-only HEAD~1 HEAD (post Task 2 commit) -> (empty, no deletions)
```

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- The D-17 evidentiary base is in place: both golden fixtures exist, are pinned by a green test, and are proven immutable at exactly one commit each.
- Plans 57-04 and 57-06 have their exact, permitted edit points to this suite already documented in its header comment — no cross-plan hunting required.
- `errcode.All()` now returns fifteen codes including `unsupported_operation`; downstream plans that reject an unknown `operation` value (57-02/57-03 per 57-CONTEXT.md D-03) can use this constant directly.
- No blockers for wave 2 (plans 57-02/57-03).

---
*Phase: 57-operation-dimension-output-roles*
*Completed: 2026-08-09*

## Self-Check: PASSED

All created/modified files confirmed present on disk (`ls -la`); both task commit hashes (`0346eee`, `be1b9c2`) confirmed present in `git log --oneline --all`.
