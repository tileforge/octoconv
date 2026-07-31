#!/usr/bin/env bash
# pandoc-timing-measure.sh -- Phase 49 (PDCE-04) wall-seconds-per-conversion
# go/no-go measurement gate.
#
# Structural clone of scripts/ocr-timing-measure.sh (itself cloned from
# scripts/av-rtf-measure.sh/scripts/audio-rtf-measure.sh), adapted for the
# pandoc engine:
#   1. Metric is p95 WALL-CLOCK SECONDS PER CONVERSION (wall_clock_seconds /
#      1, i.e. the plain per-invocation wall time) -- NEVER a per-page
#      divisor (OCR) and NEVER an RTF ratio (audio/av). Pandoc's cost scales
#      with input SIZE x the (from,to) pair, not with a page count or a
#      media duration, so the p95 wall-clock time of one `pandoc` invocation
#      IS the per-conversion cost already -- there is no multiplier to fold
#      in here the way OCR_MAX_PAGES/AUDIO_MAX_DURATION_SECONDS/
#      AV_MAX_DURATION_SECONDS do for their own engines
#      (49-CONTEXT.md D-01, PITFALLS.md Pitfall 7 -- never copy a
#      neighboring engine class's timeout or cost model).
#   2. Sweeps a source-format x input-size x target MATRIX: readers
#      {md, latex, html, docx} x sizes {small ~KB, medium ~100KB,
#      large @~PANDOC_MAX_INPUT_BYTES, env-bounded so the run finishes} x a
#      representative (text-out, zip-out) TARGET PAIR per source, restricted
#      exclusively to pairs present in the CLOSED pandocPairTable
#      (internal/convert/pandoc.go): md->{html,docx}, latex->{html,docx},
#      html->{md,docx} (html cannot target itself; md is its own cheapest
#      text-out analog), docx->{html,epub} (docx cannot target itself;
#      epub is its zip-out analog). The corpus is SYNTHESIZED INSIDE the
#      container (never a newly-committed large binary): a small
#      deterministic Markdown paragraph is repeated to hit each size
#      bucket, then the image's own pinned `pandoc` binary renders that
#      Markdown into genuine latex/html/docx SOURCE fixtures at each size
#      bucket -- so every timed cell exercises a real, format-valid source
#      document, not a synthetic blob with the right extension only.
#   3. The FULL production argv shape is timed for every cell:
#      pandocReaderSpec's exact per-source reader-extension table
#      (md/html get "-raw_html-raw_tex"; latex gets "-raw_tex"; docx gets
#      the bare format name -- it has neither extension to disable) THEN
#      `-t <pandocFormatName(tgt)> --sandbox` and, ONLY when tgt is one of
#      pandocStructuralTargets (html/latex/docx/epub), `--standalone`
#      (internal/convert/pandoc.go's pandocConvertArgs/pandocReaderSpec) --
#      replicated verbatim, never hardcoded to always include
#      --standalone (the html->md cell in this matrix correctly omits it,
#      matching production, since md is not a structural target).
#   4. Pandoc is a single-threaded, deterministic Haskell subprocess with no
#      OpenMP-style thread-count knob (unlike tesseract's OMP_THREAD_LIMIT,
#      ocr-timing-measure.sh section 3) -- this script's own section 3 is
#      therefore purely INFORMATIONAL: it reads and prints this container's
#      own cgroup v2 cpu.max/memory.max for the record, but derives and
#      exports no thread-limit env var.
#   5. Peak memory (cgroup v2 memory.peak) is a companion observation, same
#      as ocr-timing-measure.sh/av-rtf-measure.sh/audio-rtf-measure.sh --
#      informational only, not a hard gate here.
#   6. The closing DERIVATION HELP block PRINTS (never computes-to-a-
#      committed-value) the formula PANDOC_ENGINE_TIMEOUT = ceil(worst-cell
#      p95_sec_per_conversion x 2.0 safety) -- deliberately NO
#      OCR_MAX_PAGES-style multiplier, since pandoc's own p95 already IS the
#      per-conversion cost -- and that it MUST stay strictly under the 900s
#      (15m) RECONCILER_ACTIVE_STALE_AFTER global CAP -- if the derived
#      timeout would breach it, the lever is LOWERING
#      PANDOC_MAX_INPUT_BYTES (Path-B, the input-size cap
#      internal/convert/markupsniff.go's enforcePandocInputSize enforces),
#      which shrinks the large-size cell and lowers the worst-cell p95;
#      re-measure after lowering it. Never raise the global CAP and never
#      copy a neighboring engine class's timeout (PITFALLS.md Pitfall 7).
#
# This script's exit code gates MEASUREMENT INTEGRITY ONLY (build/
# container/corpus/pipeline must all succeed) -- there is no pre-existing
# sec/conversion number to compare against here either; the GO/NO-GO
# decision on the DERIVED PANDOC_ENGINE_TIMEOUT is made separately (Plan 03,
# supervised/operator-reviewed), reading the per-cell p95 this script
# prints. No p95_sec_per_conversion value, however high, causes this script
# itself to exit non-zero. No PANDOC_ENGINE_TIMEOUT value is ever assigned
# or committed by this script.
set -euo pipefail

