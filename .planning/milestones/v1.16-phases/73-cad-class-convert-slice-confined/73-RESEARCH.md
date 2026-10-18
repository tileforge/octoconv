# Phase 73: CAD Class & Convert Slice, Confined - Research

**Researched:** 2026-10-05
**Domain:** adding the tenth engine class (`cad`: STEP/IGES -> binary STL via OCP) to a Go/asynq/Postgres/S3 service, confined by Landlock, with a guard pre-pass and bomb handling
**Confidence:** MEDIUM-HIGH. The class-touch checklist, limits arithmetic, units evidence and Landlock candidates are read from code and 71 evidence this session (HIGH). How the real OCP reader behaves on a self-referencing STEP, non-numeric coordinates and assembly-expansion bombs is NOT measured anywhere in the repo (LOW); this research lists it as Wave-0 spikes rather than predicting it.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**Worker shape: follow the newest class exactly**
- **D-01:** `cmd/cad-worker` mirrors `cmd/tectonic-worker` / `cmd/document-worker` (the phase 64/70 pattern):
  - sandbox helper re-exec (`internal/sandbox`);
  - `preflightSandbox` and then `preflightEngine` before touching Postgres;
  - the same Landlock escape-hatch environment variable the other workers use, named in the refusal message;
  - asynq server on queue `cad`;
  - concurrency defaults to 1 (`CAD_WORKER_CONCURRENCY`), because the pod limit was derived for one job at a time (71 D-07).

  No new worker idioms.
- **D-02:** Engine invocation goes through the existing hardened `runCommand` (Setpgid plus SIGKILL of the process group). `internal/convert/exec.go` stays byte-for-byte unchanged, which is an invariant. A killed worker mid-job must leave no live engine process; this is proven by test, as in earlier classes.

**Engine and image**
- **D-03:** The production engine is a Python helper hardened from the phase 71 measurement helper (`scripts/cad-measure/cad_mesh.py`). It is a separate production copy, not a shared file, so the 71 measurement image tag stays stable.
  - The planner picks the location.
  - The helper is minimal: a `guard` pre-pass and a `mesh` (-> binary STL) mode only.
  - It has no measurement flags and no measurement-only output.
  - Its output is a binary STL plus a small JSON status on stdout that the Go worker parses. The verdict is judged by content, never by exit code alone.
- **D-04:** `Dockerfile.cad-worker` is built from the APPROVED 71 lock files only:
  - `requirements-common.txt` (numpy) and `requirements-ocp-7.9.3.1.1.txt` (the OCP pin chosen by the user);
  - the same digest-pinned `debian:bookworm-slim`;
  - `USER nobody`;
  - `--require-hashes`;
  - no new pip or apt packages. Any extra package is a supply-chain checkpoint.

  VTK is not needed for mesh. It arrives with render in phase 74, pruned by the 71 keep-list. The image is built for both arm64 and amd64; the 71 keep-list fix proved the amd64 path.
- **D-05:** Tessellation constants and threads come from 71-DECISION:
  - quality `standard`;
  - `occt_parallel=off`.

  `LP_NUM_THREADS` is irrelevant to mesh and is set anyway for parity.

**Units**
- **D-06:** The output STL is always in mm.
  - 71 proved OCP scales `units_inch.step` (6 in^3 -> 98322.384 mm^3).
  - 71 did NOT prove scaling for `nist_ctc_05`, which uses lowercase `'inch'` and for which OCP `FileUnits` returns an empty name.
  - The helper reads the declared unit from the file text, as in 71. If OCP did not scale it, the helper scales explicitly.
  - A test on both files proves the mm output by a bbox comparison against a known dimension.

