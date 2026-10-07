# Phase 75: DXF 2D Pipeline - Pattern Map

**Mapped:** 2026-10-07
**Files analyzed:** 24 new/modified
**Analogs found:** 21 / 24

All analog paths are git-tracked (`git ls-files` checked).

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `cmd/cad-worker/helper/cad-dxf` (new) | helper (confined engine) | file-I/O + verdict line | `cmd/cad-worker/helper/cad-render` (venv shebang, fd-dup verdict) + `cad-mesh` (`--measure` draft) | exact |
| `cmd/cad-worker/helper/cad-guard` (mod, DXF mode) | helper (stdlib pre-pass) | streaming scan | itself: `scan_step` / `scan_iges` / `main` | exact |
| `cmd/cad-worker/requirements-dxf.txt` (new) | config (hash lock) | - | `cmd/cad-worker/requirements-vtk.txt` | exact |
| `Dockerfile.cad-worker` (mod) | config | build | itself, lines 9-11 and 20-28 | exact |
| `internal/convert/caddxf.go` (new) | service (converter) | staged pipeline | `internal/convert/cadrender.go` + `cad.go` `Convert` | exact |
| `internal/convert/caddxfsvg.go` (new) | utility (validator) | transform | `internal/convert/cadpixel.go` (judge returning `ErrCAD*`) | role-match |
| `internal/convert/caddxfpdf.go` (new) | utility (judge) | transform | `internal/convert/cadpixel.go` `JudgeCADPNG` | role-match |
| `internal/convert/cadpixel.go` (mod, drawing mode) | utility | transform | itself, `JudgeCADPNG` / `cadJudgeFacts` | exact |
| `internal/convert/cad.go` (mod: consts, sentinels, `cadMaxBytes`, stage names, timeouts, classify) | service | - | itself | exact |
| `internal/convert/cadsandbox.go` (mod: DXF/rsvg sets, timeouts, preflight) | config/service | - | itself lines 36-105, 122-288 | exact |
| `internal/convert/cadopts.go` (mod: `ValidateCADApplicability`) | validation | request-response | itself lines 144-166 | exact |
| `internal/convert/cadmeasure.go` (mod: DXF branch in `cadUnitFor`) | transform | - | itself lines 125-165 | exact |
| `internal/convert/converters.go` (mod) | config (registration) | - | itself lines 97-98 | exact |
| `cmd/cad-worker/main.go` (mod: env timeouts, Replace, log line) | entry point | - | itself lines 50-80 | exact |
| `internal/api/api.go` (mod: `defaultMaxFormatBytes[cad]["dxf"]`) | config | - | itself lines 513-519 | exact |
| `internal/convert/caddxf_test.go`, `caddxfsvg_test.go`, `caddxfpdf_test.go` (new) | test | - | `cadrender_test.go`, `cadpixel_test.go` | role-match |
| `internal/convert/cadguard_test.go` (mod, DXF cases) | test (exec helper) | - | itself lines 1-40 (`guardResult`) | exact |
| `internal/convert/cadopts_test.go` (mod) | test (table) | - | itself lines 114-153 | exact |
| `internal/convert/cadsandbox_test.go` (mod, pinning) | test | - | itself | exact |
| `internal/convert/testdata/cad/synthetic/*.dxf` (new fixtures) | fixture | - | existing `blocks_minsert.dxf`, `legacy_cp1251.dxf`; generator `scripts/cad-corpus-gen.py` | exact |
| `.planning/research/SANDBOX-PATHS.md` `## cad` (mod) | doc/pin | - | conf05 blocks lines 1474-1500 | exact |
| `scripts/conf05-crosscheck.sh` (mod: `caddxf`, `cadrsvg` classes) | script | - | itself lines 62-63, 82, 96-98, 111-113 | exact |
| `scripts/cad-worker-sandbox-measure.sh` / new DXF measure + liveproof cases | script | batch | `scripts/cad-worker-sandbox-measure.sh`, `scripts/cad-liveproof.sh`, `scripts/cad-stl-case.sh` | role-match |
| `docs/MEASUREMENTS.md`, ERROR-CODES (doc, if needed) | doc | - | existing dxf rows | exact |

