#!/usr/bin/env bash
set -euo pipefail

MODE=${1:?usage: reuse-values-probe.sh pre|post}
case "$MODE" in pre|post) ;; *) echo "mode must be pre or post" >&2; exit 2 ;; esac

PREV_REV=caf0718d464086321c4123ff221a9c2d53829f7c
HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(git rev-parse --show-toplevel)
cd "$ROOT"
CHART=deploy/chart/octoconv

WORK=$(mktemp -d)
STUB_PID=""
cleanup() {
  if [ -n "$STUB_PID" ]; then kill "$STUB_PID" 2>/dev/null || true; wait "$STUB_PID" 2>/dev/null || true; fi
  rm -rf "${WORK:?}"
}
trap cleanup EXIT

echo "helm: $(helm version --short)  yq: $(yq --version)  python3: $(python3 --version)  HEAD: $(git rev-parse --short HEAD)  mode: $MODE"

mkdir "$WORK/prev"
git archive -o "$WORK/prev.tar" "$PREV_REV" deploy/chart/octoconv
tar -x -f "$WORK/prev.tar" -C "$WORK/prev"
PREV_CHART="$WORK/prev/deploy/chart/octoconv"

PORT=""
for P in 18471 18472 18473 18474 18475; do
  if [ -z "$(lsof -nP -iTCP:$P -sTCP:LISTEN 2>/dev/null || true)" ]; then PORT=$P; break; fi
done
[ -n "$PORT" ] || { echo "RESULT: FAIL: no free port"; exit 1; }
python3 "$HERE/fake-kube-discovery.py" "$PORT" 2> "$WORK/stub.log" &
STUB_PID=$!
READY=0
for _ in $(seq 1 25); do
  if curl -s "http://127.0.0.1:$PORT/version" > /dev/null 2>&1; then READY=1; break; fi
  sleep 0.2
done
[ "$READY" = 1 ] || { echo "RESULT: FAIL: discovery stub did not come up on $PORT"; exit 1; }
echo "stub on 127.0.0.1:$PORT"

cat > "$WORK/kubeconfig" <<KC
apiVersion: v1
kind: Config
clusters:
- name: stub
  cluster:
    server: http://127.0.0.1:$PORT
contexts:
- name: stub
  context:
    cluster: stub
    user: stub
current-context: stub
users:
- name: stub
  user: {}
KC
export KUBECONFIG="$WORK/kubeconfig"
export HELM_DRIVER=memory

: > "$WORK/empty.yaml"
HELM_MEMORY_DRIVER_DATA="$WORK/empty.yaml" helm install octoconv "$PREV_CHART" -n octoconv -f "$PREV_CHART/values-local.yaml" --dry-run=client -o yaml > "$WORK/rel.yaml" 2> "$WORK/install.err" || { cat "$WORK/install.err"; echo "RESULT: FAIL: seeding the previous release failed"; exit 1; }
yq '[. | .info.status = "deployed"]' "$WORK/rel.yaml" > "$WORK/rels.yaml"
export HELM_MEMORY_DRIVER_DATA="$WORK/rels.yaml"

run_upgrade() {
  local flag=$1 out=$2 err=$3
  set +e
  helm upgrade octoconv "$ROOT/$CHART" -n octoconv "$flag" --dry-run=client -o yaml > "$out" 2> "$err"
  UP_RC=$?
  set -e
}

run_upgrade --reset-then-reuse-values "$WORK/reset.yaml" "$WORK/reset.err"
RESET_RC=$UP_RC
echo "control --reset-then-reuse-values: rc=$RESET_RC"
run_upgrade --reuse-values "$WORK/reuse.yaml" "$WORK/reuse.err"
REUSE_RC=$UP_RC
echo "--reuse-values: rc=$REUSE_RC"
echo "--reuse-values stderr (without level=INFO):"
{ grep -v 'level=INFO' "$WORK/reuse.err" || true; } | sed 's/^/    /'

if [ "$RESET_RC" -ne 0 ]; then
  { grep -v 'level=INFO' "$WORK/reset.err" || true; } | sed 's/^/    /'
  echo "RESULT: FAIL: control --reset-then-reuse-values rc=$RESET_RC (probe harness problem)"
  exit 1
fi

if [ "$MODE" = pre ]; then
  if [ "$REUSE_RC" -ne 0 ] && grep -q 'minio.dataOwnership.uid must be a whole number, got <nil>' "$WORK/reuse.err"; then
    echo "RESULT: PASS"
  else
    echo "RESULT: FAIL: expected the Codex error from --reuse-values (rc=$REUSE_RC)"
  fi
  exit 0
fi

fail() { echo "RESULT: FAIL: $1"; exit 1; }
[ "$REUSE_RC" -eq 0 ] || fail "--reuse-values rc=$REUSE_RC"

