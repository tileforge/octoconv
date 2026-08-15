#!/usr/bin/env bash
# inspect-burst-liveproof.sh -- Phase 58 (INSP-06) SC6 live hard gate.
#
# WHAT THIS PROVES, dословно by SC6: a pathological burst of synchronous
# POST /v1/inspect requests against a LIVE api Deployment, in a REAL OrbStack
# Kubernetes cluster, leaves GET /healthz answering inside kubelet's actual
# liveness contract for the WHOLE run -- and, independently, that the api
# container's own restartCount never moves. It does NOT prove, and does not
# attempt to prove, that every individual inspect request returns a
# "correct" report body -- internal/inspect's own unit tests already cover
# per-format parsing correctness; this fixture family (scripts/fixtures/
# README.md) is deliberately built to stress CEILINGS, not parser
# correctness, and this script's job stops at "the fleet stayed up", never
# "the report was right".
#
# WHY `go test -race` IS NOT ENOUGH: internal/api's own inspectgate_test.go
# and inspect_handlers_test.go prove the concurrency semaphore, the byte/
# time budget (internal/inspect/budget.go's Tracker), and the per-client
# rate limiter all behave as documented -- INSIDE ONE PROCESS, driven by Go's
# own scheduler, never touching a real kubelet, a real liveness probe, or a
# real HTTP accept path under genuine concurrent load. Whether a REAL
# kubelet, polling a REAL container's REAL :8091 listener under a REAL
# pathological burst, ever accumulates six consecutive liveness failures and
# restarts the pod is a production-topology question no unit test asks --
# it is exactly what INSP-06 names and exactly what this script exercises:
# real HTTP requests, over the real network, landing on one real api pod,
# observed by an independent poller that never shares a process with either
# side.
#
# THE ORBSTACK OPERATOR CONTRACT (same discipline as every sibling live
# gate in this directory, e.g. scripts/quota-multireplica-liveproof.sh's own
# header, STATE.md's four recorded OrbStack daemon wedges): the CALLER must
# have already brought the compose stack down and assembled fresh
# octoconv-api/octoconv-worker images, SEQUENTIALLY, tagged with a pinned,
# non-"latest" tag, BEFORE this script runs. This script never brings the
# compose stack up and never assembles an image itself -- it only checks,
# in STEP 1 below, that the compose stack is down and that an image tagged
# $APP_IMAGE_TAG already exists locally, failing loudly and by name if
# either precondition is unmet. It tears the k8s install down
# UNCONDITIONALLY on exit via a trap, success or failure, so OrbStack is
# never left hot.
#
# WHY THE BURST RUNS FROM AN IN-CLUSTER POD, NEVER FROM THIS HOST AND NEVER
# THROUGH `kubectl port-forward`: this project's own live-gate history
# already answered this question once, the hard way (Phase 55,
# .planning/milestones/v1.12-phases/55-live-gate-multi-replica-quota-correctness/evidence/README.md,
# calibration-FAILED-20260806T214551Z.log) -- a real operator run found 0 of
# 20 calibration probes to the api Service's ClusterIP returning anything at
# all from this macOS host; OrbStack's Kubernetes does not route a Service
# ClusterIP from the host through the real kube-proxy DNAT path the way a
# conformant CNI would. `kubectl port-forward` is separately, and always,
# wrong for a burst regardless of that finding: a port-forwarded connection
# pins to ONE backend TCP connection for its lifetime, which would collapse
# this gate's own POLLER and its BURST DRIVER onto the same single
# connection the target api pod happens to be serving through -- exactly
# the confound this script's separate-pods design (STEP 6/7 below) exists
# to avoid. Both the poller and the burst driver therefore run from
# throwaway pods INSIDE the octoconv namespace, dialing the api Service's
# ClusterIP the way any ordinary in-cluster client would -- Kubernetes
# Service routing from inside the cluster is not the same claim the
# host-routing finding above disproved, and this project's own
# NetworkPolicy (deploy/chart/octoconv/templates/networkpolicy-metrics.yaml,
# ingress rule (c)) was written specifically to admit exactly this in-
# cluster :8091 traffic for this gate.
#
# THE ROUND-4 DEFECT AND ITS DIRECT ANALOG HERE (read this before touching
# the poll-failure assertion below -- Plan 55-07 (раунд 4, "round 4" in the
# committed evidence's own filename),
# .planning/milestones/v1.12-phases/55-live-gate-multi-replica-quota-correctness/evidence/README.md
# "gate-run4-FAILED-round4-20260807T141252Z.log"): that gate's round 4
# asserted an EXACT admitted count without first proving the precondition
# under which that exact count was required to hold -- a legitimate
# concurrency-slot race became indistinguishable from a real cap violation,
# because nothing had checked, independently, whether the precondition
# (no admitted job goes terminal mid-burst) actually held for that specific
# round. THE DIRECT ANALOG THIS SCRIPT MUST NOT REPEAT: asserting "GET
# /healthz returned 200 on EVERY single poll across the whole run" as this
# gate's pass condition. At the chart's own liveness contract
# (periodSeconds:10, failureThreshold:6, deployment-api.yaml -- unchanged by
# this phase, restated here because it is the number this whole gate is
# calibrated against), a single slow or missed response recovering on the
# next tick is NOT a production-contract failure; asserting zero tolerance
# for any individual miss would produce a false "gate failed" on ordinary
# scheduling noise, indistinguishable from a genuine liveness violation --
# exactly the false-equivalence the round-4 defect taught this project to
# stop making. This script's actual pass bar (STEP 11 below) is stated
# against the REAL contract instead: the longest run of CONSECUTIVE poll
# failures must stay strictly below 6 (roughly 60 continuous seconds of
# failure, the exact threshold that trips a kubelet restart), and the
# independent ground truth is the api container's own restartCount, snapshot
# before and after -- never the poller's own read of itself.
#
# THIS SCRIPT'S EXIT CODE IS THE GATE: every assertion below prints the
# value it observed on BOTH success and failure, so a committed transcript
# is self-documenting evidence; any failed assertion aborts non-zero with a
# loud FAIL line naming what was expected and what was actually observed.
set -euo pipefail

cd "$(dirname "$0")/.."

# ---------------------------------------------------------------------------
# Config / constants (env-overridable via ${VAR:-default}, mirroring every
# sibling live gate in this directory).
# ---------------------------------------------------------------------------
NAMESPACE="${NAMESPACE:-octoconv}"
CHART_DIR="${CHART_DIR:-deploy/chart/octoconv}"
VALUES_LOCAL="${VALUES_LOCAL:-deploy/chart/octoconv/values-local.yaml}"
VALUES_INSPECT_GATE="${VALUES_INSPECT_GATE:-deploy/chart/octoconv/values-inspect-gate.yaml}"
APP_IMAGE_TAG="${APP_IMAGE_TAG:-dev}"

