---
phase: 58-synchronous-inspection
plan: 04
subsystem: api
tags: [go, pdf, zip, ooxml, odf, findings, inspect, d-02a]

requires:
  - phase: 58-synchronous-inspection (plan 02)
    provides: internal/inspect's closed findings vocabulary (findings.go),
      three-value CheckStatus/Reason/Coverage (status.go), and the bounded
      readWindow primitive (window.go) this plan's two analyzers consume
      without reinventing
provides:
  - internal/inspect.AnalyzePDF -- PDF findings/characteristics analyzer
    whose compressed-object-stream detector (/ObjStm, /Type/XRef) runs
    FIRST and forbids StatusClean on every PDF-domain code when present
    (D-02a), while a positive token match always survives that downgrade
  - internal/inspect.AnalyzeContainer -- ZIP-container (OOXML/ODF/epub)
    analyzer reusing convert.SniffContainer as-is for macro/zip-bomb
    signals, adding external-reference detection (.rels TargetMode, ODF
    xlink:href) and xlsx sheet count as the only genuinely new parsing
  - A corrected honesty rule for container.go's external-reference scan: a
    part read that filled its entire io.LimitReader allowance without
    finding a marker reports unknown/work_ceiling_bytes, never clean
  - PDF page-count fast path (/Type/Pages + nearby /Count) with the naive
    "/Type /Page" occurrence-counting fallback deliberately NOT implemented
affects: [58-06, 58-07, 60-sanitize]

tech-stack:
  added: []
  patterns:
    - "Structural-blindness detector runs BEFORE any absence-claim verdict
      and gates the whole block's ability to say clean (D-02a) -- a
      positive finding is never weakened by the same blindness"
    - "Iteration-ceiling loop (scanAllOccurrences) bounded by an explicit
      count parameter, not context, for the one loop whose length is
      driven by the FILE'S content (repeated-token count) rather than by
      an already-bounded window size alone"
    - "Cross-part byte budget (budgetLeft) threaded through a multi-entry
      zip scan, with io.LimitReader fed the REMAINING budget per part, not
      a fresh per-part allowance every time"
    - "A read that exactly filled its LimitReader allowance is treated as
      a possible truncation (unknown/work_ceiling_bytes), never as a
      completed, clean scan"

key-files:
  created:
    - internal/inspect/pdf.go
    - internal/inspect/pdf_test.go
    - internal/inspect/container.go
    - internal/inspect/container_test.go
  modified: []

key-decisions:
  - "PDF /Encrypt is downgraded to unknown/compressed_object_streams under
    /ObjStm exactly like every other PDF code, even though ISO 32000
    forbids the encryption dict inside an /ObjStm -- that restriction was
    cross-verified via qpdf docs, not read directly from ISO 32000
    (Assumptions Log A5, MEDIUM-HIGH not HIGH confidence), and the
    conservative direction on a MEDIUM-HIGH claim is to downgrade anyway,
    not carve out an exception"
  - "PDF page count's incremental-update fast path takes the LAST
    occurrence of /Type/Pages (or /Type /Pages) with a legible /Count
    nearby, not the first -- the revision closest to EOF is the most
    likely to be current, without ever summing/counting occurrences (the
    forbidden fallback)"
  - "container.go's external-reference scan tracks a per-request byte
    budget across MULTIPLE opened parts (budgetLeft), not just a flat
    per-part cap -- discovered mid-implementation that a part read which
    exactly fills its LimitReader allowance must report
    unknown/work_ceiling_bytes rather than clean, since the read may have
    been cut short before it could have found anything (see Deviations)"
  - "archive/zip's own checksumReader independently refuses to return more
    bytes than a file's declared UncompressedSize64 (reader.go's
    'nread > f.UncompressedSize64 -> ErrFormat') -- confirmed empirically
    while designing TestAnalyzeContainerNeverAllocatesOnDeclaredSize. This
    means a part that LIES about being small while actually being huge is
    blocked by the stdlib itself, independent of this package's own
    declared-size skip check; the skip check's real value is avoiding the
    decompression/CPU cost of opening such a part at all, not a data leak"

patterns-established:
  - "Any future untrusted-byte parser in internal/inspect should reuse
    readWindow/scanAllOccurrences's iteration-ceiling shape rather than
    inventing a new bounding primitive per file"

requirements-completed: [INSP-01, INSP-02, INSP-03]

duration: ~2h
completed: 2026-08-10
---

# Phase 58 Plan 04: PDF and ZIP-Container Analyzers Summary

