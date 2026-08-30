---
phase: 59-born-digital-pdf-routing
plan: 05
subsystem: inspect
tags: [pdf, text-layer, decompression, budget, inversion-proof, objstm]

requires:
  - plan: 59-01
    provides: "PDF corpus with measured reference counts; golden pin that caught the budget regression"
  - plan: 59-02
    provides: "text_layer_reason can carry read_failed and reach Coverage"
  - plan: 59-04
    provides: "ocrUsableTextLayerThreshold — the shared threshold this plan's parity test reads"
provides:
  - "text_layer / text_layer_reason characteristics for PDF uploads"
  - "internal/inspect/pdftext.go — bounded content-stream decode and text-operator scan"
affects: [59-07]

tech-stack:
  added: []
  removed: []
status: complete
---

# Plan 59-05 — Summary

Commit `0dba00f`. Closes BDPD-01.

## What it answers

| `text_layer` | When |
|---|---|
| `present` | enough text was seen — **regardless of coverage** |
| `absent` | no text **and** everything that could hold it was examined |
| `null` + reason | anything else |

The asymmetry is D-09. Text that was seen stays seen no matter how blind the
rest of the scan was — the same rule `pdf.go` already applies to findings. Only
the absence claim degrades.

Counts match `pdftotext` byte for byte across the corpus: **82, 82, 53, 42, 0,
0**. Those numbers are pinned, not just the verdicts — see the second inversion
for why that turned out to be necessary.

## The `/ObjStm` claim, made observable

`objstm_born_digital.pdf` reports `uses_compressed_object_streams: true`, every
risk check `unknown/compressed_object_streams`, **and** `text_layer: present` —
in one body, asserted by one test. ISO 32000 §7.5.7 forbids stream objects from
living inside an `/ObjStm`, and a text layer lives in content streams. The claim
is that the three coexist, so they are asserted together.

## Three defects found by measuring, none of them predicted

**1. The lookback crossed object boundaries.** The dictionary window took a flat
512 bytes and asked whether they contained `/Filter` — and on a plain scanned
document it reached back past an object boundary and picked up the *preceding*
object's `/Filter [ /FlateDecode ]`. Three raw streams classified opaque,
coverage lost, every scan degraded to `unknown`. Every **count** was still
correct, which is what made it dangerous: the numbers looked right and the
answer was useless on exactly the files this check exists for.

**2. I reproduced the PR9R9 defect I had documented in 59-03.** Charging
inflated bytes through `tr.Consume` exhausts the shared budget instantly,
because `dispatchInspectFamilies` pre-charges the analyzer's entire allowance
before calling it. Four small valid PDFs came back `work_ceiling_bytes`. **The
59-01 golden pin caught it** — the pin existed for exactly this and earned its
place on its first real test. The scan now spends a local allowance and signals
exhaustion through `MarkCeilingHit`, the receptacle built for a ceiling *inside*
an analyzer.

**3. Tidy arithmetic cost security coverage.** Carving the inflate reserve out
of `maxBytes` before sizing the head window keeps the total inside the
documented ceiling — and shrinks the risk-token window by a quarter. Three
existing tests immediately showed `/JavaScript`, `/OpenAction` and friends going
undetected on files near the allowance. Detection of executable content is not
tradeable for accounting neatness.

**The resulting decision, stated rather than buried:** the inflate bound is
additive and derived (`maxBytes/4`), so worst-case memory for one inspect
operation is now **1.25 × maxBytes**. `maxBytes` has always meant *bytes read*,
and decompression output is not a read. Calling it "inside the ceiling" would
have made the documented ceiling a half-truth. D-03's objection was to two
places that **charge**; this is one charge site and one bound — the same shape
`pdfMaxInflatedStreamBytes` already has per stream.

## Both inversions needed a second pass

| Inversion | First attempt | After |
|---|---|---|
| Remove the image/font skip | corpus still answered `absent` — the default allowance absorbs a three-page fixture, so the claim "this keeps scans answerable" was **undemonstrated** | measured the real cost (**2406** bytes with the skip, **182406** without — ×76 on three pages), added a test at an allowance between them |
| Loosen the operator match to "within 64 bytes" | `embedded_font.pdf` counted **647** where the truth is **42**, and every verdict test stayed green — both numbers mean `present` | pinned the **counts** against pdftotext's own answers; the corpus test now fails too |

Both first attempts described the defence instead of testing it. The verdict is
too coarse a signal to protect a count, and a fixture too small to exhaust a
budget cannot demonstrate a budget defence.

## Gate

| Check | Result |
|---|---|
| `go build` / `go vet` / `gofmt -l` | clean |
| `go test ./...` | green, 30 packages |
| `internal/inspect/findings.go` | untouched — no new Code, no new Check (D-02) |
| `git diff go.mod go.sum` | empty |
| Golden pin diff | **only** `text_layer: "present"` × 4 — one of the two permitted exceptions, nothing else moved |

## Handoff to 59-07

The two detectors are now demonstrably on one scale: the threshold parity test
reads both declarations out of source and requires them equal, and the corpus
counts match `pdftotext` exactly. The divergence table can therefore measure a
distance rather than compare incomparables — on this corpus that distance is
currently **zero**, which is itself the baseline 59-07 has to widen with
fixtures that provoke disagreement.
