# Phase 73: CAD Class & Convert Slice, Confined - Context

**Gathered:** 2026-10-05
**Status:** Ready for planning
**Mode:** auto. The user pre-delegated every answer to Claude's recommended option on 2026-10-04 ("на все вопросы отвечаю твоими рекомендованными вариантами"). Each choice below is that recommendation, and each is logged in 73-DISCUSSION-LOG.md.

<domain>
## Phase Boundary

The `cad` class appears in the API whole, already confined. `convert` step/stp/iges/igs → stl runs end to end:
- API: accept, sniff with the phase 72 detector, ceilings;
- queue `cad`;
- `cmd/cad-worker`, with a guard pre-pass and the mesh stage under a measured Landlock ruleset;
- one binary STL in millimetres;
- the reconciler and error classification.

`cad` lands as one atomic change across `AllEngines()`, the queue, the enqueue route, the reconciler, the timeout/estimate/size maps, compose, the chart Deployment and the ScaledObject.

Out of scope:
- `render` and `measurements` output: phase 74.
- DXF: phase 75.
- Tuning `keda.cad.minReplicaCount`, the render time guard, the live gate: phase 76.

</domain>

<decisions>
## Implementation Decisions

### Worker shape: follow the newest class exactly
- **D-01:** `cmd/cad-worker` mirrors `cmd/tectonic-worker` / `cmd/document-worker` (the phase 64/70 pattern):
  - sandbox helper re-exec (`internal/sandbox`);
  - `preflightSandbox` and then `preflightEngine` before touching Postgres;
  - the same Landlock escape-hatch environment variable the other workers use, named in the refusal message;
  - asynq server on queue `cad`;
  - concurrency defaults to 1 (`CAD_WORKER_CONCURRENCY`), because the pod limit was derived for one job at a time (71 D-07).

  No new worker idioms.
- **D-02:** Engine invocation goes through the existing hardened `runCommand` (Setpgid plus SIGKILL of the process group). `internal/convert/exec.go` stays byte-for-byte unchanged, which is an invariant. A killed worker mid-job must leave no live engine process; this is proven by test, as in earlier classes.

### Engine and image
- **D-03:** The production engine is a Python helper hardened from the phase 71 measurement helper (`scripts/cad-measure/cad_mesh.py`). It is a separate production copy, not a shared file, so the 71 measurement image tag stays stable.
  - The planner picks the location.
  - The helper is minimal: a `guard` pre-pass and a `mesh` (→ binary STL) mode only.
  - It has no measurement flags and no measurement-only output.
  - Its output is a binary STL plus a small JSON status on stdout that the Go worker parses. The verdict is judged by content, never by exit code alone.
- **D-04:** `Dockerfile.cad-worker` is built from the APPROVED 71 lock files only:
  - `requirements-common.txt` (numpy) and `requirements-ocp-7.9.3.1.1.txt` (the OCP pin chosen by the user);
  - the same digest-pinned `debian:bookworm-slim`;
  - `USER nobody`;
  - `--require-hashes`;
  - no new pip or apt packages. Any extra package is a supply-chain checkpoint.

  VTK is not needed for mesh. It arrives with render in phase 74, pruned by the 71 keep-list. The image is built for both arm64 and amd64; the 71 keep-list fix proved the amd64 path.
- **D-05:** Tessellation constants and threads come from 71-DECISION:
  - quality `standard`;
  - `occt_parallel=off`.

  `LP_NUM_THREADS` is irrelevant to mesh and is set anyway for parity.

### Units
- **D-06:** The output STL is always in mm.
  - 71 proved OCP scales `units_inch.step` (6 in³ → 98322.384 mm³).
  - 71 did NOT prove scaling for `nist_ctc_05`, which uses lowercase `'inch'` and for which OCP `FileUnits` returns an empty name.
  - The helper reads the declared unit from the file text, as in 71. If OCP did not scale it, the helper scales explicitly.
  - A test on both files proves the mm output by a bbox comparison against a known dimension.

### Limits (from 71-DECISION after the Codex corrections on PR #32)
- **D-07:** The pod memory limit is 4Gi.
- **D-08:** Input byte ceilings are enforced at job creation with 413 `payload_too_large`, naming the format and ceiling in `message`:
  - STEP 63 MiB;
  - IGES 155 MiB.

  The worker re-checks them, which is defence in depth. Read the numbers from 71-DECISION's DECISION lines, not from memory.
- **D-09:** The guard pre-pass, run before mesh in the worker under the same sandbox, enforces two caps:
  - STEP entity cap 754762;
  - NAUO instance cap 1518.

  Both come from 71-DECISION. Exceeding either is a terminal failure with its own code. The "STL with a lying triangle count" bomb is already rejected by the phase 72 sniffer; a test in this phase proves it at the job level. Non-numeric coordinates and a self-referencing STEP must end as terminal failures, within measured memory and time.
- **D-10:** The mesh stage timeout is derived from the 71 evidence. It is the worst measured mesh wall at the ceiling size × a visible margin, and the derivation is written down. The research phase computes it. There is also a cgroup memory bound per engine run, following the existing `cgroup.go` pattern.

### Error classification (CADW-03)
- **D-11:** Error classes:
  - **Sandbox unavailable:** transient (retry).
  - **Unreadable input (OCP read failure):** terminal, with its own code.
  - **Empty output (0 triangles or no shapes):** terminal, with its own code.
  - **Over a ceiling or cap:** terminal, with its own code.
  - **Mesh stage timeout:** terminal, with its own code, and no retry loop.

  Existing async codes are reused where the client action matches, for example a generic engine timeout if one exists. A new `cad_*` code is added only where the client action differs. All codes go through `internal/worker/descriptors.go` and the ERROR-CODES async table with the existing sync tests. The research phase lists exactly which codes exist today. **Reversibility:** one-way. The async code strings go on the wire.

