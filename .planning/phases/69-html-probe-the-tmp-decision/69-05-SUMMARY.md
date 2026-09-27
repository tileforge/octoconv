---
phase: 69-html-probe-the-tmp-decision
plan: 05
subsystem: security
tags: [chromium, dev-shm, landlock, helm, docker-compose, g-69-3]

# Dependency graph
requires:
  - phase: 69-html-probe-the-tmp-decision (plan 04)
    provides: the document-start CSP splice and frame-refusing CSP (G-69-2), and the in-image live test runner (scripts/sandbox-liveproof.sh html, internal/convert/chromium_live_test.go) this plan reuses
provides:
  - "TestChromiumLargeDocumentNeedsNoDevShmLive: an 80 MiB production-scale HTML document (18 page-break sections, incompressible noise PNGs, a 5000-row table) converts through the confined production ChromiumConverter.Convert at the deployed limits (--cpus=2 --memory=2g) with no /dev/shm mount, judged by content (/Title marker, 10 MiB PDF floor) -- plus a negative control proving the method would catch a real /dev/shm dependency if one existed"
  - "scripts/sandbox-liveproof.sh: new 'html large-document-without-dev-shm' check wired after the CSP live check; html's own --shm-size docker-run flag dropped"
  - "deploy/chart/octoconv/templates/deployment-chromium-worker.yaml: the dshm volumeMounts/volumes block removed (7 lines, confirmed by a before/after helm template diff); the D-10 comment replaced by a cross-reference to SANDBOX-PATHS.md"
  - "deploy/chart/octoconv/values.yaml: chromiumWorker.shmSize removed"
  - "docker-compose.yml: chromium-worker's shm_size and its stale comment removed, replaced by a one-line cross-reference; deploy.resources.limits (2.0 CPU / 2g) unchanged"
  - ".planning/research/SANDBOX-PATHS.md: the 2026-09-27 large-document reconfirmation recorded in the html section, right after the existing '/dev/shm и /run не нужны' line"
  - ".planning/debug/chromium-dev-shm-mount.md: Evidence entry correcting the '~64Mi Docker default' assumption with the measured ~7.8 GiB default and the Task 1 live run; fix_applied line added"
affects: [69-VERIFICATION, 69-UAT, 70]

# Actuals (#2632)
actuals:
  tokens: 6653
  tasks: 2
  commits: 2
  plan_head_before: 140bec99f652c1fe3804045294f92e7373e6254d
  plan_head_after: 57715bbff75ee664f074b090515efaba15465158

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "A negative control that removes the ONE flag under test (--disable-dev-shm-usage) from the SAME argv builder (chromiumPrintArgs), rather than a hand-written second command line -- proves the test method would see a real dependency, without risking the control drifting from the production argv over time"
    - "A stale rationale is retired by cross-reference, not by a second restated claim: the chart/compose comments now point at SANDBOX-PATHS.md's measurement instead of repeating (and risking re-diverging from) the reasoning inline"

key-files:
  created: []
  modified:
    - internal/convert/chromium_live_test.go
    - scripts/sandbox-liveproof.sh
    - deploy/chart/octoconv/templates/deployment-chromium-worker.yaml
    - deploy/chart/octoconv/values.yaml
    - docker-compose.yml
    - .planning/research/SANDBOX-PATHS.md
    - .planning/debug/chromium-dev-shm-mount.md

key-decisions:
  - "The large-document check's transcript states the size actually in effect (~7.8 GiB, OrbStack's default) rather than the diagnosis session's assumed ~64Mi -- the validity of dropping the mount never depended on that size: the ruleset denies /dev/shm at any size, which the negative control (failing on /dev/shm, not on an exhausted tmpfs) demonstrates"
  - "The negative control is built by stripping --disable-dev-shm-usage out of chromiumPrintArgs's own output (withoutDisableDevShm), never a hand-written duplicate argv -- so the control can never silently diverge from the production flag set"
  - "html's shared docker-run flag (formerly --shm-size=256m) was dropped from scripts/sandbox-liveproof.sh entirely, not merely left commented out -- class_row's fifth field is now '-' for every class, matching the measurement that it was never needed"
  - "CONF-01/CONF-05 stay 'In Progress' in REQUIREMENTS.md after this plan (requirements.mark-complete ran and correctly declined to flip them): both requirements span phases 67-70, and phase 70 has not landed yet -- this plan's contribution (html/probe rulesets, now with the /dev/shm question closed) is complete, but the requirement as a whole is not"

