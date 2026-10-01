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

echo "### anti-vacuity gate: exactly 9 ScaledObjects must render in BOTH files, checked before any diff or isolation claim"
# `grep -c` exits 1 on zero matches, and under `set -euo pipefail` an
# assignment whose command substitution fails kills the script. Zero
# matches is EXACTLY the vacuity case this gate exists to report, so
# without `|| true` the diagnostic below would be unreachable in the
# only case it matters (same hazard the `grep -v` capture further
# down is already guarded against).
DEFAULT_COUNT=$(grep -c '^kind: ScaledObject' "$DEFAULT_RENDER" || true)
OVERLAY_COUNT=$(grep -c '^kind: ScaledObject' "$OVERLAY_RENDER" || true)
if [ "$DEFAULT_COUNT" -ne 9 ] || [ "$OVERLAY_COUNT" -ne 9 ]; then
  echo "FAIL: expected 9 ScaledObjects in both renders, got default=$DEFAULT_COUNT overlay=$OVERLAY_COUNT"
  exit 1
fi
echo "OK: 9 ScaledObjects in both renders"

echo "### per-class isolation (SC2): the eight non-image classes must render byte-identical, image must differ -- specific about WHICH object changed, not just that the diff is small"
for class in document html audio av ocr pandoc probe tectonic; do
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
echo "### every Deployment that gets the app secret must wait for BOTH startup deps"
# The class guard, not the instance. All ten Go entrypoints treat a startup
# dependency failure as fatal, so any pod started before its dependencies answer
# crash-loops until they do. Fixing the one deployment that was reported and
# leaving nine identical ones is the shape the previous milestone spent eight
# review rounds learning to stop repeating -- so this asserts the property for
# every deployment at once, and a new one cannot be added without it.
#
# BOTH waits are required, and the second is the one the reported symptom hid.
# A postgres-only wait was written, installed on a cold namespace, and the api
# still came up at restartCount 2 -- because storage.New fails closed on an
# unreachable MinIO *and* on an absent bucket, and the bucket is created by a
# post-install HOOK that cannot have run when the main-release pods start. So
# the guard counts wait-for-postgres AND wait-for-bucket; passing on one of the
# two is exactly the half-fix the live gate caught.
#
# One yq pass emitting "<name> <hasSecretEnv> <hasInitContainer>" per Deployment.
# The first version ran a yq invocation per deployment inside a shell loop; the
# per-name expressions errored, the errors went to /dev/null, the counts came
# back empty, and the guard passed on a deployment with the initContainer
# REMOVED. Found by inverting it, which is the only reason it is not still
# silently green.
TRIPLES=$(yq -e '. | select(.kind=="Deployment") | .metadata.name + " " + (([.spec.template.spec.containers[].envFrom // [] | .[] | select(.secretRef.name != null)] | length) | tostring) + " " + (([.spec.template.spec.initContainers // [] | .[] | select(.name=="wait-for-postgres")] | length) | tostring) + " " + (([.spec.template.spec.initContainers // [] | .[] | select(.name=="wait-for-bucket")] | length) | tostring)' "$DEFAULT_RENDER" | grep -v '^---$')

if [ -z "$TRIPLES" ]; then
  echo "FAIL: no Deployments parsed out of the render -- this guard would pass vacuously"
  exit 1
fi

MISSING=$(echo "$TRIPLES" | awk '$2 > 0 && ($3 == 0 || $4 == 0) { print $1 " (postgres=" $3 " bucket=" $4 ")" }')
if [ -n "$MISSING" ]; then
  echo "FAIL: these Deployments receive the app secret (so they will dial postgres"
  echo "      AND MinIO at startup, both fatal on failure) but are missing one or"
  echo "      both wait initContainers, so they crash-loop on a cold install:"
  echo "$MISSING" | sed 's/^/        /'
  exit 1
fi
echo "OK: every Deployment carrying the app secret waits for postgres and the bucket ($(echo "$TRIPLES" | awk '$2 > 0' | wc -l | tr -d ' ') of them)"

