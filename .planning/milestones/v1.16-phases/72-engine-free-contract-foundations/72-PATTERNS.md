# Phase 72: Engine-Free Contract Foundations - Pattern Map

**Mapped:** 2026-10-05
**Files analyzed:** 27 new/modified
**Analogs found:** 25 / 27 (2 partial, no exact one for the Python reader helper's reader logic and for the golden TSV)

All analog paths below were checked as git-tracked source (`git ls-files`). No gitignored mirror paths are named.

Orchestrator decisions applied (override RESEARCH.md open questions):
1. Inspect reports CAD rejections via sentinel `Report.Format` values built from the `errcode` constants (zip_malformed precedent). No new `inspect.AllCodes()` entries, Pre59 golden untouched.
2. BOM, trailing NUL and binary-STL trailing bytes are rejected by default pending the reader differential. Binary DXF is a "policy" rejection category in the divergence table.
3. Rejection fixtures go to `internal/convert/testdata/cad/reject/` (NOT `synthetic/`). The reader helper is `scripts/cad-sniff-reader.py` (NOT under `scripts/cad-measure/`, so the 71 image tag stays stable).

Project rule for every Go file below: no code comments (user CLAUDE.md). The excerpts below carry comments only because they are quoted from existing files; do not copy the comments.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `internal/convert/cadsniff.go` (new) | utility (sniffer) | transform over byte windows + ReadAt | `internal/convert/markupsniff.go`, `internal/convert/docsniff.go` | role-match |
| `internal/convert/cadsniff_test.go` (new) | test | table/corpus | `internal/convert/markupsniff_test.go`, `internal/convert/sniffprefix_test.go` | role-match |
| `internal/convert/convert.go` (mod: `NormalizeFormat`) | utility | transform | itself (lines 182-201) | exact |
| `internal/convert/format_alias_sync_test.go` (new) | test (doc/code sync) | file-parse compare | `internal/convert/readme_sync_test.go` | exact |
| `internal/convert/convert_test.go` (mod: no-CAD assertions) | test | guard | `TestAllEnginesCoversEveryEngineClass` in same file | exact |
| `internal/errcode/errcode.go` (mod) | config (constants) | n/a | itself (lines 14-60) | exact |
| `docs/ERROR-CODES.md` (mod) | doc/contract | n/a | existing sync table rows (lines 98-100) | exact |
| `docs/openapi.yaml` (mod: Error enum + InspectReport.format text) | doc/contract | n/a | `zip_malformed` text (line ~560) and Error enum (lines 115-132) | exact |
| `internal/api/handlers.go` (mod: create-path CAD branch, rejection map) | controller | request-response | the latex/md branches in the same file (lines 355-420) | exact |
| `internal/api/inspect_handlers.go` (mod: sentinels, branch, family-result cases) | controller | request-response | `inspectZipMalformedFormat` handling (lines 117-162, 1393, 1538) | exact |
| `internal/api/cad_sniff_test.go` (new: create/inspect agree, no-advertise) | test | request-response | `inspect_handlers_test.go` (line ~798), `inspect_offer_spec_sync_test.go` | role-match |
| `internal/inspect/bindings.go` (mod: 4 `ErrCode` bindings) | config | n/a | `RejectBindings` entry `unrecognized_content` (line 56) | exact |
| `docs/FINDINGS.md` (mod: 4 binding rows) | doc/contract | n/a | line 253 binding row | exact |
| `internal/measurements/measurements.go` (new) | model + validator | transform (JSON validate) | `internal/inspect/status.go` / `findings.go` (closed vocab + `All*()`) | role-match |
| `internal/measurements/measurements_test.go` (new) | test | table | `internal/inspect/findings_test.go` | role-match |
| `internal/measurements/doc_sync_test.go` (new) | test (doc/code sync) | file-parse compare | `internal/convert/doc_sync_test.go` | exact |
| `docs/MEASUREMENTS.md` (new) | doc/contract | n/a | `docs/SANITIZE.md` (two tables, phrase) | role-match |
| `internal/db/migrations/0018_output_role_measurements.sql` (new) | migration | DDL | `0016_tectonic_engine.sql` | exact |
| `internal/jobs/jobs.go` (mod: `OutputRoleMeasurements`) | model (const) | n/a | `OutputRoleReport` (line ~96) | exact |
| `internal/jobs/output_role_test.go` (mod: legal list) | test | live-DB | itself (line 143) | exact |
| `internal/api/handlers_test.go` (mod: measurements never `download_url`) | test | request-response | `TestGetJob_DownloadURLResolvesToPrimaryNotOrdinalZero` (line 2069) | exact |
| `frontend/src/formats.ts` + `formats.test.ts` (mod) | utility + test | transform | itself | exact |
| `scripts/cad-corpus-gen.py` (mod: reject fixtures) | utility (generator) | batch/file-I/O | itself (`FIXTURES`/`EXPECTED`/`build_*`) | exact |
| `internal/convert/testdata/cad/reject/*` (new fixtures) + `PROVENANCE.md` (mod) | fixture + doc | file | `synthetic/*` + existing PROVENANCE sections | exact |
| `scripts/cad-sniff-differential.sh` (new) | utility (driver) | batch, docker run | `scripts/cad-corpus-inventory.sh` | exact |
| `scripts/cad-sniff-reader.py` (new) | utility (in-image helper) | batch | `scripts/cad-measure/cad_mesh.py`, `cad_dxf.py` (call shapes only) | partial |
| `internal/convert/testdata/cad/sniff-golden.tsv` (new) + evidence table | fixture/evidence | file | none (new format) | no analog |

## Pattern Assignments

### `internal/convert/cadsniff.go` (utility, transform + bounded ReadAt)

**Analog:** `internal/convert/markupsniff.go` (pure `*FromPrefix` decision over already-read bytes, one window constant) plus `internal/api/inspect_handlers.go:570-584` (`readSniffWindow`, the ReadAt idiom).

**Package header / imports** (markupsniff.go lines 1-17): single-file package doc comment in the `convert` package, stdlib-only imports (`bytes`, `io`, `encoding/binary`, `strconv`, `strings`). Add no new dependency. Do NOT copy the comments (project rule).

**Pure-over-windows shape** (markupsniff.go `ValidateSniffPrefix`, lines 47-80; it takes raw bytes and `size`, never touches I/O):
```go
func ValidateSniffPrefix(raw []byte, size int64) ([]byte, bool) {
	buf := raw
	if len(buf) == 0 {
		return nil, false
	}
	...
}
```
Copy the shape (`func SniffCADWindows(head, tail []byte, size int64, declared string) CADResult`), but do NOT call `ValidateSniffPrefix`: it rejects NUL and invalid UTF-8, which would reject binary STL and legacy 8-bit files (D-03).

**ReadAt idiom** (inspect_handlers.go 570-584):
```go
func readSniffWindow(r io.ReaderAt, size int64) ([]byte, error) {
	if size <= 0 {
		return nil, nil
	}
	n := size
	if n > convert.SniffWindowBytes {
		n = convert.SniffWindowBytes
	}
	buf := make([]byte, n)
	read, err := r.ReadAt(buf, 0)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf[:read], nil
}
```
`ReadCADWindows(r io.ReaderAt, size int64)` and `readCADTail` follow this: `ReadAt` only, `err != io.EOF` rule, never `Read`.

**Core decision logic:** copy the verified skeleton in 72-RESEARCH.md "Code Examples" (lines 551-751: `CADRejection`, `CADResult`, `CADHeadBytes/CADTailBytes = 4096`, `IsCADDeclared`, `SniffCADWindows`, `sniffIGES`, `sniffDXF`, `sniffSTL`, `binarySTLSize`, `trimRightSpace`, `allDigits`). It was run over 101 corpus files with 0 false rejections. Apply the five deviations from RESEARCH Findings 4 (STL ASCII tail has a name; STL declared-gated and last; IGES stride/CRLF/trailing-space tolerance; STEP missing HEADER = truncated; DXF skips `999` pairs). Per orchestrator decision 2, BOM / trailing NUL / binary-STL trailing bytes stay rejected (strict equality) until the differential changes the `policy` column.

**Error handling:** rejection is a typed value (`CADRejection`), never an error; `convert` must not import `errcode` (layering, RESEARCH Pattern 2).

---

### `internal/convert/cadsniff_test.go` (test, table over corpus)

**Analog:** `internal/convert/markupsniff_test.go` / `sniffprefix_test.go` (stdlib `testing`, no assertion lib). Corpus walk uses `internal/convert/testdata/cad/{public,synthetic,reject}`.

Pattern to follow (RESEARCH test map): `TestSniffCAD` (corpus table, 8-bit files accepted), `TestSniffCADRejects` (four classes + mutation table), `TestSniffCADDWG` (STL-header-`AC10` precedence fixture accepted), `TestSniffCADGolden` (verdicts equal `sniff-golden.tsv`; fetched rows only when present under `$OCTOCONV_CAD_CACHE/fetched` and sha256 matches `scripts/cad-corpus-manifest.tsv`, else `t.Skip` that row). Garbage `.dxf`/`.step` without magic must yield an empty result (falls to `unrecognized_content`), not `truncated`.

---

### `internal/convert/convert.go` `NormalizeFormat` (utility, transform)

**Analog:** itself, lines 182-201:
```go
func NormalizeFormat(f string) string {
	f = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(f), "."))
	switch f {
	case "jpeg":
		return "jpg"
	case "tif":
		return "tiff"
	case "htm":
		return "html"
	case "tex":
		return "latex"
	default:
		return f
	}
}
```
Either add `case "stp": return "step"` and `case "igs": return "iges"` (minimal diff), or convert to an unexported `formatAliases` map as RESEARCH recommends so the sync test can compare sets. Pick one; the sync test must read whichever you choose.

### `frontend/src/formats.ts` + `formats.test.ts` (utility, transform)

**Analog:** `frontend/src/formats.ts` lines 13-33 (same switch). Add `case 'stp': return 'step';` and `case 'igs': return 'iges';` after `'tex'`, and update the doc comment table list. Extend `formats.test.ts` "folds the rest of the alias table" with the two cases. The TS regex in the sync test is `case '<from>':\s*return '<to>';` so keep exactly that one-statement-per-case layout.

### `internal/convert/format_alias_sync_test.go` (test, file-parse compare)

**Analog:** `internal/convert/readme_sync_test.go` (reads a repo-relative file from `internal/convert/`, compares both directions, names the offender):
```go
import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/apaderin/octoconv/internal/convert"
)

const readmePath = "../../README.md"
```
Use `const formatsTSPath = "../../frontend/src/formats.ts"`, `os.ReadFile`, a `regexp` for `case '([a-z]+)':\s*return '([a-z]+)';`, and compare against the Go alias table both ways. Note no Go<->TS sync test exists today (RESEARCH section 9).

### `internal/convert/convert_test.go` (guard additions)

**Analog:** `TestAllEnginesCoversEveryEngineClass` in the same file (stays `len == 9`, no `cad`). Add an assertion that `Default` registers no pair whose `From`/`To` is step/iges/stl/dxf/dwg. Do not touch `readme_sync_test.go`.

---

### `internal/errcode/errcode.go` (constants)

**Analog:** itself, lines 14-60. Append four constants in the existing aligned-comment style (inline trailing comment per constant is the file convention here; one-line only, keep them short or omit per the no-comment rule):
```go
	InvalidRequest        = "invalid_request"        // ...
	UnsupportedConversion = "unsupported_conversion" // ...
```
New values: `TruncatedCADFile = "truncated_cad_file"`, `AmbiguousSTL = "ambiguous_stl"`, `BinaryDXFUnsupported = "binary_dxf_unsupported"`, `DWGUnsupported = "dwg_unsupported"`. Edit the package comment "closed at seventeen codes" (line 16) to twenty-one by hand (not machine-checked). Land this commit FIRST: `TestRejectBindingsAreWellFormed` calls `errcode.IsKnown`.

### `docs/ERROR-CODES.md`, `docs/openapi.yaml` (contract docs)

**Analog:** ERROR-CODES.md rows at lines 98-100:
```
| `unsupported_conversion` | 422 | The requested `(source, target)` format pair is not served by any registered engine | Do not retry unmodified; check `GET /v1/formats` ... |
```
Add four rows with first cell matching `^\|\s*\`([a-z][a-z0-9_]*)\`` (checked by `TestErrorCodesDocMatchesGoConstants`); edit the "Seventeen codes" prose (line ~92). openapi.yaml: four lines `            - truncated_cad_file` etc. (12-space indent) inside the `Error.code` enum (lines ~115-132, checked by `internal/errcode/openapi_sync_test.go`); extend the `InspectReport.format` description (around the `zip_malformed` text, line ~560) to name each of the four sentinel strings (checked by `TestInspectSentinelFormatsMatchTheContract`). State in ERROR-CODES.md that compressed/binary IGES and dotted ancient DWG signatures fall through as unrecognized.

---

### `internal/api/handlers.go` (controller, request-response) CAD branch

**Analog:** the latex / md-rst-org branches, lines ~355-420 in the same file. Insertion point: AFTER the md/rst/org branch, BEFORE `if detected == "" && convert.IsOLECFB(file) {` (so before `SniffVideo`/`SniffAudio`).

**Branch shape to copy** (md/rst/org branch, same file):
```go
	if detected == "" && (source == "md" || source == "rst" || source == "org") && convert.LooksLikeText(file, header.Size) {
		detected = source
	}
```
Use the RESEARCH insertion (lines 758-770): gate on `detected == "" && convert.IsCADDeclared(source)`, `convert.ReadCADWindows(file, header.Size)` (ReadAt only; do NOT rebind `rest`), on rejection:
```go
log.Printf("content validation rejected: client_id=%s filename=%q reason=%s", client.ID, filename, code)
writeError(w, http.StatusUnprocessableEntity, code, cadRejectionMessage(res))
return
```
Match the exact existing log-line format by grepping `content validation rejected` in `handlers.go` before writing (the scanning test `TestRejectBindingsCoverEveryContentValidationReason` parses `reason=<token>`). A read error leaves `detected == ""` (fail-closed to the existing 422), same as the latex window read.

**Code mapping** (Pattern 2): `var cadRejectionCode = map[convert.CADRejection]string{convert.CADRejectTruncated: errcode.TruncatedCADFile, ...}` in `internal/api`. `TestNoLiteralErrorCodesInAPISource` forbids string literals at `writeError` sites, so always `errcode.X`. Messages are fixed strings naming a format constant, never filename or content.

### `internal/api/inspect_handlers.go` (controller, request-response) sentinels

**Analog 1 (declaration)**, lines 117-162:
```go
	inspectZipMalformedFormat = "zip_malformed"
...
var inspectPublicSentinelFormats = []string{
	inspectZipTooLargeFormat,
	inspectZipReadFailedFormat,
	inspectZipMalformedFormat,
}
```
Append `errcode.TruncatedCADFile, errcode.AmbiguousSTL, errcode.BinaryDXFUnsupported, errcode.DWGUnsupported` to `inspectPublicSentinelFormats` (built from the constants, so "same code string" is structural).

**Analog 2 (detection insertion)**, in `detectInspectFormat` lines 764-780: put the CAD block between the `if prefix, textShaped := convert.ValidateSniffPrefix(...)` block and `if detected == "" && convert.IsOLECFBFromPrefix(window)`, OUTSIDE the `textShaped` condition (CAD bytes fail the text envelope). Use RESEARCH lines 776-790. A tail `ReadAt` error sets `sniffReadFailed = true` (the PR9R25/R27 rule), and the tail read is charged `tr.Consume(int64(len(tail)))` like `tr.Consume(int64(len(window)))` at line 750.

**Analog 3 (family result per sentinel)**, lines ~1393 and ~1538 (two switch sites):
```go
	case detected == inspectZipMalformedFormat:
		return inspect.FamilyResult{Checks: []inspect.Check{
			{Code: inspect.CodeExtensionMismatch, Status: inspect.StatusUnknown, Reason: inspect.ReasonMalformedInput},
		}}
```
Add cases per RESEARCH mapping table: truncated/ambiguous -> `StatusUnknown` + `ReasonMalformedInput`; binary DXF / DWG -> `StatusUnknown` + `ReasonParserUnavailable`. Neither flips coverage. No change to `internal/inspect/findings.go` or `AllCodes()` (the Pre59 golden and `TestAllCodesCount == 14` stay).

### `internal/inspect/bindings.go` and `docs/FINDINGS.md`

**Analog:** `RejectBindings` entry line 56:
```go
	"unrecognized_content": {ErrCode: errcode.InvalidRequest},
```
Add four entries keyed by the log tokens (`truncated_cad_file` etc.), each `{ErrCode: errcode.X}`; plus four rows in the FINDINGS.md table under "Binding to existing job-creation rejections (D-04)", row format at line 253: ``| `unrecognized_content` | not a finding — `errcode.invalid_request` (...) |``. Same commit as the log line (Pitfall 2).

### `internal/api/cad_sniff_test.go` (test)

**Analog:** `inspect_handlers_test.go` (sentinel assertion style, line ~798: `resp.Format != inspectZipMalformedFormat`) and the `NewServer(...)` construction in `handlers_test.go:2083`. Required tests: `TestCADCreateInspectAgree` (every corpus/reject fixture through BOTH endpoints: inspect `format` is a sentinel iff create answered that 422 code; an accepted file gets inspect `format == detected` and create `unsupported_conversion`), a valid `.stp` gets `unsupported_conversion: step -> png`, `/v1/formats` body names no CAD format, extend `TestInspectSentinelFormatsMatchTheContract` scope by the contract text only.

---

### `internal/measurements/measurements.go` (model + validator)

**Analog:** `internal/inspect/status.go` / `findings.go` (closed string vocabulary as typed consts + `AllReasons()`/`AllCodes()` that return copies, enumerated in a doc and pinned by a test). Copy: const block per vocabulary (`Unit*`, `UnitSource*`, `NullReason*`), `func AllUnits() []string` returning a fresh slice (cf. `TestAllCodesReturnsCopy`), `const Schema = "octoconv.measurements/v1"`, `Validate(raw []byte) error` using `json.Decoder` with `DisallowUnknownFields`, `math.IsNaN/IsInf` on decoded floats, bbox bound `|v| <= 1e12` (mirrors `FINITE_LIMIT = 1e12` at `scripts/cad-measure/cad_mesh.py:41`), volume/area strictly `> 0` with `closed_solids >= 1`, null needs a reason and non-null forbids one, unit `unspecified` iff source `none`, per-format reason subsets and 2- vs 3-coordinate bbox (RESEARCH Finding 11). Wrap errors `fmt.Errorf("<action>: %w", err)`; library code never logs (project convention). Producer key names come from `cad_mesh.py:282-346`: `solids`, `closed_solids`, `open_solids`, `invalid_solids`, `volume`, `area`, `volume_null_reason`, `bbox{min,max}`, `unit_text`, `triangles`.

### `internal/measurements/measurements_test.go` (test)

**Analog:** `internal/inspect/findings_test.go` (count pin, copy-return test, stdlib table tests). Include a table test confirming Go `encoding/json` rejects bare `NaN`/`Infinity`/`1e999` (assumption A8) and the 1e100 sentinel bbox rejection.

### `internal/measurements/doc_sync_test.go` + `docs/MEASUREMENTS.md`

**Analog:** `internal/convert/doc_sync_test.go` lines 1-80. Copy verbatim in structure:
```go
const (
	sanitizeDocPath   = "../../docs/SANITIZE.md"
	threatTableHeader = "## Threat classes removed"
)
var codeRowPattern = regexp.MustCompile("^\\|\\s*`([a-z][a-z0-9_]*)`")
func extractTableCodes(doc, startHeader string) []string { ... }
```
Use `../../docs/MEASUREMENTS.md`, one `## ` section per vocabulary (units, unit sources, null reasons), `extractTableCodes` first-cell tokens, bidirectional `diffSets` against `All*()`. The schema string contains `.` and `/`, so pin it as a named phrase constant like `guaranteeBoundaryPhrase` (line 30-60 pattern) rather than a table cell. Reuse (copy, unexported per package) the same `codeRowPattern`. Add the doc to README docs table (`README.md:470-473`) and `docs/README.md`.

---

### `internal/db/migrations/0018_output_role_measurements.sql` (migration)

**Analog:** `internal/db/migrations/0016_tectonic_engine.sql` (the DROP CONSTRAINT / ADD CONSTRAINT pair; the existing file's long explanatory header is a comment style; keep the new file's header to a short external-constraint note or none):
```sql
ALTER TABLE jobs DROP CONSTRAINT jobs_engine_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_engine_check
    CHECK (engine IN ('image', 'document', ...));
```
New file:
```sql
ALTER TABLE job_outputs DROP CONSTRAINT job_outputs_role_check;
ALTER TABLE job_outputs ADD CONSTRAINT job_outputs_role_check
    CHECK (role IN ('primary', 'report', 'preview', 'thumbnail', 'measurements'));
```
The constraint name `job_outputs_role_check` is confirmed by `output_role_test.go:138` (asserts the error mentions it). Next number is 0018 (files end at 0017). No engine migration needed (`'cad'` is already in `jobs_engine_check`).

### `internal/jobs/jobs.go` (const)

**Analog:** `OutputRoleReport` at line ~96. Add `const OutputRoleMeasurements = "measurements"` beside it. `Repo.AddOutputWithRole` (repo.go:646) already exists; `MarkDone` counts only `role = 'primary'` (repo.go:278), so no repo change.

### `internal/jobs/output_role_test.go`

**Analog:** itself, line 143: change `legal := []string{"primary", "report", "preview", "thumbnail"}` to include `"measurements"`; keep the bogus-role rejection assertion. Needs `DATABASE_URL` (skips otherwise); say in the plan that the compose Postgres must be up for it to count as run.

### `internal/api/handlers_test.go` (measurements never `download_url`)

**Analog:** `TestGetJob_DownloadURLResolvesToPrimaryNotOrdinalZero`, lines 2069-2110. Copy it with outputs `{Ordinal: 0, ObjectKey: measKey, Format: "json", Role: "measurements"}` first and `{Ordinal: 1, ..., Role: "primary"}` second; positive assertion `resp["download_url"] == primary URL`, and `outputs[]` carries `role: "measurements"`. No handler code change (`handleGetJob` already selects by `o.Role == outputRolePrimary`, handlers.go:1285-1288). Optional worker-side twin next to `internal/worker/webhook_role_test.go`.

---

### `scripts/cad-corpus-gen.py` (generator) + `internal/convert/testdata/cad/reject/*`

**Analog:** the script itself: `FIXTURES` list (line 38), `EXPECTED` marker dict (line 59), `build_*` functions (`build_stl_binary` line 230, `build_stl_ascii` line 240, `build_comments_999`, `build_iges_no_newlines` line 209), `build_all()` line 362. Add a SEPARATE `REJECT_FIXTURES` / `REJECT_EXPECTED` pair and a separate output directory `reject/` (orchestrator decision 3) so `synthetic/` and the 71 inventory stay unchanged. Each reject fixture gets self-check markers. Required fixtures: truncated step / iges (T record dropped) / dxf (no EOF) / binary STL (cut by 1 byte); ambiguous STL where `\nendsolid\n` ends the last triangle record AND the self-check asserts BOTH `size == 84 + 50n` and the ASCII markers (RESEARCH Pitfall 4); binary DXF sentinel `AutoCAD Binary DXF\r\n\x1a\x00`; `AC1027` + zeros as `.dwg`; `stl_binary_ac10_header.stl` (must be ACCEPTED, STL-over-DWG precedence); BOM-prefixed `.dxf`, trailing-NUL `.step`, binary STL + 1 trailing byte (policy cases for the differential); an 84-byte n=0 binary STL (accepted, note for phase 73). Commit the generated bytes BEFORE the judging code (D-10, 71 D-05). Run the generator inside the existing image per the script's own docstring (`-v "$PWD:/src:ro"`); do not run docker in this planning step.

### `internal/convert/testdata/cad/PROVENANCE.md`

**Analog:** the existing per-file section format (line 21+): heading `## \`dir/file\` — N bytes`, then `Source`/`Licence: generated`/`sha256`/`Used for`/`Proves`. Add one section per `reject/` fixture. Note that `reject/` sits under `testdata/cad`, which `scripts/cad-corpus-inventory.sh` walks recursively (Pitfall 8): either exclude `reject/` in the inventory script's `find` or state in PROVENANCE that inventory re-runs now include it. Prefer excluding it so 71 numbers stay comparable (small edit to `cad-corpus-inventory.sh`'s `count_container`; that script is not under `scripts/cad-measure/`, so the image hash is unaffected).

