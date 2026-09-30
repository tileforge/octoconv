#!/usr/bin/env bash
# verapdf-differential.sh -- phase 70 D-02: veraPDF in the NATIVE document image
# against veraPDF in the linux/amd64 (emulated) document image, on one corpus.
#
# Why this is RUN rather than argued: Dockerfile.document-worker pins only its
# verapdf build stage to linux/amd64 because /opt/verapdf holds jars and a shell
# script and no ELF object. That is a claim about bytes; what the phase needs is
# that the JVM output on the native runtime stage is the same verdict as on the
# emulated one, including on real soffice PDF/A output. So the corpus is built
# from real soffice exports made with the exact PDF/A filter suffix the worker
# uses (read from internal/convert/opts.go, never retyped), a real plain soffice
# PDF, the two committed non-compliant fixtures, and a corrupt PDF, and the same
# bytes go through both validators. This compares veraPDF, not soffice.
#
# What is compared per file: isCompliant, failedToParse and the set of failed
# <clause>/<testNumber> pairs. veraPDF's exit status is printed and never judged
# (exit 7 means both "non-compliant" and "could not parse").
#
# The script refuses to report a verdict it did not earn (exit 2): amd64 docker
# server (nothing would be emulated), wrong image architecture, an extractor
# that misreads a committed report, or a corpus that did not exercise a compliant
# file, a non-compliant file with failed rules and a failedToParse=1 file on both
# images. Exit 0 = DIVERGENCES: 0, exit 1 = divergences found.
#
# Env: KEEP_IMAGES=1 keeps both images, KEEP_CORPUS=1 keeps the corpus dir.
# Run from anywhere; macOS/BSD-safe (no GNU-only flags); diagnostics on stderr.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

log() { printf '%s\n' "$*" >&2; }
die() { log "FATAL: $*"; exit 2; }

SERVER_ARCH="$(docker version --format '{{.Server.Arch}}')"
case "$SERVER_ARCH" in
	amd64|x86_64) die "docker server is $SERVER_ARCH: nothing would be emulated here, so this run cannot prove D-02" ;;
esac

RUN_ID="$(date -u +%Y%m%dT%H%M%SZ)-$$"
NATIVE_IMG="octoconv-document-worker:diff-native-$RUN_ID"
AMD64_IMG="octoconv-document-worker:diff-amd64-$RUN_ID"
CORPUS="$(mktemp -d /tmp/verapdf-diff.XXXXXX)"
BUILDLOG="$(mktemp -d /tmp/verapdf-diff-build.XXXXXX)"
chmod 0777 "$CORPUS"

cleanup() {
	if [ "${KEEP_IMAGES:-0}" != 1 ]; then
		docker image rm -f "$NATIVE_IMG" "$AMD64_IMG" >/dev/null 2>&1 || true
	fi
	if [ "${KEEP_CORPUS:-0}" != 1 ]; then
		rm -rf "$CORPUS"
	else
		log "corpus kept at $CORPUS"
	fi
	rm -rf "$BUILDLOG"
}
trap cleanup EXIT

