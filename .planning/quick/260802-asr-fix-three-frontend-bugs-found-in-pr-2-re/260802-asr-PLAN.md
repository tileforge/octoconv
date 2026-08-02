---
phase: quick-260802-asr
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - frontend/package.json
  - frontend/package-lock.json
  - frontend/src/formats.ts
  - frontend/src/formats.test.ts
  - frontend/src/api.ts
  - frontend/src/api.test.ts
  - frontend/src/App.tsx
autonomous: true
requirements: [PR2-F1-FINALIZE-RACE, PR2-F2-FINALIZE-RETRY, PR2-F3-TARGET-FILTER]

must_haves:
  truths:
    - "A job that reaches `done` renders its Download link with no manual refresh — the post-terminal GET /v1/jobs/{id} is no longer discarded by the effect teardown that its own status transition triggers"
    - "A single transient failure of that post-terminal GET does not permanently hide a completed job's result — the fetch is retried a bounded number of times"
    - "Retries are bounded and never fire on a permanent error: 401/403/404/422 and AbortError throw on the first attempt"
    - "The target-format select offers only formats the server can actually produce from the selected file's source format — a .docx never offers `alto`, and the default selection is always a registered pair"
    - "photo.jpeg, paper.tex, page.htm and scan.tif offer exactly the same targets as photo.jpg, paper.latex, page.html and scan.tiff — the client mirrors internal/convert NormalizeFormat's alias table"
    - "A file whose source format has no registered targets (e.g. archive.zip) yields an empty, non-submittable selector instead of a silently-wrong default"
    - "The pure logic behind all of the above lives in importable modules with genuine automated regression tests; `npm run build` (tsc --noEmit && vite build) stays green"
  artifacts:
    - path: "frontend/src/formats.ts"
      provides: "normalizeFormat / sourceFormatOfFilename / targetFormatsFor / targetFormatsOf — the pure capability-filtering layer, importable and side-effect free"
      exports: ["normalizeFormat", "sourceFormatOfFilename", "targetFormatsFor", "targetFormatsOf"]
    - path: "frontend/src/formats.test.ts"
      provides: "alias-table + source-filter regression tests, incl. the jpeg/tex aliases F3 explicitly warns about"
      contains: "jpeg"
    - path: "frontend/src/api.ts"
      provides: "isRetryableError + getJobWithRetry — the bounded-retry core for the finalize path"
      exports: ["isRetryableError", "getJobWithRetry"]
    - path: "frontend/src/api.test.ts"
      provides: "retry-count, backoff-order, last-error-propagation and no-retry-on-permanent-failure tests"
      contains: "getJobWithRetry"
    - path: "frontend/src/App.tsx"
      provides: "live-progress effect keyed on job id only, identity-gated finalize write, source-derived target selector"
  key_links:
    - from: "frontend/src/App.tsx live-progress effect"
      to: "frontend/src/api.ts getJobWithRetry"
      via: "finalize() calls the retrying variant, not bare getJob"
      pattern: "getJobWithRetry"
    - from: "frontend/src/App.tsx finalize()"
      to: "setJob functional update"
      via: "job-id identity guard replacing the `cancelled` gate"
      pattern: "prev\\.job_id === jobId"
    - from: "frontend/src/App.tsx target selector"
      to: "frontend/src/formats.ts targetFormatsFor"
      via: "useMemo over (formatsResp, file)"
      pattern: "targetFormatsFor\\("
    - from: "frontend/src/formats.ts normalizeFormat"
      to: "internal/convert/convert.go NormalizeFormat"
      via: "hand-mirrored alias table (jpeg->jpg, tif->tiff, htm->html, tex->latex)"
      pattern: "latex"
---

<objective>
Fix the three frontend bugs surfaced by the PR#2 review and deferred at the time. All three
were re-confirmed against current code before this plan; do not re-litigate whether they are real.

