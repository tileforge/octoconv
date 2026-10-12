---
phase: 75-dxf-2d-pipeline
verified: 2026-10-12T05:30:00Z
status: passed
score: 9/9 must-haves verified
covered_files: [".env.example",".planning/phases/75-dxf-2d-pipeline/75-01-PLAN.md",".planning/phases/75-dxf-2d-pipeline/75-01-SUMMARY.md",".planning/phases/75-dxf-2d-pipeline/75-02-PLAN.md",".planning/phases/75-dxf-2d-pipeline/75-02-SUMMARY.md",".planning/phases/75-dxf-2d-pipeline/75-03-PLAN.md",".planning/phases/75-dxf-2d-pipeline/75-03-SUMMARY.md",".planning/phases/75-dxf-2d-pipeline/75-04-PLAN.md",".planning/phases/75-dxf-2d-pipeline/75-04-SUMMARY.md",".planning/phases/75-dxf-2d-pipeline/75-05-PLAN.md",".planning/phases/75-dxf-2d-pipeline/75-05-SUMMARY.md",".planning/phases/75-dxf-2d-pipeline/75-06-PLAN.md",".planning/phases/75-dxf-2d-pipeline/75-06-SUMMARY.md",".planning/phases/75-dxf-2d-pipeline/75-07-PLAN.md",".planning/phases/75-dxf-2d-pipeline/75-07-SUMMARY.md",".planning/phases/75-dxf-2d-pipeline/75-08-PLAN.md",".planning/phases/75-dxf-2d-pipeline/75-08-SUMMARY.md",".planning/phases/75-dxf-2d-pipeline/75-09-PLAN.md",".planning/phases/75-dxf-2d-pipeline/75-09-SUMMARY.md",".planning/phases/75-dxf-2d-pipeline/75-10-PLAN.md",".planning/phases/75-dxf-2d-pipeline/75-10-SUMMARY.md",".planning/phases/75-dxf-2d-pipeline/evidence/75-03-guard.txt",".planning/phases/75-dxf-2d-pipeline/evidence/75-05-bounds.txt",".planning/phases/75-dxf-2d-pipeline/evidence/75-08-landlock.txt",".planning/phases/75-dxf-2d-pipeline/evidence/75-10-liveproof.txt",".planning/research/SANDBOX-PATHS.md","Dockerfile.cad-worker","cmd/cad-worker/helper/cad-dxf","cmd/cad-worker/helper/cad-guard","cmd/cad-worker/main.go","cmd/cad-worker/requirements-dxf.txt","deploy/chart/octoconv/templates/configmap.yaml","docker-compose.yml","docs/ERROR-CODES.md","docs/MEASUREMENTS.md","internal/api/api.go","internal/api/cad_ceiling_test.go","internal/api/cad_opts_test.go","internal/api/formats_handlers_test.go","internal/convert/cad.go","internal/convert/cad_test.go","internal/convert/caddxf.go","internal/convert/caddxf_fixtures_test.go","internal/convert/caddxf_test.go","internal/convert/caddxfpdf.go","internal/convert/caddxfpdf_test.go","internal/convert/caddxfpdffloor_test.go","internal/convert/caddxfsvg.go","internal/convert/caddxfsvg_test.go","internal/convert/cadguard_test.go","internal/convert/cadmeasure.go","internal/convert/cadmeasure_test.go","internal/convert/cadopts.go","internal/convert/cadopts_test.go","internal/convert/cadpixel.go","internal/convert/cadpixel_test.go","internal/convert/cadsandbox.go","internal/convert/cadsandbox_test.go","internal/convert/convert_test.go","internal/convert/converters.go","internal/convert/sandboxcoverage_test.go","internal/convert/sniff.go","internal/convert/sniff_test.go","internal/worker/cad_outputs_test.go","internal/worker/descriptors.go","internal/worker/pinning_test.go","scripts/cad-corpus-gen.py","scripts/cad-dxf-bounds.sh","scripts/cad-dxf-cr-fixture.sh","scripts/cad-dxf-guard-diff.sh","scripts/cad-dxf-probe.sh","scripts/cad-liveproof.sh","scripts/cad-worker-sandbox-measure.sh","scripts/cmd/cad-pdf-check/main.go","scripts/cmd/cad-pixel-judge/main.go","scripts/cmd/cad-svg-check/main.go","scripts/conf05-crosscheck.sh"]
covered_digest: "v3:sha256:1cae6f899bd84dfff5c6745016178aecd2eae31e580732dd208ebf9dfc9ade30"
behavior_unverified: 0
overrides_applied: 0
---

