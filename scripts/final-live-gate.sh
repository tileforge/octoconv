#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT=$(pwd)

ALL_STEPS=(preflight ci-local tectonic sandbox cad frontend)
FLOOR_KIB=6291456
BUDGET_S=540
D19_QUIET_LOAD=8
ROW_QUIET_LOAD=11
CHECK_PORTS="18090 8091 9100 9101 9190 9191 5434 6379 8980"
SANDBOX_CLASSES="image pandoc ocr av audio html probe document cad"
OUTPUT_NAMES="76-11-final-gate.txt 76-11-sandbox-liveproof.txt 76-11-liveproof.txt 76-11-liveproof-load.txt 76-11-liveproof-console.txt 76-11-liveproof-rerun.txt 76-11-liveproof-rerun-load.txt 76-11-liveproof-rerun-console.txt 76-11-frontend.txt 76-11-tectonic-worker.log 76-11-watchdog.txt"
OVERRIDE_FILE="$ROOT/docker-compose.override.yml"
E2E_PORTS_FILE="$ROOT/docker-compose.e2e-ports.yml"
UUID_RE='^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'

usage() {
	echo "usage: scripts/final-live-gate.sh --evidence-dir DIR [--steps preflight,ci-local,tectonic,sandbox,cad,frontend]" >&2
	exit 64
}

EVD=""
STEPS_ARG="preflight,ci-local,tectonic,sandbox,cad,frontend"
while [ $# -gt 0 ]; do
	case "$1" in
	--evidence-dir)
		[ $# -ge 2 ] || usage
		EVD=$2
		shift 2
		;;
	--steps)
		[ $# -ge 2 ] || usage
		STEPS_ARG=$2
		shift 2
		;;
	*) usage ;;
	esac
done
[ -n "$EVD" ] || usage
[ -n "$STEPS_ARG" ] || usage
for s in $(echo "$STEPS_ARG" | tr ',' ' '); do
	case " ${ALL_STEPS[*]} " in
	*" $s "*) ;;
	*)
		echo "unknown step: $s" >&2
		usage
		;;
	esac
done

mkdir -p "$EVD"
EVD=$(cd "$EVD" && pwd)
for n in $OUTPUT_NAMES; do
	if [ -e "$EVD/$n" ]; then
		echo "refusing: $EVD/$n already exists; move the earlier attempt's files away first, they are never overwritten" >&2
		exit 64
	fi
done

OUT="$EVD/76-11-final-gate.txt"
WD_LOG="$EVD/76-11-watchdog.txt"
STATE=$(mktemp -d)
MAIN_PID=$$
CUR_PID=""
WD_PID=""
SAMPLER_PID=""
STOPPED=0
STOP_WRITTEN=0
FAILED=0
PREFLIGHT_OK=0
WROTE_OVERRIDE=0
WROTE_E2E_PORTS=0
COMPOSE_TOUCHED=0
CI_LOG=""
CI_START_UTC=""
STEP_RC=0
STEP_TRANSCRIPT=""
: > "$OUT"
: > "$WD_LOG"

utc() { date -u +%Y-%m-%dT%H:%M:%SZ; }
ev() {
	printf '%s\n' "$*" >> "$OUT"
	printf '%s\n' "$*"
}
load1() { sysctl -n vm.loadavg 2>/dev/null | tr -d '{}' | awk '{ print $1 }'; }
san() { printf '%s' "$*" | tr ', \n' ';__'; }
selected() {
	case ",$STEPS_ARG," in *",$1,"*) return 0 ;; esac
	return 1
}

tmo() {
	perl -e '
		my $n = shift @ARGV;
		my $pid = fork();
		die "fork: $!\n" unless defined $pid;
		if ($pid == 0) { setpgrp(0, 0); exec { $ARGV[0] } @ARGV or exit 127; }
		$SIG{TERM} = sub { kill "TERM", -$pid; sleep 3; kill "KILL", -$pid; waitpid($pid, 0); exit 143; };
		$SIG{ALRM} = sub { kill "KILL", -$pid; waitpid($pid, 0); print STDERR "TIMEOUT after ${n}s: @ARGV\n"; exit 124; };
		alarm $n;
		waitpid($pid, 0);
		alarm 0;
		my $s = $?;
		exit(($s & 127) ? 128 + ($s & 127) : $s >> 8);
	' "$@"
}

killtree() {
	local p=$1 c
	for c in $(pgrep -P "$p" 2>/dev/null || true); do
		killtree "$c"
	done
	kill -TERM "$p" 2>/dev/null || true
}

vm_free_kib() { tmo 90 docker run --rm alpine df -k / 2>/dev/null | awk 'NR==2 { print $4 }'; }

watchdog() {
	local misses=0 k h
	while :; do
		k=$(vm_free_kib || true)
		h=$(df -k "$ROOT" 2>/dev/null | awk 'NR==2 { print $4 }' || true)
		printf '%s free_kib=%s host_free_kib=%s load1=%s\n' "$(utc)" "${k:-unknown}" "${h:-unknown}" "$(load1 || true)" >> "$WD_LOG"
		case "$k" in
		'' | *[!0-9]*)
			misses=$((misses + 1))
			if [ "$misses" -ge 3 ]; then
				echo "docker_unresponsive,misses=$misses" > "$STATE/stop"
				kill -TERM "$MAIN_PID" 2>/dev/null || true
				return 0
			fi
			;;
		*)
			misses=0
			if [ "$k" -lt "$FLOOR_KIB" ]; then
				echo "vm_disk_below_floor,free_kib=$k" > "$STATE/stop"
				kill -TERM "$MAIN_PID" 2>/dev/null || true
				return 0
			fi
			;;
		esac
		sleep 60
	done
}

