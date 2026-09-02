---
phase: 58-synchronous-inspection
plan: 03
subsystem: api
tags: [pgxpool, chi, http, healthz, helm, docker-compose, liveness, resources-limits]

# Dependency graph
requires:
  - phase: 58-synchronous-inspection
    provides: "Plan 58-01's pre-phase pinning suite (internal/api/pre58_pinning_test.go, testdata/pre58_*.golden.json) that this plan's Task 2 must leave byte-identical"
provides:
  - "db.ConnectHealth: a dedicated 2-connection Postgres pool used only by GET /healthz's ping, closing the pool-contention path a burst of POST /v1/inspect requests would otherwise share with Repo.PreAdmit"
  - "db.Connect: explicit main-pool sizing via DB_MAX_CONNS (default 10 = 4 concurrent inspect + 6 other traffic), replacing the prior unset-MaxConns/node-CPU-count default"
  - "internal/api.(*Server).HealthRoutes(): a second, minimal chi.Router carrying only GET /healthz, bound to its own http.Server on HEALTH_ADDR"
  - "cmd/api/main.go: three-server graceful shutdown ordering (httpSrv, metricsSrv, healthSrv last)"
  - "deploy/chart/octoconv: health containerPort/Service port, liveness/readiness probes repointed to it, api's first-ever resources.limits (D-06b)"
  - "HEALTH_ADDR and DB_MAX_CONNS declared in .env.example, docker-compose.yml, and the Helm ConfigMap"
affects: [58-06, 58-08]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Two dedicated pgxpool.Pool instances with different sizes for different purposes (main traffic pool vs. a single-consumer health-ping pool), both sized explicitly via pgxpool.ParseConfig+NewWithConfig rather than left to pgxpool's implicit runtime.NumCPU()-derived default"
    - "Second http.Server/chi.Router pair for a single low-traffic endpoint (GET /healthz), mirroring the existing metricsSrv precedent, to decouple accept-path saturation on the main listener from a liveness probe"
    - "Explicit N-step graceful shutdown ordering (public traffic first, operational endpoints last) so the liveness probe answers longest during a rolling-deploy drain"

key-files:
  created:
    - internal/db/db_test.go
    - cmd/api/wiring_test.go
    - .planning/phases/58-synchronous-inspection/deferred-items.md
  modified:
    - internal/db/db.go
    - internal/api/routes.go
    - internal/api/routes_test.go
    - cmd/api/main.go
    - deploy/chart/octoconv/templates/deployment-api.yaml
    - deploy/chart/octoconv/templates/service-api.yaml
    - deploy/chart/octoconv/templates/networkpolicy-metrics.yaml
    - deploy/chart/octoconv/templates/configmap.yaml
    - deploy/chart/octoconv/values.yaml
    - docker-compose.yml
    - .env.example
    - docs/openapi.yaml

key-decisions:
  - "A second HTTP listener for /healthz does NOT by itself isolate the endpoint from inspect load -- the Postgres ping inside handleHealth shares a connection pool with Repo.PreAdmit unless given its own pool (D-06a). Both fixes ship together in this plan: db.ConnectHealth for the dependency half, HealthRoutes/HEALTH_ADDR for the HTTP-accept half."
  - "Main-pool sizing (DB_MAX_CONNS, default 10) is recorded with its arithmetic in the doc comment (4 concurrent inspect + 6 headroom), not inherited from pgxpool's implicit max(4, runtime.NumCPU()) default, which was accidental sizing tied to the node's core count rather than a deliberate ceiling."
  - "api's Deployment gets resources.limits for the first time (D-06b): starting values (2 CPU / 1Gi, mirroring worker) explicitly flagged as subject to live-gate (58-08) calibration, not measured yet."
  - "Shutdown order is httpSrv, then metricsSrv, then healthSrv last, so the liveness probe answers as long as possible while /v1 drains during a rolling deploy."
  - "helm lint/helm template without -f fails on this chart for a pre-existing, unrelated reason (secret.yaml requires values-local.yaml's secrets block) -- logged to deferred-items.md as out of scope, not fixed; all Task 3 verification used the chart's own values-local.yaml overlay."

patterns-established:
  - "Pattern: dedicated small pgxpool for a single low-frequency dependency check, sized independently of the main traffic pool, with MinConns kept warm so the check never pays connection-establishment latency"
  - "Pattern: source-scan wiring tests (cmd/api/wiring_test.go) for wiring facts that cannot be proven by behavior alone -- which identifier landed in which struct-literal field in main()"

