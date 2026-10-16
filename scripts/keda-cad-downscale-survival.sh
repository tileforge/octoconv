#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT=$(pwd)

NAMESPACE="octoconv"
KEDA_NAMESPACE="keda"
KEDA_VERSION="2.20.1"
CHART_DIR="deploy/chart/octoconv"
VALUES="$CHART_DIR/values.yaml"
VALUES_LOCAL="$CHART_DIR/values-local.yaml"
VALUES_LOADPROOF="$CHART_DIR/values-loadproof.yaml"
CONFIGMAP="$CHART_DIR/templates/configmap.yaml"
API_LOCAL_PORT="18099"
DB_LOCAL_PORT="15443"
API_BASE="http://127.0.0.1:${API_LOCAL_PORT}"
SCALED_OBJECT="worker-cad-scaledobject"
CAD_LABEL="app.kubernetes.io/component=cad-worker"
APISERVICE="v1beta1.external.metrics.k8s.io"
FLOOR_KIB=6291456
CORPUS="internal/convert/testdata/cad"
IMAGES="api cad-worker webhook-worker mcp-http"
PHASE_EVIDENCE="$ROOT/.planning/phases/76-measured-bounds-keda-proof-client-docs/evidence"
CELLS="$PHASE_EVIDENCE/76-03-cells.txt"
INWORKER="$PHASE_EVIDENCE/76-03-inworker.txt"
DERIVED="$PHASE_EVIDENCE/76-03-derived.txt"
CAD_CACHE="${OCTOCONV_CAD_CACHE:-$HOME/.cache/octoconv/cad-corpus}"
EVIDENCE_DIR="$PHASE_EVIDENCE"
TRANSPORT_CAP=104857600
BAND_LOW=180
SCALEUP_WINDOW_S=300
CENSUS_WINDOW_S=120
DOWNSCALE_WINDOW_S=240
QUIET_LOAD=11
QUIET_WAIT_S=3600
CALIBRATE_ONLY=0

usage() { echo "usage: $0 [--calibrate-only] [--evidence-dir DIR] [--quiet-load 11]" >&2; exit 64; }
while [ $# -gt 0 ]; do
	case "$1" in
	--calibrate-only) CALIBRATE_ONLY=1; shift ;;
	--evidence-dir) [ $# -ge 2 ] || usage; EVIDENCE_DIR=$2; shift 2 ;;
	--quiet-load) [ $# -ge 2 ] || usage; QUIET_LOAD=$2; shift 2 ;;
	*) usage ;;
	esac
done
case "$QUIET_LOAD" in '' | *[!0-9]*) usage ;; esac

for tool in kubectl helm docker jq curl perl awk lsof git go orbctl shasum; do
	command -v "$tool" >/dev/null || { echo "error: $tool is required" >&2; exit 69; }
done

RUN_TS=$(date -u +%Y%m%dT%H%M%SZ)
mkdir -p "$EVIDENCE_DIR"
CAL_OUT="$EVIDENCE_DIR/76-10-keda-downscale-calibration.txt"
if [ "$CALIBRATE_ONLY" = 1 ]; then
	MODE="keda_cad_downscale_calibration"
	OUT="$CAL_OUT"
	LOG_FILE="$EVIDENCE_DIR/76-10-keda-downscale-calibration-transcript-${RUN_TS}.log"
else
	MODE="keda_cad_downscale_survival"
	OUT="$EVIDENCE_DIR/76-10-keda-downscale.txt"
	LOG_FILE="$EVIDENCE_DIR/76-10-keda-downscale-transcript-${RUN_TS}.log"
	[ -s "$CAL_OUT" ] || { echo "error: $CAL_OUT is missing; run --calibrate-only first" >&2; exit 66; }
	grep -q "^SUMMARY,mode=keda_cad_downscale_calibration,.*result=PASS" "$CAL_OUT" || { echo "error: $CAL_OUT has no PASS summary" >&2; exit 66; }
fi
W=$(mktemp -d "${TMPDIR:-/tmp}/keda-cad-downscale.XXXXXX")
: > "$OUT"
exec > >(tee "$LOG_FILE") 2>&1

PASS_COUNT=0
GATE_OK=""
STEP="init"
API_PF_PID=""
DB_PF_PID=""
SAMPLER_PID=""
WATCH_PID=""
CAD_IMAGE_ID=""
INSTALLED_KEDA=0
INSTALLED_OCTOCONV=0
SNAP_TAKEN=0
BUSY_POD=""
TRANS_DUMPED=0
PORTS_FREE=0

kt() {
	local n=$1
	shift
	perl -e '
		my $n = shift @ARGV;
		my $pid = fork();
		die "fork: $!" unless defined $pid;
		if ($pid == 0) { exec @ARGV or exit 127; }
		local $SIG{ALRM} = sub { kill "KILL", $pid; waitpid($pid, 0); print STDERR "timeout after ${n}s: @ARGV\n"; exit 124; };
		local $SIG{TERM} = sub { kill "KILL", $pid; waitpid($pid, 0); exit 143; };
		alarm $n;
		waitpid($pid, 0);
		alarm 0;
		exit($? >> 8);
	' "$n" "$@"
}

k() { kt 60 kubectl "$@"; }
kn() { kt 60 kubectl -n "$NAMESPACE" "$@"; }

ev() { printf '%s\n' "$*" | tee -a "$OUT"; }

fail() {
	echo "FAIL: $*" >&2
	ev "FAIL,step=$STEP,reason=$(printf '%s' "$*" | tr ',\n' '; ')"
	exit 1
}

pass() {
	PASS_COUNT=$((PASS_COUNT + 1))
	echo "PASS: $*"
}

step() { STEP=$1; echo ""; echo "--- $* ---"; }

nowf() { perl -MTime::HiRes=time -e 'printf "%.3f\n", time'; }

iso_at() { perl -MPOSIX=strftime -e 'my $t = shift; printf "%s.%03dZ\n", strftime("%Y-%m-%dT%H:%M:%S", gmtime(int($t))), int(($t - int($t)) * 1000)' "$1"; }

