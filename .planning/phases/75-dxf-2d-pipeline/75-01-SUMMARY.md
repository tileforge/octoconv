---
phase: 75-dxf-2d-pipeline
plan: 01
subsystem: cad-worker engine (DXF)
tags: [ezdxf, rsvg-convert, landlock, dxf, svg-text, fixtures, supply-chain]

requires:
  - phase: 71-evaluation-gate
    provides: approved hash lock scripts/cad-measure/requirements-common.txt, dxf_path ezdxf-svg+rsvg-convert, renderer_user_approval
  - phase: 74-3d-render-measurements
    provides: engine stage of Dockerfile.cad-worker, cad-render verdict pattern, sandbox-probe tracer method
provides:
  - cmd/cad-worker/requirements-dxf.txt (five non-numpy blocks of the 71 lock, 183 hashes)
  - Dockerfile.cad-worker engine stage with ezdxf, librsvg2-bin, fonts-dejavu-core and /usr/local/bin/cad-dxf
  - cad-dxf helper core (SRC SVG, CADVERDICT stage dxf, DXF text as SVG text elements)
  - scripts/cad-dxf-probe.sh tracer and fixtures modes
  - 41 deterministic DXF fixtures under internal/convert/testdata/cad/dxf and TestDXFFixturesSniffAsDXF
affects: [75-02, 75-03, 75-04, 75-06, 75-07, 75-08, 75-09, 75-10]

actuals:
  tokens: 24140
  tasks: 3
  commits: 5
plan_head_before: 23b5ce48e9ddd4f4c468b547f2b0ab9347820174
plan_head_after: 6e2972080da63465baaedb9da73d2993d744dd06

tech-stack:
  added: [ezdxf 1.4.4, fonttools 4.66.1, pillow 12.3.0, pyparsing 3.3.3, typing-extensions 4.16.0, librsvg2-bin 2.54.7+dfsg-1~deb12u1, fonts-dejavu-core 2.37-6]
  patterns:
    - "RenderPipeline2d.draw_text override records text plus Matrix44; text emitted as <text transform=matrix(...)> after the page transform, font size folded out of the matrix"
    - "SVGRenderBackend subclass writing presentation attributes instead of CSS classes; output_coordinate_space 10000"
    - "Frame = recorded primitive boxes plus text glyph boxes; XLINE/RAY stashed and drawn clipped to the frame"
    - "Probe evidence: one shared LOCK line, per-mode HDR/SUMMARY lines, mode re-run replaces its own lines"

