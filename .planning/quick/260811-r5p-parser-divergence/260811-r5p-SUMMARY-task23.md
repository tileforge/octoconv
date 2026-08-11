# quick/260811-r5p: Tasks 2 and 3 Summary (internal/inspect only)

Scope note: this executor ran Tasks 2 and 3 ONLY, against `internal/inspect`.
Task 1 (`internal/api` ZIP-directory gate) was owned by a parallel executor
and is not covered here. Task 4 (package-wide sweep) was explicitly excluded
from this run.

## Commits

- `c578cf9` — fix(quick-260811-r5p): parse ODF/OOXML external refs
  semantically, close malformed-vs-absent ambiguity (Task 2)
- `dc0820f` — fix(quick-260811-r5p): accept tail moov candidates only when
  structurally usable (Task 3)

## Task 2: ODF/EPUB semantic parsing + honest parse-status signal

### Finding 1 (container.go:419) — namespace binding, not literal prefix

`containsODFExternalHref` (new function, replacing the byte-comparison
branch inside the old `containsExternalReference`) now parses with
`encoding/xml` and matches `attr.Name.Space == xlinkNamespace &&
attr.Name.Local == "href"`, where `xlinkNamespace =
"http://www.w3.org/1999/xlink"`. This is namespace-URI matching, not
prefix-string matching: any document binding that URI to a different
prefix (verified with a real `xmlns:ns7="http://www.w3.org/1999/xlink"` /
`ns7:href="..."` fixture) is still recognized, because `encoding/xml`
resolves the prefix internally (confirmed by reading
`$(go env GOROOT)/src/encoding/xml/xml.go`'s `Token()`: a `StartElement`'s
own `xmlns:` attributes are pushed onto the namespace stack **before** that
same element's other attribute names are translated — lines 300–324).

The scheme check itself (`odfExternalHrefSchemes`, now `[]string{"http:",
"https:", "ftp:", "file:"}`) is applied to the attribute's *already
semantically extracted* value via `strings.HasPrefix`, not to raw
serialized bytes — this is a value comparison, not a structural-format byte
comparison, so it does not reintroduce the class this fix closes.

### Finding 2 (container.go:462) — malformed input must not look like "not found"

The previous `bool` return from `containsExternalRelationship` collapsed
three real outcomes into two signals:
1. read to completion, found the marker → `true`
2. read to completion, found nothing → `false`
3. decode error (truncated by our own ceiling, or genuinely malformed XML,
   fully read) → `false` — **indistinguishable from (2)**

Fixed by replacing `bool` with a new three-value type `xmlScanResult`
(`xmlScanFound` / `xmlScanNotFound` / `xmlScanMalformed`), used by both
`containsExternalRelationship` (OOXML) and the new `containsODFExternalHref`
(ODF/epub). `dec.Token()`'s error is inspected: `io.EOF` (decoder consumed
the whole part with no match) → `xmlScanNotFound`; any other error (a real
`*xml.SyntaxError`, confirmed empirically — see Inversion 2 below) →
`xmlScanMalformed`.

