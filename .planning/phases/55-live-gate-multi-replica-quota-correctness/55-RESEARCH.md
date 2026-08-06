# Phase 55: Live Gate — Multi-Replica Quota Correctness - Research

**Researched:** 2026-08-06
**Domain:** Kubernetes Service networking on OrbStack (host↔cluster reachability with real
multi-pod distribution), chi/asynq/Postgres observability plumbing, bash live-gate harness design
**Confidence:** MEDIUM — the load-bearing networking claim (P0) is CITED from official OrbStack
docs but could not be exercised live this session (see `## Environment Availability`); everything
about the existing codebase (admission mechanism, chi middleware, `manage-clients`, chart) is
VERIFIED by direct source read.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

## Implementation Decisions

### Run environment

- **D-01:** The live run is **OrbStack Kubernetes + Helm, full cycle** — build the `:dev`-tagged
  images sequentially, `helm upgrade --install` with `api.replicas: 2`, run the gate, tear down.
  Not docker-compose.

  **Why:** SC1 is written as "`api.replicas` set to 2 or more" — that is literally a chart/k8s
  concept, and every other live gate in this project (keda-gate, the four loadproofs, the three
  downscale-survival gates) runs the same way. Proving the claim on a compose stack would require
  arguing that two compose containers behind a proxy are equivalent to what SC1 names, which is
  weaker evidence for no real saving.

  **OrbStack discipline carried forward from STATE.md (non-negotiable, four recorded daemon
  wedges):** the compose stack must be DOWN before the k8s stack comes up; images are pre-built
  **sequentially** with a non-`latest` pinned tag; the gate script itself never runs
  `docker build` / `docker compose up`; it tears the k8s stack down unconditionally on exit via a
  `trap`, success or failure. This is exactly `scripts/keda-gate.sh`'s contract — copy it.

### Replica attribution

- **D-02:** The gate attributes each request to a replica by having the **client supply its own
  `X-Request-Id`** (`burst-<round>-<index>`) and then grepping each pod's own log
  (`kubectl logs <pod>`) for those IDs. **Zero production code changes.**

  **Why this works without touching the API** (verified against the vendored chi v5.3.0 source
  during discussion, `middleware/request_id.go`):
  - `RequestIDHeader = "X-Request-Id"`, and the middleware uses an **incoming** value verbatim if
    present (`requestID := r.Header.Get(RequestIDHeader)`; it only generates one when that is
    empty).
  - `middleware.RequestID` is already installed on the router (`internal/api/routes.go`), ahead of
    `middleware.Logger`, which prints the request ID on every access-log line.
  - Therefore a client-chosen ID appears verbatim in exactly one pod's log — the pod that served
    it. The client separately knows that request's HTTP status. Joining the two gives
    `(request index → replica, status)` with nothing added to the product.

  **Why not a response header:** adding `X-Octoconv-Instance` would make the harness simpler, but
  it is new production surface introduced solely to make a test observable, and the observability
  already exists. Rejected.

  **Why not "both pods got traffic" counting alone:** see D-05 — it is too weak for SC2.

  Note for the planner: chi generates `<hostname>/<b64>-<counter>` when the client sends no
  `X-Request-Id`, and in k8s `hostname` is the pod name. That is a useful cross-check but is NOT
  the mechanism — the client-supplied ID is, because only it lets the harness join a specific
  HTTP status to a specific pod.

### Gate scope

- **D-03:** The gate exercises the **concurrency cap only**. Not the daily-jobs cap, not the
  daily-bytes cap.

  **Why:** SC1 names concurrency specifically. The daily-jobs cap does have its own advisory-lock
  path (and its own in-process burst test, `TestConcurrentBurstDailyJobsCapAdmitsExactlyOne`), so
  extending the gate to it was considered on its merits and deliberately deferred — see
  `<deferred>`. The daily-bytes cap additionally carries Phase 52's **documented** imprecision
  (D-15: it reads `client_usage_daily`, which only updates on a terminal transition), so a live
  gate there would be asserting a boundary the project already declares approximate.

### Burst generation and assertions

- **D-04:** The burst generator is **bash with backgrounded `curl`s released by a shared FIFO
  barrier** — every request blocks reading one named pipe, then a single write releases all of
  them at once. Placed in `scripts/`, matching the twenty existing live-gate scripts.

  **Why not a Go runner or a tagged Go test:** both would give tighter barriers and cleaner JSON,
  but this project's live-gate precedent is unambiguously "an operator-runnable script in
  `scripts/` with a long header explaining why `go test` cannot prove this". An operator has to be
  able to read the gate before trusting it. Precision beyond "N requests genuinely in flight
  together" buys nothing here — the contended window is a Postgres advisory lock, not microseconds.

- **D-05:** The load-bearing anti-vacuity assertion is: **at least one admitted (`201`) and at
  least one rejected (`429 quota_exceeded`) request must have been served by DIFFERENT pods.**

  **Why this specific assertion and not a weaker one:** "both pods received some traffic" would
  still pass if one pod consumed the entire cap before the other pod's first request arrived — no
  cross-replica decision would have occurred, and the gate would prove nothing about QUOT-05 while
  reporting green. Requiring an admit and a reject on different pods is exactly the claim: the cap
  decision crossed the replica boundary. This is the direct lesson from Phase 54, where a CI job
  passed every shape assertion against a chart that rendered zero objects.

  A second, independent assertion still applies per SC1: **admitted count == K exactly**, never
  more, and `N - K` rejections.

- **D-06:** Parameters: **`K = 2`, `N = 10` concurrent requests, `R = 10` rounds — every round
  must pass.**

  **Why K=2/N=10:** 8 rejections against 2 admissions gives a wide contention window and makes
  D-05's cross-pod admit/reject split overwhelmingly likely on a 2-replica Service. `K=1` was
  considered (sharpest failure signal — any atomicity break shows as 2+ admits) but rejected
  because with a single admitted request the D-05 assertion can fail through pure scheduling luck,
  failing the gate with no bug present.

  **Why 10 rounds:** races are nondeterministic; a single green run is weak evidence. This matches
  the project's own live-gate discipline (keda-gate 21/21, cancel 12/12, SSE 15/15, progress
  40/40).

  **Planner must handle — slot drain between rounds:** a concurrency slot is occupied by
  `queued` + `active` and there is **no release code** (Phase 52 D-06/D-08); a slot frees only when
  the job reaches a terminal status. So round R+1 cannot start until round R's admitted jobs are
  terminal. The harness must explicitly drain — either wait for terminal status or cancel via
  `POST /v1/jobs/{id}/cancel` (`canceled` is terminal) — and must **assert** the drain completed
  before the next round, otherwise later rounds silently start with the cap already consumed and
  admit 0, which would pass the "never more than K" check vacuously.

### Folded todos

- **D-07:** All three open v1.12 todos are **folded into this phase**, so the milestone closes with
  an empty todo queue. They are code fixes and are independent of the live gate — the planner
  should keep them in their own plan(s), separate from the gate plan, so the gate's diff stays
  reviewable on its own.

### Claude's Discretion

### Claude's Discretion

- Exact script name (`scripts/quota-multireplica-liveproof.sh` or similar) and the shape of its
  committed evidence artifact (transcript file vs. embedded in SUMMARY.md) — as long as SC2's
  required fields are present: timestamps, replica identifiers, admitted/rejected counts.
- Which engine class the burst targets. `image` is the cheapest and fastest to reach a terminal
  state, which matters for the ten-round slot drain.
- How the test client and its `client_limits` row are provisioned (`cmd/manage-clients set-limit`
  + `set-mode enforce`) and torn down.
- Whether the drain between rounds uses cancel or waits for natural completion.
- Where the "no `replicas: 1` caveat" statement is recorded (package doc comment near
  `internal/jobs/admission.go`, mirroring `internal/ratelimit/ratelimit.go:10`'s caveat, is the
  obvious home).

