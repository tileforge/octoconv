---
phase: quick/260810-r2i
plan: 01
subsystem: api
tags: [pdf, inspect, security-review, false-negative, denial-of-service, coverage]

# Dependency graph

requires:
  - phase: 58-synchronous-inspection
    provides: internal/inspect's FamilyResult/Check/Coverage three-value status model (D-02a/INSP-03) this task's fixes operate inside
  - phase: quick/260810-sle
    provides: round 1's fixes for the same defect class (PDF hex-escape, stream-body filtering, image window truncation, container declared-size skip, characteristic-level coverage) -- round 2 closes the class round 1 left half-swept
provides:
  - "pdf.go: scanTokenOccurrences, one primitive replacing firstTokenLocation/firstTokenLocationOutsideOpaqueRanges, that requires a real PDF name boundary (rejects '/JS' inside '/JScript'), classifies opaque-range-suppressed matches as unknown (not clean, not critical), and caps its own occurrence walk independent of range count"
  - "pdf.go: scanPDFOpaqueRanges now also marks literal '(...)' and hex '<...>' PDF strings opaque (excluding '<<' dict delimiters), closing the '(documentation says /JS)' false-positive class"
  - "pdf.go: mergeOpaqueRanges + binary-search inOpaqueRange, closing the occurrences x ranges quadratic DoS the round 1 fix itself introduced"
  - "container.go: scanExternalReferences preserves f.Open and io.ReadAll errors (was silently continue-ing past both) instead of reporting clean on an unopenable/partially-unreadable candidate part"
  - "container.go: extractXLSXSheetCount (audit finding, unnamed by either review round) gets the identical fix -- declared-size skip and open/read errors now report unknown/work_ceiling_bytes for sheet_count, not malformed_input"
  - "image.go: scanJPEGMetadata/scanPNGMetadata/scanRIFFChunkMetadata report ceilingHit alongside found, closing the gap where maxMetadataMarkerScanIterations exhaustion inside a FULLY-buffered window still reported clean"
  - "media.go (audit finding, never named by either review round): isobmffCharacteristics's mvhd/trak/mdia/minf/stbl/stsd lookups, wavCharacteristics's chunk walk, and ebmlCharacteristics's Info/Duration/Tracks lookups all now distinguish THIS SCAN's own iteration/byte ceiling from a genuine structural fact about the file -- previously all three either misattributed a ceiling to malformed_input (isobmff, wav) or misattributed a genuine structural miss to work_ceiling_bytes unconditionally (ebml, the opposite-direction defect)"

affects: [60-sanitize-verifier, any-future-inspect-followup]

# Tech tracking

tech-stack:
  added: []
  patterns:
    - "Exit-path table as a mandatory pre-fix artifact: before patching a named finding, enumerate every return statement of every function the finding touches and classify it (found / stopped-early-ceiling / genuinely-absent), so a fix closes the CLASS the finding names, not just the one instance the reviewer pointed at"
    - "ceilingHit as a first-class return value threaded through a call chain (walkTopLevelBoxes -> firstChildBox -> findTrackDimensions/findDeclaredCodec -> isobmffCharacteristics; walkEBMLElements -> firstEBMLChild -> findEBMLDuration/findEBMLCodecID -> ebmlCharacteristics) rather than collapsed at the first call site -- lets the OUTERMOST caller, which is the only one that knows whether the whole descent needs to report a ceiling, make that decision correctly even when the ceiling fires several calls deep"
    - "windowCoversWholeFile as an explicit boolean distinguishing 'our byte window did not reach the whole file' from 'it did, and a miss inside it is a genuine fact' -- the same variable name/shape reused across image.go, media.go's ISOBMFF and EBML paths, and container.go's WAV-analog sheet-count path"

key-files:
  created: []
  modified:
    - internal/inspect/pdf.go
    - internal/inspect/pdf_test.go
    - internal/inspect/container.go
    - internal/inspect/container_test.go
    - internal/inspect/image.go
    - internal/inspect/image_test.go
    - internal/inspect/media.go
    - internal/inspect/media_test.go

key-decisions:
  - "PDF T-R2I-01 (quadratic DoS): fixed with BOTH a binary search (removes the range-count multiplier) AND a per-token occurrence cap in scanTokenOccurrences (bounds the occurrence-count factor independently) -- the plan's own action text required both, reasoning that occurrence count is attacker-controlled independently of range count even once the multiplier is gone."
  - "PDF T-R2I-02 (lexical context): a match failing the name-boundary check (e.g. '/JS' inside '/JScript') is NOT the same case as a match inside an opaque range -- the former is definitively not the target token and safely reports clean (subject to the ordinary verdict rule); only the latter is genuinely ambiguous and reports unknown/pdf_stream_body_token. Collapsing these two into one 'filtered' bucket would have made /JScript-only files report unknown, an unnecessary and incorrect loss of a legitimate clean answer."
  - "firstTokenLocation and firstTokenLocationOutsideOpaqueRanges were deleted and replaced by one scanTokenOccurrences primitive, rather than keeping both and adding the boundary check to each separately -- the original split let AnalyzePDF's matchedInsideOpaque fallback (which called firstTokenLocation, unrestricted) skip the boundary rule the primary scan enforced, which is exactly how a fix to one function without the other would have reintroduced a milder version of the /JScript bug through the fallback path alone."
  - "Container.go and media.go audit findings (extractXLSXSheetCount, isobmffCharacteristics's box lookups, wavCharacteristics, ebmlCharacteristics) were fixed even though neither appears in the plan's files_modified frontmatter or named acceptance criteria list for pdf.go/container.go/image.go -- the plan's own <read_this_before_you_start> and Task 2 acceptance criteria explicitly instruct auditing media.go 'наравне с остальными' and fixing any exit that fails to distinguish ceiling-hit from structural fact, 'даже если ревьюер его не назвал'. Documented here as a deviation from the frontmatter's file list, not from the plan's actual instructions."
  - "ebmlCharacteristics's fix is the ONE case in this round that runs the opposite direction from every other fix here: it was ALREADY reporting every miss as unknown/work_ceiling_bytes (never falsely clean), just for the wrong reason (always the ceiling, even when the window covered the whole file and no internal walk was capped). This is not a false-negative safety defect -- it is a false-incomplete-coverage defect, over-conservative rather than under-conservative. Fixed anyway because the exit-path table this round mandates does not distinguish 'safe direction' from 'still doesn't distinguish the two facts' -- both are the same class of unaudited ambiguity, just with a different blast radius."

