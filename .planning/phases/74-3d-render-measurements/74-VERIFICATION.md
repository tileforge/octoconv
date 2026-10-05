---
phase: 74-3d-render-measurements
verified: 2026-10-05T22:05:09Z
status: gaps_found
score: 8/9 must-haves verified
covered_files: [".env.example", ".planning/ci-local/ci-local-20261005T214321Z.log", ".planning/phases/74-3d-render-measurements/74-01-PLAN.md", ".planning/phases/74-3d-render-measurements/74-01-SUMMARY.md", ".planning/phases/74-3d-render-measurements/74-02-PLAN.md", ".planning/phases/74-3d-render-measurements/74-02-SUMMARY.md", ".planning/phases/74-3d-render-measurements/74-03-PLAN.md", ".planning/phases/74-3d-render-measurements/74-03-SUMMARY.md", ".planning/phases/74-3d-render-measurements/74-04-PLAN.md", ".planning/phases/74-3d-render-measurements/74-04-SUMMARY.md", ".planning/phases/74-3d-render-measurements/74-05-PLAN.md", ".planning/phases/74-3d-render-measurements/74-05-SUMMARY.md", ".planning/phases/74-3d-render-measurements/74-06-PLAN.md", ".planning/phases/74-3d-render-measurements/74-06-SUMMARY.md", ".planning/phases/74-3d-render-measurements/74-07-PLAN.md", ".planning/phases/74-3d-render-measurements/74-07-SUMMARY.md", ".planning/phases/74-3d-render-measurements/74-CONTEXT.md", ".planning/phases/74-3d-render-measurements/evidence/74-01-wave0.txt", ".planning/phases/74-3d-render-measurements/evidence/74-02-mesh.txt", ".planning/phases/74-3d-render-measurements/evidence/74-06-judge.txt", ".planning/phases/74-3d-render-measurements/evidence/74-06-landlock.txt", ".planning/phases/74-3d-render-measurements/evidence/74-06-sandbox-liveproof.txt", ".planning/phases/74-3d-render-measurements/evidence/74-07-liveproof.txt", ".planning/research/SANDBOX-PATHS.md", "Dockerfile.cad-worker", "cmd/cad-worker/helper/cad-mesh", "cmd/cad-worker/helper/cad-render", "cmd/cad-worker/main.go", "cmd/cad-worker/requirements-vtk.txt", "cmd/cad-worker/vtk-keeplist.txt", "cmd/cad-worker/vtk_keeplist.py", "deploy/chart/octoconv/templates/configmap.yaml", "deploy/chart/octoconv/values.yaml", "docker-compose.yml", "docs/ERROR-CODES.md", "docs/MEASUREMENTS.md", "internal/api/api.go", "internal/api/cad_ceiling_test.go", "internal/api/cad_opts_test.go", "internal/api/formats_handlers_test.go", "internal/api/handlers.go", "internal/api/handlers_test.go", "internal/api/pre58_pinning_test.go", "internal/convert/cad.go", "internal/convert/cad_test.go", "internal/convert/cadmeasure.go", "internal/convert/cadmeasure_test.go", "internal/convert/cadopts.go", "internal/convert/cadopts_test.go", "internal/convert/cadpixel.go", "internal/convert/cadpixel_test.go", "internal/convert/cadrender.go", "internal/convert/cadrender_test.go", "internal/convert/cadsandbox.go", "internal/convert/cadsandbox_test.go", "internal/convert/convert.go", "internal/convert/convert_test.go", "internal/convert/converters.go", "internal/convert/operations_test.go", "internal/convert/sandboxcoverage_test.go", "internal/jobs/add_outputs_test.go", "internal/jobs/repo.go", "internal/measurements/consistency_test.go", "internal/measurements/measurements.go", "internal/worker/cad_outputs_test.go", "internal/worker/descriptors.go", "internal/worker/pinning_test.go", "internal/worker/probe_sideoutput_test.go", "internal/worker/runner.go", "internal/worker/webhook_role_test.go", "internal/worker/worker.go", "internal/worker/worker_test.go", "scripts/cad-liveproof.sh", "scripts/cad-render-probe.sh", "scripts/cad-unit-probe.py", "scripts/cad-worker-sandbox-measure.sh", "scripts/cmd/cad-measurements-check/main.go", "scripts/cmd/cad-pixel-judge/main.go", "scripts/conf05-crosscheck.sh"]
covered_digest: "v3:sha256:bd9a0cbce2e50dcaa430951af8cc392c6070b1bb7111600746720bf334f90505"
behavior_unverified: 0
overrides_applied: 0
gaps:
  - truth: "The final cad-worker image (now carrying vtk 9.7.1 and libosmesa6) builds for linux/amd64 and, run emulated, refuses to start without Landlock (the emulated-amd64-refuses check of scripts/sandbox-liveproof.sh cad, which phase 73 ran and passed on its image)"
    status: failed
    reason: "NOT RUN. evidence/74-06-sandbox-liveproof.txt records 'NOT RUN: cad emulated-amd64-refuses -- the emulated linux/amd64 build was stopped by a disk watchdog at 5.77 GiB free (floor 6 GiB)' and its RESULT line says 'this is NOT an ALL PASS line'. Phase 74 changed Dockerfile.cad-worker (a second hash-locked wheel install, a keep-list prune of site-packages, a new apt package, a vtkmodules import smoke). None of that has been built for amd64, so whether the amd64 image builds at all, whether the x86_64 vtk wheel hash in the copied lock installs, whether the keep-list prune leaves a working VTK on amd64, and whether the class still refuses to run unconfined there are all unknown. Every Landlock measurement in this phase is native arm64."
    artifacts:
      - path: ".planning/phases/74-3d-render-measurements/evidence/74-06-sandbox-liveproof.txt"
        issue: "4 PASS, 1 NOT RUN (emulated-amd64-refuses); no ALL PASS line"
      - path: "Dockerfile.cad-worker"
        issue: "changed by this phase; amd64 build unproven"
    missing:
      - "Free at least 6 GiB of docker disk and run scripts/sandbox-liveproof.sh cad on the final tree until it ends with an ALL PASS line including emulated-amd64-refuses; commit the transcript as evidence"
      - "If the amd64 build fails (wheel hash, prune, OSMesa), fix it in a separate commit and re-run the arm64 live proof as well"
