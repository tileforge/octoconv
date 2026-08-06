#!/usr/bin/env bash
# quota-multireplica-liveproof.sh -- Phase 55 (QUOT-05) live hard gate.
#
# Proves, against a REAL OrbStack Kubernetes cluster with api.replicas: 2,
# that the Phase 52 quota admission mechanism (internal/jobs/admission.go:
# pg_advisory_xact_lock on quotaLockKeys(clientID, engine) + a COUNT over
# queued+active inside the SAME transaction as the job insert) enforces a
# client's per-engine CONCURRENCY CAP K correctly when N concurrent
# job-creation requests hit TWO SEPARATE api pods, not one:
#   SC1: with api.replicas: 2, N concurrent job-creation requests from one
#     client whose per-engine concurrency cap is K result in EXACTLY K
#     admitted jobs, never more, regardless of which replica served which
#     request.
#   SC2: timestamps, replica identifiers, and admitted/rejected counts are
#     captured as committed evidence.
#
# WHY THIS SCRIPT EXISTS -- `go test -race` alone is NOT sufficient proof:
# internal/jobs/admission_test.go's TestConcurrentBurstAdmitsExactlyK
# (:198) proves the cap holds under N concurrent GOROUTINES INSIDE ONE
# PROCESS sharing ONE *pgxpool.Pool. It cannot prove -- and does not even
# attempt to prove -- that TWO SEPARATE api processes on two pods, with
# separate connection pools and separate Postgres connections, racing the
# SAME pg_advisory_xact_lock key pair, still hold the cap. That is the
# actual production topology QUOT-05 names, and it is exactly what this
# script exercises: real HTTP requests, over the real network, landing on
# two real, independent api pods.
#
# EXPLICITLY OUT OF SCOPE (CONTEXT.md D-03): the daily-jobs cap and the
# daily-bytes cap -- SC1 names concurrency specifically, and the
# daily-bytes cap additionally carries Phase 52's documented D-15
# imprecision. Also out of scope: running this gate in CI (needs a real
# multi-replica cluster) and any change to internal/jobs/admission.go's
# algorithm -- this script proves the mechanism, it never edits it.
#
# THE REACHABILITY DIVERGENCE (read this before touching this script):
# this script reaches the api by Service ClusterIP, NEVER by
# `kubectl port-forward` to the api Service, because a port-forwarded
# Service connection pins to a SINGLE backend pod for its lifetime --
# correct for every prior live gate in this repo (scripts/keda-gate.sh and
# every keda-*-loadproof.sh/keda-*-downscale-survival.sh sibling all ran at
# api.replicas: 1, where this distinction never mattered) but FATAL here,
# because it would make the cross-pod admit/reject assertion this gate
# exists to make permanently unwinnable. A `kubectl port-forward` to the
# postgres Service is still used below and is fine -- it only carries
# manage-clients/psql traffic, never the burst, so its single-backend
# pinning is not the load-bearing path.
#
# THE MIGRATION-RACE CONSTRAINT: cmd/api/main.go calls db.Migrate on every
# process start, and internal/db/db.go's Migrate takes NO advisory lock
# (only a check-then-apply against schema_migrations) -- so two api pods
# starting simultaneously against a fresh database would race. The install
# below is deliberately two-phase: install at --set api.replicas=1 first,
# wait for api Available (migrations applied by that single pod), THEN
# helm upgrade into the overlay's real api.replicas: 2.
#
# ORBSTACK CALLER CONTRACT (same discipline as every sibling live gate,
# STATE.md, four recorded daemon wedges): the CALLER must ensure the
# compose stack is DOWN and that all app images are pre-built SEQUENTIALLY
# with the pinned `dev` tag BEFORE this script runs. This script never runs
# `docker build` or `docker compose up`. It tears the k8s stack down
# unconditionally on exit via a trap, success or failure, so OrbStack is
# never left hot.
#
# THIS SCRIPT'S EXIT CODE IS THE GATE: any failed assertion aborts non-zero
# with a loud FAIL line, and every check prints the value it observed, so
# the transcript is self-documenting evidence.
set -euo pipefail

cd "$(dirname "$0")/.."

