---
phase: 75-dxf-2d-pipeline
verified: 2026-10-12T14:09:22Z
status: passed
score: 12/12 must-haves verified
covered_files: [".env.example",".github/workflows/ci.yml",".planning/phases/75-dxf-2d-pipeline/75-01-PLAN.md",".planning/phases/75-dxf-2d-pipeline/75-01-SUMMARY.md",".planning/phases/75-dxf-2d-pipeline/75-02-PLAN.md",".planning/phases/75-dxf-2d-pipeline/75-02-SUMMARY.md",".planning/phases/75-dxf-2d-pipeline/75-03-PLAN.md",".planning/phases/75-dxf-2d-pipeline/75-03-SUMMARY.md",".planning/phases/75-dxf-2d-pipeline/75-04-PLAN.md",".planning/phases/75-dxf-2d-pipeline/75-04-SUMMARY.md",".planning/phases/75-dxf-2d-pipeline/75-05-PLAN.md",".planning/phases/75-dxf-2d-pipeline/75-05-SUMMARY.md",".planning/phases/75-dxf-2d-pipeline/75-06-PLAN.md",".planning/phases/75-dxf-2d-pipeline/75-06-SUMMARY.md",".planning/phases/75-dxf-2d-pipeline/75-07-PLAN.md",".planning/phases/75-dxf-2d-pipeline/75-07-SUMMARY.md",".planning/phases/75-dxf-2d-pipeline/75-08-PLAN.md",".planning/phases/75-dxf-2d-pipeline/75-08-SUMMARY.md",".planning/phases/75-dxf-2d-pipeline/75-09-PLAN.md",".planning/phases/75-dxf-2d-pipeline/75-09-SUMMARY.md",".planning/phases/75-dxf-2d-pipeline/75-10-PLAN.md",".planning/phases/75-dxf-2d-pipeline/75-10-SUMMARY.md",".planning/phases/75-dxf-2d-pipeline/deferred-items.md",".planning/phases/75-dxf-2d-pipeline/evidence/75-03-guard.txt",".planning/phases/75-dxf-2d-pipeline/evidence/75-05-bounds.txt",".planning/phases/75-dxf-2d-pipeline/evidence/75-08-landlock.txt",".planning/phases/75-dxf-2d-pipeline/evidence/75-10-liveproof.txt",".planning/quick/261012-gos-close-phase-75-deferred-items-job-dir-wr/261012-gos-PLAN.md",".planning/quick/261012-gos-close-phase-75-deferred-items-job-dir-wr/261012-gos-SUMMARY.md",".planning/quick/261012-gos-close-phase-75-deferred-items-job-dir-wr/evidence/rg-av-progress-liveproof.txt",".planning/quick/261012-gos-close-phase-75-deferred-items-job-dir-wr/evidence/rg-cad-liveproof.transcript.log",".planning/quick/261012-gos-close-phase-75-deferred-items-job-dir-wr/evidence/rg-cad-liveproof.txt",".planning/quick/261012-gos-close-phase-75-deferred-items-job-dir-wr/evidence/rg-cancel-liveproof.txt",".planning/quick/261012-gos-close-phase-75-deferred-items-job-dir-wr/evidence/rg-ci-local-run1-20261012T113321Z.log",".planning/quick/261012-gos-close-phase-75-deferred-items-job-dir-wr/evidence/rg-ci-local-run2-20261012T115951Z.log",".planning/quick/261012-gos-close-phase-75-deferred-items-job-dir-wr/evidence/rg-ci-local-run3-4c1c1ef-20261012T134203Z.log",".planning/quick/261012-gos-close-phase-75-deferred-items-job-dir-wr/evidence/rg-convert-timing.txt",".planning/quick/261012-gos-close-phase-75-deferred-items-job-dir-wr/evidence/rg-gate-46e742c-20261012T124733Z.log",".planning/quick/261012-gos-close-phase-75-deferred-items-job-dir-wr/evidence/rg-gate-b2c0db8-20261012T124216Z.log",".planning/quick/261012-gos-close-phase-75-deferred-items-job-dir-wr/evidence/rg-sandbox-liveproof-audio.txt",".planning/quick/261012-gos-close-phase-75-deferred-items-job-dir-wr/evidence/rg-sandbox-liveproof-av.txt",".planning/quick/261012-gos-close-phase-75-deferred-items-job-dir-wr/evidence/rg-sandbox-liveproof-cad.txt",".planning/quick/261012-gos-close-phase-75-deferred-items-job-dir-wr/evidence/rg-sandbox-liveproof-document.txt",".planning/quick/261012-gos-close-phase-75-deferred-items-job-dir-wr/evidence/rg-sandbox-liveproof-html.txt",".planning/quick/261012-gos-close-phase-75-deferred-items-job-dir-wr/evidence/rg-sandbox-liveproof-image.txt",".planning/quick/261012-gos-close-phase-75-deferred-items-job-dir-wr/evidence/rg-sandbox-liveproof-ocr.txt",".planning/quick/261012-gos-close-phase-75-deferred-items-job-dir-wr/evidence/rg-sandbox-liveproof-pandoc.txt",".planning/quick/261012-gos-close-phase-75-deferred-items-job-dir-wr/evidence/rg-sandbox-liveproof-probe.txt",".planning/quick/261012-gos-close-phase-75-deferred-items-job-dir-wr/evidence/rg-sandbox-liveproof-progress.log",".planning/quick/261012-gos-close-phase-75-deferred-items-job-dir-wr/evidence/rg-sse-liveproof.txt",".planning/research/SANDBOX-PATHS.md","Dockerfile.cad-worker","cmd/cad-worker/helper/cad-dxf","cmd/cad-worker/helper/cad-guard","cmd/cad-worker/main.go","cmd/cad-worker/requirements-dxf.txt","deploy/chart/octoconv/templates/configmap.yaml","docker-compose.yml","docs/ERROR-CODES.md","docs/MEASUREMENTS.md","internal/api/api.go","internal/api/cad_ceiling_test.go","internal/api/cad_opts_test.go","internal/api/formats_handlers_test.go","internal/api/handlers.go","internal/api/inspect_handlers.go","internal/api/multipart_spill_test.go","internal/convert/av.go","internal/convert/cad.go","internal/convert/cad_test.go","internal/convert/caddxf.go","internal/convert/caddxf_fixtures_test.go","internal/convert/caddxf_test.go","internal/convert/caddxfpdf.go","internal/convert/caddxfpdf_test.go","internal/convert/caddxfpdffloor_test.go","internal/convert/caddxfsvg.go","internal/convert/caddxfsvg_test.go","internal/convert/cadguard_test.go","internal/convert/cadmeasure.go","internal/convert/cadmeasure_test.go","internal/convert/cadopts.go","internal/convert/cadopts_test.go","internal/convert/cadpixel.go","internal/convert/cadpixel_test.go","internal/convert/cadrender.go","internal/convert/cadrender_test.go","internal/convert/cadsandbox.go","internal/convert/cadsandbox_test.go","internal/convert/convert_test.go","internal/convert/converters.go","internal/convert/jobdirfs.go","internal/convert/jobdirfs_test.go","internal/convert/libreoffice.go","internal/convert/ocrpdf.go","internal/convert/pandoc.go","internal/convert/sandboxcoverage_test.go","internal/convert/sanitizeconverter.go","internal/convert/sanitizepdf.go","internal/convert/sanitizeverify.go","internal/convert/sniff.go","internal/convert/sniff_test.go","internal/convert/whisper.go","internal/worker/cad_outputs_test.go","internal/worker/descriptors.go","internal/worker/jobdirread_test.go","internal/worker/pinning_test.go","internal/worker/worker.go","scripts/cad-corpus-gen.py","scripts/cad-dxf-bounds.sh","scripts/cad-dxf-cr-fixture.sh","scripts/cad-dxf-guard-diff.sh","scripts/cad-dxf-probe.sh","scripts/cad-liveproof.sh","scripts/cad-worker-sandbox-measure.sh","scripts/ci-local.sh","scripts/cmd/cad-pdf-check/main.go","scripts/cmd/cad-pixel-judge/main.go","scripts/cmd/cad-svg-check/main.go","scripts/conf05-crosscheck.sh"]
covered_digest: "v3:sha256:211ac0b6f88d9b59dff5df1667842e7f5280eb0f17ed014891493d7213ae247e"
behavior_unverified: 0
overrides_applied: 0
re_verification:
  previous_status: passed
  previous_score: 9/9
  previous_head: d34fbdf
  reason: "quick 261012-gos changed covered files after d34fbdf (job-dir read/write helpers, multipart cleanup, requireCADPython floor, gate timeout 5m to 10m)"
  gaps_closed: []
  gaps_remaining: []
  regressions: []
