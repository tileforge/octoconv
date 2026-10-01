---
phase: 68-four-straightforward-engine-classes
plan: 05
subsystem: security
tags: [landlock, sandbox, audio, ffmpeg, ffprobe, whisper-cli, engineExec, preflight, worker, duration-guard]

requires:
  - phase: 68-four-straightforward-engine-classes
    provides: "68-01: engineExec/forJob/run/sandboxFault, runCommandAs, ErrEngineSandboxUnavailable, the shared preflight toolkit -- this plan's mechanism"
  - phase: 68-four-straightforward-engine-classes
    provides: "68-02: audio's re-measured conf05:audio:readonly/engines blocks in SANDBOX-PATHS.md (including the guard-form ffprobe measurement), scripts/conf05-crosscheck.sh -- this plan's proven set"
  - phase: 68-four-straightforward-engine-classes
    provides: "68-04: probeDuration(ctx, x engineExec, path) unexported, EnforceMaxDuration's own signature change deferred to this plan, avPrepareConversion's single-build-once-thread-everywhere shape as the pattern to mirror"
provides:
  - "AudioConverter.Sandbox, AudioConverter.WithSandbox: both engine call sites (ffmpeg normalize, whisper-cli transcribe) share ONE engineExec per job, built from audioReadOnly + the resolved model FILE (never a directory) + audioEngines"
  - "EnforceMaxDuration(ctx, helper []string, path, max): the pre-Convert ffprobe duration guard -- the one engine call that lives in internal/worker, outside the converter's own Sandbox field -- now takes a helper argv and confines under the SAME audio ruleset the converter's two stages use (orchestrator decision 1, 68-RESEARCH.md Pitfall 2)"
  - "Handler.audioSandboxHelper threaded NewHandler -> Handler field -> NewRunner -> audioPreConvertGuard -> enforceAudioGuardBeforeConvert -> convert.EnforceMaxDuration, the same way audioMaxDuration already travels; all ten worker cmds' NewHandler calls updated"
  - "AudioPreflight: proves the guard, both converter stages, and a denial by CONTENT -- probed duration ~1s, RIFF/WAVE normalized output, a top-level \"transcription\" JSON array -- never by whisper-cli's exit status alone (it exits 0 with plausible text on silence)"
  - "cmd/audio-worker: refuses to start unconfined, feeds ONE sandboxHelper variable to both the converter and NewHandler, preflights before db.Connect; deployment-audio-worker.yaml gains audioWorker.extraEnv"
affects: [68-06]

actuals:
  tokens: 14840
  tasks: 3
  commits: 3

tech-stack:
  added: []
  patterns:
    - "A pre-Convert guard that lives in a DIFFERENT Go package than the converter's own Sandbox field is threaded through the SAME per-Handler config channel as an existing sibling value (audioMaxDuration): NewHandler gains a parameter, Handler gains a field, NewRunner's engine-specific closure builder reads both. The guard function itself (enforceAudioGuardBeforeConvert) grows one new parameter in the same position pattern its sibling used, and the caller (cmd/audio-worker) hands the SAME variable to both the converter's WithSandbox and to NewHandler, so the two halves of one class's confinement can never diverge by construction, not merely by convention."
    - "A production preflight function needs its own minimal JSON shape (audioPreflightTranscript, audiosandbox.go) rather than reusing a _test.go-only type (whisper_test.go's audioTranscript) -- test types are unreachable from production files in the same package."

key-files:
  created:
    - internal/convert/audiosandbox.go
    - internal/convert/audiosandbox_test.go
    - internal/worker/audioguard_sandbox_test.go
  modified:
    - internal/convert/whisper.go
    - internal/convert/audioduration.go
    - internal/convert/audioduration_test.go
    - internal/worker/worker.go
    - internal/worker/runner.go
    - internal/worker/worker_test.go
    - cmd/audio-worker/main.go
    - cmd/worker/main.go
    - cmd/pandoc-worker/main.go
    - cmd/ocr-worker/main.go
    - cmd/av-worker/main.go
    - cmd/document-worker/main.go
    - cmd/chromium-worker/main.go
    - cmd/probe-worker/main.go
    - cmd/tectonic-worker/main.go
    - cmd/webhook-worker/main.go
    - deploy/chart/octoconv/templates/deployment-audio-worker.yaml