# BURST_N: parallel POST /v1/inspect requests fired together per round,
# rotating through the three committed pathological fixtures
# (scripts/fixtures/README.md). 9 (a multiple of 3) keeps the rotation even
# across rounds; comfortably above INSPECT_MAX_CONCURRENT's default (4) so
# every round genuinely saturates the concurrency semaphore
# (internal/api/inspectgate.go) at least once, exercising its own 429
# inspect_busy path, not merely the byte/time ceilings inside each analyzer.
BURST_N="${BURST_N:-9}"
# ROUNDS: repeated bursts, so a single lucky (or unlucky) round cannot
# stand in for the whole gate -- mirrors every sibling live gate's own
# multi-round design.
ROUNDS="${ROUNDS:-10}"
# BURST_ROUND_TIMEOUT_S bounds ONE round's kubectl exec. A round is BURST_N
# concurrent requests each bounded by the api's own INSPECT_MAX_WORK_DURATION
# (2s in values-inspect-gate.yaml) behind a semaphore of INSPECT_MAX_CONCURRENT
# (4), so an honest round finishes in seconds; observed rounds on this gate's
# own reference stand run ~1.3s. 60s is therefore generous by more than an
# order of magnitude -- it exists to catch a STALL, never to time a slow-but-
# working round, and a round that genuinely needs a minute is itself a finding
# worth failing on. See the watchdog at STEP 10 for the run that made this
# necessary.
BURST_ROUND_TIMEOUT_S="${BURST_ROUND_TIMEOUT_S:-60}"
# POLL_INTERVAL_S / POLL_TIMEOUT_S: the SAME cadence and per-probe timeout
# kubelet itself uses against :8091 (deployment-api.yaml's own
# livenessProbe: periodSeconds:10 governs the RESTART contract; this
# poller samples at 1s -- ten times kubelet's own resolution -- specifically
# so the "six CONSECUTIVE failures" assertion below has enough resolution
# to distinguish a genuine multi-second stall from a single slow tick, not
# so it reproduces kubelet's own schedule exactly).
POLL_INTERVAL_S="${POLL_INTERVAL_S:-1}"
POLL_TIMEOUT_S="${POLL_TIMEOUT_S:-1}"
# MAX_CONSECUTIVE_POLL_FAILURES: the actual liveness contract this gate is
# calibrated against (deployment-api.yaml: periodSeconds:10,
# failureThreshold:6 -- six consecutive failures at kubelet's OWN 10s
# cadence is ~60s of continuous failure before a restart). This poller
# samples once per second, ten times kubelet's resolution, so "six
# consecutive FAILURES OF THIS POLLER" is a materially STRICTER bar than
# kubelet's own six-consecutive-10s-failures contract -- deliberately: a
# poller sampling ten times as often that never accumulates six
# consecutive misses proves kubelet, sampling ten times more sparsely,
# could not have either. Loosening this number without re-deriving it
# against BOTH periodSeconds and this poller's own POLL_INTERVAL_S would
# quietly weaken the bar below what deployment-api.yaml actually enforces.
MAX_CONSECUTIVE_POLL_FAILURES="${MAX_CONSECUTIVE_POLL_FAILURES:-6}"

DB_LOCAL_PORT="${DB_LOCAL_PORT:-15435}"
API_KEY_SALT="${API_KEY_SALT:-dev-only-change-me-in-real-deploys}"
EVIDENCE_DIR="${EVIDENCE_DIR:-.planning/phases/58-synchronous-inspection/evidence}"
RUN_ID="${RUN_ID:-$(date -u +%Y%m%dT%H%M%SZ)}"
# RUN_ID_LOWER: Kubernetes object names must be RFC 1123 lowercase; RUN_ID's
# own default format carries uppercase T/Z -- lowered once here, used for
# every pod name below, so a stale pod from a previous run can never be
# silently reused.
RUN_ID_LOWER=$(printf '%s' "$RUN_ID" | tr '[:upper:]' '[:lower:]')

# BURST_IMAGE: same pinned, curl-carrying image every sibling live gate in
# this directory already depends on (scripts/quota-multireplica-liveproof.sh's
# own BURST_IMAGE doc comment explains the rejected alternatives: no
# busybox, no plain alpine curl image -- both lack a toolchain this
# script's inline drivers need).
BURST_IMAGE="${BURST_IMAGE:-buildpack-deps:bookworm-curl}"

FIXTURE_PDF="${FIXTURE_PDF:-scripts/fixtures/inspect-burst-pathological.pdf}"
FIXTURE_DOCX="${FIXTURE_DOCX:-scripts/fixtures/inspect-burst-pathological.docx}"
FIXTURE_MP4="${FIXTURE_MP4:-scripts/fixtures/inspect-burst-pathological.mp4}"

# EVIDENCE_POLL_TSV / EVIDENCE_BURST_TSV: committed, re-checkable evidence --
# named by RUN_ID so a repeat run never clobbers a prior one.
EVIDENCE_POLL_TSV="${EVIDENCE_DIR}/inspect-burst-${RUN_ID}-poll.tsv"
EVIDENCE_BURST_TSV="${EVIDENCE_DIR}/inspect-burst-${RUN_ID}-burst.tsv"

PASS_COUNT=0
GATE_OK=""
DB_PF_PID=""
POLL_EXEC_PID=""
POLL_POD=""
BURST_POD=""
API_POD=""
CLUSTER_IP=""

# ---------------------------------------------------------------------------
# Assertion helpers -- every one prints the observed value on success AND on
# failure, mirroring every sibling live gate in this directory.
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

assert_nonempty_redacted() {
	local value="$1" label="$2"
	if [ -z "$value" ]; then
		echo "FAIL: $label -- expected a non-empty value, got empty" >&2
		exit 1
	fi
	PASS_COUNT=$((PASS_COUNT + 1))
	echo "PASS: $label == [REDACTED, ${#value} chars]"
}

assert_ge() {
	local a="$1" b="$2" label="$3"
	if [ "$a" -lt "$b" ]; then
		echo "FAIL: $label -- $a is less than required $b" >&2
		exit 1
	fi
	PASS_COUNT=$((PASS_COUNT + 1))
	echo "PASS: $label ($a >= $b)"
}

assert_lt() {
	local a="$1" b="$2" label="$3"
	if [ "$a" -ge "$b" ]; then
		echo "FAIL: $label -- $a is not strictly less than $b" >&2
		exit 1
	fi
	PASS_COUNT=$((PASS_COUNT + 1))
	echo "PASS: $label ($a < $b)"
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
# Arg parsing -- iterate over ALL of "$@", never test only "$1" (a sibling
# gate's own review recorded a defect where an early flag was matched only
# as an exact $1).
# ---------------------------------------------------------------------------
for arg in "$@"; do
	case "$arg" in
	--calibrate-only)
		CALIBRATE_ONLY=1
		;;
	*)
		echo "usage: $0 [--calibrate-only]" >&2
		exit 1
		;;
	esac
done
CALIBRATE_ONLY="${CALIBRATE_ONLY:-}"

# WORKDIR: never a fixed path -- always a fresh mktemp -d, removed
# unconditionally in the teardown trap below.
WORKDIR="$(mktemp -d)"

# ---------------------------------------------------------------------------
# Teardown -- ALWAYS runs (trap on EXIT), success or failure. Stops the
# poller first (it would otherwise keep polling a namespace mid-deletion),
# then the postgres port-forward, then the two throwaway pods explicitly
# (a leaked pod keeping OrbStack hot is exactly the failure mode this
# project has recorded daemon wedges from -- delete it first,
# unconditionally, before the namespace delete would eventually collect it
# anyway), then uninstalls the release and deletes the namespace. NEVER
# deletes $EVIDENCE_DIR -- that is committed evidence, not scratch.
# ---------------------------------------------------------------------------
teardown() {
	local exit_code=$?
	echo ""
	echo "=== TEARDOWN (OrbStack must never be left hot) ==="

	if [ -n "$POLL_EXEC_PID" ]; then
		kill "$POLL_EXEC_PID" >/dev/null 2>&1 || true
		wait "$POLL_EXEC_PID" 2>/dev/null || true
	fi

	if [ -n "$DB_PF_PID" ]; then
		kill "$DB_PF_PID" >/dev/null 2>&1 || true
	fi

	if [ -n "$POLL_POD" ]; then
		kubectl delete pod "$POLL_POD" -n "$NAMESPACE" --force --grace-period=0 --wait=false >/dev/null 2>&1 || true
	fi
	if [ -n "$BURST_POD" ]; then
		kubectl delete pod "$BURST_POD" -n "$NAMESPACE" --force --grace-period=0 --wait=false >/dev/null 2>&1 || true
	fi

	helm uninstall octoconv -n "$NAMESPACE" >/dev/null 2>&1 || true
	kubectl delete namespace "$NAMESPACE" --wait=false >/dev/null 2>&1 || true

	rm -rf "$WORKDIR"

	echo ""
	if [ "$exit_code" -eq 0 ] && [ "$GATE_OK" = "1" ]; then
		echo "PASS -- Phase 58 (INSP-06) inspect-burst live gate complete ($PASS_COUNT checks)."
	else
		echo "FAIL -- Phase 58 (INSP-06) inspect-burst live gate did NOT complete (exit=$exit_code, checks passed=$PASS_COUNT)." >&2
	fi
	exit "$exit_code"
}
trap teardown EXIT

