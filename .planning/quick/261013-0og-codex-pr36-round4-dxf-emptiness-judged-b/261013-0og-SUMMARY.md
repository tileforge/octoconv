---
phase: quick-261013-0og
plan: 01
subsystem: cad-dxf
tags: [dxf, svg, pdf, png, pixel-judge, emptiness, codex-pr36]
status: partial
requires: [phase-75 dxf pipeline, quick-261012-u2s]
provides: [one pixel definition of an empty DXF drawing on svg, pdf and png, JudgeCADDrawingInk, cad-pixel-judge --ink, probe ink mode, six fixtures]
affects: [internal/convert caddxf/cadpixel/cadsandbox, internal/worker descriptors, scripts/cad-dxf-probe.sh, scripts/cad-liveproof.sh, scripts/cad-corpus-gen.py, scripts/cmd/cad-pixel-judge, docs/MEASUREMENTS.md, docs/ERROR-CODES.md]
tech-stack:
  added: []
  patterns: [emptiness decided by J1 on a raster of the drawing on a white page, the same check on every route; attribute counts kept only as earlier refusals]
key-files:
  created:
    - internal/convert/caddxfink_test.go
    - internal/convert/cadpixelmeasure_test.go
    - internal/convert/testdata/cad/dxf/{white_geometry,white_text,white_one_black,degenerate_fill,faint_only,zwsp_text}.dxf
    - internal/convert/testdata/cad/dxf-out/white_one_black.{svg,render.svg,texts.json,pdf}
  modified:
    - internal/convert/caddxf.go
    - internal/convert/cadpixel.go
    - internal/convert/cadsandbox.go
    - internal/convert/caddxf_test.go
    - internal/convert/cadsandbox_test.go
    - internal/convert/cad_test.go
    - internal/convert/cad_docs_sync_test.go
    - internal/convert/testdata/cad/PROVENANCE.md
    - internal/worker/descriptors.go
    - internal/worker/pinning_test.go
    - scripts/cmd/cad-pixel-judge/main.go
    - scripts/cad-corpus-gen.py
    - scripts/cad-dxf-probe.sh
    - scripts/cad-liveproof.sh
    - docs/MEASUREMENTS.md
    - docs/ERROR-CODES.md
decisions:
  - "A DXF drawing is empty when its validated render-only SVG, drawn by the confined rsvg-convert into the letterbox of a 2048 x 2048 frame on an opaque white page, has no J1 ink pixel (a channel below 250). Every route runs this one check, so svg, pdf and png agree by construction"
  - "The page is white on every route: the PDF's explicit -b white page, the white the client SVG declares for its page (#ffffff at opacity 0), the opaque PNG, and also a transparent PNG, because emptiness belongs to the drawing and not to the requested background"
  - "2048 px (CADMaxRenderSize) because coverage grows with the frame; a smaller check could refuse what a 2048 preview shows"
  - "The helper's nothing_visible and Go's Visible == 0 stay as refusals before any raster (they cannot refuse a drawing with ink); the PDF InkOps rule stays as a check of the PDF file. None of them decides emptiness"
  - "A malformed check frame is ErrCADDrawingFailed (engine fault, retried), never an empty result"
  - "Every DXF job now runs two rsvg-convert stages: ValidateCADTimeouts requires guard + dxf + 2 x rsvg + I/O <= engine (534 <= 886 s)"
metrics:
  completed: 2026-10-13
  duration: about 2h50m
actuals:
  tokens: 78000
  tasks: 3
  commits: 3
plan_head_before: bc6710d414e76e2bffb2efe7b8255d8e3320363d
plan_head_after: 0b5e730801c9f556a4909ffdf8ced5e9db184588
---

# Quick 261013-0og: codex PR #36 round 4 (a DXF's emptiness judged by pixels) Summary

**Partial: the code, tests, fixtures, docs and mutation probe are done and committed; the live gates (probe ink corpus and ceilings, cad-liveproof from a fresh clone, ci-local helm gate race) are NOT RUN, because the OrbStack VM stayed below the 6 GiB floor (it fell to 0.90 GiB) through disk use by other work on this machine.**

A DXF drawing is now empty when nothing in it darkens a white page. Go draws the validated render-only SVG with the confined `rsvg-convert` into the 2048 px letterbox on a white page and requires J1 ink. The svg, pdf and png routes all run that one check, so a drawing of only true-colour white, only alpha 1/255, only zero-area fills or only a U+200B text is `cad_empty_result` on all three, and a white drawing with one black line still converts on all three. The old attribute checks (helper `nothing_visible`, Go `Visible`, PDF `InkOps`) remain, but only to refuse early; none of them decides emptiness any more.

