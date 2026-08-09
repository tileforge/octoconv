---
phase: 57-operation-dimension-output-roles
plan: 07
subsystem: mcp,frontend
tags: [go, typescript, mcp, operation-dimension, additive-api-contract, backward-compat]

# Dependency graph
requires:
  - phase: 57-operation-dimension-output-roles
    provides: "57-04's operation dimension in the public API: POST /v1/jobs accepts and validates the operation multipart field (fail-closed 422 unsupported_operation), and GET /v1/formats gains the additive {operation -> engine -> pairs} operations tree"
provides:
  - "internal/mcpserver (serving both cmd/mcp-server and cmd/mcp-http) accepts and forwards an optional operation string through CreateJob/ConvertBlocking/ConvertFileInput, never arbitrating its value locally"
  - "frontend/src/api.ts createJob accepts an optional operation, guarded exactly like the pre-existing opts field -- absent/empty appends nothing to FormData"
  - "frontend/src/api.ts types FormatsResponse.operations and JobStatusResponse.outputs as optional, additive keys"
  - "frontend/src/App.tsx describeError handles code=unsupported_operation with a server-message-first, neutral-fallback branch"
affects: [60]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Single-package MCP fix serving two binaries: internal/mcpserver is imported by both cmd/mcp-server and cmd/mcp-http, so one change to client.go/tools.go reaches both without touching cmd/"
    - "Guarded multipart/FormData append repeated a third time: operation now joins target/preset/opts (Go) and opts (TS) in the 'write the field only if non-empty, so an absent value produces a byte-identical request body' pattern"
    - "Additive optional TS fields for forward/backward API compatibility: FormatsResponse.operations and JobStatusResponse.outputs are both `?:` so this frontend tolerates either an older API server (key absent) or the current one (key present)"

key-files:
  created: []
  modified:
    - internal/mcpserver/client.go
    - internal/mcpserver/tools.go
    - internal/mcpserver/client_test.go
    - internal/mcpserver/tools_test.go
    - frontend/src/api.ts
    - frontend/src/App.tsx
    - frontend/src/api.test.ts
    - frontend/src/errorcodes.test.ts

key-decisions:
  - "operation was inserted as the parameter immediately after filePath in both CreateJob(ctx, filePath, operation, target, preset, opts) and ConvertBlocking(ctx, filePath, operation, target, preset, opts, onTick) -- 'what to do' (operation) precedes 'on what' (target/preset) and 'with what settings' (opts), applied consistently at every call site including pre-existing tests"
  - "No operation selector was added to App.tsx -- today there is exactly one operation (convert), so adding UI for a single-valued dimension would be speculative; the plan explicitly scoped this out"

requirements-completed: [OPRN-01]

# Metrics
duration: ~25min
completed: 2026-08-09
---

# Phase 57 Plan 07: MCP + Frontend Operation Dimension Summary

**Optional `operation` field now reaches all three public surfaces: both MCP servers (`cmd/mcp-server`, `cmd/mcp-http`, via the single shared `internal/mcpserver` package) and the reference frontend forward it to `POST /v1/jobs` byte-identically absent when unset, and the frontend types+handles the additive `operations`/`outputs` keys and the new `unsupported_operation` error code.**

## Performance

- **Duration:** ~25 min
- **Started:** 2026-08-09T21:15:00+03:00 (approx, first edit)
- **Completed:** 2026-08-09T21:22:16+03:00
- **Tasks:** 2/2 completed
- **Files modified:** 8 (0 created, 8 modified)

## Accomplishments

