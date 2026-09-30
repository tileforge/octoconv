---
phase: 70-document-on-amd64
verified: 2026-10-01T01:10:00Z
status: passed
human_approved: 2026-10-01 (user approved human-check items 1-4: measured sets, emulated-amd64 refusal, final evidence set, html /dev/shm control redesign; residuals tracked as pending todos)
score: 15/15 must-haves verified
covered_files:
  - .env.example
  - Dockerfile.chromium-worker
  - Dockerfile.document-worker
  - cmd/audio-worker/main.go
  - cmd/av-worker/main.go
  - cmd/chromium-worker/main.go
  - cmd/document-worker/main.go
  - cmd/ocr-worker/main.go
  - cmd/pandoc-worker/main.go
  - cmd/probe-worker/main.go
  - cmd/tectonic-worker/main.go
  - cmd/worker/main.go
  - deploy/chart/octoconv/templates/configmap.yaml
  - deploy/chart/octoconv/templates/deployment-document-worker.yaml
  - docker-compose.e2e.yml
  - docker-compose.yml
  - internal/convert/chromium_live_test.go
  - internal/convert/devshm_observer_linux_test.go
  - internal/convert/devshm_observer_other_test.go
  - internal/convert/documentsandbox.go
  - internal/convert/documentsandbox_test.go
  - internal/convert/htmlsandbox_test.go
  - internal/convert/libreoffice.go
  - internal/convert/libreoffice_test.go
  - internal/convert/sandboxexec_test.go
  - internal/convert/sandboxwrap.go
  - internal/convert/verapdf.go
  - internal/e2e/testdata/verapdf-shim/soffice
  - internal/sandbox/execmode.go
  - internal/sandbox/sandbox.go
  - internal/sandbox/sandbox_linux.go
  - internal/worker/worker.go
  - scripts/conf05-crosscheck.sh
  - scripts/document-sandbox-measure.sh
  - scripts/sandbox-liveproof.sh
  - scripts/verapdf-differential.sh
covered_digest: "v2:sha256:a2808f73c9a00bd3b8f8ca4843a1c4036c017b6be63415b7541a92b59418acc7"
behavior_unverified: 0
overrides_applied: 0
re_verification:
  previous_status: gaps_found
  previous_score: 9/11
  gaps_closed:
    - "Eight-class live gate red on html large-document-without-dev-shm (closed by 70-06 control rebuild, 70-09 gate ALL PASS (40 checks))"
    - "WR-01: sharedTmpGuard bypassed for concurrency < 1 (closed by 70-07; refusal, not clamp)"
  gaps_remaining: []
  regressions: []