## Premise check (before any code)

Ad hoc DXFs drawn by the shipped image (`octoconv-cad-worker:latest`, 27946fc build): helper, then `rsvg-convert -b white` into the 2048 letterbox of the render-only SVG, ink counted as any channel below 250.

| DXF | helper | ink pixels at 2048 on white |
|---|---|---|
| LINE, CIRCLE, solid HATCH, SOLID, POINT, true colour white | `ok` | 0 (min channel 255) |
| TEXT and MTEXT, true colour white | `ok` | 0 |
| the white geometry plus one ACI 7 LINE | `ok` | 3594 |
| a collinear SOLID and a zero-area HATCH | `ok` | 0 |
| LINE, HATCH, TEXT at alpha 1/255 (opacity 0.004) | `ok` | 0 (min channel 254) |
| a fully transparent LINE plus an opaque TEXT of U+200B | `ok`, 1 text | 0 |
| an opaque TEXT of U+200B alone | `empty`/`nothing_drawable` (already refused) | - |

Every 0-ink row converted on svg and pdf to a blank file at HEAD. So the finding held, and it held for the whole class the phase-75 verifier listed (white, faint, degenerate fill, invisible character), not only for white.

## Design decision

- **One definition.** Empty = no pixel with a channel below 250 (J1, `cadBackgroundCutoff`, the same `cadMeasure` as the PNG route) in the 2048 x 2048 letterbox frame of the validated render-only SVG drawn on an opaque white page. Same argv and same rsvg stage and ruleset as the PNG route and the sandbox preflight: `-b white -w W -h H --page-width 2048 --page-height 2048 --left L --top T -o ink.png clean.svg`. `JudgeCADDrawingInk` and `JudgeCADDrawingPNG` share `cadDrawingInk`.
- **Background per route.**
  - PDF: drawn with `-b white`, an explicit white page, so the check is exact.
  - Client SVG: it declares its page as `<rect fill="#ffffff" ... fill-opacity="0.0">`. That is ezdxf's page colour, white, left unpainted (`BackgroundPolicy.OFF`), and ezdxf picks every colour for it (ACI 7 becomes black). The SVG is judged against the white it declares.
  - Opaque PNG: the white page itself.
  - Transparent PNG: also judged on white. A white-only drawing is now refused as a transparent PNG too, where before it converted to white strokes on alpha. Recorded in MEASUREMENTS as a consequence of "emptiness belongs to the drawing".
- **2048 px**, the largest render size. Stroke and glyph coverage grow with the frame, so a smaller check frame could refuse what a 2048 preview shows. The render route keeps its own J1 to J3 at the requested size, and its documented sub-pixel limit is unchanged.
- **Kept checks, and why none can refuse a drawing J1 sees ink in.**
  - Helper `nothing_visible` and Go `Visible == 0` fire only when no element has opacity above 0, which cannot leave a pixel off white. They save a raster for all-transparent drawings and keep the client/render agreement check.
  - PDF `InkOps` is a check of the PDF file, run after the drawing passed J1. An inkless PDF at that point lost what the raster drew, and refusing that PDF is correct.
  - The probe has a crafted-SVG differential for `Visible == 0`, but it was not run (see Gates).
- **Faults.** A check frame that is not a PNG, has the wrong size, is not opaque, or is a symlink is `ErrCADDrawingFailed` (retried), never `cad_empty_result`. `ink.png` joins the job-name collision list and is removed after judging.
- **Messages.** `ErrCADNothingDrawable`: "the drawing shows nothing on a white page: model space is empty or hidden, holds only 3D solids, or everything in it is fully transparent, white, too faint or too small to see". `ErrCADRsvgTimeout`: "the drawing took too long to render or convert to PDF or PNG; ...".
- **Performance.** `cadMeasure` now skips all-white rows on an opaque page (`bytes.Equal` against a white row). A differential test against a per-pixel reference covers 300 random frames x NRGBA/RGBA/Gray x sub-images x both backgrounds, with flat near-white rows.
  - The Go judge on a 2048 frame went from about 180 ms to about 21 ms (host, best of 5).
  - Under `-race`, a DXF route test went from about 8 s to about 1.8 s.

## Sweep: every "content / ink / visible / empty" decision

