# Phase 70: Document on amd64 - Research

**Researched:** 2026-09-29
**Domain:** Landlock filesystem confinement for LibreOffice (`soffice`) + veraPDF (`verapdf`), on the one engine class whose image is pinned `platform: linux/amd64`
**Confidence:** HIGH for D-01 (unpin feasibility) and D-09 (TMPDIR/JVM redirection); HIGH for D-10/D-11 (veraPDF grant set, live-proven); MEDIUM for D-05/D-06 (production amd64 deployment) — all measured live on this host during this session, commands and raw output recorded below.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01:** Measure first whether the amd64 pin can be dropped. Hypothesis (not yet measured, at CONTEXT time): the only amd64-bound thing is the `verapdf/cli` build stage. `/opt/verapdf` is Java jars plus a launch script, and the runtime JRE is already Debian's `openjdk-17-jre-headless`. If so, `FROM --platform=linux/amd64 verapdf/cli:v1.30.2 AS verapdf` plus a native final stage gives a native arm64 image. The same Dockerfile still builds natively on amd64. Research must first show there are no native `.so`/ELF files under `/opt/verapdf`. — Reversibility: reversible: one `FROM` line and the compose `platform:` key.
- **D-02:** The pin comes off only on differential evidence. One corpus (conforming PDF/A-2b, non-conforming PDFs with known failing rules, a corrupt or unparseable PDF, and outputs of the real `soffice` PDF/A path) runs through veraPDF in the current amd64 image (emulated) and in the new native image. For every file the verdict (`isCompliant`) and the set of failed rule IDs must match. Any divergence goes into a table in the research, and a divergence the phase cannot explain blocks the unpin. The PDFA e2e must stay green on the native image. Do not predict equivalence; test it.
- **D-03:** If the unpin fails, stop and ask the user. The phase does not pick an amd64 machine on its own. Planning puts a `checkpoint:decision` at that point, and no plan assumes a particular amd64 host.
- **D-04:** If the unpin succeeds, rewrite CONF-07 in REQUIREMENTS.md (and ROADMAP.md's Phase 70 goal/criteria wording) to what was actually done and why. Keep the old wording's reason visible. Also update the comments in `docker-compose.yml` (lines ~572, ~773, ~892, ~1059, ~1208) and `Dockerfile.document-worker` (~9–17) that call document-worker the "veraPDF-forced amd64 pin".
- **D-05:** Production amd64 is handled the same way as the other seven classes. Grants are directory-level, so multiarch subdirectories are covered. In production the real-engine preflight is the safety net. This phase adds ONE DEBT/follow-up entry covering all eight confined classes: "first amd64 deployment: check every worker's preflight log". The phase does not require an amd64 gate.
- **D-06:** No quiet unconfined mode. The escape hatch is named in the same style as the other classes (e.g. `DOCUMENT_SANDBOX_DISABLED`). Forwarded as `${DOCUMENT_SANDBOX_DISABLED:-}` in `docker-compose.yml`, documented but unset in `.env.example`, reachable through chart `extraEnv`. Not defaulted on in compose.
- **D-07:** One refusal message, no emulation detection code. The worker uses the shared `sandbox.ResolveHelper` refusal (variable name + the reason string from `sandbox.Available`). No GOARCH-vs-uname check.
- **D-08:** Criterion 3's "including emulation" is proven live. The html/probe live-gate pattern gets one extra step: the document image built with `--platform linux/amd64` runs on the arm64 host and must refuse before connecting to Postgres, naming the variable. If the pin stays (D-03 route), the regular native gate runs on whatever host the user supplies.
- **D-09:** Measure TMPDIR redirection first, as probe did in phase 69 (D-05 there). Research measures whether the per-engine environment moves every write out of `/tmp`: `TMPDIR=<job>/tmp` for soffice, and `-Djava.io.tmpdir=<job>/tmp` plus `-XX:-UsePerfData` (or equivalent) for the JVM, to stop `hsperfdata_*`. Also check where soffice writes besides its per-job `-env:UserInstallation` profile, including `HOME`/`~/.config` lookups.
  - If every write moves: `document` gets **no `/tmp` grant**, and `DOCUMENT_WORKER_CONCURRENCY` stays at its default of 2.
  - If not: apply phase 69's decision as is, i.e. concurrency 1 in `cmd/document-worker/main.go`, compose and the chart configmap; `sharedTmpGuard`-style refusal to start when confined concurrency > 1; and `sweepSharedTmp` before each engine run. Record the reason alongside.
  - Decide by measurement, not by reading the LibreOffice/JVM docs.
- **D-10:** veraPDF gets its own measured ruleset, separate from soffice's. The JRE and `/opt/verapdf` are read-only. The job's PDF is readable. Writes go only to a per-job scratch dir, and only if measurement shows veraPDF needs one. No write grant otherwise.
- **D-11:** veraPDF's preflight is judged by content, not exit status. A positive run on a file inside the grant must produce a parseable XML report with a verdict for that file. A negative run points veraPDF at a file outside the grant and must produce no verdict for it. `requireOutsideGrants` must prove the outside path really is ungranted. veraPDF's exit code is ignored.
- **D-12 (carried forward):** Shared `engineExec`, `sandbox.ResolveHelper` fail-closed start, a real-engine preflight before Postgres/queue, retryable classification of sandbox faults (`ErrEngineSandboxUnavailable` checked first in `isDocumentTerminal`, `internal/worker/worker.go:237`), `sandboxcoverage_test.go` with the pending allow-list emptied, grant sets pinned by test and cross-checked by `scripts/conf05-crosscheck.sh document`, measurement recorded in `.planning/research/SANDBOX-PATHS.md`, a live gate in `scripts/sandbox-liveproof.sh document`, and class-closure inversions for `libreoffice.go`/`verapdf.go`.
- **D-13 (carried forward):** `internal/convert/exec.go`'s `runCommand` (Setpgid + group SIGKILL) stays byte-for-byte unchanged.

### Claude's Discretion

- The soffice preflight's conversion and marker. Judge by the content of the output, with a per-run marker in raw output bytes, no new packages in the production image. Which input format, which target, and which field carries the marker are for research to prove on the real image.
- The exact name of the escape-hatch variable (following the `<CLASS>_SANDBOX_DISABLED` pattern).
- The composition of the D-02 corpus, as long as it covers conforming, non-conforming, corrupt and real-soffice-output PDFs.

### Deferred Ideas (OUT OF SCOPE)

- amd64 production confirmation for all eight confined classes: one DEBT/follow-up entry (D-05), checked on the first amd64 deployment; not a gate for this phase.
- In-process parsing of untrusted input in Go workers; still out of scope.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| CONF-01 | Each of the confined engine classes runs its engine under a Landlock ruleset; the engine cannot read a file outside the job directory and its granted paths | veraPDF's grant set is **measured and live-proven** on this host (native arm64 build). LibreOffice's grant set is **measured** (trace + partial verify); full sufficiency proof is blocked by a newly discovered structural gap in `internal/sandbox` — see "Blocking Finding" below. |
| CONF-02 | A worker that needs confinement and cannot get it refuses to start, naming the escape-hatch variable | `sandbox.ResolveHelper`/`RunExecModeIfRequested` pattern already exists and is reused verbatim (chromium-worker's `main.go` is the template); `isDocumentTerminal` is currently missing the `ErrEngineSandboxUnavailable` guard every other confined class already has — a gap this phase must close |
| CONF-04 | Each worker preflights on the real engine before taking a task, judged by output content not exit code | `HTMLPreflight`/`ProbePreflight` are the templates; this research proves (live) that veraPDF's exit code is unreliable in exactly the documented way (exit 7 on a genuine denial, with a parseable XML report) and recommends soffice content-judging, pending resolution of the blocking finding |
| CONF-05 | The granted path set for each class matches `SANDBOX-PATHS.md` and is held by a test, not a comment | This research adds the "document" section data to `SANDBOX-PATHS.md`'s shape (see below); `scripts/conf05-crosscheck.sh` and `scripts/sandbox-liveproof.sh` need a `document` case added, following the `html`/`probe` pattern verbatim |
| CONF-07 | `document` runs its engines under a ruleset; measurement done on an amd64 node because of the pin | **This research's central finding: the amd64 pin is very likely unnecessary.** D-01/D-02 both passed with zero divergence, measured live in this session — see below. If the planner accepts the unpin, CONF-07's own wording is rewritten per D-04, and the whole rest of this phase's grant-set work happens **natively on this arm64 host**, with a real live gate, not merely a paper design for a machine nobody has provisioned. |
</phase_requirements>

## Summary

This session performed the measurement D-01/D-02 call for, not a paper argument: `docker pull --platform linux/amd64 verapdf/cli:v1.30.2` was inspected byte-for-byte — zero ELF/`.so`/`.dylib`/`.dll` files exist under `/opt/verapdf` (only Java `.class` files inside two jars, a POSIX shell launch script, and a Java-serialized `.installationinformation` file). A scratch Dockerfile pinning **only** the `verapdf` build stage to `--platform=linux/amd64` (leaving the runtime stage unpinned) built successfully on this arm64 host, produced an `arm64/linux` image, and `verapdf --version` / `java -version` both ran natively using Debian's own `openjdk-17-jre-headless` — exactly D-01's hypothesis. A four-file differential corpus (a real `soffice`-produced PDF/A-2b compliant export, a real `soffice`-produced non-conformant plain PDF export, the repo's existing `verapdf_noncompliant.pdf` fixture, and a deliberately corrupt/unparseable PDF) was run through **both** the new native-arm64 image and a freshly built `--platform linux/amd64` emulated image (matching the currently-committed `Dockerfile.document-worker` byte for byte). Every one of the four files produced an **identical** `isCompliant` verdict and an **identical** set of failing rule clause/testNumber pairs on both images; the corrupt file produced the byte-identical `failedToParse=1` exception message on both. Zero divergence, D-02 satisfied.

`D-09`'s TMPDIR/JVM question also got a definitive, measured answer, but a mixed one. `TMPDIR=<job>/tmp` fully redirects every scratch write soffice makes during a normal conversion (confirmed via `strace`: zero opens under bare `/tmp` when TMPDIR is set, versus five distinct scratch paths — including an NSS certificate database — under bare `/tmp` when it is not). `-Djava.io.tmpdir=<job>/tmp -XX:-UsePerfData` does the same for veraPDF's JVM (zero bare-`/tmp` opens, config/log files land correctly inside the redirected directory, `hsperfdata_*` does not appear). **However**, `soffice.bin` unconditionally attempts to `bind()` a `AF_UNIX` socket at a hardcoded `/tmp/OSL_PIPE_0_SingleOfficeIPC_<hash>` path during startup — this is LibreOffice's single-instance IPC mechanism, and it is **not** redirected by `TMPDIR`, is **not** disabled by `--nolockcheck` (that flag only skips the *detection* logic, not the bind attempt itself), and fails with `EACCES` under every Landlock ruleset tested in this session — including one that granted `/tmp` read-write. The reason is structural, not a missing grant: `internal/sandbox/sandbox_linux.go`'s `accessReadWrite` constant does not include `LANDLOCK_ACCESS_FS_MAKE_SOCK`, and **no class in this codebase currently grants that right to any path**. This is squarely D-09's "if not" branch (apply phase 69's html-style decision: shared `/tmp`, concurrency 1, `sharedTmpGuard`, sweep) — but getting there requires a small, shared change to `internal/sandbox` itself, not merely a document-specific grant-list edit, because `accessReadWrite` is common to every already-confined class. This is flagged as the single most important open item for the planner.

veraPDF's own grant set was **fully live-proven** on this host — not merely traced: sufficiency (a real PDF/A-2b file inside the job directory validates as `isCompliant="true"`) and effectiveness (the identical bytes placed outside the job directory produce `Permission denied` inside veraPDF's own parse-failure path, `failedJobs="1"`, zero verdict) both passed against a real Landlock ruleset built with `scripts/cmd/sandbox-probe`. veraPDF needed no `/tmp` grant at all once its JVM options redirected its own scratch use — D-10's hypothesis is confirmed, and D-11's "exit code is unreliable, content isn't" finding is independently reproduced (`exit=7` on both the compliant success path and the effectiveness-denial path; only the XML content tells them apart).

**Primary recommendation:** Take the unpin (D-01/D-02 fully support it — remove `platform: linux/amd64` from `docker-compose.yml`'s `document-worker` service and pin only the `verapdf` build stage in the Dockerfile). This turns the whole rest of the phase into ordinary native-arm64 measurement work, identical in kind to phases 68/69, with a real live gate on this host rather than a paper design. Before writing the grant-set plan for `soffice`, resolve the `MAKE_SOCK` gap explicitly — the two live options found this session are (a) add `unix.LANDLOCK_ACCESS_FS_MAKE_SOCK` to `internal/sandbox`'s `accessReadWrite` and grant `document` a shared `/tmp` (mirroring html's D-01..D-04 concurrency-1+sweep shape exactly), or (b) further research into disabling/relocating `soffice.bin`'s `OSL_PIPE_0_SingleOfficeIPC` bind entirely (neither `--nolockcheck` nor `TMPDIR` worked in this session; `SAL_ENABLE_FILE_LOCKING` is hardcoded to `1` by the wrapper script itself and cannot be overridden via environment). Given the codebase's own "measure, don't predict" discipline and this session's exhausted time budget on live avoidance attempts, option (a) is the recommended default — it is the smaller, better-understood change (one `const` addition, reviewed once, applying identically to every class's own job-directory grant, none of which currently attempt a socket bind).

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Filesystem confinement (Landlock ruleset) | Worker process (document-worker) | `internal/sandbox` (shared library) | The ruleset is applied by the process that then execs the engine; `internal/sandbox` owns the mechanism, `internal/convert/documentsandbox.go` (new) owns the class-specific grant set |
| LibreOffice conversion | Worker process (document-worker) | — | `internal/convert/libreoffice.go`, unchanged conversion logic; only the exec path changes to route through `engineExec` |
| PDF/A validation (veraPDF) | Worker process (document-worker) | — | `internal/convert/verapdf.go`, unchanged parsing logic; only the exec path changes |
| Image build / platform pin | Build tier (Dockerfile, docker-compose.yml, Helm chart) | — | The amd64-vs-native decision is a build-time/deploy-time concern, not a runtime one; D-01/D-02 findings are actionable here |
| Startup refusal (CONF-02) | Worker process entry point (`cmd/document-worker/main.go`) | `internal/sandbox` (`ResolveHelper`) | Mirrors every other confined worker's `main.go` shape exactly |
| Terminal-vs-retryable classification | API/Backend tier (`internal/worker/worker.go`) | — | `isDocumentTerminal` is pure classification logic, no I/O; the missing `ErrEngineSandboxUnavailable` check belongs here |

## Package Legitimacy Audit

Not applicable. This phase adds no new external dependencies: LibreOffice (`libreoffice-writer-nogui`, `libreoffice-calc-nogui`, `libreoffice-impress-nogui`), fonts, `tini`, and `verapdf/cli:v1.30.2` are all already pinned in the committed `Dockerfile.document-worker`. The only build-time change under consideration is **which platform** the `verapdf` build stage is fetched for (`--platform=linux/amd64` on that one stage only, instead of pinning the whole image) — not a new package.

## D-01: Is `/opt/verapdf` native-code-free? — MEASURED, CONFIRMED

**Commands run (this session, this host, arm64):**

```bash
docker pull --platform linux/amd64 verapdf/cli:v1.30.2
docker run --rm --platform linux/amd64 --entrypoint find verapdf/cli:v1.30.2 /opt/verapdf -type f
```

**Result** — every file under `/opt/verapdf`:

```
/opt/verapdf/.installationinformation
/opt/verapdf/Uninstaller/install.log
/opt/verapdf/Uninstaller/uninstaller.jar
/opt/verapdf/bin/cli-1.30.2.jar
/opt/verapdf/plugins/README.txt
/opt/verapdf/verapdf
```

Byte-level magic check on each (`head -c 4 | od -An -tx1`):

| File | Magic bytes | Identity |
|---|---|---|
| `.installationinformation` | `ac ed 00 05` | Java object-serialization stream |
| `Uninstaller/install.log` | `2f 6f 70 74` (`/opt`) | plain text |
| `Uninstaller/uninstaller.jar` | `50 4b 03 04` (`PK\x03\x04`) | ZIP (jar) |
| `bin/cli-1.30.2.jar` | `50 4b 03 04` | ZIP (jar) |
| `plugins/README.txt` | `3d 3d 3d 3d` | plain text |
| `verapdf` | `23 21 2f 62` (`#!/b`) | POSIX shell script |

**Zero ELF/`.so`/`.dll`/`.dylib` files anywhere.** `unzip -l /opt/verapdf/bin/cli-1.30.2.jar | grep -icE '\.so$|\.dll$|\.dylib$'` → `0`. The jar contains only Java `.class` files (Saxon XSLT library, veraPDF's own classes). `[VERIFIED: docker run --platform linux/amd64 verapdf/cli:v1.30.2, this session]`

**Native build test.** A scratch Dockerfile (`Dockerfile.document-worker.nativetest`, not committed) identical to the production `Dockerfile.document-worker` except the `verapdf` stage alone is pinned `FROM --platform=linux/amd64 verapdf/cli:v1.30.2 AS verapdf` and the final runtime stage carries **no** platform override:

```bash
docker build -f Dockerfile.document-worker.nativetest -t octoconv-document-worker:nativetest .
docker image inspect octoconv-document-worker:nativetest --format '{{.Architecture}}/{{.Os}}'
# -> arm64/linux
docker run --rm --entrypoint verapdf octoconv-document-worker:nativetest --version
# -> veraPDF 1.30.2, Built: Wed Jun 03 10:55:00 UTC 2026 ...
docker run --rm --entrypoint java octoconv-document-worker:nativetest -version
# -> openjdk version "17.0.20.1" 2026-08-18 (Debian's own package, not verapdf's bundled JRE)
```

This build produces a **native arm64 image**, and veraPDF runs on it using **Debian's own `openjdk-17-jre-headless`**, confirming the exact mechanism the CONTEXT hypothesis named (Path B, the same JRE substitution already used for the amd64 image per `Dockerfile.document-worker`'s own comment at lines 34–46: "the jlink-trimmed JRE's native libs are musl-linked and cannot resolve against bookworm's glibc dynamic loader"). Nothing about that substitution is architecture-specific — it works identically for arm64. `[VERIFIED: docker build + docker run, this session, this host]`

**Recommendation:** change exactly one line in `Dockerfile.document-worker` (`FROM verapdf/cli:v1.30.2 AS verapdf` → `FROM --platform=linux/amd64 verapdf/cli:v1.30.2 AS verapdf`) and remove `platform: linux/amd64` from `docker-compose.yml`'s `document-worker` service (~line 574). The Dockerfile still builds correctly on a genuine amd64 host (the `--platform` pin on that one stage is then a no-op).

## D-02: Differential veraPDF corpus, native vs. emulated — MEASURED, ZERO DIVERGENCE

**Corpus (4 files), all generated or already present in-repo:**

1. **Compliant PDF/A-2b** — real `soffice` export of `internal/e2e/testdata/sample.docx` using the exact `pdfaFilterOptionsSuffix` from `internal/convert/opts.go:23` (`SelectPdfVersion=2`, `EmbedStandardFonts=true`).
2. **Non-conforming, known failing rules** — real `soffice` export of the same `sample.docx`, plain PDF export (no PDF/A `FilterOptions`).
3. **Non-conforming, second source** — the repo's own committed fixture `internal/convert/testdata/verapdf_noncompliant.pdf`.
4. **Corrupt/unparseable** — a deliberately malformed 3-line file starting `%PDF-1.4` with no `startxref` keyword.

**Method:** built the currently-committed `Dockerfile.document-worker` unmodified with `docker build --platform linux/amd64 ...` (reproducing exactly what ships today), and separately used the native-arm64 `nativetest` image from D-01. Ran `verapdf -f 2b --format xml <file>` against all 4 files on both images.

**Result:**

| File | Native arm64 `isCompliant` | Native arm64 failing rules | Emulated amd64 `isCompliant` | Emulated amd64 failing rules | Match |
|---|---|---|---|---|---|
| Compliant PDF/A-2b (soffice) | `true` | — | `true` | — | ✅ |
| Plain PDF (soffice, non-conforming) | `false` | `6.2.4.3`/testNumber 2, `6.6.2.1`/testNumber 1 | `false` | `6.2.4.3`/testNumber 2, `6.6.2.1`/testNumber 1 | ✅ identical rule set |
| `verapdf_noncompliant.pdf` (repo fixture) | `false` | `6.2.4.3`/testNumber 2, `6.6.2.1`/testNumber 1 | `false` | `6.2.4.3`/testNumber 2, `6.6.2.1`/testNumber 1 | ✅ identical rule set |
| Corrupt/unparseable | `failedToParse=1`, exit 7, `"Document doesn't contain startxref keyword"` | — | `failedToParse=1`, exit 7, `"Document doesn't contain startxref keyword"` | — | ✅ byte-identical exception message |

**Divergence table: empty. Zero divergences found across all 4 corpus files.** `[VERIFIED: docker build + verapdf runs on both images, this session, this host]`

This satisfies D-02's own bar exactly ("For every file the verdict (`isCompliant`) and the set of failed rule IDs must match. Any divergence goes into a table ... and a divergence the phase cannot explain blocks the unpin"). The table above is that table, and it is empty by measurement, not by assumption.

### In-phase differential (70-01)

Repeatable run of `scripts/verapdf-differential.sh` against the committed `Dockerfile.document-worker` (verapdf stage pinned alone), run id `20260930T140106Z-30055`, commit `7744ddf` plus the uncommitted Dockerfile edit under test (3 tracked-dirty files, two of them orchestrator state). Docker server arch arm64; native image arch arm64, comparison image arch amd64 (emulated). The extractor was self-checked on the two committed reports before any verdict, and non-vacuity held on both images (3 compliant, 3 non-compliant with failed rules, 1 corrupt file with failedToParse=1 and isCompliant absent). Full transcript: `evidence/verapdf-differential.txt`.

| File | Native isCompliant | Native failedToParse | Native failed rules | amd64 isCompliant | amd64 failedToParse | amd64 failed rules | Result |
|---|---|---|---|---|---|---|---|
| soffice_docx_pdfa.pdf (real PDF/A-2b export of sample.docx) | true | 0 | none | true | 0 | none | MATCH |
| soffice_xlsx_pdfa.pdf (real PDF/A-2b export of sample.xlsx) | true | 0 | none | true | 0 | none | MATCH |
| soffice_pptx_pdfa.pdf (real PDF/A-2b export of sample.pptx) | true | 0 | none | true | 0 | none | MATCH |
| soffice_docx_plain.pdf (plain soffice export) | false | 0 | 6.2.4.3/2 6.6.2.1/1 | false | 0 | 6.2.4.3/2 6.6.2.1/1 | MATCH |
| fixture_noncompliant.pdf | false | 0 | 6.2.4.3/2 6.6.2.1/1 | false | 0 | 6.2.4.3/2 6.6.2.1/1 | MATCH |
| fixture_noncompliant_marker.pdf | false | 0 | 6.6.4/4 6.6.4/5 | false | 0 | 6.6.4/4 6.6.4/5 | MATCH |
| corrupt.pdf | ABSENT | 1 | none | ABSENT | 1 | none | MATCH |

Last line of the transcript: `DIVERGENCES: 0`. veraPDF exit statuses were 0 for the three PDF/A exports, 1 for the three non-compliant files and 7 for the corrupt file, identical on both images (printed, never judged).

**What the D-02 decision does not cover, and the planner should note explicitly:** this session's corpus did not include the actual PDFA e2e test suite (`internal/e2e`'s own PDFA-tagged tests) run end-to-end against a compiled native binary talking to a running document-worker container — that is a heavier integration-test-level proof CONTEXT's D-02 also asks for ("The PDFA e2e must stay green on the native image"). Running the actual `go test ./internal/e2e/...` suite against a natively-built document-worker image is recommended as an early Wave 0/1 task, not a research-phase task — it needs the full compose stack (Postgres/Redis/MinIO), which this research session deliberately avoided starting per the OrbStack "don't run compose while k8s is hot" constraint (k8s was hot on this host throughout this session; only isolated `docker build`/`docker run` commands were used, never `docker compose up`).

## D-09: TMPDIR / JVM temp-dir redirection — MEASURED, MIXED RESULT

### soffice's ordinary scratch writes: fully redirected by `TMPDIR`

**Without `TMPDIR` set**, `strace -f -e trace=open,openat,creat` on a real `docx→pdf/a` conversion showed 5 distinct write targets under bare `/tmp`, none of them inside the `-env:UserInstallation` profile directory:

```
/tmp/7F6dlG
/tmp/CuO5YA
/tmp/lu1556vr46o.tmp/lu1556vr46p.tmp
/tmp/lu1556vr46o.tmp/lu1556vr46q.tmp
/tmp/lu1556vr46o.tmp/lu1556vr46t.tmp/{cert9.db,cert9.db-journal,key4.db,key4.db-journal,pkcs11.txt}
/tmp/u2XN7D
/tmp/UbcxqO
```

(the `cert9.db`/`key4.db`/`pkcs11.txt` set is an NSS security-database profile, created by LibreOffice's digital-signature subsystem probing for a certificate store — confirmed by the `gpg`/`gpgconf`/`gpgsm` child processes seen in a parallel `execve`-only trace).

**With `TMPDIR=/job/tmp` set** (in the engine's own environment only, mirroring `engineEnv`'s existing pattern — never the worker process's own env), the identical conversion produced **zero** opens under bare `/tmp` (`grep -oE '"(/tmp/[^"]*)"' | grep -v /tmp/in.docx` on the full strace log returned no matches), and the redirected directory contained only expected artifacts. `[VERIFIED: strace on octoconv-document-worker:nativetest, this session]`

### soffice's `HOME`-relative lookups: already fail harmlessly today, no grant needed

The same trace (run as `nobody`, matching the production `USER nobody` in `Dockerfile.document-worker`) showed soffice/GLib probing many `HOME`-relative paths: `~/.cache/dconf`, `~/.cache/fontconfig`, `~/.config/dconf`, `~/.config/user-dirs.dirs`, `~/.fontconfig`, `~/.gnupg/{gpg,gpgsm}.conf`, `~/.icedove/profiles.ini` (Thunderbird), `~/.mozilla/{firefox,seamonkey}/profiles.ini`, `~/Documents`, `~/Templates`. In the shipped image `nobody`'s home is `/nonexistent` (`/etc/passwd`), so **all of these already fail today**, without Landlock, under plain Unix permissions (`Permission denied` writing to a directory that doesn't exist) — and the conversion still succeeds. Under Landlock these same lookups will be denied identically (no grant on `/nonexistent`), producing the same already-tolerated failure mode. **No `HOME`-relative grant is needed.** `[VERIFIED: strace, this session — cross-checked against the unconfined baseline run which also showed the identical dconf/fontconfig warnings and still produced a valid PDF]`

### veraPDF's JVM temp-dir: fully redirected by `-Djava.io.tmpdir` + `-XX:-UsePerfData`

`/opt/verapdf/verapdf`'s launch script (line 121: `exec "$JAVACMD" $JAVA_OPTS ...`) honours a `JAVA_OPTS` environment variable. With `JAVA_OPTS="-Djava.io.tmpdir=/job/tmp -XX:-UsePerfData"` set:

```bash
docker exec -e JAVA_OPTS="-Djava.io.tmpdir=/job/vtmp -XX:-UsePerfData" \
  <container> setpriv --reuid nobody --regid nogroup --clear-groups \
  strace -f -e trace=open,openat,creat -o /tmp/verapdf.strace \
  verapdf -f 2b --format xml /job/in.pdf
```

produced **zero** bare-`/tmp` opens, `isCompliant="true"` (exit 0), and the redirected `/job/vtmp` directory contained exactly what a JVM temp root normally holds — a per-run temp subdirectory (`validator.xml`, `fixer.xml`, `features.xml`, `plugins.xml`, `app.xml` — veraPDF's own app-config files, confirmed by the stack trace surfaced when the directory was *not* writable: `org.verapdf.apps.Applications.createAppConfigManager` → `Files.createTempDirectory` → `java.io.tmpdir`) and one `logs*.log` file. **No `hsperfdata_*` file appeared** — confirming `-XX:-UsePerfData` is the correct flag (its absence in an earlier attempt without this flag showed `hsperfdata_nobody`/`hsperfdata_root` under bare `/tmp`, which `java.io.tmpdir` alone does not move, since `hsperfdata` uses a different mechanism than `java.io.tmpdir`-based temp-file creation). `[VERIFIED: strace + XML output, this session]`

### The blocking finding: soffice's single-instance IPC socket is NOT redirectable, and needs a right `internal/sandbox` doesn't grant anywhere

Independent of `TMPDIR`, `soffice.bin` (the real binary the `soffice` wrapper script execs into, confirmed via `strace -f -e trace=execve`) unconditionally attempts:

```
socket(AF_UNIX, SOCK_STREAM, 0) = 11
bind(11, {sa_family=AF_UNIX, sun_path="/tmp/OSL_PIPE_0_SingleOfficeIPC_<hash>"}, 110) = -1 EACCES (Permission denied)
```

Findings from this session, in order of what was tried:

1. **`TMPDIR=/job/tmp` does not move this path.** The bind target stays `/tmp/OSL_PIPE_0_SingleOfficeIPC_*` regardless. The `<hash>` suffix does vary by `-env:UserInstallation` profile path (confirmed: two different profile dirs produced two different hashes), so per-job pipe names would not collide across concurrent jobs — but the **directory component** (`/tmp`) is hardcoded and unaffected by any environment variable tested.
2. **`--nolockcheck` does not eliminate the bind() call.** soffice's own `--help` describes it as "Disables check for remote instances" — tested directly; the `bind()` syscall (and its `EACCES`) still appears in the strace, twice, immediately before the process exits. The flag skips the *detection* logic that reads an existing pipe, not the *creation* of this process's own pipe.
3. **`SAL_ENABLE_FILE_LOCKING` cannot be overridden via environment.** The `/usr/bin/soffice` wrapper script (a POSIX shell script, not the real binary — see the wrapper finding below) unconditionally sets `SAL_ENABLE_FILE_LOCKING=1; export SAL_ENABLE_FILE_LOCKING` near its top, before any user-supplied environment is consulted, so a caller cannot turn this off by pre-setting the variable (`engineEnv`'s wholesale environment replacement would need to invoke `soffice.bin` directly, bypassing the wrapper, to have any chance of controlling this — and the socket bind happens inside `soffice.bin` itself, not the wrapper, so bypassing the wrapper does not avoid it either). `[VERIFIED: cat /usr/bin/soffice, this session]`
4. Under a ruleset granting `/tmp` **read-write** (`PROBE_RW="/job:/tmp"`), the bind still failed with the identical `EACCES`. This is the decisive finding: **it is not a missing grant, it is a missing Landlock *access right*.** `internal/sandbox/sandbox_linux.go`'s `accessReadWrite` constant (lines 17–20) is:

   ```go
   accessReadWrite = unix.LANDLOCK_ACCESS_FS_READ_FILE | unix.LANDLOCK_ACCESS_FS_READ_DIR |
       unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_TRUNCATE |
       unix.LANDLOCK_ACCESS_FS_MAKE_REG | unix.LANDLOCK_ACCESS_FS_MAKE_DIR |
       unix.LANDLOCK_ACCESS_FS_REMOVE_FILE | unix.LANDLOCK_ACCESS_FS_REMOVE_DIR
   ```

   `LANDLOCK_ACCESS_FS_MAKE_SOCK` is present in `handledAccess` (line 44) but is explicitly commented as "granted nowhere" (line 41: "symlink and device creation are handled here and granted nowhere") — meaning **Landlock actively restricts socket creation everywhere in this codebase today**, and no existing class needs to create one, so nobody has hit this before. `document` is the first class whose engine creates a Unix socket during normal operation. `[VERIFIED: internal/sandbox/sandbox_linux.go:13-48, read this session — quoted verbatim above]`

**What this means for the plan:** D-09's own "if not" branch (apply phase 69's html-style decision: shared `/tmp` RW, concurrency 1, `sharedTmpGuard`, `sweepSharedTmp`) is the right shape, but getting there is **not** a document-specific grant-list edit alone — it requires adding `unix.LANDLOCK_ACCESS_FS_MAKE_SOCK` to `accessReadWrite` in `internal/sandbox/sandbox_linux.go`, a change shared by every already-confined class's job-directory grant. This is almost certainly safe (granting MAKE_SOCK on a path already granted READ/WRITE/MAKE_REG/MAKE_DIR does not meaningfully widen what a compromised engine could do inside its own job directory or a shared `/tmp`, and no other class attempts to bind a socket so the change is inert for them) — but it is a change to shared security-relevant code, not a class-local file, and should get its own explicit call-out and probably its own small commit/review in the plan, separate from `document`'s own grant-set files. **Two remaining avenues neither tried nor ruled out in this session, worth a brief Wave 0 spike before committing to the MAKE_SOCK route:**
- Passing `-env:UserInstallation=file:///job/lo-profile` combined with directly invoking `soffice.bin` (bypassing the `/usr/bin/soffice` wrapper) with an *undocumented* internal flag or a differently-named IPC mechanism — LibreOffice's `officeipc` subsystem is known (from public LibreOffice documentation, not verified this session) to sometimes honour `-env:OOO_INSTALL_PIPE_NAME` or similar; this was not tested for lack of time and is `[ASSUMED]`, not `[VERIFIED]`.
- Whether a *newer* LibreOffice release changed this behavior (bookworm ships 7.4; this is a multi-year-old LTS branch) — not checked this session.

## D-10/D-11: veraPDF's own grant set — MEASURED AND LIVE-PROVEN (sufficiency + effectiveness, real Landlock ruleset)

**Trace phase** (`scripts/sandbox-paths-measure.sh trace`, real image, real argv):

```bash
scripts/sandbox-paths-measure.sh trace octoconv-document-worker:nativetest ./job-verapdf \
  sh -c 'JAVA_OPTS="-Djava.io.tmpdir=/job/tmp -XX:-UsePerfData" verapdf -f 2b --format xml /job/in.pdf'
```

Candidate set found, outside `/job` (excluding `/lib`/`/usr/lib`, which are large dynamic-library trees shared with soffice):

```
/dev/null  /dev/random  /dev/urandom
/etc/host.conf  /etc/hosts  /etc/ld.so.cache  /etc/localtime -> /usr/share/zoneinfo/Etc/UTC
/etc/nsswitch.conf  /etc/passwd  /etc/resolv.conf
/opt/verapdf/bin/  /opt/verapdf/bin/cli-1.30.2.jar  /opt/verapdf/verapdf
/proc/cpuinfo  /proc/meminfo  /proc/net/if_inet6  /proc/self/cgroup  /proc/self/coredump_filter
/proc/self/mountinfo  /proc/stat  /proc/sys/vm/overcommit_memory
/sys/devices/system/cpu/{online,possible}
/sys/fs/cgroup/{cgroup.controllers,cpu.max,memory.max}
/sys/kernel/mm/transparent_hugepage/{enabled,hpage_pmd_size}
/usr/bin/which -> /usr/bin/which.debianutils
/usr/lib/jvm/java-17-openjdk-arm64/{lib/libjli.so, lib/libjava.so, lib/libjimage.so, lib/libmanagement*.so, lib/libnet.so, lib/libnio.so, conf/logging.properties -> /etc/java-17-openjdk/logging.properties, conf/security/java.security -> /etc/java-17-openjdk/security/java.security, lib/jvm.cfg -> /etc/java-17-openjdk/jvm-arm64.cfg}
```

(all `/usr/lib/jvm/...` entries fall inside a wholesale `/usr/lib` grant, which soffice's own set already requires — no separate JVM library grant is needed once `/usr/lib` is granted for the class as a whole. `/etc/java-17-openjdk` is the resolved symlink TARGET of the JVM's own `conf/` files and needs its own explicit grant, the same "trace records the path before resolution, Landlock restricts after" trap `SANDBOX-PATHS.md`'s own "Ловушки" §7 already documents for ghostscript.)

**Verify phase — sufficiency AND effectiveness, both PASSED, against a real Landlock ruleset built via `scripts/cmd/sandbox-probe`:**

```bash
RO="/etc/host.conf:/etc/hosts:/etc/ld.so.cache:/etc/localtime:/etc/nsswitch.conf:/etc/passwd:/etc/resolv.conf:/etc/java-17-openjdk:/lib:/usr/lib:/opt/verapdf:/proc:/sys/devices/system/cpu:/sys/fs/cgroup:/sys/kernel/mm/transparent_hugepage:/dev/urandom:/dev/random:/usr/bin"
docker exec -e PROBE_RW="/job:/dev/null" -e PROBE_RO="$RO" \
  -e JAVA_OPTS="-Djava.io.tmpdir=/job/tmp -XX:-UsePerfData" \
  <container> /usr/local/bin/sandbox-probe verapdf -f 2b --format xml /job/in.pdf
# -> exit=0, isCompliant="true"

# effectiveness: identical bytes placed OUTSIDE the job directory
docker exec -e PROBE_RW="/job:/dev/null" -e PROBE_RO="$RO" \
  -e JAVA_OPTS="-Djava.io.tmpdir=/job/tmp -XX:-UsePerfData" \
  <container> /usr/local/bin/sandbox-probe verapdf -f 2b --format xml /outside-verapdf.pdf
# -> exit=7, XML report present, isCompliant ABSENT:
#    <exceptionMessage>Exception: Couldn't parse stream caused by exception:
#      /outside-verapdf.pdf (Permission denied)</exceptionMessage>
#    <batchSummary failedToParse="1" ...><validationReports failedJobs="1">1</validationReports>
```

`[VERIFIED: sandbox-probe against a real Landlock ruleset, this session, this host]`. This is a **complete, live proof** for veraPDF, matching D-10 (own grant set, no write grant needed since `java.io.tmpdir` redirected it into the job directory) and D-11 exactly: exit code (`7`) is identical for the effectiveness-denial case as it would be for many ordinary parse failures, but the XML content unambiguously distinguishes them — `verapdf.go`'s existing `parseVeraPDFReport` (which checks `bs.FailedToParse > 0` before ever looking at `IsCompliant`) already classifies this correctly today, with zero code changes needed on the parsing side. `/dev/null` needed a **read-write** grant, not read-only — the launch script writes to it directly (`/opt/verapdf/verapdf: 1: cannot create /dev/null: Permission denied` was the first symptom when it was granted read-only only).

**Note on `/opt/verapdf/bin/` vs `/opt/verapdf`:** the trace only opened files directly under `bin/`, but granting the whole `/opt/verapdf` directory (as D-10 specifies — "the JRE and `/opt/verapdf` are read-only") is simpler and was what the live-proof above actually used; no reason to narrow it further was found.

## Structural finding: `soffice`'s CLI entrypoint is itself a wrapper script (like `html`'s D-08 precedent)

`/usr/bin/soffice` is **not** a binary — it is a 61+ line POSIX shell script (`readlink -f` resolves it to `/usr/lib/libreoffice/program/soffice`, itself a further wrapper), which resolves its own install directory via `dirname`/`basename`/`ls`/`sed`, execs `oosplash`, which forks `soffice.bin`. `internal/convert/libreoffice.go`'s existing code (unmodified by any prior phase) calls `runCommand(ctx, "soffice", args...)` with no awareness this is a wrapper — this has always been true, and worked because the container ran fully unconfined. Under Landlock, the wrapper's own child processes are separate `execve`s that must each be independently granted:

```
execve("/bin/sh")            <- the wrapper's own shebang interpreter
execve("/usr/bin/basename")
execve("/usr/bin/dirname")
execve("/usr/bin/gpg")       <- LibreOffice's OpenPGP signing subsystem probe (harmless if denied — not required for conversion)
execve("/usr/bin/gpgconf")
execve("/usr/bin/gpgsm")
execve("/usr/bin/grep")
execve("/usr/bin/ls")
execve("/usr/bin/sed")
execve("/usr/bin/soffice")   <- the wrapper itself
execve("/usr/bin/uname")
execve("/usr/lib/libreoffice/program/javaldx")   <- optional; "Warning: failed to launch javaldx" is already tolerated unconfined
execve("/usr/lib/libreoffice/program/oosplash")  <- covered by a wholesale /usr/lib grant
execve("/usr/lib/libreoffice/program/soffice.bin")  <- covered by a wholesale /usr/lib grant
```

`[VERIFIED: strace -f -e trace=execve, this session, real octoconv-document-worker:nativetest image]`. `oosplash` and `soffice.bin` fall inside a wholesale `/usr/lib` grant (already needed for soffice's hundreds of shared libraries, per the trace's un-filtered output — 200+ distinct `.so` files under `/lib/aarch64-linux-gnu`/`/usr/lib/aarch64-linux-gnu`), so no separate grant is needed for them. `dirname`/`basename`/`ls`/`sed`/`grep`/`uname` are all under `/usr/bin`, and this session's testing granted `/usr/bin` wholesale rather than name each one — narrower necessity-by-removal testing (which of the six coreutils the wrapper actually needs vs. which only fire on an unused code path like `--record`/`--backtrace`/`--valgrind`) was not completed and is a Wave 0/1 task; granting `/usr/bin` wholesale is the safe starting candidate, to be narrowed the same way OCR's grant set was narrowed by removal in phase 68.

`gpg`/`gpgconf`/`gpgsm` explain the NSS certificate-database files (`cert9.db`, `key4.db`, `pkcs11.txt`) seen under bare `/tmp` in the unredirected TMPDIR trace — LibreOffice's digital-signature subsystem probes for a certificate store unconditionally at startup, regardless of whether the document being converted uses signatures. These are optional/best-effort probes: the unconfined baseline conversion succeeded despite `Warning: failed to launch javaldx` and multiple `dconf-CRITICAL`/`Fontconfig error` messages already present today, so a denial of `gpg`/`gpgconf`/`gpgsm` themselves is expected to be similarly tolerated — **not separately verified this session** due to the MAKE_SOCK blocker preventing a full end-to-end sufficiency run.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Filesystem confinement | A custom seccomp/namespace sandbox for `document` | `internal/sandbox`'s existing Landlock package, extended with one `const` (`MAKE_SOCK`) | Seven classes already share this exact mechanism; a document-specific mechanism would be the ninth engine's ninth ad-hoc solution to a problem already solved once |
| Content-judged preflight | A new preflight framework for `document` | `internal/convert/sandboxpreflight.go`'s `requireOutput`/`requireDenied`/`requireContains`/`requireLacks`/`preflightMarker` toolkit | Exactly the shape `HTMLPreflight`/`ProbePreflight` already use; `verapdf.go`'s own report parsing already ignores exit code (D-11 is already implemented in `parseVeraPDFReport`, zero code changes needed there) |
| Startup refusal | A document-specific "can I confine?" check | `sandbox.ResolveHelper(documentSandboxDisabledEnv)` | Byte-identical pattern to `chromium-worker`/`pandoc-worker`/etc.; the refusal message (`"landlock is unavailable on this kernel; ... Set %s=true to accept that deliberately"`) is already generic |
| PDF-A / non-PDF-A JVM temp isolation | A custom subprocess wrapper for veraPDF | `JAVA_OPTS="-Djava.io.tmpdir=<job>/tmp -XX:-UsePerfData"` passed through `engineEnv`/`engineExec.withEnv` | The launch script already reads `JAVA_OPTS` (verified this session); no new mechanism needed, same shape as `TMPDIR` for soffice |

**Key insight:** every mechanism this phase needs except `MAKE_SOCK` already exists in the codebase, proven across seven classes. The one genuinely new thing — a Unix-domain-socket bind right — is small, well-scoped, and should be treated as a516 deliberate, reviewed, one-line addition to shared code, not hidden inside a document-specific file.

## Common Pitfalls

### Pitfall 1: Trusting `--nolockcheck`'s name
**What goes wrong:** Assuming a flag named "disables check for remote instances" means the process no longer attempts to acquire the IPC resource at all.
**Why it happens:** The flag disables the *read* side of the lock-check protocol (looking for an existing pipe and asking it to activate), not the *write* side (this process still creates and binds its own pipe so a *future* instance could find it).
**How to avoid:** Verified via `strace -e trace=bind` with and without the flag — the `bind()` call and its `EACCES` are present either way.
**Warning signs:** A ruleset with `/tmp` granted but no `MAKE_SOCK` right still fails identically with or without `--nolockcheck`.

### Pitfall 2: Believing "file created, non-empty" is sufficient for soffice (mirrors the project's own prior ghostscript/chromium lessons)
**What goes wrong:** `validateDocumentOutput`/`validatePDF` already guard against LibreOffice's documented exit-0-but-empty-or-corrupt failure mode — but a *new* preflight for the Landlock ruleset itself must independently prove content, following the exact precedent `SANDBOX-PATHS.md`'s "Ловушки" §8 already documents for ghostscript (same-size skeleton output on denial) and htmlsandbox.go's D-06/D-07 for chromium (blank-but-present PDF on denial).
**Why it happens:** Not yet measured this session whether a Landlock-denied soffice conversion (e.g., an outside-the-grant input file) fails loudly (no output file at all — most likely, since actually reading and parsing the input document is required before any output can be produced, unlike chromium which can render a blank page regardless of input) or silently (chromium's anomalous behavior).
**How to avoid:** This must be measured directly once the MAKE_SOCK blocker is resolved and a full ruleset run becomes possible — Open Question below.
**Warning signs:** A preflight that only checks `convErr != nil` (matching `requireDenied`'s default behavior) without checking output content, if soffice turns out to behave like chromium.

### Pitfall 3: Granting `/opt/verapdf` and `document`'s general `/usr/lib` grant separately without checking for overlap
**What goes wrong:** Assuming veraPDF needs its own copy of `/usr/lib/jvm/...` granted.
**Why it happens:** The JVM's shared libraries live under `/usr/lib/jvm/java-17-openjdk-arm64/lib/*.so`, which is a subdirectory of the `/usr/lib` grant soffice's own hundreds of shared libraries already require.
**How to avoid:** Grant `/usr/lib` once for the whole `document` class (as this session's live-proof did), not per-engine.
**Warning signs:** A grant-set test that lists `/usr/lib/jvm` as a separate readonly entry alongside a wholesale `/usr/lib` entry — redundant, though harmless.

### Pitfall 4: Assuming a shared `/tmp` grant alone is enough once D-09's "if not" branch is taken
**What goes wrong:** Copying html's `htmlReadWrite = []string{"/tmp", "/dev/null"}` verbatim into a new `documentReadWrite` var and expecting the socket bind to start working.
**Why it happens:** `accessReadWrite` (the rights granted to every path in a class's `ReadWrite` list) does not include `MAKE_SOCK` today — adding a path to `ReadWrite` does not add a right that was never in the access mask to begin with.
**How to avoid:** The `internal/sandbox/sandbox_linux.go` change (adding `unix.LANDLOCK_ACCESS_FS_MAKE_SOCK` to `accessReadWrite`) must land **before or together with** the document-specific grant-set file, or the sufficiency test will fail with the exact same `EACCES` this session hit.
**Warning signs:** `soffice.bin` exits 1 silently, immediately after opening `.../user/temp/embeddedfonts/{fromdocs,fromsystem}` (both expected `ENOENT`s, unrelated), with no other error printed — this is the fingerprint of the MAKE_SOCK gap specifically; `strace -e trace=bind` is the fastest way to confirm it's this and not something else.

## Code Examples

### Verified TMPDIR/JAVA_OPTS pattern for `engineEnv` (mirrors `probesandbox.go`'s existing `withEnv(engineEnv(jobDir))` call)

```go
// documentsandbox.go (illustrative — not yet written; mirrors probesandbox.go's exec() method)
func (c LibreOfficeConverter) exec(jobDir string) engineExec {
    // soffice: TMPDIR alone moves every ordinary scratch write (measured,
    // this session) EXCEPT the OSL_PIPE_0_SingleOfficeIPC bind, which is
    // covered by the shared /tmp grant + MAKE_SOCK access right instead.
    return c.Sandbox.forJobRW(jobDir, documentReadWrite, documentReadOnly, documentEngines).
        withEnv(engineEnv(jobDir))
}

// verapdf's own JAVA_OPTS, set on the SAME engineExec's environment
// (engineEnv already supports appending arbitrary additional vars the way
// TMPDIR is appended today; JAVA_OPTS would need a parallel path since it's
// a launch-script convention, not one of engineEnv's allowlisted names)
```

### Content-judged veraPDF result (already implemented, zero changes needed)

```go
// internal/convert/verapdf.go:96-101 -- ALREADY does exactly what D-11 asks:
// exit code is never consulted; the batchSummary failure counters (which a
// Landlock denial trips via failedToParse) are checked BEFORE isCompliant.
if bs.FailedToParse > 0 || bs.OutOfMemory > 0 || bs.VeraExceptions > 0 || bs.ValidationReports.FailedJobs > 0 {
    return false, "", fmt.Errorf(
        "batch summary reports a validator failure (failedToParse=%d outOfMemory=%d veraExceptions=%d failedJobs=%d)",
        bs.FailedToParse, bs.OutOfMemory, bs.VeraExceptions, bs.ValidationReports.FailedJobs,
    )
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|---------------|--------|
| `document-worker` pinned to `platform: linux/amd64` for the whole image, because `verapdf/cli` is amd64-only | Pin only the `verapdf` build stage; the runtime stage (and therefore the whole shipped image) builds natively for whatever host builds it | This phase, pending planner acceptance of D-01/D-02's measured evidence | `document` joins the other seven classes as a normally-measurable, normally-live-gated class on any host; the "needs an amd64 machine" constraint (D-03's fallback) very likely disappears entirely |
| `internal/sandbox`'s `accessReadWrite` grants no socket-creation right anywhere | (proposed) `accessReadWrite` includes `LANDLOCK_ACCESS_FS_MAKE_SOCK` | Not yet changed — this phase's likely first code change | Every already-confined class's writable grant gains the ability to `bind()` a Unix socket inside its own job directory or shared writable paths; harmless for classes that don't do this (seven of eight), necessary for `document` |

**Deprecated/outdated:** The CONTEXT's own hopeful framing of D-09 ("If every write moves: `document` gets no `/tmp` grant, and `DOCUMENT_WORKER_CONCURRENCY` stays at its default of 2") does not survive measurement — the ordinary scratch writes do move, but the IPC socket bind does not, and the "if not" branch applies. This is exactly the kind of finding CONTEXT's own D-09 anticipated might happen ("Decide by measurement, not by reading the docs") — the docs would never have surfaced this, since it isn't documented LibreOffice behavior, it's an emergent Landlock interaction the project's own strace-first method exists to catch.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `-env:OOO_INSTALL_PIPE_NAME` or a similar undocumented LibreOffice option might allow relocating/disabling the `OSL_PIPE_0_SingleOfficeIPC` bind, avoiding the `MAKE_SOCK` grant entirely | D-09 blocking finding | If false, no impact — MAKE_SOCK route already recommended as the default; if true, a smaller/less invasive fix becomes available and the planner should re-check before committing to the sandbox.go change |
| A2 | `gpg`/`gpgconf`/`gpgsm` denial (once `document`'s grant set is narrowed away from wholesale `/usr/bin`) is tolerated the same way `javaldx`/dconf/fontconfig denials already are | Structural finding: soffice wrapper script | If false, PDF-signature-adjacent conversions could fail in ways not caught by this session's plain-docx corpus; low risk since no test fixture in the repo exercises digital signatures |
| A3 | Soffice's Landlock-denial failure mode for a genuinely-outside-grant INPUT file is a loud failure (non-zero exit, no output file) rather than chromium's silent-blank-output anomaly | Pitfall 2 | If false (soffice behaves like chromium), `DocumentPreflight`'s effectiveness check needs a content marker (like chromium's `/Title`) rather than a bare `convErr != nil` check — this was not directly measured this session because the MAKE_SOCK blocker prevented a full successful ruleset run to compare against |
| A4 | Granting `/usr/bin` wholesale (rather than naming `dirname`/`basename`/`ls`/`sed`/`grep`/`uname` individually) is an acceptable starting candidate, later narrowed by necessity-by-removal | Structural finding | Low risk — this is exactly OCR's/probe's own measurement methodology (start broad, narrow by removal); the wholesale grant is strictly less secure until narrowed, so the plan should not treat it as final |

**If this table is empty:** N/A — see above.

## Open Questions

1. **Does soffice fail loudly or silently on a genuinely Landlock-denied conversion?**
   - What we know: LibreOffice's documented failure mode (exit 0, no output file, `filterFor`'s "no export filter for" case) is unrelated to Landlock specifically. Chromium's own Landlock-specific failure mode (blank output, not loud) was a genuine surprise discovered in phase 69.
   - What's unclear: whether soffice — which must actually parse the input document before producing any output, unlike chromium which can always render *something* — fails the same loud way probe's `gs`/`pdfinfo`/`unzip` do, or the same silent way chromium does.
   - Recommendation: measure this directly as the first Wave 0/1 task, once the `MAKE_SOCK` blocker is resolved and a full successful ruleset exists to compare against. Reuse the exact effectiveness-test shape `probesandbox.go`'s `probePreflightEffectiveness` or `htmlsandbox.go`'s `HTMLPreflight` already establish, whichever the measurement points to.

2. **Is the `MAKE_SOCK` grant the right fix, or is there a LibreOffice-side way to avoid the bind entirely?**
   - What we know: `--nolockcheck` doesn't work; `TMPDIR` doesn't move the bind target; `SAL_ENABLE_FILE_LOCKING` can't be overridden via env (the wrapper hardcodes it).
   - What's unclear: whether an undocumented flag, a newer LibreOffice version, or invoking `soffice.bin` with a hand-built minimal environment (bypassing the wrapper script's own `export`s) changes this.
   - Recommendation: budget a short (1-2 hour) Wave 0 spike to check LibreOffice's own source/changelog for `OSL_PIPE`/`officeipc` env var overrides before committing to the `internal/sandbox` change; if nothing surfaces quickly, take the `MAKE_SOCK` route (this session's own recommendation) rather than spending further research-phase time on it.

3. **What is `verapdf.go`'s exact `engineEnv`/`JAVA_OPTS` wiring shape, given `engineEnv` today only supports `PATH`/`HOME`/`LANG`/`LC_ALL`/`TMPDIR`?**
   - What we know: the launch script reads `JAVA_OPTS` from its environment (verified) and `engineExec.withEnv` fully replaces the engine's environment (verified, `sandboxwrap.go:119-126`).
   - What's unclear: whether `JAVA_OPTS` should become a new allowlisted variable in `engineEnv` (widening it for every class, though only `document` would ever set it) or a document-specific environment-building function separate from `engineEnv`.
   - Recommendation: a document-specific env builder (e.g. `documentVerapdfEnv(jobDir)`) that calls `engineEnv(jobDir)` and appends `JAVA_OPTS` is the smaller, more scoped change — avoid widening the shared allowlist for a variable only one class needs.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Docker / OrbStack | image builds, live measurement | ✓ | Docker 29.4.0, OrbStack context | — |
| `verapdf/cli:v1.30.2` (Docker Hub) | veraPDF stage | ✓ (pulled and inspected this session) | v1.30.2, confirmed amd64-only manifest | — |
| `golang:1.26-bookworm` (build stage) | Go build | ✓ (already used by the committed Dockerfile) | matches `go.mod` | — |
| `debian:bookworm-slim` + `libreoffice-*-nogui` packages | runtime stage | ✓ (already committed) | LibreOffice 7.4 (bookworm) | — |
| A genuine amd64 host/node | D-03's fallback, IF the unpin fails | Not checked this session (unpin succeeded, so not needed) | — | Explicit `checkpoint:decision`, per D-03, if the planner or plan-checker finds a reason this session's evidence is insufficient |
| k8s cluster (OrbStack) | none for this research | hot throughout this session (other projects' pods) | — | Deliberately avoided: no `docker compose up` was run this session, only isolated `docker build`/`docker run`, per the OrbStack discipline noted in the task and this user's own memory ("never compose and k8s hot simultaneously") |

**Missing dependencies with no fallback:** none.

**Missing dependencies with fallback:** the genuine amd64 host (D-03's fallback path) — not needed given this session's D-01/D-02 results, but the plan should still carry the `checkpoint:decision` D-03 specifies, in case the plan-checker or a later measurement surfaces something this session's corpus didn't cover (e.g., the fuller PDFA e2e suite noted under D-02 above).

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | unrelated to this phase |
| V3 Session Management | no | unrelated to this phase |
| V4 Access Control | yes | Landlock filesystem confinement IS the access-control mechanism this phase implements; `internal/sandbox` (existing, extended by one const) |
| V5 Input Validation | no (unchanged this phase) | already covered by phases 60/67-69's existing sanitize/validate pipeline |
| V6 Cryptography | no | unrelated — note LibreOffice's own NSS-database probing (gpg/gpgconf/gpgsm) is an *optional* signature-verification subsystem this phase does not enable or rely on, and denying it under Landlock is the intended (and already-tolerated-unconfined) outcome |
| V12 File and Resources | yes | This entire phase — Landlock is the file-access boundary; `runCommand`'s existing process-group-kill (`exec.go`, D-13, unchanged) is the resource boundary |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| A malicious `.docx`/`.xlsx`/`.pptx` embeds a macro or external reference attempting to read a file outside the job directory (the exact class of bug PR #19 found in `.tex`, and PR #21/phases 67-69 closed for the other seven engine classes) | Information Disclosure | Landlock ruleset, this phase's whole purpose — `document` is the LAST of nine engine classes to close this gap |
| A malicious PDF handed to veraPDF triggers a path-traversal read during validation (veraPDF parses arbitrary PDF structure, itself a large Java codebase) | Information Disclosure | Same Landlock ruleset; D-10/D-11's separate, narrower veraPDF grant set (no `/usr/bin` wholesale, no soffice's coreutils) limits blast radius further than a single combined grant would |
| A confined engine attempts to bind a Unix socket for some purpose OTHER than the legitimate single-instance IPC (e.g., an exploited LibreOffice binary tries to open a listening socket for data exfiltration or a reverse shell) | Elevation of Privilege / Information Disclosure | `MAKE_SOCK` is proposed to be granted only within the same paths already granted `READ`/`WRITE` (the job directory and a shared `/tmp`) — it does not grant network-socket rights (Landlock's filesystem-access-control scope does not cover `AF_INET`/`AF_INET6` at all; this codebase's own `REQUIREMENTS.md` "Out of Scope" table already notes network confinement is deliberately deferred to the deployment's own egress controls, not Landlock) |

## Runtime State Inventory

Not applicable — this is not a rename/refactor/migration phase.

## Sources

### Primary (HIGH confidence — measured live, this session, on this host)
- `docker pull --platform linux/amd64 verapdf/cli:v1.30.2` + `find`/`file`-equivalent inspection — D-01
- Scratch native-arm64 Dockerfile build + `docker run` — D-01
- Fresh `docker build --platform linux/amd64 -f Dockerfile.document-worker` (the committed, unmodified Dockerfile) + 4-file corpus run — D-02
- `strace -f -e trace=open,openat,creat` on real `soffice`/`verapdf` invocations, with and without `TMPDIR`/`JAVA_OPTS` — D-09
- `strace -f -e trace=execve` on a real `soffice` conversion — wrapper-script finding
- `strace -f -e trace=bind,socket,connect` on a real `soffice` conversion under a Landlock ruleset — MAKE_SOCK blocking finding
- `scripts/cmd/sandbox-probe` built for the image's own architecture, run against a real Landlock ruleset (via `docker run --entrypoint`) — D-10/D-11's full sufficiency+effectiveness proof for veraPDF
- `internal/sandbox/sandbox_linux.go` (read in full, this session) — `accessReadWrite`/`accessReadOnly`/`handledAccess` constants quoted verbatim
- `internal/convert/verapdf.go`, `internal/convert/libreoffice.go`, `internal/worker/worker.go` (read in full, this session) — existing code shape, `isDocumentTerminal`'s missing `ErrEngineSandboxUnavailable` guard
- `Dockerfile.document-worker`, `docker-compose.yml` (document-worker service + sibling comment references), `deploy/chart/octoconv/templates/deployment-document-worker.yaml` (read this session)

### Secondary (MEDIUM confidence)
- `.planning/research/SANDBOX-PATHS.md` — the seven already-measured classes' shape/method/pitfalls, directly reused this session
- `.planning/todos/pending/2026-09-06-other-engines-still-unconfined.md` — the original emulation-vs-native measurement table (2026-09-19) this session's D-01/D-02 supersedes for `document` specifically

### Tertiary (LOW confidence, flagged `[ASSUMED]` in-line)
- A1 (possible undocumented LibreOffice IPC-pipe env var) — not verified, training-knowledge-adjacent guess only

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — no new packages, existing Dockerfile/compose/chart shape reused verbatim
- Architecture: HIGH — mirrors 7 already-shipped classes exactly, one new shared-code change identified and scoped
- Pitfalls: HIGH for the MAKE_SOCK finding (fully reproduced with strace evidence); MEDIUM for the soffice-loud-vs-silent-failure question (not yet measured, honestly flagged as Open Question 1)

**Research date:** 2026-09-29
**Valid until:** 30 days (stable — LibreOffice 7.4/bookworm and veraPDF 1.30.2 are both pinned, unlikely to move; re-verify if either pin changes)