epoch_of() {
	perl -MTime::Local=timegm -e '
		my $s = shift;
		$s =~ /^(\d{4})-(\d\d)-(\d\d)[T ](\d\d):(\d\d):(\d\d)(\.\d+)?(Z|[+-]00(:?00)?)?$/ or die "bad timestamp $s\n";
		printf "%.3f\n", timegm($6, $5, $4, $3, $2 - 1, $1) + ($7 // 0);
	' "$1"
}

diff_s() { awk -v a="$1" -v b="$2" 'BEGIN { printf "%.1f", b - a }'; }

le() { awk -v a="$1" -v b="$2" 'BEGIN { exit !(a <= b) }'; }

lt() { awk -v a="$1" -v b="$2" 'BEGIN { exit !(a < b) }'; }

load1() { sysctl -n vm.loadavg | awk '{ print $2 }'; }

vm_free_kib() { kt 120 docker run --rm alpine df -k / | awk 'NR == 2 { print $4 }'; }

gib() { awk -v k="$1" 'BEGIN { printf "%.2f", k / 1048576 }'; }

over() { awk -v l="$1" -v q="$QUIET_LOAD" 'BEGIN { exit !(l > q) }'; }

wait_quiet() {
	local waited=0 l
	while :; do
		l=$(load1)
		if ! over "$l"; then
			echo "$l"
			return 0
		fi
		[ "$waited" -lt "$QUIET_WAIT_S" ] || return 1
		sleep 15
		waited=$((waited + 15))
	done
}

quiet_line() {
	local step=$1 l0=$2 l1=$3 lmax=$4 c=0
	if over "$l0" || over "$l1"; then c=1; fi
	ev "QUIET,step=$step,load1_start=$l0,load1_end=$l1,load1_max_sampled=$lmax,quiet_load=$QUIET_LOAD,contaminated=$c"
	[ "$c" = 0 ]
}

yaml_cad_value() {
	awk -v key="$2" '
		/^[[:space:]]*#/ || /^[[:space:]]*$/ { next }
		{
			match($0, /^ */); ind = RLENGTH; t = $0; sub(/^ */, "", t); sub(/[[:space:]]+$/, "", t)
			if (ind == 0) { keda = (t == "keda:"); cad = 0; next }
			if (keda && ind == 2) { cad = (t == "cad:"); next }
			if (cad && ind == 4) { kk = t; sub(/:.*/, "", kk); v = t; sub(/^[^:]*:[[:space:]]*/, "", v); gsub(/"/, "", v); if (kk == key) print v }
		}
	' "$1"
}

configmap_value() { sed -n "s/^[[:space:]]*$1:[[:space:]]*\"\{0,1\}\([^\"]*\)\"\{0,1\}[[:space:]]*\$/\1/p" "$CONFIGMAP" | head -n 1; }

derived_value() { sed -n "s/^DERIVED,key=$1,value=\([0-9]*\),.*/\1/p" "$DERIVED" | head -n 1; }

snapshot() {
	local f=$1
	{
		kt 60 helm list -A -o json | jq -r '.[] | "helm,item=\(.namespace)/\(.name)"'
		k get ns -o json | jq -r '.items[].metadata.name | "ns,item=\(.)"'
		k get crd -o json | jq -r '.items[].metadata.name | select(endswith("keda.sh")) | "crd,item=\(.)"'
		if k get apiservice "$APISERVICE" >/dev/null 2>&1; then echo "apiservice,item=$APISERVICE,present=1"; else echo "apiservice,item=$APISERVICE,present=0"; fi
		k get pods -A -o json | jq -r --arg a "$NAMESPACE" --arg b "$KEDA_NAMESPACE" '.items[] | select(.metadata.namespace != $a and .metadata.namespace != $b) | "pod,item=\(.metadata.namespace)/\(.metadata.name),restarts=\([.status.containerStatuses[]?.restartCount] | add // 0)"'
	} | LC_ALL=C sort > "$f"
	[ -s "$f" ]
}

diag_ns() {
	{
		echo "## pods"
		kt 60 kubectl -n "$NAMESPACE" get pods -o wide
		echo "## deployments"
		kt 60 kubectl -n "$NAMESPACE" get deployments
		echo "## scaledobjects"
		kt 60 kubectl -n "$NAMESPACE" get scaledobject
		echo "## hpa"
		kt 60 kubectl -n "$NAMESPACE" get hpa -o wide
		echo "## events"
		kt 60 kubectl -n "$NAMESPACE" get events --sort-by=.lastTimestamp | tail -n 60
		echo "## node requests"
		kt 60 kubectl describe node | sed -n '/Allocated resources/,/Events/p'
		echo "## keda-operator"
		kt 60 kubectl -n "$KEDA_NAMESPACE" logs -l app=keda-operator --tail=60
	} 2>&1 | sed 's/^/DIAG: /'
}

was_absent() { ! grep -qx "$1" "$W/snap-before"; }

deploy_counts() {
	kn get deployment cad-worker -o json 2>/dev/null | jq -r '"\(.spec.replicas // 0) \(.status.replicas // 0) \(.status.readyReplicas // 0)"'
}

cad_replicas() { deploy_counts | awk '{ print $2 }'; }

live_cad_pods() {
	kn get pod -l "$CAD_LABEL" -o json | jq -r '[.items[] | select(.metadata.deletionTimestamp == null and .status.phase == "Running")] | sort_by(.metadata.creationTimestamp) | .[].metadata.name'
}

stop_group() {
	local pid=$1 flag=$2
	if [ -n "$pid" ]; then
		touch "$W/$flag"
		kill -- -"$pid" >/dev/null 2>&1 || true
		wait "$pid" 2>/dev/null || true
	fi
}

stop_sampler() { stop_group "$SAMPLER_PID" sampler.stop; SAMPLER_PID=""; }

stop_watch() { stop_group "$WATCH_PID" watch.stop; WATCH_PID=""; }

stop_pf() {
	local pid=$1 pattern=$2
	if [ -n "$pid" ]; then
		pkill -TERM -P "$pid" >/dev/null 2>&1 || true
		sleep 1
		kill "$pid" >/dev/null 2>&1 || true
		wait "$pid" 2>/dev/null || true
		pkill -TERM -f "$pattern" >/dev/null 2>&1 || true
	fi
}

ports_free() {
	local port
	for port in "$API_LOCAL_PORT" "$DB_LOCAL_PORT"; do
		if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then return 1; fi
	done
}

teardown() {
	local exit_code=$? restore=ok n
	set +e
	echo ""
	echo "=== TEARDOWN ==="
	stop_sampler
	stop_watch
	if [ "$TRANS_DUMPED" != 1 ] && [ -s "$W/transitions" ]; then sed 's/^/PARTIAL_/' "$W/transitions" | tee -a "$OUT"; fi
	stop_pf "$API_PF_PID" "kubectl port-forward -n $NAMESPACE svc/api ${API_LOCAL_PORT}:8090"
	stop_pf "$DB_PF_PID" "kubectl port-forward -n $NAMESPACE svc/postgres ${DB_LOCAL_PORT}:5432"
	PORTS_FREE=0
	for _ in $(seq 1 10); do
		if ports_free; then PORTS_FREE=1; break; fi
		sleep 1
	done
	if [ "$INSTALLED_OCTOCONV" = 1 ]; then
		kt 420 helm uninstall octoconv -n "$NAMESPACE" --wait --timeout 6m
	fi
	if [ "$INSTALLED_KEDA" = 1 ]; then
		kt 420 helm uninstall keda -n "$KEDA_NAMESPACE" --wait --timeout 6m
	fi
	if [ "$SNAP_TAKEN" = 1 ]; then
		for ns in "$NAMESPACE" "$KEDA_NAMESPACE"; do
			if was_absent "ns,item=$ns" && k get ns "$ns" >/dev/null 2>&1; then
				kt 1000 kubectl delete ns "$ns" --wait=true --timeout=960s
			fi
		done
		for crd in $(k get crd -o json | jq -r '.items[].metadata.name | select(endswith("keda.sh"))'); do
			if was_absent "crd,item=$crd"; then kt 120 kubectl delete crd "$crd" --wait=true --timeout=100s; fi
		done
		if grep -qx "apiservice,item=$APISERVICE,present=0" "$W/snap-before" && k get apiservice "$APISERVICE" >/dev/null 2>&1; then
			kt 120 kubectl delete apiservice "$APISERVICE" --wait=true --timeout=100s
		fi
		for _ in $(seq 1 60); do
			n=$(k get pods -A --no-headers 2>/dev/null | awk -v a="$NAMESPACE" -v b="$KEDA_NAMESPACE" '$1 == a || $1 == b' | wc -l | tr -d ' ')
			[ "$n" = 0 ] && break
			sleep 5
		done
		snapshot "$W/snap-after" || true
		if ! cmp -s "$W/snap-before" "$W/snap-after"; then
			restore=FAIL
			echo "RESTORE diff (< before, > after):"
			diff "$W/snap-before" "$W/snap-after"
		fi
		if [ "$(kt 30 orbctl config get k8s.enable 2>/dev/null)" != true ]; then restore=FAIL; fi
		[ "$PORTS_FREE" = 1 ] || restore=FAIL
		ev "RESTORE,snapshot_lines=$(wc -l < "$W/snap-before" | tr -d ' '),diff_lines=$(diff "$W/snap-before" "$W/snap-after" 2>/dev/null | grep -c '^[<>]'),octoconv_pods=${n:-unknown},k8s_enabled=$(kt 30 orbctl config get k8s.enable 2>/dev/null),local_ports_free=$PORTS_FREE,result=$restore"
	else
		restore=not_taken
	fi
	if [ "$exit_code" = 0 ] && [ "$GATE_OK" = 1 ] && [ "$restore" = ok ]; then
		ev "SUMMARY,mode=$MODE,checks=$PASS_COUNT,end_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ),end_load1=$(load1),result=PASS"
	else
		ev "SUMMARY,mode=$MODE,checks=$PASS_COUNT,exit=$exit_code,gate_ok=${GATE_OK:-0},restore=$restore,end_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ),result=FAIL"
		[ "$exit_code" != 0 ] || exit_code=1
	fi
	rm -rf "$W"
	exit "$exit_code"
}
trap teardown EXIT
trap 'exit 143' TERM
trap 'exit 130' INT

step "preflight"
COMMIT=$(git rev-parse HEAD)
DIRT=$(git status --porcelain --untracked-files=no | wc -l | tr -d ' ')
CONTEXT=$(k config current-context)
[ "$CONTEXT" = orbstack ] || fail "kubectl context is $CONTEXT, want orbstack"
[ "$(kt 30 orbctl config get k8s.enable)" = true ] || fail "orbctl reports k8s.enable is not true"
k get nodes >/dev/null || fail "kubectl does not reach the cluster"
if [ -n "$(kt 30 docker ps -q --filter label=com.docker.compose.project=octoconv)" ] || kt 30 docker ps --format '{{.Names}}' | grep -q '^octoconv-'; then
	fail "an octoconv compose container is running; run docker compose -p octoconv stop first (compose and k8s never run together)"
fi
pass "no octoconv compose container runs"
for port in "$API_LOCAL_PORT" "$DB_LOCAL_PORT"; do
	if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then fail "local port $port is in use"; fi
done
pass "ports $API_LOCAL_PORT and $DB_LOCAL_PORT are free"
FREE=$(vm_free_kib)
case "$FREE" in '' | *[!0-9]*) fail "cannot read the VM free disk" ;; esac
[ "$FREE" -ge "$FLOOR_KIB" ] || fail "VM free disk $(gib "$FREE") GiB is below the 6 GiB floor"
pass "VM free disk $(gib "$FREE") GiB"
kt 120 helm repo add kedacore https://kedacore.github.io/charts >/dev/null 2>&1 || true
kt 300 helm repo update kedacore >/dev/null
kt 60 helm search repo kedacore/keda --versions | awk '{ print $2 }' | grep -qx "$KEDA_VERSION" || fail "KEDA $KEDA_VERSION is not resolvable in kedacore/keda"
pass "KEDA $KEDA_VERSION resolvable"

for f in "$CELLS" "$INWORKER" "$DERIVED"; do
	[ -s "$f" ] || fail "missing $f"
done
THRESHOLD=$(yaml_cad_value "$VALUES" threshold)
MAXR=$(yaml_cad_value "$VALUES" maxReplicaCount)
MINR=$(yaml_cad_value "$VALUES" minReplicaCount)
POLL=$(yaml_cad_value "$VALUES" pollingInterval)
COOLDOWN=$(yaml_cad_value "$VALUES" cooldownPeriod)
PROD_STAB=$(yaml_cad_value "$VALUES" scaleDownStabilizationSeconds)
STAB=$(yaml_cad_value "$VALUES_LOADPROOF" scaleDownStabilizationSeconds)
for v in "$THRESHOLD" "$MAXR" "$MINR" "$POLL" "$COOLDOWN" "$PROD_STAB" "$STAB"; do
	case "$v" in '' | *[!0-9]*) fail "keda.cad in values.yaml or values-loadproof.yaml has a missing or non-integer value" ;; esac
done
[ "$MAXR" = 2 ] || fail "values.yaml keda.cad.maxReplicaCount is $MAXR; this proof needs exactly 2"
[ "$MINR" = 0 ] || fail "values.yaml keda.cad.minReplicaCount is $MINR; the proof starts from 0"
[ "$THRESHOLD" = 1 ] || fail "values.yaml keda.cad.threshold is $THRESHOLD; LONG plus SHORT must ask for 2 replicas"
ENGINE_S=$(configmap_value CAD_ENGINE_TIMEOUT | tr -d 's')
CONCURRENCY=$(configmap_value CAD_WORKER_CONCURRENCY)
UPLOAD_CAP=$(configmap_value MAX_UPLOAD_BYTES)
case "$ENGINE_S" in '' | *[!0-9]*) fail "configmap CAD_ENGINE_TIMEOUT is not whole seconds" ;; esac
[ "$CONCURRENCY" = 1 ] || fail "configmap CAD_WORKER_CONCURRENCY is $CONCURRENCY; one job per pod is required"
[ "$UPLOAD_CAP" = "$TRANSPORT_CAP" ] || fail "configmap MAX_UPLOAD_BYTES is $UPLOAD_CAP, want $TRANSPORT_CAP"
DERIVED_ENGINE=$(derived_value cad_engine_timeout_s)
DERIVED_GRACE=$(derived_value cad_pod_grace_s)
[ -n "$DERIVED_ENGINE" ] || fail "no DERIVED cad_engine_timeout_s in $DERIVED"
[ "$DERIVED_ENGINE" = "$ENGINE_S" ] || fail "configmap CAD_ENGINE_TIMEOUT $ENGINE_S differs from DERIVED $DERIVED_ENGINE"
EXPECTED_GRACE=$((DERIVED_ENGINE + 20))
[ "$DERIVED_GRACE" = "$EXPECTED_GRACE" ] || fail "DERIVED cad_pod_grace_s $DERIVED_GRACE differs from engine + 20 = $EXPECTED_GRACE"
BAND_HIGH=$((DERIVED_ENGINE * 2 / 3))
DRAIN_BOUND=$((COOLDOWN + STAB + 300))

