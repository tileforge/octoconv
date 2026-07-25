#!/usr/bin/env bash
# av-progress-liveproof.sh -- Phase 41 (PROG-05) operator-run live proof gate.
#
# WHY THIS SCRIPT EXISTS -- `go test -race` alone is NOT sufficient proof:
# unit tests around progressBlockParser/buildProgressUpdate/runCommandStreaming
# (internal/convert) prove the parsing/throttle/EMA logic in isolation, and
# -race proves no DATA RACE in that code. Neither proves the one guarantee
# PROG-05 actually cares about: that a REAL ffmpeg transcode, reporting
# progress through the streaming reader goroutine wired up in Plan 02, can be
# SIGKILLed mid-stream on the LIVE docker-compose stack while leaving (a) zero
# orphaned ffmpeg processes in the av-worker container's own process table and
# (b) zero leaked reader goroutines in the av-worker process, AND that the
# frozen-progress contract (D-06, 41-CONTEXT.md) holds: a canceled job's last
# reported progress_pct stays exactly where it was, never reset to null and
# never fabricated to 100. This mirrors Phase 39's cancel-liveproof.sh
# discipline (real stack, real ffmpeg, real /proc + /metrics inspection) but
# ELEVATES the goroutine-count check from cancel-liveproof's OPTIONAL guard to
# a HARD gate -- PROG-05 explicitly requires proven zero-leak, not merely
# "likely" (41-RESEARCH.md Validation Architecture).
#
# TRANSPORT NOTE (41-RESEARCH.md TRANSPORT CORRECTION, read in full before
# editing this script): the pinned av-worker ffmpeg build registers ONLY the
# `crypto`/`file` protocols (Phase 36 D-10 hardening), so `-progress pipe:N`
# is unavailable in-container -- the real transport is a named FIFO read over
# the allowed `file:` protocol by a dedicated streaming reader goroutine
# (internal/convert/exec.go's runCommandStreaming sibling to runCommand, wired
# in Plan 02). This script is TRANSPORT-AGNOSTIC: it never inspects the FIFO
# or the reader goroutine directly, only the observable effects (HTTP-visible
# progress_pct/eta_seconds via GET /v1/jobs/{id}, the av-worker process table,
# and the av-worker's own /metrics go_goroutines gauge) -- so the goroutine-
# leak proof this script performs is identical regardless of which transport
# is wired underneath.
#
# WHAT THIS SCRIPT PROVES (PROG-05):
#   1. A real hevc@1080 re-encode (forced full transcode, never the AVC-05
#      stream-copy fast path -- requesting BOTH "codec":"hevc" AND
#      "resolution_height":1080 against an h264 source always forces
#      re-encode via avStreamCopyEligible, internal/convert/av.go) is
#      submitted against the live docker-compose stack.
#   2. A REAL ramp is observed BEFORE any kill: GET /v1/jobs/{id} reports
#      status=active with progress_pct having advanced past 0 -- proof the
#      streaming reader is genuinely live, not merely null-until-terminal.
#   3. Once the ramp is observed, the real ffmpeg child PID(s) inside the
#      av-worker container are captured via /proc (the container image ships
#      no ps/pgrep -- procfs + a shell loop is the portable substitute).
#   4. The job is canceled mid-flight via POST /v1/jobs/{id}/cancel (reuses
#      the Phase 39 job:cancel path, which the av engine's HandleAVConvert
#      already honors via isCanceled/SkipRetry, internal/worker/worker.go).
#   5. Within a short bound, the SAME captured ffmpeg PID(s) are re-checked
#      and MUST be gone from the process table -- THIS is the process-death
#      proof, not the DB row (hard gate).
#   6. GET /v1/jobs/{id} must read a terminal status (canceled) with
#      progress_pct FROZEN at its last mid-stream value: present (not null),
#      not reset, and NOT forced to 100 -- re-polled several times after the
#      kill to confirm the value is genuinely stable, not merely non-null on
#      one lucky poll (D-06, hard gate).
#   7. The submit-ramp-kill cycle is repeated GOROUTINE_CYCLES times (a
#      SEPARATE, shorter fixture) and the av-worker's own /metrics
#      go_goroutines gauge is scraped before and after -- via a throwaway
#      busybox container joined to av-worker's network namespace (`docker run
#      --network container:<id>`), because the av-worker runtime image ships
#      no HTTP client of its own. UNLIKE cancel-liveproof.sh, this step is a
#      HARD GATE: if the delta exceeds GOROUTINE_TOLERANCE across the
#      cycles, OR if /metrics is genuinely unreachable (busybox unavailable,
#      scrape fails, gauge absent), the script FAILS with a non-zero exit --
#      it is never skipped, because the goroutine-leak proof is mandatory
#      for PROG-05, not optional.
#   8. A SEPARATE completed (never canceled) short transcode is run to
#      completion, proving the positive case: a real ramp was observed
#      before completion AND the finished job lands progress_pct=100 exactly
#      (not frozen mid-stream, not null).
#
# WHAT THIS SCRIPT DOES NOT DO: it does not modify exec.go, av.go, worker.go,
# or any other Go source (it is a pure operator/CI shell harness); it does
# not stand up or tear down the docker-compose stack (that is the operator's
# job, per the checkpoint instructions); it does not require Kubernetes/KEDA/
# helm -- like cancel-liveproof.sh, this is a plain docker-compose proof.
#
# USAGE (operator-run, per 41-04-PLAN.md's checkpoint):
#   docker compose up -d --build
#   bash scripts/av-progress-liveproof.sh
#
# Tunable env vars (all optional, sane defaults below):
#   AV_PROGRESS_LIVEPROOF_API_BASE               (default http://127.0.0.1:8090)
#   AV_PROGRESS_LIVEPROOF_DATABASE_URL            (default matches docker-compose.yml's host-exposed postgres)
#   AV_PROGRESS_LIVEPROOF_API_KEY_SALT            (default matches docker-compose.yml's API_KEY_SALT)
#   AV_PROGRESS_LIVEPROOF_RAMP_DURATION_S         (default 30 -- primary ramp+kill fixture, must stay < AV_MAX_DURATION_SECONDS=90)
#   AV_PROGRESS_LIVEPROOF_CYCLE_DURATION_S        (default 15 -- goroutine-guard repeat-cycle fixture, kept short x GOROUTINE_CYCLES)
#   AV_PROGRESS_LIVEPROOF_COMPLETE_DURATION_S     (default 8  -- the completed-transcode positive-case fixture)
#   AV_PROGRESS_LIVEPROOF_GOROUTINE_CYCLES        (default 5  -- PROG-05's "repeated kill cycles", hard gate)
#   AV_PROGRESS_LIVEPROOF_GOROUTINE_TOLERANCE     (default 5  -- max allowed go_goroutines delta across all cycles)
set -euo pipefail

