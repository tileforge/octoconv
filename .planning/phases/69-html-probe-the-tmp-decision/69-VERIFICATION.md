---
phase: 69-html-probe-the-tmp-decision
verified: 2026-09-29T13:12:48Z
status: passed
score: 15/15 must-haves verified
covered_files:
  - .env.example
  - .planning/REQUIREMENTS.md
  - .planning/debug/chromium-dev-shm-mount.md
  - .planning/debug/csp-bypass-before-head.md
  - .planning/phases/69-html-probe-the-tmp-decision/69-01-PLAN.md
  - .planning/phases/69-html-probe-the-tmp-decision/69-01-SUMMARY.md
  - .planning/phases/69-html-probe-the-tmp-decision/69-02-PLAN.md
  - .planning/phases/69-html-probe-the-tmp-decision/69-02-SUMMARY.md
  - .planning/phases/69-html-probe-the-tmp-decision/69-03-PLAN.md
  - .planning/phases/69-html-probe-the-tmp-decision/69-03-SUMMARY.md
  - .planning/phases/69-html-probe-the-tmp-decision/69-04-PLAN.md
  - .planning/phases/69-html-probe-the-tmp-decision/69-04-SUMMARY.md
  - .planning/phases/69-html-probe-the-tmp-decision/69-05-PLAN.md
  - .planning/phases/69-html-probe-the-tmp-decision/69-05-SUMMARY.md
  - .planning/phases/69-html-probe-the-tmp-decision/69-CONTEXT.md
  - .planning/phases/69-html-probe-the-tmp-decision/69-RESEARCH.md
  - .planning/phases/69-html-probe-the-tmp-decision/69-REVIEW-DISPOSITION.md
  - .planning/phases/69-html-probe-the-tmp-decision/69-REVIEW.md
  - .planning/phases/69-html-probe-the-tmp-decision/69-SECURITY.md
  - .planning/phases/69-html-probe-the-tmp-decision/69-UAT.md
  - .planning/research/SANDBOX-PATHS.md
  - cmd/chromium-worker/main.go
  - cmd/probe-worker/main.go
  - deploy/chart/octoconv/templates/configmap.yaml
  - deploy/chart/octoconv/templates/deployment-chromium-worker.yaml
  - deploy/chart/octoconv/templates/deployment-probe-worker.yaml
  - deploy/chart/octoconv/values.yaml
  - docker-compose.yml
  - internal/convert/chromium.go
  - internal/convert/chromium_live_test.go
  - internal/convert/chromium_test.go
  - internal/convert/htmlsandbox.go
  - internal/convert/htmlsandbox_test.go
  - internal/convert/htmlsniff.go
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
covered_digest: "v2:sha256:78d5d5a68d70bf72c9a19af9cbc6eccf56bde271ff8f2ba023a3bfe5a8fbd8cb"
behavior_unverified: 0
overrides_applied: 0
re_verification:
  previous_status: human_needed
  previous_score: "14/14 (plus 1 human-verification item)"
  gaps_closed:
    - "Human item #1 (fourth class-closure inversion, chromium.go) resolved. 69-UAT.md test 1 records it as `pass` (evidence dated 2026-09-27, first committed in 5a6ac7b). This verifier did not rely on that record alone: it reproduced the inversion itself today on a throwaway `git archive HEAD` copy in the session scratchpad. The repo's working tree was never modified. On that copy, `chromium.go:240`'s `c.exec(workDir).run(ctx, \"chromium-headless-shell\", ...)` was replaced with `runCommand(...)`, and then with `runCommandAs(ctx, \"html\", ...)`. Both mutants made `TestConfinedClassesExecOnlyThroughTheSandbox` FAIL, naming `chromium.go:240 ... calls runCommand|runCommandAs directly` plus the non-vacuity error `found zero engineExec calls in chromium.go`. The unmutated copy and the reverted copy both pass. After the run, `git status --porcelain internal/` in the repo is empty."
  gaps_remaining: []
  regressions: []
