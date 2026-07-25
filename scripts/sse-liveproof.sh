#!/usr/bin/env bash
# sse-liveproof.sh -- Phase 42 (PUSH-01) operator-run live proof gate.
#
# WHY THIS SCRIPT EXISTS -- Plan 02's httptest.ResponseRecorder unit tests
# prove handleJobEvents' framing/throttle/ownership LOGIC in isolation, but
# httptest.ResponseRecorder is NOT a real socket: it buffers writes in memory
# and can never distinguish "wrote N chunks, flushed each one immediately" from
# "buffered everything and wrote it all at once right before returning". The
# ONE guarantee PUSH-01 actually cares about -- that GET /v1/jobs/{id}/events
# delivers INCREMENTAL `event: progress` frames over a REAL TCP socket, through
# the FULL chi middleware chain (RequestID -> ClientIPFromRemoteAddr -> Logger
# -> Recoverer -> ratelimit.ByIP -> auth.Middleware -> ratelimit.PerClient),
# before any terminal frame -- can only be proven against a live docker-compose
# stack. This closes the STATE.md-carried Phase 42 concern: "Live-verify SSE
# behavior against the real reverse-proxy/ingress topology in Phase 42, not
# just localhost unit tests -- proxy buffering is a named pitfall for
# long-lived streaming responses." Mirrors the 39-04/41-04 live-gate
# discipline (scripts/cancel-liveproof.sh, scripts/av-progress-liveproof.sh):
# authored + static-checked by the executor, RUN by the operator.
#
# WHAT THIS SCRIPT PROVES (PUSH-01):
#   1. GET /v1/jobs/{id}/events responds with Content-Type: text/event-stream.
#   2. A real hevc@1080 re-encode is submitted -- BOTH "codec":"hevc" AND
#      "resolution_height":1080 against an h264/aac source, so
#      avStreamCopyEligible (internal/convert/av.go) is false on either
#      condition alone (belt-and-suspenders) -- forcing the ONLY ramping path
#      (convertTranscodeWithProgress's full re-encode branch), never the
#      AVC-05 stream-copy remux fast path, which emits zero progress ticks.
#      This gives the stream an in-flight job to report real incremental
#      progress on for tens of seconds to minutes (worst measured cell
#      hevc@1080 p95_RTF=4.179133s/source-second, 36-04-SUMMARY.md).
#   3. At least TWO `event: progress` data frames are observed at DIFFERENT
#      wall-clock timestamps BEFORE the stream's one terminal frame -- THE
#      proof that frames are flushed incrementally as they happen, not
#      buffered into a single write at connection close (the one thing
#      httptest structurally cannot exercise).
#   4. At least one `: keepalive` heartbeat comment line is observed on the
#      stream -- proving the heartbeat cadence (SSE_HEARTBEAT_INTERVAL,
#      default 15s, .env.example) really reaches the client over the real
#      socket, not just inside an in-process ResponseRecorder.
#   5. The stream closes within a short, bounded window after its one
#      terminal frame (internal/api/sse.go's emitTerminal: a data body
#      carrying ONLY {"status":...}, deliberately never progress_pct/
#      eta_seconds/a presigned result URL -- D-02).
#   6. A SECOND client's ApiKey opened against the SAME job id gets HTTP 404
#      -- the live confirmation of the cross-client ownership guard
#      (T-39-01/T-42-04 precedent: a cross-client job is reported exactly
#      like a truly-missing one, never a distinct 403).
#
# HEARTBEAT NOTE (read before tuning): SSE_HEARTBEAT_INTERVAL defaults to 15s
# (envDuration fallback, cmd/api/main.go) and docker-compose.yml's `api`
# service does NOT override it, so a live stack always uses 15s regardless of
# host environment variables (compose only passes through vars explicitly
# listed in a service's `environment:` block). Rather than requiring an
# operator to edit docker-compose.yml (out of this script's scope --
# files_modified is scripts/sse-liveproof.sh ONLY), this script sizes its
# forced re-encode source long enough (SSE_LIVEPROOF_SOURCE_DURATION_S,
# default 15s of DECLARED source duration -- comfortably under
# AV_MAX_DURATION_SECONDS=90) that, at the worst measured RTF, real wall-clock
# transcode time is comfortably >60s -- more than four 15s heartbeat ticks --
# so the heartbeat assertion is expected to pass by a wide margin without any
# env override. If an operator has ALREADY lowered SSE_HEARTBEAT_INTERVAL
# (e.g. via a docker-compose.override.yml), the assertion still passes, just
# sooner; SSE_LIVEPROOF_STREAM_MAX_S bounds the total wait either way.
#
# WHAT THIS SCRIPT DOES NOT DO: it does not modify sse.go, routes.go, av.go,
# or any other Go source (it is a pure operator/CI shell harness); it does not
# stand up or tear down the docker-compose stack (operator's job, per the
# checkpoint instructions); it requires no Kubernetes/KEDA/helm -- plain
# docker-compose, like cancel-liveproof.sh/av-progress-liveproof.sh.
#
# OPERATIONAL DISCIPLINE (OrbStack, carried from Phase 39/41 precedent): use
# pre-built, non-`latest`-tagged images for every compose service; NEVER run
# a docker-compose stack and a local k8s stack (kind/k3d/minikube) hot at the
# same time -- four confirmed daemon-wedge incidents on record across
# v1.6/v1.7/v1.8 history (STATE.md).
#
# ===========================================================================
# OPERATOR RUNBOOK
# ===========================================================================
# Prerequisites:
#   - docker, curl, go on PATH.
#   - The live docker-compose stack already up, with migration 0008 (Plan 01)
#     applied and the SSE route (Plan 02, GET /v1/jobs/{id}/events) deployed:
#       docker compose up -d --build
#     (postgres, redis, minio, api, av-worker must all be `running`.)
#   - No local k8s stack (kind/k3d/minikube) running at the same time.
#
# How to run:
#   bash scripts/sse-liveproof.sh
#
# Expected PASS output (tail of the run):
#   === ALL <N> ASSERTIONS PASSED ===
#   PUSH-01: GET /v1/jobs/<job-id>/events streamed Content-Type: text/event-stream,
#   >=2 event: progress frames at distinct wall-clock timestamps before its one
#   terminal frame (status=<done|failed|canceled>), >=1 : keepalive heartbeat,
#   closed <=<Ns> after the terminal frame, and a second client's ApiKey got
#   404 opening the same job's /events -- PASS
#
# Tunable env vars (all optional, sane defaults below):
#   SSE_LIVEPROOF_API_BASE               (default http://127.0.0.1:8090)
#   SSE_LIVEPROOF_DATABASE_URL           (default matches docker-compose.yml's host-exposed postgres)
#   SSE_LIVEPROOF_API_KEY_SALT           (default matches docker-compose.yml's API_KEY_SALT)
#   SSE_LIVEPROOF_SOURCE_DURATION_S      (default 15 -- declared source duration fed to the forced re-encode, must stay < AV_MAX_DURATION_SECONDS=90)
#   SSE_LIVEPROOF_STREAM_MAX_S           (default 180 -- curl --max-time cap on the whole SSE capture, safety bound only)
#   SSE_LIVEPROOF_STREAM_CLOSE_TOLERANCE_S (default 5 -- max seconds allowed between the terminal frame and the stream actually closing)
set -euo pipefail

