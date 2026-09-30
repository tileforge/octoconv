#!/usr/bin/env bash
# document-sandbox-measure.sh -- phase 70: measure the document image's two
# engines (LibreOffice's soffice and veraPDF) under a REAL Landlock ruleset, in
# an image built from this tree, through the PRODUCTION wire format
# (`/usr/local/bin/document-worker __sandbox-exec rw:... sock:... ro:... -- <engine>`),
# and judge every check by CONTENT.
#
# Why document has its own driver rather than `sandbox-paths-measure.sh verify`:
#
#   1. The socket right is new in the wire format. scripts/cmd/sandbox-probe
#      builds a Paths value itself; only the production helper proves that a
#      `sock:` token reaches Apply and that the worker binary in the image
#      really is the helper.
#   2. verify judges sufficiency by exit status. SANDBOX-PATHS.md "Ловушки" §8
#      records why that is not enough (ghostscript's same-size skeleton), and
#      veraPDF exits 7 for a verdict AND for a denial, so its status is not
#      evidence either. Here nothing is judged by an exit status: the fixture
#      text inside the converted ODF/OOXML part, `%PDF-` plus `/GTS_PDFA` for the
#      PDF/A export, and veraPDF's `isCompliant` attribute decide.
#   3. veraPDF runs a JVM. A missing cgroup or /proc read does not fail a small
#      validation; it silently changes heap and CPU sizing inside a 1 GiB
#      container. Removing those reads cannot be judged by V1 alone, so V0
#      compares `java -XshowSettings:system` under the ruleset and without it.
#   4. A /proc entry is resolved by the helper in ITS OWN pid at Apply time, so
#      a `/proc/self` grant names the helper's pid. The JVM is that pid only if
#      the launcher (a dash script) execs it in place; V0 runs java directly and
#      cannot see a fork. V0L therefore starts the JVM the way production does,
#      through /opt/verapdf's launcher.
#   5. The environment scrub (engineEnv) closes the engine's OWN
#      /proc/self/environ. Whether a confined same-uid engine can open ANOTHER
#      process's environ (the worker's DATABASE_URL and S3 keys) depends on the
#      class's /proc grant: N3 measures it instead of assuming it.
#
# Every run gets a fresh host job directory mounted at /tmp/job (beneath /tmp, as
# in production, where the worker makes job directories under os.TempDir()) and a
# fresh host directory mounted at /outside holding byte-identical fixture copies
# that no grant covers.
#
# Checks (each prints PASS:/FAIL: with its reason and elapsed seconds, then a
# TMP-RESIDUE line naming what was left in the container's /tmp):
#   S1 docx->odt        content.xml carries the fixture text
#   S2 odt->docx        word/document.xml carries the fixture text
#   S3 xlsx->ods        content.xml carries "octoconv"
#   S4 pptx->odp        content.xml carries the fixture text
#   S5 docx->pdf PDF/A  starts %PDF- and carries /GTS_PDFA
#   V1 veraPDF on S5's output: isCompliant="true"
#   F1 a docx asking for Calibri, Cambria and Arial exports to a PDF that embeds
#      Carlito, Caladea and LiberationSans: fontconfig's metric aliases
#      (/etc/fonts) were read. A denial there changes layout, not text, so the
#      fixture text alone cannot see it
#   V2 veraPDF is not slower under the ruleset than without it (at most 3x plus
#      2s): a denied /dev/random makes the JVM's SecureRandom seed through its
#      threaded fallback, about 8s slower, and still returns the right verdict
#   V0 the JVM sees the same container: java -XshowSettings:system Operating
#      System Metrics and the ergonomic -XX:+PrintFlagsFinal values (heap sizes,
#      processor and GC thread counts) identical with and without the ruleset
#   V0L the same comparison with the JVM started by the production launcher
#      ($VERAPDF_BIN -f 2b --format xml on S5's PDF/A, JAVA_OPTS = the production
#      value plus -XshowSettings:system -XX:+PrintFlagsFinal), with and without
#      the ruleset, same limits
#   N1 soffice on a file outside every grant: no output carries the fixture text;
#      prints SOFFICE-DENIAL: loud|silent (the exit status is reported, never judged)
#   N2 veraPDF on a PDF/A outside every grant: no isCompliant verdict
#   N3 under veraPDF's ruleset (engine: /usr/bin/dash) /proc/1/environ, the
#      helper's parent's /proc/<ppid>/environ and /proc/self/root/etc/hostname
#      do not open. First an UNCONFINED run in the same container shape must show
#      both environ files hold a per-run canary, else the probe proves nothing
#      and N3 FAILS. PROC-OTHER: lines report, unjudged, whether other processes'
#      cmdline and status open; CONTROL: lines whether the engine's own
#      /proc/self/environ and cmdline open (what the grant leaves to itself)
#   C1 (only when the soffice set has a socket entry or /tmp read-write) S1 with
#      every sock: token and any rw:/tmp removed must NOT produce the fixture
#      text: the grant is load-bearing, not decorative
# Last line: `ALL PASS (<n> checks)` or `FAILURES PRESENT (<n> checks run)` (exit 1).
#
# Sets come from `scripts/conf05-crosscheck.sh --doc soffice verapdf` (the conf05
# blocks in SANDBOX-PATHS.md). Overrides, colon-separated, each REPLACING its
# list and announced by an `OVERRIDE:` line (an empty value is an empty list):
#   SOFFICE_RW  SOFFICE_SOCK  SOFFICE_RO  VERAPDF_RW  VERAPDF_RO
# Fixture overrides: FIXTURE_DOCX (path) and FIXTURE_TEXT (its expected text).
# Other env: KEEP_IMAGE=1 keeps the image; KEEP_WORK=1 keeps the scratch
# directory (per-check stdout/stderr, job directories); MEASURE_TMP=<dir> hosts the scratch
# directories (default /tmp -- it must be a path the docker host can mount);
# MEASURE_MEMORY / MEASURE_CPUS (default 1g / 2.0, the worker's compose limits);
# RUN_TIMEOUT seconds per engine run (default 60: a denied config read can make
# soffice wait forever instead of failing, and a hang is a failed check).
#
# Exit 2: the run could not establish what it measures (missing block, emulated
# image). Saves nothing into the repo. macOS/BSD-safe; diagnostics on stderr.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

