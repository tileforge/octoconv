---
phase: 70-document-on-amd64
verified: 2026-09-30T23:30:00Z
status: gaps_found
score: 9/11 must-haves verified
covered_files:
  - .env.example
  - Dockerfile.document-worker
  - cmd/document-worker/main.go
  - deploy/chart/octoconv/templates/configmap.yaml
  - deploy/chart/octoconv/templates/deployment-document-worker.yaml
  - docker-compose.e2e.yml
  - docker-compose.yml
  - internal/convert/documentsandbox.go
  - internal/convert/libreoffice.go
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
covered_digest: "v2:sha256:06cef21ca53b811a8b29e4f9c78dae166ffd910a0e57130b1b567e31362b13d7"
behavior_unverified: 0
overrides_applied: 0
gaps:
  - truth: "The eight-class live gate (scripts/sandbox-liveproof.sh image pandoc ocr av audio html probe document) ends ALL PASS (39|40 checks) on images rebuilt from this tree (70-05 must-have 3; ROADMAP goal 'measured and gated on this host')"
    status: failed
    reason: "The committed transcript ends `FAILURES PRESENT (40 checks run)`: 39 PASS (all 7 document checks) and 1 FAIL, html `large-document-without-dev-shm` (TestChromiumLargeDocumentNeedsNoDevShmLive negative control: Chromium exits 0 without --disable-dev-shm-usage, so the control cannot see a /dev/shm dependency). Reproduced on the phase-69 tip 705f962 built the same day (evidence/liveproof-8class-70-05.txt lines ~140-205), attributed to unpinned Chromium 153 -> 154 (Dockerfile.chromium-worker installs chromium-headless-shell from Debian unpinned). Not caused by phase 70, but phase 70's acceptance gate is the eight-class gate and it is red. User decision: close via a gap-closure plan, recorded as a gap, not an override."
    artifacts:
      - path: "internal/convert/chromium_live_test.go"
        issue: "line ~405 negative control asserts Chromium fails without --disable-dev-shm-usage; Chromium 154 no longer does"
      - path: "Dockerfile.chromium-worker"
        issue: "Chromium version not pinned, so the gate is not reproducible across days"
      - path: ".planning/phases/70-document-on-amd64/deferred-items.md"
        issue: "records the failure as deferred to an html follow-up; no later roadmap phase covers it"
    missing:
      - "Measure Chromium 154 behaviour with /dev/shm denied (fallback path) and rebuild the negative control so it can falsify 'html needs no /dev/shm', or pin the Chromium version the gate is measured against"
      - "Re-run the eight-class gate to a final line of ALL PASS (40 checks) on images built from the fixing commit"
  - truth: "A confined document worker never runs more than one task at a time while soffice's grant includes /tmp read-write; a confined worker configured above 1 refuses to start (70-03 must-have + prohibition; CONF-06 shared-/tmp guarantee)"
    status: partial
    reason: "WR-01 confirmed by probe. sharedTmpGuard returns nil for concurrency <= 1 (internal/convert/sandboxwrap.go:230) and envInt passes through any parsed integer (cmd/document-worker/main.go:232), so DOCUMENT_WORKER_CONCURRENCY=0 or -1 is accepted. The same value is handed to asynq.Config.Concurrency (main.go:176), and asynq v0.26.0 server.go maps n < 1 to runtime.NumCPU(). Probe run by the verifier: ConfinedDocument(helper, n, env) refused=false for n=-1, 0, 1 and refused=true only for n=2. A confined document worker then runs NumCPU tasks concurrently over the shared /tmp that soffice may read and write, and each task's pre-run sweep deletes the others' job directories and sockets. The shipped defaults are all 1 (compose, .env.example, ConfigMap), so this is reachable only through a misconfiguration (extraEnv / .env typo), but it is exactly the class of input the guard exists for, and TestConfinedDocument covers 1, 2 and 4 but not 0 or negatives."
    artifacts:
      - path: "internal/convert/sandboxwrap.go"
        issue: "sharedTmpGuard: `concurrency <= 1` returns nil; must treat < 1 as a refusal when confined and /tmp is granted read-write"
      - path: "cmd/document-worker/main.go"
        issue: "envInt result reaches both the guard and asynq unclamped"
      - path: "internal/convert/documentsandbox_test.go"
        issue: "no 0 / negative concurrency case"
    missing:
      - "Refuse concurrency < 1 when the sandbox confines and the grant set contains the shared /tmp (fix in the shared guard; cmd/chromium-worker/main.go has the same shape and benefits), or clamp before both guard and asynq"
      - "Add 0 and -1 cases to TestConfinedDocument (and the html equivalent)"
