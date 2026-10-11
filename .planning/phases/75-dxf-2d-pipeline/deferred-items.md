# Phase 75 deferred items

## From 75-07

- **3D render sidecar is written with `os.WriteFile` after a confined stage (phase 74 code, out of scope here).**
  - Where: `internal/convert/cadrender.go:126`, `CADRenderConverter.Convert`.
  - What: `cad-render` runs with write access to the job directory. If it leaves a symlink at `out.png.measurements.json`, the worker's `os.WriteFile` follows that symlink and writes the measurements document wherever it points, as the worker user.
  - Proof of the class: in 75-07, `TestCADDXFRenderWritesTheSidecarAsANewFile/a_symlink_planted_by_rsvg-convert` fails when the DXF path uses `os.WriteFile`, and passes with the remove-then-`O_EXCL` write (`cadDXFWriteNew`).
  - Suggested fix: write the 3D sidecar the same way (`cadDXFWriteNew`), and add the same planted-symlink test to cadrender_test.go.
