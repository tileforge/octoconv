---
status: awaiting_human_verify
trigger: "TestCADStageDeadlineIsNotTheAttemptDeadline mesh subtest: cad_test.go:508: took 30.592649542s (limit 5s) under full ./... run; err still ErrCADMeshTimeout"
created: 2026-10-17T04:05:00Z
updated: 2026-10-17T05:55:00Z
---

## Current Focus

bug_class: Heisenbug (timing race in the kernel, deterministic once the kill lands inside the fork)

reasoning_checkpoint:
  hypothesis: "On darwin (XNU) killpg(SIGKILL) is not atomic against a concurrent fork. The fake mesh `#!/bin/sh` + `sleep 30` makes sh fork sleep right after Start; when the 300ms stage deadline's group SIGKILL lands inside that fork, the new sleep is created in the group but never receives the signal, is reparented to launchd, and keeps the stdout/stderr pipe write ends. runCommandCore's <-done waits on cmd.Wait, which after reaping sh blocks in os/exec awaitGoroutines until sleep exits on its own (30s)."
  confirming_evidence:
    - "Real test binary under load: 9 of 400 runs took 30.6-30.9s; goroutine dump in os/exec.(*Cmd).awaitGoroutines with both pipe readers in IO wait (sh already reaped); ps shows `sleep 30` with ppid 1 and pgid equal to the killed group"
    - "Standalone copy of the runCommandCore kill path, deadline swept 0-30ms into the fork window: 26 escapes in 3600 runs with `sleep 3`, every escapee in the killed pgid with ppid 1, pipe EOF exactly at sleep's natural exit; 0 escapes in 3600 runs with `exec sleep 3`"
    - "A second kill(-pgid, SIGKILL) after an escape killed the escapee and delivered pipe EOF within 0.3-2ms in every case where it was tried"
  falsification_test: "An escape with `exec sleep` (no fork in the group), or an escapee whose pgid differs from the killed group, or a 30s wait with sh not yet reaped"
  fix_rationale: "exec.go is frozen. The test-side cause is the fake forking at the kill instant; `exec sleep 30` keeps a single process in the group, so the group kill cannot race a fork. Group-kill coverage of a forked grandchild stays with TestCADCancelMidMeshLeavesNoProcessOfTheGroupAlive, which kills only after the grandchild exists."
  blind_spots: "Linux atomicity is from kernel source/history (2006 'make fork() atomic wrt pgrp/session signals'), not observed here because no live stack may run; whether any shipped engine's descendant leaves the process group on Linux (setsid) was not checked live"
  candidate_causes:
    - "code: runCommandCore single killpg plus unbounded cmd.Wait with pipe-backed stdout/stderr (no WaitDelay, no re-kill)"
    - "environment: XNU killpg vs fork race (FreeBSD fixed the same class in 2023, D40493); Linux fixed in 2006"
    - "environment: host stalls (Jamf Protect ES extension at 100-150% CPU, restarted during the run) making the leader or the test process itself unschedulable for 3-14s"
    - "data: test fixture shape (shell forks the long-running child)"
  and_gate: "yes: the 30s wait needs (1) a fork in flight inside the group at the kill instant (fixture shape + host load shifting sh's fork towards the deadline) AND (2) a kernel whose killpg misses it (darwin) AND (3) a wait not bounded after the kill (runCommandCore). Removing any one removes the 30s wait."

next_action: human confirms the next full-suite gate; then archive and add the knowledge-base entry

## Symptoms

expected: mesh stage with MeshTimeout 300ms returns ErrCADMeshTimeout within 5s
actual: returned ErrCADMeshTimeout after 30.59s, i.e. after the fake `sleep 30` exited on its own
errors: cad_test.go:508: took 30.592649542s
reproduction: one-off during GOTOOLCHAIN=go1.26.4 go test ./... -count=1 (parallel packages, loaded host); 5 isolated reruns and a second full-package run passed
started: observed once in phase 76 verification

## Eliminated

