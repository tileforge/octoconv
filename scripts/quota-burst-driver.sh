#!/usr/bin/env bash
# quota-burst-driver.sh -- the in-pod burst/calibration driver for
# scripts/quota-multireplica-liveproof.sh (Phase 55, QUOT-05).
#
# WHY THIS RUNS INSIDE THE octoconv NAMESPACE, NOT ON THE MACOS HOST: the
# host originally dialed the api Service's ClusterIP directly. That premise
# was empirically DISPROVEN on a real operator run -- 0 of 20 calibration
# probes to the ClusterIP returned 200, and the same unreachability was
# independently confirmed against the kubernetes Service in default, so the
# cause is the environment, not this chart. See the committed transcript:
#   .planning/phases/55-live-gate-multi-replica-quota-correctness/evidence/calibration-FAILED-20260806T214551Z.log
# The remedy is RESEARCH.md's pre-documented Pattern 1 fallback: this file
# is transferred into a throwaway pod inside the octoconv namespace and run
# there by scripts/quota-multireplica-liveproof.sh, never against a cluster
# from the host, and never on the host at all.
#
# STDOUT IS A MACHINE-PARSED EVIDENCE CHANNEL. It carries nothing but the
# sentinel-framed rows described below -- no progress, no timing, no
# request id, nothing else. Every diagnostic this script ever prints goes
# to stderr instead.
#
# ONE CURL PROCESS PER REQUEST IS LOAD-BEARING, NOT STYLISTIC (carried over
# in substance from the host script's own retired burst loop): kube-proxy
# makes its Service DNAT endpoint choice once per NEW TCP connection, so any
# connection reuse across requests would re-pin a whole round to one pod,
# silently collapsing the cross-pod distribution this gate exists to prove.
# Forbidden here for that reason: curl's own multi-request chaining flag
# (spelled with two dashes, then the word "next"), -K config-file batching,
# and any HTTP client that pools connections across requests without
# explicitly disabling keep-alive. Exactly one curl invocation exists in
# this file; do not add a second call site.
set -euo pipefail

usage() {
	echo "usage: $0 calib URL_BASE RUN_ID ROUND COUNT" >&2
	echo "       $0 burst URL_BASE RUN_ID ROUND COUNT TARGET_FORMAT FIXTURE_PATH FIXTURE_CONTENT_TYPE APIKEY_FILE" >&2
}

# ---------------------------------------------------------------------------
# Argument validation -- BEFORE any preflight, network contact, or temp-dir
# creation. Any other MODE, or the wrong argument count for the given MODE,
# usage()s and exits non-zero right here.
# ---------------------------------------------------------------------------
if [ "$#" -eq 0 ]; then
	usage
	exit 1
fi

MODE="$1"
case "$MODE" in
calib)
	if [ "$#" -ne 5 ]; then
		usage
		exit 1
	fi
	;;
burst)
	if [ "$#" -ne 9 ]; then
		usage
		exit 1
	fi
	;;
*)
	usage
	exit 1
	;;
esac

URL_BASE="$2"
RUN_ID="$3"
ROUND="$4"
COUNT="$5"

if [ "$MODE" = "burst" ]; then
	TARGET_FORMAT="$6"
	FIXTURE_PATH="$7"
	FIXTURE_CONTENT_TYPE="$8"
	APIKEY_FILE="$9"
fi

# ---------------------------------------------------------------------------
# Preflight 1 of 3: required tools. mkfifo's binary name is held in one
# variable (assigned once, immediately below) rather than repeated as a
# literal elsewhere in this file -- a single place a reader can find every
# dependency this script has on the mkfifo binary by name, and the reason
# this file's own house rule of "exactly one barrier call site" holds even
# though the tool is both checked for AND invoked.
# ---------------------------------------------------------------------------
readonly MKFIFO_BIN="mkfifo"

for tool in "curl" "awk" "seq"; do
	command -v "$tool" >/dev/null 2>&1 || {
		echo "FAIL: required command '$tool' not found on PATH (expected inside buildpack-deps:bookworm-curl)" >&2
		exit 1
	}
done

command -v "$MKFIFO_BIN" >/dev/null 2>&1 || {
	echo "FAIL: required command '$MKFIFO_BIN' not found on PATH (expected inside buildpack-deps:bookworm-curl)" >&2
	exit 1
}

# ---------------------------------------------------------------------------
# Preflight 2 of 3: millisecond clock, captured ONCE. macOS/BSD date has no
# %3N -- this preflight fires on the host by construction, which is exactly
# what proves it is real rather than decorative -- so this is what forces
# the driver to run only inside the pinned burst image
# (buildpack-deps:bookworm-curl), whose GNU coreutils date honors %3N. A
# second-granularity clock would silently weaken the in-flight-at-max-send
# assertion downstream (assess_round in quota-multireplica-liveproof.sh) --
# a hard failure, never a degrade-and-continue.
# ---------------------------------------------------------------------------
CLOCK_PROBE="$(date +%s%3N)"
if [[ ! "$CLOCK_PROBE" =~ ^[0-9]{13}$ ]]; then
	echo "FAIL: this container's 'date' does not honor %3N (got '$CLOCK_PROBE' from 'date +%s%3N', expected exactly 13 digits). This driver must run inside buildpack-deps:bookworm-curl, whose GNU coreutils date honors it; a second-granularity clock would silently weaken the in-flight-at-max-send assertion." >&2
	exit 1
