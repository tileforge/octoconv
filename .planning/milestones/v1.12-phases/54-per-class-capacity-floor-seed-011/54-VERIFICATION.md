---
phase: 54-per-class-capacity-floor-seed-011
verified: 2026-08-06T03:10:02Z
status: passed
score: 11/11 must-haves verified
overrides_applied: 0
overrides:
  - must_have: "Third-party action azure/setup-helm@v4 pinned to a commit SHA (WR-03)"
    reason: "Plan 54-02's recorded threat model (T-54-03) explicitly considered and rejected SHA-pinning this one action because the file's four other third-party actions (actions/checkout@v7, actions/setup-go@v6, docker/setup-buildx-action@v4, docker/bake-action@v7, actions/upload-artifact@v7) are all tag-pinned; pinning only the new job differently was judged inconsistent security theatre rather than a real improvement. Compensating controls (job-level `permissions: contents: read`, helm binary pinned via `version: v4.2.3`) are in place."
    accepted_by: "phase-54 threat model (T-54-03), reaffirmed by code review WR-03 disposition"
    accepted_at: "2026-08-06T00:00:00Z"
  - must_have: "Workflow-level permissions: hoisted onto pre-existing CI jobs (WR-04)"
    reason: "gate/race/docker-build/e2e predate this phase. Adding permissions: to them would edit lines outside this phase's diff and break the plan 54-02 acceptance invariant that the ci.yml change is purely additive (git diff ... | grep -c '^-[^-]' must be 0). The new helm job itself does declare permissions: contents: read. Confirmed still additive-only at HEAD."
    accepted_by: "phase-54 code review WR-04 disposition"
    accepted_at: "2026-08-06T00:00:00Z"
---

# Phase 54: Per-Class Capacity Floor (SEED-011) Verification Report

**Phase Goal:** An operator can choose a non-zero `minReplicaCount` per engine class to eliminate cold-start latency under sustained load, instead of the hardcoded zero every class ships with today.
**Verified:** 2026-08-06T03:10:02Z
**Status:** passed
**Re-verification:** No — initial verification

## Important note on process

