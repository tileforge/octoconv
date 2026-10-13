---
phase: 75-dxf-2d-pipeline
plan: 06
subsystem: cad-worker Go plumbing (DXF)
tags: [dxf, opts, measurements, constants, classification, sandbox, timeouts, config]
status: complete

requires:
  - phase: 75-dxf-2d-pipeline
    provides: 75-02 cad-dxf verdict and measure draft; 75-03 cad-guard DXF mode and cad_dxf_nesting_bound; 75-05 DERIVED bounds and CADDXFPDFTextFloorPt
provides:
  - "ValidateCADApplicability DXF rules: view and quality refused on a dxf render, every option refused on a dxf convert"
  - "cadUnitFor dxf branch (cadUnitForDXF): drawing units, factor exactly 1, $INSUNITS names declared, raw codes unspecified and not fillable"
  - "docs/MEASUREMENTS.md ## DXF drawings: bbox source, isolated points, XLINE and RAY, $INSUNITS table, kerning limit"
  - "cad.go: CADMaxDXFBytes, CADMaxDXFExpandedInserts, CADMaxDXFExpandedEntities, CADMaxDXFNesting, CADMaxDXFSVGBytes, CADMaxDXFPDFBytes, CADDefaultDXFTimeout, CADDefaultRsvgTimeout, cadStageDXF, cadStageRsvg, ErrCADDXFTimeout, ErrCADRsvgTimeout, ErrCADDrawingFailed, ErrCADNothingDrawable, cadGuardArgs, classifyCADRsvg; CADConverter.DXFTimeout and RsvgTimeout; DXF verdict fields on cadVerdict"
  - "cadsandbox.go: cadDXFReadOnly, cadDXFEngines, cadRsvgReadOnly, cadRsvgEngines, cadDrawStage, cadPrivateDir; ValidateCADTimeouts and ConfinedCAD with dxf and rsvg"
  - "CAD_DXF_TIMEOUT=162s and CAD_RSVG_TIMEOUT=141s read by cad-worker and set in compose, the chart and .env.example"
affects: [75-07, 75-08, 75-09, 76]

tech-stack:
  added: []
  patterns:
    - "Every DXF constant is pinned by a test that parses the DERIVED line of its evidence file and names the script to re-run"
    - "Draw stages get engineEnv minus HOME plus a per-job HOME/XDG_CACHE_HOME/XDG_CONFIG_HOME under jobDir/.home, created 0700 and refused if it is not a real directory"
    - "Two independent stage sums against CAD_ENGINE_TIMEOUT: guard+mesh+render+60 and guard+dxf+rsvg+60"

key-files:
  created:
    - .planning/phases/75-dxf-2d-pipeline/75-06-SUMMARY.md
  modified:
    - internal/convert/cadopts.go
    - internal/convert/cadopts_test.go
    - internal/convert/cadmeasure.go
    - internal/convert/cadmeasure_test.go
    - docs/MEASUREMENTS.md
    - internal/convert/cad.go
    - internal/convert/cad_test.go
    - internal/convert/cadsandbox.go
    - internal/convert/cadsandbox_test.go
    - cmd/cad-worker/main.go
    - docker-compose.yml
    - deploy/chart/octoconv/templates/configmap.yaml
    - .env.example

key-decisions:
  - "75-06: a dxf draft whose declared_unit is outside the unit table is a draft error, not unspecified; the helper only ever writes vocabulary names, so anything else is a helper contract breach"
  - "75-06: cadDrawStage refuses a .home, .cache or .config that is not a real 0700 directory, so a symlink planted by an earlier confined stage cannot redirect the worker's mkdir"
  - "75-06: on stage dxf an ok verdict with a non-zero exit, an unknown verdict and a non-SIGKILL signal are all ErrCADDrawingFailed (retryable); SIGKILL stays ErrCADEngineKilled"

requirements-completed: []