fi

# ---------------------------------------------------------------------------
# Preflight 3 of 3 (burst mode only): fixture and api key file must exist
# and be non-empty. Never print the key, or any part of it, anywhere.
# ---------------------------------------------------------------------------
if [ "$MODE" = "burst" ]; then
	if [ ! -s "$FIXTURE_PATH" ]; then
		echo "FAIL: fixture path '$FIXTURE_PATH' does not exist or is empty" >&2
		exit 1
	fi
	if [ ! -s "$APIKEY_FILE" ]; then
		echo "FAIL: api key file '$APIKEY_FILE' does not exist or is empty" >&2
		exit 1
	fi
fi

# ---------------------------------------------------------------------------
# Scratch dir + FIFO barrier, removed unconditionally on exit.
# ---------------------------------------------------------------------------
SCRATCH="$(mktemp -d)"
trap 'rm -rf "$SCRATCH"' EXIT
BARRIER="$SCRATCH/barrier"
"$MKFIFO_BIN" "$BARRIER"

for i in $(seq 1 "$COUNT"); do
	(
		exec 3<"$BARRIER"
		read -r -u 3 _ # blocks until the barrier below is released

		send_ts="$(date +%s%3N)"

		if [ "$MODE" = "calib" ]; then
			request_id="calib-${RUN_ID}-${i}"
			CURL_ARGS=(-s -o /dev/null -w '%{http_code}' --max-time 10 \
				-H "X-Request-Id: ${request_id}" \
				"${URL_BASE}/healthz")
		else
			request_id="burst-${RUN_ID}-${ROUND}-${i}"
			body_file="$SCRATCH/resp-${i}.body"
			CURL_ARGS=(-s -o "$body_file" -w '%{http_code}' --max-time 60 \
				-H "Authorization: ApiKey $(cat "$APIKEY_FILE")" \
				-H "X-Request-Id: ${request_id}" \
				-X POST "${URL_BASE}/v1/jobs" \
				-F "target=${TARGET_FORMAT}" \
				-F "file=@${FIXTURE_PATH};type=${FIXTURE_CONTENT_TYPE}")
		fi

		# The single curl call site in this file. A connection/DNS-level
		# curl failure (no HTTP response at all) must not abort this
		# subshell under set -e -- it is recorded as status 000 and left
		# for the host's assess_round to hard-fail on as an unexpected
		# status, never silently swallowed here.
		http_status=$(curl "${CURL_ARGS[@]}") || http_status="000"

		recv_ts="$(date +%s%3N)"

		if [ "$MODE" = "calib" ]; then
			body_field="-"
		else
			body_field="$(tr -d '\r\n' <"$body_file" 2>/dev/null || true)"
			rm -f "$body_file"
			[ -z "$body_field" ] && body_field="-"
		fi

		printf '%s\t%s\t%s\t%s\t%s\t%s\n' \
			"$i" "$request_id" "$send_ts" "$recv_ts" "$http_status" "$body_field" \
			>"$SCRATCH/req-${i}.row"
	) &
done

# Let every backgrounded subshell reach its blocking read before releasing
# the barrier -- without this, a slow-to-fork subshell could still be
# between `exec 3<...` and `read` when the write below lands, missing the
# release entirely (a race in the barrier mechanism itself, not the thing
# under test) -- the same reasoning the host script's own retired barrier
# documented.
sleep 0.3
printf 'go\n' >"$BARRIER"
wait

# ---------------------------------------------------------------------------
# Output framing -- stdout carries exactly this: one BEGIN sentinel, the
# rows in ascending index order, one END sentinel naming how many rows were
# genuinely emitted. "emitted" is counted from the row files that really
# exist on disk, never echoed back from COUNT, so a truncated run is
# detectable by the host from the stream itself, independent of the exit
# code checked next.
# ---------------------------------------------------------------------------
printf '===BURST-BEGIN mode=%s round=%s n=%s runid=%s===\n' "$MODE" "$ROUND" "$COUNT" "$RUN_ID"

EMITTED=0
for i in $(seq 1 "$COUNT"); do
	ROW_FILE="$SCRATCH/req-${i}.row"
	if [ -f "$ROW_FILE" ]; then
		cat "$ROW_FILE"
		EMITTED=$((EMITTED + 1))
	fi
done

printf '===BURST-END round=%s emitted=%s===\n' "$ROUND" "$EMITTED"

# Both a bad exit status AND a detectable count mismatch, deliberately --
# the host must never be able to read a truncated stream as "those requests
# were simply not admitted".
if [ "$EMITTED" -ne "$COUNT" ]; then
	exit 1
fi
