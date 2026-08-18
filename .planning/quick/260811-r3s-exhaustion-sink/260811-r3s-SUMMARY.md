---
phase: quick-260811-r3s
plan: 01
subsystem: inspect
tags: [security-review, coverage, tracker, denial-of-service, false-negative, false-positive]

# Dependency graph
requires:
  - phase: 58-synchronous-inspection
    provides: internal/inspect's FamilyResult/Check/Coverage three-value status model (D-02a/INSP-03), and Tracker (budget.go) as the per-operation object this task extends
  - phase: quick/260810-r2i
    provides: round 2's exit-path table -- the worklist of every ceilingHit producer this task's receptacle now backstops
provides:
  - "internal/inspect.Tracker.MarkCeilingHit(): a nil-safe receptacle method every scanner calls the instant its OWN local resource ceiling fires (independent of Consume's byte/time budget); ExhaustedReason/Exhausted now read this alongside Consume's own exhausted field, so BuildReport's existing tr-fallback in buildCoverage makes Coverage.Complete false unconditionally -- no caller has anything left to thread or forget"
  - "AnalyzePDF/AnalyzeContainer/AnalyzeContainerWith/AnalyzeMedia/AnalyzeImage all gain a *Tracker parameter (alongside, not replacing, maxBytes), threaded into every unexported helper that detects a local ceiling"
  - "pdf.go:800 fixed: extractPDFPageCount checks ceilingHit before trusting a scanned page_count value"
  - "pdf.go:543 fixed: scanPDFOpaqueRanges's three sub-scans report their own ceilingHit; an incomplete opaque-range set downgrades both a found match and a clean verdict to unknown"
  - "media.go:575 fixed: findEBMLDuration tracks TimestampScale's own ceilingHit and refuses to publish a duration computed against the unproven default scale"
  - "container.go:497 fixed: extractXLSXSheetCount reads one extra byte past maxRelsPartBytes to detect the io.LimitReader boundary trap"
affects: [60-sanitize-verifier, any-future-inspect-followup]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Receptacle over return-value threading: a per-operation object (Tracker) that scanners write to directly at the point of detection, read once by the single assembler (BuildReport), replacing a return-value chain a caller several layers up could forget to propagate. Applied here to 'this service's own resource ceiling fired somewhere in this operation' -- the same shape could generalize to any other cross-cutting fact a whole operation needs to know regardless of which specific call frame discovers it."
    - "Separate the receptacle field from the pre-existing signal it augments (ceilingHit vs exhausted) rather than merging them, when merging would change an unrelated caller's observable behavior (Consume's own first-line short-circuit) that no test in scope exercises or requires changing."

key-files:
  created: []
  modified:
    - internal/inspect/budget.go
    - internal/inspect/budget_test.go
    - internal/inspect/pdf.go
    - internal/inspect/pdf_test.go
    - internal/inspect/container.go
    - internal/inspect/container_test.go
    - internal/inspect/image.go
    - internal/inspect/image_test.go
    - internal/inspect/media.go
    - internal/inspect/media_test.go
    - internal/inspect/report_test.go
    - internal/api/inspect_handlers.go