requirements-completed: [INSP-05, INSP-06]

# Metrics
duration: 25min
completed: 2026-08-10
---

# Phase 58 Plan 03: Health Isolation and Deploy Resource Limits Summary

**Dedicated Postgres pool + dedicated HTTP listener for GET /healthz (closing two independent isolation gaps, not one), plus api's first-ever Helm/compose resources.limits.**

## Performance

- **Duration:** ~25 min
- **Started:** 2026-08-10T01:15:00Z (approx, first task commit 04:17:18+03:00)
- **Completed:** 2026-08-10T01:32:54Z
- **Tasks:** 3
- **Files modified:** 15 (12 modified, 3 created)

## Accomplishments

- Closed the pool-contention path a burst of `POST /v1/inspect` requests would otherwise exploit against `GET /healthz`'s Postgres ping, via a dedicated 2-connection pool (`db.ConnectHealth`) that never touches inspect traffic.
- Gave the main pool an explicit, arithmetic-backed size (`DB_MAX_CONNS`, default 10) instead of the previous unset-`MaxConns` default that silently inherited `max(4, runtime.NumCPU())` from the node.
- Moved `GET /healthz` onto its own `http.Server`/`chi.Router` pair (`HealthRoutes()`/`HEALTH_ADDR`) while proving byte-for-byte that the response body did not change, and that the existing main-router route (relied on by three live operational consumers) still works unmodified.
- Made graceful shutdown ordering explicit and mechanically checkable: `httpSrv` → `metricsSrv` → `healthSrv` last.
- Closed the long-standing gap where the `api` Deployment carried no `resources.limits` at all (unlike every worker) — added starting values in the Helm chart and mirrored them in compose.
- Propagated the new `HEALTH_ADDR`/`DB_MAX_CONNS` knobs to all three required surfaces (`.env.example`, `docker-compose.yml`, Helm `ConfigMap`), and updated `docs/openapi.yaml`'s `/healthz` description to document the two-listener shape without touching its pinned schema/response bodies.

## Task Commits

Each task was committed atomically:

1. **Task 1: Dedicated health-ping pool + explicit main-pool sizing (D-06a)** - `002ec5b` (feat)
2. **Task 2: /healthz on its own listener with ordered graceful shutdown** - `fb028c0` (feat)
3. **Task 3: Deploy health port + api resources.limits (D-06b/D-08)** - `cbc0608` (feat)

_No separate plan-metadata commit yet — this SUMMARY.md itself is committed as part of closing this plan (see below)._

## Files Created/Modified

- `internal/db/db.go` - `Connect` (explicit `DB_MAX_CONNS` sizing via `pgxpool.ParseConfig`+`NewWithConfig`), new `ConnectHealth` (dedicated 2/1 pool), `envInt32` tolerant parser, package/function doc comments recording the D-06a arithmetic and correcting the earlier "second listener isolates completely" assumption
- `internal/db/db_test.go` - integration tests (skip without `DATABASE_URL`, mirroring `internal/jobs/repo_test.go`) proving `DB_MAX_CONNS` sizing, default fallback, tolerant parsing, health-pool sizing, and pool distinctness
- `cmd/api/main.go` - `healthPool` construction and wiring into `api.HealthDeps.Postgres`, `healthSrv` on `HEALTH_ADDR` with its own goroutine and `ReadHeaderTimeout`, three-step shutdown ordering
- `cmd/api/wiring_test.go` - source-scan tests proving `db.ConnectHealth` is called exactly once, `HealthDeps.Postgres` receives `healthPool` (not `pool`), and `jobs.NewRepo` receives `pool` (not `healthPool`)
- `internal/api/routes.go` - new `HealthRoutes()` method (Recoverer-only minimal router), updated `Routes()` doc comment explaining why `/healthz` stays registered there too
- `internal/api/routes_test.go` - `TestHealthRoutes_*` and `TestHealthRoutesBodyIdenticalToMainRouter` covering Task 2's behavior contract
- `deploy/chart/octoconv/templates/deployment-api.yaml` - `health` containerPort 8091, probes repointed to it (numbers unchanged), new `resources.limits` block
- `deploy/chart/octoconv/templates/service-api.yaml` - `health` Service port for the live-gate's in-cluster driver
- `deploy/chart/octoconv/templates/networkpolicy-metrics.yaml` - new ingress rule admitting `:8091` from the namespace, mirroring the existing `:8090` rule
- `deploy/chart/octoconv/templates/configmap.yaml` - `HEALTH_ADDR`/`DB_MAX_CONNS` entries
- `deploy/chart/octoconv/values.yaml` - `api.resources.limits` starting values
- `docker-compose.yml` - `HEALTH_ADDR`/`DB_MAX_CONNS` env, published port 8091, `deploy.resources.limits` for the `api` service
- `.env.example` - `HEALTH_ADDR`/`DB_MAX_CONNS` documented inline
- `docs/openapi.yaml` - `/healthz` description documents the two-listener shape (schema/response codes untouched)
- `.planning/phases/58-synchronous-inspection/deferred-items.md` - records the pre-existing, out-of-scope `helm lint`/`helm template` (no `-f`) failure caused by `secret.yaml` requiring a secrets overlay