requirements-completed: [CONF-01, CONF-05]

coverage:
  - id: D1
    description: "Large-document live check (TestChromiumLargeDocumentNeedsNoDevShmLive): a production-scale HTML document converts through the confined production Convert path at the deployed limits with no /dev/shm, and a negative control proves the method would catch a real dependency"
    requirement: "CONF-01"
    verification:
      - kind: integration
        ref: "scripts/sandbox-liveproof.sh html -- \"PASS: html large-document-without-dev-shm\", \"ALL PASS (9 checks)\""
        status: pass
      - kind: unit
        ref: "go vet ./internal/convert/ && go test -count=1 ./internal/convert/ (live test skips on host, compiles clean)"
        status: pass
    human_judgment: false
  - id: D2
    description: "Chart/compose/gate carry no /dev/shm mount or sizing for chromium-worker, and every place that used to justify one now cross-references the measurement"
    requirement: "CONF-01"
    verification:
      - kind: integration
        ref: "helm lint/template for values-local.yaml + every values-*.yaml overlay; rendered chromium-worker Deployment has no volumes/volumeMounts (yq); conf05-crosscheck.sh html MATCH; ci-local.sh gate/race/helm all PASS"
        status: pass
    human_judgment: true
    rationale: "Task 2's own <verify><human-check> asks the user to review the SUMMARY's before/after helm-template diff, the large-document transcript, and the final 9-check gate directly -- WR-01/G-69-3 was a user decision (drop the mount, contingent on the large-document evidence) the user wanted to see, not merely re-asserted by the executor."

duration: ~40min
completed: 2026-09-27
status: complete
---

# Phase 69 Plan 05: Large-Document /dev/shm Reconfirmation + Mount Removal (G-69-3) Summary

**Closes UAT gap G-69-3: an 80 MiB production-scale HTML document converts through the confined production ChromiumConverter.Convert path at the deployed limits with no /dev/shm mount and no Landlock denial (negative control included), and the chromium-worker chart, compose service, and live gate no longer mount, size, or claim to need /dev/shm.**

## Performance

- **Duration:** ~40 min (approx.)
- **Started:** 2026-09-27T03:30:00Z (approx.)
- **Completed:** 2026-09-27T04:07:02Z
- **Tasks:** 2
- **Files modified:** 7

## Accomplishments

- `TestChromiumLargeDocumentNeedsNoDevShmLive` (`internal/convert/chromium_live_test.go`): builds a deterministic ~80 MiB HTML fixture (18 page-break sections each with a heading, a paragraph, and one of three incompressible noise PNGs rotated across sections, plus a 5000-row table), converts it through `ConfinedChromium`'s production `Convert` path, then re-runs the engine directly on the rendered output twice through a new test-only `liveEngineRun` helper (mirrors `engineExec.run`'s `wrapEngine`/env handling but surfaces the exit status and stderr `run()` discards on success): once with the production argv (exit 0, no `/dev/shm` in stderr, marker present) and once with `--disable-dev-shm-usage` stripped as a negative control (non-zero exit, stderr names `/dev/shm`, marker absent).
- `scripts/sandbox-liveproof.sh`: new `check_html_large_document_live` / `run_html_large_document_live_test`, wired in as the `"html large-document-without-dev-shm"` check right after the CSP live check, running at the deployed limits (`--cpus=2 --memory=2g`); html's own `--shm-size=256m` docker-run flag removed from `class_row` (now `-` for every class, like the other six).
- `deploy/chart/octoconv/templates/deployment-chromium-worker.yaml`: the `dshm` `volumeMounts`/`volumes` block deleted (7 lines, confirmed by a before/after `helm template` diff -- see Live Evidence below); the D-10 header comment replaced with a cross-reference to `SANDBOX-PATHS.md`'s html section.
- `deploy/chart/octoconv/values.yaml`: `chromiumWorker.shmSize` deleted (it fed only the removed volume).
- `docker-compose.yml`: `chromium-worker`'s `shm_size: "256m"` and its four-line "belt-and-suspenders" comment deleted, replaced by a one-line cross-reference; `deploy.resources.limits` (`cpus: "2.0"`, `memory: 2g`) untouched.
- `.planning/research/SANDBOX-PATHS.md`: a dated paragraph recording the large-document reconfirmation (fixture/PDF sizes, the deployed limits, the observed ~7.8 GiB `/dev/shm`, and why the size does not matter) inserted right after the existing "`/dev/shm` и `/run` в наборе не нужны" line; the `conf05:html:*` marker blocks untouched.
- `.planning/debug/chromium-dev-shm-mount.md`: an Evidence entry appended correcting the "~64Mi Docker default" text in this session's own earlier findings, and a `fix_applied` note naming every file this plan changed.

