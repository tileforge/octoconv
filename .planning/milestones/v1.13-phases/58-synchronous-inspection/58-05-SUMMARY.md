---
phase: 58-synchronous-inspection
plan: 05
subsystem: api
tags: [isobmff, ebml, riff, mp4, matroska, wav, pure-go, bounded-parser, inspect]

# Dependency graph
requires:
  - phase: 58-synchronous-inspection (plan 02)
    provides: internal/inspect vocabulary (findings.go, status.go, FamilyResult), window.go's readWindow bounded-read primitive
provides:
  - AnalyzeMedia (internal/inspect/media.go): duration/resolution/declared_codec for mp4/mov/m4a via a head-then-tail moov search over ISOBMFF boxes; duration/declared_codec for mkv/webm continuing the existing EBML walk into Segment/Info/Tracks; duration for wav via RIFF chunks; honest unknown (parser_unavailable) for mp3/ogg/avi
  - AnalyzeImage (internal/inspect/image.go): width/height/pixels reused from convert.Dimensions, image_pixel_bomb_shape finding/check computed in uint64, metadata_present finding for EXIF/XMP across jpg/png/tiff/webp/heic
affects: [58-07 (report assembler consuming FamilyResult from every family analyzer), 60-sanitize (consumes the same findings vocabulary)]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Head-then-tail bounded search for a trailer-placed structure (MP4 moov): try the head window first (covers faststart files for free), then a tail-anchored io.ReaderAt read, locating the box by literal type-token search rather than assuming a box-aligned start offset"
    - "EBML unknown-size element handling: an element whose declared size is EBML's reserved all-1s marker (or simply larger than the remaining bounded buffer) is clamped to the rest of the already-read buffer rather than rejected outright, since Segment is routinely written with unknown size by real encoders"
    - "Reason bifurcation for 'not found': ReasonWorkCeilingBytes when the analyzer's own bounded windows did not cover the whole file, vs. ReasonMalformedInput when they did and the structure is genuinely absent -- never collapsed into one reason"

key-files:
  created:
    - internal/inspect/media.go
    - internal/inspect/media_test.go
    - internal/inspect/image.go
    - internal/inspect/image_test.go
  modified: []

key-decisions:
  - "MP4/MOV moov search is head-window-first, then tail-anchored (own mp4TailWindow=2 MiB constant, clamped by the caller's maxBytes) -- moov lives at the file tail for the overwhelming majority of non-faststart encoder output (Assumption A1), and the upload's io.ReaderAt makes a tail read possible without buffering the whole file"
  - "mp3/ogg/avi honestly report every applicable characteristic as nil+parser_unavailable rather than attempting a whole-file walk (MP3 VBR without Xing/VBRI, OGG's tail-anchored granule position, and an undesigned AVI RIFF parser) -- D-01's 'unknown is a correct answer' applied literally"
  - "AnalyzeImage reuses convert.Dimensions rather than reimplementing pixel-dimension parsing -- a second implementation would be a guaranteed future divergence from what handleCreateJob actually enforces (VALID-03), the same reasoning D-03 applies to findings"
  - "Unknown dimensions map to CheckStatus unknown for image_pixel_bomb_shape, never clean -- 'we don't know the size' is not 'we know there is no bomb' (T-58-26)"
  - "Media family (AnalyzeMedia) emits zero Findings/Checks of its own -- none of the fourteen closed-vocabulary Codes describe a media-specific risk; reducing every Code to not_applicable for a media upload is left to the plan 58-07 report assembler, which has the full cross-family picture"

patterns-established:
  - "walkTopLevelBoxes/firstChildBox (media.go): a from-scratch, package-local ISOBMFF box walker mirroring convert/dimensions.go's walkBoxes discipline exactly (declared-size-vs-remaining-buffer check before trust, iteration cap) -- deliberately re-implemented, not imported, since walkBoxes is unexported by its own package"
  - "walkEBMLElements/firstEBMLChild (media.go): a package-local EBML element walker continuing convert/avsniff.go's matchEBML vint-decoding logic (also re-implemented, same unexported-across-package-boundary reason) one level deeper, into Segment/Info/Tracks"

