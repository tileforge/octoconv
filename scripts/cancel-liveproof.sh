#!/usr/bin/env bash
# cancel-liveproof.sh -- Phase 39 (CNCL-03) operator-run live process-death
# proof gate.
#
# WHY THIS SCRIPT EXISTS -- `go test -race` alone is NOT sufficient proof:
# unit/integration tests (internal/jobs/repo_test.go's
# TestCanceledAllowListBackstop, internal/reconciler's
# TestSweepNeverResurrectsCanceled) prove the Postgres-authoritative
# non-resurrection backstop (D-05/D-10) -- that a canceled job's status can
# never be flipped back to active/done/failed. They do NOT, and cannot,
# prove that a REAL external ffmpeg child process spawned by the av-worker
# is genuinely SIGKILLed when a job is canceled mid-flight: that requires a
# real docker-compose stack, a real long-running ffmpeg re-encode, a real
# cancel request, and a real look at the OS process table. This mirrors
# Phase 9's zombie-reaping precedent (RETROSPECTIVE.md): a live process-kill
# proof found an actual zombie-reaping gap (fixed with tini as PID 1) that a
# mocked/unit test had declared safe. The DB row reading "canceled" is
# necessary but NOT sufficient -- the only genuine proof is watching the
# ffmpeg PID disappear from the av-worker container's own process table.
#
# WHAT THIS SCRIPT PROVES (CNCL-03):
#   1. A real hevc@1080 re-encode (forced full transcode, never the AVC-05
#      stream-copy fast path -- requesting "hevc" against an h264 source
#      always forces re-encode, internal/convert/av.go avStreamCopyEligible)
#      is submitted against the live docker-compose stack.
#   2. Once the job reaches status=active, the real ffmpeg child PID(s)
#      inside the av-worker container are captured via /proc (the container
#      image ships no ps/pgrep -- procfs + grep is the portable substitute).
#   3. POST /v1/jobs/{id}/cancel is issued; the response must be 200 with
#      status=canceled.
#   4. Within a short bound (a few seconds -- the SIGKILL via
#      internal/convert/exec.go's ctx.Done()-triggered process-group kill is
#      immediate), the SAME captured ffmpeg PID(s) are re-checked and MUST be
#      gone from the process table. THIS is the proof, not the DB row.
#   5. GET /v1/jobs/{id} must read status=canceled and expose NO
#      download_url (D-11 -- a canceled job never has output).
#   6. OPTIONAL, best-effort: a small repeated submit/cancel cycle checks the
#      av-worker's own /metrics go_goroutines gauge does not grow across
#      cycles (Pitfall 1/3 goroutine-leak guard). This step is genuinely
#      optional (per this plan's spec) -- the av-worker runtime image ships
#      no HTTP client, so this reads /metrics via a throwaway busybox
#      container joined to av-worker's network namespace
#      (`docker run --network container:<id>`); if that is unavailable
#      (offline, no docker network access) this step is SKIPPED with a clear
#      message and never fails the overall gate.
#
# WHAT THIS SCRIPT DOES NOT DO: it does not modify exec.go (already the
# load-bearing SIGKILL path, untouched by this whole phase), does not stand
# up or tear down the docker-compose stack (that is the operator's job, per
# the checkpoint instructions), and does not require Kubernetes/KEDA/helm --
# unlike this phase's sibling live-proof scripts (scripts/keda-av-*.sh),
# CNCL-03 is a plain docker-compose proof.
#
# USAGE (operator-run, per 39-04-PLAN.md's checkpoint):
#   docker compose up -d --build
#   bash scripts/cancel-liveproof.sh
set -euo pipefail

cd "$(dirname "$0")/.."

# ---------------------------------------------------------------------------
# Config / constants
# ---------------------------------------------------------------------------
API_BASE="${CANCEL_LIVEPROOF_API_BASE:-http://127.0.0.1:8090}"
DATABASE_URL_HOST="${CANCEL_LIVEPROOF_DATABASE_URL:-postgres://octo:octo-pass@127.0.0.1:5434/octo_db}"
# MUST match docker-compose.yml's api/av-worker API_KEY_SALT exactly, or the
# minted client's key will never resolve against the running api's hash.
API_KEY_SALT="${CANCEL_LIVEPROOF_API_KEY_SALT:-dev-only-change-me-in-real-deploys}"

