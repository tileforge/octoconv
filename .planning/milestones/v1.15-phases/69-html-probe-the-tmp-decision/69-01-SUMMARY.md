---
phase: 69-html-probe-the-tmp-decision
plan: 01
subsystem: security
tags: [landlock, sandbox, chromium, html, engineExec, preflight, worker, tmp-decision]

requires:
  - phase: 68-four-straightforward-engine-classes
    provides: internal/convert/sandboxwrap.go (engineSandbox, engineExec, forJob, run, sandboxFault), internal/convert/sandboxpreflight.go (preflightWorkspace, place, requireOutput, requireDenied, preflightMarker), scripts/sandbox-liveproof.sh, scripts/conf05-crosscheck.sh
provides:
  - "CONF-06: the /tmp decision (D-01..D-05) recorded in SANDBOX-PATHS.md with its measurements -- one task per container while a class grants the shared /tmp, held by a startup guard and a sweep, not a comment"
  - "forJobRW(jobDir, extraRW, readOnly, engines): forJob generalised to grant a class-shared writable path beyond the job directory; forJob itself is forJobRW with nil extras, byte-identical for every prior class"
  - "engineExec.env/withEnv/engineEnv: an engine's environment can be REPLACED wholesale (allowlist PATH/HOME/LANG/LC_ALL + optional TMPDIR), reused from tectonicEnv's precedent"
  - "sharedTmpDir/sharedTmpGuard: a confined class whose writable set grants the container's shared /tmp refuses to start above concurrency 1, keyed on the GRANT SET not the class name"
  - "requireAbsent/newPreflightWorkspaceOutside/requireOutsideGrants: preflight primitives for a class whose negative run cannot go through Convert (it copies its input in first) and whose outside fixture cannot live under the class's own writable grant"
  - "html confined end-to-end: ChromiumConverter.Sandbox/sharedTmp, htmlReadWrite/htmlReadOnly/htmlEngines (measured, D-08 wrapper/interpreter/real-binary triad), ConfinedChromium, sweepSharedTmp, HTMLPreflight (judged by the PDF /Title marker), cmd/chromium-worker refuses to start without confinement, refuses HTML_WORKER_CONCURRENCY above 1 while confined, and proves both before touching Postgres"
affects: [69-02-probe-sandbox, 69-03-edge-ledger]

actuals:
  tokens: 25500
  tasks: 3
  commits: 4

# Measured via `git rev-list --count ${plan_head_before}..HEAD` = 4 (#3968)
plan_head_before: 810a479a8b3bd22846d9cb381457203905ddc5ca

tech-stack:
  added: []
  patterns:
    - "forJobRW(jobDir, extraRW, readOnly, engines): a class-shared writable path (beyond the job directory) is declared once, in the SAME ordered rw: token stream forJob already builds -- forJob is now a one-line call to forJobRW with nil extras"
    - "engineExec.withEnv: an engine's environment is REPLACED (never merged) via a plain []string field; nil keeps every pre-existing class's runCommandAs path untouched, non-nil routes through runCommandCore. progressexec.go's stream refuses a non-nil env outright rather than silently dropping it (no env parameter exists on that path)"
    - "sharedTmpGuard(readWrite, sandbox, concurrency, concurrencyEnv): a startup refusal keyed on the GRANT SET (any class whose writable set contains the shared path), not a class-name special case -- the same shape CONF-02's ResolveHelper refusal already established, applied to a different invariant"
    - "requireAbsent as requireDenied's own marker judgment, extracted: a class whose negative preflight run cannot use requireDenied's convErr check (chromium exits 0 and writes a blank PDF when denied) still shares one byte judgment"

