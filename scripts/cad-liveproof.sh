#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT=$(pwd)

usage() {
	cat <<'USAGE'
usage: scripts/cad-liveproof.sh [--out FILE]

Runs the CAD live proof against the compose stack of this checkout and writes
one evidence line per check to FILE (relative to the repository root, default
.planning/phases/74-3d-render-measurements/evidence/74-07-liveproof.txt).
Starts only the services that are not running and stops only those.
USAGE
}

EVID=".planning/phases/74-3d-render-measurements/evidence/74-07-liveproof.txt"
while [ $# -gt 0 ]; do
	case "$1" in
	--out)
		[ $# -ge 2 ] || {
			usage >&2
			exit 64
		}
		EVID=$2
		shift 2
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		usage >&2
		exit 64
		;;
	esac
done

API_BASE="${CAD_LIVEPROOF_API_BASE:-http://127.0.0.1:8090}"
DB_URL="${CAD_LIVEPROOF_DATABASE_URL:-postgres://octo:octo-pass@127.0.0.1:5434/octo_db}"
SALT="${CAD_LIVEPROOF_API_KEY_SALT:-dev-only-change-me-in-real-deploys}"
CNAME=octoconv-cad-worker
SERVICES="postgres redis minio api cad-worker"
CORPUS=internal/convert/testdata/cad
FETCHED="${OCTOCONV_CAD_CACHE:-$HOME/.cache/octoconv/cad-corpus}/fetched"
SCALED="${OCTOCONV_CAD_CACHE:-$HOME/.cache/octoconv/cad-corpus}/scaled"
REPL50_NAME=dxf-replicate-uncommon-50MiB.dxf
REPL50_SHA=5d14734c8907e813031cfd99ce7242164ee8ae525d4bfd2974a742967489f652
DXF_SPECIAL="A&B <x> \"q\" 'a' & <script>alert(1)</script>"
export GOTOOLCHAIN="${GOTOOLCHAIN:-go1.26.4}"

W=""
STARTED_BY_ME=""
FINISHED=""
N_FORMATS=0 N_INSPECT=0 N_CONVERT=0 N_QUALITY=0 N_RENDER=0 N_VIEWS=0 N_MEAS=0 N_OPTS_REJECT=0 N_REJECT=0 N_BOMB=0
N_RENDER_RETRY=0 N_MESH_SIGNAL=0 N_EMPTY=0 N_CANCEL_RENDER=0 N_CANCEL=0 N_KILL=0 N_IMAGE=0
N_SANDBOX=0 N_DXF_SVG=0 N_DXF_PDF=0 N_DXF_RENDER=0 N_DXF_MEAS=0 N_DXF_OPTS_REJECT=0 N_DXF_BOMB=0 N_DXF_EMPTY=0 N_DXF_CEILING=0 N_DXF_RETRY=0 N_DXF_CANCEL=0

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
for tool in docker curl jq python3 go shasum pdftotext pdfinfo pdffonts; do
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
for f in synthetic/units_inch.step synthetic/stl_ascii_box.stl synthetic/surfaces_only.iges public/nist_ctc_05_asme1_ap242-e1.stp public/nist-d2mi-827-9999-906.stp public/LinearSlide-MGN15x-Block.step public/nasa-hubble-all-pieces.stl; do
	[ -f "$CORPUS/$f" ] || fail "missing $CORPUS/$f"
done
[ -f "$FETCHED/freecad-festo-valve-5-2.igs" ] || fail "missing $FETCHED/freecad-festo-valve-5-2.igs"
for f in synthetic/legacy_cp1251.dxf public/uncommon.dxf public/3dsolids.dxf dxf/text_cases.dxf dxf/layers_off_frozen.dxf dxf/never_resolved.dxf dxf/wide.dxf dxf/tall.dxf dxf/far_point.dxf dxf/wrong_extents.dxf dxf/units_0.dxf dxf/units_1.dxf dxf/units_3.dxf dxf/units_13.dxf dxf/all_hidden.dxf dxf/single_point.dxf dxf/text_alpha.dxf dxf/text_alpha_faint.dxf dxf/text_fonts.dxf dxf-out/text_fonts.texts.json dxf-out/text_fonts.render.svg dxf/alpha_clear_geometry.dxf dxf/alpha_clear_all.dxf dxf/alpha_one_visible.dxf dxf-out/alpha_one_visible.texts.json dxf-out/alpha_one_visible.render.svg dxf/white_geometry.dxf dxf/white_text.dxf dxf/white_one_black.dxf dxf-out/white_one_black.texts.json dxf-out/white_one_black.render.svg dxf/degenerate_fill.dxf dxf/faint_only.dxf dxf/zwsp_text.dxf; do
	[ -f "$CORPUS/$f" ] || fail "missing $CORPUS/$f"
done
REPL50="$SCALED/$REPL50_NAME"
[ -f "$REPL50" ] || fail "missing $REPL50"
[ "$(shasum -a 256 "$REPL50" | cut -d' ' -f1)" = "$REPL50_SHA" ] || fail "$REPL50 is not the 75-02 replicate (sha256 differs from $REPL50_SHA)"

