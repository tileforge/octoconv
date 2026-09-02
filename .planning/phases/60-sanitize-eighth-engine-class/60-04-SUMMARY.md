---
phase: 60-sanitize-eighth-engine-class
plan: 04
status: complete
completed: 2026-08-31
commits: ["8aed57c"]
---

# 60-04 — Independent verification

## What was built

`internal/convert/sanitizeverify.go` and its tests. Verification is called from
inside `SanitizeFile`, not left to the caller, and there is no flag to skip it.

| Format | Removed by | Verified by |
|---|---|---|
| PDF | ghostscript | pdfcpu v0.15.0 (Go, different codebase) |
| OOXML | Go `archive/zip` | `unzip` + `file`/libmagic (two C codebases) |
| Image | segment walker, never decodes | stdlib decode + libmagic + byte grep |

The image row was not in the plan — 60-03 chose a structural strip instead of
the `exiftool` the plan assumed, so an image verifier was needed. The
no-skip rule is what surfaced that: with none registered, every image sanitize
failed closed. That is louder than a TODO and it is the correct failure.

## The tool the research recommended, and the measurement that rejected it

The research recommended `oletools`/`olevba` and said plainly that it had not
run it. Running it changed the answer.

On the corpus fixture — a package containing `word/vbaProject.bin` whose first
eight bytes are the OLE CFB signature — `olevba` 0.60.2 reports `"macros": []`
and exits 0. `oleid` prints `VBA Macros: No`. The reason is in `olevba.py`: it
scans every zip member by magic, so it is *not* fooled by a rename, but it then
requires a well-formed VBA project — a `VBA` storage plus `PROJECT`,
`VBA/_VBA_PROJECT` and `VBA/dir` streams — before it reports anything at all.

That makes olevba a good macro **analyser** and the wrong **verifier**. Its
"clean" means "I found no VBA project I could parse", and a residual OLE
container it cannot parse is exactly what a verifier must not wave through.
Reporting clean because parsing failed is D-07's failure mode wearing a tool's
clothes.

`unzip` + libmagic was chosen instead and measured on the same fixture:
`word/vbaProject.bin → application/CDFV2`, `word/document.xml → text/xml`.
Neither step reads a name.

## What the verifier found on its first run

It rejected the sanitized PDF immediately: `/Info` present, four entries.

```
/Producer     (GPL Ghostscript 10.07.1)
/Creator      (GPL Ghostscript 10071 (ps2write))
/CreationDate (D:20260831221239+03'00')
/ModDate      (D:20260831221239+03'00')
```

plus a plaintext `%%Invocation:` line at the top of every output carrying our
full command line and flags.

Plan 60-02's tests had grepped for the *input's* values — "Secret Internal
Title", "jane.doe@corp.example" — all of which were gone. They were
structurally incapable of seeing what Ghostscript wrote in their place, because
it sat inside a compressed `/ObjStm`. **This is precisely the case for
independent verification, and it arrived on day one.** It is also `/ObjStm`
blindness in the flesh — the limit `internal/inspect/pdf.go` documents for
phase 58's byte scanner, reproduced inside our own output.

D-09 removed the XMP packet whole and named "does not leak the producer
version" as a reason. Leaving `/Producer` would contradict that decision in the
file it was made for. Three fixes were measured; two failed:

| Attempt | Result |
|---|---|
| `-dOmitInfoDate` | drops `/CreationDate` and `/ModDate`. **Kept.** |
| `-dOmitInfoDate -dOmitID` | the dates **come back**. Not used. |
| `[ /Producer () /DOCINFO pdfmark` | ignored for Producer/Creator, and **adds** empty `/Author` and `/Title`. Not used. |

Ghostscript always signs its output. So the second pass now targets
`-dCompatibilityLevel=1.4` and the two remaining values are blanked in place.

