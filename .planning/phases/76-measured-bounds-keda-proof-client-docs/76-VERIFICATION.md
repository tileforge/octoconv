---
phase: 76-measured-bounds-keda-proof-client-docs
verified: 2026-10-17T02:35:17Z
status: passed
score: 5/5 roadmap success criteria verified (4/4 requirements satisfied)
covered_files: [".env.example",".planning/debug/cad-mesh-timeout-30s-wait.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-01-PLAN.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-01-SUMMARY.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-02-PLAN.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-02-SUMMARY.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-03-PLAN.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-03-SUMMARY.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-04-PLAN.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-04-SUMMARY.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-05-PLAN.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-05-SUMMARY.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-06-PLAN.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-06-SUMMARY.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-07-PLAN.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-07-SUMMARY.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-08-PLAN.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-08-SUMMARY.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-09-PLAN.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-09-SUMMARY.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-10-PLAN.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-10-SUMMARY.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-11-PLAN.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-11-SUMMARY.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-CONTEXT.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/deferred-items.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-01-stack.txt",".planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-02-bounds.txt",".planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-03-derived.txt",".planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-09-image-size.txt",".planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-09-keda-loadproof.txt",".planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-10-keda-downscale.txt",".planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-11-final-gate.txt",".planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-11-frontend.txt",".planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-11-liveproof.txt",".planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-11-sandbox-liveproof.txt",".planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/ci-local-20261016T230204Z.log",".planning/todos/pending/2026-10-17-darwin-killpg-fork-race.md","deploy/chart/octoconv/templates/configmap.yaml","deploy/chart/octoconv/values-loadproof.yaml","docker-compose.yml","docs/CAD.md","docs/ERROR-CODES.md","docs/MEASUREMENTS.md","docs/PREVIEW.md","docs/README.md","docs/openapi.yaml","frontend/src/App.tsx","frontend/src/formats.test.ts","frontend/src/formats.ts","frontend/src/styles.css","internal/api/cad_ceiling_test.go","internal/api/estimate.go","internal/convert/cad.go","internal/convert/cad_doc_test.go","internal/convert/cad_docs_sync_test.go","internal/convert/cad_openapi_doc_test.go","internal/convert/cad_test.go","internal/convert/caddxf.go","internal/convert/caddxf_test.go","internal/convert/caddxfpdf.go","internal/convert/caddxfpdf_test.go","internal/convert/caddxfsvg.go","internal/convert/cadpixel.go","internal/convert/cadpixel_test.go","internal/convert/cadpixelbg_test.go","internal/convert/cadpixelmeasure_test.go","internal/convert/cadrender_test.go","internal/convert/cadsandbox_test.go","internal/convert/preview_doc_test.go","internal/convert/testdata/cad/PROVENANCE.md","internal/convert/testdata/cad/render-bg/degenerate_plate_front_1280_opaque.png","internal/convert/testdata/cad/render-bg/degenerate_plate_front_1280_transparent.png","internal/convert/testdata/cad/render-bg/degenerate_rod_iso_256_opaque.png","internal/convert/testdata/cad/render-bg/degenerate_rod_iso_256_transparent.png","internal/convert/testdata/cad/render-bg/linearslide_iso_256_opaque.png","internal/convert/testdata/cad/render-bg/linearslide_iso_256_transparent.png","internal/convert/testdata/cad/render-bg/nauo4_iso_1280_opaque.png","internal/convert/testdata/cad/render-bg/nauo4_iso_1280_transparent.png","internal/convert/testdata/cad/render-bg/w15_iso_256_opaque.png","internal/convert/testdata/cad/render-bg/w15_iso_256_transparent.png","internal/e2e/cad_grace_chain_test.go","internal/mcpserver/client.go","internal/mcpserver/descriptions_test.go","internal/mcpserver/mcpserver.go","internal/mcpserver/tools.go","internal/worker/cad_codes_doc_test.go","internal/worker/pinning_test.go","scripts/cad-corpus-gen.py","scripts/cad-dxf-bounds.sh","scripts/cad-image-measure.sh","scripts/cad-inworker-measure.sh","scripts/cad-liveproof.sh","scripts/ci-local.sh","scripts/cmd/cad-pixel-judge/main.go","scripts/cmd/cad-png-compare/compare.go","scripts/cmd/cad-png-compare/compare_test.go","scripts/cmd/cad-png-compare/main.go","scripts/final-live-gate.sh","scripts/keda-cad-downscale-survival.sh","scripts/keda-cad-loadproof.sh"]
covered_digest: "v3:sha256:5fb07ea4a7d5851d496636f14bb58e1355877ca46eb2d3a89376a125720ff115"
behavior_unverified: 0
overrides_applied: 0
re_verification:
  previous_status: human_needed
  previous_score: 5/5 roadmap success criteria verified (4/4 requirements satisfied)
  gaps_closed:
    - "Human item 1 (the red full-suite run, TestCADStageDeadlineIsNotTheAttemptDeadline mesh subtest waited 30.59 s): the root cause is established in .planning/debug/cad-mesh-timeout-30s-wait.md, the fixture fix 69fee40 is test-only, and the verifier's own `GOTOOLCHAIN=go1.26.4 go test ./... -count=1` is now rc 0 (32 packages ok, internal/convert 382.1 s), followed by `go test ./internal/convert -count=1` ok (374.3 s)"
  gaps_remaining: []
  regressions: []