sampler() {
	while :; do
		printf '%s %s\n' "$(date +%s)" "$(load1 || true)" >> "$STATE/loads"
		sleep 10
	done
}

on_term() {
	STOPPED=1
	[ -s "$STATE/stop" ] || echo terminated > "$STATE/stop"
	if [ -n "$CUR_PID" ]; then
		killtree "$CUR_PID"
	fi
}

cleanup() {
	local rc=$?
	trap - TERM INT
	[ -z "$WD_PID" ] || killtree "$WD_PID"
	[ -z "$SAMPLER_PID" ] || killtree "$SAMPLER_PID"
	[ -z "$CUR_PID" ] || killtree "$CUR_PID"
	if [ "$COMPOSE_TOUCHED" = 1 ]; then
		tmo 300 docker compose -p octoconv stop > /dev/null 2>&1 < /dev/null || true
	fi
	[ "$WROTE_OVERRIDE" = 0 ] || rm -f "$OVERRIDE_FILE"
	[ "$WROTE_E2E_PORTS" = 0 ] || rm -f "$E2E_PORTS_FILE"
	rm -rf "$STATE"
	exit "$rc"
}

stop_reason() {
	case "$(cat "$STATE/stop" 2>/dev/null || true)" in
	vm_disk_below_floor*) echo stopped_vm_disk ;;
	docker_unresponsive*) echo stopped_docker_unresponsive ;;
	*) echo stopped_terminated ;;
	esac
}

write_stop_once() {
	if [ "$STOP_WRITTEN" = 0 ]; then
		STOP_WRITTEN=1
		ev "STOP,reason=$(cat "$STATE/stop" 2>/dev/null || echo terminated)"
	fi
}

nr() { ev "NOT_RUN,step=$1,reason=$(san "$2")"; }

bg_run() {
	"$@" &
	CUR_PID=$!
	local rc=0
	wait "$CUR_PID" || rc=$?
	if [ "$STOPPED" = 1 ]; then
		wait "$CUR_PID" 2> /dev/null || true
	fi
	CUR_PID=""
	return "$rc"
}

volume_list() { tmo 60 docker volume ls -q < /dev/null | sort; }

dirt_count() {
	local out
	if out=$(git status --porcelain --untracked-files=no); then
		printf '%s\n' "$out" | grep -c . || true
	else
		echo unknown
	fi
}

trap cleanup EXIT
trap on_term TERM INT

COMMIT=$(git rev-parse HEAD)
TRACKED_DIRT=$(dirt_count)
FREE0=$(vm_free_kib || true)
ev "HDR,date_utc=$(utc)"
ev "HDR,commit=$COMMIT"
ev "HDR,tracked_dirt=$TRACKED_DIRT"
ev "HDR,branch=$(git rev-parse --abbrev-ref HEAD)"
ev "HDR,clone=$ROOT"
ev "HDR,go=$(GOTOOLCHAIN=go1.26.4 go version 2> /dev/null | awk '{ print $3 }' || true)"
ev "HDR,server_arch=$(tmo 60 docker version --format '{{.Server.Arch}}' < /dev/null 2> /dev/null || true)"
ev "HDR,load1=$(load1 || true)"
ev "HDR,vm_free_gib=$(awk -v k="${FREE0:-0}" 'BEGIN { printf "%.2f", k / 1048576 }')"
ev "HDR,host_free_gib=$(df -k "$ROOT" | awk 'NR==2 { printf "%.2f", $4 / 1048576 }')"
ev "HDR,python3=$(python3 --version 2>&1 | awk '{ print $2 }' || true)"
ev "HDR,steps=$(echo "$STEPS_ARG" | tr ',' ';')"

volume_list > "$STATE/vol_before" || true
ev "VOLUMES,phase=before,count=$(grep -c . "$STATE/vol_before" || true),sha256=$(shasum -a 256 < "$STATE/vol_before" | cut -d' ' -f1)"
tmo 60 docker images --no-trunc --format '{{.Repository}}:{{.Tag}} {{.ID}}' < /dev/null 2> /dev/null | awk '$1 ~ /^octoconv-/ { print $2 }' | sort -u > "$STATE/images_before" || true

watchdog &
WD_PID=$!
sampler &
SAMPLER_PID=$!

