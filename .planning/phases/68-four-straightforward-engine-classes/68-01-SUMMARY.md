---
phase: 68-four-straightforward-engine-classes
plan: 01
subsystem: security
tags: [landlock, sandbox, pandoc, engineExec, preflight, worker]

requires:
  - phase: 67-image-class-and-sandbox-groundwork
    provides: internal/sandbox (ResolveHelper, RunExecModeIfRequested, SetupExit, Apply, Paths, Limits), internal/convert/sandboxwrap.go (engineSandbox, wrapEngine, enginePathFor, spec, confines)
provides:
  - "engineExec: one job's ruleset (sandbox + spec), built once by forJob and shared by every engine call site of that job"
  - "runCommandAs: engine error shapes keep naming the ENGINE even when the process actually exec'd is the worker binary in helper mode"
  - "ErrEngineSandboxUnavailable: a helper exit 78 is a node fault, classified retryable in isAudioTerminal/isAVTerminal/isOCRTerminal/isPandocTerminal ahead of every class-prefix match"
  - "the preflight toolkit: preflightWorkspace (sibling job/outside dirs), place, requireOutput, requireDenied, preflightMarker -- content judgments in both directions"
  - "scripts/sandbox-liveproof.sh: the per-class live gate (confined-startup, refuses-without-landlock, escape-hatch, preflight-catches-restrict-self), calibrated on phase 67's image worker, all five classes' rows pre-populated"
  - "pandoc confined end-to-end: PandocConverter.Sandbox, pandocReadOnly ([]), pandocEngines ([pandoc]), WithSandbox, PandocPreflight, cmd/pandoc-worker refuses to start without confinement and proves it before touching Postgres"
affects: [68-02-ocr-pdf-remeasure, 68-03-ocr-sandbox, 68-04-av-sandbox, 68-05-audio-sandbox, 68-06]

actuals:
  tokens: 17300
  tasks: 3
  commits: 3

tech-stack:
  added: []
  patterns:
    - "engineExec/forJob/run: one ruleset built per job, shared across every engine invocation that job makes -- replaces a per-call-site spec rebuild"
    - "runCommandAs(ctx, label, name, args...): error text names the caller's chosen label, not the process actually exec'd -- needed because a confined engine call execs the WORKER BINARY in helper mode, not the engine"
    - "content-judged preflight (sandboxpreflight.go): sufficiency and effectiveness both proven by output bytes, never by exit status alone, using a sibling job/outside directory pair and a random per-run marker"
    - "sandbox sentinel checked before class-prefix text: every engine-scoped terminal classifier checks errors.Is(err, convert.ErrEngineSandboxUnavailable) first, so a node fault is never misclassified as a client file defect even when the wrapped text also carries that class's own terminal shape"

key-files:
  created:
    - internal/convert/sandboxpreflight.go
    - internal/convert/pandocsandbox.go
    - internal/convert/sandboxexec_test.go
    - internal/convert/sandboxpreflight_test.go
    - internal/convert/pandocsandbox_test.go
    - internal/worker/engine_sandbox_retryable_test.go
    - scripts/sandbox-liveproof.sh
  modified:
    - internal/convert/exec.go
    - internal/convert/sandboxwrap.go
    - internal/convert/pandoc.go
    - internal/convert/libvips.go
    - internal/worker/worker.go
    - cmd/pandoc-worker/main.go
    - docker-compose.yml
    - .env.example
    - deploy/chart/octoconv/templates/deployment-pandoc-worker.yaml

key-decisions:
  - "pandocReadOnly is empty: the release pandoc binary is statically linked (measured, SANDBOX-PATHS.md), so the only grant pandoc's ruleset needs beyond the job directory is the resolved binary itself, added by forJob via pandocEngines"
  - "forJob never grants an input directory separately (unlike libvipsSpec): the job directory is the ONLY writable/extra-readable path, because the worker always materialises inputs there and the preflight's negative run depends on nothing else being reachable"
  - "runCommandLimitedEnvCPU's body moved into runCommandCore(ctx, limit, env, label, name, args...) with label=name at every existing call site, so runCommandAs is additive and every pre-existing error string in the codebase stays byte-identical"

patterns-established:
  - "Pattern: shared engineExec built once via forJob(jobDir, readOnly, engines), reused across every call site of one job -- OCR's four call sites and AV's five (plans 03/04) plug in without re-deriving a spec each time"
  - "Pattern: preflightWorkspace + place/requireOutput/requireDenied/preflightMarker -- every future <Class>Preflight (ocr, av, audio) is built from this toolkit, not a per-class copy"