so_window() { awk '/^---/ { f = 0 } /^  name: worker-cad-scaledobject$/ { f = 1 } f && /stabilizationWindowSeconds:/ { print $2; exit }'; }
cad_grace() { awk '/^---/ { d = 0; f = 0 } /^kind: Deployment$/ { d = 1 } d && /^  name: cad-worker$/ { f = 1 } f && /terminationGracePeriodSeconds:/ { print $2; exit }'; }
kt 60 helm template octoconv "$CHART_DIR" -f "$VALUES_LOCAL" -f "$VALUES_LOADPROOF" > "$W/render-overlay.yaml" || fail "helm template with the overlay failed"
kt 60 helm template octoconv "$CHART_DIR" -f "$VALUES_LOCAL" > "$W/render-local.yaml" || fail "helm template without the overlay failed"
R_OW=$(so_window < "$W/render-overlay.yaml")
R_PW=$(so_window < "$W/render-local.yaml")
R_OG=$(cad_grace < "$W/render-overlay.yaml")
R_PG=$(cad_grace < "$W/render-local.yaml")
[ "$R_OW" = "$STAB" ] && [ "$R_OW" = 15 ] || fail "the overlay render gives cad stabilizationWindowSeconds $R_OW, want 15"
[ "$R_PW" = "$PROD_STAB" ] || fail "the values-local render gives cad stabilizationWindowSeconds $R_PW, want $PROD_STAB"
[ "$R_OG" = "$EXPECTED_GRACE" ] && [ "$R_PG" = "$EXPECTED_GRACE" ] || fail "the cad Deployment grace renders $R_OG with the overlay and $R_PG without, want $EXPECTED_GRACE"
pass "the overlay shortens only the cad window; the grace stays $EXPECTED_GRACE"

ev "HDR,mode=$MODE,date_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ),commit=$COMMIT,tracked_dirt=$DIRT,load1=$(load1),vm_free_gib=$(gib "$FREE"),context=$CONTEXT,keda_version=$KEDA_VERSION,values=values-local.yaml+values-loadproof.yaml"
ev "HDR,keda_cad_threshold=$THRESHOLD,min_replica_count=$MINR,max_replica_count=$MAXR,polling_interval=$POLL,cooldown_period=$COOLDOWN,scale_down_stabilization_seconds=$STAB,production_scale_down_stabilization_seconds=$PROD_STAB,cad_worker_concurrency=$CONCURRENCY,cad_engine_timeout_s=$ENGINE_S,derived_engine_s=$DERIVED_ENGINE,expected_grace_s=$EXPECTED_GRACE,transport_cap_bytes=$UPLOAD_CAP,drain_bound_s=$DRAIN_BOUND"
ev "RENDER_CHECK,overlay_window_s=$R_OW,production_window_s=$R_PW,overlay_grace_s=$R_OG,production_grace_s=$R_PG,expected_grace_s=$EXPECTED_GRACE,result=ok"

CEIL_STEP=$(derived_value cad_step_ceiling_bytes)
CEIL_IGES=$(derived_value cad_iges_ceiling_bytes)
CEIL_STL=$(derived_value cad_stl_ceiling_bytes)
CEIL_DXF=$(derived_value cad_dxf_ceiling_bytes)
for v in "$CEIL_STEP" "$CEIL_IGES" "$CEIL_STL" "$CEIL_DXF"; do
	[ -n "$v" ] || fail "a DERIVED format ceiling is missing in $DERIVED"
done
PATHB_FIRED=$(grep -c '^PATHB,.*fired=1' "$DERIVED" || true)
[ "$PATHB_FIRED" = 0 ] || fail "a PATHB render ceiling fired in $DERIVED; the rule has no render ceiling source for it"
ev "RULE,key=long_select,source=evidence/76-03-cells.txt CELLSPEC lines of the last derive76_select block with result=ok;evidence/76-03-inworker.txt INWORKER_ROW lines with status=done;evidence/76-03-derived.txt format ceilings,transport_cap_bytes=$TRANSPORT_CAP,ceilings=step:$CEIL_STEP;iges:$CEIL_IGES;stl:$CEIL_STL;dxf:$CEIL_DXF,pathb_fired=0,rule=among CELLSPEC cells with operation=render whose bytes are at most the transport cap and at most the ceiling of their format (no PATHB fired so the render ceiling is the format ceiling) keep those whose largest in-worker attempt_wall_s lies in [band_low;band_high] and take the smallest such wall; the file is OCTOCONV_CAD_CACHE/scaled/<path after scaled/ in the CELLSPEC file> and its sha256 and bytes must equal the CELLSPEC line; no eligible cell writes STOP reason=no_long_candidate_in_band and exits 1"
ev "RULE,key=band,low=$BAND_LOW,high=$BAND_HIGH,derivation=high=floor(CAD_ENGINE_TIMEOUT/1.5)=floor($DERIVED_ENGINE/1.5),rule=the LONG attempt wall jobs.finished_at - jobs.started_at must lie in [low;high]"
ev "RULE,key=survival,rule=LONG is active while exactly one cad pod runs; SHORT then takes the Deployment 1->2 with 2 Ready within $SCALEUP_WINDOW_S s; exactly one Running pod holds an engine process (cad-guard;cad-mesh;cad-render;cad-dxf by comm or python cmdline;rsvg-convert by comm) and it is the earliest-created pod and the pod LONG went active on; it is annotated controller.kubernetes.io/pod-deletion-cost=-1000 while spec.replicas is still 2; its spec.terminationGracePeriodSeconds equals the DERIVED engine + 20 = $EXPECTED_GRACE; after SHORT is done status.replicas reaches 1 within $DOWNSCALE_WINDOW_S s; LONG ends done with attempts 1 and one queued->active and its wall in the band; the busy pod's Killing event precedes LONG's finished_at; the busy pod terminates Completed with exit 0 within $EXPECTED_GRACE s of the Killing event; the Deployment drains to 0 within $DRAIN_BOUND s of LONG's finished_at; TRANSITION timestamps never decrease"
ev "RULE,key=zero,rule=before each trigger cad-worker status.replicas is 0; the ScaledObject Fallback condition is not True and the external metric is present with value 0"
ev "RULE,key=quiet_host,rule=each trigger waits for load1 <= $QUIET_LOAD; a step whose load1 is above $QUIET_LOAD at its start or its end is recorded contaminated=1 and does not count"

for img in $IMAGES; do
	id=$(kt 30 docker image inspect "octoconv-$img:dev" --format '{{.Id}}' 2>/dev/null) || fail "missing image octoconv-$img:dev"
	ev "IMAGE,name=octoconv-$img:dev,local_id=$id"
	[ "$img" != cad-worker ] || CAD_IMAGE_ID=$id
done
[ -n "$CAD_IMAGE_ID" ] || fail "no cad-worker image id"
pass "the four :dev images are present"

snapshot "$W/snap-before" || fail "the snapshot before the proof failed"
SNAP_TAKEN=1
sed 's/^/SNAPSHOT_BEFORE,kind=/' "$W/snap-before" | tee -a "$OUT"
for x in "helm,item=$NAMESPACE/octoconv" "helm,item=$KEDA_NAMESPACE/keda" "ns,item=$NAMESPACE" "ns,item=$KEDA_NAMESPACE"; do
	was_absent "$x" || fail "$x already exists before this proof; refusing to touch it"
