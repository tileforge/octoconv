# Phase 75 deferred items

## From 75-07

- **3D render sidecar is written with `os.WriteFile` after a confined stage (phase 74 code, out of scope here).**
  - Where: `internal/convert/cadrender.go:126`, `CADRenderConverter.Convert`.
  - What: `cad-render` runs with write access to the job directory. If it leaves a symlink at `out.png.measurements.json`, the worker's `os.WriteFile` follows that symlink and writes the measurements document wherever it points, as the worker user.
  - Proof of the class: in 75-07, `TestCADDXFRenderWritesTheSidecarAsANewFile/a_symlink_planted_by_rsvg-convert` fails when the DXF path uses `os.WriteFile`, and passes with the remove-then-`O_EXCL` write (`cadDXFWriteNew`).
  - Suggested fix: write the 3D sidecar the same way (`cadDXFWriteNew`), and add the same planted-symlink test to cadrender_test.go.
- **Resolved:** quick 261012-gos, c092fa8 4939340 48985d3 07b9c41 982ace8 e90c0c4 cbdc2e1 de21bc3 7a25d55 d572972 ef27f77 9b14473: the write side and the read side of the same class are both closed through the shared helpers in internal/convert/jobdirfs.go (writeNewFile for every job-dir write after a stage, OpenJobFile/ReadJobFile for every job-dir read after a stage), each site with a planted-link test that failed first.

- **`TestCADMeshRefusesPartialMeshingBeforeWritingTheSTL` fails on a host whose `python3` is older than 3.10 (pre-existing, not 75-07 code).**
  - Where: `internal/convert/cadmesh_test.go` with `requireCADPython` in cadguard_test.go; the helper is `cmd/cad-worker/helper/cad-mesh` under `testdata/cadmesh-fake/run.py`.
  - What: the fake OCP harness makes `Interface_Static.SetCVal_s` a `staticmethod` object, which is callable only from Python 3.10. With macOS `/usr/bin/python3` 3.9.6, the helper raises `TypeError: 'staticmethod' object is not callable`, prints no verdict, and every subtest fails. `requireCADPython` only skips when python3 is missing; it does not check the version.
  - Proof: at the 75-07 base 3bc583a the test fails the same way with 3.9.6; at f7bf043 it passes with python 3.14 first on PATH. None of these files changed in 75-07.
  - Suggested fix: have `requireCADPython` require 3.10 or later (skip with the found version otherwise), or make the fake call a plain function.
- **Resolved:** quick 261012-gos, cbca9c8: `requireCADPython` skips below 3.10 and names the version and path it found; it passes with 3.14 first on PATH.
  status: acknowledged

## From 75-10

- **The internal/api ceiling tests leak a multipart temp file of the full upload size on every run (test-only, not 75-10 code).**
  - Where: `internal/api/cad_ceiling_test.go` and the other ceiling tests that build a request at or above a format ceiling and call `srv.Routes().ServeHTTP` on an `httptest.NewRecorder`.
  - What: `handlers.go` calls `r.ParseMultipartForm`, which spills the file part to `$TMPDIR/multipart-*`. A real `net/http` server removes the form after the handler returns; a direct `ServeHTTP` call does not. Each `go test ./...` left 6 files (66060289, 123731969 and 141557787 bytes, about 0.7 GB). By 2026-10-12 there were 74 of them (8.4 GB) in the host temp dir, which also shrinks the OrbStack VM's free space.
  - Proof: the file sizes are exactly the 63 MiB, 118 MiB and 135 MiB ceilings plus one byte, and their mtimes match the ci-local and `go test` runs of 75-09 and 75-10. 75-10 removed all 74 after checking that no process held them open.
  - Suggested fix: `t.Cleanup(func() { if req.MultipartForm != nil { req.MultipartForm.RemoveAll() } })` in the shared request helper, or serve these tests through `httptest.NewServer`.
- **Resolved:** quick 261012-gos, 63ad6c9 fcd561b: the real server path leaked too, because chi hands the handler a request copy and net/http removes only the original request's form, so the fix is in the two handlers (a guarded `RemoveAll` defer right after `ParseMultipartForm`); `go test ./internal/api/... -count=1` now leaves 0 multipart files.
  status: acknowledged
