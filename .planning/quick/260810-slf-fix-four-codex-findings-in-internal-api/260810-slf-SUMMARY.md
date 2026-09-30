---
phase: quick-260810-slf
plan: 01
subsystem: api
tags: [go, http, security, dos-hardening, zip, archive-zip, semaphore, quota]

requires:
  - phase: 58-synchronous-inspection
    provides: "POST /v1/inspect handler, internal/inspect analyzers, the D-06 four-mechanism defense-in-depth (work ceiling, concurrency semaphore, dedicated healthz listener, own rate budget)"
provides:
  - "Structural pre-parse bound on ZIP central-directory size/entry-count, checked from the EOCD record before archive/zip.NewReader ever runs"
  - "inspectSemaphore.acquireOrReject: slot acquisition moved from outer middleware to inside handleInspect, after body intake"
  - "Work-ceiling context for Repo.PreAdmit created after body intake, not at handler entry"
  - "estimateSourceFor: per-request provenance downgrade when a measured engine's configured timeout diverges from its documented measured value"

affects: [58-synchronous-inspection, "phase 60 (sanitize) if it reuses estimate.go's provenance pattern"]

tech-stack:
  added: []
  patterns:
    - "Structural pre-parse bound: peek a format's own trailer/header metadata (ZIP EOCD) before invoking the stdlib parser that would walk the whole structure unconditionally"
    - "Sentinel Detected-format value (inspectZipTooLargeFormat, mirroring the existing inspectOLECFBFormat pattern) to route a declined-to-parse upload through the honest unknown/work_ceiling_bytes path instead of not_applicable or a silent misclassification"
    - "acquireOrReject: a semaphore exposes both an outer-middleware wrapper AND a direct acquire/release pair, so a caller can move the acquisition point inside its own handler without duplicating the non-blocking-select-plus-429 logic"

key-files:
  created: []
  modified:
    - internal/api/inspect_handlers.go
    - internal/api/inspect_handlers_test.go
    - internal/api/inspectgate.go
    - internal/api/routes.go
    - internal/api/api.go
    - internal/api/estimate.go
    - internal/api/estimate_test.go

key-decisions:
  - "Task 1: closed the ZIP central-directory DoS vector with a structural pre-parse bound (EOCD-peek, <=4096 entries / <=4 MiB directory) rather than threading a precomputed convert.ContainerResult into inspect.AnalyzeContainer, because this quick task's parallel-execution boundary confines changes to internal/api/* (a sibling agent owns internal/inspect/*). The literal 'exactly one SniffContainer call system-wide' framing in the plan's own acceptance criteria is not fully met for uploads WITHIN the new bound (detectInspectFormat's own call and AnalyzeContainer's internal one both still run) -- but that residual double parse is now provably bounded, never the unbounded attacker-controlled work the finding described, which is fully closed."
  - "Task 2: chose option (a) (acquire the concurrency slot after body intake) over option (b) (a body-read deadline) for the semaphore fix, per the plan's own preference ('прямее и не добавляет ещё одного таймаута'). Verified this preserves the D-06a property (a held slot still bounds concurrent Repo.PreAdmit calls against the shared pool) because PreAdmit still runs while the slot is held -- only body transfer, which never touches the pool or a parser, no longer does. No cmd/api/main.go body-read timeout was added: a server-wide http.Server.ReadTimeout would also apply to POST /v1/jobs' own legitimately large/slow uploads."
  - "Task 2: the work-ceiling context for Repo.PreAdmit is created immediately before availableOperations is called (after body intake AND after the CPU-bound family-analyzer dispatch), not at handler entry -- its only functioning consumer is that one call."
  - "Task 3: chose option (б) (conservative unmeasured-relabel) over option (а) (threading provenance through Config as a new field) for the estimate-provenance fix, per the plan's own explicit invitation to pick either. estimateSourceFor compares the actual reported seconds against engineMeasuredDefaultSeconds (the exact .env.example figure each measured engine's documented RTF run produced); any divergence -- including an engine with no measured provenance at all -- downgrades to unmeasured."

patterns-established:
  - "When a stdlib parser (archive/zip.NewReader) unconditionally walks an entire declared structure before any caller-supplied cap can apply, check the structure's own trailer/header metadata directly (EOCD) BEFORE invoking that parser, not after."
  - "A semaphore/gate type should expose both a full-handler middleware wrapper and a direct acquire/release pair sharing the same core logic, so the acquisition point can move without duplicating the busy-response shape."

