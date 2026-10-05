---
phase: 73-cad-class-convert-slice-confined
plan: 06
subsystem: cad-liveproof
tags: [cad, liveproof, landlock, bombs, cancel, worker-kill, compose]
requires:
  - phase: 73-05
    provides: the cad class exposed everywhere at once (49729fd), the cad-worker compose service, the per-format ceilings
provides:
  - scripts/sandbox-liveproof.sh check_emulated_amd64_refuses CLASS, called for document and cad
  - scripts/cad-liveproof.sh, the compose live proof of the convert slice, rejections, bombs, cancel and worker kill
  - evidence/73-06-sandbox-liveproof.txt and evidence/73-06-liveproof.txt, both naming the commit and a tracked-dirt count of 0
affects: [74, 76]
actuals:
  tokens: 9200
  tasks: 2
  commits: 12
plan_head_before: c707553ec02a42304aa34e306d059d43a885c31a
plan_head_after: 10f0fd9
tech-stack:
  added: []
  patterns:
    - a live check judged by content (cad-stl-judge) and by the container's own OOMKilled, RestartCount, StartedAt and memory.events counters
    - process census by start time against the container's PID 1 instead of a bare process-name scan
key-files:
  created:
    - scripts/cad-liveproof.sh
    - .planning/phases/73-cad-class-convert-slice-confined/evidence/73-06-sandbox-liveproof.txt
    - .planning/phases/73-cad-class-convert-slice-confined/evidence/73-06-liveproof.txt
  modified:
    - scripts/sandbox-liveproof.sh
key-decisions:
  - "No product code changed; every live check passed, so no gap plan is needed"
  - "The 'orphan' definition for the worker kill is a cad-mesh process older than the restarted container's PID 1, because the worker's own startup preflight runs cad-mesh and a bare name scan sees it"
  - "The 'tracked dirt' figure of sandbox-liveproof.sh now excludes untracked files (git status --short --untracked-files=no), matching its label"
requirements-completed: [CADW-01, CADW-02, CADW-03, CADIN-04, CCONV-01]
duration: 75min
completed: 2026-10-05
status: complete
---

# Phase 73 Plan 06: Live proofs on the final tree Summary

**The cad class is proven on a running system built from the final tree: the real image confines both engines and refuses to start without Landlock (natively and under amd64 emulation), step, stp, iges and igs jobs return exactly one millimetre STL judged by content, twelve bomb inputs (the eleven of the first run plus a partially meshed face) end as terminal coded failures with the worker never OOMKilled or restarted, a cancel kills `cad-mesh` in 0.2 s, and a SIGKILLed worker lets the job finish through the asynq lease in 112.1 s.**

## Accomplishments

