---
id: SEED-019
status: dormant
planted: 2026-08-03
planted_during: post-Phase-51 (asked right after the DELETE-based retention purge landed)
trigger_when: when the DELETE-based purge stops keeping up — measurable as purge sweeps hitting the maxPurgeBatchesPerSweep ceiling every tick, table/index bloat that autovacuum cannot reclaim, or purge duration climbing into the sweep interval
scope: Large (schema migration across five FK relationships on live tables)
audit_acknowledged:
  milestone: v1.15
  at: 2026-09-30
  status: dormant
---

# SEED-019: Partition `jobs`/`job_events` and drop old partitions instead of deleting rows

Phase 51 shipped retention as a batched `DELETE`. At volume, dropping a
partition is dramatically cheaper than deleting rows: no per-row work, no dead
tuples, no autovacuum pressure, no index bloat. This seed records what it would
actually take here, because the answer is not "partition one table".

## Why This Matters

`DELETE` retention is correct and was the right call for a phase whose subject
was accounting — but it pays per row, forever. Partition drop is O(1) in the
number of rows removed. The question is only *when* the crossover arrives.

## The constraint that shapes the whole design

Postgres requires the partition key to be part of **every** unique constraint on
a partitioned table. `jobs.id` is a bare `uuid PRIMARY KEY`
(`0001_init.sql:42`), so partitioning by `created_at` forces the primary key to
become the composite `(id, created_at)`.

That cascades, because **five** foreign keys reference `jobs(id)`, all
`ON DELETE CASCADE`:

| Referencing | Line |
|---|---|
| `jobs.parent_id` (self-reference) | `0001_init.sql:44` |
| `job_inputs.job_id` | `:78` |
| `job_outputs.job_id` | `:91` |
| `job_events.job_id` | `:106` |
| `webhook_deliveries.job_id` | `:116` |

A foreign key may only reference a unique constraint, and on a partitioned table
that constraint must include the partition key. So every one of those five
relationships becomes composite — meaning each child table needs the parent's
`created_at` duplicated into it, populated on insert and kept consistent. The
self-reference on `parent_id` needs the same treatment.

**And that still does not buy `DROP PARTITION` on its own.** Dropping a `jobs`
partition does not cascade to children; the foreign keys simply refuse the drop.
To drop wholesale, all four child tables must be partitioned on the same key and
their partitions dropped in lockstep with the parent's.

Finally: a live table cannot be converted in place. There is no
`ALTER TABLE ... PARTITION BY`. The migration is create-new → backfill → swap,
or attach the existing table as a first partition — either way a real data
migration, not an additive DDL step like every migration this project has
written since `0001_init.sql`.

## The cheap path, if growth is the real problem

**`job_events` is partitionable on its own, and almost certainly the fastest
growing table** — it is an append-only transition log with several rows per job,
and **nothing references it** (verified: zero `REFERENCES job_events` in the
schema).

No dependents means no composite-key cascade: partition it by its own timestamp,
drop old partitions freely, done. One table, no FK redesign, no touching `jobs`.

If the motivation is "retention is getting expensive", start here and measure
again before considering the `jobs` family. It is plausible this alone removes
the pressure.

## Why this is dormant rather than scheduled

The question was asked as *"if the jobs table is that big"* — and we do not know
that it is. Phase 51 has only just bounded `jobs`/`job_events` to a 90-day window
(`JOBS_RETENTION=2160h`); before that they grew without limit, so any intuition
about their size predates the fix. Partitioning now would be optimising on
speculation, which is the pattern this project has twice chosen to avoid in
favour of measurement.

Phase 53 (operator usage visibility) is what produces the volume data to decide
on.

## Concrete trigger signals

Prefer these over intuition:

- The purge hits `maxPurgeBatchesPerSweep` (currently 10 batches ×
  `RETENTION_BATCH_SIZE`, default 1000) on **every** tick — it is no longer
  draining the backlog, only nibbling at it.
- `octoconv_reconciler_actions_total{action="purged_jobs"}` keeps climbing while
  table size does not fall.
- Autovacuum cannot keep up: dead-tuple ratio stays high, index bloat grows.
- Purge duration approaches `RECONCILER_SWEEP_INTERVAL`.

## Nuances to respect when it is built

- **Point lookups by id alone would fan out across partitions.** `GET /v1/jobs/{id}`
  queries by primary key today; with range partitioning on `created_at` and no
  date in the predicate, Postgres must check every partition. Either the API
  carries a date hint or accepts the fan-out — decide deliberately, do not
  discover it in production latency.
- **Partition maintenance is itself a job.** Something must create next month's
  partition before rows need it. The reconciler tick that already hosts the purge
  is the obvious home, mirroring Phase 51's D-11 reasoning about not adding a
  deploy unit.
- **Keep the DELETE path.** Partition drop handles whole windows; per-row
  deletion is still needed for anything that is not aligned to the partition
  boundary.
- `client_usage_daily` does **not** need this. It grows as
  `clients × engines × days` — bounded and small by construction (Phase 51 D-01).

## Relation to other work

- **Phase 51 D-11** already recorded partitioning as "genuinely cheaper at large
  volume and the right answer later", deferring it because migrating live tables
  carried risk disproportionate to a phase about accounting. This seed is that
  deferral, written up.
- **SEED-017** (PDF split) would multiply `job_outputs` rows per job, which
  shifts the growth profile — worth re-checking the crossover if it ships.
- **SEED-018** (pipelines) would add per-step rows, same caveat.

## Breadcrumbs

- Retention as it stands: `internal/jobs/retention.go` (`PurgeJobs`/`PurgeUsage`),
  `internal/reconciler/reconciler.go` (`purge`, `maxPurgeBatchesPerSweep = 10`).
- Knobs: `JOBS_RETENTION`, `USAGE_RETENTION`, `RETENTION_BATCH_SIZE` — set
  identically in `.env.example`, `docker-compose.yml`, the chart ConfigMap and
  `cmd/webhook-worker/main.go:105`.
- Schema and the five FKs: `internal/db/migrations/0001_init.sql:41-120`.
