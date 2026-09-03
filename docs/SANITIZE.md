# OctoConv Sanitize

**Status: introduced in milestone v1.13 Phase 60 (SANI-01 … SANI-05).**

This document is the canonical statement of what the `sanitize` operation removes from a file,
what it destroys as a side effect, how each removal is proven, and — the part most worth reading
— what the operation does **not** claim.

Go constants are the source of truth. `internal/convert/findings.go` declares both vocabularies;
`internal/convert/doc_sync_test.go` mechanically proves this document and those constants never
drift apart, in both directions. See "Keeping this document honest" at the bottom.

## Relationship to docs/FINDINGS.md

Two documents, two different verbs.

- `docs/FINDINGS.md` answers **"what was found in the file"**. It reports on a file nobody has
  changed.
- `docs/SANITIZE.md` (this document) answers **"what was removed from the file"**. It reports on
  a file that has been rebuilt.

The same construct appears in both under different codes — `pdf_javascript` is a finding,
`javascript` is a removed threat class. They are not duplicates and neither is derived from the
other: a file can be inspected and never sanitized, and a file can be sanitized without anyone
having inspected it first.

## Scope of the guarantee

Read this section before reading the tables.

The service removes the **enumerated classes of construct** listed in the first table below, and
verifies their absence in the output using an implementation that did not perform the removal.
That is the entire claim.

Specifically, the service:

- **does not determine whether a document was malicious.** Content Disarm and Reconstruction
  removes carriers. It does not analyse what any code would have done, and it never forms an
  opinion about intent.
- **does not detect malicious content outside the enumerated classes.** A construct this document
  does not list is a construct the operation does not look for. Absence from the report means
  "not enumerated", not "not present".
- **is not a substitute for antivirus scanning.** It performs no signature matching, no heuristic
  analysis, and no behavioural detection of any kind.
- **preserves colour-management data in images, including its vendor-defined parts.** An ICC
  profile (JPEG `APP2`, PNG `iCCP`) and the Adobe colour-transform marker (`APP14`) are kept,
  because dropping them changes how the picture renders: a wide-gamut image displayed as sRGB, a
  CMYK image with inverted channels. An ICC profile is a structured container with vendor tags, so
  a preserved profile can carry bytes this service did not enumerate. That is a boundary of the
  guarantee and it is stated here rather than left to be discovered.

This is a description of the method rather than a legal disclaimer, and it is written that way on
purpose. A disclaimer reads like something to skip; a description of the method reads like
something to understand — and understanding it is what lets a client decide correctly whether
`sanitize` is the right tool for their situation.

The report the operation returns carries codes and no verdict. There is no `clean` boolean and no
summary sentence, because either would assert something the operation never tested.

## Bounds on the input

Sanitize refuses documents past a size the service is willing to spend time on, and the bound is
stated here because a client hitting it needs to know it exists and is not a fault in their file.

**Page ceiling (`PROBE_MAX_PAGES`, default 200).** For PDFs, sanitize cost is per *page* and
linear — roughly 2.0 seconds per full-page scan, measured in the worker container at 2 CPU / 1 GiB.
Byte size is close to irrelevant: a 219 KB scanned document costs more than a 2.2 MB one. The
engine timeout (`PROBE_ENGINE_TIMEOUT`, 600s) was derived from that cost for a 200-page document,
so 200 is not a second, independently chosen number — it is the same document bound, now enforced
rather than assumed.

A PDF over the ceiling fails with the error code **`too_many_pages`**, on the **first** attempt —
no retries are spent, because a document that is too long is too long every time.

The refusal happens in the worker, before conversion starts, and **not** at upload time. An
earlier design also refused at admission, so that nothing was stored; it was removed because a page
count cannot be derived reliably from untrusted bytes in the API process. A PDF's page tree is a
tree, so a long document declares several different counts — the root's total and each subtree's —
and a reader that does not resolve the catalog's own `/Pages` reference cannot tell which is which.
Seven distinct ways a *valid* document could be refused were found before that one was, and the
refusal that mattered least was the one it could actually make: it answered confidently only for
short documents, where the ceiling does not apply.

So an over-long document costs one upload and one queue slot before it is refused. What that buys
is that a valid document is never refused by a guess.