- **F1** — a completed conversion shows no download link. The live-progress effect in
  `frontend/src/App.tsx` lists `job?.status` in its dependency array. On a terminal SSE frame,
  `onProgress` calls `setJob({...prev, ...evt})`, flipping `job.status` to terminal, which changes
  the deps, which runs the cleanup and sets `cancelled = true` — all before the `finalize()` GET it
  just launched resolves. `finalize()` then hits `if (!cancelled)` and throws the response away.
  Terminal SSE frames deliberately carry no `download_url` (`internal/api/sse.go` `emitTerminal`
  emits `{"status": ...}` only, by design D-02), and the re-run of the effect early-returns on
  `isTerminal`, so nothing ever fetches it again: a successful job renders as done with no result.
- **F2** — `finalize()` has no retry. One transient network blip permanently hides a completed
  job's result, because the effect will not re-run for a terminal job. Same code path as F1 —
  fixed as one coherent redesign of the finalize path, not two bolted-on patches.
- **F3** — the target selector offers impossible conversions. `targetFormatsOf` flattens every
  engine's `pair[1]` into one global set, discarding the source half of the capability data the
  server already sends; `App.tsx` never recomputes on file selection and defaults to
  `nextFormats[0]` — alphabetically first, which lands on `alto` (an OCR output). Uploading a
  docx or markdown with that default yields an avoidable 422 even though `GET /v1/formats` told
  us the exact supported pairs.

Purpose: three behavioural bug fixes in the reference SPA. No UI redesign, no restyling, no
server-contract change — everything needed is already in `GET /v1/formats`.

Output: a new pure `frontend/src/formats.ts` capability layer, a bounded-retry core in
`frontend/src/api.ts`, both under automated tests, and a rewired `App.tsx`.
</objective>

<execution_context>
@/Users/apaderin/dev/octoconv/.claude/get-shit-done/workflows/execute-plan.md
@/Users/apaderin/dev/octoconv/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/STATE.md
@CLAUDE.md
@frontend/src/App.tsx
@frontend/src/api.ts
@frontend/package.json
@frontend/tsconfig.json

<interfaces>
<!-- Contracts the executor needs. Extracted from the codebase — no exploration required. -->

Server capability payload — `internal/api/formats_handlers.go` `handleListFormats`:
`{"engines": {"<class>": {"pairs": [["<from>","<to>"], ...]}}}`.
`internal/convert` `Registry.Register` normalizes BOTH halves of every pair through
`NormalizeFormat` before storing, and `Registry.Classes()` walks that same map — so the
`pair[0]` values the client receives are ALREADY normalized. The client only has to normalize
the *file extension* it derives locally; normalizing `pair[0]` defensively is still cheap and
future-proof.

`internal/convert/convert.go` `NormalizeFormat` (lines 52-71) — the table to mirror:
lowercase, `TrimSpace`, strip one leading `"."`, then `jpeg -> jpg`, `tif -> tiff`,
`htm -> html`, `tex -> latex`; everything else identity.

Already exported from `frontend/src/api.ts` (do not change these signatures):
- `type JobStatus = 'queued' | 'active' | 'done' | 'failed' | 'canceled'`
- `function isTerminal(status: JobStatus): boolean`
- `type FormatsResponse = { engines: Record<string, { pairs: [string, string][] }> }`
- `type JobStatusResponse = { job_id, status, progress_pct, eta_seconds, opts?, download_url?, error_code?, error_message? }`
- `type ProgressEvent = { status, progress_pct, eta_seconds }`  (NOTE: no `download_url` — this is F1's root cause)
- `class ApiError extends Error { readonly status: number }`
- `function getJob(params: { apiKey: string; jobId: string }): Promise<JobStatusResponse>`
- `function subscribeJobEvents(params: { apiKey, jobId, signal, onProgress }): Promise<void>`
  — resolves on the terminal frame (it calls `reader.cancel()`), returns silently on AbortError,
  rejects otherwise so callers can fall back to polling.
</interfaces>
</context>

<decision_record>
## Tooling trade-off (explicit planning decision)

`frontend/package.json` ships only `dev`, `build` and `preview`; there is NO test runner.
The two options were: (a) introduce vitest so the pure logic gets genuine regression tests, or
(b) keep the diff dependency-free and verify with `tsc --noEmit` plus manual repro steps.

**Taken: (a), scoped to vitest alone.**

Why:
- The alias table in F3 is exactly the class of logic that regresses silently. The Go side already
  has a scar from it — the `tex -> latex` case in `NormalizeFormat` carries a comment explaining
  that every real LaTeX upload 422'd until the alias was added. A hand-mirrored copy of that table
  in TypeScript with no test is the same trap, one language over.
- F2's retry has off-by-one semantics (attempts vs. retries), an ordering requirement (backoff
  sequence) and a classification predicate (retry 5xx, never 401). Those are cheap to pin and
  expensive to get subtly wrong.