## Decisions Made

- **D-06a closed two ways, not one:** dedicated Postgres pool (Task 1) for the dependency half, dedicated listener (Task 2) for the HTTP-accept half. Neither substitutes for the other; both doc comments say so explicitly to prevent a future reader from re-making the "second listener isolates everything" mistake this plan corrects.
- **`DB_MAX_CONNS` default is 10, with recorded arithmetic** (4 concurrent inspect + 6 headroom), not inherited from pgxpool's implicit default.
- **`api.resources.limits` starting values (2 CPU/1Gi) are explicitly unmeasured**, flagged for calibration by the live gate (58-08), same posture as every worker's own RTF-measured limits were before their own measurement passes.
- **Shutdown order:** `httpSrv` → `metricsSrv` → `healthSrv` last, verified mechanically by line-number comparison, not "by eye."
- **`helm lint`/`helm template` without `-f` is a pre-existing, unrelated failure** (missing secrets in `values.yaml` by design — see `values.yaml`'s own header comment: "Real dev-credential values live in `values-local.yaml`, never here"). Logged to `deferred-items.md`, not fixed; all Task 3 verification below used `-f deploy/chart/octoconv/values-local.yaml`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Used `values-local.yaml` overlay for helm lint/template verification**
- **Found during:** Task 3 acceptance-criteria verification
- **Issue:** `helm lint deploy/chart/octoconv` / `helm template ...` with no `-f` fail with `nil pointer evaluating interface {}.postgresPassword` — `templates/secret.yaml` requires a `secrets:` values block that `values.yaml` deliberately never provides (project convention, unrelated to this plan's edits).
- **Fix:** Ran all Task 3 helm verification commands with `-f deploy/chart/octoconv/values-local.yaml` (the chart's own supplied dev-secrets overlay) instead. Logged the literal, unmodified-repo failure to `deferred-items.md` rather than silently substituting the command.
- **Files modified:** `.planning/phases/58-synchronous-inspection/deferred-items.md` (new)
- **Verification:** Confirmed the same nil-pointer failure occurs on a clean pre-Task-3 chart (i.e., not introduced by this plan); `helm lint`/`helm template` both succeed cleanly with the overlay after Task 3's changes.
- **Committed in:** `cbc0608` (Task 3 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking, pre-existing/out-of-scope)
**Impact on plan:** No scope creep — the underlying chart gap (`secret.yaml` requiring an overlay) predates Phase 58 entirely and is not caused by any file this plan touches.

## Issues Encountered

None beyond the deviation above.

## Verification Evidence

All commands below were run for real against this worktree; output is reproduced (trimmed where noted).

### Task 1

```
$ go test ./internal/db/... ./cmd/api/... -count=1        # no DATABASE_URL -- integration tests skip
ok  	github.com/apaderin/octoconv/internal/db	1.044s
ok  	github.com/apaderin/octoconv/cmd/api	0.954s

$ DATABASE_URL="postgres://octo:octo-pass@localhost:5434/octo_db" go test ./internal/db/... -count=1 -v
--- PASS: TestConnect_MaxConnsFromEnv (0.03s)
--- PASS: TestConnect_MaxConnsDefaultWhenUnset (0.01s)
--- PASS: TestConnect_MaxConnsToleratesUnparseable (0.01s)
--- PASS: TestConnectHealth_Sizing (0.01s)
--- PASS: TestConnectHealth_DistinctFromConnect (0.02s)
PASS

$ grep -c 'func ConnectHealth' internal/db/db.go            # 1
$ grep -c 'pgxpool.ParseConfig' internal/db/db.go           # 2
$ grep -c 'pgxpool.New(ctx, dsn)' internal/db/db.go         # 0
$ grep -v '^\s*//' internal/db/db.go | grep -c 'MaxConns'   # 4  (>= 2 required)
$ grep -v '^\s*//' cmd/api/main.go | grep -c 'Postgres: *healthPool'  # 1
$ grep -v '^\s*//' cmd/api/main.go | grep -c 'Postgres: *pool'        # 0
$ grep -v '^\s*//' cmd/api/main.go | grep -c 'jobs.NewRepo(pool)'     # 1
```

Arithmetic markers present in `internal/db/db.go`: `DB_MAX_CONNS` (7 occurrences), `INSPECT_MAX_CONCURRENT` (4), `D-06a` (7) — none missing.

**Inversion 1** (`Postgres: pool` instead of `healthPool`):
```
$ go test ./cmd/api/... -run TestHealthDepsPostgresIsHealthPool -count=1 -v
--- FAIL: TestHealthDepsPostgresIsHealthPool (0.00s)
    wiring_test.go:70: api.HealthDeps{Postgres: pool, ...} in main.go, want Postgres: healthPool (D-06a) --
    Postgres: pool reopens the pool-contention path a burst of inspect requests can exploit to fail
    /healthz's liveness ping regardless of which listener answers the HTTP request
```
Reverted; test green again.

**Inversion 2** (removed `cfg.MaxConns = healthPoolMaxConns` from `ConnectHealth`):
```
$ DATABASE_URL=... go test ./internal/db/... -run TestConnectHealth_Sizing -count=1 -v
--- FAIL: TestConnectHealth_Sizing (0.04s)
    db_test.go:89: Config().MaxConns = 11, want 2
```
Reverted; test green again. (11 is this host's `pgxpool` node-CPU-derived default — itself evidence of the accidental sizing this task replaces.)

`go vet ./...` exit 0. `gofmt -l internal/db cmd/api` empty. `go test ./internal/api/ -run 'TestPre58Golden' -count=1` PASS.

### Task 2

```
$ go test ./internal/api/... ./cmd/api/... -count=1
ok  	github.com/apaderin/octoconv/internal/api	0.567s
ok  	github.com/apaderin/octoconv/cmd/api	1.067s

$ grep -c 'func (s \*Server) HealthRoutes()' internal/api/routes.go   # 1
$ grep -v '^\s*//' internal/api/routes.go | grep -c '"/healthz"'     # 2
$ grep -v '^\s*//' cmd/api/main.go | grep -c 'HEALTH_ADDR'           # 1
$ grep -v '^\s*//' cmd/api/main.go | grep -c 'healthSrv'             # 3
$ grep -n 'httpSrv.Shutdown\|metricsSrv.Shutdown\|healthSrv.Shutdown' cmd/api/main.go
355:	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
358:	if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
361:	if err := healthSrv.Shutdown(shutdownCtx); err != nil {
```
`healthSrv.Shutdown` (361) is strictly greater than both other line numbers.

```
$ go test ./internal/api/ -run 'TestHealthRoutesBodyIdenticalToMainRouter' -count=1 -v   # PASS
$ go test ./internal/api/ -run 'TestHealthz' -count=1 -v                                  # PASS (both subtests)
$ DATABASE_URL=... go test ./internal/api/ -run 'TestPre58Golden' -count=1 -v             # PASS
$ git rev-list --count HEAD -- internal/api/testdata/pre58_healthz.golden.json            # 1
```

**Inversion 1** (registered `/v1/jobs` on `HealthRoutes()`):
```
$ go test ./internal/api/ -run 'TestHealthRoutes_CarriesNoV1Routes' -count=1 -v
--- FAIL: TestHealthRoutes_CarriesNoV1Routes (0.00s)
    routes_test.go:70: status = 500, want 404 (HealthRoutes() must carry no /v1 routes); body=
```
(500 rather than a clean 404 because the unauthenticated request panicked inside `handleListJobs` on a nil auth context, caught by `middleware.Recoverer` — the assertion still correctly failed, which is what matters.) Reverted; test green again.

**Inversion 2** (moved `healthSrv.Shutdown` first):
```
$ grep -n 'httpSrv.Shutdown\|metricsSrv.Shutdown\|healthSrv.Shutdown' cmd/api/main.go
355:	if err := healthSrv.Shutdown(shutdownCtx); err != nil {
358:	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
361:	if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
```
`healthSrv.Shutdown` (355) is no longer the largest line number — the order-check command itself (not a separate assertion) surfaces the wrong order. Reverted; original order (355/358/361 = httpSrv/metricsSrv/healthSrv) restored and re-verified.

`go vet ./...` exit 0. `gofmt -l internal/api cmd/api` empty.

### Task 3

```
$ helm lint deploy/chart/octoconv                          # exit 1, pre-existing/unrelated (see Deviations)
$ helm lint deploy/chart/octoconv -f deploy/chart/octoconv/values-local.yaml
==> Linting deploy/chart/octoconv
[INFO] Chart.yaml: icon is recommended
1 chart(s) linted, 0 chart(s) failed                        # exit 0

$ helm template octoconv deploy/chart/octoconv --namespace octoconv -f deploy/chart/octoconv/values-local.yaml > /tmp/58-03-render.yaml
$ echo exit=$?                                               # exit=0

$ awk '/^  name: api$/,/^---/' /tmp/58-03-render.yaml | grep -c 'port: 8091'   # 3  (>= 2 required)
$ awk '/kind: Deployment/,/^---/' /tmp/58-03-render.yaml | grep -A3 'httpGet:' | grep -c 'port: 8090'  # 0
$ grep -c 'periodSeconds: 10' /tmp/58-03-render.yaml         # 10 (same as pre-Task-3 baseline)
$ grep -c 'failureThreshold: 6' /tmp/58-03-render.yaml       # 10 (same as pre-Task-3 baseline)
$ grep -c 'HEALTH_ADDR' /tmp/58-03-render.yaml               # 2
$ awk '/^  name: api$/,/^---/' /tmp/58-03-render.yaml | grep -c 'memory:'  # 1
$ grep -c 'port: 8091' /tmp/58-03-render.yaml                # 4  (>= 2 required, Service + NetworkPolicy)

$ docker compose config -q; echo exit=$?                     # exit=0
$ docker compose config | grep -c 'HEALTH_ADDR'               # 1

$ grep -c '^HEALTH_ADDR=' .env.example                        # 1
$ grep -c '^DB_MAX_CONNS=' .env.example                       # 1
```
Both `HEALTH_ADDR` and `DB_MAX_CONNS` confirmed present in all three of `.env.example`, `docker-compose.yml`, and the Helm ConfigMap (checked pairwise with `grep -qF`, all three checks succeeded).

```
$ grep -c 'HEALTH_ADDR' docs/openapi.yaml                     # 2
$ DATABASE_URL=... go test ./internal/api/ -run 'TestPre58Golden' -count=1   # PASS
```

**Baseline (captured BEFORE any Task 3 edit, chart rendered with the same `-f values-local.yaml`):** `periodSeconds: 10` = 10 occurrences, `failureThreshold: 6` = 10 occurrences, `containerPort: 8090` = 1 occurrence. Post-Task-3 numbers for the first two are unchanged (10/10); the probe threshold contract the live gate calibrates against was not touched, only the port they poll.

**Inversion** (removed the `health` port block from `service-api.yaml`):
```
$ helm template ... -f values-local.yaml | grep -c 'port: 8091'   # 3 (was 4)
```
Reverted; re-rendered, count back to 4.

`git diff --stat -- go.mod go.sum` — empty (no dependency changes, D-01/D-02 respected).

### Full-plan verification

```
$ DATABASE_URL="postgres://octo:octo-pass@localhost:5434/octo_db" go test ./... -count=1
ok  	github.com/apaderin/octoconv/cmd/api	0.865s
... (all 27 testable packages) ...
ok  	github.com/apaderin/octoconv/internal/worker	7.328s
$ go vet ./...              # exit 0
$ gofmt -l internal/db internal/api cmd/api   # empty
```

## Phase Goal (D-17): every path load on /v1/* can still reach /healthz

Per this plan's `<verification>` requirement, every path by which `/v1/*` traffic can still affect `/healthz`'s answer, and its disposition:

| Shared path | Closed by | Status |
|---|---|---|
| Postgres connection pool (`handleHealth`'s ping vs. `Repo.PreAdmit`'s quota check) | **This plan (Task 1, D-06a)** | Closed — dedicated `ConnectHealth` pool, never touched by inspect/job traffic |
| HTTP accept path / listener socket (`/v1/*` saturating the same `http.Server` `/healthz` answers on) | **This plan (Task 2)** | Closed — separate `http.Server` on `HEALTH_ADDR` |
| Node-level cgroup pressure (no CPU/memory ceiling on the `api` container at all) | **This plan (Task 3, D-06b)** | Closed as a backstop — `resources.limits` added; starting values, calibrated by 58-08 |
| OS process / `GOMAXPROCS` / Go scheduler (CPU contention between an inspect parse goroutine and `handleHealth`'s own goroutine) | **58-06** (work ceiling + concurrency semaphore) | Residual until 58-06 lands; Go's async preemption (1.14+) bounds this to added latency, not starvation, per 58-RESEARCH.md Q4 — measured by the live gate (58-08), not closed here |
| Shared Redis client (`redisPinger`'s dedicated connection vs. asynq's own connections) | Not applicable to inspect | Residual/inapplicable — per 58-CONTEXT.md D-06, synchronous inspect never enqueues a job or touches Redis; this path is not stressed by inspect load. Unaddressed by any plan in this phase because there is nothing to address |
| Shared S3/MinIO client (`s.health.S3`'s ping vs. `storage.Upload`) | Not applicable to inspect | Residual/inapplicable — inspect is a pure in-memory byte parse (D-01/D-02); it never calls `Storage.Upload`. Same reasoning as Redis above |
| Main Postgres pool's OWN headroom under a real burst (Task 1 sizes it, but `POST /v1/jobs` and future inspect `PreAdmit` calls still share it) | **This plan (Task 1) sizes it; 58-06 populates the actual inspect load; 58-08 measures it live** | Partially closed — explicit sizing with recorded arithmetic ships here; the live gate is what proves the arithmetic holds under a real burst |
| inspect's own rate/concurrency budget (INSP-05's semaphore and rate limiter) | **58-06** | Not this plan's scope — this plan only prepares the ground (pool sizing, listener split, resource ceiling) that 58-06's mechanisms will run inside of |

## User Setup Required

None — no external service configuration required. Operators who run compose or the Helm chart pick up `HEALTH_ADDR`/`DB_MAX_CONNS` automatically from the updated defaults; no manual `.env` edit is required unless overriding them.

## Next Phase Readiness

- The main pool's `DB_MAX_CONNS` default (10) already assumes `INSPECT_MAX_CONCURRENT=4` (Plan 58-06's own default) — if 58-06 ships a different default, `internal/db/db.go`'s doc comment arithmetic and `.env.example`'s comment both need a matching update, not just the code.
- `PreAdmit` calls introduced by inspect (58-06) will run against the MAIN pool this plan sized — 58-06 should not introduce a second pool for inspect's own PreAdmit reads; that would defeat the whole point of Task 1's arithmetic.
- The live gate (58-08) has concrete numbers to calibrate against now: `resources.limits` (2 CPU/1Gi, starting values) and the unchanged probe contract (`periodSeconds: 10`, `failureThreshold: 6`, i.e. ~60s of continuous failure before a restart).
- `.planning/phases/58-synchronous-inspection/deferred-items.md` now exists and should be checked by later plans in this phase before assuming a clean `helm lint`/`helm template` run — the pre-existing secrets-overlay requirement is unrelated to any of them but will surface identically.

## Self-Check: PASSED

Verified directly against this worktree:
- `internal/db/db.go`, `internal/db/db_test.go`, `cmd/api/main.go`, `cmd/api/wiring_test.go`, `internal/api/routes.go`, `internal/api/routes_test.go` — all FOUND on disk with the content described above.
- `deploy/chart/octoconv/templates/deployment-api.yaml`, `service-api.yaml`, `networkpolicy-metrics.yaml`, `configmap.yaml`, `values.yaml`, `docker-compose.yml`, `.env.example`, `docs/openapi.yaml` — all FOUND, modified as described.
- `.planning/phases/58-synchronous-inspection/deferred-items.md` — FOUND, force-added past `.planning/`'s top-level `.gitignore` entry (matching the project's existing convention of tracking curated `.planning` files).
- Commits `002ec5b`, `fb028c0`, `cbc0608` — all FOUND in `git log --oneline`.
- `git rev-list --count HEAD -- internal/api/testdata/pre58_healthz.golden.json` = 1 — golden file untouched across all three tasks.

---
*Phase: 58-synchronous-inspection*
*Completed: 2026-08-10*
