---
phase: 70-document-on-amd64
plan: 08
subsystem: infra
tags: [sandbox, landlock, proc, verapdf, jvm, environ, gap-closure, wr-02]
status: complete

requires:
  - phase: 70-document-on-amd64
    provides: "the measured veraPDF set and V0 (70-02), the document gate (70-03/70-04), 70-REVIEW.md WR-02, 70-07's shared-/tmp guard"
provides:
  - "OUTCOME: NARROWED. veraPDF's ro:/proc is replaced by /proc/cgroups, /proc/self/cgroup and /proc/self/mountinfo; each fails V0 and V0L when removed, the other six /proc entries the JVM opens do not"
  - "Two driver checks: V0L (the JVM's view through the production launcher) and N3 (other processes' environ, with a canary proven readable unconfined first; PROC-OTHER and CONTROL rows)"
  - "The review's WR-02 claim, measured: a confined engine could NOT open /proc/1/environ or the parent's environ even under ro:/proc whole; it COULD open the same processes' cmdline and status. The narrowed grant denies those too"
  - "LAUNCHER: exec -- the veraPDF launcher execs java in place, so a /proc/self entry resolved in the helper's pid covers the JVM"
  - "Engine-environment comments that say the scrub closes the engine's OWN /proc/self/environ and cite the measured N3 result"
  - "html's N3 row (same pattern: environ denied, cmdline/status open) and todo 2026-09-30-proc-grant-other-process-entries.md"
affects: [70-09, scripts/document-sandbox-measure.sh, SANDBOX-PATHS.md]

requirements-completed: [CONF-01, CONF-05]

actuals:
  tokens: 9800
  tasks: 2
  commits: 2
plan_head_before: 3a34778e3a9b5acc1667a11d6d47928dd0540640
plan_head_after: 5018dca3a3d179dbb85431358f0fc1c617cc0249

tech-stack:
  added: []
  patterns:
    - "A /proc/self grant is resolved by the helper in its own pid at Apply time; it covers an engine only when every hop to the engine is an exec (here: helper -> dash launcher -> java), which V0L checks and V0 cannot"
    - "A denial probe is valid only with a control in the same container shape that proves the target readable without the ruleset (canary in PID 1's and the parent's environment)"

key-files:
  created:
    - .planning/phases/70-document-on-amd64/evidence/verapdf-proc-measure-70-08.txt
    - .planning/todos/pending/2026-09-30-proc-grant-other-process-entries.md
  modified:
    - scripts/document-sandbox-measure.sh
    - internal/convert/documentsandbox.go
    - internal/convert/documentsandbox_test.go
    - internal/convert/sandboxwrap.go
    - .planning/research/SANDBOX-PATHS.md

key-decisions:
  - "Narrowed by removal, judged by V0 and V0L, never by V1 alone: three /proc entries kept, six removed, candidates and every removal run are in the evidence file"
  - "/proc/cgroups is kept although production heap/GC sizing holds without it: without it the JVM's Operating System Metrics (-XshowSettings:system, OperatingSystemMXBean) stop matching the container, and the plan's criterion is an identical view"
  - "html and tectonic are not narrowed (prohibition, user decision 3); html's position is one measured probe plus the todo"

patterns-established:
  - "Comments about a confinement mechanism say 'inferred' where only the outcome was measured (the denial of other processes' environ is measured; Landlock's ptrace check as its cause is not)"

requirements_completed: [CONF-01, CONF-05]
---

# Phase 70 Plan 08: Narrow veraPDF's /proc (WR-02) Summary

**Outcome: narrowed.** veraPDF's `ro:/proc` is now `/proc/cgroups`, `/proc/self/cgroup` and `/proc/self/mountinfo`; the JVM's container view (V0 and V0L) is unchanged, and the confined engine can no longer open other processes' `cmdline`, `status` or `environ`.

## What the measurement showed

**The review's claim was wrong as stated.** WR-02 said a same-uid confined JVM reads the worker's environ. N3's baseline on the unchanged grant (`ro:/proc` whole), with a canary proven readable unconfined in both PID 1 and the helper's parent (quoted from `evidence/verapdf-proc-measure-70-08.txt`):

```
CANARY-PRESENT /proc/1/environ
CANARY-PRESENT /proc/6/environ
DENIED /proc/1/environ
DENIED /proc/6/environ
DENIED /proc/self/root/etc/hostname
PROC-OTHER: /proc/1/cmdline opened
PROC-OTHER: /proc/6/cmdline opened
PROC-OTHER: /proc/6/status opened
CONTROL: /proc/self/environ opened
CONTROL: /proc/self/cmdline opened
```

Other processes' environ was denied even under a whole-/proc grant; their cmdline and status opened. The filesystem grant is not what denies environ (cmdline of the same process opens), so the denial is at the open of environ itself. Landlock's ptrace check is the inferred cause; only the denial is measured, and the comments say so. What the old grant did leak to the engine was the argv of the worker and PID 1. That is gone after the narrowing (PROC-OTHER all denied).

**Launcher.** `/opt/verapdf/verapdf` ends with `exec "$JAVACMD" $JAVA_OPTS ...`. strace shows two `execve` calls (the script, then `/usr/bin/java`) in one pid, the JVM's other trace files being threads of it: `LAUNCHER: exec`. So a `/proc/self/...` entry resolved by the helper in its own pid covers the JVM. That removes the 70-02 argument ("the path depends on the pid, it cannot be granted statically").