requirements-completed: [INSP-01, INSP-02, INSP-03]

# Metrics
duration: ~70min
completed: 2026-08-10
---

# Phase 58 Plan 05: Media and Image Analyzers Summary

**Pure-Go AnalyzeMedia (mp4/mov via head-then-tail moov search, mkv/webm via EBML continuation, wav via RIFF, honest unknown for mp3/ogg) and AnalyzeImage (reusing convert.Dimensions, uint64 pixel-bomb math, EXIF/XMP presence) for Phase 58's synchronous inspect path**

## Performance

- **Duration:** ~70 min
- **Tasks:** 2
- **Files modified:** 4 (all new)

## Accomplishments

- `AnalyzeMedia` extracts duration for mp4/mov regardless of whether `moov` sits at the file head (fast-start) or tail (the overwhelming majority of real, non-optimized encoder output) — a genuine tail-anchored `io.ReaderAt` read, not a head-peek
- `mvhd` version 0 (32-bit) and version 1 (64-bit) duration fields both decode correctly; `tkhd`'s 16.16 fixed-point width/height decode to the actual integer value, not a value 65536x too large
- mkv/webm duration/codec continue the existing `matchEBML` walk (which today stops at `DocType`) into `Segment → Info → Duration/TimestampScale` and `Segment → Tracks → TrackEntry → CodecID`, within a fixed, non-adaptive window
- wav duration is exact and cheap (RIFF `fmt `/`data` chunks); mp3/ogg/avi honestly report `unknown` with `parser_unavailable` — a decision, not a gap
- `AnalyzeImage` reuses `convert.Dimensions` (zero second implementation), computes the pixel-bomb product in `uint64`, and never reports `clean` for an unknown-size image
- EXIF/XMP presence detected for jpg/png/tiff/webp/heic via the same bounded marker-walk discipline the existing dimension parsers use

## Task Commits

Each task was committed atomically:

1. **Task 1: Media analyzer (mp4/mov/mkv/webm/wav, honest unknown for mp3/ogg)** - `bb7cb3c` (feat)
2. **Task 2: Image analyzer (dimensions reuse, pixel-bomb shape, metadata presence)** - `1d4678e` (feat)

_No TDD RED/GREEN/REFACTOR split commits were used — tests and implementation were written and verified together per task, then committed once green, consistent with how this package's prior waves (58-02/58-03) committed._

## Files Created/Modified

