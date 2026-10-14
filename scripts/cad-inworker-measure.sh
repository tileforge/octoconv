#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO_ROOT"

usage() {
	cat >&2 <<'USAGE'
Usage:
  scripts/cad-inworker-measure.sh --smoke [--out FILE]
  scripts/cad-inworker-measure.sh --cells FILE --out FILE

Measures cad jobs inside the production cad-worker of the compose stack of this checkout (plan 76-03).
Every row restarts the cad-worker, records the preflight-only memory.peak, watches the container's
/proc every 0.2 s for cad-guard, cad-mesh, cad-render, cad-dxf and rsvg-convert, creates one job through
the API and waits for it, then reads started_at, finished_at and attempts of the job row, VmHWM of the
Go worker (PID 1), memory.peak and the oom_kill counter of the container. A row is written only for a
job that is done with attempts=1.

--smoke   one render_step row of public/LinearSlide-MGN15x-Block.step to png at size 1280
          (default output: a file under TMPDIR).
--cells   the CELLSPEC lines of the last derive76 --select-cells section of FILE (the whole file when it has no
          section header), CAD_INWORKER_N rows each (default 3); a STOP line in that section refuses the run.

The cad-worker must carry the measurement deadlines of an untracked docker-compose.override.yml
(guard 120s, mesh 900s, render 900s, dxf 600s, rsvg 600s, engine 3600s); the API is reached at
CAD_INWORKER_API_BASE (default http://127.0.0.1:18090). The run must be a child of caffeinate -dimsu.
It refuses to start when webhook-worker runs, an octoconv k8s pod exists, a go test or docker build
runs, or the compose project is not octoconv. It mints and revokes its own client and stops only the
services it started.
Exit codes: 64 usage, 69 a tool is missing, 3 VM disk below 6 GiB, 1 a failure.
USAGE
	exit 64
}

API_BASE="${CAD_INWORKER_API_BASE:-http://127.0.0.1:18090}"
DB_URL="${CAD_INWORKER_DATABASE_URL:-postgres://octo:octo-pass@127.0.0.1:5434/octo_db}"
N_REPS="${CAD_INWORKER_N:-3}"
GO_TOOLCHAIN=go1.26.4
export GOTOOLCHAIN=$GO_TOOLCHAIN
MIN_FREE_GIB=6
PROJECT=octoconv
CNAME=octoconv-cad-worker
SERVICES="postgres redis minio api cad-worker"
OVERRIDE=docker-compose.override.yml
SMOKE_FILE=internal/convert/testdata/cad/public/LinearSlide-MGN15x-Block.step
JOB_LIMIT_S=4000
WANT_ENV="CAD_GUARD_TIMEOUT=120s CAD_MESH_TIMEOUT=900s CAD_RENDER_TIMEOUT=900s CAD_DXF_TIMEOUT=600s CAD_RSVG_TIMEOUT=600s CAD_ENGINE_TIMEOUT=3600s"

SMOKE=0
CELLS=""
OUT=""
while [ $# -gt 0 ]; do
	case "$1" in
	--smoke) SMOKE=1; shift ;;
	--cells) [ $# -ge 2 ] || usage; CELLS=$2; shift 2 ;;
	--out) [ $# -ge 2 ] || usage; OUT=$2; shift 2 ;;
	-h | --help) usage ;;
	*) usage ;;
	esac
done
if [ "$SMOKE" = 1 ]; then
	[ -z "$CELLS" ] || usage
	[ -n "$OUT" ] || OUT="${TMPDIR:-/tmp}/76-03-smoke.txt"
else
	[ -n "$CELLS" ] && [ -n "$OUT" ] || usage
	[ -f "$CELLS" ] || { echo "error: $CELLS does not exist" >&2; exit 64; }
fi
case "$N_REPS" in '' | *[!0-9]* | 0) usage ;; esac

for tool in docker curl jq python3 go shasum git pmset sysctl; do
	command -v "$tool" >/dev/null 2>&1 || { echo "error: $tool is required" >&2; exit 69; }
done

