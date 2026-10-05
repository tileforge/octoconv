# Phase 73: CAD Class & Convert Slice, Confined - Discussion Log

> **Audit trail only.** Decisions live in CONTEXT.md.

**Date:** 2026-10-05
**Mode:** auto, under the user's 2026-10-04 delegation. Each line records the recommended option, which was auto-selected.

## Worker shape
[auto] Q: Should the worker follow the existing worker skeleton or a new design? → Selected: mirror tectonic-worker/document-worker exactly (recommended).
[auto] Q: What concurrency per pod? → Selected: 1, because the 4Gi limit was derived for one job.

## Engine and image
[auto] Q: Should the production helper share the 71 measurement helper or be a separate copy? → Selected: a separate hardened copy, so the 71 image tag stays stable.
[auto] Q: Should VTK be in the image now? → Selected: no. It arrives with render in phase 74.
[auto] Q: What packages may the image use? → Selected: only the approved 71 lock files, 7.9.3.1.1 pin; no new packages.

## Units
[auto] Q: How are inch inputs handled? → Selected: read the declared unit from the file text, and scale explicitly when OCP did not. Both files must be tested.

## Limits
[auto] Q: Where are the byte ceilings enforced? → Selected: at create, with 413 payload_too_large, plus a worker re-check.
[auto] Q: How are the caps enforced? → Selected: a guard pre-pass in the worker under the sandbox; exceeding a cap is a terminal failure.
[auto] Q: How is the timeout set? → Selected: derived from the 71 evidence with a visible margin.

## Errors
[auto] Q: New codes or reuse? → Selected: reuse where the client action matches; add a new cad_* code only where it differs.

## Confinement
[auto] Q: Which Landlock set? → Selected: the 71 candidate parametrised by arch, re-measured on the production image, with a SANDBOX-PATHS section, a test and the crosscheck.

## Exposure
[auto] Q: How is the class exposed? → Selected: one atomic change across every registry and the deploy files; the phase 72 no-CAD guards are inverted in the same change.
[auto] Q: What minReplicaCount? → Selected: 0; phase 76 tunes it.

## Deferred
Render, measurements output, DXF, KEDA tuning, the render time guard, amd64 runtime measurement, the legal OK.