coverage:
  - id: D1
    description: "D-07 option contract on dxf: view/quality refused on render, any option refused on convert to svg or pdf, size/background/assume_unit accepted on render, 3D rows unchanged"
    requirement: RNDR-06
    verification:
      - kind: unit
        ref: "internal/convert/cadopts_test.go TestValidateCADApplicability (19 DXF rows added)"
        status: pass
    human_judgment: false
  - id: D2
    description: "D-10/D-11 dxf measurements: drawing units, $INSUNITS names declared, 0/missing fillable by assume_unit, raw codes not fillable, refusals wrap ErrCADMeasure; real 75-02 drafts build"
    requirement: RNDR-06
    verification:
      - kind: unit
        ref: "internal/convert/cadmeasure_test.go TestCADUnitDXF, TestBuildCADMeasurementsDXFRefusals, TestBuildCADMeasurementsDXFRealDrafts"
        status: pass
      - kind: unit
        ref: "internal/measurements doc-sync tests (vocabulary unchanged, git diff main -- internal/measurements empty)"
        status: pass
    human_judgment: false
  - id: D3
    description: "R-5/R-7: every DXF constant equals its DERIVED line; the pin fails on a mutated value"
    requirement: CCONV-02
    verification:
      - kind: unit
        ref: "internal/convert/cad_test.go TestCADDXFConstantsComeFromTheEvidence (mutation run with 140 and 17 failed naming both evidence files)"
        status: pass
    human_judgment: false
  - id: D4
    description: "D-12/D-13: guard DXF argv, dxf-stage and rsvg classification, 3D classes unchanged, sandbox fault first"
    requirement: CCONV-03
    verification:
      - kind: unit
        ref: "TestCADDXFGuardArgv (stub argv and the real cad-guard accepting it), TestCADDXFStageClassification, TestCADDXFStageLeavesTheOtherStagesClassesAlone, TestCADRsvgClassification"
        status: pass
    human_judgment: false
  - id: D5
    description: "D-14/R-5: job-local HOME and XDG dirs, timeouts through config, both stage sums checked against the engine timeout"
    requirement: CCONV-02
    verification:
      - kind: unit
        ref: "TestCADDrawStage* (4), TestValidateCADTimeouts, TestConfinedCADCarriesTheDXFTimeoutsAndRefusesAnOverBudgetSum, TestCADSetsDrawSetsAreTheSeeds"
        status: pass
      - kind: integration
        ref: "scripts/ci-helm-checks.sh rc 0; config grep gate CONFIG-OK"
        status: pass
    human_judgment: false

actuals:
  tokens: 15653
  tasks: 3
  commits: 3
plan_head_before: 4d3eb51232b7bb267ce7d2144073441b02b8a7d4
plan_head_after: 4ae52b35231cb397096ff36ea99ff09253c9620f

duration: about 17 min (2026-10-11 06:43Z to 07:00Z)
completed: 2026-10-11
---

# Phase 75 Plan 06: DXF Go plumbing Summary

**This plan adds the DXF option contract, the dxf measurements branch and its documentation, the DXF constants (pinned to the 75-05 and 75-03 DERIVED lines), the guard's DXF argv, and the dxf and rsvg failure classes. It also adds a draw-stage builder that gives each job its own HOME, and passes CAD_DXF_TIMEOUT=162s and CAD_RSVG_TIMEOUT=141s through the worker, compose, the chart and .env.example. None of it is reachable yet: no dxf pair is registered, and the API and worker are unchanged.**

## Performance

- **Duration:** about 17 min (06:43Z to 07:00Z)
- **Tasks:** 3 of 3
- **Commits:** 3 task commits since 4d3eb51, all SSH-signed (G)

## Accomplishments

- **D-07 options.** Three new refusals, all 422 `opts_not_applicable` once exposed:
  - `view does not apply to a dxf source`;
  - `quality does not apply to a dxf source`;
  - `no option applies to a dxf conversion`.

  The source is normalized, so `DXF` and `.dxf` behave like `dxf`. Every 3D row passes unchanged.
- **D-10/D-11 measurements.**
  - **Factor and units.** A dxf draft must carry `applied_factor` exactly 1. Lengths stay in drawing units. A declared `$INSUNITS` name is `declared`. `0` or missing is `unspecified`/`none` unless `assume_unit` fills it as `client_assumed`. A raw code outside the vocabulary (`units_3`) stays `unspecified`/`none`, and `assume_unit` does not apply to it.
  - **Refusals, all wrapping ErrCADMeasure:**
    - solid counts;
    - both a name and a raw code;
    - a null factor or a factor other than 1;
    - a volume;
    - a 3D or STL reason;
    - three coordinates per corner.
  - **Real drafts.** The 75-02 drafts `wrong_extents` (bbox 10,20 to 40,30, mm declared) and `units_3` build.
- **MEASUREMENTS.md** gains `## DXF drawings`:
  - the drawn-primitives-plus-glyph-box bbox (never `$EXTMIN`/`$EXTMAX`, never `bbox.extents`);
  - XLINE and RAY drawn but excluded from the bbox;
  - isolated points included (R-4);
  - the `$INSUNITS` table;
  - the R-1 kerning limit (up to 3.3% narrower).

  The Units table notes now give `$INSUNITS` codes for `cm`, `foot` and `micrometre`, and the full `unspecified` rule. No vocabulary column changed, and the doc-sync tests pass.
