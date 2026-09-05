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
