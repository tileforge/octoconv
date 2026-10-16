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
EVIDENCE_DIR="$ROOT/.planning/phases/76-measured-bounds-keda-proof-client-docs/evidence"
QUIET_LOAD=11
QUIET_WAIT_S=3600

usage() { echo "usage: $0 [--evidence-dir DIR] [--quiet-load 11]" >&2; exit 64; }
while [ $# -gt 0 ]; do
	case "$1" in
	--evidence-dir) [ $# -ge 2 ] || usage; EVIDENCE_DIR=$2; shift 2 ;;
	--quiet-load) [ $# -ge 2 ] || usage; QUIET_LOAD=$2; shift 2 ;;
	*) usage ;;
	esac
done
case "$QUIET_LOAD" in '' | *[!0-9]*) usage ;; esac

for tool in kubectl helm docker jq curl perl awk lsof git go orbctl; do
	command -v "$tool" >/dev/null || { echo "error: $tool is required" >&2; exit 69; }
done

RUN_TS=$(date -u +%Y%m%dT%H%M%SZ)
mkdir -p "$EVIDENCE_DIR"
OUT="$EVIDENCE_DIR/76-09-keda-loadproof.txt"
LOG_FILE="$EVIDENCE_DIR/76-09-keda-loadproof-transcript-${RUN_TS}.log"
W=$(mktemp -d "${TMPDIR:-/tmp}/keda-cad-loadproof.XXXXXX")
: > "$OUT"
exec > >(tee "$LOG_FILE") 2>&1

PASS_COUNT=0
GATE_OK=""
STEP="init"
API_PF_PID=""
DB_PF_PID=""
SAMPLER_PID=""
FIXTURE_CONTAINER=""
CAD_IMAGE_ID=""
INSTALLED_KEDA=0
INSTALLED_OCTOCONV=0
SNAP_TAKEN=0

kt() {
	local n=$1
	shift
	perl -e '
		my $n = shift @ARGV;
		my $pid = fork();
		die "fork: $!" unless defined $pid;
		if ($pid == 0) { exec @ARGV or exit 127; }
		local $SIG{ALRM} = sub { kill "KILL", $pid; waitpid($pid, 0); print STDERR "timeout after ${n}s: @ARGV\n"; exit 124; };
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
}