echo "### the MinIO pod hands root-owned data to uid 65532 before the server starts"
minio_ownership() {
  yq 'select(.kind=="Deployment" and .metadata.name=="minio") | .spec.template.spec as $p | ($p.containers[] | select(.name=="minio")) as $s | ($s.volumeMounts[] | select(.name=="minio-data") | .mountPath) as $m | [$p.initContainers // [] | .[] | select(.volumeMounts[].name=="minio-data")] as $i | $i[0] as $c | "inits=" + ($i | length | tostring) + " image=" + (if $c.image == $s.image then "same" else "differs" end) + " mount=" + (if ($c.volumeMounts[] | select(.name=="minio-data") | .mountPath) == $m then "same" else "differs" end) + " target=" + (if $c.command[-1] == $m then "same" else "differs" end) + " flags=" + ([$c.command[] | select(test("^-"))] | join(",")) + " user=" + ($c.securityContext.runAsUser | tostring) + " group=" + ($c.securityContext.runAsGroup | tostring) + " escalation=" + ($c.securityContext.allowPrivilegeEscalation | tostring) + " rorf=" + ($c.securityContext.readOnlyRootFilesystem | tostring) + " drop=" + ($c.securityContext.capabilities.drop | join(",")) + " add=" + ($c.securityContext.capabilities.add | join(",")) + " owner=" + ([$c.command[] | select(test("^[0-9]+:[0-9]+$"))] | join(",")) + " runAsUser=" + ($p.securityContext.runAsUser | tostring) + " runAsGroup=" + ($p.securityContext.runAsGroup | tostring) + " fsGroup=" + ($p.securityContext.fsGroup | tostring)' "$1" 2>&1 | grep -v '^---$' || true
}
MIGRATE_WANT="inits=1 image=same mount=same target=same flags=-R,-h,--from=0 user=0 group=0 escalation=false rorf=true drop=ALL add=CHOWN owner=65532:65532 runAsUser=65532 runAsGroup=65532 fsGroup=65532"
MIGRATE_GOT=$(minio_ownership "$DEFAULT_RENDER")
if [ -z "$MIGRATE_GOT" ]; then
  echo "FAIL: no Deployment named minio parsed out of the render -- this guard would pass vacuously"
  exit 1
fi
if [ "$MIGRATE_GOT" != "$MIGRATE_WANT" ]; then
  echo "FAIL: the MinIO pod's ownership-migration init container has the wrong shape"
  echo "      want: $MIGRATE_WANT"
  echo "      got:  $MIGRATE_GOT"
  exit 1
fi
echo "OK: MinIO pod runs one init container from the server image that chowns $(echo "$MIGRATE_WANT" | sed 's/.*owner=\([^ ]*\).*/\1/') over the data mount with only CHOWN"

echo "### minio.dataOwnership.uid/gid move the pod identity and the chown target together, and nothing else"
OWN_BASE=$(mktemp)
OWN_SET=$(mktemp)
OWN_VALUES=$(mktemp)
OWN_FILE=$(mktemp)
helm template octoconv "$CHART" $SECRETS_SET > "$OWN_BASE"
helm template octoconv "$CHART" $SECRETS_SET --set minio.dataOwnership.uid=1000 --set minio.dataOwnership.gid=2000 > "$OWN_SET"
OWN_WANT="${MIGRATE_WANT/owner=65532:65532 runAsUser=65532 runAsGroup=65532 fsGroup=65532/owner=1000:2000 runAsUser=1000 runAsGroup=2000 fsGroup=2000}"
OWN_GOT=$(minio_ownership "$OWN_SET")
if [ "$OWN_GOT" != "$OWN_WANT" ]; then
  echo "FAIL: --set minio.dataOwnership.uid=1000 gid=2000 did not move the pod identity and the chown target together"
  echo "      want: $OWN_WANT"
  echo "      got:  $OWN_GOT"
  exit 1
fi
OWN_DIFF=$(diff "$OWN_BASE" "$OWN_SET" | grep '^[<>]' || true)
OWN_DIFF_COUNT=$(echo "$OWN_DIFF" | grep -c . || true)
OWN_DIFF_BAD=$(echo "$OWN_DIFF" | { grep -vE '^[<>][[:space:]]+((runAsUser|runAsGroup|fsGroup): [0-9]+|- "[0-9]+:[0-9]+")$' || true; })
if [ "$OWN_DIFF_COUNT" -ne 8 ] || [ -n "$OWN_DIFF_BAD" ]; then
  echo "FAIL: expected exactly 8 changed lines (runAsUser, runAsGroup, fsGroup, owner x default/custom), got $OWN_DIFF_COUNT"
  echo "$OWN_DIFF" | sed 's/^/        /'
  exit 1
