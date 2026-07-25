# Phase 41: Progress Core - Research

**Researched:** 2026-07-25
**Domain:** ffmpeg `-progress` stream parsing + hardened subprocess exec extension (Go `os/exec`) + non-locking Postgres denormalized writes
**Confidence:** HIGH

## Summary

This phase adds real-time `progress_pct`/`eta_seconds` to `GET /v1/jobs/{id}` for av jobs, sourced from ffmpeg's `-progress` machine-readable stream, without touching the hardened `runCommand` SIGKILL contract. The core technical risk (a leaked reader goroutine or a broken process-group kill) was **live-verified in this research session** against the actual pinned ffmpeg 8.1.2/n8.1.2 binary running on this machine, using the exact pattern this project's own prior v1.9 research (`STACK.md`/`ARCHITECTURE.md`/`PITFALLS.md`) already specified: a dedicated `fd 3` pipe wired via `cmd.ExtraFiles`, `-progress pipe:3` (not `pipe:1`/stdout — stdout stays untouched, avoiding any collision with `runCommand`'s existing diagnostics capture), the parent's write-end `*os.File` closed immediately after `cmd.Start()`, and a scanner goroutine that is joined (`<-scanDone`) on both the success and the `ctx.Done()`/SIGKILL exit paths.

A live 5-iteration SIGKILL-mid-stream test (this session, see Code Examples) confirmed: zero goroutine-count delta (`runtime.NumGoroutine()` before/after), zero orphaned ffmpeg processes in the OS process table, and progress lines were captured on every iteration before the kill. This is the same live-verification discipline the project applied to Phase 9's zombie-reaping and Phase 39's `cancel-liveproof.sh` — the planner should require an equivalent live-gated script/test, not `go test -race` alone (PROG-05 explicitly says race alone is insufficient).

A real fixture captured from `ffmpeg 8.1.2` (matches the pinned `n8.1.2` build byte-for-byte per version string, though **not** literally the same container binary — see Open Questions) confirms the well-known `out_time_ms`-is-microseconds naming wart: `out_time_us` and `out_time_ms` report the **identical** value (both microseconds, e.g. `19933333` for a 19.933333s point), and the first block(s) of a run can report `out_time_us=N/A`, `speed=N/A` (warm-up/no-data-yet state) — the parser MUST treat `N/A` as "no data yet," not attempt a numeric parse.

Three prior-research documents already exist in `.planning/research/` (STACK.md, ARCHITECTURE.md, PITFALLS.md, SUMMARY.md, dated 2026-07-24) containing a fully-worked `runCommandWithProgress` code sketch, a non-locking `UpdateProgress` SQL pattern, and an exhaustive pitfalls catalog for this exact phase. This RESEARCH.md treats those as HIGH-confidence prior art (cross-verified live in this session), synthesizes them against the 41-CONTEXT.md locked decisions (D-01..D-06), and resolves the one real discrepancy found (STATE.md's stray note about `AudioConverter`'s ffmpeg-normalize stage also implementing progress — superseded by CONTEXT.md D-01, which scopes the optional interface to `AVConverter` only).

**Primary recommendation:** Implement `runCommandStreaming` as a byte-for-byte-independent sibling of `runCommand` in `exec.go`, using `os.Pipe()` + `cmd.ExtraFiles` (fd 3) + `-progress pipe:3`; implement a `ProgressSink`/`ProgressReporter` optional interface satisfied only by `AVConverter`; write progress through a new non-locking `Repo.UpdateProgress` that never contends with `transition()`; gate every intermediate write behind a dual time+delta throttle with an unconditional terminal-write bypass; derive ETA via an EMA of `speed=` after a short warm-up.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| ffmpeg `-progress` stream capture (subprocess exec) | Worker (API/Backend) | — | `internal/convert` is invoked exclusively inside the worker binary; no browser/frontend tier exists in this project |
| Progress parsing (key=value → percent/ETA) | Worker (API/Backend) | — | Pure function inside `internal/convert`, unit-testable without a live process |
| Progress persistence (denormalized `jobs` columns) | Database / Storage | — | Postgres is system of record (`PROJECT.md`); non-locking `UPDATE` is the write path |
| Progress read/surface on `GET /v1/jobs/{id}` | API / Backend | — | `internal/api/handlers.go`'s `handleGetJob`, null-aware JSON |
| Cancel-mid-progress signal delivery | API / Backend (Redis pub/sub) | Worker (registry) | Reuses Phase 39's `internal/cancel` package unchanged — no new tier |

There is no browser/CDN tier in OctoConv (internal API service, no frontend shipped in this milestone) — all five phase requirements live entirely in the worker + API + database tiers already mapped in `CLAUDE.md`'s Layers section.

## Standard Stack

### Core

No new dependencies. Everything needed is stdlib, already vendored, or already used elsewhere in this codebase `[VERIFIED: go.mod, go.sum — read directly]`:

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| stdlib `os` (`os.Pipe`) | Go 1.26.4 | Progress fd plumbing | Same primitive family `exec.go`'s `runCommand` already uses |
| stdlib `os/exec` + `syscall` | Go 1.26.4 | Process spawn + Setpgid + SIGKILL | Identical to existing `runCommand` — the streaming variant is additive, not a replacement |
| stdlib `bufio.Scanner` | Go 1.26.4 | Line-by-line `-progress` block parsing | Simplest correct tool for a `key=value\n` stream; no JSON, no custom framing needed |
| `github.com/jackc/pgx/v5` v5.10.0 (existing) | already pinned | Non-locking `UPDATE ... WHERE status='active'` | Reuses the existing pool; no new query builder needed for a 4-column UPDATE |

### Supporting

None. This is a stdlib-only phase.

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `os.Pipe()` + `cmd.ExtraFiles` (fd 3) | Named FIFO (`syscall.Mkfifo`) or Unix domain socket | Only relevant if progress needed to survive a worker-process restart mid-conversion — not a requirement here; adds filesystem cleanup risk for zero benefit `[CITED: .planning/research/STACK.md]` |
| `-progress pipe:3` (dedicated fd) | `-progress pipe:1` (stdout) | Technically also works (AVConverter's ffmpeg calls don't use stdout for anything today) but conflates stdout's meaning conditionally and risks future collision (veraPDF already uses stdout for its own machine-readable report elsewhere in this codebase) `[VERIFIED: live test, this session]` |
| EMA for `speed=`-derived ETA | Raw `remaining/speed` per tick, or fixed-N sliding window | Raw ratio is jittery (rejected per D-04); fixed-N needs a buffer and reacts harshly to outliers (rejected per D-04) |

**Installation:**
```bash
# No new dependencies — nothing to install.
```

**Version verification:** ffmpeg is not a Go module dependency; it is a system binary pinned by commit hash in `Dockerfile.av-worker` (`FFMPEG_COMMIT=38b88335f99e76ed89ff3c93f877fdefce736c13`, tag `n8.1.2`, asserted at build time via `ffmpeg -version | grep -q "ffmpeg version n\?8\.1\.2"`). The local dev machine's Homebrew ffmpeg (`/opt/homebrew/bin/ffmpeg`, version string `ffmpeg version 8.1.2`) matches this version string exactly `[VERIFIED: ffmpeg -version, this session]`, making it a valid stand-in for capturing fixtures, though it is **not** the identical compiled binary (Homebrew build vs. the project's minimal from-source build with `--disable-everything` + a narrow codec/filter allowlist). See Open Questions for the residual gap.

