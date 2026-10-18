---
created: 2026-10-18
area: deploy
severity: medium
origin: v1.16 milestone audit, TD-1
audit_acknowledged:
  milestone: v1.16
  at: 2026-10-18
---

# The Helm chart caps uploads below the measured CAD ceilings

## What

`deploy/chart/octoconv/templates/configmap.yaml:30,41` sets `MAX_UPLOAD_BYTES` and
`INSPECT_MAX_UPLOAD_BYTES` to 100 MiB. The measured ceilings in `internal/convert/cad.go:25,29,36`
are IGES 120 MiB, STL 182 MiB and DXF 118 MiB. In k8s an IGES, STL or DXF input between
100 MiB and its ceiling is refused with the generic transport 413 instead of being accepted,
while compose (2 GiB) accepts it. Inspect is capped at 100 MiB in compose too, so inspecting a
101–182 MiB STL fails in both deployments.

The difference is documented in `.env.example` and `docs/CAD.md`, but the chart does not carry
the measured constants and no test holds the relation.

## Idea

- Raise the chart's API upload cap to at least the largest CAD ceiling (it is a process-wide
  cap; the per-format ceilings in code still apply), or make it a value with a documented
  per-class note.
- Add a test that the chart's (and compose's inspect) cap is at least every per-format ceiling
  the registry advertises, or that the docs state each exception.

## Constraints

- A larger process-wide cap changes the memory/disk exposure of the API pod for every class;
  check the API's spill and request-body handling before raising it.
