---
phase: 76-measured-bounds-keda-proof-client-docs
plan: 04
subsystem: cad-bounds
tags: [cad, timeouts, ceilings, in-02, grace-chain, provenance, derive76]
status: complete

requires:
  - phase: 76-03
    provides: evidence/76-03-derived.txt (16 DERIVED lines, render_remainder_all, IGES ceiling lowered by memory, SVG cap from RULE bytes)
provides:
  - internal/convert/cad.go constants equal to the derive76 DERIVED lines; engine = max(3D sum, DXF sum)
  - TestCADConstantsComeFromThe76Evidence, TestCADEngineTimeoutIsTheLongerRoute, TestCADGraceChainFollowsTheEngineTimeout
  - cad inspect estimate_source=measured at 886 s
  - evidence/76-04-ci-local.txt (helm, gate, race ALL REQUESTED TIERS PASSED at 764e022, tracked_dirt=0)
affects: [76-05, 76-06, 76-07, 76-08, 76-09]

actuals:
  tokens: 12765
  tasks: 3
  commits: 4
plan_head_before: 146d0f191a757e8d3b373cb717c9cef391e68dcc
plan_head_after: 764e02233153c22eedabe965804b51f9fc67981c

tech-stack:
  added: []
  patterns:
    - "Every derived CAD constant is pinned by one table row that reads its DERIVED line; a 1 s mutation fails it"
    - "The grace chain (ConfigMap, compose, values grace, KEDA stabilization, cad-worker stage vars) is checked against the Go defaults, not against literals"
    - "Client-visible messages and docs numbers in tests are formatted from the constants"

key-files:
  created:
    - internal/e2e/cad_grace_chain_test.go
    - .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-04-ci-local.txt
  modified:
    - internal/convert/cad.go
    - internal/convert/cad_test.go
    - internal/convert/cadsandbox_test.go
    - internal/convert/cad_docs_sync_test.go
    - internal/api/estimate.go
    - internal/api/cad_ceiling_test.go
    - docs/ERROR-CODES.md
    - docker-compose.yml
    - deploy/chart/octoconv/templates/configmap.yaml
    - .env.example
    - scripts/cad-liveproof.sh

key-decisions:
  - "CADDefaultEngineTimeout is Go's built-in max over the two route sums (886 vs 661); it stays 886 s, so CAD_ENGINE_TIMEOUT, stop_grace_period 906 s and cadWorker grace 906 are unchanged"
  - "The widened KNOWN_LIMIT (3D render cost is not bounded by input bytes) is stated on the cad_too_complex row of ERROR-CODES with the 521 s default, and held by the docs sync test"
  - "cad inspect provenance flipped to measured with engineMeasuredDefaultSeconds[cad]=886 in the same commit as the .env.example number"

requirements-completed: []

duration: about 34 min (2026-10-15 00:43Z to 01:17Z), including a 4 min wait for load1 < 8 before the gate
completed: 2026-10-15
---

# Phase 76 Plan 04: Measured CAD bounds in code, config and docs Summary

**The derive76 numbers are now the CAD defaults: guard 21 s, mesh 324 s, render 521 s, I/O 20 s, DXF 186 s, rsvg 217 s, engine max(886, 661) = 886 s. The IGES ceiling is 120 MiB and the SVG cap 134 MiB. Each value is pinned by a test that reads evidence/76-03-derived.txt. Compose and the chart follow these values, the cad inspect estimate says `measured`, and helm, gate and race pass from a clean clone at 764e022.**

## Constants changed (old -> new)

