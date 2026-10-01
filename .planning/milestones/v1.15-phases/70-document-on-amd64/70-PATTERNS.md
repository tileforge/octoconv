# Phase 70: Document on amd64 - Pattern Map

**Mapped:** 2026-09-29
**Files analyzed:** 13 (new/modified)
**Analogs found:** 13 / 13

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|--------------------|------|-----------|-----------------|----------------|
| `internal/convert/documentsandbox.go` (new) | service (confinement + preflight) | file-I/O | `internal/convert/probesandbox.go` (multi-engine class) + `internal/convert/htmlsandbox.go` (shared-`/tmp`/concurrency-guard shape) | role-match (composite: two engines like probe, shared-writable-path like html) |
| `internal/sandbox/sandbox_linux.go` (modified: add `MAKE_SOCK` to `accessReadWrite`) | utility (shared security primitive) | transform | itself (existing file, one `const` edit) | exact |
| `internal/convert/libreoffice.go` (modified: route `runCommand("soffice", …)` through `c.exec(...).run(...)`, add `TMPDIR` env) | service (converter) | request-response | `internal/convert/probesandbox.go`'s `exec`/`engineEnv` wiring applied to an *existing* converter, same shape as html's `chromium.go` inversion in phase 69 | exact (pattern already proven for `chromium.go`/`tesseract.go`) |
| `internal/convert/verapdf.go` (modified: route `runCommand("verapdf", …)` through `c.exec(...).run(...)`, add `JAVA_OPTS`) | service (converter) | request-response | same as above; `JAVA_OPTS` wiring is new but mirrors `engineEnv`'s `TMPDIR`-append shape | role-match |
| `internal/convert/sandboxcoverage_test.go` (modified: empty `sandboxPendingFiles`, add `libreoffice.go`/`verapdf.go` to `sandboxClassFileEngines`) | test | transform | itself (existing file, edit the two maps) | exact |
| `cmd/document-worker/main.go` (modified: add `ResolveHelper`, converter re-registration, concurrency guard, preflight-before-Postgres) | provider (worker entrypoint) | request-response | `cmd/probe-worker/main.go` (multi-engine class, `ResolveHelper` + preflight-before-DB shape) | exact |
| `docker-compose.yml` (modified: drop `platform: linux/amd64` from `document-worker` if D-01/D-02 unpin accepted; add `DOCUMENT_SANDBOX_DISABLED` forwarding; update sibling pin comments at ~773/892/1059/1208) | config | request-response | `docker-compose.yml`'s own `probe-worker` service block (`PROBE_SANDBOX_DISABLED: "${PROBE_SANDBOX_DISABLED:-}"`, line 1481) | exact |
| `Dockerfile.document-worker` (modified: pin only the `verapdf` build stage to `--platform=linux/amd64`, drop whole-image pin comment) | config | file-I/O | itself (existing file, `FROM` line + comment edit) | exact |
| `deploy/chart/octoconv/templates/deployment-document-worker.yaml` (modified: `extraEnv` reachability for `DOCUMENT_SANDBOX_DISABLED`) | config | request-response | sibling chart templates for other confined classes (e.g. `deployment-probe-worker.yaml` pattern) — not read this session; same `extraEnv` shape as `values.yaml` documents | role-match |
| `deploy/chart/octoconv/templates/configmap.yaml` (modified: `DOCUMENT_WORKER_CONCURRENCY` default, `DOCUMENT_SANDBOX_DISABLED` key) | config | request-response | existing `configmap.yaml` (own file) | exact |
| `.env.example` (modified: document `DOCUMENT_SANDBOX_DISABLED`, unset) | config | — | existing `.env.example`'s `PROBE_SANDBOX_DISABLED` entry | exact |
| `scripts/sandbox-liveproof.sh` (modified: add `document` case, including D-08's emulation-refusal step) | test (live gate script) | event-driven | existing `document` case if present else `probe`/`html` case shape | role-match |
| `scripts/conf05-crosscheck.sh` (modified: add `document` case) | test (cross-check script) | batch | existing `probe`/`html` case shape | role-match |
| `.planning/research/SANDBOX-PATHS.md` (modified: add "document" section) | config (docs, not source) | — | existing sections for `html`/`probe` | exact |

## Pattern Assignments

### `internal/convert/documentsandbox.go` (new file)

**Primary analog:** `internal/convert/probesandbox.go` (two-engine class, own grant sets, own `ConcurrencyGuard` function, own `Preflight`)
**Secondary analog:** `internal/convert/htmlsandbox.go` (shared-`/tmp` + `sharedTmpGuard` + `sweepSharedTmp` + `Confined<Class>` constructor shape — needed IF D-09's MAKE_SOCK route is taken, since `document` will then also grant `/tmp` read-write like `html`)

**Package doc comment pattern** (probesandbox.go:1-7):
```go
// Package convert (this file): probesandbox.go is probe's half of the
// engineExec contract -- the measured grant (D-10), the per-job ruleset
// builder, the concurrency guard, and the preflight that proves all four
// engines (...) work inside the job directory and are denied a file
// outside it -- mirroring ocrsandbox.go's shape, but for a class whose
// call sites span sanitizepdf.go and sanitizeverify.go rather than living
// beside its own single converter file.
```
For `document`, adapt: "documentsandbox.go is document's half of the engineExec contract — soffice's grant (measured, D-09/D-13) plus veraPDF's own separate grant (D-10/D-11), each engine's own environment (TMPDIR for soffice, JAVA_OPTS for verapdf), and the preflight that proves both engines run inside the job directory and are denied a file outside it."

**Grant-set vars pattern** (probesandbox.go:22-71, mirrored by htmlReadWrite/htmlReadOnly in htmlsandbox.go:18-52): two separate `var <engine>ReadWrite`/`<engine>ReadOnly` pairs are expected — D-10 explicitly wants "veraPDF gets its own measured ruleset, separate from soffice's." Do NOT merge them into one `documentReadWrite`/`documentReadOnly` pair; follow the two-set shape, e.g.:
```go
var sofficeReadWrite = []string{"/tmp"} // ONLY if MAKE_SOCK route taken (D-09); else empty, like probeReadWrite
var sofficeReadOnly = []string{ /* /usr/bin, /usr/lib, /lib, ... measured */ }
var verapdfReadWrite = []string{} // D-10/D-11 measured: no /tmp grant needed once JAVA_OPTS redirects java.io.tmpdir
var verapdfReadOnly = []string{
    "/opt/verapdf", "/usr/lib/jvm/...", "/etc/java-17-openjdk", // per research's live-proven RO set
}
```

**`exec` method pattern** (probesandbox.go:83-93, adapted per-engine):
```go
func (c SanitizeConverter) exec(outPath string) engineExec {
    jobDir := filepath.Dir(outPath)
    return c.Sandbox.forJobRW(jobDir, probeReadWrite, probeReadOnly, probeEngines).withEnv(engineEnv(jobDir))
}
```
`document` needs TWO such builders (one per engine's own grant set), e.g. `sofficeExec(workDir string) engineExec` and `verapdfExec(workDir string) engineExec`, each calling `c.Sandbox.forJobRW(jobDir, <engine>ReadWrite, <engine>ReadOnly, <engine>Engines)`. `JAVA_OPTS` is NOT one of `engineEnv`'s allowlisted vars (sandboxwrap.go:186, `keep := []string{"PATH", "HOME", "LANG", "LC_ALL"}`) — research's Open Question 3 recommends a document-specific env builder, e.g. `documentVerapdfEnv(jobDir string) []string { return append(engineEnv(jobDir), "JAVA_OPTS=-Djava.io.tmpdir="+jobDir+"/tmp -XX:-UsePerfData") }`.

**Concurrency guard pattern** (probesandbox.go:95-103, `ProbeConcurrencyGuard`):
```go
func ProbeConcurrencyGuard(c SanitizeConverter, concurrency int, concurrencyEnv string) error {
    return sharedTmpGuard(probeReadWrite, c.Sandbox, concurrency, concurrencyEnv)
}
```
Adapt to `DocumentConcurrencyGuard(c LibreOfficeConverter, concurrency int, concurrencyEnv string) error` calling `sharedTmpGuard(sofficeReadWrite, c.Sandbox, concurrency, concurrencyEnv)` — keyed on soffice's grant set specifically, since that's the one that may carry `/tmp` (D-09's MAKE_SOCK branch), never verapdf's.

**`WithSandbox` method pattern** (probesandbox.go:73-81, htmlsandbox.go:69-77): a plain method, not a `Confined<Class>` constructor, UNLESS the MAKE_SOCK route is taken — then mirror html's `ConfinedChromium` (htmlsandbox.go:79-99) exactly, since that function is precisely "build + enforce sharedTmpGuard + wire the sweep before returning anything usable":
```go
func ConfinedDocument(helper []string, concurrency int, concurrencyEnv string) (LibreOfficeConverter, error) {
    c := LibreOfficeConverter{}.WithSandbox(helper)
    if err := sharedTmpGuard(sofficeReadWrite, c.Sandbox, concurrency, concurrencyEnv); err != nil {
        return LibreOfficeConverter{}, err
    }
    if c.Sandbox.confines() && slices.Contains(sofficeReadWrite, sharedTmpDir) {
        c.sharedTmp = sharedTmpDir
    }
    return c, nil
}
```
This requires `LibreOfficeConverter` to gain a `sharedTmp string` field (mirroring `ChromiumConverter.sharedTmp`) and `Convert` to call `sweepSharedTmp` before each soffice invocation, exactly as D-09's "if not" branch and CONTEXT's `sharedTmpGuard`-style refusal describe.

**Preflight pattern** (probesandbox.go:169-344, `ProbePreflight`/`probePreflightSufficiency`/`probePreflightEffectiveness`): same three-part shape — `requireOutsideGrants`, sufficiency (convert a real fixture inside job dir, judge output by content via `requireOutput`/`requireContains`), effectiveness (point the SAME engine argv at a file outside the job dir, judge via `requireAbsent`/`requireLacks`). For `document`:
- soffice route: content-judged per Pitfall 2/Open Question 1 (research flags this as NOT YET MEASURED whether soffice fails loud or silent on denial — the plan must measure this as Wave 0/1 before writing the preflight body, per CONTEXT's Claude's Discretion item on marker/field placement).
- verapdf route: research's D-10/D-11 section already gives the EXACT live-proven shape to copy verbatim — see "Verified veraPDF sufficiency+effectiveness" excerpt below.

**Verified veraPDF preflight shape** (from RESEARCH.md D-10/D-11, live-proven this session — copy directly into `documentPreflightVeraPDF`):
```
sufficiency: verapdf -f 2b --format xml <job>/in.pdf  -> exit=0, isCompliant="true"
effectiveness: verapdf -f 2b --format xml <outside>/in.pdf -> exit=7, XML present,
    isCompliant ABSENT, <exceptionMessage>...Permission denied</exceptionMessage>,
    <batchSummary failedToParse="1" ...><validationReports failedJobs="1">1</validationReports>
```
This maps onto `requireOutput`/`requireAbsent` via veraPDF's OWN existing `parseVeraPDFReport` (verapdf.go:90-129) — do not re-derive the judgment; call `parseVeraPDFReport` on both the inside and outside runs' stdout, exactly as `ValidatePDFA`/`validateReport` already do in production code.

---

### `internal/sandbox/sandbox_linux.go` (modified)

**Analog:** itself. **Change:** add `unix.LANDLOCK_ACCESS_FS_MAKE_SOCK` to the `accessReadWrite` const (sandbox_linux.go:17-20), ONLY if the MAKE_SOCK route (D-09 recommendation) is taken after the Wave 0 spike on `OSL_PIPE`/`officeipc` alternatives comes back negative.

**Current shape** (sandbox_linux.go:13-48, quoted verbatim in RESEARCH.md D-09):
```go
accessReadWrite = unix.LANDLOCK_ACCESS_FS_READ_FILE | unix.LANDLOCK_ACCESS_FS_READ_DIR |
    unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_TRUNCATE |
    unix.LANDLOCK_ACCESS_FS_MAKE_REG | unix.LANDLOCK_ACCESS_FS_MAKE_DIR |
    unix.LANDLOCK_ACCESS_FS_REMOVE_FILE | unix.LANDLOCK_ACCESS_FS_REMOVE_DIR
```
`handledAccess` (line 43-47) already includes `LANDLOCK_ACCESS_FS_MAKE_SOCK` — it is "handled" (restricted) but "granted nowhere" today (line 41-42 comment). The one-line fix is adding the same constant to `accessReadWrite`'s OR-chain. This is shared code — every already-confined class's `ReadWrite` grant gains the right inertly (research's own risk assessment: "harmless for classes that don't [bind a socket]"). Treat as its own reviewed commit per RESEARCH.md's explicit recommendation, separate from `document`'s own grant-set files.

---

### `internal/convert/libreoffice.go` (modified: route through `engineExec`)

**Analog:** the phase-69 "class-closure inversion" pattern already applied to `chromium.go`/other engine files — carried forward per CONTEXT D-12 ("class-closure inversions: at minimum a literal low-level exec restored in `libreoffice.go` and in `verapdf.go`, each shown to fail `TestConfinedClassesExecOnlyThroughTheSandbox`, then reverted"). The file to inspect for the inversion TEST shape is `sandboxcoverage_test.go`'s `TestConfinedClassesExecOnlyThroughTheSandbox` (referenced at line ~301, "calls engineExec.%s but is not registered in sandboxClassFileEngines").

**Current low-level call** (libreoffice.go:109):
```go
if _, err := runCommand(ctx, "soffice", args...); err != nil {
    return fmt.Errorf("libreoffice: %w", err)
}
```
**Target shape** (mirrors probesandbox.go's `x := c.exec(...)` then `x.run(ctx, "gs", ...)`, sandboxwrap.go:234-246's `engineExec.run`):
```go
x := c.sofficeExec(workDir) // new documentsandbox.go builder, TMPDIR set via engineEnv(jobDir)
if _, err := x.run(ctx, "soffice", args...); err != nil {
    return fmt.Errorf("libreoffice: %w", err)
}
```
`x.run` already turns a helper-exit-`sandbox.SetupExit` failure into `ErrEngineSandboxUnavailable` (sandboxwrap.go:248-261) — no new error-classification code needed in `libreoffice.go` itself; `isDocumentTerminal` just needs the guard added (see worker.go section below).

---

### `internal/convert/verapdf.go` (modified: route through `engineExec`)

**Analog:** same inversion pattern, applied to `ValidatePDFA` (verapdf.go:166-180).

**Current low-level call** (verapdf.go:170):
```go
stdout, runErr := runCommand(vctx, "verapdf", "-f", "2b", "--format", "xml", path)
```
**Target shape:**
```go
x := documentVerapdfExec(filepath.Dir(path)) // new documentsandbox.go builder, JAVA_OPTS set
stdout, runErr := x.run(vctx, "verapdf", "-f", "2b", "--format", "xml", path)
```
`ValidatePDFA` is a package-level function (not a method on a converter with a `Sandbox` field) — it will need either a converter argument threaded through from `libreoffice.go`'s call site (`validateDocumentOutput` → `ValidatePDFA`), or its own package-level `*engineSandbox` set via a `SetVeraPDFSandbox`-style setter mirroring `SetVeraPDFTimeout`'s existing single-write-before-concurrent-read pattern (verapdf.go:20-27). The latter is smaller and matches D-12's "single choke point set once at startup" convention already used for the timeout.

---

### `internal/convert/sandboxcoverage_test.go` (modified)

**Analog:** itself.

**Current pending map** (sandboxcoverage_test.go:63-69):
```go
// sandboxPendingFiles belong to a class a LATER phase confines. A
var sandboxPendingFiles = map[string]string{
    "libreoffice.go": "phase 70",
    "verapdf.go":     "phase 70",
}
```
**Target:** remove both entries — `sandboxPendingFiles` becomes `map[string]string{}` once both files route through `engineExec`. Add `"libreoffice.go"` and `"verapdf.go"` as new `case` arms in `sandboxClassFileEngines` (sandboxcoverage_test.go:79-117), listing the literal engine names each file execs (`"soffice"` and `"verapdf"` respectively) — mirror the existing `case "sanitizeconverter.go", ..., "probesandbox.go":` arm shape at line 93/117.

---

### `cmd/document-worker/main.go` (modified)

**Analog:** `cmd/probe-worker/main.go` (multi-engine class, same "ResolveHelper → build converter → concurrency guard → Replace → preflight before Postgres" ordering CONF-04/D-11 both require).

**RunExecModeIfRequested guard** (probe-worker/main.go:49-56) — MUST be added as the very first lines of `main()`:
```go
if sandbox.RunExecModeIfRequested(os.Args) {
    return
}
```

**ResolveHelper + escape-hatch naming** (probe-worker/main.go:38-42, 61-69):
```go
const probeSandboxDisabledEnv = "PROBE_SANDBOX_DISABLED"
...
sandboxHelper, err := sandbox.ResolveHelper(probeSandboxDisabledEnv)
if err != nil {
    log.Fatalf("probe sandbox: %v", err)
}
```
Adapt to `const documentSandboxDisabledEnv = "DOCUMENT_SANDBOX_DISABLED"` per CONTEXT D-06/D-07 — no GOARCH/emulation-detection code, the refusal is the shared `sandbox.ResolveHelper` message verbatim.

**Concurrency guard, wired BEFORE `convert.Default.Replace`** (probe-worker/main.go:101-113):
```go
concurrency := envInt(probeConcurrencyEnv, 1)
if err := convert.ProbeConcurrencyGuard(conv, concurrency, probeConcurrencyEnv); err != nil {
    log.Fatalf("probe sandbox: %v", err)
}
convert.Default.Replace(conv)
```
Note document-worker's CURRENT default concurrency is `2` (document-worker/main.go:99, `envInt("DOCUMENT_WORKER_CONCURRENCY", 2)`) — if D-09's MAKE_SOCK/shared-`/tmp` branch is taken, this default must drop to `1` in THREE places at once (CONTEXT explicit): `cmd/document-worker/main.go`, `docker-compose.yml`, and the chart configmap — mirroring html's own D-01..D-04 concurrency-1 precedent (`ConcurrencyEnv` default already `1` in probe/pandoc, per probe-worker/main.go:47/108).

**Preflight-before-Postgres ordering** (probe-worker/main.go:126-140) — `convert.ProbePreflight` runs and `log.Fatalf`s BEFORE `db.Connect` is ever called:
```go
preflightCtx, cancelPreflight := context.WithTimeout(context.Background(), 60*time.Second)
if err := convert.ProbePreflight(preflightCtx, conv); err != nil {
    cancelPreflight()
    log.Fatalf("probe sandbox preflight: %v", err)
}
cancelPreflight()
...
pool, err := db.Connect(ctx)
```
`document-worker/main.go`'s CURRENT ordering calls `db.Connect(ctx)` as the very first statement (document-worker/main.go:33) — this must be reordered so `sandbox.ResolveHelper` + preflight happen first (CONF-04's "before Postgres/queue" requirement, explicitly named in RESEARCH.md's Integration Points section).

**Log lines pattern** (probe-worker/main.go:114-134): mirror the exact emoji/wording convention — `"📄 ..."` for config, `"⚠️ %s=true — ..."` for the disabled case, `"🔒 ... sandbox: landlock via %s"` and `"🔒 ... sandbox proven: ..."` for the confined+proven case.

---

### `docker-compose.yml` (modified)

**Analog (escape-hatch env forwarding):** the `probe-worker` service's existing line (docker-compose.yml:1481):
```yaml
PROBE_SANDBOX_DISABLED: "${PROBE_SANDBOX_DISABLED:-}"
```
Add the identical shape for `document-worker`: `DOCUMENT_SANDBOX_DISABLED: "${DOCUMENT_SANDBOX_DISABLED:-}"`, per D-06 ("It is **not** defaulted on in compose").

**Analog (dropping the platform pin, IF D-01/D-02 accepted):** current `document-worker` service block (docker-compose.yml:567-580):
```yaml
document-worker:
  # veraPDF (PDFA-02, D-03) is bundled into this image from the pinned
  # verapdf/cli:v1.30.2 Docker Hub image, which is published amd64-only
  # (verified live against the registry, 23-01) -- pin the whole service
  # to linux/amd64 so the build (...) doesn't accidentally cross-copy
  # incompatible architecture binaries into the runtime stage.
  platform: linux/amd64
  build:
    context: .
    dockerfile: Dockerfile.document-worker
```
If the unpin ships: delete `platform: linux/amd64` and rewrite the comment per D-04 ("the pin was the verapdf stage, the image is native, measured and gated on this host... Keep the old wording's reason visible"). Every sibling comment referencing "document-worker's veraPDF-forced amd64 pin" (lines ~773-774, ~892-894, ~1059-1060, ~1208-1210) must be updated in the SAME commit — these are the "no platform: pin needed... unlike document-worker's veraPDF-forced amd64 pin" comments on av-worker/audio-worker/ocr-worker/tectonic-worker/probe-worker services; find with `grep -n "veraPDF-forced amd64 pin" docker-compose.yml`.

---

### `Dockerfile.document-worker` (modified)

**Analog:** itself. Current verapdf stage (Dockerfile.document-worker:9):
```dockerfile
FROM verapdf/cli:v1.30.2 AS verapdf
```
**Target (per D-01, measured, confirmed this session):**
```dockerfile
FROM --platform=linux/amd64 verapdf/cli:v1.30.2 AS verapdf
```
The lengthy comment block above it (Dockerfile.document-worker:9-17, "the whole document-worker image must therefore be built with `--platform linux/amd64`...") must be rewritten to describe the NEW, narrower reality: only this one build stage is amd64-pinned; the runtime stage and shipped image are native.

---

## Shared Patterns

### Sandbox confinement construction (`engineSandbox`/`engineExec`)
**Source:** `internal/convert/sandboxwrap.go` (whole file — `newEngineSandbox`, `forJobRW`, `engineEnv`, `sharedTmpGuard`, `engineExec.run`, `sandboxFault`)
**Apply to:** `documentsandbox.go`, `libreoffice.go`, `verapdf.go`

### Preflight toolkit
**Source:** `internal/convert/sandboxpreflight.go` (whole file — `preflightWorkspace`, `requireOutsideGrants`, `requireOutput`/`requireDenied`/`requireAbsent`/`requireContains`/`requireLacks`, `preflightMarker`)
**Apply to:** `documentsandbox.go`'s new `DocumentPreflight` function

### Fail-closed startup refusal (CONF-02)
**Source:** `internal/sandbox` package's `ResolveHelper`/`RunExecModeIfRequested`, applied verbatim in `cmd/probe-worker/main.go:49-69`
**Apply to:** `cmd/document-worker/main.go`

### Engine-scoped terminal classification (CONF-02's retryable-vs-terminal split)
**Source:** `internal/worker/worker.go:237-239` (`isDocumentTerminal`, currently MISSING the guard) vs. `internal/worker/worker.go:253-261` (`isHTMLTerminal`, which HAS it):
```go
func isHTMLTerminal(err error) bool {
    // Phase 69/CONF-06: a node that cannot confine the engine or sweep its
    // shared /tmp is retried elsewhere, never blamed on the file -- checked
    // before delegating to timeoutIsTerminal...
    if errors.Is(err, convert.ErrEngineSandboxUnavailable) {
        return false
    }
    return timeoutIsTerminal(err)
}
```
**Apply to:** `isDocumentTerminal` must gain the identical guard, checked FIRST, before its existing `timeoutIsTerminal(err)` delegation:
```go
func isDocumentTerminal(err error) bool {
    if errors.Is(err, convert.ErrEngineSandboxUnavailable) {
        return false
    }
    return timeoutIsTerminal(err)
}
```
This is CONTEXT D-12's explicitly named gap ("`ErrEngineSandboxUnavailable` checked first in `isDocumentTerminal`, `internal/worker/worker.go:237`") and RESEARCH.md's CONF-02 finding ("`isDocumentTerminal` is currently missing the ... guard every other confined class already has").

### Escape-hatch env var forwarding (compose + chart + .env.example)
**Source:** `PROBE_SANDBOX_DISABLED` end-to-end: `docker-compose.yml:1481`, `.env.example`'s equivalent entry, chart `extraEnv`
**Apply to:** `DOCUMENT_SANDBOX_DISABLED`, forwarded the same three places

### Hardened process exec (unchanged, D-13)
**Source:** `internal/convert/exec.go`'s `runCommand` (Setpgid + group SIGKILL) — stays byte-for-byte unchanged; `engineExec.run` (sandboxwrap.go:234-246) calls into `runCommandAs`/`runCommandCore`, which themselves call the same underlying exec machinery `exec.go` provides. No changes needed to `exec.go` itself.
**Apply to:** nothing — this is a "do not touch" pattern, listed for completeness since D-13 explicitly calls it out.

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `deploy/chart/octoconv/templates/deployment-document-worker.yaml` extraEnv wiring | config | request-response | Not read this session (out of the required-reading + core analog set); the planner should read the file directly alongside a sibling confined-class deployment template (e.g. `deployment-probe-worker.yaml`, `deployment-html-worker.yaml`) for the exact `extraEnv` block shape — same `values.yaml`-driven mechanism CONTEXT cites as "CONF-03 pattern," not independently verified here |
| `scripts/sandbox-liveproof.sh` document case (including D-08's emulation-refusal step) | test (live gate) | event-driven | Not read this session; RESEARCH.md and CONTEXT both point at the file and describe the required shape ("the html/probe live-gate pattern gets one extra step") but the file's current per-class case structure was not opened — read it directly before writing the `document` case, mirroring whichever of `html`/`probe`'s cases is structurally closest to a two-engine class |
| `scripts/conf05-crosscheck.sh` document case | test (cross-check) | batch | Same as above — not read this session; described as "needs a `document` case added, following the `html`/`probe` pattern verbatim" in RESEARCH.md CONF-05 row |

## Metadata

**Analog search scope:** `internal/convert/`, `internal/sandbox/`, `internal/worker/`, `cmd/*-worker/`, `Dockerfile.document-worker`, `docker-compose.yml`, `.planning/phases/69-html-probe-the-tmp-decision/`
**Files scanned:** ~18 (all git-tracked; verified via `git ls-files`)
**Pattern extraction date:** 2026-09-29
