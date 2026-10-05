---
phase: 73-cad-class-convert-slice-confined
verified: 2026-10-05T14:10:00Z
status: passed
score: 16/16 must-haves verified
covered_files:
  - .env.example
  - .planning/REQUIREMENTS.md
  - .planning/ROADMAP.md
  - .planning/phases/73-cad-class-convert-slice-confined/73-01-PLAN.md
  - .planning/phases/73-cad-class-convert-slice-confined/73-01-SUMMARY.md
  - .planning/phases/73-cad-class-convert-slice-confined/73-02-PLAN.md
  - .planning/phases/73-cad-class-convert-slice-confined/73-02-SUMMARY.md
  - .planning/phases/73-cad-class-convert-slice-confined/73-03-PLAN.md
  - .planning/phases/73-cad-class-convert-slice-confined/73-03-SUMMARY.md
  - .planning/phases/73-cad-class-convert-slice-confined/73-04-PLAN.md
  - .planning/phases/73-cad-class-convert-slice-confined/73-04-SUMMARY.md
  - .planning/phases/73-cad-class-convert-slice-confined/73-05-PLAN.md
  - .planning/phases/73-cad-class-convert-slice-confined/73-05-SUMMARY.md
  - .planning/phases/73-cad-class-convert-slice-confined/73-06-PLAN.md
  - .planning/phases/73-cad-class-convert-slice-confined/73-06-SUMMARY.md
  - .planning/phases/73-cad-class-convert-slice-confined/73-CONTEXT.md
  - .planning/phases/73-cad-class-convert-slice-confined/evidence/73-01-spikes.txt
  - .planning/phases/73-cad-class-convert-slice-confined/evidence/73-03-landlock.txt
  - .planning/phases/73-cad-class-convert-slice-confined/evidence/73-06-liveproof.txt
  - .planning/phases/73-cad-class-convert-slice-confined/evidence/73-06-sandbox-liveproof.txt
  - .planning/research/SANDBOX-PATHS.md
  - Dockerfile.cad-worker
  - cmd/cad-worker/helper/cad-guard
  - cmd/cad-worker/helper/cad-mesh
  - cmd/cad-worker/main.go
  - cmd/cad-worker/requirements-mesh.txt
  - deploy/chart/octoconv/templates/deployment-cad-worker.yaml
  - deploy/chart/octoconv/templates/scaledobject-cad.yaml
  - deploy/chart/octoconv/values.yaml
  - docker-compose.yml
  - docs/ERROR-CODES.md
  - docs/openapi.yaml
  - internal/api/api.go
  - internal/api/cad_ceiling_test.go
  - internal/api/estimate.go
  - internal/api/handlers.go
  - internal/convert/cad.go
  - internal/convert/cad_test.go
  - internal/convert/cadsandbox.go
  - internal/convert/cadsandbox_test.go
  - internal/convert/convert.go
  - internal/convert/converters.go
  - internal/convert/exec.go
  - internal/queue/client.go
  - internal/queue/queue.go
  - internal/reconciler/reconciler.go
  - internal/sandbox/helper.go
  - internal/worker/descriptors.go
  - internal/worker/pinning_test.go
  - scripts/cad-liveproof.sh
  - scripts/cad-measure/requirements-common.txt
  - scripts/cad-measure/requirements-ocp-7.9.3.1.1.txt
  - scripts/ci-helm-checks.sh
  - scripts/conf05-crosscheck.sh
covered_digest: "v3:sha256:2c53d2b41509f8884eedab24431fd1bcb7cab61a2deac9abf7e5d9b7db84e27b"
behavior_unverified: 0
overrides_applied: 0
re_verification: null
---

# Phase 73: CAD Class & Convert Slice, Confined - Verification Report

**Phase Goal:** The `cad` class appears in the API as a whole and at once under a measured Landlock ruleset: STEP/IGES -> STL through the queue, the worker, preflight and error classification.
**Verified:** 2026-10-05
**Status:** passed
**Re-verification:** No, initial verification

## Cheap checks run by the verifier