## Package Legitimacy Audit

Not applicable — zero new Go dependencies, zero new external packages of any kind. `slopcheck`/registry verification was not run because there is nothing to install.

## Architecture Patterns

### System Architecture Diagram

```text
                         asynq task (job_id only)
                                │
                                ▼
                    Worker: Handler.HandleAVConvert
                                │
                                ▼
                    Handler.process(ctx, job)
                                │
                    registry.Lookup(source,target) → conv (AVConverter)
                                │
              type-assert: conv.(convert.ProgressConverter) ?
                        │                           │
                       yes                          no (4 other engines)
                        │                           │
                        ▼                           ▼
        conv.ConvertWithProgress(ctx, in, out,      conv.Convert(ctx, in, out, opts)
          opts, onProgress)                         (existing path, byte-for-byte unchanged)
                        │
                        ▼
        AVConverter internally calls runCommandStreaming
        for the TRANSCODE ffmpeg invocation only (D-02);
        audio-extract/thumbnail/stream-copy call plain runCommand (null progress)
                        │
        ┌───────────────┴────────────────────────────┐
        │ runCommandStreaming(ctx, name, args, onLine)│
        │  os.Pipe() → cmd.ExtraFiles=[w] (fd 3)      │
        │  args include "-progress","pipe:3"          │
        │  cmd.Start(); w.Close() [PARENT'S COPY]     │
        │  goroutine A: bufio.Scanner(r) → onLine()   │
        │  goroutine B: cmd.Wait() → done chan        │
        │  select { ctx.Done(): SIGKILL(-pid); <-done;│
        │           <-scanDone   /* join, no leak */  │
        │         case <-done:   <-scanDone; return } │
        └───────────────┬──────────────────────────────┘
                        │ onLine(rawKVLine)
                        ▼
        parseProgressLine → accumulate block until "progress=" terminator
                        │
        onProgress(ProgressUpdate{outTimeSeconds, speed, done bool})
                        │
                        ▼
        worker's throttle wrapper (time>=2s AND delta>=1%, OR terminal)
                        │
                        ▼
        jobs.Repo.UpdateProgress(ctx, jobID, pct, eta)
          → UPDATE jobs SET progress_pct=$2, eta_seconds=$3,
                             progress_updated_at=now()
            WHERE id=$1 AND status='active'
            AND (progress_pct IS NULL OR progress_pct <= $2 OR $2=100)
          -- NO SELECT...FOR UPDATE, NO job_events row
                        │
                        ▼
        GET /v1/jobs/{id} (handleGetJob) reads jobs row
          → JSON: "progress_pct": <int|null>, "eta_seconds": <int|null>
```

### Recommended Project Structure

No new packages/directories. New files fit the existing per-responsibility convention:

```
internal/convert/
├── exec.go              # unchanged (runCommand stays byte-for-byte identical)
├── progressexec.go       # NEW: runCommandStreaming + fd-3 wiring (mirrors exec.go's doc-comment style)
├── progress.go            # NEW: ProgressConverter interface, ProgressUpdate struct, block parser, EMA
├── av.go                 # MOD: convertTranscode calls runCommandStreaming when a sink is provided
└── progressexec_test.go   # NEW: unit + live SIGKILL-mid-stream test (own file, own gated verification)

internal/jobs/
├── repo.go                # MOD: + UpdateProgress (non-locking, self-conditioning)
└── jobs.go                # MOD: + ProgressPct/EtaSeconds/SourceDurationSeconds/ProgressUpdatedAt on Job

internal/worker/
└── worker.go               # MOD: type-assert ProgressConverter in process(); throttle wrapper

internal/api/
└── handlers.go              # MOD: handleGetJob surfaces progress_pct/eta_seconds (null-aware)

scripts/
└── av-progress-liveproof.sh # NEW: mirrors cancel-liveproof.sh — live SIGKILL-mid-progress proof
                              #      against the real av-worker container + go_goroutines metric delta
```

### Pattern 1: `runCommandStreaming` — fd-3 progress pipe, Setpgid+SIGKILL contract preserved

**What:** A new sibling to `runCommand` that adds a THIRD file descriptor (fd 3, via `cmd.ExtraFiles`) carrying ffmpeg's `-progress` stream, while stdout/stderr capture and the Setpgid+SIGKILL kill discipline stay identical to `runCommand`.

