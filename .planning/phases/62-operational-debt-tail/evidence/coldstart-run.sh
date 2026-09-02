#!/usr/bin/env bash
# Cold-install observation. $1 = chart dir, $2 = label.
set -uo pipefail
CHART="$1"; LABEL="$2"
SP="$(dirname "$0")"
NS=octoconv
echo "=== $LABEL ==="
kubectl delete ns "$NS" --wait=true --timeout=180s >/dev/null 2>&1
kubectl create ns "$NS" >/dev/null
START=$(date +%s)
helm install octoconv "$CHART" -n "$NS" \
  -f deploy/chart/octoconv/values-local.yaml -f "$SP/values-coldstart.yaml" >/dev/null
echo "helm install returned at t+$(( $(date +%s) - START ))s"
DEADLINE=$(( START + 240 ))
while [ "$(date +%s)" -lt "$DEADLINE" ]; do
  READY=$(kubectl get deploy api -n "$NS" -o jsonpath='{.status.readyReplicas}' 2>/dev/null)
  [ "${READY:-0}" = "1" ] && break
  sleep 2
done
ELAPSED=$(( $(date +%s) - START ))
echo "api ready after ${ELAPSED}s (readyReplicas=${READY:-0})"
echo "--- pods ---"
kubectl get pods -n "$NS" -o custom-columns='NAME:.metadata.name,READY:.status.containerStatuses[*].ready,RESTARTS:.status.containerStatuses[*].restartCount,STATUS:.status.phase'
echo "--- api restartCount ---"
POD=$(kubectl get pods -n "$NS" -l app.kubernetes.io/component=api -o jsonpath='{.items[0].metadata.name}')
echo "pod=$POD"
kubectl get pod "$POD" -n "$NS" -o jsonpath='{range .status.containerStatuses[*]}{.name}{" restartCount="}{.restartCount}{"\n"}{end}'
echo "--- api container waiting/terminated reasons ---"
kubectl get pod "$POD" -n "$NS" -o jsonpath='{range .status.containerStatuses[*]}{.name}{" lastState="}{.lastState}{"\n"}{end}'
echo "--- events (BackOff/Failed/Unhealthy) ---"
kubectl get events -n "$NS" --sort-by=.lastTimestamp 2>/dev/null | grep -Ei 'backoff|failed|unhealthy|crash' | tail -15
echo "=== end $LABEL ==="
