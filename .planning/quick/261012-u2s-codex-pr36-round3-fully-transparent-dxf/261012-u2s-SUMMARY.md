---
phase: quick-261012-u2s
plan: 01
subsystem: cad-dxf
tags: [dxf, svg, pdf-judge, transparency, fonts, codex-pr36]
status: complete
requires: [phase-75 dxf pipeline, quick-261012-o2d, quick-261012-qds]
provides: [nothing_visible verdict, CADSVGFacts.Visible, PDF ink by graphics state, font slant as fontconfig reads it, DXF_EMPTY liveproof rows]
affects: [cmd/cad-worker/helper/cad-dxf, internal/convert caddxf*, internal/worker descriptors, scripts/cad-dxf-probe.sh, scripts/cad-liveproof.sh, scripts/cmd/cad-svg-check, scripts/cmd/cad-pdf-check, docs/ERROR-CODES.md, docs/MEASUREMENTS.md]
tech-stack:
  added: []
  patterns: [visible-element count computed independently by the helper, the Go validator and two Python walks; PDF ink decided by ExtGState alpha, soft mask and text rendering mode]
key-files:
  created:
    - internal/convert/caddxfvisible_test.go
    - internal/convert/testdata/cad/dxf/alpha_clear_geometry.dxf
    - internal/convert/testdata/cad/dxf/alpha_clear_all.dxf
    - internal/convert/testdata/cad/dxf/alpha_one_visible.dxf
    - internal/convert/testdata/cad/dxf/alpha_one_fill.dxf
    - internal/convert/testdata/cad/dxf-out/alpha_one_visible.{svg,render.svg,texts.json,pdf}
  modified:
    - cmd/cad-worker/helper/cad-dxf
    - internal/convert/caddxf.go
    - internal/convert/caddxfsvg.go
    - internal/convert/caddxfpdf.go
    - internal/convert/cad_docs_sync_test.go
    - internal/convert/testdata/cad/PROVENANCE.md
    - internal/worker/descriptors.go
    - internal/worker/pinning_test.go
    - scripts/cad-corpus-gen.py
    - scripts/cad-dxf-probe.sh
    - scripts/cad-liveproof.sh
    - scripts/cmd/cad-svg-check/main.go
    - scripts/cmd/cad-pdf-check/main.go
    - docs/ERROR-CODES.md
    - docs/MEASUREMENTS.md
decisions:
  - "The frame and bbox stay paint-agnostic; a drawing must in addition show at least one element, else cad_empty_result on svg, pdf and the png render (reverses 261012-o2d's 'the SVG of an all-transparent drawing converts')"
  - "Helper verdict empty/nothing_visible (exit 3) when no stroke, fill or text it emits has an opacity above 0; Go recounts from the SVG and refuses 0, and refuses client/render-only disagreement"
  - "The PDF judge counts a paint operator as ink only if /CA or /ca, the soft mask and the text rendering mode let it show; the empty-page rule uses InkOps"
  - "Font slant follows FreeType and fontconfig, proven by fc-query and an rsvg round trip: subfamily Oblique/Italic/Kursiv first, then fsSelection bit 9 (oblique) or bit 0 (italic), macStyle only when there is no OS/2 table"
  - "ErrCADNothingDrawable message: the drawing contains nothing visible: model space is empty, hidden, fully transparent or holds only 3D solids"
metrics:
  completed: 2026-10-12
  duration: about 3h
actuals:
  tokens: 23900
  tasks: 3
  commits: 3
plan_head_before: b30063b3edac10030a334d1e45c9035418a89e0f
plan_head_after: 27946fc439e0facce0ae417ef373367326ab1988
---

# Quick 261012-u2s: codex PR #36 round 3 (a DXF with nothing visible) Summary

A DXF whose every line, fill and text is fully transparent is now `cad_empty_result` as SVG, PDF and PNG. The helper refuses it (`empty`/`nothing_visible`), and Go refuses it again from the SVG it validates, before any rsvg run. One visible element among transparent ones is enough, and the frame keeps counting the transparent ones. The PDF judge now reads `/CA`, `/ca`, `/SMask` and `Tr` before it calls a paint operator ink. A font with only `fsSelection` bit 9 is emitted `oblique`; the rest of the slant rule now matches what fontconfig reads from the same file. Live: ALL PASS from a fresh clone at 27946fc, with 6 new DXF_EMPTY rows.

