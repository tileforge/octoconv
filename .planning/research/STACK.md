# Technology Stack — v1.16 CAD Engine (tenth engine class `cad`)

**Project:** OctoConv
**Researched:** 2026-10-02
**Scope:** only what the NEW cad class needs. Existing nine classes, `engineExec`, Landlock, registry, `role` enum, closed opts are taken as given.
**Overall confidence:** HIGH for everything marked "measured" (run on this machine in Docker, arm64 native, amd64 under Rosetta; scratch evidence listed at the bottom). MEDIUM for version/availability facts from PyPI/GitHub APIs. LOW where only a web page was summarised (flagged inline).

## Executive Verdict

| Question | Verdict |
|---|---|
| Do we need full FreeCAD? | **No.** FreeCAD's STEP/IGES kernel *is* OCCT. Measured side by side on the same STEP: identical volume (858.6283305884542), identical binary STL (6084 B at lin 0.1 / ang 0.5). FreeCAD costs 1.56 GiB (trixie) / 1.88 GiB (bookworm) of installed packages (289-350 packages, Qt + VTK + boost + pyside) vs ~250 MiB for OCP. Its GUI offscreen path does not render headless. |
| Does OCCT alone suffice? | **Yes, via the `cadquery-ocp-novtk` wheel** (self-contained OCCT, Python bindings, cp311/cp312/cp313, manylinux_2_28 aarch64 **and** x86_64, 61-67 MB wheel). Reads STEP/IGES, exact volume (`BRepGProp`), exact bbox (`BRepBndLib.AddOptimal`), tessellates (`BRepMesh_IncrementalMesh`), writes STL. Debian ships no pythonocc/OCP package in bookworm or trixie, so pip wheel with hashes is the only pinned route. |
| Headless render without X? | **Yes: VTK 9.7.1 PyPI wheel + Debian `libosmesa6`, instantiating `vtkOSOpenGLRenderWindow` directly.** Measured as uid `nobody`, `env -i`, no X, no `/dev/dri`, no `/tmp`: 0.8-1.3 s end to end for a small part, 1.7 s for a 550k-triangle mesh, arm64 native and amd64 (emulated), bookworm and trixie. Trace shows it reads only libs + `/sys/devices/system/cpu` + `/proc/cpuinfo`/`/proc/sys` — **the same shape as the libvips grant set**. No Xvfb, no sockets. |
| dxf to svg/pdf/png? | **ezdxf 1.4.4 (MIT) native SVG backend, then `rsvg-convert` (Debian `librsvg2-bin`, LGPL) for pdf and png.** Measured 0.4 s + 0.16 s. Avoid PyMuPDF (AGPL) and LibreCAD/qcad (Qt, GUI stack, no stable headless export). |
| Base image? | **Stay on `debian:bookworm-slim`.** Everything recommended is a pinned pip wheel + a handful of bookworm apt packages; verified working on bookworm (python 3.11.2, mesa 22.3.6, librsvg 2.54.7). Distro-packaged FreeCAD/f3d/ezdxf on bookworm are too old (0.20.2 / 1.3.1 / 0.18.1) and trixie would be a one-off base only for FreeCAD, which we do not need. |
| Go-side new dependencies? | **None.** The class adds Python helper scripts executed through `engineExec`; Go stays CGO-free. |
| Largest image yet? | Yes: est. **~1.1 GB uncompressed** after pruning VTK (measured 1.42 GB unpruned). Existing max is document-worker 769 MB. FreeCAD route would be >=2 GB. |

## Recommended Stack

### Engine runtime (new image `Dockerfile.cad-worker`, `debian:bookworm-slim`, `USER nobody`)

