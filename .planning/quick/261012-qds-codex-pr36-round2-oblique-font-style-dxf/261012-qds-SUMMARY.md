---
phase: quick-261012-qds
plan: 01
subsystem: cad-dxf
tags: [dxf, svg, fonts, liveproof, docs, codex-pr36]
status: complete
requires: [phase-75 dxf pipeline, quick-261012-o2d]
provides: [font attributes from the measured face, font cross-check, fonts probe group, asserted liveproof counters, DXF client docs with sync tests]
affects: [cmd/cad-worker/helper/cad-dxf, internal/convert caddxf*, scripts/cad-dxf-probe.sh, scripts/cad-liveproof.sh, README.md, docs/ERROR-CODES.md, docs/MEASUREMENTS.md, docs/openapi.yaml]
tech-stack:
  added: []
  patterns: [SVG font attributes derived from the OS/2, head and name tables of the font that measured the text; round trip proven by pdffonts against the font file's PostScript name]
key-files:
  created:
    - internal/convert/testdata/cad/dxf/text_fonts.dxf
    - internal/convert/testdata/cad/dxf-out/text_fonts.{svg,render.svg,texts.json,pdf}
    - internal/convert/cad_docs_sync_test.go
  modified:
    - cmd/cad-worker/helper/cad-dxf
    - internal/convert/caddxfsvg.go
    - internal/convert/caddxf.go
    - internal/convert/caddxfsvg_test.go
    - internal/convert/caddxf_test.go
    - internal/convert/testdata/cad/PROVENANCE.md
    - scripts/cad-corpus-gen.py
    - scripts/cad-dxf-probe.sh
    - scripts/cad-liveproof.sh
    - README.md
    - docs/ERROR-CODES.md
    - docs/MEASUREMENTS.md
    - docs/openapi.yaml
decisions:
  - "Font attributes come from the measured font's tables: family from name ID 16 (else 1), weight from usWeightClass (bold at 700, the number otherwise, absent at 400), style from fsSelection/macStyle italic bits (oblique when fsSelection bit 9 or the subfamily says Oblique), stretch from usWidthClass"
  - "The cad-worker keeps fonts-dejavu-core only: fonts-dejavu-extra would make ezdxf's fallback DejaVuSansCondensed.ttf and change every default-font text; the oblique path is proven in a probe-only image variant"
  - "The Go validator records each text's font (family, size, weight, style, stretch of the text and its tspans); the cross-check rejects a client and render-only SVG that disagree"
  - "The liveproof judges each DXF PDF against the committed texts.json and render-only SVG, so expected_runes is never 0 on a fixture row"
metrics:
  completed: 2026-10-12
  duration: about 3h
actuals:
  tokens: 21396
  tasks: 3
  commits: 3
plan_head_before: 2d8d25072ddc0360c8d3e4ae8f7ad68a5dbeae89
plan_head_after: ebd2e53cd18882e62ab8f7061cc72710aafdd876
---

# Quick 261012-qds: codex PR #36 round 2 (font face, DXF docs, liveproof counters) Summary

`cad-dxf` now writes the SVG font attributes from the font that measured each text, so rsvg-convert draws every face that sized the frame. In a probe image with all 22 DejaVu faces, each one round-trips through the PDF; before the fix 10 failed. The README and ERROR-CODES no longer say a DXF is unsupported, and sync tests hold the numbers to the code. The liveproof now asserts the counters it used to only record. Live: ALL PASS from a fresh clone. `ci-local` helm and gate PASS. Race FAILS on ci-local's fixed 10m timeout, which HEAD also exceeds on this loaded machine (721s).

## Premise check: the shipped image cannot show the codex defect

The cad-worker image installs `fonts-dejavu-core` only (`HDR,font_files=6` in `evidence/probe-fonts-shipped.txt`): Sans, Sans Mono and Serif, each Book and Bold. A style naming `DejaVuSansMono-Oblique.ttf` falls back to `DejaVuSans.ttf`. It is measured and emitted with that face, so measurement and SVG agree today. The defect becomes real as soon as an oblique face can be resolved. The extra-fonts probe at HEAD shows it: 10 of 22 faces were measured slanted and drawn upright (`evidence/probe-fonts-extra-RED.txt`). Seven condensed and light faces also got family names the Go allowlist rejects (`DejaVu Sans Condensed`, `DejaVu Sans Light`), so a whole drawing using them would be refused.

Shipping `fonts-dejavu-extra` was rejected. ezdxf's `DEFAULT_FONTS` lists `DejaVuSansCondensed.ttf` before `DejaVuSans.ttf`, so the extra package would change the default face of every DXF text, every committed fixture and the measured PDF text floor. As a consequence, the live proof cannot show an Oblique face in `pdffonts`: the shipped image has none. What the live proof does show is that the oblique-named styles round-trip to the face they were measured with: `pdffonts` lists exactly the six core faces and no oblique face. The oblique path itself is proven, confined, in the probe variant (`OCTOCONV_PROBE_FONT_PACKAGES=fonts-dejavu-extra`).

## Sweep 1: font attributes, measured face against what the SVG requests

| Attribute | Before | After | Proof |
|---|---|---|---|
| family | `get_fontname`: name ID 1 (`DejaVu Sans Condensed`, `DejaVu Sans Light` for condensed and light faces, both outside the allowlist) | name ID 16, else ID 1 (`DejaVu Sans` + stretch, `DejaVu Sans` + weight) | probe extra: 22/22 PDF fonts equal the file's PostScript name, 21 accepted by Go, Math TeX Gyre refused by the family allowlist as expected |
| weight | `"Bold" in` name ID 2: bold or nothing; ExtraLight emitted as normal | `usWeightClass` rounded to a CSS weight: `bold` at 700, the number otherwise, absent at 400 (existing outputs byte-identical) | `DejaVuSans-ExtraLight.ttf` row `svg_font=DejaVu_Sans:200` |
| style | dropped | italic when `fsSelection` bit 0 or `macStyle` bit 1; `oblique` when `fsSelection` bit 9 or the subfamily says Oblique | 10 RED rows to ok; Serif Italic gives `italic`, Sans/Mono Oblique give `oblique` |
| stretch | dropped (only the ID 1 family carried it) | `usWidthClass` other than 5 as its CSS keyword (`semi-condensed` for DejaVu Condensed, fontconfig width 87) | condensed rows |
| size | em x transform scale | unchanged (same face, same metrics) | — |
| fallback for an absent file | measured and drawn with `DejaVuSans.ttf` | unchanged, consistent | live DXF_SVG/DXF_PDF text_fonts: MONO OBLIQUE and MONO BOLD OBLIQUE are `DejaVu Sans`; `pdffonts` has no oblique face |
| extended font data (family + italic/bold) and MTEXT `\f...|b1|i1;` | ezdxf's `find_best_match` keeps only the `Regular` style when it exists, so the Book face is measured | unchanged, consistent; documented as a limit | fixture rows XDATA MONO ITALIC, XDATA MONO BOLD ITALIC, INLINE MONO BOLD ITALIC: `DejaVu Sans Mono`, no weight, in both images |
| TEXT oblique angle (geometric shear) | ezdxf's `simplified_text_chunks` ignores it, for measurement and drawing alike | unchanged, consistent; documented | `SHEARED TEXT` transform `matrix(0.7 0 0 1 …)`: width factor applied, no shear |
| width factor | in the transform; the SVG matrix is normalised by the y axis, size carries the rest | unchanged | same row |
| style width/oblique on a TEXT | DXF semantics: a TEXT's own codes 41/51 govern, the style values are defaults | unchanged | `SHEARED STYLE` upright, width 1 |
| client vs render-only SVG | not compared for fonts | `CADSVGFacts.TextFonts` (text and tspan font attributes) must agree text by text | `TestCADDXFConvertRejectsAFontTheOutputsDisagreeOn` (7 mutations x svg/pdf) |

## Sweep 2: public and client docs after phase 75

Every number below was taken from code, and the new sync test `internal/convert/cad_docs_sync_test.go` holds it there.

| Document | Statement | State before | Change |
|---|---|---|---|
| README, Engines intro | operations are `convert` and `sanitize` | false since phase 74 | `render` added, with its `measurements` output |
| README, engine table cad row | STEP/IGES → STL only | false | `dxf` → `svg` `pdf`; `render`: `step` `iges` `stl` `dxf` → `png`; the test checks both ways against `OperationClasses()` |
| README, cmd tree | cad-worker: STEP and IGES → STL | false | adds DXF → SVG/PDF and PNG previews |
| README, docs index | MEASUREMENTS: "no engine emits it yet" | false since phase 74 | emitted by `render`; covers DXF and its limits |
| ERROR-CODES `payload_too_large` | ceilings 63 MiB STEP, 135 MiB IGES | incomplete | adds 182 MiB STL (`CADMaxSTLBytes` 190840832), 118 MiB DXF (`CADMaxDXFBytes`) |
| ERROR-CODES CAD paragraph | "a valid DXF or STL … is still answered `unsupported_conversion`" | false | rewritten: DXF convert pairs, render pairs, four ceilings, DXF inspect `max_bytes` 123731968, the 100 MiB chart transport cap now named for IGES, STL and DXF |
| ERROR-CODES, new DXF paragraph | absent | — | `cad_too_complex` bounds (170031 instances, 850155 entities, nesting 16, SVG 129 MiB, 162 s and 141 s defaults), `cad_unreadable`, `cad_empty_result` (fully transparent drawing), `engine_error`, DXF option applicability |
| ERROR-CODES `engine_error` | "disagreed with its own text list" | incomplete since 261012-o2d | adds disagreement with the render-only SVG on content, opacity or font |
| ERROR-CODES async `invalid_options` | "cad accepts no options at all" | false since phase 74 | cad re-parses the five option values; applicability is `opts_not_applicable` at the API |
| ERROR-CODES `input_too_large` | 63/135 MiB | incomplete | adds 182 MiB STL, 118 MiB DXF |
| ERROR-CODES `cad_too_complex`, `cad_unreadable`, `cad_empty_result` rows | DXF-aware since phase 75 | true | unchanged |
| MEASUREMENTS, DXF drawings | transparency and kerning (R-1) only | incomplete | fonts rule, R-9 (U+FFFD outside the font), R-10 (PDF text below 0.00469174 pt, `CADDXFPDFTextFloorPt`), R-11 (sub-pixel features refused for render), TEXT oblique angle, extended font data |
| openapi `operation` and `outputs` | sanitize only | incomplete | `render`, `role=measurements`, DXF convert pairs |
| openapi enums, FINDINGS sentinels | `truncated_cad_file`, `binary_dxf_unsupported`, `dwg_unsupported` | true | unchanged |
| SANITIZE.md, TYPESETTING.md | no CAD statement | — | unchanged |
| Helm chart values.yaml and templates | no DXF prose; `CAD_DXF_TIMEOUT` 162s and `CAD_RSVG_TIMEOUT` 141s equal the code defaults | true | unchanged |
| MCP tool descriptions (`internal/mcpserver`) | no format list | — | unchanged |

`docs/CAD.md` and the MCP and frontend format tables remain phase 76 work (ROADMAP criterion 4); this task only removes false statements and records the limits phase 75 decided.

## Sweep 3: liveproof counters, recorded against asserted

| Row | Counter | Before | Now |
|---|---|---|---|
| DXF_PDF text_alpha | `pdftotext_clear_lines` | recorded | must be 0 |
| DXF_PDF text_alpha_faint | `pdftotext_gone_lines` | recorded | must be 0 |
| DXF_PDF (all six) | `expected_runes` | always 0: `cad-pdf-check` ran without an expectation | judged with `--texts`/`--svg` of the committed helper outputs, so it must be above 0; `invisible_text` must equal the fixture's (4, 1, 0) |
| DXF_SVG | `texts` | asserted only for uncommon (≥ 40) | equals the committed helper output's count, or at least 1 where none is committed; the Go count must equal the Python walk's `svg_texts` (every SVG row, the retry included) |
| MESH_SIGNAL | `code` | only non-empty | must be `cad_unreadable` (cad.go: a reader that died on a signal; 7 earlier runs agree) |
| DXF_PDF `pdftotext_chars` | recorded | justified: it counts pdftotext's layout whitespace too; the content is asserted by named strings and the Go judge |
| DXF_RETRY pdf | `expected_runes=0` | justified: the 50 MiB replicate has no committed outputs; the worker judged that PDF against its own texts.json, and the row's subject is the retry |
| HDR | `tracked_dirt` | justified: run provenance; this gate reads it (0 here), and asserting it would refuse every development run from a working tree |
| HDR | `disk_free_gib` | asserted ≥ 6 before it is written | unchanged |
| INSPECT | `estimated_max_seconds` | justified: `estimate_source=unmeasured` is asserted; the value is measured in phase 76 |
| VIEWS iso | `box`, `h_over_w` | justified: iso has no expected ratio; front and top are asserted, and three distinct images are |
| DXF_BOMB | `seconds` | justified: bounded by `wait_job` against the engine timeout, and the status and code are asserted |
| WORKER_KILL | `fresh_mesh_at_restart`, `restarted_after_s` | justified: a fresh mesh is the retried job's own (0 or 1); the restart is bounded by the 30 s loop and its compose fallback, and `kill_to_done` and `orphans=0` are asserted |
| DXF_MEAS (`any` rows) | `bbox` | justified: those rows test the unit; the bbox is pinned where a fixture defines one |
| IMAGE | sizes | justified: facts for phase 76 sizing |

Every other value in an `ev` line is asserted before it is written, or is an identifier, hash or duration.

**The new assertions fire** (`evidence/liveproof-assert-mutations.txt`). The functions are cut verbatim from the script and fed crafted PDFs: the committed render-only SVGs with every `fill-opacity="0.000"` set to 1, drawn by rsvg-convert. The Go PDF judge accepts both, because extra text is not a rejection. The new rows fail them: "pdftotext -raw holds a fully transparent text: CLEAR TEXT …" and "… holds the fully transparent GONE". A wrong invisible count fails. A text_fonts PDF with one Sans Mono text drawn from the oblique face fails on `pdffonts`. A PDF from another drawing fails the judge. The good PDFs pass. The HEAD functions, given the same crafted PDFs, record `pdftotext_clear_lines=3` and `pdftotext_gone_lines=1` and still print `result=ok`.

## Tasks and commits

| Task | Commit | What |
|---|---|---|
| 1 | 6fd65f0 | helper font attributes, Go allowlist, `TextFonts` and cross-check, tests, `text_fonts` fixture and outputs, probe `fonts` group and font-package variant, provenance |
| 2 | 8287bc6 | liveproof: counters asserted, text_fonts SVG/PDF/1280 render rows |
| 3 | ebd2e53 | README, ERROR-CODES, MEASUREMENTS, openapi, docs sync tests |

## TDD evidence

- `evidence/red-go-compile.txt`: the HEAD Go code with the new tests does not compile (`TextFonts`).
- `evidence/red-go-assert.txt`: the HEAD Go code, without the two `TextFonts` tests, using the real text_fonts outputs:
  - 34 strict-enum subtests fail: every accepted `font-style`, numeric weight and stretch, on text and on tspan;
  - 6 of the 14 disagreement subtests fail (dropped weight, swapped family, resized text). The other 8 are rejected at HEAD only because HEAD refuses `font-style`, `font-stretch` and weight 200 outright, which is exactly the defect for a real oblique face.
- `evidence/probe-fonts-extra-RED.txt`: HEAD helper and validator with `fonts-dejavu-extra`: `rows=22,fail=10`.
- `evidence/red-docs.txt`: the docs sync tests on the old docs: 10 README pair errors, 9 ceiling errors, no DXF paragraph, no floor.
- GREEN: `go test ./internal/convert/... ./scripts/...` ok (`internal/convert` 525.4s), plus the doc-sync packages (`measurements`, `errcode`, `inspect`, `api`, `worker`). `go vet ./...` clean, gofmt clean. `internal/convert/exec.go` is byte-identical to main.
- Mutation probe of the Go checks (`evidence/mutations-go.txt`). Each of 5 mutations fails a test:
  - the cross-check ignores fonts;
  - the tspan font is not recorded;
  - `font-style=normal` is admitted;
  - `font-size` is left out of the descriptor;
  - weight 400 is admitted.

## Live and gate results

| Gate | Result | Evidence |
|---|---|---|
| `scripts/cad-dxf-probe.sh fixtures --write` | ok: 44 fixtures, 44 committed, 0 strict expectations failed; `text_fonts` strict-clean; the 43 earlier files unchanged in git | `evidence/probe-fixtures.txt` |
| `helper --group semantics --write-out` | semantics 22 rows 0 fail; writeout 45 rows 0 fail. Every earlier SVG, texts.json, draft, PNG and frames.tsv came out byte-identical; the 8 PDFs differ only in `/CreationDate`, so the committed bytes were kept | `evidence/probe-helper.txt`, `evidence/writeout-pdf-dates.txt` |
| `helper --group fonts`, shipped engine | 6/6 ok | `evidence/probe-fonts-shipped.txt` |
| `helper --group fonts`, `fonts-dejavu-extra` variant | 22/22 ok (HEAD: 10 fail) | `evidence/probe-fonts-extra.txt` |
| `scripts/cad-liveproof.sh` from a fresh clone at ebd2e53, bundle copied, `tracked_dirt=0` | **ALL PASS**: dxf_svg=7, dxf_pdf=6, dxf_render=8 (one text_fonts row each); every other counter equals 261012-o2d's | `evidence/cad-liveproof.txt`, `.transcript.log` |
| `scripts/ci-local.sh helm gate race` from the same clone | **helm PASS, gate PASS, race FAIL**: helm PASS (local helm v4.3.0 vs pinned v4.2.3, as the transcript warns), gate PASS (`internal/convert` 524.7s), race **FAIL**: `internal/convert` hit ci-local's fixed 10m budget (606.970s, `panic: test timed out`, no `DATA RACE`). Not attributable to this task: on the same loaded machine (load average 7 to 12 from another project's k8s stand and compose stacks) HEAD 2d8d250 takes 721.1s under `-race` and ebd2e53 takes 664.1s, both `ok` with a 30m budget. 261012-o2d measured 361.9s gate and 561.7s race on a quieter machine | `evidence/ci-local-ebd2e53-20261012T170352Z.log`, `evidence/race-attribution.txt` |
| `scripts/sandbox-liveproof.sh cad` | **not run, not needed**. The helper reads the same font files under `/usr/share/fonts` that ezdxf already opened (fontTools tables of the open `TTFont`), with no new path, import or argv. The fonts probe ran the helper under its candidate Landlock set with 22 faces, and the liveproof SANDBOX row proved the confined worker | — |

