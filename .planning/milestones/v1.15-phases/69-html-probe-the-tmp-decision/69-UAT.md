---
status: complete
phase: 69-html-probe-the-tmp-decision
source: [69-VERIFICATION.md]
started: 2026-09-26T22:09:48Z
updated: 2026-09-29T00:10:00Z
---

## Current Test

[testing complete]

## Tests

### 1. Fourth class-closure inversion (chromium.go)
expected: TestConfinedClassesExecOnlyThroughTheSandbox fails naming chromium.go when a raw exec is restored; clean after revert — or the generic Phase 68 self-test is explicitly accepted
result: pass
evidence: "2026-09-27: chromium.go:187 c.exec(workDir).run -> runCommand; TestConfinedClassesExecOnlyThroughTheSandbox FAIL naming chromium.go:187 (direct runCommand) + non-vacuity (zero engineExec calls in chromium.go); reverted, git diff --exit-code internal/ clean, test ok (go1.26.4)"

### 2. CR-01 re-test after gap closure 69-04 (CSP spliced at document start)
expected: A <script> placed anywhere before the literal <head>/<html> (pre-tag script, <header> decoy, '<head>' in a comment, uppercase, BOM) does not execute in chromium-headless-shell: `scripts/sandbox-liveproof.sh html` reports all 18 live CSP differential rows PASS, and the 24 unit rows in chromium_test.go pass
result: pass
prior_result: issue (reported "чиним", blocker) — fixed by 69-04, gap G-69-2
decision: fix now via gap closure — CSP must precede any parser-executable content regardless of where the literal <head>/<html> sits; proof must be a live chromium-headless-shell differential test (script before <head> does not run), not a code-reading argument

### 3. WR-01 re-test after gap closure 69-05 (/dev/shm mount dropped)
expected: The chromium-worker chart has no dshm volume/mount, values.yaml has no chromiumWorker.shmSize, docker-compose.yml has no chromium-worker shm_size, the old D-10 comments point to SANDBOX-PATHS.md; helm template renders cleanly; `scripts/sandbox-liveproof.sh html` reports `PASS: html large-document-without-dev-shm`
result: pass
prior_result: issue (reported "убирай монтирование", major) — fixed by 69-05, gap G-69-3
decision: drop the /dev/shm emptyDir mount and chromiumWorker.shmSize value; replace the D-10 comment with a cross-reference to SANDBOX-PATHS.md; confirm with a large HTML document through the confined worker that no Landlock denial appears

## Summary

total: 3
passed: 3
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps

- gap_id: G-69-2
  resolved_by: 69-04-PLAN.md
  resolved_at: 2026-09-29
  truth: "The JS-disable CSP injected by injectCSPFirst governs every script in an uploaded HTML document, including a <script> placed before the literal <head>/<html> tag"
  status: resolved
  reason: "User reported: чиним (CR-01 from 69-REVIEW.md, decided fix-now)"
  severity: blocker
  test: 2
  root_cause: "injectCSPFirst picks the splice point by a tag-boundary-free byte search for <head then <html, assuming source order == HTML5 tree-construction order. LooksLikeHTML only gates the prefix, so a <script>, a <header> decoy, or '<head>' text inside a comment may precede the literal tag; chromium implicitly opens html/head, runs the script, and the CSP spliced relative to the discarded literal tag lands in body (chromium: 'meta outside head ... ignored') or inside a comment as inert text. Empirically confirmed on the production Chromium 153 binary with chromiumPrintArgs: 8/9 crafted shapes execute JS under current code; all 9 blocked when CSP is spliced right after a leading <!doctype> (offset 0 only) or at byte 0."
  artifacts:
    - path: "internal/convert/chromium.go"
      issue: "injectCSPFirst (99-113) splices after first literal <head/<html substring; bare prefix match also hits <header and comment text"
    - path: "internal/convert/chromium_test.go"
      issue: "regression tests (65-108) only cover scripts after the injection point"
    - path: "internal/convert/htmlsniff.go"
      issue: "LooksLikeHTML permits arbitrary content before literal <head>/<html> (not a bug; the attack surface)"
  missing:
    - "Splice the CSP unconditionally after a leading <!doctype ...> matched at offset 0 (after BOM/whitespace trim consistent with LooksLikeHTML), else at byte 0; no <head/<html search"
    - "Unit regression tests for all 9 shapes (pre-<html>/<head> script, <header> decoy both orders, '<head>' in comment both orders, uppercase, BOM, in-head control)"
    - "Live chromium differential test (script effect absent from PDF) wired into the existing html live gate, not a code-reading argument"
  debug_session: ".planning/debug/csp-bypass-before-head.md"

- gap_id: G-69-3
  resolved_by: 69-05-PLAN.md
  resolved_at: 2026-09-29
  truth: "The chromium-worker chart carries no /dev/shm mount that the phase-69 Landlock ruleset denies anyway, and its comments agree with SANDBOX-PATHS.md"
  status: resolved
  reason: "User reported: убирай монтирование (WR-01 from 69-REVIEW.md)"
  severity: major
  test: 3
  root_cause: "Four artifacts still encode the pre-Landlock Phase-15 belief that chromium touches /dev/shm even with --disable-dev-shm-usage; phase 69 measured the opposite but only the Go ruleset was updated. Live proof on the real image + production ruleset/argv via __sandbox-exec: a 105 MB HTML (18 page sections, large data-URI PNGs, 5000-row table) converts with exit 0 and zero Landlock/shm errors under Docker's default 64Mi /dev/shm; --shm-size=256m is byte-identical output except timestamps; negative control (flag removed) reproduces the /dev/shm EACCES failure, so the method would catch a real dependency. --disable-dev-shm-usage is unconditional in chromiumPrintArgs, so the unconfined HTML_SANDBOX_DISABLED escape hatch doesn't need shm either."
  artifacts:
    - path: "deploy/chart/octoconv/templates/deployment-chromium-worker.yaml"
      issue: "D-10 comment (9-12) + dshm volumeMounts/volumes (91-98)"
    - path: "deploy/chart/octoconv/values.yaml"
      issue: "chromiumWorker.shmSize (225), feeds only the dshm volume"
    - path: "docker-compose.yml"
      issue: "chromium-worker shm_size 256m (764-768) with the same stale comment"
    - path: "scripts/sandbox-liveproof.sh"
      issue: "html class --shm-size=256m extra docker-run arg (24,32) citing compose shm_size"
  missing:
    - "Drop the dshm mount/volume and chromiumWorker.shmSize; replace the D-10 comment with a cross-reference to .planning/research/SANDBOX-PATHS.md html section"
    - "Drop compose shm_size and the liveproof --shm-size=256m for the same measured reason (fix the class, not only the file the review named), with a one-line cross-reference"
    - "helm lint + helm template of the chart and every values-*.yaml overlay render cleanly; sandbox-liveproof.sh html passes without --shm-size"
  debug_session: .planning/debug/chromium-dev-shm-mount.md