step_preflight() {
	local ok=1 k n out krc h p r prc
	pf() {
		ev "PREFLIGHT,check=$1,result=$2${3:+,$3}"
		[ "$2" = ok ] || ok=0
	}
	if [ "$(git rev-parse --git-dir)" = ".git" ]; then pf git_dir ok; else pf git_dir fail "git_dir=$(git rev-parse --git-dir)"; fi
	if [ "$TRACKED_DIRT" = unknown ]; then pf tracked_dirt fail "count=unknown"; else pf tracked_dirt ok "count=$TRACKED_DIRT"; fi
	for t in docker kubectl lsof jq perl shasum npm curl python3 go git; do
		command -v "$t" > /dev/null 2>&1 || pf tool fail "missing=$t"
	done
	prc=0
	out=$(tmo 60 docker ps -q --filter label=com.docker.compose.project=octoconv < /dev/null 2>&1) || prc=$?
	if [ "$prc" != 0 ]; then
		pf compose_running fail "docker_ps_rc=$prc"
	else
		r=$(printf '%s\n' "$out" | grep -c . || true)
		if [ "$r" = 0 ]; then pf compose_running ok "count=0"; else pf compose_running fail "count=$r"; fi
	fi
	krc=0
	out=$(tmo 60 kubectl get pods -A --no-headers < /dev/null 2>&1) || krc=$?
	if [ "$krc" != 0 ]; then
		pf k8s fail "kubectl_rc=$krc"
	else
		n=$(printf '%s\n' "$out" | grep -ciE 'octoconv|keda' || true)
		if [ "$n" = 0 ]; then pf k8s ok "octoconv_or_keda_pods=0"; else pf k8s fail "octoconv_or_keda_pods=$n"; fi
	fi
	for p in $CHECK_PORTS; do
		h=$(lsof -nP -iTCP:"$p" -sTCP:LISTEN 2> /dev/null | awk 'NR==2 { print $1 "/" $2 }' || true)
		if [ -z "$h" ]; then pf port ok "port=$p"; else pf port fail "port=$p,holder=$h"; fi
	done
	k=$(vm_free_kib || true)
	case "$k" in
	'' | *[!0-9]*) pf vm_disk fail "free_kib=unknown" ;;
	*)
		if [ "$k" -lt "$FLOOR_KIB" ]; then
			echo "vm_disk_below_floor,free_kib=$k" > "$STATE/stop"
			STOPPED=1
			return 2
		fi
		pf vm_disk ok "free_kib=$k,floor_kib=$FLOOR_KIB"
		;;
	esac
	if [ -s deploy/tectonic-bundle/octoconv-bundle.ttb ]; then
		pf tectonic_bundle ok "bytes=$(wc -c < deploy/tectonic-bundle/octoconv-bundle.ttb | tr -d ' ')"
	else
		pf tectonic_bundle fail "missing=deploy/tectonic-bundle/octoconv-bundle.ttb"
	fi
	if GOTOOLCHAIN=go1.26.4 go version 2> /dev/null | grep -q ' go1\.26\.4 '; then pf go ok "go=go1.26.4"; else pf go fail "go=$(GOTOOLCHAIN=go1.26.4 go version 2>&1 | tr ' ' '_')"; fi
	if [ -e "$OVERRIDE_FILE" ] || [ -e "$E2E_PORTS_FILE" ]; then
		pf override_files fail "existing=$([ -e "$OVERRIDE_FILE" ] && echo override)$([ -e "$E2E_PORTS_FILE" ] && echo e2e_ports)"
	elif [ "$ok" = 1 ]; then
		printf 'services:\n  api:\n    ports: !override\n      - "127.0.0.1:18090:8090"\n      - "8091:8091"\n' > "$OVERRIDE_FILE"
		WROTE_OVERRIDE=1
		printf 'services:\n  api:\n    ports: !override\n      - "127.0.0.1:18090:8090"\n      - "8091:8091"\n      - "9190:9090"\n' > "$E2E_PORTS_FILE"
		WROTE_E2E_PORTS=1
		pf override_files ok "written=docker-compose.override.yml;docker-compose.e2e-ports.yml"
	fi
	if [ "$ok" = 1 ]; then
		PREFLIGHT_OK=1
		return 0
	fi
	return 1
}

