---
phase: 41-progress-core
plan: 02
subsystem: convert
tags: [ffmpeg, progress-streaming, fifo, exec-hardening, tdd]

# Dependency graph
requires:
  - phase: 41-progress-core
    provides: "ProgressUpdate/ProgressSink/ProgressConverter seam contracts, progressBlockParser/computeProgressPct/etaEstimator/buildProgressUpdate (41-01)"
provides:
  - "runCommandStreaming: a byte-for-byte-independent sibling of runCommand that streams ffmpeg -progress over a named FIFO on the file: protocol, with the identical Setpgid+SIGKILL discipline and a leak-safe reader (zero goroutine leak on SIGKILL AND on a never-opened FIFO)"
  - "AVConverter.ConvertWithProgress: implements ProgressConverter (D-01) -- transcode's full re-encode branch ramps 0->100 monotonically; stream-copy-eligible transcode, audio-extract, and thumbnail all stay null; source_duration_seconds reported once (D-05)"
  - "avPrepareConversion: the shared preamble Convert and ConvertWithProgress both call, so the two entry points can never diverge on accept/reject decisions"
affects: [41-03-worker-throttle-write, 41-04]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Named-FIFO-over-file:-protocol transport for streaming a subprocess's side-channel output when the hardened binary's protocol allowlist excludes fd-based schemes"
    - "Retry-on-transient-EOF/EAGAIN reader loop gated by an explicit Go stop channel, since a FIFO's read() cannot itself distinguish 'no writer yet' from 'writer gone for good'"
    - "Shared preamble helper (avPrepareConversion) reused by two sibling entry points (Convert/ConvertWithProgress) instead of duplicating guard-stage logic"

key-files:
  created:
    - internal/convert/progressexec.go
    - internal/convert/progressexec_test.go
    - internal/convert/av_progress_test.go
  modified:
    - internal/convert/av.go

key-decisions:
  - "Transport is a named FIFO opened via syscall.Mkfifo, fed to ffmpeg as \"-progress file:<fifo>\" -- NOT the fd-3/pipe:3/cmd.ExtraFiles design in 41-RESEARCH.md, which was live-verified (orchestrator, prior to this plan) to fail \"Protocol not found\" against the pinned octoconv-av-worker:dev ffmpeg build (--enable-protocol='file,crypto' only). Locked in 41-02-PLAN.md's <transport_correction> block; this plan implements exactly that correction."
  - "The reader goroutine treats BOTH io.EOF and EAGAIN from a non-blocking FIFO read as transient 'no data yet' signals and retries (5ms backoff) unless an explicit Go 'stop' channel has been closed by the parent after reaping the child. This was discovered empirically during this plan's execution (see Deviations) -- a FIFO read() with zero writers connected returns an immediate EOF regardless of blocking mode, and once ffmpeg's writer IS connected but has not yet written data, a non-blocking read returns EAGAIN, not EOF. Both must be retried; only 'stop' being closed makes the NEXT such signal final."
  - "avPrepareConversion factors AVOptsFromMap + target-format fail-fast + duration/resolution/disk guard stage + avProbeSource into one function called by BOTH Convert and ConvertWithProgress, per the task's explicit 'do not duplicate that logic' instruction -- Convert's observable behavior is unchanged (verified by the full existing av_test.go suite passing unmodified)."
  - "convertTranscodeWithProgress's stream-copy-eligible branch delegates straight back to the existing convertTranscode (not a duplicate remux call) -- this is what makes 'zero Update calls on stream-copy' provably true rather than merely asserted."

patterns-established:
  - "Pattern: retry-on-EOF-and-EAGAIN reader loop gated by a Go stop channel -- the correct idiom for consuming a named FIFO whose writer's lifecycle (opens late, may never open) cannot be inferred from read() return values alone."

requirements-completed: []  # Deliberately NOT marking PROG-01/PROG-05 complete per this plan's scope -- Plan 03 (worker throttle-write) and Plan 04 (live gate) finish the requirements end-to-end.

# Metrics
duration: ~55min
completed: 2026-07-25
---

# Phase 41 Plan 02: Streaming Exec + AV Progress Seam Summary

**`runCommandStreaming` streams ffmpeg's `-progress` feed over a named FIFO on the allowed `file:` protocol (not the unavailable `pipe:` scheme), with a leak-safe reader proven at zero goroutines on both SIGKILL and a never-opened FIFO; `AVConverter.ConvertWithProgress` wires it into the transcode path only, ramping 0->100 monotonically while audio-extract/thumbnail/stream-copy stay null.**