**When to use:** Only for the `AVConverter.convertTranscode` ffmpeg invocation (D-02) — the one ffmpeg call site whose duration is long enough that a monotonic ramp is worth reporting. Every other ffmpeg call site in `av.go` (`convertAudioExtract`, `convertThumbnail`, the stream-copy remux path) and every other engine (`libvips`, LibreOffice, Chromium, whisper-cli) keeps calling plain `runCommand`, completely untouched.

**Example (live-verified this session — see Code Examples for the exact test run/output):**
```go
// Source: adapted from .planning/research/STACK.md's worked example,
// live-verified against the pinned ffmpeg 8.1.2 binary in this research
// session (5x SIGKILL-mid-stream cycles: goroutine delta=0, zero orphaned
// processes, progress lines captured on every iteration before kill).
func runCommandStreaming(ctx context.Context, onLine func(string), name string, args ...string) ([]byte, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("progress pipe: %w", err)
	}
	cmd := exec.Command(name, args...) // args MUST include "-progress", "pipe:3"
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // IDENTICAL to runCommand
	cmd.ExtraFiles = []*os.File{w}                        // becomes fd 3 in the child

	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr // untouched, same diagnostics contract as runCommand

	if err := cmd.Start(); err != nil {
		w.Close()
		r.Close()
		return nil, fmt.Errorf("start %s: %w", name, err)
	}
	w.Close() // CRITICAL: parent's copy of the write end. Forgetting this is THE
	          // single most common bug in this pattern — the reader would never
	          // see EOF even after the child dies, because the parent itself
	          // still holds the fd open.

	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)
		defer r.Close()
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			onLine(sc.Text())
		}
	}()

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // identical to runCommand
		<-done     // reap, same as runCommand
		<-scanDone // NEW: join the scanner goroutine — the killed group closes fd 3,
		           // which unblocks Scan() with EOF on its own; this join is what
		           // makes "zero leaked goroutines" true rather than merely likely
		return nil, fmt.Errorf("%s killed: %w", name, ctx.Err())
	case err := <-done:
		<-scanDone // ffmpeg exiting closes fd 3 — wait for the scanner to notice
		           // BEFORE returning, so the caller's terminal write never races
		           // a still-in-flight progress line
		if err != nil {
			return stdout.Bytes(), fmt.Errorf("%s failed: %w: %s", name, err, stderr.String())
		}
		return stdout.Bytes(), nil
	}
}
```

**Why this does not break the Setpgid+SIGKILL contract:** `cmd.SysProcAttr.Setpgid` controls the *process group*; `cmd.Stdout`/`cmd.Stderr`/`cmd.ExtraFiles` are unrelated fields controlling *fd wiring*. `syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)` still targets the entire group — fd 3 dies with it, and a goroutine blocked on `Read()` observes EOF exactly like stdout/stderr already do. `[VERIFIED: live test, this session — see Code Examples]`

### Pattern 2: Optional-capability interface, type-asserted by the worker (D-01)

**What:** A new interface, additive alongside `Converter` (not a change to its 3-method shape), implemented ONLY by `AVConverter`.

**When to use:** The worker's `process()` function, right after `registry.Lookup`.

```go
// Source: internal/convert/convert.go pattern (Converter/Registry), extended
// per 41-CONTEXT.md D-01. Concrete name/signature is Claude's Discretion.
type ProgressUpdate struct {
	Pct   int     // 0-100, monotonic within a job
	ETA   *int    // seconds, nil until derivable (warm-up, D-04)
}

// ProgressConverter is an OPTIONAL capability a Converter may additionally
// implement. Converters with no native progress signal (libvips, LibreOffice,
// Chromium, whisper-cli's transcribe stage) do NOT implement this — the
// worker type-asserts and falls back to plain Convert (D-01).
type ProgressConverter interface {
	ConvertWithProgress(ctx context.Context, inPath, outPath string, opts map[string]any, onProgress func(ProgressUpdate)) error
}
```

```go
// internal/worker/worker.go process() — the ONLY call site of the type-assert.
conv, ok := h.registry.Lookup(job.SourceFormat, job.TargetFormat)
if !ok {
	return fmt.Errorf("no converter for %s -> %s", job.SourceFormat, job.TargetFormat)
}

convertFn := func() error { return conv.Convert(attemptCtx, inPath, outPath, job.Opts) }
if pc, ok := conv.(convert.ProgressConverter); ok {
	lastWrite, lastPct := time.Time{}, -1
	convertFn = func() error {
		return pc.ConvertWithProgress(attemptCtx, inPath, outPath, job.Opts, func(u convert.ProgressUpdate) {
			terminal := u.Pct >= 100
			if !terminal && time.Since(lastWrite) < progressMinInterval && u.Pct-lastPct < progressMinDeltaPct {
				return // throttled — D-03
			}
			lastWrite, lastPct = time.Now(), u.Pct
			_ = h.repo.UpdateProgress(attemptCtx, job.ID, u.Pct, u.ETA) // best-effort, mirrors webhook-enqueue discipline
		})
	}
}
```

**Rejected alternatives (per D-01):** a ctx-value sink (context-value anti-pattern for a non-request-scoped dependency); widening `Convert`'s signature for every engine (forces a no-op progress param onto 4 engines that can never call it).

### Pattern 3: Non-locking, self-conditioning progress write (D-03b)

**What:** `Repo.UpdateProgress` is a plain `UPDATE` — never `transition()`, never `SELECT...FOR UPDATE`, never a `job_events` insert.

