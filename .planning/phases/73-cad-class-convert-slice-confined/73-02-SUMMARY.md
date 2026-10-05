---
phase: 73-cad-class-convert-slice-confined
plan: 02
subsystem: cad-engine
tags: [cad-guard, cad-mesh, landlock, stl, sentinels, preflight]
requires:
  - phase: 73-01
    provides: RULE and DERIVED lines of evidence/73-01-spikes.txt, the CADVERDICT contract, cad-mesh
provides:
  - cmd/cad-worker/helper/cad-guard, the stdlib-only pre-pass that runs before OCP sees the bytes
  - cad-mesh with load_fails, numeric-class load-check refusal, out_of_memory and reader_exception verdicts
  - CADConverter (guard stage, mesh stage, Go-side STL judgment, six sentinels), nothing registered
  - engineExec.runBounded, CADPreflight, ConfinedCAD, ValidateCADTimeouts
affects: [73-03, 73-04, 73-05, 73-06]
actuals:
  tokens: 33700
  tasks: 3
  commits: 3
plan_head_before: d919c1c32a24a4bf181793f4f8fc045b84ebfea1
plan_head_after: 3d127ede92979dddf3a565db93bd7a297beb1790
tech-stack:
  added: []
  patterns:
    - helper verdict as the last prefixed stdout line, matched on stage, judged again by Go on the produced file
    - one classifier per stage keyed on the stage context, never on context.DeadlineExceeded alone
key-files:
  created:
    - cmd/cad-worker/helper/cad-guard
    - internal/convert/cad.go
    - internal/convert/cadsandbox.go
    - internal/convert/cadguard_test.go
    - internal/convert/cad_test.go
    - internal/convert/cadsandbox_test.go
  modified:
    - cmd/cad-worker/helper/cad-mesh
    - internal/convert/convert.go
    - internal/convert/sandboxwrap.go
    - internal/convert/sandboxcoverage_test.go
key-decisions:
  - "The guard bounds EXPANDED instances (a DAG path count) for NAUO and for MAPPED_ITEM nesting with one cap, 1518, taken from argv"
  - "No assembly_cycle verdict exists (RULE assembly_cycle=reader_handles); the expansion count terminates on cyclic edges by forcing one node per stuck step"
  - "IGES ceiling is 135 MiB (141557760), STEP 63 MiB (66060288), per the orchestrator and the DERIVED lines"
  - "ConfinedCAD refuses any concurrency but 1 whether or not a helper is given"
requirements-completed: [CADIN-04, CADW-02, CADW-03, CCONV-01]
duration: 75min
completed: 2026-10-05
status: complete
---

# Phase 73 Plan 02: Guard stage and CADConverter Summary

**A stdlib-only guard that counts entities as semicolons minus 3 and bounds expanded NAUO and MAPPED_ITEM instances before OCP loads anything, and a CADConverter that runs guard and mesh as two confined stages, re-judges the STL itself, and classifies every helper failure by stage, with the class still unregistered and unadvertised.**

## Accomplishments

- `cad-guard`: STEP entity count with early exit (754762 passes, 754763 is `too_complex/entities_over_cap`), bare-token NAUO cap (1518 passes, 1519 is `nauo_over_cap`), expanded-instance bound for the NAUO hierarchy and for MAPPED_ITEM through REPRESENTATION_MAP. Empty file, no `DATA;`, a directory and a missing path are `unreadable`, never `ok`. The caps arrive on argv only (a test greps the helper for both numbers).
- `cad-mesh`: `load_fails` in the verdict, numeric-class load check (`not a Real`, `not an Integer`) refused as `unreadable/load_check_fail` before TransferRoots, `MemoryError` as `too_complex/out_of_memory` (exit 4), any other exception after the imports as `unreadable/reader_exception`. Imports stay outside the try. Smoke-run on the real OCP 7.9.3.1.1 image (a 71 measurement image, not the 73-01 production image, which is no longer present locally): inch STEP gives 12 triangles, a 51-fail `abc` coordinate file is `load_check_fail`, a syntactically broken DATA section is `no_shape`, the IGES surface fixture gives 12 triangles.
- `CADConverter.Convert`: byte ceiling re-check (63 MiB STEP, 135 MiB IGES) before any engine, guard stage then mesh stage through `runBounded` with literal engine names, each with its own read-only set, env (`engineEnv` plus `LP_NUM_THREADS=2`, `TMPDIR` is the job directory) and stage deadline, then `judgeBinarySTL` (size `84+50n`, `n>0`, finite vertices).
- Classification per stage in the planned order: sandbox exit 78 first, then the last `CADVERDICT` line whose stage matches, then the stage deadline (mesh only gets `ErrCADMeshTimeout`, wrapped with `%v` so it does not read as an attempt deadline), then the parent context, then SIGKILL with both contexts alive (`ErrCADEngineKilled`), other signals (`ErrCADUnreadable`), then a plain no-verdict error.
- `CADPreflight`: guard and mesh proven in both directions on the embedded inch fixture. Inside the job directory the guard's entity count equals Go's own count and the mesh gives 12 triangles at sorted extents 25.4/50.8/76.2 mm. The outside copy gets no ok from the guard and no STL from the mesh (invoked directly, not through Convert). A sandbox-unavailable error on the outside runs is not accepted as a denial.
- `ConfinedCAD`, `ValidateCADTimeouts`, `WithSandbox`; the four sandbox sets, `cadMultiarchLib` and the pinned constants are held by tests whose messages say to re-measure (73-03) or re-derive from the evidence rather than edit the expectation.

## RULE lines and where each landed

