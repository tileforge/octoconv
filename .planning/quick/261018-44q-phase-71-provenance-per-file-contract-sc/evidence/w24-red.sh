set -euo pipefail
dir="$(pwd -P)/internal/convert/testdata/cad"
inv=$(find "$dir" -type f ! -name '*.md' ! -path '*/reject/*' | wc -l | tr -d ' ')
rss=$(find "$dir" -type f ! -path '*/reject/*' \( -iname '*.step' -o -iname '*.stp' -o -iname '*.iges' \
    -o -iname '*.igs' -o -iname '*.stl' -o -iname '*.dxf' \) | wc -l | tr -d ' ')
pairs=$(find "$dir" -type f \( -iname '*.step' -o -iname '*.stp' -o -iname '*.iges' -o -iname '*.igs' -o -iname '*.stl' -o -iname '*.dxf' \) |
    awk '{ e = tolower($0); sub(/^.*\./, "", e)
           if (e == "step" || e == "stp" || e == "iges" || e == "igs" || e == "stl") n++
           else if (e == "dxf") n += 2 }
         END { print n + 0 }')
echo "W24_RED,head=$(git rev-parse --short HEAD),inventory_committed_files=$inv,rss_time_list_cad_files=$rss,corpus_other_expected_pairs=$pairs,phase71_expected=37/37/49"
