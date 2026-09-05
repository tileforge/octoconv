#!/usr/bin/env bash
# Local stand-in for .github/workflows/ci.yml, tier by tier, with recorded
# evidence.
#
# WHY THIS EXISTS: GitHub Actions has not run for this repository since
# 2026-08-16 -- every job fails in ~2s with "The job was not started because
# recent account payments have failed or your spending limit needs to be
# increased", and billing is restored at the end of August 2026. Until then the
# only evidence behind any commit is what was run locally, and "I ran the tests"
# is not evidence: this script runs the SAME commands CI runs, in the same
# order, and writes a transcript naming the commit and the UTC run id.
#
# WHAT IT DOES NOT DO, on purpose:
#   - It does not replace CI. When Actions is restored, push and let it run;
#     a green transcript here is a substitute under duress, not a ratification.
#   - It skips CI's GitHub-only mechanics (GHA layer caches, artifact upload,
#     runner disk cleanup), which are infrastructure, not assertions.
#   - It runs on this machine's toolchain (darwin/arm64), not ubuntu-latest.
#     Anything platform-sensitive -- and the docker images ARE, being built for
#     the host architecture here -- is weaker evidence than CI would be. That
#     limitation is printed in the transcript rather than left for a reader to
#     infer.
#
# The helm tier calls scripts/ci-helm-checks.sh, which the workflow also calls:
# one copy of those assertions, two callers.
#
# Usage:
#   scripts/ci-local.sh                # every tier
#   scripts/ci-local.sh gate race      # only the named tiers
#
# Tiers: helm gate race docker-build e2e

set -uo pipefail

cd "$(dirname "$0")/.."
REPO_ROOT="$(pwd)"

RUN_ID="$(date -u +%Y%m%dT%H%M%SZ)"
COMMIT="$(git rev-parse --short HEAD)"
BRANCH="$(git rev-parse --abbrev-ref HEAD)"
# Only TRACKED modifications weaken the claim "this transcript describes that
# commit"; untracked scratch files do not. Both are reported, separately, so a
# reader can judge rather than take a one-word label on trust.
TRACKED_DIRT="$(git status --porcelain --untracked-files=no 2>/dev/null)"
UNTRACKED_DIRT="$(git ls-files --others --exclude-standard 2>/dev/null)"
DIRTY=""
[ -n "$TRACKED_DIRT" ] && DIRTY=" (TRACKED CHANGES NOT IN THIS COMMIT)"

EVIDENCE_DIR="${CI_LOCAL_EVIDENCE_DIR:-.planning/ci-local}"
mkdir -p "$EVIDENCE_DIR"
TRANSCRIPT="$EVIDENCE_DIR/ci-local-$RUN_ID.log"

