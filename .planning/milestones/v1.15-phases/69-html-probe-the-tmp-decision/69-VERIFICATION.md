---
phase: 69-html-probe-the-tmp-decision
verified: 2026-10-01T00:00:00Z
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
  - Dockerfile.chromium-worker
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
  - internal/convert/devshm_observer_linux_test.go
  - internal/convert/devshm_observer_other_test.go
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
  - internal/sandbox/execmode.go
  - internal/sandbox/sandbox.go
  - internal/sandbox/sandbox_linux.go
  - internal/worker/descriptors.go
  - internal/worker/engine_sandbox_retryable_test.go
  - internal/worker/worker.go
  - scripts/conf05-crosscheck.sh
  - scripts/sandbox-liveproof.sh
covered_digest: "v2:sha256:b62203bd68f89f13f3bb92ae5c15b870d79acd5e7be9482b1c94ae3ba731d52e"
behavior_unverified: 0
overrides_applied: 0
re_verification:
  previous_status: passed
  previous_score: "15/15"
  reason: "Report read `stale`: phase 70 changed files this report covered (html control rebuilt in 70-06, sharedTmpGuard and preflight contexts in 70-07, sandbox package and worker.go additions for the document class)."
  gaps_closed: []
  gaps_remaining: []
  regressions: []
gaps: []
advisory:
  - finding: "The chromium-worker chart header (deploy/chart/octoconv/templates/deployment-chromium-worker.yaml:15-17) still says 'a negative control confirmed the method would have caught a real /dev/shm dependency had one existed'. After 70-06 the control is an inotify observation (control=1 production=0), not the phase-69 'flag removed, run must fail' control the UAT root_cause describes. The sentence is still true in the new sense; the wording is only ambiguous about which control."
    category: other
    reason: "Comment wording only. No behavior, no must-have affected. SANDBOX-PATHS.md html section (corrected in 70-06) carries the accurate account."
    evidence_status: "direct: chart file read today, control rebuilt in chromium_live_test.go:413-470"
  - finding: "Open point recorded by 70-06 and SANDBOX-PATHS.md: behaviour of Chromium when memfd_create is denied (e.g. by a custom seccomp profile) was not measured. The deployed profile (Docker default, no seccomp override in chart or compose: grep empty) allows memfd_create."
    category: other
    reason: "Out of the deployed configuration; already documented in the decision record. Would matter only if an operator adds a seccomp profile that blocks memfd_create."
    evidence_status: "SANDBOX-PATHS.md:278-280; grep -i seccomp over chromium deployment, values.yaml, docker-compose.yml empty"
  - finding: "WR-02 (69-REVIEW.md): scripts/sandbox-liveproof.sh's CSP_LIVE_ROWS=18 is a hand-maintained integer that must track cspShapeTable's row count. Fails loudly on divergence."
    category: other
    reason: "Still `open` in 69-REVIEW-DISPOSITION.md. Maintainability trap, not a vacuous-pass risk. Carried forward unchanged."
    evidence_status: "69-REVIEW.md file:line evidence; liveproof line 33-36 still carries the constant, the 70-09 gate printed 'csp-live: 18 rows'"
  - finding: "IN-02 (69-REVIEW.md): chromium_live_test.go's liveEngineRun duplicates exec.go's process-group hardening without a drift guard."
    category: other
    reason: "Still `open` in 69-REVIEW-DISPOSITION.md. Informational. Carried forward unchanged."
    evidence_status: "69-REVIEW.md file:line evidence"
---

# Phase 69: HTML, Probe & the /tmp Decision Verification Report (Re-verification on the phase-70 tree)

**Phase Goal:** `html` и `probe` под Landlock, и решение по `/tmp` принято и записано с причиной
(ROADMAP.md, Phase 69).
**Verified:** 2026-10-01 (tree at commit `ebcacda`; non-planning files byte-identical to `5db6c74`,
the commit the live gate was built from: `git diff --stat 5db6c74 HEAD -- . ':(exclude).planning'` is empty)
**Status:** passed
**Re-verification:** Yes. The previous report (2026-09-29, `a9716fd`, `passed` 15/15) read `stale`
because phase 70 changed files it covered.