## Pattern Assignments

### `internal/convert/caddxf.go` (converter, staged pipeline)

**Analog:** `internal/convert/cadrender.go` (whole file, 163 lines) and `internal/convert/cad.go:102-135`.

Converter shape (cadrender.go:19-29): embed `CADConverter`, `Operation()`, `Pairs()`. For DXF: a convert converter with pairs `dxf→svg`, `dxf→pdf` (Operation convert) and either extend `CADRenderConverter.Pairs()` with `{dxf, png}` or a separate render type; the registry keys on (op, from, to), so `dxf→png` must not collide.

Entry checks (cadrender.go:31-52), copy order exactly:
```go
parsed, err := CADOptsFromMap(opts)
if err != nil { return fmt.Errorf("cad: %w", err) }
format := NormalizeFormat(filepath.Ext(inPath))
target := NormalizeFormat(filepath.Ext(outPath))
if (format != "step" && ...) || target != "png" {
	return fmt.Errorf("cad: unsupported render %s -> %s", format, target)
}
if err := ValidateCADApplicability(EngineCAD, OperationRender, format, target, parsed); err != nil {
	return fmt.Errorf("cad: %w", err)
}
o := parsed.Resolved()
info, err := os.Stat(inPath)
...
if ceiling := cadMaxBytes(format); info.Size() > ceiling {
	return fmt.Errorf("cad: %w: %s input is %d bytes against a %d-byte ceiling", ErrCADInputTooLarge, format, info.Size(), ceiling)
}
```
Note: `Resolved()` defaults `View` to `iso`; DXF path must not forward it (RESEARCH Pattern 9).

Sidecar cleanup on error (cadrender.go:58-63) — reuse for the measurements second output (D-07/74, D-11):
```go
draftPath := filepath.Join(jobDir, cadMeasureDraftName)
sidecar := CADMeasurementsPath(outPath)
defer func() {
	if err != nil {
		os.Remove(sidecar)
	}
}()
```
Draft → doc → judge → write sidecar (cadrender.go:90-128): `parseCADMeasureDraft`, `cadZeroExtent` → `ErrCADEmptyResult`, `buildCADMeasurements(draft, o.AssumeUnit)`, judge the output, then `json.Marshal(doc)` + `os.WriteFile(sidecar, body, 0o600)`. DXF order: guard → cad-dxf (svg + draft) → Go SVG validate/re-serialize → (svg: done | rsvg pdf → PDF judge | rsvg png → `JudgeCADPNG` drawing mode) → sidecar.

Stage call shape (cadrender.go:141-163 / cad.go:144-158):
```go
x, err := c.cadStage(jobDir, cadRenderReadOnly, cadRenderEngines)
if err != nil { return err }
timeout := c.renderTimeout()
stageCtx, cancel := context.WithTimeout(ctx, timeout)
defer cancel()
out, cpu, err := x.runBounded(stageCtx, cadOutputLimit, "cad-render", "--view", o.View, ...)
v, err := classifyCADStage(cadStageRender, ctx, stageCtx, timeout, out, cpu, err)
```
Then cross-check the verdict echoes the request (cadrender.go:157-161) → `ErrCADRenderRejected`. For `rsvg-convert` there is no CADVERDICT line: write a dedicated classify (timeout → terminal named-stage sentinel; SIGKILL → `ErrCADEngineKilled`; other non-zero exit on validated input → retryable engine fault, per D-13), keeping `errors.Is(err, ErrEngineSandboxUnavailable)` first as in cad.go:235-237.

### `internal/convert/cad.go` (modify)

