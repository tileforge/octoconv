# Phase 55: Live Gate — Multi-Replica Quota Correctness - Pattern Map

**Mapped:** 2026-08-06
**Files analyzed:** 6 (1 new script, 3 modified Go files, 1 doc-comment addition, 1 spec file; chart
overlay is optional/Claude's Discretion per D-01 — no overlay file is created if `--set` is used)
**Analogs found:** 6 / 6

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|--------------------|------|-----------|-----------------|----------------|
| `scripts/quota-multireplica-liveproof.sh` (name at Claude's Discretion) | test/utility (operator-run live-gate bash script) | request-response (burst) + batch (repeated rounds) | `scripts/keda-gate.sh` (header/trap/teardown/OrbStack discipline) + `scripts/cancel-liveproof.sh` (argument register + real-observable-not-DB-row discipline) + `scripts/av-progress-liveproof.sh` STEP 6 (repeated-cycle loop shape) | role-match (structural composite; no exact prior-art for ClusterIP-direct or FIFO-barrier burst) |
| `deploy/chart/octoconv/values-multireplica.yaml` (OPTIONAL — Claude's Discretion, D-01 says `--set` also acceptable) | config (Helm values overlay) | batch (rendered once at install) | `deploy/chart/octoconv/values-capacity-floor.yaml` (single-purpose overlay register, exact invocation comment, layering-divergence note) | exact (if the overlay path is chosen) |
| `internal/jobs/admission.go` (package doc comment addition only) | model (doc comment) | n/a | `internal/ratelimit/ratelimit.go` lines 1-13 (the caveat being mirrored / explicitly NOT repeated) | exact |
| `internal/api/handlers.go` (`quotaRejection`, `secondsUntilUTCMidnight`, sentinel contract) | controller | request-response | same file's existing `quotaRejection`/`writeQuotaError`/`secondsUntilUTCMidnight` (self-analog — this is a targeted bug fix, not a new pattern) | exact |
| `internal/jobs/jobs.go` / `internal/jobs/admission.go` (sentinel error contract change for todo 1) | model | n/a | `internal/jobs/admission.go` lines 45-55 (existing three-sentinel `var` block + doc comment explaining why three, not one) | exact |
| `internal/api/system_usage_handlers.go` (`enforced` flag + enum-safety guard) | controller | request-response (CRUD read) | `internal/jobs/usage_query.go` lines 85-128 (`usageInFlightJobsSQL`/`usageInFlightBytesSQL`'s `client_id IS NOT NULL` defensive-guard pattern + its comment register) | role-match (mirror the *defensive-guard reasoning*, not the SQL itself — the todo 3 bug is in the handler's two-read merge, not in usage_query.go) |
| `docs/openapi.yaml` (schema updates for `enforced` field + confirm `enforcement_mode` enum never empty) | config (API spec) | n/a | same file's existing `system_usage` response schema (lines ~934-1109), specifically the `enforcement_mode` property (lines 1060-1070) and `required` array (line 1108) | exact |

## Pattern Assignments

### `scripts/quota-multireplica-liveproof.sh` (test/utility, request-response + batch)

**Primary analog:** `scripts/keda-gate.sh` (structural shell: header, assertion helpers, trap teardown)
**Secondary analog:** `scripts/cancel-liveproof.sh` (argument register, "real observable not DB row")
**Tertiary analog:** `scripts/av-progress-liveproof.sh` STEP 6 (repeated-round loop shape)

**Header/contract register** (`scripts/keda-gate.sh:1-30`):
```bash
#!/usr/bin/env bash
# keda-gate.sh -- Phase 27 (KEDA autoscaling) live hard gate.
#
# Proves, against a REAL OrbStack Kubernetes cluster, D-12:
#   SC1 (D-12a): ...
#
# OrbStack discipline (D-13): the CALLER must ensure the compose stack is
# DOWN (`docker compose ... down -v`) and that all app images are
# pre-built SEQUENTIALLY with the pinned `dev` tag BEFORE this script runs.
# This script itself never runs `docker compose up`/`docker build`. It
# tears the k8s stack down (helm uninstall) unconditionally on exit (trap),
# success or failure, so OrbStack is never left hot.
#
# This script's exit code IS the gate: any failed assertion aborts non-zero
# (set -e) with a loud FAIL message, and every check prints the value it
# observed so the transcript is self-documenting evidence.
set -euo pipefail

cd "$(dirname "$0")/.."
```
Copy this register verbatim in spirit for the new script's own header: what SC1/SC2 this proves,
what is explicitly out of scope (daily-jobs/daily-bytes caps per D-03, CI per `<deferred>`), the
OrbStack caller contract (compose DOWN, sequential pre-built `:dev` images, script never runs
`docker build`/`docker compose up`), and the "exit code IS the gate" sentence.

**THE required divergence — do NOT copy this line** (`scripts/keda-gate.sh:316`):
```bash
kubectl port-forward -n "$NAMESPACE" svc/api "${API_LOCAL_PORT}:8090" >/tmp/keda-gate-api-pf.log 2>&1 &
API_PF_PID=$!
```
RESEARCH.md Pattern 1 / Pitfall 1 is explicit: every existing gate reaches the API via
`kubectl port-forward svc/api`, which pins to ONE backend pod for the life of the connection —
correct for every prior gate (all ran at `api.replicas: 1`) but fatal for this one (D-05's
cross-pod admit/reject assertion can never pass). Replace with the ClusterIP-direct dial from
RESEARCH.md's Code Examples section (discover via
`kubectl get svc api -n octoconv -o jsonpath='{.spec.clusterIP}'`, curl that IP directly, no
port-forward for the burst traffic — a `kubectl port-forward` to `svc/postgres` for DB access,
mirroring `keda-gate.sh:318`, remains fine since it is not the load-bearing path). Include the
mandatory calibration step (RESEARCH.md Code Examples, "Calibration step") as the FIRST gate step.

**Assertion helpers** (`scripts/keda-gate.sh:59-93`):
```bash
assert_eq() {
	local expected="$1" actual="$2" label="$3"
	if [ "$expected" != "$actual" ]; then
		echo "FAIL: $label -- expected [$expected], got [$actual]" >&2
		exit 1
	fi
	PASS_COUNT=$((PASS_COUNT + 1))
	echo "PASS: $label == $actual"
}

assert_nonempty() {
	local value="$1" label="$2"
	if [ -z "$value" ]; then
		echo "FAIL: $label -- expected a non-empty value, got empty" >&2
		exit 1
	fi
	PASS_COUNT=$((PASS_COUNT + 1))
	echo "PASS: $label == $value"
}

log() { echo ""; echo "--- $* ---"; }
```
`scripts/cancel-liveproof.sh:112-120` additionally has `assert_empty` (mirror for "zero
`rate_limited` responses observed" per RESEARCH.md Pattern 3's discriminator requirement) and
`sse-liveproof.sh:171-186` has `assert_le`/`assert_ge` (useful for "admitted count == K exactly").

**Trap-based unconditional teardown** (`scripts/keda-gate.sh:100-133`):
```bash
teardown() {
	local exit_code=$?
	echo ""
	echo "=== TEARDOWN (D-13: OrbStack must never be left hot) ==="

	if [ -n "$API_PF_PID" ]; then
		kill "$API_PF_PID" >/dev/null 2>&1 || true
	fi
	...
	helm uninstall octoconv -n "$NAMESPACE" >/dev/null 2>&1 || true
	helm uninstall keda -n "$KEDA_NAMESPACE" >/dev/null 2>&1 || true
	...
	echo ""
	if [ "$exit_code" -eq 0 ] && [ "$GATE_OK" = "1" ]; then
		echo "✅ PASS -- ..."
	else
		echo "❌ FAIL -- ..." >&2
	fi
	exit "$exit_code"
}
trap teardown EXIT
```
This phase's gate additionally needs to tear down the throwaway test client
(`cmd/manage-clients` has no `delete`; `scripts/cancel-liveproof.sh` and `keda-gate.sh` both leave
the minted client behind — that is the established precedent, not an omission to fix here).

**Preflight discipline** (`scripts/keda-gate.sh:140-150`):
```bash
kubectl get nodes >/dev/null
echo "PASS: kubectl reaches the OrbStack cluster (context: $(kubectl config current-context))"

COMPOSE_UP=$(docker compose ps --format '{{.Names}}' 2>/dev/null | grep -c '^octoconv-' || true)
if [ "${COMPOSE_UP:-0}" -gt 0 ]; then
	echo "FAIL: compose stack appears to be UP ($COMPOSE_UP octoconv-* containers running) -- compose and k8s stacks must NEVER be hot simultaneously (D-13). Run 'docker compose ... down -v' first." >&2
	exit 1
fi
echo "PASS: compose stack is down (0 octoconv-* containers running)"
```
Copy verbatim (this phase's D-01 explicitly carries forward the same OrbStack discipline).

**"Why go test is not sufficient proof" argument register** (`scripts/cancel-liveproof.sh:5-19`):
```bash
# WHY THIS SCRIPT EXISTS -- `go test -race` alone is NOT sufficient proof:
# unit/integration tests (internal/jobs/repo_test.go's
# TestCanceledAllowListBackstop, internal/reconciler's
# TestSweepNeverResurrectsCanceled) prove the Postgres-authoritative
# non-resurrection backstop (D-05/D-10) -- that a canceled job's status can
# never be flipped back to active/done/failed. They do NOT, and cannot,
# prove that a REAL external ffmpeg child process spawned by the av-worker
# is genuinely SIGKILLed when a job is canceled mid-flight: that requires a
# real docker-compose stack, ... The DB row reading "canceled" is
# necessary but NOT sufficient -- the only genuine proof is watching the
# ffmpeg PID disappear from the av-worker container's own process table.
```
Rewrite this argument for THIS phase's specific case: `internal/jobs/admission_test.go`'s
`TestConcurrentBurstAdmitsExactlyK` (`:198`) proves the cap holds under N concurrent goroutines
INSIDE ONE PROCESS sharing one `*pgxpool.Pool` — it cannot prove two SEPARATE API processes
(replicas) racing the SAME advisory lock from SEPARATE connections/pools, which is the actual
production topology QUOT-05 claims. Name that test explicitly and state precisely what it does
NOT cover, per this project's established register.

**Test client + limit provisioning** (`scripts/cancel-liveproof.sh:187-198`, exact `manage-clients`
syntax verified against `cmd/manage-clients/main.go` `create`/`set-limit`/`set-mode` cases at
lines 47/107/204):
```bash
SUFFIX=$(date +%s)
CLIENT_OUT=$(DATABASE_URL="$DATABASE_URL_HOST" API_KEY_SALT="$API_KEY_SALT" go run ./cmd/manage-clients create "cancel-liveproof-${SUFFIX}")
CLIENT_KEY=$(printf '%s\n' "$CLIENT_OUT" | awk -F': ' '/^api key/{print $2}')
if [ -z "$CLIENT_KEY" ]; then
	echo "FAIL: could not mint a client/API key against $DATABASE_URL_HOST -- output was:" >&2
	echo "$CLIENT_OUT" >&2
	exit 1
fi
```
Extend with `set-limit "$CLIENT_ID" image 2 - -` and `set-mode "$CLIENT_ID" enforce`
(exact syntax verified in RESEARCH.md's Code Examples section, sourced from
`cmd/manage-clients/main.go:107-149,204-218`). Note RESEARCH.md's Open Question 1: the exact label
`manage-clients create` prints for the client ID (as opposed to the key) was not confirmed this
session — grep `cmd/manage-clients/main.go`'s `create` case for the literal print format before
writing the `awk` line, or resolve the ID via a direct `SELECT id FROM clients WHERE name = $1`.

**Repeated-round loop shape** (`scripts/av-progress-liveproof.sh:562-579`, the closest existing
"repeat N times, aggregate a check across all reps" precedent in this repo — no exact `R
rounds, each independently pass/fail, report N/N` precedent exists, per RESEARCH.md's explicit
"ASSUMED-NEW" flag on `mkfifo`):
```bash
log "STEP 6 (HARD GATE): goroutine-count guard across $GOROUTINE_CYCLES repeated ramp+kill cycles"

for cycle in $(seq 1 "$GOROUTINE_CYCLES"); do
	echo "-- goroutine-guard cycle $cycle/$GOROUTINE_CYCLES --"
	rampThenKillOneCycle "$CYCLE_DURATION_S" "av-progress-liveproof-cycle-${cycle}.mkv" >/dev/null
done
```
Adapt this shape but make EACH round its own hard gate (unlike this example, where only the
AGGREGATE post-loop delta is asserted): per D-06, round R must assert, before round R+1 starts,
(a) admitted count == K exactly, (b) `N - K` rejections all classified `quota_exceeded` (never
`rate_limited` — Pattern 3's discriminator), (c) D-05's cross-pod admit+reject-on-different-pods
split, and (d) the slot-drain from round R completed (all admitted jobs terminal) BEFORE round
R+1's barrier fires (Pitfall 4). Track a `ROUNDS_PASSED` counter and print `$ROUNDS_PASSED/10` in
the final summary, matching this project's own stated discipline ("keda-gate 21/21, cancel 12/12,
SSE 15/15, progress 40/40" — CONTEXT.md D-06).

**FIFO barrier + per-request curl process** (RESEARCH.md Code Examples — genuinely new to this
repo, no existing analog; write from the researched pattern, not a codebase copy):
```bash
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
sleep 0.2
printf 'go\n' > "$BARRIER"
wait
rm -f "$BARRIER"
```
Pitfall 2 is load-bearing: one `curl` PROCESS per request (not one curl invocation reused across
requests) is what guarantees one fresh TCP connection per request, which is what lets kube-proxy's
per-NEW-connection DNAT actually distribute — do not "optimize" this into a single curl call.

**Job GET / status polling + terminal-state drain** (`scripts/keda-gate.sh:454-464`, the existing
poll-until-terminal shape to reuse for D-06's slot drain):
```bash
job_status=""
for i in $(seq 1 60); do
	code=$(curl -s -o /tmp/keda-gate-image-job.json -w '%{http_code}' -H "Authorization: ApiKey $CLIENT_KEY" "$API_BASE/v1/jobs/$IMAGE_JOB_ID")
	job_status=$(grep -o '"status":"[^"]*"' /tmp/keda-gate-image-job.json | head -1 | cut -d'"' -f4 || true)
	if [ "$job_status" = "done" ] || [ "$job_status" = "failed" ]; then
		break
	fi
	sleep 2
done
assert_eq "done" "$job_status" "image job $IMAGE_JOB_ID reaches done"
```
`scripts/cancel-liveproof.sh`'s `waitForStatus` helper (lines 241-259) is the more general form
(treats `done`/`failed`/`canceled` all as terminal) and its `cancelJob` helper (lines 316-329) is
the direct pattern for the D-06 discretion choice "drain via cancel" — reuse both nearly verbatim
if cancel is chosen over natural-completion wait.

**Classifying admit/reject by body `code`, never bare status** (new pattern per RESEARCH.md
Pattern 3, grounded in `internal/api/handlers.go:1009`'s `quota_exceeded` literal and
`internal/ratelimit/ratelimit.go:117`'s `rate_limited` literal):
```bash
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
Note: `postJob` in `scripts/keda-gate.sh:348-360` asserts `202` for job creation, but
`internal/api/handlers.go`'s actual admit status is `201` (confirm the real status code against
`internal/api/handlers.go`'s `handleCreateJob` success path before wiring this — do not blindly
copy keda-gate.sh's `202` literal).

**Redacted-value printing for anything sensitive** (`scripts/keda-gate.sh:83-91`):
```bash
assert_nonempty_redacted() {
	local value="$1" label="$2"
	if [ -z "$value" ]; then
		echo "FAIL: $label -- expected a non-empty value, got empty" >&2
		exit 1
	fi
	PASS_COUNT=$((PASS_COUNT + 1))
	echo "PASS: $label == [REDACTED, ${#value} chars]"
}
```
Apply to the minted client's API key in transcript output (mirrors `cancel-liveproof.sh:198`'s
own `${#CLIENT_KEY} chars` redaction).

---

### `deploy/chart/octoconv/values-multireplica.yaml` (OPTIONAL, config)

**Analog:** `deploy/chart/octoconv/values-capacity-floor.yaml` (single-purpose overlay register)

```yaml
# Capacity-floor overlay — EXAMPLE ONLY, this is NOT a production values
# file to be applied unreviewed. ...
#
# LAYERING DELIBERATELY DIVERGES from its overlay siblings
# (values-loadproof.yaml, values-e2e.yaml): those layer ON TOP OF
# values-local.yaml to configure a live local cluster for a gate. This
# file layers directly on the chart's own values.yaml defaults instead,
# because its whole point is to be the ONLY delta from the shipped
# defaults ...
#
# Exact invocation (matches the `helm` job in .github/workflows/ci.yml):
#
#   helm template octoconv deploy/chart/octoconv \
#     -f deploy/chart/octoconv/values-capacity-floor.yaml \
#     --set keda.enabled=true --set prometheus.enabled=true
```
If Claude's Discretion lands on a dedicated overlay rather than a bare `--set api.replicas=2`, this
is the register to copy: state the exact invocation this gate uses (layered ON TOP of
`values-local.yaml`, per `values-loadproof.yaml`'s convention — NOT the capacity-floor file's own
"layer on bare defaults" choice, since this gate needs the full local dev secrets/config stack),
and state why `2` (SC1 literally names "2 or more") is the chosen value. Also add the
`helm get deployment api -n octoconv -o jsonpath='{.spec.replicas}'` assertion (Pitfall 6) as the
gate's own preflight confirmation that the value actually took effect, mirroring
`scripts/keda-gate.sh:247-248`'s `WEBHOOK_REPLICAS_START` assertion pattern.

`deploy/chart/octoconv/values-loadproof.yaml` (lines 1-11) is the sibling layering-convention
analog if the "layer on values-local.yaml" path is chosen instead of capacity-floor's
"layer on bare defaults" path:
```yaml
# Load-proof overlay — layered ON TOP of values-local.yaml, NEVER standalone
# (same convention as values-e2e.yaml):
#
#   helm install octoconv deploy/chart/octoconv \
#     -f deploy/chart/octoconv/values-local.yaml \
#     -f deploy/chart/octoconv/values-loadproof.yaml -n octoconv
```

---

### `internal/jobs/admission.go` (doc-comment addition, no logic change)

**Analog:** `internal/ratelimit/ratelimit.go:1-13` — the caveat being mirrored (as a deliberate
counter-statement), not repeated:
```go
// Package ratelimit provides in-process HTTP rate-limiting middleware for the
// /v1 API surface: ...
//
// Both are in-process (github.com/go-chi/httprate's default in-memory
// counter), which matches the current single-API-instance docker-compose
// deployment. If the API is later horizontally scaled behind a load
// balancer, this package's state stops being shared across replicas; the
// upgrade path at that point is httprate's WithLimitCounter option backed by
// a Redis-based counter (e.g. github.com/go-redis/redis_rate), not a
// rewrite of the ByIP/PerClient call sites.
```
`internal/jobs/admission.go`'s existing file-level doc comment (lines 1-23) already documents the
advisory-lock design:
```go
// Package jobs (this file): the QUOT/FAIR admission gate (QUOT-01, QUOT-02,
// QUOT-04, FAIR-01, Phase 52).
//
// The authoritative admission check runs INSIDE Repo.Create's existing
// transaction, as the first statement before INSERT INTO jobs (repo.go) --
// never as a separate pre-check transaction. A bare count query committed in
// its own transaction, followed by an insert in a second transaction, lets
// two API replicas both observe "below cap" and both admit (ROADMAP SC2a,
// D-07). Serializing the count and the insert under one advisory-locked
// transaction is what makes the cap hold under N replicas.
```
Append a sentence in this same register, e.g.: "Unlike `internal/ratelimit` (see that package's
own doc comment), this mechanism carries **no** `api.replicas: 1`-only caveat — the advisory lock
plus same-transaction `COUNT` design above is correct under any number of API replicas by
construction, and Phase 55's live gate (`scripts/quota-multireplica-liveproof.sh`) is the evidence."
Do not restate httprate's upgrade-path sentence — this package needs no upgrade path, that IS the
point being recorded.

---

### `internal/api/handlers.go` (`quotaRejection`/`secondsUntilUTCMidnight`, folded todo 1)

**Analog:** the same file's existing pattern (self-analog — this is a bug fix to an established
mechanism, not a new one).

**Current (buggy) code** (`internal/api/handlers.go:1018-1029`):
```go
func quotaRejection(err error) (msg string, retryAfterSeconds int, ok bool) {
	switch {
	case errors.Is(err, jobs.ErrConcurrencyLimit):
		return "too many concurrent jobs for this conversion type", quotaConcurrencyRetryAfterSeconds, true
	case errors.Is(err, jobs.ErrDailyBytesLimit):
		return "daily byte quota exceeded for this conversion type", secondsUntilUTCMidnight(time.Now()), true
	case errors.Is(err, jobs.ErrDailyJobsLimit):
		return "daily job quota exceeded for this conversion type", secondsUntilUTCMidnight(time.Now()), true
	default:
		return "", 0, false
	}
}
```
Bug: `time.Now()` here is the API PROCESS's own wall clock at RESPONSE-WRITE time, not the
Postgres transaction's `now()` at ADMISSION-EVALUATION time (`internal/jobs/usage_query.go:189`
documents the sibling correct pattern: "Resolve the current day from the SAME clock evaluate()
reads (WR-03): Postgres's own now(), via s.usageRepo.UTCToday, never this API process's own wall
clock"). Fix per D-07/todo 1: propagate the evaluation timestamp through the sentinel-error
contract change (`jobs.ErrDailyBytesLimit`/`ErrDailyJobsLimit` need to become, or be wrapped by, an
error that carries the `now` Postgres actually evaluated against — e.g. a typed error with an
`EvaluatedAt time.Time` field, unwrapped via `errors.As` the same way `http.MaxBytesError` is
unwrapped elsewhere in this file). Existing three-sentinel doc comment to extend
(`internal/jobs/admission.go:45-55`):
```go
// Quota sentinels returned by PreAdmit and, via Repo.Create, by admitLocked.
// Three distinct sentinels -- never one generic ErrQuotaExceeded -- are what
// let the handler compute a per-limit-type Retry-After via errors.Is (D-11):
// a daily rejection returns the seconds remaining until UTC midnight, a
// concurrency rejection returns a short constant, because a slot frees
// unpredictably.
var (
	ErrConcurrencyLimit = errors.New("concurrency limit exceeded")
	ErrDailyBytesLimit  = errors.New("daily byte limit exceeded")
	ErrDailyJobsLimit   = errors.New("daily job-count limit exceeded")
)
```
`secondsUntilUTCMidnight` itself (`internal/api/handlers.go:977-995`) is already correct and
reusable as-is — only its CALLER's `now` argument is wrong; do not rewrite the function, change
the argument at both of its two call sites (`quotaRejection` at line 1023 and 1025).

---

### `internal/api/system_usage_handlers.go` (folded todos 2/3)

**Analog for the defensive-guard reasoning to mirror:** `internal/jobs/usage_query.go:102-128`
```go
const usageInFlightJobsSQL = `
	SELECT client_id, engine, count(*)
	FROM jobs
	WHERE status IN ('queued', 'active')
		AND client_id IS NOT NULL -- jobs.client_id is nullable with ON DELETE SET NULL (0001_init.sql:43); a deleted client's still-queued/active jobs would otherwise collapse into one NULL group that scans into uuid.Nil without error, fabricating a phantom client row carrying enforcement_mode: "" — a value docs/openapi.yaml declares as enum: [observe, enforce] and required. client_usage_daily.client_id is NOT NULL + ON DELETE CASCADE, so the terminal term (usageTerminalSQL) needs no equivalent guard.
		AND ($1::uuid IS NULL OR client_id = $1)
	GROUP BY client_id, engine`
```
This guard defends the SAME output shape (`enforcement_mode` must never be `""`) against a
DIFFERENT failure mode (a NULL-client_id job row) than todo 3's bug (a client-lookup MISS between
two unsynchronized reads). Mirror the REASONING register — name exactly which enum the OpenAPI
spec declares, name exactly why the gap can occur, comment the fix inline — not the SQL literal.

**Todo 3 bug location — the two unsynchronized reads** (`internal/api/system_usage_handlers.go:202,
208`):
```go
usageRows, err := s.usageRepo.UsageSummary(ctx, clientID, dayFrom, dayTo)
if err != nil {
	writeError(w, http.StatusInternalServerError, "failed to load usage")
	return
}

limitRows, err := s.limitsRepo.ListAllLimits(ctx, clientID)
if err != nil {
	writeError(w, http.StatusInternalServerError, "failed to load limits")
	return
}
```
These are two SEPARATE, unsynchronized queries (no shared transaction/snapshot). The merge that
consumes them (line 276): `cm := meta[k.clientID]` — if a client_id present in `usageRows` or
`limitRows`' key-derived `keySet` is ABSENT from `meta` (built only from `limitRows` at line 220-225,
so this can happen if `ListAllLimits` and `UsageSummary` observe different client sets between the
two reads), `cm` is the zero-value `clientMeta{}`, whose `enforcementMode` field is `""` — exactly
the enum violation WR-02 flags. Existing defensive comment register to extend right at that lookup
(`internal/api/system_usage_handlers.go:270-275`):
```go
// A usage row's client should always have a metadata entry (every
// client_usage_daily row's client_id has a FK to clients, ON DELETE
// CASCADE) — this lookup is defensive only. If it is somehow absent,
// still emit the row with empty name/enforcement_mode rather than
// dropping it, because dropping would hide real usage from the
// operator.
```
This comment currently JUSTIFIES the gap it should instead be CLOSING per WR-02 — the fix (per
`53-REVIEW-GAPS.md` WR-02) is to default `enforcementMode` to a valid enum member (`"observe"`,
the schema's own documented default per `internal/jobs/admission.go:35-39`'s
`enforcementModeObserve` constant) rather than leaving it as the Go zero value `""`, and to record
in the comment WHY `"observe"` is the safe default (matches the column's own DB DEFAULT).

**Todo 2 target — response-level flag to make per-row** (`internal/api/system_usage_handlers.go:
352-365`):
```go
resp := systemUsageResponse{
	...
	RemainingMatchesAdmissionGate: period == periodToday && !s.quotaEnforcementDisabled,
	...
}
switch {
case period != periodToday:
	...
case s.quotaEnforcementDisabled:
	resp.RemainingSemantics = "computed_but_not_enforced"
	resp.RemainingNote = "period=today: the arithmetic is exactly what the admission gate (evaluate()) computes, but QUOTA_ENFORCEMENT_DISABLED is set service-wide right now — no request is being rejected. ..."
default:
	resp.RemainingSemantics = "live_admission_gate"
	resp.RemainingNote = "period=today: remaining is exactly what the admission gate (evaluate()) computes right now. A row whose own enforcement_mode is \"observe\" has its verdict computed and counted here too, but that row is never actually enforced — a negative remaining on such a row does not mean requests are being rejected for it."
}
```
The existing `resp.RemainingNote` in the `default` branch ALREADY DESCRIBES the exact gap WR-01
flags in prose ("a row whose own enforcement_mode is observe... is never actually enforced") — the
fix is to promote that per-row fact from a global prose caveat into an actual per-row boolean field
(`Enforced bool` on `systemUsageRow`, computed as
`period == periodToday && !s.quotaEnforcementDisabled && row.EnforcementMode == enforcementModeEnforce`)
next to the existing `EnforcementMode` field (`internal/api/system_usage_handlers.go:282`):
```go
row := systemUsageRow{
	ClientID:        k.clientID,
	ClientName:      cm.name,
	Engine:          k.engine,
	EnforcementMode: cm.enforcementMode,
	...
}
```

---

### `docs/openapi.yaml` (schema updates for todos 2/3)

**Analog:** the same file's existing `system_usage` response schema.

**`enforcement_mode` enum property to keep intact/enforced** (`docs/openapi.yaml:1060-1070`):
```yaml
enforcement_mode:
  type: string
  enum: [observe, enforce]
  description: >-
    This client's configured enforcement mode for this engine. "observe" (the
    default for every provisioned client) means the admission gate computes
    and counts a verdict for this row but never rejects on it — a negative
    remaining on an observe-mode row does not mean requests are being
    rejected.
```
**Required-fields array to extend for the new `enforced` field** (`docs/openapi.yaml:1108`):
```yaml
required: [client_id, client_name, engine, enforcement_mode, used_terminal, used_in_flight, limits, remaining]
```
Add an `enforced` sibling property (boolean, required) next to `enforcement_mode` in the same
`items.properties` block (lines 1047-1107), with a description referencing the SAME semantics
already spelled out in the handler's `RemainingNote` prose (quote it, do not diverge from it) —
this is exactly the kind of schema/code semantic pairing this file already does throughout (see
the `enforcement_mode` description above, which is verbatim consistent with the handler's own
`RemainingNote` wording).

## Shared Patterns

### Live-gate header/trap/teardown/OrbStack-discipline
**Source:** `scripts/keda-gate.sh:1-150`
**Apply to:** `scripts/quota-multireplica-liveproof.sh` (structural skeleton)

### "print every observed value so the transcript is self-documenting evidence"
**Source:** every `assert_*` helper across `scripts/keda-gate.sh`, `scripts/cancel-liveproof.sh`,
`scripts/av-progress-liveproof.sh`, `scripts/sse-liveproof.sh`
**Apply to:** every assertion in the new gate script — never a bare `exit 1` without printing the
observed value

### Quota vs. rate-limit 429 discrimination by body `code`, never bare status
**Source:** `internal/api/handlers.go:1009` (`quota_exceeded`), `internal/ratelimit/ratelimit.go:117`
(`rate_limited`)
**Apply to:** the gate script's result-classification loop

### Sentinel-error → Retry-After mapping via `errors.Is`
**Source:** `internal/api/handlers.go:1018-1029` (`quotaRejection`)
**Apply to:** the folded todo 1 fix — extend this exact function, do not introduce a parallel
mechanism

### Defensive NULL/zero-value guards near an enum contract
**Source:** `internal/jobs/usage_query.go:106,126` (`client_id IS NOT NULL` + its comment
register); `internal/api/system_usage_handlers.go:270-275` (existing but currently
gap-justifying comment)
**Apply to:** the folded todo 3 fix

## No Analog Found

None — every file in scope has at least a role-match analog in the existing codebase or research
document. The one genuinely novel construct (the `mkfifo` FIFO-barrier burst generator) has no
prior-art analog in this repo (confirmed via grep, zero `mkfifo` hits in `scripts/` before this
phase, per RESEARCH.md's "ASSUMED-NEW" tag) — it must be written from RESEARCH.md's Code Examples
section rather than copied from an existing script.

## Metadata

**Analog search scope:** `scripts/*.sh` (all 18 existing live-gate/measurement scripts),
`deploy/chart/octoconv/values-*.yaml`, `internal/ratelimit/`, `internal/jobs/{admission,jobs,
usage_query}.go`, `internal/api/{handlers,system_usage_handlers,routes}.go`, `docs/openapi.yaml`,
`cmd/manage-clients/main.go`
**Files scanned:** ~30 (read in full or targeted ranges)
**Pattern extraction date:** 2026-08-06