**Limits (from 71-DECISION after the Codex corrections on PR #32)**
- **D-07:** The pod memory limit is 4Gi.
- **D-08:** Input byte ceilings are enforced at job creation with 413 `payload_too_large`, naming the format and ceiling in `message`:
  - STEP 63 MiB;
  - IGES 155 MiB.

  The worker re-checks them, which is defence in depth. Read the numbers from 71-DECISION's DECISION lines, not from memory.
- **D-09:** The guard pre-pass, run before mesh in the worker under the same sandbox, enforces two caps:
  - STEP entity cap 754762;
  - NAUO instance cap 1518.

  Both come from 71-DECISION. Exceeding either is a terminal failure with its own code. The "STL with a lying triangle count" bomb is already rejected by the phase 72 sniffer; a test in this phase proves it at the job level. Non-numeric coordinates and a self-referencing STEP must end as terminal failures, within measured memory and time.
- **D-10:** The mesh stage timeout is derived from the 71 evidence. It is the worst measured mesh wall at the ceiling size x a visible margin, and the derivation is written down. The research phase computes it. There is also a cgroup memory bound per engine run, following the existing `cgroup.go` pattern.

**Error classification (CADW-03)**
- **D-11:** Error classes:
  - **Sandbox unavailable:** transient (retry).
  - **Unreadable input (OCP read failure):** terminal, with its own code.
  - **Empty output (0 triangles or no shapes):** terminal, with its own code.
  - **Over a ceiling or cap:** terminal, with its own code.
  - **Mesh stage timeout:** terminal, with its own code, and no retry loop.

  Existing async codes are reused where the client action matches, for example a generic engine timeout if one exists. A new `cad_*` code is added only where the client action differs. All codes go through `internal/worker/descriptors.go` and the ERROR-CODES async table with the existing sync tests. The research phase lists exactly which codes exist today. **Reversibility:** one-way. The async code strings go on the wire.

**Confinement (CADW-02)**
- **D-12:** The mesh and guard stages get their own Landlock ruleset.
  - It starts from the 71 measured candidate: helper dir, `/opt/venv`, `/usr/bin/python3.11`, `/usr/lib/<arch>-linux-gnu`, `/usr/lib/python3.11`.
  - It is parametrised by architecture; this is the 71 verifier's W17 warning.
  - It is re-measured on the PRODUCTION cad-worker image by the established SANDBOX-PATHS method, with a removal table and a negative control.
  - A new `## cad` section goes into `.planning/research/SANDBOX-PATHS.md`. The set is held by a Go test and by `scripts/conf05-crosscheck.sh`.
  - An engine aimed at a file outside the job directory is refused, and a test proves it.
- **D-13:** The worker refuses to start without Landlock and names the escape-hatch variable. `preflightEngine` runs a real minimal STEP through guard and mesh under the sandbox, and judges the output STL's content: its triangle count and bbox.

**Atomic exposure (CADW-01)**
- **D-14:** One atomic change covers all of the following:
  - `AllEngines()` + `cad`;
  - queue/route;
  - reconciler;
  - timeout/estimate/size maps;
  - the README classes table;
  - `/v1/formats`, which now shows the cad convert pairs;
  - `/v1/inspect`, whose estimate is marked `unmeasured`;
  - compose `cad-worker`;
  - the chart `deployment-cad-worker.yaml` and `scaledobject-cad.yaml`, using the existing template pattern.

  The phase 72 guard tests that assert "no CAD pair" are inverted or updated in that same change, deliberately, with the reason in the commit. The other nine classes and the metric labels `{engine,status}` / `{queue,state}` are unchanged and pinned by the existing goldens. **Reversibility:** costly. It touches every class registry and the deploy manifests.
- **D-15:** The chart ScaledObject follows the existing pattern with `minReplicaCount: 0`. Phase 76 tunes it, using the 71 cold-pull cost.

### Claude's Discretion
- File and package names, the helper location, the JSON status shape, and new error code names within D-11's rule.

### Deferred Ideas (OUT OF SCOPE)
- Render, measurements output, DXF, minReplicaCount tuning, the render time guard, the live KEDA gate: phases 74-76.
- amd64 runtime measurement (only the build is proven).
- Lawyer review of copyleft libraries: an open, non-blocking item from 71.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| CADW-01 | `cmd/cad-worker` со своей очередью, Dockerfile, compose, Helm-чартом и KEDA ScaledObject; `cad` появляется в API только целиком — одним атомарным изменением `AllEngines()` | Section 1 (full touch checklist with anchors), Section 9 (tests that flip), Section 8 (Dockerfile) |
| CADW-02 | Каждый этап движка работает под своим замеренным Landlock-ruleset; наборы путей записаны в `SANDBOX-PATHS.md` и удерживаются тестом; воркер без Landlock не стартует; preflight на реальном движке судит по выходу | Section 2 (worker skeleton, preflight), Section 7 (Landlock sets, crosscheck, re-measurement) |
| CADW-03 | Ошибки классифицируются transient/terminal по этапам; убийство посреди джобы не оставляет живых процессов и не держит джобу до полного таймаута | Section 3 (codes), Section 2.4 (kill/recovery proof), Section 4.4 (OOM backstop) |
| CADIN-04 | Вход-бомба (самоссылающийся STEP, гигантское число треугольников, MINSERT, нечисловые координаты) завершает джобу терминальной ошибкой в замеренных границах памяти и времени, а не OOM-киллом воркера (STEP/IGES/STL part in this phase; DXF in 75) | Section 6 (guard design, bomb fixtures), Section 4 (limits, timeout), Pitfalls 3-5 |
| CCONV-01 | `step`/`iges` -> `stl`: один бинарный STL в миллиметрах | Section 5 (units evidence and bbox test), Section 8 (no-VTK feasibility), Code Examples |
</phase_requirements>

## Project Constraints (from CLAUDE.md)

- No code comments by default; a comment only for an external constraint the code cannot express (user rule, stricter than the project CLAUDE.md). Design rationale goes to the PR description. Code examples below follow this.
- Go 1.26 (`go 1.26.4` in `go.mod`); run gates with `GOTOOLCHAIN=go1.26.4`, because the local `go` is 1.27.1 (a green on the local toolchain is not a green on the pinned one).
- Library code under `internal/*` never logs; only `cmd/*/main.go` logs. Errors wrap with `fmt.Errorf("<action>: %w", err)`. No `panic` for control flow. HTTP layer never echoes internal error text.
- Tests use stdlib `testing` only (no assertion library).
- Engine invocation goes through hardened process exec; `internal/convert/exec.go` is an invariant (D-02).
- Do not edit the repo outside a GSD workflow; `.planning/` is force-tracked (`git add -f`).
- Local gates, not GitHub Actions (`scripts/ci-local.sh`, tiers `helm gate race docker-build e2e`); ci.yml auto-triggers are off.
- "Judge by content, not by return code" and "a scan of nothing is never clean" are project rules (v1.15, 71 D-11); guards must count what the helper actually parsed, and a guard that could not count is a failure.

## Summary

The `cad` class is the tenth class and the first whose engine is a Python helper importing a C++ geometry kernel. Structurally it is the tectonic/pandoc/document pattern: `engineSandbox` + `engineExec` + a `<Class>Preflight`, one `EngineDescriptor`, one queue, one `cmd/*-worker`. Everything the class touches is enumerated in Section 1 with file:line anchors, and most of it is held by existing derived-from-`AllEngines()` tests that will fail by name until each registry has a `cad` entry (that is the atomicity mechanism, and it is the intended safety net).

Several CONTEXT premises do not survive contact with the code or the 71 evidence and the planner must not build on them as written: (1) there is no per-run cgroup memory bound anywhere in the repo, and `internal/convert/cgroup.go` is only a `cpu.max` reader; (2) 71 already contains enough evidence to resolve the `nist_ctc_05` unit question: OCP scaled it (Section 5), so the "helper scales explicitly" fallback is a double-scaling hazard; (3) the NAUO cap bounds the NAUO edge count, not assembly expansion; (4) the chart ConfigMap's `MAX_UPLOAD_BYTES` is 100 MiB, below the 155 MiB IGES ceiling; (5) a landlock denial of the mesh stage is indistinguishable at helper level from an unreadable file (rc=2, no text), so sufficiency must be proven by preflight before the worker consumes a queue; (6) the mesh timeout cannot honestly be "worst measured at or below the ceiling x margin" (that is 172 s and covers a 63 MiB file by 3%); the derivation that brackets the ceiling from above gives 316 s (Section 4.3).

**Primary recommendation:** implement `cad` as a copy of the tectonic/document shape with two separately-measured rulesets (`cadguard`, `cadmesh`), a pure-stdlib guard that counts conservatively (statement terminators, not line-anchored regex) and is unit-testable without OCP, per-format byte ceilings (63/155 MiB) via a new per-format map consulted by create and inspect, five error outcomes mapped to one reused code (`input_too_large`) and four new `cad_*` codes, a 316 s mesh-stage timeout (derivation shown) inside a ~406 s attempt timeout, and Wave-0 spikes against the real OCP reader for every bomb shape before any guard rule is finalised.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Content sniff, per-format byte ceiling (413), pair routing, enqueue | API / Backend (`internal/api`) | Database / Storage (job row first) | Fail-closed gates run before `storage.Upload`; sniffer is phase 72's `convert.SniffCADWindows` |
| Queue routing and retry/unique-lock derivation | API / Backend (`internal/queue`) | Reconciler (recovery re-enqueue) | `queue.NewClient` derives UniqueTTL in every enqueuing process (IN-02) |
| Guard pre-pass (entity/NAUO counts, cycle check) | Worker process, sandboxed Python stage `cadguard` | Go worker (verdict parse, byte re-check) | Must run before OCP loads hostile bytes; own ruleset without `/opt/venv` |
| Mesh (OCP read, units to mm, tessellate, binary STL) | Worker process, sandboxed Python stage `cadmesh` | Go worker (content judgment of the STL) | Only stage that loads OCP; confined to job dir |
| Failure classification (transient/terminal, code) | Go worker (`internal/worker/descriptors.go`) | `internal/convert` sentinels | Descriptor pattern; code strings go on the wire |
| Process-group kill, deadline, OOM classification | Go worker (`runCommandCore` via `engineExec`) | Container cgroup (4Gi) | `exec.go` unchanged; cgroup is the only memory bound |
| Confinement | Kernel Landlock via `internal/sandbox` helper re-exec | Preflight in worker `main` | Sets held by Go var + SANDBOX-PATHS.md block + crosscheck |
| Stale-job recovery | Reconciler (in webhook-worker) | asynq lease recoverer | Needs a `cad` route or jobs strand on `unroutable_engine` |
| Scaling | Chart ScaledObject (KEDA, Prometheus queue depth) | compose (no scaling) | `minReplicaCount: 0` per D-15 |

## Standard Stack

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `cadquery-ocp-novtk` | 7.9.3.1.1 | OCCT bindings, STEP/IGES readers, mesher, STL writer | User-chosen pin (71 `DECISION,key=ocp_pin,value=7.9.3.1.1`); lock hashes for aarch64 and x86_64 in `scripts/cad-measure/requirements-ocp-7.9.3.1.1.txt` |
| `cadquery-ocp-proxy` | 7.9.3.1.1 | namespace proxy for the same pin | Present in the same approved lock file |
| `numpy` | 2.4.6 | transitive in `requirements-common.txt` | Approved lock; not imported by the mesh path |
| Python | 3.11.2 (`debian:bookworm-slim` apt `python3`) | helper runtime | 71 measured on it; helper must stay 3.11-compatible (host dev Python is 3.14.8) |
| `debian:bookworm-slim@sha256:3783cc01769c7b2b1b83a5c5ad96c815348e28ed7da68e2e3687004faa906251` | pinned | base image | Index digest from `Dockerfile.cad-measure`; covers arm64 and amd64 |
| Go modules already in `go.mod` | asynq v0.26.0, chi v5.3.0, pgx v5.10.0, minio-go v7.2.1 | worker, API | No new Go dependency is needed |

[VERIFIED: `scripts/cad-measure/requirements-ocp-7.9.3.1.1.txt` lines 1-5: `cadquery-ocp-novtk==7.9.3.1.1 \` with two sha256 hashes, `cadquery-ocp-proxy==7.9.3.1.1 \` with one] [VERIFIED: `Dockerfile.cad-measure:3`: `FROM debian:bookworm-slim@sha256:3783cc01769c7b2b1b83a5c5ad96c815348e28ed7da68e2e3687004faa906251 AS venv`] [VERIFIED: `requirements-common.txt` package lines `ezdxf==1.4.4`, `fonttools==4.66.1`, `numpy==2.4.6`, `pillow==12.3.0`, `pyparsing==3.3.3`, `typing-extensions==4.16.0`]

### Supporting

| Item | Version | Purpose | When to Use |
|------|---------|---------|-------------|
| `scripts/cad-measure/cad_mesh.py` | 71 final | source of the production helper (units, finite-bbox fallback, quality table) | Copy and strip; do not share the file |
| `scripts/cmd/sandbox-probe` | repo | real-ruleset probe for Landlock measurement | Re-measurement on the production image |
| `scripts/cad-sandbox-measure.sh` | repo | trace -> candidate -> sufficiency -> effectiveness -> removal table | Parametrise for the production image tag |
| `scripts/cad-corpus-gen.py` (`step-nauo`, `iges-from-step` kinds) | repo | deterministic NAUO-scaled and IGES fixtures | Reuse for bomb fixtures that need real geometry |
| `libgl1`, `libx11-6` (apt) | bookworm | `import OCP` hard needs: `libTKService` -> `libX11.so.6`; the cad_mesh trace opens `libGL.so.1` | Subset of the 71 apt list; see Section 8 |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `requirements-common.txt` verbatim | A mesh-only subset (numpy + typing-extensions) with identical hash lines | Drops ezdxf, pillow, fonttools, pyparsing from the mesh image (smaller attack surface) but is a new file; D-04 literally names the two files, so default to verbatim and log the subset as an option |
| Guard in Python stage (D-09) | Guard in Go inside the worker | Go RE2 is linear and `go test`-able everywhere, but reads hostile bytes in the unconfined worker process and contradicts D-09's "under the same sandbox"; keep Python, make it stdlib-only and host-testable |
| RLIMIT_AS/RLIMIT_DATA for a per-run memory bound | container cgroup (4Gi) + caps + OOM classification | RLIMIT_AS counts virtual size (OCP maps far more than it touches); no measurement exists; do not add without a measurement |

**Installation (production image, mirrors the 71 stage, no new pip package):**
```bash
/opt/venv/bin/pip install --no-cache-dir --only-binary=:all: --require-hashes -r /req/requirements-common.txt
/opt/venv/bin/pip install --no-cache-dir --only-binary=:all: --no-deps --require-hashes -r /req/requirements-ocp-7.9.3.1.1.txt
```

**Version verification:** versions above are read from the committed lock files (sha256 of `requirements-common.txt` = `9d391daad60a17595077c7aa68f16890c95511a617dbb209fefaf0b5b69a78da`, of `requirements-ocp-7.9.3.1.1.txt` = `72b833ac43ac4cdab8fbb694ed9b2954472f2c1ab09b4ebc32684bf2387147df`, computed this session). A plan task should assert the production Dockerfile consumes files with exactly these hashes; that is the "approved lock files only" check.

## Package Legitimacy Audit

No new package is introduced: the Python set is the user-approved 71 lock (71-DECISION: "Lock-файлы Python: допущены, установка только `pip --require-hashes`"; wheel hashes cross-checked, `evidence/71-01-lock.txt` `CROSSCHECK ... match=yes`). The legitimacy seam was still run:

| Package | Registry | Age | Downloads | Source Repo | Verdict | Disposition |
|---------|----------|-----|-----------|-------------|---------|-------------|
| cadquery-ocp-novtk | PyPI | latest release 2026-09-05 | seam: unknown | seam: none | SUS (`too-new`, `unknown-downloads`, `no-repository`) | Already user-approved in 71 by hash; not new |
| cadquery-ocp-proxy | PyPI | latest release 2026-09-05 | seam: unknown | seam: none | SUS (same reasons) | Already user-approved in 71 by hash; not new |
| numpy | PyPI | latest release 2026-09-06 | seam: unknown | seam: none | SUS (same reasons) | Not new; seam signal is non-discriminating (numpy itself is SUS) |

**Packages removed due to [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** all three above, for seam-signal reasons (PyPI lacks download and repo metadata in this seam; the verdict is not evidence about numpy). Because these are pinned by sha256 in lock files the user approved in phase 71, the planner should NOT add a `checkpoint:human-verify` per package; instead add one task asserting the two lock files are byte-identical to the approved ones (hashes above). `ca-certificates` (apt) is the only package this research suggests beyond the 71 apt list; that one IS a D-04 supply-chain checkpoint (Open Question 4).

## Architecture Patterns

### System Architecture Diagram

```
client -> POST /v1/jobs (step|stp|iges|igs -> stl)
   |
   v
API handleCreateJob
   sniff (convert.SniffCADWindows, 4 KiB head+tail)  --reject--> 422 truncated_cad_file | ...
   pair lookup  EngineForOp("convert", step, stl) = "cad"
   per-format ceiling (NEW map: step 63 MiB, iges 155 MiB) --over--> 413 payload_too_large (message names format + ceiling)
   PreAdmit quota -> Upload to S3 -> repo.Create (job row, queued) -> enqueueForEngine -> asynq queue "cad"
   |
   v                                                     reconciler (webhook-worker sweep): stale job
asynq server (cad-worker, concurrency 1)  <------------  with engine=cad -> EnqueueCADConvert
   Runner.Handle: parse payload -> load job -> OptsFromMap (no options) -> MarkActive
   process(): download in.<step|iges> -> Converter.Convert(ctx, in, out)
        CADConverter.Convert:
          1. size re-check vs ceiling            --over--> ErrCADInputTooLarge  (terminal, reuse input_too_large)
          2. stage cadguard (own ruleset, no OCP)   python -I -B guard   --> JSON verdict
                caps: entities 754762, NAUO 1518, cycle    --over--> ErrCADTooComplex (terminal)
                could not count                         --> guard failure (terminal, never a pass)
          3. stage cadmesh (own ruleset, OCP)       python -I -B mesh    --> out.stl + JSON verdict on stdout tail
                stage deadline 316 s                    --> ErrCADMeshTimeout (terminal, no retry)
                read failure / no shape / reader crash  --> ErrCADUnreadable (terminal)
                0 triangles / non-finite bbox           --> ErrCADEmptyResult (terminal)
                SIGKILL with ctx alive (cgroup OOM)     --> terminal (engine killed)
                helper exit 78 (landlock setup)         --> ErrEngineSandboxUnavailable (transient)
          4. judge out.stl by CONTENT: size == 84+50n, n>0, finite bbox
   upload out.stl (role primary) -> MarkDone
   |
   v
GET /v1/jobs/{id} -> presigned download_url (one binary STL, mm)

startup (cmd/cad-worker): RunExecModeIfRequested -> ResolveHelper(CAD_SANDBOX_DISABLED) -> CADPreflight
   (real minimal inch STEP: guard + mesh in job dir, STL content judged; same bytes outside job dir DENIED)
   -> only then db.Connect, storage, redis, asynq
```

### Recommended Project Structure

```
cmd/cad-worker/
  main.go                  # document-worker shape: sandbox -> preflight -> Postgres
  helper/cad_guard.py      # stdlib-only: counts, cycle check, JSON verdict
  helper/cad_mesh.py       # OCP: read, units, mesh, binary STL, JSON verdict
internal/convert/
  cad.go                   # CADConverter, Pairs (step->stl, iges->stl), Engine() = EngineCAD, errors
  cadsandbox.go            # cadGuardReadOnly, cadMeshReadOnly, engines, WithSandbox, CADPreflight
  cadjudge.go              # binary STL content judgment (84+50n, n>0, finite bbox)
internal/worker/descriptors.go   # CADDescriptor, isCADTerminal, cadFailureCode (+ 4 codes)
internal/queue/                  # TypeCADConvert, QueueCAD, CADRetryDelay, CADUniqueTTL, NewCADConvertTask, EnqueueCADConvert
Dockerfile.cad-worker
deploy/chart/octoconv/templates/deployment-cad-worker.yaml, scaledobject-cad.yaml
```

### Pattern 1: engineSandbox/engineExec per stage (two rulesets, one converter)

**What:** one `engineSandbox` built from the helper; `forJob(jobDir, readOnly, engines)` is called once per stage with that stage's own read-only set (soffice/verapdf precedent in `documentsandbox.go`).
**When to use:** guard and mesh. The guard set omits `/opt/venv` so OCP is unreachable from the stage that reads hostile bytes first.
**Example:**
```go
func (c CADConverter) stage(jobDir string, readOnly []string) engineExec {
	return c.Sandbox.forJob(jobDir, readOnly, cadEngines).withEnv(engineEnv(jobDir))
}
```
Source: `internal/convert/sandboxwrap.go:forJob/withEnv/engineEnv` (read this session).

### Pattern 2: preflight in the shared toolkit

**What:** `newPreflightWorkspace("cad")`, `place(name, data)` writes identical bytes inside and outside the job dir, `requireOutput`/`requireDenied` judge by content; `PandocPreflight` (`pandocsandbox.go`) is the closest template. For CAD the sufficiency judgment is the STL: triangle count and bbox; the positive control for the negative run is the same bytes under the same ruleset.
**Example (judgment shape):**
```go
type stlFacts struct {
	Triangles int
	Min, Max  [3]float64
}

func judgeBinarySTL(path string) (stlFacts, error)
```

### Pattern 3: single source of truth for ceilings and caps

Define `CADMaxSTEPBytes`, `CADMaxIGESBytes`, `CADMaxSTEPEntities`, `CADMaxNAUO` once in `internal/convert`; API ceilings, worker re-check and the helper's argv caps all read them. A pinned test names `71-DECISION.md` in its failure message (the `TestTectonicTimeoutAndInputCeilingAreOnePair` precedent, `internal/api/estimate_test.go:649`).

### Anti-Patterns to Avoid

- **Treating `no_solids` as a failure.** Every real IGES in the 71 corpus yields `solids:0;closed_solids:0;volume:null` yet meshes fine (Section 4.2). Empty output means 0 triangles, not 0 solids.
- **Using `engineExec.run` unbounded for an engine that prints through OCCT's messenger.** `engineExec.run` routes through `runCommandAs`/`runCommandCore(ctx, 0, ...)` with an unbounded buffer. Tectonic measured 235 bytes of input -> 323 MB of stdout in 30 s. A malformed STEP can make OCCT chatty. Add a bounded variant in `sandboxwrap.go` (not `exec.go`) that calls `runCommandCore(ctx, limit, env, ...)`; the status line must be last so the retained tail contains it.
- **A line-anchored regex as the entity counter.** `^|(?<=;)` + `#N=` misses entities hidden behind a preceding comment or other token (Pitfall 3).
- **Scaling in the helper when OCP already scaled.** Pitfall 6.
- **Tests that read `.planning/` for numbers.** Pin the numbers as Go constants with a failure message pointing at `71-DECISION.md`.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Process-group kill on timeout | A new exec wrapper | `runCommandCore` (`exec.go`), via `engineExec`/a new bounded variant in `sandboxwrap.go` | `exec.go` is an invariant; Setpgid + SIGKILL of the group is proven per class |
| Landlock setup and sandbox helper | Anything in the worker | `internal/sandbox` (`ResolveHelper`, `RunExecModeIfRequested`, `ExecMode`, `SetupExit`=78) | One security-relevant sequence whose order matters |
| Preflight workspace, inside/outside canary, denial judgment | A new harness | `sandboxpreflight.go` (`newPreflightWorkspace`, `place`, `requireOutput`, `requireDenied`, `preflightMarker`) | Already judges by content in both directions |
| Per-class sandbox-fault retry | New sentinel | `ErrEngineSandboxUnavailable` (`sandboxwrap.go`), checked first in the class classifier | `TestEngineSandboxUnavailableIsRetryableForEveryConfinedClass` pattern |
| Env allowlist for the engine | Manual env | `engineEnv(tmpDir)` + `engineExec.withEnv` | Closes `/proc/self/environ` secrets leak |
| Unique-lock TTL | Hard-coded TTL | `queue.CADUniqueTTL(maxRetry, timeout)` mirroring `TectonicUniqueTTL` | Derived in every enqueuing process; IN-02 |
| Unit conversion of CAD lengths | Own scale factors | OCP reader's `xstep.cascade.unit` (default MM) | Scaling comes from the file's conversion factor, not the unit name |
| STL writing | Own writer | `StlAPI_Writer` with `ASCIIMode = False` | Already what 71 measured |
| Magic-byte sniffing | New detection | `convert.SniffCADWindows` / `IsCADDeclared` / `ReadCADWindows` | Phase 72, differential-tested |

**Key insight:** every defect class this project has shipped for a new engine (a registry that skipped the class, a copied timeout, an unbounded engine log, a stale doc number) is held by a test derived from `AllEngines()`. Add the class to the list and let those tests drive the checklist.

## Section 1. What a new engine class touches (derived from phases 64 and 70, and from the tectonic grep)

Method: `git show --stat e443c23` (phase 64 PR #16, 98 files), grep of every non-planning file mentioning `tectonic`/`pandoc-worker`, and a read of every test that iterates `convert.AllEngines()`. The `cad` name collides with nothing: `jobs.engine` already allows it.

[VERIFIED: `internal/db/migrations/0001_init.sql:48`: `CHECK (engine IN ('image', 'document', 'av', 'cad', 'archive', 'probe')),` and `0016_tectonic_engine.sql` keeps `'cad'` in the list] so NO migration is needed for the engine column, unlike phases 5/6/9/10/16.

### 1a. Go registries (each fails a derived test until it has a `cad` entry)

| # | Anchor | Change |
|---|--------|--------|
| 1 | `internal/convert/convert.go:45` (`EngineTectonic = "tectonic"`), `:84-96` (`AllEngines`) | add `EngineCAD = "cad"` and append to `AllEngines()`; update the doc comment count |
| 2 | `internal/convert/converters.go:96` (`Default.Register(TectonicConverter{})`) | `Default.Register(CADConverter{})` zero value (no sandbox); `cmd/cad-worker` replaces it via `convert.Default.Replace` |
| 3 | `internal/convert/convert.go:190-191` | `stp`->`step`, `igs`->`iges` aliases already exist (phase 72); no change |
| 4 | `internal/convert/sniff.go:157` `MIMEType` | add `step`, `iges`, `stl` content types; today they fall to `application/octet-stream` for stored input and for the output (`worker.go:1457` uses `MIMEType(job.TargetFormat)`) |
| 5 | `internal/queue/queue.go:35` (`TypeTectonicConvert = "tectonic:convert"`), `:55` (`QueueTectonic`), `:548` (`RetryDelayFunc` switch), `:854` (`AllConvertQueues`), `:980-1020` (retry/backoff/TTL/task) | `TypeCADConvert = "cad:convert"`, `QueueCAD = convert.EngineCAD`, `CADRetryDelay`, `cadBackoffSum`, `CADUniqueTTL`, `NewCADConvertTask`; add to `AllConvertQueues` and the `RetryDelayFunc` switch |
| 6 | `internal/queue/client.go:105-108` fields, `:175-182` env reads, `:202-203` init, `:310` method | `cadMaxRetry`/`cadUniqueTTL`, `envInt("CAD_MAX_RETRY", 2)`, `envDuration("CAD_ENGINE_TIMEOUT", ...)`, `EnqueueCADConvert` |
| 7 | `internal/api/api.go:130` (`Enqueuer` interface) | `EnqueueCADConvert(ctx, jobID) error`; every fake enqueuer must implement it (Section 1d) |
| 8 | `internal/api/handlers.go:1196-1220` (`enqueueForEngine`) | `case convert.EngineCAD: return true, s.queue.EnqueueCADConvert(ctx, id)` |
| 9 | `internal/reconciler/reconciler.go:93` (`enqueuer` interface), `:388-395` (switch) | add `EnqueueCADConvert` and the `case convert.EngineCAD`; the `default` comment at `:396-405` says "cad/archive are out of scope this milestone" and must be reworded |
| 10 | `internal/api/api.go:429-453` (`defaultEngineTimeouts`) | `convert.EngineCAD: <CAD_ENGINE_TIMEOUT default>` |
| 11 | `internal/api/api.go:473-505` (`defaultMaxEngineBytes`) + new per-format map | engine-wide `155 << 20`; per-format 63/155 MiB (Section 4.5) |
| 12 | `internal/api/estimate.go:102-135` (`engineEstimateSource`), `:152-166` (`engineMeasuredDefaultSeconds`) | `convert.EngineCAD: EstimateSourceUnmeasured`; NOT in `engineMeasuredDefaultSeconds` (D-14: unmeasured) |
| 13 | `cmd/api/main.go:425-448` (`engineTimeoutsFromEnv`) | `convert.EngineCAD: envDuration("CAD_ENGINE_TIMEOUT", <default>)` |
| 14 | `internal/worker/descriptors.go:34-88` (codes + `allFailureCodes`), `:468` (descriptor pattern) | `CADDescriptor`, `isCADTerminal`, `cadFailureCode`, four new codes (Section 3) |
| 15 | `internal/api/handlers.go:703` (`s.maxEngineBytes[engine]` check) and `internal/api/estimate.go:353` (`limit, gated := s.maxEngineBytes[p.engine]`) | both must consult the per-format map first (create and inspect read one rule) |

No change needed: `internal/api/handlers.go:409-432` (CAD sniff wiring) and `inspect_handlers.go` already call the phase 72 detector; `jobs.engine` CHECK; metric labels (`cmd/api/main.go:106-117` derives the queue-depth collector from `queue.AllConvertQueues()`).

### 1b. Deploy and ops

| # | Anchor | Change |
|---|--------|--------|
| 16 | `Dockerfile.cad-worker` (new) | Section 8 |
| 17 | `docker-compose.yml:1579-1683` (`tectonic-worker` block as model) | new `cad-worker` service: `stop_grace_period` = timeout + 10 s asynq + 10 s margin; `deploy.resources.limits` `cpus: "2.0"`, `memory: 4g` (71 `memory_limit_mib=4096`); `CAD_WORKER_CONCURRENCY: "1"`; `CAD_SANDBOX_DISABLED: "${CAD_SANDBOX_DISABLED:-}"` |
| 18 | `docker-compose.yml` lines 197-198, 323-324, 454-455, 575-576, 683-684, 785-786, 904-905, 1051-1052, 1196-1197, 1352-1353, 1520-1521, 1628-1629 (the 12 `TECTONIC_ENGINE_TIMEOUT`/`TECTONIC_MAX_RETRY` pairs) | add `CAD_ENGINE_TIMEOUT` and `CAD_MAX_RETRY` to all 12 services plus the new `cad-worker` = 13; held by `TestComposeReplicatesEveryEngineVarToEveryQueueClientService` (`internal/queue/compose_in02_test.go`) |
| 19 | `.env.example:434-437` (tectonic block) | `CAD_ENGINE_TIMEOUT=`, `CAD_MAX_RETRY=`, `CAD_WORKER_CONCURRENCY=`; held by `TestEveryEngineVarIsDocumentedInEnvExample`; the timeout line must say PLACEHOLDER / not measured (see Section 9 `TestEstimateSourceMatchesEnvExampleProvenance`) |
| 20 | `deploy/chart/octoconv/templates/configmap.yaml:164-217` (tectonic block) | `CAD_ENGINE_TIMEOUT`, `CAD_MAX_RETRY`, `CAD_WORKER_CONCURRENCY: "1"`; `CAD_SANDBOX_DISABLED` deliberately absent (tectonic comment at `:205`) |
| 21 | `deploy/chart/octoconv/templates/deployment-tectonic-worker.yaml` (model) -> `deployment-cad-worker.yaml` | `terminationGracePeriodSeconds` from `.Values.cadWorker`; `octoconv.waitForDeps`; `octoconv.io/tier: app` (covers the networkpolicy automatically: header of the tectonic template says "no networkpolicy edit is needed") |
| 22 | `deploy/chart/octoconv/templates/scaledobject-tectonic.yaml` (model) -> `scaledobject-cad.yaml` | own `queue="cad"` PromQL literal; `octoconv.kedaMinReplicaCount` with `class: cad`; `minReplicaCount: 0` |
| 23 | `deploy/chart/octoconv/values.yaml:182` (`tectonicWorker:`), `:484` (`keda.tectonic:`) | `cadWorker` (image repo `octoconv-cad-worker`, `replicas: 1`, `terminationGracePeriodSeconds`, `resources.limits` cpu `"2"` memory `"4Gi"`) and `keda.cad` (threshold, `maxReplicaCount`, pollingInterval, cooldownPeriod, `scaleDownStabilizationSeconds`, `minReplicaCount: 0`). `octoconv.kedaMinReplicaCount` does `index .root.Values.keda .class` so a missing `keda.cad` aborts the render |
| 24 | `deploy/chart/octoconv/templates/configmap.yaml:30` `MAX_UPLOAD_BYTES: "104857600"` | see Pitfall 8 |
| 25 | `scripts/ci-helm-checks.sh:65` (`-ne 9` twice and the echo), `:72` (`for class in document html audio av ocr pandoc probe tectonic`) | 9 -> 10 ScaledObjects; add `cad` to the isolation loop |
| 26 | `internal/e2e/chart_grace_test.go:~36-42` (`workers` map: `"tectonicWorker": "TECTONIC_ENGINE_TIMEOUT"`) | `"cadWorker": "CAD_ENGINE_TIMEOUT"`; the test fails by name for a worker missing from the table |
| 27 | `.gitignore` block `# cmd-binaries-begin`..`# cmd-binaries-end` (entries such as `/tectonic-worker`) | `/cad-worker`; held by `internal/repohygiene/gitignore_sync_test.go` (compares to `cmd/` both ways) |
| 28 | `.github/workflows/ci.yml:139-140` and `:190` (per-service bake cache scopes) | `cad-worker.cache-to/-from` entries; `docker-compose.yml` services are all baked with no `targets:` so the new service builds automatically (no external fetch needed, unlike tectonic's bundle) |
| 29 | `scripts/sandbox-liveproof.sh` `class_row()` | `cad) echo "Dockerfile.cad-worker cad-worker CAD_SANDBOX_DISABLED yes -"`; usage/error text lists classes |
| 30 | `scripts/conf05-crosscheck.sh` (`class_go_file`, `class_go_readonly_var`, `class_go_engines_var`, header text) | add `cadguard` and `cadmesh` (Section 7.3) |
| 31 | `README.md:62-64` table row, `:~107` layout list (`cad-worker/`), `:329-333` engine-class marker line | add `cad`; `TestREADMEEngineClassesMatchAllEngines` (`internal/convert/readme_sync_test.go:88`) holds the line equal to `AllEngines()` both ways |
| 32 | `docs/ERROR-CODES.md:120` | the sentence "While no CAD engine is registered, a valid CAD file is answered `unsupported_conversion`." becomes false and is not test-held; edit it |
| 33 | `.planning/research/SANDBOX-PATHS.md` | new `## cad` section with `conf05:cadguard:*` and `conf05:cadmesh:*` marker blocks |

There is no Makefile, `mise.toml` or `hk.pkl` in the repo; the local gate stand-in is `scripts/ci-local.sh` (tiers `helm gate race docker-build e2e`, `scripts/ci-local.sh:31`). [VERIFIED: `ls` of the repo root this session]

### 1c. Not part of this phase (explicitly)

`docs/CAD.md`, OpenAPI async-code prose, MCP descriptions, frontend formats table (CADW-06, phase 76); `values-loadproof.yaml`/`values-inspect-gate.yaml` overlays do not list tectonic or probe either (no change); render/measurements (74); DXF (75); tuning (76).

### 1d. Test doubles that must gain `EnqueueCADConvert`

`internal/api/handlers_test.go:281` (`fakeQueue`), `internal/reconciler/reconciler_test.go:236` (`fakeEnqueuer`, plus `cadCallIDs` accessor at `:317` and the `accessors` map at `:606`), `internal/mcpserver/quota_admission_test.go:554` (`quotaCountingEnqueuer`). Missing one is a compile error, not a runtime surprise.

## Section 2. The worker skeleton

### 2.1 Which template

`cmd/document-worker/main.go` is the newer shape and the one D-01's "before touching Postgres" requires. `cmd/tectonic-worker/main.go` connects to Postgres first (`db.Connect` at `:47`) and its `preflightSandbox` function (`:346`) is not called from `main` (only `preflightEngine`, `:197`).

Order in `cmd/document-worker/main.go`: `sandbox.RunExecModeIfRequested(os.Args)` (`:54`) -> `signal.NotifyContext` (`:58`) -> `sandbox.ResolveHelper(documentSandboxDisabledEnv)` (`:68`) -> concurrency read and `convert.ConfinedDocument(sandboxHelper, concurrency, env)` (`:78-80`) -> `convert.Default.Replace(conv)` (`:98`) -> preflight under `context.WithTimeout(ctx, 60*time.Second)` (`:110`) -> only then `db.Connect` (`:120`), `storage.New`, `queue.RedisOpt`, cancel subscriber, `queue.NewClient`, `worker.NewHandler`, `asynq.ServeMux` with `worker.NewRunner(h, worker.DocumentDescriptor).Handle`, `asynq.NewServer` with `ShutdownTimeout: timeout + 10*time.Second`, metrics listener on `METRICS_ADDR` (default `127.0.0.1:9090`), `srv.Shutdown()` on `ctx.Done()`.

Every other confined worker follows `ResolveHelper -> <Class>Preflight(preflightCtx, conv) -> Postgres` (`cmd/pandoc-worker/main.go:74`, `cmd/probe-worker/main.go:127`, `cmd/audio-worker/main.go:109`, `cmd/av-worker/main.go:111`, `cmd/chromium-worker/main.go:83`, `cmd/ocr-worker/main.go:106`, `cmd/worker/main.go:68`). `sandbox.ResolveHelper` already fails with the message "Set %s=true to accept that deliberately", which is the "names the escape-hatch variable" requirement; no worker adds its own refusal text. [VERIFIED: `internal/sandbox/helper.go` read this session]

### 2.2 Escape-hatch variable and concurrency

Every class has its own, per-class name; there is no shared one: `IMAGE_SANDBOX_DISABLED` (`cmd/worker/main.go:34`), `DOCUMENT_SANDBOX_DISABLED` (`cmd/document-worker/main.go:34`), `HTML_SANDBOX_DISABLED`, `AUDIO_SANDBOX_DISABLED`, `AV_SANDBOX_DISABLED`, `OCR_SANDBOX_DISABLED`, `PANDOC_SANDBOX_DISABLED`, `PROBE_SANDBOX_DISABLED`, `TECTONIC_SANDBOX_DISABLED`. The cad name by that rule is `CAD_SANDBOX_DISABLED` (a convention, not a verified fact; the planner chooses). The value must be literally `true` (`ResolveHelper` compares `os.Getenv(disabledEnv) == "true"`).

Concurrency: `envInt("<CLASS>_WORKER_CONCURRENCY", 1)` feeds both the sharedTmp guard and `asynq.Config.Concurrency` in the document worker. CAD's rulesets grant no shared `/tmp`, so `sharedTmpGuard` does not apply, but the 4Gi derivation assumes one job at a time (71 `D-07`); recommend refusing a value other than 1 at startup with a message naming `CAD_WORKER_CONCURRENCY` (the document worker refuses >1 and <1; the tectonic worker only defaults to 1).

### 2.3 Content-judged preflight

Template: `PandocPreflight` (`internal/convert/pandocsandbox.go`): `newPreflightWorkspace`, `preflightMarker`, `place` (identical bytes inside and outside the job dir, outside copy read back before any ruleset), run the SAME converter value inside (sufficiency, judged by `requireOutput`), run it on the outside copy (effectiveness, judged by `requireDenied`). For CAD:

- Sufficiency: run the real guard then the real mesh on an embedded minimal STEP whose answer is known. `units_inch.step` is the right shape: 12 triangles, extents 1:2:3 x 25.4 mm. [VERIFIED: `evidence/71-04-helpers.txt:218` row for `units_inch.step`: `triangles:12;bbox_diag:95.038098`, and `25.4 * sqrt(1+4+9) = 95.038`] Judge: guard verdict `ok` with counts matching the fixture; `out.stl` binary, `84 + 50*12` bytes, bbox extents within tolerance of (25.4, 50.8, 76.2). This proves confinement is not too tight AND that units come out in mm in the shipped image.
- Effectiveness: the identical bytes at `outside` must fail. Note the 71 EFFECT row for `cad_mesh`: `outside_confined=denied,...,confined_rc=2,...,evidence=no_permission_text_in_log` [VERIFIED: `evidence/71-07-landlock.txt:140`], i.e. the helper exits 2 with no permission text, indistinguishable from "read failed". The preflight's negative run therefore judges by absence of an STL carrying the marker/known triangle count, not by an error message.
- `requireOutsideGrants` (`sandboxpreflight.go`) must be called with both stages' read-only sets so the outside directory is provably outside every grant (the temp dir is under `/tmp`; no CAD ruleset grants `/tmp`).
- ctx is the signal context so SIGINT/SIGTERM ends the preflight (`73e7b79 fix(70-07)`: "startup preflights end on SIGINT/SIGTERM in every worker").

### 2.4 Kill, timeout, reconciler: what proves "no live engine process, not held to the full timeout"

Mechanism today (read, not assumed):
- `runCommandCore` (`internal/convert/exec.go:194`) starts the child with `Setpgid: true` and on `ctx.Done()` sends `syscall.Kill(-pid, SIGKILL)` to the whole group, then `<-done` reaps. `engineExec.run` (`sandboxwrap.go`) goes through it with the engine named in errors and a helper exit 78 turned into `ErrEngineSandboxUnavailable`.
- Tests that prove the group kill exists per class, none for CAD yet: `internal/convert/convert_test.go:271` `TestRunCommandTimeout` (generic), `internal/convert/sandboxexec_test.go:312` `TestEngineExecKilledOnContextDeadline` (error names the engine, wraps `DeadlineExceeded`), `internal/convert/libreoffice_test.go:525` `TestLibreOfficeConverter_TimeoutKillsRealProcess` (document: polls `ps` for `soffice.bin`, kills, asserts zero survivors), `internal/convert/chromium_live_test.go:262` (html), `internal/convert/probebounds_test.go:366` (probe), `internal/convert/progressexec_test.go:129` `TestRunCommandStreaming_SIGKILLMidStream_NoLeak` (av/audio/ocr streaming), tectonic via `ErrTeXEngineKilled` classification (`tectonic.go:1357`).
- Worker death (SIGKILL of the worker, not a ctx cancel): the engine is in its own process group, so it is NOT killed by the worker's death; it dies because the container's PID 1 (the worker, no tini in the tectonic/pandoc images) exiting tears down the PID namespace. The task is recovered by asynq: a lease-expired task is listed with `cutoff := time.Now().Add(-30 * time.Second)` and the recoverer runs every minute, so recovery latency is roughly 60-120 s, then `retry` or `archive` by `msg.Retried >= msg.Retry` [CITED: `asynq@v0.26.0/recoverer.go:91-105`, `server.go:567` `interval: 1 * time.Minute`]. `MarkActive` allows `active -> active` (`internal/jobs/repo.go:239`: `[]string{StatusQueued, StatusActive}`), so a redelivered task proceeds. The reconciler is only the second line (`RECONCILER_ACTIVE_STALE_AFTER` `15m`, `deploy/chart/octoconv/templates/configmap.yaml:233`).
- No existing test or script proves worker-SIGKILL recovery per class. `scripts/sandbox-liveproof.sh` uses `docker kill` only as a watchdog on its own run; `scripts/cancel-liveproof.sh` proves `job:cancel` kills the engine; the `keda-*-downscale-survival.sh` scripts prove SIGTERM survival for av/ocr/pandoc. So SC4's last clause needs a new proof (below), and it needs docker.

Recommended proofs for CADW-03 (the planner should plan both):
1. Unit, no docker: a fake helper (shell script on `PATH`, `fakeEngine` precedent in `sandboxexec_test.go`) that forks a grandchild `sleep 300` and spins; run `CADConverter.Convert` with a 60 s stage timeout, cancel the parent ctx after ~200 ms, assert (a) `Convert` returns within ~2 s (not 60 s), (b) the grandchild PID is gone (`kill -0`). This is the `TestLibreOfficeConverter_TimeoutKillsRealProcess` shape with a deterministic fake instead of a real engine.
2. Live, docker (operator or Wave-N): start a compose `cad-worker`, submit a slow job (a derived NAUO file, 56 s at n=1000, `evidence/71-06-ladder-...par-off-lp2.txt:991`), `docker kill` the worker mid-mesh, assert no `python` process remains in the container namespace after kill and that after restart the job reaches `done` in about 60-120 s (asynq lease path), not at 900 s. Mark it `NOT RUN` rather than a pass if docker is unavailable (the `sandbox-liveproof.sh` convention).

## Section 3. Failure codes

### 3.1 What exists today

[VERIFIED: `internal/worker/descriptors.go:34-61` and `:72-88`] The async vocabulary (`allFailureCodes`) is: `engine_error`, `invalid_options`, `duration_exceeded`, `timecode_out_of_range`, `resolution_exceeded`, `no_video_stream`, `too_many_pages`, `page_too_large`, `image_too_large`, `unsupported_language`, `unsupported_target`, `input_too_large`, `output_too_large`, `reconciler_exhausted`, `unsupported_package` (fifteen). Historical: `born_digital`. `docs/ERROR-CODES.md:124` says "Fifteen codes".

Relevant facts:
- There is NO generic "engine timeout" code. Timeouts are `engine_error` with a distinct message (`tectonicFailureCode`: "the document did not finish typesetting within the time limit"). CONTEXT D-11's "a generic engine timeout if one exists" does not exist.
- `input_too_large` exists (`CodeInputTooLarge`, `descriptors.go:55`), documented as `pandoc` only (`docs/ERROR-CODES.md` row: "Input exceeds the configured `PANDOC_MAX_INPUT_BYTES` cap, rejected before the subprocess runs"). Client action (send a smaller file) matches the CAD worker-side byte re-check exactly.
- `output_too_large` exists, documented as tex-only ("only the tex engine can reach it").
- Precedent for a "new code only where the client action differs": tectonic added `unsupported_package` and `output_too_large` and mapped every other failure to `engine_error` with a distinguishing message.

### 3.2 Proposed minimal set under D-11

| Outcome | Terminal? | Code | Why |
|---------|-----------|------|-----|
| Sandbox cannot be established (helper exit 78) | NO, retry | none (`ErrEngineSandboxUnavailable`) | Node fault; checked first in `isCADTerminal`; add a `cad` case to `TestEngineSandboxUnavailableIsRetryableForEveryConfinedClass` and its control |
| Worker-side byte ceiling re-check (defence in depth) | yes | reuse `input_too_large` | Same client action as pandoc; extend the doc row's "Emitted by" to `pandoc, cad` |
| Entity cap, NAUO cap, assembly cycle/expansion, triangle cap | yes | NEW `cad_too_complex` | Client action differs from "smaller file": simplify or split the model |
| OCP could not read or transfer; no shape; reader crash (SIGSEGV/SIGABRT); guard could not count | yes | NEW `cad_unreadable` | Client action: re-export from the CAD system |
| Read fine, 0 triangles or non-finite bbox | yes | NEW `cad_empty_result` | Client action differs: the file has no meshable geometry |
| Mesh stage deadline | yes, no retry | NEW `cad_mesh_timeout` | Distinct code per D-11; never retried (Section 4.3) |
| Killed by SIGKILL with ctx alive (cgroup OOM) | yes | `cad_too_complex` (backstop for what the guard missed) | Precedent `ErrTeXEngineKilled`; terminal because concurrency is 1 and the container is fixed |

Four new codes, one reused. `cad_too_complex` and `cad_mesh_timeout` give clients similar remedies; D-11 asks for a separate timeout code and this keeps it, but merging them is a legitimate planner simplification (one fewer wire string).

### 3.3 Sync tests and docs that change with the codes

- `internal/errcode/doc_sync_test.go:TestErrorCodesDocMatchesGoConstants` (asynchronous table must equal `worker.AllFailureCodes()` both ways), `TestActiveAndHistoricalFailureCodesAreDisjoint`; `docs/ERROR-CODES.md` async table gets four rows, the "Fifteen codes" sentence (`:124`) and the `input_too_large` row's emitter list change. The 413 for per-format ceilings reuses sync `payload_too_large`, so no sync code and no `docs/openapi.yaml` `Error.code` enum change.
- New `internal/worker` pinning tests mirroring `TestPinnedTectonicFailureVerdicts` (`pinning_test.go:1096`): sandbox sentinel wins over a wrapped terminal shape; each terminal sentinel terminal; mesh timeout terminal AND a plain `context.DeadlineExceeded` (an S3 stall) NOT terminal (the PR #16 round-2 lesson: key the sentinel on the engine's own stage deadline, not on `DeadlineExceeded`).

## Section 4. Limits from 71 evidence

### 4.1 Decision lines (read from `71-DECISION.md`, not memory)

```
DECISION,key=memory_limit_mib,value=4096,...                 (71-DECISION.md:47)
DECISION,key=ceiling_step_mib,value=63,...                   (:48)
DECISION,key=ceiling_iges_mib,value=155,...                  (:49)
DECISION,key=cap_render_triangles,value=4014975,...          (:53)
DECISION,key=cap_step_entities,value=754762,...              (:54)
DECISION,key=cap_nauo,value=1518,...                         (:55)
DECISION,key=lp_num_threads,value=2,...                      (:57)
DECISION,key=occt_parallel,value=off,...                     (:58)
DECISION,key=quality_standard,value=0.001/0.35,...           (:61)
```
Bytes: 63 MiB = 66 060 288, 155 MiB = 162 529 280. All limits were derived at `--cpus 2`, 4096 MiB, `par=off,lp=2` (`:14-16`); at another `cpu` the numbers do not transfer. The pod limit 4Gi deviates from the fleet's 1Gi (71-DECISION "Лимит пода"); phase 76 owns the node-sizing consequence. A Deployment that sets only `limits` gets `requests = limits` in Kubernetes, so one cad pod requests 2 CPU and 4Gi once KEDA scales it from 0.

### 4.2 Worst measured mesh walls (source `evidence/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt`, `op=mesh`, par=off, 2 CPU, 4g)

| Series | Row (line) | Bytes (MiB) | Wall s | RSS / cgroup peak MiB | Triangles | Inside ceiling and caps? |
|--------|-----------|-------------|--------|------------------------|-----------|---------------------------|
| STEP copies (freecad) 40MiB | `:913` | 43.92 | 114.52 | 650 / 729 | 2 184 210 | yes: 43.92 <= 63; 445 330 entities <= 754 762; 510 NAUO <= 1 518 |
| STEP copies (freecad) 80MiB | `:917` | 79.24 | 210.63 | 979 / 1124 | 3 931 578 | no: above 63 MiB and above the entity cap (801 594) |
| STEP copies (LinearSlide) 40MiB | - | 39.84 | 13.25 | 851 / 889 | 428 256 | yes |
| NAUO n=1000 | `:991` | 1.10 | 56.35 | 268 / 513 | - | yes |
| NAUO n=2500 | `:995` | 1.99 | 123.41 | 449 / 933 | - | no: above the NAUO cap |
| NAUO n=10000 | `:1003` | 6.44 | 900.01, `rc=137`, `timeout=1` | 291 / 239 | - | no |
| IGES copies 20MiB (109.60 MiB real) | `:862` | 109.60 | 60.65 | 1474 / 1677 | 971 954 | yes (<= 155 MiB) |
| IGES copies 40MiB (274.01 MiB) | `:866` | 274.01 | 156.85 | 2411 / 2569 | 2 429 885 | no: above 155 MiB |
| IGES copies 80MiB (493.30 MiB) | - | 493.30 | 189.79, `rc=137`, `oom=1` | 4089 / 4096 | - | no |

(Entity and NAUO counts per file from `evidence/71-03-corpus-inventory.txt` `CNT` rows joined by file name; the evidence file's `bytes=` for the IGES copies is the real size, 114 920 208 and 287 323 362.)

Two facts that matter for design: every real IGES row in the ladder reports `solids:0;closed_solids:0;volume:null` yet meshes with thousands of triangles (for example `freecad-festo-valve-5-2.igs`: 65 392 triangles, 4.93 s), so `no_solids` cannot mean "empty output" (Anti-Pattern 1); and a 79.24 MiB freecad-copies STEP already yields 3 931 578 triangles, essentially the 71 render triangle cap (4 014 975), so a triangle cap on convert output is coherent (STL of 4 014 975 triangles is 191 MiB, under the 277 MiB binary-STL ceiling).

### 4.3 Mesh-stage timeout derivation

The literal reading of D-10 ("worst measured wall at or below the ceiling sizes"): STEP 114.52 s (`:913`, 43.92 MiB <= 63) and IGES 60.65 s (`:862`, 109.60 MiB <= 155). Times a 1.5 margin (the margin tectonic and probe use, `internal/api/api.go:429-453` comments): `ceil(114.52 x 1.5) = 172 s`.

That number is not a bound at the ceiling. Linear interpolation between the two freecad rungs on either side of 63 MiB gives `114.52 + (63 - 43.92)/(79.24 - 43.92) x (210.63 - 114.52) = 166.4 s`, so 172 s covers a legitimate 63 MiB file by 3%. On this engine a mesh timeout is terminal (D-11), so an undersized bound refuses legitimate files with no retry. The tectonic precedent measured on a document slightly LARGER than the ceiling for the same reason (`api.go:441-446`).

Derivation that brackets from above (recommended):
- STEP: the first rung above both the 63 MiB byte ceiling and the entity cap is 79.24 MiB at 210.63 s (26% larger than the ceiling, 6% above the entity cap). Wall is monotone in this series, so the wall at the ceiling is below 210.63 s.
- IGES: the first rung above 155 MiB is 274.01 MiB at 156.85 s; interpolated at 155 MiB it is 87.2 s.
- NAUO at the cap (1 518): interpolated 79.5 s; worst rung above the cap 123.41 s.
- Interpolated at the entity cap (754 762): 198.0 s.
- Worst bracket: `210.63 s`. Margin 1.5: `ceil(210.63 x 1.5) = 316 s`. (At 2.0 it would be 422 s; both are under the 900 s reconciler threshold, 316 s with 584 s to spare.)

Recommended settings, all derived and to be re-derived in the same edit if any input changes:
- `CAD_MESH_TIMEOUT` (worker-only stage deadline): **316 s**.
- Guard stage deadline: 30 s as a placeholder. Guard time at 63 MiB STEP and 155 MiB IGES is NOT measured anywhere (Assumption A7); measure it in the production image in Wave 0 and set it to a multiple of the measured worst.
- `CAD_ENGINE_TIMEOUT` (attempt-level; read by `queue.NewClient`, the API publication, the worker's `ShutdownTimeout`): guard + mesh + I/O allowance = `30 + 316 + 60 = 406 s`. The 60 s covers S3 download of up to 155 MiB and upload of up to ~191 MiB; it is a reasoned allowance (A7). `CadUniqueTTL = (2+1) x 406 + backoff + 120 s`.
- Pod grace: `406 + 10 + 10 = 426 s` (`TestChartGraceOutlivesAsynqShutdown`: grace must exceed ShutdownTimeout by >= 10 s).

Unmeasured and stated: a file at BOTH caps at once (NAUO about 1 518 and entities about 754 762). The two costs were measured on separate series (`step-nauo` is a flat prototype copy with 20 240 entities at n=1000; the freecad series carries 510 NAUO at 445 330 entities), and if they add, the combined case is about 80 s + 198 s = 280 s, under 316 s but with a 1.13 margin, not 1.5. Flag it in the evidence rather than hiding it (Open Question 2).

The stage timeout must fire strictly BEFORE the attempt deadline: `Runner.process` bounds the whole attempt (download, convert, upload) with one deadline, and `context.DeadlineExceeded` from an S3 stall is transient on every engine. Classify mesh timeout by a sentinel set only when the STAGE context expired while the engine ran (`ErrTeXEngineTimeout` pattern, `tectonic.go:1333`), so a stalled upload is retried and a non-terminating mesh is not.

### 4.4 Memory: what bounds a run (correction to D-10)

[VERIFIED: `internal/convert/cgroup.go:1-65`] The file contains `parseCPUMax` and `CgroupCPULimit`, which read `/sys/fs/cgroup/cpu.max` to size whisper-cli's threads. No file in the repo reads or writes `memory.max`, `memory.peak`, `RLIMIT_AS` or `RLIMIT_DATA` (`grep` over `*.go`, `*.yaml`, `Dockerfile*`: hits only for `RLIMIT_CPU` in `internal/sandbox/limits_unix.go:45` and tectonic text). The only memory bounds in the fleet are the container limit (chart `resources.limits.memory`, compose `deploy.resources.limits.memory`) and input-size ceilings (pandoc's 3 MiB `PANDOC_MAX_INPUT_BYTES` was derived from an OOMKill at 1Gi).

So the honest memory story for this phase:
1. Pre-emptive bound: the guard caps (entities 754 762 and NAUO 1 518 were derived as `(0.70 x 4096 - floor - Go) / k`, so a file within them stays under 70% of the limit in the 71 pipeline) plus the byte ceilings. For a mesh-only worker the headroom is larger than the render pipeline the caps were derived for: the worst mesh within caps is 1124 MiB cgroup peak (`:917`).
2. Backstop: container cgroup 4Gi. The kernel OOM-kills the largest process, which is the Python child, not the Go worker (Go RSS 24.9 MiB, `evidence/71-06-go-worker-rss.txt`). `runCommandCore` returns an `*exec.ExitError` with `WaitStatus.Signaled()` and `SIGKILL`; classify "killed with ctx alive" as terminal exactly as tectonic does (`tectonic.go:1353-1358`). Tectonic's measured result applies in kind: "The container survives (OOMKilled fires for the child, PID 1 lives)".
3. Do NOT add RLIMIT_AS/RLIMIT_DATA or a Go-side RSS watchdog without a measurement and without extending `sandbox.Limits` (today only `CPUSeconds`): OCP maps far more address space than it touches, so RLIMIT_AS would refuse legitimate jobs; a watchdog needs the child pid, which `runCommandCore` does not expose and `exec.go` must not change.
4. Success criterion 3 ("worker alive, OOMKilled none") is therefore a property of the guard and the timeout holding, not of an OOM backstop. Every bomb shape must be shown to end by guard or stage deadline in a real container at `--memory 4g --cpus 2`, with `docker inspect` `OOMKilled=false` and `oom_kill` unchanged in `memory.events`, as a Wave-N proof (Section 6.4).

### 4.5 How the API enforces byte ceilings today, and what is missing

- Pre-parse: `http.MaxBytesReader(w, r.Body, s.maxUploadByte)` (`handlers.go:148`); `MAX_UPLOAD_BYTES` default 2 GiB in compose (`docker-compose.yml:110` `"2147483648"`) and `cmd/api/main.go:216` default `2<<30`, but `104857600` (100 MiB) in the chart ConfigMap (`configmap.yaml:30`).
- Per engine, post-detection: `s.maxEngineBytes[engine]` (`handlers.go:703`) from `defaultMaxEngineBytes` (`api.go:473-505`), 413 `payload_too_large` with the fixed message "file exceeds size limit for this conversion type". The same map is read by inspect (`estimate.go:353`) for `available`/`unavailable_reason`/`max_bytes`.
- There is NO per-format size map. D-08 needs one: STEP 63 MiB and IGES 155 MiB differ within one engine, and the message must name the format and ceiling. Recommended: a second map keyed by `(engine, format)` consulted first by `handleCreateJob` and by the inspect estimate (so `max_bytes` for a step offer is 63 MiB and for iges 155 MiB), with the engine-wide `defaultMaxEngineBytes[EngineCAD]` set to the larger (155 MiB) so any code that reads only the engine map is not stricter than the format map. Keep every other class's 413 message byte-identical (`pre58_createjob_rejections.golden.json` pins existing rejections); only a class with a per-format entry gets the new message, for example "step file exceeds the 63 MiB limit for this conversion".
- Inspect pre-parse ceiling `INSPECT_MAX_UPLOAD_BYTES` is 100 MiB (`docker-compose.yml:127`, `configmap.yaml:41`): an IGES between 100 and 155 MiB can be created but not inspected (413 at the transport). Pre-existing shape (av's 2 GiB has the same mismatch); note it, do not fix it here.

## Section 5. Units

### 5.1 What OCP does (resolved from existing evidence; the CONTEXT premise "not proven" is out of date)

- OCCT converts lengths to the target unit configured by `xstep.cascade.unit`, default MM. [CITED: occt3d.com/dev/doc/overview/html/occt_user_guides__step.html: "defines units to which a shape should be converted when translated from IGES or STEP to CASCADE. Normally it is MM"]
- `nist_ctc_05_asme1_ap242-e1.stp` declares its length unit as a complex entity: `CONVERSION_BASED_UNIT('inch',#13404) LENGTH_UNIT() NAMED_UNIT(#13406)` (read from the committed file this session). OCP `FileUnits` returned an empty name for it (`unit_fileunits:unknown;unit_agree:0`). [VERIFIED: `evidence/71-04-helpers.txt:108` row: `unit_text:inch;unit_fileunits:unknown;unit_agree:0;triangles:9450;bbox_diag:907.168765;bbox_source:shape`]
- Ground truth independent of OCP, computed this session from the committed file's `VERTEX_POINT` coordinates (file units): extents `[20.75, 17.5, 19.0]`, diagonal `33.1333` inch; times 25.4 = `841.585` mm. OCP reported `bbox_diag 907.168765`. If OCP had NOT scaled, the diagonal would be about 33-36 (inch numbers); 907 is 25.4 times that, so OCP scaled it. The 7.8% excess over the vertex-box figure is `BRepBndLib.Add_s` looseness (it bounds NURBS by control polygons; the V-Slot row shows it can even return +-1e100), not unit error.
- Second inch file, uppercase `'INCH'`: `nist-d2mi-827-9999-906.stp`, vertex diagonal `4.9243` inch x 25.4 = `125.077` mm; OCP `bbox_diag 125.920841`, `unit_fileunits:inch` (`evidence/71-04-helpers.txt:98`). Agreement within 0.7%.
- Conclusion: scaling uses the conversion factor in the file (`#13404` carries 25.4), not the name's case. Only the reported name is empty for lowercase `'inch'`.

### 5.2 What the helper must do

1. Do not scale in the helper. Scaling a second time multiplies by 25.4 again. D-06's "if OCP did not scale it, the helper scales explicitly" has no demonstrated trigger on the corpus; if the planner keeps an explicit fallback, its trigger must be a measured disagreement between the text-declared unit and the OUTPUT mesh (bbox) and it must be proven on both files that it does not fire.
2. Set the target unit explicitly rather than relying on a default: `Interface_Static.SetCVal_s("xstep.cascade.unit", "MM")` before `ReadFile` (the 71 helper relied on the default). [ASSUMED: exact OCP binding spelling; verify in the image]
3. Report `unit_text` (read from the file text exactly as `cad_mesh.py:step_text_facts` does: `CONV_RE` on `CONVERSION_BASED_UNIT('...')`, `SI_RE`) as a diagnostic in the status JSON; it is the source of truth for provenance, `FileUnits` is only a flag.
4. IGES: the same reader parameter applies. The corpus has only mm IGES (`unit_flag=2`, 4 files, 71-DECISION "Единицы"), so IGES inch scaling is UNPROVEN. Generate a deterministic inch IGES (the `iges-from-step` kind of `scripts/cad-corpus-gen.py` run with an inch write unit, or write-unit `INCH` in OCP's IGES writer) and add it to the mm test (Open Question 3).
5. Compute the deflection basis from a finite box: keep the `cad_mesh.py:323-348` logic (`FINITE_LIMIT = 1e12`, per-face `AddOptimal_s` fallback, reject empty or zero-size box). 71 FINDING for `V-Slot-20x20-100mm.step`: `BRepBndLib.Add_s` returned +-1e100 and the tracer produced a 672-triangle mesh. [VERIFIED: `evidence/71-04-helpers.txt:253`]

### 5.3 Proposed bbox test for both inch files (mm proof)

Judge the OUTPUT STL, not OCP's own box: parse the binary STL and compute vertex min/max. Extents are bounded by [vertex box, BndLib box] because tessellation covers every edge/face and `Add_s` is looser than the tessellation hull.
- `nist-d2mi-827-9999-906.stp`: STL diagonal within 3% of `125.08` mm (OCP shape box says 125.92).
- `nist_ctc_05_asme1_ap242-e1.stp`: STL diagonal in `[841.0, 908.0]` mm (vertex box 841.6; shape box 907.2) AND not within `[30, 40]`, which is the unscaled signature.
- `units_inch.step` (synthetic, deterministic): extents sorted equal `(25.4, 50.8, 76.2)` within 1e-3 and 12 triangles; this is the preflight fixture.
Compute the expected values in the test from the fixture's own text (parse `CARTESIAN_POINT` of `VERTEX_POINT` and the declared conversion factor), so the expectation comes from the file and not from OCP.

## Section 6. Guard pre-pass

### 6.1 How 71 counted

[VERIFIED: `scripts/cad-measure/counters.py:20-24`]
```
STEP_ENTITY = re.compile(rb"(?:^|(?<=;))[ \t]*#\d+[ \t]*=", re.M)
STEP_NAUO = re.compile(rb"(?<![A-Z_0-9])NEXT_ASSEMBLY_USAGE_OCCURRENCE[ \t\r\n]*\(")
```
`count_step` opens the file with `mmap`, finds `DATA;`, and sums `finditer` matches (O(1) memory beyond the mapping; mapped file pages count toward RSS/page cache but are bounded by the byte ceiling).

### 6.2 Can production count in bounded memory before OCP loads the file? Yes, and conservatively

Recommended counters (pure stdlib, Python 3.11-safe, no OCP import, runs in its own ruleset):
- Entity upper bound = number of `;` after `DATA;`. Every entity statement must end with `;`, so this cannot undercount, and it needs no line or comment assumptions. Verified this session on all 24 committed STEP files: the count of `;` from `DATA;` onward equals the line-anchored entity count plus exactly 3 (`DATA;`, `ENDSEC;`, `END-ISO-10303-21;`) in every file; the largest ratio is 1.0319 (a 94-entity file). So subtract nothing and compare `> cap + 3`, or compare the raw count to the cap (stricter by 3 entities).
- Read in chunks (`fh.read(1 << 24)`, `bytes.count(b";")`) with an early exit once above the cap. Memory is one chunk; time is linear and C-speed.
- NAUO: `mmap.find(b"NEXT_ASSEMBLY_USAGE_OCCURRENCE", pos)` in a loop with early exit at cap+1. Counting the bare token (no `(`) can only over-count (strings, comments), which is the safe direction.
- IGES: no cap exists in 71 for IGES entities; the guard for IGES is the byte ceiling plus a structural record check (the sniffer already does section counts). State this plainly in the evidence instead of implying an IGES cap.
- "Could not count" (file unreadable in the stage, `mmap` failure, unexpected exception) is `cad_unreadable`/guard failure, never a pass (project rule).

### 6.3 What 71 does NOT tell us about bombs, and the design consequence

- No self-referencing STEP, non-numeric-coordinate STEP or hierarchy bomb exists anywhere in the repo: `grep -i "self-ref|bomb|non-numeric"` over `scripts/`, 71, 72 and `.planning/research/` finds only the PITFALLS.md checklist line ("self-referencing STEP, deep assembly, `MINSERT` bomb, cyclic DXF blocks, binary STL with huge `n`, `1e300` coordinates all end in a coded terminal failure within bounds", `.planning/research/PITFALLS.md:299`). The 15 `reject/` fixtures are truncation/BOM/ambiguity cases, not bombs. No 71 row records what OCP does on them.
- The NAUO count is an EDGE count. A hierarchy where each level references the same child twice has a handful of NAUO statements and an exponential number of expanded instances; `MAPPED_ITEM` nesting expands the same way with no NAUO at all. A count-only guard bounds neither. [ASSUMED: that OCP expands shared definitions on read or mesh; this is exactly what a spike must establish] Backstops that do exist are the stage deadline (316 s) and the cgroup OOM classification; the guard can additionally build the NAUO graph (at most 1 518 edges after the count passes) and reject cycles and saturating-sum expanded instance counts above the cap, and can count `MAPPED_ITEM` tokens the same way. Whether to add those is decided by the spike results, not by this document.
- Non-numeric coordinates may NOT fail the read: OCCT's STEP reader can report syntax problems in its check list and still return `IFSelect_RetDone` with an entity dropped, which would yield an incomplete STL with exit 0. [ASSUMED] The helper should read the load check list (`PrintCheckLoad` in failsonly mode or the model's checks) and the production verdict must be derived from it; but the same check could fire on legitimate corpus files, so measure it over the 87-file corpus before choosing strictness (Open Question 1).

### 6.4 Bomb fixtures, deterministic, and how to test them

Generate in the Go/Python test (bytes in memory or `t.TempDir()`), not committed, except the three tiny OCP-reader fixtures:
1. `step_nauo_over_cap`: header + N `NEXT_ASSEMBLY_USAGE_OCCURRENCE` statements (N = 1 518 passes the NAUO check, 1 519 fails). The 71 `scripts/cad-corpus-gen.py --kind step-nauo --targets 1519` produces a real-geometry variant (n1000 is 1.10 MiB, n2500 is 1.99 MiB, so n1519 is below the 2 MiB commit threshold if a committed fixture is preferred).
2. `step_entities_over_cap`: DATA with 754 763 trivial statements (about 15 MB). Boundary pair: cap passes, cap+1 fails. Also an evasion variant that puts a `/* x */` comment before each statement or puts statements on one line, proving the `;` counter is not fooled while a line-anchored regex would be (differential: count by `;` vs count by regex must satisfy `semicolons >= regex`).
3. `step_self_ref_nauo`: a `NEXT_ASSEMBLY_USAGE_OCCURRENCE` whose relating and related definitions are the same id, plus a two-node cycle. ~2 KiB each.
4. `step_nonnumeric_coords`: `CARTESIAN_POINT('',(1.0,abc,3.0))`, plus `NaN`, `1E999`, `1..2`, `--1`. ~2 KiB.
5. `step_huge_coords`: `1E300` coordinates (PITFALLS list item; finite-bbox path).
6. STL lying triangle count: bytes in the test: 80-byte header + little-endian `0xFFFFFFFF` + 50 bytes. The phase 72 sniffer rejects it as `truncated_cad_file` (size is not `84 + 50 n`) before the pair lookup, which the existing `postCADCreate` helper (target `png`) already demonstrates for the committed truncated fixtures; the job-level proof is: `POST /v1/jobs` returns 422 `truncated_cad_file`, `fakeStorage` saw no upload, `fakeQueue` saw no enqueue. No existing reject fixture carries a huge `n` (`stl_binary_truncated.stl` has `n=12`), so this one is new.

Tests for 3-5 against the REAL reader require the image (docker); run them as a Wave-0 spike script modelled on the 71 ladder (`--memory 4g --cpus 2`, 900 s budget, record rc, wall, child RSS, `cg_peak`, `oom_kill`, stdout size) BEFORE any guard rule or classification is finalised, and commit the observed outcomes as evidence. The guard unit tests (1, 2, STL) need only `python3` on the host (skip when absent) and run in `go test`.

## Section 7. Landlock for the production image

### 7.1 What 71 found for cad_mesh

[VERIFIED: `evidence/71-07-landlock.txt:188`]
```
LANDLOCK_MINIMAL,helper=cad_mesh,rw=/job,ro=/opt/cad-measure:/opt/venv:/usr/bin/python3.11:/usr/lib/aarch64-linux-gnu:/usr/lib/python3.11,note=candidate for phase 73 not a grant
```
Removal table (`:` rows `REMOVAL,helper=cad_mesh,...`): `/opt/cad-measure`, `/opt/venv`, `/usr/bin/python3.11`, `/usr/lib/aarch64-linux-gnu`, `/usr/lib/python3.11` are `load-bearing`; `/etc/ld.so.cache`, `/etc/nsswitch.conf`, `/etc/passwd`, `/sys/devices/system/cpu`, `/usr/lib/locale`, `/usr/share/zoneinfo` are `removable`. `/proc`, `/sys`, `/tmp` are not needed (`TMP_NEED,helper=cad_mesh,none`). Measured at `LP_NUM_THREADS=2`, `occt_parallel=off`, user `nobody`, native arm64, on the MEASUREMENT image (which also had libosmesa6, librsvg2-bin, fonts, VTK), with `memory=2g` in the header (irrelevant to paths).

The trace of the cad_mesh process opens `libGL.so.1`, `libGLX.so.0`, `libGLdispatch.so.0`, `libX11.so.6` (`evidence/71-07-landlock.txt:88`): `import OCP` pulls OpenGL/X11 client libraries through `libTKOpenGl`/`libTKService`. All of them live under the multiarch directory, so the five-path minimum already covers them.

### 7.2 Arch parametrisation (71 W17)

The set is arm64-only (`/usr/lib/aarch64-linux-gnu`); amd64 uses `/usr/lib/x86_64-linux-gnu`. `sandbox.addPath` treats a missing path as an ERROR (`sandbox_linux.go` `addPath`: "A path that does not exist is an ERROR rather than a skip"), so listing both triplets in one set breaks on both. Precedents avoid the problem by granting a whole arch-neutral directory (`sofficeReadOnly` and `verapdfReadOnly` grant `/usr/lib`, `probeReadOnly` grants `/lib` and `/usr/lib`). Options for the planner:
- (a) Grant `/usr/lib` whole for both stages (class precedent; weaker, but reads of `/usr/lib` leak nothing secret). Simplest crosscheck.
- (b) Keep the measured minimum and add `/usr/lib/<triplet>` chosen at spec-build time from `runtime.GOARCH` (`arm64` -> `aarch64-linux-gnu`, `amd64` -> `x86_64-linux-gnu`); the arch-neutral entries stay in a flat `cadMeshReadOnly` var and the triplet map gets its own conf05 block kind; `conf05-crosscheck.sh` needs one new block kind.
D-12 asks for (b). emulated amd64 cannot establish Landlock, so an amd64 measurement needs a native amd64 host; it is deferred (CONTEXT Deferred Ideas), but the amd64 build and an amd64 path-existence check (`ls`) in the image are cheap and should be a Wave-0 task.

### 7.3 How a set is held (per-class Go test, SANDBOX-PATHS block, crosscheck)

- Go var: `var <class>ReadOnly = []string{...}` and `var <class>Engines = []string{...}` in `internal/convert/<class>sandbox.go` (`pandocsandbox.go:23`, `documentsandbox.go:51`).
- Doc: `.planning/research/SANDBOX-PATHS.md` section with HTML-comment marker pairs `<!-- conf05:<class>:readonly -->` / `<!-- /conf05:<class>:readonly -->` and `...:engines`, one entry per line (`SANDBOX-PATHS.md:488-492` for pandoc). A class with a shared writable set also needs `:readwrite`; CAD writes only the job dir.
- Script: `scripts/conf05-crosscheck.sh [--doc] <class>...` diffs the doc lists against the Go vars; exit 2 on a missing block, exit 3 on a missing Go var; it parses `var <name> = []string{` literals only (`extract_go_list`). Add `cadguard` and `cadmesh` to the three `case` helpers (`class_go_file`, `class_go_readonly_var`, `class_go_engines_var`), the usage text, and (document precedent) a `cad` alias expanding to both. The script's awk reads quoted strings from a gofmt-shaped literal; a computed triplet entry cannot live in that var (hence option (b)'s separate block kind).
- Go test: `Test<Class>ReadOnlySetIsTheMeasuredOne` / `...EnginesAreTheMeasuredOnes` (`pandocsandbox_test.go:15-37`, `documentsandbox_test.go:36-80`), plus `TestNeitherEngineIsGrantedUsrBinWholesale` (document) and the AST scanner `internal/convert/sandboxcoverage_test.go`, which fails the build when a file in `internal/convert` runs an engine outside `engineExec.run`/`.stream` or is not assigned to a class: new `cad*.go` files must be added to `sandboxClassFileEngines` (`:~78-98`), `sandboxClassFileGroups` (`:~104-120`) and `sandboxGroupEngines`, and the per-class engine equality check.

### 7.4 Re-measurement on the production image

Reuse `scripts/cad-sandbox-measure.sh` (569 lines, 71 `EVID` paths, uses `cad_build_image` from `scripts/cad-measure/common.sh` and `scripts/sandbox-paths-measure.sh trace|verify`, `scripts/cmd/sandbox-probe`). Parametrise: image tag (production `octoconv-cad-worker:<tag>` instead of the measure image), helper path (`/opt/<helper>/cad_*.py` instead of `/opt/cad-measure`), stage list (guard, mesh), output file (a phase 73 evidence file). Requirements it must keep: production argv (`/opt/venv/bin/python -I -B ...` for mesh), env only `HOME`, `LANG`, `PROBE_*`, `USER nobody`, judge by content against an unconfined run, an outside-job-dir input proving effectiveness (the 71 canary `/etc/hostname`), a removal table (one entry at a time), a negative control (an entry removed from the set must make sufficiency fail, which proves the test can fail), `--recheck` mode. New for this phase: the guard stage runs under `/usr/bin/python3` (not the venv) with its own removal table; measure whether the guard needs `/opt/venv` at all (it should not: stdlib only, but `/opt/venv/bin/python` reads `pyvenv.cfg`, so a venv interpreter would force the grant). Native only: the script refuses an emulated image (`cad_require_native`).

## Section 8. Dockerfile.cad-worker

- Shape: stage 1 `golang:1.26-bookworm` builds `CGO_ENABLED=0 go build -o /out/cad-worker ./cmd/cad-worker` (every worker Dockerfile); stage 2 the 71 `venv` stage (`debian:bookworm-slim@sha256:3783cc01...` + `python3 python3-venv` + `python3 -m venv /opt/venv` + the two `pip install --require-hashes` commands above) WITHOUT `requirements-vtk.txt` and WITHOUT the `venv-pruned` stage; stage 3 `debian:bookworm-slim@sha256:3783cc01...` runtime with `COPY --from=venv /opt/venv /opt/venv`, the worker binary, the helper dir, `compileall -q` of the helper (71 does this, and `-B` at run time means no `.pyc` writes under a read-only grant), an import smoke `RUN /opt/venv/bin/python -I -B -c "import OCP"`, `USER nobody`, `ENTRYPOINT ["/usr/local/bin/cad-worker"]` (tectonic and pandoc call the binary directly; tini is used only where grandchildren exist, and a single-process Python/OCP with `occt_parallel=off` has none [ASSUMED; confirm by a process listing during a mesh], and tini would be a new apt package under D-04).
- Multi-arch: the base reference is an index digest; the OCP lock lists two wheel hashes (aarch64 and x86_64) so pip selects the right one under `--only-binary=:all:`; pure-Python and numpy wheels are multi-arch; the Go stage builds natively per platform. Do not copy the 71 keep-list pruning (VTK is absent from this image).
- No-VTK feasibility: [VERIFIED: `scripts/cad-measure/cad_mesh.py:245-258`] `mesh()` imports only `OCP.BRep`, `OCP.BRepBndLib`, `OCP.BRepCheck`, `OCP.BRepGProp`, `OCP.BRepMesh`, `OCP.Bnd`, `OCP.GProp`, `OCP.IFSelect`, `OCP.IGESControl`, `OCP.STEPControl`, `OCP.StlAPI`, `OCP.TopAbs`, `OCP.TopExp`; `numpy` is imported only by `read_stl`/`stlstats`, which the production helper drops. The wheel is `cadquery-ocp-novtk`, so VTK is not a dependency. `import OCP` does still need system libraries: `libX11.so.6` (`libTKService` DT_NEEDED, `evidence/71-07-libtrace.txt:70`) and, per the cad_mesh trace, `libGL.so.1`/`libGLX.so.0`/`libGLdispatch.so.0`. So the apt set for this image is `python3 libgl1 libx11-6` (a strict subset of the 71 list `python3 libosmesa6 libgl1 libx11-6 librsvg2-bin fonts-dejavu-core`); `libosmesa6`, `librsvg2-bin`, `fonts-dejavu-core` are for render/DXF. System libs `libc`, `libm`, `libpthread`, `libstdc++.so.6`, `libgcc_s.so.1`, `libz.so.1` are also needed (the first four from the base; `libstdc++6`/`libz` presence in bookworm-slim is [ASSUMED]; the import smoke test proves it).
- `ca-certificates`: every other runtime image installs it for TLS S3 (`Dockerfile.document-worker:33`, `Dockerfile.probe-worker:48`, ...). The 71 list does not contain it. D-04 forbids new apt packages without a checkpoint; the Go worker cannot reach a TLS S3 without it, so this is a decision for the user (Open Question 4).
- Helper placement (planner's choice): `cmd/cad-worker/helper/` keeps the Python next to the binary that ships it, and `.dockerignore` (`bin/`, `.env`, `.git/`, `*.md`) does not exclude it. Keep `cad_guard.py` importable without OCP so Go tests can run it with host `python3`.

## Section 9. Phase 72 guard tests that assert "no CAD pair", and what else flips

| Test | Anchor | Today | Change in the atomic commit |
|------|--------|-------|------------------------------|
| `TestDefaultRegistersNoCADPair` | `internal/convert/convert_test.go:303` | fails if any registered pair touches `step stp iges igs stl dxf dwg` or the class is `cad` | Replace the blanket ban with an exact allowlist: the only CAD pairs are `step->stl` and `iges->stl`, engine `cad`, operation `convert`; any `stl`/`dxf`/`dwg` source, any `render`/`measurements`, any other target still fails. Keep the vacuity guard (`walked == 0`). Do not delete the test (it is the phase 74/75 tripwire) |
| `TestListFormats_AdvertisesNoCADFormat` | `internal/api/formats_handlers_test.go:257` | same ban on `/v1/formats` | Same allowlist on the response; the "extract/archive/inspect/render are not registered" test (`:~245`) stays true until phase 74 |
| `TestAllEnginesCoversEveryEngineClass` | `internal/convert/convert_test.go:183` | literal `len(got) != 9` and a 9-entry `want` map | 10 and add `EngineCAD`; also the routed-class cross-checks below it |
| `TestListFormats_EnginesKeyUnchanged` | `internal/api/formats_handlers_test.go:~86` | raw byte prefix against `pre57_formats.golden.json` | `cad` sorts between `av` and `document` in the engines map, so the prefix check fails. The golden already contains `tectonic` because phase 64 edited it in place (`git log` shows two commits touching it, `e443c23` among them), contradicting the file's own "must print 1" rule. Preferred: keep golden bytes untouched and compare after stripping `cad` from the live body with `withoutEngine` (`pre58_pinning_test.go:399`), asserting the key is present (the tectonic comparator's non-vacuity clause) |
| `TestPre57GoldenListFormats` | `internal/api/golden_pinning_test.go:~102` | `bytes.Equal` of the re-encoded `engines` sub-map to the golden | same strip |
| `assertFormatsGolden58ModuloSanitize` | `internal/api/pre58_pinning_test.go:~150-210` | strips `tectonic`, tolerates only `sanitize` as a new operation | add `cad` to the stripped set for both `Engines` and `Operations["convert"]`; `operations` gains no new key in this phase, so the "outside the single permitted exception" check is unchanged |
| `TestEveryEngineHasAnEnqueueRoute` | `internal/api/handlers_test.go:3994` | derived from `AllEngines()`, fixtures per engine at `:~4037` | add a `fixture{"in.step", "stl", ...}` row and an `enqueuedCAD` field on `fakeQueue`; a valid STEP now flows to enqueue |
| `TestSweepRoutesEveryEngineConstant` | `internal/reconciler/reconciler_test.go:590` | derived; fails by name | add `cadCalls`/`cadCallIDs` and the `accessors` entry |
| `TestAllConvertQueuesCoversEveryEngine` | `internal/queue/queue_test.go:792` | derived | passes once `AllConvertQueues` includes `QueueCAD` |
| `TestEngineTimeoutsFromEnvCoversEveryEngine` | `cmd/api/enginetimeouts_test.go` | derived | add the entry in `engineTimeoutsFromEnv` |
| `TestEveryEngineHasEstimateSource`, `TestEveryEngineHasAnEnvExampleVar`, `TestEveryEngineHasADefaultCeilingAndTimeout`, `TestEstimateSourceMatchesEnvExampleProvenance` | `internal/api/estimate_test.go:338, 522, 610, 580` | derived | add `envExampleEngineVar[convert.EngineCAD] = "CAD_ENGINE_TIMEOUT"` (`:~367`). The estimate source is `unmeasured`, so `.env.example`'s own `CAD_ENGINE_TIMEOUT=` line must contain "placeholder" or "not measured" (`envExampleClaimsMeasured`, `:~395`); the derivation from 71 evidence is real but is not a measurement in the production worker, and phase 76 (CADW-04) replaces it |
| `TestREADMEEngineClassesMatchAllEngines` | `internal/convert/readme_sync_test.go:88` | derived | README line `:333` |
| `TestComposeReplicatesEveryEngineVarToEveryQueueClientService`, `TestEveryEngineVarIsDocumentedInEnvExample` | `internal/queue/compose_in02_test.go` | derived from `NewClient` env reads | 13 compose services, `.env.example` |
| `TestChartGraceOutlivesAsynqShutdown` | `internal/e2e/chart_grace_test.go:27` | table of workers | add `cadWorker` |
| `TestErrorCodesDocMatchesGoConstants` | `internal/errcode/doc_sync_test.go` | derived | async table rows |
| gitignore sync | `internal/repohygiene/gitignore_sync_test.go` | `cmd/` vs `.gitignore` block | `/cad-worker` |
| sandbox coverage | `internal/convert/sandboxcoverage_test.go` | AST scan | register `cad*.go` files and the class's engine list |
| `TestEngineSandboxUnavailableIsRetryableForEveryConfinedClass` + control | `internal/worker/engine_sandbox_retryable_test.go` | explicit cases | add `cad` to both |
| `TestCADTracerSTEPBothEndpoints`, `TestCADCreateRejections` | `internal/api/cad_sniff_test.go` | post target `png` | UNCHANGED and still true (`step -> png` stays unsupported until phase 74); this is why phase 72 chose `png` |

Goldens that must not change byte-for-byte: `pre58_createjob_rejections.golden.json` (existing 422/413 messages stay identical because the per-format message applies only to `cad`), `pre59_inspect_pdf.golden.json`, `pre60_sanitize_rejection.golden.json`, `internal/convert/testdata/cad-sniff-golden.tsv`. `pre57_formats.golden.json` is the one with a decision attached (row 4 above).

## Common Pitfalls

### Pitfall 1: Calling a Landlock denial a bad file
**What goes wrong:** with a too-tight or arch-wrong ruleset every job ends as "unreadable input" (terminal, no retry, client blamed).
**Why it happens:** the helper exits rc=2 with no text under denial (`evidence/71-07-landlock.txt:140`: `confined_rc=2,...,evidence=no_permission_text_in_log`), the same exit as `ReadFile != RetDone`. Missing `/opt/venv` gives rc=1, missing interpreter rc=6, missing helper dir rc=2 (`REMOVAL` rows).
**How to avoid:** preflight proves sufficiency on THIS image and arch before the queue is touched; keep the status JSON as the only source of "unreadable"; a run with no parsable verdict at all is an environment fault, not a client fault.
**Warning signs:** a burst of `cad_unreadable` on a fresh deployment or a new node pool.

### Pitfall 2: `no_solids` as an emptiness signal
**What goes wrong:** every IGES fails.
**Why:** OCP returns surfaces/faces for IGES, `solids:0` on all 12 IGES mesh rows of the ladder, with 65 392 to 2 429 885 triangles.
**How to avoid:** emptiness is `triangles == 0` or non-finite bbox in the OUTPUT STL, judged by Go.

### Pitfall 3: Counting entities with a line-anchored regex
**What goes wrong:** a file with entities hidden behind a preceding token (a `/* */` remark, same-line packing, CR-only line endings) undercounts and passes the cap. [ASSUMED: that OCCT's lexer accepts such forms; test it]
**How to avoid:** count statement terminators; require `semicolons >= regex_count` in a test; keep the 71 regex only as a diagnostic.

### Pitfall 4: NAUO edges are not instances
**What goes wrong:** a billion-laughs hierarchy passes the NAUO cap with a few dozen statements.
**How to avoid:** spike the real reader with a doubling hierarchy and a nested `MAPPED_ITEM`; decide on graph expansion in the guard from the result; rely on the 316 s stage deadline and the OOM classification as backstops (Section 6.3).

### Pitfall 5: A read that "succeeds" with dropped entities
**What goes wrong:** non-numeric coordinates produce `RetDone`, a partial shape and a valid-looking STL.
**How to avoid:** read the reader's load check list; measure how many corpus files have fail entries before choosing the strictness; add a differential fixture (same part, one corrupted coordinate) asserting a terminal failure, not a smaller STL.

### Pitfall 6: Scaling twice
**What goes wrong:** the helper multiplies a mesh OCP already scaled, giving 25.4x larger geometry.
**How to avoid:** Section 5.2; the bbox test on all three inch files fails on a double scale (`diag` about 21 400 mm for ctc_05).

### Pitfall 7: Unbounded engine output
**What goes wrong:** OCCT messages on a hostile file fill the worker's memory (tectonic measured 323 MB of stdout from 235 bytes of input).
**How to avoid:** a bounded `runCommandCore(ctx, limit, ...)` variant in `sandboxwrap.go` (not `exec.go`); the verdict line is last; parse the retained tail; judge the STL independently.

### Pitfall 8: Chart-level upload ceiling below the IGES ceiling
**What goes wrong:** in Helm deployments an IGES of 100-155 MiB is refused 413 at the transport with the generic "file exceeds size limit" message before the per-format check runs.
**Why:** `deploy/chart/octoconv/templates/configmap.yaml:30` `MAX_UPLOAD_BYTES: "104857600"` while compose uses `2147483648`. (The chart therefore also never admitted av's 2 GiB; that predates this phase.)
**How to avoid:** decide and record whether the chart's pre-parse cap rises (it only bounds the request body; the per-engine map still holds the other classes at 100 MiB), or accept a 100 MiB effective IGES ceiling in Helm and say so (Open Question 5).

### Pitfall 9: A timeout copied or derived below the ceiling
**What goes wrong:** terminal mesh timeout refuses legitimate files, or a copied neighbour value (PITFALLS Pitfall 7) is unrelated to this engine's cost.
**How to avoid:** Section 4.3 bracket-from-above derivation, re-derived in the same edit as any input; chart/compose/values comments carry the derivation (`TestChartGraceOutlivesAsynqShutdown` guards the grace chain).

### Pitfall 10: The golden rule that was already broken
**What goes wrong:** editing `pre57_formats.golden.json` again deepens a violation of 57 D-17.
**How to avoid:** Section 9 row 4: strip-and-assert in the tests, golden bytes untouched.

## Code Examples

### Guard counters (stdlib, Python 3.11, host-testable)

```python
import mmap

NAUO = b"NEXT_ASSEMBLY_USAGE_OCCURRENCE"
CHUNK = 1 << 24


def count_statements(path, limit):
    total = 0
    started = False
    with open(path, "rb") as fh:
        tail = b""
        while True:
            block = fh.read(CHUNK)
            if not block:
                return total
            if not started:
                joined = tail + block
                at = joined.find(b"DATA;")
                if at < 0:
                    tail = joined[-8:]
                    continue
                started = True
                block = joined[at:]
            total += block.count(b";")
            if total > limit:
                return total


def count_nauo(path, limit):
    with open(path, "rb") as fh, mmap.mmap(fh.fileno(), 0, access=mmap.ACCESS_READ) as mm:
        n = 0
        pos = mm.find(NAUO)
        while pos != -1:
            n += 1
            if n > limit:
                return n
            pos = mm.find(NAUO, pos + len(NAUO))
        return n
```
Source: derived from `scripts/cad-measure/counters.py` counting and the `;`-terminator measurement in Section 6.2. An empty file must be refused before `mmap` (zero-length `mmap` raises); the helper turns any exception into a `cannot_count` verdict.

### Binary STL content judgment (Go, no OCP)

```go
func judgeBinarySTL(path string) (stlFacts, error) {
	f, err := os.Open(path)
	if err != nil {
		return stlFacts{}, fmt.Errorf("open stl: %w", err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return stlFacts{}, fmt.Errorf("stat stl: %w", err)
	}
	var head [84]byte
	if _, err := io.ReadFull(f, head[:]); err != nil {
		return stlFacts{}, fmt.Errorf("stl header: %w", ErrCADEmptyResult)
	}
	n := int64(binary.LittleEndian.Uint32(head[80:84]))
	if n == 0 || st.Size() != 84+50*n {
		return stlFacts{}, fmt.Errorf("stl size %d does not match %d triangles: %w", st.Size(), n, ErrCADEmptyResult)
	}
	return scanSTLBounds(f, n)
}
```
Source: the 84 + 50 n identity is the sniffer's own rule (`internal/convert/cadsniff.go`, phase 72). `scanSTLBounds` reads in blocks and rejects NaN/Inf.

### Class descriptor and classifier (shape from `isTectonicTerminal`)

```go
var CADDescriptor = EngineDescriptor{
	Engine: convert.EngineCAD,
	Type:   queue.TypeCADConvert,
	OptsFromMap: func(m map[string]any) (any, error) {
		return convert.CADOptsFromMap(m)
	},
	IsTerminal:  isCADTerminal,
	FailureCode: cadFailureCode,
}

func isCADTerminal(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, convert.ErrEngineSandboxUnavailable) {
		return false
	}
	for _, sentinel := range []error{
		convert.ErrCADInputTooLarge, convert.ErrCADTooComplex, convert.ErrCADUnreadable,
		convert.ErrCADEmptyResult, convert.ErrCADMeshTimeout, convert.ErrCADEngineKilled,
	} {
		if errors.Is(err, sentinel) {
			return true
		}
	}
	return isTerminal(err)
}
```

### Per-format ceiling lookup used by create and inspect

```go
func (s *Server) sizeLimit(engine, format string) (int64, bool) {
	if byFormat, ok := s.maxFormatBytes[engine]; ok {
		if limit, ok := byFormat[format]; ok {
			return limit, true
		}
	}
	limit, ok := s.maxEngineBytes[engine]
	return limit, ok
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| FreeCAD engine (1.6-1.9 GiB) | OCP wheel `cadquery-ocp-novtk` | milestone research, ratified 71 | Mesh image has no VTK; about 250 MB venv |
| Per-engine byte ceiling | per-format ceiling within one engine | this phase | New map; create and inspect must read one rule |
| Timeout copied/placeholder | timeout derived from a measured series bracketing the ceiling | tectonic (64), probe (60) | Section 4.3 |
| Line-anchored regex counters (71 measurement) | terminator-based conservative counter in production | this phase | Pitfall 3 |

**Deprecated/outdated:**
- CONTEXT D-10's "cgroup memory bound following `cgroup.go`": no such pattern exists (Section 4.4).
- CONTEXT D-06's "71 did NOT prove scaling for nist_ctc_05": resolved by evidence already in the repo (Section 5.1).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | OCCT's STEP lexer accepts `/* */` remarks and same-line statement packing, so a line-anchored regex can be evaded | Pitfall 3, 6.2 | Low: the `;` counter is correct either way; only the justification changes |
| A2 | OCP expands shared assembly definitions or nested `MAPPED_ITEM` on read/mesh such that a doubling hierarchy explodes | 6.3, Pitfall 4 | High if true and un-spiked: the NAUO cap does not bound it; if false the cap is sufficient |
| A3 | OCP behaviour on a self-referencing STEP (hang, recursion crash, rejection) and on non-numeric coordinates (fail, partial, success) | 6.3-6.4, Pitfall 5 | High: decides the guard rules and the `cad_unreadable` mapping; Wave-0 spike resolves it |
| A4 | `Interface_Static.SetCVal_s("xstep.cascade.unit","MM")` is the exact OCP binding for the unit target | 5.2 | Low: default is already MM (cited); only the explicit pin may need a different spelling |
| A5 | IGES inch files are scaled by the same reader parameter | 5.2 | Medium: no inch IGES in corpus; test fixture needed |
| A6 | `libz1`, `libstdc++6`, `libgcc-s1` are present in `debian:bookworm-slim` + `python3` so `import OCP` needs only `libgl1 libx11-6` extra | 8 | Low: the import smoke test in the image build proves it |
| A7 | Guard time on 63 MiB STEP / 155 MiB IGES is under 30 s; the 60 s I/O allowance suffices | 4.3 | Medium: undersized guard deadline turns legitimate large files terminal; measure |
| A8 | A 1.5x margin is the right "visible margin" (tectonic/probe use 1.5, ocr 2.0) | 4.3 | Policy choice; changing it changes the timeout, grace and UniqueTTL together |
| A9 | Worker-death recovery is about 60-120 s via asynq lease expiry plus a one-minute recoverer | 2.4 | Low-medium: cited from source, not measured end to end |
| A10 | A single-process Python/OCP helper spawns no grandchildren, so tini is unnecessary | 8 | Low: a process listing during a mesh confirms |
| A11 | The escape-hatch name is `CAD_SANDBOX_DISABLED` | 2.2 | Low: naming convention only |
| A12 | IANA media types for `model/step`, `model/iges`, `model/stl` | 1a row 4 | Low: cosmetic content type; any consistent choice works |
| A13 | `ca-certificates` is absent from `debian:bookworm-slim` and needed for TLS S3 | 8 | Medium: if present, no extra package is needed |
| A14 | Applying the 71 render triangle cap (4 014 975) to convert output is acceptable | 4.2, 3.2 | Low-medium: planner may choose another output cap or none |
| A15 | A file at both caps at once costs about the sum of the two series (about 280 s) | 4.3 | Medium: unmeasured combination |

## Open Questions

1. **Strictness of the reader's load check.**
   - Known: non-numeric coordinates may not fail `ReadFile`; the check list may record them.
   - Unclear: whether any of the 87 corpus files carry check-list failures (a strict rule would then reject valid files).
   - Recommendation: Wave-0 spike over the corpus recording check-list counts per file; choose "any fail entry -> unreadable" only if the real corpus has zero.
2. **Combined worst case (both caps) and expansion bombs.**
   - Known: series measured separately; no expansion bomb measured.
   - Recommendation: spike a file near both caps and a doubling hierarchy; record in a phase 73 evidence file; if the combination exceeds 316 s, either lower a cap or raise the timeout and say which.
3. **IGES inch.**
   - Recommendation: add a deterministic inch IGES (write unit inch) to the corpus and the mm test; if OCP does not scale it, that is a real helper task, otherwise a regression test only.
4. **`ca-certificates` and the lock-file subset.**
   - Recommendation: ask the user to approve `ca-certificates` (the only package beyond the 71 apt list) and to choose verbatim `requirements-common.txt` (extra, unused ezdxf/pillow/fonttools) versus a hash-identical subset.
5. **Chart `MAX_UPLOAD_BYTES` (100 MiB) vs IGES 155 MiB.**
   - Recommendation: raise the chart value to compose parity (`2147483648`; the per-engine map keeps other classes at 100 MiB) in this atomic change, or record 100 MiB as the effective Helm IGES ceiling.
6. **Where the stage timeouts live.**
   - Recommendation: `CAD_ENGINE_TIMEOUT` stays the only IN-02-replicated var (13 compose services); `CAD_MESH_TIMEOUT` and `CAD_GUARD_TIMEOUT` are worker-only (like `TECTONIC_MAX_OUTPUT_BYTES`), validated at startup so `guard + mesh + 60 s <= CAD_ENGINE_TIMEOUT` and `CAD_ENGINE_TIMEOUT < RECONCILER_ACTIVE_STALE_AFTER` (900 s).
7. **Arch parametrisation option (a) vs (b)** (Section 7.2) and whether the guard stage uses `/usr/bin/python3` instead of the venv interpreter.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go (pinned) | build/test | yes | go1.26.4 via `GOTOOLCHAIN=go1.26.4` (local default go1.27.1) | none; always set `GOTOOLCHAIN` |
| python3 (host) | guard unit tests | yes | 3.14.8 (image is 3.11.2) | skip tests when absent; keep helper 3.11-compatible |
| OCP on host | real-reader tests | no (`ModuleNotFoundError: No module named 'OCP'`) | - | tests run in the image (docker) |
| docker | image build, Landlock measurement, live bombs, kill proof | present at `/usr/local/bin/docker`; NOT run in this research | - | mark `NOT RUN`, never a pass |
| helm 4.3.0 / yq 4.54.1 / jq | chart checks | yes | CI pins a different helm (ci-local warns) | - |
| Landlock | confinement proof | native arm64 only; emulated amd64 cannot establish it | - | amd64 runtime measurement deferred (CONTEXT) |
| Postgres 18 (compose) | DB-backed tests (`DATABASE_URL`) | via compose (72 verifier ran it) | - | tests skip without it |

**Missing dependencies with no fallback:** a native amd64 host for the amd64 Landlock set (deferred by CONTEXT).
**Missing dependencies with fallback:** OCP on the host (use the image); docker was deliberately not run here.

## Security Domain

`security_enforcement` is not set in `.planning/config.json` (absent = enabled).

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no change | existing API-key middleware on `/v1/jobs` |
| V3 Session Management | no | - |
| V4 Access Control | yes (job ownership) | existing ownership guard in `handleGetJob`; worker confinement is the new control |
| V5 Input Validation | yes | phase 72 sniffer, per-format ceilings, guard caps, strict `CADOptsFromMap` (no options), judge STL by content |
| V6 Cryptography | no | none introduced |
| V12 Files and Resources | yes | job-dir-only Landlock, env allowlist, no network engine, `USER nobody`, pinned hash-locked wheels |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Hostile STEP/IGES reading files or env (engine as reader) | Information disclosure | Landlock per stage (guard without OCP), `engineEnv` allowlist (`/proc/self/environ` holds `DATABASE_URL`, S3 keys) |
| Parser bombs: entity flood, assembly expansion, self-reference, huge coordinates | Denial of service | byte ceilings, guard caps counted conservatively, stage deadline 316 s, cgroup 4Gi + terminal OOM classification, finite-bbox check |
| STL with lying triangle count | Denial of service | phase 72 sniffer (`truncated_cad_file`) before any storage write; output judged by `84 + 50 n` |
| Engine output flood | Denial of service | bounded tail buffer, status line last |
| Sandbox silently absent | Elevation of privilege | `ResolveHelper` refusal naming `CAD_SANDBOX_DISABLED`; content-judged preflight with a negative run |
| Supply chain | Tampering | `--require-hashes`, digest-pinned base, lock-file hash assertion, `ca-certificates` checkpoint |
| Wrong units (silent 25.4x) | Integrity | bbox tests on three inch files and in the preflight |

## Sources

### Primary (HIGH confidence, read this session)
- `/Users/apaderin/dev/octoconv/.planning/phases/71-evaluation-gate/71-DECISION.md` (DECISION lines 43-62, memory/ceilings/caps/Landlock candidates, W17 reference)
- `.planning/phases/71-evaluation-gate/evidence/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt` (mesh rows and `LADDER_STOP`), `71-06-limits.txt` (`LIMIT`, `CEILING`, `CAP`), `71-03-corpus-inventory.txt` (`CNT`), `71-04-helpers.txt` (units rows, FINDING), `71-07-landlock.txt` (`LANDLOCK_MINIMAL`, `REMOVAL`, `EFFECT`, `TRACE`), `71-07-libtrace.txt` (`NEEDED`)
- `.planning/phases/72-engine-free-contract-foundations/72-VERIFICATION.md`; `.planning/phases/73-cad-class-convert-slice-confined/73-CONTEXT.md`
- `cmd/tectonic-worker/main.go`, `cmd/document-worker/main.go`; `internal/convert/{convert,converters,exec,cgroup,sandboxwrap,sandboxpreflight,pandocsandbox,sniff}.go`; `internal/sandbox/{helper,sandbox,sandbox_linux,execmode,limits_unix}.go`; `internal/queue/{queue,client}.go`; `internal/api/{api,handlers,estimate}.go`; `internal/worker/{descriptors,runner}.go`; `internal/reconciler/reconciler.go`; `cmd/api/main.go`; `docker-compose.yml`; chart templates and `values.yaml`; `scripts/cad-measure/{cad_mesh,counters}.py`, `scripts/conf05-crosscheck.sh`, `scripts/cad-sandbox-measure.sh`, `scripts/ci-helm-checks.sh`; `Dockerfile.cad-measure`; the sync tests named in Section 9
- `git show --stat e443c23` (phase 64 PR #16) and its diffs for `pre57_formats.golden.json`, `pre58_pinning_test.go`, `openapi.yaml`
- asynq v0.26.0 module source (`recoverer.go`, `server.go`) in the Go module cache
- Local computations this session: NIST CTC-05 and d2mi vertex-box vs OCP bbox; `;` vs entity count on 24 committed STEP files; timeout interpolations; lock-file sha256

### Secondary (MEDIUM confidence)
- [CITED: occt3d.com/dev/doc/overview/html/occt_user_guides__step.html] `xstep.cascade.unit` default MM
- `gsd-tools query package-legitimacy check --ecosystem pypi ...` (seam output; signals weak for PyPI)

### Tertiary (LOW confidence, flagged)
- OCCT lexer comment handling, assembly expansion, load-check semantics, IANA media types, base-image package presence: Assumptions Log

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH: lock files and Dockerfile read; versions are the approved pins.
- Architecture and checklist: HIGH: every anchor read; derived-from-`AllEngines()` tests enumerate omissions.
- Limits and timeout: HIGH for the evidence rows and arithmetic; MEDIUM for the choice of margin and the unmeasured combined case.
- Bomb behaviour of the real reader: LOW until the Wave-0 spike runs.
- Landlock production set: MEDIUM: candidates measured on the measurement image, not the production one.
- Pitfalls: HIGH where tied to a row or line cited; flagged where assumed.

**Research date:** 2026-10-05
**Valid until:** 2026-11-04 for repo anchors (the tree is moving: phase 72 branch and the 71 worktree are being edited concurrently); the 71 numbers are stable until a re-measurement.
