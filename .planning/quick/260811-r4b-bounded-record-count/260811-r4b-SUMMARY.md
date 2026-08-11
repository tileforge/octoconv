---
phase: quick-260811-r4b
plan: 01
subsystem: api
tags: [go, security, dos-hardening, zip, mp4, isobmff, xml, ooxml, encoding/xml, archive/zip]

requires:
  - phase: quick-260810-r2a
    provides: "zipDirectoryExceedsStructuralBound's round-2 dirBytes-only exceeds decision, and the inspectMaxZipDirBytes/inspectMaxZipDirEntries constants this round reuses (the byte constant is repurposed as a read-window size, not a comparison ceiling)"
provides:
  - "zipDirectoryExceedsStructuralBound's exceeds decision counts REAL central-directory records inside a fixed-size window read from the EOCD's declared directoryOffset -- it no longer compares any single EOCD-declared field (entries, dirBytes, or their combination) against a ceiling, closing the specific round-4 bypass (65,537 real records behind a 46-byte lying declared size) that both round 1 and round 2's field-comparison approaches could not"
  - "internal/inspect.containsExternalRelationship parses OOXML .rels parts with encoding/xml, recognizing TargetMode=\"External\" under every legal XML serialization (quote character, whitespace, attribute order), not only the single byte sequence the prior version matched"
  - "internal/inspect.findTrackByHandler selects an MP4/MOV/M4A trak by its own mdia -> hdlr -> handler_type (\"vide\"/\"soun\"), bounded by a new maxTrakCandidatesScanned cap, instead of always the first trak encountered"
  - "internal/inspect.findMoovInTail walks multiple candidate \"moov\" byte-sequence occurrences in the tail-anchored search, bounded by maxMoovTailCandidatesScanned, instead of giving up after the first occurrence fails box-header validation"
affects: ["58-synchronous-inspection (the phase this bound and these parsers belong to)"]

tech-stack:
  added: []
  patterns:
    - "A structural bound on a hostile, self-describing format must never derive its decision from a field that format declares about itself, however many times the specific field is swapped for another -- the only trustworthy source is a bounded, direct read of the real bytes, anchored at a value the format supplies (a start offset is fine) but sized by a constant the reader chooses, never by a value the format supplies."
    - "A window-bounded structural walk has exactly two 'insufficient information' outcomes that must collapse to the SAME conservative answer: the resource ceiling was reached, or the window ran out before the walk reached its own independently-located terminator. Treating the second as 'probably fine' is how the same bound gets bypassed a second and third time."
    - "'The first match' is never a safe stand-in for 'the only candidate' when a structured format can legitimately contain more than one instance of a pattern (multiple tracks, multiple substring occurrences) -- the fix is a bounded candidate walk, with its own separate ceiling, not merely retrying once."

key-files:
  created: []
  modified:
    - internal/api/inspect_handlers.go
    - internal/api/inspect_handlers_test.go
    - internal/inspect/container.go
    - internal/inspect/container_test.go
    - internal/inspect/media.go
    - internal/inspect/media_test.go

key-decisions:
  - "Chose a bounded real-record-counting walk over a fourth field comparison. The recurring finding's own diagnosis (both prior fixes bounded a hostile structure by reading that structure's own description of itself) ruled out any decision that reads a value the EOCD declares about its own size/count; the walk anchors on directoryOffset (used only as a read position) and on the EOCD's own structurally LOCATED offset (found by scanning, not declared), neither of which the archive can misrepresent in a way that defeats the bound."
  - "Kept a cheap ground-truth sanity check (dirBytes cannot exceed the upload's real total size) alongside the walk, explicitly documented as NOT what closes round 4 (round 4's own dirBytes, 46, is smaller than its real file) -- it exists only to keep two round-2 tests (which construct a bare, contentless EOCD claiming an oversized directory) green without editing them, and as legitimate defense in depth against a different, cruder lie."
  - "Reused inspectMaxZipDirBytes as the READ WINDOW size rather than introducing a new constant -- same numeric value round 2 chose (inspectMaxZipDirEntries * zipCentralDirMinRecordLen, ~184 KiB), but its role changed from 'ceiling compared against a declared field' to 'bytes we choose to read', which is exactly the property this round's fix needed."
  - "Applied the trak handler-type selection to BOTH dimension AND codec extraction, not only dimensions (which is all the named finding covered) -- leaving codec extraction on 'first trak' while fixing only dimensions would have reported a video file's width/height from the video track but its declared_codec from a preceding audio track, an inconsistency within the same response."
  - "Chose encoding/xml (stdlib) over a broader byte-pattern match for OOXML relationship parsing, consistent with D-02: a .rels part is small and bounded by maxRelsPartBytes (256 KiB), so a real parse is cheap here, unlike the deliberately-avoided whole-PDF byte scan."

