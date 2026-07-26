#!/usr/bin/env bash
# ocr-timing-measure.sh -- Phase 45 (OCRE-04) seconds-per-page go/no-go
# measurement gate.
#
# Structural clone of scripts/av-rtf-measure.sh (itself cloned from
# scripts/audio-rtf-measure.sh), adapted for the ocr (tesseract/poppler)
# engine:
#   1. Metric is p95 SECONDS-PER-PAGE (wall_clock_seconds / pages_rendered),
#      NEVER an RTF ratio -- OCR cost scales with PAGE COUNT and page
#      complexity, not with an input's "duration" the way audio/av do
#      (D-02, 45-CONTEXT.md). OCR_ENGINE_TIMEOUT/OCR_MAX_PAGES are DERIVED
#      FROM this measurement, never asserted against nor copied from a
#      neighboring engine class's constant (PITFALLS.md Pitfall 7 --
#      exactly the mistake the project's own Key Decisions log already
#      flags for audio: "измеряется RTF-гейтом, не копируется от соседних
#      классов").
#   2. Sweeps a 3-document x 2-language MATRIX (D-02): a CLEAN single-page
#      scan, a DENSE page (routed through the SAME pdftoppm-render-then-OCR
#      shape a real scanned PDF source takes, ocrpdf.go), and a NOISY/
#      adversarial scan -- each OCR'd in BOTH eng and rus (Pitfall 7:
#      "measure separately per language ... a single blended number risks
#      being wrong in both directions"). Fixtures are copied in from the
#      already-committed internal/convert/testdata/ocr/ corpus (Plan 02/03
#      unit-test fixtures) rather than a newly-committed large binary --
#      text_sample.png (clean), adversarial_text.png (noisy),
#      multipage_image_only.pdf (dense: page 1, an actual image-only
#      scanned-PDF fixture, exercising the SAME pdftoppm -> tesseract
#      per-page shape convertPDF drives in production, ocrpdf.go). These
#      are small synthetic ImageMagick-generated fixtures, not real-world
#      scans -- an operator with access to a more representative corpus MAY
#      swap the three OCR_TIMING_*_SRC paths below before running this
#      script; the matrix/derivation machinery does not change either way.
#   3. The FULL production argv shapes are timed: for the PDF-sourced dense
#      cell, ocrRasterizePageArgs' pdftoppm shape (-r 300 -f N -l N
#      -singlefile -png, internal/convert/ocrpdf.go) THEN
#      ocrTesseractArgs' tesseract shape (imagename outputbase --psm 3
#      --oem 1 -l <lang>, internal/convert/ocr.go); for the two
#      already-image cells (clean/noisy), tesseract is invoked directly --
#      no pdftoppm step -- exactly mirroring Convert()'s image-source
#      branch (ocr.go), which never rasterizes an already-image input.
#   4. OMP_THREAD_LIMIT is read live from THIS container's own cgroup v2
#      cpu.max (floor of quota/period, min 1) and exported before every
#      timed tesseract invocation -- mirrors cmd/ocr-worker/main.go's
#      resolveOCRThreadLimit()/CgroupCPULimit() discipline (D-03), so
#      tesseract's own OpenMP thread use honors the container's real CPU
#      quota instead of the host's full core count (PITFALLS.md Pitfall 5's
#      threading discipline, applied to OCR's OpenMP-parallel LSTM engine).
#   5. Peak memory (cgroup v2 memory.peak) is a companion observation, same
#      as av-rtf-measure.sh/audio-rtf-measure.sh -- informational only, not
#      a hard gate here.
#   6. The closing DERIVATION HELP block PRINTS (never computes-to-a-
#      committed-value) the formula OCR_ENGINE_TIMEOUT = ceil(OCR_MAX_PAGES
#      x worst-cell p95_sec_per_page x 2.0 safety), and that it MUST stay
#      strictly under the 900s (15m) RECONCILER_ACTIVE_STALE_AFTER global
#      CAP -- if the derived timeout would breach it, the lever is LOWERING
#      OCR_MAX_PAGES (Path-B, exactly the AUDIO_MAX_DURATION_SECONDS/
#      AV_MAX_DURATION_SECONDS NO-GO precedent, 32-03-SUMMARY.md/
#      36-04-SUMMARY.md), never raising the global CAP or copying a
#      neighboring engine's timeout.
#
# This script's exit code gates MEASUREMENT INTEGRITY ONLY (build/
# container/corpus/pipeline must all succeed) -- there is no pre-existing
# sec/page number to compare against here either; the GO/NO-GO decision on
# the DERIVED OCR_ENGINE_TIMEOUT/OCR_MAX_PAGES is made separately (Plan 03,
# supervised/operator-reviewed), reading the per-cell p95 this script
# prints. No p95_sec_per_page value, however high, causes this script
# itself to exit non-zero. No OCR_ENGINE_TIMEOUT value is ever assigned or
# committed by this script.
set -euo pipefail