LARGE_STL=$(ls -S "$ROOT"/$CORPUS/public/*.stl "$FETCHED"/*.stl | head -1)
[ -f "$LARGE_STL" ] || fail "no STL found for the render retry"

mkdir -p "$(dirname "$EVID")"
: > "$EVID"
W=$(mktemp -d)
chmod 755 "$W"
mkdir -p "$W/bin"
go build -o "$W/bin/cad-pixel-judge" ./scripts/cmd/cad-pixel-judge || fail "cannot build cad-pixel-judge"
go build -o "$W/bin/cad-measurements-check" ./scripts/cmd/cad-measurements-check || fail "cannot build cad-measurements-check"
go build -o "$W/bin/cad-svg-check" ./scripts/cmd/cad-svg-check || fail "cannot build cad-svg-check"
go build -o "$W/bin/cad-pdf-check" ./scripts/cmd/cad-pdf-check || fail "cannot build cad-pdf-check"

RUNNING=$(docker compose ps --status running --services 2>/dev/null || true)
for s in $SERVICES; do
	echo "$RUNNING" | grep -qx "$s" || STARTED_BY_ME="$STARTED_BY_ME $s"
done
if echo "$RUNNING" | grep -qx webhook-worker; then
	fail "webhook-worker (the reconciler host) is running; WORKER_KILL needs it absent"
fi
log "starting the stack (not running before:${STARTED_BY_ME:- none})"
UP_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ)
docker compose up -d --build postgres redis minio createbucket api cad-worker >&2

for _ in $(seq 1 60); do
	curl -fsS -o /dev/null "$API_BASE/healthz" 2>/dev/null && break
	sleep 2
done
curl -fsS -o /dev/null "$API_BASE/healthz" || fail "api /healthz not reachable at $API_BASE"
for _ in $(seq 1 60); do
	WORKER_LOG=$(docker compose logs --no-color --since "$UP_AT" cad-worker 2>/dev/null || true)
	case "$WORKER_LOG" in *"cad sandbox proven"*) break ;; esac
	sleep 2
done
case "$WORKER_LOG" in *"cad sandbox proven"*) ;; *) fail "cad-worker never logged 'cad sandbox proven' since $UP_AT" ;; esac

CAD_IMAGE=$(docker inspect -f '{{.Image}}' "$CNAME")
API_CID=$(docker compose ps -q api)
ENGINE_TIMEOUT_RAW=$(docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$CNAME" | sed -n 's/^CAD_ENGINE_TIMEOUT=//p')
ENGINE_TIMEOUT=${ENGINE_TIMEOUT_RAW%s}
RENDER_TIMEOUT_RAW=$(docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$CNAME" | sed -n 's/^CAD_RENDER_TIMEOUT=//p')
RENDER_TIMEOUT=${RENDER_TIMEOUT_RAW%s}
SANDBOX_ENV=$(docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$CNAME" | sed -n 's/^CAD_SANDBOX_DISABLED=//p')
[ -z "$SANDBOX_ENV" ] || fail "the cad-worker container has CAD_SANDBOX_DISABLED=$SANDBOX_ENV"
case "$ENGINE_TIMEOUT" in '' | *[!0-9]*) fail "cannot read CAD_ENGINE_TIMEOUT from the container (got '$ENGINE_TIMEOUT_RAW')" ;; esac
case "$RENDER_TIMEOUT" in '' | *[!0-9]*) fail "cannot read CAD_RENDER_TIMEOUT from the container (got '$RENDER_TIMEOUT_RAW')" ;; esac
DXF_TIMEOUT_RAW=$(docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$CNAME" | sed -n 's/^CAD_DXF_TIMEOUT=//p')
DXF_TIMEOUT=${DXF_TIMEOUT_RAW%s}
RSVG_TIMEOUT_RAW=$(docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$CNAME" | sed -n 's/^CAD_RSVG_TIMEOUT=//p')
RSVG_TIMEOUT=${RSVG_TIMEOUT_RAW%s}
case "$DXF_TIMEOUT" in '' | *[!0-9]*) fail "cannot read CAD_DXF_TIMEOUT from the container (got '$DXF_TIMEOUT_RAW')" ;; esac
case "$RSVG_TIMEOUT" in '' | *[!0-9]*) fail "cannot read CAD_RSVG_TIMEOUT from the container (got '$RSVG_TIMEOUT_RAW')" ;; esac

ev "HDR,date_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
ev "HDR,commit=$(git rev-parse HEAD)"
DIRT=$(git status --short --untracked-files=no | awk -v e="$EVID" '$2 != e { print $2 }')
ev "HDR,tracked_dirt=$(printf '%s' "$DIRT" | grep -c . || true)"
ev "HDR,tracked_dirt_paths=$(printf '%s' "$DIRT" | tr '\n' ';' | sed 's/;$//')"
ev "HDR,api_image_id=$(docker inspect -f '{{.Image}}' "$API_CID")"
ev "HDR,cad_worker_image_id=$CAD_IMAGE"
ev "HDR,cad_engine_timeout_s=$ENGINE_TIMEOUT"
ev "HDR,cad_render_timeout_s=$RENDER_TIMEOUT"
ev "HDR,cad_dxf_timeout_s=$DXF_TIMEOUT"
ev "HDR,cad_rsvg_timeout_s=$RSVG_TIMEOUT"
ev "HDR,sandbox_disabled=false"
ev "HDR,started_by_run=$(echo "${STARTED_BY_ME:-none}" | tr -s ' ' | sed 's/^ //' | tr ' ' ';')"
ev "HDR,server_arch=$(docker version --format '{{.Server.Arch}}')"
ev "HDR,disk_free_gib=$DISK_GIB"

LANDLOCK_LINE=$(printf '%s\n' "$WORKER_LOG" | grep 'cad sandbox: landlock via ' | tail -1)
PROVEN_LINE=$(printf '%s\n' "$WORKER_LOG" | grep 'cad sandbox proven: ' | tail -1)
case "$LANDLOCK_LINE" in *"(convert, render and dxf)"*) ;; *) fail "SANDBOX: the cad-worker log has no landlock line naming dxf: '$LANDLOCK_LINE'" ;; esac
case "$PROVEN_LINE" in *cad-dxf*rsvg-convert*"ran inside the job directory and were denied a file outside it"*) ;; *) fail "SANDBOX: the proven line does not name cad-dxf and rsvg-convert: '$PROVEN_LINE'" ;; esac
ev "SANDBOX,landlock_line=$(printf '%s' "${LANDLOCK_LINE#*cad sandbox: }" | sed 's/,/;/g'),proven_line=$(printf '%s' "${PROVEN_LINE#*cad sandbox proven: }" | sed 's/,/;/g'),names_cad_dxf=1,names_rsvg_convert=1,result=ok"
N_SANDBOX=1

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
	local args=(-F "target=$3" -F "file=@$1;filename=$2")
	if [ -n "${4:-}" ]; then
		args+=(-F "operation=$4")
	fi
	if [ -n "${5:-}" ]; then
		args+=(-F "opts=$5")
	fi
	HTTP=$(http "$W/create.json" -X POST "$API_BASE/v1/jobs" "${args[@]}")
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

fetch_url() {
	docker compose exec -T cad-worker /usr/bin/python3 -c \
		'import sys, urllib.request; sys.stdout.buffer.write(urllib.request.urlopen(sys.argv[1]).read())' "$1" > "$2"
}

download_output() {
	local url out=$1
	url=$(jq -r '.download_url // empty' "$W/job.json")
	[ -n "$url" ] || return 1
	fetch_url "$url" "$out"
}

role_url() { jq -r --arg r "$1" '[.outputs[]? | select(.role == $r) | .url][0] // empty' "$W/job.json"; }

judge() {
	local stl=$1 line rc=0
	shift
	line=$(python3 scripts/cad-stl-judge.py "$stl" "$@") || rc=$?
	JUDGE_FIELDS=$(printf '%s' "$line" | sed -e 's/^JUDGE,//' -e 's/^stl=[^,]*,//')
	JUDGE_RC=$rc
}

proc_pids() {
	docker exec "$CNAME" sh -c '
		for d in /proc/[0-9]*; do
			p=${d#/proc/}
			c=$(cat "$d/comm" 2>/dev/null) || continue
			case "$c" in
			"$1") echo "$p" ;;
			python*) grep -qa "$1" "$d/cmdline" 2>/dev/null && echo "$p" ;;
			esac
		done
		true' sh "$1" 2>/dev/null | tr '\n' ' ' || true
}

mesh_pids() { proc_pids cad-mesh; }
render_pids() { proc_pids cad-render; }

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
[ "$TOUCHING" = '[["cad","dxf","pdf"],["cad","dxf","svg"],["cad","iges","stl"],["cad","step","stl"]]' ] || fail "engines touching cad formats: $TOUCHING"
WANT_OPS='[["convert","cad","dxf","pdf"],["convert","cad","dxf","svg"],["convert","cad","iges","stl"],["convert","cad","step","stl"],["render","cad","dxf","png"],["render","cad","iges","png"],["render","cad","step","png"],["render","cad","stl","png"]]'
[ "$OPS_TOUCHING" = "$WANT_OPS" ] || fail "operations touching cad formats: $OPS_TOUCHING"
[ "$(echo "$OPS_TOUCHING" | jq length)" = 8 ] || fail "operations touching cad formats are not eight pairs: $OPS_TOUCHING"
ev "FORMATS,convert_cad_pairs=dxf>pdf;dxf>svg;iges>stl;step>stl,render_cad_pairs=dxf>png;iges>png;step>png;stl>png,cad_pairs=8,other_pairs_touching_cad_formats=0,operations_convert_cad=4,operations_render_cad=4,result=ok"
N_FORMATS=1

inspect_offers() {
	local file=$1 upname=$2 fmt=$3 op=$4 target=$5 want_max=$6 offer
	CODE=$(http "$W/inspect.json" -X POST "$API_BASE/v1/inspect" -F "file=@$file;filename=$upname")
	[ "$CODE" = 200 ] || fail "POST /v1/inspect returned $CODE: $(cat "$W/inspect.json")"
	[ "$(jq -r .format "$W/inspect.json")" = "$fmt" ] || fail "inspect format of $upname is $(jq -r .format "$W/inspect.json"), want $fmt"
	offer=$(jq -c --arg op "$op" --arg t "$target" '[.operations[] | select(.operation == $op and .target == $t and .engine == "cad")] | .[0] // empty' "$W/inspect.json")
	[ -n "$offer" ] || fail "inspect of $upname offers no $op $fmt->$target on engine cad: $(jq -c .operations "$W/inspect.json")"
	[ "$(echo "$offer" | jq -r .estimate_source)" = unmeasured ] || fail "estimate_source of $upname $op is $(echo "$offer" | jq -r .estimate_source), want unmeasured"
	[ "$(echo "$offer" | jq -r .max_bytes)" = "$want_max" ] || fail "max_bytes of $upname $op is $(echo "$offer" | jq -r .max_bytes), want $want_max"
	[ "$(echo "$offer" | jq -r .available)" = true ] || fail "the cad offer of $upname $op is not available: $offer"
	ev "INSPECT,format=$fmt,operation=$op,target=$target,engine=cad,estimate_source=unmeasured,estimated_max_seconds=$(echo "$offer" | jq -r .estimated_max_seconds),max_bytes=$want_max,available=true,result=ok"
	N_INSPECT=$((N_INSPECT + 1))
}

inspect_offers "$CORPUS/synthetic/units_inch.step" units_inch.step step convert stl 66060288
inspect_offers "$CORPUS/synthetic/units_inch.step" units_inch.step step render png 66060288
inspect_offers "$CORPUS/synthetic/surfaces_only.iges" surfaces_only.iges iges convert stl 141557760
inspect_offers "$CORPUS/synthetic/surfaces_only.iges" surfaces_only.iges iges render png 141557760
inspect_offers "$CORPUS/synthetic/stl_ascii_box.stl" stl_ascii_box.stl stl render png 190840832
inspect_offers "$CORPUS/synthetic/legacy_cp1251.dxf" legacy_cp1251.dxf dxf convert svg 123731968
inspect_offers "$CORPUS/synthetic/legacy_cp1251.dxf" legacy_cp1251.dxf dxf convert pdf 123731968
inspect_offers "$CORPUS/synthetic/legacy_cp1251.dxf" legacy_cp1251.dxf dxf render png 123731968
DXF_OFFERS=$(jq -c '[.operations[] | select(.engine == "cad") | [.operation, .target]] | sort' "$W/inspect.json")
[ "$DXF_OFFERS" = '[["convert","pdf"],["convert","svg"],["render","png"]]' ] || fail "INSPECT: the cad offers for a dxf are $DXF_OFFERS, want convert pdf, convert svg and render png"
ev "INSPECT,format=dxf,cad_offers=convert>pdf;convert>svg;render>png,offers=3,max_bytes=123731968,result=ok"
N_INSPECT=$((N_INSPECT + 1))

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

png_judge() {
	local line rc=0
	line=$("$W/bin/cad-pixel-judge" --size "$2" --background "$3" "$1" 2>"$W/judge.err") || rc=$?
	PJ_LINE=$(printf '%s' "$line" | sed -e 's/^GOJUDGE,png=[^,]*,//')
	if [ "$rc" != 0 ]; then
		PJ_LINE="$PJ_LINE,err=$(tr '\n' ' ' < "$W/judge.err" | sed 's/,/;/g')"
	fi
	PJ_RC=$rc
}

meas_check() {
	local line rc=0
	line=$("$W/bin/cad-measurements-check" "$1") || rc=$?
	MC_LINE=$(printf '%s' "$line" | sed 's/^MEAS,//')
	MC_RC=$rc
}

mfield() { printf '%s' "$2" | tr ',' '\n' | sed -n "s/^$1=//p" | head -1; }

ratio_is() { awk -v a="$1" -v b="$2" -v w="$3" 'BEGIN { d = a / b - w; if (d < 0) d = -d; exit !(d <= 0.03 * w) }'; }

near() { awk -v a="$1" -v b="$2" 'BEGIN { d = a - b; if (d < 0) d = -d; m = b < 0 ? -b : b; if (m < 1) m = 1; exit !(d <= 0.0001 * m) }'; }

render_start() {
	R_T0=$(now)
	create_job "$2" "$3" png render "${4:-}"
	[ "$HTTP" = 202 ] && [ -n "$JOB_ID" ] || fail "RENDER $1: create returned $HTTP: $(cat "$W/create.json")"
}

render_finish() {
	local label=$1 facts roles
	wait_job "$JOB_ID" "$ENGINE_TIMEOUT"
	R_SECS=$(elapsed "$R_T0" "$(now)")
	[ "$JOB_STATUS" = done ] || fail "RENDER $label: job $JOB_ID ended $JOB_STATUS: $(cat "$W/job.json")"
	facts=$(psqlq "select count(*), count(*) filter (where role = 'primary' and ordinal = 0 and format = 'png'), count(*) filter (where role = 'measurements' and ordinal = 1 and format = 'json') from job_outputs where job_id = '$JOB_ID'")
	[ "$facts" = 2,1,1 ] || fail "RENDER $label: job_outputs facts '$facts', want 2,1,1 (rows, primary png at 0, measurements json at 1)"
	roles=$(jq -c '[.outputs | sort_by(.ordinal)[] | [.role, .ordinal, .format]]' "$W/job.json")
	[ "$roles" = '[["primary",0,"png"],["measurements",1,"json"]]' ] || fail "RENDER $label: the response outputs are $roles"
	PNG_URL=$(role_url primary)
	MEAS_URL=$(role_url measurements)
	[ -n "$PNG_URL" ] && [ -n "$MEAS_URL" ] || fail "RENDER $label: the response gives no url for a role"
	[ "$(jq -r .download_url "$W/job.json")" = "$PNG_URL" ] || fail "RENDER $label: download_url is not the primary output url"
	R_PNG="$W/$label.png"
	R_JSON="$W/$label.measurements.json"
	fetch_url "$PNG_URL" "$R_PNG" || fail "RENDER $label: cannot fetch the primary url"
	fetch_url "$MEAS_URL" "$R_JSON" || fail "RENDER $label: cannot fetch the measurements url"
	[ "$(head -c 8 "$R_PNG" | od -An -tx1 | tr -d ' \n')" = 89504e470d0a1a0a ] || fail "RENDER $label: the download_url body does not start with the PNG signature"
	[ "$(wc -c < "$R_PNG" | tr -d ' ')" = "$(jq -r '[.outputs[] | select(.role == "primary") | .size_bytes][0]' "$W/job.json")" ] || fail "RENDER $label: PNG size differs from the response"
	[ "$(wc -c < "$R_JSON" | tr -d ' ')" = "$(jq -r '[.outputs[] | select(.role == "measurements") | .size_bytes][0]' "$W/job.json")" ] || fail "RENDER $label: measurements size differs from the response"
}

render_job() {
	render_start "$@"
	render_finish "$1"
}

render_judged() {
	png_judge "$R_PNG" "$2" "$3"
	[ "$PJ_RC" = 0 ] || fail "RENDER $1: the pixel judge rejected the PNG: $PJ_LINE"
	meas_check "$R_JSON"
	[ "$MC_RC" = 0 ] || fail "RENDER $1: the measurements body failed Validate: $MC_LINE"
}

render_one() {
	local label=$1 file=$2 upname=$3
	render_job "$label" "$file" "$upname"
	render_judged "$label" 1280 opaque
	ev "RENDER,name=$label,upload=$upname,job=$JOB_ID,seconds=$R_SECS,outputs=2,roles=primary@0;measurements@1,png_bytes=$(wc -c < "$R_PNG" | tr -d ' '),png_sha256=$(shasum -a 256 "$R_PNG" | cut -d' ' -f1),measurements_bytes=$(wc -c < "$R_JSON" | tr -d ' '),judge=ok,$PJ_LINE,measurements=valid,$MC_LINE"
	N_RENDER=$((N_RENDER + 1))
}

render_one units_inch_step "$CORPUS/synthetic/units_inch.step" units_inch.step
render_one nist_ctc_05_stp "$CORPUS/public/nist_ctc_05_asme1_ap242-e1.stp" nist_ctc_05_asme1_ap242-e1.stp
render_one festo_valve_igs "$FETCHED/freecad-festo-valve-5-2.igs" freecad-festo-valve-5-2.igs
render_one festo_valve_iges "$FETCHED/freecad-festo-valve-5-2.igs" freecad-festo-valve-5-2.iges
render_one stl_ascii_box_stl "$CORPUS/synthetic/stl_ascii_box.stl" stl_ascii_box.stl

VIEW_SHAS=""
for view in iso front top; do
	render_job "view_$view" "$CORPUS/synthetic/units_inch.step" units_inch.step "{\"view\":\"$view\"}"
	render_judged "view_$view" 1280 opaque
	VSHA=$(shasum -a 256 "$R_PNG" | cut -d' ' -f1)
	VIEW_SHAS="$VIEW_SHAS $VSHA"
	BOX_W=$(printf '%s' "$PJ_LINE" | sed -n 's/.*box=\([0-9]*\)x\([0-9]*\).*/\1/p')
	BOX_H=$(printf '%s' "$PJ_LINE" | sed -n 's/.*box=\([0-9]*\)x\([0-9]*\).*/\2/p')
	case "$view" in
	front) ratio_is "$BOX_H" "$BOX_W" 0.5 || fail "VIEWS: the front view of the 2 x 3 x 1 inch box (X across, Z up) is ${BOX_W}x${BOX_H}, want height/width 0.5" ;;
	top) ratio_is "$BOX_H" "$BOX_W" 1.5 || fail "VIEWS: the top view of the 2 x 3 x 1 inch box (X across, Y up) is ${BOX_W}x${BOX_H}, want height/width 1.5" ;;
	esac
	ev "VIEWS,name=view_$view,job=$JOB_ID,png_sha256=$VSHA,box=${BOX_W}x${BOX_H},h_over_w=$(awk -v h="$BOX_H" -v w="$BOX_W" 'BEGIN { printf "%.3f", h / w }'),judge=ok,$PJ_LINE"
	N_VIEWS=$((N_VIEWS + 1))