### Folded Todos

1. **Fix Retry-After midnight race in daily quota rejection**
   (`.planning/todos/pending/2026-08-04-fix-retry-after-midnight-race.md`) —
   `quotaRejection` computes `secondsUntilUTCMidnight(time.Now())` on the API's own wall clock,
   independent of when the admission transaction actually evaluated the daily window, so it can
   advertise ~86400s when the daily quota has already reset. Needs a sentinel-error contract
   change; deliberately deferred out of PR #5. Touches `internal/api/handlers.go:1017-1026`,
   `:986`, and the `jobs.ErrDailyBytesLimit`/`ErrDailyJobsLimit` sentinels.
2. **Add per-row `enforced` flag to `GET /v1/system/usage`**
   (`.planning/todos/pending/2026-08-05-per-row-enforced-flag.md`) — from
   `53-REVIEW-GAPS.md` WR-01. Touches `internal/api/system_usage_handlers.go:353,363-365` and
   `docs/openapi.yaml`.
3. **`GET /v1/system/usage` can emit `enforcement_mode:""`, violating its own enum**
   (`.planning/todos/pending/2026-08-05-usage-row-empty-enforcement-mode.md`) — from
   `53-REVIEW-GAPS.md` WR-02. Two unsynchronized reads
   (`internal/api/system_usage_handlers.go:202`, `:208`) can yield a row whose
   `enforcement_mode` is absent from the `[observe, enforce]` enum the OpenAPI spec declares
   required. `internal/jobs/usage_query.go:106,126` already carries the analogous guards for the
   same output shape — reuse that pattern rather than inventing a new one.

### Deferred Ideas (OUT OF SCOPE)

`. The daily-bytes cap additionally carries Phase 52's **documented** imprecision
  (D-15: it reads `client_usage_daily`, which only updates on a terminal transition), so a live
  gate there would be asserting a boundary the project already declares approximate.

### Burst generation and assertions

- **D-04:** The burst generator is **bash with backgrounded `curl`s released by a shared FIFO
  barrier** — every request blocks reading one named pipe, then a single write releases all of
  them at once. Placed in `scripts/`, matching the twenty existing live-gate scripts.

  **Why not a Go runner or a tagged Go test:** both would give tighter barriers and cleaner JSON,
  but this project's live-gate precedent is unambiguously "an operator-runnable script in
  `scripts/` with a long header explaining why `go test` cannot prove this". An operator has to be
  able to read the gate before trusting it. Precision beyond "N requests genuinely in flight
  together" buys nothing here — the contended window is a Postgres advisory lock, not microseconds.

- **D-05:** The load-bearing anti-vacuity assertion is: **at least one admitted (`201`) and at
  least one rejected (`429 quota_exceeded`) request must have been served by DIFFERENT pods.**

  **Why this specific assertion and not a weaker one:** "both pods received some traffic" would
  still pass if one pod consumed the entire cap before the other pod's first request arrived — no
  cross-replica decision would have occurred, and the gate would prove nothing about QUOT-05 while
  reporting green. Requiring an admit and a reject on different pods is exactly the claim: the cap
  decision crossed the replica boundary. This is the direct lesson from Phase 54, where a CI job
  passed every shape assertion against a chart that rendered zero objects.

  A second, independent assertion still applies per SC1: **admitted count == K exactly**, never
  more, and `N - K` rejections.

- **D-06:** Parameters: **`K = 2`, `N = 10` concurrent requests, `R = 10` rounds — every round
  must pass.**

  **Why K=2/N=10:** 8 rejections against 2 admissions gives a wide contention window and makes
  D-05's cross-pod admit/reject split overwhelmingly likely on a 2-replica Service. `K=1` was
  considered (sharpest failure signal — any atomicity break shows as 2+ admits) but rejected
  because with a single admitted request the D-05 assertion can fail through pure scheduling luck,
  failing the gate with no bug present.

  **Why 10 rounds:** races are nondeterministic; a single green run is weak evidence. This matches
  the project's own live-gate discipline (keda-gate 21/21, cancel 12/12, SSE 15/15, progress
  40/40).

  **Planner must handle — slot drain between rounds:** a concurrency slot is occupied by
  `queued` + `active` and there is **no release code** (Phase 52 D-06/D-08); a slot frees only when
  the job reaches a terminal status. So round R+1 cannot start until round R's admitted jobs are
  terminal. The harness must explicitly drain — either wait for terminal status or cancel via
  `POST /v1/jobs/{id}/cancel` (`canceled` is terminal) — and must **assert** the drain completed
  before the next round, otherwise later rounds silently start with the cap already consumed and
  admit 0, which would pass the "never more than K" check vacuously.

### Folded todos

- **D-07:** All three open v1.12 todos are **folded into this phase**, so the milestone closes with
  an empty todo queue. They are code fixes and are independent of the live gate — the planner
  should keep them in their own plan(s), separate from the gate plan, so the gate's diff stays
  reviewable on its own.

### Claude's Discretion

- Exact script name (`scripts/quota-multireplica-liveproof.sh` or similar) and the shape of its
  committed evidence artifact (transcript file vs. embedded in SUMMARY.md) — as long as SC2's
  required fields are present: timestamps, replica identifiers, admitted/rejected counts.
- Which engine class the burst targets. `image` is the cheapest and fastest to reach a terminal
  state, which matters for the ten-round slot drain.
- How the test client and its `client_limits` row are provisioned (`cmd/manage-clients set-limit`
  + `set-mode enforce`) and torn down.
- Whether the drain between rounds uses cancel or waits for natural completion.
- Where the "no `replicas: 1` caveat" statement is recorded (package doc comment near
  `internal/jobs/admission.go`, mirroring `internal/ratelimit/ratelimit.go:10`'s caveat, is the
  obvious home).

### Folded Todos

1. **Fix Retry-After midnight race in daily quota rejection**
   (`.planning/todos/pending/2026-08-04-fix-retry-after-midnight-race.md`) —
   `quotaRejection` computes `secondsUntilUTCMidnight(time.Now())` on the API's own wall clock,
   independent of when the admission transaction actually evaluated the daily window, so it can
   advertise ~86400s when the daily quota has already reset. Needs a sentinel-error contract
   change; deliberately deferred out of PR #5. Touches `internal/api/handlers.go:1017-1026`,
   `:986`, and the `jobs.ErrDailyBytesLimit`/`ErrDailyJobsLimit` sentinels.
2. **Add per-row `enforced` flag to `GET /v1/system/usage`**
   (`.planning/todos/pending/2026-08-05-per-row-enforced-flag.md`) — from
   `53-REVIEW-GAPS.md` WR-01. Touches `internal/api/system_usage_handlers.go:353,363-365` and
   `docs/openapi.yaml`.
3. **`GET /v1/system/usage` can emit `enforcement_mode:""`, violating its own enum**
   (`.planning/todos/pending/2026-08-05-usage-row-empty-enforcement-mode.md`) — from
   `53-REVIEW-GAPS.md` WR-02. Two unsynchronized reads
   (`internal/api/system_usage_handlers.go:202`, `:208`) can yield a row whose
   `enforcement_mode` is absent from the `[observe, enforce]` enum the OpenAPI spec declares
   required. `internal/jobs/usage_query.go:106,126` already carries the analogous guards for the
   same output shape — reuse that pattern rather than inventing a new one.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### The mechanism being proven (do not modify)
- `internal/jobs/admission.go` — `admitLocked` (`:388`), `quotaLockKeys` (`:172`), `evaluate`
  (`:236`), `PreAdmit` (`:496`). The advisory-lock + same-transaction `COUNT` design is what makes
  the cap replica-safe by construction; this phase proves it, never edits it.
- `internal/jobs/admission_test.go` — `TestConcurrentBurstAdmitsExactlyK` (`:198`) is the
  in-process analogue of this gate. Read it to see what is already covered and therefore what the
  live gate must add (cross-process, cross-replica), not duplicate.
- `.planning/phases/52-quota-enforcement-intra-class-fairness/52-CONTEXT.md` — D-06/D-07/D-08 (slot
  = `queued`+`active`, `COUNT` in the same transaction, **no release code**), D-09/D-10/D-11
  (single enforcement point, `code` field, `Retry-After` derivation), D-15 (daily-window
  imprecision).
- `.planning/phases/52-quota-enforcement-intra-class-fairness/52-VERIFICATION.md` — what Phase 52
  already proved statically, so the gate does not re-assert it.

### The precedent this gate must match
- `scripts/keda-gate.sh` §1-30 — the canonical live-gate header: what is proven, what is
  explicitly out of scope, the OrbStack caller contract, the unconditional teardown `trap`, and
  "this script's exit code IS the gate".
- `scripts/cancel-liveproof.sh` §1-40 — the canonical "why `go test -race` is NOT sufficient proof"
  argument, and the discipline of asserting on the real observable rather than on the DB row.
- `scripts/keda-av-downscale-survival.sh`, `scripts/keda-ocr-downscale-survival.sh` — repeated-round
  gates (the R-rounds-all-must-pass shape D-06 needs).

### Replica attribution mechanics
- `internal/api/routes.go:28-31` — `middleware.RequestID` is installed ahead of
  `middleware.Logger`; this is the entire basis of D-02.
- `$(go env GOMODCACHE)/github.com/go-chi/chi/v5@v5.3.0/middleware/request_id.go:25,47,59,70-73` —
  `RequestIDHeader = "X-Request-Id"`, incoming value honored verbatim, otherwise
  `<hostname>/<b64>-<counter>`. Verified during discussion; re-verify before relying on it.
- `internal/ratelimit/ratelimit.go:10` — the documented `replicas: 1`-only caveat that SC2 requires
  this phase to explicitly NOT repeat for quotas. The wording here is the model for where and how
  the quota counter-statement gets recorded.

### Chart / deployment
- `deploy/chart/octoconv/values.yaml` — `api.replicas` (currently `1`); `webhook-worker` at
  `replicas: 2` is the existing precedent that the chart already runs a multi-replica Deployment.
- `deploy/chart/octoconv/values-local.yaml` — the overlay the live stack is installed with; also
  supplies the `secrets:` block that base `values.yaml` deliberately omits.

### Requirements and roadmap
- `.planning/ROADMAP.md` §"Phase 55: Live Gate — Multi-Replica Quota Correctness" — goal, SC1, SC2,
  and the `Autonomous: false` marker.
- `.planning/REQUIREMENTS.md` — QUOT-05.
- `.planning/STATE.md` §Blockers/Concerns — OrbStack operational discipline (sequential
  pre-builds, non-`latest` tags, never compose+k8s hot simultaneously).

### Folded-todo targets
- `.planning/todos/pending/2026-08-04-fix-retry-after-midnight-race.md`
- `.planning/todos/pending/2026-08-05-per-row-enforced-flag.md`
- `.planning/todos/pending/2026-08-05-usage-row-empty-enforcement-mode.md`
- `.planning/phases/53-operator-usage-visibility/53-REVIEW-GAPS.md` — WR-01/WR-02, the source of
  todos 2 and 3.
- `internal/jobs/usage_query.go:106,126` — the existing guards to mirror for todo 3.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/jobs/admission.go` is complete and enforcing — this phase adds no admission code.
- `middleware.RequestID` + `middleware.Logger` are already wired, giving per-request,
  per-pod observability for free (D-02).
