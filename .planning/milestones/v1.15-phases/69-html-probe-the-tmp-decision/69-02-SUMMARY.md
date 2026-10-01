---
phase: 69-html-probe-the-tmp-decision
plan: 02
subsystem: security
tags: [landlock, sandbox, probe, sanitize, engineExec, preflight, worker, tmp-decision]

requires:
  - phase: 69-html-probe-the-tmp-decision
    provides: "plan 69-01 -- forJobRW, engineExec.withEnv/engineEnv, sharedTmpGuard, requireAbsent/newPreflightWorkspaceOutside/requireOutsideGrants, the liveproof probe row and per-class extra-run-args mechanism, conf05-crosscheck.sh's readwrite kind and probe wiring"
provides:
  - "CONF-01 (probe half): every external engine the sanitize path execs (pdfinfo, gs x2, unzip x2, file x2 -- seven call sites, four binaries) runs through ONE engineExec built once per job in SanitizeConverter.Convert, granting the job directory as the ONLY writable path plus probeReadOnly and the four engines read-only"
  - "CONF-06 (probe half): /tmp dropped by measurement (D-05 -- gs honours TMPDIR), TMPDIR travels in each engine's OWN environment via engineEnv(jobDir)/withEnv, never a process-wide os.Setenv; sanitizePDF's and verifyOOXML's scratch directories relocated inside the job directory (the D-05 precondition)"
  - "CONF-04 (probe half): ProbePreflight proves all four engines in both directions by CONTENT -- the production PDF path's sanitized output still carries a txtwrite-readable marker, pdfinfo/gs/unzip fail loudly when denied, file(1) exits 0 when denied so its exit status is never the judgment"
  - "CONF-02 (probe half): cmd/probe-worker refuses to start without Landlock, naming PROBE_SANDBOX_DISABLED; ProbeConcurrencyGuard (keyed on the grant set, a no-op today); the preflight runs before Postgres/queue"
  - "CONF-05 (probe half): probeReadWrite (empty)/probeReadOnly/probeEngines pinned by tests and equal the corrected SANDBOX-PATHS.md conf05:probe blocks exactly, held by scripts/conf05-crosscheck.sh probe"
  - "sandbox-fault preservation: classifyEngineFailure and unverifiableEngineFailure (new) preserve ErrEngineSandboxUnavailable before falling through to ErrSanitizeMalformedInput/ErrSanitizeUnverifiable; isProbeTerminal checks the sentinel first, before any of its own terminal classifications"
affects: [69-03-edge-ledger]

actuals:
  tokens: 27284
  tasks: 3
  commits: 4

# Measured via `git rev-list --count ${plan_head_before}..HEAD` = 4 (#3968)
plan_head_before: 6636eda3a5ee1fe7e29b10bd45ec1a273d3e243c

tech-stack:
  added: []
  patterns:
    - "Multi-call-site class threading (mirrors OCR, not pandoc/html): x engineExec built ONCE in SanitizeConverter.Convert (before the page-ceiling guard and any format dispatch) and threaded as an explicit parameter through sanitizeFormats/verifiers' shared map value types and every function assigned into them -- sanitizeOOXML/sanitizeImage/verifyPDF carry it unused since they never exec anything, the honest cost of a uniform dispatch signature"
    - "unverifiableEngineFailure(err, format, args...): the ErrSanitizeUnverifiable wrap point analogue to classifyEngineFailure -- checks ErrEngineSandboxUnavailable first and preserves it unwrapped-through-a-labelled-%w, otherwise reproduces the pre-existing '%w: format: %v' wrap byte-for-byte"
    - "c.exec(outPath) always calls .withEnv(engineEnv(jobDir)), confined or not -- TMPDIR/PATH/HOME/LANG/LC_ALL scrubbing is unconditional, not gated on Confine, so an unconfined test converter and a confined production one build the same shape of engineExec"

