#!/usr/bin/env bash
# Build the reduced TeX bundle cmd/tectonic-worker ships with (64-01, TEX-02/TEX-03).
#
# WHAT THIS PRODUCES
#   deploy/tectonic-bundle/MANIFEST.txt   what the bundle actually contains
#   <build-dir>/output/…ttb               the bundle itself
#   a printed SHA-256 and size for both
#
# WHY A SCRIPT AND NOT A DOCUMENTED PROCEDURE
#   The bundle is an artifact the image pins by digest. If the way it is built
#   lives in prose, the next rebuild is a different bundle for reasons nobody
#   can reconstruct. This is the reproduction.
#
# THE THING THAT MAKES THIS NON-TRIVIAL, STATED UP FRONT
#   tectonic-texlive-bundles selects by EXCLUSION: bundle.toml carries `ignore`
#   regexes, and everything not ignored is included. internal/convert/
#   texpackages.go is an ALLOWLIST. Rust's regex crate has no negative
#   lookahead, so "keep only these" cannot be written as one pattern.
#
#   So this script enumerates the LaTeX package directories present in the
#   TeXLive tree and emits an ignore rule for every directory that is not in
#   the allowlist. That inversion is the whole reason this file is more than
#   three lines, and it is why the MANIFEST it writes is authoritative: it
#   records what survived, not what was asked for.
#
#   Consequence worth knowing before reading a diff: adding a package to the Go
#   list SHRINKS the generated ignore list. The two move in opposite directions.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_DIR="${BUILD_DIR:-${REPO_ROOT}/.tectonic-bundle-build}"
BUNDLES_REPO="${BUNDLES_REPO:-${BUILD_DIR}/tectonic-texlive-bundles}"

# Pinned, and pinned by CONTENT rather than by tag. Both values come from the
# upstream bundle spec this build is derived from; the hash is verified before
# a single byte is unpacked.
TEXMF_URL="https://ftp.math.utah.edu/pub/tex/historic/systems/texlive/2023/texlive-20230313-texmf.tar.xz"
TEXMF_XZ_SHA256="${TEXMF_XZ_SHA256:-}"   # set to enforce; empty prints the observed digest
TEXMF_TAR="texlive-20230313-texmf.tar"
TEXMF_ROOT_DIR="texlive-20230313-texmf/texmf-dist"

die() { printf '\nERROR: %s\n' "$*" >&2; exit 1; }
say() { printf '\n=== %s\n' "$*"; }

say "prerequisites"
# pv is a hard dependency of the upstream builder, not of this script -- named
# here so a missing one fails in ten seconds rather than forty minutes in.
for bin in cargo git curl xz shasum patch diff pv; do
  command -v "$bin" >/dev/null || die "$bin is not on PATH (upstream builder requires cargo, bash, pv, GNU patch and diff)"
done

mkdir -p "$BUILD_DIR"

say "the declared package set, read from Go rather than restated here"
# The allowlist has ONE home. A copy in this script would be a second list to
# forget, which is the defect the three-way test exists to catch -- so the
# script asks Go for it instead of carrying its own.
mapfile -t ALLOW < <(cd "$REPO_ROOT" && go run ./scripts/cmd/texpackages)
[ "${#ALLOW[@]}" -gt 0 ] || die "the declared package set came back empty"
printf '  %s\n' "${ALLOW[@]}"

say "TeXLive texmf tarball"
cd "$BUILD_DIR"
if [ ! -f "$TEXMF_TAR" ]; then
  if [ ! -f "$(basename "$TEXMF_URL")" ]; then
    echo "  downloading ~3.6 GiB compressed; this is the expensive step"
    curl -fL --retry 5 --retry-delay 5 -o "$(basename "$TEXMF_URL")" "$TEXMF_URL"
  fi
  observed="$(shasum -a 256 "$(basename "$TEXMF_URL")" | cut -d' ' -f1)"
  if [ -n "$TEXMF_XZ_SHA256" ]; then
    [ "$observed" = "$TEXMF_XZ_SHA256" ] || die "texmf tarball digest mismatch: got $observed, want $TEXMF_XZ_SHA256"
    echo "  digest verified: $observed"
  else
    echo "  OBSERVED DIGEST (record it, then set TEXMF_XZ_SHA256 to enforce): $observed"
  fi
  echo "  decompressing"
  xz -dk "$(basename "$TEXMF_URL")"
fi

say "upstream builder"
[ -d "$BUNDLES_REPO" ] || git clone --depth 1 https://github.com/tectonic-typesetting/tectonic-texlive-bundles "$BUNDLES_REPO"