cad_value() {
	awk -v key="$1" '
		/^[[:space:]]*#/ || /^[[:space:]]*$/ { next }
		{
			match($0, /^ */); ind = RLENGTH; t = $0; sub(/^ */, "", t); sub(/[[:space:]]+$/, "", t)
			if (ind == 0) { keda = (t == "keda:"); cad = 0; next }
			if (keda && ind == 2) { cad = (t == "cad:"); next }
			if (cad && ind == 4) { kk = t; sub(/:.*/, "", kk); v = t; sub(/^[^:]*:[[:space:]]*/, "", v); gsub(/"/, "", v); if (kk == key) print v }
		}
	' "$VALUES"
}

configmap_value() { sed -n "s/^[[:space:]]*$1:[[:space:]]*\"\{0,1\}\([^\"]*\)\"\{0,1\}[[:space:]]*\$/\1/p" "$CONFIGMAP" | head -n 1; }

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

was_absent() { ! grep -qx "$1" "$W/snap-before"; }

deploy_counts() {
	kn get deployment cad-worker -o json 2>/dev/null | jq -r '"\(.spec.replicas // 0) \(.status.replicas // 0) \(.status.readyReplicas // 0)"'
}

cad_replicas() { deploy_counts | awk '{ print $2 }'; }

stop_sampler() {
	if [ -n "$SAMPLER_PID" ]; then
		touch "$W/sampler.stop"
		kill -- -"$SAMPLER_PID" >/dev/null 2>&1 || true
		wait "$SAMPLER_PID" 2>/dev/null || true
		SAMPLER_PID=""
	fi
}

teardown() {
	local exit_code=$? restore=ok n
	set +e
	echo ""
	echo "=== TEARDOWN ==="
	stop_sampler
	[ -z "$FIXTURE_CONTAINER" ] || kt 60 docker rm -fv "$FIXTURE_CONTAINER" >/dev/null 2>&1
	[ -z "$API_PF_PID" ] || kill "$API_PF_PID" >/dev/null 2>&1
	[ -z "$DB_PF_PID" ] || kill "$DB_PF_PID" >/dev/null 2>&1
	if [ "$INSTALLED_OCTOCONV" = 1 ]; then
		kt 420 helm uninstall octoconv -n "$NAMESPACE" --wait --timeout 6m
	fi
	if [ "$INSTALLED_KEDA" = 1 ]; then
		kt 420 helm uninstall keda -n "$KEDA_NAMESPACE" --wait --timeout 6m
	fi
	if [ "$SNAP_TAKEN" = 1 ]; then
		for ns in "$NAMESPACE" "$KEDA_NAMESPACE"; do
			if was_absent "ns,item=$ns" && k get ns "$ns" >/dev/null 2>&1; then
				kt 420 kubectl delete ns "$ns" --wait=true --timeout=400s
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
		ev "RESTORE,snapshot_lines=$(wc -l < "$W/snap-before" | tr -d ' '),diff_lines=$(diff "$W/snap-before" "$W/snap-after" 2>/dev/null | grep -c '^[<>]'),octoconv_pods=${n:-unknown},k8s_enabled=$(kt 30 orbctl config get k8s.enable 2>/dev/null),result=$restore"
	else
		restore=not_taken
	fi
	if [ "$exit_code" = 0 ] && [ "$GATE_OK" = 1 ] && [ "$restore" = ok ]; then
		ev "SUMMARY,mode=keda_cad_loadproof,checks=$PASS_COUNT,end_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ),end_load1=$(load1),result=PASS"
	else
		ev "SUMMARY,mode=keda_cad_loadproof,checks=$PASS_COUNT,exit=$exit_code,gate_ok=${GATE_OK:-0},restore=$restore,end_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ),result=FAIL"
		[ "$exit_code" != 0 ] || exit_code=1
	fi
	rm -rf "$W"
	exit "$exit_code"
}
trap teardown EXIT

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

THRESHOLD=$(cad_value threshold)
MAXR=$(cad_value maxReplicaCount)
MINR=$(cad_value minReplicaCount)
POLL=$(cad_value pollingInterval)
COOLDOWN=$(cad_value cooldownPeriod)
STABILIZATION=$(cad_value scaleDownStabilizationSeconds)
for v in "$THRESHOLD" "$MAXR" "$MINR" "$POLL" "$COOLDOWN" "$STABILIZATION"; do
	case "$v" in '' | *[!0-9]*) fail "values.yaml keda.cad has a missing or non-integer value" ;; esac
done
[ "$MAXR" = 2 ] || fail "values.yaml keda.cad.maxReplicaCount is $MAXR; this proof drives the queue above 2"
[ "$MINR" = 0 ] || fail "values.yaml keda.cad.minReplicaCount is $MINR; a scale-from-zero proof needs 0"
ENGINE_S=$(configmap_value CAD_ENGINE_TIMEOUT | tr -d 's')
CONCURRENCY=$(configmap_value CAD_WORKER_CONCURRENCY)
case "$ENGINE_S" in '' | *[!0-9]*) fail "configmap CAD_ENGINE_TIMEOUT is not whole seconds" ;; esac
DRAIN_BOUND=$((COOLDOWN + STABILIZATION + 300))

ev "HDR,mode=keda_cad_loadproof,date_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ),commit=$COMMIT,tracked_dirt=$DIRT,load1=$(load1),vm_free_gib=$(gib "$FREE"),context=$CONTEXT,keda_version=$KEDA_VERSION,values=values-local.yaml"
ev "HDR,keda_cad_threshold=$THRESHOLD,min_replica_count=$MINR,max_replica_count=$MAXR,polling_interval=$POLL,cooldown_period=$COOLDOWN,scale_down_stabilization_seconds=$STABILIZATION,cad_worker_concurrency=$CONCURRENCY,cad_engine_timeout_s=$ENGINE_S,drain_bound_s=$DRAIN_BOUND"
ev "RULE,key=zero,rule=before each trigger cad-worker status.replicas is 0, the ScaledObject Fallback condition is not True and the external metric is present with value 0"
ev "RULE,key=max,rule=three jobs reach replicas 2 and readyReplicas 2 within 300 s and no 2 s sample sees more than $MAXR"
ev "RULE,key=quiet_host,rule=each trigger waits for load1 <= $QUIET_LOAD; a step whose load1 is above $QUIET_LOAD at its start or its end is recorded contaminated=1 and its timings do not count"
ev "RULE,key=drain,rule=drain_s = first sample with status.replicas 0 minus the last finished_at; drain_s <= cooldownPeriod + scaleDownStabilizationSeconds + 300 = $DRAIN_BOUND"

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

