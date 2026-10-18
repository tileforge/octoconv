# Phase 72: Engine-Free Contract Foundations - Context

**Gathered:** 2026-10-04
**Status:** Ready for planning
**Mode:** auto. The user pre-delegated every answer to Claude's recommended option on 2026-10-04 ("на все вопросы отвечаю твоими рекомендованными вариантами"). Every choice below is that recommendation, and each one is logged in 72-DISCUSSION-LOG.md.

<domain>
## Phase Boundary

This phase builds and proves, with tests, the fail-closed content validation for CAD uploads and the
`measurements` JSON contract. There is still no CAD engine. It delivers:

- a content sniffer for step/stp/iges/igs/stl/dxf (plus DWG recognition), used by both `POST /v1/inspect`
  and job creation;
- stable 422 codes for truncated STEP/IGES/DXF, ambiguous STL, binary DXF and DWG;
- a differential test of the sniffer against the real readers (OCP, ezdxf) on the phase 71 corpus;
- the versioned measurements schema, its closed vocabularies and a Go validator;
- the `measurements` output role (enum, migration, `handleGetJob`);
- `stp`/`igs` alias normalization (Go and frontend).

The class `cad` is NOT registered. `AllEngines()` is unchanged, `/v1/formats` shows no cad pairs, and
nothing runs OCP or VTK in production. The engine, the worker and the input ceilings belong to phases 73-76.

</domain>

<decisions>
## Implementation Decisions

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

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase 71 decisions (inputs)
- `.planning/phases/71-evaluation-gate/71-DECISION.md`: "Черновик словарей для фазы 72" (units, reason codes, views, findings); the OCP pin; the image tags.
- `.planning/phases/71-evaluation-gate/71-04-SUMMARY.md`: observed OCP behaviour (orientation repair, open shell gives 0 solids, inch scaling, the ±1e100 bbox).
- `.planning/phases/71-evaluation-gate/evidence/71-04-helpers.txt`: the UNITS, ROW and FINDING lines.
- `internal/convert/testdata/cad/PROVENANCE.md`: the corpus, licences and the synthetic generator protocol.
- `scripts/cad-corpus-gen.py`, `scripts/cad-measure/common.sh`, `Dockerfile.cad-measure`: generator and reader image.

### Contracts
- `docs/ERROR-CODES.md`: the code-space rules (D-02 class granularity, D-03 one code space, the sync rules in "Keeping this document honest").
- `internal/errcode/errcode.go`: where the codes are defined.
- `.planning/REQUIREMENTS.md`: CADIN-01..03 and MEAS-05, plus MEAS-01..04 for the shape phase 74 will emit.
- `.planning/ROADMAP.md` § Phase 72: the success criteria.

### Existing patterns
- `internal/convert/markupsniff.go`, `docsniff.go`, `avsniff.go`, `htmlsniff.go`: sniffer envelope and window.
- `internal/api/inspect_handlers.go` (`detectInspectFormat`), `internal/api/handlers.go` (`handleCreateJob` sniff order): the one-detector-two-endpoints rule.
- `internal/db/migrations/0015_operation_and_output_roles.sql`, `internal/jobs/jobs.go` (OutputRole*): role pattern.
- `internal/convert/doc_sync_test.go`: doc-to-code sync test pattern.
- `frontend/src/formats.ts`, `frontend/src/formats.test.ts`: the frontend alias copy.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `readSniffPrefix` and `SniffWindowBytes` (`markupsniff.go`): a bounded head read. The CAD path needs a tail read too.
- `inspect.Tracker` budget: every detection read is charged to it.
- `errcode` and `writeError`: the envelope; the format detail goes in `message`.
- Corpus with 37 public files and 18 synthetic files, the fetched large files (`scripts/cad-corpus-fetch.sh`), and the measurement image with OCP 7.9.3.1.1 and ezdxf 1.4.4.

### Established Patterns
- Content beats extension. The extension is only an extra fail-closed gate.
- Two endpoints, one detector order. Inspect reports and never rejects.
- Error codes are per class, documented, and covered by sync tests.
- Generated fixtures are committed before the code that judges them.

### Integration Points
- `handleCreateJob` sniff block, which runs before pair lookup.
- `detectInspectFormat` and the inspect family dispatch.
- `NormalizeFormat`, and the frontend `formats.ts`.
- `job_outputs.role` CHECK and `handleGetJob` output selection.

</code_context>

<specifics>
## Specific Ideas

- Avoid optimistic answers for unexamined input. A sniffer that did not look at the tail cannot call a file "not truncated", so the tail read is mandatory.
- Do not predict a parser; test against it. That is the reason for the differential test with a committed divergence table, rather than clever byte rules alone.

</specifics>

<deferred>
## Deferred Ideas

- Input byte ceilings and operational caps (triangles, entities, NAUO, MINSERT): phases 73 and 76, using the 71-06 numbers. Those numbers are being re-measured after Codex P1 on PR #32.
- Render time guard for stacked duplicates: phase 76.
- Reviewed todos, not folded (score 0.2, keyword-only match): the amd64 preflight check, the /proc grant for other-process entries, soffice socket removal, and sweep tests touching host /tmp. None of them is in the phase 72 domain.

</deferred>

---

*Phase: 72-engine-free-contract-foundations*
*Context gathered: 2026-10-04*
