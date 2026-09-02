---
phase: 60-sanitize-eighth-engine-class
plan: 01
status: complete
completed: 2026-08-31
commits: ["4824bb7"]
---

# 60-01 — Corpus and pre-phase pin

## What was built

`scripts/sanitize-corpus.py` (deterministic, self-checking) and the three
fixtures it emits into `internal/convert/testdata/sanitize/`, with
`PROVENANCE.md`; plus `internal/api/pre60_pinning_test.go` and its golden
snapshot of today's 422 on four `operation` values.

No production code was touched — verified mechanically, not asserted:

```
$ git show --name-only --format= HEAD | grep -E '^(internal/.../[^/]*\.go|cmd/)' | grep -v _test.go
none — only tests, testdata and scripts
$ git rev-list --count HEAD -- internal/api/testdata/pre60_sanitize_rejection.golden.json
1
```

## The R1 table, re-measured on the committed fixture

The research measured R1 on `/tmp/threat.pdf`, a path from a session that has
since ended. Re-running it against the committed file made the case for two
steps **stronger**, in two rows that both moved against the sanitizer:

| Marker | input | after `pdfwrite` | after `ps2write → pdfwrite` |
|---|---|---|---|
| `/OpenAction`, `/AA`, `/Launch`, `/URI`, `evil.example` | 1 each | 0 | 0 |
| `JavaScript` | 3 | 1 | 0 |
| `EmbeddedFile` | 2 | **2** | 0 |
| `Secret Internal Title` | 1 | 1 | 0 |
| `jane.doe@corp.example` | 1 | 1 | 0 |
| `xpacket` | **0** | **2** | 0 |

* **`EmbeddedFile` 2 → 2, not 1 → 1.** Object 6 carries a real
  `/Type /EmbeddedFile` stream, which the research fixture lacked. A single
  `pdfwrite` pass preserves both occurrences, one of them on a genuine stream
  object.
* **`xpacket` 0 → 2.** The input has **no** XMP packet. Direct `pdfwrite`
  **creates one**, carrying the title and author it was invoked to remove. This
  is R2 in its sharpest form: the sanitizer would be introducing the metadata
  carrier itself. With `-dOmitXMP` (D-09) the count is 0.

Text survives exactly: 42 non-whitespace characters in and out, the same unit
as Phase 59's corpus and `countNonWhitespace`.

The two-step output has exactly one `startxref` and one `%%EOF` — SC3's
prior-revision clause, measured here and put under test by 60-02/60-04.

## What went wrong, and what caught it

The generator's self-check failed on its **first run**, on a marker its own
author had written: `macroEnabled` was asserted against the raw bytes of a
deflate archive, where it cannot appear. The fix was to move the assertion to
the decompressed part, not to weaken the check.

Worth recording because it is the check's whole purpose in miniature. A
generator that silently emits a threat fixture carrying no threat hands every
downstream test in this phase a green result on an empty subject — the exact
defect class this corpus exists to prevent, caught on the corpus's own author.

## Deviations from the plan

**The pin covers four operations, not the "sanitize plus at least two"
minimum.** `sanitize` is the subject; `inspect` and `extract` are schema-legal
and engine-less and are *not* lifted by this phase; `bogus` was never legal
anywhere. The third and fourth cases separate "Gate 1 stopped rejecting
schema-legal values" from "Gate 1 stopped rejecting anything at all" — without
them, widening the gate and registering one engine produce an identical diff
for the sanitize case.

**The generator lives at `scripts/sanitize-corpus.py`, not in
`scripts/fixtures/`.** The project has two fixture conventions split by
consumer: `scripts/fixtures/` for live-gate shell fixtures (generator beside
output, `README.md` as provenance) and `internal/<pkg>/testdata/` for Go-test
corpora (`PROVENANCE.md` beside the files, Phase 59). This corpus is read by Go
tests, so it follows the second. Recorded in the script header so a future
reader does not "fix" the split.

## Verification

| Gate | Result |
|---|---|
| `python3 scripts/sanitize-corpus.py` | exit 0, three files, self-check passes |
| Determinism | byte-identical on rerun (md5 compared) |
| PDF markers | `/OpenAction` 1, `/AA` 1, `JavaScript` 3, `EmbeddedFile` 2, `/URI` 1, `Launch` 1 |
| `pdftotext` on fixture | prints body text; 42 non-whitespace |
| `unzip -l macro_enabled.docm` | lists `word/vbaProject.bin`; `[Content_Types].xml` carries `macroEnabled` |
| `file exif_gps.jpg` | parses the IFD independently: `manufacturer=OctoConvCam, model=FixtureModel X1, GPS-Data` |
| `go test ./internal/api/ -run TestPre60` | pass |
| `go test ./internal/api/ -run 'TestPre58\|TestPre59\|TestPre60'` | all pass — four `-update-golden*` flags coexist, no `flag redefined` |
| `go test ./... -count=1` | **exit code 0**, captured explicitly rather than read off an unconditional echo |
| `go build ./... && go vet ./...` | clean |
| `gofmt -l internal/ cmd/` | empty |
| `git diff --stat -- go.mod go.sum` | empty |

Local gates only — GitHub Actions was not run.

## What this does not establish

The corpus is 1.9 KB at its largest. R4's "constant PostScript overhead, not a
multiplier" holds on 1.9–36 KB and nothing here speaks to production scale —
Plan 60-06's timeout still needs a measurement on a real corpus. Encrypted
PDFs, non-VBA OLE objects, and image metadata other than EXIF have no fixture;
`PROVENANCE.md` lists all of it under "What this corpus does NOT cover".
