---
audit_acknowledged:
  milestone: v1.15
  at: 2026-09-30
  status: unknown
---

# Quick Task 260812-r6c: Criterion Collapse — Summary

Sixth review round on PR #9. Both P2 findings confirmed against code and fixed.

## Task 1: moov usability criterion collapsed into its consumer

**File:** `internal/inspect/media.go`

**The defect (third defeat of the same acceptance bar):** `isUsableMoovCandidate`
previously required only that a tail-anchored `moov` candidate's `mvhd` and `trak`
children be *present* (round 5's own bar, itself a replacement for round 4's
"parses as a self-consistent box" bar, itself a replacement for round 3's "literal
substring found" bar). A 24-byte decoy — `moov` wrapping an empty 8-byte `mvhd` and
an empty 8-byte `trak` — satisfies "present" without carrying a single real field,
so `findMoovInTail` accepted it and never reached the genuine `moov` that followed,
losing all MP4/MOV characteristics for an otherwise-valid file.

**The fix:** removed the separate, independently-choosable threshold entirely.
`isUsableMoovCandidate` now runs the *actual* extraction `isobmffCharacteristics`
performs against an accepted candidate — `parseMVHD`'s own duration decode, or the
`wantHandler`-selected track's `tkhd`/`stsd` decode — and accepts the candidate
only if that real extraction yields a real answer. `wantHandler`/`includeDims` are
now resolved before `findMoov` is called (previously computed only after a
candidate was already accepted) so the usability check can run the same
handler-selection logic the consumer will. A shared `codecFromTrak` helper was
extracted so `findDeclaredCodec` and the usability check run the *identical*
`trak -> mdia -> minf -> stbl -> stsd` descent, closing the risk of validating
against one extraction and later publishing a value computed by a different one.

Because a criterion collapsed into its own consumer has no separate threshold to
defeat, defeating it would require a candidate that satisfies `parseMVHD`/
`parseTKHD`/`parseSTSDCodec` themselves — which are already covered by their own
tests (`TestAnalyzeMediaMVHDVersion1SixtyFourBit`,
`TestAnalyzeMediaTkhdDimensionsAreFixedPointDecoded`, etc.).

**Cost bound (T-R6C-02):** the per-candidate check got more expensive (up to one
`mvhd` descent + one `findTrackByHandler` search + up to five more descents for the
matched track, all against pre-existing `maxISOBMFFBoxes`/`maxTrakCandidatesScanned`
caps), and this cost is paid at most `maxMoovTailCandidatesScanned` (64) times per
`findMoov` call. `findMoovInTail` still aborts the entire candidate scan the instant
any single candidate's own check is inconclusive (ceiling hit), so the worst-case
total work is a fixed, input-independent constant, not a function of file size.
Documented in `isUsableMoovCandidate`'s own doc comment and proven with
`TestFindMoovInTailWorstCaseCandidateValidationStaysBounded` (adversarial 64
candidates × 20 non-matching trak siblings each, asserted to complete well under 2s).

**Tests added:**
- `TestAnalyzeMediaRejectsEmptyMvhdTrakDecoyForRealMoov` — the finding's own
  worked example (24-byte decoy), verbatim in the doc comment.
- `TestFindMoovInTailWorstCaseCandidateValidationStaysBounded` — wall-clock
  boundedness proof.
- All round-4/round-5 tests (`TestAnalyzeMediaFindsMoovDespiteEarlierIncidentalOccurrenceInPayload`,
  `TestAnalyzeMediaSkipsIncidentalEmptyMoovBoxForRealOne`,
  `TestFindMoovInTailUsabilityCeilingExhaustionIsWorkCeilingBytes`,
  `TestFindMoovInTailUsabilityCheckOwnCeilingIsWorkCeilingBytes`) pass **without
  edits**.
- `TestAnalyzeMediaNoVideoTrackYieldsUnknownDimensionsNotZero` (existing, unedited)
  continues to prove an MP4 with no video track still reports a real duration and
  `unknown` (not zero) width/height.

### Inversion (verbatim)

Reverted `isUsableMoovCandidate` to round 5's own "children present" criterion
(`firstChildBox(payload, "mvhd")` + `firstChildBox(payload, "trak")`, ignoring
`wantHandler`/`includeDims`) and reran the new test:

```
=== RUN   TestAnalyzeMediaRejectsEmptyMvhdTrakDecoyForRealMoov
    media_test.go:548: duration_seconds = <nil>, want 5.0 -- a 24-byte decoy moov
    with EMPTY mvhd and trak children (present, but carrying zero real fields)
    must not be accepted just because both children exist; the real moov that
    follows must still be found
--- FAIL: TestAnalyzeMediaRejectsEmptyMvhdTrakDecoyForRealMoov (0.00s)
```

Restored from a `cp` backup (never `git checkout`); full suite re-verified green
afterward.

## Task 2: read failures no longer report as clean (or malformed)

**Files:** `internal/inspect/image.go`, `internal/inspect/media.go`,
`internal/inspect/status.go`, `docs/FINDINGS.md`

**The defect:** `scanImageMetadata` (image.go:163-166) called `readWindow`
exactly once. On failure it returned every flag `false`
(`("", false, windowTruncated, false)`); when the window was sized to cover the
file IN FULL (`size <= maxBytes`, so `windowTruncated == false`) but the
underlying `io.ReaderAt.ReadAt` genuinely failed (real non-EOF error, or a
truthful empty read), `AnalyzeImage`'s switch fell through every case to
`default: StatusClean`. A temp-file or disk read failure was presented as a
complete, clean scan. Round 5's own sweep of this exact file fixed every WALK
exit that conflated "aborted" with "found nothing" and missed this READ exit one
line above them.

**The fix:** `scanImageMetadata` now distinguishes three cases on read failure,
not two:
1. `windowTruncated` (window smaller than the file, including a non-positive
   `maxBytes`) → `ceilingHit` (unchanged from before).
2. `winLen <= 0` (`size <= 0`: `readWindow`'s own `off>=size` guard fires before
   any `ReadAt` call — no read was even attempted) → the pre-fix zero-value
   result, preserving existing behavior for a genuinely empty upload rather than
   mislabeling a read that never happened.
3. `winLen > 0` and **not** `windowTruncated` (window sized to cover the file in
   full, but the read itself failed) → new `readFailed` signal.

`AnalyzeImage` reports `unknown`/`ReasonReadFailed` for case 3, ahead of the
existing `ceilingHit`/`malformedHit` cases in the switch.

**New Reason constant:** `ReasonReadFailed = "read_failed"` (status.go), documented
as neither a file-structure fact (`malformed_input`) nor this service's own preset
byte/time budget (`work_ceiling_bytes`/`work_ceiling_time`) — an operational I/O
failure independent of both. Deliberately **not** added to `MarkTruncated`'s
Coverage-affecting gate (stays alongside the other seven non-selective reasons):
whether a read failure should also flip `Coverage.Complete` is a narrower, still-
open design question this round leaves alone rather than deciding unilaterally by
touching `report.go`'s Coverage-marking invariants (out of this round's write
scope). `AllReasons()` and `docs/FINDINGS.md`'s reason table/prose are updated;
`TestReasonsDocMatchesGoConstants` and `TestAllReasonsCoversEveryDeclaredConstant`
stay green.