cd "$(dirname "$0")/.."

# ---------------------------------------------------------------------------
# Config / constants
# ---------------------------------------------------------------------------
API_BASE="${SSE_LIVEPROOF_API_BASE:-http://127.0.0.1:8090}"
DATABASE_URL_HOST="${SSE_LIVEPROOF_DATABASE_URL:-postgres://octo:octo-pass@127.0.0.1:5434/octo_db}"
# MUST match docker-compose.yml's api/av-worker API_KEY_SALT exactly, or the
# minted clients' keys will never resolve against the running api's hash.
API_KEY_SALT="${SSE_LIVEPROOF_API_KEY_SALT:-dev-only-change-me-in-real-deploys}"

# Declared source duration for the forced re-encode fixture -- comfortably
# under docker-compose.yml's AV_MAX_DURATION_SECONDS=90 ceiling, long enough
# at the measured worst-cell hevc@1080 p95_RTF=4.179133s (36-04-SUMMARY.md)
# to yield well over a minute of real wall-clock transcode time -- enough for
# several PROGRESS_MIN_INTERVAL_SECONDS (default 2s, .env.example) ticks and
# at least one SSE_HEARTBEAT_INTERVAL (default 15s) heartbeat before the job
# ever reaches a terminal status.
SOURCE_DURATION_S="${SSE_LIVEPROOF_SOURCE_DURATION_S:-15}"
# Safety cap only -- curl gives up after this many seconds regardless, so a
# genuinely wedged/never-terminating stream fails this gate with a clear
# assertion message instead of hanging the script forever.
STREAM_MAX_S="${SSE_LIVEPROOF_STREAM_MAX_S:-180}"
# Max seconds allowed between the terminal frame's timestamp and curl
# actually observing the connection close.
STREAM_CLOSE_TOLERANCE_S="${SSE_LIVEPROOF_STREAM_CLOSE_TOLERANCE_S:-5}"

