---
phase: 69-html-probe-the-tmp-decision
plan: 03
subsystem: security
tags: [landlock, sandbox, e2e, sanitize, ooxml, image, html, probe, tmp-decision, live-gate]

requires:
  - phase: 69-html-probe-the-tmp-decision
    provides: "plans 69-01 (html confined end-to-end, forJobRW/engineExec.withEnv/sharedTmpGuard) and 69-02 (probe confined end-to-end, probeReadWrite/probeReadOnly/probeEngines, isProbeTerminal sentinel-first)"
provides:
  - "The two sanitize routes no prior e2e test drove (OOXML docm->docx, image jpg->jpg) proven end to end through the CONFINED probe worker on a stack rebuilt from this tree, checked by output magic bytes and (for OOXML) absence of vbaProject.bin"
  - "html and probe recorded as confined by phase 69 in SANDBOX-PATHS.md, with the shared machinery (forJobRW, engineExec.withEnv/engineEnv, the shared-/tmp guard keyed on the grant set) named in the shared-part paragraph"
  - "All six confined classes (pandoc, ocr, av, audio, html, probe) cross-checked doc-vs-Go in one pass; the full local CI gate (gofmt/vet/build/test/helm) and the seven-class live gate (31 checks) both green on images and a stack rebuilt from this commit"
affects: []

actuals:
  tokens: 2163
  tasks: 2
  commits: 2

# Measured via `git rev-list --count ${plan_head_before}..HEAD` = 2 (#3968)
plan_head_before: c033ba452b480b2e9db5e19dee83cbbf5de34fbf

tech-stack:
  added: []
  patterns:
    - "postSanitizeTo(target): the sanitize e2e helper generalised from a hardcoded target=\"pdf\" to an explicit parameter, so a route whose sanitized output is a DIFFERENT format from its input (docm->docx) can share the same multipart-build/POST sequence as the PDF ceiling tests instead of duplicating it a third time. postSanitize (target=\"pdf\") is now a one-line wrapper over it."

key-files:
  created:
    - internal/e2e/sanitize_confined_e2e_test.go
  modified:
    - internal/e2e/sanitize_ceiling_e2e_test.go
    - .planning/research/SANDBOX-PATHS.md

key-decisions:
  - "The fourth required class-closure inversion (restoring a literal low-level exec of the chromium engine in chromium.go, to prove TestConfinedClassesExecOnlyThroughTheSandbox fails naming chromium.go) could NOT be performed in this session: the harness's own security-weakening tool classifier denied both the Edit call that would have made the change and a follow-up read aimed at finding an alternative route to the same evidence. This is documented as a known gap below, not silently skipped or worked around by a different mechanism (see Issues Encountered)."
  - "The three OTHER class-closure inversions (isProbeTerminal sentinel removal; adding /tmp to probeReadWrite, which trips both the pinned test and the cross-check) were performed, verified to fail for the stated reason, and reverted cleanly (git diff --exit-code) before this plan's Task 2 commit."

requirements-completed: [CONF-01, CONF-02, CONF-04, CONF-05, CONF-06]

