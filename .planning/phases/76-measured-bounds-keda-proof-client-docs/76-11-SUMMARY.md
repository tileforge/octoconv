---
phase: 76-measured-bounds-keda-proof-client-docs
plan: 11
subsystem: final-live-gate
tags: [ci-local, e2e, final-gate, tectonic, sandbox, cad, frontend, d-10, d-18, d-19, ar-2, ar-3, ar-20, ar-21]
status: complete

requires:
  - phase: 76-08
    provides: scripts/cad-liveproof.sh rows and evidence/76-08-liveproof.txt (ALL PASS)
  - phase: 76-10
    provides: k8s restored to the pre-phase state (no octoconv or keda pod)
provides:
  - scripts/ci-local.sh e2e knobs CI_LOCAL_E2E_API_PORT, CI_LOCAL_E2E_EXTRA_COMPOSE_FILES, CI_LOCAL_E2E_TEARDOWN, CI_LOCAL_E2E_GO_TEST_FLAGS (defaults reproduce main)
  - scripts/final-live-gate.sh (preflight, ci-local, tectonic, sandbox, cad, frontend; one transcript ending SUMMARY)
  - evidence/76-11-final-gate.txt (attempt 4, SUMMARY ALL PASS at 07d1024), 76-11-liveproof.txt, 76-11-sandbox-liveproof.txt, 76-11-frontend.txt, ci-local-20261016T230204Z.log
  - evidence/76-11-attempts/1..3 and 4-not-started, evidence/76-11-smoke/
affects: [phase-76-verification, milestone-v1.16]

actuals:
  tokens: 14598
  tasks: 2
  commits: 9
plan_head_before: 471aee9dbcabc191bd580bc08bf8583c1290161a
plan_head_after: 20977da

tech-stack:
  added: []
  patterns:
    - "A gate wrapper's verdict is computed from its own lines: ALL PASS only with every step PASS, zero NOT_RUN lines, tracked dirt 0 at start and end, and equal volume lists"
    - "Every docker, kubectl and step command runs under a perl timeout that kills the whole process group; long steps run in the background so a TERM from the disk watchdog is handled at once"
    - "A sandbox escape hatch is judged by its value (exactly true), the same rule internal/sandbox ResolveHelper applies, never by the variable's presence"
    - "The build context includes .planning/, so a gate clone at a commit that differs only in .planning still misses every COPY . . layer"

key-files:
  created:
    - scripts/final-live-gate.sh
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-11-final-gate-test.sh
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-11-ci-local-knobs-test.sh
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-11-final-gate.txt
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-11-liveproof.txt
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-11-sandbox-liveproof.txt
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-11-frontend.txt
  modified:
    - scripts/ci-local.sh

key-decisions:
  - "The gate commit is 07d1024; attempt 4 from a fresh clone at it ends SUMMARY,result=ALL PASS with not_run=0 and tracked_dirt=0, and every later commit touches only .planning/"
  - "Attempts 1 (INCOMPLETE, stopped before the disk floor), 2 (FAIL, apt mirror outage) and 3 (FAIL, wrapper false negative on the tectonic escape hatch) stay on record unedited"
  - "The start threshold for a gate run is the measured full-eviction peak of 19 GiB plus the 6 GiB floor (approved), replacing the 23 GiB estimate"
  - "The tectonic escape hatch is judged by the value true, matching internal/sandbox ResolveHelper; the empty value compose forwards is confined"
  - "CADW-05 is complete: the all-class live gate, cad included, passes on images built from the final commit; scale-from-zero and minReplicaCount were proven in 76-09"

requirements-completed: [CADW-05]

duration: 2026-10-16T16:43Z to 2026-10-17T00:45Z, wall clock including four disk checks, quiet-window waits, two user-approved cleanups and four gate attempts
completed: 2026-10-17
---

# Phase 76 Plan 11: the all-classes final gate passes on images from the final commit Summary

