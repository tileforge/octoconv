# Typesetting (`latex` → `pdf`)

OctoConv typesets LaTeX with [tectonic](https://tectonic-typesetting.github.io),
offline, from a bundle baked into the worker image.

Read the scope section before the package table. What this operation supports is
a **closed, declared set** — that is the whole design, not a limitation to be
worked around, and the boundary is more useful to know than the list.

## Scope of the guarantee

The service typesets a document that uses **only the packages listed below**,
with **no network access** at any point, and **no ability for the document to
run a program**.

Specifically, it:

- **does not fetch anything a document asks for.** The bundle is fixed at image
  build time. A `\usepackage` naming something outside the set fails; it never
  causes a download. This is the point: letting a document decide what gets
  fetched is the vector the `sanitize` operation exists to remove from other
  people's files.
- **does not execute shell commands from document content.** `\write18` and
  `\input{|command}` are refused, and refused structurally — `--untrusted` makes
  them un-enableable regardless of any other flag on the same command line.
- **does not ship `biber`.** See "What is deliberately unavailable".
- **does not accept user-supplied fonts.** The bundle's fonts are whatever the
  declared packages need.

### Bounds

Two numbers, and they are one decision rather than two.

| bound | value | why |
|---|---|---|
| source size | **2 MiB** (2 097 152 bytes) | 745 pages of dense typeset output on the measured corpus — past any real document. A `.tex` above it is refused with `413` before it is stored. |
| engine timeout | **271s** | `ceil(25.76 s/pass x 7 passes x 1.5)`, measured 2026-09-04 in the worker container at 2 CPU / 1 GiB, on a document 2.2% larger than the size bound. |

The seven is the **engine's** own limit: tectonic reruns until cross-references
and citations converge, then stops and says `stopping at 6 passes`. That is why
the timeout is derived per PASS and not from a wall-clock percentile — a typical
document costs two passes and a document with a bibliography costs seven, so a
percentile would report the mix of documents somebody chose to measure.

The two bounds cannot move independently. Cost is linear in source size, so the
timeout is only a bound while the input is: at the 100 MiB this engine first
inherited, a dense `.tex` would need roughly 9000s. Raising the size bound
without re-running `scripts/tectonic-timeout-measure.sh` admits work the timeout
will kill.

A timeout is TRANSIENT — the job is retried — while a compilation error is
terminal and is not retried at all.

## Supported packages

<!-- This table is held equal to internal/convert/texpackages.go AND to the
     built bundle's own manifest by a mechanical test. Editing it alone will
     fail that test, which is the intent. -->

| Package | For |
|---------|-----|
| `amsmath`, `amssymb`, `amsthm` | mathematics |
| `graphicx` | including images |
| `xcolor`, `colortbl` | colour, including in tables |
| `geometry` | page geometry |
| `titlesec`, `fancyhdr` | headings, headers and footers |
| `enumitem` | list formatting |
| `caption` | caption formatting |
| `booktabs` | tables |
| `tcolorbox` | coloured/framed boxes (brings `pgf`/`tikz`) |
| `hyperref` | links and cross-references |
| `listings` | source-code listings |
| `babel` | language support |
| `biblatex`, `natbib` | bibliographies — see the limitation below |

Everything the base `article`/`report` classes need is present too, and is not
listed here: it is the LaTeX format's own dependency closure rather than a
choice. Measured, it is 354 files and 44.5 MiB before any package above is
loaded — the declared set adds 2.4 MiB on top of it.

## What is deliberately unavailable

Absence from a list is not a declared limitation. These are declared.

**`biber`, and the `biblatex` styles that require it.** No `biber` binary ships
in the image. `biblatex` runs with `backend=bibtex`, which tectonic executes
inside its own process, and that covers ordinary citation and reference lists.
Styles and sorting that only `biber` can produce are unavailable.

This is a security decision, not an oversight. Tectonic launches `biber` as an
EXTERNAL program when a document produces a `.bcf` file, and takes the argv from
a `.run.xml` the document's own packages write. Whether `--untrusted` gates that
path was not established — and with no binary in the image the question does not
arise, which is a stronger guarantee than an answer would have been. If `biber`
is ever added, that question has to be settled by running a hostile document
first.

**Custom fonts, and `fontspec`.** Deferred (TEX-07). `fontspec` is the entry
point to user-supplied fonts, so it is not in the set.

**Anything else.** A document that loads a package this deployment's bundle
cannot resolve is refused with a message naming the package. It is never
silently typeset differently, and never accepted only to fail with an
unexplained engine error.

## Supported is not the same as accepted

The table above is what OctoConv **supports**: what we test, document, and will
help you with. It is not the admission rule.

What gets **accepted** is anything the shipped bundle can actually load, which
is a strictly larger set — a bundle necessarily carries the dependency closure
of the table above, plus everything the LaTeX format itself loads. Measured
against the built bundle: 608 `.sty` files for eighteen declared packages.

The distinction is deliberate and was measured, not assumed. Of twenty-three
common packages compiled against the real bundle, twelve resolve and produce a
PDF while being outside the table — including `fontenc`, `inputenc`, `lmodern`,
`textcomp` and `microtype`, all five of which appear in **pandoc's own default
LaTeX template**. Refusing those would mean rejecting documents this service
typesets correctly, which is worse than not promising them.

So: a package outside the table but inside the bundle is accepted and will
almost certainly work — it is simply not something we have committed to keeping.
A package outside the bundle is refused, by name, before the upload is stored.

**The refusal can arrive at either of two moments.** `POST /v1/jobs` scans a
bounded window of the preamble and refuses immediately with HTTP 422 and
`code: unsupported_package`. When the preamble runs past that window — or the
`\usepackage` is behind a macro or an `\input` — the API abstains, the job is
accepted, and the worker refuses it with the SAME `error_code` and the same
message. Which of the two you get depends on where `\begin{document}` falls in
your file, so the code deliberately does not distinguish them.

One case does NOT name a package: if the engine reports a missing file that is
not a `.sty` — a font definition, a class, a config file — the job fails with
the generic `engine_error`. That is on purpose. Naming a package for a missing
`.fd` would send you deleting a `\usepackage` line your document does not have.

## Asking for a package

The set is small on purpose and expected to grow on evidence. Extending it means
rebuilding the bundle (`scripts/tectonic-bundle-build.sh`), publishing the new
artifact, and updating three things that a test holds equal: the Go list, the
bundle manifest, and this table.

The procedure itself — how a consumer requests one, and what turnaround to
expect — is TEX-06 and not yet written. This phase ships its preconditions: a
declared set, a reproducible build, and an artifact pinned by digest.

## Keeping this document honest

A mechanical test proves the table above equals `internal/convert/texpackages.go`
in both directions, and that every package in the Go list is present in the
built bundle's manifest.

A second test holds the ADMISSION set equal to the bundle manifest, also in both
directions. It is written by `scripts/tectonic-bundle-build.sh` in the same run
that writes the manifest (`internal/convert/texresolvable.txt`), so the two
cannot come from different builds — and a third test holds the API's gate and
the worker's classification equal to each other, since this is one rule
deliberately implemented in two places.

The manifest half is the one with teeth. Holding the table equal to the Go list
proves this document matches what we *wrote down*; it cannot notice that what we
*shipped* is different — and a package we advertise and did not ship is a
document accepted and then failed. If the test fails, the fix is to correct
whichever side is wrong. It is never to relax the test.