## Performance

- **Duration:** ~55 min
- **Completed:** 2026-07-25
- **Tasks:** 2/2 completed
- **Files modified:** 4 (3 new, 1 modified)

## Accomplishments
- Implemented `runCommandStreaming`, mirroring `runCommand`'s exact Setpgid + `syscall.Kill(-pid, SIGKILL)` process-group-kill discipline while adding a named-FIFO `-progress file:<fifo>` transport -- `exec.go` remains byte-for-byte unchanged (`git diff --exit-code` passes).
- Discovered and fixed a real hang during test-driven development: a FIFO read with zero writers returns an immediate `io.EOF` regardless of blocking mode, and once a writer IS connected but silent, a non-blocking read returns `EAGAIN` (not `EOF`) -- both had to be treated as retryable, gated by an explicit stop signal, or the reader would either terminate prematurely (closing the read end and deadlocking the parent's post-`Wait()` write-end poke) or spin forever.
- Proved zero goroutine leak across >=5 real SIGKILL-mid-stream iterations AND a never-opened-FIFO case (a command that exits without ever touching the progress FIFO) via `settledGoroutines` (`runtime.NumGoroutine()` delta assertion with a settle-loop to avoid scheduling-lag flakes).
- Refactored `AVConverter.Convert`'s preamble into `avPrepareConversion`, reused verbatim by the new `ConvertWithProgress` so both entry points make the provably identical guard/probe decision -- confirmed by the full pre-existing `av_test.go` suite passing unmodified.
- Implemented `AVConverter.ConvertWithProgress`: reports `source_duration_seconds` once from the already-probed `src.duration` (no new ffprobe, D-05); the transcode target's full re-encode branch streams through `runCommandStreaming` and reports a monotonic 0->100 ramp via `newTranscodeProgressReporter` (clamped against any raw regression); the stream-copy-eligible transcode branch, audio-extract, and thumbnail all delegate to the existing plain-`runCommand` paths, emitting zero `Update()` calls (D-02).

## Task Commits

Each task followed RED -> GREEN:

1. **Task 1: runCommandStreaming -- named-FIFO progress transport with leak-safe SIGKILL drain**
   - `b57f3b9` test(41-02): add failing tests for runCommandStreaming FIFO progress transport
   - `a208c91` feat(41-02): implement runCommandStreaming named-FIFO progress transport
2. **Task 2: AVConverter.ConvertWithProgress -- ramp on transcode, null on cheap paths (D-02)**
   - `c6fbde6` test(41-02): add failing tests for AVConverter.ConvertWithProgress
   - `b08ecda` feat(41-02): implement AVConverter.ConvertWithProgress (D-02 ramp split)

_No REFACTOR commit needed -- both GREEN implementations landed clean after the one debugging fix (folded into the Task 1 feat commit, see Deviations)._

**Plan metadata:** committed separately below (docs commit)

## Files Created/Modified
- `internal/convert/progressexec.go` - `progressStreamArgs` (pure argv-prepend helper, unit-testable without a subprocess) + `runCommandStreaming` (Mkfifo, O_NONBLOCK read-open, retry-on-EOF/EAGAIN reader loop gated by a `stop` channel, post-`Wait()` O_WRONLY poke, `scanDone` joined in both select arms)
- `internal/convert/progressexec_test.go` - argv-hardening (pure function, `file:` not `pipe:`), never-opened-FIFO goroutine-delta test, real-ffmpeg success-path test, and a >=5-iteration real-ffmpeg SIGKILL-mid-stream goroutine-delta test (both real-ffmpeg tests gated behind `testing.Short()` + `requireLiveAVBinaries`)
- `internal/convert/av.go` - `avPrepareConversion` (shared preamble), `avBuildReencodeArgs` (shared re-encode argv builder, extracted from `convertTranscode`), `ConvertWithProgress` + `convertTranscodeWithProgress` + `newTranscodeProgressReporter`, `var _ ProgressConverter = AVConverter{}`
- `internal/convert/av_progress_test.go` - synthetic monotonic-clamp unit test (no subprocess) plus live-binary tests for the ramp path, the stream-copy-eligible null path, and the audio-extract/thumbnail null paths, each asserting `SourceDuration` called exactly once

## Decisions Made
- Transport: named FIFO + `file:` protocol, per the plan's `<transport_correction>` (the fd-3/`pipe:3` design in 41-RESEARCH.md does not work against the pinned hardened ffmpeg binary).
- Reader retry policy: both `io.EOF` and `EAGAIN` are treated as transient on a non-blocking FIFO read; only an explicit Go `stop` channel (closed by the parent after `cmd.Wait()` returns) makes the next such signal final. This is NOT optional ceremony -- it is what makes the never-opened-FIFO case resolve instead of hang (see Deviations).
- `avPrepareConversion`/`avBuildReencodeArgs` extraction: reuse over duplication, per the task's explicit instruction, verified by the full existing `av_test.go` suite passing with zero behavior changes to `Convert`.
- The stream-copy-eligible branch in `convertTranscodeWithProgress` delegates directly to `convertTranscode` rather than re-implementing the remux call, so "zero Update calls" is structurally guaranteed, not just asserted by a passing test.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Reader goroutine treated EAGAIN as a fatal unexpected error, causing a real hang**
- **Found during:** Task 1, initial live-ffmpeg success-path test run (`go test -run TestRunCommandStreaming_SuccessPath` hung past a 45s timeout)
- **Issue:** The reader's `bufio.Reader.ReadString` loop only special-cased `io.EOF` as "retry, transient no-writer-yet state" and treated any other error (including `EAGAIN`, wrapped in a `*fs.PathError`) as fatal, closing the read end and exiting the goroutine. Empirically (verified with a standalone debug harness against the real local ffmpeg binary), once ffmpeg's writer connects to the FIFO but has not yet written a line, a non-blocking read returns `EAGAIN`, not `EOF` -- this is a DIFFERENT transient signal than "zero writers" EOF, but must be retried identically. The reader's premature exit closed its own read-end fd; the parent's subsequent post-`Wait()` write-end poke (`os.OpenFile(fifoPath, os.O_WRONLY, 0)`) then blocked forever waiting for a reader that no longer existed, hanging the whole call.
- **Fix:** Extended the reader's retry condition to `rerr == io.EOF || errors.Is(rerr, syscall.EAGAIN)`, both gated by the same `stop`-channel check before retrying with a short backoff.
- **Files modified:** `internal/convert/progressexec.go`
- **Verification:** Reproduced the hang in an isolated standalone Go program against the real local ffmpeg binary, confirmed the `EAGAIN` error text (`"resource temporarily unavailable"`), applied the fix in the standalone repro first (confirmed it resolved the hang, full `-progress` block stream captured correctly), then applied the identical fix to `progressexec.go`. All four `TestRunCommandStreaming_*` tests (argv-hardening, never-opened-FIFO, success-path, 5x SIGKILL-mid-stream) pass with `-race`, and `git diff --exit-code internal/convert/exec.go` still returns 0.
- **Committed in:** `a208c91` (Task 1 feat commit -- discovered and fixed before the GREEN commit landed, so no separate fix commit was needed)

None of this deviation altered the plan's scope, transport choice, or the seam contract shapes -- it corrected a genuine bug in the leak-safety implementation of the reader loop the plan specified, caught by the plan's own required test coverage before it ever reached a commit.

## Issues Encountered
None blocking beyond the deviation above, which was fully resolved within Task 1's TDD cycle before its GREEN commit.

## Requirements Status
`requirements-completed: []` -- deliberately left empty per this plan's explicit instruction ("do NOT mark PROG requirements complete -- Plan 03/04 finish them"), even though the plan frontmatter lists `[PROG-01, PROG-05]` as the requirements this work contributes to. PROG-01 (the real ramp / null policy) needs Plan 03's worker wiring + DB write path to be observable end-to-end; PROG-05 (live-verified leak-safety) needs Plan 04's live gate (process-table + goroutine inspection against the real docker-compose stack) in addition to this plan's unit-level proof.

## Known Stubs
None. `runCommandStreaming` and `AVConverter.ConvertWithProgress` are both fully implemented against real ffmpeg invocations -- no placeholder returns, no hardcoded empty values. The worker never yet calls `ConvertWithProgress` (that wiring is Plan 03's job); this is the documented, intentional scope boundary of this plan, not an oversight.

## Threat Flags
None beyond what the plan's own `<threat_model>` already covered (T-41-04/05/06/07) -- no new network endpoints, auth paths, or schema changes were introduced by this plan.

## Self-Check: PASSED

- FOUND: internal/convert/progressexec.go
- FOUND: internal/convert/progressexec_test.go
- FOUND: internal/convert/av_progress_test.go
- FOUND: internal/convert/av.go (modified)
- FOUND: commit b57f3b9
- FOUND: commit a208c91
- FOUND: commit c6fbde6
- FOUND: commit b08ecda
