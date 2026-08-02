---
id: SEED-018
status: dormant
planted: 2026-08-02
planted_during: post-v1.11 (asked while v1.12 "Consumer Accounting & Fairness" was being planned)
trigger_when: when a consumer needs two or more conversions chained server-side on one upload — typically to avoid round-tripping a large intermediate through the client — AND the engine classes those steps need already exist
scope: Large (architectural — touches routing, retry classification, job status, the reconciler and progress simultaneously)
---

# SEED-018: Job pipelines (chained engine invocations in one job)

One upload, several conversions applied in sequence, one result. The example
that prompted this: transcode a video to another format, then archive the
output.

## First, a caveat about that example

**Zipping an already-compressed video saves almost nothing.** mp4/webm are
already compressed; a zip wrapper around one of them buys percent-fractions. If
the goal is size reduction, this mechanism will not deliver it.

So before building the literal case, establish which need is real:

- **"One call instead of two"** — the value is avoiding a round trip of a large
  intermediate through the client. For video this is a genuine, measurable win
  (`MAX_UPLOAD_BYTES` is 2 GiB for av) and is the strongest argument for
  server-side chaining.
- **"One artifact containing several files"** — that is *packaging a set*, not
  archiving one file, and it composes with SEED-017 (split a PDF into pages, then
  zip the pages) where the zip actually earns its place.

Naming which one is wanted changes the design.

## Why This Matters

Today a job is exactly one engine invocation, and seven engine classes rest on
that. Chaining is the first requirement that does not fit, which makes it an
architectural decision rather than a feature.

## What it breaks

- **`EngineFor(source, target)`** routes one format pair to one converter. A chain
  `mp4 → webm → zip` has no single pair, and pair-based routing is the core
  primitive.
- **Queue topology.** Each class has its own queue and worker binary, so a chained
  job must hop between queues mid-flight.
- **Retry classification.** `isXTerminal` answers "retryable?" for one invocation.
  When step 2 of 3 fails: retry the step (step 1's output is still in S3, so this
  is both possible and desirable) or fail the job? Requires per-step state.
- **Job status.** `queued → active → done|failed` is a single column with guarded
  transitions. A chain needs an inner state machine, or the job sits `active` for
  the whole sequence with no visibility.
- **Reconciler.** Recovers by `jobs.engine` — one column. A chained job has N
  engines and must resume at the right step.
- **Progress (v1.9).** Reports one engine's 0–100. A chain needs stage-aware
  aggregation.
- **Intermediate artifacts.** Step N's output is step N+1's input: it needs a
  lifecycle, and only the final artifact should be exposed as the download.

## Another piece of schema foresight

Like `ordinal` in SEED-017: the `presets` table's `operation` column has carried
`CHECK (operation IN ('convert', 'extract', 'archive', 'inspect', 'render'))`
since `0001_init.sql:22-23`. The **`archive` operation was anticipated in the
original schema** — but no archive engine exists (that is SEED-006, dormant).

So the literal video→zip case needs **two** things: pipelines *and* an archive
engine class.

## Three designs, and the recommendation

### 1. Client-side chaining (status quo)

Two jobs; the client downloads the intermediate and re-uploads it. Costs a full
round trip of the intermediate — which for video is exactly the pain being
complained about.

### 2. Named pipelines, curated like presets — **recommended**

The operator defines a finite set of chains (`video-archive: av:transcode →
archive:zip`); the client selects one **by name**, not by describing a graph.

This fits the grain the project already has. `presets` are exactly this: curated,
versioned, operator-owned, with a closed `operation` allowlist — the same
discipline as the converter pair allowlist. A pipeline preset is a small step
from an opts preset, and it bounds the problem to a reviewable, testable set of
chains instead of arbitrary composition.

### 3. Arbitrary client-supplied DAG

Maximum flexibility and maximum surface: chain validation, cycle detection, cost
estimation, per-step opts, and a security review of arbitrary composition. This
is a workflow engine. Not worth writing for two scenarios.

## Implementation shape for option 2

The important property: **no separate orchestrator process is needed.**

- Steps live in a table keyed by `(job_id, ordinal)` — the same idiom already used
  for `job_inputs`/`job_outputs`.
- A worker that finishes step N enqueues step N+1 onto the appropriate queue
  itself; workers already hold a queue client. Hand-off, not central dispatch.
- The outer `jobs.status` stays the envelope; per-step status is an inner state
  machine using the existing guarded-transition discipline.
- The reconciler resumes at the first non-terminal step rather than by
  `jobs.engine`.
- Progress aggregates as (completed steps + current step's fraction) / total.
- Intermediates live under the job's own S3 prefix; only the final output is
  exposed as `download_url`.

Riskiest parts: per-step retry semantics, and making the reconciler step-aware
without weakening its existing recovery guarantees.

## Interaction with v1.12 (design question, not a code constraint today)

A pipeline job consumes capacity in **several** engine classes over its lifetime.
v1.12's per-client concurrency cap (FAIR-01) counts per engine class — a chained
job would count against each class it passes through, and quota accounting
(ACCT-02) would see one job spanning multiple engines.

This does not require changing v1.12 code now, but the quota design should be
able to answer "what does a multi-engine job consume" rather than discover the
question later.

## Sequencing

1. **v1.12 first** — otherwise accounting and quotas get rewritten once chained
   jobs exist.
2. **An archive engine class** (SEED-006) as an ordinary eighth engine on the
   proven 4-phase pattern — chaining is useless without a second step to chain to.
3. **Pipelines as their own milestone** — they touch routing, retry
   classification, job status, the reconciler and progress at the same time, which
   is milestone-shaped work, not a phase bolted onto something else.

## Relation to other seeds

- **SEED-017** (PDF split/merge) named heterogeneous merge (docx + jpg + pdf → one
  PDF) as needing exactly this mechanism, and deferred it here rather than
  smuggling it into a merge feature.
- **SEED-006** (archive engine class) is a hard prerequisite for the motivating
  example.
- **SEED-016** (client-assigned priority) composes: a chained job's priority would
  need to apply to every step, or an urgent job loses its priority at the first
  hand-off.

## Breadcrumbs

- Pair-based routing: `internal/convert/convert.go` (`EngineFor`, the `Registry`).
- Per-engine queue/worker split: `internal/queue/queue.go`, `cmd/*-worker/`.
- Guarded transitions: `internal/jobs/repo.go` (`Repo.transition`).
- Engine-keyed recovery: `internal/reconciler/reconciler.go`.
- The anticipated `archive` operation: `internal/db/migrations/0001_init.sql:22-23`.