step "fixtures"
FIXTURE_CONTAINER="octoconv-cad-loadproof-fixture-$$"
mkdir -p "$W/out"
chmod 777 "$W/out"
kt 900 docker run --name "$FIXTURE_CONTAINER" --network none --cpus 2 --memory 4g --memory-swap 4g --user nobody -e HOME=/tmp -e LANG=C.UTF-8 \
	-v "$ROOT:/src:ro" -v "$ROOT/$CORPUS/public:/base:ro" -v "$W/out:/out" -w /src \
	--entrypoint /opt/venv/bin/python octoconv-cad-worker:dev -I -B scripts/cad-corpus-gen.py \
	--scaled-from /base/LinearSlide-MGN15x-Block.step --kind step-nauo --targets 1000 --scaled-dir /out >/dev/null
kt 60 docker rm -fv "$FIXTURE_CONTAINER" >/dev/null
FIXTURE_CONTAINER=""
NAUO="$W/out/step-nauo-LinearSlide-MGN15x-Block-n1000.step"
[ -s "$NAUO" ] || fail "the step-nauo n=1000 input was not generated"
CAL_INPUT="$CORPUS/synthetic/units_inch.step"
[ -s "$CAL_INPUT" ] || fail "missing $CAL_INPUT"
ev "FIXTURE,name=step-nauo-LinearSlide-MGN15x-Block-n1000.step,bytes=$(wc -c < "$NAUO" | tr -d ' '),sha256=$(shasum -a 256 "$NAUO" | cut -d' ' -f1)"
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

step "install octoconv with values-local.yaml only"
INSTALLED_OCTOCONV=1
kt 300 helm install octoconv "$CHART_DIR" -f "$VALUES_LOCAL" -n "$NAMESPACE" --create-namespace
pass "helm install octoconv"
kt 300 kubectl -n "$NAMESPACE" wait --for=condition=Available deployment/redis --timeout=280s || fail "deployment/redis not Available"
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
	kt 320 kubectl -n "$NAMESPACE" wait --for=condition=Available "deployment/$d" --timeout=300s || fail "deployment/$d not Available"
	pass "deployment/$d Available"
done

step "port-forward and client"
kubectl port-forward -n "$NAMESPACE" svc/api "${API_LOCAL_PORT}:8090" > "$W/api-pf.log" 2>&1 &
API_PF_PID=$!
kubectl port-forward -n "$NAMESPACE" svc/postgres "${DB_LOCAL_PORT}:5432" > "$W/db-pf.log" 2>&1 &
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
	kt 300 go run ./cmd/manage-clients create "keda-cad-loadproof-$(date +%s)")
KEY=$(printf '%s\n' "$CLIENT_OUT" | awk -F': ' '/^api key/ { print $2 }')
[ -n "$KEY" ] || fail "could not mint a client"
pass "client minted (key redacted, ${#KEY} chars)"

psqlq() { kn exec deploy/postgres -- psql -U octo -d octo_db -tAc "$1" | tr -d '\r'; }