---

### `scripts/cad-sniff-differential.sh` (driver, batch)

**Analog:** `scripts/cad-corpus-inventory.sh` lines 1-40 (REPO_ROOT discovery, sourcing `common.sh`, arg parsing with `usage`, docker presence check, `cad_build_image`, `cad_require_native`, `mktemp` work dir with `trap`):
```bash
#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO_ROOT"
. "$REPO_ROOT/scripts/cad-measure/common.sh"

OCP=7.9.3.1.1
...
if ! command -v docker >/dev/null 2>&1; then
  echo "error: docker is required" >&2
  exit 69
fi

TAG="$(cad_build_image "$OCP" "$VARIANT")"
cad_require_native "$TAG"
```
Differences: `VARIANT=pruned` (shipping variant; inventory uses `full`, a different tag); use `cad_evidence_header "$TAG" 2 4g`; run the helper by bind mount, not baked in: `docker run --rm --network none --user nobody -v "$REPO_ROOT/scripts:/lib:ro" -v "$CORPUS:/corpus:ro" "$TAG" /opt/venv/bin/python -I -B /lib/cad-sniff-reader.py ...`. Make `scaled` opt-in (slow, hundreds of MB). Writes the divergence table to `.planning/phases/72-engine-free-contract-foundations/evidence/72-sniff-divergence.txt` (tracked with `git add -f`) and the golden to `internal/convert/testdata/cad/sniff-golden.tsv`. Columns: name, group, sha256, sniff_format, sniff_code, reader, reader_accepts, policy, note. `policy` categories include binary-dxf / dwg / bom / trailing-nul / binary-stl-trailing so policy rejections are not mistaken for sniffer defects. Acceptance check: `scripts/cad-measure/common.sh tag 7.9.3.1.1 pruned` prints the same value before and after (Pitfall 6).