fi
printf 'minio:\n  dataOwnership:\n    uid: 1000000\n    gid: 1000001\n' > "$OWN_VALUES"
helm template octoconv "$CHART" $SECRETS_SET -f "$OWN_VALUES" > "$OWN_FILE"
OWN_WANT="${MIGRATE_WANT/owner=65532:65532 runAsUser=65532 runAsGroup=65532 fsGroup=65532/owner=1000000:1000001 runAsUser=1000000 runAsGroup=1000001 fsGroup=1000001}"
OWN_GOT=$(minio_ownership "$OWN_FILE")
if [ "$OWN_GOT" != "$OWN_WANT" ]; then
  echo "FAIL: a values file with uid 1000000 / gid 1000001 did not render as whole numbers"
  echo "      want: $OWN_WANT"
  echo "      got:  $OWN_GOT"
  exit 1
fi
rm -f "$OWN_BASE" "$OWN_SET" "$OWN_VALUES" "$OWN_FILE"
echo "OK: uid and gid reach runAsUser, runAsGroup, fsGroup and the chown target through --set and through a values file, and nothing else moves"

echo "### no chart value may be spliced into an initContainer shell program"
# Regression test for the review finding on PR #13. `postgres.user: "app;false"`
# is a legal PostgreSQL role name and legal URI userinfo; interpolated into
# `sh -c` it made the `until` condition literally `false`, so every application
# pod would sit in Init forever while Postgres was perfectly healthy. Rendering
# with a hostile value and asserting it does NOT appear in the program text is
# the only version of this check that cannot pass by inspection.
HOSTILE_RENDER=$(helm template octoconv "$CHART" $SECRETS_SET --set 'postgres.user=app;false' 2>&1)
HOSTILE_CMDS=$(echo "$HOSTILE_RENDER" | yq -e '. | select(.kind=="Deployment") | (.spec.template.spec.initContainers // [])[].command[]' 2>/dev/null)
if [ -z "$HOSTILE_CMDS" ]; then
  echo "FAIL: no initContainer commands parsed out of the hostile render -- this guard would pass vacuously"
  exit 1
fi
# `grep -c`, not `grep -q`: this script runs under `set -o pipefail`, and
# `grep -q` exits on its first match, which SIGPIPEs the `echo` feeding it and
# makes the whole pipeline exit 141. Under `if !` that reads as "no match" --
# so the -q form failed here on a render that contained eleven matches.
IN_PROGRAM=$(echo "$HOSTILE_CMDS" | grep -c 'app;false' || true)
if [ "$IN_PROGRAM" -gt 0 ]; then
  echo "FAIL: a chart value reached an initContainer shell program as text:"
  echo "$HOSTILE_CMDS" | grep -n 'app;false' | sed 's/^/        /'
  echo "      Pass it through env and reference it quoted (\"\$PGUSER\") instead."
  exit 1
fi
IN_ENV=$(echo "$HOSTILE_RENDER" | grep -c 'value: "app;false"' || true)
if [ "$IN_ENV" -eq 0 ]; then
  echo "FAIL: the hostile value did not reach the initContainer env either -- the"
  echo "      guard is testing a render that never used postgres.user at all"
  exit 1
fi
echo "OK: postgres.user reaches the wait initContainer as env, never as program text"

echo "### every mc invocation must use the transport s3.useSSL asks for"
# Review found the wait-for-bucket initContainer dialling http:// unconditionally
# while s3.useSSL -- an existing public value -- said otherwise. Against a
# TLS-only endpoint that probe can never succeed, so every application pod sits
# in Init forever. The createbucket Job had the same literal and would never
# create the bucket they are all waiting for, so fixing one and not the other
# would have left the deadlock intact.
#
# This asserts the rendered VALUE, not the shape: a chart that ignored the flag
# would still contain twelve well-formed `mc alias set` lines.
# internal/envcfg.TestChartSchemeMatchesParseBool covers the same property
# against ParseBool's full input table; this is the cheap render-level half that
# runs with the rest of the chart gate.
for WANT in http https; do
  case "$WANT" in
    http)  SSL=false ;;
    https) SSL=true ;;
  esac
  SCHEMES=$(helm template octoconv "$CHART" $SECRETS_SET --set "s3.useSSL=$SSL" 2>/dev/null \
    | grep -o 'mc alias set local "https\?://' | sort -u)
  COUNT=$(helm template octoconv "$CHART" $SECRETS_SET --set "s3.useSSL=$SSL" 2>/dev/null \
    | grep -c 'mc alias set local' || true)
  if [ "$COUNT" -eq 0 ]; then
    echo "FAIL: no 'mc alias set' lines rendered with s3.useSSL=$SSL -- this guard would pass vacuously"
    exit 1
  fi
  if [ "$SCHEMES" != "mc alias set local \"$WANT://" ]; then
    echo "FAIL: with s3.useSSL=$SSL every mc alias must use $WANT://, got:"
    echo "$SCHEMES" | sed 's/^/        /'
    exit 1
  fi
  echo "OK: s3.useSSL=$SSL renders $COUNT mc aliases, all $WANT://"
