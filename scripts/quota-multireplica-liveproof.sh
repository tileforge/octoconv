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
# THE REACHABILITY DIVERGENCE (read this before touching this script --
# REWRITTEN after a failed live calibration; the paragraph this replaced
# asserted a reachability path that a real run disproved):
#
# (1) The burst NEVER uses `kubectl port-forward` to the api Service, for
#     the original and still-correct reason: a port-forwarded Service
#     connection pins to a SINGLE backend pod for its lifetime -- correct
#     for every prior live gate in this repo (scripts/keda-gate.sh and
#     every keda-*-loadproof.sh/keda-*-downscale-survival.sh sibling all ran
#     at api.replicas: 1, where this distinction never mattered) but FATAL
#     here, because it would make the cross-pod admit/reject assertion this
#     gate exists to make permanently unwinnable.
#
# (2) The burst ALSO no longer dials the api Service ClusterIP from the
#     macOS host. That WAS this script's original design, and it was
#     DISPROVEN on a real operator run: 0 of 20 calibration probes to the
#     ClusterIP returned 200, independently confirmed against the
#     `kubernetes` Service in `default` (equally unreachable from the same
#     host), so RESEARCH.md's assumption A1 (OrbStack routes a Service
#     ClusterIP from the macOS host through the real kube-proxy DNAT path)
#     does NOT hold in this environment. NetworkPolicy is a documented
#     NON-cause (deploy/chart/octoconv/templates/networkpolicy-metrics.yaml
#     records that OrbStack ships no CNI NetworkPolicy controller). See the
#     committed transcript and its write-up:
#       .planning/phases/55-live-gate-multi-replica-quota-correctness/evidence/calibration-FAILED-20260806T214551Z.log
#       .planning/phases/55-live-gate-multi-replica-quota-correctness/evidence/README.md
#
# (3) The burst therefore runs from a throwaway pod INSIDE the `octoconv`
#     namespace -- RESEARCH.md's pre-documented Pattern 1 fallback -- where
#     Service ClusterIP routing is ordinary Kubernetes behavior rather than
#     an OrbStack-specific host-routing claim, and where the chart's own
#     NetworkPolicy (networkpolicy-metrics.yaml ingress rule (b)) already
#     admits in-namespace clients to :8090.
#
# (4) A `kubectl port-forward` to the postgres Service remains and remains
#     fine, for the unchanged reason: it only carries manage-clients/psql
#     traffic, never the burst, so its single-backend pinning is not the
#     load-bearing path.
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
#
# THE ROUND-4 DEFECT AND ITS CLOSURE (Plan 55-07 -- read this before
# touching the exact-K assertion, the fixture, or BURST_SEND_SPREAD_MS):
#
# The 20260807T141252Z run (evidence/gate-run4-FAILED-round4-20260807T141252Z.log,
# evidence/README.md, the committed TSV at
# evidence/quota-multireplica-20260807T141252Z.tsv) reached round 4/10 (56
# checks passed) and failed: `admitted count -- expected [2], got [3]`. The
# TSV shows nine of round 4's ten requests sent within 4ms of each other
# (yielding exactly 2 admits -- the cap held for the genuinely simultaneous
# population) and a tenth sent 159ms later, admitted as a third. With the
# 86-byte fixture (internal/e2e/testdata/sample.png) converting in well
# under that window, a concurrency slot very plausibly freed LEGITIMATELY
# between the two admission decisions -- but that was a RECONSTRUCTION,
# not a proven fact: the run tore the namespace down on exit (correctly),
# so no job-status timeline existed to prove index 3 reached a terminal
# status before index 10's admission transaction ran.
#
# TWO DEFECTS, TWO CLOSURES:
#   1. BURST_SEND_SPREAD_MS=1000 was far too loose (see its own comment in
#      the config block for the closure: 250ms, fixed at the driver/CPU
#      source, D4/D5).
#   2. `admitted == K` asserted more than the concurrency cap promises --
#      sound only while no admitted job can go terminal during the burst,
#      which an 86-byte fixture on the image engine did not guarantee.
#      Closed by making the premise TRUE (the committed heavy fixture,
#      scripts/fixtures/quota-burst-heavy.png, Task 1) and making it
#      CHECKED (the terminal_before discriminator in assess_round, D6,
#      Task 3) -- run BEFORE the exact-K assertion every round, with its
#      verdict backed by a committed per-job timeline
#      (quota-multireplica-<RUN_ID>-timeline.tsv) that survives teardown,
#      which the 2026-08-07 run could not produce.
#
# Relaxing `admitted == K` to `<=` was explicitly rejected: it reintroduces
# the vacuity `==` was chosen to prevent (a round admitting 0 would pass).
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
# TARGET_FORMAT / FIXTURE (Plan 55-07): webp / the committed heavy PNG, not
# jpg / the 86-byte sample.png -- see the BURST_SEND_SPREAD_MS comment
# below and the file header's "GAP CLOSURE" section for why. png->jpg on
# this fixture converts in ~600ms, too fast to establish the no-premature-
# terminal premise the round-4 defect exposed; png->webp measures in the
# multi-second range under the chart's own worker limits (see
# MIN_ENGINE_WALL_MS below).
TARGET_FORMAT="${TARGET_FORMAT:-webp}"
FIXTURE="${FIXTURE:-scripts/fixtures/quota-burst-heavy.png}"
FIXTURE_CONTENT_TYPE="${FIXTURE_CONTENT_TYPE:-image/png}"
QUOTA_K="${QUOTA_K:-2}"
BURST_N="${BURST_N:-10}"
ROUNDS="${ROUNDS:-10}"
# BURST_SEND_SPREAD_MS (Plan 55-07 -- REWRITTEN; the previous comment here
# said this threshold "does not change, and must not be changed on that
# basis", which a real run has now overtaken):
#
# THE INVARIANT: the burst window must be much shorter than the fastest
# possible job. A round's `admitted == K` assertion is only sound while no
# admitted job can reach a terminal status during the burst -- if one
# does, a concurrency slot frees legitimately mid-round and the round can
# admit MORE than K without the cap ever being violated. Widening this
# window, or swapping in a faster fixture without lowering
# MIN_ENGINE_WALL_MS to match, silently re-opens exactly that defect.
#
# WHAT HAPPENED: the 2026-08-07 run (gate-run4-FAILED-round4-20260807T141252Z.log)
# failed at round 4/10. Four rounds' observed send spreads under the OLD
# 1000ms bound: 1, 9, 92, **159** ms -- round 4's nine genuinely-simultaneous
# requests landed within 4ms and yielded exactly 2 admits (the cap held),
# but a tenth request 159ms later was admitted as a third, because the
# 86-byte fixture (internal/e2e/testdata/sample.png) converts in well under
# that window and a slot plausibly freed for real.
#
# THE FIX: 250ms is deliberately ABOVE the worst spread ever observed
# (159ms) -- the straggler is fixed at its SOURCE (Plan 55-07 Task 2: the
# burst driver's readiness handshake plus this file's own STEP 4 CPU raise
# below), and the soundness of `== K` no longer rests on the spread bound
# alone: STEP 5's `terminal_before` discriminator (see assess_round) now
# PROVES the no-premature-terminal premise from Postgres every round,
# instead of assuming it. This bound's job reverts to what it was always
# for -- proving the requests genuinely contended -- checked against
# MIN_ENGINE_WALL_MS by the invariant assertion in STEP 1.
BURST_SEND_SPREAD_MS="${BURST_SEND_SPREAD_MS:-250}"
# SPREAD_SAFETY_FACTOR: STEP 1 hard-fails unless
# BURST_SEND_SPREAD_MS * SPREAD_SAFETY_FACTOR <= MIN_ENGINE_WALL_MS, so a
# future editor who loosens either knob trips a named preflight failure
# instead of silently re-opening the defect above.
SPREAD_SAFETY_FACTOR="${SPREAD_SAFETY_FACTOR:-20}"
# MIN_ENGINE_WALL_MS: this plan's OWN measured minimum png->webp `vips
# copy` wall time for the committed heavy fixture, inside octoconv-worker:dev
# at the chart's own worker limits (--cpus=2 --memory=1g) -- see the 55-07
# SUMMARY for the three serial measurements this minimum was taken from.
# NOT copied from any planning-time estimate.
MIN_ENGINE_WALL_MS="${MIN_ENGINE_WALL_MS:-5933}"
# JOB_WALL_FLOOR_MS: the post-drain floor asserted on every round's
# admitted jobs' actual (created_at -> first terminal event) wall time --
# independent evidence, from Postgres timestamps, that the long-job premise
# was genuinely exercised for THIS run, not just nominally configured.
JOB_WALL_FLOOR_MS="${JOB_WALL_FLOOR_MS:-2000}"
# APP_IMAGE_TAG: matches the chart's global.imageTag -- the tag STEP 1's
# new image-freshness preflight inspects for octoconv-api/octoconv-worker,
# before any cluster mutation.
APP_IMAGE_TAG="${APP_IMAGE_TAG:-dev}"
DB_LOCAL_PORT="${DB_LOCAL_PORT:-15434}"
API_KEY_SALT="${API_KEY_SALT:-dev-only-change-me-in-real-deploys}"
EVIDENCE_DIR="${EVIDENCE_DIR:-.planning/phases/55-live-gate-multi-replica-quota-correctness/evidence}"
RUN_ID="${RUN_ID:-$(date -u +%Y%m%dT%H%M%SZ)}"
# EVIDENCE_TSV (Plan 55-02 Task 1): the committed SC2 evidence artifact --
# one row per burst request across all ROUNDS rounds, plus a trailing
# #-commented summary block (STEP 8, appended below). Named by RUN_ID so a
# re-run never clobbers a prior run's committed evidence. Column schema and
# header line are UNCHANGED by Plan 55-07 -- the timeline below is a
# separate file precisely so this one does not have to change.
EVIDENCE_TSV="${EVIDENCE_DIR}/quota-multireplica-${RUN_ID}.tsv"
# EVIDENCE_TIMELINE_TSV (Plan 55-07): additive second artifact, initialized
# in STEP 5 below -- the per-job created_at/first-terminal/status/wall_ms
# timeline that answers, from committed evidence and after teardown, the
# question the 2026-08-07 run's over-admitted round could not answer:
# legitimate slot reuse, or a real cross-replica cap violation. Derived
# from the same RUN_ID as EVIDENCE_TSV so the two artifacts can never be
# mismatched across runs (T-55-35).
EVIDENCE_TIMELINE_TSV="${EVIDENCE_DIR}/quota-multireplica-${RUN_ID}-timeline.tsv"