key-files:
  created:
    - internal/convert/probesandbox.go
    - internal/convert/probesandbox_test.go
  modified:
    - internal/convert/sanitize.go
    - internal/convert/sanitizeconverter.go
    - internal/convert/sanitizepdf.go
    - internal/convert/sanitizeverify.go
    - internal/convert/sanitizeooxml.go
    - internal/convert/sanitizeimage.go
    - internal/convert/ocrsandbox.go
    - internal/convert/sandboxpreflight.go
    - internal/convert/sandboxcoverage_test.go
    - internal/convert/pdflex_test.go
    - internal/convert/pdflexr4_test.go
    - internal/convert/pdflexr5_test.go
    - internal/convert/probebounds_test.go
    - internal/convert/sanitizeicc_test.go
    - internal/convert/sanitizeimage_test.go
    - internal/convert/sanitizemainpart_test.go
    - internal/convert/sanitizeooxml_test.go
    - internal/convert/sanitizepdf_test.go
    - internal/convert/sanitizepr14_test.go
    - internal/convert/sanitizepr14r2_test.go
    - internal/convert/sanitizereport_test.go
    - internal/convert/sanitizeverify_config_test.go
    - internal/convert/sanitizeverify_test.go
    - cmd/probe-worker/main.go
    - internal/worker/descriptors.go
    - internal/worker/engine_sandbox_retryable_test.go
    - docker-compose.yml
    - .env.example
    - deploy/chart/octoconv/templates/deployment-probe-worker.yaml
    - .planning/research/SANDBOX-PATHS.md

key-decisions:
  - "Step 0 measurement (before any code): built octoconv-probe-worker from this tree, ran the real ps2write+pdfwrite production passes against testdata/sanitize/threat_all_classes.pdf, then gs -sDEVICE=txtwrite on the result -- printed 'Body text that must survive sanitization intact.' verbatim, confirming the content judgment ProbePreflight relies on before any implementation depended on it"
  - "TMPDIR travels in the engine's OWN environment (engineEnv(jobDir) via engineExec.withEnv), not a process-wide os.Setenv as RESEARCH.md's own illustrative code sketched -- plan 69-01's withEnv/engineEnv machinery made the narrower, race-free design available, so probe never needed the process-wide-env risk RESEARCH.md's Open Question 2 flagged"
  - "D-10 re-measurement corrected two things from the 2026-09-20 record: file(1) was entirely missing (added, with its own read-only grants), and /etc-magic/usr-share-misc were proven UNNECESSARY by removal rather than assumed needed -- /usr/lib (already granted) resolves file(1)'s compiled magic database through the symlink chain"
  - "/usr/share/poppler kept as a conservative grant despite one fixture's removal test showing no difference -- an unused directory grant costs nothing security-relevant, while removing it risks a silent mis-decode of a non-ASCII PDF title/author field that still exits 0 (same trap class as OCR's /usr/share/fonts)"
  - "probesandbox.go's DOCX/JPEG preflight fixtures are built with archive/zip and image/jpeg directly (production code, not a test helper) -- the DOCX carries the real WordprocessingML main-part content type, not the 'application/xml' shortcut some unit tests use, and needs no _rels/.rels (confirmed sufficient by the unconfined sufficiency test)"

patterns-established:
  - "Pattern: a multi-call-site class whose dispatch runs through shared map value types (sanitizeFormats/verifiers) threads engineExec as an explicit parameter through EVERY function assigned into those maps, including the ones that never exec anything -- the uniform signature is worth more than per-function minimalism, and a partial thread would not compile"
  - "Pattern: c.exec(outPath)'s job directory is ALWAYS filepath.Dir(outPath) -- both the production Convert path and a preflight's own manual x.run(...) calls resolve to the identical ruleset when they share the same output-file naming convention (out.pdf/out.docx/out.jpg all under the same w.jobDir)"

requirements-completed: []
# CONF-01/02/04/05/06 are declared by BOTH this plan and sibling 69-03 (the
# edge ledger). Per the shared-ID gate (#2388), none may be marked complete
# until every declaring plan has a SUMMARY -- verified via
# `requirements.ready-ids`, which returned 0/5 ready at this plan's own
# STATE update (69-03 has no SUMMARY.md yet).