- hypothesis: kill lands before the child calls setpgid
  evidence: Go's forkExec returns from Start only after the child exec'd (CLOEXEC status pipe), and setpgid runs in the child before exec; every escapee observed is IN the killed pgid
  timestamp: 2026-10-17T04:40:00Z

- hypothesis: sh itself survives the kill (leader not reaped)
  evidence: in all 9 30s-class hangs the dump shows Process.Wait already returned (awaitGoroutines); in the sweep the leader was reaped in ~1ms
  timestamp: 2026-10-17T04:40:00Z

- hypothesis: the sandbox helper or a re-exec holds the pipes
  evidence: the test's CADConverter has a nil Sandbox; the helper uses syscall.Exec (internal/sandbox/execmode.go:98), which replaces the process rather than forking
  timestamp: 2026-10-17T04:40:00Z

## Evidence

- timestamp: 2026-10-17T04:05:00Z
  checked: internal/convert/exec.go runCommandCore
  found: Setpgid true, cmd.Stdout/Stderr are tailBuffers (non-*os.File, so os/exec creates pipes and copy goroutines), on ctx.Done it sends SIGKILL to -pid once then blocks on <-done (cmd.Wait) with no WaitDelay
  implication: Wait returns only when every holder of the pipe write ends has exited

- timestamp: 2026-10-17T04:05:00Z
  checked: cad.go meshStage, sandboxwrap.go runBounded, test fixture
  found: nil Sandbox, so the fake #!/bin/sh script `sleep 30` runs directly through runCommandCore; bash (/bin/sh -> /bin/bash) forks sleep as a child holding fds 1 and 2
  implication: a 30s Wait means sleep survived the group SIGKILL

- timestamp: 2026-10-17T04:10:00Z
  checked: real mesh subtest, -count=200 under 22 `yes` workers on 11 cores
  found: 1 failure, 11.3s (not 30s)
  implication: reproduces under load; the 11s one is a different slow mode (see below)

- timestamp: 2026-10-17T04:25:00Z
  checked: temporary watchdog test (goroutine dump + ps at 3s), -count=400 under load
  found: 13 slow; 9 at 30.6-30.9s with awaitGoroutines + orphan `sleep 30` ppid 1 in the killed pgid; 4 at 3.5-13.8s with the guard stage still running or the killed leader not yet reaped, ps showing `(bash)` in state R
  implication: two modes; the 30s mode matches the original failure and is the fork escape; the 3-14s mode is host stall

- timestamp: 2026-10-17T04:35:00Z
  checked: standalone probe, deadline swept 0-30ms, 4 workers, no extra load
  found: fork shape `sleep 3`: 6/1200, 12/1800, 4/1800, 17/1800, 9/1800 escapes; exec shape `exec sleep 3`: 0/1800, 0/1800; rekill after escape: EOF in 0.3-2ms every time
  implication: escape requires a fork in flight inside the group; a second killpg closes it

- timestamp: 2026-10-17T04:45:00Z
  checked: probe slow-reap entries with exec shape
  found: 16 runs with reap 6-14s but the 1s watchdog timer in the same process did not fire either
  implication: the whole parent process was stalled; host stall, independent of fixture shape and kill code

- timestamp: 2026-10-17T04:45:00Z
  checked: every process start/kill path in non-test code
  found: runCommandCore (exec.go) and runCommandStreaming (progressexec.go) both do a single kill(-pid, SIGKILL) then an unbounded cmd.Wait over pipe-backed buffers; every engine (libvips, libreoffice, pandoc, tectonic, chromium, ocr, whisper, av, audio, verapdf, all cad stages, probes) goes through one of the two; the sandbox helper execs (no fork)
  implication: on darwin every engine whose process forks children shares the gap; on Linux the kernel makes fork atomic against pgrp signals

