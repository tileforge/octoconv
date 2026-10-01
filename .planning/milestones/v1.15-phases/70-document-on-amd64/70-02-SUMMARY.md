---
phase: 70-document-on-amd64
plan: 02
subsystem: infra
tags: [landlock, sandbox, libreoffice, verapdf, sandbox-paths, conf05, measurement]

requires:
  - phase: 70-document-on-amd64
    provides: "native document image, DIVERGENCES: 0, route decision scoped-socket, soffice bind-unavoidable evidence"
provides:
  - "sandbox.Paths.Socket / sock: token / accessMakeSocket (MAKE_SOCK only), wired through ParseSpec, Apply and RunExecMode"
  - "document-worker enters sandbox helper mode as its first statement"
  - "scripts/document-sandbox-measure.sh: content-judged driver of both document engines through the production helper"
  - "measured, removal-narrowed soffice and veraPDF sets with conf05 blocks in SANDBOX-PATHS.md"
  - "the /tmp route decided by measurement: read-write (socket-only does not work)"
  - "conf05-crosscheck.sh classes soffice, verapdf, alias document, socket block kind"
affects: [70-03, 70-04, 70-05, internal/sandbox, internal/convert]

actuals:
  tokens: 17000
  tasks: 2
  commits: 2
plan_head_before: 3fb3ccea31b7a1ee767b869b7dc6b7226893b43d
plan_head_after: 90bfd88f4fa31f71290dacf1fb1faae7d4650aac

tech-stack:
  added: []
  patterns:
    - "A grant kind of one access right (sock: = MAKE_SOCK) in its own const, sharing no bit with the read-write or read-only masks"
    - "Removal-narrowed path sets judged by content, latency and JVM view, never by exit status"

key-files:
  created:
    - scripts/document-sandbox-measure.sh
    - .planning/phases/70-document-on-amd64/evidence/document-sandbox-measure.txt
  modified:
    - internal/sandbox/sandbox.go
    - internal/sandbox/sandbox_linux.go
    - internal/sandbox/execmode.go
    - internal/sandbox/spec_test.go
    - internal/sandbox/access_linux_test.go
    - cmd/document-worker/main.go
    - scripts/conf05-crosscheck.sh
    - scripts/cmd/sandbox-probe/main.go
    - scripts/sandbox-paths-measure.sh
    - .planning/research/SANDBOX-PATHS.md

key-decisions:
  - "/tmp route: read-write. sock:/tmp alone does not let soffice convert; it also needs rw:/tmp, so phase 69's concurrency 1, guard and sweep apply in 70-03/70-04"
  - "veraPDF's writable set is /dev/null only (job directory implicit); /dev/random is kept on a latency measurement"
  - "Neither engine is granted /usr/bin as a directory"

patterns-established:
  - "A check added after a removal run showed a silent regression (F1 fonts, V2 latency, V0 JVM ergonomics, per-run timeout)"

requirements-completed: []
requirements-advanced: [CONF-01, CONF-05, CONF-07]

duration: n/a
completed: 2026-09-30
status: complete
---

# Phase 70 Plan 02: socket right, measured document sets, /tmp route Summary

**The socket-only grant kind exists end to end and both document engines convert and validate under a ruleset read from SANDBOX-PATHS.md, judged by content, but MAKE_SOCK alone is not enough for soffice: its socket unlink needs REMOVE_FILE, so the measured /tmp route is read-write.**

## Measured results (what 70-03 and 70-04 depend on)