- `cmd/manage-clients` already has `set-limit` / `clear-limit` / `show-limits` / `set-mode` — the
  harness provisions and tears down the test client's limits through it, no new tooling.
- Twenty existing live-gate scripts in `scripts/` supply the header register, the `set -euo
  pipefail` + `trap` teardown shape, and the "print every observed value so the transcript is
  self-documenting evidence" convention.
- `POST /v1/jobs/{id}/cancel` (Phase 39) reaches a terminal `canceled` status, which is the fastest
  way to free a concurrency slot between rounds.

### Established Patterns
- **A live gate's exit code IS the gate.** Every assertion aborts non-zero with a loud FAIL and
  prints what it observed.
- **Anti-vacuity assertions come before any conclusion is drawn.** Phase 54's CI job learned this
  the hard way; D-05 is that lesson applied here.
- **Slot semantics have no release path.** `queued` + `active` occupy a slot and only a terminal
  transition frees it — this shapes the whole multi-round design.
- **Rejections are `429` with `code: "quota_exceeded"`** (`internal/api/handlers.go:1009`), not
  `403` — the harness must classify on the code, not just the status.
- **`internal/ratelimit` documents its own replicas caveat in a package comment.** The quota
  counter-statement belongs in the same register.

### Integration Points
- `deploy/chart/octoconv/values.yaml` `api.replicas: 1` → `2` for the gate run (via `--set` or a
  gate-specific overlay; D-04's script decides which).
- `kubectl logs <api-pod>` → the harness's replica-attribution join.
- `cmd/manage-clients` → the test client's `client_limits` row and `enforce` mode.
- The three folded todos touch `internal/api/handlers.go`,
  `internal/api/system_usage_handlers.go`, `docs/openapi.yaml`, and read
  `internal/jobs/usage_query.go` — entirely disjoint from the gate script.

</code_context>

<specifics>
## Specific Ideas

- The gate must classify a rejection by `code: "quota_exceeded"` in the JSON body, not by the
  `429` status alone — `ratelimit.ByIP` and `ratelimit.PerClient` also return `429`, and a burst of
  10 simultaneous requests from one client is exactly the shape that could trip the per-client RPM
  limiter instead of the quota gate. **If the harness cannot distinguish them, the gate proves the
  wrong thing.** The planner should either raise the client's RPM limit for the run or assert that
  zero rate-limit rejections occurred.
- Evidence must include per-request send/receive timestamps, so a reader can confirm the ten
  requests were genuinely in flight together rather than serialized.
- The `X-Request-Id` values should be unique per round (`burst-<round>-<index>`), so log grepping
  cannot accidentally match a previous round's line.
- Record the two API pod names once at the start of the run; if the Deployment reschedules a pod
  mid-run, the attribution join breaks silently — assert the pod set is unchanged at the end.

</specifics>

<deferred>
## Deferred Ideas

- **Extending the gate to the daily-jobs cap.** It is a genuinely separate multi-replica race with
  its own advisory-lock path, and it has an in-process burst test but no live proof. Deliberately
  out of scope here (D-03) because SC1 names concurrency; worth revisiting if the daily path ever
  changes shape. The harness D-04 builds is the natural place to grow it.
- **Extending the gate to the daily-bytes cap.** Weaker candidate than daily-jobs: Phase 52 D-15
  already declares this boundary approximate (it reads `client_usage_daily`, which only updates on
  a terminal transition), so a live gate would be asserting a limit the project has already
  documented as imprecise.
- **Running this gate in CI.** It needs a real multi-replica cluster; the same reasoning that keeps
  every other live gate operator-run applies. (Phase 54's `helm` CI job is the counter-example that
  works precisely because it needs no cluster.)
- **Adding `X-Octoconv-Instance` to API responses.** Rejected for this phase (D-02) because the
  observability already exists via request IDs and pod logs. If replica identity ever becomes
  genuinely useful for production debugging on its own merits, it should be argued on those merits,
  not smuggled in as test scaffolding.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| QUOT-05 | Счётчик квоты корректен при нескольких репликах API — атомарно в Postgres, а не in-process. Existing `internal/ratelimit` documents it is correct only at `api.replicas: 1`; quotas must not repeat this caveat. | Pattern 1 (ClusterIP-direct reachability, the mechanism that makes a genuine multi-pod burst possible), Pattern 2 (D-02 replica attribution via `X-Request-Id` + `kubectl logs`, verified against chi source), Pattern 3 (quota-vs-ratelimit 429 discriminator so the proof exercises the right rejection path), Pitfall 4 (slot-drain-between-rounds so 10 rounds are each a genuine independent trial), and the SC2 counter-statement code example (where to record the "no replicas:1 caveat" in `internal/jobs/admission.go`, mirroring `internal/ratelimit/ratelimit.go:10`). |
</phase_requirements>

## Summary

This phase's only genuinely novel risk is **not** the quota mechanism (Phase 52 already built and
proved it statically) — it is **getting a 10-request burst to actually land on two different API
pods from the macOS host**, because every existing live gate in this repo (`keda-gate.sh` and all
seven `keda-*-loadproof.sh`/`keda-*-downscale-survival.sh` siblings) reaches the API via
`kubectl port-forward -n octoconv svc/api`, and **`kubectl port-forward` to a Service pins to a
single backend pod for the life of the forwarded connection — it does not load-balance.** Every
prior gate got away with this because `api.replicas` was always `1`. Phase 55 is the first gate
where this matters, and copying the precedent verbatim (D-01 says to copy `keda-gate.sh`'s
teardown/OrbStack-discipline shape, not its reachability mechanism) would silently make D-05
unwinnable — the gate would report FAIL forever with a bug that isn't there, or worse, a weaker
harness might not even notice and just always exercise one pod.

