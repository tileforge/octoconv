---
phase: 42-live-push
plan: 04
subsystem: testing
tags: [sse, server-sent-events, ffmpeg, docker-compose, live-proof, bash, curl]

# Dependency graph
requires:
  - phase: 42-live-push (Plan 02)
    provides: "GET /v1/jobs/{id}/events SSE handler (internal/api/sse.go), route wiring (internal/api/routes.go)"
  - phase: 42-live-push (Plan 01)
    provides: "migration 0008 SSE tunables schema/config plumbing"
provides:
  - "scripts/sse-liveproof.sh: docker-compose-live operator gate proving real-socket SSE incremental streaming, heartbeat, clean close, and cross-client 404"
affects: [42-live-push, PUSH-01]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Live-proof bash scripts share: set -euo pipefail + cd repo root; assert_eq/assert_nonempty/assert_le/assert_ge helpers logging to STDERR only; a teardown EXIT trap that cleans only the script's own scratch dir; cmd/manage-clients key minting against the host-exposed Postgres port; a PASS/FAIL summary block (mirrors scripts/cancel-liveproof.sh, scripts/av-progress-liveproof.sh)."
    - "SSE frame capture via `curl -N --max-time <cap> | while IFS= read -r line; do printf '%s %s\\n' \"$(date +%s)\" \"$line\"; done` -- epoch-second timestamp prepended per line, portable (no stdbuf/timeout dependency), with errexit deliberately disabled around the pipeline so a --max-time cutoff produces a clear downstream assertion failure instead of an uninformative `set -e` abort."
    - "Terminal-vs-progress SSE frame classification is content-based, not event-name-based: internal/api/sse.go's emitTerminal reuses the literal `event: progress` name for its one terminal frame, distinguished only by its JSON body omitting the progress_pct/eta_seconds keys entirely (emitTerminal writes {\"status\":...} only)."

key-files:
  created:
    - scripts/sse-liveproof.sh
  modified: []

key-decisions:
  - "Task 2 (the live operator-run proof) is intentionally NOT executed by this agent -- it is a checkpoint:human-verify gate (gate=\"blocking\") the orchestrator/operator owns, per the plan's own frontmatter (autonomous: false) and this execution's explicit scope (Task 1 only)."
  - "PUSH-01 is NOT marked complete in REQUIREMENTS.md by this partial execution -- only the orchestrator, after the operator approves Task 2's live run, should close it out (mirrors Phase 39's 39-04 CNCL-03 and Phase 41's 41-04 PROG-05 convention exactly)."
  - "Both codec:hevc AND resolution_height:1080 are set on the submitted job's opts (belt-and-suspenders, identical rationale to scripts/av-progress-liveproof.sh): avStreamCopyEligible (internal/convert/av.go) returns false whenever ResolutionHeight != 0 OR the requested codec differs from the source's h264, so either condition alone forces the full re-encode ramp path (convertTranscodeWithProgress) -- setting both closes any doubt about which condition actually fired."
  - "SSE_HEARTBEAT_INTERVAL is NOT overridden via a docker-compose.yml edit (out of this task's files_modified scope, scripts/sse-liveproof.sh only). Instead the script sizes its forced re-encode's declared source duration (SSE_LIVEPROOF_SOURCE_DURATION_S, default 15s) against the measured worst-cell hevc@1080 p95_RTF=4.179133s (36-04-SUMMARY.md) so real wall-clock transcode time comfortably exceeds 60s -- more than four 15s heartbeat ticks -- making the heartbeat assertion pass by a wide margin without any env/compose change."
  - "Terminal-frame detection is content-based (absence of the progress_pct JSON key), not name-based -- internal/api/sse.go's emitTerminal literally emits `event: progress` for its one terminal frame too, so the script cannot distinguish frames by event name alone and instead treats the LAST captured `data:` line as the terminal candidate and asserts its body carries only {\"status\":...}."
  - "errexit is deliberately disabled (`set +e` / `set -e`) around the curl-capture pipeline: curl's `--max-time` is a safety cap, not the expected exit path, and under `set -o pipefail` a timeout's non-zero exit would otherwise abort the whole script via `set -e` before any of the frame/terminal-shape assertions get a chance to produce a clear diagnostic."
  - "A second throwaway client/API key is minted up front (mintClient helper, reused for both keys) specifically to prove the cross-client 404 ownership guard (T-42-11) against the SAME job id used for the primary stream proof, mirroring the plan's `key_links` requirement."

patterns-established:
  - "SSE live-proof frame capture pattern: timestamp-prepend every raw response line via a portable `while read` loop (no stdbuf/timeout binary dependency), then classify frames purely from JSON body shape (presence/absence of a specific key) rather than assuming a distinct SSE event name exists for terminal vs. incremental frames."

requirements-completed: []

# Metrics
duration: ~30min (Task 1 only; Task 2 not run)
completed: 2026-07-25 (Task 1 only -- plan NOT closed)
---

# Phase 42 Plan 04: PUSH-01 Live SSE Streaming Proof Script Summary

