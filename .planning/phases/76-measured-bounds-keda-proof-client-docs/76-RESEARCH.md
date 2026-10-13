# Phase 76: Measured Bounds, KEDA Proof & Client Docs - Research

**Researched:** 2026-10-13
**Domain:** in-repo measurement harnesses, Go constants pinned to evidence, Helm/KEDA live proofs on OrbStack k8s, doc-vs-code sync tests (Go + vitest)
**Confidence:** HIGH for codebase facts (read this session); MEDIUM for the stacking-cost extrapolation and the k8s first-run risks; LOW where tagged `[ASSUMED]`

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

#### Carried forward (binding, not re-decided)
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

#### Measured timeout (CADW-04, SC1)
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

#### DXF PDF backstop
- **D-07: Fix it.** A PDF over `CADMaxDXFPDFBytes` maps to `cad_too_complex`, not `cad_empty_result`. The wire code already exists, so the fix is a one-line mapping plus a test that fails first. It is unreachable within the measured bounds, but the docs-sync test must be able to state the true code. — **Reversibility:** reversible.

#### KEDA scale-from-zero and minReplicaCount (CADW-05, SC2)
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

#### Final-commit live gate (CADW-05, SC3)
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

#### Client docs held by tests (CADW-06, SC4)
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

#### Preview contract (PREV-01, SC5)
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

#### Environment and gates (constraints, not options)
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

### Deferred Ideas (OUT OF SCOPE)
- The preview contract on the other classes (image, document, html, av, …): a future milestone, from SEED-007.
- amd64 runtime measurement of bounds and Landlock rulesets: needs a native amd64 host. It is documented as arm64-only.
- Splitting or speeding up `internal/convert`'s test time (370–520 s against 10m): a separate hygiene task if the gate keeps climbing.
- A heuristic stacking guard for 3D render: not built. A measured ceiling bounds the cost (D-04).
- Raising `MAX_UPLOAD_BYTES` in the chart to expose the full IGES, STL and DXF ceilings: an operator decision, outside this phase.
- Todos matched by `todo.match-phase` (amd64 first-deployment preflight, /proc grant entries, soffice socket grant, sweep tests and host tmp): each scored 0.2, below the 0.4 fold threshold, and is not folded.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| CADW-04 | `CAD_ENGINE_TIMEOUT` замерен по обеим операциям и спарен с потолком размера входа; оценка времени в inspect помечена как замеренная | §Measurement architecture (stage harness = `cad-dxf-bounds.sh` extended + in-worker compose rows); §Pinning pattern (`cadDerivedValue`); §IN-02 propagation list; Pitfalls 1–6 (stacking, evidence overwrite, Go-side time, I/O) |
| CADW-05 | Живой гейт всех классов, включая `cad`, на образах из финального коммита; scale-from-zero `cad` через KEDA доказан; `minReplicaCount` для `cad` выбран осознанно | §KEDA proof (clone pandoc/ocr pair, seed `cad` queue, overlay key, port 18099/15443); §Image size (production image 1172325149 bytes); §Final gate wrapper (ci-local e2e port/`down -v` blocker); §Kill-mid-render rows; §Determinism comparator |
| CADW-06 | Клиентская документация: `docs/CAD.md`, коды ошибок, OpenAPI, MCP-описания и таблица форматов фронтенда | §Docs sync tests (existing harness `cad_docs_sync_test.go`, `errcode` openapi tests); MCP drift found (`JobOutput.Role` names no `measurements`); frontend has no table today, vitest not run by any gate |
| PREV-01 | Контракт превью (SEED-007) записан и закреплён на CAD; всё, что CAD не заставил решить, помечено «не зафиксировано» | §Preview constants (verbatim: sizes, background, margins 0.06 / 0.03, cutoffs); D-07 mapping fix so the non-previewable table is honest |
</phase_requirements>

## Project Constraints (from CLAUDE.md and the orchestrator's hard rules)

- Go 1.26, chi, asynq + Redis, PostgreSQL 18, S3/MinIO; not revisited. Run every Go gate as `GOTOOLCHAIN=go1.26.4` (local default is go1.27.1, verified this session: `go version go1.27.1 darwin/arm64`; `GOTOOLCHAIN=go1.26.4 go version` → `go1.26.4`).
- **No code comments at all** in planned code, scripts, YAML or tests (user global rule, stricter than the project CLAUDE.md). Rationale goes to SUMMARY/PR body. 75-05 verified "no comment lines besides the shebang" in new scripts; repeat that check. The cloned KEDA scripts must be stripped of the source scripts' long comment blocks.
- `internal/convert/exec.go` stays byte-identical to `main`.
- `go vet ./...` clean, `gofmt -l .` empty; library code (`internal/*`) never logs.
- HTTP layer never leaks internal error text; error mapping by `errors.Is` sentinels.
- Commits: SSH-signed via 1Password (stop and report if signing fails), no `Co-Authored-By` trailer, `.planning` files via `git add -f`, never push.
- Never pass `model` to the Agent tool (HTTP 403).
- Executors run sequentially on the main tree (no worktrees); live gates use compose project `octoconv`.
- Live/measurement hygiene: ≥ 6 GiB VM free (abort below); never prune volumes; never touch other projects' containers/images/volumes; never `go clean -cache`; never restart OrbStack; host 8090 is taken → `docker-compose.override.yml` mapping API to `127.0.0.1:18090` + `CAD_LIVEPROOF_API_BASE=http://127.0.0.1:18090`; fresh clone with `deploy/tectonic-bundle/octoconv-bundle.ttb` copied in and tracked dirt 0; `docker compose -p octoconv stop`, never `down -v`; local gates only; a FAIL or NOT RUN is never a pass.

## Summary

Everything this phase needs already exists as a pattern in the repo; almost nothing is greenfield. The DXF bounds harness (`scripts/cad-dxf-bounds.sh`, pre-registered RULE → BOUND_ROW → DERIVED lines, disk watchdog, production `engine` stage of `Dockerfile.cad-worker`) is the template for CADW-04. The Go side already pins constants to evidence via `cadDerivedValue` (regex `^DERIVED,key=<k>,value=(\d+),`). The inspect provenance flip is a two-map edit held by `TestEstimateSourceMatchesEnvExampleProvenance`. KEDA proofs have a mature script pair per class. Docs sync tests have three styles to copy (`errcode/doc_sync_test.go` both-ways tables, `convert/cad_docs_sync_test.go` per-constant substrings, `convert/format_alias_sync_test.go` cross-language file scan).

Three findings change what the planner must schedule. (1) **The stacking cost is probably not bounded by bytes.** The production `cad-render` runs `vtkCleanPolyData` → `vtkPolyDataNormals(Splitting)` + `vtkFeatureEdges(NonManifold)` on the mesh; 71 measured 36 stacked copies at 214128 triangles render in 61.57 s versus 1.18 s unstacked. Stack depth, not bytes, drives the cost, and an STL can stack a tiny part arbitrarily deep far below its 182 MiB ceiling. The existing `stl-tile` generator tiles (spreads) copies, so D-02's "STL at the triangle cap" built with it would miss the worst shape entirely. D-04's Path-B ("lower the ceiling") may have no ceiling that works; a cheap pre-registered stacking probe must run before the full measurement, with a decision checkpoint if bytes do not bound cost. (2) **The final gate cannot run as written on this host.** `ci-local.sh`'s e2e tier hard-refuses when anything listens on :8090 (now an OrbStack k8s LoadBalancer for another project's `identity-stand`), hard-codes `http://localhost:8090`, and tears down with `down -v`, which the hygiene rules forbid. The e2e tier needs an override-port and stop-teardown knob before D-10 can end `ALL PASS`. (3) **OrbStack k8s is already on** and runs another project (`identity-stand`, `envoy-gateway-system`). "Restore prior state" therefore means leave k8s enabled and remove only `octoconv` and `keda`. This is also the first time a cad-worker pod will ever run in k8s (73–75 proved only compose).