done
[ "$(printf '%s\n' $VIEW_SHAS | sort -u | wc -l | tr -d ' ')" = 3 ] || fail "VIEWS: iso, front and top of one part are not three different images:$VIEW_SHAS"
ev "VIEWS,name=distinct,views=iso;front;top,distinct_sha256=3,front_h_over_w=0.5,top_h_over_w=1.5,z_up=1,result=ok"
N_VIEWS=$((N_VIEWS + 1))

render_job size_2048 "$CORPUS/synthetic/units_inch.step" units_inch.step '{"size":2048}'
render_judged size_2048 2048 opaque
[ "$(mfield w "$PJ_LINE")" = 2048 ] && [ "$(mfield h "$PJ_LINE")" = 2048 ] || fail "VIEWS: size 2048 gave $(mfield w "$PJ_LINE") x $(mfield h "$PJ_LINE")"
ev "VIEWS,name=size_2048,job=$JOB_ID,png_sha256=$(shasum -a 256 "$R_PNG" | cut -d' ' -f1),judge=ok,$PJ_LINE"
N_VIEWS=$((N_VIEWS + 1))

render_job background_transparent "$CORPUS/synthetic/units_inch.step" units_inch.step '{"background":"transparent"}'
png_judge "$R_PNG" 1280 transparent
[ "$PJ_RC" = 0 ] || fail "VIEWS: the transparent render was rejected by the pixel judge: $PJ_LINE"
[ "$(mfield alpha "$PJ_LINE")" = 1 ] || fail "VIEWS: the transparent render carries no alpha channel: $PJ_LINE"
png_judge "$R_PNG" 1280 opaque
[ "$PJ_RC" != 0 ] || fail "VIEWS: the transparent render passes the opaque judge, so its background is not alpha 0"
png_judge "$R_PNG" 1280 transparent
ev "VIEWS,name=background_transparent,job=$JOB_ID,png_sha256=$(shasum -a 256 "$R_PNG" | cut -d' ' -f1),judge=ok,$PJ_LINE"
N_VIEWS=$((N_VIEWS + 1))

meas_case() {
	local name=$1 file=$2 upname=$3 assume=$4 w_unit=$5 w_src=$6 w_ext=$7 w_vol=$8 w_area=$9 w_reason=${10} opts="" got i
	if [ -n "$assume" ]; then
		opts="{\"assume_unit\":\"$assume\"}"
	fi
	render_job "meas_$name" "$file" "$upname" "$opts"
	meas_check "$R_JSON"
	[ "$MC_RC" = 0 ] || fail "MEAS $name: the measurements body failed Validate: $MC_LINE"
	png_judge "$R_PNG" 1280 opaque
	[ "$PJ_RC" = 0 ] || fail "MEAS $name: the pixel judge rejected the PNG: $PJ_LINE"
	if [ "$w_unit" != any ]; then
		[ "$(mfield unit "$MC_LINE")" = "$w_unit" ] || fail "MEAS $name: unit $(mfield unit "$MC_LINE"), want $w_unit"
		[ "$(mfield unit_source "$MC_LINE")" = "$w_src" ] || fail "MEAS $name: unit_source $(mfield unit_source "$MC_LINE"), want $w_src"
	fi
	if [ "$w_ext" != any ]; then
		got=$(mfield extents "$MC_LINE")
		for i in 1 2 3; do
			near "$(echo "$got" | cut -d: -f$i)" "$(echo "$w_ext" | cut -d: -f$i)" || fail "MEAS $name: extents $got, want $w_ext"
		done
	fi
	for pair in "volume:$w_vol" "area:$w_area"; do
		got=$(mfield "${pair%%:*}" "$MC_LINE")
		if [ "${pair#*:}" = null ]; then
			[ "$got" = null ] || fail "MEAS $name: ${pair%%:*} is $got, want null"
		else
			near "$got" "${pair#*:}" || fail "MEAS $name: ${pair%%:*} is $got, want ${pair#*:}"
		fi
	done
	[ "$(mfield reason "$MC_LINE")" = "$w_reason" ] || fail "MEAS $name: reason $(mfield reason "$MC_LINE"), want $w_reason"
	ev "MEAS,name=$name,upload=$upname,assume_unit=${assume:-none},job=$JOB_ID,$MC_LINE,expected_unit=$w_unit,expected_volume=$w_vol,expected_area=$w_area,result=ok"
	N_MEAS=$((N_MEAS + 1))
}

meas_case units_inch "$CORPUS/synthetic/units_inch.step" units_inch.step "" inch declared 1:2:3 6 22 none
meas_case units_inch_assume_mm "$CORPUS/synthetic/units_inch.step" units_inch.step mm inch declared 1:2:3 6 22 none
meas_case stl_ascii_box "$CORPUS/synthetic/stl_ascii_box.stl" stl_ascii_box.stl "" unspecified none 10:20:30 6000 2200 none
meas_case stl_ascii_box_assume_mm "$CORPUS/synthetic/stl_ascii_box.stl" stl_ascii_box.stl mm mm client_assumed 10:20:30 6000 2200 none
meas_case surfaces_only_iges "$CORPUS/synthetic/surfaces_only.iges" surfaces_only.iges "" any any any null null no_solids
meas_case nasa_hubble_stl "$CORPUS/public/nasa-hubble-all-pieces.stl" nasa-hubble-all-pieces.stl "" unspecified none any null null stl_not_watertight
python3 - "$CORPUS/synthetic/stl_ascii_box.stl" "$W" <<'PY' || fail "cannot generate the stl case variants"
import sys

src, out = sys.argv[1], sys.argv[2]
with open(src, "rb") as fh:
    data = fh.read()
lines = data.split(b"\n")
body = b"\n".join(lines[1:-2])
for word, repl in ((b"facet normal", b"Facet Normal"), (b"outer loop", b"OUTER loop"), (b"vertex", b"VeRtEx"), (b"endloop", b"EndLoop"), (b"endfacet", b"endFACET")):
    body = body.replace(word, repl)
mixed = b"\n".join([lines[0], body] + lines[-2:])
assert lines[0].startswith(b"solid ") and lines[-2].startswith(b"endsolid ") and lines[-1] == b""
header = b"\n".join([b"SOLID" + lines[0][5:]] + lines[1:-2] + [b"ENDSOLID" + lines[-2][8:], b""])
for name, blob in (("upper", data.upper()), ("mixed_body", mixed), ("header_upper", header)):
    with open("%s/stl_case_%s.stl" % (out, name), "wb") as fh:
        fh.write(blob)
PY
meas_case stl_case_upper "$W/stl_case_upper.stl" stl_ascii_box_upper.stl "" unspecified none 10:20:30 6000 2200 none
meas_case stl_case_mixed_body "$W/stl_case_mixed_body.stl" stl_ascii_box_mixed.stl "" unspecified none 10:20:30 6000 2200 none
meas_case stl_case_header_upper "$W/stl_case_header_upper.stl" stl_ascii_box_header_upper.stl "" unspecified none 10:20:30 6000 2200 none
python3 - "$W/stl_hollow.stl" <<'PY' || fail "cannot generate the hollow stl"
import sys

QUADS = (
    ((0, 0, 0), (0, 1, 0), (1, 1, 0), (1, 0, 0)),
    ((0, 0, 1), (1, 0, 1), (1, 1, 1), (0, 1, 1)),
    ((0, 0, 0), (1, 0, 0), (1, 0, 1), (0, 0, 1)),
    ((0, 1, 0), (0, 1, 1), (1, 1, 1), (1, 1, 0)),
    ((0, 0, 0), (0, 0, 1), (0, 1, 1), (0, 1, 0)),
    ((1, 0, 0), (1, 1, 0), (1, 1, 1), (1, 0, 1)),
)


def cube(lo, size, inward):
    tris = []
    for quad in QUADS:
        a, b, c, d = [tuple(lo + size * k for k in v) for v in quad]
        for t in ((a, b, c), (a, c, d)):
            tris.append((t[0], t[2], t[1]) if inward else t)
    six = sum(a[0] * (b[1] * c[2] - b[2] * c[1]) - a[1] * (b[0] * c[2] - b[2] * c[0]) + a[2] * (b[0] * c[1] - b[1] * c[0]) for a, b, c in tris)
    assert six == (-6 if inward else 6) * size ** 3, (lo, size, inward, six)
    return tris


lines = ["solid hollow"]
for a, b, c in cube(0, 10, False) + cube(3, 4, True):
    lines += ["  facet normal 0 0 0", "    outer loop"]
    lines += ["      vertex %d %d %d" % v for v in (a, b, c)]
    lines += ["    endloop", "  endfacet"]
lines += ["endsolid hollow", ""]
with open(sys.argv[1], "w") as fh:
    fh.write("\n".join(lines))
PY
meas_case stl_hollow_ambiguous "$W/stl_hollow.stl" stl_hollow_cube.stl "" unspecified none 10:10:10 null null stl_ambiguous_shells

quality_one() {
	local q=$1 stl
	create_job "$CORPUS/public/LinearSlide-MGN15x-Block.step" LinearSlide-MGN15x-Block.step stl convert "{\"quality\":\"$q\"}"
	[ "$HTTP" = 202 ] && [ -n "$JOB_ID" ] || fail "QUALITY $q: create returned $HTTP: $(cat "$W/create.json")"
	wait_job "$JOB_ID" "$ENGINE_TIMEOUT"
	[ "$JOB_STATUS" = done ] || fail "QUALITY $q: job $JOB_ID ended $JOB_STATUS: $(cat "$W/job.json")"
	stl="$W/quality_$q.stl"
	download_output "$stl" || fail "QUALITY $q: no download_url on the done job"
	judge "$stl"
	[ "$JUDGE_RC" = 0 ] || fail "QUALITY $q: judge failed: $JUDGE_FIELDS"
	Q_TRI=$(mfield triangles "$JUDGE_FIELDS")
	[ -n "$Q_TRI" ] || fail "QUALITY $q: the judge reported no triangle count: $JUDGE_FIELDS"
	Q_JOB=$JOB_ID
}

quality_one draft
TRI_DRAFT=$Q_TRI
quality_one standard
TRI_STANDARD=$Q_TRI
quality_one fine
TRI_FINE=$Q_TRI
[ "$TRI_DRAFT" -le "$TRI_STANDARD" ] && [ "$TRI_STANDARD" -le "$TRI_FINE" ] && [ "$TRI_DRAFT" -lt "$TRI_FINE" ] || fail "QUALITY: triangles draft=$TRI_DRAFT standard=$TRI_STANDARD fine=$TRI_FINE are not non-decreasing and distinct at the ends"
render_job quality_fine "$CORPUS/public/LinearSlide-MGN15x-Block.step" LinearSlide-MGN15x-Block.step '{"quality":"fine"}'
render_judged quality_fine 1280 opaque
ev "QUALITY,name=LinearSlide-MGN15x-Block.step,draft=$TRI_DRAFT,standard=$TRI_STANDARD,fine=$TRI_FINE,non_decreasing=1,render_fine_job=$JOB_ID,render_fine=done,render_fine_judge=ok,result=ok"
N_QUALITY=1

reject_opts() {
	local name=$1 op=$2 target=$3 file=$4 upname=$5 opts=$6 want=$7 before after
	local args=(-F "target=$target" -F "file=@$file;filename=$upname" -F "opts=$opts")
	if [ -n "$op" ]; then
		args+=(-F "operation=$op")
	fi
	before=$(psqlq "select count(*) from jobs")
	CODE=$(http "$W/rej.json" -X POST "$API_BASE/v1/jobs" "${args[@]}")
	[ "$CODE" = 422 ] && [ "$(jq -r .code "$W/rej.json")" = "$want" ] || fail "OPTS_REJECT $name: got $CODE $(cat "$W/rej.json"), want 422 $want"
	[ "$(jq 'has("job_id")' "$W/rej.json")" = false ] || fail "OPTS_REJECT $name: the response carries a job_id"
	after=$(psqlq "select count(*) from jobs")
	[ "$before" = "$after" ] || fail "OPTS_REJECT $name: jobs rows moved $before -> $after"
	ev "OPTS_REJECT,name=$name,operation=${op:-convert},target=$target,upload=$upname,http=422,code=$want,jobs_created=0,result=ok"
	N_OPTS_REJECT=$((N_OPTS_REJECT + 1))
}

