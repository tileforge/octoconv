#!/usr/bin/env bash
# cad-image-trace.sh -- Phase 71 (EVAL-01, EVAL-04) library trace, VTK pruning
# check and OCP wheel licence record for the CAD measurement image.
#
# Usage:
#   scripts/cad-image-trace.sh libs        [--out FILE]
#   scripts/cad-image-trace.sh prune-check [--out FILE]
#   scripts/cad-image-trace.sh licences    [--out FILE]
#   scripts/cad-image-trace.sh self-test
#
# libs         LD_DEBUG and /proc/self/maps of every operation on both OCP pins
#              (render, dxf and rsvg only on the pinned one), the DT_NEEDED chain,
#              wheel libraries never loaded, the generated VTK keep-list.
# prune-check  full and pruned image of one build: size, byte-identical renders,
#              the other helpers, and two stale keep-list cases.
# licences     wheel library inventory, upstream licences, D-18 outcome.
# self-test    the verdict functions of libs and prune-check on synthetic input; no docker.
#
# PIN, LP and PAR come from evidence/71-06-limits.txt (PIN_DECISION,
# THREADS_DECISION). An emulated image is not a measurement.
#
# Exit codes: 64 usage, 69 docker missing, 3 libs: the research expectation did
# not match (the evidence is written, a human decides), 1 a measurement failed.

set -euo pipefail
export LC_ALL=C

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO_ROOT"
. "$REPO_ROOT/scripts/cad-measure/common.sh"

EVID="$REPO_ROOT/.planning/phases/71-evaluation-gate/evidence"
CORPUS="$REPO_ROOT/internal/convert/testdata/cad"
LIMITS_FILE="$EVID/71-06-limits.txt"
KEEPLIST="$REPO_ROOT/scripts/cad-measure/vtk-keeplist.txt"
PY=(/opt/venv/bin/python -I -B)
LIB=/opt/cad-measure
SITE=/opt/venv/lib/python3.11/site-packages
LIMITS=(--network none --cpus 2 --memory 2g --memory-swap 2g --user nobody)
VIEWS=(iso front back left right top bottom)

usage() {
  sed -n '/^# Usage:/,/^# PIN, LP/p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
  exit 64
}

CMD=${1:-}
[ -n "$CMD" ] || usage
shift
OUT=""
while [ $# -gt 0 ]; do
  case "$1" in
    --out) OUT=${2:?}; shift 2 ;;
    -h|--help) usage ;;
    *) echo "error: unknown argument $1" >&2; usage ;;
  esac
done
case "$CMD" in
  libs) : "${OUT:=$EVID/71-07-libtrace.txt}" ;;
  prune-check) : "${OUT:=$EVID/71-07-prune.txt}" ;;
  licences) : "${OUT:=$EVID/71-07-licences.txt}" ;;
  self-test) ;;
  *) usage ;;
esac

corpus_inputs() {
  local corpus=$1 kind=$2 rel
  rel=$(cad_corpus_cad_files "$corpus") || return 1
  [ -n "$rel" ] || return 0
  printf '%s\n' "$rel" | CAD_INPUT_DIR="$corpus" CAD_INPUT_KIND="$kind" awk '
    { e = tolower($0); sub(/^.*\./, "", e)
      if (ENVIRON["CAD_INPUT_KIND"] == "mesh") keep = (e == "step" || e == "stp" || e == "iges" || e == "igs")
      else keep = (e == "stl")
      if (keep) print ENVIRON["CAD_INPUT_DIR"] "/" $0 }'
}

corpus_other_expected() {
  cad_corpus_cad_files "$1" |
    awk '{ e = tolower($0); sub(/^.*\./, "", e)
           if (e == "step" || e == "stp" || e == "iges" || e == "igs" || e == "stl") n++
           else if (e == "dxf") n += 2 }
         END { print n + 0 }'
}

prune_other_verdict() {
  local full=$1 pruned=$2 expected=$3 full_rc=$4 pruned_rc=$5
  awk -F, -v EXPECTED="$expected" -v FRC="$full_rc" -v PRC="$pruned_rc" '
    function kv(s, k,    n, i, parts) {
      n = split(s, parts, ",")
      for (i = 1; i <= n; i++) if (index(parts[i], k "=") == 1) return substr(parts[i], length(k) + 2)
      return ""
    }
    FNR == 1 { side = (FILENAME == ARGV[1]) ? "f" : "p" }
    /^ROW,/ {
      head = $0; sub(/,out=.*$/, "", head)
      out = $0; sub(/^.*,out=/, "", out)
      n = split(out, parts, ";"); o = ""
      for (i = 1; i <= n; i++) if (parts[i] !~ /^t_/) o = o parts[i] ";"
      key = kv(head, "sha") "|" kv(head, "op")
      val = "rc=" kv(head, "rc") "/pix=" kv(head, "pix") "/" o
      nm[key] = kv(head, "name")
      if (side == "f") { full[key] = val; nfull++; if (kv(head, "rc") == "0") nok++ } else { pr[key] = val; npr++ }
    }
    /^SKIP,/ && /reason=prestep-failed-/ {
      key = kv($0, "name") "|" kv($0, "op")
      if (side == "f") { sfull[key] = 1; nsf++ } else { spr[key] = 1; nsp++ }
    }
    END {
      why = ""
      if (FRC != 0 || PRC != 0) why = why "a measurement run failed (full_rc=" FRC ",pruned_rc=" PRC ");"
      if (nfull + 0 == 0 || npr + 0 == 0) why = why "a run wrote no ROW (full=" nfull + 0 ",pruned=" npr + 0 ");"
      if (nfull + nsf != EXPECTED) why = why "full run has " nfull + 0 " rows and " nsf + 0 " pre-step skips, expected " EXPECTED " pairs;"
      if (npr + nsp != EXPECTED) why = why "pruned run has " npr + 0 " rows and " nsp + 0 " pre-step skips, expected " EXPECTED " pairs;"
      for (k in sfull) if (!(k in spr)) { diff++; printf "PRUNE_OTHER_DIFF,name=%s,op=%s,full=prestep-skipped,pruned=%s\n", substr(k, 1, index(k, "|") - 1), substr(k, index(k, "|") + 1), ((k in pr) ? "row" : "missing") > "/dev/stderr" }
      for (k in spr) if (!(k in sfull)) { diff++; printf "PRUNE_OTHER_DIFF,name=%s,op=%s,full=%s,pruned=prestep-skipped\n", substr(k, 1, index(k, "|") - 1), substr(k, index(k, "|") + 1), ((k in full) ? "row" : "missing") > "/dev/stderr" }
      for (k in full) {
        rows++
        if ((k in pr) && pr[k] == full[k]) same++
        else { diff++; printf "PRUNE_OTHER_DIFF,name=%s,op=%s,full=%s,pruned=%s\n", nm[k], substr(k, index(k, "|") + 1), full[k], (k in pr ? pr[k] : "missing") > "/dev/stderr" }
      }
      for (k in pr) if (!(k in full)) { rows++; diff++ }
      if (rows + 0 == 0 || same + 0 == 0) why = why "no row was compared;"
      if (nok + 0 == 0) why = why "no full-image row succeeded, so the pruned image was compared against failures only;"
      if (diff + 0 != 0) why = why "rows differ;"
      if (why != "") {
        printf "PRUNE_OTHER,status=FAILED,rows=%d,same=%d,diff=%d,expected=%d,reason=%s\n", rows, same, diff, EXPECTED, why
        exit 1
      }
      printf "PRUNE_OTHER,rows=%d,same=%d,diff=%d,expected=%d,prestep_skipped=%d,runs_ok=1\n", rows, same, diff, EXPECTED, nsf
    }' "$full" "$pruned"
}

libs_expectation() {
  awk -F'\t' -v PIN="$1" -v HOT="$2" '
    BEGIN {
      split("import:1 mesh-step:4 mesh-iges:" 1 + HOT " stlstats:2", base, " ")
      split("render-view:7 render-transparent:1 render-512:1 render-2048:1 render-lp1:1 render-lpN:1 render-degenerate:3 render-exit2:1 render-exit3-unreachable:1 dxf:4 rsvg-png:4 rsvg-pdf:4", pinonly, " ")
    }
    {
      seen[$1 SUBSEP $2]++
      ocps[$1] = 1
      jp_expected = ($1 == "8.0.1.0.0") ? 1 : 0
      if ($7 != 1 || $8 != 1) printf "%s/%s(op did not run as expected: rc_ok=%s,ld_and_maps=%s);", $1, $2, $7, $8
      imports_ocp = ($2 == "import" || $2 ~ /^mesh-/)
      if (imports_ocp) {
        if ($6 == 0) printf "%s/%s(imports OCP but no wheel library was seen loading);", $1, $2
        else if ($3 != 1 || $4 != 1 || $5 != jp_expected)
          printf "%s/%s(fi=%s,raw=%s,jp=%s,want_jp=%d);", $1, $2, $3, $4, $5, jp_expected
      } else if ($6 > 0 || $3 != 0 || $4 != 0 || $5 != 0) {
        printf "%s/%s(ocp-free op loads ocp_libs=%s,fi=%s,raw=%s,jp=%s);", $1, $2, $6, $3, $4, $5
      }
    }
    END {
      if (length(ocps) == 0) printf "no op rows;"
      for (o in ocps) {
        for (i = 1; i in base; i++) { split(base[i], b, ":"); want[o SUBSEP b[1]] = b[2] }
        if (o == PIN) for (i = 1; i in pinonly; i++) { split(pinonly[i], b, ":"); want[o SUBSEP b[1]] = b[2] }
      }
      for (k in want) { split(k, kk, SUBSEP); if (seen[k] + 0 != want[k]) printf "%s/%s(%d rows, expected %d);", kk[1], kk[2], seen[k] + 0, want[k] }
      for (k in seen) if (!(k in want)) { split(k, kk, SUBSEP); printf "%s/%s(unexpected op);", kk[1], kk[2] }
    }' "$3"
}

keeplist_verdict() {
  local identical=$1 entries=$2 maps=$3 planned=$4 mapped=$5
  if [ "$identical" = 1 ] && [ "$entries" -gt 0 ] && [ "$planned" -gt 0 ] && [ "$maps" = "$planned" ] && [ "$mapped" = "$planned" ]; then
    echo 1
  else
    echo 0
  fi
}

