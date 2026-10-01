---
phase: 70-document-on-amd64
plan: 01
subsystem: infra
tags: [docker, verapdf, libreoffice, landlock, multi-arch, differential]

requires:
  - phase: 69-sandbox-document-class
    provides: "document worker class, Landlock ruleset semantics, phase 69 concurrency-1 / guard / sweep fallback"
provides:
  - "document image built natively (only the verapdf stage is pinned to linux/amd64)"
  - "scripts/verapdf-differential.sh: repeatable native-vs-emulated veraPDF differential with extractor self-check and non-vacuity"
  - "measured soffice single-instance socket bind evidence (bind-unavoidable) and soffice/verapdf execve sets"
  - "recorded route decision: scoped-socket"
affects: [70-02, 70-03, 70-04, 70-05, internal/sandbox]

actuals:
  tokens: 13000
  tasks: 3
  commits: 3
plan_head_before: 7744ddf096e475669441e921d90a31dd7af6cad1
plan_head_after: 937a2e0b1a9335f019563a97d2f4bec318054aae

tech-stack:
  added: []
  patterns:
    - "Differential before unpin: a pin comes off only on a transcript that proves its own extractor and non-vacuity"
    - "Stage-only platform pin in a multi-stage Dockerfile for an arch-independent artifact tree"

key-files:
  created:
    - scripts/verapdf-differential.sh
    - .planning/phases/70-document-on-amd64/evidence/verapdf-differential.txt
    - .planning/phases/70-document-on-amd64/evidence/soffice-ipc-spike.txt
    - .planning/phases/70-document-on-amd64/deferred-items.md
  modified:
    - Dockerfile.document-worker
    - docker-compose.yml
    - deploy/chart/octoconv/templates/deployment-document-worker.yaml
    - scripts/verapdf-measure.sh
    - scripts/audio-rtf-measure.sh
    - .planning/phases/70-document-on-amd64/70-RESEARCH.md

key-decisions:
  - "Route decision: scoped-socket (socket-only Landlock grant kind, sock:DIR, MAKE_SOCK and nothing else)"
  - "Only the verapdf build stage is pinned to linux/amd64; the runtime stage is native"
  - "document-worker's compose platform pin removed on DIVERGENCES: 0"

patterns-established:
  - "Evidence-gated config change: the compose key is removed only after a committed transcript ending DIVERGENCES: 0"

requirements-completed: [CONF-07, CONF-01]

coverage:
  - id: D1
    description: "Document image is native on an arm64 docker server; verapdf stage alone pinned to linux/amd64"
    requirement: CONF-07
    verification:
      - kind: other
        ref: "scripts/verapdf-differential.sh -> DIVERGENCES: 0 (evidence/verapdf-differential.txt, run 20260930T140106Z-30055)"
        status: pass
    human_judgment: false
  - id: D2
    description: "PDF/A and document conversion e2e green on the native document image"
    requirement: CONF-07
    verification:
      - kind: e2e
        ref: "GOFLAGS=-v scripts/ci-local.sh e2e -> TestPDFAExportE2E, TestPDFANonCompliantE2E, TestDocumentConversionE2E (run 20260930T140523Z)"
        status: pass
    human_judgment: false
  - id: D3
    description: "soffice socket-bind measurements and the resulting route decision"
    requirement: CONF-01
    verification:
      - kind: other
        ref: "evidence/soffice-ipc-spike.txt CLASSIFICATION: bind-unavoidable"
        status: pass
    human_judgment: true
    rationale: "The socket route changes shared, security-relevant sandbox code; the user chose it at the blocking checkpoint. Whether MAKE_SOCK alone suffices is proven in 70-02, not here."

duration: n/a (spans an interrupted checkpoint)
completed: 2026-09-30
status: complete
---

# Phase 70 Plan 01: Native document image, differential, socket spike, route decision Summary

**The document image now builds natively on arm64 with only the verapdf/cli stage pinned to amd64, gated by a repeatable veraPDF differential that ended `DIVERGENCES: 0` on real soffice PDF/A output; the soffice socket bind was measured as unavoidable under /tmp and the user chose the socket-only Landlock grant (`scoped-socket`).**

## Performance

- **Tasks:** 3 (two auto, one blocking-human decision checkpoint)
- **Files modified:** 6 tracked source files, 1 new script, 4 planning artifacts

## Accomplishments

