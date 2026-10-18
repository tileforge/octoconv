set -uo pipefail
. scripts/cad-measure/common.sh
E=.planning/phases/71-evaluation-gate/evidence
W="$(mktemp -d "${TMPDIR:-/tmp}/w24-proof.XXXXXX")"
trap 'rm -rf "$W"' EXIT
ok=1
check() {
  local name=$1 want=$2 got=$3
  if [ "$want" = "$got" ]; then echo "W24_CHECK,$name,ok=1,$4"; else echo "W24_CHECK,$name,ok=0,$4"; ok=0; fi
}

if ! cad_corpus_files "$CAD_CORPUS_ROOT" > "$W/files.txt"; then echo "W24_CHECK,select-files,ok=0,rc=nonzero"; ok=0; fi
if ! cad_corpus_cad_files "$CAD_CORPUS_ROOT" > "$W/cad.txt"; then echo "W24_CHECK,select-cad,ok=0,rc=nonzero"; ok=0; fi
nf=$(wc -l < "$W/files.txt" | tr -d ' ')
nc=$(wc -l < "$W/cad.txt" | tr -d ' ')
np=$(grep -c '^public/' "$W/files.txt")
ns=$(grep -c '^synthetic/' "$W/files.txt")

while IFS= read -r rel; do
  printf '%s,%s\n' "$(basename "$rel")" "$(shasum -a 256 "$CAD_CORPUS_ROOT/$rel" | cut -d' ' -f1)"
done < "$W/files.txt" | LC_ALL=C sort > "$W/a-got.txt"
awk '/^INV,group=committed,/{on = 1; next} /^INV,group=fetched,/{on = 0} on && /^CNT,/' "$E/71-03-corpus-inventory.txt" |
  awk -F, '{ n = ""; s = ""; for (i = 2; i <= NF; i++) { if ($i ~ /^name=/) n = substr($i, 6); if ($i ~ /^sha=/) s = substr($i, 5) } print n "," s }' |
  LC_ALL=C sort > "$W/a-want.txt"
na=$(wc -l < "$W/a-want.txt" | tr -d ' ')
check a-inventory-pairs-equal-71-03 "$(cat "$W/a-want.txt")" "$(cat "$W/a-got.txt")" "selected=$nf,public=$np,synthetic=$ns,phase71_cnt_lines=$na"

sed 's|.*/||' "$W/cad.txt" | LC_ALL=C sort -u > "$W/b-got.txt"
grep -E '^(ROW|SKIP),' "$E/71-04-helpers.txt" | grep -oE '(^|,)name=[^,]*' | sed 's/^,//; s/^name=//' | LC_ALL=C sort -u > "$W/b-want.txt"
nb=$(wc -l < "$W/b-want.txt" | tr -d ' ')
check b-ladder-names-equal-71-04 "$(cat "$W/b-want.txt")" "$(cat "$W/b-got.txt")" "selected=$nc,phase71_names=$nb"

awk '{ e = tolower($0); sub(/^.*\./, "", e); b = $0; sub(/^.*\//, "", b)
       if (e == "step" || e == "stp" || e == "iges" || e == "igs") print "mesh-" b
       else if (e == "stl") print b }' "$W/cad.txt" | LC_ALL=C sort > "$W/c-got.txt"
grep '^PRUNE_PNG,' "$E/71-07-prune.txt" | grep -oE 'input=[^,]*' | sed 's/^input=//' | grep -v '^fetched-' | LC_ALL=C sort -u > "$W/c-want.txt"
nc3=$(wc -l < "$W/c-got.txt" | tr -d ' ')
ncw=$(wc -l < "$W/c-want.txt" | tr -d ' ')
check c-prune-inputs-equal-71-07 "$(cat "$W/c-want.txt")" "$(cat "$W/c-got.txt")" "selected_3d=$nc3,phase71_corpus_inputs=$ncw"

pairs=$(awk '{ e = tolower($0); sub(/^.*\./, "", e)
               if (e == "step" || e == "stp" || e == "iges" || e == "igs" || e == "stl") n++
               else if (e == "dxf") n += 2 }
             END { print n + 0 }' "$W/cad.txt")
dxf=$(grep -ciE '\.dxf$' "$W/cad.txt")
check d-prune-other-pairs 49 "$pairs" "pairs=$pairs,3d=$nc3,dxf=$dxf"

walks=$(awk '
  FILENAME ~ /cad-corpus-inventory/ && /find[[:space:]]+(\/corpus|"\$dir")/ { print FILENAME ":" FNR }
  FILENAME ~ /cad-image-trace/ && /find[[:space:]]+("\$CORPUS"|"\$1")/ { print FILENAME ":" FNR }
  FILENAME ~ /cad-rss-time-measure/ && /find[[:space:]]+"\$1"/ { print FILENAME ":" FNR }
  /-iname[[:space:]]+.\*\.(step|stp|iges|igs|stl|dxf)./ { print FILENAME ":" FNR }
' scripts/cad-corpus-inventory.sh scripts/cad-rss-time-measure.sh scripts/cad-image-trace.sh | tr '\n' ' ')
uses=$(grep -c 'cad_corpus_files\|cad_corpus_cad_files' scripts/cad-corpus-inventory.sh scripts/cad-rss-time-measure.sh scripts/cad-image-trace.sh | tr '\n' ' ')
check e-no-corpus-walk-outside-common "" "$walks" "walks_outside_common=[${walks}],uses=[${uses}]"

if [ "$ok" = 1 ]; then echo W24-OK; else echo W24-FAILED; exit 1; fi