# ---------------------------------------------------------------------------
# Config / constants (all env-overridable via ${VAR:-default}).
# ---------------------------------------------------------------------------
NAMESPACE="${NAMESPACE:-octoconv}"
CHART_DIR="${CHART_DIR:-deploy/chart/octoconv}"
VALUES_LOCAL="${VALUES_LOCAL:-deploy/chart/octoconv/values-local.yaml}"
VALUES_MULTIREPLICA="${VALUES_MULTIREPLICA:-deploy/chart/octoconv/values-multireplica.yaml}"
ENGINE="${ENGINE:-image}"
TARGET_FORMAT="${TARGET_FORMAT:-jpg}"
FIXTURE="${FIXTURE:-internal/e2e/testdata/sample.png}"
FIXTURE_CONTENT_TYPE="${FIXTURE_CONTENT_TYPE:-image/png}"
QUOTA_K="${QUOTA_K:-2}"
BURST_N="${BURST_N:-10}"
ROUNDS="${ROUNDS:-10}"
# BURST_SEND_SPREAD_MS (Plan 55-02 Task 1): the max allowed spread, in
# milliseconds, between the earliest and latest send_ts in one round's
# burst -- the "concurrency of flight" assertion (assess_round, appended
# below) fails a round whose requests were serialized rather than genuinely
# in flight together.
BURST_SEND_SPREAD_MS="${BURST_SEND_SPREAD_MS:-1000}"
DB_LOCAL_PORT="${DB_LOCAL_PORT:-15434}"
API_KEY_SALT="${API_KEY_SALT:-dev-only-change-me-in-real-deploys}"
EVIDENCE_DIR="${EVIDENCE_DIR:-.planning/phases/55-live-gate-multi-replica-quota-correctness/evidence}"
RUN_ID="${RUN_ID:-$(date -u +%Y%m%dT%H%M%SZ)}"
# EVIDENCE_TSV (Plan 55-02 Task 1): the committed SC2 evidence artifact --
# one row per burst request across all ROUNDS rounds, plus a trailing
# #-commented summary block (STEP 8, appended below). Named by RUN_ID so a
# re-run never clobbers a prior run's committed evidence.
EVIDENCE_TSV="${EVIDENCE_DIR}/quota-multireplica-${RUN_ID}.tsv"

PASS_COUNT=0
GATE_OK=""
DB_PF_PID=""
CALIBRATE_ONLY=""
API_POD_1=""
API_POD_2=""
CLUSTER_IP=""

# ---------------------------------------------------------------------------
# Assertion helpers -- every one prints the observed value on success and
# aborts non-zero with a loud FAIL naming the observed value on failure, so
# the transcript is self-documenting evidence (mirrors every sibling live
# gate in scripts/).
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

