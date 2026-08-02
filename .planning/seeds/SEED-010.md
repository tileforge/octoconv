---
id: SEED-010
status: dormant
planted: 2026-07-26
planted_during: post-v1.9 (design discussion after milestone close)
trigger_when: when adding a new worker/engine should require ZERO changes to core app code (only new isolated worker code + deploy config), OR when the backlog holds 3+ new engine classes to add
scope: Large
---

# SEED-010: Dynamic capability registry (self-registering workers, no API redeploy)

Turn the current compile-time engine wiring into a **data-driven named-capability registry**: a worker built for one named task (e.g. `tex2pdf` = LaTeX→PDF) **self-registers** into a `capabilities` table on startup; the API reads that catalog to validate + route jobs. Adding a new converter becomes "build the worker + deploy it" — no API rebuild/redeploy.

## PRIMARY GOAL (north-star, locked 2026-07-26)

**A developer adds a new worker WITHOUT changing any core/shared app code.** The actor is a **developer at deploy time** — NOT the client. The client only *uses* a capability by name (`operation=tex2pdf`); it never defines or registers one (registration is a trusted dev/deploy path, which is what removes the main security concern).

Precise success criterion (additive, not invasive): "don't change code" specifically means **the API / dispatcher code stays untouched** — adding a worker produces **zero diffs to the API/dispatcher**: no edits to `internal/convert/converters.go` (`init()`), `internal/queue/queue.go` (engine→queue constants), the `switch engine` branches in `internal/api/handlers.go`, or the `engine` CHECK enum migration. A brand-new worker IS expected and fine — that's new, isolated code, never a diff to the dispatcher. The ONLY new artifacts are: (1) the worker's OWN isolated code (its `cmd/<x>-worker`, Dockerfile, convert logic), (2) its self-registration on startup, and (3) deploy config (compose/helm/KEDA scaler).

## Why This Matters

OctoConv is already dynamic at the *process/queue* level (each engine = its own container consuming its own asynq queue; KEDA scale-from-zero already gives launch-on-demand). What is NOT dynamic is the **API's compile-time knowledge** of engines: the registry is populated by `init()` in `converters.go`, engine→queue is constants in `queue.go`, the `engine` value is a hardcoded CHECK enum in `0001_init.sql`, and per-engine limits/branches are `switch engine` in handlers. So every new engine needs an API rebuild + migration. With 4+ engines already queued in the backlog (OCR SEED-009, CAD SEED-005, archive SEED-006, preview SEED-007, plus `tex2pdf`), a registry that decouples the API from compile-time converter knowledge starts to pay off.

## Confirmed design (converged in discussion 2026-07-26)

**Model: named-capability (option A).** The client names the task (`operation=tex2pdf`), not a `(source→target)` pair. One worker may expose several named capabilities; maps onto the existing `operation` column concept.

**`capabilities` table (Postgres):**
```
capability      text pk      -- "tex2pdf" (what the client asks for)
queue           text         -- asynq queue to enqueue to (e.g. "latex")
source_formats  text[]       -- {tex, latex}
target_formats  text[]       -- {pdf}
opts_schema     jsonb        -- optional: validated opts contract
last_heartbeat  timestamptz  -- liveness
status          text         -- active | draining
```
Worker on startup: `INSERT … ON CONFLICT (capability) DO UPDATE` (self-advertise) + a heartbeat loop.

**Flow (SAME as today's workers — the API never launches anything):**
1. Client `POST /v1/jobs` with `operation=tex2pdf` + file.
2. API looks up `tex2pdf` in the catalog → gets the queue, validates (is a worker live? does the file match `source_formats`?).
3. API enqueues to that queue (payload = `job_id`, unchanged).
4. Worker picks it up — running, or **KEDA scale-from-zero** brings it up on queue depth — shells out via the hardened `runCommand`, uploads to S3, marks done.

**LOCKED: the API routes, it does NOT launch workers.** "Launch on demand" is already provided by KEDA scale-from-zero (worker sleeps at 0 replicas → job lands on its queue → KEDA spins it up → processes → scales back to 0). The API must NOT spawn containers itself (that would couple it to the orchestrator + a large security surface). This mirrors exactly how the current image/document/html/audio/av workers behave.

## The 5 seams to change

1. `capabilities` table + worker self-upsert + heartbeat on startup.
2. Job creation keyed by capability **name** (`tex2pdf`), not engine-inferred-from-pair.
3. engine→queue routing from the catalog, not the `queue.go` constants.
4. `engine`/operation CHECK enum in the DB → drop or validate against the catalog (else a new capability can't be inserted).
5. Liveness/staleness — **reuse the reconciler pattern**: heartbeat stale → stop accepting that capability, return fail-closed 422/503.

## Gotchas / decisions before scoping

- **Registration trust boundary.** Anyone with DB/queue access can claim a capability — workers are internal/trusted (same creds), acceptable, but registration must be a trusted path, NEVER client-facing.
- **"No live worker for X"** — decide fail-closed: reject on submit (422/503), don't silently queue forever.
- **Staleness** — reuse the reconciler ticker/sweep for heartbeat expiry.
- **Per-engine container isolation is load-bearing, not optional.** `tex2pdf` is the motivating *and* cautionary example: LaTeX is a security minefield (`\write18`/shell-escape → command execution, `\input` → file reads, infinite-loop/memory bombs). It MUST run `-no-shell-escape`, inside the hardened `runCommand` (Setpgid+SIGKILL+timeout), resource-limited, no network. You cannot safely run untrusted LaTeX in a shared process — which is exactly why the "one container per engine" model must stay. Go `plugin`/`.so` loading is explicitly rejected (brittle toolchain coupling, Linux-only, no unload, and it destroys per-engine resource isolation + KEDA scaling).
- **Opts contract** — either the worker advertises an `opts_schema` in the catalog (central fail-fast validation at the API edge, matches the project's fail-closed + injection-safety philosophy) or the API passes opts through opaquely and the worker validates. Prefer central validation.

## Breadcrumbs

- `internal/convert/convert.go` (Registry/Converter), `internal/convert/converters.go` (`init()` compile-time registration to replace), `internal/api/formats_handlers.go` (`convert.Default.Classes()` — already registry-driven, becomes catalog-driven).
- `internal/queue/queue.go` (engine→queue constants — the routing to make data-driven).
- `internal/db/migrations/0001_init.sql:48` (`engine IN (...)` CHECK enum to loosen).
- `internal/reconciler/*` (heartbeat/staleness sweep pattern to reuse for worker liveness).
- Related engine-class seeds this would generalize: SEED-005 CAD, SEED-006 archive, SEED-007 preview, SEED-009 OCR.

## Notes

Motivating example throughout: `tex2pdf` (LaTeX→PDF). This is a platform/extensibility milestone (generalizes the 5 hardcoded engines into a catalog), not a small fix — weigh against the YAGNI baseline ("one registration + a migration + a compose/helm entry" is cheap at ~1 engine/milestone). Pays off once several engines are actually queued.
