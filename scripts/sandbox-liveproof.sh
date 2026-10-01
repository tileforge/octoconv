#!/usr/bin/env bash
# The per-class live gate: proves a FRESHLY BUILT, uniquely tagged worker
# image actually confines its engine -- not by reading the code, but by
# running the built image and judging its actual startup output.
#
# Usage:
#   scripts/sandbox-liveproof.sh <class>...
#
# Every class this script knows about is in the table below from the start
# (image, pandoc, ocr, av, audio, html, probe, document), including classes not yet
# confined by any converter -- so a class that lands later extends the
# CONVERTER, never this script.
#
# The document class adds three checks of its own after the four generic
# ones (70-04): emulated-amd64-refuses -- the SAME Dockerfile built for
# linux/amd64 and run emulated must refuse to start before Postgres, naming
# DOCUMENT_SANDBOX_DISABLED; and, because 70-02 measured the read-write /tmp
# route, concurrency-guard and sweeps-shared-tmp. The emulation check needs an
# arm64 docker server: on an amd64 one nothing is emulated, so it prints
# "NOT RUN:", is never counted as a pass, and the final line says how many
# checks did not run.
#
# A UNIQUE tag per run, not :latest: a shared tag can be overwritten by a
# concurrent build, and the object under test has to be the one this run
# actually produced, not whatever happened to be tagged last.
set -euo pipefail

die() { echo "error: $*" >&2; exit 1; }

# CSP_LIVE_ROWS must equal the row count of cspShapeTable
# (internal/convert/chromium_test.go) -- all 18 top-level rows (s1-s9,
# x1-x5, f1-f4) of 69-04-PLAN.md's Shape table.
CSP_LIVE_ROWS=18

# class_row prints "dockerfile binary envvar expects_proven extra_run_args"
# for class, or dies on an unknown one. extra_run_args is "-" (meaning none)
# for every class today -- html's own former shared-memory sizing flag (a
# belt-and-suspenders docker /dev/shm size matching docker-compose.yml's
# since-dropped compensating value) was measured unused (G-69-3,
# .planning/research/SANDBOX-PATHS.md html section: a large document converts
# clean through the confined production path with NO /dev/shm mount at all)
# and removed here too, so nothing in this script props up a rationale the
# measurement no longer supports. The field exists for a class whose runs
# genuinely need extra docker arguments -- none does, today.
class_row() {
  case "$1" in
    image)  echo "Dockerfile.worker worker IMAGE_SANDBOX_DISABLED no -" ;;
    pandoc) echo "Dockerfile.pandoc-worker pandoc-worker PANDOC_SANDBOX_DISABLED yes -" ;;
    ocr)    echo "Dockerfile.ocr-worker ocr-worker OCR_SANDBOX_DISABLED yes -" ;;
    av)     echo "Dockerfile.av-worker av-worker AV_SANDBOX_DISABLED yes -" ;;
    audio)  echo "Dockerfile.audio-worker audio-worker AUDIO_SANDBOX_DISABLED yes -" ;;
    html)   echo "Dockerfile.chromium-worker chromium-worker HTML_SANDBOX_DISABLED yes -" ;;
    probe)  echo "Dockerfile.probe-worker probe-worker PROBE_SANDBOX_DISABLED yes -" ;;
    document) echo "Dockerfile.document-worker document-worker DOCUMENT_SANDBOX_DISABLED yes -" ;;
    *) die "unknown class: $1 (expected image, pandoc, ocr, av, audio, html, probe or document)" ;;
  esac
}

