#!/usr/bin/env bash
# tectonic-timeout-measure.sh -- Phase 64 (TEX-05) TECTONIC_ENGINE_TIMEOUT
# measurement gate.
#
# WHY THIS SCRIPT EXISTS
#
# This project has five measured engine timeouts -- 65s, 600s, 742s, 744s,
# 753s -- every one of them measured on its own engine's own corpus, and
# copying any of them to a sixth engine is a named project antipattern
# (PITFALLS.md Pitfall 7). TECTONIC_ENGINE_TIMEOUT ships as a 300s PLACEHOLDER
# that every file carrying it says is a placeholder, and this run is what
# replaces it.
#
# WHY A PER-PASS COST AND NOT A p95
#
# scripts/probe-timeout-measure.sh reports a p95 and .env.example records, in
# as many words, why 600s was NOT derived from it: when cost is set by a
# per-unit count and the corpus is chosen by hand, a wall-clock percentile
# measures the choice of corpus. The same trap is here and it is WORSE, because
# a document's pass count varies far more than a PDF's page count does -- the
# corpus below spans 2 to 7 passes for the same body text.
#
# So the derivation is per-pass cost x a pass CEILING x a margin, and the
# ceiling is not a corpus statistic: it is the engine's own hard maximum.
# Tectonic reruns until its references converge and then stops, saying so:
#
#     warning: TeX rerun seems needed, but stopping at 6 passes
#
# Seven TeX invocations is therefore the most any document can cost, whatever
# it contains. A bound derived from that is a bound on the ENGINE; a p95 would
# have been a bound on nine files somebody chose.
#
# WHERE IT MEASURES
#
# Inside the DEPLOYED worker image, at the cgroup limits the chart sets
# (2 CPU / 1 GiB), with --network none and USER nobody -- the conditions a real
# job runs under. Phase 60 recorded why this matters: its measurement container
# resolved ghostscript 10.00.0 while the host had 10.07.1, and a number
# produced against a different binary is a number about a machine nobody runs.
# There is no host fallback here at all, because tectonic is not installed on
# the host and a fallback nobody can run is a fallback nobody checks.
#
# WHAT IT DOES NOT COVER
#
# The ENGINE, not the whole job: the worker also downloads the input from S3
# and uploads the PDF back. Those are bounded by the storage client, not by
# TECTONIC_ENGINE_TIMEOUT, but the timeout has to leave room for them. The
# evidence file records a full-pipeline cross-check against the live API for
# the largest document, so the gap is a measured number rather than an
# assumption.
#
# FAIL-CLOSED ON THE CORPUS
#
# If any file fails to compile, the run FAILS and prints no recommendation. A
# timeout measured on a corpus that partly does not compile is a number about
# the files that happened to work. The first draft of the corpus had three such
# files (a blank line inside an equation environment) and this check is what
# found them.
#
# USAGE
#
#   scripts/tectonic-timeout-measure.sh [corpus-dir] [output-file]

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

