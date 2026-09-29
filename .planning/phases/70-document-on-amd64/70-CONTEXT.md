# Phase 70: Document on amd64 - Context

**Gathered:** 2026-09-29
**Status:** Ready for planning

<domain>
## Phase Boundary

The `document` class runs both of its engines under Landlock rulesets with the
machinery phases 67–69 built: `soffice` (LibreOffice, `internal/convert/libreoffice.go`)
and `verapdf` (Java, `internal/convert/verapdf.go`). Both files leave
`sandboxPendingFiles` in `internal/convert/sandboxcoverage_test.go`, which
then contains no entries.

The obstacle is the only one the roadmap names. `document-worker` is the only
service pinned to `platform: linux/amd64`, because `verapdf/cli:v1.30.2` is
published amd64-only, and an emulated amd64 process gets no Landlock
(`landlock_create_ruleset` fails; measured 2026-09-19). This phase **first
tests whether the pin is needed at all** (D-01) and only falls back to an
amd64 machine if it is.

Requirements: CONF-01, CONF-02, CONF-04, CONF-05, CONF-07. Once `document` is
confined, CONF-02/04/05 (which cover every worker/class) can be marked complete.

</domain>

<decisions>
## Implementation Decisions

### Getting a Landlock-capable run for `document`
- **D-01:** **Measure first whether the amd64 pin can be dropped.** Hypothesis
  (not yet measured): the only amd64-bound thing is the `verapdf/cli` build
  stage. `/opt/verapdf` is Java jars plus a launch script, and the runtime JRE is
  already Debian's `openjdk-17-jre-headless` (the jlink musl JRE was abandoned in
  23-01). If so, `FROM --platform=linux/amd64 verapdf/cli:v1.30.2 AS verapdf`
  plus a native final stage gives a native arm64 image. The same Dockerfile
  still builds natively on amd64. Research must first show there are no native
  `.so`/ELF files under `/opt/verapdf` (`find … -exec file`). —
  **Reversibility:** reversible: one `FROM` line and the compose `platform:` key.
- **D-02:** **The pin comes off only on differential evidence.** One corpus
  (conforming PDF/A-2b, non-conforming PDFs with known failing rules, a corrupt
  or unparseable PDF, and outputs of the real `soffice` PDF/A path) runs through
  veraPDF in the current amd64 image (emulated) and in the new native image.
  For every file the verdict (`isCompliant`) and the set of failed rule IDs must
  match. Any divergence goes into a table in the research, and a divergence the
  phase cannot explain blocks the unpin. The PDFA e2e must stay green on the
  native image. Do not predict equivalence; test it (project memory:
  "don't predict a parser, test against it").
- **D-03:** **If the unpin fails, stop and ask the user.** The phase does not
  pick an amd64 machine on its own. GitHub Actions, a cloud VM and a cluster
  node all need money, access or scarce Actions minutes, and each is an
  outward-facing choice. Planning puts a `checkpoint:decision` at that point,
  and no plan assumes a particular amd64 host.
- **D-04:** **If the unpin succeeds, rewrite CONF-07** in REQUIREMENTS.md
  (and ROADMAP.md's Phase 70 goal/criteria wording) to what was actually done
  and why: "the pin was the verapdf stage, the image is native, measured and
  gated on this host". Keep the old wording's reason visible (emulation denies
  Landlock) so the history reads straight. Also update the comments in
  `docker-compose.yml` (lines ~572, ~773, ~892, ~1059, ~1208) and
  `Dockerfile.document-worker` (~9–17) that call document-worker the
  "veraPDF-forced amd64 pin".
- **D-05:** **Production amd64 is handled the same way as the other seven
  classes.** They were all proven only on arm64. Grants are directory-level
  (`/lib`, `/usr/lib`, …), so multiarch subdirectories (`aarch64-linux-gnu`
  vs `x86_64-linux-gnu`) are covered. In production the real-engine preflight
  is the safety net, because a wrong grant set means the worker refuses to
  start. This phase adds **one** DEBT/follow-up entry covering all eight
  confined classes: "first amd64 deployment: check every worker's preflight
  log". The phase does not require an amd64 gate.

### Local development and the emulation refusal (criterion 3)
- **D-06:** **No quiet unconfined mode.** The escape hatch is named in the
  same style as the other classes (e.g. `DOCUMENT_SANDBOX_DISABLED`). It is
  forwarded as `${DOCUMENT_SANDBOX_DISABLED:-}` (empty) in `docker-compose.yml`,
  documented but unset in `.env.example`, and reachable through chart
  `extraEnv` (CONF-03 pattern). It is **not** defaulted on in compose. If the
  pin stays and a developer on an arm64 Mac gets the refusal, they set the
  variable deliberately.