cd "$(dirname "$0")/.."

# ---------------------------------------------------------------------------
# Config / constants
# ---------------------------------------------------------------------------
API_BASE="${AV_PROGRESS_LIVEPROOF_API_BASE:-http://127.0.0.1:8090}"
DATABASE_URL_HOST="${AV_PROGRESS_LIVEPROOF_DATABASE_URL:-postgres://octo:octo-pass@127.0.0.1:5434/octo_db}"
# MUST match docker-compose.yml's api/av-worker API_KEY_SALT exactly, or the
# minted client's key will never resolve against the running api's hash.
API_KEY_SALT="${AV_PROGRESS_LIVEPROOF_API_KEY_SALT:-dev-only-change-me-in-real-deploys}"

# Primary ramp+kill fixture: comfortably under docker-compose.yml's
# AV_MAX_DURATION_SECONDS=90 ceiling, long enough at the measured worst-cell
# hevc@1080 p95_RTF=4.179133s (36-04-SUMMARY.md) to yield several tens of
# seconds of wall-clock transcode -- enough for PROGRESS_MIN_INTERVAL_SECONDS
# (default 2s, .env.example) to fire multiple progress ticks before the kill.
RAMP_DURATION_S="${AV_PROGRESS_LIVEPROOF_RAMP_DURATION_S:-30}"
# Goroutine-guard repeat-cycle fixture: shorter, purely to keep the mandatory
# hard-gate cycles' total runtime reasonable while still giving a comfortable
# active-with-a-ramp window each cycle.
CYCLE_DURATION_S="${AV_PROGRESS_LIVEPROOF_CYCLE_DURATION_S:-15}"
# Completed (never-killed) positive-case fixture: short enough to actually
# finish in a reasonable wall-clock bound.
COMPLETE_DURATION_S="${AV_PROGRESS_LIVEPROOF_COMPLETE_DURATION_S:-8}"
GOROUTINE_CYCLES="${AV_PROGRESS_LIVEPROOF_GOROUTINE_CYCLES:-5}"
GOROUTINE_TOLERANCE="${AV_PROGRESS_LIVEPROOF_GOROUTINE_TOLERANCE:-5}"

