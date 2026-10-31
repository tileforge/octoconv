#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

API_BASE="${STAGE_LIVEPROOF_API_BASE:-http://127.0.0.1:8090}"
DB_URL="${STAGE_LIVEPROOF_DATABASE_URL:-postgres://octo:octo-pass@127.0.0.1:5434/octo_db}"
SALT="${STAGE_LIVEPROOF_API_KEY_SALT:-dev-only-change-me-in-real-deploys}"
SECRET="${STAGE_LIVEPROOF_SECRET:-dev-only-change-me-in-real-deploys}"
PORT="${STAGE_LIVEPROOF_HOOK_PORT:-18777}"
OUT="${STAGE_LIVEPROOF_OUT:-$(mktemp -d)}"
IMAGE_SOURCE="${STAGE_LIVEPROOF_IMAGE_SOURCE:-}"
EXPECT_API="${STAGE_LIVEPROOF_EXPECT_API_IMAGE:-}"
EXPECT_CAD="${STAGE_LIVEPROOF_EXPECT_CAD_IMAGE:-}"
EXPECT_WORKER="${STAGE_LIVEPROOF_EXPECT_WORKER_IMAGE:-}"
EXPECT_WEBHOOK="${STAGE_LIVEPROOF_EXPECT_WEBHOOK_IMAGE:-}"
IMAGE_PATHS=(cmd internal Dockerfile.api Dockerfile.cad-worker Dockerfile.worker Dockerfile.webhook-worker go.mod go.sum docker-compose.yml docker-compose.e2e.yml)
CALLBACK="http://host.docker.internal:$PORT/hook"
JOB_LIMIT_S=900
export GOTOOLCHAIN="${GOTOOLCHAIN:-go1.27.3}"

mkdir -p "$OUT"
TRANSCRIPT="$OUT/transcript.log"
HOOKS="$OUT/hooks.jsonl"
: >"$TRANSCRIPT"
: >"$HOOKS"
CHECKS=0
FAILS=0
RECV_PID=""
SSE_PID=""
CLIENT_KEY=""
JOB_ID=""
JOB_STATUS=""
ALL_JOBS=()
CAD_JOBS=()
CAD_COUNTS=()

say() { printf '%s\n' "$*" | tee -a "$TRANSCRIPT"; }
pass() {
	CHECKS=$((CHECKS + 1))
	say "PASS: $*"
}
fail() {
	CHECKS=$((CHECKS + 1))
	FAILS=$((FAILS + 1))
	say "FAIL: $*"
}
die() {
	fail "$*"
	say "SUMMARY FAIL failed=$FAILS"
	exit 1
}
check() {
	local label=$1
	shift
	if "$@"; then
		pass "$label"
	else
		fail "$label"
	fi
}
is_true() { [ "$1" = true ]; }

cleanup() {
	if [ -n "$SSE_PID" ]; then
		kill "$SSE_PID" 2>/dev/null || true
	fi
	if [ -n "$RECV_PID" ]; then
		kill "$RECV_PID" 2>/dev/null || true
		wait "$RECV_PID" 2>/dev/null || true
	fi
}
trap cleanup EXIT

say "HDR,date_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ),head=$(git rev-parse HEAD),out=$OUT"

K8S=$(orbctl config get k8s.enable 2>/dev/null || true)
[ "$K8S" = false ] || die "preflight: OrbStack k8s.enable is '${K8S:-unreadable}', want false (compose and k8s never run together)"
pass "preflight: OrbStack k8s.enable=false"

