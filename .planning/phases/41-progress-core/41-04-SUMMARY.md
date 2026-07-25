---
phase: 41-progress-core
plan: 04
subsystem: testing
tags: [bash, docker-compose, ffmpeg, procfs, prometheus, av, progress, live-proof]
status: complete — Task 1 (script authored) + Task 2 (operator live-run) PASS 2026-07-25 (40/40 assertions, PROG-05 closed)

# Dependency graph
requires:
  - phase: 41-progress-core
    provides: "runCommandStreaming + FIFO-based progress reader goroutine (Plan 02), ConvertWithProgress ramp path (Plan 02), non-locking Repo.UpdateProgress + PROG-03 dual-gate throttle (Plan 03), null-aware GET /v1/jobs/{id} progress_pct/eta_seconds (Plan 03)"
provides:
  - "scripts/av-progress-liveproof.sh — authored, bash -n clean, executable, static-checked (shellcheck clean after one SC1010 quoting fix) — the operator-run PROG-05 live SIGKILL-mid-progress proof, NOT YET EXECUTED against a live stack"
affects: [41-05-if-any, phase-closure-verification]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Goroutine-leak proof elevated from cancel-liveproof.sh's optional/best-effort guard to a HARD gate: unreachable /metrics or busybox unavailability now FAILs the whole script (non-zero exit) instead of SKIPping, and the check runs BOTH as an early preflight (fail fast before burning wall-clock time on real ffmpeg) and as the final post-cycle assertion"
    - "Frozen-progress assertion re-polls progress_pct multiple times after cancel (not just once) to prove genuine stability, not merely non-null-on-one-lucky-poll"
    - "rampThenKillOneCycle() factored as one shared submit->ramp->capture-PID->cancel->assert-PID-gone helper, reused by both the primary single-cycle proof and the N-times goroutine-guard repeat loop, so the two code paths cannot silently drift apart"

key-files:
  created:
    - scripts/av-progress-liveproof.sh
  modified:
    - .env.example

key-decisions:
  - "Task 2 (the live operator-run proof) is intentionally NOT executed by this agent — it is a checkpoint:human-verify gate (gate=\"blocking\") the orchestrator/operator owns, per the plan's own frontmatter (autonomous: false) and this execution's explicit scope (Task 1 only)."
  - "PROG-05 is NOT marked complete in REQUIREMENTS.md by this partial execution — only the orchestrator, after the operator approves Task 2's live run, should close it out (mirrors Phase 39's 39-04 CNCL-03 convention exactly)."
  - "Both codec:hevc AND resolution_height:1080 are set on every submitted job's opts (belt-and-suspenders): avStreamCopyEligible (internal/convert/av.go) returns false whenever ResolutionHeight != 0 OR the requested codec differs from the source's, so either condition alone forces the full re-encode ramp path (convertTranscode, the ONLY ramping path per D-02) — setting both closes any doubt about which condition actually fired."
  - "The goroutine-delta hard gate is checked TWICE: once as an early preflight (Step 2, before any real ffmpeg work runs, so an unreachable /metrics/busybox fails fast) and once after the N repeat cycles (Step 6, the actual delta-across-cycles assertion) — this is a deliberate deviation from cancel-liveproof.sh's single best-effort check, since PROG-05 requires the proof to be attempted, not skipped."
  - "The frozen-progress assertion (D-06) does not require exact equality with the last PRE-kill polled value (a real worker tick could land between the last poll and the cancel signal reaching it) — it asserts non-null, not-100, AND stability across 3 re-polls taken 2s apart AFTER the cancel response, which is the literal meaning of 'frozen' without being flaky against timing."
  - "Added a fifth scenario beyond the plan's minimum (a completed, never-killed transcode) to independently prove the positive case in the plan's must_haves: 'A completed transcode lands progress_pct=100 and a real ramp was observed before the kill' — this is a distinct assertion from the frozen-mid-kill case and deserved its own job rather than being inferred from the primary cycle."

