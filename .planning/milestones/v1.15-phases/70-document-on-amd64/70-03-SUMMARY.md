---
phase: 70-document-on-amd64
plan: 03
subsystem: infra
tags: [landlock, sandbox, libreoffice, verapdf, preflight, conf05, document-worker]

requires:
  - phase: 70-document-on-amd64
    provides: "socket-only grant kind, measured soffice and veraPDF sets, /tmp route read-write, preflight fixture and cost"
provides:
  - "document class confined in code: soffice and veraPDF each under their own ruleset, built once per task"
  - "engine environments scrubbed of worker secrets, TMPDIR and JAVA_OPTS inside the job directory"
  - "DocumentPreflight: content-judged sufficiency and effectiveness over both engines, run before Postgres"
  - "document-worker refuses to start without Landlock and refuses concurrency above 1 while soffice is granted /tmp read-write"
  - "scripts/sandbox-liveproof.sh document row (four generic checks pass on a fresh image)"
  - "isDocumentTerminal treats ErrEngineSandboxUnavailable as retryable before any text signature"
affects: [70-04, 70-05, internal/convert, internal/worker, cmd/document-worker, Dockerfile.document-worker]

actuals:
  tokens: 19000
  tasks: 2
  commits: 2
plan_head_before: 22d524c2cef7cca0f7d7c35ca9650e7129dc01ae
plan_head_after: fb833a51144b4fc2893cb7a90613c2c80d4df743

tech-stack:
  added: []
  patterns:
    - "Two rulesets per task for one image: each engine gets its own engineExec built from its own path vars, never shared"
    - "A negative preflight run is evidence only because the identical argv and ruleset passed on the inside copy first; judged by content (content.xml marker, isCompliant attribute), never by exit status"

key-files:
  created:
    - internal/convert/documentsandbox.go
    - internal/convert/documentsandbox_test.go
    - .planning/phases/70-document-on-amd64/evidence/liveproof-document-70-03.txt
  modified:
    - internal/convert/sandboxwrap.go
    - internal/convert/libreoffice.go
    - internal/convert/verapdf.go
    - internal/convert/libreoffice_test.go
    - internal/convert/sandboxcoverage_test.go
    - internal/convert/sandboxexec_test.go
    - cmd/document-worker/main.go
    - internal/worker/worker.go
    - internal/worker/engine_sandbox_retryable_test.go
    - scripts/sandbox-liveproof.sh
    - Dockerfile.document-worker

key-decisions:
  - "Route applied: scoped-socket (70-01) with /tmp route read-write (70-02): sofficeExec uses forJobRWSock with rw:/tmp and sock:/tmp; concurrency 1 default and guard keyed on the grant set; sweep of /tmp before every task; no pipe cleanup (pipe residue: absent)"
  - "The veraPDF negative is judged by the presence of an isCompliant attribute (veraPDFHasVerdict), not by parseVeraPDFReport's error, so a denial report that parses cleanly but carries no verdict still counts as a clean denial"
  - "Preflight timeout is the 60 s floor (twice the measured 6.0 s rounds to 20 s, below the floor)"

patterns-established:
  - "Class-closure test-of-the-test: a literal low-level exec in a document class file fails TestConfinedClassesExecOnlyThroughTheSandbox"

requirements-completed: []
requirements-advanced: [CONF-01, CONF-02, CONF-04, CONF-05, CONF-07]