## Premise check: what codex said, what held

Ad hoc DXFs through the shipped image (`octoconv-cad-worker:latest`, ebd2e53 build) before any change:

| DXF | helper | SVG | PDF content stream (cairo 1.16) | Go PDF judge |
|---|---|---|---|---|
| LINE, CIRCLE, solid HATCH at 100% | `ok` | three paths at opacity `0.000` | background `re f` only | `empty page: 1 paint operations` |
| LINE + TEXT at 100% | `ok`, texts=1 | path and text at `0.000` | background only | `empty page` |
| LINE 100% + LINE 50% | `ok` | two paths | background, `/a1 gs` (`/CA 0.498039`), `S` | ok |

- **SVG route:** the finding held. An all-transparent drawing converted to an SVG with nothing visible, which 261012-o2d had documented as intended.
- **PDF route:** the finding did not hold for the shipped renderer. Cairo drops alpha-0 paint, so the judge saw only the background and refused, which is what o2d's claim rested on. But the judge itself was blind: it never looked at `gs`, and a renderer that writes `/CA 0` before `S`, or `3 Tr` before `Tj`, would have passed. The new crafted-PDF tests prove the old judge accepted both.
- **PNG route:** J1 reads pixels, so alpha was already in. Unchanged.
- o2d's evidence for "PDF and PNG refused" was an ad hoc drawing; it held for transparent geometry too, but only because of cairo.

## Font slant premise (verifier note)

The note says a font with only `fsSelection` bit 9 must be emitted oblique. I checked that against the renderer instead of the spec: `evidence/font-slant-premise.txt`.

- fontconfig 2.14.1 (`fc-query`) on DejaVuSans copies with edited tables:
  - bit 9 only gives slant 100; bit 0 gives 100;
  - subfamily `Oblique` gives 110, `Italic` and `Kursiv` give 100, with or without bits;
  - `macStyle` bit 1 with an OS/2 table gives 0 (FreeType reads OS/2 and ignores macStyle).
- Round trip: two faces in one family (roman + one variant) and rsvg-convert to PDF:
  - bit 9 face: requesting `normal` embeds the **roman** face, while `italic` and `oblique` embed the bit-9 face;
  - so HEAD drew a bit-9 face from a different file than the one that measured it. That is the qds defect class.
  - a macStyle-only face cannot be reached by any request: it is roman to fontconfig. HEAD asked for `italic` and got a synthetic shear of the roman face.
- HEAD disagreed with fontconfig on 5 of 10 variants: bit9, mac_with_os2, named_oblique, named_italic, named_kursiv (`evidence/probe-helper-RED-safety.txt`).
- New rule: the subfamily goes first (`oblique`, then `italic`/`kursiv`), then bit 9 gives `oblique` and bit 0 gives `italic`, and macStyle counts only when there is no OS/2 table.
- The probe row `font_style_bits` checks the helper against `fc-query` on every variant: slanted iff fontconfig slant > 0, and `oblique` wherever fontconfig says 110.
- The 6 shipped faces and the 22 faces of `fonts-dejavu-extra` still round-trip (`probe-fonts-shipped.txt`, `probe-fonts-extra.txt`).

## Sweep: every place that decides "is there content / is there ink"