| Check | Result |
| ----- | ------ |
| `GOTOOLCHAIN=go1.26.4 go build ./... && go vet ./... && go test ./...` | all packages ok, no FAIL (convert 106 s) |
| `scripts/conf05-crosscheck.sh cad` | `MATCH: cadguard readonly (2) engines (1)`, `MATCH: cadmesh readonly (3) engines (1)`, `MATCH: cad multiarch (2)`, exit 0 |
| `scripts/ci-helm-checks.sh` | exit 0 |
| `go test ./internal/errcode -run Doc\|Sync` | `TestErrorCodesDocMatchesGoConstants` and the historical, shared-code and unknown-fallback tests PASS |
| `git diff main..HEAD -- internal/convert/exec.go internal/metrics` | empty |
| Added-line scan for TBD/FIXME/XXX/TODO/HACK | only `mktemp ...XXXXXX` templates; no debt markers |

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
| - | ----- | ------ | -------- |
| 1 | SC1: step/stp/iges/igs -> stl returns exactly one binary STL in mm; inch STEP gives mm geometry | VERIFIED | `evidence/73-06-liveproof.txt` CONVERT rows: `units_inch_step` outputs=1 primary=1 triangles=12 extents=25.4:50.8:76.2 (declared inches -> mm); nist STEP, `.igs` and `.iges` festo valve all `judge=ok`. `cad-mesh` pins `xstep.cascade.unit=MM`, writes `ASCIIMode=False`. `CADPreflight` re-proves the inch fixture at 25.4/50.8/76.2 mm on every start. |
| 2 | SC1: exposure is atomic (one change to `AllEngines()` with queue, enqueue, reconciler, timeout/estimate/size maps, compose, Deployment, ScaledObject) | VERIFIED | `git show --stat 49729fd` is one commit touching `internal/convert/convert.go` (AllEngines + `EngineCAD`), `converters.go` (Register), `queue.go` (AllConvertQueues), `api.go`/`handlers.go` (EnqueueCADJob + maps), `reconciler.go`, `queue/client.go`, `docker-compose.yml`, `deployment-cad-worker.yaml`, `scaledobject-cad.yaml`, `values.yaml`, `configmap.yaml`. Earlier commits (`d266f07`, `56a4f94`, `831c04b`) add only the `EngineCAD` constant, converter, descriptor and an inert per-format ceiling; none registers `cad` in `AllEngines`/`Default`/`AllConvertQueues`. Live FORMATS row: exactly `iges>stl;step>stl` under cad, `other_pairs_touching_cad_formats=0`. |
| 3 | SC1: inspect estimate is `unmeasured` | VERIFIED | `internal/api/estimate.go` maps `EngineCAD: EstimateSourceUnmeasured`; live INSPECT row `estimate_source=unmeasured,estimated_max_seconds=406,max_bytes=66060288`. |
| 4 | SC1: other nine classes, metric labels and goldens unchanged | VERIFIED | No `internal/metrics` change; no `*.golden.json` / `testdata` golden file is in `git diff main..HEAD`. Comparators strip only `cad` (`withoutEngine`) and each now asserts the live body does contain `cad`, so the strip cannot hide a regression (`golden_pinning_test.go`, `pre58_pinning_test.go`). `AllConvertQueues` appends `QueueCAD` last. Full suite green. |
| 5 | SC2: `cad-worker` refuses to start without Landlock and names `CAD_SANDBOX_DISABLED` | VERIFIED | `cmd/cad-worker/main.go` calls `sandbox.ResolveHelper("CAD_SANDBOX_DISABLED")` first and `log.Fatalf`s; the error text names the variable. Live: `evidence/73-06-sandbox-liveproof.txt` `PASS: cad refuses-without-landlock` and `PASS: cad emulated-amd64-refuses` print `Set CAD_SANDBOX_DISABLED=true to accept that deliberately`; `escape-hatch` PASS. Only the exact string `true` disables it (`helper.go`). |
| 6 | SC2: preflight runs before Postgres and judges by content on the real engine | VERIFIED | In `main.go` `convert.CADPreflight` precedes `db.Connect`. Live log order: "cad sandbox proven" then `postgres: ping ... failed`. `CADPreflight` (`cadsandbox.go`) runs the real `cad-guard` and `cad-mesh` under their rulesets, judges entity count, triangle count (12) and sorted mm extents (inch fixture), then requires both stages to be denied for a file outside the job directory; a stage that merely could not establish the sandbox does not count as a denial (`requireCADDenied`). `preflight-catches-restrict-self` PASS (exit 78 -> startup refusal). |
| 7 | SC2: SANDBOX-PATHS `## cad`, the Go sets and `conf05-crosscheck.sh cad` are equal | VERIFIED | Crosscheck exit 0 with three MATCH lines. Go: guard RO `/usr/bin/python3.11`, `/usr/lib/python3.11` (+ multiarch), mesh adds `/opt/venv`; guard set has no path to the reader (`TestCADGuardSetHasNoPathToTheReader`). Document records both sets, deletion tables with load-bearing entries, and negative controls; `evidence/73-03-landlock.txt` is the transcript. Measured on the production engine stage, not the 71 VTK image. |
| 8 | SC2: an engine pointed at a file outside the job directory is refused | VERIFIED | `73-03-landlock.txt` EFFECT rows (candidate and measured set, both stages): `unconfined=ok,result=denied,stl_produced=no,confined_rc=2` with the `/etc/hostname` canary read before the ruleset; tracer row `units_inch_outside ... result=denied`; preflight re-checks on every start. |
| 9 | SC3: bomb inputs end as terminal coded failures within measured bounds, worker alive, no OOMKilled | VERIFIED | Live BOMB rows (11): entity flood 754763, NAUO 1519, hierarchical NAUO K12, MAPPED_ITEM text K12 -> `cad_too_complex` in 2.3-2.6 s against bound 30 s; self-referential and 2-cycle STEP, `abc`, `--`, `1e300`, `1e999`, `nan` coordinates -> `cad_unreadable` in about 2.4 s against bound 406 s; every row `oom_killed=false,restart_count=0,started_at_unchanged=1,oom_kill=0`. The lying-triangle-count STL is refused at the API (`422 truncated_cad_file`, no CAD STL source pair exists in this phase) and `judgeBinarySTL` re-checks declared count against size on the output (`TestCADJudgeBinarySTL`). Evidence run is on commit `ce195dd`; nothing outside `scripts/cad-liveproof.sh` changed after it (`git diff --stat 922025f HEAD -- . ':!.planning'`). |
| 10 | SC3: spike RULE lines are implemented in the helpers | VERIFIED | `load_check=numeric_strict` -> `cad-mesh load_check()` counts only "not a Real"/"not an Integer" fails, any numeric fail -> `unreadable/load_check_fail`; `assembly_cycle=reader_handles` -> no guard cycle logic, live cycle rows terminal `cad_unreadable`; `expansion=guard_bounds_expanded_instances` -> `expanded_instances()` topological path count, cap 1518; `mapped_item=guard_bounds` -> `mapped_item_edges()` + same cap; `entity_counter=semicolons_minus_3` -> `count_entities()` with `STRUCTURAL_TERMINATORS = 3`, caps 754762/1518 pinned in `cad.go` and in the measurement argv. Bindings `unit_pin` and `check_list` are the `cad-mesh` calls. |
| 11 | SC4: failure classification by stage, four new codes synced with docs | VERIFIED | `worker/descriptors.go`: `cad_too_complex`, `cad_unreadable`, `cad_empty_result`, `cad_mesh_timeout` in `allFailureCodes`; over-ceiling maps to existing `input_too_large`; `ErrEngineSandboxUnavailable` is retryable and wins over any terminal-looking wrap; unrelated deadlines (S3 stall), cancellation and plain engine errors stay retryable or `engine_error`. `TestPinnedCADFailureVerdicts` pins verdict, SkipRetry, code and message per sentinel. `docs/ERROR-CODES.md` lists all four ("Nineteen codes") and `internal/errcode` doc-sync tests pass. `docs/openapi.yaml` enumerates only the synchronous `errcode` vocabulary (no worker codes such as `engine_error` or `reconciler_exhausted` appear there), so nothing to sync in this phase; OpenAPI/MCP CAD coverage is CADW-06 in phase 76. |
| 12 | SC4: worker killed mid-job leaves no live engine and does not hold the job for the full timeout | VERIFIED | Live WORKER_KILL: `killed_mesh_pids=353`, restart, `orphans=0`, `fresh_mesh_at_restart=1`, job `done` with events `queued;queued>active;active>active;active>done`, `kill_to_done_s=112.7` against `cad_engine_timeout_s=406`, `reconciler_running=0` (recovery via asynq, not the reconciler). CANCEL row: `mesh_pids=326 pid_gone=1 gone_s=0.3 status=canceled`. `TestCADCancelMidMeshLeavesNoProcessOfTheGroupAlive` passes; the run goes through unchanged `exec.go` (process-group kill). |
| 13 | Units are mm; IGES ceiling 135 MiB supersedes 71's 155 MiB with derivation; STEP ceiling 63 MiB | VERIFIED | `cad.go`: `CADMaxIGESBytes = 135 << 20` (141557760), `CADMaxSTEPBytes = 63 << 20` (66060288), both equal to the DERIVED rows in `73-01-spikes.txt`. Derivation checked: IGES points 135.4 MiB -> 2703 MiB peak (<= 2867 = 70 % of 4096), 148.0 MiB -> 2925 MiB (over), so the largest clean row floors to 135 MiB; the GAP row `iges_ceiling_supersedes_71` states the supersession. STEP: largest measured copy row 61.6 MiB at 1150 MiB, so 71's 63 MiB is kept. Live REJECT `step_63mib_plus_1` -> `413 payload_too_large` naming the format. Documented in `ERROR-CODES.md` and `.env.example`. |
| 14 | Timeouts derived with arithmetic shown | VERIFIED | Recomputed from the DERIVED rows: guard 30 s (3 x 0.32 s largest count wall, under 30); mesh 316 s = max(316, ceil(1.5 x 84.62) = 127), 316 = ceil(210.63 x 1.5) from the 71 ladder; I/O 60 s (assumption, labelled); engine 30+316+60 = 406 s; pod grace 406+20 = 426 s. Compose and chart use 406/426; `ShutdownTimeout` = engine+10 s; `ValidateCADTimeouts` enforces guard+mesh+I/O <= engine; 406 s is under the 900 s reconciler cap. `.env.example` shows the sum and says "DERIVED, not measured"; the estimate is labelled `unmeasured`. |
| 15 | `exec.go` unchanged; supply chain limited to approved hashes plus ca-certificates | VERIFIED | `git diff main..HEAD -- internal/convert/exec.go` empty. `Dockerfile.cad-worker`: base pinned by digest `sha256:3783cc...` (same as 71), apt `python3 python3-venv` (build stage only) and `python3 libgl1 libx11-6 ca-certificates` in the engine stage, all in the 71 list plus ca-certificates; pip with `--only-binary --no-deps --require-hashes`. `requirements-mesh.txt` has 3 pins (`numpy==2.4.6`, `cadquery-ocp-novtk==7.9.3.1.1`, `cadquery-ocp-proxy==7.9.3.1.1`) and 75 hashes, all 75 present in `scripts/cad-measure/requirements-common.txt` + `requirements-ocp-7.9.3.1.1.txt` (`comm -23` empty). No vtk, ezdxf or font packages. |
| 16 | No code comments outside the allowed exceptions | VERIFIED (advisory) | New Go, Python helper, Dockerfile, compose and chart files contain no comments (only the `//go:embed` directive). Added-line scan finds comments only in edited pre-existing comment blocks (`convert.go` AllEngines doc, `reconciler.go`, test comments updated for the new count) and in shell script header blocks (see Anti-Patterns). |

