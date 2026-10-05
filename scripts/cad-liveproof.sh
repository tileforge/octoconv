#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT=$(pwd)

API_BASE="${CAD_LIVEPROOF_API_BASE:-http://127.0.0.1:8090}"
DB_URL="${CAD_LIVEPROOF_DATABASE_URL:-postgres://octo:octo-pass@127.0.0.1:5434/octo_db}"
SALT="${CAD_LIVEPROOF_API_KEY_SALT:-dev-only-change-me-in-real-deploys}"
EVID=".planning/phases/73-cad-class-convert-slice-confined/evidence/73-06-liveproof.txt"
CNAME=octoconv-cad-worker
SERVICES="postgres redis minio api cad-worker"
CORPUS=internal/convert/testdata/cad
FETCHED="${OCTOCONV_CAD_CACHE:-$HOME/.cache/octoconv/cad-corpus}/fetched"
export GOTOOLCHAIN="${GOTOOLCHAIN:-go1.26.4}"

W=""
STARTED_BY_ME=""
FINISHED=""
N_FORMATS=0 N_INSPECT=0 N_CONVERT=0 N_REJECT=0 N_BOMB=0 N_CANCEL=0 N_KILL=0

log() { echo "$*" >&2; }
ev() { echo "$*" | tee -a "$EVID"; }

fail() {
	log "FAIL: $*"
	ev "SUMMARY,result=FAIL,reason=$*"
	exit 1
}

now() { python3 -c 'import time; print("%.3f" % time.time())'; }
elapsed() { awk -v a="$1" -v b="$2" 'BEGIN { printf "%.1f", b - a }'; }
within() { awk -v v="$1" -v m="$2" 'BEGIN { exit !(v <= m) }'; }

cleanup() {
	local rc=$?
	if [ "$rc" -ne 0 ] && [ -z "$FINISHED" ]; then
		log "--- cad-worker log tail ---"
		docker compose logs --no-color --tail 60 cad-worker >&2 2>/dev/null || true
		log "--- api log tail ---"
		docker compose logs --no-color --tail 30 api >&2 2>/dev/null || true
	fi
	if [ -n "$STARTED_BY_ME" ]; then
		log "stopping the services this run started:$STARTED_BY_ME"
		docker compose stop -t 15 $STARTED_BY_ME >&2 || true
	fi
	[ -n "$W" ] && rm -rf "$W"
	exit "$rc"
}
trap cleanup EXIT

[ "$(git rev-parse --git-dir)" = ".git" ] || fail "refusing to run outside the main checkout (git dir is $(git rev-parse --git-dir))"
for tool in docker curl jq python3 go shasum; do
	command -v "$tool" >/dev/null 2>&1 || fail "$tool not found on PATH"
done
PROJECT=$(docker compose config --format json | jq -r .name)
[ "$PROJECT" = "octoconv" ] || fail "compose project is '$PROJECT', want octoconv"
if command -v kubectl >/dev/null 2>&1; then
	PODS=$(kubectl get pods -A 2>/dev/null | grep -c octoconv || true)
	[ "$PODS" = "0" ] || fail "$PODS octoconv pods are running in k8s; compose and k8s must not run hot together"
fi
[ -z "${CAD_SANDBOX_DISABLED:-}" ] || fail "CAD_SANDBOX_DISABLED is set in the environment; the proof needs the confined worker"
DISK_GIB=$(df -k / | awk 'NR==2 { printf "%d", $4 / 1048576 }')
[ "$DISK_GIB" -ge 6 ] || fail "free disk is ${DISK_GIB} GiB, below the 6 GiB floor"
for f in synthetic/units_inch.step public/nist_ctc_05_asme1_ap242-e1.stp public/nist-d2mi-827-9999-906.stp public/LinearSlide-MGN15x-Block.step; do
	[ -f "$CORPUS/$f" ] || fail "missing $CORPUS/$f"
done
[ -f "$FETCHED/freecad-festo-valve-5-2.igs" ] || fail "missing $FETCHED/freecad-festo-valve-5-2.igs"