| # | Decision point | Before | Blind spot | Now | Proof |
|---|---|---|---|---|---|
| 1 | helper `nothing_drawable` (frame has data) | geometry at any opacity | alpha ignored by design | unchanged (frame stays paint-agnostic) | `alpha_one_visible` bbox reaches the transparent MTEXT (y 120.09) |
| 2 | helper `zero_extent` | geometry | none (extent, not paint) | unchanged | `single_point` row |
| 3 | helper emission | none | all-alpha-0 output reported `ok` | `nothing_visible` when no emitted stroke, fill or text has opacity > 0; both render backends (the CSS one of `TEXT_AS_OUTLINE` too) | semantics rows; helper mutations H1 to H4, H8, H9 |
| 4 | Go SVG facts (`ValidateCADSVG`) | text opacity only | shapes not judged at all; `fill="none"`, stroke none, stroke width 0, defs/clipPath, zero-size shapes | `Visible`: effective fill/stroke/width/opacity with `g` inheritance, shapes with an extent, text characters that show (per text node, so a visible empty `tspan` does not count) | 39-case table; every committed helper SVG; Go mutations G3 to G10 |
| 5 | Go cross-check / `dxfDrawing` | texts, opacity, fonts, page | no visible-count check | client and render-only `Visible` must agree; 0 is `ErrCADNothingDrawable`+`ErrCADEmptyResult` before rsvg, on svg, pdf and png | `TestCADDXFRefusesADrawingWithNothingVisibleOnEveryRoute` (2 drawings x 3 routes, no rsvg, no output, no sidecar); G1, G2 |
| 6 | PDF judge (`JudgeCADDXFPDF`, `JudgeCADPDF`) | counted operators | `/CA`, `/ca`, `/SMask`, `Tr`, empty shows, unknown `gs` | `InkOps` from the graphics state; `q`/`Q` save it; unknown ExtGState, alpha outside [0,1], `Tr` outside 0..7 rejected | 22 ink cases, 8 rejection cases, 8 helper PDFs; P1 to P11 |
| 7 | pixel judge J1 | pixels | none: reads the rendered alpha/channels | unchanged | live DXF_EMPTY png rows fail earlier, at 5 |
| 8 | render route measurements sidecar | written after J1 | none | refused at 5, no sidecar | `noSidecar` in the route test |
| 9 | sandbox preflight (`cadsandbox.go`) | uses 5 and 6 | none of its own | inherits 5 and 6; its fixture has visible text | full suite |
| 10 | `cad-pdf-check` | library | as 6 | prints `ink_ops` | live DXF_PDF rows |
| 11 | `cad-svg-check` | library | no visible count | prints `visible` | live DXF_SVG rows |
| 12 | `cad-liveproof.sh` `svg_judged` | texts only | an inkless SVG passed | Go `visible` must equal an independent Python walk and be ≥ 1 on every SVG row | every `svg_judged` call: the 8 DXF_SVG rows, the letterbox SVG of each DXF_RENDER drawing and the cad-dxf DXF_RETRY |
| 13 | `cad-liveproof.sh` `pdf_judged` rows | judge ok | none beyond 6 | DXF_PDF asserts `ink_ops` ≥ 2; alpha_one_visible pins `paint_ops=2,ink_ops=2`, no text | live |
| 14 | probe judge `judge.py` | verdict/outputs | no visible count | `visible_count` (independent walk) for the one-visible cases | semantics |
| 15 | probe writeout PDF rows | `covered_subsequence=1` | would fail a text-free drawing | `alpha_one_visible` requires `pdf_text_chars=0` | writeout |
| 16 | mesh render (`cad-render`, STEP/IGES/STL) | J1 part pixels | not this class: meshes carry no colour or alpha (`cad-render` sets only the background alpha) | unchanged | — |

The Go and Python counts were compared on all 24 committed SVGs (dxf-out and dxf-cr): identical on every file.

## Decisions

- **Frame paint-agnostic, plus a visibility floor.** A frame that skipped transparent records would move the frame and bbox of every partly transparent drawing (text_alpha, text_alpha_faint) and their measurements. Keeping the frame and adding a floor changes nothing for a drawing that still converts. Every earlier helper output came out byte-identical.
- **The helper counts only what it can see at emission.** A stroke-width or drawing-command check in the helper is unreachable from a DXF: ezdxf's minimum stroke is 2 units, and a one-vertex polyline emits no path. The mutation probe showed no test could catch either check, so both were removed. Go enforces both against the SVG, which is where an untrusted helper could cheat.
- **Soft mask = not ink.** A paint under a soft mask other than `/None` cannot be proven visible without evaluating the mask. Cairo emits no soft mask for this pipeline's SVG subset (the allowlist has no `opacity`, `mask` or `filter`), so this refuses nothing real.

## Tasks and commits

| Task | Commit | What |
|---|---|---|
| 1 | 12f2cbd | helper `nothing_visible` and font slant, Go `Visible`/cross-check/refusal, PDF judge graphics state, worker message, check tools, tests, 4 fixtures, `alpha_one_visible` outputs, probe cases, provenance |
| 2 | 1dba3f4 | liveproof: 6 DXF_EMPTY rows, alpha_one_visible SVG/PDF/RENDER rows, visible and ink assertions on every SVG/PDF row |
| 3 | 27946fc | MEASUREMENTS and ERROR-CODES, docs sync test |