BOXSTEP="$CORPUS/synthetic/units_inch.step"
reject_opts view_unknown render png "$BOXSTEP" units_inch.step '{"view":"sideways"}' invalid_options
reject_opts size_255 render png "$BOXSTEP" units_inch.step '{"size":255}' invalid_options
reject_opts size_2049 render png "$BOXSTEP" units_inch.step '{"size":2049}' invalid_options
reject_opts size_string render png "$BOXSTEP" units_inch.step '{"size":"1280"}' invalid_options
reject_opts quality_unknown render png "$BOXSTEP" units_inch.step '{"quality":"ultra"}' invalid_options
reject_opts background_unknown render png "$BOXSTEP" units_inch.step '{"background":"green"}' invalid_options
reject_opts key_unknown render png "$BOXSTEP" units_inch.step '{"colour":"red"}' invalid_options
reject_opts render_key_on_convert convert stl "$BOXSTEP" units_inch.step '{"view":"top"}' opts_not_applicable
reject_opts quality_on_stl_render render png "$CORPUS/synthetic/stl_ascii_box.stl" stl_ascii_box.stl '{"quality":"fine"}' opts_not_applicable

python3 - "$W/empty.stl" <<'PY'
import struct, sys
body = b"\0" * 80 + struct.pack("<I", 4)
for _ in range(4):
    body += struct.pack("<12fH", 0, 0, 1, 5, 5, 5, 5, 5, 5, 5, 5, 5, 0)
open(sys.argv[1], "wb").write(body)
PY
create_job "$W/empty.stl" zero_extent.stl png render
[ "$HTTP" = 202 ] && [ -n "$JOB_ID" ] || fail "EMPTY: create returned $HTTP: $(cat "$W/create.json")"
wait_job "$JOB_ID" "$ENGINE_TIMEOUT"
[ "$JOB_STATUS" = failed ] || fail "EMPTY: job $JOB_ID ended $JOB_STATUS, want failed: $(cat "$W/job.json")"
[ "$(jq -r '.error_code // empty' "$W/job.json")" = cad_empty_result ] || fail "EMPTY: error_code is $(jq -r '.error_code // empty' "$W/job.json"), want cad_empty_result"
EMPTY_OUTPUTS=$(psqlq "select count(*) from job_outputs where job_id = '$JOB_ID'")
[ "$EMPTY_OUTPUTS" = 0 ] || fail "EMPTY: $EMPTY_OUTPUTS output rows exist"
[ "$(jq 'has("download_url") or has("outputs")' "$W/job.json")" = false ] || fail "EMPTY: the failed job exposes download_url or outputs"
ev "EMPTY,job=$JOB_ID,input=zero_extent.stl,bytes=$(wc -c < "$W/empty.stl" | tr -d ' '),status=failed,code=cad_empty_result,outputs=0,download_url=absent,result=ok"
N_EMPTY=1

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

cat > "$W/watch.sh" <<'WATCH'
echo ready
n=$1
sig=$2
i=0
while [ "$i" -lt 12000 ]; do
	for d in /proc/[0-9]*; do
		p=${d#/proc/}
		[ "$p" = "$$" ] && continue
		c=$(cat "$d/comm" 2>/dev/null) || continue
		hit=""
		case "$c" in
		"$n") hit=1 ;;
		python*) grep -qa "$n" "$d/cmdline" 2>/dev/null && hit=1 ;;
		esac
		if [ -n "$hit" ]; then
			echo "$p $c"
			kill -s "$sig" "$p"
			exit 0
		fi
	done
	i=$((i + 1))
	sleep 0.05
done
exit 3
WATCH

inject_start() {
	: > "$3"
	docker exec -i "$CNAME" sh -s "$1" "$2" < "$W/watch.sh" > "$3" 2>&1 &
	INJ_BG=$!
	INJ_OUT=$3
	for _ in $(seq 1 100); do
		grep -q '^ready' "$3" 2>/dev/null && return 0
		sleep 0.1
	done
	return 1
}

inject_wait() {
	wait "$INJ_BG" || return 1
	INJ_HIT=$(sed -n 2p "$INJ_OUT")
	INJ_HIT_PID=${INJ_HIT%% *}
	[ -n "$INJ_HIT_PID" ]
}

job_events() { psqlq "select string_agg(coalesce(from_status, '-') || '>' || to_status, ';' order by id) from job_events where job_id = '$1'"; }

LARGE_NAME=$(basename "$LARGE_STL")
LARGE_BYTES=$(wc -c < "$LARGE_STL" | tr -d ' ')

inject_start cad-render TERM "$W/inj_render.out" || fail "RENDER_RETRY: the signal watcher did not start"
render_start render_retry "$LARGE_STL" "$LARGE_NAME"
inject_wait || fail "RENDER_RETRY: no cad-render process was found to signal for job $JOB_ID"
RETRY_HIT=$INJ_HIT
render_finish render_retry
render_judged render_retry 1280 opaque
RETRY_EVENTS=$(job_events "$JOB_ID")
case ";$RETRY_EVENTS;" in *";active>active;"*) ;; *) fail "RENDER_RETRY: job $JOB_ID events '$RETRY_EVENTS' show no retry" ;; esac
ev "RENDER_RETRY,job=$JOB_ID,input=$LARGE_NAME,bytes=$LARGE_BYTES,signal=TERM,target=cad-render,hit=$(echo "$RETRY_HIT" | tr ' ' ':'),status=done,retried=1,events=$RETRY_EVENTS,seconds=$R_SECS,outputs=2,judge=ok,measurements=valid,result=ok"
N_RENDER_RETRY=1

inject_start cad-mesh TERM "$W/inj_mesh.out" || fail "MESH_SIGNAL: the signal watcher did not start"
create_job "$CORPUS/public/nist_ctc_05_asme1_ap242-e1.stp" nist_ctc_05_asme1_ap242-e1.stp png render
[ "$HTTP" = 202 ] && [ -n "$JOB_ID" ] || fail "MESH_SIGNAL: create returned $HTTP: $(cat "$W/create.json")"
inject_wait || fail "MESH_SIGNAL: no cad-mesh process was found to signal for job $JOB_ID"
MESH_HIT=$INJ_HIT
wait_job "$JOB_ID" "$ENGINE_TIMEOUT"
[ "$JOB_STATUS" = failed ] || fail "MESH_SIGNAL: job $JOB_ID ended $JOB_STATUS, want failed: $(cat "$W/job.json")"
MESH_CODE=$(jq -r '.error_code // empty' "$W/job.json")
[ "$MESH_CODE" = cad_unreadable ] || fail "MESH_SIGNAL: error_code '$MESH_CODE', want cad_unreadable (a reader that died on a signal)"
MESH_EVENTS=$(job_events "$JOB_ID")
case ";$MESH_EVENTS;" in *";active>active;"*) fail "MESH_SIGNAL: job $JOB_ID was retried: $MESH_EVENTS" ;; esac
[ "$(psqlq "select count(*) from job_outputs where job_id = '$JOB_ID'")" = 0 ] || fail "MESH_SIGNAL: output rows exist for a failed job"
ev "MESH_SIGNAL,job=$JOB_ID,input=nist_ctc_05_asme1_ap242-e1.stp,signal=TERM,target=cad-mesh,hit=$(echo "$MESH_HIT" | tr ' ' ':'),status=failed,code=$MESH_CODE,retried=0,events=$MESH_EVENTS,outputs=0,result=ok"
N_MESH_SIGNAL=1

inject_start cad-render 0 "$W/inj_cancel.out" || fail "CANCEL_RENDER: the watcher did not start"
create_job "$LARGE_STL" "$LARGE_NAME" png render
[ "$HTTP" = 202 ] && [ -n "$JOB_ID" ] || fail "CANCEL_RENDER: create returned $HTTP: $(cat "$W/create.json")"
CR_JOB=$JOB_ID
inject_wait || fail "CANCEL_RENDER: no cad-render process ever appeared for job $CR_JOB"
CR_PID=$INJ_HIT_PID
T_CANCEL=$(now)
CODE=$(http "$W/cancel.json" -X POST "$API_BASE/v1/jobs/$CR_JOB/cancel")
[ "$CODE" = 200 ] && [ "$(jq -r .status "$W/cancel.json")" = canceled ] || fail "CANCEL_RENDER: cancel returned $CODE $(cat "$W/cancel.json")"
GONE=0
for _ in $(seq 1 24); do
	case " $(render_pids) " in *" $CR_PID "*) ;; *)
		GONE=1
		break
		;;
	esac
	sleep 0.25
done
GONE_S=$(elapsed "$T_CANCEL" "$(now)")
[ "$GONE" = 1 ] && within "$GONE_S" 5 || fail "CANCEL_RENDER: cad-render pid $CR_PID still alive ${GONE_S}s after cancel"
wait_job "$CR_JOB" 30
[ "$JOB_STATUS" = canceled ] || fail "CANCEL_RENDER: job reads $JOB_STATUS, want canceled"
[ "$(jq 'has("download_url") or has("outputs")' "$W/job.json")" = false ] || fail "CANCEL_RENDER: a canceled job exposes download_url or outputs"
[ "$(psqlq "select count(*) from job_outputs where job_id = '$CR_JOB'")" = 0 ] || fail "CANCEL_RENDER: output rows exist for a canceled job"
ev "CANCEL_RENDER,job=$CR_JOB,input=$LARGE_NAME,render_pid=$CR_PID,pid_gone=1,gone_s=$GONE_S,status=canceled,download_url=absent,outputs=0,result=ok"
N_CANCEL_RENDER=1

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

DXF="$CORPUS/dxf"
DXF_OUT="$CORPUS/dxf-out"

cat > "$W/svgwalk.py" <<'PY'
import json
import re
import sys
import xml.etree.ElementTree as ET

path, out = sys.argv[1], sys.argv[2]
with open(path, "rb") as fh:
    data = fh.read()
doctype = 1 if re.search(rb"<!DOCTYPE|<!ENTITY", data, re.I) else 0
raw_script = 1 if re.search(rb"<script", data, re.I) else 0
if doctype or raw_script:
    print("root_svg=0,elements=0,forbidden_elements=0,on_attrs=0,href_attrs=0,doctype=%d,raw_script_tag=%d,svg_texts=0" % (doctype, raw_script))
    sys.exit(1)
root = ET.fromstring(data)
root_svg = 1 if root.tag.rsplit("}", 1)[-1] == "svg" else 0
SHAPES = ("path", "polyline", "polygon", "line", "circle", "ellipse", "rect")
PAINT = ("fill", "fill-opacity", "stroke", "stroke-opacity", "stroke-width")


def num(v):
    try:
        return float(v)
    except (TypeError, ValueError):
        return 0.0


def paint_of(el, parent):
    paint = dict(parent)
    for k in PAINT:
        if el.get(k) is not None:
            paint[k] = el.get(k)
    return paint


def stroked(p):
    return p["stroke"] != "none" and num(p["stroke-opacity"]) > 0 and num(p["stroke-width"]) > 0


def filled(p):
    return p["fill"] != "none" and num(p["fill-opacity"]) > 0


def has_extent(el, tag):
    if tag == "rect":
        return num(el.get("width")) > 0 and num(el.get("height")) > 0
    if tag == "circle":
        return num(el.get("r")) > 0
    if tag == "ellipse":
        return num(el.get("rx")) > 0 and num(el.get("ry")) > 0
    if tag == "path":
        return any(c in "LlQqCcZz" for c in el.get("d", ""))
    if tag in ("polyline", "polygon"):
        return len(re.findall(r"-?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][-+]?\d+)?", el.get("points", ""))) >= 4
    return True


visible = 0


def walk(el, parent, hidden):
    global visible
    tag = el.tag.rsplit("}", 1)[-1]
    paint = paint_of(el, parent)
    hidden = hidden or tag in ("defs", "clipPath")
    if not hidden and tag in SHAPES and ((filled(paint) and tag != "line") or stroked(paint)) and has_extent(el, tag):
        visible += 1
    if not hidden and tag == "text":
        pieces = [(el.text, paint)]
        for span in el:
            pieces += [(span.text, paint_of(span, paint)), (span.tail, paint)]
        if any((t or "").strip() and (filled(p) or stroked(p)) for t, p in pieces):
            visible += 1
    for child in el:
        walk(child, paint, hidden)