**Tests added (image.go):**
- `TestAnalyzeImageReadFailureWithFullWindowIsNotClean` — the finding's own named
  inversion test, using a new `readFailureReaderAt` that always returns a non-EOF
  error.
- `TestAnalyzeImageReadFailureWithTruncatedWindowStillReportsCeiling` — proves
  `windowTruncated` still wins when the window itself was also too small.
- `TestAnalyzeImageEmptyFileStaysCleanNotReadFailed` — pins the deliberate
  `size<=0` boundary (no read attempted ≠ read failed).

### Neighbour audit — every byte source in `internal/inspect`

Per the task's own instruction, every `readWindow` call, zip-part open, and other
`io.ReaderAt` use in the package was audited against the same question: *if the
read fails, is that distinguishable from "read it and found nothing"?*

| Site | File | Disposition |
|---|---|---|
| `scanImageMetadata`'s one `readWindow` call | `image.go:163` | **FIXED** (the named finding) — was clean, now `unknown/read_failed` |
| `wavCharacteristics`'s `readWindow` call | `media.go` | **FIXED** (found during audit, same defect one file over) — was `malformed_input` on a full-coverage read failure, now `unknown/read_failed` |
| `findMoov`'s head + tail `readWindow` calls | `media.go` | **Safe.** `headActual`/`tailActual` stay `0` when a read fails, so `headActual+tailActual >= size` is false and the function falls through to `ReasonWorkCeilingBytes` — never `clean`, never `malformed_input`. Imprecise (labels an I/O failure the same as an undersized window) but conservative: correctly marks Coverage incomplete. Left unchanged; relabeling to `ReasonReadFailed` would *weaken* Coverage-marking here since `ReasonReadFailed` is deliberately not Coverage-affecting. |
| `ebmlCharacteristics`'s `readWindow` call | `media.go` | **Safe**, same reasoning as `findMoov`: unconditionally reports `ReasonWorkCeilingBytes` on any read failure, never `clean`/`malformed_input`. Left unchanged for the same conservative-direction reason. |
| `AnalyzePDF`'s head `readWindow` call | `pdf.go:146` | **Found, deferred — out of this round's write scope.** `!headOK` (which, once `size>0 && maxBytes>0` is already checked at line 138, can only mean a genuine read failure) falls to `malformedPDFResult`: every PDF risk-token Check becomes `unknown/malformed_input`. Never reports `clean`, so the *severe* half of the defect class does not occur here — but it does mislabel an operational read failure as a claim about the file's structure, the same imprecision Task 2 fixes in `wavCharacteristics`. An existing test, `TestAnalyzePDFMalformedInputNeverPanicsAndReportsUnknown` (pdf_test.go), pins this exact behavior with its own `erroringReaderAt` fixture. Fixing this would touch `pdf.go` (outside `files_modified`/the orchestrator's declared write scope for this round) and would require updating that pinned test — flagged here for a future round rather than fixed silently. |
| `AnalyzePDF`'s tail `readWindow` call | `pdf.go:164` | **Safe**, no fix needed. `truncated := size > maxBytes` is computed purely from sizes (independent of whether the tail read succeeds), and the tail read is only ever attempted when `truncated` is already `true` — so `verdictStatus`/`verdictReason` already force `Unknown`/`ReasonWorkCeilingBytes` regardless of whether the tail read itself failed. A tail read failure can never flip anything to `clean`. |
| `scanExternalReferences`'s `f.Open()`/`io.ReadAll` | `container.go:380-425` | **Safe**, already closed in round 5 (comment cites PR9R5, container.go:334/:462): `f.Open()` erroring or `io.ReadAll` returning a read error both set `truncatedAnyPart = true` and call `tr.MarkCeilingHit()`, which resolves to `ceiling: true` → `ReasonWorkCeilingBytes`. Never `clean`, never `malformed_input`. |
| `extractXLSXSheetCount`'s `f.Open()`/`io.ReadAll` | `container.go:744-776` | **Safe**, same shape as above (comment cites the same round-5 discipline applied here): any `f.Open`/`io.ReadAll` failure returns `ceilingHit=true` → `ReasonWorkCeilingBytes`. Never `clean`/`malformed_input`. |
| `zip.NewReader(r, size)` | `container.go:146` | **Safe / defensive, noted.** On error, falls to `malformedContainerResult` (`unknown/malformed_input` for every container-domain code, never `clean`). The doc comment already argues this "should not happen in practice" since `SniffContainer` just parsed the same bytes successfully — a failure here would almost certainly be a genuine transient read failure rather than a structural fact, so this shares `AnalyzePDF`'s same imprecision (mislabels a probable read failure as `malformed_input`). Never reports `clean`, so not the severe defect class; out of this round's write scope (container.go). |
| `convert.SniffContainer(r, size)` (self-sniff fallback path) | `container.go:104` (`AnalyzeContainer`, not `AnalyzeContainerWith`) | **Noted, out of scope.** On error returns `notApplicableContainerResult` (`StatusNotApplicable`, not `Clean`) — technically not the "clean" defect, but `not_applicable` for a possible I/O failure is its own separate imprecision. Belongs to `internal/convert`'s own read discipline (a different package), not `internal/inspect`'s `readWindow` primitive — flagged, not fixed. |
| `convert.Dimensions(format, io.NewSectionReader(r, 0, size))` | `image.go:37` | **Noted, out of scope, deliberate.** `convert.ErrDimensionsUnknown` and any other underlying read error are both folded into `ReasonMalformedInput` for width/height/pixels — but this is an *already-existing, explicitly documented* decision in `AnalyzeImage`'s own comment ("Reuse, not reimplement"), predating this phase, delegating entirely to `internal/convert`'s own established discipline. Not part of this round's `readWindow` audit surface. |

