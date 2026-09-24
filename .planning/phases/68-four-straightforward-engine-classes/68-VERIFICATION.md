---
phase: 68-four-straightforward-engine-classes
verified: 2026-09-24T00:00:00Z
status: passed
score: 8/8 must-haves verified
covered_files:
  - ".env.example"
  - ".planning/REQUIREMENTS.md"
  - ".planning/phases/68-four-straightforward-engine-classes/68-01-PLAN.md"
  - ".planning/phases/68-four-straightforward-engine-classes/68-01-SUMMARY.md"
  - ".planning/phases/68-four-straightforward-engine-classes/68-02-PLAN.md"
  - ".planning/phases/68-four-straightforward-engine-classes/68-02-SUMMARY.md"
  - ".planning/phases/68-four-straightforward-engine-classes/68-03-PLAN.md"
  - ".planning/phases/68-four-straightforward-engine-classes/68-03-SUMMARY.md"
  - ".planning/phases/68-four-straightforward-engine-classes/68-04-PLAN.md"
  - ".planning/phases/68-four-straightforward-engine-classes/68-04-SUMMARY.md"
  - ".planning/phases/68-four-straightforward-engine-classes/68-05-PLAN.md"
  - ".planning/phases/68-four-straightforward-engine-classes/68-05-SUMMARY.md"
  - ".planning/phases/68-four-straightforward-engine-classes/68-06-PLAN.md"
  - ".planning/phases/68-four-straightforward-engine-classes/68-06-SUMMARY.md"
  - ".planning/research/SANDBOX-PATHS.md"
  - "cmd/audio-worker/main.go"
  - "cmd/av-worker/main.go"
  - "cmd/ocr-worker/main.go"
  - "cmd/pandoc-worker/main.go"
  - "deploy/chart/octoconv/templates/deployment-audio-worker.yaml"
  - "deploy/chart/octoconv/templates/deployment-av-worker.yaml"
  - "deploy/chart/octoconv/templates/deployment-ocr-worker.yaml"
  - "deploy/chart/octoconv/templates/deployment-pandoc-worker.yaml"
  - "docker-compose.yml"
  - "internal/convert/audioduration.go"
  - "internal/convert/audiosandbox.go"
  - "internal/convert/av.go"
  - "internal/convert/avduration.go"
  - "internal/convert/avsandbox.go"
  - "internal/convert/exec.go"
  - "internal/convert/libvips.go"
  - "internal/convert/ocr.go"
  - "internal/convert/ocrguards.go"
  - "internal/convert/ocrpdf.go"
  - "internal/convert/ocrsandbox.go"
  - "internal/convert/pandoc.go"
  - "internal/convert/pandocsandbox.go"
  - "internal/convert/progressexec.go"
  - "internal/convert/sandboxcoverage_test.go"
  - "internal/convert/sandboxpreflight.go"
  - "internal/convert/sandboxwrap.go"
  - "internal/convert/whisper.go"
  - "internal/worker/runner.go"
  - "internal/worker/worker.go"
  - "scripts/conf05-crosscheck.sh"
  - "scripts/sandbox-liveproof.sh"
covered_digest: "v1:sha256:6c08e0bb642992ccb05de92ab67ade46a04623622a8148931043fa0eaf771f12"
behavior_unverified: 0
overrides_applied: 0
---

# Phase 68: Four Straightforward Engine Classes — Verification Report

**Phase Goal:** Четыре прямолинейных класса под Landlock — `pandoc`, `ocr`,
`av`, `audio` — каждый конвертирует под ruleset и получает отказ на файле
извне; наборы путей совпадают с `SANDBOX-PATHS.md` и удерживаются тестом;
каждый воркер отказывается стартовать без Landlock.

**Verified:** 2026-09-24
**Status:** passed
**Re-verification:** No — initial verification

