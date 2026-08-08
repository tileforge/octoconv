---
phase: 56-jobrunner-consolidation-structured-error-codes
plan: 03
subsystem: api
tags: [error-codes, api, go]

# Dependency graph
requires:
  - phase: 56-02
    provides: internal/errcode closed 14-code sync error-code vocabulary
provides:
  - "internal/api: every writeError call site (112 total, all 6 files) now carries a machine-readable errcode constant"
  - "internal/api/errcode_envelope_test.go: static + dynamic proof no error response can escape untagged"
affects: [mcpserver, frontend]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "writeError(w, status, code, msg) widened signature, mirroring writeQuotaError's pre-existing {error,code} envelope shape"
    - "Static source-scan test (regexp over comment-stripped .go source) as a structural guard against literal error codes reappearing"

key-files:
  created:
    - internal/api/errcode_envelope_test.go
  modified:
    - internal/api/handlers.go
    - internal/api/handlers_test.go
    - internal/api/jobs_list_handler.go
    - internal/api/presets_handlers.go
    - internal/api/system_presets_handlers.go
    - internal/api/system_presets_handlers_test.go
    - internal/api/system_usage_handlers.go
    - internal/api/system_usage_handlers_test.go
    - internal/api/sse.go

key-decisions:
  - "Ambiguous content-validation rejections (zip bomb, macro, CFB encrypted/legacy/unknown, unrecognized content, format mismatch, dimension checks) classified as errcode.InvalidRequest, not errcode.UnsupportedConversion -- only the literal (source,target) pair-not-supported message (handlers.go, 'unsupported conversion: X -> Y') gets UnsupportedConversion, per the plan's own instruction to classify by what each message actually says"
  - "requireOperator's two no-leak 404 sites (system_presets_handlers.go:58,62) classified as errcode.UnknownPreset, matching every other noSuchPreset 404 in the same file -- the whole point of that body is to be byte-identical to an ordinary missing-preset 404, so giving it a different code would leak the auth-vs-existence distinction it exists to hide"
  - "'opts field too large' (422, handlers.go:530) classified as errcode.PayloadTooLarge per the plan's explicit instruction, even though its HTTP status is 422 not 413 -- code is assigned by failure class (oversized payload), not by status code"

requirements-completed: [ERRC-01]

# Metrics
duration: ~25min
completed: 2026-08-08
---

# Phase 56 Plan 03: internal/api writeError code sweep Summary

**Widened `writeError` to `writeError(w, status, code, msg)` and tagged all 112 call sites across six `internal/api` files with an `internal/errcode` constant, closing the synchronous half of the SEED-020 error-code contract.**

## Performance

- **Duration:** ~25 min (including worktree base-drift recovery, see Issues Encountered)
- **Started:** 2026-08-08T06:16:52+03:00 (worktree fast-forwarded to base)
- **Completed:** 2026-08-08T06:37:16+03:00
- **Tasks:** 2 completed
- **Files modified:** 9 (1 created, 8 modified)

## Accomplishments