cd "$(dirname "$0")/.."

IMAGE_TAG="${OCR_TIMING_IMAGE_TAG:-octoconv-ocr-worker:timing-measure}"
RUNS="${OCR_TIMING_RUNS:-10}"
CPUS="${OCR_TIMING_CPUS:-2.0}"
MEMORY="${OCR_TIMING_MEMORY:-1g}"
LANGS="${OCR_TIMING_LANGS:-eng rus}"
# The 3-document corpus (D-02): clean single-page scan, dense page (PDF
# source -- exercises the pdftoppm-render-then-OCR production shape), and a
# noisy/adversarial scan. Overridable so an operator can substitute a more
# representative corpus without touching the matrix/derivation logic below.
OCR_TIMING_CLEAN_SRC="${OCR_TIMING_CLEAN_SRC:-internal/convert/testdata/ocr/text_sample.png}"
OCR_TIMING_NOISY_SRC="${OCR_TIMING_NOISY_SRC:-internal/convert/testdata/ocr/adversarial_text.png}"
OCR_TIMING_DENSE_SRC="${OCR_TIMING_DENSE_SRC:-internal/convert/testdata/ocr/multipage_image_only.pdf}"
# Informational-only default used by the closing DERIVATION HELP block's
# worked example -- mirrors the compose ocr-worker's own provisional
# OCR_MAX_PAGES=200 ceiling (docker-compose.yml). This script never reads
# nor writes docker-compose.yml; it is purely an illustrative multiplier.
OCR_TIMING_MAX_PAGES="${OCR_TIMING_MAX_PAGES:-200}"
# Global reconciler staleness CAP (RECONCILER_ACTIVE_STALE_AFTER,
# docker-compose.yml webhook-worker-1/-2) every engine's derived timeout
# must stay strictly under (45-CONTEXT.md D-02).
RECONCILER_CAP_SECONDS=900

CONTAINER="octoconv-ocr-timing-measure-$$"
# Host-side scratch dir for logs/results captured via docker exec
# redirections -- everything written here dies with the EXIT trap below,
# never leaking into host /tmp (mirrors av-rtf-measure.sh's WR-04
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
echo "=== 1. Build ocr-worker image (matches Dockerfile.ocr-worker exactly, never stale vs source) ==="
docker build -f Dockerfile.ocr-worker -t "$IMAGE_TAG" .

echo
echo "=== 2. Start a long-lived measurement container (--cpus=$CPUS --memory=$MEMORY, matches the eventual compose ocr-worker block) ==="
docker run -d --name "$CONTAINER" \
	--cpus="$CPUS" --memory="$MEMORY" \
	--entrypoint sleep "$IMAGE_TAG" infinity >/dev/null

echo
echo "=== 3. Read cgroup v2 cpu.max -> derive tesseract's OMP_THREAD_LIMIT (floor of quota/period, min 1; matches cmd/ocr-worker/main.go's resolveOCRThreadLimit()/CgroupCPULimit()) ==="
CPU_MAX=$(docker exec "$CONTAINER" cat /sys/fs/cgroup/cpu.max)
echo "cpu.max: $CPU_MAX"
THREADS=$(echo "$CPU_MAX" | awk '{ if ($1 == "max") { print 1 } else { n = int($1/$2); print (n < 1) ? 1 : n } }')
echo "Derived OMP_THREAD_LIMIT: $THREADS"

echo
echo "=== 4. Stage the 3-document corpus INSIDE the container (clean/dense/noisy) -- copied from already-committed internal/convert/testdata/ocr/ fixtures, never a newly-committed large binary ==="
docker exec "$CONTAINER" mkdir -p /tmp/work/corpus
docker cp "$OCR_TIMING_CLEAN_SRC" "$CONTAINER:/tmp/work/corpus/clean.png"
docker cp "$OCR_TIMING_NOISY_SRC" "$CONTAINER:/tmp/work/corpus/noisy.png"
docker cp "$OCR_TIMING_DENSE_SRC" "$CONTAINER:/tmp/work/corpus/dense.pdf"
echo "Corpus staged: clean=$OCR_TIMING_CLEAN_SRC dense=$OCR_TIMING_DENSE_SRC (PDF, page 1 rendered per-run) noisy=$OCR_TIMING_NOISY_SRC"