gaps: []
advisory:
  - finding: "`.planning/WINDOWS.md` row 1 (phase 69, kind unrun-verify, `internal/convert/chromium.go`) still reads `status: open` (`open_count: 1`, last_updated 2026-09-26T20:46:31Z), even though the inversion it tracks has since been performed (69-UAT.md test 1) and reproduced independently by this verification. 69-SECURITY.md T-69-19 also records it as closed. The ledger is stale, not the code."
    category: other
    reason: "Bookkeeping discrepancy only. Not edited by this verifier (out of scope per task brief). If `workflow.windows_enforce` is enabled, `/gsd-ship` will block on `open_count > 0` until this is fixed. Resolve with `gsd-tools windows fixed 1`."
    evidence_status: "direct: WINDOWS.md read today; inversion reproduced today (see re_verification.gaps_closed)"
  - finding: "WR-02 (69-REVIEW.md): scripts/sandbox-liveproof.sh's CSP_LIVE_ROWS=18 is a hand-maintained integer that must track cspShapeTable's row count. It fails loudly (not silently) on divergence, so it is a maintainability trap, not a vacuous-pass risk."
    category: other
    reason: "Still `open` in 69-REVIEW-DISPOSITION.md (not yet triaged). Affects no must-have truth. Carried forward unchanged."
    evidence_status: "described with file:line evidence in 69-REVIEW.md; no fix required by this phase's success criteria"
  - finding: "IN-02 (69-REVIEW.md): chromium_live_test.go's liveEngineRun duplicates exec.go's process-group hardening without a drift guard."
    category: other
    reason: "Still `open` in 69-REVIEW-DISPOSITION.md. Informational; the reviewer deferred it to whenever exec.go's hardening next changes. Carried forward unchanged."
    evidence_status: "described with file:line evidence in 69-REVIEW.md; no action required for this phase"
---

# Phase 69: HTML, Probe & the /tmp Decision Verification Report (Re-verification)

**Phase Goal:** `html` и `probe` под Landlock, и решение по `/tmp` принято и записано с причиной
(ROADMAP.md, Phase 69).
**Verified:** 2026-09-29T13:12:48Z (tree at commit `a9716fd`)
**Status:** passed
**Re-verification:** Yes. The previous report (2026-09-27T04:31:46Z, commit `8144474`,
`human_needed`, 14/14 plus 1 human item) went stale because covered planning files changed.

## What changed since the previous verification

`git diff --stat 8144474 HEAD` touches only two files: `69-UAT.md` (UAT completed, 3/3 pass) and
the new `69-SECURITY.md` (`threats_open: 0`). `git diff --name-only a72e1df HEAD -- . ':(exclude).planning'`
is empty. So no source, chart, compose or script file has changed since `a72e1df`, the commit on which
the previous verifier rebuilt both worker images and re-ran the live gates itself.

Because of that, this run:

- re-ran the cheap deterministic checks with the go.mod-pinned toolchain (`GOTOOLCHAIN=go1.26.4`);
- **did not** rebuild images or re-run `scripts/sandbox-liveproof.sh html|probe`. Their inputs are
  byte-identical to the tree the previous verifier proved live, and nothing turned up to doubt that
  evidence. Truths 2-8 that rest on the live gate carry that evidence forward, and this report says so
  in each row;
