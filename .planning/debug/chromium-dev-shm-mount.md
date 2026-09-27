---
status: diagnosed
trigger: |
  UAT gap G-69-3 (goal: find_root_cause_only). truth: "The chromium-worker
  chart carries no /dev/shm mount that the phase-69 Landlock ruleset denies
  anyway, and its comments agree with SANDBOX-PATHS.md". User decision (UAT
  test 3, WR-01 in 69-REVIEW.md): drop the mount.
created: 2026-09-27T01:04:16Z
updated: 2026-09-27T01:35:00Z
---

## Current Focus
<!-- diagnose-only: investigation complete, no fix applied -->

hypothesis: CONFIRMED — see Resolution.root_cause
test: done
expecting: n/a
next_action: none — return ROOT CAUSE FOUND to caller; plan-phase --gaps applies the fix
bug_class: bohrbug (deterministic: stale doc comment + orphaned config value, not a runtime race)
reasoning_checkpoint: null  <!-- not a fix_and_verify session -->
tdd_checkpoint: null

## Symptoms

expected: |
  deployment-chromium-worker.yaml's D-10 comment and its /dev/shm mount agree
  with .planning/research/SANDBOX-PATHS.md's measurement that /dev/shm is not
  needed under the phase-69 Landlock ruleset.
actual: |
  The D-10 comment claims "even with --disable-dev-shm-usage some Chromium
  internal paths still touch /dev/shm" and mounts a 256Mi medium:Memory
  emptyDir at /dev/shm; SANDBOX-PATHS.md's html section states the opposite,
  measured by removal: "/dev/shm и /run в наборе не нужны — проверено
  снятием". htmlReadOnly/htmlReadWrite (internal/convert/htmlsandbox.go)
  grant neither /dev/shm nor /run. WR-01 (69-REVIEW.md) flagged the
  contradiction; user decided (UAT test 3): drop the mount.
errors: none (this is a documentation/config contradiction, not a crash)
reproduction: |
  grep -n "dev/shm\|shmSize" deploy/chart/octoconv/templates/deployment-chromium-worker.yaml
  deploy/chart/octoconv/templates/deployment-chromium-worker.yaml:9-12 (D-10 comment)
  vs .planning/research/SANDBOX-PATHS.md:198
started: |
  The contradiction has existed since Phase 24 (v1.6, D-10, chart authored
  pre-Landlock, citing a MEDIUM-confidence external Chromium bug report from
  Phase 15/v1.3 RESEARCH.md: issues.chromium.org/issues/40135361). Phase 69
  (2026-09-20/25) added Landlock confinement and directly MEASURED (strace +
  real ruleset) that /dev/shm is unused by this build/version, but nothing
  propagated that finding back into the chart, values.yaml, docker-compose.yml,
  or scripts/sandbox-liveproof.sh — all four still carry the pre-Landlock
  "256m/256Mi belt-and-suspenders" assumption. Code review (69-REVIEW.md
  WR-01) caught only the chart's copy of the contradiction.

## Eliminated