## Task Commits

Each task was committed atomically:

1. **Task 1: Large document through the confined engine, negative control, live gate** (`type="tracer"`)
   - `38b6f40` (feat) -- `TestChromiumLargeDocumentNeedsNoDevShmLive`, `liveEngineRun`, `noisePNG`/`buildLargeDocFixture`/`withoutDisableDevShm`/`logDevShmMount`; `scripts/sandbox-liveproof.sh`'s new check and dropped `--shm-size` flag
   - Tracer feedback gate: `gate` not set to `blocking-human`; interactive, `HUMAN_VERIFY_MODE` default (`end-of-phase`), and Task 1's own `<verify>` is automated-only -- re-ran Task 1's exact `<verify>` command end-to-end after the commit (`go vet`, `go test -count=1`, the `--shm-size` grep, and `scripts/sandbox-liveproof.sh html` ending `ALL PASS (9 checks)`); all passed, so execution continued straight to Task 2 with no checkpoint.
2. **Task 2: Remove the /dev/shm mount and sizing, point at the measurement, record it, final gates** (`type="auto"`)
   - `57715bb` (fix) -- chart/values/compose edits, `SANDBOX-PATHS.md`/debug-session evidence, before/after helm-template diff, `ci-local.sh gate race helm` all PASS, final `sandbox-liveproof.sh html` re-run: `ALL PASS (9 checks)`

**Plan metadata commit:** (this commit, docs)

## Files Created/Modified

- `internal/convert/chromium_live_test.go` -- new `TestChromiumLargeDocumentNeedsNoDevShmLive`, `liveEngineRun`, `noisePNG`, `buildLargeDocFixture`, `withoutDisableDevShm`, `logDevShmMount`; existing `TestChromiumCSPPrecedesEveryScriptLive` untouched
- `scripts/sandbox-liveproof.sh` -- `run_html_large_document_live_test`/`check_html_large_document_live`, called after `check_html_csp_live`; `class_row`'s html row loses its `--shm-size=256m` extra arg
- `deploy/chart/octoconv/templates/deployment-chromium-worker.yaml` -- `dshm` volume/volumeMount removed; header comment cross-references `SANDBOX-PATHS.md`
- `deploy/chart/octoconv/values.yaml` -- `chromiumWorker.shmSize` removed
- `docker-compose.yml` -- `chromium-worker`'s `shm_size` and its stale comment removed, replaced by a one-line cross-reference
- `.planning/research/SANDBOX-PATHS.md` -- html section gains the 2026-09-27 large-document reconfirmation paragraph
- `.planning/debug/chromium-dev-shm-mount.md` -- Evidence entry + correction appended; `status: diagnosed` and prior content left as-is

## Decisions Made

See `key-decisions` in the frontmatter above.

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

None.

## Live Evidence (for the human-check in Task 2's `<verify>`)

**Before/after `helm template` diff for `deployment-chromium-worker.yaml`** (`values-local.yaml`, `keda.enabled=true`, `prometheus.enabled=true`, dev secrets):

```diff
             limits:
               cpu: "2"
               memory: "2Gi"
-          volumeMounts:
-            - name: dshm
-              mountPath: /dev/shm
-      volumes:
-        - name: dshm
-          emptyDir:
-            medium: Memory
-            sizeLimit: "256Mi"
```

Only removed lines, all 7 inside the `dshm` volume/volumeMount block -- nothing else in the rendered manifest changed.

**Six-overlay `helm lint`/`helm template`** (`values-local.yaml` combined with each of `values-capacity-floor.yaml`, `values-e2e.yaml`, `values-inspect-gate.yaml`, `values-loadproof.yaml`, `values-local.yaml` itself, `values-multireplica.yaml`): all six `1 chart(s) linted, 0 chart(s) failed`, all six `helm template` renders exit 0.