# assert_nonempty_redacted -- same non-empty check as assert_nonempty, but
# NEVER echoes the raw value into the (committed) gate transcript. The
# minted client's API key must never be printed verbatim.
assert_nonempty_redacted() {
	local value="$1" label="$2"
	if [ -z "$value" ]; then
		echo "FAIL: $label -- expected a non-empty value, got empty" >&2
		exit 1
	fi
	PASS_COUNT=$((PASS_COUNT + 1))
	echo "PASS: $label == [REDACTED, ${#value} chars]"
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

# assert_ge: numeric "a >= b" check.
assert_ge() {
	local a="$1" b="$2" label="$3"
	if [ "$a" -lt "$b" ]; then
		echo "FAIL: $label -- $a is less than required $b" >&2
		exit 1
	fi
	PASS_COUNT=$((PASS_COUNT + 1))
	echo "PASS: $label ($a >= $b)"
}

# assert_le: numeric "a <= b" check.
assert_le() {
	local a="$1" b="$2" label="$3"
	if [ "$a" -gt "$b" ]; then
		echo "FAIL: $label -- $a exceeds $b" >&2
		exit 1
	fi
	PASS_COUNT=$((PASS_COUNT + 1))
	echo "PASS: $label ($a <= $b)"
}

log() { echo ""; echo "--- $* ---"; }

# ---------------------------------------------------------------------------
# Arg parsing -- iterate over ALL of "$@" (never test only "$1" -- a sibling
# gate's 50-REVIEW.md recorded a defect where --calibrate-only was matched
# only as an exact $1). Any unknown argument aborts with a usage line
# BEFORE any cluster contact, WORKDIR creation, or trap installation.
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

# WORKDIR: never a fixed /tmp name (a sibling gate's 50-REVIEW.md also
# recorded a fixed-path fixture/scratch-file collision finding) -- always a
# fresh mktemp -d, removed in the teardown trap below.
WORKDIR="$(mktemp -d)"

# ---------------------------------------------------------------------------
# Teardown -- ALWAYS runs (trap on EXIT), success or failure. Kills the
# postgres port-forward first so helm uninstall isn't fighting a live
# connection, then uninstalls octoconv, then deletes the namespace. Must
# NEVER delete $EVIDENCE_DIR -- that is committed evidence, not scratch.
# ---------------------------------------------------------------------------
teardown() {
	local exit_code=$?
	echo ""
	echo "=== TEARDOWN (OrbStack must never be left hot) ==="

	if [ -n "$DB_PF_PID" ]; then
		kill "$DB_PF_PID" >/dev/null 2>&1 || true
	fi

	helm uninstall octoconv -n "$NAMESPACE" >/dev/null 2>&1 || true
	kubectl delete namespace "$NAMESPACE" --wait=false >/dev/null 2>&1 || true

	rm -rf "$WORKDIR"

	echo ""
	if [ "$exit_code" -eq 0 ] && [ "$GATE_OK" = "1" ]; then
		echo "PASS -- Phase 55 (QUOT-05) quota multi-replica live gate complete ($PASS_COUNT checks)."
	else
		echo "FAIL -- Phase 55 (QUOT-05) quota multi-replica live gate did NOT complete (exit=$exit_code, checks passed=$PASS_COUNT)." >&2
	fi
	exit "$exit_code"
}
trap teardown EXIT

echo "=== Phase 55 (QUOT-05) quota multi-replica live gate -- run $RUN_ID ==="

# ---------------------------------------------------------------------------
# STEP 1: Preflight.
# ---------------------------------------------------------------------------
log "STEP 1: preflight"

kubectl get nodes >/dev/null
echo "PASS: kubectl reaches the OrbStack cluster (context: $(kubectl config current-context))"

for cmd in helm kubectl curl awk mkfifo; do
	command -v "$cmd" >/dev/null 2>&1 || {
		echo "FAIL: required command '$cmd' not found on PATH" >&2
		exit 1
	}
done
echo "PASS: helm, kubectl, curl, awk, mkfifo all present on PATH (jq is NOT required -- this script parses JSON with grep/cut, matching the existing scripts)"

[ -f "$FIXTURE" ] || {
	echo "FAIL: fixture file not found at $FIXTURE" >&2
	exit 1
}
echo "PASS: fixture file present at $FIXTURE"

COMPOSE_UP=$(docker compose ps --format '{{.Names}}' 2>/dev/null | grep -c '^octoconv-' || true)
if [ "${COMPOSE_UP:-0}" -gt 0 ]; then
	echo "FAIL: compose stack appears to be UP ($COMPOSE_UP octoconv-* containers running) -- compose and k8s stacks must NEVER be hot simultaneously (four recorded OrbStack daemon wedges, STATE.md). Run 'docker compose ... down -v' first." >&2
	exit 1
fi
echo "PASS: compose stack is down (0 octoconv-* containers running)"

# Millisecond-precision send/receive timestamps (Plan 55-02 Task 1): macOS
# `date` has no `%3N` (that flag is GNU-date-only), so this shells out to
# python3 (preferred) or perl's Time::HiRes (fallback). Asserted here, in
# STEP 1, so a missing tool fails in the first ten seconds of the run
# rather than mid-round after cluster/client setup has already happened.
TS_TOOL=""
if command -v python3 >/dev/null 2>&1; then
	TS_TOOL="python3"
elif command -v perl >/dev/null 2>&1; then
	TS_TOOL="perl"
else
	echo "FAIL: neither python3 nor perl found on PATH -- one is required for millisecond-precision send/receive timestamps (macOS 'date' has no %3N)." >&2
	exit 1
fi
echo "PASS: millisecond-timestamp tool available ($TS_TOOL)"

# ---------------------------------------------------------------------------
# STEP 2: Two-phase install -- Phase A forces api.replicas=1 so exactly one
# pod ever runs db.Migrate against a fresh database (the migration-race
# constraint, see header); Phase B upgrades into the overlay's real
# api.replicas: 2 only after Phase A's single pod is Available.
# ---------------------------------------------------------------------------
log "STEP 2: two-phase install (migration-race guard)"

helm upgrade --install octoconv "$CHART_DIR" -f "$VALUES_LOCAL" -f "$VALUES_MULTIREPLICA" -n "$NAMESPACE" --create-namespace --set api.replicas=1
echo "PASS: Phase A helm upgrade --install (api.replicas forced to 1) complete"

for d in postgres redis minio api worker; do
	kubectl wait --for=condition=Available "deployment/$d" -n "$NAMESPACE" --timeout=300s
	echo "PASS: deployment/$d Available (Phase A)"
done

helm upgrade octoconv "$CHART_DIR" -f "$VALUES_LOCAL" -f "$VALUES_MULTIREPLICA" -n "$NAMESPACE"
echo "PASS: Phase B helm upgrade (api.replicas from overlay = 2) complete"

kubectl rollout status deployment/api -n "$NAMESPACE" --timeout=300s
echo "PASS: deployment/api rollout complete (Phase B)"

# ---------------------------------------------------------------------------
# STEP 3: Replica / endpoint / pod-set assertions -- the SC1 precondition.
# ---------------------------------------------------------------------------
log "STEP 3: replica / endpoint / pod-set assertions (SC1 precondition)"

assert_eq "2" "$(kubectl get deployment api -n "$NAMESPACE" -o jsonpath='{.spec.replicas}')" "api Deployment spec.replicas"

ENDPOINT_COUNT="0"
ENDPOINT_SOURCE="endpoints"
waited=0
while [ "$waited" -lt 120 ]; do
	ENDPOINT_COUNT=$(kubectl get endpoints api -n "$NAMESPACE" -o jsonpath='{.subsets[*].addresses[*].ip}' 2>/dev/null | wc -w | tr -d '[:space:]')
	ENDPOINT_SOURCE="endpoints"
	if [ "${ENDPOINT_COUNT:-0}" != "2" ]; then
		# Fallback: EndpointSlice, for clusters where the legacy Endpoints
		# object comes back empty.
		ENDPOINT_COUNT=$(kubectl get endpointslice -n "$NAMESPACE" -l kubernetes.io/service-name=api -o jsonpath='{.items[*].endpoints[*].addresses[*]}' 2>/dev/null | wc -w | tr -d '[:space:]')
		ENDPOINT_SOURCE="endpointslice"
	fi
	if [ "${ENDPOINT_COUNT:-0}" = "2" ]; then
		break
	fi
	sleep 3
	waited=$((waited + 3))
done
echo "endpoint source used: $ENDPOINT_SOURCE"
assert_eq "2" "${ENDPOINT_COUNT:-0}" "api Service ready endpoint count"

API_PODS=$(kubectl get pods -n "$NAMESPACE" -l app.kubernetes.io/component=api -o jsonpath='{.items[*].metadata.name}')
API_POD_COUNT=$(printf '%s\n' "$API_PODS" | wc -w | tr -d '[:space:]')
assert_eq "2" "$API_POD_COUNT" "api pod count discovered"
API_POD_1=$(printf '%s\n' "$API_PODS" | awk '{print $1}')
API_POD_2=$(printf '%s\n' "$API_PODS" | awk '{print $2}')
assert_nonempty "$API_POD_1" "API_POD_1 discovered"
assert_nonempty "$API_POD_2" "API_POD_2 discovered"
echo "api pods: $API_POD_1 $API_POD_2"
printf '%s\n' "$API_POD_1" "$API_POD_2" | sort >"$WORKDIR/api-pods-start.txt"
echo "PASS: pod set recorded to $WORKDIR/api-pods-start.txt for the end-of-run pod-set-unchanged assertion (Plan 55-02)"

# ---------------------------------------------------------------------------
# STEP 4: ClusterIP discovery + calibration -- the mandatory early proof of
# RESEARCH.md's P0 assumption A1 (host->ClusterIP routing genuinely
# distributes new connections across BOTH ready api pods), run BEFORE any
# burst budget is spent.
# ---------------------------------------------------------------------------
log "STEP 4: ClusterIP discovery + calibration (RESEARCH.md P0 assumption A1)"

CLUSTER_IP=$(kubectl get svc api -n "$NAMESPACE" -o jsonpath='{.spec.clusterIP}')
assert_nonempty "$CLUSTER_IP" "api Service ClusterIP discovered"
if [ "$CLUSTER_IP" = "None" ]; then
	echo "FAIL: api Service ClusterIP is the literal 'None' -- a headless Service has no DNAT layer to distribute burst traffic through. The gate cannot proceed on this reachability path." >&2
	exit 1
fi

CALIB_N=20
for i in $(seq 1 "$CALIB_N"); do
	(
		code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 -H "X-Request-Id: calib-${RUN_ID}-${i}" "http://${CLUSTER_IP}:8090/healthz")
		echo "$code" >"$WORKDIR/calib-${i}.status"
	) &
