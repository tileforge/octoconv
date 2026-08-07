---
phase: 55-live-gate-multi-replica-quota-correctness
plan: 04
subsystem: api
tags: [openapi, operator-visibility, quota, contract-testing]

# Dependency graph
requires:
  - phase: 53-operator-usage-visibility
    provides: "GET /v1/system/usage endpoint, its three-way merge, and the WR-01/WR-02 gaps this plan closes"
provides:
  - "Per-row enforced boolean on every /v1/system/usage row, closing the observe-mode-default dashboard-lie gap (WR-01)"
  - "Metadata-less usage rows skipped instead of emitted with an out-of-enum enforcement_mode:\"\" (WR-02)"
  - "docs/openapi.yaml enforced property + required-array update, kept byte-exact with the handler"
affects: [55-05, future-operator-dashboard-work]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Two-value map lookup + continue-to-skip for defending an enum contract across two unsynchronized reads (mirrors internal/jobs/usage_query.go's client_id IS NOT NULL guard reasoning, applied at the merge site instead of in SQL)"
    - "Per-row boolean promoted out of response-level prose caveat, keeping the response-level flag scoped to only the preconditions it actually asserts"

key-files:
  created: []
  modified:
    - internal/api/system_usage_handlers.go
    - internal/api/system_usage_handlers_test.go
    - docs/openapi.yaml

key-decisions:
  - "Skipped (not defaulted-to-observe) a usage row whose client vanished between the two unsynchronized UsageSummary/ListAllLimits reads — per the plan's explicit action text, since defaulting would fabricate a configuration claim about a client that no longer exists"
  - "Updated two pre-existing tests (TestSystemUsage_UsedTerminalAndInFlightReportedSeparately, TestSystemUsage_SortedByConsumptionDescending) whose all-empty limits fakes became unrealistic once metadata-less rows are skipped — a real ListAllLimits always returns metadata for any client with usage"
  - "Updated TestSystemUsage_TodayWithLiveEnforcementIsAuthoritative's remaining_note assertion from checking for the word \"observe\" to checking for \"enforced\", matching the plan-mandated prose change that points the reader at the new per-row field instead of restating the caveat"
  - "Created .planning/todos/completed/ (did not exist) to complete the plan's todo-move output requirement"

requirements-completed: [TODO-2026-08-05-PER-ROW-ENFORCED-FLAG, TODO-2026-08-05-USAGE-ROW-EMPTY-ENFORCEMENT-MODE]

# Metrics
duration: ~20min
completed: 2026-08-06
---

# Phase 55 Plan 04: Per-Row Enforced Flag + Enum-Safe Usage Rows Summary

**GET /v1/system/usage now carries a per-row `enforced` boolean (closing the observe-mode dashboard lie) and never emits a row outside its own `enforcement_mode` enum, closing both WR-01 and WR-02 from 53-REVIEW-GAPS.md.**

## Performance

- **Duration:** ~20 min
- **Completed:** 2026-08-06
- **Tasks:** 2 (both TDD: RED/GREEN commit pairs)
- **Files modified:** 3 (`internal/api/system_usage_handlers.go`, `internal/api/system_usage_handlers_test.go`, `docs/openapi.yaml`)

## Accomplishments

- **WR-02 closed:** the row-building loop's zero-value map lookup (`cm := meta[k.clientID]`) — which could silently emit `enforcement_mode:""` for a client deleted between the two unsynchronized `UsageSummary`/`ListAllLimits` reads — is replaced with a two-value lookup that skips the row instead. The skip is proven safe: it can only fire on a `usageByKey`/`limitsByKey` key, never on the meta-seeded synthetic idle-client row (plan 53-05's three-way union), so the no-leak 404 and idle-client-visibility guarantees are untouched.
- **WR-01 closed:** every usage row now carries its own `enforced` boolean — `true` only when `period == periodToday`, the service-wide kill-switch is off, AND that row's own `enforcement_mode` is `"enforce"`. Previously the response-level `remaining_matches_admission_gate` was the strongest available signal and defaulted to `true` for the typical fleet (every client at the DB-default `observe` mode), falsely implying live enforcement.
- **OpenAPI kept honest:** `enforced` is declared `type: boolean` and added to the row's `required` array; `remaining_matches_admission_gate`'s description is narrowed to state only the response-level preconditions it actually asserts, pointing readers at each row's `enforced` field.
- Two new handler tests close coverage gaps that had none before: `TestSystemUsage_UsageRowWithoutClientMetadataIsSkipped` (the exact WR-02 race) and `TestSystemUsage_EnforcedIsPerRowNotResponseLevel` (the exact WR-01 gap, across default/kill-switch-on/multi-day server configurations, decoding into `map[string]any` so an omitted field fails rather than silently reading as `false`). A third test, `TestSystemUsage_EnforcementModeNeverOutsideEnum`, sweeps a richer fixture against `{observe, enforce}`.