### `scripts/cad-sniff-reader.py` (in-image helper)

**Analog (partial):** `scripts/cad-measure/cad_mesh.py` / `cad_dxf.py` for the reader call shapes only. Copy the helper skeleton in RESEARCH lines 797-838 (`read_step` via `STEPControl_Reader` + `IFSelect_RetDone` + `TransferRoots`/`OneShape().IsNull()`; IGES via `IGESControl_Reader` the same way; `read_dxf` recording both `ezdxf.readfile` and `ezdxf.recover.readfile`; `read_stl` via `sys.path.insert(0, "/opt/cad-measure"); import cad_mesh; cad_mesh.read_stl(path)`). No comments, no new pip package (D-08). Must NOT live under `scripts/cad-measure/` (hash includes `scripts/cad-measure/*.py`).

---

## Shared Patterns

### Content decides, extension only gates (fail closed)
**Source:** `internal/api/handlers.go` md/rst/org and latex branches (lines ~355-420); `internal/api/inspect_handlers.go` 764-780.
**Apply to:** `cadsniff.go`, both API call sites. CAD runs only when `detected == ""` after `convert.Sniff` and ZIP container, gated on the declared format, before OLE-CFB/video/audio. A PNG renamed `.step` is already `detected="png"` and never reaches CAD.

