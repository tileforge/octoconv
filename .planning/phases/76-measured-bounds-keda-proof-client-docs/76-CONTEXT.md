# Phase 76: Measured Bounds, KEDA Proof & Client Docs - Context

**Gathered:** 2026-10-13
**Status:** Ready for planning
**Mode:** `--auto`. Every choice below is the orchestrator's recommended option, under the user's v1.16 delegation (memory `v116-delegated-decisions`). Each choice is logged in 76-DISCUSSION-LOG.md.

<domain>
## Phase Boundary

This is the last phase of v1.16. It measures the class's bounds instead of deriving them, proves the class live on images built from the final commit, and writes the client contract down with tests that hold it to the code:

- **CADW-04:** `CAD_ENGINE_TIMEOUT` is measured over every cad route (3D convert, 3D render, DXF svg, DXF pdf, DXF png) at the input ceilings in the production worker. It is paired with the input ceilings, the formula and the worst cell are written down, and the inspect estimate for `cad` flips to `measured`.
- **CADW-05:** KEDA scale-from-zero and downscale-survival for `cad` are proven on OrbStack k8s with timestamps. `keda.cad.minReplicaCount` is chosen from a measured image size and cold pull. The live gate of all classes, `cad` included, ends `ALL PASS` on images built from the final commit. Killing the worker mid-render leaves zero live processes. A re-run of the same input on the same arch gives the same JSON, and the PNG matches within a tolerance.
- **CADW-06:** client docs: `docs/CAD.md`, error codes, `docs/openapi.yaml`, MCP descriptions and the reference frontend's format table, each held by a test like `docs/ERROR-CODES.md` already is.
- **PREV-01:** the preview contract (SEED-007) is written and pinned to CAD: format, sizes, background, framing, and behaviour on non-previewable input. Anything CAD did not force a decision on is marked "not fixed".

Out of scope:
- the preview contract on other classes (PROJECT.md: after it is fixed on CAD);
- `dwg` (CADV2-01);
- paper-space layouts and page options;
- raising the fleet reconciler window or the chart's global `MAX_UPLOAD_BYTES`;
- amd64 runtime measurement (no native amd64 host).

</domain>

<decisions>
## Implementation Decisions

### Carried forward (binding, not re-decided)
- **From 73/74/75:**
  - Stage pattern: guard → mesh → render for 3D; guard → cad-dxf → rsvg (ink check) → rsvg (PDF or PNG) for DXF.
  - `ValidateCADTimeouts` refuses to start when either route's stage sum plus I/O exceeds `CAD_ENGINE_TIMEOUT`.
  - A render timeout is terminal `cad_too_complex` (74 R-01).
  - `exec.go` is byte-identical to main.
  - No code comments (user rule).
  - Evidence transcripts carry their commit and tracked-dirt headers, and a FAIL or NOT RUN is never counted as a pass.
- **Current values:**
  - `CAD_ENGINE_TIMEOUT=886s` (30 guard + 316 mesh + 480 render + 60 I/O), derived, not measured.
  - DXF route sum: 30 + 162 + 2 × 141 + 60 = 534 s.
  - Pod limit: 4Gi, 2 CPU.
  - Chart: `keda.cad` threshold "1", maxReplicaCount 2, cooldown 90, scaleDownStabilizationSeconds 900, minReplicaCount 0; terminationGracePeriodSeconds 906.
- **Ceilings:**
  - STEP 63 MiB, IGES 135 MiB, STL 182 MiB (`CADMaxSTLBytes` 190840832), DXF 118 MiB (`CADMaxDXFBytes` 123731968).
  - The chart's global `MAX_UPLOAD_BYTES`/`INSPECT_MAX_UPLOAD_BYTES` stays 100 MiB (73 R-04).
- **DXF caps:** 170031 expanded inserts, 850155 expanded entities, nesting 16, SVG 129 MiB, PDF backstop `CADMaxDXFPDFBytes`.
- **Reconciler window:** 900 s, fleet-wide, not raised (74 R-01).