cd "$(dirname "$0")/.."

IMAGE_TAG="${PANDOC_TIMING_IMAGE_TAG:-octoconv-pandoc-worker:timing-measure}"
RUNS="${PANDOC_TIMING_RUNS:-10}"
CPUS="${PANDOC_TIMING_CPUS:-2.0}"
MEMORY="${PANDOC_TIMING_MEMORY:-1g}"
# Corpus size buckets (D-01): small ~KB, medium ~100KB, large @~the
# PANDOC_MAX_INPUT_BYTES Path-B cap -- bounded well under the compose
# pandoc-worker's own 50 MiB default so this run finishes in a reasonable
# wall-clock window; an operator MAY raise PANDOC_TIMING_LARGE_BYTES toward
# the real cap for a more exhaustive run.
SMALL_BYTES="${PANDOC_TIMING_SMALL_BYTES:-2048}"
MEDIUM_BYTES="${PANDOC_TIMING_MEDIUM_BYTES:-102400}"
LARGE_BYTES="${PANDOC_TIMING_LARGE_BYTES:-8388608}"
# Global reconciler staleness CAP (RECONCILER_ACTIVE_STALE_AFTER,
# docker-compose.yml webhook-worker-1/-2) every engine's derived timeout
# must stay strictly under (49-CONTEXT.md D-01).
RECONCILER_CAP_SECONDS=900

CONTAINER="octoconv-pandoc-timing-measure-$$"
# Host-side scratch dir for logs/results captured via docker exec
# redirections -- everything written here dies with the EXIT trap below,
# never leaking into host /tmp (mirrors ocr-timing-measure.sh's WR-04
# discipline).
WORKDIR=$(mktemp -d)
trap 'docker rm -f "$CONTAINER" >/dev/null 2>&1 || true; rm -rf "$WORKDIR"' EXIT

RESULTS_FILE="$WORKDIR/results.txt"
: >"$RESULTS_FILE"

echo "=== 0. OrbStack discipline: confirm k8s is down before docker build/run work (compose/k8s mutual exclusion, D-05) ==="
if command -v kubectl >/dev/null 2>&1 && kubectl cluster-info >/dev/null 2>&1; then
	echo "FAIL: kubectl reports a live cluster -- stop k8s before running this measurement" >&2
	exit 1
fi
echo "k8s confirmed down (or kubectl unavailable) -- proceeding"

echo
echo "=== 1. Build pandoc-worker image (matches Dockerfile.pandoc-worker exactly, never stale vs source) ==="
docker build -f Dockerfile.pandoc-worker -t "$IMAGE_TAG" .

echo
echo "=== 2. Start a long-lived measurement container (--cpus=$CPUS --memory=$MEMORY, matches the eventual compose pandoc-worker block) ==="
docker run -d --name "$CONTAINER" \
	--cpus="$CPUS" --memory="$MEMORY" \
	--entrypoint sleep "$IMAGE_TAG" infinity >/dev/null

echo
echo "=== 3. Read cgroup v2 cpu.max/memory.max -- INFORMATIONAL ONLY: pandoc is a single-threaded deterministic Haskell subprocess with no OpenMP-style thread-count knob to derive here (unlike tesseract's OMP_THREAD_LIMIT, ocr-timing-measure.sh) ==="
CPU_MAX=$(docker exec "$CONTAINER" cat /sys/fs/cgroup/cpu.max)
MEM_MAX=$(docker exec "$CONTAINER" cat /sys/fs/cgroup/memory.max 2>/dev/null || echo "unavailable")
echo "cpu.max: $CPU_MAX"
echo "memory.max: $MEM_MAX"

