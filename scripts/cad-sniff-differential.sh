#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO_ROOT"
. "$REPO_ROOT/scripts/cad-measure/common.sh"

OCP=7.9.3.1.1
VARIANT=pruned
SCALED=0
CPUS=2
MEMORY=4g
EVIDENCE="$REPO_ROOT/.planning/phases/72-engine-free-contract-foundations/evidence/72-sniff-divergence.txt"
GOLDEN="$REPO_ROOT/internal/convert/testdata/cad-sniff-golden.tsv"
MANIFEST="$REPO_ROOT/scripts/cad-corpus-manifest.tsv"
COMMITTED="$REPO_ROOT/internal/convert/testdata/cad"
FETCHED="$CAD_CACHE_DIR/fetched"
SCALED_DIR="$CAD_CACHE_DIR/scaled"

usage() {
  echo "usage: $0 [--variant pruned|full] [--scaled] [--evidence FILE] [--golden FILE]" >&2
  echo "  runs the Go sniffer and the real readers (OCP, ezdxf, cad_mesh.read_stl) over the corpus" >&2
  echo "  in the existing measurement image (never built or retagged here); exits 1 on a D-09 violation" >&2
  exit 64
}

while [ $# -gt 0 ]; do
  case "$1" in
    --variant) VARIANT=${2:?}; shift 2 ;;
    --scaled) SCALED=1; shift ;;
    --evidence) EVIDENCE=${2:?}; shift 2 ;;
    --golden) GOLDEN=${2:?}; shift 2 ;;
    -h|--help) usage ;;
    *) usage ;;
  esac
done

cad_variant_stage "$VARIANT" >/dev/null || exit 64

if ! command -v docker >/dev/null 2>&1; then
  echo "error: docker is required" >&2
  exit 69
fi

TAG_BEFORE="$(cad_image_tag "$OCP" "$VARIANT")"
if ! docker image inspect "$TAG_BEFORE" >/dev/null 2>&1; then
  echo "error: image $TAG_BEFORE is absent; this script never builds it" >&2
  exit 69
fi
cad_require_native "$TAG_BEFORE"
TAG="$TAG_BEFORE"

WORK="$(mktemp -d "${TMPDIR:-/tmp}/cad-sniff-diff.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT
export GOTOOLCHAIN=go1.26.4
go build -o "$WORK/cad-sniff" ./scripts/cmd/cad-sniff

ALL="$WORK/all.tsv"
: > "$ALL"