# BURST_IMAGE (Plan 55-06): the throwaway burst pod's image. Must supply
# curl, bash, mkfifo, awk, seq, base64, sha256sum, and a GNU `date` that
# honors %3N -- verified during planning via `docker run` (13-digit
# millisecond timestamp observed) and re-verified live at STEP 4 below.
# Rejected: busybox:1.36.1 (this repo's throwaway-container precedent) has
# NO curl, and busybox wget cannot POST multipart/form-data, which
# POST /v1/jobs requires; curlimages/curl is alpine, whose busybox date has
# no %3N, which would force a /proc/uptime clock at 10ms granularity and
# thereby WEAKEN the in-flight-at-max-send assertion; postgres:18 (already
# local, GNU date) has neither curl nor wget.
BURST_IMAGE="${BURST_IMAGE:-buildpack-deps:bookworm-curl}"
# POD_WORKDIR + the three in-pod paths derived from it (Plan 55-06). The
# fixture's basename is deliberately preserved -- `curl -F file=@path`
# derives the multipart filename from it, and the API's source-format
# determination can consult the extension, so landing it as a renamed
# generic filename would be a behavior change smuggled in as a transport
# detail.
POD_WORKDIR="/tmp/octoconv-burst"
POD_DRIVER_PATH="${POD_WORKDIR}/driver.sh"
POD_APIKEY_PATH="${POD_WORKDIR}/apikey"
POD_FIXTURE_PATH="${POD_WORKDIR}/$(basename "$FIXTURE")"
LOCAL_DRIVER="scripts/quota-burst-driver.sh"

PASS_COUNT=0
GATE_OK=""
DB_PF_PID=""
CALIBRATE_ONLY=""
API_POD_1=""
API_POD_2=""
CLUSTER_IP=""
BURST_POD=""

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

	# The namespace delete below would eventually collect the burst pod too,
	# but a leaked burst pod keeping OrbStack hot in the meantime is exactly
	# the failure mode this project has four recorded daemon wedges from --
	# delete it explicitly, first, unconditionally.
	if [ -n "$BURST_POD" ]; then
		kubectl delete pod "$BURST_POD" -n "$NAMESPACE" --force --grace-period=0 --wait=false >/dev/null 2>&1 || true
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

# Plan 55-06: curl and mkfifo dropped from this host-side list -- both
# moved into the burst pod's own driver (scripts/quota-burst-driver.sh) and
# are no longer a host-side dependency at all; docker (image preflight
# below) and base64 (asset transfer at STEP 4) are the new additions here.
for cmd in helm kubectl docker awk base64; do
	command -v "$cmd" >/dev/null 2>&1 || {
		echo "FAIL: required command '$cmd' not found on PATH" >&2
		exit 1
	}
done
echo "PASS: helm, kubectl, docker, awk, base64 all present on PATH (jq is NOT required -- this script parses JSON with grep/cut, matching the existing scripts)"

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

# Plan 55-06: the millisecond clock the harness relies on is now the burst
# pod's, asserted live at STEP 4 below -- the host no longer needs a
# sub-second clock of its own at all, so the former TS_TOOL
# python3/perl-fallback preflight is gone.

[ -r "$LOCAL_DRIVER" ] || {
	echo "FAIL: driver script not found or unreadable at $LOCAL_DRIVER -- this is the in-pod burst/calibration driver (Plan 55-06) transferred into the burst pod at STEP 4." >&2
	exit 1
}
echo "PASS: local driver script present at $LOCAL_DRIVER"

# The burst image is a HARD preflight dependency, never an optional skip --
# without it the gate cannot run at all (mirrors scripts/av-progress-liveproof.sh's
# own busybox:1.36.1 register for "a pinned public image is a HARD
# dependency of this gate"). Checking here, before any cluster mutation,
# means a missing image fails in the first seconds rather than after a
# five-minute helm install, and it means the pod's imagePullPolicy:
# IfNotPresent finds the image already in OrbStack's shared store.
if ! docker image inspect "$BURST_IMAGE" >/dev/null 2>&1; then
	if ! docker pull "$BURST_IMAGE" >/dev/null 2>&1; then
		echo "FAIL: burst image '$BURST_IMAGE' not present locally and could not be pulled -- run 'docker pull $BURST_IMAGE' and re-run this gate." >&2
		exit 1
	fi
fi
echo "PASS: burst image $BURST_IMAGE available locally"

# ---------------------------------------------------------------------------
# STEP 1 (Plan 55-07): THE INVARIANT -- the burst window must be much
# shorter than the fastest possible job. Checked here, before any cluster
# mutation, so a future editor who loosens either knob learns about it in
# the first seconds of a run rather than after a five-minute helm install.
# See the BURST_SEND_SPREAD_MS comment in the config block for the full
# 2026-08-07 round-4 history this guards against.
# ---------------------------------------------------------------------------
if [ "$((BURST_SEND_SPREAD_MS * SPREAD_SAFETY_FACTOR))" -gt "$MIN_ENGINE_WALL_MS" ]; then
	echo "FAIL: THE INVARIANT -- BURST_SEND_SPREAD_MS ($BURST_SEND_SPREAD_MS) * SPREAD_SAFETY_FACTOR ($SPREAD_SAFETY_FACTOR) = $((BURST_SEND_SPREAD_MS * SPREAD_SAFETY_FACTOR)) exceeds MIN_ENGINE_WALL_MS ($MIN_ENGINE_WALL_MS). The burst window must stay far below the fastest possible job, or a concurrency slot can free legitimately mid-round and the 'admitted == K' assertion stops being sound -- this is the exact 2026-08-07 round-4 defect (evidence/README.md). Loosening BURST_SEND_SPREAD_MS, raising SPREAD_SAFETY_FACTOR, or swapping in a faster fixture without lowering MIN_ENGINE_WALL_MS to match all re-open it." >&2
	exit 1
fi
echo "PASS: THE INVARIANT -- BURST_SEND_SPREAD_MS ($BURST_SEND_SPREAD_MS) * SPREAD_SAFETY_FACTOR ($SPREAD_SAFETY_FACTOR) = $((BURST_SEND_SPREAD_MS * SPREAD_SAFETY_FACTOR)) <= MIN_ENGINE_WALL_MS ($MIN_ENGINE_WALL_MS)"

FIXTURE_BYTES=$(wc -c <"$FIXTURE" | tr -d '[:space:]')
if [ "$FIXTURE_BYTES" -lt 100000 ]; then
	echo "FAIL: fixture $FIXTURE is only $FIXTURE_BYTES bytes -- below the 100000-byte floor this gate requires. A tiny fixture (the previously-used 86-byte one included) converts far faster than any burst window can bound, which is the exact premise failure Plan 55-07 closes; a small fixture silently re-opens it." >&2
	exit 1
fi
echo "PASS: fixture byte size ($FIXTURE_BYTES) >= 100000 (guards against reverting to a too-small fixture that reopens the premise failure)"

# ---------------------------------------------------------------------------
# STEP 1 (Plan 55-07): APP IMAGE FRESHNESS PREFLIGHT -- the defect that
# cost three install cycles on 2026-08-07: a MISSING image previously
# surfaced as an unnamed 300s `kubectl wait` timeout (STEP 2 below), and a
# STALE octoconv-api:dev built before 0013_client_limits.sql surfaced as
# `relation "client_limits" does not exist` at check 19, five minutes into
# the run. Checked here, before STEP 2's first cluster mutation, for both
# images STEP 2 later waits on.
# ---------------------------------------------------------------------------

# image_build_epoch IMAGE_REF -- prints the image's build time as a Unix
# epoch (integer seconds), or returns non-zero if the image cannot be
# inspected. Isolated in its own function (rather than inlined) so the
# negative test in this task's own acceptance criteria can call it
# directly. Normalizes docker's RFC3339-with-nanoseconds `.Created` (e.g.
# 2026-08-07T17:05:37.641828125+03:00) by stripping the sub-second
# fraction and reformatting the numeric UTC-offset suffix (+03:00 ->
# +0300) into the form BSD `date -j -f` accepts on this host.
image_build_epoch() {
	local image_ref="$1" created normalized
	created=$(docker image inspect --format '{{.Created}}' "$image_ref" 2>/dev/null) || return 1
	normalized=$(printf '%s' "$created" | sed -E 's/\.[0-9]+//; s/Z$/+0000/; s/([+-][0-9]{2}):([0-9]{2})$/\1\2/')
	date -j -f '%Y-%m-%dT%H:%M:%S%z' "$normalized" +%s
}