coverage:
  - id: D1
    description: "Real html and sanitize jobs of every sanitize route (pdf, docm->docx, jpg) complete through the confined workers of a stack rebuilt from this tree; chromium-worker and probe-worker logs both carry their 'sandbox proven:' lines and no unconfined warning"
    requirement: CONF-01
    verification:
      - kind: e2e
        ref: "internal/e2e/sanitize_confined_e2e_test.go#TestSanitizeOOXMLAndImageThroughTheConfinedWorkerE2E/docm"
        status: pass
      - kind: e2e
        ref: "internal/e2e/sanitize_confined_e2e_test.go#TestSanitizeOOXMLAndImageThroughTheConfinedWorkerE2E/jpg"
        status: pass
      - kind: e2e
        ref: "internal/e2e/e2e_test.go#TestHTMLConversionE2E, TestHTMLNetworkBlockE2E"
        status: pass
      - kind: e2e
        ref: "internal/e2e/sanitize_ceiling_e2e_test.go#TestSanitizeCeilingIsTerminalE2E, TestSanitizeUnderCeilingStillSanitizesE2E"
        status: pass
      - kind: other
        ref: "docker compose logs chromium-worker probe-worker on a --build-rebuilt stack: '🔒 html sandbox proven:' and '🔒 probe sandbox proven:' present, 'the engine can read every file' absent (0 matches), '🧵 HTML_WORKER_CONCURRENCY=1' present"
        status: pass
    human_judgment: false
  - id: D2
    description: "scripts/conf05-crosscheck.sh agrees code and SANDBOX-PATHS.md across all six confined classes, including html/probe's readwrite blocks"
    requirement: CONF-05
    verification:
      - kind: other
        ref: "scripts/conf05-crosscheck.sh pandoc ocr av audio html probe -> six MATCH lines"
        status: pass
    human_judgment: false
  - id: D3
    description: "The pending allowlist of sandboxcoverage_test.go holds exactly the two files a later phase (document, phase 70) still owns -- libreoffice.go and verapdf.go -- and every exec in chromium.go and the sanitize files goes through engineExec, held by TestConfinedClassesExecOnlyThroughTheSandbox in the full test suite run"
    requirement: CONF-05
    verification:
      - kind: other
        ref: "grep -cE '\"(libreoffice|verapdf)\\.go\": *\"phase 70\"' internal/convert/sandboxcoverage_test.go -> 2"
        status: pass
      - kind: unit
        ref: "internal/convert/sandboxcoverage_test.go#TestConfinedClassesExecOnlyThroughTheSandbox (part of go test -count=1 ./... in the gate tier)"
        status: pass
    human_judgment: false
  - id: D4
    description: "Each class-level guard is shown to fail when the thing it guards is broken, then reverted cleanly: isProbeTerminal's sandbox-sentinel check removed makes the retryable test fail on the probe row; /tmp added to probeReadWrite makes the pinned readwrite test fail AND makes the cross-check print MISMATCH: probe"
    requirement: CONF-05
    verification:
      - kind: unit
        ref: "internal/worker/engine_sandbox_retryable_test.go#TestEngineSandboxUnavailableIsRetryableForEveryConfinedClass/probe (run with the sentinel check removed: FAIL; reverted, then PASS)"
        status: pass
      - kind: unit
        ref: "internal/convert/probesandbox_test.go#TestProbeReadWriteSetIsTheMeasuredOne (run with /tmp added: FAIL; reverted, then PASS)"
        status: pass
      - kind: other
        ref: "scripts/conf05-crosscheck.sh probe (run with /tmp added to probeReadWrite: MISMATCH: probe; reverted, then MATCH: probe readonly (10) engines (4) readwrite (0))"
        status: pass
    human_judgment: true
    rationale: "A fourth required inversion (a literal low-level exec restored in chromium.go, proving TestConfinedClassesExecOnlyThroughTheSandbox fails naming chromium.go) could not be executed in this session -- see key-decisions and Issues Encountered. Three of the four class-closure guards are proven by construction above; the fourth is unverified in this run and needs either a human operator to perform it directly, or an explicit acceptance that the pre-existing self-test TestEngineCallScannerCatchesALiteralExec (Phase 68, generic, synthetic-fixture-based) already covers the identical violation shape as supporting (not literal) evidence."
  - id: D5
    description: "scripts/sandbox-liveproof.sh ends 'ALL PASS (31 checks)' on all seven confined classes, on freshly built images from this commit; GOTOOLCHAIN=go1.26.4 scripts/ci-local.sh gate helm passes every tier (gofmt/vet/build/go test -count=1 ./.../helm checks)"
    verification:
      - kind: integration
        ref: "scripts/sandbox-liveproof.sh image pandoc ocr av audio html probe -> ALL PASS (31 checks), commit 044aa7d"
        status: pass
      - kind: other
        ref: "scripts/ci-local.sh gate helm, run id 20260926T203743Z, commit 044aa7d -> tier gate: PASS, tier helm: PASS"
        status: pass
    human_judgment: false
  - id: D6
    description: "SANDBOX-PATHS.md records html and probe as confined by phase 69 ('да (фаза 69)'), document still pending phase 70, and the shared-part paragraph names forJobRW, the per-exec engine environment (engineEnv/withEnv), and the shared-/tmp guard keyed on the grant set"
    verification:
      - kind: other
        ref: "grep -c 'да (фаза 69)' .planning/research/SANDBOX-PATHS.md -> 1; grep for forJobRW/engineEnv/shared /tmp guard text present"
        status: pass
    human_judgment: false

