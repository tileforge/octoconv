---
phase: 71-evaluation-gate
plan: 07
subsystem: infra
tags: [cad, ocp, vtk, pruning, keep-list, landlock, licences, libfreeimage, libraw, d-17, d-18]
status: complete

requires:
  - phase: 71-06
    provides: OCP pin 7.9.3.1.1 (PIN_DECISION by user), THREADS_DECISION lp=2 occt_parallel=off
  - phase: 71-05
    provides: user acceptance of VTK 9.7.1 + OSMesa
  - phase: 71-01
    provides: D-18 outcome "record", approved lock files
provides:
  - scripts/cad-measure/vtk_keeplist.py (collect from process maps, prune with refusal on a stale list)
  - scripts/cad-measure/vtk-keeplist.txt (77 generated entries, vtk_version=9.7.1; architecture-neutral after the PR 32 fix, see the addendum)
  - Dockerfile.cad-measure stages venv-pruned and venv-selected (ARG VENV_STAGE), variant mapping in common.sh
  - scripts/cad-image-trace.sh libs | prune-check | licences
  - scripts/cad-sandbox-measure.sh (candidate and minimal Landlock sets, content-judged sufficiency, effectiveness, removal table, --recheck)
  - evidence 71-07-libtrace.txt, 71-07-prune.txt, 71-07-licences.txt, 71-07-landlock.txt
affects: [71-08, 72, 73, 74]

actuals:
  tokens: 71000
  tasks: 3
  commits: 3
plan_head_before: b93ea8af433a94fc67215a8beb02a4b97ddc5c6a
plan_head_after: e13d540071c178eb11e32351208974365c545748

tech-stack:
  added: [strace (disposable trace container only, through scripts/sandbox-paths-measure.sh)]
  patterns: [keep-list generated from /proc/self/maps and checked by a byte-for-byte regeneration, prune in the venv stage before COPY, content signature instead of exit code, removal table to a minimal set]

key-files:
  created:
    - scripts/cad-measure/vtk_keeplist.py
    - scripts/cad-measure/vtk-keeplist.txt
    - scripts/cad-image-trace.sh
    - scripts/cad-sandbox-measure.sh
    - .planning/phases/71-evaluation-gate/evidence/71-07-libtrace.txt
    - .planning/phases/71-evaluation-gate/evidence/71-07-prune.txt
    - .planning/phases/71-evaluation-gate/evidence/71-07-licences.txt
    - .planning/phases/71-evaluation-gate/evidence/71-07-landlock.txt
  modified:
    - Dockerfile.cad-measure
    - scripts/cad-measure/common.sh

key-decisions:
  - "D-18 outcome record is implemented as measured: every file of cadquery_ocp_novtk.libs is loaded at import OCP, so nothing is removable; licences are an engineering record with an open, non-blocking lawyer flag"
  - "VTK is pruned in the venv stage before COPY (275 MiB, no whiteout layer); the keep-list is generated from the maps of all render branches, not written by hand"
  - "Landlock sets for the four helpers are candidates for phase 73, not grants; the minimal sets are the Python tree plus the multiarch library directory (plus /usr/share/fonts for cad_dxf)"

requirements-completed: [EVAL-01, EVAL-04]
---

# Phase 71 Plan 07: Library trace, VTK pruning, OCP licence record, Landlock matrix Summary

On the pinned image (OCP 7.9.3.1.1, native arm64, USER nobody) the VTK tree shrinks from 564 MiB to 289 MiB by a generated keep-list with 196 of 196 renders byte-identical to the full image, every OCP wheel library is shown to load at `import OCP`, the licences of the wheel libraries are recorded with a non-blocking lawyer flag, and the four helpers run under a real Landlock ruleset with content identical to an unconfined run on all 34 matrix inputs.

## Accomplishments

### Task 1: library trace and VTK pruning (`54e93fa`)

- **Trace** (`71-07-libtrace.txt`, 47 `LIBTRACE` lines): LD_DEBUG `file=` and `/proc/self/maps` per operation, one container per operation (`--network none --cpus 2 --memory 2g --user nobody`).
  - `import OCP` and every mesh (STEP, assembly, inch, cp1251, IGES, real FreeCAD IGES) load `libfreeimage` and `libraw` on both pins; `libjasper` only on 8.0.1.0.0. `ocp_libs_loaded` is 68 of 68 wheel files on 7.9.3.1.1 and 76 of 76 on 8.0.1.0.0, so `UNLOADED` is empty.
  - `stlstats`, all render branches, `dxf`, `rsvg-png` and `rsvg-pdf` load none of the three (`D18_TRACE,ocp=7.9.3.1.1,ops=38,freeimage_ops=7,libraw_ops=7,libjasper_ops=0`; `ocp=8.0.1.0.0,ops=9,...,libjasper_ops=7`).
  - `EXPECTATION,research=V3-V7,match=1`: the research measurement holds, so no checkpoint was needed.
  - Chain by `NEEDED` lines (ELF DT_NEEDED parsed in the image): `OCP.so -> libTKService -> libfreeimage -> libraw`, and on 8.0 `libraw -> libjasper`.