## What changed since the previous verification, and what it means for phase 69

`git diff --stat a9716fd HEAD -- . ':(exclude).planning'` touches 44 files. Those that bear on html/probe:

| Change (phase 70) | Touches | Verdict on phase-69 truths |
|---|---|---|
| 70-06: html `/dev/shm` control rebuilt. `chromium_live_test.go` step e replaced; new `devshm_observer_linux_test.go` / `devshm_observer_other_test.go`; `Dockerfile.chromium-worker` comment (Chromium unpinned by user decision 2026-09-30); `sandbox-liveproof.sh` echo lines; `SANDBOX-PATHS.md` and debug record corrected. `htmlsandbox.go` and `chromium.go` unchanged. | Truth 5 (G-69-3) | Re-examined in full below. Property still proven. |
| 70-07: `sharedTmpGuard` refuses every concurrency other than exactly 1 (was: above 1). Tests in `sandboxexec_test.go`, `htmlsandbox_test.go`. All eight workers derive the preflight context from the signal context. `cmd/chromium-worker` and `cmd/probe-worker` edited accordingly. | Truths 6, 8, 9 | Strictly stronger guard; preflight still precedes `db.Connect`. Holds. |
| 70-03/70-04: `internal/sandbox` gains a `Socket` (`sock:`/MAKE_SOCK) list; `MAKE_SOCK` is now in the handled set for every class; document class grants it, every other class leaves it empty. `sandboxcoverage_test.go` gains the `document` class and empties `sandboxPendingFiles`. `worker.go` adds the sandbox-fault-first rule to `isDocumentTerminal` only. | Truths 11, 12, 10 | Additive. Html/probe rulesets render unchanged; both classes pass the live gate on the image built from this code. Holds. |
| docker-compose.yml, configmap.yaml | Truth 5 | Only comment edits for html/chromium (`git diff ... \| grep -iE 'html\|chromium\|probe'`). No volume, size or flag added. |

No html/probe source file other than those named above was modified.

## The rebuilt /dev/shm check against G-69-3

G-69-3 (69-UAT.md): *the chromium-worker chart carries no `/dev/shm` mount that the ruleset denies
anyway, and the html worker needs no `/dev/shm`.* The property to hold is "html needs no /dev/shm".

**What phase 69 did:** ran the engine without `--disable-dev-shm-usage` under a ruleset that denies
`/dev/shm` and demanded that the run FAIL on it (a negative control). **Why that had to change:**
70-06 measured Chromium 154.0.8037.57 (`chromium-devshm-measure-70-06.txt`, seven conditions, strace -ff,
84 MB fixture): it creates nothing under `/dev/shm` in any condition; shared memory comes from
`memfd_create`; with the flag off and `/dev/shm` denied it falls back to `memfd_create` and exits 0.
So the old control could no longer fail, and the gate went red on a version bump, not on a real dependency.
The user approved the redesign in phase 70.

**What proves the property now** (`chromium_live_test.go`, `TestChromiumLargeDocumentNeedsNoDevShmLive`,
read in full today):

1. **Production path under the production ruleset, `/dev/shm` not granted** (steps c and d, unchanged from
   phase 69): `conv.Convert` on an 84,543,422-byte document at `--cpus=2 --memory=2g` exits 0, the PDF carries
   the `/Title` marker and is at least 10 MiB (20,460,395 bytes in the final gate), stderr does not mention
   `/dev/shm`. `htmlReadWrite` (2 entries) does not name `/dev/shm`; the conf05 cross-check prints
   `MATCH: html readonly (15) engines (1) readwrite (2)` today. A missing `/dev/shm` is therefore not a
   failure for the production run. This alone already establishes "html needs no /dev/shm" for the deployed
   configuration.
2. **Observation with `/dev/shm` reachable** (step e): the production argv, unchanged, under the same ruleset
   plus a test-only `rw:/dev/shm`, renders the marker and an inotify watch on `/dev/shm`, running in the
   test process outside any Landlock ruleset, sees zero created entries. A fallback cannot hide a wish for the
   directory, because nothing is denied.
