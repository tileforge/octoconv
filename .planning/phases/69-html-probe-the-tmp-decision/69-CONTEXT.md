# Phase 69: HTML, Probe & the /tmp Decision - Context

**Gathered:** 2026-09-25
**Status:** Ready for planning

<domain>
## Phase Boundary

The `html` class (chromium-headless-shell) and the `probe` class (every external
binary the sanitize path execs: `pdfinfo`, `gs` ×2, `unzip`, `file`) run their
engines under a Landlock ruleset, with the same machinery phase 68 built for
pandoc/ocr/av/audio. The phase **starts with** the recorded `/tmp` decision
(CONF-06), and that decision shapes the grant sets.

Requirements: CONF-01, CONF-02, CONF-04, CONF-05, CONF-06 (html + probe share).
`document` (LibreOffice) is phase 70 and is out of scope here.

</domain>

<decisions>
## Implementation Decisions

### /tmp strategy (CONF-06): the decision this phase starts with
- **D-01:** **One task per container.** html and probe workers run at most one
  engine task at a time, so no task ever shares its container's `/tmp` with a
  concurrent task. `HTML_WORKER_CONCURRENCY` default goes 2 → 1 in
  `cmd/chromium-worker/main.go`, `docker-compose.yml` and
  `deploy/chart/octoconv/templates/configmap.yaml`. Probe is already 1. html
  throughput scales by replicas, not by in-process concurrency. The halved
  per-pod throughput is accepted. — **Reversibility:** reversible — a default
  and a startup guard, no data or contract involved.
- **D-02:** **Why not the alternatives, as recorded with the decision.**
  (a) Per-task private `/tmp` via a mount namespace is **measured
  impossible** under Docker's default seccomp:
  `unshare -Urm` → `Operation not permitted`, as root and as uid 65534
  (Docker 29.4.0, `debian:bookworm-slim`, 2026-09-25). It would need a custom
  seccomp profile in compose and the chart, which adds kernel attack surface to
  the very containers we are confining. (b) Landlock allows or denies paths;
  it cannot remap `/tmp`. (c) `TMPDIR` does not move chromium's shared memory
  (measured in SANDBOX-PATHS.md). (d) A shared `/tmp` at concurrency 2 was
  rejected: it leaves a known cross-task read path.
- **D-03:** **Hold the invariant by refusing to start, not with a comment.** A
  confined html or probe worker (sandbox enabled) whose configured concurrency
  is > 1 **refuses to start** and says why, in the same style as CONF-02
  (name the variable, name the reason). This applies whenever the class's grant
  set contains the shared `/tmp`. A test holds it.
- **D-04:** **Sweep `/tmp` before every task**, before the engine runs. This
  covers leftovers from a previous task that was SIGKILLed on timeout (process
  group kill leaves `gs_*` files and chromium shm files behind). It is safe
  only because of D-01/D-03: there is no concurrent task whose files could be
  swept out from under it.
- **D-05:** **Probe may drop `/tmp` entirely** if research **measures** that
  ghostscript honours `TMPDIR=<job>/tmp` for its `gs_*` scratch files (not yet
  measured; SANDBOX-PATHS.md only records that gs writes to `/tmp`). If it
  does, probe's grant set loses `/tmp`, and D-03/D-04 need not apply to probe.
  If it does not, probe keeps `/tmp` + concurrency 1 + sweep, the same as html.
  Decide by measurement, not by reading ghostscript docs.

### html effectiveness is judged by CONTENT (criterion 2, CONF-04)
- **D-06:** Under a ruleset, chromium renders a **blank 652-byte PDF** when a read is
  denied instead of failing (measured). "Output exists" therefore proves nothing.
  The preflight puts a per-run marker into a PDF field that chromium writes
  **uncompressed**, either `<title>` → PDF `/Title` or `<a href>` → URI
  annotation, and reuses the existing `requireOutput` / `requireDenied`
  byte-marker judgments in `internal/convert/sandboxpreflight.go`. No new
  packages go into the production image.
