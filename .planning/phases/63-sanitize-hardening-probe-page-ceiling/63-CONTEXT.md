---
phase: 63-sanitize-hardening-probe-page-ceiling
requirements: [SANI-07, SANI-08, SANI-09, PROBE-01, PROBE-02]
autonomous: true
---

# Phase 63 — decisions

Written inline (`agents_installed: false`) and under the operator's standing
instruction to take the decision I would recommend. Every decision below is
recorded so a reviewer can disagree with the choice rather than reverse-engineer
it from the diff.

## What this phase is

Three findings from round 8 of PR #12, merged **unfixed by operator decision**,
plus the bound the `PROBE_ENGINE_TIMEOUT` measurement showed was missing. The
three sanitize findings share one shape: **a boundary inferred from a substring
or a prefix instead of from the structure that declares it.** That is worth
saying out loud, because it is the same shape as the four bypasses of a single
bound recorded in `.planning/RETROSPECTIVE.md`, and it argues for fixing the
derivation rather than adding a fourth clever comparison.

---

## D-01 — ICC: validate the assembled profile, not each chunk (SANI-07)

`isKeptJPEGAppSegment` reads `payload[14:18]` as the profile's declared size.
That is true **only of chunk 1**: bytes 14..17 there are the ICC header's own
size field. In continuation chunks those four bytes are arbitrary profile data,
so `declared` is noise, and a chunk whose data happens to start with a small
big-endian number is dropped. A partially-dropped ICC profile is worse than no
profile: the file still decodes, the report still says the colour profile was
preserved, and the colours are wrong.

**Decision:** keep or drop the ICC chunk **set**, not individual chunks.
A two-pass walk — first pass collects `(sequence, count, payload)` triples,
validates the set, second pass emits. The set is valid when every chunk carries
the tag, `count >= 1`, `1 <= sequence <= count`, all chunks agree on `count`, no
sequence repeats, every sequence in `1..count` is present, and the **assembled**
payload length is within the bound. Anything else drops the whole set.

**Rejected:** relaxing to "keep any chunk with the tag". It would fix the
reported symptom and reintroduce the round-7 finding one marker over — a prefix
is not a structure.

**Bound:** applies to the assembled profile, and to the chunk count, so a file
carrying 65535 one-byte ICC chunks cannot make the sanitizer allocate without
limit. Value chosen in the plan from the ICC spec's own maximum, not invented.

## D-02 — PDF stream ends at `/Length`, with a stated fallback (SANI-08)

Both `pdfStreamRanges` (redaction) and `pdfNonStreamBytes` (verification) find a
stream's end with `bytes.Index(..., "endstream")`. A binary stream containing
those nine bytes therefore ends early: the redactor's protected range stops
short — so it may rewrite bytes **inside** a stream — and the verifier's revision
count sees `startxref`/`%%EOF` that are stream payload, not file structure.

**Decision:** derive the end from `/Length` in the stream's own dictionary.
Direct integer: end is `start + Length`, **confirmed** by requiring `endstream`
to follow within a short run of whitespace — if it does not, the dictionary and
the bytes disagree and the file is malformed input, not something to guess at.

**Indirect `/Length N 0 R`:** resolve it from a first pass indexing
`N G obj … endobj`. If it cannot be resolved, this input is **unverifiable**,
not "fall back to the substring" — D-07 fail-closed. A silent fallback would
put the defect back exactly where it is now and hide it behind a passing test.

**Measure before writing the fallback.** These two walkers only ever see
Ghostscript's own output. Whether gs writes direct or indirect `/Length` is a
fact, not a guess: the plan measures it across the pinned corpus first and
records the number. If gs never emits indirect lengths, the resolver still gets
written — the verifier must not depend on a property of one gs build — but the
measurement tells us which path the tests must exercise with a hand-built
fixture rather than a corpus file.

**Two walkers stay two walkers.** The existing doc comment explains why
(one subtle byte walker feeding both the redaction and the check that catches
its mistakes is one bug in two places), and `TestStreamRangesAgreeWithTheVerifier`
already holds them to the same answer. Both get the `/Length` derivation; the
agreement test grows the stream-containing-`endstream` case.

## D-03 — the verifier must require the main part (SANI-09)

`verifyOOXML` requires exactly one `[Content_Types].xml` and never asks whether
the document itself is still there. `relationshipDrivenDrops` can drop a part its
`.rels` points at; if that target is the main part, the result is a package that
unzips, verifies clean, and opens as nothing.