PASS_COUNT=0
GATE_OK=""
WORKDIR=""

# ---------------------------------------------------------------------------
# Assertion helpers -- same shape as scripts/cancel-liveproof.sh /
# scripts/av-progress-liveproof.sh. CRITICAL: every helper logs to STDERR
# ONLY, never stdout -- av-progress-liveproof.sh's first live run failed
# precisely because its helpers logged PASS lines to stdout and a
# `VAR=$(helper ...)` command substitution captured those log lines instead
# of the intended value, corrupting downstream state. Every function below
# that is ever used inside `$(...)` must never write anything but the
# intended value to stdout.
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

# assert_le: numeric "a <= b" check.
assert_le() {
	local a="$1" b="$2" label="$3"
	if [ "$a" -gt "$b" ]; then
		echo "FAIL: $label -- $a exceeds $b" >&2
		exit 1
	fi
	PASS_COUNT=$((PASS_COUNT + 1))
	echo "PASS: $label ($a <= $b)" >&2
}

# assert_ge: numeric "a >= b" check.
assert_ge() {
	local a="$1" b="$2" label="$3"
	if [ "$a" -lt "$b" ]; then
		echo "FAIL: $label -- $a is less than required $b" >&2
		exit 1
	fi
	PASS_COUNT=$((PASS_COUNT + 1))
	echo "PASS: $label ($a >= $b)" >&2
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
		echo "PASS -- Phase 42 sse-liveproof (PUSH-01) gate run complete ($PASS_COUNT checks)."
	else
		echo "FAIL -- Phase 42 sse-liveproof (PUSH-01) gate did NOT complete (exit=$exit_code, checks passed=$PASS_COUNT)." >&2
		echo "If the failure was at the frame-timing, heartbeat, close-timing, or cross-client assertion: do not approve the checkpoint." >&2
	fi
	exit "$exit_code"
}
trap teardown EXIT

echo "=== Phase 42 sse-liveproof gate (PUSH-01: real-socket SSE incremental streaming + heartbeat + clean close + cross-client 404) ==="

# ---------------------------------------------------------------------------
# STEP 1: Preflight -- the docker-compose stack must already be up (operator
# precondition, per the checkpoint instructions); fail loudly and name the
# exact missing piece rather than attempting to bring anything up ourselves.
# ---------------------------------------------------------------------------
log "STEP 1: preflight"

command -v docker >/dev/null 2>&1 || { echo "FAIL: docker not found on PATH" >&2; exit 1; }
command -v curl >/dev/null 2>&1 || { echo "FAIL: curl not found on PATH" >&2; exit 1; }
command -v go >/dev/null 2>&1 || { echo "FAIL: go not found on PATH (needed to mint test clients via cmd/manage-clients)" >&2; exit 1; }
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

