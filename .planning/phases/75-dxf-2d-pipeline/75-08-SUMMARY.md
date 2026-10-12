---
phase: 75-dxf-2d-pipeline
plan: 08
subsystem: cad-worker confinement and judges (DXF)
tags: [landlock, dxf, rsvg-convert, fonts, conf05, preflight, pixel-judge, r-11, live-gate]
status: complete

requires:
  - phase: 75-dxf-2d-pipeline
    provides: 75-06 seed sets, cadDrawStage and job-local HOME; 75-07 dxfDrawing, dxfStage, rsvgStage and the production argv; 75-04 JudgeCADDrawingPNG, cad-svg-check, cad-pdf-check, cad-pixel-judge; 75-05 JudgeCADDXFPDF (R-10)
provides:
  - "scripts/cad-worker-sandbox-measure.sh --stages: dxf and rsvg stages, guard DXF inputs, font negative controls, NEVER_RESOLVED trace; the 74-06 default unchanged"
  - "scripts/cad-dxf-probe.sh judge: Go drawing judge against 71 pixelcheck with pre-registered classes E1, E2, E2', E3"
  - "R-11: JudgeCADDrawingPNG J1 is at least one ink pixel; J0, J2, J3 and the 3D judge unchanged"
  - "SANDBOX-PATHS.md conf05 blocks caddxf and cadrsvg; conf05-crosscheck.sh classes caddxf and cadrsvg, cad expands to five"
  - "CADPreflight proves cad-dxf and rsvg-convert both ways on every start; cadDXFPDFArgs and cadDXFFrameArgs shared by the converters and the preflight"
affects: [75-09, 75-10, 76]

tech-stack:
  added: []
  patterns:
    - "Draw-stage measurements use the Go-built environment (job-local HOME and XDG dirs, LANG unset), not HOME=/job"
    - "A font is proven load-bearing by content: removing /usr/share/fonts must turn every text input into failed/font_unavailable (cad-dxf) or an empty decoded PDF text (rsvg-convert)"
    - "Reference non-resolution is checked on a raw strace of every file syscall with all results, with a positive control (opens of the input)"
    - "Pre-registered divergence classes are committed before the calibration run that uses them"

key-files:
  created:
    - .planning/phases/75-dxf-2d-pipeline/evidence/75-08-landlock.txt
    - .planning/phases/75-dxf-2d-pipeline/evidence/75-08-judge.txt
    - .planning/phases/75-dxf-2d-pipeline/evidence/75-08-sandbox-liveproof.txt
  modified:
    - scripts/cad-worker-sandbox-measure.sh
    - scripts/cad-dxf-probe.sh
    - scripts/conf05-crosscheck.sh
    - .planning/research/SANDBOX-PATHS.md
    - internal/convert/cadpixel.go
    - internal/convert/cadpixel_test.go
    - internal/convert/cadsandbox.go
    - internal/convert/cadsandbox_test.go
    - internal/convert/caddxf.go
    - cmd/cad-worker/main.go

key-decisions:
  - "75-08: the measured cad-dxf and rsvg-convert sets equal the 75-06 seeds; the guard set is unchanged after 53 DXF inputs"
  - "75-08: R-11 (orchestrator, after the far_point STOP) makes drawing-mode J1 absolute; far_point at 256 opaque is the one pre-registered E2' frame, rejected by both judges on span"
  - "75-08: the preflight judges the fixture PDF with JudgeCADDXFPDF (R-10) and also with JudgeCADPDF on the expected text, and refuses a fixture text below the render floor, so the font proof cannot pass vacuously"
  - "75-08: cadPreflightTimeout stays 60 s; the measured preflight wall is 4 s"

requirements-completed: [CCONV-02, CCONV-03, RNDR-06]

