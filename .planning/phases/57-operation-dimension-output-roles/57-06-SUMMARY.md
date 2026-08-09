---
phase: 57-operation-dimension-output-roles
plan: 06
subsystem: api
tags: [go, http, role-driven-output, golden-pinning, output-roles]

# Dependency graph
requires:
  - phase: 57-operation-dimension-output-roles
    plan: 03
    provides: "jobs.Output.Role field and Repo.Outputs (ORDER BY ordinal)"
  - phase: 57-operation-dimension-output-roles
    plan: 05
    provides: "D-13: MarkDone refuses to commit 'done' unless job_outputs has exactly one role='primary' row -- the invariant handleGetJob relies on to skip a defensive 'no primary output' branch"
  - phase: 57-operation-dimension-output-roles
    plan: 04
    provides: "golden_pinning_test.go's closed three-exception list (exceptions #2 and #3 are this plan's)"
provides:
  - "handleGetJob selects download_url by Output.Role == 'primary', never by outs[0] position/ordinal -- closes the live ROLE-02 defect"
  - "Additive outputs[] key ({role, ordinal, format, size_bytes, url} per output), present only when len(outs) > 1, keeping a single-output convert job's response byte-identical to the pre-Phase-57 golden fixture (SC4)"
  - "fakeStorage.presignByKey + presignCalls/presignTTLs test seam for distinguishing multiple outputs' presigned URLs and proving D-12 (eager, same-TTL, done-only presigning)"
affects: [60]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Role-driven selection over positional/ordinal selection: the primary result is chosen by comparing Output.Role against a package constant (outputRolePrimary), never by indexing outs[0] or by ordinal value"
    - "Additive-key-guarded-on-cardinality: outputs[] follows the same idiom as the pre-existing resp[\"opts\"] guard (present only when there is more than one item), so a response body that predates a new dimension stays byte-identical when that dimension is at its degenerate (single-item) case"

key-files:
  created: []
  modified:
    - internal/api/handlers.go
    - internal/api/golden_pinning_test.go
    - internal/api/handlers_test.go

key-decisions:
  - "A pre-existing test (TestGetJob_DonePresigned, not part of golden_pinning_test.go's closed exception list) also had an Output literal predating jobs.Output.Role and broke the same way the golden fixture did. Fixed as a Rule 1 test-fixture bug (added Role: \"primary\") in the same commit as Task 1's production change, since it was Task 1's own change that exposed it and the fix has zero production-code impact -- documented here rather than silently folded in."
  - "TestGetJob_AllOutputsPresignedWithSameTTL builds its Server directly via NewServer with an explicit non-default Config.PresignTTL (7 minutes) rather than newTestServer's implicit zero-value TTL, so the assertion that every PresignGet call shares 'the server's configured TTL' is unambiguous and not a coincidental match against a zero Duration."

requirements-completed: [ROLE-02]

# Metrics
duration: ~35min
completed: 2026-08-09
---

# Phase 57 Plan 06: Role-Driven download_url in handleGetJob Summary

**`handleGetJob` now selects `download_url` by `Output.Role == "primary"` and returns every output under an additive `outputs[]` key, closing the live ROLE-02 defect where a done job's first-uploaded output (by ordinal, not role) was blindly presigned as "the result" -- proven by a reverse-order test (report at ordinal 0, primary at ordinal 1) plus a literal inversion run showing the same test fails when the code is reverted to `outs[0]`.**

## Performance

- **Duration:** ~35 min
- **Completed:** 2026-08-09T21:23Z
- **Tasks:** 2/2 completed
- **Files modified:** 3 (0 created, 3 modified)

## Accomplishments

- `handleGetJob`'s `case jobs.StatusDone:` branch now presigns every output in one pass, selects `download_url` by comparing `o.Role` against a new unexported constant `outputRolePrimary = "primary"`, and adds an additive `outputs` key (`{role, ordinal, format, size_bytes, url}` per output) guarded on `len(outs) > 1` -- exactly one indexed access to `outs` (`outs[0]`) existed before this plan; zero remain after it.
- Golden-pinning suite's exception #2 applied: `TestPre57GoldenGetJobSingleOutput`'s fixture gained `Role: "primary"` on its `jobs.Output` literal, and its inline `57-06` pointer comment was replaced with a statement that the exception has been applied. Golden file bytes are unchanged (`git rev-list --count` still prints `1`).
- A second, pre-existing (not part of the closed exception list) test fixture -- `TestGetJob_DonePresigned` -- needed the identical one-line fix for the identical reason (it predates `jobs.Output.Role`); fixed as a Rule 1 auto-fix, documented below.
- `fakeStorage` gained `presignByKey` (per-object-key URL override) and `presignCalls`/`presignTTLs` counters, purely additive -- every pre-existing test using the `presigned` field is unaffected.
- Four new tests added: `TestGetJob_DownloadURLResolvesToPrimaryNotOrdinalZero` (the D-18 core proof, reverse-ordinal-order), `TestGetJob_SingleOutputHasNoOutputsKey` (SC4), `TestGetJob_AllOutputsPresignedWithSameTTL` (D-12, same TTL), `TestGetJob_ActiveJobPresignsNothing` (D-12, zero presigns for non-terminal jobs).
- D-18 inversion run performed and reverted as a procedure (not a commit) -- transcript below, verbatim.