- Constants block lines 21-42: add `CADMaxDXFBytes` (re-derived per R-7, not blindly 118 MiB), `CADMaxDXFExpandedInserts = 170031`, `CADMaxDXFExpandedEntities = 850155`, `CADMaxDXFNesting = 16`, `CADDefaultDXFTimeout`, `CADDefaultRsvgTimeout`, stage names `cadStageDXF = "dxf"`, `cadStageRsvg = "rsvg"`.
- `cadMaxBytes` switch (lines 71-79): add `case "dxf"`. Default currently falls through to STEP — add the case explicitly.
- Sentinels lines 44-56: add e.g. `ErrCADDXFTimeout`, `ErrCADRsvgTimeout`, `ErrCADOutputRejected` (SVG/PDF judge) in the same `errors.New("cad: ...")` style.
- `classifyCADStage` lines 234-300: the `render := stage == cadStageRender` boolean decides that verdicts `too_complex`/`unreadable` from render become `ErrCADRenderFailed`. The `cad-dxf` stage is a reader, so it must map `unreadable`→`ErrCADUnreadable`, `too_complex`→`ErrCADTooComplex`, `empty`→`ErrCADEmptyResult`; add its timeout case next to lines 268-275 naming the stage.
- `cadVerdict` struct lines 191-202: add DXF fields (skip counts, `declared_unit_raw` lives in the draft, not here).
- Guard argv (lines 151-155) is STEP-specific; DXF branch passes `--format dxf --max-expanded-inserts ... --max-expanded-entities ... --max-nesting 16`.

### `internal/convert/cadsandbox.go` (modify)

Sets (lines 36-57) — add, seeded from RESEARCH Landlock table then re-measured:
```go
var cadGuardReadOnly = []string{
	"/usr/bin/python3.11",
	"/usr/lib/python3.11",
}
var cadGuardEngines = []string{"cad-guard"}
```
→ `cadDXFReadOnly` (`/opt/venv`, python, `/usr/share/fonts`, ...), `cadDXFEngines = {"cad-dxf"}`, `cadRsvgReadOnly`, `cadRsvgEngines = {"rsvg-convert"}`. Multiarch dir is appended automatically by `cadStage` (lines 61-72):
```go
env := append(engineEnv(jobDir), "LP_NUM_THREADS=2")
return c.Sandbox.forJob(jobDir, ro, engines).withEnv(env), nil
```
`engineEnv` (`internal/convert/sandboxwrap.go:219-231`) passes through host HOME; D-14/Pitfall 7 need HOME and `XDG_CACHE_HOME` in a job-dir subpath — add a stage-specific env override rather than changing `engineEnv`/`exec.go`.

Timeouts (lines 79-105): extend `ValidateCADTimeouts` and `ConfinedCAD` signatures with `dxf, rsvg` (env names `CAD_DXF_TIMEOUT`, `CAD_RSVG_TIMEOUT`, R-5), same message format; the DXF sum (guard + dxf + rsvg + `CADIOAllowance`) must stay ≤ engine and < 886 s.

Preflight (lines 115-288): `requireCADDenied` (115-120) and `cadPreflightRender` / `cadPreflightRenderDenied` (241-288) are the both-ways template; embed a small DXF fixture via `//go:embed testdata/cad/synthetic/...` like `cadPreflightSTEP` (line 21-22). Judge success by content (SVG has `<text>`, PDF has text, PNG passes the drawing judge).

### `internal/convert/cadopts.go` `ValidateCADApplicability` (lines 144-166)

```go
switch operation {
case OperationConvert:
	other := o
	other.Quality = ""
	if !other.isZero() {
		return fmt.Errorf("only quality applies to a cad conversion")
	}
case OperationRender:
	if o.Quality != "" && NormalizeFormat(source) == "stl" {
		return fmt.Errorf("quality does not apply to an stl source")
	}
```
Add before the generic convert check: `NormalizeFormat(source) == "dxf"` + convert + any non-zero → error; dxf + render → `View != ""` or `Quality != ""` → error. Note `CADConverter.Convert` (cad.go:107) calls it with empty source — the DXF converter must pass the real source. Handler mapping to 422 already exists (`internal/api/handlers.go:1002-1006`). Tests: append rows to the table in `cadopts_test.go:114-150`.

### `internal/convert/cadmeasure.go` `cadUnitFor` (lines 125-165)

The STL branch is the model for an early return:
```go
switch d.Format {
case measurements.FormatSTL:
	if assumeUnit != "" {
		return assumeUnit, measurements.SourceClientAssumed, 1, nil
	}
	return measurements.UnitUnspecified, measurements.SourceNone, 1, nil
case measurements.FormatSTEP, measurements.FormatIGES:
default:
	return "", "", 0, cadDraftError("format %q has no measurements draft", d.Format)
}
```
Add a `FormatDXF` case: `DeclaredUnit` in `cadMMPerUnit` → declared, factor 1 (do NOT require `cadFactorMatches`); `DeclaredUnitRaw != ""` → unspecified; else assumeUnit → client_assumed. Draft keys unchanged (RESEARCH Pattern 8).