- hypothesis: "The small preflight fixture (a one-paragraph HTML doc) is too trivial to exercise whatever chromium code path the D-10 comment worries about, so the SANDBOX-PATHS.md finding might not generalize to production-scale documents."
  evidence: |
    Built octoconv-chromium-worker:debug-shm from the real Dockerfile.chromium-worker
    and invoked the REAL production ruleset (htmlReadOnly/htmlReadWrite from
    internal/convert/htmlsandbox.go, chromiumPrintArgs from internal/convert/chromium.go)
    via the real __sandbox-exec Landlock helper (internal/sandbox/execmode.go), against
    a synthetic 105,788,091-byte HTML fixture: 18 page-break sections, each embedding one
    of three large real PNGs (plasma noise 1600x1200, blurred shapes, radial gradient;
    ~12.7MB raw / ~17MB base64) as a data: URI, plus a 200-row and a 5000-row HTML table.
    Ran under Docker's DEFAULT /dev/shm (no --shm-size flag at all, i.e. ~64Mi tmpfs — the
    same "far below 256Mi" default the chart's own D-10 comment names as insufficient) with
    --cpus=2.0 --memory=2g (matching docker-compose.yml's chromium-worker resource limits).
    Result: exit 0, 32,913,965-byte valid PDF, ~15s wall, ZERO /dev/shm-related errors,
    ZERO Landlock denials of any kind (only the benign, already-documented
    "Failed to adjust OOM score of renderer: Permission denied" noise SANDBOX-PATHS.md
    already accounts for). Re-ran identically with --shm-size=256m (matching
    docker-compose.yml/sandbox-liveproof.sh): byte-identical output PDF except the
    embedded /CreationDate,/ModDate timestamp (4 bytes, confirmed by direct byte diff) —
    i.e. the /dev/shm mount size made NO functional difference. Confirmed the test method
    itself is sensitive: re-running the SAME large-doc setup with --disable-dev-shm-usage
    REMOVED from chromiumPrintArgs reproduces exactly the failure the D-10 comment fears —
    "Creating shared memory in /dev/shm/.org.chromium.Chromium.* failed: Permission denied
    (13)", process exits 133, no PDF written — proving a real /dev/shm dependency WOULD
    have been caught here if one existed.
  timestamp: 2026-09-27T01:30:00Z

## Evidence

- timestamp: 2026-09-27T00:50:00Z
  checked: deploy/chart/octoconv/templates/deployment-chromium-worker.yaml, deploy/chart/octoconv/values.yaml, docker-compose.yml, scripts/sandbox-liveproof.sh (grep -rniI "shm")
  found: |
    Four locations carry the pre-Landlock /dev/shm assumption:
    1. deployment-chromium-worker.yaml:9-12 (D-10 comment) + :91-98 (volumeMounts/dshm + volumes/dshm emptyDir medium:Memory sizeLimit .Values.chromiumWorker.shmSize)
    2. values.yaml:225 chromiumWorker.shmSize: "256Mi" (feeds #1 only — no other values-*.yaml overlay references it)
    3. docker-compose.yml:764-768 shm_size: "256m" on the chromium-worker service, with an identical "belt-and-suspenders...some Chromium internal paths still touch /dev/shm" comment
    4. scripts/sandbox-liveproof.sh:24,32 class_row's html row passes extra_run_args="--shm-size=256m" to EVERY docker run it makes for the html class, citing docker-compose.yml's shm_size in its own comment
  implication: WR-01/the UAT decision named only #1 (the chart) explicitly; #2 is #1's own value and must move together; #3 and #4 encode the identical stale rationale and were not in scope of the code review that produced WR-01, but are the same defect class.
- timestamp: 2026-09-27T00:52:00Z
  checked: .planning/milestones/v1.3-phases/15-html-pdf-chromium-engine/15-RESEARCH.md:233-234,378 and 15-SECURITY.md:50,74; .planning/milestones/v1.6-phases/24-helm-chart-core/24-CONTEXT.md:31, 24-02-PLAN.md:150
  found: |
    The "even with --disable-dev-shm-usage some paths still touch /dev/shm" claim traces
    to Phase 15 (v1.3, first chromium integration, pre-Landlock): a MEDIUM-confidence
    external citation (issues.chromium.org/issues/40135361) justified adding compose's
    shm_size as "belt-and-suspenders" alongside the CLI flag. Phase 24 (v1.6) copied the
    same rationale into the Helm chart as D-10. Phase 69 (2026-09-20) then directly
    MEASURED this exact image/binary under strace + a real Landlock ruleset and found the
    opposite for the shipped chromium-headless-shell build, but did not revisit #1-#4.
  implication: The chart comment is not fabricated, but it is SUPERSEDED — a general,
    medium-confidence, version-unspecified bug report predates and is now overridden by
    this project's own direct measurement of the exact deployed binary.
- timestamp: 2026-09-27T01:15:00Z
  checked: |
    Live run of the actual html ruleset (internal/convert/htmlsandbox.go's htmlReadOnly/
    htmlReadWrite, internal/convert/chromium.go's chromiumPrintArgs) via the real
    __sandbox-exec Landlock helper, against a 124-byte small fixture and a 105,788,091-byte
    large fixture (18 pages, embedded PNGs, two HTML tables), each run twice (default ~64Mi
    docker /dev/shm, and --shm-size=256m matching compose/sandbox-liveproof.sh), plus one
    negative control run with --disable-dev-shm-usage removed.
  found: |
    Small fixture: exit 0, 9,208-byte real PDF (not the 652-byte blank-denial signature
    SANDBOX-PATHS.md documents), no /dev/shm errors.
    Large fixture, both shm sizes: exit 0, 15s wall, 32,913,965-byte real PDF, output
    byte-identical between the two shm sizes except a 4-byte /CreationDate,/ModDate
    timestamp field. No Landlock denial of any kind; only the pre-documented benign
    "Failed to adjust OOM score of renderer: Permission denied" (already covered by
    SANDBOX-PATHS.md's html section as a /proc write, expected and harmless).
    Negative control (flag removed): exit 133, "Creating shared memory in
    /dev/shm/.org.chromium.Chromium.* failed: Permission denied (13)" x2, no PDF written.
  implication: |
    Confirms empirically, beyond the small preflight fixture, that (a) chromium never
    touches /dev/shm under --disable-dev-shm-usage even for a large multi-page/image
    document, (b) the size (or presence) of any /dev/shm mount is functionally inert once
    Landlock is applied (the ruleset denies /dev/shm regardless of the underlying mount),
    and (c) the test method is sensitive enough to have caught a real dependency had one
    existed. SANDBOX-PATHS.md's finding generalizes; dropping the mount is safe.
- timestamp: 2026-09-27T01:40:00Z
  checked: |
    Scratch copy of deploy/chart/octoconv (NOT the repo) with the D-10 comment replaced by
    a cross-reference to SANDBOX-PATHS.md, the volumeMounts/dshm + volumes/dshm block
    deleted from deployment-chromium-worker.yaml, and chromiumWorker.shmSize deleted from
    values.yaml; ran `helm lint` and `helm template` (helm v4.3.0) against both the
    original and the patched copy, with values-local.yaml (secrets) and every other
    values-*.yaml overlay (capacity-floor, e2e, inspect-gate, loadproof, multireplica).
  found: |
    `helm lint` passes on both (0 chart(s) failed, identical single Chart.yaml icon INFO
    on both). `helm template` renders cleanly on both; the ONLY diff in the entire
    rendered manifest is the removed volumeMounts/dshm + volumes/dshm block (7 lines) —
    no dangling references, no other template touches .Values.chromiumWorker.shmSize.
    Every values-*.yaml overlay templates cleanly (exit 0) against the patched chart too.
  implication: The chart renders correctly with the mount and value removed; no other
    template or values file depends on chromiumWorker.shmSize.
- timestamp: 2026-09-27T04:05:00Z
  checked: |
    Correction to this session's own root-cause text below: "A Kubernetes pod without
    the chart's mount gets the runtime default of 64 MiB" repeats the diagnosis
    session's assumption without re-measuring it on THIS host. Planning-time
    measurement (69-05-PLAN.md): `/proc/mounts` inside a container on this Docker
    (OrbStack), no flag at all, reads `shm /dev/shm tmpfs ... size=8212480k` -- about
    7.8 GiB, not ~64Mi. Re-ran 69-05-PLAN.md Task 1
    (TestChromiumLargeDocumentNeedsNoDevShmLive, internal/convert/chromium_live_test.go)
    at the deployed limits (--cpus=2 --memory=2g, scripts/sandbox-liveproof.sh html):
    an 84,543,422-byte HTML fixture (18 page-break sections, incompressible noise
    PNGs, a 5000-row table) converts through the confined production
    ChromiumConverter.Convert to a 20,460,780-byte PDF (/Title marker present) in
    5.94s wall; a direct engine run on the rendered output, same argv, exits 0 with no
    "/dev/shm" in stderr; the negative control (the same run with
    --disable-dev-shm-usage removed) fails in the engine with stderr naming
    /dev/shm, and its output carries no marker.
  found: |
    The actual /dev/shm size this host's containers get by default is NOT the ~64Mi
    the diagnosis assumed -- it is whatever the container runtime happens to default
    to (7.8 GiB here). The conclusion this session reached ("dropping the mount is
    safe") does not depend on that size at all: the html Landlock ruleset
    (internal/convert/htmlsandbox.go) grants no /dev/shm at any size, so the negative
    control failing on /dev/shm (not on an exhausted tmpfs) is what the conclusion
    actually rests on, both for the small fixture measured here and for the large one
    measured in 69-05-PLAN.md.
  implication: |
    fix_applied: deploy/chart/octoconv/templates/deployment-chromium-worker.yaml's
    /dev/shm volume/volumeMount removed, its D-10 comment replaced by a cross-reference
    to SANDBOX-PATHS.md; deploy/chart/octoconv/values.yaml's chromiumWorker.shmSize
    removed; docker-compose.yml's chromium-worker shm_size removed (same
    cross-reference); scripts/sandbox-liveproof.sh's html class dropped its
    --shm-size docker-run flag and gained the "html large-document-without-dev-shm"
    check. G-69-3 closed by 69-05-PLAN.md.

## Resolution

root_cause: |
  Phase 69 added Landlock confinement to the html engine class and directly MEASURED
  (strace + a real ruleset, SANDBOX-PATHS.md) that /dev/shm is not needed — the ruleset
  in internal/convert/htmlsandbox.go's htmlReadOnly/htmlReadWrite correctly omits it, and
  --disable-dev-shm-usage (internal/convert/chromium.go's chromiumPrintArgs) already
  routes chromium's shared memory to /tmp instead. But four PRE-EXISTING artifacts that
  encode the opposite, PRE-Landlock belief (a Phase-15/v1.3 medium-confidence external bug
  report, "even with --disable-dev-shm-usage some paths still touch /dev/shm") were never
  reconciled against that measurement: the chart's D-10 comment + emptyDir mount
  (deployment-chromium-worker.yaml), its feeding value (values.yaml
  chromiumWorker.shmSize), docker-compose.yml's shm_size on the chromium-worker service,
  and scripts/sandbox-liveproof.sh's --shm-size=256m extra run arg for the html class.
  Code review (69-REVIEW.md WR-01) caught only the chart's copy of the contradiction, and
  the UAT decision (this gap, G-69-3) scoped the fix to that one file. Empirically
  reconfirmed here (small AND a 105MB/18-page/image-heavy large fixture, under the real
  Landlock ruleset, via the real __sandbox-exec helper): zero /dev/shm touches, zero
  Landlock denials, and the /dev/shm mount's presence/size makes no functional difference
  to output — a negative control (removing --disable-dev-shm-usage) proves the test method
  would have caught a real dependency had one existed. Dropping the mount is safe; the
  chart renders cleanly (helm lint + helm template, scratch copy) once it and its value
  are removed.
fix: |
  NOT APPLIED (find_root_cause_only mode). Suggested for plan-phase --gaps:
  1. deploy/chart/octoconv/templates/deployment-chromium-worker.yaml: delete the
     volumeMounts (dshm) and volumes (dshm emptyDir) blocks; replace the D-10 comment with
     a cross-reference to SANDBOX-PATHS.md's html section (verbatim reconciliation text
     drafted in the scratch-copy patch this session applied, not committed).
  2. deploy/chart/octoconv/values.yaml: delete chromiumWorker.shmSize.
  3. docker-compose.yml: same measured basis applies (same binary/version, same
     --disable-dev-shm-usage flag) — recommend dropping shm_size on the chromium-worker
     service and its matching comment too, though this was not explicitly named by WR-01
     or the UAT decision text; flag for the fix-planning phase to decide scope (drop vs.
     leave with an updated comment noting it is now believed unnecessary under Landlock
     but harmless if kept for the HTML_SANDBOX_DISABLED=true escape-hatch/unconfined path).
  4. scripts/sandbox-liveproof.sh: its html class_row's "--shm-size=256m" extra_run_args
     (and the comment citing docker-compose.yml) becomes stale once #1/#3 are resolved —
     once the chart mount is gone, a real k8s pod gets whatever the container runtime's
     own default /dev/shm is (NOT 256Mi), so the live-proof script arguably should drop
     this flag too to match deployed reality, or keep it deliberately as a stricter-than-
     production headroom check. Recommend deciding this alongside #3 for consistency.
verification: |
  Empirical, not yet a committed automated test: live docker run of the exact production
  ruleset/argv against small + 105MB large HTML fixtures, both docker-default and
  --shm-size=256m, all pairs producing zero Landlock denials and functionally identical
  output (see Evidence). Negative control (flag removed) reproduces the real /dev/shm
  denial, validating the test's sensitivity. helm lint/template on a scratch chart copy
  with the mount removed renders cleanly with no other diff. No regression test written
  (out of scope for find_root_cause_only); plan-phase --gaps should consider whether
  scripts/sandbox-liveproof.sh's existing "html sweeps-shared-tmp" style live checks are
  the right place to pin "no /dev/shm denial on a large document" going forward, since the
  Go test suite has no fixture-size-dependent chromium test today.
oracle_type: derived  <!-- verified against the documented Landlock grant model + the shipped --disable-dev-shm-usage contract, not a spec literally stating this -->
files_changed: []  <!-- diagnose-only; git diff of tracked files intentionally left clean -->
