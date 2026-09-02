# OctoConv Error Codes

**Status: FROZEN as of milestone v1.13 Phase 56 (SEED-020 / ERRC-01).**

This document is the single canonical, versioned list of every machine-readable error code
OctoConv can emit — both halves. It exists so the frontend and both MCP servers have a fixed
migration target instead of a moving one (see PITFALLS.md Pitfall 11).

**The freeze takes effect at the commit that lands this document, on top of plans 56-03
(sync sweep) and 56-04 (async consolidation), in phase
`56-jobrunner-consolidation-structured-error-codes`.** Later phases (57 onward, including
`inspect`/`sanitize` in Phases 58/60) may ADD new codes to either table below. **No phase may
rename or repurpose an existing code** while consumers are migrating onto this list (D-04, D-05).

Go constants are the source of truth (D-06). This document is generated/maintained from them,
and `internal/errcode/doc_sync_test.go` mechanically proves the two never drift apart — see
"Keeping this document honest" at the bottom.

## Envelope shape

**Synchronous errors** (any `4xx`/`429` HTTP response from `/v1/*`) use one flat, non-nested
shape:

```json
{"error": "<human-readable message>", "code": "<machine-readable code>"}
```

Never nested (never `{"error": {"code": ..., "message": ...}}`) — `frontend/src/api.ts:54`
reads `body.error` as a plain string today, and this phase does not get to break that (D-01).
Future extensibility (`retry_after`, `details`, etc.) is added as additional TOP-LEVEL fields
on the same flat envelope, never by changing this shape. `429` responses already carry
`retry_after` this way; that precedent is what any future addition should follow.

**Asynchronous job failures** do not use an HTTP error envelope at all — the job itself
completes its HTTP request successfully at creation time, and the failure surfaces later as two
plain string fields:

- `GET /v1/jobs/{id}` response body: `error_code` and `error_message`, present only when
  `status` is `failed` (`internal/api/handlers.go`, `handleGetJob`).
- The terminal webhook delivery body, once, when a job's guarded transition ends in `failed`
  (`internal/worker` `HandleWebhookDeliver`).

Both surfaces carry the identical code string a client would see from the async table below —
there is no separate "webhook version" of the vocabulary.

## Consumer contract (Pitfall 12 — read this before writing an error-handling switch)

**Consumers MUST treat an unrecognized code as a generic fallback. Consumers MUST NOT throw,
crash, or fail closed on an unrecognized code.** This is a requirement, not a suggestion — a
future phase (`inspect` in 58, `sanitize` in 60) WILL add new codes to the tables below, and
those additions are contractually non-breaking only if every consumer already tolerates an
unknown value.

Recommended consumer shape: an open string type with a curated set of known values plus an
explicit `unknown` fallback branch — for example a TypeScript `type ErrorCode = "invalid_request"
| "unsupported_conversion" | ... | (string & {})`, or a Go `string`-typed value checked with a
`switch` that has a `default:` case treating anything unrecognized the same as `"unknown"`.

**Do NOT generate a closed TypeScript union or a closed Go enum from the current list.** A
closed type looks more "correct" at code-review time, but it turns every future additive code
into a compile-time or runtime breaking change for that consumer the moment this list grows —
exactly the trap Pitfall 12 documents.

## One code space (D-03)

`invalid_options` and `too_many_pages` are not coincidences of naming — each is the SAME code,
deliberately, in both tables below. A client sees the identical string `invalid_options` whether:

- it caught the failure synchronously as a `422` when creating the job (bad/malformed `opts` in
  the request body), or
- it caught the failure asynchronously in `error_code` after the job ran and the persisted
  `opts` failed re-validation inside the worker.

This is by design: from the client's point of view, "your options were rejected" is one failure
class regardless of when the service discovered it. Do not "disambiguate" this by renaming
either side — that would split one code space into two for no client-visible benefit.

`too_many_pages` works the same way, added by Phase 63 for the `sanitize` operation: a client
sees the identical string whether the API refused the upload at admission (a `422`, because the
declared page count was legible from a bounded scan of the bytes) or a worker refused it in
`error_code` (because the accurate count was only affordable there). The two gates enforce ONE
bound, `PROBE_MAX_PAGES`; they differ only in how they obtain the count, and the API's cheaper
scanner is allowed to abstain, which is why the second gate exists at all.

## Synchronous error codes (HTTP 4xx / 429)