- **Keep-list**: `vtk_keeplist.py collect` over the maps of 17 render runs (seven views, transparent, 512, 2048, lp 1, lp 2, three degenerate parts, the empty-STL branch) gives 77 `vtkmodules/` paths; regenerating from the same maps is byte-identical (`KEEPLIST_REGEN,same=1,entries=77`).
- **Prune** (`71-07-prune.txt`): `PRUNE_SIZE` vtk 564.1 -> 288.7 MiB, image 1358.3 -> 1083.0 MiB; the two differences agree within 0.1 MiB (`whiteout_free=1`). `PRUNE_IDENTITY,pngs=196,identical=196,different=0,failed=0` over all 21 meshed STEP/IGES parts, the 4 corpus STLs (seven views each, plus four branch renders on the real part) and the 17 fetched real files (iso). `PRUNE_OTHER,rows=48,same=48,diff=0` (mesh, stlstats, dxf, rsvg-png against the full image of the same build, run now).
- **Stale keep-list**: `STALE_TEST,case=version,result=detected` ("keep-list stale: generated for vtk 9.7.0, installed 9.7.1", rc 1); `case=missing_module,result=detected` (keep-list without vtkFiltersCore: prune succeeds, render under nobody fails with `No module named 'vtkmodules.vtkFiltersCore'`). The Task 1 second gate printed `STALE-REFUSED`.

### Task 2: D-18 outcome record (`b5d0f9e`)

- Outcome read from `71-01-SUMMARY.md`: `record`. `D18_OUTCOME,choice=record,pin=7.9.3.1.1,removed=none,reason=loaded_at_import,experiment=not_run,lawyer_ok=needed`; the image is unchanged and there is no `venv-nofreeimage` stage.
- `licences` inventories 68 + 76 wheel files (`WHEEL_LIB`, with component, SBOM version, `loaded`), 19 + 25 SBOM components (`SBOM_FIELD`: the `licenses` field is empty on all), and 23 `LICENCE` rows. All carry an upstream source and `VERIFIED` (expected licence text found at the URL at fetch time); none is `ASSUMED` after two pattern fixes (libuuid, jbigkit).
- OCCT: LGPL-2.1 plus "Open CASCADE exception (version 1.0)", read at tags V7_9_3 and V8_0_1; the row states the essence (object code incorporating OCCT header material may be distributed under terms of your choice with a prominent notice).
- `RECORD` block: loading facts, "только внутреннее использование, образ не распространяется вовне", assumption A2 named as an assumption, `LAWYER_OK=needed,blocking=no`. No line says the licences are cleared; VERIFIED identifies a licence and does not clear a use.
- 7.9.3.1.1 has no `libjasper` file; JasPer has a row from the 8.0.1.0.0 inventory.

### Task 3: Landlock matrix (`e13d540`)

34 inputs over four helpers (9 cad_mesh, 10 cad_render, 5 cad_dxf, 10 rsvg-convert), production argv (`/opt/venv/bin/python -I -B ...`), container environment only `HOME`, `LANG`, `PROBE_*`, no `--dump-maps`, no LD_DEBUG (the 42 `ARGV` lines contain neither).

| Helper | Minimal ro set (candidate for phase 73, not a grant), rw=/job |
|---|---|
| cad_mesh | /opt/cad-measure /opt/venv /usr/bin/python3.11 /usr/lib/aarch64-linux-gnu /usr/lib/python3.11 |
| cad_render | the same five |
| cad_dxf | the same five plus /usr/share/fonts |
| rsvg-convert | /usr/bin/rsvg-convert /usr/lib/aarch64-linux-gnu |