done
wait

CALIB_200_COUNT=0
for i in $(seq 1 "$CALIB_N"); do
	status=$(cat "$WORKDIR/calib-${i}.status" 2>/dev/null || true)
	if [ "$status" = "200" ]; then
		CALIB_200_COUNT=$((CALIB_200_COUNT + 1))
	fi
done

if [ "$CALIB_200_COUNT" -eq 0 ]; then
	echo "FAIL: calibration -- 0/${CALIB_N} probes to http://${CLUSTER_IP}:8090/healthz returned 200. host->ClusterIP is not reachable from this host at all; RESEARCH.md assumption A1 (OrbStack routes ClusterIP from the Mac host) does not hold in this environment. NetworkPolicy is a documented NON-cause (deploy/chart/octoconv/templates/networkpolicy-metrics.yaml lines 20-23 record that no CNI NetworkPolicy controller ships with OrbStack). The required remedy is the Pattern 1 fallback -- run the burst generator from a throwaway pod INSIDE the octoconv namespace -- which is a plan change, not a script flag; stop and report." >&2
	exit 1
fi
assert_eq "$CALIB_N" "$CALIB_200_COUNT" "calibration probes returning 200"

# Let container log lines flush before grepping kubectl logs.
sleep 3
POD1_HITS=$(kubectl logs "$API_POD_1" -n "$NAMESPACE" 2>/dev/null | grep -c "\[calib-${RUN_ID}-" || true)
POD2_HITS=$(kubectl logs "$API_POD_2" -n "$NAMESPACE" 2>/dev/null | grep -c "\[calib-${RUN_ID}-" || true)
POD1_HITS="${POD1_HITS:-0}"
POD2_HITS="${POD2_HITS:-0}"