deferred: []
coincidental_reliance_items:
  - truth: "SC4 units: STEP/IGES report the declared unit, numbers converted back from mm"
    reason: undeclared-precondition
    harden: "The factor used to convert back is the table value of the unit the helper's own regex reads from the STEP text (cad-mesh applied_factor), not a factor measured at run time; cadFactorMatches in cadmeasure.go therefore compares the table with itself for STEP. The result is correct only while OCP resolves the same unit from the same file. The Wave-0 probe and the live ctc_05 extents (19 x 20.75 x 22 inch, round numbers) show agreement on five STEP files. To harden it, cross-check the text unit against OCP's own unit reading where OCP reports one, and report unspecified/none on disagreement. A file that defines a CONVERSION_BASED_UNIT named INCH with a non-25.4 conversion is the case the text reading would get wrong."
---

# Phase 74: 3D Render & Measurements Verification Report

**Phase goal:** Клиент получает PNG-превью детали и вместе с ним честные размеры; два выхода одной джобы видны оба или не виден ни один.
**Verified:** 2026-10-05T22:05:09Z on branch `gsd/phase-74-3d-render-measurements`, HEAD `01be894` (base `main`)
**Status:** gaps_found
**Re-verification:** No. This is the initial verification.

## Goal Achievement

The client-facing goal is achieved on arm64. It is proven live on the final product code (`d93f6db`; the commits after it touch only `.planning/`). One gap remains: the phase changed the engine image, and its amd64 build and amd64 refusal check were never run.

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | SC1: `render` step/stp/iges/stl → png gives a non-empty, pixel-judged part image. The default is iso, 1280, opaque. Different views give different images, with Z up. | ✓ VERIFIED | 74-07-liveproof (commit d93f6db, tracked_dirt 0, ALL PASS): RENDER rows for .step, .stp, .igs, .iges and .stl are done with 2 outputs. The Go judge says `ok=1`, mode blob, 1280x1280, margin ≥76 px, `alpha=0`. The default render's PNG sha (`51c322…`) equals the explicit `view=iso` sha, so the default is iso. The iso, front and top views give 3 distinct shas. Front h/w = 0.502 and top h/w = 1.500 on the X=2, Y=3, Z=1 inch box. That holds only with Z up: a Y-up camera would give front h/w 1.5. `JudgeCADPNG` runs in `CADRenderConverter.Convert` before returning nil (cadrender.go). |
| 2 | SC2: closed options give 422. `quality` changes tessellation for both convert and render, with monotone triangle counts. `transparent` gives an alpha PNG with a fully transparent background. | ✓ VERIFIED | `ParseCADOpts` (cadopts.go) accepts five keys with closed values. Size is an integer from 256 to 2048, and null, string or fractional sizes are rejected. Unknown keys are rejected. `ValidateCADApplicability` lets convert take only quality and refuses quality on an STL render. Live OPTS_REJECT: 9 rows, all 422 with `jobs_created=0`, covering view, size 255, 2049 and string, quality, background, unknown key, a render key on convert, and quality on STL. Live QUALITY runs real convert jobs: draft 3988 ≤ standard 5948 ≤ fine 14140. Render passes the same `--quality` to the same `meshStage` (cadrender_test asserts `--quality fine` in the mesh argv), and a render with `fine` ended done and judged ok. Transparent: live `alpha=1`, frac_transparent 0.6049 + part 0.3951 = 1.0, so every non-part pixel has alpha 0. The judge requires alpha and treats any pixel with a>0 as part, so a semi-transparent background would break J3's margin. |
| 3 | SC3/MEAS-01: two outputs, written atomically. The inversion test can fail against the naive sequence. `download_url` points to the PNG. | ✓ VERIFIED | `Repo.AddOutputs` writes all rows in one `pgx.BeginFunc` (repo.go). `process()` (worker.go) runs the side-output hook (which reads the sidecar and runs `measurements.Validate`) before any upload. It then uploads the primary and the extras, calls `AddOutputs` once, then `MarkDone`. `TestAddOutputsRollsBackWhenTheSecondRowFails` holds the naive control in the same test: two `AddOutputWithRole` calls must leave exactly 1 visible row, while `AddOutputs` must leave 0, and `MarkDone` refuses with "0 primary outputs". So a naive `AddOutputs` fails that test. Worker tests cover a second upload failure, an `AddOutputs` failure, rejected measurements and a rejected PNG (cad_outputs_test.go). `download_url` selection is tested for GET (`TestGetJob_DownloadURLNeverMeasurements`) and for the webhook in reverse row order (webhook_role_test.go). Live, every render shows `roles=primary@0;measurements@1`, and the measurements body was fetched through the API URL and validated. The live DB run is backed by a scratchpad transcript, not a committed one (see Advisory). |
| 4 | SC4/MEAS-02..04: volume and area are null with a reason, never 0. The unit follows the source: the declared unit, converted back from mm. `assume_unit` never overrides a declared unit. IGES inch is a recorded GAP. | ✓ VERIFIED (coincidental-reliance) | cad-mesh `solid_summary` sums only closed and valid solids. A zero or non-finite total demotes them to `invalid_solid`, so 0 is never emitted. `buildCADMeasurements` pairs a null volume with its reason, divides by f, f² and f³, and runs `Validate`. `cadUnitFor` returns the declared unit before it looks at `assumeUnit`; assume applies only when there is neither a declared unit nor raw text; STL gives unspecified/none or client_assumed. Live MEAS: units_inch gives inch/declared 1:2:3, V 6, A 22, and stays inch with `assume_unit=mm`. stl_ascii_box gives unspecified/none, or mm/client_assumed with assume. surfaces_only.iges gives null with `no_solids`. nasa_hubble.stl gives null with `stl_not_watertight`. No document has a 0. IGES that declares anything but mm gets `applied_factor=None` and becomes unspecified/none; the GAP is recorded in 74-01-wave0 and docs/MEASUREMENTS.md. The `text_unit` decision is judged below. |
| 5 | SC5: the render stage has no OCP and runs under its own measured ruleset, which matches across SANDBOX-PATHS, Go and conf05. Preflight judges by pixels. Render failures are retryable, a render timeout is terminal, and mesh failures are terminal. | ✓ VERIFIED (arm64) | `cadRenderReadOnly` = `/opt/venv`, `/usr/bin/python3.11`, `/usr/lib/python3.11`, plus multiarch; `cadRenderEngines` = `cad-render`. This equals `MEASURED,stage=render` and the `conf05:cadrender` blocks. `scripts/conf05-crosscheck.sh cad` gives `MATCH: cadrender readonly (3) engines (1)` and exits 0. The render removal table has 12 rows; the negative control fails as expected and the outside file is denied. cad-render has no OCP/OCC/cadquery string, and `cadsandbox_test` greps its imports. `classifyCADStage`: render deadline → `ErrCADRenderTimeout` (terminal, cad_too_complex); SIGKILL → terminal; any other render failure → `ErrCADRenderFailed`, which is not in `isCADTerminal`. pinning_test covers retry and no MarkFailed. Live RENDER_RETRY (TERM to cad-render): retried=1, then done with 2 outputs. MESH_SIGNAL: failed, retried=0. CANCEL_RENDER: pid gone in 0.3 s. Preflight: the worker log line "cad-guard, cad-mesh and cad-render ran inside the job directory and were denied a file outside it", and the live stack ran with `sandbox_disabled=false`. |
| 6 | Limits: the engine timeout is 886 s, under the 900 s reconciler window. The triangle cap is 3,835,382, derived from Wave-0. | ✓ VERIFIED | `CADDefaultEngineTimeout` = 30+316+480+60 = 886 is pinned in cad_test, and cadsandbox_test asserts it is below the reconciler window. `RECONCILER_ACTIVE_STALE_AFTER` is unchanged in the diff. Helm grace is 906, and the compose, configmap and .env values are all 886. `CADMaxRenderTriangles=3835382` and `CADMaxSTLBytes=190840832` are held by a cadrender_test that regex-parses the `DERIVED` lines of 74-01-wave0.txt. Live HDR shows `cad_engine_timeout_s=886` and INSPECT shows stl max_bytes 190840832. |
| 7 | Supply chain: the only additions are vtk from the 71 lock and libosmesa6. | ✓ VERIFIED | `cmp` shows `requirements-vtk.txt`, `vtk_keeplist.py` and `vtk-keeplist.txt` byte-identical to `scripts/cad-measure/`. The Dockerfile diff adds one pip install (`--require-hashes --no-deps --only-binary`), the prune and `libosmesa6`, and nothing else. The libosmesa6 version equals 71's. IDENTITY shows 5/5 PNGs byte-identical to the 71 renders. |
| 8 | Invariants: `exec.go` unchanged, no new code comments, goldens and metric labels unchanged. | ✓ VERIFIED | `git diff main...HEAD` is empty for internal/convert/exec.go, scripts/cad-measure, Dockerfile.cad-measure and cad-guard. No golden or testdata file and nothing in internal/metrics is in the diff. Go `//` lines changed only where existing comments were edited (net ≤ 0 per file, except formats_handlers_test.go, which re-wraps an existing comment). New scripts and helpers add 0 `#` lines besides the shebang. conf05-crosscheck.sh only edits its existing header. One new comment block is in .env.example (see Anti-Patterns). |
| 9 | The final engine image builds for amd64 and refuses to run unconfined there (emulated-amd64-refuses). | ✗ FAILED (NOT RUN) | 74-06-sandbox-liveproof.txt: "NOT RUN … disk watchdog at 5.77 GiB free"; "this is NOT an ALL PASS line". The Dockerfile changed in this phase. See Gaps. |

