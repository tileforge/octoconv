# Phase 72: Engine-Free Contract Foundations - Research

**Researched:** 2026-10-05
**Domain:** Go content sniffers for CAD uploads (STEP/IGES/STL/DXF/DWG), error-code and inspect contracts, measurements JSON contract, one SQL CHECK widening, format aliases
**Confidence:** HIGH for the code-path, sync-test and corpus findings (all read or run this session); MEDIUM for reader behaviour (OCP/ezdxf were NOT run: another agent holds the docker image); the inspect-reporting mechanism is a design decision with a recommendation (see Open Questions 1)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

### Detection: one function, two callers
- **D-01:** There is one CAD detector, `convert.SniffCAD` or similar; the planner names it. It lives next to
  the existing sniffers (`docsniff.go`, `markupsniff.go`, `avsniff.go`), and `handleCreateJob` and
  `detectInspectFormat` call it in the same detector order. This matters because the two endpoints must never
  call one file two different formats; see `detectInspectFormat`'s doc comment in
  `internal/api/inspect_handlers.go`. Content decides. The declared extension is only an extra fail-closed
  gate, the same discipline as the html/latex/markup branches in `handlers.go`.
- **D-02:** Reads are bounded: a head window plus a tail window via `ReadAt`, never a full parse. The head
  reuses `SniffWindowBytes` if it is big enough for the markers; the planner sets the tail size, a few KiB.
  The reads are charged to the inspect `Tracker` the same way existing detection is (Phase 59 D-12).
  Byte-size ceilings from 71-DECISION are NOT enforced here; that is phases 73/76.
- **D-03:** The CAD markers are ASCII and byte-level, with NO UTF-8 validation of the body. This differs from
  the markup envelope (`ValidateSniffPrefix`) on purpose: legacy cp1251/Latin-1 STEP, IGES and DXF files from
  the corpus (`legacy_cp1251.*`, `legacy_latin1.*`) must not be rejected for their encoding. The sniffer also
  never claims an 8-bit encoding for STEP (71-DECISION: STEP carries no code page).
- **D-04: Per-format rules.** The researcher checks each rule against the corpus and the readers' behaviour,
  and records any deviation:
  - **STEP:**
    - The head must contain `ISO-10303-21;` and the `HEADER;` section.
    - A truncated file is one whose tail has no `END-ISO-10303-21;`, ignoring trailing whitespace.
  - **IGES:**
    - Records are fixed 80 columns, with the section letter (S/G/D/P/T) in column 73.
    - `iges_no_newlines.iges` has no line breaks and must be accepted.
    - A truncated file is one whose last record is not `T`, or whose T-record section counts do not match the
      record count implied by the file size.
  - **STL:**
    - Binary means `size == 84 + 50*n`, with n being the uint32 at offset 80.
    - ASCII means the head starts with `solid` and the tail ends with `endsolid`.
    - A file that satisfies BOTH is rejected as ambiguous.
    - A file with a binary header but a size mismatch, which is also not valid ASCII, is truncated.
  - **DXF:**
    - An ASCII DXF is made of group-code/value pairs, starting with `0`/`SECTION`.
    - A truncated file is one whose tail lacks `0`/`EOF`.
    - The binary DXF sentinel `AutoCAD Binary DXF\r\n\x1a\x00` is rejected with its own code.
  - **DWG:** the signature `AC10` plus two digits at offset 0 is rejected as "dwg not supported", never as
    unknown format.

### Error codes
- **D-05:** There are four new synchronous codes, all returned as 422, with the format named in `message`:
  `truncated_cad_file`, `ambiguous_stl`, `binary_dxf_unsupported` and `dwg_unsupported`.
  - The granularity is per failure class, as the D-02 rule in `docs/ERROR-CODES.md` requires: one truncation
    code for STEP, IGES, DXF and binary STL, not one per format.
  - DWG and binary DXF get their own codes because the client action differs (re-export as DXF / ASCII DXF).
  - The codes go into `internal/errcode/errcode.go` and the synchronous table in `docs/ERROR-CODES.md`, which
    grows from 17 to 21 codes, and are covered by the existing doc-sync tests.
  - The `.planning/ROADMAP.md` wording "отдельными стабильными кодами" is satisfied per class.
  - **Reversibility:** one-way. These strings go on the wire and are frozen once released, like
    `quota_exceeded` and `rate_limited`.
- **D-06:** Job creation runs the CAD content check BEFORE the `(source, target)` pair lookup, in the same
  position as the existing html/latex/markup sniffs.
  - While the class is unregistered, a valid STEP still gets `unsupported_conversion`, and a truncated STEP
    gets `truncated_cad_file`.
  - This is how the "wired into the job-creation chain" criterion is proven while nothing is advertised.
    Tests sit at the detector and handler level.
- **D-07:** `POST /v1/inspect` stays report-only, as Phase 58 D-03 requires: it never returns 422 for content.
  - For a valid CAD file it reports the content-detected format.
  - For a rejected one it reports the same verdict through the existing inspect status/finding vocabulary,
    using the same code string as D-05.
  - A file must never be "ok" in inspect and rejected by job creation, or the other way round.

### Differential test against the real readers
- **D-08:** A script, `scripts/cad-sniff-differential.sh` or similar, runs the Go sniffer and the real readers
  over the committed corpus, the synthetic fixtures and the fetched large files.
  - The real readers are OCP STEP/IGES and ezdxf, run in the existing `Dockerfile.cad-measure` image with the
    7.9.3.1.1 pin.
  - STL is judged against the format rule plus an independent parser, numpy-stl style or inline. Adding a pip
    package is a supply-chain item that would need approval, so prefer an inline parser.
  - The script commits a divergence table under `.planning/phases/72-*/evidence/`, plus a golden file that the
    Go test reads.
- **D-09: Divergence policy.**
  - Sniffer rejects but reader accepts, on a real (non-synthetic) file: not allowed. That is a false
    rejection; fix the sniffer.
  - Sniffer accepts but reader rejects: allowed, because a later stage fails the job asynchronously. Each case
    is listed with its reason.
  - A Go test asserts that the sniffer's verdicts on the committed corpus equal the golden verdicts, so drift
    in either direction fails CI.
- **D-10:** The generator's output is committed before any code that judges it (71 D-05 protocol). The
  truncated, ambiguous-STL, binary-DXF and DWG-signature fixtures come from extending the deterministic
  `scripts/cad-corpus-gen.py` into `internal/convert/testdata/cad/synthetic/`, with self-check markers.

### Measurements schema and vocabularies (MEAS-05)
- **D-11:** The schema is identified by a string field `"schema": "octoconv.measurements/v1"`.
  - A breaking change bumps v1 to v2. Additive optional fields do not bump it.
  - The validator rejects an unknown `schema`.
  - **Reversibility:** one-way once phase 74 emits it to clients.
- **D-12: Units vocabulary.**
  - Units are `mm`, `cm`, `m`, `inch`, `foot`, `micrometre` and `unspecified`.
  - `unspecified` replaces the draft's `unknown`, to match the REQUIREMENTS wording in MEAS-03.
  - The unit source (provenance) is one of `declared` (the file text or DXF `$INSUNITS`), `client_assumed`
    (MEAS-04) or `none`, which pairs with `unspecified`.
  - The doc marks which values were seen in the 71 corpus (mm, inch, m) and which were not.
- **D-13: Null-reason vocabulary.**
  - Volume and area use `no_solids`, `open_shell`, `invalid_solid`, `negative_volume` and
    `stl_not_watertight`.
  - `not_applicable_2d` covers DXF volume and area.
  - The codes that were not observed in 71 stay in the vocabulary and are documented as "not observed on
    corpus; OCP repairs orientation / reads open shell as 0 solids".
- **D-14:** The Go validator lives in a small package, `internal/measurements` or similar; the planner
  chooses.
  - It rejects `volume: 0` and `area: 0` unless backed by a closed solid. In practice it rejects 0 entirely,
    because 71 never produced a legitimate 0.
  - It rejects NaN and ±Inf anywhere, any value outside the vocabularies, a null without a reason code, and a
    reason code next to a non-null value.
  - The bbox must be finite with min ≤ max. 71 saw ±1e100 from `BRepBndLib.Add_s`, so huge sentinel-sized
    values are rejected; the researcher sets the bound.
- **D-15:** The vocabularies are documented in `docs/MEASUREMENTS.md` (new). A Go test parses the doc tables
  and compares them to the code constants, the same pattern as `doc_sync_test.go` and the ERROR-CODES sync.

### Output role `measurements`
- **D-16:** The `measurements` role is added three times over:
  - the `jobs.OutputRoleMeasurements` constant;
  - a new migration `0018_*` that widens `job_outputs.role` CHECK to include `'measurements'`, following the
    `0015_operation_and_output_roles.sql` style;
  - `handleGetJob`, which keeps presigning ONLY the primary output as `download_url`, plus a test proving a
    measurements row is never chosen.

  **Reversibility:** one-way. The migration widens a CHECK constraint, and undoing it needs a down-migration
  with data checks.

### Aliases and non-advertisement
- **D-17:** `NormalizeFormat` gains `stp`→`step` and `igs`→`iges`, and the same aliases go into the frontend
  copy in `frontend/src/formats.ts` with its test. A test pins the Go and TS alias tables equal, if a sync
  test pattern exists; otherwise the planner adds one.
- **D-18:** Guard tests stay green and get CAD assertions: `AllEngines()` golden, `/v1/formats` shows no
  `cad` pair, the README classes and the Pre57/58 golden pinning.

### Claude's Discretion
- Package and function names, the exact tail-window size, and the bbox magnitude bound, with the researcher
  measuring it on the corpus.
- How the differential script reuses `scripts/cad-measure/common.sh` and the 71 image tags.