All three are SSH-signed (`G`), with no Co-Authored-By trailer.

## TDD evidence

- `red-go-compile.txt`: the new tests do not compile on HEAD (`Visible`, `InkOps`).
- `red-go-assert.txt`: with the two fields stubbed, 63 subtests fail. Among them, the all-transparent drawing converts on svg/pdf/png, `/CA 0` + `S` is accepted, and `3 Tr` text is accepted.
- `red-go-worker.txt`: the pinned message.
- `red-docs.txt`: the docs sync test on the old docs (6 errors).
- `probe-helper-RED-semantics.txt` and `probe-helper-RED-safety.txt` (HEAD helper):
  - `alpha_clear_geometry` and `alpha_clear_all`: `verdict=ok`, failed `verdict_empty+reason_nothing_visible`;
  - `font_style_bits`: failed `bit9+mac_with_os2+named_oblique+named_italic+named_kursiv`.
- GREEN:
  - `go test ./...` ok on the working tree (`go-test-all-working-tree.txt`: `internal/convert` 428.8s, 31 packages ok; Go code final, helper and the alpha_one_fill fixture changed after it), and again as the ci-local gate tier at 27946fc, `go vet ./...` clean, gofmt clean.
  - Probe at the final helper (`probe-helper.txt`): semantics 26, safety 14, corpus 44 (fidelity included), writeout 49; 133 rows, 0 fail.
  - Fixtures: 48, all identical (`probe-fixtures.txt`). The 44 earlier files and the three alpha files from the first write came out byte-identical.
  - Writeout: every earlier SVG, texts.json, draft and PNG was byte-identical, and the 9 PDFs differed only in `/CreationDate`, so the committed bytes were kept (`writeout-pdf-dates.txt`).
- **Mutation probe:**
  - Go (`mutations-go.txt`): 21 of 21 caught. The first G8 attempt did not build (`p` unused), so it was rewritten and re-run: caught.
  - Helper (`mutations-helper.txt`): 9 of 9 caught. Each mutation ran a copy of the final helper against the probe's pre-registered expectations. H4, the outline backend left uncounted, is caught by the `TEXT_AS_OUTLINE` case; in the probe itself the corpus fidelity and the `outline.pdf` writeout rows catch it.
- `internal/convert/exec.go` is byte-identical to main.

## Live and gate results

| Gate | Result | Evidence |
|---|---|---|
| `scripts/cad-liveproof.sh` from a fresh clone at 27946fc, bundle copied, `tracked_dirt=0`, untracked override `127.0.0.1:18090:8090`, `CAD_LIVEPROOF_API_BASE=http://127.0.0.1:18090` | **ALL PASS**: dxf_svg=8, dxf_pdf=7, dxf_render=9, dxf_empty=6 (new); every other counter equals 261012-qds's | `evidence/cad-liveproof.txt`, `.transcript.log` |
| `scripts/ci-local.sh helm gate race` from the same clone | **helm PASS, gate PASS, race PASS** (`ALL REQUESTED TIERS PASSED`): helm with local v4.3.0 against the pinned v4.2.3, as the transcript warns; gate `internal/convert` 362.4s (10m budget); race `internal/convert` 777.0s (20m budget), no `DATA RACE`, load average 33 to 44 from other stacks | `evidence/ci-local-27946fc.log` |
| `scripts/sandbox-liveproof.sh cad` | **not run, not needed**: no new path, import, argv or engine. The helper reads only the font tables it already had open, and `fc-query` runs only in the probe harness. The liveproof SANDBOX row proved the confined worker | — |

Live rows:
- **DXF_EMPTY** `alpha_clear_geometry` and `alpha_clear_all` on svg, pdf and png render (6 rows): each `failed`, `cad_empty_result`, message "the drawing contains nothing visible: model space is empty, hidden, fully transparent or holds only 3D solids", no retry, 0 outputs.
- **DXF_SVG** alpha_one_visible: Go `visible=1` equals the Python walk; 8 clear strokes and 4 clear fills/texts kept, 1 half stroke.
- **DXF_PDF** alpha_one_visible: `paint_ops=2,ink_ops=2,expected_runes=0,invisible_text=2`, no text from pdftotext.
- **DXF_RENDER** alpha_one_visible 1280: the drawing judge passes.