---

# Phase 75: DXF 2D Pipeline Verification Report

**Phase Goal:** Клиент получает векторный чертёж и превью DXF через другой тулчейн (ezdxf + rsvg-convert) под тем же конфайнментом; выход, собранный из недоверенного текста, безопасен по построению.
**Verified:** 2026-10-12T14:09:22Z at HEAD 76d080f
**Status:** passed
**Re-verification:** Yes. The 9/9 pass at d34fbdf went stale when quick task 261012-gos changed covered files.

## Method

- **Scope of the change.** I diffed d34fbdf..76d080f.
  - Source: `internal/convert/jobdirfs.go` (new), plus call-site swaps in caddxf.go, caddxfpdf.go, cadmeasure.go, cad.go, cadrender.go, libreoffice.go, pandoc.go, av.go, whisper.go, ocrpdf.go and the three sanitize files.
  - `internal/worker/worker.go` (`uploadFrom`) and `internal/worker/descriptors.go` (two side-output reads).
  - `internal/api/handlers.go` and `inspect_handlers.go`: a guarded `MultipartForm.RemoveAll` defer.
  - `cadguard_test.go` (`requireCADPython` 3.10 floor), `.github/workflows/ci.yml` and `scripts/ci-local.sh` (gate timeout 5m to 10m).
  - ROADMAP.md and REQUIREMENTS.md did not change.
