---
created: 2026-10-18
area: api
severity: low
origin: quick task 261018-bmr research
---

# callback_url has no length bound

## What

`handleCreateJob` reads `callback_url` with `r.FormValue` and checks it only with
`validateCallbackURL` (`internal/api/callbackurl.go`: scheme, host, SSRF address checks). Nothing
bounds its length: the column is `text` (`internal/db/migrations/0001_init.sql`), and the only limit
is the multipart parser's own allowance for form values (the 32 MiB `multipartInMemoryBudget` plus
Go's 10 MiB for non-file parts). A URL of tens of MiB is accepted, stored in Postgres and handed to
the webhook worker.

It also sits inside the transport cap. 261018-bmr sized the chart's 183 MiB caps from the envelope
of the largest request the API bounds (a 255-byte input name, opts of exactly `maxOptsBytes`), which
leaves 1043638 bytes above the largest ceiling for the fields the API does not bound. A
`callback_url` longer than that, sent with the largest admissible STL, gets the generic transport
413 instead of the per-format answer. The document's own filename is in the same position: it is
not passed through `jobs.SafeInputFilename`, so its length is not bounded either.

## Idea

- Refuse a `callback_url` longer than a fixed bound (2048 bytes covers any real URL) with
  `invalid_request`, before the format-pair check, the way `opts` is refused above `maxOptsBytes`.
- Bound the document's filename the same way, then add both to the envelope
  `TestDeployedTransportCapsCoverEveryAdvertisedCeiling` measures.

## Constraints

- Existing jobs with a long `callback_url` keep working; the bound applies at create only.