W=""
KEY=""
CLIENT_ID=""
SALT=""
STARTED_BY_ME=""
WATCH_BG=""
FINISHED=""
ROWS=0

log() { echo "[$(date -u +%H:%M:%S)] $*" >&2; }
ev() { printf '%s\n' "$*" >> "$OUT"; printf '%s\n' "$*" | cut -c1-240 >&2; }
dc() { docker compose -p "$PROJECT" "$@"; }

fail() {
	log "FAIL: $*"
	[ -n "$OUT" ] && printf 'FAIL,mode=inworker,reason=%s\n' "$(printf '%s' "$*" | tr ', \n' ';__')" >> "$OUT"
	exit 1
}

cleanup() {
	local rc=$?
	if [ -n "$WATCH_BG" ]; then
		kill "$WATCH_BG" 2>/dev/null || true
	fi
	if [ "$rc" -ne 0 ] && [ -z "$FINISHED" ]; then
		dc logs --no-color --tail 40 cad-worker >&2 2>/dev/null || true
	fi
	if [ -n "$CLIENT_ID" ] && [ -n "$SALT" ]; then
		if DATABASE_URL="$DB_URL" API_KEY_SALT="$SALT" go run ./cmd/manage-clients revoke "$CLIENT_ID" primary >/dev/null 2>&1; then
			log "client $CLIENT_ID revoked"
		else
			log "warning: revoke failed for client $CLIENT_ID"
		fi
	fi
	KEY=""
	if [ -n "$STARTED_BY_ME" ]; then
		log "stopping the services this run started:$STARTED_BY_ME"
		dc stop $STARTED_BY_ME >&2 || true
	fi
	[ -n "$W" ] && rm -rf "$W"
	exit "$rc"
}
trap cleanup EXIT

vm_free_gib() { docker run --rm alpine df -k / | awk 'NR == 2 { print int($4 / 1048576) }'; }
load1_now() { sysctl -n vm.loadavg | awk '{print $2}'; }
mib() { awk -v b="$1" 'BEGIN { printf "%.1f", b / 1048576 }'; }

quiet_wait() {
	local t0 waited
	t0=$(date +%s)
	while pgrep -f 'go test' >/dev/null 2>&1 || pgrep -f 'docker build|buildx' >/dev/null 2>&1; do
		waited=$(($(date +%s) - t0))
		[ "$waited" -lt 7200 ] || fail "a go test or docker build kept running for 7200 s"
		log "a go test or docker build is running; waiting"
		sleep 30
	done
}

disk_check() {
	local free
	free=$(vm_free_gib)
	if [ -z "$free" ] || [ "$free" -lt "$MIN_FREE_GIB" ]; then
		log "VM disk ${free:-unknown} GiB is below $MIN_FREE_GIB GiB"
		printf 'FAIL,mode=inworker,reason=disk_below_threshold,free_gib=%s\n' "${free:-unknown}" >> "$OUT"
		exit 3
	fi
	FREE=$free
}