mkdir -p "$(dirname "$EVID")"
: > "$EVID"
W=$(mktemp -d)
chmod 755 "$W"

RUNNING=$(docker compose ps --status running --services 2>/dev/null || true)
for s in $SERVICES; do
	echo "$RUNNING" | grep -qx "$s" || STARTED_BY_ME="$STARTED_BY_ME $s"
done
if echo "$RUNNING" | grep -qx webhook-worker; then
	fail "webhook-worker (the reconciler host) is running; WORKER_KILL needs it absent"
fi
log "starting the stack (not running before:${STARTED_BY_ME:- none})"
docker compose up -d --build postgres redis minio createbucket api cad-worker >&2

for _ in $(seq 1 60); do
	curl -fsS -o /dev/null "$API_BASE/healthz" 2>/dev/null && break
	sleep 2
done
curl -fsS -o /dev/null "$API_BASE/healthz" || fail "api /healthz not reachable at $API_BASE"
for _ in $(seq 1 60); do
	docker compose logs --no-color cad-worker 2>/dev/null | grep -q "cad sandbox proven" && break
	sleep 2
done
docker compose logs --no-color cad-worker 2>/dev/null | grep -q "cad sandbox proven" || fail "cad-worker never logged 'cad sandbox proven'"

CAD_IMAGE=$(docker inspect -f '{{.Image}}' "$CNAME")
API_CID=$(docker compose ps -q api)
ENGINE_TIMEOUT_RAW=$(docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$CNAME" | sed -n 's/^CAD_ENGINE_TIMEOUT=//p')
ENGINE_TIMEOUT=${ENGINE_TIMEOUT_RAW%s}
SANDBOX_ENV=$(docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$CNAME" | sed -n 's/^CAD_SANDBOX_DISABLED=//p')
[ -z "$SANDBOX_ENV" ] || fail "the cad-worker container has CAD_SANDBOX_DISABLED=$SANDBOX_ENV"
case "$ENGINE_TIMEOUT" in '' | *[!0-9]*) fail "cannot read CAD_ENGINE_TIMEOUT from the container (got '$ENGINE_TIMEOUT_RAW')" ;; esac

ev "HDR,date_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
ev "HDR,commit=$(git rev-parse HEAD)"
ev "HDR,tracked_dirt=$(git status --short --untracked-files=no -- . ":(exclude)$EVID" | wc -l | tr -d ' ')"
ev "HDR,api_image_id=$(docker inspect -f '{{.Image}}' "$API_CID")"
ev "HDR,cad_worker_image_id=$CAD_IMAGE"
ev "HDR,cad_engine_timeout_s=$ENGINE_TIMEOUT"
ev "HDR,sandbox_disabled=false"
ev "HDR,started_by_run=$(echo "${STARTED_BY_ME:-none}" | tr -s ' ' | sed 's/^ //' | tr ' ' ';')"
ev "HDR,server_arch=$(docker version --format '{{.Server.Arch}}')"
ev "HDR,disk_free_gib=$DISK_GIB"

CLIENT_OUT=$(DATABASE_URL="$DB_URL" API_KEY_SALT="$SALT" go run ./cmd/manage-clients create "cad-liveproof-$(date +%s)")
KEY=$(printf '%s\n' "$CLIENT_OUT" | awk -F': ' '/^api key/ { print $2 }')
[ -n "$KEY" ] || fail "could not mint a client against $DB_URL"

http() {
	local out=$1 code tries=0
	shift
	while :; do
		code=$(curl -sS -o "$out" -w '%{http_code}' -H "Authorization: ApiKey $KEY" "$@")
		if [ "$code" != 429 ] || [ "$tries" -ge 4 ]; then
			echo "$code"
			return 0
		fi
		tries=$((tries + 1))
		sleep 15
	done
}

create_job() {
	HTTP=$(http "$W/create.json" -X POST "$API_BASE/v1/jobs" -F "target=$3" -F "file=@$1;filename=$2")
	JOB_ID=$(jq -r '.job_id // empty' "$W/create.json" 2>/dev/null || true)
}

wait_job() {
	local id=$1 limit=$2 t0 code
	t0=$(now)
	while :; do
		code=$(http "$W/job.json" "$API_BASE/v1/jobs/$id")
		if [ "$code" = 200 ]; then
			JOB_STATUS=$(jq -r .status "$W/job.json")
			case "$JOB_STATUS" in done | failed | canceled) break ;; esac
		fi
		if ! within "$(elapsed "$t0" "$(now)")" "$limit"; then
			JOB_STATUS=timeout
			break
		fi
		sleep 2
	done
}

