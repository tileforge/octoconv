---
phase: quick-261018-dlp
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - internal/api/api.go
  - internal/api/inspect_handlers.go
  - internal/api/inspectintake.go
  - internal/api/inspect_intake_test.go
  - internal/api/transport_cap_test.go
  - docs/openapi.yaml
  - .planning/quick/261018-bmr-helm-upload-cap-at-least-the-measured-ca/261018-bmr-PLAN.md
  - .planning/todos/pending/2026-10-18-create-intake-no-body-deadline.md
  - .planning/todos/pending/2026-10-18-inspect-post-intake-discard-holds-analysis-slot.md
  - .planning/STATE.md
  - .planning/quick/261018-dlp-inspect-intake-read-deadline-follows-a-m/evidence/*
  - .planning/quick/261018-dlp-inspect-intake-read-deadline-follows-a-m/261018-dlp-SUMMARY.md
autonomous: true
requirements: [quick-261018-dlp]

estimate:
  tokens: 130000
  raw_tokens: 130000
  tasks: 2
  confidence: low

must_haves:
  truths:
    - "Over a real connection (httptest server with ReadHeaderTimeout 10 s, the full srv.Routes() chain), a POST /v1/inspect body sent steadily above the minimum rate whose transfer lasts longer than the base window is answered 200; with the fixed deadline (the refactor commit, and mutation M1) the same request is answered 400 invalid_request \"invalid multipart form\""
    - "A body sent below the minimum rate is cut once the base window and the credit it earned are spent, and a client that sends no body bytes is cut at the base window; both get exactly today's answer, 400 invalid_request \"invalid multipart form\", and neither is cut before the base window"
    - "The read deadline is intake start + base window + min(bytes Read returned, INSPECT_MAX_UPLOAD_BYTES) / minimum rate; it is set once before the first Read, moves only forward, is credited only for bytes a Read call returned (not bytes requested), and never exceeds start + window + cap/rate, which with the production constants (30 s, 1 MiB/s) and the chart's 191889408-byte cap is exactly 213 s"
    - "The largest admissible STL inside the largest bounded request, at the chart's own caps, streamed over a real connection at a 20 Mbit/s-equivalent uplink with time scaled down by 10, is answered 200 with the cad render png offer available; with the fixed deadline (M1) it is cut"
    - "docs/openapi.yaml's POST /v1/inspect 400 description states the rule, and a test builds the stated numbers from the constants; the create path's missing body deadline and the post-intake discard hold are pending todos listed in STATE.md; the 261018-bmr PLAN's 6.1 MiB/s note points at this task"
    - "internal/convert/exec.go is byte-identical to main; internal/api/handlers.go and cmd/api/main.go are unchanged from 7489c7b; no file gains a comment line, and the two comment fragments this change makes false are removed; gofmt, go vet ./... and the full go test ./... -count=1 pass"
  artifacts:
    - path: internal/api/inspectintake.go
      provides: "inspectIntakeReader: wraps the size-bounded body, credits bytes returned by Read, and moves the connection read deadline to start + window + min(received, limit)/minRate after every non-empty Read"
      contains: "inspectIntakeReader"
    - path: internal/api/inspect_intake_test.go
      provides: "real-connection tests for steady, slow and stalled clients; unit tests for the bound and the returned-bytes credit; the openapi rule test"
      contains: "TestInspectIntakeAdmitsASteadyBodyThatOutlastsTheWindow"
    - path: internal/api/transport_cap_test.go
      provides: "TestChartTransportCapsAdmitTheLargestCeilingFileOverAPacedConnection, the real-connection variant of the 261018-bmr tracer"
      contains: "OverAPacedConnection"
    - path: .planning/todos/pending/2026-10-18-create-intake-no-body-deadline.md
      provides: "follow-up: POST /v1/jobs has no body read deadline and no intake bound"
    - path: .planning/todos/pending/2026-10-18-inspect-post-intake-discard-holds-analysis-slot.md
      provides: "follow-up: net/http's post-handler discard read runs under the intake deadline while the analysis slot is held"
  key_links:
    - from: "handleInspect"
      to: "inspectIntakeReader"
      via: "r.Body becomes an inspectIntakeReader around http.MaxBytesReader, so the bytes credited are the bytes the multipart parser receives"
      pattern: "inspectIntakeReader\\{"
    - from: "inspectIntakeReader.setDeadline"
      to: "http.ResponseController.SetReadDeadline"
      via: "the handler passes rc.SetReadDeadline; the real-connection stalled-client test proves it reaches the net.Conn through chi's wrapped ResponseWriter"
      pattern: "SetReadDeadline"
    - from: "NewServer"
      to: "inspectIntakeReadDeadline and inspectIntakeMinBytesPerSecond"
      via: "two unexported Server fields initialised from the constants; tests override them on the returned *Server before serving"
      pattern: "inspectIntakeMinRate:"
    - from: "TestInspectIntakeRuleIsStatedInTheOpenAPI"
      to: "docs/openapi.yaml paths./v1/inspect.post.responses.400.description"
      via: "the test parses the spec with go.yaml.in/yaml/v3 and requires sentences built from the two constants"
      pattern: "inspectIntakeMinBytesPerSecond"
---

<objective>
Codex P2 on PR #40 (branch `gsd/helm-upload-cap-cad-ceilings`; commit there, do not push). POST /v1/inspect reads its body under one absolute 30 s read deadline (`inspectIntakeReadDeadline`, set once before `ParseMultipartForm`). Since 261018-bmr, INSPECT_MAX_UPLOAD_BYTES is 191889408 (183 MiB) in every shipped configuration. Reading that in 30 s takes 6.1 MiB/s (51 Mbit/s). A valid 182 MiB STL over a 20 to 50 Mbit/s uplink is cut and answered 400 "invalid multipart form", although the config and the docs call it admissible. Every existing test went through httptest.ResponseRecorder, where the deadline is silently unsupported.

Goal, as the client sees it: an inspect upload the configuration admits is admitted over any uplink of at least 1 MiB/s (8 Mbit/s). A client that trickles below that rate, or stalls, still loses its intake slot exactly as it does today, with the same answer.

User decision (made): replace the fixed absolute deadline with a minimum-rate deadline. The deadline is intake start + base window (the current 30 s) + bytes received so far / minimum rate. A reader wrapping the body recomputes it after each Read. It only moves forward and is bounded by base + INSPECT_MAX_UPLOAD_BYTES / minRate. The minimum rate is 1 MiB/s, a Go constant.

Output: the reader and its wiring, real-connection tests (RED first), a real-connection variant of the 261018-bmr tracer, the openapi rule, two todos, the bmr note, STATE, SUMMARY.
</objective>

<decisions>
The plan implements the recommended option of each decision below. D5, D6 and D8 are flagged for orchestrator confirmation in the return message.

- **D1 (user, justified here): minimum rate 1 MiB/s (`inspectIntakeMinBytesPerSecond = 1 << 20`), a Go constant, not env-configurable.** These are the design notes on `inspectIntakeMaxConcurrent` (inspect_handlers.go:201-257).
  - The intake slot bounds heap: 8 slots x `multipartInMemoryBudget` 32 MiB = 256 MiB of the 1 GiB pod. Anything past 32 MiB spills to disk. That bound does not depend on time: a slot held for 213 s costs the same heap as one held for 30 s.
  - So the deadline exists for slot availability. The comment says the slot "is meant to tolerate genuinely slow network transfer, not an unbounded one". The minimum rate is the line between the two.
  - A stall (zero bandwidth) is still cut at 30 s, exactly as today.
  - Past the window, holding a slot costs the holder at least 1 MiB/s. Holding all 8 costs at least 8 MiB/s (64 Mbit/s) of sustained real upload.
  - Worst-case hold of one slot: 30 s + 191889408 B / 1048576 B/s = 30 s + 183 s = 213 s at the chart cap, or 30 s + 100 s = 130 s at the 100 MiB Go fallback.
  - 1 MiB/s is 2.5x below the slowest uplink Codex cites (20 Mbit/s = 2.38 MiB/s). There, a 182 MiB STL takes about 76 s, and its credit accrues 2.38x faster than time, so its deadline at the last byte is about 30 + 182 = 212 s.
  - The spill bound in the 2026-10-18 spill todo counts files (8 intake + 4 analysis), not seconds, so it is unchanged.
  - The constant follows the same rationale as `inspectIntakeMaxConcurrent`: it protects this deployment's slot and heap topology and is not a per-environment tuning knob. The code says nothing that contradicts this.
- **D2 (planner): the counting reader wraps the MaxBytesReader, not the raw body.** It credits exactly the bytes the multipart parser receives. MaxBytesReader never returns its overflow byte, and errors pass through unchanged, so `errors.As(parseErr, &maxErr)` still maps to 413. The credit is also clamped at `s.inspectMaxUploadBytes` inside the reader. This is defense in depth for the stated bound, and a unit test over an unbounded source kills its removal.
- **D3 (planner): test injection through two unexported Server fields,** `inspectIntakeWindow time.Duration` and `inspectIntakeMinRate int64`. NewServer sets them from the constants, and tests set them on the returned *Server before serving. No Config field is added, because a Config field invites env wiring (D1). This avoids package variables, which tests would have to mutate globally.
- **D4 (planner): the name `inspectIntakeReadDeadline` stays as the base window (30 s, value unchanged).** Its references in the comment at inspect_handlers.go:431 and in the spill todo's Constraints stay valid. Two comment fragments become false and are removed (no rewrite):
  - the opening sentence of the constant's comment. It says the constant bounds how long the parse may block, which is now window + credit.
  - the parenthetical at :434-435 that cites this package's tests as recorder-only, which stops being true once real-connection tests exist.
- **D5 (planner, confirm): no shipped doc states the 30 s rule.** I grepped docs/, .env.example, README.md, deploy/ and docker-compose.yml, so the literal instruction yields no edit. Recommended: state the rule once, in place, in the existing POST /v1/inspect 400 description of docs/openapi.yaml, the answer a cut client receives. A test builds the numbers from the constants. Drop option: skip the sentence and its test.
- **D6 (planner, confirm): the 6.1 MiB/s note is in 261018-bmr-PLAN.md (line 116, research; line 258, T-bmr-05), not in its SUMMARY,** which carries no deadline arithmetic. Recommended: append a pointer to those two lines in place (no new lines), and leave the SUMMARY untouched. Alternative: leave the PLAN as history and add a Residuals bullet to the bmr SUMMARY.
- **D7 (planner): the create path is recorded, not changed.** 260810-slf deliberately declined a body timeout for POST /v1/jobs. Its SUMMARY says: "No cmd/api/main.go body-read timeout was added: a server-wide http.Server.ReadTimeout would also apply to POST /v1/jobs' own legitimately large/slow uploads". Create is therefore unbounded by design, not too strict, and gets a pending todo.
- **D8 (planner finding, confirm todo-only): post-intake discard hold.**
  - What happens: when the response first flushes, net/http's writeHeader (go1.26.4 server.go:1392-1437) reads up to 256 KiB + 1 of any unread request body under the connection's current read deadline. Inspect responses above 2048 bytes (bufferBeforeChunkingSize) flush inside the handler while the `s.inspectSem` analysis slot is held. The planner measured responses of up to 3623 bytes in the api tests' Logger lines.
  - Who triggers it: a client that sends a valid form and then stalls with declared bytes left holds that slot until the intake deadline.
  - How long: at most 30 s from intake start today. After this change, at most start + window + received/minRate. The cheapest variant, a tiny body, stays at about 30 s; the longer hold requires sending the full cap first.
  - Status: pre-existing, analysed and not reproduced. Recommended: a pending todo, no fix here.
- **D9 (planner, rejected alternative): do not clear the read deadline after a successful parse.**
  - Why it was considered: clearing would remove a pre-existing edge. net/http's background read (server.go:701-777) starts at body EOF under the same deadline. If the deadline expires while the handler still runs, it cancels the request context, so PreAdmit verdicts become "unknown".
  - Why not: clearing makes D8's discard read unbounded.
  - What the new rule already does: any client that sent at or above the minimum rate throughout reaches EOF at least a full window (30 s) before its deadline. The edge is left only for a client that stalled most of the window and then burst. Recorded in the SUMMARY's Residuals.
</decisions>

<execution_context>
@~/.claude/gsd-core/workflows/execute-plan.md
@~/.claude/gsd-core/templates/summary.md
</execution_context>

<context>
@.planning/STATE.md
@.planning/quick/261018-bmr-helm-upload-cap-at-least-the-measured-ca/261018-bmr-SUMMARY.md
@internal/api/transport_cap_test.go
@internal/api/multipart_spill_test.go

Read with offsets (do not read whole):
- internal/api/inspect_handlers.go 200-285 and 379-470
- internal/api/api.go 224-256 and 636-672
- internal/api/inspect_handlers_test.go 1555-1600 (incompleteMultipartPrefix, blockingReader) and 1730-1790 (fakeContextAwarePreAdmitRepo, TestInspectPreAdmitGetsFreshContextAfterSlowBody)
- internal/api/handlers_test.go 410-440 (newFakeResolver, pngBytesFixture), 700-712 (multipartBody), 815-824 (authed, testClientKey = "testkey")
- internal/api/handlers.go 1472-1479 (writeJSON) and 1562-1564 (writeError)
- internal/api/openapi_paths_sync_test.go 30-35 (specPath)
- docs/openapi.yaml 1212-1300
- .planning/STATE.md 237-322

Session constants:
- `S=/private/tmp/claude-501/-Users-apaderin-dev-octoconv/95661ab2-afd1-49be-a390-64117af2c14a/scratchpad`. Use `TMPDIR=$S/qdlp` (create it) for every go test.
- Python 3.14 first on PATH for every go test: `PATH=$S/py314:$PATH`. `GOTOOLCHAIN=go1.26.4` for every go command.
- Pin guard before every write and every commit, from the repo root: `cd /Users/apaderin/dev/octoconv && bash $S/pin-guard.sh`. Halt on failure.
- `Q=.planning/quick/261018-dlp-inspect-intake-read-deadline-follows-a-m`, evidence under `$Q/evidence/`. `BASE=7489c7bfd255fba26453ef1d06eec9d48afb8932`; write it to `$Q/evidence/base.sha` first.

Research (planner, from the code at 7489c7b; stop and report if the executor's base disagrees):
- Intake today (inspect_handlers.go:423-460):
  - non-blocking acquire of `s.inspectIntakeSem` (8 slots);
  - `_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(inspectIntakeReadDeadline))` (:438);
  - `r.Body = http.MaxBytesReader(w, r.Body, s.inspectMaxUploadBytes)` (:440), then `ParseMultipartForm(multipartInMemoryBudget)` (:441);
  - the slot is released right after (:451);
  - `*http.MaxBytesError` maps to 413 payload_too_large "file exceeds size limit". Any other parse error, the deadline's i/o timeout included, maps to 400 invalid_request "invalid multipart form" (:452-460). writeError's body is the JSON object with keys `code` and `error`.
- net/http (go1.26.4):
  - With ReadHeaderTimeout set and ReadTimeout unset, the server clears the connection's read deadline after the headers, so the handler's SetReadDeadline is the only body deadline.
  - An expired deadline surfaces from r.Body.Read as a timeout error.
  - chi v5.3.0's middleware.Logger wrapper implements Unwrap, so ResponseController reaches the net.Conn. Only a real-connection test through srv.Routes() proves it.
- The multipart reader reads through a 4096-byte bufio.Reader. Each body Read requests at most about 4096 bytes and returns whatever has arrived, so a slow client's reads return far fewer bytes than requested (mutation M2's lever).
- The only real-connection precedent is multipart_spill_test.go:57 (`httptest.NewServer(srv.Routes())`). No existing test exercises the read deadline.
- Create path: handlers.go:146-149 calls MaxBytesReader and ParseMultipartForm with no deadline and no intake semaphore. cmd/api/main.go:304-308 sets only ReadHeaderTimeout 10 s. D7 has the 260810-slf rationale.
- Timing measured by the planner: the existing tracer's inspect of the 190840784-byte STL through ServeHTTP took 0.78 s, and 3.29 s under -race (about 58 MB/s). The paced variant below needs the server to sustain only 10.5 MB/s.
- Comment-line baselines (`grep -c '^[[:space:]]*//'`): inspect_handlers.go 1083, api.go 358, transport_cap_test.go 0. docs/openapi.yaml has 1 line matching `^[[:space:]]*#`. 261018-bmr-PLAN.md has 272 lines. STATE.md has 537 lines.

