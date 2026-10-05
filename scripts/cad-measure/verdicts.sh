#!/usr/bin/env bash
# Pass/fail verdicts of the Phase 71 measurement scripts, kept apart from the docker
# calls so that each one can be tried on synthetic numbers. A verdict returns 0 only
# when every count it is given is positive and equals the count the caller expected.
#
# Source it:   . scripts/cad-measure/verdicts.sh
# or run it:   scripts/cad-measure/verdicts.sh --self-test

is_count() {
  case "$1" in ''|*[!0-9]*) return 1 ;; esac
}

identity_verdict() {
  local total=$1 same=$2 diff=$3 bad=$4 expected=$5 v
  for v in "$total" "$same" "$diff" "$bad" "$expected"; do is_count "$v" || return 1; done
  [ "$expected" -gt 0 ] && [ "$total" = "$expected" ] && [ "$same" = "$total" ] && [ "$diff" = 0 ] && [ "$bad" = 0 ]
}

image_pair_verdict() {
  local old=$1 new=$2 old_id=$3 new_id=$4 old_pin new_pin
  [ -n "$old_id" ] && [ -n "$new_id" ] && [ "$old_id" != "$new_id" ] || return 1
  old_pin=$(printf '%s' "$old" | sed -n 's/^[^:]*:ocp\([^-]*\)-.*$/\1/p')
  new_pin=$(printf '%s' "$new" | sed -n 's/^[^:]*:ocp\([^-]*\)-.*$/\1/p')
  [ -n "$old_pin" ] && [ "$old_pin" = "$new_pin" ]
}

render_verdict() {
  local parts=$1 robust=$2 drawings=$3 pngs=$4 fail=$5 det_runs=$6 det=$7
  local want_parts=$8 want_robust=$9 want_drawings=${10} want_pngs=${11}
  local v
  for v in "$parts" "$robust" "$drawings" "$pngs" "$fail" "$det_runs" "$want_parts" "$want_robust" "$want_drawings" "$want_pngs"; do
    is_count "$v" || return 1
  done
  [ "$fail" = 0 ] && [ "$det" = identical ] &&
    [ "$want_parts" -gt 0 ] && [ "$parts" = "$want_parts" ] && [ "$det_runs" = "$want_parts" ] &&
    [ "$robust" = "$want_robust" ] && [ "$drawings" = "$want_drawings" ] && [ "$pngs" = "$want_pngs" ]
}

landlock_verdict() {
  local inputs=$1 suff=$2 diff=$3 leaked=$4 helpers_seen=$5 helpers_want=$6 v
  for v in "$inputs" "$suff" "$diff" "$leaked" "$helpers_seen" "$helpers_want"; do is_count "$v" || return 1; done
  [ "$inputs" -gt 0 ] && [ "$suff" = "$inputs" ] && [ "$diff" = 0 ] && [ "$leaked" = 0 ] &&
    [ "$helpers_want" -gt 0 ] && [ "$helpers_seen" = "$helpers_want" ]
}

f3d_verdict() {
  local fail=$1 vtk_ok=$2 f3d_ok=$3 expected=$4 sheet_ok=$5 v
  for v in "$fail" "$vtk_ok" "$f3d_ok" "$expected" "$sheet_ok"; do is_count "$v" || return 1; done
  [ "$expected" -gt 0 ] && [ "$fail" = 0 ] && [ "$vtk_ok" = "$expected" ] && [ "$f3d_ok" = "$expected" ] && [ "$sheet_ok" = 1 ]
}

kept_verdict() {
  local entries=$1 pruned=$2
  is_count "$entries" && [ "$entries" -gt 0 ] || return 1
  case "$pruned" in "PRUNED,kept=$entries,"*) return 0 ;; *) return 1 ;; esac
}

positive_reading() {
  local v
  for v in "$@"; do
    case "$v" in ''|*[!0-9.]*) return 1 ;; esac
    awk -v v="$v" 'BEGIN { exit !(v + 0 > 0) }' || return 1
  done
}