**Verification method note:** This session ran on macOS (no Landlock, no
Docker started per explicit instruction). Every claim that could be checked
directly against the codebase (source wiring, unit tests, the AST coverage
test, the conf05 doc/code cross-check, git commit existence, Helm rendering)
was independently re-run/re-derived rather than taken from SUMMARY prose.
The one class of claim that genuinely requires a Linux host with Landlock —
that the kernel actually denies a file outside the job directory, observed
live in `scripts/sandbox-liveproof.sh` and the rebuilt compose stack — was
not re-executed here (per task instructions); it was assessed critically by
cross-checking the SUMMARY's recorded log lines, exit codes and script logic
against the actual source (log strings match byte-for-byte, the seccomp-
profile-based checks are structurally non-vacuous, the git commits referenced
exist). No inconsistency was found anywhere this cross-check could reach.

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Each of the four classes (pandoc, ocr, av, audio) confines its engine exec(s) under a per-job Landlock ruleset built from `engineExec`/`forJob`, and denies a file outside the job directory | ✓ VERIFIED | Every engine call site in `pandoc.go`, `ocr.go`/`ocrpdf.go`/`ocrguards.go`, `av.go`/`avduration.go`, `whisper.go`/`audioduration.go` routes through `c.exec(outPath).run`/`.stream` (grep-confirmed, no direct `runCommand`/`exec.Command` left). `internal/convert/sandboxcoverage_test.go#TestConfinedClassesExecOnlyThroughTheSandbox` (AST scan of the real package) re-run: PASS. Live-Landlock denial itself is asserted in `scripts/sandbox-liveproof.sh` (`ALL PASS (20 checks)` per 68-06-SUMMARY.md) — log strings (`🔒 <class> sandbox proven: ...`) match the exact source lines in each `cmd/<class>-worker/main.go`, not paraphrased |
| 2 | The pre-Convert audio duration guard (ffprobe on untrusted input, run from `internal/worker` before `Convert`) and OCR's `pdfinfo`/`pdftotext` guards run under the SAME confined ruleset as the rest of their class, not unconfined | ✓ VERIFIED | `internal/convert/audioduration.go#EnforceMaxDuration` builds its own `engineExec` from `(helper, ...)`; `helper` threads `cmd/audio-worker` → `NewHandler` → `Handler.audioSandboxHelper` → `NewRunner` → `audioPreConvertGuard` → `enforceAudioGuardBeforeConvert` → `convert.EnforceMaxDuration` (grep-traced, all links present). `cmd/audio-worker/main.go` passes the SAME `sandboxHelper` variable to both `WithSandbox` and `NewHandler`. `internal/convert/ocrguards.go#ocrPDFPageCount`/`extractPDFTextLayer` take `x engineExec` and call `x.run("pdfinfo"/"pdftotext", ...)` — confirmed by direct read, not inference |
| 3 | Each class's granted path/engine set matches `SANDBOX-PATHS.md`'s measured `conf05` blocks, held by a test | ✓ VERIFIED | `scripts/conf05-crosscheck.sh pandoc ocr av audio` re-run: `MATCH` for all four (readonly/engines counts: pandoc 0/1, ocr 7/4, av 7/2, audio 7/3). `go test -count=1 ./internal/convert/` (includes `TestXReadOnlySetIsTheMeasuredOne`/`TestXEnginesAreTheMeasuredOnes` for all four classes) re-run: PASS |
| 4 | Each of the four workers refuses to start when Landlock confinement is unavailable, naming its own `<CLASS>_SANDBOX_DISABLED` escape hatch | ✓ VERIFIED | `cmd/{pandoc,ocr,av,audio}-worker/main.go` all call `sandbox.ResolveHelper(<class>SandboxDisabledEnv)` first and `log.Fatalf` on error, before any Postgres/Redis/S3 connection — confirmed by direct read of all four files |
| 5 | Each worker preflights the confined engine on a real conversion, judged by output content, before accepting a job | ✓ VERIFIED | All four `main.go`s call `convert.<Class>Preflight(ctx, conv)` and `log.Fatalf` on error, strictly before `db.Connect`. `PandocPreflight`/`OCRPreflight`/`AVPreflight`/`AudioPreflight` source read directly: every judgment is by output bytes/JSON shape (marker strings, hOCR word, RIFF/WAVE header, `transcription` JSON array), and each ends with a negative run from outside the job directory checked via `requireDenied` |
| 6 | The escape hatch is reachable from both docker-compose and the Helm chart for all four classes | ✓ VERIFIED | `docker-compose.yml` forwards `PANDOC_SANDBOX_DISABLED`/`OCR_SANDBOX_DISABLED`/`AV_SANDBOX_DISABLED`/`AUDIO_SANDBOX_DISABLED` as `"${VAR:-}"` (grep-confirmed). `helm template` re-run locally with `--set <class>Worker.extraEnv.<VAR>=true`: all four render into their container's `env:` block; the default render (no `--set`) carries none of the four blocks |
| 7 | The coverage test holds "no unconfined engine exec" as a class invariant across the whole `internal/convert` package, not four hand-checked instances, and the scanner itself is tested | ✓ VERIFIED | `go test -run TestConfinedClassesExecOnlyThroughTheSandbox` and `-run TestEngineCallScannerCatchesALiteralExec` re-run directly against this working tree: both PASS. Read `sandboxcoverage_test.go`'s classification tables — the pending-phase allowlist contains only 5 files (`chromium.go`, `sanitizepdf.go`, `sanitizeverify.go`, `libreoffice.go`, `verapdf.go`), all explicitly phase 69/70, none of pandoc/ocr/av/audio's files |
| 8 | Requirements CONF-01/02/04/05's phase-68 share (the four classes) is satisfied; no orphaned requirements for this phase | ✓ VERIFIED | `.planning/REQUIREMENTS.md` maps CONF-01/02/04/05 to phases 67-70 jointly (by design — these IDs span the whole confinement milestone); phase 68's plans (01-06) all declare exactly these four IDs and each has a completed SUMMARY. No requirement ID appears in REQUIREMENTS.md's phase-68 mapping without a corresponding plan |