- **Constants (R-5, R-7).** Each value comes from a DERIVED line:

  | Constant | Value | Evidence key |
  |---|---|---|
  | CADMaxDXFBytes | 118 MiB = 123731968 | cad_dxf_ceiling_bytes |
  | CADMaxDXFExpandedInserts | 170031 | cad_dxf_expanded_inserts_cap |
  | CADMaxDXFExpandedEntities | 850155 | cad_dxf_expanded_entities_cap |
  | CADMaxDXFNesting | 16 | cad_dxf_nesting_bound (75-03) |
  | CADMaxDXFSVGBytes | 129 MiB = 135266304 | cad_dxf_svg_max_bytes |
  | CADMaxDXFPDFBytes | 19 MiB = 19922944 | cad_dxf_pdf_max_bytes |
  | CADDefaultDXFTimeout | 162 s | cad_dxf_timeout_s |
  | CADDefaultRsvgTimeout | 141 s | cad_rsvg_timeout_s |

  `TestCADDXFConstantsComeFromTheEvidence` parses both evidence files. I mutated two constants (rsvg 140, nesting 17) and the test failed on both, naming the evidence file and the script to re-run. I then restored them. `cadMaxBytes("dxf")` is the DXF ceiling.
- **Guard argv.** For dxf the guard gets `--format dxf --max-expanded-inserts 170031 --max-expanded-entities 850155 --max-nesting 16 --max-abs-coordinate 1e+12 <src>`. The real `cad-guard` accepts exactly this argv on a corpus fixture. The step argv is unchanged.
- **D-13 classes, stage dxf:**

  | Outcome | Class |
  |---|---|
  | `ok` with exit 0 | accepted |
  | `empty` | ErrCADNothingDrawable and ErrCADEmptyResult |
  | `too_complex` | ErrCADTooComplex |
  | `unreadable` | ErrCADUnreadable |
  | `failed` | ErrCADDrawingFailed |
  | stage deadline | ErrCADDXFTimeout, `cad dxf: … no result within …, engine cpu …` |
  | SIGKILL | ErrCADEngineKilled |
  | any other signal, a non-zero exit without a verdict, exit 0 without a verdict | ErrCADDrawingFailed |

  ErrEngineSandboxUnavailable is checked first and carries no CAD sentinel. The guard, mesh and render classes are unchanged, and a test proves it.
- **classifyCADRsvg:**
  - clean exit with a non-empty output: nil;
  - stage deadline: ErrCADRsvgTimeout naming the rsvg stage;
  - SIGKILL: ErrCADEngineKilled;
  - another signal, a non-zero exit, or a clean exit with an empty or missing output: ErrCADDrawingFailed;
  - cancelled parent: cancellation.
- **D-14 HOME.** `cadDrawStage` creates `jobDir/.home`, `.home/.cache` and `.home/.config` at 0700.
  - **Environment:** `engineEnv(jobDir)` without its HOME, plus HOME, XDG_CACHE_HOME and XDG_CONFIG_HOME under `.home`. That is exactly seven entries, with no LP_NUM_THREADS.
  - **Proof:** a fake engine run through the builder sees the job-local HOME and not the worker's DATABASE_URL.
  - **Multiarch:** the directory is appended only when the sandbox confines.
  - `engineEnv` and `exec.go` are unchanged.
- **R-5 config.**
  - `ValidateCADTimeouts(guard, mesh, render, dxf, rsvg, engine)` refuses a non-positive CAD_DXF_TIMEOUT or CAD_RSVG_TIMEOUT. It also refuses guard + dxf + rsvg + 60 s above CAD_ENGINE_TIMEOUT, naming both variables and the engine. The 3D sum check is kept.
  - The defaults give 30 + 162 + 141 + 60 = 393 s ≤ 886 s.
  - `ConfinedCAD` carries both values, and concurrency other than 1 is still refused.
  - `cmd/cad-worker` reads both with `envDuration`.
  - Compose (cad-worker only), the chart configmap and .env.example set 162s and 141s. The .env.example paragraph gives the derivation and notes that the chart's 100 MiB MAX_UPLOAD_BYTES applies first.

## Task Commits

1. **Task 1: dxf option contract and dxf measurements, documented:** `d60e721` (feat)
2. **Task 2: DXF constants pinned to the evidence, guard DXF argv, stage classes:** `1153436` (feat)
3. **Task 3: confined draw stages with a job-local HOME, the timeouts through config:** `4ae52b3` (feat)

## Decisions Made

