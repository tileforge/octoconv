---
phase: 68-four-straightforward-engine-classes
plan: 04
subsystem: security
tags: [landlock, sandbox, av, ffmpeg, ffprobe, progress, fifo, engineExec, preflight, worker]

requires:
  - phase: 68-four-straightforward-engine-classes
    provides: "68-01: engineExec/forJob/run/sandboxFault, runCommandAs, ErrEngineSandboxUnavailable, the preflight toolkit -- this plan's shared mechanism"
  - phase: 68-four-straightforward-engine-classes
    provides: "68-02: av's re-measured conf05:av:readonly/engines blocks in SANDBOX-PATHS.md, scripts/conf05-crosscheck.sh -- this plan's proven set"
  - phase: 68-four-straightforward-engine-classes
    provides: "68-03: the multi-file engineExec-threading pattern (call sites across several files share one job-scoped engineExec), directly reused here across av.go/avduration.go/audioduration.go"
provides:
  - "runCommandStreaming/engineExec.stream: the progress FIFO's own directory lives inside the caller's job directory (the only writable path under a confined ruleset), not the shared system temp dir, and the -progress option is folded into the ENGINE's own argv BEFORE sandbox wrapping"
  - "AVConverter.Sandbox, AVConverter.exec/WithSandbox: all seven AV engine call sites (ffprobe duration/video-streams/audio-codec, ffmpeg transcode/streaming-transcode/audio-extract/thumbnail) share ONE engineExec per job"
  - "probeDuration/enforceMaxResolution (formerly exported ProbeDuration/EnforceMaxResolution): unexported, both now take the job's engineExec -- no exported entry point in internal/convert can run ffprobe on client input without a sandbox handle"
  - "AVPreflight/avPreflightSourceArgs/avPreflightSink: a content-judged preflight that generates a video-only h264 source inside the job directory and drives it through the STREAMING re-encode path (progress FIFO included), never a stream-copy remux or a bare probe, plus a denial from outside the job directory"
  - "cmd/av-worker: refuses to start unconfined, preflights before db.Connect; deployment-av-worker.yaml gains avWorker.extraEnv"
affects: [68-05-audio-sandbox, 68-06]

actuals:
  tokens: 18000
  tasks: 3
  commits: 3

tech-stack:
  added: []
  patterns:
    - "A streaming engine call folds its dynamically-generated argument (here, '-progress file:<fifo>') into the ENGINE's own argv before any sandbox wrapping -- wrapping first and prepending after would push the option in front of the helper's exec-mode marker (argv[1]) and the worker binary would try to start a second worker instead of exec'ing the engine (T-68-18)"
    - "A progress FIFO (or any engine-written scratch file) is created INSIDE the job directory via os.MkdirTemp(jobDir, '.progress-'), never the shared system temp dir -- the only writable path a confined job's ruleset grants, and the only place that stays invisible to concurrent jobs even when unconfined"
    - "AVPreflight forces the expensive branch on purpose: its generated source is video-only (no audio track), so avStreamCopyLegal can never be satisfied and the positive run is provably driven through the SAME streaming re-encode path a real job takes, not a vacuous remux or probe-only shortcut"

key-files:
  created:
    - internal/convert/avsandbox_test.go
  modified:
    - internal/convert/progressexec.go
    - internal/convert/progressexec_test.go
    - internal/convert/av.go
    - internal/convert/av_test.go
    - internal/convert/avduration.go
    - internal/convert/avduration_test.go
    - internal/convert/audioduration.go
    - internal/convert/audioduration_test.go
    - internal/convert/avsandbox.go
    - cmd/av-worker/main.go
    - deploy/chart/octoconv/templates/deployment-av-worker.yaml