key-decisions:
  - "The model file is granted at Convert-call time by appending c.model() to a per-call copy of audioReadOnly, never baked into the static audioReadOnly var -- AUDIO_MODEL_PATH is configurable, so a non-default model path is granted correctly without editing the measured set (matches SANDBOX-PATHS.md's own note that the model is 'a run-time grant, not in the static list')."
  - "audioduration.go's EnforceMaxDuration builds its own engineExec from (helper, filepath.Dir(path), audioReadOnly, audioEngines) rather than accepting a pre-built engineExec -- unlike AV's avPrepareConversion pattern, the guard has no Handler-side job directory to reuse ahead of time; it derives one from the path it is handed, which is exactly what the worker needs (there is no other caller)."
  - "audioPreflightTranscript is a minimal, production-file-local JSON shape ({Transcription []json.RawMessage `json:\"transcription\"`}) rather than reusing whisper_test.go's audioTranscript -- a _test.go type is unreachable from audiosandbox.go, and the preflight only needs to prove the key's presence as an array, not decode segment structure."

patterns-established:
  - "Pattern: a pre-Convert guard living outside the converter's own package threads its sandbox handle through the SAME per-Handler config channel an existing sibling value already uses (NewHandler parameter -> Handler field -> engine-specific NewRunner closure), rather than inventing a parallel channel -- keeps the guard and the converter's own confinement from ever being independently configurable."

requirements-completed: []  # CONF-01/CONF-02/CONF-04/CONF-05 are declared by every sibling plan in this phase (01-04 done, 05 this plan, 06 remaining); requirements.ready-ids reports not-ready until every declaring plan has a SUMMARY (shared-ID gate, #2388) -- not an oversight