**From a fresh clone at 07d1024, `scripts/final-live-gate.sh` ran preflight, ci-local (helm, gate, race, docker-build, e2e on 127.0.0.1:18090 with stop teardown), the tectonic proof, sandbox-liveproof over nine classes, the 76-08 cad-liveproof and the frontend's npm ci, test and build. One transcript ends `SUMMARY,result=ALL PASS,commit=07d1024…,steps=6,pass=6,fail=0,not_run=0`, with tracked_dirt 0 and the volume list unchanged. It took four attempts; the first three are kept as recorded.**

## Task 1 (tracer): the ci-local e2e knobs and the gate wrapper

### TDD

- RED, b94e785. Two stub-driven tests:
  - `evidence/76-11-ci-local-knobs-test.sh` runs `scripts/ci-local.sh e2e` with stubbed docker, lsof, curl and go. It failed 22 of 36; every default-path assertion passed on main's script.
  - `evidence/76-11-final-gate-test.sh` runs the wrapper inside a throwaway git repo with stubbed docker, kubectl, lsof, npm, go and sysctl and stub step scripts. It failed 121 of 139 (no wrapper yet).
- GREEN, a3c8a5f: 36/36 and 139/139.
- Mutations:
  - teardown forced to `down -v`: 4 assertions fail;
  - sandbox `ALL PASS (n checks, m not run)` accepted as a pass: 1 assertion fails, and the SUMMARY still stays INCOMPLETE.

### scripts/ci-local.sh

The changes sit in the `COMPOSE_E2E` block and `tier_e2e` only. No comment line was added, and shellcheck reports only the SC2164 warning main already has.

```
@@ -156,0 +157,6 @@ COMPOSE_E2E=(docker compose -f docker-compose.yml -f docker-compose.e2e.yml)
@@ -160,0 +167,18 @@ tier_e2e() {
@@ -168 +192 @@ tier_e2e() {
@@ -183 +207,5 @@ tier_e2e() {
@@ -188 +216 @@ tier_e2e() {
@@ -206 +234 @@ tier_e2e() {
@@ -211 +239 @@ tier_e2e() {
```

Every hunk starts at or after the `COMPOSE_E2E=` line (156) and ends before the `for tier in` loop (248 after the change).

- `CI_LOCAL_E2E_API_PORT` (default 8090) drives the listener check, /healthz and E2E_BASE_URL. A value that is not a number fails before `up`.
- `CI_LOCAL_E2E_EXTRA_COMPOSE_FILES` (default empty) adds `-f` files to up, logs and teardown.
- `CI_LOCAL_E2E_TEARDOWN` is `down-v` (the default, the same command main runs) or `stop`. Any other value fails before `up`.
- `CI_LOCAL_E2E_GO_TEST_FLAGS` (default empty) is appended to the e2e `go test`.
- The tier prints one `e2e settings:` line.

### Default-path probe (evidence/76-11-default-probe.txt)

- 8090 is held by another project (OrbStack pid 99558, 127.0.0.1:8090).
- `scripts/ci-local.sh e2e` with no knob exited 1 with `something is already listening on :8090`.
- `docker compose -p octoconv ps -q` was empty before and after.
- The volume sha256 was the same before and after.

### scripts/final-live-gate.sh

- **Steps:** preflight, ci-local, tectonic, sandbox, cad, frontend.
- **Exit codes:** 0 ALL PASS, 1 FAIL, 2 INCOMPLETE, 3 VM disk below 6 GiB, 64 usage or an existing output file. It never overwrites an earlier attempt's files.
- **Lines written:**
  - HDR (date_utc, commit, tracked_dirt, branch, clone, go, server_arch, load1, vm_free_gib, plus host_free_gib, python3 and steps);
  - VOLUMES before, after and equal;
  - STEP, CONVERT_TIME, TECTONIC, IMAGES and IMAGES_CHECK;
  - CLEANUP, QUIET, NOT_RUN and STOP;
  - DIRT at the end, DISK, VERDICT and SUMMARY.
- **Safety:** a 60 s VM-disk watchdog (`docker run --rm alpine df`) stops the run below 6 GiB. Three samples in a row without an answer count as docker unresponsive.
- **Hygiene:** compose is only ever stopped. The EXIT trap removes only the two override files this run wrote. The script has no comment lines and none of the words prune, rmi, `volume rm` or `down -v`, and shellcheck is clean.