hms_epoch() {
	local hms=$1 base=$2 h m s day e
	IFS=: read -r h m s <<< "$hms"
	day=$((base - base % 86400))
	e=$((day + 10#$h * 3600 + 10#$m * 60 + 10#$s))
	[ "$e" -ge $((base - 60)) ] || e=$((e + 86400))
	echo "$e"
}

load_at() { awk -v t="$1" 'BEGIN { bd = -1 } { d = $1 - t; if (d < 0) d = -d; if (bd < 0 || d < bd) { bd = d; l = $2 } } END { print l }' "$STATE/loads" 2> /dev/null; }

images_check() {
	local stack=$1 services file svc line id img tag found=0 expected=0 mm=0 pre=0 pairs="" cids
	services=$(tmo 120 docker compose -f docker-compose.yml config --format json < /dev/null 2> /dev/null | jq -r '.services | to_entries[] | select(.value.build != null) | .key' | sort || true)
	file="$STATE/inspect.$stack"
	tmo 60 docker ps -aq --filter label=com.docker.compose.project=octoconv < /dev/null > "$STATE/cids.$stack" 2> /dev/null || true
	cids=()
	while IFS= read -r line; do
		[ -z "$line" ] || cids+=("$line")
	done < "$STATE/cids.$stack"
	if [ "${#cids[@]}" -gt 0 ]; then
		tmo 60 docker inspect -f '{{index .Config.Labels "com.docker.compose.service"}} {{.Config.Image}} {{.Image}}' "${cids[@]}" < /dev/null 2> /dev/null | sort > "$file" || true
	else
		: > "$file"
	fi
	for svc in $services; do
		expected=$((expected + 1))
		line=$(awk -v s="$svc" '$1 == s { print; exit }' "$file")
		[ -n "$line" ] || continue
		found=$((found + 1))
		img=$(echo "$line" | awk '{ print $2 }')
		id=$(echo "$line" | awk '{ print $3 }')
		tag=$(tmo 60 docker image inspect -f '{{.Id}}' "$img" < /dev/null 2> /dev/null || true)
		[ "$tag" = "$id" ] || mm=$((mm + 1))
		if grep -qxF "$id" "$STATE/images_before"; then pre=$((pre + 1)); fi
		pairs="$pairs$svc=$id;"
	done
	ev "IMAGES,stack=$stack,${pairs%;}"
	if [ "$expected" -gt 0 ] && [ "$found" = "$expected" ] && [ "$mm" = 0 ]; then
		ev "IMAGES_CHECK,stack=$stack,built_services=$found,expected=$expected,tag_mismatch=$mm,ids_present_before_run=$pre,result=ok"
		return 0
	fi
	ev "IMAGES_CHECK,stack=$stack,built_services=$found,expected=$expected,tag_mismatch=$mm,ids_present_before_run=$pre,result=fail"
	return 1
}

step_ci_local() {
	local before after rc=0 ok=1 notrun=0 sec shms ehms sepoch eepoch ls le l start_epoch res
	[ "$PREFLIGHT_OK" = 1 ] || {
		nr ci-local needs_preflight
		return 2
	}
	COMPOSE_TOUCHED=1
	CI_START_UTC=$(utc)
	start_epoch=$(date +%s)
	before=$(ls "$EVD"/ci-local-*.log 2> /dev/null | sort || true)
	bg_run tmo 14400 env CI_LOCAL_EVIDENCE_DIR="$EVD" CI_LOCAL_E2E_API_PORT=18090 CI_LOCAL_E2E_EXTRA_COMPOSE_FILES="$E2E_PORTS_FILE" CI_LOCAL_E2E_TEARDOWN=stop CI_LOCAL_E2E_GO_TEST_FLAGS=-v GOTOOLCHAIN=go1.26.4 scripts/ci-local.sh helm gate race docker-build e2e > /dev/null 2>&1 || rc=$?
	STEP_RC=$rc
	[ "$STOPPED" = 0 ] || return 2
	after=$(ls "$EVD"/ci-local-*.log 2> /dev/null | sort || true)
	CI_LOG=$(comm -13 <(printf '%s\n' "$before") <(printf '%s\n' "$after") | grep . | tail -1 || true)
	if [ -z "$CI_LOG" ]; then
		ev "CI_LOCAL,rc=$rc,transcript=missing,result=fail"
		return 1
	fi
	STEP_TRANSCRIPT=$(basename "$CI_LOG")
	[ "$rc" = 0 ] || ok=0
	grep -q 'ALL REQUESTED TIERS PASSED' "$CI_LOG" || ok=0
	ev "CI_LOCAL,rc=$rc,all_requested_tiers_passed=$(grep -c 'ALL REQUESTED TIERS PASSED' "$CI_LOG" || true),transcript=$STEP_TRANSCRIPT"

	sec=$(awk '/^### tier: gate /{ g = 1; next } g && /^### tier gate: /{ g = 0 } g && $1 == "ok" && $2 == "github.com/apaderin/octoconv/internal/convert" { v = $3; sub(/s$/, "", v); print v }' "$CI_LOG" | tail -1)
	shms=$(sed -n 's/^### tier: gate (started \([0-9:]*\) UTC)$/\1/p' "$CI_LOG" | head -1)
	ehms=$(awk '/^### tier: gate /{ g = 1; next } g && /^### tier: /{ if (match($0, /[0-9][0-9]:[0-9][0-9]:[0-9][0-9]/)) print substr($0, RSTART, RLENGTH); exit }' "$CI_LOG")
	if [ -n "$shms" ]; then sepoch=$(hms_epoch "$shms" "$start_epoch"); else sepoch=$start_epoch; fi
	if [ -n "$ehms" ]; then eepoch=$(hms_epoch "$ehms" "$start_epoch"); else eepoch=$(date +%s); fi
	ls=$(load_at "$sepoch")
	le=$(load_at "$eepoch")
	l=$(awk -v a="${ls:-99}" -v b="${le:-99}" 'BEGIN { print (a + 0 >= b + 0) ? a : b }')
	if [ -z "$sec" ]; then
		res=missing
		ok=0
	elif awk -v s="$sec" -v b="$BUDGET_S" 'BEGIN { exit !(s <= b) }'; then
		res=ok
	elif awk -v l="$l" -v q="$D19_QUIET_LOAD" 'BEGIN { exit !(l < q) }'; then
		res=STOP
		ok=0
	else
		res=contaminated
		notrun=1
	fi
	ev "CONVERT_TIME,seconds=${sec:-missing},load1=$l,load1_start=${ls:-unknown},load1_end=${le:-unknown},budget_s=$BUDGET_S,result=$res"
	[ "$res" != STOP ] || ev "STOP,reason=convert_time_over_budget,seconds=$sec,load1=$l,budget_s=$BUDGET_S"
	[ "$res" != contaminated ] || nr ci-local "convert_time_over_budget_at_load1_$l"

	images_check e2e || ok=0
	[ "$ok" = 1 ] || return 1
	[ "$notrun" = 0 ] || return 2
	return 0
}

step_tectonic() {
	local p1 p2 failed landlock dlog denv dval img names="" ok=1 envout erc=0
	[ -n "$CI_LOG" ] || {
		nr tectonic needs_ci_local_transcript
		return 2
	}
	STEP_TRANSCRIPT="$(basename "$CI_LOG");76-11-tectonic-worker.log"
	p1=$(grep -cF -- '--- PASS: TestMultiFileLaTeXProjectE2E' "$CI_LOG" || true)
	p2=$(grep -cF -- '--- PASS: TestMultiFileRefusalsE2E' "$CI_LOG" || true)
	failed=$(grep -cE -- '^[[:space:]]*--- FAIL|^FAIL([[:space:]]|$)' "$CI_LOG" || true)
	tmo 60 docker logs --since "$CI_START_UTC" octoconv-tectonic-worker < /dev/null > "$EVD/76-11-tectonic-worker.log" 2>&1 || true
	landlock=$(grep -c 'tectonic sandbox: landlock via' "$EVD/76-11-tectonic-worker.log" || true)
	dlog=$(grep -c 'TECTONIC_SANDBOX_DISABLED' "$EVD/76-11-tectonic-worker.log" || true)
	envout=$(tmo 60 docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' octoconv-tectonic-worker < /dev/null 2> /dev/null) || erc=$?
	denv=$(printf '%s\n' "$envout" | grep -cx 'TECTONIC_SANDBOX_DISABLED=true' || true)
	dval=$(printf '%s\n' "$envout" | sed -n 's/^TECTONIC_SANDBOX_DISABLED=//p' | head -1)
	if [ "$erc" != 0 ]; then
		dval="unreadable"
		ok=0
	elif ! printf '%s\n' "$envout" | grep -q '^TECTONIC_SANDBOX_DISABLED='; then
		dval="unset"
	elif [ -z "$dval" ]; then
		dval="empty"
	fi
	img=$(tmo 60 docker inspect -f '{{.Image}}' octoconv-tectonic-worker < /dev/null 2> /dev/null || true)
	[ "$p1" -ge 1 ] && names="TestMultiFileLaTeXProjectE2E"
	[ "$p2" -ge 1 ] && names="${names:+$names;}TestMultiFileRefusalsE2E"
	[ "$p1" -ge 1 ] && [ "$p2" -ge 1 ] || ok=0
	[ "$failed" = 0 ] || ok=0
	[ "$landlock" -ge 1 ] || ok=0
	[ "$((dlog + denv))" = 0 ] || ok=0
	[ -n "$img" ] || ok=0
	ev "TECTONIC_DETAIL,pass_latex=$p1,pass_refusals=$p2,failed_test_lines=$failed,landlock_lines=$landlock,disabled_log_lines=$dlog,disabled_env=$denv,disabled_env_value=$(san "$dval"),since=$CI_START_UTC"
	if [ "$ok" = 1 ]; then
		ev "TECTONIC,e2e_pass=$names,landlock=1,sandbox_disabled=0,image_id=$img,result=ok"
		return 0
	fi
	ev "TECTONIC,e2e_pass=${names:-none},landlock=$([ "$landlock" -ge 1 ] && echo 1 || echo 0),sandbox_disabled=$((dlog + denv)),image_id=${img:-none},result=fail"
	STEP_RC=1
	return 1
}

step_sandbox() {
	local f="$EVD/76-11-sandbox-liveproof.txt" rc=0 last pairs left nrl lout lrc=0 cres
	STEP_TRANSCRIPT=76-11-sandbox-liveproof.txt
	bg_run tmo 14400 env GOTOOLCHAIN=go1.26.4 scripts/sandbox-liveproof.sh $SANDBOX_CLASSES > "$f" 2>&1 || rc=$?
	STEP_RC=$rc
	[ "$STOPPED" = 0 ] || return 2
	last=$(tail -1 "$f")
	pairs=$(awk '/^== [a-z]+: building /{ c = $2; sub(/:$/, "", c) } /^image id: /{ printf "%s%s=%s", sep, c, $3; sep = ";" }' "$f")
	ev "SANDBOX_IMAGES,${pairs:-none}"
	lout=$(tmo 60 docker images -q --filter 'reference=octoconv-*:sandbox-liveproof-*' < /dev/null 2> /dev/null) || lrc=$?
	if [ "$lrc" != 0 ]; then
		left=unknown
	else
		left=$(printf '%s\n' "$lout" | grep -c . || true)
	fi
	if [ "$lrc" = 0 ] && [ "$left" = 0 ]; then cres=ok; else cres=fail; fi
	ev "CLEANUP,step=sandbox,api_clients=0,minio_objects_deleted=0,leftover_tags=$left,reason=no_api_stack_and_no_clients,result=$cres"
	ev "SANDBOX,rc=$rc,last_line=$(san "$last")"
	nrl=$(grep -c '^NOT RUN: ' "$f" || true)
	if [ "$nrl" -gt 0 ]; then
		while IFS= read -r line; do
			nr sandbox "${line#NOT RUN: }"
		done < <(grep '^NOT RUN: ' "$f")
	fi
	if [ "$cres" = fail ]; then
		[ "$rc" != 0 ] || STEP_RC=1
		return 1
	fi
	if [ "$rc" = 0 ] && printf '%s\n' "$last" | grep -qE '^ALL PASS \([0-9]+ checks\)$' && [ "$nrl" = 0 ]; then
		return 0
	fi
	if [ "$rc" = 0 ] && printf '%s\n' "$last" | grep -qE '^ALL PASS \([0-9]+ checks, [0-9]+ not run\)$'; then
		[ "$nrl" -gt 0 ] || nr sandbox "not_run_count_without_lines"
		return 2
	fi
	[ "$rc" != 0 ] || STEP_RC=1
	return 1
}

cad_cleanup() {
	local since=$1 run=$2 user pass clients others ids id p line b o nj=0 inv=0 obj=0 bytes=0 bbefore bafter res=ok q1=0 q2=0 q3=0
	tmo 120 docker compose start postgres minio < /dev/null > /dev/null 2>&1 || res=fail
	for _ in $(seq 1 30); do
		tmo 30 docker compose exec -T postgres pg_isready -U octo -d octo_db < /dev/null > /dev/null 2>&1 && break
		sleep 2
	done
	user=$(tmo 120 docker compose config --format json < /dev/null 2> /dev/null | jq -r '.services.minio.environment.MINIO_ROOT_USER // empty' || true)
	pass=$(tmo 120 docker compose config --format json < /dev/null 2> /dev/null | jq -r '.services.minio.environment.MINIO_ROOT_PASSWORD // empty' || true)
	[ -n "$user" ] && [ -n "$pass" ] || res=fail
	clients=$(tmo 60 docker compose exec -T postgres psql -U octo -d octo_db -At -c "select coalesce(string_agg(name, ';' order by created_at), '') from clients where name like 'cad-liveproof-%' and created_at >= '$since'" < /dev/null 2> /dev/null) || q1=1
	others=$(tmo 60 docker compose exec -T postgres psql -U octo -d octo_db -At -c "select count(*) from clients where created_at >= '$since' and name not like 'cad-liveproof-%'" < /dev/null 2> /dev/null) || q2=1
	ids=$(tmo 60 docker compose exec -T postgres psql -U octo -d octo_db -At -c "select j.id from jobs j join clients c on c.id = j.client_id where c.name like 'cad-liveproof-%' and c.created_at >= '$since' order by j.created_at" < /dev/null 2> /dev/null) || q3=1
	[ "$q1$q2$q3" = 000 ] || res=fail
	tmo 60 docker compose exec -T minio mc alias set local http://localhost:9000 "$user" "$pass" < /dev/null > /dev/null 2>&1 || res=fail
	bbefore=$(tmo 300 docker compose exec -T minio mc du local/octoconv < /dev/null 2> /dev/null | awk '{ print $1 "_" $2 "_" $3 }' || true)
	if [ "$res" = ok ]; then
		while IFS= read -r id; do
			[ -n "$id" ] || continue
			nj=$((nj + 1))
			if ! [[ $id =~ $UUID_RE ]]; then
				inv=$((inv + 1))
				continue
			fi
			for p in uploads results; do
				line=$(tmo 120 docker compose exec -T minio mc du --json "local/octoconv/$p/$id/" < /dev/null 2> /dev/null | head -1 || true)
				b=$(printf '%s' "$line" | sed -n 's/.*"size":\([0-9]*\).*/\1/p')
				o=$(printf '%s' "$line" | sed -n 's/.*"objects":\([0-9]*\).*/\1/p')
				if [ -n "$o" ] && [ "$o" != 0 ]; then
					if tmo 300 docker compose exec -T minio mc rm --recursive --force "local/octoconv/$p/$id/" < /dev/null > /dev/null 2>&1; then
						obj=$((obj + o))
						bytes=$((bytes + ${b:-0}))
					else
						res=fail
					fi
				fi
			done
		done <<< "$ids"
	fi
	[ "$inv" = 0 ] || res=fail
	bafter=$(tmo 300 docker compose exec -T minio mc du local/octoconv < /dev/null 2> /dev/null | awk '{ print $1 "_" $2 "_" $3 }' || true)
	tmo 120 docker compose stop postgres minio < /dev/null > /dev/null 2>&1 || true
	ev "CLEANUP,step=cad,run=$run,scope=uploads_and_results_of_jobs_of_clients_named_cad-liveproof-*_created_since_$since,clients=${clients:-none},other_clients_since=${others:-unknown},jobs=$nj,objects_deleted=$obj,bytes_deleted=$bytes,invalid_ids=$inv,bucket_before=${bbefore:-unknown},bucket_after=${bafter:-unknown},result=$res"
	[ "$res" = ok ]
}

quiet_rows() {
	awk -F, -v q="$ROW_QUIET_LOAD" -v rows="$2" -v bad="$3" '
		{ key = $4; endl = $3 + 0; start = (NR == 1) ? endl : prev; prev = $5 + 0 }
		key == "HDR" || key == "RULE" || key == "RULE_AMEND" || key == "SUMMARY" { next }
		{ occ[key]++; id = key "#" occ[key]; print id > rows; if (start > q || endl > q) print id > bad }
	' "$1"
	touch "$2" "$3"
}

run_cad() {
	local n=$1 name=$2 since rc=0 f summ dirt cc ok=1 rows bad cres crc lf qres
	since=$(utc)
	f="$EVD/$name"
	bg_run tmo 21600 env CAD_LIVEPROOF_API_BASE=http://127.0.0.1:18090 CAD_LIVEPROOF_QUIET_LOAD="$ROW_QUIET_LOAD" GOTOOLCHAIN=go1.26.4 scripts/cad-liveproof.sh --out "$f" > "$EVD/${name%.txt}-console.txt" 2>&1 || rc=$?
	STEP_RC=$rc
	if [ "$STOPPED" = 1 ]; then
		cres=not_run
		if [ "$(stop_reason)" != stopped_docker_unresponsive ]; then
			crc=0
			cad_cleanup "$since" "$n" || crc=$?
			if [ "$crc" = 0 ]; then cres=ok; else cres=fail; fi
		fi
		ev "CAD_RUN,run=$n,rc=$rc,cleanup=$cres,transcript=$name,result=not_run,reason=$(stop_reason)"
		return 2
	fi
	cres=ok
	crc=0
	cad_cleanup "$since" "$n" || crc=$?
	if [ "$crc" != 0 ]; then
		cres=fail
		ok=0
	fi
	summ=$(grep -c '^SUMMARY,result=ALL PASS' "$f" 2> /dev/null || true)
	dirt=$(sed -n 's/^HDR,tracked_dirt=//p' "$f" 2> /dev/null | head -1)
	cc=$(sed -n 's/^HDR,commit=//p' "$f" 2> /dev/null | head -1)
	[ "$rc" = 0 ] || ok=0
	[ "$summ" = 1 ] || ok=0
	[ "$dirt" = 0 ] || ok=0
	[ "$cc" = "$COMMIT" ] || ok=0
	rows="$STATE/rows.$n"
	bad="$STATE/bad.$n"
	lf="${f%.txt}-load.txt"
	if [ -s "$lf" ]; then
		quiet_rows "$lf" "$rows" "$bad"
	else
		: > "$rows"
		: > "$bad"
	fi
	qres=ok
	if [ ! -s "$lf" ] || ! grep -q . "$rows"; then
		qres=fail
		ok=0
	fi
	ev "CAD_RUN,run=$n,rc=$rc,all_pass=$summ,tracked_dirt=${dirt:-missing},commit=${cc:-missing},commit_match=$([ "$cc" = "$COMMIT" ] && echo 1 || echo 0),cleanup=$cres,transcript=$name,result=$([ "$ok" = 1 ] && echo ok || echo fail)"
	ev "QUIET,step=cad,run=$n,rows=$(grep -c . "$rows" || true),contaminated=$(grep -c . "$bad" || true),bound=$ROW_QUIET_LOAD,contaminated_rows=$(tr '\n' ';' < "$bad" | sed 's/;$//'),result=$qres"
	[ "$ok" = 1 ]
}

step_cad() {
	local r=0 api cad mm=0 pre=0 t uncovered
	[ "$PREFLIGHT_OK" = 1 ] || {
		nr cad needs_preflight
		return 2
	}
	COMPOSE_TOUCHED=1
	STEP_TRANSCRIPT=76-11-liveproof.txt
	run_cad 1 76-11-liveproof.txt || r=$?
	[ "$STOPPED" = 0 ] || return 2
	api=$(sed -n 's/^HDR,api_image_id=//p' "$EVD/76-11-liveproof.txt" 2> /dev/null | head -1)
	cad=$(sed -n 's/^HDR,cad_worker_image_id=//p' "$EVD/76-11-liveproof.txt" 2> /dev/null | head -1)
	ev "IMAGES,stack=cad,api=${api:-missing};cad-worker=${cad:-missing}"
	t=$(tmo 60 docker image inspect -f '{{.Id}}' octoconv-api < /dev/null 2> /dev/null || true)
	[ -n "$api" ] && [ "$t" = "$api" ] || mm=$((mm + 1))
	t=$(tmo 60 docker image inspect -f '{{.Id}}' octoconv-cad-worker < /dev/null 2> /dev/null || true)
	[ -n "$cad" ] && [ "$t" = "$cad" ] || mm=$((mm + 1))
	grep -qxF "${api:-none}" "$STATE/images_before" && pre=$((pre + 1))
	grep -qxF "${cad:-none}" "$STATE/images_before" && pre=$((pre + 1))
	ev "IMAGES_CHECK,stack=cad,built_services=2,expected=2,tag_mismatch=$mm,ids_present_before_run=$pre,result=$([ "$mm" = 0 ] && echo ok || echo fail)"
	tmo 300 docker compose -p octoconv stop < /dev/null > /dev/null 2>&1 || true
	if [ "$r" != 0 ] || [ "$mm" != 0 ]; then
		[ "$STEP_RC" != 0 ] || STEP_RC=1
		return 1
	fi
	if [ -s "$STATE/bad.1" ]; then
		STEP_TRANSCRIPT="76-11-liveproof.txt;76-11-liveproof-rerun.txt"
		r=0
		run_cad 2 76-11-liveproof-rerun.txt || r=$?
		[ "$STOPPED" = 0 ] || return 2
		tmo 300 docker compose -p octoconv stop < /dev/null > /dev/null 2>&1 || true
		if [ "$r" != 0 ]; then
			[ "$STEP_RC" != 0 ] || STEP_RC=1
			return 1
		fi
		if cmp -s "$STATE/rows.1" "$STATE/rows.2"; then
			uncovered=$(comm -12 <(sort "$STATE/bad.1") <(sort "$STATE/bad.2") | grep -c . || true)
		else
			uncovered=$(grep -c . "$STATE/rows.1" || true)
		fi
		if [ "$uncovered" = 0 ]; then
			ev "QUIET_COVERAGE,step=cad,rows=$(grep -c . "$STATE/rows.1" || true),uncovered=0,result=ok"
		else
			ev "QUIET_COVERAGE,step=cad,rows=$(grep -c . "$STATE/rows.1" || true),uncovered=$uncovered,result=fail"
			nr cad "quiet_host_uncovered_rows_$uncovered"
			return 2
		fi
	fi
	return 0
}

step_frontend() {
	local f="$EVD/76-11-frontend.txt" rc_ci=0 rc_t=-1 rc_b=-1 lock=0 tl tf ok=1
	STEP_TRANSCRIPT=76-11-frontend.txt
	echo "== npm --prefix frontend ci ==" > "$f"
	bg_run tmo 1800 env NO_COLOR=1 npm --prefix frontend ci >> "$f" 2>&1 || rc_ci=$?
	echo "rc=$rc_ci" >> "$f"
	[ "$STOPPED" = 0 ] || return 2
	if [ "$rc_ci" = 0 ]; then
		echo "== npm --prefix frontend test ==" >> "$f"
		rc_t=0
		bg_run tmo 1800 env NO_COLOR=1 npm --prefix frontend test >> "$f" 2>&1 || rc_t=$?
		echo "rc=$rc_t" >> "$f"
		[ "$STOPPED" = 0 ] || return 2
	fi
	if [ "$rc_t" = 0 ]; then
		echo "== npm --prefix frontend run build ==" >> "$f"
		rc_b=0
		bg_run tmo 1800 env NO_COLOR=1 npm --prefix frontend run build >> "$f" 2>&1 || rc_b=$?
		echo "rc=$rc_b" >> "$f"
		[ "$STOPPED" = 0 ] || return 2
	fi
	git diff --quiet HEAD -- frontend/package.json frontend/package-lock.json || lock=1
	tl=$(perl -pe 's/\e\[[0-9;]*[A-Za-z]//g' "$f" | grep -E '^[[:space:]]*Tests[[:space:]]+[0-9]+ passed' | tail -1 || true)
	tf=$(perl -pe 's/\e\[[0-9;]*[A-Za-z]//g' "$f" | grep -E '^[[:space:]]*(Tests|Test Files)[[:space:]]' | grep -c 'failed' || true)
	[ "$rc_ci" = 0 ] && [ "$rc_t" = 0 ] && [ "$rc_b" = 0 ] || ok=0
	[ -n "$tl" ] || ok=0
	[ "$tf" = 0 ] || ok=0
	[ "$lock" = 0 ] || ok=0
	ev "FRONTEND,npm_ci_rc=$rc_ci,test_rc=$rc_t,build_rc=$rc_b,vitest=$(san "$(echo "$tl" | sed 's/^[[:space:]]*//')"),failed_summary_lines=$tf,lockfile_changed=$lock,result=$([ "$ok" = 1 ] && echo ok || echo fail)"
	if [ "$ok" = 1 ]; then
		STEP_RC=0
		return 0
	fi
	STEP_RC=$rc_ci
	[ "$STEP_RC" != 0 ] || STEP_RC=$rc_t
	[ "$STEP_RC" -gt 0 ] 2> /dev/null || STEP_RC=$rc_b
	[ "$STEP_RC" -gt 0 ] 2> /dev/null || STEP_RC=1
	return 1
}

for name in "${ALL_STEPS[@]}"; do
	if ! selected "$name"; then
		ev "STEP,name=$name,rc=na,result=NOT_RUN,started_utc=na,ended_utc=na,transcript=na"
		nr "$name" not_selected
		continue
	fi
	if [ "$STOPPED" = 1 ]; then
		write_stop_once
		ev "STEP,name=$name,rc=na,result=NOT_RUN,started_utc=na,ended_utc=na,transcript=na"
		nr "$name" "$(stop_reason)"
		continue
	fi
	if [ "$FAILED" = 1 ]; then
		ev "STEP,name=$name,rc=na,result=NOT_RUN,started_utc=na,ended_utc=na,transcript=na"
		nr "$name" after_fail
		continue
	fi
	started=$(utc)
	STEP_RC=0
	STEP_TRANSCRIPT=76-11-final-gate.txt
	r=0
	case "$name" in
	preflight) step_preflight || r=$? ;;
	ci-local) step_ci_local || r=$? ;;
	tectonic) step_tectonic || r=$? ;;
	sandbox) step_sandbox || r=$? ;;
	cad) step_cad || r=$? ;;
	frontend) step_frontend || r=$? ;;
	esac
	ended=$(utc)
	if [ "$STOPPED" = 1 ]; then
		write_stop_once
		ev "STEP,name=$name,rc=$STEP_RC,result=NOT_RUN,started_utc=$started,ended_utc=$ended,transcript=$STEP_TRANSCRIPT"
		nr "$name" "$(stop_reason)"
		continue
	fi
	case "$r" in
	0) res=PASS ;;
	2) res=NOT_RUN ;;
	*)
		res=FAIL
		FAILED=1
		[ "$STEP_RC" != 0 ] || STEP_RC=1
		;;
	esac
	ev "STEP,name=$name,rc=$STEP_RC,result=$res,started_utc=$started,ended_utc=$ended,transcript=$STEP_TRANSCRIPT"
