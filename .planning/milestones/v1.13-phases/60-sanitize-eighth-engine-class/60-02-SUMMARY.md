---
phase: 60-sanitize-eighth-engine-class
plan: 02
status: complete
completed: 2026-08-31
commits: ["d97efba"]
---

# 60-02 — PDF sanitization through PostScript

## What was built

`internal/convert/findings.go` (the closed `ThreatClass` vocabulary),
`internal/convert/sanitize.go` (`SanitizeResult`, the per-format registry,
`SanitizeFile`, `SanitizeSupports`), and `internal/convert/sanitizepdf.go` —
Ghostscript `ps2write` to an intermediate PostScript file, then `pdfwrite`
back to PDF. Both passes carry `-dSAFER`; the second carries `-dOmitXMP`.

## Why two passes, measured rather than assumed

The second pass is expensive and the plan required it to earn that cost. On
the committed corpus fixture, a single direct `pdfwrite` pass leaves behind:

| Marker | input | after `pdfwrite` alone | after `ps2write → pdfwrite` |
|---|---|---|---|
| `JavaScript` | 3 | 1 | 0 |
| `EmbeddedFile` | 2 | 2 | 0 |
| `Secret Internal Title` | 1 | 1 | 0 |
| `jane.doe@corp.example` | 1 | 1 | 0 |
| `xpacket` | 0 | 2 | 0 |

The last row is the one worth stating plainly: the input has no XMP packet at
all, and a single `pdfwrite` pass **creates** one carrying the title and the
author it was invoked to remove. That behaviour is pinned by
`TestSinglePassPdfwriteLeavesThreatsBehind`, so the day Ghostscript stops
doing it, the test says so rather than the second pass quietly becoming dead
cost.

## The prior-revision clause

Byte-absence is not absence when a PDF is written as an incremental update:
the old revision is still there, one xref back, and every grep-for-markers
test passes while the construct sits recoverable. `pdfcpu` reads by the last
xref and is blind the same way.

`TestSanitizePDFOutputIsASingleRevision` asserts exactly one `startxref` and
one `%%EOF` in the output, against an input built by `buildIncrementalUpdate`
that genuinely has two revisions with a valid `/Prev` chain.

That fixture had to be built twice. The first version appended a revision with
`startxref 0`; a probe showed Ghostscript never read the appended bytes at all
(the intermediate PostScript came out byte-identical in size to the base run,
166369). The test was green for the wrong reason. With a valid chain, a single
`pdfwrite` pass demonstrably carries the second-revision payload forward — so
the test now fails when the defence is removed, which is the only state in
which its passing means anything.

## What `Removed` claims

`pdfThreatClasses` lists six classes and is declared per format, not derived by
filtering the global list. Returning `AllThreatClasses()` here would have
claimed `vba_macro` and `ole_object` were removed from a PDF — constructs the
format cannot hold. A removal claim about something that was never there is
still a false claim.

## Inversions (D-16)

Four, each producing a distinct failure: drop the `ps2write` pass, drop
`-dOmitXMP`, drop `-dSAFER`, drop the single-revision assertion's fixture
validity. Each named its own defence in the failure output.

## Gates

`gofmt -l .` clean, `go vet ./...` clean, `go build ./...` clean,
`go test ./... -count=1` exit 0.

Ghostscript and `pdftotext` tests skip when the binary is absent, and
`OCTOCONV_REQUIRE_BINS=1` turns that skip into a failure — so a CI host that
silently lost Ghostscript reports a failure, not a pass.