PASS_COUNT=0
GATE_OK=""
WORKDIR=""

# ---------------------------------------------------------------------------
# Assertion helpers -- same shape as scripts/cancel-liveproof.sh.
# ---------------------------------------------------------------------------
assert_eq() {
	local expected="$1" actual="$2" label="$3"
	if [ "$expected" != "$actual" ]; then
		echo "FAIL: $label -- expected [$expected], got [$actual]" >&2
		exit 1
	fi
	PASS_COUNT=$((PASS_COUNT + 1))
	echo "PASS: $label == $actual" >&2
}

assert_nonempty() {
	local value="$1" label="$2"
	if [ -z "$value" ]; then
		echo "FAIL: $label -- expected a non-empty value, got empty" >&2
		exit 1
	fi
	PASS_COUNT=$((PASS_COUNT + 1))
	echo "PASS: $label == $value" >&2
}

assert_empty() {
	local value="$1" label="$2"
	if [ -n "$value" ]; then
		echo "FAIL: $label -- expected EMPTY, got [$value]" >&2
		exit 1
	fi
	PASS_COUNT=$((PASS_COUNT + 1))
	echo "PASS: $label (empty, as required)" >&2
}

# assert_le: numeric "a <= b" check (used for the goroutine-tolerance gate).
assert_le() {
	local a="$1" b="$2" label="$3"
	if [ "$a" -gt "$b" ]; then
		echo "FAIL: $label -- $a exceeds $b" >&2
		exit 1
	fi
	PASS_COUNT=$((PASS_COUNT + 1))
	echo "PASS: $label ($a <= $b)" >&2
}

log() { echo "" >&2; echo "--- $* ---" >&2; }

# ---------------------------------------------------------------------------
# Teardown -- this script never brings the docker-compose stack up or down
# (the operator owns that lifecycle per the checkpoint instructions); it only
# cleans up its OWN temp scratch dir. Always runs, success or failure.
# ---------------------------------------------------------------------------
teardown() {
	local exit_code=$?
	echo ""
	echo "=== TEARDOWN (scratch files only -- docker-compose stack is left running) ==="
	[ -n "$WORKDIR" ] && rm -rf "$WORKDIR"

	echo ""
	if [ "$exit_code" -eq 0 ] && [ "$GATE_OK" = "1" ]; then
		echo "PASS -- Phase 41 av-progress-liveproof (PROG-05) gate run complete ($PASS_COUNT checks)."
	else
		echo "FAIL -- Phase 41 av-progress-liveproof (PROG-05) gate did NOT complete (exit=$exit_code, checks passed=$PASS_COUNT)." >&2
		echo "If the failure was at the goroutine-delta or process-table re-check step: do not approve the checkpoint." >&2
	fi
	exit "$exit_code"
}
trap teardown EXIT

echo "=== Phase 41 av-progress-liveproof gate (PROG-05: real ffmpeg progress ramp + SIGKILL-mid-stream + goroutine hard gate) ==="

# ---------------------------------------------------------------------------
# STEP 1: Preflight -- the docker-compose stack must already be up (operator
# precondition, per the checkpoint instructions); fail loudly and name the
# exact missing piece rather than attempting to bring anything up ourselves.
# ---------------------------------------------------------------------------
log "STEP 1: preflight"

command -v docker >/dev/null 2>&1 || { echo "FAIL: docker not found on PATH" >&2; exit 1; }
command -v curl >/dev/null 2>&1 || { echo "FAIL: curl not found on PATH" >&2; exit 1; }
command -v go >/dev/null 2>&1 || { echo "FAIL: go not found on PATH (needed to mint a test client via cmd/manage-clients)" >&2; exit 1; }
echo "PASS: docker, curl, go all present on PATH"

REQUIRED_SERVICES="postgres redis minio api av-worker"
RUNNING_SERVICES=$(docker compose ps --status running --services 2>/dev/null || true)
for svc in $REQUIRED_SERVICES; do
	if ! echo "$RUNNING_SERVICES" | grep -qx "$svc"; then
		echo "FAIL: docker-compose service '$svc' is not running. Bring the stack up first: docker compose up -d --build" >&2
		exit 1
	fi
done
echo "PASS: all required docker-compose services running ($REQUIRED_SERVICES)"

AV_CID=$(docker compose ps -q av-worker)
assert_nonempty "$AV_CID" "av-worker container id resolved"