[ -n "$IMAGE_SOURCE" ] || die "preflight: STAGE_LIVEPROOF_IMAGE_SOURCE is required"
[ -n "$EXPECT_API" ] || die "preflight: STAGE_LIVEPROOF_EXPECT_API_IMAGE is required"
[ -n "$EXPECT_CAD" ] || die "preflight: STAGE_LIVEPROOF_EXPECT_CAD_IMAGE is required"
[ -n "$EXPECT_WORKER" ] || die "preflight: STAGE_LIVEPROOF_EXPECT_WORKER_IMAGE is required"
[ -n "$EXPECT_WEBHOOK" ] || die "preflight: STAGE_LIVEPROOF_EXPECT_WEBHOOK_IMAGE is required"
git cat-file -e "$IMAGE_SOURCE^{commit}" 2>/dev/null || die "preflight: image source $IMAGE_SOURCE is not a commit of this checkout"
IMAGE_DIFF=$(git diff --name-only "$IMAGE_SOURCE" HEAD -- "${IMAGE_PATHS[@]}")
[ -z "$IMAGE_DIFF" ] || die "preflight: image inputs differ between $IMAGE_SOURCE and HEAD: $(printf '%s' "$IMAGE_DIFF" | tr '\n' ' ')"
IMAGE_DIRT=$(git status --porcelain --untracked-files=all -- "${IMAGE_PATHS[@]}")
[ -z "$IMAGE_DIRT" ] || die "preflight: image inputs have uncommitted changes: $(printf '%s' "$IMAGE_DIRT" | tr '\n' ' ')"
pass "preflight: image inputs at HEAD equal the build commit $IMAGE_SOURCE and the tree is clean in them"

printf 'image_source=%s\n' "$IMAGE_SOURCE" >>"$OUT/images.txt"
for pair in "octoconv-api=$EXPECT_API" "octoconv-cad-worker=$EXPECT_CAD" "octoconv-worker=$EXPECT_WORKER" "octoconv-webhook-worker-1=$EXPECT_WEBHOOK"; do
	name=${pair%%=*}
	want=${pair#*=}
	got=$(docker inspect -f '{{.Image}}' "$name" 2>/dev/null || true)
	[ "$got" = "$want" ] || die "preflight: container $name runs image ${got:-none}, want $want"
	printf 'running,container=%s,image=%s\n' "$name" "$got" >>"$OUT/images.txt"
	pass "preflight: container $name runs the expected image $want"
done

HEALTH=$(curl -s -o /dev/null -w '%{http_code}' "$API_BASE/healthz" || true)
[ "$HEALTH" = 200 ] || die "preflight: api /healthz at $API_BASE answered ${HEALTH:-nothing}, want 200"
pass "preflight: api /healthz 200"

CAD_LOG=$(docker logs octoconv-cad-worker 2>&1 || true)
case "$CAD_LOG" in
*"progress webhooks enabled"*) pass "preflight: cad-worker logged 'progress webhooks enabled'" ;;
*) die "preflight: cad-worker never logged 'progress webhooks enabled'" ;;
esac

if nc -z 127.0.0.1 "$PORT" 2>/dev/null; then
	die "preflight: port $PORT is already in use"
fi

CLIENT_OUT=$(DATABASE_URL="$DB_URL" API_KEY_SALT="$SALT" go run ./cmd/manage-clients create "stage-liveproof-$(date +%s)-$$" 2>/dev/null) || die "preflight: manage-clients create failed against the host database"
CLIENT_KEY=$(printf '%s\n' "$CLIENT_OUT" | awk -F': ' '/^api key/ { print $2 }')
CLIENT_OUT=""
[ -n "$CLIENT_KEY" ] || die "preflight: manage-clients printed no api key"
pass "preflight: minted a client (key redacted, ${#CLIENT_KEY} chars)"

go build -o "$OUT/stage-hook-recv" ./scripts/cmd/stage-hook-recv || die "preflight: cannot build stage-hook-recv"
STAGE_HOOK_SECRET="$SECRET" "$OUT/stage-hook-recv" -addr "0.0.0.0:$PORT" -out "$HOOKS" >>"$OUT/recv.log" 2>&1 &
RECV_PID=$!
for _ in $(seq 1 50); do
	nc -z 127.0.0.1 "$PORT" 2>/dev/null && break
	sleep 0.2