**Score:** 8/9 truths verified (0 present-but-behavior-unverified)

### Decisions recorded

- **D-06 (the J1 relaxation for thin frames).** J1's span check (`count >= long side`) now applies only to blob-mode frames. A frame that fails the blob test is judged by the thin rule instead: aspect < 0.15 and dark_count ≥ 0.25 × long side, after J0, J2 and J3. Empty frames still fail J1 (no part pixels) or J4. `CADMinRenderSize` stays 256. The trigger was a W15 thin part at 256 px: 186 part pixels against a 206 px extent, which pixelcheck also rejects. Evidence is in 74-06-judge.txt: 49 frames, 46 agreements, 3 divergences that the thin rule admits (all W15), 0 unexplained, and blank frames rejected by both judges. Tests `TestJudgeCADPNGAcceptsW15ThinFrameAt256WithFewerPixelsThanItsExtent` and `TestJudgeCADPNGRejectsSparseNoise` are mutation-proved per 74-06-SUMMARY. Verdict: an intentional, bounded relaxation backed by evidence. It widens acceptance only for frames that pass J0, J2, J3 and the thin criteria.
- **One operation per engine, relaxed for cad (convert + render).** `operations_test.go` keeps "every class serves exactly one operation" except for cad, which must serve exactly convert and render. `convert_test.go` names the five allowed `(operation, pair)` keys. `cmd/cad-worker/main.go` replaces both converters with the same configured `CADConverter`. Verdict: the relaxation is explicit and narrow, enforced by name and count, not by omission.
- **`cad_unit_factor_source = text_unit` (the orchestrator DECISION in 74-01-wave0.txt).** The pre-registered rule gave `none` because the raw ratio for nist-d2mi was 25.4707. A follow-up probe found that the extra ratio comes from 9 OCP-created edge-split vertices; on the 130 vertices that match VERTEX_POINT the ratio is exactly 25.4. The DERIVED line was left unedited and the DECISION line was added after it with the reason and a fallback. **Judgment: honest and evidenced, with a residual precondition.** It is honest because the override is visible, the original rule result is preserved, and docs/MEASUREMENTS.md states "The helper does not measure that ratio at run time". It is evidenced by 5 STEP rows, one of them explained by a re-probe rather than matched directly, and corroborated live: ctc_05 reports 19 × 20.75 × 22 inch, round design dimensions that would be very unlikely under a wrong factor. The residual is that the helper's regex predicts OCP's unit resolution, and nothing cross-checks it at run time; this is recorded under coincidental_reliance_items. One process detail: the re-probe was "one run of the 71 pruned image with scripts/cad-unit-probe.py helpers". Its command is not a committed probe mode, so a reader cannot re-run it verbatim.