TIERS=("$@")
if [ ${#TIERS[@]} -eq 0 ]; then
  TIERS=(helm gate race docker-build e2e)
fi

exec > >(tee "$TRANSCRIPT") 2>&1

echo "=== ci-local $RUN_ID ==="
echo "commit:   $COMMIT$DIRTY"
echo "branch:   $BRANCH"
echo "host:     $(uname -sm)"
echo "go:       $(go version 2>/dev/null | awk '{print $3, $4}')"
echo "helm:     $(helm version --short 2>/dev/null)"
echo "docker:   $(docker info --format '{{.ServerVersion}} ({{.Name}})' 2>/dev/null)"
echo "tiers:    ${TIERS[*]}"
if [ -n "$TRACKED_DIRT" ]; then
  echo "WARNING:  tracked files differ from $COMMIT -- this transcript does NOT"
  echo "          describe that commit as pushed:"
  echo "$TRACKED_DIRT" | sed 's/^/            /'
fi
if [ -n "$UNTRACKED_DIRT" ]; then
  echo "untracked (present but not part of the build):"
  echo "$UNTRACKED_DIRT" | sed 's/^/            /'
fi
# The workflow PINS helm (its own comment explains why: an unpinned action
# installs whatever "latest" is that day, and chart render behavior can change
# with no commit to this repo). A local run on a different version is therefore
# evidence about a different renderer -- say so rather than let a green helm
# tier imply CI would agree.
PINNED_HELM="$(sed -n 's/^ *version: \(v[0-9][^ ]*\)$/\1/p' .github/workflows/ci.yml | head -1)"
LOCAL_HELM="$(helm version --short 2>/dev/null | sed 's/+.*//')"
if [ -n "$PINNED_HELM" ] && [ -n "$LOCAL_HELM" ] && [ "$PINNED_HELM" != "$LOCAL_HELM" ]; then
  echo "WARNING:  helm $LOCAL_HELM locally vs $PINNED_HELM pinned in ci.yml --"
  echo "          the helm tier below proves the chart against a DIFFERENT"
  echo "          renderer than CI uses. Install the pinned version to close"
  echo "          this gap, or read a green helm tier accordingly."
fi
echo "note:     stand-in for GitHub Actions, unavailable until billing is"
echo "          restored (end of August 2026). Host-arch images; see this"
echo "          script's own header for what this evidence does NOT cover."
echo

declare -a RESULTS=()
OVERALL=0

run_tier() {
  local name="$1"; shift
  local started
  started=$(date -u +%H:%M:%S)
  echo "--------------------------------------------------------------"
  echo "### tier: $name (started $started UTC)"
  echo "--------------------------------------------------------------"
  if "$@"; then
    echo "### tier $name: PASS"
    RESULTS+=("PASS  $name")
  else
    local rc=$?
    echo "### tier $name: FAIL (rc=$rc)"
    RESULTS+=("FAIL  $name (rc=$rc)")
    OVERALL=1
  fi
  echo
}

tier_helm() {
  # Identical to the workflow's helm job, which calls the same script.
  ./scripts/ci-helm-checks.sh
}

tier_gate() {
  # Mirrors the gate job's four steps exactly, in order.
  local fmt
  fmt="$(gofmt -l .)"
  if [ -n "$fmt" ]; then
    echo "gofmt -l . reported:"
    echo "$fmt"
    return 1
  fi
  echo "gofmt -l .: clean"
  go vet ./... || return 1
  echo "go vet ./...: clean"
  go build ./... || return 1
  echo "go build ./...: clean"
  go test ./... -timeout 5m || return 1
}

tier_race() {
  go test -race ./... -timeout 10m
}

tier_docker_build() {
  # CI bakes with GHA layer caches and load:false (verification only). Locally
  # the caches do not exist; the assertion that matters -- every compose bake
  # target builds -- is the same.
  docker buildx bake -f docker-compose.yml
}

# The compose invocation is a FILE-scope array, not a function-local one: the
# teardown below runs from a RETURN trap, which fires after the function's
# locals are gone. It did, and the transcript's last line was
# "compose[@]: unbound variable" -- a teardown that fails is a stack left
# running, and the message lands where a reader is already looking for the
# cause of the tier's failure.
COMPOSE_E2E=(docker compose -f docker-compose.yml -f docker-compose.e2e.yml)

tier_e2e() {
  local compose=("${COMPOSE_E2E[@]}")
  local rc=0

  # Nothing else may hold the ports this tier tests THROUGH. A developer API
  # left running from a debugging session answers /healthz exactly like the
  # compose one, and the suite then seeds a client into compose's database and
  # authenticates against a process with a different API_KEY_SALT -- which
  # arrives as "401 invalid api key" on every test, a message that says nothing
  # about the actual cause. Observed on 2026-09-05.
  for port in 8090 9100; do
    if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
      echo "error: something is already listening on :$port -- this tier would test that" >&2
      echo "       process instead of the compose stack. Stop it and re-run." >&2
      lsof -nP -iTCP:"$port" -sTCP:LISTEN | head -3 >&2
      return 1
    fi
  done

  echo "--- bringing up the compose stack (with --build: no warmed cache locally) ---"
  "${compose[@]}" up -d --build || return 1

  # shellcheck disable=SC2317  # invoked via trap
  teardown() {
    echo "--- tearing down ---"
    "${COMPOSE_E2E[@]}" down -v >/dev/null 2>&1 || true
  }
  trap teardown RETURN

  echo "--- waiting for API readiness ---"
  if ! curl -fsS --retry 30 --retry-all-errors --retry-delay 2 http://localhost:8090/healthz; then
    echo "API never became ready; dumping compose logs"
    "${compose[@]}" logs --no-color | tail -100
    return 1
  fi
  echo

  echo "--- running migrations ---"
  DATABASE_URL='postgres://octo:octo-pass@localhost:5434/octo_db' go run ./cmd/migrate || return 1

  echo "--- running the E2E suite ---"
  E2E_BASE_URL=http://localhost:8090 \
  DATABASE_URL='postgres://octo:octo-pass@localhost:5434/octo_db' \
  API_KEY_SALT=dev-only-change-me-in-real-deploys \
  WEBHOOK_SIGNING_SECRET=dev-only-change-me-in-real-deploys \
  E2E_S3_DIAL_ADDR=127.0.0.1:9100 \
    go test ./internal/e2e/... -timeout 30m || rc=1

  if [ "$rc" -ne 0 ]; then
    echo "--- compose logs (tail) ---"
    "${compose[@]}" logs --no-color | tail -200
  fi
  return "$rc"
}

for tier in "${TIERS[@]}"; do
  case "$tier" in
    helm)         run_tier helm tier_helm ;;
    gate)         run_tier gate tier_gate ;;
    race)         run_tier race tier_race ;;
    docker-build) run_tier docker-build tier_docker_build ;;
    e2e)          run_tier e2e tier_e2e ;;
    *)
      echo "unknown tier: $tier (known: helm gate race docker-build e2e)"
      exit 2
      ;;
  esac
done

echo "=============================================================="
echo "ci-local $RUN_ID summary   commit $COMMIT$DIRTY"
for r in "${RESULTS[@]}"; do echo "  $r"; done
if [ "$OVERALL" -eq 0 ]; then
  echo "  ALL REQUESTED TIERS PASSED"
else
  echo "  AT LEAST ONE TIER FAILED"
fi
echo "transcript: $REPO_ROOT/$TRANSCRIPT"
echo "=============================================================="
exit "$OVERALL"
