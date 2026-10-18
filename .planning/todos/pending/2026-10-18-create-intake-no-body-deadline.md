---
created: 2026-10-18
area: api
severity: medium
origin: quick task 261018-dlp research
---

# POST /v1/jobs reads its body with no deadline and no intake bound

## What

`handleCreateJob` (`internal/api/handlers.go:148-149`) wraps the body in `http.MaxBytesReader` and
calls `ParseMultipartForm(multipartInMemoryBudget)` with no read deadline and no intake semaphore.
`cmd/api/main.go` sets only `ReadHeaderTimeout` (10 s); once the headers are in, net/http clears the
connection's read deadline, so nothing bounds how long a create body may take.

- A stalled create holds its goroutine, its connection, up to `multipartInMemoryBudget` (32 MiB) of
  buffered file data and its spill file, indefinitely.
- Only `RATE_LIMIT_IP_RPM` (60) and `RATE_LIMIT_CLIENT_RPM` (120) bound how many accumulate. That is
  the PR9R8-F1 heap arithmetic inspect closed with `inspectIntakeMaxConcurrent`: 60 stalled creates
  of 32 MiB is about 1.9 GiB against the 1 GiB api pod.
- The same no-deadline condition makes net/http's post-handler discard read on create unbounded: a
  create answered before its body is fully read waits on the rest of the body with no deadline.

## Idea

- Apply `inspectIntakeReader` (`internal/api/inspectintake.go`, quick 261018-dlp) to create, with its
  own window and minimum rate. The decision to make: create's caps are larger, and the av ceiling of
  2 GiB in compose gives a worst-case hold of 30 + 2048 s at 1 MiB/s.
- Alternatively or additionally, add a create intake semaphore around `ParseMultipartForm`, which is
  the second idea of `2026-10-18-api-multipart-spill-unbounded-disk.md`.

## Constraints

- 260810-slf deliberately declined a body timeout here. Its SUMMARY decision reads: "No
  cmd/api/main.go body-read timeout was added: a server-wide http.Server.ReadTimeout would also apply
  to POST /v1/jobs' own legitimately large/slow uploads."
- A minimum-rate deadline does not have that problem, since a slow but steady upload keeps earning
  time, but its rate and window need a decision for create's larger caps.
- A create intake semaphore turns load into a 429/503; the answer code needs a decision, as
  inspect's `inspect_busy` did.
