---
quick_task: 260728-0ay
title: Modernize frontend SPA to API v1.10
one_liner: Rewired the frozen v1.1 image-only SPA to the v1.10 contract — dynamic /v1/formats-driven target select, fetch+ReadableStream SSE progress with polling fallback, real status vocabulary, cancel button, and a localhost:9100 download hint.
tags: [frontend, react, sse, api-v1.10]
requirements: [FE-FORMATS, FE-SSE, FE-STATUS, FE-CANCEL, FE-DOWNLOAD]
key-files:
  created:
    - frontend/src/vite-env.d.ts
  modified:
    - frontend/src/api.ts
    - frontend/src/App.tsx
    - frontend/src/styles.css
    - .gitignore

metrics:
  duration: ~20min
  completed: 2026-07-28
audit_acknowledged:
  milestone: v1.15
  at: 2026-09-30
  status: unknown
---

# Quick Task 260728-0ay: Modernize frontend SPA to API v1.10 Summary

## What changed

**`frontend/src/api.ts`** — full rewrite as the v1.10 typed API layer:
- Removed the hardcoded `targetFormats = ['webp','jpg','png','tiff','heic']` array and `TargetFormat` union.
- `JobStatus` is now the exact 5-value backend union (`queued|active|done|failed|canceled`), with an exported `terminalStatuses` set and `isTerminal()` helper mirroring the server's `isTerminalStatus`.
- Added `FormatsResponse` type + `listFormats({ apiKey })` (`GET /v1/formats`) and a pure `targetFormatsOf(resp)` helper that dedupes/sorts the union of `pair[1]` across every engine class in the response — no hardcoded engine-class list.
- `createJob` now accepts `{ apiKey, file, target: string, opts? }`; `opts` (raw JSON text, not re-stringified) is appended to the FormData only when non-empty after trimming.
- `JobStatusResponse` expanded to the full GET shape (`progress_pct`, `eta_seconds`, `opts?`, `download_url?`, `error_code?`, `error_message?`).
- Added `cancelJob({ apiKey, jobId })` → `POST /v1/jobs/{id}/cancel`.
- Added `subscribeJobEvents({ apiKey, jobId, signal, onProgress })` — fetch()+`res.body.getReader()` SSE reader (EventSource can't set the Authorization header). Buffers chunks, splits frames on `\n\n`, ignores `:`-prefixed keepalive comment lines, parses `data:` lines as JSON, calls `onProgress`, and stops the loop (cancelling the reader) once `isTerminal(status)` is true. AbortError from the signal is swallowed; any other rejection propagates so the caller can fall back to polling.

**`frontend/src/App.tsx`** — full rewrite:
- Target format is now a live `string` populated from `listFormats`/`targetFormatsOf` on API-key change (non-fatal notice on failure, rendered as a `.hint`).
- Added an "Options (JSON, optional)" free-text input passed through to `createJob`.
- New progress/SSE effect keyed on `job.job_id` + non-terminal status: opens an `AbortController`-scoped `subscribeJobEvents`; `onProgress` merges into `job` state; on a terminal event does a one-shot `getJob` to pick up `download_url`/`error_code`/`error_message`. If `subscribeJobEvents` rejects (non-abort), falls back to the previous `setInterval` polling of `getJob` — kept as the fallback branch, not removed.
- Renders a `.progress`/`.progress-bar` element with numeric percent + `ETA ~{n}s` when `progress_pct` is a number; an indeterminate "processing…" hint when `progress_pct` is null and the job is non-terminal (expected for non-AV engines).
- Cancel button (`.cancel`) shown for any non-terminal job, calling `cancelJob` and merging the response into state; guarded against double-clicks via `isCanceling`.
- Download link now paired with a `.hint` note instructing the user to rewrite `minio:9000` → `localhost:9100` for local downloads.
- Status pill (`status status-${job.status}`) now naturally covers `active`/`canceled`; no literal `'running'` remains anywhere in the file.
- File input's `accept` allowlist (previously image-only) was dropped so all engine classes' inputs are selectable.

**`frontend/src/styles.css`** — additive only, no existing rules restructured:
- `.progress` / `.progress-bar` (rounded track + accent fill, 240ms width transition for the SSE ramp).
- `.status-active` (amber pill) and `.status-canceled` (muted gray pill), distinct from `.status-done`/`.status-failed`.
- `.cancel` (outlined/muted secondary button, red-tinted hover, disabled state, included in the 720px responsive full-width rule alongside `button`/`.download`).
- `.hint` (small muted helper-note style, reused for the formats-load notice, progress percent/ETA line, and the download caveat).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking issue] Added missing `frontend/src/vite-env.d.ts`**
- **Found during:** Task 1, first `npm run build` run
- **Issue:** `tsc --noEmit` failed with `TS2882: Cannot find module or type declarations for side-effect import of './styles.css'` in `src/main.tsx` — a file this plan does not touch. The project has no `vite-env.d.ts` and `tsconfig.json` has no `types: ["vite/client"]` entry, so `import './styles.css'` in `main.tsx` had no ambient module declaration. This is a pre-existing baseline gap (confirmed via `git log` — `tsconfig.json`/`main.tsx` history shows only the original "Add minimal frontend uploader" commit; no prior working `npm run build` gate exists in history for this frontend), not something introduced by this plan's edits, but it directly blocks the mandatory acceptance gate.
- **Fix:** Added the standard Vite scaffold file `frontend/src/vite-env.d.ts` containing `/// <reference types="vite/client" />`. No new npm dependency — `vite/client.d.ts` ships inside the already-installed `vite` package.
- **Files modified:** `frontend/src/vite-env.d.ts` (new)
- **Commit:** c7cd61c

