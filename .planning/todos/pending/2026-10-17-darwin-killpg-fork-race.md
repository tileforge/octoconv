---
created: 2026-10-17
area: convert
severity: low
origin: phase 76 verification, debug session .planning/debug/cad-mesh-timeout-30s-wait.md
---

# On darwin a single group SIGKILL can miss a child forked at that moment

## What

`runCommandCore` (`internal/convert/exec.go`) and `runCommandStreaming`
(`internal/convert/progressexec.go`) send one `kill(-pgid, SIGKILL)` on the deadline and
then wait on `cmd.Wait` with no bound. On XNU, killpg is not atomic against a concurrent
fork: a child forked during the kill lands in the group without the signal, is adopted
by launchd, holds the stdout/stderr pipes and keeps `cmd.Wait` blocked until it exits on
its own. Observed: 9 of 400 runs of TestCADStageDeadlineIsNotTheAttemptDeadline under
load waited 30.6–30.9 s; standalone, 26 escapes in 3600 runs for `sh -c 'sleep 3'`,
0 for `exec sleep 3`.

Linux has made fork atomic against group signals since 2006, so the shipped worker
image is not affected (from kernel history, not a live run). Affected: a worker run
outside Docker on macOS, and the Go test suite on macOS hosts.

The test fixture was changed to `exec sleep 30` in 69fee40.

## Idea

In the `ctx.Done` branch of both runners, repeat `kill(-pgid, SIGKILL)` while waiting on
`done` until it returns ESRCH (the pgid cannot be reused while any member is alive), and
set `cmd.WaitDelay` as a backstop. A second kill reaped the escaped process in 0.3–2 ms
in every trial.

## Constraints

- `internal/convert/exec.go` has been kept byte-identical to main through v1.16; changing
  it needs an explicit decision and its own phase or quick task.
- Also check shipped engines for descendants that leave the group (`setsid`), the
  remaining exposure on Linux.