# Phase 75: DXF 2D Pipeline Verification Report

**Phase Goal:** Клиент получает векторный чертёж и превью DXF через другой тулчейн (ezdxf + rsvg-convert) под тем же конфайнментом; выход, собранный из недоверенного текста, безопасен по построению.
**Verified:** 2026-10-12T05:30:00Z
**Status:** passed
**Re-verification:** No (initial verification)

## Method

- I read ROADMAP §75, REQUIREMENTS, 75-CONTEXT (D-01..D-15, R-1..R-11), the ten PLAN/SUMMARY pairs and every evidence transcript.
- Then I read the code itself: `caddxf.go`, `caddxfsvg.go`, `caddxfpdf.go` (judge entry, page and decode), the `cadsandbox.go` diff, the `cad.go` stage classification, `cadopts.go`, `cadmeasure.go`, `descriptors.go`, `cmd/cad-worker/helper/cad-dxf` and the integration diffs.
- Docker was hung, so I ran nothing against containers. The live behaviour comes from committed transcripts. I re-ran everything that can run locally:
  - `GOTOOLCHAIN=go1.26.4 go vet ./...`: clean.
  - `go test ./... -count=1` with python 3.14 first on PATH: exit 0, 31 packages ok, no FAIL.
  - Single named tests re-run with `-v`, all PASS: `TestCADDXFConstantsComeFromTheEvidence`, `TestCADDXFPDFTextFloorComesFromTheEvidence`, `TestValidateCADSVGRoundTripsSpecialCharacters`, `TestCADDXFRenderMeasurementsComeFromTheDraft` and `TestCADDXFConvertRejectsUntrustedHelperOutputs`.
  - No DXF/CADSandbox/Guard test skipped. The only skips in that filter are two unrelated audio tests.
  - `scripts/conf05-crosscheck.sh cad`: MATCH for cadguard, cadmesh, cadrender, caddxf (4/1), cadrsvg (1/1) and multiarch (2). Exit 0.
  - `gofmt -l` over the changed Go files: empty.
  - The 8 `multipart-*` files that my test runs left in `$TMPDIR` were removed after `lsof` showed nobody held them. 0 remain.

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | **SC1 / CCONV-02.** dxf→svg is a vector of model space with visible layers only; text is not lost (cp1251 included); the SVG is well-formed with no script, foreignObject, event handlers or external refs, including for text with `&<>"'` | ✓ VERIFIED | **Model space and layers.** cad-dxf draws `doc.modelspace()` only (helper :326), and the frontend honours off and frozen layers. Live `DXF_SVG layers_off_frozen`: `visible=present, hidden_off=absent, hidden_frozen=absent`.<br>**Text as `<text>` (D-04).** `TextPipeline.draw_text` intercepts every text and emits it as SVG `<text>`; `TEXT_AS_OUTLINE=False`.<br>**Client bytes are Go bytes.** The client output is not the helper's file: `cadDXFValidate(draw, outPath)` re-serializes it through `ValidateCADSVG`.<br>**Closed allowlist (caddxfsvg.go).** No `href`, `style`, `id`, `class`, `use`, `image` or `on*`. Directives, comments and PIs other than the declaration are rejected, foreign namespaces too. Depth is capped at 16, bytes at `CADMaxDXFSVGBytes`. Attribute grammars are regex plus finite-number checks.<br>**Cross-check.** `cadDXFCrossCheck` ties texts.json, the client SVG, the render-only SVG and the verdict counts together.<br>**Live.** `legacy_cp1251 detal_cp1251=present`; `text_cases special_text=exact`, `reserialized_identical=1`, `forbidden_elements=0`, `on_attrs=0`, `href_attrs=0`, `doctype=0`, `raw_script_tag=0`.<br>**Tests.** `TestValidateCADSVGRoundTripsSpecialCharacters` PASS; `TestValidateCADSVGRejectsMutations` (61 mutations) in the green suite. |
| 2 | **SC2 / CCONV-03.** dxf→pdf gives a valid PDF whose page holds the drawing: not empty, text extractable | ✓ VERIFIED | **Path.** rsvg-convert renders the Go-validated render-only SVG (`clean.svg`), then `JudgeCADDXFPDF` judges the PDF before success.<br>**Judge (caddxfpdf.go).** It requires exactly 1 page object with `/Pages /Count 1`, and rejects Encrypt, ObjStm and XRefStm. The page needs ≥1 paint op beyond the background. The covered text must appear as an ordered subsequence (R-3), with texts below the 0.00469174 pt render floor excluded and counted (R-10).<br>**Live.** `DXF_PDF` legacy_cp1251, text_cases and uncommon all show `starts_pdf=1`, `pdfinfo_pages=1` and `pdf_check=ok`. pdftotext finds Деталь, the special string and non-empty text. |
| 3 | **SC3 / RNDR-06 (+MEAS-01, MEAS-03).** render dxf→png is top-down with the 3D size and background rules; a non-square drawing is letterboxed without cropping; `view` on dxf is 422; the second output is `measurements` with an entity 2D bbox (not $EXTMIN/$EXTMAX), no volume, units from $INSUNITS or `unspecified` | ✓ VERIFIED | **Letterbox.** `CADDrawingLetterbox` plus the rsvg `--page-width/--left/--top` argv; `JudgeCADDrawingPNG` gives J1 ≥1 ink pixel (R-11), J2 span ≥0.5 of the letterbox, J3 margin and the ink staying inside the letterbox.<br>**Live render.** `DXF_RENDER wide` gives box 1123x13 in a 1203x92 letterbox; tall the transpose; wide at 2048 transparent has `alpha=1`; legacy_cp1251 at 256. Every row has `outputs=2, roles=primary@0;measurements@1`.<br>**Live 422s.** `view top` → 422 `opts_not_applicable`; `quality fine` → 422 `opts_not_applicable`; `view sideways` → 422 `invalid_options`; convert with `size` → 422 `opts_not_applicable`. All have `jobs_created=0`.<br>**bbox.** The helper takes it from drawn primitives plus text boxes (R-2). Live `wrong_extents bbox=10:20:40:30` matches the expected value against wrong header extents. far_point is 0:0:100000:100000 (R-4).<br>**Units.** units_1 inch, units_13 micrometre, units_0 unspecified/none, units_0 with assume mm → mm/client_assumed, units_3 with assume mm → unspecified/none. This matches D-11 and docs/MEASUREMENTS.md.<br>**Volume and area.** Null with `not_applicable_2d` in every row.<br>**Atomic outputs.** The two outputs go through `AddOutputs` (`TestCADOutputsDXFRender*` in the green suite). |
| 4 | **SC4 (+CADIN-04).** A DXF bomb (MINSERT explosion, cyclic blocks, non-numeric coordinates) ends terminally within measured bounds without OOM; XREF and IMAGE are never resolved | ✓ VERIFIED | **Guard argv.** The DXF guard gets caps 170031, 850155, nesting 16 and abs-coordinate 1e12 (`cadGuardArgs`), with ezdxf-side backstops (audit code 104 → too_complex, `--max-drawn-entities`).<br>**Live `DXF_BOMB` (15 rows).** minsert_over_inserts, entities_over, dimension_minsert, cyclic_self, cyclic_ab, cyclic_case and nest_d17 → `cad_too_complex`. nonnumeric abc, nan, inf, underscore and coord_1e400 → `cad_unreadable`. 3dsolids, all_hidden and single_point → `cad_empty_result`. Every row: `retried=0, outputs=0, oom_killed=false, restart_count=0`.<br>**Bounds.** 75-05 rows peak at most 2565 MiB against the 2842.3 MiB budget, with `pct_of_budget` recorded on every row.<br>**Never resolved.** `SKIP_TYPES` covers IMAGE/IMAGEDEF/UNDERLAY/OLE, `ImagePolicy.IGNORE` is set, and an INSERT of an xref block is skipped as `XREF`. Live `never_resolved`: 6 elements, no href, no image element. |
| 5 | **SC5 (+CADW-02).** cad-dxf and rsvg-convert each run under their own measured ruleset, recorded in SANDBOX-PATHS.md and held by a test; the preflight judges by content; the font is reachable under the ruleset and text does not disappear | ✓ VERIFIED | **Rulesets.** `cadDXFReadOnly` and `cadRsvgReadOnly` in cadsandbox.go equal the `conf05:caddxf` and `conf05:cadrsvg` blocks (conf05 MATCH, re-run now). The Go pin test is `cadsandbox_test.go:583`.<br>**Measurement (75-08-landlock).** 31 removal rows. Negative controls drop `/usr/share/fonts`: cad-dxf → `failed/font_unavailable`, rsvg → PDF text empty, `failed_as_expected`.<br>**Job-local HOME.** `cadDrawStage` puts HOME and the XDG dirs in 0700 directories inside the job dir.<br>**Preflight (`cadPreflightDrawing`).** It draws legacy_cp1251 inside the job dir and requires the single text `Деталь`, fully covered. It judges the PDF through the R-10 and R-3 judges, judges the PNG with the drawing judge, and requires denial for cad-dxf and rsvg-convert on a file outside the job dir.<br>**Sandbox liveproof.** At 3d0761e (dirt 0): ALL PASS (5), including refuses-without-landlock and emulated-amd64-refuses. No sandbox file changed after 3d0761e.<br>**Final live.** The `SANDBOX` row names cad-dxf and rsvg-convert, with `sandbox_disabled=false`. |
| 6 | DERIVED constants in code equal the evidence values | ✓ VERIFIED | **By hand.** 118<<20 = 123731968; 170031; 850155; 129<<20 = 135266304; 19<<20 = 19922944; 162 s; 141 s; nesting 16 (75-03-guard); text floor 0.00469174 (75-05).<br>**By test.** `TestCADDXFConstantsComeFromTheEvidence` and `TestCADDXFPDFTextFloorComesFromTheEvidence` parse the DERIVED lines and PASS. The guard + dxf + rsvg + I/O sum is 393 s, within the 886 s engine timeout. |
| 7 | Exposure landed in exactly one commit (bd90a59), and every earlier commit is inert | ✓ VERIFIED | Only bd90a59 touches `converters.go`, `internal/api/api.go` (`defaultMaxFormatBytes[cad]["dxf"]`), `formats_handlers_test.go` and `convert_test.go`. The earlier `cmd/cad-worker/main.go` commits (4ae52b3, 3d0761e) only add timeouts and a log line; the `Default.Replace` of the DXF converters is in bd90a59.<br>The mutation evidence 75-09-mutations has 3 mutations, all FAIL as expected; they are kill evidence, not passes. |
| 8 | `internal/convert/exec.go` is byte-identical to main | ✓ VERIFIED | `git diff --quiet main HEAD -- internal/convert/exec.go` → identical. |
| 9 | No code comments added; no FAIL or NOT RUN recorded as a pass | ✓ VERIFIED | **Comments.** The added lines in `*.go`, `*.py` and `cmd/cad-worker/helper/*` hold no comment. The only matches are a `//go:embed` directive and the helper's shebang. The shell `#` changes in conf05-crosscheck.sh rewrite existing header lines (eleven → thirteen classes); they are not new comments.<br>**FAIL and NOT RUN.** In evidence, FAIL appears only in 75-09-mutations (expected kills), and NOT_RUN only inside a RULE definition. The 75-10 live proof is `ALL PASS` on b2c0db8 with `tracked_dirt=0`. After it come docs-only commits (`git diff --stat b2c0db8 HEAD` touches only .planning). The transient failures (the first 75-08 landlock run, the 75-10 dev runs) are disclosed in the SUMMARYs and SANDBOX-PATHS.md and are not used as evidence. |