verdicts_self_test() {
  local fails=0 n=0
  t() {
    local want=$1 label=$2 got=0
    shift 2
    "$@" >/dev/null 2>&1 || got=$?
    n=$((n + 1))
    if { [ "$want" = ok ] && [ "$got" = 0 ]; } || { [ "$want" = no ] && [ "$got" != 0 ]; }; then return 0; fi
    echo "SELF_TEST_FAIL,case=$label,want=$want,rc=$got" >&2
    fails=$((fails + 1))
  }
  t ok identity-all-five identity_verdict 5 5 0 0 5
  t no identity-none-compared identity_verdict 0 0 0 0 0
  t no identity-fewer-than-expected identity_verdict 4 4 0 0 5
  t no identity-different identity_verdict 5 4 1 0 5
  t no identity-broken identity_verdict 5 4 0 1 5
  t no identity-same-below-total identity_verdict 5 4 0 0 5
  t no identity-empty-count identity_verdict 5 "" 0 0 5
  t no identity-counts-inconsistent-different identity_verdict 5 5 1 0 5
  t no identity-counts-inconsistent-broken identity_verdict 5 5 0 1 5
  t ok image-pair-same-pin image_pair_verdict octoconv-cad-measure:ocp7.9.3.1.1-pruned-aaaa octoconv-cad-measure:ocp7.9.3.1.1-pruned-bbbb sha256:1 sha256:2
  t no image-pair-same-image image_pair_verdict octoconv-cad-measure:ocp7.9.3.1.1-pruned-aaaa octoconv-cad-measure:ocp7.9.3.1.1-pruned-aaaa sha256:1 sha256:1
  t no image-pair-other-pin image_pair_verdict octoconv-cad-measure:ocp7.9.3.1.1-pruned-aaaa octoconv-cad-measure:ocp8.0.1.0.0-pruned-bbbb sha256:1 sha256:2
  t no image-pair-no-id image_pair_verdict octoconv-cad-measure:ocp7.9.3.1.1-pruned-aaaa octoconv-cad-measure:ocp7.9.3.1.1-pruned-bbbb "" sha256:2
  t no image-pair-no-pin-in-tag image_pair_verdict x y sha256:1 sha256:2
  t ok render-complete render_verdict 5 6 8 108 0 5 identical 5 6 8 108
  t no render-fewer-parts render_verdict 4 6 8 108 0 5 identical 5 6 8 108
  t no render-fewer-determinism-runs render_verdict 5 6 8 108 0 4 identical 5 6 8 108
  t no render-fewer-pngs render_verdict 5 6 8 107 0 5 identical 5 6 8 108
  t no render-fewer-robust render_verdict 5 5 8 108 0 5 identical 5 6 8 108
  t no render-fewer-drawings render_verdict 5 6 7 108 0 5 identical 5 6 8 108
  t no render-fail render_verdict 5 6 8 108 1 5 identical 5 6 8 108
  t no render-nondeterministic render_verdict 5 6 8 108 0 5 DIFFERENT 5 6 8 108
  t no render-nothing-expected render_verdict 0 0 0 0 0 0 identical 0 0 0 0
  t ok landlock-complete landlock_verdict 34 34 0 0 4 4
  t no landlock-no-inputs landlock_verdict 0 0 0 0 4 4
  t no landlock-insufficient landlock_verdict 34 33 0 0 4 4
  t no landlock-different landlock_verdict 34 34 1 0 4 4
  t no landlock-leaked landlock_verdict 34 34 0 1 4 4
  t no landlock-helper-without-inputs landlock_verdict 34 34 0 0 3 4
  t no landlock-no-helpers-wanted landlock_verdict 34 34 0 0 0 0
  t ok f3d-complete f3d_verdict 0 15 15 15 1
  t no f3d-no-sheet f3d_verdict 0 15 15 15 0
  t no f3d-fewer-f3d f3d_verdict 0 15 14 15 1
  t no f3d-fewer-vtk f3d_verdict 0 14 15 15 1
  t no f3d-failures f3d_verdict 1 15 15 15 1
  t no f3d-nothing-expected f3d_verdict 0 0 0 0 1
  t ok kept-all-listed kept_verdict 77 "PRUNED,kept=77,removed=299,bytes_removed=1"
  t no kept-empty-list kept_verdict 0 "PRUNED,kept=0,removed=376,bytes_removed=1"
  t no kept-fewer kept_verdict 77 "PRUNED,kept=70,removed=299,bytes_removed=1"
  t no kept-no-line kept_verdict 77 ""
  t ok reading-positive positive_reading 24.2 24.9
  t no reading-zero positive_reading 0.0
  t no reading-empty positive_reading ""
  t no reading-second-empty positive_reading 24.2 ""
  echo "SELF_TEST,cases=$n,failed=$fails"
  [ "$fails" = 0 ]
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  set -euo pipefail
  case "${1:-}" in
    --self-test) verdicts_self_test ;;
    *) echo "usage: $0 --self-test" >&2; exit 64 ;;
  esac
fi
