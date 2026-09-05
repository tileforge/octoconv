---
created: 2026-09-02
area: sanitize
severity: medium
origin: PR #14 (plan 63-01), the highest-value follow-up of five review rounds
resolved: 2026-09-05
resolution: |
  FuzzPDFStreamPayloads in internal/convert/pdflex_fuzz_test.go, five
  properties over the reviewer's own inputs plus the pinned real files. The
  related item -- "a test that renders the flag and the xref-reading assumption
  together" -- is TestXrefAssumptionIsPinnedToTheGhostscriptFlag.

  The residual ("a decoy inside a stream that itself failed to resolve is
  reachable through the resynchronisation") is ANSWERED rather than left open:
  the mechanism is real and now observable -- the first pass records 10 decoy
  object headers out of a payload on the input the fuzzer produced -- and the
  xref-anchored retry is what makes it unreachable in the answer that is used
  (0 inside the ranges). That input is kept as a seed.

  Not closed by this: fuzzing did not find a NEW defect in the tokenizer itself
  in ~5.5M executions. That is evidence of nothing being easily reachable, not
  proof of correctness.
---

# Fuzz the PDF tokenizer

**Raised:** 2026-09-02, closing PR #14 (plan 63-01).
**Why now:** it is the single highest-value follow-up from five review rounds.

## What happened

`internal/convert/pdflex.go` was written after review found six byte-scanning
defects, and review then found four more *in the tokenizer itself* — arrays,
signed integers, comments between reference tokens, name escapes. Eighteen
findings over five rounds, every one arriving as a hand-built adversarial input
from a reviewer.

That is a person doing by hand what `go test -fuzz` does mechanically. The next
lexical context nobody thought of will not announce itself.

## What to do

A fuzz target over `scanPDF` / `pdfStreamPayloads` with properties, not
golden outputs:

* it never panics and never returns a range outside `[0, len(b))`;
* ranges are ordered and non-overlapping;
* every returned range is followed by `endstream` after only whitespace —
  the same invariant the code checks, asserted from outside it;
* `pdfNonStreamBytes` output is the same length as its input, and differs from
  it only inside returned ranges.

Seed it with the pinned corpus, this project's Ghostscript output, and the
hand-built fixtures from `pdflex_test.go`, `pdflexr4_test.go`,
`pdflexr5_test.go` — those are the reviewer's inputs and they are exactly the
seeds a corpus wants.

## Why this is not "more of the same"

`.planning/RETROSPECTIVE.md` already records the lesson: *don't predict a
parser, test against it*. Five rounds of hand-built cases is predicting. A fuzz
corpus is testing.

## Related, from the same PR

* Cross-reference **streams** (PDF 1.5+) are not read. Safe only because
  `sanitizePDF` forces `-dCompatibilityLevel=1.4`, and nothing asserts the two
  stay in step — a future change to that flag silently turns valid files into
  fail-closed rejections. Worth a test that renders the flag and the
  xref-reading assumption together.
* One residual in the retry is stated, not closed: a decoy inside a stream that
  itself failed to resolve is reachable through the resynchronisation. Fuzzing
  is the cheapest way to find out whether it is constructible.