- `LANDLOCK_SUMMARY,helpers=4,inputs=34,sufficient=34,different=0,leaked=0,removal_rows=58`: every input is `content=identical` under the candidate and under the minimal set; renders also pass pixelcheck.
- Removed by the removal table (so the trace over-reports them, as 71-02 noted): `/etc/ld.so.cache`, `/etc/nsswitch.conf`, `/etc/passwd`, `/usr/lib/locale`, `/usr/share/zoneinfo`, `/usr/local/share`, `/sys/devices/system/cpu`, `/proc/cpuinfo`, `/proc/filesystems`, `/proc/self/maps`. No set needs `/proc`, `/sys` or `/tmp` (`TMP_NEED,...,none`, `PROC_SYS,...,entries=none`).
- `/usr/share/fonts` is load-bearing for cad_dxf in a silent way: without it `legacy_cp1251.dxf` exits 0 with a different SVG (`DIFFERENT/rc0`), which is why sufficiency is judged by content.
- Effectiveness: all four `EFFECT` lines `outside_unconfined=ok,outside_confined=denied` for both sets. For cad_dxf and rsvg-convert the confined log carries a permission error; for cad_mesh and cad_render the confined helper exits 2 with no permission text, and the unconfined control on the same file succeeded.
- `--recheck`: `RECHECK-OK` for all four helpers on the current image.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Exit 3 of cad_render is unreachable from an STL**
- **Found during:** Task 1 (libs trace)
- **Issue:** the plan asks for a branch with exit 3 (zero-size geometry). `vtkSTLReader` drops every degenerate triangle (NaN, inf, three equal points), so every such input ends at exit 2; a collinear or 1e-30 triangle renders with exit 0.
- **Fix:** the zero-extent input is recorded as `render-exit3-unreachable` with the observed rc 2 and a `NOTE` line; the imports at the top of `render()` are the same on both exits, so the maps do not change.
- **Files modified:** scripts/cad-image-trace.sh
- **Commit:** 54e93fa

**2. [Rule 3 - Blocking] `/proc/<pid>/maps` in the rsvg-convert trace**
- **Found during:** Task 3
- **Issue:** the trace recorded `/proc/54/maps`; the ruleset build failed in the probe ("open /proc/54/maps: no such file or directory") because the pid differs under the probe.
- **Fix:** the collapse step rewrites `/proc/<digits>/` to `/proc/self/`; the entry is removable in the minimal set anyway.
- **Files modified:** scripts/cad-sandbox-measure.sh
- **Commit:** e13d540

**3. [Rule 1 - Bug] Script defects fixed before the recorded runs**
- A loop variable `h` in `trace_helper` clobbered the caller's helper name; the `RETRY` lines printed inside command substitutions leaked into `REMOVAL` details; the recheck parsed `,note=` into the ro set. All three were found on the first runs and fixed; the recorded evidence comes from runs after the first two fixes. The third (recheck parse) was fixed after the main run and does not touch the main path; the recheck itself was run after the fix.
- **Files modified:** scripts/cad-sandbox-measure.sh
- **Commit:** e13d540

### Plan wording versus what was done

- **`text_fonts.dxf` is not in the corpus** (as 71-04 noted); `columns_R2007.dxf` (MTEXT) covers text, and `dimension_in_nested_blocks.dxf` is the DIMENSION input.
- **DT_NEEDED chain** is read by a small ELF parser run in the image (exact direct edges, `from` and `to`) rather than `ldd` (which flattens the tree); `ldd` is kept as a count (`LDD` lines).
- **Library flags** use LD_DEBUG `file=` names and the maps file separately (`fi_ld`, `fi_maps`, ...); `freeimage` is their union. All 17 libs-run render maps are kept with a counter, because the first run overwrote the seven views into one file.
- **TRACE lines** list the paths outside `/opt/venv`, `/opt/cad-measure` and `/usr/lib/python3*` and give a count for the Python tree (about 190 to 220 `.pyc` and module files per input), to keep the evidence readable; the full lists were used to build the candidates.
- **The patchelf branch of Task 2** is not implemented: the recorded outcome is `record`, and `licences` exits 64 with a message if the outcome line ever says `patchelf`. The `nofreeimage` variant that `common.sh` mapped to a `venv-nofreeimage` stage never had that stage and was removed after the PR 32 review (see the addendum); the patchelf branch was not taken.
- **Pruning check inputs**: the fetched files are 17 (six STEP/IGES, four NIST, seven NASA STL), all rendered iso at 4g.

## Auth Gates

None. All three commits were signed (1Password SSH, `%G? = G`).

## Known Stubs

None.

## Threat Flags

None beyond the plan's register: strace is installed only in the disposable trace container (T-71-SC, T-71-20); pip was not used; the licence fetches are read-only HTTPS GETs of upstream licence pages (T-71-19, every row has a source and a status).

## Notes for plans 71-08 and 73

- Disk: free space went from 13 to 12 GiB. No image or cache was deleted. Five new `octoconv-cad-measure` tags exist from this plan (`full` on both pins under two input hashes, `pruned` on 7.9.3.1.1); they share layers.
- A lawyer review remains open and does not block (`LAWYER_OK=needed,blocking=no`); jbigkit (GPL-2.0, in the 7.9.3.1.1 wheel) and libgomp (GPL-3.0 with the GCC runtime exception) are copyleft components named in the record beside FreeImage and LibRaw.
- Pruned image size is 1083.0 MiB uncompressed on this host; cold pull and total image size are measured in 71-08.
- Candidate sets are derived with `LP_NUM_THREADS=2` passed by `--lp-threads` and `--parallel off`; with different threading settings `/sys/devices/system/cpu` and `/proc/cpuinfo` may become necessary, and `/usr/share/fonts` should be revisited if the font package set changes.