### Deferred Ideas (OUT OF SCOPE)
- Input byte ceilings and operational caps (triangles, entities, NAUO, MINSERT): phases 73 and 76, using the 71-06 numbers. Those numbers are being re-measured after Codex P1 on PR #32.
- Render time guard for stacked duplicates: phase 76.
- Reviewed todos, not folded (score 0.2, keyword-only match): the amd64 preflight check, the /proc grant for other-process entries, soffice socket removal, and sweep tests touching host /tmp. None of them is in the phase 72 domain.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| CADIN-01 | step/stp/iges/igs/stl/dxf recognised by content, fail-closed, by one logic at job creation and in `POST /v1/inspect`; legacy 8-bit not rejected for encoding | Detector order (create 285-495, inspect 615-808) with the exact insertion point; the pure `FromWindows` function shape; all 8-bit corpus files (`legacy_cp1251.*`, `legacy_latin1.*`) accepted by the probe; aliases |
| CADIN-02 | truncated STEP/IGES/DXF, STL passing both ASCII and binary, and binary DXF rejected 422 with separate codes; sniffer-vs-reader divergence closed by a differential test | Per-format rule table verified on 54 real/synthetic files plus 47 derived; mutation results; four-code blast radius; differential script mechanics and the image-hash trap |
| CADIN-03 | dwg by `AC10xx` signature rejected with its own code, not as unknown format | DWG rule, STL-header collision pitfall, `dwg_unsupported` wiring |
| MEAS-05 | versioned measurements JSON, closed documented vocabularies held equal to code by a test, validator rejecting volume 0 / NaN / Inf / out-of-vocabulary, role `measurements` (enum, CHECK migration, `handleGetJob`) | Validator design, bbox bound, doc-sync pattern, migration 0018 text, `handleGetJob` already selects by role |
</phase_requirements>

## Summary

The codebase already contains every structural pattern this phase needs, and the phase is almost entirely additive. `handleCreateJob` runs a fixed detector chain (`Sniff` 12 bytes, ZIP container, source-gated html/latex/md, OLE-CFB, `SniffVideo`, `SniffAudio`) and `detectInspectFormat` mirrors it. The CAD branch belongs between the md/rst/org branch and `IsOLECFB` in both. Binary STL has no magic bytes, so the branch must be gated on the declared extension, and it must run BEFORE `SniffVideo`/`SniffAudio`, whose 512 KiB peek is the most expensive step and could mis-read float data. `handleGetJob` already selects `download_url` by role, so the `measurements` role needs only a constant, a migration and a test.

The one non-obvious blocker is how `POST /v1/inspect` reports a rejected CAD file "with the same code string". Adding four codes to `inspect.AllCodes()` (the closed finding vocabulary) would add four checks to EVERY inspect report and change the frozen `pre59_inspect_pdf.golden.json`, whose suite forbids regeneration and says "stop and escalate". The established precedent for "I recognised this and declined to finish" is a public sentinel in `Report.Format` (`zip_malformed`, `zip_read_failed`, `zip_directory_too_large`) with `unknown` checks and a documented contract test. Using the four errcode strings as sentinels keeps the goldens byte-identical, makes `availableOperations` return an empty list by construction (no registered pair has such a `From`), and needs no change to `AllCodes()`. This is the recommendation; the alternative needs a user decision.

The detection rules in D-04 were run against all 54 files available here (19 public + 18 synthetic + 17 fetched) and 47 derived files: every one is classified correctly and none is falsely rejected. The probe also found five rule refinements that D-04 as written gets wrong or leaves open (STL ASCII tail has a name, STL needs the extension gate, IGES needs CRLF and trailing-whitespace tolerance, DWG collides with binary-STL header text, `Dockerfile.cad-measure` image tag hashes `scripts/cad-measure/*.py`). Reader behaviour on truncated/odd files was NOT measured; the differential script must produce it.

**Primary recommendation:** one pure function `convert.SniffCADWindows(head, tail []byte, size int64, declared string) CADResult` over already-read windows (the `FromPrefix` pattern), a `ReadCADWindows(io.ReaderAt, size)` helper for the create path, rejections returned as a typed `CADRejection` that `internal/api` maps to the four `errcode` constants; report rejections in inspect as public sentinel formats equal to those constants; put the reader helper script OUTSIDE `scripts/cad-measure/`; keep `AllEngines()`, `/v1/formats`, README untouched.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| CAD content detection (pure decision over windows) | `internal/convert` | — | Sits beside `docsniff.go`/`markupsniff.go`/`avsniff.go`; no I/O in the decision, so both endpoints share it (D-12 pattern, `LooksLikeHTMLFromPrefix`) |
| Bounded reads (head+tail `ReadAt`) | `internal/api` (create) / `detectInspectFormat` (inspect) | `internal/convert` helper | inspect already holds a 1 MiB window and a `Tracker`; create has only `multipart.File` |
| Rejection to wire code | `internal/api` | `internal/errcode` | `convert` must not import `errcode`; one mapping table in `api`, one declaration of each string in `errcode` |
| Inspect verdict reporting | `internal/api` (`inspect_handlers.go`) | `internal/inspect` (reasons only) | Sentinel formats live in `inspect_handlers.go`; no new inspect codes |
| Measurements schema + validator | new `internal/measurements` | `docs/MEASUREMENTS.md` | Pure Go, no engine; doc-sync test as in `internal/convert/doc_sync_test.go` |
| `measurements` role | `internal/db/migrations` (CHECK) | `internal/jobs` (const), `internal/api` (selection, already role-based) | DB CHECK is the authority; Go const names one value |
| Format aliases | `internal/convert` (`NormalizeFormat`) | `frontend/src/formats.ts` | Hand-mirrored today; no sync test exists |
| Differential run | `scripts/` (host shell + in-image Python) | `internal/convert/testdata/cad` (golden), `.planning/.../evidence` (table) | Needs OCP/ezdxf, so it cannot be a Go test; the Go test consumes its golden |

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go standard library (`bytes`, `encoding/binary`, `encoding/json`, `io`, `math`) | go 1.26.4 per `go.mod:3`; local toolchain is go1.27.1 | Sniffers, validator | Matches every other sniffer in `internal/convert`; no new dependency |
| `cadquery-ocp-novtk` | 7.9.3.1.1 | STEP/IGES reader for the differential | Pinned in `scripts/cad-measure/requirements-ocp-7.9.3.1.1.txt`, decision `ocp_pin` in 71-DECISION |
| `ezdxf` | 1.4.4 | DXF reader for the differential | Pinned in `scripts/cad-measure/lock.sh` (`ezdxf==1.4.4`) |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `numpy` | 2.4.6 (already in the image) | STL independent parser | Only through the existing `cad_mesh.read_stl` already baked at `/opt/cad-measure`; do not add anything |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Sentinel formats for inspect | 4 new `inspect.Code` values | Changes every inspect report; breaks the frozen Pre59 golden; needs user escalation |
| Hand-parsed IGES record walk | Reading the whole file | Violates D-02 (bounded reads); size arithmetic already works |
| numpy-stl / trimesh | inline parser | Supply-chain approval (D-08); `cad_mesh.read_stl` already implements the same rule |

**Installation:** none. No package is added to Go, npm or the Python image.

**Version verification:** no external package is recommended, so no registry lookup applies. The pins above were read from `scripts/cad-measure/lock.sh` (`COMMON_INPUT`, `OCP_NEW=7.9.3.1.1`) and `71-DECISION.md`. [VERIFIED: scripts/cad-measure/lock.sh:11-17]

## Package Legitimacy Audit

No external package is installed by this phase (Go stdlib, existing image contents, existing frontend dependencies). Audit not applicable.