## Task Commits

Each task followed RED → GREEN (TDD):

1. **Task 1: Never synthesize an out-of-enum enforcement_mode (WR-02)**
   - `8d660eb` test(55-04): add coverage for metadata-less usage rows (WR-02)
   - `273d34e` fix(55-04): skip usage rows with no metadata instead of emitting empty enforcement_mode (WR-02)
2. **Task 2: Per-row enforced flag plus the OpenAPI contract it belongs to (WR-01)**
   - `f7744fd` test(55-04): add coverage for per-row enforced flag (WR-01)
   - `c363d6c` feat(55-04): add per-row enforced flag to GET /v1/system/usage (WR-01)
   - `0c1adae` docs(55-04): declare per-row enforced as required in the usage schema (WR-01)
3. **Output step:** `2b3e2d4` chore(55-04): move closed todos to completed/

## Files Created/Modified

- `internal/api/system_usage_handlers.go` — skip-metadata-less-row fix (Task 1); `enforcementModeEnforce` constant, `Enforced` field on `systemUsageRow`, per-row computation, narrowed package/field doc comments and `RemainingNote` default-branch prose (Task 2)
- `internal/api/system_usage_handlers_test.go` — two new WR-02 tests, one new WR-01 test (3 sub-cases), two pre-existing test fixtures updated to carry realistic limits metadata, one pre-existing assertion updated to match the new `RemainingNote` prose
- `docs/openapi.yaml` — `enforced` property + `required` array update on the usage row schema; narrowed `remaining_matches_admission_gate` description

## Decisions Made

- **Skip over default-to-observe** for the WR-02 metadata-less-row case, per the plan's explicit action text and the todo file's own reasoning: a key reaching that branch can only describe a client that no longer exists, so skipping hides nothing from the operator, while defaulting would fabricate a configuration claim about a gone client.
- **Fixed two pre-existing test fixtures** (`TestSystemUsage_UsedTerminalAndInFlightReportedSeparately`, `TestSystemUsage_SortedByConsumptionDescending`) that used all-empty `fakeLimitsRepo{}` alongside populated usage rows — a pattern the WR-02 fix now correctly treats as "client vanished," skipping the row. A real `ListAllLimits` always returns metadata for any client with usage, so these fixtures were unrealistic; added minimal nil-Engine metadata rows to match production reality rather than weakening the fix.
- **Updated `TestSystemUsage_TodayWithLiveEnforcementIsAuthoritative`'s prose assertion** from `strings.Contains(note, "observe")` to `strings.Contains(note, "enforced")`, per the plan's explicit instruction to stop restating the observe-mode caveat as prose and instead point at the new per-row field.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Two pre-existing tests broke as a direct, intended consequence of the WR-02 fix**
- **Found during:** Task 1 verification (`go test ./internal/api/ -run 'SystemUsage'`)
- **Issue:** `TestSystemUsage_UsedTerminalAndInFlightReportedSeparately` and `TestSystemUsage_SortedByConsumptionDescending` populated `usage` rows for clients with an entirely empty `fakeLimitsRepo{}`. Before this plan's fix, the zero-value `cm := meta[k.clientID]` lookup silently defaulted those rows to `client_name:""`/`enforcement_mode:""` and the tests never checked those fields. After the fix, a metadata-less usage row is correctly skipped, so both tests started asserting `usage rows = 0, want N`.
- **Fix:** Added minimal nil-Engine metadata rows (`clients.LimitWithMeta{..., Engine: nil}`) to each fixture's `fakeLimitsRepo`, matching what a real `ListAllLimits` call always returns for a client that exists.
- **Files modified:** `internal/api/system_usage_handlers_test.go`
- **Verification:** Full `TestSystemUsage_*` suite passes (39 tests, all green) after the fix.
- **Committed in:** `8d660eb` (bundled into the RED/test commit for Task 1, since these are fixture corrections not new coverage)

