#!/usr/bin/env bash
# Chart validation for SEED-011/CAPA-01 and D-06: helm lint + helm template for
# both the default values and the values-capacity-floor.yaml overlay, the
# per-class isolation diff (SC2), and the negative cases proving D-02's
# fail-fast validation actually fires. No cluster, no external services.
#
# This file is the ONE copy of those checks. The `helm` job in
# .github/workflows/ci.yml calls it, and so does scripts/ci-local.sh -- the
# local stand-in used while GitHub Actions is unavailable (billing, restored
# end of August 2026; see .planning/todos/pending/2026-08-18-inspect-honest-
# reporting-open-items.md item 3). Two copies of a check are two checks that
# can disagree, and a local run that "passes" against a stale copy of CI's
# assertions is worse than no local run at all.
#
# Run from the repository root.

set -euo pipefail

CHART=deploy/chart/octoconv
OVERLAY=deploy/chart/octoconv/values-capacity-floor.yaml

# Placeholder secrets (dev-only, never real credentials): base
# values.yaml intentionally ships no `secrets:` block (always
# supplied by an overlay or --set in real usage), so a bare helm
# invocation fails on templates/secret.yaml before ever reaching a
# KEDA template. See 54-01-SUMMARY.md Interface Contract.
SECRETS_SET="--set secrets.databaseUrl=x --set secrets.apiKeySalt=x --set secrets.webhookSigningSecret=x --set secrets.s3AccessKey=x --set secrets.s3SecretKey=x --set secrets.postgresPassword=x"

# --set keda.enabled=true --set prometheus.enabled=true is
# MANDATORY on every helm lint/helm template invocation below.
# Every ScaledObject template is gated on
# `and .Values.keda.enabled .Values.prometheus.enabled` (the
# chart's co-dependency guard); omitting either flag renders ZERO
# ScaledObjects and makes every assertion in this job pass
# vacuously. Do not "clean up" these flags.

# `helm lint` below covers chart structure/schema only. It is NOT part
# of the D-02 validation surface and must not be treated as such
# (WR-01): helm v4.2.3 downgrades a template `fail` to an INFO log
# (`level=INFO msg="funcMap fail"`) and still reports
# `0 chart(s) failed`, exit 0. All D-02 coverage in this job comes
# from the `helm template` steps below -- do not delete one of those
# on the assumption that lint still catches it.
echo "### helm lint (default values)"
helm lint "$CHART" --set keda.enabled=true --set prometheus.enabled=true $SECRETS_SET

echo "### helm lint (capacity-floor overlay)"
helm lint "$CHART" -f "$OVERLAY" --set keda.enabled=true --set prometheus.enabled=true $SECRETS_SET

echo "### render both variants"
DEFAULT_RENDER=$(mktemp)
OVERLAY_RENDER=$(mktemp)
helm template octoconv "$CHART" --set keda.enabled=true --set prometheus.enabled=true $SECRETS_SET > "$DEFAULT_RENDER"
helm template octoconv "$CHART" -f "$OVERLAY" --set keda.enabled=true --set prometheus.enabled=true $SECRETS_SET > "$OVERLAY_RENDER"

echo "### anti-vacuity gate: exactly 8 ScaledObjects must render in BOTH files, checked before any diff or isolation claim"
# `grep -c` exits 1 on zero matches, and under `set -euo pipefail` an
# assignment whose command substitution fails kills the script. Zero
# matches is EXACTLY the vacuity case this gate exists to report, so
# without `|| true` the diagnostic below would be unreachable in the
# only case it matters (same hazard the `grep -v` capture further
# down is already guarded against).
DEFAULT_COUNT=$(grep -c '^kind: ScaledObject' "$DEFAULT_RENDER" || true)
OVERLAY_COUNT=$(grep -c '^kind: ScaledObject' "$OVERLAY_RENDER" || true)
if [ "$DEFAULT_COUNT" -ne 8 ] || [ "$OVERLAY_COUNT" -ne 8 ]; then
  echo "FAIL: expected 8 ScaledObjects in both renders, got default=$DEFAULT_COUNT overlay=$OVERLAY_COUNT"
  exit 1
fi
echo "OK: 8 ScaledObjects in both renders"

