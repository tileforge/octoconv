---
phase: 58-synchronous-inspection
plan: 07
subsystem: api
tags: [go, chi, openapi, findings, quota, inspect, dos-mitigation]

requires:
  - phase: 58-synchronous-inspection (plan 02)
    provides: "internal/inspect findings vocabulary, three-value status, Coverage/ValidateReport union invariant"
  - phase: 58-synchronous-inspection (plan 04)
    provides: "AnalyzePDF, AnalyzeContainer (pdf.go, container.go)"
  - phase: 58-synchronous-inspection (plan 05)
    provides: "AnalyzeMedia, AnalyzeImage (media.go, image.go)"
  - phase: 58-synchronous-inspection (plan 06)
    provides: "internal/inspect.Tracker (byte+time work ceiling), internal/api inspectConcurrencyGate/inspectRateLimit, five Config boundary fields"
provides:
  - "internal/inspect.BuildReport: assembles every family analyzer's FamilyResult into one closed-vocabulary Report, completing not_applicable/format_not_applicable for every unclaimed code, computing the report-wide Coverage claim separately from any one Check's status"
  - "internal/api.availableOperations (INSP-04): registry-derived (operation, target, engine) enumeration with an honest upper-bound duration (measured/unmeasured provenance) and a live, deduplicated, read-only PreAdmit quota verdict"
  - "POST /v1/inspect: synchronous truth-about-a-file endpoint, mounted behind both D-06 gates, writes nothing, pins nothing"
  - "docs/openapi.yaml: /v1/inspect path plus InspectReport/InspectFinding/InspectCheck/InspectCoverage/InspectOperation schemas"
affects: [58-08-live-gate]

tech-stack:
  added: []
  patterns:
    - "Report-level codes (extension_mismatch, ole_cfb_*) are closed by the ASSEMBLER, never by a family analyzer that was never given the input (declaredFormat) needed to decide them"
    - "A Tracker's granted allowance is charged in full on Consume, not the analyzer's actual read count, when the analyzer does not report bytes-actually-read back to its caller -- the safe direction to be imprecise in"
    - "A courtesy context.WithTimeout is only meaningful where something context-aware actually observes it (the PreAdmit DB call); it is not wrapped around CPU-bound, iteration-capped analyzer work that would ignore it anyway"

key-files:
  created:
    - internal/inspect/report.go
    - internal/inspect/report_test.go
    - internal/api/estimate.go
    - internal/api/estimate_test.go
    - internal/api/inspect_handlers.go
    - internal/api/inspect_handlers_test.go
  modified:
    - internal/api/api.go
    - internal/api/routes.go
    - cmd/api/main.go
    - docs/openapi.yaml

key-decisions:
  - "Corrected pandoc's estimate provenance against .env.example (Phase 49 Plan 03 RTF-measured and lowered PANDOC_ENGINE_TIMEOUT to 65s, superseding this plan's own stale prose describing a 90s unmeasured fallback) -- per the plan's own D-16 instruction, the comment in .env.example is authoritative over the plan text"
  - "buildCoverage's tr-wide fallback flips Coverage.Complete/Reason when the shared Tracker is exhausted but does NOT add codes to TruncatedChecks in that branch -- guessing which codes a starved/skipped analyzer would have claimed risks misclassifying a genuinely not_applicable code as ceiling-truncated; TruncatedChecks stays exactly the codes whose own Check already carries a ceiling reason"
  - "Extracted BuildReport's final self-check into degradeCoverageOnInvariantViolation so TestBuildReportSelfCheckDegradesOnInvariantViolation can exercise it directly with a manufactured adversarial pair -- buildChecks' own merge logic structurally prevents a real invariant violation from ever reaching that code through the normal (defect-free) merge path"
  - "TestBuildReportFamilyCharacteristicKeysDoNotCollide asserts the ACTUAL closed set of intentional cross-family key reuses (page_count/page_count_reason between pdf.go/container.go; width/height(+reason) between image.go/media.go -- both pairs are format-mutually-exclusive) rather than a blanket empty-intersection requirement, which the real, already-shipped analyzer code from waves 2-3 does not satisfy"
  - "The D-06 mechanism-1 courtesy context.WithTimeout is wired to the one sub-call that is genuinely context-aware and I/O-bound (Repo.PreAdmit inside availableOperations), not wrapped around the CPU-bound family-analyzer dispatch, which is bounded by the shared Tracker plus each analyzer's own hard iteration caps instead (a context deadline cannot interrupt a loop that never checks it, per budget.go's own doc comment)"
  - "detectInspectFormat operates entirely over io.ReaderAt/io.NewSectionReader, never a sequentially-advancing io.Reader with a rest' variable to track -- inspect never forwards bytes to storage.Upload, so it is structurally free of handleCreateJob's T-31-02 rest-continuity concern"