The fix is to reach the API by its **ClusterIP directly** (or the `cluster.local` Service DNS
name), not through `kubectl port-forward`. OrbStack's documented behavior is that Kubernetes
Service IPs and `*.svc.cluster.local` DNS names are **directly routable from the macOS host**
without port-forwarding — this is a documented OrbStack-specific feature, not standard behavior
for Docker Desktop, minikube, or kind. Traffic that reaches a ClusterIP transits the real
Service/kube-proxy DNAT path (the same one in-cluster clients use), which genuinely distributes
new TCP connections across all ready endpoint pods. This is CITED from OrbStack's own docs, but
the docs do not explicitly say "and this load-balances across replicas, unlike port-forward" — that
inference is architecturally sound (ClusterIP routing = real Service routing = real kube-proxy) but
was **not exercised live this session** because bringing up the k8s cluster while the compose stack
is running would violate this project's own OrbStack discipline (STATE.md: never run compose and
k8s hot simultaneously). This is the single highest-priority MUST-VERIFY-AT-EXECUTION item in this
document, and the plan should budget an early, cheap calibration step to prove it before spending
the full 10-round budget on it.

Every other piece of this phase — replica attribution via `X-Request-Id` + `kubectl logs`
(D-02, already verified against the vendored chi source), the quota-vs-ratelimit 429 discriminator,
slot-drain-between-rounds, and `manage-clients` provisioning — is well-understood from direct
source reads and carries HIGH confidence.

