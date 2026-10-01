---
phase: 70-document-on-amd64
plan: 06
subsystem: infra
tags: [chromium, html, dev-shm, inotify, liveproof, gap-closure, measurement]
status: complete

requires:
  - phase: 70-document-on-amd64
    provides: "eight-class gate transcript with the one red html check (70-05), 70-VERIFICATION.md gap 1"
  - phase: 69-html-probe-the-tmp-decision
    provides: "TestChromiumLargeDocumentNeedsNoDevShmLive and the G-69-3 no-mount decision the control defends"
provides:
  - "Measurement of what Chromium 154.0.8037.57 does with shared memory in seven conditions (evidence/chromium-devshm-measure-70-06.txt)"
  - "A /dev/shm control a fallback cannot defeat: observation with /dev/shm reachable plus an inotify observer outside the sandbox"
  - "html gate back to ALL PASS (9 checks) on an image built from this tree; PASS output carries chromium: and dev-shm-observe: lines"
  - "SANDBOX-PATHS.md html section and the debug record corrected; Chromium stays unpinned by decision"
affects: [70-09, SANDBOX-PATHS.md, scripts/sandbox-liveproof.sh]

requirements-completed: [CONF-01]

actuals:
  tokens: 6100
  tasks: 2
  commits: 3
plan_head_before: 614d9b0793efa14a09a5676e85de8005de3d99c9
plan_head_after: 7350b67b486c99d04dea8bd97a172be4ce650313

tech-stack:
  added: []
  patterns:
    - "A negative control observes the effect directly (an inotify watch outside the sandbox, with the resource reachable) instead of inferring a dependency from a failure under denial, because a fallback turns a denial into a success"
    - "A control that an engine's current behaviour cannot exercise is a program that does what the engine would do, run through the same helper and ruleset"

key-files:
  created:
    - internal/convert/devshm_observer_linux_test.go
    - internal/convert/devshm_observer_other_test.go
    - .planning/phases/70-document-on-amd64/evidence/chromium-devshm-measure-70-06.txt
    - .planning/phases/70-document-on-amd64/evidence/liveproof-html-70-06.txt
  modified:
    - internal/convert/chromium_live_test.go
    - scripts/sandbox-liveproof.sh
    - Dockerfile.chromium-worker
    - .planning/research/SANDBOX-PATHS.md
    - .planning/debug/chromium-dev-shm-mount.md

key-decisions:
  - "Control variant is dash, not the flag-less engine: DEVSHM-NO-FLAG-REACHABLE is unused, so the engine creates nothing under /dev/shm to be observed; dash (granted by htmlReadOnly) creates one entry through the same helper and observation ruleset"
  - "Chromium stays unpinned (user decision 2026-09-30); the gate logs the version and the control relies on no version-specific failure"
  - "withoutDisableDevShm was removed: nothing uses the flag-less argv any more"

patterns-established:
  - "devShmObservationExec builds the observation ruleset exactly as ChromiumConverter.exec does plus one rw entry, so the test-only grant never reaches htmlReadWrite"

duration: ~75min
completed: 2026-09-30
---

# Phase 70 Plan 06: html /dev/shm control rebuilt on a measurement Summary

**Chromium 154.0.8037.57 creates nothing under /dev/shm in any condition (memfd_create), so the phase-69 "must fail when denied" control is replaced by an inotify observation with /dev/shm reachable; html's gate is ALL PASS (9 checks) again.**

## The measurement

Image `sha256:49d0c077a294423bcaa4b585b75d53836e9e8fa0b5940b2339c92f39ea4c6a6d`, built from this tree (Dockerfile.chromium-worker), Chromium 154.0.8037.57 (Debian 154.0.8037.57-1~deb12u1), strace -ff as the image user under `--cpus=2 --memory=2g`, an 84,438,002-byte fixture (scratch copy of the buildLargeDocFixture recipe) plus a small one. Seven conditions D1-D7 as planned; all exit 0 and all PDFs carry `/Title` (20,454,608 bytes for the large fixture, D7 = production included).

- /dev/shm creates: 0 succeeded, 0 denied, 0 mkdir, 0 unlink, in every condition, for both fixtures.
- Shared memory comes from `memfd_create("shared-memory-region", ...)`: 61 succeeded per large run; 5 more fail EINVAL every run (cause not investigated; run unaffected).
- The flag's only effect: without it Chromium calls `statfs("/dev/shm")` (succeeds even when Landlock grants nothing); with it the probe is absent.
- 4 `/tmp/.org.chromium.Chromium.*` creates with and without the flag, covered by the existing rw:/tmp.
- The one `openat("/dev/shm", O_PATH)` seen in D4/D6 is the sandbox helper adding the `rw:/dev/shm` rule (present with the flag on too), not the engine.

Classification lines (verbatim in the evidence file):

```
DEVSHM-WITH-FLAG-REACHABLE: unused
DEVSHM-NO-FLAG-REACHABLE: unused
DEVSHM-NO-FLAG-DENIED: falls back to memfd_create
```

No halt condition fired (DEVSHM-WITH-FLAG-REACHABLE is unused; D7 produced the marker). The measurement ran in a scratch image layered on the built image with strace added; the tag and the two intermediate tags were removed by exact name afterwards.

## The rebuilt control

Step e of `TestChromiumLargeDocumentNeedsNoDevShmLive` (name, fixture, floors, limits, steps c and d, the `dev-shm:` line unchanged):