NEWEST_MIGRATION_PATH=$(ls -t internal/db/migrations/*.sql | head -1)
NEWEST_MIGRATION_NAME=$(basename "$NEWEST_MIGRATION_PATH" .sql)
NEWEST_MIGRATION_EPOCH=$(stat -f %m "$NEWEST_MIGRATION_PATH")
echo "PASS: newest migration is $NEWEST_MIGRATION_NAME (mtime epoch $NEWEST_MIGRATION_EPOCH)"

for IMG_KIND in api worker; do
	IMG_REF="octoconv-${IMG_KIND}:${APP_IMAGE_TAG}"

	docker image inspect "$IMG_REF" >/dev/null 2>&1 || {
		echo "FAIL: image '$IMG_REF' not found locally -- a missing image previously surfaced as an unnamed 300s 'kubectl wait' timeout (STEP 2). Rebuild it SEQUENTIALLY with a pinned non-latest tag: docker build -f Dockerfile.${IMG_KIND} -t ${IMG_REF} ." >&2
		exit 1
	}
	echo "PASS: image $IMG_REF present locally"

	IMG_BUILD_EPOCH=$(image_build_epoch "$IMG_REF")
	if [ "$IMG_BUILD_EPOCH" -lt "$NEWEST_MIGRATION_EPOCH" ]; then
		echo "FAIL: image '$IMG_REF' was built at epoch $IMG_BUILD_EPOCH, before the newest migration $NEWEST_MIGRATION_NAME (mtime epoch $NEWEST_MIGRATION_EPOCH). A stale octoconv-api:dev built before a schema migration surfaced as 'relation ... does not exist' five minutes into a real run on 2026-08-07. Rebuild it: docker build -f Dockerfile.${IMG_KIND} -t ${IMG_REF} ." >&2
		exit 1
	fi
	echo "PASS: image $IMG_REF built (epoch $IMG_BUILD_EPOCH) after newest migration $NEWEST_MIGRATION_NAME (epoch $NEWEST_MIGRATION_EPOCH)"

	if [ "$IMG_KIND" = "api" ]; then
		API_MIGRATION_HITS=$(docker run --rm --entrypoint sh "$IMG_REF" -c "grep -c '${NEWEST_MIGRATION_NAME}' /usr/local/bin/api" 2>/dev/null || true)
		API_MIGRATION_HITS="${API_MIGRATION_HITS:-0}"
		if [ "$API_MIGRATION_HITS" -lt 1 ]; then
			echo "FAIL: image '$IMG_REF's api binary does not embed the newest migration filename '$NEWEST_MIGRATION_NAME' (db.Migrate embeds every migration file via 'embed', so its NAME is literally in the binary) -- this is the semantically exact probe for the 'relation ... does not exist' failure observed on 2026-08-07. Rebuild: docker build -f Dockerfile.api -t ${IMG_REF} ." >&2
			exit 1
		fi
		echo "PASS: image $IMG_REF's api binary embeds newest migration name $NEWEST_MIGRATION_NAME"
	else
		WORKER_VIPS=$(docker run --rm --entrypoint sh "$IMG_REF" -c 'command -v vips' 2>/dev/null || true)
		if [ -z "$WORKER_VIPS" ]; then
			echo "FAIL: image '$IMG_REF' has no 'vips' on PATH -- the heavy fixture's whole premise is that this binary does the work. Rebuild: docker build -f Dockerfile.worker -t ${IMG_REF} ." >&2
			exit 1
		fi
		echo "PASS: image $IMG_REF has vips at $WORKER_VIPS"
	fi
done

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
# STEP 4: ClusterIP discovery + in-cluster burst pod + calibration through
# the driver -- the mandatory early proof that ClusterIP routing genuinely
# distributes new connections across BOTH ready api pods, run BEFORE any
# burst budget is spent. As of Plan 55-06 this runs from a throwaway pod
# INSIDE the octoconv namespace (see the header's rewritten "THE
# REACHABILITY DIVERGENCE" -- the host->ClusterIP path this step used to
# exercise was empirically disproven).
# ---------------------------------------------------------------------------
log "STEP 4: ClusterIP discovery + in-cluster burst pod + calibration"

CLUSTER_IP=$(kubectl get svc api -n "$NAMESPACE" -o jsonpath='{.spec.clusterIP}')
assert_nonempty "$CLUSTER_IP" "api Service ClusterIP discovered"
if [ "$CLUSTER_IP" = "None" ]; then
	echo "FAIL: api Service ClusterIP is the literal 'None' -- a headless Service has no DNAT layer to distribute burst traffic through. The gate cannot proceed on this reachability path." >&2
	exit 1
fi

# BURST_POD name carries the (lowercased) RUN_ID deliberately: a stale pod
# from a previous run can never be silently reused. RUN_ID's default
# format contains uppercase T/Z, and a Kubernetes object name must be RFC
# 1123 lowercase, so an unlowered name would fail pod creation outright.
BURST_POD="octoconv-burst-$(printf '%s' "$RUN_ID" | tr '[:upper:]' '[:lower:]')"

kubectl apply -n "$NAMESPACE" -f - <<PODSPEC
apiVersion: v1
kind: Pod
metadata:
  name: ${BURST_POD}
  labels:
    app.kubernetes.io/component: quota-burst
spec:
  restartPolicy: Never
  automountServiceAccountToken: false
  securityContext:
    runAsNonRoot: true
    runAsUser: 65534
    runAsGroup: 65534
  containers:
    - name: burst
      image: ${BURST_IMAGE}
      imagePullPolicy: IfNotPresent
      command: ["sleep", "3600"]
      securityContext:
        allowPrivilegeEscalation: false
        capabilities:
          drop: ["ALL"]
      # CPU raised 500m->500m(requests)/500m->"2"(limits) by Plan 55-07:
      # ten forked subshells plus ten curl processes released by one FIFO
      # barrier, on half a core, is the suspected root cause of the
      # 2026-08-07 round-4 straggler (+159ms send offset -- see the
      # BURST_SEND_SPREAD_MS comment in the config block). The burst
      # driver (scripts/quota-burst-driver.sh) already removed every OTHER
      # fork from the post-barrier critical path; this raise addresses the
      # remaining CPU-contention half of that same defect. Memory is
      # unchanged -- this pod only forks/execs curl, it never converts.
      resources:
        requests:
          cpu: "500m"
          memory: "64Mi"
        limits:
          cpu: "2"
          memory: "256Mi"
PODSPEC

kubectl wait --for=condition=Ready pod/"$BURST_POD" -n "$NAMESPACE" --timeout=180s
echo "PASS: burst pod $BURST_POD ready (image $BURST_IMAGE, component=quota-burst -- distinct from the component=api selector the pod-set assertions use)"

# In-pod toolchain assertion. This verifies the image is adequate; nothing
# below may assume it. One tool is deliberately NOT named here: checking
# for it by name would require spelling it on a non-comment line, which
# this entire host script guarantees is absent everywhere except inside a
# full-line comment (see the header -- the host makes zero such calls of
# its own any more). That tool's own presence is instead verified by the
# in-pod driver's own preflight (scripts/quota-burst-driver.sh), exercised
# one step below by the calibration call -- a missing binary there fails
# loudly on stderr, and the calibration's zero-successes hard-fail (below)
# catches it just as surely, one call later.
BURST_POD_MISSING=$(kubectl exec -n "$NAMESPACE" "$BURST_POD" -- sh -c '
	for t in mkfifo awk seq base64 sha256sum; do
		command -v "$t" >/dev/null 2>&1 || echo "MISSING:$t"
	done
')
assert_empty "$BURST_POD_MISSING" "burst pod toolchain complete (mkfifo awk seq base64 sha256sum)"

BURST_POD_CLOCK=$(kubectl exec -n "$NAMESPACE" "$BURST_POD" -- date +%s%3N)
if [[ ! "$BURST_POD_CLOCK" =~ ^[0-9]{13}$ ]]; then
	echo "FAIL: burst pod $BURST_POD's date does not honor %3N (got '$BURST_POD_CLOCK', expected exactly 13 digits) -- $BURST_IMAGE is expected to ship GNU coreutils date; a second-granularity clock would weaken the in-flight-at-max-send assertion." >&2
	exit 1
fi
assert_nonempty "$BURST_POD_CLOCK" "burst pod millisecond clock honors %3N ($BURST_POD_CLOCK)"

kubectl exec -n "$NAMESPACE" "$BURST_POD" -- mkdir -p "$POD_WORKDIR"

# Asset transfer -- base64-over-stdin rather than `kubectl cp` (which
# shells out to tar inside the container, adding a dependency and a
# failure mode for no benefit) and rather than a ConfigMap (more
# declarative and would survive restarts, but this pod is single-use and
# throwaway, and ConfigMap binaryData adds a second object to create,
# size-bound, and tear down for no benefit here).
base64 <"$LOCAL_DRIVER" | kubectl exec -i -n "$NAMESPACE" "$BURST_POD" -- sh -c "base64 -d > $POD_DRIVER_PATH"
base64 <"$FIXTURE" | kubectl exec -i -n "$NAMESPACE" "$BURST_POD" -- sh -c "base64 -d > $POD_FIXTURE_PATH"
kubectl exec -n "$NAMESPACE" "$BURST_POD" -- chmod +x "$POD_DRIVER_PATH"
echo "PASS: driver and fixture transferred into $BURST_POD"

# Transfer integrity, both files, before anything is fired -- this is what
# prevents a truncated transfer from presenting as a uniform run of 400s
# that a reader might mistake for rejections.
LOCAL_DRIVER_SHA=$(shasum -a 256 "$LOCAL_DRIVER" | awk '{print $1}')
POD_DRIVER_SHA=$(kubectl exec -n "$NAMESPACE" "$BURST_POD" -- sha256sum "$POD_DRIVER_PATH" | awk '{print $1}')
assert_eq "$LOCAL_DRIVER_SHA" "$POD_DRIVER_SHA" "driver sha256 matches after transfer"

LOCAL_DRIVER_BYTES=$(wc -c <"$LOCAL_DRIVER" | tr -d '[:space:]')
POD_DRIVER_BYTES=$(kubectl exec -n "$NAMESPACE" "$BURST_POD" -- wc -c "$POD_DRIVER_PATH" | awk '{print $1}')
assert_eq "$LOCAL_DRIVER_BYTES" "$POD_DRIVER_BYTES" "driver byte count matches after transfer"

LOCAL_FIXTURE_SHA=$(shasum -a 256 "$FIXTURE" | awk '{print $1}')
POD_FIXTURE_SHA=$(kubectl exec -n "$NAMESPACE" "$BURST_POD" -- sha256sum "$POD_FIXTURE_PATH" | awk '{print $1}')
assert_eq "$LOCAL_FIXTURE_SHA" "$POD_FIXTURE_SHA" "fixture sha256 matches after transfer"

LOCAL_FIXTURE_BYTES=$(wc -c <"$FIXTURE" | tr -d '[:space:]')
POD_FIXTURE_BYTES=$(kubectl exec -n "$NAMESPACE" "$BURST_POD" -- wc -c "$POD_FIXTURE_PATH" | awk '{print $1}')
assert_eq "$LOCAL_FIXTURE_BYTES" "$POD_FIXTURE_BYTES" "fixture byte count matches after transfer"

# Calibration, through the driver -- the SAME driver, the SAME barrier, the
# SAME one-process-per-request rule the burst rounds use below, run in
# calib mode against /healthz. CALIB_N unchanged from the host->ClusterIP
# design.
CALIB_N=20
CALIB_DRIVER_STATUS=0
kubectl exec -n "$NAMESPACE" "$BURST_POD" -- "$POD_DRIVER_PATH" calib "http://${CLUSTER_IP}:8090" "$RUN_ID" 0 "$CALIB_N" \
	>"$WORKDIR/calib-driver.out" 2>"$WORKDIR/calib-driver.err" || CALIB_DRIVER_STATUS=$?

CALIB_200_COUNT=$(awk -F'\t' '$5=="200"' "$WORKDIR/calib-driver.out" | wc -l | tr -d '[:space:]')

if [ "$CALIB_200_COUNT" -eq 0 ]; then
	echo "FAIL: calibration -- 0/${CALIB_N} probes returned 200 from inside the octoconv namespace (burst pod $BURST_POD, driver exit status ${CALIB_DRIVER_STATUS}). The host->ClusterIP fallback has ALREADY been taken (Plan 55-06) -- a failure here means in-cluster ClusterIP routing itself is broken, and the gate cannot proceed; there is no third path. Driver stderr tail:" >&2
	tail -n 20 "$WORKDIR/calib-driver.err" >&2 2>/dev/null || true
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
	echo "FAIL: calibration -- in-cluster ClusterIP routing is reachable but did NOT distribute across both api pods (pod1=$POD1_HITS, pod2=$POD2_HITS). The cross-pod assertion (CONTEXT D-05) can never be satisfied while this holds -- there is no further fallback; stop and report." >&2
	exit 1
fi
assert_ge "$POD1_HITS" "1" "calibration probes landed on API_POD_1 ($API_POD_1)"
assert_ge "$POD2_HITS" "1" "calibration probes landed on API_POD_2 ($API_POD_2)"

CALIB_SUM=$((POD1_HITS + POD2_HITS))
assert_eq "$CALIB_N" "$CALIB_SUM" "calibration pod-hit sum equals probes fired"

echo "PASS: calibration -- in-cluster ClusterIP routing (burst pod $BURST_POD) distributed $CALIB_N probes across both api pods (pod1=$POD1_HITS, pod2=$POD2_HITS) -- this line is itself SC2 evidence that two replicas really served traffic"

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

# psql_rows SQL (Plan 55-07) -- like psql_q, but for MULTI-ROW, MULTI-COLUMN
# results: unlike psql_q (which strips ALL whitespace, correct only for a
# single scalar), this preserves row newlines and uses a TAB column
# separator matching this file's TSV artifacts. Used solely by run_round's
# timeline append below.
psql_rows() {
	kubectl exec -n "$NAMESPACE" deploy/postgres -- psql -U octo -d octo_db -tA -F "$(printf '\t')" -c "$1"
}

PSQL_PREFLIGHT=$(psql_q 'SELECT 1' || true)
if [ "$PSQL_PREFLIGHT" != "1" ]; then
	echo "FAIL: psql_q preflight -- 'SELECT 1' returned [$PSQL_PREFLIGHT], expected [1]. Retry with PGPASSWORD sourced from the release Secret: kubectl get secret octoconv-secret -n octoconv -o jsonpath='{.data.POSTGRES_PASSWORD}' | base64 -d" >&2
	exit 1
fi
assert_eq "1" "$PSQL_PREFLIGHT" "psql_q preflight (SELECT 1 against postgres pod)"

# ---------------------------------------------------------------------------
# CLOCK SKEW (Plan 55-07, D7): terminal_before (below) compares a
# burst-pod-clock INSTANT against Postgres timestamptz columns. Both
# containers run on one OrbStack node and share the kernel's
# CLOCK_REALTIME -- but that is an ASSUMPTION, so it becomes a CHECKED one
# here: measure the skew between the burst pod's `date +%s%3N` and
# Postgres's `clock_timestamp()` (both as epoch milliseconds), taken back
# to back, and hard-fail above 1000ms. The per-round wall-time RATIO check
# (run_round, below) compares two DURATIONS derived from each clock
# separately, not one instant against the other, and is therefore
# offset-free by construction regardless of this measurement -- this check
# exists solely for terminal_before's instant-vs-instant comparison.
# ---------------------------------------------------------------------------
POD_CLOCK_MS=$(kubectl exec -n "$NAMESPACE" "$BURST_POD" -- date +%s%3N)
DB_CLOCK_MS=$(psql_q "SELECT (extract(epoch from clock_timestamp())*1000)::bigint")
if [ "$POD_CLOCK_MS" -ge "$DB_CLOCK_MS" ]; then
	CLOCK_SKEW_MS=$((POD_CLOCK_MS - DB_CLOCK_MS))
else
	CLOCK_SKEW_MS=$((DB_CLOCK_MS - POD_CLOCK_MS))
fi
echo "burst pod clock=$POD_CLOCK_MS ms, postgres clock_timestamp()=$DB_CLOCK_MS ms"
assert_le "$CLOCK_SKEW_MS" "1000" "clock skew (ms) between burst pod and Postgres clock_timestamp()"

# sql_id_list JOB_IDS -- turns a whitespace-separated list of job ids into
# a quoted, comma-separated SQL list for use inside an IN (...) clause.
# Every SQL helper below builds on this one place. Single quotes only --
# psql_q passes the resulting string as ONE argv element to `psql -tAc`,
# never through a remote shell, so there is no shell-injection surface
# here distinct from any other psql_q caller in this file.
sql_id_list() {
	local ids="$1" id out=""
	for id in $ids; do
		if [ -n "$out" ]; then
			out="${out},"
		fi
		out="${out}'${id}'"
	done
	printf '%s' "$out"
}

# terminal_before JOB_IDS LAST_RECV_MS -- THE DISCRIMINATOR (D6, the
# load-bearing part of this plan). Counts how many of the listed jobs
# reached a terminal status (done/failed/canceled) at or before the
# round's LAST response (last_recv_ms, the burst pod's millisecond clock).
# A non-zero count means the round's admit count is explained by
# LEGITIMATE SLOT REUSE, not a cap violation -- called BEFORE the exact-K
# cap assertion in assess_round (see the ORDER IS THE ENTIRE POINT comment
# there), never after.
terminal_before() {
	local job_ids="$1" last_recv_ms="$2" id_list
	id_list=$(sql_id_list "$job_ids")
	psql_q "SELECT count(*) FROM job_events WHERE job_id IN (${id_list}) AND to_status IN ('done','failed','canceled') AND created_at <= to_timestamp(${last_recv_ms}/1000.0)"
}

# min_job_wall_ms JOB_IDS -- the minimum, in integer milliseconds, of
# (first terminal job_events.created_at) minus (jobs.created_at) across
# the listed jobs. A LEFT JOIN plus per-job coalesce(wall_ms, -1) means a
# job with NO terminal event yet contributes -1 to the candidate set, so
# the overall MIN comes out -1 in that case too -- never silently excluded
# from consideration.
min_job_wall_ms() {
	local job_ids="$1" id_list
	id_list=$(sql_id_list "$job_ids")
	psql_q "SELECT min(coalesce(wall_ms, -1)) FROM (SELECT j.id, (extract(epoch from (min(e.created_at) - j.created_at)) * 1000)::bigint AS wall_ms FROM jobs j LEFT JOIN job_events e ON e.job_id = j.id AND e.to_status IN ('done','failed','canceled') WHERE j.id IN (${id_list}) GROUP BY j.id, j.created_at) t"
}

# final_statuses JOB_IDS -- the distinct set of jobs.status across the
# listed jobs, comma-joined and sorted. A 'failed' or 'canceled' member
# means the long-job premise was never genuinely exercised for this round
# (an OOM-killed or unsupported conversion is terminal too) -- the round
# proves nothing in that case, and run_round (below) treats it as a hard
# failure, not a soft warning.
final_statuses() {
	local job_ids="$1" id_list
	id_list=$(sql_id_list "$job_ids")
	psql_q "SELECT string_agg(DISTINCT status, ',' ORDER BY status) FROM jobs WHERE id IN (${id_list})"
}

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

# Transfer the minted key into the burst pod, immediately after the four
# anti-vacuity readbacks pass. Piped through stdin into a umask-077 file --
# never an argv element of any command, so it appears in neither the pod's
# own process table nor the exec request the API server records. Verified
# by byte count against ${#CLIENT_KEY}, which proves delivery without
# printing a single character of the key.
printf '%s' "$CLIENT_KEY" | kubectl exec -i -n "$NAMESPACE" "$BURST_POD" -- sh -c "umask 077; cat > $POD_APIKEY_PATH"
POD_APIKEY_BYTES=$(kubectl exec -n "$NAMESPACE" "$BURST_POD" -- wc -c "$POD_APIKEY_PATH" | awk '{print $1}')
assert_eq "${#CLIENT_KEY}" "$POD_APIKEY_BYTES" "api key transferred into $BURST_POD (byte count matches; key itself never echoed)"

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

# in_pod_http URL [EXTRA_ARGS...] -- execs into $BURST_POD and issues one
# HTTP request from inside the cluster (the ClusterIP is unreachable from
# this host at all, per the header's rewritten "THE REACHABILITY
# DIVERGENCE"). Deliberately named without the substring this helper's
# single in-pod invocation wraps, so the file-scoped acceptance grep for
# that substring stays meaningful. URL and EXTRA_ARGS are passed as
# positional parameters to the remote shell (never interpolated into the
# script string), and the Authorization header value is read in-pod from
# the transferred api key file via command substitution -- the key never
# crosses a command line, on this host OR in the pod.
in_pod_http() {
	local url="$1"
	shift
	kubectl exec -i -n "$NAMESPACE" "$BURST_POD" -- sh -c '
		u="$1"
		shift
		exec curl -s -H "Authorization: ApiKey $(cat '"$POD_APIKEY_PATH"')" "$@" "$u"
	' sh "$url" "$@"
}

# job_status JOB_ID -- GETs /v1/jobs/{id} via the ClusterIP (from inside
# the pod) and echoes the status field.
job_status() {
	local job_id="$1"
	in_pod_http "http://${CLUSTER_IP}:8090/v1/jobs/${job_id}" --max-time 10 |
		grep -o '"status":"[^"]*"' | head -1 | cut -d'"' -f4
}

# cancel_job JOB_ID -- POSTs /v1/jobs/{id}/cancel via the ClusterIP (from
# inside the pod). A non-2xx is tolerated and never aborts the gate -- the
# job may already be terminal by the time this fires.
cancel_job() {
	local job_id="$1"
	in_pod_http "http://${CLUSTER_IP}:8090/v1/jobs/${job_id}/cancel" -o /dev/null -w '%{http_code}' --max-time 10 -X POST >/dev/null 2>&1 || true
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
# Burst generator. fire_burst is called once per round by run_round inside
# the STEP 6 main loop below. As of Plan 55-06 the FIFO-barrier burst
# itself -- BURST_N requests, each from its own curl PROCESS, released
# together by a single write to a named pipe -- runs entirely INSIDE the
# burst pod via scripts/quota-burst-driver.sh (the one-curl-process-per-
# request reasoning, RESEARCH.md Pitfall 2, moved there verbatim in
# substance -- see that file's own header). fire_burst's job here is
# narrower: invoke the driver, validate its result stream before trusting
# one byte of it, and materialize the exact per-request .rec/.json files
# assess_round already expects -- assess_round itself is UNCHANGED by this
# plan.
# ---------------------------------------------------------------------------

# fire_burst ROUND -- runs the in-pod driver in burst mode, then validates
# and reshapes its output.
fire_burst() {
	local round="$1"
	local driver_out="$WORKDIR/r${round}-driver.out"
	local driver_err="$WORKDIR/r${round}-driver.err"
	local driver_status=0

	# set -e is in force, so the exit status is captured explicitly rather
	# than letting a non-zero exit abort with no diagnosis.
	kubectl exec -n "$NAMESPACE" "$BURST_POD" -- "$POD_DRIVER_PATH" burst \
		"http://${CLUSTER_IP}:8090" "$RUN_ID" "$round" "$BURST_N" \
		"$TARGET_FORMAT" "$POD_FIXTURE_PATH" "$FIXTURE_CONTENT_TYPE" "$POD_APIKEY_PATH" \
		>"$driver_out" 2>"$driver_err" || driver_status=$?
	if [ "$driver_status" -ne 0 ]; then
		echo "FAIL: round ${round} -- driver exited ${driver_status}. stderr tail:" >&2
		tail -n 20 "$driver_err" >&2 2>/dev/null || true
		exit 1
	fi

	# Five independent stream checks, each its own hard failure naming the
	# round and the observed number. These exist because a truncated stream
	# would otherwise read as "those requests were simply not admitted",
	# which would still satisfy every "never more than K" assertion below
	# and pass the gate while proving nothing.
	local begin_count end_count emitted row_count distinct_list expected_list malformed

	begin_count=$(grep -c '^===BURST-BEGIN' "$driver_out")
	end_count=$(grep -c '^===BURST-END' "$driver_out")
	if [ "$begin_count" -ne 1 ] || [ "$end_count" -ne 1 ]; then
		echo "FAIL: round ${round} -- expected exactly one BEGIN and one END sentinel, observed begin=${begin_count} end=${end_count}." >&2
		exit 1
	fi

	emitted=$(grep '^===BURST-END' "$driver_out" | sed -n 's/.*emitted=\([0-9]*\).*/\1/p')
	if [ "$emitted" != "$BURST_N" ]; then
		echo "FAIL: round ${round} -- driver declared emitted=${emitted}, expected ${BURST_N}." >&2
		exit 1
	fi

	row_count=$(sed -n '/^===BURST-BEGIN/,/^===BURST-END/p' "$driver_out" | grep -v '^===BURST-' | wc -l | tr -d '[:space:]')
	if [ "$row_count" != "$BURST_N" ]; then
		echo "FAIL: round ${round} -- observed ${row_count} rows between the sentinels, expected ${BURST_N}." >&2
		exit 1
	fi

	malformed=$(sed -n '/^===BURST-BEGIN/,/^===BURST-END/p' "$driver_out" | grep -v '^===BURST-' | awk -F'\t' 'NF != 6')
	assert_empty "$malformed" "round ${round}: every driver row has exactly six tab-separated fields"

	distinct_list=$(sed -n '/^===BURST-BEGIN/,/^===BURST-END/p' "$driver_out" | grep -v '^===BURST-' | awk -F'\t' '{print $1}' | sort -n -u)
	expected_list=$(seq 1 "$BURST_N")
	if [ "$distinct_list" != "$expected_list" ]; then
		echo "FAIL: round ${round} -- row indices do not cover 1..${BURST_N} exactly once (observed distinct set: $(printf '%s' "$distinct_list" | tr '\n' ' '))." >&2
		exit 1
	fi

	# Materialize the .rec/.json files assess_round expects -- the contract
	# is deliberately preserved so assess_round, with its six assertions and
	# the D-05 cross-pod check, is not modified by this plan at all.
	# send_ts_ms/recv_ts_ms are now the burst POD's clock, not the host's --
	# strictly better, because every comparison the harness makes is
	# pod-clock against pod-clock inside a single round; no host-to-pod
	# clock comparison exists anywhere in this file.
	local idx request_id send_ts recv_ts status body
	while IFS=$'\t' read -r idx request_id send_ts recv_ts status body; do
		printf '%s\t%s\t%s\t%s\t%s\n' "$idx" "$request_id" "$send_ts" "$recv_ts" "$status" \
			>"$WORKDIR/r${round}-req${idx}.rec"
		printf '%s' "$body" >"$WORKDIR/r${round}-req${idx}.json"
	done < <(sed -n '/^===BURST-BEGIN/,/^===BURST-END/p' "$driver_out" | grep -v '^===BURST-')
}