```go
// Source: pattern from .planning/research/ARCHITECTURE.md Pattern 1,
// cross-checked against internal/jobs/repo.go's existing transition()
// shape (this is deliberately NOT built on top of it).
func (r *Repo) UpdateProgress(ctx context.Context, id uuid.UUID, pct int, eta *int) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE jobs
		SET progress_pct = $2, eta_seconds = $3, progress_updated_at = now()
		WHERE id = $1 AND status = 'active'
		  AND (progress_pct IS NULL OR progress_pct <= $2)`,
		id, pct, eta)
	if err != nil {
		return fmt.Errorf("update progress for job %s: %w", id, err)
	}
	return nil // affecting 0 rows (job no longer active, or a stale out-of-order
	           // tick) is NOT an error — self-conditioning by design
}
```

**Why `WHERE status = 'active'` is sufficient without a row lock:** this mirrors `MarkActive`'s own `COALESCE(started_at, now())` idempotent-re-entry idiom — "let the WHERE clause do the guarding" instead of a transaction. A late tick after `MarkDone`/`MarkFailed`/`Cancel` already flipped the row affects zero rows: no error, no stale resurrection of a lower percentage on a row that is actually terminal.

**Monotonic guard (recommended, addresses "Additional Context" point 5):** `AND (progress_pct IS NULL OR progress_pct <= $2)` closes a narrow race where an asynq internal retry starts a fresh attempt at a low percentage while a previous attempt's higher-percentage tick is still in flight. **Exception required for D-06 (freeze-on-abort):** this guard must NOT block the terminal 100% write, and must not block a legitimately-lower value written by a *new* attempt after a genuine restart — in practice the guard only matters within a single attempt's monotonic ramp, and D-06's "freeze last value on cancel/timeout" is satisfied by simply **not writing** anything further once `ConvertWithProgress` returns an error (the frozen value is whatever the last successful `UpdateProgress` call wrote — no explicit "freeze" logic is needed, it falls out naturally from "the throttled write loop stops calling `UpdateProgress` the moment the ffmpeg subprocess dies/errors").

**Concurrency vs. the guarded transition (row-lock contention question, addressed):** `transition()`'s `SELECT ... FOR UPDATE` inside `MarkActive`/`MarkDone`/`MarkFailed`/`Cancel` takes a row-level lock only for the duration of that one short transaction (single-digit milliseconds). A concurrent plain `UPDATE` (this progress path) targeting the *same row* would briefly block if it lands inside that narrow window, then proceeds normally once the transition transaction commits — this is standard Postgres MVCC row-level write-lock queuing, not deadlock risk (no lock is held open-endedly, and progress writes never take `FOR UPDATE` themselves so there's no lock-ordering inversion possible). Given `MarkActive`/`MarkDone`/`MarkFailed`/`Cancel` each fire once (or a handful of times) per job while progress writes fire every 2s+ across a multi-minute job, the actual collision window is negligible in practice — no measurement was performed to quantify this since it is a correctness/theory question, not a scale-sensitive one (single active attempt per job at a time, per the existing `asynq.Unique` lock).

### Pattern 4: Percent + ETA math

```go
// Source: derived from .planning/research/PITFALLS.md Pitfall 2's documented
// guidance + this session's live fixture capture.
func computeProgressPct(outTimeSeconds, sourceDurationSeconds float64) (pct int, ok bool) {
	if sourceDurationSeconds <= 0 {
		return 0, false // no denominator — PROG-02's null policy, never divide by zero/garbage
	}
	raw := outTimeSeconds / sourceDurationSeconds * 100
	if raw < 0 {
		raw = 0
	}
	if raw > 100 {
		raw = 100 // clamp: encoder lookahead/B-frame reorder can transiently overshoot
	}
	return int(raw), true
}

// EMA-smoothed speed for ETA (D-04). alpha and warmupSamples are Claude's
// Discretion (CONTEXT.md) — no measured production data exists yet to derive
// them from; the values below are a reasonable, explicitly-ASSUMED starting
// point, analogous to avDiskSafetyFactorDefault's own "[ASSUMED], no measured
// ratio existed" precedent (av.go:339-344).
const progressEMAAlpha = 0.3        // [ASSUMED] higher alpha = more responsive, less smooth
const progressWarmupSamples = 3     // [ASSUMED] ~a few seconds at ffmpeg's typical multi-Hz tick rate

type etaEstimator struct {
	emaSpeed float64
	samples  int
}

func (e *etaEstimator) update(speed float64) {
	if speed <= 0 {
		return // "N/A" or non-positive: do not pollute the EMA with garbage samples
	}
	if e.samples == 0 {
		e.emaSpeed = speed
	} else {
		e.emaSpeed = progressEMAAlpha*speed + (1-progressEMAAlpha)*e.emaSpeed
	}
	e.samples++
}