Design (implement exactly; names are binding because the tests use them):
- New file internal/api/inspectintake.go (package api, no comments) holds the type `inspectIntakeReader`. Its fields are `body io.ReadCloser`, `setDeadline func(time.Time) error`, `start time.Time`, `window time.Duration`, `minRate int64`, `limit int64` and `received int64`.
  - `Read(p)` calls body.Read(p). When n > 0 it adds n to received and calls setDeadline(deadline()), ignoring its error as today. It returns n and err unchanged.
  - `Close()` delegates to body.Close.
  - `deadline()` credits min(received, max(limit, 0)) and returns start + window + (credited / minRate) whole seconds + (credited % minRate) x 1 s / minRate. This split cannot overflow int64 for any cap below about 9 PB at 1 MiB/s.
- inspect_handlers.go: add `inspectIntakeMinBytesPerSecond int64 = 1 << 20` with no comment, in the const block right after `inspectIntakeReadDeadline`. In handleInspect, replace lines 438 and 440 as follows:
  1. rc := http.NewResponseController(w) and start := time.Now();
  2. `_ = rc.SetReadDeadline(start.Add(s.inspectIntakeWindow))`. This first deadline is the one that cuts a client that never sends a byte (mutation M4);
  3. r.Body = an inspectIntakeReader whose body is `http.MaxBytesReader(w, r.Body, s.inspectMaxUploadBytes)`, with setDeadline rc.SetReadDeadline, that start, window s.inspectIntakeWindow, minRate s.inspectIntakeMinRate and limit s.inspectMaxUploadBytes.
  Nothing else in the handler changes.