gaps: []
deferred: []
behavior_unverified_items: []
coincidental_reliance_items: []
human_verification:
  - test: "Approve the measured soffice and veraPDF sets (70-02), now including the 70-08 narrowing of veraPDF's /proc: SANDBOX-PATHS.md document section, evidence/document-sandbox-measure.txt, evidence/verapdf-proc-measure-70-08.txt"
    expected: "Every kept entry has a failing removal run or the V0/V0L reason; veraPDF's read-only set is 11 entries with /proc/cgroups, /proc/self/cgroup, /proc/self/mountinfo (each fails V0 and V0L when removed, the other six opened /proc entries do not); the rw:/tmp route and socket right match what was measured; veraPDF writes only the job directory and /dev/null"
    why_human: "70-02 plan human-check: the user approves the measured set that the pinned tests and the cross-check hold the code to. The set changed after the last approval-ready state (70-08), so approve the final one"
  - test: "Approve the emulated-amd64 refusal evidence (70-04): evidence/liveproof-document-70-04.txt and, more recent, the `PASS: document emulated-amd64-refuses` block in evidence/liveproof-8class-70-09.txt"
    expected: "Both refusals name DOCUMENT_SANDBOX_DISABLED and stop before Postgres; the emulated image says `landlock is unavailable on this kernel`; concurrency-guard and sweeps-shared-tmp match the rw:/tmp route; 7 document checks"
    why_human: "70-04 plan human-check: ROADMAP criterion 3 (including emulation) and CONF-02 are proven only by running the built images; the verifier did not rebuild images or start compose and relies on committed transcripts"
  - test: "Approve the final evidence set (70-09 human-check), read in this order: evidence/liveproof-8class-70-09.txt (commit 5db6c74 on every class, nine image ids, html's `dev-shm-observe: control=1 production=0`), e2e-70-09.txt, inversions-70-09.txt, then 70-06 (chromium-devshm-measure-70-06.txt, liveproof-html-70-06.txt), 70-07 (startup-guards-70-07.txt) and 70-08 (verapdf-proc-measure-70-08.txt); liveproof-8class-70-05.txt / e2e-70-05.txt / inversions-70-05.txt are the superseded red set"
    expected: "Gate ends ALL PASS (40 checks), 40 PASS lines, no FAIL / NOT RUN; every inversion fails for its stated reason; nothing proven on a cached result; the 70-05 approval item is replaced by this one"
    why_human: "The live proof is the phase's acceptance and was not re-run by the verifier (no compose, no image rebuilds by instruction). Not self-approved by the executor"
  - test: "Accept or reject the html negative-control redesign (70-06): the control is now an inotify observation with /dev/shm reachable, whose control variant is a dash child (not Chromium) that creates one entry; Chromium stays unpinned"
    expected: "User accepts that the claim 'html needs no /dev/shm' is now held by `production creates 0 entries while the method provably sees 1`, measured on Chromium 154.0.8037.57 (it creates nothing under /dev/shm in any of 7 conditions; shared memory via memfd_create). Known limit: behaviour when memfd_create is denied (seccomp) was not measured; the --disable-dev-shm-usage flag is no longer shown load-bearing (inversions ii and iii could not run on this classification, recorded)"
    why_human: "A judgment on what the control proves. It replaces phase 69's 'must fail when denied' control, which a Chromium fallback defeated; the user decided 2026-09-30 not to pin Chromium"
  - test: "Decide the /tmp route residual: soffice holds rw:/tmp because MAKE_SOCK alone did not suffice; the narrower `sock:` grant with MAKE_SOCK + REMOVE_FILE was not measured or built (todo 2026-09-30-soffice-socket-grant-remove-file-alternative.md)"
    expected: "User accepts rw:/tmp with concurrency exactly 1 (guard now refuses <1 and >1) plus the sweep, or asks for the narrower grant as a separate change to the shared wire format"
    why_human: "Changes the shared sandbox wire-format semantics; 70-02 reserved it for the user. User decision 4 already routed it to a todo"
  - test: "Decide the html /proc residual (todo 2026-09-30-proc-grant-other-process-entries.md) and accept the corrected WR-02 finding"
    expected: "WR-02 as written (same-uid JVM reads worker/PID 1 environ) was measured false even under whole-/proc: N3 with a canary proven readable unconfined shows environ of PID 1 and the parent DENIED, cmdline/status of other processes OPENED (veraPDF baseline and html). veraPDF is narrowed so cmdline/status are now denied too. html keeps ro:/proc (chromium re-execs itself, so /proc/self entries cannot cover children) and still exposes other processes' cmdline/status; severity medium. The denial mechanism (Landlock ptrace check) is inferred, not measured, so N3 should be re-run on the deployment kernel"
    why_human: "A security trade-off; not a violation of CONF-01's wording; user decision 3 kept html and tectonic out of 70-08's scope"
  - test: "First amd64 deployment: follow .planning/todos/pending/2026-09-29-amd64-first-deployment-preflight-check.md"
    expected: "All eight confined classes log 'sandbox proven' on a real amd64 node; any class that refuses is re-measured there"
    why_human: "Out of phase-70 scope (D-05); measurement and gate ran on arm64 only"
  - test: "Bookkeeping the orchestrator owns, not a code issue: REQUIREMENTS.md still shows CONF-02/04/05 `[ ]` (In Progress) and CONF-07 `[ ]` (Pending); ROADMAP.md Phase 70 checkbox is open; the pending todos from 70-09 (minio-images-off-docker-hub, sweep-tests-must-not-touch-host-tmp) are user-scheduled follow-ups"
    expected: "After the user approves the items above, tick CONF-02/04/05/07 and mark Phase 70 complete"
    why_human: "Status transitions belong to the user's approval"
---

