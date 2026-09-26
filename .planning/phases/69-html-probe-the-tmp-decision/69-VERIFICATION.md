---
phase: 69-html-probe-the-tmp-decision
verified: 2026-09-27T00:00:00Z
status: human_needed
score: 12/12 must-haves verified (plus 3 human-verification items, none of which are failures)
covered_files:
  - .env.example
  - .planning/REQUIREMENTS.md
  - .planning/phases/69-html-probe-the-tmp-decision/69-01-PLAN.md
  - .planning/phases/69-html-probe-the-tmp-decision/69-01-SUMMARY.md
  - .planning/phases/69-html-probe-the-tmp-decision/69-02-PLAN.md
  - .planning/phases/69-html-probe-the-tmp-decision/69-02-SUMMARY.md
  - .planning/phases/69-html-probe-the-tmp-decision/69-03-PLAN.md
  - .planning/phases/69-html-probe-the-tmp-decision/69-03-SUMMARY.md
  - .planning/phases/69-html-probe-the-tmp-decision/69-CONTEXT.md
  - .planning/phases/69-html-probe-the-tmp-decision/69-RESEARCH.md
  - .planning/phases/69-html-probe-the-tmp-decision/69-REVIEW.md
  - .planning/research/SANDBOX-PATHS.md
  - cmd/chromium-worker/main.go
  - cmd/probe-worker/main.go
  - deploy/chart/octoconv/templates/configmap.yaml
  - deploy/chart/octoconv/templates/deployment-chromium-worker.yaml
  - deploy/chart/octoconv/templates/deployment-probe-worker.yaml
  - docker-compose.yml
  - internal/convert/chromium.go
  - internal/convert/chromium_test.go
  - internal/convert/htmlsandbox.go
  - internal/convert/htmlsandbox_test.go
  - internal/convert/ocrsandbox.go
  - internal/convert/probesandbox.go
  - internal/convert/probesandbox_test.go
  - internal/convert/progressexec.go
  - internal/convert/sandboxcoverage_test.go
  - internal/convert/sandboxexec_test.go
  - internal/convert/sandboxpreflight.go
  - internal/convert/sandboxpreflight_test.go
  - internal/convert/sandboxwrap.go
  - internal/convert/sanitize.go
  - internal/convert/sanitizeconverter.go
  - internal/convert/sanitizeimage.go
  - internal/convert/sanitizeooxml.go
  - internal/convert/sanitizepdf.go
  - internal/convert/sanitizeverify.go
  - internal/e2e/sanitize_ceiling_e2e_test.go
  - internal/e2e/sanitize_confined_e2e_test.go
  - internal/worker/descriptors.go
  - internal/worker/engine_sandbox_retryable_test.go
  - internal/worker/worker.go
  - scripts/conf05-crosscheck.sh
  - scripts/sandbox-liveproof.sh