**Rendered Deployment has no volumes/volumeMounts:**
```
$ helm template octoconv deploy/chart/octoconv <secrets/keda/prometheus flags> \
    --show-only templates/deployment-chromium-worker.yaml \
  | yq '.spec.template.spec.volumes, .spec.template.spec.containers[0].volumeMounts'
null
null
```

**`conf05-crosscheck.sh html`:** `MATCH: html readonly (15) engines (1) readwrite (2)`.

**`git diff --exit-code 5747b40 -- internal/convert/exec.go go.mod go.sum`:** exit 0 (D-12).

**Task 1 large-document live check (commit `38b6f40`, image `sha256:5df4e5c3a9418cd7e3aa226505534d8ee05bec38b077cfbed23fb2e043bad97f`, run right after committing Task 1):**
```
PASS: html csp-precedes-every-script
      csp-live: 18 rows: every control executed its script, every production render blocked it
PASS: html large-document-without-dev-shm
      dev-shm: shm /dev/shm tmpfs rw,nosuid,nodev,noexec,relatime,size=8212480k 0 0
      large-doc: fixture=84543422 bytes pdf=20460780 bytes wall=5.937214612s
ALL PASS (9 checks)
```

**Final re-run after Task 2's chart/compose edits (commit `57715bb`):**
```
$ GOTOOLCHAIN=go1.26.4 scripts/ci-local.sh gate race helm
### tier gate: PASS
### tier race: PASS
### tier helm: PASS
ALL REQUESTED TIERS PASSED

$ scripts/sandbox-liveproof.sh html
PASS: html large-document-without-dev-shm
      dev-shm: shm /dev/shm tmpfs rw,nosuid,nodev,noexec,relatime,size=8212480k 0 0
      large-doc: fixture=84543422 bytes pdf=20460777 bytes wall=5.593812169s
ALL PASS (9 checks)
```

The dev-shm size (~7.8 GiB) is the same both runs and matches this host's actual default, not the ~64Mi the diagnosis session assumed -- and, per the plan's own `must_haves`, the claim never rested on that number: the ruleset denies `/dev/shm` at any size, demonstrated by the negative control failing in the engine (stderr naming `/dev/shm`, no marker in its output), never in the helper (`sandbox.SetupExit` was not returned by either run).

## User Setup Required

None -- no external service configuration required.

## Next Phase Readiness

- G-69-3 fully closed: no deployment artifact (chart, compose, or the live gate itself) mounts or sizes `/dev/shm` for chromium-worker, and every place that used to justify one now points at the measurement instead of repeating a superseded pre-Landlock rationale.
- `CONF-01`/`CONF-05` remain **In Progress** in `REQUIREMENTS.md` (correctly -- `requirements.mark-complete` ran and declined to flip them): both span phases 67-70, and phase 70 has not landed. This plan's own contribution to them (html/probe rulesets, the `/dev/shm` question now closed with a repeatable live check) is complete.
- Phase 69's remaining human-verification items from `69-VERIFICATION.md` (the fourth class-closure inversion, blocked by the harness's own tool-permission classifier) are unrelated to this plan and were not in its scope; tracked separately in `WINDOWS.md`/`STATE.md`.
- No blockers for phase 69 verification finalization or for phase 70.

---
*Phase: 69-html-probe-the-tmp-decision*
*Completed: 2026-09-27*

## Self-Check: PASSED

- Key files found on disk: `internal/convert/chromium_live_test.go`, `scripts/sandbox-liveproof.sh`, `deploy/chart/octoconv/templates/deployment-chromium-worker.yaml`, `deploy/chart/octoconv/values.yaml`, `docker-compose.yml`, `.planning/research/SANDBOX-PATHS.md`, `.planning/debug/chromium-dev-shm-mount.md`, this SUMMARY.
- Both task commits (`38b6f40`, `57715bb`) found in `git log --oneline --all`.
- Plan-level `<verification>` re-run on the committed tree (commit `57715bb`): `ci-local.sh gate race helm` -> all three tiers PASS; `scripts/sandbox-liveproof.sh html` -> `ALL PASS (9 checks)`; `git diff --exit-code 5747b40 -- internal/convert/exec.go go.mod go.sum` -> exit 0; `grep -c 'func TestChromiumLargeDocumentNeedsNoDevShmLive('` -> 1; `grep -c 'large-document-without-dev-shm'` -> 2; `grep -rnE 'shmSize|dshm' deploy/chart/` -> no matches; `yq '.services."chromium-worker" | has("shm_size")' docker-compose.yml` -> false.