psqlq() { docker compose exec -T postgres psql -U octo -d octo_db -At -F, -c "$1"; }

output_facts() {
	psqlq "select count(*), count(*) filter (where role = 'primary'), coalesce(max(size_bytes), 0), coalesce(max(format), '') from job_outputs where job_id = '$1'"
}

download_output() {
	local url out=$1
	url=$(jq -r '.download_url // empty' "$W/job.json")
	[ -n "$url" ] || return 1
	docker compose exec -T cad-worker /usr/bin/python3 -c \
		'import sys, urllib.request; sys.stdout.buffer.write(urllib.request.urlopen(sys.argv[1]).read())' "$url" > "$out"
}

judge() {
	local stl=$1 line rc=0
	shift
	line=$(python3 scripts/cad-stl-judge.py "$stl" "$@") || rc=$?
	JUDGE_FIELDS=$(printf '%s' "$line" | sed -e 's/^JUDGE,//' -e 's/^stl=[^,]*,//')
	JUDGE_RC=$rc
}

mesh_pids() {
	docker exec "$CNAME" sh -c '
		for d in /proc/[0-9]*; do
			p=${d#/proc/}
			c=$(cat "$d/comm" 2>/dev/null) || continue
			case "$c" in
			cad-mesh) echo "$p" ;;
			python*) grep -qa cad-mesh "$d/cmdline" 2>/dev/null && echo "$p" ;;
			esac
		done
		true' 2>/dev/null | tr '\n' ' ' || true
}

mesh_census() {
	docker exec "$CNAME" sh -c '
		start() { set -- $(sed "s/^.*) //" /proc/$1/stat); echo "${20}"; }
		init=$(start 1)
		old=0
		new=0
		for d in /proc/[0-9]*; do
			p=${d#/proc/}
			c=$(cat "$d/comm" 2>/dev/null) || continue
			case "$c" in
			cad-mesh) ;;
			python*) grep -qa cad-mesh "$d/cmdline" 2>/dev/null || continue ;;
			*) continue ;;
			esac
			s=$(start "$p") || continue
			if [ "$s" -lt "$init" ]; then old=$((old + 1)); else new=$((new + 1)); fi
		done
		echo "$old $new"' 2>/dev/null || true
}

wait_mesh() {
	local pids=""
	for _ in $(seq 1 240); do
		pids=$(mesh_pids)
		[ -n "$pids" ] && break
		sleep 0.5
	done
	MESH_PIDS=$pids
	[ -n "$MESH_PIDS" ]
}

oom_kill_count() {
	docker exec "$CNAME" cat /sys/fs/cgroup/memory.events | awk '$1 == "oom_kill" { print $2 }'
}

container_counters() {
	C_OOMKILLED=$(docker inspect -f '{{.State.OOMKilled}}' "$CNAME")
	C_RESTARTS=$(docker inspect -f '{{.RestartCount}}' "$CNAME")
	C_STARTED=$(docker inspect -f '{{.State.StartedAt}}' "$CNAME")
	C_OOMKILL=$(oom_kill_count)
}