- **Code read.** I read every changed hunk, the new helper, the terminal-signature lists in worker.go, `isPandocTerminal`, and the runner's SkipRetry branch (`runner.go:294`).
- **Re-run locally** (docker not used):
  - `gofmt -l .`: empty.
  - `GOTOOLCHAIN=go1.26.4 go vet ./...`: clean.
  - `GOTOOLCHAIN=go1.26.4 go test ./... -count=1 -timeout 10m` with the python 3.14 shim first on PATH: exit 0, 31 `ok`, no FAIL.
  - Named tests with `-v`, all PASS and none skipped:
    - `TestCADDXFConstantsComeFromTheEvidence`, `TestCADDXFPDFTextFloorComesFromTheEvidence`
    - `TestValidateCADSVGRoundTripsSpecialCharacters`, `TestCADDXFConvertRejectsUntrustedHelperOutputs`
    - `TestCADDXFRenderWritesTheSidecarAsANewFile`, `TestCADDXFGuardArgv`, `TestCADMeshRefusesPartialMeshingBeforeWritingTheSTL`
    - `TestCADRenderWritesTheSidecarAsANewFile`, `TestOutputValidatorsRefuseAPlantedLink`
    - `TestCADOutputsDXF*` (3), `TestWorkerReadsOfTheJobDirRefuseAPlantedLink`
    - `TestUploadFromRefusesAPlantedPrimaryTerminally`, `TestUploadFromOtherOpenErrorsStayTransient`, `TestValidatorFailureTextsKeepTheirClassification`
    - `TestMultipartSpillFilesAreRemovedWhenTheHandlerReturns`
  - `scripts/conf05-crosscheck.sh cad`: MATCH for cadguard, cadmesh, cadrender, caddxf (4/1), cadrsvg (1/1) and multiarch (2). Exit 0.
  - `$TMPDIR` `multipart-*`: 6 before my runs and the same 6 after, so my runs added 0. Those 6 date from 15:42 MSK (12:42Z), the time of the b2c0db8 pre-fix gate clone run (`rg-gate-b2c0db8-20261012T124216Z.log`). No process holds them open (lsof empty). I left them in place.
