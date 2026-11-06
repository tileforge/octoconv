# Worker Protocol

[Back to OctoConv](../README.md) · [Documentation](README.md)

This specification pull request is the Codex review gate before any runtime phase.

Project version v2.0 and Worker Protocol v1 are different numbers. Client `/v1` stays the client API. Workers dial out and pull. They hold no Postgres, Redis, or shared S3 credentials. They import no Go module. This surface is not MCP and not client `/v1`.

The machine-readable contract is [worker-protocol/v1.schema.json](worker-protocol/v1.schema.json) (JSON Schema draft 2020-12). Message examples are under [worker-protocol/examples/](worker-protocol/examples/). The conformance plan adds `worker-protocol/conformance.md`.

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

A lost response is retried as the same `pull_request_id` and the same raw body, or as the same completion body. Workers follow no redirects. Responses include `Cache-Control: no-store`.

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

Attempt-scoped calls also send header `Octoconv-Attempt`. That token is a distinct secret minted with the assignment. It authorizes only lease renew, progress, fail, complete, and stopped for that `attempt_id`. Those five are the attempt-scoped methods in the method table. Session-only calls reject an attempt token and change nothing. An attempt token for attempt A presented on attempt B is `capability_denied` and changes nothing on B.

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
| `file_link_max_ttl_seconds` | 300 |
| `clock_skew_margin_seconds` | 5 |
| `pull_rate_per_second` | 2 |
| `progress_rate_per_second` | 4 |
| `file_access` | `direct` or `gateway` |

`manifest_sha256` is the SHA-256 of the exact installed manifest file bytes. The Manifest section states how the core compares it. HELLO does not grant execution.

A body the schema rejects as an unknown field is answered with the closed error object. [worker-protocol/examples/positive/error-unknown-field.json](worker-protocol/examples/positive/error-unknown-field.json) is a valid error body: `code` `unknown_field`, a short `message`, and `retryable` false. It is not the client shape that uses an `error` field.

## Methods

Every method uses a JSON body, including success. There is no `204`. An empty pull is HTTP 200 with `assignments` empty, and that response is stored for dedup. This document fully specifies HELLO, the pull assignment, and the complete request. Later text adds transition rules for the other methods and does not rename these paths. `href` values exist on the assignment. This protocol has no method that mints a new `href`.

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

`POST /worker/v1/sessions/{id}/pull` requires `protocol_version`, `pull_request_id` (UUID), `free_slots` (integer, minimum 0), and `wait_seconds` (integer). The same `pull_request_id` with the same raw body returns the stored response. The same id with a different raw body is `pull_conflict` and changes no state.

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
| `attempt_not_active` | 409 | Lease renew or a result arrived when the attempt is no longer the active one |
| `artifact_rejected` | 409 | HEAD, size, or sha256 does not match the completion list |

Stable engine-failure strings travel inside `fail`. The core maps them onto the existing client allow-list. The migrated image class stays `engine_error`. Those strings are not protocol codes.

Worker percent is an integer from 0 through 99, or JSON null when progress is unknown. A worker MUST NOT send 100. The value 100 is `invalid_progress`. The core sets client-visible progress to 100 only when the job becomes `done`. Unknown progress stays null.

## Assignment

A pull success body contains `protocol_version`, `assignments`, and `control`. `assignments` has at most one element. An empty array is the empty pull. A normal job is one step. The core mints `job_id`, `step_id`, and `attempt_id`. File bytes are not in the JSON.

| Field | Constraint |
|---|---|
| `job_id`, `step_id`, `attempt_id` | UUID strings |
| `capability_id` | Capability identifier. The tracer fixture uses `octoconv.convert.image` |
| `capability_version` | Positive integer. The tracer fixture uses `1` |
| `attempt_deadline`, `lease_expires_at` | RFC3339 UTC timestamps |
| `limits.max_output_bytes` | Integer. The tracer fixture uses `104857600` |
| `inputs`, `outputs` | Arrays of file links |

Each file link has `role`, `ordinal`, `format`, `href`, and `expires_at`. `href` is a URI. The schema does not fix the host. The tracer fixture uses input role `source`, format `jpg`, and output role `primary`, format `png`, with `https://storage.example/presigned-get` and `https://storage.example/presigned-put`. Those hrefs have no query string and no object key. No session token appears in the fixture.

On the `direct` profile, the link is a bearer URL until `expires_at`. This slice does not promise to revoke bytes already downloaded and does not promise to abort an in-progress direct GET.

The published assignment is [worker-protocol/examples/positive/pull-assignment.json](worker-protocol/examples/positive/pull-assignment.json).

## Complete

`POST /worker/v1/attempts/{id}/complete` requires `protocol_version` and `artifacts`. The worker does not choose the object key. Each artifact has `role`, `ordinal`, `format`, `byte_size`, `content_type`, and `sha256`. `sha256` is 64 lowercase hex digits, the SHA-256 of the artifact bytes.

The tracer fixture lists one artifact: role `primary`, ordinal `0`, format `png`, `byte_size` `1234`, `content_type` `image/png`, and a `sha256` of 64 `b` characters, plus `protocol_version` `1`. That body is [worker-protocol/examples/positive/complete.json](worker-protocol/examples/positive/complete.json).

The same completion body retries a lost response. A different raw body for the same attempt is `complete_conflict` and changes no state. Accepting that completion is an exactly-once core effect. It is not exactly-once computation.

## Schema and examples

[worker-protocol/v1.schema.json](worker-protocol/v1.schema.json) is one Draft 2020-12 document. `$schema` is `https://json-schema.org/draft/2020-12/schema`. `$id` is `urn:octoconv:worker-protocol:v1`. The root is `oneOf` these definitions: `helloRequest`, `helloResponse`, `pullRequest`, `pullAssignmentResponse`, `completeRequest`, and `error`. A valid message matches exactly one branch. `protocol_version` is `const` 1 on the request and success definitions. `error` requires `code`, `message`, and `retryable`, and has no `protocol_version`. No branch mints or replaces an `href`. `href` remains a field of the assignment file link.

| Example | Role |
|---|---|
| [positive/hello-request.json](worker-protocol/examples/positive/hello-request.json) | HELLO request |
| [positive/hello-response.json](worker-protocol/examples/positive/hello-response.json) | HELLO success, including the placeholder session token |
| [positive/error-unknown-field.json](worker-protocol/examples/positive/error-unknown-field.json) | Valid error body for `unknown_field`, not a client error object |
| [positive/pull-assignment.json](worker-protocol/examples/positive/pull-assignment.json) | One pull assignment response |
| [positive/complete.json](worker-protocol/examples/positive/complete.json) | One complete request |
| [negative/pull-unknown-field.json](worker-protocol/examples/negative/pull-unknown-field.json) | A pull request plus top-level `priority` `1` |

The negative file is invalid. The core MUST answer HTTP 400 with `code` `unknown_field`. The same `priority` key inside `extensions` is ignored, not rejected. There is no sibling expected-response file.
