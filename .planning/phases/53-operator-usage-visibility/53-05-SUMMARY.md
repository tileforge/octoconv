---
phase: 53-operator-usage-visibility
plan: 05
subsystem: api
tags: [go, chi, http, openapi, quota, operator, gap-closure]

# Dependency graph
requires:
  - phase: 53-04
    provides: "handleSystemUsage, systemUsageResponse/systemUsageRow, existing fakeUsageRepo/fakeLimitsRepo test doubles, newUsageTestServer/newOperatorUsageTestServer helpers"
provides:
  - "usageEngineNone constant and the third keySet-seeding pass, closing ROADMAP SC2 / OPSV-02"
  - "Regression coverage for the 'zero usage AND zero limits' merge cell and the SC2×SC3 distinguishability tension"
affects: []

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "three-way key-set union: usage keys ∪ non-nil-Engine limit keys ∪ every remaining ClientID in meta — a nil-Engine LimitWithMeta row (no client_limits row at all) is the documented signal that MUST NOT be discarded (internal/clients/limits.go:116-119), and dropping it made a real idle client's response byte-identical to a nonexistent client_id's"
    - "mutation-check verification via cp backup/restore (never git stash, which is unsafe across worktrees): temporarily comment out the fix, confirm the exact expected subset of tests fails, restore and confirm git diff --stat is empty"

key-files:
  created: []
  modified:
    - internal/api/system_usage_handlers.go
    - internal/api/system_usage_handlers_test.go
    - docs/openapi.yaml

key-decisions:
  - "usageEngineNone = \"\" kept as a plain string constant, not a nullable JSON type change — docs/openapi.yaml's `required` list for the usage row already lists `engine` as required; changing its wire type would be a breaking-change surface with no requirement pulling for it"
  - "The mutation check (plan's own acceptance criterion) was performed via cp-based backup/restore of internal/api/system_usage_handlers.go rather than git stash, per this plan's explicit isolation requirement — refs/stash is shared across worktrees and must never be touched by a worktree-isolated executor"

patterns-established:
  - "Pattern: any handler that merges N repo reads by key must seed its emitted key set from every distinct identity across ALL input maps, not just the maps that happen to carry a 'has activity' signal — an identity-only row (no usage, no limit) is a real answer, not an omission"

requirements-completed: [OPSV-02]

# Metrics
duration: ~35min
completed: 2026-08-05
---

# Phase 53 Plan 05: Close SC2/OPSV-02 Idle-Client Visibility Gap Summary

**`handleSystemUsage`'s emitted key set now seeds from every client in `meta` (not just clients with a usage or configured-limit key), so a real idle unlimited client returns exactly one synthetic row (`engine:""`, real name/mode, all-null limits/remaining) instead of `{"usage":[]}` — byte-identical to a nonexistent `client_id` — while ROADMAP SC3's non-operator 404 stays unweakened on the fix's new data path.**

## Performance

- **Duration:** ~35 min
- **Tasks:** 2
- **Files modified:** 3 (0 created, 3 modified)

## Accomplishments