caffeinated() {
	local pid=$$ ancestors="" a
	CAFF_PID=""
	CAFF_FOR=""
	while [ -n "$pid" ] && [ "$pid" -gt 1 ]; do
		ancestors="$ancestors $pid"
		pid=$(ps -o ppid= -p "$pid" 2>/dev/null | tr -d ' ')
	done
	for a in $ancestors; do
		CAFF_PID=$(pmset -g assertions | awk -v want="$a" '
			/\(caffeinate\):/ { cp = $2; sub(/\(.*/, "", cp); ps = ($0 ~ / PreventSystemSleep named/) }
			/asserting on behalf of/ && ps && !found { w = $NF; gsub(/[()]/, "", w); if (w == want) { print cp; found = 1 } }' || true)
		if [ -n "$CAFF_PID" ]; then
			CAFF_FOR=$a
			break
		fi
	done
	[ -n "$CAFF_PID" ] || fail "the run must be a child of caffeinate -dimsu holding PreventSystemSleep"
	[ "$(pmset -g assertions | awk '$1 == "PreventSystemSleep" { print $2 }')" = 1 ] || fail "PreventSystemSleep is not 1"
}

sleeps_since() {
	pmset -g log | python3 -c '
import datetime, re, sys
since = datetime.datetime.strptime(sys.argv[1], "%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=datetime.timezone.utc)
hits = []
for line in sys.stdin:
    m = re.match(r"^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}) ([+-]\d{4}) ([^\t]*?)\s*\t", line)
    if not m or m.group(3).strip() != "Sleep":
        continue
    t = datetime.datetime.strptime(m.group(1) + " " + m.group(2), "%Y-%m-%d %H:%M:%S %z").astimezone(datetime.timezone.utc)
    if t >= since:
        hits.append(t.strftime("%Y-%m-%dT%H:%M:%SZ"))
print("+".join(hits))' "$1"
}

container_env() { docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$CNAME" | sed -n "s/^$1=//p"; }

check_env() {
	local kv k v got bad=""
	for kv in $WANT_ENV; do
		k=${kv%%=*}
		v=${kv#*=}
		got=$(container_env "$k")
		[ "$got" = "$v" ] || bad="$bad $k=${got:-unset}(want_$v)"
	done
	[ -z "$bad" ] || fail "the cad-worker does not carry the measurement deadlines:$bad"
	got=$(docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$(dc ps -q api)" | sed -n 's/^CAD_ENGINE_TIMEOUT=//p')
	[ "$got" = 3600s ] || fail "the api does not carry CAD_ENGINE_TIMEOUT=3600s (got ${got:-unset})"
	got=$(container_env CAD_SANDBOX_DISABLED)
	[ -z "$got" ] || fail "the cad-worker has CAD_SANDBOX_DISABLED=$got"
}

preflight() {
	local name n running
	name=$(docker compose config --format json | jq -r .name)
	[ "$name" = "$PROJECT" ] || fail "compose project is '$name', want $PROJECT"
	[ -f "$OVERRIDE" ] || fail "$OVERRIDE is missing; the measurement deadlines come from it"
	if git ls-files --error-unmatch "$OVERRIDE" >/dev/null 2>&1; then fail "$OVERRIDE is tracked"; fi
	if command -v kubectl >/dev/null 2>&1; then
		n=$(kubectl get pods -A 2>/dev/null | grep -c octoconv || true)
		[ "$n" = 0 ] || fail "$n octoconv pods exist in k8s"
	fi
	running=$(dc ps --status running --services 2>/dev/null || true)
	if printf '%s\n' "$running" | grep -qx webhook-worker; then fail "webhook-worker runs; the reconciler must be absent"; fi
	if pgrep -f 'go test' >/dev/null 2>&1; then fail "a go test process is running"; fi
	if pgrep -f 'docker build|buildx' >/dev/null 2>&1; then fail "a docker build is running"; fi
	n=$(docker compose config --format json | jq -r '.services["cad-worker"].environment | [.CAD_GUARD_TIMEOUT, .CAD_MESH_TIMEOUT, .CAD_RENDER_TIMEOUT, .CAD_DXF_TIMEOUT, .CAD_RSVG_TIMEOUT, .CAD_ENGINE_TIMEOUT] | join(" ")')
	[ "$n" = "120s 900s 900s 600s 600s 3600s" ] || fail "the compose config does not carry the measurement deadlines (got $n)"
	for s in $SERVICES; do
		printf '%s\n' "$running" | grep -qx "$s" || STARTED_BY_ME="$STARTED_BY_ME $s"
	done
}

wait_proven() {
	local since=$1 i logs
	for i in $(seq 1 120); do
		logs=$(docker logs --since "$since" "$CNAME" 2>&1 || true)
		case "$logs" in *"cad sandbox proven"*) return 0 ;; esac
		sleep 1
	done
	fail "the cad-worker never logged 'cad sandbox proven' since $since"
}

restart_worker() {
	local before after i
	before=$(docker inspect -f '{{.State.StartedAt}}' "$CNAME")
	dc restart cad-worker >&2
	for i in $(seq 1 60); do
		after=$(docker inspect -f '{{.State.StartedAt}}' "$CNAME")
		[ "$after" != "$before" ] && [ "$(docker inspect -f '{{.State.Running}}' "$CNAME")" = true ] && break
		sleep 1
	done
	[ "$after" != "$before" ] || fail "the cad-worker did not restart"
	wait_proven "$after"
}

cg_read() { docker exec "$CNAME" cat "$1" | tr -d '\r'; }

http() {
	local out=$1 code tries=0
	shift
	while :; do
		code=$(curl -sS -o "$out" -w '%{http_code}' -H @<(printf 'Authorization: ApiKey %s\n' "$KEY") "$@")
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
	[ -z "${4:-}" ] || args+=(-F "operation=$4")
	[ -z "${5:-}" ] || args+=(-F "opts=$5")
	HTTP=$(http "$W/create.json" -X POST "$API_BASE/v1/jobs" "${args[@]}")
	JOB_ID=$(jq -r '.job_id // empty' "$W/create.json" 2>/dev/null || true)
}

wait_job() {
	local id=$1 t0 code
	t0=$(date +%s)
	JOB_STATUS=""
	while :; do
		code=$(http "$W/job.json" "$API_BASE/v1/jobs/$id")
		if [ "$code" = 200 ]; then
			JOB_STATUS=$(jq -r .status "$W/job.json")
			case "$JOB_STATUS" in done | failed | canceled) return 0 ;; esac
		fi
		[ $(($(date +%s) - t0)) -lt "$JOB_LIMIT_S" ] || { JOB_STATUS=timeout; return 0; }
		sleep 2
	done
}

psqlq() { dc exec -T postgres psql -U octo -d octo_db -At -F, -c "$1" < /dev/null; }

write_watcher() {
	cat > "$W/watch.sh" <<'WATCH'
stop=$1
tck=$(getconf CLK_TCK)
bt=$(awk '/^btime/ { print $2 }' /proc/stat)
read up _ < /proc/uptime
now=$(date +%s.%N)
echo "B $bt $tck $now $up"
live=" "
gone=" "
i=0
while [ "$i" -lt 72000 ]; do
	read up _ < /proc/uptime
	for d in /proc/[0-9]*; do
		p=${d#/proc/}
		case "$live$gone" in *" $p "*) continue ;; esac
		[ "$p" = "$$" ] && continue
		c=""
		read c 2>/dev/null < "$d/comm" || continue
		name=""
		case "$c" in
		rsvg-convert | cad-guard | cad-mesh | cad-render | cad-dxf) name=$c ;;
		python*)
			for a in $(tr '\0' '\n' 2>/dev/null < "$d/cmdline"); do
				case "${a##*/}" in
				cad-guard | cad-mesh | cad-render | cad-dxf) name=${a##*/}; break ;;
				esac
			done
			;;
		esac
		[ -n "$name" ] || continue
		line=""
		read -r line 2>/dev/null < "$d/stat" || continue
		rest=${line##*) }
		set -- $rest
		st=${20}
		live="$live$p "
		eval "seen_$p=$up"
		echo "P $p $name $st"
	done
	for p in $live; do
		if [ -d "/proc/$p" ]; then
			eval "seen_$p=$up"
		else
			eval "ls=\$seen_$p"
			echo "E $p $ls $up"
			live=$(echo "$live" | sed "s/ $p / /")
			gone="$gone$p "
		fi
	done
	if [ -e "$stop" ]; then
		echo "S $up"
		exit 0
	fi
	i=$((i + 1))
	sleep 0.2
done
echo "X $up"
exit 3
WATCH
}

watch_start() {
	local tok=$1 i
	: > "$W/watch.out"
	docker exec -i "$CNAME" sh -s "/tmp/inworker-stop-$tok" < "$W/watch.sh" > "$W/watch.out" 2>&1 &
	WATCH_BG=$!
	for i in $(seq 1 50); do
		grep -q '^B ' "$W/watch.out" && return 0
		sleep 0.2
	done
	fail "the /proc watcher did not start: $(head -c 300 "$W/watch.out" | tr '\n' ' ')"
}

watch_stop() {
	local tok=$1 i
	docker exec "$CNAME" sh -c ": > /tmp/inworker-stop-$tok" || fail "cannot write the watcher stop file"
	for i in $(seq 1 50); do
		grep -q '^S ' "$W/watch.out" && break
		sleep 0.2
	done
	wait "$WATCH_BG" 2>/dev/null || true
	WATCH_BG=""
	grep -q '^S ' "$W/watch.out" || fail "the /proc watcher did not stop cleanly: $(tail -c 300 "$W/watch.out" | tr '\n' ' ')"
	docker exec "$CNAME" rm -f "/tmp/inworker-stop-$tok" || true
}

stage_lines() {
	python3 - "$W/watch.out" "$1" "$2" "$3" "$4" "$5" <<'PY'
import sys
path, started, finished, route, cell, rep = sys.argv[1], float(sys.argv[2]), float(sys.argv[3]), sys.argv[4], sys.argv[5], sys.argv[6]
b = None
procs = {}
ends = {}
for line in open(path):
    f = line.split()
    if not f:
        continue
    if f[0] == "B":
        b = (int(f[1]), int(f[2]), float(f[3]), float(f[4]))
    elif f[0] == "P":
        procs[f[1]] = (f[2], int(f[3]))
    elif f[0] == "E":
        ends[f[1]] = (float(f[2]), float(f[3]))
if b is None:
    print("FAILED no_B_line")
    sys.exit(1)
btime, tck, epoch_b, up_b = b
offset = epoch_b - up_b
missing = [p for p in procs if p not in ends]
if missing:
    print("FAILED no_end_for_pids_" + "+".join("%s:%s" % (p, procs[p][0]) for p in missing))
    sys.exit(1)
rows = []
for p, (name, st) in procs.items():
    start_up = st / tck
    last_up, gone_up = ends[p]
    wall = last_up - start_up
    rows.append((start_up, p, name, wall, offset + start_up, offset + last_up))
rows.sort()
total = 0.0
for start_up, p, name, wall, s_ep, e_ep in rows:
    total += wall
    print("INWORKER_STAGE,route=%s,cell=%s,rep=%s,stage=%s,pid=%s,wall_s=%.2f,start_after_started_at_s=%.2f,end_before_finished_at_s=%.2f,btime_minus_uptime_base_s=%.2f" % (
        route, cell, rep, name, p, wall, s_ep - started, finished - e_ep, btime - offset))
print("SUM %.2f %d" % (total, len(rows)))
PY
}

measure_row() {
	local route=$1 cell=$2 rep=$3 file=$4 target=$5 op=$6 opts=$7 tok load free pre facts started finished attempts status out sum nstages hwm peak oom nonstage wall
	quiet_wait
	disk_check
	free=$FREE
	load=$(load1_now)
	restart_worker
	pre=$(cg_read /sys/fs/cgroup/memory.peak)
	tok="$(date +%s)-$$-$rep"
	watch_start "$tok"
	create_job "$file" "$(basename "$file")" "$target" "$op" "$opts"
	if [ "$HTTP" != 201 ] && [ "$HTTP" != 202 ] || [ -z "$JOB_ID" ]; then
		watch_stop "$tok"
		fail "create route=$route cell=$cell rep=$rep returned HTTP $HTTP: $(head -c 300 "$W/create.json" | tr '\n' ' ')"
	fi
	log "route=$route cell=$cell rep=$rep job=$JOB_ID"
	wait_job "$JOB_ID"
	facts=$(psqlq "select status, attempts, extract(epoch from started_at), extract(epoch from finished_at) from jobs where id = '$JOB_ID'")
	watch_stop "$tok"
	status=$(printf '%s' "$facts" | cut -d, -f1)
	attempts=$(printf '%s' "$facts" | cut -d, -f2)
	started=$(printf '%s' "$facts" | cut -d, -f3)
	finished=$(printf '%s' "$facts" | cut -d, -f4)
	if [ "$JOB_STATUS" != done ] || [ "$status" != done ] || [ "$attempts" != 1 ] || [ -z "$started" ] || [ -z "$finished" ]; then
		ev "FAIL,mode=inworker,route=$route,cell=$cell,rep=$rep,job=$JOB_ID,status=${status:-none},api_status=${JOB_STATUS:-none},attempts=${attempts:-none},error_code=$(jq -r '.error_code // .error.code // "none"' "$W/job.json" 2>/dev/null | tr ',' ';')"
		exit 1
	fi
	out=$(stage_lines "$started" "$finished" "$route" "$cell" "$rep") || fail "route=$route cell=$cell rep=$rep stage walls: $out"
	sum=$(printf '%s\n' "$out" | awk '$1 == "SUM" { print $2 }')
	nstages=$(printf '%s\n' "$out" | awk '$1 == "SUM" { print $3 }')
	[ "$nstages" -ge 1 ] || fail "route=$route cell=$cell rep=$rep: the watcher saw no stage process"
	hwm=$(cg_read /proc/1/status | awk '$1 == "VmHWM:" { print $2 }')
	peak=$(cg_read /sys/fs/cgroup/memory.peak)
	oom=$(cg_read /sys/fs/cgroup/memory.events | awk '$1 == "oom_kill" { print $2 }')
	[ -n "$hwm" ] && [ -n "$peak" ] && [ -n "$oom" ] || fail "route=$route cell=$cell rep=$rep: cannot read VmHWM, memory.peak or oom_kill"
	wall=$(awk -v a="$started" -v b="$finished" 'BEGIN { printf "%.2f", b - a }')
	nonstage=$(awk -v w="$wall" -v s="$sum" 'BEGIN { printf "%.2f", w - s }')
	printf '%s\n' "$out" | grep '^INWORKER_STAGE,' | while IFS= read -r l; do ev "$l"; done
	ev "INWORKER_ROW,route=$route,cell=$cell,rep=$rep,job=$JOB_ID,status=done,attempts=1,attempt_wall_s=$wall,stage_sum_s=$sum,nonstage_s=$nonstage,go_vmhwm_mib=$(awk -v k="$hwm" 'BEGIN { printf "%.1f", k / 1024 }'),container_peak_mib=$(mib "$peak"),preflight_peak_mib=$(mib "$pre"),oom_kill=$oom,load1=$load,vm_free_gib=$free,stages=$nstages,file_bytes=$(wc -c < "$file" | tr -d ' '),target=$target,operation=$op"
	ROWS=$((ROWS + 1))
}

opts_json() {
	python3 -c '
import json, sys
s = sys.argv[1]
o = {}
if s not in ("", "none"):
    for kv in s.split(";"):
        k, v = kv.split(":", 1)
        o[k] = int(v) if v.isdigit() else v
print(json.dumps(o, separators=(",", ":")) if o else "")' "$1"
}

: > "$OUT"
preflight
caffeinated
disk_check
W=$(mktemp -d "${TMPDIR:-/tmp}/cad-inworker.XXXXXX")
write_watcher

log "starting the stack (not running before:${STARTED_BY_ME:- none})"
UP_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ)
dc up -d --build postgres redis minio createbucket api cad-worker >&2
for _ in $(seq 1 60); do
	curl -fsS -o /dev/null "$API_BASE/healthz" 2>/dev/null && break
	sleep 2
done
curl -fsS -o /dev/null "$API_BASE/healthz" || fail "api /healthz not reachable at $API_BASE"
wait_proven "$UP_AT"
check_env

SALT=$(dc exec -T api printenv API_KEY_SALT | tr -d '\r\n')
[ -n "$SALT" ] || fail "cannot read API_KEY_SALT from the api container"
CREATED=$(DATABASE_URL="$DB_URL" API_KEY_SALT="$SALT" go run ./cmd/manage-clients create "cad-inworker-$(date +%s)")
CLIENT_ID=$(printf '%s\n' "$CREATED" | awk '/^client id:/ { print $3 }')
KEY=$(printf '%s\n' "$CREATED" | awk -F': ' '/^api key/ { print $2 }')
CREATED=""
[ -n "$CLIENT_ID" ] && [ -n "$KEY" ] || fail "manage-clients create returned no id or key"

HDR_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
DIRT=$(git status --porcelain --untracked-files=no | wc -l | tr -d ' ')
ENVLINE=$(for kv in $WANT_ENV; do printf '%s=%s;' "${kv%%=*}" "$(container_env "${kv%%=*}")"; done | sed 's/;$//')
MODE_NAME=inworker
[ "$SMOKE" = 0 ] || MODE_NAME=inworker_smoke
ev "HDR,mode=$MODE_NAME,date_utc=$HDR_DATE,commit=$(git rev-parse HEAD),tracked_dirt=$DIRT,server_arch=$(docker version --format '{{.Server.Arch}}')"
ev "HDR,mode=$MODE_NAME,api_image_id=$(docker inspect -f '{{.Image}}' "$(dc ps -q api)"),cad_worker_image_id=$(docker inspect -f '{{.Image}}' "$CNAME")"
ev "HDR,mode=$MODE_NAME,override_sha256=$(shasum -a 256 "$OVERRIDE" | awk '{print $1}'),override_env=$ENVLINE,api_cad_engine_timeout=$(docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$(dc ps -q api)" | sed -n 's/^CAD_ENGINE_TIMEOUT=//p')"
ev "HDR,mode=$MODE_NAME,load1=$(load1_now),vm_free_gib=$FREE,limits=cpus_$(docker inspect -f '{{.HostConfig.NanoCpus}}' "$CNAME")_nano;memory_$(docker inspect -f '{{.HostConfig.Memory}}' "$CNAME")_bytes,pid1_comm=$(cg_read /proc/1/comm),caffeinate_pid=$CAFF_PID,caffeinate_on_behalf_of=$CAFF_FOR,prevent_system_sleep=1,started_by_run=$(echo "${STARTED_BY_ME:-none}" | tr -s ' ' | sed 's/^ //' | tr ' ' ';')"

if [ "$SMOKE" = 1 ]; then
	measure_row render_step smoke 1 "$SMOKE_FILE" png render '{"size":1280}'
	found=$(sleeps_since "$HDR_DATE")
	[ -z "$found" ] || fail "the host slept during the run: $found"
	ev "SUMMARY,mode=inworker_smoke,rows=$ROWS,result=ok"
	FINISHED=1
	exit 0
fi

awk '/^HDR,mode=derive76_select,/ { n = NR } { l[NR] = $0 } END { for (i = n + 1; i <= NR; i++) print l[i] }' "$CELLS" > "$W/section.txt"
grep '^CELLSPEC,' "$W/section.txt" > "$W/cells.txt" || fail "no CELLSPEC line in the last section of $CELLS"
if grep -q '^STOP,' "$W/section.txt"; then fail "the last section of $CELLS carries a STOP line"; fi
while IFS= read -r spec; do
	file=$(printf '%s' "$spec" | tr ',' '\n' | sed -n 's/^file=//p')
	sha=$(printf '%s' "$spec" | tr ',' '\n' | sed -n 's/^sha256=//p')
	[ -f "$file" ] || fail "missing cell file $file"
	[ "$(shasum -a 256 "$file" | awk '{print $1}')" = "$sha" ] || fail "sha256 of $file differs from its CELLSPEC"
done < "$W/cells.txt"
while IFS= read -r spec <&3; do
	f() { printf '%s' "$spec" | tr ',' '\n' | sed -n "s/^$1=//p"; }
	route=$(f route)
	cell=$(f cell)
	file=$(f file)
	target=$(f target)
	op=$(f operation)
	opts=$(opts_json "$(f opts)")
	for rep in $(seq 1 "$N_REPS"); do
		measure_row "$route" "$cell" "$rep" "$file" "$target" "$op" "$opts" < /dev/null
	done
done 3< "$W/cells.txt"
found=$(sleeps_since "$HDR_DATE")
[ -z "$found" ] || fail "the host slept during the run: $found"
NROUTES=$(wc -l < "$W/cells.txt" | tr -d ' ')
[ "$ROWS" = $((NROUTES * N_REPS)) ] || fail "wrote $ROWS rows, want $((NROUTES * N_REPS)) for $NROUTES routes at N=$N_REPS"
ev "SUMMARY,mode=inworker,routes=$NROUTES,rows=$ROWS,n=$N_REPS,result=ok"
FINISHED=1
