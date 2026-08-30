---
phase: 59-born-digital-pdf-routing
plan: 01
subsystem: inspect, convert, api
tags: [regression-pinning, golden-testing, fixtures, provenance]

requires:
  - phase: 58-synchronous-inspection
    provides: "D-15 pinning protocol; POST /v1/inspect and its report shape; pre58_pinning_test.go as the template reproduced here"
  - phase: 43-ocr
    provides: "born_digital.pdf fixture, ocrBornDigitalCharThreshold, restrictedPATH test helper"
provides:
  - "PDF corpus at internal/inspect/testdata/pdf/ with written provenance, committed before the detector it judges"
  - "Byte-exact pre-Phase-59 snapshot of POST /v1/inspect for four PDFs"
  - "Pinned pre-Phase-59 rejection of pdf->txt AND pdf->hocr, with a written replacement protocol"
affects: [59-02, 59-04, 59-05, 59-06, 59-07]

tech-stack:
  added: []
  removed: []
status: complete
---

# Plan 59-01 — Summary

Three commits, no production code touched. Everything here is evidence that
must exist before the thing it judges.

| Commit | What |
|---|---|
| `bcb304d` | PDF corpus + PROVENANCE.md |
| `7f5b5ae` | Golden snapshot of the inspect report for four PDFs |
| `b9127a9` | Pinned pre-phase rejection of `pdf→txt` and `pdf→hocr` |

## Task 1 — Corpus

Three fixtures, each with a measured reference number, each proving one thing:

| Fixture | Bytes | `pdftotext` non-ws | Proves |
|---|---|---|---|
| `objstm_born_digital.pdf` | 3128 | **82** | `/ObjStm` present, text layer still readable (D-01, R4) |
| `uncompressed_text.pdf` | 681 | **53** | content stream with no `/Filter` and an indirect `/Length` (R1 branch 2) |
| `embedded_font.pdf` | 36542 | **42** | the `'`/`"`-inside-a-font-program trap (R6) |

`uncompressed_text.pdf` was hand-built rather than generated: no available tool
emits an unfiltered content stream on demand. `grep -c FlateDecode` = 0 on it,
which is the whole point.

The trap fixture was verified to actually trap, not merely to exist — the
research prototype was re-run against it at both rule strengths:

```
loose rule (operator anywhere within 64 bytes) → 817
strict rule (delimiter-bounded token + font streams skipped) → 42
```

A fixture that does not reproduce the defect is decoration.

`PROVENANCE.md` also names what the corpus does **not** cover — non-Flate
filter, encryption, CID/Identity-H — because a list of what is covered reads as
complete. Plan 59-07 closes those.

## Task 2 — Inspect report pinned

One golden file, four cases. Determinism came free (fakeRepo's `PreAdmit`
returns nil; timeouts and size caps are compiled-in defaults), so **nothing is
masked before comparison** — the whole body is pinned exactly as a client
receives it. That is recorded in the header rather than left as luck.

The exception list is deliberately **not empty**, unlike Phase 58's: two new
`characteristics` keys (59-05) and the `coverage` object (59-02) are permitted
to move. This phase is supposed to change the body, just not anywhere else.

`git rev-list --count HEAD -- internal/api/testdata/pre59_inspect_pdf.golden.json` → **1**.

### What the snapshot caught on its own

For `born_digital.pdf` the pinned body contains:

```
convert -> alto [ocr] available=true
convert -> hocr [ocr] available=true
convert -> txt  [ocr] available=true
```

The report tells a client that `pdf→txt` is available while the worker refuses
that exact job terminally. The contradiction BDPD-02 exists to close is now on
record as a committed artifact, not a claim in a planning document.

And for `objstm_born_digital.pdf`, every PDF check is
`unknown/compressed_object_streams` with no text-layer key at all — which is
precisely the "before" that makes D-01's proof legible when 59-05 adds
`text_layer: present` to that same body without disturbing a single check.

## Task 3 — The rejection that must break

`internal/convert/pre59_ocr_pinning_test.go` pins `ErrOCRBornDigital` for **both**
`txt` and `hocr`. The hocr case is the load-bearing one: today the guard ignores
the target, so the two refuse identically, and D-06's split is only demonstrable
against that baseline.

PATH restricted to `pdfinfo`+`pdftotext`, so the pre-render ordering is proven
rather than asserted — a guard that let the file through would fail with a
distinguishable missing-`pdftoppm` exec error, not with the sentinel.

The replacement protocol is written into the file: 59-04 must replace these
assertions in the same commit as the guard change and say so in the message.
Deleting the file, or weakening an assertion so it passes both before and after,
is not a replacement — it is the loss of the evidence.

## Gate

Local (GitHub Actions auto-triggers are off, `a13e67b`):

| Check | Result |
|---|---|
| `go build ./...` | OK |
| `go vet ./...` | OK |
| `gofmt -l internal/ cmd/` | empty |
| `go test ./...` | green, 30 packages |
| `TestPre58Golden*` | still green — three `-update-golden*` flags coexist without `flag redefined` |
| `git diff go.mod go.sum` | empty |

No production file was modified by this plan.

## Note for 59-05

`git rev-list --count` on the golden file is the phase's anchor value. When
59-05 regenerates it under `-update-golden-59`, the count becomes 2 and the diff
must show **only** the two new characteristics keys. Anything else in that diff
is a blocking result, not a detail.