The value is **provisional** in one specific respect. The 2.0 s/page cost is measured; "200 pages
is a generous document bound" is reasoned, from a corpus chosen by hand rather than from what
clients submit. An operator with real traffic should set it from that. The one number it may never
exceed is not a preference: at 2.0 s/page, the 900-second stale-job reconciler puts an absolute
ceiling at 450 pages.

Non-PDF sanitizable formats have no page ceiling, because they have no per-page cost — OOXML
sanitization rewrites a package's parts and images are re-encoded once. A bound without a cost
model behind it would be a number invented to look thorough.

## Threat classes removed

Eight classes, closed. Source: `internal/convert/findings.go` (`AllThreatClasses()`).

The last column is what makes this table a proof rather than a promise: for every class, the
implementation that confirms its absence in the output is **not** the implementation that removed
it (SANI-04). A verifier blind in the same place as the sanitizer proves only its own blindness.

| Code | What it means | What is removed | Verified by |
|------|---------------|-----------------|-------------|
| `javascript` | Embedded JavaScript that most readers execute automatically | `/JavaScript` name-tree entries and `/S /JavaScript` actions | pdfcpu object walk (sees inside `/ObjStm`) plus a byte scan of the output |
| `action_dict` | The document asked for something to happen without the reader choosing it | `/OpenAction` on the catalog and `/AA` additional-action dictionaries anywhere | pdfcpu object walk plus a byte scan of the output |
| `launch_action` | The document asks the reader to start an external program | `/Launch` actions | pdfcpu object walk plus a byte scan of the output |
| `vba_macro` | A VBA project inside an OOXML package | The whole `vbaProject.bin`-class part, plus its content-type override and every relationship pointing at it | `unzip` enumerates the parts and `file`/libmagic classifies each by content; a signature check covers containers too degenerate for libmagic to name |
| `ole_object` | An embedded OLE compound-file object inside an OOXML package | Any part whose bytes begin with the OLE compound-file signature, under any name | Same as `vba_macro` — the verifier reads content, never part names |
| `embedded_file` | An attachment travelling inside the document | `/EmbeddedFiles` name trees and `/Filespec` entries with an `/EF` stream; in OOXML, every part a relationship names with an embedded-content type (`package`, `oleObject`, `aFChunk`, `attachment`), wherever that part lives and whatever it is called | pdfcpu object walk plus a byte scan of the output; for OOXML, `unzip` + libmagic over the surviving parts |
| `external_reference` | A reference to a resource outside the file, which makes the document phone home when opened | `/URI` actions; OOXML relationships with `TargetMode="External"` | pdfcpu object walk plus a byte scan of the output |
| `document_metadata` | Who made this, when, with what, and where | PDF `/Info` values and the XMP packet whole; image EXIF, GPS and IPTC | pdfcpu reads `/Info` by value; the output is scanned for `xpacket`, for the engine's own signature, and for EXIF/XMP/IPTC markers |

**Why `vba_macro` is removed as a whole part rather than by editing module source.** VBA stomping
keeps benign-looking source text and hides behaviour in the compiled p-code beside it. A sanitizer
that reads source and keeps the part is defeated by construction, so nothing here reads VBA source
at all.

**Why an OOXML attachment is found by relationship, not by folder.** Office puts embedded files
under `*/embeddings/`, but OOXML does not require that: a part is what a relationship says it is,
and a package relationship may target any part name. An attachment at `word/media/attached.pdf` is
still an attachment, and is dropped as one.

**Why the metadata class covers three unrelated formats.** PDF `/Info`, an XMP packet and image
EXIF are entirely different carriers, and the client-visible fact is identical across all of them.
Classes are closed by meaning, not by spelling.

## What the method destroys

Seven capabilities, closed. Source: `internal/convert/findings.go` (`AllLostCapabilities()`).

These are **costs, not threats**, and the report keeps them in a separate list for that reason. A
lost form is not a found attack, and one heading over both would say something false about both.

| Code | What stops working | Why the method cannot keep it |
|------|--------------------|-------------------------------|
| `fillable_form_fields` | The form still displays but can no longer be filled in | The document is rebuilt by an interpreter that draws marks and has no concept of a form field |
| `bookmarks_outline` | The bookmark/outline tree is gone | PostScript has no outline construct, so nothing survives the round trip to rebuild from |
| `tagged_accessibility_structure` | Screen readers lose the document's tagged structure | Same: the tag tree does not survive an interpreter that draws marks |
| `document_level_annotations` | Comments, review markup and stamps are gone | Same |
| `vba_macro_functionality` | Macros in the package no longer run | The carrying part is removed whole; this is `vba_macro` seen from the client's side |
| `embedded_ole_objects` | Embedded charts, equations and attached workbooks are gone | Their parts are OLE containers and are removed whole |
| `document_properties` | Title, author and company no longer travel with the file | The properties part is removed |