echo "### per-class isolation (SC2): the seven non-image classes must render byte-identical, image must differ -- specific about WHICH object changed, not just that the diff is small"
for class in document html audio av ocr pandoc probe; do
  d1=$(helm template octoconv "$CHART" --set keda.enabled=true --set prometheus.enabled=true $SECRETS_SET --show-only "templates/scaledobject-${class}.yaml")
  d2=$(helm template octoconv "$CHART" -f "$OVERLAY" --set keda.enabled=true --set prometheus.enabled=true $SECRETS_SET --show-only "templates/scaledobject-${class}.yaml")
  if [ "$d1" != "$d2" ]; then
    echo "FAIL: class $class changed but should be isolated from the image-only overlay"
    diff <(echo "$d1") <(echo "$d2") || true
    exit 1
  fi
  echo "OK: $class unchanged"
done

d1=$(helm template octoconv "$CHART" --set keda.enabled=true --set prometheus.enabled=true $SECRETS_SET --show-only templates/scaledobject-image.yaml)
d2=$(helm template octoconv "$CHART" -f "$OVERLAY" --set keda.enabled=true --set prometheus.enabled=true $SECRETS_SET --show-only templates/scaledobject-image.yaml)
if [ "$d1" == "$d2" ]; then
  echo "FAIL: expected image class to DIFFER between default and overlay renders"
  exit 1
fi
echo "OK: image changed (expected)"

echo "### rendered VALUE assertions (SC1/D-05): every check above is a SHAPE check -- a chart that silently clamped the operator's floor of 2 down to 1 (removing the D-05 fallback effect entirely) would still pass all of them. These assert the actual rendered numbers."
assert_exact_line() {
  # $1 = rendered text, $2 = exact expected line, $3 = label
  if ! printf '%s\n' "$1" | grep -qxF "$2"; then
    echo "FAIL: $3 -- the rendered image ScaledObject does not contain the exact line: [$2]"
    echo "actual floor/fallback lines were:"
    printf '%s\n' "$1" | grep -nE '^  minReplicaCount:|^    replicas:' || true
    exit 1
  fi
}
# $d1/$d2 are the default and overlay image renders from the isolation
# check directly above.
assert_exact_line "$d1" '  minReplicaCount: 0' "default floor (D-01: shipped default is a zero floor)"
assert_exact_line "$d1" '    replicas: 1'      "default fallback (D-05: max(1,0) is byte-identical to the pre-phase hardcoded 1)"
assert_exact_line "$d2" '  minReplicaCount: 2' "overlay floor (the overlay's only key must actually reach the manifest)"
assert_exact_line "$d2" '    replicas: 2'      "overlay fallback (D-05: a Prometheus outage must not undo the configured floor)"
echo "OK: image renders 0/1 by default and 2/2 under the capacity-floor overlay"

echo "### whole-chart isolation scope (SC2): every differing line in the full-chart diff must be a minReplicaCount:/replicas: line, and the diff must be non-empty"
DIFF_OUT=$(diff "$DEFAULT_RENDER" "$OVERLAY_RENDER" || true)
CHANGED_LINES=$(echo "$DIFF_OUT" | grep '^[<>]' || true)
if [ -z "$CHANGED_LINES" ]; then
  echo "FAIL: overlay produced no diff at all -- a no-op overlay must not pass"
  exit 1
fi
# The pattern is ANCHORED to the exact rendered indentation of the two
# ScaledObject fields that are legitimately allowed to change
# (`  minReplicaCount: N` and `    replicas: N`). An unanchored
# substring match on `replicas:` would also match the `replicas:`
# field of every Deployment in this chart (api, webhook-worker,
# mcp-http, asynqmon, prometheus, ...), so an overlay that rescaled
# the API Deployment would silently pass a check whose whole claim is
# isolation to ONE ScaledObject.
IN_SCOPE_RE='^[<>] (  minReplicaCount: |    replicas: )[0-9]+$'
# grep -v exits 1 when nothing is left unmatched; guard the count
# capture against that under set -e/pipefail with an explicit
# `|| true` fallback rather than letting the pipeline abort.
OUT_OF_SCOPE=$(echo "$CHANGED_LINES" | { grep -vE "$IN_SCOPE_RE" || true; } | wc -l | tr -d ' ')
if [ "$OUT_OF_SCOPE" -ne 0 ]; then
  echo "FAIL: diff touched lines outside the image ScaledObject's minReplicaCount:/replicas: fields"
  echo "$CHANGED_LINES" | grep -vE "$IN_SCOPE_RE" || true
  exit 1
