---
phase: 59-born-digital-pdf-routing
plan: 07
subsystem: inspect, convert, api
tags: [differential-testing, divergence, fixtures, hex-decode, end-to-end]

requires:
  - plan: 59-05
    provides: "The Go detector and the shared threshold this suite measures against pdftotext"
  - plan: 59-06
    provides: "The retired vocabulary — the phase's contract state this join test observes"
provides:
  - "internal/inspect/testdata/pdf/DIVERGENCE.md — a committed, regenerated divergence table"
  - "Three fixtures closing the branches the research could not measure"
  - "The BDPD-01 + BDPD-02 join, asserted in one response body"
affects: []

tech-stack:
  added: []
  removed: []
status: complete
---

# Plan 59-07 — Summary

Commit `d9831a1`. Closes the phase.

## It found a real defect on its first run — in our own detector

The CID/Identity-H fixture had to be **hand-built**: ghostscript produced a
simple encoding on all four paths tried (Type1 base font, embedded TrueType
subset, two CJK system fonts). The file declares the structure directly —
`/Type0`, `/Identity-H`, `CIDFontType2`, **no `/ToUnicode`**.

First differential run:

| | count |
|---|---:|
| inspect | **44** (raw hex digits) |
| pdftotext | **10** |
| threshold | 16 |

10 is *below* the threshold and 44 is *above* it. **The over-count did not
exaggerate a verdict — it manufactured one.** inspect reported a usable text
layer for a document the worker would send to OCR, steering a client away from
the OCR they need. Class `go_wrong_present`; the suite failed.

The cause was counting hex digits instead of decoding them — and the comment
covering that code argued the over-count was *harmless*: "it can only push a
present verdict further past a threshold it already cleared." **That reasoning
was wrong, and only the measurement showed it.**

Decoding makes the two agree exactly at 10, and not by luck: Identity-H writes
one glyph as two bytes, the high byte is `0x00` for the BMP, and the counter
already excludes NUL and space — so decoded-and-counted lands on the glyph
count, which for a text document is the character count.

## The table

| Class | Fixtures | Fails? |
|---|---|---|
| `agree` | 7 | — |
| `go_unknown` | 2 — undecodable filter, encryption | no |
| `go_wrong_present` / `go_wrong_absent` | 0 | **yes** |

`go_unknown` deliberately does not fail. It is a declared limit of this build,
and a suite that failed on it would be weakened *wholesale* the first time a
legitimately undecodable fixture arrived — the maintainer would relax the class
distinction rather than the one row deserving it.

The corpus **walks** its directories rather than listing names, so a fixture
added later is measured automatically and cannot be forgotten.

## A skip cannot pass for a gate

`OCTOCONV_REQUIRE_PDFTOTEXT=1` turns the absent-binary skip into a failure.
Verified by running with a stripped PATH: fails with the flag, skips cleanly
without it. Without that, "ok" would be indistinguishable between *proved
something* and *did not run*.

## What the corpus does not cover, written beside what it does

- A production-scale scan (every image-bearing fixture is three pages).
- LZW specifically — `lzw_filtered.pdf` uses ASCII85 for the same branch.
- A CID font **with** a `/ToUnicode` map — the committed one omits it, which is
  the harder case, but a file carrying one gives `pdftotext` a route inspect
  still cannot follow.

Gaps in the measurement, not established agreement.

## The join the six earlier plans left

Each proved its own half; none asserted that **one response body** says "this
PDF has a text layer" *and* lists the operation that hands it over. Asserted on
the `/ObjStm` fixture — the hardest form, where every other check is blind.

Its first draft over-claimed: it required that **no** check be clean, and
`extension_mismatch` failed it — correctly. That check compares a declared
filename extension against a detected format; an object stream cannot hide a
filename. The assertion is now scoped to the `pdf_` codes, with the reason
written down.

## Gate

| Check | Result |
|---|---|
| `go build` / `go vet` / `gofmt -l` | clean |
| `OCTOCONV_REQUIRE_PDFTOTEXT=1 go test ./...` | green, 30 packages |
| `DIVERGENCE.md` regenerates identically | yes |
| `TestPre59GoldenInspectPDF` | unchanged |
| `git diff go.mod go.sum` | empty |
