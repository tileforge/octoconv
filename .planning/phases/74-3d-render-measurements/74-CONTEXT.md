# Phase 74: 3D Render & Measurements - Context

**Gathered:** 2026-10-05
**Status:** Ready for planning
**Mode:** auto. On 2026-10-04 the user delegated every answer to Claude's recommended option. Each choice is logged in 74-DISCUSSION-LOG.md.

<domain>
## Phase Boundary

This phase adds `render` for step/stp/iges/stl → png on the existing `cad` class from phase 73: the same queue, the same `cad:job` task type, the same worker. Each successful render job produces two outputs:
- the PNG, with role `primary`;
- the measurements JSON, with role `measurements` and schema `octoconv.measurements/v1` from phase 72.

The client sees both outputs or neither. `quality` also applies to step/iges → stl conversion.

Out of scope:
- DXF render: phase 75.
- Render time guard, minReplicaCount and measured timeouts: phase 76.
- Docs, OpenAPI and MCP: CADW-06, phase 76.

</domain>

<decisions>
## Implementation Decisions

### Pipeline and stages
- **D-01:** Stages run as guard → mesh → render. Each stage is its own confined helper with its own measured Landlock ruleset.
  - **guard:** unchanged from phase 73. For STL input it gives `ok` with no CAD caps; the phase 72 sniffer has already validated the STL.
  - **mesh:** OCP. It writes the tessellated STL plus a measurements draft (bbox, solids, closed_solids, volume, area, unit, unit source) computed with OCP. For STL input there is a numpy STL stats path with no OCP: exact bbox, and volume and area only when the mesh is watertight.
  - **render:** VTK + OSMesa, with no OCP. It reads only the STL from mesh and writes the PNG. This is the measured ruleset gap from 71-07: render does not need `/opt/venv` OCP libs, only VTK.
- **D-02:** The engine image gains the VTK wheel from the approved 71 lock (`requirements-vtk.txt`, vtk 9.7.1). The image is pruned by the 71 architecture-neutral keep-list (`scripts/cad-measure/vtk-keeplist.txt` and the `vtk_keeplist.py` logic, copied into the production build stage, not shared). No new packages are added. Any apt additions must be a subset of the 71 measurement image list, for example `libosmesa6`; anything else is a supply-chain checkpoint.
- **D-03:** Failure classes:
  - A render-stage failure is transient (retryable); the mesh stage already produced a valid STL.
  - A mesh-stage failure stays terminal, as in phase 73.
  - The triangle cap 4014975 (from 71-DECISION) is checked after mesh and before render. Exceeding it gives terminal `cad_too_complex`.
  - The render stage timeout is derived from the 71 render evidence, using the same method as phase 73. The stacking hazard (61.6 s vs 1.2 s) is deferred to phase 76, and the derived timeout must cover the measured worst case below the caps.