- **/tmp route: read-write.** `sock:/tmp` without `rw:/tmp`: every conversion fails (soffice binds, then its `unlinkat` of the socket gets EACCES because a MAKE_SOCK-only rule carries no REMOVE_FILE; the first `soffice.bin` exits 81, the second cannot rebind the stale path and exits 1). With `rw:/tmp` and `sock:/tmp` both: ALL PASS. Removing either one alone fails every conversion (`B-soffice-rw-drop-tmp`, `B-soffice-sock-drop-tmp`); C1 proves it. So `DOCUMENT_WORKER_CONCURRENCY` stays 1 and phase 69's guard and sweep apply (70-03 code, 70-04 configuration).
- **pipe residue: absent** on the chosen route (soffice unlinks its socket at exit; `TMP-RESIDUE` none on every read-write run). Present on the socket-only run (`OSL_PIPE_SingleOfficeIPC_<hash>` stays in /tmp). The socket name loses its uid segment when `/etc/passwd` is not granted (`OSL_PIPE_65534_SingleOfficeIPC_<hash>` with it, `OSL_PIPE_SingleOfficeIPC_<hash>` without), so the sweep must match `OSL_PIPE_*`.
- **SOFFICE-DENIAL: silent** (exit 0, no output file, by the plan's definition exit 0 is silent). Open Question 1 answered from the real image. The production converter turns exit-0-without-file into a terminal `produced no output file`, but 70-03's preflight must judge by absence of output carrying the marker, not by error type. Outside file never converted (N1 PASS).
- **veraPDF writes nowhere but the job directory and /dev/null.** With `JAVA_OPTS=-Djava.io.tmpdir=<job>/tmp -XX:-UsePerfData` the container's /tmp gains no entry (no hsperfdata_nobody). The image's own `hsperfdata_root` predates the run (JRE post-install at build time). N2: exit 7 with `Couldn't parse stream ... (Permission denied)` and no `isCompliant`; exit status is not evidence (7 is also the non-compliant verdict).
- **Preflight fixture part list:** `[Content_Types].xml`, `_rels/.rels`, `word/document.xml` (3 parts, 937 bytes, marker `octoconv-preflight-0123456789abcdef`). LibreOffice 7.4 converted it on the first try (marker in `content.xml`), its PDF/A export is `isCompliant="true"`; `word/_rels/document.xml.rels` was not needed.
- **preflight-shaped seconds: 6.0** (S1 1.3 + S5 1.1 + V1 1.4 + N1 1.0 + N2 1.2), 5.9-6.3 across runs, cold JVM included. 70-03 sizes its preflight timeout from this.
- **Final sets** (conf05 blocks in SANDBOX-PATHS.md):
  - soffice: readwrite `/tmp`; socket `/tmp`; readonly `/usr/lib`, `/usr/bin/dash`, `basename`, `dirname`, `ls`, `sed`, `/etc/libreoffice`, `/usr/share/fonts`, `/dev/urandom` (the engine `/usr/bin/soffice` is added separately).
  - veraPDF: readwrite `/dev/null`; readonly `/usr/lib`, `/usr/bin/dash`, `dirname`, `which`, `/opt/verapdf`, `/etc/java-17-openjdk`, `/proc`, `/sys/fs/cgroup`, `/dev/random` (the engine `/opt/verapdf/verapdf` is added separately).
  - Go var names for 70-03: `sofficeReadWrite`, `sofficeSocketDirs`, `sofficeReadOnly`, `sofficeEngines`, `verapdfReadWrite`, `verapdfReadOnly`, `verapdfEngines`.

## Accomplishments

- `internal/sandbox`: `Paths.Socket`, `sock:` token (after `rw:`, before `ro:`), `accessMakeSocket` = MAKE_SOCK alone; `accessReadWrite` and `accessReadOnly` byte-identical (diff against 5560b2a shows no change to that line), a `Paths` without sockets renders the same tokens as before, a socket-only spec still builds a ruleset. `TestNothingMayCreateSymlinksOrSpecialFiles` passes unchanged (run on linux in golang:1.26-bookworm, as the sandbox linux tests do not run on this darwin host); new `TestSocketGrantIsSocketOnly` and spec round-trip tests.
- `cmd/document-worker`: `sandbox.RunExecModeIfRequested(os.Args)` is the first statement of `main`; nothing else of 70-03's startup.
- `scripts/document-sandbox-measure.sh`: builds the image from this tree, refuses an emulated one, drives both engines through `document-worker __sandbox-exec`, reads the rulesets from the conf05 blocks, content-judged S1-S5, F1, V0, V1, V2, N1, N2, C1; ends `ALL PASS (12 checks)` on the recorded sets.
- `scripts/conf05-crosscheck.sh`: classes `soffice`, `verapdf`, alias `document`, socket block kind (missing block exits 2, empty block counts 0); the six existing classes' output is byte-identical (diffed against the previous script); the Go comparison exits 3 for both until 70-03.
- `SANDBOX-PATHS.md`: document section in Russian with sources, per-entry reasons, removal tables (both engines, wide set one entry at a time, and the final set one kept entry at a time), the socket right and route, TMP residue, the V0 table (without /proc the JVM reports the host: MaxHeapSize 4206886912 instead of 268435456, G1 instead of Serial), F1, N1/N2, the preflight fixture and cost.
- Evidence: `evidence/document-sandbox-measure.txt` holds 78 run blocks headed by their OVERRIDE lines, closed by the no-override ALL PASS run.

## Task Commits

1. **Task 1: socket right, helper mode, driver, candidate sets (tracer)** - `0b5cbe2` (feat)
2. **Task 2: narrowing by removal, /tmp route, preflight cost, evidence** - `90bfd88` (feat)

## Deviations from Plan

### Auto-fixed / measurement-driven

**1. [Rule 1 - Bug] soffice's wrapper needs `ls`, absent from the spike's M7 execve list**
- **Found during:** Task 2, narrowed baseline (`n0-baseline`: exit 127, `ls: Permission denied`, `oosplash: not found`).
- **Fix:** `/usr/bin/ls` kept (removal fails every conversion). The plan's own step listed `ls`; the spike list did not.

**2. [Rule 2 - Missing critical check] driver gained a per-run timeout, F1, V2 and a stronger V0**
- **Found during:** Task 2 removal matrix. (a) Removing `/etc/libreoffice` made soffice hang forever instead of failing (60 s timeout added; hang is a failed check). (b) Removing `/dev/random` left V1's verdict correct but made veraPDF 8 seconds slower (JVM SecureRandom seed fallback): a content-only check would have recorded the entry as removable, so V2 compares timing with an unconfined run. (c) The fixture text cannot see font substitution: F1 asserts Calibri, Cambria, Arial embed Carlito, Caladea, LiberationSans. (d) V0 compares the ergonomic `PrintFlagsFinal` values as well as the cgroup metrics.
- **Files modified:** scripts/document-sandbox-measure.sh. **Commit:** `90bfd88`.

**3. [Plan branch taken] the socket-only hypothesis failed, the plan's read-write branch applied**
- Task 1 step 6 / Task 2 step 3 anticipated this: with socket-only failing and `/tmp` read-write passing, the route is read-write. No grant beyond that documented branch was added; no further widening was needed (a second stop condition, "neither passes", was not reached).

**4. [Process] driver revised in Task 2's commit**
- `scripts/document-sandbox-measure.sh` is in Task 2's commit though the plan lists it under Task 1 (the additions in deviation 2 came from Task 2's removal runs).