CORPUS="${1:-$REPO_ROOT/internal/convert/testdata/tectonic/corpus}"
# Absolute, always: docker -v rejects a relative path as a volume NAME, and the
# error it gives ("includes invalid characters for a local volume name") does
# not obviously say so.
case "$CORPUS" in /*) ;; *) CORPUS="$REPO_ROOT/$CORPUS" ;; esac
OUT="${2:-$REPO_ROOT/tectonic-timeout-measurement.txt}"
IMAGE="${OCTOCONV_TECTONIC_IMAGE:-octoconv-tectonic-worker}"
BUNDLE="${OCTOCONV_TECTONIC_BUNDLE:-/opt/tectonic/octoconv-bundle.ttb}"

# The engine's own hard rerun maximum, observed and quoted above. Overridable
# because a future tectonic may change it -- but NOT to tune the answer: raise
# it only after re-observing the engine's own message.
PASS_CEILING="${OCTOCONV_TECTONIC_PASS_CEILING:-7}"

# The margin over the ceiling-cost. Deliberately a parameter with no clever
# default: the operator decides how much of the tail they are willing to pay
# for, and the script prints the formula so they can disagree with it.
MARGIN="${OCTOCONV_TECTONIC_MARGIN:-1.5}"

# Repetitions per file. Three, not one, and the reason is a measured mistake:
# the first run timed each file ONCE and reported single-l (30 pages) at 0.838s
# against single-s (1 page) at 1.585s -- the largest document as the fastest.
# That is not a result, it is jitter, and a derivation on top of it would have
# been a number about container scheduling. Each file is now run REPS times and
# the SLOWEST run is kept: a timeout is a bound, and the bound has to hold on
# the unlucky run, not the median one.
REPS="${OCTOCONV_TECTONIC_REPS:-3}"

# RECONCILER_ACTIVE_STALE_AFTER. A timeout at or above this makes the
# reconciler treat a genuinely in-flight job as abandoned and re-enqueue it,
# so it is a hard cap and not a preference.
RECONCILER_CAP=900

# The per-engine upload ceiling the shipped timeout is derived from
# (defaultMaxEngineBytes[EngineTectonic]). It is printed with the result
# because the two are ONE decision: cost is linear in source size, so without a
# ceiling on the input there is no bound on the output. At the 100 MiB this
# engine inherited by default, a dense .tex would need roughly 9000s -- ten
# times the reconciler cap -- and the timeout would be a number that bounds
# nothing. Measured at the ceiling, not extrapolated to it: see
# testdata/tectonic/corpus/PROVENANCE.md.
INPUT_CEILING_MIB="${OCTOCONV_TECTONIC_INPUT_CEILING_MIB:-2}"

if [[ ! -d "$CORPUS" ]]; then
  echo "error: $CORPUS is not a directory" >&2
  exit 64
fi

# THE CORPUS MUST REACH THE CEILING, or this script's own conclusion is false.
#
# The committed corpus tops out at ~369 KB, and the number this project ships
# comes from a 2 143 444-byte document that is generated on demand rather than
# committed (two 2 MiB files would outweigh everything else in testdata). So the
# DEFAULT invocation measured small documents while printing "derived from a run
# AT the ceiling, never extrapolated to it" -- a remeasurement could silently
# recommend a much smaller timeout and still claim the ceiling. Found in review
# (PR #16, codex round 3).
#
# Rather than fail and leave the operator to remember a flag, stage the full
# corpus with the generator that produced the committed one. That also PROVES
# the generator still reproduces it: every committed file must come back byte
# identical, or the corpus and its generator have diverged and neither can be
# trusted to describe the other.
CEILING_BYTES=$(( INPUT_CEILING_MIB * 1024 * 1024 ))
largest=0
for f in "$CORPUS"/*.tex; do
  [ -e "$f" ] || continue
  n="$(wc -c < "$f" | tr -d ' ')"
  [ "$n" -gt "$largest" ] && largest="$n"
done
if [ "$largest" -lt "$CEILING_BYTES" ]; then
  GEN="$REPO_ROOT/scripts/cmd/gen-tectonic-corpus.py"
  [ -f "$GEN" ] || { echo "error: corpus tops out at $largest bytes, below the ${INPUT_CEILING_MIB} MiB ceiling, and $GEN is missing" >&2; exit 64; }
  command -v python3 >/dev/null 2>&1 || { echo "error: python3 is needed to generate the ceiling document" >&2; exit 69; }

  STAGED="$(mktemp -d)"
  trap 'rm -rf "$STAGED"' EXIT
  python3 "$GEN" "$STAGED" --bound >/dev/null

  # Every committed file must be reproduced byte for byte.
  for f in "$CORPUS"/*.tex; do
    b="$(basename "$f")"
    [ -f "$STAGED/$b" ] || { echo "error: the generator no longer produces $b" >&2; exit 65; }
    cmp -s "$f" "$STAGED/$b" || { echo "error: $b differs from what the generator produces -- corpus and generator have diverged" >&2; exit 65; }
  done

  staged_largest=0
  for f in "$STAGED"/*.tex; do
    n="$(wc -c < "$f" | tr -d ' ')"
    [ "$n" -gt "$staged_largest" ] && staged_largest="$n"
  done
  [ "$staged_largest" -ge "$CEILING_BYTES" ] || { echo "error: even the generated corpus tops out at $staged_largest bytes, below $CEILING_BYTES" >&2; exit 65; }

  echo "note: committed corpus tops out at $largest bytes; staged the generated"
  echo "      corpus (largest $staged_largest bytes) so the measurement reaches"
  echo "      the ${INPUT_CEILING_MIB} MiB input ceiling it claims to."
  echo
  CORPUS="$STAGED"
fi
if ! command -v docker >/dev/null 2>&1; then
  echo "error: docker is required; tectonic is not installed on the host and this" >&2
  echo "       measurement is only meaningful inside the deployed image" >&2
  exit 69
fi
if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
  echo "error: image $IMAGE not found -- build it first:" >&2
  echo "         docker compose build tectonic-worker" >&2
  exit 69
fi

# --- the argv must be the converter's, and that is checked, not claimed ------
#
# This script invokes tectonic directly. If it did so with different flags than
# TectonicConverter.Convert uses, it would measure a different program: -Z
# shell-escape absent, a different bundle, or no -p would each change both the
# cost and the output this script parses. Every flag below is asserted to be
# present in internal/convert/tectonic.go's tectonicArgs.
CONVERTER_SRC="$REPO_ROOT/internal/convert/tectonic.go"
for flag in '"-X"' '"compile"' '"--untrusted"' '"-b"' '"-o"' '"-p"'; do
  if ! grep -q -- "$flag" "$CONVERTER_SRC"; then
    echo "error: the converter's argv no longer contains $flag ($CONVERTER_SRC)." >&2
    echo "       This script would then be timing a different program than the worker runs." >&2
    exit 1
  fi
done

TECTONIC_VERSION="$(docker run --rm --entrypoint tectonic "$IMAGE" --version 2>/dev/null | head -1 || echo unknown)"
BUNDLE_SHA="$(docker run --rm --entrypoint sh "$IMAGE" -c "sha256sum $BUNDLE 2>/dev/null | cut -d' ' -f1" || echo unknown)"
BUNDLE_SIZE="$(docker run --rm --entrypoint sh "$IMAGE" -c "wc -c < $BUNDLE 2>/dev/null" || echo unknown)"

echo "corpus:    $CORPUS"
echo "image:     $IMAGE"
echo "engine:    $TECTONIC_VERSION"
echo "bundle:    ${BUNDLE_SHA} (${BUNDLE_SIZE} bytes)"
echo "where:     container, --cpus 2.0 --memory 1g, --network none, USER nobody"
echo "host arch: $(uname -sm)"
echo
echo "reps:      $REPS per file (slowest kept)"
echo
echo "class,file,bytes,passes,pages,seconds,status"

RAW="$(mktemp)"
trap 'rm -f "$RAW"' EXIT

# One container per file, not one for all nine: a job gets a fresh process, and
# batching them would let the OS page cache from file N warm file N+1 -- which
# no real job benefits from. It costs ~0.3s of container start per file, and
# that cost is OUTSIDE the timed region below (the timing is taken inside the
# container, around the tectonic invocation alone).
for f in "$CORPUS"/*.tex; do
  name="$(basename "$f" .tex)"
  class="${name%%-*}"
  bytes="$(wc -c < "$f" | tr -d ' ')"

  # One container per REPETITION, not one for all of them: a job gets a fresh
  # process, and reusing a container would let the OS page cache from run N
  # warm run N+1 -- which no real job benefits from. `date +%s%N` inside the
  # container brackets the engine call only, so container start-up and the
  # bind mount never enter the number.
  best=""
  for _ in $(seq 1 "$REPS"); do
    row="$(docker run --rm \
      --cpus 2.0 --memory 1g --network none -u nobody \
      -v "$f:/in/$name.tex:ro" \
      -w /tmp \
      --entrypoint sh "$IMAGE" -c "
        start=\$(date +%s%N)
        if tectonic -X compile /in/$name.tex --untrusted -b $BUNDLE -o /tmp -p > /tmp/log 2>&1; then
          status=ok
        else
          status=FAILED
        fi
        end=\$(date +%s%N)
        secs=\$(awk -v s=\$start -v e=\$end 'BEGIN{printf \"%.3f\", (e-s)/1000000000}')
        passes=\$(grep -c 'Transcript written' /tmp/log || echo 0)
        pages=\$(grep -o '([0-9]* page' /tmp/log | tail -1 | tr -d '(' | cut -d' ' -f1)
        [ -n \"\$pages\" ] || pages=0
        echo \"\$passes,\$pages,\$secs,\$status\"
        if [ \"\$status\" = FAILED ]; then tail -6 /tmp/log >&2; fi
      ")"
    # A FAILED run wins immediately: the corpus check below must see it rather
    # than have it hidden by two lucky repetitions.
    case "$row" in *FAILED) best="$row"; break;; esac
    if [ -z "$best" ]; then
      best="$row"
    else
      best="$(printf '%s\n%s\n' "$best" "$row" | awk -F, '{ if ($3 + 0 > m) { m = $3 + 0; r = $0 } } END { print r }')"
    fi
  done

  echo "$class,$name,$bytes,$best" | tee -a "$RAW"
done

if grep -q ',FAILED$' "$RAW"; then
  echo
  echo "MEASUREMENT FAILED: at least one corpus file did not compile." >&2
  echo "No recommendation is printed -- a timeout measured on a corpus that partly" >&2
  echo "does not compile is a number about the files that happened to work." >&2
  grep ',FAILED$' "$RAW" >&2
  exit 1
fi

# --- per-pass cost ----------------------------------------------------------
#
# The quantity that scales. Reported per file and taken at its MAXIMUM, not its
# mean or its p95: the bound has to hold for the worst document the corpus
# contains, and with nine files a percentile is a rank, not a distribution.
WORST_PER_PASS="$(awk -F, '{ pp = $6 / $4; if (pp > m) { m = pp; f = $2; p = $4; s = $6 } } END { printf "%.3f %s %d %s", m, f, p, s }' "$RAW")"
WORST_PP="$(echo "$WORST_PER_PASS" | cut -d' ' -f1)"
WORST_FILE="$(echo "$WORST_PER_PASS" | cut -d' ' -f2)"
WORST_PASSES="$(echo "$WORST_PER_PASS" | cut -d' ' -f3)"
WORST_SECS="$(echo "$WORST_PER_PASS" | cut -d' ' -f4)"

SLOWEST="$(awk -F, '{ if ($6 + 0 > m) { m = $6 + 0; f = $2 } } END { printf "%s %.3f", f, m }' "$RAW")"

REC="$(awk -v pp="$WORST_PP" -v c="$PASS_CEILING" -v m="$MARGIN" \
  'BEGIN { v = pp * c * m; r = int(v); if (v > r) r += 1; if (r < 1) r = 1; print r }')"
CAP_MARGIN_PCT="$(awk -v r="$REC" -v cap="$RECONCILER_CAP" 'BEGIN { printf "%.1f", (cap - r) * 100.0 / cap }')"
CLEARS_CAP=$([[ "$REC" -lt "$RECONCILER_CAP" ]] && echo yes || echo NO)

{
  echo "TECTONIC_ENGINE_TIMEOUT measurement"
  echo "date:        $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "corpus:      $CORPUS ($(grep -c . "$RAW") files, all compiled)"
  echo "image:       $IMAGE"
  echo "engine:      $TECTONIC_VERSION"
  echo "bundle:      $BUNDLE_SHA ($BUNDLE_SIZE bytes)"
  echo "measured in: container, --cpus 2.0 --memory 1g, --network none, USER nobody"
  echo "host arch:   $(uname -sm)"
  echo
  echo "per-file rows (class,file,bytes,passes,pages,seconds,status),"
  echo "seconds = the SLOWEST of $REPS runs:"
  sed 's/^/  /' "$RAW"
  echo
  echo "pass counts are the ENGINE's, counted from its own -p output"
  echo "(one 'Transcript written' line per TeX invocation). The three classes"
  echo "exist to separate per-pass cost from per-document overhead."
  echo
  echo "worst per-pass cost:"
  printf "  %s: %ss over %s passes = %s s/pass\n" "$WORST_FILE" "$WORST_SECS" "$WORST_PASSES" "$WORST_PP"
  printf "  slowest file overall: %s\n" "$SLOWEST"
  echo
  echo "recommendation:"
  echo "  TECTONIC_ENGINE_TIMEOUT = ceil(worst s/pass x pass ceiling x margin)"
  echo "                          = ceil($WORST_PP x $PASS_CEILING x $MARGIN)"
  echo "                          = ${REC}s"
  echo
  echo "  pass ceiling $PASS_CEILING: the ENGINE's own maximum, not a corpus statistic."
  echo "  Tectonic reruns until its references converge and then stops, saying"
  echo "  'stopping at 6 passes' -- seven TeX invocations is the most any"
  echo "  document can cost, whatever it contains. This is why the derivation"
  echo "  is not a p95: a percentile here would measure the corpus's mix of"
  echo "  1-pass and 7-pass documents, which is a property of the corpus."
  echo
  echo "  margin $MARGIN: the operator's parameter. It covers the gap between"
  echo "  this host and the deployment, and the worker's own S3 download and"
  echo "  upload, which sit inside the same clock but outside this measurement."
  echo
  echo "  input ceiling ${INPUT_CEILING_MIB} MiB: the other half of this number, and not"
  echo "  separable from it. Cost is LINEAR in source size within the bibliography"
  echo "  class (1 MiB -> 90.0s, 2 MiB -> 180.3s, both measured at 7 passes), so a"
  echo "  timeout without a ceiling on the input bounds nothing: at the 100 MiB"
  echo "  this engine inherited by default, a dense .tex needs ~9000s. The number"
  echo "  above is derived from a run AT the ceiling, never extrapolated to it."
  echo
  echo "  reconciler cap: RECONCILER_ACTIVE_STALE_AFTER = ${RECONCILER_CAP}s."
  echo "  ${REC}s clears it: $CLEARS_CAP (margin ${CAP_MARGIN_PCT}%)."
  echo "  A value at or above the cap would have the reconciler re-enqueue jobs"
  echo "  that are still running. If a future corpus pushes the derivation past"
  echo "  it, the lever is a bound on the INPUT -- the Path-B move OCR made --"
  echo "  never a smaller margin."
  echo
  echo "  This is a recommendation, not a decision."
} | tee "$OUT"

echo
echo "written to $OUT"