done
nc -z 127.0.0.1 "$PORT" 2>/dev/null || die "preflight: stage-hook-recv is not listening on $PORT"
pass "preflight: stage-hook-recv listening on 0.0.0.0:$PORT"

api() { curl -sS -H "Authorization: ApiKey $CLIENT_KEY" "$@"; }

create_job() {
	local label=$1 file=$2 upname=$3 op=$4 target=$5 code
	local args=(-F "file=@$file;filename=$upname" -F "target=$target" -F "callback_url=$CALLBACK" -F "progress_webhook=true")
	if [ -n "$op" ]; then
		args+=(-F "operation=$op")
	fi
	JOB_ID=""
	code=$(api -o "$OUT/$label.create.json" -w '%{http_code}' -X POST "$API_BASE/v1/jobs" "${args[@]}") || code=000
	if [ "$code" = 202 ]; then
		JOB_ID=$(jq -r '.job_id // empty' "$OUT/$label.create.json" 2>/dev/null || true)
	fi
	if [ -n "$JOB_ID" ]; then
		pass "$label: POST /v1/jobs 202 job_id=$JOB_ID"
		ALL_JOBS+=("$label=$JOB_ID")
	else
		fail "$label: POST /v1/jobs answered $code without a job_id: $(head -c 300 "$OUT/$label.create.json" 2>/dev/null)"
	fi
}

follow_job() {
	local label=$1 id=$2 t0 code
	SSE_PID=""
	curl -N -sS --max-time "$JOB_LIMIT_S" -H "Authorization: ApiKey $CLIENT_KEY" "$API_BASE/v1/jobs/$id/events" >"$OUT/$label.sse" 2>"$OUT/$label.sse.err" &
	SSE_PID=$!
	: >"$OUT/$label.get.jsonl"
	JOB_STATUS=""
	t0=$(date +%s)
	while :; do
		code=$(api -o "$OUT/get.tmp" -w '%{http_code}' "$API_BASE/v1/jobs/$id") || code=000
		if [ "$code" = 200 ]; then
			jq -c . "$OUT/get.tmp" >>"$OUT/$label.get.jsonl"
			JOB_STATUS=$(jq -r .status "$OUT/get.tmp")
			case "$JOB_STATUS" in done | failed | canceled) break ;; esac
		fi
		[ $(($(date +%s) - t0)) -lt "$JOB_LIMIT_S" ] || break
		sleep 0.25
	done
	rm -f "$OUT/get.tmp"
	for _ in $(seq 1 120); do
		kill -0 "$SSE_PID" 2>/dev/null || break
		sleep 0.25
	done
	if kill -0 "$SSE_PID" 2>/dev/null; then
		fail "$label: the SSE stream stayed open 30 s after the job reached $JOB_STATUS"
		kill "$SSE_PID" 2>/dev/null || true
	fi
	wait "$SSE_PID" 2>/dev/null || true
	SSE_PID=""
	sed -n 's/^data: //p' "$OUT/$label.sse" | jq -s . >"$OUT/$label.frames.json"
}

progress_hooks() {
	jq -c --arg id "$1" 'select((.body | type) == "object" and .body.event == "progress" and .body.job_id == $id)' "$HOOKS" | jq -s .
}