behavior_unverified_items: []
coincidental_reliance_items: []
human_verification:
  - test: "Review the measured soffice and veraPDF sets: document section of .planning/research/SANDBOX-PATHS.md and the removal table in evidence/document-sandbox-measure.txt"
    expected: "Every kept entry has a failing removal run or the V0 JVM-ergonomics reason; the /tmp route and the socket right match what was measured; veraPDF writes nowhere but the job directory and /dev/null"
    why_human: "70-02 plan human-check: this record is what the pinned tests and cross-check hold the code to; the user approves the measured set as for every earlier class"
  - test: "Approve the emulated-amd64 refusal evidence for ROADMAP criterion 3 / CONF-02: evidence/liveproof-document-70-04.txt (the `emulated-amd64-refuses` PASS and its 'landlock is unavailable ... Set DOCUMENT_SANDBOX_DISABLED=true' line, emitted before Postgres) and the refusal texts in 70-03's transcript"
    expected: "Both refusals name DOCUMENT_SANDBOX_DISABLED and stop before Postgres; the emulated image says landlock is unavailable; the route checks (concurrency-guard, sweeps-shared-tmp) match the read-write /tmp route; count is 7 for the document class"
    why_human: "70-04 plan human-check: ROADMAP criterion 3 (including emulation) and CONF-02 are proven only by running the built images; the verifier did not rebuild images or start compose and relies on the committed transcript"
  - test: "Approve the 70-05 evidence set: evidence/liveproof-8class-70-05.txt, e2e-70-05.txt, inversions-70-05.txt and the eight MATCH lines"
    expected: "Every check passes on images built from the recorded commit 18c805d except the one recorded html failure; every inversion fails for its stated reason; nothing was proven on a cached result"
    why_human: "70-05 plan human-check: the live proof is the phase's acceptance. The gate is currently red (gap 1), so approval here means 'document is proven, html control failure is tracked by the gap-closure plan'"
  - test: "Decide the /tmp route residual: soffice holds rw:/tmp (not socket-only) because MAKE_SOCK alone did not suffice; the narrower alternative (sock: grant carrying MAKE_SOCK + REMOVE_FILE) was NOT measured or built (70-02-SUMMARY line ~124)"
    expected: "User either accepts rw:/tmp with concurrency 1 + sweep (after WR-01 is closed) or asks for the narrower grant as a separate change to the shared wire format"
    why_human: "Changes the shared sandbox wire-format semantics; 70-02 explicitly reserved it for the user"
  - test: "Decide WR-02: veraPDF's ro:/proc grant exposes /proc/<ppid>/environ and /proc/1/environ (worker DATABASE_URL, S3 keys) to a compromised JVM running as the same uid; the env scrub only protects /proc/self. Same exposure in html and tectonic"
    expected: "Either narrow the grant to the files V0 needs (/proc/meminfo, cpuinfo, the helper's own pid dir) and re-run scripts/document-sandbox-measure.sh V0/V2, or record the residual risk explicitly in SANDBOX-PATHS.md and the DEBT entry and correct the engineEnv comment that currently overstates what the scrub closes"
    why_human: "A security trade-off against a measured requirement (JVM container sizing needs /proc); not a literal violation of CONF-01's wording (the path is granted), so it is advisory here rather than a gap"
  - test: "First amd64 deployment: follow .planning/todos/pending/2026-09-29-amd64-first-deployment-preflight-check.md"
    expected: "All eight confined classes log 'sandbox proven' on a real amd64 node; any class that refuses is re-measured on that node"
    why_human: "Explicitly out of phase-70 scope (D-05); measurements and gate ran on arm64 only. The amd64 directory-level grants are reasoning, not measurement"
---

# Phase 70: Document on amd64 Verification Report

**Phase Goal (ROADMAP, rewritten per D-04 in 70-05):** `document` under Landlock: both engines, LibreOffice and veraPDF. The amd64 pin was the veraPDF build stage only; the image is native, measured and gated on this host. Criteria: (1) LibreOffice and veraPDF path sets measured with the same tool on the native image, after the veraPDF differential cleared the pin; (2) conversion passes under the ruleset and a file outside is unreachable; (3) the worker refuses to start without Landlock, including when the cause is emulation, not the kernel.
**Verified:** 2026-09-30
**Status:** gaps_found
**Re-verification:** No, initial verification