**Primary recommendation:** Write the burst harness to curl the API's **ClusterIP** (discovered
live via `kubectl get svc api -n octoconv -o jsonpath='{.spec.clusterIP}'`) directly from the host,
never through `kubectl port-forward svc/api`. Open the gate script with a cheap, fast **calibration
step** (a handful of unauthenticated `/healthz` requests tagged with unique `X-Request-Id`s, split
across both pods' logs) that fails loud and early if host→ClusterIP routing does not distribute —
so a routing-layer surprise is caught in seconds, not after a 10-round run that would otherwise fail
in a way indistinguishable from a real quota bug.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Burst request generation + FIFO barrier | Host / Client (bash script in `scripts/`) | — | D-04: operator-run script, not in-cluster, not a Go test |
| Host→cluster reachability with real distribution | Kubernetes Service (ClusterIP + kube-proxy) | — | The thing being exploited: OrbStack routes host traffic through the real Service DNAT path, not an app-layer proxy |
| Admission decision (concurrency cap) | API / Backend (`internal/jobs/admission.go`) | Database (Postgres advisory lock + `COUNT`) | Already built (Phase 52); this phase proves it, never edits it |
| Replica attribution | API / Backend (chi `middleware.RequestID` + `middleware.Logger`) | Ops tooling (`kubectl logs`) | D-02: client-supplied ID + per-pod log grep, zero prod code change |
| Slot drain between rounds | API / Backend (job state machine) | Database (`jobs.status`) | No release code exists (Phase 52 D-06/D-08) — a slot frees only via a terminal transition |
| Test client + limits provisioning | Ops tooling (`cmd/manage-clients` CLI) | Database (`client_limits`, `clients.enforcement_mode`) | Existing CLI, no new tooling needed |
| Chart `api.replicas: 2` | Deployment / Infra (Helm values) | — | `--set api.replicas=2` or a small gate-specific overlay |
| Evidence capture/commit | Host / Client (script output → committed artifact) | — | Mirrors every existing live-gate transcript convention |

## Standard Stack

No new libraries or packages are introduced by this phase — it is a bash harness plus (for the
three folded todos) small Go edits to already-imported packages. **Package Legitimacy Audit is not
applicable**: nothing new is installed. `slopcheck`/registry verification steps were skipped for
this reason.

### Tools relied on (already present, verified)
| Tool | Version/Source | Purpose | Confidence |
|------|-----------------|---------|------------|
| `kubectl` | local install, `/opt/homebrew/bin/kubectl` (confirmed present this session) | Pod discovery, log retrieval, Service ClusterIP lookup | VERIFIED: local shell |
| `helm` | local install, `/opt/homebrew/bin/helm` (confirmed present this session) | `helm upgrade --install ... --set api.replicas=2` | VERIFIED: local shell |
| `curl` | macOS system curl | Burst request generation (one process per request — see Pitfall 2) | VERIFIED: general tool behavior |
| `mkfifo`/bash `read` barrier | bash builtin | D-04's release barrier | ASSUMED-NEW: no existing script in this repo uses `mkfifo` (grep confirmed zero hits in `scripts/`) — this is a genuinely new pattern for this codebase, not a copy of an existing one |

## Package Legitimacy Audit

N/A — this phase installs no external packages (bash + existing Go stdlib/already-vendored
dependencies only). Folded todos 2/3 touch only already-imported `internal/jobs` code.

## Architecture Patterns

### System Architecture Diagram

```text
 macOS host (operator)
   │
   │  scripts/quota-multireplica-liveproof.sh
   │  ┌─────────────────────────────────────────────────────────┐
   │  │ 1. helm upgrade --install ... --set api.replicas=2       │
   │  │ 2. kubectl get pods -l component=api  -> record 2 names  │
   │  │ 3. kubectl get svc api -> CLUSTER_IP (NOT port-forward)  │
   │  │ 4. calibration: N tagged /healthz probes -> both pods?   │
   │  │ 5. manage-clients create + set-limit image 2 - - + enforce│
   │  │ 6. for round in 1..10:                                    │
   │  │      mkfifo barrier; 10x backgrounded curl -H             │
   │  │      "X-Request-Id: burst-<r>-<i>" --> CLUSTER_IP:8090   │
   │  │      release barrier; collect status+body per request    │
   │  │      drain: wait/cancel until all admitted jobs terminal  │
   │  │    done
   │  └─────────────────────────────────────────────────────────┘
   │           │ curl (real TCP connections)                 │ kubectl logs <pod>
   ▼           ▼                                              ▼
 ┌─────────────────────────────┐                    ┌────────────────────┐
 │ Service "api" (ClusterIP)   │  kube-proxy DNAT    │ Pod api-xxxx (1)   │
 │ selector: component=api      │ ───────────────────▶│ chi RequestID+     │
 │ 2 ready endpoints             │  per-NEW-connection │ Logger -> stdout   │
 └─────────────────────────────┘  random/RR choice    │ admission gate     │
                                    ───────────────────▶│ Pod api-yyyy (2)   │
                                                        └────────┬───────────┘
                                                                  │ pg_advisory_xact_lock
                                                                  ▼
                                                        ┌────────────────────┐
                                                        │ Postgres            │
                                                        │ quotaLockKeys(...)  │
                                                        │ COUNT queued+active │
                                                        └────────────────────┘
```

A reader traces: host curl → Service ClusterIP (real kube-proxy routing, NOT port-forward) →
one of two pods chosen per-connection → that pod's `admitLocked` takes the per-(client,engine)
advisory lock in Postgres → COUNT decides admit/reject → response returns to the host curl AND
a log line is written in that pod's own stdout, tagged with the client-supplied request ID.

### Recommended Project Structure
```
scripts/
└── quota-multireplica-liveproof.sh   # new — D-01/D-04 shape, mirrors keda-gate.sh's
                                        # header/trap/teardown, diverges on reachability
                                        # (ClusterIP direct dial, not port-forward)
deploy/chart/octoconv/
└── (no new file needed — api.replicas:2 via --set, per D-01's "no separate overlay
    strictly required" reading; see Pitfall 6 for the overlay-vs---set tradeoff)
internal/jobs/admission.go            # package doc comment gets the "no replicas:1 caveat"
                                        # statement (Claude's Discretion, mirrors
                                        # internal/ratelimit/ratelimit.go:10)
internal/api/handlers.go               # folded todo 1 (Retry-After midnight race)
internal/api/system_usage_handlers.go  # folded todos 2/3 (enforced flag, empty enum)
```

### Pattern 1: Direct ClusterIP dial instead of `kubectl port-forward` (THE deviation from precedent)
**What:** Discover the Service's ClusterIP live (`kubectl get svc api -n octoconv -o
jsonpath='{.spec.clusterIP}'`) and curl `http://<CLUSTER_IP>:8090/v1/jobs` directly from the host,
never opening a `kubectl port-forward` tunnel for the burst traffic.

**When to use:** Any live gate on OrbStack Kubernetes that needs genuine multi-pod distribution.
Every existing gate in this repo (`keda-gate.sh` et al.) uses `kubectl port-forward svc/api` and
that is CORRECT for them, because they only ever needed to reach "the API," never specifically
"both replicas of the API" — `api.replicas` was 1 in every one of those gates. Phase 55 is the
first phase where the distinction matters.

**Why this works on OrbStack specifically (and would NOT work unmodified on Docker Desktop
Kubernetes, minikube, or kind):** OrbStack documents that "All Kubernetes service types are
accessible directly from Mac, so port forwarding is not needed" and that ClusterIP addresses and
`*.svc.cluster.local` DNS names are directly reachable from the host — the host is effectively
routed into the cluster's network fabric rather than needing an app-layer tunnel.
`[CITED: docs.orbstack.dev/kubernetes/]`

**MUST-VERIFY-AT-EXECUTION:** OrbStack's docs do not explicitly state that this host-routed path
preserves genuine per-connection Service load-balancing across multiple ready endpoints (as opposed
to, hypothetically, some host-side shortcut that also pins to one pod the way port-forward does).
Architecturally this should be the real kube-proxy DNAT path (there is no reason for OrbStack to
implement a separate, single-pod-pinning proxy for ClusterIP specifically — LoadBalancer/NodePort
and ClusterIP are documented as using the same "directly accessible" mechanism, and NodePort access
is explicitly host-machine-wide, which only makes sense if it's real kernel-level routing, not a
per-request app proxy), but this was **not exercised against a live cluster this session**
(compose stack is currently up; bringing up k8s simultaneously would violate the documented OrbStack
discipline in STATE.md). The plan MUST include a cheap calibration step (see Code Examples) as the
FIRST thing the gate script does, before committing to the full 10×10-round budget.

**Fallback if calibration fails:** Run the burst generator **inside** the cluster instead — a
throwaway pod/Job (`kubectl run octoconv-burst --image=curlimages/curl --restart=Never -- sh -c
'...'` or a `kubectl exec` into an already-running throwaway pod) that curls the Service ClusterIP
from inside the cluster network. In-cluster Service routing is uncontroversial, standard Kubernetes
behavior — HIGH confidence this distributes correctly regardless of what the host-routing path
does. This composes with D-04 ("bash script in `scripts/`") by keeping the FIFO-barrier bash logic
in the committed script, but having it execute the curls via `kubectl exec` against a helper pod
rather than as host-local background processes. This is a real complication (the barrier and
per-request timestamp capture both get harder across an exec boundary) — treat it as the fallback
path, not the default, and only build it if the calibration step in the primary path fails.

### Pattern 2: Replica attribution via client-supplied `X-Request-Id` (D-02, verified)
**What:** Every burst request carries `-H "X-Request-Id: burst-<round>-<index>"`. chi's
`middleware.RequestID` (installed ahead of `middleware.Logger` in `internal/api/routes.go:29,31`)
honors an incoming header value verbatim and never overwrites it:
```go
// Source: $(go env GOMODCACHE)/github.com/go-chi/chi/v5@v5.3.0/middleware/request_id.go:69-73
requestID := r.Header.Get(RequestIDHeader)  // RequestIDHeader = "X-Request-Id"
if requestID == "" {
    myid := reqid.Add(1)
    requestID = fmt.Sprintf("%s-%06d", prefix, myid)
}
```
`middleware.Logger`'s `DefaultLogFormatter` then prints that ID as the first token of every access
log line, at request COMPLETION time (its `Write` runs in a `defer`, after the full handler chain
including admission has run):
```
// Source: same module, middleware/logger.go:99-127 (format, reconstructed from source)
[burst-3-7] "POST http://api.octoconv.svc.cluster.local:8090/v1/jobs HTTP/1.1" from 10.0.1.4 - 201 142B in 4.821ms
```
Grepping `kubectl logs <pod-name>` for the literal `burst-3-7` substring identifies which pod
served that specific request; the numeric status token on the same line cross-checks the
client-observed HTTP status. `[VERIFIED: local go module cache read this session]`

**Non-issue confirmed (do not build ANSI-stripping into the harness):** chi's colorizer (`cW` in
`middleware/terminal.go`) only emits ANSI escape codes when `IsTTY` is true, and `IsTTY` is
determined by `os.Stdout.Stat()` reporting a character device. The `api` Deployment sets no
`tty: true` on its container spec (grep-confirmed), so container stdout is a pipe, not a TTY —
`kubectl logs` output will be plain, uncolored text. A prior version of this reasoning assumed
color codes were a parsing risk; verified false for this specific deployment. `[VERIFIED: source
read, both chi middleware and the Deployment template]`