### Deferred Items

None are filtered. The amd64 gap could be closed by Phase 76 SC3 ("Живой гейт всех классов, включая `cad`, завершается `ALL PASS` на образах, собранных из финального коммита"), which needs the sandbox live proof to end with ALL PASS. Per the verification brief it stays a gap here. The DXF parts of MEAS-01 and MEAS-03 belong to Phase 75 by the REQUIREMENTS.md mapping note; they are not gaps of this phase.

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `cmd/cad-worker/helper/cad-render` | confined VTK render, CADVERDICT | ✓ VERIFIED | Shebang-only header with no OCP imports. Byte-identical PNG output to 71 on 5 parts. |
| `cmd/cad-worker/helper/cad-mesh` | `--quality`, `--measure`, `stats` | ✓ VERIFIED | QUALITY table and the mm-per-unit table are present. The stats path has no OCP import (grep and evidence). The standard-quality STL is byte-identical to 73 (IDENTITY_STL). |
| `Dockerfile.cad-worker` | hash-locked vtk, prune, libosmesa6 | ✓ VERIFIED (arm64) | amd64 is unbuilt (gap). |
| `internal/convert/cadopts.go` | closed union, applicability | ✓ VERIFIED | Wired at handlers.go and in the worker through `CADOptsFromMap`. |
| `internal/convert/cadpixel.go` | `JudgeCADPNG` J0–J4 and thin rule | ✓ VERIFIED | Called by cadrender.go, the preflight and the `cad-pixel-judge` CLI. |
| `internal/convert/cadrender.go` | `CADRenderConverter` | ✓ VERIFIED | Registered in converters.go and replaced in cad-worker main. |
| `internal/convert/cadmeasure.go` | draft → document in the declared unit | ✓ VERIFIED | Strict decode, finite checks, `Validate`. |
| `internal/jobs/repo.go` `AddOutputs` | one transaction | ✓ VERIFIED | Used by worker.go when `sideOutputs != nil` (cad and probe). |
| `internal/worker/descriptors.go` | `cadSideOutputs`, render sentinels | ✓ VERIFIED | `SideOutputs: cadSideOutputs` on `CADDescriptor`. |
| `.planning/research/SANDBOX-PATHS.md` | `conf05:cadrender` blocks | ✓ VERIFIED | conf05 MATCH. |
| evidence/74-0* | wave0, mesh, judge, landlock, sandbox, liveproof | ✓ present | The sandbox-liveproof file is a partial pass (gap). |

