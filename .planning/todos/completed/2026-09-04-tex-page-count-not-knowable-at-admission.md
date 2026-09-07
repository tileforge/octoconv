---
created: 2026-09-04
area: tectonic
severity: low
origin: phase 64 plan 05, while measuring TECTONIC_ENGINE_TIMEOUT
updated: 2026-09-05
completed: 2026-09-07
status: closed -- one recommendation REFUTED by measurement, one shipped, one shipped differently than proposed, one left open with a reason
progress: |
  "A pass ceiling of our own (tectonic -r N)" does not exist. Measured against
  the real CLI in the shipped image: -r is "rerun the TeX engine EXACTLY this
  many times after the first" -- it forces passes rather than bounding them, so
  a document that converges in two would pay seven (1.076s -> 2.437s for the
  same output on single-xl). -Zhelp lists no bounding option either in 0.17.0.
  Adopting it would have made every ordinary job pay the worst case, to gain
  nothing: the engine already stops at seven on its own.

  What shipped instead is the honest half of that idea --
  TestEngineStopsAtSevenPasses pins the term the formula rests on. The 7 in
  ceil(25.76 s/pass x 7 passes x 1.5) is a property of a binary we pin by digest
  and could raise; if a future tectonic reruns further, the timeout silently
  describes a smaller bound than it claims, and now something says so.

  CPU-SECONDS SHIPPED, but NOT "instead of wall clock" as this todo proposed.
  Measuring first showed why the proposal was half right: the two quantities
  diverge exactly when the node is busy, and each catches something the other
  cannot.

  Measured in the shipped image at the chart's 2-CPU quota, the SAME document
  twice:

    idle                    0.96s wall   0.95s CPU   cpu/wall = 1.00
    oversubscribed 4x       3.67s wall   1.08s CPU   cpu/wall = 0.28

  Wall inflated 4x; consumption did not move. So wall clock alone refuses
  documents for the sins of their neighbours -- a document legitimately needing
  180 CPU-seconds dies at the 271s wall deadline on a busy node, and the client
  is told their file did not finish typesetting. That is a FALSE REFUSAL, the
  failure class this engine keeps producing.

  But replacing wall clock outright would have been wrong too: a process stuck
  rather than working consumes no CPU at all, and only the wall deadline
  catches it. So both are kept, and the pair of numbers is used to tell the two
  situations apart:

    - RLIMIT_CPU (soft == hard, so SIGKILL rather than a grace period a runaway
      would spend running) refuses a document on what it COST. Terminal.
    - the wall deadline firing while the engine used less than HALF its budget
      is now classified as STARVATION and is RETRYABLE -- the attempt took too
      long, the document did not.

  Applied by cmd/tectonic-worker's __sandbox-exec helper, which already existed
  as the only hook that runs between fork and exec.

  Deliberately NOT done: raising TECTONIC_ENGINE_TIMEOUT now that it is no
  longer the primary bound. It also derives TectonicUniqueTTL (IN-02), where a
  mismatch silently produces duplicate processing, so that is a separate
  decision with its own blast radius.

  STILL OPEN: re-measuring bytes/page against real consumer documents. Blocked
  on there being any -- the current figure comes from a generated corpus and
  says so (corpus/PROVENANCE.md). The 2 MiB ceiling remains a heuristic and the
  bounds above remain the real backstop, which is the correct order.
---

# The tex engine's real cost driver is not visible at admission

**Found:** 2026-09-04, phase 64 plan 05, while measuring TECTONIC_ENGINE_TIMEOUT.

## What

`TECTONIC_ENGINE_TIMEOUT=271s` is derived from a 2 MiB source-size ceiling,
because source size is the only thing about a `.tex` that can be measured before
the engine runs. But the quantity cost actually tracks is **typeset pages ×
passes**, and neither is knowable until the document has been typeset.

Measured relation on the corpus: ~2 860 bytes of source per typeset page. That
holds for prose with maths and tables. It does not hold for:

- a short document with an enormous generated table or a `tikz` figure that
  costs seconds per page;
- a `\includegraphics`-heavy document, where source bytes say nothing at all;
- a pathological document that expands (`\loop`, deep macro recursion) — small
  source, unbounded work.

The last one is a real DoS shape: the 2 MiB ceiling does not bound it, and the
271s timeout is what actually stops it. That is the correct order (the timeout
is the backstop), but it means the *ceiling* is a heuristic and should be
described as one.

## Why not fix it now

There is no cheap pre-flight page count for LaTeX — "typeset it and see" is the
count. Phase 63 removed an admission gate for exactly this class of reason (a
PDF page tree is a tree, so a page count could not be derived reliably from
untrusted bytes), and inventing a worse version of that here would repeat it.

## What would actually help

- A **pass ceiling** of our own (`tectonic -r N`), making the 7 in the formula
  ours rather than the engine's default. Cheap, and it turns a discovered
  ceiling into a configured one.
- Per-job **CPU-seconds** accounting rather than wall clock, so a pathological
  document is refused on what it consumed rather than on how long it took.
- Re-measuring `bytes/page` against real consumer documents when there are any;
  the current figure comes from a generated corpus and is honest about that
  (`internal/convert/testdata/tectonic/corpus/PROVENANCE.md`).
