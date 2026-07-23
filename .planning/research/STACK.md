# Stack Research

**Domain:** Backend Go conversion service — job progress/observability/control (SSE push, ffmpeg progress parsing, in-flight task cancellation, paginated job listing) added on top of an already-hardened, production-validated stack.
**Researched:** 2026-07-24
**Confidence:** HIGH (all four sub-questions verified against locally vendored source of the exact pinned versions in `go.mod` — `asynq@v0.26.0`, `chi/v5@v5.3.0` — plus official ffmpeg docs and a live GitHub issue confirming a real asynq limitation)

This is a **replacement** stack note for OctoConv's v1.9 milestone. It supersedes the previous milestone's STACK.md content (v1.8 — ffmpeg/av engine packaging, already shipped and merged, not revisited here). Go 1.26 / chi v5 / asynq+Redis / pgx+PostgreSQL 18 / minio-go / the hardened `runCommand` exec contract / HMAC-signed webhooks are already validated (see `PROJECT.md`) and are not re-researched here. This note covers only what the four v1.9 features (progress %, ETA, live-push, job listing, cancel) need.

## Bottom Line

**Zero new Go dependencies are needed for any of the four v1.9 features.** stdlib (`net/http`'s `http.Flusher`, `os/exec`+`os.Pipe`+`bufio.Scanner`, `context`) plus the *already-present* `asynq` and `pgx/v5` cover every requirement. The only "stack changes" this milestone needs are (a) a Postgres migration adding progress/ETA columns (plain SQL, no library), and (b) new usage of an asynq capability (`Inspector.CancelProcessing` + `asynq.TaskID`) that already ships in the pinned `v0.26.0` but has an unfixed upstream retry-on-cancel race that changes how cancellation must be wired into the existing `isXTerminal`/`MarkActive` guarded-transition pattern.

## Recommended Stack

### Core Technologies (unchanged, validated — listed for grounding only)

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| Go stdlib `net/http` | Go 1.26.4 | SSE progress endpoint | `http.Flusher`/`http.ResponseController` is a complete, dependency-free SSE server implementation; chi v5.3.0's middleware chain (`middleware.Logger`/`RequestID`/`Recoverer`) is verified pass-through-compatible (see Version Compatibility) |
| Go stdlib `os/exec` + `os.Pipe` + `bufio.Scanner` | Go 1.26.4 | ffmpeg `-progress` parsing | Same primitives `internal/convert/exec.go`'s `runCommand` already uses (`exec.Command`, `cmd.SysProcAttr.Setpgid`); progress is just a second, additive output stream wired via `cmd.ExtraFiles`, not a replacement for the existing stdout/stderr capture |
| `github.com/hibiken/asynq` v0.26.0 (already a dependency) | v0.26.0 | In-flight task cancellation | `asynq.Inspector.CancelProcessing(taskID)` + `asynq.TaskID(id)` already exist in the pinned version — no upgrade, no new package. The feature is unused by the codebase today, that's all. |
| `github.com/jackc/pgx/v5` v5.10.0 (already a dependency) | v5.10.0 | `GET /v1/jobs` pagination | Plain parameterized SQL (`WHERE (created_at, id) < ($1,$2) ORDER BY created_at DESC, id DESC LIMIT $3`) executed through the existing pool — no pagination helper library exists or is needed in the Go ecosystem for this |

### Supporting Libraries

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| *(none)* | — | SSE | Never needed — see (a) below |
| *(none)* | — | ffmpeg progress parsing | Never needed — see (b) below |
| *(none)* | — | Cancellation transport | Never needed — asynq's own Redis pub/sub already does this, see (c) below |
| *(none)* | — | Pagination | Never needed — see (d) below |

### Development Tools

| Tool | Purpose | Notes |
|------|---------|-------|
| *(none new)* | — | Stdlib `testing` remains sufficient: SSE handlers are testable with `httptest.NewServer` + a real HTTP client reading the stream (an `httptest.ResponseRecorder` does NOT support `Flush`/streaming assertions — use a real listener, same pattern already needed for graceful-shutdown tests) |

## Installation

```bash
# No new dependencies. go.mod is unchanged by this milestone.
```

---

## (a) SSE over chi/net-http

**Verdict: pure stdlib `http.Flusher`. Do not add a library.**

- Verified locally (`$(go env GOMODCACHE)/github.com/go-chi/chi/v5@v5.3.0/middleware/wrap_writer.go`): chi's `middleware.Logger` (used in `internal/api/routes.go:31`) wraps `http.ResponseWriter` via `NewWrapResponseWriter`, which **type-switches on the underlying writer's capabilities** and returns a `flushWriter`/`httpFancyWriter`/`http2FancyWriter` that all satisfy `http.Flusher` and forward `Flush()` to the real writer (`var _ http.Flusher = &httpFancyWriter{}` etc.). SSE through the existing middleware stack (`RequestID`, `ClientIPFromRemoteAddr`, `Logger`, `Recoverer`) is safe.
- `internal/ratelimit/ratelimit.go` (`httprate.Limit`) and `internal/auth` are plain gate middleware — on allow they call `next.ServeHTTP(w, r)` directly with no response buffering, so they don't break streaming either. No code changes needed there.
- Pattern for the new `GET /v1/jobs/{id}/progress` (or similar) SSE endpoint:
  ```go
  func (s *Server) handleJobProgressStream(w http.ResponseWriter, r *http.Request) {
      rc := http.NewResponseController(w) // stdlib, Go 1.20+; preferred over a raw http.Flusher type-assert
      w.Header().Set("Content-Type", "text/event-stream")
      w.Header().Set("Cache-Control", "no-cache")
      w.Header().Set("Connection", "keep-alive")
      w.WriteHeader(http.StatusOK)

      ticker := time.NewTicker(1 * time.Second) // poll Postgres job_events/progress column
      defer ticker.Stop()
      heartbeat := time.NewTicker(15 * time.Second) // keep-alive comment so idle proxies don't time out
      defer heartbeat.Stop()

      for {
          select {
          case <-r.Context().Done():
              return // client disconnected (TCP close, client-side abort) — net/http cancels this automatically
          case <-ticker.C:
              // fmt.Fprintf(w, "event: progress\ndata: %s\n\n", payload); rc.Flush()
          case <-heartbeat.C:
              // fmt.Fprint(w, ": keep-alive\n\n"); rc.Flush() // SSE comment line, ignored by EventSource clients
          }
      }
  }
  ```
- **Client disconnect detection:** `r.Context()` is canceled by `net/http` itself when the underlying TCP connection closes — no polling or `CloseNotifier` (deprecated pre-1.11 API) needed. This is the same idiom already used for graceful shutdown elsewhere in the codebase (`cmd/api/main.go`), just observed per-request instead of per-process.
- **Keep-alive/heartbeat:** SSE has no protocol-level ping; the convention is a `:`-prefixed comment line flushed periodically (shown above) so intermediary proxies/load balancers with idle-connection timeouts don't kill a long-lived stream during a slow conversion. Recommend a heartbeat interval well under any reverse-proxy default (nginx default `proxy_read_timeout` is 60s — 15s is a safe margin; tune once a real proxy is in front of the API).
- **What NOT to add:** `github.com/r3labs/sse`, `github.com/tmaxmax/go-sse`, or similar are client+server SSE libraries aimed at multi-subscriber broadcast/fan-out topologies (pub/sub hubs, reconnection-id replay). OctoConv's need is a single unidirectional per-request stream tied to one job's Postgres row — introducing a hub abstraction would add a stateful in-process component this stateless-API architecture (`ARCHITECTURE.md`'s "Global state" note: `convert.Default` is the *only* intentional singleton) does not otherwise have, for a problem `http.Flusher` + a ticker already solves.

## (b) Parsing ffmpeg `-progress pipe:` output without breaking Setpgid+SIGKILL

**Verdict: stdlib `os/exec` + `os.Pipe` + `bufio.Scanner`, as an ADDITIVE second helper next to `runCommand`, not a modification of it.**

- ffmpeg's `-progress url` flag writes periodic `key=value\n` lines to the given target, terminated by a line `progress=continue` (mid-run) or `progress=end` (final), independent of and non-conflicting with the muxed output file argument. `pipe:1` = stdout, `pipe:2` = stderr, or any arbitrary fd via `pipe:N` + `cmd.ExtraFiles`. (Verified against ffmpeg's own documentation and community references.)
- **Why it does not break the Setpgid+SIGKILL contract (`internal/convert/exec.go:28-56`):** `cmd.SysProcAttr.Setpgid` controls the *process group* the child is placed in; `cmd.Stdout`/`cmd.Stderr`/`cmd.ExtraFiles` are unrelated `os.exec.Cmd` fields controlling *file descriptor wiring*. Attaching a progress pipe changes nothing about which process group `syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)` targets — the entire group, including whatever fd the progress data was flowing through, still dies atomically on `ctx.Done()`. A reader blocked on the pipe simply observes EOF/`io.ErrClosedPipe` when the group is killed, exactly like `stdout`/`stderr` do today.
- **Recommended shape** — a new sibling function, e.g. `runCommandWithProgress`, so every other engine's call to `runCommand` (libvips, LibreOffice, chromium, whisper.cpp, and ffmpeg's own non-progress invocations — audio-extract, thumbnail, stream-copy) is completely untouched:
  ```go
  func runCommandWithProgress(ctx context.Context, onLine func(line string), name string, args ...string) ([]byte, error) {
      r, w, err := os.Pipe()
      if err != nil {
          return nil, fmt.Errorf("progress pipe: %w", err)
      }
      cmd := exec.Command(name, args...) // args must include "-progress", "pipe:3"
      cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // unchanged from runCommand
      cmd.ExtraFiles = []*os.File{w}     // becomes fd 3 in the child
      var stdout, stderr bytes.Buffer
      cmd.Stdout, cmd.Stderr = &stdout, &stderr // untouched — same diagnostics capture as today

      if err := cmd.Start(); err != nil {
          w.Close(); r.Close()
          return nil, fmt.Errorf("start %s: %w", name, err)
      }
      w.Close() // parent's copy — only the child's dup keeps the pipe open; closing here is what
                 // makes the reader see EOF when ffmpeg exits (or process-group SIGKILL closes it)

      scanDone := make(chan struct{})
      go func() {
          defer close(scanDone)
          sc := bufio.NewScanner(r)
          for sc.Scan() {
              onLine(sc.Text()) // parse "key=value"; caller batches into a throttled Postgres write
          }
      }()

      done := make(chan error, 1)
      go func() { done <- cmd.Wait() }()

      select {
      case <-ctx.Done():
          _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // identical to runCommand
          <-done
          <-scanDone // drain: the killed group closes the pipe, scanner goroutine exits on its own
          return nil, fmt.Errorf("%s killed: %w", name, ctx.Err())
      case err := <-done:
          <-scanDone // ffmpeg closing fd 3 (or exiting) is what unblocks the scanner — wait for it
                      // BEFORE returning, so the caller never races a late progress line against
                      // its own MarkDone/webhook enqueue
          if err != nil {
              return stdout.Bytes(), fmt.Errorf("%s failed: %w: %s", name, err, stderr.String())
          }
          return stdout.Bytes(), nil
      }
  }
  ```