log() { printf '%s\n' "$*" >&2; }
die() { log "FATAL: $*"; exit 2; }

command -v docker >/dev/null || die "docker not found"
command -v unzip >/dev/null || die "unzip not found (host-side content judgments)"
command -v perl >/dev/null || die "perl not found (sub-second timing)"

RUN_ID="$(date -u +%Y%m%dT%H%M%SZ)-$$"
IMG="octoconv-document-worker:measure70-$RUN_ID"
WORK="$(mktemp -d "${MEASURE_TMP:-/tmp}/document-measure.XXXXXX")"
chmod 0755 "$WORK"
LIMITS=(--cpus "${MEASURE_CPUS:-2.0}" --memory "${MEASURE_MEMORY:-1g}")

cleanup() {
	if [ "${KEEP_IMAGE:-0}" != 1 ]; then
		docker image rm -f "$IMG" >/dev/null 2>&1 || true
	else
		log "image kept: $IMG"
	fi
	if [ "${KEEP_WORK:-0}" = 1 ]; then
		log "scratch kept: $WORK"
	else
		rm -rf "$WORK"
	fi
}
trap cleanup EXIT

now() { perl -MTime::HiRes=time -e 'printf "%.2f", time'; }
elapsed() { awk -v a="$1" -v b="$2" 'BEGIN { printf "%.1f", b - a }'; }

# ---- build the image from THIS tree, refuse an emulated one -----------------
log "building $IMG from Dockerfile.document-worker"
if ! docker build -q -f Dockerfile.document-worker -t "$IMG" . >"$WORK/build.log" 2>&1; then
	tail -30 "$WORK/build.log" >&2
	die "image build failed"
fi
SERVER_ARCH="$(docker version --format '{{.Server.Arch}}')"
IMAGE_ARCH="$(docker image inspect "$IMG" --format '{{.Architecture}}')"
IMAGE_ID="$(docker image inspect "$IMG" --format '{{.Id}}')"
DIRT="$(git status --porcelain --untracked-files=no | wc -l | tr -d ' ')"
echo "commit: $(git rev-parse --short HEAD) tracked-dirt: $DIRT image: $IMAGE_ID arch: $IMAGE_ARCH server: $SERVER_ARCH"
if [ "$IMAGE_ARCH" != "$SERVER_ARCH" ]; then
	die "image is $IMAGE_ARCH on a $SERVER_ARCH server: emulated, Landlock unavailable, measuring would prove nothing"
fi

# ---- the sets ---------------------------------------------------------------
DOC_OUT="$WORK/doc.txt"
DOC_OK=1
scripts/conf05-crosscheck.sh --doc soffice verapdf >"$DOC_OUT" 2>"$WORK/doc.err" || DOC_OK=0

