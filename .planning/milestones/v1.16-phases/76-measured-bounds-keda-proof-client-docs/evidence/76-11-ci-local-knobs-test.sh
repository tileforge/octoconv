#!/usr/bin/env bash
set -uo pipefail

REPO=$(git -C "$(dirname "$0")" rev-parse --show-toplevel)
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT
STUB="$T/bin"
mkdir -p "$STUB"

cat > "$STUB/docker" <<'EOF'
#!/usr/bin/env bash
echo "docker $*" >> "$STUB_LOG"
case "$1" in
  info) echo "stub (stub)" ;;
esac
exit 0
EOF

cat > "$STUB/lsof" <<'EOF'
#!/usr/bin/env bash
port=""
for a in "$@"; do
  case "$a" in -iTCP:*) port=${a#-iTCP:} ;; esac
done
echo "lsof $port" >> "$STUB_LOG"
for p in ${STUB_TAKEN_PORTS:-}; do
  if [ "$p" = "$port" ]; then
    echo "COMMAND PID USER"
    echo "OrbStack 1 stub TCP *:$port (LISTEN)"
    exit 0
  fi
done
exit 1
EOF

cat > "$STUB/curl" <<'EOF'
#!/usr/bin/env bash
echo "curl $*" >> "$STUB_LOG"
exit 0
EOF

cat > "$STUB/go" <<'EOF'
#!/usr/bin/env bash
case "$1" in
  version) echo "go version go1.26.4 stub/stub"; exit 0 ;;
  run) echo "go $*" >> "$STUB_LOG"; exit 0 ;;
  test) echo "go $* E2E_BASE_URL=${E2E_BASE_URL:-unset}" >> "$STUB_LOG"; exit "${STUB_GO_TEST_RC:-0}" ;;
esac
exit 0
EOF

