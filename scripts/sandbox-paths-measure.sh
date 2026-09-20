#!/usr/bin/env bash
# Measure what one engine actually needs to read, and prove the answer.
#
# Two phases, because neither alone is enough:
#
#   trace   runs a real conversion under strace and reports every path the
#           engine successfully opened. This DISCOVERS candidates and it
#           under-reports: the engine's own executable is execve'd, never
#           opened, so it will not appear here.
#
#   verify  runs the same conversions under a real Landlock ruleset built from
#           a candidate set, then repeats one with an input OUTSIDE the job
#           directory. Sufficiency is the engine's exit status; effectiveness
#           is that last run failing. This is the phase that decides.
#
# Landlock is unavailable to an emulated amd64 process on an arm64 host, so a
# service pinned with `platform: linux/amd64` (document-worker, for veraPDF)
# cannot be measured here at all. The script says so rather than reporting an
# engine that "needs nothing".
#
# Usage:
#   scripts/sandbox-paths-measure.sh trace  <image> <fixture-dir> <cmd...>
#   scripts/sandbox-paths-measure.sh verify <image> <fixture-dir> <ro-paths> <cmd...>
#
# Example (image engine, measured 2026-09-20):
#   scripts/sandbox-paths-measure.sh trace octoconv-worker:latest ./job vips copy /job/in.png /job/out.webp
set -euo pipefail

die() { echo "error: $*" >&2; exit 1; }

phase=${1:-}; shift || die "usage: $0 <trace|verify> ..."
image=${1:-}; shift || die "image required"
jobdir=${1:-}; shift || die "fixture directory required"
[ -d "$jobdir" ] || die "no such directory: $jobdir"
jobdir=$(cd "$jobdir" && pwd)

case "$phase" in
trace)
  [ $# -gt 0 ] || die "command required"
  # strace is installed into a throwaway container layer rather than into the
  # image: the thing being measured must stay the thing that ships.
  docker run --rm --user root --cap-add SYS_PTRACE -v "$jobdir:/job" \
    --entrypoint sh "$image" -c '
      apt-get update -qq >/dev/null 2>&1
      apt-get install -y -qq strace >/dev/null 2>&1
      command -v strace >/dev/null || { echo "strace could not be installed" >&2; exit 1; }
      rm -f /job/.trace.*
      strace -ff -e trace=openat,open -o /job/.trace "$@"
    ' sh "$@" >/dev/null

  echo "paths opened successfully, outside the job directory:"
  # Only a completed call with a non-negative result counts. An errno, or a
  # line strace split across <unfinished>/<resumed>, is a path the engine did
  # not get -- and the second kind carries no result to filter on.
  cat "$jobdir"/.trace.* \
    | grep -oE 'openat?\([^,]*, "[^"]+"[^)]*\) *= *[0-9]+' \
    | sed 's/.*"\(.*\)".*/\1/' \
    | grep '^/' | grep -v '^/job/' | sort -u | sed 's/^/  /'
  echo
  echo "NOT shown, and needed anyway: the engine's own binary. Add it to the"
  echo "candidate set by hand and let verify decide."
  rm -f "$jobdir"/.trace.*
  ;;

verify)
  ro=${1:-}; shift || die "colon-separated read-only path list required"
  [ $# -gt 0 ] || die "command required"
  probe="$jobdir/.sandbox-probe"
  CGO_ENABLED=0 GOOS=linux GOARCH="$(docker version --format '{{.Server.Arch}}')" \
    go build -o "$probe" ./scripts/cmd/sandbox-probe
  trap 'rm -f "$probe"' EXIT

  # PROBE_RW and PROBE_CANARY come from the environment: chromium needs /tmp
  # writable, and an engine whose set legitimately includes /etc needs a canary
  # that is not inside it.
  docker run --rm -v "$jobdir:/job" \
    -e PROBE_RW="${PROBE_RW:-/job}" -e PROBE_CANARY="${PROBE_CANARY:-}" -e PROBE_RO="$ro" \
    --entrypoint /job/.sandbox-probe "$image" "$@"
  ;;

*) die "unknown phase: $phase (expected trace or verify)" ;;
esac