The document class is confined and proven. The phase is not closed because two must-haves fail: the eight-class acceptance gate is red on one html check (user-decided gap, not an override), and the shared-/tmp concurrency guarantee can be bypassed by a non-positive `DOCUMENT_WORKER_CONCURRENCY`.

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | SC1: soffice and veraPDF path sets measured with the same tool on the native image, after the differential cleared the pin | VERIFIED | `evidence/verapdf-differential.txt` last line `DIVERGENCES: 0`; `evidence/document-sandbox-measure.txt` ends `ALL PASS (12 checks)` with separate soffice/veraPDF sets driven through `/usr/local/bin/document-worker __sandbox-exec`; `scripts/conf05-crosscheck.sh ... document` re-run by verifier prints `MATCH: soffice readonly (9) engines (1) readwrite (1) socket (1)` and `MATCH: verapdf readonly (9) engines (1) readwrite (1)`; neither set grants `/usr/bin` as a directory (`internal/convert/documentsandbox.go`) |
| 2 | SC2: conversion passes under the ruleset and a file outside is unreachable | VERIFIED | Measure transcript: S1/S5/V1 pass by content, `N1 no output carries the fixture text although /outside/in.docx is identical`, `N2 no isCompliant verdict ... (Permission denied)`, `C1` fails without the socket/tmp grant; `evidence/e2e-70-05.txt` TestDocumentConversionE2E, TestPDFAExportE2E, TestPDFANonCompliantE2E pass through the confined worker; `DocumentPreflight` (`documentsandbox.go:263`) judges sufficiency then effectiveness by marker/verdict, never by exit status. Not re-run by the verifier (no compose/images), relies on committed transcripts |
| 3 | SC3: worker refuses to start without Landlock, including under emulation | VERIFIED | `evidence/liveproof-document-70-04.txt` and the 8-class transcript: `PASS: document refuses-without-landlock`, `PASS: document emulated-amd64-refuses` with `document sandbox: landlock is unavailable ... Set DOCUMENT_SANDBOX_DISABLED=true`, emitted before Postgres; no emulation-detection code (`main.go` uses only `sandbox.ResolveHelper`); startup order helper mode, ResolveHelper, ConfinedDocument, SetVeraPDFTimeout, DocumentPreflight, db.Connect confirmed in `cmd/document-worker/main.go` |
| 4 | Image is native; the pin covers only the veraPDF build stage; compose carries no document-worker platform key | VERIFIED | `Dockerfile.document-worker:27` `FROM --platform=linux/amd64 verapdf/cli:v1.30.2 AS verapdf`, runtime stage `FROM debian:bookworm-slim` with no platform; the only `platform:` left in `docker-compose.yml` is `asynqmon` (unrelated); unpin happened only after `DIVERGENCES: 0` |
| 5 | CONF-05: granted sets equal the measured ones and are held by a test | VERIFIED | Eight line-exact MATCH lines re-run by verifier; `TestSofficeReadOnlySetIsTheMeasuredOne` etc. pass; `evidence/inversions-70-05.txt` shows `/tmp/x` added, trailing-slash and neighbour-order inversions each fail |
| 6 | CONF-04: preflight on the real engines, judged by content | VERIFIED | `DocumentPreflight` code read (marker in `content.xml`, veraPDF `isCompliant` via independent parser, `_ = c.Convert(outside)` not judged on error, `requireOutsideGrants`); live: `🔒 document sandbox proven:` and `preflight-catches-restrict-self` PASS (engine exit 78 surfaces) |
| 7 | CONF-02 reachability: escape hatch named in the refusal, forwarded, never defaulted to true | VERIFIED | `docker-compose.yml:607` `DOCUMENT_SANDBOX_DISABLED: "${DOCUMENT_SANDBOX_DISABLED:-}"`; `.env.example:90,108` documented, commented out; chart reaches it through `documentWorker.extraEnv`; grep finds no checked-in `DOCUMENT_SANDBOX_DISABLED=true` outside the commented example |
| 8 | Shared-/tmp guarantee: a confined document worker never runs more than one task while soffice holds rw:/tmp | FAILED (partial) | Gap 2 (WR-01): guard bypassed for concurrency <= 0; verifier probe `concurrency=-1 refused=false`, `0 refused=false`, `1 refused=false`, `2 refused=true`; asynq maps n<1 to NumCPU |
| 9 | Eight-class live gate ends ALL PASS on images rebuilt from this tree | FAILED | Gap 1: final line `FAILURES PRESENT (40 checks run)`; 39 PASS incl. all 7 document checks; 1 FAIL html `large-document-without-dev-shm`, reproduced on 705f962 |
| 10 | CONF-07 / ROADMAP wording says what was done and keeps the emulation reason visible | VERIFIED | REQUIREMENTS.md CONF-07 and ROADMAP Phase 70 goal read: stage-only pin, native image, `DIVERGENCES: 0` cited with path, arm64 measurement host, emulated image still refuses, single amd64 todo (D-05) exists |
| 11 | `internal/convert/exec.go` untouched (D-13); sandbox-unavailable is retryable for document; sandboxPendingFiles empty | VERIFIED | `git diff --exit-code 7744ddf -- internal/convert/exec.go` exit 0; `sandboxPendingFiles = map[string]string{}` (`sandboxcoverage_test.go:68`); inversion 3 shows the retryable test fails when the sentinel check is removed |