done

[ -z "$WD_PID" ] || killtree "$WD_PID"
WD_PID=""
[ -z "$SAMPLER_PID" ] || killtree "$SAMPLER_PID"
SAMPLER_PID=""

volume_list > "$STATE/vol_after" || true
ev "VOLUMES,phase=after,count=$(grep -c . "$STATE/vol_after" || true),sha256=$(shasum -a 256 < "$STATE/vol_after" | cut -d' ' -f1)"
VEQ=0
if [ -s "$STATE/vol_before" ] && cmp -s "$STATE/vol_before" "$STATE/vol_after"; then
	VEQ=1
fi
ev "VOLUMES,equal=$VEQ"
if [ "$VEQ" = 0 ]; then
	diff "$STATE/vol_before" "$STATE/vol_after" | sed -n 's/^\([<>]\) \(.*\)$/VOLUMES_DIFF,side=\1,name=\2/p' | sed 's/side=</side=before_only/; s/side=>/side=after_only/' >> "$OUT" || true
fi
DIRT_END=$(dirt_count)
ev "DIRT,phase=end,tracked_dirt=$DIRT_END"
ev "DISK,$(awk '{ for (i = 2; i <= NF; i++) { split($i, kv, "="); if (kv[1] == "free_kib" && kv[2] ~ /^[0-9]+$/) { if (m == "" || kv[2] + 0 < m + 0) m = kv[2]; n++ } if (kv[1] == "host_free_kib" && kv[2] ~ /^[0-9]+$/) { if (h == "" || kv[2] + 0 < h + 0) h = kv[2] } } } END { printf "vm_free_min_kib=%s,samples=%d,host_free_min_kib=%s,floor_kib='"$FLOOR_KIB"'", (m == "" ? "unknown" : m), n, (h == "" ? "unknown" : h) }' "$WD_LOG")"