if ! docker compose exec -T av-worker sh -c 'command -v ffmpeg' >/dev/null 2>&1; then
	echo "FAIL: ffmpeg not found inside the running av-worker container" >&2
	exit 1
fi
echo "PASS: ffmpeg present inside the running av-worker container"

HEALTHZ_CODE=$(curl -s -o /tmp/sse-liveproof-healthz.json -w '%{http_code}' "$API_BASE/healthz" || true)
assert_eq "200" "$HEALTHZ_CODE" "api /healthz reachable at $API_BASE"

WORKDIR=$(mktemp -d)

# ---------------------------------------------------------------------------
# STEP 2: mint TWO throwaway clients + API keys directly against the
# docker-compose Postgres (exposed on host port 5434 by docker-compose.yml)
# -- the primary opens the live SSE stream, the second proves the cross-
# client ownership guard (T-42-11).
# ---------------------------------------------------------------------------
log "STEP 2: mint two throwaway clients + API keys"

# mintClient LABEL -- prints ONLY the raw API key to stdout (log() sends its
# own diagnostics to stderr, so this is safe inside a `$(...)` capture).
mintClient() {
	local label="$1"
	local out
	out=$(DATABASE_URL="$DATABASE_URL_HOST" API_KEY_SALT="$API_KEY_SALT" go run ./cmd/manage-clients create "sse-liveproof-${label}-$(date +%s)-$$")
	printf '%s\n' "$out" | awk -F': ' '/^api key/{print $2}'
}

CLIENT_KEY=$(mintClient primary)
if [ -z "$CLIENT_KEY" ]; then
	echo "FAIL: could not mint the primary client/API key against $DATABASE_URL_HOST" >&2
	exit 1
fi
PASS_COUNT=$((PASS_COUNT + 1))
echo "PASS: minted primary client + API key (redacted, ${#CLIENT_KEY} chars)"

CLIENT_KEY_2=$(mintClient other)
if [ -z "$CLIENT_KEY_2" ]; then
	echo "FAIL: could not mint the second (cross-client) API key against $DATABASE_URL_HOST" >&2
	exit 1
fi
PASS_COUNT=$((PASS_COUNT + 1))
echo "PASS: minted second (cross-client) client + API key (redacted, ${#CLIENT_KEY_2} chars)"

# ---------------------------------------------------------------------------
# Helper functions.
# ---------------------------------------------------------------------------

# synthesizeFixture DURATION_S OUT_FILENAME -- synthesizes a 1080p
# testsrc+sine h264/aac fixture DIRECTLY INSIDE the already-running av-worker
# container (its production ffmpeg build already has lavfi/testsrc/sine
# compiled in -- Dockerfile.av-worker's documented deviation for
# scripts/av-rtf-measure.sh -- so no separate throwaway image/container is
# needed here), then docker-compose-cp's it to the host WORKDIR for a real
# multipart upload. Deliberately h264/aac (never hevc) -- this is the source
# codec avStreamCopyEligible must see as ineligible once a hevc/resize request
# is submitted below (mirrors scripts/av-progress-liveproof.sh exactly).
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

# postAVRampJob FIXTURE_PATH API_KEY -- POSTs a real hevc@1080 re-encode
# request. BOTH "codec":"hevc" AND "resolution_height":1080 are set
# (belt-and-suspenders, mirrors scripts/av-progress-liveproof.sh): a resize is
# by definition a re-encode (avStreamCopyEligible returns false whenever
# ResolutionHeight != 0, internal/convert/av.go) and an explicit codec
# request the h264/aac source does not already carry disqualifies the AVC-05
# stream-copy fast path too -- either condition alone forces the ramp path
# this proof needs (convertTranscodeWithProgress's full re-encode branch, the
# ONLY path that emits progress ticks, D-02), and setting both closes any
# doubt. Echoes ONLY the job id to stdout.
postAVRampJob() {
	local fixture="$1" api_key="$2"
	local out_file="$WORKDIR/post-response.json"
	local status
	status=$(curl -s -o "$out_file" -w '%{http_code}' -X POST "$API_BASE/v1/jobs" \
		-H "Authorization: ApiKey $api_key" \
		-F "target=mp4" \
		-F "file=@${fixture};type=video/x-matroska" \
		-F 'opts={"codec":"hevc","resolution_height":1080}')
	if [ "$status" != "202" ]; then
		echo "FAIL: POST /v1/jobs returned $status, body: $(cat "$out_file")" >&2
		exit 1
	fi
	grep -o '"job_id":"[^"]*"' "$out_file" | head -1 | cut -d'"' -f4
}

