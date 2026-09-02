---
phase: 63-sanitize-hardening-probe-page-ceiling
plan: 01
status: complete
completed: 2026-09-02
requirements: [SANI-07, SANI-08, SANI-09]
---

# 63-01 — three findings, one shape

All three were **merged unfixed by operator decision** at the end of PR #12, and
all three are the same mistake: a boundary inferred from a substring or a prefix
instead of from the structure that declares it. So each fix changes the
derivation. None of them adds a cleverer comparison.

## The fixtures went first, and two of the three findings had never been observed

`icc_multichunk.jpg`, `endstream_in_stream.pdf` and
`ooxml_rel_targets_main.docx` landed in their own commit, before a line of the
fixes, with a throwaway test recording what the code did with them. A fix for a
defect nobody reproduced is a fix for a defect nobody has shown exists.

Two of them behaved as reported. The third **did not**, and what it did instead
was worse — see SANI-08 below. That is the entire argument for the protocol.

## SANI-07 — the ICC decision is about the chunk set

`payload[14:18]` is the profile's declared size **in chunk 1**, where what
follows the 14-byte header is the start of the ICC profile header (ICC.1:2010
§7.2.2). In continuation chunks those four bytes are ordinary profile data, so
the comparison was against a number that means nothing.

Observed: **three chunks in, two out.** The failure is quiet in the worst way —
the file still decodes, the report still says the colour profile was preserved,
and the image renders wrong.

The unit of the decision is now the set: one agreed count, sequences exactly
`1..count`, no gaps, no repeats, and the assembled data equal to the size chunk 1
declares. Valid means every chunk survives verbatim; invalid means all of them
go. Half a profile is not a smaller profile.

**Two passes over one walker, not two walkers.** The decision needs the file read
before anything is written, and a second scanner would have had to agree with the
first about fill bytes, entropy-coded scan data and segment lengths — the exact
shape review has named three times in this package. `walkJPEGSegments` is run
twice instead.

**The verifier had the identical defect and would have failed the fix.** It
carried its own copy of the per-chunk read, so fixing only the sanitizer would
have made every multi-chunk profile it now correctly keeps come back as residue —
producing a correct file and then refusing to deliver it, which is worse than the
bug.

Equality, not `<=`, for the size check, with the cost stated: an encoder padding
the final chunk beyond the profile would lose its colour profile. None is known;
if one appears the fix is a measured allowance, not a loosening.

## SANI-08 — stream boundaries from `/Length`, and the fixture found more than was reported

The reported half: `pdfStreamRanges` returned `[274 337]` for a payload running
to 400, so the redactor's protected range stopped 63 bytes early and it was free
to rewrite the rest of a stream — preserving every xref offset while destroying
what they point at.

**The unreported half.** `pdfNonStreamBytes` got it exactly *inverted*: the
payload's own `startxref` and `%%EOF` survived blanking while the file's real
trailer was blanked to spaces. `verifySingleRevision` then counted 1 and 1 and
**passed** — by reading the payload as structure and the structure as payload,
two errors whose totals happen to agree with the right answer.

That has its own cause underneath: `"stream"` is a substring of `"endstream"`, so
after ending a range the walker re-entered on the tail of the keyword it had just
consumed. The fix is not a longer search string but a structural rule — a stream
keyword is only a stream keyword when it follows the `>>` that closes its own
dictionary.

**Measured before the resolver was written:** Ghostscript emits only *direct*
`/Length` values on this corpus (4 streams over two files, 4 direct, 0 indirect).
The indirect path is therefore exercised by a hand-built fixture, which is
exactly why it exists — "the gs build we happened to measure" is not a property
to depend on.

Unresolvable means **error**, never a substring fallback (D-07). `pdfNonStreamBytes`
used to fail closed by blanking the rest of the file; defensible for a grep,
strictly worse than saying so in a verifier.

**A real corpus file caught the first version.** `embedded_font.pdf` failed with
`declares /Length 1 but no endstream follows it`: Type1 font streams carry
`/Length1`, `/Length2`, `/Length3`, and a prefix match reads the `1` of the
**key** as the value. A name is a token, not a prefix — the same sentence this
phase writes three times over.

## SANI-09 — the verifier requires the document

A `.rels` whose `Target` is the package's own main part makes
`relationshipDrivenDrops` drop it. Observed: `SanitizeFile` **succeeded**,
reported five removed classes, and delivered a package with no
`word/document.xml` at all. It unzips, carries a well-formed
`[Content_Types].xml`, and opens as nothing.

The verifier now requires the main part **and** a content type for it. Both
halves are separately load-bearing: a part that is present and undeclared is one
Office will not open either.

**The verifier had to learn the format**, because "the main part is still there"
cannot be stated without knowing which part that is — and inferring it from
whichever main part happens to be present is the same "the check only runs when
the thing is there" defect this closes. `ooxmlMainPart` covers all nine formats
`verifyOOXML` is registered for, with a test that fails if the two lists drift.

## Two existing tests were asserting against invalid fixtures

Both are worth naming, because "the test passed" was doing no work in either:

* an ICC segment built as `tag+"profile"` — bytes 12 and 13 there are `p` and
  `r`, so it declared itself chunk 112 of 114, and it survived only because the
  check it faced was broken;
* an OOXML package with an empty `<Types/>`, which no real package has.

Both keep their propositions. They now assert them about valid inputs.

## Inversions

Ten, each run, each failing on its own test:

| Boundary removed | What failed |
|---|---|
| ICC sequence-completeness | `count says three, only two chunks are present` |
| ICC assembled-size | `assembled data does not match the declared profile size` |
| ICC back to per-chunk | `TestSanitizeDropsAnIncompleteICCSetWhole` |
| Substring stream end (sanitizer) | protected range truncates at offset 337 |
| Lexical stream keyword (verifier) | real trailer read as payload |
| `/Length` as a prefix | the `/Length1` case |
| Substring fallback instead of failing closed | three unresolvable cases pass |
| Main-part presence | `main part missing entirely` |
| Content-type declaration | `main part present but not declared` |
| One format dropped from `ooxmlMainPart` | the completeness test |

**The first round of ICC inversions found the tests inadequate, not the code.**
Removing the sequence-completeness check and removing the assembled-size check
each left every test green, because each of those cases was *also* caught by the
other boundary. Two cases were rewritten to isolate one boundary apiece. Found by
inverting, not by reading.

## Gates

`gofmt -l .` clean · `go vet ./...` clean · `go build ./...` clean ·
`go test ./... -count=1` exit 0 against live Postgres + Redis + MinIO
(25 packages). Local only.

`docs/SANITIZE.md` gains the two client-visible statements this makes true: a
colour profile is kept whole or not at all, and this operation will never return
an empty package.