3. **Non-vacuity control** (step e1): `dash` (granted by `htmlReadOnly`) creates one entry under `/dev/shm`
   through the same helper and ruleset, and the observer must see it, else the test fails with
   `the method cannot see a /dev/shm dependency`. `IN_Q_OVERFLOW` counts as activity, so dropped events
   cannot produce a clean result.
4. **Live result on the image built from this code** (`liveproof-8class-70-09.txt`, `5db6c74`):
   `PASS: html large-document-without-dev-shm`, `chromium: Chromium 154.0.8037.57`,
   `dev-shm-observe: control=1 production=0`, `large-doc: fixture=84543422 bytes pdf=20460395 bytes wall=5.09581053s`.
   The transcript was also taken separately at 70-06 (`liveproof-html-70-06.txt`) including the observer-blind
   inversion, which failed with the expected message.

**Is something weaker than phase 69?** One thing, stated honestly: the rebuilt test no longer demonstrates
"flag removed, denied, run fails", because on Chromium 154 that does not happen, and 70-06 says the flag is
not load-bearing on this version (inversions ii and iii were deliberately not run: they cannot fail by
construction). The property G-69-3 actually asserts, that the worker needs no `/dev/shm` mount, is not
weakened: it rests on (1), which has no dependence on the flag or the version, and on (2) and (3), which
observe the effect directly instead of inferring it from a failure. The residual exposure, Chromium's
behaviour if `memfd_create` is denied, is out of the deployed configuration (no seccomp override in chart or
compose) and already recorded (advisory 2). **Verdict: the rebuilt check still proves G-69-3.**

