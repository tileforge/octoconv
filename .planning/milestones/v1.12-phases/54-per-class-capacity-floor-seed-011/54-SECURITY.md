---
phase: 54
slug: per-class-capacity-floor-seed-011
status: secured
threats_total: 4
threats_closed: 4
threats_open: 0
asvs_level: default
register_authored_at_plan_time: true
created: 2026-08-06
---

# Phase 54 (per-class-capacity-floor-seed-011) — Security Audit

**Audited:** 2026-08-06
**Auditor:** Claude (gsd-security-auditor)
**ASVS Level:** default (not specified in `<config>`)
**Threats Closed:** 4/4
**Status:** SECURED

All verification below was performed by executing `helm v4.2.3` and `gh api` directly against
`HEAD` — not by trusting 54-01-SUMMARY.md / 54-02-SUMMARY.md / 54-REVIEW.md / 54-VERIFICATION.md
prose. Those documents were used only to identify *what* to re-check (notably CR-01, the
post-review fix to T-54-01/T-54-02's mitigation).

## Threat Verification

| Threat ID | Category | Disposition | Status | Evidence |
|-----------|----------|-------------|--------|----------|
| T-54-01 | Denial of Service | mitigate | CLOSED | `deploy/chart/octoconv/templates/_helpers.tpl:126-156` — `octoconv.kedaMinReplicaCount` validates `kindOf` on the **raw** value before any `int` coercion (fixed post-review, commit `adecb1f`), then rejects negative and above-ceiling floors. Independently re-executed (not trusted from docs) with `helm template` against `--set keda.image.minReplicaCount=` = `99.9`, `-0.5`, `2.9`, `2.0`, `abc`, `true`, `""`, `null`, `010`, and a 24-digit integer: **all 10 rejected** with `must be a whole number` (rc=1). Bare-integer boundary cases also verified live: `=99` (ceiling 4) → rejected naming both `keda.image.minReplicaCount`/`keda.image.maxReplicaCount`; `keda.ocr.minReplicaCount=-1` → rejected naming the key; `=2` → legally renders `minReplicaCount: 2`. `maxReplicaCount=null` also correctly rejected as its own misconfiguration. |
| T-54-02 | Tampering | mitigate | CLOSED | `deploy/chart/octoconv/templates/_helpers.tpl:171-173` — `octoconv.kedaFallbackReplicas` computes `max 1 (int (include "octoconv.kedaMinReplicaCount" .))`, i.e. derives from an `include` of the guarded helper rather than re-reading `.Values` directly, so T-54-01's validation fires from this call site too. Verified at all 7 call sites: `grep -c "dict \"class\"" <scaledobject-file>` = 2 for every one of image/document/html/audio/av/ocr/pandoc (one `kedaMinReplicaCount` include, one `kedaFallbackReplicas` include). Live-rendered: default `replicas: 1`, overlay (`minReplicaCount: 2`) → `replicas: 2`. |
| T-54-03 | Tampering | mitigate | CLOSED | `.github/workflows/ci.yml:16-32` — the new `helm` job declares `permissions: {contents: read}` (verified: job body contains no `${{ secrets.* }}` GitHub Actions secrets-context reference anywhere, only literal placeholder `--set secrets.x=x` chart values, unrelated to the CI token). `azure/setup-helm@v4` is pinned to a major tag matching the file's 5 other tag-pinned third-party actions; `version: v4.2.3` pins the installed helm binary independently. Vendor-repository check via `gh api repos/Azure/setup-helm`: resolves to the official `Azure/setup-helm` org repo ("Github Action for installing Helm", MIT-licensed, `Azure` org), and `gh api repos/Azure/setup-helm/git/refs/tags` confirms `refs/tags/v4` exists and resolves to a real tag object. WR-03's SHA-pinning suggestion was deliberately not applied — see "Assessed, Not Adopted" below. |
| T-54-04 | Denial of Service | accept | CLOSED | `deploy/chart/octoconv/values-capacity-floor.yaml:1-8` — header comment marks the file `EXAMPLE ONLY, this is NOT a production values file to be applied unreviewed` and states plainly that applying it `permanently pins 2 worker pods, at permanent infrastructure cost`. Value (`keda.image.minReplicaCount: 2`) sits inside `maxReplicaCount: 4`, a ceiling independently bounded by the T-54-01 guard. Entered into this document's Accepted Risks Log below, closing the disposition per the audit's own verification rule (accept → log entry present = CLOSED). |

## Accepted Risks Log

| Threat ID | Risk | Why Accepted | Compensating Control |
|-----------|------|---------------|----------------------|
| T-54-04 | An operator applies `values-capacity-floor.yaml` to a real cluster without reading its header, permanently pinning 2 `worker` pods at ongoing infra cost. | Guarding an operator against a values file they explicitly pass to `-f` is not something the chart can or should do; low severity because the pinned value (`2`) sits inside `maxReplicaCount: 4`, a ceiling autoscaling could already reach and that T-54-01's `fail` guard independently bounds. | Header comment (`values-capacity-floor.yaml:1-8`) explicitly marks the file EXAMPLE-only and states the cost/permanence trade-off before the single override key. |

## Unregistered Flags

None. Neither `54-01-SUMMARY.md` nor `54-02-SUMMARY.md` contains a `## Threat Flags` section, so
no executor-flagged new attack surface exists to reconcile against the register.

Independently, the code review (`54-REVIEW.md`) surfaced six warnings and six info items beyond
the four registered threats. These are **not** treated as unregistered attack-surface flags
because they are either (a) already mapped to a registered threat's disposition discussion, or
(b) pre-existing and outside this phase's diff:

- WR-01/WR-02/WR-05/WR-06 — CI-job/doc-comment correctness bugs in the T-54-01/T-54-02
  verification surface itself (not new attack surface); all four were fixed post-review
  (commits `adecb1f`, `b64a12d`, `3a050da`, `fc101ae`, `f53371f`) and independently
  re-verified live in this audit (see T-54-01/T-54-02 evidence above).
- WR-03 (SHA-pin `azure/setup-helm`) and WR-04 (workflow-level `permissions:` on the 4
  pre-existing jobs) — both deliberately skipped with recorded rationale; assessed below.
- IN-01 … IN-06 — INFO severity, no live bug remaining after CR-01's fix; IN-06 (hardcoded
  MinIO `minioadmin`/`minioadmin` default in `values.yaml:189-190`) predates this phase and is
  outside its diff (`git diff f7eaea8..HEAD -- deploy/chart/octoconv/values.yaml` touches only
  the `keda:` block) — flagged here for visibility, not as a phase-54 gap.