key-decisions:
  - "avPrepareConversion builds x := c.exec(outPath) ONCE per job and threads it through avProbeSource, convertTranscode, convertTranscodeWithProgress, convertAudioExtract and convertThumbnail -- every ffprobe/ffmpeg call a single job makes shares the exact same ruleset, mirroring OCR's own 68-03 threading pattern but concentrated in avPrepareConversion's single preamble rather than spread across several guard-stage functions"
  - "EnforceMaxDuration keeps its exported signature (ctx, path, max) in this plan and calls the now-unexported probeDuration with the zero-value engineExec{} -- its own production caller (worker.go, audio jobs) is out of this plan's scope; plan 05 threads the audio worker's own sandbox handle into it"
  - "AVPreflight's generated fixture deliberately omits an audio track: avStreamCopyLegal requires aac audio for an mp4 target, so a video-only h264 source can never take the cheap stream-copy branch -- the preflight is provably exercising the streaming re-encode path (with its FIFO), the path a real transcode-with-progress job actually takes, not a shortcut that would pass even if the FIFO plumbing were broken"

patterns-established:
  - "Pattern: a streaming (FIFO-based progress) engine call is a variant of the multi-file engineExec-threading pattern established in 68-03 -- the dynamically-built CLI option must be folded into the engine's own argv before the sandbox wrap, via a command-builder closure passed down to the raw runCommandStreaming rather than a pre-built argv slice"

requirements-completed: []  # CONF-01/CONF-02/CONF-04/CONF-05 are declared by every sibling plan in this phase (01-03 done, 05-06 remaining); requirements.ready-ids reports not-ready until every declaring plan has a SUMMARY (shared-ID gate, #2388) -- not an oversight

coverage:
  - id: D1
    description: "All seven AV engine call sites (three ffprobe forms, four ffmpeg forms) run through the job's engineExec under the re-measured set, proven by content on both the plain and progress-streaming Convert paths, with rw:<job dir> as the only writable token on every invocation"
    requirement: CONF-01
    verification:
      - kind: unit
        ref: "internal/convert/avsandbox_test.go#TestAVEveryEngineRunsThroughTheHelper"
        status: pass
      - kind: unit
        ref: "internal/convert/progressexec_test.go#TestEngineExecStream_DeliversProgressLinesThroughTheHelper"
        status: pass
      - kind: integration
        ref: "scripts/sandbox-liveproof.sh av (confined-startup check: '🔒 av sandbox proven: the engine converted inside the job directory and was denied a file outside it')"
        status: pass
    human_judgment: false
  - id: D2
    description: "cmd/av-worker refuses to start when confinement is wanted and cannot be established, naming AV_SANDBOX_DISABLED as the remedy; the escape hatch is reachable from the Helm chart and defaults to confined (absent from values.yaml)"
    requirement: CONF-02
    verification:
      - kind: integration
        ref: "scripts/sandbox-liveproof.sh av (refuses-without-landlock and escape-hatch checks)"
        status: pass
      - kind: integration
        ref: "helm template ... --set avWorker.extraEnv.AV_SANDBOX_DISABLED=true renders the value; default render carries no env block for av-worker; scripts/ci-helm-checks.sh exits 0"
        status: pass
    human_judgment: false
  - id: D3
    description: "AVPreflight judges the positive run by content (a real 64x64 h264 stream reaches the far side AND the progress sink actually received ticks, never exit status alone) and forces the STREAMING re-encode branch, not a stream-copy remux or a bare probe (CONF-04's explicit prohibition for this plan); the live gate also proves the worker refuses to start when only landlock_restrict_self is denied"
    requirement: CONF-04
    verification:
      - kind: unit
        ref: "internal/convert/avsandbox_test.go#TestAVPreflightSourceArgs"
        status: pass
      - kind: unit
        ref: "internal/convert/avsandbox_test.go#TestAVPreflightUnconfinedProvesNothing"
        status: pass
      - kind: integration
        ref: "scripts/sandbox-liveproof.sh av (preflight-catches-restrict-self check)"
        status: pass
    human_judgment: false
  - id: D4
    description: "av's granted set (avReadOnly, seven paths; avEngines, two binaries) is pinned by a test that fails when the set drifts, and cross-checked against SANDBOX-PATHS.md's conf05:av blocks"
    requirement: CONF-05
    verification:
      - kind: unit
        ref: "internal/convert/avsandbox_test.go#TestAVReadOnlySetIsTheMeasuredOne"
        status: pass
      - kind: unit
        ref: "internal/convert/avsandbox_test.go#TestAVEnginesAreTheMeasuredOnes"
        status: pass
      - kind: other
        ref: "scripts/conf05-crosscheck.sh av -> 'MATCH: av readonly (7) engines (2)'"
        status: pass
    human_judgment: false