cat > "$STUB/helm" <<'EOF'
#!/usr/bin/env bash
echo "v0.0.0"
EOF
chmod +x "$STUB"/*

PASSES=0
FAILS=0
check() {
  local name=$1; shift
  if "$@"; then
    PASSES=$((PASSES + 1)); echo "PASS: $name"
  else
    FAILS=$((FAILS + 1)); echo "FAIL: $name"
  fi
}
has() { grep -qF -- "$2" "$1"; }
hasnt() { ! grep -qF -- "$2" "$1"; }
hasx() { grep -qxF -- "$2" "$1"; }

run_case() {
  local name=$1; shift
  local dir="$T/$name"
  mkdir -p "$dir/ev"
  : > "$dir/log"
  ( cd "$REPO" && env -u CI_LOCAL_E2E_API_PORT -u CI_LOCAL_E2E_EXTRA_COMPOSE_FILES -u CI_LOCAL_E2E_TEARDOWN -u CI_LOCAL_E2E_GO_TEST_FLAGS \
      PATH="$STUB:$PATH" STUB_LOG="$dir/log" CI_LOCAL_EVIDENCE_DIR="$dir/ev" "$@" bash scripts/ci-local.sh e2e > "$dir/out" 2>&1 )
  echo $? > "$dir/rc"
}

BASE="docker compose -f docker-compose.yml -f docker-compose.e2e.yml"

run_case default
D="$T/default"
check "default: rc 0" test "$(cat "$D/rc")" = 0
check "default: lsof checks 8090" hasx "$D/log" "lsof 8090"
check "default: lsof checks 9100" hasx "$D/log" "lsof 9100"
check "default: up uses only the two files" hasx "$D/log" "$BASE up -d --build"
check "default: teardown is down -v" hasx "$D/log" "$BASE down -v"
check "default: no stop teardown" hasnt "$D/log" "$BASE stop"
check "default: healthz on 8090" hasx "$D/log" "curl -fsS --retry 30 --retry-all-errors --retry-delay 2 http://localhost:8090/healthz"
check "default: go test flags unchanged" hasx "$D/log" "go test -count=1 ./internal/e2e/... -timeout 30m E2E_BASE_URL=http://localhost:8090"
check "default: settings line" has "$D/out" "e2e settings: api_port=8090 extra_compose_files=none teardown=down-v go_test_flags=none"

run_case taken8090 STUB_TAKEN_PORTS=8090
D="$T/taken8090"
check "taken 8090: rc non-zero" test "$(cat "$D/rc")" != 0
check "taken 8090: refusal names :8090" has "$D/out" "already listening on :8090"
check "taken 8090: no compose up" hasnt "$D/log" "up -d --build"
check "taken 8090: no teardown" hasnt "$D/log" "down -v"

run_case knobs CI_LOCAL_E2E_API_PORT=18090 CI_LOCAL_E2E_EXTRA_COMPOSE_FILES="/x/a.yml /y/b.yml" CI_LOCAL_E2E_TEARDOWN=stop CI_LOCAL_E2E_GO_TEST_FLAGS="-v -run TestMultiFile" STUB_TAKEN_PORTS=8090
D="$T/knobs"
K="$BASE -f /x/a.yml -f /y/b.yml"
check "knobs: rc 0 with 8090 taken" test "$(cat "$D/rc")" = 0
check "knobs: lsof checks 18090" hasx "$D/log" "lsof 18090"
check "knobs: lsof skips 8090" hasnt "$D/log" "lsof 8090"
check "knobs: lsof checks 9100" hasx "$D/log" "lsof 9100"
check "knobs: up carries the extra files" hasx "$D/log" "$K up -d --build"
check "knobs: teardown is stop with the extra files" hasx "$D/log" "$K stop"
check "knobs: no down -v anywhere" hasnt "$D/log" "down -v"
check "knobs: healthz on 18090" hasx "$D/log" "curl -fsS --retry 30 --retry-all-errors --retry-delay 2 http://localhost:18090/healthz"
check "knobs: go test gets the extra flags" hasx "$D/log" "go test -count=1 ./internal/e2e/... -timeout 30m -v -run TestMultiFile E2E_BASE_URL=http://localhost:18090"
check "knobs: settings line" has "$D/out" "e2e settings: api_port=18090 extra_compose_files=/x/a.yml /y/b.yml teardown=stop go_test_flags=-v -run TestMultiFile"

run_case knobsfail CI_LOCAL_E2E_API_PORT=18090 CI_LOCAL_E2E_EXTRA_COMPOSE_FILES="/x/a.yml" CI_LOCAL_E2E_TEARDOWN=stop STUB_GO_TEST_RC=1
D="$T/knobsfail"
check "knobs failing test: rc non-zero" test "$(cat "$D/rc")" != 0
check "knobs failing test: logs carry the extra file" hasx "$D/log" "$BASE -f /x/a.yml logs --no-color"
check "knobs failing test: still stop teardown" hasx "$D/log" "$BASE -f /x/a.yml stop"
check "knobs failing test: no down -v" hasnt "$D/log" "down -v"

run_case badteardown CI_LOCAL_E2E_TEARDOWN=down
D="$T/badteardown"
check "bad teardown: rc non-zero" test "$(cat "$D/rc")" != 0
check "bad teardown: names the value" has "$D/out" "CI_LOCAL_E2E_TEARDOWN=down"
check "bad teardown: no compose up" hasnt "$D/log" "up -d --build"
check "bad teardown: no teardown ran" hasnt "$D/log" "down -v"

run_case badport CI_LOCAL_E2E_API_PORT=80x
D="$T/badport"
check "bad port: rc non-zero" test "$(cat "$D/rc")" != 0
check "bad port: no compose up" hasnt "$D/log" "up -d --build"

run_case taken18090 CI_LOCAL_E2E_API_PORT=18090 CI_LOCAL_E2E_TEARDOWN=stop STUB_TAKEN_PORTS=18090
D="$T/taken18090"
check "taken 18090: rc non-zero" test "$(cat "$D/rc")" != 0
check "taken 18090: refusal names :18090" has "$D/out" "already listening on :18090"
check "taken 18090: no compose up" hasnt "$D/log" "up -d --build"

echo "RESULT: ${PASSES} passed, ${FAILS} failed"
[ "$FAILS" -eq 0 ]