**PDF 1.4 does two jobs.** It moves `/Info` out of a compressed object stream,
which is the only reason a byte-level redaction is possible at all — and it
means the sanitized output contains no `/ObjStm` whatsoever, so a downstream
byte scanner (ours or a client's) can finally see all of it instead of being
blind to half.

**The redaction is length-preserving on purpose.** `(GPL Ghostscript 10.07.1)`
becomes `()` followed by exactly as many spaces as were removed, so every
offset in the xref table stays correct and the file needs no rewriting. Padding
inside a dictionary is legal PDF. This is a redaction, not a PDF writer, and
that distinction is what makes it safe to do here. Its correctness is not
asserted — pdfcpu re-parses the result, so a redaction that broke the file
fails the job instead of shipping.

Observed on the file as it ships:

```
%PDF-1.4
%
%                                                            <- invocation, blanked
/Producer()
Ghostscript: 0   Invocation: 0   ObjStm: 0
Secret Internal Title: 0   jane.doe: 0   evil.example: 0   /Launch: 0
```

## Four findings the tests produced about the verifier itself

1. **The revision check had to move ahead of the parse.** It is a pure byte
   count needing no parser, and sitting behind one it never ran on a
   two-revision file — pdfcpu failed first, complaining about the *older*
   revision's page tree, and the real defect was reported as "could not
   verify". The property no parser can see should not be gated on a parser.

2. **Zero `startxref` is not "too many revisions".** After the reorder, garbage
   bytes reported as *residue*. Zero means "this does not have the structure of
   a PDF" — the unverifiable outcome. Collapsing them would blame the sanitizer
   for a file of noise and send the wrong person looking.

3. **libmagic has a blind spot for degenerate OLE containers.** A part carrying
   the signature and little else is not called CDFV2. The sanitizer drops on the
   signature alone, so such a part surviving means the sanitizer failed, and the
   check that catches it must be at least as broad as the one that drops it. The
   verifier therefore also tests the signature directly — the one predicate it
   deliberately shares with the sanitizer. It shares no implementation, and
   neither check subsumes the other: libmagic covers well-formed containers
   under any name, the prefix check covers degenerate ones. A range no check
   covers reads as clean.

4. **pdfcpu's validator refuses the threat fixture outright** (`action O not
   allowed for source root`), so calibration on the raw input yields
   *unverifiable*, not *residue*. Demanding residue there would be demanding the
   parser be lenient enough to read an attack file, which is the wrong thing to
   want. Calibration accepts either refusal; the accept-side test pins the other
   direction, so a verifier that always refuses still fails.

## The calibration test is the important one

`TestVerifierRejectsTheUnsanitizedInput` points every verifier at the corpus
fixture *before* sanitization and requires each to refuse. Without it, a
verifier that lost its way — wrong path, empty marker table, a parse that
silently returns nothing — would pass every other test in the file by agreeing
that everything is fine. A verifier that cannot say no is worth nothing when it
says yes.

## Three outcomes, and where the refusal stands

`ErrSanitizeResidue` and `ErrSanitizeUnverifiable` are separate sentinels and a
test asserts they do not match each other. An operator reading a failure must
be able to tell a broken sanitizer from a broken verifier.

On failure the output file is **deleted**. "Nothing unverified reaches S3" is
enforced by the bytes ceasing to exist rather than by a promise about call
order, and `TestUnverifiedOutputIsDeleted` pins it with a delivery counter so a
refactor that moves verification after delivery reddens rather than passes.

`TestEverySanitizeFormatHasAVerifier` is mechanical: a format added to
`sanitizeFormats` without a verifier fails at the moment the gap appears.

## Dependencies

One new direct module: `github.com/pdfcpu/pdfcpu v0.15.0`, pinned. It brings
six transitive ones. `prometheus/client_model` and `golang.org/x/sys` also
appear in the `go.mod` diff — verified against the pre-change file: `x/sys` was
already direct and `client_model` is genuinely imported by
`internal/metrics/metrics.go`, so `go mod tidy` reclassified them. Neither is an
addition.

## Inversions (D-16)

| Defence removed | Failure |
|---|---|
| unverified output allowed through | `TestUnverifiedOutputIsDeleted`: "returned success despite an unverifiable output" |
| engine-identity redaction | `TestVerifierAcceptsSanitizedOutput` — Ghostscript's signature comes back |
| OLE prefix check | `TestVerifierCatchesRenamedOLEPart/degenerate_container` |
| single-revision check | `TestVerifierRejectsMultiRevisionOutput` |
| PDF 1.4 pin | `TestVerifierAcceptsSanitizedOutput` — `/ObjStm` returns and `/Info` is unpatchable |

## Gates

`gofmt -l .` clean, `go vet ./...` clean, `go build ./...` clean,
`go test ./... -count=1` exit 0, and
`OCTOCONV_REQUIRE_BINS=1 go test ./internal/convert/ -count=1` exit 0 — the
mode where a missing binary is red rather than quiet.

## Carried into 60-06

`unzip` and `file` are now required by the worker at runtime, on top of
ghostscript. Without them every OOXML and image sanitize fails closed, which is
correct behaviour and a container that must be built to match.