No `/dev/shm` mount, size or flag exists in the deployment artifacts today:
`grep -niIE 'shm|dshm'` over the chromium deployment template, `values.yaml`, `docker-compose.yml` and
`sandbox-liveproof.sh` returns only the G-69-3 comments, a chart-header cross-reference, test names and
the liveproof invocation of `TestChromiumLargeDocumentNeedsNoDevShmLive`. The only `--shm-size` text in the
script is a comment saying it was removed.

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | ROADMAP SC1: the `/tmp` decision is taken and recorded with its reason (CONF-06) | ✓ VERIFIED | `.planning/research/SANDBOX-PATHS.md` decision section (`## Решение по /tmp (CONF-06, фаза 69)`); `REQUIREMENTS.md:62` `CONF-06 \| 69 \| Complete`. The html section now also carries the 70-06 measurement (memfd_create, open point on seccomp). |
| 2 | ROADMAP SC2: both classes convert under their rulesets; effectiveness judged by result **content** | ✓ VERIFIED | `HTMLPreflight` and `ProbePreflight` judge by `/Title` and `gs`/`pdfinfo` output content (unchanged files). Today: `go test -count=1 ./internal/convert/` ok (92.5s). Live on an image built from the current code: html `ALL PASS` incl. `confined-startup` ("the engine rendered inside the job directory and was denied a file outside every grant"), `preflight-catches-restrict-self`; probe 4/4 (`probe sandbox proven: every probe engine ran inside the job directory and was denied a file outside it`). Eight-class transcript ends `ALL PASS (40 checks)`. |
| 3 | ROADMAP SC3: chromium grant carries the wrapper, its interpreter and the real binary (D-08) | ✓ VERIFIED | `bash scripts/conf05-crosscheck.sh html probe` today: `MATCH: html readonly (15) engines (1) readwrite (2)` and `MATCH: probe readonly (10) engines (4) readwrite (0)`. Live `PASS: html wrapper-as-measured`. |
| 4 | G-69-2 closed: the JS-disable CSP governs every script, including one before the literal `<head>`/`<html>`, behind a decoy, or in a framed raw upload | ✓ VERIFIED | `injectCSPFirst` in `chromium.go` has zero `headOpenPrefix`/`htmlOpenPrefix`/`bytes.Index(` matches (awk+grep count 0 today); `chromium.go` unchanged since `a9716fd`; the 24-row unit test runs inside today's green `./internal/convert/`; live `PASS: html csp-precedes-every-script` with `csp-live: 18 rows: every control executed its script, every production render blocked it` on the current code. |
| 5 | G-69-3 closed: html needs no `/dev/shm`; no `/dev/shm` mount or sizing in chart, compose or gate; rationale points to `SANDBOX-PATHS.md` | ✓ VERIFIED | See the section above. The mechanism changed in 70-06 (observation control instead of denied-and-fails); the property and its proof hold. Live `large-document-without-dev-shm` PASS at Chromium 154.0.8037.57. Deployment grep clean. `bash scripts/ci-local.sh helm` ran PASS in the 70-09 local gate on this code (`5db6c74`). |
| 6 | CONF-06 concurrency guard: a shared-`/tmp` class refuses a concurrency it cannot run safely | ✓ VERIFIED (strengthened) | `sharedTmpGuard` (`sandboxwrap.go:247`) now returns nil only for an unconfined sandbox, a writable set without `/tmp`, or concurrency exactly 1. Above 1 keeps the old text; below 1 (asynq would run NumCPU tasks) is refused with its own message. Keyed on the grant set, not a class name. Tests (`TestSharedTmpGuard` rows 0 and -1, `TestConfinedChromium` for 0, -1, nil helper at -1/0/1) pass today. Live: `PASS: html concurrency-guard` (`HTML_WORKER_CONCURRENCY=2`); below-1 refusals on the built chromium image in `startup-guards-70-07.txt` part 1. |
| 7 | CONF-06 sweep of the shared `/tmp` before every html task | ✓ VERIFIED | `htmlsandbox.go` `sweepSharedTmp` unchanged; live `PASS: html sweeps-shared-tmp`. A new hazard is recorded by 70-09 as a todo (a test that builds `ConfinedChromium`+reaches `Convert` sweeps the host `/tmp`): it concerns test construction, not the production sweep. |
| 8 | CONF-02: both workers refuse to start without Landlock, naming the escape hatch, off by default | ✓ VERIFIED | Live on the current images: html `refuses-without-landlock` names `HTML_SANDBOX_DISABLED=true`; probe names `PROBE_SANDBOX_DISABLED=true`; both `escape-hatch` checks PASS. `cmd/*-worker/main.go` diff since `a9716fd` is only the context change and a comment. |
| 9 | CONF-04: preflight precedes `db.Connect`/queue registration | ✓ VERIFIED | Both mains: the preflight is before `db.Connect`; 70-07 changed only its parent context (`context.WithTimeout(ctx, 60*time.Second)`, `ctx` the signal context), so SIGTERM during the preflight now ends it (document image measured 0.31 s; chromium and probe use the same construct). Live: `confined-startup` output shows the proof line before the `postgres:` line. `go build ./... && go vet ./...` clean today. |
| 10 | Sandbox faults are retryable, never blamed on the client file | ✓ VERIFIED | `go test -count=1 ./internal/worker/` ok (18.4s) today, includes `TestEngineSandboxUnavailableIsRetryableForEveryConfinedClass` and `TestEngineSandboxControlShapesAreTerminal` for html and probe (plus the new document case). |
| 11 | CONF-01/CONF-05: html/probe granted sets equal the measured document, held by a test | ✓ VERIFIED | `scripts/conf05-crosscheck.sh html probe` today: two `MATCH:` lines. 70-09's eight-class run: eight `MATCH:` lines and an inversion failing both the pinned test and the cross-check. |
| 12 | Coverage allowlist is correct; every html/probe exec goes through `engineExec` | ✓ VERIFIED | `sandboxPendingFiles` is now empty (phase 70 confined the document class, which was its only content), so no file is exempt. `TestConfinedClassesExecOnlyThroughTheSandbox` is in today's green run. Non-vacuity list still includes `chromium.go`, `sanitizepdf.go`, `sanitizeverify.go`. |
| 13 | Build/vet/test pass on this commit (pinned toolchain) | ✓ VERIFIED | `GOTOOLCHAIN=go1.26.4 go build ./... && go vet ./...` clean (`BUILDVET-OK`). `go test -count=1 ./internal/convert/ ./internal/sandbox/ ./internal/worker/ ./cmd/...`: `ok` convert 92.5s, sandbox 0.95s, worker 18.4s, cmd/api, audio-worker, manage-clients, mcp-http, ocr-worker, probe-worker, tectonic-worker ok; the rest `no test files`. No failure. |
| 14 | Helm chart renders with no `/dev/shm` mount/value for any overlay | ✓ VERIFIED | `ci-local` gate and helm tiers PASS on commit `5db6c74` (70-09 evidence: `### tier helm: PASS`), which has identical non-planning content to HEAD. Chart deployment and `values.yaml` grep clean today. Not re-run (needs `helm`; unchanged inputs). |
| 15 | Class-closure inversion for `chromium.go`: a literal unwrapped exec fails the coverage scanner by name | ✓ VERIFIED | Reproduced today on a scratch `git archive HEAD` copy (repo untouched; `git status --porcelain` over internal/cmd/scripts/deploy empty, scratch removed). Baseline `ok` (8.7s). With `chromium.go:240` turned into `runCommand(ctx, "chromium-headless-shell", ...)`: `FAIL: chromium.go:240: chromium.go calls runCommand directly -- class files must route every engine invocation through engineExec.run/.stream` and `found zero engineExec calls in chromium.go ... (non-vacuity check)`. |