covered_digest: "v1:sha256:5f3c37f2aa9505dbbb109f310f8f9b26e41f1a39743997a01dbede5da0e500b6"
behavior_unverified: 0
overrides_applied: 0
behavior_unverified_items: []
human_verification:
  - test: "Perform the fourth required class-closure inversion: temporarily restore a literal low-level runner exec of the chromium engine in internal/convert/chromium.go (replacing c.exec(workDir).run(...) with a raw runCommand/runCommandAs call), run `go test -run TestConfinedClassesExecOnlyThroughTheSandbox ./internal/convert/`, confirm it fails naming chromium.go, then revert."
    expected: "The coverage scanner (sandboxcoverage_test.go) fails specifically because chromium.go now contains an unwrapped engine exec; after reverting, `git diff --exit-code` is clean and the test passes again."
    why_human: "69-03-SUMMARY.md records that this specific edit (and a read-only follow-up aimed at an alternative route to the same evidence) was denied by the harness's own security-weakening classifier in that session. I did not attempt it either, per this verification's own instructions. Standing substitute confirmed today: internal/convert/sandboxcoverage_test.go#TestEngineCallScannerCatchesALiteralExec/low-level_runner_call_in_a_class_file (a Phase 68 generic, synthetic-fixture-based self-test of the identical scanner mechanism) passes -- `go test -run TestEngineCallScannerCatchesALiteralExec ./internal/convert/` re-run today, all five subtests PASS. This is supporting evidence for the scanner mechanism, not the literal chromium.go-specific inversion the plan's acceptance criteria names. A human operator needs to either perform the single inversion directly (with an explicit, scoped permission grant) or explicitly accept the generic self-test as sufficient and close this by note."
  - test: "Decide whether CR-01 (69-REVIEW.md) must be fixed before phase 69 is accepted, or tracked as a follow-up."
    expected: "A conscious decision recorded (fix now / open a DEBT item / accept as known risk), not silence."
    why_human: "Confirmed still present in the current tree: injectCSPFirst (internal/convert/chromium.go:99-113) matches a bare `<head`/`<html` substring textually and splices the CSP meta tag there, but HTML5's implicit-head-creation means a <script> placed BEFORE the literal <head>/<html> tag executes during DOM construction before the parser ever reaches that later literal tag -- the CSP lands in the DOM after the script already ran. Only two regression tests exist (TestInjectCSPFirstPrecedesInHeadScript, TestInjectCSPFirstNoHeadFallsBackToHTMLOpen), both confirmed today to place the adversarial <script> AFTER the injection point, neither covering the gap. This predates phase 69 (introduced in commit a016c15, phase 15) and chromium.go's phase-69 commit (d25bf62) did not touch injectCSPFirst. My own judgment: this does NOT invalidate phase 69's own success criteria (the Landlock ruleset, the content-judged preflight, and the wrapper/interpreter/binary grant all hold independently of whether script executes -- Landlock still denies any file read outside the measured grant set regardless of what the renderer's own script does). But it is a live, unresolved CRITICAL finding in the same engine class this phase confines, and D-05's own doc comment states the CSP meta tag is the ONLY JS-disable mechanism in this build -- a working bypass means the 'file:// subresources and iframes are resolved by the engine' trust boundary this phase's own threat table names is defended by Landlock's read-only set alone, not by both CSP and Landlock as the plan intended."
  - test: "Decide whether WR-01 (69-REVIEW.md) -- the deployment-chromium-worker.yaml /dev/shm comment/mount, unreconciled with this phase's own SANDBOX-PATHS.md measurement that /dev/shm is not needed (verified by removal) -- should be resolved in this phase or deferred."
    expected: "Either the /dev/shm volume/mount is dropped (the ruleset was measured sufficient without it), or the comment is corrected to explain why a production-scale document might still need it despite the measurement, with a cross-reference so the contradiction is not left for a future reader to notice independently."
    why_human: "Confirmed still present today: deployment-chromium-worker.yaml:9-12's comment says 'Even with --disable-dev-shm-usage some Chromium internal paths still touch /dev/shm,' while SANDBOX-PATHS.md's html section says '/dev/shm и /run в наборе не нужны — проверено снятием' and htmlReadOnly/htmlReadWrite grant neither path. This file is part of this phase's own changeset (the extraEnv block was added here in plan 01) but the /dev/shm rationale was not revisited. Under Landlock, a genuine /dev/shm touch on a production-scale document (never exercised by the preflight's small fixture) would be silently denied, turning into a job failure the live gate's own fixture cannot catch -- an operational risk distinct from, but adjacent to, CONF-05's own guarantee (which the six-class conf05-crosscheck confirms code and doc otherwise agree on)."
---

# Phase 69: HTML, Probe & the /tmp Decision Verification Report

**Phase Goal:** `html` и `probe` под Landlock, и решение по `/tmp` принято и записано с причиной (ROADMAP.md, Phase 69).
**Verified:** 2026-09-27
**Status:** human_needed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