- **A declared unit outside the table is a draft error.** In a dxf draft, a declared_unit outside the unit table is refused, not reported `unspecified`. The helper writes only vocabulary names, so any other value is a contract breach, not a file property.
- **Retryable outcomes on stage dxf.** An ok verdict with a non-zero exit, an unknown verdict word and a non-SIGKILL signal are all ErrCADDrawingFailed (retryable, D-13 "crash on validated input"). SIGKILL stays ErrCADEngineKilled, as in 74.
- **Extra verdict fields.** cadVerdict decodes `render_substituted` and `hatch_truncated` in addition to the plan's fields, because the 75-02 helper emits them and 75-07 needs them.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Security] The stage home must be a real directory**
- **Found during:** Task 3.
- **Issue:** cad-dxf runs with write access to the job dir. A `.home` it replaced with a symlink would make the unconfined worker's mkdir and later stages follow it out of the job dir.
- **Fix:** `cadPrivateDir` creates each directory with `os.Mkdir(0700)`, then `Lstat`s it. It refuses anything that is not a real 0700 directory. There is no chmod through a symlink.
- **Test:** `TestCADDrawStageRefusesASymlinkedHome`.
- **Files modified:** internal/convert/cadsandbox.go and internal/convert/cadsandbox_test.go.
- **Commit:** 4ae52b3.

**2. [Rule 1 - Accuracy] The .env.example ceiling sentence**
- **Found during:** Task 3.
- **Issue:** The first draft said the DXF ceiling is "enforced per format at the API and again in the worker". Neither is true until plan 75-09 registers the pair and adds `defaultMaxFormatBytes[cad]["dxf"]`.
- **Fix:** The sentence now names only the constant and the chart's 100 MiB upload cap.
- **Files modified:** .env.example.
- **Commit:** 4ae52b3.

### Additions within the plan's intent

- **Task 2:** a subtest runs the real `cad-guard` with `cadGuardArgs("dxf", …)`. This checks the argv against the parser itself, not only against a stub, so `1e+12` is checked as Python parses it.
- **Task 2:** `cadSentinels` in cad_test.go now includes the four new sentinels. Every existing "no CAD sentinel" assertion therefore also proves that none of the DXF sentinels leaks into the 3D paths.

### TDD note (not a pass claimed)

- **Tasks 1 and 2:** the RED runs were observed. In Task 1, TestCADUnitDXF, the real-draft test and TestValidateCADApplicability failed. In Task 2, the build failed on the undefined symbols.
- **Task 3:** I wrote the tests and the implementation in the same step. The first run failed to compile, but on my own test-literal typo, not on the missing symbols. So no RED run was observed for Task 3. The tests do assert the new behaviour: the new symbols and the six-argument signatures do not exist at 1153436.

## Issues Encountered

None blocking. ErrCADDXFTimeout, ErrCADRsvgTimeout, ErrCADDrawingFailed and ErrCADNothingDrawable are not yet known to `internal/worker` (isCADTerminal, cadFailureCode). Plan 75-07 owns that change, and this plan's scope rule forbids a diff in `internal/worker`. Nothing can produce these errors until 75-07 adds the stage runners.

## Known Stubs

None.

## Threat Flags

None. There is no new endpoint, registration or wire code. T-75-22/T-75-23 are mitigated by the job-local HOME/XDG directories, with the symlink refusal added. T-75-21 is mitigated by the terminal-vs-retryable classes pinned in tests. T-75-SC: no package was added.

## Verification

- **Gates:**
  - Task 1: `GATE1-OK` (12 passing option and measurement tests; measurements package ok; MEASUREMENTS.md has `$INSUNITS` and the kerning limit).
  - Task 2: `GATE2-OK` (12 passing TestCADDXF* tests, 7 of them new and 5 from 75-04 and 75-05; 1 TestCADRsvg*; no FAIL among the CAD tests).
  - Task 3: `GATE3-GO-OK` (4 passing TestCADDrawStage* tests), `CONFIG-OK`, `scripts/ci-helm-checks.sh` rc 0.
- **Acceptance:**
  - `TestCADPinnedConstantsComeFromTheEvidence` and `TestCADDXFConstantsComeFromTheEvidence` pass.
  - `TestDefaultRegistersOnlyAllowlistedCADPairs` passes unchanged.
  - `grep -c CADDXF` gives 0 for both cmd/cad-worker/main.go and internal/convert/converters.go.
  - No `//` line was added in any Go diff.
  - `git diff --exit-code main -- internal/convert/exec.go internal/convert/sandboxwrap.go internal/api internal/worker internal/measurements` is empty.
- **Whole repo:** `GOTOOLCHAIN=go1.26.4 go vet ./...` is clean, and `go test ./... -count=1` gives rc 0 with 31 packages ok. gofmt is clean.
- **State:**
  - STATE.md, ROADMAP.md and config.json are not staged or modified by this plan.
  - The compose stack was not started.
  - No image was built or tagged.

## Self-Check: PASSED

- **Files:** every modified file listed above exists, including this SUMMARY.
- **Commits:** d60e721, 1153436 and 4ae52b3 are ancestors of HEAD and SSH-signed (G).
- **Count:** the measured count since 4d3eb51 is 3 before this SUMMARY's own commit.