requirements-completed: [PR9R4-F1-BOUND-COUNTS-RECORDS, PR9R4-F2-XML-ATTR-SERIALIZATIONS, PR9R4-F3-VIDEO-TRACK-SELECTION, PR9R4-F4-MOOV-CANDIDATE-WALK]

duration: ~55min
completed: 2026-08-11
---

# Quick Task 260811-r4b: Bounded Real-Record ZIP Directory Count + Three Parser Fixes Summary

**Fourth review round: the ZIP central-directory structural bound now counts real records inside a fixed-size window instead of comparing any EOCD-declared field, closing the round-4 bypass (65,537 real records behind a 46-byte lying declared size); plus OOXML relationship parsing via encoding/xml and bounded moov/trak candidate walks for two MP4 correctness bugs.**

## Performance

- **Duration:** ~55 min
- **Started:** 2026-08-11 (approx, session start)
- **Completed:** 2026-08-11T02:15:32Z
- **Tasks:** 2
- **Files modified:** 6

## Accomplishments

- **The third bypass of the ZIP directory bound is closed by changing WHAT is trusted, not WHICH field is compared.** `zipDirectoryExceedsStructuralBound`'s `exceeds` decision no longer reads any single EOCD-declared field as its primary signal. It reads the EOCD's declared `directoryOffset` (used only as a start position for a `ReadAt`, never trusted for magnitude), then walks real `PK\x01\x02` central-directory records inside a window whose SIZE is a fixed package constant (`inspectMaxZipDirBytes`, reused from round 2 but now serving a completely different role), counting real records until either the entry ceiling is reached or the window is exhausted before reaching the EOCD's own structurally-LOCATED offset (found by scanning the tail, not declared). Both outcomes are `exceeds=true`.
- **The round-4 scenario is reproduced and rejected, with REAL bytes on disk, not a synthetic claim.** `round4RealDirectoryZipFixture` builds 65,537 genuine 46-byte `PK\x01\x02` central-directory records at the offset its EOCD honestly points to, while its EOCD lies about the directory's declared size (46, not 3,014,702). `TestZipDirectoryExceedsStructuralBoundCountsRealRecordsBehindALyingDeclaredSize` (direct) and `TestInspectRound4ScenarioReportsUnknownNot500` (end-to-end through the real HTTP handler) both prove `exceeds=true`.
- **The other required shape -- window exhaustion without any lying field -- is proven separately.** `TestZipDirectoryExceedsStructuralBoundWindowExhaustionIsExceeding` builds a 100-record, completely honestly-declared directory whose real records are large enough (2000-byte names) that the fixed window runs out before reaching the directory's real end, even though entries (100) is nowhere near the 4096 ceiling. `exceeds=true` here proves the window itself is doing the rejecting, not merely a lying-field detector.
- **Total read volume for a single call is proven bounded by a fixed constant, independent of the archive's own declared size.** `TestZipDirectoryExceedsStructuralBoundReadVolumeBoundedByConstant` uses a `virtualHostileZipReader` synthesizing a 1 TiB ZIP-shaped stream on demand (never materializing more than the single slice requested) and asserts the largest single `ReadAt` call never exceeds `inspectMaxZipDirBytes`.
- **All four tests the harness protects from edits stay green, unedited.** `TestZipDirectoryExceedsStructuralBoundIgnoresWrappedEntryCount`, `TestZipDirectoryExceedsStructuralBoundTreatsZip64SentinelAsExceeding`, `TestInspectZipDirectoryWithinBoundToleratesRealisticOfficeArchive`, `TestDispatchInspectFamiliesMarksTheTrackerItWasGiven` -- verified with a diff showing pure insertions in `inspect_handlers_test.go` (no deletions), and a direct `-run` pass against exactly those four names.
- **OOXML external-reference detection now recognizes every legal XML serialization of `TargetMode="External"`.** `containsExternalRelationship` parses `.rels` parts with `encoding/xml` instead of a single literal byte-sequence comparison -- single-quoted, spaced-around-`=`, and reordered-attribute variants are all now recognized; a relationship with no `TargetMode="External"` anywhere still reports clean (proven by a dedicated negative test).
- **Video dimensions/codec are now taken from the actual video track, never whichever trak came first.** `findTrackByHandler` selects a trak by its own `mdia -> hdlr -> handler_type` ("vide" for mp4/mov, "soun" for m4a), bounded by a new `maxTrakCandidatesScanned` cap (64) separate from the outer box-count ceiling. An audio trak preceding a video trak no longer reports 0x0 dimensions; an mp4 with no video track reports `unknown`, never 0.
- **The tail-anchored moov search now tries multiple candidate occurrences, not only the first.** `findMoovInTail` walks up to `maxMoovTailCandidatesScanned` (64) literal "moov" substring occurrences, advancing past any that fail box-header validation, so an incidental "moov" sequence inside `mdat`'s own payload no longer hides the real moov box that follows it.
- **Both new candidate walks are bounded and mark the Tracker on exhaustion**, reporting `work_ceiling_bytes` (never `malformed_input`) when the cap -- not the file's structure -- decided a miss, proven by two dedicated ceiling tests.