- **Why fd 3 (`pipe:3`) via `ExtraFiles`, not stdout (`pipe:1`):** ffmpeg's non-progress invocations in `internal/convert/av.go` don't use stdout for anything (muxed output goes to the `outPath` file argument, human logging goes to stderr), so `pipe:1` would technically work too — but reusing a *dedicated* fd keeps `runCommandWithProgress`'s stdout/stderr capture byte-for-byte identical to plain `runCommand`'s diagnostics contract (same `stderr.String()` folded into errors), rather than making stdout's meaning conditional on which ffmpeg invocation is running. This also sidesteps any future engine (veraPDF already uses stdout for its own machine-readable report, per `exec.go`'s doc comment) ever colliding with progress data on the same fd.
- **Throttling the Postgres write** (mentioned in `PROJECT.md`'s "троттлинг записи в Postgres"): do this in `onLine`'s caller, not in the pipe-reading loop — e.g. only write when `progress=continue`/`progress=end` appears (ffmpeg emits one full key=value block per output frame flush, already batched) and/or apply a `time.Since(lastWrite) > threshold` gate. No library needed; this is a few lines of stdlib `time`.
- **What NOT to add:** no ffmpeg-wrapper Go library (there is no first-party `ffmpeg-go`/`ffmpeg.wasm`-equivalent for server-side Go; community wrappers like `u2takey/ffmpeg-go` are argv-building convenience layers over `os/exec` — OctoConv already has its own hardened, tested argv builders in `internal/convert/av.go` and gains nothing from a wrapper).
- **Indeterminate-progress engines (image/document/html, PROJECT.md's "indeterminate-фолбэк для остальных движков"):** these have no `-progress`-equivalent signal at all (libvips/LibreOffice/chromium invocations are single-shot, no incremental output) — no stdlib or third-party trick substitutes for a signal the underlying tool doesn't emit. The only honest options are a status-only fallback (`queued`/`active`/`done` with no percentage) or a coarse heuristic (e.g. elapsed-time-vs-`*_ENGINE_TIMEOUT` ratio), neither of which is a stack/library question — it's a product decision belonging in FEATURES.md/roadmap, not here.

## (c) In-flight cancellation of an asynq task handler

**Verdict: use asynq's OWN existing Redis-pub/sub cancellation mechanism (`Inspector.CancelProcessing` + `asynq.TaskID`), already present in the pinned `v0.26.0` — do not build custom Redis pub/sub or a DB-polled cancel flag. But its interaction with retry has a real, confirmed upstream gap that must be designed around using the Postgres guarded-transition pattern the codebase already has.**

**How it works (verified by reading the vendored source at `$(go env GOMODCACHE)/github.com/hibiken/asynq@v0.26.0`):**
1. `asynq.Client.Enqueue` assigns each task a random UUID task ID unless the caller passes `asynq.TaskID(id)` (`client.go:116`, `composeOptions` default `taskID: uuid.NewString()`). Recommend passing `asynq.TaskID(jobID.String())` in every `NewXConvertTask` builder in `internal/queue/queue.go` — `jobID` is already a `uuid.UUID`, so this is a one-line addition per builder, needs no new Postgres column to look up "which asynq task belongs to this job," and coexists with the existing `asynq.Unique(uniqueTTL)` lock (`TaskID` and `Unique` are independent `option` fields — `client.go:230-238`; conflicts surface as the distinct `asynq.ErrTaskIDConflict` vs. today's `asynq.ErrDuplicateTask`, both effectively "already enqueued" for the reconciler's purposes, but worth handling explicitly if the reconciler's existing `errors.Is(err, asynq.ErrDuplicateTask)` check is reused for the cancel path too).
2. Each running `asynq.Server` process subscribes to a Redis pub/sub cancelation channel (`subscriber.go`) and maintains a per-process map of `taskID -> context.CancelFunc` for whatever it's *currently* executing (`processor.go:200-209`, `p.cancelations.Add(msg.ID, cancel)`).
3. `Inspector.CancelProcessing(id)` (`inspector.go:760-765`) just calls `rdb.PublishCancelation(id)` — a broadcast Redis PUBLISH. Every worker process across every engine class receives it; only the one actually running that task ID has an entry in its `cancelations` map and acts (`subscriber.go:83`, `cancel, ok := s.cancelations.Get(msg.Payload)`). This means the API server never needs to know *which* av-worker replica is running a given job — call `Inspector.CancelProcessing(jobID.String())` once, and it reaches the right process automatically.
4. Cancelling the ctx propagates down through `context.WithTimeout` exactly as a deadline expiry does — `internal/worker/worker.go:1052`'s `attemptCtx` (derived from the asynq-supplied `ctx`) is itself derived-and-cancelled, so `internal/convert/exec.go`'s `runCommand` select on `<-ctx.Done()` fires and kills the process group via the existing SIGKILL path with **zero changes to `exec.go`**. This is the single most useful fact for this feature: cancellation-as-a-signal is already fully supported by the existing hardened-exec contract because it was written to react to *any* context cancellation, not just timeouts.

**The gap that must be designed around (confirmed via a live, still-open GitHub issue against the pinned version, hibiken/asynq#987, "Task always retry when canceled"):** `processor.go`'s `exec()` select races `<-ctx.Done()` against `resErr := <-resCh` (the handler's own return value). On an explicit `CancelProcessing` call, `ctx.Done()` fires immediately and — per the reported and unresolved behavior — **wins that race before the handler's goroutine can return**, so asynq calls `p.handleFailedMessage(ctx, lease, msg, ctx.Err())` directly. This means:
- A `Handle*Convert` function's own attempt to detect `errors.Is(err, context.Canceled)` and return `fmt.Errorf("%w: ...", asynq.SkipRetry)` **may never be consulted** — asynq can retry a canceled task exactly like a transient failure, under the queue's normal `*RetryDelayFunc`/`*MaxRetry` budget, regardless of what the handler returns.
- **Do not rely on the handler's return value to stop retries of a canceled job.** Instead, reuse the pattern the codebase *already anticipates*: `internal/worker/worker.go:454-457` and its siblings already comment `// Already active/done/canceled — let asynq drop it rather than loop` on `MarkActive`'s guarded-transition failure, and `jobs.StatusCanceled` is **already a defined Go constant** (`internal/jobs/jobs.go:19`) and **already a valid value in the `jobs.status` CHECK constraint** (`internal/db/migrations/0001_init.sql:50`, `CHECK (status IN (..., 'canceled'))`) — it has simply never been wired to a `MarkCanceled` transition or an API path. The correct v1.9 design is:
  1. `POST /v1/jobs/{id}/cancel` performs a guarded Postgres transition (new `Repo.MarkCanceled`, mirroring `MarkFailed`'s shape) from `queued`/`active` to `canceled`, then calls `Inspector.CancelProcessing(jobID.String())` best-effort.
  2. If asynq nonetheless retries the (now-canceled) task, `MarkActive`'s existing allow-list check rejects the transition (job is no longer in an activatable source status) exactly as it already does for a done/failed job today, and the handler returns the existing `SkipRetry`-wrapped error at `worker.go:456` — asynq drops it. **No new retry-suppression logic is needed**; the guarded-transition pattern that already exists is sufficient, and it does not depend on winning asynq's internal race.
  3. `Inspector.CancelProcessing`'s ctx-cancellation is therefore used ONLY to make the *currently running* attempt stop promptly (kill the ffmpeg/whisper-cli/LibreOffice/chromium process group quickly, freeing the worker slot) — not as the mechanism that prevents a second attempt. That job is the Postgres transition's, which the codebase already does reliably elsewhere (RECON-04/RECON-05 precedent).
- A `queued` (not yet dequeued) job cannot be reached by `CancelProcessing` at all (nothing is executing it yet, so no process has it in its `cancelations` map) — the `MarkCanceled` Postgres transition alone is sufficient there: when the worker eventually dequeues it, `MarkActive` rejects the already-canceled job and it's dropped, same as above. No `Inspector.DeleteTask`/queue-surgery is needed.
- **No upgrade needed:** `v0.26.0` (released 2026-02-03) is confirmed the latest tagged release (checked against `CHANGELOG.md`); issue #987 is unresolved in it and in every later commit as of this research. There is no newer version to pin to that closes this gap — the guarded-transition workaround above is required regardless of version.

**What NOT to add:** a custom Redis pub/sub channel or a DB-polled "cancel flag" column that the worker checks in a loop — both would duplicate a mechanism asynq already ships (steps 1-3 above), adding a second signaling path to keep consistent with the first for no benefit. The one thing actually missing is application logic (`Repo.MarkCanceled` + wiring `Inspector.CancelProcessing` into the cancel handler + setting `asynq.TaskID`), not a library.

## (d) `GET /v1/jobs` pagination — keyset vs. offset with pgx

**Verdict: keyset (cursor) pagination on `(created_at, id)`, executed as plain parameterized SQL through the existing `pgxpool`. No pagination library exists or is needed in the Go/pgx ecosystem for this.**

- **Why keyset over offset:** `jobs` is an append-heavy, high-write table (every conversion request inserts a row — 6 engine classes now write to it). Offset pagination (`OFFSET n LIMIT m`) degrades in two ways that matter here: (1) cost is `O(n)` — Postgres must still scan and discard the first `n` rows on every deep page, and (2) correctness — a client paging through while new jobs are actively being inserted (the exact concurrent-write pattern this service has under load, per the KEDA load-proof evidence in `PROJECT.md`) sees **page drift**: rows shift between pages, causing skipped or duplicated jobs in the listing. Keyset pagination (`WHERE (created_at, id) < ($1, $2) ORDER BY created_at DESC, id DESC LIMIT $3`) is `O(log n)` via a supporting index and is stable under concurrent inserts — a new row never displaces an already-returned cursor position.
- **Index:** add a composite index scoped to the per-client "own jobs" access pattern already enforced elsewhere (`ARCHITECTURE.md`: cross-client access → 404, never leaking existence) — `CREATE INDEX ON jobs (client_id, created_at DESC, id DESC)`, optionally with `status` as a leading or included column if the filter-by-status requirement is common enough to warrant a partial/composite index; this is a plain migration file (mirrors `0002`-`0006` in `internal/db/migrations/`), not a library concern.
- **Cursor encoding:** an opaque, base64-encoded `(created_at, id)` pair using stdlib `encoding/json` + `encoding/base64` — matches the "no config-file library, minimal deps" discipline already followed elsewhere (`.env.example`-only config). Do not expose the raw `created_at`/`id` as separate query parameters the client is expected to round-trip verbatim; an opaque cursor string is the conventional REST-API contract and prevents a client from constructing an arbitrary/malformed WHERE-tuple.
- **Filter/status combined with keyset:** filters (e.g. `status=done`) become additional `WHERE` predicates ANDed with the keyset predicate — no special handling needed as long as the supporting index's column order matches the actual filter+sort usage (`client_id` always present via auth, then whatever's filtered, then `created_at, id` for the cursor comparison and sort).
- **What NOT to use:** `OFFSET`/`LIMIT` for this endpoint (correctness issue above, not just a performance nicety — an internal caller may build a UI or monitoring tool against `GET /v1/jobs` and rely on it not silently dropping rows under load); no ORM-style Go pagination helper library (e.g. generic "paginator" packages) — they add an abstraction layer for a two-column keyset query the project's own repository pattern (`internal/jobs/repo.go`) already expresses more directly and testably as one more method alongside `Get`/`Inputs`/`Outputs`.

---

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|-------------|-------------|-------------------------|
| stdlib `http.Flusher`/`http.ResponseController` for SSE | `github.com/r3labs/sse`, `github.com/tmaxmax/go-sse` | If OctoConv ever needs a shared multi-subscriber broadcast hub (many clients watching one event stream, replay-by-`Last-Event-ID`) rather than one stream per in-flight job request — not the case here |
| stdlib `os.Pipe`+`bufio.Scanner` for ffmpeg progress | Named FIFO (`syscall.Mkfifo`) or a Unix domain socket for `-progress` | Only relevant if progress needs to survive/reattach across a worker-process restart mid-conversion — not a stated v1.9 requirement, and would reintroduce filesystem cleanup concerns `os.Pipe` avoids entirely |
| `asynq.Inspector.CancelProcessing` + Postgres guarded transition | Redis-native `PUBLISH`/`SUBSCRIBE` hand-rolled on top of the existing `REDIS_ADDR` connection | Never for this use case — it would duplicate a mechanism `asynq` already exposes through `Inspector`, adding a second consistency-sensitive signaling path for the same job |
| Keyset (`created_at, id`) pagination via plain pgx SQL | `OFFSET`/`LIMIT` | Only acceptable for a UI's "jump to page N" affordance where a client explicitly wants arbitrary random access and accepts approximate/racy results under concurrent writes — not appropriate as the primary contract for an internal jobs-listing API |

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| Any third-party SSE server library | Solves multi-subscriber fan-out; OctoConv needs one stream per request tied to one job row — added stateful complexity for no capability gained | stdlib `http.Flusher` + `time.Ticker` polling Postgres |
| A generic ffmpeg Go wrapper (e.g. `u2takey/ffmpeg-go`) | Just an argv-builder convenience layer; OctoConv already has hardened, unit-tested argv builders (`internal/convert/av.go`) and its own `runCommand` exec contract | Extend `internal/convert/exec.go` with an additive `runCommandWithProgress` |
| Custom Redis pub/sub or a DB-polled cancel-flag column | Duplicates `asynq.Inspector.CancelProcessing`, which already does exactly this over the same Redis instance `REDIS_ADDR` already points at | `asynq.TaskID` + `Inspector.CancelProcessing` + the existing `MarkActive` guarded-transition pattern |
| `OFFSET`/`LIMIT` pagination on `jobs` | `O(n)` cost and page drift under concurrent inserts (an append-heavy 6-engine-class table under real load) | Keyset pagination on `(created_at, id)` with a supporting composite index |
| Relying on a `Handle*Convert` handler's own `errors.Is(err, context.Canceled)` check to stop asynq retries | Confirmed unfixed upstream race (hibiken/asynq#987, present in the pinned v0.26.0): the processor's internal select can act on `ctx.Done()` before the handler's return value is even consulted, so `asynq.SkipRetry` may never take effect | Postgres-side guarded transition (`MarkCanceled` before calling `CancelProcessing`); let the existing `MarkActive` allow-list reject any subsequent retry, mirroring the pattern already used for done/failed jobs |

## Stack Patterns by Variant

**If the API is later horizontally scaled to multiple replicas (already flagged as a future concern in `internal/ratelimit/ratelimit.go`'s doc comment for `httprate`'s in-process counters):**
- SSE streams are per-connection and per-replica already — no change needed; a client's stream simply lives on whichever replica accepted the connection.
- `Inspector.CancelProcessing` is unaffected by API replica count — it's a Redis broadcast reaching whichever *worker* process (not API replica) is running the task, regardless of which API replica received the cancel HTTP request.

**If a progress webhook event is added alongside/instead of SSE (both are in scope per `PROJECT.md`: "progress webhook-событие и/или SSE"):**
- Reuse the existing `internal/webhook` HMAC-signed delivery infrastructure and `webhook_deliveries` audit trail exactly as `done`/`failed` events do today — a `progress` event is just a new `body["status"]`-equivalent payload shape, not a new delivery mechanism. Recommend throttling enqueue of `progress` webhook tasks more aggressively than the live SSE stream (e.g. only every N% or every M seconds) since each one is a full asynq task + HTTP POST + Postgres write, unlike an SSE tick which is comparatively cheap.

**If ffmpeg progress needs to feed BOTH the throttled Postgres write AND a live SSE stream simultaneously:**
- Keep a single `onLine` callback in `runCommandWithProgress` that updates one place (e.g. writes straight to Postgres on the throttled cadence); have the SSE handler poll that same source (Postgres row, most consistent with "Postgres is the system of record" — `internal/jobs/jobs.go:2-3`) rather than wiring a second, parallel fan-out path directly from the ffmpeg-reading goroutine. This keeps progress data flowing through the one write path the rest of the architecture already treats as the source of truth, instead of adding an in-memory pub/sub the API and worker processes would need to share (they're separate binaries/containers today — no shared memory exists between them).

## Version Compatibility

| Package A | Compatible With | Notes |
|-----------|-----------------|-------|
| `github.com/go-chi/chi/v5` v5.3.0 middleware chain (`RequestID`, `ClientIPFromRemoteAddr`, `Logger`, `Recoverer`) | stdlib `http.Flusher`/SSE | Verified via `middleware/wrap_writer.go` in the exact vendored v5.3.0: `NewWrapResponseWriter` preserves `Flush()` pass-through for every writer variant it can produce |
| `github.com/go-chi/httprate` v0.15.0 (`ByIP`/`PerClient` in `internal/ratelimit/ratelimit.go`) | SSE streaming responses | No response buffering on the allow path — calls `next.ServeHTTP` directly; confirmed by reading `internal/ratelimit/ratelimit.go` (project code, not the library itself, since the wrapping is entirely inside `httprate.Limit`'s returned middleware which is a standard allow/429 gate) |
| `github.com/hibiken/asynq` v0.26.0 | `Inspector.CancelProcessing` + `asynq.TaskID` | Both APIs exist in this exact pinned version (confirmed by reading `inspector.go`, `client.go`, `processor.go`, `subscriber.go` from the vendored module) — no upgrade required. v0.26.0 (2026-02-03) is also confirmed the current latest tagged release, so there is nothing newer to pin to that would close the retry-on-cancel race (hibiken/asynq#987) |
| `jobs.status` CHECK constraint (`internal/db/migrations/0001_init.sql:50`) | `jobs.StatusCanceled` Go constant (`internal/jobs/jobs.go:19`) | `'canceled'` is **already** an allowed DB value and an existing Go constant from Phase 1 — no schema migration is needed to introduce the status itself; only a new `Repo.MarkCanceled` guarded-transition method and the API route are missing |

## Sources

- `$(go env GOMODCACHE)/github.com/hibiken/asynq@v0.26.0/{inspector.go,client.go,processor.go,subscriber.go,internal/rdb/rdb.go}` — read directly, confirms `CancelProcessing`/`PublishCancelation`/`TaskID`/`ErrTaskIDConflict` implementation and the `cancelations`-map-per-process mechanism (HIGH confidence — primary source, exact pinned version)
- `$(go env GOMODCACHE)/github.com/hibiken/asynq@v0.26.0/CHANGELOG.md` (fetched via raw.githubusercontent.com) — confirms v0.26.0 (2026-02-03) is the latest tagged release and no cancellation/retry-race fix has landed through it (MEDIUM-HIGH — changelog is official but issue-tracker cross-check below raises confidence)
- [hibiken/asynq#987 "Task always retry when canceled"](https://github.com/hibiken/asynq/issues/987) — live, open issue confirming the `processor.go` `select` race between `ctx.Done()` and the handler's own return value on explicit cancellation (HIGH confidence — verified by cross-reading the exact `processor.go` select statement in the vendored source, which matches the issue's description line-for-line)
- [hibiken/asynq Wiki: Task Timeout and Cancelation](https://github.com/hibiken/asynq/wiki/Task-Timeout-and-Cancelation) — official usage guidance for `CancelProcessing`, cross-checked against source
- `$(go env GOMODCACHE)/github.com/go-chi/chi/v5@v5.3.0/middleware/wrap_writer.go` — read directly, confirms `http.Flusher` pass-through through every response-writer wrapper variant chi's middleware can produce (HIGH confidence — primary source, exact pinned version)
- `internal/api/routes.go`, `internal/ratelimit/ratelimit.go`, `internal/convert/exec.go`, `internal/worker/worker.go`, `internal/queue/queue.go`, `internal/queue/client.go`, `internal/jobs/jobs.go`, `internal/jobs/repo.go`, `internal/db/migrations/0001_init.sql`, `internal/convert/av.go`, `internal/convert/avduration.go` — read directly from this repository (HIGH confidence — ground truth for existing contracts this milestone must not break)
- ffmpeg `-progress` flag key=value/`pipe:N` format — verified via ffmpeg official docs/mailing-list references and cross-checked examples (MEDIUM-HIGH — well-established, widely documented behavior, no version-sensitivity found)

---
*Stack research for: OctoConv v1.9 "Job Progress & Control"*
*Researched: 2026-07-24*
