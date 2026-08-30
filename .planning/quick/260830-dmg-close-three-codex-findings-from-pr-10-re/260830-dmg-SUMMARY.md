---
quick_id: 260830-dmg
status: complete
date: 2026-08-30
description: close three codex findings from the PR #10 review
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
