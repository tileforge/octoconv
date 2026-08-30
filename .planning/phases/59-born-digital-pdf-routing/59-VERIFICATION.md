---
phase: 59-born-digital-pdf-routing
status: passed
verified: 2026-08-30
requirements: [BDPD-01, BDPD-02]
plans_executed: 7
---

# Phase 59 — Verification

Goal-backward, against the ROADMAP's own two Success Criteria and the phase
Goal — not against the requirement text. That distinction earned its keep: it
produced one fix after all seven plans were green (see *Found by verification*).

## SC1 — the report answers the text-layer question, as honestly as anything else it says

**✓ SATISFIED.** Live output, default budget:

| Fixture | `text_layer` | `text_layer_reason` | `coverage.complete` |
|---|---|---|---|
| `objstm_born_digital.pdf` | `present` | — | true |
| `multipage_image_only.pdf` | `absent` | — | true |
| `lzw_filtered.pdf` | `null` | `parser_unavailable` | true |

Three distinct honest answers, including the one that matters most: a scanned
document gets a usable `absent` under the ordinary allowance, not a shrug.

The honesty is the same machinery as the rest of the report:
- Absence degrades, presence does not (D-09). Six degradation cases under test.
- `text_layer_reason` reaches `Coverage` through the existing `_reason` suffix
  convention — no special wiring.
- The reason vocabulary stays closed; no new `Reason`, no new `Code`, no new
  `Check`. `internal/inspect/findings.go` is untouched across the phase.

**The `/ObjStm` claim is observable, not argued.** One body reports
`uses_compressed_object_streams: true`, every `pdf_` check
`unknown/compressed_object_streams`, and `text_layer: present`. Asserted
together, because the claim is that they coexist.

## SC2 — a born-digital PDF gets a usable route, and the comment stops lying

**✓ SATISFIED.** Live, with `tesseract` and `pdftoppm` **absent from PATH**:

```
pdf → txt on born_digital.pdf:  err=<nil>  elapsed=63ms  bytes=101
text: "This is a born-digital PDF with real extractable text.…"
```

Before this phase: terminal failure with `born_digital`. The goal names the
alternative it was avoiding — a blind run through Tesseract on a 744-second
timeout — and the route now costs 63 ms without invoking Tesseract at all.

`grep -c 'document engine' internal/convert/ocrguards.go` → **0**. The claim it
made was false when written: `pdf` is a source format in exactly one place in
the repository. The replacement is bound to `TestPDFToTxtRouteExists`, named in
the comment itself, so it cannot quietly become false again.

`hocr`/`alto` are no longer refused: they proceed to a real render, because
`pdftotext` produces no word or line boxes and OCR is their only source.

## Requirements

| ID | Status | Evidence |
|---|---|---|
| BDPD-01 | ✓ SATISFIED | `text_layer`/`text_layer_reason`; corpus counts match `pdftotext` exactly; `DIVERGENCE.md` |
| BDPD-02 | ✓ SATISFIED | `pdf→txt` returns the layer; `born_digital` retired to the historical vocabulary; comment under test |

## Scope fence — held

Opportunity 2 (per-page Mixed-PDF classification) not started. No new binary, no
new container, no new queue, no new pair; `go.mod`/`go.sum`/`Dockerfile.ocr-worker`
unchanged across all seven plans. SEED-021's `firecrawl/pdf-inspector` remains
declined.

## Gate

Local — GitHub Actions auto-triggers are off (`a13e67b`):

```
go build ./...                                    OK
go vet ./...                                      OK
gofmt -l internal/ cmd/                           empty
OCTOCONV_REQUIRE_PDFTOTEXT=1 go test ./...        green, 30 packages
```

The env var matters: it turns the differential suite's absent-binary skip into a
failure. Verified by running with a stripped PATH — fails with it, skips cleanly
without it. A skip reported as "ok" is how a gate stops meaning anything.

## Found by verification, after all seven plans were green

The detector reported `malformed_input` for a stream whose filter this build
simply does not decode — a claim about the **file**, made from **our** limits.
Exactly the substitution the phase spent seven plans removing, surviving inside
the phase that removed it.

Fixed: three causes, three honest reasons (`parser_unavailable` for a missing
decoder or encryption, `malformed_input` only when a decoder we *have* fails
part-way, `work_ceiling_bytes` for our own budget), with the precedence tested.

This is why verification reads the goal and not the requirement text. "The
report states whether a usable text layer is present" was satisfied either way.
"With the same honesty about selectivity as any other statement" was not.

## What the phase measured rather than assumed

| Finding | Measured |
|---|---|
| Not every content stream is Flate | 2 of 3 pre-existing fixtures carry unfiltered ones |
| Image XObjects crowd out content | 2406 bytes of decompression with the skip, 182406 without (×76, three pages) |
| `'`/`"` inside a font program | 817 counted where 42 is true (×19) |
| Hex digits vs decoded bytes | 44 counted where 10 is true — **manufactured** a verdict, did not exaggerate one |
| Divergence from `pdftotext` | 7 agree, 2 honest `unknown`, **0 wrong** |

The last two rows are the phase's own case for D-10: the differential run found a
defect in *our* detector that no amount of reasoning had, and the comment
covering that code had argued the defect was harmless.

## Known gaps, recorded rather than closed

In `internal/inspect/testdata/pdf/PROVENANCE.md`, beside what the corpus *does*
cover: a production-scale scan, LZW specifically, and a CID font carrying a
`/ToUnicode` map. Gaps in the measurement, not established agreement.