---

# Phase 76: Measured Bounds, KEDA Proof & Client Docs Verification Report

**Phase Goal:** Границы класса измерены, а не скопированы; класс доказан вживую на образах из финального коммита; контракт превью и клиентская документация записаны и привязаны к коду.
**Verified:** 2026-10-17T02:35:17Z
**Status:** passed
**Re-verification:** Yes. The previous run (793f1a4, 2026-10-17T01:02:20Z) was `human_needed` for one reason only: one red full `go test ./...` run. This run re-checks that item in full and the five truths for regression.

## Re-verification: what changed since 793f1a4

`git diff --name-only 793f1a4..HEAD` lists three files:
- `internal/convert/cad_test.go` (69fee40);
- `.planning/debug/cad-mesh-timeout-30s-wait.md` (314a923);
- `.planning/todos/pending/2026-10-17-darwin-killpg-fork-race.md` (7914745).

`git diff --name-only 07d1024..HEAD -- . ':!.planning'` lists only `internal/convert/cad_test.go`. The diff is three fixture strings in TestCADStageDeadlineIsNotTheAttemptDeadline, `"sleep 30\n"` to `"exec sleep 30\n"` (two mesh fakes and one guard fake). No assertion, limit or other test changed, and no comment was added.

### Can this change affect a built image or the final gate's verdict? No.

- `_test.go` files are not part of `go build`. Every runtime Dockerfile builds with `go build -o /out/<bin> ./cmd/<bin>`, and its final stage copies only `/out/<bin>` plus non-Go assets (cad helpers, requirements, the tectonic bundle) `--from=` earlier stages.
- `Dockerfile.e2e` runs `go test -c ./internal/e2e`. That compiles only internal/e2e's own test files; test files of imported packages (internal/convert) are never compiled into another package's test binary.
- `Dockerfile.worker-test` copies the whole tree. No script or compose file references it, and the final gate does not build it.
- `.dockerignore` excludes `.git/`, so no VCS revision is stamped into the binaries. Images built from HEAD therefore carry the same binaries as the ones the final gate built from 07d1024.
- What the change does not carry over: the final gate's ci-local gate and race tiers ran the old version of this one test at 07d1024 (and passed). The new version at HEAD is covered by this run's two passes below.

### The debug session's conclusion is supported

I read `.planning/debug/cad-mesh-timeout-30s-wait.md` and checked its load-bearing claims against the code and the host:

| Claim | Check | Result |
|---|---|---|
| The runner sends one group SIGKILL, then waits on `cmd.Wait` with no bound | `internal/convert/exec.go` runCommandCore: `Setpgid: true`, `syscall.Kill(-cmd.Process.Pid, SIGKILL)` once in the `ctx.Done` branch, then `<-done`; no `WaitDelay` | Confirmed |
| Stdout and stderr are pipes, so Wait waits for every holder of the write ends | `cmd.Stdout`/`cmd.Stderr` are `*tailBuffer`, not `*os.File`, so os/exec creates pipes and copy goroutines that Wait joins | Confirmed |
| The old fake forks `sleep` inside the group, and the new one does not | `fakeEngine` writes `#!/bin/sh\n` + script. On this host, `sh` running `sleep 2` showed `sh` plus a child `sleep` in the same pgid; `exec sleep 2` showed one `sleep` process only | Confirmed |
| The 30 s wait was sleep's own exit, not a wrong error | The previous failure kept ErrCADMeshTimeout and took 30.59 s, which matches the fake's `sleep 30` | Consistent |
| The cause is a killpg-vs-fork race on XNU | Debug evidence: 9 of 400 loaded runs at 30.6 to 30.9 s, each with an orphan `sleep 30` (ppid 1, pgid of the killed group) and the dump in `awaitGoroutines`; sweep 26/3600 escapes with fork shape and 0/3600 with exec shape; a second group kill freed the pipe in 0.3 to 2 ms. The falsification test is stated, and the eliminated hypotheses (kill before setpgid, leader not reaped, sandbox helper holding pipes) are each backed by evidence | Supported. I did not re-run the 400-run reproduction |
| The fix does not weaken coverage | Overlay mutants: a no-kill exec.go fails the fixed subtest (30.7 s); a pid-only kill fails TestCADCancelMidMeshLeavesNoProcessOfTheGroupAlive. That test exists at `cad_test.go:542`; it forks `sleep 300 &` and kills only after the grandchild exists | Supported |

Limits of the conclusion, all stated in the session itself:
- "Linux production not affected" rests on kernel history (the 2006 change that made fork atomic against group signals). It was not observed live, because no live stack may run.
- Whether a shipped engine's descendants call `setsid` and leave the group was not checked.
- A separate 3 to 14 s slow mode under synthetic overload (22 `yes` workers) is host stall. It occurs with both fixture shapes, and the 5 s limit can still trip under that load.

None of these is a phase-76 must-have. The kill path is pre-existing code, and `exec.go` is byte-identical to main (blob 8c0b16cf… on both). The runner hardening (repeat the group kill until ESRCH, add `WaitDelay`) is tracked in `.planning/todos/pending/2026-10-17-darwin-killpg-fork-race.md` and deferred by the binding exec.go decision. It is recorded below as a warning, not a gap.

## Goal Achievement

### Observable Truths

