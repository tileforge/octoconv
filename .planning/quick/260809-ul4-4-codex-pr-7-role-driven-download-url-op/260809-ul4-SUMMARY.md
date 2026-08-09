---
phase: quick-260809-ul4
plan: 01
subsystem: api
tags: [webhook, mcp, openapi, errcode, redocly]

# Dependency graph
requires:
  - phase: 57-operation-dimension-output-roles
    provides: jobs.Output.Role, D-13 exactly-one-primary invariant, ROLE-02 fix for handleGetJob, the operations key of GET /v1/formats
provides:
  - Role-driven (not positional) download_url selection in the terminal webhook body
  - internal/jobs.OutputRolePrimary as the single Go declaration of the "primary" role literal
  - internal/mcpserver's FormatsResponse decoding the operations discovery key
  - docs/openapi.yaml with a required, enumerated, drift-guarded Error.code; operation/operations/outputs fields matching the shipped code
affects: [worker, mcpserver, openapi, errcode]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Shared role/vocabulary constants declared once on the owning type (jobs.OutputRolePrimary), aliased by consumers instead of re-declared"
    - "Mechanical spec<->Go set-equality sync tests (openapi_sync_test.go) mirroring the existing doc_sync_test.go idiom"

key-files:
  created:
    - internal/worker/webhook_role_test.go
    - internal/errcode/openapi_sync_test.go
  modified:
    - internal/jobs/jobs.go
    - internal/api/handlers.go
    - internal/worker/worker.go
    - internal/mcpserver/client.go
    - internal/mcpserver/client_test.go
    - docs/openapi.yaml

key-decisions:
  - "OutputRolePrimary declared once in internal/jobs (owns Output + the D-13 guard), not as a second unexported literal in internal/worker"
  - "setWebhookDownloadURL factored out as a pure, presign-closure-injected helper because HandleWebhookDeliver itself cannot be unit-tested (concrete storage.Client/webhook.Repo/webhook.Deliverer)"
  - "QuotaError schema deleted rather than kept alongside the now-required Error.code, per redocly's unused-component rule and to avoid two representations of the same error envelope"

patterns-established:
  - "Role selection by jobs.OutputRolePrimary, never by ordinal/slice position, is now enforced identically in both handleGetJob (57-06) and the terminal webhook (this plan)"

requirements-completed: [PR7-F1-WEBHOOK-ROLE, PR7-F2-MCP-OPERATIONS, PR7-F3-OPENAPI-ERRCODE, PR7-F4-OPENAPI-SHAPES]

# Metrics
duration: 7min
completed: 2026-08-09
---

# Quick Task 260809-ul4: Codex PR #7 review fixes Summary

**Role-driven (not positional) webhook download_url, a decodable MCP `operations` key, and an OpenAPI contract whose Error schema and request/response shapes now match the shipped Go code**

## Performance

- **Duration:** 7 min (first commit 22:15:36+03:00, last commit 22:22:23+03:00)
- **Tasks:** 4/4 completed
- **Files modified:** 6 modified, 2 created

## Accomplishments
- Closed the same defect class ROLE-02/57-06 fixed for `GET /v1/jobs/{id}` on the webhook delivery path: `HandleWebhookDeliver` now selects `download_url` by `jobs.OutputRolePrimary`, never by `outs[0]`
- Both MCP binaries (via the single shared `internal/mcpserver` package) now decode the `operations` discovery key that `tools.go:68`'s jsonschema already advertised
- `docs/openapi.yaml`'s `Error` schema now requires `code`, enumerated over the closed `internal/errcode` vocabulary, with a Go test (`openapi_sync_test.go`) that fails if the two ever drift
- `docs/openapi.yaml` now declares the sendable `operation` request field, the required `operations` response tree (via a shared `EngineFormats` component), and the `outputs` array — replacing the false "first output" `download_url` description

## Task Commits

Each task was committed atomically:

1. **Task 1: Select the terminal webhook's download_url by role, not by position** - `83708de` (fix)
2. **Task 2: Decode the operations key in the shared MCP client** - `67fdbad` (fix)
3. **Task 3: Make the OpenAPI error contract match the closed errcode vocabulary** - `d31117a` (docs)
4. **Task 4: Add operation, operations and outputs to the OpenAPI contract** - `4cdc43a` (docs)

_No TDD RED/GREEN split commits — each task's test additions and implementation landed together per this plan's `<action>` wording, and Task 1/Task 2's `<verify>` blocks require both the tests and the implementation before the commit is created._