**AnalyzePDF and AnalyzeContainer: PDF /ObjStm-aware findings scanner that never claims clean when structurally blind, plus a ZIP-container analyzer reusing convert.SniffContainer for macros/zip-bomb and adding bounded external-reference + xlsx sheet-count detection**

## Performance

- **Duration:** ~2h
- **Tasks:** 2 planned + 1 mid-implementation correctness fix
- **Files created:** 4 (pdf.go, pdf_test.go, container.go, container_test.go)

## Accomplishments

- `AnalyzePDF` detects `/ObjStm`/`/Type/XRef` FIRST and downgrades every
  non-firing PDF-domain finding code to `unknown`/`compressed_object_streams`
  instead of ever claiming `clean` (D-02a) -- proven by
  `TestAnalyzePDFObjStmNeverReportsClean`, whose failure message names D-02a
  explicitly, and by a live inversion (see below).
- A positive token match (e.g. `/JavaScript`) survives the same `/ObjStm`
  presence and remains a Finding -- `TestAnalyzePDFFindingSurvivesObjStm`.
- PDF page count is extracted only via the `/Type/Pages` + nearby `/Count`
  fast path (taking the LAST plausible match, closest to EOF); the naive
  `/Type /Page` occurrence-counting fallback is explicitly never
  implemented (verified by acceptance grep) because it overcounts
  incrementally-updated files (58-RESEARCH.md Pitfall 3).
- `AnalyzeContainer` reuses `convert.SniffContainer` as-is (called exactly
  once) for macro presence and the zip-bomb-shape signal; the only new
  parsing is external-reference detection (OOXML `.rels`
  `TargetMode="External"`; ODF/epub `xlink:href` with a network/file
  scheme) and xlsx sheet count from `xl/workbook.xml`.
- Every read in both files is bounded: `readWindow`/`maxTokenScanIterations`
  in pdf.go; `maxZipEntriesScanned`/`maxRelsPartsScanned`/`maxRelsPartBytes`
  plus a cross-part `io.LimitReader` budget in container.go. No buffer in
  either file is ever sized from a declared/attacker-controlled length.
- Six inversions performed (three per task) proving each new defense is
  load-bearing, not decorative -- verbatim failure text below.

## Task Commits

1. **Task 1: PDF analyzer with compression-detector priority (D-02a)** - `1118fec` (feat)
2. **Task 2: ZIP-container analyzer (macros, bomb shape, external refs)** - `c976d6b` (feat)
3. **Correctness fix found while writing Task 2's tests** - `05db7e4` (fix)

## Files Created/Modified

- `internal/inspect/pdf.go` (465 lines) - `AnalyzePDF`; compression-signal
  detector, risk-token scan, page-count fast path, all reads via
  `readWindow`
- `internal/inspect/pdf_test.go` (534 lines) - fixtures built as in-code
  byte literals; behavior + inversion coverage
- `internal/inspect/container.go` (444 lines after the fix commit) -
  `AnalyzeContainer`; wraps `convert.SniffContainer`, adds external-ref scan
  and xlsx sheet count
- `internal/inspect/container_test.go` (roughly 500 lines after the fix
  commit) - fixtures via `archive/zip`, including a byte-patching helper for
  forging a declared-size lie, and an honest multi-part ODF fixture for the
  cross-part budget test

## Decisions Made

See `key-decisions` in frontmatter above. The most consequential one made
during execution (not pre-specified in the plan) was the cross-part byte
budget correctness fix -- documented fully under Deviations below.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `scanExternalReferences` reported `clean` instead of `unknown` when a part's read was truncated by the byte budget**

- **Found during:** Task 2, while writing
  `TestAnalyzeContainerLimitReaderBoundsCrossPartBudget` (a test whose
  *purpose* was to prove `io.LimitReader` is load-bearing, not to find a
  bug)
- **Issue:** When a candidate part's real content was longer than the
  remaining per-request budget, `io.LimitReader` correctly capped the read
  -- but if no external-reference marker appeared in the truncated prefix,
  the function silently returned `("", false)` (clean), even though more of
  that part's real content was never read. This is the exact same false-
  completeness mistake D-02a exists to prevent in pdf.go, just triggered by
  this file's own byte budget instead of a compression signal.
- **Fix:** Added a `truncatedAnyPart` flag: whenever a part's read fills
  its entire `io.LimitReader` allowance (`len(data) >= limit`) without
  finding a marker, the scan now remembers this and returns
  `ceilingHit=true` at the end instead of `false` if no earlier part found
  anything either. The resulting `Check` becomes
  `unknown`/`work_ceiling_bytes` rather than `clean`.