# Evidence TSV initialization -- must happen before round 1 fires. Header
# field order is fixed and matched by STEP 7's aggregate `awk -F'\t'`
# assertions below: round(1) index(2) request_id(3) send_ts_ms(4)
# recv_ts_ms(5) http_status(6) body_code(7) serving_pod(8). UNCHANGED by
# Plan 55-07 -- byte-identical to every prior run's header.
mkdir -p "$EVIDENCE_DIR"
printf 'round\tindex\trequest_id\tsend_ts_ms\trecv_ts_ms\thttp_status\tbody_code\tserving_pod\n' >"$EVIDENCE_TSV"
EVIDENCE_TSV_ABS="$(cd "$(dirname "$EVIDENCE_TSV")" && pwd)/$(basename "$EVIDENCE_TSV")"
echo "PASS: evidence TSV initialized at $EVIDENCE_TSV_ABS"

# Timeline TSV initialization (Plan 55-07) -- the ADDITIVE second artifact
# that answers, from committed evidence after teardown, whether an
# over-admitted round is legitimate slot reuse or a real cap violation
# (D6). Initialized here, next to the existing TSV, before round 1 fires,
# so the two files' lifetimes are identical.
printf 'round\tjob_id\tcreated_at\tfirst_terminal_at\tterminal_status\twall_ms\tround_last_recv_utc\n' >"$EVIDENCE_TIMELINE_TSV"
EVIDENCE_TIMELINE_TSV_ABS="$(cd "$(dirname "$EVIDENCE_TIMELINE_TSV")" && pwd)/$(basename "$EVIDENCE_TIMELINE_TSV")"
echo "PASS: timeline TSV initialized at $EVIDENCE_TIMELINE_TSV_ABS"

