---
phase: 71-evaluation-gate
plan: 05
subsystem: infra
tags: [cad, rendering, f3d, vtk, osmesa, yardstick, contact-sheet, supply-chain]
status: complete

requires:
  - phase: 71-04
    provides: VTK + OSMesa seven-view render, pixelcheck, contact_sheet.py, four contact sheets
provides:
  - scripts/cad-f3d-yardstick.sh (sha256-pinned f3d 3.5.0 Linux-x86_64 tarball, one-off amd64 image, same STL rendered by VTK and f3d)
  - evidence 71-05-f3d.txt (F3D_ASSETS, command lines, per-render times, informative pixel lines)
  - evidence 71-05-contact-f3d.png (VTK next to f3d, iso/front/top, five real parts)
  - contact_sheet.py --title
  - user acceptance of the render (D-11 step 2): approved
affects: [71-06, 71-07, 71-08]

actuals:
  tokens: 8060
  tasks: 2
  commits: 1
plan_head_before: 8a3ed8803c54430709d4fa84379f3fc2804d7c9f
plan_head_after: c59bafe8f80e9967b1a85067287a707489f8d64e

tech-stack:
  added: []
  patterns: [one-off emulated image built from a stdin Dockerfile with a sha256-pinned tarball, fail-fast flag check against --help, evidence line per retry]

key-files:
  created:
    - scripts/cad-f3d-yardstick.sh
    - .planning/phases/71-evaluation-gate/evidence/71-05-f3d.txt
    - .planning/phases/71-evaluation-gate/evidence/71-05-contact-f3d.png
  modified:
    - scripts/cad-measure/contact_sheet.py

key-decisions:
  - "Renderer: VTK 9.7.1 + OSMesa is accepted by the user on the contact sheets; f3d stays a visual yardstick only (D-16)"
  - "f3d has no Linux arm64 asset in v3.5.0 (F3D_ASSETS linux_arm64=0, latest is also v3.5.0), so it cannot be measured natively or under Landlock"
  - "The default f3d look is compared; --edges is not used because it draws every triangle edge while VTK draws feature edges only"

requirements-completed: [EVAL-01]
---

# Phase 71 Plan 05: f3d yardstick and render acceptance Summary

f3d 3.5.0 was rendered on the same STLs as VTK in a one-off emulated amd64 image and tiled next to VTK; the user reviewed all five contact sheets and answered "approved", which accepts VTK 9.7.1 + OSMesa as the renderer and keeps f3d as a yardstick only.

## Accomplishments