# Near AV_MAX_DURATION_SECONDS=90's ceiling (docker-compose.yml), same
# provenance as scripts/keda-av-downscale-survival.sh's LONG_FIXTURE_DURATION_S:
# comfortably under the ffprobe-enforced cap while, at the measured
# worst-cell hevc@1080 p95_RTF=4.179133s (36-04-SUMMARY.md), yielding a
# wall-clock transcode of several minutes -- comfortably long enough to
# capture an active ffmpeg PID and cancel mid-flight.
LONG_FIXTURE_DURATION_S="${CANCEL_LIVEPROOF_LONG_DURATION_S:-85}"
# Optional goroutine-guard repeat cycles use a shorter fixture (still several
# tens of seconds of wall-clock transcode at hevc@1080) purely to keep the
# total gate runtime reasonable while still giving a comfortable
# active-before-cancel window each cycle.
GOROUTINE_CYCLE_DURATION_S="${CANCEL_LIVEPROOF_CYCLE_DURATION_S:-20}"
GOROUTINE_CYCLES="${CANCEL_LIVEPROOF_GOROUTINE_CYCLES:-3}"
GOROUTINE_TOLERANCE="${CANCEL_LIVEPROOF_GOROUTINE_TOLERANCE:-10}"

PASS_COUNT=0
GATE_OK=""
WORKDIR=""

# ---------------------------------------------------------------------------
# Assertion helpers -- same shape as scripts/keda-*.sh siblings.
# ---------------------------------------------------------------------------
assert_eq() {
	local expected="$1" actual="$2" label="$3"
	if [ "$expected" != "$actual" ]; then
		echo "FAIL: $label -- expected [$expected], got [$actual]" >&2
		exit 1
	fi
	PASS_COUNT=$((PASS_COUNT + 1))
	echo "PASS: $label == $actual"
}

assert_nonempty() {
	local value="$1" label="$2"
	if [ -z "$value" ]; then
		echo "FAIL: $label -- expected a non-empty value, got empty" >&2
		exit 1
	fi
	PASS_COUNT=$((PASS_COUNT + 1))
	echo "PASS: $label == $value"
}

assert_empty() {
	local value="$1" label="$2"
	if [ -n "$value" ]; then
		echo "FAIL: $label -- expected EMPTY, got [$value]" >&2
		exit 1
	fi
	PASS_COUNT=$((PASS_COUNT + 1))
	echo "PASS: $label (empty, as required)"
}

log() { echo ""; echo "--- $* ---"; }

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
		echo "PASS -- Phase 39 cancel-liveproof (CNCL-03) gate run complete ($PASS_COUNT checks)."
	else
		echo "FAIL -- Phase 39 cancel-liveproof (CNCL-03) gate did NOT complete (exit=$exit_code, checks passed=$PASS_COUNT)." >&2
		echo "If the failure was at the process-table re-check step: the SIGKILL path did NOT fire -- do not approve the checkpoint." >&2
	fi
	exit "$exit_code"
}
trap teardown EXIT

echo "=== Phase 39 cancel-liveproof gate (CNCL-03: real ffmpeg child death after cancel) ==="

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

HEALTHZ_CODE=$(curl -s -o /tmp/cancel-liveproof-healthz.json -w '%{http_code}' "$API_BASE/healthz" || true)
assert_eq "200" "$HEALTHZ_CODE" "api /healthz reachable at $API_BASE"

# ---------------------------------------------------------------------------
# STEP 2: mint a throwaway client + API key directly against the
# docker-compose Postgres (exposed on host port 5434 by docker-compose.yml --
# no port-forward needed, unlike this phase's k8s sibling scripts).
# ---------------------------------------------------------------------------
log "STEP 2: mint a throwaway client + API key"