**Score:** 9/11 truths verified (0 present, behavior-unverified)

### Deferred Items

None. ROADMAP.md has no phase after 70 (only backlog 999.1), so neither gap is covered by a later phase. `deferred-items.md` items are tracked there but are not roadmap-scheduled; the html control failure is carried as gap 1 per the user's decision.

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `Dockerfile.document-worker` | verapdf stage pinned alone | VERIFIED | line 27 pin, runtime stage native |
| `scripts/verapdf-differential.sh` | repeatable differential, `DIVERGENCES:` | VERIFIED | 202 lines; evidence ends `DIVERGENCES: 0` |
| `evidence/soffice-ipc-spike.txt` | bind-path spike with CLASSIFICATION | VERIFIED | present (18 KB); route `scoped-socket` recorded in 70-01-SUMMARY |
| `internal/sandbox/{sandbox,sandbox_linux,execmode}.go` | socket-only `sock:` grant | VERIFIED | `sandbox` tests pass; inversion 5 (run by executor in linux container) shows MAKE_SOCK in accessReadWrite is caught |
| `internal/convert/documentsandbox.go` | sets, env, guard, preflight | VERIFIED (guard has gap 2) | `ConfinedDocument`, `DocumentPreflight` present and wired from `main.go` |
| `internal/convert/libreoffice.go`, `verapdf.go` | engines through `engineExec` | VERIFIED | inversions 1 and 2 show direct `runCommand` is caught |
| `cmd/document-worker/main.go` | helper mode first; refusal, guard, preflight before Postgres | VERIFIED | order read; concurrency value shared with asynq |
| `scripts/document-sandbox-measure.sh` | content-judged driver | VERIFIED | transcript `ALL PASS (12 checks)` |
| `scripts/sandbox-liveproof.sh` | document row, emulated check, NOT RUN accounting | VERIFIED | 7 document PASS lines in the 8-class transcript |
| `.planning/research/SANDBOX-PATHS.md` | document section and conf05 blocks | VERIFIED | cross-check MATCH |
| `internal/e2e/testdata/verapdf-shim/soffice` | shim working inside soffice's ruleset | VERIFIED | e2e PASS on stack from 6a8e69e |
| `.planning/todos/pending/2026-09-29-amd64-first-deployment-preflight-check.md` | single D-05 follow-up | VERIFIED | present; folded todo moved to completed |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `verapdf-differential.sh` | `internal/convert/opts.go` | `pdfaFilterOptionsSuffix` read, not retyped | WIRED | per 70-01 must-have; differential MATCH rows include real soffice PDF/A exports |
| `docker-compose.yml` | differential | unpin only on `DIVERGENCES: 0` | WIRED | order of commits 6077229 then compose edit; no document-worker platform key |
| `cmd/document-worker/main.go` | `ConfinedDocument` | same concurrency value asynq uses | WIRED (bypass for <1, gap 2) | main.go:79 and :176 |
| `libreoffice.go` | `verapdf.go` | job's veraPDF `engineExec` threaded to `ValidatePDFA` | WIRED | build and tests pass |
| `worker.go` | `sandboxwrap.go` | sentinel checked first in `isDocumentTerminal` | WIRED | retryable test passes; inversion 3 fails |
| `document-sandbox-measure.sh` | SANDBOX-PATHS.md | rulesets read from conf05 blocks | WIRED | `--doc` calls |
| `scripts/conf05-crosscheck.sh` | SANDBOX-PATHS.md | eight classes vs Go vars | WIRED | 8 MATCH re-run |

