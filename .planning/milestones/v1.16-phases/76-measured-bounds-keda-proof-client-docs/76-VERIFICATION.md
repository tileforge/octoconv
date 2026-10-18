---
phase: 76-measured-bounds-keda-proof-client-docs
verified: 2026-10-17T10:00:48Z
status: passed
score: 5/5 roadmap success criteria verified (4/4 requirements satisfied)
covered_files: [".env.example",".planning/debug/cad-mesh-timeout-30s-wait.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-01-PLAN.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-01-SUMMARY.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-02-PLAN.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-02-SUMMARY.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-03-PLAN.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-03-SUMMARY.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-04-PLAN.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-04-SUMMARY.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-05-PLAN.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-05-SUMMARY.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-06-PLAN.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-06-SUMMARY.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-07-PLAN.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-07-SUMMARY.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-08-PLAN.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-08-SUMMARY.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-09-PLAN.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-09-SUMMARY.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-10-PLAN.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-10-SUMMARY.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-11-PLAN.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-11-SUMMARY.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/76-CONTEXT.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/deferred-items.md",".planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-01-stack.txt",".planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-02-bounds.txt",".planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-03-derived.txt",".planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-09-image-size.txt",".planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-09-keda-loadproof-test.sh",".planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-09-keda-loadproof.txt",".planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-10-keda-downscale-calibration.txt",".planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-10-keda-downscale.txt",".planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-11-final-gate-test.sh",".planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-11-final-gate.txt",".planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-11-frontend.txt",".planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-11-gate-runner.meta",".planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-11-liveproof.txt",".planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-11-sandbox-liveproof.txt",".planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/ci-local-20261016T230204Z.log",".planning/quick/261017-7y5-codex-p1-final-live-gate-cleanup-result-/261017-7y5-PLAN.md",".planning/quick/261017-7y5-codex-p1-final-live-gate-cleanup-result-/261017-7y5-SUMMARY.md",".planning/quick/261017-doa-codex-round-2-contaminated-loadproof-tim/261017-doa-PLAN.md",".planning/quick/261017-doa-codex-round-2-contaminated-loadproof-tim/261017-doa-SUMMARY.md",".planning/quick/261017-f0i-codex-round-3-cad-cleanup-fails-when-obj/261017-f0i-PLAN.md",".planning/quick/261017-f0i-codex-round-3-cad-cleanup-fails-when-obj/261017-f0i-SUMMARY.md",".planning/todos/pending/2026-10-17-cad-cleanup-mc-ls-json-live-confirmation.md",".planning/todos/pending/2026-10-17-darwin-killpg-fork-race.md",".planning/todos/pending/2026-10-17-selection-steps-ignore-load-clean.md","deploy/chart/octoconv/templates/configmap.yaml","deploy/chart/octoconv/values-loadproof.yaml","docker-compose.yml","docs/CAD.md","docs/ERROR-CODES.md","docs/MEASUREMENTS.md","docs/PREVIEW.md","docs/README.md","docs/openapi.yaml","frontend/src/App.tsx","frontend/src/formats.test.ts","frontend/src/formats.ts","frontend/src/styles.css","internal/api/cad_ceiling_test.go","internal/api/estimate.go","internal/convert/cad.go","internal/convert/cad_doc_test.go","internal/convert/cad_docs_sync_test.go","internal/convert/cad_openapi_doc_test.go","internal/convert/cad_test.go","internal/convert/caddxf.go","internal/convert/caddxf_test.go","internal/convert/caddxfpdf.go","internal/convert/caddxfpdf_test.go","internal/convert/caddxfsvg.go","internal/convert/cadpixel.go","internal/convert/cadpixel_test.go","internal/convert/cadpixelbg_test.go","internal/convert/cadpixelmeasure_test.go","internal/convert/cadrender_test.go","internal/convert/cadsandbox_test.go","internal/convert/preview_doc_test.go","internal/convert/testdata/cad/PROVENANCE.md","internal/convert/testdata/cad/render-bg/degenerate_plate_front_1280_opaque.png","internal/convert/testdata/cad/render-bg/degenerate_plate_front_1280_transparent.png","internal/convert/testdata/cad/render-bg/degenerate_rod_iso_256_opaque.png","internal/convert/testdata/cad/render-bg/degenerate_rod_iso_256_transparent.png","internal/convert/testdata/cad/render-bg/linearslide_iso_256_opaque.png","internal/convert/testdata/cad/render-bg/linearslide_iso_256_transparent.png","internal/convert/testdata/cad/render-bg/nauo4_iso_1280_opaque.png","internal/convert/testdata/cad/render-bg/nauo4_iso_1280_transparent.png","internal/convert/testdata/cad/render-bg/w15_iso_256_opaque.png","internal/convert/testdata/cad/render-bg/w15_iso_256_transparent.png","internal/e2e/cad_grace_chain_test.go","internal/mcpserver/client.go","internal/mcpserver/descriptions_test.go","internal/mcpserver/mcpserver.go","internal/mcpserver/tools.go","internal/worker/cad_codes_doc_test.go","internal/worker/pinning_test.go","scripts/cad-corpus-gen.py","scripts/cad-dxf-bounds.sh","scripts/cad-image-measure.sh","scripts/cad-inworker-measure.sh","scripts/cad-liveproof.sh","scripts/ci-local.sh","scripts/cmd/cad-pixel-judge/main.go","scripts/cmd/cad-png-compare/compare.go","scripts/cmd/cad-png-compare/compare_test.go","scripts/cmd/cad-png-compare/main.go","scripts/final-live-gate.sh","scripts/keda-cad-downscale-survival.sh","scripts/keda-cad-loadproof.sh"]
covered_digest: "v3:sha256:d7fa2f143923e9f453552de86585e47b33583e529f1b2fff711cf4797d5da977"
behavior_unverified: 0
overrides_applied: 0
re_verification:
  previous_status: passed
  previous_score: 5/5 roadmap success criteria verified (4/4 requirements satisfied)
  previous_commit: 9c4e602
  gaps_closed: []
  gaps_remaining: []
  regressions: []
