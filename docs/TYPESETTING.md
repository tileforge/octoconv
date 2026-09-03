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

**Anything else.** A document using a package outside the table is refused with
a message naming the package. It is not silently typeset differently.

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

The manifest half is the one with teeth. Holding the table equal to the Go list
proves this document matches what we *wrote down*; it cannot notice that what we
*shipped* is different — and a package we advertise and did not ship is a
document accepted and then failed. If the test fails, the fix is to correct
whichever side is wrong. It is never to relax the test.