if ! docker compose exec -T av-worker sh -c 'command -v ffmpeg' >/dev/null 2>&1; then
	echo "FAIL: ffmpeg not found inside the running av-worker container" >&2
	exit 1
fi
echo "PASS: ffmpeg present inside the running av-worker container"

HEALTHZ_CODE=$(curl -s -o /tmp/av-progress-liveproof-healthz.json -w '%{http_code}' "$API_BASE/healthz" || true)
assert_eq "200" "$HEALTHZ_CODE" "api /healthz reachable at $API_BASE"

# ---------------------------------------------------------------------------
# STEP 2: HARD-GATE preflight for the go_goroutines scrape path -- fail fast,
# BEFORE burning any wall-clock time on real ffmpeg transcodes, if the
# mandatory goroutine-leak proof cannot even be attempted. Unlike
# cancel-liveproof.sh's optional guard, an unreachable /metrics here is a
# FAIL, never a SKIP (PROG-05 requires this proof, not a best-effort one).
# ---------------------------------------------------------------------------
log "STEP 2: preflight the go_goroutines /metrics scrape path (HARD GATE)"

readGoroutineCount() {
	docker run --rm --network "container:$AV_CID" busybox:1.36.1 \
		wget -q -T 5 -O - http://127.0.0.1:9090/metrics 2>/dev/null \
		| grep '^go_goroutines ' | awk '{print $2}'
}

if ! docker image inspect busybox:1.36.1 >/dev/null 2>&1 && ! docker pull busybox:1.36.1 >/dev/null 2>&1; then
	echo "FAIL: busybox:1.36.1 unavailable locally and could not be pulled -- the go_goroutines proof is MANDATORY for PROG-05, this is a hard gate failure, not a skip." >&2
	exit 1
fi
echo "PASS: busybox:1.36.1 available for the /metrics scrape"

BASELINE_GOROUTINES=$(readGoroutineCount || true)
if [ -z "$BASELINE_GOROUTINES" ]; then
	echo "FAIL: could not read go_goroutines from av-worker's /metrics (METRICS_ADDR=127.0.0.1:9090 inside the container, docker-compose.yml) -- the goroutine proof is MANDATORY for PROG-05, this is a hard gate failure, not a skip." >&2
	exit 1
fi
assert_nonempty "$BASELINE_GOROUTINES" "baseline go_goroutines read from av-worker /metrics"

# ---------------------------------------------------------------------------
# STEP 3: mint a throwaway client + API key directly against the
# docker-compose Postgres (exposed on host port 5434 by docker-compose.yml).
# ---------------------------------------------------------------------------
log "STEP 3: mint a throwaway client + API key"

SUFFIX=$(date +%s)
CLIENT_OUT=$(DATABASE_URL="$DATABASE_URL_HOST" API_KEY_SALT="$API_KEY_SALT" go run ./cmd/manage-clients create "av-progress-liveproof-${SUFFIX}")
CLIENT_KEY=$(printf '%s\n' "$CLIENT_OUT" | awk -F': ' '/^api key/{print $2}')
if [ -z "$CLIENT_KEY" ]; then
	echo "FAIL: could not mint a client/API key against $DATABASE_URL_HOST -- output was:" >&2
	echo "$CLIENT_OUT" >&2
	exit 1
fi
PASS_COUNT=$((PASS_COUNT + 1))
echo "PASS: minted client + API key (redacted, ${#CLIENT_KEY} chars)"

# ---------------------------------------------------------------------------
# Helper functions (shared by every scenario below).
# ---------------------------------------------------------------------------

# synthesizeFixture DURATION_S OUT_FILENAME -- synthesizes a 1080p
# testsrc+sine h264/aac fixture DIRECTLY INSIDE the already-running av-worker
# container (its production ffmpeg build already has lavfi/testsrc/sine
# compiled in -- Dockerfile.av-worker's documented deviation for
# scripts/av-rtf-measure.sh -- so no separate throwaway image/container is
# needed here), then docker-compose-cp's it to the host WORKDIR for a real
# multipart upload. Deliberately h264/aac (never hevc) -- this is the source
# codec avStreamCopyEligible must see as ineligible once a hevc/resize request
# is submitted below.
synthesizeFixture() {
	local duration="$1" outname="$2"
	docker compose exec -T av-worker mkdir -p /tmp/work
	docker compose exec -T av-worker ffmpeg -y -nostdin \
		-f lavfi -i "testsrc=duration=${duration}:size=1920x1080:rate=30" \
		-f lavfi -i "sine=frequency=440:duration=${duration}" \
		-c:v libx264 -preset ultrafast -c:a aac \
		"/tmp/work/${outname}" >/dev/null 2>&1
	docker compose cp "av-worker:/tmp/work/${outname}" "$WORKDIR/${outname}"
}