coverage:
  - id: D1
    description: "Every external engine the sanitize path execs runs through ONE engineExec per job, granting the job directory as the only writable path (no /tmp) plus probeReadOnly + the four engines read-only"
    requirement: CONF-01
    verification:
      - kind: unit
        ref: "internal/convert/probesandbox_test.go#TestSanitizeConvertGrantsTheJobDirOnly"
        status: pass
      - kind: unit
        ref: "internal/convert/probesandbox_test.go#TestSanitizeEnginesGetAJobScopedTmpdirAndNoSecrets"
        status: pass
      - kind: integration
        ref: "scripts/sandbox-liveproof.sh probe (confined-startup: '🔒 probe sandbox proven: every probe engine ran inside the job directory and was denied a file outside it')"
        status: pass
    human_judgment: false
  - id: D2
    description: "CONF-06: /tmp dropped from probe's grant set by measurement (D-05, gs honours TMPDIR); TMPDIR travels per-exec in the engine's own environment, never process-wide; sanitizePDF's and verifyOOXML's scratch directories relocated inside the job directory (the required precondition)"
    requirement: CONF-06
    verification:
      - kind: unit
        ref: "internal/convert/probesandbox_test.go#TestProbeReadWriteSetIsTheMeasuredOne"
        status: pass
      - kind: unit
        ref: "internal/convert/probesandbox_test.go#TestSanitizeEnginesGetAJobScopedTmpdirAndNoSecrets"
        status: pass
      - kind: other
        ref: "grep gates: os.MkdirTemp(filepath.Dir(outPath), \".sanitize-pdf-\") and os.MkdirTemp(filepath.Dir(path), \".verify-\"), each exactly once; git grep for a process-wide Setenv(\"TMPDIR\" finds nothing"
        status: pass
    human_judgment: false
  - id: D3
    description: "ProbePreflight proves sufficiency and effectiveness for all four engines by CONTENT, on the production PDF two-pass path and the raw input for OOXML/image -- file(1)'s exit-0-on-denial is never the judgment"
    requirement: CONF-04
    verification:
      - kind: unit
        ref: "internal/convert/probesandbox_test.go#TestProbePreflightUnconfinedProvesNothing"
        status: pass
      - kind: unit
        ref: "internal/convert/probesandbox_test.go#TestProbePreflightSufficiencyUnconfined"
        status: pass
      - kind: integration
        ref: "scripts/sandbox-liveproof.sh probe (preflight-catches-restrict-self)"
        status: pass
    human_judgment: false
  - id: D4
    description: "cmd/probe-worker refuses to start when confinement is wanted and cannot be established (naming PROBE_SANDBOX_DISABLED), calls ProbeConcurrencyGuard, and preflights before db.Connect -- reachable from both compose and the Helm chart, default confined"
    requirement: CONF-02
    verification:
      - kind: integration
        ref: "scripts/sandbox-liveproof.sh probe (refuses-without-landlock, escape-hatch checks)"
        status: pass
      - kind: other
        ref: "docker compose config (PROBE_SANDBOX_DISABLED set/unset) and helm template --set probeWorker.extraEnv.PROBE_SANDBOX_DISABLED=true assertions"
        status: pass
    human_judgment: false
  - id: D5
    description: "probe's granted sets (probeReadWrite empty, probeReadOnly, probeEngines) are pinned by tests that fail when the set drifts, and equal SANDBOX-PATHS.md's conf05:probe blocks exactly"
    requirement: CONF-05
    verification:
      - kind: unit
        ref: "internal/convert/probesandbox_test.go#TestProbeReadOnlySetIsTheMeasuredOne"
        status: pass
      - kind: unit
        ref: "internal/convert/probesandbox_test.go#TestProbeEnginesAreTheMeasuredOnes"
        status: pass
      - kind: unit
        ref: "internal/convert/probesandbox_test.go#TestProbeGrantsNeitherEtcMagicNorUsrShareMisc"
        status: pass
      - kind: other
        ref: "scripts/conf05-crosscheck.sh probe -> MATCH: probe readonly (10) engines (4) readwrite (0)"
        status: pass
    human_judgment: false
  - id: D6
    description: "A node that cannot confine the engine is never misclassified as a client-file defect at any of the four wrap points (page count, ps2write pass, unzip listing, file classify) or by isProbeTerminal -- proven with a control table showing the same shapes classify exactly as before when the failure is plain"
    verification:
      - kind: unit
        ref: "internal/convert/probesandbox_test.go#TestProbeSandboxFaultSurvivesEveryWrapPoint"
        status: pass
      - kind: unit
        ref: "internal/convert/probesandbox_test.go#TestProbePlainEngineFailureKeepsItsClassification"
        status: pass
      - kind: unit
        ref: "internal/worker/engine_sandbox_retryable_test.go#TestEngineSandboxUnavailableIsRetryableForEveryConfinedClass/probe"
        status: pass
      - kind: unit
        ref: "internal/worker/engine_sandbox_retryable_test.go#TestEngineSandboxControlShapesAreTerminal/probe"
        status: pass
    human_judgment: false