**Primary recommendation:** run the phase in this order: pure-code prep (D-07 fix, generators for exact-byte and stacked shapes, PNG comparator, harness modes, ci-local knobs); stacking probe and decision checkpoint; the full measurement; landing the numbers (constants, IN-02 chain, provenance flip); docs and tests; liveproof rows; image size and KEDA proofs; the fresh-clone final wrapper last.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Stage deadlines, engine timeout, ceilings (constants) | Worker engine lib (`internal/convert`) | API (`internal/api` size map, estimate) | `ValidateCADTimeouts`/`ConfinedCAD` enforce at worker start; API enforces ceilings at create and publishes inspect estimate |
| Operation-aware ceiling (only if D-04 fires) | API (`defaultMaxFormatBytes`, `sizeLimit`) | Worker (`cadMaxBytes` re-check in `CADRenderConverter.Convert`) | 413 at create is the client contract; worker re-check is the defence-in-depth precedent |
| Measurement (stage walls, peaks) | Scripts (docker, production `engine` stage) | Evidence files under `.planning/phases/76-*/evidence/` | D-19: measurement never lives in Go tests |
| In-worker attempt wall, whole-pod peak, I/O | Compose stack (cad-worker container) | Postgres (`jobs.started_at/finished_at`, `client_usage_daily.wall_seconds`) | D-01 demands in-worker, not client round trip |
| Grace chain / IN-02 | Config (compose, chart configmap/values, `.env.example`) | `cmd/cad-worker` (ShutdownTimeout = engine + 10 s) | Value must be byte-identical across every `queue.NewClient()` service |
| Scale-from-zero | Helm chart + KEDA ScaledObject | Prometheus queue-depth metric | `query: sum(octoconv_queue_depth{queue="cad", state=~"pending|active|retry"})` |
| Client contract docs | `docs/` (CAD.md, PREVIEW.md, ERROR-CODES.md, openapi.yaml) | Go sync tests; vitest for the frontend | Docs are contracts only when a test holds them |
| Frontend format table | Browser (`frontend/src/formats.ts`, `App.tsx`) | API `GET /v1/formats` `operations` key | Derived from response, no hard-coded list |

## Standard Stack

No new libraries. Everything is in-repo tooling or already-installed tools.

### Core (in-repo, reuse)
| Asset | Path | Purpose | Why |
|-------|------|---------|-----|
| DXF bounds harness | `scripts/cad-dxf-bounds.sh` (`smoke|run|floor`, `--out FILE`) | RULE/BOUND_ROW/DERIVED, per-stage container under `--cpus 2 --memory 4g --memory-swap 4g --network none --user nobody`, `measure_wrap.py`, `sandbox-probe` confinement, `MIN_FREE_GIB=6`, builds `octoconv-cad-worker:engine75-<hash>` and removes it on exit | 75-05 precedent; CONTEXT says "Extend it rather than writing a new harness" [VERIFIED: scripts/cad-dxf-bounds.sh:10-35,36-69] |
| Corpus generator | `scripts/cad-corpus-gen.py` kinds `step-copies, step-nauo, stl-tile, stl-ascii, dxf-replicate, dxf-minsert, iges-from-step, dxf-combo, dxf-far-point` | worst-cell inputs | [VERIFIED: scripts/cad-corpus-gen.py:852] — targets are **MiB**, not bytes (`--targets ... comma separated MiB sizes`, line 2808): exact-byte padding is new work |
| 3D measure suites (evaluation image) | `scripts/cad-rss-time-measure.sh`, `cad-render-measure.sh` | ladder/resume/`--max-wall-s` chunking ideas | They run on the 71 **evaluation** image (`octoconv-cad-measure:*`), not production; borrow mechanics, not the image |
| Image size method | `scripts/cad-image-size-measure.sh` | registry:2 + fresh `docker:dind` per pull, digests pinned, `COLD_PULL`, `PULL_CALC` | Tied to 71's `PIN_DECISION` and variants pruned/full; needs an `--image REF` mode for the production image |
| Go worker RSS probe | `scripts/cad-go-worker-rss.sh` | `/proc/1/status` VmRSS/VmHWM via `docker compose exec` | Hard-codes `API=http://localhost:8090` and the image worker; adapt for cad-worker + 18090 |
| CAD liveproof | `scripts/cad-liveproof.sh` | `CAD_LIVEPROOF_API_BASE`, `proc_pids`, `mesh_census`, WORKER_KILL, CANCEL_RENDER, DXF rows | add new rows here |
| KEDA templates | `scripts/keda-pandoc-loadproof.sh`, `keda-pandoc-downscale-survival.sh`, `keda-ocr-*.sh` | KEDA 2.20.1 install, port-forward, queue seeding, pod event timeline, deletion-cost victim selection, EXIT-trap teardown | D-08 |
| Judges | `scripts/cmd/cad-pixel-judge`, `cad-pdf-check`, `cad-svg-check`, `cad-measurements-check`, `sandbox-probe` | reuse for determinism and bounds rows | |
| Local CI | `scripts/ci-local.sh` tiers `helm gate race docker-build e2e` (gate 10m, race 20m) | final wrapper | [VERIFIED: scripts/ci-local.sh:160-209] |

### Supporting (host tools, probed this session)
| Tool | Version | Use |
|------|---------|-----|
| go (pinned) | go1.26.4 via `GOTOOLCHAIN` | all gates |
| docker / OrbStack | 29.4.0, arm64 | builds, compose, k8s |
| helm | v4.3.0 (ci.yml pins v4.2.3; ci-local warns) | chart, KEDA |
| kubectl | v1.33.9, context `orbstack` | KEDA proof |
| jq | 1.8.2 | evidence parsing |
| node / npm | v26.11.1 / 11.20.0; vitest 4.1.10 in `frontend/package.json` | frontend test |
| poppler (`pdftotext`, `pdfinfo`, `pdffonts`) | present | cad-liveproof preflight requires them |
| python3 (host) | 3.9.6 | liveproof helper scripts (`cad-stl-judge.py`) |

**Installation:** none. `registry:2`, `docker:dind`, `golang:1.26-bookworm`, `prom/prometheus:v3.13.1` and the KEDA chart are pulled at run time (not present locally; see Environment Availability).

## Package Legitimacy Audit

No external package is installed by this phase (Go uses stdlib `image/png`, `encoding/json`; the frontend uses the already-locked vitest 4.1.10). The legitimacy gate was not run because there is nothing to check.

| Package | Registry | Age | Downloads | Source Repo | Verdict | Disposition |
|---------|----------|-----|-----------|-------------|---------|-------------|
| (none) | — | — | — | — | — | — |

