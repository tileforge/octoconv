---
phase: quick-261014-0lo
plan: 01
subsystem: cad-render
tags: [cad, render, png, pixel-judge, background, transparent, phase-76-finding]
status: complete
requires: [phase-74 3D pixel judge, phase-75 drawing judge, quick-261013-0og white-page definition of empty]
provides: [one part-pixel definition on both backgrounds, J1-J4 shared, alpha-only route checks, cadJudgeAlpha, RingNonTransparent fact, five committed render pairs, liveproof background_transparent_sparse row]
affects: [internal/convert/cadpixel.go, scripts/cmd/cad-pixel-judge, scripts/cad-liveproof.sh, docs/MEASUREMENTS.md, docs/ERROR-CODES.md]
tech-stack:
  added: []
  patterns: [a pixel is judged as it shows on a white page (straight alpha composited on white), whatever the requested background; background-specific checks only read the alpha channel]
key-files:
  created:
    - internal/convert/cadpixelbg_test.go
    - internal/convert/testdata/cad/render-bg/{nauo4_iso_1280,linearslide_iso_256,w15_iso_256,degenerate_plate_front_1280,degenerate_rod_iso_256}_{opaque,transparent}.png
  modified:
    - internal/convert/cadpixel.go
    - internal/convert/cadpixel_test.go
    - internal/convert/cadpixelmeasure_test.go
    - internal/convert/cad_docs_sync_test.go
    - internal/convert/testdata/cad/PROVENANCE.md
    - scripts/cmd/cad-pixel-judge/main.go
    - scripts/cad-liveproof.sh
    - docs/MEASUREMENTS.md
    - docs/ERROR-CODES.md
decisions:
  - "A part pixel is a pixel that, composited on white with its straight alpha, has a channel below 250; dark when all three are below 80. One definition for both backgrounds, in the 3D judge and in the DXF drawing judge"
  - "J1-J4 (3D) and J1-J3 (DXF drawing) run first and identically; the route checks after them read the alpha channel only: opaque has no alpha-0 pixel; transparent carries alpha and every pixel of the outermost one-pixel ring is alpha 0"
  - "cadTransparentMax removed: J1 and J4 already decide whether something is drawn, and the 95% cap was a second, stricter ink floor on the transparent route only"
  - "cadTransparentMin redefined as the ring check (T2): it was the only test that a transparent PNG's background is transparent; on the ring it cannot disagree with the opaque verdict, because J3 has already kept every part pixel 3% of the size away from the edges"
  - "The transparent route's 'no pixel is opaque' rule removed: a part drawn in partial alpha that darkens the white page is visible content"
  - "The residual difference between the two renders of one mesh is the renderer's FXAA fringe (RGB written into alpha-0 pixels); it stays within the pre-registered tolerance and is documented"
metrics:
  completed: 2026-10-14
  duration: about 1h15m (21:36 to 22:50 UTC)
actuals:
  tasks: 2
  commits: 3
plan_head_before: 8b12a8062709829bee76426b57b8c3a725462523
plan_head_after: d8ef3838302bf10dac8f42ac56960158916e0def
---

# Quick 261014-0lo: one render verdict on both backgrounds Summary

**Complete.** The 3D render judge now gives the same verdict on the opaque and the transparent PNG of one
model. The refusal phase 76-02 hit on M2 is reproduced through the service at HEAD and gone after the fix.

- Through the compose stack at d2f52ef (judge unchanged), step-nauo n=4 rendered transparent: `failed`,
  `cad_empty_result`, "the rendered preview shows no recognisable part"; the opaque render of the same upload:
  `done` (`evidence/repro-head.txt`).
- At d8ef383, `cad-liveproof.sh` from a fresh clone: **ALL PASS**, with the new row
  `background_transparent_sparse` (the same n=4 upload, transparent: `done`, blob, frac 0.0423,
  frac_transparent 0.9577, ring 0; opaque twin blob, same box 1117x673, margin and dark count, part-pixel gap
  0.0141).
- `ci-local.sh helm gate race` from the same clone: PASS (gate `internal/convert` 364.8 s, race 596.1 s, no
  `DATA RACE`).