submit() {
	local path=$1 name=$2 target=$3 op=${4:-} code args
	args=(-s -o "$W/post.json" -w '%{http_code}' --max-time 120 -X POST "$API_BASE/v1/jobs" -H "Authorization: ApiKey $KEY" -F "target=$target" -F "file=@$path;filename=$name")
	[ -z "$op" ] || args+=(-F "operation=$op")
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
		sleep 5
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

pod_events() {
	local pod=$1 ready=$2 s pl pd st pm
	s=$(event_ts "$pod" Scheduled)
	pl=$(event_ts "$pod" Pulling)
	pd=$(event_ts "$pod" Pulled)
	st=$(event_ts "$pod" Started)
	pm=$(kn get events --field-selector "involvedObject.name=$pod,reason=Pulled" -o json 2>/dev/null | jq -r '.items[0].message // ""' | grep -c 'already present' || true)
	ev "POD_EVENTS,pod=$pod,scheduled=${s:-absent},pulling=${pl:-absent},pulled=${pd:-absent},pulled_already_present=$pm,started=${st:-absent},ready=${ready:-absent}"
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
			kn get pods -o wide 2>&1 | sed 's/^/DIAG: /'
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
READY_TS=""
for _ in $(seq 1 40); do
	READY_TS=$(kn get pod "$CAL_POD" -o json | jq -r '(.status.conditions // [])[] | select(.type == "Ready" and .status == "True") | .lastTransitionTime') || READY_TS=""
	[ -n "$READY_TS" ] && break
	sleep 3
done
[ -n "$READY_TS" ] || fail "cad pod $CAL_POD was not Ready within 120 s"
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
CAL_STATUS=$(wait_job "$CAL_JOB" $((ENGINE_S + 60)))
[ "$CAL_STATUS" = "done" ] || fail "calibration job $CAL_JOB ended $CAL_STATUS"
CAL_DONE=$(nowf)
SCHED=$(event_ts "$CAL_POD" Scheduled)
STARTED=$(event_ts "$CAL_POD" Started)
[ -n "$SCHED" ] && [ -n "$STARTED" ] || fail "cad pod $CAL_POD has no Scheduled or Started event"
PULLING=$(event_ts "$CAL_POD" Pulling)
pod_events "$CAL_POD" "$READY_TS"
ok=""
while le "$(diff_s "$CAL_DONE" "$(nowf)")" "$DRAIN_BOUND"; do
	[ "$(cad_replicas)" = 0 ] && { ok=1; break; }
	sleep 3
done
[ -n "$ok" ] || fail "cad-worker did not drain to 0 within $DRAIN_BOUND s of the calibration job"
CAL_DRAINED=$(diff_s "$CAL_DONE" "$(nowf)")
quiet_line calibration "$CAL_L0" "$(load1)" na
ev "CALIBRATION,job=$CAL_JOB,pod=$CAL_POD,submit_utc=$(iso_at "$CAL_T0"),ready_utc=$READY_TS,submit_to_ready_s=$SUBMIT_TO_READY,scheduled_to_started_s=$(diff_s "$(epoch_of "$SCHED")" "$(epoch_of "$STARTED")"),pull_events=$([ -n "$PULLING" ] && echo present || echo absent),pod_image_id=$POD_IMAGE_ID,sandbox_proven=1,status=done,drained_s=$CAL_DRAINED,drain_bound_s=$DRAIN_BOUND,result=ok"
pass "calibration"

step "timed 0->2->0"
zero_check timed
: > "$W/samples"
: > "$W/transitions"
: > "$W/pods"
rm -f "$W/sampler.stop" "$W/t0"

sampler() {
	local prev="0 0" cur c t t0 r ready spec pods
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
		pods=$(kn get pod -l "$CAD_LABEL" -o json 2>/dev/null | jq -r '.items[] | "\(.metadata.name) \([(.status.conditions // [])[] | select(.type == "Ready" and .status == "True") | .lastTransitionTime][0] // "-")"' 2>/dev/null) || pods=""
		if [ -n "$pods" ]; then
			echo "$pods" | while read -r name rt; do
				[ -n "$name" ] || continue
				if ! grep -q "^$name " "$W/pods"; then echo "$name -" >> "$W/pods"; fi
				if [ "$rt" != "-" ] && grep -q "^$name -$" "$W/pods"; then
					sed -i '' "s|^$name -\$|$name $rt|" "$W/pods"
				fi
			done
		fi
		sleep 2
	done
}

set -m
sampler &
SAMPLER_PID=$!
set +m
sleep 3
T_L0=$(wait_quiet) || fail "load1 stayed above $QUIET_LOAD for $QUIET_WAIT_S s before the timed step"
T0=$(nowf)
echo "$T0" > "$W/t0"
JOBS=""
for i in 1 2 3; do
	j=$(submit "$NAUO" step-nauo-LinearSlide-MGN15x-Block-n1000.step stl)
	[ -n "$j" ] || fail "no job id for timed job $i"
	JOBS="$JOBS $j"
done
ev "TIMED_SUBMIT,submit_utc=$(iso_at "$T0"),jobs=$(printf '%s' "${JOBS# }" | tr ' ' ';'),target=stl"
ok=""
while le "$(diff_s "$T0" "$(nowf)")" 300; do
	last=$(grep -v ' err$' "$W/samples" | tail -n 1)
	if [ "$(echo "$last" | awk '{ print $3 }')" = 2 ] && [ "$(echo "$last" | awk '{ print $4 }')" = 2 ]; then ok=1; break; fi
	sleep 2
done
[ -n "$ok" ] || fail "cad-worker did not reach replicas 2 and readyReplicas 2 within 300 s (last sample: $last)"
pass "cad-worker at 2 replicas, 2 ready"

LAST_DONE=0
for j in $JOBS; do
	s=$(wait_job "$j" $((3 * ENGINE_S)))
	[ "$s" = "done" ] || fail "timed job $j ended $s"
	att=$(psqlq "select attempts from jobs where id = '$j'")
	qa=$(psqlq "select count(*) from job_events where job_id = '$j' and from_status = 'queued' and to_status = 'active'")
	fin=$(psqlq "select extract(epoch from finished_at) from jobs where id = '$j'")
	wall=$(psqlq "select round(extract(epoch from finished_at - created_at)::numeric, 1) from jobs where id = '$j'")
	[ "$att" = 1 ] || fail "timed job $j has attempts=$att, want 1"
	[ "$qa" = 1 ] || fail "timed job $j has $qa queued->active transitions, want 1"
	ev "JOB,id=$j,attempts=$att,queued_to_active=$qa,status=done,wall_s=$wall,finished_utc=$(iso_at "$fin")"
	awk -v a="$fin" -v b="$LAST_DONE" 'BEGIN { exit !(a > b) }' && LAST_DONE=$fin
done
pass "three timed jobs done, one attempt each"

ok=""
while le "$(diff_s "$LAST_DONE" "$(nowf)")" $((DRAIN_BOUND + 30)); do
	if awk -v d="$LAST_DONE" '$2 != "err" && $1 > d && $3 == 0 { f = 1 } END { exit !f }' "$W/samples"; then ok=1; break; fi
	sleep 3
done
sleep 6
stop_sampler
[ -n "$ok" ] || fail "cad-worker did not drain to 0 within $((DRAIN_BOUND + 30)) s of the last job"
ZERO_AT=$(awk -v d="$LAST_DONE" '$2 != "err" && $1 > d && $3 == 0 { print $1; exit }' "$W/samples")
DRAIN_S=$(diff_s "$LAST_DONE" "$ZERO_AT")
tee -a "$OUT" < "$W/transitions"
NTRANS=$(wc -l < "$W/transitions" | tr -d ' ')
[ "$NTRANS" -ge 2 ] || fail "only $NTRANS TRANSITION lines"
head -n 1 "$W/transitions" | grep -q '^TRANSITION,from=0,' || fail "the first TRANSITION does not start from 0"
tail -n 1 "$W/transitions" | grep -q ',to=0,ready=0,' || fail "the last TRANSITION does not end at 0"
awk -F'at_utc=' '{ split($2, a, ","); if (NR > 1 && a[1] < p) bad = 1; p = a[1] } END { exit bad }' "$W/transitions" || fail "TRANSITION timestamps decrease"
MAXOBS=$(awk '$2 != "err" { if ($2 > m) m = $2; if ($3 > m) m = $3 } END { print m + 0 }' "$W/samples")
NSAMPLES=$(grep -vc ' err$' "$W/samples" || true)
NERR=$(grep -c ' err$' "$W/samples" || true)
[ "$MAXOBS" = "$MAXR" ] || fail "the sampler's maximum was $MAXOBS replicas, want exactly $MAXR"
ev "MAX_REPLICAS,observed=$MAXOBS,max_replica_count=$MAXR,samples=$NSAMPLES,sample_errors=$NERR,result=ok"
pass "maximum replicas exactly $MAXR"
if grep -q ',from=2,to=1,' "$W/transitions"; then DRAIN_PATH="2->1->0"; else DRAIN_PATH="2->0"; fi
while read -r pod rt; do
	[ -n "$pod" ] || continue
	pod_events "$pod" "$([ "$rt" = - ] && echo "" || echo "$rt")"
done < "$W/pods"
le "$DRAIN_S" "$DRAIN_BOUND" || fail "drain took $DRAIN_S s, above the bound $DRAIN_BOUND s"
quiet_line timed "$T_L0" "$(load1)" "$(awk '$2 != "err" { if ($5 > m) m = $5 } END { print m + 0 }' "$W/samples")"
ev "DRAIN,last_done_utc=$(iso_at "$LAST_DONE"),zero_utc=$(iso_at "$ZERO_AT"),drain_s=$DRAIN_S,bound_s=$DRAIN_BOUND,path=$DRAIN_PATH,result=ok"
pass "drained to 0 in $DRAIN_S s ($DRAIN_PATH)"

GATE_OK=1
echo "=== ALL $PASS_COUNT CHECKS PASSED; teardown and restore follow ==="