echo "=== Phase 58 (INSP-06) inspect-burst live gate -- run $RUN_ID ==="

# ---------------------------------------------------------------------------
# STEP 1: Preflight. Every check here runs BEFORE any cluster mutation.
# ---------------------------------------------------------------------------
log "STEP 1: preflight"

kubectl get nodes >/dev/null
echo "PASS: kubectl reaches the OrbStack cluster (context: $(kubectl config current-context))"

for cmd in helm kubectl docker awk base64; do
	command -v "$cmd" >/dev/null 2>&1 || {
		echo "FAIL: required command '$cmd' not found on PATH" >&2
		exit 1
	}
done
echo "PASS: helm, kubectl, docker, awk, base64 all present on PATH"

for f in "$FIXTURE_PDF" "$FIXTURE_DOCX" "$FIXTURE_MP4"; do
	[ -s "$f" ] || {
		echo "FAIL: fixture file not found or empty at $f -- see scripts/fixtures/README.md (Task 1 of this plan generates these)" >&2
		exit 1
	}
done
echo "PASS: all three pathological fixtures present ($FIXTURE_PDF, $FIXTURE_DOCX, $FIXTURE_MP4)"

# The compose stack and this script's own k8s install must NEVER be hot at
# the same time -- this project's own STATE.md records four OrbStack daemon
# wedges from exactly that overlap. This script never brings the compose
# stack up itself; it only refuses to proceed while it is up.
COMPOSE_UP=$(docker compose ps --format '{{.Names}}' 2>/dev/null | grep -c '^octoconv-' || true)
if [ "${COMPOSE_UP:-0}" -gt 0 ]; then
	echo "FAIL: compose stack appears to be UP ($COMPOSE_UP octoconv-* containers running) -- the compose stack and this script's own Kubernetes install must never be hot simultaneously. Bring the compose stack down first." >&2
	exit 1
fi
echo "PASS: compose stack is down (0 octoconv-* containers running)"

# Image-freshness preflight: a MISSING pre-assembled image is this script's
# job to name loudly, at second zero -- not this script's job to remedy.
# This script never assembles an image itself (see this file's own header
# ORBSTACK OPERATOR CONTRACT paragraph); the operator sequentially prepares
# octoconv-api:$APP_IMAGE_TAG and octoconv-worker:$APP_IMAGE_TAG, pinned,
# non-"latest", BEFORE running this script.
for img_kind in api worker; do
	img_ref="octoconv-${img_kind}:${APP_IMAGE_TAG}"
	docker image inspect "$img_ref" >/dev/null 2>&1 || {
		echo "FAIL: image '$img_ref' not found locally -- this script never assembles images itself. Prepare octoconv-api and octoconv-worker SEQUENTIALLY, pinned to a non-latest tag, before running this gate (see this file's own header comment)." >&2
		exit 1
	}
	echo "PASS: image $img_ref present locally"
done

if ! docker image inspect "$BURST_IMAGE" >/dev/null 2>&1; then
	if ! docker pull "$BURST_IMAGE" >/dev/null 2>&1; then
		echo "FAIL: burst/poll image '$BURST_IMAGE' not present locally and could not be pulled" >&2
		exit 1
	fi
fi
echo "PASS: throwaway pod image $BURST_IMAGE available locally"

mkdir -p "$EVIDENCE_DIR"

# ---------------------------------------------------------------------------
# STEP 2: helm install/upgrade -- single-phase (unlike the multi-replica
# gate's own two-phase dance): api.replicas is pinned to 1 by this gate's
# own overlay (values-inspect-gate.yaml, Task 2 of this plan), so there is
# only ever ONE api pod calling db.Migrate on startup -- the migration-race
# constraint the multi-replica gate's two-phase install exists to dodge
# never arises here.
# ---------------------------------------------------------------------------
log "STEP 2: helm install/upgrade (single-phase, api.replicas pinned to 1)"

helm upgrade --install octoconv "$CHART_DIR" -f "$VALUES_LOCAL" -f "$VALUES_INSPECT_GATE" -n "$NAMESPACE" --create-namespace
echo "PASS: helm upgrade --install complete"

for d in postgres redis minio api worker; do
	kubectl wait --for=condition=Available "deployment/$d" -n "$NAMESPACE" --timeout=300s
	echo "PASS: deployment/$d Available"
done

# ---------------------------------------------------------------------------
# STEP 3: SC6's own precondition -- api.replicas MUST be exactly 1 (D-07).
# internal/ratelimit's rate budget AND internal/api/inspectgate.go's
# concurrency semaphore are BOTH per-process, in-memory state, never shared
# across replicas (58-RESEARCH.md Pitfall 5's own caveat, restated by
# values-inspect-gate.yaml's own comment on api.replicas). A gate run at
# api.replicas > 1 would silently divide the effective inspect budget
# across N processes and prove a WEAKER property than SC6 claims -- this
# check fails loudly, by name, rather than let that happen silently.
# ---------------------------------------------------------------------------
log "STEP 3: api.replicas == 1 precondition (D-07)"

API_REPLICAS_SPEC=$(kubectl get deployment api -n "$NAMESPACE" -o jsonpath='{.spec.replicas}')
if [ "$API_REPLICAS_SPEC" != "1" ]; then
	echo "FAIL: api Deployment spec.replicas is [$API_REPLICAS_SPEC], expected [1]. D-07 (58-CONTEXT.md, restated in values-inspect-gate.yaml's own api.replicas comment): the in-process inspect rate budget and concurrency semaphore are NOT shared across replicas -- a gate run at api.replicas > 1 would prove a materially WEAKER property than SC6 claims. If this Deployment's replica count changed intentionally, the D-07 caveat itself needs re-examining before this gate can mean what it says it means." >&2
	exit 1
fi
assert_eq "1" "$API_REPLICAS_SPEC" "api Deployment spec.replicas (D-07 precondition)"

API_PODS=$(kubectl get pods -n "$NAMESPACE" -l app.kubernetes.io/component=api -o jsonpath='{.items[*].metadata.name}')
API_POD_COUNT=$(printf '%s\n' "$API_PODS" | wc -w | tr -d '[:space:]')
assert_eq "1" "$API_POD_COUNT" "api pod count discovered"
API_POD=$(printf '%s\n' "$API_PODS" | awk '{print $1}')
assert_nonempty "$API_POD" "API_POD discovered"

# ---------------------------------------------------------------------------
# STEP 4: helm get values -- the run's own applied INSPECT_MAX_* boundaries,
# captured live rather than assumed. Evidence gathered against UNKNOWN
# settings proves nothing (this plan's own instruction) -- this is what
# makes the committed evidence re-checkable against the exact ceilings the
# burst actually ran under, not the ceilings this script's config block
# merely intended.
# ---------------------------------------------------------------------------
log "STEP 4: helm get values (applied inspect boundaries, for the evidence record)"

