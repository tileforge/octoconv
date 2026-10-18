---
created: 2026-10-18
area: api
severity: low
origin: quick task 261018-dlp research
---

# Inspect's post-intake discard read can hold an analysis slot until the intake deadline

## What

Analysed, not reproduced by a test.

- When a response first flushes, net/http's `writeHeader` (go1.26.4 `net/http/server.go:1392-1437`)
  reads up to 256 KiB + 1 (`maxPostHandlerReadBytes`) of any unread request body, under the
  connection's current read deadline.
- An inspect response flushes inside the handler once it exceeds 2048 bytes
  (`bufferBeforeChunkingSize`). Responses of up to 3623 bytes were measured in the api tests' Logger
  lines, so the flush, and with it the discard read, can happen while `s.inspectSem` (the analysis
  slot, `INSPECT_MAX_CONCURRENT`) is still held.
- So a client that sends a valid, complete multipart form and then stalls with declared bytes left
  (Content-Length larger than the form) holds an analysis slot until the read deadline expires.
- Before 261018-dlp that hold was at most 30 s from intake start. After it, the hold is at most
  intake start + window + received / minimum rate (`inspectIntakeReader.deadline`).
- The cheapest variant is unchanged: a tiny form earns almost no credit, so it still ends at about
  30 s. The longer hold requires sending up to the full cap first, which costs the client at least
  1 MiB/s for that time.

## Idea

- On a successful parse, set the read deadline to the earlier of the current deadline and now plus a
  short grace, so the discard read ends quickly and the analysis slot is not held for the network.

## Constraints

- This needs a decision: shortening the deadline after the parse also shortens the slack net/http's
  background read has (`server.go:701-777`). That read starts at body EOF under the same deadline,
  and its expiry cancels the request context while the handler still runs, which turns PreAdmit
  verdicts into "unknown" (decision D9 of quick 261018-dlp).
- `inspectIntakeMaxConcurrent` and the intake deadline bound the intake slot; this todo is about the
  separate analysis slot only.
