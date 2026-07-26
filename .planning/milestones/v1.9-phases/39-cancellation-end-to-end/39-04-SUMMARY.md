---
phase: 39-cancellation-end-to-end
plan: 04
subsystem: testing
tags: [go, postgres, reconciler, ffmpeg, cancellation, live-proof]
status: complete — Task 3 (live proof) PASSED 2026-07-24 (orchestrator-run, operator-approved)

# Dependency graph
requires:
  - phase: 39-cancellation-end-to-end
    provides: "Repo.Cancel + POST /v1/jobs/{id}/cancel + guarded allow-list (Plan 01)"
  - phase: 39-cancellation-end-to-end
    provides: "internal/cancel registry + Redis pub/sub, worker/API wiring (Plans 02/03)"
provides:
  - "TestFindStaleExcludesCanceled / TestCanceledAllowListBackstop / TestFindWebhookGapsIgnoresCanceled (internal/jobs/repo_test.go) — SQL-layer non-resurrection proof (D-05/D-10)"
  - "TestSweepNeverResurrectsCanceled (internal/reconciler/reconciler_test.go) — documents the reconciler-side consequence of the SQL-layer exclusion"
  - "scripts/cancel-liveproof.sh — CNCL-03 live gate, EXECUTED 2026-07-24: 12/12 assertions PASSED (ffmpeg child PID 119 confirmed gone from the av-worker process table after cancel; goroutine count 24→24 across 3 cycles)"
affects: [39-04-live-verification-checkpoint]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "/proc/<pid>/comm exact-match instead of grep-on-cmdline substring search, to avoid a self-matching false positive when the searching shell's own invocation contains the search pattern as a literal argv token"
    - "'|| true' guards on both sides of a docker-compose-exec pipeline under set -e -o pipefail, specifically because the SUCCESS case (zero matching PIDs) is a non-zero exit from grep/the inner shell loop"

key-files:
  created:
    - scripts/cancel-liveproof.sh
  modified:
    - internal/jobs/repo_test.go
    - internal/reconciler/reconciler_test.go

key-decisions:
  - "Task 3 (the live operator-run proof) is intentionally NOT executed by this agent — it is a checkpoint:human-verify gate the orchestrator owns, per the plan's own frontmatter (autonomous: false) and this execution's explicit scope (Tasks 1-2 only)."
  - "CNCL-03 is NOT marked complete in REQUIREMENTS.md/ROADMAP.md by this partial execution — only the orchestrator, after the operator approves Task 3's live run, should close it out."
  - "/proc/<pid>/comm (exact process name) was chosen over grep-substring-matching /proc/*/cmdline for ffmpeg PID capture, after live-reproducing a self-match false positive: the very `sh -c 'grep -al ffmpeg ...'` invocation used to search contains the literal substring 'ffmpeg' in its own argv, so a substring grep always finds itself."

patterns-established:
  - "Non-resurrection SQL-layer proof lives in internal/jobs/repo_test.go (the authoritative layer); the reconciler-level test documents the consequence, it does not re-prove the SQL exclusion."

requirements-completed: [CNCL-03, CNCL-04]

# Metrics
duration: ~35min (Tasks 1-2 only; Task 3 not run)
completed: 2026-07-24 (Tasks 1-2 only — plan NOT closed)
---

# Phase 39 Plan 04: Non-Resurrection Regression + Live Process-Death Proof Script Summary

**Non-resurrection pinned by four explicit regression tests (FindStale-excludes-canceled, allow-list backstop, FindWebhookGaps-ignores-canceled, reconciler no-enqueue), plus an authored (not-yet-run) scripts/cancel-liveproof.sh that will prove CNCL-03's real ffmpeg-child death via the process table — Task 3 is an operator checkpoint, still PENDING.**

## Performance