- **D-07:** **One refusal message, no emulation detection code.** The worker
  uses the shared `sandbox.ResolveHelper` refusal (variable name + the reason
  string from `sandbox.Available`). There is no GOARCH-vs-uname check.
- **D-08:** **Criterion 3's "including emulation" is proven live.** The html/probe
  live-gate pattern gets one extra step: the document image built with
  `--platform linux/amd64` runs on the arm64 host and must refuse **before
  connecting to Postgres**, naming the variable. This is on top of the usual
  refuses-without-landlock and preflight-catches-restrict-self checks. If the
  pin stays (D-03 route), the regular native gate runs on whatever host the
  user supplies.

### /tmp and concurrency
- **D-09:** **Measure TMPDIR redirection first, as probe did in phase 69 (D-05
  there).** Research measures whether the per-engine environment moves every
  write out of `/tmp`: `TMPDIR=<job>/tmp` for soffice, and
  `-Djava.io.tmpdir=<job>/tmp` plus `-XX:-UsePerfData` (or equivalent) for the
  JVM, to stop `hsperfdata_*`. Also check where soffice writes besides its
  per-job `-env:UserInstallation` profile, including `HOME`/`~/.config` lookups.
  - If every write moves: `document` gets **no `/tmp` grant**, and
    `DOCUMENT_WORKER_CONCURRENCY` stays at its default of 2.
  - If not: apply phase 69's decision as is, i.e. concurrency 1 in
    `cmd/document-worker/main.go`, compose and the chart configmap;
    `sharedTmpGuard`-style refusal to start when confined concurrency > 1; and
    `sweepSharedTmp` before each engine run. Record the reason alongside.
  - Decide by measurement, not by reading the LibreOffice/JVM docs.

### veraPDF confinement
- **D-10:** **veraPDF gets its own measured ruleset, separate from soffice's.**
  The JRE (`/usr/lib/jvm/...`, resolved through the `java` alternatives
  symlinks) and `/opt/verapdf` are read-only. The job's PDF is readable. Writes
  go only to a per-job scratch dir, and only if measurement shows veraPDF needs
  one. No write grant otherwise.
- **D-11:** **veraPDF's preflight is judged by content, not exit status.**
  A positive run on a file inside the grant must produce a parseable XML
  report with a verdict for that file. A negative run points veraPDF at a file
  outside the grant and must produce no verdict for it. `requireOutsideGrants`
  must prove the outside path really is ungranted. veraPDF's exit code is
  ignored, since `verapdf.go` already parses stdout regardless of the exit
  status.

### Carried forward from phases 67–69 (not re-decided)
- **D-12:** Shared `engineExec` (one ruleset per task), `sandbox.ResolveHelper`
  fail-closed start (CONF-02), a real-engine preflight **before Postgres/queue**
  (CONF-04), retryable classification of sandbox faults
  (`ErrEngineSandboxUnavailable` checked first in `isDocumentTerminal`,
  `internal/worker/worker.go:237`), `sandboxcoverage_test.go` with the pending allow-list emptied,
  the grant sets pinned by test and cross-checked by
  `scripts/conf05-crosscheck.sh document` (CONF-05), measurement with
  `scripts/sandbox-paths-measure.sh` recorded in `.planning/research/SANDBOX-PATHS.md`,
  a live gate in `scripts/sandbox-liveproof.sh document` on images rebuilt
  from the tree, and class-closure inversions: at minimum a literal
  low-level exec restored in `libreoffice.go` and in `verapdf.go`, each shown
  to fail `TestConfinedClassesExecOnlyThroughTheSandbox`, then reverted.
- **D-13:** `internal/convert/exec.go`'s `runCommand` (Setpgid + group SIGKILL)
  stays byte-for-byte unchanged. This matters for soffice in particular, whose
  `soffice.bin` child is the reason process-group kill exists.

### Claude's Discretion
- The soffice preflight's conversion and marker. The rule is fixed: judge by
  the content of the output, with a per-run marker in raw output bytes, and no
  new packages in the production image (phase 69 D-06 precedent). Which input
  format, which target, and which field carries the marker are for research to
  prove on the real image.
- The exact name of the escape-hatch variable (following the
  `<CLASS>_SANDBOX_DISABLED` pattern).
- The composition of the D-02 corpus, as long as it covers conforming,
  non-conforming, corrupt and real-soffice-output PDFs.

