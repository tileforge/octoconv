---
phase: 64-tectonic-ninth-engine-class
requirements: [TEX-01, TEX-02, TEX-03, TEX-04, TEX-05]
autonomous: false
---

# Phase 64 — decisions

Written inline (`agents_installed: false`) and under the operator's standing
instruction to take the decision I would recommend; all four open areas were
put to the operator as questions with a recommendation each, and all four
recommendations were accepted. Every decision is recorded so a reviewer can
disagree with the choice rather than reverse-engineer it from the diff.

**`autonomous: false`, and here is exactly where.** One checkpoint:
`TECTONIC_ENGINE_TIMEOUT` is MEASURED, on the corpus D-04 fixes, and the
measurement is an operator-run step. Nothing else in this phase needs a human.

## What was already settled before this file

From `.planning/research/TECTONIC.md` and the ROADMAP's own
"Design decision locked before planning" — not re-opened here:

* **The binary** — tectonic 0.17.0, prebuilt static musl, both arches, pinned
  tarball + SHA-256, one file extracted. Byte-for-byte the shape
  `Dockerfile.pandoc-worker` already uses.
* **Shell escape** — `--untrusted` / `TECTONIC_UNTRUSTED_MODE` makes `\write18`
  and `\input{|cmd}` **impossible to enable regardless of other settings**,
  including an explicit `-Z shell-escape` on the same command line. A structural
  guarantee, not a default a later flag can undo — the same distinction this
  project draws everywhere else (a DB CHECK over an API-side default, a closed
  registry over a convention).
* **The bundle is reduced and self-built**, not full TeXLive (~2.69 GiB on a
  layer, an order of magnitude above anything in this fleet). Network access at
  runtime was **rejected explicitly, not left unconsidered**: it would let a
  client's `\usepackage` decide what gets downloaded — the exact vector
  `sanitize` spent six plans stripping out of other people's files.
* **The ninth engine class follows the established template** — own queue,
  `cmd/tectonic-worker`, own container, transient/terminal classification, a
  ninth ScaledObject leaving the existing eight byte-identical.

## What the codebase already has, which changes the shape of this phase

Scouted before the questions were asked, because two of the four answers depend
on it:

* **`latex` is ALREADY an admitted source format.** `convert.LooksLikeLaTeX`
  (`internal/convert/markupsniff.go`) plus the `source == "latex"` branch in
  `internal/api/handlers.go` already admit a `.tex` upload, gated on the
  declared source PLUS a fail-closed structural content check — LaTeX has no
  magic bytes, exactly like HTML. Pandoc already serves
  `latex → {html, docx, epub, md}`. **This phase adds `latex → pdf`; it does
  not invent the intake path.**
* **`LooksLikeXFromPrefix` exists precisely for this.** Phase 59 D-12 split
  every markup matcher into a `(r io.ReaderAt)` form and a `(prefix []byte)`
  form so a caller needing several matchers reads ONE window instead of one per
  matcher. The package scan in D-02 below sits in that window for free.