policy_join() {
  awk -F'\t' -v OFS='\t' '
    function category(name,   b) {
      b = name
      sub(/^.*\//, "", b)
      sub(/\.[^.]*$/, "", b)
      if (b == "step_truncated" || b == "step_missing_header" || b == "iges_truncated" || b == "dxf_truncated" || b == "stl_binary_truncated" || b == "stl_ascii_truncated") return "truncated"
      if (b == "step_trailing_nul") return "trailing-nul"
      if (b == "step_bom" || b == "dxf_bom") return "bom"
      if (b == "dxf_binary") return "binary-dxf"
      if (b == "dwg_ac1027") return "dwg"
      if (b == "stl_binary_trailing_byte") return "binary-stl-trailing"
      if (b == "stl_ambiguous") return "ambiguous-stl"
      return ""
    }
    NR == FNR { rd[$1] = $2 OFS $3 OFS $4; next }
    {
      if (!($1 in rd)) { print "error: no reader row for " $1 > "/dev/stderr"; bad = 1; next }
      split(rd[$1], r, OFS)
      reader = r[1]; acc = r[2]; detail = r[3]
      sniff = ($5 != "-" && $6 == "-") ? 1 : 0
      if (acc == "1" && sniff) pol = "agree"
      else if (acc == "0" && !sniff) pol = "agree"
      else if (acc == "0" && sniff) pol = "later-stage"
      else if (sniff) pol = "later-stage"
      else {
        pol = "violation"
        if ($2 == "reject") {
          c = category($1)
          if (c != "") pol = "policy:" c
        }
      }
      print $1, $2, $3, $4, $5, $6, reader, acc, detail, pol
    }
    END { exit bad }
  ' "$1" "$2"
}

run_group() {
  local group=$1 dir=$2 sub=${3:-}
  [ -d "$dir" ] || { echo "skip: $group (no directory $dir)" >&2; return 0; }
  local files=() rels=() f
  while IFS= read -r f; do files+=("$f"); done < <(find "$dir" -type f ! -name '*.md' | LC_ALL=C sort)
  [ ${#files[@]} -gt 0 ] || { echo "skip: $group (no files)" >&2; return 0; }
  for f in "${files[@]}"; do rels+=("${f#"$dir"/}"); done
  "$WORK/cad-sniff" -group "$group" -root "$dir" "${files[@]}" > "$WORK/$group.sniff.tsv"
  docker run --rm --network none --cpus "$CPUS" --memory "$MEMORY" --memory-swap "$MEMORY" \
    -v "$REPO_ROOT/scripts:/opt/cad-sniff:ro" -v "$dir:/corpus:ro" --entrypoint /opt/venv/bin/python "$TAG" \
    -I -B /opt/cad-sniff/cad-sniff-reader.py "${rels[@]}" \
    | awk -F'\t' -v OFS='\t' -v g="$group" '{ $1 = g "/" $1; print }' > "$WORK/$group.reader.tsv"
  local want got
  want=${#files[@]}
  got=$(wc -l < "$WORK/$group.reader.tsv" | tr -d ' ')
  if [ "$got" != "$want" ]; then
    echo "error: group $group has $want files but $got reader rows; rerun, this is not a measurement" >&2
    exit 1
  fi
  if [ "$group" = fetched ]; then
    awk -F'\t' '
      NR == FNR { if (FNR > 1) want["fetched/" $1] = $5; next }
      {
        if (!($1 in want) || want[$1] != $3) { print "error: sha256 mismatch for " $1 > "/dev/stderr"; bad = 1 }
      }
      END { exit bad }
    ' "$MANIFEST" "$WORK/fetched.sniff.tsv" || exit 1
  fi
  policy_join "$WORK/$group.reader.tsv" "$WORK/$group.sniff.tsv" >> "$ALL"
}

run_group public "$COMMITTED/public"
run_group synthetic "$COMMITTED/synthetic"
run_group reject "$COMMITTED/reject"
run_group fetched "$FETCHED"
if [ "$SCALED" = 1 ]; then
  run_group scaled "$SCALED_DIR"
fi

TAG_AFTER="$(cad_image_tag "$OCP" "$VARIANT")"

mkdir -p "$(dirname "$EVIDENCE")" "$(dirname "$GOLDEN")"

{
  printf 'HDR,tool=cad-sniff-differential.sh\n'
  printf 'HDR,variant=%s\n' "$VARIANT"
  printf 'HDR,image_tag_before=%s\n' "$TAG_BEFORE"
  cad_evidence_header "$TAG" "$CPUS" "$MEMORY"
  awk -F'\t' '
    {
      d = $9; gsub(/,/, ";", d)
      printf "ROW,name=%s,group=%s,sha256=%s,bytes=%s,sniff_format=%s,sniff_rejection=%s,reader=%s,reader_accepts=%s,reader_detail=%s,policy=%s\n", $1, $2, $3, $4, $5, $6, $7, $8, d, $10
    }' "$ALL"
  awk -F'\t' '
    $10 != "agree" {
      d = $9; gsub(/,/, ";", d)
      if ($10 == "later-stage") why = "sniffer accepts, reader rejects (" d ")"
      else if ($10 == "violation") why = "reader accepts or has no reader, sniffer does not accept (" d "), D-09 violation"
      else why = "reader accepts or has no reader (" d "), sniffer rejects by policy"
      printf "DIVERGENCE,name=%s,sniff_format=%s,sniff_rejection=%s,reader=%s,reader_accepts=%s,policy=%s,reason=%s\n", $1, $5, $6, $7, $8, $10, why
    }' "$ALL"
  awk -F'\t' '
    { n++ }
    $10 == "agree" { a++ }
    $10 == "later-stage" { l++ }
    $10 ~ /^policy:/ { p++ }
    $10 == "violation" { v++ }
    END { printf "SUMMARY,rows=%d,agree=%d,later_stage=%d,policy=%d,violation=%d\n", n, a, l, p, v }' "$ALL"
  printf 'HDR,image_tag_after=%s\n' "$TAG_AFTER"
} > "$WORK/evidence.txt"

{
  printf 'name\tgroup\tsha256\tbytes\tsniff_format\tsniff_rejection\treader\treader_accepts\treader_detail\tpolicy\n'
  awk -F'\t' -v OFS='\t' '
    $2 == "public" { print 1, $0 }
    $2 == "synthetic" { print 2, $0 }
    $2 == "reject" { print 3, $0 }
    $2 == "fetched" { print 4, $0 }' "$ALL" | LC_ALL=C sort -t "$(printf '\t')" -k1,1n -k2,2 | cut -f2-
} > "$WORK/golden.tsv"

mv "$WORK/evidence.txt" "$EVIDENCE"
mv "$WORK/golden.tsv" "$GOLDEN"
echo "wrote $EVIDENCE and $GOLDEN" >&2

grep '^SUMMARY,' "$EVIDENCE" >&2
if [ "$TAG_BEFORE" != "$TAG_AFTER" ]; then
  echo "error: image tag changed during the run ($TAG_BEFORE -> $TAG_AFTER)" >&2
  exit 1
fi
if grep -q '^SUMMARY,.*violation=[1-9]' "$EVIDENCE"; then
  echo "error: D-09 violation: a real file is refused by the sniffer while its reader accepts it" >&2
  exit 1
fi