stale_missing_module_detected() {
  local prune_rc=$1 render_rc=$2 sha=$3 ref=$4
  case "$prune_rc" in ''|*[!0-9]*) return 1 ;; esac
  case "$render_rc" in ''|*[!0-9]*) return 1 ;; esac
  [ -n "$ref" ] && [ "$prune_rc" = 0 ] && { [ "$render_rc" != 0 ] || [ "$sha" != "$ref" ]; }
}

self_test() {
  local d fails=0 n=0
  d=$(mktemp -d "${TMPDIR:-/tmp}/cad-trace-selftest.XXXXXX")
  expect() {
    local want=$1 label=$2 got=0
    shift 2
    "$@" >/dev/null 2>&1 || got=$?
    n=$((n + 1))
    if { [ "$want" = ok ] && [ "$got" = 0 ]; } || { [ "$want" = no ] && [ "$got" != 0 ]; }; then return 0; fi
    echo "SELF_TEST_FAIL,case=$label,want=$want,rc=$got" >&2
    fails=$((fails + 1))
  }
  expect_eq() {
    local want=$1 label=$2 got
    shift 2
    got=$("$@" 2>/dev/null)
    n=$((n + 1))
    [ "$got" = "$want" ] && return 0
    echo "SELF_TEST_FAIL,case=$label,want=$want,got=$got" >&2
    fails=$((fails + 1))
  }
  row() {
    printf 'ROW,src=public,name=%s,sha=%s,fmt=step,bytes=1,op=%s,ocp=7.9.3.1.1,variant=%s,mem=2g,cpus=2,confined=0,rep=1,rc=%s,pix=-,out=triangles:%s;t_wall:1\n' "$1" "$2" "$3" "$4" "${5:-0}" "${6:-10}"
  }
  pair() {
    local variant=$1 f=$2 rc=${3:-0}
    {
      row a.step s1 mesh "$variant" "$rc"
      row b.stl s2 stlstats "$variant"
      row c.dxf s3 dxf "$variant"
      row c.dxf s3 rsvg-png "$variant"
    } > "$f"
  }
  pair full "$d/full.txt"; pair pruned "$d/pruned.txt"
  expect ok prune-other-both-runs-complete prune_other_verdict "$d/full.txt" "$d/pruned.txt" 4 0 0
  : > "$d/empty.txt"
  expect no prune-other-nothing-expected-nothing-compared prune_other_verdict "$d/empty.txt" "$d/empty.txt" 0 0 0
  expect no prune-other-both-runs-wrote-nothing-rc-hidden prune_other_verdict "$d/empty.txt" "$d/empty.txt" 4 0 0
  expect no prune-other-both-runs-wrote-nothing-rc-failed prune_other_verdict "$d/empty.txt" "$d/empty.txt" 4 1 1
  expect no prune-other-pruned-run-wrote-nothing prune_other_verdict "$d/full.txt" "$d/empty.txt" 4 0 0
  expect no prune-other-full-run-failed prune_other_verdict "$d/full.txt" "$d/pruned.txt" 4 1 0
  expect no prune-other-pruned-run-failed prune_other_verdict "$d/full.txt" "$d/pruned.txt" 4 0 1
  expect no prune-other-fewer-rows-than-pairs prune_other_verdict "$d/full.txt" "$d/pruned.txt" 5 0 0
  head -n 3 "$d/pruned.txt" > "$d/pruned-short.txt"
  expect no prune-other-pruned-lost-a-row prune_other_verdict "$d/full.txt" "$d/pruned-short.txt" 4 0 0
  sed 's/triangles:10/triangles:11/' "$d/pruned.txt" > "$d/pruned-diff.txt"
  expect no prune-other-output-differs prune_other_verdict "$d/full.txt" "$d/pruned-diff.txt" 4 0 0
  { head -n 3 "$d/full.txt"; printf 'SKIP,src=public,name=c.dxf,fmt=dxf,op=rsvg-png,mem=2g,rep=1,reason=prestep-failed-3-attempts\n'; } > "$d/full-skip.txt"
  cp "$d/full-skip.txt" "$d/pruned-skip.txt"
  expect ok prune-other-same-prestep-skip-is-accounted prune_other_verdict "$d/full-skip.txt" "$d/pruned-skip.txt" 4 0 0
  expect no prune-other-skip-on-one-side-only prune_other_verdict "$d/full-skip.txt" "$d/pruned.txt" 4 0 0
  sed 's/,rc=0,/,rc=1,/' "$d/full.txt" > "$d/full-allfail.txt"; sed 's/,rc=0,/,rc=1,/' "$d/pruned.txt" > "$d/pruned-allfail.txt"
  expect no prune-other-every-row-fails-identically prune_other_verdict "$d/full-allfail.txt" "$d/pruned-allfail.txt" 4 0 0
  pair full "$d/full-rc.txt" 1; pair pruned "$d/pruned-rc.txt" 1
  expect ok prune-other-identical-failing-row-still-compares prune_other_verdict "$d/full-rc.txt" "$d/pruned-rc.txt" 4 0 0
  out=$(prune_other_verdict "$d/empty.txt" "$d/empty.txt" 4 0 0 2>/dev/null || true)
  case "$out" in PRUNE_OTHER,status=FAILED,*) n=$((n + 1)) ;; *) echo "SELF_TEST_FAIL,case=prune-other-failed-line-shape,got=$out" >&2; n=$((n + 1)); fails=$((fails + 1)) ;; esac
  out=$(prune_other_verdict "$d/full.txt" "$d/pruned.txt" 4 0 0 2>/dev/null)
  case "$out" in "PRUNE_OTHER,rows=4,same=4,diff=0,expected=4,prestep_skipped=0,runs_ok=1") n=$((n + 1)) ;; *) echo "SELF_TEST_FAIL,case=prune-other-ok-line-shape,got=$out" >&2; n=$((n + 1)); fails=$((fails + 1)) ;; esac

  mkdir -p "$d/corpus/a" "$d/corpus/b"
  : > "$d/corpus/a/x.step"; : > "$d/corpus/a/y.STP"; : > "$d/corpus/a/z.iges"; : > "$d/corpus/b/w.stl"; : > "$d/corpus/b/v.dxf"; : > "$d/corpus/b/README.md"
  expect_eq 6 corpus-other-expected-pairs corpus_other_expected "$d/corpus"
  expect no corpus-other-expected-missing-directory corpus_other_expected "$d/empty-dir-that-does-not-exist"

  local dp g r
  dp=$(cd "$d" && pwd -P)
  r="$dp/root"
  for g in public synthetic dxf dxf-out dxf-cr render-bg reject public/reject; do mkdir -p "$r/$g"; done
  : > "$r/public/a.step"; : > "$r/public/B.STL"; : > "$r/public/c.dxf"; : > "$r/public/README.md"
  : > "$r/public/reject/n.step"; : > "$r/synthetic/d.iges"; : > "$r/synthetic/e.igs"
  : > "$r/synthetic/f.stp"; : > "$r/synthetic/g.txt"; : > "$r/dxf/h.dxf"; : > "$r/dxf-out/i.svg"
  : > "$r/dxf-cr/k.dxf"; : > "$r/render-bg/l.png"; : > "$r/reject/m.step"
  mkdir -p "$dp/half/public"; : > "$dp/half/public/a.step"
  sel() {
    local root=$1 out rc=0
    shift
    out=$(CAD_CORPUS_ROOT=$root "$@" 2>/dev/null) || rc=$?
    printf '%s|rc=%s' "$out" "$rc" | tr '\n' ' '
  }
  expect ok corpus-other-expected-root-public-synthetic-only test "$(sel "$r" corpus_other_expected "$r")" = "7|rc=0"
  expect ok corpus-other-expected-other-tree-walked-as-before test "$(sel "$CAD_CORPUS_ROOT" corpus_other_expected "$r")" = "11|rc=0"
  expect ok corpus-other-expected-phase71-group-walked-as-before test "$(sel "$r" corpus_other_expected "$r/public")" = "4|rc=0"
  expect ok corpus-other-expected-later-group-walked-as-before test "$(sel "$r" corpus_other_expected "$r/dxf")" = "2|rc=0"
  expect ok corpus-other-expected-reject-dir-itself-yields-nothing test "$(sel "$r" corpus_other_expected "$r/reject")" = "0|rc=0"
  expect ok corpus-other-expected-root-without-synthetic-fails test "$(sel "$dp/half" corpus_other_expected "$dp/half")" = "0|rc=1"
  expect ok prune-mesh-inputs-root test "$(sel "$r" corpus_inputs "$r" mesh)" = "$r/public/a.step $r/synthetic/d.iges $r/synthetic/e.igs $r/synthetic/f.stp|rc=0"
  expect ok prune-stl-inputs-root test "$(sel "$r" corpus_inputs "$r" stl)" = "$r/public/B.STL|rc=0"
  expect ok prune-inputs-reject-dir-itself-yields-nothing test "$(sel "$r" corpus_inputs "$r/reject" mesh)" = "|rc=0"
  expect ok prune-inputs-root-without-synthetic-fails test "$(sel "$dp/half" corpus_inputs "$dp/half" mesh)" = "|rc=1"

  ops_fixture() {
    local f=$1 hot=${2:-1} ocp op c
    : > "$f"
    for ocp in 7.9.3.1.1 8.0.1.0.0; do
      jp=0; [ "$ocp" != 8.0.1.0.0 ] || jp=1
      printf '%s\timport\t1\t1\t%s\t60\t1\t1\n' "$ocp" "$jp" >> "$f"
      for _ in 1 2 3 4; do printf '%s\tmesh-step\t1\t1\t%s\t60\t1\t1\n' "$ocp" "$jp" >> "$f"; done
      for _ in $(seq 1 $((1 + hot))); do printf '%s\tmesh-iges\t1\t1\t%s\t60\t1\t1\n' "$ocp" "$jp" >> "$f"; done
      for _ in 1 2; do printf '%s\tstlstats\t0\t0\t0\t0\t1\t1\n' "$ocp" >> "$f"; done
    done
    for spec in render-view:7 render-transparent:1 render-512:1 render-2048:1 render-lp1:1 render-lpN:1 render-degenerate:3 render-exit2:1 render-exit3-unreachable:1 dxf:4 rsvg-png:4 rsvg-pdf:4; do
      op=${spec%%:*}; c=${spec#*:}
      for _ in $(seq 1 "$c"); do printf '7.9.3.1.1\t%s\t0\t0\t0\t0\t1\t1\n' "$op" >> "$f"; done
    done
  }
  ops_fixture "$d/ops-ok.tsv" 1
  expect_eq "" libs-expectation-matches-the-research libs_expectation 7.9.3.1.1 1 "$d/ops-ok.tsv"
  ops_fixture "$d/ops-nohot.tsv" 0
  expect_eq "" libs-expectation-without-the-fetched-iges libs_expectation 7.9.3.1.1 0 "$d/ops-nohot.tsv"
  expect_eq "" libs-expectation-no-rows-is-not-a-match true
  : > "$d/ops-none.tsv"
  got=$(libs_expectation 7.9.3.1.1 1 "$d/ops-none.tsv")
  n=$((n + 1)); [ -n "$got" ] || { echo "SELF_TEST_FAIL,case=libs-expectation-empty-ops-must-mismatch" >&2; fails=$((fails + 1)); }
  sed 's/^\(7.9.3.1.1\tmesh-step\t\)1\t1\t0\t60\t1\t1$/\10\t0\t0\t0\t1\t1/' "$d/ops-ok.tsv" > "$d/ops-silent.tsv"
  got=$(libs_expectation 7.9.3.1.1 1 "$d/ops-silent.tsv")
  n=$((n + 1)); [ -n "$got" ] || { echo "SELF_TEST_FAIL,case=libs-expectation-mesh-op-loading-nothing-must-mismatch" >&2; fails=$((fails + 1)); }
  awk -F'\t' 'BEGIN { OFS = "\t" } $1 == "7.9.3.1.1" && $2 == "dxf" && !d { $7 = 0; d = 1 } { print }' "$d/ops-ok.tsv" > "$d/ops-failed.tsv"
  got=$(libs_expectation 7.9.3.1.1 1 "$d/ops-failed.tsv")
  n=$((n + 1)); [ -n "$got" ] || { echo "SELF_TEST_FAIL,case=libs-expectation-failed-op-must-mismatch" >&2; fails=$((fails + 1)); }
  awk -F'\t' 'BEGIN { OFS = "\t" } $1 == "7.9.3.1.1" && $2 == "dxf" && !d { d = 1; next } { print }' "$d/ops-ok.tsv" > "$d/ops-missing.tsv"
  got=$(libs_expectation 7.9.3.1.1 1 "$d/ops-missing.tsv")
  n=$((n + 1)); [ -n "$got" ] || { echo "SELF_TEST_FAIL,case=libs-expectation-missing-op-row-must-mismatch" >&2; fails=$((fails + 1)); }
  { cat "$d/ops-ok.tsv"; printf '7.9.3.1.1\tbogus-op\t0\t0\t0\t0\t1\t1\n'; } > "$d/ops-extra.tsv"
  got=$(libs_expectation 7.9.3.1.1 1 "$d/ops-extra.tsv")
  n=$((n + 1)); [ -n "$got" ] || { echo "SELF_TEST_FAIL,case=libs-expectation-unexpected-op-must-mismatch" >&2; fails=$((fails + 1)); }
  awk -F'\t' 'BEGIN { OFS = "\t" } $1 == "7.9.3.1.1" && $2 == "dxf" && !d { $3 = 1; d = 1 } { print }' "$d/ops-ok.tsv" > "$d/ops-loads.tsv"
  got=$(libs_expectation 7.9.3.1.1 1 "$d/ops-loads.tsv")
  n=$((n + 1)); [ -n "$got" ] || { echo "SELF_TEST_FAIL,case=libs-expectation-ocp-free-op-loading-freeimage-must-mismatch" >&2; fails=$((fails + 1)); }

  expect_eq 1 keeplist-verdict-ok keeplist_verdict 1 77 17 17 17
  expect_eq 0 keeplist-verdict-no-entries keeplist_verdict 1 0 17 17 17
  expect_eq 0 keeplist-verdict-no-maps keeplist_verdict 1 77 0 0 0
  expect_eq 0 keeplist-verdict-short-maps keeplist_verdict 1 77 16 17 16
  expect_eq 0 keeplist-verdict-not-identical keeplist_verdict 0 77 17 17 17
  expect_eq 0 keeplist-verdict-collected-fewer-than-mapped keeplist_verdict 1 77 16 17 17
  expect_eq 0 keeplist-verdict-mapped-fewer-than-planned keeplist_verdict 1 77 17 17 16

  expect ok stale-missing-module-render-fails stale_missing_module_detected 0 1 none abc
  expect ok stale-missing-module-sha-differs stale_missing_module_detected 0 0 def abc
  expect no stale-missing-module-docker-gave-no-rc stale_missing_module_detected 0 "" "" abc
  expect no stale-missing-module-prune-gave-no-rc stale_missing_module_detected "" 1 none abc
  expect no stale-missing-module-prune-failed stale_missing_module_detected 1 1 none abc
  expect no stale-missing-module-no-reference stale_missing_module_detected 0 1 none ""
  expect no stale-missing-module-render-identical stale_missing_module_detected 0 0 abc abc

  rm -rf "$d"
  echo "SELF_TEST,cases=$n,failed=$fails"
  [ "$fails" = 0 ]
}