### Measured timeout (CADW-04, SC1)
- **D-01: The engine timeout is the maximum over routes, built from measured stage deadlines.**
  - Each stage deadline is `ceil(1.5 × W)`, where W is the slowest measured wall of that stage at the admissible maximum. W includes every run in this phase's evidence and every loaded run already on record: 75-05, and 0og's helper 123.61 s on row C and ink raster 96.60 s.
  - A loaded outlier therefore raises the bound. It is never argued away.
  - `CAD_ENGINE_TIMEOUT = max over the five routes of (sum of that route's stage deadlines + measured I/O)`.
  - Cross-check: the result must also be ≥ 1.5 × the slowest measured end-to-end in-worker attempt wall of every route.
  - The 60 s I/O allowance stops being an assumption. It is measured as S3 download plus upload of the ceiling input and outputs, with the same 1.5×.
  - — **Reversibility:** costly — the value propagates under IN-02 to every `queue.NewClient()` service, compose, chart, `.env.example`, the unique TTL and the grace-period chain.
- **D-02: The worst-case inputs are the admissible maxima, built at the exact ceiling bytes.**
  - 3D:
    - the STEP ceiling (63 MiB), including the stacked-copies shape that drove 74's 437.36 s render at 40 MiB;
    - the IGES ceiling;
    - the STL ceiling at the triangle cap;
    - render at size 2048, quality `fine`, for every view class that changes cost.
  - DXF:
    - row C (exactly 850155 entities);
    - an insert-cap row that the guard admits (≤ 850155 expanded entities);
    - a replicate built to exactly 123731968 bytes, closing 75-05's unmeasured top 1.26%.
  - L170031 (850156 entities) stops being a derivation row. It is kept as a guard-refusal check, since the production guard refuses it.
  - — **Reversibility:** reversible.
- **D-03: Load control, repetitions and statistic.**
  - N = 5 per cell, on the production cad-worker image, with `--cpus 2 --memory 4g`, the measured Landlock rulesets and `LP_NUM_THREADS=2`.
  - The statistic is the maximum, not p95. For a cap-bounded cost a percentile would measure the corpus, not the engine.
  - Each run's header records the load average and free VM disk.
  - A run starts only with no compose stack, k8s proof, `go test` or docker build running. It aborts below the 6 GiB VM disk floor.
  - Pre-registered: the rule lines (cells, N, statistic, margin, refusal thresholds) are written into the evidence before the first row, as 75-05 did.