**Score:** 9/9 truths verified (0 present, behavior-unverified)

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `cmd/cad-worker/helper/cad-dxf` | ezdxf helper: model space, visible layers, text as text, skips, bbox, draft, two SVGs, texts.json | ✓ VERIFIED | 468 lines; copied and chmod'ed in Dockerfile.cad-worker; run via `dxfStage` |
| `cmd/cad-worker/helper/cad-guard` (dxf mode) | caps, cycles, nesting, numerics | ✓ VERIFIED | argv validated (`--format dxf requires ...`); differential 105 variants with 0 unexplained (75-03) |
| `cmd/cad-worker/requirements-dxf.txt` | hash-locked subset of the phase-71 lock (R-6) | ✓ VERIFIED | `LOCK,cmp=identical` in 75-02 evidence; installed with `--require-hashes --no-deps --only-binary` |
| `internal/convert/caddxf.go` | CADDXFConverter and CADDXFRenderConverter | ✓ VERIFIED | Registered in `converters.go` and replaced with the confined converter in `cmd/cad-worker/main.go` |
| `internal/convert/caddxfsvg.go` | D-05 allowlist and re-serialization | ✓ VERIFIED | Used for the client SVG, the render-only SVG and the preflight |
| `internal/convert/caddxfpdf.go` | stdlib PDF judge (R-3, R-10) | ✓ VERIFIED | Used by Convert(pdf) and the preflight |
| `internal/convert/cadpixel.go` (drawing mode) | letterbox and drawing judge (R-11) | ✓ VERIFIED | Used by the render converter and the preflight; calibrated with 79 frames and 0 unexplained (75-08-judge) |
| `internal/convert/cadsandbox.go` | DXF and rsvg rulesets, job-local HOME, preflight | ✓ VERIFIED | conf05 MATCH; preflight wired in `CADPreflight` |
| `internal/worker/descriptors.go` | terminal and retryable mapping, codes | ✓ VERIFIED | Timeouts and content failures are terminal. `ErrCADDrawingFailed` (crash) is retryable. SIGKILL → `ErrCADEngineKilled`, terminal |
| SANDBOX-PATHS.md `## cad` | caddxf and cadrsvg blocks | ✓ VERIFIED | Present; held by conf05 and the Go pin test |
| docs/MEASUREMENTS.md and ERROR-CODES.md | DXF units table and codes | ✓ VERIFIED | Units table matches `cadUnitForDXF`; the kerning limit (R-1) is documented |