## Files Created/Modified
- `internal/jobs/jobs.go` - Added exported `OutputRolePrimary = "primary"` constant above the `Output` type
- `internal/api/handlers.go` - `outputRolePrimary` now aliases `jobs.OutputRolePrimary` instead of re-declaring the literal
- `internal/worker/worker.go` - Added `setWebhookDownloadURL` pure helper below `buildTerminalWebhookBody`; `HandleWebhookDeliver`'s done-branch now calls it instead of presigning `outs[0]`
- `internal/worker/webhook_role_test.go` - New: 4 offline tests (reverse-order, single-primary, no-primary, presign-error)
- `internal/mcpserver/client.go` - `FormatsResponse` gained `Operations map[string]map[string]EngineFormats` (`omitempty`)
- `internal/mcpserver/client_test.go` - New `TestListFormats_OperationsDecoded`; existing `TestListFormats` now documents the operations-absent case and is a no-op assertion addition (`fr.Operations != nil` check)
- `docs/openapi.yaml` - Error schema rewritten (required `code`, multi-line enum), `QuotaError` deleted, 9 error examples gained `code`, new `EngineFormats` component, `FormatsResponse.operations` (required), `POST /v1/jobs`'s `operation` field, `JobStatus.outputs` array, `download_url` description fixed
- `internal/errcode/openapi_sync_test.go` - New: set-equality proof between `docs/openapi.yaml`'s `Error.code` enum and `errcode.All()`

## Decisions Made
- `OutputRolePrimary` lives in `internal/jobs`, not a parallel literal in `internal/worker` — the package that owns the `Output` type and the D-13 exactly-one-primary guard is the correct owner of the vocabulary (matches the plan's stated rationale; this is exactly how the api/worker divergence this plan fixes came about in the first place)
- `setWebhookDownloadURL` takes an injected `presign func(context.Context, string) (string, error))` closure rather than a `*storage.Client`, making it unit-testable without a live S3/BucketExists round trip
- `openapi_sync_test.go` reuses `doc_sync_test.go`'s `diffSets` helper (same `errcode_test` package) rather than duplicating it — `go vet` caught the initial duplicate declaration and it was removed before the first test run

## Deviations from Plan

None - plan executed exactly as written. All four tasks matched their `<action>` and `<verify>` blocks; the plan's own deliberate self-check (Task 3's "deliberately break one enum entry once to confirm it FAILS, then restore") was performed and reverted before the commit.

## Issues Encountered

- `go vet ./internal/errcode` initially failed with `diffSets redeclared in this block` because `openapi_sync_test.go` (external test package `errcode_test`) duplicated a helper already declared in the sibling `doc_sync_test.go` in the same package. Removed the duplicate function from the new file and reused the existing one — no functional change, resolved before any commit.

## User Setup Required

None - no external service configuration required.

## Verification Evidence (literal command output)