func (e *etaEstimator) etaSeconds(remainingSeconds float64) *int {
	if e.samples < progressWarmupSamples || e.emaSpeed <= 0 {
		return nil // D-04: null until warm-up completes
	}
	eta := int(remainingSeconds / e.emaSpeed)
	if eta < 0 {
		eta = 0
	}
	return &eta
}
```

### Anti-Patterns to Avoid

- **Modifying `runCommand` in place to add progress support:** breaks the byte-for-byte-unchanged invariant (D-01a) and risks regressing 4 other engines' already-hardened kill path for zero benefit to them.
- **Wiring `-progress` to stdout (`pipe:1`) and reusing `cmd.Stdout`:** conflates stdout's meaning conditionally per-invocation and could, in a future engine, collide with a legitimate machine-readable stdout use (veraPDF already does this elsewhere in the codebase).
- **Blocking `Read()` unconditionally with no join on the kill path:** the single most common bug in this pattern per `.planning/research/PITFALLS.md` Pitfall 3 — always `select` on both `ctx.Done()` and the scan-done signal, and always join the scanner goroutine before returning from either branch.
- **Routing progress writes through `transition()`:** would serialize every 2-second tick against real status changes and bloat `job_events` by orders of magnitude for a multi-minute job (explicitly rejected, D-03b).
- **Trusting `speed=` raw, unsmoothed, from the first tick:** early ticks report `N/A` or wildly noisy values (start-up bias) — always gate behind a warm-up window (D-04).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| ffmpeg progress parsing | A custom argv-building ffmpeg wrapper library | stdlib `os/exec` + the project's own existing hardened argv builders in `av.go` | No first-party Go ffmpeg wrapper exists; community wrappers (`u2takey/ffmpeg-go`) are just argv-builder convenience layers this project already has, hand-rolled and unit-tested, per `[CITED: .planning/research/STACK.md]` |
| Duration probing for the percent denominator | A second ffprobe subprocess inside this phase | The EXISTING `avProbeSource`/`src.duration` (av.go:609) | Already probed once per job under its own short timeout (WR-05 precedent) — PROG-02 only needs to persist the value that's already computed, per D-05 |
| Progress delivery to a live-updating client | A new worker→api Redis pub/sub progress channel | Postgres polling (this phase writes the row; Phase 42 polls it) | Matches the project's own stated architectural principle: "Redis/asynq holds only transient queue state... not consulted for job status" (`PROJECT.md` Data Flow) `[CITED: .planning/research/ARCHITECTURE.md]` |
| Progress-write rate limiting | A generic rate-limiter library | Two plain in-memory variables (`lastWrite time.Time`, `lastPct int`) checked before each write | The throttle is entirely single-goroutine, single-attempt scoped state — no concurrency, no library needed |

**Key insight:** every piece of this phase is already precedented somewhere in this codebase (hardened exec, guarded-vs-non-locking write split, `Set*`-at-startup config knobs, `avProbeSource`'s single-probe discipline) — the work is composition of existing patterns onto a new data source (ffmpeg's stdout-adjacent progress stream), not new infrastructure.

## Runtime State Inventory

Not applicable — this is a greenfield feature addition (new columns already exist from Phase 38, no rename/refactor/migration of existing state).

## Common Pitfalls

### Pitfall 1: `out_time_ms` is actually microseconds, not milliseconds
**What goes wrong:** Dividing by 1,000 instead of 1,000,000 inflates the computed percentage by 1000x.
**Why it happens:** The field name is a long-standing, never-fixed ffmpeg naming wart — the value in `out_time_ms` is identical to `out_time_us` (both microseconds).
**How to avoid:** Divide by 1,000,000. `[VERIFIED: live fixture capture, this session — out_time_ms=19933333 for a point 19.933333s into a 20s-duration synthetic clip, out_time_us reports the identical value]`
**Warning signs:** A short (few-second) test clip can visually "look plausible" even with the wrong divisor — the bug only becomes obvious against a longer clip. The parser test suite MUST include a multi-minute-scale fixture (duration can be faked/scaled in the test, doesn't need a real multi-minute encode).

### Pitfall 2: Early progress blocks report `N/A`, not a parseable number
**What goes wrong:** A naive parser that calls `strconv.ParseFloat` unconditionally panics or silently produces garbage on the first 1-2 blocks.
**Why it happens:** ffmpeg emits a progress block even before any frame has been encoded (warm-up), with `out_time_us=N/A`, `speed=N/A`, `bitrate=N/A`.
**How to avoid:** Check for the literal string `N/A` before attempting to parse each numeric field; treat as "no data yet" (skip the tick for percent/ETA purposes, but the block is still valid — `frame=0` etc. are present). `[VERIFIED: live fixture, this session — first block of a 20s test capture: `out_time_us=N/A`, `speed=N/A`]`

### Pitfall 3: Reader goroutine leak on SIGKILL if the parent's write-end fd isn't closed
**What goes wrong:** If the parent process keeps its own copy of the pipe's write-end `*os.File` open (forgetting `w.Close()` after `cmd.Start()`), the reader's `Scan()` never sees EOF even after the child (and its group) is SIGKILLed — because the write end is still technically open in the parent. This is a genuine, permanent-per-cancel goroutine leak, not a transient one.
**Why it happens:** Easy to omit because `w` still "looks used" (it was passed to `cmd.ExtraFiles`), and the code appears to work correctly in the non-cancel/success path (where `cmd.Wait()` internally closes the child's fds first, masking the bug) — the leak only manifests specifically on the SIGKILL/cancel path.
**How to avoid:** Call `w.Close()` immediately after `cmd.Start()` succeeds, unconditionally, before spawning the scanner goroutine. This is the single line in the code example above with a dedicated warning comment.
**Warning signs:** `runtime.NumGoroutine()` climbing monotonically across repeated cancel cycles in a soak test; a live-verified test asserting a goroutine-count delta of exactly 0 across N cancel cycles is the definitive check (demonstrated in Code Examples, `delta=0` across 5 iterations).

### Pitfall 4: Trusting `speed=` for ETA without a warm-up window
**What goes wrong:** `speed` is itself a coarse, ffmpeg-internal recent average, and swings hard in the first few seconds (start-up bias, `speed=N/A` on the very first ticks) and possibly near the end (flush/buffering behavior) — an ETA computed from the very first non-`N/A` sample can be wildly wrong.
**Why it happens:** The instinct is "as soon as I have ANY speed value, compute ETA" — but the first few values are the noisiest.
**How to avoid:** EMA with a warm-up window (D-04) — return `null` for `eta_seconds` until `progressWarmupSamples` real (non-`N/A`, positive) samples have been observed.
**Warning signs:** ETA that swings by more than 2x between consecutive polls in the first ~5-10 seconds of a transcode.

### Pitfall 5: Progress-write storm (no throttle)
**What goes wrong:** Writing to Postgres on every single `-progress` block (potentially many times per second) causes write amplification that scales with concurrent video jobs — already-proven KEDA multi-replica scale-out (Phase 28) makes this a real, not hypothetical, load pattern.
**Why it happens:** The naive "just wire the callback straight to the repo method" translation.
**How to avoid:** Dual-gate throttle (time AND delta, D-03) with an explicit terminal-write bypass (D-03a) — implemented in the worker's wrapper closure, not inside `runCommandStreaming` itself (keeps the exec layer free of business logic).
**Warning signs:** `pg_stat_statements` showing the progress-UPDATE as the dominant query by call count under any concurrent-job load test. `[CITED: .planning/research/PITFALLS.md Pitfall 1]`

## Code Examples

### Live-verified fixture: a real `n8.1.2`-equivalent `-progress` capture

```text
# Captured this session via:
#   ffmpeg -y -f lavfi -i testsrc=duration=20:size=1280x720:rate=30 \
#     -f lavfi -i sine=frequency=1000:duration=20 \
#     -c:v libx264 -preset veryslow -crf 18 -c:a aac \
#     -progress pipe:1 -nostats out2.mp4
# 15 total blocks emitted; first block (warm-up, "N/A" fields) and last
# block (progress=end, real numbers) shown below.