---

# Phase 76: Measured Bounds, KEDA Proof & Client Docs Verification Report

**Phase Goal:** Границы класса измерены, а не скопированы; класс доказан вживую на образах из финального коммита; контракт превью и клиентская документация записаны и привязаны к коду.
**Verified:** 2026-10-17T10:00:48Z
**Status:** passed
**Re-verification:** Yes. The previous run (9c4e602, 2026-10-17T02:35:17Z) was `passed`. Since then, three quick tasks answered Codex review rounds on PR #37: 261017-7y5, 261017-doa and 261017-f0i. This run checks those changes for regression against the five success criteria and four requirements. It also checks whether they can invalidate the recorded live verdicts of 76-09, 76-10 and 76-11.

## Re-verification: what changed since 9c4e602

`git diff --stat 9c4e602 HEAD -- . ':!.planning'` lists five files. `git diff --name-only 1003655 HEAD` gives the same five:

| File | Change | Quick |
|---|---|---|
| `scripts/final-live-gate.sh` | Captures the exit status of every command whose output a verdict reads: git status (`dirt_count`, `unknown` fails preflight), preflight `docker ps`, the tectonic env inspect (`unreadable` fails the step), the sandbox leftover-tag listing (`result=` on CLEANUP, a leftover or failed listing fails the sandbox step), and the cad load file (a missing or empty file gives QUIET `result=fail`). `cad_cleanup` now returns a status, which `run_cad` folds into `ok` with `CAD_RUN cleanup=`. The cleanup discovers objects from one checked `mc ls --recursive --json` listing before deletion and a second one after. It fails on a failed or unparseable listing, on a job with no `uploads/` key, and on any object left under a run prefix. | 7y5, f0i |
| `scripts/keda-cad-loadproof.sh` | `quiet_line` returns non-zero on contamination. A contaminated calibration or timed step now FAILs the proof, where before it was recorded and could still pass. | doa |
| `scripts/keda-cad-downscale-survival.sh` | A contaminated calibration writes `NOTE key=quiet_calibration gates=0` and is pre-registered in RULE quiet_host. A contaminated LONG still fails. An unused `L_FMT` read field becomes `_` (it has no other reference). Two `done` literals are quoted. | doa |
| `docs/CAD.md` | The `near_white` Known-limits row now states the all-channel rule with examples (249,255,255) is ink and (250,250,250) is not. The old text said "any channel at or above". | doa |
| `internal/convert/cad_doc_test.go` | New subtest `near_white_rule`. It parses the doc's examples and runs them through `cadMeasure`, `JudgeCADPNG`, `JudgeCADDrawingPNG` and `JudgeCADDrawingInk`. It fails on the old wording. | doa |