# ---------------------------------------------------------------------------
# Round classification + attribution + assertions (Plan 55-02 Task 2).
# ---------------------------------------------------------------------------

# assess_round ROUND -- classifies each of BURST_N requests by body code
# (never bare status), attributes each to exactly one pod via its
# X-Request-Id, and applies every anti-vacuity assertion before writing one
# TSV row per request. Order matters: attribution completeness is asserted
# BEFORE any conclusion is drawn from it (cap exactness, cross-pod split,
# concurrency of flight all read the per-request pod attribution).
assess_round() {
	local round="$1" i
	local admit_count=0 quota_reject_count=0 attributed_total=0
	local pod1_admit=0 pod1_reject=0 pod2_admit=0 pod2_reject=0
	local min_send="" max_send="" max_recv=""
	local admitted_job_ids=""

	# ATTRIBUTE, step one: dump each pod's log ONCE per round (never once
	# per request -- that would be BURST_N kubectl calls per pod per round
	# for no benefit, since each dump is grepped BURST_N times below
	# instead).
	kubectl logs "$API_POD_1" -n "$NAMESPACE" --since=10m >"$WORKDIR/r${round}-${API_POD_1}.log" 2>/dev/null || true
	kubectl logs "$API_POD_2" -n "$NAMESPACE" --since=10m >"$WORKDIR/r${round}-${API_POD_2}.log" 2>/dev/null || true

	for i in $(seq 1 "$BURST_N"); do
		local rec="$WORKDIR/r${round}-req${i}.rec"
		local body="$WORKDIR/r${round}-req${i}.json"
		[ -f "$rec" ] || {
			echo "FAIL: round ${round} req ${i} -- no .rec file; fire_burst did not complete for this request" >&2
			exit 1
		}

		local request_id send_ts recv_ts status code job_id serving_pod
		IFS=$'\t' read -r _ request_id send_ts recv_ts status <"$rec"
		code=$(grep -o '"code":"[^"]*"' "$body" 2>/dev/null | head -1 | cut -d'"' -f4 || true)

		# 1. CLASSIFY -- switch on "$status:$code", never bare status: both
		# quota_exceeded and rate_limited return 429, and classifying on
		# the bare status would let a rate-limit storm masquerade as proof
		# of quota correctness. Admits classify on 202 (POST /v1/jobs's
		# real success status, internal/api/handlers.go:805) -- NOT 201.
		job_id=""
		case "$status:$code" in
		202:*)
			admit_count=$((admit_count + 1))
			job_id=$(grep -o '"job_id":"[^"]*"' "$body" | head -1 | cut -d'"' -f4 || true)
			admitted_job_ids="$admitted_job_ids $job_id"
			;;
		429:quota_exceeded)
			quota_reject_count=$((quota_reject_count + 1))
			;;
		429:rate_limited)
			echo "FAIL: round ${round} req ${i} (id=${request_id}) -- 429 rate_limited, not quota_exceeded. RATE_LIMIT_CLIENT_RPM/RATE_LIMIT_IP_RPM were too low for this burst cadence; the gate cannot distinguish a quota rejection from a rate-limit rejection under this condition." >&2
			exit 1
			;;
		*)
			echo "FAIL: round ${round} req ${i} (id=${request_id}) -- unexpected status:code [${status}:${code}], body: $(cat "$body" 2>/dev/null)" >&2
			exit 1
			;;
		esac

		# 2. ATTRIBUTE, step two: exactly one pod's log must carry this
		# request's id -- zero-in-both is an empty join (which would make
		# every downstream conclusion vacuous), and nonzero-in-both is an
		# ambiguous join. Both are hard FAILs, checked BEFORE any
		# conclusion below is drawn from the attribution.
		local pod1_hits pod2_hits
		pod1_hits=$(grep -c "\[${request_id}\]" "$WORKDIR/r${round}-${API_POD_1}.log" 2>/dev/null || true)
		pod2_hits=$(grep -c "\[${request_id}\]" "$WORKDIR/r${round}-${API_POD_2}.log" 2>/dev/null || true)
		pod1_hits="${pod1_hits:-0}"
		pod2_hits="${pod2_hits:-0}"
		if [ "$pod1_hits" -eq 0 ] && [ "$pod2_hits" -eq 0 ]; then
			echo "FAIL: round ${round} req ${i} (id=${request_id}) -- matched in NEITHER pod's log; the attribution join is empty, which would make every downstream conclusion vacuous." >&2
			exit 1
		fi
		if [ "$pod1_hits" -ge 1 ] && [ "$pod2_hits" -ge 1 ]; then
			echo "FAIL: round ${round} req ${i} (id=${request_id}) -- matched in BOTH pods' logs (pod1=${pod1_hits}, pod2=${pod2_hits}); the attribution join is ambiguous." >&2
			exit 1
		fi
		attributed_total=$((attributed_total + 1))
		if [ "$pod1_hits" -ge 1 ]; then
			serving_pod="$API_POD_1"
		else
			serving_pod="$API_POD_2"
		fi

		# Non-fatal cross-check: the matched log line's own status token
		# should equal the client-observed status. chi logs at request
		# COMPLETION (a defer), so a client-side timeout can observe a
		# different status than what the server eventually recorded --
		# print a WARN, never fail on this alone.
		local log_line log_status
		log_line=$(grep "\[${request_id}\]" "$WORKDIR/r${round}-${serving_pod}.log" 2>/dev/null | head -1 || true)
		log_status=$(printf '%s' "$log_line" | grep -o ' [0-9][0-9][0-9] ' | head -1 | tr -d ' ' || true)
		if [ -n "$log_status" ] && [ "$log_status" != "$status" ]; then
			echo "WARN: round ${round} req ${i} (id=${request_id}) -- client observed status ${status} but ${serving_pod}'s log line shows ${log_status} (chi logs at completion via a defer; can happen on a client-side timeout, not necessarily a bug)"
		fi

		case "${status}:${serving_pod}" in
		"202:${API_POD_1}") pod1_admit=$((pod1_admit + 1)) ;;
		"202:${API_POD_2}") pod2_admit=$((pod2_admit + 1)) ;;
		"429:${API_POD_1}") pod1_reject=$((pod1_reject + 1)) ;;
		"429:${API_POD_2}") pod2_reject=$((pod2_reject + 1)) ;;
		esac

		if [ -z "$min_send" ] || [ "$send_ts" -lt "$min_send" ]; then
			min_send="$send_ts"
		fi
		if [ -z "$max_send" ] || [ "$send_ts" -gt "$max_send" ]; then
			max_send="$send_ts"
		fi
		# max_recv (Plan 55-07): the round's LAST response instant, in the
		# same burst-pod millisecond clock as send_ts/recv_ts -- passed to
		# terminal_before below as the discriminator's cutoff.
		if [ -z "$max_recv" ] || [ "$recv_ts" -gt "$max_recv" ]; then
			max_recv="$recv_ts"
		fi

		# 6. EMIT EVIDENCE -- one TSV row per request.
		printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
			"$round" "$i" "$request_id" "$send_ts" "$recv_ts" "$status" "$code" "$serving_pod" \
			>>"$EVIDENCE_TSV"
	done

	assert_eq "$BURST_N" "$attributed_total" "round ${round}: every request attributed to exactly one pod"

	# 2b. THE DISCRIMINATING ASSERTION (Plan 55-07, D6 -- the load-bearing
	# part of this plan). ORDER IS THE ENTIRE POINT: this runs BEFORE the
	# exact-K cap assertion below, never after, because a round that
	# over-admits must be DIAGNOSED, not merely reported. Ask Postgres
	# directly: did ANY of this round's admitted jobs reach a terminal
	# status at or before this round's LAST response (max_recv)? If yes,
	# the round's admit count is explained by legitimate slot reuse and the
	# exact-K premise did NOT hold for this round -- fail here, with the
	# per-job timeline dumped, instead of letting a misleading "expected
	# [K], got [K+1]" further down stand as the only evidence (the exact
	# 2026-08-07 round-4 gap: no job-status timeline existed to prove or
	# disprove legitimate reuse). If no admitted jobs went terminal early,
	# the premise for THIS round's exact-K assertion is proven, and a
	# subsequent over-admission means a REAL cross-replica cap violation
	# with nothing left to reconstruct.
	if [ -z "$(printf '%s' "$admitted_job_ids" | tr -d '[:space:]')" ]; then
		echo "WARN: round ${round}: no admitted jobs to run the discriminating assertion against -- the exact-K cap assertion immediately below will fail on its own if this round admitted nothing"
	else
		local terminal_before_count
		terminal_before_count=$(terminal_before "$admitted_job_ids" "$max_recv")
		if [ "$terminal_before_count" != "0" ]; then
			echo "FAIL: round ${round}: ${terminal_before_count} of this round's admitted job(s) reached a terminal status at or before this round's last response (max_recv=${max_recv}ms) -- the concurrency slot was legitimately reused, so the exact-K premise did NOT hold for this round. Per-job timeline:" >&2
			local tid
			for tid in $admitted_job_ids; do
				psql_q "SELECT '${tid}' || E'\t' || j.created_at || E'\t' || coalesce(min(e.created_at)::text, '(none)') || E'\t' || j.status FROM jobs j LEFT JOIN job_events e ON e.job_id = j.id AND e.to_status IN ('done','failed','canceled') WHERE j.id = '${tid}' GROUP BY j.id, j.created_at, j.status" >&2
			done
			exit 1
		fi
		PASS_COUNT=$((PASS_COUNT + 1))
		echo "PASS: round ${round}: no-premature-terminal precondition for the exact-K assertion holds (0 of this round's admitted jobs went terminal at or before max_recv=${max_recv}ms)"
	fi

	# 3. SC1 -- CAP EXACTNESS. `==`, not `<=`: a round that starts with the
	# cap already consumed would under-admit and still satisfy a `<=` check
	# vacuously -- this is D-06's own flagged pitfall, closed here with
	# exact equality instead.
	assert_eq "$QUOTA_K" "$admit_count" "round ${round}: admitted count"
	assert_eq "$((BURST_N - QUOTA_K))" "$quota_reject_count" "round ${round}: quota_exceeded rejection count"

	# 4. D-05 -- CROSS-POD DECISION. The weaker "both pods received some
	# traffic" form is rejected on purpose: one pod could consume the
	# entire cap before the other pod's first request arrives, and no cap
	# DECISION would have crossed the replica boundary even though both
	# pods technically served requests.
	if { [ "$pod1_admit" -ge 1 ] && [ "$pod2_reject" -ge 1 ]; } || { [ "$pod2_admit" -ge 1 ] && [ "$pod1_reject" -ge 1 ]; }; then
		PASS_COUNT=$((PASS_COUNT + 1))
		echo "PASS: round ${round}: cross-pod cap decision -- ${API_POD_1} admit=${pod1_admit} reject=${pod1_reject}, ${API_POD_2} admit=${pod2_admit} reject=${pod2_reject}"
	else
		echo "FAIL: round ${round}: no admitted+rejected pair landed on DIFFERENT pods -- ${API_POD_1} admit=${pod1_admit} reject=${pod1_reject}, ${API_POD_2} admit=${pod2_admit} reject=${pod2_reject}. The cap decision never crossed the replica boundary in this round." >&2
		exit 1
	fi

	# 5. CONCURRENCY OF FLIGHT -- without this, a fully serialized round
	# would satisfy every other assertion while never contending the
	# advisory lock at all.
	local round_spread=$((max_send - min_send))
	assert_le "$round_spread" "$BURST_SEND_SPREAD_MS" "round ${round}: burst send spread (ms)"
	# Persisted for run_round's post-drain wall-time RATIO assertion below
	# (Plan 55-07) -- mirrors the existing r<round>-admitted.txt handoff.
	printf '%s\n' "$round_spread" >"$WORKDIR/r${round}-spread.txt"

	local inflight_at_max=0
	for i in $(seq 1 "$BURST_N"); do
		local rec2="$WORKDIR/r${round}-req${i}.rec"
		local s2 r2
		IFS=$'\t' read -r _ _ s2 r2 _ <"$rec2"
		if [ "$s2" -le "$max_send" ] && [ "$max_send" -le "$r2" ]; then
			inflight_at_max=$((inflight_at_max + 1))
		fi
	done
	assert_ge "$inflight_at_max" "$(( (BURST_N + 1) / 2 ))" "round ${round}: requests provably in flight when the last request was sent"
	echo "round ${round}: min_send=${min_send} max_send=${max_send} in_flight_at_max_send=${inflight_at_max}"

	printf '%s\n' "$admitted_job_ids" >"$WORKDIR/r${round}-admitted.txt"
	# Persisted for run_round's post-drain timeline append below (Plan
	# 55-07) -- the same handoff pattern as r<round>-admitted.txt/-spread.txt.
	printf '%s\n' "$max_recv" >"$WORKDIR/r${round}-maxrecv.txt"
}