| Constant / setting | Old | New | Where |
|---|---|---|---|
| CADDefaultGuardTimeout | 30 s | 21 s | internal/convert/cad.go:30 |
| CADDefaultMeshTimeout | 316 s | 324 s | internal/convert/cad.go:31 |
| CADDefaultRenderTimeout | 480 s | 521 s | internal/convert/cad.go:32 |
| CADIOAllowance | 60 s | 20 s | internal/convert/cad.go:33 |
| CADDefaultEngineTimeout | G+M+R+IO = 886 s | max(G+M+R+IO, G+D+2S+IO) = 886 s | internal/convert/cad.go:34 |
| CADMaxIGESBytes | 135 << 20 (141557760) | 120 << 20 (125829120) | internal/convert/cad.go:25 |
| CADMaxDXFSVGBytes | 129 << 20 (135266304) | 134 << 20 (140509184) | internal/convert/cad.go:40 |
| CADDefaultDXFTimeout | 162 s | 186 s | internal/convert/cad.go:42 |
| CADDefaultRsvgTimeout | 141 s | 217 s | internal/convert/cad.go:43 |
| CADMaxSTEPBytes, CADMaxSTLBytes, CADMaxDXFBytes, CADMaxDXFPDFBytes | 66060288, 190840832, 123731968, 19922944 | unchanged (DERIVED restated) | cad.go:24, 29, 36, 41 |
| compose cad-worker CAD_GUARD/MESH/RENDER/DXF/RSVG_TIMEOUT | 30s/316s/480s/162s/141s | 21s/324s/521s/186s/217s | docker-compose.yml:1729-1733 |
| ConfigMap CAD_GUARD/MESH/RENDER/DXF/RSVG_TIMEOUT | 30s/316s/480s/162s/141s | 21s/324s/521s/186s/217s | deploy/chart/octoconv/templates/configmap.yaml:229-233 |
| .env.example CAD_ENGINE_TIMEOUT own line | "DERIVED, not measured" | "measured -- see evidence/76-03-derived.txt (phase 76)" | .env.example:596 |
| .env.example stage vars | 30s/316s/480s/162s/141s | 21s/324s/521s/186s/217s | .env.example:599-601, 620-621 |
| engineEstimateSource[cad] | unmeasured | measured | internal/api/estimate.go:135 |
| engineMeasuredDefaultSeconds[cad] | absent | 886 | internal/api/estimate.go:167 |
| ERROR-CODES IGES ceiling | 135 MiB | 120 MiB | docs/ERROR-CODES.md:105, 120, 145 |
| ERROR-CODES DXF paragraph | SVG 129 MiB, 162 s, 141 s | SVG 134 MiB, 186 s, 217 s | docs/ERROR-CODES.md:122 |
| ERROR-CODES cad_too_complex | render deadline without a value | `CAD_RENDER_TIMEOUT`, 521 s, plus the KNOWN_LIMIT that 3D render cost is not bounded by input bytes | docs/ERROR-CODES.md:148 |
| cad-liveproof INSPECT | IGES max_bytes 141557760, estimate_source unmeasured | 125829120, measured | scripts/cad-liveproof.sh:343, 346, 352-353 |

Unchanged because E = 886 did not move: every `CAD_ENGINE_TIMEOUT: "886s"` (13 compose services and the ConfigMap), compose `stop_grace_period: 906s`, `cadWorker.terminationGracePeriodSeconds: 906`, and `keda.cad.scaleDownStabilizationSeconds: 900` (≥ 886).

## Sweep of old values (repo outside .planning)