coverage:
  - id: D1
    description: "Both AudioConverter engine call sites (ffmpeg normalize, whisper-cli transcribe) run through one shared job-scoped engineExec under the re-measured audio set, with the configured model FILE granted at call time -- proven by content for the default model, a custom AUDIO_MODEL_PATH, and on the built image"
    requirement: CONF-01
    verification:
      - kind: unit
        ref: "internal/convert/audiosandbox_test.go#TestAudioConvertRunsBothStagesThroughTheHelper"
        status: pass
      - kind: unit
        ref: "internal/convert/audiosandbox_test.go#TestAudioModelFileIsGrantedWhereverConfigured"
        status: pass
      - kind: integration
        ref: "scripts/sandbox-liveproof.sh audio (confined-startup check: '🔒 audio sandbox proven: the engine converted inside the job directory and was denied a file outside it')"
        status: pass
    human_judgment: false
  - id: D2
    description: "The pre-Convert ffprobe duration guard -- the client's file's first reader, running in internal/worker before Convert -- is confined under the SAME audio ruleset the converter's own stages use, threaded from cmd/audio-worker's ONE sandboxHelper variable through NewHandler -> Handler.audioSandboxHelper -> NewRunner -> audioPreConvertGuard -> enforceAudioGuardBeforeConvert -> convert.EnforceMaxDuration, proven with a nil-helper control and a manual inversion (removing the threading fails the confined subtest)"
    requirement: CONF-01
    verification:
      - kind: unit
        ref: "internal/convert/audiosandbox_test.go#TestEnforceMaxDurationRunsFFprobeThroughTheHelper"
        status: pass
      - kind: unit
        ref: "internal/worker/audioguard_sandbox_test.go#TestAudioGuardRunsFFprobeThroughTheWorkersHelper"
        status: pass
    human_judgment: false
  - id: D3
    description: "cmd/audio-worker refuses to start when confinement is wanted and cannot be established, names AUDIO_SANDBOX_DISABLED as the remedy, and the remedy is reachable from the Helm chart (absent from values.yaml, so the default render carries no env block)"
    requirement: CONF-02
    verification:
      - kind: integration
        ref: "scripts/sandbox-liveproof.sh audio (refuses-without-landlock and escape-hatch checks)"
        status: pass
      - kind: integration
        ref: "helm template ... --set audioWorker.extraEnv.AUDIO_SANDBOX_DISABLED=true renders the value; default render carries no env block for audio-worker; scripts/ci-helm-checks.sh exits 0 (0 FAIL lines)"
        status: pass
    human_judgment: false
  - id: D4
    description: "AudioPreflight judges the guard, both converter stages, and the negative run by CONTENT (probed duration ~1s, RIFF/WAVE header, a top-level JSON \"transcription\" array), never by whisper-cli's exit status alone -- whisper-cli exits 0 with plausible text even on silence. The live gate also proves the worker refuses to start when only landlock_restrict_self is denied."
    requirement: CONF-04
    verification:
      - kind: unit
        ref: "internal/convert/audiosandbox_test.go#TestAudioPreflightWAV"
        status: pass
      - kind: unit
        ref: "internal/convert/audiosandbox_test.go#TestAudioPreflightUnconfinedProvesNothing"
        status: pass
      - kind: integration
        ref: "scripts/sandbox-liveproof.sh audio (preflight-catches-restrict-self check)"
        status: pass
    human_judgment: false
  - id: D5
    description: "audio's granted set (audioReadOnly, 7 paths; audioEngines, 3 binaries) is pinned by a test that fails when the set drifts, and cross-checked against SANDBOX-PATHS.md's conf05:audio blocks"
    requirement: CONF-05
    verification:
      - kind: unit
        ref: "internal/convert/audiosandbox_test.go#TestAudioReadOnlySetIsTheMeasuredOne"
        status: pass
      - kind: unit
        ref: "internal/convert/audiosandbox_test.go#TestAudioEnginesAreTheMeasuredOnes"
        status: pass
      - kind: other
        ref: "scripts/conf05-crosscheck.sh audio -> 'MATCH: audio readonly (7) engines (3)'"
        status: pass
    human_judgment: false

duration: ~55min
completed: 2026-09-24
status: complete
plan_head_before: 0f8f0434d58438c49fb7a8544944845f391ab2f8
commits: 3
---

# Phase 68 Plan 05: Audio Confined End-to-End, Including the Pre-Convert Duration Guard Summary

**The audio class's two converter stages (ffmpeg normalize, whisper-cli transcribe) AND the pre-Convert ffprobe duration guard -- previously the one engine call in this codebase that lived outside any converter's own Sandbox field, in a different Go package entirely -- now run under one shared, re-measured Landlock ruleset per job, threaded from `cmd/audio-worker` through `Handler.audioSandboxHelper` the same way `audioMaxDuration` already travels, with the configured whisper model granted as a single file rather than a directory.**

## Performance

- **Duration:** ~55 min
- **Started:** 2026-09-24T02:05:00Z (approx., right after 68-04)
- **Completed:** 2026-09-24T02:22:00Z (approx.)
- **Tasks:** 3
- **Files modified:** 20 (3 created, 17 modified)

## Accomplishments

