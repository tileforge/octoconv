---
quick_id: 260830-dmg
status: complete
date: 2026-08-30
description: close eight review findings across three rounds on PR #10 (three from codex round 1, four from round 2, one self-found)
---

# Quick Task 260830-dmg — Summary

Commit `04a60ae`. Three findings from codex's review of PR #10, all reproduced
before being accepted, all producing a **wrong answer** rather than a missing one.

| # | Before | After |
|---|---|---|
| Octal escapes | counted **18**, truth **6** → false `present` | 6 |
| Lexical `stream` | counted **26** → false `present` | `unknown` with a reason |
| Ambiguous `/FontFile` | skipped, coverage kept → false `absent` | coverage lost, `unknown` |

## The one that matters most is mine twice over

The phase had already disproved the "harmless over-count" argument **by
measurement** for hex strings — and left the identical argument standing, in a
comment, in the neighbouring function:

> *"an octal escape contributes its literal digits rather than one character,
> which can only over-count a genuinely text-bearing stream that is already far
> past the threshold, never manufacture text in an empty one."*

Six characters counted as eighteen, against a threshold of sixteen. It
manufactured a verdict, exactly as the hex version had. Fixing the named
instance and leaving the reasoning beside it is how a defect class survives its
own fix — the same lesson `[[verify-against-goal-not-requirement-text]]`
records, arriving from the other direction.

## Where each fix had to live

**The stream-syntax check belongs in the detector, not in `scanPDFStreamRanges`.**
That scanner is deliberately lexical because its other callers build the
opaque-range filter, where over-matching is the *safe* direction — a range
wrongly marked opaque only suppresses a finding, never invents one. Narrowing it
would have traded this false positive for a false negative in the risk-token
filter, which is the worse trade.

**A rejected range is opaque, not dropped.** Those bytes were never examined, so
the report must not go on to claim absence on their behalf. Trading a false
`present` for a false `absent` is not a fix.

**Skip markers split by what they prove.** `/Subtype /Image`, `/Length1` and the
compact font subtypes describe the stream *itself*. `/FontFile*` is a
*reference*: a font descriptor points at a font file, it is not one. The old doc
comment claimed over-matching was free "because a skipped stream is not counted
as examined" — which was **false about the code**, since `pdfStreamSkipped` never
touched `covered`. The claim and the mechanism disagreed, and the mechanism won.
That gap was the entire defect.

## The risk this created, and the check for it

A stricter skip rule could push the image-only fixtures off `absent` — the
verdict the image skip exists to make reachable — and trading one false verdict
for another would be no improvement. Measured after the change: both scans still
answer `absent` under the default budget, `embedded_font.pdf` still counts 42,
and `DIVERGENCE.md` regenerates **byte-identical**. No corpus verdict moved.

## Inversions

| Reverted | Red |
|---|---|
| octal decode | `TestOctalEscapesAreDecodedNotCounted` |
| stream-syntax check | `TestLexicalStreamKeywordCannotEstablishPresence` |
| ambiguous → opaque | `TestAmbiguousSkipMarkerCostsCoverage` |

Each reddens its own test and no other — the fixes are independent, and none is
carried by another's coverage.

## Gate

| Check | Result |
|---|---|
| `go build` / `go vet` / `gofmt -l` | clean |
| `OCTOCONV_REQUIRE_PDFTOTEXT=1 go test ./...` | green, 30 packages |
| `DIVERGENCE.md` after regeneration | identical |


---

# Round 2 — codex defeated the first fix

Commit `98130b5`. Four more findings, **two of them direct hits on the fix
above**.

| Finding | Before | After |
|---|---|---|
| `>>stream` inside a literal | counted **17** → false `present` | 0, never a candidate |
| `/EmbeddedFile` counted as page text | counted **17** → false `present` | skipped, coverage kept |
| Comment between `>>` and `stream` | `unknown` for a readable document | `present`, handled natively |
| `/Length10` matched `/Length1` | skipped, coverage kept → false `absent` | complete name tokens |

## The lesson, stated plainly

Requiring `>>` before the keyword is bypassable by putting `>>` **inside the
literal too**. That is **two bypasses of one bound**, and the second showed why a
third clever comparison would not have helped: whether a keyword sits inside a
string depends on everything *before* it, so no backward look of any fixed
distance can decide it.

The layered predicate was replaced by `scanValidatedStreamRanges` — one forward
pass tracking literal strings, comments, and stream bodies. It answers exactly
one question and stays small enough to audit. This is
`[[dont-predict-a-parser-test-against-it]]` arriving from a new direction: the
rule was not "test against the parser" but "stop patching a bound that has
already been bypassed twice".

## The judgement I got wrong

I had **recorded** the comment case as a known accepted gap, on the grounds that
it fails safe. It does — and it also degrades a perfectly readable document from
`absent` to `unknown` for no gain. "Safe" was doing too much work in that
argument. The new scanner handles it natively, so doing it properly cost
nothing.

## One expectation changed rather than preserved

The lexical-keyword test required `unknown`, reasoning that unexamined bytes
cannot support an absence claim. With a real scanner the fake keyword is
recognized as string content and never becomes a candidate — nothing is
unexamined, and `absent` is honest. The old expectation was costing coverage for
a file we can read completely. Flagged in the PR reply because it *looks* like a
weakened assertion and is not.

---

# Round 3 — found by attacking my own fix

Commit `06a6bd4`. No third-round review had arrived, so I probed
`scanValidatedStreamRanges` directly: after round 2 it is the single thing
deciding what counts as page content, so a gap in it is a gap in everything
downstream.

**`(unterminated << /Length 30 >> stream … endstream`** yielded zero streams,
**full coverage**, and `text_layer: absent`. The scanner had examined none of the
document and claimed there was no text in it.

It arrived through the path that looked most like a safe default: an
unterminated literal swallows the rest of the buffer, which is fail-closed
against a false `present` — and says nothing whatever about the absence claim.
**Being safe in one direction read as being safe.**

`skipPDFLiteral` now reports whether the literal terminated, and an unterminated
one costs coverage. The probe corpus was promoted into the suite rather than
discarded: it found a defect, so it belongs there.

A second probe round covered budget and iteration boundaries — 50 000 tiny
streams in 0.06 s, deep paren nesting without recursion, a zero allowance
degrading to `work_ceiling_bytes`. No further defects.

---

# Standing across all three rounds

| | |
|---|---|
| Findings closed | **8** — 3 codex round 1, 4 codex round 2, 1 self-found |
| False `present` closed | 4 |
| False `absent` closed | 3 |
| Legitimate answer restored | 1 (the comment form) |
| Corpus verdicts moved | **0**, every round |
| `DIVERGENCE.md` | byte-identical, every round |
| Inversions | one per fix, each reddening its own test and no other |

Every finding was **reproduced before being accepted**, and every fix was proved
by reverting it.

The pattern worth keeping: three of the eight were defects in code written to
fix an earlier one. A fix is not evidence, and neither is the reasoning attached
to it — [[verify-against-goal-not-requirement-text]], [[scoping-comments-are-claims]].