**Authored scripts/sse-liveproof.sh -- a docker-compose-live operator gate that submits a forced hevc@1080 ffmpeg re-encode, opens GET /v1/jobs/{id}/events over a real socket, and hard-gates on Content-Type: text/event-stream, >=2 event: progress frames at distinct wall-clock timestamps before the stream's one terminal frame, >=1 `: keepalive` heartbeat, a clean close shortly after the terminal frame, and a cross-client 404. Task 2 (the live operator run) has NOT been executed -- PUSH-01 stays Pending until an operator runs the script against a live docker-compose stack and confirms the PASS summary.**

## Live Gate Result (Task 2) -- PASS (operator-run 2026-07-25)

**PUSH-01's live streaming gate PASSED 15/15 on the live docker-compose stack.** A forced hevc@1080 re-encode streamed for 64s over a real socket: **33 `event: progress` frames across 33 DISTINCT wall-clock timestamps** before the one terminal frame `{"status":"done"}` — the decisive proof of real incremental flush (not one buffered write at close) that `httptest.ResponseRecorder` structurally cannot make. Plus **4 `: keepalive` heartbeats**, a **clean 0s close** after the terminal frame, and a **second client's ApiKey → 404** on the same job's `/events` (T-42-11 cross-client ownership). Terminal frame is status-only (no `download_url`, D-02).

Two failures surfaced during the run — BOTH in the harness/environment, NOT the product (proven correct by a manual reproduction: `GET /events` returns 200 and streams `event: progress` frames for an owned job; 404 only cross-client):
- First run: a transient 404 while the `api` container was mid-recreate during an image rebuild — resolved once the api deploy settled.
- `head -n -1` (all-but-last-line) is a GNU coreutils-ism BSD/macOS `head` rejects (`illegal line count -- -1`); replaced with the portable `sed '$d'` (commit `c9f155b`), same harness-portability class as Phase 41's cancel/av-progress liveproof fixes.

PUSH-01 is CLOSED in REQUIREMENTS.md. (Original authoring note preserved below for history.)

### (historical) Task 2 was PENDING at authoring time

### Operator Runbook (to close PUSH-01)

1. Bring up the docker-compose stack with pre-built, non-`latest`-tagged images (postgres, redis, minio, api, av-worker); ensure no k8s stack is running simultaneously (documented daemon-wedge hazard, STATE.md).
2. Ensure migration 0008 (Plan 01) is applied and the SSE route (Plan 02, `GET /v1/jobs/{id}/events`) is deployed on the running `api` service.
3. Run: `bash scripts/sse-liveproof.sh`
4. Confirm the final summary reads:
   ```
   === ALL <N> ASSERTIONS PASSED ===
   PUSH-01: GET /v1/jobs/<job-id>/events streamed Content-Type: text/event-stream, <M> event: progress
   frames across <K> distinct wall-clock timestamps BEFORE its one terminal frame (status=<done|failed|canceled>),
   <H> ': keepalive' heartbeat(s), closed <D>s after the terminal frame (<= 5s tolerance), and a second
   client's ApiKey got 404 opening the same job's /events -- PASS
   ```
5. If any assertion fails (single buffered flush, no heartbeat, no clean close, or a cross-client request that is not 404), capture the script's output and report it -- do NOT approve the checkpoint.
6. Once approved: the orchestrator marks PUSH-01 complete in REQUIREMENTS.md and closes 42-04-PLAN.md (mirrors 39-04/41-04 convention).

## Performance

