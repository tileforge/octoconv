# Worker Protocol

[Back to OctoConv](../README.md) · [Documentation](README.md)

This specification pull request is the Codex review gate before any runtime phase.

Project version v2.0 and Worker Protocol v1 are different numbers. Client `/v1` stays the client API. Workers dial out and pull. They hold no Postgres, Redis, or shared S3 credentials. They import no Go module. This surface is not MCP and not client `/v1`.

The machine-readable contract is [worker-protocol/v1.schema.json](worker-protocol/v1.schema.json) (JSON Schema draft 2020-12). Message examples are under [worker-protocol/examples/](worker-protocol/examples/). The conformance catalog is [worker-protocol/conformance.md](worker-protocol/conformance.md).

The key words MUST, MUST NOT, REQUIRED, SHALL, SHALL NOT, SHOULD, SHOULD NOT, RECOMMENDED, MAY, and OPTIONAL in this document are to be interpreted as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).

An external author implements this protocol from this document, the schema, and the examples. There is no shared Go module, no generated stub, and no protobuf contract.

## Decisions

These eight choices are closed. Later text adds normative rules and MUST NOT change the choice.

**Decision 1.** The transport is HTTPS JSON, HTTP/1.1 or HTTP/2 over TLS, on the prefix `/worker/v1/`. The worker opens every connection. `control` is present on every core success response. Faktory is a behavior reference, not a dependency. Rejected: gRPC, the Faktory wire (TCP 7419, RESP, server-first HI, and pwdhash), and WebSocket or any worker listen socket.

**Decision 2.** TLS 1.3 with a client certificate opens the connection, a Bearer session token revokes it, and an `Octoconv-Attempt` token fences the attempt. Rejected: bearer only, mTLS only, and Faktory pwdhash.

**Decision 3.** A capability is an identifier plus a positive integer version. The operator installs the manifest out of band. HELLO does not grant execution. Rejected: self-registration that grants execution, and a new compile-time switch per worker.

**Decision 4.** The installation enum is `direct` or `gateway`. `direct` is the honest presign profile. It does not promise clawback of bytes already downloaded. Rejected: a revoke promise on presigned URLs, and handing the worker shared S3 credentials.

**Decision 5.** HELLO advertises bounds. The worker uses that advertisement. Unmeasured numbers block release until Phase 86. Rejected: treating an unmeasured number as cleared for release.

**Decision 6.** A result is an artifact object. Dedup is SHA-256 of the exact raw body. Worker percent stops at 99. Rejected: RFC 8785 canonical JSON.

**Decision 7.** The image Go worker migration target is `cmd/worker` (libvips), capability `octoconv.convert.image`. The Python operation is `octoconv.convert.imagepdf`. The repository `tileforge/octoconv-worker-imagepdf` does not exist and is not created here. Rejected as the first migrated class: document, HTML, Pandoc, CAD, Tectonic, probe, audio, video, OCR, and webhook delivery.

**Decision 8.** Each job has one immutable dispatch owner, `legacy` or `protocol`, chosen once at admission. One owner per job. `parent_id` is not `step_id`. Rejected: two owners for one job, and using `parent_id` as the step identifier.

## Transport

The worker protocol uses a dedicated listener. It MUST NOT be mounted on the client API mux, MUST NOT share an origin with the client API, and MUST NOT offer a plaintext switch. Workers open every connection. The core does not connect to the worker. Workers MUST NOT listen for core connections.

HTTP/1.1 over TLS and HTTP/2 over TLS are the allowed HTTP versions. Every request body and every success body carries `protocol_version` with the integer value `1`. There is no minor version. A missing required field is `invalid_message`. Any other integer for `protocol_version` is `incompatible_protocol`. An additive protocol field outside `extensions` is a version bump.

Objects are closed: `additionalProperties` false. The only open protocol object is OPTIONAL `extensions`. Keys inside `extensions` are ignored. Any other unknown field is `unknown_field`. Phase 87 MUST fit in `job_id`, `step_id`, `attempt_id`, the file arrays, and `extensions`. The core assigns an attempt only to a session whose manifest declared that capability id and version.

Every success response carries `control`:

| Field | Type | Meaning |
|---|---|---|
| `cancel_attempt_ids` | array of attempt ids | Attempts the worker stops |
| `drain` | bool | Stop pulling, finish in-flight work, then DELETE the session |
| `terminate` | bool | Stop local work and send `stopped`, not `fail` |

`drain` means stop pulling, finish in-flight work, then `DELETE` the session. `terminate` means stop local work and send `stopped`, not `fail`. `draining` is `control.drain`, not an error code.

A lost pull response is retried as the same `pull_request_id` and the same raw body. While that attempt is still active, the core returns the stored assignment and does not extend the lease. If the attempt is no longer active, the replay is `attempt_not_active` and issues no new attempt. A lost completion is retried as the same completion body and returns the stored success even after the lease has ended. Workers follow no redirects. Responses include `Cache-Control: no-store`.

After HELLO, a session call sends `Authorization: Bearer` and the session token. A call under `/worker/v1/attempts/` also sends `Octoconv-Attempt` for that attempt. The pull and complete examples contain no session token. The HELLO success fixture's `session_token` is a placeholder, not a secret. Direct file requests send no `Authorization` and no attempt token. The `href` itself is the bearer until `expires_at`.

This protocol does not promise exactly-once worker computation. Only an accepted core effect is exactly once. A worker MAY run the conversion again. A repeated pull or completion with the same raw body is the same effect, not a second one. Dedup is SHA-256 of that exact raw body. Different whitespace is a different body. RFC 8785 is not used.

## TLS

TLS 1.3 is the minimum. Early data is disabled. Renegotiation is not used. There is no configuration flag that turns TLS off. Refusing TLS 1.2, including a middlebox that only offers 1.2, fails closed (assumption A1). Mixing the worker origin with the client API origin is forbidden.