key-files:
  created:
    - internal/convert/htmlsandbox.go
    - internal/convert/htmlsandbox_test.go
  modified:
    - internal/convert/sandboxwrap.go
    - internal/convert/progressexec.go
    - internal/convert/sandboxpreflight.go
    - internal/convert/chromium.go
    - internal/convert/chromium_test.go
    - internal/convert/sandboxcoverage_test.go
    - internal/convert/sandboxexec_test.go
    - internal/convert/sandboxpreflight_test.go
    - internal/worker/worker.go
    - internal/worker/engine_sandbox_retryable_test.go
    - cmd/chromium-worker/main.go
    - scripts/sandbox-liveproof.sh
    - scripts/conf05-crosscheck.sh
    - docker-compose.yml
    - .env.example
    - deploy/chart/octoconv/templates/configmap.yaml
    - deploy/chart/octoconv/templates/deployment-chromium-worker.yaml
    - .planning/research/SANDBOX-PATHS.md

key-decisions:
  - "D-01/D-03: HTML_WORKER_CONCURRENCY default 1 everywhere it is configured (main.go, compose, .env.example, chart ConfigMap); a confined worker refuses to start above 1, checked against the SAME variable asynq.Config.Concurrency is built from"
  - "D-04: sweep implementation is sweepSharedTmp(dir, keep) -- removes every entry of dir except keep (a symlink is removed as a symlink, its target survives); a sweep failure wraps ErrEngineSandboxUnavailable and is retryable, a node fault never blamed on the client's file (Claude's discretion, per CONTEXT.md)"
  - "D-06/D-07: HTMLPreflight judges by the PDF's /Title field (not the URI-annotation alternative both measured to work) -- simpler fixture, no annotation object to hunt for"
  - "The html preflight's outside fixture lives under /var/tmp, proven outside every grant by requireOutsideGrants at run time rather than assumed from the path string alone"
  - "TMPDIR is deliberately left UNSET in ChromiumConverter.exec's engineEnv(\"\") call -- chromium sees the measured default (/tmp), matching D-05's finding that TMPDIR does not move its shared memory anyway"

patterns-established:
  - "Pattern: a class-shared writable path is declared via forJobRW's extraRW parameter, not a second spec-building path -- plan 69-02's probe class (if it ever needed one) or any future class reuses forJobRW directly"
  - "Pattern: engineEnv(tmpDir) is the one allowlist-builder every environment-replacing class shares (mirrors tectonicEnv, now generalised out of a single-class file into sandboxwrap.go)"

requirements-completed: [CONF-01, CONF-02, CONF-04, CONF-05, CONF-06]