- independently resolved the one carried-forward human item (see Truth 15).

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | ROADMAP SC1: the `/tmp` decision is taken and recorded with its reason (CONF-06) | ✓ VERIFIED | `.planning/research/SANDBOX-PATHS.md:264` `## Решение по /tmp (CONF-06, фаза 69)`, cross-referenced at :184 and :882; `REQUIREMENTS.md:62` `CONF-06 \| 69 \| Complete` |
| 2 | ROADMAP SC2: both classes convert under their rulesets; effectiveness judged by result **content** | ✓ VERIFIED | Preflights in `htmlsandbox.go`/`probesandbox.go` judge by `/Title`/`gs`/`pdfinfo` content. Unit level re-run today: `go test -count=1 ./internal/convert/` ok (61.4s). Live gate (`html` 9/9, `probe` 4/4 ALL PASS) carried from the previous verifier's own run on freshly built images at `a72e1df`; no non-planning file changed since |
| 3 | ROADMAP SC3: chromium grant carries the wrapper, its interpreter and the real binary (D-08) | ✓ VERIFIED | `conf05-crosscheck.sh` today: `MATCH: html readonly (15) engines (1) readwrite (2)`; live `wrapper-as-measured` carried (source unchanged) |
| 4 | G-69-2 closed: the JS-disable CSP governs every script, including one before the literal `<head>`/`<html>`, behind a decoy, or in a framed raw upload | ✓ VERIFIED | `awk '/^func injectCSPFirst\(/,/^}/' chromium.go \| grep -cE 'headOpenPrefix\|htmlOpenPrefix\|bytes\.Index\('` = 0 today; the 24-row `TestInjectCSPFirstLeadsEveryShape` is inside today's green `./internal/convert/` run; the 18-row live differential is carried; UAT test 2 pass |
| 5 | G-69-3 closed: no `/dev/shm` mount or sizing in chart, compose or gate; rationale points to `SANDBOX-PATHS.md` | ✓ VERIFIED | `grep -rniIE 'shm\|dshm'` over the chart deployment, `values.yaml`, `docker-compose.yml`, `sandbox-liveproof.sh` today: only test names (`TestChromiumLargeDocumentNeedsNoDevShmLive`), a sweep-fixture name and cross-reference comments, no volume/value/flag. Helm tier re-run today: `PASS helm`. Live `large-document-without-dev-shm` carried; UAT test 3 pass |
| 6 | CONF-06 concurrency guard: shared-`/tmp` class refuses concurrency > 1 | ✓ VERIFIED | `sandboxwrap.go` `sharedTmpGuard` unchanged; unit tests in today's green run; live `concurrency-guard` carried |
| 7 | CONF-06 sweep of the shared `/tmp` before every html task | ✓ VERIFIED | `htmlsandbox.go` `sweepSharedTmp` unchanged; live `sweeps-shared-tmp` carried |
| 8 | CONF-02: both workers refuse to start without Landlock, naming the escape hatch, off by default | ✓ VERIFIED | `cmd/chromium-worker/main.go`, `cmd/probe-worker/main.go` unchanged since `a72e1df`; live refusal text carried |
| 9 | CONF-04: preflight precedes `db.Connect`/queue registration | ✓ VERIFIED | both `main.go` files unchanged; `go build ./...` and `go vet ./...` clean today |
| 10 | Sandbox faults are retryable, never blamed on the client file | ✓ VERIFIED | `go test -count=1 ./internal/worker/` ok (11.8s) today, which includes `TestEngineSandboxUnavailableIsRetryableForEveryConfinedClass` |
| 11 | CONF-01/CONF-05: html/probe granted sets equal the measured document, held by a test | ✓ VERIFIED | `scripts/conf05-crosscheck.sh pandoc ocr av audio html probe` today: six `MATCH:` lines |
| 12 | Coverage allowlist covers exactly phase 70's pending files; every html/probe exec goes through `engineExec` | ✓ VERIFIED | `TestConfinedClassesExecOnlyThroughTheSandbox` in today's green run; isolated run on an unmutated HEAD copy: ok (9.5s) |
| 13 | Build/vet/test pass on this commit (pinned toolchain) | ✓ VERIFIED | `GOTOOLCHAIN=go1.26.4 go build ./... && go vet ./...` clean; `go test -count=1 ./internal/convert/ ./internal/worker/` ok. The full `./...` suite was not re-run: source is unchanged since the previous verifier's full green run |
| 14 | Helm chart renders with no `/dev/shm` mount/value for any overlay | ✓ VERIFIED | `GOTOOLCHAIN=go1.26.4 bash scripts/ci-local.sh helm` today: `PASS helm`, `ALL REQUESTED TIERS PASSED` (transcript `.planning/ci-local/ci-local-20260929T131159Z.log`, gitignored) |
| 15 | Class-closure inversion (fourth of four): a literal unwrapped exec restored in `chromium.go` fails the coverage scanner by name, and passes again after revert | ✓ VERIFIED | **Previously the human item, now resolved.** Reproduced by this verifier on a scratch `git archive HEAD` copy; the repo was not touched. Details under "Human item resolution" |

**Score:** 15/15 truths verified. 0 failed, 0 behavior-unverified, 0 overrides.

### Human item resolution (fourth class-closure inversion)

**Recorded evidence (69-UAT.md test 1).** `result: pass`, with the evidence line
`chromium.go:187 c.exec(workDir).run -> runCommand; ... FAIL naming chromium.go:187 (direct runCommand) + non-vacuity (zero engineExec calls in chromium.go); reverted, git diff --exit-code internal/ clean, test ok (go1.26.4)`.
Provenance checks:

- The line was first committed in `5a6ac7b` (2026-09-27 06:04). That was before the previous
  verification at `8144474`, which still routed the item to a human.
