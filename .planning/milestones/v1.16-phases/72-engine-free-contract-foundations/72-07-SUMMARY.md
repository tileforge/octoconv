---
phase: 72-engine-free-contract-foundations
plan: 07
subsystem: testing
tags: [cad, differential-test, golden, step, iges, dxf, stl, ocp, ezdxf, go]
requires:
  - phase: 72-engine-free-contract-foundations
    provides: SniffCADWindows rule set (72-04), stp/igs aliases (72-05), CAD rejections on both endpoints (72-06)
provides:
  - scripts/cmd/cad-sniff verdict emitter, scripts/cad-sniff-reader.py in-image reader helper, scripts/cad-sniff-differential.sh host driver
  - internal/convert/testdata/cad-sniff-golden.tsv (69 rows) and TestSniffCADGolden
  - evidence/72-sniff-divergence.txt (HDR, 69 ROW, 7 DIVERGENCE, SUMMARY)
affects: [73]
status: complete
commits: 3
plan_head_before: e993291901fb8c495a11b86f373c2967390fdb45
plan_head_after: cb3ffb66072bb83f9a2352139e75a7ab6e298154
actuals:
  tokens: 12400
  tasks: 2
  commits: 3
tech-stack:
  added: []
  patterns:
    - "sniffer verdicts are pinned by a golden that is recomputed in Go and cross-checked against reader outcomes recorded from the real parsers"
    - "reader helper lives outside scripts/cad-measure so the 71 image hash input is untouched"
key-files:
  created:
    - scripts/cmd/cad-sniff/main.go
    - scripts/cad-sniff-reader.py
    - scripts/cad-sniff-differential.sh
    - internal/convert/testdata/cad-sniff-golden.tsv
    - internal/convert/cadsniff_golden_test.go
    - .planning/phases/72-engine-free-contract-foundations/evidence/72-sniff-divergence.txt
  modified: []
key-decisions:
  - "No sniffer change: the differential found no real file that the sniffer refuses while its reader accepts"
requirements-completed: [CADIN-02]
---

# Phase 72 Plan 07: Sniffer vs real readers differential Summary

The sniffer was run against OCP 7.9.3.1.1 (STEP, IGES), ezdxf 1.4.4 (strict and recover) and the baked `cad_mesh.read_stl` over all 52 committed and all 17 fetched files, in the existing pruned 71 image, and every verdict is now pinned by a golden that `TestSniffCADGolden` recomputes. Result: 62 agree, 0 later-stage, 7 policy, 0 violation.

## Image tag (D-08, Pitfall 6)

`scripts/cad-measure/common.sh tag 7.9.3.1.1 pruned` printed `octoconv-cad-measure:ocp7.9.3.1.1-pruned-dc621b94ee5d` at task start, in the evidence `HDR,image_tag_before` and `HDR,image_tag_after`, and at the end of the plan. Image id `sha256:08117b26...`, arm64 on an arm64 server (native). Nothing was built or retagged; no phase-72 commit touches `scripts/cad-measure` or `Dockerfile.cad-measure`; no dependency added (`go.mod`, `go.sum`, `frontend/package.json` unchanged since d034437). The old plan-quoted tag `...-0d09b7ba39ad` is superseded by the architecture-neutral keep-list rebase.

## Divergence table (7 policy rows, all reject fixtures)

| File | Sniffer | Reader | Detail | Label |
| --- | --- | --- | --- | --- |
| reject/dwg_ac1027.dwg | dwg / dwg | none | no reader | policy:dwg |
| reject/dxf_bom.dxf | unrecognized | ezdxf accepts | strict OSError, recover ok | policy:bom |
| reject/dxf_truncated.dxf | dxf / truncated | ezdxf accepts | strict DXFStructureError, recover ok | policy:truncated |
| reject/iges_truncated.iges | iges / truncated | OCP accepts | parse 1, transfer 1 | policy:truncated |
| reject/step_trailing_nul.step | step / truncated | OCP accepts | parse 1, transfer 1 | policy:trailing-nul |
| reject/stl_ambiguous.stl | stl / ambiguous_stl | numpy accepts | binary=1, 12 triangles | policy:ambiguous-stl |
| reject/stl_ascii_truncated.stl | stl / truncated | numpy accepts | binary=0, 6 triangles | policy:truncated |

All seven are the policy rejections of D-09 and orchestrator decision 2: the reader's own leniency (ezdxf recover, OCP tolerating a cut IGES or trailing NUL, the regex STL reader taking an ASCII file cut mid-solid, a size-consistent binary STL whose header says `solid`) is exactly what the sniffer exists to refuse earlier. The BOM and trailing-NUL decisions stay as locked: no real file contradicts them.