**Timestamp precision caveat:** chi's own log line uses `log.LstdFlags` (second-granularity, no
sub-second precision, host-local `Ldate|Ltime` — not necessarily UTC depending on container TZ).
For evidence requiring finer ordering, use `kubectl logs --timestamps` (container-runtime-appended
RFC3339Nano timestamps), not the app's own embedded date/time. `[CITED: kubectl docs — general
knowledge of `--timestamps` flag behavior]`

### Pattern 3: Quota vs. rate-limit 429 discrimination (the correctness trap CONTEXT.md flags)
**What:** `internal/api/handlers.go:1009` writes quota rejections as:
```go
// Source: internal/api/handlers.go:1009
writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": msg, "code": "quota_exceeded"})
```
`internal/ratelimit`'s `limitHandler` writes rate-limit rejections as `code: "rate_limited"`
(referenced at `internal/api/handlers.go:1005`, not re-read in full this session but consistent
with the existing README/OpenAPI contract established in Phase 52). The harness MUST parse the
JSON body's `code` field, never classify by the bare `429` status alone.

**Concrete numbers to defuse the trap:** `RATE_LIMIT_CLIENT_RPM` defaults to `120`
(`.env.example:21`, `cmd/api/main.go:200`), enforced per-client via `httprate` — but this counter
is **in-process, not shared across replicas** (`internal/ratelimit/ratelimit.go`'s own package doc:
"this package's state stops being shared across replicas"). With the burst split roughly 50/50
across two pods, each pod's local counter only ever sees ~5 requests per round from this client,
well under even a single pod's full 120/min budget — but 10 rounds × 10 requests = 100 total
requests could accumulate within one pod's 60s sliding window if rounds run back-to-back fast
(image conversions are sub-second on the 86-byte `sample.png`-class fixture). **Recommendation:**
raise `RATE_LIMIT_CLIENT_RPM` (and `RATE_LIMIT_IP_RPM`, default `60`) via env/chart override for
the gate run, AND assert zero `"code":"rate_limited"` responses appear in the transcript as an
explicit anti-vacuity check — if the harness silently reclassified a rate-limit rejection as a
quota rejection (or vice versa) by only checking the status code, the gate would report a
plausible-looking pass while proving nothing about QUOT-05. `[VERIFIED: source read + env default
read]`

### Anti-Patterns to Avoid
- **Reaching the API via `kubectl port-forward svc/api` for the burst traffic:** pins to one pod
  by construction (see Pattern 1) — D-05 can never pass if this is used for the actual burst,
  regardless of how correct the admission mechanism is.
- **Classifying admit/reject by HTTP status code alone:** conflates `quota_exceeded` with
  `rate_limited`, both 429 (see Pattern 3).
- **Treating "traffic reached both pods at some point during the run" as sufficient for D-05:**
  CONTEXT.md already rejects this explicitly — the assertion must be that a SPECIFIC admit and a
  SPECIFIC reject landed on different pods, in the SAME round, proving the cap decision crossed the
  replica boundary.
- **Starting round R+1 without asserting the drain from round R completed:** with no slot-release
  code, an undrained slot silently caps round R+1's admits below K, which would still satisfy "never
  more than K admitted" — a false-pass mode CONTEXT.md flags explicitly (D-06).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Per-replica request attribution | A new response header / instrumentation | `X-Request-Id` + `kubectl logs` (D-02) | Already works, zero prod code change, verified against chi source |
| Test client + limit provisioning | A new REST/CLI tool | `cmd/manage-clients set-limit`/`set-mode` | Already exists, exact syntax below |
| Slot release between rounds | A new "reset quota" admin command | `POST /v1/jobs/{id}/cancel` (terminal `canceled`) or wait for natural completion | Phase 39 already ships cancel; no new surface needed |
| Burst concurrency barrier | A Go test with `sync.WaitGroup`/channels | bash + `mkfifo` (D-04) | Explicit project decision — an operator must be able to read the gate; `TestConcurrentBurstAdmitsExactlyK` already covers the in-process case, this proves cross-process/cross-replica |

**Key insight:** every piece of machinery this phase needs already exists in the codebase or in
standard `kubectl`/`helm`. The only genuinely new artifact is the harness script itself and its
reachability choice.

## Common Pitfalls

### Pitfall 1: `kubectl port-forward` silently defeats the entire gate (THE pitfall)
**What goes wrong:** The burst harness reaches the API via `kubectl port-forward svc/api`, copying
every prior live gate verbatim. All 10 requests per round land on the single pod kubectl happened
to pick. D-05's cross-pod admit/reject assertion can never be satisfied — not because of a bug in
the admission mechanism, but because the harness never exercised two replicas at all.
**Why it happens:** It is the literal precedent in this repo, called out by name in D-01
("this is exactly `scripts/keda-gate.sh`'s contract — copy it"), and D-01 is correct about
everything EXCEPT the reachability mechanism, which was never load-bearing in any prior gate.
**How to avoid:** Use the ClusterIP-direct pattern (Pattern 1). Copy `keda-gate.sh`'s
header/trap/teardown/OrbStack-discipline shape, NOT its `kubectl port-forward svc/api "${PORT}:8090"`
line.
**Warning signs:** If a debug run shows 100% of `burst-*` IDs in exactly one pod's log across every
round, this is the failure mode manifesting, not a quota bug.

### Pitfall 2: `curl` connection reuse defeating per-request distribution
**What goes wrong:** If the harness uses a single long-lived `curl` process (or any HTTP client
with keep-alive) to fire all 10 requests in a round, they may reuse one TCP connection, which
pins to whichever endpoint the Service DNAT chose for the FIRST connection — collapsing the
distribution back to one pod even with correct ClusterIP-direct reachability.
**Why it happens:** kube-proxy's Service DNAT decision is made once, at NEW-connection/conntrack-
entry time, not per HTTP request; HTTP/1.1 keep-alive reuses the same TCP connection (and therefore
the same DNAT decision) across multiple requests.
**How to avoid:** D-04's "backgrounded curl" shape already avoids this naturally — one `curl`
*process* per request means one new TCP connection per request (curl does not share connections
across separate process invocations). Do not "optimize" this into a single curl invocation with
multiple `-O`/`--next` URLs, and do not switch to a client library that pools connections across
requests without explicitly disabling keep-alive/connection reuse.
**Warning signs:** All requests in a round landing on the same pod even though the calibration step
(Pitfall 1's mitigation) showed distribution working for single-shot probes.

### Pitfall 3: DNS caching is a non-issue here (documented so it isn't re-litigated)
**What goes wrong (hypothetical, does not actually apply):** One might worry that macOS caching the
`cluster.local` DNS resolution for the API service would pin traffic the way a headless Service's
DNS round-robin can go stale.
**Why it doesn't apply:** A ClusterIP Service has exactly ONE stable virtual IP for its whole
lifetime; the per-connection distribution across the 2 pod endpoints happens at the iptables/
kube-proxy DNAT layer on every NEW connection, not via DNS returning different addresses. Even a
long-lived DNS cache entry for the ClusterIP itself changes nothing about the DNAT layer's endpoint
selection. Using the ClusterIP literal (discovered via `kubectl get svc`) rather than the DNS name
sidesteps this class of concern entirely and is the recommended approach anyway (Pattern 1).
**How to avoid:** Prefer the literal ClusterIP over the DNS name for the burst traffic, precisely
to remove this variable from the picture, even though it is not expected to matter.

### Pitfall 4: Slot drain silently starves later rounds (D-06's own flagged trap)
**What goes wrong:** Round R+1 starts before round R's admitted jobs reach a terminal state. The
occupied slots (no release code exists — Phase 52 D-06/D-08) mean round R+1 admits fewer than K,
possibly zero. "Never more than K admitted" still holds (0 ≤ K), so a naive assertion set would
report PASS while having proven nothing about round R+1's actual behavior under contention.
**Why it happens:** A slot is `queued`+`active`; it only frees on a terminal transition
(`done`/`failed`/`canceled`/reconciler exhaustion). There is deliberately no other release path.
**How to avoid:** Between every round, poll `GET /v1/jobs/{id}` for each admitted job in that round
until it reaches `done`/`failed`/`canceled` (or issue `POST /v1/jobs/{id}/cancel` for a faster,
deterministic drain), and **assert** the drain completed (all admitted jobs terminal) before firing
the next round's barrier. Do not just sleep a fixed duration and hope.
**Warning signs:** Admitted count trending down across rounds (10, 9, 7, 3, 0, 0...) instead of
staying near K=2 every round.

### Pitfall 5: Reconciler racing with a fast round cadence
**What goes wrong:** `RECONCILER_ACTIVE_STALE_AFTER` defaults to `15m` — far longer than this gate
runs — so the reconciler should not interfere. But if the gate script itself crashes/exits between
rounds leaving a job `active` with the worker mid-processing, a resumed/re-run gate could see an
already-consumed slot from a previous aborted run.
**Why it happens:** No slot-release path (Pitfall 4) applies equally to a partially-completed gate
run, not just between rounds within one run.
**How to avoid:** Provision a FRESH test client per gate run (`manage-clients create
"quota-liveproof-$(date +%s)"`, mirroring `keda-gate.sh`'s `SUFFIX=$(date +%s)` pattern) so a prior
aborted run's stuck slots (if any) belong to a different `client_id` and cannot affect the current
run's cap.

### Pitfall 6: `api.replicas: 2` via `--set` vs. a values overlay — pick one deliberately
**What goes wrong:** Silent inconsistency between "did this gate run actually use 2 replicas" if
the flag is easy to typo or omit on a re-run.
**Why it matters:** SC1 is written as "with `api.replicas` set to 2 or more" — the gate's own
preflight should assert this rather than assume the `--set`/overlay took effect.
**How to avoid:** After `helm upgrade --install`, assert
`kubectl get deployment api -n octoconv -o jsonpath='{.spec.replicas}'` equals `2` before doing
anything else — mirroring `keda-gate.sh`'s own `WEBHOOK_REPLICAS_START` assertion pattern for
`webhook-worker`. Given Phase 54 already established `values-capacity-floor.yaml` as the
single-purpose-overlay precedent, either a tiny `values-multireplica.yaml` (`api: {replicas: 2}`)
or a bare `--set api.replicas=2` on top of `values-local.yaml` both work; a dedicated overlay is
slightly more discoverable/diffable but is genuinely Claude's Discretion here — CONTEXT.md does not
lock this choice.

## Code Examples

### Calibration step (run FIRST, before the full gate — closes the P0 MUST-VERIFY gap cheaply)
```bash
# Source: new pattern for this phase, modeled on this repo's existing
# assert_* helper conventions (scripts/keda-gate.sh)
CLUSTER_IP=$(kubectl get svc api -n octoconv -o jsonpath='{.spec.clusterIP}')
assert_nonempty "$CLUSTER_IP" "api Service ClusterIP discovered"