**Score:** 16/16 truths verified (0 present, behavior-unverified)

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | -------- | ------ | ------- |
| `cmd/cad-worker/main.go` | confined worker entry | VERIFIED | refuses without Landlock, preflight before `db.Connect`, concurrency fixed at 1 |
| `cmd/cad-worker/helper/cad-guard`, `cad-mesh` | stdlib guard and OCP mesher | VERIFIED | substantive, rules above implemented, `-IB` interpreter flags |
| `internal/convert/cad.go`, `cadsandbox.go` | two confined stages, judge, preflight | VERIFIED | wired through `Default.Register(CADConverter{})` and `ConfinedCAD` |
| `internal/worker/descriptors.go` | `CADDescriptor`, four codes | VERIFIED | wired in `cmd/cad-worker/main.go` via `NewRunner` |
| `Dockerfile.cad-worker`, `requirements-mesh.txt` | hash-pinned engine image | VERIFIED | subset of 71 locks |
| compose `cad-worker`, chart Deployment + ScaledObject | deployment surface | VERIFIED | `ci-helm-checks.sh` exit 0; grace 426 s, 2 CPU / 4 GiB |
| `SANDBOX-PATHS.md` `## cad` | measured sets | VERIFIED | held by `conf05-crosscheck.sh cad` and `TestCADSetsAreTheMeasuredOnes` |
| `evidence/*.txt` | spikes, Landlock, live proofs | VERIFIED | self-consistent: image ids, commit `ce195dd` clean (`tracked_dirt=0`), timeouts 406 match code |