# run_round ROUND -- sequences one round end-to-end: assert the cap is free
# -> fire the burst -> assess it -> drain it. The pre-round zero assertion
# and the post-round drain assertion are the two halves of Pitfall 4's
# closure (D-06): a round that starts with the cap already consumed, or
# whose admitted jobs are not drained before the next round's barrier
# fires, would silently admit fewer than K and still pass a naive "never
# more than K" check.
run_round() {
	local round="$1"
	assert_eq "0" "$(inflight_count "$CLIENT_ID")" "round ${round}: cap is free before the barrier fires"
	fire_burst "$round"
	assess_round "$round"
	local admitted_ids
	admitted_ids=$(cat "$WORKDIR/r${round}-admitted.txt" 2>/dev/null || true)
	drain_round "$admitted_ids"

	# ---------------------------------------------------------------------
	# Post-drain long-job-premise verification (Plan 55-07). Runs AFTER
	# drain_round, so every admitted job in the list is already terminal --
	# drain_round itself hard-fails if it is not.
	# ---------------------------------------------------------------------
	if [ -n "$(printf '%s' "$admitted_ids" | tr -d '[:space:]')" ]; then
		local round_spread wall_ms statuses max_recv_val round_last_recv_utc

		round_spread=$(cat "$WORKDIR/r${round}-spread.txt" 2>/dev/null || echo 0)

		# min_job_wall_ms >= JOB_WALL_FLOOR_MS: independent evidence, from
		# Postgres timestamps, that the long-job premise was genuinely
		# exercised for THIS run -- not just nominally configured.
		wall_ms=$(min_job_wall_ms "$admitted_ids")
		assert_ge "$wall_ms" "$JOB_WALL_FLOOR_MS" "round ${round}: minimum admitted-job wall time (ms) >= JOB_WALL_FLOOR_MS"
		# min_job_wall_ms >= SPREAD_SAFETY_FACTOR * this round's send spread:
		# a ratio of two DURATIONS (one derived from each clock separately),
		# so it is offset-free across the pod/DB clocks by construction.
		assert_ge "$wall_ms" "$((SPREAD_SAFETY_FACTOR * round_spread))" "round ${round}: minimum admitted-job wall time (ms) >= SPREAD_SAFETY_FACTOR * this round's send spread"

		# final_statuses == exactly 'done': a 'failed' or 'canceled' member
		# means the long-job premise was never exercised and the round
		# proves nothing -- an OOM-killed or unsupported conversion is
		# terminal too, and terminal-fast is exactly the premise break this
		# plan closes. Hard failure, not a soft warning.
		statuses=$(final_statuses "$admitted_ids")
		assert_eq "done" "$statuses" "round ${round}: every admitted job's final status is exactly 'done'"

		# Append this round's timeline rows -- the committed artifact that
		# answers, after teardown, whether a future over-admit is legitimate
		# slot reuse or a real cap violation (D6).
		max_recv_val=$(cat "$WORKDIR/r${round}-maxrecv.txt" 2>/dev/null || echo 0)
		round_last_recv_utc=$(psql_q "SELECT to_char(to_timestamp(${max_recv_val}/1000.0) AT TIME ZONE 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS.MS\"Z\"')")
		local id_list_for_timeline
		id_list_for_timeline=$(sql_id_list "$admitted_ids")
		psql_rows "SELECT j.id, j.created_at, coalesce(min(e.created_at)::text,''), j.status, coalesce((extract(epoch from (min(e.created_at)-j.created_at))*1000)::bigint::text,'-1') FROM jobs j LEFT JOIN job_events e ON e.job_id=j.id AND e.to_status IN ('done','failed','canceled') WHERE j.id IN (${id_list_for_timeline}) GROUP BY j.id, j.created_at, j.status ORDER BY j.created_at" |
			while IFS=$'\t' read -r tl_id tl_created tl_first_terminal tl_status tl_wall_ms; do
				printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
					"$round" "$tl_id" "$tl_created" "$tl_first_terminal" "$tl_status" "$tl_wall_ms" "$round_last_recv_utc" \
					>>"$EVIDENCE_TIMELINE_TSV"
			done
		echo "PASS: round ${round}: timeline rows appended to ${EVIDENCE_TIMELINE_TSV}"
	fi
}