### Key Link Verification

| From | To | Via | Status |
|------|----|-----|--------|
| `/v1/formats` and inspect | DXF converters | `Default.Register` in converters.go (bd90a59) | ✓ WIRED (live `FORMATS cad_pairs=8`, `INSPECT dxf offers=3`) |
| API create | 118 MiB ceiling | `defaultMaxFormatBytes[cad]["dxf"] = CADMaxDXFBytes` | ✓ WIRED (live `DXF_CEILING` 413 at 123731969 bytes) |
| Converter | guard → cad-dxf → rsvg-convert | `dxfDrawing` → `guardStage` / `dxfStage` / `rsvgStage` under `cadStage` / `cadDrawStage` | ✓ WIRED |
| cad-dxf output | client SVG | `ValidateCADSVG` re-serialization to `outPath` | ✓ WIRED |
| render-only SVG | rsvg-convert | Go-reserialized `clean.svg` as the only rsvg input | ✓ WIRED |
| Render | second output | `cadDXFWriteNew` sidecar (remove, then O_EXCL) → `cadSideOutputs` → `AddOutputs` | ✓ WIRED |
| Worker startup | preflight | `CADPreflight` → `cadPreflightDrawing` | ✓ WIRED (live log line names cad-dxf and rsvg-convert) |
| Timeouts | env and config | `CAD_DXF_TIMEOUT` and `CAD_RSVG_TIMEOUT` in main.go, .env.example, compose and Helm configmap; `ValidateCADTimeouts` sum check | ✓ WIRED |

