set -euo pipefail
P=scripts/cad-render-probe.sh
D=internal/convert/testdata/cad/render-bg
H="$(mktemp -d "${TMPDIR:-/tmp}/judge-pair-harness.XXXXXX")"
trap 'rm -rf "$H"' EXIT
GO_TOOLCHAIN=go1.26.4
for f in build_judge go_verdict go_ok blank_png judge_pair; do
  awk -v f="$f" '$0 ~ "^" f "\\(\\) \\{" {on = 1} on {print} on && /^}/ {exit}' "$P" > "$H/$f.sh"
  [ -s "$H/$f.sh" ] || { echo "HARNESS_MISSING,function=$f"; exit 1; }
  . "$H/$f.sh"
done
build_judge "$H/judge"
LINES="$H/lines.txt"
: > "$LINES"
JUDGE_FRAMES=0; JUDGE_ERRORS=0; JUDGE_REAL=0; JUDGE_REAL_OK=0; JUDGE_W15=0; JUDGE_W15_OK=0; W15_FAILED=""
judge_pair nauo4.iso.1280.transparent "$D/nauo4_iso_1280_transparent.png" 1280 transparent real
judge_pair nauo4.iso.1280.opaque "$D/nauo4_iso_1280_opaque.png" 1280 opaque real
judge_pair linearslide.iso.256.transparent "$D/linearslide_iso_256_transparent.png" 256 transparent real
judge_pair w15.iso.256.opaque "$D/w15_iso_256_opaque.png" 256 opaque w15
judge_pair rod.iso.256.opaque "$D/degenerate_rod_iso_256_opaque.png" 256 opaque real
judge_pair missing.iso.256.opaque "$H/missing.png" 256 opaque real
blank_png "$H/blank_opaque.png" 1280 opaque
blank_png "$H/blank_transparent.png" 1280 transparent
printf 'BLANK,background=opaque,size=1280,%s\n' "$(go_verdict "$H/blank_opaque.png" 1280 opaque)" >> "$LINES"
printf 'BLANK,background=transparent,size=1280,%s\n' "$(go_verdict "$H/blank_transparent.png" 1280 transparent)" >> "$LINES"
cat "$LINES"
echo "COUNTERS,frames=$JUDGE_FRAMES,judge_errors=$JUDGE_ERRORS,real_frames=$JUDGE_REAL,real_go_ok=$JUDGE_REAL_OK,w15_frames=$JUDGE_W15,w15_go_ok=$JUDGE_W15_OK,w15_failed=[${W15_FAILED# }]"
if [ "$JUDGE_FRAMES" = 6 ] && [ "$JUDGE_ERRORS" = 1 ] && [ "$JUDGE_REAL" = 5 ] && [ "$JUDGE_REAL_OK" = 3 ] && [ "$JUDGE_W15" = 1 ] && [ "$JUDGE_W15_OK" = 1 ] \
  && [ "$(grep -c '^BLANK,.*,ok=0,mode=none,rule=J1,' "$LINES")" = 2 ] && ! grep -q '^PIX,\|^DIVERGENCE,' "$LINES"; then
  echo JUDGE-PAIR-HARNESS-OK
else
  echo JUDGE-PAIR-HARNESS-FAILED
  exit 1
fi