- api.go: add the fields `inspectIntakeWindow time.Duration` and `inspectIntakeMinRate int64` (no comments) after `inspectIntakeSem`. NewServer sets them to `inspectIntakeReadDeadline` and `inspectIntakeMinBytesPerSecond`. Let gofmt realign.

Rules for every task:
- No code comments in any language, and no new comment line in any file. The comment-line count of every edited file must not grow. An existing comment changes only by deleting the fragments D4 names.
- `internal/convert/exec.go` stays byte-identical to main. `internal/api/handlers.go` and `cmd/api/main.go` stay unchanged from BASE. Never `go clean -cache`.
- Never run docker, kubectl, helm install or any live stack.
- gofmt -l clean, `go vet ./...` clean.
- `.planning` paths need `git add -f`. Never add `.planning/config.json`, `.vscode/` or `octoconv.code-workspace`.
- Commits are SSH-signed. Check that `git log -1 --format='%G?'` prints G after each commit. Halt and report on a signing failure; never commit unsigned.
- No Co-Authored-By trailer. Do not push.
- Each mutation is applied to a saved copy and reverted. Afterwards the file must be `cmp`-identical to the copy.
</context>

<tasks>

<task type="tracer" tdd="true">
  <name>Task 1: The inspect intake deadline follows a minimum rate, proven on a real connection (refactor seam, RED, GREEN, mutations)</name>
  <files>internal/api/api.go, internal/api/inspect_handlers.go, internal/api/inspectintake.go, internal/api/inspect_intake_test.go, .planning/quick/261018-dlp-inspect-intake-read-deadline-follows-a-m/evidence/*</files>
  <precondition>`git rev-parse HEAD` is 7489c7bfd255fba26453ef1d06eec9d48afb8932 on branch gsd/helm-upload-cap-cad-ceilings. `git status --porcelain` lists only `.planning/config.json`, `.vscode/` and `octoconv.code-workspace`.</precondition>
  <read_first>
    - internal/api/inspect_handlers.go 200-285 and 379-470
    - internal/api/api.go 224-256 and 636-672
    - internal/api/multipart_spill_test.go (real-connection precedent)
    - internal/api/transport_cap_test.go 1-50 and 95-105 (chartLiteral, transportCapZeros)
    - internal/api/handlers_test.go 700-712 and 815-824
  </read_first>
  <behavior>
    - Refactor commit: the deadline goes through the reader seam with today's fixed rule, deadline() = start + window. The whole internal/api package passes unchanged.
    - RED, at the refactor commit: T1 fails with 400 invalid multipart form after about the window. T4 fails: the last deadline is start + window, not start + window + limit/rate. T5 fails: the deadlines do not advance per byte. T2 and T3 pass, because they guard behaviour that holds today too.
    - GREEN: T1 to T5 pass under -race, together with TestInspectIntakeConcurrencyIsBounded, TestInspectSlowBodyDoesNotHoldConcurrencySlot, TestInspectPreAdmitGetsFreshContextAfterSlowBody, TestMultipartSpillFilesAreRemovedWhenTheHandlerReturns and TestChartTransportCapsAdmitTheLargestCeilingFile. The whole internal/api package passes.
    - M1 (fixed deadline restored: deadline() returns start + window) is caught by T1, T4 and T5.
    - M2 (credit int64(len(p)) instead of n) is caught by T5, and also by T2: the credit inflates about 8x and the slow client is never cut.
    - M3 (no clamp) is caught by T4, in both halves.
    - M4 (the initial SetReadDeadline before ParseMultipartForm removed) is caught by T3: the stalled client is never cut, and the 15 s client safety deadline fails the test.
    - M5 (sliding idle deadline: deadline() measured from time.Now() instead of start) is caught by T2: the slow client is never cut and gets 200 after about 12 s.
  </behavior>
  <action>
Step 0. Run the pin guard. Write BASE to $Q/evidence/base.sha. Create $S/qdlp.

Step 1, refactor (behaviour unchanged). Implement the Design from the context with one difference: `deadline()` returns `start.Add(window)` only (today's fixed rule through the new seam). Add the constant and the two Server fields, and wire the handler as specified. Do not touch any comment yet. Run gofmt, go vet ./internal/api/, and the whole internal/api package with `-count=1`. Save the output to $Q/evidence/t1-refactor.txt. All must pass. Commit: refactor(quick-261018-dlp): the inspect intake deadline goes through a reader seam.

Step 2, RED. Create internal/api/inspect_intake_test.go (package api, no comments). Write these helpers:
- newIntakeServer(t, cfg Config, window time.Duration, minRate int64) returns (*Server, *httptest.Server).
  - It builds the server with NewServer, the fakes newSpillServer uses (fakeRepo, fakeStorage, fakeQueue, defaultFakePresetRepo, defaultFakePresetAdmin, fakeUsageRepo, fakeLimitsRepo, newFakeResolver, healthyDeps) and cfg.
  - It sets srv.inspectIntakeWindow and srv.inspectIntakeMinRate.
  - It creates httptest.NewUnstartedServer(srv.Routes()), sets its Config.ReadHeaderTimeout to 10 s as cmd/api/main.go does, starts it, and registers Close with t.Cleanup.
- pacedReader: fields r io.Reader, rate int64 (bytes per second) and chunk int, plus unexported start and sent.
  - On the first Read it records start.
  - It trims p to chunk and sleeps until start + sent x 1 s / rate.
  - It reads from r, adds n to sent, and returns.
  - Pacing is computed from start, so an oversleep is caught up rather than accumulated.
- rawInspect(t, ts, contentType string, contentLength int64, body io.Reader) returns (status int, payload map[string]string, elapsed time.Duration).
  - It dials ts.Listener.Addr() over tcp and sets a 15 s read deadline on the client conn as a hang guard.
  - It takes the start time and writes the request line POST /v1/inspect HTTP/1.1 with Host, Authorization "ApiKey " plus testClientKey, Content-Type and Content-Length headers.
  - A goroutine copies body into the conn, ignoring write errors, because the server cuts it mid-body. The goroutine closes a done channel.
  - The main goroutine reads the response with http.ReadResponse over a bufio.Reader and measures elapsed when that returns. A read error is t.Fatalf with elapsed.
  - It decodes the JSON body into payload, tolerating a non-JSON 200 by leaving payload nil, closes the conn, and waits for done.
- intakeBody(t, size) returns multipartBody(t, "in.bin", "", bytes.Repeat of 'a' size times). Arbitrary bytes are a normal 200 on inspect (D-03).

Tests:
- T1 TestInspectIntakeAdmitsASteadyBodyThatOutlastsTheWindow.
  - Setup: window 400 ms, minRate 32 KiB/s, Config{}, a 96 KiB intakeBody.
  - Pacing: a pacedReader at 64 KiB/s with 4 KiB chunks, about 1.5 s.
  - Asserts: status 200 and elapsed of at least 3 x window. The elapsed check is anti-vacuity: the transfer really outlasted the window.
- T2 TestInspectIntakeCutsABodyBelowTheMinimumRate.
  - Setup: the same server parameters and body.
  - Pacing: 8 KiB/s with 512-byte chunks; the whole body would take about 12 s.
  - Asserts: status 400, payload code invalid_request and error "invalid multipart form", elapsed at least window and below 3 s. The expected cut is about 0.55 s.
- T3 TestInspectIntakeCutsAStalledClientAtTheWindow.
  - Setup: window 400 ms, minRate 32 KiB/s, Content-Length 4096 and an empty body reader, so no body byte is ever sent.
  - Asserts: the same 400 code and error, and elapsed between window and window + 2 s.
- T4 TestInspectIntakeDeadlineIsBoundedByTheUploadCap, a unit test with no network.
  - Recording setter: a function that remembers the last value, the maximum and the call count, and fails the test if a value is earlier than the previous one. Use a fixed start of time.Unix(1700000000, 0).
  - Half (a): the reader has window 1 s, minRate 1000, limit 10, and a body of 25 zero bytes (io.NopCloser over bytes.NewReader). Read it in 3-byte buffers until io.EOF. The maximum and the last deadline must equal start + 1 s + 10 ms. Every value must be at or below that, and the setter must have been called 9 times, once per non-empty read.
  - Half (b): window inspectIntakeReadDeadline, minRate inspectIntakeMinBytesPerSecond, limit chartLiteral(t, "INSPECT_MAX_UPLOAD_BYTES"), and a body of io.LimitReader over transportCapZeros of limit + 1 MiB, drained with io.Copy into io.Discard. The maximum must equal start + window + (limit / minRate) seconds + the remainder term, derived in the test the same way. Log that value minus start as the worst-case intake slot hold at the chart cap; it reads 3m33s.
- T5 TestInspectIntakeCreditsBytesReturnedNotRequested, a unit test.
  - Setup: window 1 s, minRate 1000, limit 1 MiB, and a body of iotest.OneByteReader over "abcde". Read with a 4096-byte buffer until io.EOF.
  - Asserts: the setter received exactly 5 values, start + 1 s + k ms for k = 1..5, in order.

Run T1 to T5 at the refactor commit and save to $Q/evidence/t1-red.txt. The expected failures are in the behavior block. Stop and report if T2 or T3 fail there, or if T1, T4 or T5 pass. Commit the test file alone: test(quick-261018-dlp): the inspect intake must admit a steady body that outlasts the window.

Step 3, GREEN. Change deadline() to the Design's formula with the clamp. Then make the D4 comment deletions in inspect_handlers.go:
- In the comment block above inspectIntakeReadDeadline, delete its first sentence (lines 259-261 at BASE, from the constant's name through "bytes to arrive."). The block's first line then reads "cmd/api/main.go sets httpSrv.ReadHeaderTimeout" after the tab and slashes, and the following lines are unchanged.
- In the comment above the SetReadDeadline call (lines 431-437 at BASE), delete the parenthetical that begins "(e.g. this package" and ends "net.Conn)". Line 433 then ends "Ignored when unsupported -- the", and the two lines after it are unchanged.
Expected inspect_handlers.go comment-line count: 1079.

Run T1 to T5 and the regression tests from the behavior block with -race, then the whole internal/api package without -race. Save to $Q/evidence/t1-green.txt and include T4's logged worst-case hold.

Then run mutations M1 to M5 from the behavior block, one at a time, each against a saved copy of inspectintake.go or inspect_handlers.go. Run T1 to T5 for each, revert, and cmp against the copy. Save every output, with the mutation named in a header line, to $Q/evidence/t1-mutations.txt. Stop and report if any mutation survives.

Commit: fix(quick-261018-dlp): the inspect intake read deadline follows a minimum rate.
  </action>
  <verify>
    <automated>cd /Users/apaderin/dev/octoconv && S=/private/tmp/claude-501/-Users-apaderin-dev-octoconv/95661ab2-afd1-49be-a390-64117af2c14a/scratchpad && mkdir -p $S/qdlp && PATH=$S/py314:$PATH TMPDIR=$S/qdlp GOTOOLCHAIN=go1.26.4 go test ./internal/api/ -race -count=1 -run 'TestInspectIntake|TestInspectSlowBodyDoesNotHoldConcurrencySlot|TestInspectPreAdmitGetsFreshContextAfterSlowBody|TestMultipartSpillFilesAreRemovedWhenTheHandlerReturns' && FMT=$(GOTOOLCHAIN=go1.26.4 gofmt -l internal/api) && test -z "$FMT" && ! grep -qE '^[[:space:]]*//' internal/api/inspectintake.go internal/api/inspect_intake_test.go && ! grep -q 'inspectIntakeReadDeadline bounds how long' internal/api/inspect_handlers.go && ! grep -q 'exercise handleInspect through' internal/api/inspect_handlers.go && test "$(grep -c '^[[:space:]]*//' internal/api/inspect_handlers.go)" -le 1083 && test "$(grep -c '^[[:space:]]*//' internal/api/api.go)" -le 358 && git diff --quiet main -- internal/convert/exec.go && git diff --quiet 7489c7bfd255fba26453ef1d06eec9d48afb8932 -- internal/api/handlers.go cmd/api/main.go && SIG=$(git log -3 --format='%G?') && test "$(printf '%s' "$SIG" | tr -d '\n')" = GGG && echo T1-OK</automated>
  </verify>
  <done>t1-refactor.txt shows the whole api package green at the seam. t1-red.txt shows T1, T4 and T5 failing and T2 and T3 passing. t1-green.txt shows all five and the regression set green under -race, and the whole package green. t1-mutations.txt shows M1 to M5 each caught by the tests the behavior block names, with every file cmp-identical after revert. Three signed commits (refactor, test, fix). The comment-line rules hold.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: The 261018-bmr tracer over a paced real connection, the rule in the openapi, todos, bookkeeping and final gates</name>
  <files>internal/api/transport_cap_test.go, internal/api/inspect_intake_test.go, docs/openapi.yaml, .planning/quick/261018-bmr-helm-upload-cap-at-least-the-measured-ca/261018-bmr-PLAN.md, .planning/todos/pending/2026-10-18-create-intake-no-body-deadline.md, .planning/todos/pending/2026-10-18-inspect-post-intake-discard-holds-analysis-slot.md, .planning/STATE.md, .planning/quick/261018-dlp-inspect-intake-read-deadline-follows-a-m/evidence/*, .planning/quick/261018-dlp-inspect-intake-read-deadline-follows-a-m/261018-dlp-SUMMARY.md</files>
  <read_first>
    - internal/api/transport_cap_test.go 95-200 (largestAdmissibleSTL, writeTransportCapRequest, postLargestSTL, the tracer's offer assertions)
    - internal/api/inspect_intake_test.go (Task 1 helpers)
    - docs/openapi.yaml 1243-1260
    - .planning/todos/pending/2026-10-18-api-multipart-spill-unbounded-disk.md (todo format)
    - .planning/quick/261018-bmr-helm-upload-cap-at-least-the-measured-ca/261018-bmr-PLAN.md lines 116 and 258 only
    - .planning/STATE.md 237-322
  </read_first>
  <behavior>
    - The paced-connection variant passes at the Task 1 GREEN commit and fails under M1 (fixed deadline restored). Under M1 the upload is cut at about the scaled window of 3 s: a 400 or a transport error, either a test failure.
    - RED: TestInspectIntakeRuleIsStatedInTheOpenAPI fails before the openapi edit, naming the missing sentence. GREEN: it passes after.
    - Every shipped doc, comment and fixture text agrees with the code: no text states the fixed 30 s rule or the 6.1 MiB/s arithmetic as current.
  </behavior>
  <action>
Step 1, tests (no comments).

In internal/api/transport_cap_test.go add TestChartTransportCapsAdmitTheLargestCeilingFileOverAPacedConnection, skipped under testing.Short like the tracer.
- Server: newIntakeServer with Config{MaxUploadBytes: chart MAX, InspectMaxUploadBytes: chart INSPECT} (chartLiteral), window inspectIntakeReadDeadline / 10 and minRate inspectIntakeMinBytesPerSecond x 10. Time is scaled down by 10.
- Body: an io.Pipe, written by a goroutine with writeTransportCapRequest (engine cad, operation render, target png, the maximal ".stl" filename, largestAdmissibleSTL). The goroutine closes the pipe writer with the builder's error.
- Pacing: a pacedReader over the pipe reader at 25000000 bytes per second (20 Mbit/s x 10) with 256 KiB chunks.
- Request: POST through ts.Client() to ts.URL + "/v1/inspect", with the content type from mw.FormDataContentType() and authed.
- Asserts:
  - 200;
  - the cad render png offer has Available true and MaxBytes convert.CADMaxSTLBytes, decoded as the tracer does;
  - elapsed is greater than the scaled window (anti-vacuity; expected about 7.6 s).
- Log elapsed, the scaled window and rate, and the uplink they stand for (20 Mbit/s).
- Close the pipe reader after Do returns.

In internal/api/inspect_intake_test.go add TestInspectIntakeRuleIsStatedInTheOpenAPI.
- It reads specPath and unmarshals it with go.yaml.in/yaml/v3 into a map. It walks paths, "/v1/inspect", post, responses, "400", description, and fails closed with the path walked if any step is missing or not the expected type.
- It requires inspectIntakeReadDeadline to be a whole number of seconds and inspectIntakeMinBytesPerSecond a whole number of MiB.
- It requires the description to contain both sentences below, with W = the window in seconds and R = the rate in MiB:
  - "The body has W s, plus one second for every R MiB received so far, to arrive"
  - "a body that falls below R MiB/s once its first W s have passed is cut and answered with this 400 (invalid_request, invalid multipart form)"

Run both. Expected: the variant PASS and the openapi test FAIL. Save to $Q/evidence/t2-red.txt. Then apply M1 to inspectintake.go (deadline() returns start.Add(window)), run the variant, revert, and cmp. Save to $Q/evidence/t2-variant.txt together with the passing run. Stop and report if the variant passes under M1. Commit both tests: test(quick-261018-dlp): the largest-ceiling STL over a paced connection; the openapi states the intake rule.

Step 2, openapi (D5). In docs/openapi.yaml, the POST /v1/inspect "400" response, line 1254 at BASE, is the only line `description: Invalid multipart form / missing file.`. Make that description a `>-` folded scalar wrapped like its neighbours. Its text is the original sentence, then: "The body has 30 s, plus one second for every 1 MiB received so far, to arrive: a body that falls below 1 MiB/s once its first 30 s have passed is cut and answered with this 400 (invalid_request, invalid multipart form)." Touch no other line. The openapi test passes, and so do TestOpenAPIPathsMatchRegisteredRoutes and the responses sync test.

Step 3, the doc sweep. grep docs/, .env.example, README.md, deploy/ and docker-compose.yml, case-insensitively, for: read deadline, body deadline, 30 s or 30s near inspect, MiB/s, and inspectIntakeReadDeadline. Save the hits to $Q/evidence/doc-sweep.txt. Every hit that states the fixed rule as current is edited in place. The planner's sweep found none outside the bmr PLAN. Stop and report any hit that would need a new line.

Step 4, the bmr note (D6). Make two edits in 261018-bmr-PLAN.md, with no new lines and the line count still 272:
- Line 116: replace its final clause "; in-cluster clients are far above that." with "; Codex P2 on PR #40 showed 20 to 50 Mbit/s uplinks fall below it, and quick 261018-dlp replaced the fixed deadline with a minimum rate (1 MiB/s after a 30 s window, at most 213 s at 183 MiB)."
- Line 258, the T-bmr-05 mitigation cell: append " (premise refuted by Codex P2 on PR #40; superseded by quick 261018-dlp)" before the closing pipe.

Step 5, todos. Use the frontmatter shape of the spill todo: created 2026-10-18, area api, origin "quick task 261018-dlp research". Sections: What, Idea, Constraints. English, no code blocks.
- 2026-10-18-create-intake-no-body-deadline.md, severity medium.
  - What: POST /v1/jobs (handlers.go:146-149) reads its body with no read deadline and no intake semaphore. cmd/api/main.go sets only ReadHeaderTimeout 10 s.
    - A stalled create holds its goroutine, its connection, up to multipartInMemoryBudget of buffered file data and its spill file, indefinitely.
    - Only RATE_LIMIT_IP_RPM 60 and RATE_LIMIT_CLIENT_RPM 120 bound how many accumulate. That is the PR9R8-F1 heap arithmetic inspect closed with inspectIntakeMaxConcurrent: 60 stalled creates of 32 MiB is about 1.9 GiB against the 1 GiB api pod.
    - The same no-deadline condition makes net/http's post-handler discard read on create unbounded.
  - Idea: apply inspectIntakeReader to create with its own window and rate. The decision to make: the av ceiling of 2 GiB in compose gives a worst case of 30 + 2048 s at 1 MiB/s. Alternatively or additionally, add a create intake semaphore, which is the spill todo's second idea.
  - Constraints: 260810-slf deliberately declined a server-wide ReadTimeout because it would cut legitimately slow creates (quote the SUMMARY decision). A minimum-rate deadline does not have that problem, but its rate and window need a decision for create's larger caps.
- 2026-10-18-inspect-post-intake-discard-holds-analysis-slot.md, severity low. Write D8's analysis as stated in this plan's decisions:
  - the server.go lines;
  - the 2048-byte flush threshold;
  - the 3623-byte measured response;
  - the hold of at most 30 s before this task and at most start + window + received/minRate after it;
  - the cheapest variant unchanged;
  - the s.inspectSem slot held meanwhile.
  Mark it as analysed, not reproduced by a test. Idea: on a successful parse, set the read deadline to the earlier of the current deadline and now plus a short grace, so the discard read ends quickly. This needs a decision, because it shortens the D9 slack for the background read.

Step 6, STATE.md.
- In Pending Todos, change "Ровно десять" to "Ровно двенадцать". Add two bullets in Russian in the section's style, one per new todo, each ending with its "См. `.planning/todos/pending/...`" path.
- The list must match `ls .planning/todos/pending/`.
- At the end of the Quick Tasks Completed table, add the 261018-dlp row in Russian, in the style of the 261018-bmr row. It covers:
  - the minimum-rate rule (30 s + received / 1 MiB/s, bounded at 213 s at the chart cap);
  - RED on the seam (T1, T4, T5);
  - M1 to M5 caught;
  - the paced 182 MiB variant;
  - the openapi rule;
  - create and the discard hold left in todos.
  Its date is 2026-10-18. Its commit is the Step 1 test commit's short hash (the last commit before the SUMMARY commit, the same convention the bmr row uses). Its directory link is ./quick/261018-dlp-inspect-intake-read-deadline-follows-a-m/.

Step 7, final gates. Save everything to $Q/evidence/final-gates.txt:
- gofmt -l . is empty, and go vet ./... is clean;
- the -race run of the new tests and the regression set: the Task 1 regex plus TestInspectIntakeRuleIsStatedInTheOpenAPI, TestChartTransportCaps and TestOpenAPI;
- the full `go test ./... -count=1` with the session PATH and TMPDIR, which must exit 0;
- `git diff --quiet main -- internal/convert/exec.go`;
- `git diff --quiet $BASE -- internal/api/handlers.go cmd/api/main.go`;
- the comment-line counts of inspect_handlers.go (1079), api.go (358), transport_cap_test.go (0), the two new Go files (0) and docs/openapi.yaml '#' lines (1).

Step 8, the SUMMARY. Write $Q/261018-dlp-SUMMARY.md in English, in the shape of the 261018-bmr SUMMARY.
- Frontmatter: phase, plan, subsystem, tags, status, requires, provides, affects, key-files, decisions D1 to D9, metrics, actuals, plan_head_before (BASE) and plan_head_after (the Step 1 test commit), requirements-completed [quick-261018-dlp].
- Sections: Commits; RED and GREEN; Mutations (a table, M1 to M5 plus the variant's M1); Final gates; Residuals; Deviations from Plan; Known Stubs; Threat Flags; Self-Check.
- State the worst-case slot hold of 213 s, and 130 s at the 100 MiB fallback.
- Residuals must name:
  - D8;
  - D9's stalled-then-burst context-cancel edge (narrowed: steady clients at or above the rate keep at least 30 s of slack, where today a 183 MiB upload at 6.5 MiB/s had about 2 s);
  - the create todo;
  - that the openapi sentence is the first published timing rule of this API.

Run the pin guard. Stage with git add -f: openapi, the bmr PLAN, both todos, STATE.md, the evidence and the SUMMARY. Do not stage config.json, .vscode or the workspace file. Commit: docs(quick-261018-dlp): openapi states the inspect intake rule; todos, bmr note, STATE and SUMMARY. Check that the signature is G.
  </action>
  <verify>
    <automated>cd /Users/apaderin/dev/octoconv && S=/private/tmp/claude-501/-Users-apaderin-dev-octoconv/95661ab2-afd1-49be-a390-64117af2c14a/scratchpad && Q=.planning/quick/261018-dlp-inspect-intake-read-deadline-follows-a-m && PATH=$S/py314:$PATH TMPDIR=$S/qdlp GOTOOLCHAIN=go1.26.4 go test ./internal/api/ -race -count=1 -run 'TestInspectIntake|TestChartTransportCaps|TestOpenAPI' && FMT=$(GOTOOLCHAIN=go1.26.4 gofmt -l .) && test -z "$FMT" && test "$(grep -c '^[[:space:]]*//' internal/api/transport_cap_test.go)" = 0 && test "$(grep -c '^[[:space:]]*#' docs/openapi.yaml)" -le 1 && test "$(wc -l < .planning/quick/261018-bmr-helm-upload-cap-at-least-the-measured-ca/261018-bmr-PLAN.md | tr -d ' ')" = 272 && test "$(grep -c '261018-dlp' .planning/quick/261018-bmr-helm-upload-cap-at-least-the-measured-ca/261018-bmr-PLAN.md)" -ge 2 && test -f .planning/todos/pending/2026-10-18-create-intake-no-body-deadline.md && test -f .planning/todos/pending/2026-10-18-inspect-post-intake-discard-holds-analysis-slot.md && set -- .planning/todos/pending/*.md && test "$#" = 12 && grep -q 'Ровно двенадцать' .planning/STATE.md && grep -q '^| 261018-dlp |' .planning/STATE.md && test -f $Q/261018-dlp-SUMMARY.md && test -f $Q/evidence/final-gates.txt && git diff --quiet main -- internal/convert/exec.go && SIG=$(git log -2 --format='%G?') && test "$(printf '%s' "$SIG" | tr -d '\n')" = GG && ST=$(git status --porcelain) && test -z "$(printf '%s\n' "$ST" | grep -v -e '^ M .planning/config.json$' -e '^?? .vscode/$' -e '^?? octoconv.code-workspace$' -e '^$')" && echo T2-OK</automated>
  </verify>
  <done>The paced-connection variant passes, and t2-variant.txt shows it caught by M1. The openapi test went RED then GREEN, and the 400 description states the rule. The doc sweep is recorded. The bmr PLAN's two lines point at this task, with the line count unchanged. Two new pending todos exist, and STATE lists twelve that match the directory, plus the 261018-dlp Quick Tasks row. final-gates.txt shows gofmt, vet, the race run and the full go test green, exec.go identical and the comment counts held. The SUMMARY is written. Two signed commits (test, docs). The tree is clean apart from the three never-committed paths.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| authenticated internal client -> api POST /v1/inspect body | untrusted byte timing and volume cross here; the intake slot (8) and the analysis slot (4) are the shared resources |
