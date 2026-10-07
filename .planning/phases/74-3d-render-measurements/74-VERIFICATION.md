---
phase: 74-3d-render-measurements
verified: 2026-10-06T23:24:07Z
status: passed
score: 9/9 must-haves verified
covered_files: [".env.example",".planning/ci-local/ci-local-20261005T214321Z.log",".planning/phases/74-3d-render-measurements/74-01-PLAN.md",".planning/phases/74-3d-render-measurements/74-01-SUMMARY.md",".planning/phases/74-3d-render-measurements/74-02-PLAN.md",".planning/phases/74-3d-render-measurements/74-02-SUMMARY.md",".planning/phases/74-3d-render-measurements/74-03-PLAN.md",".planning/phases/74-3d-render-measurements/74-03-SUMMARY.md",".planning/phases/74-3d-render-measurements/74-04-PLAN.md",".planning/phases/74-3d-render-measurements/74-04-SUMMARY.md",".planning/phases/74-3d-render-measurements/74-05-PLAN.md",".planning/phases/74-3d-render-measurements/74-05-SUMMARY.md",".planning/phases/74-3d-render-measurements/74-06-PLAN.md",".planning/phases/74-3d-render-measurements/74-06-SUMMARY.md",".planning/phases/74-3d-render-measurements/74-07-PLAN.md",".planning/phases/74-3d-render-measurements/74-07-SUMMARY.md",".planning/phases/74-3d-render-measurements/74-CONTEXT.md",".planning/phases/74-3d-render-measurements/evidence/74-01-wave0.txt",".planning/phases/74-3d-render-measurements/evidence/74-02-mesh.txt",".planning/phases/74-3d-render-measurements/evidence/74-05-addoutputs-live.txt",".planning/phases/74-3d-render-measurements/evidence/74-06-judge.txt",".planning/phases/74-3d-render-measurements/evidence/74-06-landlock.txt",".planning/phases/74-3d-render-measurements/evidence/74-06-sandbox-liveproof.txt",".planning/phases/74-3d-render-measurements/evidence/74-07-liveproof.txt",".planning/phases/74-3d-render-measurements/evidence/74-08-liveproof.txt",".planning/phases/74-3d-render-measurements/evidence/74-08-stl-case.txt",".planning/quick/261006-t51-normalize-ascii-stl-case-for-cad-stats-a/261006-t51-PLAN.md",".planning/quick/261006-t51-normalize-ascii-stl-case-for-cad-stats-a/261006-t51-SUMMARY.md",".planning/quick/261007-1sx-per-shell-signed-volume-for-stl-and-per-/261007-1sx-PLAN.md",".planning/quick/261007-1sx-per-shell-signed-volume-for-stl-and-per-/261007-1sx-SUMMARY.md",".planning/research/SANDBOX-PATHS.md","Dockerfile.cad-worker","cmd/cad-worker/helper/cad-mesh","cmd/cad-worker/helper/cad-render","cmd/cad-worker/main.go","cmd/cad-worker/requirements-vtk.txt","cmd/cad-worker/vtk-keeplist.txt","cmd/cad-worker/vtk_keeplist.py","deploy/chart/octoconv/templates/configmap.yaml","deploy/chart/octoconv/values.yaml","docker-compose.yml","docs/ERROR-CODES.md","docs/MEASUREMENTS.md","internal/api/api.go","internal/api/cad_ceiling_test.go","internal/api/cad_opts_test.go","internal/api/formats_handlers_test.go","internal/api/handlers.go","internal/api/handlers_test.go","internal/api/pre58_pinning_test.go","internal/convert/cad.go","internal/convert/cad_test.go","internal/convert/cadmeasure.go","internal/convert/cadmeasure_test.go","internal/convert/cadopts.go","internal/convert/cadopts_test.go","internal/convert/cadpixel.go","internal/convert/cadpixel_test.go","internal/convert/cadrender.go","internal/convert/cadrender_test.go","internal/convert/cadsandbox.go","internal/convert/cadsandbox_test.go","internal/convert/convert.go","internal/convert/convert_test.go","internal/convert/converters.go","internal/convert/operations_test.go","internal/convert/sandboxcoverage_test.go","internal/convert/sandboxwrap.go","internal/jobs/add_outputs_test.go","internal/jobs/repo.go","internal/measurements/consistency_test.go","internal/measurements/doc_sync_test.go","internal/measurements/measurements.go","internal/measurements/measurements_test.go","internal/worker/cad_outputs_test.go","internal/worker/descriptors.go","internal/worker/pinning_test.go","internal/worker/probe_sideoutput_test.go","internal/worker/runner.go","internal/worker/webhook_role_test.go","internal/worker/worker.go","internal/worker/worker_test.go","scripts/cad-liveproof.sh","scripts/cad-render-probe.sh","scripts/cad-stl-case.sh","scripts/cad-unit-probe.py","scripts/cad-worker-sandbox-measure.sh","scripts/cmd/cad-measurements-check/main.go","scripts/cmd/cad-pixel-judge/main.go","scripts/conf05-crosscheck.sh","scripts/sandbox-liveproof.sh"]
covered_digest: "v3:sha256:f2fe0f3641bf51ebe6d918a459c01fd708b8c8a9145fa2c5e7024d7d0eb568e9"
behavior_unverified: 0
overrides_applied: 0
re_verification:
  previous_status: passed
  previous_score: 9/9
  previous_verified: 2026-10-06T17:45:35Z
  trigger: "verification.status stale: quick tasks 261006-t51 (a70085f..8ba83cf, ac81afe) and 261007-1sx (a4b877c..8dc219e, bd39505) changed covered files after 3224a9d"
  gaps_closed: []
  gaps_remaining: []
  regressions: []
