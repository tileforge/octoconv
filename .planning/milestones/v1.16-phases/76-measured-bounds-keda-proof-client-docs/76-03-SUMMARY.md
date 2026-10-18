---
phase: 76-measured-bounds-keda-proof-client-docs
plan: 03
subsystem: measurement
tags: [cad, bounds, in-worker, io, pod-rule, quiet-host, iges-ceiling, derive76, rule-amend]
status: complete

requires:
  - phase: 76-01
    provides: STACK_GLOBAL render_deadline_rule=remainder; step, iges and stl not byte-bound
  - phase: 76-02
    provides: evidence/76-02-bounds.txt (stage rows, 26 RULE lines, 3 RULE_AMEND lines)
provides:
  - scripts/cad-inworker-measure.sh (--smoke, --cells, --no-build; load gate; per-job MinIO cleanup)
  - scripts/cad-dxf-bounds.sh derive76 --select-cells | --ladder render_iges|memory_iges | --quiet | --derive
  - evidence/76-03-derived.txt with 16 DERIVED lines, PATHB, CHECK, POD_ROUTE, GAP_CLOSED, KNOWN_LIMIT and SUMMARY result=ok
  - evidence/76-03-{cells,ladders,quiet,inworker,inworker-IM120MiB,inworker-IM128MiB,inworker-partial-1,inworker-partial-2,inworker-smoke,minio-cleanup}.txt
affects: [76-04, 76-05, 76-06]

actuals:
  tokens: 121422
  tasks: 3
  commits: 29
plan_head_before: 0efc9f71ed85642427067277a37100e38d413b6b
plan_head_after: 99a67d057901290fda12e0db164074b58b3bb78b

tech-stack:
  added: []
  patterns:
    - "A rule gap found by a STOP is closed only by an orchestrator RULE_AMEND committed before the next timed row; every STOP line stays"
    - "A timed row counts only at host load1 <= hw.ncpu at its start and its end; rows on file are excluded from W by recorded load, never by wall"
    - "The pod rule is checked per route (one job per pod, CAD_WORKER_CONCURRENCY=1)"

key-files:
  created:
    - scripts/cad-inworker-measure.sh
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-03-derived.txt
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-03-cells.txt
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-03-ladders.txt
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-03-quiet.txt
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-03-inworker.txt
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-03-inworker-IM120MiB.txt
  modified:
    - scripts/cad-dxf-bounds.sh

key-decisions:
  - "render_remainder_all: every 3D render deadline is R=886-guard-mesh-io and every render variant is classified against R; no render row is a STOP by its wall"
  - "render_route_cell and the IGES render ladder: render_iges is measured on IR16MiB (16777206 B), the first rung with ceil(1.5W)<=402"
  - "iges_memory_ceiling: the IGES input ceiling is lowered by a pre-registered ladder; 128 MiB passed first and was revoked by its own load-clean rep (2933 MiB), 120 MiB passes (2675 + 53.3 = 2728.3 <= 2867.2)"
  - "quiet_host: host load1 <= 11 at the start and end of every timed row; 163 rows on file excluded from W, missing reps re-measured under the gate"
  - "pod_per_route and bytes_output: the pod rule per route; RULE bytes governs the derived SVG/PDF output caps (SVG 134 MiB)"
  - "no_build: the final in-worker rerun used the existing api and cad-worker images, checked by id and by an empty source diff from d8ef383"

patterns-established:
  - "LADDER_RUNG/LADDER_RESULT lines for pre-registered descending ceilings, revocable by a NOTE when later reps fail the same test"

requirements-completed: []

duration: about 15 h wall (2026-10-14 11:50Z to 2026-10-15 00:40Z), including four STOPs and two waits for orchestrator decisions
completed: 2026-10-15
---

# Phase 76 Plan 03: In-worker rows and derived CAD bounds Summary

**Every CAD time bound is derived from measured rows taken at host load1 ≤ 11: engine 886 s (guard 21, mesh 324, render 521, io 20), DXF route sum 661 s, IGES input ceiling lowered to 125829120 bytes (120 MiB). The pod rule holds on every route, the worst being render_iges at 2741.9 MiB against 2867.2 (`SUMMARY,mode=derive76,derived=16,...,result=ok`).**

## The STOPs and the amendments that closed them

All amendments are RULE_AMEND lines in evidence/76-03-cells.txt, each committed before the next timed row. Every STOP line stays in its file.

