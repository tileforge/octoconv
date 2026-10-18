set -uo pipefail
S=/private/tmp/claude-501/-Users-apaderin-dev-octoconv/95661ab2-afd1-49be-a390-64117af2c14a/scratchpad
PY="$S/pxvenv/bin/python"
D=internal/convert/testdata/cad/render-bg
W="$(mktemp -d "${TMPDIR:-/tmp}/pixel-table.XXXXXX")"
trap 'rm -rf "$W"' EXIT
CGO_ENABLED=0 GOTOOLCHAIN=go1.26.4 go build -o "$W/cad-pixel-judge" ./scripts/cmd/cad-pixel-judge || exit 1
echo "HDR,numpy=$("$PY" -c 'import numpy; print(numpy.__version__)'),pillow=$("$PY" -c 'import PIL; print(PIL.__version__)'),python=$("$PY" --version 2>&1 | tr ' ' '_'),go=go1.26.4,pixelcheck_sha256=$(shasum -a 256 scripts/cad-measure/pixelcheck.py | cut -c1-12)"
field() { printf '%s\n' "$1" | tr ',' '\n' | sed -n "s/^$2=//p" | head -n 1; }
: > "$W/got"
for name in nauo4_iso_1280 linearslide_iso_256 w15_iso_256 degenerate_plate_front_1280 degenerate_rod_iso_256; do
  size=${name##*_}
  row="$name"
  for bg in opaque transparent; do
    f="$D/${name}_$bg.png"
    g=$("$W/cad-pixel-judge" --size "$size" --background "$bg" "$f" 2>/dev/null)
    gok=$(field "$g" ok); gmode=$(field "$g" mode)
    s=$("$PY" scripts/cad-measure/pixelcheck.py single "$f" 2>/dev/null | tail -n 1)
    sok=$(field "$s" ok)
    if [ "$bg" = transparent ]; then
      t=$("$PY" scripts/cad-measure/pixelcheck.py transparent "$f" 2>/dev/null | tail -n 1)
      tok=$(field "$t" ok); tft=$(field "$t" frac_transparent)
    else
      tok=-; tft=-
    fi
    if [ "$gok" = 1 ]; then gcell="1 $gmode"; else gcell=0; fi
    row="$row|$bg go=$gcell single=$sok transparent=$tok ft=$tft"
    echo "ROW,frame=${name}_$bg,go_ok=$gok,go_mode=$gmode,pixelcheck_single_ok=$sok,pixelcheck_transparent_ok=$tok,frac_transparent=$tft"
  done
  echo "$row" >> "$W/got"
done
cat > "$W/want" <<'WANT'
nauo4_iso_1280|opaque go=1 blob single=1 transparent=- ft=-|transparent go=1 blob single=1 transparent=0 ft=0.9577
linearslide_iso_256|opaque go=1 blob single=1 transparent=- ft=-|transparent go=1 blob single=1 transparent=1 ft=0.7218
w15_iso_256|opaque go=1 thin single=0 transparent=- ft=-|transparent go=1 thin single=0 transparent=0 ft=0.9976
degenerate_plate_front_1280|opaque go=1 thin single=0 transparent=- ft=-|transparent go=1 thin single=0 transparent=0 ft=
degenerate_rod_iso_256|opaque go=0 single=0 transparent=- ft=-|transparent go=0 single=0 transparent=0 ft=
WANT
awk -F'|' 'NR == FNR { w[$1] = $0; next } { g = $0; sub(/ ft=[0-9.]*$/, " ft=", g); if ($0 != w[$1] && g != w[$1]) bad = 1 } END { exit bad }' "$W/want" "$W/got"
cmp_rc=$?
echo "COMPARED_WITH,source=71-VERIFICATION.md table of the 2026-10-17 pass (ft given there only for nauo4, linearslide and w15)"
if [ "$cmp_rc" = 0 ]; then echo PIXEL-TABLE-EQUAL; else diff "$W/want" "$W/got"; echo PIXEL-TABLE-DIFFERS; exit 1; fi
