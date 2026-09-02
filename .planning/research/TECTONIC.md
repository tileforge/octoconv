# Research: tectonic for `latex → pdf` (v1.14)

**Scope:** narrow, one pass. Four questions the seed left open, answered against
the source and the release API rather than from memory.
**Date:** 2026-09-02

---

## 1. The binary — settled, and it fits an existing pattern

**tectonic 0.17.0**, released 2026-07-27. Prebuilt static musl targets published
for both architectures this project builds on:

- `tectonic-0.17.0-x86_64-unknown-linux-musl.tar.gz`
- `tectonic-0.17.0-aarch64-unknown-linux-musl.tar.gz`

That is the same shape `Dockerfile.pandoc-worker` already uses: download a
pinned release tarball, verify its SHA-256, extract one file. No source build,
no platform pin, no emulation — unlike `Dockerfile.document-worker`, which is
forced to `linux/amd64` by veraPDF.

**Consequence for the plan:** the container stage is a known quantity and costs
about what pandoc's did.

---

## 2. Shell escape — off by default, and un-enableable on demand

This was the question the seed said must be answered by running rather than
reading. The documented behaviour:

- Shell-escape (`\write18`) is **not enabled by default**; it takes an explicit
  `-Z shell-escape`, and `Tectonic.toml`'s own field defaults to false.
- `--untrusted`, or `TECTONIC_UNTRUSTED_MODE` set to any non-empty value,
  makes those features **impossible to enable regardless of other settings** —
  including an explicit `-Z shell-escape` on the same command line.

That second point is what matters. It is a structural guarantee rather than a
default that a later flag can undo, which is the same distinction this project
draws everywhere else (a DB CHECK over an API-side default, a closed registry
over a convention).

**Still to verify by running, not by reading.** Phase 60 rejected `olevba`
after the research had recommended it unrun; the same discipline applies here.
The phase must include a test that a document containing `\write18{...}` and
`\input{|"command"}` fails under the production argv, and an inversion showing
that removing `--untrusted` changes the outcome — otherwise the guarantee is
documentation, not a property of this build.

---

## 3. The bundle — this is the real decision, and it is about 2.8 GB

**Local bundles are supported.** `crates/bundles/src/lib.rs`'s `detect_bundle`
resolves a local path to one of three things: a directory, a `.zip`, or a
`.ttb` (`TTBFsBundle::open`). So a bundle baked into the image is a first-class
input, not a workaround.

**But the default bundle is enormous.** The built-in prefix is
`https://relay.fullyjustified.net`, which currently redirects to
`tlextras-2020.0r0.tar` — **2,819,359,232 bytes, ~2.69 GiB**. Baking the full
TeXLive distribution into the worker image puts it an order of magnitude above
anything in this fleet.

So the phase has a genuine choice, and it should be made deliberately:

| Option | Image cost | What breaks | Shape |
|---|---|---|---|
| Full TeXLive bundle | ~2.8 GB layer | nothing a normal document does | simple, expensive |
| **Custom reduced bundle** | tens to low hundreds of MB | a document `\usepackage`ing something outside the set | a CLOSED, DECLARED package set |

`tectonic-texlive-bundles` builds a bundle from a TeXLive tarball via a
`select` job whose content directory is inspectable and editable before `pack`.
A reduced bundle is therefore buildable and reproducible.

**The reduced bundle is the better fit for this codebase, and not only on size.**
It turns "which LaTeX packages does OctoConv support" into a closed, declared
set — the same idiom as `pandocPairTable`, `AllThreatClasses`, the error-code
vocabulary, and every other place this project prefers a list it can hold a
document equal to. A document using a package outside the set then fails
*deterministically and reportably*, which is a better failure than the
alternative on offer.

**The alternative is not on offer.** Letting tectonic fetch at build time means
giving the worker egress so that a client's `\usepackage` decides what gets
downloaded — the exact vector `sanitize` spends six plans stripping out of other
people's files. Recorded here so it is a rejected option rather than an
unconsidered one.

**Open for the phase to decide:** which packages are in the set. That is a
product question (what do the internal consumers actually write?), and it wants
a starting list plus a documented way to extend it, not a guess dressed as a
default.

---

## 4. Multi-pass — a cost model to measure, not assume

Cross-references and bibliographies need more than one pass. Tectonic reruns
internally until convergence rather than making the caller do it, so the cost
of one job is "however many passes this document needs", not "one subprocess".

This is the same shape the probe timeout turned out to have, where cost was per
page rather than per byte and a wall-clock p95 measured the corpus instead of
the engine. **The timeout must be measured on a corpus that includes a
bibliography and cross-references**, or it will be derived from single-pass
documents and be wrong for exactly the documents people typeset in LaTeX.

`scripts/probe-timeout-measure.sh` is a usable shape to clone: it runs the full
pipeline, streams rows as they complete, prints the recommendation *with its
formula*, bounds each file, and fails the run if any file does not convert.

---

## What this changes about the milestone

1. The container stage is routine (pinned static musl binary, both arches).
2. The security question has a real answer (`--untrusted`), and a real
   obligation to prove it by running.
3. **The bundle is the phase's design decision, not a detail** — full versus
   reduced is a ~2.8 GB versus ~100 MB image and, more importantly, an open
   versus closed package set.
4. The timeout needs a corpus with multi-pass documents in it, decided before
   the measurement rather than after.

## Sources

- https://github.com/tectonic-typesetting/tectonic/releases (release API, asset list)
- https://tectonic-typesetting.github.io/book/latest/v2cli/compile.html (shell escape, `--untrusted`)
- https://tectonic-typesetting.github.io/book/latest/ref/tectonic-toml.html (bundle field accepts local `.ttb`/`.zip`)
- `crates/bundles/src/lib.rs` (`detect_bundle`, `TECTONIC_BUNDLE_PREFIX_DEFAULT`)
- https://github.com/tectonic-typesetting/tectonic-texlive-bundles (custom bundle build, `select`/`pack`)
- `https://relay.fullyjustified.net/default_bundle.tar` (HEAD: 2,819,359,232 bytes)
