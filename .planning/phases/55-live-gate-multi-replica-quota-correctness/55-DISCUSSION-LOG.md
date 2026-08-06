# Phase 55: Live Gate — Multi-Replica Quota Correctness - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-08-06
**Phase:** 55-live-gate-multi-replica-quota-correctness
**Areas discussed:** Run environment, Replica attribution, Gate scope, Burst generation, Anti-vacuity assertion, Burst parameters, Todo folding

---

## Pending todos

Three todos matched the phase at score 0.9. All three are Phase 52/53 API code fixes, not
live-proof work — the matcher fired on keyword overlap (quota / usage / api), the same
false-positive shape that appeared in Phase 54.

| Option | Description | Selected |
|--------|-------------|----------|
| Fold nothing | Phase 55 stays a pure operator-gated proof; todos go through `/gsd:quick` | |
| Fold Retry-After only | The one todo living in the quota path the gate exercises | |
| Fold both usage todos | The two `GET /v1/system/usage` items from 53-REVIEW-GAPS.md | |
| Fold all three | Milestone closes with an empty todo queue | ✓ |

**User's choice:** Fold all three.
**Notes:** The first multiSelect returned every option, including the mutually exclusive "fold
nothing" and "fold all three". Re-asked as a single-select to disambiguate rather than guessing —
the answer materially changes whether the phase carries code plans at all. Recorded in CONTEXT.md
as D-07, with the instruction that the todo fixes live in plan(s) separate from the gate script so
the gate's diff stays reviewable on its own.

---

## Run environment

| Option | Description | Selected |
|--------|-------------|----------|
| OrbStack k8s + Helm | Full cycle: sequential `:dev` image builds, `helm upgrade --install` with `api.replicas: 2` | ✓ |
| k8s without rebuild | Reuse already-built images, raise replicas via values or `kubectl scale` | |
| docker-compose with 2 api | Two api containers behind nginx/traefik against shared Postgres | |

**User's choice:** OrbStack k8s + Helm, full cycle.
**Notes:** SC1 literally says "`api.replicas` set to 2 or more", which is a chart/k8s concept; the
compose route would have required arguing equivalence for no real saving. Matches every existing
live gate in the project. STATE.md's OrbStack discipline (sequential pre-builds, non-`latest` tags,
never compose+k8s hot together, four recorded daemon wedges) carries into the gate's caller
contract, copied from `scripts/keda-gate.sh`.

---

## Replica attribution

Scouting finding presented before the question: chi v5.3.0's `middleware.RequestID` honors an
incoming `X-Request-Id` verbatim and otherwise generates `<hostname>/<b64>-<counter>` — and in k8s
`hostname` is the pod name. `middleware.RequestID` is already installed ahead of
`middleware.Logger` in `internal/api/routes.go`. So replica identity is already observable; the
only question was how to harvest it. (Verified against the vendored source, not assumed.)

| Option | Description | Selected |
|--------|-------------|----------|
| Client supplies X-Request-Id | Harness sends `burst-<i>`, greps each pod's log; exact request→pod join, zero prod code | ✓ |
| Response header | Add `X-Octoconv-Instance` from HOSTNAME/POD_NAME; simplest harness, new prod surface | |
| Distribution only | Just count POST /v1/jobs lines per pod; no per-request attribution | |

**User's choice:** Client supplies `X-Request-Id`.
**Notes:** Chosen over the response header specifically because the observability already exists —
adding production surface solely to make a test observable was the weaker trade. "Distribution
only" was called out as too weak for SC2, which names replica identifiers alongside
admitted/rejected counts.

---

## Gate scope

| Option | Description | Selected |
|--------|-------------|----------|
| Concurrency cap K only | Literal reading of SC1 | ✓ |
| K + daily-jobs cap | Second advisory-lock path with its own in-process burst test | |
| K + daily-jobs + daily-bytes | All three limits from Phase 52 D-03 | |