- timestamp: 2026-10-17T05:00:00Z
  checked: first versus second run of a freshly written `#!/bin/sh` script on this host
  found: first run 285-415ms (one 8.9s), second 17-77ms; Go's Start returns in ~2ms either way, so the cost is inside sh's own startup
  implication: sh reaches its fork of `sleep` roughly 300ms after Start, which is exactly the 300ms MeshTimeout; that is why this test lands its kill inside the fork window far more often than a reused script would

- timestamp: 2026-10-17T05:10:00Z
  checked: A/B run (fork-shape watchdog test and exec-shape fixed test concurrently, 300 each, under 22 yes)
  found: fork shape 0 escapes, 22 runs over 3s at 3.1-11.9s; exec shape 11/300 failures at 6.0-13.4s, none near 30s
  implication: under that load the 3-14s host-stall mode dominates both shapes and is independent of the fix; the escape rate depends on how sh's startup lines up with the deadline

- timestamp: 2026-10-17T05:30:00Z
  checked: interleaved blocks under 22 yes, 4x75 fork shape then 4x75 exec shape
  found: fork shape 2 runs at 30.66s and 31.06s, each with orphan `sleep 30` ppid 1 in the killed pgid and the dump in awaitGoroutines; exec shape 0 runs near 30s, 4 at 7.0-13.1s
  implication: the 30s mode is gone with the exec shape; the 3-14s host-stall mode remains under synthetic overload

- timestamp: 2026-10-17T05:50:00Z
  checked: build-overlay mutants of exec.go (file on disk untouched) against the fixed test
  found: no-kill mutant -> mesh subtest fails (took 30.7s); pid-only kill mutant -> fixed subtests pass, TestCADCancelMidMeshLeavesNoProcessOfTheGroupAlive fails (did not return within 2s); under the no-kill mutant the attempt-deadline subtest still passes in 1.08s
  implication: the fixed subtest still detects a missing kill; group-kill coverage of a forked child lives in the dedicated grandchild test; the attempt-deadline subtest's 300ms parent deadline expires inside the guard stage on this host, so it does not exercise the mesh stage (pre-existing, not changed here)

## Resolution

root_cause: XNU killpg is not atomic against a concurrent fork; the test fake (`sh` forking `sleep 30`) puts a fork in flight inside the group at the moment the 300ms deadline's single group SIGKILL lands (first-run startup of a freshly written script is ~300ms on this host, plus load jitter); the new sleep escapes the signal in the killed pgid, keeps the stdout/stderr pipes, and runCommandCore's unbounded cmd.Wait waits for it to exit (30s)
fix: test-only. The three fakes of TestCADStageDeadlineIsNotTheAttemptDeadline run `exec sleep 30`, so the group holds one process and the kill cannot race a fork. Production hardening proposed, not applied (exec.go frozen): in runCommandCore and runCommandStreaming, repeat kill(-pgid, SIGKILL) until ESRCH while waiting, and set cmd.WaitDelay as a backstop
verification:
  target_test: { result: pass, note: "0 runs near 30s in 300 interleaved exec-shape runs vs 2 in 300 fork-shape; 0/3600 escapes vs 26/3600 in the deadline sweep; host-stall failures at 6-14s remain under 22-yes overload" }
  mutation_check: { result: pass, reason_if_skipped: "no Stryker for Go; done with go test -overlay", mutant_killed: "no-kill mutant killed by the fixed subtest; pid-only mutant killed by TestCADCancelMidMeshLeavesNoProcessOfTheGroupAlive, survives the fixed subtest by design" }
  no_op_deletion: { result: pass, deletion_justified_by_rca: true, note: "fixture shape change, no assertion weakened" }
  adjacent_tests: { result: pass, note: "go vet ./... clean; go test ./internal/convert -count=1 ok 463s; go test ./... -count=1 rc=0, 32 packages ok" }
  revert_and_reconfirm: { result: pass, note: "fork shape (reverted fixture) reproduces the 30s mode in the interleaved blocks; exec shape does not" }
  guardrail_verdict: accepted
oracle_type: specified
files_changed: [internal/convert/cad_test.go]