coverage:
  - id: D1
    description: "CONF-06: the /tmp decision (D-01..D-05) is recorded in SANDBOX-PATHS.md with its measurements -- the seccomp EPERM on unshare -Urm, Landlock's inability to remap /tmp, TMPDIR not moving chromium's shared memory, and probe's TMPDIR-honours-gs measurement -- before any code in this task touched the html sandbox"
    requirement: CONF-06
    verification:
      - kind: other
        ref: ".planning/research/SANDBOX-PATHS.md '## Решение по /tmp (CONF-06, фаза 69)' section, written before Task 1's code changes in the same commit"
        status: pass
      - kind: unit
        ref: "internal/convert/sandboxexec_test.go#TestSharedTmpGuard"
        status: pass
    human_judgment: false
  - id: D2
    description: "html's engine subprocess runs through the Landlock helper with a per-job ruleset (rw: job dir, /tmp, /dev/null; ro: the measured 15-path set including the D-08 wrapper/interpreter/real-binary triad), proven by content in both directions"
    requirement: CONF-01
    verification:
      - kind: unit
        ref: "internal/convert/htmlsandbox_test.go#TestChromiumConvertRunsTheEngineThroughTheHelper"
        status: pass
      - kind: unit
        ref: "internal/convert/htmlsandbox_test.go#TestHTMLGrantsTheWrapperItsInterpreterAndTheRealBinary"
        status: pass
      - kind: unit
        ref: "internal/convert/sandboxexec_test.go#TestEngineExecRWGrantsJobDirThenExtrasInOrder"
        status: pass
      - kind: unit
        ref: "internal/convert/sandboxpreflight_test.go#TestRequireOutsideGrants"
        status: pass
      - kind: integration
        ref: "scripts/sandbox-liveproof.sh html (confined-startup: '🔒 html sandbox proven: the engine rendered inside the job directory and was denied a file outside every grant'; wrapper-as-measured)"
        status: pass
    human_judgment: false
  - id: D3
    description: "cmd/chromium-worker refuses to start when confinement is wanted and cannot be established, naming HTML_SANDBOX_DISABLED as the remedy, AND refuses to start when HTML_WORKER_CONCURRENCY is above 1 while confined, naming the shared /tmp (D-03); both escape hatches/knobs are reachable from compose and the Helm chart and default to confined/1"
    requirement: CONF-02
    verification:
      - kind: unit
        ref: "internal/convert/htmlsandbox_test.go#TestConfinedChromium"
        status: pass
      - kind: integration
        ref: "scripts/sandbox-liveproof.sh html (refuses-without-landlock, escape-hatch, concurrency-guard checks)"
        status: pass
      - kind: other
        ref: "docker compose config (HTML_SANDBOX_DISABLED set/unset) and helm template --set chromiumWorker.extraEnv.HTML_SANDBOX_DISABLED=true assertions"
        status: pass
    human_judgment: false
  - id: D4
    description: "HTMLPreflight judges both sufficiency (converts inside the job directory, PDF carries /Title (marker)) and effectiveness (the engine itself, pointed at a file outside every grant, produces a PDF with no /Title key) by content, never by chromium's exit status -- which is deliberately not inspected, since chromium exits 0 and writes a blank PDF when denied"
    requirement: CONF-04
    verification:
      - kind: unit
        ref: "internal/convert/sandboxpreflight_test.go#TestRequireAbsent"
        status: pass
      - kind: unit
        ref: "internal/convert/htmlsandbox_test.go#TestHTMLPreflightUnconfinedProvesNothing"
        status: pass
      - kind: unit
        ref: "internal/convert/htmlsandbox_test.go#TestHTMLPreflightRefusesAConfinedConverterThatDoesNotSweep"
        status: pass
      - kind: integration
        ref: "scripts/sandbox-liveproof.sh html (preflight-catches-restrict-self check)"
        status: pass
    human_judgment: false
  - id: D5
    description: "html's granted sets (htmlReadWrite, htmlReadOnly, htmlEngines) are pinned by tests that fail when the set drifts, and equal SANDBOX-PATHS.md's conf05:html blocks exactly, held by scripts/conf05-crosscheck.sh's new readwrite kind"
    requirement: CONF-05
    verification:
      - kind: unit
        ref: "internal/convert/htmlsandbox_test.go#TestHTMLReadWriteSetIsTheMeasuredOne"
        status: pass
      - kind: unit
        ref: "internal/convert/htmlsandbox_test.go#TestHTMLReadOnlySetIsTheMeasuredOne"
        status: pass
      - kind: unit
        ref: "internal/convert/htmlsandbox_test.go#TestHTMLEnginesAreTheMeasuredOnes"
        status: pass
      - kind: other
        ref: "scripts/conf05-crosscheck.sh html -> MATCH: html readonly (15) engines (1) readwrite (2)"
        status: pass
    human_judgment: false
  - id: D6
    description: "html's shared /tmp sweep (D-04) removes every leftover except the current job directory before the engine ever runs, and a sweep failure is retryable (ErrEngineSandboxUnavailable), never a client-file defect -- proven live by planting leftovers in a real container's /tmp before startup"
    verification:
      - kind: unit
        ref: "internal/convert/htmlsandbox_test.go#TestSweepSharedTmp"
        status: pass
      - kind: unit
        ref: "internal/convert/htmlsandbox_test.go#TestChromiumConvertSweepsTheSharedTmpBeforeTheEngine"
        status: pass
      - kind: unit
        ref: "internal/worker/engine_sandbox_retryable_test.go#TestEngineSandboxUnavailableIsRetryableForEveryConfinedClass/html"
        status: pass
      - kind: integration
        ref: "scripts/sandbox-liveproof.sh html (sweeps-shared-tmp check)"
        status: pass
    human_judgment: false
  - id: D7
    description: "the chromium engine's environment is scrubbed to an allowlist (PATH/HOME/LANG/LC_ALL) because the html grant includes /proc (and therefore /proc/self/environ) -- no secret env var reaches the engine"
    verification:
      - kind: unit
        ref: "internal/convert/sandboxexec_test.go#TestEngineEnvIsAnAllowlist"
        status: pass
      - kind: unit
        ref: "internal/convert/htmlsandbox_test.go#TestChromiumEngineEnvCarriesNoSecrets"
        status: pass
    human_judgment: false