The PDF losses are all one consequence: rebuilding the document through PostScript is what makes
"what the interpreter did not draw does not exist" true by construction instead of by hope. That
construction is the reason the removals can be believed, and this list is its price.

**The image path loses nothing.** Metadata segments are removed structurally and the compressed
image data is copied byte for byte, so the pixels that come out are the pixels that went in. Its
`lost_capabilities` list is empty because there is no loss, not because none was measured.

That claim is about the rendered picture, not only about the bytes, which is why colour management
is preserved (see the scope section above). Dropping the ICC profile would have left the scan data
identical and the image visibly different — a losslessness claim true in a sense no client means.

A client choosing between `sanitize` and not sanitizing should make that choice knowing this
table. That is why it is a table and not a footnote.

**A colour profile is kept whole or not at all.** A JPEG's ICC profile may be split across several
APP2 chunks; the sanitizer decides on the complete set — every chunk present, sequence numbers
consistent, and the assembled data matching the size the profile declares of itself. A set that
does not hold together is dropped entirely. Half a profile is not a smaller profile: the file would
still decode, the report would still say the profile was preserved, and the colours would be wrong,
which is a worse outcome than an image with no profile at all.

**A PDF whose own structure contradicts itself is refused, not guessed at.** Stream
boundaries are taken from each stream's declared `/Length` and confirmed against the bytes
that follow it. If a length cannot be resolved, or the file's bytes disagree with what it
declares, the job fails with `malformed_input` rather than proceeding on a best guess — a
guessed boundary is how a sanitizer ends up rewriting the inside of a stream while every
cross-reference offset still lines up, which is corruption that nothing downstream looks
wrong enough to catch.

**What this operation will never return is an empty package.** If sanitizing an OOXML file would
remove its main part — `word/document.xml`, `xl/workbook.xml` or `ppt/presentation.xml` — the job
FAILS rather than delivering the remains. Such a package unzips cleanly and opens as nothing, and
"every threat removed" is not a claim worth making about a file with no document in it.

## The report

The job's second output, with `role=report`, carries JSON assembled only from the codes above:

```json
{
  "format": "pdf",
  "removed": ["javascript", "action_dict", "launch_action", "embedded_file",
              "external_reference", "document_metadata"],
  "lost_capabilities": ["fillable_form_fields", "bookmarks_outline",
                        "tagged_accessibility_structure", "document_level_annotations"],
  "verified": ["javascript", "action_dict", "launch_action", "embedded_file",
               "external_reference", "document_metadata"],
  "guarantee_scope": "docs/SANITIZE.md#scope-of-the-guarantee"
}
```

`removed` and `verified` are separate fields because they are separate claims. "We removed
JavaScript" describes our code path; "we confirmed no JavaScript is present in the output"
describes the bytes the client is about to receive. A client cannot tell those apart if they share
a field.

There is no field a client can read as a verdict. That absence is deliberate and is enforced by a
test.

## Consumer contract

**Consumers MUST treat an unrecognized code in any of these lists as an unknown entry and MUST NOT
throw, crash, or fail closed on it.** Later phases may add classes. Those additions are
contractually non-breaking only if every consumer already tolerates a value it has not seen.

Do not generate a closed enum or a closed TypeScript union from the current lists. A closed type
looks more correct at review time and turns every future addition into a client outage.

## Keeping this document honest

`internal/convert/doc_sync_test.go` proves three things mechanically:

1. the set of codes in the threat table equals `AllThreatClasses()`, in both directions — a code
   here that Go does not declare is a promise of something that does not happen, and a code Go
   declares that is missing here is silence about something that does;
2. the same equality for the capability table and `AllLostCapabilities()`;
3. that neither this document nor the Go vocabulary contains an affirmative claim that a file is
   safe, clean, virus-free or free of malicious content.

If any of those fails, the fix is to correct whichever side is wrong. It is never to relax the
test.
