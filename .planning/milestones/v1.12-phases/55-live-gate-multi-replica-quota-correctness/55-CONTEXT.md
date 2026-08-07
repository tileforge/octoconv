# Phase 55: Live Gate — Multi-Replica Quota Correctness - Context

**Gathered:** 2026-08-06
**Status:** Ready for planning

<domain>
## Phase Boundary

Prove, on a live OrbStack Kubernetes run with `api.replicas: 2`, that the Phase 52 quota
admission mechanism enforces a client's per-engine **concurrency cap `K`** correctly when two API
replicas concurrently serve the same client — the one claim in v1.12 that cannot be established
statically. Capture and commit the evidence, and record explicitly that the quota mechanism,
unlike `internal/ratelimit`, carries **no** `replicas: 1`-only caveat.

The mechanism itself already exists and is already replica-safe by construction
(`internal/jobs/admission.go`: `pg_advisory_xact_lock` on `quotaLockKeys(clientID, engine)` plus a
`COUNT` over `queued`+`active` inside the same transaction as the job insert). This phase does not
build it and does not change it. This phase is the **proof**, plus a reproducible harness so the
proof can be re-run.

Folded into this phase (see D-07): the three open v1.12 API todos, so the milestone closes with an
empty todo queue.

**Out of scope:** any change to the admission algorithm, the `client_limits` schema, the rejection
shape (`429` + `code: quota_exceeded`), engine-class queues, KEDA, or the chart beyond setting
`api.replicas`.

</domain>

<decisions>
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

</deferred>

---

*Phase: 55-live-gate-multi-replica-quota-correctness*
*Context gathered: 2026-08-06*
