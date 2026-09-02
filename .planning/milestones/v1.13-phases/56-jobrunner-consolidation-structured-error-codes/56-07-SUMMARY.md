---
phase: 56-jobrunner-consolidation-structured-error-codes
plan: 07
subsystem: api
tags: [error-codes, frontend, mcp, typescript, go, testing]

# Dependency graph
requires:
  - phase: 56-06
    provides: "docs/ERROR-CODES.md: the frozen, versioned error-code contract (both halves) for the frontend and both MCP servers"
provides:
  - "frontend/src/api.ts's ApiError carrying a machine-readable code, and App.tsx's describeError selecting behavior from it"
  - "internal/mcpserver's typed *APIError (Code/Status/Message/RetryAfter) and classifyAPIError, replacing plain fmt.Errorf error values"
  - "A mechanical, demonstrated-effective proof (Go + TypeScript) that no consumer branches on error message text"
affects: []

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Code-first, status-second describeError: switch on error.code with a default fallback to the pre-existing status-based switch, never a closed TypeScript union (docs/ERROR-CODES.md's anti-closed-enum rule)"
    - "Typed Go error (*APIError) with Error() reproducing byte-identical legacy text, so a structural migration can land without touching pinned text-assertion tests"
    - "classifyAPIError: single errors.As choke point in tools.go where every Client-facing handler routes its error before returning, so code-driven behavior (retry-now vs do-not-retry) is decided in exactly one place"
    - "Curated code-literal subset (codeRateLimited/codeQuotaExceeded/codeInvalidOptions) hardcoded in internal/mcpserver rather than importing internal/errcode, preserving the package's pre-existing D-02 zero-internal-import invariant"
    - "Mechanical no-text-matching proof duplicated in Go (scans .go + frontend .ts/.tsx as plain text) and TypeScript (vitest), both demonstrated live against a deliberately inserted violation before being trusted"

key-files:
  created:
    - frontend/src/errorcodes.test.ts
    - frontend/src/no-text-matching.test.ts
    - internal/mcpserver/errcode_consumer_test.go
    - internal/mcpserver/no_text_matching_test.go
  modified:
    - frontend/src/api.ts
    - frontend/src/App.tsx
    - internal/mcpserver/client.go
    - internal/mcpserver/tools.go

key-decisions:
  - "internal/errcode was deliberately NOT imported into internal/mcpserver. The plan's action text left this as a go-list-deps check, but the package already carries its own, stronger, pre-existing D-02 constraint documented directly in the code (client.go:28, config.go:4): internal/mcpserver imports NO other internal/* package, full stop, because it speaks only the API's public wire contract. go list -deps confirms the import would have been acyclic, but honoring the existing architectural invariant took precedence over deduplicating four string literals. A curated subset of code constants (codeRateLimited, codeQuotaExceeded, codeInvalidOptions) is hardcoded instead, mirroring the pre-existing statusDone/statusFailed pattern in the same file."
  - "APIError.RetryAfter uses -1 as the 'not present or not usable' sentinel, distinct from a legitimate 'retry in 0 seconds' Retry-After value, which parseRetryAfterSeconds already treats as valid (n >= 0)."
  - "classifyAPIError is applied at every tools.go handler that can surface a Client error (convert_file, get_job_status, download_result, list_supported_formats, list_presets), not just convert_file, since both rate_limited and quota_exceeded can occur on any authenticated request, not only job creation."
  - "The async failure path's code-driven behavior lives in client.go's new describeFailedJob (called from ConvertBlocking), not in tools.go, because JobStatus for a failed job flows to get_job_status as plain output data (never an error) -- ConvertBlocking is the one place a failed async job becomes a Go error a handler returns."

requirements-completed: [ERRC-03]

# Metrics
duration: ~55min
completed: 2026-08-08
---

# Phase 56 Plan 07: Frontend + MCP consumers select behavior from the error code Summary

**Frontend's `ApiError` now carries a machine-readable `code` (open string, never a closed union) that `describeError` switches on first; both MCP servers' `apiError` now returns a typed `*APIError{Code,Status,Message,RetryAfter}` recovered via `errors.As` and branched on structurally in a single `classifyAPIError` choke point — and a mechanical Go+TypeScript scan, demonstrated live against a deliberately inserted violation, proves no consumer branches on error message text instead.**

## Performance

- **Duration:** ~55 min
- **Started:** 2026-08-08 (worktree reset to base a6bb65f7, no drift encountered)
- **Completed:** 2026-08-08T04:25:30Z
- **Tasks:** 3 completed
- **Files modified:** 8 (4 created, 4 modified)

## Accomplishments

- `frontend/src/api.ts`: `ApiError` gained `readonly code?: string`, extracted defensively in `parseResponse` alongside the existing `error` text; `frontend/src/App.tsx`'s `describeError` (now exported) switches on `error.code` first for ten codes (`payload_too_large`, `unauthorized`, `rate_limited`, `quota_exceeded`, `unsupported_conversion`, `invalid_options`, `opts_not_applicable`, `unknown_preset`, `missing_file`, `not_found`), falling through unchanged to the pre-existing status-based switch for any unrecognized or absent code — no closed TypeScript union was introduced (`grep -c 'type ErrorCode'` returns 0), per `docs/ERROR-CODES.md`'s explicit anti-closed-enum rule.
- `frontend/src/errorcodes.test.ts` (new, 6 tests): proves code-driven selection, that the two 429 classes (`rate_limited` vs `quota_exceeded`) now produce distinguishable messages, an unknown-code fixture (`totally_new_code_v99`) degrades safely without throwing, a code-less body still produces the pre-existing status-derived message, and `ApiError.code` flows end-to-end through the real `parseResponse`/`getJob` pipeline (not a copy).
- `internal/mcpserver/client.go`: `apiError` now returns `*APIError{Action,Status,Code,Message,RetryAfter}` instead of a plain `fmt.Errorf` value. `Error()` reproduces byte-for-byte the three message formats this package has always emitted (message-only, message+code, message+code+retry_after) — verified, not assumed, against the pre-existing `client_test.go`/`quota_admission_test.go` text assertions, which remain unmodified (`git diff --stat` on both files is empty). `RetryAfter` uses `-1` as an explicit "not present" sentinel, distinct from a legitimate zero-second wait hint.
- `describeFailedJob` (new, `client.go`) routes `ConvertBlocking`'s async failure path through `job.ErrorCode` structurally — a `default` branch covers every unrecognized code unchanged, with one explicit case for the shared `invalid_options` code space (D-03).
- `internal/mcpserver/tools.go`: `classifyAPIError` recovers `*APIError` via `errors.As` and distinguishes `rate_limited` (retry-now) from `quota_exceeded` (do-not-retry) — the exact ambiguity `client.go:441-443`'s own comment names as the reason the code was surfaced in the first place — applied at every handler that can surface a `Client` error (`convert_file`, `get_job_status`, `download_result`, `list_supported_formats`, `list_presets`). Any other code, or no `*APIError` at all, passes through completely unchanged.
- `internal/mcpserver` still imports zero other `internal/*` packages (`go list -deps ./internal/mcpserver | grep apaderin/octoconv/internal` returns only itself) — the pre-existing D-02 architectural invariant is intact; see Decisions.
- `internal/mcpserver/errcode_consumer_test.go` (new, 3 tests): `TestUnknownErrorCodeFallsBackGenerically` (sync, 422) and `TestUnknownAsyncErrorCodeFallsBackGenerically` (async, `GET /v1/jobs/{id}`) both drive the real MCP tool-call path end-to-end and assert no panic, a normal `IsError` result carrying the server's message, and no API-key leak; `TestBothMCPEntryPointsShareTheClient` reads `cmd/mcp-server/main.go` and `cmd/mcp-http/main.go` and asserts both call `mcpserver.NewServer(`, proving the migration provably covers both binaries.
- `internal/mcpserver/no_text_matching_test.go` + `frontend/src/no-text-matching.test.ts` (new): a duplicated Go+TypeScript mechanical scan for message-text branching (`strings.Contains`/`HasPrefix`/`EqualFold` on `err.Error()`/`.Message`/`.ErrorMessage`, and `.message.includes/startsWith/indexOf/match` or equality against `error.message`), anchored specifically to those expressions (not a blanket ban, since `strings.ContainsAny` is used legitimately elsewhere in the package for filename sanitization). Comment lines are skipped; test files are excluded. Demonstrated live against two deliberately inserted violations (see Manual Verification Record below), then both files were restored to their exact committed state.
- `go build ./... && go vet ./... && go test ./... -count=1` green (all 20 tested packages). `cd frontend && npm run build && npx vitest run` green (4 test files, 30 tests).

## Task Commits

1. **Task 1: Make the frontend select behavior from the code**
   - `d1700ee` (feat) - `frontend/src/api.ts`, `frontend/src/App.tsx`, `frontend/src/errorcodes.test.ts`

2. **Task 2: Make both MCP servers carry and branch on the code structurally**
   - `0e421c5` (feat) - `internal/mcpserver/client.go`, `internal/mcpserver/tools.go`, `internal/mcpserver/errcode_consumer_test.go`

3. **Task 3: Prove no consumer branches on error message text**
   - `1c615c5` (test) - `internal/mcpserver/no_text_matching_test.go`, `frontend/src/no-text-matching.test.ts`

**Plan metadata:** this commit (docs: complete plan) — created after this SUMMARY.

## Files Created/Modified

- `frontend/src/api.ts` - `ApiError` gained an optional `code` field; `parseResponse` extracts `body.code` defensively alongside the existing `body.error` extraction
- `frontend/src/App.tsx` - `describeError` (now exported) switches on `error.code` first, falling through to the pre-existing status-based switch
- `frontend/src/errorcodes.test.ts` (new) - code-driven behavior, 429-class distinguishability, unknown-code fallback, code-less-body regression, end-to-end `parseResponse` proof
- `frontend/src/no-text-matching.test.ts` (new) - TypeScript-side mechanical no-message-text-branching scan
- `internal/mcpserver/client.go` - new `*APIError` type + `Error()`, rewritten `apiError` constructor, new `describeFailedJob`, new curated code-literal constants
- `internal/mcpserver/tools.go` - new `classifyAPIError` helper, applied at five handler error-return sites; updated package-level doc comment stating the ERRC-03 consumer contract
- `internal/mcpserver/errcode_consumer_test.go` (new) - sync/async unknown-code fixtures plus the both-entry-points-share-the-client proof
- `internal/mcpserver/no_text_matching_test.go` (new) - Go-side mechanical no-message-text-branching scan (covers both `internal/mcpserver/*.go` and the two frontend files as plain text)

## Decisions Made

See `key-decisions` in frontmatter. The most consequential: **`internal/errcode` was not imported into `internal/mcpserver`**, even though `go list -deps` confirmed the import would have been acyclic — the package's own pre-existing, code-documented D-02 invariant (zero `internal/*` imports, wire-contract-only) is a stronger and older constraint than the plan's "check first" guidance implied should be reconsidered. A curated subset of code-literal constants was hardcoded instead, extending the exact pattern (`statusDone`/`statusFailed`) the file already used for the same reason.

## Deviations from Plan

None — plan executed exactly as written. All three tasks' acceptance criteria were met without needing any Rule 1-4 deviation. The "check `go list -deps`, state which option was taken and why" instruction in Task 2 was followed literally (see Decisions) and is not itself a deviation.

## Manual Verification Record (Task 3 acceptance criteria)

Per the plan's instruction to prove both scans actually catch a real violation, not merely exist:

**1. Go-side violation** — temporarily inserted `if strings.Contains(err.Error(), "quota") {` into `classifyAPIError` (`internal/mcpserver/tools.go`, plus the `"strings"` import needed for it to compile) and reran the Go scan:

```
=== RUN   TestNoConsumerBranchesOnErrorMessageText
    no_text_matching_test.go:68: found 1 consumer branch(es) on error message text -- branch on the machine-readable code instead:
        .../internal/mcpserver/tools.go:51: if strings.Contains(err.Error(), "quota") {
--- FAIL: TestNoConsumerBranchesOnErrorMessageText (0.00s)
```

`tools.go` was restored to its exact committed state afterward (`git diff --stat internal/mcpserver/tools.go` empty; `go build ./...` and the full `internal/mcpserver` suite green again).

**2. TypeScript-side violation** — temporarily inserted `if (error.message.includes('quota')) { /* ... */ }` into `describeError` (`frontend/src/App.tsx`) and reran BOTH scans:

```
FAIL  src/no-text-matching.test.ts > no consumer branches on error message text (ERRC-03) > api.ts and App.tsx contain no message-text branch
AssertionError: found message-text branch(es):
App.tsx:27: if (error.message.includes('quota')) {
```

```
=== RUN   TestNoConsumerBranchesOnErrorMessageText
    no_text_matching_test.go:68: found 1 consumer branch(es) on error message text -- branch on the machine-readable code instead:
        .../frontend/src/App.tsx:27: if (error.message.includes('quota')) {
--- FAIL: TestNoConsumerBranchesOnErrorMessageText (0.00s)
```

Confirming the Go scan (which also treats the two frontend files as plain text) and the vitest scan both independently catch the same violation, each naming the exact file and line. `App.tsx` was restored to its exact committed state afterward (`git diff --stat frontend/src/App.tsx` empty; `npm run build`, full `npx vitest run`, and the full Go suite all green again).

## Issues Encountered

None beyond the expected setup step: `frontend/node_modules` was absent at execution start (this worktree had never run `npm install`); ran `npm ci` against the existing, unmodified `package-lock.json` before the first `npm run build`/`vitest run` — no new dependency was added, satisfying the plan's `<threat_model>` T-56-SC constraint.

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- Both consumer halves of the error-code contract frozen in 56-06 are now demonstrably consumed and mechanically proven honest — closes ERRC-03 and ROADMAP Phase 56's SC5 consumer half.
- `internal/mcpserver` remains a zero-`internal/*`-import package; any future phase adding a new sync/async code only needs to (a) add it to `docs/ERROR-CODES.md` and the emitting Go constant (56-02/56-04's territory) and (b) optionally add a `classifyAPIError`/`describeFailedJob` case here if a code-driven behavior is warranted — the unknown-code fallback means skipping step (b) is always safe.
- No blockers for the remainder of Phase 56 or subsequent phases (57+).

---
*Phase: 56-jobrunner-consolidation-structured-error-codes*
*Completed: 2026-08-08*

## Self-Check: PASSED

- FOUND: frontend/src/api.ts
- FOUND: frontend/src/App.tsx
- FOUND: frontend/src/errorcodes.test.ts
- FOUND: frontend/src/no-text-matching.test.ts
- FOUND: internal/mcpserver/client.go
- FOUND: internal/mcpserver/tools.go
- FOUND: internal/mcpserver/errcode_consumer_test.go
- FOUND: internal/mcpserver/no_text_matching_test.go
- FOUND: .planning/phases/56-jobrunner-consolidation-structured-error-codes/56-07-SUMMARY.md
- FOUND commit: d1700ee (feat(56-07): frontend selects behavior from ApiError.code)
- FOUND commit: 0e421c5 (feat(56-07): both MCP servers carry and branch on the error code structurally)
- FOUND commit: 1c615c5 (test(56-07): mechanically prove no consumer branches on error message text)