- `internal/mcpserver/client.go` gained `formFieldOperation = "operation"` and a new `operation string` parameter on both `CreateJob` and `ConvertBlocking`, inserted right after `filePath`. The write is guarded exactly like the pre-existing `target`/`preset`/`opts` fields: `if operation != "" { mw.WriteField(...) }` -- an empty operation writes no multipart part at all, so the wire form of an existing caller that never sends it is unchanged, proven by `TestClientCreateJob_OperationFieldAbsentWhenEmpty`.
- `internal/mcpserver/tools.go`'s `ConvertFileInput` gained `Operation string` (`json:"operation,omitempty"`) with a `jsonschema` description pointing the agent at `GET /v1/formats`'s `operations` key for the legal value set, forwarded verbatim into `ConvertBlocking`.
- Both `cmd/mcp-server` and `cmd/mcp-http` import only `internal/mcpserver` for this logic -- confirmed no file under `cmd/` was touched (`git diff --name-only HEAD~2 HEAD -- cmd/` is empty).
- Neither MCP entry point arbitrates whether an operation is legal: the doc comment on `CreateJob` now states explicitly that this client forwards operation without checking it, and a 422 `unsupported_operation` response from the API's registry gate (`convert.Default`, plan 57-04) is surfaced to the caller unchanged, proven by `TestClient_UnsupportedOperation422_ForwardedVerbatim`.
- `frontend/src/api.ts`'s `createJob` gained an optional `operation` param with the identical trim-then-guard pattern already used for `opts`; `FormatsResponse.operations?` and `JobStatusResponse.outputs?` are both declared optional so the SPA survives an API server's response with or without these additive 57-04 keys (proven by `listFormats operations key` tests covering both shapes).
- `frontend/src/App.tsx`'s `describeError` gained a `case 'unsupported_operation':` branch before `default`, preferring the server's message and falling back to a fixed neutral string -- the pre-existing `default` branch and its comment about safely landing future additive codes were left untouched.
- No UI selector for `operation` was added, and `App.tsx`'s existing format-selection/result-rendering logic is otherwise unchanged (in-scope restriction from the plan, since exactly one operation, `convert`, currently exists).

## Task Commits

1. **Task 1: Опциональный operation в обоих MCP-серверах** - `5570265` (feat) — `internal/mcpserver/client.go`, `internal/mcpserver/client_test.go`, `internal/mcpserver/tools.go`, `internal/mcpserver/tools_test.go`
2. **Task 2: operation, operations и outputs в референсном фронтенде** - `5a132ba` (feat) — `frontend/src/api.ts`, `frontend/src/App.tsx`, `frontend/src/api.test.ts`, `frontend/src/errorcodes.test.ts`

## Files Created/Modified