- **Task 1 (`c59bafe`)**: `scripts/cad-f3d-yardstick.sh`. Gate `F3D-OK`, `F3D_SUMMARY,parts=5,fail=0,retries=0`.
  - `F3D_ASSETS,release=v3.5.0,linux_x86_64=1,linux_arm64=0`; `releases/latest` is also v3.5.0.
  - `F3D_SHA256=3e4590c709e6ecd14ec3088527dd625f1ed79b970009eb3a4baef9b410650f10` equals the GitHub asset digest; the script aborts otherwise. Tarball 86803419 bytes, verified.
  - One-off image `octoconv-f3d-yardstick:11b1ac717bfc`, linux/amd64, built from the digest-pinned bookworm-slim base, `USER nobody`, rendered with `--network none --cpus 2 --memory 2g`. No Dockerfile was added to the repo, and `grep -c f3d Dockerfile.cad-measure` is 0.
  - 15 pairs (5 real parts by iso, front, top, 1280x1280): the same orientation in every pair, part bounding boxes within 14 px (typically 11 to 12 px), f3d fits with a 63 to 64 px margin against VTK's 76 px, and the filled fraction is 2 to 4 percent higher in f3d.
  - f3d draws no feature edges (dark fraction 0.0000 to 0.0022 against VTK's 0.0046 or more) and shades darker and flatter. The VTK pixel judge gives `ok=0` for 14 of 15 f3d images because of the missing edges; this is informational only.
  - f3d median 3.33 s per render, emulated amd64 and pessimistic; VTK median 1.26 s per container, native arm64. Neither is a basis for a decision, and nothing here is a Landlock or sandbox claim.
- **Task 2 (checkpoint:human-verify, blocking-human)**: contact-sheet acceptance. Sheets shown: `71-04-contact-views.png`, `71-04-contact-quality.png`, `71-04-contact-robustness.png`, `71-04-contact-dxf.png`, `71-05-contact-f3d.png`.

## User acceptance (D-11 step 2)

- **Answer:** "approved" (2026-10-04). The coordinator relayed it and said the user opened the evidence directory and reviewed all five sheets. I received it only through that relay, not directly from the user.
- **Remarks and re-renders:** none, so no sheet was regenerated.
- **Accepted:** the renderer VTK 9.7.1 + OSMesa. f3d stays a yardstick only (D-16).
- **Not done here:** the renderer choice is not written to `71-DECISION.md` by this plan. That file does not exist yet.

## Deviations from Plan

### Approved supply-chain item

**1. [Supply chain - approved] `libssl3` added to the one-off f3d image**
- **Found during:** Task 1 first run (`f3d: error while loading shared libraries: libcrypto.so.3`).
- **Issue:** the plan's apt line is `libosmesa6 libgl1`. `ldd` showed `libcrypto.so.3` as the only missing library, and bookworm-slim has no OpenSSL runtime. The plan does not pin `libssl3`.
- **Action:** I stopped and checkpointed. The coordinator relayed the user's answer "libssl3 approved". `libssl3` is installed only in the one-off `octoconv-f3d-yardstick` image, from the same digest-pinned base and apt sources. It is not in `Dockerfile.cad-measure` or any production image. It is logged as an `APPROVAL` line in `71-05-f3d.txt`.
- **Commit:** `c59bafe`

### Auto-fixed Issues

**2. [Rule 3 - Blocking] `.tar.gz` instead of `.tar.xz`**
- **Issue:** bookworm-slim has no `xz`, and `xz-utils` would be another unpinned package.
- **Fix:** pinned `F3D-3.5.0-Linux-x86_64.tar.gz` (sha256 above). Logged as a `DEVIATION` line in the evidence.
- **Files modified:** scripts/cad-f3d-yardstick.sh
- **Commit:** `c59bafe`

**3. [Rule 3 - Blocking] `contact_sheet.py --title`**
- **Issue:** the plan requires the caption "f3d 3.5.0, emulated amd64, no Landlock - yardstick only" on the sheet, and `contact_sheet.py` had no way to draw one.
- **Fix:** optional `--title TEXT` that draws a title band; behavior without it is unchanged. The file is not in the plan's `files_modified`. Because the measurement-image tag hashes `scripts/cad-measure/*.py`, the tag moved to `octoconv-cad-measure:ocp7.9.3.1.1-full-48d9162df359`, rebuilt from the approved lock files with no new packages.
- **Files modified:** scripts/cad-measure/contact_sheet.py
- **Commit:** `c59bafe`

**4. [Rule 1 - Bug] f3d 3.5.0 flag names and camera behavior**
- **Issue:** `--bg-color` does not exist; the flag is `--background-color`. `--edges` draws every triangle edge, which is not comparable to VTK feature edges. With `--up=+Z` and a top-down camera, f3d ignores `--camera-view-up` and the part came out rotated 90 degrees against VTK (measured on the slide top view: bounding box 63:352:1215:927 against VTK 357:76:922:1202). f3d also needs `HOME` and `XDG_CACHE_HOME` on the writable job directory because `nobody`'s home does not exist.
- **Fix:** `--background-color=1,1,1`, no `--edges`, `--up=+Y` for the top view only, `-e HOME=/job -e XDG_CACHE_HOME=/job/.cache`. The script checks every flag against `f3d --help` and stops before rendering if one is missing, and it stops after the first failed f3d render. Each fix is recorded in `F3D_NOTE` and `F3D_FLAG` lines.
- **Files modified:** scripts/cad-f3d-yardstick.sh
- **Commit:** `c59bafe`

### Plan wording versus observed behaviour (documented, not forced)

- **VTK PNGs are re-rendered by the yardstick script.** The 71-04 PNGs live in a temporary directory that is deleted at exit, so the script re-meshes and re-renders the VTK side itself. The same real set and pixel values match the 71-04 evidence, since the VTK render is deterministic.
- **f3d pixel lines are informative.** `F3DPIX` lines carry `informative=1`. The part-mode dark-pixel criterion was calibrated for VTK feature edges, so it does not apply to f3d.

## Auth gates

None. The commit was signed on the first attempt (SSH signature verified as "Good").

## Known Stubs

None.

## Threat Flags

None beyond the plan's register. T-71-13: sha256 constant equals the GitHub asset digest, mismatch removes the tarball and exits 1, the image is one-off, rendering runs with `--network none` and `USER nobody`. T-71-14 is accepted: the container mounts the STL read-only and a scratch job directory only, and f3d never reaches a production image.

## Self-Check: PASSED

- scripts/cad-f3d-yardstick.sh, evidence 71-05-f3d.txt and 71-05-contact-f3d.png: present
- commit `c59bafe` on gsd/phase-71-evaluation-gate; `git rev-list --count` from the ledger gives 1
- gate printed `F3D-OK`; `internal`, `cmd`, `deploy`, `docker-compose.yml` unchanged against 4e595c0 (excluding the CAD testdata)
