#!/usr/bin/env bash
# probe-timeout-measure.sh -- Phase 60 (SANI-06/SC5) PROBE_ENGINE_TIMEOUT
# measurement gate.
#
# WHY THIS SCRIPT EXISTS
#
# The project has four engine timeouts -- 65s, 742s, 744s, 753s -- every one
# of them measured on its own engine's own corpus, and copying any of them to
# a fifth engine is recorded as a project antipattern (PITFALLS.md Pitfall 7).
# PROBE_ENGINE_TIMEOUT must therefore come from a run, and the run has to be
# on documents representative of the operator's real load: the phase's own
# research measured 1.9-36 KB fixtures and said plainly that this is not a
# basis for a production timeout.
#
# WHAT IT TIMES
#
# The FULL pipeline -- sanitize plus the mandatory independent verification --
# by driving internal/convert's own TestProbeTimingHarness. Timing `gs` alone
# would leave out the pdfcpu parse, the unzip/libmagic walk and the report
# build, all of which the worker pays for on every job.
#
# WHERE IT TIMES IT
#
# Inside a container built from the SAME debian suite as the worker image, at
# the SAME cgroup limits the deployment sets (2 CPU / 1 GiB), for a reason
# measured rather than assumed: the developer host running this had ghostscript
# 10.07.1 while Dockerfile.probe-worker resolves to 10.00.0. A number produced
# against a different ghostscript, with different CPU and memory, is a number
# about a machine nobody runs. This is the same discipline
# scripts/pandoc-timing-measure.sh and scripts/ocr-timing-measure.sh already
# apply by running inside their own engine images.
#
# OCTOCONV_PROBE_LOCAL=1 runs on the host instead. Useful for a quick sanity
# check of the corpus; the resulting number is NOT a basis for the deployed
# timeout, and the output says so.
#
# FAIL-CLOSED ON THE CORPUS
#
# If any file fails to sanitize or verify, the run FAILS and prints no
# recommendation. A timeout measured on a corpus that partly does not sanitize
# is a number about the files that happened to work.
#
# USAGE
#
#   scripts/probe-timeout-measure.sh <corpus-dir> [output-file]
#
# The output file (default: evidence/probe-timeout-<date>.txt under the phase
# directory, or ./probe-timeout-measurement.txt) is meant to be COMMITTED
# alongside the number it justifies. Phase 59 committed its corpus next to its
# goldens for the same reason: six months from now, "measured" and "copied"
# are indistinguishable unless the measurement is in the repository.

set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: $0 <corpus-dir> [output-file]" >&2
  exit 64
fi

CORPUS="$1"
OUT="${2:-probe-timeout-measurement.txt}"

if [[ ! -d "$CORPUS" ]]; then
  echo "error: $CORPUS is not a directory" >&2
  exit 64
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

LOCAL="${OCTOCONV_PROBE_LOCAL:-0}"
RAW="$(mktemp)"
trap 'rm -f "$RAW"' EXIT

if [[ "$LOCAL" == "1" ]]; then
  # The verifiers are external binaries. Missing ones would turn every file
  # into an "unverifiable" failure, which would look like a corpus problem.
  for bin in gs unzip file; do
    if ! command -v "$bin" >/dev/null 2>&1; then
      echo "error: $bin is required by the sanitize pipeline and is not on PATH" >&2
      exit 69
    fi
  done
  GS_VERSION="$(gs --version 2>/dev/null || echo unknown)"
  WHERE="HOST (OCTOCONV_PROBE_LOCAL=1) -- NOT a basis for the deployed timeout"
  echo "corpus:   $CORPUS"
  echo "where:    $WHERE"
  echo "gs:       $GS_VERSION"
  echo
  set +e
  OCTOCONV_PROBE_CORPUS="$CORPUS" \
    go test ./internal/convert/ -run TestProbeTimingHarness -count=1 -v -timeout 60m >"$RAW" 2>&1
  HARNESS_STATUS=$?
  set -e