done

echo "### every container that runs mc uses minio.mcImage when set, minio.image when it is empty"
MINIO_IMAGE=$(yq '.minio.image' "$CHART/values.yaml")
MC_OVERRIDE="registry.example.invalid/mirror/mc@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
mc_rows() {
  yq 'select(.kind=="Deployment" or .kind=="Job") | .metadata.name as $d | .spec.template.spec | ((.initContainers // []) + .containers)[] | $d + " " + .name + " " + .image + " " + (((.command // []) + (.args // [])) | join(" ") | test("(^|[^a-z])mc (alias|ready|mb|ls)") | tostring)' "$1" | grep -v '^---$'
}
MC_PLAIN=$(mktemp)
MC_SET=$(mktemp)
helm template octoconv "$CHART" $SECRETS_SET > "$MC_PLAIN.render"
helm template octoconv "$CHART" $SECRETS_SET --set "minio.mcImage=$MC_OVERRIDE" > "$MC_SET.render"
mc_rows "$MC_PLAIN.render" > "$MC_PLAIN"
mc_rows "$MC_SET.render" > "$MC_SET"
for ROWS in "$MC_PLAIN" "$MC_SET"; do
  BLANK=$(awk 'NF != 4 { print }' "$ROWS")
  if [ -n "$BLANK" ]; then
    echo "FAIL: container rows without an image (or malformed) in a render:"
    echo "$BLANK" | sed 's/^/        /'
    exit 1
  fi
  BLIND=$(awk '($2 == "wait-for-minio" || $2 == "createbucket" || $2 == "wait-for-bucket") && $4 != "true" { print }' "$ROWS")
  if [ -n "$BLIND" ]; then
    echo "FAIL: the mc classifier missed containers that run mc:"
    echo "$BLIND" | sed 's/^/        /'
    exit 1
  fi
done
MC_COUNT=$(awk '$4 == "true"' "$MC_PLAIN" | wc -l | tr -d ' ')
if [ "$MC_COUNT" -eq 0 ]; then
  echo "FAIL: no container classified as running mc -- this guard would pass vacuously"
  exit 1
fi
MC_SET_NAMES=$(awk '$4 == "true" { print $1 " " $2 }' "$MC_SET" | sort)
MC_PLAIN_NAMES=$(awk '$4 == "true" { print $1 " " $2 }' "$MC_PLAIN" | sort)
if [ "$MC_PLAIN_NAMES" != "$MC_SET_NAMES" ]; then
  echo "FAIL: setting minio.mcImage changed WHICH containers run mc"
  diff <(echo "$MC_PLAIN_NAMES") <(echo "$MC_SET_NAMES") | sed 's/^/        /' || true
  exit 1
fi
WRONG=$(awk -v want="$MINIO_IMAGE" '$4 == "true" && $3 != want { print }' "$MC_PLAIN")
if [ -n "$WRONG" ]; then
  echo "FAIL: with minio.mcImage empty every mc container must run $MINIO_IMAGE, got:"
  echo "$WRONG" | sed 's/^/        /'
  exit 1
fi
WRONG=$(awk -v want="$MC_OVERRIDE" '$4 == "true" && $3 != want { print }' "$MC_SET")
if [ -n "$WRONG" ]; then
  echo "FAIL: with minio.mcImage set every mc container must run $MC_OVERRIDE, got:"
  echo "$WRONG" | sed 's/^/        /'
  exit 1