# Phase 70: Document on amd64 Verification Report

**Phase Goal (ROADMAP, rewritten per D-04 in 70-05):** `document` under Landlock: both engines, LibreOffice and veraPDF. The amd64 pin was the veraPDF build stage only; the image is native, measured and gated on this host. Criteria: (1) LibreOffice and veraPDF path sets measured with the same tool on the native image, after the veraPDF differential cleared the pin; (2) conversion passes under the ruleset and a file outside is unreachable; (3) the worker refuses to start without Landlock, including when the cause is emulation, not the kernel.
**Verified:** 2026-10-01
**Status:** human_needed
**Re-verification:** Yes, after gap closure (70-06..70-09). Previous: gaps_found, 9/11.

Both gaps are closed against the code and the committed evidence. No FAILED truth remains and no regression was found in the original nine. The status is `human_needed` only because the plans' human-checks (measured sets, emulated refusal, final evidence set) are explicitly reserved for the user and the verifier did not rebuild images or start compose.

## Gap closure

### Gap 1: eight-class gate red on html /dev/shm control - CLOSED

- `evidence/liveproof-8class-70-09.txt`: last line `ALL PASS (40 checks)`; `grep -c '^PASS: '` = 40 (verifier ran it); no `FAIL` or `NOT RUN` line; eight `commit: 5db6c74da4794eee51a10baa59c90fb68d10ffaa` build blocks plus the document one, each with its own image id; `PASS: html large-document-without-dev-shm` present.
- The commit is the current code: `git diff 5db6c74 HEAD` outside `.planning` is empty (verifier ran it). Everything the gate built is therefore what is in the tree now.
- Root cause was a control that inferred a dependency from a failure under denial. Chromium 154 falls back to `memfd_create`, so the failure vanished. 70-06 measured seven conditions (`evidence/chromium-devshm-measure-70-06.txt`: 0 creates under /dev/shm in every one), then rebuilt step e of `TestChromiumLargeDocumentNeedsNoDevShmLive` (read by the verifier, `chromium_live_test.go:333-520`): inotify observer outside the sandbox (`devshm_observer_linux_test.go`, IN_Q_OVERFLOW counted as activity, `!linux` stand-in fails rather than skips) over the real `/dev/shm`, a control that must be seen (`created ... observer saw nothing -- the method cannot see a /dev/shm dependency`), then the production argv under the same ruleset must render the marker with zero entries. The PASS line carries `dev-shm-observe: control=1 production=0` and the Chromium version.
- Failure shown, not assumed: 70-06 inversion (i) blind observer FAILS with the control message; (iv) a stray `/dev/shm` entry FAILS the production assertion. Inversions (ii) and (iii) could not run by construction and are recorded as such. `htmlsandbox.go` and `chromium.go` are unchanged; `htmlReadWrite` does not contain `/dev/shm` (conf05 html MATCH).
- Chromium stays unpinned by user decision; `Dockerfile.chromium-worker` diff since 18c805d is a 4-line comment only (`chromium-headless-shell=` is absent). The gate no longer depends on a version-specific failure, so a real future `/dev/shm` use would make it red for the right reason.

### Gap 2: WR-01 concurrency < 1 bypassing sharedTmpGuard - CLOSED