## Task Commits

1. **Task 1: Граница считает записи, а не верит полям** - `ce79504` (fix)
2. **Task 2: Три бага парсеров** - `3c05179` (fix)

**Plan metadata:** (this commit, following)

## Files Created/Modified

- `internal/api/inspect_handlers.go` - `zipDirectoryExceedsStructuralBound` rewritten to locate the EOCD (unchanged tail-scan logic) then delegate to new `zipCentralDirectoryExceedsBound`, which walks real central-directory records inside a fixed-size window; full three-round bypass history recorded in the function's own doc comment per the plan's explicit instruction; `inspectMaxZipDirBytes`'s doc comment rewritten to describe its new role as a read-window size
- `internal/api/inspect_handlers_test.go` - `realZipCentralDirectoryFixture` (general builder for real, honest-or-lying central directories), `round4RealDirectoryZipFixture` (the exact round-4 scenario), `TestZipDirectoryExceedsStructuralBoundCountsRealRecordsBehindALyingDeclaredSize`, `TestInspectRound4ScenarioReportsUnknownNot500`, `TestZipDirectoryExceedsStructuralBoundWindowExhaustionIsExceeding`, `virtualHostileZipReader` + `TestZipDirectoryExceedsStructuralBoundReadVolumeBoundedByConstant`
- `internal/inspect/container.go` - `containsExternalRelationship` (encoding/xml-based .rels parsing) replaces a literal byte-sequence comparison for the docx/xlsx/pptx branch of `containsExternalReference`
- `internal/inspect/container_test.go` - `docxFixtureWithRels` helper; `TestAnalyzeContainerExternalRefRecognizesEveryLegalXMLSerialization` (table test: single-quoted/spaced, double-quoted/unspaced, reordered attributes); `TestAnalyzeContainerExternalRefInternalRelationshipStaysClean`
- `internal/inspect/media.go` - `findTrackByHandler` + `maxTrakCandidatesScanned` (trak selection by handler_type); `findMoovInTail` + `maxMoovTailCandidatesScanned` (bounded multi-candidate tail search); `isobmffHandlerVideo`/`isobmffHandlerAudio` constants; `findTrackDimensions`/`findDeclaredCodec` signatures updated to take `wantHandler`; `findMoov`'s tail-search ceiling now properly propagates into the overall malformed-vs-work-ceiling decision (previously structurally incapable of firing, since the old single-fn-call design never re-entered the loop)
- `internal/inspect/media_test.go` - `hdlrBox` fixture helper; `trakBox`/`fastStartMP4Fixture`/`tailMoovMP4Fixture` signatures extended with a `handlerType` parameter (and all call sites updated -- every fixture representing a real track now carries a real `hdlr` box, since production code now depends on it existing); five new tests: audio-before-video dimension/codec selection, no-video-track-yields-unknown, moov-behind-incidental-payload-occurrence, and both candidate-ceiling tests