| # | Decision point | Decided by | Blind spot before | Now | Can it refuse what J1 sees ink in? |
|---|---|---|---|---|---|
| 1 | helper `nothing_drawable` | frame has data (geometry at any paint) | none for its question | unchanged | no: no record, no paint |
| 2 | helper `zero_extent` | frame has extent | none (extent, not paint) | unchanged | no: a zero-size frame has no letterbox |
| 3 | helper `nothing_visible` | an emitted stroke, fill or text with opacity > 0 | white, faint, zero-area, U+200B all passed | earlier refusal only | no: all opacity 0 leaves the page white |
| 4 | Go `CADSVGFacts.Visible` | effective paint with opacity > 0 and an extent | same as 3; the verifier's five crafted SVGs | earlier refusal plus client/render agreement | no, same reason (crafted differential written, not run) |
| 5 | Go `dxfDrawing` + `dxfInk` (new) | J1 on the 2048 raster on white | - | the one arbiter, svg, pdf and png | it is J1 |
| 6 | PDF judge `InkOps` (`JudgeCADDXFPDF`, `JudgeCADPDF`) | paint operators the graphics state lets show | colour-blind (white counted) | a check of the PDF file after 5 | only a PDF that paints nothing while the raster of the same SVG has ink |
| 7 | drawing judge J1 (`JudgeCADDrawingPNG`) | a pixel off the background | on a transparent background white strokes are ink | unchanged; shares `cadDrawingInk`; 5 runs first | reference |
| 8 | render route J2/J3, sidecar | span and margin at the requested size | not this class | unchanged, after 5 | J2 can refuse a feature below one pixel at a small `size` (documented) |
| 9 | sandbox preflight | 5 inside the sandbox | - | a blank check frame is named "ink check" | - |
| 10 | `cad-pdf-check` / `cad-svg-check` | as 6 / as 4 | colour-blind | unchanged | - |
| 11 | `cad-pixel-judge` | as 7 | - | `--ink` runs `JudgeCADDrawingInk` | - |
| 12 | liveproof `svg_judged`, `pdf_judged` | visible >= 1, `ink_ops` >= 2 | colour-blind | unchanged; a `done` job now implies 5 passed; new DXF_EMPTY and white_one_black rows | - |
| 13 | probe `judge.py` `visible_count` | Python walk | colour-blind | semantics rows prove the helper accepts the six new fixtures, so Go's check is what refuses them | - |
| 14 | probe judge mode (75-08) | J1 to J4 at 256/1280/2048 | none | unchanged | - |
| 15 | probe ink mode (new) | 5 on every committed DXF plus a crafted differential | - | written, not completed (Gates) | - |
| 16 | 3D render J1 (`JudgeCADPNG`) | part pixels | not this class: meshes carry no colour or alpha | unchanged | - |

## Cost of the added raster

- **Go side (measured).** `JudgeCADDrawingInk` on a 2048 frame takes about 21 ms after the row skip (host, best of 5). In the container, `cad-pixel-judge --ink` ran in 0.20 to 0.28 s wall with a 45 MiB peak, process start included.
- **Check raster, small fixtures (observed, not recorded).** These numbers come from the probe ink run that the disk guard stopped, so no evidence file holds them:
  - `rsvg-convert` took 0.10 to 0.20 s and 37 MiB per drawing;
  - rows seen: `alpha_one_fill` ink 166786, `alpha_one_visible` 2764, `degenerate_fill` 0, `faint_only` 0, `far_point` 8, `header_far_extents` 4960.
- **At the DXF ceilings: not re-measured.**
  - The check raster is exactly the 75-05 row step `rsvg_png_opaque`: the same argv, 2048, opaque, in the letterbox of the validated render-only SVG, under the rsvg ruleset.
  - 75-05 measured it at L170031 88.58 s / 1821 MiB, R118 4.65 s / 775 MiB, C 12.90 s, B 25.55 s.
  - The slowest rsvg stage stays the PDF one (93.46 s, 2565 MiB), so `CAD_RSVG_TIMEOUT` 141 s (1.5 x 93.46) still bounds each rsvg stage.
  - The peak stays below the 2842.3 MiB budget, because stages run one at a time.
  - The engine sum now counts both rsvg stages: 30 + 162 + 2 x 141 + 60 = 534 s, at most 886 s. `ValidateCADTimeouts` and the evidence test enforce it.
  - At the cap the check adds about 89 s of wall time to an svg, pdf or png job.
  - The probe's `--ceilings` re-measurement against the 75-05 inputs (sha256 verified) was written and started, but did not run (Gates).

## Tasks and commits

| Task | Commit | What |
|---|---|---|
| 1 | d1ca3a3 | Go: check, judge, row skip, timeout sum, messages, `cad-pixel-judge --ink`, tests; six fixtures; white_one_black outputs; provenance |
| 2 | 1bfdeb3 | probe ink mode, semantics and write-out rows; liveproof rows; PLAN; RED, probe and mutation evidence |
| 3 | 0b5e730 | MEASUREMENTS Empty, ERROR-CODES, docs sync test; u2s not-fixed items marked resolved |