duration: ~51min
completed: 2026-09-26
status: complete
---

# Phase 69 Plan 02: Probe Confined End-to-End Summary

**The probe engine class (gs, pdfinfo, unzip, file -- seven call sites across sanitizepdf.go/sanitizeverify.go) runs under one Landlock ruleset per job with no `/tmp` grant at all, TMPDIR delivered per-exec in each engine's own scrubbed environment, and a content-judged four-engine preflight that never trusts an exit code.**

## Performance

- **Duration:** ~51 min
- **Started:** 2026-09-26T16:13:05Z
- **Completed:** 2026-09-26T17:04:33Z
- **Tasks:** 3
- **Files modified:** 32 (2 created, 30 modified)

## Accomplishments

- All seven probe exec sites (`pdfinfo`, `gs` x2, `unzip` x2, `file` x2) now route through one `engineExec` per job, built once in `SanitizeConverter.Convert` before any format dispatch, granting the job directory as the sole writable path -- `/tmp` dropped entirely, measured (D-05) that ghostscript honours `TMPDIR` and produces zero bare-`/tmp` opens
- `sanitizePDF`'s and `verifyOOXML`'s scratch directories relocated inside the job directory (the required precondition for dropping `/tmp` -- both previously used `os.MkdirTemp("", ...)`, which would have failed every sanitize job at directory creation the moment `/tmp` left the grant set)
- `classifyEngineFailure` and the new `unverifiableEngineFailure` helper preserve `ErrEngineSandboxUnavailable` before falling through to `ErrSanitizeMalformedInput`/`ErrSanitizeUnverifiable`; `isProbeTerminal` checks the sentinel first -- a node that cannot confine the engine is retried, never blamed on the client's document
- `ProbePreflight` proves all four engines in both directions by content: the production two-pass PDF path's sanitized output still carries a `gs -sDEVICE=txtwrite`-readable marker, `pdfinfo`/`gs`/`unzip` fail loudly when denied, `file(1)` exits 0 and prints a non-MIME answer when denied -- so the preflight never trusts an exit code
- `cmd/probe-worker` refuses to start without Landlock (naming `PROBE_SANDBOX_DISABLED`), calls `ProbeConcurrencyGuard` (a no-op today, keyed on the empty `probeReadWrite` grant set), and preflights before `db.Connect`
- Live-gated on a freshly built `probe-worker` image: `scripts/sandbox-liveproof.sh probe` -> `ALL PASS (4 checks)`, including the `🔒 probe sandbox proven:` evidence line before the Postgres connect attempt
- `SANDBOX-PATHS.md`'s probe section rewritten from the code's own exec sites (D-10), correcting the incomplete 2026-09-20 record that omitted `file(1)` entirely; `/etc/magic`/`/usr/share/misc` proven unnecessary by removal, `/usr/share/poppler` kept conservatively

## Task Commits

Each task was committed atomically:

1. **Task 1: Probe confined end-to-end** - `0cf0f0d` (feat)
2. **Task 2: Pin the contract (RED)** - `1174361` (test)
2. **Task 2: isProbeTerminal never misclassifies a sandbox fault (GREEN)** - `88d3ce9` (feat)
3. **Task 3: Remedy reachable from compose/chart, doc corrected, full gate** - `0ddab94` (feat)

**Plan metadata:** (this commit)

_Note: Task 2 is a TDD task and produced two commits (test → feat); no REFACTOR commit was needed -- the implementation was already clean._

## Files Created/Modified