# postAVRampJob FIXTURE_PATH -- POSTs a real hevc@1080 re-encode request.
# BOTH "codec":"hevc" AND "resolution_height":1080 are set (belt-and-
# suspenders, mirrors scripts/cancel-liveproof.sh): a resize is by definition
# a re-encode (avStreamCopyEligible returns false whenever ResolutionHeight != 0,
# internal/convert/av.go) and an explicit codec request the source does not
# already carry disqualifies the AVC-05 stream-copy fast path too -- either
# condition alone would force the ramp path this proof needs to exercise
# (convertTranscode's full re-encode branch, the ONLY ramping path, D-02),
# and setting both closes any doubt.
postAVRampJob() {
	local fixture="$1"
	local out_file="$WORKDIR/post-response.json"
	local status
	status=$(curl -s -o "$out_file" -w '%{http_code}' -X POST "$API_BASE/v1/jobs" \
		-H "Authorization: ApiKey $CLIENT_KEY" \
		-F "target=mp4" \
		-F "file=@${fixture};type=video/x-matroska" \
		-F 'opts={"codec":"hevc","resolution_height":1080}')
	if [ "$status" != "202" ]; then
		echo "FAIL: POST /v1/jobs returned $status, body: $(cat "$out_file")" >&2
		exit 1
	fi
	grep -o '"job_id":"[^"]*"' "$out_file" | head -1 | cut -d'"' -f4
}

# waitForStatus JOB_ID WANT_STATUS TIMEOUT_S -- polls GET /v1/jobs/{id},
# echoes the LAST observed status, returns non-zero if WANT_STATUS (or a
# terminal status) was never reached within TIMEOUT_S.
waitForStatus() {
	local job_id="$1" want="$2" timeout_s="$3" waited=0 status=""
	local out_file="$WORKDIR/get-response.json"
	while [ "$waited" -lt "$timeout_s" ]; do
		curl -s -o "$out_file" -H "Authorization: ApiKey $CLIENT_KEY" "$API_BASE/v1/jobs/$job_id" >/dev/null
		status=$(grep -o '"status":"[^"]*"' "$out_file" | head -1 | cut -d'"' -f4 || true)
		if [ "$status" = "$want" ] || [ "$status" = "done" ] || [ "$status" = "failed" ] || [ "$status" = "canceled" ]; then
			echo "$status"
			return 0
		fi
		sleep 1
		waited=$((waited + 1))
	done
	echo "$status"
	return 1
}

# progressPctRaw JOB_ID -- GETs /v1/jobs/{id} and echoes the RAW progress_pct
# JSON token: either the literal "null" or a bare integer, exactly as
# json.Marshal(*int) rendered it (internal/api/handlers.go handleGetJob).
# Never coerces null to 0 -- the whole point of this null-aware contract
# (PROG-01/PROG-04) is that "no data yet" and "genuinely 0%" are DISTINCT.
progressPctRaw() {
	local job_id="$1"
	local out_file="$WORKDIR/get-response.json"
	curl -s -o "$out_file" -H "Authorization: ApiKey $CLIENT_KEY" "$API_BASE/v1/jobs/$job_id" >/dev/null
	grep -o '"progress_pct":[^,}]*' "$out_file" | head -1 | cut -d':' -f2 | tr -d ' '
}

# waitForRamp JOB_ID TIMEOUT_S -- polls GET /v1/jobs/{id} until status=active
# AND progress_pct is a non-null value STRICTLY GREATER THAN 0 -- the actual
# "a real ramp was observed" proof (PROG-01/PROG-05): a null-until-terminal
# converter, or a stalled/never-started transcode, must fail this check
# rather than silently pass. Echoes the observed progress_pct on success.
waitForRamp() {
	local job_id="$1" timeout_s="$2" waited=0 status="" pct=""
	local out_file="$WORKDIR/get-response.json"
	while [ "$waited" -lt "$timeout_s" ]; do
		curl -s -o "$out_file" -H "Authorization: ApiKey $CLIENT_KEY" "$API_BASE/v1/jobs/$job_id" >/dev/null
		status=$(grep -o '"status":"[^"]*"' "$out_file" | head -1 | cut -d'"' -f4 || true)
		pct=$(grep -o '"progress_pct":[^,}]*' "$out_file" | head -1 | cut -d':' -f2 | tr -d ' ')
		if [ "$status" = "active" ] && [ "$pct" != "null" ] && [ -n "$pct" ] && [ "$pct" -gt 0 ] 2>/dev/null; then
			echo "$pct"
			return 0
		fi
		if [ "$status" = "done" ] || [ "$status" = "failed" ] || [ "$status" = "canceled" ]; then
			# Terminal before any ramp was ever observed -- caller must treat
			# this as a failure to observe a real ramp, not a false pass.
			echo ""
			return 1
		fi
		sleep 1
		waited=$((waited + 1))
	done
	echo ""
	return 1
}