- Observation ruleset: `devShmObservationExec` = `forJobRW(runDir, htmlReadWrite + /dev/shm, htmlReadOnly, htmlEngines).withEnv(engineEnv(""))`, the same as `ChromiumConverter.exec` plus one rw entry. `htmlsandbox.go` and `chromium.go` are unchanged (git diff vs b5e0f3f empty).
- Observer (`devshm_observer_linux_test.go`): inotify on /dev/shm (IN_CREATE, IN_MOVED_TO; IN_Q_OVERFLOW counts as activity), started before an engine run, drained after it exits. The `!linux` stand-in fails the test so darwin vet and test still compile.
- Control (variant "unused"): `dash -c ': > /dev/shm/devshm-control-<marker>'` through the observation ruleset; the observer must see an entry, else `the method cannot see a /dev/shm dependency`. The flag is not load-bearing on this Chromium.
- Production observation: `chromiumPrintArgs` unchanged under the same ruleset must render the marker with zero entries.
- Logs `chromium: Chromium 154.0.8037.57` and `dev-shm-observe: control=1 production=0`.

## Gate transcript

`scripts/sandbox-liveproof.sh html` at commit `02e8d1c8571417c45cdf9d8ed6eed0cb467fffcb`, image id `sha256:49d0c077a294423bcaa4b585b75d53836e9e8fa0b5940b2339c92f39ea4c6a6d`, final line `ALL PASS (9 checks)` (committed in `6b88ada`, `evidence/liveproof-html-70-06.txt`). The large-document PASS shows `chromium:`, `dev-shm-observe: control=1 production=0` and `large-doc: fixture=84543422 bytes pdf=20461106 bytes wall=4.86s`. No shared-memory docker flag exists in the script. Host `go vet`, linux/arm64 `go vet`, `go test ./internal/convert/` (63s) and `scripts/conf05-crosscheck.sh html` (MATCH) all pass.

## Inversions (each on a freshly built image, then reverted; `git diff --quiet -- internal/ cmd/ scripts/` held after each)

- (i) Observer made blind (`devShmWatchDir = "/srv"`): FAIL, `control: created /dev/shm/devshm-control-... under /dev/shm and the observer saw nothing -- the method cannot see a /dev/shm dependency`. Reverted.
- (ii) Drop rw:/dev/shm, (iii) remove --disable-dev-shm-usage: NOT RUN. With the classification "unused" they cannot fail by construction (the engine creates nothing under /dev/shm whether or not it can, and the flag only removes a statfs); recorded in the evidence file and in SANDBOX-PATHS.md, as the plan directs.
- (iv) Extra, production side: the test process itself creates an entry under /dev/shm between starting the production observer and the production run: FAIL, `production observation: the production argv created 1 entr(ies) under /dev/shm ...`. This shows the production assertion trips on an entry the observer reports. Reverted.

Both failing outputs are in `evidence/liveproof-html-70-06.txt`.

## Task commits

1. Task 1: `02e8d1c` test(70-06) rebuild the control, observer files, gate echo, Dockerfile comment, measurement evidence; `6b88ada` docs(70-06) gate transcript
2. Task 2: `7350b67` docs(70-06) SANDBOX-PATHS.md html section, debug record, inversion evidence

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Measurement layer instead of per-run apt-get**
- **Found during:** Task 1 step 1
- **Issue:** sandbox-paths-measure.sh trace installs strace in every container; seven conditions x two fixtures would repeat it.
- **Fix:** one scratch image `FROM` the built image with strace added; same mechanics otherwise (root prepares, setpriv drops to the image user, SYS_PTRACE, -ff, completed calls only). The traced call set was widened from open/create/mkdir/unlink/memfd_create to `%file,memfd_create` so a stat/statfs of /dev/shm is visible; this is what found the flag's statfs effect.
- **Files modified:** none in the repo (scratch only)

**2. [Plan text, not a defect] Docker's default /dev/shm is not 64 MiB here**
- Every DEVSHM-MOUNT line shows 8212480k; D5 is the only 64 MiB run. Recorded in the evidence file.

**3. Extra inversion (iv)** added beyond the plan because (ii) and (iii) cannot run on this classification and the production-side assertion would otherwise have no shown failure.

## Open points, not blocking

- Behaviour when `memfd_create` is denied (for example by a seccomp profile) was not measured: there Chromium could fall back to /dev/shm. Stated in SANDBOX-PATHS.md; the deployed runtime profile allows memfd_create.
- The 5 EINVAL memfd_create calls per run were not investigated.
- `withoutDisableDevShm` was deleted as dead code.

## Known Stubs

None.

## Threat Flags

None. The test-only `rw:/dev/shm` exists only in `devShmObservationExec`; `htmlReadWrite` is unchanged and conf05 html prints MATCH. `internal/convert/exec.go` is byte-identical to 7744ddf; go.mod and go.sum are unchanged.

## Self-Check: PASSED

Files present: internal/convert/devshm_observer_linux_test.go, devshm_observer_other_test.go, chromium_live_test.go (contains `func TestChromiumLargeDocumentNeedsNoDevShmLive(`), both evidence files, this summary. Commits `02e8d1c`, `6b88ada`, `7350b67` exist. `grep -c 'InotifyInit' devshm_observer_linux_test.go` is 1 (InotifyInit1); `grep -cE 'chromium-headless-shell=' Dockerfile.chromium-worker` is 0; `git diff --exit-code b5e0f3f -- internal/convert/htmlsandbox.go internal/convert/chromium.go` exits 0.