All three commits are SSH-signed (`G`) and carry no Co-Authored-By trailer. `internal/convert/exec.go` is byte-identical to main.

## TDD evidence

- `red-go-compile.txt`: the new tests do not compile on HEAD (`CADDXFInkSize`, `JudgeCADDrawingInk`).
- `red-go-assert.txt`: with stubs, 51 failing tests and subtests across 10 tests. Among them:
  - the ink judge cases;
  - the check never runs on any route;
  - the svg route runs no rsvg;
  - the two-rsvg timeout sum;
  - the preflight ink case;
  - the docs definition.
- `red-go-worker.txt`: the two pinned messages.
- GREEN:
  - `go test ./internal/convert ./internal/worker ./scripts/...` ok (`internal/convert` 398.7 s);
  - `go vet` clean, `gofmt` clean;
  - the docs tests pass on the final docs.
- Under `-race`, the new ink tests take 54.6 s together.
- **Mutation probe** (`mutations-go.txt`, final code): 19 of 20 caught. The survivor is equivalent:
  - G4 changes only the background name `cadReadFrame` validates;
  - G4b, which changes the background the ink judge measures with, is caught.
- The first run found three real gaps, all fixed by new tests:
  - an input-only name collision test could never reach the name list (a non-`.dxf` input is refused by its extension first), so an output at `ink.png` is now tested;
  - a redundant regular-file check was replaced by `OpenJobFile` alone, with a symlink test;
  - the row-skip test now has flat near-white rows.

## Probe and fixture results

- `probe-fixtures.txt`: 54 fixtures generated; 54 identical after `--write`; `git status` shows only the six new files, so the 48 earlier ones are byte-identical. All six new ones are strict-clean.
- `probe-helper.txt`, semantics 32 rows, 0 fail. The six new fixtures are `verdict=ok`, so the helper hands them to Go:
  - white_geometry: visible 8, white paints 8;
  - white_text: 2 white texts;
  - white_one_black: 11 visible, one black stroke;
  - degenerate_fill: 2 fills, no stroke;
  - faint_only: 3 paints at 0.004;
  - zwsp_text: one U+200B text.
- `probe-helper.txt`, write-out 53 rows, 0 fail. Every earlier SVG, texts.json, draft and PNG is byte-identical. The ten earlier PDFs differed only in `/CreationDate`, so the committed bytes were kept (`writeout-pdf-dates.txt`).

## Gates

| Gate | Result | Why |
|---|---|---|
| Go tests, vet, gofmt, mutation probe | PASS (above) | host only |
| `cad-dxf-probe.sh fixtures --write`, `helper --group semantics --write-out` | PASS | ran while the VM had 6.5 GiB |
| `cad-dxf-probe.sh ink --ceilings` | **NOT RUN** (3 attempts) | 1st: stopped by me after `coord_1e300` showed 0 ink; it never reaches the helper in production (guard `coordinate_out_of_range`), so the mode now runs the production guard first. 2nd and 3rd: `STOP,reason=disk_below_threshold,free_gib=5` partway through the corpus |
| `cad-liveproof.sh` from a fresh clone (bundle, override 127.0.0.1:18090) | **NOT RUN** | VM free 0.90 to 2.34 GiB from 02:58Z on, below the 6 GiB floor; runner prepared |
| `ci-local.sh helm gate race` | **NOT RUN** | host free 4.4 to 6.1 GiB. A race build of the repo needs about 2 to 3 GiB of Go cache, which would push the VM, about 4 GiB below host free, to zero and risk other projects' containers |
| `sandbox-liveproof.sh cad` | not needed | no new stage, path, import or argv. The check is the rsvg stage with the PNG route's argv and ruleset, already run by the preflight under Landlock. The preflight now also runs the check inside the sandbox |

Disk readings: host free fell from 9.8 to 4.35 GiB and the VM from 6.5 to 0.90 GiB, mostly while this task was idle. The consumers were another project's `go test -race ./...` (`/Users/apaderin/work/gsh-identity-service`, `mise run test`, Go 1.27.2) refilling the shared Go build cache, and the OrbStack image itself. Someone also pruned `alpine:latest` three times, and four images of the identity stand.

To finish:
1. Rerun `scripts/cad-dxf-probe.sh ink --ceilings --out <evidence>/probe-ink.txt` when the VM has 6 GiB.
2. Then run the prepared fresh-clone liveproof: clone a bundle of `gsd/phase-75-dxf-2d-pipeline` into a dir named `octoconv`, add an untracked override with `ports: !override ["127.0.0.1:18090:8090"]` on `api`, and set `CAD_LIVEPROOF_API_BASE=http://127.0.0.1:18090`.
3. Then `scripts/ci-local.sh helm gate race` from the same clone.

