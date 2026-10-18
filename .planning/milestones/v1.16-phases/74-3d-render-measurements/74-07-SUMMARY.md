---
phase: 74-3d-render-measurements
plan: 07
subsystem: cad-engine
tags: [live-proof, render, measurements, pixel-judge, compose]
status: complete
requires:
  - phase: 74-06
    provides: render confined and measured, the Go pixel judge calibrated, preflight proving render both ways
provides:
  - scripts/cad-liveproof.sh covering render, views, measurements, options, quality and the stage failure classes, with --out
  - scripts/cmd/cad-measurements-check, a measurements body validated with internal/measurements
  - evidence/74-07-liveproof.txt, the phase proven on a running compose stack, ALL PASS with tracked dirt 0
affects: [phase-74-verification]
key-files:
  created:
    - scripts/cmd/cad-measurements-check/main.go
    - .planning/phases/74-3d-render-measurements/evidence/74-07-liveproof.txt
    - .planning/ci-local/ci-local-20261005T214321Z.log
  modified:
    - scripts/cad-liveproof.sh
key-decisions:
  - "The VIEWS check asserts the camera from the fixture's real geometry: units_inch.step is X=2, Y=3, Z=1 inch, so a Z-up front view is wider than tall (height/width 0.5) and the top view is 1.5; a Y-up camera would give 1.5 for front and fail"
  - "Tracked dirt 0 for the live run was obtained by checking out the orchestrator's STATE.md modification for the run window and restoring it byte-identical afterwards (sha256 d81e8ba8...), not by committing or discarding it"
commits: 2
plan_head_before: c220cf0e4958ef60f312f9e2af2183cc8aeeec32
plan_head_after: a1873972e70f2bff8067e6485696355488e3a143
actuals:
  tokens: 31000
  tasks: 2
  commits: 2
requirements-completed: [RNDR-01, RNDR-02, RNDR-03, RNDR-04, RNDR-05, MEAS-01, MEAS-02, MEAS-03, MEAS-04]
duration: "Task 1 by the previous executor (commit d93f6db, 2026-10-05 22:47 +0300); Task 2 2026-10-05T21:30Z to 21:58Z"
completed: 2026-10-05
---

# Phase 74 Plan 07: End-to-end live proof Summary

Render of STEP, IGES and STL to a pixel-judged PNG with a validated measurements document beside it, observed on a running compose stack built from commit d93f6db: `SUMMARY,result=ALL PASS` with tracked dirt 0, plus the convert regression, options, honesty of units and the failure classes by stage.

## Status: complete

## Task 1 (commit d93f6db, by the previous executor)

The previous executor committed Task 1 and died on an API auth error before recording anything. Re-checked here:

- `bash -n`, `go vet` and `go build` of `scripts/cmd/cad-measurements-check`, `--help` naming `--out`: pass.
- No comment line in the script besides the shebang, no `//` line added to the CLI: pass (see deviation 2 for the plan's check).
- `role = 'measurements'` appears only in the row-count fact query (line 394); the body is fetched through the URL the job response gives for role `measurements`.
- The FORMATS row was updated by d93f6db: it requires exactly `[["convert","cad","iges","stl"],["convert","cad","step","stl"],["render","cad","iges","png"],["render","cad","step","png"],["render","cad","stl","png"]]` under operations and `iges>stl;step>stl` under engines, failing the run on any other set.

The previous executor also ran the proof once (tracked dirt 1, the STATE.md modification) and left the output uncommitted. That run is superseded; the file was overwritten by the run below.

## Task 2: the live proof (commit a187397)

`scripts/cad-liveproof.sh --out .planning/phases/74-3d-render-measurements/evidence/74-07-liveproof.txt`, run once from the main checkout, exit 0.

- HDR: `commit=d93f6db`, `tracked_dirt=0`, `cad_engine_timeout_s=886`, `cad_render_timeout_s=480`, `sandbox_disabled=false`, `server_arch=arm64`, `started_by_run=postgres;redis;minio;api;cad-worker`, cad-worker image `sha256:adf190a3...` (1,044,293,309 bytes; the phase 73 image `sha256:84b6710a...` is 729,816,267).
- `SUMMARY,result=ALL PASS,formats=1,inspect=5,convert=5,quality=1,render=5,views=6,meas=6,opts_reject=9,reject=2,bomb=12,render_retry=1,mesh_signal=1,empty=1,cancel_render=1,cancel=1,worker_kill=1,image=1`
- RENDER x5 (units_inch.step, NIST .stp, festo .igs, festo as .iges, stl_ascii_box.stl): `outputs=2`, `roles=primary@0;measurements@1`, PNG judged ok at 1280 opaque by `cad-pixel-judge`, `measurements=valid` by `cad-measurements-check`.
- VIEWS: iso, front and top give three different sha256; front box 1128x566 (h/w 0.5), top 752x1128 (h/w 1.5), so Z is up; size 2048 judged at 2048 x 2048; background transparent judged ok with `--background transparent`, `alpha=1`, `frac_transparent=0.6049` against part `frac=0.3951`, and rejected by the opaque judge.
- MEAS: units_inch `inch/declared extents=1:2:3 volume=6 area=22`, the same with `assume_unit=mm`; stl_ascii_box `unspecified/none 10:20:30 volume=6000 area=2200` and `mm/client_assumed` with `assume_unit=mm`; surfaces_only.iges `volume=null area=null reason=no_solids`; nasa-hubble-all-pieces.stl `null/null stl_not_watertight`. No 0.
- Convert regression: `CONVERT,name=units_inch_step ... triangles=12 ... extents=25.400000:50.799999:76.199997`, judged ok; the other four convert rows ok.
- QUALITY: LinearSlide-MGN15x-Block.step draft 3988, standard 5948, fine 14140; render with fine done and judged ok.
- OPTS_REJECT x9: view unknown, size 255, 2049, string, quality unknown, background unknown, unknown key (`invalid_options`); render key on convert, quality on an STL render (`opts_not_applicable`); every one 422 with `jobs_created=0`.
- EMPTY: zero_extent.stl `failed cad_empty_result outputs=0 download_url=absent`.
- RENDER_RETRY: SIGTERM to cad-render during the nasa-pillars-of-creation render, `status=done retried=1`, events `active>active`, outputs 2, judged ok, measurements valid.
- MESH_SIGNAL: SIGTERM to cad-mesh during the NIST .stp render, `failed cad_unreadable retried=0`, outputs 0.
- CANCEL_RENDER: cad-render gone in 0.3 s, `canceled`, no download_url, outputs 0.
- REJECT x2, BOMB x12, CANCEL, WORKER_KILL (`orphans=0`, `triangles=3820000`, done after restart): unchanged phase 73 sections, all ok.

The plan's awk gate over the evidence, `grep '^HDR,tracked_dirt=0$'` and `grep '^SUMMARY,result=ALL PASS'` pass.

## Local gates

- `GOTOOLCHAIN=go1.26.4 scripts/ci-local.sh helm gate race`: helm, gate, race PASS, `ALL REQUESTED TIERS PASSED`, transcript `/Users/apaderin/dev/octoconv/.planning/ci-local/ci-local-20261005T214321Z.log` on commit a187397. The transcript says `TRACKED CHANGES NOT IN THIS COMMIT`: that is `.planning/STATE.md` only. The race tier reported cached packages (29 `(cached)` lines).
- Because of that, `GOTOOLCHAIN=go1.26.4 go test ./... -count=1` was run uncached: exit 0, 31 packages ok, no failure.
- `git diff --exit-code main -- internal/convert/exec.go`: no diff.

## Host and stack

- No octoconv pod in k8s (orbstack context), no octoconv container before or after (0 and 0).
- The run started only postgres, redis, minio, api and cad-worker and stopped only those; no `down -v`; no other project's container touched; `.env` not read.
- Compose builds were cache hits (the image ids match the earlier run); no image was built new and none was removed.
- Free disk: 12 GiB before the run, 9 GiB after the run and the Go gates.

## Deviations from Plan

1. **[Rule 1 - Plan text] The front view of the inch box is not taller than wide.** The plan's truth assumed the box is 1 x 2 x 3 with Z the longest side. The fixture is X=2, Y=3, Z=1 inch, so with Z up the front view is 2 wide and 1 tall. Task 1 asserts front h/w 0.5 and top h/w 1.5, which tests the actual claim (Z up): a Y-up camera would show the 3 inch side upright in front and fail. Observed 1128x566 and 752x1128.
2. **[Rule 1 - Plan check] The plan's comment check could not pass on any file.** `grep -nE '^\s*#' FILE | grep -v ':1:#!'` assumes a filename prefix that `grep -n` on one file does not print, so the shebang line `1:#!/usr/bin/env bash` is never filtered. Run as `grep -v '^1:#!'`: no comment line.
3. **OPTS_REJECT has 9 rows, not 8.** The must_haves list counts nine cases (size 255, 2049 and a string are three), so the plan's ">= 8" holds.
4. **Tracked dirt.** `.planning/STATE.md` carried the orchestrator's uncommitted modification. For the live run only it was copied to the scratchpad, checked out to HEAD, and copied back after the evidence commit; sha256 `d81e8ba872146aa8d1f753c05fb1dd6b452efaa1ac66c2d14c553b92bb65abb5` before and after. STATE.md was not committed or otherwise changed. ci-local ran with it present.

No product code changed in this plan.

## Known Stubs

None.

## Self-Check: PASSED

- FOUND: scripts/cad-liveproof.sh, scripts/cmd/cad-measurements-check/main.go, evidence/74-07-liveproof.txt, .planning/ci-local/ci-local-20261005T214321Z.log
- FOUND: d93f6db, a187397 (both ancestors of HEAD, both with a good SSH signature)