FORMATS_JSON="$W/formats.json"
CODE=$(http "$FORMATS_JSON" "$API_BASE/v1/formats")
[ "$CODE" = 200 ] || fail "GET /v1/formats returned $CODE"
CAD_FORMATS='["step","stp","iges","igs","stl","dxf","dwg"]'
TOUCHING=$(jq -c --argjson f "$CAD_FORMATS" '[.engines | to_entries[] | .key as $e | .value.pairs[] | select(any(.[]; . as $x | $f | index($x))) | [$e, .[0], .[1]]] | sort' "$FORMATS_JSON")
OPS_TOUCHING=$(jq -c --argjson f "$CAD_FORMATS" '[.operations | to_entries[] | .key as $o | .value | to_entries[] | .key as $e | .value.pairs[] | select(any(.[]; . as $x | $f | index($x))) | [$o, $e, .[0], .[1]]] | sort' "$FORMATS_JSON")
[ "$TOUCHING" = '[["cad","iges","stl"],["cad","step","stl"]]' ] || fail "engines touching cad formats: $TOUCHING"
[ "$OPS_TOUCHING" = '[["convert","cad","iges","stl"],["convert","cad","step","stl"]]' ] || fail "operations touching cad formats: $OPS_TOUCHING"
ev "FORMATS,cad_pairs=iges>stl;step>stl,other_pairs_touching_cad_formats=0,operations_convert_cad=2,result=ok"
N_FORMATS=1

CODE=$(http "$W/inspect.json" -X POST "$API_BASE/v1/inspect" -F "file=@$CORPUS/synthetic/units_inch.step;filename=units_inch.step")
[ "$CODE" = 200 ] || fail "POST /v1/inspect returned $CODE: $(cat "$W/inspect.json")"
OFFER=$(jq -c '[.operations[] | select(.target == "stl" and .engine == "cad")] | .[0] // empty' "$W/inspect.json")
[ -n "$OFFER" ] || fail "inspect offers no step->stl on engine cad: $(jq -c .operations "$W/inspect.json")"
[ "$(jq -r .format "$W/inspect.json")" = step ] || fail "inspect format is $(jq -r .format "$W/inspect.json"), want step"
[ "$(echo "$OFFER" | jq -r .estimate_source)" = unmeasured ] || fail "estimate_source is $(echo "$OFFER" | jq -r .estimate_source), want unmeasured"
[ "$(echo "$OFFER" | jq -r .max_bytes)" = 66060288 ] || fail "max_bytes is $(echo "$OFFER" | jq -r .max_bytes), want 66060288"
[ "$(echo "$OFFER" | jq -r .available)" = true ] || fail "the cad offer is not available: $OFFER"
ev "INSPECT,format=step,operation=$(echo "$OFFER" | jq -r .operation),target=stl,engine=cad,estimate_source=unmeasured,estimated_max_seconds=$(echo "$OFFER" | jq -r .estimated_max_seconds),max_bytes=66060288,available=true,result=ok"
N_INSPECT=1

convert_one() {
	local label=$1 file=$2 upname=$3
	shift 3
	local t0 t1 secs facts n primary size fmt stl
	t0=$(now)
	create_job "$file" "$upname" stl
	[ "$HTTP" = 202 ] && [ -n "$JOB_ID" ] || fail "CONVERT $label: create returned $HTTP: $(cat "$W/create.json")"
	wait_job "$JOB_ID" "$ENGINE_TIMEOUT"
	t1=$(now)
	secs=$(elapsed "$t0" "$t1")
	[ "$JOB_STATUS" = "done" ] || fail "CONVERT $label: job $JOB_ID ended $JOB_STATUS: $(cat "$W/job.json")"
	facts=$(output_facts "$JOB_ID")
	n=${facts%%,*}
	primary=$(echo "$facts" | cut -d, -f2)
	size=$(echo "$facts" | cut -d, -f3)
	fmt=$(echo "$facts" | cut -d, -f4)
	[ "$n" = 1 ] && [ "$primary" = 1 ] && [ "$fmt" = stl ] || fail "CONVERT $label: job_outputs facts '$facts', want exactly one primary stl"
	[ "$(jq 'has("outputs")' "$W/job.json")" = false ] || fail "CONVERT $label: the response carries an outputs array"
	stl="$W/$label.stl"
	download_output "$stl" || fail "CONVERT $label: no download_url on the done job"
	[ "$(wc -c < "$stl" | tr -d ' ')" = "$size" ] || fail "CONVERT $label: downloaded $(wc -c < "$stl" | tr -d ' ') bytes, job_outputs says $size"
	judge "$stl" "$@"
	[ "$JUDGE_RC" = 0 ] || fail "CONVERT $label: judge failed: $JUDGE_FIELDS"
	ev "CONVERT,name=$label,upload=$upname,job=$JOB_ID,seconds=$secs,outputs=$n,primary=$primary,bytes=$size,sha256=$(shasum -a 256 "$stl" | cut -d' ' -f1),judge=ok,$JUDGE_FIELDS"
	N_CONVERT=$((N_CONVERT + 1))
}