done
pass "no octoconv or keda release or namespace exists before the proof"

step "long_select"
awk '
	/^HDR,mode=derive76_select/ { n = 0; next }
	/^CELLSPEC,/ { b[++n] = $0; next }
	/^SUMMARY,mode=derive76_select,.*result=ok/ { out = ""; for (i = 1; i <= n; i++) out = out b[i] "\n"; n = 0 }
	END { printf "%s", out }
' "$CELLS" > "$W/cellspec"
[ -s "$W/cellspec" ] || fail "no CELLSPEC block with result=ok in $CELLS"
awk -F, -v cap="$TRANSPORT_CAP" -v lo="$BAND_LOW" -v hi="$BAND_HIGH" \
	-v c_step="$CEIL_STEP" -v c_iges="$CEIL_IGES" -v c_stl="$CEIL_STL" -v c_dxf="$CEIL_DXF" '
	function kv(key,   i, p) { for (i = 1; i <= NF; i++) { p = index($i, "="); if (p && substr($i, 1, p - 1) == key) return substr($i, p + 1) } return "" }
	BEGIN { ceil["step"] = c_step; ceil["iges"] = c_iges; ceil["stl"] = c_stl; ceil["dxf"] = c_dxf }
	FILENAME == ARGV[1] && /^INWORKER_ROW,/ && kv("status") == "done" {
		id = kv("route") SUBSEP kv("cell"); w = kv("attempt_wall_s") + 0
		if (!(id in wall) || w > wall[id]) wall[id] = w
		rows[id]++
		next
	}
	FILENAME == ARGV[2] && /^CELLSPEC,/ && kv("operation") == "render" {
		r = kv("route"); c = kv("cell"); id = r SUBSEP c; fmt = kv("format"); by = kv("bytes") + 0
		st = "eligible"
		if (!(fmt in ceil) || ceil[fmt] == "") st = "unknown_format"
		else if (by > cap) st = "over_transport_cap"
		else if (by > ceil[fmt] + 0) st = "over_format_ceiling"
		else if (!(id in wall)) st = "no_inworker_rows"
		else if (wall[id] < lo) st = "below_band"
		else if (wall[id] > hi) st = "above_band"
		printf "%s|%s|%s|%s|%s|%s|%s|%s|%d|%s|%d|%.2f|%s\n", r, c, kv("file"), kv("sha256"), kv("target"), kv("opts"), fmt, by, ceil[fmt], rows[id] + 0, (id in wall) ? 1 : 0, wall[id] + 0, st
	}
' "$INWORKER" "$W/cellspec" > "$W/candidates"
[ -s "$W/candidates" ] || fail "no render CELLSPEC cell in $CELLS"
ev "LONG_CANDIDATES,list=$(awk -F'|' '{ printf "%s%s:%s:%s:%s", (NR > 1 ? "+" : ""), $1, $2, $12, $13 }' "$W/candidates")"
PICK=$(awk -F'|' '$13 == "eligible"' "$W/candidates" | LC_ALL=C sort -t'|' -k12,12n | head -n 1)
if [ -z "$PICK" ]; then
	ev "STOP,reason=no_long_candidate_in_band,band_low=$BAND_LOW,band_high=$BAND_HIGH,candidates=$(awk -F'|' '{ printf "%s%s:%s:%s", (NR > 1 ? "+" : ""), $2, $12, $13 }' "$W/candidates")"
	exit 1
fi
IFS='|' read -r L_ROUTE L_CELL L_SPECFILE L_SHA L_TARGET L_OPTS L_FMT L_BYTES L_CEIL L_ROWS _ L_WALL _ <<< "$PICK"
case "$L_SPECFILE" in */scaled/*) ;; *) fail "the CELLSPEC file $L_SPECFILE is not under a scaled/ directory" ;; esac
L_FILE="$CAD_CACHE/scaled/${L_SPECFILE#*/scaled/}"
[ -s "$L_FILE" ] || fail "the LONG input $L_FILE is missing"
L_ACT_BYTES=$(wc -c < "$L_FILE" | tr -d ' ')
L_ACT_SHA=$(shasum -a 256 "$L_FILE" | cut -d' ' -f1)
[ "$L_ACT_SHA" = "$L_SHA" ] || fail "the LONG input sha256 $L_ACT_SHA differs from the CELLSPEC $L_SHA"
[ "$L_ACT_BYTES" = "$L_BYTES" ] || fail "the LONG input has $L_ACT_BYTES bytes, the CELLSPEC says $L_BYTES"
L_OPTS_JSON=$(printf '%s' "$L_OPTS" | awk -F';' '
	$0 == "" || $0 == "none" { exit }
	{
		printf "{"
		for (i = 1; i <= NF; i++) {
			p = index($i, ":"); kk = substr($i, 1, p - 1); vv = substr($i, p + 1)
			printf "%s\"%s\":", (i > 1 ? "," : ""), kk
			if (vv ~ /^[0-9]+$/) printf "%s", vv; else printf "\"%s\"", vv
		}
		printf "}"
	}')
L_NAME=$(basename "$L_FILE")
ev "LONG_SELECT,route=$L_ROUTE,cell=$L_CELL,file=$L_FILE,sha256=$L_ACT_SHA,bytes=$L_ACT_BYTES,target=$L_TARGET,operation=render,opts=$L_OPTS,format_ceiling_bytes=$L_CEIL,inworker_rows=$L_ROWS,expected_wall_s=$L_WALL,band_low=$BAND_LOW,band_high=$BAND_HIGH,result=ok"
pass "LONG is $L_ROUTE $L_CELL with an in-worker wall of $L_WALL s"
if [ "$CALIBRATE_ONLY" = 0 ]; then
	CAL_SEL=$(grep '^LONG_SELECT,' "$CAL_OUT" | head -n 1 | awk -F, '{ for (i = 1; i <= NF; i++) if ($i ~ /^(route|cell|sha256)=/) printf "%s;", $i }')
	THIS_SEL="route=$L_ROUTE;cell=$L_CELL;sha256=$L_ACT_SHA;"
	[ "$CAL_SEL" = "$THIS_SEL" ] || fail "LONG_SELECT gives $THIS_SEL; the calibration chose $CAL_SEL"
	ev "LONG_SELECT_MATCH,calibration=$(basename "$CAL_OUT"),selection=$(printf '%s' "$THIS_SEL" | sed 's/;$//'),result=ok"
	pass "the selection equals the calibration's"
fi
CAL_INPUT="$CORPUS/synthetic/units_inch.step"
[ -s "$CAL_INPUT" ] || fail "missing $CAL_INPUT"
ev "FIXTURE,name=units_inch.step,bytes=$(wc -c < "$CAL_INPUT" | tr -d ' '),sha256=$(shasum -a 256 "$CAL_INPUT" | cut -d' ' -f1)"

step "install KEDA $KEDA_VERSION"
INSTALLED_KEDA=1
kt 420 helm install keda kedacore/keda --namespace "$KEDA_NAMESPACE" --create-namespace --version "$KEDA_VERSION" --wait --timeout 6m
pass "KEDA $KEDA_VERSION installed"
ok=""
for _ in $(seq 1 60); do
	if [ "$(k get apiservice "$APISERVICE" -o jsonpath='{.status.conditions[?(@.type=="Available")].status}' 2>/dev/null)" = True ]; then ok=1; break; fi
	sleep 2
done
[ -n "$ok" ] || fail "$APISERVICE never became Available"
pass "$APISERVICE Available"

step "install octoconv with values-local.yaml and values-loadproof.yaml"
INSTALLED_OCTOCONV=1
read -r -a ASSIST <<< "$(grep -h -A2 'scaleTargetRef:' "$CHART_DIR"/templates/scaledobject-*.yaml | awk '$1 == "name:" { print $2 }' | grep -vx cad-worker | LC_ALL=C sort -u | tr '\n' ' ')"
[ "${#ASSIST[@]}" -ge 1 ] || fail "no non-cad ScaledObject target found in the chart"
kt 420 helm install octoconv "$CHART_DIR" -f "$VALUES_LOCAL" -f "$VALUES_LOADPROOF" -n "$NAMESPACE" --create-namespace > "$W/helm-install.log" 2>&1 &
HELM_PID=$!
SCALED=()
A0=$(nowf)
while [ "${#SCALED[@]}" -lt "${#ASSIST[@]}" ] && le "$(diff_s "$A0" "$(nowf)")" 240; do
	for d in "${ASSIST[@]}"; do
		case " ${SCALED[*]:-} " in *" $d "*) continue ;; esac
		if kn get deployment "$d" >/dev/null 2>&1 && kn scale deployment "$d" --replicas=0 >/dev/null 2>&1; then
			SCALED+=("$d")
		fi
	done
	sleep 1