fi
LEAK=$(awk -v o="$MC_OVERRIDE" '$4 != "true" && $3 == o { print }' "$MC_SET")
if [ -n "$LEAK" ]; then
  echo "FAIL: minio.mcImage reached containers that do not run mc:"
  echo "$LEAK" | sed 's/^/        /'
  exit 1
fi
MINIO_ROWS=$(awk '$1 == "minio" && ($2 == "minio" || $2 == "migrate-data-ownership")' "$MC_SET")
if [ "$(echo "$MINIO_ROWS" | grep -c . || true)" -ne 2 ]; then
  echo "FAIL: expected the minio and migrate-data-ownership containers in the MinIO Deployment, got:"
  echo "$MINIO_ROWS" | sed 's/^/        /'
  exit 1
fi
MINIO_WRONG=$(echo "$MINIO_ROWS" | awk -v want="$MINIO_IMAGE" '$3 != want { print }')
if [ -n "$MINIO_WRONG" ]; then
  echo "FAIL: minio.mcImage must not reach the MinIO server pod, got:"
  echo "$MINIO_WRONG" | sed 's/^/        /'
  exit 1
fi
rm -f "$MC_PLAIN" "$MC_SET" "$MC_PLAIN.render" "$MC_SET.render"
echo "OK: $MC_COUNT containers run mc: all minio.image when mcImage is empty, all the override when it is set; the MinIO pod stays on minio.image"

echo "### no extraEnv may override S3_USE_SSL"
# The scheme is decided at RENDER time from .Values.s3.useSSL, while the
# application reads S3_USE_SSL from the ConfigMap at RUN time. Those are the
# same value today. A per-Deployment extraEnv entry for S3_USE_SSL would split
# them -- the app would dial one transport and its own initContainer would have
# probed the other -- which is the exact failure this whole guard exists to
# prevent, reintroduced through a values file instead of a template. Named here
# because the seam is invisible from either side alone.
# Only four Deployments render an extraEnv block today (api, document-worker,
# pandoc-worker, probe-worker). The vacuity check uses one of THOSE, and also
# asserts the set is still what it was -- if a fifth grows one, the check below
# already covers it, but this line makes the growth visible rather than silent.
EXTRAENV_TEMPLATES=$(grep -l 'extraEnv' "$CHART"/templates/deployment-*.yaml | wc -l | tr -d ' ')
if [ "$EXTRAENV_TEMPLATES" -eq 0 ]; then
  echo "FAIL: no Deployment template references extraEnv -- the vacuity probe below"
  echo "      cannot fire, so this guard would pass without testing anything"
  exit 1
fi
BAD_ENV=$(helm template octoconv "$CHART" $SECRETS_SET --set 'api.extraEnv.S3_USE_SSL=true' 2>/dev/null \
  | grep -c 'name: S3_USE_SSL' || true)
if [ "$BAD_ENV" -eq 0 ]; then
  echo "FAIL: setting api.extraEnv.S3_USE_SSL rendered nothing -- this guard cannot"
  echo "      detect the override it exists to forbid, so it would pass vacuously"
  exit 1
fi
LIVE_ENV=$(yq -e '. | select(.kind=="Deployment") | .metadata.name + " " + (([.spec.template.spec.containers[].env // [] | .[] | select(.name=="S3_USE_SSL")] | length) | tostring)' "$DEFAULT_RENDER" | grep -v '^---$' | awk '$2 > 0 { print $1 }')
if [ -n "$LIVE_ENV" ]; then
  echo "FAIL: these Deployments set S3_USE_SSL through extraEnv, which splits the"
  echo "      transport the app dials from the one its initContainer probed:"
  echo "$LIVE_ENV" | sed 's/^/        /'
  exit 1
fi
echo "OK: no Deployment overrides S3_USE_SSL through extraEnv ($EXTRAENV_TEMPLATES templates accept one)"

echo "### the octoconv chart must never be installed with --wait"
# The wait-for-bucket initContainer above depends on an install that does NOT
# block on readiness: the bucket is created by the octoconv-createbucket
# post-install hook, so `helm install --wait` would block on api becoming Ready,
# api would block on the bucket, and the hook that creates it would never run.
# Every install in scripts/ is already deliberately async and says so in its own
# log line -- this makes that a property of the repo rather than a convention,
# because the deadlock costs a five-minute timeout to discover.
# Comment/log/echo lines are stripped: several of these scripts announce
# "WITHOUT --wait" in their own progress output, and a guard that fires on the
# sentence describing the rule rather than on a violation of it is noise.
BADWAIT=$(grep -rn 'helm \(install\|upgrade\)[^|]*octoconv' scripts/ --include='*.sh' \
  | grep -Ev ':[0-9]+:[[:space:]]*(#|log |echo )' \
  | grep -- '--wait' || true)
