---
phase: 76-measured-bounds-keda-proof-client-docs
plan: 05
subsystem: cad-bounds
tags: [cad, path-b, render-ceiling, d-04, ar-1, evidence-pinned-tests]
status: complete

requires:
  - phase: 76-03
    provides: evidence/76-03-derived.txt (four PATHB lines, all fired=0; DERIVED cad_<f>_ceiling_bytes)
  - phase: 76-01
    provides: evidence/76-01-stack.txt (STACK_DECISION not_byte_bound for step, iges and stl)
  - phase: 76-04
    provides: CADMaxIGESBytes = 125829120 and the other ceilings pinned to the 76 evidence
provides:
  - TestCADPathBFiredForNoFormat (the four PATHB lines are fired=0, and each reason agrees with 76-01)
  - TestCADRenderReChecksTheDerivedCeilingOfEveryFormat (the worker's render re-check uses the derived convert ceiling for step, iges and stl)
  - TestCADEveryOperationRefusesAboveTheDerivedFormatCeiling (create 413 and inspect max_bytes are the same for convert and render)
affects: [76-06, 76-08]

actuals:
  tokens: 9000
  tasks: 3
  commits: 3
plan_head_before: 745ae15de4a088d0460730edbead50d6a45eb100
plan_head_after: 16f09b89e7332c224bf157e5733e333b8a6d7c44

tech-stack:
  added: []
  patterns:
    - "A Path-B decision recorded in the evidence (fired=0) is held by a test, so a re-derivation that fires Path-B fails the build until the operation-aware ceiling exists"
    - "Byte-ceiling tests read the DERIVED line directly, not the Go constant, and cover every operation of every format"

key-files:
  created: []
  modified:
    - internal/convert/cad_test.go
    - internal/convert/cadrender_test.go
    - internal/api/cad_ceiling_test.go

key-decisions:
  - "Not-fired branch: no render ceiling, no operation-aware size map and no ERROR-CODES change, because every PATHB line in 76-03-derived.txt says fired=0"
  - "The absence is held by tests that read the evidence, not just written down. Only test files changed; no production code and no docs"

requirements-completed: []

duration: about 35 min (2026-10-15 01:22Z to 01:57Z), including a 7 min wait for load1 < 8 before the gate
completed: 2026-10-15
---

# Phase 76 Plan 05: Path-B render ceiling (not fired) Summary

**Path-B fired for no format, so no render ceiling exists. Three tests now hold that outcome against the evidence. The first fails if any PATHB line becomes fired=1, or if a line's reason disagrees with the 76-01 STACK_DECISION. The other two check that create, inspect and the worker refuse convert and render at the same derived ceiling, step, IGES (125829120) and STL. Only test files changed.**

## Branch taken: not fired

The four PATHB lines of `evidence/76-03-derived.txt` (lines 73-76), quoted verbatim:

```
PATHB,format=step,fired=0,reason=not_byte_bound_in_76-01_STACK_DECISION
PATHB,format=iges,fired=0,reason=not_byte_bound_in_76-01_STACK_DECISION;iges_ceiling_lowered_by_the_iges_memory_ceiling_amendment_not_by_Path-B
PATHB,format=stl,fired=0,reason=not_byte_bound_in_76-01_STACK_DECISION
PATHB,format=dxf,fired=0,reason=no_76-01_byte_bound_decision_for_dxf;dxf_route_sum_661
```

The matching 76-01 lines (`evidence/76-01-stack.txt`:115-118) are `STACK_DECISION,format=step|iges|stl,decision=not_byte_bound,...` and `STACK_GLOBAL,render_deadline_rule=remainder,...,byte_bound=none`. No STACK_DECISION exists for dxf. There is no fired format, so there is no fired-but-not-byte_bound contradiction to return.

| Task | Plan text for fired=0 | Outcome |
|---|---|---|
| 1 | record the four PATHB lines, change nothing | Lines quoted above. No `CADMax<F>RenderBytes`, no `cadMaxRenderBytes`, and CADRenderConverter keeps `cadMaxBytes(format)`. Added TestCADPathBFiredForNoFormat and TestCADRenderReChecksTheDerivedCeilingOfEveryFormat (deviation 1) |
| 2 | not applicable | No `defaultMaxOperationFormatBytes`, no `Config.MaxOperationFormatBytes`, and `sizeLimit(engine, format)` is unchanged. Added TestCADEveryOperationRefusesAboveTheDerivedFormatCeiling (deviation 1) |
| 3 | not applicable | docs/ERROR-CODES.md and cad_docs_sync_test.go unchanged. The docs already state the one per-format ceiling for every cad offer, held by TestErrorCodesDocStatesTheCADCeilingsOfTheCode |

The IGES ceiling, 125829120 bytes (120 MiB), was lowered by the iges_memory_ceiling amendment, not by Path-B. 76-04 already enforces it through `CADMaxIGESBytes` at four points:

- create 413: `defaultMaxFormatBytes` in internal/api/api.go:513 and `sizeLimit` at handlers.go:707;
- inspect `max_bytes`: estimate.go:355;
- the worker: `cadMaxBytes` in cad.go:84, read by CADConverter and by CADRenderConverter at cadrender.go:50;
- docs/ERROR-CODES.md:105, 120 and 145.

This plan adds the tests that hold the same ceiling on every operation, read straight from the DERIVED line.

## What changed (file:line)

| File | Lines | What |
|---|---|---|
| internal/convert/cad_test.go | 789 | `cad76StackEvidence`, the path to 76-01-stack.txt |
| internal/convert/cad_test.go | 791-840 | `TestCADPathBFiredForNoFormat`. It requires every `^PATHB,` line to parse, exactly one line for each of step, iges, stl and dxf, and `fired=0` on each. It also checks each reason against the evidence. A reason of `not_byte_bound_in_76-01_STACK_DECISION` needs a 76-01 `decision=not_byte_bound`. A reason of `no_76-01_byte_bound_decision_for_<f>` needs no STACK_DECISION for that format. Any other reason fails |
| internal/convert/cadrender_test.go | 345-381 | `TestCADRenderReChecksTheDerivedCeilingOfEveryFormat`. For step, iges and stl it reads `cad_<f>_ceiling_bytes`. At ceiling+1 (sparse) a render gets ErrCADInputTooLarge, the error names that value, and no engine runs. At exactly the ceiling the render reaches an engine |
| internal/api/cad_ceiling_test.go | 295-312 | `cad76DerivedEvidence` and `cad76DerivedCeiling`, which read a DERIVED ceiling line |
| internal/api/cad_ceiling_test.go | 314-319 | `cadZeroSTLAbove`, a structurally valid binary STL just above a size |
| internal/api/cad_ceiling_test.go | 321-388 | `TestCADEveryOperationRefusesAboveTheDerivedFormatCeiling`. It builds step, iges and stl inputs above their DERIVED ceilings. Create gets 413 `payload_too_large` with `<f> file exceeds the <n> MiB limit for this conversion` for convert and for render, and nothing is uploaded, created or enqueued. In inspect, every cad offer (exactly convert stl + render png, or render png for stl) has `max_bytes` = the DERIVED ceiling, `available=false` and `payload_too_large` |

No production file, cmd/ file or doc changed. None of the added lines is a comment (0 added `//` lines).

## Tests and mutations

All mutations were made on the working tree, each file was restored from a byte copy, and `cmp` confirmed every restore.

| Mutation | Test | Result |
|---|---|---|
| evidence `PATHB,format=iges,fired=1` | TestCADPathBFiredForNoFormat | FAIL "PATHB fired=1 for iges ... D-04 then needs an operation-aware render ceiling" |
| 76-01 `STACK_DECISION,format=step,decision=byte_bound` | TestCADPathBFiredForNoFormat | FAIL "PATHB step says not_byte_bound in 76-01, but ... has decision \"byte_bound\"" |
| evidence dxf PATHB line deleted | TestCADPathBFiredForNoFormat | FAIL "0 PATHB lines for dxf, want 1" |
| evidence `fired=yes` for stl | TestCADPathBFiredForNoFormat | FAIL "4 PATHB lines and 3 of them parse" |
| cadrender.go render ceiling `cadMaxBytes(format) - 1` (a render ceiling below convert) | TestCADRenderReChecksTheDerivedCeilingOfEveryFormat | FAIL: at ceiling refused; the error names the wrong ceiling |
| cadrender.go render ceiling `cadMaxBytes(format) + 1` | TestCADRenderReChecksTheDerivedCeilingOfEveryFormat | FAIL on all three formats: one byte over reached the guard |
| handlers.go size gate skipped for operation render | TestCADEveryOperationRefusesAboveTheDerivedFormatCeiling | FAIL: render png gets 202 on step, iges and stl |
| estimate.go render offer limit + 1 GiB | TestCADEveryOperationRefusesAboveTheDerivedFormatCeiling | FAIL: render offer available with the wrong max_bytes, on all three formats |
| api.go `"iges": CADMaxIGESBytes + 1<<20` | TestCADEveryOperationRefusesAboveTheDerivedFormatCeiling | FAIL on iges (convert stl 202); step and stl PASS |

After the restores, all three new tests pass, and `go test ./internal/api/ -count=1` gives ok (7.2 s). The plan's three verify commands take the not-fired `else` branch, and each exits 0:

- T1: no `cadMaxRenderBytes`, no `RenderBytes =` and no `TestCADRenderCeilingWorkerRecheck`;
- T2: no `defaultMaxOperationFormatBytes` and no `TestCADRenderCeiling`;
- T3: no "MiB FORMAT for render" in ERROR-CODES and no `RenderBytes` in cad_docs_sync_test.go.

Other checks:

- `gofmt -l .` prints nothing;
- `GOTOOLCHAIN=go1.26.4 go vet ./...` exits 0;
- `git diff --exit-code main -- internal/convert/exec.go` exits 0.

## Gate

`evidence/76-05-ci-local.txt` holds the `scripts/ci-local.sh gate` transcript, byte-identical to the run's stdout. The run's setup:

- a fresh clone of a git bundle at 06c1612, with deploy/tectonic-bundle/octoconv-bundle.ttb copied in;
- GOTOOLCHAIN=go1.26.4, python 3.14.8 first on PATH, and CI_LOCAL_EVIDENCE_DIR in scratch;
- no tracked-changes marker on the commit line (tracked_dirt=0), and 0 porcelain lines in the clone after the run;
- started at load1 7.96 (after waiting for it to fall below 8) at 01:29:15Z, ended at load1 5.62 at 01:53:31Z.

| Tier | Result | internal/convert | internal/api |
|---|---|---|---|
| gate | PASS: gofmt clean, go vet clean, go build clean, go test ./... ok | 328.203 s, under the 540 s stop line (D-19) | 9.101 s |

`ALL REQUESTED TIERS PASSED`. The transcript has no `--- FAIL` and no `FAIL` line. The plan asks for this gate only on the fired branch; it was run here because this plan added tests.

## Deviations from Plan

1. **[Orchestrator / fix the class] Tests added on the not-fired branch.** The plan's first truth and Task 1's acceptance say that with no fired line, no file under internal/ changes. On the orchestrator's instruction, this plan instead holds the not-fired outcome with tests that read the evidence. Before this plan:
   - nothing in the code read the PATHB lines, so a re-derivation that fired Path-B would have passed every test with no render ceiling;
   - the render path's byte re-check in the worker was tested only for STL;
   - create's 413 for a render was tested only for DXF.

   The plan's commits touch three test files under internal/ (`git show --stat` in the Self-Check). They touch no production code, cmd/ or docs/. The plan's not-fired negative greps still pass, because the new names avoid the fired-branch identifiers.
2. **No other deviation.** No render ceiling, operation-aware map, heuristic stacking guard, convert ceiling change, triangle cap change or CADMaxNAUO change was made (prohibitions honoured).

## Known Stubs

None.

## Threat Flags

None. T-76-09 is not applicable because nothing fired. The tests show that create and the worker refuse every operation at the measured format ceiling. T-76-10 still holds: render and convert share one ceiling.

## Self-Check: PASSED

- Files exist: internal/convert/cad_test.go, internal/convert/cadrender_test.go, internal/api/cad_ceiling_test.go, evidence/76-05-ci-local.txt.
- Commits from 745ae15 are in HEAD, all SSH-signed (G), with no Co-Authored-By trailer. `git show --stat` for each:
  - deba500 test: internal/convert/cad_test.go +53, internal/convert/cadrender_test.go +38
  - 06c1612 test: internal/api/cad_ceiling_test.go +97
  - 16f09b8 test: evidence/76-05-ci-local.txt +78
- Under cmd/, internal/ and docs/, `git diff --name-only 745ae15..HEAD` lists only the three test files.