- **D-07:** Research must **prove on the real `octoconv-chromium-worker` image**
  that the chosen field carries the marker verbatim in raw PDF bytes: present
  for the inside file, absent for the denied outside file. If neither field
  works, fall back to adding `poppler-utils` (`pdftotext`) to the chromium image
  and record why.

### chromium launch path (criterion 3)
- **D-08:** Keep exec'ing through the wrapper `/usr/bin/chromium-headless-shell`
  (`#!/bin/sh -e` + `exec /usr/lib/chromium/chromium-headless-shell "$@"`, no
  env or flags; verified in the image on 2026-09-25). The grant set includes the
  wrapper, its interpreter `/usr/bin/dash`, and the real binary under
  `/usr/lib/chromium`. That matches the measured set and satisfies ROADMAP
  criterion 3 literally. The pinned-set test names all three, so a package
  update that changes the wrapper shows up as a failure. argv in
  `internal/convert/chromium.go` is unchanged.

### probe boundary
- **D-09:** Probe confinement covers **only the external engine execs**
  (`pdfinfo`, `gs`, `unzip`, `file`) through `engineExec`, like phase 68's
  classes. The in-process parsing of untrusted input (pdfcpu and the Go-side
  OOXML/zip/image handling in `internal/convert/sanitize*.go`,
  `internal/inspect`, `internal/api`) is **not** confined in this phase. It is
  recorded in Deferred with its reason: CONF-01 is about engines, and Landlock
  on a multi-threaded Go worker that also needs TLS, S3 and Postgres is a
  separate design.