## Task Commits

1. **Task 1: Role-driven выбор primary и аддитивный ключ outputs[] в handleGetJob** - `ebf095e` (feat) — `internal/api/handlers.go`, `internal/api/golden_pinning_test.go`, `internal/api/handlers_test.go`
2. **Task 2: Доказательство обратным порядком и инверсионный прогон D-18** - `6f7cdad` (test) — `internal/api/handlers_test.go`

## Files Created/Modified

- `internal/api/handlers.go` — `outputRolePrimary` const added next to `operationConv`; `case jobs.StatusDone:` rewritten to loop over `outs` once, presign each into `items`, select `download_url` by role, and add `outputs` only when `len(outs) > 1`. Comments encode D-11 (one shape covers all future roles), D-12 (eager same-TTL presign, done-only), D-13 (why no defensive "no primary" branch is needed).
- `internal/api/golden_pinning_test.go` — `TestPre57GoldenGetJobSingleOutput`'s `jobs.Output` literal gains `Role: "primary"`; the `57-06` pointer comment above it replaced with a statement that exception #2 has been applied. Header comment (lines 1-62) untouched.
- `internal/api/handlers_test.go` — `fakeStorage.presignByKey`/`presignCalls`/`presignTTLs` fields and `PresignGet` rewritten to use them; `TestGetJob_DonePresigned`'s fixture gains `Role: "primary"` (Rule 1 fix, see Deviations); four new tests added after `TestGetJob_DonePresigned`.

## Decisions Made

- The primary-selection comparison is against a named constant (`outputRolePrimary`), not an inline string literal, per the plan's explicit instruction, so the string `"primary"` in this concept's context exists in exactly one declared place in `handlers.go`.
- `download_url` presence is gated on a `hasPrimary` boolean set during the presign loop, not on `downloadURL != ""`, avoiding any theoretical ambiguity if a presigned URL were ever the empty string.
- `TestGetJob_AllOutputsPresignedWithSameTTL` explicitly configures `Config.PresignTTL` to a distinguishing non-default value (7 minutes) rather than relying on `newTestServer`'s implicit zero-value TTL, so the "same TTL as the server's configured value" assertion is unambiguous.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Pre-existing `TestGetJob_DonePresigned` fixture also predated `jobs.Output.Role`**