Seventeen codes, one per class of rejection (D-02). Granularity is by failure CLASS, not by
`writeError` call site — the specific detail of what went wrong always lives in `message`,
never encoded as a finer-grained code. Source: `internal/errcode/errcode.go`.

| Code | Typical HTTP status | Failure class | What a client should do |
|------|---------------------|----------------|--------------------------|
| `invalid_request` | 400 | Generic request/form validation failure: missing target format, preset+target conflict, unrecognized source format, invalid `callback_url`, malformed job id, invalid `status`/`engine` filter, invalid `client_id`, invalid period filter, invalid JSON body, invalid preset name | Fix the request; the `message` names the specific field/reason |
| `unsupported_conversion` | 422 | The requested `(source, target)` format pair is not served by any registered engine | Do not retry unmodified; check `GET /v1/formats` for a supported pair |
| `unsupported_operation` | 422 | The requested `operation` is not registered by any engine at all — no `(source, target)` triple exists under it (client typo, or the engine is not compiled into this build) | Check the `operation` value against `GET /v1/formats`; do not retry unmodified |
| `invalid_options` | 422 | `opts` present but malformed/invalid for the chosen conversion (shared code space with the async `invalid_options` below — D-03) | Fix `opts` against the conversion's documented schema; do not retry unmodified |
| `opts_not_applicable` | 422 | `opts` are well-formed JSON but not meaningful for this specific conversion | Omit `opts` or use only fields the target conversion documents |
| `missing_file` | 400 | No file part present in the multipart request body | Attach the file; do not retry unmodified |
| `unknown_preset` | 404 | The named preset does not exist, is not active for this client, or (deliberately, to avoid leaking auth-vs-existence to a non-operator) an operator-only lookup target does not exist | Verify the preset name via `GET /v1/presets`; do not treat this as evidence a client-visible preset was deleted |
| `payload_too_large` | 413 (multipart body) or 422 (an oversized single field, e.g. `opts`) | The request body or a specific field exceeds its configured size cap | Reduce payload size; status code varies by which cap fired, code does not |
| `not_found` | 404 | The requested job or preset does not exist for this authenticated client | Verify the id/name; a 404 here never distinguishes "exists but not yours" from "does not exist" |
| `invalid_cursor` | 400 | An opaque pagination cursor on `GET /v1/jobs` failed to decode | Drop the cursor and restart pagination from the first page |
| `already_exists` | 409 | A preset name collision on create | Choose a different name, or update the existing preset instead |
| `unauthorized` | 401 | Missing/malformed `Authorization` header, or the supplied API key is invalid | Supply a valid `Authorization: ApiKey <key>` header |
| `quota_exceeded` | 429 | v1.12 admission-gate quota rejection — this exact string is already on the wire and frozen (ERRC-V2-01 predates this phase) | Respect the accompanying `Retry-After` header; this is distinct from `rate_limited` and never a client bug |
| `rate_limited` | 429 | `internal/ratelimit` per-client or pre-auth IP rate limit — this exact string is already on the wire and frozen | Respect the accompanying `Retry-After` header; back off and retry later |
| `inspect_busy` | 429 | The service has exhausted its bounded concurrency budget for synchronous `inspect` parsing right now and refuses to accept another one immediately — the request is neither queued nor parsed (58-CONTEXT.md D-06, mechanism 2); distinct from `rate_limited` (own request rate) and `quota_exceeded` (own daily/concurrent job budget) — this is the service's own capacity, not the client's | Respect the accompanying `Retry-After` header; retry later unchanged — do not reduce file size or alter the request, this is not a property of the request |
| `too_many_pages` | 422 | The uploaded PDF declares more pages than the `sanitize` operation's configured ceiling (`PROBE_MAX_PAGES`), refused at admission before any storage write. Same code string as the async `too_many_pages` below — one code space (D-03) | Split the document, or ask the operator to raise `PROBE_MAX_PAGES`; do not retry unmodified |
| `internal_error` | 5xx | Anything the client cannot act on (unexpected server-side failure) | Retry later or escalate; do not attempt to parse `message` for a remediation |

## Asynchronous job-failure codes (`error_code`)

Fourteen codes, frozen unchanged during this phase's consolidation (D-04). Source:
`internal/worker/descriptors.go` (`AllFailureCodes()`), with per-engine mapping logic in
`internal/worker/worker.go` (`avFailureCode`, `ocrFailureCode`, `pandocFailureCode`,
`audioFailureCode`) and the sole non-`internal/worker` emitter in
`internal/reconciler/reconciler.go`.