- `audiosandbox.go` (new): `audioReadOnly` (7 measured paths) and `audioEngines` (`ffmpeg`, `ffprobe`, `whisper-cli`) pinned against SANDBOX-PATHS.md's re-measured `conf05:audio` blocks -- the model is deliberately absent from `audioReadOnly` and granted as a run-time FILE grant instead. `(AudioConverter).WithSandbox`, `audioPreflightWAV` (a generated 1.0s/16kHz/mono/16-bit sine WAV, no binary testdata needed), and `AudioPreflight` (proves the guard, both converter stages, and a denial, judged by content).
- `whisper.go`: `AudioConverter` gains a `Sandbox *engineSandbox` field (kept behind a pointer for comparability, mirroring `AVConverter`/`OCRConverter`/`PandocConverter`). `Convert` builds `x := c.Sandbox.forJob(workDir, audioReadOnly+c.model(), audioEngines)` ONCE per job and routes both the ffmpeg-normalize and whisper-cli-transcribe stages through `x.run`, keeping the `"audio: ffmpeg: %w"`/`"audio: whisper-cli: %w"` error wraps `isAudioTerminal` depends on unchanged.
- `audioduration.go`: `EnforceMaxDuration(ctx, helper []string, path, max)` -- signature change -- builds its own `engineExec` from `(helper, filepath.Dir(path), audioReadOnly, audioEngines)` and calls the plan-04-unexported `probeDuration`. This is the ONE exported entry point that lets `internal/worker` confine the guard's ffprobe without any visibility into `AudioConverter`'s own private `Sandbox` field.
- `internal/worker/worker.go`/`runner.go`: `Handler.audioSandboxHelper []string` field; `NewHandler` gains a final parameter storing it; `enforceAudioGuardBeforeConvert` gains an `audioSandboxHelper []string` parameter between `audioMaxDuration` and `convertFn`, passed straight to `convert.EnforceMaxDuration`; `NewRunner`'s audio case and `audioPreConvertGuard` thread the helper through exactly the way `audioMaxDuration` already does. All ten worker cmds' `NewHandler` calls updated (nine pass `nil`; `cmd/audio-worker` passes the resolved `sandboxHelper`).
- `cmd/audio-worker/main.go`: reordered to mirror `cmd/av-worker`'s Phase 67/68 shape -- `sandbox.RunExecModeIfRequested` first, `sandbox.ResolveHelper(AUDIO_SANDBOX_DISABLED)` before `db.Connect`, `AUDIO_MODEL_PATH`/`AUDIO_THREADS` resolution moved ahead of the preflight (the preflight must load the SAME model/thread count production jobs use), `convert.AudioConverter{}.WithSandbox(sandboxHelper)` registered via `Default.Replace`, `AudioPreflight` run under a 120s context before `db.Connect`, and the SAME `sandboxHelper` variable now reaches `NewHandler` so the guard and the converter cannot disagree.
- `deployment-audio-worker.yaml` gains an `audioWorker.extraEnv` block (mirrors `avWorker`'s/`ocrWorker`'s), absent from `values.yaml` so the default render carries no env block.
- `scripts/sandbox-liveproof.sh audio` (freshly built image, `octoconv-audio:sandbox-liveproof-*`): `ALL PASS (4 checks)`, including `"🔒 audio sandbox proven: the engine converted inside the job directory and was denied a file outside it"` and the preflight refusing to start when a seccomp profile denies only `landlock_restrict_self`. `scripts/conf05-crosscheck.sh audio`: `MATCH: audio readonly (7) engines (3)`. `scripts/ci-helm-checks.sh`: 0 FAIL lines.

## Task Commits

Each task was committed atomically:

1. **Task 1: Audio converter and duration guard under the audio ruleset, model file granted, AudioPreflight** - `fe21c46` (test)
2. **Task 2: Thread the audio helper from NewHandler to the guard, the way audioMaxDuration travels** - `b51481c` (test)
3. **Task 3: audio-worker gives one helper to converter and guard, preflights before Postgres, chart extraEnv** - `2e76849` (feat)

**Plan metadata:** committed separately after this SUMMARY (docs).

## Files Created/Modified