if [ "$POD1_HITS" -eq 0 ] || [ "$POD2_HITS" -eq 0 ]; then
	echo "FAIL: calibration -- host->ClusterIP is reachable but did NOT distribute across both api pods (pod1=$POD1_HITS, pod2=$POD2_HITS). The cross-pod assertion (CONTEXT D-05) can never be satisfied on this path -- fall back to the in-cluster burst generator (RESEARCH.md Pattern 1 fallback)." >&2
	exit 1
fi
assert_ge "$POD1_HITS" "1" "calibration probes landed on API_POD_1 ($API_POD_1)"
assert_ge "$POD2_HITS" "1" "calibration probes landed on API_POD_2 ($API_POD_2)"

CALIB_SUM=$((POD1_HITS + POD2_HITS))
assert_eq "$CALIB_N" "$CALIB_SUM" "calibration pod-hit sum equals probes fired"

echo "PASS: calibration -- host->ClusterIP routing distributed $CALIB_N probes across both api pods (pod1=$POD1_HITS, pod2=$POD2_HITS) -- this line is itself SC2 evidence that two replicas really served traffic"

if [ -n "$CALIBRATE_ONLY" ]; then
	GATE_OK=1
	echo ""
	echo "=== --calibrate-only: $PASS_COUNT checks passed ==="
	echo "calibration summary: pod1=$API_POD_1 hits=$POD1_HITS, pod2=$API_POD_2 hits=$POD2_HITS, total=$CALIB_SUM/$CALIB_N"
	exit 0
fi

# ---------------------------------------------------------------------------
# STEP 5: postgres port-forward, DB ground truth, throwaway client
# provisioning. Everything below this point runs only when --calibrate-only
# was NOT passed.
#
# DB GROUND TRUTH: the concurrency slot is queued+active occupancy with NO
# release code anywhere in this codebase (Phase 52 D-06/D-08) -- a slot
# frees only on a terminal transition. The only ground truth for "is the
# cap actually free right now" is therefore the SAME predicate evaluate()
# itself uses (internal/jobs/admission.go:239-242); asking the API would be
# asking the component under test, not an independent check on it.
# ---------------------------------------------------------------------------
log "STEP 5: postgres port-forward, DB ground truth, throwaway client provisioning"