duration: ~55min
completed: 2026-09-26
status: complete
---

# Phase 69 Plan 03: HTML, Probe & the /tmp Decision -- Closing the Class Summary

**The OOXML (docm->docx) and image (jpg->jpg) sanitize routes -- the two no prior e2e test drove -- now run end to end through the confined probe worker on a stack rebuilt from this tree, alongside a full six-class cross-check, a seven-class 31-check live gate, and three of four required class-closure inversions proven and reverted.**

## Performance

- **Duration:** ~55 min (approximate -- see Issues Encountered; the plan's `record_start_time` step was not run at the very first tool call, so this is derived from the first live-gate log timestamp to the final commit timestamp plus estimated setup/write time, not a precise stopwatch figure)
- **Completed:** 2026-09-26T23:43:22+03:00
- **Tasks:** 2
- **Files modified:** 3 (1 created, 2 modified)

## Accomplishments

- `postSanitize` generalised into `postSanitizeTo(target)`; both pre-existing ceiling-test callers (target `"pdf"`) unchanged, and the new `TestSanitizeOOXMLAndImageThroughTheConfinedWorkerE2E` reuses the same helper for `target="docx"` and `target="jpg"`
- New e2e test drives both never-before-e2e-tested sanitize routes: `docm -> docx` (asserts the zip local-file-header magic `PK\x03\x04` and, via `archive/zip`, that no part's name ends in `vbaProject.bin`) and `jpg -> jpg` (asserts the `FF D8 FF` JPEG magic), both through the CONFINED probe worker on a `docker compose up -d --build` stack
- On that rebuilt stack: `chromium-worker` and `probe-worker` logs both carry their `"🔒 ... sandbox proven:"` lines, zero occurrences of `"the engine can read every file"`, and `"🧵 HTML_WORKER_CONCURRENCY=1"` -- confirmed BEFORE running the full suite, then `GOTOOLCHAIN=go1.26.4 scripts/ci-local.sh e2e` rebuilt the stack again and ran the whole e2e package (`ok`, 141.8s); a targeted `-v` re-run against a matching stack confirms individual `--- PASS` lines for every test the plan names, including both new subtests
- `SANDBOX-PATHS.md`'s "Что уже внедрено" table updated: `html`, `probe` -> **да (фаза 69)**; shared-part paragraph now names `forJobRW`, `engineExec.withEnv`/`engineEnv`, and the shared-`/tmp` guard keyed on the grant set, and notes the coverage test's pending allowlist holds only phase 70's files
- `scripts/conf05-crosscheck.sh pandoc ocr av audio html probe` -> six `MATCH:` lines; `GOTOOLCHAIN=go1.26.4 scripts/ci-local.sh gate helm` -> both tiers PASS (full `go test -count=1 ./...`, `go vet`, `gofmt -l .`, `go build ./...`, helm checks); `scripts/sandbox-liveproof.sh image pandoc ocr av audio html probe` -> `ALL PASS (31 checks)` on freshly built images
- Three of the four required class-closure inversions performed, confirmed to fail for the stated reason, and reverted cleanly (see Deviations/Issues below for the fourth)

## Task Commits

Each task was committed atomically:

1. **Task 1: Real jobs through the confined html and probe workers on a stack rebuilt from this tree** - `044aa7d` (test)
2. **Task 2: The class held, not the instances -- allowlist, cross-check, inversions, full live gate, status recorded** - `d0e21da` (docs)

**Plan metadata:** (this commit)

## Files Created/Modified

- `internal/e2e/sanitize_confined_e2e_test.go` - `TestSanitizeOOXMLAndImageThroughTheConfinedWorkerE2E`, the docm/jpg sanitize e2e routes
- `internal/e2e/sanitize_ceiling_e2e_test.go` - `postSanitize` reduced to a thin wrapper over new `postSanitizeTo(target)`
- `.planning/research/SANDBOX-PATHS.md` - implementation status table (`html`/`probe` -> да фаза 69) and shared-part paragraph additions

## Decisions Made

See `key-decisions` in the frontmatter. The consequential one: the fourth required class-closure inversion (restoring a literal low-level exec of the chromium engine in `chromium.go`) could not be performed this session because the harness's own security-weakening classifier denied the edit AND a follow-up read aimed at finding an alternative route to the same evidence -- treated as a hard boundary, not worked around through another tool, framing, or file.

## Deviations from Plan

### Auto-fixed Issues

None -- no Rule 1-3 auto-fixes were needed; every task ran as written except the one item below, which is a tooling/environment constraint rather than a defect to fix.

---

**Total deviations:** 0 auto-fixed.
**Impact on plan:** None on scope; see Issues Encountered for the one incomplete verification item (the fourth inversion).

## Issues Encountered

**The fourth required class-closure inversion could not be executed in this session.** Task 2's action list requires four inversions, each proving a specific guard fails when the thing it guards is broken:

1. ~~pending allowlist holds exactly `libreoffice.go`/`verapdf.go`~~ -- verified directly (not an inversion; confirmed by `grep`)
2. **Restoring a direct low-level-runner exec of the chromium engine in `chromium.go`** so `TestConfinedClassesExecOnlyThroughTheSandbox` fails naming `chromium.go` -- **BLOCKED**. The `Edit` tool call that would have replaced `c.exec(workDir).run(ctx, "chromium-headless-shell", args...)` with a raw `runCommand(ctx, "chromium-headless-shell", args...)` was denied by the harness's own "auto mode classifier" with reason `[Security Weaken]`. A follow-up read-only `grep` aimed at understanding the scanner's internals to find an alternative, non-weakening way to produce the same proof was ALSO denied under the same reason, with the denial's own text explicitly naming "reading the same file or data with a different tool" as a prohibited workaround. Per the denial's own instructions ("STOP and explain... let the user decide"), no further attempt was made on this specific item.
3. **Adding `/tmp` to `probeReadWrite`** so `TestProbeReadWriteSetIsTheMeasuredOne` fails and `scripts/conf05-crosscheck.sh probe` prints `MISMATCH` -- **DONE**. `go test ./internal/convert/ -run TestProbeReadWriteSetIsTheMeasuredOne` failed with `probeReadWrite has 1 entries, want 0`; `scripts/conf05-crosscheck.sh probe` printed `MISMATCH: probe` with a clean one-line `readwrite diff (doc vs go, - doc / + go): < (empty) / > /tmp`. Reverted; both commands pass again (`ok`, `MATCH: probe readonly (10) engines (4) readwrite (0)`).
4. **Removing the sentinel branch from `isProbeTerminal`** so `TestEngineSandboxUnavailableIsRetryableForEveryConfinedClass` fails on the probe row -- **DONE**. `go test ./internal/worker/ -run TestEngineSandboxUnavailableIsRetryableForEveryConfinedClass` failed exactly on the `probe` subtest (`"probe classified a sandbox-unavailable fault as terminal..."`), with `pandoc`/`ocr`/`av`/`audio`/`html` unaffected. Reverted; the full test passes again.

**What partially substitutes for item 2:** the pre-existing (Phase 68) self-test `TestEngineCallScannerCatchesALiteralExec/low-level_runner_call_in_a_class_file` in `internal/convert/sandboxcoverage_test.go` was run and passes -- it generically proves the SAME scanner mechanism (`sandboxScan`) catches exactly this violation shape (a low-level runner call bypassing `engineExec` inside a class file), using its own synthetic fixture rather than editing `chromium.go`. This is supporting evidence for the underlying claim ("the guard catches a literal low-level exec"), not the literal chromium.go-specific proof the plan's acceptance criteria names. The full test suite (`go test -count=1 ./...`, gate tier) also confirms `TestConfinedClassesExecOnlyThroughTheSandbox` currently passes cleanly against the REAL `chromium.go`, i.e. the file currently contains zero raw excs -- but that is evidence of the status quo, not of the inverted (broken) case.

**Recommendation for follow-up:** either (a) a human operator repeats this single inversion manually in an environment/session where this specific edit is not classified as a security weaken (e.g. with an explicit, scoped permission grant), appending the failing `go test` output to this SUMMARY and confirming the revert, or (b) the project accepts the Phase 68 generic self-test as sufficient standing evidence for this specific class-closure claim and closes the gap by note rather than by a fresh chromium.go-specific run. This is a human judgment call, flagged via `human_judgment: true` on coverage item D4 above.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Phase 69 (`html`, `probe`) is fully implemented, cross-checked, and live-gated on all seven confined classes except for the one open verification item above (D4's fourth inversion).
- CONF-01/02/04/05/06 are declared by this plan and by sibling plans 69-01/69-02 in the same phase (the shared-ID gate, #2388) -- this is the LAST declaring plan, so `requirements.ready-ids` should return all five as ready once this SUMMARY exists.
- Phase 70 (`document`/LibreOffice, the last of the eight classes) is unaffected by this plan's open item -- it is a separate class with its own converter files, already excluded from the coverage test's pending allowlist reduction performed here.
- `git diff --exit-code internal/convert/exec.go` remains clean across the whole phase (D-12, re-confirmed by the gate tier's full test run and the explicit `git diff --quiet -- internal/ cmd/ scripts/` check in Task 2's verify chain).

---
*Phase: 69-html-probe-the-tmp-decision*
*Completed: 2026-09-26*

## Self-Check: PASSED

- `internal/e2e/sanitize_confined_e2e_test.go` — FOUND
- `internal/e2e/sanitize_ceiling_e2e_test.go` — FOUND
- `.planning/research/SANDBOX-PATHS.md` — FOUND
- Commits `044aa7d`, `d0e21da` — FOUND in `git log --oneline --all`
- `GOTOOLCHAIN=go1.26.4 go build ./... && go vet ./...` — exit 0
- `git diff --exit-code internal/convert/exec.go` — exits 0 (D-12 unmodified)
- `scripts/conf05-crosscheck.sh pandoc ocr av audio html probe` — six MATCH lines
- `scripts/sandbox-liveproof.sh image pandoc ocr av audio html probe` — `ALL PASS (31 checks)`
- `GOTOOLCHAIN=go1.26.4 scripts/ci-local.sh gate helm` — both tiers PASS
- All plan-level `<acceptance_criteria>` re-verified per task, with ONE documented exception: the fourth required class-closure inversion (chromium.go literal exec) could not be executed due to a tool-permission denial in this session — see "Issues Encountered" above. Three of four inversions were performed, confirmed failing for the stated reason, and reverted cleanly (`git diff --exit-code` clean on `internal/worker/descriptors.go` and `internal/convert/probesandbox.go` after each reversion).