- **Task 1: the final image proven confined.** `check_document_emulated_amd64_refuses` became `check_emulated_amd64_refuses CLASS DOCKERFILE ENVVAR`, called for document and cad (document's check name and messages are unchanged). `scripts/sandbox-liveproof.sh cad` on commit `922025f` (tracked dirt 0): `cad confined-startup`, `cad refuses-without-landlock`, `cad escape-hatch`, `cad preflight-catches-restrict-self`, `cad emulated-amd64-refuses`, `ALL PASS (5 checks)`, no NOT RUN. Re-run after the Codex P1 fix on `8023836` (image `sha256:d0c691d4...`): the same five checks, `ALL PASS (5 checks)` (see Task 3). The emulated amd64 run printed `cad sandbox: landlock is unavailable ... Set CAD_SANDBOX_DISABLED=true`, no lock line, no Postgres attempt. `scripts/sandbox-liveproof.sh document` after the refactor: `ALL PASS (7 checks)` (same seven names as before).
- **Task 2: the live compose proof.** `scripts/cad-liveproof.sh` was first run on commit `ce195dd` (cad-worker image `sha256:8eb376b1...`, bomb=11). After the Codex P1 fix (`cad-mesh` refuses partial meshing, `e3cab94`) it was re-run on commit `87b7ff0` (tracked dirt 0, cad-worker image `sha256:84b6710a...`, `CAD_ENGINE_TIMEOUT` 406 s): `SUMMARY,result=ALL PASS,formats=1,inspect=1,convert=5,reject=2,bomb=12,cancel=1,worker_kill=1`. No code under `cmd/`, `internal/` or any Dockerfile changed between `e3cab94` and `87b7ff0`, so the evidence file is for the final code. The first run's evidence is superseded.
- **Task 3: the partial-mesh fix, proven.** `evidence/73-06-partial-mesh.txt` (`scripts/cad-partial-mesh-proof.sh`, commit `29c15ad`, real OCP 7.9.3.1.1): the pre-fix helper writes 10 of 12 triangles as `ok` for `partial_face.step`; the new helper exits 2 with `incomplete/mesh_status` and no STL; the intact fixture gives 12 triangles in both. The mesh Landlock set was re-checked on the fixed engine stage (`LANDLOCK_RECHECK-OK,cases=4`), and `sandbox-liveproof.sh cad` was re-run on `8023836` (`ALL PASS (5 checks)`).

## Evidence lines mapped to success criteria and requirements

| Evidence line | What it shows | ROADMAP | Requirement |
|---------------|---------------|---------|-------------|
| `FORMATS` | exactly `step>stl` and `iges>stl` on engine cad, no other pair touching step, stp, iges, igs, stl, dxf or dwg, in `engines` and in `operations.convert` | SC1 | CADW-01 |
| `INSPECT` | units_inch.step offers `step>stl` on cad, `estimate_source=unmeasured`, `max_bytes=66060288` | SC1 | CADW-01 |
| `CONVERT` x5 | `.step`, `.stp`, `.igs`, `.iges` jobs done in 2.3 to 9.4 s; each has exactly one `job_outputs` row, role primary, format stl; downloaded size equals `size_bytes`; judge ok. units_inch.step: 12 triangles, extents 25.400000:50.799999:76.199997. NIST ctc_05 (9450 triangles) and d2mi (6870) inside their vertex-box band with factor 25.4. IGES valve: 65392 triangles, valid. | SC1 | CCONV-01 |
| `REJECT` x2 | lying-count binary STL: 422 `truncated_cad_file`; STEP of 63 MiB + 1 byte: 413 `payload_too_large`, "step file exceeds the 63 MiB limit for this conversion" | SC3 | CADIN-04 |
| `BOMB` x12 | all `status=failed`, `oom_killed=false`, `restart_count=0`, StartedAt unchanged, `oom_kill=0` after every one. Entity flood (754763), NAUO flood (1519), NAUO doubling hierarchy K12 and mapped-item doubling K12: `cad_too_complex` in 2.6, 2.2, 2.3 and 0.2 s (bound 30 s). Self-reference, cycle, non-numeric coordinates `abc`, `--1`, `1E300`, `1E999`, `NaN`, and `partial_face.step` (a face the mesher cannot triangulate): `cad_unreadable` in 2.2 to 2.3 s (bound 406 s) | SC3 | CADIN-04 |
| `CANCEL` | job active, `cad-mesh` visible (pid 314), cancel 200, pid gone 0.2 s later (bound 5 s), job `canceled`, no `download_url` | SC4 | CADW-03 |
| `WORKER_KILL` | `docker kill --signal KILL` mid-mesh (pid 356); container back 41.6 s later; 0 `cad-mesh` processes older than the restarted container's init; job events `->queued;queued>active;active>active;active>done`; kill-to-done 112.1 s against 406 s; webhook-worker (the reconciler host) not running; STL judged ok (3820000 triangles) | SC4 | CADW-03 |
| sandbox transcript | the five cad checks above | SC2 | CADW-02 |

The expected codes for the structural bombs came from `evidence/73-01-spikes.txt` (RULE `load_check=numeric_strict`, `assembly_cycle=reader_handles`, `expansion=guard_bounds_expanded_instances`) and the cad-guard and cad-mesh code of plan 73-04: shape-null self-reference and cycle end `cad_unreadable` (`no_shape`), numeric load failures end `cad_unreadable` (`load_check_fail`) before `TransferRoots`, so the NaN input that ran 400 s in the 73-01 reader spike now ends in 2.3 s.

## Task Commits

1. **Task 1a: shared emulated-amd64 check** - `922025f` (feat)
2. **Task 1b: sandbox evidence** - `17cf183` (test)
3. **Task 2a: cad-liveproof.sh** - `cfa7663` (feat)
4. **Deviation fix: orphan census** - `ce195dd` (fix)
5. **Task 2b: live evidence** - `f5fc5e9` (test), superseded by `10f0fd9`
6. **Codex P1 fix: refuse partial meshing** - `e3cab94` (fix, plan 73-02 code)
7. **Partial-mesh fixture in the live proof** - `8023836` (test)
8. **Sandbox live proof re-run on the fix** - `29c15ad` (test)
9. **Partial-mesh proof on the real OCP reader** - `1af40cb` (test)
10. **Live proof leaves its own evidence file out of the dirt count** - `5c0f080` (test)
11. **Startup log read without a SIGPIPE race** - `87b7ff0` (test)
12. **Cad live proof re-run on the fix** - `10f0fd9` (test)

All SSH-signed (`G`), no trailer.

## Verification

- `GOTOOLCHAIN=go1.26.4 go test ./... -count=1`: every package ok (internal/convert 117 s).
- `GOTOOLCHAIN=go1.26.4 scripts/ci-local.sh helm gate race`: `ALL REQUESTED TIERS PASSED` at commit `f5fc5e9`. Transcript: `/Users/apaderin/dev/octoconv/.planning/ci-local/ci-local-20261005T104615Z.log` (directory gitignored).
- `git diff --exit-code e219f8c -- internal/convert/exec.go go.mod go.sum` prints nothing.
- The Task 2 awk gate over the evidence (CONVERT 5, BOMB 11, CANCEL 1, WORKER_KILL 1, REJECT 2, none bad) and the Task 1 grep gate both pass.
- Stack left at rest: `docker compose stop` for exactly the five services the run started (postgres, redis, minio, api, cad-worker); the one-shot createbucket had exited by itself. `docker ps` shows no octoconv container running, nothing removed, no `down -v`, no other project touched, `kubectl get pods -A | grep -c octoconv` was 0. Free disk after the run: 11 GiB (floor 6). The compose-built images `octoconv-cad-worker:latest` and `octoconv-api:latest` were kept (they are the images the evidence names); the unique sandbox-liveproof tags were removed by the script itself.

## Deviations from Plan

**1. [Rule 1 - Operand] The first live run failed on a false orphan.** The WORKER_KILL check scanned the restarted container for any process named `cad-mesh` and found pid 20 at 42.1 s. That was the worker's own startup preflight (it runs the real `cad-mesh` inside the sandbox before Postgres, 10:38:10 to 10:38:11 in the worker log), not a leftover: the kill leaves the PID namespace torn down with PID 1. The scan was replaced by a census comparing each `cad-mesh` start time (`/proc/PID/stat` field 22) with PID 1's, and the line records `orphans=0` (older than init) next to `fresh_mesh_at_restart=1` (the preflight). Everything before the failure had passed and was re-proven by the second full run; only the second run is the evidence file. Fix commit `ce195dd`.

**2. [Rule 3 - Blocking] `docker kill` disables the restart policy.** `restart: always` did not bring the worker back within 30 s after `docker kill --signal KILL` (Docker treats a manual kill as a stop), so the script followed the plan's fallback, `docker compose up -d cad-worker` (`restart=compose_up`, back after 42.1 s). Kill-to-done therefore includes about 42 s of operator-side restart; it stays far below 406 s. A real node fault (OOM, node loss) would go through the restart policy or the scheduler; this is not a product defect.

**3. [Rule 1 - Operand] "Tracked dirt" counted untracked files.** `sandbox-liveproof.sh` printed `git status --short | wc -l`, which includes the untracked `.vscode/` and `octoconv.code-workspace`, so it could never read 0. It now uses `--untracked-files=no`, matching its label (part of `922025f`).

**4. Invariance base.** Every `git diff --exit-code` that the plan writes against `520ecd4` ran against `e219f8c`, as the dispatch said.

**5. Fifth CONVERT line.** The IGES valve is uploaded under both `.igs` and `.iges` names so all four aliases (`.step`, `.stp`, `.igs`, `.iges`) are covered; the plan's list had four files.

**6. The `1..2` coordinate variant is not a bomb.** The 73-01 S1 rows show the reader accepts `1..2` with no load failure and converts it to the same 12 triangles, so it is not submitted as a failing input; the other five `gen-bombs` coordinate variants all fail as required.

**Total deviations:** 6 (one harness defect found in my own script, one environment fact, rest scope notes). No product code changed.

## Issues Encountered

- The `mesh_census` handling of processes older than init could not be exercised against a real pre-restart process (a container cannot contain one); it was checked only for the zero-leftover and one-fresh cases (`0 1` with a renamed `sleep`).
- The IP rate limit (60 per minute) bounds polling to one request per 2 s; durations in the evidence therefore carry about 2 s of resolution except the process-table timings (cancel, restart), which are measured directly.

## Known Stubs

None.

## Known Gaps

- The live proof exercised the STEP limit 63 MiB + 1 at the API only; the 135 MiB IGES ceiling at creation is covered by `internal/api/cad_ceiling_test.go` (73-05), not on the live stack.
- The 406 s engine timeout and the resource sizing are derived, not measured in production; scale-from-zero and chart sizing remain phase 76.
- The Landlock proof is the native arm64 server's kernel (7.0.14-orbstack); a distinct production kernel is outside this plan.

## Threat Flags

None beyond the register. T-73-17: only the six services of this repository's compose project were started, only the five this run started were stopped, no `down -v`, no worktree. T-73-18: both evidence files name the commit, tracked dirt 0 and the image ids; the generated bomb inputs are recorded by sha256 in their BOMB, CANCEL and WORKER_KILL lines. T-73-SC: no package or image was pulled; downloads used the python already in the cad-worker image.

## Self-Check: PASSED

- FOUND: scripts/cad-liveproof.sh, evidence/73-06-liveproof.txt (`SUMMARY,result=ALL PASS`), evidence/73-06-sandbox-liveproof.txt (`ALL PASS (5 checks)`)
- FOUND commits 922025f, 17cf183, cfa7663, ce195dd, f5fc5e9, e3cab94, 8023836, 29c15ad, 1af40cb, 5c0f080, 87b7ff0, 10f0fd9 (ancestors of HEAD); evidence/73-06-partial-mesh.txt (`SUMMARY,result=ALL PASS`)
- `git rev-list --count c707553..HEAD` was 5 before this summary's own commit