key-files:
  created:
    - cmd/cad-worker/requirements-dxf.txt
    - cmd/cad-worker/helper/cad-dxf
    - scripts/cad-dxf-probe.sh
    - internal/convert/caddxf_fixtures_test.go
    - internal/convert/testdata/cad/dxf/*.dxf (41 files)
    - .planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt
  modified:
    - Dockerfile.cad-worker
    - scripts/cad-corpus-gen.py
    - internal/convert/testdata/cad/PROVENANCE.md

key-decisions:
  - "75-01: cad-dxf maps RecursionError and MemoryError to too_complex during drawing as well as during read/audit; any other draw exception is failed/draw_exception"
  - "75-01: cad-dxf reports a missing TrueType font as failed/font_unavailable (an engine fault), never as silently dropped text"
  - "75-01: entities_drawn counts every Frontend.draw_entity call, virtual block entities included (the unit the 75-02 backstop counts)"
  - "75-01: a zero-width or zero-height frame is padded to the 1/1000 aspect clamp around its centre; a zero-size frame becomes 1 x 1 drawing unit"
  - "75-01: XLINE/RAY are intercepted in draw_line_entity (top-level and inside blocks), excluded from the frame and drawn clipped to it after the main pass"
  - "75-01: wrong_extents and header_far_extents are byte-edited after save because ezdxf 1.4.4 rewrites $EXTMIN/$EXTMAX on saveas"
  - "75-01: the TestDXFFixturesSniffAsDXF expected-rejection map stays empty: all 41 fixtures sniff as dxf with no rejection"

patterns-established:
  - "Fixture generation for DXF: ezdxf drawings under PYTHONHASHSEED=0 + write_fixed_meta_data_for_testing, stdlib R12 pair writer for bombs and numeric cases, stdlib --check-only markers"

requirements-completed: [CCONV-02, CCONV-03]

coverage:
  - id: D1
    description: "requirements-dxf.txt is a byte-for-byte copy of the five non-numpy blocks of the approved 71 lock and is installed with --require-hashes --no-deps --only-binary"
    requirement: CCONV-02
    verification:
      - kind: other
        ref: "awk extraction | cmp - cmd/cad-worker/requirements-dxf.txt; LOCK line in evidence/75-01-engine.txt"
        status: pass
    human_judgment: false
  - id: D2
    description: "cad-dxf under its Landlock ruleset draws legacy_cp1251.dxf to an SVG whose only text is a <text> reading Деталь, and uncommon.dxf with 49 text elements"
    requirement: CCONV-02
    verification:
      - kind: integration
        ref: "scripts/cad-dxf-probe.sh tracer (TRACER stage=cad-dxf rows)"
        status: pass
    human_judgment: false
  - id: D3
    description: "rsvg-convert under its own ruleset turns both SVGs into PDFs whose pdftotext output contains the SVG text, and into 1280 x 1280 letterboxed PNGs"
    requirement: CCONV-03
    verification:
      - kind: integration
        ref: "scripts/cad-dxf-probe.sh tracer (TRACER stage=rsvg-pdf and rsvg-png rows)"
        status: pass
    human_judgment: false
  - id: D4
    description: "A DXF or an SVG outside every grant yields no SVG and no PDF (negative controls with unconfined controls)"
    verification:
      - kind: integration
        ref: "scripts/cad-dxf-probe.sh tracer (result=denied rows)"
        status: pass
    human_judgment: false
  - id: D5
    description: "41 DXF fixtures regenerate byte for byte, read as expected by ezdxf, and sniff as dxf"
    verification:
      - kind: integration
        ref: "scripts/cad-dxf-probe.sh fixtures (two consecutive runs)"
        status: pass
      - kind: unit
        ref: "internal/convert/caddxf_fixtures_test.go#TestDXFFixturesSniffAsDXF"
        status: pass
    human_judgment: false

duration: 27min
completed: 2026-10-07
status: complete
---

approved: 71 lock admitted (Lock-файлы Python допущены, renderer_user_approval approved_2026-10-04), five blocks byte-identical, 183 hashes, numpy block identical to requirements-mesh.txt

# Phase 75 Plan 01: DXF engine tracer and fixture set Summary

**ezdxf 1.4.4 from the approved 71 lock draws DXF text as real SVG `<text>` under a Landlock ruleset in the production engine image; rsvg-convert under its own ruleset turns it into a PDF from which pdftotext extracts `Деталь`, and 41 deterministic DXF fixtures are committed and sniff as dxf.**

## Performance

- **Duration:** about 27 min (2026-10-07 10:55Z to 11:22Z)
- **Tasks:** 3 of 3 (Task 1 answered by the orchestrator under the v1.16 delegation, re-checked here)
- **Files:** 49 changed (41 of them generated fixtures)

## Task 1: R-6 checkpoint, re-run by the executor

The resume answer is recorded verbatim at the top of this file. Steps 1 to 3 were re-run on the working tree before any install:

Step 1, `awk '/^[A-Za-z0-9_.-]+==/{keep = ($0 !~ /^numpy==/)} keep' scripts/cad-measure/requirements-common.txt | grep -E '^[A-Za-z0-9_.-]+=='`:
```
ezdxf==1.4.4 \
fonttools==4.66.1 \
pillow==12.3.0 \
pyparsing==3.3.3 \
typing-extensions==4.16.0 \
```
Step 2, the same extraction: `grep -c 'hash=sha256:'` prints `183`; `shasum -a 256` prints `45d7e1b2e1722ef0b8f8693b3693aceb1ba704c6f7056ecea584182737f3231b`.

Step 3, the numpy block of `cmd/cad-worker/requirements-mesh.txt` against the numpy block of the 71 lock: `cmp` exit `0`.

71-DECISION.md line 45: `DECISION,key=renderer_user_approval,value=approved_2026-10-04,evidence=71-05-SUMMARY.md#User acceptance (D-11 step 2); evidence/71-05-contact-f3d.png`. Line 86: `| Lock-файлы Python | допущены, установка только `pip --require-hashes` | пользователь |`.

Comparison with the orchestrator's check outputs: the five package lines, the hash count 183, the subset digest, the numpy cmp result and both 71-DECISION lines match. No mismatch, so execution continued.

## Prior attempt leftovers

The dead executor left `Dockerfile.cad-worker` modified and `cmd/cad-worker/requirements-dxf.txt` untracked. `requirements-dxf.txt` was byte-identical to the step-1 extraction (`cmp` exit 0). The Dockerfile delta was exactly the planned change: the third hash-locked pip install, the apt line `python3 libgl1 libx11-6 libosmesa6 librsvg2-bin fonts-dejavu-core ca-certificates`, the `cad-dxf` COPY and chmod, the ezdxf import smoke under a throwaway HOME removed in the same RUN, and `rsvg-convert --version`. Both were kept. It had also left an image tag `octoconv-cad-worker:engine75-devwork` (with a 27-byte stub `cad-dxf`). That tag was removed.

## Accomplishments

- `cmd/cad-worker/requirements-dxf.txt`: five blocks, 183 hashes, sha256 `45d7e1b2…231b`. numpy is not installed twice.
- `cmd/cad-worker/helper/cad-dxf`: `#!/opt/venv/bin/python -IB`, mode 100755, no comment lines, no OCP/VTK import. It saves fd 1 and points fd 1 and `sys.stdout` at stderr before importing ezdxf. It then does a strict `ezdxf.readfile` plus `audit()`: code 104 gives too_complex/block_cycle, any other unfixed error gives unreadable/audit_error, and read exceptions give unreadable/read_error. The `TextPipeline.draw_text` override records each string with its matrix, em size, baseline shift, family, weight, colour and glyph box. The configuration is background OFF, image IGNORE, proxy graphic SHOW and colour COLOR. The page is 297 mm on the long side with 10 mm margins and a coordinate space of 10000. Text is set through ElementTree. The last stdout line is `CADVERDICT` with the keys stage, verdict, reason, texts, entities_drawn, page_w_mm, page_h_mm, auditor_fixes, insunits, codepage and skipped.
- `scripts/cad-dxf-probe.sh`: `tracer` and `fixtures [--write]`, with usage in a heredoc and no comment lines besides the shebang. Exit codes are 64, 69, 3 and 1. It builds `octoconv-cad-worker:engine75-<12 hex>` with `--target engine`, refuses an emulated image, guards the VM disk at 6 GiB through `docker run --rm alpine df -k /` and removes every tag it built (`docker images | grep -c engine75-` prints 0).
- Tracer at 9e5e86f, with only the orchestrator's `config.json` dirty, under `--network none --cpus 2 --memory 4g --memory-swap 4g --user nobody` and HOME/XDG under `/job/.home`, TMPDIR `/job`, LANG unset:

| Run | Result | Key facts |
|---|---|---|
| cad-dxf legacy_cp1251 | ok | 1 text element, `Деталь`; elements ⊆ {svg, defs, rect, g, path, text}; no style/class/href/id/on* |
| rsvg-pdf legacy_cp1251 | ok | pdftotext output contains `Деталь` (6 chars of 6) |
| rsvg-png legacy_cp1251 | ok | IHDR 1280 x 1280, scaled 1203 x 349 at offset 38+465 |
| cad-dxf uncommon | ok | 49 text elements, verdict texts 49, 230 entities drawn |
| rsvg-pdf uncommon | ok | the 299 SVG text chars are an ordered subsequence of the 299 pdftotext chars |
| rsvg-png uncommon | ok | IHDR 1280 x 1280, scaled 1203 x 1043 |
| cad-dxf DXF at /outside | denied | rc 2, unreadable/read_error, no SVG; the unconfined control produced an SVG |
| rsvg-pdf SVG at /outside | denied | rc 1, `Error opening file /outside/draw.svg: Permission denied`, no PDF; the unconfined control produced a PDF |

- Fixture set: `scripts/cad-corpus-gen.py --dxf-fixtures [--out-dir DIR] [--check-only]` writes the 41 files. The probe's `fixtures` mode passed twice in a row without `--write`: 41 fixtures, 41 identical, 0 failed expectations. All drawing, units and extents fixtures read strict with no audit error. All four cyclic fixtures show audit code 104.
- `TestDXFFixturesSniffAsDXF`: 41 subtests pass and the 72 golden test passes. The expected-rejection map stays empty, because no fixture is rejected by the sniffer.
- PROVENANCE.md has a `dxf/` section giving the generator command, the determinism settings, a table of group purposes, and the statement that all content is generated.

## Fixture facts recorded for later plans (not judged here)

| Fixture | ezdxf strict read | Note |
|---|---|---|
| nonnumeric_abc, nonnumeric_comma | DXFStructureError | already terminal at read |
| nonnumeric_nan, nonnumeric_inf, nonnumeric_underscore, coord_1e300, coord_1e400 | ok, no audit error | ezdxf accepts them; the guard of 75-03 must reject them. A smoke run of cad-dxf on nonnumeric_inf gave failed/draw_exception (`ValueError: non-finite drawing extents`), which is retryable without the guard |
| nonfinite_header/tables/objects | ok, no audit error | `$LTSCALE`=nan, VPORT code 40=inf, LAYOUT (AcDbPlotSettings) code 40=`1_0`; the input for the R-8 scope check |
| nest_d16, nest_d17, minsert_over_inserts, entities_over, dimension_minsert | ok, no audit error | the bounds belong to the guard (75-03) |

Other cad-dxf smoke results (unconfined, not evidence): text_cases gave ok with 18 texts. layers_off_frozen gave ok with 1 text, so the hidden layers were absent. all_hidden gave empty/nothing_drawable. wide and tall gave pages of 297 x 22.8 mm and 22.8 x 297 mm. wrong_extents gave a page of 297 x 112.3 mm, which comes from the LINE and not from the header. cyclic_self gave too_complex/block_cycle.

## Decisions Made

- These follow the key-decisions in the frontmatter.
- The LOCK line is shared by both modes and replaced on every run, so the evidence always holds exactly one LOCK line. HDR and SUMMARY lines are tagged by mode.
- Fixture construction choices:
  - `text_illegal_chars` is an ezdxf R2018 file whose placeholder TEXT value is byte-edited to `AB\x01CD\xffEF`. A full R2018 file keeps the other sections valid, so the illegal bytes are the only anomaly.
  - Bombs and numeric cases are written as R12 pairs with the standard library.
  - `minsert_over_inserts` is 16 x 10627 = 170032 cells.
  - `entities_over` is 851 separate INSERTs of a 1000-entity block.
  - `header_far_extents` has $EXTMIN at -1e+20 and $EXTMAX at +1e+20.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Empty-array expansion under the host's bash 3.2**
- **Found during:** Task 2
- **Issue:** `/usr/bin/env bash` on the host is GNU bash 3.2. With `set -u`, `"${MOUNTS[@]}"` aborts when the array is empty.
- **Fix:** `${MOUNTS[@]+"${MOUNTS[@]}"}`
- **Files modified:** scripts/cad-dxf-probe.sh
- **Commit:** 2862258

**2. [Rule 1 - Bug] The fixtures-mode header named the tracer's stage environment**
- **Found during:** Task 3, while re-reading the evidence
- **Issue:** The HDR env line and the ruleset line described the confined stage env, but the fixtures mode runs the generator and the reader unconfined with HOME=/tmp and PYTHONHASHSEED=0.
- **Fix:** The mode-specific header now records the actual environment. Both modes were re-run at the fixed commit.
- **Files modified:** scripts/cad-dxf-probe.sh
- **Commit:** 9e5e86f, with the evidence in 6e29720

**3. [Rule 3 - Blocking] ezdxf rewrites $EXTMIN/$EXTMAX on save**
- **Found during:** Task 3
- **Issue:** Setting the header extents through ezdxf did not survive `saveas`.
- **Fix:** A stdlib byte edit of the header point after save (`dxf_set_header_point`). The `--check-only` markers pin the edited values.
- **Files modified:** scripts/cad-corpus-gen.py
- **Commit:** 58728b6

Two evidence-only commits (42c8d41, 6e29720) re-ran the probe at the committed code, so that each HDR commit names a tree that contains the code it measured. 42c8d41 is superseded by 6e29720.

## Known Stubs

| File | Line | Reason |
|---|---|---|
| cmd/cad-worker/helper/cad-dxf | `new_state()` `"skipped": {}` | By the plan's contract, the verdict key `skipped` is empty in 75-01. Plan 75-02 adds the entity filter (IMAGE/UNDERLAY/OLE/XREF/proxy/3DSOLID) that fills it. Until then IMAGE stays undrawn through `ImagePolicy.IGNORE`, and Landlock is the second line. |

The broken-windows ledger append was not attempted: this run had no `gsd_run` resolver in its shell, and the ledger is optional.

## Threat Flags

None. The probe and the generator are local measurement tools. No network endpoint, auth path or schema changed. No dxf pair is registered (`TestDefaultRegistersOnlyAllowlistedCADPairs` passes unchanged). `internal/api`, `internal/worker`, `internal/queue`, `docker-compose.yml`, `deploy/`, `internal/convert/exec.go`, `scripts/cad-measure/` and `Dockerfile.cad-measure` have no diff against main.

## Verification

- Task 2 gate: `bash -n`, `ast.parse(feature_version=(3,11))`, the lock `cmp`, `scripts/cad-dxf-probe.sh tracer | tail -20 | grep '^TRACER-OK$'`, and the protected-path `git diff --exit-code main` all pass.
- Task 3 gate: `--check-only` passes. `fixtures` gives `SUMMARY … result=ok`. There are 41 files. `TestDXFFixturesSniffAsDXF` and `TestSniffCADGolden` pass. No added `//` lines in the test.
- `GOTOOLCHAIN=go1.26.4 go build ./... && go vet ./...` is green. `go test ./... -count=1` is green, including internal/repohygiene.
- Evidence: 1 LOCK line with result=ok, 6 TRACER lines with result=ok, 2 with result=denied, TRACER-OK, 41 FIXTURE lines and both SUMMARY lines with result=ok.
- After the runs the VM has 8.6 GiB free and no `engine75-` tags remain.

## Self-Check: PASSED

All six key files exist; commits 2862258, 58728b6, 42c8d41, 9e5e86f and 6e29720 are ancestors of HEAD; STATE.md and ROADMAP.md are unchanged since 23b5ce4.