echo
echo "=== 4. Synthesize the corpus INSIDE the container (never a newly-committed large binary): a deterministic Markdown paragraph repeated to hit each size bucket, THEN the image's own pinned pandoc renders genuine latex/html/docx SOURCE fixtures at each size ==="
docker exec "$CONTAINER" mkdir -p /tmp/work/corpus
docker exec \
	-e SMALL_BYTES="$SMALL_BYTES" \
	-e MEDIUM_BYTES="$MEDIUM_BYTES" \
	-e LARGE_BYTES="$LARGE_BYTES" \
	"$CONTAINER" sh -c '
    set -e
    para="# OctoConv Pandoc Timing Corpus\n\nThis is a representative Markdown paragraph synthesized deterministically for scripts/pandoc-timing-measure.sh (Phase 49, PDCE-04/D-01). It is repeated to reach each size bucket below. No client data and no external content -- pure synthetic filler text used solely to drive pandoc through its real production argv at bounded, representative input sizes.\n\n"
    gen_md() {
      target="$1"; out="$2"
      : > "$out"
      while [ "$(wc -c < "$out")" -lt "$target" ]; do
        printf "%b" "$para" >> "$out"
      done
      head -c "$target" "$out" > "${out}.tmp"
      mv "${out}.tmp" "$out"
    }
    gen_md "$SMALL_BYTES"  /tmp/work/corpus/md_small.md
    gen_md "$MEDIUM_BYTES" /tmp/work/corpus/md_medium.md
    gen_md "$LARGE_BYTES"  /tmp/work/corpus/md_large.md
  '
for SIZE in small medium large; do
	docker exec "$CONTAINER" pandoc -f markdown-raw_html-raw_tex -t latex --sandbox --standalone \
		"/tmp/work/corpus/md_${SIZE}.md" -o "/tmp/work/corpus/latex_${SIZE}.latex"
	docker exec "$CONTAINER" pandoc -f markdown-raw_html-raw_tex -t html --sandbox --standalone \
		"/tmp/work/corpus/md_${SIZE}.md" -o "/tmp/work/corpus/html_${SIZE}.html"
	docker exec "$CONTAINER" pandoc -f markdown-raw_html-raw_tex -t docx --sandbox --standalone \
		"/tmp/work/corpus/md_${SIZE}.md" -o "/tmp/work/corpus/docx_${SIZE}.docx"
done
echo "Corpus staged: md/latex/html/docx source fixtures at sizes small(~${SMALL_BYTES}B) medium(~${MEDIUM_BYTES}B) large(~${LARGE_BYTES}B)"

# pandoc_reader_spec builds the `-f` reader-format spec for src, mirroring
# internal/convert/pandoc.go's pandocReaderSpec EXACTLY for the four source
# formats this corpus covers.
pandoc_reader_spec() {
	case "$1" in
	md) echo "markdown-raw_html-raw_tex" ;;
	html) echo "html-raw_html-raw_tex" ;;
	latex) echo "latex-raw_tex" ;;
	docx) echo "docx" ;;
	esac
}

# pandoc_target_fmt translates this project's own normalized format token
# into pandoc's writer format name -- mirrors pandocFormatName (only "md"
# needs translating to "markdown"; every other format in this matrix is
# already a valid pandoc format name verbatim).
pandoc_target_fmt() {
	case "$1" in
	md) echo "markdown" ;;
	*) echo "$1" ;;
	esac
}

# pandoc_is_structural reports (via exit code) whether tgt is one of
# pandocStructuralTargets (html/latex/docx/epub) -- mirrors
# pandocConvertArgs' own --standalone gate exactly, so a non-structural
# target (only "md" in this matrix, the html->md cell) correctly times the
# production argv WITHOUT --standalone, never hardcoded to always include it.
pandoc_is_structural() {
	case "$1" in
	html | latex | docx | epub) return 0 ;;
	*) return 1 ;;
	esac
}

# size_bytes_for maps a size-bucket label to its configured byte target --
# purely for the RESULT line's informational input_bytes field.
size_bytes_for() {
	case "$1" in
	small) echo "$SMALL_BYTES" ;;
	medium) echo "$MEDIUM_BYTES" ;;
	large) echo "$LARGE_BYTES" ;;
	esac
}