- `internal/mcpserver/client.go` — `formFieldOperation` const; `CreateJob`/`ConvertBlocking` gain an `operation` parameter with guarded multipart write; doc comments extended to state the no-local-arbitration rule explicitly for `operation`
- `internal/mcpserver/client_test.go` — updated call sites for the new signature; three new tests: field-present-when-non-empty, field-absent-when-empty, and 422 `unsupported_operation` forwarded verbatim
- `internal/mcpserver/tools.go` — `ConvertFileInput.Operation` (`json:"operation,omitempty"`, `jsonschema` description referencing `GET /v1/formats`'s `operations` key); forwarded to `ConvertBlocking`
- `internal/mcpserver/tools_test.go` — `TestConvertFile_OperationForwardedToCreateJob`: an end-to-end tool-call test proving `ConvertFileInput.Operation` reaches the `operation` multipart field of `POST /v1/jobs`
- `frontend/src/api.ts` — `createJob({ operation? })` guarded append; `FormatsResponse.operations?`; `JobStatusResponse.outputs?` (both commented as additive/optional for backward+forward compatibility)
- `frontend/src/App.tsx` — `describeError` gains the `unsupported_operation` case
- `frontend/src/api.test.ts` — `createJob operation field` (3 tests: present, absent, blank) and `listFormats operations key` (2 tests: with and without the key) describe blocks, both driving the real `fetch`+`parseResponse` pipeline via a stubbed global `fetch`
- `frontend/src/errorcodes.test.ts` — two new tests for the `unsupported_operation` code (server message present / absent)

## Decisions Made

- **Parameter order: `operation` immediately after `filePath`, before `target`/`preset`/`opts`.** Chosen and applied consistently to `CreateJob`, `ConvertBlocking`, and every call site (including pre-existing tests in `client_test.go`) so "what to do" precedes "on what" and "with what settings" -- no call site was left on the old 4/5-arg signature.
- **No operation selector added to the frontend UI.** The plan explicitly scoped this out: with exactly one operation (`convert`) live today, a selector would have nothing meaningful to select from and would be speculative UI for a future multi-operation world (Phase 60+).

## Deviations from Plan

None - plan executed exactly as written. `npm ci` was run in `frontend/` because `node_modules` was absent in this fresh worktree (needed to run `tsc`/`vitest`); this is routine dependency installation from the existing, unmodified lockfile, not a deviation from the plan's action items -- `git diff --name-only frontend/package.json frontend/package-lock.json go.mod go.sum` is empty, confirming no dependency changed.

## Issues Encountered

None.

## User Setup Required

None - no new external service configuration. No new npm or Go dependencies were added.

## Acceptance Criteria — literal command output

Task 1:

```
$ go build ./... ; echo exit=$?
exit=0
$ go vet ./... ; echo exit=$?
exit=0
$ gofmt -l internal/mcpserver
(empty)
$ go test ./internal/mcpserver/ -count=1
ok  	github.com/apaderin/octoconv/internal/mcpserver	0.826s
$ grep -c 'formFieldOperation *= *"operation"' internal/mcpserver/client.go
1
$ grep -v '^\s*//' internal/mcpserver/client.go | grep -c 'mw.WriteField(formFieldOperation, operation)'
1
$ grep -c 'json:"operation,omitempty"' internal/mcpserver/tools.go
1
$ grep -v '^\s*//' internal/mcpserver/tools.go | grep -c 'in.Operation'
1
$ git diff --name-only HEAD~2 HEAD -- cmd/
(empty)
```

Task 2:

```
$ grep -c "form.append('operation'" frontend/src/api.ts
1
$ grep -c 'operation?: string' frontend/src/api.ts
1
$ grep -c 'operations?:' frontend/src/api.ts
1
$ grep -c 'outputs?:' frontend/src/api.ts
1
$ grep -c "case 'unsupported_operation':" frontend/src/App.tsx
1
$ npm --prefix frontend run build
> tsc --noEmit && vite build
✓ built in 648ms
$ npm --prefix frontend test -- --run
Test Files  4 passed (4)
     Tests  37 passed (37)
$ git diff -U0 HEAD~2 HEAD -- frontend/src/no-text-matching.test.ts frontend/src/formats.test.ts | grep -c '^[+-][^+-]'
0
$ git diff --name-only frontend/package.json frontend/package-lock.json go.mod go.sum
(empty)
```

Overall verification:

```
$ go build ./... && go vet ./...
(clean)
$ gofmt -l internal/mcpserver
(empty)
$ go test ./internal/mcpserver/ -count=1
ok  	github.com/apaderin/octoconv/internal/mcpserver	0.826s
$ go test ./... -count=1   # full suite, 28 packages
ok for every package with tests (0 failures)
$ npm --prefix frontend run build && npm --prefix frontend test -- --run
build: ✓ built in 95ms
test: Test Files 4 passed (4), Tests 37 passed (37)
$ git diff --name-only frontend/package.json frontend/package-lock.json go.mod go.sum
(empty)
```

Live-gate note (plan `<verification>`'s "Живая проверка" item -- `cmd/mcp-http` `convert_file` tool call with `operation: "sanitize"` against a running compose stack): NOT run this session. This plan executed as a parallel worktree agent alongside a sibling (57-06) sharing the same live Postgres/Redis/MinIO; standing up `cmd/mcp-http` against that shared stack risked interfering with the sibling's concurrent state. The unit-level equivalent (`TestClient_UnsupportedOperation422_ForwardedVerbatim` and `TestConvertFile_OperationForwardedToCreateJob`, both against `httptest` fakes returning the exact 422 `unsupported_operation` shape the API produces) proves the same code path without touching shared infrastructure. Recommend the orchestrator run the live gate once both wave-4 agents have merged.

## Next Phase Readiness

- All three public surfaces (REST, both MCP servers, reference frontend) now speak the `operation` dimension end-to-end for OPRN-01.
- Phase 60 can add new operations purely at the engine/queue/container layer -- no further changes to `internal/mcpserver`, `frontend/src/api.ts`, or `frontend/src/App.tsx` are anticipated, since the frontend already learns the operation list from `GET /v1/formats`'s `operations` key rather than hardcoding it.
- No blockers identified for phase 60 or for the orchestrator's post-wave merge.

---
*Phase: 57-operation-dimension-output-roles*
*Completed: 2026-08-09*

## Self-Check: PASSED

All eight created/modified source files confirmed present on disk (`ls -la`); both task commit hashes (`5570265`, `5a132ba`) confirmed present in `git log --oneline --all`.