# --- FIRST BLOCK ---
frame=0
fps=0.00
stream_0_0_q=0.0
bitrate=N/A
total_size=48
out_time_us=N/A
out_time_ms=N/A
out_time=N/A
dup_frames=0
drop_frames=0
speed=N/A
progress=continue

# --- LAST BLOCK ---
frame=600
fps=80.99
stream_0_0_q=-1.0
bitrate= 198.2kbits/s
total_size=493876
out_time_us=19933333
out_time_ms=19933333
out_time=00:00:19.933333
dup_frames=0
drop_frames=0
speed=2.69x
progress=end
```

Confirms: (a) `out_time_us` and `out_time_ms` carry the IDENTICAL value (microseconds) despite the `_ms` name; (b) `progress=continue` on every intermediate block, `progress=end` on the final one — this is the authoritative block terminator, not a blank line; (c) `speed=` is formatted with a trailing `x` (e.g. `2.69x`) — the parser must strip it before `ParseFloat`; (d) `bitrate=` has a leading space when non-`N/A` (`" 198.2kbits/s"`) and a `kbits/s` suffix — irrelevant to this phase's percent/ETA math (only `out_time_us`/`out_time_ms`/`speed` are consumed) but worth noting for a parser that captures the full block.

### Live-verified fd-3 isolation from stdout

```bash
# This session: confirms `-progress pipe:3` writes EXCLUSIVELY to fd 3,
# never touching stdout — validating the "dedicated fd, not pipe:1" choice.
ffmpeg -y -f lavfi -i testsrc=duration=6:size=640x480:rate=25 \
  -f lavfi -i sine=frequency=1000:duration=6 \
  -c:v libx264 -preset veryslow -crf 20 -c:a aac \
  -progress pipe:3 -nostats out3.mp4 \
  3> progress_fd3.txt 2> stderr3.txt 1> stdout3.txt
# Result: stdout3.txt = 0 lines; progress_fd3.txt = 24 lines (full blocks)
```

### Live-verified SIGKILL-mid-stream: zero goroutine leak, zero orphaned process

```text
# This session: 5 iterations, each starting a real ~30s-duration ffmpeg
# transcode (1280x720, veryslow/crf12 to guarantee it's still running when
# killed) via the exact runCommandStreaming pattern above, cancelled via an
# 800ms context timeout on every iteration.

iter 0: err=ffmpeg killed: context deadline exceeded
iter 1: err=ffmpeg killed: context deadline exceeded
iter 2: err=ffmpeg killed: context deadline exceeded
iter 3: err=ffmpeg killed: context deadline exceeded
iter 4: err=ffmpeg killed: context deadline exceeded
lines seen total=60 goroutines before=1 after=1 (delta=0)