echo
echo "=== 5. Timed loop INSIDE the container: $RUNS runs per matrix cell, FULL production argv shape (pandocReaderSpec/pandocConvertArgs, internal/convert/pandoc.go) ==="
echo "Matrix: sources=[md latex html docx] sizes=[small medium large] -- each source paired with ONE cheap text-out target and ONE heavier zip-out target, restricted to the CLOSED pandocPairTable: md->{html,docx} latex->{html,docx} html->{md,docx} docx->{html,epub}"

run_cell() {
	src="$1"
	size="$2"
	tgt="$3"
	cell_label="${src}_${size}_${tgt}"
	in_path="/tmp/work/corpus/${src}_${size}.${src}"
	reader=$(pandoc_reader_spec "$src")
	tgtfmt=$(pandoc_target_fmt "$tgt")
	input_bytes=$(size_bytes_for "$size")
	standalone="false"
	if pandoc_is_structural "$tgt"; then
		standalone="true"
	fi

	echo
	echo "--- Cell: cell=${cell_label} (src=$src, size=$size ~${input_bytes}B, target=$tgt, standalone=${standalone}) ---"

	RAW_MILLIS=$(docker exec \
		-e CELL_RUNS="$RUNS" \
		-e CELL_IN="$in_path" \
		-e CELL_READER="$reader" \
		-e CELL_TGTFMT="$tgtfmt" \
		-e CELL_STANDALONE="$standalone" \
		-e CELL_OUTEXT="$tgt" \
		-e CELL_LABEL="$cell_label" \
		"$CONTAINER" sh -c '
      set -e
      i=1
      while [ "$i" -le "$CELL_RUNS" ]; do
        out="/tmp/work/out_${CELL_LABEL}_${i}.${CELL_OUTEXT}"
        start=$(date +%s%N)
        if [ "$CELL_STANDALONE" = "true" ]; then
          pandoc -f "$CELL_READER" -t "$CELL_TGTFMT" --sandbox --standalone "$CELL_IN" -o "$out"
        else
          pandoc -f "$CELL_READER" -t "$CELL_TGTFMT" --sandbox "$CELL_IN" -o "$out"
        fi
        end=$(date +%s%N)
        echo $(( (end - start) / 1000000 ))
        i=$((i + 1))
      done
    ')

	echo "Per-run wall-clock (ms), execution order:"
	echo "$RAW_MILLIS"

	# One conversion per run, no page/RTF divisor (D-01) -- sec_per_conversion
	# is plain wall-clock seconds, kept as an explicit division-by-one so the
	# metric name is unambiguous: WALL-SEC-PER-CONVERSION, never confused with
	# a per-page or RTF-style ratio.
	RAW_SEC_PER_CONVERSION=$(echo "$RAW_MILLIS" | awk '{ printf "%.6f\n", $1/1000 }')
	SORTED=$(echo "$RAW_SEC_PER_CONVERSION" | sort -n)
	N=$(echo "$SORTED" | wc -l | tr -d ' ')
	RANK=$(awk -v n="$N" 'BEGIN { r = n * 0.95; rr = (r == int(r)) ? r : int(r) + 1; print rr }')
	P95_SEC_PER_CONVERSION=$(echo "$SORTED" | sed -n "${RANK}p")

	echo "Sorted ascending seconds-per-conversion: $SORTED"
	echo "N=$N, nearest-rank p95 rank=$RANK, p95 sec/conversion=${P95_SEC_PER_CONVERSION}"
	echo "RESULT cell=${cell_label} src=${src} size=${size} target=${tgt} N=$N p95_sec_per_conversion=${P95_SEC_PER_CONVERSION} cpus=${CPUS} memory=${MEMORY} input_bytes=${input_bytes}"
	echo "${cell_label} ${P95_SEC_PER_CONVERSION}" >>"$RESULTS_FILE"
}

for SRC_TGT in "md:html" "md:docx" "latex:html" "latex:docx" "html:md" "html:docx" "docx:html" "docx:epub"; do
	SRC="${SRC_TGT%%:*}"
	TGT="${SRC_TGT##*:}"
	for SIZE in small medium large; do
		run_cell "$SRC" "$SIZE" "$TGT"
	done
done

echo
echo "=== 6. Peak memory observation (cgroup v2 memory.peak, inside the ${MEMORY} limit; informational only, not a hard gate) ==="
PEAK_BYTES=$(docker exec "$CONTAINER" cat /sys/fs/cgroup/memory.peak 2>/dev/null || echo "unavailable")
if [ "$PEAK_BYTES" != "unavailable" ]; then
	PEAK_MB=$(awk -v b="$PEAK_BYTES" 'BEGIN { printf "%.1f", b / 1024 / 1024 }')
	echo "Peak memory (single-job pandoc RSS, across all cells run so far in this container): ${PEAK_BYTES} bytes (~${PEAK_MB} MiB) of ${MEMORY} limit"
else
	echo "Peak memory: cgroup v2 memory.peak not available in this environment"
fi

echo
echo "=== 7. Image size + measurement-host architecture (Phase-23/32/36/45-style arch caveat) ==="
IMAGE_SIZE=$(docker image inspect "$IMAGE_TAG" --format '{{.Size}}')
IMAGE_SIZE_MB=$(awk -v b="$IMAGE_SIZE" 'BEGIN { printf "%.1f", b / 1024 / 1024 }')
HOST_ARCH=$(uname -m)
echo "Image size: ${IMAGE_SIZE} bytes (~${IMAGE_SIZE_MB} MiB)"
echo "Measurement host architecture: ${HOST_ARCH} -- production amd64 sec/conversion is expected to differ from this number, same caveat as ocr-timing-measure.sh/av-rtf-measure.sh/audio-rtf-measure.sh"

echo
echo "=== 8. DERIVATION HELP (prints ONLY -- this script commits NO PANDOC_ENGINE_TIMEOUT value) ==="
WORST_LINE=$(sort -k2,2 -n "$RESULTS_FILE" | tail -n1)
WORST_CELL=$(echo "$WORST_LINE" | awk '{ print $1 }')
WORST_P95=$(echo "$WORST_LINE" | awk '{ print $2 }')
EXAMPLE_TIMEOUT=$(awk -v p95="$WORST_P95" 'BEGIN { v = p95 * 2.0; printf "%d", (v == int(v)) ? v : int(v) + 1 }')
echo "Worst-cell p95 sec/conversion observed this run: ${WORST_P95}s (cell=${WORST_CELL})"
echo "Formula: PANDOC_ENGINE_TIMEOUT = ceil(worst_cell_p95_sec_per_conversion x 2.0 safety factor) -- deliberately NO OCR_MAX_PAGES-style multiplier: pandoc's per-conversion cost scales with input SIZE x the (from,to) pair, not with a page count or media duration, so the p95 above IS the per-conversion cost already."
echo "Worked example (informational only, this run's own worst cell): ceil(${WORST_P95} x 2.0) = ${EXAMPLE_TIMEOUT}s"
echo "This MUST stay strictly under the ${RECONCILER_CAP_SECONDS}s (15m) RECONCILER_ACTIVE_STALE_AFTER global CAP (docker-compose.yml webhook-worker-1/-2) -- if the derived timeout would breach ${RECONCILER_CAP_SECONDS}s, the lever is LOWERING PANDOC_MAX_INPUT_BYTES (Path-B, internal/convert/markupsniff.go's enforcePandocInputSize cap -- shrinks the large-size cell, lowering the worst-cell p95; re-measure), never raising the global CAP and never copying a neighboring engine class's timeout (PITFALLS.md Pitfall 7)."
echo "NO value is committed by this script. The GO/NO-GO decision on the real docker-compose.yml PANDOC_ENGINE_TIMEOUT is Plan 03's supervised, operator-reviewed step, reading the per-cell p95 RESULT lines printed above."

echo
echo "=== ALL GREEN: measurement integrity checks passed (build, container, cgroup read, corpus synthesis, timed matrix sweep x24 cells, per-cell p95, memory, image size all captured) ==="
echo "This script's exit code reflects MEASUREMENT INTEGRITY ONLY -- no RESULT line's p95_sec_per_conversion value, however high, causes a non-zero exit here. The GO/NO-GO on PANDOC_ENGINE_TIMEOUT derived from these numbers is a separate, operator-reviewed decision (Plan 03)."
echo "peak_mem_bytes=${PEAK_BYTES} image_size_bytes=${IMAGE_SIZE} host_arch=${HOST_ARCH}"