if [ "$CMD" = self-test ]; then
  self_test
  exit $?
fi

command -v docker >/dev/null 2>&1 || { echo "error: docker is required" >&2; exit 69; }
[ -s "$LIMITS_FILE" ] || { echo "error: $LIMITS_FILE is missing" >&2; exit 1; }

PIN=$(sed -n 's/^PIN_DECISION,choice=\([^,]*\),.*/\1/p' "$LIMITS_FILE")
LP=$(sed -n 's/^THREADS_DECISION,lp_num_threads=\([^,]*\),.*/\1/p' "$LIMITS_FILE")
PAR=$(sed -n 's/^THREADS_DECISION,lp_num_threads=[^,]*,occt_parallel=\([^,]*\),.*/\1/p' "$LIMITS_FILE")
[ -n "$PIN" ] && [ -n "$LP" ] && [ -n "$PAR" ] || { echo "error: PIN_DECISION or THREADS_DECISION missing in $LIMITS_FILE" >&2; exit 1; }
case "$PIN" in
  7.9.3.1.1) OTHER=8.0.1.0.0 ;;
  8.0.1.0.0) OTHER=7.9.3.1.1 ;;
  *) echo "error: unknown pin $PIN" >&2; exit 1 ;;
esac

mkdir -p "$(dirname "$OUT")"
: > "$OUT"
emit() { printf '%s\n' "$*" | tee -a "$OUT"; }

WORK=$(mktemp -d "${TMPDIR:-/tmp}/cad-trace.XXXXXX")
trap 'rm -rf "$WORK"' EXIT

newdir() {
  local d
  d=$(mktemp -d "$WORK/job.XXXXXX")
  chmod 0777 "$d"
  printf '%s\n' "$d"
}