- Cost is ONE dev-only devDependency. `environment: 'node'` — no jsdom, no
  @testing-library/react, no browser runtime. Nothing reaches `frontend/dist`, which is the only
  artifact ever served.
- Vitest 4.1.10 declares `vite: ^6 || ^7 || ^8` (repo has vite 8.1.3), `@types/node: >=24`
  (repo has 26.1.0) and `node: >=24` — verified compatible against this repo's exact versions,
  no lockfile churn beyond the new subtree.

What (a) deliberately does NOT buy: F1 is a React effect race. Proving it under test needs
jsdom + @testing-library/react + a fake SSE transport — a far larger dependency footprint for one
effect. F1 is therefore verified structurally (`job?.status` is gone from the dep array; the
finalize write is identity-gated, not `cancelled`-gated) plus a precise manual reproduction.
The pure cores of F2 and F3 carry the automated coverage.

`npm run build` (tsc --noEmit && vite build) MUST stay green regardless — test files live in
`src/`, so `tsc --noEmit` type-checks them too, and they are unreachable from `index.html`, so
`vite build` never bundles them.

## Package legitimacy (npm install gate)

No RESEARCH.md exists for a quick task, so the audit was performed at plan time against the
live registry:

| Package | Version | Provenance | Verdict |
|---------|---------|------------|---------|
| vitest | 4.1.10 | repo `git+https://github.com/vitest-dev/vitest.git`; published by `GitHub Actions` (trusted publishing); maintainer `ariperkkio` (Vitest core); package created 2021-12-03; tarball on registry.npmjs.org | `[VERIFIED]` — same org as the already-present `vite` / `@vitejs/plugin-react` deps |