This report does **not** rely on 54-01-SUMMARY.md / 54-02-SUMMARY.md claims as evidence. A code
review (`54-REVIEW.md`) found one CRITICAL (CR-01: the D-02 `fail` guards were bypassable because
sprig's `int` silently coerces non-integer `--set` input to `0`) plus six warnings, **after** both
SUMMARYs were written. All verification below was performed against the current `HEAD`
(`9155b1e`), i.e. after the fix commits (`adecb1f`, `fc101ae`, `3a050da`, `f53371f`, `a918397`,
`b64a12d`), by re-running `helm` directly — not by re-reading the SUMMARY text.

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | (ROADMAP SC1) Each of the 7 classes' `minReplicaCount` is a Helm values field, defaulting to today's `0` — zero behavior change | ✓ VERIFIED | `grep -c 'minReplicaCount: 0' values.yaml` = 7; `grep -rn 'minReplicaCount: 0' templates/` = 0 (no hardcoded literal remains); live `helm template` diff vs. pre-phase baseline (`f7eaea8`) at default values shows **0 non-comment differing lines** (49 comment-only diffs) |
| 2 | (ROADMAP SC2) A non-zero floor on one class does not alter another class's ScaledObject/PromQL or scale-from-zero behavior | ✓ VERIFIED | Ran the committed CI job's script verbatim (extracted via `ruby -ryaml`): "OK: document/html/audio/av/ocr/pandoc unchanged", "OK: image changed (expected)"; whole-chart diff between default and `values-capacity-floor.yaml` renders is exactly 4 lines, all `minReplicaCount:`/`replicas:` on the image ScaledObject |
| 3 | (ROADMAP SC3) `helm lint`/`helm template` render cleanly for default values and an example non-zero override, no live cluster required | ✓ VERIFIED | `helm lint` exits 0 for both `deploy/chart/octoconv` and `-f values-capacity-floor.yaml`; both renders contain exactly 7 `ScaledObject`s (`grep -c '^kind: ScaledObject'` = 7 each) |
| 4 | No `scaledobject-*.yaml` contains a hardcoded `minReplicaCount: 0` any more; all 7 read `.Values.keda.<class>.minReplicaCount` via the helper (D-01) | ✓ VERIFIED | All 7 files show `{{ include "octoconv.kedaMinReplicaCount" (dict "class" "<class>" "root" $) }}` at the `minReplicaCount:` line; `grep -rn 'minReplicaCount: 0' templates/` = 0 |
| 5 | Validation + value render live in a single choke point in `_helpers.tpl`, called by a one-line include from every `scaledobject-*.yaml` (D-03) | ✓ VERIFIED | `octoconv.kedaMinReplicaCount` (validates + emits) and `octoconv.kedaFallbackReplicas` (derives via `include` of the former, not a re-read of `.Values` — so validation cannot be bypassed via the fallback path, T-54-02) are the only two `octoconv.keda*` defines; all 14 call sites (7×2) present and correctly parameterized with each file's own class string |
| 6 | `fallback.replicas` renders as `max(1, minReplicaCount)`; at default `0` it still renders `1` (D-05) | ✓ VERIFIED | Live: default image ScaledObject renders `minReplicaCount: 0` / `replicas: 1`; under `values-capacity-floor.yaml` (`keda.image.minReplicaCount: 2`) it renders `minReplicaCount: 2` / `replicas: 2` |
| 7 | A negative `minReplicaCount`, or one greater than that class's `maxReplicaCount`, aborts `helm template` naming the class and both values (D-02) | ✓ VERIFIED | `--set keda.image.minReplicaCount=99` (max=4) → `Error: ... keda.image.minReplicaCount (99) must not exceed keda.image.maxReplicaCount (4)`, rc=1. `--set 'keda.ocr.minReplicaCount=-1'` → `Error: ... keda.ocr.minReplicaCount (-1) must not be negative`, rc=1 |
| 8 | D-02's guard cannot be bypassed by non-bare-integer `--set` input (post-review CR-01 fix — the actual current behavior, not the pre-fix SUMMARY claim) | ✓ VERIFIED | Re-tested directly (not trusting the SUMMARY): `--set keda.image.minReplicaCount=2.9\|99.9\|-0.5\|true\|abc\|""` each now exit 1 with `"... must be a whole number, got ... (kind ...)"`. Pre-fix these all rendered `minReplicaCount: 0` and exited 0 (confirmed by 54-REVIEW.md's CR-01 table; not independently re-verified against the *broken* commit, but the post-fix behavior on `HEAD` is directly confirmed live) |
| 9 | No `scaledobject-*.yaml` still claims `fallback.replicas: 1 holds one replica for this class` | ✓ VERIFIED | `grep -rn 'fallback.replicas: 1 holds one replica' templates/` = 0; spot-checked `scaledobject-image.yaml`'s trigger comment now reads "fallback.replicas holds max(1, keda.image.minReplicaCount) replicas ... instead of falsely scaling to zero" |
| 10 | An example non-zero override overlay exists; the three pre-existing overlays are untouched (D-04) | ✓ VERIFIED | `deploy/chart/octoconv/values-capacity-floor.yaml` exists, single key `keda.image.minReplicaCount: 2`; `git diff f7eaea8..HEAD --stat` shows zero changes to `values-local.yaml`/`values-e2e.yaml`/`values-loadproof.yaml` |
| 11 | A new `helm` job in `ci.yml` runs lint+render for both variants, diffs them, and includes a D-02 negative case; it cannot pass vacuously (D-06) | ✓ VERIFIED | Extracted and ran the committed job's `run:` script verbatim end-to-end: `EXIT=0`, includes the anti-vacuity 7-ScaledObject gate (now reachable under `set -e` per WR-05 fix), the per-class isolation loop, an exact-rendered-value assertion (WR-02 fix), the anchored whole-chart-diff-scope assertion (WR-06 fix), and 4 negative cases (A/B basic D-02, C/D the post-CR-01 coercion-bypass regression cases) |