requirements-completed: []  # CONF-01/02/04/05 are declared by every sibling plan in this phase (68-02..06); requirements.ready-ids reported 0/4 ready -- marked complete only once every declaring plan has a SUMMARY (shared-ID gate, #2388)

coverage:
  - id: D1
    description: "pandoc's engine subprocess runs through the Landlock helper with a per-job ruleset (rw: job dir only, ro: pandoc binary only), proven by content in both directions"
    requirement: CONF-01
    verification:
      - kind: unit
        ref: "internal/convert/sandboxexec_test.go#TestEngineExecRunsThroughTheHelperWithTheExpectedSpec"
        status: pass
      - kind: unit
        ref: "internal/convert/pandocsandbox_test.go#TestPandocConvertRunsTheEngineThroughTheHelper"
        status: pass
      - kind: integration
        ref: "scripts/sandbox-liveproof.sh pandoc (confined-startup check: '🔒 pandoc sandbox proven: the engine converted inside the job directory and was denied a file outside it')"
        status: pass
    human_judgment: false
  - id: D2
    description: "cmd/pandoc-worker refuses to start when confinement is wanted and cannot be established, naming PANDOC_SANDBOX_DISABLED as the remedy; the escape hatch is reachable from compose and the Helm chart and defaults to confined"
    requirement: CONF-02
    verification:
      - kind: integration
        ref: "scripts/sandbox-liveproof.sh pandoc (refuses-without-landlock and escape-hatch checks)"
        status: pass
      - kind: integration
        ref: "helm template ... --set pandocWorker.extraEnv.PANDOC_SANDBOX_DISABLED=true renders the value; docker compose config with PANDOC_SANDBOX_DISABLED unset renders empty"
        status: pass
    human_judgment: false
  - id: D3
    description: "PandocPreflight judges both sufficiency (converts inside the job directory, output carries a marker) and effectiveness (identical bytes outside the job directory are denied) by content, never by exit status alone; the live gate also proves the worker refuses to start when only landlock_restrict_self is denied (ABI query allowed, ResolveHelper reports confined) -- the preflight, not ResolveHelper, catches that failure"
    requirement: CONF-04
    verification:
      - kind: unit
        ref: "internal/convert/sandboxpreflight_test.go (TestRequireOutput*, TestRequireDenied*)"
        status: pass
      - kind: unit
        ref: "internal/convert/pandocsandbox_test.go#TestPandocPreflightUnconfinedProvesNothing"
        status: pass
      - kind: integration
        ref: "scripts/sandbox-liveproof.sh pandoc (preflight-catches-restrict-self check)"
        status: pass
    human_judgment: false
  - id: D4
    description: "pandoc's granted set (pandocReadOnly=[], pandocEngines=[pandoc]) is pinned by a test that fails when the set drifts, not held by a comment alone"
    requirement: CONF-05
    verification:
      - kind: unit
        ref: "internal/convert/pandocsandbox_test.go#TestPandocReadOnlySetIsTheMeasuredOne"
        status: pass
      - kind: unit
        ref: "internal/convert/pandocsandbox_test.go#TestPandocEnginesAreTheMeasuredOnes"
        status: pass
    human_judgment: false
  - id: D5
    description: "a helper exit of sandbox.SetupExit (78) is retryable, not blamed on the client's file, for all four confined classes (pandoc, ocr, av, audio) even when the wrapped text also carries that class's own terminal shape"
    verification:
      - kind: unit
        ref: "internal/worker/engine_sandbox_retryable_test.go#TestEngineSandboxUnavailableIsRetryableForEveryConfinedClass"
        status: pass
      - kind: unit
        ref: "internal/worker/engine_sandbox_retryable_test.go#TestEngineSandboxControlShapesAreTerminal"
        status: pass
    human_judgment: false

duration: 75min
completed: 2026-09-24
status: complete
---

# Phase 68 Plan 01: Pandoc Confined End-to-End Summary

**Pandoc runs its engine through the shared Landlock `engineExec`/`forJob`/preflight toolkit, proven by content in both directions and by a live gate that also calibrates on phase 67's image worker (8/8 checks pass).**

## Performance

- **Duration:** 75 min (includes debugging a real bash `command substitution holds a pipe open` bug found live in the gate script)
- **Started:** 2026-09-24T02:40:00+03:00 (approx.)
- **Completed:** 2026-09-24T02:56:00+03:00
- **Tasks:** 3
- **Files modified:** 16 (7 created, 9 modified)