- `sharedTmpGuard` (`internal/convert/sandboxwrap.go:247`) now returns nil only for an unconfined sandbox, a writable set without `/tmp`, or `concurrency == 1`; below 1 returns its own refusal naming `<ENV>=<n>` and the shared /tmp; above 1 unchanged. Refused, not clamped (the prohibition in the gap plan).
- Both entry points share it (`documentsandbox.go:221` ConfinedDocument; ConfinedChromium via the same function). `cmd/document-worker/main.go:78-80,177` and `cmd/chromium-worker/main.go:65-67,150` pass the one `concurrency` value to the guard and to asynq, so the asynq-NumCPU path is unreachable when confined.
- Tests (run by the verifier, all PASS): `TestSharedTmpGuard` rows `confined_shared_/tmp_concurrency_0` and `_-1`, with controls (unconfined, no-/tmp set stay nil), `TestConfinedDocument`, `TestConfinedChromium`. RED run against the old guard was recorded in 70-07-SUMMARY.
- Live: `evidence/startup-guards-70-07.txt` part 1, images built from 3be14e2: document and chromium each at 0 and -1 exit 1 with `<VAR>=<n> with the sandbox enabled: asynq treats a concurrency below 1 as the CPU count...`, no `sandbox proven`, no `postgres:` line. The script requires a non-zero exit, so a run that never reached the worker cannot pass (70-07 deviation 4 fixed the first vacuous run). The above-1 text is unchanged, so the gate's `concurrency-guard` check still matches (PASS in the final gate).
- Probe/pandoc/etc. grant no `/tmp`, so the guard does not apply to them by design (keyed on the grant set).

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | SC1: soffice and veraPDF path sets measured with the same tool on the native image, after the differential cleared the pin | VERIFIED | Unchanged from initial report (differential `DIVERGENCES: 0`, measure driver). Regression check: verifier re-ran `scripts/conf05-crosscheck.sh` for all eight classes, eight MATCH lines. veraPDF now 11 read-only entries after 70-08; re-measured by a 14-check driver run (`ALL PASS (14 checks)`, `evidence/verapdf-proc-measure-70-08.txt`) |
| 2 | SC2: conversion passes under the ruleset and a file outside is unreachable | VERIFIED | Driver N1 (no output carrying the fixture text although `/outside/in.docx` is identical) and N2 (`/outside/in.pdf (Permission denied)`), C1; `evidence/e2e-70-09.txt` on a stack built from 033978c (includes 70-08): 52 `--- PASS`, 0 FAIL/SKIP, TestDocumentConversionE2E, TestPDFAExportE2E (+NoOptsRegression), TestPDFANonCompliantE2E pass through the confined worker. Committed transcripts, not re-run by the verifier |
| 3 | SC3: worker refuses to start without Landlock, including under emulation | VERIFIED | `PASS: document refuses-without-landlock` and `PASS: document emulated-amd64-refuses` in the 70-09 gate (`landlock is unavailable on this kernel ... Set DOCUMENT_SANDBOX_DISABLED=true`); startup order in `cmd/document-worker/main.go` is helper mode, ResolveHelper, ConfinedDocument, preflight, then db.Connect (concurrency guard now also before Postgres, shown live) |
| 4 | Image is native; the pin covers only the veraPDF build stage | VERIFIED | No regression: `Dockerfile.document-worker` and compose untouched by 70-06..09 outside the unrelated comment; diff since 18c805d in Dockerfile/compose/deploy is the chromium comment only |
| 5 | CONF-05: granted sets equal the measured ones and are held by a test | VERIFIED | Eight MATCH lines re-run; `TestSofficeReadOnlySetIsTheMeasuredOne`, `TestVerapdfReadOnlySetIsTheMeasuredOne` PASS; `evidence/inversions-70-09.txt`: `/tmp/x` added to `verapdfReadOnly` fails the pinned test and the cross-check (exit 1), reverted |
| 6 | CONF-04: preflight on the real engines, judged by content | VERIFIED | `DocumentPreflight` unchanged in logic; now derived from the signal ctx and refuses whitespace TMPDIR first; gate PASS `preflight-catches-restrict-self` (exit 78 surfaces); `🔒 document sandbox proven:` in e2e startup lines |
| 7 | CONF-02 reachability: escape hatch named, forwarded, never defaulted to true | VERIFIED | Verifier grep: only `.env.example:108` (commented) and a chart comment mention `DOCUMENT_SANDBOX_DISABLED=true`; compose passes `${DOCUMENT_SANDBOX_DISABLED:-}`; e2e overlay does not set it (`yq ... has(...)` false per 70-09) |
| 8 | Shared-/tmp guarantee: a confined document worker never runs other than one task while soffice holds rw:/tmp | VERIFIED (was FAILED) | Gap 2 closure above; code read, tests run, live refusals at 0 and -1 |
| 9 | Eight-class live gate ends ALL PASS on images rebuilt from this tree | VERIFIED (was FAILED) | Gap 1 closure above; 40 PASS, final line `ALL PASS (40 checks)`, commit 5db6c74 = current non-.planning tree |
| 10 | CONF-07 / ROADMAP wording says what was done and keeps the emulation reason visible | VERIFIED | CONF-07 text in REQUIREMENTS.md read (stage-only pin, native image, `DIVERGENCES: 0` path, arm64 host, emulated image still refuses, D-05 todo exists). Checkbox is still open, see human item |
| 11 | `internal/convert/exec.go` untouched (D-13); sandbox-unavailable retryable for document | VERIFIED | `git diff --exit-code 7744ddf -- internal/convert/exec.go` exit 0 (verifier ran); worker tests PASS |
| 12 | 70-06: a /dev/shm control a fallback cannot defeat; html gate green; SANDBOX-PATHS html section and the debug record corrected | VERIFIED | See Gap 1. Control observes with /dev/shm reachable; both failing inversions in `liveproof-html-70-06.txt`; `Dockerfile.chromium-worker` not pinned. Judgment on control design is a human item |
| 13 | 70-07: IN-01 preflight ctx from the signal ctx in all workers; IN-02 whitespace job dir refused with TMPDIR named; IN-03 script hygiene | VERIFIED | IN-01: `grep 'preflightCtx, cancelPreflight :=' cmd/` = 8, no `context.Background()` preflight left; tectonic helpers take ctx and its one real call passes main's ctx. Measured exit 0.31-0.33 s after SIGTERM (was 3.0-3.75 s). IN-02: `requireNoWhitespaceJobDir` used by `libreoffice.go:117` (PDF/A only) and `documentsandbox.go:330` (every preflight); `TestConvertPDFARefusesWhitespaceJobDir` and `TestDocumentPreflightRefusesWhitespaceTMPDIR` PASS; live refusal in 0.6 s (was a 60 s soffice timeout). IN-03: `rest` in the `local` line (`verapdf-differential.sh:174`), EXIT-trap removal logs (`:56`). The differential script itself was not re-run end to end (recorded by the executor) |
| 14 | 70-08 (WR-02): veraPDF's `ro:/proc` narrowed or the exposure explicitly measured, with comments that do not overstate the scrub | VERIFIED | Outcome NARROWED: `verapdfReadOnly` = `/proc/cgroups`, `/proc/self/cgroup`, `/proc/self/mountinfo` (code read, `documentsandbox.go:80-110`); removal runs show these three fail V0 and V0L, the other six do not; N3 with canary proven readable unconfined; `LAUNCHER: exec` measured so `/proc/self` covers the JVM; driver `ALL PASS (14 checks)` with V0, V0L, N3; conf05 MATCH `verapdf readonly (11)`; `engineEnv`/`sofficeEnv` comments now say the scrub closes the engine's OWN `/proc/self/environ` and cite N3 (`sandboxwrap.go`). soffice and tectonic grant no `/proc`; html deliberately untouched (prohibition), position recorded with a todo |
| 15 | 70-09: real document jobs pass the confined worker on a rebuilt stack; gate, eight MATCH lines and inversion recorded; WINDOWS entry closed only after the final line; todos recorded | VERIFIED | e2e/gate/inversion evidence read; `.planning/todos/pending/` holds the five todo files the plan names (verifier `ls`); `deferred-items.md` marks the html item resolved with paths. `git diff 033978c HEAD -- internal cmd scripts deploy` empty per summary and consistent with the verifier's own diff against 5db6c74 |

