set -u
S=/private/tmp/claude-501/-Users-apaderin-dev-octoconv/95661ab2-afd1-49be-a390-64117af2c14a/scratchpad
Q=.planning/quick/261018-44q-phase-71-provenance-per-file-contract-sc
CMDS="$S/v71-cmds.txt"
EV=.planning/phases/71-evaluation-gate/evidence
W="$(mktemp -d "${TMPDIR:-/tmp}/reverify.XXXXXX")"
export GOTOOLCHAIN=go1.26.4
fails=0

run() {
  local name=$1 want=$2 cmd=$3 rc
  bash -c "$cmd" > "$W/out" 2>&1
  rc=$?
  last=$(grep -v '^[[:space:]]*$' "$W/out" | tail -n 1 | cut -c1-200)
  if [ "$want" = rc0 ]; then [ "$rc" = 0 ] && st=PASS || st=FAIL
  elif [ "$want" = empty ]; then [ "$rc" = 0 ] && [ ! -s "$W/out" ] && st=PASS || st=FAIL
  else [ "$rc" = 0 ] && [ "$last" = "$want" ] && st=PASS || st=FAIL; fi
  [ "$st" = PASS ] || fails=$((fails + 1))
  printf 'CHECK,name=%s,status=%s,rc=%s,last=%s\n' "$name" "$st" "$rc" "$last"
  printf '  $ %s\n' "$(printf '%s' "$cmd" | cut -c1-300)"
}

gate() {
  run "$1" "$1" "$(sed -n "${2}p" "$CMDS")"
}

echo "HDR,head=$(git rev-parse HEAD),date_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ),python3=$(python3 --version 2>&1 | tr ' ' '_'),go=$(go env GOVERSION),tmpdir=$TMPDIR"
echo "HDR,dirty=[$(git status --porcelain | tr '\n' ';')]"

run gofmt empty 'gofmt -l .'
run go-vet empty 'GOTOOLCHAIN=go1.26.4 go vet ./...'

: > "$W/syntax"
while IFS= read -r f; do
  case "$f" in
    *.sh) bash -n "$f" 2>> "$W/syntax" || echo "FAIL $f" >> "$W/syntax" ;;
    *.py) python3 -c 'import ast, sys; ast.parse(open(sys.argv[1]).read())' "$f" 2>> "$W/syntax" || echo "FAIL $f" >> "$W/syntax" ;;
  esac
done < "$Q/evidence/scripts71.txt"
n=$(wc -l < "$Q/evidence/scripts71.txt" | tr -d ' ')
run script-syntax "files=$n,failures=0" "test ! -s '$W/syntax' && echo files=$n,failures=0"

run verdicts-self-test rc0 'scripts/cad-measure/verdicts.sh --self-test'
run rss-time-self-test rc0 'scripts/cad-rss-time-measure.sh --self-test 2>&1 | tail -n 1 | grep -E "^SELF_TEST,cases=[0-9]+,failed=0$"'
run image-trace-self-test rc0 'scripts/cad-image-trace.sh self-test 2>&1 | tail -n 1 | grep -E "^SELF_TEST,cases=[0-9]+,failed=0$"'
run vtk-keeplist-test rc0 'python3 scripts/cad-vtk-keeplist-test.py'
run deriver-self-check rc0 'python3 scripts/cad-derive-limits.py --self-check'

mkdir -p "$W/ev"
cp -R "$EV/." "$W/ev/"
run deriver-regenerates-limits-byte-for-byte "LIMITS-REGEN-IDENTICAL" "python3 scripts/cad-derive-limits.py '$W/ev' --pin 7.9.3.1.1 --out '$W/ev/71-06-limits.regen.txt' >/dev/null && cmp '$EV/71-06-limits.txt' '$W/ev/71-06-limits.regen.txt' && echo LIMITS-REGEN-IDENTICAL"
run deriver-refuses-without-pin "exit=2" "python3 scripts/cad-derive-limits.py '$W/ev' --out '$W/ev/nopin.txt' >/dev/null 2>&1; echo exit=\$?"
run LIMITS-OK LIMITS-OK "$(sed -n 16p "$CMDS" | sed "s|^D=.planning/phases/71-evaluation-gate/evidence |D='$W/ev' |")"

run corpus-gen-check-only rc0 'python3 scripts/cad-corpus-gen.py --check-only'
run corpus-fetch-check rc0 'scripts/cad-corpus-fetch.sh --check'
gate SELF-CHECK-FIRES 5
gate TAMPER-DETECTED 8

gate LOCKS-OK 1
gate LADDER-OK 14
gate MEASURES-OK 15
gate PRUNE-OK 17
gate D18-OK 19
gate LANDLOCK-OK 20
gate DECISION-OK 23

gate CORPUS-OK 6
gate PROVENANCE-COMPLETE 7
run provenance-go-test rc0 "GOTOOLCHAIN=go1.26.4 go test -count=1 -run '^TestCADProvenanceCoversEveryCommittedFile\$' ./internal/convert/"

run W24-proof W24-OK "bash $Q/evidence/w24-proof.sh"
run judge-png-proof JUDGE-PNG-OK "bash $Q/evidence/judge-png-proof.sh"
run pixel-table "PIXEL-TABLE-EQUAL" "bash $Q/evidence/pixel-table.sh > '$W/pixel-table.txt' && cp '$W/pixel-table.txt' $Q/evidence/pixel-table.txt && tail -n 1 '$W/pixel-table.txt'"

run phase71-evidence-untouched empty "git status --porcelain $EV"

for g in HASHES-OK TRACER-OK GEN-DETERMINISTIC INVENTORY-OK PRIVACY-OK HELPERS-OK RENDER-OK F3D-OK STALE-REFUSED RECHECK-OK SIZE-OK NO-WIRING; do
  echo "CHECK,name=$g,status=NOT_RUN,reason=needs_docker_or_a_live_stack"
done
rm -rf "$W"
echo "REVERIFY,failed=$fails"