## What was wrong

The two backgrounds were judged with two definitions and two sets of rules.

- **Part pixel.** Opaque: a channel below 250. Transparent: any alpha above 0. A lit face can reach 0.998 of
  white (0.8 x (0.45 + 0.55 x 1.45)), and a faint anti-aliased pixel composites to near white, so the two
  sets differed even before any threshold.
- **Rules.** The transparent route added a band of 5%..95% transparent pixels before J1 to J4. Its upper
  bound is an ink floor of 5% of the frame, while the opaque route's blob floor is 2% and a thin part needs
  no fraction at all. So every sparse assembly (4-5% ink) and every thin part was refused on transparent and
  accepted on opaque. The 63-pair corpus shows the class, not only M2: 10 pairs flipped from refused to
  accepted (step-nauo n=4/16/64 at 1280 and 2048, the W15 slice at 256/1280/2048, the degenerate plate edge
  on), and 3 changed only their reason (degenerate_rod: "transparent fraction" before, J4 now, as on opaque).
- The DXF drawing judge had the same split: white strokes were ink on a transparent frame and not on an
  opaque one, so J2/J3 saw a different drawing (the committed `wide` and `text_alpha_faint` twins had
  different dark counts and counts at HEAD; now identical).

## Decision on cadTransparentMin / cadTransparentMax

| Old rule | Its job | Done already by | Now |
|---|---|---|---|
| `cadTransparentMax` 0.95 (at least 5% not transparent) | something is drawn | J1 (a part pixel exists) and J4 (blob frac >= 0.02, or thin with dark pixels along the long side), on the same pixel set on both routes | **removed**; it was a stricter second floor on one route, the M2 defect |
| `cadTransparentMin` 0.05 (at least 5% transparent) | the transparent PNG's background is transparent | nothing: an alpha-254 white background composites to white and holds no part pixel, so J1-J4 pass | **redefined as T2**: every pixel of the outermost one-pixel ring is alpha 0. It reads only the alpha channel, runs after the shared J3 (which has already put every part pixel at least 3% of the size from each edge; the renderer frames with 6%), so on a mesh both routes accept it cannot refuse the transparent one unless its background is not transparent |
| transparent `opaquePx == 0` ("no pixel is opaque") | something is drawn | J1 on composited pixels | **removed**: a part in partial alpha that darkens a white page is content on both routes |
| transparent HasAlpha | the PNG carries alpha | - | kept (T1), after J1-J4 |
| opaque `FracTransparent > 0` | no hole in an opaque PNG | - | kept, after J1-J4 |