field() {
  local h=",$1," v
  v=${h#*,$2=}
  [ "$v" != "$h" ] || { echo ""; return 0; }
  printf '%s\n' "${v%%,*}"
}

mib() { awk -v b="$1" 'BEGIN { printf "%.1f", b / 1048576 }'; }

mesh_plain() {
  local tag=$1 src=$2 dst=$3 mem=${4:-2g} attempt=1 job ext rc
  ext=$(printf '%s' "${src##*.}" | tr '[:upper:]' '[:lower:]')
  while [ "$attempt" -le 3 ]; do
    job=$(newdir)
    rc=0
    docker run --rm --network none --cpus 2 --memory "$mem" --memory-swap "$mem" --user nobody \
      -v "$job:/job" -v "$src:/job/in.$ext:ro" "$tag" \
      "${PY[@]}" "$LIB/cad_mesh.py" mesh "/job/in.$ext" /job --parallel "$PAR" >/dev/null 2>&1 </dev/null || rc=$?
    if [ -s "$job/part.stl" ]; then
      cp "$job/part.stl" "$dst"
      rm -rf "$job"
      return 0
    fi
    rm -rf "$job"
    attempt=$((attempt + 1))
  done
  return 1
}

if [ "$CMD" = libs ]; then
  TAG_PIN=$(cad_build_image "$PIN" full)
  TAG_OTHER=$(cad_build_image "$OTHER" full)
  cad_require_native "$TAG_PIN" || exit 1
  cad_require_native "$TAG_OTHER" || exit 1

  emit "# Plan 71-07 library trace: which wheel libraries load on which operation (D-18 measurement)"
  cad_evidence_header "$TAG_PIN" 2 2g | tee -a "$OUT"
  emit "HDR,pin=$PIN,other_pin=$OTHER,lp_threads=$LP,occt_parallel=$PAR,other_image=$TAG_OTHER"

  STLDIR="$WORK/stl"; SVGDIR="$WORK/svg"; MAPDIR="$WORK/rendermaps"
  mkdir -p "$STLDIR" "$SVGDIR" "$MAPDIR"
  chmod 0755 "$STLDIR" "$SVGDIR"

  IMPORT_PY='import OCP
libs = set()
with open("/proc/self/maps") as fh:
    for line in fh:
        p = line.split(None, 5)
        if len(p) == 6 and ".so" in p[5]:
            libs.add(p[5].strip())
with open("/job/maps.txt", "w") as fh:
    fh.write("\n".join(sorted(libs)) + "\n")'

  ELF_PY='import os, struct, sys
SITE = "/opt/venv/lib/python3.11/site-packages"
LIBS = SITE + "/cadquery_ocp_novtk.libs"
pin = os.environ["PIN"]
def needed(path):
    with open(path, "rb") as fh:
        d = fh.read()
    phoff = struct.unpack_from("<Q", d, 0x20)[0]
    phentsize, phnum = struct.unpack_from("<HH", d, 0x36)
    loads, dyn = [], None
    for i in range(phnum):
        p_type, _f, off, va, _pa, fsz, _msz, _al = struct.unpack_from("<IIQQQQQQ", d, phoff + i * phentsize)
        if p_type == 1:
            loads.append((va, off, fsz))
        elif p_type == 2:
            dyn = (off, fsz)
    def to_off(v):
        for va, off, fsz in loads:
            if va <= v < va + fsz:
                return v - va + off
    names, strtab = [], None
    for i in range(0, dyn[1], 16):
        tag, val = struct.unpack_from("<qQ", d, dyn[0] + i)
        if tag == 0:
            break
        if tag == 1:
            names.append(val)
        elif tag == 5:
            strtab = to_off(val)
    return [d[strtab + n:d.index(b"\0", strtab + n)].decode() for n in names]
targets = []
for name in sorted(os.listdir(SITE + "/OCP")):
    if name.startswith("OCP.cpython-311") and name.endswith(".so"):
        targets.append(("OCP/" + name, SITE + "/OCP/" + name))
for name in sorted(os.listdir(LIBS)):
    if name.startswith(("libTKService", "libfreeimage", "libraw", "libjasper")):
        targets.append((name, LIBS + "/" + name))
for label, path in targets:
    for n in needed(path):
        print("NEEDED,ocp=%s,from=%s,to=%s" % (pin, label, n))
for name in sorted(os.listdir(LIBS)):
    print("LIBFILE,ocp=%s,file=%s,bytes=%d" % (pin, name, os.path.getsize(LIBS + "/" + name)))'

  : > "$WORK/ops.tsv"
  KEEP_STL_AS=""
  MAPN=0
  RENDER_PLANNED=0
  HOT=0

  count_hits() {
    local pat=$1 file=$2
    if [ -s "$file" ]; then grep -cE "$pat" "$file" || true; else echo 0; fi
  }

  ldd_hits() {
    local n
    n=$(docker run --rm --network none --entrypoint sh "$1" -c \
      "l=\$(ldd $2) || exit 3; printf '%s\n' \"\$l\" | grep -cE 'libfreeimage|libraw|libjasper' || true")
    case "$n" in ''|*[!0-9]*) echo "error: ldd of $2 gave no count" >&2; exit 1 ;; esac
    printf '%s\n' "$n"
  }

  trace_op() {
    local op=$1 input=$2 ocp=$3 tag=$4 expect=$5 infile=$6 ext=$7 maps=$8
    shift 8
    local attempt=1 rc job ok libfile="$WORK/libfiles.$ocp"
    while [ "$attempt" -le 3 ]; do
      job=$(newdir)
      rc=0
      if [ -n "$infile" ]; then
        docker run --rm "${LIMITS[@]}" -e LD_DEBUG=files -e LD_DEBUG_OUTPUT=/job/ld \
          -v "$job:/job" -v "$infile:/job/in.$ext:ro" "$tag" "$@" >"$WORK/cell.log" 2>&1 || rc=$?
      else
        docker run --rm "${LIMITS[@]}" -e LD_DEBUG=files -e LD_DEBUG_OUTPUT=/job/ld \
          -v "$job:/job" "$tag" "$@" >"$WORK/cell.log" 2>&1 || rc=$?
      fi
      ok=1
      ls "$job"/ld.* >/dev/null 2>&1 || ok=0
      if [ "$maps" = 1 ] && [ ! -s "$job/maps.txt" ]; then ok=0; fi
      if [ "$ok" = 1 ] && [ "$rc" = "$expect" ]; then break; fi
      emit "RETRY,op=$op,input=$input,ocp=$ocp,attempt=$attempt,rc=$rc,expected_rc=$expect,ld_file=$ok"
      if [ "$attempt" -lt 3 ]; then rm -rf "$job"; fi
      attempt=$((attempt + 1))
    done
    [ "$attempt" -le 3 ] || attempt=3
    local rc_ok=0
    [ "$rc" = "$expect" ] && rc_ok=1

    cat "$job"/ld.* 2>/dev/null | grep -a 'file=' | sed -n 's/.*file=\([^ ]*\) \[.*/\1/p' | sed 's|.*/||' | sort -u > "$WORK/names.ld"
    if [ "$maps" = 1 ] && [ -s "$job/maps.txt" ]; then
      sed 's|.*/||' "$job/maps.txt" | sort -u > "$WORK/names.maps"
    else
      : > "$WORK/names.maps"
    fi
    local fi_ld fi_mp raw_ld raw_mp jp_ld jp_mp
    fi_ld=$(count_hits 'libfreeimage' "$WORK/names.ld"); fi_mp=$(count_hits 'libfreeimage' "$WORK/names.maps")
    raw_ld=$(count_hits '^libraw[-.]' "$WORK/names.ld"); raw_mp=$(count_hits '^libraw[-.]' "$WORK/names.maps")
    jp_ld=$(count_hits 'libjasper' "$WORK/names.ld"); jp_mp=$(count_hits 'libjasper' "$WORK/names.maps")
    local fi=0 raw=0 jp=0
    [ $((fi_ld + fi_mp)) -gt 0 ] && fi=1
    [ $((raw_ld + raw_mp)) -gt 0 ] && raw=1
    [ $((jp_ld + jp_mp)) -gt 0 ] && jp=1
    sort -u "$WORK/names.ld" "$WORK/names.maps" > "$WORK/names.all"
    comm -12 "$WORK/names.all" "$libfile" > "$WORK/names.ocp"
    cat "$WORK/names.ocp" >> "$WORK/loaded.$ocp"
    local ocpn vtkn=0 src=ld_debug
    ocpn=$(wc -l < "$WORK/names.ocp" | tr -d ' ')
    if [ "$maps" = 1 ]; then
      src=ld_debug+maps
      if [ -s "$job/maps.txt" ]; then vtkn=$(grep -c '/vtkmodules/' "$job/maps.txt" || true); fi
    fi
    emit "LIBTRACE,op=$op,input=$input,ocp=$ocp,rc=$rc,freeimage=$fi,libraw=$raw,libjasper=$jp,ocp_libs_loaded=$ocpn,vtk_so_mapped=$vtkn,source=$src,fi_ld=$fi_ld,fi_maps=$fi_mp,raw_ld=$raw_ld,raw_maps=$raw_mp,jp_ld=$jp_ld,jp_maps=$jp_mp,rc_ok=$rc_ok,attempts=$attempt"
    printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$ocp" "$op" "$fi" "$raw" "$jp" "$ocpn" "$rc_ok" "$ok" >> "$WORK/ops.tsv"
    if [ "$ocp" = "$PIN" ] && [ "$maps" = 1 ] && [ -s "$job/maps.txt" ] && [ "${op#render}" != "$op" ]; then
      MAPN=$((MAPN + 1))
      cp "$job/maps.txt" "$MAPDIR/$(printf '%03d-%s' "$MAPN" "$op-$input" | tr -c 'A-Za-z0-9._-' '_').txt"
    fi
    if [ -n "$KEEP_STL_AS" ] && [ -s "$job/part.stl" ]; then cp "$job/part.stl" "$STLDIR/$KEEP_STL_AS"; fi
    if [ -n "${KEEP_SVG_AS:-}" ] && [ -s "$job/out.svg" ]; then cp "$job/out.svg" "$SVGDIR/$KEEP_SVG_AS"; fi
    rm -rf "$job"
  }

  for ocp_tag in "$PIN|$TAG_PIN" "$OTHER|$TAG_OTHER"; do
    ocp=${ocp_tag%%|*}
    tag=${ocp_tag#*|}
    docker run --rm --network none --entrypoint "${PY[0]}" -e "PIN=$ocp" "$tag" "${PY[@]:1}" -c "$ELF_PY" > "$WORK/elf.$ocp"
    grep '^NEEDED,' "$WORK/elf.$ocp" | while IFS= read -r l; do emit "$l"; done
    grep '^LIBFILE,' "$WORK/elf.$ocp" | sed 's/^.*,file=\([^,]*\),.*/\1/' | sort -u > "$WORK/libfiles.$ocp"
    : > "$WORK/loaded.$ocp"
    emit "LIBFILES,ocp=$ocp,count=$(wc -l < "$WORK/libfiles.$ocp" | tr -d ' '),bytes=$(grep '^LIBFILE,' "$WORK/elf.$ocp" | sed 's/.*,bytes=//' | awk '{s += $1} END {print s}')"
    LDD_N=$(ldd_hits "$tag" "$SITE/OCP/OCP.cpython-311-*.so")
    emit "LDD,ocp=$ocp,freeimage_in_ocp_ldd=$LDD_N"
  done

  run_ocp_ops() {
    local ocp=$1 tag=$2 f name ext
    trace_op import "import-OCP" "$ocp" "$tag" 0 "" x 1 "${PY[@]}" -c "$IMPORT_PY"
    for spec in "mesh-step|slide|public/LinearSlide-MGN15x-Block.step" \
                "mesh-step|assembly|public/SC8UU.step" \
                "mesh-step|units_inch|synthetic/units_inch.step" \
                "mesh-step|legacy_cp1251|synthetic/legacy_cp1251.step" \
                "mesh-iges|surfaces_only|synthetic/surfaces_only.iges"; do
      IFS='|' read -r op name rel <<< "$spec"
      f="$CORPUS/$rel"
      ext=$(printf '%s' "${f##*.}" | tr '[:upper:]' '[:lower:]')
      KEEP_STL_AS=""
      [ "$ocp" != "$PIN" ] || [ "$name" != slide ] || KEEP_STL_AS=slide.stl
      trace_op "$op" "$name" "$ocp" "$tag" 0 "$f" "$ext" 1 \
        "${PY[@]}" "$LIB/cad_mesh.py" mesh "/job/in.$ext" /job --parallel "$PAR" --dump-maps /job/maps.txt
      KEEP_STL_AS=""
    done
    local iges="$CAD_CACHE_DIR/fetched/freecad-mk8-hotend.iges"
    HOT=0
    if [ -f "$iges" ]; then
      HOT=1
      trace_op mesh-iges freecad-mk8-hotend "$ocp" "$tag" 0 "$iges" iges 1 \
        "${PY[@]}" "$LIB/cad_mesh.py" mesh /job/in.iges /job --parallel "$PAR" --dump-maps /job/maps.txt
    else
      emit "SKIP,op=mesh-iges,input=freecad-mk8-hotend,reason=not-in-cache"
    fi
    for spec in "nasa-hubble|public/nasa-hubble-all-pieces.stl" "stl_ascii_box|synthetic/stl_ascii_box.stl"; do
      IFS='|' read -r name rel <<< "$spec"
      trace_op stlstats "$name" "$ocp" "$tag" 0 "$CORPUS/$rel" stl 1 \
        "${PY[@]}" "$LIB/cad_mesh.py" stlstats /job/in.stl /job --dump-maps /job/maps.txt
    done
  }

  run_ocp_ops "$PIN" "$TAG_PIN"
  run_ocp_ops "$OTHER" "$TAG_OTHER"

  for spec in "degenerate_plate|synthetic/degenerate_plate.step" "degenerate_rod|synthetic/degenerate_rod.step" \
              "single_face|synthetic/single_face.step"; do
    IFS='|' read -r name rel <<< "$spec"
    mesh_plain "$TAG_PIN" "$CORPUS/$rel" "$STLDIR/$name.stl" || { emit "FAIL,reason=no-stl-for-$name"; exit 1; }
  done
  python3 - "$STLDIR" <<'PYEOF'
import struct, sys
d = sys.argv[1]
with open(d + "/empty.stl", "wb") as fh:
    fh.write(b"\0" * 80 + struct.pack("<I", 0))
with open(d + "/zero_extent.stl", "wb") as fh:
    fh.write(b"\0" * 80 + struct.pack("<I", 1) + struct.pack("<12fH", 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0))
PYEOF
  [ -s "$STLDIR/slide.stl" ] || { emit "FAIL,reason=no-stl-for-slide"; exit 1; }

  render_op() {
    local op=$1 name=$2 expect=$3
    shift 3
    RENDER_PLANNED=$((RENDER_PLANNED + 1))
    trace_op "$op" "$name" "$PIN" "$TAG_PIN" "$expect" "$STLDIR/$name.stl" stl 1 \
      "${PY[@]}" "$LIB/cad_render.py" /job/in.stl /job/out.png "$@" --dump-maps /job/maps.txt
  }
  for v in "${VIEWS[@]}"; do render_op render-view slide 0 --view "$v" --size 1280 --background white; done
  render_op render-transparent slide 0 --view iso --background transparent
  render_op render-512 slide 0 --view iso --size 512
  render_op render-2048 slide 0 --view iso --size 2048
  render_op render-lp1 slide 0 --view iso --lp-threads 1
  render_op render-lpN slide 0 --view iso --lp-threads "$LP"
  for name in degenerate_plate degenerate_rod single_face; do render_op render-degenerate "$name" 0 --view iso; done
  render_op render-exit2 empty 2 --view iso
  emit "NOTE,op=render-exit3,reason=vtkSTLReader drops every degenerate triangle (nan, inf, three equal points): such input reaches exit 2, and exit 3 needs a cell with an empty bounding box, so the branch is unreachable from an STL; the imports at the top of render() are the same on both exits"
  render_op render-exit3-unreachable zero_extent 2 --view iso

  for spec in "hatches_2|public/hatches_2.dxf" "blocks_minsert|synthetic/blocks_minsert.dxf" \
              "legacy_cp1251|synthetic/legacy_cp1251.dxf" "columns_R2007|public/columns_R2007.dxf"; do
    IFS='|' read -r name rel <<< "$spec"
    KEEP_SVG_AS="$name.svg"
    trace_op dxf "$name" "$PIN" "$TAG_PIN" 0 "$CORPUS/$rel" dxf 1 \
      "${PY[@]}" "$LIB/cad_dxf.py" /job/in.dxf /job --dump-maps /job/maps.txt
    KEEP_SVG_AS=""
    [ -s "$SVGDIR/$name.svg" ] || { emit "FAIL,reason=no-svg-for-$name"; exit 1; }
    trace_op rsvg-png "$name" "$PIN" "$TAG_PIN" 0 "$SVGDIR/$name.svg" svg 0 \
      rsvg-convert -f png -w 1280 -h 1280 --keep-aspect-ratio -b '#FFFFFF' /job/in.svg -o /job/out.png
    trace_op rsvg-pdf "$name" "$PIN" "$TAG_PIN" 0 "$SVGDIR/$name.svg" svg 0 \
      rsvg-convert -f pdf /job/in.svg -o /job/out.pdf
  done
  LDD_N=$(ldd_hits "$TAG_PIN" /usr/bin/rsvg-convert)
  emit "LDD_RSVG,freeimage_libraw_jasper_in_ldd=$LDD_N"

  for ocp in "$PIN" "$OTHER"; do
    sort -u "$WORK/loaded.$ocp" > "$WORK/loaded.$ocp.u"
    grep '^LIBFILE,' "$WORK/elf.$ocp" | while IFS= read -r l; do
      file=$(field "$l" file); bytes=$(field "$l" bytes)
      if ! grep -Fxq "$file" "$WORK/loaded.$ocp.u"; then emit "UNLOADED,ocp=$ocp,file=$file,bytes=$bytes"; fi
    done
    awk -F'\t' -v P="$ocp" '
      $1 == P {
        ops++; if ($3 == 1) fi++; if ($4 == 1) rw++; if ($5 == 1) jp++
        if ($6 == 0 && !($2 in seen)) { seen[$2] = 1; free = free (free == "" ? "" : "+") $2 }
      }
      END { printf "D18_TRACE,ocp=%s,ops=%d,freeimage_ops=%d,libraw_ops=%d,libjasper_ops=%d,ocp_free_ops=%s\n", P, ops, fi, rw, jp, (free == "" ? "none" : free) }' \
      "$WORK/ops.tsv" | while IFS= read -r l; do emit "$l"; done
  done

  MISMATCH=$(libs_expectation "$PIN" "$HOT" "$WORK/ops.tsv")
  if [ -z "$MISMATCH" ]; then
    emit "EXPECTATION,research=V3-V7,match=1,detail=every op that imports OCP loads libfreeimage and libraw on both pins; libjasper only on 8.0.1.0.0; render dxf rsvg and stlstats load none"
  else
    emit "EXPECTATION,research=V3-V7,match=0,detail=$MISMATCH"
  fi

  VTK_VERSION=$(docker run --rm --network none --entrypoint "${PY[0]}" "$TAG_PIN" "${PY[@]:1}" \
    -c 'import importlib.metadata as m; print(m.version("vtk"))')
  MAPS=("$MAPDIR"/*.txt)
  python3 scripts/cad-measure/vtk_keeplist.py collect --vtk-version "$VTK_VERSION" --out "$KEEPLIST" "${MAPS[@]}" | while IFS= read -r l; do emit "$l"; done
  python3 scripts/cad-measure/vtk_keeplist.py collect --vtk-version "$VTK_VERSION" --out "$WORK/keeplist.again" "${MAPS[@]}" >/dev/null
  IDENTICAL=0
  if cmp -s "$KEEPLIST" "$WORK/keeplist.again"; then IDENTICAL=1; fi
  ENTRIES=$(($(wc -l < "$KEEPLIST") - 2))
  SAME=$(keeplist_verdict "$IDENTICAL" "$ENTRIES" "${#MAPS[@]}" "$RENDER_PLANNED" "$MAPN")
  emit "KEEPLIST_REGEN,same=$SAME,entries=$ENTRIES,maps_files=${#MAPS[@]},maps_expected=$RENDER_PLANNED,vtk_version=$VTK_VERSION"

  [ -z "$MISMATCH" ] || { echo "expectation mismatch: a human decides before the pruning step" >&2; exit 3; }
  [ "$SAME" = 1 ] || exit 1
  exit 0
fi

if [ "$CMD" = prune-check ]; then
  [ -s "$KEEPLIST" ] || { echo "error: $KEEPLIST is missing; run libs first" >&2; exit 1; }
  TAG_FULL=$(cad_build_image "$PIN" full)
  TAG_PRUNED=$(cad_build_image "$PIN" pruned)
  cad_require_native "$TAG_FULL" || exit 1
  cad_require_native "$TAG_PRUNED" || exit 1
  FAILED=0

  emit "# Plan 71-07 prune check: the pruned image of one build against the full image (D-17)"
  cad_evidence_header "$TAG_PRUNED" 2 2g | tee -a "$OUT"
  emit "HDR,pin=$PIN,lp_threads=$LP,occt_parallel=$PAR,keeplist_sha256=$(shasum -a 256 "$KEEPLIST" | awk '{print $1}')"
  emit "PRUNE_BUILD,pin=$PIN,full=$TAG_FULL,pruned=$TAG_PRUNED,full_id=$(docker image inspect "$TAG_FULL" --format '{{.Id}}'),pruned_id=$(docker image inspect "$TAG_PRUNED" --format '{{.Id}}')"

  vtk_kb() {
    docker run --rm --network none --entrypoint sh "$1" -c \
      "du -sk --apparent-size $SITE/vtkmodules $SITE/vtk.libs 2>/dev/null | awk '{s += \$1} END {print s}'"
  }
  KB_FULL=$(vtk_kb "$TAG_FULL")
  KB_PRUNED=$(vtk_kb "$TAG_PRUNED")
  IMG_FULL=$(docker image inspect "$TAG_FULL" --format '{{.Size}}')
  IMG_PRUNED=$(docker image inspect "$TAG_PRUNED" --format '{{.Size}}')
  SIZE_LINE=$(awk -v kf="$KB_FULL" -v kp="$KB_PRUNED" -v bf="$IMG_FULL" -v bp="$IMG_PRUNED" 'BEGIN {
      dv = (kf - kp) / 1024; di = (bf - bp) / 1048576
      gap = di - dv; if (gap < 0) gap = -gap
      ok = (bp < bf && dv > 0 && gap <= 0.05 * dv) ? 1 : 0
      printf "PRUNE_SIZE,vtk_full_mib=%.1f,vtk_pruned_mib=%.1f,image_full_mib=%.1f,image_pruned_mib=%.1f,delta_vtk_mib=%.1f,delta_image_mib=%.1f,gap_mib=%.1f,gap_pct_of_delta_vtk=%.2f,whiteout_free=%d\n",
        kf / 1024, kp / 1024, bf / 1048576, bp / 1048576, dv, di, gap, (dv > 0 ? 100 * gap / dv : 0), ok }')
  emit "$SIZE_LINE"
  case "$SIZE_LINE" in *whiteout_free=1) ;; *) FAILED=1; emit "FAIL,reason=image size difference does not match the VTK size difference (whiteout or no reduction)" ;; esac

  STLDIR="$WORK/stl"; mkdir -p "$STLDIR"; chmod 0755 "$STLDIR"
  INPUTS="$WORK/inputs.tsv"; : > "$INPUTS"
  add_input() { printf '%s\t%s\t%s\t%s\n' "$1" "$2" "$3" "$4" >> "$INPUTS"; }

  MESH_INPUTS=$(corpus_inputs "$CORPUS" mesh)
  STL_INPUTS=$(corpus_inputs "$CORPUS" stl)
  [ -z "$MESH_INPUTS" ] || while IFS= read -r f; do
    name=$(basename "$f")
    if mesh_plain "$TAG_FULL" "$f" "$STLDIR/mesh-$name.stl" 4g; then
      add_input "mesh-$name" "$STLDIR/mesh-$name.stl" 2g corpus
    else
      emit "PRUNE_SKIP,input=$name,reason=mesh-gave-no-stl-after-3-attempts"
      FAILED=1
    fi
  done <<< "$MESH_INPUTS"
  [ -z "$STL_INPUTS" ] || while IFS= read -r f; do add_input "$(basename "$f")" "$f" 2g corpus; done <<< "$STL_INPUTS"
  FETCHED_EXPECTED=$(awk -F'\t' 'NR > 1 && NF > 0' "$REPO_ROOT/scripts/cad-corpus-manifest.tsv" | wc -l | tr -d ' ')
  FETCHED_FOUND=0
  if [ -d "$CAD_CACHE_DIR/fetched" ]; then
    FETCHED_FOUND=$(find "$CAD_CACHE_DIR/fetched" -type f | wc -l | tr -d ' ')
  fi
  if [ "$FETCHED_EXPECTED" -lt 1 ] || [ "$FETCHED_FOUND" != "$FETCHED_EXPECTED" ]; then
    emit "FAIL,reason=the fetched corpus has $FETCHED_FOUND files, the manifest lists $FETCHED_EXPECTED; run scripts/cad-corpus-fetch.sh"
    FAILED=1
  fi
  if [ -d "$CAD_CACHE_DIR/fetched" ]; then
    while IFS= read -r f; do
      name=$(basename "$f")
      case "$name" in
        *.stl|*.STL) add_input "fetched-$name" "$f" 4g fetched ;;
        *)
          if mesh_plain "$TAG_FULL" "$f" "$STLDIR/fetched-$name.stl" 4g; then
            add_input "fetched-$name" "$STLDIR/fetched-$name.stl" 4g fetched
          else
            emit "PRUNE_SKIP,input=$name,reason=mesh-gave-no-stl-after-3-attempts"
            FAILED=1
          fi ;;
      esac
    done < <(find "$CAD_CACHE_DIR/fetched" -type f | LC_ALL=C sort)
  fi

  RENDER_SH='in=$1; shift
for spec in "$@"; do
  view=${spec%%:*}; rest=${spec#*:}
  size=${rest%%:*}; rest=${rest#*:}
  bg=${rest%%:*}; lp=${rest#*:}
  rm -f /tmp/o.png /tmp/o.png.json
  /opt/venv/bin/python -I -B /opt/cad-measure/cad_render.py "$in" /tmp/o.png --view "$view" --size "$size" --background "$bg" --lp-threads "$lp" >/dev/null 2>&1
  rc=$?
  if [ -s /tmp/o.png ]; then sha=$(sha256sum /tmp/o.png | cut -d" " -f1); else sha=none; fi
  echo "PNG,rc=$rc,spec=$spec,sha=$sha"
done'

  render_batch() {
    local tag=$1 stl=$2 mem=$3 outf=$4 want attempt=1 got
    shift 4
    want=$#
    while [ "$attempt" -le 3 ]; do
      docker run --rm --network none --cpus 2 --memory "$mem" --memory-swap "$mem" --user nobody \
        -v "$stl:/in/in.stl:ro" --entrypoint sh "$tag" -c "$RENDER_SH" sh /in/in.stl "$@" > "$outf" 2>/dev/null </dev/null || true
      got=$(grep -c '^PNG,' "$outf" || true)
      [ "$got" = "$want" ] && return 0
      emit "RETRY,tag=$tag,input=$(basename "$stl"),attempt=$attempt,lines=$got,expected=$want"
      attempt=$((attempt + 1))
    done
    return 1
  }

  VIEW_SPECS=(); for v in "${VIEWS[@]}"; do VIEW_SPECS+=("$v:1280:white:$LP"); done
  BRANCH_SPECS=("iso:1280:transparent:$LP" "iso:512:white:$LP" "iso:2048:white:$LP" "iso:1280:white:1")
  ISO_SPEC="iso:1280:white:$LP"
  REAL_PART="mesh-LinearSlide-MGN15x-Block.step"

  EXPECT_PNGS=$(awk -F'\t' -v real="$REAL_PART" -v nv="${#VIEWS[@]}" -v nb="${#BRANCH_SPECS[@]}" '
    { if ($4 == "fetched") n += 1; else { n += nv; if ($1 == real) n += nb } }
    END { print n + 0 }' "$INPUTS")
  PNGS=0; SAME=0; DIFFERENT=0; BROKEN=0
  : > "$WORK/ref.sha"
  while IFS="$(printf '\t')" read -r name stl mem kind; do
    case "$kind" in
      fetched) specs=("$ISO_SPEC") ;;
      *) specs=("${VIEW_SPECS[@]}"); [ "$name" != "$REAL_PART" ] || specs+=("${BRANCH_SPECS[@]}") ;;
    esac
    if ! render_batch "$TAG_FULL" "$stl" "$mem" "$WORK/png.full" "${specs[@]}" ||
       ! render_batch "$TAG_PRUNED" "$stl" "$mem" "$WORK/png.pruned" "${specs[@]}"; then
      emit "PRUNE_PNG,input=$name,view=all,size=-,bg=-,same=0,detail=container-gave-no-result-after-3-attempts"
      PNGS=$((PNGS + ${#specs[@]})); BROKEN=$((BROKEN + ${#specs[@]}))
      continue
    fi
    paste -d'#' "$WORK/png.full" "$WORK/png.pruned" > "$WORK/png.pair"
    while IFS='#' read -r lf lp; do
      spec=$(field "$lf" spec); rc_f=$(field "$lf" rc); sha_f=$(field "$lf" sha)
      rc_p=$(field "$lp" rc); sha_p=$(field "$lp" sha)
      IFS=: read -r view size bg lpt <<< "$spec"
      PNGS=$((PNGS + 1))
      if [ "$rc_f" = 0 ] && [ "$rc_p" = 0 ] && [ "$sha_f" != none ] && [ "$sha_p" != none ]; then
        if [ "$sha_f" = "$sha_p" ]; then
          same=1; SAME=$((SAME + 1))
        else
          same=0; DIFFERENT=$((DIFFERENT + 1))
        fi
        detail=""
      else
        same=0; BROKEN=$((BROKEN + 1)); detail=",detail=rc_full=$rc_f/rc_pruned=$rc_p"
      fi
      emit "PRUNE_PNG,input=$name,view=$view,size=$size,bg=$bg,same=$same,lp=$lpt,sha=${sha_f:0:16}$detail"
      printf '%s|%s\t%s\n' "$name" "$spec" "$sha_f" >> "$WORK/ref.sha"
    done < "$WORK/png.pair"
  done < "$INPUTS"
  emit "PRUNE_IDENTITY,pngs=$PNGS,identical=$SAME,different=$DIFFERENT,failed=$BROKEN,expected=$EXPECT_PNGS"
  if [ "$DIFFERENT" = 0 ] && [ "$BROKEN" = 0 ] && [ "$SAME" -gt 0 ] && [ "$SAME" = "$PNGS" ] && [ "$PNGS" = "$EXPECT_PNGS" ]; then
    :
  else
    FAILED=1
    emit "FAIL,reason=pruned renders differ from or fail against the full image, or fewer renders were compared than the inputs call for"
  fi

  run_other() {
    local variant=$1 file=$2 rc=0
    scripts/cad-rss-time-measure.sh --ocp "$PIN" --variant "$variant" --memory 2g --repeats 1 \
      --ops mesh,stlstats,dxf,rsvg-png --parallel "$PAR" --lp-threads "$LP" --out "$file" "$CORPUS" >/dev/null || rc=$?
    return "$rc"
  }
  RC_FULL=0; RC_PRUNED=0
  run_other full "$WORK/other.full" || RC_FULL=$?
  run_other pruned "$WORK/other.pruned" || RC_PRUNED=$?
  EXPECT_OTHER=$(corpus_other_expected "$CORPUS")
  OTHER_RC=0
  OTHER_LINE=$(prune_other_verdict "$WORK/other.full" "$WORK/other.pruned" "$EXPECT_OTHER" "$RC_FULL" "$RC_PRUNED" 2> "$WORK/other.diff") || OTHER_RC=$?
  cat "$WORK/other.diff" | while IFS= read -r l; do emit "$l"; done
  emit "$OTHER_LINE"
  [ "$OTHER_RC" = 0 ] || FAILED=1

  K=$(mktemp -d "$WORK/stale.XXXXXX")
  chmod 0755 "$K"
  sed '1s/.*/vtk_version=9.7.0/' "$KEEPLIST" > "$K/stale.txt"
  chmod 0644 "$K/stale.txt"
  RC=0
  docker run --rm --network none --user root -v "$K:/k:ro" "$TAG_FULL" \
    "${PY[@]}" "$LIB/vtk_keeplist.py" prune --keeplist /k/stale.txt --site "$SITE" > "$K/out.version" 2>&1 || RC=$?
  if grep -q 'keep-list stale' "$K/out.version" && [ "$RC" -ne 0 ]; then
    emit "STALE_TEST,case=version,result=detected,rc=$RC,message=$(head -n 1 "$K/out.version")"
  else
    emit "STALE_TEST,case=version,result=MISSED,rc=$RC,message=$(head -n 1 "$K/out.version")"; FAILED=1
  fi

  grep -v 'vtkFiltersCore' "$KEEPLIST" > "$K/nomod.txt"
  chmod 0644 "$K/nomod.txt"
  REF_SHA=$(awk -F'\t' -v key="$REAL_PART|$ISO_SPEC" '$1 == key {print $2; exit}' "$WORK/ref.sha")
  RC=0
  docker run --rm --network none --user root -v "$K:/k:ro" -v "$STLDIR/$REAL_PART.stl:/in/in.stl:ro" --entrypoint sh "$TAG_FULL" -c '
    '"${PY[*]}"' '"$LIB"'/vtk_keeplist.py prune --keeplist /k/nomod.txt --site '"$SITE"'; echo "PRUNE_RC=$?"
    rm -f /tmp/o.png
    setpriv --reuid nobody --regid nogroup --clear-groups '"${PY[*]}"' '"$LIB"'/cad_render.py /in/in.stl /tmp/o.png --view iso --lp-threads '"$LP"' >/tmp/render.log 2>&1
    echo "RENDER_RC=$?"
    tail -n 1 /tmp/render.log
    if [ -s /tmp/o.png ]; then echo "SHA=$(sha256sum /tmp/o.png | cut -d" " -f1)"; else echo "SHA=none"; fi' > "$K/out.missing" 2>&1 || RC=$?
  P_RC=$(sed -n 's/^PRUNE_RC=//p' "$K/out.missing"); R_RC=$(sed -n 's/^RENDER_RC=//p' "$K/out.missing")
  R_SHA=$(sed -n 's/^SHA=//p' "$K/out.missing"); R_MSG=$(grep -a -v '^\(PRUNE_RC\|RENDER_RC\|SHA\|PRUNED\)' "$K/out.missing" | tail -n 1 | tr ',;' '__')
  if stale_missing_module_detected "$P_RC" "$R_RC" "$R_SHA" "$REF_SHA"; then
    emit "STALE_TEST,case=missing_module,result=detected,module=vtkFiltersCore,removed_entries=$(($(wc -l < "$KEEPLIST") - $(wc -l < "$K/nomod.txt"))),prune_rc=$P_RC,render_rc=$R_RC,sha=${R_SHA:0:16},reference_sha=${REF_SHA:0:16},message=$R_MSG"
  else
    emit "STALE_TEST,case=missing_module,result=MISSED,module=vtkFiltersCore,prune_rc=$P_RC,render_rc=$R_RC,sha=${R_SHA:-none},reference_sha=${REF_SHA:-none}"; FAILED=1
  fi

  [ "$FAILED" = 0 ] || exit 1
  exit 0
fi

if [ "$CMD" = licences ]; then
  D18=$(sed -n 's/^D-18 outcome: //p' "$REPO_ROOT/.planning/phases/71-evaluation-gate/71-01-SUMMARY.md")
  case "$D18" in
    record) ;;
    patchelf) echo "error: the patchelf outcome is not implemented (no image variant removes libfreeimage); 71-01-SUMMARY.md records 'record'" >&2; exit 64 ;;
    *) echo "error: no D-18 outcome in 71-01-SUMMARY.md (found '$D18')" >&2; exit 1 ;;
  esac
  TRACE_EVID="$EVID/71-07-libtrace.txt"
  [ -s "$TRACE_EVID" ] || { echo "error: $TRACE_EVID is missing; run libs first" >&2; exit 1; }
  grep -q "^HDR,pin=$PIN,other_pin=$OTHER," "$TRACE_EVID" ||
    { echo "error: $TRACE_EVID was traced for another pin pair than $PIN and $OTHER; run libs first" >&2; exit 1; }
  TAG_PIN=$(cad_build_image "$PIN" full)
  TAG_OTHER=$(cad_build_image "$OTHER" full)
  cad_require_native "$TAG_PIN" || exit 1
  cad_require_native "$TAG_OTHER" || exit 1

  emit "# Plan 71-07 licence record of the OCP wheel libraries (EVAL-04, D-18 outcome: $D18)"
  emit "# Engineering record only. The licences are NOT legally cleared; the lawyer flag below stays open and does not block the phase."
  cad_evidence_header "$TAG_PIN" 2 2g | tee -a "$OUT"
  emit "HDR,pin=$PIN,other_pin=$OTHER,d18_outcome=$D18"

  INV_PY='import json, os, sys