Inside `.planning` the following changed. No evidence transcript changed: `git diff --stat 9c4e602 HEAD -- <phase>/evidence` lists only the two stub suites `76-09-keda-loadproof-test.sh` (new) and `76-11-final-gate-test.sh` (extended). The other changes are one `## Follow-up` paragraph each in 76-09/76-10/76-11-SUMMARY.md, the three quick task directories, two new todos and STATE.md. ROADMAP.md and REQUIREMENTS.md are unchanged.

### Can these changes affect a built image? No.

- Every runtime Dockerfile builds `go build -o /out/<bin> ./cmd/<bin>`, and its final stage copies only `/out/<bin>` plus non-Go assets from fixed paths: cad helpers and requirements under `cmd/cad-worker/`, and the tectonic bundle. No stage copies `scripts/*.sh`. `Dockerfile.cad-measure` copies only `scripts/cad-measure/*`, which is unchanged.
- `.dockerignore` excludes `*.md`, so `docs/CAD.md` is not in any build context. No `go:embed` outside tests reads docs/ or scripts/: the only embeds are migrations, two cad testdata files and `texresolvable.txt`.
- `cad_doc_test.go` and `cad_test.go` are `_test.go`. `Dockerfile.e2e` compiles only internal/e2e's own tests.
- So `git diff --name-only 07d1024..HEAD -- . ':!.planning'` (docs/CAD.md, the two convert test files and the three scripts) changes no image binary or asset. The images the final gate built at 07d1024 are the images HEAD builds.

### Can the stricter scripts invalidate the recorded live verdicts?

The recorded verdicts were produced by the older scripts. For each verdict, the check is whether the committed transcript also satisfies every condition the current script adds.

| Verdict | Condition the current script adds | Value in the committed transcript | Satisfied? |
|---|---|---|---|
| 76-09 loadproof PASS | a contaminated calibration or timed step fails | `76-09-keda-loadproof.txt:63` calibration contaminated=0 (10.46 to 8.71); `:78` timed contaminated=0 (8.82 to 8.49); `:81` SUMMARY PASS | **Yes** |
| 76-10 downscale PASS | a contaminated calibration is a NOTE (gates=0); a contaminated LONG fails (unchanged) | `76-10-keda-downscale.txt:67` calibration contaminated=0; `:96` long contaminated=0; `:98` PASS; `76-10-keda-downscale-calibration.txt:66` contaminated=0 | **Yes**; the NOTE would not be written |
| 76-11 gate ALL PASS | tectonic env must be readable | `76-11-final-gate.txt:37` `disabled_env_value=empty`, which is printed only when the inspect output contained `TECTONIC_SANDBOX_DISABLED=`; a failed read printed `unset` | **Yes** |
| | cad load file present with rows | `:46` QUIET rows=164; `76-11-liveproof-load.txt` has 184 lines | **Yes** |
| | start dirt readable | `:15` count=0, which a failed git status also printed; but `76-11-gate-runner.meta` records `tracked_dirt=0` at the clone, measured outside the wrapper | **Yes, by the independent record** |
| | preflight `docker ps` rc 0 | `:16` count=0 (ambiguous alone); the runner meta has `running_octoconv=0` | **Yes, by the independent record** |
| | cad cleanup returns ok | `:44` `result=ok` | **Yes** for the old checks |
| | cad cleanup: `jobs_without_upload=0` and `leftover_objects=0` from checked listings | not recorded: attempt 4 used the per-prefix `mc du --json ... \|\| true`. objects_deleted=302 equals the bucket drop 1884 to 1582, but a skipped prefix would be missing from both numbers | **Not re-proven** |
| | sandbox leftover tags 0 from a checked listing | `:41` `leftover_tags=0`, which a failed listing also printed; no independent record | **Not re-proven** |
| | end dirt readable | `:54` `tracked_dirt=0`; no independent record of the end state | **Not re-proven** |

