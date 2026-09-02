---
id: SEED-020
status: implemented
planted: 2026-08-03
planted_during: post-Phase-51 (reconciling the Notion architecture review against the code)
trigger_when: opens v1.13, immediately after v1.12 ships — this is scheduled work, not a speculative idea
scope: Large
---

> **Harvested in v1.13 Phase 56** (JobRunner consolidation & structured error codes).
> Status corrected 2026-09-02: it had stayed `dormant` through the milestone that
> implemented it.

# SEED-020: Consolidate the seven engine handlers behind a common JobRunner

`internal/worker/worker.go` is 1989 lines. Seven conversion handlers —
`HandleImageConvert`, `HandleDocumentConvert`, `HandleHTMLConvert`,
`HandleAudioConvert`, `HandleAVConvert`, `HandleOCRConvert`,
`HandlePandocConvert` — run 94 to 163 lines each and share one skeleton:

    MarkActive -> download input -> run converter -> upload output
                -> AddOutput -> MarkDone / MarkFailed

They differ only in the engine invoked and its options. `HandleWebhookDeliver`
(427 lines) is a genuinely different shape and is **not** in scope.

## Why This Matters

The cost is measured, not aesthetic.

Phase 51 had to touch **14 symmetric call sites** to add a single Prometheus
counter. The PR #4 review then found exactly the defect that structure
produces: the per-client failure-path counter was incremented
**unconditionally**, before checking that `MarkFailed` had actually committed —
in seven places at once, because it had been copied seven times. One reviewer
finding, seven identical fixes.

Every future cross-cutting concern pays that same tariff.

## The load-bearing distinction (and a correction)

**Not every cross-cutting concern costs ×7. Only *post-commit* ones do.**

This project already has a single home for *transactional* concerns:
`Repo.transition`'s `apply` closure, which Phase 51 established by putting
`recordUsage` there rather than in each handler. Anything that must be atomic
with the status change goes in one place, today, with no refactor.

The ×7 tax falls on concerns that **cannot** live inside the transaction — a
fire-and-forget Prometheus counter being the canonical example, since emitting
it inside a transaction that may still roll back would be wrong.

**A claim was asserted and then withdrawn on inspection: "JobRunner must come
before quotas, or quota logic gets copied seven times."** That does not hold and
should not be re-derived. Phase 52's admission check is API-side, across three
job-creation paths (`POST /v1/jobs`, `cmd/mcp-server`, `cmd/mcp-http`), not
seven handlers; and its only worker-side piece — releasing a concurrency slot on
every terminal state — is transactional, so it belongs in `transition` where
`recordUsage` already lives. Quotas do not pay the tax.

The argument for doing this work is therefore **generic**, not quota-specific:
every post-commit concern from here on costs seven edits and carries a
seven-fold chance of the kind of asymmetry the PR #4 review caught. That was
judged sufficient to open a milestone with, and not sufficient to preempt one.

## In scope: structured error codes

Decided 2026-08-03, after the Notion review argued (correctly) that structured
error codes should be lifted out of its P2 bucket.

They belong **in this work, not before it**. JobRunner is where retry
classification and terminal error handling get centralised, which is their
natural home; doing them as a separate earlier phase would edit the same seven
sites twice. Today the API returns fixed strings and consumers — the frontend
and both MCP servers — have to match on text.

The same consolidation is the right place to close the `invalid_options`
metrics gap: those terminal failures currently appear in neither
`octoconv_client_job_outcomes_total` nor `octoconv_job_outcomes_total` (the
Postgres rollup does record them, so accounting is intact). That gap is
symmetric and pre-existing rather than a Phase 51 regression, and closing it by
adding two more counters at six copied sites would be the exact anti-pattern
this seed exists to end.

## Why the refactor is safe

The hard part is already done. The transactional seam — `Repo.transition` with
its `apply func(ctx, tx) error` closure, row lock, allow-list and `job_events`
append — exists and is proven. This work moves only the *orchestration* around
it: nothing about the state machine, its guarantees, or its tests needs to
change.

## Nuances to respect when it is built

- **Do not fold `HandleWebhookDeliver` in.** It is not a conversion, its retry
  and dead-letter semantics differ, and forcing it into a shared runner would
  distort the abstraction for the seven cases that fit.
- **Per-engine differences that must survive:** each class has its own timeout,
  input-size cap, options schema and, in some cases, disk guards (`av`) and page
  caps (`ocr`). The runner must take these as data, not bury them in branches —
  which is the point of contact with SEED-010's catalog.
- **Post-commit hooks are the whole reason for the refactor**, so make emitting
  a terminal outcome exactly one call in exactly one place, and make it
  structurally impossible to fire before the transition commits.
- **Keep the pinning tests.** `TestJobDurationLabelsPinned` and
  `TestQueueDepthLabelsPinned` guard the KEDA contract; a runner that
  accidentally changes a metric's label set would break scale-from-zero
  platform-wide.

## Relation to other work

- **SEED-010** (dynamic capability registry / engine catalog) is the
  complementary half: the catalog makes what an engine *is* into data, this
  makes what an engine *does* into one code path.
- **SEED-017 / SEED-018** (PDF split, pipelines) both multiply the number of
  jobs flowing through the handler path; doing them against seven copies would
  compound the problem.

## Breadcrumbs

- `internal/worker/worker.go` — the seven handlers and their 14 metric call
  sites.
- `internal/jobs/repo.go` — `Repo.transition` and its `apply` closure; the seam
  the runner orchestrates around.
- `internal/metrics/metrics.go` — `RecordJobOutcome` /
  `RecordClientJobOutcome`, and the two label-pinning tests.