### Render options (closed, validated at create → 422 `invalid_options`)
- **D-04:**
  - `view`: one of iso (default), front, back, left, right, top, bottom. Camera definitions come from 71-DECISION: orthographic, Z up, fit by bbox, never `ResetCamera`.
  - `size`: a square integer from 256 to 2048, default 1280. The 2048 ceiling is a recommendation; research validates its render RSS and time against the 71 evidence. If those exceed the pod budget, lower it and record why.
  - `quality`: draft, standard or fine. The tessellation constants are relative to the bbox diagonal, using the 71 STACK.md values. For convert as well as render, a finer level gives at least as many triangles, tested on the same part.
  - `background`: `opaque` (default, white #FFFFFF) or `transparent` (an RGBA PNG whose background alpha is 0).
  - Any unknown key or value is a 422.
- **D-05:** `quality` on convert (step/iges → stl) is the same option with the same constants. Phase 73's default (`standard`) stays the default.

### Pixel judgment (preflight and worker)
- **D-06:** The worker judges the PNG by content before upload: the frame is not empty, the part fits with a margin, and the view reported by the helper matches. The 71 frac-only rule falsely rejected a thin real part (71 W15), so the judge uses projected-extent/aspect-aware criteria. At minimum it applies the 71 "sliver" mode, extended to aspect below about 0.1. Research tunes this on the corpus so that every real corpus part and the thin STL prefix pass, and an empty frame fails. A failed judgment is a terminal output failure, not silently uploaded. Preflight renders a fixture and judges it by pixels.

### Two outputs, atomically (MEAS-01)
- **D-07:** Both outputs are produced, validated and uploaded before the job is marked done.
  - The PNG passes the pixel judge.
  - The JSON passes `measurements.Validate`.
  - If either fails, the job fails and no output row is visible. This is proven by an inversion test at the repository level: force the second write to fail and assert the client sees zero outputs.
  - `handleGetJob` `download_url` points to the PNG; the role selection from phase 72 already guarantees this.
  - A webhook payload gets the same treatment.

### Measurements honesty (MEAS-02..04)
- **D-08:** The rules of the phase 72 validator are binding.
  - Volume and area come only from closed solids. Otherwise they are null, with a reason code.
  - STL: area is computed from triangles when the mesh is watertight, and both volume and area are null with `stl_not_watertight` otherwise. This follows the phase 72 pairing rule; 72-VERIFICATION flagged it for this phase.
  - Units follow the source:
    - STEP and IGES: the declared unit, with source `declared`. OCP reports values in mm after scaling (phase 73 R-01), but the document reports the declared unit and the values in that unit. Research decides whether to convert back or to report mm with the declared unit noted; the honest choice must keep "STEP in inches with assume_unit=mm stays inches".
    - STL: `unspecified`, with source `none`.
  - `assume_unit` fills only a missing unit, with source `client_assumed`. It never overrides a declared unit.
  - IGES inch scaling is unproven (73 GAP). Report the declared unit and record the gap; do not assert a scale.

### Confinement
- **D-09:** The render stage's Landlock set is measured on the production image by the 71/73 method: a removal table, a negative control, and judgment by pixels. The result is recorded in SANDBOX-PATHS `## cad` and held by the Go test and conf05-crosscheck. The mesh and guard sets are re-measured only if their argv or imports change; the measurements code adds OCP calls, so re-check the mesh set.

### Claude's Discretion
- Helper names and the JSON handoff format between stages.
- Whether measurements are emitted by mesh or by a separate small stage, as long as D-01's "render has no OCP" holds.

</decisions>

<canonical_refs>
## Canonical References
- `.planning/phases/71-evaluation-gate/71-DECISION.md`: renderer, views, camera constants, pixel criteria, triangle cap, tessellation, image keep-list.
- `.planning/phases/71-evaluation-gate/71-VERIFICATION.md`: W15 (thin-part pixel finding), W17 (arch-specific Landlock).
- `.planning/phases/71-evaluation-gate/evidence/71-04-render.txt`, `71-06-*` (render RSS and time), `71-07-landlock.txt` (cad_render candidate set).
- `scripts/cad-measure/cad_render.py`, `pixelcheck.py`, `vtk_keeplist.py`, `vtk-keeplist.txt`, `requirements-vtk.txt`: the measured render implementation and its judge.
- `.planning/phases/72-engine-free-contract-foundations/72-VERIFICATION.md`: the STL area pairing warning. `docs/MEASUREMENTS.md`, `internal/measurements/*`: the contract.
- `.planning/phases/73-cad-class-convert-slice-confined/73-VERIFICATION.md` and the 73-0x SUMMARYs: the class skeleton, sets, codes and liveproof scripts.
- `cmd/cad-worker/*`, `Dockerfile.cad-worker`, `internal/convert/cad*.go`, `internal/worker/descriptors.go`, `scripts/cad-liveproof.sh`, `scripts/sandbox-liveproof.sh`.
</canonical_refs>

<deferred>
## Deferred Ideas
- DXF render: phase 75.
- Render time guard for stacked duplicates, minReplicaCount, measured timeouts, OpenAPI/MCP docs: phase 76.
- amd64 runtime Landlock measurement. IGES inch scaling proof (no corpus file).
</deferred>

---
*Phase: 74-3d-render-measurements · Context gathered: 2026-10-05*

<research_resolutions>
## Resolutions after research (2026-10-05, auto-selected recommendations under the user's delegation)

These supersede any conflicting D-xx.

- **R-01 (render timeout):** render = 480 s. The engine timeout becomes 886 s, which stays under the fleet reconciler's 900 s window; pod grace is 906 s. Do NOT raise `RECONCILER_ACTIVE_STALE_AFTER` fleet-wide. A render *timeout* is deterministic, so it is **terminal** `cad_too_complex`, with a distinct sentinel and message. This refines D-03: other render-stage failures (process crash, sandbox) stay retryable. Stacked-duplicate inputs above about 40 MiB stay bounded only by this timeout until the phase 76 render guard; record a GAP.
- **R-02 (size and triangle cap):** the size ceiling is 2048. The triangle cap is lowered to **3,806,000** so the 70% rule holds at 2048 px, instead of 4,014,975. Wave 0 must re-measure the 2048-px cost at `LP_NUM_THREADS=2` on the production image and re-derive the cap from that measurement. A different number in the evidence wins over 3,806,000. Add the STL byte ceiling derived from the cap, about 191 MiB as research proposes, re-derived after Wave 0.
- **R-03 (options):** the closed render options are `view`, `size`, `quality`, `background` and **`assume_unit`**; MEAS-04 requires `assume_unit`. Convert accepts only `quality`. `quality` on an STL source gives `opts_not_applicable`.
- **R-04 (units):** convert measurements back to the declared unit in Go, using the factor table (mm 1, cm 10, m 1000, inch 25.4, foot 304.8, micrometre 0.001). Never report mm with `unit_source: declared`. For `nist_ctc_05` (lowercase `CBU:inch`), Wave 0 probes the scale OCP actually applied, and the code divides by that factor. If the probe cannot establish the factor, report the document unit as `unspecified` with source `none` and record a GAP, rather than asserting a scale.
- **R-05 (edge vocab):**
  - A declared unit outside the vocabulary becomes `unit=unspecified`, `unit_source=none`, and the raw text goes into a diagnostic log (not the document).
  - A zero-volume watertight or closed mesh is reported with reason `invalid_solid`. Allow `invalid_solid` for STL as an **additive** change to the vocabulary, updating `docs/MEASUREMENTS.md` and the doc-sync test in the same commit. Never report 0.
- **R-06 (render rejection code):** reuse `cad_empty_result` with a new sentinel and message for a PNG that fails the pixel judge. No new wire code.
- **R-07 (atomic outputs):** follow the research design.
  - Add `Repo.AddOutputs`, one transaction for both rows.
  - Order: validate both artefacts in memory, upload both objects, call `AddOutputs`, then `MarkDone`.
  - The DB-gated inversion test includes a naive-sequence control, so it can fail against the naive version.
  - Add worker, hook and webhook tests as research lists.
- **R-08 (guard and STL):** Go skips cad-guard for STL; the guard helper stays unchanged. STL area is computed in chunks.
- **R-09 (render Landlock):** add a separate `cadRenderReadOnly` slice, measured on the production image. Its paths may equal the mesh set, but the exec grant is `cad-render`. "No OCP in render" is enforced by the helper's imports, and a test greps the helper for OCP imports.
- **R-10 (vtk legitimacy):** vtk 9.7.1 is hash-locked and was user-approved at the 71 legitimacy gate. A `checkpoint:human-verify` for it may be answered by the orchestrator under the delegation, quoting the 71 approval. Verify that the hashes are byte-equal to `requirements-vtk.txt`.
- **R-11 (port fidelity):** the production `cad-render` is a verbatim port of the 71 `cad_render.py` render paths. The pruned keep-list was collected from those paths. Run prune-identity against the 71 renders as a gate.
- **R-12 (build on the P1 fix):** plans build on the tree after the PR #34 P1 fix (mesh completeness, the `incomplete` verdict, `requireSTLMatchesVerdict`) lands.
</research_resolutions>