echo
echo "=== 5. Timed loop INSIDE the container: $RUNS runs per matrix cell, FULL production argv shape (ocrRasterizePageArgs/ocrTesseractArgs, internal/convert/ocrpdf.go+ocr.go) ==="
echo "Matrix: docs=[clean dense noisy] langs=[$LANGS] -- dense is PDF-sourced (pdftoppm render THEN tesseract, per page); clean/noisy are already images (tesseract only, no render step -- matches Convert()'s image-source branch)"

# doc_is_pdf reports whether doc's corpus source is PDF-sourced (only
# "dense" in this corpus) -- the branch point between the two production
# argv shapes ocr.go's Convert() dispatches on source format.
doc_is_pdf() {
	[ "$1" = "dense" ]
}

run_cell() {
	doc="$1"
	lang="$2"
	cell_label="${doc}_${lang}"

	echo
	echo "--- Cell: cell=${cell_label} (doc=$doc, lang=$lang) ---"

	if doc_is_pdf "$doc"; then
		# PDF source: render page 1 via pdftoppm (ocrRasterizePageArgs'
		# exact shape: -r 300 -f N -l N -singlefile -png) THEN tesseract
		# the rendered page -- timed together, matching convertPDF's
		# per-page pipeline (ocrpdf.go). pages_rendered=1 per run (a
		# single representative page, D-02's "3-document" corpus).
		RAW_MILLIS=$(docker exec "$CONTAINER" sh -c "
      set -e
      for i in \$(seq 1 $RUNS); do
        start=\$(date +%s%N)
        pdftoppm -r 300 -f 1 -l 1 -singlefile -png /tmp/work/corpus/dense.pdf /tmp/work/dense_page_\${i}
        OMP_THREAD_LIMIT=$THREADS tesseract /tmp/work/dense_page_\${i}.png /tmp/work/out_${cell_label}_\${i} --psm 3 --oem 1 -l ${lang} >/tmp/work/tess_${cell_label}_\${i}.log 2>&1
        end=\$(date +%s%N)
        echo \$(( (end - start) / 1000000 ))
      done
    ")
	else
		# Already-image source: tesseract invoked directly, no pdftoppm
		# step -- matches Convert()'s image-source branch (ocr.go), which
		# never rasterizes an already-image input. pages_rendered=1.
		RAW_MILLIS=$(docker exec "$CONTAINER" sh -c "
      set -e
      for i in \$(seq 1 $RUNS); do
        start=\$(date +%s%N)
        OMP_THREAD_LIMIT=$THREADS tesseract /tmp/work/corpus/${doc}.png /tmp/work/out_${cell_label}_\${i} --psm 3 --oem 1 -l ${lang} >/tmp/work/tess_${cell_label}_\${i}.log 2>&1
        end=\$(date +%s%N)
        echo \$(( (end - start) / 1000000 ))
      done
    ")
	fi

	echo "Per-run wall-clock (ms), execution order:"
	echo "$RAW_MILLIS"

	# pages_rendered=1 for every run in this corpus (each document is a
	# single representative page) -- sec_per_page reduces to plain
	# wall-clock seconds, but the division is kept explicit so the metric
	# is unambiguously SECONDS-PER-PAGE (D-02), never confused with an
	# RTF-style ratio.
	PAGES=1
	RAW_SEC_PER_PAGE=$(echo "$RAW_MILLIS" | awk -v pages="$PAGES" '{ printf "%.6f\n", ($1/1000)/pages }')
	SORTED=$(echo "$RAW_SEC_PER_PAGE" | sort -n)
	N=$(echo "$SORTED" | wc -l | tr -d ' ')
	RANK=$(awk -v n="$N" 'BEGIN { r = n * 0.95; rr = (r == int(r)) ? r : int(r) + 1; print rr }')
	P95_SEC_PER_PAGE=$(echo "$SORTED" | sed -n "${RANK}p")

	echo "Sorted ascending seconds-per-page: $SORTED"
	echo "N=$N, nearest-rank p95 rank=$RANK, p95 sec/page=${P95_SEC_PER_PAGE}"
	echo "RESULT cell=${cell_label} doc=${doc} lang=${lang} N=$N p95_sec_per_page=${P95_SEC_PER_PAGE} cpus=${CPUS} memory=${MEMORY} omp_thread_limit=${THREADS} pages=${PAGES}"
	echo "${cell_label} ${P95_SEC_PER_PAGE}" >>"$RESULTS_FILE"
}

for DOC in clean dense noisy; do
	for LANG in $LANGS; do
		run_cell "$DOC" "$LANG"
	done
done

echo
echo "=== 6. Peak memory observation (cgroup v2 memory.peak, inside the ${MEMORY} limit; informational only, not a hard gate) ==="
PEAK_BYTES=$(docker exec "$CONTAINER" cat /sys/fs/cgroup/memory.peak 2>/dev/null || echo "unavailable")
if [ "$PEAK_BYTES" != "unavailable" ]; then
	PEAK_MB=$(awk -v b="$PEAK_BYTES" 'BEGIN { printf "%.1f", b / 1024 / 1024 }')
	echo "Peak memory (tesseract+pdftoppm single-job RSS, across all cells run so far in this container): ${PEAK_BYTES} bytes (~${PEAK_MB} MiB) of ${MEMORY} limit"
else
	echo "Peak memory: cgroup v2 memory.peak not available in this environment"
fi

echo
echo "=== 7. Image size + measurement-host architecture (Phase-23/32/36-style arch caveat) ==="
IMAGE_SIZE=$(docker image inspect "$IMAGE_TAG" --format '{{.Size}}')
IMAGE_SIZE_MB=$(awk -v b="$IMAGE_SIZE" 'BEGIN { printf "%.1f", b / 1024 / 1024 }')
HOST_ARCH=$(uname -m)
echo "Image size: ${IMAGE_SIZE} bytes (~${IMAGE_SIZE_MB} MiB)"
echo "Measurement host architecture: ${HOST_ARCH} -- production amd64 sec/page is expected to differ from this number, same caveat as av-rtf-measure.sh/audio-rtf-measure.sh"

echo
echo "=== 8. DERIVATION HELP (prints ONLY -- this script commits NO OCR_ENGINE_TIMEOUT/OCR_MAX_PAGES value) ==="
WORST_LINE=$(sort -k2,2 -n "$RESULTS_FILE" | tail -n1)
WORST_CELL=$(echo "$WORST_LINE" | awk '{ print $1 }')
WORST_P95=$(echo "$WORST_LINE" | awk '{ print $2 }')
EXAMPLE_TIMEOUT=$(awk -v pages="$OCR_TIMING_MAX_PAGES" -v p95="$WORST_P95" 'BEGIN { v = pages * p95 * 2.0; printf "%d", (v == int(v)) ? v : int(v) + 1 }')
echo "Worst-cell p95 sec/page observed this run: ${WORST_P95}s (cell=${WORST_CELL})"
echo "Formula: OCR_ENGINE_TIMEOUT = ceil(OCR_MAX_PAGES x worst_cell_p95_sec_per_page x 2.0 safety factor)"
echo "Worked example (informational only, using the compose ocr-worker's current PROVISIONAL OCR_MAX_PAGES=${OCR_TIMING_MAX_PAGES}): ceil(${OCR_TIMING_MAX_PAGES} x ${WORST_P95} x 2.0) = ${EXAMPLE_TIMEOUT}s"
echo "This MUST stay strictly under the ${RECONCILER_CAP_SECONDS}s (15m) RECONCILER_ACTIVE_STALE_AFTER global CAP (docker-compose.yml webhook-worker-1/-2) -- if the derived timeout would breach ${RECONCILER_CAP_SECONDS}s, the lever is LOWERING OCR_MAX_PAGES (Path-B, mirrors AUDIO_MAX_DURATION_SECONDS's/AV_MAX_DURATION_SECONDS's own NO-GO precedent, 32-03-SUMMARY.md/36-04-SUMMARY.md), never raising the global CAP and never copying a neighboring engine class's timeout (PITFALLS.md Pitfall 7)."
echo "NO value is committed by this script. The GO/NO-GO decision on the real docker-compose.yml OCR_ENGINE_TIMEOUT/OCR_MAX_PAGES is Plan 03's supervised, operator-reviewed step, reading the per-cell p95 RESULT lines printed above."

echo
echo "=== ALL GREEN: measurement integrity checks passed (build, container, cgroup read, corpus staging, timed matrix sweep x6 cells, per-cell p95, memory, image size all captured) ==="
echo "This script's exit code reflects MEASUREMENT INTEGRITY ONLY -- no RESULT line's p95_sec_per_page value, however high, causes a non-zero exit here. The GO/NO-GO on OCR_ENGINE_TIMEOUT/OCR_MAX_PAGES derived from these numbers is a separate, operator-reviewed decision (Plan 03)."
echo "peak_mem_bytes=${PEAK_BYTES} image_size_bytes=${IMAGE_SIZE} host_arch=${HOST_ARCH}"
