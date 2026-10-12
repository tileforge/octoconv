---
phase: quick-261012-o2d
plan: 01
subsystem: cad-dxf
tags: [dxf, svg, transparency, pdf-judge, codex-pr36]
status: complete
requires: [phase-75 dxf pipeline]
provides: [text fill-opacity, invisible-text exclusion in the PDF judge, text_alpha fixtures]
affects: [cmd/cad-worker/helper/cad-dxf, internal/convert caddxf*, scripts/cad-liveproof.sh, docs/MEASUREMENTS.md]
tech-stack:
  added: []
  patterns: [effective fill opacity computed by the Go SVG validator and cross-checked against texts.json]
key-files:
  created:
    - internal/convert/testdata/cad/dxf/text_alpha.dxf
    - internal/convert/testdata/cad/dxf/text_alpha_faint.dxf
    - internal/convert/testdata/cad/dxf-out/text_alpha.{svg,render.svg,texts.json,pdf}
    - internal/convert/testdata/cad/dxf-out/text_alpha_faint.{svg,render.svg,texts.json,pdf}
    - internal/convert/testdata/cad/dxf-out/text_alpha_faint_1280_{opaque,transparent}.png
  modified:
    - cmd/cad-worker/helper/cad-dxf
    - internal/convert/caddxfsvg.go
    - internal/convert/caddxf.go
    - internal/convert/caddxfpdf.go
    - internal/convert/cadsandbox.go
    - scripts/cmd/cad-pdf-check/main.go
    - scripts/cad-corpus-gen.py
    - scripts/cad-dxf-probe.sh
    - scripts/cad-liveproof.sh
    - docs/MEASUREMENTS.md
    - internal/convert/testdata/cad/PROVENANCE.md
    - internal/convert/testdata/cad/dxf-out/frames.tsv
decisions:
  - "DXF transparency is paint, not geometry: bbox and frame count every drawn primitive and text at any opacity, as ezdxf's recorder bbox already did for shapes"
  - "A text is invisible iff its alpha is 00; texts.json carries invisible:true only then; the Go validator derives effective text opacity (own, inherited from g, fill=none is 0, tspan can only raise it) and the cross-check and PDF judge reject any disagreement"
  - "Complex MTEXT runs that set their own colour inherit the MTEXT's alpha in cad-dxf (ezdxf drops it); fixed rather than documented as a limit"
metrics:
  completed: 2026-10-12
  duration: about 2h
actuals:
  tokens: 19800
  tasks: 3
  commits: 3
plan_head_before: b71eac59df83e2ff880075f900678319679ca56d
plan_head_after: b48e93be64d3f79a9c0c79c31f5b515e3a153207
---

# Quick 261012-o2d: DXF text transparency (codex PR #36 P2) Summary

`cad-dxf` now writes `fill-opacity` on transparent TEXT and MTEXT, using the same `alpha_to_opacity` call that ezdxf's `resolve_color` makes for shapes. ByLayer, ByBlock and inline-coloured complex MTEXT runs are covered too. The Go validator, cross-check, PDF judge and preflight treat fully transparent text as invisible and keep half-transparent text required. This is proven live: ALL PASS from a fresh clone.

## Per-site sweep: every colour cad-dxf writes into SVG