**Score:** 11/11 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `deploy/chart/octoconv/templates/_helpers.tpl` | `octoconv.kedaMinReplicaCount` + `octoconv.kedaFallbackReplicas` defines | ✓ VERIFIED | Both defines present (lines 126, 171); post-CR-01 raw-kind guard confirmed live; wired into all 7 templates |
| `deploy/chart/octoconv/values.yaml` | `minReplicaCount: 0` explicit on all 7 `keda.<class>` blocks | ✓ VERIFIED | `grep -c 'minReplicaCount: 0'` = 7; each commented per the chart's established rationale register |
| `deploy/chart/octoconv/templates/scaledobject-image.yaml` (and 6 siblings) | include-driven `minReplicaCount`/`fallback.replicas` | ✓ VERIFIED | All 7 files call both includes with their own class string; falsy-0 `hasKey`/`ne nil` guard on `scaleDownStabilizationSeconds` intact in the 5 files that carry it (document/audio/av/ocr/pandoc) |
| `deploy/chart/octoconv/values-capacity-floor.yaml` | example single-class override `keda.image.minReplicaCount: 2` | ✓ VERIFIED | Exists, exactly one non-comment key, documents invocation/layering/EXAMPLE status |
| `.github/workflows/ci.yml` | `helm` job — lint, render, isolation diff, negative validation | ✓ VERIFIED | `helm:` job present at line 16, before `gate:` at 227, no `needs:`; `permissions: contents: read`; `azure/setup-helm@v4` pinned with `version: v4.2.3`; script runs end-to-end exit 0 |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `scaledobject-*.yaml` (×7) | `octoconv.kedaMinReplicaCount` | `include (dict "class" "<class>" "root" $)` | ✓ WIRED | All 7 files, correct class string each |
| `scaledobject-*.yaml` (×7) | `octoconv.kedaFallbackReplicas` | `include` in `fallback.replicas` position | ✓ WIRED | All 7 files, correct class string each |
| `octoconv.kedaMinReplicaCount` | `.Values.keda.<class>.minReplicaCount` | `index .root.Values.keda .class` | ✓ WIRED | Confirmed live: per-class values correctly resolved and validated |
| `.github/workflows/ci.yml` | `values-capacity-floor.yaml` | `helm template -f` | ✓ WIRED | Present in job script, executed live, renders `minReplicaCount: 2`/`replicas: 2` on image only |
| `.github/workflows/ci.yml` | 7 ScaledObject templates | `--show-only templates/scaledobject-<class>.yaml` loop | ✓ WIRED | Loop present and executed for all 6 non-image classes plus explicit image check |

### Data-Flow Trace (Level 4)

Not applicable in the conventional sense (no application runtime state) — the "data flow" here is
Helm's own render pipeline, values → template → manifest. This was traced directly by rendering
with real and adversarial `--set`/`-f` inputs rather than reading source only:

| Input | Path | Rendered Output | Status |
|-------|------|-----------------|--------|
| Default `values.yaml` (`minReplicaCount: 0`) | `values.yaml` → helper → template | `minReplicaCount: 0`, `replicas: 1` | ✓ FLOWING |
| `-f values-capacity-floor.yaml` (`minReplicaCount: 2`) | overlay → helper → template | `minReplicaCount: 2`, `replicas: 2` | ✓ FLOWING |
| `--set keda.image.minReplicaCount=99` | `--set` → helper's `fail` guard | render aborted, error names class+both values | ✓ FLOWING (fails correctly) |
| `--set keda.image.minReplicaCount=2.9` (coercion-bypass class) | `--set` → helper's raw-kind guard (post CR-01 fix) | render aborted, error: "must be a whole number, got \"2.9\" (kind string)" | ✓ FLOWING (fails correctly — this is the exact path that was silently broken pre-fix) |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Default render is byte-identical to pre-phase baseline except comments (SC1) | `diff` of `f7eaea8` render vs `HEAD` render, both with `keda.enabled=true`/`prometheus.enabled=true` | 0 non-comment differing lines, 49 comment-only lines | ✓ PASS |
| `helm lint` clean, default + overlay | `helm lint deploy/chart/octoconv [...]` ×2 | both exit 0 | ✓ PASS |
| 7 ScaledObjects render at default | `grep -c '^kind: ScaledObject'` | 7 | ✓ PASS |
| Negative case A (floor > ceiling) | `--set keda.image.minReplicaCount=99` | rc=1, names both keys+values | ✓ PASS |
| Negative case B (negative floor) | `--set 'keda.ocr.minReplicaCount=-1'` | rc=1, names key+value | ✓ PASS |
| CR-01 coercion-bypass inputs (`2.9`/`99.9`/`-0.5`/`true`/`abc`/`""`) | 6× `--set keda.image.minReplicaCount=<value>` | all rc=1 with raw-kind `fail` message | ✓ PASS |
| Full committed CI `helm` job script, extracted verbatim via `ruby -ryaml` | `bash /tmp/vjob.sh` | `EXIT=0`, all internal OK-assertions print, including the 4 negative cases (A/B/C/D) | ✓ PASS |
| `go build ./...` unaffected (chart-only phase) | `go build ./...` | exit 0 | ✓ PASS |
| `internal/convert/exec.go` byte-for-byte unchanged (project invariant) | `git diff f7eaea8..HEAD -- internal/` | 0 lines | ✓ PASS |
| Three pre-existing overlays untouched (D-04) | `git diff f7eaea8..HEAD --stat -- values-local.yaml values-e2e.yaml values-loadproof.yaml` | empty | ✓ PASS |
| `ci.yml` diff is purely additive | `git diff f7eaea8..HEAD -- .github/workflows/ci.yml \| grep -c '^-[^-]'` | 0 | ✓ PASS |

### Probe Execution

No `scripts/*/tests/probe-*.sh` convention exists in this repo, and the phase declares its own
verification surface as the CI `helm` job script rather than a standalone probe script. That
script was extracted from the committed `ci.yml` (via `ruby -ryaml`, per the environment notes)
and executed directly — treated as the probe-equivalent for this phase, run under `bash`, from
the repository root. Result: exit 0 (see Behavioral Spot-Checks table above for the itemized
internal assertions).

| "Probe" | Command | Result | Status |
|---------|---------|--------|--------|
| `ci.yml` `helm` job's embedded script | `bash /tmp/vjob.sh` (extracted verbatim from `.github/workflows/ci.yml`'s `helm.steps[run]`) | exit 0, all internal `OK:` assertions printed, no `FAIL:` lines | PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|-------------|--------------|--------|----------|
| CAPA-01 | 54-01-PLAN.md, 54-02-PLAN.md | `minReplicaCount` настраивается для каждого класса движка вместо жёстко зашитого нуля (SEED-011) | ✓ SATISFIED (code) / ⚠ stale doc | All 7 classes now read `minReplicaCount` from Helm values via a validated helper; default preserved at `0`; live-verified override changes exactly one class. **However**, `.planning/REQUIREMENTS.md` line 42 still shows CAPA-01 as an unchecked `- [ ]` item, and the traceability table (line 97) still reads `CAPA-01 \| Phase 54 \| Pending` — neither was updated even though `git diff f7eaea8..HEAD --stat` shows `REQUIREMENTS.md` was not touched at all in this phase's commits. ROADMAP.md's own Phase 54 line IS marked `[x]` completed. This is a documentation-sync gap, not a functional gap — see note below. |