## Decisions Made

See `key-decisions` in frontmatter above. In one line each:
- Real-record-counting walk (not a fourth field comparison) closes round 4, because every EOCD field is attacker-controlled and no comparison against one can ever be trustworthy.
- A cheap ground-truth (dirBytes vs. real upload size) sanity check rides alongside the walk for defense in depth and to keep two round-2 tests green unedited, explicitly documented as NOT what closes round 4 itself.
- `inspectMaxZipDirBytes` reused as a READ WINDOW size (not a comparison ceiling) -- same numeric value, different role.
- Trak handler-type selection applied to both dimensions AND codec extraction for consistency, even though the named finding only covered dimensions.
- `encoding/xml` (stdlib) chosen for `.rels` parsing since the part is small and bounded (256 KiB), consistent with D-02's stdlib-only policy.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Extended the trak-handler selection fix to codec extraction, not only dimensions**
- **Found during:** Task 2, while implementing the named `findTrackDimensions` fix
- **Issue:** The PR9R4 finding named only dimension extraction (`media.go:275`) as reporting the wrong track's data. Fixing only `findTrackDimensions` while leaving `findDeclaredCodec` on "first trak" would have produced an internally inconsistent report for the audio-before-video case: correct video width/height alongside a `declared_codec` pulled from the preceding audio track.
- **Fix:** `findDeclaredCodec` now also takes `wantHandler` and uses the same `findTrackByHandler` selection ("vide" for mp4/mov, "soun" for m4a).
- **Files modified:** `internal/inspect/media.go`
- **Commit:** `3c05179`

**2. [Rule 1 - Bug] `findMoov`'s tail-search ceiling now properly reaches the overall reason decision**
- **Found during:** Task 2, while rewriting the tail search into `findMoovInTail`
- **Issue:** The prior single-candidate tail search called `walkTopLevelBoxes` with a closure that always returned `false` after exactly one box, meaning its own `ceilingHit` return value could structurally never be `true` (the loop never got a second iteration to check the count cap). `findMoov`'s final malformed-vs-work-ceiling decision therefore silently assumed the tail search's ceiling could never fire.
- **Fix:** `findMoovInTail`'s own candidate-cap exhaustion (`maxMoovTailCandidatesScanned`) now sets a real `tailCeilingHit` that participates in `findMoov`'s final decision (`!headCeilingHit && !tailCeilingHit`).
- **Files modified:** `internal/inspect/media.go`
- **Commit:** `3c05179`

---

**Total deviations:** 2 auto-fixed (both Rule 1 -- bug fixes directly adjacent to, and required for consistency with, the two named media.go findings).
**Impact on plan:** Both extensions were necessary for correctness/consistency within the scope the plan already opened; no scope creep beyond the plan's own two media.go findings.

## Issues Encountered

- **Self-inflicted verification error, caught and corrected before any commit.** Partway through Task 2 (media.go), several `go build`/`go test` verification commands were mistakenly run with a `cd /Users/apaderin/dev/octoconv &&` prefix -- the shared main checkout, not this worktree -- so they silently verified the WRONG (unmodified) source for a stretch of the session. This was caught when new test names failed to appear in `go test -list`. All affected verification (build, full test run, and all four D-16 inversions for the media.go findings) was re-run correctly from the worktree's own default working directory before any commit was made; nothing incorrect was ever committed.
- **A D-16 inversion attempt on `maxTrakCandidatesScanned` itself hung/OOM'd.** The constant is also referenced by the test fixture (`const n = maxTrakCandidatesScanned + 20`), so temporarily setting the constant to `1 << 30` for the inversion caused the fixture to attempt building over a billion `trak` boxes. Killed the runaway process, reverted, and redid the inversion by disabling the runtime check (`if false && candidates >= maxTrakCandidatesScanned`) instead of touching the shared constant -- this correctly isolated the ceiling's own effect without perturbing fixture sizing.