gaps: []
deferred: []
advisory:
  - finding: "Positive STL shells (and closed STEP/IGES solids) are summed without an overlap check, so two intersecting but edge-disjoint bodies report a volume that counts their intersection twice"
    category: other
    reason: "Raised while reading stats_shells / solid_summary after 261007-1sx. docs/MEASUREMENTS.md states the rule truthfully ('when every shell encloses a positive volume, the volume is their sum'), the behaviour predates both quick tasks, and no fixture or test shows a client file hitting it. Resolving it would need a self-intersection or boolean-union check, or a documented limitation in the client docs (Phase 76)."
    evidence_status: "none provided"
coincidental_reliance_items:
  - truth: "SC4 units: STEP/IGES report the declared unit, numbers converted back from mm"
    reason: undeclared-precondition
    harden: "Carried forward unchanged. The factor used to convert back is the table value of the unit the helper's own regex reads from the STEP text (cad-mesh applied_factor), not a factor measured at run time; cadFactorMatches therefore compares the table with itself for STEP. Correct only while OCP resolves the same unit from the same file. Harden by cross-checking the text unit against OCP's own unit reading and reporting unspecified/none on disagreement; a CONVERSION_BASED_UNIT named INCH with a non-25.4 factor is the case the text reading would get wrong."
---

# Phase 74: 3D Render & Measurements Verification Report

**Phase goal:** Клиент получает PNG-превью детали и вместе с ним честные размеры; два выхода одной джобы видны оба или не виден ни один.
**Verified:** 2026-10-06T23:24:07Z on branch `gsd/phase-74-3d-render-measurements`, HEAD `bd39505`
**Status:** passed
**Re-verification:** Yes. The previous run (2026-10-06T17:45:35Z on `720cb93`, report committed in `3224a9d`) passed 9/9. `verification.status` then went `stale` because two Codex-review fixes changed covered files. This run re-checks all nine truths against the current tree.

## What changed since 3224a9d

`git diff --stat 3224a9d HEAD -- . ':!.planning'`, product files only:

| File | Change | Quick task |
|---|---|---|
| `cmd/cad-worker/helper/cad-mesh` | Case-insensitive ASCII STL keywords. `stats` writes a normalized binary `mesh.stl` (rebuilt records, zero normals and attributes, a non-finite float32 refuses the file). Per-shell signed STL volume via an edge-connected union-find. STEP/IGES: any closed solid with negative mass gives `negative_volume`. | t51, 1sx |
| `internal/convert/cad.go` | `statsStage` passes the `mesh.stl` output path | t51 |
| `internal/convert/cadrender.go` | `renderInput` is `jobDir/mesh.stl` for every format. An input at that path is refused. Every format goes through `judgeBinarySTL`, `requireSTLMatchesVerdict` and a new check that the draft triangles equal the mesh | t51 |
| `internal/measurements/measurements.go` | `ReasonSTLAmbiguousShells = "stl_ambiguous_shells"`, added to `nullReasons` and to `reasonsByFormat[stl]` only | 1sx |
| `docs/MEASUREMENTS.md` | STL parse rules, per-shell rule, the new reason row, the per-format reasons bullet | t51, 1sx |
| tests | `cadrender_test.go` (refusal table, STEP draft mismatch, input at mesh path), `cadmeasure_test.go`, `consistency_test.go`, `measurements_test.go` (vocabulary 6 to 7) | t51, 1sx |
| scripts | `cad-stl-case.sh` (new), `cad-liveproof.sh` (case rows, hollow STL row), `cad-render-probe.sh` and `cad-worker-sandbox-measure.sh` (stats argv) | t51, 1sx |

