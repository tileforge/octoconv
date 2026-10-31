# HTTP API

[Back to OctoConv](../README.md) · [Documentation](README.md)

Every `/v1/*` route requires `Authorization: ApiKey <raw-key>`. `GET /healthz` is public and
really pings Postgres, Redis and S3.

| Route | Purpose |
|---|---|
| `POST /v1/jobs` | Create a conversion job from a multipart upload |
| `GET /v1/jobs` | List the caller's jobs, paginated |
| `GET /v1/jobs/{id}` | Job status, and a presigned download URL once `done` |
| `GET /v1/jobs/{id}/events` | Server-sent progress stream for one job |
| `POST /v1/jobs/{id}/cancel` | Cancel a `queued` or `active` job |
| `POST /v1/inspect` | Inspect a file synchronously — no job, no storage write |
| `GET /v1/formats` | Supported operations and format pairs, from the live registry |
| `/v1/presets` | Named conversion presets: create, list, show, update, deactivate |
| `/v1/system/presets`, `/v1/system/usage` | Operator-only; non-operators get `404`, never `403` |

**Status semantics**

- Request line and headers together above 64 KiB, or more than 500 header values → `431` with a plain-text body, answered
  before the key or the rate limit is checked; no `code`
- No key, or an invalid/revoked one → `401`
- A refusal sent before the request body is read (`401`, `429` and the other early answers)
  comes with `Connection: close`: the server reads no more of the body, so send the retry on a
  new connection
- Any other answer sent before the request body is read, `2xx` included (for example
  `GET /healthz` with a body), waits at most 500 ms for the rest of a body the route does not
  read: a body that has not arrived by then is dropped and the connection closes after the
  answer; one that arrives in time keeps the connection
- A JSON request body (`POST` and `PUT` on `/v1/presets` and `/v1/system/presets`) has to
  arrive in full within 1250 ms of the moment the server starts reading it: a body still
  incomplete then is answered with `400` and `code: "invalid_request"`, and the connection
  closes after the answer
- A job belonging to another client, or one that doesn't exist → `404`; the API never
  confirms the existence of jobs you don't own
- Over the rate limit → `429` with `Retry-After` and `code: "rate_limited"`
- Over quota → `429` with `Retry-After` and `code: "quota_exceeded"` — a consumption limit,
  never an authorization boundary
- Every create intake slot of the API process taken → `429` with `Retry-After` and
  `code: "intake_busy"` — the service's own capacity, not your budget; retry the request
  unchanged
- Unsupported pair, mismatched content, oversized declared dimensions, embedded macros or a
  zip-bomb shape → `422`, rejected before anything touches storage
- A ZIP-based upload whose central directory is past the bound `POST /v1/inspect` reads →
  `422` with `code: "invalid_request"` and the message "the archive's central directory is
  larger than this service reads; repackage the archive with fewer entries", rejected before
  anything touches storage

**Correlating a job with your own request.** Send your identifier as a
`Correlation-Id` header on `POST /v1/jobs`. It comes back in the job status and in the
webhook, and is written beside the `job_id` in every lifecycle log record, so a conversion
can be followed from your side without a database session. At most 128 bytes of printable
ASCII without spaces; anything else is `422` rather than truncated, because a value silently
cut in half no longer matches what you searched for. Absent is not an error, and nothing is
generated to replace it.

> [!NOTE]
> It is not a trace id and carries no tracing semantics. If OpenTelemetry is added later, it
> links to this rather than replacing it.

The HTTP contract is [`docs/openapi.yaml`](openapi.yaml). Two things in it are held by
tests rather than by care: its operation set is compared both ways against the live router,
and every status a route can produce has a declared response — the handler's read from its
source, the middleware's observed by driving the real router with requests that trip each
rejection. The second is coverage, not equality: the spec may declare a response nothing
produces. Response bodies and examples are maintained by hand,
and the spec says which is which rather than promising an exactness nothing checks. Every
machine-readable error code is frozen in [`docs/ERROR-CODES.md`](ERROR-CODES.md).

<details>
<summary>A LaTeX job can be a project, not just a file</summary>

Repeat the `file` part. The first one is the document that gets typeset; the rest are
materialised beside it under their own names, so `\input{preamble}`, `\bibliography{refs}`
and `\includegraphics{fig.png}` resolve:

```bash
curl -H "Authorization: ApiKey <raw-key>" -F target=pdf -F source=latex \
  -F file=@paper.tex -F file=@preamble.tex -F file=@refs.bib -F file=@fig.png \
  http://localhost:8090/v1/jobs
```

Up to 32 parts, and the size limit applies to their **sum**, not to each — a thousand
one-byte attachments would otherwise be a thousand uploads charged as one small job. An
attachment named after a file the worker owns (`in.tex`, `out.pdf`) is refused rather than
silently renamed, and every name is flattened to its base, so `../../etc/passwd` cannot name
anything outside the job.

</details>