if [ -n "$BADWAIT" ]; then
  echo "FAIL: an octoconv chart install uses --wait, which deadlocks against the"
  echo "      wait-for-bucket initContainer (the bucket is created by a post-install"
  echo "      hook that --wait would never reach):"
  echo "$BADWAIT" | sed 's/^/        /'
  exit 1
fi
echo "OK: no octoconv chart install uses --wait"

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

echo "### invalid minio.dataOwnership.uid/gid must abort the render"
OWN_BAD_OUT=$(mktemp)
own_must_fail() {
  set +e
  helm template octoconv "$CHART" $SECRETS_SET "$@" > "$OWN_BAD_OUT" 2>&1
  OWN_RC=$?
  set -e
}
for KEY in uid gid; do
  for bad in '-1' '1.5' '2.0' 'abc' 'true' '' 'null' '1000:0'; do
    own_must_fail --set "minio.dataOwnership.$KEY=$bad"
    if [ "$OWN_RC" -eq 0 ]; then
      echo "FAIL: --set minio.dataOwnership.$KEY=[${bad}] rendered successfully -- the value was accepted or silently coerced"
      exit 1
    fi
    if ! grep -q "minio.dataOwnership.$KEY" "$OWN_BAD_OUT"; then
      echo "FAIL: the error for minio.dataOwnership.$KEY=[${bad}] does not name the key:"
      sed 's/^/        /' "$OWN_BAD_OUT"
      exit 1
    fi
    if [ "$bad" = '-1' ]; then
      WANT_MSG='must not be negative'
    else
      WANT_MSG='must be a whole number'
    fi
    if ! grep -q "$WANT_MSG" "$OWN_BAD_OUT"; then
      echo "FAIL: the error for minio.dataOwnership.$KEY=[${bad}] lacks [$WANT_MSG]:"
      sed 's/^/        /' "$OWN_BAD_OUT"
      exit 1
    fi
    echo "OK: $KEY=[${bad}] rejected (rc=$OWN_RC)"
  done
done
for CASE in 'uid: 1.5|uid|must be a whole number' 'uid: "1000"|uid|must be a whole number' 'gid: -5|gid|must not be negative'; do
  LINE="${CASE%%|*}"
  REST="${CASE#*|}"
  KEY="${REST%%|*}"
  WANT_MSG="${REST#*|}"
  OWN_BAD_VALUES=$(mktemp)
  printf 'minio:\n  dataOwnership:\n    %s\n' "$LINE" > "$OWN_BAD_VALUES"
  own_must_fail -f "$OWN_BAD_VALUES"
  rm -f "$OWN_BAD_VALUES"
  if [ "$OWN_RC" -eq 0 ]; then
    echo "FAIL: a values file with [$LINE] rendered successfully"
    exit 1
  fi
  if ! grep -q "minio.dataOwnership.$KEY" "$OWN_BAD_OUT" || ! grep -q "$WANT_MSG" "$OWN_BAD_OUT"; then
    echo "FAIL: the error for a values file with [$LINE] does not name minio.dataOwnership.$KEY with [$WANT_MSG]:"
    sed 's/^/        /' "$OWN_BAD_OUT"
    exit 1
  fi
  echo "OK: values file [$LINE] rejected (rc=$OWN_RC)"
done
own_must_fail --set minio.dataOwnership=null
if [ "$OWN_RC" -eq 0 ]; then
  echo "FAIL: --set minio.dataOwnership=null rendered successfully"
  exit 1
fi
if ! grep -q 'minio.dataOwnership.uid' "$OWN_BAD_OUT"; then
  echo "FAIL: a missing minio.dataOwnership block did not produce the named error:"
  sed 's/^/        /' "$OWN_BAD_OUT"
  exit 1
fi
rm -f "$OWN_BAD_OUT"
echo "OK: a missing minio.dataOwnership block is rejected with the key named"
