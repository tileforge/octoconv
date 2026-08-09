---
phase: 57-operation-dimension-output-roles
verified: 2026-08-09T18:40:53Z
status: passed
score: 9/9 must-haves verified
overrides_applied: 0
---

# Phase 57: Operation Dimension & Output Roles Verification Report

**Phase Goal:** У джобы появляется измерение «операция» (`convert` по умолчанию, абсолютная обратная совместимость), реестр разрешает движок по тройке `(operation, from, to)`, а у выходов появляется роль — так что отчёт больше никогда не может быть отдан вместо основного результата.
**Verified:** 2026-08-09T18:40:53Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths (ROADMAP Success Criteria + Requirements)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | SC1 — client not sending `operation` observes zero behavioral change; `engines` key stays byte-identical | ✓ VERIFIED | `internal/api/golden_pinning_test.go` `TestPre57GoldenListFormats`/`TestPre57GoldenGetJobSingleOutput` re-run PASS locally; orchestrator's independent LIVE gate proved byte-for-byte prefix identity on real binaries (`GET /v1/formats` pre==post minus closing brace, `engines` deep-equal). Golden file commit-count = 1 for both files (orchestrator-verified). |
| 2 | SC2 — registry resolves by `(operation, from, to)`; `sanitize` pdf→pdf does not collide with, and cannot be selected for, `convert` | ✓ VERIFIED | `internal/convert/convert.go:161-266` (`opKey{Operation,From,To}`, `LookupOp`, `Lookup` delegates with `OperationConvert`); `internal/convert/operations_test.go` `TestSanitizeIdentityPairDisjointFromConvert` builds a **fresh registry** with a **real converter** (`LibreOfficeConverter{}`) plus a synthetic sanitize pdf→pdf converter and proves `Lookup("pdf","pdf")`/`LookupOp("convert","pdf","pdf")` both return false — re-run locally, PASS. Not a self-referential fixture: it exercises `Registry.Lookup`/`LookupOp`, the same methods production code calls. |
| 3 | SC3 — unsupported `(operation, from, to)` rejected 422 fail-closed, before S3 write and before job-row creation | ✓ VERIFIED | `internal/api/handlers.go:421-455` — two sequential gates (`IsKnownOperation` → `unsupported_operation`; `EngineForOp` → `unsupported_conversion`), both strictly before `s.storage.Upload`/`s.repo.Create` (confirmed by reading code order). Orchestrator's LIVE gate: `operation=sanitize` → 422 + zero DB row delta. |
| 4 | SC4 — `job_outputs` carries role, existing rows correct with no backfill, `GET /v1/jobs/{id}` returns every output with its role, single-output convert job stays byte-identical | ✓ VERIFIED | Migration `internal/db/migrations/0015_operation_and_output_roles.sql` (additive `role text NOT NULL DEFAULT 'primary'` + CHECK); `TestOutputRoleDefaultAppliesToPreExistingRows` (inserts pre-migration-shaped row, reads back `'primary'`) re-run live against PostgreSQL 18 — PASS; `internal/api/handlers.go:892-974` `handleGetJob` builds `outputs[]` guarded on `len(outs) > 1`, selects `download_url` by `Role == outputRolePrimary`. |
| 5 | OPRN-02 disjointness enforcement is structural, not incidental | ✓ VERIFIED | `Registry.Register` panics on triple collision (`convert.go:198-210`), verified by `TestRegisterPanicsOnTripleCollision` (positive: same triple panics; negative control: `(inspect,pdf,pdf)` — different op, same pair — does NOT panic) — re-run, PASS. `TestOneOperationPerEngineClass` (D-14) re-run, PASS. |
| 6 | ROLE-02 defect (`outs[0]` blind presign) is closed, proven by reverse-order + inversion | ✓ VERIFIED | `TestGetJob_DownloadURLResolvesToPrimaryNotOrdinalZero` (report at ordinal 0, primary at ordinal 1) re-run, PASS. Orchestrator independently reproduced the D-18 inversion: reverting to `outs[0]` makes the test fail with the wrong URL; restoring makes it pass. |
| 7 | D-13 — a `done` job without exactly one primary output is unreachable (write-time invariant, not read-time) | ✓ VERIFIED | `internal/jobs/repo.go:204-241` `MarkDone`'s apply closure runs `SELECT count(*) ... role='primary'` inside the SAME locked transaction as the status UPDATE; `n != 1` aborts before UPDATE/`recordUsage`. `TestMarkDoneRejectsJobWithOnlyReportOutput`, `TestMarkDoneRejectsJobWithTwoPrimaryOutputs`, `TestMarkDoneAcceptsPrimaryPlusReport`, `TestMarkDoneAcceptsSinglePrimaryOutput` re-run live against PostgreSQL 18 — all PASS. |
| 8 | D-05 — all three public surfaces (REST, both MCP servers, reference frontend) accept optional `operation` | ✓ VERIFIED | REST: `handlers.go:33,160-163`. Both MCP binaries (`cmd/mcp-server`, `cmd/mcp-http`) share `internal/mcpserver.Client.CreateJob`/`tools.go ConvertFileInput.Operation` (`internal/mcpserver/client.go:174-191`, `tools.go:68,115`) — one shared implementation serves both entry points. Frontend: `frontend/src/api.ts:107-124` (`createJob({operation})`, guarded on non-empty), `frontend/src/App.tsx:48-49` (maps `unsupported_operation` to a distinct message). |
| 9 | Requirements OPRN-01/02/03, ROLE-01/02 all traced to plans, no orphans | ✓ VERIFIED | All 5 IDs appear in `requirements:` frontmatter across 57-01/02/03/04/05/06/07; `.planning/REQUIREMENTS.md` lists the same 5 IDs mapped to "Phase 57"; no additional Phase-57-mapped ID found unclaimed by any plan. |

