#!/usr/bin/env bash
# The per-class live gate: proves a FRESHLY BUILT, uniquely tagged worker
# image actually confines its engine -- not by reading the code, but by
# running the built image and judging its actual startup output.
#
# Usage:
#   scripts/sandbox-liveproof.sh <class>...
#
# Every class this script knows about is in the table below from the start
# (image, pandoc, ocr, av, audio), including classes not yet confined by any
# converter -- so the classes that land later (plans 03-05) extend the
# CONVERTER, never this script.
#
# A UNIQUE tag per run, not :latest: a shared tag can be overwritten by a
# concurrent build, and the object under test has to be the one this run
# actually produced, not whatever happened to be tagged last.
set -euo pipefail

die() { echo "error: $*" >&2; exit 1; }

# class_row prints "dockerfile binary envvar expects_proven" for class, or
# dies on an unknown one.
class_row() {
  case "$1" in
    image)  echo "Dockerfile.worker worker IMAGE_SANDBOX_DISABLED no" ;;
    pandoc) echo "Dockerfile.pandoc-worker pandoc-worker PANDOC_SANDBOX_DISABLED yes" ;;
    ocr)    echo "Dockerfile.ocr-worker ocr-worker OCR_SANDBOX_DISABLED yes" ;;
    av)     echo "Dockerfile.av-worker av-worker AV_SANDBOX_DISABLED yes" ;;
    audio)  echo "Dockerfile.audio-worker audio-worker AUDIO_SANDBOX_DISABLED yes" ;;
    *) die "unknown class: $1 (expected image, pandoc, ocr, av or audio)" ;;
  esac
}