coverage:
  - id: D1
    description: "cad-dxf and rsvg-convert sets measured on the production image by trace, removal table, effect rows and a font negative control; guard re-measured with DXF inputs"
    requirement: CCONV-02
    verification:
      - kind: integration
        ref: "scripts/cad-worker-sandbox-measure.sh --stages guard,dxf,rsvg (evidence/75-08-landlock.txt) and --recheck (RECHECK-OK, 12 cases)"
        status: pass
    human_judgment: false
  - id: D2
    description: "References named by never_resolved.dxf are never opened by the helper; the confined run equals the unconfined one"
    requirement: CCONV-03
    verification:
      - kind: integration
        ref: "NEVER_RESOLVED,trace_hits=0 in evidence/75-08-landlock.txt"
        status: pass
    human_judgment: false
  - id: D3
    description: "Sets recorded once in SANDBOX-PATHS.md and held equal to the Go vars"
    requirement: CCONV-02
    verification:
      - kind: integration
        ref: "scripts/conf05-crosscheck.sh cad, caddxf, cadrsvg and the other classes; mutation of each new readonly block gives rc=1"
        status: pass
      - kind: unit
        ref: "TestCADDrawSetsAreTheMeasuredOnes, TestCADSetsGrantNoSharedWritableOrProcessPath, TestCADDXFHelperImportsNoOpenCascadeOrVTK"
        status: pass
    human_judgment: false
  - id: D4
    description: "Go drawing judge calibrated against 71 pixelcheck under R-11"
    requirement: RNDR-06
    verification:
      - kind: integration
        ref: "scripts/cad-dxf-probe.sh judge (evidence/75-08-judge.txt, JUDGE_SUMMARY result=ok, 0 unexplained)"
        status: pass
      - kind: unit
        ref: "TestJudgeCADDrawingPNGRejects (single_ink_pixel_wrong_span J2, blank J1), TestJudgeCADDrawingPNGAcceptsAnySpanningInkHoweverSparse"
        status: pass
    human_judgment: false
  - id: D5
    description: "Preflight proves cad-dxf and rsvg-convert both ways on every start, live on the final image"
    requirement: CCONV-03
    verification:
      - kind: unit
        ref: "TestCADPreflight* (stub helpers: missing text, fontless PDF, blank frame, guard too_complex, outside leaks, sandbox-unavailable outside runs, covering grants)"
        status: pass
      - kind: integration
        ref: "scripts/sandbox-liveproof.sh cad (evidence/75-08-sandbox-liveproof.txt, ALL PASS (5 checks))"
        status: pass
    human_judgment: false

actuals:
  tokens: 17505
  tasks: 3
  commits: 7
plan_head_before: 53b613e43a01cdfa3c65265b4ad06a2f166fd31e
plan_head_after: ae295c6

duration: "about 7 h 15 min of wall time (2026-10-11 19:43Z to 2026-10-12 03:00Z), including a STOP for R-11, a docker daemon stall and two disk halts"
completed: 2026-10-12
---

# Phase 75 Plan 08: DXF rulesets, drawing judge and preflight Summary

**What this plan establishes:**
- **Rulesets.** cad-dxf and rsvg-convert run only under sets measured on the shipped image, and fonts are proven necessary there.
- **References.** No reference a DXF names is ever opened.
- **Record.** SANDBOX-PATHS.md holds the sets once, and the crosscheck holds the Go vars equal to them.
- **Judge.** The Go drawing judge is calibrated against the phase-71 judge under R-11.
- **Startup proof.** Every cad-worker start proves both new stages both ways. The live gate on the final image ends ALL PASS, including the emulated amd64 refusal.

## Task Commits

All are SSH-signed (G).

1. **R-11 judge change:** `e88422f` fix. Drawing-mode J1 is now "at least one ink pixel". The tests were written first and three of them failed (RED).
2. **Task 1 scripts:** `8134c87` feat. The measurement stages and the judge mode, with E2' registered before the run.
3. **Task 1 evidence:** `3d97a73` test. Contains 75-08-landlock.txt and 75-08-judge.txt.
4. **Task 2:** `c8c98c4` feat. SANDBOX-PATHS.md, the crosscheck classes and the Go pins.
5. **Task 3 code:** `3d0761e` feat. The preflight DXF proof, the shared rsvg argv builders and the proven line.
6. **Task 3 evidence:** `ae295c6` test. The liveproof transcript.

The measured count since 53b613e is 7, which includes the coordinator's `d04c99d` (R-11 in 75-CONTEXT.md).

## Accomplishments

### Rulesets (D-14, SC5)

Image `sha256:687fe04a…`: Python 3.11.2, ezdxf 1.4.4, librsvg2-bin 2.54.7+dfsg-1~deb12u1 and fonts-dejavu-core 2.37-6, native arm64. The evidence header gives commit 8134c87, and its tracked dirt is config.json only.

| Stage | Inputs | Measured read-only set (plus multiarch and its own exec grant) |
|---|---|---|
| guard | 63 (10 STEP/IGES + 53 DXF) | `/usr/bin/python3.11`, `/usr/lib/python3.11`; unchanged |
| cad-dxf | 10 | `/opt/venv`, `/usr/bin/python3.11`, `/usr/lib/python3.11`, `/usr/share/fonts` |
| rsvg-convert | 30 (10 SVGs × pdf, png 1280 opaque, png 2048 transparent) | `/usr/share/fonts` |