else
  if ! command -v docker >/dev/null 2>&1; then
    echo "error: docker is required (or set OCTOCONV_PROBE_LOCAL=1 to measure on the host," >&2
    echo "       accepting that the result describes the host's ghostscript, not the image's)" >&2
    exit 69
  fi
  echo "building the measurement image (same debian suite as the worker image)..."
  docker build -f Dockerfile.probe-measure -t octoconv-probe-measure:dev . >/dev/null

  GS_VERSION="$(docker run --rm octoconv-probe-measure:dev gs --version 2>/dev/null || echo unknown)"
  WHERE="container, --cpus 2.0 --memory 1g (the deployed limits)"
  echo "corpus:   $CORPUS"
  echo "where:    $WHERE"
  echo "gs:       $GS_VERSION"
  echo

  set +e
  docker run --rm \
    --cpus 2.0 --memory 1g \
    -v "$REPO_ROOT:/src:ro" \
    -v "$CORPUS:/corpus:ro" \
    -e OCTOCONV_PROBE_CORPUS=/corpus \
    -e GOFLAGS=-mod=mod \
    -e GOCACHE=/tmp/gocache \
    -e GOMODCACHE=/tmp/gomodcache \
    octoconv-probe-measure:dev \
    go test ./internal/convert/ -run TestProbeTimingHarness -count=1 -v -timeout 60m >"$RAW" 2>&1
  HARNESS_STATUS=$?
  set -e
fi

grep -E '^(ROW|SKIP|SUMMARY),' "$RAW" || true

if [[ $HARNESS_STATUS -ne 0 ]]; then
  echo
  echo "MEASUREMENT FAILED: at least one file did not sanitize or did not verify." >&2
  echo "No recommendation is printed. A timeout measured on a corpus that partly" >&2
  echo "does not sanitize is a number about the files that happened to work." >&2
  echo >&2
  grep -E '^\s+probetiming_test\.go' "$RAW" >&2 || true
  exit 1
fi

# --- distribution -----------------------------------------------------------
#
# Nearest-rank p95, the same definition every other timing script in this
# repository uses (audio/av/ocr/pandoc), so the four numbers stay comparable.

TIMES="$(grep -E '^ROW,' "$RAW" | awk -F, '{print $7}' | sort -n)"
N="$(printf '%s\n' "$TIMES" | grep -c . || true)"

if [[ "$N" -eq 0 ]]; then
  echo "error: no timed rows" >&2
  exit 1
fi

pick() { printf '%s\n' "$TIMES" | sed -n "${1}p"; }

MIN="$(pick 1)"
MAX="$(pick "$N")"
MED="$(pick $(( (N + 1) / 2 )))"
P95_RANK=$(( (N * 95 + 99) / 100 ))   # nearest-rank, ceil(N*0.95)
[[ $P95_RANK -lt 1 ]] && P95_RANK=1
P95="$(pick "$P95_RANK")"

# --- recommendation ---------------------------------------------------------
#
# The formula is PRINTED, not just its result. The operator has to be able to
# disagree with it: this script does not know what share of timeout failures
# is acceptable for their load, and a bare number invites being accepted
# without that judgement being made at all.
#
# x3 rather than pandoc's x2 for one stated reason: the sanitize pipeline runs
# TWO ghostscript passes plus a parse, so its tail is longer relative to its
# median than a single-subprocess engine's, and the retry classification makes
# a timeout TRANSIENT (isProbeTerminal) -- an over-tight bound costs retries,
# not failures, but it costs them on every busy minute.

REC="$(awk -v p="$P95" 'BEGIN { v = p * 3; r = int(v); if (v > r) r += 1; if (r < 1) r = 1; print r }')"

{
  echo "PROBE_ENGINE_TIMEOUT measurement"
  echo "corpus:      $CORPUS"
  echo "files timed: $N"
  echo "measured in: $WHERE"
  echo "gs:          $GS_VERSION"
  echo "host arch:   $(uname -sm)"
  echo
  echo "wall-clock seconds per file (sanitize + independent verification):"
  printf "  min     %s\n" "$MIN"
  printf "  median  %s\n" "$MED"
  printf "  p95     %s   (nearest rank %d of %d)\n" "$P95" "$P95_RANK" "$N"
  printf "  max     %s\n" "$MAX"
  echo
  echo "recommendation:"
  echo "  PROBE_ENGINE_TIMEOUT = ceil(p95 x 3) = ceil($P95 x 3) = ${REC}s"
  echo
  echo "  x3, not pandoc's x2: two ghostscript passes plus a parse give this"
  echo "  pipeline a longer tail relative to its median than a single-subprocess"
  echo "  engine has. A probe timeout is classified TRANSIENT (isProbeTerminal),"
  echo "  so an over-tight bound spends retries rather than failing jobs -- but"
  echo "  it spends them on every busy minute."
  echo
  echo "  This is a recommendation, not a decision. The share of timeout-driven"
  echo "  retries that is acceptable for your load is not something this script"
  echo "  can know."
  echo
  echo "per-file rows:"
  grep -E '^ROW,' "$RAW" | sed 's/^ROW,/  /'
  grep -E '^SKIP,' "$RAW" | sed 's/^SKIP,/  skipped: /' || true
} | tee "$OUT"

echo
echo "written to $OUT -- commit it alongside the value it justifies."
