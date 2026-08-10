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
| `compressed_object_streams` | the file's structure (D-02a: PDF `/ObjStm` blindness) | No |
| `parser_unavailable` | this build | No |
| `malformed_input` | the file | No |
| `work_ceiling_bytes` | **this service's own load** | **Yes** |
| `work_ceiling_time` | **this service's own load** | **Yes** |

Only the last two reasons may ever mark a report `coverage.complete: false`. A structurally
`unknown` check (parser blindness, an inapplicable format, a malformed file) does not, by itself,
make the report incomplete — the checker did everything it could and is honestly reporting what
it couldn't determine. This is the actual denial-of-service-relevant distinction Phase 58 exists
to make honest: "we don't know" about one file property is not the same claim as "we stopped
looking early because we were busy."

## Keeping this document honest

`internal/inspect/doc_sync_test.go` (`TestFindingCodesDocMatchesGoConstants`) parses the code
column of the table above and asserts set equality, in both directions, against
`inspect.AllCodes()`. If you add, remove, or (never) rename a Go constant, that test fails naming
the exact code that drifted — update this document, not the test. The correct response to a
failure of that test is always to fix whichever side (Go or this document) is wrong, never to
relax the assertion.