| # | Truth (ROADMAP SC) | Status | Evidence |
|---|---|---|---|
| 1 | `CAD_ENGINE_TIMEOUT` is measured over both operations (convert and render, 2D included) on a real corpus at the input ceiling. The value is the maximum over operations, paired with the input ceilings, and the formula and worst cell are recorded. The inspect estimate is marked measured. | ✓ VERIFIED (with the AR-1 reading, see below) | `evidence/76-03-derived.txt`: DERIVED guard 21 = ceil(1.5x13.43), mesh 324 = ceil(1.5x215.84), dxf 186, rsvg 217, io 20, render 521 = remainder 886-21-324-20, engine = max(886 for 3D, 661 for DXF) = 886, worst_cell IR16MiB, plus every ceiling. All eight in-worker crosschecks give ceil(1.5 x attempt wall) <= 886 (largest 586, dxf_pdf). In the previous run I re-ran `scripts/cad-dxf-bounds.sh derive76 --derive --out <scratch>` at HEAD, a pure computation with no docker. Its body was byte-identical to the committed file. `internal/convert/cad.go` holds 21/324/521/20/186/217, the IGES ceiling 120 MiB and `CADDefaultEngineTimeout = max(3D sum, DXF sum)`, all pinned to the DERIVED lines by TestCADConstantsComeFromThe76Evidence and TestCADEngineTimeoutIsTheLongerRoute. `internal/api/estimate.go` gives `EngineCAD: EstimateSourceMeasured` and 886. Compose (13 services), the configmap and `.env.example` carry the same values. Live: nine INSPECT rows in `76-11-liveproof.txt` have `estimate_source=measured,estimated_max_seconds=886`. Regression: none of these files changed since 793f1a4; the pin tests passed in this run's full suite. |
| 2 | Scale-from-zero of `cad` through KEDA is proven on OrbStack k8s (0->N->0 with timestamps), together with downscale-survival. `keda.cad.minReplicaCount` is chosen deliberately, with the measured image size and cold pull recorded. | ✓ VERIFIED | `76-09-keda-loadproof.txt` ends `SUMMARY ... result=PASS`. It shows TRANSITION 0->1 at 15:24:21.986Z, 1->2 at 15:24:32.667Z, 2 Ready at 15:24:43.487Z and 2->0 at 15:26:21.855Z, with MAX_REPLICAS observed=2 and DRAIN 98.6 s against a 1290 s bound. `76-10-keda-downscale.txt` ends PASS. The busy pod was annotated, got SIGTERM at 16:22:29Z and finished the LONG render job `done` with attempts=1 at 16:25:11.714Z. The pod ended Completed with exit 0 after 162 s of its 906 s grace. `76-09-image-size.txt` has IMAGE_SIZE 1118.1/315.5 MiB, cold pull max 7.11 s, preflight max 7.67 s and DECISION value 0. `values.yaml keda.cad.minReplicaCount: 0`. The docs/CAD.md Scaling table is held to values.yaml and the evidence fields by TestCADDocMatchesTheCode. Regression: evidence unchanged since 793f1a4. |
| 3 | The live gate of all classes, `cad` included, ends `ALL PASS` on images built from the final commit. Killing the worker mid-render leaves zero live processes. A re-run of the same input on the same arch gives the same JSON, and the PNG is compared by tolerance, not by hash. | ✓ VERIFIED | `76-11-final-gate.txt`: commit 07d1024, tracked_dirt 0 at start and end, a fresh clone, six steps PASS (preflight, ci-local helm/gate/race/docker-build/e2e, tectonic, sandbox `ALL_PASS_(45_checks)`, cad, frontend `42 passed`), `VERDICT,fail_steps=0,not_run=0`, `SUMMARY,result=ALL PASS`. IMAGES_CHECK: 13/13 e2e and 2/2 cad images built in this run. Since 07d1024 the only non-planning change is the test fixture in `internal/convert/cad_test.go`, which is compiled into no image (see above), so the images the gate built are the images HEAD builds. Mid-render kill: WORKER_KILL_RENDER orphans=0, vm_orphans=0, kill_to_done 123.1 s; WORKER_KILL_RSVG orphans=0, vm_orphans=0; WORKER_KILL (mesh) orphans=0. Determinism: six DETERMINISM rows (3D iso/top x opaque/transparent, DXF x2), json_equal=1 on canonical bytes, under the pre-registered RULE `hash=never` tolerance. |
| 4 | Client docs (docs/CAD.md, error codes, docs/openapi.yaml, MCP descriptions and the reference frontend's format table) are held to the code by a test, as docs/ERROR-CODES.md already is. | ✓ VERIFIED | TestCADDocMatchesTheCode compares both ways against the registry, CADOpts, ceilings, timeouts, DERIVED/KNOWN_LIMIT lines, values.yaml and the loadproof. TestCADCodesDocMatchesTheMapping, TestCADOpenAPIMatchesTheCode and TestMCPDescriptionsMatchTheRegistry use `convert.Default.OperationClasses()`, the CADOpts reflection and a published-schema comparison. `frontend/src/formats.ts operationTable` is fed from `/v1/formats operations`, App.tsx sends `operation` and lists outputs by role, and vitest passed 42/42 in the previous run. Regression: the Go doc tests passed in this run's full suite; no frontend file changed since 793f1a4. |
| 5 | The preview contract (SEED-007) is written and pinned on CAD: format, sizes, background and behaviour on non-previewable input. Everything CAD did not force is marked "not fixed". | ✓ VERIFIED | docs/PREVIEW.md has the sections Fixed by CAD, Non-previewable input ("No placeholder or substitute image is ever returned") and Not fixed (11 items). TestPreviewDocMatchesTheCode holds the size range and default, backgrounds, views, the DXF view refusal through ValidateCADApplicability, the margins and roles to the constants, and fails on any fixed row it does not hold. SEED-007 line 16 points to docs/PREVIEW.md. Regression: passed in this run's full suite. |

**Score:** 5/5 truths verified (0 present-but-behavior-unverified). The two behavior-dependent truths are SC2 (downscale survival) and SC3 (zero processes after a kill). Each rests on directly observed live behavior in a committed transcript, not on presence.

#### SC1 under AR-1: what the 886 s means

- 886 s is not the measured worst case of every admissible input. It is the fleet budget: 900 s reconciler window, engine + 20 = 906 s pod grace.
- The 3D render deadline (521 s) is what remains of that budget after the measured guard, mesh and I/O.
- 76-01's pre-registered stacking probe established that render cost is not bounded by bytes (`STACK_DECISION ... not_byte_bound` for step, iges and stl). Under D-04 the timeout is never raised and a byte ceiling cannot bound render cost, so Path-B fired for no format.
- Stacked-copy renders at the ceilings are therefore refused `cad_too_complex`. Examples: M1, a 63 MiB STEP of stacked copies, took 951 to 1224 s at fine; M3, an IGES, hit the 1800 s budget.
- That is proven live by three STACKED_REFUSAL rows (step 533.6 s, iges 548.4 s, stl 521.4 s, attempts=1, census 0). docs/CAD.md states it in Known limits as `render_cost_not_bounded_by_input_bytes` and `stacked_render_refused`.
- Every admitted, non-stacked render cell fits with margin; the largest render crosscheck is ceil(1.5 x 194.90) = 293 s.
- I read the SC as achieved under this documented decision. It was resolved in plan 76-01 (AR-1) and recorded in RULE lines before any row.

#### The cad RENDER_CEILING row is NOT RUN: no must-have is left unproven

- `76-11-liveproof.txt` records `NOT_APPLICABLE,row=RENDER_CEILING,reason=no_pathb_fired`, with all four PATHB lines `fired=0`. The final gate does not count it as a pass: its VERDICT `not_run=0` counts NOT_RUN step lines, and this row is a separate NOT_APPLICABLE row.
- The row would prove an operation-aware render byte ceiling, the D-04 lever. That ceiling exists only if Path-B fires, and no such code exists (76-05 took the not-fired branch).
- The absence is held by tests that read the evidence:
  - TestCADPathBFiredForNoFormat fails if any PATHB line becomes fired=1;
  - TestCADRenderReChecksTheDerivedCeilingOfEveryFormat checks that the worker re-check equals the convert ceiling;
  - TestCADEveryOperationRefusesAboveTheDerivedFormatCeiling checks create 413 and inspect max_bytes for render and convert alike.
- The behaviour that does exist on the render path is proven live: the deadline refusal (STACKED_REFUSAL x3) and the shared byte ceiling (DXF_CEILING 413).
- So the NOT RUN row leaves no SC1 to SC5 truth and no requirement unproven.

### Required Artifacts

`gsd-tools verify.artifacts` passed on every plan in the previous run: 37/37 artifacts across 76-01..76-11, exit 0 for each. No artifact file changed since then except `internal/convert/cad_test.go` (fixture strings only).

| Artifact | Status | Details |
|---|---|---|
| `internal/convert/cad.go` measured constants | ✓ VERIFIED | Values pinned to the DERIVED lines by tests |
| `internal/api/estimate.go` | ✓ VERIFIED | cad measured, 886 |
| `docs/CAD.md`, `docs/PREVIEW.md` | ✓ VERIFIED | Substantive; held both ways by tests |
| `docs/openapi.yaml`, MCP `mcpserver.go`/`tools.go`/`client.go`, `frontend/src/*` | ✓ VERIFIED | Tests derive from the registry or the response |
| `scripts/keda-cad-loadproof.sh`, `scripts/keda-cad-downscale-survival.sh`, `scripts/cad-image-measure.sh` | ✓ VERIFIED | Produced the PASS transcripts |
| `scripts/final-live-gate.sh`, `scripts/ci-local.sh` knobs | ✓ VERIFIED | Produced the ALL PASS transcript; the escape-hatch fix (07d1024) matches `internal/sandbox/helper.go:20` (`== "true"`), and landlock_lines>=1 is still required |
| `scripts/cad-liveproof.sh` new rows | ✓ VERIFIED | 184 rows, `SUMMARY,result=ALL PASS` at 07d1024 |
| `internal/convert/cad_test.go` (69fee40) | ✓ VERIFIED | Fixture-only change; the 5 s and ErrCADMeshTimeout assertions are unchanged |

### Key Link Verification

`verify.key-links` exits 1 on every plan with "Source file not found" because the plans' `from:` fields are prose. I checked each link by hand in the previous run; none of the linked files changed since.

| From | To | Status | Details |
|---|---|---|---|
| 76-03-derived.txt DERIVED | cad.go constants | WIRED | TestCADConstantsComeFromThe76Evidence parses the DERIVED lines |
| CADDefaultEngineTimeout | compose, configmap, .env.example, unique TTL, ShutdownTimeout | WIRED | 13/13 compose `886s`, configmap `886s`; `cmd/cad-worker/main.go:135` ShutdownTimeout = engine+10; values.yaml cadWorker grace 906; TestCADGraceChainFollowsTheEngineTimeout |
| caddxfpdf.go PDF rejections | worker cadFailureCode | WIRED | wrapped `ErrCADTooComplex` (D-07); descriptors.go:733 maps it to `cad_too_complex`; TestCADCodesDocMatchesTheMapping |
| docs/CAD.md tables | OperationClasses, CADOpts, constants | WIRED | TestCADDocMatchesTheCode |
| /v1/formats operations | frontend table and select | WIRED | App.tsx:148-178 `operationTable`/`operationsOf`; vitest |
| OperationClasses | MCP operation tag and convertToolDescription | WIRED | TestMCPDescriptionsMatchTheRegistry |
| derived DERIVED/PATHB/KNOWN_LIMIT | cad-liveproof expectations | WIRED | HDR `derived_sha256`, TIMEOUTS/INSPECT/STACKED_REFUSAL rows |
| cad-png-compare | convert.JudgeCADPNG / JudgeCADDrawingPNG | WIRED | compare.go:184-186 |
| image-size DECISION, loadproof lines | docs/CAD.md Scaling | WIRED | TestCADDocMatchesTheCode Scaling block |
| final-live-gate.sh | ci-local.sh, cad-liveproof.sh | WIRED | final-live-gate.sh:353, :523 |

### Data-Flow Trace (Level 4)

| Artifact | Data | Source | Real data | Status |
|---|---|---|---|---|
| inspect `estimate_source`/`estimated_max_seconds` for cad | 886, measured | estimate.go maps, which a test ties to .env.example | live INSPECT rows | ✓ FLOWING |
| frontend operation table | rows | `/v1/formats` response `operations` | vitest with response fixtures; no hard-coded list | ✓ FLOWING |

### Behavioral Spot-Checks (this run)

| Behavior | Command | Result | Status |
|---|---|---|---|
| Only a test file changed after the gate commit | `git diff --name-only 07d1024..HEAD -- . ':!.planning'` | `internal/convert/cad_test.go` only; diff is three fixture strings | ✓ PASS |
| exec.go identical to main | `git rev-parse main:internal/convert/exec.go HEAD:internal/convert/exec.go`; `git diff --quiet main HEAD -- internal/convert/exec.go`; worktree clean | both 8c0b16cf7a1876bd3fcba2efe69ea0da8739f391, no diff | ✓ PASS |
| gofmt | `gofmt -l .` | empty | ✓ PASS |
| vet | `GOTOOLCHAIN=go1.26.4 go vet ./...` (go1.26.4 darwin/arm64) | rc 0 | ✓ PASS |
| Full test suite | `GOTOOLCHAIN=go1.26.4 go test ./... -count=1`, py314 (Python 3.14.8) first on PATH, 02:21:10Z to 02:27:45Z | **rc 0**: 32 packages ok, no FAIL lines; internal/convert ok 382.131 s, internal/e2e ok 2.817 s | ✓ PASS |
| The package again | `GOTOOLCHAIN=go1.26.4 go test ./internal/convert -count=1` | rc 0, ok 374.269 s | ✓ PASS |
| Fork vs exec fixture shape | a `#!/bin/sh` script with `sleep 2` vs `exec sleep 2`, inspected with `ps` | fork shape: `sh` plus child `sleep` in one pgid; exec shape: a single `sleep` | ✓ PASS |

From the previous run, unchanged inputs, not repeated: the derivation reproduces byte-identically (`scripts/cad-dxf-bounds.sh derive76 --derive`), vitest 42/42, and decision coverage 19/19.

Per the instructions, I ran no docker, kubectl, helm or live stack, and no `go clean -cache`. All live evidence is the committed transcripts.

### Probe Execution

No `scripts/*/tests/probe-*.sh` is declared by the phase. Step 7c is not applicable. The live proofs are the committed transcripts above.

### Requirements Coverage

| Requirement | Source Plans | Status | Evidence |
|---|---|---|---|
| CADW-04 | 76-01..76-05, 76-08 | ✓ SATISFIED | SC1 |
| CADW-05 | 76-08..76-11 | ✓ SATISFIED | SC2, SC3 |
| CADW-06 | 76-06, 76-07 | ✓ SATISFIED | SC4 |
| PREV-01 | 76-06 | ✓ SATISFIED | SC5 |

No orphaned requirements: REQUIREMENTS.md maps exactly these four IDs to phase 76.

### Anti-Patterns and Comment Check

- **Debt markers:** no `TBD`, `FIXME` or `XXX` on added lines outside `.planning`. No TODO, HACK or PLACEHOLDER either. The 69fee40 diff adds none.
- **Added code comments, branch vs main (`git diff main...HEAD`), from the previous run:**
  - Go: 0 added lines starting `//`, `/*` or `*`; no trailing `//` comments.
  - Shell (7862 added lines): 0 added `#` lines apart from shebangs, and no trailing `#` comments.
  - TS/TSX: 0.
  - Python (`scripts/cad-corpus-gen.py`): 0.
  - CSS and YAML (chart, compose): 0.
  - 69fee40 changes three string literals and adds no comment.
- **Info:** `.env.example` rewrites its existing CAD provenance comment block, with 27 `#` lines added and 29 removed. This is the config-doc convention that TestEstimateSourceMatchesEnvExampleProvenance reads. It is not a Go, shell or TS file, so it is listed here and not counted as a violation.
- **Skips:** `TestCADCeilingsAtTheDefaults` and `TestCADEveryOperationRefusesAboveTheDerivedFormatCeiling` skip only under `-short`. Neither ci-local nor the runs above use `-short`. This is not a blocker.

| File | Line | Pattern | Severity | Impact |
|---|---|---|---|---|
| internal/convert/exec.go, internal/convert/progressexec.go | runCommandCore, runCommandStreaming | One group SIGKILL, then an unbounded `cmd.Wait` over pipes; on darwin a child forked at the kill instant escapes and holds Wait until it exits | ⚠️ Warning | Pre-existing, identical to main; tracked in the todo; affects macOS hosts (tests, a worker run outside Docker), not the Linux images per kernel history |
| internal/convert/cad_test.go | 494-535 | The 5 s bound can still trip under synthetic overload (the 3 to 14 s host-stall mode in the debug session) | ℹ️ Info | Not observed in this run's two passes |
| docs/CAD.md | Time limits | "Every default was measured, not assumed" while `CAD_RENDER_TIMEOUT` is the remainder of the 886 s budget (the same paragraph says so) | ℹ️ Info | Wording, not a contract error |

### Test Quality Audit

| Test File | Linked Req | Skipped | Circular | Assertion Level | Verdict |
|---|---|---|---|---|---|
| internal/convert/cad_test.go (76 pins), cad_doc_test.go | CADW-04, CADW-06 | 0 | no; expected values come from measured evidence lines, not the code under test | Value | OK |
| internal/convert/preview_doc_test.go | PREV-01 | 0 | no | Value | OK; the non-previewable code set is a literal in the test, while the codes' existence is held by cad_codes_doc_test |
| internal/convert/cad_openapi_doc_test.go, internal/worker/cad_codes_doc_test.go, internal/mcpserver/descriptions_test.go | CADW-06 | 0 | no | Value, both ways | OK |
| internal/api/cad_ceiling_test.go | CADW-04 | only under -short | no; reads DERIVED | Behavioral (413, nothing stored) | OK |
| frontend/src/formats.test.ts | CADW-06 | 0 | no | Value | OK |

Disabled tests on requirements: 0. Circular patterns: 0. The 69fee40 fixture change weakens no assertion: the debug session's no-kill overlay mutant still fails the fixed subtest.

### Decision Coverage

All 19 trackable CONTEXT.md decisions are honored by shipped artifacts.

### Warnings (not gaps)

1. The darwin killpg-vs-fork race in the engine runners (table above). It is outside phase 76's must-haves, and fixing it would break the binding "exec.go byte-identical to main" decision. It is tracked in `.planning/todos/pending/2026-10-17-darwin-killpg-fork-race.md`. That todo also covers checking shipped engines for `setsid` descendants, the remaining exposure on Linux.
2. The debug session's frontmatter still reads `status: awaiting_human_verify`, waiting on "the next full-suite gate". This run is that gate and it is green; archiving the session is left to the orchestrator.
3. Two deferred items from `deferred-items.md`, outside this phase's scope and recorded:
   - The chart's fresh install can starve api or minio on a small node. Both KEDA proofs used INSTALL_ASSIST and scaled the nine non-cad targets to 0 during install; cad-worker was untouched.
   - In the downscale-survival script, the deletion-cost annotation races the HPA by 0.26 to 1.26 s. The passing run is valid; the script order is fragile.
4. The KEDA proofs ran at 3af7906 and 86a4d05, not at 07d1024. SC2 does not require the final commit. The cad-worker image id in both (sha256:4c541fc…) is the same id that the final gate's sandbox step built from the 07d1024 clone, so the proofs ran the same cad-worker image bytes.

## Human Verification Required

None. The previous run's single item is closed:
- The cause of the 30.59 s wait is established with reproduction, falsification and mutation evidence.
- The fix is test-only and outside every image.
- The full suite and the package re-run requested for this re-verification are both green on go1.26.4.

## Gaps Summary

All five roadmap success criteria and all four requirements are achieved in the code and the committed evidence:
- the bounds are measured and pinned, and the derivation is reproducible;
- KEDA 0->2->0 and downscale survival PASS;
- the all-class gate is ALL PASS on images whose binaries HEAD reproduces, with zero orphans after a kill and determinism by tolerance;
- the docs are held both ways by tests;
- the preview contract is pinned with its "not fixed" list.

The cad RENDER_CEILING NOT_APPLICABLE row leaves no must-have unproven, because Path-B did not fire and that absence is held by tests.

There are no gaps and no human items. Status: `passed`.

---

_Verified: 2026-10-17T02:35:17Z_
_Verifier: Claude (gsd-verifier)_