### Data-Flow Trace (Level 4)

Not applicable: no dynamic-data rendering artifacts. The data path that matters (job input through sandboxed soffice/veraPDF to output) is covered by the e2e and measure transcripts above.

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Build | `GOTOOLCHAIN=go1.26.4 go build ./...` | ok | PASS |
| Tests | `GOTOOLCHAIN=go1.26.4 go test -count=1 ./internal/convert/ ./internal/sandbox/ ./internal/worker/ ./cmd/...` | all packages ok (convert 68s, sandbox, worker, cmd/* ok or no test files) | PASS |
| D-13 exec.go untouched | `git diff --exit-code 7744ddf -- internal/convert/exec.go` | exit 0 | PASS |
| CONF-05 eight classes | `scripts/conf05-crosscheck.sh pandoc ocr av audio html probe document` | 8 MATCH lines | PASS |
| Guard at non-positive concurrency | temp probe test on `ConfinedDocument(helper, n, ...)` (removed afterwards; tree clean) | refused only for n=2 | FAIL (gap 2) |
| Landlock-effective tests (`access_linux_test.go`) | not runnable on darwin; relies on the executor's linux-container run and the live gate | n/a | SKIP (covered by committed live evidence) |

### Probe Execution

| Probe | Command | Result | Status |
|-------|---------|--------|--------|
| `scripts/verapdf-differential.sh` | not re-run (needs docker builds of native and emulated images; instructed not to rebuild) | committed evidence ends `DIVERGENCES: 0` | SKIP, committed evidence used |
| `scripts/document-sandbox-measure.sh` | not re-run (docker) | committed evidence ends `ALL PASS (12 checks)` | SKIP, committed evidence used |
| `scripts/sandbox-liveproof.sh ...` | not re-run (docker) | committed evidence `FAILURES PRESENT (40 checks run)` | SKIP, see gap 1 |

### Requirements Coverage

| Requirement | Source Plans | Description | Status | Evidence |
|-------------|--------------|-------------|--------|----------|
| CONF-01 | 70-01, 70-02, 70-03, 70-04, 70-05 | each engine class under Landlock, cannot read outside job dir and granted paths | SATISFIED for document (see WR-02 advisory: `/proc` is a granted path but reaches sibling-process environ) | measure N1/N2, e2e, preflight |
| CONF-02 | 70-03, 70-04, 70-05 | worker refuses to start when confinement needed and unavailable, naming the variable | SATISFIED for document, including emulated amd64 | liveproof transcripts. REQUIREMENTS.md still shows `[ ]` / In Progress |
| CONF-04 | 70-03, 70-05 | preflight on the real engine judged by output | SATISFIED | `DocumentPreflight`, live proof lines. REQUIREMENTS.md still `[ ]` |
| CONF-05 | 70-02, 70-03, 70-05 | granted set equals measured set, held by a test | SATISFIED | tests + 8 MATCH. REQUIREMENTS.md still `[ ]` |
| CONF-07 | 70-01, 70-03, 70-04, 70-05 | document confined, both engines; amd64 question resolved by measurement | SATISFIED for the document class and the unpin; phase acceptance gate red on html (gap 1). REQUIREMENTS.md still `[ ]` / Pending |

All five IDs declared in PLAN frontmatter appear in REQUIREMENTS.md; no orphaned Phase 70 requirements. CONF-03 and CONF-06 map to other phases. The REQUIREMENTS.md checkboxes and Traceability status for CONF-02/04/05/07 are still open; update them only after the gaps below close.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `internal/convert/sandboxwrap.go` | 230 | `concurrency <= 1` accepted as safe | BLOCKER (gap 2) | bypasses the shared-/tmp guard at 0 / negative |
| `internal/convert/documentsandbox.go` | 92 | `ro:/proc` grants `/proc/<pid>/environ` of same-uid siblings | WARNING (WR-02) | veraPDF JVM could read the worker's DB and S3 secrets if compromised |
| `cmd/document-worker/main.go` | 109 | preflight context derived from `context.Background()` not the signal ctx (IN-01) | Info | SIGTERM during the ~6 s preflight is delayed |
| `internal/convert/documentsandbox.go` | 136 | `JAVA_OPTS` tmp path not quoted (IN-02) | Info | whitespace in TMPDIR would fail closed, opaquely |
| `scripts/verapdf-differential.sh` | 170, 174 | `rest` not local; cleanup of container-owned files (IN-03) | Info | minor hygiene |

Debt-marker scan (`TBD|FIXME|XXX`) on the phase's Go files and scripts was not separately re-run beyond the review; 70-REVIEW.md reports none and no marker was surfaced during reading.

### Judgment-tier prohibitions (non-authoritative verifier verdict, human review recommended)

| Prohibition | Verdict | Evidence |
|-------------|---------|----------|
| 70-01: no compose unpin without `DIVERGENCES: 0` | held | commit order; last line 0 |
| 70-01: no choosing/provisioning an amd64 machine | held | none in tree; D-05 todo only |
| 70-01/02: no widening of `accessReadWrite`/`accessReadOnly` | held | inversion 5; `sandbox` tests pass |
| 70-02: no path set recorded on exit status or file existence | held | driver judges by content (transcript) |
| 70-02: no `/usr/bin` directory grant; no soffice `/tmp` rw without a removal run | held; C1 is the removal run | sets in `documentsandbox.go`, C1 PASS |
| 70-03: no worker secrets in engine env | held | `engineEnv` allowlist; but see WR-02 for `/proc` read path |
| 70-03: no exit-status evidence in the preflight | held | code read |
| 70-03: veraPDF writable set = job dir + /dev/null only | held | `verapdfReadWrite = {"/dev/null"}` |
| 70-03: no more than one task with rw:/tmp | VIOLATED for concurrency < 1 | gap 2 |
| 70-03/05: no edit to `internal/convert/exec.go` | held | git diff exit 0 |
| 70-04: no checked-in `DOCUMENT_SANDBOX_DISABLED=true`; no NOT RUN counted as pass | held | greps; emulated check PASS ran on arm64 |
| 70-05: no cached/stale-image proof; no widened grant to pass a scan | held as recorded; gate evidence is at commit 18c805d and later commits touch only docs (`git diff 18c805d HEAD` outside `.planning` is empty) | transcripts |
| 70-05: no disabling of the document sandbox in the e2e overlay | held | `docker-compose.e2e.yml` comment, no variable set |

These are judgment-tier; the verdicts are an LLM reading, not a proof. Flagged `unverified-prohibition - human review recommended` for the soffice `/tmp` and `/proc` items.

### Human Verification Required

See `human_verification` in the frontmatter (six items): approve 70-02 measured sets, 70-04 emulated-refusal evidence, 70-05 evidence set; decide the rw:/tmp vs REMOVE_FILE alternative; decide WR-02; and the first amd64 deployment todo. They do not change the status because gaps_found takes precedence.

### Gaps Summary

1. **Eight-class gate is red (html control).** Everything document-specific passed (7/7 checks, e2e, inversions, cross-check), but the phase's acceptance transcript ends `FAILURES PRESENT`. Root cause is outside phase 70's code (unpinned Chromium drift, reproduced on 705f962), and the user has chosen a gap-closure plan: measure Chromium 154 with /dev/shm denied and rebuild the control (or pin Chromium), then re-run the gate to `ALL PASS (40 checks)`.
2. **Shared-/tmp guard bypass (WR-01).** The guarantee that a confined document worker runs one task at a time holds for the shipped defaults and for any value >= 1, but `DOCUMENT_WORKER_CONCURRENCY=0` or a negative value yields asynq's NumCPU concurrency with no refusal. Small fix in `sharedTmpGuard` (also fixes chromium-worker) plus two test cases. It can be closed in the same gap-closure plan as gap 1 since both touch the shared convert/html area.

Advisory, not a gap: WR-02 (`ro:/proc` exposing `/proc/<ppid>/environ`). The path is a granted, measured one and CONF-01's literal wording is met, so it is a decision item for the user; the `engineEnv` comment should not claim the env scrub closes the secret-leak path while `/proc` is granted.

Bookkeeping left for after the gaps close: REQUIREMENTS.md checkboxes and Traceability for CONF-02/04/05/07, and ROADMAP's Phase 70 checkbox.

---

_Verified: 2026-09-30_
_Verifier: Claude (gsd-verifier)_