## Verification Performed (literal output pasted)

**`go build ./... && go vet ./...`:**
```
BUILD_OK
VET_OK
```

**`gofmt -l internal/api internal/inspect`:** empty (no output), exit 0.

**`go test ./... -count=1 -race`:** every package `ok`, full tail:
```
ok  	github.com/apaderin/octoconv/internal/queue	3.403s
ok  	github.com/apaderin/octoconv/internal/ratelimit	3.207s
ok  	github.com/apaderin/octoconv/internal/reconciler	3.839s
ok  	github.com/apaderin/octoconv/internal/storage	3.817s
ok  	github.com/apaderin/octoconv/internal/webhook	3.930s
ok  	github.com/apaderin/octoconv/internal/worker	8.247s
```
(`internal/api` and `internal/inspect`, the two touched packages, both `ok` in the same run.)

**Golden fixtures, `git log --oneline -- <file>` for each of the three `pre58_*.golden.json` files:** all three show exactly one commit (`ea3f2c8`), same hash, confirming zero new commits touched them.

**`git diff --stat go.mod go.sum`:** empty (zero new dependencies).

**Four protected tests, run in isolation by exact name:**
```
=== RUN   TestInspectZipDirectoryWithinBoundToleratesRealisticOfficeArchive
--- PASS: TestInspectZipDirectoryWithinBoundToleratesRealisticOfficeArchive (0.01s)
=== RUN   TestZipDirectoryExceedsStructuralBoundIgnoresWrappedEntryCount
--- PASS: TestZipDirectoryExceedsStructuralBoundIgnoresWrappedEntryCount (0.00s)
=== RUN   TestZipDirectoryExceedsStructuralBoundTreatsZip64SentinelAsExceeding
--- PASS: TestZipDirectoryExceedsStructuralBoundTreatsZip64SentinelAsExceeding (0.00s)
=== RUN   TestDispatchInspectFamiliesMarksTheTrackerItWasGiven
--- PASS: TestDispatchInspectFamiliesMarksTheTrackerItWasGiven (0.05s)
PASS
```
`git diff --stat internal/api/inspect_handlers_test.go` (against the commit before this plan) shows `214 insertions(+)`, zero deletions -- confirming these four tests were never edited, only new tests added around them.

### Inversion 1 (Task 1, D-16): revert to round 2's dirBytes-only comparison

Expected: `TestZipDirectoryExceedsStructuralBoundCountsRealRecordsBehindALyingDeclaredSize` fails, reproducing the round-4 bypass.

```go
func zipCentralDirectoryExceedsBound(...) bool {
	return dirBytes == 0xFFFFFFFF || dirBytes > inspectMaxZipDirBytes // INVERSION-TEMP
	...
```

Actual:
```
=== RUN   TestZipDirectoryExceedsStructuralBoundCountsRealRecordsBehindALyingDeclaredSize
    inspect_handlers_test.go:611: exceeds = false, want true -- 65,537 REAL central-directory records sit behind a 46-byte lying declared size; a bound that only reads declared fields (round 1's entries, round 2's dirBytes) cannot catch this
--- FAIL: TestZipDirectoryExceedsStructuralBoundCountsRealRecordsBehindALyingDeclaredSize (0.00s)
FAIL
```
Reverted; `grep -n "INVERSION-TEMP"` returned nothing after; full `internal/api` suite green again.

### Inversion 2 (Task 2, D-16): revert `containsExternalReference`'s docx/xlsx/pptx branch to the literal byte comparison

Expected: the single-quoted/spaced-around-`=` subtest of `TestAnalyzeContainerExternalRefRecognizesEveryLegalXMLSerialization` fails; the double-quoted/unspaced subtest still passes.

Actual:
```
=== RUN   TestAnalyzeContainerExternalRefRecognizesEveryLegalXMLSerialization/single-quoted,_spaces_around_equals
    container_test.go:495: no Finding for CodeExternalReference (got Check {Code:external_reference Status:clean Reason:} instead) -- this serialization must NOT reach a clean verdict
=== RUN   TestAnalyzeContainerExternalRefRecognizesEveryLegalXMLSerialization/double-quoted,_no_spaces_(original_serialization)
--- PASS
=== RUN   TestAnalyzeContainerExternalRefRecognizesEveryLegalXMLSerialization/attribute_order_reversed
--- PASS
--- FAIL: TestAnalyzeContainerExternalRefRecognizesEveryLegalXMLSerialization (0.00s)
```
Reverted; `internal/inspect` suite green again.