convert_one units_inch_step "$CORPUS/synthetic/units_inch.step" units_inch.step --expect-triangles 12 --expect-extents 25.4,50.8,76.2
convert_one nist_ctc_05_stp "$CORPUS/public/nist_ctc_05_asme1_ap242-e1.stp" nist_ctc_05_asme1_ap242-e1.stp --step-source "$CORPUS/public/nist_ctc_05_asme1_ap242-e1.stp"
convert_one nist_d2mi_906_stp "$CORPUS/public/nist-d2mi-827-9999-906.stp" nist-d2mi-827-9999-906.stp --step-source "$CORPUS/public/nist-d2mi-827-9999-906.stp"
convert_one festo_valve_igs "$FETCHED/freecad-festo-valve-5-2.igs" freecad-festo-valve-5-2.igs
convert_one festo_valve_iges "$FETCHED/freecad-festo-valve-5-2.igs" freecad-festo-valve-5-2.iges

python3 - "$W/lying.stl" <<'PY'
import struct, sys
with open(sys.argv[1], "wb") as fh:
    fh.write(b"\0" * 80 + struct.pack("<I", 0xFFFFFFFF) + b"\0" * 50)
PY
CODE=$(http "$W/reject1.json" -X POST "$API_BASE/v1/jobs" -F "target=png" -F "file=@$W/lying.stl;filename=part.stl")
[ "$CODE" = 422 ] && [ "$(jq -r .code "$W/reject1.json")" = truncated_cad_file ] || fail "REJECT lying STL: got $CODE $(cat "$W/reject1.json")"
ev "REJECT,name=lying_count_stl,bytes=$(wc -c < "$W/lying.stl" | tr -d ' '),http=422,code=truncated_cad_file,result=ok"
N_REJECT=$((N_REJECT + 1))

python3 - "$CORPUS/synthetic/units_inch.step" "$W/padded.step" <<'PY'
import sys
src = open(sys.argv[1], "rb").read()
at = src.index(b"DATA;") + len(b"DATA;")
need = 63 * 1024 * 1024 + 1 - len(src)
assert need > 0
open(sys.argv[2], "wb").write(src[:at] + b" " * need + src[at:])
PY
PADDED_BYTES=$(wc -c < "$W/padded.step" | tr -d ' ')
[ "$PADDED_BYTES" = 66060289 ] || fail "padded STEP is $PADDED_BYTES bytes, want 66060289"
CODE=$(http "$W/reject2.json" -X POST "$API_BASE/v1/jobs" -F "target=stl" -F "file=@$W/padded.step;filename=padded.step")
[ "$CODE" = 413 ] && [ "$(jq -r .code "$W/reject2.json")" = payload_too_large ] || fail "REJECT padded STEP: got $CODE $(cat "$W/reject2.json")"
MSG=$(jq -r .error "$W/reject2.json")
case "$MSG" in *step*63\ MiB*) ;; *) fail "REJECT padded STEP: message '$MSG' does not name step and 63 MiB" ;; esac
ev "REJECT,name=step_63mib_plus_1,bytes=$PADDED_BYTES,http=413,code=payload_too_large,message=$MSG,result=ok"
N_REJECT=$((N_REJECT + 1))
rm -f "$W/padded.step"