### Key Link Verification

| From | To | Via | Status |
|------|----|-----|--------|
| handlers.go create | `ParseCADOpts` + `ValidateCADApplicability` | engine-keyed opts switch | ✓ WIRED (live OPTS_REJECT) |
| worker `CADDescriptor.OptsFromMap` | `CADOptsFromMap` | re-parse of the stored map | ✓ WIRED (cadrender.go also re-parses) |
| cadrender.go | `JudgeCADPNG`, `buildCADMeasurements` | before Convert returns nil | ✓ WIRED |
| worker.go `process()` | `Repo.AddOutputs` | single write path for hooked engines | ✓ WIRED |
| cad-worker main | `CADRenderConverter`, `CAD_RENDER_TIMEOUT` | `Default.Replace`, `envDuration` | ✓ WIRED |
| SANDBOX-PATHS | cadsandbox.go | conf05-crosscheck.sh cad | ✓ WIRED (MATCH, exit 0) |
| wave0 DERIVED lines | cad.go constants | cadrender_test regex | ✓ WIRED |

### Data-Flow Trace (Level 4)

| Artifact | Data | Source | Real data | Status |
|----------|------|--------|-----------|--------|
| measurements output | bbox, solids, volume, area, unit | cad-mesh OCP draft or numpy stats → `buildCADMeasurements` → sidecar → `cadSideOutputs` → S3 + `AddOutputs` | yes (live MEAS rows from API-fetched bodies) | ✓ FLOWING |
| PNG primary | rendered frame | cad-render under Landlock → `JudgeCADPNG` → upload | yes (live judge on the downloaded bytes) | ✓ FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Build, vet and all tests on the pinned toolchain | `GOTOOLCHAIN=go1.26.4 go build ./... && go vet ./... && go test -count=1 ./...` | 31 packages ok, exit 0 | ✓ PASS |
| Formatting | `gofmt -l .` | empty | ✓ PASS |
| Landlock sets match the doc | `scripts/conf05-crosscheck.sh cad` | cadguard, cadmesh, cadrender and multiarch MATCH, exit 0 | ✓ PASS |
| Chart checks | `scripts/ci-helm-checks.sh` | all OK, exit 0 | ✓ PASS |
| vtk lock and keep-list identity | `cmp scripts/cad-measure/* cmd/cad-worker/*` | 3/3 identical | ✓ PASS |
| Invariant files | `git diff main...HEAD -- internal/convert/exec.go scripts/cad-measure Dockerfile.cad-measure cmd/cad-worker/helper/cad-guard` | empty | ✓ PASS |
| Live proof taken on final code | `git diff --stat d93f6db HEAD` | only `.planning/` files | ✓ PASS |