key-decisions:
  - "MarkCeilingHit is a SEPARATE Tracker field from Consume's own `exhausted`, not a reuse of it. Reusing would make MarkCeilingHit short-circuit every subsequent Consume call in the same operation (Consume's own first line: `if t.exhausted != \"\" { return false }`) -- a behavior change with no test in this task's scope requiring or exercising it. ExhaustedReason()/Exhausted() read both fields, so BuildReport's observable contract (Coverage reads completeness from the Tracker) is a strict superset of before, without touching Consume's byte/time semantics."
  - "Analyzer signatures gained *Tracker ALONGSIDE maxBytes, not instead of it -- the plan's own design text sanctioned either choice ('или вместе с ним, если бюджет и приёмник разумнее держать раздельно'). Replacing maxBytes with tr would have forced every one of the ~85 existing test call sites to construct a Tracker with a specific MaxBytes value instead of a mechanical `, nil` append, for no behavioral gain -- maxBytes still governs window sizing exactly as before; tr is purely the new write-only receptacle channel."
  - "Local ceilings all report as ReasonWorkCeilingBytes when they reach a Check/characteristic Reason (unchanged, pre-existing convention) -- MarkCeilingHit deliberately takes no Reason parameter and always means 'the same kind of thing Consume's own byte ceiling means, detected somewhere Consume itself was never called'. Time-vs-bytes distinction stays exclusively Consume's own concern."
  - "status.go and report.go needed ZERO code changes -- report.go's buildCoverage already had a `tr != nil && tr.Exhausted()` fallback (58-06/58-07) that nothing in this task's scope required touching; extending Tracker's own Exhausted()/ExhaustedReason() to also see MarkCeilingHit's mark was sufficient to make that pre-existing fallback the receptacle's read side for free. The plan's own files_modified frontmatter listed status.go/report.go as candidates ('на усмотрение исполнителя'); this is documented here as the concrete implementation choice, not a deviation from an instruction."
  - "pdf.go:543's fix downgrades a risk-token match found OUTSIDE every KNOWN opaque range to StatusUnknown when the range scan itself was truncated -- deliberately NOT following the /ObjStm precedent ('a positive finding is never weakened by the same blindness'). The two cases are different: /ObjStm blindness never disputes that the matched BYTES are really there; an incomplete opaque-range scan disputes whether those bytes sit inside PDF syntax or inside an as-yet-unscanned literal string's DATA -- the exact ambiguity the 'filtered' branch already treats as unknown, just reached via truncation instead of a completed scan."
  - "extractXLSXSheetCount's fix reads maxRelsPartBytes+1 (not merely checking io.LimitReader's own EOF) and, on overflow, discards the count entirely (0, false, true) rather than returning the correct-but-partial count for the first maxRelsPartBytes bytes -- matching the function's own existing convention for every other ceiling case (declared-size skip, open error, read error all already return (0, false, true)), not inventing a new partial-success shape."

requirements-completed: [PR9R3-STRUCTURAL-SINK, PR9R3-F1-PAGECOUNT-CEILING, PR9R3-F2-EBML-TIMESTAMPSCALE, PR9R3-F3-LIMITREADER-EOF, PR9R3-F4-OPAQUE-RANGE-EXHAUSTION]

# Metrics
duration: ~2h40min
completed: 2026-08-11
---

# Quick Task 260811-r3s: Exhaustion Sink Summary

**Replaced the return-value-threading shape that let three successive review rounds each find a caller dropping an honest exhaustion signal with a receptacle (`Tracker.MarkCeilingHit()`) every scanner writes to directly, so `BuildReport`'s Coverage claim can never come back `Complete:true` for an operation where some scanner gave up early -- regardless of whether the specific Check/characteristic wiring three rounds of point-patches have been chasing got it right this time.**

## Performance