`[VERIFIED]` — no blocking human legitimacy checkpoint required. Install exactly
`vitest@4.1.10`; if the executor finds it must deviate to any other package or major version,
STOP and surface it rather than substituting.
</decision_record>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Add vitest and extract the source-aware capability layer (F3 pure core)</name>
  <files>frontend/package.json, frontend/package-lock.json, frontend/src/formats.ts, frontend/src/formats.test.ts, frontend/src/api.ts</files>
  <behavior>
    normalizeFormat:
    - 'JPEG' -> 'jpg'; ' .JPEG ' -> 'jpg'; 'jpeg' -> 'jpg'
    - 'tif' -> 'tiff'; 'htm' -> 'html'; 'tex' -> 'latex'
    - 'md' -> 'md'; 'docx' -> 'docx' (identity for everything unaliased)
    - '' -> ''
    sourceFormatOfFilename:
    - 'photo.jpeg' -> 'jpg'; 'paper.tex' -> 'latex'; 'page.HTM' -> 'html'; 'scan.TIF' -> 'tiff'
    - 'my.report.docx' -> 'docx' (last dot wins)
    - 'README' -> ''; '.gitignore' -> '' (leading dot is not an extension); 'archive.' -> ''
    targetFormatsFor(resp, source), against a fixture with engines
      ocr:    [['png','alto'], ['png','txt']]
      pandoc: [['docx','md'], ['docx','html'], ['md','docx']]
      image:  [['jpg','png']]
    - (resp, 'docx')  -> ['html', 'md']            (sorted; 'alto' absent — this IS F3)
    - (resp, 'PNG')   -> ['alto', 'txt']           (case-insensitive)
    - (resp, 'jpeg')  -> ['png']                   (alias — the half-broken case F3 calls out)
    - (resp, 'zip')   -> []                        (no registered source)
    - (resp, '')      -> []                        (no extension => nothing offered)
    targetFormatsOf(resp):
    - -> ['alto', 'docx', 'html', 'md', 'png', 'txt']  (global union unchanged; the no-file-yet fallback)
  </behavior>
  <action>
    Install the test runner: from `frontend/`, run `npm install --save-dev --save-exact vitest@4.1.10`.
    Pin exactly that version (matches the repo's exact-version devDependency style — every existing
    entry is unranged). Add `"test": "vitest run"` to `scripts` in `frontend/package.json`. Do NOT
    add jsdom, happy-dom, @testing-library/* or @vitest/ui. Vitest's defaults already use
    `environment: 'node'` and an include glob covering `src/**/*.test.ts`, so no vitest config file
    is expected; only if the defaults demonstrably fail to discover `src/formats.test.ts` may the
    executor add a minimal `frontend/vitest.config.ts` (and it must then be listed in the SUMMARY).
    Commit `package-lock.json`.

    Create `frontend/src/formats.ts` — pure, side-effect free, no React and no fetch. It imports
    only `import type { FormatsResponse } from './api';` (type-only, so no runtime cycle; `api.ts`
    must never import `formats.ts`). Export four functions:

    - `normalizeFormat(raw: string): string` — hand-mirror of `internal/convert/convert.go`
      `NormalizeFormat` (lines 52-71): trim whitespace, strip one leading `.`, lowercase, then map
      `jpeg->jpg`, `tif->tiff`, `htm->html`, `tex->latex`, identity otherwise. Carry a doc comment
      naming that Go function as the source of truth and stating that the two tables must be kept
      in sync — a client that matches raw extensions against pair sources wrongly rejects
      `photo.jpeg` and `paper.tex`.
    - `sourceFormatOfFilename(filename: string): string` — take the substring after the LAST `.`;
      return `''` when there is no dot, when the only dot is at index 0 (dotfiles), or when the
      trailing segment is empty; otherwise return `normalizeFormat` of it.
    - `targetFormatsFor(resp: FormatsResponse, source: string): string[]` — normalize `source`;
      return `[]` immediately if it is empty. Walk every engine's pairs, and for each pair whose
      `normalizeFormat(pair[0])` equals the normalized source, collect `normalizeFormat(pair[1])`.
      Dedupe through a `Set`, return `Array.from(...).sort()`.
    - `targetFormatsOf(resp: FormatsResponse): string[]` — MOVE the existing implementation verbatim
      out of `api.ts` (behaviour unchanged: deduped, sorted global union of every `pair[1]`); keep
      its doc comment and extend it to say it is now only the fallback used before a file is chosen.

    In `frontend/src/api.ts`, DELETE `targetFormatsOf` and its doc comment (it now lives in
    `formats.ts`). Leave `FormatsResponse` and every other export in `api.ts` untouched. `App.tsx`
    still imports `targetFormatsOf` from `./api` at this point and will therefore fail to compile
    until Task 3 rewires it — that is expected and is why the build gate lands in Task 3; this
    task's gate is `npx vitest run` alone.

    Create `frontend/src/formats.test.ts` with `import { describe, expect, it } from 'vitest';`
    (explicit imports, no globals — so no `types` entry is needed in `tsconfig.json`). Cover every
    case in the behavior block above, including the two alias regressions (`photo.jpeg`,
    `paper.tex`) that F3 explicitly warns about.

    Do NOT touch Go code, the chart, compose, `scripts/*.sh`, `frontend/src/styles.css`, or the
    stale "4 MiB" comment prose — all separately tracked.
  </action>
  <verify>
    <automated>cd frontend && npx vitest run src/formats.test.ts</automated>
    <automated>cd frontend && node -e "const p=require('./package.json'); if(p.devDependencies.vitest!=='4.1.10') throw new Error('vitest not pinned to 4.1.10'); if(p.scripts.test!=='vitest run') throw new Error('test script missing'); for (const bad of ['jsdom','happy-dom','@testing-library/react']) if (p.devDependencies[bad]) throw new Error('unexpected dep '+bad); console.log('package.json OK')"</automated>
    <automated>cd frontend && ! grep -q 'targetFormatsOf' src/api.ts && grep -q 'export function targetFormatsOf' src/formats.ts && echo 'targetFormatsOf moved api.ts -> formats.ts'</automated>
  </verify>
  <done>`vitest run src/formats.test.ts` passes with every behavior case green; `vitest@4.1.10` is an exact-pinned devDependency with a `test` script and no DOM-runtime packages alongside it; `targetFormatsOf` no longer appears in `api.ts`.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Add the bounded-retry core for the finalize path (F2 pure core)</name>
  <files>frontend/src/api.ts, frontend/src/api.test.ts</files>
  <behavior>
    isRetryableError:
    - new ApiError(500, 'x') -> true;  new ApiError(503, 'x') -> true
    - new ApiError(429, 'x') -> true;  new ApiError(408, 'x') -> true
    - new ApiError(401, 'x') -> false; new ApiError(403, 'x') -> false
    - new ApiError(404, 'x') -> false; new ApiError(422, 'x') -> false
    - new DOMException('aborted', 'AbortError') -> false  (an aborted request is never retried)
    - new TypeError('Failed to fetch') -> true            (network-layer failure)
    getJobWithRetry (with injected fetchJob + sleep):
    - fetchJob resolves first call -> resolves that value; fetchJob called 1x; sleep called 0x
    - fetchJob rejects ApiError(503) twice then resolves -> resolves; fetchJob called 3x;
      sleep called with 400 then 1200, in that order
    - fetchJob always rejects ApiError(503) -> rejects with the LAST error; fetchJob called
      exactly 3x (attempts default); sleep called 2x
    - fetchJob rejects ApiError(401) -> rejects immediately; fetchJob called 1x; sleep called 0x
    - attempts: 1 -> fetchJob called 1x even for a retryable error; sleep called 0x
  </behavior>
  <action>
    Add two exports to `frontend/src/api.ts`, next to `getJob` (do not modify `getJob` itself —
    the non-retrying primitive stays available and is what the retry helper defaults to calling).

    `export function isRetryableError(error: unknown): boolean` — an `ApiError` is retryable iff
    its status is `>= 500` or is exactly `429` or `408`; every other `ApiError` (notably 401/403/
    404/422) is permanent. A `DOMException` whose `name` is `'AbortError'` is NOT retryable. Any
    other value (fetch's `TypeError` on a network failure, and anything unrecognised) IS retryable.
    Doc comment must state why: retrying a 401 or a 404 cannot succeed and only burns the budget,
    while a blip on the wire is exactly the case F2 exists for.

    `export async function getJobWithRetry(params: { apiKey: string; jobId: string; attempts?: number; delaysMs?: number[]; fetchJob?: (p: { apiKey: string; jobId: string }) => Promise<JobStatusResponse>; sleep?: (ms: number) => Promise<void> }): Promise<JobStatusResponse>`
    — defaults: `attempts = 3` (one initial call plus two retries), `delaysMs = [400, 1200]`,
    `fetchJob = getJob`, `sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))`.
    `fetchJob` and `sleep` exist solely as seams so the retry semantics are testable without
    timers or a network — they are not part of the calling contract in `App.tsx`.
    Loop `attempt` from 1 to `attempts`: return on success; on failure, rethrow immediately if
    `!isRetryableError(err)` or `attempt === attempts`, otherwise `await sleep(delaysMs[attempt - 1] ?? delaysMs[delaysMs.length - 1] ?? 0)`
    and continue. The budget is bounded by construction: at most `attempts` requests and at most
    ~1.6s of added latency — a completed job's result is never re-hammered in a loop.

    Create `frontend/src/api.test.ts` with `import { describe, expect, it, vi } from 'vitest';`
    covering every case in the behavior block. Use `vi.fn()` stubs for `fetchJob` and `sleep`
    (an async no-op recording its argument) — do NOT use fake timers, do NOT stub global `fetch`,
    and do NOT exercise `getJob`'s network path. Assert call counts and the exact `sleep` argument
    order, and assert that the rejection propagated is the last error thrown, not the first.

    Do NOT touch Go code, the chart, compose, `scripts/*.sh`, or the stale "4 MiB" comment prose.
  </action>
  <verify>
    <automated>cd frontend && npx vitest run src/api.test.ts</automated>
    <automated>cd frontend && grep -q 'export async function getJobWithRetry' src/api.ts && grep -q 'export function isRetryableError' src/api.ts && echo 'retry core exported'</automated>
  </verify>
  <done>`vitest run src/api.test.ts` passes with every behavior case green; `isRetryableError` and `getJobWithRetry` are exported from `api.ts`; `getJob` is unchanged.</done>
</task>

<task type="auto">
  <name>Task 3: Rewire App.tsx — finalize survives the terminal transition, selector follows the file</name>
  <files>frontend/src/App.tsx</files>
  <action>
    Three behavioural changes to `frontend/src/App.tsx`. No restyling, no layout redesign, no new
    CSS classes — reuse the existing `.field` / `.hint` / `<option>` markup.

    **(1) F1 — the effect must not tear itself down.** Hoist `const jobId = job?.job_id ?? null;`
    above the live-progress effect and change its dependency array from
    `[apiKey, job?.job_id, job?.status]` to `[apiKey, jobId]`. Replace the
    `if (!job || isTerminal(job.status))` early return with `if (!jobId) return undefined;`.
    Dropping the `isTerminal` guard is safe and required: a job only ever enters this component as
    `queued` (from `handleSubmit`), `subscribeJobEvents` already returns on the terminal frame, the
    server closes the stream after one terminal event, and the polling fallback already clears its
    own interval on `isTerminal(nextJob.status)`. With `job?.status` out of the deps, the terminal
    transition no longer runs the cleanup, so the in-flight `finalize()` GET survives the very
    status change that triggered it — this is F1's root cause, removed.

    **(2) F1/F2 — one coherent finalize path.** Rewrite `finalize()`:
    - guard against a double run with a `let finalized = false;` declared in the effect scope;
      `finalize()` returns immediately if it is already set and sets it on entry;
    - call `getJobWithRetry({ apiKey: trimmedKey, jobId })` instead of `getJob` (F2);
    - write the result through an identity-gated functional update, NOT the `cancelled` flag:
      `setJob((prev) => (prev && prev.job_id === jobId ? finalJob : prev));`. The correctness
      guard is now "is this still the job on screen", which is immune to the effect lifecycle;
      a stale response for a superseded job is still dropped.
    - on exhaustion, report through `if (!cancelled) setError(describeError(finalizeError));`
      (an error surfacing after the user moved on is noise worth dropping).
    Apply the same identity guard inside `onProgress`:
    `setJob((prev) => (prev && prev.job_id === jobId ? { ...prev, ...evt } : prev));`.
    Keep `cancelled` and `controller.abort()` in the cleanup — they still stop the SSE reader and
    the polling interval; they simply no longer gate the finalize write.
    Leave the polling fallback otherwise as-is: its own GET already returns `download_url`, so it
    needs no finalize call.
    Add a short comment above the dep array recording WHY `job?.status` must stay out of it, so
    the bug cannot be reintroduced by a future "the effect should react to status" edit.

    **(3) F3 — targets derived from the selected file.** Replace
    `const [formats, setFormats] = useState<string[]>([])` with
    `const [formatsResp, setFormatsResp] = useState<FormatsResponse | null>(null)` (import the
    `FormatsResponse` type from `./api`). The formats-loading effect now stores the raw response
    (`setFormatsResp(resp)`), sets it to `null` when the trimmed key is empty, and its
    `setTarget(...)` call is REMOVED. Add:
    `const availableTargets = useMemo(() => { if (!formatsResp) return []; return file ? targetFormatsFor(formatsResp, sourceFormatOfFilename(file.name)) : targetFormatsOf(formatsResp); }, [formatsResp, file]);`
    and a selection-sync effect
    `useEffect(() => { setTarget((prev) => (prev && availableTargets.includes(prev) ? prev : (availableTargets[0] ?? ''))); }, [availableTargets]);`
    — a still-valid choice is preserved, otherwise the default falls to the first entry of the
    FILTERED list, which is a registered pair by construction (the alphabetical `alto` default
    that produced the avoidable 422 is gone).
    Render the select from `availableTargets` instead of `formats`. Keep exactly one placeholder
    `<option value="">`, with its text chosen by state: no `formatsResp` -> the existing
    "Enter API key to load formats"; a file is selected but `availableTargets` is empty ->
    `No conversions available for this file`; otherwise the existing text. `canSubmit` already
    requires `target !== ''`, so an unsupported source leaves the button disabled with no code
    change.
    Update imports: `targetFormatsFor`, `sourceFormatOfFilename` and `targetFormatsOf` now come
    from `./formats`; `getJobWithRetry` and the `FormatsResponse` type from `./api`. Remove the
    now-unused `targetFormatsOf` import from `./api` and drop `getJob` from the import list only
    if nothing else still uses it (the polling fallback does — keep it).

    Do NOT touch Go code, the chart, compose, `scripts/*.sh`, `frontend/src/styles.css`, or the
    stale "4 MiB" comment prose — all separately tracked. No API changes: the server contract is fixed.
  </action>
  <verify>
    <automated>cd frontend && npm run build</automated>
    <automated>cd frontend && npm test</automated>
    <automated>cd frontend && ! grep -q 'job?\.status' src/App.tsx && echo 'F1 OK: job?.status gone from App.tsx (and therefore from the dep array)'</automated>
    <automated>cd frontend && grep -q 'prev.job_id === jobId' src/App.tsx && grep -q 'getJobWithRetry' src/App.tsx && grep -q 'targetFormatsFor' src/App.tsx && grep -q 'sourceFormatOfFilename' src/App.tsx && echo 'F1/F2/F3 wiring present'</automated>
    <human-check>
      Compose stack up (`set -a && . ./.env && set +a && docker compose up -d`), then
      `cd frontend && npm run dev` and open http://localhost:5173.

      F3 — selector:
      1. Paste a valid ApiKey; the format list loads.
      2. Select a `.docx`. EXPECT: the target list contains markup targets (md/html/odt/...) and
         does NOT contain `alto` or `hocr`; the pre-selected default is one of those valid targets.
      3. Rename a JPEG to `photo.jpeg` and select it. EXPECT: a NON-empty target list (the alias
         works; before the fix a raw-extension match would have offered nothing or everything).
         Repeat with `paper.tex` and `page.htm`.
      4. Select an `archive.zip`. EXPECT: empty list, "No conversions available for this file",
         Convert button disabled — not a silently-wrong default.

      F1 — download link:
      5. Select a small PNG, keep the default target, press Convert. Watch status go
         queued -> active -> done.
         EXPECT (PASS): the "Download converted file" link appears within ~2s of the status
         flipping to `done`, with NO manual page refresh. (Before the fix it never appeared.)

      F2 — retry:
      6. Repeat step 5, but open DevTools -> Network -> Block request URL and add a
         `*/v1/jobs/*` rule the moment the status flips to `done`; remove the block within ~1.5s.
         EXPECT (PASS): the download link still appears — a retry succeeded. Leaving the block on
         permanently must instead surface an error message, not a silent blank result.
    </human-check>
  </verify>
  <done>`npm run build` (tsc --noEmit && vite build) and `npm test` both pass; `job?.status` is absent from the live-progress dependency array; `finalize()` uses `getJobWithRetry` and an identity-gated `setJob`; the target selector is derived from the selected file's normalized source format; the manual F1/F2/F3 reproductions behave as specified.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| browser SPA -> OctoConv API | The `ApiKey` header and all job data cross here; the SPA is untrusted UI over a server that re-validates every pair (`Registry.Supports`) and every upload |
| npm registry -> frontend devDependencies | New build-time dependency subtree (vitest) enters the developer's machine |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-asr-01 | Denial of Service | `getJobWithRetry` retry loop | mitigate | Bounded by construction: `attempts = 3` total requests, fixed `[400, 1200]ms` backoff, no unbounded loop and no recursion. Retry is scoped to the single post-terminal GET, never to upload or cancel |
| T-asr-02 | Denial of Service | retry on 429 | mitigate | 429 is retryable but capped at 2 retries with backoff, so a rate-limited client cannot amplify; 401/403/404/422 are permanent and cost exactly one request |
| T-asr-03 | Information Disclosure | `finalize()` writing a stale job's presigned `download_url` into the UI | mitigate | Identity-gated functional update (`prev.job_id === jobId`) — a response for a superseded job is dropped rather than rendered, which is strictly stronger than the `cancelled` flag it replaces |
| T-asr-04 | Spoofing / Elevation | Client-side target filtering treated as authorization | accept | `targetFormatsFor` is a UX affordance only. The server remains the sole authority: `Registry.Supports` still 422s any unsupported pair regardless of what the select offered. No API change, no weakening of a server-side check |
| T-asr-SC | Tampering | `npm install vitest@4.1.10` | mitigate | Plan-time legitimacy audit performed against the live registry (repo `vitest-dev/vitest`, GitHub-Actions trusted publishing, maintainer `ariperkkio`, package created 2021-12-03, same org as the incumbent `vite` dep) -> `[VERIFIED]`, exact-pinned, dev-only, never bundled into `frontend/dist`. Any deviation from that exact package/version stops the task |
</threat_model>

<verification>
- `cd frontend && npm test` — all of `src/formats.test.ts` and `src/api.test.ts` green.
- `cd frontend && npm run build` — `tsc --noEmit` (which type-checks the test files too, since they
  live under `src/`) followed by `vite build`, both clean.
- `git diff --stat` touches only the seven files in `files_modified`. Specifically: zero changes
  under `internal/`, `cmd/`, `charts/`, `scripts/`, `docker-compose*.yml`, and zero edits to the
  stale "4 MiB" comment prose.
- Manual F1/F2/F3 reproductions in Task 3's `<human-check>` all behave as specified.
</verification>

<success_criteria>
- A completed conversion renders its Download link with no manual refresh (F1 closed).
- A transient failure of the post-terminal GET is retried up to 3 attempts before the result is
  given up on, and permanent failures (401/403/404/422/abort) are not retried at all (F2 closed).
- The target selector offers only server-supported targets for the selected file's normalized
  source format, including via the `jpeg`/`tif`/`htm`/`tex` aliases, and its default selection is
  always a registered pair (F3 closed).
- The pure logic behind F2 and F3 lives in importable modules under automated vitest coverage;
  `npm run build` stays green.
- No Go, chart, compose, `scripts/*.sh` or "4 MiB" prose changes; no API contract change.
</success_criteria>

<output>
Create `.planning/quick/260802-asr-fix-three-frontend-bugs-found-in-pr-2-re/260802-asr-SUMMARY.md` when done
</output>