### Inversion 3 (Task 2, D-16): revert `findTrackDimensions` to `firstChildBox(moovPayload, "trak", tr)` (first trak, unconditionally)

Expected: `TestAnalyzeMediaVideoDimensionsFoundWhenAudioTrackPrecedesVideoTrack` fails, reporting 0x0.

Actual:
```
=== RUN   TestAnalyzeMediaVideoDimensionsFoundWhenAudioTrackPrecedesVideoTrack
    media_test.go:406: width = 0x0, want 640 -- the VIDEO track's real dimensions, not the preceding audio track's 0x0
--- FAIL: TestAnalyzeMediaVideoDimensionsFoundWhenAudioTrackPrecedesVideoTrack (0.00s)
```
Reverted; `internal/inspect` suite green again.

### Inversion 4 (Task 2, D-16): revert `findMoov`'s tail search to the original single-candidate version

Expected: `TestAnalyzeMediaFindsMoovDespiteEarlierIncidentalOccurrenceInPayload` fails.

Actual:
```
=== RUN   TestAnalyzeMediaFindsMoovDespiteEarlierIncidentalOccurrenceInPayload
    media_test.go:470: duration_seconds = <nil>, want 5.0 -- an incidental earlier "moov" byte sequence inside mdat's own payload must not stop the search from reaching the REAL moov box that follows it
--- FAIL: TestAnalyzeMediaFindsMoovDespiteEarlierIncidentalOccurrenceInPayload (0.00s)
```
Reverted; `internal/inspect` suite green again.

### Inversion 5 (candidate-cap itself, D-16, per the plan's "plus one for the ceiling" requirement)

Disabled `findTrackByHandler`'s own candidate-cap check (`if false && candidates >= maxTrakCandidatesScanned`) without touching the shared `maxTrakCandidatesScanned` constant (touching the constant directly perturbs the test fixture's own size, see Issues Encountered).

Expected: `TestFindTrackByHandlerCandidateCeilingIsWorkCeilingBytes` fails -- with the cap disabled, the search runs to completion and correctly determines no video track exists, flipping the reason from `work_ceiling_bytes` to `malformed_input`.

Actual:
```
=== RUN   TestFindTrackByHandlerCandidateCeilingIsWorkCeilingBytes
    media_test.go:501: width_reason = "malformed_input", want "work_ceiling_bytes" -- OUR OWN candidate cap, not the file's structure, decided this miss
--- FAIL: TestFindTrackByHandlerCandidateCeilingIsWorkCeilingBytes (0.00s)
```
Reverted; `internal/inspect` suite green again; `grep -n "INVERSION-TEMP"` returned nothing.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- The structural bound and both media.go parser fixes belong to Phase 58 (Synchronous Inspection), which is otherwise complete pending its own live operator gate (SC6, `autonomous: false`). This quick task strengthens mechanisms SC6 will calibrate/exercise regardless and does not reopen or block that gate.
- No further findings from PR #9 round 4 are known to remain unaddressed by this task; this quick task covered exactly the four findings scoped to it (PR9R4-F1..F4).
- If a FIFTH review round ever proposes tightening the ZIP directory bound again by comparing yet another EOCD field against a ceiling, that proposal should be treated as a regression on its face -- the whole point of this round's fix, and its own doc comment, is that no such comparison can ever be trustworthy for this format.

---
*Quick task: 260811-r4b*
*Completed: 2026-08-11*

## Self-Check: PASSED

All 6 modified source files verified present on disk (`internal/api/inspect_handlers.go`, `internal/api/inspect_handlers_test.go`, `internal/inspect/container.go`, `internal/inspect/container_test.go`, `internal/inspect/media.go`, `internal/inspect/media_test.go`); commits `ce79504` and `3c05179` both verified present in `git log --oneline --all`.