| RULE | value | guard / mesh branch | subtest |
|---|---|---|---|
| load_check | numeric_strict | cad-mesh: numeric fails before TransferRoots give `load_check_fail`; other fails reported only in `load_fails` | verified on the real reader (smoke run above); no OCP on the host, so no Go test |
| assembly_cycle | reader_handles | none: no `assembly_cycle` reason exists in the guard | none for a verdict; one test pins that the expansion count terminates on a self-reference and a two-node cycle and returns ok |
| expansion | guard_bounds_expanded_instances | `expansion_over_cap`, path-count DAG sum, saturating | 2^11 from 22 statements rejected, 2^9 ok with `expanded=1022`, assembly_nauo4 ok with 4 |
| mapped_item | guard_bounds | `mapped_item_expansion_over_cap` through MAPPED_ITEM, REPRESENTATION_MAP and representation items | level 10 rejected with `nauo=0`, level 9 ok with 1022 |
| entity_counter | semicolons_minus_3 | `count_entities` | boundary files, comment/single-line/CR-only forms, corpus differential against the line-anchored regex |

## Task Commits

1. **Task 1: cad-guard and the mesh verdict rules** - `5c46a99` (feat)
2. **Task 2: CADConverter, runBounded, coverage scanner** - `d266f07` (feat)
3. **Task 3: CADPreflight, ConfinedCAD, timeout validation** - `3d127ed` (feat)

All three are SSH-signed (`Good "git" signature`).

## Deviations from Plan

**1. [Rule 1 - Operand] IGES ceiling.** The plan text says 155 MiB; the orchestrator and the DERIVED line `cad_iges_ceiling_bytes` say 135 MiB (141557760). `CADMaxIGESBytes = 135 << 20`, and the pinned-constants test names the evidence file as the source.

**2. [Rule 3 - Blocking] Invariance base.** `git diff --exit-code` for exec.go, go.mod, go.sum, converters.go, internal/api and internal/queue was run against `e219f8c`, not `520ecd4` (73-01 deviation 1). Clean.

**3. [Rule 2 - Missing functionality] Expansion count on cyclic edges.** With `assembly_cycle=reader_handles` no cycle verdict exists, but a path count over a cyclic edge list is undefined. The helper runs Kahn's algorithm and, when it is stuck, forces the lowest-id unprocessed node, ignoring its remaining incoming edges, so the count is finite and a bomb hanging off a cycle is still counted. A cycle file therefore gets ok from the guard and is left to the reader, which 73-01 measured as rc=2 with a null shape in under 1 s. This is the same bound, applied to a boundary of its input, not a different bound.

**4. [Rule 2] MAPPED_ITEM container selection.** Containers are statements whose type name contains `REPRESENTATION` (simple or inside a complex entity), keyed by the second parameter (the items list). Tokens that are not well-formed statements, or whose ids cannot be resolved, are not edges, which is the plan's own rule for NAUO applied the same way. On the committed corpus the NIST files report expanded 0, 1 and 25 and stay ok.

**5. Task split.** `WithSandbox` is in Task 3 as planned; the Task 2 tests build `CADConverter{Sandbox: newEngineSandbox(h)}` directly so Task 2 does not depend on it. The coverage-scanner change landed in the same commit as the first `runBounded` caller, as the plan required.

**6. cad-mesh `load_fails = -1`.** The plan says -1 when the binding was recorded unavailable. BINDING recorded it available, so -1 is used only for "not evaluated yet" (the read itself failed). A check-list exception after a successful read falls into `reader_exception`.

**7. Preflight guard-effectiveness acceptance.** Any guard error other than sandbox-unavailable counts as a denial of the outside file. In the real image the denial is `ErrCADUnreadable`; I did not tighten to that sentinel so a crash mode of the guard does not make the start-up check brittle. Sufficiency in the same ruleset already proves the guard can run.

**Total deviations:** 7, none outside the plan's files.

## Issues Encountered

- The 73-01 production image tag is not present locally (only the 71 measurement images), so the cad-mesh changes were smoke-tested on `octoconv-cad-measure:ocp7.9.3.1.1-full-dc621b94ee5d` with the new helper mounted. 73-03 re-runs it on the production image.
- `go test ./internal/convert/` takes about 110 s now (the fake-engine tests cost about 0.7 s each on this host).

## Known Stubs

None.

## Known Gaps

- `1..2` stays the recorded GAP from 73-01 and is not handled.
- IGES has no entity cap and no expansion rule: 71 measured none and none was invented. Only the byte ceiling and the stage deadlines bound it.
- The cad-mesh load-check and exception paths have no Go test (no OCP on the host); they are covered by the smoke runs above and the 73-03 re-measurement.
- The four sandbox sets are the 71 candidate translated to the production layout, unmeasured on the production image until 73-03.

## Threat Flags

None beyond the register: T-73-04 (guard rules, uncountable is unreadable), T-73-05 (last matching verdict plus Go-side STL judgment), T-73-06 (1 MiB tail via `runBounded`), T-73-07 (process-group kill proven with a forked grandchild whose PID is asserted dead).

## Self-Check: PASSED

- Created files exist: cad-guard (mode 100755), cad.go, cadsandbox.go, cadguard_test.go, cad_test.go, cadsandbox_test.go.
- Commits `5c46a99`, `d266f07`, `3d127ed` are ancestors of HEAD.
- `go build ./...`, `go vet ./...`, `gofmt -l internal cmd` clean; `go test ./... -count=1` green.
- `git diff --exit-code e219f8c -- internal/convert/exec.go go.mod go.sum internal/convert/converters.go internal/api internal/queue` prints nothing; `AllEngines()` still has nine classes; `TestDefaultRegistersNoCADPair` passes.
- No comment lines in the new Go files except the single `go:embed` directive; no `#` comment lines in the helpers beyond the shebang; neither `754762` nor `1518` appears in cad-guard.
