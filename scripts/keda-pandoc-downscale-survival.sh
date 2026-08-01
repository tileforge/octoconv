#!/usr/bin/env bash
# keda-pandoc-downscale-survival.sh -- Phase 50 (PDCP-02, SC4) pandoc N->N-1
# downscale-survival live-cluster proof gate.
#
# Sibling of scripts/keda-pandoc-loadproof.sh -- reuses its header/preflight/
# teardown/postJobPath/waitForReplicas*/assertion-helper/fixture-synthesis
# shapes (D-05, mirrors scripts/keda-ocr-downscale-survival.sh's own SC4
# triple-check discipline byte-for-byte in STEP 13 below) -- but this script
# is NOT a mechanical clone of the ocr downscale-survival shape. Pandoc's
# worst-case job is ~32s (PANDOC_ENGINE_TIMEOUT=65s, measured Phase 49)
# versus ocr's 90-120s "LONG job", so ocr's "submit LONG first, then a SHORT
# to force a second replica" victim-selection trick CANNOT work here: a
# genuine KEDA/HPA scale-from-zero to 2 replicas alone can outlive pandoc's
# entire worst-case job, leaving no in-flight window for a downscale to
# land inside.
#
# DEPTH-ARITHMETIC DRIVE SHAPE (the deliberate divergence from the ocr
# clone): with the loadproof overlay pinning pandocWorker.extraEnv
# PANDOC_WORKER_CONCURRENCY=1 (one job per pod, values-loadproof.yaml) and
# keda.pandoc.threshold="2" (desired replicas = ceil(queue_depth/2), where
# depth = pending+active+retry), this script submits PANDOC_DOWNSCALE_JOBS
# (default 4, env-overridable) IDENTICAL ~4 MiB html->md jobs in ONE batch:
#   depth=4 -> desired=2 (a genuine scale-from-zero to 2 replicas)
#   both pods pick up a job each (still depth=4: 2 active + 2 pending)
#   as the first two jobs finish, each pod immediately dequeues its next
#   pending job -> depth falls to 2 (2 active + 0 pending) -> desired=1
# At the MOMENT depth first reaches 2, BOTH pods are freshly re-busy on
# their FINAL job with nothing left pending -- this is exactly when KEDA/
# HPA decides to scale 2->1, so the genuine downscale lands while both
# pods are mid-conversion. This script never issues a manual pod-deletion
# or a directly-forced replica-count command anywhere -- the 2->1 step is
# produced SOLELY by the queue-depth metric crossing the threshold.
#
# ATTRIBUTION (replaces ocr's "we know which job we submitted first" --
# with N identical jobs in one batch there is no submission-order signal
# to rely on). A background poller runs every 2s against the annotated
# victim pod: `kubectl exec ... -- sh -c 'cat /proc/[0-9]*/cmdline | tr NUL
# newline'`, extracts the first `/tmp/octoconv-<job_id>-<rand>/` path
# component the worker's own os.MkdirTemp("", "octoconv-"+job.ID.String()+
# "-") writes (internal/worker/worker.go ~line 1793), reduces it to the
# 36-char job UUID, and appends `read_ts=<iso8601>
# victim_inflight_job=<uuid-or-empty>` lines to the SC4 evidence file. The
# in-flight job attributed to the downscale is resolved as the LAST
# non-empty sample recorded at-or-before the kubelet's Killing-event
# timestamp -- if every sample is empty (the victim was genuinely idle at
# SIGTERM time), this script FAILS LOUDLY with the poller transcript and a
# re-run diagnostic (raise PANDOC_DOWNSCALE_JOBS), NEVER silently guessing
# a job id.
#
# SC4 ASSERTIONS mirror scripts/keda-ocr-downscale-survival.sh's triple-check
# EXACTLY (STEP 13 below), retargeted at the attributed INFLIGHT_JOB_ID and
# the PRODUCTION grace value 85 (PANDOC_ENGINE_TIMEOUT=65s + 10s asynq
# ShutdownTimeout margin + 10s SIGKILL margin, D-04b, values.yaml
# pandocWorker.terminationGracePeriodSeconds), plus one addition ocr's
# single-in-flight-job shape did not need: every OTHER job in the batch
# must also reach done (no collateral loss on the surviving pod).
#
# SCOPE NOTE (D-05a, carry verbatim, do not silently drop): the
# signed-webhook half of D-05's evidence line is NOT re-proven in-cluster
# by this gate -- an in-cluster webhook receiver would require enabling the
# webhook SSRF private-IP opt-out purely for the gate, adding a failure
# mode to the proof that is orthogonal to what SC4 actually validates. The
# pandoc signed-webhook path is already live-proven end-to-end in Phase 49
# Plan 04 (TestPandocConversionE2E, md->html + md->docx + signed webhook
# against the live compose stack). THIS gate proves the downscale-survival
# half only: the in-flight job reaches `done`, exactly one queued->active
# transition (no requeue/retry), and the victim pod exits gracefully.
#
# --calibrate-only: runs ONLY the fixture-synthesis + a single timed
# html->md conversion at the PRODUCTION container limits (cpus=2,
# memory=1g), asserts the fixture lands in [3000000, 4194304] bytes and the
# conversion lands in [15, 50] wall-seconds, writes a committed calibration
# evidence file, and exits 0 -- WITHOUT touching kubectl or helm at all
# (Task 2 of this plan runs this mode before any cluster time is spent).
#
# scripts/keda-gate.sh, scripts/keda-load-proof.sh,
# scripts/keda-audio-loadproof.sh, scripts/keda-av-loadproof.sh,
# scripts/keda-av-downscale-survival.sh, scripts/keda-ocr-loadproof.sh,
# scripts/keda-ocr-downscale-survival.sh, and scripts/keda-pandoc-loadproof.sh
# are left byte-unchanged by this plan -- pandoc downscale-survival logic
# lives exclusively in this file. No frozen/sibling script is touched.
#
# This gate is SELF-CONTAINED: it installs KEDA itself, layers BOTH
# values-local.yaml AND values-loadproof.yaml on top of the chart, and
# tears everything down via an EXIT trap -- success or failure, OrbStack is
# never left hot. It refuses to run if the docker-compose stack is up
# (compose and k8s stacks must never be hot simultaneously -- four
# confirmed OrbStack daemon wedges on record).
#
# T-50-10 (tampering -- a faked/forced downscale) / T-50-11 (repudiation --
# wrong job attributed) / T-50-12 (tampering/data-loss -- premature SIGKILL)
# / T-50-13 (DoS -- orphaned watcher/poller/fixture container) / T-50-14
# (DoS -- oversized fixture OOM) / T-50-15 (DoS -- conversion crossing
# PANDOC_ENGINE_TIMEOUT) from this plan's threat register are mitigated
# below: no manual pod-deletion/forced-replica-count command exists in this
# file anywhere; attribution reads the victim pod's OWN /proc/<pid>/cmdline
# rather than trusting submission order; the EXIT trap kills the pod-state
# watcher AND the attribution poller AND the fixture container by
# process-group + belt-and-suspenders pkill (Phase-29 WR-01/WR-02/WR-03
# pattern); the fixture is capped at the production 4 MiB
# PANDOC_MAX_INPUT_BYTES; and Task 2's --calibrate-only run proves the job
# window is [15, 50]s -- safely under PANDOC_ENGINE_TIMEOUT=65s -- before
# any cluster time is spent.
set -euo pipefail

cd "$(dirname "$0")/.."

# ---------------------------------------------------------------------------
# --calibrate-only flag (Task 2 precondition -- container-only, no kubectl/
# helm dependency at all).
# ---------------------------------------------------------------------------
CALIBRATE_ONLY=""
if [ "${1:-}" = "--calibrate-only" ]; then
	CALIBRATE_ONLY="1"
fi

