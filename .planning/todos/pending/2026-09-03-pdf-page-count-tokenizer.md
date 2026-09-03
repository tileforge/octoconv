---
created: 2026-09-03
area: inspect
severity: low
origin: PR #15 (phase 63-02), removed with the admission gate rather than kept
---

# internal/inspect's page_count is derived by a byte scan with four known defects

Phase 63-02 built `convert.PDFTypedDictUints` — a page-count reader on
`pdflex.go`'s tokenizer — for the sanitize admission gate, and deleted it when
that gate was removed (63-CONTEXT.md D-04, reversed). The gate is gone; the
defects it exposed in `internal/inspect` are not.

`extractPDFPageCount` still finds a `/Type /Pages` marker by literal bytes and
takes the first `/Count` within 512 bytes after it. Four ways that is wrong were
found by review, each reproduced by running:

| what | measured |
|---|---|
| `/Type /Pages /Count 300` inside a content stream | read as structure |
| `<< /Count 2 /Type /Pages >>` followed by `/Outlines /Count 300` | reports 300 |
| `/Count300` | matched as the key `/Count` |
| `/Extra [ /Count 300 ]` | read as a direct key |

Plus one the tokenizer also fixed: `/Count 5 0 R` is read as five pages rather
than as a reference to object 5.

## Why this is LOW and not urgent

`page_count` is an advisory CHARACTERISTIC of `POST /v1/inspect`. It is reported
as data, never enforced, so a wrong number is a wrong number in a report — which
is why these defects survived a whole phase unnoticed. They became severe only
when a gate started REFUSING uploads on the value, and that gate no longer
exists.

## The fix, already written once

`git show 09e85b2 -- internal/convert/pdfdictscan.go` is the tokenizer-based
reader, and `git show c05fc27` / `28416c7` carry its key/value model and the
string-value fix. `git show 9e155bd -- internal/inspect/pdfpagecount_differential_test.go`
is a differential table against poppler over every committed PDF, which is the
test that would keep it honest.

**It must not be restored as-is.** Two P1 resource findings against it were
still open when it was removed:

* the xref-derived object index was unbounded (`pdfXrefObjects`' own subsection
  limit is 1,048,576; ~28.5 MB measured on a 6.5 MiB input);
* dictionary nesting grew the frame stack without a depth cap (313 MB and ~2s
  measured on a 2 MiB body of `<<` tokens).

Both are cheap to bound and both are recorded here so a future restore starts
from the finished list rather than rediscovering them.

## Also open, and independent of all the above

`pdfTrailerPrev` finds the first raw `/Prev` substring, including one inside a
trailer string, so `<< /Note (/Prev 999) /Prev 12345 >>` follows the decoy. That
one is in `pdflex.go` and affects the **sanitize worker** today, not only
inspect — it predates 63-02 and was found in the same review.
