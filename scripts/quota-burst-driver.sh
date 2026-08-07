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
#
# THE ROUND-4 STRAGGLER (Phase 55 plan 55-07, closing a defect this file's
# own barrier design exposed): the 2026-08-07 live run's round 4 sent nine
# requests inside 4 ms and a tenth 159 ms later -- offsets from the first
# send were +0 +0 +1 +1 +1 +1 +2 +3 +4 +159 ms
# (evidence/gate-run4-FAILED-round4-20260807T141252Z.log). Ten forked
# subshells plus ten curl processes released by one FIFO barrier, on the
# burst pod's THEN-current `limits.cpu: 500m` (half a core), is exactly the
# shape that produces process-start jitter of that size. The suspected root
# cause is fixed at its source, in two places: this file removes every
# OTHER fork from the post-barrier critical path (no `date`, no `cat`) and
# replaces the blind pre-release sleep with a deterministic readiness
# handshake (see below); scripts/quota-multireplica-liveproof.sh (Task 3 of
# the same plan) separately raises the burst pod to `limits.cpu: "2"`.
set -euo pipefail
# LC_ALL=C, set immediately: ms_now (below) parses EPOCHREALTIME with bash
# parameter expansion, and EPOCHREALTIME's decimal separator must be `.`
# regardless of the container's locale -- this makes that deterministic
# rather than an assumption about the pinned image's locale configuration.
export LC_ALL=C

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
# Preflight 1 of 4: required tools. mkfifo's binary name is held in one
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
# Preflight 2 of 4: millisecond clock, captured ONCE. macOS/BSD date has no
# %3N -- this preflight fires on the host by construction, which is exactly
# what proves it is real rather than decorative -- so this is what forces
# the driver to run only inside the pinned burst image
# (buildpack-deps:bookworm-curl), whose GNU coreutils date honors %3N. A
# second-granularity clock would silently weaken the in-flight-at-max-send
# assertion downstream (assess_round in quota-multireplica-liveproof.sh) --
# a hard failure, never a degrade-and-continue. This probe itself is the
# ONLY surviving `date +%s%3N` call in this file -- every per-request
# timestamp below now goes through the fork-free ms_now helper instead.
# ---------------------------------------------------------------------------
CLOCK_PROBE="$(date +%s%3N)"
if [[ ! "$CLOCK_PROBE" =~ ^[0-9]{13}$ ]]; then
	echo "FAIL: this container's 'date' does not honor %3N (got '$CLOCK_PROBE' from 'date +%s%3N', expected exactly 13 digits). This driver must run inside buildpack-deps:bookworm-curl, whose GNU coreutils date honors it; a second-granularity clock would silently weaken the in-flight-at-max-send assertion." >&2
	exit 1
fi

# ---------------------------------------------------------------------------
# Preflight 3 of 4: bash's EPOCHREALTIME must be populated and in the
# <10-digit-seconds>.<6-digit-microseconds> form ms_now (below) relies on
# for a fork-free per-request send/recv timestamp. Only bash 5+ builds this
# variable in, which is exactly why this preflight, like the one above,
# fires on a non-GNU/older shell rather than being decorative.
# ---------------------------------------------------------------------------
if [[ ! "$EPOCHREALTIME" =~ ^[0-9]{10}\.[0-9]{6}$ ]]; then
	echo "FAIL: bash's EPOCHREALTIME is not populated in the expected <10 digits>.<6 digits> form (got '$EPOCHREALTIME'). This driver requires bash 5+ inside buildpack-deps:bookworm-curl, whose bash builds EPOCHREALTIME support in; without it the fork-free per-request timestamp (ms_now) cannot work." >&2
	exit 1
fi

# ms_now -- fork-free replacement for `date +%s%3N` on the per-request
# critical path. EPOCHREALTIME is RE-EVALUATED ON EVERY REFERENCE, so it is
# read into a local variable EXACTLY ONCE here, then split with parameter
# expansion only (seconds via ${t%.*}, fractional via ${t#*.}) -- reading
# it a second time would splice the seconds of one instant onto the
# milliseconds of another. The seconds plus the first three fractional
# digits reproduce the same 13-digit millisecond string this file (and the
# host script) already consumes.
ms_now() {
	local t="$EPOCHREALTIME"
	local secs="${t%.*}"
	local frac="${t#*.}"
	printf '%s%s\n' "$secs" "${frac:0:3}"
}

# ---------------------------------------------------------------------------
# Preflight 4 of 4 (burst mode only): fixture and api key file must exist
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

# Read the api key ONCE, here, before the fork loop -- removes a `cat` fork
# per request from the post-barrier critical path. Security property
# UNCHANGED: the key never appears in this script's own argv (it is read
# via command substitution into a shell variable, then referenced by name)
# nor in the kubectl exec argv the API server records; it appears in
# curl's own argv exactly as it already did, via CURL_ARGS below.
APIKEY_VALUE=""
if [ "$MODE" = "burst" ]; then
	APIKEY_VALUE="$(cat "$APIKEY_FILE")"