# ---------------------------------------------------------------------------
# STEP 6: the ${ROUNDS}-round burst loop. `set -euo pipefail` is in force
# and every assertion inside run_round exits non-zero on failure, so
# reaching the ROUNDS_PASSED increment below means the round genuinely
# passed -- there is deliberately no separate status check here, and a
# future editor should not add one (a redundant check could itself become
# a place a real failure gets silently swallowed).
# ---------------------------------------------------------------------------
log "STEP 6: ${ROUNDS}-round burst loop (K=${QUOTA_K}, N=${BURST_N} per round)"

ROUNDS_PASSED=0
for r in $(seq 1 "$ROUNDS"); do
	log "round ${r}/${ROUNDS}"
	run_round "$r"
	ROUNDS_PASSED=$((ROUNDS_PASSED + 1))
done

# ---------------------------------------------------------------------------
# STEP 7: end-of-run global assertions -- each one re-derivable from the
# committed TSV alone by a reviewer who never re-runs the gate.
# ---------------------------------------------------------------------------
log "STEP 7: end-of-run global assertions"

# POD SET UNCHANGED: a pod rescheduled mid-run would break the log-based
# attribution join silently -- every earlier per-round attribution would
# have been drawn against a now-stale pod set without any round-level
# assertion ever catching it.
API_PODS_END=$(kubectl get pods -n "$NAMESPACE" -l app.kubernetes.io/component=api -o jsonpath='{.items[*].metadata.name}')
printf '%s\n' "$API_PODS_END" | tr ' ' '\n' | sort >"$WORKDIR/api-pods-end.txt"
if ! diff -q "$WORKDIR/api-pods-start.txt" "$WORKDIR/api-pods-end.txt" >/dev/null 2>&1; then
	echo "FAIL: api pod set changed during the run." >&2
	echo "  start: $(tr '\n' ' ' <"$WORKDIR/api-pods-start.txt")" >&2
	echo "  end:   $(tr '\n' ' ' <"$WORKDIR/api-pods-end.txt")" >&2
	exit 1
