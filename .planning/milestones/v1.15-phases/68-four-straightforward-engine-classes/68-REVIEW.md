---
phase: 68-four-straightforward-engine-classes
reviewed: 2026-09-24T00:00:00Z
depth: standard
files_reviewed: 52
files_reviewed_list:
  - .env.example
  - cmd/audio-worker/main.go
  - cmd/av-worker/main.go
  - cmd/chromium-worker/main.go
  - cmd/document-worker/main.go
  - cmd/ocr-worker/main.go
  - cmd/pandoc-worker/main.go
  - cmd/probe-worker/main.go
  - cmd/tectonic-worker/main.go
  - cmd/webhook-worker/main.go
  - cmd/worker/main.go
  - deploy/chart/octoconv/templates/deployment-audio-worker.yaml
  - deploy/chart/octoconv/templates/deployment-av-worker.yaml
  - deploy/chart/octoconv/templates/deployment-ocr-worker.yaml
  - deploy/chart/octoconv/templates/deployment-pandoc-worker.yaml
  - docker-compose.yml
  - internal/convert/audioduration.go
  - internal/convert/audioduration_test.go
  - internal/convert/audiosandbox.go
  - internal/convert/audiosandbox_test.go
  - internal/convert/av.go
  - internal/convert/av_test.go
  - internal/convert/avduration.go
  - internal/convert/avduration_test.go
  - internal/convert/avsandbox.go
  - internal/convert/avsandbox_test.go
  - internal/convert/exec.go
  - internal/convert/libvips.go
  - internal/convert/ocr.go
  - internal/convert/ocrguards.go
  - internal/convert/ocrguards_test.go
  - internal/convert/ocrpdf.go
  - internal/convert/ocrsandbox.go
  - internal/convert/ocrsandbox_test.go
  - internal/convert/pandoc.go
  - internal/convert/pandocsandbox.go
  - internal/convert/pandocsandbox_test.go
  - internal/convert/pre59_ocr_pinning_test.go
  - internal/convert/progressexec.go
  - internal/convert/progressexec_test.go
  - internal/convert/sandboxcoverage_test.go
  - internal/convert/sandboxexec_test.go
  - internal/convert/sandboxpreflight.go
  - internal/convert/sandboxpreflight_test.go
  - internal/convert/sandboxwrap.go
  - internal/convert/whisper.go
  - internal/worker/audioguard_sandbox_test.go
  - internal/worker/engine_sandbox_retryable_test.go
  - internal/worker/runner.go
  - internal/worker/worker.go
  - internal/worker/worker_test.go
  - scripts/conf05-crosscheck.sh
  - scripts/sandbox-liveproof.sh
  - scripts/sandbox-paths-measure.sh
findings:
  critical: 0
  warning: 0
  info: 2
  total: 2
status: clean
---

# Phase 68: Code Review Report

**Reviewed:** 2026-09-24T00:00:00Z
**Depth:** standard (executed with cross-file, call-chain tracing given the security-critical nature of the phase)
**Files Reviewed:** 52
**Status:** clean

## Summary

Phase 68 confines pandoc, OCR (tesseract/pdftoppm/pdfinfo/pdftotext), AV (ffmpeg/ffprobe, including the progress-FIFO streaming path) and audio (ffmpeg/whisper-cli, including the pre-`Convert` ffprobe duration guard that lives in `internal/worker`, a different package from the converter) under the shared Landlock `engineExec`/`forJob`/preflight mechanism introduced in plan 01.

I traced every job-path engine call site across all four classes end to end:

- **No unconfined engine exec escapes the sandbox.** Every `runCommand*`/`exec.Command`/`exec.CommandContext` call in the four classes' files (`pandoc.go`, `ocr.go`/`ocrpdf.go`/`ocrguards.go`, `av.go`/`avduration.go`, `whisper.go`/`audioduration.go`) has been replaced by a call through `engineExec.run`/`.stream`, confirmed both by direct grep (zero literal low-level-runner calls remain in these files) and by the new AST+type-checked `sandboxcoverage_test.go`, which fails the build on any literal exec whose name isn't in its class's `<class>Engines` list, any low-level-runner call outside the mechanism itself, and any listed-but-never-exec'd engine (stale grant). The scanner's own self-test proves each violation shape actually fires.
- **The job directory is the only writable/extra-readable grant**, consistently, across all four classes: `forJob(filepath.Dir(outPath), ...)` for pandoc/OCR/AV, and `forJob(workDir, audioReadOnly+model, audioEngines)` for audio's `Convert`, `EnforceMaxDuration`'s own `forJob(filepath.Dir(path), ...)` for the pre-Convert guard, and the progress FIFO's directory (`os.MkdirTemp(fifoDir, ".progress-")`, `fifoDir` = the job directory) for AV's streaming path. I traced `internal/worker/worker.go`'s `process()` to confirm `inPath`/`outPath` are both inside the same `workDir`, and that the audio pre-Convert guard receives that same `inPath`, so the guard's `filepath.Dir(path)` grant and the converter's own job-directory grant can never diverge.
- **Sandbox node faults are classified retryable, not blamed on the client**, in all four `isXTerminal` classifiers (`isPandocTerminal`, `isOCRTerminal`, `isAVTerminal`, `isAudioTerminal`): each checks `errors.Is(err, convert.ErrEngineSandboxUnavailable)` first and returns `false` (transient) before any class-prefix/substring match, including matches that the sandbox fault's own wrapped text also carries (`"pandoc: pandoc:"`, `"ocr: tesseract:"`, `"audio: ffmpeg:"`, `ErrAVTranscodeFailed`). `engine_sandbox_retryable_test.go`'s non-vacuity control (the same wrapped shapes, without the sentinel, must still be terminal) is present and correct.
- **Every preflight judges by output content, never exit status alone** (`PandocPreflight`, `OCRPreflight`, `AVPreflight`, `AudioPreflight`, all built on the shared `sandboxpreflight.go` toolkit): sufficiency is `requireOutput` (non-empty + a per-run random marker), effectiveness is `requireDenied` (the identical bytes from a sibling directory outside the job dir must fail, and must leave no marker-carrying output behind). OCR's preflight fixture deliberately uses a non-embedded font to reproduce the exact silent-degradation trap (`pdftoppm`+`tesseract` both exit 0, empty text) that motivated granting `/usr/share/fonts`. AV's preflight fixture is deliberately video-only so the positive run is forced onto the full streaming re-encode branch (FIFO included), never the cheap stream-copy remux. Audio's preflight additionally proves the cross-package duration guard through the exact `EnforceMaxDuration` entry point `internal/worker` calls, and checks for a structured `"transcription"` JSON array rather than trusting `whisper-cli`'s exit 0 (which it returns even on silence).
- **The `*_SANDBOX_DISABLED` escape hatches are the only way to get an unconfined engine**, and none of the four workers can reach that state implicitly: `sandbox.ResolveHelper` (unchanged, phase 67) does an exact `== "true"` string match, so any typo/case variant fails closed (stays confined); `docker-compose.yml` forwards all four vars via `"${VAR:-}"` (unset by default); the four Helm `deployment-*-worker.yaml` templates gain `extraEnv` blocks absent from `values.yaml`, so a default render carries no such env at all. I checked for YAML anchors/shared env blocks that could leak one class's override into another's container — none exist; each service's env block is independent.
- **Shell script portability**: `scripts/conf05-crosscheck.sh`, `scripts/sandbox-liveproof.sh`, and `scripts/sandbox-paths-measure.sh` avoid GNU-only flags on the host side (the one GNU-ism risk, `awk -v close=...`, is explicitly worked around with a renamed variable and a comment explaining why BWK/one-true-awk on macOS would otherwise reject it). `sandbox-liveproof.sh` uses a background-watchdog-based substitute for `timeout(1)` (absent on macOS) and explicitly redirects the watchdog's own stdio so it cannot hold a `$(...)` capture open — a real bug from an earlier session in this same phase, already found and fixed (documented in the 68-01 SUMMARY) and correctly generalized to `check_confined_startup`'s own watchdog too.
- **No hardcoded secrets, debug artifacts, or empty catch blocks** were found via pattern scan across every changed file in this phase.

I did not find any BLOCKER or WARNING-tier defect in this phase's own diff. Two INFO-level observations follow, neither a functional or security defect.

## Info

### IN-01: `enforceMaxResolution` has zero production callers

**File:** `internal/convert/avduration.go:181`
**Issue:** `enforceMaxResolution(ctx, x engineExec, path string, maxHeight int)` is unexported and, by design (per its own doc comment, "it has no production caller"), is only exercised from `avduration_test.go`. This is a deliberate security decision carried over from plan 04 (T-68-20: unexporting the old `EnforceMaxResolution` removes an entry point that could run `ffprobe` on client input with no sandbox handle), not an oversight — but it does mean the function is dead code in the shipped binary, kept solely to give the underlying probe+enforce composition (`enforceMaxResolutionOf`) unit coverage independent of `avPrepareConversion`'s own integration test.
**Fix:** No action required for this phase. If a future phase never adds a caller, consider inlining the test coverage into `enforceMaxResolutionOf`'s own tests and removing the now-genuinely-unused wrapper, or explicitly note it as intentionally-retained API surface for a specific future caller.

### IN-02: `engineSandbox.Confine == false` (helper present, ruleset not applied) is unreachable for all four classes this phase confines

**File:** `internal/convert/sandboxwrap.go:127-141` (`forJob`), `internal/convert/pandocsandbox.go:35-38`, `internal/convert/ocrsandbox.go:79-82`, `internal/convert/avsandbox.go:57-60`, `internal/convert/audiosandbox.go:58-61` (each class's `WithSandbox`)
**Issue:** `forJob`'s doc comment describes a `Confine: false` branch ("run through the helper, restrict nothing... tectonic's own CPU-budget-without-filesystem-ruleset shape") as a real, load-bearing configuration. For pandoc/OCR/AV/audio, every `WithSandbox(helper)` call goes through `newEngineSandbox(helper)`, which sets `Confine: true` unconditionally whenever `len(helper) > 0` — there is no way to construct a `PandocConverter`/`OCRConverter`/`AVConverter`/`AudioConverter` in production with a non-empty helper and `Confine: false`. The branch exists only to serve `TectonicConverter`'s own (pre-existing, unrelated) mechanism, and is not covered by any of this phase's own tests for the four classes it introduces.
**Fix:** No action required — this is inherited shared-code generality, not a defect introduced by this phase, and the branch is exercised by `tectonic.go`'s own tests. Worth a one-line note in `forJob`'s doc comment (or a follow-up test) if a future phase ever wires a fifth confined class through a `Confine: false` configuration, so the first user of that path isn't also its first tester.

---

_Reviewed: 2026-09-24T00:00:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