All truths below were checked against the actual codebase (not SUMMARY prose) by reading the
implementation, re-running the pinned unit/coverage tests, re-running `scripts/conf05-crosscheck.sh`,
and — for the two most load-bearing claims — independently rebuilding the `chromium-worker` and
`probe-worker` images from this tree and either reading their live startup logs or re-running
`scripts/sandbox-liveproof.sh html probe` myself (not merely reading the SUMMARY's transcript).

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | ROADMAP SC1: the `/tmp` decision is taken and recorded with its reason (CONF-06, D-01..D-05) | ✓ VERIFIED | `.planning/research/SANDBOX-PATHS.md` lines 243-300 (`## Решение по /tmp (CONF-06, фаза 69)`): D-01 (concurrency 1), D-02 (rejected alternatives with `unshare -Urm` EPERM measurement), D-03 (startup guard), D-04 (sweep), D-05 (probe drops `/tmp`, `gs_*` strace measurement) — all present, in Russian, with measurement references, written before any code per the plan |
| 2 | ROADMAP SC2: both classes convert under their rulesets; effectiveness proven by result **content**, not file existence | ✓ VERIFIED | `internal/convert/htmlsandbox.go` (`HTMLPreflight`, judged by PDF `/Title` marker) and `internal/convert/probesandbox.go` (`ProbePreflight`, judged by `gs`/`pdfinfo`/`unzip`/`file` output content) read directly; both preflights explicitly discard `chromium`'s and `file(1)`'s exit status. Independently re-ran `scripts/sandbox-liveproof.sh html probe` today (fresh images, commit `0c90bb6`): `ALL PASS (11 checks)`, including `PASS: html preflight-catches-restrict-self` and `PASS: probe preflight-catches-restrict-self` |
| 3 | ROADMAP SC3: the chromium grant carries the wrapper, its interpreter, and the real binary (D-08) | ✓ VERIFIED | `internal/convert/htmlsandbox.go` lines 26-52 (`htmlReadOnly` includes `/usr/bin/dash` literally, `/usr/lib` covers the real binary, `htmlEngines` resolves the wrapper via PATH); live gate `PASS: html wrapper-as-measured` re-run today |
| 4 | CONF-06 concurrency guard: a confined class granting the shared `/tmp` refuses to start above concurrency 1, keyed on the grant set | ✓ VERIFIED | `internal/convert/sandboxwrap.go` `sharedTmpGuard` (lines 204-221); `TestSharedTmpGuard` and `TestEngineSandbox*` pass (re-run today); live gate `PASS: html concurrency-guard` reproduced live with the exact refusal text |
| 5 | CONF-06 sweep: the shared `/tmp` is swept before every html task, keeping only the current job | ✓ VERIFIED | `internal/convert/htmlsandbox.go` `sweepSharedTmp`; live gate `PASS: html sweeps-shared-tmp` |
| 6 | CONF-02: both workers refuse to start without Landlock, naming the escape-hatch variable, and the escape hatch is reachable from compose and the Helm chart, off by default | ✓ VERIFIED | `cmd/chromium-worker/main.go` / `cmd/probe-worker/main.go` read directly: `sandbox.ResolveHelper` before `db.Connect`; `docker-compose.yml:699,1484` forward `"${HTML_SANDBOX_DISABLED:-}"` / `"${PROBE_SANDBOX_DISABLED:-}"`; `.env.example` documents both, unset; chart `extraEnv` blocks present, `values.yaml` unchanged. Independently rebuilt both images and observed the live refusal text with Landlock unavailable in this sandboxed dev environment: `"html sandbox: landlock is unavailable on this kernel..."` / same for probe |
| 7 | CONF-04: preflight ordering — helper resolution and preflight precede `db.Connect`/queue registration | ✓ VERIFIED | Both `main.go` files read line-by-line: `RunExecModeIfRequested` first, then `ResolveHelper`/guard/`ConfinedChromium`/`ProbeConcurrencyGuard`, then `HTMLPreflight`/`ProbePreflight`, then `db.Connect` |
| 8 | Sandbox faults are retryable, never blamed on the client's file, for both classes | ✓ VERIFIED | `isHTMLTerminal` (`internal/worker/worker.go:253-267`) and `isProbeTerminal` (`internal/worker/descriptors.go:311-323`) both check `errors.Is(err, convert.ErrEngineSandboxUnavailable)` first; `TestEngineSandboxUnavailableIsRetryableForEveryConfinedClass` and its control table re-run today, all six classes PASS |
| 9 | CONF-01/CONF-05: html/probe granted sets equal the measured document, held by a test, not a comment | ✓ VERIFIED | `internal/convert/htmlsandbox.go`/`probesandbox.go` var literals read directly; `scripts/conf05-crosscheck.sh pandoc ocr av audio html probe` re-run today: six `MATCH:` lines, `html readonly (15) engines (1) readwrite (2)`, `probe readonly (10) engines (4) readwrite (0)` |
| 10 | Coverage allowlist reduced to exactly phase 70's files; every html/probe exec goes through `engineExec` | ✓ VERIFIED | `sandboxPendingFiles` in `sandboxcoverage_test.go` holds exactly `libreoffice.go`/`verapdf.go`; `TestConfinedClassesExecOnlyThroughTheSandbox` re-run today, PASS |
| 11 | Real jobs of every sanitize route (pdf, docm→docx, jpg) and both html e2e tests complete through the confined workers of a stack rebuilt from this tree | ✓ VERIFIED | `internal/e2e/sanitize_confined_e2e_test.go` exists, compiles, and is exercised (skips only for lack of `E2E_BASE_URL`, confirmed by running it locally today). Persisted transcript `.planning/ci-local/ci-local-20260926T200730Z.log` (commit `c033ba4` + the two tracked-dirt e2e files that became commit `044aa7d`) shows a `--build` stack rebuild followed by `ok github.com/apaderin/octoconv/internal/e2e 141.782s` and `### tier e2e: PASS` — a real transcript, not SUMMARY narrative alone |
| 12 | Full gate/helm tiers pass on this commit | ✓ VERIFIED | `.planning/ci-local/ci-local-20260926T203743Z.log` (commit `044aa7d`): `PASS gate`, `PASS helm`; independently re-ran `GOTOOLCHAIN=go1.26.4 go build ./... && go vet ./... && go test -count=1 ./...` today — all packages `ok`, `internal/convert` 73s, `internal/worker` 12.5s |
| — | Class-closure inversions: each guard shown to fail when the thing it guards is broken | ⚠️ See Human Verification #1 | 3 of 4 required inversions performed and reverted in the recorded session with concrete failing-test output quoted in `69-03-SUMMARY.md`; the fourth (a literal low-level exec restored in `chromium.go`) was blocked by the harness's security-weakening classifier and not performed. Not attempted by this verifier either, per this task's own instruction |

**Score:** 12/12 checkable truths verified directly against the codebase; 0 failed; 1 truth (the fourth
inversion) routed to human verification because it cannot be completed without either a scoped
permission grant or an explicit acceptance of a partial substitute — see below.

### Code Review Findings Carried Forward (69-REVIEW.md)

`69-REVIEW.md` (dated after all three plans' commits) found one CRITICAL and one WARNING issue,
both confirmed still present in the tree today. Neither is a phase-69 must-have (none of the
declared `must_haves.prohibitions` across the three plans concern CSP/script execution or the
`/dev/shm` mount), and neither breaks any of the Landlock/tmp-decision truths verified above — but
both are real, live findings the developer should consciously decide on rather than let this
verification silently pass over. See `human_verification` items #2 and #3.

- **CR-01 (critical, pre-existing, phase 15)**: `injectCSPFirst`'s JS-disable CSP can be bypassed
  by a `<script>` placed before the literal `<head>`/`<html>` tag (HTML5 implicit-head-creation
  executes it before the parser reaches the later literal tag). Confirmed unfixed in
  `internal/convert/chromium.go:99-113` today; confirmed only two regression tests exist, neither
  covering this gap (`internal/convert/chromium_test.go:70,96`).
- **WR-01 (warning)**: `deploy/chart/octoconv/templates/deployment-chromium-worker.yaml`'s
  `/dev/shm` comment/mount is unreconciled with this phase's own `SANDBOX-PATHS.md` measurement
  that `/dev/shm` is not needed (verified by removal). Confirmed unreconciled today.

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/convert/htmlsandbox.go` | measured html grant, `ConfinedChromium`, `sweepSharedTmp`, `HTMLPreflight` | ✓ VERIFIED | read in full; matches every plan claim |
| `internal/convert/probesandbox.go` | measured probe grant, `ProbeConcurrencyGuard`, `ProbePreflight` (sufficiency+effectiveness) | ✓ VERIFIED | read in full; matches every plan claim |
| `internal/convert/chromium.go` | `Sandbox`/`sharedTmp` fields, `chromiumPrintArgs`, `exec`, sweep-then-Convert | ✓ VERIFIED | read in full |
| `internal/convert/sandboxwrap.go` | `forJobRW`, `withEnv`, `engineEnv`, `sharedTmpDir`/`sharedTmpGuard` | ✓ VERIFIED | read in full |
| `internal/convert/sandboxpreflight.go` | `requireAbsent`, `newPreflightWorkspaceOutside`, `requireOutsideGrants`, `requireContains`/`requireLacks` | ✓ VERIFIED | read in full |
| `cmd/chromium-worker/main.go`, `cmd/probe-worker/main.go` | helper→guard→register→preflight, all before `db.Connect` | ✓ VERIFIED | read in full; ordering confirmed by line position |
| `internal/e2e/sanitize_confined_e2e_test.go` | `TestSanitizeOOXMLAndImageThroughTheConfinedWorkerE2E` | ✓ VERIFIED | exists, compiles, present in `go test -v ./internal/e2e/` output (skipped only for lack of a live stack) |
| `.planning/research/SANDBOX-PATHS.md` | CONF-06 decision section, `conf05:html:*`/`conf05:probe:*` blocks, "да (фаза 69)" | ✓ VERIFIED | read in full |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `cmd/chromium-worker/main.go` | `internal/convert/htmlsandbox.go` | same concurrency value for guard and asynq | ✓ WIRED | `concurrency := envInt(htmlConcurrencyEnv, 1)` feeds both `ConfinedChromium(...)` and `asynq.Config{Concurrency: concurrency}` |
| `internal/convert/chromium.go` | `internal/convert/sandboxwrap.go` | `forJobRW` | ✓ WIRED | `c.Sandbox.forJobRW(workDir, htmlReadWrite, htmlReadOnly, htmlEngines)` |
| `internal/convert/htmlsandbox.go` | `internal/convert/chromium.go` | preflight negative run uses `chromiumPrintArgs` | ✓ WIRED | `chromiumPrintArgs(w.jobDir, outOutside, "file://"+outside)` in `HTMLPreflight` |
| `cmd/probe-worker/main.go` | `internal/convert/probesandbox.go` | preflight on the same registered converter | ✓ WIRED | `conv` built once, passed to `ProbeConcurrencyGuard`, `convert.Default.Replace(conv)`, and `ProbePreflight(preflightCtx, conv)` |
| `internal/worker/descriptors.go`/`worker.go` | `internal/convert/sandboxwrap.go` | sentinel checked first in both classifiers | ✓ WIRED | confirmed by reading both functions and re-running the retryable test table |
| `scripts/conf05-crosscheck.sh` | `.planning/research/SANDBOX-PATHS.md` | doc blocks vs Go vars, all six confined classes | ✓ WIRED | re-ran today, six `MATCH:` lines |

### Behavioral Spot-Checks / Live Gate (re-run by this verifier, not read from a transcript alone)

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Fresh chromium-worker + probe-worker images log "sandbox proven" before Postgres, no unconfined warning | `docker compose up -d --build chromium-worker probe-worker` then `docker compose logs chromium-worker probe-worker` (run by this verifier today, commit `0c90bb6`) | `🔒 html sandbox proven: ...` and `🔒 probe sandbox proven: ...` both present; `HTML_WORKER_CONCURRENCY=1` present; no "the engine can read every file" line | ✓ PASS |
| Full html+probe live gate on freshly built images | `scripts/sandbox-liveproof.sh html probe` (run by this verifier today) | `ALL PASS (11 checks)` — confined-startup, refuses-without-landlock, escape-hatch, preflight-catches-restrict-self ×2, concurrency-guard, wrapper-as-measured, sweeps-shared-tmp | ✓ PASS |
| Six-class doc/code cross-check | `scripts/conf05-crosscheck.sh pandoc ocr av audio html probe` (run by this verifier today) | six `MATCH:` lines | ✓ PASS |
| Full test suite on this commit | `GOTOOLCHAIN=go1.26.4 go build ./... && go vet ./... && go test -count=1 ./...` (run by this verifier today) | all packages `ok`, no failures | ✓ PASS |
| Coverage/inversion-adjacent unit tests | `go test -v -run TestConfinedClassesExecOnlyThroughTheSandbox\|TestEngineSandbox\|TestEngineCallScannerCatchesALiteralExec ./internal/convert/ ./internal/worker/` (run by this verifier today) | all PASS | ✓ PASS |
| `internal/convert/exec.go` byte-identical across the phase (D-12) | `git diff --exit-code internal/convert/exec.go` (run by this verifier today) | exit 0 | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|------------|-------------|--------|----------|
| CONF-01 | 69-01, 69-02, 69-03 | engine cannot read outside job dir + grants | ✓ SATISFIED (for html/probe; requirement spans phases 67-70, correctly "In Progress" in REQUIREMENTS.md pending phase 70) | preflight negatives, live gate, code read |
| CONF-02 | 69-01, 69-02 | refuse to start, name the variable | ✓ SATISFIED (html/probe half) | both `main.go` files, live refusal text reproduced today |
| CONF-04 | 69-01, 69-02, 69-03 | preflight on the real engine, judged by output | ✓ SATISFIED (html/probe half) | `HTMLPreflight`/`ProbePreflight` content judgments |
| CONF-05 | 69-01, 69-02, 69-03 | granted set equals `SANDBOX-PATHS.md`, held by a test | ✓ SATISFIED (html/probe half) | pinned tests + conf05-crosscheck re-run |
| CONF-06 | 69-01, 69-02 | `/tmp` decision for html and probe | ✓ SATISFIED, marked **Complete** in REQUIREMENTS.md (phase 69 is its only owner) | CONF-06 decision section, guard, sweep, empty `probeReadWrite` |

No orphaned requirements found: REQUIREMENTS.md's phase mapping (`CONF-01,02,04,05,06`) matches
what all three plans declare in frontmatter.

### Anti-Patterns Found

None of TBD/FIXME/XXX/TODO/HACK/PLACEHOLDER found in any file this phase modified (checked by grep
across every file in `covered_files` under `internal/`, `cmd/`, `scripts/`). No stub returns, no
hardcoded empty data flowing to output. The two carried-forward code-review findings (CR-01, WR-01)
are documented above rather than repeated here as anti-patterns, since they were already
characterized with file:line evidence by `69-REVIEW.md` and independently reconfirmed by this
verifier.

## Gaps Summary

No must-have truth failed. Every Landlock/`/tmp`-decision claim in the three plans and the ROADMAP's
three success criteria was checked against the actual code, re-run tests, and (for the two most
load-bearing claims) a live rebuild-and-observe pass performed by this verifier independently of the
SUMMARY narratives — all held.

Three items are routed to human verification, none of which are code defects this verifier can fix
by itself without violating explicit instructions or without a project-level policy decision:

1. One of four required class-closure inversions (proving the coverage scanner fails on a literal
   low-level exec restored specifically in `chromium.go`) could not be performed in the executing
   session and was not attempted by this verifier either, per this task's own instruction. A generic
   Phase-68 self-test of the identical scanner mechanism stands as a partial, non-literal substitute
   and was re-confirmed passing today.
2. CR-01 (critical, pre-existing the phase): the html engine's only JS-disable mechanism can be
   bypassed by script placed before the literal `<head>`/`<html>` tag. Does not break this phase's
   own Landlock/tmp-decision success criteria, but is a live, unresolved security finding in the
   same engine class this phase confines, and needs a conscious decision (fix now vs. tracked
   follow-up), not silence.
3. WR-01 (warning): a stale `/dev/shm` rationale in a file this phase itself modified, contradicting
   this phase's own measurement. Needs a conscious decision (drop the mount vs. correct the comment).

---
*Verified: 2026-09-27*
*Verifier: Claude (gsd-verifier)*