- Line 187 matches `chromium.go` as it stood then. `git show 5a6ac7b:internal/convert/chromium.go`
  has the `c.exec(workDir).run(ctx, "chromium-headless-shell", ...)` call at line 187. 69-04's
  `d7ce865`/`8ef24d0` later moved it to line 240.
- Both failure texts the record quotes ("calls runCommand directly", "found zero engineExec calls ...
  non-vacuity") are messages the scanner really emits (`sandboxcoverage_test.go:293,477`). The scanner
  has not changed since `0cf0f0d`, which predates the record.

So the record is internally consistent. A record is still not reproduction, so this verifier also
ran the inversion itself:

| Step | Mutation on scratch copy of HEAD | `go test -count=1 -run '^TestConfinedClassesExecOnlyThroughTheSandbox$' ./internal/convert/` |
|------|----------------------------------|------|
| baseline | none | `ok` (9.5s) |
| inversion A | `chromium.go:240` → `runCommand(ctx, "chromium-headless-shell", args...)` | `FAIL`: `chromium.go:240: chromium.go calls runCommand directly -- class files must route every engine invocation through engineExec.run/.stream, not a low-level runner` and `found zero engineExec calls in chromium.go -- ... (non-vacuity check)` |
| inversion B | `chromium.go:240` → `runCommandAs(ctx, "html", "chromium-headless-shell", args...)` | `FAIL`: same two messages, naming `runCommandAs` |
| revert | `chromium.go` restored from `git show HEAD:` | `ok` (9.9s) |
| repo untouched | none | `git status --porcelain internal/` in the repo: empty; scratch copy deleted |

This is the exact chromium.go-specific inversion the 69-03 plan's acceptance criteria name, run on
today's tree and not only the pre-69-04 one. The human item is **resolved**. 69-SECURITY.md T-69-19
already records it as closed on the same basis.

**Discrepancy (not edited):** `.planning/WINDOWS.md` row 1 still reads `open` for this same item
(`open_count: 1`, last_updated 2026-09-26). The ledger is stale. It is recorded in `advisory`
and does not affect this phase's status. It may block `/gsd-ship` if `workflow.windows_enforce` is on.
Fix: `gsd-tools windows fixed 1`.

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `.planning/research/SANDBOX-PATHS.md` | `/tmp` decision section with reasons; html large-document paragraph | ✓ VERIFIED | section at :264; conf05 cross-check matches six classes |
| `internal/convert/htmlsandbox.go`, `probesandbox.go`, `sandboxwrap.go` | rulesets, preflights, guard, sweep | ✓ VERIFIED | unchanged since `a72e1df`; covered by today's green `./internal/convert/` run |
| `internal/convert/chromium.go` | document-start CSP splice; frame-refusing CSP; engine exec only via `c.exec(workDir).run` | ✓ VERIFIED | no tag search in `injectCSPFirst`; single engine call at :240 is `engineExec` |
| `internal/convert/sandboxcoverage_test.go` | scanner that catches a raw exec in a class file | ✓ VERIFIED | shown load-bearing for `chromium.go` today (inversions A/B) |
| `cmd/chromium-worker/main.go`, `cmd/probe-worker/main.go` | refusal + preflight before Postgres | ✓ VERIFIED | unchanged; build/vet clean |
| chart deployment files, `values.yaml`, `docker-compose.yml` | no `/dev/shm` mount/size; escape hatches forwarded unset | ✓ VERIFIED | grep clean; helm tier PASS today |
| `scripts/sandbox-liveproof.sh`, `scripts/conf05-crosscheck.sh` | live gate and doc/code cross-check | ✓ VERIFIED | cross-check re-run today; live gate carried (unchanged inputs) |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `sandboxcoverage_test.go` | `chromium.go` | go/types AST scan of class files | ✓ WIRED | proven today by mutation: a raw exec in `chromium.go` is caught by file:line |
| `scripts/conf05-crosscheck.sh` | `SANDBOX-PATHS.md` ↔ Go pinned sets | doc blocks vs Go vars | ✓ WIRED | six `MATCH:` today |
| `cmd/*-worker/main.go` | `htmlsandbox.go` / `probesandbox.go` | guard → preflight before `db.Connect` | ✓ WIRED | unchanged since previous full read |
| `scripts/sandbox-liveproof.sh` | `chromium_live_test.go` → `ChromiumConverter.Convert` | cross-compiled test binary inside the built image | ✓ WIRED | carried from previous live run; inputs unchanged |
| chart chromium-worker header | `SANDBOX-PATHS.md` | cross-reference comment | ✓ WIRED | grep today |

### Behavioral Spot-Checks (run today, tree `a9716fd`)

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Build + vet, pinned toolchain | `GOTOOLCHAIN=go1.26.4 go build ./... && go vet ./...` | clean (`go version go1.26.4 darwin/arm64`) | ✓ PASS |
| Convert + worker tests | `GOTOOLCHAIN=go1.26.4 go test -count=1 ./internal/convert/ ./internal/worker/` | `ok` 61.4s / `ok` 11.8s | ✓ PASS |
| Six-class doc/code cross-check | `bash scripts/conf05-crosscheck.sh pandoc ocr av audio html probe` | six `MATCH:` | ✓ PASS |
| Helm tier | `bash scripts/ci-local.sh helm` | `PASS helm` | ✓ PASS |
| chromium.go inversion (scratch copy) | see table above | FAIL when mutated (A and B), ok when reverted | ✓ PASS |
| Live gates (`sandbox-liveproof.sh html` / `probe`) | not re-run | skipped: zero non-planning changes since `a72e1df`, where the previous verifier rebuilt images and got `ALL PASS (9)` / `ALL PASS (4)` itself | ? SKIPPED (explicit, evidence carried) |

### Probe Execution

The phase's runnable proofs are `scripts/sandbox-liveproof.sh` (Docker/Landlock live gate) and
`scripts/conf05-crosscheck.sh`. The cross-check was run today and passed. The live gate was not
re-run, for the reason stated above. There are no `scripts/*/tests/probe-*.sh` files in this repo.

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|------------|-------------|--------|----------|
| CONF-01 | 69-01..69-05 | engine cannot read outside job dir + grants | ✓ SATISFIED (html/probe half; `In Progress` in REQUIREMENTS.md pending phase 70) | rulesets, preflight negatives, cross-check |
| CONF-02 | 69-01, 69-02 | refuse to start, name the variable | ✓ SATISFIED (html/probe half) | both `main.go` |
| CONF-04 | 69-01..69-03 | preflight on the real engine, judged by output | ✓ SATISFIED (html/probe half) | `HTMLPreflight`/`ProbePreflight` |
| CONF-05 | 69-01..69-05 | granted set equals `SANDBOX-PATHS.md`, held by a test | ✓ SATISFIED (html/probe half) | cross-check today; coverage scanner load-bearing |
| CONF-06 | 69-01, 69-02 | `/tmp` decision for html and probe | ✓ SATISFIED; REQUIREMENTS.md `Complete` | decision section, guard, sweep |

No orphaned requirements. REQUIREMENTS.md maps CONF-01/02/04/05/06 to phase 69, and every one is
claimed by a plan.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `cmd/probe-worker/main.go` | 173 | the word "PLACEHOLDER" inside a historical comment explaining that an earlier value was *not* a measurement | ℹ️ Info | not a placeholder, since the comment documents the correction |
| `deploy/chart/octoconv/templates/deployment-probe-worker.yaml` | 22 | same, "180s is a PLACEHOLDER" quoted as the old wording | ℹ️ Info | same |

No TBD/FIXME/XXX debt markers in the phase's source, chart or script files. No stubs. Source is
unchanged since the previous full scan.

### Human Verification Required

None. The single carried item was resolved by independent reproduction (Truth 15).

### Gaps Summary

No gaps. All 15 truths are verified. The three ROADMAP success criteria (the recorded `/tmp` decision,
content-judged confinement of both classes, and the chromium wrapper/interpreter/binary triad) hold
on the current tree. The live-gate evidence is carried from the previous verifier's own rebuild at
`a72e1df`, and no file outside `.planning/` has changed since then. The one remaining open item from
the last round, the chromium.go class-closure inversion, now rests on this verifier's own
reproduction and not only on the UAT record. Three advisories are left that do not block: the stale
`WINDOWS.md` row 1 (bookkeeping, may block `/gsd-ship`), and review findings WR-02 and IN-02 (still
`open` in the disposition).

---
*Verified: 2026-09-29T13:12:48Z*
*Verifier: Claude (gsd-verifier)*
