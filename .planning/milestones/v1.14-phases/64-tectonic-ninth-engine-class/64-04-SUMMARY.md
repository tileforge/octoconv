---
phase: 64-tectonic-ninth-engine-class
plan: 04
status: complete
completed: 2026-09-04
requirements: [TEX-05]
---

# 64-04 — the loop closed, and three defects that only a mechanism could find

64-02 had already landed this plan's Tasks 1–4 in Go (migration, queue, descriptor,
`cmd/tectonic-worker`) because registering the converter forced them. What was left
was the part 64-02 could not see from inside the converter: whether anything else
in the system had actually kept the promise that registry entry made.

Three things had not, and none of them was found by reading.

## The API 500'd on every real `.tex`

`handleCreateJob`'s enqueue switch had no tectonic case. `GET /formats` advertised
`latex → pdf`, the registry served it, `EngineFor` returned `tectonic`, and the
switch fell to its fail-closed default: **500 `failed to enqueue job`**, on every
upload. Proven before it was fixed (evidence file), which is the only reason the
before/after is on record rather than asserted.

The one-line fix is not the finding. The switch lived inside a handler, so an
engine could only be seen by a test that had a **multipart fixture** for it — and
four of the nine have none. Extracted to `Server.enqueueForEngine` and walked by
`convert.AllEngines()` in `TestEveryEngineHasAnEnqueueRoute`, which needs no
fixture and fails by name. Its negative half pins that an unknown engine is still
refused, so it cannot pass against a switch that stopped failing closed.

## PR #12's defect is gone; a quieter one took its place

The plan names this as "the API publishes `estimated_max_seconds: 0`". That can no
longer happen: 63-02 made `NewServer` **merge** `defaultEngineTimeouts` into a
partial map, so a missing key publishes 300s, not 0.

What survives is the divergence one layer down — the API publishes its own default
while the worker runs on the operator's `TECTONIC_ENGINE_TIMEOUT` — and it is
**invisible at the api package layer**, because the merge makes the map look
complete there. `cmd/api`'s inline literal was extracted to `engineTimeoutsFromEnv`
and tested where the divergence is actually visible: set the env var, assert the
published number moves.

## IN-02 was broken for eight engines, and had been since Phase 32

`queue.TestComposeReplicatesEveryEngineVarToEveryQueueClientService` derives the
var list from `NewClient`'s own source and **infers** which compose services
construct a client (any service declaring one engine var must declare all of
them). On its first run it reported the ninth engine's gap — and both
webhook-workers missing `ENGINE_TIMEOUT`, `IMAGE_MAX_RETRY` and the
`DOCUMENT_*`/`HTML_*` pairs.

Phase 32 recorded that as "pre-existing and out of scope" in a compose comment.
It stayed invisible for eight phases because it is **harmless by coincidence**:
compose sets exactly the Go defaults those processes fell back to. The first
operator to tune any of the six would have split the fleet's unique-lock TTLs —
in the service that runs the **reconciler**, whose entire job is re-enqueuing work
the fleet already has. Fixed, and now mechanical.

## The eight ScaledObjects were diffed, not asserted

197 added lines, **0 removed**, and each of the eight compared identical as a
parsed YAML document. A first pass compared `--show-only` text and reported probe
as CHANGED by one blank line — a document separator, because probe used to be
last. Re-done at document level: a text diff cannot tell a manifest change from a
concatenation artifact, and reporting either as the other is wrong in a different
direction. `scripts/ci-helm-checks.sh` moved 8 → 9 and runs green.

## End to end, on live infrastructure

A document with formulas, a **forward** cross-reference and a biblatex
bibliography: queued → active → done in 6s, 28438-byte PDF, and `pdftotext` shows
every reference resolved and both citations rendered. D-07's stated cost is not
paid on this document — the `backend=bibtex` warning appears and describes a
reduced feature set the document does not use.

The failure half was run too: an undefined control sequence fails **terminal on
attempt 1** with `engine_error` / "the document could not be typeset".

**Finding for TEX-01:** a multi-file LaTeX project cannot be submitted. The API is
single-input, so `paper.tex` + `refs.bib` is not expressible; the bibliography
above works only because the `.bib` is embedded with `filecontents*`. The schema
already carries an `ordinal` on `job_inputs` and no code path populates more than
one.

**Finding for 64-05:** the engine printed `warning: TeX rerun seems needed, but
stopping at 6 passes` on a correct document. Cost is bounded by six passes, not by
an unbounded rerun — a corpus that never approaches the cap measures a floor
rather than a bound.

## Two smaller corrections

- `cmd/tectonic-worker`'s `TECTONIC_ENGINE_TIMEOUT` comment was probe's, credited a
  measurement run to a value 64-05 has not measured, and named `ProbeUniqueTTL`.
  `internal/queue/client.go` states the invariant it broke: the placeholder is
  "marked as such everywhere it appears". It appeared twice and was honest once.
- The `.env.example` provenance check read "measur" over the whole paragraph, which
  would have called tectonic's own PLACEHOLDER disclaimer a measurement. The
  disclaimer is now read from the var's **own line**; block scope was tried and was
  wrong, because av's and pandoc's paragraphs both discuss a historical placeholder
  that a measurement replaced.

## Not changed, deliberately

`docs/openapi.yaml` is in this plan's `files_modified` and needed no edit: the
engines surface is a dynamic map keyed by engine class, not an enum. 64-03 adds
the `unsupported_package` code to it.