### Key Link Verification

| From | To | Via | Status |
| ---- | -- | --- | ------ |
| `AllEngines()` | `Default` registry, queues, API enqueue, reconciler | `EngineCAD` | WIRED (`49729fd`) |
| API create | `EnqueueCADJob` -> queue `cad` -> `cad-worker` mux `TypeCADJob` | `NewRunner(h, CADDescriptor)` | WIRED, proven end-to-end by live CONVERT rows |
| `cad-worker` startup | `CADPreflight` -> Postgres | ordering in `main.go` | WIRED, live log order |
| Go sets | `SANDBOX-PATHS.md` | `conf05-crosscheck.sh cad` | WIRED, MATCH x3 |
| `cadFailureCode` | `ERROR-CODES.md` | `internal/errcode` doc-sync test | WIRED, PASS |

### Data-Flow Trace (Level 4)

STL bytes come from `StlAPI_Writer` over a real `reader.OneShape()`; `judgeBinarySTL` re-reads the file and the live proof records sha256, triangle count and extents per job. No hardcoded or static path found.

### Behavioral Spot-Checks

Not re-run by decision (live Docker proofs trusted from committed evidence); the Go suite including `TestCADCancelMidMeshLeavesNoProcessOfTheGroupAlive`, `TestPinnedCADFailureVerdicts`, `TestCADPinnedConstantsComeFromTheEvidence` and the doc-sync tests passed in this verifier's own run.