walk(root, {"fill": "#000000", "fill-opacity": "1", "stroke": "none", "stroke-opacity": "1", "stroke-width": "1"}, False)
elements = forbidden = on_attrs = href_attrs = 0
texts = []
for el in root.iter():
    elements += 1
    local = el.tag.rsplit("}", 1)[-1]
    if local in ("script", "foreignObject", "style", "use", "image"):
        forbidden += 1
    for key in el.attrib:
        name = key.rsplit("}", 1)[-1].lower()
        if name.startswith("on"):
            on_attrs += 1
        if name == "href":
            href_attrs += 1
    if local == "text":
        texts.append("".join(el.itertext()))
with open(out, "w", encoding="utf-8") as fh:
    json.dump(texts, fh, ensure_ascii=False)
bad = forbidden or on_attrs or href_attrs or doctype or raw_script or not root_svg
print("root_svg=%d,elements=%d,forbidden_elements=%d,on_attrs=%d,href_attrs=%d,doctype=%d,raw_script_tag=%d,svg_texts=%d,svg_visible=%d" % (
    root_svg, elements, forbidden, on_attrs, href_attrs, doctype, raw_script, len(texts), visible))
sys.exit(1 if bad else 0)
PY

cat > "$W/svgalpha.py" <<'PY'
import sys
import xml.etree.ElementTree as ET

want = [
    ("OPAQUE TEXT", "#000000", None), ("HALF TEXT", "#000000", "0.498"), ("CLEAR TEXT", "#000000", "0.000"),
    ("OPAQUE MTEXT", "#000000", None), ("HALF MTEXT", "#000000", "0.498"), ("CLEAR MTEXT", "#000000", "0.000"),
    ("HALF", "#000000", "0.498"), ("SPLIT", "#000000", "0.498"), ("INLINE", "#ff0000", "0.498"), ("RED", "#ff0000", "0.498"),
    ("CLEAR", "#000000", "0.000"), ("SPLIT", "#000000", "0.000"),
    ("LAYER TEXT", "#000000", "0.498"), ("BLOCK TEXT", "#000000", "0.498"),
]
ns = "{http://www.w3.org/2000/svg}"
root = ET.parse(sys.argv[1]).getroot()
got = [("".join(el.itertext()), el.get("fill"), el.get("fill-opacity")) for el in root.iter(ns + "text")]
strokes = sum(1 for el in root.iter(ns + "path") if el.get("stroke-opacity") == "0.498" and el.get("fill") == "none")
fills = sum(1 for el in root.iter(ns + "path") if el.get("fill-opacity") == "0.498" and el.get("stroke") is None)
ok = got == want and strokes == 2 and fills == 1
print("texts_opaque=%d,texts_half=%d,texts_clear=%d,texts_as_expected=%d,stroke_opacity_half=%d,fill_opacity_half=%d" % (
    sum(1 for g in got if g[2] is None), sum(1 for g in got if g[2] == "0.498"),
    sum(1 for g in got if g[2] == "0.000"), int(got == want), strokes, fills))
sys.exit(0 if ok else 1)
PY

cat > "$W/svgfonts.py" <<'PY'
import sys
import xml.etree.ElementTree as ET

want = [
    ("MONO BOOK", "DejaVu Sans Mono", None), ("MONO BOLD", "DejaVu Sans Mono", "bold"),
    ("MONO OBLIQUE", "DejaVu Sans", None), ("MONO BOLD OBLIQUE", "DejaVu Sans", None),
    ("SANS BOOK", "DejaVu Sans", None), ("SANS BOLD", "DejaVu Sans", "bold"),
    ("SERIF BOOK", "DejaVu Serif", None), ("SERIF BOLD", "DejaVu Serif", "bold"),
    ("XDATA MONO ITALIC", "DejaVu Sans Mono", None), ("XDATA MONO BOLD ITALIC", "DejaVu Sans Mono", None),
    ("INLINE", "DejaVu Sans Mono", None), ("MONO", "DejaVu Sans Mono", None), ("BOLD", "DejaVu Sans Mono", None),
    ("ITALIC", "DejaVu Sans Mono", None), ("SHEARED STYLE", "DejaVu Sans Mono", None), ("SHEARED TEXT", "DejaVu Sans Mono", None),
]
ns = "{http://www.w3.org/2000/svg}"
root = ET.parse(sys.argv[1]).getroot()
texts = list(root.iter(ns + "text"))
got = [("".join(el.itertext()), el.get("font-family"), el.get("font-weight")) for el in texts]
slanted = sum(1 for el in texts if el.get("font-style") is not None)
stretched = sum(1 for el in texts if el.get("font-stretch") is not None)
tspans = sum(1 for _ in root.iter(ns + "tspan"))
ok = got == want and slanted == 0 and stretched == 0 and tspans == 0
print("font_texts=%d,fonts_as_expected=%d,bold=%d,font_style=%d,font_stretch=%d,tspans=%d" % (
    len(got), int(got == want), sum(1 for g in got if g[2] == "bold"), slanted, stretched, tspans))
sys.exit(0 if ok else 1)
PY

fetch_ct() {
	docker compose exec -T cad-worker /usr/bin/python3 -c \
		'import sys, urllib.request; r = urllib.request.urlopen(sys.argv[1]); r.read(); print(r.headers.get("Content-Type", ""))' "$1"
}

dxf_convert() {
	local label=$1 file=$2 upname=$3 target=$4 out=$5 facts n primary size fmt
	DC_T0=$(now)
	create_job "$file" "$upname" "$target"
	[ "$HTTP" = 202 ] && [ -n "$JOB_ID" ] || fail "$label: create returned $HTTP: $(cat "$W/create.json")"
	wait_job "$JOB_ID" "$ENGINE_TIMEOUT"
	DC_SECS=$(elapsed "$DC_T0" "$(now)")
	[ "$JOB_STATUS" = done ] || fail "$label: job $JOB_ID ended $JOB_STATUS: $(cat "$W/job.json")"
	facts=$(output_facts "$JOB_ID")
	n=${facts%%,*}
	primary=$(echo "$facts" | cut -d, -f2)
	size=$(echo "$facts" | cut -d, -f3)
	fmt=$(echo "$facts" | cut -d, -f4)
	[ "$n" = 1 ] && [ "$primary" = 1 ] && [ "$fmt" = "$target" ] || fail "$label: job_outputs facts '$facts', want exactly one primary $target"
	[ "$(jq 'has("outputs")' "$W/job.json")" = false ] || fail "$label: the convert response carries an outputs array"
	DC_URL=$(jq -r '.download_url // empty' "$W/job.json")
	[ -n "$DC_URL" ] || fail "$label: no download_url on the done job"
	fetch_url "$DC_URL" "$out" || fail "$label: cannot fetch the download_url"
	[ "$(wc -c < "$out" | tr -d ' ')" = "$size" ] || fail "$label: downloaded $(wc -c < "$out" | tr -d ' ') bytes, job_outputs says $size"
	DC_CT=$(fetch_ct "$DC_URL") || fail "$label: cannot read the content type of the download_url"
	DC_JOB=$JOB_ID
	DC_BYTES=$size
}

svg_judged() {
	local label=$1 svg=$2 line rc=0
	line=$("$W/bin/cad-svg-check" --out "$svg.reser" "$svg") || rc=$?
	[ "$rc" = 0 ] || fail "$label: cad-svg-check rejected the downloaded SVG: $line"
	SV_TEXTS=$(mfield texts "$line")
	SV_VISIBLE=$(mfield visible "$line")
	SV_SIZE="$(mfield width_mm "$line")x$(mfield height_mm "$line")"
	cmp -s "$svg" "$svg.reser" || fail "$label: the re-serialization of the downloaded SVG differs from the download"
	rc=0
	SV_WALK=$(python3 "$W/svgwalk.py" "$svg" "$svg.texts.json") || rc=$?
	[ "$rc" = 0 ] || fail "$label: the downloaded SVG holds a forbidden construct: $SV_WALK"
	[ "$(mfield svg_texts "$SV_WALK")" = "$SV_TEXTS" ] || fail "$label: the Go validator counts $SV_TEXTS texts, the Python walk $(mfield svg_texts "$SV_WALK")"
	[ "$(mfield svg_visible "$SV_WALK")" = "$SV_VISIBLE" ] || fail "$label: the Go validator counts $SV_VISIBLE visible elements, the Python walk $(mfield svg_visible "$SV_WALK")"
	[ "$SV_VISIBLE" -ge 1 ] || fail "$label: the downloaded SVG shows nothing: $line"
}

dxf_svg_one() {
	local name=$1 file=$2 upname=$3 texts_check
	dxf_convert "DXF_SVG $name" "$file" "$upname" svg "$W/dxf_$name.svg"
	[ "$DC_CT" = image/svg+xml ] || fail "DXF_SVG $name: the download content type is '$DC_CT', want image/svg+xml"
	svg_judged "DXF_SVG $name" "$W/dxf_$name.svg"
	if [ -f "$DXF_OUT/$name.texts.json" ]; then
		[ "$SV_TEXTS" = "$(jq '.texts | length' "$DXF_OUT/$name.texts.json")" ] || fail "DXF_SVG $name: $SV_TEXTS texts, the committed helper output has $(jq '.texts | length' "$DXF_OUT/$name.texts.json")"
	else
		[ "$SV_TEXTS" -ge 1 ] || fail "DXF_SVG $name: no text in the SVG"
	fi
	case "$name" in
	legacy_cp1251)
		jq -e 'any(.[]; contains("Деталь"))' "$W/dxf_$name.svg.texts.json" >/dev/null || fail "DXF_SVG $name: no text holds Деталь: $(cat "$W/dxf_$name.svg.texts.json")"
		texts_check="detal_cp1251=present"
		;;
	text_cases)
		jq -e --arg s "$DXF_SPECIAL" 'any(.[]; . == $s)' "$W/dxf_$name.svg.texts.json" >/dev/null || fail "DXF_SVG $name: no text equals the special string exactly: $(cat "$W/dxf_$name.svg.texts.json")"
		texts_check="special_text=exact"
		;;
	layers_off_frozen)
		jq -e 'any(.[]; contains("VISIBLE"))' "$W/dxf_$name.svg.texts.json" >/dev/null || fail "DXF_SVG $name: VISIBLE is missing"
		! grep -q -e HIDDEN_OFF -e HIDDEN_FROZEN "$W/dxf_$name.svg" || fail "DXF_SVG $name: a hidden layer's text is in the SVG"
		texts_check="visible=present,hidden_off=absent,hidden_frozen=absent"
		;;
	uncommon)
		[ "$SV_TEXTS" -ge 40 ] || fail "DXF_SVG $name: $SV_TEXTS texts, want at least 40"
		texts_check="texts_at_least_40=1"
		;;
	text_alpha)
		texts_check=$(python3 "$W/svgalpha.py" "$W/dxf_$name.svg") || fail "DXF_SVG $name: the opacities are not the DXF transparencies: $texts_check"
		;;
	text_fonts)
		texts_check=$(python3 "$W/svgfonts.py" "$W/dxf_$name.svg") || fail "DXF_SVG $name: the font attributes are not those of the measured faces: $texts_check"
		;;
	white_one_black)
		[ "$(grep -o '"#ffffff"' "$W/dxf_$name.svg" | wc -l | tr -d ' ')" -ge 10 ] && [ "$(grep -o 'stroke="#000000"' "$W/dxf_$name.svg" | wc -l | tr -d ' ')" = 1 ] || fail "DXF_SVG $name: want the white geometry and texts kept and one black stroke"
		[ "$SV_VISIBLE" -ge 11 ] || fail "DXF_SVG $name: $SV_VISIBLE visible elements, want the white ones counted too"
		texts_check="white_paints_kept=1,black_strokes=1,visible_elements=$SV_VISIBLE"
		;;
	alpha_one_visible)
		[ "$SV_VISIBLE" = 1 ] || fail "DXF_SVG $name: $SV_VISIBLE visible elements, want exactly the one 50% line"
		[ "$(grep -o 'stroke-opacity="0.000"' "$W/dxf_$name.svg" | wc -l | tr -d ' ')" = 8 ] && [ "$(grep -o 'fill-opacity="0.000"' "$W/dxf_$name.svg" | wc -l | tr -d ' ')" = 4 ] || fail "DXF_SVG $name: the fully transparent strokes, fills and texts are not all kept"
		[ "$(grep -o 'stroke-opacity="0.498"' "$W/dxf_$name.svg" | wc -l | tr -d ' ')" = 1 ] || fail "DXF_SVG $name: the 50% line is not the one half-transparent stroke"
		texts_check="visible_elements=1,clear_strokes=8,clear_fills_and_texts=4,half_strokes=1"
		;;
	*)
		texts_check="texts_checked=none"
		;;
	esac
	ev "DXF_SVG,name=$name,upload=$upname,job=$DC_JOB,seconds=$DC_SECS,outputs=1,bytes=$DC_BYTES,sha256=$(shasum -a 256 "$W/dxf_$name.svg" | cut -d' ' -f1),content_type=$DC_CT,svg_check=ok,reserialized_identical=1,texts=$SV_TEXTS,page_mm=$SV_SIZE,$SV_WALK,$texts_check,result=ok"
	N_DXF_SVG=$((N_DXF_SVG + 1))
}

