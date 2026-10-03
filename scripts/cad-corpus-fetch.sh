#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

. scripts/cad-measure/common.sh

MANIFEST="scripts/cad-corpus-manifest.tsv"
LICENCE_RE='^(PD|CC0-1\.0|CC-BY-3\.0|CC-BY-4\.0|MIT|BSD-2-Clause|BSD-3-Clause)$'
CHECK=0
DEST="$CAD_CACHE_DIR/fetched"

usage() {
  echo "usage: $0 [--check] [--dest DIR]" >&2
  echo "  fetch every manifest row into DIR (default: \$OCTOCONV_CAD_CACHE/fetched) and verify sha256;" >&2
  echo "  --check verifies files already in DIR without touching the network" >&2
}

while [ $# -gt 0 ]; do
  case "$1" in
    --check) CHECK=1; shift ;;
    --dest)
      if [ $# -lt 2 ]; then usage; exit 64; fi
      DEST="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) usage; exit 64 ;;
  esac
done

digest_of() { shasum -a 256 "$1" | cut -d' ' -f1; }

TAB="$(printf '\t')"
TMP=""
cleanup() {
  if [ -n "$TMP" ]; then rm -rf "$TMP"; fi
}
trap cleanup EXIT

bad=0
rows=0
while IFS="$TAB" read -r name url archive_sha member sha bytes licence author source; do
  rows=$((rows + 1))
  if ! printf '%s' "$licence" | grep -Eq "$LICENCE_RE"; then
    echo "error: $name has licence '$licence', outside the allowed list (PD, CC0-1.0, CC-BY-3.0, CC-BY-4.0, MIT, BSD-2-Clause, BSD-3-Clause)" >&2
    exit 1
  fi
  case "$name" in
    */*|.*|"") echo "error: unsafe name '$name' in $MANIFEST" >&2; exit 1 ;;
  esac
done < <(tail -n +2 "$MANIFEST")

if [ "$rows" -eq 0 ]; then
  echo "error: $MANIFEST has no rows" >&2
  exit 1
fi

mkdir -p "$DEST"
TMP="$(mktemp -d "${TMPDIR:-/tmp}/cad-fetch.XXXXXX")"

fetch_archive() {
  local url=$1 want=$2 out got
  out="$TMP/archive-$want"
  if [ -f "$out" ]; then
    printf '%s\n' "$out"
    return 0
  fi
  if ! curl -fsSL --retry 3 -A 'Mozilla/5.0' -o "$out.part" "$url"; then
    rm -f "$out.part"
    echo "error: download failed: $url" >&2
    return 1
  fi
  got="$(digest_of "$out.part")"
  if [ "$got" != "$want" ]; then
    rm -f "$out.part"
    echo "error: archive sha256 mismatch for $url" >&2
    echo "  have $got" >&2
    echo "  want $want" >&2
    return 1
  fi
  mv "$out.part" "$out"
  printf '%s\n' "$out"
}

while IFS="$TAB" read -r name url archive_sha member sha bytes licence author source; do
  target="$DEST/$name"
  if [ -f "$target" ]; then
    got="$(digest_of "$target")"
    if [ "$got" = "$sha" ]; then
      echo "ok: $name"
      continue
    fi
    echo "error: $name present but sha256 mismatches the manifest" >&2
    echo "  have $got" >&2
    echo "  want $sha" >&2
    if [ "$CHECK" -eq 1 ]; then
      bad=1
      continue
    fi
    rm -f "$target"
  elif [ "$CHECK" -eq 1 ]; then
    echo "error: $name is absent from $DEST" >&2
    bad=1
    continue
  fi

  echo "fetching $name ($bytes bytes)" >&2
  if [ "$archive_sha" = "-" ]; then
    if ! curl -fsSL --retry 3 -o "$target.part" "$url"; then
      rm -f "$target.part"
      echo "error: download failed: $url" >&2
      exit 1
    fi
  else
    if ! archive_path="$(fetch_archive "$url" "$archive_sha")"; then
      exit 1
    fi
    if ! unzip -p "$archive_path" "$member" > "$target.part"; then
      rm -f "$target.part"
      echo "error: member '$member' not extractable from the archive for $name" >&2
      exit 1
    fi
  fi
  got="$(digest_of "$target.part")"
  if [ "$got" != "$sha" ]; then
    rm -f "$target.part" "$target"
    echo "error: sha256 mismatch for $name" >&2
    echo "  have $got" >&2
    echo "  want $sha" >&2
    exit 1
  fi
  mv "$target.part" "$target"
  echo "ok: $name"
done < <(tail -n +2 "$MANIFEST")

if [ "$bad" -ne 0 ]; then
  exit 1
fi
echo "corpus fetch ok: $rows files in $DEST"
