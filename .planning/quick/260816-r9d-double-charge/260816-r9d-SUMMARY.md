---
task: quick-260816-r9d
status: complete
note: written by the orchestrator — the executor completed and committed all work but was terminated by an API error before writing this file
---

# Round 9 — three findings, all consequences of round 8

## Finding 1 (P1, `media.go:337`) — two owners for one budget

Reproduced by the orchestrator through the real HTTP handler before any
planning, verbatim:

```
coverage.complete=false reason="work_ceiling_bytes"
REPRODUCED: a small valid mp4 reports partial coverage
```

Every inspection of a valid MP4 was reporting selective coverage. Round 8
correctly required that actual reads be charged rather than the declared
ceiling, and the implementation added a per-window charge inside `findMoov`
without noticing `dispatchInspectFamilies` already charged `tr.Consume(maxBytes)`
once per request. Two independent charges against one budget is a resource
with no owner, not a stray line.

**Resolution:** the dispatcher is the single owner; `consumeActual` is
deleted. Round 8's own finding (head + tail reading up to ~2× `maxBytes`) is
NOT reopened by this, and that was checked rather than assumed: the tail
reservation (`headLen + tailReserve <= maxBytes`, gated on `size > maxBytes`)
is what structurally bounds the read volume, and it remains in place, pinned
by `TestAnalyzeMediaNeverReadsBeyondBudget`. With reads bounded structurally,
a single `maxBytes` charge can only over-charge, never under-charge.

## The finding behind the finding

The regression shipped through a fully green suite. It could, because:

- `internal/inspect`'s tests hand a `Tracker` in directly, so the
  dispatcher's own charge is never exercised there;
- `internal/api`'s tests asserted coverage only for *failure* paths — no
  test anywhere asserted that a **valid** file reports complete coverage.

That question was unpinned for every format family, not just MP4.
`TestInspectValidFilesWithinBudgetReportCompleteCoverage` now pins it
through the HTTP handler for mp4, wav, pdf, docx and png.

## Finding 2 (P2, `docsniff.go:120`) — a recorded signal that stopped being read

The `readerAtRecorder` added in round 8 records whether the reader itself
failed, but the flag was consulted at exactly one point. A non-EOF failure
during the ODF/EPUB `mimetype` entry read set the flag afterwards, and
`SniffContainer` returned nil error with an empty format — so the inspect
path reported `format_unrecognized` instead of `read_failed`. The same class
the round-8 fix closed, one call later. The flag is now consulted where the
return decision is made.

## Finding 3 (P2, `media.go:382`) — one family disagreeing with its siblings

A non-EOF read failure on the MP4 head window reported `work_ceiling_bytes`
though no ceiling fired, while WAV and image analysis in the same repository
already mapped that case to `read_failed`. Fixed, and every path in
`media.go` where a `readWindow` failure becomes a Reason was brought to the
same rule rather than patching the named line alone.

## Verification

`go build`, `go vet`, `gofmt`, `go test ./... -count=1 -race` — 25 packages
green. Prior budget tests pass unedited.