# doc_list <class> <ro|engine|rw|sock>: one path per line.
doc_list() {
	awk -v c="$1" -v k="$2" '
		/^DOC: / { cur = $2; next }
		cur == c && index($0, "  " k ": ") == 1 { print substr($0, length(k) + 5) }
	' "$DOC_OUT"
}

# load_set <ARRAY-name> <OVERRIDE-var-name> <class> <kind>
# An override REPLACES the doc list (and is announced); with no override and no
# readable doc block the run cannot proceed.
load_set() {
	local arr=$1 ovar=$2 class=$3 kind=$4 line list=""
	if [ "${!ovar+set}" = set ]; then
		echo "OVERRIDE: $ovar=${!ovar}"
		list="$(printf '%s' "${!ovar}" | tr ':' '\n')"
	else
		if [ "$DOC_OK" != 1 ]; then
			cat "$WORK/doc.err" >&2
			die "SANDBOX-PATHS.md conf05 blocks unreadable and $ovar not set"
		fi
		list="$(doc_list "$class" "$kind")"
	fi
	eval "$arr=()"
	while IFS= read -r line; do
		[ -n "$line" ] || continue
		eval "$arr+=(\"\$line\")"
	done <<EOF
$list
EOF
}

load_set SOF_RW SOFFICE_RW soffice rw
load_set SOF_SOCK SOFFICE_SOCK soffice sock
load_set SOF_RO SOFFICE_RO soffice ro
load_set VER_RW VERAPDF_RW verapdf rw
load_set VER_RO VERAPDF_RO verapdf ro

# Engine paths from INSIDE the image, appended as ro: tokens exactly as forJobRW
# adds its engines.
SOFFICE_BIN="$(docker run --rm --entrypoint sh "$IMG" -c 'command -v soffice')"
VERAPDF_BIN="$(docker run --rm --entrypoint sh "$IMG" -c 'command -v verapdf')"
[ -n "$SOFFICE_BIN" ] && [ -n "$VERAPDF_BIN" ] || die "engines not found in the image"
echo "engines: soffice=$SOFFICE_BIN verapdf=$VERAPDF_BIN"

# ---- fixtures ---------------------------------------------------------------
FIXDIR="internal/e2e/testdata"
DOCX="${FIXTURE_DOCX:-$FIXDIR/sample.docx}"
DOCX_TEXT="${FIXTURE_TEXT:-Hello from OctoConv E2E fixture}"
FIXTURE_TEXT_ALL="Hello from OctoConv E2E fixture"
[ "${FIXTURE_DOCX+set}" = set ] && echo "OVERRIDE: FIXTURE_DOCX=$FIXTURE_DOCX"
[ "${FIXTURE_TEXT+set}" = set ] && echo "OVERRIDE: FIXTURE_TEXT=$FIXTURE_TEXT"
for f in "$DOCX" "$FIXDIR/sample.odt" "$FIXDIR/sample.xlsx" "$FIXDIR/sample.pptx"; do
	[ -f "$f" ] || die "fixture missing: $f"
done
PDFA_SUFFIX="$(grep '^const pdfaFilterOptionsSuffix' internal/convert/opts.go | sed 's/^[^`]*`\(.*\)`$/\1/')"
[ -n "$PDFA_SUFFIX" ] || die "could not read the PDF/A filter suffix from internal/convert/opts.go"

# ---- runner -----------------------------------------------------------------
NJOB=0
JOB=""
OUT=""
# new_job <fixture-file> <name-in-job>: a fresh job dir (with the tmp and
# profile subdirectories the production worker makes) and a fresh /outside dir
# holding an identical copy of the fixture.
new_job() {
	NJOB=$((NJOB + 1))
	JOB="$WORK/job.$NJOB"
	OUT="$WORK/out.$NJOB"
	mkdir -p "$JOB/tmp" "$JOB/lo-profile" "$OUT"
	chmod 0777 "$JOB" "$JOB/tmp" "$JOB/lo-profile" "$OUT"
	cp "$1" "$JOB/$2"
	cp "$1" "$OUT/$2"
	chmod 0666 "$JOB/$2" "$OUT/$2"
}

# The image's own /tmp is not empty (apt's JRE post-install leaves a root-owned
# hsperfdata_root behind), so TMP-RESIDUE is what a run ADDED to that baseline.
BASE_TMP="$(docker run --rm --entrypoint sh "$IMG" -c 'ls -A /tmp' | sort)"
echo "image /tmp baseline: ${BASE_TMP:-<empty>}" | tr '\n' ' '; echo