[ $# -gt 0 ] || die "usage: $0 <class>..."

RUN_ID=$(date +%s)-$$
FAILED=0
CHECK_COUNT=0

pass() { CHECK_COUNT=$((CHECK_COUNT + 1)); echo "PASS: $1"; }
fail() {
  CHECK_COUNT=$((CHECK_COUNT + 1))
  FAILED=1
  local what=$1; shift
  echo "FAIL: $what"
  for line in "$@"; do echo "  $line"; done
}

# check_confined_startup runs class's freshly built tag with an unreachable
# DATABASE_URL (fails fast, right after the preflight) and judges the
# combined output. PASS iff it (1) logged the landlock startup line naming
# the exact helper argv, (2) logged "sandbox proven" when this class expects
# one, (3) reached the postgres connect attempt at all (proving the preflight
# cleared), (4) never logged a preflight failure, and (5) exited non-zero.
check_confined_startup() {
  local class=$1 bin=$2 expects_proven=$3 tag=$4
  local cname="octoconv-liveproof-${class}-confined-${RUN_ID}"

  # macOS has no timeout(1); a background watchdog kills the container if the
  # run somehow hangs instead of failing fast against the bad DATABASE_URL.
  # Named per run so the watchdog can never kill a sibling class's container.
  ( sleep 300; docker kill "$cname" >/dev/null 2>&1 || true ) </dev/null >/dev/null 2>&1 &
  local watchdog=$!

  set +e
  local out status
  out=$(docker run --name "$cname" --rm \
    -e DATABASE_URL=postgres://gate:gate@127.0.0.1:1/gate \
    "$tag" 2>&1)
  status=$?
  set -e
  kill "$watchdog" >/dev/null 2>&1 || true
  wait "$watchdog" 2>/dev/null || true

  local landlock_line proven_line postgres_line
  landlock_line=$(printf '%s\n' "$out" | grep -F "🔒 ${class} sandbox: landlock via /usr/local/bin/${bin} __sandbox-exec" || true)
  proven_line=$(printf '%s\n' "$out" | grep -F "🔒 ${class} sandbox proven:" || true)
  postgres_line=$(printf '%s\n' "$out" | grep -F "postgres:" | head -1 || true)

  local ok=1
  local reasons=()
  if [ -z "$landlock_line" ]; then
    ok=0
    reasons+=("missing the landlock startup line naming /usr/local/bin/${bin} __sandbox-exec")
  fi
  if [ "$expects_proven" = yes ] && [ -z "$proven_line" ]; then
    ok=0
    reasons+=("missing the '🔒 ${class} sandbox proven:' line")
  fi
  if [ -z "$postgres_line" ]; then
    ok=0
    reasons+=("never reached the postgres connect attempt -- the preflight did not clear")
  fi
  if printf '%s\n' "$out" | grep -qF "${class} sandbox preflight:"; then
    ok=0
    reasons+=("the preflight itself FAILED ('${class} sandbox preflight:' is in the output)")
  fi
  if [ "$status" -eq 0 ]; then
    ok=0
    reasons+=("exit status 0, want non-zero (postgres at 127.0.0.1:1 must be unreachable)")
  fi

  if [ "$ok" = 1 ]; then
    pass "${class} confined-startup"
    [ -n "$landlock_line" ] && echo "  $landlock_line"
    [ -n "$proven_line" ] && echo "  $proven_line"
    [ -n "$postgres_line" ] && echo "  $postgres_line"
  else
    fail "${class} confined-startup" "${reasons[@]}" "--- full output ---" "$out"
  fi
}

# write_seccomp_profile writes a minimal docker seccomp profile to out:
# everything is SCMP_ACT_ALLOW except the named syscalls, which return errno.
# This is the only way a normal (non-privileged) docker run can simulate "this
# kernel does not have Landlock" or "this policy allows the ABI query and
# denies restrict_self" -- OrbStack's host genuinely has Landlock, so the only
# way to prove the refusal/preflight paths is to take a syscall away from the
# container, not from the kernel.
write_seccomp_profile() {
  local out=$1 errno=$2
  shift 2
  local names_json="" n
  for n in "$@"; do
    [ -n "$names_json" ] && names_json="${names_json},"
    names_json="${names_json}\"${n}\""
  done
  cat >"$out" <<JSON
{
  "defaultAction": "SCMP_ACT_ALLOW",
  "syscalls": [
    {"names": [${names_json}], "action": "SCMP_ACT_ERRNO", "errnoRet": ${errno}}
  ]
}
JSON
}

# run_gated runs tag under a seccomp profile (or none, when profile is "")
# with the given extra docker run args, against the same unreachable
# DATABASE_URL every check here uses, and echoes "<status>\x1e<output>" --
# 0x1e (RS) as the separator because engine/helper stderr can legitimately
# contain any other byte.
run_gated() {
  local class=$1 suffix=$2 profile=$3 tag=$4
  shift 4
  local cname="octoconv-liveproof-${class}-${suffix}-${RUN_ID}"
  local secopt=()
  [ -n "$profile" ] && secopt=(--security-opt "seccomp=${profile}")

  # Redirected to /dev/null explicitly, and not merely relying on the inner
  # `docker kill` command's own redirection: this whole function's stdout is
  # itself captured by the CALLER via `result=$(run_gated ...)`, and a
  # background job forked while that capture is active inherits its pipe as
  # its own stdout unless told otherwise. Bash's command substitution does
  # not see EOF until every process holding that pipe's write end has
  # exited -- so an unredirected watchdog would hold the caller's `$(...)`
  # open for the full 300s even after `docker run` above returns in under a
  # second (found live: the gate hung for exactly one watchdog period per
  # check before this fix).
  ( sleep 300; docker kill "$cname" >/dev/null 2>&1 || true ) </dev/null >/dev/null 2>&1 &
  local watchdog=$!
  set +e
  local out status
  out=$(docker run --name "$cname" --rm "${secopt[@]}" "$@" \
    -e DATABASE_URL=postgres://gate:gate@127.0.0.1:1/gate \
    "$tag" 2>&1)
  status=$?
  set -e
  kill "$watchdog" >/dev/null 2>&1 || true
  wait "$watchdog" 2>/dev/null || true
  printf '%s\x1e%s' "$status" "$out"
}

# check_refuses_without_landlock: a policy that denies all three Landlock
# syscalls must make the worker refuse to start, naming the escape hatch --
# and never claim confinement or reach postgres.
check_refuses_without_landlock() {
  local class=$1 envvar=$2 tag=$3
  local profile; profile=$(mktemp)
  write_seccomp_profile "$profile" 38 landlock_create_ruleset landlock_add_rule landlock_restrict_self

  local result out status
  result=$(run_gated "$class" noland "$profile" "$tag")
  status=${result%%$'\x1e'*}
  out=${result#*$'\x1e'}
  rm -f "$profile"

  local ok=1 reasons=()
  printf '%s\n' "$out" | grep -qF "${class} sandbox: landlock is unavailable" \
    || { ok=0; reasons+=("missing 'landlock is unavailable' in the refusal"); }
  printf '%s\n' "$out" | grep -qF "Set ${envvar}=true" \
    || { ok=0; reasons+=("refusal does not name Set ${envvar}=true as the remedy"); }
  printf '%s\n' "$out" | grep -qF "🔒" \
    && { ok=0; reasons+=("output contains 🔒 -- confinement should never have been attempted"); }
  printf '%s\n' "$out" | grep -qF "postgres:" \
    && { ok=0; reasons+=("reached the postgres connect attempt -- should have refused at startup"); }
  [ "$status" -ne 0 ] \
    || { ok=0; reasons+=("exit status 0, want non-zero"); }

  if [ "$ok" = 1 ]; then
    pass "${class} refuses-without-landlock"
    printf '%s\n' "$out" | grep -F "landlock is unavailable" | head -1 | sed 's/^/  /'
  else
    fail "${class} refuses-without-landlock" "${reasons[@]}" "--- full output ---" "$out"
  fi
}

# check_escape_hatch: the SAME denied policy, plus the class's own escape
# hatch set to "true" -- the worker must start unconfined, say so loudly, and
# never claim the lock symbol.
check_escape_hatch() {
  local class=$1 envvar=$2 tag=$3
  local profile; profile=$(mktemp)
  write_seccomp_profile "$profile" 38 landlock_create_ruleset landlock_add_rule landlock_restrict_self

  local result out status
  result=$(run_gated "$class" escape "$profile" "$tag" -e "${envvar}=true")
  status=${result%%$'\x1e'*}
  out=${result#*$'\x1e'}
  rm -f "$profile"

  local ok=1 reasons=()
  printf '%s\n' "$out" | grep -qF "⚠️  ${envvar}=true" \
    || { ok=0; reasons+=("missing the ⚠️  ${envvar}=true unconfined warning"); }
  printf '%s\n' "$out" | grep -qF "postgres:" \
    || { ok=0; reasons+=("never reached the postgres connect attempt"); }
  printf '%s\n' "$out" | grep -qF "🔒" \
    && { ok=0; reasons+=("output contains 🔒 -- an escape-hatched run must never claim confinement"); }

  if [ "$ok" = 1 ]; then
    pass "${class} escape-hatch"
    printf '%s\n' "$out" | grep -F "${envvar}=true" | head -1 | sed 's/^/  /'
  else
    fail "${class} escape-hatch" "${reasons[@]}" "--- full output ---" "$out"
  fi
}

# check_preflight_catches_restrict_self: a policy that allows the ABI query
# (landlock_create_ruleset) and landlock_add_rule but denies ONLY
# landlock_restrict_self -- ResolveHelper answers "confined", and only the
# PREFLIGHT's real exec-and-judge catches that the ruleset can never actually
# be applied. Must refuse at the preflight, never reach postgres.
check_preflight_catches_restrict_self() {
  local class=$1 bin=$2 tag=$3
  local profile; profile=$(mktemp)
  write_seccomp_profile "$profile" 1 landlock_restrict_self

  local result out status
  result=$(run_gated "$class" restrictself "$profile" "$tag")
  status=${result%%$'\x1e'*}
  out=${result#*$'\x1e'}
  rm -f "$profile"

  local ok=1 reasons=()
  printf '%s\n' "$out" | grep -qF "🔒 ${class} sandbox: landlock via" \
    || { ok=0; reasons+=("missing the '🔒 ${class} sandbox: landlock via' startup line"); }
  printf '%s\n' "$out" | grep -qF "${class} sandbox preflight:" \
    || { ok=0; reasons+=("missing '${class} sandbox preflight:' -- the preflight should have failed and refused startup"); }
  printf '%s\n' "$out" | grep -qF "postgres:" \
    && { ok=0; reasons+=("reached the postgres connect attempt -- the preflight should have refused first"); }

  if [ "$ok" = 1 ]; then
    pass "${class} preflight-catches-restrict-self"
    printf '%s\n' "$out" | grep -F "sandbox preflight:" | head -1 | sed 's/^/  /'
  else
    fail "${class} preflight-catches-restrict-self" "${reasons[@]}" "--- full output ---" "$out"
  fi
}

for class in "$@"; do
  read -r dockerfile bin envvar expects_proven < <(class_row "$class")
  tag="octoconv-${class}:sandbox-liveproof-${RUN_ID}"

  echo "== ${class}: building ${tag} from ${dockerfile} =="
  build_log=$(mktemp)
  if ! docker build -f "$dockerfile" -t "$tag" . >"$build_log" 2>&1; then
    cat "$build_log"
    rm -f "$build_log"
    die "build failed for class ${class} (${dockerfile})"
  fi
  rm -f "$build_log"

  echo "commit: $(git rev-parse HEAD)"
  echo "tracked dirt: $(git status --short | wc -l | tr -d ' ') file(s)"
  echo "image id: $(docker image inspect "$tag" --format '{{.Id}}')"

  check_confined_startup "$class" "$bin" "$expects_proven" "$tag"
  check_refuses_without_landlock "$class" "$envvar" "$tag"
  check_escape_hatch "$class" "$envvar" "$tag"
  check_preflight_catches_restrict_self "$class" "$bin" "$tag"

  docker rmi -f "$tag" >/dev/null 2>&1 || true
done

if [ "$FAILED" -eq 0 ]; then
  echo "ALL PASS (${CHECK_COUNT} checks)"
else
  echo "FAILURES PRESENT (${CHECK_COUNT} checks run)"
  exit 1
fi