# ---------------------------------------------------------------------------
# STEP 3: synthesize the forced-re-encode fixture and submit the job.
# ---------------------------------------------------------------------------
log "STEP 3: synthesize a ${SOURCE_DURATION_S}s h264/aac 1080p fixture and submit the forced hevc/resize re-encode job"

synthesizeFixture "$SOURCE_DURATION_S" "sse-liveproof-source.mkv"

JOB_ID=$(postAVRampJob "$WORKDIR/sse-liveproof-source.mkv" "$CLIENT_KEY")
assert_nonempty "$JOB_ID" "job submitted (forced hevc@1080 re-encode, ${SOURCE_DURATION_S}s declared source duration)"

# ---------------------------------------------------------------------------
# STEP 4: open GET /v1/jobs/{id}/events and capture the live stream with
# wall-clock (epoch-seconds) timestamps on every received line.
# ---------------------------------------------------------------------------
log "STEP 4: open GET /v1/jobs/{id}/events and capture the live stream"

CAPTURE_FILE="$WORKDIR/sse-capture.txt"
HEADERS_FILE="$WORKDIR/sse-headers.txt"

STREAM_START_EPOCH=$(date +%s)
# NOTE: temporarily disable errexit around this pipeline. curl's --max-time
# is a SAFETY CAP, not the expected exit path -- if the real ffmpeg re-encode
# somehow outruns STREAM_MAX_S, curl exits non-zero (timeout) and, under
# `set -o pipefail`, that would abort the whole script via `set -e` before
# the assertions below get a chance to report a clear FAIL. Capturing the
# pipeline's exit status explicitly lets the frame/terminal-shape assertions
# below produce the actual diagnostic instead of a bare `set -e` abort.
set +e
curl -N -s --max-time "$STREAM_MAX_S" -D "$HEADERS_FILE" \
	-H "Authorization: ApiKey $CLIENT_KEY" \
	"$API_BASE/v1/jobs/$JOB_ID/events" \
	| while IFS= read -r line || [ -n "$line" ]; do
		printf '%s %s\n' "$(date +%s)" "$line"
	done >"$CAPTURE_FILE"
PIPE_STATUS=$?
set -e
STREAM_END_EPOCH=$(date +%s)

if [ "$PIPE_STATUS" -ne 0 ]; then
	echo "NOTE: curl/capture pipeline exited with status $PIPE_STATUS (e.g. 28 = --max-time reached before the server closed the stream). This is NOT automatically a FAIL -- the assertions below check whether a real terminal frame was actually captured within the window." >&2
fi

STREAM_WALL_SECONDS=$((STREAM_END_EPOCH - STREAM_START_EPOCH))
echo "stream ran for ${STREAM_WALL_SECONDS}s wall-clock (STREAM_MAX_S cap=${STREAM_MAX_S}s)" >&2

# ---------------------------------------------------------------------------
# STEP 5: assert the response's Content-Type is text/event-stream.
# ---------------------------------------------------------------------------
log "STEP 5: assert Content-Type: text/event-stream and HTTP 200"

STATUS_LINE=$(head -1 "$HEADERS_FILE" | tr -d '\r')
HTTP_CODE=$(printf '%s\n' "$STATUS_LINE" | awk '{print $2}')
assert_eq "200" "$HTTP_CODE" "GET /v1/jobs/{id}/events HTTP status"