requirements-completed: [PR9-F7-ZIP-DIRECTORY-UNBOUNDED, PR9-F8-SEMAPHORE-BEFORE-BODY, PR9-F9-PREADMIT-CONTEXT, PR9-F10-PROVENANCE-OVERRIDE]

duration: 55min
completed: 2026-08-10
audit_acknowledged:
  milestone: v1.15
  at: 2026-09-30
  status: unknown
---

# Quick Task 260810-slf: Four Codex PR #9 Findings in internal/api Summary

**ZIP central-directory parse now structurally bounded before `zip.NewReader` runs; inspect's concurrency slot guards parsing (not body transfer); PreAdmit gets a fresh context after body intake; estimate provenance downgrades to unmeasured on operator override.**

## Performance

- **Duration:** 55 min
- **Started:** 2026-08-10T20:56:00Z
- **Completed:** 2026-08-10T21:51:00Z
- **Tasks:** 3
- **Files modified:** 7

## Accomplishments

- **T-SLF-01 closed structurally.** `zipDirectoryExceedsStructuralBound` peeks the ZIP End-Of-Central-Directory record's own declared entry count and central-directory byte size (at most 64 KiB of `ReadAt`, zero decompression) BEFORE `convert.SniffContainer`/`archive/zip.NewReader` ever runs. A ZIP-shaped upload exceeding the bound (`>4096` entries or `>4 MiB` declared directory) is routed to a new `zip_directory_too_large` sentinel format: the three container-domain codes plus `extension_mismatch` report `unknown`/`work_ceiling_bytes` -- never `clean`, never 500 -- and `Coverage.Complete` is honestly marked `false`.
- **T-SLF-02 closed.** The D-06 mechanism-2 concurrency slot is now acquired inside `handleInspect`, via the new `inspectSemaphore.acquireOrReject`, immediately AFTER `ParseMultipartForm` completes -- not as outer middleware wrapping the whole handler. `routes.go` no longer mounts a concurrency gate ahead of `POST /v1/inspect`. Proven with a slow-body test: 3 concurrent clients trickling an incomplete multipart body (exceeding `InspectMaxConcurrent=2`) do not block a legitimate fast request.
- **T-SLF-03 closed.** The work-ceiling `context.WithTimeout` that bounds `Repo.PreAdmit` is now created after body intake and family-analyzer dispatch, not at handler entry. Proven with a delayed-body test (200ms delay vs. a 50ms `InspectMaxWorkDuration`): every `operations[].quota.allowed` is non-nil after the slow-but-legitimate upload.
- **T-SLF-04 closed.** `estimateSourceFor` compares the number actually being reported against `engineMeasuredDefaultSeconds` (742/753/744/65s for audio/av/ocr/pandoc); a divergence -- an operator override -- downgrades `estimate_source` from `measured` to `unmeasured`.

## Task Commits

1. **Task 1: ZIP central directory — structural bound before parse** - `f086aca` (fix)
2. **Task 2: semaphore guards parsing, PreAdmit gets fresh context** - `d458e52` (fix)
3. **Task 3: estimate provenance downgrades on override** - `a96d62f` (fix)

**SUMMARY commit:** (this file, committed separately below)

## Files Created/Modified

- `internal/api/inspect_handlers.go` - `zipDirectoryExceedsStructuralBound`, `zipDirectoryCeilingFamilyResult`, `inspectZipTooLargeFormat` sentinel; `handleInspect` restructured: body intake -> concurrency-slot acquisition -> detect/dispatch -> work-ceiling context -> `availableOperations`
- `internal/api/inspect_handlers_test.go` - ZIP-bound tests (over-bound/within-bound/direct-function/inversion), slow-body concurrency test, fresh-context-after-slow-body test, `blockingReader`/`delayedReader`/`incompleteMultipartPrefix`/`fakeContextAwarePreAdmitRepo` test helpers
- `internal/api/inspectgate.go` - `inspectSemaphore.acquireOrReject` (new), `middleware` refactored to call it
- `internal/api/routes.go` - removed the `inspectConcurrencyGate` middleware mount ahead of `POST /v1/inspect`; updated the group's own doc comment
- `internal/api/api.go` - removed the now-unused `inspectConcurrencyGate` method
- `internal/api/estimate.go` - `engineMeasuredDefaultSeconds`, `estimateSourceFor`; `availableOperations` now calls it instead of indexing `engineEstimateSource` directly
- `internal/api/estimate_test.go` - provenance-override tests, `.env.example`-cross-check test for the new measured-default map

## Decisions Made

