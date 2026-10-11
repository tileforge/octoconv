# Phase 75 deferred items

## From 75-07

- **3D render sidecar is written with `os.WriteFile` after a confined stage (phase 74 code, out of scope here).**
  - Where: `internal/convert/cadrender.go:126`, `CADRenderConverter.Convert`.
  - What: `cad-render` runs with write access to the job directory. If it leaves a symlink at `out.png.measurements.json`, the worker's `os.WriteFile` follows that symlink and writes the measurements document wherever it points, as the worker user.
  - Proof of the class: in 75-07, `TestCADDXFRenderWritesTheSidecarAsANewFile/a_symlink_planted_by_rsvg-convert` fails when the DXF path uses `os.WriteFile`, and passes with the remove-then-`O_EXCL` write (`cadDXFWriteNew`).
  - Suggested fix: write the 3D sidecar the same way (`cadDXFWriteNew`), and add the same planted-symlink test to cadrender_test.go.

- **`TestCADMeshRefusesPartialMeshingBeforeWritingTheSTL` fails on a host whose `python3` is older than 3.10 (pre-existing, not 75-07 code).**
  - Where: `internal/convert/cadmesh_test.go` with `requireCADPython` in cadguard_test.go; the helper is `cmd/cad-worker/helper/cad-mesh` under `testdata/cadmesh-fake/run.py`.
  - What: the fake OCP harness makes `Interface_Static.SetCVal_s` a `staticmethod` object, which is callable only from Python 3.10. With macOS `/usr/bin/python3` 3.9.6, the helper raises `TypeError: 'staticmethod' object is not callable`, prints no verdict, and every subtest fails. `requireCADPython` only skips when python3 is missing; it does not check the version.
  - Proof: at the 75-07 base 3bc583a the test fails the same way with 3.9.6; at f7bf043 it passes with python 3.14 first on PATH. None of these files changed in 75-07.
  - Suggested fix: have `requireCADPython` require 3.10 or later (skip with the found version otherwise), or make the fake call a plain function.