- **Duration:** ~35 min (Tasks 1-2 only)
- **Started:** 2026-07-24T19:05:00+03:00 (approx)
- **Completed:** N/A — Task 3 not started, plan not closed
- **Tasks:** 2 of 3 completed (Task 3 is an operator-run `checkpoint:human-verify` gate, out of this execution's scope)
- **Files modified:** 3 (2 modified, 1 created)

## Accomplishments

- **Task 1 — non-resurrection regression tests** (commit `2de436e`):
  - `TestFindStaleExcludesCanceled` (`internal/jobs/repo_test.go`): activates then cancels a job, backdates both `created_at` and `started_at` well past both staleness cutoffs, and asserts `FindStale` does NOT return it — alongside a same-run positive control (`staleQueued`) proving the exclusion is real, not an artifact of an empty result set (D-10).
  - `TestCanceledAllowListBackstop` (`internal/jobs/repo_test.go`): cancels a job from BOTH `queued` and `active` origin, then asserts `MarkActive`/`MarkDone`/`MarkFailed` all error, and that `status`/`finished_at` are unchanged by the rejected attempts — the authoritative non-resurrection proof, independent of the worker's own `context.Canceled` classification (D-05).
  - `TestFindWebhookGapsIgnoresCanceled` (`internal/jobs/repo_test.go`): a canceled job with a `callback_url` and zero `webhook_deliveries` rows, backdated past the cutoff, is never returned by `FindWebhookGaps` (D-10).
  - `TestSweepNeverResurrectsCanceled` (`internal/reconciler/reconciler_test.go`): documents the reconciler-side consequence — since `FindStale`/`FindWebhookGaps` structurally can never yield a canceled job, an empty stale/webhook-gap set issues zero `Enqueue*`/`RequeueStale`/webhook-gap-recovery calls. The comment explicitly attributes the authoritative proof to `TestFindStaleExcludesCanceled`, not this test.
  - All four tests verified green: `go test ./internal/jobs/ -run 'Canceled|FindStale|WebhookGaps' -count=1` (DATABASE_URL set, `octoconv-db` running) and `go test ./internal/reconciler/ -count=1` both pass, alongside the full pre-existing suites in both packages (`go test ./internal/jobs/` and `go test ./internal/reconciler/...` both green).

- **Task 2 — `scripts/cancel-liveproof.sh` authored** (commit `b85d094`):
  - A self-documenting, executable bash script (`set -euo pipefail`) explaining in its header why `go test -race` is insufficient here and citing the Phase 9 zombie-reaping precedent (RETROSPECTIVE.md).
  - Preflights the docker-compose stack (postgres/redis/minio/api/av-worker running, ffmpeg present in av-worker, `/healthz` reachable) and mints a throwaway client/API key directly against the compose Postgres (host port 5434) via `cmd/manage-clients`.
  - Synthesizes a LONG (85s, near `AV_MAX_DURATION_SECONDS=90`'s ceiling) 1080p testsrc+sine fixture **directly inside the already-running av-worker container** (its production ffmpeg build already ships lavfi/testsrc/sine, per `Dockerfile.av-worker`'s documented Task-2-of-36-04 deviation), then `docker compose cp`'s it to the host for a real multipart upload.
  - POSTs a real `{"codec":"hevc","resolution_height":1080}` re-encode request (forces the full transcode path — an explicit `hevc` request is never copy-eligible per `avStreamCopyEligible`, regardless of source codec), polls to `active`, captures the real ffmpeg child PID via `/proc/<pid>/comm`, POSTs cancel (asserts 200 + `status=canceled`), then re-checks the process table and asserts the SAME PID is gone within 15s — explicitly commented as the actual proof, not the DB row.
  - Also asserts the canceled job's `GET /v1/jobs/{id}` exposes no `download_url` (D-11), and includes an optional, best-effort goroutine-count guard (`go_goroutines` via a throwaway pinned `busybox:1.36.1` container joined to av-worker's network namespace) across repeated submit/cancel cycles — skips gracefully (never fails the gate) if that mechanism is unavailable.
  - `bash -n` clean, executable, `grep -qi ffmpeg`/`grep -qi cancel` both match (Task 2's stated verification command).

## Task Commits

Each task was committed atomically:

1. **Task 1: non-resurrection regression tests** - `2de436e` (test)
2. **Task 2: author the live process-death proof script** - `b85d094` (feat)

**Plan metadata:** (this commit) `docs(39-04): partial progress — Tasks 1-2 complete, Task 3 pending operator checkpoint`

_Task 3 (`checkpoint:human-verify`, gate="blocking") is intentionally NOT executed by this agent — see "Task 3: PENDING" below._

## Files Created/Modified

- `internal/jobs/repo_test.go` - `TestFindStaleExcludesCanceled`, `TestCanceledAllowListBackstop`, `TestFindWebhookGapsIgnoresCanceled`
- `internal/reconciler/reconciler_test.go` - `TestSweepNeverResurrectsCanceled`
- `scripts/cancel-liveproof.sh` (new) - operator-run docker-compose live process-death proof for CNCL-03

## Decisions Made

- Followed the plan and 39-CONTEXT.md's locked decisions exactly: D-05 (allow-list is the authoritative backstop), D-10 (explicit FindStale/webhook-gap regression), CNCL-03's process-table-not-DB-row proof shape.
- `/proc/<pid>/comm` exact-match chosen over `grep`-substring-matching `/proc/*/cmdline` for the live-proof script's PID capture, after live-reproducing (against the real `octoconv-av-worker:dev` image) that a substring search self-matches its own invoking `sh -c 'grep -al ffmpeg ...'` command line. Verified live with a real ffmpeg process (`libx265 -preset veryslow`) that the `comm`-based capture correctly finds only the true ffmpeg PID, correctly reports it alive while running, and correctly reports it gone after `kill -9`.
- `synthesizeFixture` uses direct `docker compose exec <argv...>` calls (matching `scripts/keda-av-downscale-survival.sh`'s precedent) rather than a nested `sh -c "..."` string, avoiding a fragile double-quoting layer.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `grep ffmpeg /proc/*/cmdline` self-matches its own invocation**
- **Found during:** Task 2, live smoke-testing the PID-capture logic against the real `octoconv-av-worker:dev` image (a fresh, unused `docker run` container, never the operator's actual compose stack)
- **Issue:** The naive approach (`docker compose exec av-worker sh -c 'grep -al ffmpeg /proc/[0-9]*/cmdline'`) returned the PID of the invoking `sh -c` process itself, because that process's own cmdline literally contains the search pattern `"ffmpeg"` as a grep argument — a false positive that would have made the "process is gone" assertion silently pass even if the real ffmpeg was still alive (since the self-match PID persists across a fresh, different `sh -c` invocation, never matching the SAME PID twice, which could mask a genuine failure to kill the real process, or conversely could produce a spurious non-empty capture that never goes away).
- **Fix:** Rewrote `capturedFfmpegPIDs`/`pidsAlive` to compare `/proc/<pid>/comm` (the bare executable name, e.g. `"ffmpeg"`, `"sh"`) via an exact `=` test, which cannot self-match since the searching shell's own `comm` is `"sh"`, never `"ffmpeg"`.
- **Files modified:** scripts/cancel-liveproof.sh
- **Verification:** Live-reproduced the bug and the fix against three fresh `docker run --entrypoint sleep octoconv-av-worker:dev` containers: (1) confirmed the substring-grep self-match, (2) confirmed the `comm`-based capture correctly isolates only the real `libx265`-running ffmpeg PID, (3) confirmed it correctly transitions from "alive" to "gone" after an explicit `kill -9` of that PID.
- **Committed in:** `b85d094` (Task 2 commit — fixed before commit, not as a follow-up)

**2. [Rule 3 - Blocking] `set -e -o pipefail` would abort the script at the exact moment the proof succeeds**
- **Found during:** Task 2, reasoning through the script's own control flow under its declared `set -euo pipefail`
- **Issue:** A bare (unguarded) command-substitution assignment whose underlying pipeline legitimately returns non-zero — which is exactly what happens when `capturedFfmpegPIDs`/`pidsAlive` correctly find zero matching PIDs (the poll-for-not-yet-spawned case, and the SUCCESS case where the ffmpeg child is confirmed dead) — aborts the whole script under `set -e`, before the corresponding `assert_*` call can even run.
- **Fix:** Added `|| true` at both the in-container shell layer (`; true` as the loop's last statement) and the host-side pipeline layer (`|| true` after the final pipe stage) for both functions.
- **Files modified:** scripts/cancel-liveproof.sh
- **Verification:** Live-tested both functions standalone under `set -e -o pipefail` (bash `-c` snippets) confirming the guarded versions return 0 and empty output on a zero-match case, where the unguarded versions killed the shell.
- **Committed in:** `b85d094` (Task 2 commit — fixed before commit, not as a follow-up)

---

**Total deviations:** 2 auto-fixed (1 bug, 1 blocking) — both found and fixed during live smoke-testing while authoring Task 2, before the task commit; no follow-up fixes needed after commit.
**Impact on plan:** Both fixes are essential correctness properties of the live-proof script itself (a self-matching PID capture or a script that aborts on its own success condition would make the CNCL-03 gate worthless). No scope creep — `scripts/cancel-liveproof.sh` remains the only file this task touches.

## Issues Encountered

None beyond the two deviations above, both resolved before commit. `internal/convert/exec.go` is untouched by this plan (confirmed: its last commit is unrelated and predates Phase 39 entirely). No production code (`internal/`, `cmd/`) was modified by either task — only test files and a new operator script.

## Task 3: live process-death proof — PENDING (operator-run)

**This task was NOT executed by this agent.** Per the plan's frontmatter (`autonomous: false`) and this execution's explicit scope, Task 3 is a `checkpoint:human-verify` gate (`gate="blocking"`) that only the orchestrator/operator may run and approve. `scripts/cancel-liveproof.sh` exists, is syntactically valid, and its PID-capture/alive-check logic has been live-verified in isolation (see Deviation #1 above) — but the script has **never been run end-to-end against a real docker-compose stack + real API + real cancel request**. CNCL-03 remains `Pending` in `REQUIREMENTS.md` and Phase 39's checkbox remains unchecked in `ROADMAP.md`.

### Operator Runbook

1. **Bring up the stack** (from the repo root):
   ```bash
   docker compose up -d --build
   ```
   Confirm all services report healthy/running: `docker compose ps`. In particular, `av-worker` must be built from the current `Dockerfile.av-worker` (which already ships the lavfi/testsrc/sine-capable ffmpeg build the script needs for fixture synthesis).

2. **Run the gate:**
   ```bash
   bash scripts/cancel-liveproof.sh
   ```
   This takes several minutes (the primary proof submits an ~85s 1080p hevc re-encode with a measured p95 RTF of ~4.18 — i.e. several minutes of wall-clock ffmpeg activity before cancel is issued mid-flight; the optional goroutine-guard section adds up to 3 more shorter cycles).

3. **Confirm the PASS summary**, specifically that the transcript shows, in order:
   - `PASS: real ffmpeg child PID(s) captured in av-worker's process table while job is active`
   - `PASS: cancel response status for job ... == canceled`
   - `PASS: ffmpeg PID(s) [...] are gone from the process table within 15s of cancel` — **this is the actual proof; do not approve based on the DB status alone**
   - `PASS: canceled job exposes NO download_url (D-11)`
   - (optional) either a `PASS: go_goroutines stayed within tolerance ...` line, or a `SKIP: ...` line (the goroutine guard is explicitly optional and a `SKIP` is not a failure)
   - Final line: `=== ALL <N> ASSERTIONS PASSED ===` and `CNCL-03: ... were confirmed GONE from the av-worker process table (not merely the DB row) -- PASS`

4. **If the script FAILs at the process-table re-check step** (`STEP 6`): the SIGKILL path did not fire as expected. Do NOT approve. Report the exact FAIL line and the captured PID(s) — this indicates a genuine regression in the cancel-kill wiring built in Plans 01-03, not a script bug (the script's own logic has already been live-verified in isolation, see Deviation #1/#2 above).

5. **Once the operator confirms a clean PASS run:**
   - Mark `CNCL-03` complete in `.planning/REQUIREMENTS.md` (alongside `CNCL-04`, which this partial execution's Task 1 already proves but leaves unmarked pending the whole plan's closure).
   - Check the `39-04-PLAN.md` checkbox and the Phase 39 top-level checkbox in `.planning/ROADMAP.md`.
   - Run `roadmap.update-plan-progress` for phase 39 and `state.advance-plan` to formally close out this plan and Phase 39.
   - This is the orchestrator's responsibility per this execution's explicit scope boundary, not this agent's.

## User Setup Required

None beyond the operator runbook above (docker-compose stack + running `scripts/cancel-liveproof.sh`).

## Next Phase Readiness

- Tasks 1-2 are fully done, committed, and verified green. The non-resurrection guarantee (CNCL-04) is now backed by explicit, passing regression tests at both the SQL layer (`internal/jobs`) and the reconciler layer.
- `scripts/cancel-liveproof.sh` is ready for the operator to run; its internal PID-capture/liveness-check logic has already been live-verified in isolation against the real `octoconv-av-worker:dev` image (not merely `bash -n`).
- **Blocker for phase/plan closure:** Task 3's live operator run has not happened. Phase 39 cannot be marked complete, and CNCL-03 cannot be marked complete in `REQUIREMENTS.md`, until the operator runs `scripts/cancel-liveproof.sh` against a live `docker compose up` stack and confirms the PASS summary per the runbook above.

---
*Phase: 39-cancellation-end-to-end*
*Completed: Tasks 1-2 only — 2026-07-24. Task 3 PENDING operator checkpoint.*

## Self-Check: PASSED

All modified/created files found on disk; both task commits (`2de436e`, `b85d094`) verified present in `git log`. See below.

## Task 3: Live process-death proof — PASSED (2026-07-24, orchestrator-run, operator-approved)

`bash scripts/cancel-liveproof.sh` was run against the live docker-compose stack (k8s down): **12/12 assertions PASSED.**

- Job `95738d6e-31fb-4cec-acb1-fffa0f9c34e1` — a real hevc@1080 re-encode of an 85s 1080p lavfi fixture.
- While `status=active`, the real ffmpeg child **PID 119** was captured in the av-worker container's process table.
- `POST /v1/jobs/{id}/cancel` → `200`, `status=canceled`.
- **Process-table proof (the CNCL-03 guarantee):** PID 119 was **GONE** from the av-worker process table within 15s of cancel — the real SIGKILL fired, not merely the DB row flipping. `go test -race` is explicitly insufficient for this (Phase 9 zombie-reaping precedent).
- `GET /v1/jobs/{id}` = `canceled`, **no `download_url`** (D-11).
- Goroutine-leak guard: `go_goroutines` held at **24 → 24** across 3 repeated cancel cycles (ceiling 34) — no reader/subscriber goroutine leak (Pitfall 1/3).

CNCL-03 (real process death) and CNCL-04 (non-resurrection, via the four regression tests) are both delivered. Phase 39 complete.