The required cipher is `TLS_AES_128_GCM_SHA256`. `TLS_AES_256_GCM_SHA384` and `TLS_CHACHA20_POLY1305_SHA256` MAY be offered. Certificate signatures and groups stay inside the [RFC 8446](https://www.rfc-editor.org/rfc/rfc8446) section 9.1 mandatory set: `rsa_pkcs1_sha256`, `rsa_pss_rsae_sha256`, `ecdsa_secp256r1_sha256`, and key exchange `secp256r1`. `X25519` MAY be offered. Implementations MUST NOT hand-roll a cipher list outside that set.

HTTP/1.1 is required. HTTP/2 over TLS is allowed. `h2c` is forbidden. The leaf certificate DNS SAN MUST match the configured origin using the platform verifier. Trust anchors are an operator private CA bundle, not public WebPKI (assumption A5).

## Authentication

The only profile is a TLS 1.3 client certificate, a revocable session token, and a separate attempt token. Bearer-only authentication is rejected. A session token presented without the client certificate gets no access. mTLS without a session token is rejected, because a certificate checked only at handshake cannot be withdrawn for the rest of that connection. There is no plaintext switch.

The client-certificate SAN is a URI `urn:octoconv:worker:` followed by the worker id, not SPIFFE (assumption A7). The URI MUST match the approved worker identity. A handshake with an unknown identity gets no session.

Later calls send `Authorization: Bearer` and the session token. The token is 32 bytes from a CSPRNG, encoded on the wire as 64 lowercase hex characters, stored only as a hash, and is not logged. The session row is the revoke switch. The fixture `session_token` is 64 `a` characters. That value is a placeholder, not a secret.

Attempt-scoped calls also send header `Octoconv-Attempt`. The assignment object carries that secret as `attempt_token`. It is 32 bytes from a CSPRNG, encoded on the wire as 64 lowercase hex characters, stored only as a hash, and is not logged. The core mints it with the assignment and does not send it in a response header. It authorizes only lease renew, progress, fail, complete, and stopped for that `attempt_id`. Those five are the attempt-scoped methods in the method table. Renew does not mint a new attempt token. Session-only calls reject an attempt token and change nothing. An attempt token for attempt A presented on attempt B is `capability_denied` and changes nothing on B. The fixture value is 64 `c` characters. That value is a placeholder, not a secret.

hrefs exist only on the assignment. Renew does not mint or extend them. A new link requires a new assignment message. This profile has no route, request body, response body, or schema branch that mints or extends an `href`.

Identity revoke denylists the certificate serial and closes every session for that identity. The next call returns `session_closed`. Rotation is an overlapping new certificate, then denylist the old serial. Session tokens are not rotated in place. A restarted process creates a new session and does not release old leases or bypass owner limits. Session close is durable. Dedup rows MAY be deleted after close, and a request after close is still `session_closed`.

Credentials are not forwarded to another origin. `direct` file requests carry no `Authorization` and no attempt token. `gateway` uses the attempt token only against the gateway origin named in the assignment. Workers do not follow redirects, because a caller-supplied `Authorization` is not reliably stripped. Any 3xx is a worker-local outcome named `origin_rejected`; the body is not sent to the new URI. `origin_rejected` is not a core error-table row.

### HELLO

`POST /worker/v1/sessions` opens one session. The request fields are `protocol_version`, `worker_id` (UUID), `manifest_sha256` (64 lowercase hex digits), and `free_slots` (integer, minimum 0). The published request is [worker-protocol/examples/positive/hello-request.json](worker-protocol/examples/positive/hello-request.json).

HELLO success fields are `session_id`, `session_token`, `protocol_version`, `control`, and every worker-facing advertisement below. Workers use the advertisement. The published success body is [worker-protocol/examples/positive/hello-response.json](worker-protocol/examples/positive/hello-response.json).

| Field | Starting advertisement |
|---|---|
| `pull_wait_seconds` | 20 |
| `heartbeat_interval_seconds` | 10 |
| `lease_seconds` | 30 |
| `session_max_lifetime_seconds` | 86400 |
| `max_message_bytes` | 262144 |
| `file_link_max_ttl_seconds` | 886 |
| `clock_skew_margin_seconds` | 5 |
| `pull_rate_per_second` | 2 |
| `progress_rate_per_second` | 4 |
| `file_access` | `direct` or `gateway` |

`manifest_sha256` is the SHA-256 of the exact installed manifest file bytes. The Manifest section states how the core compares it. HELLO does not grant execution.

A body the schema rejects as an unknown field is answered with the closed error object. [worker-protocol/examples/positive/error-unknown-field.json](worker-protocol/examples/positive/error-unknown-field.json) is a valid error body: `code` `unknown_field`, a short `message`, and `retryable` false. It is not the client shape that uses an `error` field.

## Methods

Every method uses a JSON body, including success. There is no `204`. An empty pull is HTTP 200 with `assignments` empty, and that response is stored for dedup. This document fully specifies HELLO, the pull assignment, and the complete request. The State machine section states the transitions for pull, lease, progress, cancel, fail, complete, stopped, heartbeat, and session delete, and does not rename these paths. `href` values exist on the assignment. This protocol has no method that mints a new `href`.

| Method | Purpose |
|---|---|
| `POST /worker/v1/sessions` | HELLO. Open one session. |
| `POST /worker/v1/sessions/{id}/heartbeat` | Report process liveness. The success body carries `control`. |
| `POST /worker/v1/sessions/{id}/pull` | Ask for at most one assignment. The worker mints `pull_request_id`. |
| `POST /worker/v1/attempts/{id}/lease` | Renew the attempt lease. |
| `POST /worker/v1/attempts/{id}/progress` | Report worker percent, an integer 0–99, or unknown. |
| `POST /worker/v1/attempts/{id}/fail` | Report an engine failure. The core decides retry. |
| `POST /worker/v1/attempts/{id}/complete` | Report success and list the artifacts that were written. |
| `POST /worker/v1/attempts/{id}/stopped` | Report that local work stopped for cancel, terminate, or a lost lease. Not an engine failure. |
| `DELETE /worker/v1/sessions/{id}` | Close the session after the worker finishes draining. |

### Pull request

`POST /worker/v1/sessions/{id}/pull` requires `protocol_version`, `pull_request_id` (UUID), `free_slots` (integer, minimum 0), and `wait_seconds` (integer). While the stored attempt is still active, the same `pull_request_id` with the same raw body returns that stored assignment and does not extend the lease. If that attempt is no longer active, the same replay is `attempt_not_active` and issues no new attempt. An empty stored pull still returns the empty list. The same id with a different raw body is `pull_conflict` and changes no state.

## Errors

Protocol errors are a third vocabulary. The body is `code`, `message`, and `retryable` (boolean). It is not the client shape that uses an `error` field. These codes MUST NOT be added to `jobs.error_code` and MUST NOT be added to client `/v1`. The six client job statuses are not given new names.

| Code | HTTP | When |
|---|---|---|
| `unauthorized` | 401 | Missing or bad client-certificate binding, or a bad session token |
| `session_closed` | 401 | The session is closed, expired, or the identity is revoked |
| `manifest_mismatch` | 403 | The HELLO digest does not match the operator-installed manifest |
| `capability_denied` | 403 | The session may not run that capability, or an attempt token is presented for a different attempt |
| `incompatible_protocol` | 400 | `protocol_version` is not the integer 1 |
| `invalid_message` | 400 | The message fails the schema, including a missing required field or a bad type |
| `unknown_field` | 400 | A field outside `extensions` is not in the schema |
| `invalid_progress` | 400 | Percent is outside 0–99, including 100. Stored progress is unchanged. The attempt stays active |
| `message_too_large` | 413 | The control body exceeds the advertised cap. File bytes do not count |
| `rate_limited` | 429 | Pull or progress exceeds the advertised rate. No attempt is created. A progress rejection leaves the attempt alive |
| `pull_conflict` | 409 | The same `pull_request_id` was sent with a different raw body |
| `complete_conflict` | 409 | The same attempt completion was sent with a different raw body. No state change |
| `attempt_not_active` | 409 | The attempt is no longer active. This covers lease renew, a result, and a pull replay whose stored attempt is past `attempt_deadline`, canceled, or replaced |
| `artifact_rejected` | 409 | Size or sha256 of the immutable copy does not match the completion list |

Stable engine-failure strings travel inside `fail`. The core maps them onto the existing client allow-list. The migrated image class stays `engine_error`. Those strings are not protocol codes.

Worker percent is an integer from 0 through 99, or JSON null when progress is unknown. A worker MUST NOT send 100. The value 100 is `invalid_progress`. The core sets client-visible progress to 100 only when the job becomes `done`. Unknown progress stays null.

## Assignment

A pull success body contains `protocol_version`, `assignments`, and `control`. `assignments` has at most one element. An empty array is the empty pull. A normal job is one step. The core mints `job_id`, `step_id`, and `attempt_id`. File bytes are not in the JSON.

| Field | Constraint |
|---|---|
| `job_id`, `step_id`, `attempt_id` | UUID strings |
| `attempt_token` | 64 lowercase hex characters, delivered in this object. The tracer fixture is a placeholder, not a secret |
| `capability_id` | Capability identifier. The tracer fixture uses `octoconv.convert.image` |
| `capability_version` | Positive integer. The tracer fixture uses `1` |
| `attempt_deadline`, `lease_expires_at` | RFC3339 UTC timestamps |
| `limits.max_output_bytes` | Integer. The tracer fixture uses `104857600` |
| `inputs`, `outputs` | Arrays of file links |

Each file link has `role`, `ordinal`, `format`, `href`, and `expires_at`. `href` is a URI. The schema does not fix the host. The tracer fixture uses input role `source`, format `jpg`, and output role `primary`, format `png`, with `https://storage.example/presigned-get` and `https://storage.example/presigned-put`. Those hrefs have no query string and no object key. Each link `expires_at` in the fixture is `2026-11-06T12:02:00Z`, equal to `attempt_deadline` and later than `lease_expires_at`. No session token appears in the fixture. The `attempt_token` in that fixture is the placeholder described under Authentication.

On the `direct` profile, the link is a bearer URL until `expires_at`. This slice does not promise to revoke bytes already downloaded and does not promise to abort an in-progress direct GET.

The published assignment is [worker-protocol/examples/positive/pull-assignment.json](worker-protocol/examples/positive/pull-assignment.json).

## Complete

`POST /worker/v1/attempts/{id}/complete` requires `protocol_version` and `artifacts`. The worker does not choose the object key. Each artifact has `role`, `ordinal`, `format`, `byte_size`, `content_type`, and `sha256`. `sha256` is 64 lowercase hex digits, the SHA-256 of the artifact bytes.

The tracer fixture lists one artifact: role `primary`, ordinal `0`, format `png`, `byte_size` `1234`, `content_type` `image/png`, and a `sha256` of 64 `b` characters, plus `protocol_version` `1`. That body is [worker-protocol/examples/positive/complete.json](worker-protocol/examples/positive/complete.json).

The same completion body retries a lost response. A different raw body for the same attempt is `complete_conflict` and changes no state. Accepting that completion is an exactly-once core effect. It is not exactly-once computation.

## Manifest

A capability id matches `^[a-z][a-z0-9]*(\.[a-z][a-z0-9]*)+$`: one lowercase letter, then lowercase letters or digits, then one or more dot-separated segments of the same shape. It is at most 128 characters. Version is a positive integer, not semver. One deployed worker class advertises one capability, not one capability per format pair.

The operator installs the manifest out of band. HELLO sends `worker_id` and `manifest_sha256`. `manifest_sha256` is the SHA-256 of the exact installed manifest file bytes. The core compares `worker_id` and that digest to the approved row. A mismatch is `manifest_mismatch`. HELLO does not insert a capability and does not grant execution. The catalog row exists at zero replicas and survives a core restart. Self-registration does not grant execution.

Together, HELLO and the manifest describe the worker id, the protocol version, the operations and formats, the capability version, the parameter schema, and the input and output counts and roles. A missing required field, or a required field whose type or constraint does not match the schema, is `invalid_message`. An unknown field outside `extensions` is `unknown_field`. `protocol_version` other than the integer 1 is `incompatible_protocol`.

Selection at admission:

- If the job or step already pins a capability version, use that pin.
- Otherwise the approved map from operation, source format, and target format yields exactly one capability version.
- Zero matches stay the existing client codes `unsupported_conversion` or `unsupported_operation`.
- A second approved claim for the same triple is rejected at approval time, so request time sees zero or one. Do not add a client error code for an ambiguous catalog.
- A heartbeat never selects a capability.

`legacy` is present only when `engine` is one of `image`, `document`, `av`, `cad`, `archive`, `probe`, `html`, `audio`, `ocr`, `pandoc`, or `tectonic`. It is a closed object with `operation` and `engine`.

The first capability is `octoconv.convert.image`, version `1`. `legacy` is operation `convert` and engine `image`. The client operation is `convert`. It has one input role `source` with min 1 and max 1, one output role `primary` with min 1 and max 1, and `parameter_schema` `{}`. Its formats are all 20 ordered pairs among `png`, `jpg`, `webp`, `heic`, and `tiff` where `from` differs from `to`. The list is not abbreviated. The fixture is [worker-protocol/examples/positive/manifest-image.json](worker-protocol/examples/positive/manifest-image.json).

The second capability is `octoconv.convert.imagepdf`, version `1`. The client operation is `convert`. It has no `legacy` object, one pair `png` to `pdf`, one `source`, one `primary`, and `parameter_schema` `{}`. The fixture is [worker-protocol/examples/positive/manifest-imagepdf.json](worker-protocol/examples/positive/manifest-imagepdf.json).

Cad publishes four capabilities. A manifest holds one capability, and each operation and format pair has one approved owner. `octoconv.convert.cad`, version `1`, has legacy operation `convert` and engine `cad`. Its pairs are step to stl and iges to stl. `octoconv.render.cad`, version `1`, has legacy operation `render` and engine `cad`. Its pairs are step to png, iges to png, and stl to png. That render capability declares output role `measurements` with min 0 and max 1. `octoconv.convert.caddraw`, version `1`, has legacy operation `convert` and engine `cad`. Its pairs are dxf to svg, dxf to pdf, dwg to svg, dwg to pdf, and dwg to png. `octoconv.render.caddraw`, version `1`, has legacy operation `render` and engine `cad`. Its pairs are dxf to png and dwg to png, and it declares the same optional `measurements` output. All four use `parameter_schema` `{}`. The fixtures are [worker-protocol/examples/positive/manifest-cad-convert.json](worker-protocol/examples/positive/manifest-cad-convert.json), [worker-protocol/examples/positive/manifest-cad-render.json](worker-protocol/examples/positive/manifest-cad-render.json), [worker-protocol/examples/positive/manifest-caddraw-convert.json](worker-protocol/examples/positive/manifest-caddraw-convert.json), and [worker-protocol/examples/positive/manifest-caddraw-render.json](worker-protocol/examples/positive/manifest-caddraw-render.json). Approving the matching convert manifest moves a new cad convert job that has no options and no progress webhook to the external worker when its attempt budget is at most `file_link_max_ttl`. A cad render, a cad job that sets options, a cad job whose engine timeout is longer than that cap, a cad job that opts into progress webhooks, or a cad job with no approved manifest still goes to asynq. Protocol progress copies a CAD stage onto the job when `stage`, `stage_index`, and `stage_count` are present, the stage word is one of the public CAD stages, and the index is greater than the stored index. The assignment `limits.max_output_bytes` for that convert job is the output ceiling of the target: SVG allows `CADMaxDXFSVGBytes`, STL allows `CADMaxSTLBytes`, and PDF stays at 100 MiB. Image assignments stay at 100 MiB. Completion accepts a primary artifact only up to the ceiling advertised for that job.

Wire output roles that later engines may use are `primary`, `report`, `preview`, `thumbnail`, and `measurements`. The image and imagepdf manifests declare only `primary`. Input role `source` is a protocol field. The current `job_inputs` table has no role column. This phase does not add one.

A new capability must not add a compile-time engine case or a per-worker reconciler case. `jobs.engine` cannot store a new capability id until a later phase. Phase 83, not this phase, widens that check once with the single placeholder `external`. `external` is not a queue name. Routing, quotas, usage, list filters, and KEDA do not read `external`; they use `capability_id`. For a legacy-mapped image job the accounting key stays `image`. For `octoconv.convert.imagepdf` the accounting key is the capability id. Do not collapse those onto one sentinel. This phase adds no SQL migration.

The schema branch is the closed `manifest` definition. `capabilities` is a required array of one closed capability: `id`, `version`, optional `legacy`, `client_operation`, `inputs`, `outputs`, `parameter_schema`, and `formats`. `parameter_schema` is a JSON object with `additionalProperties` true because it is an embedded JSON Schema, not a second protocol extension point. Protocol fields beside it remain closed. Each formats item is a closed object with `from` and `to` strings.

## File access

Installation policy is a required enum: `direct` or `gateway`. Unset file access refuses process start. `gateway` selected in a binary that has no gateway refuses process start. There is no silent fallback to presigns.

hrefs exist only on the assignment. Renew does not mint or extend them. A new link requires a new assignment message. No route, request body, response body, or schema branch issues a link apart from that assignment message.

### direct

`direct` is specified now and implemented in a later phase. Each input is an HTTPS presigned GET. Each attempt-scoped output is an HTTPS presigned PUT. The URL is method-locked and key-locked. The worker checks that the URL origin equals the origin in the assignment, that the scheme is HTTPS, and that it sends no `Authorization`. It follows no redirect.

Effective lifetime is the minimum of `issued_at` plus `file_link_max_ttl`, the fixed `attempt_deadline`, and `job_deadline`. It is not capped by `lease_expires_at`. The lease fences attempt-scoped JSON calls. An issued link stays usable until its own `expires_at`, including after a successful renew and after the lease has lapsed. If that instant is not strictly after `server_now` plus `clock_skew_margin`, the core issues no link. Renewing a lease does not rewrite an already issued link. Cancel, revoke, and complete issue no new links.

The URL is a bearer and can be reused until `expires_at`. A download that started before expiry continues. A late PUT can replace the object at that key. `direct` does not abort an in-progress GET and does not revoke bytes already downloaded.

Object immutability is a core copy onto a key the PUT URL cannot write. The core checks size and SHA-256 on that copy, and only then opens the acceptance transaction. A later PUT to the staging key does not change the copy. Checking the staging object and then copying it is not sufficient. Bucket versioning and Object Lock are not the mechanism.

`file_link_max_ttl` is the default CAD attempt budget, advertised as `file_link_max_ttl_seconds` 886. It MUST be finite, at least 1 second, and at most 604800 seconds. Do not use 15 minutes. The client download default of 15 minutes is a different clock and is not `file_link_max_ttl`. A job whose attempt budget is longer than this cap stays on asynq, so a signed link covers the whole attempt.

### gateway

`gateway` is specified now and is not implemented in this phase. Every byte request presents the attempt token to the gateway. The gateway asks the core, allows no cache past that request, and aborts the stream when the attempt is no longer active. If it cannot reach the core, it fails closed. It still does not promise clawback of bytes the worker has already read.

Rejected: presign-only with a revoke promise, and handing the worker S3 credentials.

## Bounds

The server clock is the only clock that orders leases and links. Timestamps are RFC3339 UTC. A deadline is expired when `server_now` is greater than or equal to `expires_at`. Workers use the HELLO advertisement, not a constant compiled from this table. Starting numbers are advertised policy, not measurements (assumption A3). Rows that need a measurement block release until that Phase 86 method passes. Do not invent an RTT in this spec.

The published inequality is `lease_expires_at` less than or equal to `attempt_deadline`, which is less than or equal to `job_deadline`.

| Name | Starting policy | Gate |
|---|---|---|
| `session_max_lifetime` | 24h, then a new HELLO | Policy. |
| Heartbeat interval | 10s | Policy. Three missed intervals (30s) mark the instance offline for display only. That does not close the session and does not expire the lease. |
| Reconnect | Same session token until close, lifetime, or revoke | A process restart is a new session. Old leases stay until their attempt deadline. |
| `pull_wait` | 20s, then HTTP 200 and an empty `assignments` array | Phase 86 may lower it if a proxy cuts the poll. |
| Pull rate | 2 requests per second per session | Excess is `rate_limited` and creates no attempt. One in-flight pull per free slot. A pull assigns at most one attempt. `free_slots` is a hint, not a batch size. |
| Lease | The minimum of 30s and the time until `attempt_deadline` | Unmeasured. Phase 86 gate: on the intended remote network, p99 renew RTT is less than lease divided by 3 over a stated sample of 100 renewals, with zero false expiry; otherwise raise the lease before release. |
| Renew | `lease_expires_at` is the minimum of `server_now` plus the lease and `attempt_deadline` | If `server_now` is already at or past `attempt_deadline`, respond `attempt_not_active` and leave the row unchanged. If the computed expiry is not strictly later than the current one, reject. Renew does not move `attempt_deadline`, does not mint a new attempt token, and does not extend old links. Running renew twice does not move the attempt deadline or rewrite links. |
| Attempt deadline | Fixed at issue: the minimum of `job_deadline` and `issued_at` plus `attempt_budget` | Heartbeat and renew do not move it. |
| Image `attempt_budget` | `ENGINE_TIMEOUT`, default 120s | Not a new number. Policy. |
| Image attempts | 1 initial plus 4 extra, 5 total, from `IMAGE_MAX_RETRY=4` | Policy. |
| Image backoff | Full jitter in the closed range from 0 to the minimum of 15s and 2s times 2 to the power n, where n is the number of failures already recorded | The `octoconv.convert.image` manifest carries that policy for protocol-owned image jobs. The core classifies retryable versus terminal. |
| imagepdf `attempt_budget` | Unset in v1 | Phase 86 measures 30 runs of `internal/e2e/testdata/sample.png` on the reference host. Budget is the ceiling of p99 times 2, floor 10s, never above the job deadline. Release of that capability waits on the measurement. |
| `unavailable_wait` | 15 minutes, operator-set | Not the legacy reconciler staleness threshold. Protocol jobs leave the backlog when the job deadline passes. Phase 86 proves a deadline expiry removes the job from the backlog series. |
| Job deadline, one step | `created_at` plus `unavailable_wait` plus `attempt_budget` times `max_attempts_total` | Do not treat the existing `jobs.expires_at` column as this deadline. |
| Control message | 256 KiB (262144 bytes), else `message_too_large` | File bytes never count. Phase 86 checks the committed fixtures fit, expecting well under 64 KiB. |
| Progress messages | 4 per second per attempt | Excess is `rate_limited` and the attempt stays. |
| `clock_skew_margin` | 5 seconds, advertised as `clock_skew_margin_seconds` | Policy. The core will not issue a link or a lease that would expire inside that margin. |
| `file_link_max_ttl` | 886 seconds, advertised as `file_link_max_ttl_seconds` | Finite, at least 1 second, and at most 604800 seconds. Not 15 minutes. Issued `expires_at` is the minimum of `issued_at` plus this cap, `attempt_deadline`, and `job_deadline`. The lease is not that cap. |

## Progress

Worker percent is an integer 0 through 99, or JSON null. Percent 100 is `invalid_progress`. Stored progress is unchanged and the attempt is not failed, including when that request is repeated. A repeated progress value of 100 still changes nothing. Within one attempt a lower percent is ignored. A new attempt clears client-visible progress to null before any tick. `stage`, `stage_index`, `stage_count`, and `eta_seconds` are optional fields of `progressRequest`. They are not a percent, and omitting them leaves `percent` as the only progress value. `stage` is a string of at least one character. It is not the client CAD stage enum. `stage_index` and `stage_count` are integers of at least 1. `stage_index` counts from 1. When both are present, `stage_index` MUST NOT be greater than `stage_count`; the core answers `invalid_message`. `eta_seconds` is an integer of at least 0, or JSON null when unknown. Null is unknown and is not the same as zero.

Client statuses stay `awaiting_upload`, `queued`, `active`, `done`, `failed`, and `canceled`. Do not add protocol states to that enum.

| Situation | Client status | Progress |
|---|---|---|
| Waiting for a worker | `queued` | unchanged |
| Attempt assigned or running | `active` | null or 0 through 99 |
| Core accepted success of the whole job | `done` | 100 |
| Terminal failure | `failed` | last percent frozen, not forced to 100 |
| Cancel | `canceled` | not redefined by this protocol |

Unknown stays null, never zero. `octoconv.convert.imagepdf` v1 reports null. A chain must not invent a percent from the step index. That policy is Phase 87.

Today's image class can finish `done` with a null percent. After migration, a protocol image job shows 100 only when status becomes `done`, never while `active`.

## State machine

These invariants keep their names. Process heartbeat is separate from attempt lease. The attempt deadline is fixed. Pull and completion are deduplicated. Fencing. Immutable artifacts. Atomic acceptance plus continuation intent. Finite file access. Draining and wakeup.

Workers dial out. The core never opens a connection to the worker. Wakeup means a scaled worker dials out and pulls. The core does not start that process. No message carries Postgres, Redis, or shared S3 credentials. A normal job is one step. The envelope already carries `job_id`, `step_id`, and `attempt_id`, so a later second step does not need a new major version.

| Call | Transition |
|---|---|
| Heartbeat | Proves the process is connected and returns `control`. No new lease. |
| Pull | Assigns at most one attempt, or stores an empty `assignments` array. |
| Lease | Renews under Bounds, or rejects with the row unchanged. |
| Progress | Applies the percent rules. Percent 100 does not move the attempt. |
| Fail | Reports an engine failure. The core decides retry. |
| Complete | Asks the core to accept artifacts. The same raw body is the same effect. |
| Stopped | Reports cancel, terminate, or a lost lease. Not an engine failure. |
| `DELETE` session | Closes the session after the worker finishes draining. |

### Heartbeat

`POST /worker/v1/sessions/{id}/heartbeat` uses `protocolOnlyRequest`: `protocol_version`, and no other field except optional `extensions`. Success is `heartbeatResponse`: `protocol_version` and `control` only. That success MUST NOT include `assignments`, `session_id`, or a top-level `lease_expires_at`. Heartbeat does not include a new `lease_expires_at`. It does not move `attempt_deadline` or an existing file-link `expires_at`. Three missed heartbeats are display-only offline. They do not close the session and do not expire the lease.

### Pull

`POST /worker/v1/sessions/{id}/pull` assigns at most one attempt or returns an empty `assignments` array, and both results are stored. The request is `pullRequest`. The success is `pullAssignmentResponse`. A lost response is retried as the same `pull_request_id` and the same raw body. While the stored attempt is still active, including after the lease has lapsed, the core returns that assignment, does not extend the lease, and does not create a second attempt. If `server_now` is at or past `attempt_deadline`, or the attempt was canceled or replaced, the replay is `attempt_not_active` and issues no new attempt. An empty stored pull still returns the empty list.

### Lease

`POST /worker/v1/attempts/{id}/lease` renews the attempt lease. The request is `protocolOnlyRequest`. Success is `leaseResponse`: `protocol_version`, top-level `lease_expires_at`, top-level `attempt_deadline`, and `control`. Renew follows the Bounds section. It does not move `attempt_deadline`, does not mint a new attempt token, and does not extend old links.

### Progress

`POST /worker/v1/attempts/{id}/progress` uses `progressRequest`. `percent` is an integer from 0 through 99, or JSON null. The schema rejects 100. The core answers `invalid_progress`. Stored progress is unchanged and the attempt stays active, including when that request is repeated. Optional `stage`, `stage_index`, `stage_count`, and `eta_seconds` are defined on this object in the schema. The rest of the percent rules are in Progress.

### Fail

`POST /worker/v1/attempts/{id}/fail` uses `failRequest`. It requires `engine_code` and `message`. `engine_code` is an engine-failure string. It is not a protocol error code. `fail` is an engine failure and the core decides retry.

### Stopped and cancel

`POST /worker/v1/attempts/{id}/stopped` uses `stoppedRequest`. `reason` is `cancel`, `terminate`, or `lease_lost`. `stopped` means the worker obeyed cancel, terminate, or lost the lease, and it is not an engine failure. The core MUST NOT burn the attempt budget for `stopped`.

`control.cancel_attempt_ids` names attempts whose later `complete` is `attempt_not_active`. That `complete` changes no state. The worker reports the cancel with `stopped` and reason `cancel`. `control.terminate` causes `stopped` with reason `terminate`, not `fail`, so a drain does not burn the attempt budget. `control.drain` still means stop pulling, finish in-flight work, then `DELETE` the session. Drain does not turn those attempts into `fail`.

### Session delete

`DELETE /worker/v1/sessions/{id}` is how a worker finishes draining. The request is `protocolOnlyRequest`. Success for progress, fail, stopped, and session delete is `protocol_version` and `control`, the same closed shape as `heartbeatResponse`, with no top-level `lease_expires_at`. After session close, a pull is `session_closed` even when dedup rows were deleted. The session row remains the authority. Dedup rows MAY be deleted after close. Deleting them does not make a closed session accept a new pull.

### Dedup

The dedup digest is the SHA-256 of the exact raw request body after schema validation. The retry MUST be byte-identical. Different whitespace is a conflict. Do not use RFC 8785. A proxy that rewrites JSON is out of profile. The worker TLS session ends at the core origin.

The pull key is the pair `worker_session_id` and `pull_request_id`, plus that digest. Empty pulls are stored. Rows live for the session and MAY be deleted after close. While the stored attempt is still active, the same id and the same raw body return that assignment and do not extend the lease. If that attempt is no longer active, the replay is `attempt_not_active`, issues no new attempt, and does not replace the stored row. An empty stored pull still returns the empty list. Two in-flight pulls with the same id produce one assignment and one stored response. The same id and a different body is `pull_conflict` and changes no state. That is a payload conflict. No second attempt is created.

The completion key is `job_id`, `step_id`, `attempt_id`, and the literal `complete`, plus the digest. The same id and the same raw body return the stored success even after the lease has ended, and accounting is not applied again. The same id and a different body is `complete_conflict` and changes no state. A lost completion response is retried as that same raw body.

A new session does not release the previous session's leases and does not raise the owner cap.

### Fencing

The attempt token for attempt A presented to attempt B is `capability_denied` and B is unchanged. Renew when `server_now` is at or past `attempt_deadline` returns `attempt_not_active` and does not change the row. The inequality is `lease_expires_at` less than or equal to `attempt_deadline` less than or equal to `job_deadline`. Heartbeat and renew do not move `attempt_deadline`.

### Acceptance

Acceptance is specified and not implemented in this phase. The worker PUTs bytes, then `complete` lists the artifacts. The core copies each object onto an immutable result key the PUT URL cannot write. It then checks size and SHA-256 of that copy against the completion list. Only a matching copy is accepted. The core then commits one database transaction that records acceptance, artifacts, accounting, the event, and continuation intent. A mismatch is `artifact_rejected` and does not accept. A PUT that replaces the staging object after the copy does not change the accepted bytes. This is exactly-once accepted effects, not exactly-once computation. A worker MAY run the conversion again.

### Finite file access

Finite file access is the File access section. Every issued link stays inside those clocks. Heartbeat does not move an existing file-link `expires_at`. Renew does not extend old links. Cancel, revoke, and complete issue no new links. Fencing a result does not replace that limit.

### Draining and wakeup

Draining and wakeup are dial-out behaviors. `control.drain` stops new pulls, lets in-flight work finish, and ends with session delete. `control.terminate` stops local work by `stopped`, not by `fail`. Wakeup means infrastructure scales a worker and that worker dials out. The core never opens a connection to the worker. Three missed heartbeats show the instance offline for display only and leave the session and the lease running. A missed heartbeat does not remove a capability from the catalog.

## Migration

The first migrated worker is the existing image worker. The command path is `cmd/worker`. It uses libvips, queue name `image`, task name `image:convert`, and capability `octoconv.convert.image` version `1`. An external author implements that capability from this specification. This document names the command and the capability only. It does not ask that author to import a Go module or to read an internal package.

The Python operation is `octoconv.convert.imagepdf` version `1`, one PNG in and one single-page PDF out (assumption A6). No such repository exists. The proposed name, to be confirmed before Phase 85, is `tileforge/octoconv-worker-imagepdf` (assumption A2). This phase does not create the repository and does not pin Pillow, img2pdf, or any other library. The capability id does not depend on the repo name.

The Phase 88 chain that does not need an archiver, a CAD feature, or a DAG is JPEG to PNG on the Go capability, then PNG to PDF on the Python capability. The Go input is `internal/convert/testdata/sanitize/exif_gps.jpg`. The Python single-step input is `internal/e2e/testdata/sample.png`. `png` to `png` is not a pair, so the chain cannot start from `sample.png`. The core binary stays unchanged when the new capability is attached: a catalog row, operator approval, and a separate deployment.

Rejected as the first migrated class, and not specified here:

| Class | Why it is not first |
|---|---|
| document | Office conversion, outside this image seam |
| html | Does not continue from a libvips output |
| pandoc | Not the pdf step of this chain |
| cad | Deferred. This phase adds no CAD feature |
| tectonic | Multi-input, not the single-file chain |
| probe | A report plus a primary, not a single-file chain |
| av | Long-running media, a poor first contract test |
| audio | Long-running media, a poor first contract test |
| ocr | Progress and long timeouts, a poor first contract test |
| webhook deliver | Not a file conversion |

### Dispatch owner

Each job gets an immutable dispatch owner at admission: `legacy` or `protocol`.

`legacy` is owned by the existing queue and reconciler. The protocol scheduler must not claim it. Pull does not assign a legacy-owned job.

`protocol` is owned by the protocol scheduler. The legacy reconciler must not enqueue it. That is one skip for protocol-owned rows, not a new case per capability.

`parent_id` is not `step_id`. A step is not a child job. Client `/v1` status enum, error codes, SSE, webhooks, and MCP stay. The one intentional client-visible change for the migrated image class is progress 100 when status becomes `done`.

### Rollout

This order is written now. Execution is later.

1. This phase is spec only.
2. Phases 82-84: the core understands both owners and still admits image as `legacy`.
3. Phase 85: a protocol-capable image worker is deployed and receives nothing until the manifest is approved.
4. The operator approves the image manifest.
5. New image admissions flip to `protocol` and in-flight queue tasks drain.
6. Scale the legacy image deployment to 0 only after the image queue pending, active, and retry depth is 0.
7. The PromQL string does not change.

Quote this PromQL exactly: `sum(octoconv_queue_depth{queue="image", state=~"pending|active|retry"})`. The series name stays `octoconv_queue_depth`. The labels stay `queue` and `state`. There is no `job_id` label. During mixed mode a job has one owner, so it is counted in one place. `imagepdf` does not join queue `image` and does not get a ScaledObject in this milestone. Phase 86 may later feed protocol image counts into that same series: waiting as `pending`, leased or running as `active`, waiting for retry as `retry`.

### Rollback

Stop issuing new protocol attempts. Do not flip the owner of a job that has an active attempt. Do not enqueue the same job to the legacy queue while a protocol attempt exists. Jobs still queued with no active attempt can move back to `legacy` in one transaction that also enqueues the legacy task. An active protocol attempt finishes or expires under the lease rules. Only the protocol scheduler retries it if budget remains.

This phase adds no migration SQL, no worker process, no Python tree, and no change to the Helm chart.

## Schema and examples

[worker-protocol/v1.schema.json](worker-protocol/v1.schema.json) is one Draft 2020-12 document. `$schema` is `https://json-schema.org/draft/2020-12/schema`. `$id` is `urn:octoconv:worker-protocol:v1`. The root is `oneOf` these definitions: `helloRequest`, `helloResponse`, `manifest`, `pullRequest`, `pullAssignmentResponse`, `completeRequest`, `protocolOnlyRequest`, `heartbeatResponse`, `leaseResponse`, `progressRequest`, `failRequest`, `stoppedRequest`, and `error`. `capability` is the closed item inside `manifest`, not a second root branch. A valid document matches exactly one branch. `protocol_version` is `const` 1 on the request and success definitions. `error` requires `code`, `message`, and `retryable`, and has no `protocol_version`. The manifest document has no `protocol_version`. No branch mints or replaces an `href`. `href` remains a field of the assignment file link.

`protocolOnlyRequest` is only `protocol_version`, plus optional `extensions`. It is the body for heartbeat, lease renew, and `DELETE` session. `heartbeatResponse` is `protocol_version` plus `control`. It does not match a body that has `assignments`, `session_id`, or a top-level `lease_expires_at`. `leaseResponse` adds top-level `lease_expires_at` and `attempt_deadline` plus `control`. `progressRequest` requires `percent`, an integer from 0 through 99 or JSON null, so that 100 fails validation. Optional properties are `stage` (string, at least one character), `stage_index` and `stage_count` (integers of at least 1), and `eta_seconds` (integer of at least 0, or JSON null). `failRequest` requires `engine_code` and `message`. `engine_code` is not a protocol error code. `stoppedRequest` requires `reason`, one of `cancel`, `terminate`, and `lease_lost`. These objects are closed. `additionalProperties` is not true on them. `extensions` stays the only open object.

| Example | Role |
|---|---|
| [positive/hello-request.json](worker-protocol/examples/positive/hello-request.json) | HELLO request |
| [positive/hello-response.json](worker-protocol/examples/positive/hello-response.json) | HELLO success, including the placeholder session token |
| [positive/error-unknown-field.json](worker-protocol/examples/positive/error-unknown-field.json) | Valid error body for `unknown_field`, not a client error object |
| [positive/pull-assignment.json](worker-protocol/examples/positive/pull-assignment.json) | One pull assignment response, including the placeholder attempt token |
| [positive/complete.json](worker-protocol/examples/positive/complete.json) | One complete request |
| [positive/manifest-image.json](worker-protocol/examples/positive/manifest-image.json) | `octoconv.convert.image` manifest, 20 format pairs |
| [positive/manifest-imagepdf.json](worker-protocol/examples/positive/manifest-imagepdf.json) | `octoconv.convert.imagepdf` manifest, png to pdf, no legacy engine |
| [positive/manifest-cad-convert.json](worker-protocol/examples/positive/manifest-cad-convert.json) | `octoconv.convert.cad` manifest, step to stl and iges to stl |
| [positive/manifest-cad-render.json](worker-protocol/examples/positive/manifest-cad-render.json) | `octoconv.render.cad` manifest, step, iges, and stl to png |
| [positive/manifest-caddraw-convert.json](worker-protocol/examples/positive/manifest-caddraw-convert.json) | `octoconv.convert.caddraw` manifest, dxf to svg and pdf, and dwg to svg, pdf and png |
| [positive/manifest-caddraw-render.json](worker-protocol/examples/positive/manifest-caddraw-render.json) | `octoconv.render.caddraw` manifest, dxf and dwg to png |
| [negative/pull-unknown-field.json](worker-protocol/examples/negative/pull-unknown-field.json) | A pull request plus top-level `priority` `1` |

The negative file is invalid. The core MUST answer HTTP 400 with `code` `unknown_field`. The same `priority` key inside `extensions` is ignored, not rejected. There is no sibling expected-response file.