| Technology | Version (pin) | Purpose | Why |
|---|---|---|---|
| Python (Debian) | 3.11.2 (`python3`, `python3-venv`; bookworm) | Interpreter for 3 helper scripts | Wheels below all ship cp311; no pip in the final image (venv built in builder stage and copied; delete `pip`/`setuptools` from it). |
| `cadquery-ocp-novtk` | **7.9.3.1.1** (OCCT 7.9.3, 2026-05-28) default; **8.0.1.0.0** (2026-09-05) = eval candidate | STEP/IGES read, exact volume + bbox, tessellation, STL write | Same kernel as FreeCAD at ~1/6 the weight. Both versions run the full pipeline and produce byte-identical STL on the test part. 7.9.3 is the mature line; 8.0 is a major release <1 month old and changes bindings (`Bnd_Box.Get()` is unusable in 8.0.1 -> use `CornerMin()/CornerMax()`; write the helper that way and it works on both). Use `-novtk`: the plain `cadquery-ocp` pulls in its own VTK. |
| `vtk` (PyPI wheel, `pip install --no-deps`) | **9.7.1** (2026-09-28; aarch64 129 MB, x86_64 140 MB) | Offscreen shaded render of STL to PNG | Since VTK 9.4 one wheel falls back to EGL/OSMesa at runtime, so no `vtk-osmesa` variant. `--no-deps` avoids dragging matplotlib (VTK declares it as a requirement). Installed size 566 MB; **pruning every `.so` not opened by the real render path cuts it to 290 MB with a byte-identical PNG** (measured). |
| `libosmesa6` (apt) | 22.3.6 (bookworm) | Software GL (llvmpipe) for VTK | Needs nothing from X, `/dev`, `/tmp`. Pulls LLVM (~130 MB) but `libgl1` (needed by OCP, see below) already pulls the same Mesa/LLVM stack, so marginal cost is ~15 MB. |
| `libgl1`, `libx11-6` (apt) | bookworm | Load-time dependency of OCP | OCP's `TKOpenGl` links `libGL.so.1`/`libX11.so.6`; **`import OCP` fails without `libGL.so.1`** (measured). They are never used at runtime. |
| `ezdxf` | **1.4.4** (2026-05-14; MIT; cp311 aarch64+x86_64 wheels) | DXF read, 2D bbox (`ezdxf.bbox`), `$INSUNITS`, SVG export via `ezdxf.addons.drawing` `svg.SVGBackend` | Native SVG backend (no Qt/matplotlib), text is emitted as paths so downstream tools need no fonts. Bookworm's `python3-ezdxf` is 0.18.1 (pre-SVG-backend) - use the wheel. |
| `pillow` | latest (pin by hash) | Import-time requirement of `ezdxf.addons.drawing` | `ModuleNotFoundError: PIL` on a bare `pip install ezdxf` (measured). Easy to miss. |
| `numpy`, `fonttools`, `pyparsing`, `typing_extensions` | resolved by pip, pinned by hash | ezdxf deps | `pip install --require-hashes -r requirements.txt` with the full resolved set. |
| `trimesh` | 5.1.0 (MIT, pure python) | STL-source stats: bounds, `is_watertight`, tessellated volume | Used only when the *source* is STL (no B-rep). Note tessellated volume is approximate (858.86 vs exact 858.63 on the test part), which is why STEP/IGES volume must come from the B-rep, never from the STL. Hand-rolled numpy signed-tetra sum is an equally valid replacement. |
| `librsvg2-bin` (apt) | 2.54.7 (bookworm) | `rsvg-convert -f pdf` and `-f png -w 1280 -h 1280 --keep-aspect-ratio -b white` | 0.16 s per conversion, page size taken from the SVG's `mm` width/height, LGPL, ~70 MB with cairo/pango. Gives dxf->png "render" for free. |
| `fonts-dejavu-core` (apt) | bookworm | Text in DXF (ezdxf's font manager refuses to run with no fonts) | Without any font: `no fonts available, not even fallback fonts` and text is dropped. The Landlock set then needs `/usr/share/fonts` (same lesson as ocr/probe). |

### Not installed on purpose

`freecad*`, `f3d` (Debian), `xvfb`, `librecad`, `inkscape`, `gmsh`, `blender`, `occt-draw`, `python3-vtk9`, PyMuPDF, matplotlib, pyrender, ODA File Converter, `libredwg-*` (absent from bookworm and trixie; DWG is out for licence reasons anyway).

## Render candidates for the evaluation phase (ranked)

All numbers are from this session's prototype unless marked "not measured".

| Rank | Candidate | Status | Cost | What the eval phase must measure before choosing |
|---|---|---|---|---|
| **1** | **VTK 9.7.1 wheel + OSMesa**, helper script instantiates `vtkOSOpenGLRenderWindow` (or sets `VTK_DEFAULT_OPENGL_WINDOW` in-process before importing; both verified under `env -i`) | Works: arm64 native, amd64 emulated, bookworm + trixie, as `nobody`, no X. PNG bytes differ between arches (25174 vs 25172 B) - llvmpipe is not bit-reproducible across CPUs | +290 MB pruned / +566 MB unpruned; ~15 MB extra Mesa | (a) `strace -ff` grant set on the real image via `scripts/sandbox-paths-measure.sh` incl. execve of `/usr/bin/python3.11` (venv `python` is a symlink; trap 1 and 7 of SANDBOX-PATHS.md), then `verify` with content check (a blank white PNG has the right size - judge by pixel stats, not rc). (b) Camera framing: prototype used `ResetCamera()`, which cropped the part's right edge; use a bbox-fit with margin per view; closed vocabulary (iso, front, back, left, right, top, bottom) via `SetPosition/SetViewUp` with **Z-up**. (c) Quality: add `UseFXAAOn()` or supersample, feature edges (`vtkFeatureEdges`) and a fixed light kit; judge visually on 3 real parts. (d) `LP_NUM_THREADS`: llvmpipe sizes its thread pool from host cores (11 here), not the cgroup quota; set it inside the helper (engine env is an allowlist, so env vars from the worker never reach it) to the pod's CPU limit. (e) Time/RSS on a 500k+ triangle mesh at 1280x1280 (measured once: 1.7 s on 11 cores; re-measure at `--cpus=2`). (f) Tests must not byte-compare PNGs across architectures; compare dimensions + pixel statistics. |
| 2 | **f3d 3.5.0 official `Linux-x86_64` tarball** (2026-04-05, 58 MB xz / 306 MB installed) with `--rendering-backend=egl\|osmesa` | Works **on amd64 only** (tested emulated, as non-root, no X): both backends rendered a 1280x1280 PNG in 4-5 s under emulation. **Gotcha found:** f3d `dlopen`s the *unversioned* `libEGL.so` / `libOSMesa.so` (verified in `library/src/context.cxx`), which Debian only ships in `-dev` packages; without `ln -s libOSMesa.so.8 libOSMesa.so` every backend reports "unavailable" / "Cannot find EGL library". No official aarch64 Linux build exists (releases list x86_64 only; the PyPI `f3d` wheel is x86_64 only) | +306 MB; arm64 needs a from-source VTK+f3d build (not measured; expect long builds and a pinned-source maintenance burden) | Only relevant if (1) fails on visual quality. Measure native amd64 time and a `--camera-*` mapping for the closed vocabulary (`--camera-direction`, `--camera-view-up`, `--up=+Z`; defaults draw a grid, axes and filename - need `--no-config` and the overlay-off flags). Decide whether the arm64 dev loop can live with a different renderer than prod (it should not). |
| 3 | **Own CPU rasteriser** (numpy z-buffer in the helper, or a small Go program) | Not measured. No GL, no Mesa/LLVM, no VTK; deterministic across arches; grant set = job dir only | ~0 MB, but real code to write and a quality ceiling (flat/Gouraud shading, simple AA) | Build only if (1)'s image size or arch non-determinism is judged unacceptable. Measure triangle throughput at 1280x1280 with 2x2 supersampling and visual quality vs (1) on the same three parts. |
| 4 | Debian `f3d` 3.1.0 (trixie) + `xvfb-run` | Works (1.7 s) **but only under Xvfb**. With `libegl1`/`libosmesa6`/dev symlinks and no X: `egl` "Cannot use a EGL context on this platform", `osmesa` "Window type is OSMesa but the underlying VTK version is not recent enough" - Debian's VTK 9.3.0 is built GLX-only. bookworm's f3d 1.3.1 predates `--rendering-backend` (added in f3d 3.0) | 798 MiB apt delta (trixie) + Xvfb 211 MiB | **Reject.** Second process + `/tmp/.X11-unix` socket (the one thing Landlock cannot grant to this class; only `document` holds `sock:`). |
| 5 | FreeCAD GUI offscreen (`QT_QPA_PLATFORM=offscreen`, `View3DInventor.saveImage`) | Fails in prototype: `QOpenGLWidget: Failed to create context` | Needs the full 1.5-1.9 GiB FreeCAD | **Reject.** |
| 6 | pyrender + OSMesa; matplotlib `Poly3DCollection` | Not measured. pyrender's last release is 2021-02 (v0.1.45); matplotlib 3D has no real z-buffer and is slow past ~50k triangles | small | Do not evaluate unless 1-3 all fail. |
| - | Blender | Rejected on size (hundreds of MB) and GL requirement; not tested | | |

**Recommendation to the evaluation phase:** run (1) first; (2) only as a visual-quality yardstick and amd64 fallback; (3) only if (1) fails its size/determinism gate. Keep OCP and VTK in **separate processes** (mesh helper, then render helper): each gets its own Landlock ruleset like `document`'s soffice/veraPDF pair, the render ruleset carries no OCP, and a crash in one cannot take the other's result.

## Pipeline mapping (what runs for each requested output)

| Request | Process chain (each exec through `engineExec`, own ruleset) | New role/opts implications |
|---|---|---|
| STEP/IGES -> STL | `python cad_mesh.py` (OCP: read, mesh, write STL) | opts: closed `quality` enum (below) |
| STEP/IGES -> render png + measure json | `cad_mesh.py` writes `part.stl` **and** `measure.json` (exact B-rep volume + bbox, same process, one STEP read) -> `cad_render.py part.stl` -> `render.png` | second output = `role` for the JSON; reuse the same STEP parse for both so the 1.2 GB peak is paid once |
| STL -> render png + measure json | `cad_render.py` (VTK) + `cad_stats.py` (trimesh: bounds, watertight; volume only when watertight, otherwise null) | units: STL has none -> report `units: null` |
| DXF -> svg | `python cad_dxf.py` (ezdxf SVG backend) | |
| DXF -> pdf / png | `cad_dxf.py` -> `/usr/bin/rsvg-convert -f pdf\|png` | png size from the same image-size opt as 3D render |
| DXF -> render png + measure json | same as DXF->png; json = 2D bbox (`ezdxf.bbox.extents`), `$INSUNITS` mapped to a unit name or null | no volume; bbox z is 0 |

STEP/IGES length units: OCCT converts to millimetres on read by default (`xstep.cascade.unit` = MM; per OCCT docs - LOW, doc page summarised - consistent with measured box 20x10x5 -> 1000 mm^3 before the hole). So for STEP/IGES the JSON can honestly say `mm`/`mm^3` *after conversion*; if the source unit must also be echoed, parse the STEP header (`LENGTH_UNIT ... SI_UNIT / CONVERSION_BASED_UNIT`) - not measured. DXF units come only from `$INSUNITS` (0 = unspecified -> null).

## STEP/IGES to STL meshing knobs

Engine: `BRepMesh_IncrementalMesh(shape, linearDeflection, isRelative, angularDeflection, inParallel)` then `StlAPI_Writer` (binary). FreeCAD's `MeshPart.meshFromShape(Shape, LinearDeflection, AngularDeflection, Relative)` is the same mesher (identical STL bytes at the same parameters, measured), so nothing is lost by dropping FreeCAD.

| Knob | Meaning | Guidance |
|---|---|---|
| Linear deflection | max chord distance (model units = mm after read) | **Derive from the bbox diagonal, not a constant**: `lin = k * diag`. A constant is wrong for both a 2 mm part and a 2 m part. Starting `k`: draft 0.004, standard 0.001, fine 0.0003 (to be tuned in the eval phase on real parts; the test part had diag 22.9, lin 0.1 = 0.0044 and lin 0.01 = 0.00044 gave 6084 B and 13484 B). Prefer this over OCCT's `Relative=True`, which scales per edge/face and is inconsistent on mixed-size assemblies. |
| Angular deflection | max angle between adjacent facets, **radians** (0.5 rad = 28.6 deg is the OCCT default; the fetched docs page says "degrees" - the API takes radians, confirmed by the 0.5 default producing the same STL in FreeCAD and OCP) | draft 0.5, standard 0.35, fine 0.2 as starting points. |
| `inParallel` | multi-thread meshing | Keep on; it uses all host cores regardless of cgroup quota - cap via `OMP`/`TBB`-style env is not available (allowlist env), so decide after measuring at `--cpus=2`. |
| `MinSize`, `InternalVerticesMode`, `ControlSurfaceDeflection` (`IMeshTools_Parameters`) | fine control | Leave at OCCT defaults. Expose only the single `quality` enum (draft/standard/fine) in the closed opts; never raw floats from clients. |

Scale datapoint (synthetic, **single sample, one machine**): 3000 copies of the test part as one compound = 66.7 MB STEP -> 15.7 s read + exact volume + bbox + mesh + STL write on 11 cores; **peak RSS 1.2 GB (~18x the file size)**; 27.6 MB STL (~550k triangles); volume summed exactly (2,575,884.99 = 3000 x 858.63). Implications: set a STEP size cap well below `MAX_UPLOAD_BYTES` generic limits, give the pod >= 2 GiB, and **measure `CAD_ENGINE_TIMEOUT` the way v1.8-v1.11 did** on real-world files, not synthetic ones. No real CAD corpus was available offline here; the eval phase must source one (public-domain STEP sets exist; not verified in this session).

## Landlock / `engineExec` integration (candidate grant sets from a prototype trace)

Trace method: `strace -ff` run as root wrapping `setpriv` to `nobody`, `env -i HOME=/job/home`, prototype image (trixie/arm64/python 3.13). **These are hypotheses for `scripts/sandbox-paths-measure.sh` on the real bookworm image, not final sets.** Paths under `/opt/venv` and the python stdlib are read-only trees.

| Helper | Read-write | Read-only (beyond `/lib`, `/usr/lib`, `/opt/venv`, `/etc/ld.so.cache`) | Notes |
|---|---|---|---|
| `cad_mesh.py` (OCP) | job dir | `/proc/sys/vm/overcommit_memory`, `/proc/self/status`-style entries, `/sys/devices/system/cpu/{online,possible}`; **add by hand** `/usr/bin/python3.11` (execve is invisible in `openat`) and the symlink targets of `/opt/venv/bin/python` | No fonts, no `/tmp`, no `/etc` beyond ld cache (fontconfig libs are bundled in the wheel but were not opened). OCCT prints transfer statistics to **stdout**: always write results to files and ignore stdout. |
| `cad_render.py` (VTK, OSMesa) | job dir | `/proc/cpuinfo`, `/proc/sys/vm/overcommit_memory`, `/sys/devices/system/cpu` (llvmpipe reads `cpu*/cpu_capacity`) | **No** `/dev`, `/tmp`, `/etc/glvnd`, `/usr/share/glvnd`, `/usr/share/drirc.d`, X socket. If the EGL window class were used instead, those glvnd/drirc paths and a harmless `/dev/dri` ENOENT probe appear: **choose OSMesa, it needs strictly less.** |
| `cad_dxf.py` (ezdxf) | job dir (+ `$HOME` inside it) | `/usr/share/fonts`, `/usr/local/share/fonts` | ezdxf writes `~/.cache/ezdxf/font_manager_cache.json` on first use; set `HOME` to a job-dir subpath (the allowlist already forces `HOME`) or pre-seed the cache at image build and make it read-only. Rebuild-per-job cost was not isolated (whole script 0.39 s). Ensure no `matplotlib` import so no `MPLCONFIGDIR` need. |
| `rsvg-convert` | job dir | `/usr/bin/rsvg-convert`; libs only | `/proc/self/maps`, `/proc/filesystems` observed. SVG has no embedded text (paths), so no font grant needed for it. Do not pass `--unlimited`; keep the default libxml2 entity/size limits since the SVG is produced by us from untrusted DXF. |

General:
- **Env allowlist** (`PATH/HOME/LANG/LC_ALL/TMPDIR`) means anything the helper needs (`VTK_DEFAULT_OPENGL_WINDOW`, `LP_NUM_THREADS`, `PYTHONDONTWRITEBYTECODE`, `PYTHONHASHSEED`) is set **inside the script** or via `engineExec.withEnv` if that is allowed for the class. Run python as `python -I -B` and precompile `.pyc` at build time (read-only venv).
- No class here needs `/tmp` -> no shared-`/tmp` guard (D-01..D-04 of CONF-06) is triggered; concurrency is bounded by memory, not by the guard. Memory (STEP at ~18x) is the limiter: default worker concurrency 1 until measured.
- Python helpers are **parsers of untrusted STEP/IGES/DXF**: OCCT and ezdxf have a history of crash/hang bugs on malformed input. Landlock + `Setpgid` kill + timeout is the right envelope; add magic-byte validation first (STEP `ISO-10303-21;`, IGES column-73 `S`/`G`/`D`/`T` records, ASCII DXF `0\nSECTION` / binary DXF sentinel `AutoCAD Binary DXF`, STL ASCII `solid` vs binary size-consistency check - an ASCII STL that starts with `solid` can be binary).
- Worker preflight must run the *real* helpers on a tiny STEP and DXF fixture (generated at build time with OCP/ezdxf), prove a file outside the job dir is refused, and judge by content (non-blank PNG pixel stats, JSON volume == 1000-141.37), not by return code.

## Image size estimate

| Layer | Measured / estimated (uncompressed) |
|---|---|
| `debian:bookworm-slim` | ~75 MB image |
| python3 + venv module | ~30 MB |
| libgl1 + libx11 + libosmesa6 (+ Mesa/LLVM) | ~215 MB (measured on trixie 204-208 MiB; bookworm similar) |
| librsvg2-bin + fonts-dejavu-core | ~70-90 MB (88 MiB measured together with poppler on trixie; poppler not needed here) |
| OCP wheel installed (`OCP` + `.libs`) | 244 MB (7.9.3: 149 + 95; 8.0.1: 145 + 111) |
| VTK wheel installed | 566 MB unpruned -> **290 MB pruned** (identical render) |
| numpy, pillow, fonttools, ezdxf, trimesh | ~170 MB |
| **Total** | **measured 1.42 GB unpruned (`cadeval-bw`); ~1.1 GB expected after pruning VTK and removing pip/`__pycache__`** (compressed registry size not measured; guess 40-45%, LOW). Compare: document 769 MB, audio 687 MB, chromium 682 MB. FreeCAD-based alternative: 1.84 GB measured (trixie, FreeCAD alone + Mesa) before adding any renderer. |

Pruning VTK is a maintenance item (regenerate the keep-list per VTK bump from a traced render); keep it behind a build script and a test that renders after pruning. If that is judged too fragile, accept 566 MB.

## Alternatives Considered

| Category | Recommended | Alternative | Why Not |
|---|---|---|---|
| STEP/IGES kernel | `cadquery-ocp-novtk` wheel | FreeCAD (apt: bookworm 0.20.2, trixie 1.0.0; upstream 1.1.4 AppImage 720-782 MB compressed, aarch64 + x86_64; conda-forge 2026.09.16) | Same OCCT, identical results measured; 6-8x heavier; Qt/VTK/pyside deps; AppImage needs FUSE or `--appimage-extract-and-run` and is a ~2 GB tree; conda is outside the Debian-package/pin discipline. FreeCAD adds nothing needed for volume, bbox, meshing, DXF-to-SVG. |
| STEP/IGES kernel | `cadquery-ocp-novtk` | `occt-draw` / `libocct-*-dev` (apt; 7.6.3 bookworm, 7.8.1 trixie) | Tcl DRAW scripting, no Python API, a 360 MB bookworm install, no pinned-newer OCCT; bookworm's 7.6.3 is old. |
| STEP/IGES kernel | `cadquery-ocp-novtk` | `pythonocc-core` | Conda-first distribution (PyPI shows only an old 0.16 listing); not in Debian. |
| Mesh render | VTK wheel + OSMesa | f3d (any packaging) | See ranked table: no arm64 official build; Debian build only works under Xvfb. |
| Mesh render | VTK wheel + OSMesa | `python3-vtk9` (Debian 9.1.0 bookworm / 9.3.0 trixie) | Built GLX-only (X required); 553-819 MiB; older. |
| DXF to SVG | ezdxf SVG backend | LibreCAD 2.2.0 (`dxf2pdf`), qcad, Inkscape, FreeCAD importDXF/TechDraw | Qt GUI stacks (LibreCAD pulled 105 packages), need offscreen Qt platform plugins, behaviour in a Landlocked headless container unproven; FreeCAD's DXF import is 2D->Part conversion, not drawing fidelity. |
| DXF to PDF | `rsvg-convert` from ezdxf SVG | ezdxf `PyMuPdfBackend` | PyMuPDF is AGPL-3.0 (or commercial); a network service is exactly the AGPL trigger - legal question not worth opening. |
| DXF to PDF | `rsvg-convert` | ezdxf `MatplotlibBackend` + `savefig('x.pdf')` | Works (0.45 s, ~5 KB PDF) but drags matplotlib (~36 MB + deps) and gives you figure-size not page-size control; also text-as-paths means `pdftotext` is empty either way. Keep as fallback only. |
| STL stats | trimesh / numpy | Compute from VTK (`vtkMassProperties`) | Would force VTK into the measure process; trimesh is 0.8 MB pure Python. |

## Installation (Dockerfile sketch; versions are the pins to put in the file)

```dockerfile
FROM debian:bookworm-slim AS venv
RUN apt-get update && apt-get install -y --no-install-recommends python3 python3-venv ca-certificates \
 && python3 -m venv /opt/venv
COPY cad-requirements.txt /tmp/
# cad-requirements.txt: pip-compile --generate-hashes output for
#   cadquery-ocp-novtk==7.9.3.1.1  ezdxf==1.4.4  pillow  numpy  trimesh==5.1.0
RUN /opt/venv/bin/pip install --no-cache-dir --require-hashes -r /tmp/cad-requirements.txt \
 && /opt/venv/bin/pip install --no-cache-dir --no-deps --require-hashes vtk==9.7.1   # hash line from the same file
# prune unused vtkmodules/*.so here (keep-list traced from a real render), then: pip uninstall -y pip setuptools

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
      python3 libgl1 libx11-6 libosmesa6 librsvg2-bin fonts-dejavu-core ca-certificates \
 && rm -rf /var/lib/apt/lists/*
COPY --from=venv /opt/venv /opt/venv
COPY cad/*.py /opt/octoconv/cad/
COPY --from=build /out/cad-worker /usr/local/bin/cad-worker
USER nobody
ENTRYPOINT ["/usr/local/bin/cad-worker"]
```

Notes: runtime stage must install the same `python3` minor as the builder (venv symlinks into `/usr/bin/python3`); no `python3-venv` needed at runtime. Pin Debian packages by exact version only if you also own the re-pin cadence (Debian removes old versions - the project already decided this for chromium on 2026-09-30); wheels are pinned by hash.

## Open items / gaps (for phase-specific research)

- **Real-file corpus + timeout measurement** (STEP assemblies, IGES with trimmed surfaces, large STL, DXF with hatches/blocks/dimensions/xrefs): none available here; prototype used synthetic geometry. Memory at ~18x STEP size comes from one sample.
- **IGES robustness**: OCCT's IGES reader often yields shells/faces instead of solids; volume of non-solids is meaningless. Helper must count `TopAbs_SOLID` and emit `volume: null` otherwise. Not exercised beyond the one round-trip file (volume matched, bbox expanded by ~1e-7 from tolerance).
- **Assemblies / multi-body / negative-oriented solids**: compound volume is a sum; reversed orientation can give negative volume. Not tested.
- **DXF**: 3DSOLID/ACIS, proxy entities, and external fonts (SHX) are not rendered by ezdxf; DXF text uses DejaVu fallback. Page sizing for non-square drawings vs "1280x1280 default" needs a product decision (letterbox on white). Auto page size path used (`Page(0, 0, Units.mm)`); paper-space layouts not tested.
- **VTK pruning keep-list stability** across VTK bumps; **llvmpipe thread cap**; **VTK/OSMesa render determinism on the same arch** (not asserted); amd64 numbers were taken under emulation (timings pessimistic, behaviour valid).
- **Licences to clear**: `cadquery-ocp` wheels bundle OCCT (LGPL-2.1 + exception) plus `libfreeimage` (GPL/FIPL), `libraw`, `libjasper` - internal-only, not redistributed, but get a one-line legal OK. DWG stays out.
- f3d on arm64 (source build) and f3d native-amd64 timing: not measured; only relevant if rank 1 fails.

## Sources

Measured here (HIGH): Docker prototypes on arm64 OrbStack (amd64 via Rosetta) - `debian:bookworm-slim`/`trixie-slim`; apt candidate versions and installed-size deltas via `apt-cache policy` / `dpkg-query`; FreeCAD 1.0.0 `freecadcmd` vs `cadquery-ocp-novtk` 7.9.3.1.1/8.0.1.0.0 on the same STEP; VTK 9.7.1 OSMesa/EGL renders; f3d 3.1.0 (Debian) and 3.5.0 (official x86_64 tarball) backend tests; ezdxf 1.4.4 SVG/matplotlib backends; `rsvg-convert` 2.54.7/2.60.0; `strace -ff` traces.

Registries and APIs (MEDIUM): PyPI JSON for `cadquery-ocp`, `cadquery-ocp-novtk`, `ezdxf`, `vtk`, `f3d`, `trimesh`, `pyrender`, `pythonocc-core`, `pyvista` (versions, wheel tags and sizes, 2026-10-02); GitHub releases API for `f3d-app/f3d` (v3.5.0 assets) and `FreeCAD/FreeCAD` (1.1.4, weekly AppImages); anaconda.org conda-forge `freecad`.

Documentation (MEDIUM/LOW):
- f3d options and backends - https://raw.githubusercontent.com/f3d-app/f3d/master/doc/user/03-OPTIONS.md (`--rendering-backend=<auto|egl|osmesa|glx|wgl>`, `--list-rendering-backends`); f3d changelog (3.0 added `--rendering-backend`, headless-specific release dropped; 3.3 offscreen without GLX lib) - https://raw.githubusercontent.com/f3d-app/f3d/master/doc/CHANGELOG.md; f3d `library/src/context.cxx` (dlopens unversioned `libEGL.so`/`libOSMesa.so`) - https://raw.githubusercontent.com/f3d-app/f3d/v3.5.0/library/src/context.cxx
- VTK single-wheel EGL/OSMesa runtime fallback from 9.4 - https://discourse.vtk.org/t/status-update-vtk-python-wheels/11212 and https://docs.vtk.org/en/latest/advanced/available_python_wheels.html (LOW: search-result summary; confirmed by measurement)
- OCCT STEP unit parameter `xstep.cascade.unit` (default MM) - https://occt3d.com/dev/doc/overview/html/occt_user_guides__step.html (LOW: page summarised); OCCT mesh parameters - https://occt3d.com/dev/doc/overview/html/occt_user_guides__mesh.html (LOW; its "degrees" claim for angular deflection contradicts the API, which is radians)
- ezdxf drawing add-on backends (matplotlib, svg, PyMuPDF/mupdf, qt) - from the installed 1.4.4 package (`ezdxf draw --help`, `addons/drawing/`), METADATA (`PyMuPDF` only in the `draw` extra)
- `gsd-tools classify-confidence`: web-search/web-fetch-only claims classify LOW (MEDIUM when cross-verified); hands-on measurements above are the verification.