#### Do the three not-re-proven conditions leave a must-have unproven? No.

- **cad MinIO leftovers and `jobs_without_upload`.** This is bucket hygiene after the cad rows ran. SC3 and CADW-05 ask for ALL PASS of every class, zero live processes after a mid-render kill, and determinism by tolerance. Those are proven by the 184 cad-liveproof rows (`76-11-liveproof.txt` `SUMMARY,result=ALL PASS`), the WORKER_KILL rows and the DETERMINISM rows. The cleanup runs after those rows and feeds none of them. The worst case is objects of the test client left in a dev bucket, which no proof row reads.
- **Sandbox leftover tags.** These are image tags left after the sandbox step. No proof row reads them. The sandbox verdict is `ALL PASS (45 checks)` in `76-11-sandbox-liveproof.txt`.
- **End dirt.** What ties the images to the commit is the tree state while they were built:
  - Start: the runner meta and the wrapper both record `tracked_dirt=0`, and the clone is a fresh clone of 07d1024.
  - At the cad step start (23:42:45Z): `76-11-liveproof.txt` HDR `tracked_dirt=0`, commit 07d1024. `scripts/cad-liveproof.sh` runs under `set -euo pipefail`, so a failed `git status` there would have aborted the run before `ALL PASS`. This read is fail-closed, and it covers the window after ci-local, tectonic and the sandbox builds.
  - The cad-worker image id from the sandbox step is the one the KEDA proofs ran (previous report, warning 4).
  - Only the window after the cad HDR is left without a checked read, and the frontend step builds no image.
- **mc JSON shape unmeasured live.** This limits only a future gate run. If the assumed shape is wrong, the current cleanup fails loudly: the `uploads/` anchor and `BAD` parse rows make it `result=fail`. It cannot produce a false PASS, and it does not touch any recorded verdict. It is tracked in `.planning/todos/pending/2026-10-17-cad-cleanup-mc-ls-json-live-confirmation.md`.

Precisely, then: the recorded 76-11 `SUMMARY,result=ALL PASS` is the verdict of the wrapper at 07d1024. Under the current wrapper, the same transcript satisfies every added condition except three: cad bucket leftovers, sandbox leftover tags and end dirt. None of these three is part of SC1 to SC5, CADW-04 to CADW-06 or PREV-01. They are recorded below as warnings, not gaps. If the hardened wrapper's own fail-closed rules need a live green, that needs a new live gate run, which this verification may not do (no docker, kubectl or helm).

## Goal Achievement

### Observable Truths