- **Files modified:** `internal/inspect/container.go` (function
  `scanExternalReferences`), `internal/inspect/container_test.go` (the
  test's own assertion was corrected from "want clean" to "want
  unknown/work_ceiling_bytes" -- the test's ORIGINAL assertion was itself
  wrong, matching the bug it happened to also exercise)
- **Verification:** `TestAnalyzeContainerLimitReaderBoundsCrossPartBudget`
  passes with the fix; re-running the `io.LimitReader`-removal inversion
  against the corrected code still fails as expected (see inversion 6
  below)
- **Committed in:** `05db7e4`

---

**Total deviations:** 1 auto-fixed (Rule 1 - bug found via test design, not via the plan's own acceptance criteria)
**Impact on plan:** Strictly a correctness improvement inside this plan's own scope (container.go, same function the plan specified) -- no scope creep, no new files, no new dependencies.

## Issues Encountered

**Zip-forgery pitfall discovered while designing `TestAnalyzeContainerNeverAllocatesOnDeclaredSize`'s second scenario.** An early attempt to prove `io.LimitReader` matters by forging a part whose declared `UncompressedSize64` LIES SMALL while its real decompressed content is large ran into `archive/zip`'s own `checksumReader`, which refuses to return more bytes than the file's declared `UncompressedSize64` (`reader.go`: `if r.nread > r.f.UncompressedSize64 { return 0, ErrFormat }`). That specific attack shape (declared-small, actually-huge) is therefore blocked by the standard library itself, independent of this package's code -- so the test for `io.LimitReader`'s real value was redesigned around a cross-part budget with two entirely honest, truthfully-declared parts (`odtFixtureWithTwoParts`) instead, which is what surfaced the Rule-1 bug above.

## Inversions (D-16), verbatim failure text

Six inversions were performed (temporarily break the defense, confirm the target test fails with a message naming the mechanism, then revert). All were reverted immediately after confirming failure; final state is 100% the intended defense.

### Task 1 (pdf.go)

**1. Main inversion (D-02a): removed the `compressed ||` branch from `verdictStatus` (would let a non-firing PDF code answer `clean` under `/ObjStm`).**

```
pdf_test.go:236: D-02a violated: code "pdf_javascript" reported StatusClean on a PDF with /ObjStm present -- a compressed-object-stream PDF must never answer clean
```

**2. Removed the `off >= size` bounds guard from `readWindow` (window.go).**

```
window_test.go:139: readWindow(off=1073741824, n=1, size=5) panicked: runtime error: makeslice: len out of range
```

**3. Removed the `maxTokenScanIterations` loop cap from `scanAllOccurrences` (changed `for i := 0; i < max; i++` to an unconditional `for {}`).**

```
pdf_test.go:512: scanAllOccurrences ceilingHit = false, want true (buffer has more occurrences than the cap)
```

### Task 2 (container.go)

**4. Removed the `ReasonWorkCeilingBytes` downgrade on `maxZipEntriesScanned` exhaustion (made it return `StatusClean` instead).**

```
container_test.go:346: Check(CodeExternalReference).Status = clean, want unknown -- the entry-scan ceiling was exceeded, so clean is not an honest answer
```

**5. Made `cr.DuplicateRootPart` inert (`if false && cr.DuplicateRootPart`).**

```
container_test.go:307: Check("ooxml_macro").Status = clean, want unknown for a DuplicateRootPart container
```

**6. Removed `io.LimitReader` from the external-reference part read (`io.ReadAll(rc)` instead of `io.ReadAll(io.LimitReader(rc, limit))`) -- performed once against the pre-fix code and again against the corrected code after the Rule-1 fix above; both times the same test failed for the same reason.**

```
container_test.go:445: got Finding {Code:external_reference Severity:warning Location:styles.xml} with a 100-byte total budget -- io.LimitReader must stop the styles.xml read long before its tail-placed marker, using the budget REMAINING after content.xml was already read, not a fresh maxRelsPartBytes allowance per part
```

## D-17 goal check: every path that could print `clean` for content this code never actually saw, and how each is closed

The plan's stated phase goal (not just its text requirements) is: *the report never overclaims its own completeness.* Enumerated below are every path identified during design/testing where either analyzer could have answered `clean` without having actually seen the relevant bytes, and the mechanism that closes each one:

1. **Compressed object streams (PDF `/ObjStm`/`/Type/XRef`).** Closed by the compression-signal detector running first and gating `verdictStatus`/`verdictReason` for every PDF-domain code (`AnalyzePDF`). Proven by `TestAnalyzePDFObjStmNeverReportsClean` + inversion 1.
2. **Byte-ceiling truncation of the PDF head/tail window (`maxBytes < size`).** Closed by the `truncated` flag forcing `unknown`/`work_ceiling_bytes` for every PDF code not found within the actually-read window. Proven by `TestAnalyzePDFCeilingReasonDiffersFromCompressionReason`.
3. **Iteration-ceiling exhaustion during the PDF page-count multi-occurrence scan (`scanAllOccurrences`).** Closed by `maxTokenScanIterations` feeding `ceilingHit`, which `extractPDFPageCount` turns into `ReasonWorkCeilingBytes` rather than a guessed page count. Proven by `TestScanAllOccurrencesRespectsIterationCeiling` + inversion 3.
4. **An unopened or partially-read `.rels`/`content.xml`/`styles.xml` part in the container scan (entry-count ceiling, parts-opened ceiling, or per-request byte budget exhausted mid-read).** Closed by `scanExternalReferences`'s `ceilingHit` return value (including the Deviations-section fix for the mid-read-truncation case) forcing `unknown`/`work_ceiling_bytes` instead of `clean`. Proven by `TestAnalyzeContainerCeilingYieldsUnknownNotClean` + inversion 4, and `TestAnalyzeContainerLimitReaderBoundsCrossPartBudget` + inversion 6.
5. **An ambiguous container (`DuplicateRootPart`).** Closed by `duplicateRootPartContainerResult` forcing `unknown`/`malformed_input` on every container-domain code before any Characteristics are even published. Proven by `TestAnalyzeContainerDuplicateRootPartAllUnknown` + inversion 5.
6. **An input declaring a `size` larger than its actual byte count.** Both `readWindow` (pdf.go, via `err != nil && err != io.EOF` handling a short `ReadAt`) and `archive/zip.NewReader`'s own error path (container.go's `malformedContainerResult` on `zerr != nil`) fail closed to `unknown`/`malformed_input` rather than silently treating a truncated read as complete. Proven by `TestAnalyzePDFNeverPanics`'s "claims size larger than actual data" case and `TestAnalyzeContainerNeverPanics`'s "truncated zip" case (neither panics; neither analyzer's Characteristics ever assert a fact it could not have verified).

## Known Stubs

None -- both analyzers are fully wired; no placeholder/mock data paths exist. `internal/inspect`'s AnalyzePDF and AnalyzeContainer are pure functions with no external dependencies (DB, network, etc.) requiring stubbing.

## Threat Flags

None. Every new parsing surface introduced by this plan (PDF token scan, ZIP `.rels`/`content.xml`/`styles.xml`/`xl/workbook.xml` reads) was already named in the plan's own `<threat_model>` (T-58-16 through T-58-22) and is mitigated exactly as that table specifies -- no new trust boundary or attack surface was discovered outside it.

## Next Phase Readiness

- `AnalyzePDF` and `AnalyzeContainer` both return `FamilyResult` and are ready for 58-06 (API wiring) to call based on detected format, alongside 58-05's media/image analyzers.
- `go.mod`/`go.sum` and `internal/convert/` are untouched by this plan (verified via `git diff --stat`) -- D-01/D-02's "stdlib only, in this process" constraint holds.
- The cross-part budget correctness fix (Deviation 1) is a precedent worth remembering for 58-05's own analyzers if they ever scan multiple candidate parts/boxes against a shared byte budget: "read exactly up to the cap without finding anything" must map to `unknown`, not `clean`.

## Self-Check: PASSED

- FOUND: internal/inspect/pdf.go
- FOUND: internal/inspect/pdf_test.go
- FOUND: internal/inspect/container.go
- FOUND: internal/inspect/container_test.go
- FOUND: 1118fec (feat(58-04): add PDF analyzer with compressed-object-stream downgrade (D-02a))
- FOUND: c976d6b (feat(58-04): add ZIP-container analyzer for macros, bomb shape, external refs)
- FOUND: 05db7e4 (fix(58-04): report unknown, not clean, when a truncated .rels read finds nothing)
- Verified: `go test ./internal/inspect/... -count=1 -race` green; `go test ./... -count=1` green; `go vet ./...` clean; `gofmt -l internal/inspect` empty; `git diff --stat -- go.mod go.sum internal/convert/` empty

---
*Phase: 58-synchronous-inspection*
*Completed: 2026-08-10*
