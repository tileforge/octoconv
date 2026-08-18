---
phase: quick/260810-sle
plan: 01
subsystem: api
tags: [pdf, findings, coverage, inspect, security-review, false-negative]

# Dependency graph
requires:
  - phase: 58-synchronous-inspection
    provides: internal/inspect's FamilyResult/Check/Coverage three-value status model (D-02a/INSP-03) this task's fixes operate inside
provides:
  - PDF hex-escaped name detection (containsPDFNameHexEscape) that downgrades the whole PDF block to unknown/pdf_name_escape instead of reporting clean on an escaped /JavaScript or /ObjStm
  - PDF stream-body/comment lexical filtering (scanPDFOpaqueRanges, firstTokenLocationOutsideOpaqueRanges) that reports unknown/pdf_stream_body_token instead of a false critical finding or a false clean for a token found only inside opaque payload bytes
  - Image metadata window-truncation awareness (scanImageMetadata's new truncated return) so a miss past the byte ceiling reports unknown/work_ceiling_bytes, never clean
  - Container external-reference scan now sets ceilingHit when a candidate part is skipped by its own declared size, not only when io.LimitReader cuts a read short
  - Report-wide Coverage now scans characteristic-level "<name>_reason" keys (not only Check.Reason) for the two service-ceiling reasons
  - Unrecognized-format reports now emit unknown/format_unrecognized for the whole vocabulary instead of not_applicable/format_not_applicable
affects: [60-sanitize-verifier, any-future-inspect-followup]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "PDF name-escape/opaque-range detection as a whole-block downgrade signal, following D-02a's own /ObjStm precedent: a scanner blind spot degrades the ABSENCE claim (StatusClean) but never the PRESENCE claim (a real Finding survives)"
    - "Coverage.MarkTruncatedByCharacteristic as the characteristic-level sibling of Coverage.MarkTruncated -- same ceiling-reason gate and first-reason-wins rule, but keyed by nothing (no Code) rather than by a Check's Code"

key-files:
  created: []
  modified:
    - internal/inspect/pdf.go
    - internal/inspect/pdf_test.go
    - internal/inspect/image.go
    - internal/inspect/image_test.go
    - internal/inspect/container.go
    - internal/inspect/container_test.go
    - internal/inspect/report.go
    - internal/inspect/report_test.go
    - internal/inspect/status.go

key-decisions:
  - "Finding 1 (hex-escaped PDF names): chose option (б) from the plan -- detect the mere POSSIBILITY of an escape and downgrade to unknown, rather than writing a name-escape decoder. Matches D-01/D-02's bounded-parsing discipline and the existing /ObjStm precedent."
  - "Finding 1's downgrade reason (ReasonPDFNameEscape) is a DIFFERENT Reason from ReasonCompressedObjectStreams, per the plan's explicit instruction -- two distinct blind spots must stay distinguishable in the response."
  - "Finding 2 (stream-body false positives): a token found ONLY inside an opaque range is neither promoted to a Finding NOR allowed to fall through to the ordinary verdict rule -- a third outcome (unknown/pdf_stream_body_token) was added rather than reusing an existing Reason, so a future reader can tell this apart from /ObjStm blindness or a byte-ceiling truncation."
  - "scanPDFOpaqueRanges deliberately OVER-approximates stream/comment ranges (unanchored substring match on \"stream\"/\"%\") rather than under-approximating -- over-marking only ever suppresses a Finding in favor of Unknown, never promotes a Check to Clean, so the imprecision is safe in the direction this package already treats as correct."
  - "Finding 5 (characteristic-level coverage): added Coverage.MarkTruncatedByCharacteristic as a new method rather than overloading MarkTruncated, because TruncatedChecks is Code-keyed and a bare characteristic key names no Code -- forcing a fake Code there would be a worse lie than leaving the list incomplete."
  - "Finding 6 (unrecognized format): buildChecks now takes the detected format string directly rather than inferring emptiness from the families slice, since an empty format with a non-empty families slice is theoretically possible and the fallback must key off Report.Format's own documented meaning, not off how many analyzers happened to run."

requirements-completed: [PR9-F1-PDF-HEX-ESCAPES, PR9-F2-PDF-STREAM-FALSE-POSITIVE, PR9-F3-IMAGE-WINDOW, PR9-F4-RELS-SKIPPED, PR9-F5-COVERAGE-CHARACTERISTICS, PR9-F6-UNKNOWN-FORMAT]

# Metrics
duration: ~50min
completed: 2026-08-10
---

# Quick Task 260810-sle: Fix Six Codex Findings in internal/inspect Summary

**Closed all six codex PR #9 review findings in `internal/inspect` -- five false-`clean` defects (hex-escaped PDF names, stream-body token matches, image metadata past the read window, a container part skipped by declared size, and characteristic-level ceiling truncation invisible to report-wide coverage) plus one false-`not_applicable` defect (unrecognized format), each closed with a test that fails on the pre-fix code and passes after, with every fix verified live by inversion.**

## Performance

- **Duration:** ~50 min (three task commits, `2026-08-10T20:55` to `2026-08-10T21:03`)
- **Tasks:** 3
- **Files modified:** 9 (`internal/inspect/{pdf,pdf_test,image,image_test,container,container_test,report,report_test,status}.go`)

## Accomplishments

- `AnalyzePDF` no longer reports `clean` for a PDF whose risk-token names use a hex escape (`/Java#53cript`, `/Obj#53tm`) -- the mere syntactic possibility of an escape now downgrades the whole PDF block to `unknown/pdf_name_escape`, closing an exploitable false negative (a malicious PDF could previously spell `/JavaScript` around the byte-scanner entirely).
- `AnalyzePDF` no longer promotes a risk-token literal found only inside a `stream`...`endstream` body or a `%` comment to a critical Finding, but also no longer lets that same match fall through to `clean` -- it reports `unknown/pdf_stream_body_token`, an honest third answer for "found something, cannot verify it's real PDF syntax."
- `AnalyzeImage`'s metadata check now distinguishes "the whole file was scanned and nothing was there" (legitimately `clean`) from "the byte-ceiling window did not cover the whole file" (`unknown/work_ceiling_bytes`) -- a PNG/WebP/HEIC/JPEG with EXIF/XMP past the default 8 MiB window can no longer be reported clean.
- `AnalyzeContainer`'s external-reference scan now treats a candidate part skipped because its OWN declared size exceeds `maxRelsPartBytes` the same as a part cut short mid-read: both set `ceilingHit`/`truncatedAnyPart`, so the container's only relevant `.rels`/`content.xml`/`styles.xml` part being un-openable can no longer report clean.
- `BuildReport`'s Coverage claim now also scans the merged Characteristics bag for `"<name>_reason"` keys carrying a service-ceiling reason (not only `Check.Reason`) -- a media analyzer reporting `duration_seconds_reason=work_ceiling_bytes` with zero accompanying Checks can no longer leave `coverage.complete: true` in the same response body.
- `BuildReport` now falls back to `unknown/format_unrecognized` for the entire closed vocabulary when detection never claimed a format at all, instead of the recognized-but-irrelevant `not_applicable/format_not_applicable` fallback -- a client uploading unrecognized content no longer receives a report implying every risk check was inapplicable.

## Task Commits

Each task was committed atomically:

1. **Task 1: PDF hex-escaped names and stream-body false positives** - `eae11f2` (fix)
2. **Task 2: Image metadata window and container part-size skip** - `7591ae4` (fix)
3. **Task 3: Report coverage characteristics and unrecognized-format fallback** - `8b98ca5` (fix)

_No TDD red/green split -- `type="auto"` tasks per the plan (not `tdd="true"`); each commit bundles the fix, its test(s), and any doc-comment updates together, per this repo's convention for `fix(...)` commits._

## Files Created/Modified

- `internal/inspect/pdf.go` - Added `containsPDFNameHexEscape`, `pdfOpaqueRange`/`scanPDFOpaqueRanges`/`firstTokenLocationOutsideOpaqueRanges`, `isPDFNameByte`/`isHexDigit`; extended `verdictStatus`/`verdictReason`/`extractPDFPageCount` with a `hexEscape` parameter; `AnalyzePDF`'s per-code loop now tracks `matchedInsideOpaque` separately from `found`.
- `internal/inspect/pdf_test.go` - Added `hexEscapeJSPDFFixture`, `hexEscapeObjStmPDFFixture`, `jsInsideStreamOnlyPDFFixture`, and three new tests (`TestAnalyzePDFHexEscapedJavaScriptNeverReportsClean`, `TestAnalyzePDFHexEscapedObjStmDowngradesToUnknown`, `TestAnalyzePDFTokenOnlyInsideStreamBodyIsUnknownNotCriticalNotClean`). `TestAnalyzePDFObjStmNeverReportsClean` and `TestAnalyzePDFFindingSurvivesObjStm` left byte-for-byte unedited.
- `internal/inspect/image.go` - `scanImageMetadata` now returns a third `truncated bool`; `AnalyzeImage`'s metadata branch is now a three-way switch (found / truncated / clean).
- `internal/inspect/image_test.go` - Added `TestAnalyzeImageMetadataPastWindowIsUnknownNotClean`.
- `internal/inspect/container.go` - `scanExternalReferences`'s declared-size-skip branch now sets `truncatedAnyPart = true` before `continue`; doc comment updated.
- `internal/inspect/container_test.go` - `TestAnalyzeContainerNeverAllocatesOnDeclaredSize` keeps its "part never opened" (`no Finding`) assertion and gains the missing ceiling-verdict assertion (`unknown/work_ceiling_bytes`, not `clean`).
- `internal/inspect/report.go` - `buildChecks` now takes `format string` and falls back to `unknown/format_unrecognized` when `format == ""`; `buildCoverage` now takes `merged Characteristics` and scans its `"<name>_reason"` keys (sorted, for determinism) via the new `Coverage.MarkTruncatedByCharacteristic`.
- `internal/inspect/report_test.go` - Added `TestBuildReportCharacteristicCeilingReasonMarksCoveragePartial`, `TestBuildReportStructuralCharacteristicReasonKeepsCoverageComplete`, `TestBuildReportUnrecognizedFormatYieldsUnknownNotNotApplicable`.
- `internal/inspect/status.go` - Added `ReasonFormatUnrecognized`, `ReasonPDFNameEscape`, `ReasonPDFStreamBodyToken` to the closed `Reason` vocabulary (doc comment's "first N reasons" count updated 4→6→7 across the three commits); added `Coverage.MarkTruncatedByCharacteristic`.

## Decisions Made

See `key-decisions` in frontmatter above for the six per-finding decisions. In addition:

- Left `internal/api/inspect_handlers.go`'s stale doc comment ("...returns an empty slice -- inspect.BuildReport completes the full closed-vocabulary section from `not_applicable` fallbacks for that case") untouched. This comment is now imprecise (the fallback is `unknown/format_unrecognized` for a truly unrecognized format, `not_applicable` only for a recognized-but-irrelevant one), but `internal/api/*` is the sibling parallel agent's owned scope for this worktree, not mine -- flagged here rather than edited.

## Deviations from Plan

None - plan executed exactly as written, including the option choice explicitly left to author discretion (option (б) for finding 1) and the exact Reason-naming discipline the plan called out (a distinct reason from `compressed_object_streams` for finding 1).

## Issues Encountered

None.

## Verification

Per the plan's `<verification>` block, run after all three tasks:

```
go build ./...          # clean
go vet ./...             # clean
gofmt -l internal/inspect  # clean (no output)
go test ./... -count=1 -race   # ok across all 22 testable packages, including internal/inspect and internal/api
git diff --stat -- go.mod go.sum   # empty (zero new dependencies, D-02)
git rev-list --count HEAD -- internal/api/testdata/pre58_healthz.golden.json               # 1
git rev-list --count HEAD -- internal/api/testdata/pre58_formats.golden.json               # 1
git rev-list --count HEAD -- internal/api/testdata/pre58_createjob_rejections.golden.json  # 1
```

All confirmed exactly as above (`go test ./... -count=1 -race` produced `ok` for every package with test files; the three `pre58_*.golden.json` files remain pinned at commit-count 1, confirming phase 58's pre-existing golden bytes were never touched).

## Inversions (dated per constraint: expected failure stated first, run, literal output pasted, reverted, confirmed green)

### Task 1a -- PDF hex-escape detection (T-SLE-01)

**Change:** `hexEscape := containsPDFNameHexEscape(head) || ...` → `hexEscape := false`.
**Expected:** `TestAnalyzePDFHexEscapedJavaScriptNeverReportsClean` and `TestAnalyzePDFHexEscapedObjStmDowngradesToUnknown` fail (report `clean`).
**Actual output:**
```
=== RUN   TestAnalyzePDFHexEscapedJavaScriptNeverReportsClean
    pdf_test.go:363: code "pdf_javascript" reported StatusClean on a PDF using a hex-escaped name (/Java#53cript) -- an escaped name must never be trusted as absent
--- FAIL: TestAnalyzePDFHexEscapedJavaScriptNeverReportsClean (0.00s)
=== RUN   TestAnalyzePDFHexEscapedObjStmDowngradesToUnknown
    pdf_test.go:389: code "pdf_javascript" reported StatusClean on a PDF with a hex-escaped /ObjStm marker (/Obj#53tm)
--- FAIL: TestAnalyzePDFHexEscapedObjStmDowngradesToUnknown (0.00s)
FAIL
```
Reverted; full suite green again.

### Task 1b -- PDF stream-body lexical filtering (T-SLE-02)

**Change:** `firstTokenLocationOutsideOpaqueRanges(head, rt.tokens, headOpaque)` → `firstTokenLocation(head, rt.tokens)` (the unrestricted, pre-fix scan).
**Expected:** `TestAnalyzePDFTokenOnlyInsideStreamBodyIsUnknownNotCriticalNotClean` fails (a critical Finding is produced for the stream-body-only `/JS`).
**Actual output:**
```
=== RUN   TestAnalyzePDFTokenOnlyInsideStreamBodyIsUnknownNotCriticalNotClean
    pdf_test.go:410: got a critical Finding {Code:pdf_javascript Severity:critical Location:212} for a /JS token that occurs ONLY inside a stream body
--- FAIL: TestAnalyzePDFTokenOnlyInsideStreamBodyIsUnknownNotCriticalNotClean (0.00s)
FAIL
```
Reverted; full suite green again.

### Task 2a -- container part-size-skip ceiling (T-SLE-04)

**Change:** removed `truncatedAnyPart = true` before the `continue` in `scanExternalReferences`'s declared-size-skip branch.
**Expected:** `TestAnalyzeContainerNeverAllocatesOnDeclaredSize` fails (reports `clean`).
**Actual output:**
```
=== RUN   TestAnalyzeContainerNeverAllocatesOnDeclaredSize
    container_test.go:393: Check(CodeExternalReference).Status = clean, want unknown -- the only candidate part was skipped by its own declared size (this scan's own ceiling), so clean is not an honest answer about the file
--- FAIL: TestAnalyzeContainerNeverAllocatesOnDeclaredSize (0.00s)
FAIL
```
Reverted; full suite green again.

### Task 2b -- image metadata window truncation (T-SLE-03)

**Change:** forced `metaWindowTruncated = false` immediately after `scanImageMetadata` in `AnalyzeImage`.
**Expected:** `TestAnalyzeImageMetadataPastWindowIsUnknownNotClean` fails (reports `clean`).
**Actual output:**
```
=== RUN   TestAnalyzeImageMetadataPastWindowIsUnknownNotClean
    image_test.go:188: check metadata_present status = clean, want unknown
--- FAIL: TestAnalyzeImageMetadataPastWindowIsUnknownNotClean (0.00s)
FAIL
```
Reverted; full suite green again.

### Task 3a -- report coverage from characteristics (T-SLE-05)

**Change:** made `buildCoverage`'s characteristics-scanning loop unconditionally `continue` (skip the whole loop body) before the `_reason` suffix check.
**Expected:** `TestBuildReportCharacteristicCeilingReasonMarksCoveragePartial` fails (`Coverage.Complete` stays `true`).
**Actual output:**
```
=== RUN   TestBuildReportCharacteristicCeilingReasonMarksCoveragePartial
    report_test.go:215: Coverage.Complete = true, want false -- duration_seconds_reason admitted work_ceiling_bytes truncation with no accompanying Check
--- FAIL: TestBuildReportCharacteristicCeilingReasonMarksCoveragePartial (0.00s)
FAIL
```
Reverted; full suite green again.

### Task 3b -- unrecognized-format fallback (T-SLE-06, bonus inversion beyond the plan's single explicit requirement for this task)

**Change:** `unrecognizedFormat := format == ""` → `unrecognizedFormat := false && format == ""` (always `false`).
**Expected:** `TestBuildReportUnrecognizedFormatYieldsUnknownNotNotApplicable` fails (every code reports `not_applicable/format_not_applicable`).
**Actual output (truncated to first code; all fourteen codes failed identically):**
```
=== RUN   TestBuildReportUnrecognizedFormatYieldsUnknownNotNotApplicable
    report_test.go:257: code ooxml_macro: status=not_applicable, want unknown -- an unrecognized format cannot honestly claim any code is inapplicable
    report_test.go:260: code ooxml_macro: status=not_applicable, want unknown
    report_test.go:263: code ooxml_macro: reason=format_not_applicable, want format_unrecognized
    [... repeated for the remaining 13 codes ...]
FAIL
```
Reverted; full suite green again.

## User Setup Required

None - no external service configuration required.

## Threat Model / Threat Flags

No new security-relevant surface introduced beyond what the plan's own `<threat_model>` register already enumerates (T-SLE-01 through T-SLE-06, all disposition `mitigate`, all six now closed as described above). No `Threat Flags` section needed.

## Known Stubs

None - no stubs introduced; every fix is a complete, tested behavior change.

## Next Phase Readiness

- All six codex PR #9 `internal/inspect` findings are closed; the sibling quick task (260810-slf, `internal/api`) is unaffected -- `go test ./... -count=1 -race` confirms `internal/api` still passes against the updated `internal/inspect` package (which it imports).
- `docs/FINDINGS.md`'s "Report coverage is a separate claim" table now under-documents the Reason vocabulary (it lists six reasons; nine now exist after this task). This table is not mechanically enforced the way the Code column is (`doc_sync_test.go` only checks Codes), so no test fails, but a future doc-sync pass should extend that table with `pdf_name_escape`, `pdf_stream_body_token`, and `format_unrecognized`. Deliberately NOT fixed here: `docs/FINDINGS.md` is outside this task's declared `files_modified` scope (`internal/inspect/*.go` only) and outside this worktree's `internal/inspect/*`-only touch boundary.
- `internal/api/inspect_handlers.go`'s `dispatchInspectFamilies` doc comment (mentioned above) is similarly now imprecise for the unrecognized-format case -- flagged for the sibling agent or a follow-up, not fixed here (out of this worktree's owned-file boundary).

---
*Phase: quick/260810-sle*
*Completed: 2026-08-10*