# `ps aux | grep ffmpeg` after the run: zero orphaned ffmpeg processes found.
```

This is the exact test shape the planner should require for PROG-05's live gate (mirrors `cancel-liveproof.sh`'s discipline): real subprocess, real SIGKILL, `runtime.NumGoroutine()` delta assertion across N cycles, and an OS-level process-table check — not `go test -race` alone.

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|---------------|--------|
| No progress signal at all (status-only polling) | ffmpeg `-progress` stream → denormalized Postgres columns → poll-based read | This phase (v1.9) | Clients can render a real 0→100 ramp + ETA for the one operation class (transcode) that actually benefits from it |

**Deprecated/outdated:** None — this is additive; no prior progress mechanism existed to deprecate.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `progressEMAAlpha = 0.3` and `progressWarmupSamples = 3` are reasonable starting defaults | Code Examples / Pattern 4 | No measured production ETA-accuracy data exists yet (mirrors `avDiskSafetyFactorDefault`'s own precedent of shipping an assumed default pending real measurement) — if wrong, ETA is merely noisier/smoother than ideal, not incorrect in direction; low risk, easily tuned later since these should be **compile-time constants, not env vars** unless the planner decides otherwise (CONTEXT.md's Claude's Discretion leaves this open) |
| A2 | The local Homebrew ffmpeg 8.1.2 build's `-progress` output format is representative of the project's own minimally-compiled `n8.1.2` av-worker binary (`--disable-everything` + narrow allowlist) | Summary, Standard Stack | The `-progress` reporting mechanism (`fftools/ffmpeg.c`'s progress-print logic) is core encoder-loop infrastructure, not a compile-time-optional feature gated by any of the `--enable-*` flags in `Dockerfile.av-worker` — very unlikely to differ, but the planner should still capture ONE real fixture from the actual `av-worker` Docker image as a committed test fixture rather than relying solely on this session's Homebrew capture, per this project's own "measure against the pinned build, not assumed" discipline (36-RESEARCH.md precedent) |
| A3 | `PROGRESS_MIN_INTERVAL_SECONDS`/`PROGRESS_MIN_DELTA_PCT` (or similarly-named) env knobs, defaulting to 2s/1%, are adequate starting points | Common Pitfalls / D-03 | CONTEXT.md D-03 explicitly says these are "measure-first discipline, mirrors the RTF/timeout env-knob precedent" — meaning the defaults should ideally be tuned against a live multi-job load test (REQUIREMENTS.md Notes section says the same) before being trusted as final; if wrong, worst case is either a mild write-amplification (too aggressive) or a choppier client-visible ramp (too conservative), neither of which is a correctness bug |

**None of the core mechanics (fd-3 wiring, out_time_ms units, progress=end terminator, N/A handling, SIGKILL-drain safety) are assumptions — all were live-verified in this session against a real ffmpeg 8.1.2 binary.**

## Open Questions

1. **Is the exact `n8.1.2` av-worker Docker image's `-progress` output byte-identical to this session's Homebrew capture?**
   - What we know: Both report `ffmpeg version 8.1.2`/`n8.1.2`; the `-progress` machine-readable output format is generic encoder-loop infrastructure, not gated by any of the narrow `--enable-*` flags the project's from-source build restricts.
   - What's unclear: Whether any obscure ffmpeg build-flag combination could alter field presence/ordering (unlikely, unverified against the literal container binary).
   - Recommendation: The planner's Wave 0 should include capturing ONE real fixture from `docker run octoconv-av-worker:dev ffmpeg -progress pipe:3 ...` (the actual pinned build already present locally per `docker images`) as the committed golden fixture for parser unit tests, superseding this session's Homebrew capture as the canonical source.

2. **Should the EMA alpha / warm-up window / throttle interval-delta be env-tunable constants or compile-time constants?**
   - What we know: CONTEXT.md explicitly requires `PROGRESS_*` env knobs for the throttle gates (D-03, locked). It is silent on whether the EMA alpha/warm-up are similarly env-tunable — these are listed under "Claude's Discretion."
   - What's unclear: Whether the planner should treat EMA tuning with the same `Set*`-at-startup config-knob ceremony (`SetAVDiskSafetyFactor` precedent) or just hardcode them as package constants (simpler, since there's no measured data to tune against yet, unlike the throttle gates which have an explicit load-test mandate in REQUIREMENTS.md Notes).
   - Recommendation: Hardcode EMA alpha/warm-up as package constants (like `avDefaultThumbnailTimecode`); reserve env-var ceremony for the two throttle gates that REQUIREMENTS.md explicitly calls out as needing "measured against a live multi-job load test."

3. **Exact interface/method naming (`ProgressConverter` vs `ProgressReporter`, `ConvertWithProgress` vs a separate sink parameter)?**
   - What we know: CONTEXT.md explicitly delegates this to Claude's Discretion. Prior research (`ARCHITECTURE.md`) used `ProgressReporter`/`ConvertWithProgress`; this document mirrors that naming for consistency with the already-written prior research, but it is not binding.
   - What's unclear: Nothing blocking — purely a naming choice.
   - Recommendation: Planner picks either name; recommend keeping `ProgressReporter`/`ConvertWithProgress` since it is what the pre-existing `.planning/research/` documents (already read by future agents via canonical_refs) already use, minimizing terminology drift across the phase's own paper trail.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| `ffmpeg`/`ffprobe` (local dev, Homebrew) | Capturing/verifying `-progress` fixtures during planning/implementation | ✓ | 8.1.2 (matches pinned `n8.1.2` version string) | — |
| `octoconv-av-worker` Docker image | Capturing the CANONICAL golden fixture from the exact pinned binary | ✓ | `octoconv-av-worker:dev` / `:latest` present locally (`docker images`) | Homebrew ffmpeg 8.1.2 is a valid interim stand-in (Open Question 1) |
| Go 1.26 toolchain | Building/testing `runCommandStreaming` | ✓ | go1.26.4 darwin/arm64 (project's pinned toolchain) | — |
| Prometheus `go_goroutines` metric (via `promauto`'s default registerer) | Live goroutine-leak gate for `av-progress-liveproof.sh` (mirrors `cancel-liveproof.sh`) | ✓ | Already registered by `internal/metrics/metrics.go`'s `promauto` usage — no new instrumentation needed | — |

**Missing dependencies with no fallback:** None.

**Missing dependencies with fallback:** None currently missing — both ffmpeg sources (Homebrew local + the built av-worker image) are available.

## Validation Architecture

> Note: `.planning/config.json` has `workflow.nyquist_validation: false` — this section is included per the instruction's stated default-inclusion rule for completeness, but per config it is not enforced as a gate.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` (project convention — no third-party assertion library) |
| Config file | none — plain `go test ./...` |
| Quick run command | `go test ./internal/convert/... ./internal/jobs/... ./internal/worker/...` |
| Full suite command | `go test ./...` |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| PROG-01 | `progress_pct` null for non-av-transcode engines; real ramp for transcode | unit | `go test ./internal/convert/... -run TestProgress -v` | ❌ Wave 0 (`progress_test.go`) |
| PROG-02 | `source_duration_seconds` persisted from existing probe | unit | `go test ./internal/jobs/... -run TestUpdateProgress -v` | ❌ Wave 0 (`repo_test.go` addition) |
| PROG-03 | Non-locking write, dual throttle, terminal bypass | unit | `go test ./internal/worker/... -run TestProgressThrottle -v` | ❌ Wave 0 |
| PROG-04 | `eta_seconds` EMA + warm-up, null before derivable | unit | `go test ./internal/convert/... -run TestETA -v` | ❌ Wave 0 |
| PROG-05 | Zero orphaned processes, zero leaked goroutines on SIGKILL mid-stream | unit (goroutine delta) + live (process table) | `go test ./internal/convert/... -run TestRunCommandStreaming -race -v` (unit) + `scripts/av-progress-liveproof.sh` (live, operator-run) | ❌ Wave 0 (both) |

