---
phase: 58-synchronous-inspection
plan: 08
subsystem: testing
tags: [live-gate, kubernetes, orbstack, inspect, liveness-probe, shell, python, helm]

# Dependency graph
requires:
  - phase: 58-synchronous-inspection (plans 01-07)
    provides: "POST /v1/inspect, the byte/time/concurrency ceilings (internal/inspect, internal/api/inspectgate.go), and the :8091 health listener/NetworkPolicy rule this harness drives against"
  - phase: 55-live-gate-multi-replica-quota-correctness
    provides: "the live-gate harness shape and its own recorded round-4 defect/closure, reused as the direct precedent for this harness's pass-condition design"
provides:
  - "Three deterministic, re-runnable, stdlib-only Python fixture generators plus their committed output (PDF/DOCX/MP4), each targeting a specific internal/inspect ceiling and verified against the real analyzer functions before commit"
  - "scripts/inspect-burst-liveproof.sh -- the SC6 live-gate harness itself, ready for an operator to run, not yet run"
  - "deploy/chart/octoconv/values-inspect-gate.yaml -- the gate's own values overlay (api.replicas:1, lowered INSPECT_MAX_WORK_BYTES)"
  - "A placeholder evidence/README.md recording what the eventual live run must produce and analyze"
affects: ["58-08 Task 3 (the actual live gate run, still pending)", "any future phase relying on SC6 having been proven"]

tech-stack:
  added: []
  patterns:
    - "Ceiling-targeting fixtures: pathological inputs sized and shaped to stress a named internal/inspect resource ceiling, not to find a parser-correctness bug -- verified against the real Analyze*/BuildReport functions before commit, with observed (not assumed) findings/checks/coverage recorded in scripts/fixtures/README.md"
    - "Independent poller + separate burst driver, two distinct in-cluster throwaway pods created via `kubectl run` -- reused from Phase 55's live-gate precedent, adapted so CPU contention between the measuring tool and the thing it measures can never confound the result"
    - "Pass condition stated against the actual production contract (failureThreshold:6 consecutive liveness failures) plus an independent ground truth (restartCount), never a zero-tolerance single-probe read -- the direct, deliberate analog of Phase 55's own round-4 harness-defect closure"

key-files:
  created:
    - scripts/fixtures/gen_inspect_burst_pdf.py
    - scripts/fixtures/gen_inspect_burst_docx.py
    - scripts/fixtures/gen_inspect_burst_mp4.py
    - scripts/fixtures/inspect-burst-pathological.pdf
    - scripts/fixtures/inspect-burst-pathological.docx
    - scripts/fixtures/inspect-burst-pathological.mp4
    - scripts/fixtures/README.md
    - scripts/inspect-burst-liveproof.sh
    - deploy/chart/octoconv/values-inspect-gate.yaml
    - .planning/phases/58-synchronous-inspection/evidence/README.md
  modified: []

key-decisions:
  - "Lowered the gate's own INSPECT_MAX_WORK_BYTES to 1 MiB via values-inspect-gate.yaml's api.extraEnv, rather than sizing fixtures against the 8 MiB ConfigMap default -- keeps the three committed fixtures at a few MB total (per this plan's own instruction) while still sitting strictly above the work ceiling and below the upload ceiling"
  - "DOCX fixture entry count computed by the generator script itself (not hand-typed) to land just under internal/api/inspect_handlers.go's EARLIER, cheaper zip-directory structural gate (188,416 declared bytes) -- a naive 4096-entry archive with real (non-empty) filenames trips that gate almost immediately, short-circuiting before internal/inspect/container.go's own scan ever runs"
  - "Declined to chase the 200 MiB zip-bomb-shape finding threshold for the DOCX fixture -- reaching it honestly would require either genuinely writing 200+ MB or hand-patching a ZIP's declared size field after the fact, the exact 'hand-crafted binary' provenance this plan's own constraint prohibits; recorded explicitly in scripts/fixtures/README.md as an intentionally out-of-scope target for this fixture"
  - "Burst driver and poller run as two separate `kubectl run` pods with the driver logic piped via `sh -s` stdin rather than a third committed file -- keeps Task 2's file list at exactly the two files the plan scopes it to, while still reusing the FIFO-barrier concurrency-forcing shape from Phase 55's own burst driver"