dxf_svg_one legacy_cp1251 "$CORPUS/synthetic/legacy_cp1251.dxf" legacy_cp1251.dxf
dxf_svg_one text_cases "$DXF/text_cases.dxf" text_cases.dxf
dxf_svg_one layers_off_frozen "$DXF/layers_off_frozen.dxf" layers_off_frozen.dxf
dxf_svg_one never_resolved "$DXF/never_resolved.dxf" never_resolved.dxf
dxf_svg_one uncommon "$CORPUS/public/uncommon.dxf" uncommon.dxf
dxf_svg_one text_alpha "$DXF/text_alpha.dxf" text_alpha.dxf
dxf_svg_one text_fonts "$DXF/text_fonts.dxf" text_fonts.dxf
dxf_svg_one alpha_one_visible "$DXF/alpha_one_visible.dxf" alpha_one_visible.dxf
dxf_svg_one white_one_black "$DXF/white_one_black.dxf" white_one_black.dxf

pdf_judged() {
	local label=$1 pdf=$2 line rc=0
	shift 2
	[ "$(head -c 5 "$pdf")" = "%PDF-" ] || fail "$label: the PDF does not start with %PDF-"
	PD_PAGES=$(pdfinfo "$pdf" 2>/dev/null | awk '/^Pages:/ { print $2 }')
	[ "$PD_PAGES" = 1 ] || fail "$label: pdfinfo reports '$PD_PAGES' pages, want 1"
	line=$("$W/bin/cad-pdf-check" "$@" "$pdf") || rc=$?
	[ "$rc" = 0 ] || fail "$label: cad-pdf-check rejected the PDF: $line"
	PD_LINE=$(printf '%s' "$line" | sed -e 's/^GOPDF,pdf=[^,]*,//' -e 's/,reason=none$//')
	pdftotext -raw -enc UTF-8 "$pdf" "$pdf.txt" || fail "$label: pdftotext failed"
}

dxf_pdf_one() {
	local name=$1 file=$2 upname=$3 invisible=$4 text_check fonts
	dxf_convert "DXF_PDF $name" "$file" "$upname" pdf "$W/dxf_$name.pdf"
	[ "$DC_CT" = application/pdf ] || fail "DXF_PDF $name: the download content type is '$DC_CT', want application/pdf"
	pdf_judged "DXF_PDF $name" "$W/dxf_$name.pdf" --texts "$DXF_OUT/$name.texts.json" --svg "$DXF_OUT/$name.render.svg"
	if [ "$name" = alpha_one_visible ]; then
		[ "$(mfield expected_runes "$PD_LINE")" = 0 ] || fail "DXF_PDF $name: the judge expected text from a drawing whose texts are all fully transparent: $PD_LINE"
	else
		[ "$(mfield expected_runes "$PD_LINE")" -gt 0 ] || fail "DXF_PDF $name: the judge expected no text: $PD_LINE"
	fi
	[ "$(mfield ink_ops "$PD_LINE")" -ge 2 ] || fail "DXF_PDF $name: the judge saw no ink beyond the background: $PD_LINE"
	[ "$(mfield invisible_text "$PD_LINE")" = "$invisible" ] || fail "DXF_PDF $name: invisible_text=$(mfield invisible_text "$PD_LINE"), want $invisible: $PD_LINE"
	case "$name" in
	legacy_cp1251)
		grep -qF "Деталь" "$W/dxf_$name.pdf.txt" || fail "DXF_PDF $name: pdftotext -raw holds no Деталь: $(head -c 300 "$W/dxf_$name.pdf.txt")"
		text_check="pdftotext_detal_cp1251=present"
		;;
	text_cases)
		grep -qF -- "$DXF_SPECIAL" "$W/dxf_$name.pdf.txt" || fail "DXF_PDF $name: pdftotext -raw does not hold the special string: $(head -c 600 "$W/dxf_$name.pdf.txt")"
		text_check="pdftotext_special_text=present"
		;;
	text_alpha)
		for s in "OPAQUE TEXT" "HALF TEXT" "OPAQUE MTEXT" "HALF MTEXT" "LAYER TEXT" "BLOCK TEXT" "INLINE" "RED"; do
			grep -qF -- "$s" "$W/dxf_$name.pdf.txt" || fail "DXF_PDF $name: pdftotext -raw does not hold '$s': $(head -c 600 "$W/dxf_$name.pdf.txt")"
		done
		[ "$(grep -c CLEAR "$W/dxf_$name.pdf.txt" || true)" = 0 ] || fail "DXF_PDF $name: pdftotext -raw holds a fully transparent text: $(grep CLEAR "$W/dxf_$name.pdf.txt" | head -3)"
		text_check="pdftotext_opaque_and_half_texts=8,pdftotext_clear_lines=0"
		;;
	alpha_one_visible)
		[ "$(mfield ink_ops "$PD_LINE")" = 2 ] && [ "$(mfield paint_ops "$PD_LINE")" = 2 ] || fail "DXF_PDF $name: want the background and the 50% line as the only paint and ink: $PD_LINE"
		[ "$(grep -c '[[:alnum:]]' "$W/dxf_$name.pdf.txt" || true)" = 0 ] || fail "DXF_PDF $name: pdftotext -raw holds a fully transparent text: $(head -c 300 "$W/dxf_$name.pdf.txt")"
		text_check="pdftotext_text=empty"
		;;
	white_one_black)
		for s in "WHITE TEXT" "WHITE MTEXT"; do
			grep -qF -- "$s" "$W/dxf_$name.pdf.txt" || fail "DXF_PDF $name: pdftotext -raw does not hold the white text '$s': $(head -c 300 "$W/dxf_$name.pdf.txt")"
		done
		text_check="pdftotext_white_texts=2"
		;;
	text_alpha_faint)
		grep -qF "FAINT ONLY" "$W/dxf_$name.pdf.txt" || fail "DXF_PDF $name: pdftotext -raw does not hold FAINT ONLY: $(head -c 300 "$W/dxf_$name.pdf.txt")"
		[ "$(grep -c GONE "$W/dxf_$name.pdf.txt" || true)" = 0 ] || fail "DXF_PDF $name: pdftotext -raw holds the fully transparent GONE: $(grep GONE "$W/dxf_$name.pdf.txt" | head -3)"
		text_check="pdftotext_faint_only=1,pdftotext_gone_lines=0"
		;;
	text_fonts)
		for s in "MONO BOOK" "MONO BOLD" "MONO OBLIQUE" "MONO BOLD OBLIQUE" "SANS BOOK" "SANS BOLD" "SERIF BOOK" "SERIF BOLD" \
			"XDATA MONO ITALIC" "XDATA MONO BOLD ITALIC" "INLINE" "ITALIC" "SHEARED STYLE" "SHEARED TEXT"; do
			grep -qF -- "$s" "$W/dxf_$name.pdf.txt" || fail "DXF_PDF $name: pdftotext -raw does not hold '$s': $(head -c 600 "$W/dxf_$name.pdf.txt")"
		done
		fonts=$(pdffonts "$W/dxf_$name.pdf" | awk 'NR > 2 { sub(/^[A-Z][A-Z][A-Z][A-Z][A-Z][A-Z]\+/, "", $1); print $1 }' | sort | paste -sd ';' -)
		[ "$fonts" = "DejaVuSans;DejaVuSans-Bold;DejaVuSansMono;DejaVuSansMono-Bold;DejaVuSerif;DejaVuSerif-Bold" ] || fail "DXF_PDF $name: pdffonts lists '$fonts', want exactly the six faces of fonts-dejavu-core"
		text_check="pdftotext_font_texts=14,pdffonts=$fonts,oblique_faces=0"
		;;
	*)
		[ -s "$W/dxf_$name.pdf.txt" ] && grep -q '[[:alnum:]]' "$W/dxf_$name.pdf.txt" || fail "DXF_PDF $name: pdftotext -raw extracts no text"
		text_check="pdftotext_text=nonempty"
		;;
	esac
	ev "DXF_PDF,name=$name,upload=$upname,job=$DC_JOB,seconds=$DC_SECS,outputs=1,bytes=$DC_BYTES,sha256=$(shasum -a 256 "$W/dxf_$name.pdf" | cut -d' ' -f1),content_type=$DC_CT,starts_pdf=1,pdfinfo_pages=$PD_PAGES,pdf_check=ok,$PD_LINE,pdftotext_chars=$(wc -m < "$W/dxf_$name.pdf.txt" | tr -d ' '),$text_check,result=ok"
	N_DXF_PDF=$((N_DXF_PDF + 1))
}

dxf_pdf_one legacy_cp1251 "$CORPUS/synthetic/legacy_cp1251.dxf" legacy_cp1251.dxf 0
dxf_pdf_one text_cases "$DXF/text_cases.dxf" text_cases.dxf 0
dxf_pdf_one uncommon "$CORPUS/public/uncommon.dxf" uncommon.dxf 0
dxf_pdf_one text_alpha "$DXF/text_alpha.dxf" text_alpha.dxf 4
dxf_pdf_one text_alpha_faint "$DXF/text_alpha_faint.dxf" text_alpha_faint.dxf 1
dxf_pdf_one text_fonts "$DXF/text_fonts.dxf" text_fonts.dxf 0
dxf_pdf_one alpha_one_visible "$DXF/alpha_one_visible.dxf" alpha_one_visible.dxf 2
dxf_pdf_one white_one_black "$DXF/white_one_black.dxf" white_one_black.dxf 0

drawing_judge() {
	local png=$1 size=$2 background=$3 svg=$4 line rc=0
	line=$("$W/bin/cad-pixel-judge" --size "$size" --background "$background" --drawing --letterbox-svg "$svg" "$png" 2>"$W/judge.err") || rc=$?
	DJ_LINE=$(printf '%s' "$line" | sed -e 's/^GOJUDGE,png=[^,]*,//')
	if [ "$rc" != 0 ]; then
		DJ_LINE="$DJ_LINE,err=$(tr '\n' ' ' < "$W/judge.err" | sed 's/,/;/g')"
	fi
	DJ_RC=$rc
}

lb_svg() {
	local name=$1 file=$2 upname=$3
	LB_SVG="$W/lb_$name.svg"
	if [ ! -s "$LB_SVG" ]; then
		dxf_convert "letterbox svg $name" "$file" "$upname" svg "$LB_SVG"
		svg_judged "letterbox svg $name" "$LB_SVG"
		echo "$DC_JOB" > "$LB_SVG.job"
	fi
	LB_JOB=$(cat "$LB_SVG.job")
}

dxf_meas_2d() {
	local label=$1 json=$2
	meas_check "$json"
	[ "$MC_RC" = 0 ] || fail "$label: the measurements body failed Validate: $MC_LINE"
	[ "$(mfield format "$MC_LINE")" = dxf ] || fail "$label: measurements format $(mfield format "$MC_LINE"), want dxf"
	jq -e '(.bbox.min | length) == 2 and (.bbox.max | length) == 2 and .volume == null and .area == null and .volume_null_reason == "not_applicable_2d" and .area_null_reason == "not_applicable_2d"' "$json" >/dev/null || fail "$label: the measurements body is not a 2D bbox with null volume and area for not_applicable_2d: $(cat "$json")"
	MB_BBOX=$(jq -r '[.bbox.min[0], .bbox.min[1], .bbox.max[0], .bbox.max[1]] | map(tostring) | join(":")' "$json")
}