- **D-10:** **Re-measure the probe class from the code's exec sites, not from
  the document's list.** `sanitizeverify.go` execs `file --mime-type`
  (lines ~850, ~1327), which SANDBOX-PATHS.md's probe set
  (`/usr/bin/{gs,pdfinfo,unzip}`) omits. Research enumerates every
  `runCommand(` in the probe path, re-runs `scripts/sandbox-paths-measure.sh`
  over all of them (including `file`'s magic database), and corrects
  SANDBOX-PATHS.md. `scripts/conf05-crosscheck.sh` then holds code and document
  in agreement.

### Carried forward from phases 67–68 (not re-decided)
- **D-11:** Shared `engineExec` (one ruleset per task, applied once), the
  `sandbox.ResolveHelper` fail-closed start (CONF-02) naming
  `HTML_SANDBOX_DISABLED` / `PROBE_SANDBOX_DISABLED`, both escape hatches
  reachable from `docker-compose.yml` and chart `extraEnv` (CONF-03 pattern),
  a real-engine preflight **before Postgres/queue**, retryable classification of
  sandbox faults, `sandboxcoverage_test.go` extended so html/probe leave the
  "future phase" allow-list, `conf05-crosscheck.sh html probe`, and
  `scripts/sandbox-liveproof.sh html probe` on rebuilt images.
- **D-12:** `internal/convert/exec.go`'s `runCommand` (Setpgid + group SIGKILL)
  stays byte-for-byte unchanged.

### Claude's Discretion
- Exact sweep implementation (who sweeps, and how a sweep failure is
  classified: retryable vs fatal).
- The exact wording of the D-03 refusal message.
- Which of `/Title` vs URI annotation to use for D-06, whichever the research
  proves works.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Measured grant sets and method
- `.planning/research/SANDBOX-PATHS.md` §"html (chromium)" (lines ~162–209):
  measured set, the `/tmp` shm finding, and the 652-byte blank-PDF effectiveness table
- `.planning/research/SANDBOX-PATHS.md` §"probe (gs + pdfinfo + unzip)" (lines ~709–722):
  measured set. **Incomplete**, because it omits `file` (see D-10)
- `.planning/research/SANDBOX-PATHS.md` §"Ловушки" and §"Что уже внедрено": method traps and the shared machinery

### Requirements and roadmap
- `.planning/REQUIREMENTS.md`: CONF-01, CONF-02, CONF-04, CONF-05, CONF-06
- `.planning/ROADMAP.md` §"Phase 69": goal and three success criteria

### Prior phase (the pattern to replicate)
- `.planning/phases/68-four-straightforward-engine-classes/68-RESEARCH.md`
- `.planning/phases/68-four-straightforward-engine-classes/68-01-PLAN.md`: pandoc end-to-end, the template for a class
- `.planning/phases/68-four-straightforward-engine-classes/68-06-PLAN.md`: coverage test, compose escape hatches, full live proof
- `.planning/phases/68-four-straightforward-engine-classes/68-VERIFICATION.md`

### Background
- `.planning/todos/pending/2026-09-06-other-engines-still-unconfined.md`: origin of the milestone and where Landlock can be measured (reviewed, not folded)

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/sandbox/` (`ResolveHelper`, `Apply`, `Available`, `Paths`): engine-agnostic Landlock helper; Landlock only, no namespaces
- `internal/convert/sandboxpreflight.go`: `newPreflightWorkspace`, `place`, `requireOutput`, `requireDenied`, `preflightMarker` (byte-marker judgments that D-06 reuses)
- `internal/convert/sandboxcoverage_test.go`: fails the build if an engine exec bypasses `engineExec`, unless the file is classified to a future phase
- `scripts/sandbox-paths-measure.sh`, `scripts/conf05-crosscheck.sh`, `scripts/sandbox-liveproof.sh`

### Established Patterns
- Phase 68 per-class shape: converter via `engineExec` → class preflight → worker `main.go` preflights before Postgres → compose + chart escape hatch → coverage test → live gate
- Exec sites to confine: `internal/convert/chromium.go:187` (`runCommand(ctx, "chromium-headless-shell", …)`); `internal/convert/sanitizepdf.go:221,264,281` (`pdfinfo`, `gs`, `gs`); `internal/convert/sanitizeverify.go:791,841,850,1327` (`unzip`, `unzip`, `file`, `file`)

### Integration Points
- `cmd/chromium-worker/main.go:91`: `envInt("HTML_WORKER_CONCURRENCY", 2)` → 1, plus the D-03 guard
- `cmd/probe-worker/main.go:167`: `envInt("PROBE_WORKER_CONCURRENCY", 1)`, plus the D-03 guard if probe keeps `/tmp`
- `docker-compose.yml`: `HTML_WORKER_CONCURRENCY: "2"` (~line 690); chromium-worker `shm_size: "256m"`
- `deploy/chart/octoconv/templates/configmap.yaml:48`: `HTML_WORKER_CONCURRENCY: "2"`; `:158` `PROBE_WORKER_CONCURRENCY: "1"`
- `deploy/chart/octoconv/values.yaml` `chromiumWorker` (`shmSize: "256Mi"`, replicas 1)

</code_context>

<specifics>
## Specific Ideas

- The `/tmp` decision and its reasons (D-01…D-05) must also be written into
  SANDBOX-PATHS.md next to the html/probe sections, so that CONF-06's
  "recorded with a reason" lives where the grant sets live.
- Measurement over reasoning, throughout: the seccomp EPERM, the wrapper's
  contents, and gs's `TMPDIR` behaviour are each settled by running the thing.

</specifics>

<deferred>
## Deferred Ideas

- **Confining in-process parsing of untrusted input** (pdfcpu, Go-side
  OOXML/zip/image parsing in probe, inspect, api). This is not an engine exec,
  so it falls outside CONF-01. Landlock on a whole multi-threaded Go worker
  (`AllThreadsSyscall`, and grants for TLS roots, S3 and Postgres sockets) is a
  separate design and a separate phase.
- **Per-task private `/tmp` via user+mount namespaces.** Blocked by default
  seccomp (measured). Revisit only if the deployment ever ships a custom
  seccomp profile for other reasons.

### Reviewed Todos (not folded)
- `2026-09-06-other-engines-still-unconfined.md`: spans the whole milestone,
  including `document` (phase 70). Close it after phase 70, not here.

</deferred>

---

*Phase: 69-html-probe-the-tmp-decision*
*Context gathered: 2026-09-25*