### One detector, two endpoints, no divergence
**Source:** `detectInspectFormat` doc comment (inspect_handlers.go:586-592). **Apply to:** both call sites call the same `SniffCADWindows` with the same 4 KiB head window; the agree-test enforces "never ok in inspect and rejected by create".

### Error codes: constants only, per class, documented
**Source:** `internal/errcode/errcode.go`; `TestNoLiteralErrorCodesInAPISource`, `TestEveryErrorResponseCarriesKnownCode` (`internal/api/errcode_envelope_test.go`). **Apply to:** every `writeError` for the four new codes. HTTP layer never echoes client input; messages are fixed strings.

### Sync tests are the contract
**Source:** `internal/errcode/doc_sync_test.go`, `internal/errcode/openapi_sync_test.go`, `internal/inspect/bindings_test.go`, `internal/convert/doc_sync_test.go`, `internal/convert/readme_sync_test.go`. **Apply to:** each new vocabulary lands code, doc and test in one commit; run these suites after every doc/const edit.

### Frozen goldens: do not move
**Source:** `internal/api/pre58_pinning_test.go`, `pre59_pinning_test.go` (`git rev-list --count HEAD -- internal/api/testdata/pre59_inspect_pdf.golden.json` must print 1). **Apply to:** all of the above; a diff touching `internal/inspect/findings.go` or any `pre5*_*.golden.json` is a stop-and-escalate signal.