### `internal/convert/caddxfsvg.go` / `caddxfpdf.go` (validators)

**Analog:** `internal/convert/cadpixel.go:61-92` — bounded read then typed rejections:
```go
data, err := io.ReadAll(io.LimitReader(r, cadPixelMaxBytes+1))
if err != nil {
	return facts, fmt.Errorf("cad: read the rendered image: %w", err)
}
if len(data) > cadPixelMaxBytes {
	return facts, fmt.Errorf("%w: the image exceeds %d bytes", ErrCADRenderRejected, cadPixelMaxBytes)
}
```
Return a facts struct + error wrapping a terminal sentinel. SVG: `encoding/xml` Decoder token loop with closed allowlist maps and re-serialize via `xml.Encoder` (RESEARCH Pattern 4). PDF: stdlib regexp/zlib (RESEARCH Pattern 6). No comments (user rule).

### `internal/convert/cadpixel.go` drawing mode

Extend `CADPixelFacts.Mode` (already a string, line 37) and add a drawing branch in `cadJudgeFacts` (line 156) using thresholds from RESEARCH Pattern 7; keep the J0 size check (lines 82-84) and edge-touch/margin checks. Tests mirror `cadpixel_test.go` synthetic-image cases (lines 99-142).

### `cmd/cad-worker/helper/cad-dxf` (new helper)

**Analog:** `cmd/cad-worker/helper/cad-render`.
- Shebang `#!/opt/venv/bin/python -IB` (line 1); stdlib imports, module constants, `EXIT_*` codes, `EXIT_USAGE = 64`.
- `Parser(argparse.ArgumentParser)` overriding `error` to write `cad-dxf: ...` and exit 64 (lines 36-39).
- Verdict line via a duplicated fd so library stdout can't forge it (lines 59-72, 226-233):
```python
fd = os.dup(1)
os.dup2(2, 1)
try:
    return render(fd, args)
except Exception as exc:
    sys.stderr.write("cad-render: %s: %s\n" % (exc.__class__.__name__, exc))
    verdict(fd, args, "failed", "render_exception")
    return EXIT_RENDER
```
`os.write(fd, ("CADVERDICT " + json.dumps(obj, separators=(",", ":"), sort_keys=True) + "\n").encode("ascii"))` with `"stage": "dxf"`. Set HOME/XDG env before `import ezdxf` (RESEARCH Pattern 3). For a reader stage, an unexpected exception must not be classified retryable if deterministic (R-8): the guard pre-empts those.
- Prototype logic source: `scripts/cad-measure/cad_dxf.py` (phase 71) and RESEARCH Pattern 1/3 code.

### `cmd/cad-worker/helper/cad-guard` (DXF mode)

Extend `--format choices=("step", "iges")` (line 287) with `"dxf"`, add new cap args, and a `scan_dxf(path, ...)` alongside `scan_step` (232-256) that raises `Finding("too_complex"|"unreadable", reason, counters)`; `main()` dispatch (lines 291-305) and `emit()` (278-282) unchanged. Keep the `S_ISREG` fstat check and chunked reading (line 233-235). Stays stdlib-only (`#!/usr/bin/python3 -IB`) so its ruleset is unchanged. Tests: `cadguard_test.go` runs the helper by path (`cadGuardHelperPath`, line 20) and parses `guardResult` (lines 26-40).

### `cmd/cad-worker/requirements-dxf.txt` + `Dockerfile.cad-worker`

Dockerfile lines 9-11 (venv stage), copy for the new lock:
```dockerfile
COPY cmd/cad-worker/requirements-vtk.txt /req/requirements-vtk.txt
RUN /opt/venv/bin/pip install --no-cache-dir --only-binary=:all: --no-deps --require-hashes \
      -r /req/requirements-vtk.txt
```
Engine stage: apt line 21 gains `librsvg2-bin fonts-dejavu-core`; lines 24-27 add `COPY cmd/cad-worker/helper/cad-dxf /usr/local/bin/cad-dxf` and chmod; the import smoke line 28 gains `ezdxf, ezdxf.addons.drawing`. R-6: requirement blocks must `cmp`-match `scripts/cad-measure/requirements-common.txt`.

