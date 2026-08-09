# Phase 56: JobRunner Consolidation & Structured Error Codes - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-08-08
**Phase:** 56-JobRunner Consolidation & Structured Error Codes
**Areas discussed:** Sync error envelope & granularity, Async taxonomy freeze vs rationalize, Source of truth & enforcement, EngineDescriptor form & pinning strategy

---

## Sync error envelope & granularity

| Option | Description | Selected |
|--------|-------------|----------|
| Плоский `{error, code}` | Same style as existing `writeQuotaError`/`ratelimit`; zero diff for those two; frontend doesn't branch on it yet | ✓ |
| Вложенный `{error:{code,message,...}}` | More canonical for future growth, but breaks frontend and both existing 429 bodies (ERRC-V2-01 territory) | |
| Плоский + Problem-Details fields | `application/problem+json` synonyms alongside flat shape | |

**User's choice:** Плоский `{error, code}` (recommended option).
**Notes:** Matches existing `writeQuotaError` precedent exactly; extensibility comes from adding top-level fields later, not changing shape.

| Option | Description | Selected |
|--------|-------------|----------|
| По классу отказа, ~10-14 кодов | e.g. `unsupported_conversion`, `invalid_options`, `invalid_request`, `not_found`, etc. — details stay in `message` | ✓ |
| Один код на call site, ~40-60 кодов | Maximum precision but reintroduces text-matching-style fragility across a 4x-wider enum | |
| Гибрид: класс + опциональное подполе | More work, contract stays narrow | |

**User's choice:** По классу отказа, ~10-14 кодов (recommended option).

| Option | Description | Selected |
|--------|-------------|----------|
| Одно пространство кодов | `invalid_options` means the same thing whether hit synchronously (create-job 422) or asynchronously (job failure `error_code`) | ✓ |
| Разные пространства (sync_*/async as-is) | Clearer stage separation but doubles the taxonomy | |

**User's choice:** Одно пространство кодов (recommended option).

| Option | Description | Selected |
|--------|-------------|----------|
| Один `invalid_request` для общей валидации формы | Covers "missing target format", "preset+target conflict", "cannot determine source format", etc. — only `message` differs | ✓ |
| Разбить тоньше (`missing_field`/`conflicting_params`/etc.) | More precision if a concrete client-branching need exists | |

**User's choice:** Один `invalid_request` (recommended option).
**Notes:** No concrete need for finer client-side branching surfaced during discussion.

---

## Async taxonomy freeze vs rationalize

| Option | Description | Selected |
|--------|-------------|----------|
| Заморозить без изменений | 14 existing string values carry over unchanged into `EngineDescriptor.FailureCode`; no rename wave this phase (Pitfall 11) | ✓ |
| Рационализировать catch-all `engine_error` | Would touch a value all 7 engines share identically today, risking consumer churn mid-milestone | |

**User's choice:** Заморозить без изменений (recommended option).
**Notes:** MCP already consumes these structurally (`mcpserver/client.go`); renaming now means a second migration later for consumers, which Pitfall 11 explicitly warns against.

---

## Source of truth & enforcement

| Option | Description | Selected |
|--------|-------------|----------|
| Go-константы + `docs/ERROR-CODES.md` из кода | Compiler catches typos; doc is generated/maintained from code, tested for Go↔doc parity | ✓ |
| `docs/openapi.yaml` как enum, Go генерируется | Cleaner for external consumers but needs codegen tooling not yet present; this is explicitly ERRC-V2-02 | |

**User's choice:** Go-константы + `docs/ERROR-CODES.md` (recommended option).

| Option | Description | Selected |
|--------|-------------|----------|
| Статический анализ + тест-фикстур на unknown code | grep/lint check across frontend + both MCP servers for message-text branching; separate test sends an unrecognized code and asserts fallback behavior, not a crash | ✓ |
| Только документировать требование | Cheaper but weaker against regression | |

**User's choice:** Статический анализ + тест-фикстур (recommended option).

---

## EngineDescriptor form & pinning strategy

| Option | Description | Selected |
|--------|-------------|----------|
| Package-level переменные в `internal/worker` | `ImageDescriptor`, `DocumentDescriptor`, etc. as `var`s; each `cmd/*-worker/main.go` imports its own — matches today's one-binary-one-engine deployment model | ✓ |
| Реестр по имени движка (`map[string]EngineDescriptor`) | Centralizes all seven in one place but introduces indirection the project doesn't use today | |

**User's choice:** Package-level переменные (recommended option).

| Option | Description | Selected |
|--------|-------------|----------|
| Пиннинг-тесты до рефакторинга + инверсионный прогон | 3-step: (1) write/extend pinning tests on today's seven-handler network before touching code, (2) refactor, same tests must pass unchanged, (3) temporarily move the metrics/webhook emission before the guarded-transition commit — test must fail, proving it actually catches this defect class | ✓ |
| Только пиннинг-тесты, без инверсионного прогона | Cheaper but weaker proof of SC4 ("structurally impossible") | |

**User's choice:** Пиннинг-тесты + инверсионный прогон (recommended option).

---

## Claude's Discretion

- Exact package name/path for the new sync error-code constants (`internal/errcode` used as a working name, not finalized).
- Exact final list of ~10-14 sync codes — the D-02 starting list is indicative; finalizing the full mapping across all ~60 `writeError` sites is left to planning/execution within the agreed granularity.
- Format of `docs/ERROR-CODES.md` (table vs list) — left to whoever writes it.

## Deferred Ideas

- `docs/openapi.yaml` error-code enum + codegen (ERRC-V2-02) — explicit next step, not this phase.
- 429-body envelope migration (ERRC-V2-01) — not blocking under the flat-envelope decision (D-01); already machine-readable.
- v1.12 debt tail (stuck in-flight job relief, README quota section, `PurgeUsage` clock fix) — stays in Phase 61 per roadmap placement; zero-overlap file set by design.
- Operation dimension / output roles / `OperationalConverter` — Phase 57, confirmed hard dependency (process() must read job.Operation from one consolidated place first).
