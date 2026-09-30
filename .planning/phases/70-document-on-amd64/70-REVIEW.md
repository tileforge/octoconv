---
phase: 70-document-on-amd64
reviewed: 2026-09-30T00:00:00Z
depth: standard
files_reviewed: 31
files_reviewed_list:
  - .env.example
  - Dockerfile.document-worker
  - cmd/document-worker/main.go
  - deploy/chart/octoconv/templates/configmap.yaml
  - deploy/chart/octoconv/templates/deployment-document-worker.yaml
  - docker-compose.e2e.yml
  - docker-compose.yml
  - internal/convert/documentsandbox.go
  - internal/convert/documentsandbox_test.go
  - internal/convert/libreoffice.go
  - internal/convert/libreoffice_test.go
  - internal/convert/sandboxcoverage_test.go
  - internal/convert/sandboxexec_test.go
  - internal/convert/sandboxwrap.go
  - internal/convert/verapdf.go
  - internal/e2e/testdata/verapdf-shim/soffice
  - internal/sandbox/access_linux_test.go
  - internal/sandbox/execmode.go
  - internal/sandbox/sandbox.go
  - internal/sandbox/sandbox_linux.go
  - internal/sandbox/spec_test.go
  - internal/worker/engine_sandbox_retryable_test.go
  - internal/worker/worker.go
  - scripts/audio-rtf-measure.sh
  - scripts/cmd/sandbox-probe/main.go
  - scripts/conf05-crosscheck.sh
  - scripts/document-sandbox-measure.sh
  - scripts/sandbox-liveproof.sh
  - scripts/sandbox-paths-measure.sh
  - scripts/verapdf-differential.sh
  - scripts/verapdf-measure.sh
findings:
  critical: 0
  warning: 2
  info: 3
  total: 5
status: issues_found
---

# Phase 70: Code Review Report

**Reviewed:** 2026-09-30
**Depth:** standard
**Files Reviewed:** 31
**Status:** issues_found

## Summary

Reviewed the diff 7744ddf..HEAD for all 31 files. `go build ./...`, `go vet` on the touched packages, and `go test` for `internal/sandbox`, `internal/worker` and the document/sandbox tests of `internal/convert` all pass.

Points the review asked about, and what was found:

- **Socket-only grant (`sock:` / `accessMakeSocket`).** Sound. `accessMakeSocket` equals `LANDLOCK_ACCESS_FS_MAKE_SOCK` exactly and is granted only through the `Paths.Socket` loop in `Apply`. `accessReadWrite`, `accessReadOnly` and `handledAccess` are byte-for-byte unchanged (MAKE_SOCK was already handled). `addPath` narrows a non-directory to `accessFileOnly`, which shares no bit with MAKE_SOCK, so a `sock:` token naming a file fails closed (EINVAL path) instead of silently granting nothing. `RunExecMode` counts `len(p.Socket)` in the "has any rule" test, so a sock-only spec is not turned into an empty ruleset that denies everything. `ParseSpec` and `Spec` round-trip the token, and `forJobRW` passes a nil socket list, so every other class's token stream is unchanged (pinned by tests).
- **`internal/convert/exec.go`.** Unchanged since 7744ddf (empty `git diff --stat`).
- **Fail-closed startup order.** `RunExecModeIfRequested`, then `ResolveHelper`, then `ConfinedDocument` (concurrency guard), then `SetVeraPDFTimeout`, then `DocumentPreflight`, then `db.Connect`. Correct; see WR-01 for a hole in the guard.
- **Retryable classification.** `isDocumentTerminal` checks `ErrEngineSandboxUnavailable` first, before the timeout rule and before the "pdf/a validation error" signature. `ValidatePDFA` keeps the sentinel reachable with `%w`. The new non-vacuous test case covers it.
- **Shared-/tmp sweep.** `Convert` sweeps before any engine runs and spares the job directory. The Dockerfile removes the root-owned `hsperfdata_root` left by the JRE post-install in the same layer.
- **Content-judged preflight.** Sufficiency precedes each negative on the same argv and ruleset. The marker is per-run random. The outside fixture lives in `/var/tmp`, and `requireOutsideGrants` proves it lies outside every grant set at run time. `veraPDFHasVerdict` is independent of `parseVeraPDFReport`.
- **Shell scripts.** No `set -e` traps, unquoted expansions or BSD-incompatible constructs were found in the new or changed scripts. Empty-array expansions use the `${a[@]+"${a[@]}"}` idiom for bash 3.2. The remaining issues are minor (IN-02, IN-03).

No Critical issues. Two Warnings and three Info items follow.

## Warnings

### WR-01: `DOCUMENT_WORKER_CONCURRENCY<=0` bypasses the shared-/tmp guard and runs asynq at NumCPU

**File:** `cmd/document-worker/main.go:77` (with `internal/convert/sandboxwrap.go:230`)
**Issue:** `sharedTmpGuard` returns nil for `concurrency <= 1`, and `envInt` returns whatever `strconv.Atoi` parses, so `DOCUMENT_WORKER_CONCURRENCY=0` (or a negative value) reaches `asynq.Config{Concurrency: concurrency}` unchanged. asynq v0.26.0 (`server.go:449-452`) treats `n < 1` as `runtime.NumCPU()`. The worker then runs several tasks at once in one container.