CONTENT_TYPE_LINE=$(grep -i '^content-type:' "$HEADERS_FILE" | tail -1 | tr -d '\r')
assert_nonempty "$CONTENT_TYPE_LINE" "Content-Type header present on the /events response"
case "$CONTENT_TYPE_LINE" in
*text/event-stream*) ;;
*)
	echo "FAIL: Content-Type is '$CONTENT_TYPE_LINE', expected text/event-stream" >&2
	exit 1
	;;
esac
PASS_COUNT=$((PASS_COUNT + 1))
echo "PASS: Content-Type is text/event-stream ($CONTENT_TYPE_LINE)" >&2

# ---------------------------------------------------------------------------
# STEP 6: parse the captured frames and assert incremental delivery.
#
# Every `data: ` line in the capture carries its receive-time epoch as the
# first field (prepended by the capture loop above). Frames are written to
# the same response body in strict emission order (internal/api/sse.go never
# reorders/buffers across writes), so file order == real temporal order.
# The handler's emitTerminal writes ONLY {"status":...} (no progress_pct/
# eta_seconds key at all) -- every OTHER `data: ` line necessarily carries
# progress_pct (possibly JSON null on the very first connect-time snapshot,
# but the KEY is always present, internal/api/sse.go's sseProgressPayload).
# ---------------------------------------------------------------------------
log "STEP 6: assert >=2 event: progress frames at distinct timestamps before the one terminal frame"

DATA_LINES_FILE="$WORKDIR/data-lines.txt"
grep -E '^[0-9]+ data: ' "$CAPTURE_FILE" | sed -E 's/^([0-9]+) data: /\1 /' >"$DATA_LINES_FILE" || true

TOTAL_DATA_LINES=$(wc -l <"$DATA_LINES_FILE" | tr -d ' ')
assert_ge "$TOTAL_DATA_LINES" 3 "total event: progress data frames captured (initial connect emit + >=1 real incremental tick + terminal frame)"

LAST_LINE=$(tail -1 "$DATA_LINES_FILE")
TERMINAL_TS=$(printf '%s' "$LAST_LINE" | awk '{print $1}')
TERMINAL_JSON=$(printf '%s' "$LAST_LINE" | cut -d' ' -f2-)

if printf '%s' "$TERMINAL_JSON" | grep -q 'progress_pct'; then
	echo "FAIL: the LAST captured event: progress frame still carries progress_pct -- expected the terminal shape {\"status\":...} only (internal/api/sse.go's emitTerminal, D-02). Body: $TERMINAL_JSON" >&2
	exit 1
fi
PASS_COUNT=$((PASS_COUNT + 1))
echo "PASS: final captured frame matches the terminal shape (status-only, no progress_pct/eta_seconds) -- body: $TERMINAL_JSON" >&2

TERMINAL_STATUS=$(printf '%s' "$TERMINAL_JSON" | grep -o '"status":"[^"]*"' | cut -d'"' -f4)
case "$TERMINAL_STATUS" in
done | failed | canceled) ;;
*)
	echo "FAIL: terminal frame status '$TERMINAL_STATUS' is not one of done/failed/canceled" >&2
	exit 1
	;;
esac
PASS_COUNT=$((PASS_COUNT + 1))
echo "PASS: terminal frame reports a legal terminal status ($TERMINAL_STATUS)" >&2

PRE_TERMINAL_FILE="$WORKDIR/pre-terminal-lines.txt"
# All lines except the last (the terminal frame). `head -n -1` is a GNU
# coreutils-ism that BSD/macOS `head` rejects ("illegal line count -- -1");
# `sed '$d'` (delete last line) is portable on both.
sed '$d' "$DATA_LINES_FILE" >"$PRE_TERMINAL_FILE"
PRE_TERMINAL_COUNT=$(wc -l <"$PRE_TERMINAL_FILE" | tr -d ' ')
assert_ge "$PRE_TERMINAL_COUNT" 2 "event: progress frames observed BEFORE the terminal frame (initial connect emit + >=1 real incremental tick)"