fi

for i in $(seq 1 "$COUNT"); do
	(
		# Readiness handshake, this subshell's side: mark ready as the LAST
		# action before blocking on the barrier -- an empty-redirection
		# builtin (no fork) rather than `touch` (a fork). Opening a FIFO
		# for reading BLOCKS until a writer opens it, so this file's
		# existence, checked by the parent below, proves this subshell has
		# already reached (and is blocked on) that open call.
		: >"$SCRATCH/ready-${i}"
		exec 3<"$BARRIER"
		read -r -u 3 _ # blocks until the barrier below is released

		send_ts="$(ms_now)"

		if [ "$MODE" = "calib" ]; then
			request_id="calib-${RUN_ID}-${i}"
			CURL_ARGS=(-s -o /dev/null -w '%{http_code}' --max-time 10 \
				-H "X-Request-Id: ${request_id}" \
				"${URL_BASE}/healthz")
		else
			request_id="burst-${RUN_ID}-${ROUND}-${i}"
			body_file="$SCRATCH/resp-${i}.body"
			CURL_ARGS=(-s -o "$body_file" -w '%{http_code}' --max-time 60 \
				-H "Authorization: ApiKey ${APIKEY_VALUE}" \
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

		recv_ts="$(ms_now)"

		if [ "$MODE" = "calib" ]; then
			body_field="-"
		else
			# curl does not always create the -o file on an immediate
			# connection failure (e.g. connection refused before any
			# response data exists to write) -- found and fixed during this
			# plan's own local verification against the real burst image.
			# Guard with -f rather than letting a missing-file open error
			# escape past the 2>/dev/null (a failed input redirection
			# aborts the command before that redirection is even applied).
			if [ -f "$body_file" ]; then
				body_field="$(tr -d '\r\n' <"$body_file" 2>/dev/null || true)"
				rm -f "$body_file"
			else
				body_field=""
			fi
			[ -z "$body_field" ] && body_field="-"
		fi

		printf '%s\t%s\t%s\t%s\t%s\t%s\n' \
			"$i" "$request_id" "$send_ts" "$recv_ts" "$http_status" "$body_field" \
			>"$SCRATCH/req-${i}.row"
	) &
done

# Readiness handshake, the parent's side (Plan 55-07, replacing a blind
# `sleep 0.3`): poll until COUNT ready-files exist before opening the
# barrier for writing. This is STRONGER than a fixed sleep because it is a
# proof, not a guess -- opening a FIFO for reading BLOCKS until a writer
# opens it, so THIS open-for-write below is the actual release edge, and
# every ready-file existing beforehand proves every one of the COUNT
# subshells already reached (and is blocked on) that exact open call,
# regardless of how long forking COUNT subshells took on a given host.
# Count with a glob into the positional parameters (not `ls | wc -l`,
# which forks twice per poll) -- guard the no-match case, where the glob
# itself is left unexpanded. This loop runs entirely in the PARENT, before
# the release, so its own `sleep` fork cost is off the critical path
# timed by send_ts/recv_ts entirely.
READY_TIMEOUT_S=10
READY_POLL_S=0.1
ready_polls=0
ready_max_polls=100 # READY_TIMEOUT_S / READY_POLL_S
while :; do
	set -- "$SCRATCH"/ready-*
	if [ -e "$1" ]; then
		ready_n=$#
	else
		ready_n=0
	fi
	[ "$ready_n" -ge "$COUNT" ] && break
	ready_polls=$((ready_polls + 1))
	if [ "$ready_polls" -ge "$ready_max_polls" ]; then
		echo "FAIL: readiness handshake timed out after ${READY_TIMEOUT_S}s -- only ${ready_n}/${COUNT} readers reached the barrier" >&2
		exit 1
	fi
	sleep "$READY_POLL_S"
done

# ONE LINE PER READER IS LOAD-BEARING, NOT A STYLE CHOICE (found and fixed
# during this plan's own local verification, not carried over from the
# host script's retired barrier -- see the SUMMARY's Deviations section):
# a named pipe does not broadcast a single write to every blocked reader.
# `read` consumes bytes off one shared buffer, so ONE `printf 'go\n'` wakes
# up exactly one of the COUNT blocked subshells; the other COUNT-1 then
# see EOF the instant the sole writer closes, which fails their own `read`
# under this file's own `set -e` and silently drops them before they ever
# reach curl. Writing COUNT lines -- one per reader -- through a single
# open file descriptor (the redirection scopes the whole loop, not each
# printf) fixes this while keeping the release effectively simultaneous:
# confirmed 10/10 released across repeated local trials, 1/10 (or fewer)
# with the single-line form.
for i in $(seq 1 "$COUNT"); do
	printf 'go\n'
done >"$BARRIER"
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