- `Dockerfile.document-worker`: `FROM --platform=linux/amd64 verapdf/cli:v1.30.2 AS verapdf`; the runtime stage carries no platform, so the image is built for the builder's architecture and runs veraPDF on Debian's openjdk-17-jre-headless.
- `scripts/verapdf-differential.sh`: builds the committed Dockerfile natively and for linux/amd64, self-checks its report extractor on the two committed reports, runs a 7-file corpus (three real soffice PDF/A-2b exports, a plain soffice PDF, two non-compliant fixtures, a corrupt PDF) through veraPDF in both images, enforces non-vacuity, and exits 2 rather than report a verdict it did not earn.
- document-worker's `platform:` key removed from docker-compose.yml; every comment describing the old whole-image pin (compose and its four sibling comments, Dockerfile, chart template header, verapdf-measure.sh, audio-rtf-measure.sh) rewritten to the stage-only pin, keeping the old reason (an emulated amd64 process cannot establish Landlock) visible.
- Spike: measured where soffice binds its single-instance socket under seven avoidance attempts; classification `bind-unavoidable`.

## Task Commits

1. **Task 1: native document image, verapdf stage pinned alone, differential, compose unpin, PDFA e2e green** - `6077229` (feat)
2. **Task 2: soffice single-instance socket spike** - `4a5ae2f` (docs)
3. **Task 3: route decision (checkpoint)** - recorded in this SUMMARY, no code commit
4. **Aux: unpullable minio images recorded as a deferred item** - `937a2e0` (docs)

## Route decision

option: scoped-socket

- Differential last line: `DIVERGENCES: 0` (evidence/verapdf-differential.txt, run `20260930T140106Z-30055`, commit 7744ddf with 3 tracked-dirty files; native image arm64 `sha256:593cf90e...`, amd64 image `sha256:c7ff39b3...`).
- Per-file table: soffice_docx_pdfa, soffice_xlsx_pdfa, soffice_pptx_pdfa MATCH `true|0|`; soffice_docx_plain and fixture_noncompliant MATCH `false|0|6.2.4.3/2 6.6.2.1/1`; fixture_noncompliant_marker MATCH `false|0|6.6.4/4 6.6.4/5`; corrupt.pdf MATCH `ABSENT|1|`. Non-vacuity OK on both images (compliant=3, non-compliant-with-rules=3, corrupt-parse-failure=1).
- Spike classification: `CLASSIFICATION: bind-unavoidable`
- Gate check: `yq '.services."document-worker" | has("platform")' docker-compose.yml` is false, so the native routes were selectable.

### What 70-02 must act on