### Folded Todos
- **`.planning/todos/pending/2026-09-06-other-engines-still-unconfined.md`**
  ("The sandbox exists now, and only one of nine engines uses it"). Phases
  67–69 confined seven classes; `document` is the last. The todo already
  listed "an arm64 build of veraPDF" as a way out, which D-01 tests. This phase
  closes the todo: move it to done when the phase ships.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Measured grant sets and method
- `.planning/research/SANDBOX-PATHS.md`: sets for the seven native classes,
  the method traps (§"Ловушки"), the shared machinery (§"Что уже внедрено").
  `document` has no section yet; this phase adds it.
- `.planning/todos/pending/2026-09-06-other-engines-still-unconfined.md`
  §"Где это можно делать, а где нельзя — замерено 2026-09-19": the emulation
  measurement table and the three ways out.

### Requirements and roadmap
- `.planning/REQUIREMENTS.md`: CONF-01, CONF-02, CONF-04, CONF-05, CONF-07
- `.planning/ROADMAP.md` §"Phase 70": goal and three success criteria (wording
  may change per D-04)

### Prior phase (the pattern to replicate)
- `.planning/phases/69-html-probe-the-tmp-decision/69-CONTEXT.md`: /tmp
  decision D-01..D-05, content-judged preflight D-06/D-07
- `.planning/phases/69-html-probe-the-tmp-decision/69-01-PLAN.md`,
  `69-02-PLAN.md`, `69-03-PLAN.md`: class confinement, preflight, and live-gate
  plans to mirror
- `.planning/phases/69-html-probe-the-tmp-decision/69-SECURITY.md`: threat
  register shape for a confined class
- `.planning/phases/68-four-straightforward-engine-classes/`: the per-class template

### Build and deploy
- `Dockerfile.document-worker`: verapdf stage (lines ~9–17), JRE path B
  (~31–48)
- `docker-compose.yml` document-worker service (~565–580, `platform:
  linux/amd64`) and the sibling comments citing that pin
- `deploy/chart/octoconv/templates/deployment-document-worker.yaml`,
  `configmap.yaml`, `values.yaml` (`extraEnv`)

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/convert/sandboxwrap.go` (`engineExec`, `forJobRW`), `internal/convert/sandboxpreflight.go`
  (`requireOutput`, `requireDenied`, `requireOutsideGrants`): the shared confinement
  and preflight machinery.
- `internal/convert/htmlsandbox.go` (`sweepSharedTmp`) and the `sharedTmpGuard`
  pattern: needed only if D-09 measurement says `/tmp` stays.
- `internal/convert/probesandbox.go`: the latest class file, with per-engine
  env (`engineEnv(jobDir)`) and multi-engine preflight (the closest analogue
  to soffice + verapdf).
- `scripts/sandbox-paths-measure.sh`, `scripts/conf05-crosscheck.sh`,
  `scripts/sandbox-liveproof.sh`: measurement, cross-check, live gate.

### Established Patterns
- `libreoffice.go` already isolates the LibreOffice profile per job
  (`-env:UserInstallation=file://<workDir>/lo-profile`) and treats "exit 0 with
  no output" as failure (it knows soffice's documented failure mode).
- `verapdf.go` parses veraPDF's stdout regardless of exit code, which matches
  D-11's "judge by content".
- Each confined class drops out of `sandboxPendingFiles` and is added to
  `sandboxClassFileEngines` with its literal engine names.

### Integration Points
- `cmd/document-worker/main.go`: `ResolveHelper` start, preflight before
  Postgres, `DOCUMENT_WORKER_CONCURRENCY` (default 2).
- `internal/worker/worker.go:237` (`isDocumentTerminal`): must check
  `ErrEngineSandboxUnavailable` first.
- `internal/convert/sandboxcoverage_test.go`: `sandboxPendingFiles` becomes empty.

</code_context>

<specifics>
## Specific Ideas

- The user prefers measurement over reasoning at every fork in this phase:
  the unpin (D-02), `/tmp` (D-09) and veraPDF's needs (D-10) are all decided
  by what the real image does.
- Choosing an amd64 host is explicitly the user's call (D-03).

</specifics>

<deferred>
## Deferred Ideas

- **amd64 production confirmation for all eight confined classes:** one
  DEBT/follow-up entry (D-05), checked on the first amd64 deployment; not a
  gate for this phase.
- In-process parsing of untrusted input in Go workers (already deferred in
  phase 69 D-09); still out of scope.

### Reviewed Todos (not folded)
None. The single matching todo was folded.

</deferred>

---

*Phase: 70-document-on-amd64*
*Context gathered: 2026-09-29*
