# Phase 76: Measured Bounds, KEDA Proof & Client Docs - Pattern Map

**Mapped:** 2026-10-13
**Files analyzed:** 30
**Analogs found:** 28 / 30

All analog paths below were checked with `git ls-files` (tracked source, no mirrors). Everything planned carries no code comments (user rule). Analogs that are comment-heavy (keda-pandoc/ocr, ci-local, estimate.go) are copied for shape only; strip their comments.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match |
|---|---|---|---|---|
| `scripts/cad-dxf-bounds.sh` (new modes: 3D stage rows, E2E/IO/POD rows, `--out` 76) | measurement script | batch | itself (`run` mode) | exact |
| `scripts/cad-corpus-gen.py` (exact-byte padding, `stl-stack`) | utility/generator | file-I/O | itself: `stl-tile` (lines ~1104-1131), `dxf-replicate` | exact |
| `scripts/cad-image-size-measure.sh` (`--image REF` mode) | measurement script | batch | itself | exact |
| `scripts/cad-go-worker-rss.sh` (cad-worker + 18090) | measurement script | request-response | itself | exact |
| `scripts/cmd/cad-png-compare/main.go` (new) | CLI utility | transform | `scripts/cmd/cad-pixel-judge/main.go` | exact |
| `scripts/keda-cad-loadproof.sh` (new) | live-proof script | event-driven | `scripts/keda-pandoc-loadproof.sh` | exact |
| `scripts/keda-cad-downscale-survival.sh` (new) | live-proof script | event-driven | `scripts/keda-ocr-downscale-survival.sh` (+ `keda-pandoc-downscale-survival.sh`) | exact |
| `scripts/cad-liveproof.sh` (WORKER_KILL_RENDER, WORKER_KILL_RSVG, DETERMINISM, L170031 refusal) | live-proof script | request-response | its own WORKER_KILL row (lines 1527-1580), `proc_pids`/`mesh_census` (265, 281) | exact |
| `scripts/ci-local.sh` (e2e knobs: port, extra compose files, stop teardown) | config/CI script | batch | its own `tier_e2e` (lines 160-210) | exact |
| final-gate wrapper script (name at planner's discretion) | CI script | batch | `scripts/ci-local.sh` tier runner + `cad-liveproof.sh` HDR/ev lines | role-match |
| `internal/convert/cad.go` (timeouts, `CADIOAllowance`, ceilings) | model/constants | config | itself | exact |
| `internal/convert/cad_test.go` (retarget pins to 76 evidence) | test | file-I/O | `TestCADDXFConstantsComeFromTheEvidence` (lines 737-770), `cadDerivedValue` (720-735) | exact |
| `internal/convert/cadsandbox_test.go`, `internal/queue/queue_test.go`, chart/compose IN-02 tests | test | config | existing literals (cadsandbox_test.go 402-427, 635-643) | exact |
| `internal/convert/caddxfpdf.go` (D-07, line 1485) | service | transform | `cadFailureCode` ordering, descriptors.go 733/757 | exact |
| `internal/worker/pinning_test.go` (D-07 row) | test | transform | row at line 1541 | exact |
| `internal/api/estimate.go` (flip cad to measured) | config map | request-response | tectonic entries (lines ~134-160) | exact |
| `.env.example`, `docker-compose.yml`, chart `values.yaml`/`configmap.yaml`/`values-loadproof.yaml` | config | config | existing CAD block; `values-loadproof.yaml` ocr/pandoc keys | exact |
| `internal/api/api.go` size map (only if D-04 fires) | config/service | request-response | `defaultMaxFormatBytes` + `cadMaxBytes` | role-match |
| `docs/CAD.md` (new) | doc | n/a | `docs/MEASUREMENTS.md`, `docs/ERROR-CODES.md` | role-match |
| `docs/PREVIEW.md` (new) | doc | n/a | `docs/MEASUREMENTS.md` | role-match |
| `internal/convert/cad_doc_test.go` (CAD.md both ways, internal package) | test | file-I/O | `internal/convert/cad_docs_sync_test.go` + `internal/errcode/doc_sync_test.go` | exact |
| `internal/convert/preview_doc_test.go` (PREVIEW.md vs constants) | test | file-I/O | `cad_docs_sync_test.go` | exact |
| `docs/ERROR-CODES.md`, `docs/README.md`, `docs/MEASUREMENTS.md` edits | doc | n/a | existing | exact |
| `docs/openapi.yaml` + `internal/api/openapi_*_sync_test.go`, `internal/errcode/openapi_sync_test.go` | doc + test | file-I/O | existing openapi sync tests | exact |
| worker cad-code-set test (Open Q5) | test | transform | `internal/errcode/doc_sync_test.go` (both-ways tables) | role-match |
| `internal/mcpserver/tools.go`, `client.go:112` + new description test | tool schema + test | request-response | `tools.go:68` Operation jsonschema tag | role-match |
| `frontend/src/formats.ts`, `App.tsx`, `formats.test.ts` | component/utility + test | request-response | `formats.ts` `targetFormatsFor`, `formats.test.ts` | exact |
| `.planning/seeds/SEED-007.md` pointer | doc | n/a | — | n/a |
| evidence files under `76-*/evidence/` | evidence | batch | `75-dxf-2d-pipeline/evidence/75-05-bounds.txt` | exact |
| `scripts/cad-stack-probe` (if separate from bounds) | measurement | batch | `cad-dxf-bounds.sh` RULE/BOUND_ROW | role-match |

## Pattern Assignments

### `scripts/cad-dxf-bounds.sh` (extend) — analog: itself, lines 1-69
Header shape to keep (no comments besides shebang):
```bash
#!/usr/bin/env bash

set -euo pipefail
export LC_ALL=C

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO_ROOT"
. "$REPO_ROOT/scripts/cad-measure/common.sh"

PHASE_DIR="$REPO_ROOT/.planning/phases/75-dxf-2d-pipeline"
EVID="$PHASE_DIR/evidence/75-05-bounds.txt"
...
MIN_FREE_GIB=6
LIMITS=(--network none --cpus 2 --memory 4g --memory-swap 4g --user nobody)
STAGE_ENV=(-e HOME=/job/.home -e XDG_CACHE_HOME=/job/.home/.cache -e XDG_CONFIG_HOME=/job/.home/.config -e TMPDIR=/job)
HELPERS=(cad-mesh cad-guard cad-render cad-dxf)
GO_CLIS=(cad-svg-check cad-pdf-check cad-pixel-judge)
```
- New modes go in `usage()` (heredoc USAGE, exit 64) beside `smoke|run|floor`. Exit codes: 64 usage, 69 tool missing, 3 disk floor, 1 STOP.
- The 76 mode must default `EVID` to a 76 file, never the 75 file (`TestCADDXFConstantsComeFromTheEvidence` still reads 75-05).
- Lines: `RULE,...` written before any row, then `BOUND_ROW,...`, then `DERIVED,key=<k>,value=<int>,derivation=...`. Integer value is required by the Go regex.
- Confinement: `scripts/cmd/sandbox-probe` with `PROBE_RO`/`PROBE_RW` (lines ~787-815); wall/peak via `scripts/cad-measure/measure_wrap.py`.

### `scripts/cmd/cad-png-compare/main.go` (new) — analog `scripts/cmd/cad-pixel-judge/main.go` lines 1-40
```go
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/apaderin/octoconv/internal/convert"
)
...
func main() {
	size := flag.Int("size", 0, "requested frame size in pixels")
	background := flag.String("background", "opaque", "opaque or transparent")
	...
	flag.Parse()
	if ... flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: cad-pixel-judge --print-letterbox --svg FILE --size N")
		os.Exit(64)
	}
```
Reuse `convert.JudgeCADPNG` / `JudgeCADDrawingPNG` for J1-J4; stdlib `image/png` for MAD, >16/255 fraction, ink bbox, alpha mode. Canonical JSON: `json.Decoder.UseNumber()` then re-marshal. Add a `_test.go` beside it (pure Go).

### `scripts/keda-cad-loadproof.sh` — analog `scripts/keda-pandoc-loadproof.sh` (714 lines)
Copy: `PASS_COUNT` + assert helpers (lines 109-168), `teardown` + `trap teardown EXIT` (174-227), preconditions (kubectl context 240, compose down 244, image present 250, KEDA version re-verify 259, external.metrics Available 289), helm install (305-310), queue seeding (329):
```bash
kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli SADD asynq:queues image document html audio av ocr pandoc webhook >/dev/null
```
Changes: add `cad` to the SADD list; ports `API_LOCAL_PORT=18099`, `DB_LOCAL_PORT=15443`; images `octoconv-cad-worker:dev`, `-api`, `-webhook-worker`, `-mcp-http`; calibration step (`/healthz` 200 via port-forward, cad pod log `cad sandbox proven`, Ready within startupProbe) before timing; record pod `imageID`. Teardown removes only `octoconv` and `keda` releases; never touch `identity-stand`/`envoy-gateway-system`; do not toggle k8s. Strip all comments. The compose-up message must not say `down -v` (use `stop`).

### `scripts/keda-cad-downscale-survival.sh` — analog `scripts/keda-ocr-downscale-survival.sh` (744 lines)
Same skeleton as above; victim pinning via `controller.kubernetes.io/pod-deletion-cost=-1000` on the busy pod; requires `keda.cad.scaleDownStabilizationSeconds: 15` in `deploy/chart/octoconv/values-loadproof.yaml` (copy the ocr/pandoc key shape there). LONG job calibrated offline first (pandoc `--calibrate-only` band check precedent).

### `scripts/cad-liveproof.sh` new rows — analog WORKER_KILL, lines 1527-1580
Core sequence to copy:
```bash
PRE_STARTED=$(docker inspect -f '{{.State.StartedAt}}' "$CNAME")
docker kill --signal KILL "$CNAME" >/dev/null
T_KILL=$(now)
... wait for new StartedAt, else docker compose up -d cad-worker; RESTART_VIA=compose_up|restart_policy
CENSUS=$(mesh_census)
ORPHAN_COUNT=${CENSUS% *}
[ "$ORPHAN_COUNT" = 0 ] || fail "WORKER_KILL: ..."
wait_job "$KILL_JOB" "$ENGINE_TIMEOUT"
within "$KILL_TO_DONE" "$ENGINE_TIMEOUT" || fail "..."
RECONCILER=$(docker compose ps --status running --services 2>/dev/null | grep -cx webhook-worker || true)
ev "WORKER_KILL,job=...,orphans=0,...,kill_to_done_s=$KILL_TO_DONE,cad_engine_timeout_s=$ENGINE_TIMEOUT,...,events=$EVENTS"
```
Helpers: `ev()` line 61, `fail()` 63, `wait_job()` 220, `proc_pids()` 265, `mesh_census()` 281. Generalize the census to `cad-guard cad-mesh cad-render cad-dxf rsvg-convert`. RSVG kill: wait for an `rsvg-convert` whose `/proc/<pid>/cmdline` names the output PNG, not `ink.png`. Process-gone pattern for a single engine: DXF_CANCEL row (lines ~1520-1525, `LEFT_DXF=$(proc_pids cad-dxf)`).

### `scripts/ci-local.sh` tier_e2e knobs — analog lines 160-210
Hard-coded points to parameterize with defaults unchanged: `for port in 8090 9100`, `http://localhost:8090/healthz`, `E2E_BASE_URL=http://localhost:8090`, and teardown `"${COMPOSE_E2E[@]}" down -v` (add a `stop` mode). Note: an explicit `-f` list does not auto-load `docker-compose.override.yml`.

### `internal/convert/cad_test.go` pins — analog lines 720-770
```go
func cadDerivedValue(t *testing.T, evidence, key string) int64 {
	...
	m := regexp.MustCompile(`(?m)^DERIVED,key=` + regexp.QuoteMeta(key) + `,value=(\d+),`).FindSubmatch(raw)
```
```go
		if want := cadDerivedValue(t, c.evidence, c.key); c.got != want {
			t.Errorf("%s = %d, %s DERIVED %s says %d: re-derive it with %s rather than editing the expectation",
				c.name, c.got, c.evidence, c.key, want, c.script)
		}
```
Add a 76 table (`CADDefaultGuard/Mesh/Render/DXF/RsvgTimeout`, `CADIOAllowance`, `CADDefaultEngineTimeout`, ceilings) against the 76 bounds file; retarget the hard-coded wants in `TestCADPinnedConstantsComeFromTheEvidence` (lines 146-172). Also move literals in `cadsandbox_test.go:402-427,635-643`, `queue_test.go` (`CADUniqueTTL`), chart grace and compose IN-02 tests together. Route sums in `cadsandbox.go:158-165` stay the enforcement.

### `internal/convert/caddxfpdf.go` D-07 — line 1485
Today:
```go
return facts, nil, cadPDFReject("the PDF is %d bytes, over the %d byte limit", st.Size(), maxBytes)
```
`cadFailureCode` checks `ErrCADTooComplex` (descriptors.go:733) before `ErrCADPDFRejected` (757), so wrap with `fmt.Errorf("%w: %w", ErrCADTooComplex, cadPDFReject(...))`. Failing-first test: add a row to `internal/worker/pinning_test.go` beside line 1541 (`{"dxf pdf rejected", convert.ErrCADPDFRejected, ...}`) and an `errors.Is(err, ErrCADTooComplex)` test in `internal/convert`.

### `internal/api/estimate.go` — analog tectonic entries
```go
	convert.EngineTectonic: EstimateSourceMeasured,
	convert.EngineCAD:      EstimateSourceUnmeasured,
...
var engineMeasuredDefaultSeconds = map[string]int{
	...
	convert.EngineTectonic: 271,
}
```
Flip `EngineCAD` to `EstimateSourceMeasured` and add `convert.EngineCAD: <value>,` in the same commit as the number and `.env.example` line ~598. No new comments. Held by `TestEstimateSourceMatchesEnvExampleProvenance` (`internal/api/estimate_test.go:581`): own line must not contain "not measured"/"placeholder", block must contain "measur".

### `internal/convert/cad_doc_test.go` + `preview_doc_test.go` — analog `internal/convert/cad_docs_sync_test.go` (213 lines) and `internal/errcode/doc_sync_test.go` (282)
`cad_docs_sync_test.go` is `package convert_test`, uses `readDoc`, `docRow` and `convert.Default.OperationClasses()`:
```go
	classes := convert.Default.OperationClasses()
	for _, op := range []string{convert.OperationConvert, convert.OperationRender} {
		pairs := classes[op][convert.EngineCAD]
		...
		for _, p := range pairs {
			for _, f := range []string{p.From, p.To} {
				if !strings.Contains(converts, "`"+f+"`") {
```
For the option vocabularies (`cadViews`, `cadQualities`, `cadBackgrounds`, `cadUnits`, unexported, `cadopts.go:21-28`) and `cadMarginMin`/`cadBackgroundCutoff` (`cadpixel.go:14-31`), place the new tests in `package convert` (internal) so no API is added. Reverse direction ("every one the doc names exists") follows `errcode/doc_sync_test.go`: extract a table section between `## ` headers and compare sets both ways. Applicability: call `ValidateCADApplicability`, do not restate rules. 3D margin: parse `MARGIN = 0.06` from `cmd/cad-worker/helper/cad-render`. Must stay pure Go (D-19).

### `internal/mcpserver/tools.go` — analog line 68
```go
	Operation    string         `json:"operation,omitempty" jsonschema:"operation to perform on the file, defaults to convert when omitted -- see the operations key of the list_supported_formats/GET /v1/formats response ..."`
```
Name `render` and docs/CAD.md here; fix `client.go:112` `JobOutput.Role` to name `measurements`. New test asserts every operation in `convert.Default.OperationClasses()` appears in the tag (via reflect on the struct tag), test style as `internal/mcpserver/client_test.go`.

### `frontend/src/formats.ts` / `formats.test.ts` — analog `formats.test.ts`
```ts
import { describe, expect, it } from 'vitest';
import { normalizeFormat, sourceFormatOfFilename, targetFormatsFor, targetFormatsOf } from './formats';
import type { FormatsResponse } from './api';
```
Add a pure function grouping `FormatsResponse.operations` by operation; test feeds a synthetic response with a `render` cad pair and asserts the table equals it (no hard-coded list). Run `cd frontend && npm test && npm run build` explicitly (no gate runs vitest).

## Shared Patterns

### Evidence line protocol
**Source:** `75-dxf-2d-pipeline/evidence/75-05-bounds.txt`, `cad-liveproof.sh` `ev()` line 61
**Apply to:** all measurement and live-proof scripts. HDR lines with commit, tracked dirt, arch, image id, load, free disk; RULE lines first; CSV-ish `KEY,field=value,...`; DERIVED integers; a FAIL/NOT RUN is never counted as pass.

### Disk floor and hygiene
**Source:** `cad-dxf-bounds.sh` `MIN_FREE_GIB=6`, exit 3. Apply to every live script; never `down -v`, never prune volumes, 18090/18099/15443 ports.

### Error wrapping
`fmt.Errorf("%w: ...", sentinel)`; worker maps via `errors.Is` in `cadFailureCode` (descriptors.go 690-764), order matters.

### Go toolchain
All gates `GOTOOLCHAIN=go1.26.4`.

## No Analog Found

| File | Role | Data Flow | Reason |
|---|---|---|---|
| final-gate wrapper | CI script | batch | no existing all-classes wrapper; compose from ci-local tiers + `sandbox-liveproof.sh` + `cad-liveproof.sh`; tectonic proof has no script (use e2e tectonic PASS lines + tectonic-worker `sandbox proven` log) |
| `stl-stack` generator kind | generator | file-I/O | no stacked-STL kind; derive from `stl-tile` with pitch 0 |

## Metadata

**Analog search scope:** `scripts/`, `scripts/cmd/`, `internal/convert`, `internal/worker`, `internal/api`, `internal/errcode`, `internal/mcpserver`, `frontend/src`, `deploy/chart/octoconv`
**Files scanned:** ~25
**Pattern extraction date:** 2026-10-13