coverage:
  - id: D1
    description: "A freshly built document-worker image, confined by default, logs the landlock line and the sandbox-proven line before it attempts Postgres"
    requirement: CONF-04
    verification:
      - kind: other
        ref: "scripts/sandbox-liveproof.sh document -> PASS: document confined-startup (evidence/liveproof-document-70-03.txt)"
        status: pass
    human_judgment: false
  - id: D2
    description: "Refusal without Landlock naming DOCUMENT_SANDBOX_DISABLED, the escape hatch, and the preflight catching a denied restrict_self"
    requirement: CONF-02
    verification:
      - kind: other
        ref: "scripts/sandbox-liveproof.sh document -> ALL PASS (4 checks)"
        status: pass
    human_judgment: false
  - id: D3
    description: "Both engines' sets equal the conf05 blocks of SANDBOX-PATHS.md, in order"
    requirement: CONF-05
    verification:
      - kind: other
        ref: "scripts/conf05-crosscheck.sh document -> MATCH: soffice, MATCH: verapdf"
        status: pass
      - kind: unit
        ref: "internal/convert/documentsandbox_test.go#TestSofficeReadOnlySetIsTheMeasuredOne (and the six sibling set tests)"
        status: pass
    human_judgment: false
  - id: D4
    description: "Two rulesets per task, scrubbed environments, sentinel through ValidatePDFA, retryable document classifier"
    requirement: CONF-01
    verification:
      - kind: unit
        ref: "internal/convert/documentsandbox_test.go#TestLibreOfficeConvertRunsEachEngineUnderItsOwnRuleset, #TestDocumentEnginesGetAJobScopedTmpAndNoSecrets, #TestValidatePDFAKeepsTheSandboxSentinel; internal/worker/engine_sandbox_retryable_test.go"
        status: pass
    human_judgment: false
  - id: D5
    description: "The preflight actually distinguishes an engine that works inside the job directory from one denied a file outside it"
    requirement: CONF-04
    verification:
      - kind: other
        ref: "live: the same preflight passed on the unmodified image and failed with a named error under a seccomp profile denying landlock_restrict_self (check preflight-catches-restrict-self)"
        status: pass
    human_judgment: true
    rationale: "The negative (denied-outside) half is proven live only by the startup log line 'sandbox proven'; a dedicated adversarial run (an engine deliberately granted the outside directory) was not performed here."

duration: n/a
completed: 2026-09-30
status: complete
---

# Phase 70 Plan 03: Document class confined in code Summary

**soffice and veraPDF now each run under their own Landlock ruleset built once per task, with scrubbed engine environments, a shared-/tmp guard and sweep, a content-judged two-engine startup preflight before Postgres, and the generic live gate passing on a freshly built image.**

## Route applied