- `internal/inspect/media.go` (417 lines) — `AnalyzeMedia`; ISOBMFF box walker + moov head/tail search; EBML element walker continuing `matchEBML`; RIFF/WAV chunk walker; honest `parser_unavailable` for mp3/ogg/avi
- `internal/inspect/media_test.go` (496 lines) — fixture builders (ISOBMFF box literals, EBML element literals, RIFF/WAV literals), `countingReaderAt` read-budget wrapper, full `<behavior>` coverage plus the three D-16 inversions
- `internal/inspect/image.go` (251 lines) — `AnalyzeImage`; reuses `convert.Dimensions`; pixel-bomb-shape finding/check in `uint64`; EXIF/XMP marker scan per format
- `internal/inspect/image_test.go` (216 lines) — fixture builders (PNG/JPEG byte literals mirroring `internal/convert/dimensions_test.go`'s shape), full `<behavior>` coverage plus the two D-16 inversions

## Decisions Made

See `key-decisions` in frontmatter. The one worth restating in prose: **moov search is head-then-tail, not tail-only or head-only** — a head-only search would silently degrade every non-faststart real-world upload to `unknown`; a tail-only search would do one extra, unnecessary read on every faststart file. Trying head first and falling back to tail gets both cases right with one analyzer.

## Deviations from Plan

None — plan executed exactly as written. Two clarifications worth recording (not deviations, since the plan's own text explicitly permits `Claude's Discretion`/analyzer-local decisions in the areas below):

1. **`m4a` routed through the same ISOBMFF path as mp4/mov** (with `includeDims=false`), per the plan's own Part 1 heading ("ISOBMFF (mp4/mov/m4a)") — `m4a`'s `duration_seconds`/`declared_codec` come from the identical `moov`/`mvhd`/`stsd` walk, just without emitting `width`/`height` keys (audio has no resolution concept).
2. **`avi` was given an explicit "undesigned, honestly unknown" path** (`unsupportedVideoCharacteristics`) rather than falling into the generic `default` (empty-map) branch, so that every video-applicable key (`duration_seconds`, `width`, `height`, `declared_codec`) is present-but-`nil`+`parser_unavailable` per the truths bullet's stated applicable-key list for the video family, rather than absent. This is the honest, minimal-scope choice for a format this plan's research never designed a parser for (58-RESEARCH.md scopes only mp4/mov/mkv/webm/wav as "cheap and bounded"; AVI was never in that set).

## Issues Encountered

None.

## D-16 Inversion Proofs

All five required inversions were performed, observed to fail as predicted, and reverted. Exact failure text below, transcribed from the actual `go test -v` runs during execution (not reconstructed afterward).

### Task 1 (media.go) — three inversions

**1. Disable the tail-anchored moov search (leave only the head-window search).**
Change: `tailOK = false` forced immediately after the tail `readWindow` call.
```
=== RUN   TestAnalyzeMediaFindsMoovAtTail
    media_test.go:246: duration_seconds missing or wrong type: <nil> (reason=work_ceiling_bytes)
--- FAIL: TestAnalyzeMediaFindsMoovAtTail (0.00s)
```
Reverted; test passes again.

**2. Replace the fixed EBML window with adaptive growth ("until Duration is found").**
Change: `winLen = size` (ignoring the `maxBytes`/`ebmlInfoWindow` clamp) immediately before the EBML head-window read.
```
=== RUN   TestAnalyzeMediaEBMLWindowDoesNotGrow
    media_test.go:351: duration_seconds = 0.0125, want nil (Duration sits outside the bounded window)
--- FAIL: TestAnalyzeMediaEBMLWindowDoesNotGrow (0.00s)
```
The failure demonstrates the actual defect directly: once the window is allowed to grow past its budget, `Duration` that was deliberately placed outside the intended bounded window is found anyway, producing a non-`nil` result the test's own budget assertion exists to prevent. Reverted; test passes again (`duration_seconds == nil`, `reason == work_ceiling_bytes`, bytes read `<= 2048`).

**3. Remove the "declared box size fits within the remaining buffer" check in `walkTopLevelBoxes`.**
Change: commented out `if size64 < uint64(headerLen) || size64 > uint64(len(buf)-i) { return }`.
```
=== RUN   TestAnalyzeMediaNeverPanics/mp4_box_declares_size_larger_than_remaining_buffer
    media_test.go:474: AnalyzeMedia panicked: runtime error: slice bounds out of range [:2147483647] with capacity 256
--- FAIL: TestAnalyzeMediaNeverPanics
```
(This exact scenario — an `mvhd` box inside `moov` whose size field is corrupted to `0x7FFFFFFF` — was already covered by `TestAnalyzeMediaMP4OversizedBoxDoesNotPanicOrOverAllocate`; a dedicated case with the identical fixture was also added to `TestAnalyzeMediaNeverPanics`'s table so the plan's designated test name fails directly, not just an adjacent one.) Reverted; both tests pass again.

### Task 2 (image.go) — two inversions

**4. Make `ErrDimensionsUnknown` report `image_pixel_bomb_shape` as clean instead of unknown.**
Change: the `!dimsKnown` branch's `Check` temporarily set to `StatusClean`.
```
=== RUN   TestAnalyzeImageUnknownDimensionsNeverReportClean
    image_test.go:161: check image_pixel_bomb_shape status = clean, want unknown
--- FAIL: TestAnalyzeImageUnknownDimensionsNeverReportClean
```
Reverted; test passes again.

**5. Compute the pixel product in `uint32` before widening to `uint64`.**
Change: both `c["pixels"]` and the pixel-bomb-shape comparison changed from `uint64(width) * uint64(height)` to `uint64(width * height)`.
```
=== RUN   TestAnalyzeImagePixelProductDoesNotOverflow
    image_test.go:120: pixels = 4, want 18446744056529682436 (uint64 product, not a 32-bit-truncated one)
--- FAIL: TestAnalyzeImagePixelProductDoesNotOverflow
```
The observed value (`4`) is the wraparound of `0xFFFFFFFE * 0xFFFFFFFE` truncated to 32 bits before widening — exactly the overflow class T-58-27 exists to prevent. Reverted; test passes again.

## D-17 Goal Verification: Every Numeric Characteristic, Its Failure Mode, and Its Test

Per this plan's `<verification>` block, every characteristic these two analyzers report as a **number** (not `nil`+reason), the specific way that number could be wrong, and the test that closes it:

| Characteristic | Format(s) | Failure mode this could produce | Closed by |
|---|---|---|---|
| `duration_seconds` (mp4/mov/m4a) | mp4, mov, m4a | Reading `mvhd` version 1's 64-bit fields as if they were version 0's 32-bit fields (garbage/truncated value) | `TestAnalyzeMediaMVHDVersion1SixtyFourBit` |
| `duration_seconds` (mp4/mov/m4a) | mp4, mov, m4a | Division by zero if `timescale == 0` | Handled by explicit `timescale == 0` check in `parseMVHD` returning `(0, false, true)` → surfaces as `nil`+`malformed_input`, never a `NaN`/`Inf` float; covered indirectly by every mvhd test using a non-zero timescale and by `TestAnalyzeMediaNeverPanics` for adversarial inputs |
| `width`/`height` (mp4/mov) | mp4, mov | Reading `tkhd`'s 16.16 fixed-point fields as plain integers → value 65536x too large (T-58-28) | `TestAnalyzeMediaTkhdDimensionsAreFixedPointDecoded` |
| `duration_seconds` (mkv/webm) | mkv, webm | `Duration`'s IEEE-754 bytes misread as an integer, or the wrong `TimestampScale`/1e9 conversion factor applied | `TestAnalyzeMediaMKVDurationWithinHeadWindow` (asserts the exact expected value `12.5 * 1000000 / 1e9`) |
| `duration_seconds` (wav) | wav | Wrong byte-rate offset inside the `fmt ` chunk, or dividing by a zero byte rate | `TestAnalyzeMediaWAVDuration`; zero-byte-rate path returns `nil`+`malformed_input` (exercised implicitly by `TestAnalyzeMediaNeverPanics`'s truncated/garbage wav cases) |
| `pixels` (image) | png, jpg, webp, heic, tiff | 32-bit overflow of `width * height` for a hostile near-max-uint32 pair (T-58-27) | `TestAnalyzeImagePixelProductDoesNotOverflow` + inversion 5 above |
| `width`/`height` (image) | png, jpg, webp, heic, tiff | N/A — passed through verbatim from `convert.Dimensions`, which already carries its own dedicated test suite (`internal/convert/dimensions_test.go`); this plan adds no new decoding logic for these two fields, only the reuse call site | `TestAnalyzeImagePNGDimensions` (reuse call site behaves correctly end-to-end) |

**Characteristics deliberately NOT extracted, and confirmation each is `nil`+reason, never zero:**

| Characteristic | Format(s) | Reason reported | Confirmed by |
|---|---|---|---|
| `duration_seconds` | mp3, ogg | `parser_unavailable` | `TestAnalyzeMediaMP3AndOGGReportUnknownDuration` |
| `duration_seconds`, `width`, `height`, `declared_codec` | avi | `parser_unavailable` | `TestAnalyzeMediaNeverPanics`'s `undesigned_avi` case (existence check); the four-key-all-nil shape is asserted directly by reading `unsupportedVideoCharacteristics`'s own literal map, which is exercised end-to-end through `AnalyzeMedia("avi", ...)` |
| `duration_seconds`, `width`, `height`, `declared_codec` | mp4/mov/m4a where `moov` cannot be located | `work_ceiling_bytes` (windows didn't cover the file) or `malformed_input` (they did, and moov is genuinely absent) — two distinct reasons, never collapsed | `TestAnalyzeMediaMoovNotFoundWithinCoverageIsMalformed`, `TestAnalyzeMediaMoovNotFoundBeyondCoverageIsWorkCeiling` |
| `duration_seconds`, `declared_codec` | mkv/webm where `Duration`/`Tracks` fall outside the bounded window | `work_ceiling_bytes` | `TestAnalyzeMediaEBMLWindowDoesNotGrow` |
| `width`, `height` (as ABSENT keys, not `nil`) | mp3, wav, m4a, ogg | N/A — key absence, not a value | `TestAnalyzeMediaAudioHasNoResolutionKeys` |
| `width`, `height`, `pixels` | any image format where `convert.Dimensions` returns `ErrDimensionsUnknown` | `malformed_input` | `TestAnalyzeImageUnknownDimensionsNeverReportClean` |

Every "not extracted" row above reports `nil` plus a `<key>_reason` string — never a zero value, never an empty string standing in for "unknown". No test in either file asserts a zero-as-placeholder value anywhere.

**Every path untrusted bytes take into the API process, as required by D-17 (enumerated for this plan's scope):** both `AnalyzeMedia` and `AnalyzeImage` accept an `io.ReaderAt` over already-uploaded (not yet stored) request bytes and a caller-supplied `size`/`maxBytes` work ceiling. Neither function performs unbounded reads (every read goes through `readWindow`, plan 58-02's bounded-read primitive, or a slice of a buffer `readWindow` already returned), neither shells out to an external process (`exec`/`ffprobe` are absent from `media.go`'s non-comment lines, mechanically verified above), and neither allocates a buffer sized by an attacker-declared length (every box/element/chunk size is checked against the already-bounded buffer's remaining length before being trusted). This plan does not wire either function into `internal/api`'s HTTP handlers — that wiring, and the semaphore/rate-budget/`/healthz`-isolation mechanisms (D-06) that bound how many of these analyzers can run concurrently, belong to plan 58-06 per this plan's own `files_modified` scope (58-06 owns `internal/api/`, `cmd/api/main.go`).

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- `AnalyzeMedia` and `AnalyzeImage` are ready for the plan 58-07 report assembler to call alongside the PDF/container analyzers from plan 58-04, and to reduce every `Code` neither family opinion on to `not_applicable`.
- No wiring into `internal/api`'s HTTP handlers exists yet by design (that is plan 58-06's scope) — `AnalyzeMedia`/`AnalyzeImage` are pure functions over `(format, io.ReaderAt, size, maxBytes[, pixelBombThreshold])` with no dependency on `internal/api` types, so 58-06/58-07 can call them without any further refactor.
- `internal/convert` was not modified (`git diff --stat -- internal/convert/` is empty) — `AnalyzeImage`'s reuse of `convert.Dimensions` is read-only.

---
*Phase: 58-synchronous-inspection*
*Completed: 2026-08-10*

## Self-Check: PASSED

- FOUND: internal/inspect/media.go
- FOUND: internal/inspect/media_test.go
- FOUND: internal/inspect/image.go
- FOUND: internal/inspect/image_test.go
- FOUND: bb7cb3c (Task 1 commit)
- FOUND: 1d4678e (Task 2 commit)