**2. [Rule 2 - hygiene] Gitignored `frontend/dist`**
- **Found during:** Task 1, post-build `git status` check
- **Issue:** Running `npm run build` (required by the acceptance gate) produces `frontend/dist/` build output, which was untracked and not covered by any `.gitignore` entry — leaving generated files untracked violates the commit protocol's "never leave generated files untracked" rule.
- **Fix:** Added `frontend/dist` to the root `.gitignore` (next to the existing `frontend/node_modules` entry).
- **Files modified:** `.gitignore`
- **Commit:** c7cd61c

**3. [Rule - wording adjustment] Reworded "long-running" to avoid tripping the `! grep -qw "running"` source assertion**
- **Found during:** Task 1 verification
- **Issue:** The lede copy originally read "...live progress for long-running jobs", and `grep -w` treats the hyphen as a word boundary, so `running` matched as a whole word and failed the plan's automated `! grep -qw "running"` check even though no status-vocabulary literal `'running'` was present.
- **Fix:** Reworded to "...live progress for lengthy jobs."
- **Files modified:** `frontend/src/App.tsx`
- **Commit:** c7cd61c

No other deviations. Plan's interface contract (formats/jobs/events/cancel wire shapes) implemented verbatim as specified — not re-guessed.

## Known Stubs

None. All data paths (formats, create, get, cancel, SSE progress) are wired to live endpoints; no hardcoded/empty placeholder data remains in the rendered UI.

## Threat Flags

None beyond what the plan's threat model already covers — no new network endpoints, auth paths, or trust boundaries introduced. SSE/getJob JSON is rendered via React text interpolation only (no `dangerouslySetInnerHTML`); `download_url` used only as an `href`.

## Verification

- `cd frontend && npm run build` (tsc --noEmit && vite build) passes clean after both tasks.
- All Task 1 and Task 2 automated grep source assertions from the plan pass.
- Only `frontend/src/{api.ts,App.tsx,styles.css,vite-env.d.ts}` and `.gitignore` changed; no backend Go changes, no new npm dependencies, nothing outside `frontend/` except the `.gitignore` hygiene fix.

## Self-Check: PASSED

- FOUND: frontend/src/api.ts
- FOUND: frontend/src/App.tsx
- FOUND: frontend/src/styles.css
- FOUND: frontend/src/vite-env.d.ts
- FOUND commit c7cd61c
- FOUND commit b3351d0