- **Removable entries.** The removal table drops every /proc, /sys, /etc/fonts, fontconfig and cache entry from both new sets. /usr/share/fonts is load-bearing for both.
- **Font negative controls.** All 10 text inputs of cad-dxf give `failed/font_unavailable` and no SVG. All 10 PDFs from rsvg-convert decode to empty text.
- **NEVER_RESOLVED.** `trace_hits=0` over 3830 traced calls, with 4 opens of the input. The helper skipped IMAGE ×2, OLE2FRAME, PDFUNDERLAY and XREF. The confined run is identical to the unconfined one.
- **Totals.** 206 of 206 sufficiency checks pass, 7 of 7 EFFECT rows are denied, 3 negative controls fail as expected, there are 31 removal rows and none of the sets holds /proc, /sys or /tmp. `--recheck` ends with RECHECK-OK.

### Drawing judge (D-09, R-11)

There are 79 frames: 13 drawings × 3 sizes × 2 backgrounds, plus a blank one.

| Result | Count |
|---|---|
| Agreements | 30 |
| E1 (256 opaque hairlines, pixelcheck rejects on dark only) | 7 |
| E2 (far_point 1280 and 2048 opaque, pixelcheck rejects on frac and dark, Go accepts) | 2 |
| E2' (far_point 256 opaque, both reject, Go on J2: 1 px span of 241) | 1 |
| E3 (transparent frames, which pixelcheck flattens) | 39 |
| Unexplained | 0 |

- 77 of 78 real frames pass in Go.
- Twins match 38 of 38 within 2 px. The far_point 256 pair is recorded as uncompared, because its opaque frame is the E2' frame.
- The blank 1280 frame is rejected by both judges.

### Record and crosscheck

- **SANDBOX-PATHS.md** has a new `## cad` subsection in Russian. It covers the image, the input matrix, the argv, the job-local HOME, both removal tables, the effect rows, the font control, the never-resolved trace, the guard's DXF re-measurement and amd64 being deferred.
- **conf05 blocks.** It also has the four blocks `caddxf`/`cadrsvg` × `readonly`/`engines`, each with exactly one open and one close marker.
- **Crosscheck.** `conf05-crosscheck.sh cad` checks five classes plus the multiarch map. Mutating either new readonly block gives rc=1 for `cad` and for that class, while the other classes stay 0.

### Preflight (D-15)

`CADPreflight` places the embedded `legacy_cp1251.dxf` inside and outside the job directory, then:
1. the guard in dxf mode must accept the inside copy;
2. the production `dxfDrawing` must give texts of exactly `Деталь`, every character covered;
3. rsvg-convert's PDF must pass `JudgeCADDXFPDF` with no text below the render floor, and `JudgeCADPDF` on the expected text;
4. a 256 px letterboxed frame must pass `JudgeCADDrawingPNG`;
5. cad-dxf on the outside copy and rsvg-convert on an outside SVG must each be denied and leave no output. A sandbox that could not be established is never taken as a denial.

`requireOutsideGrants` includes both new sets. In the live run the preflight wall is 4 s (02:54:26 to 02:54:30), so the 60 s timeout is unchanged.

## Deviations from Plan

1. **[R-11, orchestrator decision] The judge threshold changed after a STOP.**
   - **The STOP.** The first judge run (all at 256, 1280 and 2048, opaque and transparent) found that the Go judge rejected all six far_point frames on the old J1 floor (frac ≥ 0.0003). The plan's stop rule fired, and I reported without touching the threshold.
   - **The decision.** The coordinator recorded R-11 (`d04c99d`): J1 in drawing mode only becomes "at least one ink pixel". I implemented it test-first (`e88422f`) and committed E2' before the new judge run.
2. **[R-9/R-10 adaptation] The production argv and the PDF judge differ from the plan text.**
   - **Measurement argv.** It is the 75-07 production one, including `--render-svg JOB/draw.render.svg`. The dxf signature also covers the render-only SVG.
   - **PDF judge.** The preflight uses `JudgeCADDXFPDF` (R-10), as the plan's binding context requires, and also `JudgeCADPDF` with the expected text, as the plan text says.
3. **[Rule 3] The measurement script needed fixes to run at all.**
   - The 74-06 build context lacked `requirements-dxf.txt` and `cad-dxf`. The current Dockerfile needs both, so the context now copies every requirements file and helper.
   - Guard inputs that the guard rejects are traced through `sh -c '"$@"; exit 0'`. Without it, the shared trace script stops on the engine's non-zero exit. 15 fixtures are traced this way, and they are judged by their verdict like every other input.