The AddOutputs DB tests skip without `DATABASE_URL`. Neither the local `go test` nor the ci-local gate tier sets it, so they skipped in both runs. No octoconv postgres was running and no service was started for this verification.

### Probe Execution

The docker probes were not re-run, as the brief required. The committed transcripts were checked for self-consistency instead:

| Probe | Result | Taken on | Status |
|-------|--------|----------|--------|
| `scripts/cad-liveproof.sh` → 74-07-liveproof.txt | `SUMMARY,result=ALL PASS` | d93f6db, tracked_dirt 0 (product code = HEAD) | PASS |
| `scripts/sandbox-liveproof.sh cad` → 74-06-sandbox-liveproof.txt | 4 PASS, 1 NOT RUN | c40b54f, dirt = STATE.md; c40b54f→d93f6db changed only scripts and summaries | PARTIAL (gap) |
| `scripts/cad-worker-sandbox-measure.sh` → 74-06-landlock.txt | `MEASURED,stage=render…`, leaked 0, 3 negative controls | image d1ee5a9e (helpers last changed in e873193) | PASS |
| `scripts/cad-render-probe.sh judge` → 74-06-judge.txt | 46/46 real ok, 3 thin-only, 0 unexplained | 3c2c1ee (the J1 reorder) | PASS |
| `scripts/cad-render-probe.sh tracer/identity/cost/units/mesh` → 74-01, 74-02 | ok, IDENTITY 5/5, DERIVED lines | Wave 0 commits with tracked dirt 2–3 (pre-commit work in progress) | PASS for Wave 0; the final behaviour is re-proven live in 74-07 |