**Score:** 15/15 truths verified (0 present, behavior-unverified). Truths 1-11 are the original must-haves; 12-15 are the gap plans' own.

### Deferred Items

None. ROADMAP.md has no phase after 70 (only backlog 999.1). The todos are user-scheduled follow-ups, not roadmap phases, and neither is needed for any truth above.

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/convert/sandboxwrap.go` | guard refusing != 1; honest engineEnv comment | VERIFIED | code read |
| `internal/convert/devshm_observer_{linux,other}_test.go` | inotify observer, non-linux stand-in fails | VERIFIED | read; wired from `chromium_live_test.go` |
| `internal/convert/chromium_live_test.go` | rebuilt step e | VERIFIED | read |
| `internal/convert/documentsandbox.go` | narrowed veraPDF set, whitespace check | VERIFIED | read; pinned test passes |
| `scripts/document-sandbox-measure.sh` | V0L and N3 | VERIFIED | transcript `ALL PASS (14 checks)` |
| `cmd/*/main.go` (8 workers + tectonic) | preflight from signal ctx | VERIFIED | grep count 8 |
| evidence files named in the task | present | VERIFIED | all seven present and read (70-06 x2, 70-07, 70-08, 70-09 x3) |
| five pending todos | present | VERIFIED | `ls` |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `cmd/document-worker/main.go` | `ConfinedDocument` and asynq | one `concurrency` value | WIRED | lines 78-80 and 177; guard refuses < 1 and > 1 |
| `cmd/chromium-worker/main.go` | `ConfinedChromium` and asynq | one `concurrency` value | WIRED | lines 65-67 and 150 |
| `ConfinedDocument` / `DocumentPreflight` | `requireNoWhitespaceJobDir` | before any engine | WIRED | `documentsandbox.go:330` |
| `verapdfReadOnly` | SANDBOX-PATHS.md conf05 block | cross-check and pinned test | WIRED | MATCH, inversion fails |
| `TestChromiumLargeDocumentNeedsNoDevShmLive` | `sandbox-liveproof.sh html` | gate echo of `chromium:` and `dev-shm-observe:` | WIRED | PASS output in the 70-09 transcript |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Build | `GOTOOLCHAIN=go1.26.4 go build ./...` | ok | PASS |
| Tests | `GOTOOLCHAIN=go1.26.4 go test -count=1 ./internal/convert/ ./internal/sandbox/ ./internal/worker/ ./cmd/...` | convert 63.1s, sandbox, worker, cmd/* all ok | PASS |
| Vet / fmt | `go vet ./...`, `gofmt -l .` | clean | PASS |
| Guard at 0 and -1, whitespace refusals, pinned sets | `go test -count=1 -v -run 'TestSharedTmpGuard\|TestConfined...\|TestDocumentPreflightRefusesWhitespaceTMPDIR\|TestConvertPDFARefusesWhitespaceJobDir\|...SetIsTheMeasuredOne'` | all PASS incl. rows 0 and -1 | PASS |
| D-13 | `git diff --exit-code 7744ddf -- internal/convert/exec.go` | exit 0 | PASS |
| CONF-05 eight classes | `scripts/conf05-crosscheck.sh pandoc ocr av audio html probe document` | 8 MATCH (soffice 9/1/1/1, verapdf 11/1/1), exit 0 | PASS |
| Gate transcript integrity | `grep -c '^PASS: '`, last line, FAIL/NOT RUN grep | 40, `ALL PASS (40 checks)`, none | PASS |
| Code equals what the gate built | `git diff 5db6c74 HEAD -- . ':!.planning'` | empty | PASS |
| Landlock-effective tests, image builds, e2e, differential | not runnable on darwin / instructed not to rebuild or start compose | committed evidence used | SKIP |

### Probe Execution

| Probe | Command | Result | Status |
|-------|---------|--------|--------|
| `scripts/sandbox-liveproof.sh ...` | not re-run (docker; instructed) | committed `ALL PASS (40 checks)` | SKIP, evidence used |
| `scripts/document-sandbox-measure.sh` | not re-run | committed `ALL PASS (14 checks)` (70-08) | SKIP, evidence used |
| `scripts/verapdf-differential.sh` | not re-run | committed `DIVERGENCES: 0` (70-01); script edited in 70-07 (IN-03) and not re-run end to end | SKIP, noted |

### Requirements Coverage

| Requirement | Source Plans | Description | Status | Evidence |
|-------------|--------------|-------------|--------|----------|
| CONF-01 | 70-01..70-09 | engines confined, cannot read outside job dir and granted paths | SATISFIED for document and html. Advisory (not a gap): html keeps `ro:/proc`; veraPDF narrowed | measure N1/N2/N3, e2e, gate |
| CONF-02 | 70-03..70-05, 70-07, 70-09 | worker refuses to start when confinement needed and unavailable, naming the variable | SATISFIED incl. emulated amd64 and the concurrency refusal | gate transcript |
| CONF-04 | 70-03, 70-05, 70-07 | preflight on the real engine judged by output | SATISFIED | preflight code, live lines, SIGTERM and whitespace behaviour |
| CONF-05 | 70-02, 70-03, 70-05, 70-08, 70-09 | granted set equals measured set, held by a test | SATISFIED | tests, 8 MATCH, inversion |
| CONF-07 | 70-01, 70-03..70-05, 70-09 | document confined, both engines; amd64 question resolved by measurement | SATISFIED | gate green; wording check |

No orphaned Phase 70 requirements; CONF-03 and CONF-06 map to other phases. REQUIREMENTS.md checkboxes for CONF-02/04/05/07 and the ROADMAP Phase 70 checkbox are still open (bookkeeping, human item 8).

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| - | - | Debt-marker scan (`TBD\|FIXME\|XXX`) over the phase's Go files and scripts | none found | verifier ran it on the files modified by 70-06..70-09 |
| `cmd/tectonic-worker/main.go` | 346 | `preflightSandbox` has no caller | Info | dead code predating this phase; 70-07 gave it the ctx parameter and left it |
| `internal/convert/chromium_live_test.go` | 405 | control variant is dash, not Chromium | Info | recorded design decision, human item 4 |
| `70-07` | - | one `go test ./internal/convert/` failure observed once and not captured | Info | not reproduced: passes in 70-07's later runs and in the verifier's run; cause unnamed, likely load from parallel image builds |
| error text in SIGTERM-mid-preflight | - | prefix `the engines cannot convert under the sandbox` reads like a sandbox fault when the cause is cancellation | Info | accurate in its tail; one-line follow-up noted by 70-07 |

### Judgment-tier prohibitions (non-authoritative verifier verdict, human review recommended)

| Prohibition | Verdict | Evidence |
|-------------|---------|----------|
| 70-06: no Chromium pin, no shm docker flag in the gate, no widening of `htmlReadWrite`/`htmlsandbox.go`/`chromium.go` | held | `chromium-headless-shell=` absent; no `--shm-size`/`--ipc` in the script; conf05 html MATCH; files unchanged since b5e0f3f per 70-06 |
| 70-07: concurrency refused, never clamped | held | code read |
| 70-07: no edit to `internal/convert/exec.go` | held | git diff exit 0 |
| 70-08: no widening of any grant; html/tectonic not narrowed; narrowing judged by V0/V0L, not V1 alone | held | conf05 MATCH; removal tables in the evidence file |
| 70-08: a denial probe only with a control proving the target readable unconfined | held | `CANARY-PRESENT /proc/1/environ`, `/proc/6/environ` before the `DENIED` rows |
| 70-09: no stale-image proof; window closed only after the final line; no compose/chart change for MinIO | held | commit and image ids in the transcript; `git diff --quiet b5e0f3f -- docker-compose.yml deploy/` per 70-09 |
| Earlier prohibitions (no compose unpin before `DIVERGENCES: 0`, no amd64 provisioning, no `DOCUMENT_SANDBOX_DISABLED=true` checked in) | held | unchanged since the initial report; greps above |

Flagged `unverified-prohibition - human review recommended` for the soffice `/tmp` residual and the html `/proc` residual (human items 5 and 6).

### Human Verification Required

See `human_verification` in the frontmatter (eight items): the three plan human-checks you named (70-02 measured sets, 70-04 emulated refusal, 70-09 final evidence set), plus the html control redesign, the rw:/tmp residual (todo), the html `/proc` residual with the corrected WR-02 finding (todo), the first amd64 deployment todo, and the status bookkeeping after approval. Approving item 3 supersedes the 70-05 approval item of the previous report.

### Gaps Summary

None. Gap 1 (html control, red gate) and gap 2 (WR-01 bypass) are closed against the code: the control cannot be defeated by a fallback and shows failure under two inversions, the gate's last line is `ALL PASS (40 checks)` on images built from the commit whose non-planning tree is the current one, and the guard refuses any concurrency other than exactly 1 when confined with the shared /tmp, proven by unit tests and on both built images. WR-02 was measured rather than assumed: the review's environ claim did not hold, the real exposure (other processes' cmdline and status) is removed for veraPDF and recorded for html. The phase is waiting on your approval of the evidence, not on more code.

---

_Verified: 2026-10-01_
_Verifier: Claude (gsd-verifier)_