### Probe Execution

No `probe-*.sh` declared by the phase plans; conf05 crosscheck (above) is the declared consistency check and passed.

### Requirements Coverage

| Requirement | Source Plans | Description | Status | Evidence |
| ----------- | ------------ | ----------- | ------ | -------- |
| CADW-01 | 73-04, 73-05, 73-06 | own queue, Dockerfile, compose, chart, ScaledObject; atomic `AllEngines()` | SATISFIED | truths 2, 15; `49729fd` |
| CADW-02 | 73-02, 73-03, 73-04, 73-06 | per-stage measured Landlock, SANDBOX-PATHS, no start without Landlock, content-judged preflight | SATISFIED | truths 5-8 (convert-stage scope; render stage is phase 74, DXF phase 75, per ROADMAP) |
| CADW-03 | 73-02, 73-04, 73-06 | transient/terminal by stage; kill leaves no process | SATISFIED | truths 11, 12 (mesh stage; render stage is phase 74) |
| CADIN-04 | 73-01, 73-02, 73-05, 73-06 | bomb inputs terminal within measured bounds | SATISFIED | truths 9, 10 (STEP/STL shapes; DXF bombs are phase 75) |
| CCONV-01 | 73-01, 73-02, 73-05, 73-06 | step/iges -> one binary STL in mm | SATISFIED | truths 1, 13 |

No orphaned requirements: ROADMAP and REQUIREMENTS traceability map exactly CADIN-04, CCONV-01, CADW-01..03 to phase 73 and all five appear in plan frontmatter. REQUIREMENTS.md checkboxes still read `Pending`; that is tracking for the orchestrator, not a code gap.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| `scripts/cad-spike.sh`, `scripts/cad-liveproof.sh` | file headers (24 and 11 lines) | usage / purpose header blocks in new shell scripts | Warning | Not Go or helper code; they are usage text matching the existing script-header convention, but they exceed the "no comments by default" rule if read strictly. No behavior impact. |
| `.env.example` | CAD section | multi-line derivation comment | Info | Configuration documentation file, not code; it is where the derivation arithmetic is meant to be readable. |

No debt markers, no stubs, no empty implementations in the phase's code.

### Notes for later phases (not gaps against Phase 73)

- IGES inch scaling is recorded in the spike evidence as `GAP,key=iges_inch_scaling,value=unproven` (no inch IGES in the corpus). Phase 73's inch criterion names STEP only and is verified; phase 74 (MEAS unit provenance) and phase 76 should not assume IGES inch handling is proven.
- With the default chart `MAX_UPLOAD_BYTES` and `INSPECT_MAX_UPLOAD_BYTES` at 100 MiB, the effective IGES ceiling is 100 MiB, not 135 MiB; this is documented in `ERROR-CODES.md` and `.env.example`.
- The Landlock proof is on the native arm64 kernel (OrbStack 7.0.14); a distinct production kernel is outside the plan, as 73-06 states.
- The CADW-04 timeout is derived, not measured, and every user-visible surface says so; phase 76 owns the measurement.

### Human Verification Required

None. Every behavior-dependent truth (cancellation, kill, ordering of preflight before Postgres, retry vs terminal) has either a passing named test or committed live evidence that is consistent with the final tree.

### Gaps Summary

No gaps. The phase goal is achieved in the codebase: `cad` is exposed in one atomic change, runs only under a measured and preflight-proven Landlock ruleset, converts STEP/IGES to mm binary STL, terminates the bomb shapes as coded terminal failures without OOM or worker restart, classifies failures by stage with documented codes, and leaves no engine process after a kill.

---

_Verified: 2026-10-05T14:10:00Z_
_Verifier: Claude (gsd-verifier)_