```
$ gofmt -l ./cmd ./internal
(empty)

$ go build ./... && go vet ./...
(clean, no output)

$ go test ./internal/worker/ -run 'TestSetWebhookDownloadURL' -count=1 -v
=== RUN   TestSetWebhookDownloadURL_ReverseOrderSelectsPrimary
--- PASS: TestSetWebhookDownloadURL_ReverseOrderSelectsPrimary (0.00s)
=== RUN   TestSetWebhookDownloadURL_SinglePrimaryUnchanged
--- PASS: TestSetWebhookDownloadURL_SinglePrimaryUnchanged (0.00s)
=== RUN   TestSetWebhookDownloadURL_NoPrimaryOmitsKey
--- PASS: TestSetWebhookDownloadURL_NoPrimaryOmitsKey (0.00s)
=== RUN   TestSetWebhookDownloadURL_PresignErrorPropagates
--- PASS: TestSetWebhookDownloadURL_PresignErrorPropagates (0.00s)
PASS
ok  	github.com/apaderin/octoconv/internal/worker	1.058s

$ go test ./internal/api/ -run 'TestPre57' -count=1
PASS (TestPre57GoldenListFormats, TestPre57GoldenGetJobSingleOutput both pass)
ok  	github.com/apaderin/octoconv/internal/api	0.749s

$ grep -v '^\s*//' internal/worker/worker.go | grep -c 'outs\[0\]'
0

$ grep -c 'jobs.OutputRolePrimary' internal/worker/worker.go
2

$ go test ./internal/mcpserver/ -run 'TestListFormats' -count=1 -v
=== RUN   TestListFormats
--- PASS: TestListFormats (0.00s)
=== RUN   TestListFormats_OperationsDecoded
--- PASS: TestListFormats_OperationsDecoded (0.00s)
PASS
ok  	github.com/apaderin/octoconv/internal/mcpserver	1.370s

$ grep -c 'json:"operations' internal/mcpserver/client.go
1

$ go test ./internal/errcode/ -count=1 -v
=== RUN   TestAllCodesUniqueAndNonEmpty
--- PASS
=== RUN   TestAllCodesMatchMachineSafePattern
--- PASS
=== RUN   TestWireValuesFrozen
--- PASS
=== RUN   TestIsKnown
--- PASS
=== RUN   TestAllReturnsCopy
--- PASS
=== RUN   TestErrorCodesDocMatchesGoConstants
--- PASS
=== RUN   TestSyncAndAsyncShareInvalidOptions
--- PASS
=== RUN   TestDocDeclaresUnknownCodeFallback
--- PASS
=== RUN   TestOpenAPIErrorCodeEnumMatchesGoConstants
--- PASS
PASS
ok  	github.com/apaderin/octoconv/internal/errcode	0.931s

$ (deliberately corrupted "internal_error" -> "internal_error_typo" in the enum, re-ran, then restored)
=== RUN   TestOpenAPIErrorCodeEnumMatchesGoConstants
    openapi_sync_test.go:116: drift between errcode.All() and docs/openapi.yaml's Error.code enum -- missing from the spec: [internal_error]; present in the spec but not declared by errcode.All(): [internal_error_typo]. Fix whichever side is wrong (Go constants are the source of truth, D-06) -- never relax this test.
--- FAIL: TestOpenAPIErrorCodeEnumMatchesGoConstants (0.00s)
FAIL
(restored; re-ran clean: ok  	github.com/apaderin/octoconv/internal/errcode	0.473s)

$ grep -c 'QuotaError' docs/openapi.yaml
0

$ grep -c '^                code: ' docs/openapi.yaml
9

$ npx --yes @redocly/cli@latest lint docs/openapi.yaml
[after Task 3] Woohoo! Your API description is valid. 🎉 You have 2 warnings. (info-license, healthz-4xx — both pre-existing/accepted per quick task 260719-2f5)
[after Task 4] Woohoo! Your API description is valid. 🎉 You have 2 warnings. (same two)

$ grep -c 'EngineFormats' docs/openapi.yaml
4
$ grep -c 'operations:' docs/openapi.yaml
2
$ grep -c 'outputs:' docs/openapi.yaml
1
$ grep -c 'first output' docs/openapi.yaml
0
$ git diff --quiet HEAD -- internal/ ; echo $?
0

$ gofmt -l ./cmd ./internal
(empty)
$ go build ./... && go vet ./...
(clean)
$ go test ./internal/... -count=1
ok for all 19 packages (api, auth, cancel, clients, convert, db, e2e, envcfg,
errcode, jobs, mcpserver, metrics, presets, queue, ratelimit, reconciler,
storage, webhook, worker)

$ go test ./internal/metrics/ ./internal/worker/ -run 'LabelsPinned|TestPinned' -count=1 -v
TestJobDurationLabelsPinned --- PASS
TestQueueDepthLabelsPinned --- PASS
(plus all TestPinned* worker suites) --- PASS, ok for both packages

$ git rev-list --count HEAD -- internal/api/testdata/pre57_formats.golden.json
1
$ git rev-list --count HEAD -- internal/api/testdata/pre57_job_done_single_output.golden.json
1
$ git diff --stat HEAD~4 -- internal/worker/pinning_test.go
(empty)
$ git diff --stat HEAD~4 -- cmd/
(empty)
```

## Next Phase Readiness

- All four codex PR #7 findings closed; no follow-on work identified by this task.
- The two redocly warnings (info-license, healthz missing a 4xx) are pre-existing and accepted per quick task 260719-2f5 — not new to this task, not blocking.
- No architectural changes, no new dependencies, no metric label changes — safe to merge alongside the rest of PR #7.

## Self-Check: PASSED

All 8 files created/modified by this plan found on disk (internal/jobs/jobs.go,
internal/api/handlers.go, internal/worker/worker.go,
internal/worker/webhook_role_test.go, internal/mcpserver/client.go,
internal/mcpserver/client_test.go, docs/openapi.yaml,
internal/errcode/openapi_sync_test.go). All 4 task commit hashes
(83708de, 67fdbad, d31117a, 4cdc43a) found in `git log --oneline --all`.

---
*Quick task: 260809-ul4*
*Completed: 2026-08-09*