### Tracer smokes (approved deviations a and the two-stage start)

Both ran from a git-bundle clone C1 at a3c8a5f (evidence/76-11-smoke/):

- **Knobbed e2e smoke** (17:42:06Z to 18:05:25Z, load1 6.32 at the start):
  - ALL REQUESTED TIERS PASSED;
  - `--- PASS: TestMultiFileLaTeXProjectE2E (6.08s)` and `--- PASS: TestMultiFileRefusalsE2E (0.02s)`, no `--- FAIL`;
  - `cmp` of the sorted volume lists: identical (49);
  - nothing left running.
- **Wrapper smoke** `--steps preflight,frontend`: exit 2, `SUMMARY,result=INCOMPLETE,...,pass=2,fail=0,not_run=4`, frontend PASS (vitest `Tests 42 passed (42)`).
- **Tracer gate:** the Task 1 verify printed TASK1_VERIFY_PASS, so the plan expanded.
- **Tree check:** `ci-local.sh helm gate race` in C1 changed nothing in the tree, ignored files included (0 status lines, 0 newer files). internal/convert took 408.069 s.

## Task 2: the final gate

### Attempts

| # | Commit | Result | Cause | Evidence |
|---|---|---|---|---|
| 1 | a3c8a5f | INCOMPLETE | The build cache had lost six of twelve Dockerfiles since the smoke. Projected below the 6 GiB floor, so I stopped it with TERM (see deviation 7) | evidence/76-11-attempts/1/ |
| 2 | a3c8a5f | FAIL | The docker-build tier: apt exit 100, deb.debian.org timeouts (75.9 MB in 4 min 9 s). helm, gate, race and e2e passed | evidence/76-11-attempts/2/ |
| 3 | a3c8a5f | FAIL | The tectonic step: the wrapper counted the empty `TECTONIC_SANDBOX_DISABLED=` that compose forwards as disabled. ci-local passed in full | evidence/76-11-attempts/3/ |
| 4 | 07d1024 | not started, then **ALL PASS** | First 4 start checks in 30 min: VM free 25.65-26.17M KiB, need 26214400. After the user-approved image removal: ALL PASS | evidence/76-11-attempts/4-not-started/, evidence/76-11-final-gate.txt |

Every attempt ran from a fresh clone, with tracked_dirt 0, the bundle copied in, python 3.14 first on PATH, GOTOOLCHAIN=go1.26.4, caffeinate, a quiet window (load1 ≤ 8 for 60 s) and the watchdog. The transcripts are kept unedited.

### Attempt 4 (official, evidence/76-11-final-gate.txt)

- **HDR:**
  - commit 07d1024aab7f7b342d6b2f8bc394b1f0d82a2f9d, tracked_dirt 0;
  - branch gsd/phase-76-measured-bounds-keda-proof-client-docs;
  - clone /Users/apaderin/.cache/octoconv/clones/07d1024/octoconv;
  - go1.26.4, arm64, python 3.14.8;
  - load1 7.44, VM free 29.98 GiB, host 35.17 GiB.
- **Timing:** 23:01:59Z to 00:40:04Z.

STEP lines:

```
STEP,name=preflight,rc=0,result=PASS,started_utc=2026-10-16T23:02:01Z,ended_utc=2026-10-16T23:02:04Z,transcript=76-11-final-gate.txt
STEP,name=ci-local,rc=0,result=PASS,started_utc=2026-10-16T23:02:04Z,ended_utc=2026-10-16T23:33:35Z,transcript=ci-local-20261016T230204Z.log
STEP,name=tectonic,rc=0,result=PASS,started_utc=2026-10-16T23:33:35Z,ended_utc=2026-10-16T23:33:35Z,transcript=ci-local-20261016T230204Z.log;76-11-tectonic-worker.log
STEP,name=sandbox,rc=0,result=PASS,started_utc=2026-10-16T23:33:35Z,ended_utc=2026-10-16T23:42:45Z,transcript=76-11-sandbox-liveproof.txt
STEP,name=cad,rc=0,result=PASS,started_utc=2026-10-16T23:42:45Z,ended_utc=2026-10-17T00:39:52Z,transcript=76-11-liveproof.txt
STEP,name=frontend,rc=0,result=PASS,started_utc=2026-10-17T00:39:52Z,ended_utc=2026-10-17T00:40:03Z,transcript=76-11-frontend.txt
```

