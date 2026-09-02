---
phase: 64-tectonic-ninth-engine-class
date: 2026-09-02
supersedes_in_part: .planning/research/TECTONIC.md
---

# Phase 64 — research

`.planning/research/TECTONIC.md` (2026-09-02) settled the binary, the shell-escape
flag, the bundle decision and the multi-pass cost model. This file covers what
planning needed and that file did not have, and it **corrects one omission that
changes the phase's design**.

Everything below was checked against the CLI reference, the changelog and the
source. Where something was NOT checked by running, it says so — that
distinction is the point of this file.

---

## 1. The production argv is now known

`tectonic -X compile` (V2 CLI), verified flag by flag against the reference:

| Flag | Meaning |
|---|---|
| `-o` / `--outdir <dir>` | output directory; default is the input's own directory |
| `-b` / `--bundle <path or url>` | **use this bundle instead of the default** — a local path is a first-class value |
| `--untrusted` | "Input is untrusted — disable all known-insecure features" |
| `-k` / `--keep-intermediates` | keep intermediates (we do NOT want this) |
| `-p` / `--print` | print engine chatter — **this is how the worker gets a diagnosable failure message** |
| `-r` / `--reruns <count>` | rerun the engine exactly N times after the first (we do NOT set this: convergence is tectonic's job) |
| `--pass <pass>` | `bibtex_first` runs BibTeX before the first TeX pass |
| `--synctex` | not wanted |

`-b` answers the question the bundle decision left open: pointing tectonic at a
baked-in `.ttb` is a flag, not a workaround.

**`-r` is deliberately unset.** Tectonic reruns internally until convergence.
Pinning the count would replace "as many passes as this document needs" with a
number, which is precisely the thing the timeout measurement exists to
characterise rather than assume.

## 2. CORRECTION to TECTONIC.md — tectonic launches an EXTERNAL `biber`

TECTONIC.md answered "can the document execute a command" with `--untrusted`
and stopped. It missed a second external-process path that has nothing to do
with shell-escape.

From the changelog, verbatim:

| Version | Entry |
|---|---|
| 0.7.0 (2021-06-19) | "Tectonic's new support detects a need to run `biber` by checking for the creation of a file whose name ends in `.bcf`" |
| 0.7.1 (2021-07-04) | "Improve launching of `biber` by parsing the `.run.xml` file to find out which resource files are needed." |
| 0.8.1 (2022-02-28) | "Tectonic could obliterate `biber` input files whose locations were given as absolute paths" |
| 0.15.0 (2024-02-05) | "If an executable named `tectonic-biber` is installed, it's now preferred over `biber`" |

Biber is a Perl program and is **not** embedded; tectonic runs whatever it finds
on `PATH`. Two facts follow that matter to this phase:

**(a) The launch does not obviously honour untrusted mode.** In `driver.rs` the
shell-escape path is gated on `shell_escape_mode != Disabled`; the external-tool
pass appears not to consult the security settings, and the argv is taken from
`.run.xml` rather than being a constant:

```rust
let mut argv = match s {
    (true, Ok(text)) if !text.trim().is_empty() =>
        text.split_whitespace().map(|x| x.to_owned()).collect(),
    _ => vec!["biber".to_owned()],
};
```

**This is an unverified reading and is recorded as one.** It is one file seen
through a summarizer. Where `text` originates, and how far a document controls
`.run.xml`, were NOT established, and no hostile document was run. Claiming a
vulnerability here on that evidence would be the same error as claiming safety
on it — this project's recorded failure class, pointed the other way.

**(b) biber ↔ biblatex version coupling is a live, recurring failure.**
Tectonic issues #893 and #1267 are both "versions are incompatible"; the
community fix is pinning biber down to a specific version to match the bundle's
biblatex. Since our bundle pins biblatex and the image would pin biber, this
would be a second one-rule-in-two-places axis on top of the package list's.

**Design consequence (64-CONTEXT.md D-07): no `biber` in the image.** The
question becomes moot rather than answered — there is nothing to launch — and
the coupling disappears with it. `biblatex` runs `backend=bibtex`, executed
inside tectonic's own process. The obligation to settle the `.run.xml` question
by running is recorded against any future phase that adds biber.

## 3. What the phase must still establish BY RUNNING

Reading is not evidence here. Phase 60 rejected `olevba` after the research had
recommended it unrun, and that precedent is the reason these are listed as
obligations rather than assumptions:

1. **`--untrusted` actually refuses `\write18{...}` and `\input{|"cmd"}`** under
   the production argv — plus an inversion showing that removing `--untrusted`
   changes the outcome. Without the inversion the test proves the document
   failed, not that our flag is why (TEX-04).
2. **The floor of the package set** — what `article`/`report` needs to compile at
   all. Determined by building, not by listing.
3. **What tectonic's failure output looks like for an unresolvable package**, in
   enough detail to NAME the package back to the client (TEX-03). This is a
   parser against real output, and this project's own rule applies: do not
   predict the output shape, capture it and test against it.
4. **The bundle size**, with and without `pgf`/`tikz`, so `tcolorbox`'s cost is
   a number rather than a claim.
5. **The per-pass cost**, on the three-class corpus (64-CONTEXT.md D-04).

## 4. The bundle

`tectonic-texlive-bundles` builds a bundle from a TeXLive tarball through a
`select` job (its content directory is inspectable and editable) followed by
`pack`. `crates/bundles/src/lib.rs`'s `detect_bundle` accepts a directory, a
`.zip` or a `.ttb`, so a baked-in file is supported input.

**The manifest is the artifact that makes D-01's test possible.** The Go list of
supported packages must be held equal to what the bundle actually contains, not
merely to what `docs/TYPESETTING.md` says — a doc-sync test proves the list
matches what we wrote down and cannot notice that what we shipped is different.
The plan must therefore emit a machine-readable manifest as part of the bundle
build and commit it beside the Go list.

## 5. Fleet-shape facts confirmed in-repo

* `latex` is **already** an admitted source format: `convert.LooksLikeLaTeX` /
  `LooksLikeLaTeXFromPrefix` (`internal/convert/markupsniff.go`) and the
  `source == "latex"` branch at `internal/api/handlers.go:318`. LaTeX has no
  magic bytes, exactly like HTML, and the existing gate is "declared source PLUS
  a fail-closed structural content check".
* `(convert, latex, pdf)` is **unclaimed** today — pandoc serves
  `latex → {html, docx, epub, md}` (`internal/convert/pandoc.go:49`). Re-check
  at implementation time: `Registry.Register` PANICS on a triple collision
  (Phase 57 D-09), so a collision is a startup crash, not a silent overwrite.
* `Dockerfile.pandoc-worker` is the pinned-artifact pattern to clone: `ARG`
  version, per-arch `ARG *_SHA256`, `curl -fL --retry 5`, `sha256sum -c`,
  extract one file — and its own comment records the rule that the checksum is
  computed locally, never taken from a published checksum page.

## Sources

- https://tectonic-typesetting.github.io/book/latest/v2cli/compile.html (flag reference, quoted above)
- https://github.com/tectonic-typesetting/tectonic/blob/release/CHANGELOG.md (biber entries, quoted with versions)
- `tectonic/src/driver.rs` (biber launch site; read, NOT run)
- https://github.com/tectonic-typesetting/tectonic/issues/35 (biber support, closed)
- https://github.com/tectonic-typesetting/tectonic/issues/893, /issues/1267 (biber↔biblatex version incompatibility)
- https://github.com/tectonic-typesetting/tectonic-texlive-bundles (select/pack)
- `.planning/research/TECTONIC.md` (binary, shell escape, bundle decision, multi-pass model)