## Addendum: PR 32 review (keep-list architecture, nofreeimage variant)

- **The keep-list named arm64 paths.** The committed list required `vtkmodules/<mod>.cpython-311-aarch64-linux-gnu.so`, and `vtk_keeplist.py prune` exits on the first absent entry, so no amd64 build could pass the prune. `vtk_keeplist.py` now drops the `.cpython-<n>-<arch>-linux-gnu` tag when collecting and when matching (`normalize`), and the committed `vtk-keeplist.txt` carries `vtkmodules/<mod>.so` (77 entries, 23 renamed, `vtk_version=9.7.1`). A version mismatch is still refused and a listed module that is absent from the tree still exits 1 with `keep-list stale: missing ...`; an old arch-specific list still matches its own architecture.
- **New image tags** (OCP 7.9.3.1.1, native arm64): `octoconv-cad-measure:ocp7.9.3.1.1-pruned-dc621b94ee5d` (image id `08117b26d4d0`) and `...-full-dc621b94ee5d`, built with `common.sh build` one after the other; the `...-0d09b7ba39ad` images were not deleted. `libs` was re-run: the regenerated list from the real arm64 maps is byte-identical to the normalized committed list (`KEEPLIST_REGEN,same=1,entries=77,maps_files=17`). `prune-check` was re-run on the new tag: `PRUNE_SIZE` unchanged (564.1 -> 288.7 MiB, image 1358.3 -> 1083.0 MiB, `whiteout_free=1`), `PRUNE_IDENTITY,pngs=196,identical=196,different=0,failed=0`, `PRUNE_OTHER,rows=48,same=48,diff=0`, both `STALE_TEST` cases detected; the plan gates `PRUNE-OK` and `STALE-REFUSED` pass on the new image.
- **Identity against the previous pruned image** (`scripts/cad-prune-identity.sh`, `evidence/71-07-prune-arch.txt`): iso renders of five real parts (LinearSlide-MGN15x-Block, LM8UU, SC8UU, V-Slot-20x20-100mm, 2020_corner_bracket-Corner; 1280, white, `LP_NUM_THREADS=2`) are byte-identical, `IDENTITY,parts=5,identical=5,different=0,failed=0`. The tree hash of `/opt/venv` and `/opt/cad-measure` (without `vtk_keeplist` and the helper pyc files) is equal (`TREE_SHA`), and the image is 1135589363 bytes against 1135587708 (+1655 bytes, the source of `vtk_keeplist.py`), 6 layers in both (`IMAGE_BYTES`).
- **amd64** (`scripts/cad-prune-amd64-check.sh`, `evidence/71-07-prune-amd64.txt`): built under OrbStack emulation in about two minutes, so no fallback was needed. With the previous list the build is refused (`keep-list stale: missing vtkmodules/vtkCommonCore.cpython-311-aarch64-linux-gnu.so`); with the new list `PRUNED,kept=77,removed=299,bytes_removed=279846920` and the import check of the final stage passes (`image_arch=amd64`). This is a build proof; an emulated image is still not a measurement. `scripts/cad-vtk-keeplist-test.py` additionally covers the matcher over synthetic arm64 and amd64 trees (`KEEPLIST_TEST,cases=10,failed=0`), including a synthetic amd64 tree pruned with the committed list.
- **`nofreeimage` removed.** `common.sh` no longer lists the variant (`build OCP nofreeimage` exits 64 as an unknown variant), `scripts/cad-image-size-measure.sh` no longer looks for such an image, and the `licences` message for a `patchelf` outcome no longer points at a stage. The D-18 outcome stays `record`; the patchelf branch was not taken.

## Self-Check: PASSED

- Files present: scripts/cad-measure/vtk_keeplist.py, scripts/cad-measure/vtk-keeplist.txt, scripts/cad-image-trace.sh, scripts/cad-sandbox-measure.sh, Dockerfile.cad-measure (stages venv-pruned, venv-selected), evidence 71-07-libtrace.txt, 71-07-prune.txt, 71-07-licences.txt, 71-07-landlock.txt.
- Commits present: 54e93fa, b5d0f9e, e13d540 on gsd/phase-71-evaluation-gate; `git rev-list --count b93ea8a..e13d540` is 3.
- Gates: `PRUNE-OK` and `STALE-REFUSED` (Task 1), `D18-OK` (Task 2), `LANDLOCK-OK` and `RECHECK-OK` (Task 3).
- `git diff --name-only 4e595c0 -- internal cmd deploy docker-compose.yml | grep -v '^internal/convert/testdata/cad/'` is empty; STATE.md and ROADMAP.md untouched.
