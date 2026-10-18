# Phase 74: 3D Render & Measurements - Discussion Log

> Audit trail only. Decisions live in CONTEXT.md.

**Date:** 2026-10-05
**Mode:** auto, under the user's 2026-10-04 delegation. Every option below is the recommended one, auto-selected.

- **Pipeline:** three confined stages, guard → mesh → render. The render stage has no OCP. Alternative considered: a single render stage that imports OCP.
- **Failures:** a render-stage failure is retryable; a mesh-stage failure is terminal; the triangle cap is checked before render.
- **Options:** a closed set, validated with 422 at create time.
  - view: 7 values, default iso.
  - size: 256..2048, default 1280.
  - quality: draft/standard/fine, applied to convert as well as render.
  - background: opaque/transparent.
- **Pixel judge:** aspect-aware, so it does not repeat the frac-only false rejection from 71 W15.
- **Two outputs:** atomic, proven by a repository-level inversion test.
- **STL area:** computed from triangles for watertight meshes; otherwise volume and area are both null with stl_not_watertight (follows the 72 pairing rule).
- **Units:** the declared unit is honoured; assume_unit only fills a missing unit; IGES inch scaling is recorded as a gap.
- **Render ruleset:** measured on the production image; the mesh set is re-checked.