done
HELM_RC=0
wait "$HELM_PID" || HELM_RC=$?
cat "$W/helm-install.log"
[ "$HELM_RC" = 0 ] || { diag_ns; fail "helm install octoconv failed rc=$HELM_RC"; }
[ "${#SCALED[@]}" = "${#ASSIST[@]}" ] || { diag_ns; fail "scaled ${#SCALED[@]} of ${#ASSIST[@]} non-cad ScaledObject targets to 0"; }
ev "INSTALL_ASSIST,scaled_to_zero=$(IFS=';'; echo "${SCALED[*]}"),count=${#SCALED[@]},assist_s=$(diff_s "$A0" "$(nowf)"),cad_worker=untouched,reason=every KEDA-owned Deployment starts at 1 replica at install and ten 2-CPU worker pods starve api or minio on this 11-CPU node (76-09 attempts 3 and 4)"
pass "helm install octoconv"
SO_WINDOW=$(kn get scaledobject "$SCALED_OBJECT" -o jsonpath='{.spec.advanced.horizontalPodAutoscalerConfig.behavior.scaleDown.stabilizationWindowSeconds}') || SO_WINDOW=""
DEP_GRACE=$(kn get deployment cad-worker -o jsonpath='{.spec.template.spec.terminationGracePeriodSeconds}') || DEP_GRACE=""
[ "$SO_WINDOW" = "$STAB" ] || fail "the live cad ScaledObject window is $SO_WINDOW, want $STAB"
[ "$DEP_GRACE" = "$EXPECTED_GRACE" ] || fail "the live cad-worker Deployment grace is $DEP_GRACE, want $EXPECTED_GRACE"
ev "LIVE_CHECK,scaledobject_window_s=$SO_WINDOW,deployment_grace_s=$DEP_GRACE,expected_grace_s=$EXPECTED_GRACE,result=ok"
pass "the live ScaledObject window is $SO_WINDOW s and the Deployment grace $DEP_GRACE s"
kt 300 kubectl -n "$NAMESPACE" wait --for=condition=Available deployment/redis --timeout=280s || { diag_ns; fail "deployment/redis not Available"; }
REDIS_POD=$(kn get pod -l app.kubernetes.io/component=redis -o jsonpath='{.items[0].metadata.name}') || REDIS_POD=""
[ -n "$REDIS_POD" ] || fail "no redis pod"
QUEUES=$(grep -ho 'queue="[a-z]*"' "$CHART_DIR"/templates/scaledobject-*.yaml | sed 's/queue="\(.*\)"/\1/' | LC_ALL=C sort -u | tr '\n' ' ')
echo "$QUEUES" | grep -qw cad || fail "no ScaledObject template names the cad queue"
read -r -a QARR <<< "$QUEUES"
kn exec "$REDIS_POD" -- redis-cli SADD asynq:queues "${QARR[@]}" webhook >/dev/null
SEEDED=$(kn exec "$REDIS_POD" -- redis-cli SMEMBERS asynq:queues | LC_ALL=C sort | tr '\n' ';')
ev "SEED,key=asynq:queues,members=$SEEDED"
for q in $QUEUES webhook; do
	echo ";$SEEDED" | grep -q ";$q;" || fail "asynq:queues lacks $q after seeding"
done
pass "asynq:queues seeded with every ScaledObject queue and webhook"
for d in postgres minio api prometheus webhook-worker mcp-http; do
	kt 320 kubectl -n "$NAMESPACE" wait --for=condition=Available "deployment/$d" --timeout=300s || { diag_ns; fail "deployment/$d not Available"; }
	pass "deployment/$d Available"
done

step "port-forward and client"
kt 14400 kubectl port-forward -n "$NAMESPACE" svc/api "${API_LOCAL_PORT}:8090" > "$W/api-pf.log" 2>&1 &
API_PF_PID=$!
kt 14400 kubectl port-forward -n "$NAMESPACE" svc/postgres "${DB_LOCAL_PORT}:5432" > "$W/db-pf.log" 2>&1 &
DB_PF_PID=$!
ok=""
for _ in $(seq 1 30); do
	if [ "$(curl -s -o "$W/healthz.json" -w '%{http_code}' --max-time 5 "$API_BASE/healthz" || true)" = 200 ]; then ok=1; break; fi
	sleep 2
done
[ -n "$ok" ] || fail "/healthz never returned 200 through the port-forward"
ev "CALIBRATION_STEP,check=healthz,http=200,via=port-forward,result=ok"
pass "/healthz 200 through the port-forward"
CLIENT_OUT=$(DATABASE_URL="postgres://octo:octo-pass@127.0.0.1:${DB_LOCAL_PORT}/octo_db" API_KEY_SALT="dev-only-change-me-in-real-deploys" \
	kt 300 go run ./cmd/manage-clients create "keda-cad-downscale-$(date +%s)")
KEY=$(printf '%s\n' "$CLIENT_OUT" | awk -F': ' '/^api key/ { print $2 }')
[ -n "$KEY" ] || fail "could not mint a client"
pass "client minted (key redacted, ${#KEY} chars)"

psqlq() { kn exec deploy/postgres -- psql -U octo -d octo_db -tAc "$1" | tr -d '\r'; }

submit() {
	local path=$1 name=$2 target=$3 op=${4:-} opts=${5:-} code args
	args=(-s -o "$W/post.json" -w '%{http_code}' --max-time 120 -X POST "$API_BASE/v1/jobs" -H "Authorization: ApiKey $KEY" -F "target=$target" -F "file=@$path;filename=$name")
	[ -z "$op" ] || args+=(-F "operation=$op")
	[ -z "$opts" ] || args+=(--form-string "opts=$opts")
	code=$(curl "${args[@]}" || true)
	[ "$code" = 202 ] || fail "POST /v1/jobs $name -> $target returned $code: $(cat "$W/post.json" 2>/dev/null)"
	jq -r '.job_id // empty' "$W/post.json"
}

job_status() {
	curl -s --max-time 30 -H "Authorization: ApiKey $KEY" "$API_BASE/v1/jobs/$1" | jq -r '.status // empty' 2>/dev/null || true
}

wait_job() {
	local id=$1 limit=$2 t0 s
	t0=$(nowf)
	while :; do
		s=$(job_status "$id")
		case "$s" in done | failed | canceled) echo "$s"; return 0 ;; esac
		le "$(diff_s "$t0" "$(nowf)")" "$limit" || { echo "timeout"; return 0; }
		sleep 3
	done
}

zero_check() {
	local phase=$1 waited=0 r fb name val
	while :; do
		r=$(cad_replicas) || r=err
		fb=$(kn get scaledobject "$SCALED_OBJECT" -o json | jq -r '[(.status.conditions // [])[] | select(.type == "Fallback") | .status][0] // "absent"') || fb=err
		name=$(kn get scaledobject "$SCALED_OBJECT" -o json | jq -r '.status.externalMetricNames[0] // empty') || name=""
		val=""
		if [ -n "$name" ]; then
			val=$(k get --raw "/apis/external.metrics.k8s.io/v1beta1/namespaces/$NAMESPACE/$name?labelSelector=scaledobject.keda.sh%2Fname%3D$SCALED_OBJECT" 2>/dev/null | jq -r '.items[0].value // empty' 2>/dev/null || true)
		fi
		if [ "$r" = 0 ] && [ "$fb" != True ] && [ "$val" = 0 ]; then break; fi
		if [ "$waited" -ge 240 ]; then
			{
				echo "## zero_check $phase failed: replicas=$r fallback=$fb metric=$name value=$val"
				kn get hpa -o wide
				kn get scaledobject "$SCALED_OBJECT" -o yaml
				k logs -n "$KEDA_NAMESPACE" -l app=keda-operator --tail=100 | grep -iE 'cad|error|fallback'
			} 2>&1 | sed 's/^/DIAG: /'
			fail "cad-worker did not settle at a real zero within 240 s (replicas=$r fallback=$fb metric_value=$val)"
		fi
		sleep 5
		waited=$((waited + 5))
	done
	ev "ZERO,phase=$phase,replicas=0,fallback=$fb,metric=$name,metric_value=$val,settle_s=$waited,result=ok"
	pass "cad-worker at a real zero ($phase)"
}

event_ts() {
	kn get events --field-selector "involvedObject.name=$1,reason=$2" -o json 2>/dev/null |
		jq -r '[.items[] | (.firstTimestamp // .eventTime // .metadata.creationTimestamp)] | map(select(. != null)) | sort | .[0] // empty' | sed 's/\.[0-9]*Z$/Z/'
}

settle_all() {
	local waited=0 comps busy
	while :; do
		comps=$(kn get scaledobject -o json | jq -r '.items[].spec.scaleTargetRef.name' | while read -r d; do
			kn get deployment "$d" -o json | jq -r '.spec.selector.matchLabels."app.kubernetes.io/component"'
		done | LC_ALL=C sort -u | tr '\n' ' ') || comps=""
		[ -n "$comps" ] || fail "cannot read the ScaledObject targets"
		busy=$(kn get pods -o json | jq -r --arg c " $comps" '[.items[] | ((.metadata.labels."app.kubernetes.io/component" // "-") as $l | select($c | contains(" " + $l + " ")))] | map(.metadata.name) | join(";")') || busy=err
		if [ -z "$busy" ]; then break; fi
		if [ "$waited" -ge 300 ]; then
			diag_ns
			fail "ScaledObject targets still hold pods after 300 s: $busy"
		fi
		sleep 5
		waited=$((waited + 5))
	done
	ev "SETTLE,scaled_targets=$(printf '%s' "$comps" | sed 's/ $//' | tr ' ' ';'),pods=0,settle_s=$waited,result=ok"
	pass "every ScaledObject target holds no pod"
}