**ci-local**
- Tiers: helm, gate, race, docker-build and e2e all PASS, and ALL REQUESTED TIERS PASSED. `ok internal/e2e 119.240s`.
- D-19: `CONVERT_TIME,seconds=369.758,load1=8.12,load1_start=6.96,load1_end=8.12,budget_s=540,result=ok`.
- `IMAGES_CHECK,stack=e2e,built_services=13,expected=13,tag_mismatch=0,ids_present_before_run=12,result=ok`. The 12 ids already present were built by attempt 3 from the same Dockerfiles. Their inputs are unchanged by 07d1024's script-only diff, so `--build` resolved them to the same ids.
- IMAGES (stack e2e):

  | Service | Id (sha256) |
  |---|---|
  | api | 37d6ee6eb12d |
  | audio-worker | ad97c167650f |
  | av-worker | dc450cbd571e |
  | cad-worker | f50534c1fcc8 |
  | chromium-worker | 63fdd4c16077 |
  | document-worker | 91fecdba3d3f |
  | ocr-worker | 277ae731ee29 |
  | pandoc-worker | 9d4e2dff1e61 |
  | probe-worker | f076e58eafac |
  | tectonic-worker | 3bb21e6825ac |
  | webhook-worker-1 | 09f9e3b3de22 |
  | webhook-worker-2 | a0136c9c4d1b |
  | worker | 61c121265320 |

  Full ids are in the transcript.

**tectonic** (AR-3)

```
TECTONIC_DETAIL,pass_latex=1,pass_refusals=4,failed_test_lines=0,landlock_lines=1,disabled_log_lines=0,disabled_env=0,disabled_env_value=empty,since=2026-10-16T23:02:04Z
TECTONIC,e2e_pass=TestMultiFileLaTeXProjectE2E;TestMultiFileRefusalsE2E,landlock=1,sandbox_disabled=0,image_id=sha256:3bb21e6825ac84d284f0a0d78f338d553c781dc1fc4966c16f384aa3f2449473,result=ok
```

The worker log (evidence/76-11-tectonic-worker.log) opens with `🔒 tectonic sandbox: landlock via /usr/local/bin/tectonic-worker __sandbox-exec`.

**sandbox**
- Last line: `ALL PASS (45 checks)`, with no `NOT RUN:` line, over image, pandoc, ocr, av, audio, html, probe, document and cad. This includes the emulated-amd64 refusals for document and cad.
- `CLEANUP,step=sandbox,api_clients=0,minio_objects_deleted=0,leftover_tags=0`. The script's own tags are gone.

**cad**
- The 76-08 rows: 164 rows, `SUMMARY,result=ALL PASS,...,determinism=6,guard_edge=2,stacked_refusal=3,render_ceiling=0,worker_kill_render=1,worker_kill_rsvg=1`.
- HDR commit 07d1024…, tracked_dirt=0, `build=compose_up_build`.
- `TIMEOUTS,guard=21s,mesh=324s,render=521s,dxf=186s,rsvg=217s,engine=886s,api_engine=886s,...,result=ok`
- `CAD_RUN,run=1,rc=0,all_pass=1,tracked_dirt=0,commit=07d1024…,commit_match=1,result=ok`
- `QUIET,step=cad,run=1,rows=164,contaminated=0,bound=11`, so no rerun was needed.
- `IMAGES,stack=cad,api=sha256:985cf154a4bb…;cad-worker=sha256:f50534c1fcc8…` and `IMAGES_CHECK,stack=cad,...,tag_mismatch=0,result=ok`.
  - The api id differs from the e2e stack's because compose builds it here without docker-compose.e2e.yml. Both came from `--build` in the clone.
  - cad-worker resolves to f50534c1fcc8 in every build since fa12a13, because its inputs have not changed.