**No orphaned requirements**: REQUIREMENTS.md maps exactly one requirement (CAPA-01) to Phase 54, and both plans declare it in their `requirements:` frontmatter. Nothing is missing on the plan side.

**Note on the stale REQUIREMENTS.md checkbox:** This is flagged as a documentation-hygiene gap, not
a BLOCKER, because the underlying code fully and verifiably satisfies CAPA-01 (see Observable
Truths 1-9 above, all independently re-executed against `HEAD`). Recommend a trivial follow-up
commit checking the CAPA-01 box and updating the traceability table to `Complete`, matching the
pattern already used for ACCT-01…RETN-01 in the same file.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| — | — | No `TBD`/`FIXME`/`XXX`/`TODO`/`HACK` debt markers found in any of the 10 files this phase modified | — | None — clean |
| `.github/workflows/ci.yml` | 26 | `azure/setup-helm@v4` pinned to a mutable major tag, not a SHA (WR-03) | ℹ️ Info (accepted residual) | Explicit, recorded threat-model decision (T-54-03); consistent with this file's other 5 tag-pinned actions; compensated by job-level `permissions: contents: read` + pinned `version: v4.2.3` helm binary. See override entry in frontmatter. |
| `.github/workflows/ci.yml` | 1-8 | No workflow-level `permissions:` on the 4 pre-existing jobs (WR-04) | ℹ️ Info (accepted residual, out of phase scope) | Adding it would edit lines outside this phase's diff and break the "ci.yml change is purely additive" invariant this phase's own acceptance criteria assert. See override entry in frontmatter. |
| `.github/workflows/ci.yml` | 16-18 (helm job) | No `timeout-minutes` on the new job (IN-04, review-skipped, info) | ℹ️ Info | `docker-build`/`e2e` have explicit timeouts; `helm` job relies on the 6-hour default. Low risk (job runs in seconds). Not required by any must-have. |
| `deploy/chart/octoconv/values.yaml` | 189-190 | Hardcoded `minioadmin`/`minioadmin` defaults (IN-06, pre-existing, outside this phase's diff) | ℹ️ Info | Predates phase 54; noted by the code review as inconsistent with this chart's own "secrets never default in values.yaml" rule, but not introduced or touched by this phase. |

None of the above are BLOCKERs. CR-01 (the one CRITICAL finding) and WR-01/WR-02/WR-05/WR-06 (4 of 6 warnings) were fixed and independently re-verified live in this report. WR-03/WR-04 are documented, deliberate residuals with override entries above, matching the `important_context` guidance provided for this verification.

### Human Verification Required

None. This phase is explicitly scoped as chart-only with SC3 stating "no live cluster required" and
ROADMAP.md's own research note "skip live-gate for this phase." All must-haves are mechanically
verifiable via `helm lint`/`helm template`, and all were independently re-executed in this report
rather than trusted from SUMMARY.md text.

### Gaps Summary

No functional gaps. One documentation-hygiene item (REQUIREMENTS.md's CAPA-01 checkbox/traceability
row still reads "Pending" despite the phase being functionally complete and ROADMAP.md marking it
`[x]`) is noted above for a trivial follow-up — it does not block phase acceptance because it is a
bookkeeping artifact, not a codebase artifact the phase goal depends on.

The one CRITICAL code-review finding (CR-01: D-02 fail-fast guard bypassable via non-bare-integer
`--set` input) was found and fixed **after** the SUMMARYs were written; this report independently
re-executed the exact bypass inputs (`2.9`, `99.9`, `-0.5`, `true`, `abc`, `""`) against the current
`HEAD` and confirmed all now correctly abort the render, closing the gap the SUMMARYs did not know
about at the time they were authored.

---

*Verified: 2026-08-06T03:10:02Z*
*Verifier: Claude (gsd-verifier)*
