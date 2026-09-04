#!/usr/bin/env python3
"""Generate the tectonic measurement corpus (64-05 Task 1).

Three CLASSES, by how many TeX passes the document forces, and three SIZES
per class, so per-pass cost can be separated from per-document overhead:

  1. single  -- no \\label/\\ref, no toc, no bibliography
  2. xref    -- \\tableofcontents + \\label/\\ref + hyperref
  3. bib     -- biblatex with backend=bibtex (D-07: no biber in the image)

Every document uses ONLY packages from convert.TeXPackages (the declared set).
A corpus that needs a package the bundle lacks would measure the admission gate
rather than the engine.

Re-run this to regenerate the corpus; the .tex files are committed so the
measurement is reproducible by anyone, and re-runnable when the bundle changes.
"""
import os
import sys

OUT = sys.argv[1] if len(sys.argv) > 1 and not sys.argv[1].startswith("-") else "internal/convert/testdata/tectonic/corpus"

# Sections per size. Chosen so the three sizes span roughly one, ten and forty
# pages of set text -- enough separation for a per-page term to show up if one
# exists, without a corpus that takes an hour to measure.
# The xl size exists because the first run of the harness could not separate a
# per-PAGE term from fixed per-pass overhead at s/m/l: single-l (30 pages) came
# out FASTER than single-s (1 page), i.e. the signal was entirely inside the
# noise. If a per-page cost exists, a document an order of magnitude longer is
# where it becomes visible -- and a client submitting a 200-page book is the
# case the bound has to hold for.
SIZES = {"s": 2, "m": 20, "l": 60, "xl": 220}

# The CEILING document, generated on demand rather than committed: 1280
# sections is ~2 MiB of LaTeX source, which is the per-engine upload ceiling
# TECTONIC_ENGINE_TIMEOUT is derived from. It is not in the committed corpus
# because two 2 MiB files would be larger than everything else in testdata put
# together, and the generator is deterministic -- regenerate with:
#
#     python3 scripts/cmd/gen-tectonic-corpus.py <dir> --bound
#
# and time bib-bound.tex in the worker image. That is the run 64-05's number
# comes from, and re-running it is how the number is re-checked when the bundle
# or the engine changes.
BOUND_SIZES = {"bound": 1280}

PARA = (
    "The quick brown fox jumps over the lazy dog, and the typesetter must break "
    "this paragraph into lines that fit the measure without leaving a river of "
    "white space down the page. Kerning, hyphenation and the badness function "
    "all contribute to the cost of a single pass. "
)

# The label placeholder is on the SAME line as the first term, deliberately.
# The first version put it on its own line, which for the label-less `single`
# class left a BLANK line inside the equation environment -- illegal in math
# mode, and all three single-* documents failed to compile. Caught by the
# convert-before-timing check the plan requires, which is exactly what that
# check is for: a corpus with a file that does not convert measures nothing.
MATH = r"""
\begin{equation}%s
\int_{-\infty}^{\infty} e^{-x^{2}}\,dx = \sqrt{\pi}, \qquad
\sum_{n=1}^{\infty} \frac{1}{n^{2}} = \frac{\pi^{2}}{6}.
\end{equation}
"""

TABLE = r"""
\begin{center}
\begin{tabular}{lrr}
\toprule
Engine & Passes & Cost \\
\midrule
tectonic & many & measured \\
others & one & irrelevant \\
\bottomrule
\end{tabular}
\end{center}
"""


def body(n, cls):
    out = []
    for i in range(1, n + 1):
        out.append("\\section{Section %d}" % i)
        if cls == "xref" or cls == "bib":
            out.append("\\label{sec:%d}" % i)
        out.append(PARA * 3)
        out.append(MATH % (("\\label{eq:%d}" % i) if cls != "single" else ""))
        out.append(PARA * 2)
        if i % 3 == 0:
            out.append(TABLE)
        if cls == "xref":
            # A FORWARD reference on every third section: it cannot resolve on
            # the first pass, which is the whole point of this class.
            out.append("See Section~\\ref{sec:%d} and equation~\\eqref{eq:%d}."
                       % (min(i + 1, n), min(i + 1, n)))
        if cls == "bib":
            out.append("See Section~\\ref{sec:%d}; discussed by \\textcite{k%d} and \\textcite{l%d}."
                       % (min(i + 1, n), (i % 5) + 1, ((i + 2) % 5) + 1))
    return "\n".join(out)


def bibfile(n=5):
    entries = []
    for i in range(1, n + 1):
        entries.append(
            "@book{k%d,\n  author = {Knuth, Donald E.},\n  title = {Volume %d},\n"
            "  publisher = {Addison-Wesley},\n  year = {198%d}\n}\n" % (i, i, i))
        entries.append(
            "@article{l%d,\n  author = {Lamport, Leslie},\n  title = {Paper %d},\n"
            "  journal = {TUGboat},\n  year = {199%d}\n}\n" % (i, i, i))
    return "".join(entries)


def doc(cls, size):
    n = SIZES[size]
    preamble = [
        r"\documentclass{article}",
        r"\usepackage{amsmath}",
        r"\usepackage{amssymb}",
        r"\usepackage{booktabs}",
        r"\usepackage{geometry}",
    ]
    front = []
    if cls in ("xref", "bib"):
        preamble.append(r"\usepackage{hyperref}")
        front.append(r"\tableofcontents")
    if cls == "bib":
        # Embedded with filecontents because the API is single-input: a client
        # cannot upload a separate .bib today (64-04 finding). The corpus is
        # shaped like what this service can actually accept.
        preamble.append(r"\usepackage[backend=bibtex]{biblatex}")
        preamble.append(r"\addbibresource{corpusrefs.bib}")
    head = "\n".join(preamble)
    if cls == "bib":
        head = (r"\documentclass{article}" + "\n"
                + "\\begin{filecontents*}[overwrite]{corpusrefs.bib}\n"
                + bibfile()
                + "\\end{filecontents*}\n"
                + "\n".join(preamble[1:]))
    tail = r"\printbibliography" if cls == "bib" else ""
    return "%s\n\\begin{document}\n%s\n%s\n%s\n\\end{document}\n" % (
        head, "\n".join(front), body(n, cls), tail)


if "--bound" in sys.argv:
    SIZES = dict(SIZES, **BOUND_SIZES)

os.makedirs(OUT, exist_ok=True)
for cls in ("single", "xref", "bib"):
    for size in SIZES:
        path = os.path.join(OUT, "%s-%s.tex" % (cls, size))
        with open(path, "w") as f:
            f.write(doc(cls, size))
        print("%-12s %6d bytes" % (os.path.basename(path), os.path.getsize(path)))