requirements-completed: [INSP-01, INSP-03, INSP-04, INSP-05]

duration: ~2h (including full-context re-read of waves 1-3 after worktree base correction)
completed: 2026-08-10
---

# Phase 58 Plan 07: Synchronous Inspection Endpoint Summary

**`POST /v1/inspect` assembled from the phase's own vocabulary/analyzers/bounds: magic-byte format detection, a closed-vocabulary findings/checks report with honest coverage, a registry-derived operations enumeration carrying upper-bound duration provenance and a live deduplicated quota check, writes nothing, pins nothing a later job-creation request could use to skip its own fail-closed validation.**

## Performance

- **Duration:** ~2h (worktree base correction + full re-read of waves 1-3 + three tasks)
- **Started:** 2026-08-10 (session start, after worktree reset to f5fef5a)
- **Completed:** 2026-08-10T06:01:21+03:00 (Task 3 commit)
- **Tasks:** 3/3 completed
- **Files modified:** 10 (6 created, 4 modified)

## Accomplishments

- `internal/inspect.BuildReport` (Task 1): merges every family analyzer's `FamilyResult`, completes the closed 14-code vocabulary for every code no family claimed (`not_applicable`/`format_not_applicable`), and computes the report-wide `Coverage` claim -- kept structurally separate from any one `Check`'s own three-value status. `ValidateReport`'s union invariant is checked at the end of every `BuildReport` call and degrades `Coverage` safely (never panics) if it is ever violated.
- `internal/api.availableOperations` (Task 2, INSP-04): reads `convert.Default.OperationClasses()` for the detected format and calls `Repo.PreAdmit` **exactly once per distinct engine**, never once per `(operation, target)` pair -- proven by a dedicated dedup test. Each offer carries an honest upper-bound duration (the same per-engine timeout each worker already enforces on itself) tagged `measured`/`unmeasured`, and a live quota verdict that is `null`/`"unknown"` (never `true`) when the pre-admission check itself could not run.
- `POST /v1/inspect` (Task 3): accepts a multipart file, detects format by magic bytes only (same detector functions and order as `handleCreateJob`), dispatches to the one applicable family analyzer under a shared `Tracker`, closes the two report-level codes (`extension_mismatch`, `ole_cfb_*`) at the assembler, and returns the full report plus the `operations` enumeration. Mounted behind both D-06 gates in the documented order (own rate budget after auth, concurrency semaphore immediately before the handler). Never calls `Storage.Upload` or `Repo.Create`.
- `docs/openapi.yaml` gains the `/v1/inspect` path and five new schemas; `status` is a closed three-value enum, finding `code` stays an open string backed by `docs/FINDINGS.md`.

## Task Commits

1. **Task 1: Report assembly, closed findings vocabulary, honest coverage** - `cb7fbf8` (feat)
2. **Task 2: availableOperations, upper-bound duration provenance, live quota** - `bf203f9` (feat)
3. **Task 3: POST /v1/inspect endpoint, dispatcher, openapi contract** - `1910cc9` (feat)