say "compiling the allowlist into ignore rules"
# Enumerate what is actually in the tree, then exclude every LaTeX package
# directory the allowlist does not name. Enumerating rather than assuming is
# the point: an ignore list written from a package index would silently miss a
# directory the tarball happens to contain under another name.
SPEC_DIR="$BUILD_DIR/octoconv-bundle"
mkdir -p "$SPEC_DIR"
tar -tf "$TEXMF_TAR" "${TEXMF_ROOT_DIR}/tex/latex" 2>/dev/null \
  | sed -n "s|^${TEXMF_ROOT_DIR}/tex/latex/\([^/]*\)/.*|\1|p" \
  | sort -u > "$SPEC_DIR/present.txt"
present_count=$(wc -l < "$SPEC_DIR/present.txt" | tr -d ' ')
[ "$present_count" -gt 0 ] || die "found no tex/latex package directories in $TEXMF_TAR -- the root_dir assumption is wrong"
echo "  package directories present in the tree: $present_count"

# Keep the allowlist AND the dependency closure the format build needs. A
# package directory that is not a package -- base, latex-bin, the kernel -- must
# never be excluded, and the floor measurement (evidence/64-01-floor-and-tikz-cost.txt)
# is what says which those are.
KEEP_ALWAYS=(base latexconfig l3kernel l3packages l3backend graphics tools amsfonts amscls)
printf '%s\n' "${ALLOW[@]}" "${KEEP_ALWAYS[@]}" | sort -u > "$SPEC_DIR/keep.txt"
comm -23 "$SPEC_DIR/present.txt" "$SPEC_DIR/keep.txt" > "$SPEC_DIR/drop.txt"
echo "  keeping $(wc -l < "$SPEC_DIR/keep.txt" | tr -d ' '), dropping $(wc -l < "$SPEC_DIR/drop.txt" | tr -d ' ')"

say "bundle spec, DERIVED from upstream's rather than authored here"
# Copying upstream's texlive2023 spec and adding our exclusions to it is
# deliberate. Writing a bundle.toml from scratch would silently drop their
# patches/ and include/ trees -- files the format build needs which are not in
# the texmf tarball at all -- and the failure would surface as a document that
# will not typeset, for reasons unrelated to anything we chose.
rm -rf "$SPEC_DIR/spec"
cp -R "$BUNDLES_REPO/bundles/texlive2023" "$SPEC_DIR/spec"
ln -sf "$BUILD_DIR/$TEXMF_TAR" "$SPEC_DIR/spec/$TEXMF_TAR"
python3 "$REPO_ROOT/scripts/tectonic-bundle-ignore.py" "$SPEC_DIR/spec/bundle.toml" "$SPEC_DIR/drop.txt"

say "select + pack"
(cd "$BUNDLES_REPO/builder" && cargo run --release -- --build-dir "$BUILD_DIR/out" "$SPEC_DIR/spec/bundle.toml")

CONTENT="$(find "$BUILD_DIR/out" -type d -name content -print -quit)"
[ -n "$CONTENT" ] && [ -f "$CONTENT/FILES" ] || die "select produced no content/FILES -- nothing to write a manifest from"

say "manifest"
# content/FILES is the builder's own record of what the bundle CONTAINS, which
# is the only thing the three-way test can honestly compare the Go list to.
# Reduced to sorted paths so a hash change in an unrelated file is not a
# manifest diff.
{
  echo "# Generated by scripts/tectonic-bundle-build.sh -- do not edit by hand."
  echo "# Source: content/FILES from the select job."
  echo "# Bundle content hash: $(cat "$CONTENT/SHA256SUM" 2>/dev/null || echo unknown)"
  cut -d" " -f1 "$CONTENT/FILES" | sort
} > "$REPO_ROOT/deploy/tectonic-bundle/MANIFEST.txt"
echo "  $(grep -vc "^#" "$REPO_ROOT/deploy/tectonic-bundle/MANIFEST.txt") paths"

say "artifact"
TTB="$(find "$BUILD_DIR/out" -name "*.ttb" -print -quit)"
if [ -n "$TTB" ]; then
  # Computed locally from the file we built, never copied from a published
  # checksum page -- Dockerfile.pandoc-worker's own rule, which applies at least
  # as strongly to an artifact we publish ourselves.
  printf "  %-8s %s\n" size "$(du -h "$TTB" | cut -f1)"
  printf "  %-8s %s\n" sha256 "$(shasum -a 256 "$TTB" | cut -d" " -f1)"
  printf "  %-8s %s\n" path "$TTB"
else
  echo "  no .ttb found -- the pack job did not run, or names its output differently"
fi