requirements-completed: []  # INSP-06/SC6 is proven by the LIVE RUN (Task 3), not by harness authoring alone -- explicitly not claimed complete here.

duration: ~90min
completed: 2026-08-11
---

# Phase 58 Plan 08 (harness-authoring subset): Inspect-burst SC6 live gate — fixtures and harness, not yet run

**Three ceiling-targeting pathological fixtures (PDF/DOCX/MP4) with deterministic Python generators, plus a 800+-line, shellcheck-clean live-gate harness and its values overlay — built and verified locally, but the live OrbStack run itself (Task 3) has not happened.**

## Performance

- **Duration:** ~90 min
- **Tasks:** 2 of 3 (Task 1: fixtures; Task 2: harness + overlay; Task 3: the live run itself is explicitly out of scope for this agent — operator-only, requires a real cluster)
- **Files created:** 10

## Accomplishments

- Three pathological fixtures (`inspect-burst-pathological.{pdf,docx,mp4}`), each generated deterministically by a committed, stdlib-only Python script, each verified against the REAL `internal/inspect` analyzer functions (not assumed) before commit — observed findings/checks/coverage transcribed into `scripts/fixtures/README.md`, including an honest note on what the DOCX fixture does NOT target (the 200 MiB zip-bomb-shape threshold) and why.
- `scripts/inspect-burst-liveproof.sh`: a complete, `bash -n`-clean, `shellcheck`-clean (0 issues) live-gate harness that installs the chart at `api.replicas:1`, provisions a client, runs two SEPARATE in-cluster throwaway pods (an independent `/healthz` poller and a FIFO-barrier burst driver), asserts against the actual liveness contract (`periodSeconds:10`/`failureThreshold:6`) via a max-consecutive-failure check rather than a zero-tolerance single-probe read, cross-checks against `restartCount` before/after as the independent ground truth, verifies every burst response is `200`/`429` with a known `code`, samples `kubectl top pod` against `resources.limits`, captures the applied `INSPECT_MAX_*` ceilings via `helm get values`/pod env introspection, writes two `RUN_ID`-named TSVs, and tears the cluster down unconditionally on exit.
- `deploy/chart/octoconv/values-inspect-gate.yaml`: the gate's own overlay, pinning `api.replicas:1` (D-07's own precondition) and restating all four `INSPECT_MAX_*` boundaries explicitly, `helm lint`/`helm template`-verified when layered with `values-local.yaml` (the same convention every sibling overlay in this chart already requires).
- `.planning/phases/58-synchronous-inspection/evidence/README.md`: a placeholder recording what the eventual live run must produce and analyze, so the shape of the evidence is on record before any run exists.

## Task Commits

1. **Task 1: Pathological fixtures maximizing each analyzer's work** — `1d5337f` (feat)
2. **Task 2: Live-gate harness — in-cluster burst, independent probe polling, checks against the real contract** — `727a302` (feat)
3. **Evidence directory placeholder** — `31102c9` (docs) — not a plan task on its own; committed because the parallel-execution contract for this agent scopes `.planning/phases/58-synchronous-inspection/evidence/README.md` as one of its four files.

**This summary's own commit:** see below (docs, committed immediately after this file is written, per this agent's REQUIRED ORDER instruction).

_Task 3 (the live gate run itself, `type="checkpoint:human-verify"`, `gate="blocking"`) was explicitly excluded from this agent's scope by the orchestrating instructions — it requires an OrbStack operator to prepare the environment (bring the compose stack down, assemble fresh `api`/`worker` images sequentially with a pinned tag) in ways the harness itself refuses to do on its own behalf. It remains open._

