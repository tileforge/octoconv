# Conformance

[Worker Protocol](../WORKER-PROTOCOL.md) · [Documentation](../README.md)

The prose in [Worker Protocol](../WORKER-PROTOCOL.md), the schema in [v1.schema.json](v1.schema.json), and the fixtures under [examples/](examples/) are the source of truth. They are not generated from `internal/errcode`. Phase 81 does not automate these scenarios. This file is not a test harness and it is not a Go test.

An independent author can execute the numbered scenarios later, without a running API in this phase. Each item names the message, the expected code or control flag, and the state that must not change. On the wire, an unknown top-level field is an error. A key inside `extensions` is not. This list does not tell a client to accept an unknown client error code. Client `/v1` codes stay out of this vocabulary. No scenario mints or extends an `href`. hrefs stay on the assignment.

## Scenarios

1. Lost pull response: message `POST /worker/v1/sessions/{id}/pull` retried with the same `pull_request_id` and the same raw body. Expected: the stored assignment or the stored empty list, and one attempt exists. Unchanged: no second attempt and no second core effect. The stored assignment is [examples/positive/pull-assignment.json](examples/positive/pull-assignment.json).

2. Two in-flight pulls with the same id: message two concurrent `POST /worker/v1/sessions/{id}/pull` calls that share one `pull_request_id` and one raw body. Expected: one assignment and one stored response. Unchanged: no second attempt is created.

3. Payload conflict on pull: message the same `pull_request_id` with a different raw body. Expected: HTTP 409 `pull_conflict`. Unchanged: no second attempt, and the stored response for the first body is not replaced.

4. Empty pull is stored and replays as empty: message `POST /worker/v1/sessions/{id}/pull` whose stored success has an empty `assignments` array. Expected: a later pull with the same raw body returns that empty list, not an assignment. Unchanged: replay does not create an attempt. [examples/positive/pull-empty.json](examples/positive/pull-empty.json) is the stored empty pull.

5. After session close, a pull is `session_closed` even if dedup rows were deleted: message `POST /worker/v1/sessions/{id}/pull` on a closed session. Expected: HTTP 401 `session_closed`. Unchanged: deleting dedup rows does not reopen the session and does not create an attempt.

6. A new session does not release the previous session's leases and does not raise the owner cap: message a new `POST /worker/v1/sessions` after an earlier session still holds leases. Expected: the new session is a separate row. Unchanged: the previous session's leases stay, and the owner cap is not raised.

7. Heartbeats do not move deadlines: message `POST /worker/v1/sessions/{id}/heartbeat`. Expected: success carries `control` and does not include a new lease expiry. Unchanged: `attempt_deadline` and every existing file-link `expires_at` stay where they are.

8. Renew at the deadline: message `POST /worker/v1/attempts/{id}/lease` when `server_now` is at or past `attempt_deadline`. Expected: HTTP 409 `attempt_not_active`. Unchanged: the attempt row is not changed, `attempt_deadline` does not move, and no link is extended.

9. Lost completion response: message `POST /worker/v1/attempts/{id}/complete` replayed with the same raw body after lease expiry. Expected: the stored success. Unchanged: accounting is not double-applied and a second attempt is not opened. [examples/positive/complete.json](examples/positive/complete.json) is the completion body.

10. Payload conflict on complete: message `POST /worker/v1/attempts/{id}/complete` for the same attempt with a different raw body than the stored completion. Expected: HTTP 409 `complete_conflict`. Unchanged: no state change, and accounting is not applied again.

11. Cancel on heartbeat: message `POST /worker/v1/sessions/{id}/heartbeat` whose success sets `control.cancel_attempt_ids` for an attempt. Expected: a later `POST /worker/v1/attempts/{id}/complete` for that attempt is HTTP 409 `attempt_not_active`. Unchanged: that complete does not apply the result.

12. Identity revoke: message the next call on the open connection after the certificate serial is denylisted and every session for that identity is closed. Expected: HTTP 401 `session_closed`. Unchanged: the closed sessions stay closed and no new attempt is assigned on that connection.

