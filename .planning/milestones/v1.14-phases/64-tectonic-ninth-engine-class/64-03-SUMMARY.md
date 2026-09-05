---
phase: 64-tectonic-ninth-engine-class
plan: 03
status: complete
completed: 2026-09-04
requirements: [TEX-03]
deviation: "Rule 4 — the admission allowlist is the bundle, not the declared set. Measured first, confirmed with the operator before building."
---

# 64-03 — the gate refuses what cannot succeed, which is not what the plan said

## The measurement that changed the plan

The plan gates admission on `convert.TeXPackages`, the eighteen packages this
service declares. Before building it, twenty-three common packages were compiled
one at a time against the real bundle in the built container.

**Twelve resolve and produce a PDF while being outside the declared set** —
`fontenc`, `inputenc`, `lmodern`, `microtype`, `textcomp`, `url`, `array`,
`longtable`, `verbatim`, `amsfonts`, `subcaption`, `tikz`. Five are in pandoc's
**default LaTeX template**. One (`fontenc`) is in the paper 64-04 ran end to end
and verified.

So the planned gate would have refused, with "package not supported", twelve
kinds of document this service typesets correctly today. That is 63-02's defect
exactly — an admission gate that rejects valid work — which cost a whole gate
and a reworded PROBE-01. The measurement was put to the operator with the three
options and their costs; the bundle-derived allowlist was chosen before anything
was written.

Two sets, answering two questions:

- `TeXPackages` (18) — the **promise**: documented, tested against the bundle,
  listed in `docs/TYPESETTING.md`. Unchanged.
- `TeXPackageResolvable` (608 `.sty`) — the **admission rule**: what the engine
  can load at all. Written by `scripts/tectonic-bundle-build.sh` in the same run
  that writes `MANIFEST.txt`, so the two cannot come from different builds, and
  held equal to it in both directions.

TEX-03 is reworded the way PROBE-01 was, and its goal is untouched: a clear
refusal naming the package instead of an unexplained engine failure.
`pgfplots`, `siunitx`, `minted`, `csquotes` and `mathtools` are still refused —
before the upload rather than after a queue round trip.

## The scanner's own test found the failure the scanner is built to avoid

`ScanTeXPreamble` is allowed to MISS a package — macros, `\input` and
conditionals all hide one, and the worker is the backstop. It must never INVENT
one. `TestScanTeXPreambleNeverInventsAPackage` failed on its first run:

```
\newcommand{\loadit}[1]{\usepackage{#1}}
```

The scanner does not expand macros, so it read the literal `#1` — and the gate
would have refused the document for loading a package called `#1`. A package
name is a filename; names that cannot be one are dropped.

## The parser was written after the capture, against it

`internal/convert/testdata/tectonic/` holds what tectonic 0.17.0 actually
printed in the built image, with `PROVENANCE.md`. The exit code in it (1) was
measured on the `docker run` itself — the first attempt piped into `tail` and
read tail's `0`, which would have made "the engine exits 0 on a broken document"
a finding that was never true.

The parser names **only `.sty` misses**. A missing `.fd`/`.cls`/`.cfg`/`.tex` is
a fault in the bundle or in a package's own dependencies, and naming one would
send the client deleting a `\usepackage` line their document does not have. The
plan called that outcome a legitimate finding; it is instead the contract, with
six negative cases including the Fontconfig line that appears on every
successful run.

## Both gates, proven live

| | |
|---|---|
| package visible to the scan | `422 unsupported_package`, "…does not provide the package pgfplots" |
| preamble past the 1 MiB window | `202`, abstention logged, then job `failed` with the **same** code and the **same** message |

A client cannot tell which gate saw it — which is the point of one code space.
The difference is where `\begin{document}` falls in their file, not a fact about
their document.

**A third case is a finding.** A `\usepackage` hidden behind `\input{...}` fails
as generic `engine_error`, because what the engine reports missing is the
`.tex`, not a `.sty`. The parser declining to guess there is correct — and it
re-states 64-04's finding, that the single-input API cannot carry a multi-file
project at all.

## Inversions recorded

- gate moved below `storage.Upload` → the storage double fails naming the object
  it would have written;
- worker's `ErrTeXUnsupportedPackage` branches removed → the **verdict stays
  terminal** (the output contains "LaTeX Error" and the signature list catches
  it) but the code drops to `engine_error`. An inversion that asserted only the
  verdict would have reported success and proved nothing.