**2. [Rule 3 - Blocking] `.planning/todos/completed/` directory did not exist**
- **Found during:** Output step (moving the two closed todo files)
- **Issue:** The plan's `<output>` block instructs moving both todo files to `.planning/todos/completed/` "the existing sibling of `pending/`" — but only `.planning/todos/pending/` existed; no prior plan had created `completed/`.
- **Fix:** Created the directory and moved both files into it with `git mv`.
- **Files modified:** `.planning/todos/completed/2026-08-05-per-row-enforced-flag.md`, `.planning/todos/completed/2026-08-05-usage-row-empty-enforcement-mode.md`
- **Verification:** `git status --short` confirms both files tracked as renames, `ls .planning/todos/` shows both `pending/` and `completed/`.
- **Committed in:** `2b3e2d4`

---

**Total deviations:** 2 auto-fixed (1 test-fixture bug fix, 1 blocking missing-directory fix)
**Impact on plan:** Both fixes were necessary for the plan's own acceptance criteria ("every pre-existing TestSystemUsage_* test still reports PASS" and "move both todo files to completed/"). No scope creep — no production behavior was touched beyond what the plan specified.

## Issues Encountered

None beyond the two deviations above. One process note: an early attempt to verify the RED phase of Task 1 via `git stash` was corrected mid-flight — `git stash` is prohibited in worktree mode per this project's own destructive-git-prohibition rule (shared `refs/stash` across worktrees), and the stash was popped back immediately with content verified via `git stash show -p` before popping. No data was lost and no sibling worktree was affected (verified the stash list was single-entry throughout). Subsequent RED-phase verification (Task 2) used a file-copy-based revert/restore instead, avoiding `git stash` entirely.

## Response Body Shape / OpenAPI Contract Check (required by orchestrator)

Final `usage[]` row shape (Go struct field order == JSON key order):

```json
{
  "client_id": "uuid",
  "client_name": "string",
  "engine": "string",
  "enforcement_mode": "observe" | "enforce",
  "enforced": true | false,
  "used_terminal": { "job_count": 0, "bytes_in": 0, "bytes_out": 0 },
  "used_in_flight": { "job_count": 0, "bytes_in": 0 },
  "limits": { "max_concurrent": null, "max_daily_bytes": null, "max_daily_jobs": null },
  "remaining": { "concurrent": null, "daily_bytes": null, "daily_jobs": null }
}
```

`docs/openapi.yaml`'s row `required` array is now exactly:
`[client_id, client_name, engine, enforcement_mode, enforced, used_terminal, used_in_flight, limits, remaining]`
— byte-identical to the plan's acceptance criterion, confirmed via `grep -c` returning `1`. The `enforcement_mode` enum (`[observe, enforce]`) is unchanged and is now provably never violated: `TestSystemUsage_EnforcementModeNeverOutsideEnum` sweeps every row of a multi-client fixture, and `TestSystemUsage_UsageRowWithoutClientMetadataIsSkipped` proves the one path that used to violate it is now closed. `enforced` is a plain `boolean` (never omitted, never a pointer) — `TestSystemUsage_EnforcedIsPerRowNotResponseLevel` decodes into `map[string]any` specifically so an accidentally-omitted key fails the test.

YAML validity confirmed with: `ruby -ryaml -e 'YAML.load_file("docs/openapi.yaml", aliases: true); puts "ok"'` → `ok`.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Both folded todos (2 and 3 of D-07) are closed; the milestone's todo queue is now empty except for todo 1 (Retry-After midnight race), which is a separate plan's scope.
- `go build ./...`, `go vet ./...`, `gofmt -l internal/api` (empty), and `go test ./... -timeout 5m` all pass at the end of this plan.
- No known stubs or threat-surface additions beyond what the plan's own `<threat_model>` already registered (T-55-15 through T-55-18, all `mitigate`, all addressed by the changes above).

---
*Phase: 55-live-gate-multi-replica-quota-correctness*
*Completed: 2026-08-06*

## Self-Check: PASSED

All claimed files verified present on disk (`internal/api/system_usage_handlers.go`,
`internal/api/system_usage_handlers_test.go`, `docs/openapi.yaml`,
`.planning/todos/completed/2026-08-05-per-row-enforced-flag.md`,
`.planning/todos/completed/2026-08-05-usage-row-empty-enforcement-mode.md`). All six task
commits (`8d660eb`, `273d34e`, `f7744fd`, `c363d6c`, `0c1adae`, `2b3e2d4`) verified present via
`git log --oneline --all`.