psql_q() {
	kubectl exec -n "$NAMESPACE" deploy/postgres -- psql -U octo -d octo_db -tAc "$1" | tr -d '[:space:]'
}

PSQL_PREFLIGHT=$(psql_q 'SELECT 1' || true)
if [ "$PSQL_PREFLIGHT" != "1" ]; then
	echo "FAIL: psql_q preflight -- 'SELECT 1' returned [$PSQL_PREFLIGHT], expected [1]. Retry with PGPASSWORD sourced from the release Secret: kubectl get secret octoconv-secret -n octoconv -o jsonpath='{.data.POSTGRES_PASSWORD}' | base64 -d" >&2
	exit 1
fi
assert_eq "1" "$PSQL_PREFLIGHT" "psql_q preflight (SELECT 1 against postgres pod)"

# inflight_count mirrors internal/jobs/admission.go:239-242's concurrency
# predicate EXACTLY (status IN ('queued','active')) -- ground truth, not an
# approximation of it.
inflight_count() {
	local client_id="$1"
	psql_q "SELECT count(*) FROM jobs WHERE client_id = '${client_id}' AND engine = '${ENGINE}' AND status IN ('queued','active')"
}

# Postgres port-forward -- deliberate and safe: it carries ONLY
# manage-clients/psql traffic, never the burst, so its single-backend
# pinning (the same kubectl-port-forward limitation the header warns about
# for the api Service) is irrelevant here -- postgres is a single-replica
# Deployment anyway.
kubectl port-forward -n "$NAMESPACE" svc/postgres "${DB_LOCAL_PORT}:5432" >"$WORKDIR/db-pf.log" 2>&1 &
DB_PF_PID=$!
sleep 3

export DATABASE_URL="postgres://octo:octo-pass@127.0.0.1:${DB_LOCAL_PORT}/octo_db"
export API_KEY_SALT="$API_KEY_SALT"

# Mint a FRESH client per run: a previous aborted run's stuck queued/active
# jobs belong to a DIFFERENT client_id and therefore cannot consume this
# run's cap (Pitfall 5).
CLIENT_OUT=$(go run ./cmd/manage-clients create "quota-multireplica-liveproof-${RUN_ID}")
CLIENT_ID=$(printf '%s\n' "$CLIENT_OUT" | awk -F': ' '/^client id/{print $2}')
CLIENT_KEY=$(printf '%s\n' "$CLIENT_OUT" | awk -F': ' '/^api key/{print $2}')
if [ -z "$CLIENT_ID" ] || [ -z "$CLIENT_KEY" ]; then
	echo "FAIL: could not parse client id / api key from manage-clients create output:" >&2
	echo "$CLIENT_OUT" >&2
	exit 1
fi
assert_nonempty "$CLIENT_ID" "minted client id"
assert_nonempty_redacted "$CLIENT_KEY" "minted client api key"

go run ./cmd/manage-clients set-limit "$CLIENT_ID" "$ENGINE" "$QUOTA_K" - -
go run ./cmd/manage-clients set-mode "$CLIENT_ID" enforce
echo "PASS: set-limit + set-mode invoked (max-concurrent=$QUOTA_K, unlimited daily bytes/jobs, enforcement_mode=enforce)"

# ANTI-VACUITY READBACK, not a hope: without these four readbacks, an
# observe-mode or kill-switch-disabled run would admit all N requests and
# the gate would be measuring nothing.
LIMIT_MAX_CONCURRENT=$(psql_q "SELECT max_concurrent FROM client_limits WHERE client_id='${CLIENT_ID}' AND engine='${ENGINE}'")
assert_eq "$QUOTA_K" "$LIMIT_MAX_CONCURRENT" "client_limits.max_concurrent readback"

LIMIT_DAILY_BYTES_NULL=$(psql_q "SELECT (max_daily_bytes IS NULL) FROM client_limits WHERE client_id='${CLIENT_ID}' AND engine='${ENGINE}'")
assert_eq "t" "$LIMIT_DAILY_BYTES_NULL" "client_limits.max_daily_bytes IS NULL readback"

LIMIT_DAILY_JOBS_NULL=$(psql_q "SELECT (max_daily_jobs IS NULL) FROM client_limits WHERE client_id='${CLIENT_ID}' AND engine='${ENGINE}'")
assert_eq "t" "$LIMIT_DAILY_JOBS_NULL" "client_limits.max_daily_jobs IS NULL readback"