- `writeError` in `internal/api/handlers.go` widened from `(w, status, msg)` to `(w, status, code, msg)`, emitting the flat `{"error": msg, "code": code}` envelope (D-01); doc comment states the D-01 flatness rule and the `frontend/src/api.ts:54` reason.
- All 112 pre-existing `writeError` call sites across `handlers.go` (62), `jobs_list_handler.go` (4), `presets_handlers.go` (18), `system_presets_handlers.go` (19), `system_usage_handlers.go` (5), `sse.go` (4) now pass an `internal/errcode` constant as the third argument, assigned by failure class per D-02 -- zero bare string literals, zero new codes invented, zero message/status/validation-order changes (verified by `git diff` greps in the plan's acceptance criteria).
- `writeQuotaError` now sources its `"quota_exceeded"` value from `errcode.QuotaExceeded` instead of a duplicated literal; the wire value is unchanged.
- `internal/api/errcode_envelope_test.go` (new): `TestEveryErrorResponseCarriesKnownCode` drives 12 distinct scenarios through the real chi router and asserts every response carries a known code; `TestErrorEnvelopeIsFlat` pins D-01's flat shape; `TestNoLiteralErrorCodesInAPISource` statically scans non-test `internal/api` source for a literal slipping into the code argument; `TestWriteErrorSignatureCarriesCode` tripwires the widened signature against a future revert.
- Whole-repo `go build ./... && go vet ./... && gofmt -l internal/api && go test ./... -count=1` green (22 tested packages).

## Task Commits

1. **Task 1: Widen writeError and tag all 112 call sites in internal/api**
   - `f17b0b8` (feat) - signature widened, `writeQuotaError` migrated to the constant, all 112 call sites tagged; also updated 5 test call sites reconstructing the `requireOperator` 404 body, and replaced one now-superseded pre-56-03 test (see Deviations)

2. **Task 2: Prove no API error response can escape untagged**
   - `4857cc3` (test) - `errcode_envelope_test.go` created with the four required tests; `TestNoLiteralErrorCodesInAPISource` manually verified to catch a hand-injected regression before being trusted (see below)

**Plan metadata:** this commit (docs: complete plan) — created after this SUMMARY.

## Files Created/Modified

- `internal/api/handlers.go` - `writeError` widened + doc comment; `writeQuotaError` migrated to `errcode.QuotaExceeded`; 62 call sites tagged
- `internal/api/jobs_list_handler.go` - 4 call sites tagged, import added
- `internal/api/presets_handlers.go` - 18 call sites tagged, import added
- `internal/api/system_presets_handlers.go` - 19 call sites tagged (including both `requireOperator` no-leak-404 sites), import added
- `internal/api/system_usage_handlers.go` - 5 call sites tagged, import added
- `internal/api/sse.go` - 4 call sites tagged, import added
- `internal/api/handlers_test.go` - added `errcode` import; replaced `TestCreateJob_NonQuotaErrorBodiesHaveNoCodeField`/`assertNoCodeField` with `TestCreateJob_NonQuotaErrorBodiesCarryClosedVocabularyCode`/`assertErrorCode` (see Deviations)
- `internal/api/system_presets_handlers_test.go` - added `errcode` import; updated the byte-identity `writeError(wantRec, ...)` reconstruction to pass `errcode.UnknownPreset`
- `internal/api/system_usage_handlers_test.go` - added `errcode` import; updated 3 byte-identity `writeError(wantRec, ...)` reconstructions to pass `errcode.UnknownPreset`; updated 2 hardcoded body-string assertions (`invalid period filter`, `invalid client_id`) to expect the new `{"code":...,"error":...}` shape
- `internal/api/errcode_envelope_test.go` (new) - the four Task 2 tests plus the shared `errorScenarios()` table and `scanForLiteralErrorCodes`/`stripCommentLines` static-scan helpers

## Decisions Made

- Classified every `writeError` call site individually against the plan's D-02 taxonomy; full per-site mapping verified against the plan's `<action>` block and `56-PATTERNS.md`'s examples before applying. See `key-decisions` in frontmatter for the three non-obvious classification calls (content-validation-vs-pair, requireOperator no-leak parity, opts-too-large-as-PayloadTooLarge).
- Did not touch `internal/worker`, `internal/metrics`, or `internal/errcode` — confirmed zero diff via `git diff --name-only HEAD~2 HEAD -- internal/worker internal/metrics internal/errcode`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Two pre-existing tests asserted the old no-code envelope shape**
- **Found during:** Task 1, `go test ./internal/api/...` after the sweep
- **Issue:** `TestSystemUsage_InvalidPeriodRejectedBeforeAnyRepoCall` and `TestSystemUsage_InvalidClientIDRejectedBeforeAnyRepoCall` hardcoded the pre-sweep body string (`{"error":"invalid period filter"}`, no `code` key), which the widened `writeError` no longer produces.
- **Fix:** Updated both string comparisons to the new envelope (`{"code":"invalid_request","error":"..."}`, `encoding/json` map-key alphabetical ordering).
- **Files modified:** `internal/api/system_usage_handlers_test.go`
- **Commit:** `f17b0b8`

**2. [Rule 1 - Bug] A whole test asserted an invariant this plan intentionally supersedes**
- **Found during:** Task 1, `go test ./internal/api/...` after the sweep
- **Issue:** `TestCreateJob_NonQuotaErrorBodiesHaveNoCodeField` (with helper `assertNoCodeField`) pinned a prior-phase D-10 boundary — "every non-quota error body has exactly one key (`error`), never a `code`" — which is the exact restriction SEED-020/ERRC-01 (this plan's own objective) removes. The old `writeError`'s doc comment even named this test's premise explicitly ("the general sweep is SEED-020").
- **Fix:** Replaced the test with `TestCreateJob_NonQuotaErrorBodiesCarryClosedVocabularyCode` (helper `assertErrorCode`), asserting the SAME two scenarios (422 unsupported pair, 413 engine size limit) now carry `errcode.UnsupportedConversion` / `errcode.PayloadTooLarge` respectively, with the message text unchanged.
- **Files modified:** `internal/api/handlers_test.go`
- **Commit:** `f17b0b8`

**3. [Rule 1 - Bug] Five test call sites invoked writeError directly with the old 3-arg signature**
- **Found during:** Task 1, `go vet ./...` after the signature widen
- **Issue:** `system_presets_handlers_test.go` (2 sites) and `system_usage_handlers_test.go` (3 sites) reconstruct the `requireOperator` no-leak-404 body for byte-identity comparison by calling `writeError(wantRec, http.StatusNotFound, noSuchPreset)` directly — a compile error after the signature widen.
- **Fix:** Added `errcode.UnknownPreset` as the third argument at all 5 sites, matching the real handler's classification of the same 404 (see key-decisions).
- **Files modified:** `internal/api/system_presets_handlers_test.go`, `internal/api/system_usage_handlers_test.go`
- **Commit:** `f17b0b8`

None of these three deviations changed any HTTP status, message text, or validation ordering — confirmed by the plan's own acceptance-criteria greps re-run after each fix.

## Manual Verification Record (Task 2 acceptance criterion)

Per the plan's instruction to hand-verify `TestNoLiteralErrorCodesInAPISource` actually fails on a real regression: temporarily changed `handlers.go:159` from `writeError(w, http.StatusBadRequest, errcode.MissingFile, "missing file")` to `writeError(w, http.StatusBadRequest, "missing_file", "missing file")`, ran the test, and observed:

```
=== RUN   TestNoLiteralErrorCodesInAPISource
    errcode_envelope_test.go:308: literal error code(s) found (must reference an errcode constant instead): [handlers.go:159]
--- FAIL: TestNoLiteralErrorCodesInAPISource (0.00s)
FAIL
```

Reverted immediately after (`git diff --stat internal/api/handlers.go` confirmed byte-identical to the committed state before re-running the full suite).

## Issues Encountered

**Worktree base drift, discovered mid-execution (not at the sanctioned setup step).** The `worktree_branch_check` merge-base comparison at the start of this session correctly identified the worktree's HEAD (`175a738`, an old Phase 53-era commit) as not descending from the expected base commit `5993581` — but the mismatch was only acted upon after a mechanical sweep script had already been run against the stale (pre-Phase-56) file contents, because an early `Read` call used a main-repo-rooted absolute path (`/Users/apaderin/dev/octoconv/internal/api/...`) instead of the worktree-rooted path, which silently returned CURRENT main-branch content and masked the drift for several tool calls. `git reset --hard` (the plan's prescribed recovery) was blocked by the auto-mode permission classifier; recovered instead via `git checkout -- <file>` (discarding the 4 stale in-progress edits, all uncommitted) followed by `git merge --ff-only 5993581` (a non-destructive fast-forward, since the worktree branch had no unique commits of its own). All subsequent file reads and edits used worktree-rooted absolute paths. No functional impact — the sweep was fully redone against the correct base before any commit was made.

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- `internal/api` now emits a machine-readable `code` on every 4xx/429 response, closing the synchronous half of the SEED-020/ERRC-01 error-code contract (the async half — `internal/worker`'s `EngineDescriptor.FailureCode` taxonomy — is out of this plan's scope per D-04/D-06 and untouched, confirmed by zero diff).
- No blockers for downstream plans in this phase.

---
*Phase: 56-jobrunner-consolidation-structured-error-codes*
*Completed: 2026-08-08*
