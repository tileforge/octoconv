#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO_ROOT"
. "$REPO_ROOT/scripts/cad-measure/common.sh"

OCP=7.9.3.1.1
VARIANT=full
CPUS=2
MEMORY=4g
OUT=""

usage() {
  echo "usage: $0 [--out FILE]" >&2
  echo "  prints HDR, one CNT line per corpus file (committed, fetched, scaled) and the PRIVATE_CORPUS status" >&2
  echo "  of \$OCTOCONV_CAD_PRIVATE_CORPUS; private files are never named, only hashed and counted" >&2
  exit 64
}

while [ $# -gt 0 ]; do
  case "$1" in
    --out) OUT=${2:?}; shift 2 ;;
    -h|--help) usage ;;
    *) usage ;;
  esac
done

if ! command -v docker >/dev/null 2>&1; then
  echo "error: docker is required" >&2
  exit 69
fi

TAG="$(cad_build_image "$OCP" "$VARIANT")"
cad_require_native "$TAG"

WORK="$(mktemp -d "${TMPDIR:-/tmp}/cad-inventory.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT
BODY="$WORK/body.txt"
: > "$BODY"

count_container() {
  local mount=$1 names=$2 list=$3
  local flag=""
  if [ "$names" = "names" ]; then flag="--names"; fi
  sed 's|^|/corpus/|' "$list" | docker run --rm -i --network none --cpus "$CPUS" --memory "$MEMORY" --memory-swap "$MEMORY" \
    -v "$mount:/corpus:ro" --entrypoint sh "$TAG" -c \
    "tr '\\n' '\\0' | xargs -0 -r /opt/venv/bin/python -I -B /opt/cad-measure/counters.py $flag"
}

public_group() {
  local group=$1 dir=$2 total list
  if [ ! -d "$dir" ]; then
    echo "error: group $group: $dir is missing; fetch or generate the corpus first, an inventory of nothing is not an inventory" >&2
    exit 1
  fi
  list="$WORK/list-$group.txt"
  cad_corpus_files "$dir" > "$list"
  total=$(wc -l < "$list" | tr -d ' ')
  if [ "$total" -lt 1 ]; then
    echo "error: group $group has no files in $dir; an inventory of nothing is not an inventory" >&2
    exit 1
  fi
  printf 'INV,group=%s,files=%s\n' "$group" "$total" >> "$BODY"
  if [ "$total" -gt 0 ]; then
    count_container "$dir" names "$list" > "$WORK/group.txt"
    got=$(grep -c '^CNT,' "$WORK/group.txt" || true)
    bad=$(grep -c '^CNT,error=' "$WORK/group.txt" || true)
    if [ "$got" != "$total" ] || [ "$bad" != "0" ]; then
      echo "error: group $group has $total files but $got CNT lines ($bad errors); rerun, this is not a measurement" >&2
      exit 1
    fi
    cat "$WORK/group.txt" >> "$BODY"
  fi
}

public_group committed "$REPO_ROOT/internal/convert/testdata/cad"
public_group fetched "$CAD_CACHE_DIR/fetched"
public_group scaled "$CAD_CACHE_DIR/scaled"

private_files=0
if [ -n "$CAD_PRIVATE_DIR" ] && [ -d "$CAD_PRIVATE_DIR" ]; then
  private_files=$(find "$CAD_PRIVATE_DIR" -type f | wc -l | tr -d ' ')
fi

if [ "$private_files" -eq 0 ]; then
  printf 'PRIVATE_CORPUS,files=0,status=реальный поток не замерен\n' >> "$BODY"
else
  printf 'PRIVATE_CORPUS,files=%s,status=measured\n' "$private_files" >> "$BODY"
  PRIVATE_CNT="$WORK/private.txt"
  PRIVATE_LIST="$WORK/list-private.txt"
  cad_corpus_files "$CAD_PRIVATE_DIR" > "$PRIVATE_LIST"
  count_container "$CAD_PRIVATE_DIR" anonymous "$PRIVATE_LIST" > "$PRIVATE_CNT"
  cat "$PRIVATE_CNT" >> "$BODY"
  counted=$(grep -c '^CNT,' "$PRIVATE_CNT" || true)
  unreadable=$(grep -c '^CNT,error=' "$PRIVATE_CNT" || true)
  printf 'PRIVATE_COUNTERS,files=%s,counted=%s,unreadable=%s\n' "$private_files" "$counted" "$unreadable" >> "$BODY"
  grep '^CNT,' "$PRIVATE_CNT" | grep -v '^CNT,error=' | awk -F, '
    {
      fmt = ""; b = ""
      for (i = 1; i <= NF; i++) {
        if ($i ~ /^fmt=/) fmt = substr($i, 5)
        if ($i ~ /^bytes=/) b = substr($i, 7)
      }
      if (fmt != "" && b != "") print fmt, b
    }' | sort -k1,1 -k2,2n | awk '
    function flush(   r50, r95, over, i) {
      if (n == 0) return
      r50 = int((n * 50 + 99) / 100)
      r95 = int((n * 95 + 99) / 100)
      over = 0
      for (i = 1; i <= n; i++) if (v[i] > 41943040) over++
      printf "PRIVATE_SIZES,fmt=%s,n=%d,min=%d,median=%d,p95=%d,max=%d,over_40MiB=%d\n", cur, n, v[1], v[r50], v[r95], v[n], over
    }
    {
      if ($1 != cur) { flush(); cur = $1; n = 0 }
      n++; v[n] = $2
    }
    END { flush() }' >> "$BODY"
fi

emit() {
  printf 'HDR,tool=cad-corpus-inventory.sh\n'
  cad_evidence_header "$TAG" "$CPUS" "$MEMORY"
  cat "$BODY"
}

if [ -n "$OUT" ]; then
  mkdir -p "$(dirname "$OUT")"
  emit > "$WORK/out.txt"
  mv "$WORK/out.txt" "$OUT"
  echo "wrote $OUT ($(grep -c '^CNT,' "$OUT") CNT lines)" >&2
else
  emit
fi