`scanExternalReferences`'s own return also changed shape, from `(location
string, ceilingHit bool)` to a named struct `externalRefStatus{location,
ceiling, malformed}` — a genuinely new field, not a bolt-on second bool the
caller could ignore. `appendExternalReferenceResult`'s switch now has four
arms (`location != ""` / `ceiling` / `malformed` / clean-default), each
mapped to the correct `Check` (`clean`, `unknown/work_ceiling_bytes`,
`unknown/malformed_input`). `malformedAnyPart` deliberately loses to
`truncatedAnyPart` in the final priority switch: a malformed result
computed from a read this scan **also** truncated (via `readErr != nil` or
the `io.LimitReader` boundary trap) is an artifact of that truncation, not
new information about the file.

The wrong doc comment on `containsExternalRelationship` that rationalized
returning `false` on any decode error ("it is the caller's own truncation
bookkeeping ... that is responsible for downgrading the overall verdict")
was deleted and replaced with a comment explaining why that rationale was
wrong (truncation bookkeeping only fires on an actual byte-ceiling hit, not
merely because `dec.Token()` errored).

No new `Reason` constant was needed — `ReasonMalformedInput` already
existed in the closed vocabulary (`status.go`), so `AllReasons()` /
`docs/FINDINGS.md` doc-parity tests required no changes.

### Fixture fix required

`odtFixtureWithTwoParts` (used by
`TestAnalyzeContainerLimitReaderBoundsCrossPartBudget`) previously embedded
the literal string `xlink:href="http://example.com"` as raw bytes with no
enclosing XML element at all — valid for the old byte-comparison
implementation, but semantically meaningless XML (just character data) for
the new parser. Rewrote `styles.xml`'s content to be genuinely well-formed
XML (`<office:document-styles xmlns:xlink="...">›<office:body><!--
padding --><text:a xlink:href="..."/></office:body></office:document-styles>`)
with the same padding-then-marker-near-tail shape, so the cross-part-budget
truncation test still exercises exactly what it did before, and the
fixture's own "generous budget finds it" sanity check now genuinely
exercises the semantic parser.

## Task 3: moov candidate acceptance by usability, not by parse success

### Finding (media.go:321)

Round 4 raised `findMoovInTail`'s acceptance criterion from "the literal
substring `moov` was found somewhere" to "parses as a self-consistent box"
— but a bare, empty box header (`\x00\x00\x00\x08moov`: size=8, type=moov,
zero-length payload) parses exactly as cleanly as a real one, so the
"first match wins" defect survived one level up.

Fixed with a new `isUsableMoovCandidate(payload, tr) (usable bool,
ceilingHit bool)` that requires the candidate's payload to yield **both**
`mvhd` (ISO/IEC 14496-12's own mandatory moov child) **and** at least one
`trak` (also mandatory — a moov describing zero tracks describes no media)
before acceptance. `findMoovInTail` now calls this after `matchedType`
succeeds; a candidate that parses as `moov` but fails usability causes the
search to continue to the next substring occurrence, still bounded by
`maxMoovTailCandidatesScanned` (64, unchanged).

`isUsableMoovCandidate`'s own ceiling propagation was deliberately made
strict rather than folding an inconclusive check into "reject and keep
going": if either the `mvhd` or `trak` lookup itself exhausts
`maxISOBMFFBoxes` before reaching an answer, `findMoovInTail` aborts the
*whole* search immediately with a ceiling result
(`unknown/work_ceiling_bytes`), rather than silently treating "we couldn't
tell" as "not usable" and potentially reporting a real, valid moov's
absence as a structural fact about the file. This is a genuinely new
correctness improvement beyond the minimum needed to pass the named
finding — verified by `TestFindMoovInTailUsabilityCheckOwnCeilingIsWorkCeilingBytes`,
which constructs a real moov (with genuine mvhd+trak) buried behind more
than `maxISOBMFFBoxes` filler boxes, reachable only via the tail search,
and confirms the result is `work_ceiling_bytes`, not `malformed_input`.

### Sibling-walk check (as instructed by the task)

Checked `findTrackByHandler` (trak candidate selection) and the generic
`firstChildBox`/`walkTopLevelBoxes` (mvhd/tkhd/hdlr/minf/stbl/stsd lookups)
for the same "first-match-wins-on-a-weak-criterion" shape:

- `findTrackByHandler` already selects by the real semantic criterion
  (`handler_type == wantHandler`, fixed in round 4) and already continues
  past a non-matching trak — first-match-wins here means "first trak whose
  handler_type genuinely matches", which is correct ISO/IEC selection
  semantics, not an incidental-bytes false positive. No fix needed.
- `firstChildBox`/`walkTopLevelBoxes` for mvhd/tkhd/hdlr/stsd/mdia/minf/stbl
  are all "first box of declared TYPE X within an already-bounded,
  self-consistent sibling chain" — ISO/IEC 14496-12 defines these as
  singular per parent, so "first of type X" *is* the correct selection
  criterion, unlike `moov`'s substring-scan-over-opaque-media-bytes
  vulnerability. No fix needed.

**Deliberately NOT extended to `findMoov`'s head-window branch.** I
attempted to apply `isUsableMoovCandidate` to the head-window's own moov
lookup (`firstChildBox(headWin, "moov", tr)`) for consistency, reasoning
that a forged top-level empty `moov` sibling placed before a genuine
top-level `moov` box is structurally the same class of attack. This
introduced a real regression:
`TestAnalyzeMediaISOBMFFChildBoxIterationCeilingIsWorkCeilingBytes` (an
existing test, unrelated to this task, exercising a moov at the *head*
with >4096 filler boxes before mvhd) started reporting
`duration_seconds_reason = malformed_input` instead of the expected
`work_ceiling_bytes`, because a naive `firstUsableMoov` wrapper's own
outer-walk `ceilingHit` return does not see the *inner* ceiling hit that
fires inside the usability check itself, and `findMoov`'s
`headActual+tailActual >= size && !headCeilingHit && !tailCeilingHit`
reasoning then misclassifies the miss. Rather than thread this through
(which would require restructuring `findMoov`'s own reason logic, a larger
change than this finding calls for, and risks further regressions in the
existing fine-grained per-characteristic ceiling/malformed distinctions
that `isobmffCharacteristics` already computes downstream), I reverted that
part and left the head-window branch as a plain `firstChildBox(headWin,
"moov", tr)` type-match. Rationale for why this is acceptable, not merely
expedient: a genuine top-level duplicate `moov` box requires forging the
*entire file's* top-level box chain (ftyp + fakeMoov + realMoov + mdat all
self-consistently chained) — a fundamentally different, rarer shape than an
incidental 4-byte ASCII collision inside arbitrary, fully attacker-
controlled `mdat` payload bytes (the actual named finding). Even in that
rare case, the outcome is not unsafe: `isobmffCharacteristics` downstream
would report every characteristic as `malformed_input` (the empty moov has
no mvhd/trak), never a false/incorrect value and never `clean` — a
correctness/completeness nuance, not a security-relevant defect.

## Inversions performed

Per the plan's own protocol: break the fix, confirm the NAMED test fails,
record the failure verbatim, restore from a saved copy (never `git
checkout <file>`).

### Inversion 1 (Task 2, Finding 1 — namespace binding)

Reverted `containsODFExternalHref` to the old
`bytes.Contains(data, []byte("xlink:href=\""+scheme))` byte-comparison.
Ran `TestAnalyzeContainerODFExternalHrefRecognizesEveryLegalXMLSerialization`:

```
=== RUN   TestAnalyzeContainerODFExternalHrefRecognizesEveryLegalXMLSerialization
=== RUN   TestAnalyzeContainerODFExternalHrefRecognizesEveryLegalXMLSerialization/single-quoted,_spaces_around_equals,_conventional_xlink_prefix
    container_test.go:575: no Finding for CodeExternalReference (got Check {Code:external_reference Status:clean Reason:} instead) -- this serialization must NOT reach a clean verdict
