---
quick_id: 260830-dmg
type: execute
description: close three codex findings from the PR #10 review
files_modified:
  - internal/inspect/pdftext.go
  - internal/inspect/pdftext_test.go
  - internal/inspect/testdata/pdf/DIVERGENCE.md
autonomous: true
---

<objective>
Close three findings from codex's review of PR #10. All three were reproduced
before being accepted, and all three produce a FALSE VERDICT — the outcome this
phase exists to prevent.

| # | Finding | Reproduced |
|---|---|---|
| P1 | Octal escapes are not decoded | `(\101\102\103\104\105\106) Tj` counts **18**, `pdftotext` yields **6**, threshold is 16 → false `present` |
| P1 | `stream` is matched lexically | a literal string containing the word `stream` in a non-content object counts **26** → false `present` |
| P2 | An ambiguous skip marker is treated as proof | a text-bearing Form XObject whose dict mentions `/FontFile2` is skipped, coverage stays true → false `absent` |

The octal one is the sharpest, because it is mine twice over: the phase fixed
exactly this class for hex strings and left the identical wrong argument standing
in the neighbouring function, in a comment that said the over-count was harmless.
That is the named defect class "fix the class, not the instance".

The Form XObject one is the most serious in kind: a false `absent` is the one
direction D-09 forbids outright.
</objective>

<tasks>

<task type="auto">
  <name>Task 1: Decode octal escapes in literal strings</name>
  <files>internal/inspect/pdftext.go</files>
  <read_first>
    - internal/inspect/pdftext.go — `readPDFLiteralString` and its doc comment, which currently argues the over-count is safe; and `readPDFHexString`, which had the identical argument until a measurement disproved it
  </read_first>
  <action>
Decode the octal escapes ISO 32000 §7.3.4.2 defines: `\ddd` (one to three octal
digits), plus the named escapes `\n \r \t \b \f \( \) \\`, plus a backslash
followed by an end-of-line, which is a line continuation and contributes nothing.

Delete the paragraph claiming the over-count is harmless and replace it with what
the measurement showed, so the next reader does not re-derive the same wrong
conclusion from the same comment.
  </action>
  <verify>go test ./internal/inspect/ -run 'CountTextOperator' -count=1</verify>
  <acceptance_criteria>
    - `(\101\102\103\104\105\106) Tj` counts 6, not 18
    - One- and two-digit octal escapes are handled (`\7`, `\41`)
    - A backslash-newline continuation contributes nothing
    - `\(` and `\)` contribute one byte each and do not terminate the string
    - No comment in the file still claims an over-count is harmless
  </acceptance_criteria>
  <done>Octal escapes are decoded; the disproved argument is gone from the file</done>
</task>

<task type="auto">
  <name>Task 2: Require real stream syntax, not a bare keyword</name>
  <files>internal/inspect/pdftext.go</files>
  <read_first>
    - internal/inspect/pdf.go — `scanPDFStreamRanges`, whose contract is deliberately lexical: it feeds the OPAQUE-RANGE filter, where over-matching is the safe direction. This detector needs the opposite guarantee, so it must narrow the ranges itself rather than change a scanner two other callers depend on
  </read_first>
  <action>
Keep using `scanPDFStreamRanges` for discovery — narrowing it would change a
shared primitive whose over-matching is correct for its other callers — and add a
syntactic check on top: a range only counts as a real stream when the `stream`
keyword is preceded by the end of a dictionary (`>>`, allowing whitespace).

A range that fails the check must be treated as OPAQUE, not dropped: the bytes
were not examined, so the report loses its right to say `absent`. Dropping it
silently would trade a false `present` for a false `absent`.
  </action>
  <verify>go test ./internal/inspect/ -count=1</verify>
  <acceptance_criteria>
    - A literal string containing the word `stream` no longer establishes `present`
    - Such a range yields `covered == false`, not a silent skip
    - Every corpus fixture still produces its PROVENANCE.md reference count
  </acceptance_criteria>
  <done>Only ranges with real stream syntax can establish presence</done>
</task>

<task type="auto">
  <name>Task 3: Skip only on proof; ambiguity costs coverage</name>
  <files>internal/inspect/pdftext.go</files>
  <read_first>
    - internal/inspect/pdftext.go — `pdfSkipStreamMarkers` and its doc comment, which claims over-matching is safe because "a skipped stream is not counted as examined". That claim is FALSE as written: `pdfStreamSkipped` leaves `covered` untouched, which is exactly what makes the false `absent` possible
    - internal/inspect/testdata/pdf/PROVENANCE.md — which fixture depends on font streams being skipped, and by which marker
  </read_first>
  <action>
Split the markers into two classes.

PROVEN — the key describes the stream itself, so skipping costs nothing:
`/Subtype /Image` in its two spellings, `/Length1` (present only on a
font-program stream), `/Subtype /Type1C`, `/CIDFontType0C`.

AMBIGUOUS — the key may merely be REFERENCED by a dictionary that also describes
a text-bearing stream: `/FontFile`, `/FontFile2`, `/FontFile3`. A font descriptor
POINTS at a font file; it is not one. These now yield opaque, costing coverage.

Fix the doc comment: over-matching was never free, because `pdfStreamSkipped`
does not touch `covered`. That was the mechanism of the reported defect.

Then MEASURE the corpus again. The stricter rule must not push the image-only
fixtures off `absent` — that verdict is the whole point of the image skip, and
trading one false verdict for another would be no improvement.
  </action>
  <verify>go test ./internal/inspect/ -count=1 && go test ./internal/inspect/ -run TestDifferential -count=1</verify>
  <acceptance_criteria>
    - A text-bearing Form XObject mentioning `/FontFile2` yields `covered == false`, never `absent`
    - `multipage_image_only.pdf` and `multipage_text.pdf` still yield `absent` under the DEFAULT budget
    - `embedded_font.pdf` still counts 42
    - The doc comment no longer claims over-matching is free
  </acceptance_criteria>
  <done>Only a proven marker skips; ambiguity degrades honestly</done>
</task>

<task type="auto">
  <name>Task 4: Pin all three as regression tests and regenerate the table</name>
  <files>internal/inspect/pdftext_test.go, internal/inspect/testdata/pdf/DIVERGENCE.md</files>
  <read_first>
    - internal/inspect/pdftext_test.go — the existing count and degradation tables these cases join
  </read_first>
  <action>
One test per finding, each carrying the reproduction from the review verbatim so
the case cannot be quietly generalised away, and each naming the wrong number it
used to produce.

Regenerate `DIVERGENCE.md` and check it did not move: these fixes must not change
any corpus verdict. If a row does move, that is a finding, not a detail.
  </action>
  <verify>OCTOCONV_REQUIRE_PDFTOTEXT=1 go test ./... -count=1</verify>
  <acceptance_criteria>
    - Three tests, one per finding, each failing against the pre-fix code
    - `DIVERGENCE.md` unchanged by the fixes
    - Full suite green with the differential required
  </acceptance_criteria>
  <done>All three findings are regressions under test</done>
</task>

</tasks>

<verification>
- `OCTOCONV_REQUIRE_PDFTOTEXT=1 go test ./... -count=1` — green
- `go build ./... && go vet ./... && gofmt -l internal/ cmd/` — clean
- `DIVERGENCE.md` regenerates identically
- Each fix proved by inversion: reverting it reddens its own test
- GATE LOCAL — CI auto-triggers are off
</verification>

<success_criteria>
- No false `present` from octal escapes or from a lexically matched keyword
- No false `absent` from an ambiguous skip marker
- Corpus verdicts unchanged
- Every disproved comment removed, not left standing beside corrected code
</success_criteria>
