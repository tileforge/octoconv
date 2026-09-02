---
phase: 59-born-digital-pdf-routing
plan: 03
subsystem: convert, api
tags: [read-failed, sniffing, budget, equivalence-testing, inversion-proof]

requires:
  - phase: 58-synchronous-inspection
    provides: "detectInspectFormat chain, sniffReadFailed accumulator (PR9R25/PR9R27), Tracker"
  - plan: 59-02
    provides: "read_failed now makes a report incomplete — without it this fix would surface a reason nothing acted on"
provides:
  - "convert.SniffWindowBytes, ValidateSniffPrefix, and four *FromPrefix matchers"
  - "One sniff window in detectInspectFormat, charged to the operation's budget"
  - "Three distinguishable outcomes: read error / not text-shaped / matched"
affects: [59-05, 59-07]

tech-stack:
  added: []
  removed: []
status: complete
---

# Plan 59-03 — Summary

Commit `a804f87`. Closes the second folded todo (item 2).

## The defect

Four matchers, four separate `ReadAt`s, four bare `bool` returns. A read failure
inside any of them was indistinguishable from "not this format", so an upload
whose temp file died on exactly those reads came back `format_unrecognized` — a
claim about the **file**, made on the strength of our own inability to read it.
Same class as PR9R25/PR9R27, one layer down.

## Shape

`readSniffWindow` reads once; three outcomes stay apart:

| Outcome | Result |
|---|---|
| non-EOF `ReadAt` error | `sniffReadFailed`, matchers do not run |
| bytes read, envelope rejects (NUL / invalid UTF-8 / empty) | not text-shaped — chain continues, **not** a read failure |
| bytes read and accepted | the three text matchers judge them |

Folding the middle case into the first would be a new lie in the other
direction, so a binary upload still reports unrecognized.

The four `io.ReaderAt` signatures are unchanged and delegate to `*FromPrefix`.
That delegation is the point: a copy would be the defect `ocrPDFGuardStage` was
factored out to remove — and `LooksLikeHTML` already carried its own copy once
and missed the PR#2 mid-rune fix because of it.

`IsOLECFBFromPrefix` takes **raw** bytes, not the validated prefix: the
signature starts `0xD0` and is binary throughout, so the text envelope would
reduce it to nothing. One read, two views.

**Matcher order preserved exactly.** The two groups cannot both match one file,
so the order is unobservable — kept anyway, because an unobservable reordering
is still a second change riding inside the first.

## The budget, and an invariant I nearly broke

Charging the window required moving the `Tracker` construction ahead of
detection. The first attempt used `Consume`'s boolean as a gate, and
`TestInspectCFBWalkStaysInsideTheWorkAllowance` went red immediately: with a
64-byte budget, detection starved and reported `format_unrecognized` — the exact
our-budget-becomes-a-claim-about-the-file substitution this phase removes.

The bytes are already read by the time `Consume` is called. Refusing to *look*
at them buys nothing. The call is for the accounting and the exhaustion latch;
its result is deliberately not a gate, and the comment says so.

`dispatchInspectFamilies` claimed to be **the sole `Consume` caller in the whole
inspect path** (PR9R9 finding #3). That sentence is no longer true, and I updated
it rather than leaving it to rot. The distinction matters: PR9R9 was
**double-counting** — media.go charged the same bytes dispatch had already
charged. Detection charges a *different* read, once; dispatch charges what
remains, once. One charge per read; nothing counted twice.

## Inversions — two, for two properties

| Inversion | Red |
|---|---|
| Undo the shared window (four direct calls) | `TestInspectContentMatcherReadFailure…`, `TestInspectSniffWindowIsChargedToTheBudget` |
| Re-split the wrapper from its prefix form (drop the BOM trim) | `TestMatcherFormsAgree`, both directions |

## The test that took three attempts

Worth recording, because two of the three were green for the wrong reason.

1. **Cascading fake** (`failAfterNthReadAtFile`: fail read N and everything
   after). Passed against the **old** code too — the later video/audio sniffers
   also failed, and PR9R27 had already taught *those* to report `read_failed`.
   Green, and about nothing.
2. **Fail read #2 by index** — a guess. The read order was then **measured**:
   12 bytes (`convert.Sniff` magic peek), 4 bytes (PK/zip prefix), then the
   window. Read #2 is the PK peek, whose error is discarded by design. The test
   was targeting a read it has no opinion about.
3. **Target by read size**, over a file larger than `SniffWindowBytes` so no
   other read shares that length — plus `matched == 1` asserted. That assertion
   is what caught attempt 2b, where a small file made every later sniffer read
   the whole thing and the fake fired three times.

The inversion is what exposed attempt 1. Had I written the summary after the
first green run, it would have claimed a reproduction that never happened —
`[[optimistic-answer-for-unexamined-input]]` in test form.

## Gate

| Check | Result |
|---|---|
| `go build` / `go vet` / `gofmt -l` | clean |
| `go test ./...` | green, 30 packages |
| `internal/api/handlers.go`, `internal/convert/pandoc.go` | untouched (D-12) |
| Existing `convert` tests | pass unmodified — signatures preserved |
| `TestPre59GoldenInspectPDF` | unchanged |