### `internal/convert/converters.go` / `cmd/cad-worker/main.go` / `internal/api/api.go`

- converters.go:97-98 `Default.Register(CADConverter{})`, `Default.Register(CADRenderConverter{})` — add the DXF converter in the single exposure commit.
- main.go:50-63 `envDuration("CAD_GUARD_TIMEOUT", convert.CADDefaultGuardTimeout)` pattern for `CAD_DXF_TIMEOUT`/`CAD_RSVG_TIMEOUT`; main.go:65-66 `convert.Default.Replace(...)` for the new converter; update the 🔒 log at line 79 to list cad-dxf and rsvg-convert.
- api.go:513-519 add `"dxf": convert.CADMaxDXFBytes`; also check the engine-level `convert.EngineCAD: convert.CADMaxIGESBytes` (line ~509) still bounds the largest per-format ceiling.

### `.planning/research/SANDBOX-PATHS.md` + `scripts/conf05-crosscheck.sh`

Doc blocks (SANDBOX-PATHS.md:1474-1480):
```
<!-- conf05:cadguard:readonly -->
/usr/bin/python3.11
/usr/lib/python3.11
<!-- /conf05:cadguard:readonly -->
<!-- conf05:cadguard:engines -->
cad-guard
<!-- /conf05:cadguard:engines -->
```
Add `caddxf` and `cadrsvg` blocks. In conf05-crosscheck.sh: `cad` expansion line 63 (`classes+=(cadguard cadmesh cadrender)`), go-file mapping line 82, readonly var names lines 96-98, engine var names lines 111-113, and the unknown-class message line 83.

## Shared Patterns

### Verdict protocol
**Source:** `internal/convert/cad.go:191-220` (`cadVerdict`, `parseCADVerdict` takes last `CADVERDICT ` line and requires `v.Stage == stage`). Apply to cad-guard DXF mode and cad-dxf.

### Failure classes
**Source:** `internal/convert/cad.go:234-300` + codes in `internal/worker/descriptors.go:62-64` (`cad_too_complex`, `cad_unreadable`, `cad_empty_result`). Sandbox-unavailable first, then verdict, then stage deadline, then parent cancel, then signal.

### Error wrapping
`fmt.Errorf("cad %s: %w: %s", stage, ErrX, reason)` for stages, `fmt.Errorf("cad: %w: ...", ErrX, ...)` in converters. Library code never logs.

### Tests
stdlib `testing`, table-driven, `strings.Contains(err.Error(), want)` (cadopts_test.go:140-150); helper tests exec the helper by relative path (cadguard_test.go:19-24). Run with `GOTOOLCHAIN=go1.26.4 go test ./... -count=1`.

### Evidence scripts
`scripts/cad-worker-sandbox-measure.sh` (removal table on production image), `scripts/cad-liveproof.sh` / `scripts/cad-stl-case.sh` (differential live cases), `scripts/cad-corpus-gen.py` (fixtures). Transcripts carry commit + tracked-dirt headers.

## No Analog Found

| File | Role | Data Flow | Reason |
|---|---|---|---|
| SVG allowlist validate/re-serialize (`caddxfsvg.go`) | utility | transform | no XML validator exists; use RESEARCH Pattern 4 |
| PDF judge (`caddxfpdf.go`) | utility | transform | no PDF parsing in repo; use RESEARCH Pattern 6 prototype |
| ezdxf text-as-`<text>` pipeline override inside `cad-dxf` | helper | transform | new; use RESEARCH Pattern 1 |

## Metadata

**Analog search scope:** `internal/convert/cad*.go`, `cmd/cad-worker/**`, `Dockerfile.cad-worker`, `internal/api/api.go`, `scripts/cad-*.sh`, `scripts/conf05-crosscheck.sh`, `.planning/research/SANDBOX-PATHS.md`
**Files scanned:** ~20
**Pattern extraction date:** 2026-10-07