**Score:** 9/9 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/convert/convert.go` | `OperationalConverter`, triple-keyed `Registry`, `Register`/`Replace`, `LookupOp`/`SupportsOp`/`EngineForOp`, `Operations`/`IsKnownOperation`/`OperationClasses` | ✓ VERIFIED | All present, read in full; wired into `handlers.go`, `worker.go`, `formats_handlers.go`. |
| `internal/convert/operations_test.go` | Triple-disjointness, identity pdf→pdf, Register-panic, Replace-semantics, D-14 tests | ✓ VERIFIED | 253 lines, 6 tests, all re-run PASS. |
| `internal/db/migrations/0015_operation_and_output_roles.sql` | Additive `job_outputs.role` + `jobs_operation_check` widened to `sanitize` | ✓ VERIFIED | Present, applied (tests read back `role='primary'` on the live DB); documents both intentional divergences (DB accepts `sanitize`, API rejects it; `presets.operation` untouched). |
| `internal/api/handlers.go` | `formFieldOperation`, D-04 normalization, two-gate 422, role-driven `handleGetJob` | ✓ VERIFIED | Read in full at both the create-job (140-207, 420-455) and get-job (863-982) sites. |
| `internal/api/formats_handlers.go` | Additive `operations` key, `engines` unchanged | ✓ VERIFIED | Read in full; `engines` loop explicitly marked unchanged; `operations` built from `OperationClasses()`. |
| `internal/jobs/repo.go` | `Output.Role`, `role` in `Outputs` SELECT, D-13 precondition in `MarkDone` | ✓ VERIFIED | `jobs.go:72-80` (`Output.Role` field), `repo.go:580-608` (`Outputs` selects role), `repo.go:204-241` (D-13 in `MarkDone`'s closure). |
| `cmd/av-worker/main.go`, `cmd/ocr-worker/main.go`, `cmd/pandoc-worker/main.go` | `Replace` instead of `Register` | ✓ VERIFIED | `grep` confirms all three call `convert.Default.Replace(...)`, zero `Register` calls for configured instances. |
| `internal/mcpserver/client.go`, `tools.go` | Optional `operation` plumbed to `POST /v1/jobs` | ✓ VERIFIED | `formFieldOperation`, `CreateJob(..., operation, ...)`, `ConvertFileInput.Operation`. |
| `frontend/src/api.ts`, `App.tsx` | `createJob({operation})`, typed `operations`/`outputs`, `unsupported_operation` message | ✓ VERIFIED | All three present; `App.tsx` has no operation *UI control* (not required by 57-07's must_haves, which only require the client to be *able* to send it and to type the new response keys). |
| `docs/ERROR-CODES.md`, `internal/errcode/errcode.go` | `unsupported_operation` documented and Go↔doc synced | ✓ VERIFIED | `errcode.go:29`, doc row present; `TestErrorCodesDocMatchesGoConstants` re-run PASS. |

### Key Link Verification

| From | To | Via | Status | Details |
|------|-----|-----|--------|---------|
| `handlers.go` `handleCreateJob` | `convert.Default.IsKnownOperation`/`EngineForOp` | direct call, before `Upload`/`Create` | ✓ WIRED | Confirmed by code order read (lines 421-455 precede `s.storage.Upload` at 780+ / `s.repo.Create` at 780). |
| `internal/worker/worker.go` `process()` | `Registry.LookupOp` | `h.registry.LookupOp(job.Operation, job.SourceFormat, job.TargetFormat)` | ✓ WIRED | `worker.go:1267`. |
| `internal/mcpserver/tools.go ConvertFileInput` | `internal/mcpserver/client.go CreateJob` | `in.Operation` forwarded through `ConvertBlocking` | ✓ WIRED | `tools.go:115` → `client.go:391-392`. |
| `frontend/src/api.ts createJob` | `POST /v1/jobs` | `form.append('operation', ...)` guarded on non-empty | ✓ WIRED | `api.ts:119-124`; tested by `api.test.ts` (`appends`/`omits when absent`/`omits when empty`). |
| `internal/jobs/repo.go MarkDone` | `job_outputs` role count | `SELECT count(*) ... role='primary'` inside the transition's locked tx | ✓ WIRED | `repo.go:226-234`, proven live by 4 DB-integration tests. |
| `handleGetJob` | `outputs[]`/`download_url` | role-driven selection loop | ✓ WIRED | `handlers.go:928-973`; proven by reverse-order + inversion tests. |

### Deep-Dive: Items Flagged for Extra Scrutiny

**1. OPRN-02 disjointness — real behavior, not a self-testing fixture.**
`TestSanitizeIdentityPairDisjointFromConvert` constructs `NewRegistry()` (empty, not `convert.Default`), registers a REAL converter (`LibreOfficeConverter{}`, which owns `docx→pdf` among other real convert pairs) alongside the synthetic `sanitizeIdentityConverter` (`sanitize, pdf, pdf, engine=probe`), then asserts through the registry's actual public methods (`Lookup`, `LookupOp`, `EngineFor`, `EngineForOp`) that the sanitize triple is invisible to `convert`. This exercises the same `opKey{Operation,From,To}` map and the same delegation path (`Lookup` → `LookupOp(OperationConvert,...)`) that production code (`handlers.go`, `worker.go`) uses. It is not testing a fixture's own bookkeeping — it is testing `Registry`'s real map-keyed lookup. `TestRegisterPanicsOnTripleCollision`'s negative control (`(inspect,pdf,pdf)` registers cleanly after `(sanitize,pdf,pdf)` panics) additionally proves the panic guard is triple-scoped, not pair-scoped. Verdict: legitimate, re-run and confirmed PASS.

**2. `AddOutput`/D-13 seam — intentional, documented, correctly scoped to Phase 60, not a Phase 57 gap.**
`internal/jobs/repo.go:449-464` `AddOutput`'s INSERT statement does not name the `role` column at all — every row it writes gets `role='primary'` purely from the DB `DEFAULT`. There is currently **no public `Repo` method that can create a second, non-primary output** (`report`/`preview`/`thumbnail`). This is called out explicitly and by name in the migration's own header comment (`0015_operation_and_output_roles.sql:9-11`: *"AddOutput deliberately never writes this column"*) and independently corroborated by `57-05-SUMMARY.md`'s documented deviation (a pre-existing usage test that called `AddOutput` twice had to be patched with a raw `UPDATE ... SET role='report'` because `AddOutput` cannot express a non-primary role). All of Phase 57's own tests that need a `report`/mixed-role row (`TestMarkDoneRejectsJobWithOnlyReportOutput`, `TestMarkDoneAcceptsPrimaryPlusReport`, `TestMarkDoneRejectsJobWithTwoPrimaryOutputs`) bypass `AddOutput` with direct SQL for exactly this reason.
This is **in scope by design, not a Phase 57 gap**: ROLE-01 only requires the schema to *carry* a role with a correct default and no backfill (satisfied) and D-13 only requires the *invariant* to hold at `MarkDone` time regardless of how rows got there (satisfied and tested against raw-SQL-inserted rows). Nothing in OPRN-01/02/03/ROLE-01/ROLE-02 or the ROADMAP success criteria requires the worker to be able to *produce* a second output yet — that capability is Phase 60's job (sanitize engine), and the ROADMAP text for Phase 60 explicitly depends on Phase 57 for "output role" as a *schema and read-path* precondition, not a write-path one. Phase 60's planner will need to extend `AddOutput` (or add a role-aware sibling) — this is a real, load-bearing seam, correctly and explicitly flagged in the migration comment for the next phase's planner to find. Not a blocker.

**3. Golden fixture (`pre57_formats.golden.json`) contains a synthetic `["mp4","avtestout"]` pair — pre-existing (Phase 35), not introduced by Phase 57, does not weaken any Phase 57 claim.**
`internal/api/handlers_test.go:524-550`'s `fakeAVConverter`/`avTestTarget` (`"avtestout"`) is registered into the process-global `convert.Default` via a package-level `init()` that predates Phase 57 (comment cites "35-04", "35-06"). Because Go test binaries share one process, this synthetic pair appears in `convert.Default.Classes()` and therefore in both `pre57_formats.golden.json` (captured by Plan 57-01's first commit) and every subsequent `TestPre57GoldenListFormats` run — it is present in the "before" and "after" snapshots equally. `golden_pinning_test.go`'s own header comment does not mention this, but it also does not claim the golden bytes represent a clean/real-world registry — its stated purpose is narrower: prove the Go test binary's `GET /v1/formats` handler output for the `engines` key is byte-for-byte unchanged by Phase 57's code changes, using whatever registry state that binary happens to have. That narrower claim holds regardless of `avtestout`'s presence, because the contaminant is identical on both sides of the comparison. The claim that matters for real users — SC1, production-binary byte-identity — was independently proven by the orchestrator against the actual compiled `api` binary (which never links `handlers_test.go` and therefore never registers `fakeAVConverter`). Verdict: cosmetic imprecision in a pre-existing test-only artifact, zero impact on any Phase 57 requirement or success criterion. Not a gap.

**4. Deviation-narrative honesty (57-04, 57-06) — independently confirmed.**
- 57-04's two "unsatisfiable grep pattern" claims were re-run verbatim: `grep -c 'SetEscapeHTML(false)' internal/api/golden_pinning_test.go` → 2 (matches the SUMMARY's claim and stated cause: a pre-existing Plan-57-01 doc-comment occurrence plus this plan's own new call); `grep -c 'assertGolden(t, "pre57_formats.golden.json"'` → 0, while `grep -c 'assertGolden(t, "formats.golden.json"'` → 1, confirming `assertGolden`'s signature prepends the `pre57_` prefix internally, exactly as the SUMMARY describes. Both discrepancies are honestly attributed to pre-existing code shape, not gamed.
- 57-06's claim that `TestGetJob_DonePresigned` is a pre-existing fixture outside `golden_pinning_test.go`'s closed three-exception list is accurate: the test lives in `handlers_test.go` (a different file from the exception list's scope, which is explicitly "the Go source [of golden_pinning_test.go]"), predates `jobs.Output.Role` (added in 57-03), and needed the identical one-line `Role: "primary"` fix for the identical reason. Confirmed by reading the test and its inline comment.

### Requirements Coverage

| Requirement | Source Plan(s) | Description | Status | Evidence |
|---|---|---|---|---|
| OPRN-01 | 57-01, 57-04, 57-07 | Client can specify `operation`; absence means `convert`; zero observable change for existing clients incl. byte-identical `engines` | ✓ SATISFIED | D-04 normalization (`handlers.go:160-163`); golden pinning suite; all 3 public surfaces wired (D-05). |
| OPRN-02 | 57-02 | Registry resolves by `(operation, from, to)`; `pdf→pdf` sanitize does not collide with/leak into convert | ✓ SATISFIED | `opKey` triple map; `TestSanitizeIdentityPairDisjointFromConvert`, `TestRegisterPanicsOnTripleCollision`. |
| OPRN-03 | 57-01, 57-04 | Unsupported triple rejected 422 with machine-readable code, before S3 write and job-row creation | ✓ SATISFIED | Two-gate 422 (`handlers.go:421-455`); orchestrator LIVE gate: zero DB delta on rejection. |
| ROLE-01 | 57-03, 57-05 | `job_outputs` carries role via additive migration, default `primary`, existing rows correct without backfill | ✓ SATISFIED | `0015_operation_and_output_roles.sql`; `TestOutputRoleDefaultAppliesToPreExistingRows` (live PG18). |
| ROLE-02 | 57-01, 57-05, 57-06 | `GET /v1/jobs/{id}` returns every output with its role; single-output convert response stays byte-identical; closes `outs[0]` defect | ✓ SATISFIED | `handlers.go:892-974`; `TestGetJob_DownloadURLResolvesToPrimaryNotOrdinalZero` + D-18 inversion (orchestrator-reproduced). |

No orphaned requirements: `.planning/REQUIREMENTS.md`'s 5 Phase-57-mapped IDs (OPRN-01/02/03, ROLE-01/02) all appear in at least one plan's `requirements:` frontmatter.

### Anti-Patterns Found

None. Scanned all phase-touched production files (`convert.go`, `repo.go`, `jobs.go`, `handlers.go`, `formats_handlers.go`, `golden_pinning_test.go`, `worker.go`, `mcpserver/client.go`, `mcpserver/tools.go`, `frontend/src/api.ts`, `frontend/src/App.tsx`, the 0015 migration, all three worker `main.go`) for `TBD`/`FIXME`/`XXX`/`TODO`/`HACK`/`PLACEHOLDER`/"not yet implemented" — zero matches.

### Behavioral Spot-Checks (in addition to orchestrator's re-run of `go build`/`go vet`/`go test`/`npm test`)

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| OPRN-02 registry disjointness (real registry, real converter + synthetic sanitize pair) | `go test ./internal/convert/... -run 'TestSanitizeIdentityPairDisjointFromConvert\|TestRegisterPanicsOnTripleCollision\|TestReplaceOverwritesWithoutPanic\|TestOperationsDerivedFromRegistry\|TestOneOperationPerEngineClass\|TestClassesExcludesNonConvertOperations' -v` | 6/6 PASS | ✓ PASS |
| ROLE-01/D-13 against live PostgreSQL 18 | `DATABASE_URL=... go test ./internal/jobs/... -run 'TestOutputRoleDefaultAppliesToPreExistingRows\|TestAddOutputWithoutRoleReadsBackAsPrimary\|TestOutputRoleRejectsValueOutsideAllowlist\|TestMarkDoneRejects...\|TestMarkDoneAccepts...' -v` | 8/8 PASS | ✓ PASS |
| OPRN-01/03/ROLE-02 golden pinning + create/get-job gates | `go test ./internal/api/... -run 'TestPre57Golden...\|TestGetJob_...\|TestCreateJob_Operation...\|TestListFormats_...' -v` | 12/12 PASS | ✓ PASS |
| Error-code doc↔Go sync incl. `unsupported_operation` | `go test ./internal/errcode/... -v` | 8/8 PASS | ✓ PASS |
| Worker `Replace` (not `Register`) in all 3 configured-instance sites | `grep convert.Default.Replace/Register cmd/{av,ocr,pandoc}-worker/main.go` | 3/3 `Replace`, 0 `Register` | ✓ PASS |

### Human Verification Required

None. Every must-have in this phase is a backend/API/registry/DB behavior with a deterministic, programmatically re-runnable proof (unit test, live-DB integration test, or a byte-comparison golden test); the frontend surface change is a typed pass-through plus an error-message branch, both covered by `frontend/src/api.test.ts`/`errorcodes.test.ts` (37/37 green, orchestrator-confirmed) with no new visual/UX surface requiring human judgment.

### Gaps Summary

No gaps. All 9 derived truths (4 ROADMAP success criteria + 5 requirement IDs, cross-checked against the deep-dive scrutiny items requested) are verified against the live codebase and, where DB-dependent, against a live PostgreSQL 18 instance — not merely against SUMMARY.md narrative. The one genuine architectural seam found (`AddOutput` cannot write non-primary roles) is explicitly documented in the phase's own migration file as an intentional Phase-60 dependency, is exercised correctly by every Phase 57 test that needs it (via direct SQL), and does not fall under any of the five Phase 57 requirement IDs — it is noted here for the Phase 60 planner's benefit, not as a blocker.

---

_Verified: 2026-08-09T18:40:53Z_
_Verifier: Claude (gsd-verifier)_