| api -> net/http connection | the read deadline is the only body bound; the handler sets it through http.ResponseController |

## STRIDE Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation Plan |
|-----------|----------|-----------|----------|-------------|-----------------|
| T-dlp-01 | Denial of Service | inspect intake slot vs a trickling client | high | mitigate | the minimum-rate deadline cuts any body that falls behind start + 30 s + received / 1 MiB/s (T2; M5 shows a sliding idle deadline would not) |
| T-dlp-02 | Denial of Service | inspect intake slot vs a stalled client | high | mitigate | the deadline is set before the first Read, so zero bytes are cut at 30 s, as today (T3; M4) |
| T-dlp-03 | Tampering | credit for bytes not delivered | medium | mitigate | credit counts n returned by Read, never len(p), and is taken outside MaxBytesReader so the overflow byte never counts (T5; M2) |
| T-dlp-04 | Denial of Service | deadline growth past the cap | medium | mitigate | credit is clamped at s.inspectMaxUploadBytes, and MaxBytesReader bounds the body; the worst case is 213 s per slot at the chart cap (T4 both halves; M3) |
| T-dlp-05 | Denial of Service | longer slot holds by a paying client | low | accept | a hold past 30 s costs at least 1 MiB/s per slot (all 8: 64 Mbit/s sustained) and at most 213 s per request, under RATE_LIMIT_INSPECT_RPM 30; the heap bound (8 x 32 MiB) is time-independent |
| T-dlp-06 | Denial of Service | POST /v1/jobs body intake | medium | transfer | no deadline and no intake bound by design (260810-slf); filed as 2026-10-18-create-intake-no-body-deadline.md |
| T-dlp-07 | Denial of Service | post-intake discard read holds the analysis slot | low | transfer | pre-existing, at most the intake deadline; the cheapest variant is unchanged at about 30 s; filed as 2026-10-18-inspect-post-intake-discard-holds-analysis-slot.md |
| T-dlp-SC | Tampering | npm/pip/cargo installs | high | accept | no package is installed or added by this plan; go.mod and go.sum are untouched |
</threat_model>