CLIENT_ENFORCEMENT_MODE=$(psql_q "SELECT enforcement_mode FROM clients WHERE id='${CLIENT_ID}'")
assert_eq "enforce" "$CLIENT_ENFORCEMENT_MODE" "clients.enforcement_mode readback"

QUOTA_KILL_SWITCH=$(kubectl exec -n "$NAMESPACE" "$API_POD_1" -- printenv QUOTA_ENFORCEMENT_DISABLED 2>/dev/null || true)
QUOTA_KILL_SWITCH="${QUOTA_KILL_SWITCH:-false}"
if [ "$QUOTA_KILL_SWITCH" = "true" ]; then
	echo "FAIL: QUOTA_ENFORCEMENT_DISABLED=true on $API_POD_1 -- the service-wide quota kill-switch is on. Without this readback an observe-mode or kill-switch-disabled run would admit all requests and the gate would measure nothing." >&2
	exit 1
fi
assert_eq "false" "$QUOTA_KILL_SWITCH" "QUOTA_ENFORCEMENT_DISABLED kill-switch off (read from $API_POD_1)"

assert_eq "0" "$(inflight_count "$CLIENT_ID")" "fresh client starts with zero in-flight jobs"

# ---------------------------------------------------------------------------
# Drain helpers -- called by Plan 55-02 between rounds. A concurrency slot
# frees ONLY on a terminal transition (queued/active occupy it; Phase 52
# D-06/D-08 -- there is deliberately no release code anywhere).
# ---------------------------------------------------------------------------

# job_status JOB_ID -- GETs /v1/jobs/{id} via the ClusterIP and echoes the
# status field.
job_status() {
	local job_id="$1"
	curl -s --max-time 10 -H "Authorization: ApiKey $CLIENT_KEY" "http://${CLUSTER_IP}:8090/v1/jobs/${job_id}" |
		grep -o '"status":"[^"]*"' | head -1 | cut -d'"' -f4
}

# cancel_job JOB_ID -- POSTs /v1/jobs/{id}/cancel via the ClusterIP. A
# non-2xx is tolerated and never aborts the gate -- the job may already be
# terminal by the time this fires.
cancel_job() {
	local job_id="$1"
	curl -s -o /dev/null -w '%{http_code}' --max-time 10 -X POST \
		-H "Authorization: ApiKey $CLIENT_KEY" "http://${CLUSTER_IP}:8090/v1/jobs/${job_id}/cancel" >/dev/null 2>&1 || true
}

# wait_terminal JOB_ID TIMEOUT_S -- polls job_status every 2s until the
# status is one of done/failed/canceled. Returns 0 on terminal, 1 on
# timeout; prints the last observed status either way.
wait_terminal() {
	local job_id="$1" timeout_s="$2" waited=0 status=""
	while [ "$waited" -lt "$timeout_s" ]; do
		status=$(job_status "$job_id" || true)
		if [ "$status" = "done" ] || [ "$status" = "failed" ] || [ "$status" = "canceled" ]; then
			echo "$status"
			return 0
		fi
		sleep 2
		waited=$((waited + 2))
	done
	echo "$status"
	return 1
}

# drain_round JOB_IDS -- waits every job in the given whitespace-separated
# list to a terminal status (180s budget), cancelling and re-waiting (60s)
# any that time out, then ASSERTS inflight_count is 0. This assertion is a
# CORRECTNESS requirement, not hygiene: if round R+1 starts with the cap
# still consumed, it silently admits fewer than K, which would still
# satisfy "never more than K admitted" and pass the check VACUOUSLY (D-06's
# own flagged pitfall).
drain_round() {
	local job_ids="$1" job_id status remaining
	for job_id in $job_ids; do
		status=$(wait_terminal "$job_id" 180) || {
			echo "job $job_id did not reach terminal within 180s (last status: $status) -- cancelling"
			cancel_job "$job_id"
			status=$(wait_terminal "$job_id" 60) || true
		}
		echo "drain: job $job_id last status=$status"
	done
	remaining=$(inflight_count "$CLIENT_ID")
	if [ "$remaining" != "0" ]; then
		echo "FAIL: drain_round -- inflight_count after draining [$job_ids] is $remaining, expected 0. A round starting with the cap still consumed would admit fewer than K and pass the 'never more than K' check VACUOUSLY (D-06)." >&2
		exit 1
	fi
	PASS_COUNT=$((PASS_COUNT + 1))
	echo "PASS: drain_round -- all jobs [$job_ids] terminal, inflight_count($CLIENT_ID)=0"
}