- **Live evidence was not re-run** (docker excluded). It is read from the quick task's committed transcripts and tied to the code by commit. The last source change is eebd7ef. 46e742c differs from it only in the SUMMARY. 4c1c1ef and 76d080f add only the two timeout lines and docs/evidence (`git diff --name-only 46e742c HEAD`).

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | **SC1 / CCONV-02.** dxf→svg is a vector of model space with visible layers only; text is not lost (cp1251 included); the SVG is well-formed with no script, foreignObject, event handlers or external refs, including for text with `&<>"'` | ✓ VERIFIED | **Unchanged since d34fbdf:** `caddxfsvg.go`, `cmd/cad-worker/helper/cad-dxf` and the allowlist.<br>**Changed:** `cadDXFValidate` now opens the helper SVG through `OpenJobFile`. That adds O_NONBLOCK and a regular-file fstat check to the previous O_NOFOLLOW. The Go re-serialization to `outPath` is unchanged.<br>**Live at 46e742c (dirt 0, `rg-cad-liveproof.txt`).** 5 DXF_SVG rows, result=ok: legacy_cp1251, text_cases, layers_off_frozen, never_resolved, uncommon. All have `svg_check=ok, reserialized_identical=1`, with 0 forbidden elements, on/href attrs, doctype or script tags.<br>**Tests.** `TestValidateCADSVGRoundTripsSpecialCharacters` PASS, and the full suite is green. |
| 2 | **SC2 / CCONV-03.** dxf→pdf gives a valid PDF whose page holds the drawing: not empty, text extractable | ✓ VERIFIED | **Changed:** `cadPDFDecode`, `cadDXFReadSVGFacts` and `parseCADDXFTexts` now open through `OpenJobFile`. Size limits and the judge logic are unchanged.<br>**Live at 46e742c.** 3 DXF_PDF rows (legacy_cp1251, text_cases, uncommon) show `starts_pdf=1, pdfinfo_pages=1, pdf_check=ok`, result=ok. DXF_RETRY pdf (rsvg-convert SIGTERM) shows `retried=1, status=done, pdf_check=ok`. |
| 3 | **SC3 / RNDR-06 (+MEAS-01, MEAS-03).** render dxf→png is top-down with the 3D size and background rules; a non-square drawing is letterboxed without cropping; `view` on dxf is 422; the second output is `measurements` with an entity 2D bbox, no volume, units from $INSUNITS or `unspecified` | ✓ VERIFIED | **Changed:** the DXF render sidecar is written by `writeNewFile` (remove, then O_EXCL, 0600). That is the same mechanism as the deleted `cadDXFWriteNew`, now shared. `parseCADMeasureDraft` reads through `OpenJobFile`. `cadSideOutputs` reads through `ReadJobFile`, with the classification unchanged (`ErrCADMeasurementsInvalid`).<br>**Live at 46e742c.** 4 DXF_RENDER rows with `outputs=2, roles=primary@0;measurements@1`. 7 DXF_MEAS rows: wrong_extents bbox 10:20:40:30, units_1 inch, units_13 micrometre, units_0 unspecified, assume-mm cases, far_point; volume/area null with `not_applicable_2d`. 4 DXF_OPTS_REJECT rows, all 422 with `jobs_created=0`.<br>**Tests.** `TestCADDXFRenderWritesTheSidecarAsANewFile` PASS; `TestCADOutputsDXF*` (3) PASS. |
| 4 | **SC4 (+CADIN-04).** A DXF bomb ends terminally within measured bounds without OOM; XREF and IMAGE are never resolved | ✓ VERIFIED | The guard and helper are unchanged.<br>**Live at 46e742c.** 15 DXF_BOMB rows, all result=ok with `retried=0, outputs=0, oom_killed=false, restart_count=0`. Codes match the phase-75 run: cad_too_complex for the MINSERT, cyclic and nesting cases, cad_unreadable for the non-numeric ones, cad_empty_result for 3dsolids, all_hidden and single_point.<br>The DXF_SVG never_resolved row is ok. DXF_CEILING is 413 at 123731969 bytes with `jobs_created=0`. |
| 5 | **SC5 (+CADW-02).** cad-dxf and rsvg-convert each run under their own measured ruleset, recorded in SANDBOX-PATHS.md and held by a test; the preflight judges by content; the font is reachable and text does not disappear | ✓ VERIFIED | `cadsandbox.go` and SANDBOX-PATHS.md are unchanged. `conf05-crosscheck.sh cad` re-run now: all MATCH, exit 0.<br>**Sandbox liveproof at 46e742c (dirt 0).** All 9 classes ALL PASS: audio 4, av 4, cad 5, document 7, html 9, image 4, ocr 4, pandoc 4, probe 4. The html file's "landlock: restrict self: operation not permitted" line belongs to its refuses-without-landlock negative check, and the file ends ALL PASS (9 checks).<br>**Live cad SANDBOX row.** It names cad-dxf and rsvg-convert, with `sandbox_disabled=false`. |
| 6 | DERIVED constants in code equal the evidence values | ✓ VERIFIED | No constant changed. `TestCADDXFConstantsComeFromTheEvidence` and `TestCADDXFPDFTextFloorComesFromTheEvidence` PASS. The live header shows cad_dxf_timeout_s=162, cad_rsvg_timeout_s=141 and cad_engine_timeout_s=886. |
| 7 | Exposure landed in exactly one commit (bd90a59), and every earlier commit is inert | ✓ VERIFIED | The quick task did not touch `converters.go`, `internal/api/api.go` (ceilings) or `cmd/cad-worker/main.go`. The live run shows FORMATS and INSPECT rows ok and the SUMMARY counts identical to 75-10, with the same row schema line for line. |
| 8 | `internal/convert/exec.go` is byte-identical to main | ✓ VERIFIED | `git diff --quiet main HEAD -- internal/convert/exec.go` → identical. It is also unchanged since d34fbdf. |
| 9 | No code comments added; no FAIL or NOT RUN recorded as a pass | ✓ VERIFIED | **Comments.** Added lines in `*.go`, `*.sh` and `*.yml` over d34fbdf..HEAD contain no `//`, `/*` or `#` comment.<br>**FAILs are disclosed, not counted as passes.** ci-local run1 at 46e742c: gate FAIL (5m timeout), docker-build FAIL and e2e FAIL (missing gitignored tectonic bundle in the clone). Run2 at 46e742c: gate FAIL (5m), docker-build and e2e PASS. `rg-gate-46e742c`: FAIL, 5m timeout. Each failure is a `panic: test timed out after 5m0s` in internal/convert with no `--- FAIL` line.<br>All of these are reported as FAIL in SUMMARY rows 1a–1c. The pass claim rests on run3 at 4c1c1ef: helm, gate and race PASS, "ALL REQUESTED TIERS PASSED". 4c1c1ef changes only the timeout value against 46e742c. |
| 10 | The quick task's job-dir hardening (writes after an untrusted stage use remove-then-O_EXCL; reads use O_NOFOLLOW + O_NONBLOCK + an fstat regular-file check) exists, is wired at every listed site, and is exercised live | ✓ VERIFIED | **Helpers.** `jobdirfs.go` has `writeNewFile`, `OpenJobFile`, `ReadJobFile`, `statJobOutput` and `jobOutputOpenFailure`.<br>**Write sites:** 3D sidecar, DXF sidecar, both OCR PDF writes, sanitize-PDF redaction, sanitize report, OOXML verify parts.<br>**Read sites:** DXF SVG/texts/PDF/draft, 3D STL and PNG, OCR page, sanitized PDF, the libreoffice/pandoc/av/audio validators, `uploadFrom`, and the probe and cad side outputs.<br>**Tests.** The planted-link and planted-fifo tests PASS now.<br>**Live coverage of this code at 46e742c.**<br>- cad liveproof covers DXF reads and writes plus the 3D sidecar and STL.<br>- e2e (run2) covers the libreoffice, chromium, image, audio, ocr, pandoc, sanitize and tectonic validators and `uploadFrom`.<br>- The av-progress, cancel and sse liveproofs, all ok, cover the rest.<br>- The 9 sandbox liveproof classes are ALL PASS. |
| 11 | The changed retry classification preserves the old texts; an irregular entry is terminal; other open errors stay transient as before | ✓ VERIFIED | **Missing output.** `statJobOutput` keeps `os.Stat` first, so a missing output yields the exact old `<engine>: stat output: stat <path>: no such file or directory`. `requireOldMissingText` pins this per validator.<br>**Irregular entry.** It maps to the `stat output` text, which is terminal through `terminalChromiumSignatures` "stat output", `terminalAudioSignatures` and `isPandocTerminal`'s "pandoc:". For av it maps to `ErrAVOutputMissingOrEmpty`, the same sentinel as before.<br>**Other open failures.** They keep `<engine>: open output: open <path>: permission denied`, pinned by `requireOldOpenText`, which also checks that `fs.ErrPermission` stays visible. `TestValidatorFailureTextsKeepTheirClassification` pins the classes: document and html open failures transient; pandoc and av open failures terminal, as they were before via the "pandoc:" prefix and the AV sentinel.<br>**uploadFrom.** A planted primary returns `asynq.SkipRetry` wrapping `ErrJobFileIrregular`, and the runner's `errors.Is(err, asynq.SkipRetry)` branch marks the job failed. `TestUploadFromRefusesAPlantedPrimaryTerminally` asserts 1 MarkFailed for all 10 descriptors. A mode-000 primary stays retryable with the old `open output: open <path>: permission denied` text and 0 MarkFailed (`TestUploadFromOtherOpenErrorsStayTransient`, all 10 descriptors). |
| 12 | deferred-items.md has its three items resolved and no new open item | ✓ VERIFIED | It has exactly 3 items (75-07 3D sidecar, 75-07 python<3.10, 75-10 multipart leak), each followed by a `- **Resolved:** quick 261012-gos, <commits>` bullet. No other item or section was added.<br>Each resolution holds in code:<br>- `cadrender.go:126` uses `writeNewFile`, and `TestCADRenderWritesTheSidecarAsANewFile` PASS.<br>- `requireCADPython` skips below 3.10, naming the version and path.<br>- The handlers have a guarded `RemoveAll` defer, `TestMultipartSpillFilesAreRemovedWhenTheHandlerReturns` PASS, and my full-suite run added 0 multipart files. |