BOMBS="$W/bombs"
mkdir -p "$BOMBS"
chmod 777 "$BOMBS"
python3 scripts/cad-spike.py gen-bombs "$BOMBS" --flood-entities 754763 --flood-nauo 1519 >/dev/null
python3 scripts/cad-spike.py gen-mapped-text --levels 12 "$BOMBS/hier_mappedtext_K12.step" >/dev/null
sed "s/^#35 = DIRECTION('',(1\\.,0\\.,-0\\.));\$/#35 = DIRECTION('',(0.,0.,0.));/" "$CORPUS/synthetic/units_inch.step" >"$BOMBS/partial_face.step"
! cmp -s "$CORPUS/synthetic/units_inch.step" "$BOMBS/partial_face.step" || fail "the partial_face.step fixture equals its source"
image_python() {
	docker run --rm --network none --cpus 2 --memory 4g --memory-swap 4g --user nobody -e HOME=/tmp -e LANG=C.UTF-8 \
		-v "$ROOT:/src:ro" -v "$ROOT/$CORPUS/public:/base:ro" -v "$W/out:/out" -w /src \
		--entrypoint /opt/venv/bin/python "$CAD_IMAGE" -I -B "$@"
}
mkdir -p "$W/out"
chmod 777 "$W/out"
image_python scripts/cad-spike.py gen-hierarchy --kind nauo --levels 12 --leaf /src/$CORPUS/synthetic/units_inch.step /out/hier_nauo_K12.step >/dev/null
mv "$W/out/hier_nauo_K12.step" "$BOMBS/hier_nauo_K12.step"
image_python scripts/cad-corpus-gen.py --scaled-from /base/LinearSlide-MGN15x-Block.step --kind step-nauo --targets 1000 --scaled-dir /out >/dev/null
NAUO1000="$W/out/step-nauo-LinearSlide-MGN15x-Block-n1000.step"
[ -s "$NAUO1000" ] || fail "the step-nauo n=1000 input was not generated"

container_counters
BASE_RESTARTS=$C_RESTARTS
BASE_STARTED=$C_STARTED
BASE_OOMKILL=$C_OOMKILL
[ "$C_OOMKILLED" = false ] || fail "the cad-worker was already OOMKilled before the bombs"
log "baseline: restarts=$BASE_RESTARTS started=$BASE_STARTED oom_kill=$BASE_OOMKILL"

bomb_one() {
	local name=$1 want=$2 bound=$3 file="$BOMBS/$1" t0 t1 secs code
	t0=$(now)
	create_job "$file" "$name" stl
	[ "$HTTP" = 202 ] && [ -n "$JOB_ID" ] || fail "BOMB $name: create returned $HTTP: $(cat "$W/create.json")"
	wait_job "$JOB_ID" "$ENGINE_TIMEOUT"
	t1=$(now)
	secs=$(elapsed "$t0" "$t1")
	[ "$JOB_STATUS" = failed ] || fail "BOMB $name: job $JOB_ID ended $JOB_STATUS, want failed: $(cat "$W/job.json")"
	code=$(jq -r '.error_code // empty' "$W/job.json")
	[ "$code" = "$want" ] || fail "BOMB $name: error_code '$code', want $want: $(cat "$W/job.json")"
	within "$secs" "$bound" || fail "BOMB $name: took ${secs}s, bound ${bound}s"
	container_counters
	[ "$C_OOMKILLED" = false ] || fail "BOMB $name: the cad-worker container was OOMKilled"
	[ "$C_RESTARTS" = "$BASE_RESTARTS" ] || fail "BOMB $name: RestartCount moved $BASE_RESTARTS -> $C_RESTARTS"
	[ "$C_STARTED" = "$BASE_STARTED" ] || fail "BOMB $name: StartedAt moved"
	[ "$C_OOMKILL" = "$BASE_OOMKILL" ] || fail "BOMB $name: memory.events oom_kill moved $BASE_OOMKILL -> $C_OOMKILL"
	ev "BOMB,name=$name,bytes=$(wc -c < "$file" | tr -d ' '),sha256=$(shasum -a 256 "$file" | cut -d' ' -f1),job=$JOB_ID,status=failed,code=$code,seconds=$secs,bound_s=$bound,oom_killed=false,restart_count=$C_RESTARTS,started_at_unchanged=1,oom_kill=$C_OOMKILL"
	N_BOMB=$((N_BOMB + 1))
}