duration: ~95min (two sessions, split by an environment 1Password SSH-signing outage between Task 1's implementation and its commit)
completed: 2026-09-26
status: complete
---

# Phase 69 Plan 01: HTML Confined End-to-End & the CONF-06 /tmp Decision Summary

**The html engine class runs chromium-headless-shell through a Landlock ruleset that shares `/tmp` by measured necessity, held safe by a concurrency-1 startup guard and a pre-task sweep, with a preflight that judges confinement by the produced PDF's `/Title` field rather than chromium's exit status.**

## Performance

- **Duration:** ~95 min across two sessions (see Issues Encountered)
- **Started:** 2026-09-26 (first session)
- **Completed:** 2026-09-26T19:06:16+03:00
- **Tasks:** 3
- **Files modified:** 20 (2 created, 18 modified)

## Accomplishments

- Recorded the CONF-06 `/tmp` decision (D-01..D-05) in `.planning/research/SANDBOX-PATHS.md`, with its measurements (the `unshare -Urm` seccomp EPERM, `gs`'s `TMPDIR` result), before writing any html sandbox code
- `html` confined end-to-end: measured ruleset (job dir + shared `/tmp` + `/dev/null` writable; the D-08 wrapper/interpreter/real-binary triad plus 12 other paths read-only), an allowlisted engine environment, a pre-task `/tmp` sweep, a concurrency-1 startup guard, and a preflight judged by the PDF's `/Title` field in both directions
- Generalised `forJob` into `forJobRW` (a class-shared writable path beyond the job directory), added `engineExec.withEnv`/`engineEnv` (a replaceable, allowlisted environment), and extracted `requireAbsent`/`newPreflightWorkspaceOutside`/`requireOutsideGrants` from the existing preflight toolkit -- all four consumed by plan 69-02's probe class
- Live-gated on a freshly built `chromium-worker` image: `scripts/sandbox-liveproof.sh html` ends `ALL PASS (7 checks)`, including three html-only checks (concurrency-guard, wrapper-as-measured, sweeps-shared-tmp) added this plan

## Task Commits

Each task was committed atomically:

1. **Task 1: Record the /tmp decision, then html confined end-to-end** - `d25bf62` (feat)
2. **Task 2: Pin the contract (RED)** - `d3bad5b` (test)
2. **Task 2: isHTMLTerminal never misclassifies a sandbox fault (GREEN)** - `b041f69` (feat)
3. **Task 3: Concurrency default 1, escape hatch, conf05 readwrite cross-check, live checks** - `b29725f` (feat)

**Plan metadata:** (this commit)

_Note: Task 2 is a TDD task and produced two commits (test → feat); no REFACTOR commit was needed._

## Files Created/Modified

- `internal/convert/htmlsandbox.go` - html's measured sets, `WithSandbox`, `ConfinedChromium`, `sweepSharedTmp`, `HTMLPreflight`
- `internal/convert/htmlsandbox_test.go` - pinned sets, `ConfinedChromium`/preflight/sweep/env behavior
- `internal/convert/sandboxwrap.go` - `forJobRW`, `engineExec.env`/`withEnv`, `engineEnv`, `sharedTmpDir`/`sharedTmpGuard`
- `internal/convert/progressexec.go` - `stream` refuses a per-exec environment
- `internal/convert/sandboxpreflight.go` - `requireAbsent`, `newPreflightWorkspaceOutside`, `requireOutsideGrants`
- `internal/convert/chromium.go` - `Sandbox`/`sharedTmp` fields, `chromiumPrintArgs` extracted, `Convert` wired through `engineExec` + sweep
- `internal/convert/chromium_test.go` - one pinned-substring test updated for the new call-site formatting (Rule 1)
- `internal/convert/sandboxcoverage_test.go` - `chromium.go`/`htmlsandbox.go` reclassified from pending to the html class
- `internal/convert/sandboxexec_test.go` / `sandboxpreflight_test.go` - the shared-contract pinning tests
- `internal/worker/worker.go` - `isHTMLTerminal` checks `ErrEngineSandboxUnavailable` first
- `internal/worker/engine_sandbox_retryable_test.go` - html row + control row
- `cmd/chromium-worker/main.go` - `HTML_SANDBOX_DISABLED`/`HTML_WORKER_CONCURRENCY` wiring, preflight before Postgres
- `scripts/sandbox-liveproof.sh` - per-class extra `docker run` args, html/probe rows, three html-only checks
- `scripts/conf05-crosscheck.sh` - the `readwrite` kind, html/probe wiring
- `docker-compose.yml` / `.env.example` / `deploy/chart/octoconv/templates/configmap.yaml` / `deployment-chromium-worker.yaml` - concurrency default 1, escape hatch
- `.planning/research/SANDBOX-PATHS.md` - the CONF-06 decision section, conf05:html blocks

## Decisions Made

See `key-decisions` in the frontmatter. The most consequential: html keeps `/tmp` on the ruleset (measured necessity, chromium's shared memory) and pays for it with a concurrency-1 startup guard plus a mandatory pre-task sweep, rather than a mount-namespace-based private `/tmp` per task (measured impossible under Docker's default seccomp, D-02).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `chromium_test.go`'s pinned-substring test broke on a legitimate gofmt reformatting**
- **Found during:** Task 1
- **Issue:** `TestChromiumArgvContainsRequiredFlags` scans `chromium.go`'s source text for the literal expression `"file://" + renderedPath`. Moving that concatenation from a slice literal (`chromiumPrintArgs`'s old inline form) to a plain function-call argument at the `Convert` call site changed gofmt's own spacing decision for the same expression (`"file://"+renderedPath`, no spaces) -- a real, gofmt-verified formatting difference, not a mistake.
- **Fix:** Updated the test's expected string to the new (correctly gofmt'd) formatting, with a comment explaining why the spacing differs.
- **Files modified:** `internal/convert/chromium_test.go`
- **Verification:** `go test ./internal/convert/ -run TestChromiumArgvContainsRequiredFlags` passes; `gofmt -l` reports clean.
- **Committed in:** `d25bf62` (Task 1 commit)

