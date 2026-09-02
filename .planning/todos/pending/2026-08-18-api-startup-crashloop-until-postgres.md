---
created: 2026-08-18T02:05:00.000Z
title: api container crash-loops at every fresh k8s install until postgres accepts connections
area: api
files:
  - cmd/api/main.go (log.Fatalf on startup dependency failures)
  - deploy/chart/octoconv/templates/deployment-api.yaml (no initContainer / startupProbe gating on postgres)
origin: observed while running the phase 58 INSP-06 live gate, 2026-08-18
resolves_phase: 62
---

## Observed, not inferred

A fresh `helm upgrade --install` of the chart brings the api pod up before
postgres is accepting connections. The api process exits(1), kubelet restarts
it, and the pod reaches `restartCount` 2–3 before it ever becomes Ready:

```
api-788b6f9d8d-7qpht  0/1  CrashLoopBackOff  1 (10s ago)  17s
api-788b6f9d8d-7qpht  1/1  Running           2 (13s ago)  20s
```

The previous container's own log names the cause:

```
postgres: ping postgres: failed to connect to `user=octo database=octo_db`:
  dial tcp 192.168.194.210:5432: connect: connection refused
```

`cmd/api/main.go` uses `log.Fatalf` for unrecoverable init errors (DB connect,
migrate, storage, queue) — a deliberate convention recorded in CLAUDE.md, and
the right call for a process that cannot serve. In Kubernetes the effect is a
crash-loop as the dependency-wait mechanism.

## Why it is worth deciding rather than leaving

- **It is not free.** Each restart pays CrashLoopBackOff's exponential delay,
  so a cold install is slower to converge than it needs to be, and the backoff
  grows if postgres is slow.
- **It burns the signal an operator watches.** `restartCount` is the primary
  ground truth of this project's own live gates (INSP-06's is exactly that
  metric) and of any production dashboard. A metric that is routinely 2–3 for
  benign reasons is a metric nobody will trust at 4.
- **It is invisible in compose**, where dependency ordering is handled by
  healthchecks — so it only shows up in the deployment target that matters.

## Options

1. **initContainer that waits for postgres** (`pg_isready` loop) — the pod stays
   `Init:0/1` instead of crash-looping, `restartCount` stays 0, and the api
   binary keeps its fail-fast convention untouched. Most contained.
2. **Bounded retry around the startup ping** in `cmd/api/main.go` — keeps
   Kubernetes out of it but weakens the "fatal means fatal" convention, and the
   same argument then applies to the worker binaries.
3. **Accept and document** — record the expected non-zero baseline where
   operators read restart counts, and leave the behavior alone.

Not decided here: this was found while proving something else, and changing pod
startup semantics is not a change to make in passing. Note the live gate itself
is unaffected either way — it asserts the delta across a burst, never the
absolute count.