### Data-Flow Trace (Level 4)

| Artifact | Data | Source | Real data | Status |
|----------|------|--------|-----------|--------|
| measurements.json (dxf render) | bbox, unit | helper draft (drawn primitives plus text boxes, `$INSUNITS`) → `buildCADMeasurements` → `measurements.Validate` | yes (live wrong_extents, units_* rows) | ✓ FLOWING |
| PDF text | texts | texts.json → R-3/R-10 expectation vs decoded ToUnicode text | yes (live text_runes 6, 147, 299) | ✓ FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Full suite | `GOTOOLCHAIN=go1.26.4 go test ./... -count=1` (python 3.14 first) | exit 0, 31 ok | ✓ PASS |
| Vet | `GOTOOLCHAIN=go1.26.4 go vet ./...` | clean | ✓ PASS |
| Constants pinned to evidence | `go test -run TestCADDXFConstantsComeFromTheEvidence -v` | PASS | ✓ PASS |
| `&<>"'` round trip | `go test -run TestValidateCADSVGRoundTripsSpecialCharacters -v` | PASS | ✓ PASS |
| Ruleset document ↔ Go | `scripts/conf05-crosscheck.sh cad` | 6 MATCH, exit 0 | ✓ PASS |
| Live pipeline | committed `75-10-liveproof.txt` (docker hung; not re-run) | ALL PASS, b2c0db8, dirt 0 | ✓ PASS (transcript) |