- **Duration:** ~2h40min (one atomic commit per the plan's own instruction: "Execute both tasks; commit atomically")
- **Files modified:** 12 (`internal/inspect/{budget,budget_test,pdf,pdf_test,container,container_test,image,image_test,media,media_test,report_test}.go`, `internal/api/inspect_handlers.go`)
- **Commit:** `3a2d3ba`

## Was → Became (signature changes, per the plan's own acceptance criteria)

### Exported (`internal/inspect`)

| Function | Was | Became |
|---|---|---|
| `AnalyzePDF` | `(r io.ReaderAt, size, maxBytes int64) FamilyResult` | `(r io.ReaderAt, size, maxBytes int64, tr *Tracker) FamilyResult` |
| `AnalyzeContainer` | `(r io.ReaderAt, size, maxBytes int64, bombThreshold uint64) FamilyResult` | `(r io.ReaderAt, size, maxBytes int64, bombThreshold uint64, tr *Tracker) FamilyResult` |
| `AnalyzeContainerWith` | `(cr convert.ContainerResult, r io.ReaderAt, size, maxBytes int64, bombThreshold uint64) FamilyResult` | `(cr convert.ContainerResult, r io.ReaderAt, size, maxBytes int64, bombThreshold uint64, tr *Tracker) FamilyResult` |
| `AnalyzeMedia` | `(format string, r io.ReaderAt, size, maxBytes int64) FamilyResult` | `(format string, r io.ReaderAt, size, maxBytes int64, tr *Tracker) FamilyResult` |
| `AnalyzeImage` | `(format string, r io.ReaderAt, size, maxBytes int64, pixelBombThreshold uint64) FamilyResult` | `(format string, r io.ReaderAt, size, maxBytes int64, pixelBombThreshold uint64, tr *Tracker) FamilyResult` |
| `Tracker` | (no ceiling-mark method) | `+ MarkCeilingHit()` (nil-safe); `ExhaustedReason`/`Exhausted` now also read it |

No exported function returns a boolean exhaustion flag as part of its result, before or after -- verified by reading every signature above (`FamilyResult` and `*Tracker`'s own methods never have, and still don't).

### Unexported, per file

**`pdf.go`:**

| Function | Was | Became |
|---|---|---|
| `scanPDFOpaqueRanges` | `(buf []byte) []pdfOpaqueRange` | `(buf []byte, tr *Tracker) ([]pdfOpaqueRange, bool)` -- now returns its own ceilingHit |
| `scanPDFStreamRanges` | `(buf []byte) []pdfOpaqueRange` | `(buf []byte, tr *Tracker) (ranges []pdfOpaqueRange, ceilingHit bool)` |
| `scanPDFCommentRanges` | `(buf []byte) []pdfOpaqueRange` | `(buf []byte, tr *Tracker) (ranges []pdfOpaqueRange, ceilingHit bool)` |
| `scanPDFLiteralAndHexStringRanges` | `(buf []byte) []pdfOpaqueRange` | `(buf []byte, tr *Tracker) (ranges []pdfOpaqueRange, ceilingHit bool)` |
| `scanTokenOccurrences` | `(buf []byte, tokens [][]byte, opaque []pdfOpaqueRange) (pos int, found, filtered, ceilingHit bool)` | `+ tr *Tracker` param |
| `scanAllOccurrences` | `(buf, token []byte, max int, fn func(pos int) bool) (ceilingHit bool)` | `(buf, token []byte, max int, tr *Tracker, fn func(pos int) bool) (ceilingHit bool)` |
| `extractPDFPageCount` | `(head []byte, compressed, truncated, hexEscape bool) (*int, Reason)` | `+ tr *Tracker` param; **also** the ceilingHit-before-found reorder (pdf.go:800 fix) |
| `verdictStatus` | `(compressed, truncated, hexEscape bool) CheckStatus` | `(compressed, truncated, hexEscape, opaqueRangesCeilingHit bool) CheckStatus` |
| `verdictReason` | `(compressed, truncated, hexEscape bool) Reason` | `(compressed, truncated, hexEscape, opaqueRangesCeilingHit bool) Reason` |

**`container.go`:**

| Function | Was | Became |
|---|---|---|
| `appendExternalReferenceResult` | `(res *FamilyResult, zr *zip.Reader, format string, maxBytes int64)` | `+ tr *Tracker` param |
| `scanExternalReferences` | `(zr *zip.Reader, format string, maxBytes int64) (location string, ceilingHit bool)` | `+ tr *Tracker` param |
| `setSheetAndPageCharacteristics` | `(res *FamilyResult, zr *zip.Reader, format string)` | `+ tr *Tracker` param |
| `extractXLSXSheetCount` | `(zr *zip.Reader) (count int, ok, ceilingHit bool)` | `(zr *zip.Reader, tr *Tracker) (count int, ok, ceilingHit bool)` -- **also** the LimitReader+1 boundary fix (container.go:497) |

**`image.go`:**

| Function | Was | Became |
|---|---|---|
| `scanImageMetadata` | `(format string, r io.ReaderAt, size, maxBytes int64) (location string, found, truncated bool)` | `+ tr *Tracker` param |
| `scanJPEGMetadata` | `(b []byte) (location string, found, ceilingHit bool)` | `(b []byte, tr *Tracker) (...)` |
| `scanPNGMetadata` | `(b []byte) (location string, found, ceilingHit bool)` | `(b []byte, tr *Tracker) (...)` |
| `scanRIFFChunkMetadata` | `(b []byte) (location string, found, ceilingHit bool)` | `(b []byte, tr *Tracker) (...)` |

**`media.go`:**

| Function | Was | Became |
|---|---|---|
| `isobmffCharacteristics` | `(r io.ReaderAt, size, maxBytes int64, includeDims bool) Characteristics` | `+ tr *Tracker` param |
| `findMoov` | `(r io.ReaderAt, size, maxBytes int64) ([]byte, Reason)` | `+ tr *Tracker` param |
| `findTrackDimensions` | `(moovPayload []byte) (width, height uint32, ok, ceilingHit bool)` | `(moovPayload []byte, tr *Tracker) (...)` |
| `findDeclaredCodec` | `(moovPayload []byte) (codec string, ok, ceilingHit bool)` | `(moovPayload []byte, tr *Tracker) (...)` |
| `walkTopLevelBoxes` | `(buf []byte, fn func(string, []byte) bool) (ceilingHit bool)` | `(buf []byte, tr *Tracker, fn func(string, []byte) bool) (ceilingHit bool)` |
| `firstChildBox` | `(buf []byte, boxType string) (payload []byte, found, ceilingHit bool)` | `(buf []byte, boxType string, tr *Tracker) (...)` |
| `ebmlCharacteristics` | `(r io.ReaderAt, size, maxBytes int64) Characteristics` | `+ tr *Tracker` param |
| `findEBMLDuration` | `(segment []byte) (durationSeconds float64, ok, ceilingHit bool)` | `(segment []byte, tr *Tracker) (...)` -- **also** the TimestampScale-ceilingHit fix (media.go:575) |
| `findEBMLCodecID` | `(segment []byte) (codec string, ok, ceilingHit bool)` | `(segment []byte, tr *Tracker) (...)` |
| `firstEBMLChild` | `(buf []byte, id uint32) (payload []byte, found, ceilingHit bool)` | `(buf []byte, id uint32, tr *Tracker) (...)` |
| `walkEBMLElements` | `(buf []byte, fn func(uint32, []byte) bool) (ceilingHit bool)` | `(buf []byte, tr *Tracker, fn func(uint32, []byte) bool) (ceilingHit bool)` |
| `wavCharacteristics` | `(r io.ReaderAt, size, maxBytes int64) Characteristics` | `+ tr *Tracker` param |

`status.go` and `report.go`: **zero code changes** -- see Decisions Made.

## Accomplishments

### Task 1: the receptacle

- `Tracker.MarkCeilingHit()` -- nil-safe, called unconditionally from every low-level ceiling-detection site (11 distinct call sites across `pdf.go`/`container.go`/`image.go`/`media.go`) the instant a scanner's own local iteration/entry/box cap fires.
- `ExhaustedReason()`/`Exhausted()` read the union of Consume's own `exhausted` field and the new `ceilingHit` field, so `BuildReport`'s pre-existing `tr != nil && tr.Exhausted()` fallback in `buildCoverage` (58-06/58-07, untouched) now drives Coverage from the receptacle regardless of what any Check/characteristic Reason says.
- Proven by `TestBuildReportCallerMarkedCeilingHitAloneMakesCoverageIncomplete`: a `Tracker` marked directly, a `FamilyResult` with **zero** ceiling-carrying Check/characteristic anywhere -- `Coverage.Complete` is still `false`. This is the acceptance criteria's own required proof, verbatim.
- The two-kinds-of-incompleteness distinction (structural blindness vs. this service's own ceiling) is preserved and proven side-by-side by `TestBuildReportStructuralBlindnessKeepsCompleteWhileCeilingHitDoesNot`: the identical `/ObjStm` PDF, run through two Trackers -- one untouched (Coverage stays complete), one with `MarkCeilingHit()` called directly (Coverage goes incomplete) -- proving the receptacle carries the distinction rather than collapsing it.

### Task 2: the four named findings

- **pdf.go:800** -- `extractPDFPageCount` now checks `ceilingHit || truncated` BEFORE trusting `found`, closing the stale-page-count defect where a walk cut short by `maxTokenScanIterations` could publish an earlier marker's `/Count` as authoritative even though a later marker (never reached) carried a different one.
- **pdf.go:543** -- `scanPDFStreamRanges`/`scanPDFCommentRanges`/`scanPDFLiteralAndHexStringRanges` each now report their own ceilingHit; `AnalyzePDF`'s per-code loop downgrades a `found` match to unknown when the opaque-range scan that would have filtered it was itself truncated, AND folds the same signal into `verdictStatus`/`verdictReason`'s clean-vs-unknown gate -- both directions the finding names ("has no right to confirm a finding OR declare clean").
- **media.go:575** -- `findEBMLDuration` now tracks whether the TimestampScale lookup's own search was cut short by `maxEBMLElements`; if so, a separately-found Duration is never published against the unproven 1,000,000ns/unit default scale.
- **container.go:497** -- `extractXLSXSheetCount` reads `maxRelsPartBytes+1` bytes instead of exactly `maxRelsPartBytes`; if the extra byte comes back, the part's real content exceeds this scan's own ceiling and no partial (silently truncated) sheet count is published.

### `io.LimitReader` audit (Task 2's explicit "check every other place" instruction)

Every use of `io.LimitReader` (or an equivalent read-to-a-declared-boundary) in `internal/inspect`:

| Location | Verdict |
|---|---|
| `container.go` `scanExternalReferences` (`io.LimitReader(rc, limit)`, ~line 347) | **Already safe.** `if int64(len(data)) >= limit { truncatedAnyPart = true }` (pre-existing, round 1/T-SLE-04) already treats "filled the entire allowance" as ambiguous/truncated -- the conservative direction, even though it can occasionally over-flag a part whose real size exactly equals the limit. No change needed. |
| `container.go` `extractXLSXSheetCount` (`io.LimitReader(rc, maxRelsPartBytes)`, line 492 pre-fix) | **Fixed this session** (container.go:497 finding) -- was the one unguarded LimitReader boundary in the package; now reads `maxRelsPartBytes+1` and treats overflow as a ceiling, matching `scanExternalReferences`'s own already-correct pattern. |

`window.go`'s `readWindow` uses `r.ReadAt` directly (not `io.LimitReader`) and is already bounds-correct by construction (returns the actual bytes read, never assumes a full read). No other `io.LimitReader` call exists anywhere in `internal/inspect` (confirmed by `grep -n "LimitReader" internal/inspect/*.go`, excluding test files and comments).

## Deviations from Plan

### Auto-fixed Issues

None beyond what the plan's own Task 2 explicitly named and required (the four findings) -- no Rule 1/2/3 auto-fixes outside the plan's own scope were needed.

### Design choices requiring documentation (per plan's own "Claude's discretion" clause)

**1. Receptacle implemented as a new `Tracker` field, not a new type `Tracker` owns.**
- The plan explicitly left this open ("Естественный кандидат -- расширить Tracker... либо завести отдельный тип, которым Tracker владеет"). Extending `Tracker` directly (a `ceilingHit bool` field + `MarkCeilingHit()`) was chosen over a separate owned type because `Tracker` is already the one object threaded to every call site that would need the receptacle (via the new `tr *Tracker` parameter) -- a separate type would have needed its own accessor method on `Tracker` anyway, adding indirection with no additional safety property.

**2. `status.go`/`report.go` needed zero code changes**, contrary to the plan's `files_modified` frontmatter listing both.
- **Found during:** Task 1, once `report.go`'s existing `buildCoverage` was read closely -- it already had `if tr != nil && tr.Exhausted() { coverage.Complete = false; ... }` from a prior phase (58-06/58-07).
- **Why this satisfies the plan's own requirement:** the plan's acceptance criteria is about the RECEPTACLE mechanism and BuildReport reading from it -- not about which specific file's line count changes. Extending `Tracker.ExhaustedReason()`/`Exhausted()` to also see `MarkCeilingHit`'s mark was sufficient to make the pre-existing, untouched `buildCoverage` fallback become the receptacle's read side, satisfying "`BuildReport` вычисляет полноту, читая приёмник" (BuildReport computes completeness by reading the receptacle) without touching `report.go` at all.
- **Verification:** `git diff --stat internal/inspect/report.go internal/inspect/status.go` is empty (confirmed above); the `TestBuildReportCallerMarkedCeilingHitAloneMakesCoverageIncomplete` proof exercises exactly this untouched code path.

**3. Analyzer signatures gained `*Tracker` ALONGSIDE `maxBytes`, not replacing it.**
- Explicitly sanctioned by the plan's own text ("или вместе с ним, если бюджет и приёмник разумнее держать раздельно"). Chosen because `maxBytes` still governs window sizing identically to before (unchanged semantics), and replacing it would have forced restructuring ~85 existing test call sites around constructing a per-test `Tracker` with a specific `MaxBytes`, for zero behavioral gain over the chosen shape (a mechanical `, nil` append at each site).

**Impact on plan:** None of these are scope deviations from the plan's own instructions -- all three are exercises of discretion the plan itself explicitly left open ("Конкретная форма -- на усмотрение исполнителя", "или вместе с ним, если..."), documented here per the plan's own instruction to record such choices.

---

**Total deviations:** 0 unauthorized; 3 documented design choices, all within the plan's own stated discretion.

## Issues Encountered

- **First attempt at the pdf.go:543 fixture (`jsInsideLiteralStringPastOpaqueScanCeilingPDFFixture`) wrapped the filler in one outer `(...)`.** This made the entire ~2MB filler ONE nested literal string (the depth tracker treats interior `(`/`)` as depth++/depth--, never reaching 0 until the closing paren I added after the filler) -- `scanPDFOpaqueRanges` consumed it in a SINGLE iteration instead of `maxTokenScanIterations` separate ones, so `ceilingHit` never fired and the target string ended up correctly filtered as expected-but-for-the-wrong-reason (the test would have passed even without the fix, which is exactly the "passes by coincidence" failure mode D-16 warns about). Diagnosed via a throwaway debug test logging `scanPDFOpaqueRanges`'s own `ceilingHit`/range count directly. Fixed by removing the outer wrapping parens so each `"()"` pair is its own complete, depth-0-closing top-level string.
- **That same fixture, once fixed to produce `maxTokenScanIterations` SEPARATE ranges, took 17.77s** because `scanPDFLiteralAndHexStringRanges`'s own `idxAngle := bytes.IndexByte(buf[pos:], '<')` probe (run every iteration, to decide paren-vs-hex-string precedence) has to scan the ENTIRE remaining buffer when no `'<'` exists anywhere ahead -- a pre-existing O(iterations × remaining-buffer-length) cost in code this task did not otherwise touch, not a regression introduced here. Fixed the TEST (not the production code, which is out of this task's named scope) by scattering a harmless `'<'` DATA byte inside every 16th pair (`"(<)"` instead of `"()"`, still a complete, depth-0-closing literal string), keeping the probe's target always nearby. Runtime dropped to 0.04-0.65s.

## User Setup Required

None -- no external service configuration required.

## Verification

```
$ go build ./... && go vet ./...
(clean, no output)

$ gofmt -l internal/inspect internal/api
(clean, no output)

$ go test ./... -count=1 -race
ok for all 25 testable packages, including internal/inspect (6.9-8.1s across runs) and internal/api (5.0-6.0s)

$ git diff --stat -- go.mod go.sum
(empty -- zero new dependencies)

$ git rev-list --count HEAD -- internal/api/testdata/pre58_healthz.golden.json
1
$ git rev-list --count HEAD -- internal/api/testdata/pre58_formats.golden.json
1
$ git rev-list --count HEAD -- internal/api/testdata/pre58_createjob_rejections.golden.json
1
```

Five protected tests, re-run explicitly, unedited (only the mechanical `, nil`/`tr` argument each needed to compile against the new signatures):

```
$ go test ./internal/inspect/... -run 'TestAnalyzePDFObjStmNeverReportsClean|TestAnalyzePDFFindingSurvivesObjStm|TestReasonsDocMatchesGoConstants|TestAllReasonsCoversEveryDeclaredConstant|TestAnalyzeContainerWithMatchesAnalyzeContainer' -v -count=1
--- PASS: TestAnalyzeContainerWithMatchesAnalyzeContainer (0.00s)
    --- PASS: TestAnalyzeContainerWithMatchesAnalyzeContainer/macro_docx (0.00s)
    --- PASS: TestAnalyzeContainerWithMatchesAnalyzeContainer/plain_xlsx (0.00s)
--- PASS: TestAnalyzePDFObjStmNeverReportsClean (0.00s)
--- PASS: TestAnalyzePDFFindingSurvivesObjStm (0.00s)
--- PASS: TestReasonsDocMatchesGoConstants (0.00s)
--- PASS: TestAllReasonsCoversEveryDeclaredConstant (0.00s)
PASS
```

## Inversions (expected failure stated first, run, literal output pasted, reverted, confirmed green)

### Task 1 inversion -- receptacle drives Coverage

**Change:** `buildCoverage`'s `tr != nil && tr.Exhausted()` fallback -> `if false && tr != nil && tr.Exhausted() { ... }` (disabled).
**Expected:** `TestBuildReportCallerMarkedCeilingHitAloneMakesCoverageIncomplete` fails (`Coverage.Complete` stays `true`).
**Actual output:**
```
=== RUN   TestBuildReportCallerMarkedCeilingHitAloneMakesCoverageIncomplete
    report_test.go:443: Coverage.Complete = true, want false -- tr.MarkCeilingHit() was called directly and nothing else propagated it, proving the receptacle alone drives Coverage
--- FAIL: TestBuildReportCallerMarkedCeilingHitAloneMakesCoverageIncomplete (0.00s)
FAIL
```
Reverted; `go test ./internal/inspect/...` green again.

### Task 2 inversion -- pdf.go:800 (page_count ceiling-before-found)

**Change:** `extractPDFPageCount`'s check order -> reverted to `if found != nil { return found, "" }` BEFORE the `ceilingHit || truncated` check (pre-fix order).
**Expected:** `TestExtractPDFPageCountCeilingHitBeforeFoundIsUnknownNotStale` fails (`page_count` comes back non-nil).
**Actual output:**
```
=== RUN   TestExtractPDFPageCountCeilingHitBeforeFoundIsUnknownNotStale
    pdf_test.go:894: page_count = 99, want nil -- a ceiling-truncated walk must never publish a value it saw along the way as if it were authoritative
--- FAIL: TestExtractPDFPageCountCeilingHitBeforeFoundIsUnknownNotStale (0.33s)
FAIL
```
Reverted; full suite green again.

### Task 2 inversion -- media.go:575 (EBML TimestampScale ceilingHit discarded)

**Change:** `findEBMLDuration`'s `if tsCeilingHit { return 0, false, true }` -> `if false && tsCeilingHit { ... }` (disabled).
**Expected:** `TestFindEBMLDurationTimestampScaleCeilingHitYieldsUnknownNotDefaultScale` and `TestAnalyzeMediaEBMLTimestampScaleCeilingHitYieldsUnknownDuration` both fail (a duration is computed against the default scale).
**Actual output:**
```
=== RUN   TestFindEBMLDurationTimestampScaleCeilingHitYieldsUnknownNotDefaultScale
    media_test.go:643: findEBMLDuration returned ok=true, dur=0.0125 -- must not compute a duration against an unproven default TimestampScale (its own search was cut short, not proven absent)
--- FAIL: TestFindEBMLDurationTimestampScaleCeilingHitYieldsUnknownNotDefaultScale (0.00s)
=== RUN   TestAnalyzeMediaEBMLTimestampScaleCeilingHitYieldsUnknownDuration
    media_test.go:676: duration_seconds = 0.0125, want nil -- TimestampScale's search was cut short, not proven absent, so no value may be published
--- FAIL: TestAnalyzeMediaEBMLTimestampScaleCeilingHitYieldsUnknownDuration (0.00s)
FAIL
```
Reverted; full suite green again.

### Task 2 inversion -- pdf.go:543 (opaque-range scan ceiling suppresses a found match)

**Change:** the `if foundOpaqueCeilingHit { ... }` downgrade in `AnalyzePDF`'s per-code loop -> `if false && foundOpaqueCeilingHit { ... }` (disabled).
**Expected:** `TestAnalyzePDFOpaqueRangeScanCeilingSuppressesFalsePositiveFinding` fails (a critical `pdf_javascript` Finding is produced).
**Actual output:**
```
=== RUN   TestAnalyzePDFOpaqueRangeScanCeilingSuppressesFalsePositiveFinding
    pdf_test.go:960: got a critical Finding for pdf_javascript -- the opaque-range scan that would have recognized this match sits inside a literal string was itself truncated by its own cap, so this is exactly the false-positive class the literal/hex-string filter exists to close
--- FAIL: TestAnalyzePDFOpaqueRangeScanCeilingSuppressesFalsePositiveFinding (0.06s)
FAIL
```
Reverted; full suite green again.

### Task 2 inversion -- container.go:497 (LimitReader boundary trap)

**Change:** `extractXLSXSheetCount`'s `io.LimitReader(rc, maxRelsPartBytes+1)` + overflow check -> reverted to `io.LimitReader(rc, maxRelsPartBytes)` with no overflow check (pre-fix shape).
**Expected:** `TestAnalyzeContainerSheetCountLimitReaderBoundaryIsWorkCeilingNotComplete` fails (a truncated, undercounted `sheet_count` is reported as complete).
**Actual output:**
```
=== RUN   TestAnalyzeContainerSheetCountLimitReaderBoundaryIsWorkCeilingNotComplete
    container_test.go:403: sheet_count = 4827, want nil -- the part's real content runs past maxRelsPartBytes even though it DECLARES exactly maxRelsPartBytes; a count computed from the truncated prefix would silently omit sheets past the boundary
--- FAIL: TestAnalyzeContainerSheetCountLimitReaderBoundaryIsWorkCeilingNotComplete (0.02s)
FAIL
```
(4827 of 10000 real `<sheet/>` elements silently omitted -- exactly the finding's own worked example.) Reverted; full suite green again.

## Threat Model / Threat Flags

All five threats in the plan's own `<threat_model>` register (T-R3S-01 through T-R3S-05) are mitigated as described in that table; T-R3S-SC (npm/pip/cargo installs) is not applicable -- zero new dependencies (`git diff --stat -- go.mod go.sum` confirmed empty). No new security-relevant surface (endpoint, auth path, file-access pattern, schema change at a trust boundary) was introduced -- every change is internal to `internal/inspect`'s existing analysis pipeline and the one new `*Tracker` parameter `internal/api/inspect_handlers.go` already had access to. No `Threat Flags` section needed.

## Known Stubs

None -- every fix is a complete, tested behavior change; no placeholder values or unwired data paths were introduced.

## Next Phase Readiness

- The receptacle mechanism is now the load-bearing path for Coverage's "this service's own ceiling fired" claim -- a fourth review round targeting this same class would need to find a scanner that hits a LOCAL resource ceiling WITHOUT calling `tr.MarkCeilingHit()` at all (a genuinely new omission, not a threading bug three rounds have already swept), which is a materially harder defect to introduce than the ones this task closes.
- All four codex-named findings (pdf.go:800, pdf.go:543, media.go:575, container.go:497) are closed and proven by dedicated tests plus inversions.
- The `io.LimitReader` audit found and fixed the one remaining unguarded boundary in the package (`extractXLSXSheetCount`); `scanExternalReferences`'s own use was already safe and required no change.
- Phase 60 (sanitize verifier) inherits a `Tracker` whose `MarkCeilingHit()` receptacle is available to any new scanner it introduces, without needing to re-invent this mechanism.

---
*Phase: quick-260811-r3s*
*Completed: 2026-08-11*

## Self-Check: PASSED