| # | Truth (ROADMAP SC) | Status | Evidence |
|---|---|---|---|
| 1 | `CAD_ENGINE_TIMEOUT` is measured over both operations (convert and render, 2D included) on a real corpus at the input ceiling. The value is the maximum over operations, paired with the input ceilings, and the formula and worst cell are recorded. The inspect estimate is marked measured. | ✓ VERIFIED (with the AR-1 reading, see below) | `evidence/76-03-derived.txt` DERIVED: guard 21, mesh 324, dxf 186, rsvg 217, io 20, render 521, engine = max(886, 661) = 886, worst_cell IR16MiB, and every ceiling. `internal/convert/cad.go` constants are pinned to those lines by TestCADConstantsComeFromThe76Evidence and TestCADEngineTimeoutIsTheLongerRoute. `internal/api/estimate.go` gives cad measured, 886. Nine live INSPECT rows give `estimate_source=measured,estimated_max_seconds=886`. **Regression:** none of cad.go, estimate.go, the evidence, compose, configmap or `.env.example` changed since 9c4e602. The pin tests passed in this run's full suite. |
| 2 | Scale-from-zero of `cad` through KEDA is proven on OrbStack k8s (0->N->0 with timestamps), together with downscale-survival. `keda.cad.minReplicaCount` is chosen deliberately, with the measured image size and cold pull recorded. | ✓ VERIFIED | `76-09-keda-loadproof.txt` PASS: 0->1 15:24:21.986Z, 1->2 15:24:32.667Z, 2->0 15:26:21.855Z, max 2, drain 98.6 s of 1290. `76-10-keda-downscale.txt` PASS: the busy pod finished LONG `done` with attempts=1 after SIGTERM and exited 0 at 162 s of its 906 s grace. `76-09-image-size.txt` IMAGE_SIZE, cold pull and DECISION value 0; `values.yaml keda.cad.minReplicaCount: 0`. **Regression:** both scripts became stricter about contamination, and both committed transcripts satisfy the stricter rule (table above). The new stub suite 76-09-keda-loadproof-test.sh passed 34/34 in this run. The docs/CAD.md Scaling block is still held by TestCADDocMatchesTheCode, which passed. |
| 3 | The live gate of all classes, `cad` included, ends `ALL PASS` on images built from the final commit. Killing the worker mid-render leaves zero live processes. A re-run of the same input on the same arch gives the same JSON, and the PNG is compared by tolerance, not by hash. | ✓ VERIFIED | `76-11-final-gate.txt`: commit 07d1024, six steps PASS, `VERDICT,fail_steps=0,not_run=0`, `SUMMARY,result=ALL PASS`, IMAGES_CHECK 13/13 and 2/2. No change since 07d1024 enters an image (see above). WORKER_KILL_RENDER/RSVG/mesh orphans=0, vm_orphans=0; six DETERMINISM rows json_equal=1 under RULE `hash=never`. **Regression:** the wrapper is stricter now. The transcript meets every added condition except three hygiene/end-state reads, none of which this SC names (section above). The stub suite 76-11-final-gate-test.sh passed 309/309 in this run. |
| 4 | Client docs (docs/CAD.md, error codes, docs/openapi.yaml, MCP descriptions and the reference frontend's format table) are held to the code by a test, as docs/ERROR-CODES.md already is. | ✓ VERIFIED | TestCADDocMatchesTheCode, TestCADCodesDocMatchesTheMapping, TestCADOpenAPIMatchesTheCode, TestMCPDescriptionsMatchTheRegistry, frontend `operationTable` from `/v1/formats`. **Change:** the `near_white` row was wrong. It said "any channel", while `cadpixel.go:226` requires all three channels >= `cadBackgroundCutoff` (250). It is now correct and held by the new `near_white_rule` subtest, which runs the doc's own examples through the four judges. This strengthens SC4. The full suite passed. |
| 5 | The preview contract (SEED-007) is written and pinned on CAD: format, sizes, background and behaviour on non-previewable input. Everything CAD did not force is marked "not fixed". | ✓ VERIFIED | docs/PREVIEW.md sections Fixed by CAD, Non-previewable input and Not fixed (11 items), held by TestPreviewDocMatchesTheCode. **Regression:** docs/PREVIEW.md unchanged since 9c4e602; the test passed in this run's full suite. |

**Score:** 5/5 truths verified (0 present-but-behavior-unverified). The behavior-dependent truths SC2 (downscale survival) and SC3 (zero processes after a kill) rest on directly observed live behavior in committed transcripts. Those transcripts are byte-unchanged since 9c4e602, and each still satisfies the current scripts' stricter verdict rules where those rules touch the proof rows.

#### SC1 under AR-1: what the 886 s means (unchanged)

- 886 s is the fleet budget: 900 s reconciler window, engine + 20 = 906 s pod grace. The 3D render deadline (521 s) is what remains after the measured guard, mesh and I/O.
- 76-01's stacking probe established `not_byte_bound` render cost for step, iges and stl, so Path-B fired for no format. Stacked-copy renders at the ceilings are refused `cad_too_complex`, proven live by three STACKED_REFUSAL rows. docs/CAD.md states it in Known limits.
- Every admitted, non-stacked render cell fits with margin (largest crosscheck ceil(1.5 x 194.90) = 293 s). The SC is achieved under this decision, which was resolved in 76-01 (AR-1) and pre-registered in RULE lines.

#### The cad RENDER_CEILING row is NOT_APPLICABLE: no must-have is left unproven (unchanged)

`76-11-liveproof.txt` records `NOT_APPLICABLE,row=RENDER_CEILING,reason=no_pathb_fired` with all four PATHB lines `fired=0`. The absence of a render byte ceiling is held by TestCADPathBFiredForNoFormat, TestCADRenderReChecksTheDerivedCeilingOfEveryFormat and TestCADEveryOperationRefusesAboveTheDerivedFormatCeiling, all green in this run.

### Required Artifacts

| Artifact | Status | Details |
|---|---|---|
| `internal/convert/cad.go` measured constants | ✓ VERIFIED | Unchanged; pinned to DERIVED lines |
| `internal/api/estimate.go` | ✓ VERIFIED | Unchanged; cad measured, 886 |
| `docs/CAD.md`, `docs/PREVIEW.md` | ✓ VERIFIED | CAD.md `near_white` row corrected and held by `near_white_rule`; PREVIEW.md unchanged |
| `internal/convert/cad_doc_test.go` | ✓ VERIFIED | +97 lines, one subtest, value-level against the real judges; no comment line |
| `docs/openapi.yaml`, MCP files, `frontend/src/*` | ✓ VERIFIED | Unchanged |
| `scripts/keda-cad-loadproof.sh`, `scripts/keda-cad-downscale-survival.sh` | ✓ VERIFIED | Stricter contamination verdicts; `bash -n` ok; shellcheck -S warning clean; loadproof stub suite 34/34 |
| `scripts/final-live-gate.sh` | ✓ VERIFIED | Fail-closed reads and checked bucket listings; `bash -n` ok; shellcheck clean; stub suite 309/309 |
| `scripts/cad-liveproof.sh`, `scripts/ci-local.sh`, `scripts/cad-image-measure.sh` | ✓ VERIFIED | Unchanged since 9c4e602 |

### Key Link Verification

No linked file in the previous table changed, except the two now verified here:
- `final-live-gate.sh` still calls `ci-local.sh` and `cad-liveproof.sh`.
- docs/CAD.md is still held by TestCADDocMatchesTheCode. Its new `near_white` link, docs/CAD.md to `cadBackgroundCutoff` and the four judges, is WIRED through `near_white_rule`.

| From | To | Status | Details |
|---|---|---|---|
| 76-03-derived.txt DERIVED | cad.go constants | WIRED | TestCADConstantsComeFromThe76Evidence |
| CADDefaultEngineTimeout | compose, configmap, .env.example, TTL, ShutdownTimeout, grace | WIRED | TestCADGraceChainFollowsTheEngineTimeout |
| docs/CAD.md tables | OperationClasses, CADOpts, constants, `cadMeasure`/Judge* | WIRED | TestCADDocMatchesTheCode (incl. near_white_rule) |
| /v1/formats operations | frontend table and select | WIRED | unchanged |
| OperationClasses | MCP descriptions | WIRED | TestMCPDescriptionsMatchTheRegistry |
| loadproof/image-size lines | docs/CAD.md Scaling | WIRED | TestCADDocMatchesTheCode scaling |
| final-live-gate.sh | ci-local.sh, cad-liveproof.sh | WIRED | unchanged call sites |

### Behavioral Spot-Checks (this run)

| Behavior | Command | Result | Status |
|---|---|---|---|
| gofmt | `gofmt -l .` | empty | ✓ PASS |
| vet | `GOTOOLCHAIN=go1.26.4 go vet ./...` (go1.26.4 darwin/arm64) | rc 0 | ✓ PASS |
| Full test suite | `GOTOOLCHAIN=go1.26.4 go test ./... -count=1`, py314 (Python 3.14.8) first on PATH, 09:43:23Z to 09:53:31Z | **rc 0**: 32 packages ok, no FAIL or panic lines; internal/convert ok 595.793 s, internal/e2e ok 3.723 s, internal/worker ok 13.299 s | ✓ PASS |
| 76-09 loadproof stub suite | `perl -e 'alarm 1200; exec @ARGV' bash evidence/76-09-keda-loadproof-test.sh` | rc 0, `RESULT: 34 passed, 0 failed` (27 s) | ✓ PASS |
| 76-11 final-gate stub suite | `perl -e 'alarm 1200; exec @ARGV' bash evidence/76-11-final-gate-test.sh` | rc 0, `RESULT: 309 passed, 0 failed` (4 min 53 s) | ✓ PASS |
| exec.go identical to main | `git rev-parse main:internal/convert/exec.go HEAD:internal/convert/exec.go`; `git diff --quiet main HEAD -- internal/convert/exec.go`; worktree diff | both `8c0b16cf7a1876bd3fcba2efe69ea0da8739f391`; no diff; worktree clean | ✓ PASS |
| Changed scripts parse and lint | `bash -n` on the three scripts; `shellcheck -S warning` | ok; rc 0, no findings | ✓ PASS |
| Committed transcripts untouched | `git diff --stat 9c4e602 HEAD -- <phase>/evidence` | only the two stub suites | ✓ PASS |
| Working tree after the stub suites | `git status --porcelain` | only the pre-existing `.planning/config.json`, `.vscode/`, `octoconv.code-workspace` | ✓ PASS |

Per the instructions, I ran no docker, kubectl, helm or live stack, and no `go clean -cache`. The stub suites put stubs first on PATH for docker, kubectl, helm, orbctl and the other tools. All live evidence is the committed transcripts.

### Probe Execution

No `scripts/*/tests/probe-*.sh` is declared by the phase. Step 7c is not applicable.

### Requirements Coverage

| Requirement | Source Plans | Status | Evidence |
|---|---|---|---|
| CADW-04 | 76-01..76-05, 76-08 | ✓ SATISFIED | SC1 |
| CADW-05 | 76-08..76-11 | ✓ SATISFIED | SC2, SC3; the hardened scripts do not invalidate the recorded verdicts (section above) |
| CADW-06 | 76-06, 76-07 | ✓ SATISFIED | SC4, strengthened by `near_white_rule` |
| PREV-01 | 76-06 | ✓ SATISFIED | SC5 |

No orphaned requirements. REQUIREMENTS.md, which is unchanged, maps exactly these four IDs to phase 76.

### Anti-Patterns and Comment Check

- **Debt markers:** no `TBD`, `FIXME`, `XXX`, `TODO`, `HACK` or `PLACEHOLDER` on lines added outside `.planning` since 1003655.
- **Added comment lines, branch vs main (`git diff main...HEAD`):**
  - Go: 0 added lines starting `//`, `/*` or `*`, and no trailing `//`.
  - Shell, including the `.planning` stub suites: 0 added `#` lines apart from shebangs, and 0 trailing ` # ` (excluding `$#`/`${#`).
  - TS/TSX: 0.
  - Since 1003655, no Go or shell comment line was added.

| File | Line | Pattern | Severity | Impact |
|---|---|---|---|---|
| internal/convert/exec.go, progressexec.go | runCommandCore, runCommandStreaming | One group SIGKILL, then an unbounded `cmd.Wait`; the darwin killpg-vs-fork race | ⚠️ Warning | Pre-existing, identical to main; `.planning/todos/pending/2026-10-17-darwin-killpg-fork-race.md` |
| scripts/final-live-gate.sh | cad_bucket_list | `mc ls --json` shape assumed from upstream mc, unmeasured against the pinned chainguard image | ⚠️ Warning | A wrong shape fails the next gate loudly (BAD rows, uploads anchor), never a false PASS; todo `2026-10-17-cad-cleanup-mc-ls-json-live-confirmation.md` |
| scripts (two selection steps, per 261017-doa sweep) | — | read load-contaminated rows without a stub harness | ℹ️ Info | Err toward FAIL, never a false PASS; todo `2026-10-17-selection-steps-ignore-load-clean.md` |
| internal/convert/cad_test.go | 494-535 | 5 s bound can trip under synthetic overload | ℹ️ Info | Not observed; this run's internal/convert took 595.8 s (slower than the previous 382 s) and still passed |

### Test Quality Audit

| Test File | Linked Req | Skipped | Circular | Assertion Level | Verdict |
|---|---|---|---|---|---|
| internal/convert/cad_doc_test.go `near_white_rule` | CADW-06 | 0 | no; the expected counts come from the doc's examples, and the code under test is the real judges | Behavioral (pixel counts, ErrCADEmptyResult) | OK; per 261017-doa it fails on the old wording (evidence/red-doc.txt) |
| evidence/76-09-keda-loadproof-test.sh | CADW-05 | 0 | no; runs the real script against stubs | Behavioral (exit, SUMMARY, FAIL lines) | OK; base vs fix 23/11 to 34/0 per 261017-doa |
| evidence/76-11-final-gate-test.sh | CADW-05 | 0 | no | Behavioral | OK; the 261017-f0i mutations M1 to M7 all killed |
| (unchanged rows from the previous run) | CADW-04, CADW-06, PREV-01 | 0 | no | Value | OK |

### Warnings (not gaps)

1. The darwin killpg-vs-fork race in the engine runners (unchanged, tracked).
2. **The hardened 76-11 wrapper is not proven live.** The recorded ALL PASS meets every added condition except three, none of them a must-have:
   - cad bucket leftovers and `jobs_without_upload`;
   - sandbox leftover tags;
   - end dirt.

   The mc JSON shape the new cleanup reads is unmeasured live. A new live gate run would close all four. Until then, a future gate run could fail on the cleanup if the shape assumption is wrong.
3. Deferred items from `deferred-items.md` (chart install starvation on a small node; the deletion-cost annotation race in the downscale script) are unchanged.
4. The KEDA proofs ran at 3af7906 and 86a4d05, not at 07d1024. SC2 does not require the final commit, and the cad-worker image id is the same one the final gate built.
5. The debug session `.planning/debug/cad-mesh-timeout-30s-wait.md` is now `status: resolved` (1003655), which closes the previous run's warning 2.

## Human Verification Required

None.

## Gaps Summary

There are no regressions and no gaps:
- The five code and doc changes since 9c4e602 only make two verdicts stricter: the contamination rule in the KEDA proofs, and the fail-closed reads and checked cleanup in the final gate.
- They correct one doc row and pin it with a test.
- No image is affected, and no committed transcript changed.
- 76-09 and 76-10 satisfy the stricter rules outright.
- 76-11 satisfies them except for three post-proof hygiene and end-state reads: cad bucket leftovers, sandbox tags and end dirt. These are recorded above with why none of them backs SC1 to SC5 or a requirement.
- gofmt, vet, the full suite and both stub suites are green, and exec.go is byte-identical to main.

Status: `passed`.

---

_Verified: 2026-10-17T10:00:48Z_
_Verifier: Claude (gsd-verifier)_