**Plan metadata:** (this commit, docs: complete plan)

## Files Created/Modified

- `internal/inspect/report.go` - `Report`, `BuildReport`, merge/completion/coverage helpers
- `internal/inspect/report_test.go` - full `<behavior>` coverage + two named D-16-proof tests
- `internal/api/estimate.go` - `EstimateSource`, `engineEstimateSource`, `operationOffer`/`quotaOffer`, `availableOperations`
- `internal/api/estimate_test.go` - dedup proof, unknown-is-not-allowed proof, `.env.example` provenance sync test
- `internal/api/inspect_handlers.go` - `handleInspect`, `detectInspectFormat`, `dispatchInspectFamilies`, `cfbFamilyResult`, `extensionMismatchFamilyResult`
- `internal/api/inspect_handlers_test.go` - the eight named behavior tests plus the D-09 TOCTOU proof against the pre58 golden fixture
- `internal/api/api.go` - `Config.EngineTimeouts`/`Server.engineTimeouts`, defaulting in `NewServer`
- `internal/api/routes.go` - `/v1/inspect` route, mounted inside its own sub-group behind both gates
- `cmd/api/main.go` - reads the seven existing per-engine timeout env vars into `Config.EngineTimeouts` (no new env var names)
- `docs/openapi.yaml` - `/v1/inspect` path, `Inspect` tag, five new schemas

## Decisions Made

See `key-decisions` in frontmatter. The one worth restating in prose: **pandoc's estimate provenance in this plan's own action text was stale.** 58-07-PLAN.md describes pandoc's 90s timeout as an "unmeasured fallback" — that described `.env.example`'s state before Phase 49 Plan 03 RTF-measured it and lowered the value to 65s. `.env.example` (re-verified directly, line-by-line, before writing `engineEstimateSource`) now documents pandoc as measured, identically to audio/av/ocr. Per the plan's own D-16 instruction ("если комментарий противоречит этому списку — верен комментарий, а не план"), `engineEstimateSource[convert.EnginePandoc] = EstimateSourceMeasured`, and `TestEstimateSourceMatchesEnvExampleProvenance` mechanically re-verifies this against the real file on every test run so a future edit cannot silently drift.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Mechanical acceptance-check commands using `awk` range patterns did not work under this environment's BSD `awk`**
- **Found during:** Task 3, verifying the openapi acceptance commands (`awk '/^    InspectCheck:/,/^    [A-Z]/' ... | grep -cE ...`, `awk '/^    InspectFinding:/,/^    [A-Z]/' ... | grep -c 'enum:'`)
- **Issue:** BSD `awk`'s range-pattern operator ends a range immediately when the START line also matches the END pattern -- and every schema's own name line (`    InspectCheck:`, four spaces + uppercase) always matches `^    [A-Z]`. Verified with a minimal controlled input (`printf 'x\n    Foo:\n      a: 1\n    Bar:\n...' | awk '/^    Foo:/,/^    [A-Z]/'` prints only the `Foo:` line) — this is not specific to this file's content, it is a structural property of the awk range operator colliding with this project's own 4-space schema-naming convention. No `gawk` is installed in this environment to test whether GNU awk's range semantics differ.
- **Fix:** Verified the same intent (status enum has exactly 3 values; InspectFinding has no `enum:` at all) with `sed -n '/^    InspectCheck:/,/^    InspectCoverage:/p' docs/openapi.yaml | grep -cE '^ +- (clean|not_applicable|unknown)$'` → `3`, and `sed -n '/^    InspectFinding:/,/^    InspectCheck:/p' docs/openapi.yaml | grep -c 'enum:'` → `0`, using the real next-schema name as an unambiguous range boundary instead of a character class that collides with every schema's own name.
- **Files modified:** none (verification-only; the schema YAML itself was already correctly structured)
- **Verification:** commands above, both printed the expected values
- **Committed in:** 1910cc9 (Task 3 commit; the schema text was correct from the start, only the verification method needed correcting)