| # | STOP | Amendment (orchestrator) | Commit of the rule |
|---|---|---|---|
| 1 | select-cells `route=render_iges,reason=no_eligible_cell` (every IGES render refused at R<=402) | render_remainder_all, render_route_cell (IGES render ladder), iges_memory_ceiling (IGES ceiling ladder), nothing_else | 548212f |
| 2 | eight-route in-worker run `watchdog_disk_below_threshold,free_gib=5` (partial-1, 17 rows, loaded host) | user-approved MinIO cleanup of the 76-03 jobs (6270767755 B), quiet_host, pod_per_route, bytes_output, rerun_eight_routes | 755edef |
| 3 | gated rerun `watchdog_disk_below_threshold,free_gib=5` before its first row (partial-2) | no_build (existing images, id and diff check); the user pruned unused build cache | c9fd441 |
| 4 | derive76 `pod_per_route` on convert_iges and render_iges (IM128MiB rep 121 peak 2933 + 54.2 = 2987.2) | none (B1 as written): NOTE ladder_rung_revoked, the ladder continued at IM120MiB | 57ee5d0 (NOTE) |

## Ladders (evidence/76-03-ladders.txt)

- IGES render ladder: IR16MiB fits at the first rung. Max render wall 75.63 s on the loaded run, ceil(1.5W)=114 against 402.
- IGES memory ladder:
  - IM128MiB (134217648 B): ladder fine peaks 2724–2764 MiB; the quiet reps reached 2933 MiB, so the pass was revoked.
  - IM120MiB (125829045 B), measured under the load gate: five clean fine reps peak 2576–2675 MiB, standard 2489, draft 2449. Go peaks 50.2–53.3 MiB, container peak up to 3149.3 MiB, no OOM. **pass**

## Quiet host re-measure (evidence/76-03-quiet.txt)

- 163 load-contaminated setter rows on file got one NOTE each: guard 35, mesh 51, render 21, dxf 14, rsvg_ink 15, rsvg_pdf 13, rsvg_png_transparent 14.
- 357 gated rows re-measured the missing reps, including M1 and M3 renders, the DXF chains, IR16MiB and IM128MiB. `QUIET_END,...,result=ok`.
- With the quiet rows whose own end load passed 11, derive excludes guard 36, mesh 51, render 28, dxf 23, rsvg_ink 19, rsvg_pdf 16 and rsvg_png_transparent 19, plus 4 in-worker rows.

W before and after (all rows → load-clean rows):

| Stage | All rows | Load-clean | Deadline |
|---|---|---|---|
| guard | 13.43 (stress_wide) | 13.43 | 21 |
| mesh | 308.15 (M3, load1 157.9) | 215.84 (M2) | 324 |
| dxf | 170.84 (C, load1 27.5) | 123.61 (0og on-record) | 186 |
| rsvg | 180.29 (L170030) | 144.54 (L170030, in-worker) | 217 |

## In-worker rows (evidence/76-03-inworker.txt, no-build, commit 7e4e84f, tracked_dirt=0)

| Route | Cell | Clean rows | Attempt wall (s) | Max nonstage (s) | Max go VmHWM (MiB) | Max container peak (MiB) |
|---|---|---|---|---|---|---|
| convert_step | M2 fine | 3/3 | 6.43–7.27 | 3.10 | 55.7 | 1275.3 |
| convert_iges | IM128MiB (above the new ceiling, walls only) | 3/3 | 88.24–89.43 | 1.71 | 54.2 | 3172.5 |
| convert_iges | IM120MiB (76-03-inworker-IM120MiB.txt) | 3/3 | 86.55–90.09 | 3.40 | 53.3 | 3149.3 |
| render_step | M2 draft iso opaque | 3/3 | 192.28–194.90 | 0.67 | 56.2 | 2049.8 |
| render_iges | IR16MiB | 3/3 | 63.06–64.27 | 0.70 | 66.9 | 1091.2 |
| render_stl | S1_k256 | 3/3 | 39.08–39.50 | 0.47 | 66.5 | 656.3 |
| dxf_svg | L170030 | 3/3 | 198.50–209.41 | 9.88 | 156.1 | 1969.3 |
| dxf_pdf | L170030 | 3/4 | 310.06–390.55 | 12.86 | 155.3 | 2717.1 |
| dxf_png | L170030 transparent 2048 | 3/3 | 329.30–355.95 | 11.29 | 150.7 | 1974.7 |

Every oom_kill is 0. One dxf_pdf rep ended at load1 14.61 and was repeated. Each row's own MinIO objects were deleted after it (MINIO_DELETED lines).

## Derivation (evidence/76-03-derived.txt), recomputed by hand