fi
PASS_COUNT=$((PASS_COUNT + 1))
echo "PASS: api pod set unchanged across the run ($(tr '\n' ' ' <"$WORKDIR/api-pods-end.txt"))"

# ZERO RATE-LIMIT RESPONSES: redundant with the per-round hard FAIL by
# design -- this is the assertion a reader of the committed evidence can
# re-run against the TSV without re-running the gate at all. It is
# expected to always find 0 rows in practice (assess_round's per-round
# 429:rate_limited branch already exits 1 before any such row is ever
# written), which is exactly the point: this check is here for the
# reviewer of the committed artifact, not as a live-run trigger.
RATE_LIMITED_ROWS=$(awk -F'\t' 'NR>1 && $7=="rate_limited"' "$EVIDENCE_TSV" | wc -l | tr -d '[:space:]')
assert_eq "0" "$RATE_LIMITED_ROWS" "rate_limited responses across the whole run"

# AGGREGATE TOTALS.
TOTAL_ROWS=$(awk -F'\t' 'NR>1' "$EVIDENCE_TSV" | wc -l | tr -d '[:space:]')
TOTAL_202=$(awk -F'\t' 'NR>1 && $6=="202"' "$EVIDENCE_TSV" | wc -l | tr -d '[:space:]')
TOTAL_429_QUOTA=$(awk -F'\t' 'NR>1 && $6=="429" && $7=="quota_exceeded"' "$EVIDENCE_TSV" | wc -l | tr -d '[:space:]')
assert_eq "$((ROUNDS * BURST_N))" "$TOTAL_ROWS" "TSV data rows (rounds * burst_n)"
assert_eq "$((ROUNDS * QUOTA_K))" "$TOTAL_202" "TSV rows with status 202 (rounds * K)"
assert_eq "$((ROUNDS * (BURST_N - QUOTA_K)))" "$TOTAL_429_QUOTA" "TSV rows with status 429 code quota_exceeded (rounds * (N-K))"
echo "aggregate totals: rows=${TOTAL_ROWS} admits=${TOTAL_202} quota_rejects=${TOTAL_429_QUOTA}"

# CROSS-POD COVERAGE: strictly stronger than the per-round D-05 assertion,
# cheap once the TSV exists -- a failure here is a genuine signal that one
# replica never participated in a cap decision at all across the ENTIRE
# run, not just some individual round.
POD1_202=$(awk -F'\t' -v p="$API_POD_1" 'NR>1 && $6=="202" && $8==p' "$EVIDENCE_TSV" | wc -l | tr -d '[:space:]')
POD1_429=$(awk -F'\t' -v p="$API_POD_1" 'NR>1 && $6=="429" && $8==p' "$EVIDENCE_TSV" | wc -l | tr -d '[:space:]')
POD2_202=$(awk -F'\t' -v p="$API_POD_2" 'NR>1 && $6=="202" && $8==p' "$EVIDENCE_TSV" | wc -l | tr -d '[:space:]')
POD2_429=$(awk -F'\t' -v p="$API_POD_2" 'NR>1 && $6=="429" && $8==p' "$EVIDENCE_TSV" | wc -l | tr -d '[:space:]')
assert_ge "$POD1_202" "1" "pod ${API_POD_1} served at least one 202 across the run"
assert_ge "$POD1_429" "1" "pod ${API_POD_1} served at least one 429 quota_exceeded across the run"
assert_ge "$POD2_202" "1" "pod ${API_POD_2} served at least one 202 across the run"
assert_ge "$POD2_429" "1" "pod ${API_POD_2} served at least one 429 quota_exceeded across the run"
echo "cross-pod coverage: ${API_POD_1} 202=${POD1_202} 429=${POD1_429}, ${API_POD_2} 202=${POD2_202} 429=${POD2_429}"

assert_eq "$ROUNDS" "$ROUNDS_PASSED" "rounds passed"

# ---------------------------------------------------------------------------
# STEP 8: evidence summary -- appended as `#`-prefixed lines so the TSV
# stays greppable by field, and printed to stdout so it also lands in the
# operator's own transcript.
# ---------------------------------------------------------------------------
log "STEP 8: evidence summary"

# RUN_ID already carries a UTC start timestamp by construction (its default
# is `date -u +%Y%m%dT%H%M%SZ`, see the config block) -- no separate "run
# start" variable is introduced here, only the run's end.
RUN_END_UTC=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
{
	echo "#"
	echo "# quota-multireplica-liveproof.sh evidence summary"
	echo "# run_id=${RUN_ID}"
	echo "# run_end_utc=${RUN_END_UTC}"
	echo "# values_files=${VALUES_LOCAL},${VALUES_MULTIREPLICA}"
	echo "# api_replicas_observed_live=$(kubectl get deployment api -n "$NAMESPACE" -o jsonpath='{.spec.replicas}' 2>/dev/null || echo unknown)"
	echo "# api_pod_1=${API_POD_1}"
	echo "# api_pod_2=${API_POD_2}"
	echo "# client_id=${CLIENT_ID}"
	echo "# engine=${ENGINE}"
	echo "# quota_k=${QUOTA_K}"
	echo "# burst_n=${BURST_N}"
	echo "# rounds=${ROUNDS}"
	echo "# rounds_passed=${ROUNDS_PASSED}"
	echo "# total_rows=${TOTAL_ROWS}"
	echo "# total_202=${TOTAL_202}"
	echo "# total_429_quota_exceeded=${TOTAL_429_QUOTA}"
	echo "# pod1_202=${POD1_202} pod1_429=${POD1_429} pod2_202=${POD2_202} pod2_429=${POD2_429}"
	# Plan 55-07 additions -- append-only, existing lines above are
	# untouched so a diff of this block against a pre-55-07 run only ever
	# grows.
	echo "# fixture=${FIXTURE}"
	echo "# fixture_bytes=${FIXTURE_BYTES}"
	echo "# target_format=${TARGET_FORMAT}"
	echo "# burst_send_spread_ms_bound=${BURST_SEND_SPREAD_MS}"
	echo "# min_engine_wall_ms=${MIN_ENGINE_WALL_MS}"
	echo "# timeline_tsv=${EVIDENCE_TIMELINE_TSV_ABS}"
	echo "#"
} | tee -a "$EVIDENCE_TSV"

# =============================================================================
# ALL-PASSED summary -- set only after every assertion above has passed
# (there is no optional/best-effort branch in this script). Teardown runs
# unconditionally via the EXIT trap above and never deletes $EVIDENCE_DIR --
# but it does not commit it either, so the operator must do that explicitly
# before the trap uninstalls the release.
# =============================================================================
GATE_OK=1
echo ""
echo "=== GATE PASS: ${ROUNDS_PASSED}/${ROUNDS} rounds passed, ${PASS_COUNT} assertions passed, evidence at ${EVIDENCE_TSV_ABS} ==="
echo "SC1 (QUOT-05): exactly K (${QUOTA_K}) jobs admitted in every round; every round's cap decision crossed the ${API_POD_1}/${API_POD_2} replica boundary; aggregate cross-pod coverage confirmed both pods served both an admit and a quota_exceeded reject over the whole run. The no-premature-terminal precondition for the exact-K assertion was proven from Postgres every round (Plan 55-07)."
echo "SC2: timestamps, replica identifiers, and admitted/rejected counts committed to ${EVIDENCE_TSV_ABS}; the per-job status timeline (created_at/first_terminal_at/status/wall_ms) committed to ${EVIDENCE_TIMELINE_TSV_ABS}"
echo "IMPORTANT: BOTH evidence files must be committed (ROADMAP SC2) BEFORE the trap below uninstalls the release -- teardown never deletes \$EVIDENCE_DIR but does not commit it either. Commit them explicitly, e.g.:"
echo "  git add -f ${EVIDENCE_TSV_ABS} ${EVIDENCE_TIMELINE_TSV_ABS}"