**2. [Rule 3 - Blocking] `grep -c` acceptance checks for `inspectConcurrencyGate`/`inspectRateLimit` in `routes.go` initially printed 2, not 1**
- **Found during:** Task 3, running the plan's own acceptance grep
- **Issue:** My first draft of the routing comment named both identifiers in prose immediately above the actual `r.Use(...)` call lines, so `grep -c` (counting matching LINES) found two lines each, not one.
- **Fix:** Reworded the comment to describe "inspect's own rate budget" and "the concurrency gate below" instead of repeating the literal Go identifiers, keeping the single `r.Use(s.inspectRateLimit())`/`r.Use(s.inspectConcurrencyGate)` lines as the only literal occurrences.
- **Files modified:** internal/api/routes.go
- **Verification:** `grep -c 'inspectConcurrencyGate' internal/api/routes.go` → `1`; `grep -c 'inspectRateLimit' internal/api/routes.go` → `1`
- **Committed in:** 1910cc9 (Task 3 commit)

---

**Total deviations:** 2 (1 verification-method correction, no production-code change; 1 self-inflicted acceptance-grep false positive caught and fixed before committing)
**Impact on plan:** No scope creep, no behavior change. Both are documentation/verification-precision fixes caught by running the plan's own acceptance commands before committing, exactly as the plan's own doctrine requires.

## Issues Encountered

- **Worktree was several commits ahead of, then found to actually be BEHIND, the expected phase-58 base.** `git merge-base HEAD f5fef5a...` returned the worktree's own HEAD (`1b91e9f`, from an unrelated later-phase branch state), proving `f5fef5a` was NOT an ancestor of the starting HEAD — the mandatory `<worktree_branch_check>` correctly detected this and `git reset --hard f5fef5a0e1c838dd3d7e4857ffdd8253f9d838f8` was run before any source was read, per the setup instructions. No uncommitted work was lost (`git status --short` was clean before the reset).
- **Real, intentional cross-family characteristic-key reuse in the already-shipped wave 2/5 code contradicted the plan's literal "empty intersection" instruction for `TestBuildReportFamilyCharacteristicKeysDoNotCollide`.** `pdf.go`/`container.go` both use `page_count`/`page_count_reason`; `image.go`/`media.go` both use `width`/`width_reason`/`height`/`height_reason`. Both pairs are format-mutually-exclusive families (a PDF never also runs `AnalyzeContainer`; an image never also runs `AnalyzeMedia`), so this is real, intentional key sharing across formats that never coexist in one report — not a defect from parallel authorship. The test was written to assert exactly this closed set of intentional reuses is unchanged, and that every OTHER pairing has zero intersection, rather than a blanket empty-intersection assertion that would fail against the real, correct, already-committed code.

## User Setup Required

None — no external service configuration required. `cmd/api/main.go` reads seven ALREADY-DOCUMENTED `.env.example` variables (`ENGINE_TIMEOUT`, `DOCUMENT_ENGINE_TIMEOUT`, `HTML_ENGINE_TIMEOUT`, `AUDIO_ENGINE_TIMEOUT`, `AV_ENGINE_TIMEOUT`, `OCR_ENGINE_TIMEOUT`, `PANDOC_ENGINE_TIMEOUT`) that every worker process already reads; `git diff HEAD -- .env.example` is empty, confirming no new variable was introduced.

## Next Phase Readiness

- `POST /v1/inspect` is live, mounted, and documented. Plan 58-08 (the live k8s gate, `autonomous: false`) can now calibrate `InspectMaxWorkBytes`/`InspectMaxWorkDuration`/`InspectMaxConcurrent`/`RATE_LIMIT_INSPECT_RPM` against this real endpoint under load, per the residual-measurement list Plan 58-06's own SUMMARY already recorded.
- No blockers. `go test ./... -count=1 -race` is green across the entire repository. `go vet ./...` is clean. `gofmt -l internal/api internal/inspect cmd/api` is empty. `git diff --stat -- go.mod go.sum` is empty.