dxf_render_one() {
	local name=$1 file=$2 upname=$3 size=$4 background=$5 opts=$6
	lb_svg "$name" "$file" "$upname"
	render_job "dxf_$name" "$file" "$upname" "$opts"
	drawing_judge "$R_PNG" "$size" "$background" "$LB_SVG"
	[ "$DJ_RC" = 0 ] || fail "DXF_RENDER $name: the drawing judge rejected the PNG: $DJ_LINE"
	[ "$(mfield w "$DJ_LINE")" = "$size" ] && [ "$(mfield h "$DJ_LINE")" = "$size" ] || fail "DXF_RENDER $name: the PNG is $(mfield w "$DJ_LINE") x $(mfield h "$DJ_LINE"), want $size x $size"
	if [ "$background" = transparent ]; then
		[ "$(mfield alpha "$DJ_LINE")" = 1 ] || fail "DXF_RENDER $name: the transparent render carries no alpha channel: $DJ_LINE"
	fi
	dxf_meas_2d "DXF_RENDER $name" "$R_JSON"
	ev "DXF_RENDER,name=$name,upload=$upname,size=$size,background=$background,opts=${opts:-none},job=$JOB_ID,seconds=$R_SECS,outputs=2,roles=primary@0;measurements@1,png_bytes=$(wc -c < "$R_PNG" | tr -d ' '),png_sha256=$(shasum -a 256 "$R_PNG" | cut -d' ' -f1),letterbox_svg_job=$LB_JOB,judge=drawing_ok,$DJ_LINE,measurements=valid,$MC_LINE,bbox=$MB_BBOX,coords_per_corner=2,result=ok"
	N_DXF_RENDER=$((N_DXF_RENDER + 1))
}

dxf_render_one wide "$DXF/wide.dxf" wide.dxf 1280 opaque ""
dxf_render_one tall "$DXF/tall.dxf" tall.dxf 1280 opaque ""
dxf_render_one wide "$DXF/wide.dxf" wide.dxf 2048 transparent '{"size":2048,"background":"transparent"}'
dxf_render_one legacy_cp1251 "$CORPUS/synthetic/legacy_cp1251.dxf" legacy_cp1251.dxf 256 opaque '{"size":256}'
dxf_render_one text_alpha "$DXF/text_alpha.dxf" text_alpha.dxf 1280 opaque ""
dxf_render_one text_alpha_faint "$DXF/text_alpha_faint.dxf" text_alpha_faint.dxf 1280 opaque ""
dxf_render_one text_alpha_faint "$DXF/text_alpha_faint.dxf" text_alpha_faint.dxf 1280 transparent '{"background":"transparent"}'
dxf_render_one text_fonts "$DXF/text_fonts.dxf" text_fonts.dxf 1280 opaque ""
dxf_render_one alpha_one_visible "$DXF/alpha_one_visible.dxf" alpha_one_visible.dxf 1280 opaque ""
dxf_render_one white_one_black "$DXF/white_one_black.dxf" white_one_black.dxf 1280 opaque ""

dxf_meas_one() {
	local name=$1 file=$2 upname=$3 assume=$4 w_unit=$5 w_src=$6 w_bbox=$7 opts="" i extra=""
	if [ -n "$assume" ]; then
		opts="{\"assume_unit\":\"$assume\"}"
	fi
	render_job "dxf_meas_$name" "$file" "$upname" "$opts"
	local job=$JOB_ID
	dxf_meas_2d "DXF_MEAS $name" "$R_JSON"
	if [ "$w_unit" != any ]; then
		[ "$(mfield unit "$MC_LINE")" = "$w_unit" ] || fail "DXF_MEAS $name: unit $(mfield unit "$MC_LINE"), want $w_unit"
		[ "$(mfield unit_source "$MC_LINE")" = "$w_src" ] || fail "DXF_MEAS $name: unit_source $(mfield unit_source "$MC_LINE"), want $w_src"
	fi
	case "$w_bbox" in
	any) ;;
	far)
		awk -v x="$(echo "$MB_BBOX" | cut -d: -f3)" 'BEGIN { exit !(x >= 100000) }' || fail "DXF_MEAS $name: bbox $MB_BBOX, want max x at least 1e5"
		lb_svg "$name" "$file" "$upname"
		drawing_judge "$R_PNG" 1280 opaque "$LB_SVG"
		[ "$DJ_RC" = 0 ] || fail "DXF_MEAS $name: the drawing judge rejected the PNG: $DJ_LINE"
		extra=",max_x_at_least_1e5=1,letterbox_svg_job=$LB_JOB,judge=drawing_ok,$DJ_LINE"
		;;
	*)
		for i in 1 2 3 4; do
			near "$(echo "$MB_BBOX" | cut -d: -f$i)" "$(echo "$w_bbox" | cut -d: -f$i)" || fail "DXF_MEAS $name: bbox $MB_BBOX, want $w_bbox"
		done
		extra=",expected_bbox=$w_bbox"
		;;
	esac
	ev "DXF_MEAS,name=$name,upload=$upname,assume_unit=${assume:-none},job=$job,outputs=2,$MC_LINE,bbox=$MB_BBOX,expected_unit=$w_unit,expected_unit_source=$w_src$extra,result=ok"
	N_DXF_MEAS=$((N_DXF_MEAS + 1))
}

dxf_meas_one wrong_extents "$DXF/wrong_extents.dxf" wrong_extents.dxf "" mm declared 10:20:40:30
dxf_meas_one units_1 "$DXF/units_1.dxf" units_1.dxf "" inch declared any
dxf_meas_one units_13 "$DXF/units_13.dxf" units_13.dxf "" micrometre declared any
dxf_meas_one units_0 "$DXF/units_0.dxf" units_0.dxf "" unspecified none any
dxf_meas_one units_0_assume_mm "$DXF/units_0.dxf" units_0.dxf mm mm client_assumed any
dxf_meas_one units_3_assume_mm "$DXF/units_3.dxf" units_3.dxf mm unspecified none any
dxf_meas_one far_point "$DXF/far_point.dxf" far_point.dxf "" any any far

dxf_reject_opts() {
	local name=$1 op=$2 target=$3 opts=$4 want=$5 before after
	local args=(-F "target=$target" -F "file=@$CORPUS/synthetic/legacy_cp1251.dxf;filename=legacy_cp1251.dxf" -F "opts=$opts")
	if [ -n "$op" ]; then
		args+=(-F "operation=$op")
	fi
	before=$(psqlq "select count(*) from jobs")
	CODE=$(http "$W/rej.json" -X POST "$API_BASE/v1/jobs" "${args[@]}")
	[ "$CODE" = 422 ] && [ "$(jq -r .code "$W/rej.json")" = "$want" ] || fail "DXF_OPTS_REJECT $name: got $CODE $(cat "$W/rej.json"), want 422 $want"
	[ "$(jq 'has("job_id")' "$W/rej.json")" = false ] || fail "DXF_OPTS_REJECT $name: the response carries a job_id"
	after=$(psqlq "select count(*) from jobs")
	[ "$before" = "$after" ] || fail "DXF_OPTS_REJECT $name: jobs rows moved $before -> $after"
	ev "DXF_OPTS_REJECT,name=$name,operation=${op:-convert},target=$target,upload=legacy_cp1251.dxf,opts=$(printf '%s' "$opts" | sed 's/,/;/g'),http=422,code=$want,jobs_created=0,result=ok"
	N_DXF_OPTS_REJECT=$((N_DXF_OPTS_REJECT + 1))
}

dxf_reject_opts render_view_top render png '{"view":"top"}' opts_not_applicable
dxf_reject_opts render_quality_fine render png '{"quality":"fine"}' opts_not_applicable
dxf_reject_opts render_view_sideways render png '{"view":"sideways"}' invalid_options
dxf_reject_opts convert_svg_size_1024 "" svg '{"size":1024}' opts_not_applicable

container_counters
DXF_BASE_RESTARTS=$C_RESTARTS
DXF_BASE_STARTED=$C_STARTED
DXF_BASE_OOMKILL=$C_OOMKILL
[ "$C_OOMKILLED" = false ] || fail "the cad-worker was OOMKilled before the DXF bombs"

dxf_bomb_one() {
	local file=$1 upname=$2 want=$3 t0 secs code events n
	t0=$(now)
	create_job "$file" "$upname" svg
	[ "$HTTP" = 202 ] && [ -n "$JOB_ID" ] || fail "DXF_BOMB $upname: create returned $HTTP: $(cat "$W/create.json")"
	wait_job "$JOB_ID" "$ENGINE_TIMEOUT"
	secs=$(elapsed "$t0" "$(now)")
	[ "$JOB_STATUS" = failed ] || fail "DXF_BOMB $upname: job $JOB_ID ended $JOB_STATUS, want failed: $(cat "$W/job.json")"
	code=$(jq -r '.error_code // empty' "$W/job.json")
	[ "$code" = "$want" ] || fail "DXF_BOMB $upname: error_code '$code', want $want: $(cat "$W/job.json")"
	events=$(job_events "$JOB_ID")
	case ";$events;" in *";active>active;"*) fail "DXF_BOMB $upname: job $JOB_ID was retried: $events" ;; esac
	n=$(psqlq "select count(*) from job_outputs where job_id = '$JOB_ID'")
	[ "$n" = 0 ] || fail "DXF_BOMB $upname: $n output rows exist"
	[ "$(jq 'has("download_url") or has("outputs")' "$W/job.json")" = false ] || fail "DXF_BOMB $upname: the failed job exposes download_url or outputs"
	container_counters
	[ "$C_OOMKILLED" = false ] || fail "DXF_BOMB $upname: the cad-worker container was OOMKilled"
	[ "$C_RESTARTS" = "$DXF_BASE_RESTARTS" ] || fail "DXF_BOMB $upname: RestartCount moved $DXF_BASE_RESTARTS -> $C_RESTARTS"
	[ "$C_STARTED" = "$DXF_BASE_STARTED" ] || fail "DXF_BOMB $upname: StartedAt moved"
	[ "$C_OOMKILL" = "$DXF_BASE_OOMKILL" ] || fail "DXF_BOMB $upname: memory.events oom_kill moved $DXF_BASE_OOMKILL -> $C_OOMKILL"
	ev "DXF_BOMB,name=$upname,bytes=$(wc -c < "$file" | tr -d ' '),sha256=$(shasum -a 256 "$file" | cut -d' ' -f1),job=$JOB_ID,status=failed,code=$code,retried=0,events=$events,outputs=0,download_url=absent,seconds=$secs,oom_killed=false,restart_count=$C_RESTARTS,started_at_unchanged=1,oom_kill=$C_OOMKILL,result=ok"
	N_DXF_BOMB=$((N_DXF_BOMB + 1))
}

for b in minsert_over_inserts entities_over dimension_minsert cyclic_self cyclic_ab cyclic_case nest_d17; do
	dxf_bomb_one "$DXF/$b.dxf" "$b.dxf" cad_too_complex
done
for b in nonnumeric_abc nonnumeric_nan nonnumeric_inf nonnumeric_underscore coord_1e400; do
	dxf_bomb_one "$DXF/$b.dxf" "$b.dxf" cad_unreadable
done
dxf_bomb_one "$CORPUS/public/3dsolids.dxf" 3dsolids.dxf cad_empty_result
dxf_bomb_one "$DXF/all_hidden.dxf" all_hidden.dxf cad_empty_result
dxf_bomb_one "$DXF/single_point.dxf" single_point.dxf cad_empty_result

dxf_empty_one() {
	local name=$1 target=$2 op=$3 opts=${4:-} want_msg code msg events n
	create_job "$DXF/$name.dxf" "$name.dxf" "$target" "$op" "$opts"
	[ "$HTTP" = 202 ] && [ -n "$JOB_ID" ] || fail "DXF_EMPTY $name $target: create returned $HTTP: $(cat "$W/create.json")"
	wait_job "$JOB_ID" "$ENGINE_TIMEOUT"
	[ "$JOB_STATUS" = failed ] || fail "DXF_EMPTY $name $target: job $JOB_ID ended $JOB_STATUS, want failed: $(cat "$W/job.json")"
	code=$(jq -r '.error_code // empty' "$W/job.json")
	[ "$code" = cad_empty_result ] || fail "DXF_EMPTY $name $target: error_code '$code', want cad_empty_result: $(cat "$W/job.json")"
	msg=$(jq -r '.error_message // empty' "$W/job.json")
	want_msg="the drawing shows nothing on a white page: model space is empty or hidden, holds only 3D solids, or everything in it is fully transparent, white, too faint or too small to see"
	[ "$msg" = "$want_msg" ] || fail "DXF_EMPTY $name $target: message '$msg', want '$want_msg': $(cat "$W/job.json")"
	events=$(job_events "$JOB_ID")
	case ";$events;" in *";active>active;"*) fail "DXF_EMPTY $name $target: job $JOB_ID was retried: $events" ;; esac
	n=$(psqlq "select count(*) from job_outputs where job_id = '$JOB_ID'")
	[ "$n" = 0 ] || fail "DXF_EMPTY $name $target: $n output rows exist"
	[ "$(jq 'has("download_url") or has("outputs")' "$W/job.json")" = false ] || fail "DXF_EMPTY $name $target: the failed job exposes download_url or outputs"
	ev "DXF_EMPTY,name=$name.dxf,target=$target,operation=${op:-convert},opts=$(printf '%s' "${opts:-none}" | sed 's/,/;/g'),job=$JOB_ID,status=failed,code=$code,message=$(printf '%s' "$msg" | sed 's/,/;/g'),retried=0,events=$events,outputs=0,download_url=absent,result=ok"
	N_DXF_EMPTY=$((N_DXF_EMPTY + 1))
}