Live text_fonts rows:
- DXF_SVG: `font_texts=16,fonts_as_expected=1,bold=3,font_style=0,font_stretch=0`.
- DXF_PDF: `expected_runes=153`, `pdffonts=DejaVuSans;DejaVuSans-Bold;DejaVuSansMono;DejaVuSansMono-Bold;DejaVuSerif;DejaVuSerif-Bold,oblique_faces=0`.
- DXF_RENDER 1280: the drawing judge passes (ink box 1123x1018 inside the 1203x1098 letterbox, margin 78 px).

## Deviations from Plan

1. **[Rule 3 - Blocking] API port 8090 held by another project.** The first liveproof attempt failed because `gsh-identity-service-envoy-1` holds `127.0.0.1:8088-8090` (`evidence/cad-liveproof.port-conflict.txt`). That container was not touched. The clone got an untracked `docker-compose.override.yml` mapping the API to `18090:8090` (`ports: !override`), and the run used `CAD_LIVEPROOF_API_BASE=http://127.0.0.1:18090`. Tracked dirt stays 0. Nothing else in the stack changed.
2. **[Rule 2 - Missing critical functionality] The liveproof PDF rows judged against nothing.** `cad-pdf-check` ran without `--texts`, so `expected_runes=0` on every row: a scan of nothing. The rows now judge against the committed helper outputs (commit 8287bc6). Found in the counter sweep.
3. **[Rule 2] MESH_SIGNAL accepted any code.** It is now pinned to `cad_unreadable` (commit 8287bc6).
4. **Docs beyond DXF.** Several statements were made false by phase 74 rather than 75: the render operation, the measurements output, cad options and the STL ceiling. They sat in the same paragraphs and were fixed in the same pass (commit ebd2e53).