=== RUN   TestAnalyzeContainerODFExternalHrefRecognizesEveryLegalXMLSerialization/double-quoted,_no_spaces_(original_serialization)
=== RUN   TestAnalyzeContainerODFExternalHrefRecognizesEveryLegalXMLSerialization/XLink_namespace_bound_to_a_DIFFERENT_prefix
    container_test.go:575: no Finding for CodeExternalReference (got Check {Code:external_reference Status:clean Reason:} instead) -- this serialization must NOT reach a clean verdict
--- FAIL: TestAnalyzeContainerODFExternalHrefRecognizesEveryLegalXMLSerialization (0.00s)
    --- FAIL: TestAnalyzeContainerODFExternalHrefRecognizesEveryLegalXMLSerialization/single-quoted,_spaces_around_equals,_conventional_xlink_prefix (0.00s)
    --- PASS: TestAnalyzeContainerODFExternalHrefRecognizesEveryLegalXMLSerialization/double-quoted,_no_spaces_(original_serialization) (0.00s)
    --- FAIL: TestAnalyzeContainerODFExternalHrefRecognizesEveryLegalXMLSerialization/XLink_namespace_bound_to_a_DIFFERENT_prefix (0.00s)
FAIL
```

The double-quoted subtest still passes under the inverted code (it's the
exact literal byte pattern the old code matched) — exactly as expected;
the single-quoted and other-prefix subtests fail, proving the semantic
parse is load-bearing for those two cases. Restored `container.go` from
the pre-inversion saved copy (`cp`, not `git checkout`); confirmed the
restored file is byte-identical to the pre-inversion copy and the full
`internal/inspect` suite passes again.

### Inversion 2 (Task 2, Finding 2 — malformed vs. not-found)

Reverted `containsExternalRelationship`'s error branch to fold every
`dec.Token()` error (both `io.EOF` and a genuine syntax error) into
`xmlScanNotFound`. Ran `TestAnalyzeContainerMalformedRelsPartYieldsUnknownMalformedNotCeiling`:

```
=== RUN   TestAnalyzeContainerMalformedRelsPartYieldsUnknownMalformedNotCeiling
    container_test.go:641: Check(CodeExternalReference) = {Code:external_reference Status:clean Reason:}, want unknown/malformed_input -- the part was read to completion (declared size == real size, budget is 1<<30) and its XML genuinely failed to parse; this must not be reported the same way a ceiling hit is