duration: ~60min (includes a killed prior executor's partial Task 1 work, resumed and completed by this continuation)
completed: 2026-09-24
status: complete
plan_head_before: f7860e56bf14a3a59699b022fdfbece653503e2a
commits: 3
---

# Phase 68 Plan 04: AV Confined End-to-End Summary

**Every ffprobe/ffmpeg call the AV converter makes (three probe forms, four ffmpeg forms including the progress-streaming re-encode) now runs through one shared per-job Landlock ruleset, the progress FIFO moved inside the job directory with its `-progress` option folded into the engine's own argv before sandbox wrapping, and a content-judged preflight proves the streaming re-encode path -- not a stream-copy shortcut -- before the worker accepts a single job.**

## Performance

- **Duration:** ~60 min (continuation of a plan whose Task 1 was left mid-refactor by a prior executor killed on an API auth error; this session read, judged, and finished the partial work, then completed Tasks 2-3)
- **Started:** 2026-09-24T01:03:52Z (approx., right after 68-03)
- **Completed:** 2026-09-24T02:05:00Z (approx.)
- **Tasks:** 3
- **Files modified:** 12 (1 created, 11 modified)

## Accomplishments

- `progressexec.go`: `runCommandStreaming`'s FIFO directory now lives inside the caller-supplied `fifoDir` (the job directory) via `os.MkdirTemp(fifoDir, ".progress-")`, replacing the old shared-system-temp-dir `octoconv-progress-*`. Its signature takes a `command func(fifoPath string) (name string, args []string, err error)` builder instead of a bare argv, so the caller can fold `-progress file:<fifo>` into the engine's own argv before any sandbox wrapping. `(engineExec).stream` is the new confined entry point: it calls `wrapEngine` on `progressStreamArgs(fifoPath, args)`, never the reverse order (T-68-18 -- reversing it would push `-progress` in front of the helper's `__sandbox-exec` marker).
- `av.go`/`avduration.go`/`audioduration.go`: `avPrepareConversion` builds `x := c.exec(outPath)` once per job and threads it through `avProbeSource`, `convertTranscode`, `convertTranscodeWithProgress`, `convertAudioExtract` and `convertThumbnail`. `probeVideoStreams`/`probeVideoStream`/`probeDuration` (formerly exported `ProbeDuration`) all gained an `x engineExec` parameter and route through `x.run`. `EnforceMaxResolution` is now unexported `enforceMaxResolution` with the same treatment -- it has no production caller, so leaving it exported and unconfined-by-default was exactly the shape this phase's decision 1 closes. `EnforceMaxDuration` keeps its exported signature this plan (its production caller is out of scope here) and calls `probeDuration` with the zero-value `engineExec{}`.
- `avsandbox.go`: `avReadOnly` (7 measured paths, unchanged from the original 2026-09-20 measurement) and `avEngines` (`ffmpeg`, `ffprobe`) pinned against SANDBOX-PATHS.md's re-measured `conf05:av` blocks; `(AVConverter).exec(outPath)` builds one shared `engineExec` per job. `AVPreflight`/`avPreflightSourceArgs`/`avPreflightSink` (new): generates a video-only 64x64 h264 lavfi test source inside the job directory through the job's own ruleset, then drives `ConvertWithProgress` against it -- video-only means `avStreamCopyLegal` can never be satisfied (needs aac audio), so the positive run is forced onto the full streaming re-encode branch, proving the FIFO plumbing (sink received ticks) and the output content (exactly one 64x64 h264 stream), not merely that ffmpeg exited 0.
- `avsandbox_test.go` (new): pins the measured set, proves both `Convert` (mp4→mp3) and `ConvertWithProgress` (mov→mp4) route every ffprobe/ffmpeg call through the helper with `rw:<job dir>` as the only writable grant, and asserts the streaming call's post-`--` argv is `[<ffmpeg path>, -progress, ...]`.
- `progressexec_test.go`: the four pre-existing `runCommandStreaming` tests updated to the new signature; four new `engineExec.stream` tests added (delivers-progress-through-the-helper with argv-order assertion, unconfined-runs-directly, sandbox-fault-sentinel, FIFO-directory-removed-on-failure).
- `cmd/av-worker/main.go`: `sandbox.RunExecModeIfRequested` is now the first statement of `main`; the sandbox helper is resolved, a sandbox-wired `AVConverter` is built and registered (exactly one `Registry.Replace`), `SetAVDiskSafetyFactor` runs before the preflight (the guard stage it configures reads it), and `AVPreflight` runs under a 60s context -- all before `db.Connect`.
- `deployment-av-worker.yaml` gains an `avWorker.extraEnv` block (mirrors `ocrWorker`'s) so `AV_SANDBOX_DISABLED=true` is reachable from the chart; absent from `values.yaml`, so the default render carries no env block for av-worker.
- `scripts/sandbox-liveproof.sh av` (built image): `ALL PASS (4 checks)`. `scripts/conf05-crosscheck.sh av`: `MATCH: av readonly (7) engines (2)`. `scripts/ci-helm-checks.sh`: 0 FAIL lines (per-class isolation diff confirms `av` unchanged, `image` changed as expected).

## Task Commits

Each task was committed atomically:

1. **Task 1: Streaming runner — FIFO inside the job directory, progress option added before wrapping, engineExec.stream** - `97a33b0` (test)
2. **Task 2: Route every AV engine call through the job's engineExec; no unconfined exported ffprobe entry points** - `a2f2453` (feat)
3. **Task 3: AVPreflight on the streaming path, routing and set tests, av-worker startup and chart — proven on the built image** - `777ac28` (feat)

**Plan metadata:** committed separately after this SUMMARY (docs).

## Files Created/Modified

- `internal/convert/progressexec.go` - `runCommandStreaming` new signature (fifoDir/label/command builder), `(engineExec).stream`
- `internal/convert/progressexec_test.go` - four existing tests updated, four new `engineExec.stream` tests
- `internal/convert/av.go` - `AVConverter.Sandbox` field; `avPrepareConversion` builds and returns `x engineExec`; `convertAudioExtract`/`convertThumbnail` gained `x engineExec` and route through `x.run`; `convertTranscode`/`convertTranscodeWithProgress` already took `x` (finished by this session)
- `internal/convert/av_test.go` - call sites updated to the new `x engineExec`/`probeVideoStream(ctx, x, ...)`/`probeAudioCodec(ctx, x, ...)` signatures
- `internal/convert/avduration.go` - `probeVideoStreams`/`probeVideoStream` gained `x engineExec`; `EnforceMaxResolution` → unexported `enforceMaxResolution(ctx, x, path, maxHeight)`
- `internal/convert/avduration_test.go` - call sites updated to pass `engineExec{}`
- `internal/convert/audioduration.go` - `ProbeDuration` → unexported `probeDuration(ctx, x engineExec, path)`; `EnforceMaxDuration` unchanged signature, calls `probeDuration(ctx, engineExec{}, path)`
- `internal/convert/audioduration_test.go` - call sites updated to `probeDuration(ctx, engineExec{}, ...)`
- `internal/convert/avsandbox.go` - `avReadOnly`, `avEngines`, `(AVConverter).exec`, `(AVConverter).WithSandbox` (pre-existing from the killed prior executor's Task 1 work, judged correct and kept), plus (this session) `avPreflightSourceArgs`, `avPreflightSink`, `AVPreflight`
- `internal/convert/avsandbox_test.go` - pinned sets, every-engine-through-the-helper proof, preflight source-argv/unconfined tests (new)
- `cmd/av-worker/main.go` - `avSandboxDisabledEnv`; startup block reordered: helper mode first, `ResolveHelper`, `Replace`, `SetAVDiskSafetyFactor`, `AVPreflight`, all before `db.Connect`
- `deploy/chart/octoconv/templates/deployment-av-worker.yaml` - `avWorker.extraEnv` block added

## Decisions Made

See `key-decisions` in frontmatter. Most consequential for plan 05 (audio): the streaming/FIFO variant of the multi-file `engineExec`-threading pattern (68-03) is now proven end-to-end — a dynamically-built CLI option must be folded into the engine's own argv via a command-builder closure passed down to the raw exec helper, resolved only after the FIFO exists, and BEFORE any sandbox wrapping happens.

## Deviations from Plan

None beyond the continuation itself, which is expected process, not a deviation. This plan resumed from a killed prior executor: `internal/convert/av.go`, `internal/convert/progressexec.go`, `internal/convert/progressexec_test.go` carried uncommitted partial edits, and `internal/convert/avsandbox.go` existed as an untracked new file with `avReadOnly`/`avEngines`/`(AVConverter).exec`/`(AVConverter).WithSandbox` already written. Per the resume instructions, this session read the plan, diffed the partial work against it, and judged it: the FIFO/streaming refactor in `progressexec.go`/`progressexec_test.go` and the `avsandbox.go` groundwork were both correct and consistent with the plan's `<action>` blocks, so they were kept as-is. What was genuinely incomplete (not incorrect) was finished: `convertAudioExtract`/`convertThumbnail` in `av.go` were still calling the old `runCommand` directly without the `x engineExec` parameter the plan's own call sites (`Convert`/`ConvertWithProgress`) were already passing to them — a straightforward signature-completion, not a bug in the prior work. `avduration.go` and `audioduration.go` had not been touched at all by the prior executor and were done fresh by this session per Task 2's action list.

## Issues Encountered

The package did not compile at hand-off (as flagged in the resume state): `avProbeSource`/`Convert`/`ConvertWithProgress` were already calling `convertAudioExtract`/`convertThumbnail` with the new `x` argument, but those two functions' own signatures had not been updated to accept it. Fixed as part of Task 2's own scope (the same signature-completion described above), not tracked as a separate deviation since it was squarely inside Task 2's planned action.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- `engineExec.stream`, the FIFO-inside-job-directory pattern, and `avPrepareConversion`'s single-build-once-thread-everywhere shape are ready for plan 05 (audio: `EnforceMaxDuration`'s own signature change, deferred here, is plan 05's to make).
- CONF-01/02/04/05 stay "In Progress" in REQUIREMENTS.md until every plan in this phase that declares them (01-03 done, 05-06 remaining) has its own SUMMARY -- the shared-ID gate (#2388) working as intended.
- No blockers for 68-05/68-06.

---
*Phase: 68-four-straightforward-engine-classes*
*Completed: 2026-09-24*

## Self-Check: PASSED

All ten key files verified present on disk with `[ -f ]`. All three task commits
(`97a33b0`, `a2f2453`, `777ac28`) verified present via `git log --oneline --all`.
`go build ./...`, `go vet ./...`, `go test -count=1 ./internal/convert/
./internal/worker/ ./cmd/...` all exit 0. `scripts/conf05-crosscheck.sh av` prints
`MATCH: av readonly (7) engines (2)`. `scripts/sandbox-liveproof.sh av` (run
earlier this session against a freshly built image) ended `ALL PASS (4 checks)`.
`scripts/ci-helm-checks.sh` ran clean (0 FAIL lines). Every plan-level
`<verification>` command re-run and passing.