1. **Bind path.** `soffice.bin` (not oosplash) binds an AF_UNIX socket at `/tmp/OSL_PIPE_<uid>_SingleOfficeIPC_<hash>` (uid 65534 for nobody) and connects to it itself. `-env:OSL_SOCKET_PATH`, the `OSL_SOCKET_PATH` environment variable and `--nolockcheck` do not move it. Running `soffice.bin` directly still binds under /tmp (and fails without the wrapper's environment).
2. **The pipe directory is chosen only from /tmp then /var/tmp.** With both unwritable soffice exits with "no valid pipe path found" and produces no PDF; nothing under the job directory or TMPDIR is considered. So the grant must be `sock:/tmp`, and /tmp must stay writable by file permissions (the worker creates job directories there via `os.MkdirTemp`; a per-task mount namespace was rejected in phase 69).
3. **soffice unlinks its socket at exit.** Baseline leaves nothing in /tmp. Under a ruleset an unlink needs `LANDLOCK_ACCESS_FS_REMOVE_FILE` on /tmp, which a MAKE_SOCK-only rule does not carry. A MAKE_SOCK-only rule therefore likely yields EACCES on that unlink and leaves a stale `OSL_PIPE_*` in /tmp. 70-02 must measure this and, if so, have the worker clean the path (D-09).
4. **Not yet proven that MAKE_SOCK alone suffices** for soffice to convert. 70-02's tracer proves it first and stops if it does not. If soffice needs more on /tmp, /tmp is also granted read-write and phase 69's concurrency 1, guard and sweep apply (D-09 second branch).
5. **Execve sets for the candidate lists.** soffice: `/bin/sh` (dash), `basename`, `dirname`, `grep`, `sed`, `uname`, `/usr/bin/soffice` -> `/usr/lib/libreoffice/program/soffice`, `oosplash`, `soffice.bin`. verapdf: `/opt/verapdf/verapdf`, `dirname`, `uname`, `which` (-> `which.debianutils`), `/usr/bin/java` -> `/usr/lib/jvm/java-17-openjdk-<arch>/bin/java` (arm64 on this host; the JVM path is architecture-specific).
6. **Constraint carried:** `accessReadWrite`, `accessReadOnly` and every existing class's token stream must stay byte-identical; the socket rule is a separate path set carrying MAKE_SOCK and nothing else.

## Files Created/Modified

- `Dockerfile.document-worker` - verapdf stage pinned alone, comment rewritten
- `docker-compose.yml` - document-worker platform key removed; pin comments rewritten (asynqmon's pin untouched)
- `deploy/chart/octoconv/templates/deployment-document-worker.yaml` - header no longer claims an x86 translation layer
- `scripts/verapdf-differential.sh` - new differential
- `scripts/verapdf-measure.sh`, `scripts/audio-rtf-measure.sh` - platform flags and pin wording removed
- `.planning/phases/70-document-on-amd64/70-RESEARCH.md` - "In-phase differential (70-01)" table
- `.planning/phases/70-document-on-amd64/evidence/{verapdf-differential,soffice-ipc-spike}.txt` - transcripts
- `.planning/phases/70-document-on-amd64/deferred-items.md` - minio images

No file under `internal/` or `cmd/` was changed (no sandbox right added before the decision).

## Decisions Made

- Only the verapdf stage is pinned; a native build is possible because `/opt/verapdf` holds jars, a shell script and data with no ELF object (D-01).
- The compose pin came off only on `DIVERGENCES: 0` (D-02, D-04).
- Route: `scoped-socket`, user-selected at the blocking checkpoint (T-70-04 mitigation held: no internal/sandbox edit preceded the choice).

## Deviations from Plan

### Environment and process deviations

**1. [Rule 3 - Blocking] minio images no longer pullable**
- **Found during:** Task 1 step 6 (`scripts/ci-local.sh e2e`)
- **Issue:** `docker compose up` failed with `pull access denied` for `minio/minio` and `minio/mc`; docker-compose.yml lines ~31 and ~53 reference them.
- **Fix:** Aliased locally, outside the repo: `docker tag cgr.dev/chainguard/minio:latest minio/minio:latest` and `minio/mc:latest`. No tracked file changed. Durable fix deferred (`.planning/phases/70-document-on-amd64/deferred-items.md`).
- **Committed in:** `937a2e0` (deferred-items record only)

**2. [Process] e2e ran with GOFLAGS=-v**
- The plan's verify names `scripts/ci-local.sh e2e`; it was run with `GOFLAGS=-v` so the transcript carries the per-test PASS lines the acceptance criteria require. Run id `20260930T140523Z`, transcript `.planning/ci-local/ci-local-20260930T140523Z.log`. It is stamped `7744ddf (TRACKED CHANGES NOT IN THIS COMMIT)` because the task commit `6077229` came after the run; the tree tested is the tree committed there.

**3. [Process] Task 1 automated verify satisfied by equivalent runs**
- The single-line `<automated>` verify was not executed verbatim (it re-runs the differential and the e2e in one chain). Its clean-branch conditions were each satisfied by the equivalent runs above: differential last line `DIVERGENCES: 0` in the saved evidence file, `document-worker` has no platform key, the built image's architecture equals the server's (arm64), and the e2e tier passed.

**Total deviations:** 3 (1 blocking environment fix outside the repo, 2 process). **Impact:** no scope creep; no tracked-file change beyond the plan.

## e2e evidence (Task 1 step 6)

From `.planning/ci-local/ci-local-20260930T140523Z.log`, commit stamp `7744ddf (TRACKED CHANGES NOT IN THIS COMMIT)`:

- `### tier e2e: PASS`
- `--- PASS: TestPDFAExportE2E (6.07s)` with subtests `PDFA` and `NoOptsRegression`
- `--- PASS: TestPDFANonCompliantE2E (2.06s)`
- `--- PASS: TestDocumentConversionE2E (12.24s)` (docx, xlsx, pptx, odt, ods, odp)

## Issues Encountered

None beyond the deviations above.

## Known Stubs

None.

## Threat Flags

None. No network endpoint, auth path or trust-boundary schema change was introduced.

## Next Phase Readiness

- 70-02 proceeds on the `scoped-socket` route: add the socket-only grant kind to internal/sandbox, then prove in its tracer that MAKE_SOCK on /tmp suffices (and measure the unlink-on-exit behaviour) before anything else.
- Open item outside phase scope: the minio/mc images (see deferred-items.md) are only aliased on this host.

## Self-Check: PASSED

- Commits `6077229`, `4a5ae2f`, `937a2e0` exist in `git log`; `git rev-list --count 7744ddf..HEAD` measured 3 before this SUMMARY commit.
- Evidence files exist; differential last line `DIVERGENCES: 0`; spike file carries `CLASSIFICATION: bind-unavoidable`.

---
*Phase: 70-document-on-amd64*
*Completed: 2026-09-30*