HELM_VALUES_OUT="$WORKDIR/helm-values.yaml"
helm get values octoconv -n "$NAMESPACE" -a >"$HELM_VALUES_OUT"
INSPECT_MAX_WORK_BYTES_APPLIED=$(kubectl get pod "$API_POD" -n "$NAMESPACE" -o jsonpath='{range .spec.containers[0].env[*]}{.name}={.value}{"\n"}{end}' | awk -F= '$1=="INSPECT_MAX_WORK_BYTES"{print $2}')
INSPECT_MAX_UPLOAD_BYTES_APPLIED=$(kubectl get pod "$API_POD" -n "$NAMESPACE" -o jsonpath='{range .spec.containers[0].env[*]}{.name}={.value}{"\n"}{end}' | awk -F= '$1=="INSPECT_MAX_UPLOAD_BYTES"{print $2}')
INSPECT_MAX_CONCURRENT_APPLIED=$(kubectl get pod "$API_POD" -n "$NAMESPACE" -o jsonpath='{range .spec.containers[0].env[*]}{.name}={.value}{"\n"}{end}' | awk -F= '$1=="INSPECT_MAX_CONCURRENT"{print $2}')
assert_nonempty "$INSPECT_MAX_WORK_BYTES_APPLIED" "INSPECT_MAX_WORK_BYTES applied on $API_POD"
assert_nonempty "$INSPECT_MAX_UPLOAD_BYTES_APPLIED" "INSPECT_MAX_UPLOAD_BYTES applied on $API_POD"
assert_nonempty "$INSPECT_MAX_CONCURRENT_APPLIED" "INSPECT_MAX_CONCURRENT applied on $API_POD"
echo "applied ceilings: INSPECT_MAX_WORK_BYTES=$INSPECT_MAX_WORK_BYTES_APPLIED INSPECT_MAX_UPLOAD_BYTES=$INSPECT_MAX_UPLOAD_BYTES_APPLIED INSPECT_MAX_CONCURRENT=$INSPECT_MAX_CONCURRENT_APPLIED"

RESOURCES_LIMITS_CPU=$(kubectl get pod "$API_POD" -n "$NAMESPACE" -o jsonpath='{.spec.containers[0].resources.limits.cpu}')
RESOURCES_LIMITS_MEMORY=$(kubectl get pod "$API_POD" -n "$NAMESPACE" -o jsonpath='{.spec.containers[0].resources.limits.memory}')
assert_nonempty "$RESOURCES_LIMITS_CPU" "api resources.limits.cpu applied (Plan 58-03, D-06b)"
assert_nonempty "$RESOURCES_LIMITS_MEMORY" "api resources.limits.memory applied (Plan 58-03, D-06b)"

# ---------------------------------------------------------------------------
# STEP 5: restartCount BEFORE the burst -- the main ground truth (see this
# file's own header on the round-4 defect and why it, not the poller's own
# read of itself, is what decides this gate).
# ---------------------------------------------------------------------------
log "STEP 5: restartCount snapshot BEFORE the burst"

RESTART_COUNT_BEFORE=$(kubectl get pod "$API_POD" -n "$NAMESPACE" -o jsonpath='{.status.containerStatuses[0].restartCount}')
assert_nonempty "$RESTART_COUNT_BEFORE" "api container restartCount BEFORE"

# ---------------------------------------------------------------------------
# STEP 6: client + API key provisioning (postgres port-forward carries
# ONLY this management traffic, never the burst -- its single-backend
# pinning is irrelevant here, exactly like every sibling gate's own
# postgres port-forward).
# ---------------------------------------------------------------------------
log "STEP 6: client + API key provisioning"

kubectl port-forward -n "$NAMESPACE" svc/postgres "${DB_LOCAL_PORT}:5432" >"$WORKDIR/db-pf.log" 2>&1 &
DB_PF_PID=$!
sleep 3

export DATABASE_URL="postgres://octo:octo-pass@127.0.0.1:${DB_LOCAL_PORT}/octo_db"
export API_KEY_SALT="$API_KEY_SALT"

CLIENT_OUT=$(go run ./cmd/manage-clients create "inspect-burst-liveproof-${RUN_ID}")
CLIENT_ID=$(printf '%s\n' "$CLIENT_OUT" | awk -F': ' '/^client id/{print $2}')
CLIENT_KEY=$(printf '%s\n' "$CLIENT_OUT" | awk -F': ' '/^api key/{print $2}')
if [ -z "$CLIENT_ID" ] || [ -z "$CLIENT_KEY" ]; then
	echo "FAIL: could not parse client id / api key from manage-clients create output:" >&2
	echo "$CLIENT_OUT" >&2
	exit 1
fi
assert_nonempty "$CLIENT_ID" "minted client id"
assert_nonempty_redacted "$CLIENT_KEY" "minted client api key"

printf '%s' "$CLIENT_KEY" >"$WORKDIR/apikey"
chmod 600 "$WORKDIR/apikey"

if [ -n "$CALIBRATE_ONLY" ]; then
	GATE_OK=1
	echo ""
	echo "=== --calibrate-only: preflight, install, D-07 precondition, and client provisioning all passed ($PASS_COUNT checks) ==="
	exit 0
fi

# ---------------------------------------------------------------------------
# STEP 7: independent POLLER pod -- an entirely SEPARATE pod from the burst
# driver (STEP 8 below), created via `kubectl run`. If the poller and the
# driver shared one pod/container, CPU contention between the tool and the
# thing it measures would make this gate measure ITSELF, not the service
# (the exact confound every sibling gate's own separate-pod discipline
# exists to avoid). Polls GET /healthz on port 8091 (Phase 58's dedicated
# health listener, D-06/D-08 -- distinct from :8090, so a pathological
# burst against :8090 cannot starve this probe's own accept path) once per
# POLL_INTERVAL_S with a POLL_TIMEOUT_S timeout -- the same per-probe
# timeout kubelet itself uses by default.
# ---------------------------------------------------------------------------
log "STEP 7: independent poller pod (separate from the burst driver)"

kubectl run octoconv-inspect-poll-"${RUN_ID_LOWER}" \
	--image="$BURST_IMAGE" \
	--restart=Never \
	--namespace="$NAMESPACE" \
	--labels="app.kubernetes.io/component=inspect-poll" \
	--overrides='{"spec":{"automountServiceAccountToken":false,"securityContext":{"runAsNonRoot":true,"runAsUser":65534,"runAsGroup":65534},"containers":[{"name":"poll","image":"'"$BURST_IMAGE"'","command":["sleep","3600"],"securityContext":{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]}},"resources":{"requests":{"cpu":"100m","memory":"32Mi"},"limits":{"cpu":"500m","memory":"128Mi"}}}]}}' \
	-- sleep 3600
POLL_POD="octoconv-inspect-poll-${RUN_ID_LOWER}"
kubectl wait --for=condition=Ready pod/"$POLL_POD" -n "$NAMESPACE" --timeout=180s
echo "PASS: poller pod $POLL_POD ready (image $BURST_IMAGE, component=inspect-poll)"