DISTINCT_TS_COUNT=$(awk '{print $1}' "$PRE_TERMINAL_FILE" | sort -u | wc -l | tr -d ' ')
assert_ge "$DISTINCT_TS_COUNT" 2 "distinct wall-clock (epoch-second) timestamps among pre-terminal event: progress frames -- the actual proof of incremental flush, not one buffered write at close"

FIRST_PROGRESS_TS=$(head -1 "$PRE_TERMINAL_FILE" | awk '{print $1}')
echo "observed inter-frame timestamps: first pre-terminal frame at epoch=$FIRST_PROGRESS_TS, terminal frame at epoch=$TERMINAL_TS ($((TERMINAL_TS - FIRST_PROGRESS_TS))s span, $PRE_TERMINAL_COUNT pre-terminal frames across $DISTINCT_TS_COUNT distinct timestamps)" >&2

# ---------------------------------------------------------------------------
# STEP 7: assert at least one ': keepalive' heartbeat comment was observed.
# ---------------------------------------------------------------------------
log "STEP 7: assert >=1 ': keepalive' heartbeat comment observed on the stream"

HEARTBEAT_FILE="$WORKDIR/heartbeat-lines.txt"
grep -E '^[0-9]+ : keepalive' "$CAPTURE_FILE" >"$HEARTBEAT_FILE" || true
HEARTBEAT_COUNT=$(wc -l <"$HEARTBEAT_FILE" | tr -d ' ')
assert_ge "$HEARTBEAT_COUNT" 1 "': keepalive' heartbeat comment observed on the stream (SSE_HEARTBEAT_INTERVAL default 15s)"

# ---------------------------------------------------------------------------
# STEP 8: assert the stream closed within a short bound after the terminal
# frame (D-02: the handler returns -- and thus closes the response -- right
# after writing the one terminal frame).
# ---------------------------------------------------------------------------
log "STEP 8: assert the stream closed shortly after the terminal frame"

CLOSE_DELTA=$((STREAM_END_EPOCH - TERMINAL_TS))
if [ "$CLOSE_DELTA" -lt 0 ]; then
	CLOSE_DELTA=0
fi
assert_le "$CLOSE_DELTA" "$STREAM_CLOSE_TOLERANCE_S" "stream closed within tolerance after the terminal frame ($CLOSE_DELTA <= ${STREAM_CLOSE_TOLERANCE_S}s)"

# ---------------------------------------------------------------------------
# STEP 9: cross-client ownership guard -- a SECOND client's ApiKey opening
# the SAME job's /events must get 404 (T-42-11), never a distinct 403 and
# never the stream body.
# ---------------------------------------------------------------------------
log "STEP 9: assert a second client's ApiKey gets 404 opening the same job's /events"

CROSS_CLIENT_STATUS=$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 \
	-H "Authorization: ApiKey $CLIENT_KEY_2" \
	"$API_BASE/v1/jobs/$JOB_ID/events")
assert_eq "404" "$CROSS_CLIENT_STATUS" "second client's ApiKey gets 404 opening job $JOB_ID's /events (T-42-11 cross-client ownership guard)"

# =============================================================================
# ALL-PASSED summary -- set only after every assertion above has passed.
# Teardown runs unconditionally via the EXIT trap.
# =============================================================================
GATE_OK="1"
echo ""
echo "=== ALL $PASS_COUNT ASSERTIONS PASSED ==="
echo "PUSH-01: GET /v1/jobs/$JOB_ID/events streamed Content-Type: text/event-stream, $PRE_TERMINAL_COUNT event: progress frames across $DISTINCT_TS_COUNT distinct wall-clock timestamps BEFORE its one terminal frame (status=$TERMINAL_STATUS), $HEARTBEAT_COUNT ': keepalive' heartbeat(s), closed ${CLOSE_DELTA}s after the terminal frame (<= ${STREAM_CLOSE_TOLERANCE_S}s tolerance), and a second client's ApiKey got 404 opening the same job's /events -- PASS"