## Deviations from Plan

1. **[Rule 2] Extra fixture `alpha_one_fill`.** Found by the helper mutation probe: removing the fill count was caught by no case, because every one-visible case drew a line. Added a drawing whose only visible element is a 50% solid HATCH. Fixtures are now 48, not 47. Commit 12f2cbd.
2. **[Rule 1] Two more slant mismatches fixed with bit 9.** The fc-query differential found 5 mismatches on HEAD, not 1: subfamily-only Oblique, Italic and Kursiv, and macStyle with an OS/2 table, besides bit 9. All five are the same class: the face rsvg draws must be the face that measured the text. Commit 12f2cbd.
3. **[Rule 3] Probe harness `outline_lines`.** Its first, counting run draws with every record suppressed, so it now ends `nothing_visible`. Its rc is ignored; the per-line runs keep theirs. Corpus fidelity is 44/44.
4. **Helper checks removed after the mutation probe:** stroke width and the drawing-command test are unreachable from a DXF (see Decisions). Go keeps both.
5. **Host disk:** the VM fell to 5 GiB free, and the probe's disk guard stopped one run. I ran `go clean -cache` (6.6 GiB of the user's regenerable Go build cache) to restore headroom. No docker object of any project was touched.

## Findings not fixed (for the orchestrator)

- **Faint is visible.** A fill or stroke at 1/255 opacity counts as visible in the SVG and PDF checks, while J1 on an opaque PNG needs a channel below 250. A drawing made only of near-invisible paint therefore converts to SVG and PDF, and its PNG is refused. The new rule is about alpha 0 exactly, as the codex finding was.
- **Paint the background hides.** White on the white `-b white` background counts as ink. ezdxf maps ACI 7 to black on a white background, so this needs an explicit true colour of white.
- **Geometric degeneracy in Go:** a `line` element of zero length with a round cap counts as visible, which is correct (rsvg draws a dot), while a `path` with only a moveto does not.

## Known Stubs

None.

## Cleanup

- The live proof stopped what it started; `docker compose -p octoconv stop` afterwards found everything already exited. Never `down -v`; no volume was touched.
- Images: the probe removed its own `engine75-*` and font-variant tags. `octoconv-api:latest` and `octoconv-cad-worker:latest` are now the 27946fc builds (`c0d36cf33909`, `f97a6f58af0f`). The previous builds `71fd2d10716a` and `43001b8b3cbf`, and qds's dangling `214047e0d440` and `6bda20713304`, no longer exist. I did not remove them: my only image removals were the probe's own tags. During the task `alpine:latest` also disappeared twice and was pulled again, so something else on this machine is pruning images.
- Build cache: 70 records were created by this task (from 18:47Z; every description is an octoconv cad-worker, api or probe font-variant step). They were pruned by ID in three passes (57, 9, 4). The 7 shared records back the kept images and were kept.
- Go build cache: `go clean -cache` twice (6.6 GiB, then 3.8 GiB regrown by the gates). It is regenerable, and the host disk was holding the OrbStack VM below its 6 GiB floor. No other project's container, image, volume or cache record was touched, `gsh-identity-service-envoy-1` on port 8090 included.
- Removed: the fresh clone (with its untracked compose override and bundle copy) and the scratch fixtures and fonts. `git worktree list` shows only the main checkout. No `$TMPDIR` probe or multipart leftovers.
- Free space at the end: OrbStack VM 8.8 GiB (at least 6 required), host 13 GiB.

## Self-Check

**PASSED**
- Commits 12f2cbd, 1dba3f4 and 27946fc are ancestors of HEAD; `git rev-list --count b30063b..HEAD` = 3 before this summary's commit; each is SSH-signed (`G`).
- The test file, the four fixtures, the alpha_one_visible outputs, the PLAN and the 18 evidence files exist.
- `internal/convert/exec.go` is byte-identical to main.
- `.planning/config.json`, `.vscode/` and `octoconv.code-workspace` were not touched.