### Sampling Rate
- **Per task commit:** `go test ./internal/convert/... ./internal/jobs/... ./internal/worker/...`
- **Per wave merge:** `go test ./...`
- **Phase gate:** Full suite green + `scripts/av-progress-liveproof.sh` operator-run PASS before `/gsd:verify-work` (mirrors Phase 39's `cancel-liveproof.sh` checkpoint:human-verify precedent, since PROG-05 explicitly requires live proof, not just `-race`)

### Wave 0 Gaps
- [ ] `internal/convert/progressexec_test.go` — unit tests for `runCommandStreaming` (success path, SIGKILL-mid-stream goroutine-delta assertion, argv hardening test mirroring `TestAVBuildersHardenEveryInvocation`'s style)
- [ ] `internal/convert/progress_test.go` — parser unit tests against a captured real fixture (normal, unknown-duration/`N/A`-heavy, non-monotonic, >100%-overshoot cases) — capture the canonical fixture from `octoconv-av-worker:dev` per Open Question 1, not hand-written
- [ ] `internal/jobs/repo_test.go` addition — `TestUpdateProgress` (self-conditioning WHERE clause, monotonic guard, no `job_events` row, no lock)
- [ ] `internal/worker/worker_test.go` addition — `TestProgressThrottle` (dual-gate: N ticks within time+delta window → exactly 1 write; terminal write always lands)
- [ ] `scripts/av-progress-liveproof.sh` — NEW, mirrors `scripts/cancel-liveproof.sh`'s structure: submit a real long transcode against the live docker-compose stack, capture the ffmpeg PID, cancel/timeout mid-stream, confirm PID gone from `/proc` AND `go_goroutines` metric delta ≈ 0 across repeated cycles
- [ ] Framework install: none — stdlib `testing` already covers everything

## Security Domain

`security_enforcement` is not present in `.planning/config.json` — treated as enabled per the instruction's default.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | Unchanged — this phase adds no new auth surface (progress is read via the existing authenticated `GET /v1/jobs/{id}`) |
| V3 Session Management | no | N/A — stateless API-key auth, unchanged |
| V4 Access Control | yes | Unchanged existing ownership guard (`job.ClientID != client.ID` → 404) already wraps `handleGetJob` — no new route added by this phase, so no new access-control surface to design |
| V5 Input Validation | yes | ffmpeg's own `-progress` output is TRUSTED (server-generated, not client input) — no new client-facing input surface in this phase; the only "input" is the already-validated `job.Opts` reused unchanged by `convertTranscode` |
| V6 Cryptography | no | N/A |
| V7 Error Handling / Logging | yes | Raw engine stderr must stay in `job_events.detail` only, never in the client-facing `progress_pct`/`eta_seconds` fields — trivially satisfied since progress values are plain integers with no text passthrough, but worth stating explicitly since PITFALLS.md flags this class of leak generally |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Cross-client progress leakage via `GET /v1/jobs/{id}` | Information Disclosure | Already mitigated by the existing ownership guard — this phase adds fields to an already-guarded response, introduces no new leak surface |
| Resource exhaustion via unthrottled progress writes (a form of self-inflicted DoS against Postgres) | Denial of Service | Dual-gate throttle (D-03), addressed at length above |
| Goroutine/fd exhaustion via a leaked reader on repeated cancel/timeout | Denial of Service | Live-verified fix (fd-3 + parent-close + scanner-join), addressed at length above |

## Sources

### Primary (HIGH confidence)
- Live command execution in this research session: `ffmpeg -version` (confirms local 8.1.2 matches pinned `n8.1.2`), captured `-progress` fixture (normal + fd-3-isolation runs), and a live Go SIGKILL-mid-stream harness (5 iterations, goroutine-delta + process-table verification)
- `internal/convert/exec.go`, `internal/convert/av.go`, `internal/convert/convert.go`, `internal/convert/avduration.go`, `internal/worker/worker.go`, `internal/jobs/repo.go`, `internal/jobs/jobs.go`, `internal/cancel/cancel.go`, `internal/cancel/pubsub.go`, `internal/api/handlers.go`, `internal/db/migrations/0007_job_progress.sql`, `Dockerfile.av-worker`, `.env.example`, `scripts/cancel-liveproof.sh`, `internal/metrics/metrics.go` — read directly from this repository
- `.planning/phases/41-progress-core/41-CONTEXT.md` — locked decisions D-01 through D-06

### Secondary (MEDIUM confidence)
- `.planning/research/STACK.md`, `.planning/research/ARCHITECTURE.md`, `.planning/research/PITFALLS.md`, `.planning/research/SUMMARY.md` (dated 2026-07-24, this project's own prior v1.9 milestone research) — treated as MEDIUM-HIGH since they were themselves grounded against the vendored `asynq`/`chi` source and this codebase, but their ffmpeg-specific claims (field names, `pipe:N` syntax) were independently re-verified live in this session rather than trusted at face value
- `.planning/STATE.md` Accumulated Context — pre-recorded Phase 41 decision notes, cross-checked against 41-CONTEXT.md's locked decisions (one minor discrepancy found and resolved: STATE.md's speculative note about `AudioConverter`'s ffmpeg-normalize stage also implementing progress is superseded by CONTEXT.md D-01's explicit "ONLY AVConverter" scoping)

### Tertiary (LOW confidence)
- None — every ffmpeg-specific claim in this document was either live-verified in this session or explicitly tagged `[ASSUMED]` in the Assumptions Log.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — zero new dependencies, stdlib-only, live-verified
- Architecture: HIGH — the fd-3/ExtraFiles pattern was live-tested end-to-end (spawn, capture, SIGKILL, drain, goroutine-count, process-table) in this session, not just read from documentation
- Pitfalls: HIGH for the exec/goroutine-leak/units pitfalls (all live-verified); MEDIUM for the EMA-alpha/throttle-default tuning specifics (explicitly flagged as assumptions pending real load-test data, consistent with the project's own "measure, don't assume" discipline)

**Research date:** 2026-07-25
**Valid until:** 30 days (stable domain — ffmpeg's `-progress` format and the project's own hardened exec pattern are not fast-moving; re-verify only if the pinned ffmpeg commit is ever bumped past `n8.1.2`)
