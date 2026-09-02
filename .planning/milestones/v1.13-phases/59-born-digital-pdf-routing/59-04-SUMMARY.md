---
phase: 59-born-digital-pdf-routing
plan: 04
subsystem: convert
tags: [routing, ocr, pdftotext, pinning-replacement, inversion-proof]

requires:
  - phase: 43-ocr
    provides: "ocrPDFGuardStage, pdftotext invocation, the 16-character threshold, born_digital.pdf fixture"
  - plan: 59-01
    provides: "Pinned pre-phase rejection with a written replacement protocol; golden snapshot recording the advertised-yet-refused contradiction"
provides:
  - "pdf->txt returns the extracted text layer; hocr/alto go on to a real render"
  - "extractPDFTextLayer / hasUsableTextLayer — extraction separated from decision"
  - "TestBornDigitalSentinelIsUnreachable — the proof Plan 59-06 will cite"
affects: [59-06, 59-07]

tech-stack:
  added: []
  removed: []
status: complete
---

# Plan 59-04 — Summary

Commit `d240872`. Closes BDPD-02's routing half.

## The route was never missing

`ocrPDFGuardStage` already ran `pdftotext`, counted the characters, **threw the
text away**, and failed the job terminally — while `GET /v1/formats` and
`POST /v1/inspect` both went on advertising `pdf→txt` as available. 59-01's
golden snapshot has that contradiction on record as a committed artifact.

Nothing new was built: no binary, no container, no queue, not even a new pair.
SEED-021's proposed `firecrawl/pdf-inspector` stays declined (D-05), and
`go.mod`, `go.sum` and `Dockerfile.ocr-worker` are unchanged.

## Shape

| Target | Text layer | Route |
|---|---|---|
| `txt` | usable | extracted text **is** the result — no render, no tesseract |
| `txt` | not usable | ordinary per-page OCR loop |
| `hocr` / `alto` | irrelevant | per-page OCR loop, **and `pdftotext` is never invoked** |

`detectBornDigitalPDF` split into `extractPDFTextLayer` (bytes **and** count
from **one** invocation) and `hasUsableTextLayer` (the pure decision). One
invocation is a requirement, not tidiness: re-running `pdftotext` to fetch the
text after counting would let the count that chose the route and the text
returned to the client disagree on the same input, inside one job.

The guard takes the target and remains the **single** decision point —
`convertPDF` never re-derives the routing. Verified mechanically: one definition,
one call site. Page-count cap runs first on **every** route, extraction
included; bounding the size of the work is independent of the method chosen.

`ocrBornDigitalCharThreshold` → `ocrUsableTextLayerThreshold`. Value unchanged
(16); what changed is the sentence around it — a rejection threshold became a
routing threshold.

## Three comments now bound to tests, by name

The comment BDPD-02 named said the rejection was steering clients *"to the
document engine instead"*. That route never existed: `pdf` is a source format in
exactly one place in this repository, and every other engine only **produces**
PDFs. It had been false since it was written, and nothing could notice — **a
comment cannot fail**.

| Comment claims | Test that can fail |
|---|---|
| a born-digital PDF has a route | `TestPDFToTxtRouteExists` |
| the sentinel is unreachable | `TestBornDigitalSentinelIsUnreachable` |
| box targets never invoke pdftotext | `TestOCRPDFBoxTargetsSkipTextExtraction` |

The last one is proven by **absence**: PATH carries `pdfinfo` and `pdftoppm` but
not `pdftotext`, so an implementation that still probed would fail with a
missing-`pdftotext` error. A code review could only have asserted it.

## Pinning replaced, openly

The 59-01 suite's three assertions are **inverted**, in this commit, with the
commit message saying so — the protocol its own header laid down. Not deleted,
not weakened until it passed both before and after. Its header now describes
post-phase behavior and records what it used to pin.

`TestPre59PairAdvertisedToday` is **unchanged**, deliberately: it is the half
that survives. Before, it sat beside a terminal refusal and was a contradiction.

`TestOCRPDFBornDigital` (Phase 43) is replaced rather than adapted — it asserted
the opposite outcome, and adapting it would have meant editing an assertion
until it agreed.

## Inversions — two properties, two reverts

| Inversion | Red |
|---|---|
| Unconditional refusal restored (pre-phase) | `TestPre59PDFToTxtReturnsTheTextLayer`, `TestPre59PDFToHOCRIsNotRefused…`, `TestBornDigitalSentinelIsUnreachable`, `TestPDFToTxtRouteExists` — **4** |
| Guard made target-blind (extract for hocr too) | `TestOCRPDFBoxTargetsSkipTextExtraction`, `TestPre59PDFToHOCRIsNotRefused…` — **2** |

The second is the one that matters: it isolates target-dependence specifically,
rather than merely showing the refusal is gone.

## One deviation from the plan, caught and reverted

The first pass deleted `ErrOCRBornDigital` outright. The plan explicitly keeps
it until 59-06, because `internal/worker` references it in three places whose
unwinding — retiring `born_digital` from the active vocabulary while keeping it
documented for jobs that already failed with it — is that plan's entire subject.
Deleting it here would have broken the build across packages to save one commit.
Restored, with a doc comment stating it is unreachable and naming the test that
proves it.

## Gate

| Check | Result |
|---|---|
| `go build` / `go vet` / `gofmt -l` | clean |
| `go test ./...` | green, 30 packages |
| `grep -c 'runCommand(ctx, "pdftotext"'` | **1** — no second invocation |
| `ocrPDFGuardStage` | 1 definition, 1 call site |
| `git diff go.mod go.sum Dockerfile.ocr-worker` | empty |
| `TestPre59GoldenInspectPDF` | unchanged |

## Handoff to 59-06

`TestBornDigitalSentinelIsUnreachable` is the justification 59-06 must cite when
it edits `internal/worker/pinning_test.go`, whose header declares any assertion
change a protocol violation requiring explicit justification. It covers **all
three** targets, not only the two that changed behavior — a proof of
unreachability that skips a route proves nothing about that route.
