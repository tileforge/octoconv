---
phase: 60-sanitize-eighth-engine-class
plan: 03
status: complete
completed: 2026-08-31
commits: ["002a750"]
---

# 60-03 — OOXML macros and image metadata

## What was built

`internal/convert/sanitizeooxml.go` and `internal/convert/sanitizeimage.go`,
plus their tests, and a fix to the corpus generator that the image work
uncovered.

## OOXML: drop the part, then repair everything that pointed at it

Removing `word/vbaProject.bin` and stopping there produces a package that Word
refuses to open, because `[Content_Types].xml` still declares the part and
`word/_rels/document.xml.rels` still has a `<Relationship>` targeting it. So
the sanitizer rewrites both: overrides for dropped parts are removed, the
`bin` default is removed, and every relationship whose resolved target no
longer exists in the package is dropped.

`ooxmlConsistency` in the tests is a general traversal — it walks *every*
Override and *every* Relationship in the output and reports any that dangles,
rather than checking the three the sanitizer happens to touch. A future part
that gets dropped without its references being repaired fails this test
without anyone updating it.

Parts are identified by **content**, not by name: `isDroppedPart` runs
`IsOLECFBFromPrefix` over the bytes, so `word/notavirus.bin` renamed from
`vbaProject.bin` is dropped anyway. `TestSanitizeOOXMLDropsRenamedMacroPart`
pins that.

The output extension follows the resulting content type, not the input's
name — a sanitized `.docm` is a `.docx` because that is what it now is.

## Images: a third route, and why it beats both the plan offered

Plan 60-03 named two: shell out to `exiftool`, or re-encode through the stdlib.
Neither shipped.

`exiftool` means a new binary in the image and a guarantee that rests on an
option surface meaning what its docs say. Re-encoding a JPEG decodes and
recompresses it — quality loss on every single file, silently and
irreversibly, which the plan itself said was unacceptable without reporting it.

**Structural stripping** makes the question moot: walk the container, copy
everything forward except the metadata segments, and never decode the image.
All of APP0–APP15 and COM go from JPEG (an allowlist of "known-safe" segments
would let the next vendor extension through); `tEXt`/`iTXt`/`zTXt`/`eXIf`/`tIME`
go from PNG. The entropy-coded scan and the IDAT chunks are copied verbatim.

`LostCapabilities` is `nil`, and that is a claim, so
`TestSanitizeImageIsLossless` compares the scan data byte-for-byte to back it.

Dispatch is on the file's magic bytes, not the caller's claimed format: a PNG
uploaded as `.jpg` gets the PNG walker instead of an error.

The cost of this route is that each container needs its own walker, which is
why the supported set is small and closed rather than a claim about images in
general.

## The corpus defect this uncovered

Three image tests failed with `expected a marker at offset 274`. The fixture
was wrong, not the sanitizer: `exif_gps.jpg`, pinned by 60-01 at 330 bytes,
had a SOF0 segment declaring length 11 while carrying 8 payload bytes — the
component-count byte was missing. Go's `image/jpeg` could not decode it at all
(`bad Tq value`).

It had passed both of 60-01's independent checks. The generator's self-check
greps, and every marker it greps for was present; a grep cannot see a segment
whose *declared length* is a lie. `file` parses leniently and called the image
valid, `1x1, components 1`.

Two fixes, and the second is the one that matters:

1. The generator derives the SOF0 length from its payload instead of typing it.
2. The self-check now **walks** each fixture as the format it claims to be —
   marker chain from SOI to SOS, every declared length landing exactly on the
   next marker, each SOF payload matching `6 + 3 × components` — and resolves
   the PDF's `startxref` offset onto an actual `xref`.

Run against the old 330-byte fixture, the new walk fails with exactly the two
lines the defect produces. The corrected fixture is 331 bytes; the PDF and the
`.docm` came out byte-identical, the generator being deterministic.

This is the `optimistic-answer-for-unexamined-input` class again, one level
down: 60-01's check examined the markers and reported on the file.

The D-15 pin was not violated — it stands on
`internal/api/testdata/pre60_sanitize_rejection.golden.json`, still at one
commit. `exif_gps.jpg` is a test *input*, and fixing a malformed input is not
editing a record of prior behaviour. The correction is recorded in
`PROVENANCE.md` next to the size it corrects.

## Inversions (D-16)

| Defence removed | Failure |
|---|---|
| JPEG APPn segments kept | `Exif\x00\x00`, `OctoConvCam`, `FixtureModel X1`, GPS IFD pointer all survive |
| One scan byte flipped after SOS | lossless test names the changed scan; the decodable test still passes, which is why they are separate |
| PNG metadata chunk set emptied | `tEXt`, `eXIf`, `SecretAuthor` survive |
| (OOXML, three) content-types not rewritten / rels not rewritten / content-based detection replaced by name | each names its own dangling reference |

The strengthened generator check was inverted by running it against the
committed-but-broken fixture, which is a stronger inversion than a synthetic
one: the failure it produces is the failure that actually happened.

## Gates

`gofmt -l .` clean, `go vet ./...` clean, `go build ./...` clean,
`go test ./... -count=1` exit 0 (25 packages ok).