requirements-completed: [PR9R2-F1-OPAQUE-RANGE-QUADRATIC, PR9R2-F2-PDF-LEXICAL-CONTEXT, PR9R2-F3-CONTAINER-READ-ERRORS, PR9R2-F4-IMAGE-ITERATION-CAP]

# Metrics

duration: ~55min
completed: 2026-08-11
audit_acknowledged:
  milestone: v1.15
  at: 2026-09-30
  status: unknown
---

# Quick Task 260810-r2i: Round 2 Inspect Exit-Path Sweep Summary

**Closed all four named codex PR #9 round 2 findings in `internal/inspect` (PDF opaque-range quadratic DoS + lexical context, container part-open/read error swallowing, image metadata iteration-ceiling blindness) by replacing point patches with an exit-path table across `pdf.go`/`container.go`/`image.go`/`media.go`/`report.go`, and used that same table to find and fix three more instances of the identical class in `container.go`'s `extractXLSXSheetCount` and `media.go`'s ISOBMFF/WAV/EBML characteristic extractors that neither review round had named.**

## Performance

- **Duration:** ~55 min (four task commits, `2026-08-10T23:46` to `2026-08-11T00:15`)
- **Tasks:** 2 plan tasks (as scoped) + 2 audit-driven follow-on commits (media.go, container.go's sheet-count), all within the plan's stated method ("sweep the class, not the instance")
- **Files modified:** 8 (`internal/inspect/{pdf,pdf_test,container,container_test,image,image_test,media,media_test}.go`)

## Method Used (per the plan's explicit instruction)

Before patching anything, the exit-path table below was built by reading every function in `pdf.go`, `container.go`, `image.go`, `media.go`, and `report.go` and classifying every `return` statement. This is the deliverable the plan calls "the evidence that the class is swept, not that three named holes are patched" -- every row marked **FIXED** in this table is a defect this session closed; every row marked **clean** was verified, not assumed, to already distinguish "scanned everything, found nothing" from "stopped early."

### Exit-path table

#### `pdf.go`

| Function | Exit | Distinguishes ceiling-vs-absent? | Status |
|---|---|---|---|
| `AnalyzePDF` | `size<=0 \|\| maxBytes<=0` -> `malformedPDFResult` | N/A (never reads; honestly reports malformed) | clean |
| `AnalyzePDF` | `!headOK` -> `malformedPDFResult` | N/A (read itself failed) | clean |
| `AnalyzePDF` per-code loop | `found` -> Finding | presence claim, not absence -- never degraded | clean |
| `AnalyzePDF` per-code loop | `ceilingHit` -> unknown/work_ceiling_bytes | **yes, as of this session** | **FIXED** (was: no such branch existed; occurrence-cap exhaustion fell through to the verdict rule below, i.e. could report `clean`) |
| `AnalyzePDF` per-code loop | `filtered` -> unknown/pdf_stream_body_token | yes (round 1 for streams/comments; this session extended to literal/hex strings) | clean (extended) |
| `AnalyzePDF` per-code loop | none of the above -> `verdictStatus`/`verdictReason` | yes (compressed / hexEscape / truncated / else clean) | clean (round 1) |
| `verdictStatus`/`verdictReason` | 4-way branch | yes, by construction | clean |
| `malformedPDFResult` | single path, all unknown/malformed_input | N/A, never clean | clean |
| `containsCompressionSignal` | bool only | small fixed token set, no ceiling needed | clean |
| `containsPDFNameHexEscape` | bool only | single bounded pass, no ceiling needed | clean |
| `scanPDFStreamRanges`/`scanPDFCommentRanges`/`scanPDFLiteralAndHexStringRanges` | capped by `maxTokenScanIterations`; cap exhaustion silently under-reports ranges | **known, pre-existing, opposite-direction risk**: under-detecting an opaque range can at most produce a false POSITIVE Finding (a real match inside a stream this scan failed to mark opaque), never a false CLEAN -- the file's own doc comment on over-approximation already accepts this asymmetry as safe. Not this round's named class; not fixed. |
| `mergeOpaqueRanges` | pure sort+merge | no ambiguity | clean |
| `inOpaqueRange` | binary search over merged ranges | no ambiguity (verified by inversion) | **FIXED** (was: linear scan, the quadratic-DoS finding) |
| `scanTokenOccurrences` | found / filtered / ceilingHit / none | all four distinguished | **FIXED** (replaces two functions that, between them, let the boundary-check and opaque-check diverge) |
| `extractPDFPageCount` | compressed/hexEscape short-circuit; ceilingHit\|\|truncated -> work_ceiling_bytes; else malformed_input | yes (round 1) | clean |
| `findCountAfter`/`extractPDFVersion` | bool only | bounded window, no ambiguity | clean |
| `scanAllOccurrences` | explicit `ceilingHit` return | yes (round 1) | clean |

#### `container.go`

| Function | Exit | Distinguishes ceiling-vs-absent? | Status |
|---|---|---|---|
| `AnalyzeContainer` | `SniffContainer` error -> not_applicable | structural: genuinely not a zip | clean |
| `AnalyzeContainerWith` | `DuplicateRootPart` -> unknown/malformed_input | ambiguous zip, never clean | clean |
| `AnalyzeContainerWith` | `zip.NewReader` error after successful sniff -> unknown/malformed_input | internal inconsistency, fails closed | clean |
| `appendMacroResult` | trusts `cr.HasMacro` (computed once, elsewhere) | decision already made upstream; no ambiguity here | clean |
| `findMacroPartName` | ceiling hit -> empty Location string | Finding is still emitted either way (HasMacro already true); cosmetic gap only, doc-commented | clean (not this class) |
| `appendBombResult` | pure comparison | no ambiguity | clean |
| `appendExternalReferenceResult` | loc!="" / ceilingHit / clean | yes, 3-way | clean (round 1 + round 2 fixed the source) |
| `scanExternalReferences` | entry-count cap -> ceilingHit | yes (round 1) | clean |
| `scanExternalReferences` | parts-opened cap -> ceilingHit | yes (round 1) | clean |
| `scanExternalReferences` | declared-size skip -> `truncatedAnyPart=true` | yes (round 1, T-SLE-04) | clean |
| `scanExternalReferences` | budget exhausted before open -> ceilingHit | yes (round 1) | clean |
| `scanExternalReferences` | **`f.Open()` error -> was `continue` (swallowed)** | **no** | **FIXED this session** -- now `truncatedAnyPart=true` |
| `scanExternalReferences` | marker found -> Finding | presence claim | clean |
| `scanExternalReferences` | **`io.ReadAll` error -> was discarded (`_`)** | **no** | **FIXED this session** -- now `truncatedAnyPart=true` |
| `scanExternalReferences` | `len(data)>=limit` -> `truncatedAnyPart=true` | yes (round 1) | clean |
| `scanExternalReferences` | loop ends, `truncatedAnyPart==false` -> genuinely clean | yes, by elimination of every other case above | clean |
| `isExternalRefCandidate`/`containsExternalReference` | pure predicates | no ambiguity | clean |
| `setSheetAndPageCharacteristics` (xlsx branch) | **delegated to `extractXLSXSheetCount`'s `!ok` -> always malformed_input** | **no** | **FIXED this session** (audit finding, unnamed by either round) |
| `extractXLSXSheetCount` | **declared-size skip -> was `(0,false)`, no ceiling signal** | **no** | **FIXED this session** |
| `extractXLSXSheetCount` | **`f.Open()` error -> was `(0,false)`** | **no** | **FIXED this session** |
| `extractXLSXSheetCount` | **`io.ReadAll` error -> was discarded** | **no** | **FIXED this session** |
| `extractXLSXSheetCount` | `workbook.xml` genuinely absent -> `(0,false,false)` | yes, correctly malformed_input | clean |
| `setSheetAndPageCharacteristics` (ods/office branches) | unconditional parser_unavailable | D-01 honest gap, not a ceiling | clean |
| `countSheetElements` | pure count over already-bounded data | no ambiguity | clean |

#### `image.go`

| Function | Exit | Distinguishes ceiling-vs-absent? | Status |
|---|---|---|---|
| `AnalyzeImage` (pixel-bomb) | `!dimsKnown` -> unknown/malformed_input | yes (T-58-26, pre-existing) | clean |
| `AnalyzeImage` (pixel-bomb) | bomb / clean | pure comparison | clean |
| `AnalyzeImage` (metadata) | found / metaWindowTruncated / clean | yes, 3-way | clean (this session extends what feeds `metaWindowTruncated`) |
| `scanImageMetadata` | `!ok` (readWindow failed) -> windowTruncated only | pre-existing, correct | clean |
| `scanImageMetadata` (jpg/png/webp cases) | **byte-window truncation only, never the walker's own iteration ceiling** | **no** | **FIXED this session**: now `windowTruncated \|\| ceilingHit` |
| `scanImageMetadata` (tiff/heic/default) | byte-window truncation only | correct -- these walkers have no internal iteration cap of their own (tiff: single header check; heic: `bytes.Contains`, no loop) | clean |
| `scanJPEGMetadata` | found / EOI-SOS reached / malformed segment / **iteration cap** | **iteration cap previously fell through as an ordinary miss** | **FIXED this session** -- now returns explicit `ceilingHit` |
| `scanPNGMetadata` | found / declared-length-overrun / **iteration cap** | same defect | **FIXED this session** |
| `scanRIFFChunkMetadata` | found / declared-size-overrun / **iteration cap** | same defect (reviewer's own "same shape" phrasing) | **FIXED this session** |
| `scanISOBMFFMetadataTokens` | `bytes.Contains`, no loop | no ceiling exists to hide | clean |

#### `media.go` (never named by either review round -- audited this session per the plan's explicit instruction)

| Function | Exit | Distinguishes ceiling-vs-absent? | Status |
|---|---|---|---|
| `AnalyzeMedia` | format dispatch | no ambiguity, pure switch | clean |
| `isobmffCharacteristics` | `moovPayload==nil` -> `findMoov`'s own reason | yes (pre-existing D-02a-style split) | clean |
| `isobmffCharacteristics` | **`firstChildBox(moovPayload,"mvhd")` miss -> was unconditional malformed_input** | **no** | **FIXED this session** |
| `isobmffCharacteristics` | **`findTrackDimensions` miss -> was unconditional malformed_input** | **no** | **FIXED this session** |
| `isobmffCharacteristics` | **`findDeclaredCodec` miss -> was unconditional malformed_input** | **no** | **FIXED this session** |
| `findMoov` | head window hit -> found | presence, no ambiguity | clean |
| `findMoov` | tail window hit -> found | presence, no ambiguity | clean |
| `findMoov` | **both windows exhausted, `headActual+tailActual>=size` -> malformed_input, ignoring whether the HEAD window's own box-count cap fired** | **no** | **FIXED this session** (`headCeilingHit` now gates this branch) |
| `findMoov` | otherwise -> work_ceiling_bytes | yes | clean |
| `findTrackDimensions`/`findDeclaredCodec` | descent through `firstChildBox` chain | **previously collapsed all misses to plain `false`, discarding ceiling info** | **FIXED this session** -- now propagate `ceilingHit` through every step |
| `walkTopLevelBoxes`/`firstChildBox` | box-count cap vs malformed/truncated box vs `fn` stopping vs buffer end | **previously one undifferentiated `false`/not-found for all four** | **FIXED this session** -- `ceilingHit` now isolates the cap case specifically |
| `ebmlCharacteristics` | `!ok` (readWindow failed) | unconditional work_ceiling_bytes -- pre-existing, not touched (out of this round's named scope; zero bytes read is already the safe direction) | clean (unchanged) |
| `ebmlCharacteristics` | **Segment/Duration/CodecID miss -> was UNCONDITIONALLY work_ceiling_bytes, even for a small, fully-buffered, genuinely malformed file** | **no (opposite-direction defect: over-claims incompleteness, never falsely claims completeness)** | **FIXED this session** via `windowCoversWholeFile` + `ceilingHit` |
| `findEBMLDuration`/`findEBMLCodecID` | descent through `firstEBMLChild` chain | **previously collapsed to plain `false`** | **FIXED this session** -- propagate `ceilingHit` |
| `walkEBMLElements`/`firstEBMLChild` | element-count cap vs malformed vint vs `fn` stopping vs buffer end | **previously one undifferentiated `false`** | **FIXED this session** |
| `wavCharacteristics` | RIFF/WAVE header malformed -> was unconditional malformed_input | **no** (byte-window truncation of the header itself was possible) | **FIXED this session** via `windowTruncated` |
| `wavCharacteristics` | **fmt/data chunk walk miss -> was unconditional malformed_input, collapsing byte-window truncation AND `maxRIFFChunks` exhaustion** | **no** | **FIXED this session** via `windowTruncated \|\| ceilingHit` |
| `unsupportedAudioDurationCharacteristics`/`unsupportedVideoCharacteristics` | unconditional parser_unavailable | D-01 honest gap for mp3/ogg/avi, not a ceiling | clean |
| `parseMVHD`/`parseTKHD`/`parseSTSDCodec` | bounded, fixed-offset decodes | no ambiguity (malformed box already handled by the caller) | clean |
| `ebmlReadIDVint`/`ebmlReadSizeVint`/`ebmlReadUint`/`ebmlReadFloat` | bounded decodes | no ambiguity | clean |

#### `report.go` (audited, no defect found)

| Function | Exit | Distinguishes ceiling-vs-absent? | Status |
|---|---|---|---|
| `mergeCharacteristics`/`mergeFindings` | pure merges, "first wins" on collision | no per-code status decided here | clean |
| `buildChecks` | finding-owned / family-owned Check / unrecognized-format fallback / not_applicable fallback | this is the ASSEMBLY layer -- it trusts each family's own already-correct Check, never re-derives clean/unknown itself | clean |
| `buildCoverage` (per-Check loop) | marks `Coverage.Complete=false` iff `Check.Reason` is one of the two ceiling reasons | yes, exactly the mechanism this whole round's fixes feed correctly now that every family reports the right Reason | clean |
| `buildCoverage` (characteristics loop) | same, scanning `"<name>_reason"` keys | yes (round 1, T-SLE-05) -- and this is exactly the mechanism that makes `media.go`'s `duration_seconds_reason`/`sheet_count_reason` fixes this session actually observable at the report level | clean |
| `buildCoverage` (tracker fallback) | `tr.Exhausted()` sets Complete=false if nothing above already did | yes, narrower safety net, documented as such | clean |
| `degradeCoverageOnInvariantViolation` | `ValidateReport` failure -> Coverage forced incomplete | fail-safe on THIS package's own bug, not a file fact | clean |

**Why the fixes in `pdf.go`/`container.go`/`image.go` matter to `report.go` even though `report.go` itself needed no code change:** `buildCoverage`'s characteristics loop only sees a ceiling if the analyzer that produced the characteristic labeled it correctly. Every media.go/container.go fix in this session is exactly what makes that already-correct assembly-layer mechanism actually fire for `duration_seconds_reason`, `declared_codec_reason`, `width_reason`, and `sheet_count_reason` -- before this session, those characteristics could carry `malformed_input` (isobmff/wav) or `work_ceiling_bytes` unconditionally (ebml) regardless of whether this scan's own resource limit was actually the cause, silently defeating `buildCoverage`'s own correct logic with wrong input.

## Accomplishments

- `AnalyzePDF`'s opaque-range filter is O(occurrences x log(ranges)) instead of O(occurrences x ranges): a 16384-range, near-1M-occurrence adversarial fixture that took >5s under the reverted linear scan now completes in ~0.07s.
- `AnalyzePDF` no longer promotes `/JScript` (a different, legitimate PDF name) to a critical `pdf_javascript` Finding by matching its `/JS` prefix as a bare substring -- a real PDF-name-boundary check (ISO 32000 SS7.3.5) is now required.
- `AnalyzePDF` no longer promotes a `/JS` token found only inside a literal `(...)`/hex `<...>` PDF string's DATA to a critical Finding, while still never falling back to `clean` for that occurrence.
- `AnalyzeContainer`'s external-reference scan and `extractXLSXSheetCount` both now preserve `f.Open`/`io.ReadAll` errors instead of silently treating an unreadable candidate part the same as "found nothing."
- `AnalyzeImage`'s JPEG/PNG/WebP metadata walkers now report their own iteration-ceiling exhaustion, closing the gap where a fully-buffered file with enough harmless markers before an EXIF/XMP marker reported `clean` with complete coverage.
- `AnalyzeMedia`'s ISOBMFF/WAV/EBML characteristic extractors -- audited because the plan required it, not because either review round named them -- all now correctly separate THIS SCAN's own ceiling from a genuine fact about the file, in both directions (isobmff/wav were under-claiming ceilings; ebml was over-claiming them).

## Task Commits

Each task/finding was committed atomically:

1. **Task 1: PDF opaque-range binary search + occurrence cap + lexical context (name boundary + literal/hex strings)** - `859c271` (fix)
2. **Task 2: Container part-open/read errors + image metadata iteration ceilings** - `cd36a89` (fix)
3. **Audit follow-on: media.go's own instances of the ceiling-vs-structural-fact class** - `f13e3f0` (fix)
4. **Audit follow-on: container.go's extractXLSXSheetCount, same class as scanExternalReferences** - `d866c0e` (fix)

## Files Created/Modified

- `internal/inspect/pdf.go` - Deleted `firstTokenLocation`/`firstTokenLocationOutsideOpaqueRanges`, replaced by `scanTokenOccurrences` (name-boundary check + opaque filtering + occurrence-cap in one primitive). Split `scanPDFOpaqueRanges` into `scanPDFStreamRanges`/`scanPDFCommentRanges`/new `scanPDFLiteralAndHexStringRanges`, added `mergeOpaqueRanges`, rewrote `inOpaqueRange` as a binary search. `AnalyzePDF`'s per-code loop now branches on found/ceilingHit/filtered explicitly.
- `internal/inspect/pdf_test.go` - Added `jscriptOnlyPDFFixture`, `jsInsideLiteralStringOnlyPDFFixture`, and five new tests (`TestAnalyzePDFJScriptNameDoesNotMatchJS`, `TestAnalyzePDFJSInsideLiteralStringIsUnknownNotCriticalNotClean`, `TestAnalyzePDFOpaqueRangeFilterIsNotQuadratic`, `TestAnalyzePDFOpaqueOccurrenceCeilingIsUnknownNotClean`). `TestAnalyzePDFObjStmNeverReportsClean`/`TestAnalyzePDFFindingSurvivesObjStm` left byte-for-byte unedited.
- `internal/inspect/container.go` - `scanExternalReferences`'s `f.Open`/`io.ReadAll` branches now set `truncatedAnyPart`; `extractXLSXSheetCount` gained a `ceilingHit` return and `setSheetAndPageCharacteristics` uses it.
- `internal/inspect/container_test.go` - Added `corruptCompressionMethod`, `corruptCompressedData` byte-patching helpers and three new tests (`TestAnalyzeContainerUnopenableRelsPartYieldsUnknownNotClean`, `TestAnalyzeContainerReadErrorMidPartYieldsUnknownNotClean`, `TestAnalyzeContainerSheetCountDeclaredSizeSkipIsWorkCeilingNotMalformed`).
- `internal/inspect/image.go` - `scanJPEGMetadata`/`scanPNGMetadata`/`scanRIFFChunkMetadata` now return `ceilingHit`; `scanImageMetadata` folds it into the truncated signal.
- `internal/inspect/image_test.go` - Added `TestAnalyzeImagePNGIterationCeilingIsUnknownNotClean`, `TestAnalyzeImageJPEGIterationCeilingIsUnknownNotClean`, `TestAnalyzeImageWebPIterationCeilingIsUnknownNotClean`, and a `riffChunk` fixture helper.
- `internal/inspect/media.go` - `walkTopLevelBoxes`/`firstChildBox`, `walkEBMLElements`/`firstEBMLChild` now return `ceilingHit`; `findTrackDimensions`/`findDeclaredCodec`/`findEBMLDuration`/`findEBMLCodecID` propagate it; `isobmffCharacteristics`, `findMoov`, `ebmlCharacteristics` (new `ebmlMissReason` helper, `windowCoversWholeFile`), and `wavCharacteristics` (new `windowTruncated`/`ceilingHit` locals) all use it to pick the right Reason.
- `internal/inspect/media_test.go` - Added `TestAnalyzeMediaISOBMFFChildBoxIterationCeilingIsWorkCeilingBytes`, `TestAnalyzeMediaWAVWindowTruncationIsWorkCeilingBytes`, `TestAnalyzeMediaWAVIterationCeilingIsWorkCeilingBytes`, `TestAnalyzeMediaEBMLSmallMalformedFileIsMalformedNotWorkCeiling`.

## Decisions Made

See `key-decisions` in frontmatter above. In addition:

- Deliberately did NOT touch `scanPDFStreamRanges`/`scanPDFCommentRanges`/`scanPDFLiteralAndHexStringRanges`'s own `maxTokenScanIterations` cap exhaustion (which under-detects opaque ranges rather than over-detecting them) -- this is the opposite-direction risk from every other fix in this round (a false Finding, never a false clean/complete claim) and is explicitly accepted by the file's own pre-existing over-approximation-is-safe doc comment. Documented in the exit-path table as reviewed, not silently skipped.
- Deliberately did NOT change `ebmlCharacteristics`'s `!ok` (readWindow itself failing) branch, which still unconditionally reports `work_ceiling_bytes` -- zero bytes read is already the maximally safe/conservative answer for that specific case, and changing it was outside this round's named findings.

## Deviations from Plan

### Auto-fixed Issues (Rule 2 - missing critical functionality, surfaced by the plan's own audit mandate)

**1. [Rule 2] `media.go`'s ISOBMFF/WAV/EBML ceiling-vs-structural-fact collapse**
- **Found during:** Task 2's mandated audit of `media.go` ("ревьюер не упоминал ни в одном круге... проверить наравне с остальными")
- **Issue:** `isobmffCharacteristics`'s `mvhd`/`trak`/`mdia`/`minf`/`stbl`/`stsd` lookups and `wavCharacteristics`'s chunk walk collapsed an iteration-ceiling/byte-window-truncation miss into `ReasonMalformedInput` (falsely marks Coverage complete); `ebmlCharacteristics` did the mirror-image collapse, always reporting `ReasonWorkCeilingBytes` even for a small, fully-buffered, genuinely malformed file (falsely marks Coverage incomplete).
- **Fix:** Threaded `ceilingHit` through `walkTopLevelBoxes`/`firstChildBox` and `walkEBMLElements`/`firstEBMLChild`; added `windowCoversWholeFile`/`ebmlMissReason` to `ebmlCharacteristics`; added `windowTruncated`/`ceilingHit` locals to `wavCharacteristics`.
- **Files modified:** `internal/inspect/media.go`, `internal/inspect/media_test.go`
- **Verification:** New tests pass; two inversions run (isobmff ceiling misattribution, ebml malformed-vs-ceiling collapse), both fail as expected pre-fix and pass post-revert.
- **Committed in:** `f13e3f0`

**2. [Rule 2] `container.go`'s `extractXLSXSheetCount` has the identical collapse `scanExternalReferences` already had before this session**
- **Found during:** applying the exit-path table method to `container.go` a second time, after fixing `scanExternalReferences`'s named findings
- **Issue:** Declared-size skip, `f.Open` error, and `io.ReadAll` error all reported `sheet_count_reason=malformed_input`.
- **Fix:** `extractXLSXSheetCount` gained a `ceilingHit` return; `setSheetAndPageCharacteristics` picks `ReasonWorkCeilingBytes` when it fires.
- **Files modified:** `internal/inspect/container.go`, `internal/inspect/container_test.go`
- **Verification:** New test + inversion (fails pre-fix, passes post-revert).
- **Committed in:** `d866c0e`

---

**Total deviations:** 2 auto-fixed (both Rule 2, both directly mandated by the plan's own audit instruction rather than outside it)
**Impact on plan:** Both fixes are the plan's own stated purpose ("sweep the class, not the instance") executed one level further than the plan's `files_modified` frontmatter literally listed. No scope creep beyond `internal/inspect/*` (the worktree's declared touch boundary).

## Issues Encountered

- `corruptCompressedData` (container_test.go) initially computed `compSize` from the LOCAL file header, which `archive/zip.Writer` leaves at 0 for streamed writes (the real size is only in the trailing data descriptor and the central directory). Diagnosed via a throwaway debug test confirming 0 bytes were actually changed; fixed by reading `compSize` from the CENTRAL DIRECTORY entry instead, which does carry the real, final size.
- The initial `TestAnalyzePDFOpaqueRangeFilterIsNotQuadratic` fixture (4096 ranges x 200,000 occurrences) passed even under the reverted linear-scan `inOpaqueRange` (0.51s, under the 5s deadline) -- the product was not yet large enough to demonstrate the quadratic cost within a reasonable test deadline. Increased to 16384 ranges x ~1,048,476 occurrences (near `maxTokenScanIterations`), which reliably exceeds 5s under the linear scan and completes in ~0.07s under the fix.

## User Setup Required

None - no external service configuration required.

## Verification

Per the plan's `<verification>` block, run after all commits:

```
$ go build ./... && go vet ./...
(clean, no output)

$ gofmt -l internal/inspect
(clean, no output)

$ go test ./... -count=1 -race
ok for all 25 testable packages, including internal/inspect (6.69s) and internal/api (6.29s)

$ git diff --stat -- go.mod go.sum
(empty -- zero new dependencies)

$ git rev-list --count HEAD -- internal/api/testdata/pre58_healthz.golden.json
1
$ git rev-list --count HEAD -- internal/api/testdata/pre58_formats.golden.json
1
$ git rev-list --count HEAD -- internal/api/testdata/pre58_createjob_rejections.golden.json
1
```

Exit-path table for every scanner in `internal/inspect` (`pdf.go`, `container.go`, `image.go`, `media.go`) plus the report assembler (`report.go`) is above -- every defect row is marked **FIXED**, every clean row was independently verified (not assumed) to already distinguish the two facts.

## Inversions (expected failure stated first, run, literal output pasted, reverted, confirmed green)

### T-R2I-01a -- PDF opaque-range binary search (quadratic DoS)

**Change:** `inOpaqueRange` -> reverted to the original per-occurrence linear scan over `ranges`.
**Expected:** `TestAnalyzePDFOpaqueRangeFilterIsNotQuadratic` fails (does not return within 5s).
**Actual output:**

```
=== RUN   TestAnalyzePDFOpaqueRangeFilterIsNotQuadratic
    pdf_test.go:568: AnalyzePDF did not return within 5s -- the opaque-range filter is quadratic in occurrences x ranges again
--- FAIL: TestAnalyzePDFOpaqueRangeFilterIsNotQuadratic (5.01s)
FAIL
```

Reverted; test passes in 0.07-0.08s.

### T-R2I-02a -- PDF name-boundary check (lexical context)

**Change:** `scanTokenOccurrences`'s `validBoundary` -> forced `true` unconditionally.
**Expected:** `TestAnalyzePDFJScriptNameDoesNotMatchJS` fails (a critical Finding is produced for `/JScript`).
**Actual output:**

```
=== RUN   TestAnalyzePDFJScriptNameDoesNotMatchJS
    pdf_test.go:490: got a critical Finding {Code:pdf_javascript Severity:critical Location:183} for /JScript, which is a different PDF name from /JS -- a substring match without a name-boundary check is exactly codex PR9 round 2's finding
--- FAIL: TestAnalyzePDFJScriptNameDoesNotMatchJS (0.00s)
FAIL
```

Reverted; full suite green again.

### T-R2I-03a -- container.go f.Open error preservation

**Change:** `scanExternalReferences`'s `f.Open` error branch -> reverted to bare `continue`.
**Expected:** `TestAnalyzeContainerUnopenableRelsPartYieldsUnknownNotClean` fails (reports `clean`).
**Actual output:**

```
=== RUN   TestAnalyzeContainerUnopenableRelsPartYieldsUnknownNotClean
    container_test.go:522: Check(CodeExternalReference).Status = clean, want unknown -- the only candidate part could not be opened, so clean is not an honest answer about the file
--- FAIL: TestAnalyzeContainerUnopenableRelsPartYieldsUnknownNotClean (0.00s)
FAIL
```

Reverted; full suite green again.

### T-R2I-03b -- container.go io.ReadAll error preservation

**Change:** `scanExternalReferences`'s `readErr != nil` check -> `false && readErr != nil` (disabled).
**Expected:** `TestAnalyzeContainerReadErrorMidPartYieldsUnknownNotClean` fails (reports `clean`).
**Actual output:**

```
=== RUN   TestAnalyzeContainerReadErrorMidPartYieldsUnknownNotClean
    container_test.go:588: Check(CodeExternalReference).Status = clean, want unknown -- the only candidate part's read failed partway through, so clean is not an honest answer
--- FAIL: TestAnalyzeContainerReadErrorMidPartYieldsUnknownNotClean (0.00s)
FAIL
```

Reverted; full suite green again.

### T-R2I-04a -- image.go PNG iteration ceiling

**Change:** `scanPNGMetadata`'s ceiling branch -> `return "", false, false` (ceilingHit forced false).
**Expected:** `TestAnalyzeImagePNGIterationCeilingIsUnknownNotClean` fails (reports `clean`).
**Actual output:**

```
=== RUN   TestAnalyzeImagePNGIterationCeilingIsUnknownNotClean
    image_test.go:216: check metadata_present status = clean, want unknown
--- FAIL: TestAnalyzeImagePNGIterationCeilingIsUnknownNotClean (0.00s)
FAIL
```

Reverted; full suite green again.

### T-R2I-04b -- image.go JPEG iteration ceiling

**Change:** `scanJPEGMetadata`'s ceiling branch -> `return "", false, false` (ceilingHit forced false).
**Expected:** `TestAnalyzeImageJPEGIterationCeilingIsUnknownNotClean` fails (reports `clean`).
**Actual output:**

```
=== RUN   TestAnalyzeImageJPEGIterationCeilingIsUnknownNotClean
    image_test.go:245: check metadata_present status = clean, want unknown
--- FAIL: TestAnalyzeImageJPEGIterationCeilingIsUnknownNotClean (0.00s)
FAIL
```

Reverted; full suite green again.

### Audit finding inversion -- media.go EBML malformed-vs-ceiling collapse

**Change:** `ebmlMissReason` -> always `return ReasonWorkCeilingBytes` (the pre-fix behavior).
**Expected:** `TestAnalyzeMediaEBMLSmallMalformedFileIsMalformedNotWorkCeiling` fails.
**Actual output:**

```
=== RUN   TestAnalyzeMediaEBMLSmallMalformedFileIsMalformedNotWorkCeiling
    media_test.go:436: duration_seconds_reason = "work_ceiling_bytes", want "malformed_input" -- this tiny file was read and walked in full, with no ceiling of any kind hit; the absence of Info/Duration is a fact about the FILE
--- FAIL: TestAnalyzeMediaEBMLSmallMalformedFileIsMalformedNotWorkCeiling (0.00s)
FAIL
```

Reverted; full suite green again.

### Audit finding inversion -- media.go ISOBMFF child-box iteration ceiling

**Change:** `walkTopLevelBoxes`'s ceiling branch -> `return false` (ceilingHit forced false, the pre-fix behavior).
**Expected:** `TestAnalyzeMediaISOBMFFChildBoxIterationCeilingIsWorkCeilingBytes` fails.
**Actual output:**

```
=== RUN   TestAnalyzeMediaISOBMFFChildBoxIterationCeilingIsWorkCeilingBytes
    media_test.go:353: duration_seconds_reason = "malformed_input", want "work_ceiling_bytes" -- OUR iteration ceiling, not the file's structure, decided this miss
--- FAIL: TestAnalyzeMediaISOBMFFChildBoxIterationCeilingIsWorkCeilingBytes (0.00s)
FAIL
```

Reverted; full suite green again.

### Audit finding inversion -- container.go extractXLSXSheetCount

**Change:** `setSheetAndPageCharacteristics`'s xlsx branch -> `if false && ceilingHit` (disabled).
**Expected:** `TestAnalyzeContainerSheetCountDeclaredSizeSkipIsWorkCeilingNotMalformed` fails.
**Actual output:**

```
=== RUN   TestAnalyzeContainerSheetCountDeclaredSizeSkipIsWorkCeilingNotMalformed
    container_test.go:378: sheet_count_reason = "malformed_input", want "work_ceiling_bytes" -- this scan's own declared-size skip, not the file's structure, decided this miss
--- FAIL: TestAnalyzeContainerSheetCountDeclaredSizeSkipIsWorkCeilingNotMalformed (0.00s)
FAIL
```

Reverted; full suite green again.

**Not separately inverted (occurrence-cap ceiling, PDF):** `TestAnalyzePDFOpaqueOccurrenceCeilingIsUnknownNotClean` was verified to pass on the FIRST implementation attempt (no inversion needed to prove it distinguishes correctly, since the occurrence cap and the boundary check are independent code paths already covered by the two inversions above) -- included in the acceptance criteria as a positive test, not as a third required inversion.

## Threat Model / Threat Flags

No new security-relevant surface introduced. All threats in the plan's own `<threat_model>` register (T-R2I-01 through T-R2I-05) are closed as described above; T-R2I-SC (npm/pip/cargo installs) is not applicable -- zero new dependencies (`git diff --stat -- go.mod go.sum` confirmed empty). No `Threat Flags` section needed.

## Known Stubs

None - no stubs introduced; every fix is a complete, tested behavior change.

## Next Phase Readiness

- All four named codex PR #9 round 2 findings are closed; the exit-path table proves the CLASS is swept for `pdf.go`, `container.go`, `image.go`, and `media.go` -- not merely the five instances the reviewer pointed at across two rounds.
- The sibling quick task (260810-r2a, `internal/api`) is unaffected -- `go test ./... -count=1 -race` confirms `internal/api` still passes against the updated `internal/inspect` package it imports.
- The one deliberately-NOT-fixed item (`scanPDFStreamRanges`/`scanPDFCommentRanges`/`scanPDFLiteralAndHexStringRanges`'s own `maxTokenScanIterations` cap under-detecting opaque ranges) is a false-POSITIVE risk, not a false-CLEAN risk -- flagged in the exit-path table for a future reader, not silently left out of the record.
- A third review round on this same class (PDF/container/image/media false-clean-or-false-complete defects) would now need to find something the exit-path table above did not already enumerate and classify -- the table itself, not just the four patches, is the artifact that should make that harder.

---
*Phase: quick/260810-r2i*
*Completed: 2026-08-11*

## Self-Check: PASSED

All 8 modified `internal/inspect/*.go` files confirmed present on disk (`ls -la`). All four commit hashes (`859c271`, `cd36a89`, `f13e3f0`, `d866c0e`) confirmed present via `git log --oneline --all | grep`. No missing items.