POLL_URL="http://api.${NAMESPACE}.svc.cluster.local:8091/healthz"
# Deliberate mixed single/double-quote concatenation: the $t0/$code/$t1
# references inside the single-quoted segments must NOT expand on this
# host at all -- they are meant to expand inside the POLLER POD's own
# remote shell once this string is handed to `sh -c` below (kubectl exec),
# while $POLL_TIMEOUT_S/$POLL_URL/$POLL_INTERVAL_S (the double-quoted
# splices) are meant to expand HERE, once, at construction time. Switching
# the whole string to double quotes would make the remote-only variables
# expand against this host's empty environment instead, silently breaking
# the loop.
# shellcheck disable=SC2016
POLL_LOOP='while true; do t0=$(date +%s%3N); code=$(curl -s -o /dev/null -w "%{http_code}" --max-time '"$POLL_TIMEOUT_S"' '"$POLL_URL"' 2>/dev/null || echo 000); t1=$(date +%s%3N); printf "%s\t%s\t%s\n" "$t0" "$code" "$((t1-t0))"; sleep '"$POLL_INTERVAL_S"'; done'
kubectl exec -n "$NAMESPACE" "$POLL_POD" -- sh -c "$POLL_LOOP" >"$WORKDIR/poll-raw.tsv" 2>"$WORKDIR/poll.err" &
POLL_EXEC_PID=$!
sleep 3
if ! kill -0 "$POLL_EXEC_PID" 2>/dev/null; then
	echo "FAIL: poller exec process exited immediately -- stderr:" >&2
	cat "$WORKDIR/poll.err" >&2 2>/dev/null || true
	exit 1
fi
echo "PASS: poller loop running in background (PID $POLL_EXEC_PID), polling $POLL_URL every ${POLL_INTERVAL_S}s"

# ---------------------------------------------------------------------------
# STEP 8: BURST DRIVER pod -- the SECOND, entirely separate throwaway pod,
# also created via `kubectl run`. It fires BURST_N genuinely concurrent
# POST /v1/inspect requests per round (a FIFO-barrier release, the same
# concurrency-forcing shape every sibling burst driver in this directory
# uses), rotating through the three committed pathological fixtures.
# ---------------------------------------------------------------------------
log "STEP 8: burst driver pod (separate from the poller)"

kubectl run octoconv-inspect-burst-"${RUN_ID_LOWER}" \
	--image="$BURST_IMAGE" \
	--restart=Never \
	--namespace="$NAMESPACE" \
	--labels="app.kubernetes.io/component=inspect-burst" \
	--overrides='{"spec":{"automountServiceAccountToken":false,"securityContext":{"runAsNonRoot":true,"runAsUser":65534,"runAsGroup":65534},"containers":[{"name":"burst","image":"'"$BURST_IMAGE"'","command":["sleep","3600"],"securityContext":{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]}},"resources":{"requests":{"cpu":"500m","memory":"64Mi"},"limits":{"cpu":"2","memory":"512Mi"}}}]}}' \
	-- sleep 3600
BURST_POD="octoconv-inspect-burst-${RUN_ID_LOWER}"
kubectl wait --for=condition=Ready pod/"$BURST_POD" -n "$NAMESPACE" --timeout=180s
echo "PASS: burst driver pod $BURST_POD ready (image $BURST_IMAGE, component=inspect-burst -- distinct pod from $POLL_POD)"

POD_WORKDIR="/tmp/octoconv-inspect-burst"
kubectl exec -n "$NAMESPACE" "$BURST_POD" -- mkdir -p "$POD_WORKDIR"

for f in "$FIXTURE_PDF" "$FIXTURE_DOCX" "$FIXTURE_MP4"; do
	base=$(basename "$f")
	base64 <"$f" | kubectl exec -i -n "$NAMESPACE" "$BURST_POD" -- sh -c "base64 -d > ${POD_WORKDIR}/${base}"
	local_sha=$(shasum -a 256 "$f" | awk '{print $1}')
	pod_sha=$(kubectl exec -n "$NAMESPACE" "$BURST_POD" -- sha256sum "${POD_WORKDIR}/${base}" | awk '{print $1}')
	assert_eq "$local_sha" "$pod_sha" "fixture $base sha256 matches after transfer"
done

POD_APIKEY_PATH="${POD_WORKDIR}/apikey"
printf '%s' "$CLIENT_KEY" | kubectl exec -i -n "$NAMESPACE" "$BURST_POD" -- sh -c "umask 077; cat > $POD_APIKEY_PATH"
POD_APIKEY_BYTES=$(kubectl exec -n "$NAMESPACE" "$BURST_POD" -- wc -c "$POD_APIKEY_PATH" | awk '{print $1}')
assert_eq "${#CLIENT_KEY}" "$POD_APIKEY_BYTES" "api key transferred into $BURST_POD (byte count matches; key itself never echoed)"

CLUSTER_IP=$(kubectl get svc api -n "$NAMESPACE" -o jsonpath='{.spec.clusterIP}')
assert_nonempty "$CLUSTER_IP" "api Service ClusterIP discovered"
API_URL="http://api.${NAMESPACE}.svc.cluster.local:8090"

# BURST_DRIVER_SCRIPT: piped into the burst pod via stdin (`sh -s`), never
# committed as a separate file -- this plan's own file list scopes Task 2
# to exactly scripts/inspect-burst-liveproof.sh and
# deploy/chart/octoconv/values-inspect-gate.yaml. Fires N requests as N
# background subshells released together by a single write to a shared
# FIFO (the same concurrency-forcing barrier shape every sibling burst
# driver in this directory uses), rotating through the three transferred
# fixtures round-robin, and prints one TSV row per request to stdout:
# index, fixture_ordinal, http_status, body_code, duration_ms.
BURST_DRIVER_SCRIPT=$(cat <<'DRIVER_EOF'
set -eu
N="$1"; APIKEY_FILE="$2"; URL_BASE="$3"; POD_WORKDIR="$4"
SCRATCH=$(mktemp -d)
trap 'rm -rf "$SCRATCH"' EXIT
# START BARRIER -- a file, deliberately not a FIFO.
#
# This used to be a mkfifo that the parent fed one "go" line per worker while
# all N workers blocked on `read` from it. That is a race, and it is what hung
# this gate three separate times (rounds 6, 21 and 32 of three different
# runs): a FIFO does not distribute lines one-per-reader, so a single reader
# can buffer more than its share, leaving another blocked on `read` forever.
# `wait` then never returns and the whole kubectl exec stalls with every pod
# healthy and the node idle -- which is exactly the confusing shape the
# earlier diagnostics reported (driver container running, restartCount 0, no
# node pressure, /healthz answering in 1.4ms).
#
# Spinning on a file's existence has no such race at any N. The added start
# skew is one poll interval, ~10ms against rounds that take ~1.3s, which
# leaves "genuinely concurrent" as true as the FIFO ever made it.
BARRIER="$SCRATCH/go"
APIKEY_VALUE=$(cat "$APIKEY_FILE")
i=1
while [ "$i" -le "$N" ]; do
	idx=$(( (i - 1) % 3 + 1 ))
	case "$idx" in
	1) FIX="${POD_WORKDIR}/inspect-burst-pathological.pdf"; CT="application/pdf" ;;
	2) FIX="${POD_WORKDIR}/inspect-burst-pathological.docx"; CT="application/vnd.openxmlformats-officedocument.wordprocessingml.document" ;;
	3) FIX="${POD_WORKDIR}/inspect-burst-pathological.mp4"; CT="video/mp4" ;;
	esac
	(
		while [ ! -f "$BARRIER" ]; do sleep 0.01; done
		t0=$(date +%s%3N)
		body="$SCRATCH/resp-${i}.json"
		status=$(curl -s -o "$body" -w '%{http_code}' --max-time 30 \
			-H "Authorization: ApiKey ${APIKEY_VALUE}" \
			-X POST "${URL_BASE}/v1/inspect" \
			-F "file=@${FIX};type=${CT}") || status="000"
		t1=$(date +%s%3N)
		code=$(grep -o '"code":"[^"]*"' "$body" 2>/dev/null | head -1 | cut -d'"' -f4 || true)
		[ -z "$code" ] && code="-"
		printf '%s\t%s\t%s\t%s\t%s\n' "$i" "$idx" "$status" "$code" "$((t1 - t0))" >"$SCRATCH/row-${i}.tsv"
	) &
	i=$((i + 1))
