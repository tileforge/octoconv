# Multi-file LaTeX projects cannot be submitted

**Found:** 2026-09-04, phase 64 (plans 64-04 and 64-03, independently).
**Requirement touched:** TEX-01.

## What

`POST /v1/jobs` takes ONE file. A real LaTeX project is usually several:
`paper.tex` + `refs.bib`, `\input{preamble}`, `\includegraphics{fig.pdf}`.
None of that can be sent today.

## Evidence

Two runs, both against the live service:

1. The end-to-end paper in 64-04 typesets its bibliography only because the
   `.bib` is embedded with `filecontents*`. With a separate `refs.bib` there is
   no way to upload the second file at all.
2. A document with `\input{hidden-preamble}` was accepted and failed with the
   generic `engine_error` — the engine reported `hidden-preamble.tex' not
   found`, which is a *correct* refusal to guess a package name (the parser only
   names `.sty` misses), but the underlying cause is that the file could never
   have been there.

## Why it is not just a missing feature

The schema already anticipates it: `job_inputs` carries an `ordinal` column and
`storage.InputKey` takes one, both since Phase 01 — and no code path populates
more than ordinal 0. So the gap is in the API and the worker, not the model.

## Options, in rough order of cost

1. **Accept an archive.** A `.zip` whose root contains one `.tex`. Reuses the
   ZIP walk the sanitize engine already has, and inherits its zip-bomb bounds —
   but it puts an archive parser in front of the tex engine, which is exactly
   the surface `--untrusted` and the offline bundle exist to keep small.
2. **Multi-part upload.** Several `file` parts, one job, ordinals in order.
   Honest to the schema, and the 2 MiB tex ceiling would then apply to the sum.
3. **Leave it, and document `filecontents`.** Cheapest; already written into
   `docs/TYPESETTING.md`'s corpus notes. It covers a `.bib` and nothing else —
   no figures.

Option 1 or 2 needs a decision on where the tex input ceiling (2 MiB, paired
with the measured 271s timeout) applies: per part, or to the total. Cost is
linear in total source size, so it must be the total.