- `CLEANUP,step=cad,run=1,...,clients=cad-liveproof-1792194288,other_clients_since=0,jobs=153,objects_deleted=302,bytes_deleted=914565489,invalid_ids=0,bucket_before=4.0GiB_1884_objects,bucket_after=3.1GiB_1582_objects,result=ok`.

**frontend**
- `FRONTEND,npm_ci_rc=0,test_rc=0,build_rc=0,vitest=Tests__42_passed_(42),failed_summary_lines=0,lockfile_changed=0,result=ok`.

**Closing lines**
- `VOLUMES,phase=before|after,count=42,sha256=899d0d04e6c8e8bb9628ddb53b097f5ef826328c4d925873f425803d16a6c992`, then `VOLUMES,equal=1`.
- `DIRT,phase=end,tracked_dirt=0`.
- `DISK,vm_free_min_kib=23620016,samples=90`.
- `SUMMARY,result=ALL PASS,commit=07d1024aab7f7b342d6b2f8bc394b1f0d82a2f9d,steps=6,pass=6,fail=0,not_run=0`.

**Task 2 verify:** it printed TASK2_VERIFY_PASS.

**After ALL PASS**
- `docker compose -p octoconv stop`, and nothing is running.
- The clone is removed.
- 0 `multipart-*` files were newer than the start marker, and the marker was removed.
- VM free is 27904164 KiB.
- **No non-.planning file changed after the gate commit:** `git diff --name-only 07d1024..HEAD -- . ':!.planning'` is empty.

## Deviations from Plan

1. **(a) Smokes from a git-bundle clone, approved.**
   - The e2e smoke and the wrapper smoke ran from clone C1, not the main tree, so the gate does not build a second set of Go layers.
   - The e2e ports file was written at the clone root with the wrapper's exact content, not in $TMPDIR, so C1's build context equals the gate clone's.
2. **(b) Scoped MinIO cleanup, approved.**
   - After each cad-liveproof run, the wrapper deletes `uploads/<id>/` and `results/<id>/` of jobs whose clients are named `cad-liveproof-*` and were created since that run started. Each id must match the UUID regex.
   - Every run writes a CLEANUP line.
   - sandbox-liveproof makes no clients, and its CLEANUP line records 0 objects deleted.
3. **Two-stage start, approved.** The smokes ran first. The gate's increment was then estimated from measured numbers:
   - the smoke's net cost was 9.73 GiB, with a transient 4.4 GiB above that;
   - images grew by 3.24 GB and the cache by 5.91 GB.
4. **Tree-write check.** `ci-local.sh helm gate race` ran in C1 to prove these tiers do not change the build context. They changed nothing.
5. **Clones at the gate commit, not at HEAD.** The build context includes `.planning/`, so the runner does `git checkout -B <branch> <gate commit>` in each fresh clone.
6. **[Rule 2] The wrapper is stricter than the plan:**
   - `CAD_LIVEPROOF_QUIET_LOAD=11` plus one automatic rerun for quiet_host-contaminated rows, with coverage by row key and occurrence;
   - D-19 over budget at load1 ≥ 8 records `result=contaminated` plus a NOT_RUN, never a pass or a STOP;
   - preflight also checks ports 9101, 6379 and 8980, which compose publishes;
   - tracked dirt is measured at the end too;
   - the first FAIL stops the later steps (`NOT_RUN,reason=after_fail`);
   - IMAGES_CHECK records ids that already existed instead of requiring new ones, because a reproducible build legitimately returns the same id.