[ $# -gt 0 ] || die "usage: $0 <class>..."

RUN_ID=$(date +%s)-$$
FAILED=0
CHECK_COUNT=0
NOT_RUN_COUNT=0

pass() { CHECK_COUNT=$((CHECK_COUNT + 1)); echo "PASS: $1"; }
fail() {
  CHECK_COUNT=$((CHECK_COUNT + 1))
  FAILED=1
  local what=$1; shift
  echo "FAIL: $what"
  for line in "$@"; do echo "  $line"; done
}

# not_run records a check that could not run here. It is NOT a pass and does
# not bump CHECK_COUNT; the final line reports how many did not run.
not_run() {
  NOT_RUN_COUNT=$((NOT_RUN_COUNT + 1))
  echo "NOT RUN: $1"
}

# check_confined_startup runs class's freshly built tag with an unreachable
# DATABASE_URL (fails fast, right after the preflight) and judges the
# combined output. PASS iff it (1) logged the landlock startup line naming
# the exact helper argv, (2) logged "sandbox proven" when this class expects
# one, (3) reached the postgres connect attempt at all (proving the preflight
# cleared), (4) never logged a preflight failure, and (5) exited non-zero.
check_confined_startup() {
  local class=$1 bin=$2 expects_proven=$3 tag=$4
  shift 4
  local extra=("$@") # the class's own extra docker run args (class_row's fifth field), "" for none
  local cname="octoconv-liveproof-${class}-confined-${RUN_ID}"

  # macOS has no timeout(1); a background watchdog kills the container if the
  # run somehow hangs instead of failing fast against the bad DATABASE_URL.
  # Named per run so the watchdog can never kill a sibling class's container.
  ( sleep 300; docker kill "$cname" >/dev/null 2>&1 || true ) </dev/null >/dev/null 2>&1 &
  local watchdog=$!

  set +e
  local out status
  out=$(docker run --name "$cname" --rm ${extra[@]+"${extra[@]}"} \
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
  # A conditional (not an empty-array-expansion) branch on profile: bash 3.2
  # (macOS's shipped /bin/bash) raises "unbound variable" under `set -u`
  # when expanding a DECLARED-BUT-EMPTY array via "${arr[@]}" -- a real, if
  # dormant, bug this file's own doc comment already promised support for
  # ("or none, when profile is ''"), first exercised by a caller with no
  # seccomp profile at all.
  if [ -n "$profile" ]; then
    out=$(docker run --name "$cname" --rm --security-opt "seccomp=${profile}" "$@" \
      -e DATABASE_URL=postgres://gate:gate@127.0.0.1:1/gate \
      "$tag" 2>&1)
  else
    out=$(docker run --name "$cname" --rm "$@" \
      -e DATABASE_URL=postgres://gate:gate@127.0.0.1:1/gate \
      "$tag" 2>&1)
  fi
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
  shift 3
  local extra=("$@")
  local profile; profile=$(mktemp)
  write_seccomp_profile "$profile" 38 landlock_create_ruleset landlock_add_rule landlock_restrict_self

  local result out status
  result=$(run_gated "$class" noland "$profile" "$tag" ${extra[@]+"${extra[@]}"})
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
  shift 3
  local extra=("$@")
  local profile; profile=$(mktemp)
  write_seccomp_profile "$profile" 38 landlock_create_ruleset landlock_add_rule landlock_restrict_self

  local result out status
  result=$(run_gated "$class" escape "$profile" "$tag" -e "${envvar}=true" ${extra[@]+"${extra[@]}"})
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
  shift 3
  local extra=("$@")
  local profile; profile=$(mktemp)
  write_seccomp_profile "$profile" 1 landlock_restrict_self

  local result out status
  result=$(run_gated "$class" restrictself "$profile" "$tag" ${extra[@]+"${extra[@]}"})
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

# check_html_concurrency_guard (D-03): a confined worker whose configured
# concurrency is above 1 must refuse to start, naming HTML_WORKER_CONCURRENCY
# and the shared /tmp, before ever reaching Postgres or claiming confinement
# proven -- the ruleset's shared-/tmp grant is safe only at concurrency 1.
check_html_concurrency_guard() {
  local tag=$1
  shift
  local extra=("$@")

  local result out status
  result=$(run_gated html concurrencyguard "" "$tag" -e "HTML_WORKER_CONCURRENCY=2" ${extra[@]+"${extra[@]}"})
  status=${result%%$'\x1e'*}
  out=${result#*$'\x1e'}

  local ok=1 reasons=()
  printf '%s\n' "$out" | grep -qF "HTML_WORKER_CONCURRENCY=2" \
    || { ok=0; reasons+=("refusal does not name HTML_WORKER_CONCURRENCY=2"); }
  printf '%s\n' "$out" | grep -qF "shared /tmp" \
    || { ok=0; reasons+=("refusal does not name the shared /tmp"); }
  printf '%s\n' "$out" | grep -qF "🔒 html sandbox proven:" \
    && { ok=0; reasons+=("output claims 🔒 html sandbox proven: -- should have refused before the preflight ever ran"); }
  printf '%s\n' "$out" | grep -qF "postgres:" \
    && { ok=0; reasons+=("reached the postgres connect attempt -- should have refused at startup"); }
  [ "$status" -ne 0 ] \
    || { ok=0; reasons+=("exit status 0, want non-zero"); }

  if [ "$ok" = 1 ]; then
    pass "html concurrency-guard"
    printf '%s\n' "$out" | grep -F "HTML_WORKER_CONCURRENCY=2" | head -1 | sed 's/^/  /'
  else
    fail "html concurrency-guard" "${reasons[@]}" "--- full output ---" "$out"
  fi
}

# check_html_wrapper_as_measured (D-08): a package update that changes the
# wrapper shows up here. PASS iff the four values are EXACTLY the measured
# ones: the wrapper's first line, its interpreter (resolved from /bin/sh),
# the count of lines naming the real binary, and the wrapper's byte count.
check_html_wrapper_as_measured() {
  local tag=$1
  shift
  local extra=("$@")
  local cname="octoconv-liveproof-html-wrapper-${RUN_ID}"

  ( sleep 300; docker kill "$cname" >/dev/null 2>&1 || true ) </dev/null >/dev/null 2>&1 &
  local watchdog=$!

  set +e
  local out status
  out=$(docker run --name "$cname" --rm ${extra[@]+"${extra[@]}"} --entrypoint sh "$tag" -c '
    head -n1 /usr/bin/chromium-headless-shell
    readlink -f /bin/sh
    grep -c "^/usr/lib/chromium/chromium-headless-shell " /usr/bin/chromium-headless-shell
    wc -c < /usr/bin/chromium-headless-shell
  ' 2>&1)
  status=$?
  set -e
  kill "$watchdog" >/dev/null 2>&1 || true
  wait "$watchdog" 2>/dev/null || true

  local trimmed
  trimmed=$(printf '%s\n' "$out" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//')
  local want
  want=$(printf '%s\n' '#!/bin/sh -e' '/usr/bin/dash' '1' '61')

  if [ "$status" -eq 0 ] && [ "$trimmed" = "$want" ]; then
    pass "html wrapper-as-measured"
  else
    fail "html wrapper-as-measured" "found: $(printf '%s' "$trimmed" | tr '\n' '|')" \
      "want: $(printf '%s' "$want" | tr '\n' '|')" "--- full output ---" "$out"
  fi
}

# check_html_sweeps_shared_tmp (D-04): a leftover planted in the container's
# /tmp before startup -- a plain file (mimics a killed gs-style scratch file)
# and a chromium-shm-shaped name -- must be gone once the confined worker's
# preflight has proven the sandbox and swept /tmp, BEFORE the engine would
# run a real task.
check_html_sweeps_shared_tmp() {
  local tag=$1
  shift
  local extra=("$@")
  local cname="octoconv-liveproof-html-sweep-${RUN_ID}"

  ( sleep 300; docker kill "$cname" >/dev/null 2>&1 || true ) </dev/null >/dev/null 2>&1 &
  local watchdog=$!

  set +e
  local out status
  out=$(docker run --name "$cname" --rm ${extra[@]+"${extra[@]}"} \
    -e DATABASE_URL=postgres://gate:gate@127.0.0.1:1/gate \
    --entrypoint sh "$tag" -c '
    touch /tmp/octoconv-liveproof-leftover
    touch /tmp/.org.chromium.Chromium.liveproof
    /usr/local/bin/chromium-worker
    echo "--- after ---"
    ls -A /tmp
  ' 2>&1)
  status=$?
  set -e
  kill "$watchdog" >/dev/null 2>&1 || true
  wait "$watchdog" 2>/dev/null || true

  local ok=1 reasons=()
  printf '%s\n' "$out" | grep -qF "🔒 html sandbox proven:" \
    || { ok=0; reasons+=("missing '🔒 html sandbox proven:' -- the preflight never cleared, so the sweep proves nothing here"); }
  local after
  after=$(printf '%s\n' "$out" | awk '/^--- after ---$/{f=1; next} f')
  if printf '%s\n' "$after" | grep -qF "octoconv-liveproof-leftover"; then
    ok=0
    reasons+=("the planted plain-file leftover survived the sweep")
  fi
  if printf '%s\n' "$after" | grep -qF ".org.chromium.Chromium.liveproof"; then
    ok=0
    reasons+=("the planted chromium-shm-shaped leftover survived the sweep")
  fi

  if [ "$ok" = 1 ]; then
    pass "html sweeps-shared-tmp"
  else
    fail "html sweeps-shared-tmp" "${reasons[@]}" "--- full output ---" "$out"
  fi
}

# run_html_csp_live_test docker-runs the freshly cross-compiled convert.test
# binary, read-only bind-mounted at /liveproof, inside tag under tini,
# invoking only TestChromiumCSPPrecedesEveryScriptLive with
# OCTOCONV_CHROMIUM_LIVE=1 -- modeled on run_gated's watchdog discipline
# (background watchdog, stdio redirected to /dev/null, so a $(...) capture is
# not held open) but with a longer watchdog period (900s): the live test
# builds one docker-run per shape-table row, all inside a single process.
run_html_csp_live_test() {
  local tag=$1 bindir=$2
  shift 2
  local extra=("$@")
  local cname="octoconv-liveproof-html-csplive-${RUN_ID}"

  ( sleep 900; docker kill "$cname" >/dev/null 2>&1 || true ) </dev/null >/dev/null 2>&1 &
  local watchdog=$!

  set +e
  local out status
  out=$(docker run --name "$cname" --rm ${extra[@]+"${extra[@]}"} \
    -v "${bindir}:/liveproof:ro" -e OCTOCONV_CHROMIUM_LIVE=1 \
    --entrypoint /usr/bin/tini "$tag" -- \
    /liveproof/convert.test -test.run '^TestChromiumCSPPrecedesEveryScriptLive$' -test.v -test.timeout 10m 2>&1)
  status=$?
  set -e
  kill "$watchdog" >/dev/null 2>&1 || true
  wait "$watchdog" 2>/dev/null || true
  printf '%s\x1e%s' "$status" "$out"
}

# check_html_csp_live (G-69-2): PASS iff exit 0, the output contains
# "--- PASS: TestChromiumCSPPrecedesEveryScriptLive" (unanchored -- -test.v
# indents subtest lines), the count of
# "--- PASS: TestChromiumCSPPrecedesEveryScriptLive/" lines equals
# CSP_LIVE_ROWS, and there is no "--- SKIP" and no "--- FAIL" line.
check_html_csp_live() {
  local tag=$1 bindir=$2
  shift 2
  local extra=("$@")

  local result out status
  result=$(run_html_csp_live_test "$tag" "$bindir" ${extra[@]+"${extra[@]}"})
  status=${result%%$'\x1e'*}
  out=${result#*$'\x1e'}

  local ok=1 reasons=()
  if [ "$status" -ne 0 ]; then
    ok=0
    reasons+=("exit status ${status}, want 0")
  fi
  printf '%s\n' "$out" | grep -q -- "--- PASS: TestChromiumCSPPrecedesEveryScriptLive" \
    || { ok=0; reasons+=("missing '--- PASS: TestChromiumCSPPrecedesEveryScriptLive'"); }
  local pass_count
  pass_count=$(printf '%s\n' "$out" | grep -c -- "--- PASS: TestChromiumCSPPrecedesEveryScriptLive/" || true)
  if [ "$pass_count" -ne "$CSP_LIVE_ROWS" ]; then
    ok=0
    reasons+=("found ${pass_count} passing subtests, want ${CSP_LIVE_ROWS}")
  fi
  if printf '%s\n' "$out" | grep -q -- "--- SKIP"; then
    ok=0
    reasons+=("output contains a --- SKIP line")
  fi
  if printf '%s\n' "$out" | grep -q -- "--- FAIL"; then
    ok=0
    while IFS= read -r line; do reasons+=("$line"); done < <(printf '%s\n' "$out" | grep -- "--- FAIL")
  fi

  if [ "$ok" = 1 ]; then
    pass "html csp-precedes-every-script"
    printf '%s\n' "$out" | grep -F "csp-live:" | head -1 | sed 's/^/  /'
  else
    fail "html csp-precedes-every-script" "${reasons[@]}" "--- full output ---" "$out"
  fi
}

# run_html_large_document_live_test docker-runs the freshly cross-compiled
# convert.test binary, modeled on run_html_csp_live_test (same read-only
# bind mount, tini entrypoint, 900s watchdog), but ADDS --cpus=2 --memory=2g
# to the docker-run arguments -- the deployed limits (chart values.yaml
# chromiumWorker.resources.limits and compose's chromium-worker deploy
# limits) -- and invokes ONLY TestChromiumLargeDocumentNeedsNoDevShmLive
# (69-05-PLAN.md Task 1, G-69-3).
run_html_large_document_live_test() {
  local tag=$1 bindir=$2
  shift 2
  local extra=("$@")
  local cname="octoconv-liveproof-html-largedoc-${RUN_ID}"

  ( sleep 900; docker kill "$cname" >/dev/null 2>&1 || true ) </dev/null >/dev/null 2>&1 &
  local watchdog=$!

  set +e
  local out status
  out=$(docker run --name "$cname" --rm --cpus=2 --memory=2g ${extra[@]+"${extra[@]}"} \
    -v "${bindir}:/liveproof:ro" -e OCTOCONV_CHROMIUM_LIVE=1 \
    --entrypoint /usr/bin/tini "$tag" -- \
    /liveproof/convert.test -test.run '^TestChromiumLargeDocumentNeedsNoDevShmLive$' -test.v -test.timeout 10m 2>&1)
  status=$?
  set -e
  kill "$watchdog" >/dev/null 2>&1 || true
  wait "$watchdog" 2>/dev/null || true
  printf '%s\x1e%s' "$status" "$out"
}

# check_html_large_document_live (G-69-3): PASS iff exit 0, the output
# contains "--- PASS: TestChromiumLargeDocumentNeedsNoDevShmLive" (unanchored
# -- -test.v indents subtest lines), no "--- SKIP" and no "--- FAIL". Runs at
# the deployed limits (--cpus=2 --memory=2g) with no shared-memory docker
# flag of any kind: the whole point of this check is that the confined
# engine needs none.
#
# The test's negative control was rebuilt in 70-06. It no longer demands that
# a run without --disable-dev-shm-usage FAIL on a denied /dev/shm (Chromium 154
# does not fail there: its shared memory is memfd_create), because a failure
# is only one thing a denial can cause. Instead the test runs under the
# production ruleset plus one extra entry, rw:/dev/shm, where /dev/shm is
# reachable and no fallback can hide a wish for it, and an inotify observer
# outside the sandbox must see a control program create an entry there and
# must see the production argv create none. The PASS output carries the
# engine's version (the image installs chromium-headless-shell unpinned, on
# purpose) and the observer's counts.
check_html_large_document_live() {
  local tag=$1 bindir=$2
  shift 2
  local extra=("$@")

  local result out status
  result=$(run_html_large_document_live_test "$tag" "$bindir" ${extra[@]+"${extra[@]}"})
  status=${result%%$'\x1e'*}
  out=${result#*$'\x1e'}

  local ok=1 reasons=()
  if [ "$status" -ne 0 ]; then
    ok=0
    reasons+=("exit status ${status}, want 0")
  fi
  printf '%s\n' "$out" | grep -q -- "--- PASS: TestChromiumLargeDocumentNeedsNoDevShmLive" \
    || { ok=0; reasons+=("missing '--- PASS: TestChromiumLargeDocumentNeedsNoDevShmLive'"); }
  if printf '%s\n' "$out" | grep -q -- "--- SKIP"; then
    ok=0
    reasons+=("output contains a --- SKIP line")
  fi
  if printf '%s\n' "$out" | grep -q -- "--- FAIL"; then
    ok=0
    while IFS= read -r line; do reasons+=("$line"); done < <(printf '%s\n' "$out" | grep -- "--- FAIL")
  fi

  if [ "$ok" = 1 ]; then
    pass "html large-document-without-dev-shm"
    printf '%s\n' "$out" | grep -F "dev-shm:" | head -1 | sed 's/^/  /'
    printf '%s\n' "$out" | grep -F "chromium:" | head -1 | sed 's/^/  /'
    printf '%s\n' "$out" | grep -F "dev-shm-observe:" | head -1 | sed 's/^/  /'
    printf '%s\n' "$out" | grep -F "large-doc:" | head -1 | sed 's/^/  /'
  else
    fail "html large-document-without-dev-shm" "${reasons[@]}" "--- full output ---" "$out"
  fi
}

# check_document_concurrency_guard (D-09, CONF-06): 70-02 measured that
# soffice needs the shared /tmp read-write, so a confined document worker
# configured above one task at a time must refuse to start, naming
# DOCUMENT_WORKER_CONCURRENCY=2 in the guard's own words and the shared /tmp,
# before the preflight or Postgres. The worker's earlier "📄 <var>=<n>" log
# line also names the variable, so the match is on the guard's wording.
check_document_concurrency_guard() {
  local tag=$1
  shift
  local extra=("$@")

  local result out status
  result=$(run_gated document concurrencyguard "" "$tag" -e "DOCUMENT_WORKER_CONCURRENCY=2" ${extra[@]+"${extra[@]}"})
  status=${result%%$'\x1e'*}
  out=${result#*$'\x1e'}

  local ok=1 reasons=()
  printf '%s\n' "$out" | grep -qF "DOCUMENT_WORKER_CONCURRENCY=2 with the sandbox enabled" \
    || { ok=0; reasons+=("refusal does not name DOCUMENT_WORKER_CONCURRENCY=2 as the sandbox guard's error"); }
  printf '%s\n' "$out" | grep -qF "shared /tmp" \
    || { ok=0; reasons+=("refusal does not name the shared /tmp"); }
  printf '%s\n' "$out" | grep -qF "🔒" \
    && { ok=0; reasons+=("output contains 🔒 -- should have refused before confinement was claimed or the preflight ran"); }
  printf '%s\n' "$out" | grep -qF "postgres:" \
    && { ok=0; reasons+=("reached the postgres connect attempt -- should have refused at startup"); }
  [ "$status" -ne 0 ] \
    || { ok=0; reasons+=("exit status 0, want non-zero"); }

  if [ "$ok" = 1 ]; then
    pass "document concurrency-guard"
    printf '%s\n' "$out" | grep -F "with the sandbox enabled" | head -1 | sed 's/^/  /'
  else
    fail "document concurrency-guard" "${reasons[@]}" "--- full output ---" "$out"
  fi
}

# check_document_sweeps_shared_tmp (CONF-06): entries planted in the
# container's /tmp before startup -- a plain file and a soffice-pipe-shaped
# name -- must be gone once the confined worker's preflight has proven the
# sandbox. The preflight converts through the production converter, which
# sweeps /tmp before every task.
check_document_sweeps_shared_tmp() {
  local tag=$1
  shift
  local extra=("$@")
  local cname="octoconv-liveproof-document-sweep-${RUN_ID}"

  ( sleep 300; docker kill "$cname" >/dev/null 2>&1 || true ) </dev/null >/dev/null 2>&1 &
  local watchdog=$!

  set +e
  local out status
  out=$(docker run --name "$cname" --rm ${extra[@]+"${extra[@]}"} \
    -e DATABASE_URL=postgres://gate:gate@127.0.0.1:1/gate \
    --entrypoint sh "$tag" -c '
    touch /tmp/octoconv-liveproof-leftover
    touch /tmp/OSL_PIPE_SingleOfficeIPC_liveproof
    /usr/local/bin/document-worker
    echo "--- after ---"
    ls -A /tmp
  ' 2>&1)
  status=$?
  set -e
  kill "$watchdog" >/dev/null 2>&1 || true
  wait "$watchdog" 2>/dev/null || true

  local ok=1 reasons=()
  printf '%s\n' "$out" | grep -qF "🔒 document sandbox proven:" \
    || { ok=0; reasons+=("missing '🔒 document sandbox proven:' -- the preflight never cleared, so the sweep proves nothing here"); }
  printf '%s\n' "$out" | grep -qF -- "--- after ---" \
    || { ok=0; reasons+=("missing the '--- after ---' separator -- the listing never ran"); }
  local after
  after=$(printf '%s\n' "$out" | awk '/^--- after ---$/{f=1; next} f')
  if printf '%s\n' "$after" | grep -qF "octoconv-liveproof-leftover"; then
    ok=0
    reasons+=("the planted plain-file leftover survived the sweep")
  fi
  if printf '%s\n' "$after" | grep -qF "OSL_PIPE_SingleOfficeIPC_liveproof"; then
    ok=0
    reasons+=("the planted soffice-pipe-shaped leftover survived the sweep")
  fi

  if [ "$ok" = 1 ]; then
    pass "document sweeps-shared-tmp"
  else
    fail "document sweeps-shared-tmp" "${reasons[@]}" "--- full output ---" "$out"
  fi
}

# check_document_emulated_amd64_refuses (D-07, D-08): the SAME Dockerfile,
# built for linux/amd64 and run emulated, cannot establish Landlock, so the
# worker must refuse before Postgres with sandbox.ResolveHelper's own message
# naming DOCUMENT_SANDBOX_DISABLED. No seccomp profile and no escape hatch:
# whatever refuses here is the emulation itself. On an amd64 docker server
# nothing is emulated, so the check is NOT RUN -- never a pass.
check_document_emulated_amd64_refuses() {
  local dockerfile=$1 envvar=$2
  shift 2
  local extra=("$@")

  local server_arch
  server_arch=$(docker version --format '{{.Server.Arch}}' 2>/dev/null || true)
  [ -n "$server_arch" ] || die "cannot read the docker server's architecture"
  case "$server_arch" in
    amd64|x86_64)
      not_run "document emulated-amd64-refuses (the docker server is amd64: nothing is emulated here)"
      return
      ;;
  esac

  local tag2="octoconv-document:sandbox-liveproof-amd64-${RUN_ID}"
  local build_log; build_log=$(mktemp)
  if ! docker build --platform linux/amd64 -f "$dockerfile" -t "$tag2" . >"$build_log" 2>&1; then
    cat "$build_log"
    rm -f "$build_log"
    die "amd64 build failed for class document (${dockerfile})"
  fi
  rm -f "$build_log"
  echo "emulated image id: $(docker image inspect "$tag2" --format '{{.Id}}') ($(docker image inspect "$tag2" --format '{{.Architecture}}'))"

  local result out status
  result=$(run_gated document emulated "" "$tag2" --platform linux/amd64 ${extra[@]+"${extra[@]}"})
  status=${result%%$'\x1e'*}
  out=${result#*$'\x1e'}
  docker rmi -f "$tag2" >/dev/null 2>&1 || true

  local ok=1 reasons=()
  printf '%s\n' "$out" | grep -qF "document sandbox: landlock is unavailable" \
    || { ok=0; reasons+=("missing 'document sandbox: landlock is unavailable' in the refusal"); }
  printf '%s\n' "$out" | grep -qF "Set ${envvar}=true" \
    || { ok=0; reasons+=("refusal does not name Set ${envvar}=true as the remedy"); }
  printf '%s\n' "$out" | grep -qF "🔒" \
    && { ok=0; reasons+=("output contains 🔒 -- an emulated image must never claim confinement"); }
  printf '%s\n' "$out" | grep -qF "postgres:" \
    && { ok=0; reasons+=("reached the postgres connect attempt -- should have refused at startup"); }
  [ "$status" -ne 0 ] \
    || { ok=0; reasons+=("exit status 0, want non-zero"); }

  if [ "$ok" = 1 ]; then
    pass "document emulated-amd64-refuses"
    printf '%s\n' "$out" | grep -F "landlock is unavailable" | head -1 | sed 's/^/  /'
  else
    fail "document emulated-amd64-refuses" "${reasons[@]}" "--- full output ---" "$out"
  fi
}

for class in "$@"; do
  read -r dockerfile bin envvar expects_proven extra_raw < <(class_row "$class")
  tag="octoconv-${class}:sandbox-liveproof-${RUN_ID}"

  # The class's own extra docker run args (class_row's fifth field), applied
  # to EVERY docker run this script makes for this class -- "-" means none.
  extra_run_args=()
  if [ "$extra_raw" != "-" ]; then
    # shellcheck disable=SC2206  # deliberate word-splitting: extra_raw is one of class_row's own values, never user/network input
    extra_run_args=($extra_raw)
  fi

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

  check_confined_startup "$class" "$bin" "$expects_proven" "$tag" ${extra_run_args[@]+"${extra_run_args[@]}"}
  check_refuses_without_landlock "$class" "$envvar" "$tag" ${extra_run_args[@]+"${extra_run_args[@]}"}
  check_escape_hatch "$class" "$envvar" "$tag" ${extra_run_args[@]+"${extra_run_args[@]}"}
  check_preflight_catches_restrict_self "$class" "$bin" "$tag" ${extra_run_args[@]+"${extra_run_args[@]}"}

  if [ "$class" = "html" ]; then
    # Cross-compile the convert.test binary for the IMAGE's architecture, on
    # go.mod's own toolchain (never the host default -- the host toolchain is
    # newer than go.mod, #4619/#4748 class of drift), so the binary the live
    # check runs is proven to be the tree's own build, not an incidental one.
    image_arch=$(docker image inspect "$tag" --format '{{.Architecture}}')
    [ -n "$image_arch" ] || die "cannot read the architecture of $tag"
    go_toolchain="go$(awk '$1=="go"{print $2; exit}' go.mod)"
    echo "toolchain: $(GOTOOLCHAIN="$go_toolchain" go version)"
    bindir=$(mktemp -d)
    if ! CGO_ENABLED=0 GOOS=linux GOARCH="$image_arch" GOTOOLCHAIN="$go_toolchain" \
        go test -c -o "$bindir/convert.test" ./internal/convert >"$bindir/build.log" 2>&1; then
      cat "$bindir/build.log"
      rm -rf "$bindir"
      die "cross-compile of convert.test failed for class ${class}"
    fi

    check_html_concurrency_guard "$tag" ${extra_run_args[@]+"${extra_run_args[@]}"}
    check_html_wrapper_as_measured "$tag" ${extra_run_args[@]+"${extra_run_args[@]}"}
    check_html_sweeps_shared_tmp "$tag" ${extra_run_args[@]+"${extra_run_args[@]}"}
    check_html_csp_live "$tag" "$bindir" ${extra_run_args[@]+"${extra_run_args[@]}"}
    check_html_large_document_live "$tag" "$bindir" ${extra_run_args[@]+"${extra_run_args[@]}"}

    rm -rf "$bindir"
  fi

  if [ "$class" = "document" ]; then
    check_document_emulated_amd64_refuses "$dockerfile" "$envvar" ${extra_run_args[@]+"${extra_run_args[@]}"}
    check_document_concurrency_guard "$tag" ${extra_run_args[@]+"${extra_run_args[@]}"}
    check_document_sweeps_shared_tmp "$tag" ${extra_run_args[@]+"${extra_run_args[@]}"}
  fi

  docker rmi -f "$tag" >/dev/null 2>&1 || true
done

if [ "$FAILED" -eq 0 ]; then
  if [ "$NOT_RUN_COUNT" -eq 0 ]; then
    echo "ALL PASS (${CHECK_COUNT} checks)"
  else
    echo "ALL PASS (${CHECK_COUNT} checks, ${NOT_RUN_COUNT} not run)"
  fi
else
  if [ "$NOT_RUN_COUNT" -eq 0 ]; then
    echo "FAILURES PRESENT (${CHECK_COUNT} checks run)"
  else
    echo "FAILURES PRESENT (${CHECK_COUNT} checks run, ${NOT_RUN_COUNT} not run)"
  fi
  exit 1
fi