- `internal/api/system_usage_handlers.go`: added the `usageEngineNone` constant and a third `keySet`-seeding pass (`for id := range meta { ... }`) inserted between the existing `usageByKey`/`limitsByKey` seeding loops and `rows := make([]systemUsageRow, ...)`; rewrote the comment block above `keySet` to describe the three-way union and cite the violated `internal/clients/limits.go:116-119` contract ("MUST NOT be discarded by the caller"); added a one-line comment at the `consumption` accumulation noting a synthetic row contributes 0 and sorts to the bottom via the existing total order — `sortSystemUsageRows` itself is byte-for-byte unchanged
- `docs/openapi.yaml`: documented the empty `engine` label's meaning ("exists, unlimited, idle" row), extended the `usage` array description with the never-omit-an-idle-client guarantee, and added one sentence to the endpoint description distinguishing a real idle client's one-row response from a nonexistent client's `[]` — neither `required:` list was touched
- `internal/api/system_usage_handlers_test.go`: five new tests — `TestSystemUsage_IdleUnlimitedClientAppearsAsOneRow` (the previously-untested "zero usage AND zero limits" merge cell), `TestSystemUsage_NonexistentClientIDReturnsEmptyUsage`, `TestSystemUsage_IdleClientDistinguishableFromNonexistentClient` (the exact CR-01/Gap #1 regression, asserting the two response bodies are NOT `bytes.Equal`), `TestSystemUsage_AllClientsIncludesIdleUnlimitedClients` (un-narrowed shape: 3 rows, busy client first per D-02), `TestSystemUsage_NonOperatorNoLeak404EvenWithClientsPresent` (SC3 regression on the fix's new data path, populated limits fake, zero repo calls)
- Mutation check performed exactly as the plan's acceptance criteria required: `cp internal/api/system_usage_handlers.go /tmp/.../suh.bak`, commented out the meta-seeding loop, ran the suite, restored via `cp`, confirmed `git diff --stat` was empty — no `git stash` used anywhere, per the worktree isolation requirement

## Task Commits

Each task was committed atomically:

1. **Task 1: Seed the emitted key set from every client in meta** — `7c71a5e` (fix)
2. **Task 2: Prove the idle client is visible and still distinguishable from a nonexistent one** — `ec16dfe` (test)

## Files Modified

- `internal/api/system_usage_handlers.go` — `usageEngineNone` constant, third `keySet` seeding pass, rewritten merge comment block, one-line comment at `consumption` accumulation
- `internal/api/system_usage_handlers_test.go` — five new test functions (additions only; no existing test edited — `git diff | grep -c '^-[^-]'` = 0)
- `docs/openapi.yaml` — `engine` property description, `usage` array description, endpoint description sentence; both `required:` lists byte-for-byte unchanged

## Decisions Made

- Kept `engine` a plain (never-null) JSON string for the synthetic row (`usageEngineNone = ""`), matching the plan's locked row shape and avoiding any OpenAPI `required` list change or wire-type break for existing consumers.
- Performed the plan's mandatory mutation check with `cp`-based backup/restore instead of `git stash`, since `refs/stash` is shared across worktrees and this executor is running in a git worktree alongside sibling agents.

## Deviations from Plan

None — plan executed exactly as written. Both tasks' acceptance criteria were verified literally (see Verification Evidence below).

## Verification Evidence

### Exact synthetic-row JSON for an idle unlimited client

For `TestSystemUsage_IdleUnlimitedClientAppearsAsOneRow` (client id redacted to `<id>`, name `"idle-co"`, mode `"observe"`), the emitted row (per `systemUsageRow`'s JSON tags) is:

```json
{
  "client_id": "<id>",
  "client_name": "idle-co",
  "engine": "",
  "enforcement_mode": "observe",
  "used_terminal": {"job_count": 0, "bytes_in": 0, "bytes_out": 0},
  "used_in_flight": {"job_count": 0, "bytes_in": 0},
  "limits": {"max_concurrent": null, "max_daily_bytes": null, "max_daily_jobs": null},
  "remaining": {"concurrent": null, "daily_bytes": null, "daily_jobs": null}
}
```

Observed live response size for this exact case: `200 640B` (narrowed `?client_id=` request), confirmed via `go test -v` request logging.

### Mutation-check observed failure count

With Task 1's third seeding pass (`for id := range meta { ... }`) commented out and the build still compiling cleanly:

```
--- FAIL: TestSystemUsage_IdleUnlimitedClientAppearsAsOneRow
--- FAIL: TestSystemUsage_IdleClientDistinguishableFromNonexistentClient
--- FAIL: TestSystemUsage_AllClientsIncludesIdleUnlimitedClients
```

Exactly the 3 tests the plan's acceptance criteria named (tests 1, 3, 4) — no more, no fewer. Tests 2 (`TestSystemUsage_NonexistentClientIDReturnsEmptyUsage`) and 5 (`TestSystemUsage_NonOperatorNoLeak404EvenWithClientsPresent`) correctly kept passing under the mutation, since neither depends on the fix. After restoring via `cp`, `git diff --stat internal/api/system_usage_handlers.go` was empty (byte-identical to the pre-mutation committed state).

### Clean-diff invariants

- `git diff --exit-code go.mod go.sum` — clean (exit 0), no dependency change
- `git diff --exit-code internal/convert/exec.go` — clean (exit 0), untouched
- `grep -c "usage not found" internal/api/*.go` — 0, no new 404 literal introduced (SC3's single-literal invariant holds)
- `git diff internal/api/system_usage_handlers.go | grep -c "^-.*sort.Slice"` — 0, `sortSystemUsageRows` byte-for-byte unchanged
- `grep -c 'required: \[client_id, client_name, engine, enforcement_mode, used_terminal, used_in_flight, limits, remaining\]' docs/openapi.yaml` — 1, required list undisturbed

### Test suite results

- `go build ./... && go vet ./... && gofmt -l .` — all clean, no output
- `go test ./internal/api/ -run TestSystemUsage -count=1 -v` — 33 `--- PASS` lines (22 top-level test functions, some table-driven with subtests, including the 5 new ones), 0 `--- FAIL`, 0 `--- SKIP`
- Full repo suite against the live shared `octoconv-db`/`octoconv-redis`/`octoconv-minio` containers: `go test ./... -count=1` — every package `ok` **except** `internal/storage`'s `TestRoundTrip`, which failed with `"Storage backend has reached its minimum free drive threshold. Please delete a few objects to proceed."` This is a pre-existing MinIO disk-space environment condition on the shared container, entirely unrelated to this plan's changes (this plan never touches `internal/storage`) — out of scope per the executor's scope-boundary rule (only auto-fix issues directly caused by the current task's changes). `internal/api` itself passed cleanly (`ok`, 2.448s), including all 33 `TestSystemUsage` cases against the live DB path.

## Known Stubs

None.

## Threat Flags

None — this plan closes an existing gap; it introduces no new network endpoints, auth paths, or schema changes. The threat model in 53-05-PLAN.md (T-53-05-01..04, T-53-05-SC) already covers the one new surface (a narrowed request becomes a client-existence oracle for operators, `accept`ed and pinned by test 5).

## Issues Encountered

- The shared `internal/storage` `TestRoundTrip` failure (MinIO free-drive threshold) is a pre-existing environment condition, not caused by this plan. Logged here for visibility; not auto-fixed, per scope-boundary rules (out-of-scope package, unrelated to any file this plan modifies).

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- ROADMAP SC2 / OPSV-02 is now satisfiable end-to-end: an operator querying `?client_id=<a real idle unlimited client>` gets exactly one row identifying that client with all-null limits, distinguishable byte-for-byte from a nonexistent `client_id`'s `{"usage":[]}`.
- ROADMAP SC3 remains provably unweakened: the non-operator 404 stays byte-identical with zero repo calls, now proven specifically against the fix's new (populated-limits) data path.
- `internal/convert/exec.go` and `go.mod`/`go.sum` are untouched, confirming this was a scoped, single-file (plus its test and docs) gap closure with no supply-chain or unrelated-subsystem surface.
- No blockers for phase completion.

---
*Phase: 53-operator-usage-visibility*
*Completed: 2026-08-05*

## Self-Check: PASSED

- FOUND: internal/api/system_usage_handlers.go
- FOUND: internal/api/system_usage_handlers_test.go
- FOUND: docs/openapi.yaml
- FOUND: .planning/phases/53-operator-usage-visibility/53-05-SUMMARY.md
- FOUND commit: 7c71a5e
- FOUND commit: ec16dfe