step "calibration"
settle_all
zero_check calibration
NODE=$(k get nodes -o json)
PODS_JSON=$(k get pods -A -o json)
LIMITS=$(kn get deployment cad-worker -o json | jq -c '.spec.template.spec.containers[0].resources')
FIT=$(jq -rn --argjson node "$NODE" --argjson pods "$PODS_JSON" --argjson res "$LIMITS" '
	def cpu: if . == null then 0 elif (tostring | endswith("m")) then (tostring | .[:-1] | tonumber) / 1000 else (tostring | tonumber) end;
	def mem: if . == null then 0 else (tostring | capture("^(?<n>[0-9.e+]+)(?<u>[A-Za-z]*)$") as $c | ($c.n | tonumber) * ({"": 1, "Ki": 1024, "Mi": 1048576, "Gi": 1073741824, "Ti": 1099511627776, "k": 1000, "K": 1000, "M": 1000000, "G": 1000000000, "T": 1000000000000}[$c.u])) end;
	($node.items[0].status.allocatable) as $a
	| [$pods.items[] | select(.spec.nodeName != null and .status.phase != "Succeeded" and .status.phase != "Failed") | .spec.containers[] | .resources.requests // {}] as $r
	| (($res.requests.cpu // $res.limits.cpu) | cpu) as $pc
	| (($res.requests.memory // $res.limits.memory) | mem) as $pm
	| (($a.cpu | cpu) - ($r | map(.cpu | cpu) | add // 0)) as $fc
	| (($a.memory | mem) - ($r | map(.memory | mem) | add // 0)) as $fm
	| "alloc_cpu=\($a.cpu | cpu),requested_cpu=\(($r | map(.cpu | cpu) | add // 0) * 1000 | round / 1000),free_cpu=\($fc * 1000 | round / 1000),need_cpu=\(2 * $pc),alloc_mem_mib=\(($a.memory | mem) / 1048576 | floor),requested_mem_mib=\(($r | map(.memory | mem) | add // 0) / 1048576 | floor),free_mem_mib=\($fm / 1048576 | floor),need_mem_mib=\(2 * $pm / 1048576 | floor),fits=\(if ($fc >= 2 * $pc and $fm >= 2 * $pm) then 1 else 0 end)"')
ev "CALIBRATION_STEP,check=node_fit,$FIT"
case "$FIT" in
*fits=1) pass "the node fits two cad pods" ;;
*)
	jq -rn --argjson pods "$PODS_JSON" '$pods.items[] | select(.spec.nodeName != null and .status.phase != "Succeeded" and .status.phase != "Failed") | "DIAG: \(.metadata.namespace)/\(.metadata.name) \(.status.phase) cpu=\([.spec.containers[].resources.requests.cpu // "0"] | join("+")) mem=\([.spec.containers[].resources.requests.memory // "0"] | join("+"))"'
	fail "the node does not fit two cad pods: $FIT"
	;;
esac

CAL_L0=$(wait_quiet) || fail "load1 stayed above $QUIET_LOAD for $QUIET_WAIT_S s before the calibration"
CAL_T0=$(nowf)
CAL_JOB=$(submit "$CAL_INPUT" units_inch.step png render)
[ -n "$CAL_JOB" ] || fail "no job id for the calibration render"
ok=""
for _ in $(seq 1 60); do
	[ "$(cad_replicas)" -ge 1 ] && { ok=1; break; }
	sleep 3
done
[ -n "$ok" ] || fail "cad-worker never scaled 0->1 within 180 s of the calibration job"
CAL_UP_S=$(diff_s "$CAL_T0" "$(nowf)")
pass "cad-worker scaled 0->1 for $CAL_JOB in about $CAL_UP_S s"
CAL_POD=""
for _ in $(seq 1 20); do
	CAL_POD=$(kn get pod -l "$CAD_LABEL" -o json | jq -r '[.items[] | select(.metadata.deletionTimestamp == null)] | sort_by(.metadata.creationTimestamp) | .[-1].metadata.name // empty') || CAL_POD=""
	[ -n "$CAL_POD" ] && break
	sleep 1
done
[ -n "$CAL_POD" ] || fail "no cad-worker pod found"
CAL_C0=$(kn get pod "$CAL_POD" -o jsonpath='{.metadata.creationTimestamp}') || CAL_C0=""
[ -n "$CAL_C0" ] || fail "cad pod $CAL_POD has no creationTimestamp"
READY_TS=""
for _ in $(seq 1 40); do
	READY_TS=$(kn get pod "$CAL_POD" -o json | jq -r '(.status.conditions // [])[] | select(.type == "Ready" and .status == "True") | .lastTransitionTime') || READY_TS=""
	[ -n "$READY_TS" ] && break
	sleep 3
done
[ -n "$READY_TS" ] || fail "cad pod $CAL_POD was not Ready within 120 s"
CREATED_TO_READY=$(diff_s "$(epoch_of "$CAL_C0")" "$(epoch_of "$READY_TS")")
le "$CREATED_TO_READY" 120 || fail "cad pod $CAL_POD took $CREATED_TO_READY s from creation to Ready, above 120 s"
SUBMIT_TO_READY=$(diff_s "$CAL_T0" "$(epoch_of "$READY_TS")")
PROVEN=0
for _ in $(seq 1 30); do
	if kn logs "$CAL_POD" 2>/dev/null | grep -q 'cad sandbox proven'; then PROVEN=1; break; fi
	sleep 2
done
[ "$PROVEN" = 1 ] || fail "cad pod $CAL_POD did not log 'cad sandbox proven'"
POD_IMAGE_ID=$(kn get pod "$CAL_POD" -o jsonpath='{.status.containerStatuses[0].imageID}') || POD_IMAGE_ID=""
case "$POD_IMAGE_ID" in *"${CAD_IMAGE_ID#sha256:}") ;; *) fail "cad pod imageID $POD_IMAGE_ID does not end with the local id $CAD_IMAGE_ID" ;; esac
pass "cad pod $CAL_POD runs the local octoconv-cad-worker:dev ($CAD_IMAGE_ID)"
CAL_GRACE=$(kn get pod "$CAL_POD" -o jsonpath='{.spec.terminationGracePeriodSeconds}') || CAL_GRACE=""
[ "$CAL_GRACE" = "$EXPECTED_GRACE" ] || fail "cad pod $CAL_POD has terminationGracePeriodSeconds $CAL_GRACE, want $EXPECTED_GRACE"
pass "cad pod $CAL_POD grace $CAL_GRACE s"
CAL_STATUS=$(wait_job "$CAL_JOB" $((ENGINE_S + 60)))
[ "$CAL_STATUS" = "done" ] || fail "calibration job $CAL_JOB ended $CAL_STATUS"
CAL_DONE=$(nowf)
ok=""
while le "$(diff_s "$CAL_DONE" "$(nowf)")" "$DRAIN_BOUND"; do
	[ "$(cad_replicas)" = 0 ] && { ok=1; break; }
	sleep 3
done
[ -n "$ok" ] || fail "cad-worker did not drain to 0 within $DRAIN_BOUND s of the calibration job"
CAL_DRAINED=$(diff_s "$CAL_DONE" "$(nowf)")
quiet_line calibration "$CAL_L0" "$(load1)" na || echo "calibration timings are contaminated; the calibration checks are not timings"
ev "CALIBRATION,job=$CAL_JOB,pod=$CAL_POD,submit_utc=$(iso_at "$CAL_T0"),ready_utc=$READY_TS,submit_to_ready_s=$SUBMIT_TO_READY,created_to_ready_s=$CREATED_TO_READY,pod_image_id=$POD_IMAGE_ID,grace_s=$CAL_GRACE,expected_grace_s=$EXPECTED_GRACE,sandbox_proven=1,status=done,drained_s=$CAL_DRAINED,drain_bound_s=$DRAIN_BOUND,result=ok"
pass "calibration"

if [ "$CALIBRATE_ONLY" = 1 ]; then
	GATE_OK=1
	echo "=== CALIBRATION: ALL $PASS_COUNT CHECKS PASSED; teardown and restore follow ==="
	exit 0
fi

step "survival"
zero_check survival
: > "$W/samples"
: > "$W/transitions"
: > "$W/busy-watch"
rm -f "$W/sampler.stop" "$W/watch.stop" "$W/t0"

sampler() {
	local prev="0 0" cur c t t0 r ready spec
	while [ ! -f "$W/sampler.stop" ]; do
		c=$(deploy_counts) || c=""
		t=$(nowf)
		if [ -n "$c" ]; then
			spec=${c%% *}
			r=$(echo "$c" | awk '{ print $2 }')
			ready=$(echo "$c" | awk '{ print $3 }')
			echo "$t $spec $r $ready $(load1)" >> "$W/samples"
			cur="$r $ready"
			if [ "$cur" != "$prev" ]; then
				t0=$(cat "$W/t0" 2>/dev/null || echo "$t")
				echo "TRANSITION,from=${prev%% *},to=$r,ready=$ready,spec=$spec,at_utc=$(iso_at "$t"),since_submit_s=$(diff_s "$t0" "$t")" >> "$W/transitions"
				prev=$cur
			fi
		else
			echo "$t err" >> "$W/samples"
		fi
		sleep 2
	done
}

CENSUS_SH='for d in /proc/[0-9]*; do c=""; { read -r c < "$d/comm"; } 2>/dev/null || continue; n=""; case "$c" in rsvg-convert|cad-guard|cad-mesh|cad-render|cad-dxf) n=$c ;; python*) for a in $(tr "\0" "\n" < "$d/cmdline" 2>/dev/null); do case "${a##*/}" in cad-guard|cad-mesh|cad-render|cad-dxf) n=${a##*/}; break ;; esac; done ;; esac; [ -z "$n" ] || echo "$n:${d#/proc/}"; done; true'

census() {
	local out
	out=$(kn exec "$1" -- sh -c "$CENSUS_SH" 2>/dev/null) || return 1
	printf '%s\n' "$out" | tr -d '\r' | sed '/^$/d' | LC_ALL=C sort | tr '\n' '+' | sed 's/+$//'
}

watcher() {
	while [ ! -f "$W/watch.stop" ]; do
		kt $((EXPECTED_GRACE + 900)) kubectl -n "$NAMESPACE" get pod "$BUSY_POD" -w --output-watch-events \
			-o jsonpath='{.type}{" phase="}{.object.status.phase}{" term_reason="}{.object.status.containerStatuses[0].state.terminated.reason}{" term_exit="}{.object.status.containerStatuses[0].state.terminated.exitCode}{" term_finished="}{.object.status.containerStatuses[0].state.terminated.finishedAt}{"\n"}' 2>/dev/null |
			while IFS= read -r l; do echo "read_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ) $l" >> "$W/busy-watch"; done || true
		kn get pod "$BUSY_POD" >/dev/null 2>&1 || break
		sleep 1
	done
}

set -m
sampler &
SAMPLER_PID=$!
set +m
sleep 3
S_L0=$(wait_quiet) || fail "load1 stayed above $QUIET_LOAD for $QUIET_WAIT_S s before the LONG submit"
T0=$(nowf)
echo "$T0" > "$W/t0"
LONG_JOB=$(submit "$L_FILE" "$L_NAME" "$L_TARGET" render "$L_OPTS_JSON")
[ -n "$LONG_JOB" ] || fail "no job id for LONG"
ev "LONG_SUBMIT,job=$LONG_JOB,submit_utc=$(iso_at "$T0"),file=$L_NAME,target=$L_TARGET,opts=$L_OPTS_JSON,load1=$S_L0"
LS=""
while le "$(diff_s "$T0" "$(nowf)")" "$SCALEUP_WINDOW_S"; do
	LS=$(job_status "$LONG_JOB")
	case "$LS" in active | done | failed | canceled) break ;; esac
	sleep 2
done
[ "$LS" = active ] || { diag_ns; fail "LONG $LONG_JOB is $LS, not active, $SCALEUP_WINDOW_S s after the submit"; }
LA=$(nowf)
FIRST_PODS=$(live_cad_pods) || FIRST_PODS=""
N_FIRST=$(printf '%s\n' "$FIRST_PODS" | sed '/^$/d' | wc -l | tr -d ' ')
[ "$N_FIRST" = 1 ] || fail "LONG went active while $N_FIRST cad pods ran ($FIRST_PODS), want exactly 1"
FIRST_POD=$FIRST_PODS
ev "LONG_ACTIVE,job=$LONG_JOB,pod=$FIRST_POD,pods=1,active_seen_utc=$(iso_at "$LA"),since_submit_s=$(diff_s "$T0" "$LA"),load1=$(load1),result=ok"
pass "LONG active on the only cad pod $FIRST_POD"
TS0=$(nowf)
SHORT_JOB=$(submit "$CAL_INPUT" units_inch.step png render)
[ -n "$SHORT_JOB" ] || fail "no job id for SHORT"
ev "SHORT_SUBMIT,job=$SHORT_JOB,submit_utc=$(iso_at "$TS0"),file=units_inch.step,target=png"
ok=""
last=""
while le "$(diff_s "$TS0" "$(nowf)")" "$SCALEUP_WINDOW_S"; do
	last=$(grep -v ' err$' "$W/samples" | tail -n 1)
	if [ "$(echo "$last" | awk '{ print $3 }')" = 2 ] && [ "$(echo "$last" | awk '{ print $4 }')" = 2 ]; then ok=1; break; fi
	sleep 2
done
[ -n "$ok" ] || { diag_ns; fail "cad-worker did not reach replicas 2 and readyReplicas 2 within $SCALEUP_WINDOW_S s of SHORT (last sample: $last)"; }
pass "cad-worker 1->2 with 2 Ready"

C0=$(nowf)
TRIES=0
HOLD_PROCS=""
CENSUS_LINE=""
EARLIEST=""
while le "$(diff_s "$C0" "$(nowf)")" "$CENSUS_WINDOW_S"; do
	TRIES=$((TRIES + 1))
	PODS=$(live_cad_pods) || PODS=""
	np=0
	holders=0
	hold=""
	procs=""
	errs=0
	CENSUS_LINE=""
	for p in $PODS; do
		np=$((np + 1))
		e=$(census "$p") || { e=ERR; errs=$((errs + 1)); }
		CENSUS_LINE="$CENSUS_LINE;$p=${e:-none}"
		if [ "$e" != ERR ] && [ -n "$e" ]; then holders=$((holders + 1)); hold=$p; procs=$e; fi
	done
	if [ "$np" = 2 ] && [ "$errs" = 0 ] && [ "$holders" = 1 ]; then
		BUSY_POD=$hold
		HOLD_PROCS=$procs
		EARLIEST=$(printf '%s\n' "$PODS" | head -n 1)
		break
	fi
	echo "census try $TRIES: pods=$np holders=$holders errors=$errs$CENSUS_LINE"
	sleep 2
done
CENSUS_LINE=${CENSUS_LINE#;}
[ -n "$BUSY_POD" ] || { ev "BUSY,pod=none,census=$CENSUS_LINE,tries=$TRIES,result=FAIL"; fail "no census within $CENSUS_WINDOW_S s found exactly one of two Running cad pods holding an engine process"; }
IS_EARLIEST=0
[ "$BUSY_POD" = "$EARLIEST" ] && [ "$BUSY_POD" = "$FIRST_POD" ] && IS_EARLIEST=1
if [ "$IS_EARLIEST" != 1 ]; then
	ev "BUSY,pod=$BUSY_POD,engine_procs=$HOLD_PROCS,earliest=0,first_pod=$FIRST_POD,earliest_pod=$EARLIEST,result=FAIL"
	fail "the engine-holding pod $BUSY_POD is not both the earliest pod $EARLIEST and the pod LONG went active on $FIRST_POD"
fi
kn annotate pod "$BUSY_POD" controller.kubernetes.io/pod-deletion-cost=-1000 --overwrite >/dev/null || fail "annotating $BUSY_POD failed"
ANN_AT=$(nowf)
SPEC_AT_ANN=$(deploy_counts | awk '{ print $1 }')
COST=$(kn get pod "$BUSY_POD" -o json | jq -r '.metadata.annotations["controller.kubernetes.io/pod-deletion-cost"] // empty') || COST=""
[ "$COST" = "-1000" ] || fail "the busy pod's deletion cost reads $COST, want -1000"
[ "$SPEC_AT_ANN" = 2 ] || fail "spec.replicas was $SPEC_AT_ANN when the annotation landed; the scale-down decision may already have been taken"
BUSY_GRACE=$(kn get pod "$BUSY_POD" -o jsonpath='{.spec.terminationGracePeriodSeconds}') || BUSY_GRACE=""
[ "$BUSY_GRACE" = "$EXPECTED_GRACE" ] || fail "the busy pod's terminationGracePeriodSeconds is $BUSY_GRACE, want $EXPECTED_GRACE"
ev "BUSY_CENSUS,tries=$TRIES,census_s=$(diff_s "$C0" "$ANN_AT"),pods=$CENSUS_LINE,first_pod=$FIRST_POD,earliest_pod=$EARLIEST,annotated_utc=$(iso_at "$ANN_AT"),spec_replicas_at_annotation=$SPEC_AT_ANN"
ev "BUSY,pod=$BUSY_POD,engine_procs=$HOLD_PROCS,earliest=1,deletion_cost=$COST,grace_s=$BUSY_GRACE,expected_grace_s=$EXPECTED_GRACE,result=ok"
pass "busy pod $BUSY_POD holds $HOLD_PROCS and carries deletion cost -1000"
set -m
watcher &
WATCH_PID=$!
set +m

SS=$(wait_job "$SHORT_JOB" 600)
[ "$SS" = done ] || fail "SHORT $SHORT_JOB ended $SS"
SHORT_FIN=$(psqlq "select extract(epoch from finished_at) from jobs where id = '$SHORT_JOB'")
[ -n "$SHORT_FIN" ] || fail "no finished_at for SHORT"
ev "SHORT,job=$SHORT_JOB,status=done,finished_at=$(iso_at "$SHORT_FIN")"
pass "SHORT done"
ok=""
while le "$(diff_s "$SHORT_FIN" "$(nowf)")" "$DOWNSCALE_WINDOW_S"; do
	if awk -v d="$SHORT_FIN" '$2 != "err" && $1 > d && $3 <= 1 { f = 1 } END { exit !f }' "$W/samples"; then ok=1; break; fi
	sleep 2
done
SPEC_ONE=$(awk -v d="$SHORT_FIN" '$2 != "err" && $1 > d && $2 <= 1 { print $1; exit }' "$W/samples")
STATUS_ONE=$(awk -v d="$SHORT_FIN" '$2 != "err" && $1 > d && $3 <= 1 { print $1; exit }' "$W/samples")
[ -n "$ok" ] || { diag_ns; fail "status.replicas did not reach 1 within $DOWNSCALE_WINDOW_S s of SHORT done"; }
ev "DOWNSCALE,short_done_utc=$(iso_at "$SHORT_FIN"),spec_one_utc=$([ -n "$SPEC_ONE" ] && iso_at "$SPEC_ONE" || echo absent),status_one_utc=$(iso_at "$STATUS_ONE"),since_short_done_s=$(diff_s "$SHORT_FIN" "$STATUS_ONE"),window_s=$DOWNSCALE_WINDOW_S,result=ok"
pass "cad-worker 2->1 $(diff_s "$SHORT_FIN" "$STATUS_ONE") s after SHORT done"

LSF=$(wait_job "$LONG_JOB" $((ENGINE_S + 120)))
L_END_LOAD=$(load1)
FACTS=$(psqlq "select status || ' ' || attempts || ' ' || extract(epoch from started_at) || ' ' || extract(epoch from finished_at) from jobs where id = '$LONG_JOB'")
read -r L_STATUS L_ATT L_START L_FIN <<< "$FACTS"
L_QA=$(psqlq "select count(*) from job_events where job_id = '$LONG_JOB' and from_status = 'queued' and to_status = 'active'")
L_WALL=$(awk -v a="${L_START:-0}" -v b="${L_FIN:-0}" 'BEGIN { printf "%.2f", b - a }')
IN_BAND=0
if [ -n "${L_FIN:-}" ] && le "$BAND_LOW" "$L_WALL" && le "$L_WALL" "$BAND_HIGH"; then IN_BAND=1; fi
ev "LONG,job=$LONG_JOB,attempts=${L_ATT:-na},queued_to_active=${L_QA:-na},status=${L_STATUS:-$LSF},started_at=$([ -n "${L_START:-}" ] && iso_at "$L_START" || echo absent),finished_at=$([ -n "${L_FIN:-}" ] && iso_at "$L_FIN" || echo absent),attempt_wall_s=$L_WALL,in_band=$IN_BAND"
[ "$LSF" = done ] && [ "$L_STATUS" = done ] || fail "LONG $LONG_JOB ended $LSF ($L_STATUS)"
[ "$L_ATT" = 1 ] || fail "LONG has attempts=$L_ATT, want 1"
[ "$L_QA" = 1 ] || fail "LONG has $L_QA queued->active transitions, want 1"
pass "LONG done once"

SIGTERM_TS=$(event_ts "$BUSY_POD" Killing)
[ -n "$SIGTERM_TS" ] || fail "no Killing event for the busy pod $BUSY_POD"
SIGTERM_E=$(epoch_of "$SIGTERM_TS")
ev "SIGTERM,pod=$BUSY_POD,killing_at=$SIGTERM_TS"
SIGTERM_UB=$(awk -v s="$SIGTERM_E" 'BEGIN { printf "%.3f", s + 1 }')
ORDER_OK=0
lt "$SIGTERM_UB" "$L_FIN" && lt "$ANN_AT" "$SIGTERM_E" && ORDER_OK=1
ev "ORDER_DETAIL,annotated_utc=$(iso_at "$ANN_AT"),killing_at=$SIGTERM_TS,long_finished_at=$(iso_at "$L_FIN"),sigterm_second_upper_bound=$(iso_at "$SIGTERM_UB"),sigterm_to_finish_s=$(diff_s "$SIGTERM_UB" "$L_FIN"),annotation_before_sigterm=$(lt "$ANN_AT" "$SIGTERM_E" && echo 1 || echo 0)"
[ "$ORDER_OK" = 1 ] || { ev "ORDER,sigterm_before_finish=$(lt "$SIGTERM_UB" "$L_FIN" && echo 1 || echo 0),result=FAIL"; fail "the Killing event $SIGTERM_TS does not fall between the annotation and LONG's finished_at"; }
ev "ORDER,sigterm_before_finish=1,result=ok"
pass "SIGTERM reached the busy pod at least $(diff_s "$SIGTERM_UB" "$L_FIN") s before LONG finished"

waited=0
while [ "$waited" -lt "$EXPECTED_GRACE" ]; do
	if grep 'term_finished=' "$W/busy-watch" 2>/dev/null | grep -qv 'term_finished=$'; then break; fi
	if ! kn get pod "$BUSY_POD" >/dev/null 2>&1; then sleep 3; break; fi
	sleep 3
	waited=$((waited + 3))
done
T_REASON=$(kn get pod "$BUSY_POD" -o jsonpath='{.status.containerStatuses[0].state.terminated.reason}' 2>/dev/null || true)
T_EXIT=$(kn get pod "$BUSY_POD" -o jsonpath='{.status.containerStatuses[0].state.terminated.exitCode}' 2>/dev/null || true)
T_FIN=$(kn get pod "$BUSY_POD" -o jsonpath='{.status.containerStatuses[0].state.terminated.finishedAt}' 2>/dev/null || true)
T_SRC=live
if [ -z "$T_FIN" ]; then
	T_LINE=$(grep 'term_finished=' "$W/busy-watch" | grep -v 'term_finished=$' | tail -n 1 || true)
	T_REASON=$(printf '%s' "$T_LINE" | sed -n 's/.*term_reason=\([^ ]*\).*/\1/p')
	T_EXIT=$(printf '%s' "$T_LINE" | sed -n 's/.*term_exit=\([^ ]*\).*/\1/p')
	T_FIN=$(printf '%s' "$T_LINE" | sed -n 's/.*term_finished=\([^ ]*\).*/\1/p')
	T_SRC=watch
fi
stop_watch
sed 's/^/BUSY_WATCH,/' "$W/busy-watch" | tr ' ' ',' | tee -a "$OUT" >/dev/null
[ -n "$T_FIN" ] || fail "no terminated state captured for the busy pod $BUSY_POD"
T_FIN_E=$(epoch_of "$T_FIN")
T_SINCE=$(diff_s "$SIGTERM_E" "$T_FIN_E")
ev "POD_TERMINATION,pod=$BUSY_POD,reason=$T_REASON,exit=$T_EXIT,finished_at=$T_FIN,since_sigterm_s=$T_SINCE,grace_s=$BUSY_GRACE,source=$T_SRC"
[ "$T_EXIT" != 137 ] || fail "the busy pod exited 137 (SIGKILL)"
[ "$T_REASON" = Completed ] && [ "$T_EXIT" = 0 ] || fail "the busy pod terminated reason=$T_REASON exit=$T_EXIT, want Completed and 0"
le "$T_SINCE" "$BUSY_GRACE" || fail "the busy pod exited $T_SINCE s after SIGTERM, above the grace $BUSY_GRACE s"
le "$L_FIN" "$((${T_FIN_E%.*} + 1))" || fail "the busy pod exited before LONG finished"
pass "the busy pod exited Completed 0, $T_SINCE s after SIGTERM"

ok=""
while le "$(diff_s "$L_FIN" "$(nowf)")" $((DRAIN_BOUND + 30)); do
	if awk -v d="$L_FIN" '$2 != "err" && $1 > d && $3 == 0 { f = 1 } END { exit !f }' "$W/samples"; then ok=1; break; fi
	sleep 3
done
sleep 6
stop_sampler
[ -n "$ok" ] || fail "cad-worker did not drain to 0 within $((DRAIN_BOUND + 30)) s of LONG's finish"
ZERO_AT=$(awk -v d="$L_FIN" '$2 != "err" && $1 > d && $3 == 0 { print $1; exit }' "$W/samples")
DRAIN_S=$(diff_s "$L_FIN" "$ZERO_AT")
tee -a "$OUT" < "$W/transitions"
TRANS_DUMPED=1
NTRANS=$(wc -l < "$W/transitions" | tr -d ' ')
[ "$NTRANS" -ge 3 ] || fail "only $NTRANS TRANSITION lines"
head -n 1 "$W/transitions" | grep -q '^TRANSITION,from=0,' || fail "the first TRANSITION does not start from 0"
grep -q '^TRANSITION,from=2,to=1,' "$W/transitions" || fail "no TRANSITION from 2 to 1"
tail -n 1 "$W/transitions" | grep -q ',to=0,ready=0,' || fail "the last TRANSITION does not end at 0"
awk -F'at_utc=' '{ split($2, a, ","); if (NR > 1 && a[1] < p) bad = 1; p = a[1] } END { exit bad }' "$W/transitions" || fail "TRANSITION timestamps decrease"
MAXOBS=$(awk '$2 != "err" { if ($2 > m) m = $2; if ($3 > m) m = $3 } END { print m + 0 }' "$W/samples")
[ "$MAXOBS" -le "$MAXR" ] || fail "the sampler saw $MAXOBS replicas, above $MAXR"
ev "SAMPLER,interval_s=2,samples=$(grep -vc ' err$' "$W/samples" || true),sample_errors=$(grep -c ' err$' "$W/samples" || true),max_replicas=$MAXOBS"
le "$DRAIN_S" "$DRAIN_BOUND" || fail "drain took $DRAIN_S s, above the bound $DRAIN_BOUND s"
ev "DRAIN,long_done_utc=$(iso_at "$L_FIN"),zero_utc=$(iso_at "$ZERO_AT"),drain_s=$DRAIN_S,bound_s=$DRAIN_BOUND,result=ok"
pass "drained to 0 in $DRAIN_S s"
LMAX=$(awk '$2 != "err" { if ($5 > m) m = $5 } END { print m + 0 }' "$W/samples")
quiet_line long "$S_L0" "$L_END_LOAD" "$LMAX" || fail "LONG's row is contaminated (load1 start $S_L0, end $L_END_LOAD); its wall does not count"
[ "$IN_BAND" = 1 ] || fail "LONG's attempt wall $L_WALL s is outside the band [$BAND_LOW;$BAND_HIGH]"
pass "LONG's wall $L_WALL s lies in [$BAND_LOW;$BAND_HIGH]"

GATE_OK=1
echo "=== ALL $PASS_COUNT CHECKS PASSED; teardown and restore follow ==="