### Requirements Coverage

| Requirement | Source plan | Status | Evidence |
|-------------|-------------|--------|----------|
| RNDR-01 | 74-01, 04, 05, 06, 07 | ✓ SATISFIED (arm64) | Truth 1; live RENDER on all 5 extensions |
| RNDR-02 | 74-03, 07 | ✓ SATISFIED | Closed view vocabulary, 422 live, Z-up ratios |
| RNDR-03 | 74-01, 03, 07 | ✓ SATISFIED | Size 256..2048, default 1280, 255, 2049 and string give 422, 2048 live |
| RNDR-04 | 74-02, 03, 04, 07 | ✓ SATISFIED | Same constants for convert and render; live monotone counts |
| RNDR-05 | 74-03, 04, 06, 07 | ✓ SATISFIED | Opaque by default (alpha=0); transparent has background alpha 0 |
| MEAS-01 | 74-05, 07 | ✓ SATISFIED for 3D | The DXF render half belongs to Phase 75 |
| MEAS-02 | 74-02, 07 | ✓ SATISFIED | Nulls with reasons, never 0 |
| MEAS-03 | 74-01, 02, 07 | ✓ SATISFIED for STEP, IGES and STL | The DXF half belongs to Phase 75; IGES non-mm gives unspecified (GAP recorded) |
| MEAS-04 | 74-02, 03, 07 | ✓ SATISFIED | Declared wins; client_assumed only when no unit is declared |

There are no orphaned requirements: REQUIREMENTS.md maps exactly these 9 IDs to Phase 74, and each one appears in at least one plan.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `.env.example` | ~581–584 | 4 new comment lines of design rationale ("480s render CHOSEN … covers 437.36s by 1.10x … GAP"), plus an inline note on `CAD_RENDER_TIMEOUT` | ℹ️ Info | They extend the file's existing derivation block, which is config documentation rather than code. Under the user's global rule, rationale belongs in the PR body; consider moving it there. |
| `internal/api/handlers_test.go` | 4047 | `TestCreateJobCADTakesNoOptions` name is stale (CAD now takes options) | ℹ️ Info | The test body still asserts 422 for foreign keys, so its behaviour is right. |
| `internal/worker/worker.go` | process() | Probe's sanitize report now also goes through `AddOutputs`, and its hook runs before the primary upload | ℹ️ Info | Deliberate (`TestCADOutputsProbeReportGoesThroughAddOutputsAtOrdinalOne`). It is atomic now, which is stricter than before. |
| (none) | | TBD, FIXME or XXX in changed files | | 0 matches |

### Advisory

| # | Finding | Category | Why advisory |
|---|---------|----------|--------------|
| 1 | The live-DB run of `TestAddOutputs*`, and its naive-mutation run, are evidenced only by 74-05-SUMMARY and an uncommitted scratchpad transcript, where all 7 role and AddOutputs tests PASS against a DB with no SKIP. ci-local's gate tier does not set `DATABASE_URL`, so this test skips in every committed transcript. | other | The test design itself can fail against the naive sequence (the in-test control). Committing a `DATABASE_URL=… go test -v -run 'TestAddOutputs' ./internal/jobs/` transcript would close this. |
| 2 | `applied_factor` in the draft is the table value of the text unit, not a factor OCP reported. `cadFactorMatches` is therefore tautological for STEP. | other | It is documented truthfully in docs/MEASUREMENTS.md; see coincidental_reliance_items. |

### Human Verification Required

None.

### Gaps Summary

There is one gap, and its root cause is the environment, not the code. The emulated-amd64 sandbox check was not run because the docker disk was below the 6 GiB watchdog floor. This phase is the first to put VTK, OSMesa and a site-packages prune into the cad image, so the amd64 build of that image has never been exercised. Every confinement measurement here is native arm64, and the phase does not claim otherwise (SANDBOX-PATHS "Ещё не замерено" says so). A native amd64 Landlock measurement is out of scope per CONTEXT deferred; the gap is only the build plus refusal check that phase 73 had passed. Closing it takes one `scripts/sandbox-liveproof.sh cad` run with enough free disk, ending in an ALL PASS line that is then committed.

---

_Verified: 2026-10-05T22:05:09Z_
_Verifier: Claude (gsd-verifier)_