# ---------------------------------------------------------------------------
# Config / constants
# ---------------------------------------------------------------------------
NAMESPACE="octoconv"
KEDA_NAMESPACE="keda"
KEDA_VERSION="2.20.1"
CHART_DIR="deploy/chart/octoconv"
VALUES_LOCAL="deploy/chart/octoconv/values-local.yaml"
VALUES_LOADPROOF="deploy/chart/octoconv/values-loadproof.yaml"

# The pandoc-worker image is NOT built by this script -- operator
# precondition (same as scripts/keda-pandoc-loadproof.sh): `docker compose
# build pandoc-worker` (or an equivalent docker build against
# Dockerfile.pandoc-worker) must already have produced this exact tag.
PANDOC_IMAGE_TAG="octoconv-pandoc-worker:dev"

# Distinct local ports from every sibling gate script (ocr's 18095/15439,
# 18096/15440; pandoc SC3's 18097/15441) so this gate can never collide with
# a concurrently-running one.
API_LOCAL_PORT="18098"
API_BASE="http://127.0.0.1:${API_LOCAL_PORT}"
DB_LOCAL_PORT="15442"

# N identical LONG (~4 MiB html->md) jobs submitted in one batch (drive
# shape derivation in the header comment above) -- env-overridable for a
# re-run with more headroom if the attribution poller ever finds the victim
# idle at SIGTERM time.
PANDOC_DOWNSCALE_JOBS="${PANDOC_DOWNSCALE_JOBS:-4}"

# Production input-size ceiling (ConfigMap PANDOC_MAX_INPUT_BYTES,
# docker-compose.yml pandoc-worker, Path-B/49-03-SUMMARY.md) -- the LONG
# fixture must stay strictly under this, enforced worker-side by
# enforcePandocInputSize.
PANDOC_MAX_INPUT_BYTES=4194304
FIXTURE_BAND_LOW=3000000

PASS_COUNT=0
GATE_OK=""
API_PF_PID=""
DB_PF_PID=""
SNAPSHOT_PID=""
POLLER_PID=""
FIXTURE_CONTAINER=""
# VICTIM_POD is set once the newer of the two replicas is identified; kept
# declared here (empty) so teardown() can unconditionally reference it
# regardless of how far the run got before failing.
VICTIM_POD=""
CAL_FILE=""

EVIDENCE_DIR=".planning/phases/50-keda-helm-chart-integration/evidence"
RUN_TS=$(date -u +%Y%m%dT%H%M%SZ)
mkdir -p "$EVIDENCE_DIR"
LOG_FILE="$EVIDENCE_DIR/gate-transcript-downscale-${RUN_TS}.log"

# Tee the whole run to the timestamped transcript -- every PASS/FAIL line,
# every observed value, becomes part of the committed evidence.
exec > >(tee "$LOG_FILE") 2>&1

# ---------------------------------------------------------------------------
# Assertion helpers -- copied verbatim from scripts/keda-ocr-downscale-
# survival.sh / scripts/keda-pandoc-loadproof.sh / every sibling gate.
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

log() { echo ""; echo "--- $* ---"; }

# ---------------------------------------------------------------------------
# synthesizeLongFixture -- reuses scripts/keda-pandoc-loadproof.sh's SC3
# fixture-synthesis helper shape (in-container corpus generation via the
# image's own pinned pandoc binary, docker-cp'd out to a host path) rather
# than inventing a second mechanism. Aims the rendered HTML at just under
# the PANDOC_MAX_INPUT_BYTES ceiling: generate a deterministic Markdown
# paragraph repeated to a byte target, render it through the image's own
# `pandoc -f markdown-raw_html-raw_tex -t html --sandbox --standalone`
# (the exact production reader spec, internal/convert/pandoc.go), measure
# with `wc -c`, and if the rendered HTML exceeds the ceiling regenerate
# from a proportionally smaller Markdown target -- bounded to 3 attempts,
# then a hard fail with the observed sizes (T-50-14 mitigation). Requires
# FIXTURE_CONTAINER to already be running (--cpus/--memory already applied
# by the caller) with /tmp/work created. Writes the fixture to
# $1 (host path) and echoes the final byte count on stdout (all
# diagnostics go to stderr) so callers can capture it with $(...).
# ---------------------------------------------------------------------------
synthesizeLongFixture() {
	local out_host_path="$1"
	local md_target=3200000
	local attempt=1
	local html_bytes=0

	while [ "$attempt" -le 3 ]; do
		docker exec -e MD_TARGET_BYTES="$md_target" "$FIXTURE_CONTAINER" sh -c '
			set -e
			para="# OctoConv Pandoc SC4 Downscale-Survival Fixture\n\nThis is a deterministic Markdown paragraph synthesized at gate-run time for scripts/keda-pandoc-downscale-survival.sh (Phase 50, PDCP-02/SC4), mirroring the corpus-generation shape in scripts/pandoc-timing-measure.sh (Phase 49) and scripts/keda-pandoc-loadproof.sh (SC3). No client data and no external content -- pure synthetic filler text used solely to drive a real html->md job through the production pipeline at a representative worst-case size.\n\n"
			: > /tmp/work/base.md
			while [ "$(wc -c < /tmp/work/base.md)" -lt "$MD_TARGET_BYTES" ]; do
				printf "%b" "$para" >> /tmp/work/base.md
			done
			head -c "$MD_TARGET_BYTES" /tmp/work/base.md > /tmp/work/base.md.tmp
			mv /tmp/work/base.md.tmp /tmp/work/base.md
		' >&2
		docker exec "$FIXTURE_CONTAINER" pandoc -f markdown-raw_html-raw_tex -t html --sandbox --standalone \
			/tmp/work/base.md -o /tmp/work/long.html >&2
		html_bytes=$(docker exec "$FIXTURE_CONTAINER" wc -c < /tmp/work/long.html | tr -d '[:space:]')
		echo "fixture synthesis attempt=$attempt md_target_bytes=$md_target html_bytes=$html_bytes" >&2
		if [ "$html_bytes" -le "$PANDOC_MAX_INPUT_BYTES" ] && [ "$html_bytes" -ge "$FIXTURE_BAND_LOW" ]; then
			break
		fi
		if [ "$html_bytes" -gt "$PANDOC_MAX_INPUT_BYTES" ]; then
			md_target=$((md_target * PANDOC_MAX_INPUT_BYTES / html_bytes))
		else
			md_target=$((md_target * 2))
		fi
		attempt=$((attempt + 1))
	done

	if [ "$html_bytes" -gt "$PANDOC_MAX_INPUT_BYTES" ] || [ "$html_bytes" -lt "$FIXTURE_BAND_LOW" ]; then
		echo "FAIL: could not synthesize an HTML fixture in [$FIXTURE_BAND_LOW, $PANDOC_MAX_INPUT_BYTES] bytes within 3 attempts (last observed: $html_bytes bytes)" >&2
		exit 1
	fi

	docker cp "$FIXTURE_CONTAINER:/tmp/work/long.html" "$out_host_path" >&2
	echo "$html_bytes"
}