HAS=$(yq '.chart.values.minio | has("dataOwnership")' "$WORK/reuse.yaml")
echo ".chart.values.minio has dataOwnership: $HAS"
echo ".chart.values.minio keys: $(yq '.chart.values.minio | keys | join(",")' "$WORK/reuse.yaml")"
[ "$HAS" = false ] || fail "reuse path kept a dataOwnership block (has=$HAS), the probe did not exercise the absent-block path"

mkdir "$WORK/sim"
cp -R "$ROOT/$CHART" "$WORK/sim/octoconv"
cp "$PREV_CHART/values.yaml" "$WORK/sim/octoconv/values.yaml"
helm template octoconv "$WORK/sim/octoconv" -n octoconv -f "$ROOT/$CHART/values-local.yaml" > "$WORK/sim.yaml"
yq 'select(.metadata.annotations."helm.sh/hook" == null)' "$WORK/sim.yaml" | grep -v '^---$' > "$WORK/sim-nonhook.yaml" || true
yq '.manifest' "$WORK/reuse.yaml" | yq '.' | grep -v '^---$' > "$WORK/real-manifest.yaml" || true
[ -s "$WORK/real-manifest.yaml" ] || fail "empty manifest from the real upgrade"
[ -s "$WORK/sim-nonhook.yaml" ] || fail "empty simulated manifest"
if cmp -s "$WORK/real-manifest.yaml" "$WORK/sim-nonhook.yaml"; then
  echo "manifest: real helm upgrade --reuse-values == simulation (byte-identical, $(wc -l < "$WORK/real-manifest.yaml" | tr -d ' ') lines)"
else
  diff "$WORK/real-manifest.yaml" "$WORK/sim-nonhook.yaml" | head -40
  fail "real manifest differs from the simulation"
fi

yq '.hooks[].manifest' "$WORK/reuse.yaml" | yq '.. | select(has("image")) | .image' | grep -v '^---$' | sort > "$WORK/real-hook-images.txt" || true
yq 'select(.metadata.annotations."helm.sh/hook" != null) | .. | select(has("image")) | .image' "$WORK/sim.yaml" | grep -v '^---$' | sort > "$WORK/sim-hook-images.txt" || true
cmp -s "$WORK/real-hook-images.txt" "$WORK/sim-hook-images.txt" || { diff "$WORK/real-hook-images.txt" "$WORK/sim-hook-images.txt"; fail "hook container images differ from the simulation"; }
echo "hook images equal ($(wc -l < "$WORK/real-hook-images.txt" | tr -d ' ') images)"

INITS=$(yq '.manifest' "$WORK/reuse.yaml" | yq 'select(.kind=="Deployment" and .metadata.name=="minio") | .spec.template.spec.initContainers[].name' | grep -v '^---$' || true)
[ "$INITS" = "migrate-data-ownership" ] || fail "minio init containers are [$INITS]"
WAITS=$(yq '.manifest' "$WORK/reuse.yaml" | grep -c 'name: wait-for-bucket' || true)
[ "$WAITS" -gt 0 ] || fail "no wait-for-bucket in the manifest"

echo "--- reuse-values pod, for the PR note ---"
yq '.manifest' "$WORK/reuse.yaml" | yq 'select(.kind=="Deployment" and .metadata.name=="minio") | "server image: " + (.spec.template.spec.containers[] | select(.name=="minio") | .image)' | grep -v '^---$'
yq '.manifest' "$WORK/reuse.yaml" | yq 'select(.kind=="Deployment" and .metadata.name=="minio") | "init image: " + .spec.template.spec.initContainers[0].image' | grep -v '^---$'
yq '.manifest' "$WORK/reuse.yaml" | yq 'select(.kind=="Deployment" and .metadata.name=="minio") | "pod runAsUser: " + (.spec.template.spec.securityContext.runAsUser | tostring)' | grep -v '^---$'
yq '.manifest' "$WORK/reuse.yaml" | yq 'select(.kind=="Deployment" and .metadata.name=="minio") | "chown owner: " + (.spec.template.spec.initContainers[0].command[-2])' | grep -v '^---$'
yq '.manifest' "$WORK/reuse.yaml" | yq '.. | select(has("name") and .name == "wait-for-bucket") | "wait-for-bucket image: " + .image' | grep -v '^---$' | sort -u
echo "--- reset-then-reuse-values pod ---"
yq '.manifest' "$WORK/reset.yaml" | yq 'select(.kind=="Deployment" and .metadata.name=="minio") | "server image: " + (.spec.template.spec.containers[] | select(.name=="minio") | .image)' | grep -v '^---$'
echo "RESULT: PASS"