## Accomplishments

- `engineExec`/`forJob`/`run`/`sandboxFault` (sandboxwrap.go) and `runCommandAs`/`runCommandCore` (exec.go): the shared per-job ruleset and engine-error-naming mechanism every confined class from here on plugs into with a one-line `forJob(...)` call.
- `ErrEngineSandboxUnavailable`: a helper exit 78 is now retryable, not terminal, in all four of `isAudioTerminal`/`isAVTerminal`/`isOCRTerminal`/`isPandocTerminal` -- checked before any class-prefix text match, with a non-vacuity control test proving the same wrapped shapes stay terminal without the sentinel.
- `sandboxpreflight.go`: a reusable preflight toolkit (`preflightWorkspace`, `place`, `requireOutput`, `requireDenied`, `preflightMarker`) that judges both sufficiency and effectiveness by output content.
- `PandocConverter.Sandbox` wired through `Convert`; `pandocReadOnly` (empty -- the release binary is static) and `pandocEngines` (`[pandoc]`) pinned by tests; `PandocPreflight` proves both directions on the SAME converter value that gets registered.
- `cmd/pandoc-worker/main.go` refuses to start when confinement is wanted and cannot be established, and proves the ruleset via `PandocPreflight` before ever calling `db.Connect`.
- `scripts/sandbox-liveproof.sh`: a five-class live gate (image/pandoc/ocr/av/audio rows all present) with four checks each (confined-startup, refuses-without-landlock, escape-hatch, preflight-catches-restrict-self), each generating its own docker seccomp profile in a temp dir. Run against freshly built images: `scripts/sandbox-liveproof.sh image pandoc` → `ALL PASS (8 checks)`.
- `PANDOC_SANDBOX_DISABLED` forwarded in `docker-compose.yml` (unset stays confined), documented in `.env.example`, and reachable from the Helm chart's existing `pandocWorker.extraEnv`.

## Task Commits

Each task was committed atomically:

1. **Task 1: Pandoc confined end-to-end** - `9d3dcbf` (feat)
2. **Task 2: Pin the contract (engineExec, preflight, pandoc sets, retryable sandbox faults)** - `a12e8d5` (test)
3. **Task 3: Remedy reachable from compose/chart; complete the live gate** - `63f3bc2` (feat, includes a Rule 1 gofmt fix)

**Plan metadata:** committed separately after this SUMMARY (docs).

## Files Created/Modified

- `internal/convert/exec.go` - `runCommandAs`/`runCommandCore`: engine error shapes name a caller-chosen label, not the process actually exec'd
- `internal/convert/sandboxwrap.go` - `ErrEngineSandboxUnavailable`, `newEngineSandbox`, `engineExec`, `forJob`, `run`, `sandboxFault`
- `internal/convert/sandboxpreflight.go` - the reusable preflight toolkit (new)
- `internal/convert/pandoc.go` - `Sandbox` field wired through `Convert`
- `internal/convert/pandocsandbox.go` - `pandocReadOnly`, `pandocEngines`, `WithSandbox`, `PandocPreflight` (new)
- `internal/convert/libvips.go` - fixed a doc comment naming a nonexistent env var (`LIBVIPS_SANDBOX_DISABLED` → `IMAGE_SANDBOX_DISABLED`)
- `internal/worker/worker.go` - sandbox-fault sentinel check added to all four engine-scoped terminal classifiers
- `cmd/pandoc-worker/main.go` - startup block mirrors `cmd/worker/main.go`: helper mode first, `ResolveHelper`, `Replace`, preflight before `db.Connect`
- `scripts/sandbox-liveproof.sh` - the five-class, four-check live gate (new)
- `docker-compose.yml`, `.env.example`, `deploy/chart/octoconv/templates/deployment-pandoc-worker.yaml` - `PANDOC_SANDBOX_DISABLED` reachable and documented
- `internal/convert/sandboxexec_test.go`, `internal/convert/sandboxpreflight_test.go`, `internal/convert/pandocsandbox_test.go`, `internal/worker/engine_sandbox_retryable_test.go` - test coverage (new)

## Decisions Made

