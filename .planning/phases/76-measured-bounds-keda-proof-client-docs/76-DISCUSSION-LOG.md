# Phase 76: Measured Bounds, KEDA Proof & Client Docs - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-10-13
**Phase:** 76-measured-bounds-keda-proof-client-docs
**Mode:** `--auto`. In every area the orchestrator picked the recommended option, under the user's v1.16 delegation. No AskUserQuestion was shown.
**Areas discussed:** Timeout measurement protocol, Bounds carry-forward (L170031, loaded runs, 1.26%, RSS), Stacked-copies render, DXF PDF backstop, KEDA proof and minReplicaCount, Final-commit live gate, Determinism, Client docs and sync tests, Preview contract, CI budget

[auto] Selected all gray areas: the ten above.

---

## Timeout measurement protocol

| Option | Description | Selected |
|--------|-------------|----------|
| Max over routes of measured stage deadlines plus measured I/O, cross-checked against 1.5 × the end-to-end wall | Keeps the `ValidateCADTimeouts` invariant and makes each stage measured | ✓ |
| `ceil(1.5 × slowest end-to-end wall)` only | Simpler, but can fall below the stage sum the worker enforces at startup | |
| p95 × 2.0 (pandoc style) | Measures the corpus mix, not a cap-bounded engine | |

**Choice:** the first option, with N=5 per cell, the max statistic, a 1.5× margin, a load-controlled host and pre-registered rule lines.
**Notes:** The loaded outliers already on record (helper 123.61 s against 107.35 s; ink raster 96.60 s against the pre-registered 94 s) enter the max. A loaded run raises the bound; it is never argued away.

## Bounds carry-forward

| Option | Description | Selected |
|--------|-------------|----------|
| Derive from admissible maxima only (C, an admitted insert-cap row, a replicate at exactly 123731968 bytes); L170031 becomes a guard-refusal check | "Measured at the ceiling" means the admitted ceiling; closes the 1.26% gap | ✓ |
| Keep L170031 as the derivation row | Acceptable conservatism (75 verifier), but measures a load production refuses | |

**Go RSS gap:** measure the whole-pod peak in the production container and apply the 70% rule to stage + Go. Keep 4Gi if it fits, otherwise Path-B. ✓ (Alternative: raise the pod limit. Rejected: the project's lever is the ceiling.)

## Stacked-copies render (74 T-74-07 GAP)

| Option | Description | Selected |
|--------|-------------|----------|
| Measure at the 63 MiB STEP ceiling; if it breaks 886 s, add an operation-aware render ceiling (Path-B) | A measured bound the project already trusts | ✓ |
| A heuristic stacking guard in the helper | A new parser-prediction surface; memory "don't predict a parser" | |
| Raise the timeout or the reconciler window | Forbidden by 74 R-01 | |

## DXF PDF backstop over `CADMaxDXFPDFBytes`

| Option | Description | Selected |
|--------|-------------|----------|
| Fix: map to `cad_too_complex` | The honest existing code; one mapping and one test; docs stay true | ✓ |
| Document as `cad_empty_result` | Unreachable within bounds, but the docs would state a dishonest mapping | |

## KEDA proof and minReplicaCount

| Option | Description | Selected |
|--------|-------------|----------|
| Clone the pandoc/ocr loadproof + downscale-survival pair; measure the production image size, registry cold pull and preflight time; choose 0 | Class precedent; cold start is small against minute-scale jobs; an idle 4Gi pod is costly | ✓ |
| minReplicaCount 1 (warm pod) | Removes the cold start at the cost of a permanently reserved 4Gi/2 CPU | |

**Notes:** A calibration step runs first (ClusterIP is unreachable from the host). Port 18090. Compose and k8s never run concurrently. The k8s state is restored afterwards.

## Final-commit live gate

| Option | Description | Selected |
|--------|-------------|----------|
| A fresh clone at the final commit: ci-local (helm gate race docker-build e2e) + sandbox-liveproof over all classes + tectonic liveproof + cad-liveproof, serial, one wrapper transcript ending `ALL PASS` | Covers "all classes" and "images from the final commit" literally | ✓ |
| cad-liveproof only | Does not prove the other classes on the final images | |

**Worker kill:** new rows that kill the worker during `cad-render` and during a DXF `rsvg-convert`, with a zero-process census and no full-timeout wait. ✓

## Determinism

| Option | Description | Selected |
|--------|-------------|----------|
| JSON byte-identical after canonical encoding; PNG by a pre-registered tolerance (dims, alpha, J1–J4, ink bbox ±1 px, MAD ≤ 1/255, ≤ 0.1% pixels > 16/255) | Roadmap SC3: tolerance, not hash; tighten only before the run | ✓ |
| PNG by hash | Excluded by the roadmap | |

## Client docs and sync tests

| Option | Description | Selected |
|--------|-------------|----------|
| New docs/CAD.md with a mandatory Known limits section; a two-way pure-Go sync test against registry, options, ceilings, timeouts, codes and limit constants; openapi/MCP/frontend each held by a test | Matches the ERROR-CODES precedent; adds milliseconds to the test budget | ✓ |
| Prose docs without a test | Violates SC4 | |

**Frontend:** the table is built from `/v1/formats` `operations`, the form can send `operation`, and render outputs are listed by role, with a vitest test that nothing is hard-coded. ✓ (Alternative: leave the frontend convert-only. Rejected: the format table would hide every render pair.)

## Preview contract

| Option | Description | Selected |
|--------|-------------|----------|
| `docs/PREVIEW.md`, cross-class by intent, pinned on CAD's fixed values, with an explicit "not fixed" list and a sync test | SEED-007 is cross-class; the contract should not live inside the CAD doc | ✓ |
| A section inside docs/CAD.md | Ties a cross-class contract to one class's document | |

## CI budget

| Option | Description | Selected |
|--------|-------------|----------|
| A constraint: new internal/convert tests are pure Go; stop and report if the gate tier exceeds 540 s at a normal load | Keeps the phase on its goal | ✓ |
| Restructure internal/convert tests now | Scope creep, deferred | |

---

## Claude's Discretion

- Script and helper names, the evidence layout and the wrapper shape.
- docs/CAD.md section order and prose.
- The frontend layout.
- The source of the in-worker attempt wall.

## Deferred Ideas

- The preview contract on the other classes.
- amd64 runtime measurement.
- Speeding up the internal/convert tests.
- A heuristic stacking guard.
- Raising the chart's `MAX_UPLOAD_BYTES`.
- Four todos at score 0.2, which were not folded.