**2. [Rule 1 - Bug] `scripts/sandbox-liveproof.sh`'s `run_gated` broke on bash 3.2 (macOS) for a no-seccomp-profile call**
- **Found during:** Task 3, adding the html concurrency-guard check
- **Issue:** `run_gated` unconditionally expanded a bash array (`"${secopt[@]}"`) that was left empty when no seccomp profile was requested (`profile=""`, a case the function's own doc comment already promised support for). macOS's shipped bash (3.2.57) raises "unbound variable" under `set -u` when expanding a DECLARED-BUT-EMPTY array -- a real, dormant bug never exercised because every pre-existing caller always passed a non-empty seccomp profile. The same bug pattern was independently present in this plan's own new `local extra=("$@")` pattern in `check_confined_startup`/`check_refuses_without_landlock`/`check_escape_hatch`/`check_preflight_catches_restrict_self` (added in Task 1 for the per-class extra-run-args mechanism), which broke EVERY class's live gate the moment a class had zero extra args -- i.e. every class except `html`.
- **Fix:** `run_gated` now branches explicitly on `profile` instead of expanding a possibly-empty array; every `"${extra[@]}"`/`"${extra_run_args[@]}"` call site converted to the bash-3.2-safe `${arr[@]+"${arr[@]}"}` idiom.
- **Files modified:** `scripts/sandbox-liveproof.sh`
- **Verification:** `scripts/sandbox-liveproof.sh html` -> `ALL PASS (7 checks)`; `scripts/sandbox-liveproof.sh image` -> `ALL PASS (4 checks)` (regression check for a class with no extra args).
- **Committed in:** `b29725f` (Task 3 commit)

---

**Total deviations:** 2 auto-fixed (both Rule 1 -- bugs found while implementing the plan, not scope creep)
**Impact on plan:** Both fixes were necessary for the plan's own verification commands to run at all; no behavior outside this plan's scope was touched.

## Issues Encountered

- **Environment blocker, not a code defect:** partway through Task 1 (after all code was written, built, vetted, tested, and live-gated successfully), `git commit` failed repeatedly with `error: 1Password: failed to fill whole buffer` -- the 1Password SSH-signing agent (`gpg.format=ssh`) refused every signing request on this machine, reproduced independently of git via a raw `ssh-keygen -Y sign` test. Per the user's own instruction (never bypass commit signing), the executor halted and returned a `checkpoint:human-action` rather than force the commit through. The coordinator confirmed the agent was working again in a follow-up message; the plan resumed from exactly where it stopped (re-ran the root-pin guard, committed Task 1's already-verified working tree, then continued Tasks 2 and 3 normally). No code was lost or redone.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- `forJobRW`, `engineExec.withEnv`/`engineEnv`, `sharedTmpGuard`, `requireAbsent`, `newPreflightWorkspaceOutside`/`requireOutsideGrants` are all in place and pinned by tests -- plan 69-02 (probe) consumes them directly per the plan's own `<interfaces>` contract.
- `scripts/sandbox-liveproof.sh`'s probe row and per-class extra-run-args mechanism are already in place (added this plan); plan 69-02 runs probe's own live gate.
- `scripts/conf05-crosscheck.sh`'s `readwrite` kind is generic (keyed on `class_go_readwrite_var`, not hardcoded to html); probe only needs its own case added.
- CONF-01/02/04/05/06 are declared by this plan AND by sibling plans 69-02/69-03 in the same phase (the probe half and the edge ledger) -- per the shared-ID gate (#2388), REQUIREMENTS.md marks them complete only once every declaring plan has a SUMMARY, handled by the `requirements.ready-ids` check in this plan's own STATE update step.

---
*Phase: 69-html-probe-the-tmp-decision*
*Completed: 2026-09-26*

## Self-Check: PASSED

- `internal/convert/htmlsandbox.go` — FOUND
- `internal/convert/htmlsandbox_test.go` — FOUND
- Commits `d25bf62`, `d3bad5b`, `b041f69`, `b29725f` — FOUND in `git log --oneline --all`
- `GOTOOLCHAIN=go1.26.4 go build ./... && go vet ./... && go test -count=1 ./...` — exit 0, all packages `ok`
- `git diff --exit-code internal/convert/exec.go` — exits 0 (D-12 unmodified)
- `scripts/sandbox-liveproof.sh html` — `ALL PASS (7 checks)`
- `scripts/conf05-crosscheck.sh html` — `MATCH: html readonly (15) engines (1) readwrite (2)`
- `scripts/ci-helm-checks.sh` — exit 0
- All plan-level `<acceptance_criteria>` re-verified per task, including both required inversions (temporarily removing the `isHTMLTerminal` sentinel check, and temporarily adding `/tmp/x` to `htmlReadOnly`), both reverted cleanly (`git diff` clean on `internal/worker/worker.go` and `internal/convert/htmlsandbox.go` after reversion, save for the intentional GREEN commit already in place)