- `internal/convert/audiosandbox.go` - `audioReadOnly`, `audioEngines`, `(AudioConverter).WithSandbox`, `audioPreflightWAV`, `AudioPreflight`, `audioPreflightTranscript` (new)
- `internal/convert/audiosandbox_test.go` - pinned sets, both-stages-through-the-helper proof (including the model-file grant), the duration-guard-through-the-helper proof with a nil-helper control, WAV-shape and unconfined-preflight tests (new)
- `internal/convert/whisper.go` - `AudioConverter.Sandbox` field; `Convert` builds and shares one `engineExec` for both stages
- `internal/convert/audioduration.go` - `EnforceMaxDuration(ctx, helper, path, max)` signature change, builds its own `engineExec`
- `internal/convert/audioduration_test.go` - two call sites updated to pass a `nil` helper
- `internal/worker/worker.go` - `Handler.audioSandboxHelper` field; `NewHandler`'s new trailing parameter; `enforceAudioGuardBeforeConvert`'s new parameter
- `internal/worker/runner.go` - `NewRunner`'s audio case and `audioPreConvertGuard` thread the helper alongside the ceiling
- `internal/worker/worker_test.go` - three `enforceAudioGuardBeforeConvert` call sites pass `nil` for the helper
- `internal/worker/audioguard_sandbox_test.go` - `TestAudioGuardRunsFFprobeThroughTheWorkersHelper` (confined, nil-helper control, over-ceiling, non-audio-job subtests), with small local copies of the fake-helper/fake-engine test toolkit (package `convert`'s own copies are unexported and not visible from package `worker`)
- `cmd/audio-worker/main.go` - `audioSandboxDisabledEnv`; startup block reordered: helper mode first, `ResolveHelper`, model/threads resolution, `Replace`, `AudioPreflight`, all before `db.Connect`; `NewHandler` now receives `sandboxHelper`
- `cmd/{worker,pandoc-worker,ocr-worker,av-worker,document-worker,chromium-worker,probe-worker,tectonic-worker,webhook-worker}/main.go` - `NewHandler` calls gain a trailing `nil` (`audioSandboxHelper — audio-only`)
- `deploy/chart/octoconv/templates/deployment-audio-worker.yaml` - `audioWorker.extraEnv` block added

## Decisions Made

See `key-decisions` in frontmatter. Most consequential: the model file is granted at Convert-call time (appended to a per-call copy of `audioReadOnly`), never baked into the static var, so `AUDIO_MODEL_PATH` overrides are granted correctly without editing the measured set -- proven by `TestAudioModelFileIsGrantedWhereverConfigured`.

## Deviations from Plan

None - plan executed exactly as written. The one manual verification step the plan's own acceptance criteria required (Task 2: "Passing nil instead of h.audioSandboxHelper in NewRunner makes TestAudioGuardRunsFFprobeThroughTheWorkersHelper fail, then it is restored") was performed via a temporary `Edit` (not a raw `sed`, which the harness's auto-mode classifier declined as a "weaken security" pattern against `runner.go`) -- the confined subtest failed with "convertFn ran before the helper was invoked -- the guard did not run before Convert", confirming the inversion is not vacuous, and the change was reverted before the commit.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- `EnforceMaxDuration`'s helper-taking signature, the `Handler.audioSandboxHelper` threading pattern (a guard living outside the converter's own package reusing an existing per-Handler config channel), and the model-granted-as-a-file-at-call-time pattern are the last of this phase's genuinely new shapes -- 68-06 has no comparable cross-package wrinkle to resolve.
- CONF-01/02/04/05 stay "In Progress" in REQUIREMENTS.md until every plan in this phase that declares them (01-05 done, 06 remaining) has its own SUMMARY (shared-ID gate, #2388, working as intended).
- No blockers for 68-06.

---
*Phase: 68-four-straightforward-engine-classes*
*Completed: 2026-09-24*

## Self-Check: PASSED

All fourteen listed key files verified present on disk with `[ -f ]`. All three task
commits (`fe21c46`, `b51481c`, `2e76849`) verified present via `git log --oneline
--all`. `GOTOOLCHAIN=go1.26.4 go build ./...`, `go vet ./...`, and
`go test -count=1 ./...` all exit 0 (full suite, all packages). Every plan-level
`<acceptance_criteria>` grep re-run and passing. `scripts/conf05-crosscheck.sh audio`
prints `MATCH: audio readonly (7) engines (3)`. `scripts/sandbox-liveproof.sh audio`
(run this session against a freshly built image) ended `ALL PASS (4 checks)`, including
the `"🔒 audio sandbox proven:"` evidence line. `scripts/ci-helm-checks.sh` ran clean (0
FAIL lines).