RUN_ENV=()
RUN_CMD=()
RC=""
RESIDUE=""
# run_container <label>: runs RUN_CMD as a CHILD of an unconfined sh (the helper
# confines only itself), records its exit status and what /tmp holds afterwards.
run_container() {
	local label=$1
	rm -f "$JOB/.rc" "$JOB/.residue"
	docker run --rm "${LIMITS[@]}" -v "$JOB:/tmp/job" -v "$OUT:/outside" \
		-e RUN_TIMEOUT="${RUN_TIMEOUT:-60}" ${RUN_ENV[@]+"${RUN_ENV[@]}"} --entrypoint sh "$IMG" -c '
			timeout -k 3 "$RUN_TIMEOUT" "$@" >/tmp/job/.stdout 2>/tmp/job/.stderr
			echo $? >/tmp/job/.rc
			ls -A /tmp | grep -vx job >/tmp/job/.residue || true
		' sh "${RUN_CMD[@]}" >/dev/null 2>&1 || true
	RC="$(cat "$JOB/.rc" 2>/dev/null || echo '?')"
	RESIDUE="$(sort "$JOB/.residue" 2>/dev/null | comm -23 - <(printf '%s\n' "$BASE_TMP") | tr '\n' ' ' || true)"
	[ -n "${RESIDUE// /}" ] || RESIDUE="none"
	cp "$JOB/.stdout" "$WORK/$label.stdout" 2>/dev/null || : >"$WORK/$label.stdout"
	cp "$JOB/.stderr" "$WORK/$label.stderr" 2>/dev/null || : >"$WORK/$label.stderr"
	echo "TMP-RESIDUE $label: $RESIDUE"
}

# tokens <class> [nosock]: the helper's ruleset tokens.
helper_tokens() {
	local class=$1 nosock=${2:-} p
	echo "rw:/tmp/job"
	if [ "$class" = soffice ]; then
		for p in ${SOF_RW[@]+"${SOF_RW[@]}"}; do
			if [ -n "$nosock" ] && [ "$p" = /tmp ]; then continue; fi
			echo "rw:$p"
		done
		if [ -z "$nosock" ]; then
			for p in ${SOF_SOCK[@]+"${SOF_SOCK[@]}"}; do echo "sock:$p"; done
		fi
		for p in ${SOF_RO[@]+"${SOF_RO[@]}"}; do echo "ro:$p"; done
		echo "ro:$SOFFICE_BIN"
	else
		for p in ${VER_RW[@]+"${VER_RW[@]}"}; do echo "rw:$p"; done
		for p in ${VER_RO[@]+"${VER_RO[@]}"}; do echo "ro:$p"; done
		echo "ro:$VERAPDF_BIN"
	fi
}

# soffice_cmd <input> <convert-to> [nosock]: sets RUN_ENV and RUN_CMD for one
# production-shaped soffice run.
soffice_cmd() {
	local input=$1 convert_to=$2 nosock=${3:-} t
	RUN_ENV=(-e TMPDIR=/tmp/job/tmp)
	RUN_CMD=(/usr/local/bin/document-worker __sandbox-exec)
	while IFS= read -r t; do RUN_CMD+=("$t"); done < <(helper_tokens soffice "$nosock")
	RUN_CMD+=(-- "$SOFFICE_BIN" --headless --invisible --nocrashreport --nodefault
		--nologo --nofirststartwizard --norestore
		-env:UserInstallation=file:///tmp/job/lo-profile
		--convert-to "$convert_to" --outdir /tmp/job "$input")
}

# verapdf_cmd <input> [extra JAVA_OPTS]
verapdf_cmd() {
	local t
	RUN_ENV=(-e "JAVA_OPTS=-Djava.io.tmpdir=/tmp/job/tmp -XX:-UsePerfData${2:+ $2}")
	RUN_CMD=(/usr/local/bin/document-worker __sandbox-exec)
	while IFS= read -r t; do RUN_CMD+=("$t"); done < <(helper_tokens verapdf)
	RUN_CMD+=(-- "$VERAPDF_BIN" -f 2b --format xml "$1")
}

# ---- verdicts ---------------------------------------------------------------
NCHECKS=0
NFAIL=0
pass() { NCHECKS=$((NCHECKS + 1)); echo "PASS: $1 ($2s) $3"; }
failed() { NCHECKS=$((NCHECKS + 1)); NFAIL=$((NFAIL + 1)); echo "FAIL: $1 ($2s) $3"; }

# part_has <zip> <part> <text>: the text is inside that part of the archive.
part_has() { unzip -p "$1" "$2" 2>/dev/null | grep -qF -- "$3"; }