- Socket route: **scoped-socket** (70-01). `sofficeExec` is `forJobRWSock(jobDir, sofficeReadWrite, sofficeSocketDirs, sofficeReadOnly, sofficeEngines)`; `forJobRW` is a one-line call with nil socket directories and `spec` a one-line call to `specWithSockets`, so every other class's token stream is unchanged (`TestEngineExecSocketTokensFollowRWAndPrecedeRO` checks a plain `forJobRW` renders no `sock:`).
- `/tmp` route: **read-write** (70-02). Guard keyed on the grant set (`sharedTmpGuard(sofficeReadWrite, ...)`), `DOCUMENT_WORKER_CONCURRENCY` default 1, `ConfinedDocument` sets `sharedTmp`, `Convert` calls `sweepSharedTmp` first of all. `sweepSharedTmp` removes every entry but the job directory, so it covers `OSL_PIPE_*` without matching on the name.
- `pipe residue: absent` on this route, so `removeSofficePipes` and `pipeDir` were **not** written (the plan's socket-only-with-residue branch).
- Tests written for this route: the seven set tests including `TestSofficeSocketDirsAreTheMeasuredOnes`, `TestConfinedDocument` (read-write variant), `TestDocumentPreflightRefusesAConfinedConverterThatDoesNotSweep`, `TestEngineExecSocketTokensFollowRWAndPrecedeRO`. Not written: `TestRemoveSofficePipesRemovesOnlyItsSockets` (residue branch).

## Accomplishments

- `internal/convert/documentsandbox.go` (new): `sofficeReadWrite`, `sofficeSocketDirs`, `sofficeReadOnly`, `sofficeEngines`, `verapdfReadWrite`, `verapdfReadOnly`, `verapdfEngines` equal the conf05 blocks in order; `sofficeEnv` / `verapdfEnv` (PATH, HOME, LANG, LC_ALL when set, `TMPDIR=<job>/tmp`; veraPDF adds `JAVA_OPTS=-Djava.io.tmpdir=<job>/tmp -XX:-UsePerfData`; `engineEnv` untouched); `WithSandbox`, `sofficeExec`, `verapdfExec`, `ConfinedDocument`, `DocumentPreflight`, `documentPreflightSufficiency` / `documentPreflightEffectiveness`, `documentPreflightDOCX` (three parts, as 70-02 measured), `zipPartContains`, `veraPDFHasVerdict`.
- `libreoffice.go`: `Convert` has a value receiver, builds `sx` and `vx` once, runs soffice through `sx.run(ctx, "soffice", ...)`, threads `vx` into `validateDocumentOutput` and `ValidatePDFA`; creates `<job>/tmp`; sweeps first when `sharedTmp` is set.
- `verapdf.go`: `verapdfArgs(path)` shared by production and preflight; `ValidatePDFA(ctx, x engineExec, path)` still parses stdout whatever the exit status; a helper exit 78 reaches the worker as `ErrEngineSandboxUnavailable` inside the "pdf/a validation error" text.
- `cmd/document-worker/main.go`: order is helper mode, signal context, `sandbox.ResolveHelper`, concurrency (default 1) with the guard, `SetVeraPDFTimeout`, one `convert.Default.Replace`, preflight (60 s), then `db.Connect`. `asynq.Config.Concurrency` reads the same variable. No architecture or emulation detection (`grep -ciE 'goarch|uname'` prints 0).
- `internal/worker/worker.go`: `isDocumentTerminal` returns false for the sentinel before anything else (the count of `errors.Is(err, convert.ErrEngineSandboxUnavailable)` in the file is 6).
- `sandboxPendingFiles` is `map[string]string{}`; `libreoffice.go`, `verapdf.go`, `documentsandbox.go` are document class files, and `document` is in the non-vacuity and group loops.
- `scripts/sandbox-liveproof.sh`: `document` row in `class_row`, header and unknown-class message list all eight classes.

## Gate results

- `scripts/sandbox-liveproof.sh document` -> `ALL PASS (4 checks)` on a freshly built image (native arm64): `PASS: document confined-startup` with the lines `🔒 document sandbox: landlock via /usr/local/bin/document-worker __sandbox-exec` and `🔒 document sandbox proven: soffice and veraPDF ran inside the job directory and were denied a file outside it`, then the postgres attempt; `refuses-without-landlock`, `escape-hatch`, `preflight-catches-restrict-self` pass. Transcript: `evidence/liveproof-document-70-03.txt` (with the first, failing run below it).
- `scripts/conf05-crosscheck.sh document` -> `MATCH: soffice readonly (9) engines (1) readwrite (1) socket (1)` and `MATCH: verapdf readonly (9) engines (1) readwrite (1)`.
- `go build ./... && go vet ./... && go test -count=1 ./...` exit 0; `git diff --exit-code internal/convert/exec.go` exit 0.

## Inversions performed

- **Class closure:** temporarily replaced `sx.run(ctx, "soffice", ...)` in `libreoffice.go` and `x.run(vctx, "verapdf", ...)` in `verapdf.go` with a literal `runCommand(...)` -> `TestConfinedClassesExecOnlyThroughTheSandbox` failed with "libreoffice.go calls runCommand directly" and "verapdf.go calls runCommand directly" (plus the non-vacuity and stale-grant lines); reverted, passes. A first attempt of this inversion did not compile (unused `sx`) and was redone with `_ = sx`.
- **Pinned set:** temporarily added `/tmp/x` to `sofficeReadOnly` -> `TestSofficeReadOnlySetIsTheMeasuredOne` failed naming `scripts/document-sandbox-measure.sh`; reverted.
- **Worker sentinel:** the document row was written first and run RED against `isDocumentTerminal` without the check (failed: "document classified a sandbox-unavailable fault as terminal"), then the check added; re-inverted after the fact by removing the check again (same failure) and restored. The control row (same text around a plain "exit status 1") stays terminal, so the pair is not vacuous.

## Task Commits

1. **Task 1: document confined end-to-end (tracer)** - `1c7db1c` (feat)
2. **Task 2: pin the contract, retryable document classifier** - `fb833a5` (test)

The tracer gate (live confined startup with both engines proven) passed before Task 2's tests were written, so nothing was expanded on a broken foundation.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Sweep could not clear the image's root-owned `/tmp/hsperfdata_root`**
- **Found during:** Task 1, first run of `scripts/sandbox-liveproof.sh document`
- **Issue:** the confined worker's preflight failed with `sweep /tmp before the task: unlinkat /tmp/hsperfdata_root: operation not permitted`. The JRE's post-install runs java as root at image build time and leaves `/tmp/hsperfdata_root`; `/tmp` is sticky and the worker runs as `nobody`, so every sweep would fail and every task would retry forever. 70-02's SANDBOX-PATHS.md recorded the directory as image residue (`TMP-RESIDUE` measured relative to it) but no plan step routed it.
- **Fix:** `Dockerfile.document-worker` removes `/tmp/hsperfdata_*` in the same layer as the apt install, so it is gone from the image (verified: `ls -la /tmp` on the built image is empty). The sweep itself was not weakened to tolerate undeletable entries.
- **Files modified:** `Dockerfile.document-worker` (not in the plan's `files_modified`)
- **Verification:** second gate run `ALL PASS (4 checks)`; the first, failing run is kept in the evidence file.
- **Commit:** `1c7db1c`

### Process deviations

**2. [Process] Task 2's worker test row was written before the code, but Task 1's commit precedes it**
- The plan's Task 2 is tdd: its RED run (the document row failing against the unchanged `isDocumentTerminal`) was executed while Task 1's gate was still running; both changes are in Task 2's commit.

**Total deviations:** 1 auto-fixed (Rule 1, image), 1 process. **Impact:** one image-level change outside the listed files; no scope creep in code.

## What 70-04 must know

- `docker-compose.yml:600` and `.env.example:85` still set `DOCUMENT_WORKER_CONCURRENCY` to 2, and `deploy/chart/octoconv/templates/configmap.yaml:47` sets `"2"`: a confined worker started with those values now refuses to start naming `DOCUMENT_WORKER_CONCURRENCY` and the shared `/tmp` (D-09). They must move to 1 (`values-loadproof.yaml` already says 1). That configuration work is 70-04's, as the plan states.
- The document-only live checks (emulated amd64 refusal, /tmp route checks) are 70-04's; only the four generic checks ran here.
- The startup preflight took about 4 s on this host (log timestamps 15:40:52 -> 15:40:56); its timeout is 60 s.

## Issues Encountered

None beyond the deviation above.

## Known Stubs

None.

## Threat Flags

None beyond the plan's threat model (T-70-10 .. T-70-16). Mitigations present: content-judged preflight (T-70-10, T-70-11, T-70-13), allowlisted environments pinned by `TestDocumentEnginesGetAJobScopedTmpAndNoSecrets` (T-70-12), sentinel first in `isDocumentTerminal` (T-70-14), refusal and preflight before Postgres (T-70-15), guard plus sweep at concurrency 1 (T-70-16).

## Self-Check: PASSED

- Files exist: `internal/convert/documentsandbox.go`, `internal/convert/documentsandbox_test.go`, `evidence/liveproof-document-70-03.txt`.
- Commits `1c7db1c` and `fb833a5` exist; `git rev-list --count 22d524c..HEAD` measured 2 before this SUMMARY commit.
- Acceptance greps: `var sandboxPendingFiles = map[string]string{}` count 1; `sx.run(ctx, "soffice"` count 1; `x.run(vctx, "verapdf", verapdfArgs(path)...)` count 1; `JAVA_OPTS=` present in `documentsandbox.go`, 0 in `sandboxwrap.go`; `convert.Default.Replace(` count 1; `Concurrency:    concurrency,` count 1; `errors.Is(err, convert.ErrEngineSandboxUnavailable)` count 6 in `worker.go`; `sandbox.RunExecModeIfRequested` precedes `signal.NotifyContext`, and `ConfinedDocument(`, `SetVeraPDFTimeout(`, `DocumentPreflight(` all precede `db.Connect(`.
- STATE.md and ROADMAP.md untouched.

---
*Phase: 70-document-on-amd64*
*Completed: 2026-09-30*
