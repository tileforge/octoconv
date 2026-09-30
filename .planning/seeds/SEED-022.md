---
id: SEED-022
status: dormant
planted: 2026-09-01
planted_during: post-Phase-60 (PR #12 open, reviewing codex findings)
trigger_when: when a client asks for latex→pdf or md→pdf with real typesetting — or when "pandoc can already do markup, why not PDF" comes up for the second time
scope: Medium
audit_acknowledged:
  milestone: v1.15
  at: 2026-09-30
  status: dormant
---

# SEED-022: Tectonic as a ninth engine class for latex → pdf

[Tectonic](https://tectonic-typesetting.github.io/) — Rust, MIT/Apache-2.0, a
self-contained XeTeX/TeXLive redistribution shipped as one binary.

## The gap is real and is already documented as deliberate

Verified against the code, not assumed:

* `pandocPairTable` (`internal/convert/pandoc.go:46-51`) has no `pdf` target for
  any source. `latex` maps to `{html, docx, epub, md}` and nothing else.
* `Dockerfile.pandoc-worker:15-19` excludes a TeX toolchain by name — and names
  **tectonic specifically** among the exclusions:

  > Deliberately EXCLUDED: poppler-utils, tesseract, and above all
  > texlive/latex/tectonic/wkhtmltopdf/weasyprint — pandoc's ->pdf writer is not
  > in the allowlist […] so a TeX toolchain would be pure dead weight and is
  > deliberately never installed (D-02, 49-CONTEXT.md).

* The roadmap deferred `->pdf` to v2 for the same reason.

So this is not an oversight to correct; it is a scoped-out capability whose
scope-out reason (image weight) disappears the moment the toolchain lives in its
own image.

## Shape: a ninth engine class, not an addition to pandoc

The eighth class (`probe`, Phase 60) cost one plan now that the descriptor
pattern from Phase 56 exists: engine constant, task type, queue, descriptor,
`cmd/<name>-worker`, Dockerfile, compose service, chart deployment, ScaledObject.
The seven existing ScaledObjects rendered byte-identical, proven with a
before/after `helm template`.

Adding TeX to `Dockerfile.pandoc-worker` instead would undo a decision that
image made explicitly, and would put a ~200 MB toolchain into the memory-tightest
worker in the fleet (`PANDOC_WORKER_CONCURRENCY=1` because 2 OOMKills at 1 GiB).

## The one architectural risk, and it is the whole design question

**Tectonic fetches packages from the network on demand.** Its default behaviour
is to pull what a document `\usepackage`s from a remote bundle at build time.
Every worker in this fleet reaches only Postgres, Redis and S3; giving one of
them egress so a client's `.tex` can decide what to download is precisely the
vector `sanitize` (Phase 60) exists to strip out of other people's files.

So the seed is only harvestable with:

* an **offline bundle baked into the image and pinned by version/digest** —
  tectonic supports a local bundle (`--bundle` / `TECTONIC_BUNDLE_LOCKED`), and
  the pinned-tag discipline `Dockerfile.document-worker` uses for veraPDF is the
  precedent;
* **no egress** from the worker, asserted rather than assumed — otherwise the
  conversion silently depends on a third-party CDN and on whatever it serves
  that day;
* a check, **by running it rather than by reading docs**, that shell escape
  (`\write18`, `\input{|...}`) is off and cannot be re-enabled from document
  content. Phase 60 rejected `olevba` on exactly this kind of measurement after
  the research had recommended it unrun.

## Also to settle before planning

* **Timeout must be measured**, on a corpus of real `.tex`, in a container built
  from the image, at the deployed cgroup limits. Copying 65s/742s/744s/753s — or
  Phase 60's own unmeasured 180s — is a recorded project antipattern.
  `scripts/probe-timeout-measure.sh` is a usable shape to clone: it runs the full
  pipeline, prints the distribution with the recommendation's formula, and fails
  the run if any file does not convert.
* **First compile vs. multi-pass.** Cross-references and bibliographies need more
  than one pass; tectonic handles this internally, but the cost model (and hence
  the timeout) is not "one subprocess" the way pandoc's is.
* **Is the output PDF sanitize-worthy?** A `.tex` from an untrusted client
  produces a PDF this service generated. Whether that output should route through
  the probe engine before delivery is a decision, not an obvious yes.
* **Pair-table scope.** `latex → pdf` alone, or also `md → pdf` via
  pandoc-then-tectonic? The second crosses two engine classes in one job, which
  no current job shape does — that is a bigger question than the engine itself.

## Why not wkhtmltopdf / weasyprint / chromium

`ChromiumConverter` already covers `html → pdf`. This seed is specifically about
**typeset** output — real math, real bibliographies — which is what a client
asking for LaTeX is asking for and what an HTML renderer does not give.