# soffice_check <id> <fixture> <in-name> <convert-to> <out-name> <part> <text>
soffice_check() {
	local id=$1 fixture=$2 inname=$3 convert_to=$4 outname=$5 part=$6 text=$7 t0 t1
	new_job "$fixture" "$inname"
	soffice_cmd "/tmp/job/$inname" "$convert_to"
	t0=$(now); run_container "$id"; t1=$(now)
	LAST_ELAPSED="$(elapsed "$t0" "$t1")"
	if [ -f "$JOB/$outname" ] && part_has "$JOB/$outname" "$part" "$text"; then
		pass "$id" "$(elapsed "$t0" "$t1")" "$outname/$part carries \"$text\" (exit $RC, not judged)"
	else
		failed "$id" "$(elapsed "$t0" "$t1")" "no $outname/$part carrying \"$text\" (exit $RC): $(head -c 300 "$WORK/$id.stderr" | tr '\n' ' ')"
	fi
}

LAST_ELAPSED=0
TIME_S1=0; TIME_S5=0; TIME_V1=0; TIME_V2=0; TIME_N1=0; TIME_N2=0

soffice_check S1 "$DOCX" in.docx "odt:writer8" in.odt content.xml "$DOCX_TEXT"
TIME_S1="$LAST_ELAPSED"

soffice_check S2 "$FIXDIR/sample.odt" in.odt "docx:MS Word 2007 XML" in.docx word/document.xml "$FIXTURE_TEXT_ALL"
soffice_check S3 "$FIXDIR/sample.xlsx" in.xlsx "ods:calc8" in.ods content.xml "octoconv"
soffice_check S4 "$FIXDIR/sample.pptx" in.pptx "odp:impress8" in.odp content.xml "$FIXTURE_TEXT_ALL"

# S5: PDF/A export.
new_job "$DOCX" in.docx
soffice_cmd /tmp/job/in.docx "pdf:writer_pdf_Export:$PDFA_SUFFIX"
t0=$(now); run_container S5; t1=$(now)
S5_PDF="$WORK/s5.pdf"
rm -f "$S5_PDF"
if [ -f "$JOB/in.pdf" ] && [ "$(head -c 5 "$JOB/in.pdf")" = "%PDF-" ] && grep -aq '/GTS_PDFA' "$JOB/in.pdf"; then
	cp "$JOB/in.pdf" "$S5_PDF"
	pass S5 "$(elapsed "$t0" "$t1")" "in.pdf starts %PDF- and carries /GTS_PDFA (exit $RC, not judged)"
else
	failed S5 "$(elapsed "$t0" "$t1")" "no PDF/A output (exit $RC): $(head -c 300 "$WORK/S5.stderr" | tr '\n' ' ')"
fi
TIME_S5="$(elapsed "$t0" "$t1")"

# F1: metric-compatible font substitution (fontconfig's aliases under /etc/fonts).
FONTS_DOCX="$WORK/fonts.docx"
FDIR="$WORK/fonts-src"
mkdir -p "$FDIR/_rels" "$FDIR/word"
cat >"$FDIR/[Content_Types].xml" <<'XML'
<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>
XML
cat >"$FDIR/_rels/.rels" <<'XML'
<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>
XML
cat >"$FDIR/word/document.xml" <<'XML'
<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri"/></w:rPr><w:t>octoconv font calibri</w:t></w:r></w:p><w:p><w:r><w:rPr><w:rFonts w:ascii="Cambria" w:hAnsi="Cambria"/></w:rPr><w:t>octoconv font cambria</w:t></w:r></w:p><w:p><w:r><w:rPr><w:rFonts w:ascii="Arial" w:hAnsi="Arial"/></w:rPr><w:t>octoconv font arial</w:t></w:r></w:p></w:body></w:document>
XML
(cd "$FDIR" && zip -X -q "$FONTS_DOCX" '[Content_Types].xml' _rels/.rels word/document.xml)
new_job "$FONTS_DOCX" in.docx
soffice_cmd /tmp/job/in.docx "pdf:writer_pdf_Export"
t0=$(now); run_container F1; t1=$(now)
missing=""
for font in Carlito Caladea LiberationSans; do
	grep -aq "$font" "$JOB/in.pdf" 2>/dev/null || missing="$missing $font"
done
if [ -f "$JOB/in.pdf" ] && [ -z "$missing" ]; then
	pass F1 "$(elapsed "$t0" "$t1")" "in.pdf embeds Carlito, Caladea and LiberationSans for Calibri, Cambria and Arial (exit $RC, not judged)"
else
	failed F1 "$(elapsed "$t0" "$t1")" "metric-compatible substitution missing:${missing:- (no PDF)} (exit $RC): $(head -c 200 "$WORK/F1.stderr" | tr '\n' ' ')"