# ---- extractor -------------------------------------------------------------
# extract <report.xml> -> "<isCompliant>|<failedToParse>|<sorted failed rules>"
extract() {
	local f="$1" flat c p rules
	if [ ! -s "$f" ]; then
		printf 'ABSENT|ABSENT|\n'
		return 0
	fi
	flat="$(tr '\n' ' ' <"$f")"
	c="$(printf '%s' "$flat" | grep -o 'isCompliant="[^"]*"' | head -n 1 | sed 's/^isCompliant="//; s/"$//' || true)"
	p="$(printf '%s' "$flat" | grep -o 'failedToParse="[^"]*"' | head -n 1 | sed 's/^failedToParse="//; s/"$//' || true)"
	[ -n "$c" ] || c=ABSENT
	[ -n "$p" ] || p=ABSENT
	rules="$(printf '%s' "$flat" | grep -o '<rule [^>]*>' | grep ' status="failed"' | while IFS= read -r el; do
		cl="$(printf '%s' "$el" | sed -n 's/.* clause="\([^"]*\)".*/\1/p')"
		tn="$(printf '%s' "$el" | sed -n 's/.* testNumber="\([^"]*\)".*/\1/p')"
		printf '%s/%s\n' "$cl" "$tn"
	done | sort -u | tr '\n' ' ' | sed 's/ $//' || true)"
	printf '%s|%s|%s\n' "$c" "$p" "$rules"
}

# ---- extractor self-check on the two committed reports ---------------------
got="$(extract internal/convert/testdata/verapdf_compliant.mrr.xml)"
[ "$got" = "true|0|" ] || die "extractor misreads verapdf_compliant.mrr.xml: got '$got', want 'true|0|'"
got="$(extract internal/convert/testdata/verapdf_noncompliant.mrr.xml)"
[ "$got" = "false|0|6.2.4.3/2 6.6.2.1/1" ] || die "extractor misreads verapdf_noncompliant.mrr.xml: got '$got', want 'false|0|6.2.4.3/2 6.6.2.1/1'"
log "extractor self-check: OK (both committed reports read correctly)"

# ---- PDF/A suffix from the const -------------------------------------------
SUFFIX="$(sed -n 's/^const pdfaFilterOptionsSuffix = `\(.*\)`$/\1/p' internal/convert/opts.go)"
[ -n "$SUFFIX" ] || die "could not read pdfaFilterOptionsSuffix from internal/convert/opts.go"

# ---- builds ----------------------------------------------------------------
build() { # <tag> [extra docker build args...]
	local tag="$1"
	shift
	log "building $tag ..."
	if ! docker build -f Dockerfile.document-worker "$@" -t "$tag" . >"$BUILDLOG/$(printf '%s' "$tag" | tr ':/' '__').log" 2>&1; then
		cat "$BUILDLOG/$(printf '%s' "$tag" | tr ':/' '__').log" >&2
		die "build of $tag failed"
	fi
}
build "$NATIVE_IMG"
build "$AMD64_IMG" --platform linux/amd64

NATIVE_ARCH="$(docker image inspect "$NATIVE_IMG" --format '{{.Architecture}}')"
AMD64_ARCH="$(docker image inspect "$AMD64_IMG" --format '{{.Architecture}}')"
NATIVE_ID="$(docker image inspect "$NATIVE_IMG" --format '{{.Id}}')"
AMD64_ID="$(docker image inspect "$AMD64_IMG" --format '{{.Id}}')"
DIRTY="$(git status --porcelain --untracked-files=no | wc -l | tr -d ' ')"
echo "run: $RUN_ID"
echo "commit: $(git rev-parse HEAD) (tracked-dirty files: $DIRTY)"
echo "docker server arch: $SERVER_ARCH"
echo "native image: $NATIVE_IMG id=$NATIVE_ID arch=$NATIVE_ARCH"
echo "amd64  image: $AMD64_IMG id=$AMD64_ID arch=$AMD64_ARCH"
echo "pdfa suffix: $SUFFIX"
[ "$NATIVE_ARCH" = "$SERVER_ARCH" ] || die "native image architecture is $NATIVE_ARCH, docker server is $SERVER_ARCH"
[ "$AMD64_ARCH" = "amd64" ] || die "amd64 image architecture is $AMD64_ARCH, want amd64"

# ---- corpus: real soffice exports made in the NATIVE image -----------------
export_pdf() { # <src> <filter> <with-suffix 0|1> <out-name>
	local src="$1" filter="$2" withsuffix="$3" out="$4" base wd target
	base="$(basename "$src")"
	wd="$CORPUS/work-${out%.pdf}"
	mkdir -p "$wd"
	chmod 0777 "$wd"
	cp "$src" "$wd/$base"
	chmod 0666 "$wd/$base"
	target="pdf:$filter"
	[ "$withsuffix" = 1 ] && target="$target:$SUFFIX"
	log "soffice export -> $out ($filter suffix=$withsuffix)"
	docker run --rm --entrypoint soffice -v "$CORPUS:/corpus" "$NATIVE_IMG" \
		--headless --invisible --nocrashreport --nodefault --nologo --nofirststartwizard --norestore \
		"-env:UserInstallation=file:///corpus/profile-${out%.pdf}" \
		--convert-to "$target" --outdir "/corpus/work-${out%.pdf}" "/corpus/work-${out%.pdf}/$base" >&2 || true
	local produced="$wd/${base%.*}.pdf"
	[ -s "$produced" ] || die "soffice produced no $produced for $out"
	[ "$(head -c 5 "$produced")" = "%PDF-" ] || die "$produced does not start with %PDF-"
	cp "$produced" "$CORPUS/$out"
}

export_pdf internal/e2e/testdata/sample.docx writer_pdf_Export 1 soffice_docx_pdfa.pdf
export_pdf internal/e2e/testdata/sample.xlsx calc_pdf_Export 1 soffice_xlsx_pdfa.pdf
export_pdf internal/e2e/testdata/sample.pptx impress_pdf_Export 1 soffice_pptx_pdfa.pdf
export_pdf internal/e2e/testdata/sample.docx writer_pdf_Export 0 soffice_docx_plain.pdf
cp internal/convert/testdata/verapdf_noncompliant.pdf "$CORPUS/fixture_noncompliant.pdf"
cp internal/e2e/testdata/verapdf_noncompliant_marker.pdf "$CORPUS/fixture_noncompliant_marker.pdf"
printf '%%PDF-1.4\n1 0 obj\n<< >>\n' >"$CORPUS/corrupt.pdf"
chmod 0644 "$CORPUS"/*.pdf

FILES="soffice_docx_pdfa.pdf soffice_xlsx_pdfa.pdf soffice_pptx_pdfa.pdf soffice_docx_plain.pdf fixture_noncompliant.pdf fixture_noncompliant_marker.pdf corrupt.pdf"

# ---- run veraPDF in both images --------------------------------------------
OUT="$CORPUS/out"
mkdir -p "$OUT/native" "$OUT/amd64"
run_vera() { # <label> <image> [platform args...]
	local label="$1" img="$2" f rc
	shift 2
	for f in $FILES; do
		rc=0
		docker run --rm "$@" --entrypoint verapdf -v "$CORPUS:/corpus:ro" "$img" \
			-f 2b --format xml "/corpus/$f" >"$OUT/$label/$f.xml" 2>"$OUT/$label/$f.err" || rc=$?
		echo "verapdf exit status ($label, $f): $rc (printed, not judged)"
	done
}
run_vera native "$NATIVE_IMG"
run_vera amd64 "$AMD64_IMG" --platform linux/amd64

# ---- non-vacuity -----------------------------------------------------------
nonvacuity() { # <label>
	local label="$1" f r c p rules ncomp=0 nnon=0 ncorrupt=0
	for f in $FILES; do
		r="$(extract "$OUT/$label/$f.xml")"
		c="${r%%|*}"
		rest="${r#*|}"
		p="${rest%%|*}"
		rules="${rest#*|}"
		[ "$c" = true ] && ncomp=$((ncomp + 1))
		[ "$c" = false ] && [ -n "$rules" ] && nnon=$((nnon + 1))
		[ "$f" = corrupt.pdf ] && [ "$p" = 1 ] && [ "$c" = ABSENT ] && ncorrupt=1
	done
	[ "$ncomp" -ge 1 ] || die "non-vacuity ($label): no file came out isCompliant=true"
	[ "$nnon" -ge 1 ] || die "non-vacuity ($label): no file came out non-compliant with failed rules"
	[ "$ncorrupt" -eq 1 ] || die "non-vacuity ($label): corrupt.pdf did not come out failedToParse=1 with isCompliant absent"
	log "non-vacuity ($label): OK (compliant=$ncomp non-compliant-with-rules=$nnon corrupt-parse-failure=$ncorrupt)"
}
nonvacuity native
nonvacuity amd64

# ---- compare ---------------------------------------------------------------
DIV=0
for f in $FILES; do
	n="$(extract "$OUT/native/$f.xml")"
	a="$(extract "$OUT/amd64/$f.xml")"
	if [ "$n" = "$a" ]; then
		echo "MATCH $f native=$n amd64=$a"
	else
		echo "DIVERGE $f native=$n amd64=$a"
		DIV=$((DIV + 1))
	fi
done
echo "DIVERGENCES: $DIV"
[ "$DIV" -eq 0 ] || exit 1