## Findings not fixed (for the orchestrator)

- **Geometric oblique is not drawn.** ezdxf 1.4.4 ignores a TEXT's oblique angle (`simplified_text_chunks`). A DXF with ISO-style slanted text (oblique 15) renders upright in SVG, PDF and PNG, though the frame stays correct. Applying the shear in `cad-dxf` would also diverge from the corpus fidelity reference, which is ezdxf's own outline rendering, so it needs its own decision. It is documented in MEASUREMENTS.
- **Font requests by family are drawn upright.** Extended font data and MTEXT `\f` codes that ask for italic or bold get the Book face, because ezdxf's `find_best_match` filters on `Regular` first. Documented.
- **A backstop PDF ceiling reads as an empty result.** A PDF above `CADMaxDXFPDFBytes` (19 MiB, 1.5x the largest PDF measured at the input bounds) is a judge rejection and maps to `cad_empty_result`. It is unreachable within the measured input bounds; if it ever fires, `cad_too_complex` would be the honest code.
- **The race tier no longer fits ci-local's 10m budget on this machine under load.** HEAD alone takes 721s. Either re-run the race tier when the other stacks are idle, or the budget (which mirrors ci.yml) needs a decision; it was not changed here.
- **The Go PDF judge accepts extra text.** A PDF that shows text the SVG makes invisible passes it (shown by the crafted PDFs). rsvg-convert does not draw alpha-0 text, and the liveproof now asserts it, so this is recorded rather than changed.