## Files Created/Modified

- `scripts/fixtures/gen_inspect_burst_pdf.py` — deterministic PDF generator: `/ObjStm` compression-signal trigger + 190,648 near-miss `/JavaScrip` token repeats, 2,097,152 bytes
- `scripts/fixtures/gen_inspect_burst_docx.py` — deterministic DOCX generator: 3,778 total zip entries (3,772 computed filler + 6 real parts) landing at 187,895/188,416 declared central-directory bytes; four `.rels` candidate parts at 258,040 bytes each with `TargetMode="External"` near each part's own end
- `scripts/fixtures/gen_inspect_burst_mp4.py` — deterministic MP4 generator: exactly 4,096 `free` boxes (mirrors `maxISOBMFFBoxes`) before a tail-anchored `moov` (3,145,728 bytes total) with valid `mvhd`/`tkhd`/`stsd`
- `scripts/fixtures/inspect-burst-pathological.{pdf,docx,mp4}` — the committed generator output
- `scripts/fixtures/README.md` — per-fixture pathology rationale, measured sizes vs. both ceilings, and REAL observed `inspect.AnalyzePDF`/`AnalyzeContainer`/`AnalyzeMedia` + `BuildReport` output (findings/checks/coverage), including a documented `grep -c` vs. `grep -ac` binary-content caveat for the MP4 acceptance command
- `scripts/inspect-burst-liveproof.sh` — the SC6 live-gate harness (executable, 815 lines)
- `deploy/chart/octoconv/values-inspect-gate.yaml` — the gate's values overlay
- `.planning/phases/58-synchronous-inspection/evidence/README.md` — evidence-directory placeholder (force-added; `.planning/` is gitignored by default in this repo, matching every other committed planning artifact's convention)

## Decisions Made

See `key-decisions` in frontmatter above. Additionally:

- Named this summary `58-08-HARNESS-SUMMARY.md` rather than `58-08-SUMMARY.md`, per this agent's explicit instructions, so it cannot collide with the eventual summary that will record eventual Task 3's actual gate run.
- Did NOT touch `STATE.md` or `ROADMAP.md` (explicit instruction) and did NOT run `requirements mark-complete` for INSP-06 — that requirement is proven by the live run, not by harness authoring, and marking it complete here would overclaim.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] shellcheck SC2016 false-positive on intentional mixed-quote construction**
- **Found during:** Task 2, verifying `shellcheck` cleanliness (a stated acceptance criterion)
- **Issue:** The poller's one-liner loop deliberately concatenates single- and double-quoted segments so some `$variable` references expand on this host (at construction time) and others expand only later, inside the remote pod's own shell. `shellcheck` correctly, but unhelpfully, flags this shape as SC2016 ("expressions don't expand in single quotes") even though the non-expansion is the entire point.
- **Fix:** Added a `# shellcheck disable=SC2016` directive immediately above the line, with a preceding comment explaining why the mixed quoting is intentional and what would break if it were "fixed" to all-double-quotes.
- **Files modified:** `scripts/inspect-burst-liveproof.sh`
- **Verification:** `shellcheck scripts/inspect-burst-liveproof.sh` exits 0
- **Committed in:** `727a302` (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (1 bug/tooling-friction fix). No scope creep — the fix is a comment/directive addition, not a behavior change.

## Issues Encountered

- **This plan's own literal `helm lint`/`helm template` acceptance commands (single `-f values-inspect-gate.yaml`, no `values-local.yaml`) fail identically for `values-inspect-gate.yaml` AND for every pre-existing sibling overlay in this chart** (`values-e2e.yaml`, `values-multireplica.yaml` — both verified to fail the same way, same root cause: `secrets.postgresPassword` and friends have no chart-level default, only a `values-local.yaml`-level one). This is a pre-existing, systemic property of the chart, not a defect introduced by this plan's own overlay. Verified instead with the layering every overlay's own header comment (including this new one's) documents as the actual required usage: `helm lint deploy/chart/octoconv -f deploy/chart/octoconv/values-local.yaml -f deploy/chart/octoconv/values-inspect-gate.yaml` — exits 0. `helm template ... | grep -c 'replicas: 1'` under the same layering returns 13 (>= 1, satisfying the stated bar).
- **`grep -c moov` (no `-a`) against the committed binary MP4 fixture returns nothing / exit 1 on this host's `grep` (BSD grep, macOS)**, rather than a numeric count — binary-content detection suppresses `-c`'s normal output unless `-a`/`--text` forces text mode (GNU `grep` differs in presentation but is equally non-numeric by default). Documented in `scripts/fixtures/README.md` with the corrected `grep -ac` invocation and its actual observed output (`1` for the tail window, `0` for the head window), so a future operator re-running this plan's literal acceptance command is not surprised by an apparently-empty result on a passing fixture.
- **`unzip -p ... | grep -c 'TargetMode="External"'` returns `1` (a LINE count), not `4` (the true OCCURRENCE count across all four `.rels` parts)**, because the fixture's rels-part content has no internal newlines and `unzip -p`'s concatenated output across all four matched files is therefore one unbroken stream. Both readings satisfy this plan's own stated bar (`>= 1`); documented in `scripts/fixtures/README.md` alongside the `grep -o | wc -l` form that recovers the true count (`4`), so the divergence is not mistaken for a fixture defect later.
- **`inspect.BuildReport`'s `Coverage.Complete` reports `true` for all three fixtures despite every one of them sitting above the gate's own `INSPECT_MAX_WORK_BYTES` override** — initially assumed this would flip to `false` (an under-calibration to investigate), but tracing `internal/inspect/report.go`'s `buildCoverage` showed this is the CORRECT, honest value: it only flips on a per-Check `ReasonWorkCeilingBytes`/`ReasonWorkCeilingTime` (none of these three fixtures' checks carry those specific reasons) or on the shared `Tracker` being genuinely `Exhausted()` (which requires EXCEEDING the budget, not merely reaching it exactly — and `internal/api/inspect_handlers.go`'s own single-family dispatch charges exactly one full allowance per request, landing AT the ceiling, never past it, today). Documented as a "Coverage note" in `scripts/fixtures/README.md` rather than treated as a bug, since re-verification against the actual shipped code confirmed the behavior is correct and intentional.

## User Setup Required

None — no external service configuration required. The live gate itself (Task 3) requires an OrbStack operator to prepare the environment before running `scripts/inspect-burst-liveproof.sh`; that is a plan-defined checkpoint, not ad hoc user setup, and is explicitly out of this agent's scope.

## Next Phase Readiness

- The harness and fixtures are ready for an operator to run Task 3 exactly as written in `58-08-PLAN.md`'s own `how-to-verify`: bring the compose stack down, assemble `octoconv-api:dev`/`octoconv-worker:dev` sequentially, confirm Kubernetes is enabled in OrbStack with free disk, then `bash scripts/inspect-burst-liveproof.sh 2>&1 | tee /tmp/inspect-gate-$(date -u +%Y%m%dT%H%M%SZ).log`.
- SC6 is NOT yet proven. This phase cannot close until Task 3 runs successfully (or its failure is triaged per the plan's own decision tree) and the real `58-08-SUMMARY.md` (not this harness-authoring summary) records the outcome with committed evidence.
- No blockers to running Task 3 identified during harness authoring — every precondition the harness checks (fixture presence, compose-down, image freshness, `api.replicas==1`, applied `INSPECT_MAX_*` ceilings) is verifiable by the script itself at run time, and every literal acceptance-criteria grep/helm/shellcheck check for this plan's Tasks 1-2 has been run and passed locally, as recorded above.

---
*Phase: 58-synchronous-inspection*
*Completed (harness-authoring subset only): 2026-08-11*