fi

# V1: veraPDF on S5's output, inside the job directory.
if [ -f "$S5_PDF" ]; then
	new_job "$S5_PDF" in.pdf
	verapdf_cmd /tmp/job/in.pdf
	t0=$(now); run_container V1; t1=$(now)
	if grep -q 'isCompliant="true"' "$WORK/V1.stdout"; then
		pass V1 "$(elapsed "$t0" "$t1")" "isCompliant=\"true\" (exit $RC, not judged)"
	else
		failed V1 "$(elapsed "$t0" "$t1")" "no isCompliant=\"true\" (exit $RC): $(head -c 400 "$WORK/V1.stdout" | tr '\n' ' ')"
	fi
	TIME_V1="$(elapsed "$t0" "$t1")"

	# V2: the same validation without any ruleset, timed against V1.
	new_job "$S5_PDF" in.pdf
	RUN_ENV=(-e "JAVA_OPTS=-Djava.io.tmpdir=/tmp/job/tmp -XX:-UsePerfData")
	RUN_CMD=("$VERAPDF_BIN" -f 2b --format xml /tmp/job/in.pdf)
	t0=$(now); run_container V2-unconfined; t1=$(now)
	TIME_V2="$(elapsed "$t0" "$t1")"
	if awk -v a="$TIME_V1" -v b="$TIME_V2" 'BEGIN { exit !(a <= 3 * b + 2) }'; then
		pass V2 "$TIME_V1" "veraPDF under the ruleset ${TIME_V1}s vs ${TIME_V2}s without (limit 3x + 2s)"
	else
		failed V2 "$TIME_V1" "veraPDF under the ruleset took ${TIME_V1}s vs ${TIME_V2}s without: a denied read is being retried or timed out"
	fi
else
	failed V1 0 "S5 produced no PDF/A to validate"
fi

# V0: the JVM sees the same container with and without the ruleset: its cgroup
# metrics and the ergonomic values it derives from them and from the CPU count.
jvm_view() {
	awk '/Operating System Metrics/ { f = 1; print; next } f && /^[^ \t]/ { f = 0 } f'
}
jvm_flags() {
	grep -E '^ *[a-z_]+ +(MaxHeapSize|InitialHeapSize|MaxRAM|ActiveProcessorCount|ParallelGCThreads|ConcGCThreads|CICompilerCount|UseContainerSupport|UseTransparentHugePages|UseG1GC|UseSerialGC) ' | sed 's/  */ /g' || true
}
metrics() { tee "$WORK/jvm.raw" | jvm_view; jvm_flags <"$WORK/jvm.raw"; }
new_job "$DOCX" in.docx
BASE_METRICS="$(docker run --rm "${LIMITS[@]}" --entrypoint java "$IMG" -XshowSettings:system -XX:+PrintFlagsFinal -version 2>&1 | metrics)"
RUN_ENV=()
RUN_CMD=(/usr/local/bin/document-worker __sandbox-exec)
while IFS= read -r t; do RUN_CMD+=("$t"); done < <(helper_tokens verapdf)
RUN_CMD+=(-- /usr/bin/java -XshowSettings:system -XX:+PrintFlagsFinal -version)
t0=$(now); run_container V0; t1=$(now)
SANDBOX_METRICS="$(cat "$WORK/V0.stderr" "$WORK/V0.stdout" | metrics)"
if [ -n "$BASE_METRICS" ] && [ "$BASE_METRICS" = "$SANDBOX_METRICS" ]; then
	pass V0 "$(elapsed "$t0" "$t1")" "JVM cgroup metrics and ergonomic flags identical ($(printf '%s\n' "$BASE_METRICS" | grep -c .) lines)"
else
	failed V0 "$(elapsed "$t0" "$t1")" "JVM view differs under the ruleset -- without: [$(printf '%s' "$BASE_METRICS" | tr '\n' ';')] with: [$(printf '%s' "$SANDBOX_METRICS" | tr '\n' ';')]"
fi