4. **[Rule 1] My own bugs, found and fixed before any evidence was used.**
   - Duplicate guard labels: a STEP and a DXF input were both called `legacy_cp1251`. DXF guard labels are now prefixed, and a duplicate check was added.
   - Two broken Python string literals in the judge classifier.
5. **[Fidelity] Shared rsvg argv builders.** `cadDXFPDFArgs` and `cadDXFFrameArgs` were extracted in caddxf.go, which is not in the plan's file list, so the preflight uses the converters' exact argv. The 75-07 argv tests (frames.tsv) still pass.
6. **[Test robustness] Engine matching in the sandbox-unavailable wrapper.** The wrapper matches on the engine basename after `--`, not on substrings of temp paths. A mutation that skips `requireCADDenied` fails both subtests.

## Issues Encountered (prior attempts, recorded and not counted as passes)

- **Landlock measurement.**
  - One run, on a freshly cold-built image, stopped at `FAIL,stage=guard,input=dxf_text_cases,reason=the trace recorded no path`. A manual trace of the same input on the same image saw the paths, and the failure did not repeat.
  - One run was stopped by the VM disk watchdog (below 6 GiB) during the REF phase.
  - One run hung in a `docker run` while the OrbStack daemon stopped answering; the VM disk was at 99%.
  - The committed transcript is the clean run that came after these. SANDBOX-PATHS.md mentions the first two.
- **Live gate.** Two attempts were stopped by the 6 GiB watchdog during the emulated amd64 build. In both, the four arm64 checks passed and `emulated-amd64-refuses` did not run.
  - Attempt 1 dropped to 5.51 GiB.
  - Attempt 2 dropped to 5.99 GiB, after the arm64 image had been released and the build cache pruned.
  - After the user freed disk, the third attempt ended `ALL PASS (5 checks)` with 16.0 GiB free at its lowest point.
- **Host python.** `go test` needs python 3.10 or later first on PATH (`/opt/homebrew/opt/python@3.14/libexec/bin`), as noted in deferred-items.md.

## Verification

- **Task 1:**
  - `bash -n` passes on both scripts.
  - The awk gate on 75-08-landlock.txt finds 3 MEASURED rows, 2 font controls `failed_as_expected`, 4 denied dxf/rsvg EFFECT rows and `NEVER_RESOLVED,trace_hits=0`, and no SUFFICIENT row with `different`.
  - The recheck ends RECHECK-OK.
  - The judge summary reads `unexplained_divergence=0` and `result=ok`.
  - HDR lines name the commit, the tracked dirt and the image id, and no engine75 tag remains.
- **Task 2:**
  - The crosscheck and the mutation gate pass, and the document is restored byte for byte by `cmp`.
  - `go test -run TestCAD ./internal/convert/...` passes.
  - No `//` line was added in cadsandbox.go or cadsandbox_test.go.
- **Task 3:**
  - `TestCADPreflight|TestConfinedCAD` passes.
  - The liveproof was run from a clean worktree at 3d0761e with tracked dirt 0. It ends `ALL PASS (5 checks)`, contains the proven line naming cad-dxf and rsvg-convert, and has no NOT RUN.
  - The only added `//` line is `//go:embed`.
  - `git diff --exit-code main -- internal/convert/exec.go internal/convert/converters.go` is empty.
- **Whole repo:** run with Tasks 2 and 3 applied (before they were committed), with python 3.14 first on PATH and GOTOOLCHAIN=go1.26.4. `go vet ./...` is clean, and `go test ./... -count=1` gives 31 packages ok. gofmt is clean.
- **State:**
  - STATE.md, ROADMAP.md and config.json were not staged.
  - The compose stack was never started.
  - No measure, engine75 or liveproof image remains, my build cache is pruned and the worktrees are removed.

## Known Stubs

None.

## Threat Flags

None beyond the plan's register:
- **T-75-28:** removal tables, effect rows, the never-resolved trace and the preflight denial.
- **T-75-29:** the font negative control and the PDF text proof in the preflight.
- **T-75-30:** the conf05 classes with the mutation proof.
- **T-75-31:** the calibrated judge with pre-registered classes.
- **T-75-SC:** no package was added.

## Self-Check: PASSED

- **Files:** the evidence files, SANDBOX-PATHS.md, both scripts, the crosscheck, cadpixel.go, cadsandbox.go, caddxf.go and main.go all exist with the changes described.
- **Commits:** e88422f, 8134c87, 3d97a73, c8c98c4, 3d0761e and ae295c6 are ancestors of HEAD and show signature G.