- **Found during:** Task 1, running `go test ./internal/api/ -count=1` immediately after the `handleGetJob` production-code change
- **Issue:** `TestGetJob_DonePresigned` (an existing test, not part of `golden_pinning_test.go`'s closed three-exception list, since it lives in `handlers_test.go` and predates this plan by several waves) constructs a `jobs.Output` literal with no `Role` field set -- same root cause as the golden fixture the plan explicitly calls out: the field did not exist when this literal was written (plan 57-03, wave 2, added it later). Once `handleGetJob` started selecting `download_url` by `Role == "primary"`, this fixture had no primary output, `download_url` disappeared from the response, and the test's own assertion (`resp["download_url"] != "https://example/download"`) failed.
- **Fix:** Added `Role: "primary"` to the one-line `jobs.Output` literal, matching exactly the same fix and rationale as the golden fixture's exception #2, with an inline comment cross-referencing it.
- **Files modified:** `internal/api/handlers_test.go`
- **Verification:** `go test ./internal/api/ -run TestGetJob_DonePresigned -count=1 -v` → `PASS`; full package suite green afterward.
- **Committed in:** `ebf095e` (Task 1 commit, alongside the production-code change that exposed it)

---

**Total deviations:** 1 auto-fixed (1 pre-existing test-fixture bug, exposed by the new role-driven selection working as intended -- the exact same class of "not a regression" fixture staleness the plan's own warning anticipated for the golden-pinning suite, just in a second location the plan text did not name).

## Issues Encountered

None beyond the fixture staleness documented above.

## Acceptance-criteria wording discrepancy (documented, not gamed)

`grep -c 'Role: *"primary"' internal/api/golden_pinning_test.go` prints **2**, not the plan's expected 1. This is not a second literal edit: `internal/api/golden_pinning_test.go`'s header comment (lines 1-62, explicitly out of scope for this task and unmodified) already contains the prose sentence *"the TestPre57GoldenGetJobSingleOutput fixture gains Role: "primary" on its jobs.Output literal"* (written by plan 57-01 when it defined the closed exception list). That sentence's text happens to match the same grep pattern as the actual code literal this task added. Confirmed via `grep -n`:

```
$ grep -n 'Role: *"primary"' internal/api/golden_pinning_test.go
28://     gains Role: "primary" on its jobs.Output literal. jobs.Output.Role
179:		Role:        "primary",
```

Line 28 is pre-existing header prose (not touched by this plan); line 179 is the one and only code-literal edit this task made. The behavioral claim the criterion exists to prove -- that the fixture's `jobs.Output` literal gained the field -- is true and independently confirmed by the full-diff acceptance criterion below (`git diff` count `<=2`, actual `1`) and by `TestPre57GoldenGetJobSingleOutput` passing.

## User Setup Required

None -- no external service configuration required. `go build`/`go vet`/`go test`/`gofmt` were all run against the local toolchain; no live compose-stack service was started or required for the `internal/api` unit-test suite.

## Live compose-stack gate: deferred (documented, not run)

The plan's overall `<verification>` block includes a live curl+jq gate against a running compose stack (`api` container serving a real `png→webp` job through to `done`, then `curl ... | jq -e 'has("outputs") | not'` and `jq -r '.download_url'`). This session's shared Postgres/Redis/MinIO (`octoconv-db`, `octoconv-redis`, `octoconv-minio`) were already up as long-lived containers shared across worktrees, but no `api`/`worker` containers were running, and a sibling parallel executor (plan 57-07, `internal/mcpserver/` + `frontend/`) was active in a separate worktree at the same time. Standing up `cmd/api`/`cmd/worker` from this worktree's checkout against the shared live database carries the same cross-worktree-write risk documented in `57-04-SUMMARY.md`'s "Live-gate scope note" and `57-05-SUMMARY.md`'s equivalent deferral -- writing a real job row into the shared `jobs`/`job_outputs` tables could interfere with a concurrently-running sibling agent's own assumptions about database state. Following the same precedent, this exact live-HTTP path is deferred to post-merge integration testing, where a single agent owns the stack exclusively. All unit-level proof of the identical code path (`internal/api`'s own `Repo`/`Storage` interfaces, exercised through the real `handleGetJob` via `httptest`) is provided instead, per the Acceptance Criteria output below.

## D-18 Inversion Run — verbatim transcript

Per Task 2 step 3, `handleGetJob`'s primary-selection was temporarily reverted to `outs[0]` (a one-line change: `resp["download_url"] = items[0]["url"]` added immediately after the role-driven assignment, so it always wins), the new reverse-order test was run, the failure output captured verbatim, then the temporary line was removed and the test re-run to confirm it passed again. No commit was made for either state; only the final, un-inverted code is committed (`ebf095e`, `6f7cdad`).

**Step (b) — inverted code, test run, FAILS as required:**

```
$ go build ./... && go test ./internal/api/ -run 'TestGetJob_DownloadURLResolvesToPrimaryNotOrdinalZero' -count=1 -v
=== RUN   TestGetJob_DownloadURLResolvesToPrimaryNotOrdinalZero
2026/08/09 21:22:43 [.../Nj8BWL9Dkf-000001] "GET http://example.com/v1/jobs/eebd478e-e87f-430e-a6a4-46b59784bd79 HTTP/1.1" from 192.0.2.1 - 200 347B in 210.334µs
    handlers_test.go:2014: download_url = https://example/report, want the primary output's url (https://example/primary)
--- FAIL: TestGetJob_DownloadURLResolvesToPrimaryNotOrdinalZero (0.00s)
FAIL
FAIL	github.com/apaderin/octoconv/internal/api	0.738s
FAIL
```

The failure is exactly the defect class T-57-24 names: with the report at ordinal 0 and primary at ordinal 1, the `outs[0]`-based (inverted) code returned the *report's* URL (`https://example/report`) as `download_url` instead of the primary's (`https://example/primary`) -- proving the test genuinely detects "selection by position" bugs, not just "selection is present".

**Step (c) — inversion reverted, test run again, PASSES:**

```
$ go build ./... && go test ./internal/api/ -run 'TestGetJob_DownloadURLResolvesToPrimaryNotOrdinalZero' -count=1 -v
=== RUN   TestGetJob_DownloadURLResolvesToPrimaryNotOrdinalZero
2026/08/09 21:23:04 [.../BLnmzdZwuU-000001] "GET http://example.com/v1/jobs/c19aa5d0-6e1b-48ca-adbb-8cf8452bf169 HTTP/1.1" from 192.0.2.1 - 200 348B in 215.416µs
--- PASS: TestGetJob_DownloadURLResolvesToPrimaryNotOrdinalZero (0.00s)
PASS
ok  	github.com/apaderin/octoconv/internal/api	0.379s
```

Conclusion: the test fails when the code regresses to `outs[0]`-based selection and passes when the code is role-driven -- it catches the exact class of defect it was written to catch, not passing by coincidence.

## Acceptance Criteria — literal command output

Task 1:

```
$ grep -v '^\s*//' internal/api/handlers.go | grep -c 'outs\[0\]'
0
$ grep -c 'outputRolePrimary *= *"primary"' internal/api/handlers.go
1
$ grep -v '^\s*//' internal/api/handlers.go | grep -c 'resp\["outputs"\] ='
1
$ grep -v '^\s*//' internal/api/handlers.go | grep -c 'len(outs) > 1'
1
$ grep -v '^\s*//' internal/api/handlers.go | grep -c '"size_bytes"'
1
$ grep -v '^\s*//' internal/api/handlers.go | grep -c 'failed to presign output'
1
$ go test ./internal/api/ -run 'TestPre57Golden' -count=1
ok  	github.com/apaderin/octoconv/internal/api	0.426s
$ grep -c 'Role: *"primary"' internal/api/golden_pinning_test.go
2   # see "Acceptance-criteria wording discrepancy" above -- 1 pre-existing header prose (57-01), 1 actual code literal (this plan)
$ git diff -U0 HEAD~2 -- internal/api/golden_pinning_test.go | grep '^[+-]' | grep -v '^[+-][+-]' | grep -vc '^[+-]\s*//'
1
$ git rev-list --count HEAD -- internal/api/testdata/pre57_job_done_single_output.golden.json
1
$ git status --porcelain -- internal/api/testdata/
(empty)
```

Task 2:

```
$ grep -c 'presignByKey' internal/api/handlers_test.go
4
$ go test ./internal/api/ -run 'TestGetJob_DownloadURLResolvesToPrimaryNotOrdinalZero' -count=1 -v
=== RUN   TestGetJob_DownloadURLResolvesToPrimaryNotOrdinalZero
--- PASS: TestGetJob_DownloadURLResolvesToPrimaryNotOrdinalZero (0.00s)
PASS
ok  	github.com/apaderin/octoconv/internal/api	0.408s
$ go test ./internal/api/ -run 'TestGetJob_SingleOutputHasNoOutputsKey|TestGetJob_AllOutputsPresignedWithSameTTL|TestGetJob_ActiveJobPresignsNothing' -count=1 -v
--- PASS: TestGetJob_SingleOutputHasNoOutputsKey (0.00s)
--- PASS: TestGetJob_AllOutputsPresignedWithSameTTL (0.00s)
--- PASS: TestGetJob_ActiveJobPresignsNothing (0.00s)
PASS
ok  	github.com/apaderin/octoconv/internal/api	0.406s
$ go test ./internal/api/ -count=1
ok  	github.com/apaderin/octoconv/internal/api	0.564s
$ git rev-list --count HEAD -- internal/api/testdata/pre57_job_done_single_output.golden.json
1
$ git status --porcelain -- internal/api/testdata/
(empty)
$ gofmt -l internal/api
(empty)
```

Overall verification:

```
$ go build ./... && go vet ./...
(clean, exit 0)
$ gofmt -l .
(empty)
$ go test ./internal/api/ -count=1
ok  	github.com/apaderin/octoconv/internal/api	0.703s
$ git diff --diff-filter=D --name-only ebf095e~1 ebf095e   # Task 1
(empty)
$ git diff --diff-filter=D --name-only 6f7cdad~1 6f7cdad   # Task 2
(empty)
```

## Next Phase Readiness

- ROLE-02 is closed: `handleGetJob` returns a `download_url` derived from `Output.Role == "primary"` and never from output position, for any number of outputs.
- The `outputs[]` shape (`{role, ordinal, format, size_bytes, url}`) is ready to carry Phase 60's second output (`role='report'`) without any new response key or handler branch -- D-11's whole point.
- The golden-pinning suite's closed three-exception list is now fully applied and closed: exceptions #1 (57-04), #2 and #3 (this plan) are all done; no further edits to `golden_pinning_test.go`'s Go source are anticipated for Phase 57.
- No blockers identified for Phase 60.

---
*Phase: 57-operation-dimension-output-roles*
*Completed: 2026-08-09*

## Self-Check: PASSED

All three modified files confirmed present on disk (`internal/api/handlers.go`, `internal/api/golden_pinning_test.go`, `internal/api/handlers_test.go`); both task commit hashes (`ebf095e`, `6f7cdad`) confirmed present in `git log --oneline --all`.