**Score:** 15/15 truths verified. 0 failed, 0 behavior-unverified, 0 overrides.

### Human approvals carried forward

The substance of phase 69's approved items is unchanged. 69-UAT.md (3/3 pass; G-69-2 and G-69-3 recorded as
resolved by 69-04 and 69-05) stands. The fourth class-closure inversion, once a human item and resolved by
reproduction in the previous report, was reproduced again today (truth 15). The one change needing approval
was the 70-06 replacement of the G-69-3 control; the user approved that redesign in phase 70.

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `.planning/research/SANDBOX-PATHS.md` | `/tmp` decision; html section incl. 70-06 measurement | ✓ VERIFIED | decision section present; cross-check matches |
| `internal/convert/htmlsandbox.go`, `probesandbox.go` | rulesets, preflights, sweep | ✓ VERIFIED | unchanged since `a9716fd`; in today's green run |
| `internal/convert/sandboxwrap.go` | shared guard | ✓ VERIFIED | stricter guard at :247, tested |
| `internal/convert/chromium.go` | document-start CSP splice; engine exec only via `c.exec(workDir).run` | ✓ VERIFIED | unchanged; inversion shows the scanner bites |
| `internal/convert/chromium_live_test.go` + `devshm_observer_{linux,other}_test.go` | rebuilt `/dev/shm` control | ✓ VERIFIED | read in full; control + production observation, substantive, wired into the gate (`sandbox-liveproof.sh:540,580`) |
| `internal/convert/sandboxcoverage_test.go` | scanner catches raw exec in a class file | ✓ VERIFIED | load-bearing for `chromium.go` (truth 15) |
| `cmd/chromium-worker/main.go`, `cmd/probe-worker/main.go` | refusal + preflight before Postgres | ✓ VERIFIED | build/vet clean; live refusals on current images |
| chart deployment files, `values.yaml`, `docker-compose.yml` | no `/dev/shm` mount/size; escape hatches forwarded unset | ✓ VERIFIED | grep clean |
| `scripts/sandbox-liveproof.sh`, `scripts/conf05-crosscheck.sh` | live gate and doc/code cross-check | ✓ VERIFIED | cross-check re-run today; gate transcript on current code |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `sandboxcoverage_test.go` | `chromium.go` | go/types AST scan | ✓ WIRED | mutation today |
| `scripts/conf05-crosscheck.sh` | `SANDBOX-PATHS.md` ↔ Go sets | doc blocks vs Go vars | ✓ WIRED | two `MATCH:` today (html, probe) |
| `cmd/*-worker/main.go` | `ConfinedChromium` guard, `HTMLPreflight` / `ProbePreflight` | before `db.Connect` | ✓ WIRED | main.go read; live startup order in transcript |
| `sandbox-liveproof.sh` | `TestChromiumLargeDocumentNeedsNoDevShmLive` | cross-compiled test binary in the built image | ✓ WIRED | `:540`, pass pattern `:580`, echoes `chromium:` and `dev-shm-observe:` `:593-595` |
| chart chromium-worker header | `SANDBOX-PATHS.md` | cross-reference comment | ✓ WIRED | header read today |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|--------------------|--------|
| html large-document check | `controlSeen`, `prodSeen` | inotify events read from the kernel queue on `/dev/shm` by a process outside the ruleset | yes: control=1 observed from a real `dash` create | ✓ FLOWING |
| html large-document check | PDF output | real `chromium-headless-shell` via the confined helper, `/Title` marker and size floor asserted | yes | ✓ FLOWING |