7. **Attempt 1 was stopped by me with TERM, not by the watchdog.** The projection, from measured numbers, was about 2 GiB at the end, below the 6 GiB floor. The wrapper recorded `STOP,reason=terminated` and INCOMPLETE.
8. **Start threshold lowered from 23 to 19 GiB, approved.** The basis is the measured full-eviction peak: fleet net 9.73 + amd64 5.9 + MinIO 1.7 + host and e2e 1.6 GiB. The 6 GiB floor and the 60 s watchdog are unchanged. The 23 GiB check had refused at 30304300 KiB free (need 30408704).
9. **[Rule 1] The tectonic escape-hatch check is judged by value** (RED 54641ff, GREEN 07d1024).
   - Attempt 3 failed because the wrapper treated the empty `TECTONIC_SANDBOX_DISABLED=` from docker-compose.yml as disabled. `internal/sandbox.ResolveHelper` honours only `true`.
   - The check now counts `TECTONIC_SANDBOX_DISABLED=true` exactly and records the value.
   - The wrapper suite is now 144/144. 07d1024 became the gate commit.
10. **Space freed by the orchestrator, not by this executor, each time with the user's approval:**
    - `docker builder prune -f` (4.4 GB) before the smokes;
    - the npm and uv caches, 7 unused volumes of other projects (each checked for 0 containers) and the stopped octoconv-cad-worker container with 0.85 GB of leftover job dirs, before attempt 2;
    - 38 dangling octoconv images, removed by id with `docker rmi` and no `-f`, before attempt 4. They were the predecessors of tags re-pointed by this plan's rebuilds; the 3 foreign dangling images were kept.

    These changed the volume list between attempts, so each attempt takes its own before-baseline. Within attempt 4 the volume list is equal before and after.

## NOT RUN

- The RENDER_CEILING fired branch of cad-liveproof: every PATHB line is fired=0, as in 76-08. It is recorded as `render_ceiling=0` and counts as no pass.

The official run has no other NOT RUN.

## Known Stubs

None.

## Threat Flags

None new.

- **T-76-28:** HDR commit and tracked_dirt come from the clone; the cad HDR names the same commit; IMAGES and IMAGES_CHECK cover both stacks; there is no non-.planning change after 07d1024.
- **T-76-29:** stop teardown only, with equal VOLUMES in every attempt.
- **T-76-30:** NOT_RUN gives INCOMPLETE, as attempt 1 shows.
- **T-76-31:** the 60 s watchdog and the measured start threshold. Nothing was pruned by the executor.
- **T-76-SC:** npm ci left the lockfile unchanged.

## Self-Check: PASSED

- `scripts/final-live-gate.sh`, `scripts/ci-local.sh` and every evidence file named above exist.
- Commits b94e785, a3c8a5f, 40512a9, 8009913, 00c26b3, 54641ff, 07d1024, 14e4410 and 20977da are ancestors of HEAD. All are signed G, and none has a Co-Authored-By trailer.
- `GOTOOLCHAIN=go1.26.4 go vet ./...` exits 0, and `gofmt -l .` is empty.
- `git diff --exit-code main -- internal/convert/exec.go` exits 0. So does the frozen set: keda-ocr, keda-pandoc and keda-av downscale-survival, keda-load-proof, keda-ocr-loadproof, keda-pandoc-loadproof, cad-image-size-measure and sandbox-liveproof.

## Follow-up

2026-10-17, quick 261017-7y5: on Codex review of PR #37, after the gate, the wrapper was hardened so that a failed cleanup or a masked command failure fails its step; the gate verdict is unaffected, because attempt 4's cad cleanup recorded `result=ok` (evidence/76-11-final-gate.txt line 44); the attempt-4 status of the other hardened checks is in `.planning/quick/261017-7y5-codex-p1-final-live-gate-cleanup-result-/261017-7y5-SUMMARY.md`.

2026-10-17, quick 261017-f0i: on Codex round 3 of PR #37, the cad cleanup now finds job objects from one recursive bucket listing (`mc ls --recursive --json local/octoconv`), checked before and after deletion, and fails on a failed or unread listing, on a job without an `uploads/` key, and on any object left under a run prefix; attempt 4's CLEANUP line (evidence/76-11-final-gate.txt line 44) was computed with the per-prefix `mc du --json` listing unchecked, and although its objects_deleted=302 matches the bucket drop from 1884 to 1582 objects, that match cannot show a skipped prefix, so the absence of leftovers at attempt 4 is not re-proven.