**Score:** 12/12 truths verified (0 present, behavior-unverified)

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `cmd/cad-worker/helper/cad-dxf`, `cad-guard`, `requirements-dxf.txt` | ezdxf helper, DXF guard, hash-locked deps | ✓ VERIFIED | Unchanged since d34fbdf; live DXF rows ok at 46e742c |
| `internal/convert/caddxf.go`, `caddxfsvg.go`, `caddxfpdf.go`, `cadpixel.go`, `cadsandbox.go` | converters, SVG allowlist, PDF judge, drawing judge, rulesets | ✓ VERIFIED | Only the open/write call sites changed (to the shared helpers); logic is unchanged |
| `internal/convert/jobdirfs.go` | shared job-dir read/write helpers | ✓ VERIFIED | 81 lines; 30 call sites outside tests across convert and worker; `jobdirfs_test.go` covers symlink, fifo, missing, empty and unopenable cases |
| `internal/worker/descriptors.go`, `worker.go` | terminal and retryable mapping; side outputs; upload | ✓ VERIFIED | Signature lists unchanged; `uploadFrom` irregular → SkipRetry; side outputs through `ReadJobFile` |
| `internal/api/handlers.go`, `inspect_handlers.go` | multipart spill removal | ✓ VERIFIED | `defer r.MultipartForm.RemoveAll()` right after `ParseMultipartForm` when the form is non-nil, on both success and error paths |
| SANDBOX-PATHS.md `## cad`, docs/MEASUREMENTS.md, ERROR-CODES.md | rulesets and docs | ✓ VERIFIED | Unchanged; conf05 MATCH |