--- FAIL: TestAnalyzeContainerMalformedRelsPartYieldsUnknownMalformedNotCeiling (0.00s)
FAIL
```

Confirmed empirically (via a standalone `encoding/xml` probe against
`go1.26.5`'s stdlib) that a truncated-mid-tag `.rels`/ODF fragment produces
a real `*xml.SyntaxError` ("unexpected EOF"), distinct from the `io.EOF` a
genuinely exhausted, well-formed document produces — this is what makes
`xmlScanMalformed` a reachable, meaningful branch rather than dead code.
Restored `container.go` from the pre-inversion saved copy; confirmed
byte-identical and the full suite green again.

### Inversion 3 (Task 3 — moov candidate usability)

Reverted `findMoovInTail`'s loop body to `return candidatePayload, true,
false` immediately on `matchedType`, without the `isUsableMoovCandidate`
call (round 4's exact behavior). Ran
`TestAnalyzeMediaSkipsIncidentalEmptyMoovBoxForRealOne`:

```
=== RUN   TestAnalyzeMediaSkipsIncidentalEmptyMoovBoxForRealOne
    media_test.go:505: duration_seconds = <nil>, want 5.0 -- an incidental, self-consistent but EMPTY "moov" box (valid header, no mvhd/trak) must not be accepted as the real moov just because it PARSES; the real moov that follows must still be found
--- FAIL: TestAnalyzeMediaSkipsIncidentalEmptyMoovBoxForRealOne (0.00s)
FAIL
```

Restored `media.go` from the pre-inversion saved copy; confirmed
byte-identical and the full suite green again.

## Verification

- `go build ./...` — clean
- `go vet ./...` — clean
- `gofmt -l internal/inspect` — empty
- `go test ./internal/inspect/ -count=1 -race` — pass
- `go test ./... -count=1` — pass (whole repo, all packages)
- `git status --short` after both commits — clean (no untracked/deleted
  files)
- No files outside `internal/inspect/{container,media}{,_test}.go` were
  touched; `internal/api` was left entirely untouched for the parallel
  Task 1 executor.

## Known Stubs

None introduced.

## Threat Flags

None — no new network endpoints, auth paths, or trust-boundary schema
changes. Both fixes tighten existing parsing/classification logic within
`internal/inspect`'s pre-existing threat surface (T-R5P-03, T-R5P-04,
T-R5P-05 in the plan's own threat model).

## Self-Check

- `internal/inspect/container.go` — FOUND
- `internal/inspect/container_test.go` — FOUND
- `internal/inspect/media.go` — FOUND
- `internal/inspect/media_test.go` — FOUND
- commit `c578cf9` — FOUND (`git log --oneline --all | grep c578cf9`)
- commit `dc0820f` — FOUND (`git log --oneline --all | grep dc0820f`)

## Self-Check: PASSED