Every site not fixed either (a) already fails safe in the conservative direction
(never `clean`, and for `findMoov`/`ebmlCharacteristics` specifically, correctly
marks Coverage incomplete — which relabeling to the new, non-Coverage-affecting
`ReasonReadFailed` would have *weakened*), or (b) sits in a file outside this
round's declared write scope (`pdf.go`, `container.go`) and is flagged for a
follow-up round rather than fixed silently.

### Inversion (verbatim)

**image.go:** collapsed the three-way `!ok` branch in `scanImageMetadata` back to
the pre-fix `return "", false, windowTruncated, false, false` and reran the named
test:

```
=== RUN   TestAnalyzeImageReadFailureWithFullWindowIsNotClean
    image_test.go:595: check metadata_present status = clean, want unknown
--- FAIL: TestAnalyzeImageReadFailureWithFullWindowIsNotClean (0.00s)
```

**media.go (wavCharacteristics, the neighbour fix):** reverted the `!ok` branch
back to the pre-fix `reason := ReasonMalformedInput; if windowTruncated { reason =
ReasonWorkCeilingBytes }` and reran:

```
=== RUN   TestAnalyzeMediaWAVReadFailureWithFullWindowIsNotMalformed
    media_test.go:784: duration_seconds_reason = "malformed_input", want
    "read_failed" -- a read failure with a full-coverage window is neither a fact
    about the file's structure (malformed_input) nor this scan's own byte ceiling
    (work_ceiling_bytes)
--- FAIL: TestAnalyzeMediaWAVReadFailureWithFullWindowIsNotMalformed (0.00s)
```