### Key Link Verification

| From | To | Via | Status |
|------|----|-----|--------|
| `/v1/formats` and inspect | DXF converters | `Default.Register` (bd90a59) | ✓ WIRED (live FORMATS and INSPECT ok at 46e742c) |
| API create | 118 MiB ceiling | `defaultMaxFormatBytes[cad]["dxf"]` | ✓ WIRED (live DXF_CEILING 413) |
| Converter | guard → cad-dxf → rsvg-convert | `dxfDrawing` → stages under `cadStage` / `cadDrawStage` | ✓ WIRED |
| cad-dxf output | client SVG | `OpenJobFile` → `ValidateCADSVG` re-serialization to `outPath` | ✓ WIRED |
| Render | second output | `writeNewFile` sidecar → `cadSideOutputs` (`ReadJobFile`) → `AddOutputs` | ✓ WIRED (live `outputs=2`) |
| Validators / `uploadFrom` | runner classification | error text or SkipRetry → `IsTerminal` / `runner.go:294` | ✓ WIRED (tests pin each class) |
| API handlers | spilled multipart files | guarded `RemoveAll` defer | ✓ WIRED (spill test PASS; 0 new files after the full suite) |
| Worker startup | preflight | `CADPreflight` → `cadPreflightDrawing` | ✓ WIRED (live SANDBOX row) |