| # | Site | Entities that reach it | Alpha before | After | Proof |
|---|---|---|---|---|---|
| 1 | `NoCssRenderBackend.add_strokes` (stroke + `stroke-opacity` from `resolve_color`) | LINE, ARC, CIRCLE, ELLIPSE, (LW)POLYLINE, SPLINE, POINT (`draw_point`), hatch pattern lines (`draw_solid_lines`), dimension and leader lines | carried | unchanged | live DXF_SVG text_alpha: `stroke_opacity_half=2` (LINE, XLINE) |
| 2 | `NoCssRenderBackend.add_filling` (fill + `fill-opacity`) | solid HATCH, SOLID, TRACE, 3DFACE, filled polylines, MTEXT background mask | carried | unchanged | live: `fill_opacity_half=1` (HATCH) |
| 3 | infinite lines: `fe.pipeline.draw_line` → `get_backend_properties` → `add_strokes` | XLINE, RAY (clipped to the frame) | carried | unchanged | live: the XLINE is one of the two 0.498 strokes |
| 4 | `<text>` emission in `draw()` | every TEXT, MTEXT, ATTRIB, dimension and MLEADER text | **dropped** (`fill = color[:7]`) | `fill-opacity="%.3f"` when below 1, the shape format; `invisible` in texts.json at alpha 00 | fixture + live: 8 texts at 0.498, 4 at 0.000, 2 opaque |
| 5 | complex MTEXT runs (`mtext_complex.new_text_properties` replaces the colour with `#RRGGBB` before any SVG write) | runs with `\C`/`\c`, and their decorations and background | **dropped** upstream in ezdxf | `TextPipeline.get_backend_properties` appends the MTEXT's alpha (`run_alpha`, set in `DXFFrontend.draw_complex_mtext`) to any 7-char run colour | `evidence/helper-red-green.txt`: `INLINE`/`RED` absent → 0.498 |
| 6 | background `rect` (`set_background`, `BackgroundPolicy.OFF`) | page | carried (`fill-opacity="0.0"`) | unchanged | every fixture SVG |
| 7 | ByLayer and ByBlock transparency | resolved by ezdxf's `RenderContext` into `properties.color` before sites 1 to 5 | carried | unchanged | live: `LAYER TEXT`, `BLOCK TEXT` at 0.498 |
| 8 | `TEXT_AS_OUTLINE` path (stock `SVGRenderBackend`, CSS classes) | none (the constant is `False`) | stock backend puts the opacity in its class | n/a | dead path |
| 9 | `draw_image` | none (`ImagePolicy.IGNORE`; ezdxf's `draw_image` is a no-op) | n/a | n/a | — |

## Rules (stated before code, kept)

- **Bbox and frame:** geometry, not paint. Every drawn primitive counts at any opacity, text and shapes alike. A fully transparent shape was already in `drawn_box` through ezdxf's recorder bbox, and the text glyph box already counted. Nothing changed. Live: the text_alpha_faint bbox reaches y = -15.19, which is the fully transparent `GONE`.
- **Invisible:** alpha `00` gives `fill-opacity="0.000"`. Only then does texts.json get `"invisible":true`, so opaque output stays byte-identical to before. Every pre-existing dxf-out SVG, texts.json and PNG came out identical.
- **PDF judge:** an invisible text is excluded from the in-order expectation and counted in `InvisibleText`/`InvisibleRunes`, beside `BelowRenderFloor`. A flag that disagrees with the SVG is a rejection. Half-transparent text stays required (`TestJudgeCADDXFPDFRequiresAHalfTransparentText`). Cairo drops alpha-0 text from the PDF and keeps half-transparent text behind an ExtGState `gs`, which the judge already admits.
- **Pixel judge:** faint ink is ink. On the opaque background any channel below 250 counts, so 50% black on white (about 128) counts and text at about 2% opacity or less does not. On a transparent background any alpha above 0 counts. Live: a 1280 frame whose only ink is 50% text passes both opaque and transparent (`count=49124` / `49377`).
- **All-transparent drawing:** it has a frame but no ink. The SVG converts; the PDF is refused ("empty page") and the PNG is refused (J1 no ink), and both map to `cad_empty_result`. Evidence: `evidence/helper-red-green.txt` (an ad hoc DXF, not a fixture).
- **Allowlist:** `fill-opacity` on `text` and `tspan` was already admitted with `cadSVGUnit`, a strict number in [0,1]. It is now pinned by tests: 9 accepts, and 20 rejects (out of range, sign, whitespace, `%`, `calc()`, `var()`, `NaN`, `inf`, `1e400`, hex, comma, units, two numbers).

## Tasks and commits

| Task | Commit | What |
|---|---|---|
| 1+2 | 82ab86b | helper, Go validator/cross-check/judge/preflight/tool, tests, generator, probe, fixtures, dxf-out outputs, provenance |
| 3 | a76a30c | cad-liveproof rows: DXF_SVG text_alpha, DXF_PDF text_alpha and text_alpha_faint, DXF_RENDER text_alpha 1280 and text_alpha_faint 1280 opaque/transparent |
| 3 | b48e93b | docs/MEASUREMENTS.md: transparency rule |

## TDD evidence

- RED on the HEAD Go code, run in a detached scratch worktree with the new tests and fixtures copied in:
  - `evidence/red-go-compile.txt`: the new API is missing.
  - `evidence/red-go-assert.txt`: the old-API tests fail on the real outputs. `TestCADDXFConvertSVGReturnsOnlyGoBytes/text_alpha*` and `...PDFRunsRsvg.../text_alpha*` fail with `unknown field "invisible"`. The frames test failed too, which also exposed a name-splitting bug in `TestCADDXFRenderLetterboxes…`, fixed in the same commit.
  - The HEAD PDF judge on flagless texts gives `missing text: 21 of 75` (`evidence/helper-red-green.txt`).
- RED for the helper: `evidence/helper-red-green.txt`. The HEAD helper writes no text opacity at all. Without the run-alpha fix the inline-coloured MTEXT run is opaque.
- GREEN: `go test ./...` (all 31 packages ok, py314 on PATH, GOTOOLCHAIN=go1.26.4, 10m timeout); `go vet ./...` clean; gofmt clean; `internal/convert/exec.go` byte-identical to main.
- Mutation probe (`evidence/mutations.txt`). Each of 8 removed checks fails a test:
  - cross-check flag, and client/render agreement;
  - tspan max, `g` inheritance, `fill="none"`;
  - judge flag check, judge exclusion;
  - the `null` flag rejection.

## Live and gate results

| Gate | Result | Evidence |
|---|---|---|
| `scripts/cad-dxf-probe.sh fixtures --write` | ok: 43 fixtures, 43 identical, 0 strict expectations failed (text_alpha and text_alpha_faint strict-clean) | `evidence/probe-fixtures.txt` |
| `scripts/cad-dxf-probe.sh helper --group semantics --write-out` | semantics 22 rows 0 fail; writeout 41 rows 0 fail | `evidence/probe-helper.txt` |
| `scripts/cad-liveproof.sh` from a fresh clone at b48e93b, bundle copied, `tracked_dirt=0` | **ALL PASS**: dxf_svg=6, dxf_pdf=5, dxf_render=7. Every other counter equals 261012-gos's | `evidence/cad-liveproof.txt`, `.transcript.log` |
| `scripts/ci-local.sh helm gate race` from the same clone (commit b48e93b, no TRACKED CHANGES marker) | **ALL REQUESTED TIERS PASSED**: helm PASS (local helm v4.3.0 vs pinned v4.2.3, as the transcript warns), gate PASS (`internal/convert` 361.9s), race PASS (`internal/convert` 561.7s) | `evidence/ci-local-b48e93b-20261012T151300Z.log` |
| `scripts/sandbox-liveproof.sh cad` | **not run, not needed**: the helper argv is unchanged, the image differs only in the helper's Python body, and there are no new imports (`svg` and `RenderPipeline2d` were already imported) and no new file access. cad-liveproof's SANDBOX row still proved the confined worker (landlock line, proven line naming cad-dxf and rsvg-convert) | — |

## Deviations from Plan

1. **[Rule 2 - Missing critical functionality] Complex MTEXT runs drop alpha upstream.** Found during the sweep (Task 1). ezdxf's `mtext_complex.new_text_properties` replaces a run's colour with a 7-char `#RRGGBB`, so a transparent MTEXT run with an inline colour came out opaque. I first planned to document this as a known limit. Instead it is fixed in `cad-dxf` with `run_alpha`, and the `text_alpha` fixture gained `HALF\PSPLIT`, `\C1;INLINE RED` and `CLEAR\PSPLIT`. Commit 82ab86b.
2. **[Rule 1 - Bug] `TestCADDXFRenderLetterboxesTheValidatedSVGWithTheProbeArgv`.** It derived the drawing name from the frame file name by splitting at the first `_`, which breaks for `text_alpha_faint_…`. It now strips the `_<size>_<background>.png` suffix, and the frame count is 5. Commit 82ab86b.
3. **Probe write-out side effect.** The write-out re-run rewrote the six earlier dxf-out PDFs. They differ only in `/CreationDate`; this was checked byte by byte with the date removed. So the committed bytes and the 75-02 `OUT` lines were kept, and PROVENANCE says so.

## Known Stubs

None.

## Cleanup

- `docker compose -p octoconv stop` (the live proof stopped the stack it started; no octoconv container runs). Never `down -v`.
- Probe engine images `octoconv-cad-worker:engine75-*` were removed by the probe's own cleanup.
- Build cache: the 27 records created by this run were found by creation time. The 20 unshared ones were pruned by ID; the 7 shared ones back the kept `octoconv-api:latest` and `octoconv-cad-worker:latest` (now b48e93b builds) and were kept. The run's 6 build-history records (4 probe engine builds, 2 compose builds) were removed. The previous `octoconv-api`/`octoconv-cad-worker` builds (9017b91bb571, 4076b99e5fc9) existed before this run; they are now dangling and were left untouched, as in 261012-gos. No other project's image, container, volume or cache record was touched. Final free space: VM 22 GiB, host 27 GiB.
- Clones and the RED worktree were deleted. No `$TMPDIR/multipart-*` files were left by this run.

## Self-Check: PASSED

- Commits 82ab86b, a76a30c and b48e93b are ancestors of HEAD; `git rev-list --count b71eac5..HEAD` = 3.
- Fixtures, dxf-out outputs and evidence files exist.
