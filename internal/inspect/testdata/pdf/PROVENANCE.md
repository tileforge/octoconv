# PDF corpus — provenance

These files are **inputs** to tests, never their output. Each one exists to make
one specific claim about the text-layer detector observable rather than argued.
Replacing a file means replacing the section below with it; a fixture nobody can
regenerate is a fixture nobody can dispute.

Committed by Plan 59-01, **before** the detector they judge (`internal/inspect/pdftext.go`,
Plan 59-05). A fixture added alongside a detector proves what the detector already
does. A fixture committed first proves what it must do.

The reference number in every section is:

    pdftotext <file> - | tr -d '[:space:]' | wc -c

which is the same unit the worker's own guard counts
(`countNonWhitespace`, `internal/convert/ocrguards.go`) — see 59-CONTEXT.md D-04
for why the two detectors must share a unit at all.

---

## `objstm_born_digital.pdf` — 3128 bytes, reference: **82**

```
gs -q -dNOPAUSE -dBATCH -sDEVICE=pdfwrite -dCompatibilityLevel=1.5 \
   -sOutputFile=internal/inspect/testdata/pdf/objstm_born_digital.pdf \
   internal/convert/testdata/ocr/born_digital.pdf
```

**Proves (59-RESEARCH.md R4, the empirical half of D-01):** this file contains
`/ObjStm`, so every other PDF check in `pdf.go` must degrade to
`unknown/compressed_object_streams` — and the text-layer verdict must stay
`present` anyway. ISO 32000 §7.5.7 forbids only stream objects and the encryption
dictionary from living inside an `/ObjStm`, and a text layer lives in content
streams, which *are* stream objects. Without this file that argument is a citation
in a comment; with it, it is a property under test.

Checks: `strings … | grep -c ObjStm` > 0, and the reference number above.

**If replaced:** the one test asserting both facts in a single report body
(59-05 Task 4, 59-07 Task 4) loses its subject, and D-01 goes back to being an
assertion.

---

## `uncompressed_text.pdf` — 681 bytes, reference: **53**

Hand-built (Plan 59-01, Task 1). A minimal one-page PDF whose content stream
carries `BT /F1 18 Tf 72 700 Td (…) Tj ET` with **no `/Filter` at all** and an
**indirect `/Length` (`/Length 6 0 R`)**. Text:
`Uncompressed content stream carrying real extractable text.`

**Proves (59-RESEARCH.md R1, branch 2):** not every content stream is Flate-compressed.
Two of the three pre-existing OCR fixtures already carry unfiltered content
streams, so a detector that inflates Flate and silently skips whatever fails to
inflate is blind to a whole class of writers — and blind in the direction of a
false `absent`, the one direction D-09 forbids.

The indirect `/Length` is deliberate too: it confirms the stream-range scan can
delimit on the `endstream` keyword without resolving indirect objects.

Checks: `grep -c FlateDecode` = 0, and the reference number above.

**If replaced:** branch 2 of the three-way stream classification loses its only
positive fixture, and a Flate-only implementation would pass every remaining test.

---

## `embedded_font.pdf` — 36542 bytes, reference: **42**

```
printf '%%!PS\n/ArialMT findfont 24 scalefont setfont\n72 700 moveto (Divergence probe with a TrueType subset font here) show\nshowpage\n' > /tmp/probe.ps
gs -q -dNOPAUSE -dBATCH -sDEVICE=pdfwrite -dEmbedAllFonts=true -dSubsetFonts=true \
   -sFONTPATH=/System/Library/Fonts/Supplemental \
   -sOutputFile=internal/inspect/testdata/pdf/embedded_font.pdf /tmp/probe.ps
```

On a machine without `/System/Library/Fonts/Supplemental`, any command producing
a PDF with a non-empty `/FontFile*` will do — but record it here verbatim,
together with the reference number it yields, or the fixture becomes
unreproducible.

**Proves (59-RESEARCH.md R6) — the trap this corpus exists for:** two of PDF's
four text operators are the single bytes `'` (0x27) and `"` (0x22), and an
inflated font program is full of both. A rule that looks for an operator
"somewhere in the next 64 bytes" counts font binary as shown text.

Measured on this exact file, both numbers reproduced by the prototype at plan time:

| rule | result |
|---|---|
| loose — operator anywhere within 64 bytes | **817** |
| strict — operator as a delimiter-bounded token + font-program streams skipped | **42** |

A **×19 overcount**, invisible to every other fixture in the repository, because
none of the others embeds a font.

Checks: `strings … | grep -cE 'FontFile|Length1'` > 0, and the reference number above.

**If replaced:** the inversion in 59-05 Task 4 has nothing to break, and the
detector's own worst measured defect stops being detectable.

---

## What this corpus does NOT yet cover

Named here rather than omitted, because a corpus listing only what it covers
reads as complete (59-07 Task 1 closes these):

- **Non-Flate filter on a content stream** (LZW / ASCII85) — branch 3 of the
  classification: Go must answer `unknown`, `pdftotext` will answer with text.
  A legitimate divergence that belongs in the table as such.
- **Encrypted PDF with an empty owner password** — `pdftotext` reads it, the Go
  scanner cannot.
- **CID/Identity-H font** — the one divergence source D-08 predicts and
  59-RESEARCH.md could not measure: ghostscript produced simple encodings on
  both attempted paths, and Go agreed with `pdftotext` 42/42 on the TrueType
  subset above. Embedding a font is evidently *not* the trigger; a multi-byte
  encoding is.