# ---------------------------------------------------------------------------
# Burst generator (Plan 55-02 Task 1). epoch_ms/fire_burst are called once
# per round by run_round (Plan 55-02 Task 2) inside the STEP 6 main loop
# (Plan 55-02 Task 3, appended below this block).
# ---------------------------------------------------------------------------

# epoch_ms -- millisecond-precision epoch timestamp. See STEP 1's TS_TOOL
# preflight assertion above for why python3/perl rather than `date +%s%3N`
# (unsupported on macOS/BSD date).
epoch_ms() {
	if [ "$TS_TOOL" = "python3" ]; then
		python3 -c 'import time; print(int(time.time() * 1000))'
	else
		perl -MTime::HiRes -e 'printf("%d\n", Time::HiRes::time() * 1000)'
	fi
}

# fire_burst ROUND -- FIFO-barrier burst: BURST_N requests, each from its
# own curl PROCESS (never a reused connection), released together by a
# single write to a per-round named pipe.
#
# ONE-CURL-PROCESS-PER-REQUEST IS LOAD-BEARING, NOT STYLISTIC (RESEARCH.md
# Pitfall 2): kube-proxy makes its Service DNAT endpoint choice once per
# NEW TCP connection. A single curl process reusing one keep-alive
# connection across all BURST_N requests of a round would pin every
# request in that round to whichever pod the FIRST connection landed on --
# collapsing the cross-pod distribution the calibration step (STEP 4)
# already proved works, even though ClusterIP reachability itself would
# still be fine. Do NOT collapse this into one curl invocation with
# --next/multiple -O, and do NOT switch to an HTTP client that pools
# connections across requests without explicitly disabling keep-alive.
fire_burst() {
	local round="$1" i
	local barrier="$WORKDIR/barrier-${round}"
	mkfifo "$barrier"

	for i in $(seq 1 "$BURST_N"); do
		(
			exec 3<"$barrier"
			read -r -u 3 _ # blocks until the barrier below is released
			local request_id="burst-${RUN_ID}-${round}-${i}"
			local send_ts recv_ts http_status
			send_ts=$(epoch_ms)
			http_status=$(curl -s -o "$WORKDIR/r${round}-req${i}.json" -w '%{http_code}' \
				--max-time 60 \
				-H "Authorization: ApiKey $CLIENT_KEY" \
				-H "X-Request-Id: burst-${RUN_ID}-${round}-${i}" \
				-X POST "http://${CLUSTER_IP}:8090/v1/jobs" \
				-F "target=${TARGET_FORMAT}" \
				-F "file=@${FIXTURE};type=${FIXTURE_CONTENT_TYPE}")
			recv_ts=$(epoch_ms)
			printf '%s\t%s\t%s\t%s\t%s\n' "$i" "$request_id" "$send_ts" "$recv_ts" "$http_status" \
				>"$WORKDIR/r${round}-req${i}.rec"
		) &
	done

	# Let every backgrounded subshell reach its blocking read before
	# releasing the barrier -- without this, a slow-to-fork subshell could
	# still be between `exec 3<...` and `read` when the write below lands,
	# missing the release entirely (a race in the barrier mechanism itself,
	# not the thing under test).
	sleep 0.3
	printf 'go\n' >"$barrier"
	wait
	rm -f "$barrier"
}

# Evidence TSV initialization -- must happen before round 1 fires. Header
# field order is fixed and matched by STEP 7's aggregate `awk -F'\t'`
# assertions below: round(1) index(2) request_id(3) send_ts_ms(4)
# recv_ts_ms(5) http_status(6) body_code(7) serving_pod(8).
mkdir -p "$EVIDENCE_DIR"
printf 'round\tindex\trequest_id\tsend_ts_ms\trecv_ts_ms\thttp_status\tbody_code\tserving_pod\n' >"$EVIDENCE_TSV"
EVIDENCE_TSV_ABS="$(cd "$(dirname "$EVIDENCE_TSV")" && pwd)/$(basename "$EVIDENCE_TSV")"
echo "PASS: evidence TSV initialized at $EVIDENCE_TSV_ABS"