- **D-04: If the maximum exceeds the 886 s budget, the Path-B lever fires; the timeout is not raised.**
  - If any route needs more than 886 s (the fleet's 900 s window minus the existing margin), lower the input ceiling of the route that breaks it.
  - That includes a render-only ceiling per format if stacked STEP copies at 63 MiB break the render bound. In that case an operation-aware ceiling (`render` vs `convert` per format) is added to the size map, enforced at create (413 `payload_too_large`) and in the worker.
  - There is no heuristic "stacking guard": a measured ceiling is the bound the project already trusts. This resolves the 74 T-74-07 GAP.
  - — **Reversibility:** one-way if an operation-aware ceiling is added, because it is a published 413 contract and changes inspect's `max_bytes` per offer.
- **D-05: Pairing and provenance.**
  - The DERIVED lines name the worst cell, the formula and each ceiling.
  - Go constants (`CADDefault*Timeout`, ceilings) are pinned by tests that parse the DERIVED lines, as with `CADDXFPDFTextFloorPt`.
  - `.env.example` replaces "DERIVED, not measured" with the measured provenance.
  - `engineEstimateSource[cad]` flips to `measured`, and `engineMeasuredDefaultSeconds[cad]` gets the value, in the same commit as the number (the probe/tectonic precedent). `TestEstimateSourceMatchesEnvExampleProvenance` holds it.
  - The grace chain is re-derived from the value: asynq ShutdownTimeout, `terminationGracePeriodSeconds`, compose `stop_grace_period`, and `scaleDownStabilizationSeconds` ≥ the engine timeout.
- **D-06: The Go-side RSS gap is closed by measuring the whole pod.**
  - The peak of the Go worker plus stage, measured in the production container at the ceiling rows (PDF judge and pixel judge included), replaces the 24.9 MiB Go-worker share assumption.
  - Rule: worst stage peak + measured Go peak ≤ 0.70 × pod limit.
  - Keep 4Gi if it fits (expected: 2510 + 61 MiB against 2867 MiB).
  - Otherwise lower the ceiling (Path-B). Do not raise the pod.
  - This closes `GAP go_worker_rss_dxf`.

### DXF PDF backstop
- **D-07: Fix it.** A PDF over `CADMaxDXFPDFBytes` maps to `cad_too_complex`, not `cad_empty_result`. The wire code already exists, so the fix is a one-line mapping plus a test that fails first. It is unreachable within the measured bounds, but the docs-sync test must be able to state the true code. — **Reversibility:** reversible.

### KEDA scale-from-zero and minReplicaCount (CADW-05, SC2)
- **D-08: Scripts follow the class precedent.**
  - `scripts/keda-cad-loadproof.sh` and `scripts/keda-cad-downscale-survival.sh`, cloned from the pandoc and ocr pair; the frozen scripts stay byte-unchanged.
  - Loadproof: 0→N→0 with timestamps per transition, and N reaches `maxReplicaCount` (2).
  - Downscale-survival: a long render job in flight while the queue drains and the cooldown passes. The job ends `done`, with no pod killed mid-job.
  - A cheap calibration step runs first and asserts reachability, because ClusterIP is unreachable from the host: port-forward for a single replica, an in-cluster driver pod for anything multi-replica.
- **D-09: Measure the production image, then choose `minReplicaCount: 0`.**
  - Re-measure on the production `cad-worker` image (it now carries VTK, ezdxf and rsvg; 71-08 measured only the evaluation image), using `scripts/cad-image-size-measure.sh` or its method:
    - uncompressed and compressed size;
    - cold pull from a local `registry:2`;
    - startup preflight time (every stage proven both ways).
  - Recommended value: 0. The cold start (pull + preflight) is small next to minute-scale cad jobs and the 886 s bound, and an idle 4Gi/2 CPU pod per cluster costs more than the cold start.
  - The measured cold start and the network-time table are written beside the value in `values.yaml` and docs/CAD.md.
  - Recorded as "OrbStack shared store: the pull is near 0, registry cold pull measured separately", as in v1.7 phase 33.
  - — **Reversibility:** reversible — a values knob.

### Final-commit live gate (CADW-05, SC3)
- **D-10: Gate scope.**
  - Run from a fresh clone at the final commit, with images built from that clone: `ci-local.sh helm gate race docker-build e2e`, `sandbox-liveproof.sh` over every class it knows, the tectonic liveproof, and `cad-liveproof.sh` extended with the new rows.
  - The runs are serial, never concurrent with a k8s proof.
  - One wrapper transcript names the commit and ends `ALL PASS`. Any NOT RUN is listed and is not a pass.
- **D-11: Worker kill mid-render.**
  - A new row next to the existing mesh-stage WORKER_KILL.
  - It kills the worker while `cad-render` runs, and separately while `rsvg-convert` runs a DXF PNG.
  - It asserts a process census of zero engine processes after the restart, and that the job reaches its terminal state without waiting the full timeout.
- **D-12: Determinism.**
  - Same input, same arch, run twice through the API.
  - The measurements JSON is byte-identical after canonical encoding.
  - The PNG is compared by a pre-registered tolerance, never by hash:
    - same dimensions and same alpha mode;
    - the same J1–J4 verdicts;
    - ink bbox within 1 px;
    - mean absolute channel difference ≤ 1/255;
    - at most 0.1% of pixels differing by more than 16/255.
  - The planner may tighten the tolerance from observed repeat variance before the run, never loosen it after.
  - Rows cover 3D (iso and top, opaque and transparent) and DXF (opaque and transparent).

### Client docs held by tests (CADW-06, SC4)
- **D-13: `docs/CAD.md` is the client entry point.** It covers:
  - formats and aliases (`stp`, `igs`);
  - operations and pairs;
  - closed render options with ranges and defaults, and their applicability per format;
  - the two outputs (`primary` PNG, `measurements` JSON) and their atomicity;
  - per-format ceilings, plus the effective ceiling under the chart's 100 MiB transport cap;
  - the measured timeout and the inspect estimate;
  - error codes, by link to ERROR-CODES;
  - a "Known limits" section.
- **D-14: The "Known limits" section is mandatory, and every limit with a constant cites it.**
  - R-1 kerning: text up to 3.3% narrower.
  - R-9: characters outside DejaVu are U+FFFD in PNG/PDF; the client SVG keeps them exact.
  - R-10: PDF text below 0.00469174 pt may be missing from the text layer (`CADDXFPDFTextFloorPt`).
  - R-11: sub-pixel features can refuse a render; use a larger size.
  - DXF rulesets and every bound were measured on arm64 only.
  - TEXT oblique angle is not drawn.
  - Italic or bold chosen by family name is drawn regular (Book).
  - A missing font file falls back to `DejaVuSans.ttf`.
  - Near-white paint (any channel ≥ 250, `cadBackgroundCutoff`) is no ink.
  - Empty means no ink on a white 2048 page (`CADMaxRenderSize`) on every route, transparent PNG included. A transparent PNG of only white lines is refused.
  - Fully transparent, white, too-faint, zero-area or glyphless drawings are `cad_empty_result`.
  - IGES inch scaling is unproven (73 GAP).
  - Overlapping STL shells or closed solids are summed without an overlap check (74 verification).
  - Any limit the measurement run adds.
  - The Go-side RSS gap and the unmeasured top 1.26% are closed by D-02/D-06 instead of documented. If they cannot be closed, they are documented.
- **D-15: A sync test, both ways, in the `cad_docs_sync_test.go` style.**
  - It parses docs/CAD.md and checks, against `OperationClasses()`, the option schema in `cadopts.go`, the ceiling constants, the timeout constants, the error-code descriptors and the limit constants:
    - every pair, option, ceiling, code and limit constant in the code appears in the doc;
    - every one the doc names exists in the code.
  - It is pure Go, with no python or docker, so it adds milliseconds to `internal/convert`'s budget.
- **D-16: openapi, MCP and frontend.**
  - **openapi:** `operation=render`, the cad pairs, the render opts schema, `role=measurements` and the cad error codes, held by the existing openapi sync tests extended to cad.
  - **MCP:** the `convert_file` description and its `operation` and `opts` field descriptions name `render` and point to `GET /v1/formats` `operations` and docs/CAD.md. A test asserts the description is derived from, or matches, the registry, not prose that can drift.
  - **Reference frontend:**
    - The format table is built from the `/v1/formats` `operations` key, grouped by operation, so cad render pairs appear.
    - The form can send `operation` (`api.ts` already supports it).
    - A finished render job lists its outputs by role.
    - A vitest test proves the table is derived from the response, with no hard-coded format list. The existing `TestFormatAliasesMatchFrontend` keeps the aliases.
  - — **Reversibility:** reversible.

### Preview contract (PREV-01, SC5)
- **D-17: `docs/PREVIEW.md`, a cross-class contract pinned on CAD.**
  - **Fixed by CAD:**
    - operation `render`, output PNG with role `primary`;
    - a square `size` from 256 to 2048, default 1280;
    - `background` `opaque` (white #FFFFFF, the default) or `transparent` (alpha 0);
    - framing: fit with a margin and centred. 3D uses an orthographic bbox fit; DXF is top-down and letterboxed, never cropped or stretched;
    - 3D views: iso (default), front, back, left, right, top, bottom. DXF: top only (`view` → 422 `opts_not_applicable`);
    - a second output, `measurements`, written atomically with the PNG;
    - the pixel judge before upload;
    - non-previewable input: an empty or invisible drawing → `cad_empty_result`; unreadable → `cad_unreadable`; over-complex → `cad_too_complex`; an unsupported format → 422 at create. No placeholder image is ever returned.
  - **Marked "not fixed":**
    - whether other classes use `render` or a separate `preview` engine;
    - non-square sizes;
    - JPEG/WebP output;
    - page or frame selection for multi-page or time-based input;
    - colour profile and gamma;
    - DPI metadata;
    - sizes below 256;
    - cross-arch determinism;
    - font fallback outside DejaVu;
    - caching and re-use of previews.
  - A sync test holds the fixed values against the code constants.
  - SEED-007 gets a pointer to the contract and stays dormant for the other classes.
  - — **Reversibility:** one-way for the fixed values — a published client contract.

### Environment and gates (constraints, not options)
- **D-18: Live-proof hygiene.**
  - OrbStack shared VM: ≥ 6 GiB free before and during every run.
  - Never prune volumes. Never touch other projects' containers, images or volumes (`log-format-postgres-1` etc.).
  - Host port 8090 belongs to another project. Live proofs use the 18090 override.
  - Live proofs run from a fresh clone with `deploy/tectonic-bundle/octoconv-bundle.ttb` copied in.
  - Compose and k8s never run at the same time.
  - k8s is enabled for the KEDA proofs and restored to its prior state afterwards, with `:dev` tags including `webhook-worker`.
  - Local gates only; no GitHub Actions.
- **D-19: CI budget pressure is a constraint here, not a work item.**
  - `internal/convert` takes 370–520 s against the 10m gate budget (race 20m).
  - Every test this phase adds to `internal/convert` must be pure Go and fast. Measurement and live checks live in scripts.
  - Each plan's gate transcript records the package time. If the gate tier exceeds 540 s at a normal load, stop and report rather than raise the budget again.

### Claude's Discretion
- Script and helper names, the evidence file layout and the wrapper's shape.
- The exact docs/CAD.md section order and prose.
- The frontend's visual layout for the operation-grouped table and the outputs list.
- Whether the end-to-end attempt wall comes from worker logs, job timestamps or a wrapper. It must be in-worker, not client round trip.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope and requirements
- `.planning/ROADMAP.md` §Phase 76: goal and SC1–SC5.
- `.planning/REQUIREMENTS.md`: CADW-04, CADW-05, CADW-06, PREV-01.
- `.planning/PROJECT.md` §Current Milestone v1.16 and Key Decisions: measured-timeout precedents (audio, av, ocr, pandoc, probe, tectonic) and the Path-B lever.
- `.planning/seeds/SEED-007.md`: the preview seed this phase pins on CAD.

### Bounds and timeouts, the derivations being replaced
- `.env.example` (CAD block, lines ~570–620): the current derivation, the GAP text, and the IN-02 rule.
- `internal/convert/cad.go`: `CADDefault*Timeout`, `CADIOAllowance`, the ceilings, `CADMaxRenderSize`, `CADMaxDXFPDFBytes`.
- `internal/convert/cadsandbox.go`: `ValidateCADTimeouts` and the two route sums.
- `internal/api/estimate.go`: `engineEstimateSource`, `engineMeasuredDefaultSeconds`.
- `.planning/phases/73-cad-class-convert-slice-confined/73-CONTEXT.md` R-07, and `73-VERIFICATION.md`: mesh 316 s; the IGES inch gap.
- `.planning/phases/74-3d-render-measurements/74-CONTEXT.md` R-01/R-02, `74-RESEARCH.md` (render timing table, stacked copies), `74-04-SUMMARY.md` (T-74-07 GAP), `74-VERIFICATION.md` (STL shell overlap note).
- `.planning/phases/75-dxf-2d-pipeline/75-CONTEXT.md`: R-1, R-7, R-9, R-10, R-11.
- `75-05-SUMMARY.md`: bounds derivation, the 1.26% gap, `GAP go_worker_rss_dxf`, the L170031 off-by-one.
- `75-VERIFICATION.md` §Anti-Patterns and §Known accepted limits.
- `.planning/phases/75-dxf-2d-pipeline/evidence/75-05-bounds.txt`: DERIVED lines.
- `.planning/phases/75-dxf-2d-pipeline/deferred-items.md`: all resolved; context only.
- `scripts/cad-dxf-bounds.sh`, `scripts/cad-render-measure.sh`, `scripts/cad-rss-time-measure.sh`, `scripts/cad-image-size-measure.sh`: the measurement tooling to reuse.
- `.planning/phases/71-evaluation-gate/71-DECISION.md` §Образ (image size, cold pull 6.38 s, network table; input for minReplicaCount).

### Quick tasks after phase 75 (facts this phase carries)
- `.planning/quick/261012-gos-close-phase-75-deferred-items-job-dir-wr/261012-gos-SUMMARY.md`: job-dir I/O helpers; the gate timeout raised from 5m to 10m.
- `.planning/quick/261012-o2d-codex-pr36-p2-preserve-dxf-text-opacity-/261012-o2d-SUMMARY.md`: text opacity.
- `.planning/quick/261012-qds-codex-pr36-round2-oblique-font-style-dxf/261012-qds-SUMMARY.md`: font limits; docs sweep table (what is held and what is left for 76); the race tier at 20m.
- `.planning/quick/261012-u2s-codex-pr36-round3-fully-transparent-dxf/261012-u2s-SUMMARY.md`: nothing_visible.
- `.planning/quick/261013-0og-codex-pr36-round4-dxf-emptiness-judged-b/261013-0og-SUMMARY.md`: emptiness by pixels; two rsvg stages; the loaded-run outliers 123.61 s and 96.60 s.

### KEDA and live gates
- `scripts/keda-pandoc-loadproof.sh`, `scripts/keda-pandoc-downscale-survival.sh`, `scripts/keda-ocr-*.sh`: the templates to clone.
- `deploy/chart/octoconv/values.yaml` (`keda.cad`, `cadWorker`), `deploy/chart/octoconv/templates/scaledobject-cad.yaml`, `deployment-cad-worker.yaml`, `_helpers.tpl` (minReplicaCount validation).
- `scripts/cad-liveproof.sh` (WORKER_KILL, the DXF rows), `scripts/sandbox-liveproof.sh`, `scripts/ci-local.sh`.
- Memory `orbstack-k8s-live-proof.md`, `orbstack-clusterip-unreachable-from-host.md`, `local-gates-no-github-ci.md` (under `~/.claude/projects/-Users-apaderin-dev-octoconv/memory/`).

### Docs and their sync tests
- `docs/ERROR-CODES.md`, `docs/MEASUREMENTS.md`, `docs/openapi.yaml`, `README.md`.
- `internal/convert/cad_docs_sync_test.go`, `doc_sync_test.go`, `readme_sync_test.go`, `format_alias_sync_test.go`.
- `internal/errcode/openapi_sync_test.go`, `internal/api/openapi_paths_sync_test.go`, `openapi_responses_sync_test.go`.
- `internal/mcpserver/mcpserver.go`, `tools.go` (convert_file description and fields).
- `frontend/src/formats.ts`, `api.ts`, `App.tsx`, `formats.test.ts`.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `scripts/cad-dxf-bounds.sh run` already re-derives the DXF bounds on the production image, with RULE/BOUND_ROW/DERIVED lines and a disk watchdog. Extend it rather than writing a new harness. The 3D side has `cad-render-measure.sh` and `cad-rss-time-measure.sh`.
- `scripts/cad-image-size-measure.sh` (71-08): registry:2 cold pull and the size method.
- The Go constant pinned by a test that parses DERIVED (`TestCADDXFPDFTextFloorComesFromTheEvidence`) is the pattern for every new measured constant.
- `TestEstimateSourceMatchesEnvExampleProvenance` already ties inspect provenance to `.env.example`.
- The keda pandoc and ocr script pair, with an EXIT-trap teardown.
- cad-liveproof's WORKER_KILL row with a process census.

### Established Patterns
- Provisional-then-measured: the provenance flag flips in the same commit as the number (OCR 300→744, pandoc 90→65, probe 180→600, tectonic).
- Path-B: lower the input ceiling instead of raising the timeout or the pod (audio, av, ocr, pandoc).
- Evidence headers carry commit and tracked dirt, plus the arch, image id, load and disk.
- IN-02: an engine-timeout value must be byte-identical across every `queue.NewClient()` service in compose and the chart.

### Integration Points
- `internal/convert/cad.go` constants, and `cmd/cad-worker/main.go`, `cmd/api/main.go:448`, `internal/queue/client.go:188` (unique TTL).
- Compose, chart configmap and values, `.env.example`.
- `internal/api/estimate.go`.
- The size map (`defaultMaxFormatBytes[cad]`), if D-04 fires.
- The worker failure mapping for the PDF backstop (`internal/worker/descriptors.go`, `caddxf.go`).
- New `docs/CAD.md` and `docs/PREVIEW.md`, plus their sync tests.
- `internal/mcpserver`, `frontend/src`.

</code_context>

<specifics>
## Specific Ideas
- Worst cells to build: a 63 MiB STEP of stacked copies (the 74 LinearSlide shape), an IGES at 135 MiB, an STL at the triangle cap, DXF row C, an admitted insert-cap row, and a DXF replicate at exactly 123731968 bytes.
- The off-by-one row L170031 becomes a guard-refusal check, so the guard's refusal at 850156 is proven live.
- Determinism rows: the same STEP rendered iso and top, opaque and transparent; the same DXF opaque and transparent; twice each.

</specifics>

<deferred>
## Deferred Ideas
- The preview contract on the other classes (image, document, html, av, …): a future milestone, from SEED-007.
- amd64 runtime measurement of bounds and Landlock rulesets: needs a native amd64 host. It is documented as arm64-only.
- Splitting or speeding up `internal/convert`'s test time (370–520 s against 10m): a separate hygiene task if the gate keeps climbing.
- A heuristic stacking guard for 3D render: not built. A measured ceiling bounds the cost (D-04).
- Raising `MAX_UPLOAD_BYTES` in the chart to expose the full IGES, STL and DXF ceilings: an operator decision, outside this phase.
- Todos matched by `todo.match-phase` (amd64 first-deployment preflight, /proc grant entries, soffice socket grant, sweep tests and host tmp): each scored 0.2, below the 0.4 fold threshold, and is not folded.

</deferred>

---

*Phase: 76-measured-bounds-keda-proof-client-docs*
*Context gathered: 2026-10-13*