# V0L: the same comparison through the PRODUCTION launcher. The launcher is a
# dash script that ends in `exec "$JAVACMD" $JAVA_OPTS ...`; a /proc/self grant
# is resolved in the helper's pid, which is the JVM's only if that exec happens
# in place. V0 starts java directly and cannot see a fork.
if [ -f "$S5_PDF" ]; then
	JVM_EXTRA="-XshowSettings:system -XX:+PrintFlagsFinal"
	new_job "$S5_PDF" in.pdf
	RUN_ENV=(-e "JAVA_OPTS=-Djava.io.tmpdir=/tmp/job/tmp -XX:-UsePerfData $JVM_EXTRA")
	RUN_CMD=("$VERAPDF_BIN" -f 2b --format xml /tmp/job/in.pdf)
	run_container V0L-unconfined
	BASE_METRICS_L="$(cat "$WORK/V0L-unconfined.stderr" "$WORK/V0L-unconfined.stdout" | metrics)"
	new_job "$S5_PDF" in.pdf
	verapdf_cmd /tmp/job/in.pdf "$JVM_EXTRA"
	t0=$(now); run_container V0L; t1=$(now)
	SANDBOX_METRICS_L="$(cat "$WORK/V0L.stderr" "$WORK/V0L.stdout" | metrics)"
	if [ -n "$BASE_METRICS_L" ] && [ "$BASE_METRICS_L" = "$SANDBOX_METRICS_L" ] && grep -q 'isCompliant="true"' "$WORK/V0L.stdout"; then
		pass V0L "$(elapsed "$t0" "$t1")" "JVM view through the production launcher identical ($(printf '%s\n' "$BASE_METRICS_L" | grep -c .) lines) and the verdict is still isCompliant=\"true\""
	else
		failed V0L "$(elapsed "$t0" "$t1")" "JVM view through the launcher differs under the ruleset -- without: [$(printf '%s' "$BASE_METRICS_L" | tr '\n' ';')] with: [$(printf '%s' "$SANDBOX_METRICS_L" | tr '\n' ';')] (exit $RC)"
	fi
else
	failed V0L 0 "S5 produced no PDF/A to validate"
fi