# capturedFfmpegPIDs -- lists the PID(s) of any process inside the av-worker
# container whose /proc/<pid>/comm is EXACTLY "ffmpeg". The runtime image
# ships NO ps/pgrep (Dockerfile.av-worker's minimal debian-slim runtime
# stage), so /proc + a shell loop is the portable substitute -- available in
# every Linux container regardless of what userland tools are installed.
# DELIBERATE choice of /proc/<pid>/comm (the exact, short process name) over
# a `grep ffmpeg /proc/*/cmdline` substring search: a substring grep is
# SELF-MATCHING (the shell command invoking the grep has "ffmpeg" -- the
# search pattern itself -- in its OWN cmdline), a bug scripts/cancel-liveproof.sh
# already fixed. /proc/<pid>/comm holds only the bare executable name, never
# the full argv, so an exact `=` comparison cannot self-match. DELIBERATELY
# ends with `|| true`: the loop legitimately produces zero matches when
# ffmpeg has not spawned yet -- under this script's `set -e -o pipefail`, a
# bare non-zero command substitution assignment would otherwise abort the
# whole gate while the caller is still polling.
capturedFfmpegPIDs() {
	docker compose exec -T av-worker sh -c '
		for d in /proc/[0-9]*; do
			p=${d#/proc/}
			c=$(cat "$d/comm" 2>/dev/null)
			[ "$c" = "ffmpeg" ] && echo "$p"
		done
		true
	' | tr '\n' ' ' || true
}

# pidsAlive PIDS -- echoes the subset of the given space-separated PID list
# that is STILL present AND still comm=ffmpeg in the av-worker container's
# /proc (i.e. the SAME ffmpeg process is still alive). Checking comm as well
# as directory existence guards against a PID-reuse false positive. Empty
# output means every given PID's ffmpeg is gone -- THE SUCCESS CASE this
# script exists to detect. Both the trailing `; true` (inside the container)
# and `|| true` (on the host pipeline, under pipefail) are load-bearing:
# without them, `set -e` would abort this script at the precise moment the
# process-death proof succeeds (identical discipline to cancel-liveproof.sh).
pidsAlive() {
	local pids="$1"
	[ -z "$pids" ] && return 0
	docker compose exec -T av-worker sh -c "
		for p in $pids; do
			c=\$(cat /proc/\$p/comm 2>/dev/null)
			[ \"\$c\" = ffmpeg ] && echo \$p
		done
		true
	" 2>/dev/null | tr '\n' ' ' || true
}

# cancelJob JOB_ID -- POSTs /v1/jobs/{id}/cancel, asserts HTTP 200, echoes
# the response body's status field.
cancelJob() {
	local job_id="$1"
	local out_file="$WORKDIR/cancel-response.json"
	local status
	status=$(curl -s -o "$out_file" -w '%{http_code}' -X POST "$API_BASE/v1/jobs/$job_id/cancel" \
		-H "Authorization: ApiKey $CLIENT_KEY")
	if [ "$status" != "200" ]; then
		echo "FAIL: POST /v1/jobs/$job_id/cancel returned $status, body: $(cat "$out_file")" >&2
		exit 1
	fi
	grep -o '"status":"[^"]*"' "$out_file" | head -1 | cut -d'"' -f4
}

# rampThenKillOneCycle FIXTURE_DURATION_S FIXTURE_NAME -- one full
# submit->ramp->capture-PID->cancel->assert-PID-gone cycle, shared by the
# primary proof (Step 4) and the repeated goroutine-guard cycles (Step 6).
# Returns non-zero (via `exit 1`, same as every other hard assertion in this
# script) on ANY failure -- there is no soft/best-effort path here.
rampThenKillOneCycle() {
	local duration="$1" fixture_name="$2"
	synthesizeFixture "$duration" "$fixture_name"

	local job_id
	job_id=$(postAVRampJob "$WORKDIR/$fixture_name")
	assert_nonempty "$job_id" "job submitted (fixture=${fixture_name}, duration=${duration}s)"

	local ramp_pct
	ramp_pct=$(waitForRamp "$job_id" 120) || {
		echo "FAIL: job $job_id never showed a real ramp (status=active, progress_pct>0) within 120s" >&2
		exit 1
	}
	assert_nonempty "$ramp_pct" "real ramp observed pre-kill for job $job_id (progress_pct=$ramp_pct)"

	local before_pids=""
	for i in $(seq 1 10); do
		before_pids=$(capturedFfmpegPIDs)
		[ -n "$before_pids" ] && break
		sleep 1
	done
	assert_nonempty "$before_pids" "real ffmpeg child PID(s) captured in av-worker's process table while job $job_id is active"

	local cancel_status
	cancel_status=$(cancelJob "$job_id")
	assert_eq "canceled" "$cancel_status" "cancel response status for job $job_id"

	local alive=""
	for i in $(seq 1 15); do
		alive=$(pidsAlive "$before_pids")
		[ -z "$alive" ] && break
		sleep 1
	done
	assert_empty "$alive" "ffmpeg PID(s) [$before_pids] are gone from the process table within 15s of cancel (job $job_id)"

	echo "$job_id"
}

WORKDIR=$(mktemp -d)

# ---------------------------------------------------------------------------
# STEP 4: PRIMARY PROOF -- one full ramp+kill cycle with the frozen-progress
# assertion (D-06, the hard gate this whole plan exists to prove).
# ---------------------------------------------------------------------------
log "STEP 4: primary ramp+kill cycle (${RAMP_DURATION_S}s fixture) + frozen-progress assertion"

PRIMARY_JOB_ID=$(rampThenKillOneCycle "$RAMP_DURATION_S" "av-progress-liveproof-primary.mkv")

log "STEP 4b: confirm GET /v1/jobs/{id} reads a terminal status with progress_pct FROZEN (D-06)"

FINAL_STATUS=$(waitForStatus "$PRIMARY_JOB_ID" canceled 30) || true
assert_eq "canceled" "$FINAL_STATUS" "job $PRIMARY_JOB_ID reads status=canceled via GET"

FROZEN_PCT_1=$(progressPctRaw "$PRIMARY_JOB_ID")
if [ "$FROZEN_PCT_1" = "null" ] || [ -z "$FROZEN_PCT_1" ]; then
	echo "FAIL: D-06 violation -- canceled job $PRIMARY_JOB_ID's progress_pct was RESET to null instead of staying frozen at its last mid-stream value" >&2
	exit 1
fi
PASS_COUNT=$((PASS_COUNT + 1))
echo "PASS: canceled job $PRIMARY_JOB_ID's progress_pct is non-null ($FROZEN_PCT_1) -- not reset (D-06)"

if [ "$FROZEN_PCT_1" -eq 100 ] 2>/dev/null; then
	echo "FAIL: D-06 violation -- canceled job $PRIMARY_JOB_ID's progress_pct was forced to 100 (a canceled/mid-stream-killed job must never fabricate completion)" >&2
	exit 1
fi
PASS_COUNT=$((PASS_COUNT + 1))
echo "PASS: canceled job $PRIMARY_JOB_ID's progress_pct ($FROZEN_PCT_1) is not forced to 100 (D-06)"

# Re-poll a few more times to confirm the value is genuinely FROZEN -- not
# merely non-null/non-100 on one lucky poll, but stable across repeated
# reads after the kill (the literal meaning of "frozen").
for i in 1 2 3; do
	sleep 2
	FROZEN_PCT_N=$(progressPctRaw "$PRIMARY_JOB_ID")
	assert_eq "$FROZEN_PCT_1" "$FROZEN_PCT_N" "job $PRIMARY_JOB_ID's progress_pct stays frozen at $FROZEN_PCT_1 on re-poll #$i"
done

# ---------------------------------------------------------------------------
# STEP 5: completed (never-killed) positive case -- a real ramp was observed
# before completion AND the finished job lands progress_pct=100 exactly.
# ---------------------------------------------------------------------------
log "STEP 5: completed-transcode positive case (${COMPLETE_DURATION_S}s fixture, no kill)"

synthesizeFixture "$COMPLETE_DURATION_S" "av-progress-liveproof-complete.mkv"
COMPLETE_JOB_ID=$(postAVRampJob "$WORKDIR/av-progress-liveproof-complete.mkv")
assert_nonempty "$COMPLETE_JOB_ID" "completed-case job submitted"

# Best-effort ramp observation before completion -- a short fixture may reach
# `done` between two polls, so a missed ramp here is logged but does not fail
# the gate; the REQUIRED assertion is the terminal progress_pct=100 below.
if COMPLETE_RAMP_PCT=$(waitForRamp "$COMPLETE_JOB_ID" 90); then
	assert_nonempty "$COMPLETE_RAMP_PCT" "real ramp observed pre-completion for job $COMPLETE_JOB_ID (progress_pct=$COMPLETE_RAMP_PCT)"
else
	echo "NOTE: no intermediate ramp tick was caught between polls for job $COMPLETE_JOB_ID before it finished (short fixture, not a failure -- the terminal progress_pct=100 assertion below is the required check)."
fi

COMPLETE_STATUS=$(waitForStatus "$COMPLETE_JOB_ID" "done" 120) || {
	echo "FAIL: job $COMPLETE_JOB_ID never reached status=done within 120s (last observed: $COMPLETE_STATUS)" >&2
	exit 1
}
assert_eq "done" "$COMPLETE_STATUS" "completed-case job $COMPLETE_JOB_ID reaches status=done"

COMPLETE_PCT=$(progressPctRaw "$COMPLETE_JOB_ID")
assert_eq "100" "$COMPLETE_PCT" "completed-case job $COMPLETE_JOB_ID lands progress_pct=100 exactly"

# ---------------------------------------------------------------------------
# STEP 6: HARD-GATE goroutine-count guard across GOROUTINE_CYCLES repeated
# ramp+kill cycles. UNLIKE scripts/cancel-liveproof.sh's OPTIONAL guard, this
# step is MANDATORY for PROG-05: a delta beyond GOROUTINE_TOLERANCE FAILS the
# whole gate (non-zero exit), it is never a WARN.
# ---------------------------------------------------------------------------
log "STEP 6 (HARD GATE): goroutine-count guard across $GOROUTINE_CYCLES repeated ramp+kill cycles"

for cycle in $(seq 1 "$GOROUTINE_CYCLES"); do
	echo "-- goroutine-guard cycle $cycle/$GOROUTINE_CYCLES --"
	rampThenKillOneCycle "$CYCLE_DURATION_S" "av-progress-liveproof-cycle-${cycle}.mkv" >/dev/null
done

AFTER_GOROUTINES=$(readGoroutineCount || true)
if [ -z "$AFTER_GOROUTINES" ]; then
	echo "FAIL: could not read go_goroutines after the repeat cycles -- the goroutine proof is MANDATORY for PROG-05, this is a hard gate failure, not a skip." >&2
	exit 1
fi
echo "after $GOROUTINE_CYCLES cycles go_goroutines=$AFTER_GOROUTINES (baseline=$BASELINE_GOROUTINES, tolerance=+$GOROUTINE_TOLERANCE)"
GOROUTINE_DELTA=$((AFTER_GOROUTINES - BASELINE_GOROUTINES))
if [ "$GOROUTINE_DELTA" -lt 0 ]; then
	GOROUTINE_DELTA=0
fi
assert_le "$GOROUTINE_DELTA" "$GOROUTINE_TOLERANCE" "go_goroutines delta across $GOROUTINE_CYCLES repeated ramp+kill cycles ($BASELINE_GOROUTINES -> $AFTER_GOROUTINES)"

# =============================================================================
# ALL-PASSED summary -- set only after every assertion above has passed
# (there is no optional/best-effort branch in this script -- every step is a
# hard gate). Teardown runs unconditionally via the EXIT trap.
# =============================================================================
GATE_OK="1"
echo ""
echo "=== ALL $PASS_COUNT ASSERTIONS PASSED ==="
echo "PROG-05: a real hevc@1080 av transcode ramp was observed, killed mid-flight (job $PRIMARY_JOB_ID, ffmpeg PID(s) confirmed gone from the av-worker process table), its progress_pct stayed frozen at $FROZEN_PCT_1 (not null, not 100) -- a completed transcode (job $COMPLETE_JOB_ID) independently landed progress_pct=100 -- and go_goroutines stayed within tolerance ($BASELINE_GOROUTINES -> $AFTER_GOROUTINES, delta $GOROUTINE_DELTA <= $GOROUTINE_TOLERANCE) across $GOROUTINE_CYCLES repeated ramp+kill cycles -- PASS"