**5. [Process] first matrix ran on an earlier driver**
- Matrix A (wide set, one entry removed) ran before V2 existed; V1 times in it carry the latency evidence, and matrix B plus the closing run used the final driver. Recorded in the evidence header.

**Total deviations:** 5 (1 bug, 1 missing check, 1 plan branch, 2 process). No scope creep.

## Auth gates

None.

## Decision for the user (not implemented)

`rw:/tmp` is what the measurement forces given a MAKE_SOCK-only `sock:` grant, and it exposes sibling job files under `/tmp` to read and write for the LibreOffice process (mitigated by phase 69's concurrency 1, guard and sweep). A narrower alternative was NOT measured and NOT built: a `sock:` grant that carries MAKE_SOCK plus REMOVE_FILE on `/tmp` (lets soffice unlink its own socket; would also let it delete sibling files but not read or write them). It changes the shared wire format's access semantics, so it is a decision for the user, not something to add on the executor's own authority.

## Known Stubs

None.

## Threat Flags

None beyond the plan's threat model (T-70-05..T-70-09): the `sock:` token is the new parser surface the plan already lists; unknown tokens still error, and the mitigation tests pass.

## Self-Check: PASSED

- Files exist: `scripts/document-sandbox-measure.sh`, `.planning/phases/70-document-on-amd64/evidence/document-sandbox-measure.txt`, `.planning/research/SANDBOX-PATHS.md` (conf05 blocks for soffice readwrite/socket/readonly/engines and verapdf readwrite/readonly/engines present).
- Commits `0b5cbe2` and `90bfd88` exist; `git rev-list --count 3fb3cce..HEAD` measured 2 before this SUMMARY commit.
- `git diff --exit-code internal/convert/exec.go` exits 0; `scripts/conf05-crosscheck.sh pandoc ocr av audio html probe` prints six MATCH lines; `scripts/document-sandbox-measure.sh` last line `ALL PASS (12 checks)`; `go test ./internal/sandbox/ ./internal/convert/ ./cmd/...` pass.
- STATE.md and ROADMAP.md untouched.

---
*Phase: 70-document-on-amd64*
*Completed: 2026-09-30*