- `internal/convert/probesandbox.go` - probe's measured sets, `WithSandbox`, `exec`, `ProbeConcurrencyGuard`, `gsTextArgs`, DOCX/JPEG preflight fixtures, `ProbePreflight`/`probePreflightSufficiency`/`probePreflightEffectiveness`
- `internal/convert/probesandbox_test.go` - pinned sets, D-10 exclusions, job-dir-only ruleset, TMPDIR/env, sentinel-at-every-wrap-point behavior
- `internal/convert/sanitize.go` / `sanitizeconverter.go` / `sanitizepdf.go` / `sanitizeverify.go` - `x engineExec` threaded through `SanitizeFile`/`VerifySanitized`/`sanitizeFormats`/`verifiers`/`sanitizePageCeilings`; `classifyEngineFailure` and `unverifiableEngineFailure` preserve the sandbox sentinel; scratch directories relocated
- `internal/convert/sanitizeooxml.go` / `sanitizeimage.go` - unused `x engineExec` parameter (shared dispatch signature, no exec in either file)
- `internal/convert/sandboxpreflight.go` - `requireContains`/`requireLacks` for engine-stdout judgments
- `internal/convert/ocrsandbox.go` - widened `ocrPreflightPDF` doc comment (reused directly by probe's PDF fixture)
- `internal/convert/sandboxcoverage_test.go` - probe files/class reclassified from pending to confined
- 14 test files - mechanically threaded `engineExec{}` into ~65 call sites; `TestVerifierRejectsTheUnsanitizedInput` copies corpus fixtures into `t.TempDir()` (D-05 precondition: verifying a testdata/ path directly would write a scratch directory into the tracked source tree)
- `cmd/probe-worker/main.go` - `PROBE_SANDBOX_DISABLED`/`ProbeConcurrencyGuard` wiring, preflight before Postgres, single `convert.Default.Replace`
- `internal/worker/descriptors.go` / `engine_sandbox_retryable_test.go` - `isProbeTerminal` checks the sentinel first; probe rows in both retryable-class tables
- `docker-compose.yml` / `.env.example` / `deployment-probe-worker.yaml` - escape hatch wired and documented
- `.planning/research/SANDBOX-PATHS.md` - probe section rewritten from the code, conf05:probe blocks added

## Decisions Made

See `key-decisions` in the frontmatter. The most consequential: TMPDIR travels in each engine's own environment (`engineEnv(jobDir)` via plan 69-01's `withEnv`), not a process-wide `os.Setenv` -- avoiding the goroutine-level env race RESEARCH.md's Open Question 2 flagged as a real hazard for a future concurrency increase.

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- `probeReadWrite`/`probeReadOnly`/`probeEngines`, `ProbeConcurrencyGuard`, `ProbePreflight`, `unverifiableEngineFailure`, `requireContains`/`requireLacks` are all in place and pinned by tests.
- CONF-01/02/04/05/06 are declared by this plan AND sibling plan 69-03 (the edge ledger) in the same phase -- per the shared-ID gate (#2388), REQUIREMENTS.md marks them complete only once every declaring plan has a SUMMARY. `requirements.ready-ids` returned 0/5 ready at this plan's own STATE update (69-03 has no SUMMARY yet) -- correctly withheld, not an oversight.
- `scripts/sandbox-liveproof.sh` and `scripts/conf05-crosscheck.sh` need no further changes for probe; both were already wired in plan 69-01.

---
*Phase: 69-html-probe-the-tmp-decision*
*Completed: 2026-09-26*

## Self-Check: PASSED

- `internal/convert/probesandbox.go` — FOUND
- `internal/convert/probesandbox_test.go` — FOUND
- Commits `0cf0f0d`, `1174361`, `88d3ce9`, `0ddab94` — FOUND in `git log --oneline --all`
- `GOTOOLCHAIN=go1.26.4 go build ./... && go vet ./... && go test -count=1 ./...` — exit 0, all packages `ok`
- `git diff --exit-code internal/convert/exec.go` — exits 0 (D-12 unmodified)
- `scripts/sandbox-liveproof.sh probe` — `ALL PASS (4 checks)`
- `scripts/conf05-crosscheck.sh probe html` — two `MATCH` lines
- `scripts/ci-helm-checks.sh` — exit 0
- All plan-level `<acceptance_criteria>` re-verified per task, including both required inversions (temporarily removing the `classifyEngineFailure` sentinel branch, and temporarily adding `/tmp` to `probeReadWrite`), both reverted cleanly (`git diff --exit-code` clean on `internal/convert/sanitizepdf.go` and `internal/convert/probesandbox.go` after reversion)