**Packages removed due to [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none
Container images pulled at run time (`registry:2`, `docker:dind`, `kedacore/keda` chart 2.20.1) are pinned by digest/version in the existing scripts. Keep those pins.

## Architecture Patterns

### System Architecture Diagram

```
                ┌──────────── pre-registered RULE lines (cells, N=5, max, 1.5x, STOP thresholds)
                │
 corpus-gen ──► worst cells (exact ceiling bytes; stacked + tiled 3D; C / insert-cap / R123731968 DXF; L170031 refusal)
                │
     ┌──────────┴───────────────────────────────┐
     ▼                                          ▼
 STAGE HARNESS (docker run per stage,      IN-WORKER ROWS (compose, cad-worker 2 CPU/4g,
 production engine stage, sandbox-probe    18090 override, API job per row, container
 confinement, LP_NUM_THREADS=2)            restarted per row for a fresh cgroup)
   guard / mesh / stats / render /           attempt wall = finished_at - started_at (attempts=1)
   cad-dxf / rsvg ink / rsvg pdf|png /       whole-pod peak = /sys/fs/cgroup/memory.peak
   Go judges                                 Go peak = /proc/1/status VmHWM
     │ BOUND_ROW (wall, cg_peak)                 I/O probe (S3 get/put of ceiling sizes)
     └──────────────┬────────────────────────────┘
                    ▼
     DERIVED lines: per-stage ceil(1.5*W), io, engine = max(route sums), cross-check >= 1.5*e2e,
                    pod rule (stage peak + Go peak <= 0.70*4096), Path-B ceilings if > 886
                    │
         ┌──────────┼──────────────────────────────────────────────┐
         ▼          ▼                                              ▼
   Go constants  IN-02 chain (compose x13, configmap, values     docs (CAD.md, ERROR-CODES,
   pinned by     grace/stabilization, .env.example, estimate    PREVIEW.md, openapi, MCP,
   tests         maps, UniqueTTL)                               frontend) + sync tests
                    │
                    ▼
   images from final commit (fresh clone) ──► wrapper: ci-local tiers → sandbox-liveproof (9 classes)
                                              → tectonic coverage → cad-liveproof (+kill-render,
                                              +kill-rsvg-png, +determinism, +L170031 refusal) → ALL PASS
   separately, serial: image size + registry cold pull → k8s: keda-cad-loadproof → keda-cad-downscale-survival
```

### Recommended evidence layout
```
.planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/
├── 76-0X-stacking-probe.txt     # premise test (Pitfall 1), before the full run
├── 76-0X-bounds.txt             # RULE / BOUND_ROW / E2E_ROW / IO_ROW / POD_ROW / DERIVED / CHECK / GAP
├── 76-0X-image-size.txt         # IMAGE_SIZE / COLD_PULL / PREFLIGHT / PULL_CALC
├── 76-0X-keda-loadproof-*.log   # + sc-cad timeline csv
├── 76-0X-keda-downscale-*.log
├── 76-0X-liveproof.txt          # cad-liveproof incl. new rows
└── 76-0X-final-gate.txt         # wrapper transcript: commit, tracked_dirt, per-step result, ALL PASS
```

### Pattern 1: Pin a measured constant to its DERIVED line
**What:** each new or changed constant has a test that reads the DERIVED line from the evidence file and fails with a "re-derive, do not edit" message.
**Existing helper (verbatim regex):** `regexp.MustCompile(`(?m)^DERIVED,key=` + regexp.QuoteMeta(key) + `,value=(\d+),`)` [VERIFIED: internal/convert/cad_test.go:720-735]. Integers only; keys must be emitted as `DERIVED,key=<k>,value=<int>,derivation=...`.
**Retarget:** `TestCADDXFConstantsComeFromTheEvidence` reads `../../.planning/phases/75-dxf-2d-pipeline/evidence/75-05-bounds.txt` [VERIFIED: internal/convert/cad_test.go:737-770]. `TestCADPinnedConstantsComeFromTheEvidence` uses hard-coded wants, including `{"CADDefaultRenderTimeout", ..., 480, ".../74-RESEARCH.md Render stage timeout (chosen under the 900 s window, not measured)"}` and `{"CADIOAllowance", ..., 60, spikes + " cad_io_allowance_s"}` [VERIFIED: internal/convert/cad_test.go:146-172]. Both move to the 76 evidence for every value that changes.
```go
{"CADDefaultRenderTimeout", int64(CADDefaultRenderTimeout / time.Second), bounds76, "cad_render_timeout_s", script76},
{"CADIOAllowance", int64(CADIOAllowance / time.Second), bounds76, "cad_io_allowance_s", script76},
{"CADDefaultEngineTimeout", int64(CADDefaultEngineTimeout / time.Second), bounds76, "cad_engine_timeout_s", script76},
```
(key names are the planner's choice `[ASSUMED]`; the pattern is verified.)

### Pattern 2: The routes and their stage sums
Verified from code [VERIFIED: internal/convert/cad.go:138-171, cadrender.go:31-130, caddxf.go:42-90,98-184,230-314]:

| Route | Stages run (in order) | Deadline sum |
|-------|----------------------|--------------|
| 3D convert `step|iges → stl` | guard, mesh | g + m + io |
| 3D render `step|iges → png` | guard, mesh(+measure), render, Go PNG judge | g + m + r + io |
| 3D render `stl → png` | stats (uses **mesh** timeout, `cad-mesh stats`), render | m + r + io |
| DXF convert `dxf → svg` | guard, cad-dxf, Go SVG validation ×2, rsvg **ink check** (2048), Go ink judge | g + d + rsvg + io |
| DXF convert `dxf → pdf` | as svg, then rsvg PDF, Go PDF judge | g + d + 2·rsvg + io |
| DXF render `dxf → png` | as svg, then rsvg PNG, Go drawing judge | g + d + 2·rsvg + io |

So the five routes collapse to the two sums `ValidateCADTimeouts` already checks:
```go
if guard+mesh+render+CADIOAllowance > engine { ... }
if guard+dxf+2*rsvg+CADIOAllowance > engine { ... }
```
[VERIFIED: internal/convert/cadsandbox.go:158-165]. `CADIOAllowance` is a compile-time constant, not an env var: measuring I/O changes the constant and both checks follow.

**Go-side time is in the attempt but in no stage deadline.** It covers SVG validation, PDF decode, pixel judges and measurements JSON; 75-05 measured it at 23.98 s at L170031 [CITED: 75-05-SUMMARY.md "Go side"]. Fold it into the measured "I/O" term (S3 + Go-side, both 1.5×) or add a separate DERIVED term. The D-01 cross-check (≥ 1.5 × in-worker e2e) catches an omission either way.

### Pattern 3: Provisional → measured flip (same commit as the number)
```go
convert.EngineCAD: EstimateSourceMeasured,
```
plus `convert.EngineCAD: <value>,` in `engineMeasuredDefaultSeconds`; today `convert.EngineCAD: EstimateSourceUnmeasured,` and no entry in `engineMeasuredDefaultSeconds` [VERIFIED: internal/api/estimate.go:134-167]. `.env.example`'s own line must stop saying "not measured". The test logic is: own-line contains "placeholder" or "not measured" → unmeasured; else block contains "measur" [VERIFIED: internal/api/estimate_test.go:394-406]. Current line: `CAD_ENGINE_TIMEOUT=886s   # DERIVED, not measured in the production worker -- see the derivation above` (`.env.example:598`).

### Pattern 4: KEDA cad scripts (clone, strip comments, adapt)
- Image precondition `octoconv-cad-worker:dev` plus `octoconv-api:dev`, `octoconv-webhook-worker:dev` and `octoconv-mcp-http:dev` (chart `mcpHttp.enabled: true`) [CITED: memory orbstack-k8s-live-proof; values.yaml:256-260].
- **Seed `cad`** into `asynq:queues` before expecting a genuine zero. The pandoc script seeds only `image document html audio av ocr pandoc webhook` [VERIFIED: keda-pandoc-loadproof.sh:329]. Without `cad` the metric is absent; with `ignoreNullValues: "false"` KEDA falls back to `max(1, minReplicaCount)` = 1 replica [VERIFIED: templates/_helpers.tpl:171-173, scaledobject-cad.yaml], which looks exactly like a scale-from-zero that no job caused.
- Ports: use `API_LOCAL_PORT=18099`, `DB_LOCAL_PORT=15443` (both free, unused by other gates; 18090–18098 and 15434–15442 are taken by existing scripts).
- 0→2: threshold "1", `CAD_WORKER_CONCURRENCY=1` → queue depth 2 desires 2 replicas. Submit through the API port-forward. One API pod is enough because workers pull from Redis, so no ClusterIP load-balancing claim is involved. A calibration step still asserts `/healthz` 200 through the port-forward and a cad pod reaching `cad sandbox proven`.
- Downscale-survival needs `keda.cad.scaleDownStabilizationSeconds: 15` in `values-loadproof.yaml` (precedent: ocr added its key there; production 900 s would push the 2→1 event 15 min out) [VERIFIED: values-loadproof.yaml keys document/av/ocr/pandoc]. Production grace 906 is untouched by the overlay and is what the gate validates. Victim selection via `controller.kubernetes.io/pod-deletion-cost=-1000` on the busy pod, as in the ocr pair.
- LONG job: a render that runs well beyond 15 s stabilization plus metric propagation (Prometheus 5 s scrape in the overlay) but far below the render deadline. Calibrate offline first, like pandoc's `--calibrate-only` band check.

### Pattern 5: Kill-mid-render rows
Reuse the WORKER_KILL row (`docker kill --signal KILL octoconv-cad-worker`, wait for a new `StartedAt`, else `compose up -d cad-worker`, census, `wait_job "$KILL_JOB" "$ENGINE_TIMEOUT"`, `kill_to_done < ENGINE_TIMEOUT`, `webhook-worker` absent) [VERIFIED: scripts/cad-liveproof.sh:1527-1580]. Precedent result: `kill_to_done_s=116.1,cad_engine_timeout_s=886,...,events=->queued;queued>active;active>active;active>done` [VERIFIED: 75-10-liveproof.txt:123].
- Generalize `mesh_census` to all engine names: `cad-guard`, `cad-mesh`, `cad-render`, `cad-dxf` (python helpers matched by cmdline) and `rsvg-convert` (native, matched by comm).
- **DXF PNG kill must target the second rsvg run.** The PNG route runs `rsvg-convert` twice: first the ink check writing `ink.png`, then the PNG [VERIFIED: caddxf.go:292-314, 165-170]. Wait for an `rsvg-convert` whose `/proc/<pid>/cmdline` names the job's output PNG, not `ink.png`.
- Pick inputs where the target stage lasts long enough to catch (a render of several seconds; an rsvg PNG on the 50 MiB replicate or larger).

### Pattern 6: Determinism comparator (new small Go CLI)
`scripts/cmd/cad-png-compare` (stdlib `image/png`): dims, alpha mode (`cadPNGHasAlpha` equivalent: color type 4/6 or tRNS), MAD per channel, fraction of pixels with any channel diff > 16/255, ink bbox, plus the J1–J4 verdict through `convert.JudgeCADPNG` / `JudgeCADDrawingPNG` on both. Canonical JSON: decode with `json.Decoder.UseNumber()` into `any`, re-marshal (sorted map keys, number literals preserved). Avoid `jq -S`, which can reformat numbers. Prior evidence: the 71 evaluation renderer was pixel-identical across reruns on the same arch (`SAME,item=slide,kind=determinism,sha_equal=1,pixel_equal=1,diff_pixels=0`) [VERIFIED: 71-04-render.txt]. The planner may pre-register a tighter tolerance from this before the run (D-12 allows tightening only).

### Anti-Patterns to Avoid
- **Re-running `cad-dxf-bounds.sh run` without `--out`.** The default `EVID` is `75-dxf-2d-pipeline/evidence/75-05-bounds.txt` [VERIFIED: cad-dxf-bounds.sh:11-12], and `TestCADDXFConstantsComeFromTheEvidence` still reads it. Always write to a 76 file and leave 75 evidence intact.
- **Measuring 3D on the evaluation image** (`cad-rss-time-measure.sh` default) and calling it production. D-03 requires the production image.
- **Counting a stacked-shape timeout as "over the ceiling"** without first testing whether bytes bound the cost (Pitfall 1).
- **Comments in cloned KEDA scripts** (user rule; the source scripts are comment-heavy).
- **values.yaml prose for the cold-start table.** D-09 asks for it "beside the value in values.yaml", but YAML comments are code comments under the user rule; see Open Question 4.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Stage confinement in the harness | new Landlock wrapper | `scripts/cmd/sandbox-probe` with `PROBE_RO`/`PROBE_RW` as in `cad-dxf-bounds.sh:787-815` | already measured sets |
| Peak/wall capture | `docker stats` sampling | `scripts/cad-measure/measure_wrap.py` (WRAP line: wall, child RSS, cgroup peak, oom_kill) | sampling misses peaks |
| Evidence-to-constant link | hand-copied numbers | `cadDerivedValue` + DERIVED lines | 75 precedent |
| Pixel verdicts | new heuristics | `convert.JudgeCADPNG`, `JudgeCADDrawingPNG`, `JudgeCADDrawingInk` | production judge |
| Letterbox | recompute in bash | `cad-pixel-judge --print-letterbox` | same Go code as worker |
| KEDA install/teardown | new script shape | clone pandoc/ocr pair (EXIT trap, KEDA 2.20.1 re-verify) | four prior classes |
| Cold pull | timing `docker pull` on the host store | `cad-image-size-measure.sh` method (fresh dind per pull, registry:2 by digest) | host store makes pull ≈ 0 |
| In-worker wall | client-side stopwatch | `jobs.started_at` → `finished_at` with `attempts = 1`, cross-checked by `client_usage_daily.wall_seconds` for a fresh client | D-01 says in-worker; `started_at = COALESCE(started_at, now())` at MarkActive [VERIFIED: internal/jobs/repo.go:241,286] |

**Key insight:** every bound in this class was found by a measured counterexample. The unmeasured edit is where the next defect comes from, so reuse the measured paths.

## Runtime / Environment State (live-proof preconditions observed this session)

| Category | Found | Action |
|----------|-------|--------|
| OrbStack k8s | `orbctl config get k8s.enable` → `true`; namespaces `identity-stand`, `envoy-gateway-system` (another project, helm releases `eg`, `identity-stand`, `identity-stand-router`); no KEDA, no external.metrics APIService | Do not toggle k8s or restart OrbStack. "Restore prior state" = uninstall only `octoconv` + `keda` (+ KEDA CRDs/APIService if the chart left them) and leave k8s on. Never touch the other namespaces |
| Host port 8090 | held by `OrbStack` (127.0.0.1:8090, the k8s LoadBalancer of identity-stand's envoy); 18090, 18098, 18099, 15442, 15443 free | 18090 override for compose; 18099/15443 for the cad KEDA port-forwards |
| Compose octoconv | containers `octoconv-api`, `-cad-worker`, `-av-worker`, `-minio`, `-db`, `-redis` exist **Exited (0)** | fine; liveproof starts only what it needs |
| Images | present: `octoconv-api:latest` 144MB, `octoconv-cad-worker:latest` 1.17GB, `octoconv-av-worker:latest`; absent: all other octoconv workers, `golang:1.26-bookworm`, `registry:2`, `docker:dind`, `prom/prometheus` | docker-build/e2e/sandbox-liveproof rebuild everything (ocr-worker from-source tesseract ≈ 10–15 min) |
| Disk | host APFS 33 GiB free; docker images 12.5 GB, build cache 2.4 GB, volumes 7.0 GB | ≥ 6 GiB floor holds for now; full rebuild plus measurement inputs (~0.7 GB generated) plus dind pulls need margin; watchdog per run |
| Load | host load average 7.00 / 7.82 / 9.06 on 11 CPUs at research time | record in every header; loaded runs raise W (D-01), so expect larger stage deadlines than 75-05 |
| Corpus cache | `~/.cache/octoconv/cad-corpus/scaled` has DXF replicates (2–118 MiB), combo C (122179816 B), minsert ladder; **no step-copies, no iges copies, no STL stacks** | regenerate 3D cells; the cad-liveproof `REPL50` sha check needs the 50 MiB replicate (present) |

## Common Pitfalls

### Pitfall 1: Stacked geometry cost is not bounded by input bytes (the D-04 premise)
**What goes wrong:** the planner builds "STL at the triangle cap" with `stl-tile` (spread copies, `tile_triangles(blob, count, pitch, total)`) [VERIFIED: cad-corpus-gen.py:1104-1131] and measures a benign render. Or it lowers a byte ceiling that cannot bound a stacked input.
**Why it happens:** the render helper runs `vtkCleanPolyData()` then `vtkPolyDataNormals` with `SplittingOn()` and `vtkFeatureEdges` with `NonManifoldEdgesOn()` on the whole mesh [VERIFIED: cmd/cad-worker/helper/cad-render:114-130]. Coincident copies merge into shared vertices and edges with very many incident triangles. Measured (evaluation renderer, same filter chain): 36 stacked copies at 214128 triangles took `wall_s=61.57` stacked versus `wall_s=1.18` spread [VERIFIED: 71-06-render-stacking.txt:3-4]. The step-copies ladder render walls: 10 MiB 9.40 s, 20 MiB 61.26 s, 40 MiB 437.36 s, 80 MiB timeout at 900 s [VERIFIED: 71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt]. That is about ×7 per doubling. Extrapolated to 63 MiB: ≈ 1.5×10³ s `[ASSUMED: power-law extrapolation, exponent ≈ 2.8]`. An STL needs no OCP and can stack a 12-triangle part tens of thousands of times inside a few MiB.
**How to avoid:** pre-register and run a cheap **stacking probe** before the full run: a new `stl-stack` (pitch 0) generator kind, sweeping stack depth k at a small fixed part through the production `cad-render` under the harness limits, with a wall budget per cell. Also measure stacked STEP and stacked IGES at their exact ceilings (step-copies / iges-from-step are stacked by construction).
**Decision rule to pre-register:** if any stacked row below a candidate ceiling exceeds the render deadline that would fit 886 s, then "lower the ceiling" has no fixed point. Stop with a `checkpoint:decision` for the user. Do not invent a guard: D-04 and Deferred forbid it.
**Warning signs:** render wall grows with k at constant bytes; `t_total_s ≫ t_render_s` in the helper's own timing.

### Pitfall 2: Quality `fine` can push a ceiling cell over the triangle cap
**What goes wrong:** a STEP at 63 MiB meshed `fine` exceeds `CADMaxRenderTriangles = 3835382` and is refused (`cad_too_complex`) before render, so the "worst render cell" silently measures nothing.
**How to avoid:** for each 3D cell, record the triangle count and the verdict; the admissible maximum is the quality/size combination that is admitted. A refused cell is a guard-refusal row, never a timing row.

### Pitfall 3: The final gate's e2e tier refuses on this host and uses `down -v`
**What goes wrong:** `tier_e2e` loops `for port in 8090 9100` and returns 1 if anything listens; it polls `http://localhost:8090/healthz`; it sets `E2E_BASE_URL=http://localhost:8090`; teardown is `"${COMPOSE_E2E[@]}" down -v` [VERIFIED: scripts/ci-local.sh:168-185,188,206]. 8090 is held by OrbStack (k8s LB). With explicit `-f docker-compose.yml -f docker-compose.e2e.yml`, a `docker-compose.override.yml` is **not** auto-loaded, and base compose maps `"8090:8090"` and `"8091:8091"` [VERIFIED: docker-compose.yml:218-220].
**How to avoid:** a small ci-local change (env knobs, defaults unchanged): API host port / base URL (e.g. `CI_LOCAL_E2E_API_PORT`), an extra compose file list, and a teardown mode `stop` (no `-v`). Or run the e2e suite by hand in the wrapper with the same commands. Either way the wrapper records exactly which command ran. Leaving the tier red, or recording it NOT RUN, fails D-10.

### Pitfall 4: "The tectonic liveproof" does not exist as a script
**What goes wrong:** D-10 names it, but there is no `scripts/*tectonic*liveproof*` and `sandbox-liveproof.sh`'s class table has no tectonic row (`image, pandoc, ocr, av, audio, html, probe, document, cad`) [VERIFIED: scripts/sandbox-liveproof.sh class_row]. Live tectonic coverage today is the e2e suite (`internal/e2e/multifile_e2e_test.go`) through the confined tectonic-worker.
**How to avoid:** the planner names what proves tectonic in the wrapper, e.g. the e2e tier's tectonic tests run verbose with their PASS lines grepped, plus the tectonic-worker's `sandbox proven` startup log line. Mark the item explicitly; never leave it silently absent.

### Pitfall 5: Evidence overwrite and stale pins
See Anti-Patterns: default `EVID` of `cad-dxf-bounds.sh` is the 75 file. Also the hard-coded test literals that must move together with a new engine value: `cadsandbox_test.go:402-427` (886 tables), `TestCADEngineTimeoutStaysUnderTheReconcilerWindow` (requires grace `== 906*time.Second` and `engine + 20 < 910`, so engine ≤ 889) [VERIFIED: cadsandbox_test.go:635-643], `queue_test.go` `CADUniqueTTL`, `chart_grace_test.go` (grace ≥ timeout + 20), `compose_in02_test.go` (byte-identity across 13 compose services).

### Pitfall 6: Whole-pod peak needs a fresh cgroup per row
**What goes wrong:** `memory.peak` is cumulative per cgroup. A long-lived cad-worker container reports the max over every job since start, including the startup preflight.
**How to avoid:** restart the cad-worker container before each pod-peak row and record a preflight-only control peak. Read `/sys/fs/cgroup/memory.peak` and `memory.events` inside the container, and `VmHWM` from `/proc/1/status` for the Go-only share. Per-fd `memory.peak` reset exists on recent kernels [CITED: lkml.iu.edu/2607.3/13090.html], but needs an fd held open across the job, which `docker exec` cannot do. Restart instead.

### Pitfall 7: k8s first run of cad-worker
**What goes wrong:** 73–75 proved cad-worker only under compose. The chart's cad Deployment has no securityContext or volumes, `startupProbe` /metrics 5 s × 24 = 120 s, limits 2 CPU / 4Gi (requests default to limits, so 8 GiB reserved at N=2). The node already carries identity-stand (requests 1062Mi, limits 2132Mi). VM is 16 GiB, 11 CPUs.
**How to avoid:** the calibration step installs once, scales a cad pod, asserts `cad sandbox proven` in its log and Ready within the startup probe, and only then runs the timed proof. Landlock under k8s's default seccomp profile is `[ASSUMED]` to behave like compose (same kernel 7.0.14-orbstack).

### Pitfall 8: Loaded host
D-03 forbids starting with compose/k8s-proof/go test/docker build running, but identity-stand's k8s workload is permanent background load (load avg ~7–9). Record `uptime` load and `kubectl top`-free signals (`docker ps -q | wc -l`, as `host_noise` in prior scripts) in every header. Loaded rows raise W; that is the decided behaviour.

### Pitfall 9: Vitest is run by no gate
`ci.yml`/`ci-local.sh` have no frontend job (no `npm`/`vitest` in ci.yml, verified by grep). A frontend test nobody runs is a scan of nothing. The plan must run `cd frontend && npm test && npm run build` in its verification and in the final wrapper.

### Pitfall 10: Two images of the same tag
k8s `:dev` tags are made by `docker tag octoconv-<svc>:latest octoconv-<svc>:dev`. Record the image id used by the pod (`kubectl get pod -o jsonpath='{.status.containerStatuses[0].imageID}'`) and compare it with the one built from the commit (memory "check the same object").

## Code Examples

### D-07: PDF over the byte limit → `cad_too_complex` (test first)
Today the size check rejects with `cadPDFReject("the PDF is %d bytes, over the %d byte limit", st.Size(), maxBytes)` [VERIFIED: internal/convert/caddxfpdf.go:1484-1486]. That wraps `ErrCADPDFRejected`, which `cadFailureCode` maps to `CodeCADEmptyResult` [VERIFIED: internal/worker/descriptors.go:757-758]. `cadFailureCode` tests `ErrCADTooComplex` **before** `ErrCADPDFRejected` [VERIFIED: descriptors.go:733 vs 757], so double-wrapping is the minimal change:
```go
if st.Size() > maxBytes {
	return facts, nil, fmt.Errorf("%w: %w", ErrCADTooComplex, cadPDFReject("the PDF is %d bytes, over the %d byte limit", st.Size(), maxBytes))
}
```
The failing-first test lives in `internal/worker` (pinning table style, `pinning_test.go:1541` has the `ErrCADPDFRejected` row) and in `internal/convert` (`errors.Is(err, ErrCADTooComplex)` on an oversize file). `cadPDFDecode` is shared by `JudgeCADPDF` (scripts' `cad-pdf-check`, `cadsandbox.go:405`). Check that `cad-pdf-check`'s exit mapping does not depend on the sentinel. Decide the inflate cap (`errCADPDFInflateCap` → `cadPDFReject`, caddxfpdf.go:180-182) in the same commit; it is the same "too much output" class `[ASSUMED]`.

### Exact-byte padding (generator extension)
DXF: append a `999` comment group before `EOF` sized to hit the target; STEP: a `/* ... */` comment before `ENDSEC;` (or a bytes-neutral whitespace run). Assert `os.path.getsize == target` and re-run the reader check (the generator's existing `check=` field). L170031 stays a **refusal** row: production guard returns `too_complex` at 850156 entities.

### Docs sync test skeleton (pure Go, `internal/convert`)
Option vocabularies are unexported maps [VERIFIED: internal/convert/cadopts.go:21-28]:
```go
cadViews = map[string]bool{
	"iso": true, "front": true, "back": true, "left": true, "right": true, "top": true, "bottom": true,
}
cadQualities   = map[string]bool{"draft": true, "standard": true, "fine": true}
cadBackgrounds = map[string]bool{"opaque": true, "transparent": true}
cadUnits       = map[string]bool{"mm": true, "cm": true, "m": true, "inch": true, "foot": true, "micrometre": true}
```
Either place the CAD.md sync test in `package convert` (internal test, reads the maps directly), or add exported accessors. The internal test adds no API surface. Size constants: `CADMinRenderSize = 256`, `CADMaxRenderSize = 2048`, `CADDefaultRenderSize = 1280` [VERIFIED: cadopts.go:10-13]. Applicability rules come from `ValidateCADApplicability` (dxf: no option on convert; `view`/`quality` not on dxf render; `quality` not on stl render; convert takes only `quality`). Assert them by calling the function, not by restating them.

### Preview constants (for PREVIEW.md and its sync test)
- 3D margin: `MARGIN = 0.06` per side in the Python helper [VERIFIED: cmd/cad-worker/helper/cad-render:22], orthographic (`cam.ParallelProjectionOn()`), white background `renderer.SetBackground(1.0, 1.0, 1.0)` with alpha 0 when transparent (lines 151-177). The sync test can parse the `MARGIN = ` line from the helper file, pure Go.
- DXF letterbox: `k := float64(size) * (1 - 2*cadMarginMin) / math.Max(wMM, hMM)`, centred `(size - sw) / 2` [VERIFIED: cadpixel.go:111-122], `cadMarginMin = 0.03`, ink cutoff `cadBackgroundCutoff = 250`, `CADDXFInkSize = CADMaxRenderSize`, `CADInkChannelCutoff = cadBackgroundCutoff` [VERIFIED: cadpixel.go:14-31].
- Output roles: `OutputRolePrimary = "primary"`, `OutputRoleMeasurements = "measurements"`. DB check allows `'primary', 'report', 'preview', 'thumbnail', 'measurements'` (`0018_output_role_measurements.sql:3`). PREVIEW.md should state that `preview`/`thumbnail` are reserved and unused ("not fixed").

## State of the Art (in this repo)

| Old | Current target | Where |
|-----|----------------|-------|
| `CAD_ENGINE_TIMEOUT=886s` DERIVED (30+316+480+60) | measured max over routes | cad.go:30-34, `.env.example:570-603` |
| render 480 s "chosen under the 900 s window, not measured" | `ceil(1.5 × W_render)` at admitted maxima, or Path-B | cad_test.go:160 |
| I/O 60 s "assumption_A7" | measured S3 + Go-side, 1.5× | 73-01-spikes `DERIVED,key=cad_io_allowance_s,value=60,formula=assumption_A7` |
| DXF stage deadlines from 75-05 (162/141) | include the loaded outliers already on record: `ceil(1.5×123.61)=186`, `ceil(1.5×96.60)=145` as **floors** before this phase's rows (arithmetic on D-01's own W) | DXF sum floor = 30 + 186 + 2×145 + io = 506 + io |
| Go-worker share 24.9 MiB | measured whole-pod peak | 75-05 GAP lines (PDF judge up to 61 MiB) |
| docs/README.md says MEASUREMENTS "no engine emits it yet" | stale since phase 74; fix with the docs plan | docs/README.md |
| MCP `JobOutput.Role` "report, preview and thumbnail are secondary artifacts" | must name `measurements`; `preview`/`thumbnail` are never produced | internal/mcpserver/client.go:112 [VERIFIED] |

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Stacked render cost at 63 MiB STEP ≈ 1.5×10³ s (power-law extrapolation of the 71 ladder) | Pitfall 1 | If much lower, no Path-B; if higher, Path-B may have no fixed point. The probe resolves it |
| A2 | The production `cad-render` filter chain gives the same stacking cliff as the evaluation renderer | Pitfall 1 | Same filters were read in both files; magnitude unverified on production |
| A3 | Landlock works in the OrbStack k8s pod as in compose | Pitfall 7 | cad pod never Ready → KEDA proof blocked; calibration step catches it |
| A4 | Double-wrapping `ErrCADTooComplex` is the intended D-07 shape, and the inflate cap should map the same way | Code Examples | A wrong code for an unreachable path; the test pins whatever is decided |
| A5 | DERIVED key names (`cad_render_timeout_s`, `cad_io_allowance_s`, `cad_engine_timeout_s`, …) | Pattern 1 | cosmetic; must match the tests |
| A6 | `docs/README.md` index should list CAD.md and PREVIEW.md | Docs | minor |

## Open Questions (RESOLVED)

All six were resolved in planning with the recommended option (the user pre-delegated every choice). Each resolution is an AR entry under "Decisions auto-resolved" in the plan named below.

1. **What if bytes do not bound stacked render cost?** (Pitfall 1)
   - Known: stacking, not triangles, drives the cost; stack depth is free for STL.
   - Unclear: whether any ceiling makes the render deadline fit 886 s for adversarial stacks.
   - Recommendation: pre-registered probe → `checkpoint:decision`. Likely options: (a) the render deadline (terminal `cad_too_complex`, no retry burn) is the documented bound for adversarial stacks, and the ceiling bounds the measured benign worst cells, recorded as a Known limit; (b) Path-B per format where it does converge. The user decides; a guard is out of scope.
   - **RESOLVED: AR-1 (76-01-PLAN.md).** A pre-registered stacking probe classifies each 3D format. A byte render ceiling (Path-B) applies only to a `byte_bound` format; otherwise the render deadline, the window remainder R, is the bound and the limit goes to docs/CAD.md Known limits. Applied in 76-02, 76-03 and 76-05, and proven live by the STACKED_REFUSAL row of 76-08.
2. **ci-local e2e on this host.** Add env knobs (port, extra compose file, stop-teardown) or run the suite by hand in the wrapper. Recommend the knobs, defaults unchanged, so the transcript is the tier's own.
   - **RESOLVED: AR-2 (76-11-PLAN.md).** Four `CI_LOCAL_E2E_*` knobs whose defaults reproduce main exactly; the final gate sets port 18090, an extra ports file and the stop teardown.
3. **"The tectonic liveproof."** No script exists. Recommend: e2e tectonic tests (verbose, PASS lines captured) plus tectonic-worker sandbox startup line, named in the wrapper.
   - **RESOLVED: AR-3 (76-11-PLAN.md).** The e2e tier's `--- PASS` lines for TestMultiFileLaTeXProjectE2E and TestMultiFileRefusalsE2E plus the tectonic-worker landlock startup line from the same stack, recorded in a TECTONIC line. No new script.
4. **Cold-start table "beside the value in values.yaml" vs. the no-comments rule.** Recommend docs/CAD.md and evidence only. Keep values.yaml to the number, unless the user says YAML provenance comments are allowed. Similarly `.env.example`: its comment block is parsed by `TestEstimateSourceMatchesEnvExampleProvenance` (it requires "measur" in the block), so it is functional documentation and must change. Record this as the exception in the SUMMARY.
   - **RESOLVED: AR-4 (76-09-PLAN.md).** The table lives in docs/CAD.md `## Scaling` and the evidence, held to values.yaml by a subtest; values.yaml keeps the bare number. The `.env.example` provenance block changes in 76-04 as functional documentation.
5. **openapi "cad error codes".** Async codes are not an openapi enum today; docs/README.md calls publishing them "ERRC-V2-02, a separate, deferred step". Recommend listing the cad codes the worker can emit (`cad_too_complex`, `cad_unreadable`, `cad_empty_result`, `cad_mesh_timeout`, plus `input_too_large`, `engine_error`, `invalid_options`) in the render operation / `error_code` description. Hold it with a worker-package test that computes the set from `cadFailureCode` over every CAD sentinel, both ways, without creating the global enum.
   - **RESOLVED: AR-5 (76-06-PLAN.md).** The codes are listed in docs/CAD.md (76-06) and in openapi's error_code and render descriptions (76-07), held by a worker-package test over every declared ErrCAD sentinel, both ways. No global enum; ERRC-V2-02 stays deferred.
6. **Which W counts for 3D.** D-01's enumeration lists on-record runs only for DXF (75-05, 0og). Recommend: 3D W = this phase's production-image rows only; 71/73 rows are cited, not used, and this is pre-registered in RULE lines.
   - **RESOLVED: AR-6 (76-02-PLAN.md).** 3D W uses only this phase's production-image rows (76-02 stage rows and 76-03 in-worker stage walls); the 71, 73 and 74 rows are cited, not used. DXF W also includes the on-record walls D-01 names.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Docker / OrbStack (arm64) | everything live | ✓ | 29.4.0 | — |
| OrbStack k8s | KEDA proofs | ✓ (already enabled, shared with identity-stand) | kubectl client v1.33.9 | — |
| helm + kedacore repo | KEDA | ✓ | v4.3.0; repo `kedacore` registered | — |
| KEDA 2.20.1 chart | KEDA | network pull at run time | — | script re-verifies resolvable |
| go1.26.4 toolchain | all gates | ✓ (via GOTOOLCHAIN) | go1.26.4 | — |
| node/npm + vitest | frontend test | ✓ | v26.11.1 / 11.20.0 / vitest 4.1.10 | — |
| poppler, jq, python3, shasum | cad-liveproof | ✓ | jq 1.8.2, py 3.9.6 | — |
| `registry:2`, `docker:dind` | cold pull | ✗ locally (pulled by digest at run) | — | none needed (network) |
| `golang:1.26-bookworm` | every image build | ✗ locally | — | pulled on first build |
| `prom/prometheus:v3.13.1` | k8s chart | ✗ locally | — | pulled by k8s |
| Host port 8090 | ci-local e2e default | ✗ (taken) | — | 18090 override + ci-local knob (Pitfall 3) |
| tectonic bundle `deploy/tectonic-bundle/octoconv-bundle.ttb` | docker-build, fresh clone | ✓ (gitignored, `.gitignore:11`) | — | copy into the clone |

**Missing with no fallback:** none blocking. **Missing with fallback:** port 8090 (override), base images (network pull).

## Validation Architecture

> `.planning/config.json` has `"nyquist_validation": false`. Included because the orchestrator asked for a proposal.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go `testing` (stdlib), vitest 4.1.10 (frontend) |
| Config file | none (Go); `frontend/vite.config.ts` |
| Quick run command | `GOTOOLCHAIN=go1.26.4 go test -count=1 ./internal/convert/ -run 'CAD|Docs|Preview' ./internal/api/ -run 'Estimate|OpenAPI' ./internal/worker/ -run 'CAD|Pin' ./internal/mcpserver/ ./internal/errcode/` |
| Full suite command | `scripts/ci-local.sh helm gate race` (+ `docker-build e2e` in the final wrapper); `cd frontend && npm test && npm run build` |

### Phase Requirements → Test Map
| Req | Behavior | Type | Command / Evidence | Exists? |
|-----|----------|------|--------------------|---------|
| CADW-04 | constants equal DERIVED lines of the 76 bounds file | unit | `go test ./internal/convert -run 'CADPinned|CADDXFConstants|CAD.*Evidence'` | ❌ retarget (Wave 0) |
| CADW-04 | engine ≥ every route sum, < 900, grace = engine+20 | unit | `-run 'ValidateCADTimeouts|ReconcilerWindow'` | ✅ update literals |
| CADW-04 | inspect provenance `measured` + `.env.example` | unit | `go test ./internal/api -run EstimateSource` | ✅ |
| CADW-04 | IN-02 identity, chart grace | unit | `go test ./internal/queue -run Compose ./internal/e2e -run ChartGrace` | ✅ |
| CADW-04 | measured rows, max, 1.5×, cross-check | live script | `76-0X-bounds.txt` SUMMARY ok, no STOP | ❌ new mode |
| CADW-05 | 0→2→0 with timestamps; downscale survival | live script | `keda-cad-loadproof.sh`, `keda-cad-downscale-survival.sh` final PASS line | ❌ |
| CADW-05 | kill mid-render / mid-rsvg PNG → 0 engine processes, terminal < timeout | live | cad-liveproof rows `WORKER_KILL_RENDER`, `WORKER_KILL_RSVG` | ❌ |
| CADW-05 | determinism JSON bytes + PNG tolerance | live + Go CLI | `DETERMINISM` rows; `cad-png-compare` unit tests | ❌ |
| CADW-05 | all classes ALL PASS from final commit | live wrapper | `76-0X-final-gate.txt` | ❌ |
| CADW-06 | CAD.md both ways | unit | `go test ./internal/convert -run CADDoc` | ❌ |
| CADW-06 | openapi render/opts/role/codes | unit | `go test ./internal/api ./internal/errcode -run OpenAPI` | partial |
| CADW-06 | MCP description matches registry; `measurements` role named | unit | `go test ./internal/mcpserver -run Description` | ❌ |
| CADW-06 | frontend table from `operations` | vitest | `cd frontend && npm test` | ❌ |
| CADW-06 | D-07 PDF limit → `cad_too_complex` | unit, fails first | `go test ./internal/worker -run CADFailure ./internal/convert -run PDF` | ❌ |
| PREV-01 | PREVIEW.md fixed values = code; "not fixed" list present | unit | `go test ./internal/convert -run Preview` | ❌ |

### Sampling Rate
- Per task commit: the quick command for the package touched, with `GOTOOLCHAIN=go1.26.4`, plus a recorded `internal/convert` package time (D-19: stop above 540 s at normal load).
- Per plan: `scripts/ci-local.sh helm gate` (race when the plan touches concurrency-relevant code).
- Phase gate: the final wrapper transcript `ALL PASS`, from a fresh clone.

### Wave 0 Gaps
- [ ] generator kinds: exact-byte padding (STEP/IGES/DXF), `stl-stack`
- [ ] harness modes: 3D stage rows on the production engine stage; in-worker E2E/POD/IO rows; `--out` to 76 evidence
- [ ] `scripts/cmd/cad-png-compare` + canonical JSON helper
- [ ] ci-local e2e knobs (port, compose files, stop teardown)
- [ ] liveproof census generalized to all engine names

## Security Domain

`security_enforcement` is absent from config, so it is treated as enabled.

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no change | existing API keys (`clients` table); proofs mint and revoke their own clients |
| V3 Session Management | no | — |
| V4 Access Control | no change | — |
| V5 Input Validation | yes | per-format byte ceilings (413 at create, re-check in worker), closed option vocabularies (`ParseCADOpts` strict object), `ValidateCADApplicability` → 422 |
| V6 Cryptography | no | — |
| V12 Files/Resources | yes | ceilings, stage deadlines, cgroup 4Gi/2 CPU, Landlock per stage, process-group kill |
| V14 Configuration | yes | IN-02 byte-identity test; startup refusal in `ValidateCADTimeouts`; secrets in values-local are dev-only |

### Known Threat Patterns
| Pattern | STRIDE | Mitigation |
|---------|--------|------------|
| Stacked/duplicated geometry render bomb | DoS | render deadline is terminal `cad_too_complex` (no retry burn); measured ceilings; Pitfall 1 decision |
| Expansion bomb (DXF INSERT/MINSERT) | DoS | guard caps 170031 / 850155 / nesting 16 |
| Output blow-up (SVG/PDF) | DoS | `CADMaxDXFSVGBytes`, `CADMaxDXFPDFBytes` (+ D-07 honest code) |
| Orphaned engine processes after worker death | DoS | process-group kill; kill-mid-render census rows |
| Doc drift that misleads clients about limits/codes | Repudiation/Integrity | sync tests both ways |
| Mis-scoped live proof touching another project | Tampering | namespace-scoped uninstall, never prune volumes, never toggle k8s |

## Sources

### Primary (HIGH confidence, read this session)
- `internal/convert/cad.go:1-427`, `cadsandbox.go:144-179`, `cadrender.go:1-140`, `caddxf.go:1-360`, `caddxfpdf.go:1-72,1474-1680`, `cadopts.go:1-200`, `cadpixel.go:1-124`, `cad_test.go:146-172,720-790`, `cad_docs_sync_test.go` (all), `cadsandbox_test.go:402-643`
- `internal/worker/descriptors.go:55-65,690-764`; `internal/api/estimate.go:100-167`, `estimate_test.go:394-700`, `api.go:513-574`; `internal/jobs/repo.go:241-325`, `usage.go:45-70`; `internal/mcpserver/client.go:105-167`, `mcpserver.go:36-77`; `cmd/cad-worker/main.go:48-139`, `helper/cad-render:18-182`
- `deploy/chart/octoconv/values.yaml:220-229,430-528`, `templates/scaledobject-cad.yaml`, `templates/deployment-cad-worker.yaml`, `templates/configmap.yaml:224-233`, `templates/_helpers.tpl:126-173`, `values-loadproof.yaml`
- `docker-compose.yml:214-220,1704-1734`; `.env.example:568-628`; `scripts/ci-local.sh`; `scripts/cad-liveproof.sh`; `scripts/cad-dxf-bounds.sh:1-200`; `scripts/cad-image-size-measure.sh`; `scripts/cad-go-worker-rss.sh`; `scripts/keda-pandoc-loadproof.sh`; `scripts/keda-ocr-downscale-survival.sh` header; `scripts/cad-corpus-gen.py:840-1150,1834-1870,2798-2812`
- Evidence: `75-05-bounds.txt` + `75-05-SUMMARY.md`, `75-10-liveproof.txt:123-124`, `73-01-spikes.txt` DERIVED lines, `74-RESEARCH.md:442-490`, `74-01-wave0.txt` DERIVED lines, `71-06-render-stacking.txt`, `71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt`, `71-04-render.txt` determinism, `71-DECISION.md` §Образ, quick summaries 261012-qds and 261013-0og
- Live environment probes (read-only): `orbctl config get k8s.enable`, `kubectl get ns`, `helm list -A`, `kubectl describe node`, `lsof` on ports, `docker images`, `docker system df`, `df -h /`, `uptime`

### Secondary (MEDIUM)
- Memory notes: orbstack-k8s-live-proof, orbstack-clusterip-unreachable-from-host, run-the-fixed-build, pinned-toolchain-not-local-one, check-the-same-object, local-gates-no-github-ci, gsd-execute-gotchas-octoconv

### Tertiary (LOW)
- [LKML: memory.peak per-fd reset fix](https://lkml.iu.edu/2607.3/13090.html): only to justify "restart the container per row"

## Metadata

**Confidence breakdown:**
- Standard stack / tooling: HIGH. Every script and path was opened.
- Architecture (routes, sums, pinning, provenance, IN-02 chain): HIGH, read from code.
- Pitfalls: HIGH for 3, 4, 5, 6, 9 (code-verified); MEDIUM for 1 (evaluation-renderer evidence plus a read of the production filter chain; magnitude extrapolated); MEDIUM for 7 (first k8s run).

**Research date:** 2026-10-13
**Valid until:** 2026-11-12 for code facts; environment facts (ports, k8s tenants, disk, load) must be re-probed at the start of each live plan.