P=$(grep -cE '^STEP,name=[a-z-]+,rc=[^,]+,result=PASS,' "$OUT" || true)
FC=$(grep -cE '^STEP,name=[a-z-]+,rc=[^,]+,result=FAIL,' "$OUT" || true)
NRC=$(grep -c '^NOT_RUN,' "$OUT" || true)
CODE=0
if [ "$FC" -gt 0 ] || [ "$VEQ" = 0 ] || [ "$TRACKED_DIRT" != 0 ] || [ "$DIRT_END" != 0 ]; then
	RESULT=FAIL
	CODE=1
elif [ "$STOPPED" = 1 ] || [ "$NRC" -gt 0 ] || [ "$P" != 6 ]; then
	RESULT=INCOMPLETE
	CODE=2
else
	RESULT="ALL PASS"
fi
if [ "$STOPPED" = 1 ]; then
	case "$(stop_reason)" in
	stopped_vm_disk) CODE=3 ;;
	*) [ "$CODE" != 0 ] || CODE=2 ;;
	esac
fi
ev "VERDICT,fail_steps=$FC,not_run=$NRC,volumes_equal=$VEQ,tracked_dirt=$TRACKED_DIRT,tracked_dirt_end=$DIRT_END,stopped=$STOPPED,exit=$CODE"
ev "SUMMARY,result=$RESULT,commit=$COMMIT,steps=6,pass=$P,fail=$FC,not_run=$NRC"
exit "$CODE"