done
: >"$BARRIER"
wait
for i in $(seq 1 "$N"); do
	cat "$SCRATCH/row-${i}.tsv" 2>/dev/null || printf '%s\t?\t000\t-\t-1\n' "$i"
done
DRIVER_EOF
)

# ---------------------------------------------------------------------------
# STEP 9: evidence TSV initialization -- must happen before round 1 fires.
# ---------------------------------------------------------------------------
printf 'round\tindex\tfixture_ordinal\thttp_status\tbody_code\tduration_ms\n' >"$EVIDENCE_BURST_TSV"
EVIDENCE_BURST_TSV_ABS="$(cd "$(dirname "$EVIDENCE_BURST_TSV")" && pwd)/$(basename "$EVIDENCE_BURST_TSV")"
echo "PASS: burst evidence TSV initialized at $EVIDENCE_BURST_TSV_ABS"

TOP_SAMPLES_CPU="$WORKDIR/top-cpu.txt"
TOP_SAMPLES_MEM="$WORKDIR/top-mem.txt"
: >"$TOP_SAMPLES_CPU"
: >"$TOP_SAMPLES_MEM"

# ---------------------------------------------------------------------------
# STEP 10: the ${ROUNDS}-round burst loop.
# ---------------------------------------------------------------------------
log "STEP 10: ${ROUNDS}-round burst loop (N=${BURST_N} per round)"

for r in $(seq 1 "$ROUNDS"); do
	log "round ${r}/${ROUNDS}"

	# EVERY ROUND IS WATCHDOGGED, and the reason is a real run, not caution.
	#
	# Run 20260815T230358Z hung at round 32 and stayed hung: the api pod was
	# healthy throughout (ready=true, restartCount unchanged, /healthz
	# answering 200 in 1.4ms through a FRESH exec into the very same poller
	# pod), the node reported no pressure, and both driver pods were Running
	# -- what stalled was this one kubectl exec stream, not the service under
	# test. Without a bound, that stall is indistinguishable from progress:
	# the script simply stops forever, printing nothing, while an operator
	# waits on a PASS/FAIL line that will never come. A gate that cannot
	# report its own hang is exactly as useless as one that cannot fail.
	#
	# macOS ships no timeout(1) and this project's own live gates must run on
	# the operator's Mac, so the watchdog is a plain background killer rather
	# than coreutils: start the exec, arm a sleeper that TERMs it if the
	# round outlives BURST_ROUND_TIMEOUT_S, and disarm the sleeper the moment
	# the round returns.
	kubectl exec -i -n "$NAMESPACE" "$BURST_POD" -- sh -s -- "$BURST_N" "$POD_APIKEY_PATH" "$API_URL" "$POD_WORKDIR" \
		<<<"$BURST_DRIVER_SCRIPT" >"$WORKDIR/r${r}-rows.tsv" 2>"$WORKDIR/r${r}-driver.err" &
	exec_pid=$!
	( sleep "$BURST_ROUND_TIMEOUT_S"; kill -TERM "$exec_pid" 2>/dev/null || true ) &
	watchdog_pid=$!
	exec_rc=0
	wait "$exec_pid" || exec_rc=$?
	kill -TERM "$watchdog_pid" 2>/dev/null || true
	wait "$watchdog_pid" 2>/dev/null || true
	[ "$exec_rc" -eq 0 ] || {
		if [ "$exec_rc" -ge 128 ]; then
			echo "FAIL: round ${r} -- burst driver exec did not return within ${BURST_ROUND_TIMEOUT_S}s and was killed by this script's own watchdog (exit=${exec_rc}). Check the three reads below to tell a wedged api from a stalled kubectl stream: if the api pod is ready with an unchanged restartCount and /healthz still answers, the stall was in the exec transport, not in the service under test." >&2
		fi
		echo "FAIL: round ${r} -- burst driver exec failed. stderr tail:" >&2
		tail -n 20 "$WORKDIR/r${r}-driver.err" >&2 2>/dev/null || true
		# A bare "command terminated with exit code 137" names a signal, not
		# a cause, and 137 has at least three of them here: the driver
		# container OOMKilled against its own limits.memory, the NODE
		# evicting the pod under memory pressure, or the api pod dying and
		# taking the connection with it. Guessing between them once cost a
		# run; these three reads distinguish them from the cluster's own
		# records, and they are cheap because this branch is already fatal.
		echo "--- burst driver pod state (why 137) ---" >&2
		kubectl get pod "$BURST_POD" -n "$NAMESPACE" -o jsonpath='{range .status.containerStatuses[*]}state={.state}{"\n"}lastState={.lastState}{"\n"}restartCount={.restartCount}{"\n"}{end}' >&2 2>/dev/null || true
		echo "--- node conditions (eviction/memory pressure) ---" >&2
		kubectl get nodes -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{range .status.conditions[*]}{.type}={.status}{" "}{end}{"\n"}{end}' >&2 2>/dev/null || true
		echo "--- namespace events, most recent last ---" >&2
		kubectl get events -n "$NAMESPACE" --sort-by=.lastTimestamp 2>/dev/null | tail -n 15 >&2 || true
		exit 1
	}

	row_count=$(wc -l <"$WORKDIR/r${r}-rows.tsv" | tr -d '[:space:]')
	assert_eq "$BURST_N" "$row_count" "round ${r}: rows returned by burst driver"

	while IFS=$'\t' read -r idx ord status code dur; do
		printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$r" "$idx" "$ord" "$status" "$code" "$dur" >>"$EVIDENCE_BURST_TSV"
	done <"$WORKDIR/r${r}-rows.tsv"

	# kubectl top pod -- CALIBRATION evidence, sampled once per round rather
	# than continuously (metrics-server's own scrape interval bounds how
	# often a tighter loop would even see a new value). Best-effort: a
	# metrics-server unavailable/still-warming-up is an infra-availability
	# concern this gate does not exist to test, so a failed sample here is
	# recorded, never fatal.
	TOP_LINE=$(kubectl top pod "$API_POD" -n "$NAMESPACE" --no-headers 2>/dev/null || true)
	if [ -n "$TOP_LINE" ]; then
		echo "$TOP_LINE" | awk '{print $2}' | tr -d 'm' >>"$TOP_SAMPLES_CPU"
		echo "$TOP_LINE" | awk '{print $3}' | tr -d 'Mi' >>"$TOP_SAMPLES_MEM"
		echo "round ${r}: kubectl top pod $API_POD -> $TOP_LINE"
	else
		echo "round ${r}: kubectl top pod unavailable this sample (metrics-server may still be warming up) -- not fatal"
	fi

	echo "round ${r}: $row_count rows recorded"
done

# ---------------------------------------------------------------------------
# STEP 11: stop the poller, retrieve results, snapshot restartCount AFTER.
# ---------------------------------------------------------------------------
log "STEP 11: stop poller, snapshot restartCount AFTER"

kill "$POLL_EXEC_PID" >/dev/null 2>&1 || true
wait "$POLL_EXEC_PID" 2>/dev/null || true
POLL_EXEC_PID=""

POLL_ROWS=$(wc -l <"$WORKDIR/poll-raw.tsv" | tr -d '[:space:]')
assert_ge "$POLL_ROWS" "1" "poller produced at least one row"