**Decision:** the verifier requires, for the declared format, that the main part
(`word/document.xml`, `xl/workbook.xml`, `ppt/presentation.xml`) is present
**and** that `[Content_Types].xml` declares a content type for it. Absence is
`ErrSanitizeUnverifiable` — we cannot say the output is a sanitized document if
the document is gone.

**The verifier learns the format.** `verifyOOXML` currently takes only a path.
It needs the source format to know which main part to require. That is a
signature change inside the package; the alternative — inferring the format from
whichever main part happens to be present — is the same "check only runs when the
part is there" bug this requirement exists to close.

## D-04 — the page ceiling is enforced twice, from one bound (PROBE-01/02)

SC4 asks for `too_many_pages` **before the S3 write**, which means the API. But
the API cannot always know the page count: `internal/inspect`'s
`extractPDFPageCount` is deliberately bounded and returns "unknown" for
compressed or truncated cross-reference structures.

**Decision:** enforce in **both** places, from **one** bound and **one**
comparison function.

* **API, at admission** — bounded peek re-stitched onto the body, exactly the
  `convert.Dimensions` pattern already used for the decompression-bomb guard
  three blocks above. A **known** count over the bound is rejected with
  `too_many_pages` before `s.storage.Upload`. An **unknown** count is admitted:
  fail-closed here would reject legitimate PDFs on a heuristic's blind spot,
  and the second gate below still holds.
* **Worker, before Ghostscript** — the accurate count, the same bound, the same
  comparison. Rejection is terminal (`isProbeTerminal`), which is PROBE-02:
  unlike the timeout, this depends on the file, so a retry reproduces it
  exactly and spending the retry budget on it is pure waste.

**This is deliberately the "same rule in two places" shape this project has been
burned by, so it is built to be one rule:** one env var (`PROBE_MAX_PAGES`), one
exported comparison, and a test that fails if either call site compares against
anything else. The two differ only in how they obtain the count, and that
difference is the entire point — the cheap gate saves an S3 write, the accurate
gate is the bound.

**`too_many_pages` already exists** in `internal/worker/descriptors.go` and is
already produced by the OCR path for the identical situation. Reusing it is
required, not optional: two codes for one condition is a vocabulary defect.

**Default value:** derived from the phase-60 measurement (2.0 s/page against a
180 s timeout, so the timeout alone permits ~90 pages before it becomes the
bound). The plan states the arithmetic and the safety margin rather than picking
a round number, and marks it PROVISIONAL in the same words the timeout was.

## D-05 — every new bound is proven by inversion

Non-negotiable and stated here so no plan can quietly skip it: each new boundary
gets a test that fails when that boundary alone is removed, and the inversion is
run and its output recorded. SC5 is the whole of D-16 restated for this phase.

## Out of scope, said explicitly

* ~~Making `pdfStreamRanges` and `pdfNonStreamBytes` one function.~~ **ALSO
  REVERSED, also on evidence.** The two were separate so that one subtle walker
  could not feed both the redaction and the check meant to catch its mistakes.
  The reasoning was sound; the outcome was not. In all three review rounds the
  two walkers had *identical* bugs — review said so in as many words — because
  one author wrote both from one misunderstanding. The independence paid nothing
  on any occasion and cost a second place to fix and a second chance to diverge
  on trivia (they disagreed about whether a form feed is whitespace, which cost a
  valid file its output). They now share the tokenizer and keep their
  propositions apart, which is where the independence was always load-bearing.
* ~~A general PDF object parser. The resolver indexes `N G obj` offsets and reads
  one integer; anything more is a parser, and this phase is not writing one.~~
  **REVERSED during execution, on evidence.** Three rounds of review on PR #14
  found six defects in the byte-scanning version and every one was the same
  defect: a token recognised by its bytes without regard to the lexical context
  it sits in (`stream` inside `endstream`; `/Length` inside `/Length1`; object
  headers inside payloads; object headers inside comments; `stream` inside a
  comment; `>>` inside a literal string). Patching a seventh would have been the
  sixth clever comparison in a row against a bound that keeps getting bypassed —
  the pattern `.planning/RETROSPECTIVE.md` already records and which was resolved
  once before by replacing the comparison, not refining it. `internal/convert/pdflex.go`
  models PDF's four lexical contexts and nothing more: no document, no reference
  resolution beyond one integer, no decoding.
* Page ceilings for engines other than probe. OCR already has its own; the rest
  have no per-page cost model, and inventing one without a measurement is the
  thing phase 60 established is not worth doing.