## Assessed, Not Adopted

**WR-03 (SHA-pin `azure/setup-helm`), per the critical_context instruction to assess on its
merits rather than treat "differs from a reviewer suggestion" as automatically open:**

Agreed with the phase's disposition. The workflow's five pre-existing third-party actions
(`actions/checkout@v7`, `actions/setup-go@v6`, `docker/setup-buildx-action@v4`,
`docker/bake-action@v7`, `actions/upload-artifact@v7`) are all tag-pinned, not SHA-pinned.
SHA-pinning only the newest action while five siblings float on tags does not reduce the
workflow's actual supply-chain exposure — a compromised publisher account could still repoint
any of the other five tags, and this job's blast radius is already the smallest in the file
(`permissions: contents: read`, no secrets, no cluster/build step). Making one job's pin stricter
than its neighbors without a repo-wide policy change is inconsistent rather than protective, and
does not by itself close a real gap. Residual risk is the same class of risk already accepted for
the other five actions — this is a repo-wide hardening decision, not a T-54-03-specific gap.
T-54-03's mitigation is otherwise fully verified (least-privilege `permissions:`, no secrets
referenced, pinned helm binary version, vendor identity confirmed live).

**WR-04 (workflow-level `permissions:` on `gate`/`race`/`docker-build`/`e2e`):** these four jobs
predate phase 54 and are outside its diff (`git diff f7eaea8..HEAD -- .github/workflows/ci.yml |
grep -c '^-[^-]'` = `0`, purely additive). Not a phase-54 regression; the new `helm` job itself is
correctly least-privilege. Worth a follow-up hardening PR, not a blocker here.

## Notes for Future Audits

- The mitigation for T-54-01/T-54-02 was genuinely broken as first implemented (CR-01: sprig's
  `int` silently coerced unparseable `--set` input to `0`, discarding the floor and rendering
  clean with a bypassed ceiling guard). It was caught by code review, not by this audit process,
  because the SUMMARYs were authored before the review ran. This audit re-verified the *current*
  `_helpers.tpl` state directly rather than accepting the SUMMARY's original (now-amended) claim —
  confirming CR-01 is fixed and the guard is not bypassable by any of the ten hostile inputs
  tested live in this session.
- `helm lint` is **not** part of either mitigation's verification surface: helm v4.2.3 downgrades
  a template `fail()` to an INFO log line during lint and still exits 0. All D-02 coverage comes
  from `helm template`/`helm install`/`helm upgrade`. This is correctly documented in both
  `_helpers.tpl`'s doc comment and `ci.yml`'s job comments (WR-01 fix).

SECURITY.md: `.planning/phases/54-per-class-capacity-floor-seed-011/54-SECURITY.md`
