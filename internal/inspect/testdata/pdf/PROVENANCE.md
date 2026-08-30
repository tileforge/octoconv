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

## `lzw_filtered.pdf` — 736 bytes, reference: **56**

Hand-built (Plan 59-07, Task 1). A minimal one-page PDF whose content stream is
`/Filter /ASCII85Decode` — a filter this build does not decode — wrapping
`BT /F1 18 Tf 72 700 Td (…) Tj ET`. Text:
`ASCII85 filtered content stream carrying real extractable text.`

ASCII85 rather than LZW because base85 is unambiguous to produce with the
standard library, and the branch under test is "declares a filter we did not
decode", which does not care which filter.

**Proves (59-RESEARCH.md R1, branch 3):** an undecodable stream is OPAQUE, not
empty. inspect must report `unknown`, never `absent`, while `pdftotext` answers
normally. In DIVERGENCE.md this is a `go_unknown` row — a declared limit of this
build, not a defect.

Checks: `grep -ac ASCII85Decode` = 1, and the reference number above.

---

## `encrypted_owner.pdf` — reference: **82**

```
gs -q -dNOPAUSE -dBATCH -sDEVICE=pdfwrite -sOwnerPassword=octoconv \
   -dEncryptionR=3 -dKeyLength=128 -dPermissions=-4 \
   -sOutputFile=internal/inspect/testdata/pdf/encrypted_owner.pdf \
   internal/convert/testdata/ocr/born_digital.pdf
```

Encrypted with an owner password and an EMPTY user password, which is what makes
it interesting: `pdftotext` opens it and extracts the full text, while the Go
scanner cannot decrypt the stream bodies at all.

**Proves (D-08, third named divergence source):** encryption is one of the three
structural reasons the two detectors can disagree. Another `go_unknown` row.

Checks: `strings … | grep -c /Encrypt` > 0, and the reference number above.

---

## `cid_identity_h.pdf` — 1088 bytes, reference: **10**

Hand-built (Plan 59-07, Task 1), because no available tool would produce one.
Ghostscript was tried on four paths — a Type1 base font, an embedded TrueType
subset, and two CJK system fonts — and every one produced a simple encoding.
The file therefore declares the structure directly: `/Subtype /Type0`,
`/Encoding /Identity-H`, a `CIDFontType2` descendant, and **no `/ToUnicode`
map**. The content stream shows one hex string, `<0048 0065 …>` — eleven
two-byte glyph ids spelling `Hello World`.

**Proves (D-08's primary predicted divergence) — and it earned its place
immediately.** On its first differential run, inspect counted **44** (the raw
hex digits) where `pdftotext` counted **10**, and 10 is BELOW the shared
threshold while 44 is above it. The over-count did not exaggerate a verdict, it
manufactured one: `present` for a document the worker would send to OCR. That
was a `go_wrong_present` row and it failed the suite.

The fix was to DECODE the hex string rather than count its digits. The two now
agree exactly at 10, and the arithmetic explains why that is not luck:
Identity-H writes one glyph as two bytes, the high byte is `0x00` for the BMP,
and the counter already excludes NUL and space — so decoded-and-counted lands on
the glyph count, which for a text document is the character count.

Checks: `strings … | grep -c Identity-H` > 0, `grep -c ToUnicode` = 0, and the
reference number above.

**If replaced:** the phase loses the only fixture that provoked a dangerous
divergence, and the hex-decode rule loses the evidence that it is required
rather than tidy.

---

## What this corpus does NOT yet cover

Named here rather than omitted, because a corpus listing only what it covers
reads as complete:

- **A production-scale scan.** Every image-bearing fixture here is three pages.
  The image-skip defence is measured instead of exercised at scale
  (2406 bytes of decompression with the skip, 182406 without — ×76), and
  59-RESEARCH.md R2 measured a single image stream at 3217 → 540000 bytes. A
  50-page scan would be another two orders of magnitude, and nothing here
  reaches that.
- **LZW specifically.** `lzw_filtered.pdf` uses ASCII85 for the same branch. If
  LZW ever gains a decoder, the branch needs a fixture that is genuinely LZW.
- **A CID font WITH a `/ToUnicode` map.** The committed CID fixture deliberately
  omits it, which is the harder case. A file carrying one would let `pdftotext`
  map glyphs to characters through a route inspect still cannot follow, and the
  two might diverge again in a way this corpus would not show.