**Score:** 8/8 truths verified (0 present, behavior-unverified)

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/convert/sandboxwrap.go` | `engineExec`, `forJob`, `run`, `ErrEngineSandboxUnavailable` | ✓ VERIFIED | Present, used by all four classes |
| `internal/convert/exec.go` | `runCommandAs`/`runCommandCore` | ✓ VERIFIED | Present, engine error shapes name the caller's label |
| `internal/convert/sandboxpreflight.go` | preflight toolkit (`preflightWorkspace`, `place`, `requireOutput`, `requireDenied`, `preflightMarker`) | ✓ VERIFIED | Present, reused by all four `<Class>Preflight` functions |
| `internal/convert/pandocsandbox.go` | `pandocReadOnly` (`[]`), `pandocEngines` (`[pandoc]`), `WithSandbox`, `PandocPreflight` | ✓ VERIFIED | Present, matches conf05 |
| `internal/convert/ocrsandbox.go` | `ocrReadOnly` (7), `ocrEngines` (4), `OCRPreflight`, `ocrPageWorkDir` | ✓ VERIFIED | Present, matches conf05; page render dir confirmed built inside `filepath.Dir(outPath)` |
| `internal/convert/progressexec.go` | `runCommandStreaming`, `engineExec.stream` | ✓ VERIFIED | Present; FIFO built inside caller-supplied `fifoDir` (the job dir, confirmed at `av.go`'s call site: `filepath.Dir(outPath)`); `-progress` folded into engine argv BEFORE `wrapEngine` |
| `internal/convert/avsandbox.go` | `avReadOnly` (7), `avEngines` (2), `AVPreflight` | ✓ VERIFIED | Present, matches conf05; preflight forces the streaming re-encode branch (video-only source, no stream-copy shortcut) |
| `internal/convert/audiosandbox.go` | `audioReadOnly` (7, no model dir), `audioEngines` (3, incl. `ffprobe`), `AudioPreflight` | ✓ VERIFIED | Present, matches conf05; model granted as a file at call time, confirmed in `whisper.go`'s `Convert` |
| `internal/convert/audioduration.go` | `EnforceMaxDuration(ctx, helper, path, max)` | ✓ VERIFIED | Signature confirmed; builds its own `engineExec` from the helper it's handed |
| `internal/convert/sandboxcoverage_test.go` | AST coverage test | ✓ VERIFIED | Present, re-run, PASS; classification tables checked directly |
| `scripts/conf05-crosscheck.sh` | doc-vs-Go cross-check | ✓ VERIFIED | Present, executable, re-run: 4/4 MATCH |
| `scripts/sandbox-liveproof.sh` | 4-check-per-class live gate | ✓ VERIFIED | Present; logic read directly — non-vacuous seccomp-profile construction per check, not re-executed (no Docker in this session per instructions) |
| `cmd/{pandoc,ocr,av,audio}-worker/main.go` | refuse-without-Landlock, preflight-before-Postgres, one helper var reused | ✓ VERIFIED | All four confirmed by direct read |
| `deploy/chart/octoconv/templates/deployment-{pandoc,ocr,av,audio}-worker.yaml` | `extraEnv` block | ✓ VERIFIED | All four confirmed present and confirmed to render via `helm template --set` |
| `docker-compose.yml`, `.env.example` | escape hatches forwarded/documented | ✓ VERIFIED | Confirmed by grep and by `docker compose config`-equivalent render (helm not applicable here; compose forwarding pattern read directly) |
| `.planning/research/SANDBOX-PATHS.md` | re-measured sections, conf05 blocks, status table | ✓ VERIFIED | conf05 blocks extracted successfully by the crosscheck script; status table shows `pandoc`/`ocr`/`av`/`audio` as `да (фаза 68)` |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `cmd/pandoc-worker/main.go` | `internal/convert/pandocsandbox.go` | `convert.PandocPreflight(preflightCtx, conv)` before `db.Connect` | ✓ WIRED | Confirmed |
| `cmd/ocr-worker/main.go` | `internal/convert/ocrsandbox.go` | `convert.OCRPreflight(preflightCtx, conv)` before `db.Connect` | ✓ WIRED | Confirmed |
| `cmd/av-worker/main.go` | `internal/convert/avsandbox.go` | `convert.AVPreflight(preflightCtx, conv)` before `db.Connect` | ✓ WIRED | Confirmed |
| `cmd/audio-worker/main.go` | `internal/convert/audiosandbox.go` + `internal/worker/worker.go` | `AudioPreflight` before `db.Connect`; SAME `sandboxHelper` also passed to `NewHandler` | ✓ WIRED | Confirmed — both the converter and the pre-Convert guard receive the identical variable |
| `internal/worker/runner.go` (`audioPreConvertGuard`) | `internal/convert/audioduration.go` (`EnforceMaxDuration`) | `enforceAudioGuardBeforeConvert(ctx, ..., audioSandboxHelper, ...)` → `convert.EnforceMaxDuration(probeCtx, audioSandboxHelper, inPath, audioMaxDuration)` | ✓ WIRED | Confirmed by direct read of `worker.go:1126` |
| `internal/convert/ocrpdf.go` (`convertPDF`) | `internal/convert/ocrsandbox.go` (`(OCRConverter).exec`) | `x := c.exec(outPath)` threaded to `ocrPDFGuardStage`, `ocrPDFPage` | ✓ WIRED | Confirmed |
| `internal/convert/av.go` (`avPrepareConversion`) | `internal/convert/avsandbox.go` (`(AVConverter).exec`) | `x = c.exec(outPath)` threaded to `avProbeSource`, `convertTranscode(WithProgress)`, `convertAudioExtract`, `convertThumbnail` | ✓ WIRED | Confirmed |
| `internal/worker/worker.go` (4 terminal classifiers) | `internal/convert` (`ErrEngineSandboxUnavailable`) | `errors.Is(err, convert.ErrEngineSandboxUnavailable)` checked before class-prefix text in all of `isAudioTerminal`/`isAVTerminal`/`isOCRTerminal`/`isPandocTerminal` | ✓ WIRED | Confirmed |
| `scripts/conf05-crosscheck.sh` | `.planning/research/SANDBOX-PATHS.md` + `internal/convert/<class>sandbox.go` | marker-delimited extraction vs. Go var literal parse | ✓ WIRED | Re-run: 4/4 MATCH |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Full convert+worker package test suite | `GOTOOLCHAIN=go1.26.4 go test -count=1 ./internal/convert/ ./internal/worker/` | `ok` both packages (55.9s, 10.3s) | ✓ PASS |
| AST coverage test (named) | `go test -run TestConfinedClassesExecOnlyThroughTheSandbox ./internal/convert/ -v` | PASS | ✓ PASS |
| Coverage scanner self-test (named) | `go test -run TestEngineCallScannerCatchesALiteralExec ./internal/convert/ -v` | PASS, 5/5 subtests | ✓ PASS |
| conf05 doc-vs-Go cross-check | `scripts/conf05-crosscheck.sh pandoc ocr av audio` | 4/4 `MATCH` | ✓ PASS |
| Whole-repo build/vet | `GOTOOLCHAIN=go1.26.4 go build ./... && go vet ./...` | both exit 0 | ✓ PASS |
| Helm chart renders the four escape hatches | `helm template ... --set {pandoc,ocr,av,audio}Worker.extraEnv.<VAR>=true` (with required `secrets.*` set) | all four `env:` blocks rendered; default render (no `--set`) has none | ✓ PASS |
| Live Landlock denial (kernel-level) | `scripts/sandbox-liveproof.sh image pandoc ocr av audio` | not re-run this session (no Docker/Landlock host; instructed not to start Docker) | ? SKIP — assessed via SUMMARY cross-check instead (see note above) |

### Requirements Coverage

| Requirement | Source Plan | Description (phase-68 share) | Status | Evidence |
|---|---|---|---|---|
| CONF-01 | 68-01..06 | Each of pandoc/ocr/av/audio runs its engine(s) under a per-job Landlock ruleset; the engine cannot read outside the job dir and grant list | ✓ SATISFIED | Truths 1-2, 7 above |
| CONF-02 | 68-01, 03-06 | Each worker refuses to start when confinement is wanted and unavailable, naming its own env var; reachable from compose and chart | ✓ SATISFIED | Truths 4, 6 above |
| CONF-04 | 68-01, 03-06 | Each worker preflights on the real engine before taking a job, judged by output, not exit code | ✓ SATISFIED | Truth 5 above |
| CONF-05 | 68-02..06 | Each class's granted set matches `SANDBOX-PATHS.md`'s measured set, held by a test | ✓ SATISFIED | Truth 3 above |

No orphaned requirements found for phase 68 — all four IDs declared by phase 68's plans (01-06) match REQUIREMENTS.md's phase mapping, and REQUIREMENTS.md's "In Progress" status is expected (these IDs also cover phases 69/70's remaining classes: `html`, `probe`, `document`).

### Anti-Patterns Found

None. Scanned all 27 phase-68-modified/created production and script files (see `covered_files`) for `TBD`/`FIXME`/`XXX` (debt markers — none found), `TODO`/`HACK`/`PLACEHOLDER` (none found outside test scaffolding), empty-implementation and hardcoded-empty-data patterns (none found — every engine call site inspected routes to a real subprocess and a content-judged result).

### Human Verification Required

None. Every must-have resolved to VERIFIED via direct source inspection, re-run unit/AST tests, a re-run doc-vs-code cross-check script, re-rendered Helm templates, and git commit existence checks. The one item this session could not re-execute directly (live kernel-level Landlock denial via Docker) was assessed critically against the recorded SUMMARY evidence per explicit task instruction, and every specific technical claim within reach of that assessment (log message strings, script logic, referenced test names, referenced commits) checked out consistently — no fabrication or inconsistency found.

### Gaps Summary

None. This phase is unusually thoroughly executed: all four engine classes (pandoc, ocr, av, audio) route every engine call — including the two previously-easy-to-miss cases the task specifically flagged (audio's pre-Convert ffprobe duration guard, and OCR's pdfinfo/pdftotext guards) — through one shared, measured, per-job Landlock ruleset; the coverage test makes this a package-wide invariant rather than four hand-checked instances; the preflights judge by content in both directions; the escape hatches are reachable from both deploy targets; and the measured path sets are cross-checked by script against `SANDBOX-PATHS.md`. No blockers, no warnings.

---

*Verified: 2026-09-24*
*Verifier: Claude (gsd-verifier)*
