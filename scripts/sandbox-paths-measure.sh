#!/usr/bin/env bash
# Measure what one engine actually needs to read, and prove the answer.
#
# Two phases, because neither alone is enough:
#
#   trace   runs a real conversion under strace and reports every path the
#           engine successfully opened. This DISCOVERS candidates, and it is
#           wrong in both directions.
#
#           It says nothing reliable about what is EXECUTED. libvips is
#           execve'd and never opened, so /usr/bin/vips appears in no trace
#           under any input, and a set built from the trace alone failed every
#           conversion with "exec: permission denied". Chromium re-execs itself
#           and DOES open its own binary -- so this is a property of the
#           engine, not a rule. Add the executable by hand either way, plus the
#           interpreter when it is a wrapper: /usr/bin/chromium-headless-shell
#           is 61 bytes of `#!/bin/sh`.
#
#           strace -f interleaves processes and splits a call across
#           `<unfinished ...>` and `<... resumed>`; the first line carries the
#           path and no result, so failed opens leaked into the successful set
#           (311 such lines in one chromium run put the non-existent
#           /etc/vulkan into a candidate set). -ff writes one file per process,
#           and only a result of `= <non-negative>` is accepted.
#
#           strace is installed as root and then DROPPED: the traced command
#           runs as the image's configured user, because it is that user's path
#           set we need. Leaving the container as root made chromium report
#           /root/.config and /root/.fontconfig, which the shipped process
#           never touches (codex, PR #27).
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
  image_user=$(docker image inspect "$image" --format '{{.Config.User}}')
  [ -n "$image_user" ] || image_user=root

  docker run --rm --user root --cap-add SYS_PTRACE -v "$jobdir:/job" \
    -e PROBE_USER="$image_user" \
    --entrypoint sh "$image" -c '
      apt-get update -qq >/dev/null 2>&1
      apt-get install -y -qq strace >/dev/null 2>&1
      command -v strace >/dev/null || { echo "strace could not be installed" >&2; exit 1; }
      rm -f /job/.trace.*
      chmod 0777 /job
      # strace runs as the shipped user too and traces its own child, which
      # ptrace allows without the tracer being root.
      setpriv --reuid "$PROBE_USER" --regid nogroup --clear-groups \
        strace -ff -e trace=openat,open -o /job/.trace "$@"
    ' sh "$@" >/dev/null

  # Resolved through symlinks, because Landlock evaluates the path AFTER
  # resolution and the trace records it BEFORE. Ghostscript opens
  # /usr/share/ghostscript/10.00.0/iccprofiles/... and its fonts under
  # Resource/Font; both are symlinks into /usr/share/color and
  # /usr/share/fonts, so a grant on /usr/share/ghostscript covers neither and
  # gs silently emits an empty skeleton instead of failing (codex-adjacent,
  # found while measuring probe).
  echo "paths opened successfully, outside the job directory (-> resolved target when it differs):"
  # Only a completed call with a non-negative result counts. An errno, or a
  # line strace split across <unfinished>/<resumed>, is a path the engine did
  # not get -- and the second kind carries no result to filter on.
  # `|| true` on the whole pipeline: a statically-linked engine (pandoc) can
  # legitimately open NOTHING outside the job directory, and both `grep`
  # stages exit 1 on zero matches. Under `pipefail` that failure propagated
  # from an EARLIER stage aborted the whole script before it ever printed
  # the trailing "NOT shown, and needed anyway" note or cleaned up
  # .trace.* -- found live while re-measuring phase 68 (a genuinely empty
  # result is success, not an error).
  cat "$jobdir"/.trace.* \
    | grep -oE 'openat?\([^,]*, "[^"]+"[^)]*\) *= *[0-9]+' \
    | sed 's/.*"\(.*\)".*/\1/' \
    | grep '^/' | grep -v '^/job/' | sort -u > "$jobdir/.paths" || true

  docker run --rm -v "$jobdir:/job" --entrypoint sh "$image" -c '
    while IFS= read -r p; do
      r=$(readlink -f "$p" 2>/dev/null)
      if [ -n "$r" ] && [ "$r" != "$p" ]; then printf "  %s -> %s\n" "$p" "$r"
      else printf "  %s\n" "$p"; fi
    done < /job/.paths'
  rm -f "$jobdir/.paths"
  echo
  echo "NOT shown, and needed anyway: the engine's own binary. Add it to the"
  echo "candidate set by hand and let verify decide."
  rm -f "$jobdir"/.trace.*
  ;;

verify)
  ro=${1:-}; shift || die "colon-separated read-only path list required"
  [ $# -gt 0 ] || die "command required"
  # The IMAGE's architecture, not the server's. document-worker is pinned to
  # linux/amd64, and a probe built for an arm64 server would run natively,
  # establish Landlock successfully, and leave only the engine emulated -- so
  # the run would report a path set instead of reporting that Landlock is
  # unavailable there, which is the one thing it must say (codex, PR #27).
  image_arch=$(docker image inspect "$image" --format '{{.Architecture}}')
  [ -n "$image_arch" ] || die "cannot read the architecture of $image"
  server_arch=$(docker version --format '{{.Server.Arch}}')
  if [ "$image_arch" != "$server_arch" ]; then
    echo "note: $image is $image_arch on a $server_arch server; the probe is built" \
         "$image_arch and Landlock is expected to be unavailable to it" >&2
  fi

  probe="$jobdir/.sandbox-probe"
  CGO_ENABLED=0 GOOS=linux GOARCH="$image_arch" \
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