{
	printf 'ts_ms\thttp_status\tduration_ms\n'
	cat "$WORKDIR/poll-raw.tsv"
} >"$EVIDENCE_POLL_TSV"
EVIDENCE_POLL_TSV_ABS="$(cd "$(dirname "$EVIDENCE_POLL_TSV")" && pwd)/$(basename "$EVIDENCE_POLL_TSV")"
echo "PASS: poll evidence TSV written at $EVIDENCE_POLL_TSV_ABS ($POLL_ROWS rows)"

RESTART_COUNT_AFTER=$(kubectl get pod "$API_POD" -n "$NAMESPACE" -o jsonpath='{.status.containerStatuses[0].restartCount}')
assert_nonempty "$RESTART_COUNT_AFTER" "api container restartCount AFTER"

# ---------------------------------------------------------------------------
# STEP 12: end-of-run assertions -- every one re-derivable from the
# committed TSVs alone by a reviewer who never watched the run.
# ---------------------------------------------------------------------------
log "STEP 12: end-of-run assertions"

# THE MAIN GROUND TRUTH: restartCount unchanged. See this file's own header
# on why this, not the poller's own read of itself, is what decides this
# gate (the round-4 defect and its direct analog here).
if [ "$RESTART_COUNT_BEFORE" != "$RESTART_COUNT_AFTER" ]; then
	echo "FAIL: api container restartCount changed during the run -- before=[$RESTART_COUNT_BEFORE] after=[$RESTART_COUNT_AFTER]. The api pod restarted at least once; kubectl describe pod and the previous container's last log lines follow:" >&2
	kubectl describe pod "$API_POD" -n "$NAMESPACE" >&2 2>/dev/null || true
	kubectl logs "$API_POD" -n "$NAMESPACE" --previous --tail=100 >&2 2>/dev/null || true
	exit 1
fi
assert_eq "$RESTART_COUNT_BEFORE" "$RESTART_COUNT_AFTER" "api container restartCount unchanged (main ground truth)"