## Known Stubs

None.

## Cleanup

- The live proof stopped the stack it started (`docker compose stop`); `docker compose -p octoconv stop` afterwards found nothing running. Never `down -v`. The port-conflict attempt had also stopped what it started.
- Images: the probe removed its `octoconv-cad-worker:engine75-*` and `-fonts` tags itself. The scratch `octoconv-qds-scratch:extra` image was removed. `octoconv-api:latest` and `octoconv-cad-worker:latest` are now the ebd2e53 builds. The previous builds `214047e0d440` and `6bda20713304` existed before this task and were left untouched, now dangling, as in 261012-o2d.
- Build cache: 53 records were created by this task (creation time from 2026-10-12 15:55Z; the only builds in that window are this task's 12). The 32 unshared ones were pruned by ID: 29, then 3 that became unshared once their children were gone. The 23 shared ones back the kept images and were kept. The 12 build-history records were removed: 5 probe engine builds, 2 font variants, 1 scratch image and 4 compose builds (two per liveproof attempt).
- No other project's container, image, volume or cache record was touched. That includes `gsh-identity-service-envoy-1`, which holds port 8090.
- Removed: the fresh clone (with its untracked compose override and bundle copy), the RED worktrees and the race-timing worktree; `git worktree list` shows only the main checkout. No `$TMPDIR/multipart-*` files. One `tmp.*` file in `$TMPDIR` is oapi-codegen output from another project and was left alone.
- Free space at the end: OrbStack VM 11.5 GiB (at least 6 required), host 15 GiB.

## Self-Check

**PASSED**
- Commits 6fd65f0, 8287bc6 and ebd2e53 are ancestors of HEAD; `git rev-list --count 2d8d250..HEAD` = 3 before this summary's commit.
- The fixture, its four dxf-out outputs, the sync test, the PLAN and every evidence file named above exist.
- `internal/convert/exec.go` is byte-identical to main (empty diff).

## Race tier follow-up (orchestrator)

- `ci-local race` at 93c14d3 from a fresh clone, load average ~7: FAIL, `internal/convert` timed out at 606.2s against the 10m budget, no race reported (`evidence/ci-local-race-93c14d3-timeout.log`).
- e1bf64e raises the race tier's `go test -race` timeout to 20m in `.github/workflows/ci.yml` and `scripts/ci-local.sh` together.
- `ci-local gate race` at e1bf64e from a fresh clone, load average ~9.8: ALL REQUESTED TIERS PASSED; `internal/convert` 520.2s (gate, 10m budget) and 645.7s (race, 20m budget) (`evidence/ci-local-gate-race-e1bf64e.log`).
- The gate tier's 520s is within its 10m budget but close to it under this load.