- **Duration:** ~30 min (Task 1 only)
- **Tasks:** 1 of 2 completed (Task 2 is an operator-run `checkpoint:human-verify` gate, `gate="blocking"`, out of this execution's scope)
- **Files modified:** 1 (created)

## Task Commits

1. **Task 1: Author scripts/sse-liveproof.sh** - `a254f08` (feat)

Task 2 (checkpoint:human-verify, operator-run) is not started -- no commit.

## Accomplishments

- **`scripts/sse-liveproof.sh` authored** (commit `a254f08`):
  - Self-documenting, executable (`chmod +x`) bash script (`set -euo pipefail`) whose header explains why httptest.ResponseRecorder (Plan 02's unit tests) structurally cannot prove real-socket incremental flush, and names the exact STATE.md-carried concern it closes ("Live-verify SSE behavior against the real reverse-proxy/ingress topology... proxy buffering is a named pitfall for long-lived streaming responses").
  - Preflights the docker-compose stack (postgres/redis/minio/api/av-worker running, ffmpeg present in av-worker, `/healthz` reachable) before doing any real work, failing loudly and naming the exact missing piece rather than attempting to bring anything up itself.
  - Mints TWO throwaway clients/API keys directly against the compose Postgres (host port 5434) via `cmd/manage-clients` -- one to open the primary stream, one to prove the cross-client 404 ownership guard (T-42-11) -- via a shared `mintClient` helper that echoes only the key to stdout (all diagnostics to stderr).
  - `synthesizeFixture` builds a 1920x1080 h264/aac testsrc+sine fixture directly inside the running av-worker container (mirrors `scripts/av-progress-liveproof.sh` exactly) and `docker compose cp`'s it to the host for a real multipart upload.
  - `postAVRampJob` submits `{"codec":"hevc","resolution_height":1080}` against the h264/aac source -- both conditions independently force `avStreamCopyEligible` false (`internal/convert/av.go`), guaranteeing the full re-encode ramp path (`convertTranscodeWithProgress`), never the AVC-05 stream-copy fast path (which would emit zero progress ticks and starve the SSE proof of anything to observe).
  - Opens `GET /v1/jobs/{id}/events` via `curl -N --max-time <cap> -D <headers-file>`, piping the response body through a portable `while IFS= read -r line; do printf '%s %s\n' "$(date +%s)" "$line"; done` loop that prepends an epoch-second timestamp to every raw line -- no `stdbuf`/`timeout` binary dependency, works with bash's own line-buffered `read`.
  - Deliberately disables `set -e` (`set +e` / `set -e`) around the capture pipeline: `curl --max-time` is a safety cap only, and under `set -o pipefail` its non-zero exit on a genuine timeout would otherwise abort the whole script via `set -e` before any of the frame-count/terminal-shape assertions could produce a clear, actionable FAIL message.
  - Asserts `Content-Type: text/event-stream` and HTTP 200 from the captured response headers.
  - Parses every captured `data: ` line (each carrying its receive-time epoch, prepended by the capture loop, in strict emission order -- SSE never reorders/buffers writes) and classifies the LAST such line as the terminal candidate: its JSON body must carry ONLY `{"status":...}` (no `progress_pct`/`eta_seconds` key at all), reflecting `internal/api/sse.go`'s `emitTerminal` exactly -- since `emitTerminal` reuses the literal `event: progress` name for its one terminal frame, classification here is content-based, not event-name-based.
  - Asserts >=3 total captured data frames (initial connect emit + >=1 real incremental tick + the terminal frame), >=2 pre-terminal frames, and >=2 DISTINCT wall-clock timestamps among those pre-terminal frames -- the actual "incremental flush, not one buffered write at close" proof that httptest cannot exercise.
  - Asserts >=1 `: keepalive` heartbeat comment line was observed anywhere in the captured stream.
  - Asserts the stream closed within a small tolerance (`SSE_LIVEPROOF_STREAM_CLOSE_TOLERANCE_S`, default 5s) after the terminal frame's timestamp.
  - Asserts a fresh `GET /v1/jobs/{id}/events` request with the SECOND client's ApiKey against the SAME job id returns HTTP 404 -- the live confirmation of the cross-client ownership guard (T-39-01/T-42-04/T-42-11 precedent).
  - Emits a final PASS/FAIL summary (assertion count, observed inter-frame timestamps/span, terminal status, heartbeat count, close delta) in the same shape as `scripts/av-progress-liveproof.sh`.
  - Documents the OrbStack operational discipline (non-`latest` image tags; never run compose + k8s stacks simultaneously) and an explicit heartbeat-cadence note explaining why no `docker-compose.yml`/env override is needed (the forced re-encode's declared duration is sized so real wall-clock transcode time comfortably exceeds four heartbeat intervals).
  - `bash -n` and `shellcheck` (v0.11.0) both report zero issues; `test -x` confirmed executable.
  - No Go source touched (`sse.go`, `routes.go`, `av.go` all read-only inputs to this task).

## Files Created/Modified

- `scripts/sse-liveproof.sh` - Operator-run live SSE streaming proof script (created, not yet run).

## Decisions Made

See `key-decisions` in frontmatter above -- summarized: Task 2 intentionally out of scope for this execution (operator-run checkpoint); PUSH-01 stays Pending in REQUIREMENTS.md; both `codec`/`resolution_height` set to force the re-encode ramp unambiguously; heartbeat proof relies on fixture sizing rather than an env/compose override; terminal-frame classification is content-based since `emitTerminal` reuses the `event: progress` name; `errexit` disabled around the capture pipeline so a `--max-time` cutoff produces a clear assertion failure instead of a bare `set -e` abort.

## Deviations from Plan

None - plan executed exactly as written for Task 1. Task 2 is the plan's own operator-run checkpoint, not a deviation.

## Issues Encountered

None. Static checks (`bash -n`, `shellcheck`) passed on the first attempt.

## User Setup Required

None for Task 1. Task 2 requires an operator to run `bash scripts/sse-liveproof.sh` against a live docker-compose stack per the Operator Runbook above.

## Next Phase Readiness

- `scripts/sse-liveproof.sh` is ready for an operator to run against a live docker-compose stack.
- PUSH-01 stays **Pending** in REQUIREMENTS.md until the operator confirms the PASS summary (Task 2). Do not close 42-04-PLAN.md or mark PUSH-01 complete until that happens.
- No blockers for the script itself; the only remaining work is the operator-run live gate.

---
*Phase: 42-live-push*
*Completed: 2026-07-25 (Task 1 only)*