Rows where both sides refuse (agree): dxf_binary, step_bom (OCP parse 0), step_missing_header, step_truncated, stl_binary_trailing_byte (reader finds no ASCII vertices), stl_binary_truncated. Rows where both accept: stl_binary_ac10_header and stl_binary_empty (zero triangles, reader returns an empty array and so counts as accepting; a later stage owns the empty-geometry decision).

All 19 public, 18 synthetic and 17 fetched files are accepted by the sniffer and by their reader (OCP parse and transfer both 1; ezdxf strict and recover both 1; STL parsed to triangles). No sniffer rule changed, so `cadsniff.go` and `cadsniff_test.go` are untouched.

## Accomplishments

- `scripts/cmd/cad-sniff`: emits `name group sha256 bytes sniff_format sniff_rejection` per file, declared format from `NormalizeFormat(extension)`.
- `scripts/cad-sniff-reader.py`: one `-I -B` child per file, 900 s timeout, result through a file (OCCT writes to stdout); crash and timeout cost one row (`crash:rc=N`, `timeout`).
- `scripts/cad-sniff-differential.sh`: `cad_image_tag` only, `cad_require_native`, `--network none --cpus 2 --memory 4g --memory-swap 4g`, scripts and corpus read-only, fetched files hash-checked against the manifest, evidence and golden written before a violation exits 1; `--scaled` rows go to evidence only.
- `TestSniffCADGolden`: one-to-one golden vs `testdata/cad/<group>` (excluding `*.md`), sha256 and byte count per file, recomputed verdict through `ReadCADWindows` + `SniffCADWindows`, and a policy class recomputed from the columns so a hand-edited label cannot hide a violation. 52 committed rows plus 17 fetched subtests, zero SKIP on this machine.
- Mutation probes (each restored afterward) all fail the test: public STEP verdict changed to truncated, changed sha256, missing row, extra row, a public row edited to unrecognized+agree, a reject row relabelled agree, a fetched row relabelled policy:bom.

## Verification

- Task 1 gate `TOOLING-OK`; Task 2 gate `DIFFERENTIAL-OK`; golden probe `GOLDEN-FIRES`.
- Phase gate on go1.26.4: `go build ./...`, `go vet ./...`, `go test ./... -count=1` all green.
- Live Postgres 18 on localhost:5434 (started `octoconv-db`, stopped again after): `go test ./internal/jobs/ -count=1 -v` ok, 168 passes, no SKIP.
- `npm --prefix frontend run test`: 37 passed.
- `git rev-list --count HEAD -- internal/api/testdata/pre59_inspect_pdf.golden.json` prints 1; nothing changed under `internal/api/testdata` or `internal/inspect/findings.go` since d034437; `TestAllEnginesCoversEveryEngineClass`, `TestDefaultRegistersNoCADPair` and `TestListFormats_AdvertisesNoCADFormat` pass.
- Acceptance greps: `cad_build_image` 0, `cad_image_tag` 2, `--network none` 1, shell comment lines 1 (shebang), Python comment lines 0, no `//` lines in the Go emitter.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Reader helper cannot be mounted at /lib**
- **Found during:** Task 2, first differential run
- **Issue:** in the 71 image `/lib` is a symlink to `/usr/lib`; bind-mounting `scripts/` over it removed the dynamic linker and `exec /opt/venv/bin/python` failed.
- **Fix:** mount at `/opt/cad-sniff` (`:ro`) and call `/opt/cad-sniff/cad-sniff-reader.py`. The plan's key_link pattern `/lib/cad-sniff-reader.py` is therefore `/opt/cad-sniff/cad-sniff-reader.py`.
- **Files modified:** scripts/cad-sniff-differential.sh
- **Commit:** bdfafdc

**2. [Rule 1 - Bug] Reader and verdict files joined in swapped order**
- **Found during:** Task 2, first run reported 69 violations with an empty golden
- **Issue:** the awk join indexed the first file as the reader table but was given the verdict file first.
- **Fix:** swapped the argument order; the first run's output was discarded and the final evidence and golden come from the corrected script.
- **Files modified:** scripts/cad-sniff-differential.sh
- **Commit:** bdfafdc

Both fixes were found before any evidence was committed; commit 536d24f carries the first draft of the driver, bdfafdc the corrected one.

## Known Stubs

None.

## Threat Flags

None.

## Self-Check: PASSED

All six created files exist; commits 536d24f, bdfafdc and cb3ffb6 are ancestors of HEAD; evidence, golden and test are tracked.