# ---------------------------------------------------------------------------
# runCalibration -- Task 2's --calibrate-only path: fixture synthesis +
# ONE timed html->md conversion at the PRODUCTION container limits, no
# kubectl/helm dependency whatsoever. Proves the SC4 job window is
# measured, not assumed, before any cluster time is spent.
# ---------------------------------------------------------------------------
runCalibration() {
	log "CALIBRATE-ONLY: offline fixture synthesis + timing measurement (container-only, no kubectl/helm)"

	if ! docker image inspect "$PANDOC_IMAGE_TAG" >/dev/null 2>&1; then
		echo "FAIL: $PANDOC_IMAGE_TAG not found locally -- run 'docker compose build pandoc-worker' (or an equivalent docker build against Dockerfile.pandoc-worker) before calibrating." >&2
		exit 1
	fi
	echo "PASS: $PANDOC_IMAGE_TAG present locally"

	local cal_workdir
	cal_workdir=$(mktemp -d)
	FIXTURE_CONTAINER="octoconv-pandoc-calibrate-$$"
	# PRODUCTION limits (D-04a, values.yaml pandocWorker.resources.limits /
	# docker-compose.yml pandoc-worker deploy.resources.limits): the 49-03
	# measurement ran at exactly cpus=2.0/memory=1g and Path-B lowered
	# PANDOC_MAX_INPUT_BYTES to 4 MiB specifically for 1g headroom -- this
	# calibration MUST run at the same limits or the measured window means
	# nothing.
	docker run -d --name "$FIXTURE_CONTAINER" --cpus 2 --memory 1g --entrypoint sleep "$PANDOC_IMAGE_TAG" infinity >/dev/null
	docker exec "$FIXTURE_CONTAINER" mkdir -p /tmp/work

	local html_bytes
	html_bytes=$(synthesizeLongFixture "$cal_workdir/long.html")
	PASS_COUNT=$((PASS_COUNT + 1))
	echo "PASS: calibration fixture_bytes=$html_bytes in [$FIXTURE_BAND_LOW, $PANDOC_MAX_INPUT_BYTES]"

	# Time ONE html -> md conversion through the FULL production reader/
	# writer spec (pandocReaderSpec("html")="html-raw_html-raw_tex",
	# pandocFormatName("md")="markdown", NO --standalone since md is not a
	# pandocStructuralTarget) -- internal/convert/pandoc.go's
	# pandocConvertArgs, replicated verbatim (mirrors
	# scripts/pandoc-timing-measure.sh's run_cell timing shape, `--sandbox`
	# always on).
	local raw_ms wall_seconds in_band
	raw_ms=$(docker exec "$FIXTURE_CONTAINER" sh -c '
		start=$(date +%s%N)
		pandoc -f html-raw_html-raw_tex -t markdown --sandbox /tmp/work/long.html -o /tmp/work/long.md
		end=$(date +%s%N)
		echo $(( (end - start) / 1000000 ))
	')
	wall_seconds=$(awk -v ms="$raw_ms" 'BEGIN { printf "%.3f", ms / 1000 }')
	echo "conversion wall_seconds=$wall_seconds (raw_ms=$raw_ms)"

	in_band=$(awk -v s="$wall_seconds" 'BEGIN { print (s >= 15 && s <= 50) ? "yes" : "no" }')
	if [ "$in_band" != "yes" ]; then
		echo "FAIL: calibration wall_seconds=$wall_seconds is outside the required [15, 50] band -- below 15s the SC4 window is too short for the metric-propagation budget (Prometheus 5s scrape + KEDA 3s poll + 15s HPA sync in the loadproof overlay) and the drive shape needs more jobs; above 50s the job is uncomfortably close to PANDOC_ENGINE_TIMEOUT=65s. Do NOT edit this band -- adjust the synthesized fixture size within the $PANDOC_MAX_INPUT_BYTES ceiling and re-run." >&2
		exit 1
	fi
	PASS_COUNT=$((PASS_COUNT + 1))
	echo "PASS: calibration wall_seconds=$wall_seconds in [15, 50]"

	local host_arch
	host_arch=$(uname -m)
	CAL_FILE="$EVIDENCE_DIR/calibration-pandoc-downscale-fixture-${RUN_TS}.txt"
	{
		echo "# Phase 50 Plan 03 pandoc downscale-survival OFFLINE calibration -- run $RUN_TS"
		echo "# scripts/keda-pandoc-downscale-survival.sh --calibrate-only (Task 2, PDCP-02/SC4 precondition)"
		echo "host_arch=$host_arch"
		echo "container_cpus=2"
		echo "container_memory=1g"
		echo "fixture_bytes=$html_bytes"
		echo "conversion_command=pandoc -f html-raw_html-raw_tex -t markdown --sandbox /tmp/work/long.html -o /tmp/work/long.md"
		echo "wall_seconds=$wall_seconds"
		echo "raw_ms=$raw_ms"
		echo "comparison_49_03_worst_cell=html_large_to_md"
		echo "comparison_49_03_p95_seconds=32.318"
		echo "band_lower_seconds=15"
		echo "band_upper_seconds=50"
		echo "pandoc_engine_timeout_seconds=65"
	} >"$CAL_FILE"
	echo "PASS: calibration evidence written -- $CAL_FILE"

	docker rm -f "$FIXTURE_CONTAINER" >/dev/null 2>&1 || true
	FIXTURE_CONTAINER=""
	rm -rf "$cal_workdir"
}

# ---------------------------------------------------------------------------
# Teardown -- ALWAYS runs (trap on EXIT), success or failure: OrbStack must
# never be left hot. In --calibrate-only mode, only the fixture container
# (if still registered) is cleaned up -- kubectl/helm are never touched.
# ---------------------------------------------------------------------------
teardown() {
	local exit_code=$?
	echo ""
	echo "=== TEARDOWN ==="

	if [ -n "$SNAPSHOT_PID" ]; then
		# WR-04/29-REVIEW WR-01: kill the whole process group (own group via
		# parent `set -m` at the launch site below) so a reparented
		# `kubectl -w` pipeline cannot survive this EXIT trap.
		kill -- -"$SNAPSHOT_PID" >/dev/null 2>&1 || true
		wait "$SNAPSHOT_PID" 2>/dev/null || true
		[ -n "$VICTIM_POD" ] && pkill -f "kubectl get pod ${VICTIM_POD} .* -w" >/dev/null 2>&1 || true
	fi
	if [ -n "$POLLER_PID" ]; then
		kill -- -"$POLLER_PID" >/dev/null 2>&1 || true
		wait "$POLLER_PID" 2>/dev/null || true
		[ -n "$VICTIM_POD" ] && pkill -f "kubectl exec -n ${NAMESPACE} ${VICTIM_POD}" >/dev/null 2>&1 || true
	fi
	if [ -n "$FIXTURE_CONTAINER" ]; then
		docker rm -f "$FIXTURE_CONTAINER" >/dev/null 2>&1 || true
	fi

	if [ -n "$CALIBRATE_ONLY" ]; then
		# --calibrate-only never touches kubectl/helm/port-forwards (T-50-13
		# mitigation applies identically -- the fixture container above is
		# the only resource this mode creates) -- nothing further to tear
		# down.
		if [ "$exit_code" -eq 0 ] && [ "$GATE_OK" = "1" ]; then
			echo "PASS -- offline calibration complete. Evidence: ${CAL_FILE:-<not written>}"
		else
			echo "FAIL -- offline calibration did not complete (exit=$exit_code)." >&2
		fi
		exit "$exit_code"
	fi

	if [ -n "$API_PF_PID" ]; then
		kill "$API_PF_PID" >/dev/null 2>&1 || true
	fi
	if [ -n "$DB_PF_PID" ]; then
		kill "$DB_PF_PID" >/dev/null 2>&1 || true
	fi

	helm uninstall octoconv -n "$NAMESPACE" >/dev/null 2>&1 || true
	helm uninstall keda -n "$KEDA_NAMESPACE" >/dev/null 2>&1 || true

	echo "waiting for octoconv workloads to be gone..."
	remaining="unknown"
	for i in $(seq 1 30); do
		remaining=$(kubectl get deployment -n "$NAMESPACE" --no-headers 2>/dev/null | wc -l | tr -d '[:space:]')
		if [ "${remaining:-0}" = "0" ]; then
			break
		fi
		sleep 2
	done
	echo "octoconv namespace deployments remaining: ${remaining:-unknown}"

	echo ""
	if [ "$exit_code" -eq 0 ] && [ "$GATE_OK" = "1" ]; then
		echo "PASS -- Phase 50 pandoc downscale-survival load-proof gate run complete ($PASS_COUNT checks). Transcript: $LOG_FILE"
	else
		echo "FAIL -- Phase 50 pandoc downscale-survival load-proof gate did not complete (exit=$exit_code, checks passed=$PASS_COUNT)." >&2
	fi
	exit "$exit_code"
}
trap teardown EXIT

# ---------------------------------------------------------------------------
# --calibrate-only short-circuit -- runs before ANY kubectl/helm reference,
# so this mode has zero cluster dependency (verified statically: this
# branch returns before STEP 1 below).
# ---------------------------------------------------------------------------
if [ -n "$CALIBRATE_ONLY" ]; then
	runCalibration
	GATE_OK="1"
	exit 0
fi

echo "=== Phase 50 pandoc downscale-survival live-proof gate (PDCP-02, SC4) ==="
echo "run timestamp: $RUN_TS"
echo "evidence dir: $EVIDENCE_DIR"
echo "PANDOC_DOWNSCALE_JOBS=$PANDOC_DOWNSCALE_JOBS"

# ---------------------------------------------------------------------------
# STEP 1: Preflight -- compose and k8s stacks must never be hot
# simultaneously.
# ---------------------------------------------------------------------------
log "STEP 1: preflight"

kubectl get nodes >/dev/null
echo "PASS: kubectl reaches the OrbStack cluster (context: $(kubectl config current-context))"

COMPOSE_UP=$(docker compose ps --format '{{.Names}}' 2>/dev/null | grep -c '^octoconv-' || true)
if [ "${COMPOSE_UP:-0}" -gt 0 ]; then
	echo "FAIL: compose stack appears to be UP ($COMPOSE_UP octoconv-* containers running) -- compose and k8s stacks must NEVER be hot simultaneously. Run 'docker compose ... down -v' first." >&2
	exit 1
fi
echo "PASS: compose stack is down (0 octoconv-* containers running)"

if ! docker image inspect "$PANDOC_IMAGE_TAG" >/dev/null 2>&1; then
	echo "FAIL: $PANDOC_IMAGE_TAG not found locally -- run 'docker compose build pandoc-worker' (or an equivalent docker build against Dockerfile.pandoc-worker) before this gate." >&2
	exit 1
fi
echo "PASS: $PANDOC_IMAGE_TAG present locally"

helm repo add kedacore https://kedacore.github.io/charts >/dev/null 2>&1 || true
helm repo update >/dev/null
if ! helm search repo kedacore/keda --versions | awk '{print $2}' | grep -qx "$KEDA_VERSION"; then
	LATEST_KEDA=$(helm search repo kedacore/keda --versions | awk 'NR==2{print $2}')
	echo "FAIL: KEDA v$KEDA_VERSION is no longer resolvable in kedacore/keda -- current latest is $LATEST_KEDA. Repin KEDA_VERSION in this script and re-run." >&2
	exit 1
fi
echo "PASS: KEDA v$KEDA_VERSION re-verified resolvable in kedacore/keda (live)"

# ---------------------------------------------------------------------------
# STEP 2: Install KEDA (idempotent) -- gate is self-contained.
# ---------------------------------------------------------------------------
log "STEP 2: helm install KEDA v$KEDA_VERSION into namespace $KEDA_NAMESPACE"

if helm status keda -n "$KEDA_NAMESPACE" >/dev/null 2>&1; then
	echo "keda release already present -- upgrading in place (idempotent)"
	helm upgrade keda kedacore/keda --namespace "$KEDA_NAMESPACE" --version "$KEDA_VERSION" --wait --timeout 5m
else
	helm install keda kedacore/keda --namespace "$KEDA_NAMESPACE" --create-namespace --version "$KEDA_VERSION" --wait --timeout 5m
fi
echo "PASS: KEDA v$KEDA_VERSION installed/upgraded, operator Deployments Available"

log "STEP 3: waiting for v1beta1.external.metrics.k8s.io to become Available"
APISERVICE_READY=""
COND=""
for i in $(seq 1 30); do
	COND=$(kubectl get apiservice v1beta1.external.metrics.k8s.io -o jsonpath='{.status.conditions[?(@.type=="Available")].status}' 2>/dev/null || true)
	if [ "$COND" = "True" ]; then
		APISERVICE_READY=1
		break
	fi
	sleep 2
done
if [ -z "$APISERVICE_READY" ]; then
	echo "FAIL: v1beta1.external.metrics.k8s.io never reported Available:True after 60s (last observed condition: [$COND])" >&2
	exit 1
fi
echo "PASS: v1beta1.external.metrics.k8s.io Available:True"

# ---------------------------------------------------------------------------
# STEP 4: Install octoconv layering BOTH values-local.yaml AND
# values-loadproof.yaml (D-05: pandoc's gate override + the
# pandocWorker.extraEnv PANDOC_WORKER_CONCURRENCY=1 hook this drive shape
# depends on) -- WITHOUT --wait (Phase 24 decision: createbucket
# post-install hook <-> app-readiness chicken-egg). Then kubectl wait per
# always-on Deployment only -- pandoc-worker (KEDA/HPA-owned) is
# intentionally excluded from this wait.
# ---------------------------------------------------------------------------
log "STEP 4: helm install octoconv -f values-local.yaml -f values-loadproof.yaml"

helm install octoconv "$CHART_DIR" -f "$VALUES_LOCAL" -f "$VALUES_LOADPROOF" -n "$NAMESPACE" --create-namespace
echo "PASS: helm install octoconv complete (async install; readiness gated below)"

log "waiting for always-on / min-1 Deployments to become Available"
for d in postgres redis minio api prometheus webhook-worker; do
	kubectl wait --for=condition=Available "deployment/$d" -n "$NAMESPACE" --timeout=240s
	echo "PASS: deployment/$d Available"
done

# ---------------------------------------------------------------------------
# STEP 4b: seed asynq's queue registry (Redis "asynq:queues" SET),
# including pandoc, so the WR-01 absent-metric fallback resolves to a real
# zero instead of holding fallback.replicas:1 indefinitely on a fresh
# install.
# ---------------------------------------------------------------------------
log "STEP 4b: seed asynq queue registry (Redis), including pandoc"

REDIS_POD=$(kubectl get pod -n "$NAMESPACE" -l "app.kubernetes.io/component=redis" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
assert_nonempty "$REDIS_POD" "redis pod discovered for queue-registry seeding"
kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli SADD asynq:queues image document html audio av ocr pandoc webhook >/dev/null
echo "PASS: asynq:queues seeded (image, document, html, audio, av, ocr, pandoc, webhook) -- zero tasks created, no worker processing triggered"

# ---------------------------------------------------------------------------
# STEP 5: port-forward api+postgres, mint a client key.
# ---------------------------------------------------------------------------
log "STEP 5: port-forward api+postgres, mint client key"

kubectl port-forward -n "$NAMESPACE" svc/api "${API_LOCAL_PORT}:8090" >/tmp/keda-pandoc-downscale-api-pf.log 2>&1 &
API_PF_PID=$!
kubectl port-forward -n "$NAMESPACE" svc/postgres "${DB_LOCAL_PORT}:5432" >/tmp/keda-pandoc-downscale-db-pf.log 2>&1 &
DB_PF_PID=$!
sleep 3

echo "waiting for port-forwarded /healthz..."
healthy=""
for i in $(seq 1 30); do
	code=$(curl -s -o /tmp/keda-pandoc-downscale-healthz.json -w '%{http_code}' "$API_BASE/healthz" || true)
	if [ "$code" = "200" ]; then
		healthy=1
		break
	fi
	sleep 2
done
if [ -z "$healthy" ]; then
	echo "FAIL: /healthz never returned 200 through the port-forward after 60s" >&2
	exit 1
fi
echo "PASS: api reachable via port-forward, /healthz 200 ($(cat /tmp/keda-pandoc-downscale-healthz.json))"

export DATABASE_URL="postgres://octo:octo-pass@127.0.0.1:${DB_LOCAL_PORT}/octo_db"
export API_KEY_SALT="dev-only-change-me-in-real-deploys"

SUFFIX=$(date +%s)
CLIENT_OUT=$(go run ./cmd/manage-clients create "keda-pandoc-downscale-${SUFFIX}")
CLIENT_KEY=$(printf '%s\n' "$CLIENT_OUT" | awk -F': ' '/^api key/{print $2}')
assert_nonempty_redacted "$CLIENT_KEY" "minted gate client + API key"

# postJobPath submits an ABSOLUTE-path fixture (the batch jobs are
# synthesized at gate-run time into $WORKDIR, never committed under
# internal/convert/testdata/pandoc -- pandoc has no committed testdata).
postJobPath() {
	local path="$1" target="$2" content_type="$3" opts="${4:-}"
	local filename
	filename=$(basename "$path")
	local out_file="/tmp/keda-pandoc-downscale-post-${filename}-$$-${RANDOM}.json"
	local curl_args=(-s -o "$out_file" -w '%{http_code}' -X POST "$API_BASE/v1/jobs" \
		-H "Authorization: ApiKey $CLIENT_KEY" \
		-F "target=$target" \
		-F "file=@${path};type=${content_type}")
	if [ -n "$opts" ]; then
		curl_args+=(-F "opts=$opts")
	fi
	HTTP_STATUS=$(curl "${curl_args[@]}" || true)
	if [ "$HTTP_STATUS" != "202" ]; then
		echo "FAIL: POST /v1/jobs for $filename -> $target returned $HTTP_STATUS, body: $(cat "$out_file")" >&2
		exit 1
	fi
	grep -o '"job_id":"[^"]*"' "$out_file" | head -1 | cut -d'"' -f4
}

jobStatus() {
	local id="$1"
	curl -s -H "Authorization: ApiKey $CLIENT_KEY" "$API_BASE/v1/jobs/$id" | grep -o '"status":"[^"]*"' | head -1 | cut -d'"' -f4 || true
}

waitForReplicasAtLeast() {
	local deployment="$1" floor="$2" timeout_s="$3" observed="0"
	local waited=0
	while [ "$waited" -lt "$timeout_s" ]; do
		observed=$(kubectl get deployment "$deployment" -n "$NAMESPACE" -o jsonpath='{.status.replicas}' 2>/dev/null || echo "0")
		observed="${observed:-0}"
		if [ "$observed" -ge "$floor" ]; then
			echo "$observed"
			return 0
		fi
		sleep 3
		waited=$((waited + 3))
	done
	echo "TIMEOUT(last=$observed)"
	return 1
}

waitForReplicasAtMost() {
	local deployment="$1" ceiling="$2" timeout_s="$3" observed="0"
	local waited=0
	while [ "$waited" -lt "$timeout_s" ]; do
		observed=$(kubectl get deployment "$deployment" -n "$NAMESPACE" -o jsonpath='{.status.replicas}' 2>/dev/null || echo "0")
		observed="${observed:-0}"
		if [ "$observed" -le "$ceiling" ]; then
			echo "$observed"
			return 0
		fi
		sleep 3
		waited=$((waited + 3))
	done
	echo "TIMEOUT(last=$observed)"
	return 1
}

toEpoch() {
	local ts="$1"
	if [ -z "$ts" ]; then
		echo "0"
		return
	fi
	date -u -j -f "%Y-%m-%dT%H:%M:%SZ" "$ts" +%s 2>/dev/null || date -u -d "$ts" +%s 2>/dev/null || echo "0"
}

# ---------------------------------------------------------------------------
# STEP 6: confirm pandoc-worker is genuinely at 0 replicas before triggering
# -- the load-bearing precondition for a genuine scale-from-zero, not an
# already-warm scale-up.
# ---------------------------------------------------------------------------
log "STEP 6: confirm pandoc-worker settles at 0 replicas before triggering"

echo "waiting for pandoc-worker to settle at 0 replicas (KEDA cooldownPeriod=90s + margin)..."
PANDOC_REPLICAS_BEFORE="1"
waited=0
while [ "$waited" -lt 240 ]; do
	PANDOC_REPLICAS_BEFORE=$(kubectl get deployment pandoc-worker -n "$NAMESPACE" -o jsonpath='{.status.replicas}' 2>/dev/null || echo "0")
	PANDOC_REPLICAS_BEFORE="${PANDOC_REPLICAS_BEFORE:-0}"
	if [ "$PANDOC_REPLICAS_BEFORE" = "0" ]; then
		break
	fi
	sleep 5
	waited=$((waited + 5))
done
assert_eq "0" "$PANDOC_REPLICAS_BEFORE" "pandoc-worker Deployment status.replicas before SC4 (TRUE zero precondition)"

# ---------------------------------------------------------------------------
# STEP 7: synthesize the LONG ~4 MiB html fixture in-container (reuses
# synthesizeLongFixture, the same helper --calibrate-only exercises). Also
# generate a trivially small filler markdown file for potential future
# fast-job use -- the depth-arithmetic drive shape above submits N
# IDENTICAL LONG jobs and does not need a distinct filler job the way
# ocr's LONG-then-SHORT victim-selection trick did, so this filler is
# synthesized (per this plan's action text) but intentionally left unused.
# ---------------------------------------------------------------------------
log "STEP 7: synthesize LONG (~4 MiB html) fixture + small unused filler markdown"

WORKDIR=$(mktemp -d)
FIXTURE_CONTAINER="octoconv-pandoc-downscale-fixture-$$"
docker run -d --name "$FIXTURE_CONTAINER" --entrypoint sleep "$PANDOC_IMAGE_TAG" infinity >/dev/null
docker exec "$FIXTURE_CONTAINER" mkdir -p /tmp/work

LONG_FIXTURE_BYTES=$(synthesizeLongFixture "$WORKDIR/pandoc-downscale-long.html")
PASS_COUNT=$((PASS_COUNT + 1))
echo "PASS: LONG fixture bytes=$LONG_FIXTURE_BYTES in [$FIXTURE_BAND_LOW, $PANDOC_MAX_INPUT_BYTES]"

docker exec "$FIXTURE_CONTAINER" sh -c '
	set -e
	printf "%s\n\n" "# OctoConv Pandoc SC4 filler" > /tmp/work/filler.md
	printf "%s\n" "A trivially small deterministic Markdown filler generated for potential future fast-job use alongside the LONG fixture above. The depth-arithmetic drive shape (N identical LONG jobs) does not submit this filler as a job -- see the DEPTH-ARITHMETIC DRIVE SHAPE header comment." >> /tmp/work/filler.md
'
docker cp "$FIXTURE_CONTAINER:/tmp/work/filler.md" "$WORKDIR/pandoc-downscale-filler.md"
assert_nonempty "$(ls -la "$WORKDIR/pandoc-downscale-filler.md" 2>/dev/null || true)" "small filler markdown fixture synthesized (available, not submitted by this drive shape)"

docker rm -f "$FIXTURE_CONTAINER" >/dev/null 2>&1 || true
FIXTURE_CONTAINER=""

# ---------------------------------------------------------------------------
# STEP 8: submit PANDOC_DOWNSCALE_JOBS IDENTICAL LONG (html -> md, ~4 MiB)
# jobs in one batch -- depth=N immediately, desired=ceil(N/2) with
# threshold="2" (values.yaml keda.pandoc.threshold, D-02).
# ---------------------------------------------------------------------------
log "STEP 8: submit $PANDOC_DOWNSCALE_JOBS identical LONG jobs (html -> md, ~${LONG_FIXTURE_BYTES}B each)"

BATCH_JOB_IDS=()
for i in $(seq 1 "$PANDOC_DOWNSCALE_JOBS"); do
	JID=$(postJobPath "$WORKDIR/pandoc-downscale-long.html" "md" "text/html")
	assert_nonempty "$JID" "SC4: batch job #$i submitted (html -> md, LONG fixture)"
	BATCH_JOB_IDS+=("$JID")
done
BATCH_IDS_SQL_LIST=$(printf "'%s'," "${BATCH_JOB_IDS[@]}")
BATCH_IDS_SQL="(${BATCH_IDS_SQL_LIST%,})"

batchDepth() {
	psql "postgres://octo:octo-pass@127.0.0.1:${DB_LOCAL_PORT}/octo_db" -tAc \
		"SELECT count(*) FROM jobs WHERE id IN ${BATCH_IDS_SQL} AND status IN ('queued','active');" | tr -d '[:space:]'
}

# ---------------------------------------------------------------------------
# STEP 9: wait for pandoc-worker to reach 2 Ready replicas (bounded 240s),
# identify BOTH pods with their creationTimestamps, annotate the NEWER pod
# (latest creationTimestamp -- the last one to pick up work, so it has the
# most remaining job time at the moment depth first reaches 2) with
# pod-deletion-cost=-1000 BEFORE the downscale decision can land, and
# cross-check its PRODUCTION terminationGracePeriodSeconds (85) even under
# the loadproof overlay.
# ---------------------------------------------------------------------------
log "STEP 9: wait for 2 Ready replicas, identify + annotate the NEWER pod as downscale victim"

waitForNReplicasReady() {
	local component="$1" n="$2" timeout_s="$3"
	local waited=0 ready_count=0
	while [ "$waited" -lt "$timeout_s" ]; do
		ready_count=$(kubectl get pod -n "$NAMESPACE" -l "app.kubernetes.io/component=${component}" \
			--field-selector=status.phase=Running -o json 2>/dev/null \
			| jq -r '[.items[] | select(.status.containerStatuses[0].ready == true)] | length' 2>/dev/null || echo "0")
		ready_count="${ready_count:-0}"
		if [ "$ready_count" -ge "$n" ]; then
			echo "$ready_count"
			return 0
		fi
		sleep 3
		waited=$((waited + 3))
	done
	echo "TIMEOUT(last=$ready_count)"
	return 1
}

READY_COUNT=$(waitForNReplicasReady pandoc-worker 2 240) || {
	echo "FAIL: SC4 -- pandoc-worker never reached 2 Ready replicas within 240s of submitting $PANDOC_DOWNSCALE_JOBS jobs (observed: $READY_COUNT)" >&2
	exit 1
}
PASS_COUNT=$((PASS_COUNT + 1))
echo "PASS: SC4 -- pandoc-worker reached $READY_COUNT Ready replicas (0->2, genuine scale-from-zero)"

PODS_JSON=$(kubectl get pod -n "$NAMESPACE" -l "app.kubernetes.io/component=pandoc-worker" \
	--field-selector=status.phase=Running -o json)
VICTIM_POD=$(echo "$PODS_JSON" | jq -r '[.items[] | select(.metadata.deletionTimestamp == null)] | sort_by(.metadata.creationTimestamp) | .[-1].metadata.name // empty')
SURVIVOR_POD=$(echo "$PODS_JSON" | jq -r '[.items[] | select(.metadata.deletionTimestamp == null)] | sort_by(.metadata.creationTimestamp) | .[0].metadata.name // empty')
assert_nonempty "$VICTIM_POD" "SC4: NEWER pod (latest creationTimestamp) identified as downscale victim"
assert_nonempty "$SURVIVOR_POD" "SC4: OLDER pod (earliest creationTimestamp) identified as expected survivor"

# Annotate the victim pod IMMEDIATELY -- must land BEFORE the ReplicaSet
# controller's 2->1 deletion decision. Best-effort influence on victim
# selection; the downscale itself remains a genuine KEDA/HPA event driven
# solely by the queue-depth metric -- no manual pod-deletion or
# directly-forced replica-count command is ever issued here.
kubectl annotate pod "$VICTIM_POD" -n "$NAMESPACE" \
	controller.kubernetes.io/pod-deletion-cost=-1000 --overwrite
PASS_COUNT=$((PASS_COUNT + 1))
echo "PASS: SC4 -- pod-deletion-cost=-1000 annotated on victim pod $VICTIM_POD (before downscale decision)"

POD_GRACE=$(kubectl get pod "$VICTIM_POD" -n "$NAMESPACE" -o jsonpath='{.spec.terminationGracePeriodSeconds}' 2>/dev/null || true)
assert_eq "85" "$POD_GRACE" "SC4: victim pod $VICTIM_POD spec.terminationGracePeriodSeconds is the PRODUCTION value (85) even under the loadproof overlay"

# ---------------------------------------------------------------------------
# STEP 10: start TWO background watchers against the victim pod: (a) a
# continuous watch-based terminal-container-state capture (mirrors
# scripts/keda-ocr-downscale-survival.sh's snapshotLoop) and (b) the
# attribution poller (every 2s, /proc/[0-9]*/cmdline against the victim
# pod). Both append to the SAME SC4 evidence file, distinguished by line
# shape (attribution lines carry "victim_inflight_job=", snapshot lines
# carry "term_finished=").
# ---------------------------------------------------------------------------
log "STEP 10: start pod-state watcher + /proc attribution poller on victim pod $VICTIM_POD"

SC4_FILE="$EVIDENCE_DIR/sc4-pandoc-downscale-survival-${RUN_TS}.txt"
{
	echo "# Phase 50 pandoc downscale-survival evidence -- run $RUN_TS"
	echo "# pandoc_downscale_jobs=$PANDOC_DOWNSCALE_JOBS batch_job_ids=${BATCH_JOB_IDS[*]}"
	echo "# victim_pod=$VICTIM_POD survivor_pod=$SURVIVOR_POD"
	echo "# expected_termination_grace_period_seconds=85 observed_pod_spec_grace=$POD_GRACE"
	echo "# long_fixture_bytes=$LONG_FIXTURE_BYTES"
} >"$SC4_FILE"

snapshotLoop() {
	while true; do
		kubectl get pod "$VICTIM_POD" -n "$NAMESPACE" -w --output-watch-events \
			-o jsonpath='{.type}{" phase="}{.object.status.phase}{" term_reason="}{.object.status.containerStatuses[0].state.terminated.reason}{" term_exit="}{.object.status.containerStatuses[0].state.terminated.exitCode}{" term_finished="}{.object.status.containerStatuses[0].state.terminated.finishedAt}{"\n"}' 2>/dev/null \
			| while IFS= read -r watch_line; do
				echo "read_ts=$(date -u +"%Y-%m-%dT%H:%M:%SZ") pod=${VICTIM_POD} ${watch_line}" >>"$SC4_FILE"
			done
		if ! kubectl get pod "$VICTIM_POD" -n "$NAMESPACE" >/dev/null 2>&1; then
			break
		fi
		sleep 1
	done
}

# attributionPoller -- every 2s, dump the victim pod's own /proc/<pid>/cmdline
# entries (NUL-delimited argv, translated to newlines), extract the first
# /tmp/octoconv-<uuid>-<rand>/ path component the worker's own
# os.MkdirTemp("", "octoconv-"+job.ID.String()+"-") writes
# (internal/worker/worker.go ~line 1793), and reduce it to the bare 36-char
# job UUID. Never guesses -- an empty sample simply records an empty
# victim_inflight_job for that tick.
attributionPoller() {
	while true; do
		CMDLINE_DUMP=$(kubectl exec -n "$NAMESPACE" "$VICTIM_POD" -- sh -c 'cat /proc/[0-9]*/cmdline 2>/dev/null | tr "\0" "\n"' 2>/dev/null || true)
		JOB_UUID=$(echo "$CMDLINE_DUMP" | grep -oE '/tmp/octoconv-[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}-' | head -1 | sed -E 's#^/tmp/octoconv-##; s#-$##')
		echo "read_ts=$(date -u +"%Y-%m-%dT%H:%M:%SZ") victim_inflight_job=${JOB_UUID}" >>"$SC4_FILE"
		sleep 2
	done
}

# WR-04/29-REVIEW WR-01: enable job control in the PARENT (`set -m`) BEFORE
# backgrounding each watcher so it becomes its own process-group leader.
# `kill -- -PID` (here and in teardown()) then kills the whole group, not
# just the subshell, so a reparented pipeline cannot survive the EXIT trap.
set -m
snapshotLoop &
SNAPSHOT_PID=$!
attributionPoller &
POLLER_PID=$!
set +m
echo "pod-state watcher started (pid=$SNAPSHOT_PID), attribution poller started (pid=$POLLER_PID), both own process groups"

# ---------------------------------------------------------------------------
# STEP 11: do nothing but observe. As the batch drains, depth falls
# N -> ... -> 2 (both pods active on one job each, nothing pending) ->
# desired becomes 1 -- with stabilizationWindowSeconds:15, pollingInterval:
# 3, and prometheus.scrapeInterval:5s from the loadproof overlay, the HPA
# issues a genuine 2->1 downscale while BOTH pods are still mid-conversion.
# No manual pod-deletion or directly-forced replica-count command is ever
# issued here -- bounded 180s from the 2-replica observation.
# ---------------------------------------------------------------------------
log "STEP 11: observe depth drain + genuine 2->1 downscale (bounded 180s, metric-driven only)"

{
	echo ""
	echo "# queue_depth_history (batch job status counts polled during the observation window)"
} >>"$SC4_FILE"

DOWNSCALE_WINDOW=180
waited=0
REPLICAS_NOW="$READY_COUNT"
while [ "$waited" -lt "$DOWNSCALE_WINDOW" ]; do
	REPLICAS_NOW=$(kubectl get deployment pandoc-worker -n "$NAMESPACE" -o jsonpath='{.status.replicas}' 2>/dev/null || echo "0")
	REPLICAS_NOW="${REPLICAS_NOW:-0}"
	DEPTH_NOW=$(batchDepth)
	echo "depth_ts=$(date -u +"%Y-%m-%dT%H:%M:%SZ") replicas=${REPLICAS_NOW} batch_queue_depth=${DEPTH_NOW}" >>"$SC4_FILE"
	if [ "$REPLICAS_NOW" -le 1 ]; then
		break
	fi
	sleep 3
	waited=$((waited + 3))
done
if [ "$REPLICAS_NOW" -gt 1 ]; then
	echo "FAIL: SC4 -- pandoc-worker never downscaled 2->1 within ${DOWNSCALE_WINDOW}s (observed replica/depth history: see $SC4_FILE)" >&2
	exit 1
fi
PASS_COUNT=$((PASS_COUNT + 1))
echo "PASS: SC4 -- pandoc-worker downscaled 2->${REPLICAS_NOW} (genuine HPA event, no manual scale)"

# ---------------------------------------------------------------------------
# STEP 12: resolve INFLIGHT_JOB_ID -- the last non-empty
# victim_inflight_job sample recorded at-or-before the kubelet's Killing
# event timestamp for the victim pod. An all-empty sample set is a hard
# FAIL with the poller transcript and a re-run diagnostic (never a
# silently-guessed job id).
# ---------------------------------------------------------------------------
log "STEP 12: resolve INFLIGHT_JOB_ID from the /proc attribution poller"

SIGTERM_TS=$(kubectl get events -n "$NAMESPACE" \
	--field-selector involvedObject.name="$VICTIM_POD",reason=Killing \
	-o jsonpath='{.items[0].firstTimestamp}' 2>/dev/null || true)
assert_nonempty "$SIGTERM_TS" "SC4: kubelet Killing event SIGTERM timestamp captured for $VICTIM_POD"
SIGTERM_EPOCH=$(toEpoch "$SIGTERM_TS")

INFLIGHT_JOB_ID=""
while IFS= read -r line; do
	READ_TS=$(echo "$line" | sed -n 's/^read_ts=\([^ ]*\).*/\1/p')
	VJOB=$(echo "$line" | sed -n 's/.*victim_inflight_job=\(.*\)$/\1/p')
	[ -z "$READ_TS" ] && continue
	READ_EPOCH=$(toEpoch "$READ_TS")
	if [ "$READ_EPOCH" -gt 0 ] && [ "$READ_EPOCH" -le "$SIGTERM_EPOCH" ] && [ -n "$VJOB" ]; then
		INFLIGHT_JOB_ID="$VJOB"
	fi
done < <(grep '^read_ts=.*victim_inflight_job=' "$SC4_FILE" || true)

if [ -z "$INFLIGHT_JOB_ID" ]; then
	echo "FAIL: SC4 -- the attribution poller recorded NO victim_inflight_job at or before the Killing event ($SIGTERM_TS) -- the victim pod $VICTIM_POD was idle at SIGTERM time. This run must be repeated with a larger PANDOC_DOWNSCALE_JOBS (current: $PANDOC_DOWNSCALE_JOBS) for more headroom. Poller transcript: $SC4_FILE" >&2
	exit 1
fi
assert_nonempty "$INFLIGHT_JOB_ID" "SC4: INFLIGHT_JOB_ID resolved deterministically from /proc attribution (last sample at-or-before the Killing event)"

# ---------------------------------------------------------------------------
# STEP 13: TRIPLE-CHECK (mirrors scripts/keda-ocr-downscale-survival.sh's
# SC4 triple-check exactly) -- the attributed in-flight job survived the
# downscale gracefully (no exit-137/SIGKILL), plus batch completeness.
# ---------------------------------------------------------------------------
log "STEP 13: triple-check -- INFLIGHT_JOB_ID survived the downscale gracefully (no exit-137/SIGKILL)"

# (1) in-flight job reaches done.
echo "waiting for INFLIGHT_JOB_ID=$INFLIGHT_JOB_ID to reach a terminal status (bounded 180s, comfortably over PANDOC_ENGINE_TIMEOUT=65s)..."
inflight_final_status=""
for i in $(seq 1 60); do
	inflight_final_status=$(jobStatus "$INFLIGHT_JOB_ID")
	if [ "$inflight_final_status" = "done" ] || [ "$inflight_final_status" = "failed" ]; then
		break
	fi
	sleep 3
done
assert_eq "done" "$inflight_final_status" "SC4(1): in-flight job $INFLIGHT_JOB_ID reaches done despite the downscale"

# (2) exactly one queued->active transition (no false retry caused by a
# premature SIGKILL forcing asynq to redeliver the task).
QUEUED_TO_ACTIVE_COUNT=$(psql "postgres://octo:octo-pass@127.0.0.1:${DB_LOCAL_PORT}/octo_db" -tAc \
	"SELECT count(*) FROM job_events WHERE job_id='${INFLIGHT_JOB_ID}' AND from_status='queued' AND to_status='active';" | tr -d '[:space:]')
assert_eq "1" "$QUEUED_TO_ACTIVE_COUNT" "SC4(2): exactly one queued->active transition for $INFLIGHT_JOB_ID (no false retry)"

# (3) SIGTERM timestamp already captured above (kubelet Killing event, NEVER
# the pod's own deletion-deadline field); pod exit timestamp/exit-code from
# the continuously-captured snapshot (or a final live read if the pod
# object still exists), and jobs.finished_at via psql -- prove SIGTERM
# occurred BEFORE completion AND the container exited gracefully.
echo "waiting for the pod-state watcher to capture $VICTIM_POD's terminal state (bounded 90s)..."
waited=0
while [ "$waited" -lt 90 ]; do
	if grep '^read_ts=.*term_finished=' "$SC4_FILE" 2>/dev/null | grep -qv 'term_finished=$'; then
		break
	fi
	if ! kubectl get pod "$VICTIM_POD" -n "$NAMESPACE" >/dev/null 2>&1; then
		sleep 3
		break
	fi
	sleep 3
	waited=$((waited + 3))
done
echo "termination capture: snapshot lines=$(grep -c '^read_ts=.*term_finished=' "$SC4_FILE" 2>/dev/null || echo 0), attribution lines=$(grep -c '^read_ts=.*victim_inflight_job=' "$SC4_FILE" 2>/dev/null || echo 0)"

kill -- -"$SNAPSHOT_PID" >/dev/null 2>&1 || true
wait "$SNAPSHOT_PID" 2>/dev/null || true
kill -- -"$POLLER_PID" >/dev/null 2>&1 || true
wait "$POLLER_PID" 2>/dev/null || true
pkill -f "kubectl get pod ${VICTIM_POD} .* -w" >/dev/null 2>&1 || true
pkill -f "kubectl exec -n ${NAMESPACE} ${VICTIM_POD}" >/dev/null 2>&1 || true
SNAPSHOT_PID=""
POLLER_PID=""

POD_TERM_REASON=$(kubectl get pod "$VICTIM_POD" -n "$NAMESPACE" -o jsonpath='{.status.containerStatuses[0].state.terminated.reason}' 2>/dev/null || true)
POD_TERM_EXIT=$(kubectl get pod "$VICTIM_POD" -n "$NAMESPACE" -o jsonpath='{.status.containerStatuses[0].state.terminated.exitCode}' 2>/dev/null || true)
POD_TERM_FINISHED=$(kubectl get pod "$VICTIM_POD" -n "$NAMESPACE" -o jsonpath='{.status.containerStatuses[0].state.terminated.finishedAt}' 2>/dev/null || true)
if [ -z "$POD_TERM_FINISHED" ]; then
	POD_TERM_REASON=$(kubectl get pod "$VICTIM_POD" -n "$NAMESPACE" -o jsonpath='{.status.containerStatuses[0].lastState.terminated.reason}' 2>/dev/null || true)
	POD_TERM_EXIT=$(kubectl get pod "$VICTIM_POD" -n "$NAMESPACE" -o jsonpath='{.status.containerStatuses[0].lastState.terminated.exitCode}' 2>/dev/null || true)
	POD_TERM_FINISHED=$(kubectl get pod "$VICTIM_POD" -n "$NAMESPACE" -o jsonpath='{.status.containerStatuses[0].lastState.terminated.finishedAt}' 2>/dev/null || true)
fi
if [ -z "$POD_TERM_FINISHED" ]; then
	# Pod object already removed -- fall back to the watch-captured snapshot
	# file, taking the last non-empty term_finished line written by
	# snapshotLoop.
	POD_TERM_FINISHED=$(grep '^read_ts=.*term_finished=' "$SC4_FILE" | grep -v 'term_finished=$' | tail -1 | sed -n 's/.*term_finished=\([^ ]*\).*/\1/p' || true)
	POD_TERM_REASON=$(grep '^read_ts=.*term_finished=' "$SC4_FILE" | grep -v 'term_finished=$' | tail -1 | sed -n 's/.*term_reason=\([^ ]*\).*/\1/p' || true)
	POD_TERM_EXIT=$(grep '^read_ts=.*term_finished=' "$SC4_FILE" | grep -v 'term_finished=$' | tail -1 | sed -n 's/.*term_exit=\([^ ]*\).*/\1/p' || true)
fi
assert_nonempty "$POD_TERM_FINISHED" "SC4(3): pod $VICTIM_POD terminated.finishedAt captured (live or via continuous snapshot)"

JOB_FINISHED_AT=$(psql "postgres://octo:octo-pass@127.0.0.1:${DB_LOCAL_PORT}/octo_db" -tAc \
	"SELECT finished_at FROM jobs WHERE id='${INFLIGHT_JOB_ID}';" | tr -d '[:space:]')
assert_nonempty "$JOB_FINISHED_AT" "SC4(3): jobs.finished_at captured for in-flight job $INFLIGHT_JOB_ID"

{
	echo ""
	echo "# SC4 triple-check raw evidence"
	echo "victim_pod=$VICTIM_POD"
	echo "survivor_pod=$SURVIVOR_POD"
	echo "victim_inflight_job=$INFLIGHT_JOB_ID"
	echo "observed_pod_spec_grace=$POD_GRACE"
	echo "long_fixture_bytes=$LONG_FIXTURE_BYTES"
	echo "sigterm_killing_event_ts=$SIGTERM_TS"
	echo "pod_terminated_reason=$POD_TERM_REASON"
	echo "pod_terminated_exit_code=$POD_TERM_EXIT"
	echo "pod_terminated_finished_at=$POD_TERM_FINISHED"
	echo "job_finished_at=$JOB_FINISHED_AT"
	echo "queued_to_active_count=$QUEUED_TO_ACTIVE_COUNT"
} >>"$SC4_FILE"

# ASSERT: SIGTERM occurred before job completion, and the container exited
# gracefully (Completed/exit 0), NEVER exit-137/SIGKILL/OOMKilled.
JOB_FINISHED_EPOCH=$(date -u -j -f "%Y-%m-%d %H:%M:%S" "${JOB_FINISHED_AT%%.*}" +%s 2>/dev/null || date -u -d "$JOB_FINISHED_AT" +%s 2>/dev/null || echo "0")
if [ "${SIGTERM_EPOCH:-0}" -le 0 ] || [ "${JOB_FINISHED_EPOCH:-0}" -le 0 ]; then
	echo "FAIL: SC4(3) -- could not parse SIGTERM_TS ($SIGTERM_TS) or JOB_FINISHED_AT ($JOB_FINISHED_AT) into comparable epochs" >&2
	exit 1
fi
if [ "$SIGTERM_EPOCH" -ge "$JOB_FINISHED_EPOCH" ]; then
	echo "FAIL: SC4(3) -- SIGTERM ($SIGTERM_TS) did not occur before job completion ($JOB_FINISHED_AT); the downscale-survives-in-flight-job proof does not hold" >&2
	exit 1
fi
PASS_COUNT=$((PASS_COUNT + 1))
echo "PASS: SC4(3) -- SIGTERM ($SIGTERM_TS) occurred BEFORE job completion ($JOB_FINISHED_AT)"

if [ "$POD_TERM_EXIT" = "137" ]; then
	echo "FAIL: SC4(3) -- pod $VICTIM_POD terminated with exit 137 (SIGKILL) -- the downscale did NOT survive gracefully; terminationGracePeriodSeconds=85 was not enough (or was not honored)" >&2
	exit 1
fi
if [ "$POD_TERM_REASON" != "Completed" ] && [ "$POD_TERM_EXIT" != "0" ]; then
	echo "FAIL: SC4(3) -- pod $VICTIM_POD did not terminate gracefully (reason=$POD_TERM_REASON exit=$POD_TERM_EXIT); expected Completed/exit 0, not exit 137/SIGKILL/OOMKilled" >&2
	exit 1
fi
PASS_COUNT=$((PASS_COUNT + 1))
echo "PASS: SC4(3) -- pod $VICTIM_POD terminated gracefully (reason=$POD_TERM_REASON exit=$POD_TERM_EXIT, NOT 137/SIGKILL), well before terminationGracePeriodSeconds=85"

# (4) every OTHER job in the batch also reaches done -- no collateral loss
# on the surviving pod.
log "STEP 13b: confirm every batch job reaches done (no collateral loss on the surviving pod)"
for jid in "${BATCH_JOB_IDS[@]}"; do
	bstatus=""
	for i in $(seq 1 100); do
		bstatus=$(jobStatus "$jid")
		if [ "$bstatus" = "done" ] || [ "$bstatus" = "failed" ]; then
			break
		fi
		sleep 3
	done
	assert_eq "done" "$bstatus" "SC4(batch): job $jid reaches done"
done

echo "PASS: SC4 evidence -- $SC4_FILE"

rm -rf "$WORKDIR"

# =============================================================================
# ALL-PASSED summary -- set only after every assertion above has passed.
# Teardown runs unconditionally via the EXIT trap.
# =============================================================================
GATE_OK="1"
echo ""
echo "=== ALL $PASS_COUNT ASSERTIONS PASSED ==="
echo "SC4-pandoc (PDCP-02): ~4 MiB html->md job $INFLIGHT_JOB_ID survived a genuine KEDA/HPA 2->1 downscale via deterministic /proc-based victim attribution -- exactly one queued->active transition, graceful SIGTERM-before-completion exit (never exit-137/SIGKILL), validating terminationGracePeriodSeconds=85 live, all $PANDOC_DOWNSCALE_JOBS batch jobs completed -- PASS"
echo "Evidence: $SC4_FILE, $LOG_FILE"