**Packages removed due to [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none

## Architecture Patterns

### System Architecture Diagram

```
POST /v1/jobs (file, target)                       POST /v1/inspect (file)
        |                                                  |
  ParseMultipartForm -> FormFile                    ParseMultipartForm -> FormFile
        |                                                  |
  source = NormalizeFormat(ext)                     declared = NormalizeFormat(ext)
  (stp->step, igs->iges)                            (stp->step, igs->iges)
        |                                                  |
  convert.Sniff(12B)  --png/jpg/pdf/mp4--> detected  convert.Sniff(12B) --> detected
  ZIP container (PK)  --docx...-------------^        ZIP container (PK)  --------^
  html / latex / md-rst-org (declared-gated)         window = 1 MiB read; ValidateSniffPrefix ->
        |                                              html / latex / md-rst-org (declared-gated)
        |                                                  |
  ===== NEW: CAD branch =====                        ===== NEW: CAD branch =====
  if detected=="" && declared in {step,iges,         if detected=="" && declared in {step,iges,
     stl,dxf,dwg}:                                      stl,dxf,dwg}:
    head,tail = ReadCADWindows(file,size)               head = window[:4096]; tail = ReadAt (charged
    res = SniffCADWindows(head,tail,size,src)             to Tracker)
        |                                                res = SniffCADWindows(...)  <- SAME function
   rejection -> log reason=<code>,                   rejection -> detected = sentinel (== errcode string)
     writeError(422, errcode.X, msg)                 format    -> detected = "step"/"iges"/"stl"/"dxf"
   format    -> detected = res.Format                   |
        |                                           IsOLECFB(window) -> SniffVideo -> SniffAudio
  IsOLECFB -> SniffVideo -> SniffAudio                   |
        |                                           dispatchInspectFamilies (CAD: no analyzer, empty)
  detected=="" -> 422 invalid_request                extensionMismatch family (sentinel -> unknown/<reason>)
  detected != source -> 422 invalid_request          availableOperations(detected) -> [] (no pair has From=sentinel;
        |                                              step/iges/stl/dxf unregistered)
  IsKnownOperation(convert) ok
  EngineForOp(convert, step, png) -> !ok -> 422 unsupported_conversion   (class unregistered)
```

### Recommended Project Structure
```
internal/convert/
├── cadsniff.go            # SniffCADWindows (pure), ReadCADWindows, CADRejection, window constants
├── cadsniff_test.go       # corpus table test + mutation tests + golden test
internal/measurements/
├── measurements.go        # Schema const, vocab consts + All*(), Validate([]byte)
├── measurements_test.go
├── doc_sync_test.go       # parses ../../docs/MEASUREMENTS.md
internal/db/migrations/0018_output_role_measurements.sql
internal/convert/testdata/cad/
├── synthetic/             # + rejection fixtures (see Pitfall 8)
└── sniff-golden.tsv       # name, group, sha256, sniff_format, sniff_code, reader, reader_accepts, policy, note
scripts/
├── cad-sniff-differential.sh      # host driver (docker run), NOT under scripts/cad-measure/
└── cad-sniff-reader.py            # in-image helper, bind-mounted read-only
docs/MEASUREMENTS.md
.planning/phases/72-*/evidence/72-sniff-divergence.txt
```

### Pattern 1: One pure decision over already-read windows
**What:** `SniffCADWindows` does no I/O. Create reads two small windows; inspect passes the 1 MiB window it already has plus a tail it reads and charges. Same shape as `LooksLikeHTMLFromPrefix` / `IsOLECFBFromPrefix`.
**When to use:** always; this is how D-01's "never two different formats for one file" is made structural.
**Example:** see Code Examples.

### Pattern 2: Rejection is a typed value, mapping lives in `api`
**What:** `convert` returns `CADRejectTruncated | CADRejectAmbiguousSTL | CADRejectBinaryDXF | CADRejectDWG`. `internal/api` holds `map[convert.CADRejection]string` to `errcode` constants and a test that every rejection is mapped and every mapped code is `errcode.IsKnown`. `inspectPublicSentinelFormats` is built from the same four `errcode` constants, so "same code string" is true by construction, not by a test.
**Why:** `TestNoLiteralErrorCodesInAPISource` forbids literal codes at `writeError` call sites; `convert` importing `errcode` would invert the layering. [VERIFIED: internal/api/errcode_envelope_test.go:305-312]

### Anti-Patterns to Avoid
- **Running the CAD check on `ValidateSniffPrefix`'s output:** it rejects NUL and invalid UTF-8, which would reject every binary STL and every legacy-8-bit file. Use the raw window, exactly like `IsOLECFBFromPrefix(window)` does. [VERIFIED: internal/api/inspect_handlers.go:774-780, comment "IsOLECFB works on RAW bytes"]
- **A catch-all STL fallback not gated on the declared extension:** the probe classified a `.dxf` with a BOM, and 1000 bytes of `A`, as `truncated_cad_file` through the binary-STL size mismatch. Binary STL has no magic, so it may only be tried when declared == stl.
- **Reading the tail with the multipart cursor:** use `ReadAt` only; `handleCreateJob` re-stitches `rest` from `Sniff`'s cursor and a `Read` on `file` would corrupt the stored upload (T-31-02 class). [VERIFIED: internal/api/handlers.go:410-420 comment on `ReadAt` never disturbing the cursor]
- **Adding `Code` values to `inspect.AllCodes()`** (see Common Pitfalls 1).
- **Putting the reader helper in `scripts/cad-measure/`** (Pitfall 6).

## Findings by research ask

### 1. `handleCreateJob` detection order and where CAD fits

Verbatim order, with current line numbers in `internal/api/handlers.go` [VERIFIED: grep + Read this session]:

| Step | Line | Condition | Sets |
|------|------|-----------|------|
| `Sniff(file)` 12-byte table (png,jpg,webp,heic,tiff,mp4,mov,avi,pdf) | 285 | always | `detected`, `rest` |
| ZIP container (`PK\x03\x04`) via `ReadAt` + `SniffContainer` | after 285 | `detected==""` | `detected` |
| html | 347 | `detected=="" && source=="html"` | `detected="html"` |
| latex window + `LooksLikeLaTeXFromPrefix` | 359, 380 | `source=="latex"` | `latexPrefix`, `detected` |
| md/rst/org `LooksLikeText` | after 380 | declared in md/rst/org | `detected=source` |
| **NEW: CAD** | insert here | `detected=="" && declared in CAD set` | `detected` or 422 |
| `IsOLECFB` -> `ClassifyCFB` 422s | 409 | `detected==""` | 422 or fallthrough |
| `SniffVideo(rest)` (mkv/webm) | 466 | `detected==""` | `detected`, rebinds `rest` |
| `SniffAudio(rest)` | 486 | `detected==""` | `detected`, `rest` |
| unrecognized -> 422 `invalid_request` | 495 | `detected==""` | |
| declared != detected -> 422 `invalid_request` | 500 | | |
| `IsKnownOperation` -> 422 `unsupported_operation` | 527 | | |
| `EngineForOp` -> 422 `unsupported_conversion` | 538 | | |

Quoted verbatim, the two lines that make the plan work while the class is unregistered:

```
	engine, ok := convert.Default.EngineForOp(operation, detected, target)
	if !ok {
		writeError(w, http.StatusUnprocessableEntity, errcode.UnsupportedConversion,
			"unsupported conversion: "+detected+" -> "+target)
```
[VERIFIED: internal/api/handlers.go:538-542]

Consequences for the plan:
- A valid STEP named `x.step`/`x.stp` gets `detected="step"`, equal to `source` once `stp` is aliased, so it proceeds to `unsupported_conversion: step -> png`. D-06 holds with no extra code.
- The CAD branch must use `file.ReadAt` only; it must NOT rebind `rest`.
- Place it before `IsOLECFB`: a binary STL could otherwise reach `SniffVideo`/`SniffAudio` first.
- `convert.Sniff` runs first, so a PNG renamed `.step` is `detected="png"` and gets the existing mismatch 422; the CAD branch is never reached.
- New `log.Printf("content validation rejected: ... reason=<token>")` lines are scanned by `TestRejectBindingsCoverEveryContentValidationReason`; every token needs a `RejectBindings` entry and a FINDINGS.md row (Pitfall 2).
- The pre58 golden cases (`in.png`/`in.docx`/`in.doc`/`in.jpg`) are all non-CAD declared extensions, so a gated branch cannot change their bytes. [VERIFIED: internal/api/pre58_pinning_test.go:283-345]

### 2. How `detectInspectFormat` mirrors it and how inspect reports verdicts

`detectInspectFormat` (inspect_handlers.go:606) order: `Sniff(fresh())`, ZIP via `ReadAt`, then ONE window read (`readSniffWindow`, `SniffWindowBytes` = 1 MiB, `tr.Consume(len(window))`), inside which `ValidateSniffPrefix` gates html/latex/md and `IsOLECFBFromPrefix(window)` runs on raw bytes, then `SniffVideo(fresh())`, `SniffAudio(fresh())`, then `sniffReadFailed -> inspectSniffReadFailedFormat`. The CAD branch goes between the `if prefix, textShaped := ...` block and the `IsOLECFBFromPrefix` line (lines 764-780), outside the `textShaped` condition. A tail `ReadAt` failure must set `sniffReadFailed = true`, not be treated as "not CAD" (the PR9R25/R27 rule documented in the same function). The tail read is charged with `tr.Consume`.

How inspect reports a verdict today, from `internal/inspect`:
- Status: `StatusClean = "clean"`, `StatusNotApplicable = "not_applicable"`, `StatusUnknown = "unknown"` [VERIFIED: internal/inspect/status.go:15-27].
- Reasons: `format_not_applicable`, `format_unrecognized`, `compressed_object_streams`, `pdf_name_escape`, `pdf_stream_body_token`, `parser_unavailable`, `malformed_input`, `read_failed`, `work_ceiling_bytes`, `work_ceiling_time`; only the last three flip `coverage.complete` [VERIFIED: internal/inspect/status.go `AllReasons`, `makesReportIncomplete`].
- Finding codes: a closed set of 14, pinned by `TestAllCodesCount` (`want 14`) and `TestAllCodesOrderIsDeclarationOrder`, enumerated in `docs/FINDINGS.md`, and every report must carry exactly one finding-or-check per code (`ValidateReport`).
- Public sentinel formats: `Report.Format` already carries non-format values for "recognised, then declined": `zip_directory_too_large`, `zip_read_failed`, `zip_malformed`; declared in `inspectPublicSentinelFormats` and held to `docs/openapi.yaml` by `TestInspectSentinelFormatsMatchTheContract`. [VERIFIED: internal/api/inspect_handlers.go:158-162; internal/api/inspect_offer_spec_sync_test.go TestInspectSentinelFormatsMatchTheContract]

**Why new finding codes are blocked.** `docs/openapi.yaml` does not enumerate finding codes (no match for `pdf_javascript`), so openapi is not the obstacle. The obstacle is `pre59_inspect_pdf.golden.json`: it contains `metadata_present` and `ole_cfb_unclassified` check rows, and the suite states "Everything else is frozen ... composition and ORDER of checks ... stop and escalate rather than editing this suite or regenerating its golden bytes", with `git rev-list --count HEAD -- internal/api/testdata/pre59_inspect_pdf.golden.json # must print 1`. Four extra `Code`s add four rows to every report. [VERIFIED: internal/api/pre59_pinning_test.go:1-62; internal/inspect/findings_test.go:10-12]

**Recommended mapping (sentinel route):**

| Sniffer result | `Report.Format` | extension_mismatch check | other checks |
|----------------|-----------------|--------------------------|--------------|
| valid STEP/IGES/STL/DXF | `step`/`iges`/`stl`/`dxf` | clean when declared == detected (needs the alias), finding `extension_mismatch` otherwise | all `not_applicable/format_not_applicable` (no CAD analyzer) |
| truncated | `truncated_cad_file` | `unknown` / `malformed_input` | `not_applicable` |
| ambiguous STL | `ambiguous_stl` | `unknown` / `malformed_input` | `not_applicable` |
| binary DXF | `binary_dxf_unsupported` | `unknown` / `parser_unavailable` | `not_applicable` |
| DWG | `dwg_unsupported` | `unknown` / `parser_unavailable` | `not_applicable` |

None of `malformed_input`/`parser_unavailable` flips coverage, which is correct: the bytes were examined. `availableOperations` returns an empty list for a sentinel because it only matches `p.From == detected` over registered pairs [VERIFIED: internal/api/estimate.go:309-331]. This also keeps a future registered `step -> png` pair from being offered by inspect for a truncated STEP (the "never ok in inspect, rejected by create" rule survives phase 73).

### 3. Which tests pin sync, and what each new code/role forces

| Pin | File | Forces on this phase |
|-----|------|----------------------|
| errcode doc table | `internal/errcode/doc_sync_test.go` `TestErrorCodesDocMatchesGoConstants` | 4 rows in `docs/ERROR-CODES.md` sync table; first cell must match `^\|\s*\`([a-z][a-z0-9_]*)\`` |
| errcode openapi enum | `internal/errcode/openapi_sync_test.go` | 4 lines `            - truncated_cad_file` etc. (12-space indent, block sequence, inside `Error.code` enum at docs/openapi.yaml:115-132) |
| bindings vs handlers.go | `internal/inspect/bindings_test.go` `TestRejectBindingsCoverEveryContentValidationReason` | each new `reason=` token in a `content validation rejected` log line needs a `RejectBindings` key; a key with no log line fails as "dead binding" |
| bindings well-formed | same file `TestRejectBindingsAreWellFormed` | exactly one of `FindingCode`/`ErrCode`; `ErrCode` must satisfy `errcode.IsKnown`, so the consts must exist first |
| FINDINGS.md binding table | same file `TestFindingsDocListsEveryRejectBinding` | 4 rows `\| \`token\` \| ... \|` under `## Binding to existing job-creation rejections (D-04)` |
| envelope closed vocabulary | `internal/api/errcode_envelope_test.go` `TestEveryErrorResponseCarriesKnownCode`, `TestNoLiteralErrorCodesInAPISource` | use `errcode.X` at every `writeError`; no literals |
| sentinel contract | `internal/api/inspect_offer_spec_sync_test.go` `TestInspectSentinelFormatsMatchTheContract` | each of the 4 sentinel strings must appear in docs/openapi.yaml (extend the `InspectReport.format` description) |
| AllEngines | `internal/convert/convert_test.go` `TestAllEnginesCoversEveryEngineClass` | stays `len == 9`, no `cad`; add an assertion that `Default.Classes()`/`OperationClasses()` hold no step/iges/stl/dxf/dwg `From` |
| README classes | `internal/convert/readme_sync_test.go` | untouched; stays green because `AllEngines()` is unchanged |
| /v1/formats | `pre58_pinning_test.go` `TestPre58GoldenListFormats`; `formats_handlers_test.go` `TestListFormats_*` | untouched; add one test that the response body names no CAD format |
| Pre57/58/59/60 goldens | `internal/api/testdata/pre5*_*.golden.json` | byte-frozen; the sentinel route and the gated CAD branch change none of them |
| role CHECK | `internal/jobs/output_role_test.go` | `legal := []string{"primary", "report", "preview", "thumbnail"}` (line 143) needs `"measurements"`; the bogus-role rejection test must still pass; needs `DATABASE_URL` (skips otherwise) |
| inspect closed vocabulary | `internal/inspect/findings_test.go` | untouched on the sentinel route |

Other facts that matter: `docs/openapi.yaml` deliberately leaves `outputs[].role` an open string ("The role is not a closed enum here on purpose"), so no role enum edit; `ERROR-CODES.md` prose says "Seventeen codes" (not machine-checked) and `errcode.go:16` says "closed at seventeen codes": both must be edited to twenty-one by hand. [VERIFIED: docs/openapi.yaml:197-203; internal/errcode/errcode.go:16; docs/ERROR-CODES.md:92]

### 4. Per-format rules checked on every corpus file

Method: a throwaway Go program (kept in the session scratchpad, not in the repo) implementing the candidate rules via `ReadAt` with a 4 KiB tail and a 1 MiB head was run over `internal/convert/testdata/cad/{public,synthetic}` (37 committed files), `$HOME/.cache/octoconv/cad-corpus/fetched` (17 files, read-only) and `.../scaled` (47 derived files, read-only). Result: **101 of 101 files classified correctly, 0 false rejections, 0 misclassifications.** [VERIFIED: probe run 2026-10-05]

Facts by class (the "EOL" column was measured on the first 4 KiB):

| Class | Files | Markers observed | EOL | Verdict |
|-------|-------|------------------|-----|---------|
| STEP public | 2020_corner_bracket-Corner, LM8UU, LinearSlide-MGN15x-Block, SC8UU, V-Slot-20x20-100mm | start `ISO-10303-21;\nHEADER;`, end `END-ISO-10303-21;\n` | LF | accept |
| STEP public (NIST) | nist-d2mi-827-9999-906, nist_ctc_01, nist_ctc_05, nist_stc_06 | same, `\r\n`; nist_stc_06 has a blank line between `ISO-10303-21;` and `HEADER;`; d2mi-906 has `/* ... */` after `HEADER;` | CRLF | accept |
| STEP synthetic | assembly_nauo4, asymmetric_l_bracket, degenerate_plate, degenerate_rod, open_shell, reversed_solid, single_face, units_inch, legacy_cp1251 (17 bytes >= 0x80 in head), legacy_latin1 (5) | same | LF | accept, 8-bit not rejected |
| STEP fetched | freecad-emergency-button-disk, -faulhaber-2342l, -sprocket-ansi-z114 (LF); freecad-mit-e-vent-t-slot, nist-d2mi-827-9999-904-rev-c, nist-ftc-09, nist-stc-09, nist-stc-10 (CRLF; stc-10 has a blank line after `HEADER;`) | same | mixed | accept |
| IGES | surfaces_only.iges 12393 B, iges_no_newlines.iges 12240 B, freecad-mk8-hotend.iges 3058317 B, freecad-festo-valve-5-2.igs 11321208 B | col 73 `S`, seq `      1` or `0000001` on record 1; last record `T`; T counts `S1G4D98P49` match 153 records | LF stride 81 (3 files), none stride 80 (no_newlines) | accept; counts reconcile: 153 x 81 = 12393, 153 x 80 = 12240, 37757 x 81, 139768 x 81 |
| STL ASCII | stl_ascii_box.stl 3005 B | head `solid box`, last line `endsolid box` (**has a name**), uint32 at 80 = 538976288 (four spaces) | LF | accept as ASCII; formula 84 + 50 x 538976288 != 3005 |
| STL binary | nasa-hubble (n=8590), nasa-jwst (28096), stl_binary_solid_header (n=12, header starts `solid`), 7 fetched NASA files | `size == 84 + 50n` on all | | accept as binary |
| DXF | 3dsolids, acad_table_with_blk_ref, columns_R2007, dimension_in_nested_blocks, hatches_1, hatches_2, uncommon (LF); houses_of_parliament_georeferenced (CRLF); blocks_minsert, comments_999 (starts `999`; CRLF), legacy_cp1251 (6 bytes >= 0x80), legacy_latin1 (2) | head `  0` / `SECTION`; tail `  0` / `EOF` + final newline | LF or CRLF | accept |
| Derived (scaled) | 47 files: STEP copies/NAUO up to 205 MB, STL ASCII/tile up to 209 MB, DXF replicate/MINSERT up to 207 MB, IGES-from-STEP up to 977 MB | same markers | | accept (reads only head+tail) |

Real files with 8-bit bytes in STEP/IGES/DXF do not exist in the corpus; `legacy_*` are synthetic (PROVENANCE: "Not found in open form"). A real ASCII STL above 3 KB does not exist either (71-DECISION: "реального ASCII STL выше 3 КБ нет").

Mutation results (same probe, mutated copies of corpus files; this is what shows where D-04 as written is incomplete):

| Mutation | Result |
|----------|--------|
| STEP/DXF/IGES/STL cut by 17 bytes, 81 bytes, 5000 bytes, half | all `truncated_cad_file` |
| STEP/DXF cut by 1 byte (final newline only) | accepted: the marker is intact, trailing whitespace is ignored by rule |
| IGES cut at a record boundary (T record dropped, or 5 records dropped) | `truncated_cad_file` (last record section `P`) |
| IGES converted to CRLF, or CRLF with final newline removed, or LF with no final newline | accepted (stride 82 / 81) |
| IGES / STEP / DXF / ASCII STL with trailing spaces and CRLF appended | accepted when whitespace is stripped before the size arithmetic (the first probe version rejected IGES; fixed) |
| Any format + 1 trailing NUL, or + 512 NULs | `truncated_cad_file` (NUL is not whitespace) |
| Binary STL cut by 1 byte, or + 1 trailing byte, or n inflated by 1 | `truncated_cad_file` (strict equality) |
| ASCII STL cut mid-facet | `truncated_cad_file` |
| ASCII STL with `endsolid` and no name, or all upper case | accepted |
| `STEP` with `HEADER;` renamed | not classified as truncated unless handled explicitly (see deviation 4) |
| `.dxf` with a UTF-8 BOM prepended, 1000 x `A`, DWG `AC1.40` | **fell through to the binary-STL size-mismatch branch and was labelled `truncated_cad_file`** (deviation 2) |
| `AC1027` + zeros | `dwg_unsupported` |
| 84-byte file of zeros | accepted as binary STL with n = 0 (zero triangles) |
| Crafted "binary STL with `solid` header and `endsolid` in the last 9 bytes" | NOT ambiguous, because the last "line" began with NUL bytes (Pitfall 4) |

**Deviations the planner must apply to D-04:**
1. **STL ASCII tail.** "tail ends with `endsolid`" is wrong for the only real ASCII file (`endsolid box`). Rule: the last non-blank line, after trimming trailing whitespace, starts with `endsolid` (case-insensitive); the head after optional BOM/whitespace starts with `solid` (case-insensitive).
2. **STL is declared-gated and last.** Try DWG, binary DXF, STEP, IGES, ASCII DXF by magic first. Run the STL rules only when the declared format is `stl`. Garbage declared `.dxf`/`.step`/`.iges` with no magic must fall through to the existing `unrecognized_content` 422, not `truncated_cad_file`. Content-only STL detection is impossible because binary STL has no magic.
3. **IGES.** Infer stride from the head (`head[80]=='\n'` -> 81; `'\r','\n'` -> 82; else 80), strip trailing whitespace from the tail before computing `records = (trimmedSize + (stride-80)) / stride` with an exact-fit check, require the last 80 bytes of the stripped tail to have `T` in column 73 and `S/G/D/P` letters at columns 1, 9, 17, 25, and require `S+G+D+P+1 == records`. Head rule: column 73 is `S` and columns 74-80 parse as sequence 1. Compressed (`C` flag) and binary (`B` flag) IGES are not covered and fall through as unrecognized; say so in `docs/ERROR-CODES.md`.
4. **STEP.** A file that starts with `ISO-10303-21;` (after optional BOM and whitespace) but lacks `HEADER;` in the head or `END-ISO-10303-21;` in the tail is `truncated_cad_file`; there is no other code that fits and "starts like STEP, structure incomplete" is truncation.
5. **DXF.** Skip leading `999` comment pairs, then require group code `0` with value `SECTION` (trim spaces and `\r`); bound the head scan to the 4 KiB head; tail rule: after trimming whitespace the last line is `EOF` and the line before it trims to `0`. Strip no BOM (decide by the reader, Open Question 2).

The probe's Go skeleton is reproduced in Code Examples.

### 5. DWG rule and its collision

`AC10` + two ASCII digits at offset 0 covers AC1002 to AC1032 (R2.5 to R2018). Dotted ancient signatures (`AC1.40`, `AC2.10`) do not match and fall through; list them as out of scope. **Collision:** a binary STL header is free text and `AC1027 bracket` is a plausible part name. Without a precedence rule a real binary STL is falsely rejected as DWG, which D-09 forbids. Rule: when declared == stl and the binary size formula holds, STL wins over the DWG signature. A stronger DWG check (bytes 6-10 are NUL in R13-R2018) is `[ASSUMED]` and not needed if the precedence rule is used.

### 6. The differential script: what it needs from `common.sh` and the image

- Tag: `cad_image_tag OCP VARIANT` returns `octoconv-cad-measure:ocp<OCP>-<variant>-<12 hex>` where the hex is `shasum` over `Dockerfile.cad-measure`, `scripts/cad-measure/requirements-*.txt`, **`scripts/cad-measure/*.py`**, `vtk-keeplist.txt` and `scripts/cad-measure/negative/*`. [VERIFIED: scripts/cad-measure/common.sh `cad_inputs_hash`]
- **Trap:** adding or editing any `.py` under `scripts/cad-measure/` changes the tag, so the next `cad_build_image` rebuilds the image (network, minutes), and the other agent's running measurements derive a tag that no longer exists locally. Put the reader helper at `scripts/cad-sniff-reader.py` and bind-mount it: `docker run --rm --network none --user nobody -v "$REPO/scripts:/lib:ro" -v "$CORPUS:/corpus:ro" "$TAG" /opt/venv/bin/python -I -B /lib/cad-sniff-reader.py ...`. This is the same pattern `scripts/cad-corpus-gen.py` documents (`-v "$PWD:/src:ro"`).
- Reuse: `. scripts/cad-measure/common.sh`, `TAG="$(cad_build_image 7.9.3.1.1 pruned)"` (returns the tag with no build when the image exists), `cad_require_native "$TAG"` (refuses emulated arch), `cad_evidence_header "$TAG" 2 4g` for the HDR block, `CAD_CACHE_DIR` for the fetched/scaled directories. Use `pruned`, the shipping variant (final decision in 71-DECISION: `venv-pruned`); `cad-corpus-inventory.sh` uses `full`, which is a different tag.
- How to run the readers read-only: `STEPControl_Reader().ReadFile(path)` / `IGESControl_Reader().ReadFile(path)` compared with `IFSelect_RetDone`, then `TransferRoots()` and `OneShape().IsNull()` exactly as `cad_mesh.py` does; record parse-level and transfer-level separately. DXF: `ezdxf.readfile` (strict) AND `ezdxf.recover.readfile` (what `cad_dxf.py` uses and what phase 75 will run); record both. STL: import the already-baked `/opt/cad-measure/cad_mesh.py` and call `read_stl(path)` (`sys.path.insert(0, "/opt/cad-measure")`; the module imports only stdlib at top level, numpy inside the function). Its rule is `len(data) == 84 + 50*n` else an ASCII `vertex` regex, which is the independent parser D-08 asks for and needs no new package.
- The 71 evidence says every real STEP/IGES/DXF in the corpus is read by OCP/ezdxf (no read failure on 55 STEP/IGES files except a 100000-instance timeout and a 493 MiB IGES OOM; one DXF `rc=3` for a file with nothing drawable), so D-09 false rejections are not expected. [CITED: 71-DECISION.md "Отказы чтения"; evidence/71-04-helpers.txt SUMMARY]
- `$HOME/.cache/octoconv/cad-corpus/fetched` exists here with all 17 manifest files; `scaled` has 47. Reading fetched/scaled files through OCP is slow (up to 18 MB STEP committed-size, hundreds of MB derived); make `scaled` opt-in.
- Golden placement. No Go test reads `.planning/` (`.planning/` is gitignored and force-tracked), so the golden the Go test reads must live under `internal/convert/testdata/cad/` (e.g. `sniff-golden.tsv`), and the human divergence table under `.planning/phases/72-*/evidence/`. Evidence files need `git add -f`; `71-*` evidence is tracked that way. [VERIFIED: `git ls-files .planning/phases/71-evaluation-gate/evidence`; `.gitignore:7 /.planning/`]
- Fetched files are absent in CI: the Go test should check a fetched row only when the file exists under `$OCTOCONV_CAD_CACHE/fetched` and its sha256 matches `scripts/cad-corpus-manifest.tsv`, else `t.Skip` that row.
- Do NOT run docker: this research did not; the reader columns of the table are therefore unmeasured.

### 7. Migration numbering and how 0015 widened the CHECK

- Next number is `0018`: files present end at `0017_correlation_id.sql`. `db.Migrate` reads `migrations/` sorted by name and applies each in a transaction, recording `schema_migrations.version`. [VERIFIED: ls internal/db/migrations; internal/db/db.go:183-196]
- 0015 added the column with an inline unnamed CHECK, which Postgres names `job_outputs_role_check` (confirmed by `TestOutputRoleRejectsValueOutsideAllowlist`, which asserts the error mentions that name):

```
ALTER TABLE job_outputs
    ADD COLUMN role text NOT NULL DEFAULT 'primary'
        CHECK (role IN ('primary', 'report', 'preview', 'thumbnail'));
```
[VERIFIED: internal/db/migrations/0015_operation_and_output_roles.sql:61-63]

- The widening follows the `jobs_engine_check` pattern of `0016_tectonic_engine.sql`: `ALTER TABLE job_outputs DROP CONSTRAINT job_outputs_role_check; ALTER TABLE job_outputs ADD CONSTRAINT job_outputs_role_check CHECK (role IN ('primary', 'report', 'preview', 'thumbnail', 'measurements'));`. Additive, no backfill, no index. `jobs_engine_check` already lists `'cad'` (0016 line: `CHECK (engine IN ('image', 'document', 'av', 'cad', 'archive', ...))`), so no engine migration is needed. [VERIFIED: internal/db/migrations/0016_tectonic_engine.sql]
- Constant: `jobs.OutputRoleReport = "report"` sits at internal/jobs/jobs.go:96 with a doc comment saying the CHECK is the authority and the constant "names one of them, it does not define the set"; add `OutputRoleMeasurements = "measurements"` beside it. `Repo.AddOutputWithRole` already exists (repo.go:646) and rejects an empty role, so phase 74 can write the row without schema or repo work. `MarkDone` counts rows `WHERE job_id = $1 AND role = 'primary'` (repo.go:278), so a `measurements` row cannot be mistaken for a second primary.
- Live-DB tests (`newTestRepo`) skip when `DATABASE_URL` is unset; Postgres was not reachable on `:5434` during this research, so the migration and role tests need the compose Postgres up to count as run.

### 8. `handleGetJob` output selection

`handleGetJob` presigns every output and sets `download_url` only when `o.Role == outputRolePrimary` (`outputRolePrimary = jobs.OutputRolePrimary`), and emits `outputs[]` only when `len(outs) > 1`:

```
				if o.Role == outputRolePrimary {
					downloadURL = url
					hasPrimary = true
				}
```
[VERIFIED: internal/api/handlers.go:1285-1288, 43-50]

So D-16's `handleGetJob` item needs a test, not a code change. Template: `TestGetJob_DownloadURLResolvesToPrimaryNotOrdinalZero` (handlers_test.go:2069) with `fakeStorage` mapping object key to URL. Add a variant with `{Ordinal: 0, Role: "measurements"}` first and the primary at ordinal 1, asserting `download_url` is the primary's URL and that `outputs[]` carries `role: "measurements"`. Also add a worker-side variant next to `internal/worker/webhook_role_test.go` (the terminal webhook selects the primary by role at worker.go:1010-1026).

### 9. Aliases and the frontend

- `NormalizeFormat` is a `switch` with `jpeg->jpg`, `tif->tiff`, `htm->html`, `tex->latex` (internal/convert/convert.go:182-201); `frontend/src/formats.ts` hand-mirrors it with the same four cases and a comment "The two tables MUST be kept in sync". **No Go<->TS sync test exists** (grep for `formats.ts` in Go, TS, JSON and shell finds only the two files themselves). The planner must add one. [VERIFIED: grep this session]
- Recommended shape: turn the Go `switch` into an unexported `formatAliases` map (behaviour-preserving), and add a Go test next to `readme_sync_test.go` that parses `case '<from>':\s*return '<to>';` out of `frontend/src/formats.ts` and compares the set both ways with `formatAliases`, failing with the offending alias named. Add `case 'stp': return 'step'; case 'igs': return 'iges';` to the TS function and cases to `formats.test.ts` ("folds the rest of the alias table"). Run `cd frontend && npx vitest run` (vitest is installed in `frontend/node_modules/.bin`).
- Ripple of the alias in Go: `NormalizeFormat` is also applied to targets (`handlers.go:196`) and to the inspect `declaredFormat`; `stp` as a target would become `step`, which is harmless because no pair is registered. `.stp` uploads currently normalise to `stp`, so inspect would report `extension_mismatch` for a correct STEP: the alias is required for the "clean" mismatch check.

### 10. Bbox magnitude bound

Evidence: legitimate magnitudes in the 71 corpus are small. Largest real 3D bounding-box diagonal 1006.54 mm (`freecad-sprocket-ansi-z114.step`); largest derived (stacked) 7224.55 (`step-nauo-...-n10000.step`); the largest DXF coordinate is 2.695e+04 (`houses_of_parliament_georeferenced.dxf`, `bbox2d:1.769e+04+4967+2.695e+04+1.614e+04`). The sentinel is +-1e100 (diagonal 3.46e+100) from `BRepBndLib.Add_s` on `V-Slot-20x20-100mm.step`. The 71 helper already encodes a bound: `FINITE_LIMIT = 1e12`, above which `cad_mesh.py` treats a diagonal as unbounded and falls back to per-face boxes. [VERIFIED: evidence/71-04-helpers.txt FINDING line; evidence ROW lines; scripts/cad-measure/cad_mesh.py:41]

**Recommendation: reject any bbox coordinate with `|v| > 1e12`** (and any `NaN`/`+-Inf`). It is the number already applied to real files by the helper, it leaves 8 orders of magnitude over the largest georeferenced coordinate seen (a UTM northing in millimetres is about 1e10), and it sits 88 orders below the sentinel. Document it in `docs/MEASUREMENTS.md` and reference the helper constant. `[ASSUMED]` that no legitimate file exceeds it; nothing measured above 3e4.

### 11. Measurements validator: inputs for design (planner owns the shape)

- Existing producer output keys, from `cad_mesh.py`: `solids`, `closed_solids`, `open_solids`, `invalid_solids`, `volume`, `area`, `volume_null_reason`, `bbox{min,max}`, `unit_text`, `triangles`; STL path (`stlstats`) emits `volume_null_reason` in `{stl_not_watertight, negative_volume}`. [VERIFIED: scripts/cad-measure/cad_mesh.py:282-346, stlstats]
- Python's `json.dump` emits bare `NaN`/`Infinity` by default, and Go's `encoding/json` rejects them as a syntax error; a numeric overflow (`1e999`) is also a decode error. A validator that takes raw bytes gets "NaN/Inf rejected" at parse, but should still check struct inputs with `math.IsNaN`/`math.IsInf` and return a named error. [ASSUMED: Go json behaviour on bare NaN; confirm with a table test]
- Recommended rules beyond D-14, all fail-closed and cheap: `schema` exactly `octoconv.measurements/v1`; `DisallowUnknownFields` on producer-side validation (additive fields then require a code and doc change, matching "additive optional fields do not bump" for consumers); volume and area strictly `> 0` and require `closed_solids >= 1`; `null` requires a reason from the vocabulary and a non-null forbids one; unit `unspecified` iff source `none`; `client_assumed` and `declared` require a real unit; per-format reason subsets (step/iges: `no_solids`, `open_shell`, `invalid_solid`, `negative_volume`; stl: `stl_not_watertight`, `negative_volume`; dxf: only `not_applicable_2d`, and `not_applicable_2d` forbidden elsewhere); dxf bbox has 2 coordinates per corner, others 3; `min <= max` per axis; `solids`/`closed_solids` integers `>= 0`.
- Vocabulary doc parsing: reuse `codeRowPattern` (first cell lowercase `[a-z][a-z0-9_]*`). All unit codes (`mm`, `cm`, `m`, `inch`, `foot`, `micrometre`, `unspecified`), sources (`declared`, `client_assumed`, `none`) and reasons match it. The schema string contains `.` and `/`, so it cannot be a table first cell; pin it as a named phrase like `guaranteeBoundaryPhrase` in `internal/convert/doc_sync_test.go`. [VERIFIED: internal/convert/doc_sync_test.go:30-60]
- Add `docs/MEASUREMENTS.md` to the README docs table (README.md:470-473 lists ERROR-CODES/FINDINGS) and `docs/README.md`.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Doc-to-code vocabulary sync | A new comparison style | The `extractTableCodes`/`diffSets` pattern in `internal/convert/doc_sync_test.go` and `internal/errcode/doc_sync_test.go` | Bidirectional, names the offender, already reviewed twice |
| Reading a window at an offset | A buffered reader or `Seek` | `io.ReaderAt.ReadAt` with the `n == len || err == io.EOF` rule from `readSniffWindow` | Never disturbs the multipart cursor; handles short reads |
| STL independent parser | numpy-stl / trimesh | `cad_mesh.read_stl` already in the image | No supply-chain item (D-08) |
| Inspect "declined" reporting | New finding codes or a new response field | Public sentinel `Format` + `unknown` checks (PR9 precedent) | Keeps Pre59 golden and `AllCodes()` untouched |
| Fixture generation | Hand-edited binary files | Extend `scripts/cad-corpus-gen.py` with `FIXTURES`/`EXPECTED`/self-check entries | D-10, deterministic, self-checking |
| NaN/Inf detection | String scanning the JSON | `math.IsNaN/IsInf` on decoded floats plus json decode errors | Exact and testable |

**Key insight:** every one of these has a precedent in the repository that a reviewer already knows; the risk in this phase is review surface, so reusing the exact shapes is cheaper than any clever alternative.

## Runtime State Inventory

Not a rename/refactor/migration of existing strings. One schema widening (CHECK) and additive code; no stored data carries any new string. Stored data: none, verified by the migration being a CHECK widening with no backfill. Live service config: none. OS-registered state: none. Secrets/env vars: none. Build artifacts: none (no new binary). Frozen goldens are test fixtures, not runtime state.

## Common Pitfalls

### Pitfall 1: New inspect finding codes move frozen goldens
**What goes wrong:** adding `truncated_cad_file` etc. to `inspect.AllCodes()` adds four check rows to every report and fails the Pre59 pin (and `TestAllCodesCount`).
**Why it happens:** D-07's wording ("existing status/finding vocabulary") suggests findings.
**How to avoid:** sentinel formats built from the errcode constants; if the user prefers findings, record the Pre59 exception explicitly and escalate (Open Question 1).
**Warning signs:** a diff touching `internal/inspect/findings.go` or `pre59_inspect_pdf.golden.json`.

### Pitfall 2: Every new rejection log token needs a binding and a doc row
**What goes wrong:** `TestRejectBindingsCoverEveryContentValidationReason` and `TestFindingsDocListsEveryRejectBinding` fail, or a binding with no log line fails as dead.
**How to avoid:** one token per rejection, equal to the errcode string, added to `RejectBindings` (as `ErrCode`, because the new codes are not findings) and to the FINDINGS.md binding table in the same commit as the log line. Order the commit: errcode consts first (the well-formed test calls `errcode.IsKnown`).

### Pitfall 3: STL fallback mislabels non-STL garbage as truncated
**What goes wrong:** any declared `.step/.dxf/.iges` file with no magic and 84+ bytes reaches the binary-STL size check and is called `truncated_cad_file` (measured).
**How to avoid:** gate STL on declared == stl, after all magic-based formats; test: `.dxf` with BOM and `.step` of random bytes give `unrecognized_content`, not a CAD code.

### Pitfall 4: A crafted ambiguity fixture can silently not be ambiguous
**What goes wrong:** the natural construction (binary STL, `solid` header, `endsolid` in the last bytes) was NOT flagged because the last line began with NULs.
**How to avoid:** the generator must place `\nendsolid\n` (or `\nendsolid x\n`) at the end of the last triangle record, and its self-check must assert BOTH `size == 84 + 50n` and the ASCII markers (head `solid`, last line `endsolid`). The Go test asserts the verdict `ambiguous_stl`; a fixture that does not trip it fails the generator self-check first.
**Note:** a real ASCII STL cannot satisfy the size formula below roughly 7.5 GB (bytes 80-83 of text are at least 0x09, so n >= 0x09090909 = 151,587,081), so ambiguity is adversarial only.

### Pitfall 5: DWG signature versus binary-STL header text
**What goes wrong:** a real binary STL whose header starts `AC1027...` is rejected as DWG.
**How to avoid:** precedence rule in section 5, with a fixture (`stl_binary_ac10_header.stl`) that must be accepted; this is a D-09 false rejection otherwise.

### Pitfall 6: Putting helper scripts in `scripts/cad-measure/`
**What goes wrong:** the image tag hash changes and a rebuild is triggered; the concurrently running measurement loses its tag.
**How to avoid:** `scripts/cad-sniff-reader.py` + bind mount. Re-derive the tag before and after to prove it did not move: `scripts/cad-measure/common.sh tag 7.9.3.1.1 pruned` must print the same value.

### Pitfall 7: Trailing NUL, BOM and binary-STL trailing bytes are undecided
**What goes wrong:** the probe rejects NUL padding (all text formats), a UTF-8 BOM on DXF, and a binary STL with trailing bytes. If OCP/ezdxf/the numpy parser accept any of these on a REAL file it is a D-09 false rejection.
**How to avoid:** generate one synthetic fixture per case and let the differential run decide; record the outcome in the golden `policy` column. Default recommendation until measured: whitespace-only trailing bytes accepted, NUL and BOM rejected, binary STL strict equality (D-04 text).

### Pitfall 8: New fixtures enter the 71 inventory
**What goes wrong:** `scripts/cad-corpus-inventory.sh` walks `internal/convert/testdata/cad` recursively (`find` inside `count_container`), so rejection fixtures change its committed-group count (37) and `counters.py` runs on broken files. The 71 evidence is static and no test reads it, so nothing fails, but a re-run is no longer comparable.
**How to avoid:** accept it and say so in `PROVENANCE.md` (D-10 puts them in `synthetic/`), or place them in `synthetic/rejected/` and exclude that path in the inventory script. The A5 commit threshold (15 MiB total; committed is 7,038,250 bytes per 71-DECISION) is not at risk: the fixtures are a few KiB each. Also extend `PROVENANCE.md` with one section per fixture in the existing format (`Licence: generated`, `sha256`, `Proves`).

### Pitfall 9: Local green on a different toolchain
**What goes wrong:** `go.mod` pins `go 1.26.4`; the machine has go1.27.1. Project memory records a P1 hidden for four review rounds this way.
**How to avoid:** run every gate with `GOTOOLCHAIN=go1.26.4`; the toolchain is cached (`~/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64`). Baseline on go1.26.4, run this session: `go test ./internal/errcode/ ./internal/inspect/` ok; `go test ./internal/convert/ -run 'TestREADMEEngineClasses|TestAllEngines|TestSanitizeDoc'` ok; `go test ./internal/api/ -run 'TestPre5|TestPre6|TestListFormats|TestInspectSentinel|TestNoLiteral|TestEveryErrorResponse|TestGetJob_Download'` ok.

### Pitfall 10: A tail read failure treated as "not CAD"
**What goes wrong:** a disk-backed temp file whose tail `ReadAt` fails is reported as unrecognized.
**How to avoid:** in inspect set `sniffReadFailed = true` (the PR9R25/R27 rule in `detectInspectFormat`); in create, fail closed to the existing 422 as the latex window read does (`readSniffWindow` error leaves `detected` empty).

## Code Examples

Verified skeleton: this exact decision logic, as a standalone program, produced the corpus and mutation results above. Names are the planner's to change. No comments, per the project rule.

```go
package convert

import (
	"bytes"
	"encoding/binary"
	"io"
	"strconv"
	"strings"
)

type CADRejection string

const (
	CADRejectNone         CADRejection = ""
	CADRejectTruncated    CADRejection = "truncated"
	CADRejectAmbiguousSTL CADRejection = "ambiguous_stl"
	CADRejectBinaryDXF    CADRejection = "binary_dxf"
	CADRejectDWG          CADRejection = "dwg"
)

type CADResult struct {
	Format    string
	Rejection CADRejection
}

const (
	CADHeadBytes = 4096
	CADTailBytes = 4096
)

func IsCADDeclared(format string) bool {
	switch format {
	case "step", "iges", "stl", "dxf", "dwg":
		return true
	}
	return false
}

func ReadCADWindows(r io.ReaderAt, size int64) (head, tail []byte, err error) {
	if size <= 0 {
		return nil, nil, nil
	}
	head = make([]byte, min(size, CADHeadBytes))
	n, err := r.ReadAt(head, 0)
	if err != nil && err != io.EOF {
		return nil, nil, err
	}
	head = head[:n]
	off := max(size-CADTailBytes, 0)
	tail = make([]byte, size-off)
	n, err = r.ReadAt(tail, off)
	if err != nil && err != io.EOF {
		return nil, nil, err
	}
	return head, tail[:n], nil
}

func SniffCADWindows(head, tail []byte, size int64, declared string) CADResult {
	if len(head) >= 6 && bytes.HasPrefix(head, []byte("AC10")) && allDigits(head[4:6]) &&
		!(declared == "stl" && binarySTLSize(head, size)) {
		return CADResult{"dwg", CADRejectDWG}
	}
	if bytes.HasPrefix(head, []byte("AutoCAD Binary DXF\r\n\x1a\x00")) {
		return CADResult{"dxf", CADRejectBinaryDXF}
	}
	lead := bytes.TrimLeft(bytes.TrimPrefix(head, []byte{0xEF, 0xBB, 0xBF}), " \t\r\n\f\v")
	if bytes.HasPrefix(lead, []byte("ISO-10303-21;")) {
		if !bytes.Contains(head, []byte("HEADER;")) ||
			!bytes.HasSuffix(trimRightSpace(tail), []byte("END-ISO-10303-21;")) {
			return CADResult{"step", CADRejectTruncated}
		}
		return CADResult{"step", CADRejectNone}
	}
	if res, ok := sniffIGES(head, tail, size); ok {
		return res
	}
	if res, ok := sniffDXF(head, tail); ok {
		return res
	}
	if declared == "stl" {
		return sniffSTL(head, tail, size)
	}
	return CADResult{}
}

func sniffIGES(head, tail []byte, size int64) (CADResult, bool) {
	if len(head) < 80 || head[72] != 'S' {
		return CADResult{}, false
	}
	if seq, err := strconv.Atoi(strings.TrimSpace(string(head[73:80]))); err != nil || seq != 1 {
		return CADResult{}, false
	}
	stride := int64(80)
	switch {
	case len(head) > 80 && head[80] == '\n':
		stride = 81
	case len(head) > 81 && head[80] == '\r' && head[81] == '\n':
		stride = 82
	}
	trimmed := trimRightSpace(tail)
	size -= int64(len(tail) - len(trimmed))
	if len(trimmed) < 80 {
		return CADResult{"iges", CADRejectTruncated}, true
	}
	rec := trimmed[len(trimmed)-80:]
	if rec[72] != 'T' || rec[0] != 'S' || rec[8] != 'G' || rec[16] != 'D' || rec[24] != 'P' {
		return CADResult{"iges", CADRejectTruncated}, true
	}
	count := func(b []byte) int64 {
		v, _ := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
		return v
	}
	want := count(rec[1:8]) + count(rec[9:16]) + count(rec[17:24]) + count(rec[25:32]) + 1
	if (size+stride-80)%stride != 0 || (size+stride-80)/stride != want {
		return CADResult{"iges", CADRejectTruncated}, true
	}
	return CADResult{"iges", CADRejectNone}, true
}

func sniffDXF(head, tail []byte) (CADResult, bool) {
	lines := bytes.Split(head, []byte("\n"))
	i := 0
	next := func() (string, bool) {
		if i >= len(lines) {
			return "", false
		}
		s := strings.TrimSpace(string(lines[i]))
		i++
		return s, true
	}
	for {
		code, ok := next()
		if !ok {
			return CADResult{}, false
		}
		value, ok := next()
		if !ok {
			return CADResult{}, false
		}
		if code == "999" {
			continue
		}
		if code == "0" && value == "SECTION" {
			break
		}
		return CADResult{}, false
	}
	t := trimRightSpace(tail)
	cut := bytes.LastIndexByte(t, '\n')
	if cut < 0 || strings.TrimSpace(string(t[cut+1:])) != "EOF" {
		return CADResult{"dxf", CADRejectTruncated}, true
	}
	prev := t[:cut]
	if strings.TrimSpace(string(prev[bytes.LastIndexByte(prev, '\n')+1:])) != "0" {
		return CADResult{"dxf", CADRejectTruncated}, true
	}
	return CADResult{"dxf", CADRejectNone}, true
}

func sniffSTL(head, tail []byte, size int64) CADResult {
	isBinary := binarySTLSize(head, size)
	lead := bytes.TrimLeft(bytes.TrimPrefix(head, []byte{0xEF, 0xBB, 0xBF}), " \t\r\n")
	asciiHead := len(lead) >= 5 && bytes.EqualFold(lead[:5], []byte("solid"))
	t := trimRightSpace(tail)
	last := bytes.TrimLeft(t[bytes.LastIndexByte(t, '\n')+1:], " \t")
	asciiTail := len(last) >= 8 && bytes.EqualFold(last[:8], []byte("endsolid"))
	switch {
	case isBinary && asciiHead && asciiTail:
		return CADResult{"stl", CADRejectAmbiguousSTL}
	case isBinary, asciiHead && asciiTail:
		return CADResult{"stl", CADRejectNone}
	case size >= 84 || asciiHead:
		return CADResult{"stl", CADRejectTruncated}
	}
	return CADResult{}
}

func binarySTLSize(head []byte, size int64) bool {
	if size < 84 || len(head) < 84 {
		return false
	}
	return size == 84+50*int64(binary.LittleEndian.Uint32(head[80:84]))
}

func trimRightSpace(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == ' ' || b[len(b)-1] == '\t' || b[len(b)-1] == '\r' || b[len(b)-1] == '\n' || b[len(b)-1] == '\f' || b[len(b)-1] == '\v') {
		b = b[:len(b)-1]
	}
	return b
}

func allDigits(b []byte) bool {
	for _, c := range b {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(b) > 0
}
```

An STL with `size < 84` and no ASCII head returns an empty result (unrecognized), which is right: an 83-byte file is not an STL.

Insertion in `handleCreateJob` (between the md/rst/org branch and `IsOLECFB`):

```go
	if detected == "" && convert.IsCADDeclared(source) {
		head, tail, werr := convert.ReadCADWindows(file, header.Size)
		if werr == nil {
			res := convert.SniffCADWindows(head, tail, header.Size, source)
			if res.Rejection != convert.CADRejectNone {
				code := cadRejectionCode[res.Rejection]
				log.Printf("content validation rejected: client_id=%s filename=%q reason=%s", client.ID, filename, code)
				writeError(w, http.StatusUnprocessableEntity, code, cadRejectionMessage(res))
				return
			}
			detected = res.Format
		}
	}
```

Insertion in `detectInspectFormat`, inside the `default:` case after the `textShaped` block, before `IsOLECFBFromPrefix(window)`:

```go
				if detected == "" && convert.IsCADDeclared(declaredFormat) {
					tail, terr := readCADTail(file, size)
					if terr != nil {
						sniffReadFailed = true
					} else {
						tr.Consume(int64(len(tail)))
						res := convert.SniffCADWindows(window[:min(len(window), convert.CADHeadBytes)], tail, size, declaredFormat)
						switch {
						case res.Rejection != convert.CADRejectNone:
							detected = cadRejectionCode[res.Rejection]
						default:
							detected = res.Format
						}
					}
				}
```

The two call sites differ only in how the windows were obtained and how the verdict is surfaced; the decision is the one function. A table test must feed every corpus and mutation fixture through BOTH `handleCreateJob` and `handleInspect` and assert: inspect `format` is one of the four sentinels if and only if create answered that 422 code; an accepted file gets inspect `format == detected` and create `unsupported_conversion`.

Reader helper for the differential (in-image, read-only, no comments):

```python
import json
import sys

sys.path.insert(0, "/opt/cad-measure")


def read_step(path):
    from OCP.IFSelect import IFSelect_RetDone
    from OCP.STEPControl import STEPControl_Reader
    reader = STEPControl_Reader()
    status = reader.ReadFile(path)
    if status != IFSelect_RetDone:
        return {"parse": 0, "transfer": 0}
    reader.TransferRoots()
    return {"parse": 1, "transfer": 0 if reader.OneShape().IsNull() else 1}


def read_dxf(path):
    import ezdxf
    from ezdxf import recover
    out = {}
    try:
        ezdxf.readfile(path)
        out["strict"] = 1
    except Exception as exc:
        out["strict"] = 0
        out["strict_error"] = type(exc).__name__
    try:
        recover.readfile(path)
        out["recover"] = 1
    except Exception as exc:
        out["recover"] = 0
        out["recover_error"] = type(exc).__name__
    return out


def read_stl(path):
    import cad_mesh
    tri, binary = cad_mesh.read_stl(path)
    return {"parse": 0 if tri is None else 1, "binary": int(binary)}
```

Migration (name per 0015/0016 style):

```sql
ALTER TABLE job_outputs DROP CONSTRAINT job_outputs_role_check;
ALTER TABLE job_outputs ADD CONSTRAINT job_outputs_role_check
    CHECK (role IN ('primary', 'report', 'preview', 'thumbnail', 'measurements'));
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Role as the Go default via `AddOutput` | `AddOutputWithRole` writes the role explicitly | Phase 60 (PR #12 defect) | Phase 74 writes `measurements` without schema or repo work |
| Validate a sniff prefix as UTF-8 text | Raw-window matchers for binary signatures (`IsOLECFBFromPrefix`) | Phase 59 D-12 | CAD must use the raw-window style |
| Per-matcher read caps | One `SniffWindowBytes` window read once | Phase 59 D-12 | CAD head reuses the window in inspect |

**Deprecated/outdated:** `Dockerfile.cad-measure` variants named `nofreeimage` exist in `common.sh` but D-18 chose `record`; use `pruned`.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | OCP `STEPControl_Reader`/`IGESControl_Reader` reject a STEP/IGES truncated at the tail (no `END-ISO-10303-21;`, no T record) | Findings 6, Pitfall 7 | If OCP accepts, the differential table lists them as "sniffer rejects, reader accepts" on synthetic files only (allowed by D-09) but the product claim "truncated" would be a policy, not a reader fact |
| A2 | ezdxf reads binary DXF (so `binary_dxf_unsupported` is a deliberate policy divergence the table must mark) | Findings 4, 6 | If ezdxf cannot read it, the rejection is also reader-backed; no plan change |
| A3 | DWG R13-R2018 headers carry five NUL bytes after the version string | Findings 5 | Only affects an optional stricter check; the precedence rule does not depend on it |
| A4 | Trailing NUL padding and UTF-8 BOM are rare enough in real uploads that rejecting them is acceptable until the reader says otherwise | Pitfall 7 | A false-rejection class on real files; mitigated by the fixtures and policy column |
| A5 | Sentinel `Report.Format` values are an acceptable reading of D-07's "existing inspect status/finding vocabulary" | Summary, Findings 2 | If the user wants new finding codes, Pre59 golden and `AllCodes()` pins need an explicit, escalated exception |
| A6 | No legitimate bbox coordinate exceeds 1e12 | Findings 10 | A false validator rejection of a real measurement; the number is the 71 helper's own limit, nothing measured exceeds 3e4 |
| A7 | 4 KiB head and tail windows are enough for every marker in the wild | Code Examples | Verified on 101 corpus files only; a file with more than 4 KiB of trailing whitespace is rejected as truncated, a file with more than 4 KiB of leading `999` comments is unrecognized |
| A8 | Go `encoding/json` rejects bare `NaN`/`Infinity` and `1e999` | Findings 11 | Validator relies on explicit checks regardless; add a table test to confirm |
| A9 | The per-format reason-subset and unit/source pairing rules are wanted | Findings 11 | They are my recommendation beyond D-14; if unwanted they are simply not implemented |
| A10 | `ezdxf.readfile` strict mode behaviour on a DXF missing `EOF` (accepts or raises) | Findings 6 | Only changes what the divergence table says; the sniffer rule is independent |

## Open Questions

1. **How should inspect carry the D-05 code?**
   - What we know: new `AllCodes()` entries change every report and the frozen Pre59 golden; public sentinel formats are the precedent for "recognised, declined" and are already contract-tested.
   - What's unclear: whether the user reads D-07 as requiring finding codes.
   - Recommendation: sentinel route (this document). If findings are required, escalate the Pre59 exception before planning.
2. **BOM, trailing NUL and binary-STL trailing bytes: accept or reject?**
   - What we know: probe rejects all three; no corpus file has any.
   - What's unclear: whether OCP/ezdxf accept them.
   - Recommendation: add one synthetic fixture each, let the differential decide, record in the golden `policy` column; default to D-04's strict text.
3. **Where do rejection fixtures live?**
   - What we know: D-10 says `synthetic/`; the 71 inventory script recurses over the whole `testdata/cad`.
   - Recommendation: follow D-10, note it in `PROVENANCE.md`, accept that a re-run of the inventory is no longer comparable to the 71 evidence.
4. **Binary DXF and DWG in the divergence policy.**
   - D-09 forbids "sniffer rejects, reader accepts" on real files; binary DXF is a policy rejection ezdxf may read. These fixtures are synthetic, so D-09 allows them, but the table needs a `policy` category so they are not mistaken for sniffer defects.
5. **Should an 84-byte (n = 0) binary STL be accepted at sniff level?** Probe accepts; phase 73's triangle guard would reject later. Recommendation: accept, add a fixture, note it for phase 73.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go pinned toolchain | all Go gates | yes | go1.26.4 cached; default go1.27.1 | set `GOTOOLCHAIN=go1.26.4` |
| Node + vitest | frontend alias test | yes | node v26.10.0; vitest in `frontend/node_modules/.bin` | none needed |
| Postgres (`DATABASE_URL`) | migration 0018 and role tests | no (`:5434` no response; `DATABASE_URL` unset) | n/a | tests skip; bring up the compose Postgres before claiming them run |
| Docker + `octoconv-cad-measure` image | differential script | not probed on purpose | n/a | another agent is using it; do not run docker here. Reader columns stay unmeasured until the run |
| `$HOME/.cache/octoconv/cad-corpus/{fetched,scaled}` | differential + golden rows | yes (17 / 47 files) | n/a | skip rows when absent |
| `python3` on host | none required | yes, but `ezdxf` not installed on host | n/a | run readers only inside the image |

**Missing dependencies with no fallback:** none for planning; the docker run is a gated execution step.
**Missing dependencies with fallback:** Postgres (skips), docker (deferred).

## Validation Architecture

(Optional: `workflow.nyquist_validation` is `false` in `.planning/config.json`; included because the planner needs the test map.)

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go `testing` (stdlib only, no assertion library), vitest 4.1.10 for the frontend |
| Config file | none |
| Quick run command | `GOTOOLCHAIN=go1.26.4 go test ./internal/convert/ ./internal/errcode/ ./internal/inspect/ ./internal/measurements/` |
| Full suite command | `GOTOOLCHAIN=go1.26.4 go test ./... && (cd frontend && npx vitest run)` |

### Phase Requirements to Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| CADIN-01 | corpus (public+synthetic+fetched when present) classified; 8-bit accepted | unit | `go test ./internal/convert/ -run TestSniffCAD` | no, Wave 0 |
| CADIN-01 | create and inspect agree on every fixture | handler | `go test ./internal/api/ -run TestCADCreateInspectAgree` | no, Wave 0 |
| CADIN-02 | four rejection classes + mutation table | unit | `go test ./internal/convert/ -run TestSniffCADRejects` | no, Wave 0 |
| CADIN-02 | sniffer equals golden; no real-file false rejection | unit | `go test ./internal/convert/ -run TestSniffCADGolden` | no, Wave 0 |
| CADIN-02 | errcode/doc/openapi/bindings sync for 4 codes | unit | `go test ./internal/errcode/ ./internal/inspect/` | yes (extend docs) |
| CADIN-03 | DWG signature, STL-header precedence | unit | `go test ./internal/convert/ -run TestSniffCADDWG` | no, Wave 0 |
| MEAS-05 | validator rejects 0, NaN/Inf, out-of-vocab, orphan reason, bbox sentinel | unit | `go test ./internal/measurements/` | no, Wave 0 |
| MEAS-05 | doc tables equal code vocabularies | unit | `go test ./internal/measurements/ -run TestDocMatches` | no, Wave 0 |
| MEAS-05 | role CHECK accepts measurements, rejects bogus | integration | `DATABASE_URL=... go test ./internal/jobs/ -run TestOutputRole` | yes (extend) |
| MEAS-05 | `handleGetJob` never picks measurements as `download_url` | handler | `go test ./internal/api/ -run TestGetJob_DownloadURL` | extend |
| D-17 | Go and TS alias tables equal | unit | `go test ./internal/convert/ -run TestFormatAliasesMatchFrontend`; `npx vitest run` | no, Wave 0 |
| D-18 | nothing advertised | unit | existing Pre58/README/AllEngines tests + a new "no CAD pair in `/v1/formats` and `Classes()`" test | extend |

### Sampling Rate
- **Per task commit:** the quick run command for the touched package
- **Per wave merge:** full suite on `GOTOOLCHAIN=go1.26.4`
- **Phase gate:** full suite green on go1.26.4, `git rev-list --count` equals 1 for the four Pre5x goldens, Postgres-backed tests actually run

### Wave 0 Gaps
- [ ] `internal/convert/cadsniff_test.go` and `cadsniff.go`
- [ ] `internal/measurements/` package, tests, doc-sync test, `docs/MEASUREMENTS.md`
- [ ] generator extension, regenerated `synthetic/` fixtures, `PROVENANCE.md` sections, committed BEFORE the judging code (D-10)
- [ ] `internal/convert/testdata/cad/sniff-golden.tsv` produced by the differential run
- [ ] Go-vs-TS alias sync test
- [ ] Postgres for the role tests

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no (unchanged; `/v1/*` already behind API-key middleware) | existing `auth.Middleware` |
| V3 Session Management | no | n/a |
| V4 Access Control | no (ownership guard in `handleGetJob` untouched) | existing |
| V5 Input Validation | yes | bounded `ReadAt` windows, fail-closed typed rejection, no UTF-8 assumption for binary content, validator on emitted JSON |
| V6 Cryptography | no | n/a |
| V12 File and Resources | yes | window reads charged to the inspect `Tracker`; never full-file buffering |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Parser differential (sniffer accepts what the reader treats differently, or the reverse) | Tampering | differential run against the real readers with a committed golden and a Go test (D-08/D-09) |
| Content/extension confusion | Spoofing | content decides, extension is only a gate, mismatch 422 as today |
| Read amplification by a crafted large file | Denial of service | two 4 KiB `ReadAt`s, no scan of the body; IGES count uses size arithmetic |
| Error text echoing client input | Information disclosure | messages are fixed strings naming only a format constant, never filename or content (`internal/api` rule) |
| Sentinel float bbox (1e100) or NaN reaching clients | Tampering | validator bound 1e12 and finiteness check before the artifact is stored (enforced in phase 74, contract defined here) |
| Binary-STL header text read as another format | Tampering/false rejection | STL-over-DWG precedence, STL gated on declared extension |

## Sources

### Primary (HIGH confidence)
- `internal/api/handlers.go` (lines 40-52, 285-545, 1194-1325), `internal/api/inspect_handlers.go` (lines 1-165, 560-810, 1240-1579), `internal/api/estimate.go:309` read this session
- `internal/inspect/{findings,status,report,bindings}.go` and `findings_test.go`, `bindings_test.go` read this session
- `internal/errcode/{errcode.go,doc_sync_test.go,openapi_sync_test.go}`, `docs/ERROR-CODES.md`, `docs/FINDINGS.md` (binding table lines 230-264), `docs/openapi.yaml` (Error enum 100-135, InspectReport 525-600) read this session
- `internal/api/pre58_pinning_test.go`, `pre59_pinning_test.go`, `golden_pinning_test.go`, `errcode_envelope_test.go`, `inspect_offer_spec_sync_test.go`, `formats_handlers.go` read this session
- `internal/convert/{convert.go (NormalizeFormat, AllEngines),sniff.go,markupsniff.go,doc_sync_test.go,readme_sync_test.go,convert_test.go (AllEngines test)}` read this session
- `internal/db/migrations/0015`, `0016`, `0017`, `internal/db/db.go` (Migrate), `internal/jobs/{jobs.go,repo.go (AddOutputWithRole, MarkDone count),output_role_test.go}` read this session
- `frontend/src/formats.ts`, `formats.test.ts`, `package.json` read this session
- `scripts/cad-measure/{common.sh,cad_mesh.py,cad_dxf.py,lock.sh}`, `Dockerfile.cad-measure`, `scripts/cad-corpus-gen.py`, `scripts/cad-corpus-inventory.sh`, `internal/convert/testdata/cad/PROVENANCE.md` read this session
- `.planning/phases/71-evaluation-gate/71-DECISION.md`, `evidence/71-04-helpers.txt`, `.planning/{REQUIREMENTS,ROADMAP,STATE}.md`, `72-CONTEXT.md` read this session
- Probe run over `internal/convert/testdata/cad/{public,synthetic}`, `$HOME/.cache/octoconv/cad-corpus/{fetched,scaled}` (throwaway Go program in the session scratchpad; results quoted above)

### Secondary (MEDIUM confidence)
- none

### Tertiary (LOW confidence)
- Training knowledge about OCP/ezdxf reader behaviour and the DWG header layout, tagged `[ASSUMED]` in the log (A1-A3, A8, A10)

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH, no new packages; pins read from lock files
- Architecture: HIGH, insertion points and tests read from source; the inspect-reporting choice is a recommendation with a named alternative
- Pitfalls: HIGH for the ones reproduced by the probe (STL fallback, ambiguity fixture, IGES whitespace, image-hash trap); MEDIUM for reader-dependent ones
- Reader behaviour (truncated/BOM/NUL on OCP and ezdxf): LOW until the differential run, by design

**Research date:** 2026-10-05
**Valid until:** 2026-11-04 (stable code paths); re-check the pin list and `scripts/cad-measure/` hash inputs if phase 71 follow-ups (PR #32 re-measurement) land first