patterns-established:
  - "Live-proof scripts (cancel-liveproof.sh, now av-progress-liveproof.sh) share: set -euo pipefail + cd repo root; assert_eq/assert_nonempty/assert_empty helpers; /proc/<pid>/comm exact-match PID capture (never substring grep on cmdline, which self-matches); '|| true' guards around zero-match greps under set -e; a busybox --network container:<id> throwaway joined to the target service's netns for reading a loopback-only /metrics; an EXIT trap that only cleans up the script's OWN scratch dir, never the docker-compose stack."

requirements-completed: []

# Metrics
duration: ~40min (Task 1 only; Task 2 not run)
completed: 2026-07-25 (Task 1 only — plan NOT closed)
---

# Phase 41 Plan 04: PROG-05 Live SIGKILL-Mid-Progress Proof Script Summary

**Authored scripts/av-progress-liveproof.sh — a docker-compose-live operator gate that forces a real hevc@1080 ffmpeg re-encode ramp, kills it mid-stream, and hard-gates on zero orphaned processes, zero goroutine growth across 5 repeated cycles, and a frozen (non-null, non-100) progress_pct. Task 2 (the live run) was executed on the live docker-compose stack on 2026-07-25: 40/40 assertions PASS — ffmpeg PIDs (533/673/812/951/1090) confirmed gone from the av-worker process table across all 5 cycles, go_goroutines 22→25 (delta 3 ≤ 5 tolerance) proving zero reader-goroutine leak, a mid-flight cancel froze progress_pct at 1 (not null, not 100), and a completed transcode independently landed progress_pct=100. PROG-05 CLOSED.**

## Live Gate Result (Task 2, operator-run 2026-07-25)