| Old value | Occurrence | Action |
|---|---|---|
| 30/316/480/162/141 s, 60 s I/O | cad.go constants | updated |
| same | cad_test.go TestCADPinnedConstantsComeFromTheEvidence rows (IGES, guard, mesh, render, io, engine, "30 + 316 + 480 + 60") | removed; the 76 table is the single source. STEP entities and NAUO stay |
| same | cad_test.go TestCADDXFConstantsComeFromTheEvidence rows (ceiling, dxf, rsvg, SVG, PDF) and its sum check | removed; inserts, entities and nesting stay. The sum check is in TestCADEngineTimeoutIsTheLongerRoute |
| same | cadsandbox_test.go TestValidateCADTimeouts and TestConfinedCAD* literals | updated to 21/324/521/186/217. These literals depend on CADIOAllowance (the validator adds it), so with 20 s I/O they had to move; the boundary rows were recomputed (render 522, DXF exact 211+2x317, over 212+2x317) |
| 906 | cadsandbox_test.go window test `grace != 906*time.Second` | replaced by the computed grace |
| 30s/316s/480s/162s/141s | docker-compose.yml cad-worker, configmap.yaml | updated |
| 30/316/480/60, 162/141, 534 | .env.example CAD block | rewritten in place (27 added comment lines, 29 removed) |
| 135 MiB IGES | docs/ERROR-CODES.md x3 | updated to 120 MiB |
| 129 MiB, 162 s, 141 s | docs/ERROR-CODES.md:122 | updated |
| "135 MiB limit", "63 MiB", "118 MiB" messages | internal/api/cad_ceiling_test.go:262-265, 401 | now formatted from the constants. :120 tests formatLimitMessage with a literal input and stays |
| 141557760 | scripts/cad-liveproof.sh:352-353 | updated to 125829120. 76-08 plans to replace every literal ceiling there with a derived one |
| unmeasured (cad) | scripts/cad-liveproof.sh:343, 346; internal/api/cad_ceiling_test.go:330 | updated to measured; the test also pins EstimatedMaxSeconds = 886 |
| 141557760 | scripts/cad-dxf-bounds.sh:1348, 1365, 2061, 2087, 2106, 4900 | kept, as pre-registered measurement inputs: the stack and bounds76 M3 cell was built at the then-ceiling. KNOWN_LIMIT from_bytes records the old value on purpose. Changing them would change what a re-derivation measures |
| 30/316/480/60, 886 | scripts/cad-dxf-bounds.sh:317, 662, 1364, 4196, 4623 | kept: RULE lines and budgets of past runs. The 886 budget is the reconciler-window constraint, still true |
| 316 | scripts/cad-spike.py:21 TIMEOUT_FLOOR_S | kept: the phase-73 spike harness floor, a historical tool that is not read by the product |
| 120s/900s/600s/3600s | scripts/cad-inworker-measure.sh:58 WANT_ENV | kept: a deliberately long measurement override, not a product value |
| 60 90 105 120 135 | scripts/cad-spike.sh:542 | kept: phase-71/73 ladder targets in MiB |
| README.md, docs/openapi.yaml, docs/MEASUREMENTS.md, internal/mcpserver, frontend/src, values*.yaml overlays, docker-compose.e2e.yml | none state a CAD timeout, the IGES ceiling or the SVG cap | nothing to change. MEASUREMENTS.md:126 names CAD_RSVG_TIMEOUT without a value |
| .planning (other phases' plans, summaries and evidence) | historical | not edited. 76-08-PLAN.md:119 and :239 already plan the liveproof literal removal |

## Tests and mutations

- RED (8450299): TestCADConstantsComeFromThe76Evidence failed on guard 30≠21, mesh 316≠324, render 480≠521, dxf 162≠186, rsvg 141≠217, io 60≠20, IGES 141557760≠125829120 and SVG 135266304≠140509184. TestCADEngineTimeoutIsTheLongerRoute passed at RED: the 3D route dominated before (886 vs 534) and still does (886 vs 661).
- Mutation: `CADDefaultRenderTimeout = 521*time.Second + time.Second` made TestCADConstantsComeFromThe76Evidence FAIL on render 522≠521 and engine 887≠886. Reverted from a byte copy, then re-run PASS.
- Chain mutations (TestCADGraceChainFollowsTheEngineTimeout, all FAIL as wanted, all reverted):
  - stop_grace_period 905s, keda stabilization 885 and compose CAD_RENDER_TIMEOUT 520s gave three errors;
  - compose line 199 CAD_ENGINE_TIMEOUT 887s gave "want the ConfigMap value 886s".
- Task 1 verify: PASS for TestCADConstantsComeFromThe76Evidence, TestCADEngineTimeoutIsTheLongerRoute, TestCADEngineTimeoutStaysUnderTheReconcilerWindow, TestCADPinnedConstantsComeFromTheEvidence, TestCADDXFConstantsComeFromTheEvidence, TestValidateCADTimeouts, TestErrorCodesDocStatesTheCADCeilingsOfTheCode and TestConfinedCAD*.
- Task 2 verify: PASS for TestChartGraceOutlivesAsynqShutdown and TestCADGraceChainFollowsTheEngineTimeout. `go test ./internal/queue/` ok and `scripts/ci-helm-checks.sh` rc=0. Loop over the DERIVED keys:

  ```
  CAD_GUARD_TIMEOUT=21s compose=ok configmap=ok
  CAD_MESH_TIMEOUT=324s compose=ok configmap=ok
  CAD_RENDER_TIMEOUT=521s compose=ok configmap=ok
  CAD_DXF_TIMEOUT=186s compose=ok configmap=ok
  CAD_RSVG_TIMEOUT=217s compose=ok configmap=ok
  ```

  The engine chain and `terminationGracePeriodSeconds: 906` checks are ok. cad_grace_chain_test.go has 0 `#` lines and 0 `//`.
- Task 3 verify: the plan's full command printed PASS (provenance test, envcfg, gofmt, .env.example line, estimate.go entries, transcript).

## Gate (evidence/76-04-ci-local.txt)

- Run from a fresh clone of a git bundle at 764e022, with deploy/tectonic-bundle/octoconv-bundle.ttb copied in.
- Environment: GOTOOLCHAIN=go1.26.4, python 3.14.8 first on PATH.
- The transcript has no tracked-changes warning (tracked_dirt=0) and lists no untracked files.
- Started at load1 7.43 (after waiting for it to fall below 8) and ended at 12.83.

| Tier | Result | internal/convert |
|---|---|---|
| helm | PASS (helm v4.3.0 local vs v4.2.3 pinned, warned in the transcript) | — |
| gate | PASS: gofmt clean, go vet clean | **371.636 s**; the tier ran 00:58:24–01:05:18 (414 s), under the 540 s stop line (D-19) |
| race | PASS | 601.670 s |

`ALL REQUESTED TIERS PASSED`. The transcript has no `--- FAIL` or `FAIL` line.

Checks over the plan's diff (146d0f1..HEAD):

- added `//` or `#` lines in *.go, *.yaml and *.yml: 0;
- the same count against main: 0;
- `git diff --exit-code main -- internal/convert/exec.go`: exit 0;
- .env.example against main: 27 added `#` lines, 29 removed.

## Deviations from Plan

1. **[Amendment] Render 521 s by the remainder rule for every 3D format** (render_remainder_all). The plan text predates this. The 76 table reads cad_render_timeout_s=521 as written in the DERIVED line, and .env.example states it as the remainder 886-21-324-20.
2. **[Amendment] IGES ceiling lowered by memory** (iges_memory_ceiling): 141557760 → 125829120. The plan's sweep grep did not include 141557760 and "135 MiB". The sweep above covers them.
3. **[Amendment] SVG cap from RULE bytes:** 129 → 134 MiB. The ERROR-CODES DXF paragraph and .env.example state 134 MiB.
4. **[Amendment] Per-route pod rule:** no constant here. .env.example and ERROR-CODES do not restate the pod numbers.
5. **[Rule 2] Widened KNOWN_LIMIT in the error docs:** the plan did not ask for it. The cad_too_complex row of ERROR-CODES now states `CAD_RENDER_TIMEOUT`, 521 s by default, and that a heavy assembly of any 3D format (STEP, IGES or STL) may be refused at that deadline well below its size ceiling. TestErrorCodesDocStatesTheCADCeilingsOfTheCode in cad_docs_sync_test.go (not in files_modified) now holds the value and the wording. .env.example's render lines carry the same limit.
6. **[Rule 1] TestValidateCADTimeouts literals:** the plan said these "stay valid", but ValidateCADTimeouts adds CADIOAllowance. With 20 s I/O, "render one second over the sum" (30+316+481+20=847) would have been accepted. Every literal moved to the new stage values, and the boundary rows were recomputed against 20 s.
7. **[Rule 1] internal/api/cad_ceiling_test.go:** this file is not in files_modified, but changing the provenance and the IGES ceiling broke two of its tests.
   - TestCADCeilingsAtTheDefaults expected "135 MiB". Its STEP, IGES and DXF messages are now formatted from the constants.
   - TestCADInspectReportsPerFormatCeilingAsMaxBytes expected unmeasured. It now expects measured with EstimatedMaxSeconds = 886.
8. **[Rule 1] scripts/cad-liveproof.sh:** the live INSPECT rows would fail after this plan. They asserted IGES max_bytes 141557760 and estimate_source=unmeasured, and now expect 125829120 and measured. 76-08 will replace the literal ceilings.
9. **values.yaml not modified:** grace 906 and stabilization 900 already equal engine + 20 and ≥ engine. The plan listed the file because E might have moved; it did not.
10. **TestCADGraceChainFollowsTheEngineTimeout goes further than the plan:** it also checks each of the five cad-worker stage vars in compose and the ConfigMap against the Go defaults, which closes the "stage variables equal their DERIVED values" truth by test rather than by grep.
11. **Transcript location:** ci-local ran with CI_LOCAL_EVIDENCE_DIR in scratch, so the clone stayed clean. Its log is byte-identical to the run's stdout and was copied to evidence/76-04-ci-local.txt.

## Known Stubs

None.

## Threat Flags

None. No new endpoint, auth path or file access. T-76-07 is mitigated by TestCADGraceChainFollowsTheEngineTimeout plus the existing IN-02 test. T-76-08 is mitigated by the same-commit flip (764e022 lists .env.example and internal/api/estimate.go), and estimateSourceFor still reports unmeasured for an override.

## Self-Check: PASSED

- Files exist: internal/e2e/cad_grace_chain_test.go, evidence/76-04-ci-local.txt.
- Commits are in HEAD, all SSH-signed (G), with no Co-Authored-By trailer:
  - 8450299 test
  - 07ed08b feat
  - 85df7a4 feat
  - 764e022 feat
