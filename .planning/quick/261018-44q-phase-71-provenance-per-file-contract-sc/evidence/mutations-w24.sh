set -u
Q=.planning/quick/261018-44q-phase-71-provenance-per-file-contract-sc
C=scripts/cad-measure/common.sh
G="$TMPDIR/common.green.sh"
cp "$C" "$G"

apply() {
  local from=$1 to=$2
  grep -qF -- "$from" "$G" || { echo "MUTATION_ANCHOR_MISSING,$from"; exit 1; }
  FROM="$from" TO="$to" perl -pe 's/\Q$ENV{FROM}\E/$ENV{TO}/' "$G" > "$C"
  cmp -s "$G" "$C" && { echo "MUTATION_NOT_APPLIED,$from"; exit 1; }
}

restore() {
  cp "$G" "$C"
  cmp "$G" "$C" && echo "restored=1"
}

run() {
  echo "-- proof"
  bash "$Q/evidence/w24-proof.sh"
  echo "proof_rc=$?"
  echo "-- rss-time self-test"
  scripts/cad-rss-time-measure.sh --self-test 2>&1 | grep -E '^SELF_TEST(_FAIL)?,' | grep -E 'select-|^SELF_TEST,'
  echo "-- image-trace self-test"
  scripts/cad-image-trace.sh self-test 2>&1 | grep -E '^SELF_TEST(_FAIL)?,' | grep -E 'corpus-|prune-(mesh|stl|inputs)|^SELF_TEST,'
}

echo "== W1 drop synthetic from the groups"
apply 'CAD_CORPUS_GROUPS="public synthetic"' 'CAD_CORPUS_GROUPS="public"'
run
restore

echo "== W2 the root comparison never matches"
apply '[ "$phys" = "$root" ]' '[ "$phys" = "$root/never" ]'
run
restore

echo "== W3 no reject exclusion"
apply "-type f ! -path '*/reject/*' \"\$@\"" '-type f "$@"'
run
restore