Both restored from `cp` backups (never `git checkout`); full suite re-verified
green afterward.

## Deviations from Plan

**1. [Rule 2 — auto-add missing critical functionality] New `ReasonReadFailed`
constant plus `docs/FINDINGS.md`/`AllReasons()` updates.** The plan's own action
text for Task 2 anticipated this ("если ни один существующий не подходит по
смыслу, добавить новый — но тогда обязательно обновить `AllReasons()`,
документацию и пройти парные тесты"). `status.go` and `docs/FINDINGS.md` are not
listed in the plan's `files_modified` frontmatter but are mechanically required by
`TestReasonsDocMatchesGoConstants`/`TestAllReasonsCoversEveryDeclaredConstant`, and
the plan's own `<files_to_read>` explicitly named `status.go` for this reason.
Commit: `c92c7f3`.

**2. [Rule 2] `wavCharacteristics` read-failure fix (media.go), beyond the named
`image.go:165` line.** Discovered during the mandated neighbour audit: identical
defect class (read failure mislabeled, though as `malformed_input` rather than
`clean`) in a file already within this round's write scope. Fixed and tested per
the plan's own instruction ("Каждое найденное сверх названного место закрыто и
покрыто тестом"). Commit: `c92c7f3`.

**3. [Design decision, not a Rule 1-3 auto-fix] `ReasonReadFailed` deliberately
NOT added to `MarkTruncated`'s Coverage-affecting reason set.** Considered making
it Coverage-affecting (semantically, a read failure is "our own inability to
examine," similar in spirit to a ceiling) but decided against it to keep this
round's blast radius to the declared write scope (`report.go`'s Coverage
invariants and its own test suite were not touched). Documented as an explicitly
open question in both `status.go`'s doc comment and `docs/FINDINGS.md`'s prose,
for a future round to resolve deliberately rather than as a side effect of this
one.

**4. Found-but-deferred (not fixed, no Rule applies — outside write scope):**
`pdf.go:146`'s head-`readWindow` failure and `container.go:146`'s
`zip.NewReader` failure both mislabel a probable read failure as
`ReasonMalformedInput` (same imprecision class as the `wavCharacteristics` fix,
never the severe "clean" defect). Both are outside this round's declared write
scope (`pdf.go`, `container.go`); `pdf.go`'s behavior is additionally pinned by an
existing test (`TestAnalyzePDFMalformedInputNeverPanicsAndReportsUnknown`) that
would need to change alongside any fix. Recommend a follow-up quick task.

## Verification

- `go build ./...` — clean
- `go vet ./...` — clean
- `gofmt -l internal/inspect` — empty
- `go test ./... -count=1 -race` — green (whole repo, all packages)
- Every pre-existing test in `internal/inspect` passes **without edits**
- Both named inversions reproduce the original defect verbatim, then restored via
  `cp` backups (never `git checkout`)

## Self-Check: PASSED

Files verified to exist:
- `internal/inspect/media.go` — FOUND
- `internal/inspect/media_test.go` — FOUND
- `internal/inspect/image.go` — FOUND
- `internal/inspect/image_test.go` — FOUND
- `internal/inspect/status.go` — FOUND
- `docs/FINDINGS.md` — FOUND

Commits verified in `git log`:
- `f02f17c` — FOUND (Task 1)
- `c92c7f3` — FOUND (Task 2)