## Verification Command Outputs (pasted, as required)

```
$ go test ./... -count=1 -race
ok  	github.com/apaderin/octoconv/cmd/api	2.899s
ok  	github.com/apaderin/octoconv/internal/api	3.936s
ok  	github.com/apaderin/octoconv/internal/inspect	5.507s
... (every other package: ok or [no test files])

$ go vet ./...
(no output)

$ gofmt -l internal/api internal/inspect cmd/api
(no output)

$ go test ./internal/errcode/... -count=1
ok  	github.com/apaderin/octoconv/internal/errcode	1.313s

$ go test ./internal/api/ -run 'TestPre58Golden' -count=1
ok  	github.com/apaderin/octoconv/internal/api	0.501s

$ git rev-list --count HEAD -- internal/api/testdata/pre58_healthz.golden.json
1
$ git rev-list --count HEAD -- internal/api/testdata/pre58_formats.golden.json
1
$ git rev-list --count HEAD -- internal/api/testdata/pre58_createjob_rejections.golden.json
1

$ git diff --stat -- go.mod go.sum
(no output)
```

## Inversion Outputs (D-16, exact failure text)

**Task 1, inversion 1 (fallback removal):**
```
ValidateReport: report invariant violated: in both findings and checks: [];
missing from both: [ooxml_macro zip_bomb_shape ole_cfb_legacy ole_cfb_encrypted
ole_cfb_unclassified extension_mismatch pdf_javascript pdf_action_dict
pdf_launch_action pdf_embedded_file pdf_encrypted external_reference];
duplicated in checks: []
```

**Task 1, inversion 2 (structural reason treated as ceiling):**
```
Coverage.Complete = false, want true (structural blindness must not select the report)
```

**Task 2, inversion 1 (dedup removed):**
```
engine image: PreAdmit called 4 times, want exactly 1
engine ocr: PreAdmit called 3 times, want exactly 1
```

**Task 2, inversion 2 (unknown treated as allowed):**
```
Allowed = true, want nil
```

**Task 2, inversion 3 (html mismarked measured):**
```
engine html (HTML_ENGINE_TIMEOUT): .env.example measured=false, engineEstimateSource says measured=true
```

**Task 3, inversion 1 (select by declared_format):**
```
Format = "jpg", want png
expected extension_mismatch finding; findings=[]
```

**Task 3, inversion 2 (general upload ceiling):**
```
/v1/inspect status = 200, want 413; body={"format":"png",...}
```

**Task 3, inversion 3 (D-09 main proof — artificial cross-endpoint pinning):**
```
status = 202, want 422 (golden macro_detected)
```
After full revert: `git diff -- internal/api/handlers.go` printed nothing (confirmed empty).

## D-17: Phase-Goal Verification — Every Claim This Report Makes, and What Could Make It Wrong

Per 58-CONTEXT.md D-17 and this plan's own `<verification>` section, every statement `POST /v1/inspect`'s response makes about a file, what backs it, and when it can be wrong:

| Claim | Backed by | Can be wrong when |
|---|---|---|
| `format` (magic-byte detected) | The SAME sniffer functions/order as `handleCreateJob` (`convert.Sniff`, `SniffContainer`, `LooksLikeHTML`/`LaTeX`/`Text`, `IsOLECFB`, `SniffVideo`, `SniffAudio`) | Never influenced by the filename extension (SC1) — cannot be "wrong" in the way `handleCreateJob`'s mismatch check exists to catch; can be `""` for genuinely unrecognized content, which is itself the correct answer, not an error |
| `declared_format` | `path.Ext(filename)` + `convert.NormalizeFormat` | Purely a client-supplied string; carries no truth claim about the file itself, only about what the client called it |
| `characteristics.*` (page_count, width/height, duration_seconds, ...) | The four family analyzers, each bounded by `readWindow`/`maxTokenScanIterations`-class hard caps plus the shared `Tracker` | A value is `nil` with a `_reason` key whenever the analyzer's own bounded window didn't cover enough of the file (`work_ceiling_bytes`), the parser for that characteristic doesn't exist yet (`parser_unavailable`), or the structure is present but didn't parse (`malformed_input`) — NEVER silently wrong, only honestly absent |
| `findings[].code` (ooxml_macro, pdf_javascript, ...) | Real, bounded token/structure scans reusing `docsniff.go`'s `HasMacro`/`TotalUncompressed` and `pdf.go`'s literal-token scan | A POSITIVE finding is a real observation and is never weakened by structural blindness (pdf.go's own doc comment) — but a token scan proves PRESENCE, not absence of alternate spellings/obfuscation outside the closed token table; this is a bounded byte-scan, not a full semantic parse |
| `checks[].status = "clean"` | The absence of a Finding for that code, from a family analyzer that ran to completion within its budget | Can be WRONG in exactly one documented way: PDF's `/ObjStm` compression-signal detector downgrades ALL PDF risk-code checks to `"unknown"`/`compressed_object_streams` whenever the signal is present — this is the one place a naive reading might expect `"clean"` but the file's own analyzer refuses to claim it (D-02a, by design) |
| `checks[].status = "unknown"` | `Reason` names exactly why (structural blindness, own budget exhaustion, or malformed input) | Is itself the honest, correct answer for its stated Reason — cannot be "wrong" by construction, only potentially avoidable with a future, unbuilt parser |
| `coverage.complete` | `Tracker.Exhausted()` plus a per-Check scan for the two ceiling Reasons | ONLY reflects THIS SERVICE's own byte/time ceiling, never structural blindness about the file — could theoretically under-report a defect if a future family analyzer's own internal cap silently truncates without ever reporting a ceiling Reason (would need to be caught by that analyzer's own tests, out of this plan's scope) |
| `operations[].engine`/`target` | `convert.Default.OperationClasses()`, the same registry-derived source `GET /v1/formats` already uses | Reflects exactly what THIS build has registered; cannot claim an engine this build does not compile in |
| `operations[].estimated_max_seconds`/`estimate_source` | The literal per-engine timeout value each worker enforces on itself, tagged from `.env.example`'s own comments, mechanically re-verified by `TestEstimateSourceMatchesEnvExampleProvenance` | Is an upper bound, NEVER a real estimate (D-10) — can be a poor bound if a future worker change raises its own timeout without a corresponding `.env.example` comment update, which the sync test would then catch as a provenance mismatch, not silently drift |
| `operations[].quota.allowed` | A LIVE `Repo.PreAdmit` call, deduplicated per distinct engine | Explicitly "as of this request", NEVER a guarantee (`PreAdmit` is not linearizable under a concurrent burst, D-12) — `allowed: true` can become false by the time the client actually calls `POST /v1/jobs` |

**Every path by which untrusted bytes enter the API process, found by grepping `FormFile` across `internal/api` (excluding test files):**

```
internal/api/handlers.go:187:   file, header, err := r.FormFile(formFieldFile)   // POST /v1/jobs
internal/api/inspect_handlers.go:136: file, header, err := r.FormFile(formFieldFile) // POST /v1/inspect
```

Exactly two: `POST /v1/jobs` (existing, pre-Phase-58) and `POST /v1/inspect` (this plan). No third path exists.

## Self-Check: PASSED

- FOUND: internal/inspect/report.go
- FOUND: internal/inspect/report_test.go
- FOUND: internal/api/estimate.go
- FOUND: internal/api/estimate_test.go
- FOUND: internal/api/inspect_handlers.go
- FOUND: internal/api/inspect_handlers_test.go
- FOUND commit: cb7fbf8
- FOUND commit: bf203f9
- FOUND commit: 1910cc9

---
*Phase: 58-synchronous-inspection*
*Completed: 2026-08-10*