### Confinement (CADW-02)
- **D-12:** The mesh and guard stages get their own Landlock ruleset.
  - It starts from the 71 measured candidate: helper dir, `/opt/venv`, `/usr/bin/python3.11`, `/usr/lib/<arch>-linux-gnu`, `/usr/lib/python3.11`.
  - It is parametrised by architecture; this is the 71 verifier's W17 warning.
  - It is re-measured on the PRODUCTION cad-worker image by the established SANDBOX-PATHS method, with a removal table and a negative control.
  - A new `## cad` section goes into `.planning/research/SANDBOX-PATHS.md`. The set is held by a Go test and by `scripts/conf05-crosscheck.sh`.
  - An engine aimed at a file outside the job directory is refused, and a test proves it.
- **D-13:** The worker refuses to start without Landlock and names the escape-hatch variable. `preflightEngine` runs a real minimal STEP through guard and mesh under the sandbox, and judges the output STL's content: its triangle count and bbox.

### Atomic exposure (CADW-01)
- **D-14:** One atomic change covers all of the following:
  - `AllEngines()` + `cad`;
  - queue/route;
  - reconciler;
  - timeout/estimate/size maps;
  - the README classes table;
  - `/v1/formats`, which now shows the cad convert pairs;
  - `/v1/inspect`, whose estimate is marked `unmeasured`;
  - compose `cad-worker`;
  - the chart `deployment-cad-worker.yaml` and `scaledobject-cad.yaml`, using the existing template pattern.

  The phase 72 guard tests that assert "no CAD pair" are inverted or updated in that same change, deliberately, with the reason in the commit. The other nine classes and the metric labels `{engine,status}` / `{queue,state}` are unchanged and pinned by the existing goldens. **Reversibility:** costly. It touches every class registry and the deploy manifests.
- **D-15:** The chart ScaledObject follows the existing pattern with `minReplicaCount: 0`. Phase 76 tunes it, using the 71 cold-pull cost.

### Claude's Discretion
- File and package names, the helper location, the JSON status shape, and new error code names within D-11's rule.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase 71/72 inputs
- `.planning/phases/71-evaluation-gate/71-DECISION.md`: ceilings, caps, memory limit, threads, tessellation, Landlock candidates, image; read the DECISION lines.
- `.planning/phases/71-evaluation-gate/71-VERIFICATION.md`: warnings W1–W19, in particular W2, W10, W15, W17, W18.
- `.planning/phases/71-evaluation-gate/evidence/71-06-limits.txt`, `71-07-landlock.txt`, `71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt`.
- `scripts/cad-measure/cad_mesh.py`, `requirements-*.txt`, `Dockerfile.cad-measure`: the measurement helper, the approved lock files and the base image.
- `.planning/phases/72-engine-free-contract-foundations/72-CONTEXT.md`, `72-VERIFICATION.md`: sniffer, rejection codes, the measurements contract and role.

### Patterns
- `cmd/tectonic-worker/main.go`, `cmd/document-worker/main.go`: worker skeleton, preflight and escape hatch.
- `internal/sandbox/*`: helper re-exec and Landlock.
- `.planning/research/SANDBOX-PATHS.md`: measurement method and per-class sections.
- `scripts/conf05-crosscheck.sh`: the cross-check of sets.
- `internal/convert/exec.go`: `runCommand`, an INVARIANT that must not change.
- `internal/convert/cgroup.go`: the per-run memory bound.
- `internal/worker/descriptors.go`, `internal/reconciler/reconciler.go`, `docs/ERROR-CODES.md`: failure codes.
- `deploy/chart/octoconv/templates/deployment-tectonic-worker.yaml`, `scaledobject-tectonic.yaml`, `docker-compose.yml`: deploy pattern.
- The README engine-class table and its sync test (`readme_sync_test.go`).

</canonical_refs>

<code_context>
## Existing Code Insights
- Nine engine classes already follow one skeleton. Phase 64 (tectonic) and phase 70 (document) are the freshest references for adding a class atomically.
- The phase 72 detector (`convert.SniffCADWindows`) is already wired into create and inspect. Once a cad pair is registered, a valid STEP flows to enqueue instead of returning `unsupported_conversion`.
- `measurements` role exists; not used until phase 74.
- The 71 image tags are `octoconv-cad-measure:ocp7.9.3.1.1-{full,pruned}-dc621b94ee5d`. They are measurement-only and must not be shipped.
</code_context>

<specifics>
## Specific Ideas
- Judge preflight and engine success by output content, never by return code. This is a project rule from v1.15 and 71 D-11.
- Avoid optimistic answers for unexamined input. Guard caps count what the helper actually parsed, and a guard that could not count is a failure, not a pass.
- Do not predict a parser. Test the bombs against the real OCP reader in the image.
</specifics>

<deferred>
## Deferred Ideas
- Render, measurements output, DXF, minReplicaCount tuning, the render time guard, the live KEDA gate: phases 74–76.
- amd64 runtime measurement (only the build is proven).
- Lawyer review of copyleft libraries: an open, non-blocking item from 71.
</deferred>

---

*Phase: 73-cad-class-convert-slice-confined*
*Context gathered: 2026-10-05*