13. Plaintext URL: message any worker dial to a plaintext URL. Expected: the client profile refuses it. There is no server flag to allow plaintext. Unchanged: no session is created.

14. Redirect: message a 3xx from the core or from storage. Expected: the worker does not follow and does not send `Authorization` to the new host. The worker-local outcome is `origin_rejected`, which is not a core error-table row. Unchanged: no credentials are forwarded and the new host is not contacted with `Authorization`.

15. Bad client certificate or DNS SAN mismatch: message the TLS handshake. Expected: no session. Unchanged: no session row and no capability grant.

16. Incompatible version: message any body whose `protocol_version` is an integer other than 1, including the pull fields in [examples/negative/protocol-version-2.json](examples/negative/protocol-version-2.json). Expected: the schema rejects that fixture, and the core answers HTTP 400 `incompatible_protocol`. Unchanged: no session and no attempt are created.

17. Unknown field: message a top-level field outside the schema, as in [examples/negative/pull-unknown-field.json](examples/negative/pull-unknown-field.json). Expected: HTTP 400 `unknown_field`. The same unknown key inside `extensions` is ignored, and [examples/positive/pull-with-extensions.json](examples/positive/pull-with-extensions.json) is accepted. Unchanged: `unknown_field` creates no attempt, and an ignored `extensions` key changes no state beyond the request without that key.

18. Worker percent 100: message `POST /worker/v1/attempts/{id}/progress` with percent 100, as in [examples/negative/progress-percent-100.json](examples/negative/progress-percent-100.json). Expected: HTTP 400 `invalid_progress`. Unchanged: stored progress is unchanged and the attempt remains active, including when the request is repeated.

19. Retry progress starts unknown: message the first progress report on a retry attempt. Expected: client-visible progress is null, not the previous attempt's percent. Unchanged: the previous attempt's percent is not copied onto the new attempt.

20. File-link expiry: message assignment of a file link. Expected: `expires_at` is the minimum of `issued_at` plus `file_link_max_ttl`, the current `lease_expires_at`, the fixed `attempt_deadline`, and `job_deadline`. A link whose expiry is not strictly after `server_now` plus `clock_skew_margin` is not issued. Unchanged: no link is written in the skew margin, and already issued links are not rewritten.

21. No further links: message cancel, identity revoke, or complete. Expected: the core issues no further links. Unchanged: no new `href` is minted.

22. Direct GET at expiry: message an in-progress direct GET whose link then reaches `expires_at`. Expected: `direct` does not abort that GET. Unchanged: bytes already downloaded are not clawed back.

23. Gateway without a gateway: message process start with `file_access` `gateway` on a build that has no gateway. Expected: the process refuses to start. Unchanged: no session is opened.

24. Attempt token on the wrong attempt: message `Octoconv-Attempt` for attempt A presented on attempt B. Expected: HTTP 403 `capability_denied`. Unchanged: B is unchanged.

25. Unapproved manifest: message `POST /worker/v1/sessions` whose `manifest_sha256` does not match the approved row. Expected: HTTP 403 `manifest_mismatch`. Unchanged: no capability is granted and the catalog row is not inserted by HELLO.

26. One dispatch owner: message a pull, or a pass of the legacy reconciler. Expected: a legacy-owned job is never assigned on pull, and a protocol-owned job is never re-enqueued by the legacy reconciler. Unchanged: the job's owner is not flipped and the other owner does not claim the job.

27. Missed heartbeats: message three missed `POST /worker/v1/sessions/{id}/heartbeat` intervals. Expected: the instance is shown offline. Unchanged: the session stays open and the lease keeps running. Display-only offline does not set `control.terminate` and does not remove a capability.

28. Terminate: message a heartbeat or other success whose `control.terminate` is true, then the worker's stopped report. Expected: `control.terminate` causes `stopped`, not `fail`. Unchanged: the attempt is not recorded as an engine failure.