**Narrowing.** Candidate = every `/proc` entry the JVM opens (`cgroups`, `cpuinfo`, `meminfo`, `stat`, `net/if_inet6`, `sys/vm/overcommit_memory`, `self/cgroup`, `self/mountinfo`, `self/coredump_filter`), other entries of `verapdfReadOnly` unchanged, each candidate a full 14-check driver run:

- The trace without `-XshowSettings` did not show `/proc/cgroups`; the first candidate without it failed V0 and V0L (Operating System Metrics: "No metrics available for this platform"; heap 256 MiB and Serial unchanged). The trace with `-XshowSettings` named it, and candB (with it) passed.
- Removal one by one from candB: `cpuinfo`, `meminfo`, `stat`, `net/if_inet6`, `sys/vm/overcommit_memory`, `self/coredump_filter` each ALL PASS (removed). `cgroups`, `self/cgroup`, `self/mountinfo` each fail V0 and V0L (kept).
- candC (the three kept, six removed at once): ALL PASS, V0 and V0L 26 identical lines.

The final driver run (no override, repo blocks): `ALL PASS (14 checks)`; V0, V0L and N3 PASS; V1 isCompliant; V2 1.5 s vs 1.6 s. `scripts/conf05-crosscheck.sh document html` prints MATCH for soffice, verapdf and html. `scripts/sandbox-liveproof.sh document` ends `ALL PASS (7 checks)` (the production startup preflight runs the narrowed grant through the Go path). `go test -count=1 ./internal/convert/` passes; `internal/convert/exec.go` is byte-identical to 7744ddf.

## html and tectonic

- **html** (one probe, not narrowed): same pattern as veraPDF's baseline. Environ of PID 1 and the parent and `/proc/self/root/etc/hostname` denied; `cmdline` and `status` of other processes opened; the engine's own environ/cmdline opened. **No environ row opened** for html, so the todo is severity medium. Narrowing is not the same shape there: chromium re-executes itself for its child processes, so a `/proc/self` entry resolved in the helper's pid covers none of them; what chromium reads under `/proc` was not traced (user decision 3).
- **tectonic**: its ruleset grants no `/proc` at all (`tectonicSystemReadOnly`), so the review's "same exposure in tectonic" does not hold.
- Todo: `.planning/todos/pending/2026-09-30-proc-grant-other-process-entries.md`.

## Accomplishments

- Driver: V0L and N3 added, header updated; count 12 -> 14.
- Grant, conf05 block and pinned test equal, in order (eleven entries); document section (final-set table, removal table, V0 paragraph, new "/proc и другие процессы" subsection), "Что уже внедрено" and html's /proc paragraph updated in Russian.
- `engineEnv`, `engineExec.env`, `sofficeEnv`, `verapdfEnv` no longer claim more than the scrub closes. Rewritten `engineEnv` sentence: "What the scrub closes is the ENGINE's OWN /proc/self/environ: that file is the environment the engine was started with, so handing the worker's on would hand over DATABASE_URL and the S3 keys ... It does not decide whether a confined engine can open OTHER processes' /proc/<pid>/environ ... That depends on the class's /proc grant and was measured, not assumed (N3 ...)".

## Task Commits

1. **Task 1 (tracer): measure and narrow** - `f84f345` (fix). The tracer's `<verify>` was re-run end to end after the commit (driver ALL PASS 14, V0/V0L/N3 PASS, cross-check MATCH, convert tests, exec.go diff, liveproof ALL PASS 7): passed.
2. **Task 2: comments, record, todo** - `5018dca` (docs).

## Deviations from Plan

None in substance. Two execution notes:

- **[Note] CONTROL rows added to N3.** Not in the plan: `/proc/self/environ` and `/proc/self/cmdline` are reported (unjudged) so the baseline can show the denial is not the grant. The baseline was re-run with them; the first baseline (without) gave the same rows.
- **[Note] Commit mechanics.** The first Task 1 commit missed the force-tracked evidence file (`.planning` is ignored); it was added with `git add -f` and the commit amended before anything else depended on it.

**Total deviations:** 0 auto-fixed. **Impact:** none.

## Known Stubs

None.

## Threat Flags

None: no new endpoint, auth path or trust-boundary file access. The grant got narrower.

## Issues Encountered

- `/proc/cgroups` cannot be removed while V0 is the criterion, although production heap/GC sizing survives without it (candA). Recorded in SANDBOX-PATHS.md as an honest caveat.
- The mechanism behind the environ denial (Landlock ptrace) is inferred; it is a kernel property, so the todo asks to re-run N3 on the deployment kernel.

## Next

Ready for the phase's remaining plans / verification. STATE.md and ROADMAP.md were not touched (orchestrator's).

## Self-Check: PASSED

- Files exist: evidence file, todo, SANDBOX-PATHS.md, both Go files, driver script, this SUMMARY.
- Commits `f84f345` and `5018dca` exist; `git rev-list --count 3a34778..HEAD` = 2 before this SUMMARY.
- Acceptance: `grep -cx '/proc'` in the conf05:verapdf:readonly block prints 0; evidence has BASELINE, one `LAUNCHER: exec` line, ends with the full driver run; `go test -run TestVerapdf` passes; `grep -c 'SANDBOX-PATHS.md' sandboxwrap.go` = 1; `grep -c tectonicSystemReadOnly SANDBOX-PATHS.md` >= 1; todo frontmatter `created: 2026-09-30`, `severity: medium` consistent with no opened environ row.
- Scratch images `octoconv-document-worker:scratch70-08` and `octoconv-chromium-worker:scratch70-08` removed; no octoconv containers running; `dockerdev-*` untouched.