SITE = "/opt/venv/lib/python3.11/site-packages"
pin = os.environ["PIN"]
libs = SITE + "/cadquery_ocp_novtk.libs"
dist = SITE + "/cadquery_ocp_novtk-%s.dist-info" % pin
with open(dist + "/METADATA") as fh:
    for line in fh:
        if line.startswith("License:"):
            print("META|License|" + line.split(":", 1)[1].strip())
for name in sorted(os.listdir(libs)):
    print("LIBFILE|%s|%d" % (name, os.path.getsize(libs + "/" + name)))
sbom = json.load(open(dist + "/sboms/auditwheel.cdx.json"))
licensed = 0
seen = set()
for c in sbom["components"]:
    if c.get("licenses"):
        licensed += 1
    key = (c["name"], c.get("version", ""))
    if key not in seen:
        seen.add(key)
        print("SBOM|%s|%s|%s" % (c["name"], c.get("version", ""), c.get("purl", "")))
print("SBOMLIC|%d|%d" % (licensed, len(sbom["components"])))'

  component_of_file() {
    case "$1" in
      libTK*) echo OCCT ;;
      libfreeimage*) echo FreeImage ;;
      libraw*) echo LibRaw ;;
      libjasper*) echo JasPer ;;
      libIlmImf*|libIex*|libIlmThread*|libHalf*|libImath*) echo OpenEXR ;;
      libjpegxr*|libjxrglue*) echo jxrlib ;;
      libjpeg-*|libjpeg.*) echo libjpeg-turbo ;;
      libopenjp2*) echo openjpeg ;;
      libpng16*) echo libpng ;;
      libtiff*) echo libtiff ;;
      libwebp*) echo libwebp ;;
      libfontconfig*) echo fontconfig ;;
      libfreetype*) echo freetype ;;
      libjbig*) echo jbigkit ;;
      liblcms2*) echo lcms2 ;;
      liblzma*) echo xz ;;
      libuuid*) echo libuuid ;;
      libzstd*) echo zstd ;;
      libgomp*) echo libgomp ;;
      libbz2*) echo bzip2 ;;
      libfmt*) echo fmt ;;
      libz-*|libz.*) echo zlib ;;
      *) echo "UNMAPPED:$1" ;;
    esac
  }
  component_of_sbom() {
    case "$1" in
      libfreeimage3|freeimage) echo FreeImage ;;
      libraw19|LibRaw) echo LibRaw ;;
      jasper-libs) echo JasPer ;;
      libilmbase24|libopenexr24|ilmbase|OpenEXR-libs) echo OpenEXR ;;
      libwebp6|libwebpmux3|libwebp) echo libwebp ;;
      libfontconfig1|fontconfig) echo fontconfig ;;
      libtiff5) echo libtiff ;;
      libopenjp2-7|openjpeg2) echo openjpeg ;;
      libjbig0) echo jbigkit ;;
      liblcms2-2|lcms2) echo lcms2 ;;
      libfreetype6|freetype) echo freetype ;;
      libjxr0|jxrlib) echo jxrlib ;;
      libpng16-16|libpng) echo libpng ;;
      libjpeg-turbo8|libjpeg-turbo) echo libjpeg-turbo ;;
      bzip2-libs) echo bzip2 ;;
      fmt) echo fmt ;;
      libgomp) echo libgomp ;;
      libuuid) echo libuuid ;;
      xz-libs) echo xz ;;
      cadquery_ocp_novtk) echo OCP ;;
      *) echo "UNMAPPED:$1" ;;
    esac
  }

  : > "$WORK/components.tsv"
  for ocp_tag in "$PIN|$TAG_PIN" "$OTHER|$TAG_OTHER"; do
    ocp=${ocp_tag%%|*}; tag=${ocp_tag#*|}
    docker run --rm --network none --entrypoint "${PY[0]}" -e "PIN=$ocp" "$tag" "${PY[@]:1}" -c "$INV_PY" > "$WORK/inv.$ocp"
    emit "INVENTORY,ocp=$ocp,wheel=cadquery_ocp_novtk,metadata_license=$(sed -n 's/^META|License|//p' "$WORK/inv.$ocp")"
    emit "SBOM_FIELD,ocp=$ocp,components_with_licenses=$(sed -n 's/^SBOMLIC|\([0-9]*\)|.*/\1/p' "$WORK/inv.$ocp"),components_total=$(sed -n 's/^SBOMLIC|[0-9]*|//p' "$WORK/inv.$ocp"),note=the licenses field of auditwheel.cdx.json is empty; licences come from upstream (V8)"
    : > "$WORK/sbomver.$ocp"
    grep '^SBOM|' "$WORK/inv.$ocp" | while IFS='|' read -r _ name version purl; do
      comp=$(component_of_sbom "$name")
      printf '%s|%s|%s\n' "$comp" "$name" "$version" >> "$WORK/sbomver.$ocp"
      emit "SBOM_COMPONENT,ocp=$ocp,name=$name,version=$version,component=$comp,purl=$purl"
      printf '%s\t%s\n' "$ocp" "$comp" >> "$WORK/components.tsv"
    done
    occt_ver=$(grep '^LIBFILE|libTKernel' "$WORK/inv.$ocp" | sed 's/^LIBFILE|libTKernel[^|]*\.so\.\([0-9.]*\)|.*/\1/')
    printf '%s\t%s\n' "$ocp" OCP >> "$WORK/components.tsv"
    grep '^LIBFILE|' "$WORK/inv.$ocp" | while IFS='|' read -r _ file bytes; do
      comp=$(component_of_file "$file")
      ver=$(awk -F'|' -v c="$comp" '$1 == c { print $3; exit }' "$WORK/sbomver.$ocp")
      [ "$comp" != OCCT ] || ver=$occt_ver
      [ -n "$ver" ] || ver=not_in_sbom
      if grep -q "^UNLOADED,ocp=$ocp,file=$file," "$TRACE_EVID"; then loaded=0
      elif grep -qE "^LIBTRACE,op=import,input=import-OCP,ocp=$ocp,.*,ocp_libs_loaded=[1-9]" "$TRACE_EVID"; then loaded=1
      else loaded=0; fi
      emit "WHEEL_LIB,ocp=$ocp,file=$file,bytes=$bytes,component=$comp,component_version=$ver,loaded=$loaded"
      printf '%s\t%s\n' "$ocp" "$comp" >> "$WORK/components.tsv"
    done
    printf '%s\t%s\n' "$ocp" "$occt_ver" > "$WORK/occtver.$ocp"
  done

  flat() { sed 's/<[^>]*>//g' | tr '\n' ' ' | tr -s ' \t'; }
  LICENCE_SEEN="$WORK/licence.seen"; : > "$LICENCE_SEEN"
  licence_row() {
    local comp=$1 licence=$2 url=$3 regex=$4 note=$5 ocps=$6 key status body
    key="$comp|$url"
    grep -Fxq "$key" "$LICENCE_SEEN" && return 0
    printf '%s\n' "$key" >> "$LICENCE_SEEN"
    body=$(curl -fsSL -m 40 "$url" 2>/dev/null | flat || true)
    if [ -n "$body" ] && printf '%s' "$body" | grep -Eqi "$regex"; then
      status=VERIFIED
    else
      status=ASSUMED
      note="$note; upstream text did not match at fetch time or was unreachable"
    fi
    emit "LICENCE,component=$comp,licence=$licence,source=$url,status=$status,ocp=$ocps,note=$(printf '%s' "$note" | tr ',' ';')"
  }
  pins_of() { awk -F'\t' -v c="$1" '$2 == c { print $1 }' "$WORK/components.tsv" | sort -u | paste -sd+ -; }
  has_component() { awk -F'\t' -v c="$1" '$2 == c { f = 1 } END { exit !f }' "$WORK/components.tsv"; }

  OCCT_V_PIN=$(cut -f2 "$WORK/occtver.$PIN"); OCCT_V_OTHER=$(cut -f2 "$WORK/occtver.$OTHER")
  for pair in "$PIN|$OCCT_V_PIN" "$OTHER|$OCCT_V_OTHER"; do
    ocp=${pair%%|*}; v=${pair#*|}; tagv="V$(printf '%s' "$v" | tr . _)"
    licence_row OCCT "LGPL-2.1+Open-CASCADE-exception-1.0" \
      "https://raw.githubusercontent.com/Open-Cascade-SAS/OCCT/$tagv/OCCT_LGPL_EXCEPTION.txt" \
      'Open CASCADE exception \(version 1\.0\) to GNU LGPL version 2\.1' \
      "exception text read at tag $tagv: object code incorporating material from OCCT header files may be distributed under terms of your choice provided supporting documentation gives prominent notice that it uses or is based on Open CASCADE Technology" "$ocp"
  done
  licence_row OCP "Apache-2.0" "https://raw.githubusercontent.com/CadQuery/OCP/master/LICENSE" 'Apache License' \
    "wheel METADATA License: $(sed -n 's/^META|License|//p' "$WORK/inv.$PIN") (read from the installed dist-info)" "$(pins_of OCP)"
  licence_row FreeImage "GPL-2.0/GPL-3.0/FIPL-1.0" "https://freeimage.sourceforge.io/license.html" \
    'GNU General Public License, version 2\.0.*FreeImage Public License' \
    "dual licence chosen by the user of the library; the wheel carries Ubuntu libfreeimage3 3.18.0 and AlmaLinux freeimage 3.17.0 builds" "$(pins_of FreeImage)"
  licence_row LibRaw "LGPL-2.1/CDDL-1.0" "https://raw.githubusercontent.com/LibRaw/LibRaw/0.19.5/COPYRIGHT" \
    'LESSER GENERAL PUBLIC LICENSE version 2\.1.*COMMON DEVELOPMENT AND DISTRIBUTION LICENSE' \
    "one of two licences as you choose; tag 0.19.5 is the SBOM version on both pins" "$(pins_of LibRaw)"
  if has_component JasPer; then
    licence_row JasPer "JasPer-2.0" "https://raw.githubusercontent.com/jasper-software/jasper/version-2.0.14/LICENSE" \
      'JasPer License Version 2\.0' "present only in the wheel of $OTHER if the inventory above lists libjasper; SBOM jasper-libs 2.0.14" "$(pins_of JasPer)"
  fi
  has_component OpenEXR && licence_row OpenEXR "BSD-3-Clause" "https://raw.githubusercontent.com/AcademySoftwareFoundation/openexr/v2.3.0/LICENSE" \
    'Redistribution and use in source and binary forms.*Industrial Light' "IlmBase Half Iex IlmThread IlmImf Imath libraries; tag v2.3.0" "$(pins_of OpenEXR)"
  has_component libjpeg-turbo && licence_row libjpeg-turbo "IJG+BSD-3-Clause+Zlib" "https://raw.githubusercontent.com/libjpeg-turbo/libjpeg-turbo/main/LICENSE.md" \
    'Independent JPEG Group' "ref main" "$(pins_of libjpeg-turbo)"
  has_component libpng && licence_row libpng "libpng-2.0" "https://raw.githubusercontent.com/pnggroup/libpng/v1.6.37/LICENSE" \
    'PNG Reference Library License' "tag v1.6.37" "$(pins_of libpng)"
  has_component libtiff && licence_row libtiff "libtiff-BSD-like" "https://gitlab.com/libtiff/libtiff/-/raw/master/LICENSE.md" \
    'Permission to use, copy, modify, distribute' "ref master" "$(pins_of libtiff)"
  has_component lcms2 && licence_row lcms2 "MIT" "https://raw.githubusercontent.com/mm2/Little-CMS/master/LICENSE" \
    'Permission is hereby granted' "ref master" "$(pins_of lcms2)"
  has_component openjpeg && licence_row openjpeg "BSD-2-Clause" "https://raw.githubusercontent.com/uclouvain/openjpeg/master/LICENSE" \
    'Redistribution and use in source and binary forms' "ref master" "$(pins_of openjpeg)"
  has_component libwebp && licence_row libwebp "BSD-3-Clause" "https://raw.githubusercontent.com/webmproject/libwebp/main/COPYING" \
    'Redistribution and use in source and binary forms' "ref main" "$(pins_of libwebp)"
  has_component jxrlib && licence_row jxrlib "BSD-2-Clause" "https://raw.githubusercontent.com/4creators/jxrlib/master/LICENSE" \
    'Redistribution' "ref master" "$(pins_of jxrlib)"
  has_component freetype && licence_row freetype "FTL-or-GPL-2.0" "https://freetype.org/license.html" \
    'FreeType comes with two licenses' "ref project page" "$(pins_of freetype)"
  has_component fontconfig && licence_row fontconfig "HPND-sell-variant" "https://gitlab.freedesktop.org/fontconfig/fontconfig/-/raw/main/COPYING" \
    'Permission to use, copy, modify, distribute' "ref main" "$(pins_of fontconfig)"
  has_component bzip2 && licence_row bzip2 "bzip2-1.0.6" "https://sourceware.org/git/?p=bzip2.git;a=blob_plain;f=LICENSE;hb=HEAD" \
    'This program, "bzip2"' "ref HEAD" "$(pins_of bzip2)"
  has_component zlib && licence_row zlib "Zlib" "https://raw.githubusercontent.com/madler/zlib/master/LICENSE" 'zlib' "ref master" "$(pins_of zlib)"
  has_component jbigkit && licence_row jbigkit "GPL-2.0" "https://www.cl.cam.ac.uk/~mgk25/jbigkit/" \
    'free software under the.*General Public License' "libjbig0 in the wheel; the project page links the GPL v2 text and offers a commercial licence; an or-later grant is not stated on the page" "$(pins_of jbigkit)"
  has_component xz && licence_row xz "0BSD+public-domain" "https://raw.githubusercontent.com/tukaani-project/xz/master/COPYING" \
    'public domain|0BSD' "ref master" "$(pins_of xz)"
  has_component zstd && licence_row zstd "BSD-3-Clause/GPL-2.0" "https://raw.githubusercontent.com/facebook/zstd/dev/LICENSE" \
    'BSD License' "ref dev" "$(pins_of zstd)"
  has_component libuuid && licence_row libuuid "BSD-3-Clause" "https://raw.githubusercontent.com/util-linux/util-linux/master/libuuid/COPYING" \
    'Modified BSD License' "ref master; the COPYING file points to COPYING.BSD-3-Clause" "$(pins_of libuuid)"
  has_component libgomp && licence_row libgomp "GPL-3.0+GCC-Runtime-Library-Exception" "https://raw.githubusercontent.com/gcc-mirror/gcc/master/COPYING.RUNTIME" \
    'GCC RUNTIME LIBRARY EXCEPTION' "ref master" "$(pins_of libgomp)"
  has_component fmt && licence_row fmt "MIT" "https://raw.githubusercontent.com/fmtlib/fmt/master/LICENSE" 'Permission is hereby granted' "ref master" "$(pins_of fmt)"
  awk -F'\t' '$2 ~ /^UNMAPPED:/ { print $2 }' "$WORK/components.tsv" | sort -u | while IFS= read -r u; do
    emit "LICENCE,component=$u,licence=unknown,source=none,status=ASSUMED,ocp=-,note=no mapping from the file name to a component"
  done

  LOADED_LINE=$(grep '^D18_TRACE,' "$TRACE_EVID" | sed 's/^D18_TRACE,//' | tr '\n' ';')
  emit "D18_OUTCOME,choice=$D18,pin=$PIN,removed=none,reason=loaded_at_import,experiment=not_run,lawyer_ok=needed"
  emit "RECORD,begin=draft engineering record for 71-DECISION.md (plan 71-08)"
  emit "RECORD,loading=libfreeimage and libraw are loaded at import OCP through DT_NEEDED (libTKService -> libfreeimage -> libraw) on every operation that imports OCP, on both pins; libjasper only on 8.0.1.0.0 (libraw -> libjasper); render, dxf, rsvg-convert and stlstats load none of the three (measured: $LOADED_LINE)"
  emit "RECORD,consequence=loaded is not the same as called (assumption A1: only the patchelf experiment answers whether the code paths are used); the D-18 condition 'unused libraries are removed' finds nothing removable on the trace, so the libraries stay and the licences are recorded"
  emit "RECORD,use=только внутреннее использование, образ не распространяется вовне"
  emit "RECORD,assumption_A2=ASSUMPTION: internal use without distribution does not trigger mandatory copyleft obligations (GPL/LGPL/CDDL terms of FreeImage, LibRaw, jbigkit, libgomp); this is a working assumption, not a legal finding"
  emit "RECORD,statuses=every LICENCE row carries VERIFIED (the expected licence text was found at the source URL at fetch time) or ASSUMED (not found or not reachable); VERIFIED identifies the licence, it does not clear the use"
  emit "RECORD,legal=the libraries are not legally cleared; a lawyer review is requested and does not block the phase"
  emit "LAWYER_OK=needed,blocking=no"
  emit "NEED_CHECK=нужен OK юриста"
  exit 0
fi

exit 64