API_PODS=$(kubectl get pods -n octoconv -l app.kubernetes.io/component=api -o jsonpath='{.items[*].metadata.name}')
# assert exactly 2 names here (SC1 precondition)

for i in $(seq 1 20); do
  curl -s -o /dev/null -H "X-Request-Id: calib-${i}" "http://${CLUSTER_IP}:8090/healthz" &
done
wait
sleep 2   # let log lines flush

HIT_POD1=$(kubectl logs "$POD1" -n octoconv | grep -c '\[calib-' || true)
HIT_POD2=$(kubectl logs "$POD2" -n octoconv | grep -c '\[calib-' || true)
if [ "${HIT_POD1:-0}" -eq 0 ] || [ "${HIT_POD2:-0}" -eq 0 ]; then
  echo "FAIL: calibration -- host->ClusterIP routing did not distribute across both api pods (pod1=${HIT_POD1:-0}, pod2=${HIT_POD2:-0}). D-05 cannot be satisfied with this reachability path; fall back to an in-cluster burst generator (Pattern 1 fallback)." >&2
  exit 1
fi
echo "PASS: calibration -- host->ClusterIP routing distributed 20 probes across both pods (pod1=$HIT_POD1, pod2=$HIT_POD2)"
```

### Test client + limit provisioning (exact `manage-clients` syntax, verified from source)
```bash
# Source: cmd/manage-clients/main.go:107-149 (set-limit), :204-218 (set-mode)
CLIENT_OUT=$(go run ./cmd/manage-clients create "quota-liveproof-$(date +%s)")
CLIENT_ID=$(printf '%s\n' "$CLIENT_OUT" | awk '/^client id/{print $3}')  # verify exact label at execution
CLIENT_KEY=$(printf '%s\n' "$CLIENT_OUT" | awk -F': ' '/^api key/{print $2}')

# max-concurrent=2, max-daily-bytes=unlimited ("-"), max-daily-jobs=unlimited ("-")
go run ./cmd/manage-clients set-limit "$CLIENT_ID" image 2 - -
go run ./cmd/manage-clients set-mode "$CLIENT_ID" enforce
```

### Per-request burst with FIFO barrier (D-04 shape)
```bash
# Source: new pattern for this phase (no mkfifo precedent in this repo's scripts/)
BARRIER=$(mktemp -u)
mkfifo "$BARRIER"
for i in $(seq 1 10); do
  (
    exec 3<"$BARRIER"
    read -r -u 3 _   # blocks until the barrier is written to
    STATUS_BODY=$(curl -s -o "/tmp/round-${ROUND}-req-${i}.json" -w '%{http_code}' \
      -H "Authorization: ApiKey $CLIENT_KEY" \
      -H "X-Request-Id: burst-${ROUND}-${i}" \
      -X POST "http://${CLUSTER_IP}:8090/v1/jobs" \
      -F "target=jpg" -F "file=@internal/e2e/testdata/sample.png;type=image/png")
    echo "$STATUS_BODY" > "/tmp/round-${ROUND}-req-${i}.status"
  ) &
done
sleep 0.2   # let all 10 background curls reach the blocking read
printf 'go\n' > "$BARRIER"   # single write releases all 10 readers near-simultaneously
wait
rm -f "$BARRIER"
```

### Classifying results (status + body code, not status alone)
```bash
# Source: new pattern, discriminator per internal/api/handlers.go:1009 (quota_exceeded)
# vs. internal/ratelimit's rate_limited code
for i in $(seq 1 10); do
  status=$(cat "/tmp/round-${ROUND}-req-${i}.status")
  code=$(grep -o '"code":"[^"]*"' "/tmp/round-${ROUND}-req-${i}.json" | cut -d'"' -f4 || true)
  case "$status:$code" in
    201:*) admit_count=$((admit_count+1)) ;;
    429:quota_exceeded) quota_reject_count=$((quota_reject_count+1)) ;;
    429:rate_limited) echo "FAIL: rate_limited hit — RATE_LIMIT_CLIENT_RPM too low for this burst cadence" >&2; exit 1 ;;
    *) echo "FAIL: unexpected status:code $status:$code for req $i" >&2; exit 1 ;;
  esac
done
```

### The "no replicas:1 caveat" counter-statement location (SC2, verified wording model)
```go
// Source: internal/ratelimit/ratelimit.go:1-13 (the caveat being mirrored, NOT repeated)
// "...Both are in-process (github.com/go-chi/httprate's default in-memory
// counter), which matches the current single-API-instance docker-compose
// deployment. If the API is later horizontally scaled behind a load
// balancer, this package's state stops being shared across replicas..."
```
The quota counter-statement belongs in `internal/jobs/admission.go`'s existing package doc comment
(it already documents the advisory-lock design at the top of the file — verified this session, see
`## Code Context` in CONTEXT.md). Add a sentence stating explicitly that, unlike `internal/ratelimit`,
this mechanism carries no `api.replicas: 1`-only caveat, and cite this phase's live gate as the
evidence.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | OrbStack's host→ClusterIP routing genuinely distributes new connections across all ready endpoint pods (not just "is reachable") | Pattern 1 / Summary | If wrong, D-05 is unbuildable via the primary path; the documented fallback (in-cluster burst pod) must be used instead. Closed by the mandatory calibration step before the full gate runs. |
| A2 | `RATE_LIMIT_CLIENT_RPM`/`RATE_LIMIT_IP_RPM` defaults (120/60 rpm) are close enough to the burst's total request volume (100 over 10 rounds) that raising them for the gate run is prudent, not merely paranoid | Pattern 3 | If not raised and margins are tighter than estimated, a `rate_limited` 429 could appear mid-run and either falsely fail the gate or (if misclassified) falsely pass it — closed by the explicit zero-`rate_limited` assertion regardless of whether the limits are raised |
| A3 | kube-proxy on OrbStack's Kubernetes runs in default iptables mode (not IPVS), giving per-new-connection random (not strictly round-robin) endpoint selection | Summary / Pattern 1 | Low risk either way — both iptables-random and IPVS-round-robin distribute across 2 endpoints for 10 independent connections; this only affects the exact probability math, not the qualitative conclusion. Not load-bearing for the plan. |