### Local gate on the pinned toolchain
**Source:** memory `pinned-toolchain-not-local-one`. **Apply to:** every Go gate: `GOTOOLCHAIN=go1.26.4 go test ./internal/convert/ ./internal/errcode/ ./internal/inspect/ ./internal/measurements/ ./internal/api/ ./internal/jobs/` and `(cd frontend && npx vitest run)`. No docker in this phase's planning/mapping step; the differential run is a gated execution step.

### Error wrapping and logging (project convention)
Library code returns `fmt.Errorf("<action>: %w", err)` and never logs; only `internal/api` handlers log (`log.Printf("content validation rejected: ...")`). No comments in new Go code (user rule), design rationale goes in the PR body.

## No Analog Found

| File | Role | Data Flow | Reason |
|---|---|---|---|
| `internal/convert/testdata/cad/sniff-golden.tsv` | fixture | file | No existing golden-TSV the Go suite reads; the closest are `internal/api/testdata/pre5*_*.golden.json` (JSON, byte-frozen). Define columns per the differential section. Tracked normally under `internal/convert/testdata/` (not under gitignored `.planning/`). |
| `.planning/phases/72-*/evidence/72-sniff-divergence.txt` | evidence | file | Closest is `.planning/phases/71-evaluation-gate/evidence/71-04-helpers.txt` (HDR block + line records); needs `git add -f` (`.gitignore:7 /.planning/`). |

## Metadata

**Analog search scope:** `internal/convert`, `internal/api`, `internal/inspect`, `internal/errcode`, `internal/jobs`, `internal/db/migrations`, `frontend/src`, `scripts/`, `docs/`, `internal/convert/testdata/cad`
**Files scanned:** about 40 read or grepped
**Pattern extraction date:** 2026-10-05