run_cad() {
	local label=$1 file=$2 upname=$3 op=$4 target=$5 route=$6 n last routej frames hooks got
	routej=$(printf '%s' "$route" | jq -Rc 'split(",")')
	n=$(printf '%s' "$routej" | jq length)
	last=$(printf '%s' "$routej" | jq -r '.[-1]')
	say "--- $label: $op $upname -> $target, route $route ($n stages)"
	create_job "$label" "$file" "$upname" "$op" "$target"
	[ -n "$JOB_ID" ] || return 0
	local id=$JOB_ID
	follow_job "$label" "$id"

	check "$label: job $id reached done (got $JOB_STATUS)" [ "$JOB_STATUS" = "done" ]
	got=$(jq -s 'length > 0 and all(.[]; has("stage") and has("stage_index") and has("stage_count") and .progress_pct == null and .eta_seconds == null)' "$OUT/$label.get.jsonl")
	check "$label: all $(wc -l <"$OUT/$label.get.jsonl" | tr -d ' ') GET bodies carry stage, stage_index, stage_count with progress_pct and eta_seconds null" is_true "$got"
	got=$(jq -s --arg last "$last" --argjson n "$n" '.[-1] | .stage == $last and .stage_index == $n and .stage_count == $n' "$OUT/$label.get.jsonl")
	check "$label: final GET has stage=$last stage_index=stage_count=$n ($(jq -sc '.[-1] | {status, stage, stage_index, stage_count}' "$OUT/$label.get.jsonl"))" is_true "$got"

	frames="$OUT/$label.frames.json"
	got=$(jq -c '. as $f | reduce range(0; length) as $i ({prev: null, out: []}; ($f[$i] | [.stage, .stage_index]) as $p | (if $p[1] != null and $p != .prev then .out += [$f[$i]] else . end) | .prev = $p) | .out' "$frames")
	printf '%s\n' "$got" >"$OUT/$label.stage-frames.json"
	check "$label: SSE stage-change frames have stage_index 1..$n ($(printf '%s' "$got" | jq -c '[.[].stage_index]'))" is_true "$(printf '%s' "$got" | jq --argjson n "$n" '[.[].stage_index] == [range(1; $n + 1)]')"
	check "$label: SSE stage-change frames name the route in order ($(printf '%s' "$got" | jq -c '[.[].stage]'))" is_true "$(printf '%s' "$got" | jq --argjson r "$routej" '[.[].stage] == $r')"
	check "$label: SSE stage-change frames carry stage_count=$n" is_true "$(printf '%s' "$got" | jq --argjson n "$n" 'all(.[]; .stage_count == $n)')"
	got=$(jq '(length >= 2) and (.[-1] == {status: "done"}) and (.[-2].status == "done") and all(.[0:-2][]; .status == "queued" or .status == "active")' "$frames")
	check "$label: SSE ends with the done snapshot and {\"status\":\"done\"}, every earlier frame is queued or active ($(jq -c '[.[].status]' "$frames"))" is_true "$got"
	check "$label: no SSE frame carries a numeric progress_pct" is_true "$(jq 'all(.[]; (.progress_pct | type) != "number")' "$frames")"

	for _ in $(seq 1 40); do
		[ "$(progress_hooks "$id" | jq length)" -ge "$n" ] && break
		sleep 0.25
	done
	hooks=$(progress_hooks "$id")
	printf '%s\n' "$hooks" >"$OUT/$label.progress-hooks.json"
	check "$label: receiver got exactly $n event=progress deliveries (got $(printf '%s' "$hooks" | jq length))" is_true "$(printf '%s' "$hooks" | jq --argjson n "$n" 'length == $n')"
	check "$label: every progress delivery has sig_ok true" is_true "$(printf '%s' "$hooks" | jq 'length > 0 and all(.[]; .sig_ok == true)')"
	check "$label: progress deliveries arrived with stage_index 1..$n ($(printf '%s' "$hooks" | jq -c '[.[].body.stage_index]'))" is_true "$(printf '%s' "$hooks" | jq --argjson n "$n" '[.[].body.stage_index] == [range(1; $n + 1)]')"
	check "$label: progress deliveries name the route in order ($(printf '%s' "$hooks" | jq -c '[.[].body.stage]'))" is_true "$(printf '%s' "$hooks" | jq --argjson r "$routej" '[.[].body.stage] == $r')"
	check "$label: progress deliveries carry stage_count=$n and null progress_pct and eta_seconds" is_true "$(printf '%s' "$hooks" | jq --argjson n "$n" 'length > 0 and all(.[]; .body.stage_count == $n and (.body | has("progress_pct") and has("eta_seconds")) and .body.progress_pct == null and .body.eta_seconds == null)')"
	CAD_JOBS+=("$label=$id")
	CAD_COUNTS+=("$n")
}