bomb_one flood_entities_754763.step cad_too_complex 30
bomb_one flood_nauo_1519.step cad_too_complex 30
bomb_one hier_nauo_K12.step cad_too_complex 30
bomb_one hier_mappedtext_K12.step cad_too_complex 30
bomb_one s2_self_ref.step cad_unreadable "$ENGINE_TIMEOUT"
bomb_one s2_cycle2.step cad_unreadable "$ENGINE_TIMEOUT"
bomb_one s1_coord_abc.step cad_unreadable "$ENGINE_TIMEOUT"
bomb_one s1_coord_dblminus.step cad_unreadable "$ENGINE_TIMEOUT"
bomb_one s1_coord_1e300.step cad_unreadable "$ENGINE_TIMEOUT"
bomb_one s1_coord_1e999.step cad_unreadable "$ENGINE_TIMEOUT"
bomb_one s1_coord_nan.step cad_unreadable "$ENGINE_TIMEOUT"
bomb_one partial_face.step cad_unreadable "$ENGINE_TIMEOUT"

NAUO_NAME=step-nauo-LinearSlide-MGN15x-Block-n1000.step
NAUO_SHA=$(shasum -a 256 "$NAUO1000" | cut -d' ' -f1)

create_job "$NAUO1000" "$NAUO_NAME" stl
[ "$HTTP" = 202 ] && [ -n "$JOB_ID" ] || fail "CANCEL: create returned $HTTP: $(cat "$W/create.json")"
CANCEL_JOB=$JOB_ID
wait_mesh || fail "CANCEL: no cad-mesh process ever appeared for job $CANCEL_JOB"
BEFORE_PIDS=$MESH_PIDS
T_CANCEL=$(now)
CODE=$(http "$W/cancel.json" -X POST "$API_BASE/v1/jobs/$CANCEL_JOB/cancel")
[ "$CODE" = 200 ] && [ "$(jq -r .status "$W/cancel.json")" = canceled ] || fail "CANCEL: cancel returned $CODE $(cat "$W/cancel.json")"
GONE=0
for _ in $(seq 1 24); do
	NOW_PIDS=$(mesh_pids)
	LEFT=""
	for p in $BEFORE_PIDS; do
		case " $NOW_PIDS " in *" $p "*) LEFT="$LEFT $p" ;; esac
	done
	if [ -z "$LEFT" ]; then
		GONE=1
		break
	fi
	sleep 0.25
done
T_GONE=$(now)
GONE_S=$(elapsed "$T_CANCEL" "$T_GONE")
[ "$GONE" = 1 ] && within "$GONE_S" 5 || fail "CANCEL: cad-mesh pid(s) [$BEFORE_PIDS] still alive ${GONE_S}s after cancel"
wait_job "$CANCEL_JOB" 30
[ "$JOB_STATUS" = canceled ] || fail "CANCEL: job reads $JOB_STATUS, want canceled"
[ "$(jq 'has("download_url")' "$W/job.json")" = false ] || fail "CANCEL: a canceled job exposes a download_url"
ev "CANCEL,job=$CANCEL_JOB,input=$NAUO_NAME,sha256=$NAUO_SHA,mesh_pids=$(echo $BEFORE_PIDS | tr ' ' ';'),pid_gone=1,gone_s=$GONE_S,status=canceled,download_url=absent,result=ok"
N_CANCEL=1

sleep 3
create_job "$NAUO1000" "$NAUO_NAME" stl
[ "$HTTP" = 202 ] && [ -n "$JOB_ID" ] || fail "WORKER_KILL: create returned $HTTP: $(cat "$W/create.json")"
KILL_JOB=$JOB_ID
wait_mesh || fail "WORKER_KILL: no cad-mesh process ever appeared for job $KILL_JOB"
KILLED_PIDS=$MESH_PIDS
PRE_STARTED=$(docker inspect -f '{{.State.StartedAt}}' "$CNAME")
docker kill --signal KILL "$CNAME" >/dev/null
T_KILL=$(now)
BACK=0
for _ in $(seq 1 60); do
	if [ "$(docker inspect -f '{{.State.Running}}' "$CNAME")" = true ] && [ "$(docker inspect -f '{{.State.StartedAt}}' "$CNAME")" != "$PRE_STARTED" ]; then
		BACK=1
		break
	fi
	sleep 0.5