# MAX CONSECUTIVE POLL FAILURES -- the ACTUAL liveness contract
# (deployment-api.yaml: periodSeconds:10, failureThreshold:6), not a
# zero-tolerance single-probe assertion (see this file's own header, THE
# ROUND-4 DEFECT AND ITS DIRECT ANALOG HERE). A "failure" is any non-200
# response including a curl timeout (recorded as http_status=000).
MAX_CONSECUTIVE_FAILURES=$(awk -F'\t' '
	$2 != "200" { run++; if (run > max) max = run; next }
	{ run = 0 }
	END { print max + 0 }
' "$WORKDIR/poll-raw.tsv")
TOTAL_POLL_FAILURES=$(awk -F'\t' '$2 != "200"' "$WORKDIR/poll-raw.tsv" | wc -l | tr -d '[:space:]')
echo "poll failures: total=${TOTAL_POLL_FAILURES}/${POLL_ROWS}, max consecutive=${MAX_CONSECUTIVE_FAILURES} (informative; only the consecutive-run number below can fail this gate)"
assert_lt "$MAX_CONSECUTIVE_FAILURES" "$MAX_CONSECUTIVE_POLL_FAILURES" "max consecutive /healthz poll failures (deployment-api.yaml's own failureThreshold:6 contract)"

# EVERY BURST RESPONSE IS 200 OR 429, NEVER 5xx, NEVER EMPTY (an empty code
# cell = a hung connection, itself a failure independent of restartCount).
BURST_5XX=$(awk -F'\t' 'NR>1 && $4 ~ /^5/' "$EVIDENCE_BURST_TSV" | wc -l | tr -d '[:space:]')
assert_eq "0" "$BURST_5XX" "burst responses with a 5xx status"

BURST_UNKNOWN_STATUS=$(awk -F'\t' 'NR>1 && $4 != "200" && $4 != "429"' "$EVIDENCE_BURST_TSV" | wc -l | tr -d '[:space:]')
assert_eq "0" "$BURST_UNKNOWN_STATUS" "burst responses with a status other than 200/429"

BURST_EMPTY_STATUS=$(awk -F'\t' 'NR>1 && $4 == ""' "$EVIDENCE_BURST_TSV" | wc -l | tr -d '[:space:]')
assert_eq "0" "$BURST_EMPTY_STATUS" "burst rows with an empty status cell (a hung connection)"

# EVERY 429 CARRIES A KNOWN CODE: inspect_busy (the concurrency semaphore,
# internal/api/inspectgate.go) or rate_limited (RATE_LIMIT_INSPECT_RPM,
# internal/ratelimit) -- no other 429 code should ever appear on this route.
BURST_429_BAD_CODE=$(awk -F'\t' 'NR>1 && $4 == "429" && $5 != "inspect_busy" && $5 != "rate_limited"' "$EVIDENCE_BURST_TSV" | wc -l | tr -d '[:space:]')
assert_eq "0" "$BURST_429_BAD_CODE" "429 responses with a code other than inspect_busy/rate_limited"

# A SEMAPHORE THAT REJECTED EVERYTHING PROVES ONLY THAT IT CAN REJECT --
# never that the accept path also works. At least one 200 across the whole
# run is required.
BURST_200_COUNT=$(awk -F'\t' 'NR>1 && $4 == "200"' "$EVIDENCE_BURST_TSV" | wc -l | tr -d '[:space:]')
assert_ge "$BURST_200_COUNT" "1" "burst responses with status 200 (at least one genuine accept, not only rejects)"

BURST_TOTAL_ROWS=$(awk -F'\t' 'NR>1' "$EVIDENCE_BURST_TSV" | wc -l | tr -d '[:space:]')
assert_eq "$((ROUNDS * BURST_N))" "$BURST_TOTAL_ROWS" "burst TSV data rows (rounds * burst_n)"

# --- THE TWO ASSERTIONS BELOW EXIST BECAUSE THIS GATE ONCE PASSED WITHOUT
# --- BEING ABLE TO FAIL. Read this before relaxing either of them.
#
# The first real run of this gate (20260812T223622Z) reported GATE PASS on 36
# assertions, and every one of them was true. It still proved almost nothing:
#
#   * The whole run lasted 10.3 seconds (11 poll rows at POLL_INTERVAL_S=1).
#     The liveness contract this gate exists to defend needs SIX CONSECUTIVE
#     failures at periodSeconds:10 -- roughly 60 seconds of sustained
#     unavailability -- before kubelet restarts anything. A 10-second
#     observation window CANNOT contain the failure it claims the api
#     survived, so "max consecutive failures 0 < 6" passed vacuously.
#   * RATE_LIMIT_INSPECT_RPM was 30 while the burst sent 90 requests in those
#     10 seconds, so rounds 5 through 10 -- 60 of 90 requests -- were shed at
#     the door by internal/ratelimit and never reached the inspect path at
#     all. Only 27 requests were ever inspected, and only 3 ever reached the
#     concurrency semaphore this gate is named after.
#   * Every one of those 27 was the docx fixture: the pdf and mp4 pathological
#     fixtures were transferred, checksummed, and never actually inspected,
#     while the gate reported PASS on all three being "present".
#
# An assertion that cannot fail is not evidence. Both checks below make the
# scenario prove it was capable of producing the failure it did not produce.

# (1) The observation window must be long enough for the liveness contract to
# have been violable. Anything shorter and a clean poll record is a statement
# about the run's brevity, not about the api's resilience.
#
# periodSeconds and failureThreshold are read from the LIVE Deployment, not
# restated as literals here. Every other mention of "periodSeconds:10,
# failureThreshold:6" in this file is prose that can go stale the moment
# deployment-api.yaml changes; the number this assertion actually divides by
# must come from the object kubelet is really enforcing.
LIVENESS_PERIOD_SECONDS=$(kubectl get deployment api -n "$NAMESPACE" -o jsonpath='{.spec.template.spec.containers[0].livenessProbe.periodSeconds}' 2>/dev/null)
LIVENESS_FAILURE_THRESHOLD=$(kubectl get deployment api -n "$NAMESPACE" -o jsonpath='{.spec.template.spec.containers[0].livenessProbe.failureThreshold}' 2>/dev/null)
if ! [ "${LIVENESS_PERIOD_SECONDS:-0}" -gt 0 ] 2>/dev/null || ! [ "${LIVENESS_FAILURE_THRESHOLD:-0}" -gt 0 ] 2>/dev/null; then
	echo "FAIL: could not read the api container's own livenessProbe periodSeconds/failureThreshold from the live Deployment (got period=[${LIVENESS_PERIOD_SECONDS}] threshold=[${LIVENESS_FAILURE_THRESHOLD}]). This gate's whole claim is stated against that contract; it must not be assumed." >&2
	exit 1
fi
echo "PASS: liveness contract read from the live Deployment == periodSeconds:${LIVENESS_PERIOD_SECONDS} failureThreshold:${LIVENESS_FAILURE_THRESHOLD}"

POLL_SPAN_MS=$(awk -F'\t' 'NR==1{first=$1} {last=$1} END{print last-first}' "$WORKDIR/poll-raw.tsv")
LIVENESS_WINDOW_MS=$((LIVENESS_PERIOD_SECONDS * LIVENESS_FAILURE_THRESHOLD * 1000))
if [ "${POLL_SPAN_MS:-0}" -lt "$LIVENESS_WINDOW_MS" ]; then
	echo "FAIL: the poller observed only ${POLL_SPAN_MS}ms, shorter than the ${LIVENESS_WINDOW_MS}ms this deployment's own liveness contract needs (periodSeconds:${LIVENESS_PERIOD_SECONDS} x failureThreshold:${LIVENESS_FAILURE_THRESHOLD}) before kubelet would restart anything. A clean poll record over a window that short cannot distinguish 'the api survived the burst' from 'the burst ended before the contract could be violated'. Raise ROUNDS (currently ${ROUNDS}) until the run outlasts the window." >&2
	exit 1
fi
assert_ge "$POLL_SPAN_MS" "$LIVENESS_WINDOW_MS" "observation window outlasts the liveness contract it claims the api survived (ms)"

# (2) Every fixture must have been genuinely inspected at least once. A 200
# per fixture is the only thing separating "three pathological formats were
# stressed" from "three files were copied into a pod and one of them was
# stressed".
for ord in 1 2 3; do
	FIXTURE_200=$(awk -F'\t' -v o="$ord" 'NR>1 && $3 == o && $4 == "200"' "$EVIDENCE_BURST_TSV" | wc -l | tr -d '[:space:]')
	assert_ge "$FIXTURE_200" "1" "fixture ordinal ${ord} was actually inspected at least once (status 200), not merely transferred"
done

# RESOURCE CALIBRATION: kubectl top pod peaks vs resources.limits
# (Plan 58-03, D-06b). This is a CALIBRATION check, not a hard cap the
# gate polices on this script's own authority -- exceeding the LIMIT would
# already have OOMKilled the pod (which the restartCount check above would
# catch); this comparison exists so an operator reviewing the evidence can
# see how much headroom the STARTING values actually had under this
# fixture set, and decide whether to recalibrate them (Task 3 of this
# plan, out of this script's own scope).
if [ -s "$TOP_SAMPLES_CPU" ]; then
	PEAK_CPU_MILLI=$(sort -n "$TOP_SAMPLES_CPU" | tail -1)
	PEAK_MEM_MI=$(sort -n "$TOP_SAMPLES_MEM" | tail -1)
	echo "kubectl top pod peaks observed: cpu=${PEAK_CPU_MILLI}m memory=${PEAK_MEM_MI}Mi (against resources.limits cpu=${RESOURCES_LIMITS_CPU} memory=${RESOURCES_LIMITS_MEMORY})"
else
	echo "kubectl top pod: no samples collected this run (metrics-server unavailable throughout) -- recorded as an evidence gap, not a gate failure"
fi

# ---------------------------------------------------------------------------
# STEP 13: evidence summary -- appended as #-prefixed lines to the burst
# TSV so it stays greppable by field, and printed to stdout so it also
# lands in the operator's own `tee`-captured transcript (Task 3 of this
# plan moves that transcript, plus these two TSVs, into $EVIDENCE_DIR under
# the SAME RUN_ID before committing -- this script's own responsibility
# stops at producing the two TSVs below).
# ---------------------------------------------------------------------------
log "STEP 13: evidence summary"

RUN_END_UTC=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
{
	echo "#"
	echo "# inspect-burst-liveproof.sh evidence summary"
	echo "# run_id=${RUN_ID}"
	echo "# run_end_utc=${RUN_END_UTC}"
	echo "# values_files=${VALUES_LOCAL},${VALUES_INSPECT_GATE}"
	echo "# api_replicas_observed_live=${API_REPLICAS_SPEC}"
	echo "# api_pod=${API_POD}"
	echo "# restart_count_before=${RESTART_COUNT_BEFORE} restart_count_after=${RESTART_COUNT_AFTER}"
	echo "# poll_rows=${POLL_ROWS} poll_total_failures=${TOTAL_POLL_FAILURES} poll_max_consecutive_failures=${MAX_CONSECUTIVE_FAILURES}"
	echo "# burst_rounds=${ROUNDS} burst_n=${BURST_N} burst_total_rows=${BURST_TOTAL_ROWS} burst_200=${BURST_200_COUNT}"
	echo "# inspect_max_work_bytes_applied=${INSPECT_MAX_WORK_BYTES_APPLIED}"
	echo "# inspect_max_upload_bytes_applied=${INSPECT_MAX_UPLOAD_BYTES_APPLIED}"
	echo "# inspect_max_concurrent_applied=${INSPECT_MAX_CONCURRENT_APPLIED}"
	echo "# resources_limits_cpu=${RESOURCES_LIMITS_CPU} resources_limits_memory=${RESOURCES_LIMITS_MEMORY}"
	echo "# poll_evidence_tsv=${EVIDENCE_POLL_TSV_ABS}"
	echo "#"
} | tee -a "$EVIDENCE_BURST_TSV"

# =============================================================================
# ALL-PASSED summary.
# =============================================================================
GATE_OK=1
echo ""
echo "=== GATE PASS: ${ROUNDS} rounds x ${BURST_N} requests, ${PASS_COUNT} assertions passed ==="
echo "SC6: restartCount unchanged (${RESTART_COUNT_BEFORE} -> ${RESTART_COUNT_AFTER}); max consecutive /healthz poll failures ${MAX_CONSECUTIVE_FAILURES} < ${MAX_CONSECUTIVE_POLL_FAILURES}; zero 5xx across ${BURST_TOTAL_ROWS} burst responses; ${BURST_200_COUNT} genuine 200 accepts observed; every 429 carried inspect_busy or rate_limited."
echo "Evidence written to ${EVIDENCE_POLL_TSV_ABS} and ${EVIDENCE_BURST_TSV_ABS} (RUN_ID=${RUN_ID})."
echo "IMPORTANT (Task 3 of this plan): commit both evidence files, plus this run's own tee'd transcript under the SAME RUN_ID, e.g.:"
echo "  git add -f ${EVIDENCE_POLL_TSV_ABS} ${EVIDENCE_BURST_TSV_ABS}"
