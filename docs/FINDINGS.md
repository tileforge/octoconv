# OctoConv Findings Codes

**Status: introduced in milestone v1.13 Phase 58 (INSP-02/INSP-03).**

This document is the single canonical, versioned list of every machine-readable finding code
`POST /v1/inspect` (and, as a strict subset, `handleCreateJob`'s existing content-validation
rejections — see "Binding to existing job-creation rejections" below) can emit about a file's
content. Later phases (60 onward, including `sanitize`'s report) may ADD new codes to the table
below. **No phase may rename or repurpose an existing code** while consumers are migrating onto
this list — the same discipline `docs/ERROR-CODES.md` already applies to error codes.

Go constants are the source of truth. This document is generated/maintained from them, and
`internal/inspect/doc_sync_test.go` mechanically proves the two never drift apart — see "Keeping
this document honest" at the bottom.

## Relationship to docs/ERROR-CODES.md

These are two DIFFERENT documents answering two DIFFERENT questions:

- `docs/ERROR-CODES.md` answers **"why was the request rejected"** — a synchronous `4xx`/`429`
  HTTP response, or an async job failure.
- `docs/FINDINGS.md` (this document) answers **"what was found in the file"** — a fact about the
  uploaded content, independent of whether the request that carried it succeeded or failed.

A single file can produce a finding here and NOT produce a rejection there — that is the normal
case, not a sign of disagreement between the two documents. The synchronous `inspect` path only
**reports** findings and never rejects on them; `handleCreateJob` continues to reject on its own
(smaller) subset of the same codes. One closed vocabulary, two consumers, declared once so they
cannot drift apart from each other (see "Binding to existing job-creation rejections" below).

## Consumer contract (mirrors ERROR-CODES.md's Pitfall 12 — read this before writing a switch)

**Consumers MUST treat an unrecognized finding code as a generic fallback. Consumers MUST NOT
throw, crash, or fail closed on an unrecognized code.** This is a requirement, not a suggestion —
future phases (`sanitize` in 60 and beyond) WILL add new codes to the table below, and those
additions are contractually non-breaking only if every consumer already tolerates an unknown
value.

**Do NOT generate a closed TypeScript union or a closed Go enum from the current list on the
consumer side.** A closed type looks more "correct" at code-review time, but it turns every
future additive code into a compile-time or runtime breaking change for that consumer the moment
this list grows.

## Findings vocabulary

Fourteen codes, closed (D-05: no typed findings existed anywhere in the project before this
phase). Source: `internal/inspect/findings.go` (`AllCodes()`, `SeverityOf()`).

| Code | Severity | Applies to | Detected by | What a client should do |
|------|----------|------------|--------------|--------------------------|
| `ooxml_macro` | warning | docx/xlsx/pptx/odt/ods/odp/epub | `ContainerResult.HasMacro` (`internal/convert/docsniff.go`) | Consider `sanitize` before further processing; a macro is inert until executed by downstream tooling |
| `zip_bomb_shape` | critical | any zip-based container | `ContainerResult.TotalUncompressed` above a findings-level threshold (milder than `handleCreateJob`'s hard reject threshold) | Do not decompress the file yourself without a bound; strongly consider rejecting or sanitizing |
| `image_pixel_bomb_shape` | critical | image formats | Declared pixel dimensions (`convert.Dimensions`) above a findings-level threshold | Do not decode the image without a bound; strongly consider rejecting or sanitizing |
| `ole_cfb_legacy` | info | legacy binary Office (doc/xls/ppt) | `ClassifyCFB` returned `CFBLegacy` | No action required; informational format fact |
| `ole_cfb_encrypted` | info | any OLE-CFB | `ClassifyCFB` returned `CFBEncrypted` | No action required; a privacy signal, not a code-execution risk |
| `ole_cfb_unclassified` | info | any OLE-CFB | `ClassifyCFB` returned `CFBUnknown` (header present, directory did not classify cleanly) | Treat as "we could not classify this container" rather than "this container is clean" |
| `extension_mismatch` | info | any format | Filename extension normalizes to a format different from the one detected by magic bytes | No action required; informational fact about client-supplied metadata |
| `pdf_javascript` | critical | PDF | Literal token `/JavaScript` or `/JS` | Strongly consider `sanitize` or reject; can execute automatically in a compliant viewer |
| `pdf_action_dict` | critical | PDF | Literal token `/OpenAction` or `/AA` | Strongly consider `sanitize` or reject; can auto-trigger on open or another document event |
| `pdf_launch_action` | critical | PDF | Literal token `/Launch` | Strongly consider `sanitize` or reject; can invoke an external application/command |
| `pdf_embedded_file` | warning | PDF | Literal token `/EmbeddedFile` | Consider `sanitize` before further processing; harmless until the recipient opens/extracts it |
| `pdf_encrypted` | info | PDF | `/Encrypt` present in the trailer or XRef-stream dictionary | No action required; a privacy signal, not a code-execution risk |
| `external_reference` | warning | PDF, OOXML, ODF | PDF `/URI`, or an OOXML/ODF relationship with `TargetMode="External"` (one code, two sources, deliberately unprefixed) | Consider `sanitize` before further processing; a passive exfiltration/tracking vector |
| `metadata_present` | info | JPEG/PNG/TIFF/PDF | EXIF/XMP/GPS segment (JPEG APP1, PNG `eXIf`/`iTXt`, TIFF IFD) or PDF `/Info` | No action required; informational fact about embedded metadata |

## Severity tiers

Three tiers, not five. `docs/ERROR-CODES.md` deliberately keeps error codes coarse ("granularity
is by failure CLASS, not by call site") — the same discipline applies here. A five-level scale
(info/low/medium/high/critical) invites debate about the boundary between adjacent levels with no
operational difference in client behavior behind it. Three tiers map directly to three DIFFERENT
actions a client can honestly take:

| Severity | Meaning | Client action |
|----------|---------|----------------|
| info | Structural fact, not itself a risk | None required |
| warning | Passive risk: content that could be an exfiltration/tracking vector, but requires the recipient to act | Consider `sanitize` before further processing |
| critical | Active/auto-triggered risk: executes/takes effect without further recipient action | Strongly recommend `sanitize` or reject |

## Check status (three values, never two)

Every check `inspect` performs against a code in the vocabulary above carries exactly one of
three status values (`internal/inspect/status.go`):

| Status | Meaning |
|--------|---------|
| `clean` | The check applies to this format, ran to completion, and found nothing |
| `not_applicable` | The check does not apply to this format at all — there was nothing to look for, not "we looked and found nothing" |
| `unknown` | The check applies, but its result could not be determined — this is NEITHER a synonym for `clean` NOR for `not_applicable` |

**Consumers MUST have three branches for this field, never two.** A client-side check of the
shape `if status != "clean"` silently collapses `unknown` into `not_applicable` (or vice versa) —
exactly the defect this three-value scale exists to prevent. The correct consumer shape is a
`switch`/`if` with one branch per value plus an `unknown-value` fallback for a future addition to
this list (the same open-string discipline as the "Consumer contract" section above).

## Report coverage is a separate claim

Whether an inspect report is honest about its own completeness is a DIFFERENT question from any
individual check's status, and the two are never folded into one field (`internal/inspect/status.go`,
`Coverage`):

| Reason a check is not `clean` | Property of | Makes the report selective? |
|---|---|---|
| `format_not_applicable` | the file's format | No |
| `format_unrecognized` | the file (format could not be determined at all) | No |
| `compressed_object_streams` | the file's structure (D-02a: PDF `/ObjStm` blindness) | No |
| `pdf_name_escape` | the file's structure (a PDF name written with a `#XX` hex escape) | No |
| `pdf_stream_body_token` | the file's structure (the token was seen only inside a stream body or comment) | No |
| `parser_unavailable` | this build | No |
| `malformed_input` | the file | No |
| `read_failed` | an operational I/O failure (independent of the file's structure and of this service's own byte/time budget) | **Yes** |
| `work_ceiling_bytes` | **this service's own load** | **Yes** |
| `work_ceiling_time` | **this service's own load** | **Yes** |

`format_unrecognized` is deliberately distinct from `format_not_applicable`. The latter says
"there was nothing of this kind to look for in a file of this format" — a claim that requires
knowing the format. When the format itself could not be determined, that claim cannot be made,
so every code reports `unknown/format_unrecognized` instead of a whole vocabulary of confident
"not applicable".

`pdf_name_escape` and `pdf_stream_body_token` are both *positive-observation* blindness: something
was seen, but not in a position that proves what it means. A `#XX` escape means a name may be
spelled in a way the literal scan does not match; a token inside a stream body may be arbitrary
payload rather than a PDF name. In both cases the honest answer is `unknown` — never `clean`, and
never a confident `critical` finding either.

`read_failed` is deliberately distinct from both `malformed_input` and the two ceiling reasons: a
read failure (a real, non-EOF error from the underlying storage, or a truthful empty read where
bytes were expected) proves nothing about the file's own structure -- the bytes were never
examined at all -- so calling it `malformed_input` would be a false claim about the file. It is
also independent of how large `maxBytes`/the time budget are, so it is not one of the two ceiling
reasons either: raising either budget would not fix a read that is failing for an unrelated
operational reason. It therefore keeps its own name in the `reason` field and is never re-labelled
as a ceiling on its way to you.

### Which reasons make a report selective

**Exactly three reasons may set `coverage.complete: false`: `work_ceiling_bytes`,
`work_ceiling_time`, and `read_failed`.** What unites them is not fault but *evidence*: in all
three, some part of the upload was never examined. The two ceilings mean this service chose to
stop inside a budget it could have kept spending; `read_failed` means the bytes could not be
obtained at all. A larger budget would fix the first two and not the third, but the coverage claim
does not ask what would have fixed it — it asks whether anything went unlooked-at.

Before Phase 59, `read_failed` sat with the reasons below, and a report in which *every* check
said `unknown/read_failed` still shipped `coverage.complete: true` — telling you it had examined
in full an upload it had not been able to read at all.

**`malformed_input` is the sharpest exclusion, and it is not an oversight.** "This file is broken"
is a finished, complete answer *about the file*: the analyzer looked, and saw. Nothing further
would be learned by looking again. Compare `read_failed`, where nothing was seen at all. The two
sound similar and are opposite in exactly the way this field cares about.

The remaining reasons stay out on their own terms: `compressed_object_streams`, `pdf_name_escape`,
`pdf_stream_body_token` and `parser_unavailable` are declared limits of *this build*, honestly
stated; `format_not_applicable` and `format_unrecognized` describe applicability rather than
coverage. A structurally `unknown` check does not, by itself, make the report incomplete — the
checker did everything it could and is honestly reporting what it couldn't determine.

This is the actual denial-of-service-relevant distinction Phase 58 exists to make honest: "we
don't know" about one file property is not the same claim as "we stopped looking early because we
were busy." Widening the selective set further would erode it: report `complete: false` for the
common case of a PDF whose objects merely live in an `/ObjStm`, and the word stops carrying
information.

`truncated_checks` lists the codes whose own check was cut short — by an exhausted ceiling or by a
failed read. It is empty when the loss of coverage came from a characteristic rather than a check
(a characteristic key names no code).

## The `text_layer` characteristic (BDPD-01, Phase 59)

For an uploaded PDF the report carries a `text_layer` characteristic and, when the answer is not
known, a paired `text_layer_reason`. The key is **absent** for every other format — it does not
apply, and an inapplicable characteristic is omitted rather than reported as null (INSP-01).

| `text_layer` | Meaning |
|---|---|
| `"present"` | A usable text layer was found. This PDF can be converted to text without OCR. |
| `"absent"` | No usable text layer, **and everything that could hold one was examined**. |
| `null` | Not determined. `text_layer_reason` says why. |

"Usable" is a threshold, not a boolean: the non-whitespace characters inside the string arguments
of PDF's text-showing operators, compared against the same threshold the OCR worker's own guard
uses. Sharing that unit is deliberate — it is what lets the two answers be compared at all.

**The two directions are not symmetric, and the asymmetry is the point.** `"present"` is a positive
observation and is reported whenever enough text was actually seen, even if the rest of the scan was
blind. `"absent"` is a claim about the *whole file* and is reported only when the scan covered
everything: an exhausted work ceiling, a truncated window, an undecodable stream, or an encrypted
document all yield `null` instead. A report will tell you it does not know far more readily than it
will tell you a PDF has no text.

**This is the one check a compressed object stream does not blind.** Everywhere else, `/ObjStm`
forces `unknown` because objects can hide inside it. ISO 32000 §7.5.7 forbids stream objects from
living there, and a text layer lives in content streams — so a report can legitimately say
`uses_compressed_object_streams: true`, mark every risk check `unknown/compressed_object_streams`,
and still answer this one. Those three facts appearing together in one body is correct, not a
contradiction.

**It is advisory.** This report is produced by a bounded in-process scanner; the worker decides at
job time using a different tool (`pdftotext`) reading the whole file, and the two can disagree.
Nothing here pins anything: a `text_layer` verdict never lets a later job skip a check of its own,
and between this report and that job the bytes may not even be the same ones. Plan 59-07 measures
how far apart the two answers actually fall.

## Binding to existing job-creation rejections (D-04)

`handleCreateJob` (`internal/api/handlers.go`) already rejects thirteen classes of
content-validation failure with a `422`/`413` response, predating this findings vocabulary.
`internal/inspect/bindings.go` (`RejectBindings`) declares, for every one of those thirteen
`reason=` log tokens, whether the rejection is a statement about the FILE (a finding code from
the table above) or a statement about the REQUEST/POLICY (an `internal/errcode` value that is not
a finding about the file at all):

| Reason token | Binding |
|---|---|
| `zip_bomb` | finding `zip_bomb_shape` |
| `macro_detected` | finding `ooxml_macro` |
| `encrypted_document` | finding `ole_cfb_encrypted` |
| `legacy_document` | finding `ole_cfb_legacy` |
| `legacy_or_encrypted_document` | finding `ole_cfb_unclassified` |
| `mismatch` | finding `extension_mismatch` |
| `dimension_limit` | finding `image_pixel_bomb_shape` |
| `unrecognized_content` | not a finding — `errcode.invalid_request` (format could not be identified at all) |
| `dimensions_unknown` | not a finding — `errcode.invalid_request` (a failed characteristic extraction, not a risk signal) |
| `engine_size_limit` | not a finding — `errcode.payload_too_large` (server configuration, not file content) |
| `invalid_opts` | not a finding — `errcode.invalid_options` (a property of the request's `opts` field) |
| `opts_not_applicable` | not a finding — `errcode.opts_not_applicable` (a property of the request's `opts` field) |
| `opts_too_large` | not a finding — `errcode.payload_too_large` (a property of the request's `opts` field) |

**Migrating `handleCreateJob` onto this vocabulary is NOT done in this phase.** This section
declares the correspondence and proves it stays complete
(`TestRejectBindingsCoverEveryContentValidationReason` scans `internal/api/handlers.go` itself),
but `handleCreateJob`'s actual rejection code, HTTP statuses, and response bodies are unchanged
byte-for-byte. That migration is deliberately deferred (candidate: Phase 61) — the fail-closed
job-creation path is the single riskiest piece of code in this milestone, and a phase that already
carries Phase 58's own risk does not also get to rewrite it.

## Keeping this document honest

`internal/inspect/doc_sync_test.go` (`TestFindingCodesDocMatchesGoConstants`) parses the code
column of the table above and asserts set equality, in both directions, against
`inspect.AllCodes()`. If you add, remove, or (never) rename a Go constant, that test fails naming
the exact code that drifted — update this document, not the test. The correct response to a
failure of that test is always to fix whichever side (Go or this document) is wrong, never to
relax the assertion.