for e in alpha_clear_geometry alpha_clear_all white_geometry white_text degenerate_fill faint_only zwsp_text; do
	dxf_empty_one "$e" svg ""
	dxf_empty_one "$e" pdf ""
	dxf_empty_one "$e" png render
done
dxf_empty_one white_geometry png render '{"background":"transparent"}'
dxf_empty_one white_text png render '{"size":2048,"background":"transparent"}'

python3 - "$CORPUS/synthetic/legacy_cp1251.dxf" "$W/ceiling.dxf" 123731969 <<'PY' || fail "cannot build the padded dxf"
import sys

src = open(sys.argv[1], "rb").read()
size = int(sys.argv[3])
at = src.index(b"  0\r\nENDSEC\r\n")
chunk = 1025
need = size - len(src)
full = need // chunk - 1
last = need - full * chunk
line = b"999\r\n" + b"x" * (chunk - 7) + b"\r\n"
out = src[:at] + line * full + b"999\r\n" + b"x" * (last - 7) + b"\r\n" + src[at:]
assert len(out) == size, len(out)
open(sys.argv[2], "wb").write(out)
PY
CEIL_BYTES=$(wc -c < "$W/ceiling.dxf" | tr -d ' ')
[ "$CEIL_BYTES" = 123731969 ] || fail "DXF_CEILING: the padded dxf is $CEIL_BYTES bytes, want 123731969"
JOBS_BEFORE=$(psqlq "select count(*) from jobs")
CODE=$(http "$W/ceiling.json" -X POST "$API_BASE/v1/jobs" -F "target=svg" -F "file=@$W/ceiling.dxf;filename=ceiling.dxf")
[ "$CODE" = 413 ] && [ "$(jq -r .code "$W/ceiling.json")" = payload_too_large ] || fail "DXF_CEILING: got $CODE $(cat "$W/ceiling.json"), want 413 payload_too_large"
CEIL_MSG=$(jq -r .error "$W/ceiling.json")
case "$CEIL_MSG" in *dxf*118\ MiB*) ;; *) fail "DXF_CEILING: message '$CEIL_MSG' does not name dxf and 118 MiB" ;; esac
[ "$(jq 'has("job_id")' "$W/ceiling.json")" = false ] || fail "DXF_CEILING: the response carries a job_id"
JOBS_AFTER=$(psqlq "select count(*) from jobs")
[ "$JOBS_BEFORE" = "$JOBS_AFTER" ] || fail "DXF_CEILING: jobs rows moved $JOBS_BEFORE -> $JOBS_AFTER"
ev "DXF_CEILING,name=dxf_118mib_plus_1,bytes=$CEIL_BYTES,ceiling_bytes=123731968,padding=999_comment_pairs,http=413,code=payload_too_large,message=$CEIL_MSG,jobs_created=0,result=ok"
N_DXF_CEILING=1
rm -f "$W/ceiling.dxf"

REPL50_BYTES=$(wc -c < "$REPL50" | tr -d ' ')

inject_start cad-dxf TERM "$W/inj_dxf.out" || fail "DXF_RETRY cad-dxf: the signal watcher did not start"
DR_T0=$(now)
create_job "$REPL50" "$REPL50_NAME" svg
[ "$HTTP" = 202 ] && [ -n "$JOB_ID" ] || fail "DXF_RETRY cad-dxf: create returned $HTTP: $(cat "$W/create.json")"
inject_wait || fail "DXF_RETRY cad-dxf: no cad-dxf process was found to signal for job $JOB_ID"
DR_HIT=$INJ_HIT
DR_JOB=$JOB_ID
wait_job "$DR_JOB" "$ENGINE_TIMEOUT"
DR_SECS=$(elapsed "$DR_T0" "$(now)")
[ "$JOB_STATUS" = done ] || fail "DXF_RETRY cad-dxf: job $DR_JOB ended $JOB_STATUS: $(cat "$W/job.json")"
DR_EVENTS=$(job_events "$DR_JOB")
case ";$DR_EVENTS;" in *";active>active;"*) ;; *) fail "DXF_RETRY cad-dxf: job $DR_JOB events '$DR_EVENTS' show no retry" ;; esac
DR_FACTS=$(output_facts "$DR_JOB")
[ "${DR_FACTS%%,*}" = 1 ] && [ "$(echo "$DR_FACTS" | cut -d, -f2)" = 1 ] && [ "$(echo "$DR_FACTS" | cut -d, -f4)" = svg ] || fail "DXF_RETRY cad-dxf: job_outputs facts '$DR_FACTS'"
download_output "$W/retry_dxf.svg" || fail "DXF_RETRY cad-dxf: no download_url on the done job"
svg_judged "DXF_RETRY cad-dxf" "$W/retry_dxf.svg"
ev "DXF_RETRY,job=$DR_JOB,input=$REPL50_NAME,bytes=$REPL50_BYTES,sha256=$REPL50_SHA,target=svg,signal=TERM,signalled=cad-dxf,hit=$(echo "$DR_HIT" | tr ' ' ':'),status=done,retried=1,events=$DR_EVENTS,seconds=$DR_SECS,outputs=1,svg_check=ok,reserialized_identical=1,texts=$SV_TEXTS,$SV_WALK,result=ok"
N_DXF_RETRY=$((N_DXF_RETRY + 1))

inject_start rsvg-convert TERM "$W/inj_rsvg.out" || fail "DXF_RETRY rsvg-convert: the signal watcher did not start"
DR_T0=$(now)
create_job "$REPL50" "$REPL50_NAME" pdf
[ "$HTTP" = 202 ] && [ -n "$JOB_ID" ] || fail "DXF_RETRY rsvg-convert: create returned $HTTP: $(cat "$W/create.json")"
inject_wait || fail "DXF_RETRY rsvg-convert: no rsvg-convert process was found to signal for job $JOB_ID"
DR_HIT=$INJ_HIT
DR_JOB=$JOB_ID
wait_job "$DR_JOB" "$ENGINE_TIMEOUT"
DR_SECS=$(elapsed "$DR_T0" "$(now)")
[ "$JOB_STATUS" = done ] || fail "DXF_RETRY rsvg-convert: job $DR_JOB ended $JOB_STATUS: $(cat "$W/job.json")"
DR_EVENTS=$(job_events "$DR_JOB")
case ";$DR_EVENTS;" in *";active>active;"*) ;; *) fail "DXF_RETRY rsvg-convert: job $DR_JOB events '$DR_EVENTS' show no retry" ;; esac
DR_FACTS=$(output_facts "$DR_JOB")
[ "${DR_FACTS%%,*}" = 1 ] && [ "$(echo "$DR_FACTS" | cut -d, -f2)" = 1 ] && [ "$(echo "$DR_FACTS" | cut -d, -f4)" = pdf ] || fail "DXF_RETRY rsvg-convert: job_outputs facts '$DR_FACTS'"
download_output "$W/retry_rsvg.pdf" || fail "DXF_RETRY rsvg-convert: no download_url on the done job"
pdf_judged "DXF_RETRY rsvg-convert" "$W/retry_rsvg.pdf"
ev "DXF_RETRY,job=$DR_JOB,input=$REPL50_NAME,bytes=$REPL50_BYTES,sha256=$REPL50_SHA,target=pdf,signal=TERM,signalled=rsvg-convert,hit=$(echo "$DR_HIT" | tr ' ' ':'),status=done,retried=1,events=$DR_EVENTS,seconds=$DR_SECS,outputs=1,starts_pdf=1,pdfinfo_pages=$PD_PAGES,pdf_check=ok,$PD_LINE,result=ok"
N_DXF_RETRY=$((N_DXF_RETRY + 1))

inject_start cad-dxf 0 "$W/inj_dxf_cancel.out" || fail "DXF_CANCEL: the watcher did not start"
create_job "$REPL50" "$REPL50_NAME" svg
[ "$HTTP" = 202 ] && [ -n "$JOB_ID" ] || fail "DXF_CANCEL: create returned $HTTP: $(cat "$W/create.json")"
DX_JOB=$JOB_ID
inject_wait || fail "DXF_CANCEL: no cad-dxf process ever appeared for job $DX_JOB"
DX_PID=$INJ_HIT_PID
T_CANCEL=$(now)
CODE=$(http "$W/cancel.json" -X POST "$API_BASE/v1/jobs/$DX_JOB/cancel")
[ "$CODE" = 200 ] && [ "$(jq -r .status "$W/cancel.json")" = canceled ] || fail "DXF_CANCEL: cancel returned $CODE $(cat "$W/cancel.json")"
GONE=0
for _ in $(seq 1 24); do
	case " $(proc_pids cad-dxf) " in *" $DX_PID "*) ;; *)
		GONE=1
		break
		;;
	esac
	sleep 0.25
done
GONE_S=$(elapsed "$T_CANCEL" "$(now)")
[ "$GONE" = 1 ] && within "$GONE_S" 5 || fail "DXF_CANCEL: cad-dxf pid $DX_PID still alive ${GONE_S}s after cancel"
wait_job "$DX_JOB" 30
[ "$JOB_STATUS" = canceled ] || fail "DXF_CANCEL: job reads $JOB_STATUS, want canceled"
[ "$(jq 'has("download_url") or has("outputs")' "$W/job.json")" = false ] || fail "DXF_CANCEL: a canceled job exposes download_url or outputs"
[ "$(psqlq "select count(*) from job_outputs where job_id = '$DX_JOB'")" = 0 ] || fail "DXF_CANCEL: output rows exist for a canceled job"
LEFT_DXF=$(proc_pids cad-dxf)
[ -z "$(echo $LEFT_DXF)" ] || fail "DXF_CANCEL: cad-dxf process(es) [$LEFT_DXF] are alive after the cancel"
ev "DXF_CANCEL,job=$DX_JOB,input=$REPL50_NAME,cad_dxf_pid=$DX_PID,pid_gone=1,gone_s=$GONE_S,cad_dxf_left=0,status=canceled,download_url=absent,outputs=0,result=ok"
N_DXF_CANCEL=1

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

IMG_SIZE=$(docker image inspect -f '{{.Size}}' "$CAD_IMAGE")
P73_ID=$(sed -n 's/^HDR,cad_worker_image_id=//p' .planning/phases/73-cad-class-convert-slice-confined/evidence/73-06-liveproof.txt | head -1)
P73_SIZE=absent
if [ -n "$P73_ID" ] && docker image inspect "$P73_ID" >/dev/null 2>&1; then
	P73_SIZE=$(docker image inspect -f '{{.Size}}' "$P73_ID")
fi
ev "IMAGE,cad_worker_image_id=$CAD_IMAGE,cad_worker_size_bytes=$IMG_SIZE,phase73_image_id=${P73_ID:-unknown},phase73_size_bytes=$P73_SIZE"
N_IMAGE=1

ev "SUMMARY,result=ALL PASS,formats=$N_FORMATS,inspect=$N_INSPECT,convert=$N_CONVERT,quality=$N_QUALITY,render=$N_RENDER,views=$N_VIEWS,meas=$N_MEAS,opts_reject=$N_OPTS_REJECT,reject=$N_REJECT,bomb=$N_BOMB,render_retry=$N_RENDER_RETRY,mesh_signal=$N_MESH_SIGNAL,empty=$N_EMPTY,cancel_render=$N_CANCEL_RENDER,cancel=$N_CANCEL,worker_kill=$N_KILL,image=$N_IMAGE,sandbox=$N_SANDBOX,dxf_svg=$N_DXF_SVG,dxf_pdf=$N_DXF_PDF,dxf_render=$N_DXF_RENDER,dxf_meas=$N_DXF_MEAS,dxf_opts_reject=$N_DXF_OPTS_REJECT,dxf_bomb=$N_DXF_BOMB,dxf_empty=$N_DXF_EMPTY,dxf_ceiling=$N_DXF_CEILING,dxf_retry=$N_DXF_RETRY,dxf_cancel=$N_DXF_CANCEL"
FINISHED=1
