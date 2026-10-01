---
phase: 68-four-straightforward-engine-classes
verified: 2026-10-01T00:00:00Z
status: passed
score: 8/8 must-haves verified
behavior_unverified: 0
overrides_applied: 0
re_verification:
  previous_status: passed
  previous_score: 8/8
  gaps_closed: []
  gaps_remaining: []
  regressions: []
  reason: "verification.status read stale: phase 70 changed files this report covered"
covered_files:
  - ".env.example"
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
  - ".planning/REQUIREMENTS.md"
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
  - "internal/sandbox/execmode.go"
  - "internal/sandbox/sandbox_linux.go"
  - "internal/sandbox/sandbox.go"
  - "internal/worker/runner.go"
  - "internal/worker/worker.go"
  - "scripts/conf05-crosscheck.sh"
  - "scripts/sandbox-liveproof.sh"
covered_digest: "v2:sha256:77095dc059df9a56f1c5503045d92db073c4b9bd215cf90e9b07462d24a8815d"
---

# Phase 68: Four Straightforward Engine Classes — Verification Report

**Phase Goal:** Четыре прямолинейных класса под Landlock — `pandoc`, `ocr`,
`av`, `audio` — каждый конвертирует под ruleset и получает отказ на файле
извне; наборы путей совпадают с `SANDBOX-PATHS.md` и удерживаются тестом;
каждый воркер отказывается стартовать без Landlock.

**Verified:** 2026-10-01
**Status:** passed
**Re-verification:** Yes — against the current tree (HEAD `ebcacda`), because phase 70
changed files this report covered and `verification.status` read `stale`.

## Re-verification scope