### Probe Execution

Step 7c: no `scripts/*/tests/probe-*.sh` is declared by this phase. The phase's runnable proofs are the live and measurement scripts. Their committed transcripts are cited above, and they cannot be re-run while the docker daemon is hung.

### Requirements Coverage

| Requirement | Source Plan | Status | Evidence |
|-------------|-------------|--------|----------|
| RNDR-06 | 75-01..09 | ✓ SATISFIED | Truth 3 |
| CCONV-02 | 75-01..10 | ✓ SATISFIED | Truth 1 |
| CCONV-03 | 75-01..10 | ✓ SATISFIED | Truth 2 |
| MEAS-01 (extends) | roadmap SC3 | ✓ SATISFIED | two outputs via AddOutputs; live `outputs=2` |
| MEAS-03 (extends) | roadmap SC3 | ✓ SATISFIED | `$INSUNITS` table, 2D bbox, no volume |
| CADIN-04 (extends) | roadmap SC4 | ✓ SATISFIED | Truth 4 |
| CADW-02 (extends) | roadmap SC5 | ✓ SATISFIED | Truth 5 |

No orphaned requirements: REQUIREMENTS.md maps only RNDR-06, CCONV-02 and CCONV-03 to phase 75. The traceability rows still read Pending; the orchestrator owns that update.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| cmd/cad-worker/helper/cad-dxf | 16, 238, 277, 374 | `TEXT_AS_OUTLINE = False` test seam with dead branches in the production helper | ℹ️ Info | Disclosed in 75-02 SUMMARY ("Test seams in the production helper"). Not reachable in production |
| — | — | No TBD, FIXME or XXX added; no stubs | — | — |

### Known accepted limits (phase 76 docs, not gaps)

- Kerning R-1.
- U+FFFD outside DejaVu R-9.
- PDF text floor R-10.
- Sub-pixel features on render R-11.
- Go-side RSS gap: 61 MiB in the PDF judge against the 24.9 MiB Go share (`GAP go_worker_rss_dxf`).

### Out-of-scope findings (recorded for the orchestrator, do not affect this phase's status)

- **⚠️ 3D render sidecar written with `os.WriteFile` (phase 74 code, `internal/convert/cadrender.go`).**
  - A compromised, confined `cad-render` could plant a symlink at the sidecar path, and the unconfined worker would write through it.
  - The DXF path closes this class with `cadDXFWriteNew` (remove, then O_EXCL), which a test pins.
  - The 3D path is logged in deferred-items.md and is not covered by any phase-76 success criterion. It needs explicit scheduling.
- **ℹ️ internal/api ceiling tests leak `multipart-*` files of 63, 118 and 135 MiB into `$TMPDIR` on every `go test`.** Test-only; logged in deferred-items.md.
- **ℹ️ The cad-dxf and rsvg-convert rulesets are measured on arm64 only.** This is the same precedent as phase 74's render stage. The emulated-amd64 image refuses to start unconfined (sandbox liveproof).
- **ℹ️ The 118 MiB ceiling is measured on a 122.18 MB replicate.** The top 1.26% of admitted sizes was not measured; the row peak there is at 38.6% of budget.

### Human Verification Required

None. Every success criterion has live transcript evidence plus unit tests.

### Gaps Summary

None. The phase delivers its goal:
- dxf→svg, dxf→pdf and render dxf→png run through ezdxf and rsvg-convert, each confined by its own measured Landlock set.
- The client SVG is Go-reserialized through a closed allowlist.
- PDF and PNG are judged by content before upload.
- DXF bombs end terminally at the guard.
- The constants are pinned to the evidence by tests.

---

_Verified: 2026-10-12T05:30:00Z_
_Verifier: Claude (gsd-verifier)_