The sandbox and Landlock files are byte-identical to `3224a9d`: `exec.go`, `cadsandbox.go`, `sandboxwrap.go`, `Dockerfile.cad-worker`, `cmd/cad-worker/main.go`, `cad-guard`, `cad-render`, the vtk lock and keep-list, and `SANDBOX-PATHS.md`. The diff for them is empty.

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | SC1: `render` step/stp/iges/stl → png gives a non-empty, pixel-judged image. The default is iso, 1280, opaque. Different views give different images, with Z up. | ✓ VERIFIED | **The STL path changed and was re-proven.** In `cadrender.go` the STL branch now runs `statsStage(…, inPath, renderInput=jobDir/mesh.stl, …)`. Then every format runs `judgeBinarySTL(renderInput)`, then `requireSTLMatchesVerdict`, then the check that `draft.Triangles` equals the mesh count, and finally `renderStage(…, renderInput, …)`. `inPath` never reaches `renderStage`, and an `inPath` equal to the mesh path is refused before any stage runs. Tests run by name now: `TestCADRenderStlSourceSkipsTheGuardAndRunsStats` (exact argv), the four subtests of `TestCADRenderStlRefusesAnUnjudgedMeshBeforeAnyRender` (no mesh, verdict mismatch, the client's ASCII bytes, draft mismatch), `TestCADRenderStepDraftTrianglesMustEqualTheMesh` and `TestCADRenderRefusesAnInputAtTheMeshPath` (clean and unclean): all PASS. Live, `74-08-liveproof.txt` on 836acbc (product tree = HEAD, tracked_dirt 0, `sandbox_disabled=false`) gives `RENDER,name=stl_ascii_box_stl … judge=ok … w=1280,h=1280 … alpha=0`. The VIEWS rows show `distinct_sha256=3`, `front_h_over_w=0.5`, `top_h_over_w=1.5`, `z_up=1`, and the SUMMARY reads `render=5,views=6`. `74-08-stl-case.txt` on 95d9750 (tree = HEAD) shows nine admitted case variants that give byte-identical `mesh.stl` files, and every PNG passes the Go judge. |
| 2 | SC2: closed options give 422. `quality` changes tessellation for convert and render. `transparent` gives a fully transparent background. | ✓ VERIFIED | `cadopts.go`/`cadpixel.go` unchanged. The 836acbc liveproof gives `opts_reject=9` (all 422, jobs_created=0), `QUALITY … draft=3988,standard=5948,fine=14140,non_decreasing=1`, and the `background_transparent` row passes the judge. The unit tests pass. |
| 3 | SC3/MEAS-01 (3D half): two outputs, written atomically. The inversion test can fail against the naive sequence. `download_url` points to the PNG. | ✓ VERIFIED | `internal/jobs/repo.go`, `add_outputs_test.go` and `internal/worker/*` are unchanged since 3224a9d, so the live-DB transcript `74-05-addoutputs-live.txt` (3 PASS, no SKIP) still covers the current code. Live 836acbc: every RENDER row has `outputs=2,roles=primary@0;measurements@1`. EMPTY, CANCEL and CANCEL_RENDER show `outputs=0,download_url=absent`. |
| 4 | SC4/MEAS-02..04: null with a reason, never 0. Unit by source. `assume_unit` never overrides a declared unit. | ✓ VERIFIED (coincidental-reliance) | **The null-with-reason semantics were extended without breaking the contract.** (a) Code: `stats()` sets `vol_out, area_out` only when every edge-connected shell is positive. All negative gives `negative_volume`, mixed gives `stl_ambiguous_shells`, and any zero or non-finite shell gives `invalid_solid`. In every null case both values are `None`, never 0. In `solid_summary`, `elif negative:` gives `negative_volume` with both values null. (b) Closed vocabulary: `stl_ambiguous_shells` is in `nullReasons` and only in `reasonsByFormat[stl]`. `TestVocabularyExactMembers`, `TestVocabularyCounts` (7), `TestNullReasonFormatMatrix` (the reason is rejected for step/iges/dxf) and `TestSTLAmbiguousShellsIsAllowed` all PASS. The last one also rejects a mismatched area reason. (c) MEAS-05 docs↔code: `TestMeasurementsDocMatchesVocabularies` parses the Null reasons table and passes both ways. `TestMeasurementsDocNullReasonTableMatchesBehaviour` probes `Validate` over every format and count pair against the new row (`stl`, `none`) and PASS. (d) Live under the confined worker: `MEAS,name=stl_hollow_ambiguous … volume=null,area=null,reason=stl_ambiguous_shells,valid=1`. The three stl_case rows give 6000/2200. units_inch with `assume_unit=mm` stays `inch/declared`. STL with `assume_unit=mm` gives `client_assumed`. `surfaces_only_iges` gives `no_solids`, and `nasa_hubble_stl` gives `stl_not_watertight`. SUMMARY `meas=10`. (e) Teeth: in the 95d9750 SHELLS rows, the ac81afe helper published 875/750, 936/696, 875/750 and 1000/700 on out_in_disjoint, hollow, corner_touch_mixed and zero_shell. The fixed helper gives null with the expected reason. STEP_SIGN reaches the `negative_volume` branch in memory (`branch_reached=1`), where the old helper published 874.99. The coincidental-reliance item on the STEP text-unit factor is carried forward unchanged. |
| 5 | SC5 (extends CADW-02, CADW-03 for the render stage): the render stage has no OCP and its own measured ruleset, matched across SANDBOX-PATHS, Go and conf05. The preflight judges by pixels. Render failures are retryable, mesh failures are terminal. | ✓ VERIFIED | **There is no Landlock-relevant change.** The sandbox files are unchanged since 3224a9d (empty diff). `stats` writes `mesh.stl` into the job directory, which `forJob` already grants read-write (`rw := append([]string{jobDir}, …)`, sandboxwrap.go:179), so no grant was added. `scripts/conf05-crosscheck.sh cad` re-run now: MATCH cadguard (2/1), cadmesh (3/1), cadrender (3/1), multiarch (2), rc 0. The sandbox liveproof (`74-06-sandbox-liveproof.txt`, re-run on b14becc, tracked dirt 0) is `ALL PASS (5 checks)`, including `cad sandbox proven: cad-guard, cad-mesh and cad-render…` and emulated-amd64-refuses. `git diff b14becc HEAD -- . ':!.planning'` touches only `cad-mesh` (pure numpy shell and sign logic, no new path, import or exec), `measurements.go`, tests, docs and scripts. None of these is a sandbox input. The HEAD cad-mesh also ran confined: the 836acbc cad-liveproof asserts the worker logged `cad sandbox proven` (cad-liveproof.sh:135), refuses `CAD_SANDBOX_DISABLED` (lines 95, 144), and records `sandbox_disabled=false`, image `99952ab3…`, whose cad-mesh sha256 `3b8d1929…` equals HEAD's file (re-hashed now). The same run gives `render_retry=1` (TERM on cad-render: retried, done), `mesh_signal=1` (terminal, retried=0) and `cancel_render=1` (pid gone in 0.2 s). |
| 6 | Limits: engine timeout 886 s, under the 900 s reconciler window. Triangle cap 3,835,382 from Wave-0. | ✓ VERIFIED | The constants are unchanged. The 836acbc liveproof header shows `cad_engine_timeout_s=886`. The cap check stays in `cadrender.go` after the triangle cross-check, and `TestCADRenderTriangleCapBoundary` and `TestCADPinnedRenderCapsComeFromTheEvidence` pass in the full run. The STATS_RSS rows for the new stats (binary, ascii_upper, one-shell 3.8M triangles) stay below 70% of the memory limit and mesh timeout, with oom_kill 0. |
| 7 | Supply chain: the only additions are vtk from the 71 lock and libosmesa6. | ✓ VERIFIED | `cmp` re-run: `requirements-vtk.txt`, `vtk_keeplist.py` and `vtk-keeplist.txt` are identical to `scripts/cad-measure/`. `Dockerfile.cad-worker` is unchanged. The union-find uses numpy only, with nothing installed. |
| 8 | Invariants: `exec.go` unchanged, no new code comments, goldens and metric labels unchanged. | ✓ VERIFIED | `git diff main...HEAD -- internal/convert/exec.go scripts/cad-measure Dockerfile.cad-measure cmd/cad-worker/helper/cad-guard` is empty. `git diff 3224a9d HEAD -- '*.go'` adds no `//` lines. Among helper and scripts, the only added `#` line is the shebang of the new `scripts/cad-stl-case.sh`. There are no debt markers in added lines. |
| 9 | The final engine image builds for amd64 and refuses to run unconfined there. | ✓ VERIFIED | `74-06-sandbox-liveproof.txt` re-run on b14becc: `emulated image id: sha256:2e20aa9a… (amd64)`, `PASS: cad emulated-amd64-refuses`, `ALL PASS (5 checks)`. The Dockerfile and the import smoke are unchanged since then. The only image input that changed afterwards is the Python cad-mesh helper, which the build copies and does not import. |

**Score:** 9/9 truths verified (0 present-but-behavior-unverified)

### Evidence transcripts vs HEAD (product tree)

| Transcript | Taken on | `git diff --stat <commit> HEAD -- . ':!.planning'` | Verdict |
|---|---|---|---|
| 74-08-stl-case.txt | 95d9750 | empty | equals HEAD |
| 74-08-liveproof.txt | 836acbc | empty | equals HEAD |
| 74-06-sandbox-liveproof.txt | b14becc | cad-mesh, measurements.go (+tests), docs, cad-liveproof.sh, cad-stl-case.sh | Sandbox-relevant files equal HEAD. The cad-mesh delta is numeric only, and its confined run on the HEAD tree is covered by 74-08-liveproof (`cad sandbox proven` asserted) |
| 74-05-addoutputs-live.txt | 1950f68 | repo.go, add_outputs_test.go and worker unchanged | covers current code |
| 74-07-liveproof.txt | d93f6db | superseded by 74-08-liveproof for every row it had | historical |

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `cmd/cad-worker/helper/cad-mesh` | `--quality`, `--measure`, `stats src out`, per-shell volume | ✓ VERIFIED | sha256 equals the image used by both 74-08 transcripts |
| `cmd/cad-worker/helper/cad-render` | confined VTK render | ✓ VERIFIED | Unchanged; sha256 equals the image |
| `internal/convert/cadrender.go` | renders only the judged mesh.stl | ✓ VERIFIED | Wired, see truth 1 |
| `internal/measurements/measurements.go` | closed vocabulary with `stl_ambiguous_shells` | ✓ VERIFIED | Doc-sync and matrix tests pass |
| `docs/MEASUREMENTS.md` | reason row and per-format rules | ✓ VERIFIED | Parsed by doc_sync_test |
| `scripts/cad-stl-case.sh` | differential with baseline teeth | ✓ VERIFIED | ALL PASS on 95d9750 |
| `internal/jobs/repo.go` `AddOutputs`, `internal/worker/descriptors.go` | atomic two outputs | ✓ VERIFIED | Unchanged |
| `.planning/research/SANDBOX-PATHS.md` | conf05 blocks | ✓ VERIFIED | conf05 MATCH re-run |

### Key Link Verification

| From | To | Via | Status |
|---|---|---|---|
| `CADRenderConverter.Convert` (stl) | `cad-mesh stats --measure M src jobDir/mesh.stl` | `statsStage` argv, asserted exactly by test | WIRED |
| `statsStage`/`meshStage` output | `renderStage` input | the single `renderInput` variable, after `judgeBinarySTL` + verdict + draft-count checks | WIRED |
| cad-mesh draft `null_reason` | `measurements.Document.Validate` | `buildCADMeasurements` → `TestBuildCADMeasurementsNullReasons/stl_stl_ambiguous_shells` | WIRED |
| `docs/MEASUREMENTS.md` Null reasons | `nullReasons`, `reasonsByFormat` | `doc_sync_test.go` | WIRED |
| other links (handlers → opts, process() → AddOutputs, cad-worker main → CADRenderConverter) | | unchanged since 3224a9d | WIRED |

### Data-Flow Trace (Level 4)

| Artifact | Data | Source | Real data | Status |
|----------|------|--------|-----------|--------|
| measurements output | bbox, volume, area, reason, unit | cad-mesh stats/mesh → draft → `buildCADMeasurements` → sidecar → `AddOutputs` | yes (live MEAS rows incl. stl_hollow_ambiguous) | ✓ FLOWING |
| PNG primary | frame of normalized mesh.stl | cad-render under Landlock → `JudgeCADPNG` → upload | yes (live judge on downloaded bytes) | ✓ FLOWING |

### Behavioral Spot-Checks (this run, GOTOOLCHAIN=go1.26.4)

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Build | `go build ./...` | ok (go1.26.4 darwin/arm64) | ✓ PASS |
| Vet | `go vet ./...` | ok | ✓ PASS |
| Formatting | `gofmt -l .` | empty | ✓ PASS |
| Unit tests | `go test ./... -count=1` (one run) | 31 packages ok, internal/convert 162.3 s, rc 0, no FAIL | ✓ PASS |
| Vocabulary and doc sync | `go test -run 'TestMeasurementsDoc\|TestSTLAmbiguousShellsIsAllowed\|TestNullReasonFormatMatrix\|TestVocabulary' -v ./internal/measurements/` | 8 PASS | ✓ PASS |
| STL render path | `go test -run 'TestCADRenderStl\|TestCADRenderStepDraftTriangles\|TestCADRenderRefusesAnInputAtTheMeshPath\|TestBuildCADMeasurementsNullReasons' -v ./internal/convert/` | all PASS incl. subtests | ✓ PASS |
| Landlock sets match the doc | `scripts/conf05-crosscheck.sh cad` | 4 MATCH, rc 0 | ✓ PASS |
| vtk lock identity | `cmp scripts/cad-measure/* cmd/cad-worker/*` | 3/3 identical | ✓ PASS |
| Helper in image = tree | `shasum -a 256 cmd/cad-worker/helper/cad-{mesh,render}` | `3b8d1929…`, `e5dc03d8…` = 74-08-stl-case header | ✓ PASS |

Compose-backed live suites were not re-run by this verifier, as instructed. Their committed transcripts were checked against git history above.

### Probe Execution

| Probe | Result | Taken on | Status |
|-------|--------|----------|--------|
| `scripts/cad-stl-case.sh` → 74-08-stl-case.txt | `ALL PASS,sniff=10,baseline=9,case=9,nonfinite=1,shells=9,union=1,step_sign=4,stats_rss=3` | 95d9750, dirt 0 (tree = HEAD) | PASS |
| `scripts/cad-liveproof.sh` → 74-08-liveproof.txt | `ALL PASS … render=5,views=6,meas=10,opts_reject=9 …` | 836acbc, dirt 0 (tree = HEAD) | PASS |
| `scripts/sandbox-liveproof.sh cad` → 74-06-sandbox-liveproof.txt | `ALL PASS (5 checks)` | b14becc, dirt 0 (sandbox inputs = HEAD) | PASS |
| `scripts/conf05-crosscheck.sh cad` | MATCH ×4 | run now | PASS |

### Requirements Coverage

| Requirement | Status | Evidence |
|-------------|--------|----------|
| RNDR-01 | ✓ SATISFIED | Truth 1 (STL path re-proven), truth 9 |
| RNDR-02 | ✓ SATISFIED | Truth 1, truth 2 |
| RNDR-03 | ✓ SATISFIED | Truth 2 |
| RNDR-04 | ✓ SATISFIED | Truth 2 |
| RNDR-05 | ✓ SATISFIED | Truth 2 |
| MEAS-01 | ✓ SATISFIED for 3D | Truth 3; the DXF half is extended by Phase 75 SC2 |
| MEAS-02 | ✓ SATISFIED | Truth 4: the new reason keeps "null with a closed reason, never 0" |
| MEAS-03 | ✓ SATISFIED for STEP/IGES/STL | Truth 4; DXF half in Phase 75 |
| MEAS-04 | ✓ SATISFIED | Truth 4 |
| MEAS-05 (Phase 72, regression) | ✓ still holds | The doc↔code test covers `stl_ambiguous_shells` |

No orphaned requirements.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| (changed files since 3224a9d) | | TBD/FIXME/XXX/TODO/HACK | none | 0 in added lines (only `mktemp …XXXXXX` templates elsewhere) |
| `.planning/HANDOFF.json`, `.continue-here.md` | | pause handoff | resolved | Removed since 3224a9d |

### Advisory (New Scope, Unevidenced)

| # | Finding | Category | Why Advisory |
|---|---------|----------|--------------|
| 1 | Positive STL shells (and closed STEP/IGES solids) are summed with no overlap check. Two intersecting, edge-disjoint bodies would count their intersection twice. | other | The rule is documented truthfully in docs/MEASUREMENTS.md and predates both quick tasks. No fixture or test shows it. A candidate for the Phase 76 client docs. |
| 2 | STEP text-unit factor precondition (carried forward) | other | See coincidental_reliance_items |

### Human Verification Required

None.

### Gaps Summary

There are no gaps. Both review fixes keep the phase goal. The STL render now draws only a mesh that `cad-mesh stats` rebuilt and that Go judged and cross-checked against the verdict and the draft, so VTK never parses client bytes. The new `stl_ambiguous_shells` reason keeps MEAS-02's "null with a closed reason, never 0" contract: it is in the closed vocabulary for STL only, documented, and reconciled by the doc-sync tests. No sandbox input changed, conf05 still MATCHes, and the HEAD helper is proven confined by the 836acbc liveproof. Every evidence transcript was taken on a product tree equal to HEAD, or on one that differs only outside the sandbox. All local gates pass on go1.26.4.

---

_Verified: 2026-10-06T23:24:07Z_
_Verifier: Claude (gsd-verifier)_