See `key-decisions` in frontmatter above. In one line each:
- Task 1: EOCD-peek structural bound at the `internal/api` call site (not inside `internal/inspect`), because of this quick task's file-ownership boundary with the sibling agent.
- Task 2: option (a) — acquire the slot after body intake, not a body-read deadline.
- Task 3: option (б) — conservative unmeasured-relabel on any divergence from the documented measured value.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 4-adjacent, disclosed constraint-driven scope adjustment] Task 1's "exactly one SniffContainer call" framing not fully literal**
- **Found during:** Task 1
- **Issue:** The plan's acceptance criteria (`grep -c 'SniffContainer' internal/api/inspect_handlers.go` prints `1`, "результат первого вызова должен передаваться дальше... internal/inspect/container.go принимает готовый результат") implies either `AnalyzeContainer`'s signature changes to accept a precomputed `convert.ContainerResult`, or `SniffContainer` itself gains the structural pre-check — both require touching `internal/inspect/container.go` or `internal/convert/docsniff.go`. The orchestrator's explicit parallel-execution instruction restricts this agent to `internal/api/*` and `cmd/api/main.go` only (the sibling agent owns `internal/inspect/*`), which takes precedence over the plan's file list.
- **Resolution:** Implemented the structural bound entirely at the `internal/api` call site: `zipDirectoryExceedsStructuralBound` peeks the EOCD directly, and when it trips, `AnalyzeContainer`/`SniffContainer`/`zip.NewReader` are never called at all (closing the actual security concern). For uploads WITHIN the bound, `detectInspectFormat`'s own `SniffContainer` call (kept, satisfying the literal grep count of 1) and `AnalyzeContainer`'s internal one both still run — a residual, but now provably bounded (≤4096 entries, ≤4 MiB directory) double parse, not the unbounded attacker-controlled work the finding described.
- **Files modified:** internal/api/inspect_handlers.go
- **Verification:** `TestInspectZipDirectoryOverBoundReportsUnknownNot500` proves the oversized-archive path never resolves to the real office format and never reaches `clean`; `TestInspectZipDirectoryWithinBoundUnaffected` proves normal-sized uploads are byte-for-byte unaffected.
- **Committed in:** f086aca

---

**Total deviations:** 1 disclosed, constraint-driven scope adjustment (not a Rule 1-3 bug fix; recorded per the Rule 4 "ask/document architectural tension" spirit since it touches the plan's literal acceptance wording, even though no user round-trip was needed — the orchestrator's own instruction resolved the tension unambiguously).
**Impact on plan:** The security property (T-SLF-01: no unbounded ZIP central-directory parse) is fully closed. The "single call system-wide" nicety is partially unmet only for the already-small, already-bounded happy path, which was never the attack surface.

## Issues Encountered

- First draft of `TestZipDirectoryExceedsStructuralBoundNeverParsesOversizedArchive` asserted the wrong expected entry count (forgot the fixture's own `word/document.xml` root part adds one entry beyond the injected junk-entry count) — caught immediately by the test itself failing, fixed before commit.
- `gofmt` reformatted `estimate.go`'s struct-literal field alignment after the `estimateSourceFor` field was added with a longer name than its neighbors; applied and verified clean.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- All four PR #9 findings in `internal/api` are closed with tests proving the property (not just the intent), each independently verified by inversion (D-16): temporarily reverting each fix reproduces the exact regression the corresponding test catches, then reverted before commit.
- `go build ./... && go vet ./...` clean; `gofmt -l internal/api cmd/api` empty; `go test ./... -count=1 -race` green across the whole repository; `git diff --stat -- go.mod go.sum` empty (zero new dependencies); all three `pre58_*.golden.json` fixtures still at exactly 1 commit each.
- The live operator gate (SC6), deliberately withheld from Phase 58's close until these findings landed, can now be run against this code without discarding evidence.
- Residual, disclosed scope gap: `internal/inspect/container.go`'s own `AnalyzeContainer` still calls `convert.SniffContainer` (and thus `archive/zip.NewReader`) a second time for uploads within the new structural bound — cheap and provably bounded, not a security concern, but a future pass touching `internal/inspect/*` could thread `detectInspectFormat`'s already-computed `ContainerResult` through instead, closing the plan's literal "exactly once" framing completely.

---
*Quick task: 260810-slf*
*Completed: 2026-08-10*

## Self-Check: PASSED

All 7 modified files verified present on disk; all 3 task commits (f086aca, d458e52, a96d62f) verified present in `git log --oneline --all`.
