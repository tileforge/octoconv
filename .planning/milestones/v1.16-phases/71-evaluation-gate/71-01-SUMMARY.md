---
phase: 71-evaluation-gate
plan: 01
subsystem: infra
tags: [supply-chain, pip, hash-pinning, cadquery-ocp, vtk, ezdxf, debian-bookworm]
status: complete

requires:
  - phase: 70
    provides: milestone v1.16 roadmap and CAD evaluation scope
provides:
  - four hash-pinned lock files for the CAD measurement image
  - reproducible lock generator scripts/cad-measure/lock.sh
  - digest-pinned base image reference for plan 71-02
  - user approval of the lock files (legitimacy gate)
  - D-18 outcome "record" for plan 71-07
affects: [71-02, 71-07]

actuals:
  tokens: 8400
  tasks: 3
  commits: 2
plan_head_before: d06b755acf1f130442c14f4ebec6d043ec2c6902
plan_head_after: 4c3c6f8

tech-stack:
  added: [pip-tools 7.6.1 (disposable container only)]
  patterns: [hash-pinned requirements generated inside the pinned base image, PyPI JSON digests for no-deps wheels]

key-files:
  created:
    - scripts/cad-measure/lock.sh
    - scripts/cad-measure/requirements-common.txt
    - scripts/cad-measure/requirements-ocp-7.9.3.1.1.txt
    - scripts/cad-measure/requirements-ocp-8.0.1.0.0.txt
    - scripts/cad-measure/requirements-vtk.txt
    - .planning/phases/71-evaluation-gate/evidence/71-01-lock.txt
    - .planning/phases/71-evaluation-gate/evidence/71-01-decisions.txt
  modified: []

key-decisions:
  - "Lock files approved by the user; installation allowed only via pip --require-hashes, OCP and vtk with --no-deps"
  - "D-18 outcome: record. Libraries libfreeimage, libraw, libjasper stay in the image; licences are recorded as an engineering record with a non-blocking legal review flag"

requirements-completed: [EVAL-01, EVAL-04]
---

# Phase 71 Plan 01: Hash-pinned CAD measurement locks Summary

Four sha256-pinned lock files for the CAD measurement image, generated reproducibly inside a digest-pinned debian:bookworm-slim (Python 3.11.2), hash-verified by `pip download --require-hashes`, and approved by the user before any package was installed.

## Accomplishments

- `scripts/cad-measure/lock.sh` builds `requirements-common.txt` with pip-compile in a disposable bookworm container, and the OCP (7.9.3.1.1 and 8.0.1.0.0) and vtk 9.7.1 locks from PyPI JSON digests.
- Package set is exactly the expected one: ezdxf 1.4.4, numpy 2.4.6, pillow 12.3.0, fonttools 4.66.1, pyparsing 3.3.3, typing-extensions 4.16.0 (`UNEXPECTED: (none)`), no numpy 2.5.x.
- Both OCP aarch64 hashes match the independent research measurement (`CROSSCHECK ... match=yes` for 7.9.3.1.1 and 8.0.1.0.0).
- Hash verification by download passed for all four files in bookworm (`HASHES-OK`); no package was installed into any image and no measurement image was built.
- BASE_IMAGE: `debian:bookworm-slim@sha256:3783cc01769c7b2b1b83a5c5ad96c815348e28ed7da68e2e3687004faa906251`, recorded in `evidence/71-01-lock.txt` for plan 71-02.

## User answers

- Task 2, lock-file legitimacy gate: **approved**.
- Task 3, D-18 decision: **record** (record licences, keep libfreeimage/libraw/libjasper, non-blocking "нужен OK юриста" flag, no patchelf experiment).

D-18 outcome: record

The OCP wheel libraries are not legally cleared. The record is an engineering note (FreeImage GPL v2/v3 or FIPL, LibRaw LGPL 2.1 or CDDL 1.0, JasPer License 2.0, OCCT LGPL 2.1 with exception; internal use only, image not distributed) and the legal review flag stays open. Plan 71-07 Task 2 reads the outcome line above. Details in `evidence/71-01-decisions.txt`.

## Task Commits

| Task | Name | Commit | Files |
| ---- | ---- | ------ | ----- |
| 1 | Hash-pinned lock files and generator | 5564535 | scripts/cad-measure/*, evidence/71-01-lock.txt |
| 2, 3 | Lock approval and D-18 decision (checkpoints, no code) | 4c3c6f8 | evidence/71-01-decisions.txt |

`commits: 2` is measured with `git rev-list --count d06b755..4c3c6f8` at SUMMARY write time and excludes the SUMMARY commit itself.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `--strip-extras` added to pip-compile**
- **Found during:** Task 1
- **Issue:** the plan's pip-compile invocation did not name `--strip-extras`; it was added to the generator so the lock does not carry extras markers.
- **Fix:** `pip-compile --quiet --generate-hashes --strip-extras --no-header` in `lock.sh`.
- **Files modified:** scripts/cad-measure/lock.sh
- **Commit:** 5564535

### Other notes

- **Base digest is the index digest, not the arm64 manifest.** The research quoted the arm64 manifest `sha256:3783cc01...`; the plan required the digest of the image index from `docker buildx imagetools inspect`. The value recorded in `lock.sh` and evidence is the one resolved at execution time, so it can differ from the research text.
- **Extra evidence file.** Task 2 and Task 3 are checkpoints with no code output; their answers and the D-18 licence record were written to `evidence/71-01-decisions.txt` in addition to this SUMMARY so each checkpoint outcome has a committed artifact.

## Known Stubs

None.

## Threat Flags

None. No new network endpoints, auth paths or schema changes; `lock.sh` fetches from PyPI and Docker Hub, already covered by T-71-01, T-71-02 and T-71-SC.

## Self-Check: PASSED

- scripts/cad-measure/{lock.sh,requirements-common.txt,requirements-ocp-7.9.3.1.1.txt,requirements-ocp-8.0.1.0.0.txt,requirements-vtk.txt}: present
- evidence/71-01-lock.txt and evidence/71-01-decisions.txt: present
- commits 5564535 and 4c3c6f8: present on gsd/phase-71-evaluation-gate
- "approved" and "D-18 outcome: record" present in this SUMMARY