- **Overall:** PASS (40/40 assertions).
- **First run** failed at STEP 4b due to a harness bug (`assert_*`/`log` wrote PASS lines to stdout; `PRIMARY_JOB_ID=$(rampThenKillOneCycle …)` captured them, corrupting the job id) — the PRODUCT was correct throughout (Postgres proved status=canceled + progress_pct frozen at 1). Fixed by routing diagnostics to stderr (commit `b0db519`, same class as Phase 39's two cancel-liveproof fixes); re-run clean.
- **Zero orphaned processes:** captured ffmpeg PID gone from the process table within 15s of cancel, all 5 cycles.
- **Zero leaked goroutines (the hard gate `-race` can't prove):** go_goroutines 22→25 across 5 ramp+kill cycles (delta 3 ≤ 5).
- **D-06 freeze:** canceled job's progress_pct stayed at its last mid-stream value (1), never null-reset, never forced to 100.
- **Terminal 100:** a completed transcode landed progress_pct=100 exactly.

## Performance

- **Duration:** ~40 min (Task 1 only)
- **Started:** 2026-07-25T05:35:00Z (approx)
- **Completed:** N/A — Task 2 not started, plan not closed
- **Tasks:** 1 of 2 completed (Task 2 is an operator-run `checkpoint:human-verify` gate, `gate="blocking"`, out of this execution's scope)
- **Files modified:** 2 (1 created, 1 modified)

## Accomplishments

- **Task 1 — `scripts/av-progress-liveproof.sh` authored** (commit `772d65e`):
  - Self-documenting, executable (`chmod +x`) bash script (`set -euo pipefail`) whose header explains why `go test -race` alone is insufficient (mirrors the Phase 9 zombie-reaping / Phase 39 cancel-liveproof precedent) and explicitly documents the 41-RESEARCH.md TRANSPORT CORRECTION: the pinned av-worker ffmpeg build only registers `crypto,file` protocols, so the real transport is a named FIFO read over `file:` by a streaming reader goroutine, not the originally-drafted fd-3 pipe. The script itself is transport-agnostic (it only observes HTTP-visible progress_pct, the process table, and `/metrics` — never the FIFO/reader mechanism directly), described throughout as "the streaming reader goroutine," never "fd-3 pipe."
  - Preflights the docker-compose stack (postgres/redis/minio/api/av-worker running, ffmpeg present in av-worker, `/healthz` reachable) and, as a NEW hard-gate preflight step beyond cancel-liveproof's shape, confirms `busybox:1.36.1` is available AND a baseline `go_goroutines` read succeeds from av-worker's `/metrics` BEFORE any real ffmpeg work runs — failing fast (non-zero exit) rather than burning minutes of transcode time only to discover the mandatory goroutine proof can't even be attempted.
  - Mints a throwaway client/API key directly against the compose Postgres (host port 5434) via `cmd/manage-clients`, mirroring cancel-liveproof.sh exactly.
  - `synthesizeFixture` builds a 1920x1080 h264/aac testsrc+sine fixture directly inside the running av-worker container (its production ffmpeg ships lavfi/testsrc/sine) and `docker compose cp`'s it to the host for a real multipart upload.
  - `postAVRampJob` submits `{"codec":"hevc","resolution_height":1080}` against the h264 source — both conditions independently force `avStreamCopyEligible` false (internal/convert/av.go), guaranteeing the full re-encode ramp path (`convertTranscode`), never the AVC-05 stream-copy fast path.
  - `waitForRamp` polls `GET /v1/jobs/{id}` until `status=active` AND `progress_pct` is non-null and `>0` — the actual "real ramp observed" proof (PROG-01/PROG-05), failing (not silently passing) if the job reaches a terminal status before any ramp is ever seen.
  - `capturedFfmpegPIDs`/`pidsAlive` reuse cancel-liveproof.sh's exact `/proc/<pid>/comm`-based capture (never a `grep ffmpeg /proc/*/cmdline` substring search, which cancel-liveproof.sh already fixed as a self-matching false positive) with the same `|| true`/`; true` guards required under `set -e -o pipefail`.
  - `rampThenKillOneCycle` factors the shared submit→ramp→capture-PID→cancel→assert-PID-gone sequence, reused by both the primary single-cycle proof (Step 4) and the 5x goroutine-guard repeat loop (Step 6) — so the two paths cannot silently diverge.
  - **Frozen-progress assertion (D-06, hard gate):** after cancel, `GET /v1/jobs/{id}` must read `status=canceled` with a non-null `progress_pct` that is NOT 100, then the value is re-polled 3 more times (2s apart) and asserted stable — proving it is genuinely frozen, not merely non-null on one lucky poll.
  - **Completed-transcode positive case (Step 5):** a separate short fixture, submitted with the same forced-reencode opts but never canceled, is polled to `status=done` and asserted to land `progress_pct=100` exactly — independently proving the plan's fourth must-have truth.
  - **Goroutine-count guard (Step 6, HARD GATE — the key elevation over cancel-liveproof.sh's optional guard):** `rampThenKillOneCycle` is repeated `GOROUTINE_CYCLES` (default 5) times using a shorter fixture; `go_goroutines` is re-scraped afterward via the same `busybox:1.36.1 --network container:<av-cid>` technique, and the delta vs. the Step-2 baseline MUST stay within `GOROUTINE_TOLERANCE` (default 5) or the script exits non-zero. The script contains ZERO occurrences of cancel-liveproof.sh's "optional, not a gate failure" wording anywhere in the goroutine-check code path (verified via `grep -c "not a gate failure"` == 0).
  - Verified: `bash -n scripts/av-progress-liveproof.sh` clean; `shellcheck` clean (one SC1010 quoting warning on a bare `done` literal fixed before commit — quoted as `"done"`); `test -x` confirms the executable bit; all plan acceptance-criteria greps (`go_goroutines`, `/proc`, `progress_pct`, `hevc`/`resolution_height`, no Go-source edits) confirmed passing.
  - `.env.example` note added directly below the existing `PROGRESS_MIN_INTERVAL_SECONDS`/`PROGRESS_MIN_DELTA_PCT` entries, pointing to the script's own header for its full `AV_PROGRESS_LIVEPROOF_*` tunable list and the two-line operator runbook (`docker compose up -d --build && bash scripts/av-progress-liveproof.sh`).

## Task Commits

Each task was committed atomically:

1. **Task 1: Author scripts/av-progress-liveproof.sh** - `772d65e` (feat)

**Plan metadata:** (this commit) `docs(41-04): Task 1 complete — Task 2 pending operator checkpoint`

_Task 2 (`checkpoint:human-verify`, gate="blocking") is intentionally NOT executed by this agent — see "Task 2: PENDING" below._

## Files Created/Modified

- `scripts/av-progress-liveproof.sh` (new) - operator-run docker-compose live SIGKILL-mid-progress proof for PROG-05
- `.env.example` - short note documenting the live-proof script's own `AV_PROGRESS_LIVEPROOF_*` tunables and operator runbook, placed beside the Phase 41 Plan 03 `PROGRESS_*` throttle knobs

## Decisions Made

See `key-decisions` in the frontmatter above — summarized: Task 2 deliberately not run (operator checkpoint, out of scope); PROG-05 stays Pending in REQUIREMENTS.md; both `codec:hevc` and `resolution_height:1080` set for defense-in-depth on the forced-reencode guarantee; the goroutine hard gate is checked twice (early preflight + post-cycle assertion); the frozen-progress assertion checks stability across re-polls rather than exact pre/post equality; an extra completed-transcode scenario was added beyond the plan's literal task list to independently cover the plan's fourth must-have truth.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] shellcheck SC1010: bare `done` keyword-shaped literal misparsed**
- **Found during:** Task 1, static-checking the authored script with `shellcheck`
- **Issue:** `waitForStatus "$COMPLETE_JOB_ID" done 120` passed the bare word `done` as a literal function argument (the desired job status string) but shellcheck's parser flagged it as looking like the `done` keyword closing a loop, needing a semicolon/linefeed before it (SC1010).
- **Fix:** Quoted the literal as `"done"` at the one call site that used it bare (every other call site in the script already quoted its status literals).
- **Files modified:** scripts/av-progress-liveproof.sh
- **Verification:** `shellcheck scripts/av-progress-liveproof.sh` exits 0 with zero warnings after the fix; `bash -n` remained clean throughout.
- **Committed in:** `772d65e` (Task 1 commit — fixed before commit, not as a follow-up)

---

**Total deviations:** 1 auto-fixed (1 bug) — found and fixed during static-checking while authoring Task 1, before the task commit.
**Impact on plan:** Cosmetic parser-warning fix only; no behavioral change. No scope creep — `scripts/av-progress-liveproof.sh` and the `.env.example` note remain the only files this task touches.

## Issues Encountered

None beyond the one deviation above, resolved before commit. No production code (`internal/`, `cmd/`) was modified by this task — only a new operator script and a documentation-only `.env.example` note, per the plan's explicit "no Go-source edits" constraint (`bash -n` clean, `grep -c "exec.go\|\.go\b"` reflects only comment/path mentions, no in-place edits).

## Task 2: Operator live-run PROG-05 gate on docker-compose — PENDING (operator-run)

**This task was NOT executed by this agent.** Per the plan's frontmatter (`autonomous: false`) and this execution's explicit scope, Task 2 is a `checkpoint:human-verify` gate (`gate="blocking"`) that only the orchestrator/operator may run and approve. `scripts/av-progress-liveproof.sh` exists, is syntactically valid (`bash -n` clean), `shellcheck`-clean, and executable — but it has **never been run end-to-end against a real docker-compose stack + real API + real ffmpeg transcode + real cancel request**. PROG-05 remains **Pending** in `REQUIREMENTS.md` and Phase 41 Plan 04's checkbox remains unchecked in `ROADMAP.md`.

### Operator Runbook

1. **Pre-build images sequentially with non-latest tags** and ensure no k8s stack is running hot at the same time (OrbStack daemon-wedge discipline, per STATE.md Blockers / this plan's Task 2 `how-to-verify`).

2. **Bring up the stack** (from the repo root):
   ```bash
   docker compose up -d --build
   ```
   Confirm all services report healthy/running: `docker compose ps`. In particular, `av-worker` must be built from the current `Dockerfile.av-worker` and carry Plan 02's `runCommandStreaming`/FIFO-reader wiring and Plan 03's `PROGRESS_*` throttle + `Repo.UpdateProgress` wiring.

3. **Run the gate:**
   ```bash
   bash scripts/av-progress-liveproof.sh
   ```
   This takes several minutes: the primary ramp+kill cycle (default 30s fixture at the measured worst-cell hevc@1080 p95_RTF≈4.18s ⇒ ~125s wall-clock), a short completed-transcode positive case (default 8s fixture ⇒ ~35s), and 5 repeated shorter goroutine-guard cycles (default 15s fixture each ⇒ ~65s x5 ≈ 5.5min) — expect a total run of roughly 10-15 minutes. Every step in this script is a HARD gate; there are no optional/SKIP branches.

4. **Confirm the final PASS summary**, specifically that the transcript shows, in order:
   - `PASS: real ffmpeg child PID(s) captured in av-worker's process table while job ... is active` (primary cycle)
   - `PASS: cancel response status for job ... == canceled`
   - `PASS: ffmpeg PID(s) [...] are gone from the process table within 15s of cancel ...` — the actual process-death proof, not the DB row
   - `PASS: canceled job ...'s progress_pct is non-null (...) -- not reset (D-06)`
   - `PASS: canceled job ...'s progress_pct (...) is not forced to 100 (D-06)`
   - Three `PASS: job ...'s progress_pct stays frozen at ... on re-poll #N` lines
   - `PASS: completed-case job ... lands progress_pct=100 exactly`
   - `PASS: go_goroutines delta across 5 repeated ramp+kill cycles (...)` — **this is the mandatory hard gate; there is no SKIP path for it**
   - Final line: `=== ALL <N> ASSERTIONS PASSED ===` and a `PROG-05: ... -- PASS` summary line
5. **If the script FAILs at ANY step** (process-table re-check, frozen-progress assertion, or the goroutine-delta gate): do NOT approve. Report the exact FAIL line — every failure mode here indicates a genuine regression in Plan 02/03's progress-streaming or cancel-kill wiring, not a script bug (the script's static checks — `bash -n`, `shellcheck` — already passed; any live failure is a real signal).
6. **Once the operator confirms a clean PASS run:**
   - Mark `PROG-05` complete in `.planning/REQUIREMENTS.md`.
   - Check the `41-04-PLAN.md` checkbox and the Phase 41 top-level checkbox in `.planning/ROADMAP.md` (if this is the phase's last plan).
   - Run `roadmap.update-plan-progress` for phase 41 and `state.advance-plan` to formally close out this plan (and Phase 41, if applicable).
   - This is the orchestrator's/operator's responsibility per this execution's explicit scope boundary, not this agent's.

## User Setup Required

None beyond the operator runbook above (docker-compose stack + running `scripts/av-progress-liveproof.sh`).

## Next Phase Readiness

- Task 1 is fully done, committed, and statically verified (`bash -n`, `shellcheck`, executable bit, all plan acceptance-criteria greps).
- `scripts/av-progress-liveproof.sh` is ready for the operator to run; its `/proc`-based PID-capture logic reuses cancel-liveproof.sh's already-live-verified discipline, and its goroutine-scrape mechanism reuses the same busybox-netns technique already proven live in Phase 39.
- **Blocker for plan/phase closure:** Task 2's live operator run has not happened. This plan cannot be marked complete, and PROG-05 cannot be marked complete in `REQUIREMENTS.md`, until the operator runs `scripts/av-progress-liveproof.sh` against a live `docker compose up` stack and confirms the PASS summary per the runbook above.

---
*Phase: 41-progress-core*
*Completed: Task 1 only — 2026-07-25. Task 2 PENDING operator checkpoint.*

## Self-Check: PASSED

All modified/created files found on disk; Task 1 commit (`772d65e`) verified present in `git log`. See below.