soffice is granted `rw:/tmp` and `sock:/tmp`, and `Convert` sweeps `/tmp` before every task. At NumCPU concurrency this makes the guard's two reasons real at once:
- concurrent tasks can read and write each other's files in `/tmp`;
- each task's sweep deletes the other tasks' job directories and soffice sockets mid-run, which shows up as corrupted or failed conversions.

The guard exists to fail closed, and this value gets past it. A chart `extraEnv` or `.env` typo reaches it. `TestConfinedDocument` covers 1, 2 and 4 but not 0 or negative values.
**Fix:** Treat any value below 1 as a configuration error when confined, in the shared guard so html benefits too (`cmd/chromium-worker/main.go:62` has the same shape):
```go
// internal/convert/sandboxwrap.go
func sharedTmpGuard(readWrite []string, s *engineSandbox, concurrency int, concurrencyEnv string) error {
	if !s.confines() || !slices.Contains(readWrite, sharedTmpDir) {
		return nil
	}
	if concurrency < 1 {
		return fmt.Errorf("%s=%d with the sandbox enabled: asynq treats a value below 1 as the CPU count, "+
			"which would exceed the one task per container the shared %s requires; set %s=1 (CONF-06)",
			concurrencyEnv, concurrency, sharedTmpDir, concurrencyEnv)
	}
	if concurrency == 1 {
		return nil
	}
	// existing "above 1" refusal
}
```
Add 0 and -1 cases to `TestConfinedDocument`. Alternatively, clamp in `main.go` before both the guard and asynq.

### WR-02: veraPDF's `ro:/proc` grant exposes the parent worker's environment (DATABASE_URL, S3 keys) to a compromised JVM

**File:** `internal/convert/documentsandbox.go:92` (grant), `internal/convert/documentsandbox.go:126-137` (scrub)
**Issue:** The engine environment is scrubbed so that `/proc/self/environ` holds no secrets, and the `engineEnv` comment cites that leak as the reason. But Landlock grants by path, and `ro:/proc` covers `/proc/<pid>/environ` for every process. The veraPDF JVM runs as the same uid (`nobody`) as the worker and as tini (PID 1). The worker's `/proc/<ppid>/environ` and `/proc/1/environ` hold the unscrubbed `DATABASE_URL`, `S3_ACCESS_KEY`, `S3_SECRET_KEY` and so on, and they are readable by same-uid, dumpable processes. `no_new_privs` and Landlock do not change that. The measured reason for `/proc` is the JVM's container sizing (`/proc/meminfo`, `/proc/self/cgroup`), which does not need other processes' directories.

veraPDF parses a PDF whose bytes derive from client-controlled content, so a JVM or parser-level code-execution bug could read those variables. The environment scrub then protects only `/proc/self`. soffice's set does not grant `/proc`, so the gap is veraPDF-specific here. html and tectonic carry the same exposure. The phase goal is that the class is contained.
**Fix:** Narrow the grant to what V0 actually needs. The helper execs the engine, so the PID is stable across the exec: grant the specific files (`/proc/meminfo`, `/proc/cpuinfo`, `/proc/stat` if measured as needed) and `/proc/<helper-pid>`, resolved at spec-build time, and re-run `scripts/document-sandbox-measure.sh` V0/V2 to confirm the JVM view is unchanged. If narrowing is judged out of scope, record the residual risk explicitly in SANDBOX-PATHS.md and in the DEBT entry; the environment scrub comment currently overstates what it closes.

## Info

### IN-01: Startup preflight ignores SIGINT/SIGTERM for up to 60 s

**File:** `cmd/document-worker/main.go:109`
**Issue:** `signal.NotifyContext` is installed first, which replaces the default fatal handling of SIGINT/SIGTERM. The preflight context is then derived from `context.Background()`, not `ctx`. A `docker stop` or `kubectl delete` during the preflight (about 6 s measured, bounded by 60 s) is swallowed until the preflight ends.
**Fix:** `preflightCtx, cancel := context.WithTimeout(ctx, documentPreflightTimeout)`.

### IN-02: `verapdfEnv` builds `JAVA_OPTS` without quoting the tmp path

**File:** `internal/convert/documentsandbox.go:136`
**Issue:** The launcher script word-splits `JAVA_OPTS`, so a job-directory path containing whitespace (for example an operator-set `TMPDIR` with a space) would split `-Djava.io.tmpdir=<path>` into separate JVM arguments. The worker fails closed with a "validation error" rather than misbehaving quietly, but the cause would be opaque.
**Fix:** Validate that `documentPreflightTmpDir(jobDir)` has no whitespace when building the env, or return an error from `Convert` naming `TMPDIR`.

### IN-03: Minor shell hygiene in `scripts/verapdf-differential.sh`

**File:** `scripts/verapdf-differential.sh:170,174` and `:47-58`
**Issue:** In `nonvacuity`, `rest` is not declared `local` and leaks into the global scope. Separately, `export_pdf` has the container (as `nobody`) create `profile-*` and `work-*` content under `$CORPUS`. On a Linux Docker host the host user cannot remove those nobody-owned files, so the `rm -rf "$CORPUS"` in the EXIT trap would fail and leave the corpus behind. On macOS/OrbStack, where the script is run, file ownership is mapped and this does not bite.
**Fix:** Add `rest` to the `local` line, and make the trap's cleanup tolerant (`rm -rf "$CORPUS" 2>/dev/null || log "could not remove $CORPUS (container-owned files)"`).

---

_Reviewed: 2026-09-30_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