SUFFIX=$(date +%s)
CLIENT_OUT=$(DATABASE_URL="$DATABASE_URL_HOST" API_KEY_SALT="$API_KEY_SALT" go run ./cmd/manage-clients create "cancel-liveproof-${SUFFIX}")
CLIENT_KEY=$(printf '%s\n' "$CLIENT_OUT" | awk -F': ' '/^api key/{print $2}')
if [ -z "$CLIENT_KEY" ]; then
	echo "FAIL: could not mint a client/API key against $DATABASE_URL_HOST -- output was:" >&2
	echo "$CLIENT_OUT" >&2
	exit 1
fi
PASS_COUNT=$((PASS_COUNT + 1))
echo "PASS: minted client + API key (redacted, ${#CLIENT_KEY} chars)"

# ---------------------------------------------------------------------------
# Helper functions (shared by the primary proof and the optional
# goroutine-guard repeat cycles below).
# ---------------------------------------------------------------------------

# synthesizeFixture DURATION_S OUT_FILENAME -- synthesizes a 1080p
# testsrc+sine h264/aac fixture DIRECTLY INSIDE the already-running av-worker
# container (its production ffmpeg build already has lavfi/testsrc/sine
# compiled in -- Dockerfile.av-worker's documented deviation for
# scripts/av-rtf-measure.sh -- so no separate throwaway image/container is
# needed here), then docker-compose-cp's it to the host WORKDIR for a real
# multipart upload.
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

# postHevcJob FIXTURE_PATH -- POSTs a real hevc@1080 re-encode request
# (never the AVC-05 stream-copy fast path) and echoes the job_id.
postHevcJob() {
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
		sleep 2
		waited=$((waited + 2))
	done
	echo "$status"
	return 1
}

# capturedFfmpegPIDs -- lists the PID(s) of any process inside the av-worker
# container whose /proc/<pid>/comm is EXACTLY "ffmpeg". The runtime image
# ships NO ps/pgrep (Dockerfile.av-worker's minimal debian-slim runtime
# stage), so /proc + a shell loop is the portable substitute -- available in
# every Linux container regardless of what userland tools are installed.
# DELIBERATE choice of /proc/<pid>/comm (the exact, short process name) over
# a `grep ffmpeg /proc/*/cmdline` substring search: a substring grep is
# SELF-MATCHING here -- the very shell command that invokes the grep (e.g.
# `sh -c 'grep -al ffmpeg /proc/[0-9]*/cmdline'`) has its OWN cmdline
# containing the literal substring "ffmpeg" (the search pattern itself), so
# it always finds itself as a false positive (verified live during Task 2
# authoring: this exact self-match reproduced with a real docker container).
# /proc/<pid>/comm holds only the bare executable name ("sh", "ffmpeg"),
# never the full argv, so an exact `=` comparison cannot self-match.
# DELIBERATELY ends with `|| true`: the loop legitimately produces zero
# matches when ffmpeg has not spawned yet -- under this script's
# `set -e -o pipefail`, a bare non-zero command substitution assignment
# would otherwise abort the whole gate while the caller is still polling.
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
# as directory existence guards against a PID-reuse false positive: if the
# kernel recycles a dead ffmpeg's PID onto an unrelated new process before
# this check runs, `[ -d /proc/$p ]` alone would wrongly report "still
# alive" even though the ORIGINAL ffmpeg is long gone. Empty output means
# every given PID's ffmpeg is gone -- THE SUCCESS CASE this script exists to
# detect. The inner `for` loop's own exit status is that of its LAST test,
# which is FALSE (non-zero) exactly when the last-checked PID is already
# gone -- i.e. exactly on success. Both the trailing `; true` (inside the
# container) and `|| true` (on the host pipeline, under pipefail) are
# load-bearing: without them, `set -e` would abort this script at the
# precise moment the process-death proof succeeds.
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

WORKDIR=$(mktemp -d)

# ---------------------------------------------------------------------------
# STEP 3: synthesize the LONG (near-90s ceiling) fixture and submit the
# primary hevc@1080 re-encode job.
# ---------------------------------------------------------------------------
log "STEP 3: synthesize LONG (${LONG_FIXTURE_DURATION_S}s, 1080p) fixture, submit hevc@1080 job"