The original report (2026-09-24, commit `7c37b30`, 8/8, passed) is carried forward
only where the current code still says the same thing. Every must-have was
re-checked against the current tree, and every change to a covered file since
`7c37b30` was read as a diff (not taken from phase 70's SUMMARYs).

Changes to phase-68 code since `7c37b30` (full `git diff 7c37b30 HEAD`, read):

| File | Change | Effect on phase 68 |
|------|--------|--------------------|
| `cmd/{pandoc,ocr,av,audio}-worker/main.go` (70-07) | preflight context derived from the signal `ctx` instead of `context.Background()` | Preflight still runs, still before `db.Connect`, still `log.Fatalf` on error; it now also ends on SIGINT/SIGTERM. Timeouts (30/60/60/120s) unchanged. No regression. |
| `internal/convert/sandboxwrap.go` (69/70) | adds `forJobRW`/`forJobRWSock`, `engineExec.env`/`withEnv`, `engineEnv`, `sharedTmpGuard`, `specWithSockets` | `forJob` is `forJobRW(jobDir, nil, ...)` and `specWithSockets(rw, nil, ro)` renders the same tokens as before; `run` takes the old `runCommandAs` branch when `e.env == nil`. None of the four phase-68 classes calls `withEnv`, `forJobRW*`, or `sharedTmpGuard` (grep of the 12 class files: zero hits). Unchanged behaviour. |
| `internal/convert/sandboxpreflight.go` (69) | adds `newPreflightWorkspaceOutside`, `requireOutsideGrants`, `requireAbsent`, `requireContains`, `requireLacks`; `requireDenied` marker branch delegates to `requireAbsent` | `newPreflightWorkspace` == `newPreflightWorkspaceOutside(class, "")` (same `os.TempDir()` default). `requireDenied` judgment for nil marker and marker cases is the same bytes check (missing file = clean denial; marker present = failure). |
| `internal/convert/progressexec.go` (69) | `engineExec.stream` refuses a non-nil per-exec env | The four phase-68 classes never set `env`; the only streaming class (av) is unaffected. |
| `internal/convert/ocrsandbox.go` | doc comment only | none |
| `internal/worker/worker.go` | `isDocumentTerminal` / `isHTMLTerminal` gain the `ErrEngineSandboxUnavailable` check | `isPandocTerminal`/`isOCRTerminal`/`isAVTerminal`/`isAudioTerminal` untouched; html/document now match the pattern phase 68 set. |
| `internal/sandbox/{sandbox,sandbox_linux,execmode}.go` (70-02) | new `Socket` grant kind (`sock:` token, `MAKE_SOCK` only) | Purely additive: empty `Socket` adds no tokens and no Landlock rules; `RunExecMode` gate widened to `len(ReadOnly)+len(ReadWrite)+len(Socket) > 0`, same outcome when `Socket` is empty. |
| `internal/convert/sandboxcoverage_test.go` (69/70) | `sandboxPendingFiles` emptied; html/probe/document classes added to the scan | pandoc/ocr/av/audio entries unchanged; the test now covers strictly more. |
| `docker-compose.yml`, `.env.example`, `scripts/*` | additions for later classes | The four `<CLASS>_SANDBOX_DISABLED` forwards are still present (see truth 6). |
| `.planning/research/SANDBOX-PATHS.md` | additions for html/probe/document | The four phase-68 `conf05` blocks still match the Go sets (truth 3). |

Files not in that list (`pandoc.go`, `ocr*.go` other than `ocrsandbox.go`, `av*.go`,
`audio*.go`, `whisper.go`, `exec.go`, `libvips.go`, `runner.go`,
`pandocsandbox.go`, `avsandbox.go`, `audiosandbox.go`) have no diff since `7c37b30`.

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Each of pandoc, ocr, av, audio confines its engine exec(s) under a per-job Landlock ruleset (`engineExec`/`forJob`) and denies a file outside the job directory | ✓ VERIFIED | The four class files have zero direct `runCommand`/`exec.Command` calls (grep: only comment mentions). `TestConfinedClassesExecOnlyThroughTheSandbox` (AST scan, re-run now, PASS) now checks seven groups including these four; `TestEngineCallScannerCatchesALiteralExec` PASS. Denial of a file outside the job directory is asserted by each `<Class>Preflight` and was observed live: `evidence/liveproof-8class-70-09.txt` (images built from `5db6c74`, which has no Go/script/Docker/chart diff against HEAD) — `PASS: {pandoc,ocr,av,audio} {confined-startup,refuses-without-landlock,escape-hatch,preflight-catches-restrict-self}`, `ALL PASS (40 checks)`, plus e2e `TestPandocConversionE2E`, `TestOCRConversionE2E`, `TestAudioConversionE2E` PASS in `e2e-70-09.txt` (no av-class e2e test appears in that transcript; av rests on its live gate checks and unit tests). |
| 2 | Audio's pre-Convert ffprobe duration guard and OCR's pdfinfo/pdftotext guards run under the same confined ruleset as their class | ✓ VERIFIED | Unchanged files (`audioduration.go`, `ocrguards.go`, `runner.go`); wiring `cmd/audio-worker` → `NewHandler` → `audioSandboxHelper` → `EnforceMaxDuration` still present; `cmd/audio-worker/main.go` passes the one `sandboxHelper` to both `WithSandbox` and `NewHandler`. The audio live preflight line `ffprobe: engine sandbox could not be established ... exit status 78` in the liveproof transcript shows the guard ffprobe goes through the helper. |
| 3 | Each class's granted path/engine set matches `SANDBOX-PATHS.md`'s measured `conf05` blocks, held by a test | ✓ VERIFIED | `scripts/conf05-crosscheck.sh pandoc ocr av audio` re-run: `MATCH: pandoc readonly (0) engines (1)`, `ocr (7)/(4)`, `av (7)/(2)`, `audio (7)/(3)` — identical to 2026-09-24. `Test{Pandoc,OCR,AV,Audio}{ReadOnlySetIsTheMeasuredOne,EnginesAreTheMeasuredOnes}` re-run, all PASS. Phase 70's own inversion (`evidence/inversions-70-09.txt`) shows the same mechanism failing red when a path is added. |
| 4 | Each of the four workers refuses to start when Landlock is unavailable, naming its own `<CLASS>_SANDBOX_DISABLED` | ✓ VERIFIED | `cmd/{pandoc,ocr,av,audio}-worker/main.go` each call `sandbox.ResolveHelper(<class>SandboxDisabledEnv)` and `log.Fatalf` before any connection (re-read). Live: `PASS: <class> refuses-without-landlock` for all four in the 40-check transcript. |
| 5 | Each worker preflights the confined engine on a real conversion, judged by output content, before accepting a job | ✓ VERIFIED | All four `main.go` call `convert.<Class>Preflight(preflightCtx, conv)` then `log.Fatalf`, strictly before `db.Connect` (line order re-read: pandoc 74<83, ocr 106<115, av 111<120, audio 109<118). The 70-07 change only re-parents `preflightCtx` onto the signal context. Live: `PASS: <class> preflight-catches-restrict-self` ×4; the transcript shows each preflight refusing with the engine named when restrict_self is blocked. |
| 6 | The escape hatch is reachable from docker-compose and the Helm chart for all four classes | ✓ VERIFIED | `docker-compose.yml` still forwards `PANDOC_SANDBOX_DISABLED` (l.1292), `OCR_` (1109), `AV_` (944), `AUDIO_` (819) as `"${VAR:-}"`; `.env.example` documents them. All four `deployment-*-worker.yaml` still have the `extraEnv` block; `helm template --set <class>Worker.extraEnv.<VAR>=true` re-rendered the variable for all four. Live `PASS: <class> escape-hatch` ×4. |
| 7 | "No unconfined engine exec" is a class invariant over the whole `internal/convert` package, and the scanner is itself tested | ✓ VERIFIED | Both tests re-run, PASS. The pending-file allowlist is now empty (every class confined since phase 70), so the invariant is stricter than at 2026-09-24, where five files were allowlisted. |
| 8 | CONF-01/02/04/05's phase-68 share is satisfied; no orphaned requirements | ✓ VERIFIED | `.planning/REQUIREMENTS.md` maps CONF-01/02/04/05 to phases 67–70 jointly and marks them complete; phase 68 plans 01–06 declare exactly these IDs, each with a SUMMARY. |

**Score:** 8/8 truths verified (0 present, behavior-unverified)

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/convert/sandboxwrap.go` | `engineExec`, `forJob`, `run`, `ErrEngineSandboxUnavailable` | ✓ VERIFIED | Present; extended by later phases without changing the four classes' path (see scope table) |
| `internal/convert/exec.go` | `runCommandAs`/`runCommandCore` | ✓ VERIFIED | No diff since `7c37b30` |
| `internal/convert/sandboxpreflight.go` | preflight toolkit | ✓ VERIFIED | Only additive helpers; existing helpers behave the same |
| `internal/convert/{pandoc,ocr,av,audio}sandbox.go` | measured sets, `WithSandbox`, `<Class>Preflight` | ✓ VERIFIED | Sets match conf05; only a doc-comment change in `ocrsandbox.go` |
| `internal/convert/progressexec.go` | `runCommandStreaming`, `engineExec.stream` | ✓ VERIFIED | Only a new refuse-on-env guard, never hit by av |
| `internal/convert/audioduration.go` | `EnforceMaxDuration(ctx, helper, path, max)` | ✓ VERIFIED | No diff |
| `internal/convert/sandboxcoverage_test.go` | AST coverage test | ✓ VERIFIED | Re-run PASS; broader than before |
| `scripts/conf05-crosscheck.sh` | doc-vs-Go cross-check | ✓ VERIFIED | Re-run: 4/4 MATCH (now also handles readwrite/socket for later classes) |
| `scripts/sandbox-liveproof.sh` | live gate | ✓ VERIFIED | Ran in 70-09 on images from `5db6c74`: ALL PASS (40 checks), 0 NOT RUN |
| `cmd/{pandoc,ocr,av,audio}-worker/main.go` | refuse-without-Landlock, preflight before Postgres | ✓ VERIFIED | Re-read |
| chart `deployment-{pandoc,ocr,av,audio}-worker.yaml`, `docker-compose.yml`, `.env.example` | escape hatch | ✓ VERIFIED | Re-checked, helm re-rendered |

### Key Link Verification

| From | To | Via | Status |
|------|----|-----|--------|
| `cmd/<class>-worker/main.go` | `convert.<Class>Preflight` | called before `db.Connect` | ✓ WIRED |
| `cmd/audio-worker/main.go` | `NewHandler` → `EnforceMaxDuration` | same `sandboxHelper` as the converter | ✓ WIRED |
| `internal/worker/worker.go` (4 classifiers) | `convert.ErrEngineSandboxUnavailable` | `errors.Is` first, in pandoc/ocr/av/audio classifiers (untouched) | ✓ WIRED |
| `scripts/conf05-crosscheck.sh` | `SANDBOX-PATHS.md` ↔ `internal/convert/<class>sandbox.go` | extraction vs Go literal parse | ✓ WIRED (4/4 MATCH) |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Build and vet | `GOTOOLCHAIN=go1.26.4 go build ./... && go vet ./...` | exit 0 / exit 0 | ✓ PASS |
| Packages | `GOTOOLCHAIN=go1.26.4 go test -count=1 ./internal/convert/ ./internal/sandbox/ ./internal/worker/ ./cmd/...` | all `ok` (convert 91.0s, sandbox 4.7s, worker 16.5s, cmd/* ok or no test files) | ✓ PASS |
| Coverage + pinned-set tests, named | `go test -count=1 -run 'TestConfinedClassesExecOnlyThroughTheSandbox\|TestEngineCallScannerCatchesALiteralExec\|ReadOnlySetIsTheMeasuredOne\|EnginesAreTheMeasuredOnes' -v ./internal/convert/` | all 19 PASS | ✓ PASS |
| conf05 cross-check | `scripts/conf05-crosscheck.sh pandoc ocr av audio` | 4/4 MATCH | ✓ PASS |
| Helm escape hatch | `helm template ... --set {pandoc,ocr,av,audio}Worker.extraEnv.<VAR>=true` | variable rendered for all four | ✓ PASS |
| Live Landlock, four classes | recorded `evidence/liveproof-8class-70-09.txt` (not re-run: no docker per instruction) | `ALL PASS (40 checks)`, commit `5db6c74`; `git diff 5db6c74 HEAD` over `*.go`, `scripts`, `Dockerfile*`, `docker-compose.yml`, `deploy` is empty | ✓ PASS (recorded, code identical to HEAD) |

### Requirements Coverage

| Requirement | Source Plan | Status | Evidence |
|---|---|---|---|
| CONF-01 | 68-01..06 | ✓ SATISFIED | Truths 1, 2, 7 |
| CONF-02 | 68-01, 03–06 | ✓ SATISFIED | Truths 4, 6 |
| CONF-04 | 68-01, 03–06 | ✓ SATISFIED | Truth 5 |
| CONF-05 | 68-02..06 | ✓ SATISFIED | Truth 3 |

No orphaned requirements.

### Anti-Patterns Found

None. `TBD`/`FIXME`/`XXX` grep over the 19 phase-68 production files returned nothing.

### Human Verification Required

None. The original report recorded no human-verification items and no human approvals
for phase 68, so there is nothing to carry forward. The one item that cannot be
re-executed here (live kernel denial needs Linux/Docker) rests on the 70-09
transcript built from `5db6c74`, whose code is identical to HEAD.

### Gaps Summary

None. Phase 70's changes to shared code are additive or route phase 68's classes through
the old path (`env == nil`, `Socket` empty, `outsideBase == ""`), and the two that touch
their workers (preflight context, empty allowlist) tighten rather than relax. The coverage
test, pinned-set tests and conf05 cross-check that hold phase 68's invariants pass on the
current tree.

---

_Verified: 2026-10-01_
_Verifier: Claude (gsd-verifier)_