<verification>
- Real connection, full Routes() chain, ReadHeaderTimeout 10 s: steady above-rate client admitted past the window (T1), slow client cut with 400 invalid multipart form (T2), stalled client cut at the window (T3), largest-ceiling STL at a scaled 20 Mbit/s admitted (variant).
- Unit: the deadline is bounded at start + window + cap/rate and is exactly 213 s at the chart cap with the production constants (T4); it is credited per returned byte (T5); it never moves backward (T4 setter).
- RED evidence on the behaviour-preserving seam; M1 to M5 and the variant's M1 caught; files cmp-identical after every revert.
- openapi states the rule, and a test ties it to the constants; no shipped text states the fixed rule as current.
- exec.go identical to main; handlers.go and cmd/api/main.go unchanged; gofmt, vet and the full go test ./... -count=1 green; no comment line added.
</verification>

<success_criteria>
- A 182 MiB STL inspect over any uplink at or above 1 MiB/s is admitted; a slower-than-rate or stalled client is cut exactly as today.
- Five signed commits on gsd/helm-upload-cap-cad-ceilings (refactor, test, fix, test, docs), not pushed, none with a Co-Authored-By trailer.
- STATE.md has the 261018-dlp row and twelve pending todos that match the directory; the SUMMARY records D1 to D9, the evidence, the mutations and the residuals.
</success_criteria>

<output>
Create `.planning/quick/261018-dlp-inspect-intake-read-deadline-follows-a-m/261018-dlp-SUMMARY.md` when done (Task 2, Step 8).
</output>