synthesizeFixture "$LONG_FIXTURE_DURATION_S" "cancel-liveproof-long.mkv"
assert_nonempty "$(ls -la "$WORKDIR/cancel-liveproof-long.mkv" 2>/dev/null || true)" \
	"LONG lavfi fixture synthesized (${LONG_FIXTURE_DURATION_S}s, 1920x1080, h264/aac in mkv)"

JOB_ID=$(postHevcJob "$WORKDIR/cancel-liveproof-long.mkv")
assert_nonempty "$JOB_ID" "primary hevc@1080 re-encode job submitted"

# ---------------------------------------------------------------------------
# STEP 4: wait for active, capture the real ffmpeg child PID(s) from the
# process table.
# ---------------------------------------------------------------------------
log "STEP 4: wait for job to reach status=active, capture real ffmpeg PID(s)"

ACTIVE_STATUS=$(waitForStatus "$JOB_ID" active 120) || {
	echo "FAIL: job $JOB_ID never reached status=active within 120s (last observed: $ACTIVE_STATUS)" >&2
	exit 1
}
assert_eq "active" "$ACTIVE_STATUS" "job $JOB_ID reaches status=active"

BEFORE_PIDS=""
for i in $(seq 1 10); do
	BEFORE_PIDS=$(capturedFfmpegPIDs)
	[ -n "$BEFORE_PIDS" ] && break
	sleep 1
done
assert_nonempty "$BEFORE_PIDS" "real ffmpeg child PID(s) captured in av-worker's process table while job is active"

# ---------------------------------------------------------------------------
# STEP 5: cancel mid-flight; assert 200 + status=canceled from the response.
# ---------------------------------------------------------------------------
log "STEP 5: POST /v1/jobs/{id}/cancel"

CANCEL_STATUS=$(cancelJob "$JOB_ID")
assert_eq "canceled" "$CANCEL_STATUS" "cancel response status for job $JOB_ID"

# ---------------------------------------------------------------------------
# STEP 6: THE PROOF -- re-check the process table (NOT the DB row) and
# assert the captured ffmpeg PID(s) are GONE, within a short bound (the
# SIGKILL via internal/convert/exec.go's ctx.Done() handler is immediate).
# ---------------------------------------------------------------------------
log "STEP 6: process-table proof -- assert the captured ffmpeg PID(s) are gone"

ALIVE=""
for i in $(seq 1 15); do
	ALIVE=$(pidsAlive "$BEFORE_PIDS")
	[ -z "$ALIVE" ] && break
	sleep 1
done
echo "NOTE: this assertion reads the OS process table inside the av-worker container -- it is the actual proof of process death, NOT the jobs.status DB column (which merely reflects the intended terminal state)."
assert_empty "$ALIVE" "ffmpeg PID(s) [$BEFORE_PIDS] are gone from the process table within 15s of cancel"

# ---------------------------------------------------------------------------
# STEP 7: confirm the job reads canceled with no download_url (D-11).
# ---------------------------------------------------------------------------
log "STEP 7: confirm GET /v1/jobs/{id} reads canceled, no download_url (D-11)"

FINAL_STATUS=$(waitForStatus "$JOB_ID" canceled 30) || true
assert_eq "canceled" "$FINAL_STATUS" "job $JOB_ID reads status=canceled via GET"

FINAL_BODY=$(cat "$WORKDIR/get-response.json")
if echo "$FINAL_BODY" | grep -q '"download_url"'; then
	echo "FAIL: D-11 violation -- a canceled job exposes a download_url: $FINAL_BODY" >&2
	exit 1
fi
PASS_COUNT=$((PASS_COUNT + 1))
echo "PASS: canceled job exposes NO download_url (D-11)"

# ---------------------------------------------------------------------------
# STEP 8 (OPTIONAL, best-effort): goroutine-count guard across repeated
# submit/cancel cycles (Pitfall 1/3). The av-worker runtime image ships no
# HTTP client (no curl/wget) to hit its own loopback-only /metrics, so this
# reads /metrics via a throwaway pinned busybox container joined to
# av-worker's network namespace. If that is unavailable for any reason
# (offline, no docker network reachability), this step is SKIPPED with a
# clear message -- it is explicitly optional per this plan and never fails
# the overall gate.
# ---------------------------------------------------------------------------
log "STEP 8 (optional): goroutine-count guard across $GOROUTINE_CYCLES repeated cancel cycles"