run_cad dxf_png internal/convert/testdata/cad/dxf/wide.dxf wide.dxf render png guard,dxf,ink,rsvg,judge
run_cad step_png internal/convert/testdata/cad/synthetic/asymmetric_l_bracket.step part.step render png guard,mesh,render,judge
run_cad dxf_svg internal/convert/testdata/cad/dxf/wide.dxf wide.dxf convert svg guard,dxf,ink
run_cad step_stl internal/convert/testdata/cad/synthetic/asymmetric_l_bracket.step part.step convert stl guard,mesh,judge

say "--- control: image sample.png -> webp"
create_job control internal/e2e/testdata/sample.png sample.png "" webp
CONTROL_ID=$JOB_ID
if [ -n "$CONTROL_ID" ]; then
	follow_job control "$CONTROL_ID"
	check "control: job $CONTROL_ID reached done (got $JOB_STATUS)" [ "$JOB_STATUS" = "done" ]
	check "control: no GET body has a stage, stage_index or stage_count key" is_true "$(jq -s 'length > 0 and all(.[]; (has("stage") or has("stage_index") or has("stage_count")) | not)' "$OUT/control.get.jsonl")"
	check "control: no SSE frame has a stage, stage_index or stage_count key" is_true "$(jq 'length > 0 and all(.[]; (has("stage") or has("stage_index") or has("stage_count")) | not)' "$OUT/control.frames.json")"
fi

say "--- drain: terminal webhooks of every gate job"
for _ in $(seq 1 60); do
	missing=0
	for entry in ${ALL_JOBS[@]+"${ALL_JOBS[@]}"}; do
		id=${entry#*=}
		c=$(jq -c --arg id "$id" 'select((.body | type) == "object" and .body.event == "completed" and .body.job_id == $id)' "$HOOKS" | wc -l | tr -d ' ')
		[ "$c" -ge 1 ] || missing=$((missing + 1))
	done
	[ "$missing" -eq 0 ] && break
	sleep 1
done
sleep 3
for entry in ${ALL_JOBS[@]+"${ALL_JOBS[@]}"}; do
	label=${entry%%=*}
	id=${entry#*=}
	got=$(jq -c --arg id "$id" 'select((.body | type) == "object" and .body.event == "completed" and .body.job_id == $id)' "$HOOKS" | jq -s .)
	check "drain: $label job $id has exactly one event=completed delivery with sig_ok true (got $(printf '%s' "$got" | jq -c '[.[].sig_ok]'))" is_true "$(printf '%s' "$got" | jq 'length == 1 and .[0].sig_ok == true')"
done

i=0
for entry in ${CAD_JOBS[@]+"${CAD_JOBS[@]}"}; do
	label=${entry%%=*}
	id=${entry#*=}
	n=${CAD_COUNTS[$i]}
	i=$((i + 1))
	check "after drain: $label still has exactly $n event=progress deliveries" is_true "$(progress_hooks "$id" | jq --argjson n "$n" 'length == $n')"
done
if [ -n "$CONTROL_ID" ]; then
	check "after drain: control job $CONTROL_ID got no event=progress delivery" is_true "$(progress_hooks "$CONTROL_ID" | jq 'length == 0')"
fi

check "all five gate jobs were created (got ${#ALL_JOBS[@]})" [ "${#ALL_JOBS[@]}" -eq 5 ]

if [ "$FAILS" -eq 0 ]; then
	say "SUMMARY ALL PASS checks=$CHECKS"
	exit 0
fi
say "SUMMARY FAIL failed=$FAILS"
exit 1