fi
# Exact count, not just "all in scope": values-capacity-floor.yaml
# sets exactly one key on exactly one class, which moves exactly two
# fields, i.e. 4 diff lines (one `<` and one `>` each). Any other
# number means the overlay stopped being the single-class example the
# SC2 isolation claim rests on.
CHANGED_COUNT=$(echo "$CHANGED_LINES" | wc -l | tr -d ' ')
if [ "$CHANGED_COUNT" -ne 4 ]; then
  echo "FAIL: expected exactly 4 changed lines (minReplicaCount + fallback.replicas, x default/overlay), got $CHANGED_COUNT"
  echo "$CHANGED_LINES"
  exit 1
fi
echo "OK: whole-chart diff is exactly the 4 expected image-ScaledObject floor/fallback lines"
rm -f "$DEFAULT_RENDER" "$OVERLAY_RENDER"

echo "### negative case A -- floor above ceiling (D-02): helm template MUST exit non-zero"
set +e
OUT_A=$(helm template octoconv "$CHART" --set keda.enabled=true --set prometheus.enabled=true $SECRETS_SET --set keda.image.minReplicaCount=99 2>&1)
RC_A=$?
set -e
echo "$OUT_A"
if [ "$RC_A" -eq 0 ]; then
  echo "FAIL: expected D-02's exceeds-maxReplicaCount guard to fire on this override, but helm template exited 0"
  exit 1
fi
echo "$OUT_A" | grep -q 'keda.image.minReplicaCount'
echo "$OUT_A" | grep -q 'keda.image.maxReplicaCount'
echo "OK: negative case A failed as expected (rc=$RC_A)"

echo "### negative case B -- negative floor (D-02): helm template MUST exit non-zero"
set +e
OUT_B=$(helm template octoconv "$CHART" --set keda.enabled=true --set prometheus.enabled=true $SECRETS_SET --set 'keda.ocr.minReplicaCount=-1' 2>&1)
RC_B=$?
set -e
echo "$OUT_B"
if [ "$RC_B" -eq 0 ]; then
  echo "FAIL: expected D-02's negative-value guard to fire on this override, but helm template exited 0"
  exit 1
fi
echo "$OUT_B" | grep -q 'keda.ocr.minReplicaCount'
echo "OK: negative case B failed as expected (rc=$RC_B)"

echo "### negative case C -- non-integer floors must NOT coerce silently (CR-01): sprig's int is cast.ToInt, which discards its parse error and returns 0. Cases A and B above use only BARE INTEGERS, so they never touch this path -- every input below used to render minReplicaCount: 0 and exit 0, bypassing BOTH guards and silently discarding the operator's floor."
for bad in '99.9' '-0.5' '2.9' '2.0' 'abc' 'true' '' 'null'; do
  set +e
  OUT_C=$(helm template octoconv "$CHART" --set keda.enabled=true --set prometheus.enabled=true $SECRETS_SET --set "keda.image.minReplicaCount=${bad}" 2>&1)
  RC_C=$?
  set -e
  if [ "$RC_C" -eq 0 ]; then
    echo "FAIL: --set keda.image.minReplicaCount=[${bad}] rendered successfully -- the raw-kind guard did not fire, so the configured floor was silently coerced"
    echo "$OUT_C" | grep -E '^  minReplicaCount:' || true
    exit 1
  fi
  echo "$OUT_C" | grep -q 'keda.image.minReplicaCount'
  echo "$OUT_C" | grep -q 'must be a whole number'
  echo "OK: [${bad}] rejected (rc=$RC_C)"
done

echo "### negative case D -- a null/absent ceiling is its own misconfiguration (CR-01): without the raw-kind check it coerced to 0 and got mis-reported as a minReplicaCount problem"
set +e
OUT_D=$(helm template octoconv "$CHART" --set keda.enabled=true --set prometheus.enabled=true $SECRETS_SET --set 'keda.image.maxReplicaCount=null' 2>&1)
RC_D=$?
set -e
echo "$OUT_D"
if [ "$RC_D" -eq 0 ]; then
  echo "FAIL: expected a null keda.image.maxReplicaCount to abort the render, but helm template exited 0"
  exit 1
fi
echo "$OUT_D" | grep -q 'keda.image.maxReplicaCount'
echo "$OUT_D" | grep -q 'must be a whole number'
echo "OK: negative case D failed as expected (rc=$RC_D)"