* **`Registry.Register` PANICS on a triple collision** (Phase 57 D-09). The
  planner must confirm `(convert, latex, pdf)` is unclaimed before registering
  — it is today (pandoc's latex targets are html/docx/epub/md, `pandoc.go:49`),
  but that is a fact to re-check at implementation time, not to assume.

---

## D-01 — the package set is a minimum plus a documented way to grow (TEX-03)

**Decision:** ship a small, closed, declared set and a written extension
procedure, rather than a broad inherited one.

Starting set, grouped by what it is for. Everything below the floor line is a
choice; the floor is not:

* **Floor** — whatever the base LaTeX format needs to compile `article`/`report`
  at all. Stated explicitly in the plan after checking against a real build, not
  guessed here.
* **Math** — `amsmath`, `amssymb`, `amsthm`
* **Graphics and colour** — `graphicx`, `xcolor`, `colortbl`
* **Layout and styling** — `geometry`, `titlesec`, `fancyhdr`, `enumitem`,
  `caption`, `booktabs`, `tcolorbox` (and therefore `pgf`/`tikz`)
* **Links** — `hyperref`
* **Code listings** — `listings`
* **Language** — `babel` with the language data the consumers actually write in
* **Bibliography** — `biblatex` with `backend=bibtex`, plus `natbib` as the
  plain alternative. NOT `biber` — see D-07.

**Colour and styling are in the set because the operator asked for them by
name**, not because they looked reasonable. The first draft of this list had
neither `xcolor` nor any styling package in it, which would have made coloured
text, coloured tables and the ordinary `hyperref` link-colour setup all fail on
day one.

**`tcolorbox` pulls in `pgf`/`tikz`, and that is a real size cost** — TikZ is
among the largest things in TeXLive. It is in anyway: a document set described
as needing "colours and styling" that cannot draw a box or a diagram would
generate a TEX-06 extension request in its first week, and extending the bundle
is the expensive operation. The plan reports the bundle size with and without
it, so the cost is a measured number rather than this paragraph's assertion.

**`fontspec` is deliberately OUT.** It is the gateway to user-supplied fonts,
which is TEX-07 and deferred.

**Rejected — a ready TeXLive profile (`scheme-small`).** Broader coverage and
fewer decisions, but the set becomes *inherited* rather than *declared*, and
TEX-03 loses its content: we could say what we took, not what we support. The
whole reason the reduced bundle was chosen over the full one was to turn "which
LaTeX packages does OctoConv support" into a closed list this project can hold
a document equal to — the same idiom as `pandocPairTable`, `AllThreatClasses`
and the error-code vocabulary. A profile gives back the size win and throws
away the reason.

**Rejected — harvest the set from real consumer documents.** The most accurate
input, and the research is right that it is a product question. But it blocks
the phase on collecting documents that do not exist yet, and TEX-06 is already
in the backlog as "extension on request with a documented rebuild procedure" —
so the second half of that approach is already planned. Start narrow, extend on
evidence.

### The set is declared in TWO places, and they can drift

This is the part worth writing down, because it is this project's recorded
failure shape and it is not obvious here:

1. **The Go list** — what the API and the worker compare a document's
   `\usepackage` against.
2. **The bundle** — what tectonic can actually resolve at run time.

They drift in both directions and both are bad:

* a package in the Go list but **not** in the bundle → an admitted document
  that fails in the worker, for a package we told the client we support;
* a package in the bundle but **not** in the Go list → a document refused for a
  package the engine would have handled fine.

**So the mechanical test holds the Go list equal to the BUNDLE'S OWN MANIFEST,
not merely to the prose document.** A doc-sync test alone (the
`docs/ERROR-CODES.md` shape) proves the list matches what we *wrote down*; it
cannot notice that what we *shipped* is different. All three — Go list, bundle
manifest, `docs/TYPESETTING.md` — move together or the test fails.

## D-02 — one list, two gates, and the cheap one may abstain (TEX-03)

**Decision:** enforce in both places, from one list, in the shape 63-02 just
built and proved.

* **API, at admission** — scan the preamble for `\usepackage{...}` in the
  window `LooksLikeLaTeXFromPrefix` has **already read**, and refuse a package
  outside the set with a `422` **naming the package**, before `s.storage.Upload`.
  No second read, no peek re-stitched onto the stream (63-02 D-02's reason: a
  peek can only be wrong in the direction that silently truncates the client's
  file).
* **Worker, before/at the tectonic run** — the real bound. A package tectonic
  cannot resolve is **terminal**: it depends on the document, so a retry
  reproduces it exactly.

**The API gate is allowed to abstain, and that is the design.** A preamble
longer than the sniff window, or a `\usepackage` reached through `\input`, is
invisible to a bounded prefix scan. Failing closed there would refuse valid
documents on our own scanner's blind spot; admitting and letting the worker
decide costs one S3 write. Identical reasoning to PROBE-01, and the same
obligation: **the abstention is logged**, so an operator can see how often the
cheap gate gives up.

**Rejected — worker only.** Simpler, and tectonic is the only component that
truly knows. But every refusal then costs an S3 byte, a queue slot and a
container start, and TEX-03's "внятный отказ" arrives minutes late.

**Rejected — API only.** Cheapest, but nested `\usepackage` past the window
would sail through and tectonic's failure would be exactly the unexplained one
TEX-03 exists to prevent.

**One new error code, in BOTH vocabularies.** "Your document uses a package
this service does not ship" is a distinct client-actionable class — remove it,
or ask the operator to extend the set — so it is not `invalid_request`. It gets
one code, the same string synchronously and asynchronously, following D-03's
one-code-space rule and the two codes that already work this way
(`invalid_options`, and `too_many_pages` as of 63-02), with the same
cross-reference comment and the same equality pin test. The plan names the
string; `errcode`'s doc-sync test and `docs/openapi.yaml` keep the two halves
from drifting.

## D-03 — `--untrusted` is proven by running, and by inversion (TEX-04)

Non-negotiable, restated here so no plan can quietly satisfy it by citing the
manual. Phase 60 rejected `olevba` *after* the research had recommended it
unrun; the same discipline applies.

The phase must include a hostile document containing `\write18{...}` **and**
`\input{|"command"}`, run under the **production argv**, and an **inversion**
showing that removing `--untrusted` changes the outcome. Without the inversion
the test proves only that the document failed, not that our flag is why.

## D-04 — the corpus is decided before the measurement, and the formula is per-pass

**Decision:** three classes of committed document, each in a few sizes:

1. single-pass (no references, no bibliography);
2. cross-references (converges in two passes);
3. `biblatex` + `biber` (three or more passes).

**Why not a wall-clock p95.** The probe measurement established this and it
transfers exactly: when cost is set by a per-unit count and the corpus is
chosen by hand, a wall-clock percentile measures *the choice of corpus* rather
than the engine. `scripts/probe-timeout-measure.sh` reports p95 and its own
`.env.example` says in as many words why 600s was NOT derived from it.

So the derivation is per-**pass** cost × a pass ceiling × a margin — the shape
OCR and probe both use (per-unit cost × ceiling × margin) — and the script
prints the formula with the number, bounds each file, and fails the run if any
file does not convert.

**The absolute ceiling is not ours to choose.** `RECONCILER_ACTIVE_STALE_AFTER`
is 900s; any derived timeout must clear it with margin, as every engine's does.

**Accepted cost of committing synthetic documents:** they are ours, so they are
reproducible and the measurement can be re-run by anyone. If real consumer
documents become available they are worth more than any synthetic corpus — the
operator chose the committed corpus for v1, and using real documents as a
cross-check later is an improvement, not a contradiction.

## D-05 — the bundle is built once, offline, and fetched by SHA

**Decision:** a committed script builds the reduced `.ttb` via
`tectonic-texlive-bundles`' `select`/`pack`; the result is published as a
pinned artifact (GitHub Release of this repo); `Dockerfile.tectonic-worker`
downloads it and verifies SHA-256 before use.

This is byte-for-byte the shape already used twice in this repo — the pandoc
tarball and the tectonic binary itself — so the container stage introduces no
new pattern, and image builds stay fast and reproducible.

**Rejected — build the bundle inside the Dockerfile.** No external storage
needed, but every image build then pulls a ~2.7 GiB TeXLive tarball and depends
on its continued availability; the build stops being reproducible *in time*,
which is the property the SHA pin exists to give.

**Rejected — commit the `.ttb` to the repository.** Maximum reproducibility,
but tens to hundreds of MB in git history forever, and every extension of the
package set (TEX-06, already in the backlog) adds another blob just like it.

**The SHA is verified against a locally computed digest**, not against a
published checksum page — `Dockerfile.pandoc-worker`'s own comment records that
rule ("never trust an unverified published checksum page alone") and it applies
to an artifact we publish ourselves just as much.

## D-07 — `biber` is not in the image, and that is a security decision

**Decision:** ship no `biber` binary. `biblatex` runs with `backend=bibtex`,
which tectonic executes **inside its own process** (BibTeX is embedded; the
`--pass bibtex_first` flag drives it).

**What the research found, and why it changed this.** Tectonic launches an
EXTERNAL `biber` — added in 0.7.0, detected by the appearance of a `.bcf` file,
and since 0.15.0 preferring a `tectonic-biber` executable over `biber`. That is
not mentioned in `.planning/research/TECTONIC.md`, which answered the shell-
escape question and stopped there.

Reading `driver.rs`, the shell-escape path is gated on
`shell_escape_mode != Disabled` while the external-tool pass appears **not** to
consult the security settings at all, and the argv it launches is taken from the
`.run.xml` file rather than being a fixed constant:

```rust
let mut argv = match s {
    (true, Ok(text)) if !text.trim().is_empty() =>
        text.split_whitespace().map(|x| x.to_owned()).collect(),
    _ => vec!["biber".to_owned()],
};
```

**This is NOT recorded as a finding, because it was not run.** It is one file
read through a summarizer; where `text` comes from and how far `.run.xml`'s
contents are document-controlled were not established. Asserting a
vulnerability on unexamined input is the same error as asserting safety on
unexamined input, in the other direction, and this project has a record of the
second one it does not need to extend with the first.

**The decision makes the question moot rather than answering it.** With no
`biber` in the image there is nothing for that path to launch: the guarantee
becomes structural — the same class as `--untrusted` itself, and the same class
as "PostScript has no name trees" in the sanitizer — rather than behavioural. It
also removes the biber↔biblatex version coupling, which is a *recorded*
recurring failure in this ecosystem (tectonic issues #893, #1267) and would have
been a second one-rule-two-places axis on top of D-01's.

**Stated cost, because it is real.** Bibliography styles that only biber can
produce — much of `biblatex`'s sorting, localisation and advanced styles — are
unavailable. TEX-01 says "с библиографией" and that is still satisfied:
`backend=bibtex` and `natbib` cover ordinary citation and reference lists. The
limitation is DECLARED in `docs/TYPESETTING.md` alongside the package set, the
same idiom as the closed set itself.

**The operator was asked and chose this**, after being shown that the
alternative is a second pinned binary plus an obligation to settle the
`.run.xml` question by running a hostile document. What was wanted from
"styles" was document styling, not bibliography styling.

**If biber is ever added**, the `.run.xml` question stops being moot and must be
answered by running before the binary ships — recorded here so a later phase
inherits the obligation rather than the conclusion.

## D-06 — every new bound is proven by inversion

The house rule, restated for this phase because it applies to more than one
boundary here: the package-set gate (both halves), the `--untrusted` guarantee,
and the measured timeout. Each gets a test that fails when that boundary alone
is removed, and the inversion is **run** and its output **recorded** under the
phase's `evidence/`.

---

## Out of scope, said explicitly

* **TEX-06 — extending the package set on consumer request.** Already in the
  backlog with a documented rebuild procedure as its own item. This phase ships
  the procedure's *preconditions* (a declared set, a build script, a pinned
  artifact) and does not ship a request workflow.
* **TEX-07 — custom fonts.** Backlog. The reduced bundle's font set is whatever
  the declared packages need; user-supplied fonts are a separate intake and
  storage question. `fontspec` is kept out of the package set for the same
  reason — it is that feature's entry point.
* **`biber`-backed biblatex styles.** D-07. Declared as unavailable rather than
  silently absent.
* **Page/size ceilings for the tex engine.** Probe got one because a per-page
  cost model was *measured* first. This engine's cost model is per-pass and is
  measured in this phase — inventing an input bound before that measurement is
  the thing phase 60 established is not worth doing. If the measurement shows a
  bound is needed, it is a finding of this phase, not a premise of it.
* **`pdf → latex` or any other new direction.** The requirement is
  `latex → pdf`.

## Canonical refs

* `.planning/research/TECTONIC.md` — the four settled questions and the bundle
  decision this file builds on
* `.planning/ROADMAP.md` (Phase 64 section) — goal, six success criteria,
  `Autonomous: false` and its stated reason
* `.planning/REQUIREMENTS.md` — TEX-01..TEX-05 (in scope), TEX-06/TEX-07
  (deferred)
* `.planning/phases/63-sanitize-hardening-probe-page-ceiling/63-CONTEXT.md`
  D-04 — the one-bound-two-gates shape D-02 above reuses
* `internal/convert/markupsniff.go` — `LooksLikeLaTeX` /
  `LooksLikeLaTeXFromPrefix`, the window D-02's scan reuses
* `internal/api/handlers.go` — the `source == "latex"` admission branch that
  already exists
* `Dockerfile.pandoc-worker` — the pinned-tarball + SHA-256 pattern D-05 clones
* `.env.example` (PROBE block) — the worked example of a derived, stated,
  provisional timeout D-04 follows
* `scripts/probe-timeout-measure.sh` — the measurement harness shape to clone

## Deferred ideas

None raised during discussion. Every area put to the operator was in scope.