| Code | Emitted by | Meaning |
|------|-----------|---------|
| `engine_error` | All seven engine classes (image, document, HTML, audio, AV, OCR, pandoc) | Deliberate catch-all: unsupported or corrupted input format, or any terminal engine failure with no more specific mapping. Used identically by all seven engines and frozen against renaming (D-05) — do not add a finer-grained replacement for one engine class without adding the equivalent for all seven |
| `invalid_options` | All six opts-guarded engines (document, HTML, audio, AV, OCR, pandoc — not image, which has no opts re-parse guard) | Persisted `opts` failed re-validation inside the worker. Same code string as the sync `invalid_options` above — one code space (D-03) |
| `duration_exceeded` | audio, AV | Declared audio/video duration exceeds the configured maximum (`AudioDescriptor`/`AVDescriptor` share this sentinel deliberately, keeping the two engines' client-facing contracts symmetric) |
| `timecode_out_of_range` | AV | An explicit thumbnail timecode requested via `opts` exceeds the source's actual duration — a client fault, deliberately not clamped |
| `resolution_exceeded` | AV | Declared video resolution exceeds the configured maximum |
| `no_video_stream` | AV | No video stream found in an input misrouted to the AV engine (e.g. an audio-only ISOBMFF container) |
| `too_many_pages` | OCR, probe | PDF page count exceeds the configured maximum (`OCR_MAX_PAGES` for OCR, `PROBE_MAX_PAGES` for sanitize — two engines, two bounds, one code, because the client-facing condition is the same one). Also returned synchronously at admission by the sanitize path — one code space (D-03) |
| `page_too_large` | OCR | A PDF page would rasterize beyond the configured pixel bound |
| `image_too_large` | OCR | Image dimensions exceed the configured maximum |
| `unsupported_language` | OCR | The requested OCR language is not in the supported set |
| `unsupported_target` | OCR, pandoc | The requested target format/conversion is not supported by that engine |
| `input_too_large` | pandoc | Input exceeds the configured `PANDOC_MAX_INPUT_BYTES` cap, rejected before the subprocess runs |
| `reconciler_exhausted` | `internal/reconciler` (NOT an engine worker) | The reconciler's stale-job recovery exhausted its retry budget for this job; the job is marked `failed` by the reconciler itself rather than by any engine's `process()` |

## Historical job-failure codes (no longer produced)

Codes this build **cannot emit** but which you may still receive.

`jobs.error_code` is a persisted column, and `GET /v1/jobs/{id}` serves whatever was written when
the job failed. Retiring a code therefore removes it from the list of *possible outcomes* without
removing it from the set of values the API *returns* — a job that failed last month still carries
it. Deleting such a code from this document would fix one inaccuracy by creating another.

A consumer that INTERPRETS a code must handle these alongside the active table above. A consumer
that ENUMERATES what a job might fail with should use the active table only.

| Code | Engine | Retired in | Meaning |
|---|---|---|---|
| `born_digital` | OCR | Phase 59 | The source PDF already had an extractable text layer, so the OCR job was refused. Phase 59 replaced that refusal with a route: a `txt` target now returns the extracted text layer, and `hocr`/`alto` proceed to a real OCR render because `pdftotext` produces no word or line bounding boxes. Nothing can produce this code any more; jobs created before Phase 59 still carry it. |

This section is held equal to `worker.HistoricalFailureCodes()` by the same mechanical test that
guards the two active tables, and a second test asserts the active and historical vocabularies are
disjoint — a code cannot be both producible and retired.

## Not in scope for this phase

- **Publishing this list into `docs/openapi.yaml` as an enum with client codegen is
  ERRC-V2-02**, deliberately deferred to a future phase (D-07). This document is the
  human-readable contract until that lands.
- **Migrating the two existing `429` bodies (`quota_exceeded`, `rate_limited`) to a different
  envelope shape is ERRC-V2-01** and is not needed — both already match the flat `{"error",
  "code"}` shape D-01 mandates for every other sync error.

## Keeping this document honest

`internal/errcode/doc_sync_test.go` (`TestErrorCodesDocMatchesGoConstants`) parses the code
column of both tables above and asserts set equality, in both directions, against the union of
`errcode.All()` and `worker.AllFailureCodes()`. If you add, remove, or (never) rename a Go
constant, that test fails naming the exact code that drifted — update this document, not the
test. The correct response to a failure of that test is always to fix whichever side (Go or
this document) is wrong, never to relax the assertion.