New live rows to expect:
- DXF_EMPTY for white_geometry, white_text, degenerate_fill, faint_only and zwsp_text on svg, pdf and png, and for white_geometry and white_text as transparent PNGs (17 new rows, 23 DXF_EMPTY rows in all);
- DXF_SVG, DXF_PDF and DXF_RENDER for white_one_black.

## Deviations from Plan

1. **[Rule 1] The probe ink mode runs the production guard.** Found when `coord_1e300` showed 0 ink in the first run. The guard refuses it before the helper (`coordinate_out_of_range`), so it never converts. Without the guard, the corpus sweep would have reported a drawing production never draws.
2. **[Rule 1] Row skip in `cadMeasure`.** It was not in the plan. The extra 2048 decode and measure made every DXF route test about 8 s under `-race`, putting the 20 m race tier at risk. The skip is exact (differential test), and production gains too (about 180 ms to 21 ms per check).
3. **[Rule 2] Output-name and symlink tests.** The mutation probe showed that the name collision list and the frame's job-file guard were untested. The redundant `cadDXFRegular` before `OpenJobFile` was removed.
4. **[Rule 1] Render route order.** The draft is now parsed after the check, so an invalid draft costs one raster. The test that said "no rsvg before the measurements" now requires that only the ink check ran.
5. **Cleanup of regenerable octoconv caches for disk headroom** (see Cleanup).
6. **Gates not run** (above). `status: partial`.

## Not covered (honest list)

- Content below the page threshold at 2048 px counts as nothing, although an SVG or PDF viewer could zoom in on it. An example is a text a hundredth of a pixel high next to a fully transparent line that sets a huge frame. This is in MEASUREMENTS. `far_point`, at 8 ink pixels in the check frame, is the closest committed drawing to that edge.
- Near-white paint (every channel 250 or more, such as `#fafafa`) counts as no ink, the same as on the PNG route.
- `InkOps` is still colour-blind. That is harmless now, because it is no longer the emptiness arbiter.
- A crafted-SVG differential that proves `Visible == 0` implies 0 ink is written (36 cases, 20 of them with Visible 0) but not run. The argument that alpha 0 everywhere cannot change a pixel stands, but it is not evidence.
- The u2s round-cap/moveto note stays as it was (a `line` of zero length with a round cap is correctly visible).

## Cleanup

- No compose stack was started. `docker compose -p octoconv stop` ran at the end; every octoconv container was already exited from earlier tasks. No volume was touched (never `down -v`).
- Build cache: the 15 records this run created (probe engine builds: context and dockerfile sources) were pruned by ID. 0 baseline records were removed and 0 new records remain.
- Images: the probe removed its `engine75-*` tags. octoconv-cad-worker/api/av-worker are unchanged. `alpine:latest` was re-pulled after an outside prune, as at the start.
- Go build cache: `go clean -cache` several times. One run overlapped another project's Go build (`unlinkat ... directory not empty`). It is a regenerable cache, but cleaning it while another build is running can disturb that build.
- Removed regenerable octoconv corpus caches (50 files, list in the run log):
  - `~/.cache/octoconv/cad-corpus/scaled`: STEP and STL copies (1.7 GiB), the 200 MiB DXF replicate and the four `dxf-guard-stress-*-118MiB`;
  - `spike73` (267 MiB).
  - `scripts/cad-corpus-gen.py --scaled-from`, `cad-dxf-bounds.sh` and `cad-spike.sh` regenerate them on demand. The 75-05 ceiling inputs (L170031, R118, C, B) and the 50 MiB replicate were kept.
- A brief `/tmp/caddxf.go.keep` from a mutation check was restored and deleted.
- No fresh clone was made (the liveproof never started). `git worktree list` shows only the main checkout. `.planning/config.json`, `.vscode/`, `octoconv.code-workspace`, STATE.md and ROADMAP.md were not touched.
- The 261012-u2s SUMMARY was edited, which stales the phase-75 verification fingerprint if it covers that file.

## Self-Check

**PASSED for what was run; the live gates are NOT RUN (status partial).**
- Commits d1ca3a3, 1bfdeb3 and 0b5e730 are ancestors of HEAD and SSH-signed (`G`). `git rev-list --count bc6710d..HEAD` = 3 before this summary's commit.
- The new test files, the six fixtures, the white_one_black outputs, the PLAN and the evidence files exist.
- `internal/convert/exec.go` is byte-identical to main.