See `key-decisions` in frontmatter. The most consequential for plans 03-06: `forJob` never grants an input directory separately from the job directory (unlike `libvipsSpec`) -- every future class's converter must materialise its input inside the job directory before calling `x.run`, or the ruleset will deny it exactly as the preflight's negative run depends on.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `gofmt` formatting error in `sandboxpreflight.go`**
- **Found during:** pre-Task-3 hygiene pass (`gofmt -l .`)
- **Issue:** a `+` operator was left on the wrong side of a line-continuation in a multi-line `fmt.Errorf` call, which `gofmt -w` reformats
- **Fix:** ran `gofmt -w internal/convert/sandboxpreflight.go`
- **Files modified:** `internal/convert/sandboxpreflight.go`
- **Verification:** `gofmt -l .` reports no files
- **Committed in:** `63f3bc2` (Task 3 commit)

**2. [Rule 1 - Bug] `scripts/sandbox-liveproof.sh`'s watchdog held its own `$(run_gated ...)` capture open for a full 300s per check**
- **Found during:** the first live run of `scripts/sandbox-liveproof.sh image pandoc` for Task 3 (the run hung; diagnosed via `sample` on the stuck bash process, confirmed with an isolated reproduction, then confirmed fixed the same way before re-running the real gate)
- **Issue:** `run_gated`'s background watchdog subshell (`( sleep 300; docker kill ... ) &`) was started while `run_gated`'s own stdout was already redirected into the caller's `result=$(run_gated ...)` pipe. Bash's command substitution does not return until every process holding that pipe's write end has exited -- including a background job that writes nothing at all -- so each of the three new checks (refuses-without-landlock, escape-hatch, preflight-catches-restrict-self) would have silently taken up to 300s per class instead of the sub-second time the underlying `docker run` actually needs. `check_confined_startup` (Task 1, not refactored through `run_gated`) was unaffected.
- **Fix:** redirected the watchdog subshell's own stdin/stdout/stderr explicitly (`</dev/null >/dev/null 2>&1 &`) in both `run_gated` and, for consistency/defense-in-depth, `check_confined_startup`'s own watchdog.
- **Files modified:** `scripts/sandbox-liveproof.sh`
- **Verification:** isolated bash reproduction confirmed the hang and the fix before touching Docker; the real gate then completed in seconds per check (`scripts/sandbox-liveproof.sh image pandoc` → `ALL PASS (8 checks)` in well under a minute of actual docker-run time)
- **Committed in:** `63f3bc2` (Task 3 commit; the buggy intermediate version was never committed)

---

**Total deviations:** 2 auto-fixed (2 Rule 1 bugs). **Impact:** both fixes were necessary for correctness (one for `gofmt` cleanliness required by CLAUDE.md, one for the live gate script to actually finish in reasonable time); no scope creep.

## Issues Encountered

None beyond the deviation above (the shell command-substitution bug was found, diagnosed, and fixed within Task 3's own scope before the buggy version was ever committed).

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- `engineExec`/`forJob`/`run`/`sandboxFault`, `runCommandAs`, `ErrEngineSandboxUnavailable`, the preflight toolkit, and `scripts/sandbox-liveproof.sh` (with ocr/av/audio rows already in its class table) are ready for plans 02-06.
- 68-02 (OCR PDF path re-measurement) is unblocked and does not depend on anything from this plan beyond the shared toolkit above.
- CONF-01/02/04/05 stay "In Progress" in REQUIREMENTS.md until every plan in this phase that declares them (02-06) has its own SUMMARY -- this is the shared-ID gate (#2388) working as intended, not an oversight.
- No blockers for 68-02 through 68-06.

---
*Phase: 68-four-straightforward-engine-classes*
*Completed: 2026-09-24*

## Self-Check: PASSED

All key files (`internal/convert/exec.go`, `sandboxwrap.go`, `sandboxpreflight.go`, `pandoc.go`,
`pandocsandbox.go`, `libvips.go`, `sandboxexec_test.go`, `sandboxpreflight_test.go`,
`pandocsandbox_test.go`, `internal/worker/worker.go`, `engine_sandbox_retryable_test.go`,
`cmd/pandoc-worker/main.go`, `scripts/sandbox-liveproof.sh`, `docker-compose.yml`, `.env.example`,
`deploy/chart/octoconv/templates/deployment-pandoc-worker.yaml`) verified present on disk with `[ -f ]`.
All three task commits (`9d3dcbf`, `a12e8d5`, `63f3bc2`) verified present via `git log --oneline --all`.
`go build ./...`, `go vet ./...`, `go test -count=1 ./...` all exit 0. `scripts/sandbox-liveproof.sh
image pandoc` ends `ALL PASS (8 checks)`. `scripts/ci-helm-checks.sh` exits 0 with 0 FAIL lines.