readGoroutineCount() {
	docker run --rm --network "container:$AV_CID" busybox:1.36.1 \
		wget -q -T 5 -O - http://127.0.0.1:9090/metrics 2>/dev/null \
		| grep '^go_goroutines ' | awk '{print $2}'
}

if ! docker image inspect busybox:1.36.1 >/dev/null 2>&1 && ! docker pull busybox:1.36.1 >/dev/null 2>&1; then
	echo "SKIP: busybox:1.36.1 unavailable locally and could not be pulled -- goroutine-count guard skipped (optional, not a gate failure)"
else
	BASELINE_GOROUTINES=$(readGoroutineCount || true)
	if [ -z "$BASELINE_GOROUTINES" ]; then
		echo "SKIP: could not read go_goroutines from av-worker's /metrics -- goroutine-count guard skipped (optional, not a gate failure)"
	else
		echo "baseline go_goroutines=$BASELINE_GOROUTINES"
		for cycle in $(seq 1 "$GOROUTINE_CYCLES"); do
			echo "-- goroutine-guard cycle $cycle/$GOROUTINE_CYCLES --"
			synthesizeFixture "$GOROUTINE_CYCLE_DURATION_S" "cancel-liveproof-cycle-${cycle}.mkv"
			CYCLE_JOB_ID=$(postHevcJob "$WORKDIR/cancel-liveproof-cycle-${cycle}.mkv")
			CYCLE_STATUS=$(waitForStatus "$CYCLE_JOB_ID" active 60) || true
			if [ "$CYCLE_STATUS" != "active" ]; then
				echo "SKIP: cycle $cycle job $CYCLE_JOB_ID never reached active (status=$CYCLE_STATUS) -- skipping remaining goroutine-guard cycles (optional, not a gate failure)"
				break
			fi
			CANCEL_STATUS=$(cancelJob "$CYCLE_JOB_ID")
			echo "cycle $cycle: job $CYCLE_JOB_ID cancel response status=$CANCEL_STATUS"
		done

		AFTER_GOROUTINES=$(readGoroutineCount || true)
		if [ -z "$AFTER_GOROUTINES" ]; then
			echo "SKIP: could not read go_goroutines after the repeat cycles -- guard inconclusive (optional, not a gate failure)"
		else
			echo "after $GOROUTINE_CYCLES cycles go_goroutines=$AFTER_GOROUTINES (baseline=$BASELINE_GOROUTINES, tolerance=+$GOROUTINE_TOLERANCE)"
			CEILING=$((BASELINE_GOROUTINES + GOROUTINE_TOLERANCE))
			if [ "$AFTER_GOROUTINES" -gt "$CEILING" ]; then
				echo "WARN: go_goroutines grew from $BASELINE_GOROUTINES to $AFTER_GOROUTINES (> ceiling $CEILING) across $GOROUTINE_CYCLES cancel cycles -- possible goroutine leak (Pitfall 1/3). This is a WARNING, not a gate failure (optional guard per plan spec) -- investigate before relying on repeated cancels in production." >&2
			else
				PASS_COUNT=$((PASS_COUNT + 1))
				echo "PASS: go_goroutines stayed within tolerance across $GOROUTINE_CYCLES repeated cancel cycles ($BASELINE_GOROUTINES -> $AFTER_GOROUTINES, ceiling $CEILING)"
			fi
		fi
	fi
fi

# =============================================================================
# ALL-PASSED summary -- set only after every REQUIRED (non-optional)
# assertion above has passed. Teardown runs unconditionally via the EXIT trap.
# =============================================================================
GATE_OK="1"
echo ""
echo "=== ALL $PASS_COUNT ASSERTIONS PASSED ==="
echo "CNCL-03: a real hevc@1080 av transcode ($JOB_ID) was canceled mid-flight and its ffmpeg child PID(s) [$BEFORE_PIDS] were confirmed GONE from the av-worker process table (not merely the DB row) -- PASS"