# N1: soffice pointed at an identical file outside every grant.
new_job "$DOCX" in.docx
rm -f "$JOB/in.docx" # the ONLY copy is /outside/in.docx
soffice_cmd /outside/in.docx "odt:writer8"
t0=$(now); run_container N1; t1=$(now)
TIME_N1="$(elapsed "$t0" "$t1")"
leaked=0
for f in "$JOB"/*.odt; do
	[ -f "$f" ] || continue
	if part_has "$f" content.xml "$DOCX_TEXT"; then leaked=1; fi
done
haveout=0
ls "$JOB"/*.odt >/dev/null 2>&1 && haveout=1
if [ "$RC" != 0 ] && [ "$haveout" = 0 ]; then
	echo "SOFFICE-DENIAL: loud (exit $RC, no output file) stderr: $(head -c 300 "$WORK/N1.stderr" | tr '\n' ' ')"
else
	echo "SOFFICE-DENIAL: silent (exit $RC, output file present: $haveout) stderr: $(head -c 300 "$WORK/N1.stderr" | tr '\n' ' ')"
fi
if [ "$leaked" = 0 ]; then
	pass N1 "$TIME_N1" "no output carries the fixture text although /outside/in.docx is identical to S1's input"
else
	failed N1 "$TIME_N1" "the outside file was converted: its text is in the output. The ruleset confines nothing"
fi

# N2: veraPDF pointed at an identical PDF/A outside every grant.
if [ -f "$S5_PDF" ]; then
	new_job "$S5_PDF" in.pdf
	rm -f "$JOB/in.pdf"
	verapdf_cmd /outside/in.pdf
	t0=$(now); run_container N2; t1=$(now)
	TIME_N2="$(elapsed "$t0" "$t1")"
	if grep -q 'isCompliant=' "$WORK/N2.stdout"; then
		failed N2 "$TIME_N2" "veraPDF produced a verdict on a file outside every grant"
	else
		msg="$(grep -o '<exceptionMessage>[^<]*' "$WORK/N2.stdout" | head -1 | sed 's/<exceptionMessage>//')"
		pass N2 "$TIME_N2" "no isCompliant verdict (exit $RC, not judged); exceptionMessage: ${msg:-<none>}"
	fi
else
	failed N2 0 "S5 produced no PDF/A to hide"
fi

# N3: can a confined engine open OTHER processes' environ? The worker (the
# helper's parent in production) and PID 1 hold DATABASE_URL and the S3 keys.
# A per-run canary in the container's environment stands in for them. The
# unconfined run in the same shape has to see the canary in both files, or a
# "denied" below would prove nothing.
CANARY="octoconv-canary-$(od -An -N8 -tx1 /dev/urandom | tr -d ' \n')"
N3_OPEN='for p in /proc/1/environ /proc/$PPID/environ /proc/self/root/etc/hostname; do
	if { true < "$p"; } 2>/dev/null; then echo "OPEN $p"; else echo "DENIED $p"; fi
done
for p in /proc/1/cmdline /proc/$PPID/cmdline /proc/$PPID/status; do
	if { true < "$p"; } 2>/dev/null; then echo "PROC-OTHER: $p opened"; else echo "PROC-OTHER: $p denied"; fi
done
for p in /proc/self/environ /proc/self/cmdline; do
	if { true < "$p"; } 2>/dev/null; then echo "CONTROL: $p opened"; else echo "CONTROL: $p denied"; fi
done
echo "PPID=$PPID"'
N3_SEE='for p in /proc/1/environ /proc/$PPID/environ; do
	if grep -qa "$OCTOCONV_CANARY" "$p" 2>/dev/null; then echo "CANARY-PRESENT $p"; else echo "CANARY-ABSENT $p"; fi
done
echo "PPID=$PPID"'
if [ -f "$S5_PDF" ]; then N3_FIXTURE="$S5_PDF"; else N3_FIXTURE="$DOCX"; fi
new_job "$N3_FIXTURE" in.pdf
RUN_ENV=(-e "OCTOCONV_CANARY=$CANARY")
RUN_CMD=(sh -c "$N3_SEE")
run_container N3-unconfined
cat "$WORK/N3-unconfined.stdout"
new_job "$N3_FIXTURE" in.pdf
RUN_ENV=(-e "OCTOCONV_CANARY=$CANARY")
RUN_CMD=(/usr/local/bin/document-worker __sandbox-exec)
while IFS= read -r t; do RUN_CMD+=("$t"); done < <(helper_tokens verapdf)
RUN_CMD+=(-- /usr/bin/dash -c "$N3_OPEN")
t0=$(now); run_container N3; t1=$(now)
cat "$WORK/N3.stdout"
if [ "$(grep -c '^CANARY-PRESENT ' "$WORK/N3-unconfined.stdout")" != 2 ]; then
	failed N3 "$(elapsed "$t0" "$t1")" "the unconfined probe did not see the canary in both /proc/1/environ and the parent's environ: a denial here would prove nothing"
elif [ "$(grep -cE '^(OPEN|DENIED) ' "$WORK/N3.stdout")" != 3 ]; then
	failed N3 "$(elapsed "$t0" "$t1")" "the confined probe printed no rows (exit $RC): $(head -c 300 "$WORK/N3.stderr" | tr '\n' ' ')"
elif grep -q '^OPEN ' "$WORK/N3.stdout"; then
	failed N3 "$(elapsed "$t0" "$t1")" "a confined engine opened: $(grep '^OPEN ' "$WORK/N3.stdout" | tr '\n' ' ')"
else
	pass N3 "$(elapsed "$t0" "$t1")" "with the canary proven present unconfined, /proc/1/environ, the parent's environ and /proc/self/root/etc/hostname all denied"
fi

# C1: the socket / /tmp grant is load-bearing.
has_tmp_rw=0
for p in ${SOF_RW[@]+"${SOF_RW[@]}"}; do [ "$p" = /tmp ] && has_tmp_rw=1; done
if [ "${#SOF_SOCK[@]}" -gt 0 ] || [ "$has_tmp_rw" = 1 ]; then
	new_job "$DOCX" in.docx
	soffice_cmd /tmp/job/in.docx "odt:writer8" nosock
	t0=$(now); run_container C1; t1=$(now)
	if [ -f "$JOB/in.odt" ] && part_has "$JOB/in.odt" content.xml "$DOCX_TEXT"; then
		failed C1 "$(elapsed "$t0" "$t1")" "S1 converted with the socket grant and any rw:/tmp removed: the grant is not load-bearing"
	else
		pass C1 "$(elapsed "$t0" "$t1")" "S1 fails without the socket/tmp grant (exit $RC): $(head -c 200 "$WORK/C1.stderr" | tr '\n' ' ')"
	fi
fi

# The preflight-shaped sequence: docx->odt, PDF/A export with validation, both
# negatives.
echo "PREFLIGHT-SHAPED-SECONDS: S1 $TIME_S1 + S5 $TIME_S5 + V1 $TIME_V1 + N1 $TIME_N1 + N2 $TIME_N2 = $(awk -v a="$TIME_S1" -v b="$TIME_S5" -v c="$TIME_V1" -v d="$TIME_N1" -v e="$TIME_N2" 'BEGIN { printf "%.1f", a + b + c + d + e }')"

if [ "$NFAIL" -eq 0 ]; then
	echo "ALL PASS ($NCHECKS checks)"
else
	echo "FAILURES PRESENT ($NCHECKS checks run)"
	exit 1
fi