### Data-Flow Trace (Level 4)

| Artifact | Data | Source | Real data | Status |
|----------|------|--------|-----------|--------|
| measurements.json (dxf render) | bbox, unit | helper draft (`OpenJobFile`) → `buildCADMeasurements` → `writeNewFile` → `ReadJobFile` → `measurements.Validate` | yes (live wrong_extents, units_* rows at 46e742c) | ✓ FLOWING |
| PDF text | texts | texts.json (`OpenJobFile`) → R-3/R-10 expectation vs decoded text | yes (live DXF_PDF rows) | ✓ FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Format | `gofmt -l .` | empty | ✓ PASS |
| Vet | `GOTOOLCHAIN=go1.26.4 go vet ./...` | clean | ✓ PASS |
| Full suite | `GOTOOLCHAIN=go1.26.4 go test ./... -count=1 -timeout 10m` (python 3.14 first) | exit 0, 31 ok | ✓ PASS |
| Phase-75 and quick-task named tests | `go test -v -run ...` (list in Method) | all PASS, 0 SKIP | ✓ PASS |
| Ruleset document ↔ Go | `scripts/conf05-crosscheck.sh cad` | 6 MATCH, exit 0 | ✓ PASS |
| No multipart leak | `ls $TMPDIR \| grep multipart-` before and after | 6 → 6 (0 new) | ✓ PASS |
| Live pipeline | committed `rg-cad-liveproof.txt` (not re-run, docker excluded) | ALL PASS at 46e742c, dirt 0 | ✓ PASS (transcript) |
| ci-local | committed run3 at 4c1c1ef | helm, gate, race PASS | ✓ PASS (transcript) |
| docker-build and e2e | committed run2 at 46e742c | PASS | ✓ PASS (transcript) |

### Probe Execution

Step 7c: neither the phase nor the quick task declares a `scripts/*/tests/probe-*.sh`. The runnable proofs are the live and ci-local scripts. Their transcripts are cited above and were not re-run, because docker is excluded from this verification.