done
if [ "$BACK" != 1 ]; then
	docker compose up -d cad-worker >&2
	for _ in $(seq 1 60); do
		[ "$(docker inspect -f '{{.State.Running}}' "$CNAME")" = true ] && [ "$(docker inspect -f '{{.State.StartedAt}}' "$CNAME")" != "$PRE_STARTED" ] && break
		sleep 0.5
	done
	RESTART_VIA=compose_up
else
	RESTART_VIA=restart_policy
fi
[ "$(docker inspect -f '{{.State.Running}}' "$CNAME")" = true ] || fail "WORKER_KILL: the cad-worker is not running after the kill"
CENSUS=$(mesh_census)
T_BACK=$(now)
BACK_S=$(elapsed "$T_KILL" "$T_BACK")
[ -n "$CENSUS" ] || fail "WORKER_KILL: could not read the restarted container's process table"
ORPHAN_COUNT=${CENSUS% *}
FRESH_MESH=${CENSUS#* }
[ "$ORPHAN_COUNT" = 0 ] || fail "WORKER_KILL: $ORPHAN_COUNT cad-mesh process(es) older than the restarted container's init ${BACK_S}s after the kill"
RESTARTED_AT=$(docker inspect -f '{{.State.StartedAt}}' "$CNAME")
for _ in $(seq 1 60); do
	curl -fsS -o /dev/null "$API_BASE/healthz" 2>/dev/null && break
	sleep 1
done
wait_job "$KILL_JOB" "$ENGINE_TIMEOUT"
T_DONE=$(now)
KILL_TO_DONE=$(elapsed "$T_KILL" "$T_DONE")
[ "$JOB_STATUS" = "done" ] || fail "WORKER_KILL: job $KILL_JOB ended $JOB_STATUS after ${KILL_TO_DONE}s: $(cat "$W/job.json")"
within "$KILL_TO_DONE" "$ENGINE_TIMEOUT" || fail "WORKER_KILL: kill-to-done ${KILL_TO_DONE}s is not below CAD_ENGINE_TIMEOUT ${ENGINE_TIMEOUT}s"
RECONCILER=$(docker compose ps --status running --services 2>/dev/null | grep -cx webhook-worker || true)
[ "$RECONCILER" = 0 ] || fail "WORKER_KILL: webhook-worker is running, the reconciler may have played a part"
facts=$(output_facts "$KILL_JOB")
[ "${facts%%,*}" = 1 ] && [ "$(echo "$facts" | cut -d, -f2)" = 1 ] || fail "WORKER_KILL: job_outputs facts '$facts'"
KSTL="$W/worker_kill.stl"
download_output "$KSTL" || fail "WORKER_KILL: no download_url on the done job"
judge "$KSTL"
[ "$JUDGE_RC" = 0 ] || fail "WORKER_KILL: judge failed: $JUDGE_FIELDS"
EVENTS=$(psqlq "select string_agg(coalesce(from_status, '-') || '>' || to_status, ';' order by id) from job_events where job_id = '$KILL_JOB'" || true)
ev "WORKER_KILL,job=$KILL_JOB,input=$NAUO_NAME,sha256=$NAUO_SHA,killed_mesh_pids=$(echo $KILLED_PIDS | tr ' ' ';'),restart=$RESTART_VIA,restarted_after_s=$BACK_S,started_at_before=$PRE_STARTED,started_at_after=$RESTARTED_AT,orphans=0,fresh_mesh_at_restart=$FRESH_MESH,kill_to_done_s=$KILL_TO_DONE,cad_engine_timeout_s=$ENGINE_TIMEOUT,reconciler_running=0,status=done,events=$EVENTS,judge=ok,$JUDGE_FIELDS"
N_KILL=1

ev "SUMMARY,result=ALL PASS,formats=$N_FORMATS,inspect=$N_INSPECT,convert=$N_CONVERT,reject=$N_REJECT,bomb=$N_BOMB,cancel=$N_CANCEL,worker_kill=$N_KILL"
FINISHED=1