### Behavioral Spot-Checks (run today, tree `ebcacda`)

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Build + vet, pinned toolchain | `GOTOOLCHAIN=go1.26.4 go build ./... && go vet ./...` | clean | ✓ PASS |
| Package tests | `GOTOOLCHAIN=go1.26.4 go test -count=1 ./internal/convert/ ./internal/sandbox/ ./internal/worker/ ./cmd/...` | all `ok`, none FAIL | ✓ PASS |
| Doc/code cross-check | `bash scripts/conf05-crosscheck.sh html probe` | `MATCH: html ...`, `MATCH: probe ...` | ✓ PASS |
| chromium.go inversion | scratch copy, `chromium.go:240` -> `runCommand(...)` | FAIL naming `chromium.go:240`; baseline ok | ✓ PASS |
| Live gate, html and probe | not re-run (brief: no docker, no image rebuild) | `liveproof-8class-70-09.txt`: `ALL PASS (40 checks)`, every build block names `5db6c74`; non-planning tree at HEAD identical to `5db6c74` | ? SKIPPED, evidence carried from a transcript of the same code |
| `OCTOCONV_CHROMIUM_LIVE` test on this host | not runnable (darwin; the test skips outside the built image) | n/a | ? SKIPPED, same reason |

### Probe Execution

The phase's runnable proofs are `scripts/sandbox-liveproof.sh` and `scripts/conf05-crosscheck.sh`. The
cross-check was run today and passed. The live gate was not re-run (no docker per the brief); its transcript
was built from source identical to HEAD. No `scripts/*/tests/probe-*.sh` files exist.

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| CONF-01 | 69-01..69-05 | engine cannot read outside job dir + grants | ✓ SATISFIED (html/probe); REQUIREMENTS.md `Complete` (phases 67-70) | live `confined-startup`, preflight negatives, cross-check |
| CONF-02 | 69-01, 69-02 | refuse to start, name the variable | ✓ SATISFIED | live refusal text for both classes |
| CONF-04 | 69-01..69-03 | preflight on the real engine, judged by output | ✓ SATISFIED | `HTMLPreflight` / `ProbePreflight` |
| CONF-05 | 69-01..69-05 | granted set equals `SANDBOX-PATHS.md`, held by a test | ✓ SATISFIED | cross-check today; scanner load-bearing |
| CONF-06 | 69-01, 69-02 | `/tmp` decision for html and probe | ✓ SATISFIED; `Complete` | decision section, stricter guard, sweep |

No orphaned requirements: REQUIREMENTS.md maps CONF-01/02/04/05/06 to phase 69 and each is claimed by a plan.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `cmd/probe-worker/main.go`, `deploy/chart/octoconv/templates/deployment-probe-worker.yaml` | 173 / 22 | word "PLACEHOLDER" in a historical comment documenting a correction | ℹ️ Info | not a placeholder |

No TBD/FIXME/XXX markers in the files this phase covers that were touched by phase 70 (`git diff` of the
chromium/html/probe-related files shows none added). No stubs.

### Human Verification Required

None.

### Gaps Summary

No gaps. The html/probe confinement, the recorded `/tmp` decision and the G-69-1..3 closures hold on the current
code. The phase-70 changes touching this phase either strengthen a guard (concurrency exactly 1), change only
a context parent (preflight), are additive for other classes (Socket/MAKE_SOCK, document descriptors), or
replace one test's mechanism (the `/dev/shm` control) with one that proves the same property more directly.
Four advisories remain and do not block: the chart header wording on "negative control", the unmeasured
`memfd_create`-denied case, WR-02 and IN-02 from the phase-69 review. The earlier advisory about
`WINDOWS.md` row 1 is gone: the ledger now reads `open_count: 0`.

---
*Verified: 2026-10-01*
*Verifier: Claude (gsd-verifier)*