### Requirements Coverage

| Requirement | Source Plan | Status | Evidence |
|-------------|-------------|--------|----------|
| RNDR-06 | 75-01..09 | ✓ SATISFIED | Truth 3 |
| CCONV-02 | 75-01..10 | ✓ SATISFIED | Truth 1 |
| CCONV-03 | 75-01..10 | ✓ SATISFIED | Truth 2 |
| MEAS-01 (extends) | roadmap SC3 | ✓ SATISFIED | live `outputs=2` via AddOutputs |
| MEAS-03 (extends) | roadmap SC3 | ✓ SATISFIED | `$INSUNITS` table, 2D bbox, no volume |
| CADIN-04 (extends) | roadmap SC4 | ✓ SATISFIED | Truth 4 |
| CADW-02 (extends) | roadmap SC5 | ✓ SATISFIED | Truth 5 |

No orphaned requirements: REQUIREMENTS.md maps only RNDR-06, CCONV-02 and CCONV-03 to phase 75. Their traceability rows still read Pending; the orchestrator owns that update.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| cmd/cad-worker/helper/cad-dxf | 16, 238, 277, 374 | `TEXT_AS_OUTLINE = False` test seam | ℹ️ Info | Carried from the d34fbdf report; unchanged |
| internal/worker/worker.go | 1608-1615 | `uploadFrom`'s fstat-after-open failure now reads `open output: ...` instead of `stat output: ...` | ℹ️ Info | The old text matched the "stat output" terminal signature for the document and html engines, and the new text is transient. This only triggers if fstat fails on an fd that is already open (EIO-class). That is practically unreachable and not a planted-entry case. It is not covered by a test and not called out in the SUMMARY |
| internal/convert/ocr.go | 193 | `validateOCROutput` still uses `os.Stat` (follows links) | ℹ️ Info | It only stats and reads no content. The OCR PDF path writes with `writeNewFile`. Out of the quick task's list and not a regression |
| — | — | No TBD, FIXME or XXX added; no stubs | — | — |

### Known accepted limits (phase 76 docs, not gaps)

- Kerning R-1.
- U+FFFD outside DejaVu R-9.
- PDF text floor R-10.
- Sub-pixel features on render R-11.
- Go-side RSS gap `GAP go_worker_rss_dxf`.

### Out-of-scope findings (recorded for the orchestrator, do not affect this phase's status)

- **ℹ️ 6 leftover `multipart-*` files (63, 118 and 135 MiB, two each) in `$TMPDIR` from 15:42 MSK.** They match the pre-fix b2c0db8 gate clone run. My runs added none, and no process holds them. They can be deleted.
- **ℹ️ internal/convert takes about 300 s and now needs the 10m gate timeout** (`rg-convert-timing.txt`: 271 s at b2c0db8, 295 s at 46e742c). The SUMMARY discloses this. The race tier was already at 10m.
- **ℹ️ The cad-dxf and rsvg-convert rulesets are still measured on arm64 only.** This is unchanged.

### Human Verification Required

None. Every success criterion has live transcript evidence at 46e742c, which is identical in source to HEAD, plus unit tests that pass on the current tree.

### Gaps Summary

None. The quick task's changes keep the phase goal intact:
- **Phase 75 still holds.** Every SC1–SC5 row in the 46e742c live proof is result=ok, and the run reports ALL PASS. All 9 sandbox classes pass. ci-local helm, gate and race pass at 4c1c1ef.
- **The new helpers sit on the paths phase 75 uses.** They replace the DXF-only `cadDXFWriteNew` and the O_NOFOLLOW opens; nothing else on those paths changed.
- **Retry classification is unchanged where it matters.** The tests pin the old texts and classes, plus the new terminal refusal of a planted primary.
- exec.go matches main, no comments were added, and all three deferred items are resolved.

---

_Verified: 2026-10-12T14:09:22Z_
_Verifier: Claude (gsd-verifier)_