**User's choice:** Concurrency cap only.
**Notes:** Daily-jobs was surfaced as a genuine second class of multi-replica race with no live
proof — deferred rather than dismissed. Daily-bytes was flagged as the weakest candidate because
Phase 52's D-15 already declares that boundary approximate.

---

## Burst generation

| Option | Description | Selected |
|--------|-------------|----------|
| bash + backgrounded curl with FIFO barrier | Style of the twenty existing gate scripts | ✓ |
| Small Go runner | Tighter barrier, precise timestamps, JSON out; new binary in `cmd/` | |
| Go test behind a build tag | Reuses `internal/e2e`; breaks the "gate = operator-run script" precedent | |

**User's choice:** bash + backgrounded curl with a shared FIFO barrier.
**Notes:** The contended window is a Postgres advisory lock, not microseconds, so sub-millisecond
barrier precision buys nothing. Operator readability of the gate script was the deciding factor.

---

## Anti-vacuity assertion

| Option | Description | Selected |
|--------|-------------|----------|
| Admitted and rejected from different pods | Proves the cap decision crossed the replica boundary | ✓ |
| Both pods received traffic | Closes "all N hit one pod" but not "one pod consumed the cap before the other started" | |
| Strict assert + repeats | Same as option 1, bundled with R rounds | |

**User's choice:** Admitted and rejected from different pods.
**Notes:** Directly motivated by Phase 54, where a CI job passed every shape assertion against a
chart that rendered zero objects. Option 3 bundled two independent decisions (assert strength and
round count), so the round count was asked separately below rather than treated as declined.

---

## Burst parameters

| Option | Description | Selected |
|--------|-------------|----------|
| K=2, N=10 | 8 rejections vs 2 admissions; wide contention window | ✓ |
| K=1, N=6 | Sharpest failure signal, but a single admit can fail the cross-pod assert on scheduling luck alone | |
| K=3, N=20 | Best replica spread, at 20 real multipart uploads per round | |

**User's choice:** K=2, N=10.

| Option | Description | Selected |
|--------|-------------|----------|
| 10 rounds, all must pass | Matches keda-gate 21/21, cancel 12/12, SSE 15/15, progress 40/40 | ✓ |
| 5 rounds | Faster slot draining between rounds | |
| 1 round | Fastest; script remains re-runnable by hand | |

**User's choice:** 10 rounds, all must pass.
**Notes:** Raised during the question and carried into CONTEXT.md as a planner obligation: a slot
is held by `queued`+`active` with no release code (Phase 52 D-06/D-08), so each round must
explicitly drain to terminal before the next starts — and the drain itself must be asserted, or
later rounds admit 0 and pass the "never more than K" check vacuously.

---

## Claude's Discretion

- Gate script name and the shape of the committed evidence artifact, provided SC2's fields are
  present (timestamps, replica identifiers, admitted/rejected counts).
- Which engine class the burst targets (`image` is cheapest to drive terminal, which matters across
  ten rounds).
- Test-client provisioning and teardown via `cmd/manage-clients`.
- Whether the inter-round drain cancels jobs or waits for natural completion.
- Where the "quota carries no `replicas: 1` caveat" statement is recorded — a package doc comment
  near `internal/jobs/admission.go`, mirroring `internal/ratelimit/ratelimit.go:10`, is the obvious
  home.

## Deferred Ideas

- Extending the gate to the daily-jobs cap — a real second multi-replica race, no live proof today;
  the harness built here is the natural place to grow it.
- Extending the gate to the daily-bytes cap — weaker, since Phase 52 D-15 already documents that
  boundary as approximate.
- Running this gate in CI — needs a real multi-replica cluster, same reasoning that keeps every
  other live gate operator-run.
- Adding `X-Octoconv-Instance` to API responses — if replica identity ever proves genuinely useful
  for production debugging, it should be argued on those merits rather than introduced as test
  scaffolding.