- io = ceil(1.5 × 12.86) = ceil(19.29) = **20**. The largest load-clean nonstage is the dxf_pdf rep with wall 390.55 s.
- render (remainder rule, 76-01 STACK_GLOBAL) = 886 − 21 − 324 − 20 = **521**.
- engine = max(21 + 324 + 521 + 20 = 886, 21 + 186 + 2×217 + 20 = 661) = **886**; pod grace 906.
- crosscheck: the largest ceil(1.5 × attempt wall) is 586 (dxf_pdf), at most 886 for all eight routes.
- pod per route (stage peak + go peak, limit 2867.2):

  | Route | Stage peak + go peak | Sum |
  |---|---|---|
  | convert_step | 1251 + 55.7 | 1306.7 |
  | convert_iges | 2675 + 54.2 | 2729.2 |
  | render_step | 1814 + 56.2 | 1870.2 |
  | render_iges | 2675 + 66.9 | 2741.9 |
  | render_stl | 1835 + 66.5 | 1901.5 |
  | dxf_svg | 1821 + 156.1 | 1977.1 |
  | dxf_pdf | 2566 + 155.3 | 2721.3 |
  | dxf_png | 1821 + 150.7 | 1971.7 |

  dxf_svg with go scaled to the 134 MiB SVG cap: 1821 + 234.5 = 2055.5.
- ceilings:
  - STEP 66060288, STL 190840832, DXF 123731968: unchanged.
  - IGES **125829120** (lowered from 141557760).
  - SVG cap 140509184 (134 MiB, from 93525876 B); PDF cap 19922944 (19 MiB).
- PATHB fired=0 for step, iges, stl and dxf. The IGES ceiling was lowered by iges_memory_ceiling, not by Path-B.
- GAP_CLOSED:
  - go_worker_rss_dxf: dxf_pdf 155.3 + 2566 = 2721.3.
  - dxf_top_1.26pct: R123731968 row peak 1086.
- KNOWN_LIMIT:
  - stacked_render_refused widened to render_cost_not_bounded_by_input_bytes. Smallest refused cells: step S6_n100 (598732 B), iges S5_32MiB, stl S1_k1024.
  - iges_ceiling_lowered_by_memory
  - iges_instancing_unprobed
  - arm64_only
  - quiet_host_bounds
  - short_stages_in_io
- ABOVE_CEILING: IM128MiB and M3 stay in the files for walls only.

## Deviations from Plan

1. **[Rule 4, orchestrator] Five amendment rounds** (table above). The plan's checks `! grep '^STOP,'` hold for 76-03-derived.txt, but the cells, ladders and partial in-worker files keep their STOP lines by design.
2. **[Rule 1] In-worker cells loop:** `docker compose exec` read the rest of the cells list from stdin, so the first run measured one route and still wrote SUMMARY ok. That file was overwritten and never committed. Fixed in 45c852b; SUMMARY ok now requires rows = routes × N.
3. **[Rule 1] Watcher timing:** stage start times use the /proc/uptime base instead of btime, because btime was 0.80 s off. Stages shorter than 0.2 s (cad-guard on small inputs) are not seen, so their time lands in nonstage and io.
4. **[Rule 1] Quiet-mode fixes:** the d76 tool was written too late (dff6492). A DXF chain's try limit now uses the largest missing count of the cell (a8af039). The run was restarted once while waiting at the load gate, recorded in `NOTE,key=quiet_harness_restart`. Rep labels from 101 repeat across the two quiet HDR sections.
5. **[Rule 1] TERM cleanup:** the harness now runs its cleanup on TERM (d301184). Once, after a watchdog TERM, the stack and client were cleaned up by hand; no job had been created.
6. **Selection vs. the plan's parentheticals:** the rule picks convert_step = M2 fine (not M1) and render_stl = S1_k256 (a margin rung, heavier than M4).
7. **Load-contamination reading:** a row on file is contaminated if its own load1 > 11, the preceding row's > 11, or the following row's (its end) > 11. This was pre-registered in the quiet_host line.
8. **Peaks keep every row:** memory peaks, output bytes and go peaks use every row, whether load-clean or not.
9. **Disk hygiene:**
   - The user approved deleting the 60 MinIO objects of the first 25 jobs. The final runs delete each job's own objects after its row (e20dac2).
   - Build-cache records this plan created were pruned by ID; the orchestrator pruned unused build cache once, at the user's approval.
10. **`git diff --name-only main -- cmd internal`** lists only the quick 261014-0lo judge files, which predate this plan. `internal/convert/exec.go` is byte-identical to main.

## Known Stubs

None.

## Threat Flags

None. The scripts add docker, MinIO (`mc` inside the octoconv minio container) and pmset reads; no product surface changes.

## Self-Check: PASSED

- Files exist: scripts/cad-inworker-measure.sh, scripts/cad-dxf-bounds.sh, evidence/76-03-derived.txt, 76-03-cells.txt, 76-03-ladders.txt, 76-03-quiet.txt, 76-03-inworker.txt, 76-03-inworker-IM120MiB.txt.
- Task 2 verify and Task 3 verify (the plan's commands) pass. The engine DERIVED line carries `ceilings=step:66060288;iges:125829120;stl:190840832;dxf:123731968`, and `KNOWN_LIMIT,key=stacked_render_refused` is present.
- 29 commits from 0efc9f7 to 99a67d0, all SSH-signed (G), with no Co-Authored-By trailer.