## Open Questions

1. **Does `manage-clients create`'s stdout literally print a line matching `^client id`?**
   - What we know: `keda-gate.sh` only parses the `api key` line (`awk -F': ' '/^api key/{print $2}'`)
     and separately looks up the client id via a DB query in some other gates; the exact `create`
     output format was not fully re-read this session for the `client id:`-style label.
   - What's unclear: the precise field label/format to `awk`-parse the client ID out of `create`'s
     stdout, if the harness needs it directly (it does, for `set-limit`/`set-mode`, which take a
     client ID, not a name).
   - Recommendation: the plan's implementation step should `grep -n "client id\|log.Println" cmd/manage-clients/main.go` around the `create` case before writing the parsing line, or query
     `SELECT id FROM clients WHERE name = $1` directly (asserted-available `DATABASE_URL` on the
     harness host mirroring `keda-gate.sh`'s own DB port-forward for exactly this purpose).

2. **Exact wall-clock cost per round (worst case, for a 10-round timeout budget).**
   - What we know: `sample.png` is 86 bytes, vips conversions are historically sub-second in this
     codebase's other gates; KEDA's image class has `pollingInterval: 5`/`cooldownPeriod: 60` with
     `minReplicaCount: 0` by default, so only round 1 pays a cold-start tax (subsequent rounds reuse
     the already-scaled worker, since 60s cooldown likely exceeds inter-round latency).
   - What's unclear: the actual first-round cold-start latency on OrbStack for the image worker
     (historically observed in `keda-gate.sh`'s own bounded 120s wait, but not a tight number).
   - Recommendation: budget generously (e.g., 180s for round 1, 30s for rounds 2-10) and, per
     Claude's Discretion already noted in CONTEXT.md, consider `--set
     keda.image.minReplicaCount=1` for this gate's own install (Phase 54 precedent) purely to
     remove cold-start variance from a 10-round timing budget — this is an optional robustness
     improvement, not a correctness requirement.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| `kubectl` | All cluster interaction | ✓ | (local install confirmed) | — |
| `helm` | Chart install | ✓ | (local install confirmed) | — |
| OrbStack Kubernetes cluster | The entire live gate | ✗ (not running this session — `kubectl get nodes` refused connection to `127.0.0.1:26443`) | — | None — this phase cannot be verified without it; execution must bring the cluster up per D-01's discipline |
| Docker Compose stack | — (must be DOWN before k8s comes up) | ✓ **currently UP** (`octoconv-db`, `octoconv-minio`, `octoconv-redis` running) | — | Must be torn down (`docker compose ... down -v`) before the k8s gate runs, per STATE.md discipline — this is a precondition the execution phase must satisfy, not something this research should trigger |

**Missing dependencies with no fallback:**
- A live OrbStack Kubernetes cluster with the chart installed at `api.replicas: 2` — required to
  exercise Pattern 1's calibration step and the actual gate. This research could not bring the
  cluster up (compose stack is currently hot, and starting k8s simultaneously would violate the
  project's own documented OrbStack discipline) and therefore could not empirically confirm A1.

## Security Domain

This phase adds zero new production HTTP surface (D-02: zero prod code changes for the gate
itself; the three folded todos touch existing operator-only/error-response code paths, not new
routes). No new ASVS category becomes newly applicable.

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V4 Access Control | Indirectly (folded todo 2/3 touch `/v1/system/usage`, already `requireOperator`-gated) | Existing `requireOperator` middleware, unchanged by this phase |
| V5 Input Validation | No new input surface | N/A |

No new threat patterns are introduced; the gate script itself runs against a throwaway dev-only
client with dev-only credentials (`values-local.yaml`'s already-committed dev secrets, same trust
model as every other live gate in this repo).

## Sources

### Primary (HIGH confidence)
- `internal/api/routes.go` — middleware order, `middleware.RequestID`/`middleware.Logger` install
  order (read this session)
- `$(go env GOMODCACHE)/github.com/go-chi/chi/v5@v5.3.0/middleware/request_id.go` — verbatim
  incoming-header-honored behavior (read this session)
- `$(go env GOMODCACHE)/github.com/go-chi/chi/v5@v5.3.0/middleware/logger.go` and `terminal.go` —
  access log line format, ANSI-color IsTTY gating (read this session)
- `internal/jobs/admission.go`, `internal/jobs/admission_test.go` — admission mechanism and its
  existing in-process burst test (read this session)
- `internal/api/handlers.go:1009` — `quota_exceeded` code literal (read this session)
- `internal/ratelimit/ratelimit.go` — the `replicas: 1` caveat being mirrored, and
  `RATE_LIMIT_CLIENT_RPM`/`RATE_LIMIT_IP_RPM` semantics (read this session)
- `cmd/manage-clients/main.go` — exact `set-limit`/`set-mode`/`create` CLI syntax (read this
  session)
- `deploy/chart/octoconv/templates/service-api.yaml`, `values.yaml`, `values-local.yaml` — Service
  type (ClusterIP, no `sessionAffinity` set), `api.replicas` default, KEDA image-class defaults
  (`minReplicaCount: 0`, `cooldownPeriod: 60`, `threshold: "5"`) (read this session)
- `scripts/keda-gate.sh` (and grep across all sibling `keda-*` scripts) — confirmed EVERY existing
  live gate uses `kubectl port-forward svc/api`, none uses a ClusterIP-direct or in-cluster burst
  pattern (read this session)
- Local shell: `kubectl get nodes` (connection refused — cluster not running), `orb status`
  (Running — VM up, k8s subsystem not started), `docker compose ps` (compose stack currently up)

### Secondary (MEDIUM confidence)
- OrbStack Kubernetes docs (`docs.orbstack.dev/kubernetes/`) — ClusterIP/NodePort/LoadBalancer host
  reachability without port-forwarding `[CITED]`
- `kubectl port-forward` single-pod-pinning behavior — corroborated by multiple independent sources
  (Kubernetes docs reference page, community discussion) but not exercised live this session
  `[CITED, cross-referenced]`

### Tertiary (LOW confidence)
- Exact kube-proxy mode (iptables vs. IPVS) used by OrbStack's Kubernetes distribution — not found
  in official docs during this session's search; treated as immaterial to the plan (A3 in
  Assumptions Log) since both modes distribute across 2 endpoints for independent connections.

## Metadata

**Confidence breakdown:**
- Standard stack / tooling: HIGH — nothing new installed, all tools confirmed present locally
- Architecture (reachability pattern): MEDIUM — CITED from official docs, architecturally sound,
  but not exercised live this session (blocked by the project's own OrbStack discipline given the
  compose stack was up); the plan MUST treat this as MUST-VERIFY-AT-EXECUTION via the calibration
  step
- Replica attribution mechanics (D-02): HIGH — verified directly against the vendored chi source
- Quota-vs-ratelimit discrimination: HIGH — verified directly against `internal/api/handlers.go`
  and `internal/ratelimit`, with concrete env-var defaults read
- Pitfalls: HIGH for slot-drain/reconciler/rate-limit (all read from source); MEDIUM for the
  networking pitfalls (architecturally reasoned, not live-verified)

**Research date:** 2026-08-06
**Valid until:** 14 days (this research is tightly coupled to a live infrastructure claim that
should be re-confirmed if OrbStack itself is upgraded before execution, or if execution is delayed
long enough that OrbStack's Kubernetes networking implementation could plausibly have changed)