Order: J1-J4 first, then the route's alpha checks, so a frame that fails a shared rule fails it with the same
message on both routes (the full-coverage scene is J3 on both; at HEAD it was J3 on opaque and "carries no
alpha" on transparent).

## Part-pixel definition and the tolerance

`cadMeasure` no longer takes the background. A pixel with alpha 0 is never a part pixel; any other pixel is
composited on white, `c' = (c*a + 255*(255-a) + 127) / 255`, then the existing channel thresholds apply. For an
alpha-255 pixel this is the old opaque rule unchanged: on all 63 opaque frames of the corpus the new
`GOJUDGE` line equals HEAD's apart from the new `ring_nontransparent` field.

Pre-registered in the PLAN (d2f52ef) before the corpus run; result in `evidence/pairs.txt`
(`PAIRS_SUMMARY,pairs=63,outside_tolerance=0,result=ok`):

| Rule | Bound | Measured over 63 pairs |
|---|---|---|
| (a) same verdict | identical | 63/63 (55 blob, 4 thin, 4 J4 on both) |
| (b) box and margin | at most 1 px | 0 px on all 63 |
| (c) dark count | identical | identical on all 63 |
| (d) transparent part set within the opaque one | subset | 0 pixels outside on all 63 |
| (e) blob count gap | at most 0.10 | at most 0.0386 (n64 at 1280) |
| thin count gap | reported only | 0.1398 (W15 at 256), 0.0788 (1280), 0.0482 (2048), 0 (plate) |

Where the residual comes from: the transparent render's alpha is binary, and the opaque image equals the
transparent image's RGB plane exactly. `renderer.UseFXAAOn()` blends edge colour into the RGB of the
neighbouring background pixels and leaves their alpha at 0. An opaque PNG shows that fringe; a transparent
one does not. So the transparent part set is the opaque set minus the fringe, never more, with the same
extent and dark pixels. The renderer is unchanged; MEASUREMENTS states the residual.

## Tests (pure Go, D-19)

`internal/convert/cadpixelbg_test.go`, about 0.4 s in the gate tier and 12 s under `-race`:

- `TestJudgeCADPNGGivesOneVerdictOnBothBackgrounds`: each scene drawn once on a transparent page (both a
  white and a black alpha-0 page), its opaque twin composited on white; same error text and same J facts
  required, plus the expected outcome. Scenes: sparse assembly at 1280 and 256 (4.5% ink, over 95%
  transparent: the M2 shape, pinned by `TestJudgeCADPNGSparseSceneIsTheM2Shape`), dense part, thin band,
  edge-on sliver, empty frame (J1), full coverage (J3), dot (J2), anti-aliased rim at alpha 160/64/8/5/2/1,
  near-white lit faces, an FXAA fringe, a translucent part.
- `TestJudgeCADPNGRealRenderPairsAgreeWithinTheTolerance`: the five committed pairs from the shipped renderer
  (sparse n=4, dense LinearSlide, thin W15, thin plate, J4 rod), rules (a)-(e).
- Route alpha checks: alpha-254 background, one opaque corner, one alpha-1 pixel on each of the four edges,
  alpha-1 pixels one pixel inside (accepted), no alpha channel, an alpha-0 hole on opaque; the drawing judge
  on the same rules.
- `TestCADPartPixelIsDecidedOnAWhitePage`: the composite boundaries (black alpha 5/6 for part, 175/176 for
  dark, 250 vs 249, alpha-0 grey, alpha-254 white).
- `TestJudgeCADDrawingPNGGivesOneVerdictOnBothBackgrounds`, and the helper-frame twins now must have identical
  J facts (was: boxes within 2 px).
- `cadpixelmeasure_test.go`: the per-pixel reference now composites with an independent float formula and
  counts the ring; the row-skip differential still covers 300 random frames x NRGBA/RGBA/Gray x sub-images.
- `TestCADDocsStateOneVerdictOnBothBackgrounds` pins the docs.

RED (`evidence/red-compile.txt`, `red-assert.txt`, `red-docs.txt`): the new tests do not compile on HEAD; with
only the new field added (never computed), 45 failing tests and subtests across all 11 new or changed tests,
each for the reason it targets (for example the sparse twins: "opaque accepted, transparent fraction 0.9553 is
outside 0.05..0.95"); the docs test fails on HEAD's docs.

Mutation probe (`evidence/mutations.txt`, final tests): **17 of 17 caught**, among them no composite, rounding,
composite only below alpha 128, the alpha>0 rule back, the ring without its columns or bottom row, both fast-path
ring counts, each alpha check dropped, the alpha checks before J1, the old 5%..95% band restored, dark on the
straight channel, and a fast path that skips all-zero rows.

## Sweep: every place a render or pixel judge branches on the background

| # | Place | Branch before | Now | Verdict depends on background? |
|---|---|---|---|---|
| 1 | `cadMeasure` (cadpixel.go) | part = channel < 250 (opaque) or alpha > 0 (transparent); white-row skip on opaque only | one composite definition; the white-row skip applies to any frame (an all-0xff row is opaque white) | no |
| 2 | `cadJudgeFacts` (3D) | 5%..95% band, no-opaque-pixel rule, alpha checks first | J1-J4 shared, then `cadJudgeAlpha` | alpha only |
| 3 | `JudgeCADDrawingPNG` (DXF PNG) | HasAlpha and >= 5% transparent first, alpha>0 ink | J1-J3 shared on the white page, then `cadJudgeAlpha` | alpha only |
| 4 | `JudgeCADDrawingInk` (DXF emptiness) | always opaque white page | unchanged (it measures with the same `cadMeasure`) | n/a, one page |
| 5 | `cadReadFrame` | validates the background name | unchanged | argument check |
| 6 | `renderStage` (cadrender.go) | passes `--background`, requires the renderer to echo it | unchanged | renderer contract |
| 7 | `cadDXFFrameArgs` (caddxf.go) | `-b white` on opaque | unchanged; it makes the frame, the judge then reads it on white | renderer side |
| 8 | `cad-render` helper | alpha 0 background, RGBA grab on transparent; FXAA on both | unchanged; FXAA writes RGB into alpha-0 pixels, the measured residual | renderer side |
| 9 | sandbox preflight (cadsandbox.go) | judges both backgrounds of its fixture; a blank transparent frame must be refused | unchanged; the blank frame is now J1 (was the band); the worker logged `cad sandbox proven` in the live run | no |
| 10 | `cad-pixel-judge` CLI | passes `--background` | adds `ring_nontransparent` | no |
| 11 | liveproof `background_transparent` row | transparent judge ok, alpha=1, the opaque judge must refuse it | unchanged and passing (the opaque judge still refuses alpha-0 pixels) | alpha only |
| 12 | liveproof `background_transparent_sparse` (new) | - | n=4 transparent done and blob, opaque twin within the tolerance | no |
| 13 | `cad-render-probe.sh judge` and `scripts/cad-measure/pixelcheck.py` | the 71 Python reference keeps its own 0.05..0.95 band and the probe ANDs it into its verdict on transparent frames | **not changed, not run**: the 71 image it needs (`octoconv-cad-measure:ocp7.9.3.1.1-pruned-…`) is not on this machine. Its corpus renders only the five identity parts transparent, at 14-57% ink, so it would not diverge; a sparse or thin transparent frame would show there as an UNEXPLAINED divergence. Open follow-up |
| 14 | `cad-dxf-probe.sh` GOJUDGE rows | records the judge's facts per background | not changed, not run; the committed DXF twins it produced are judged identically now (test) | no |
| 15 | 76-02 bounds harness (b76) | judges the pixel stage per background with `cad-pixel-judge` | not changed; its M2 transparent row should now pass on a re-run (the 76-02 evidence file was not touched) | - |
| 16 | `internal/worker/descriptors.go` | `ErrCADRenderRejected` -> `cad_empty_result` "the rendered preview shows no recognisable part" | unchanged; an alpha-check failure (a renderer fault) also lands there with that message, as before. Not this class; noted | - |
| 17 | docs/MEASUREMENTS.md, docs/ERROR-CODES.md | no statement on the background; nothing on the band | "PNG check" paragraph and the `cad_empty_result` row: white page whatever the `background`, alpha-only route checks, the FXAA fringe | - |
| 18 | `cadopts.go`, openapi | background option values and default | unchanged | - |

## Gates

| Gate | Result | Evidence |
|---|---|---|
| `go test ./...` (GOTOOLCHAIN=go1.26.4, py314 first, 20m) at 056a8d2's tree | PASS, 31 packages ok (`internal/convert` 387.3 s) | `go-test-all.txt` |
| docs tests after the docs change | PASS | - |
| `go vet ./...`, `gofmt -l` | clean | - |
| HEAD reproduction through the service (d2f52ef, fresh clone, image `e2d4105a85da`) | transparent `failed` `cad_empty_result`, opaque `done` | `repro-head.txt` |
| `cad-liveproof.sh` from a fresh clone at d8ef383 (bundle clone, `octoconv-bundle.ttb` copied, untracked override `127.0.0.1:18090:8090`, `CAD_LIVEPROOF_API_BASE`, `tracked_dirt=0`; images cad-worker `3546326d6a00`, api `bf795dfe2927`) | **ALL PASS**: `views=7` (6 + the sparse row), every other counter as in 261013-0og (`dxf_svg=9,dxf_pdf=8,dxf_render=10,dxf_empty=23,dxf_retry=2`, `bomb=12`, …) | `cad-liveproof.txt`, `cad-liveproof.transcript.log` |
| `ci-local.sh helm gate race` from the same clone (transcript names d8ef383; untracked only the override and `lp/`) | **PASS**: helm, gate (`internal/convert` 364.8 s against 10 m, under the 540 s D-19 line), race (596.1 s against 20 m, 0 `DATA RACE`; 611.1 s in 261013-0og), `ALL REQUESTED TIERS PASSED` | `ci-local-d8ef383.log` |
| `internal/convert/exec.go` | byte-identical to main | - |

## Deviations from Plan

1. **[Rule 2] The DXF drawing judge got the same fix** (planned as part of the class in decision 2; recorded
   because it changes a DXF route): white or alpha-1 strokes on a transparent DXF frame no longer count as ink
   for J2/J3, as on opaque and as the phase-75 ink check already decided.
2. **[Rule 1] Test cost.** The first version of the synthetic scenes ran at 1280 px and added about 108 s
   under `-race`; the scenes were redrawn at 256 px (one sparse scene stays at 1280) and the real-pair subset
   check reads the decoded pixel slices directly: about 12 s under `-race` now. RED and the mutation probe were
   re-run on the final tests.
3. **The W15 slice** was not in the cache; it was regenerated with `cad-corpus-gen.py --kind stl-ascii`
   and matched the 71 sha256 (`77492adb…`).

## Known residuals

- The FXAA fringe: a transparent PNG has up to 4% fewer part pixels than the opaque PNG of a filled model and
  14% of a thin part at 256 px, with the same extent. A model sitting exactly on the blob floor (frac 0.02) or
  the span rule (count >= long side) could still fall on different sides of it; no corpus pair is near
  either. Removing it means changing the renderer (FXAA alpha), not the judge.
- Probe 13 and the 71 pixelcheck band (sweep row 13).

## Cleanup

- `docker compose -p octoconv stop` after the HEAD reproduction and again after the liveproof (which had
  already stopped what it started). Never `down -v`; no volume touched; no other project's container, image or
  volume touched; OrbStack not restarted; `go clean -cache` not run.
- Build cache: the run created 8 records (two `COPY . .`, two `go build` mounts each for cad-worker and api,
  one set per build). The 6 unshared ones were pruned by ID; the 2 shared `COPY --from=build` records that back
  the kept `octoconv-api:latest` and `octoconv-cad-worker:latest` stay. 0 baseline records removed.
- Images: `octoconv-api:latest` and `octoconv-cad-worker:latest` are now the d8ef383 builds (`bf795dfe2927`,
  `3546326d6a00`). The previous builds `7009109e9f88` and `e2d4105a85da` are dangling and were left, as were the
  older dangling `f97a6f58af0f`, `c0d36cf33909` and `8c32a7e0f535` (not from this run).
- The fresh clones (HEAD reproduction, liveproof) and their bundles were removed; the scratch meshes and frames
  too. `git worktree list` shows only the main checkout.
- Not touched: STATE.md, ROADMAP.md, `.planning/config.json`, `.vscode/`, `octoconv.code-workspace`, the 76-02
  evidence file. `internal/convert/exec.go` byte-identical to main.
- Disk at the end: OrbStack VM 27 GiB free (overlay), host 32 GiB free.

## Self-Check

**PASSED.**
- Commits d2f52ef, 056a8d2 and d8ef383 are ancestors of HEAD and SSH-signed (`G`), no Co-Authored-By trailer;
  `git rev-list --count 8b12a80..HEAD` = 3 before this summary's commit.
- Files exist: `internal/convert/cadpixelbg_test.go`, the ten PNGs under `testdata/cad/render-bg/`, the PLAN,
  and the evidence files (`red-compile.txt`, `red-assert.txt`, `red-docs.txt`, `pairs.txt`, `mutations.txt`,
  `repro-head.txt`, `cad-liveproof.txt`, `cad-liveproof.transcript.log`, `ci-local-d8ef383.log`,
  `go-test-all.txt`, `harness/`).
- No code comments were added: no added `//` line in any Go file, and the only added `#` lines in shell are the
  shebangs of the evidence harness scripts.
