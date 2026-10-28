# Global Job Search Implementation Plan

> **For agentic workers:** Execute inline with test-first backend and UI changes; fresh review before PRs.

**Goal:** Search retained task history and sort it chronologically across API pages.
**Architecture:** Additive filter/metadata fields, literal PostgreSQL search, bound chronological cursors, server-driven UI controls.
**Tech Stack:** Go/pgx/PostgreSQL, React/TypeScript, Vitest/Playwright.
**Spec:** docs/superpowers/specs/2026-10-28-job-search-design.md

## Global Constraints
- Preserve default ordering, existing filters and legacy cursors; no cross-client leakage.
- q bounded to 256 UTF-8 bytes, no NUL; sort allowlist only; use bound SQL arguments.
- No migration or production database changes. Database checks use an isolated disposable container.

## Review Focus
- Tie timestamps and reversed pagination cannot duplicate or omit jobs.
- Literal %, _ and backslash must not act as search wildcards.
- Filters and cursor cannot cross query/order contexts or bypass client scope.
- Missing/duplicate primary input rows cannot duplicate listing rows.
- Old backend must produce an explicit compatibility message when search/order is unsupported.

### Task 1: Backend query/cursor contracts
- [ ] Add failing normalization/cursor roundtrip/context tests; implement bounded query and backwards-compatible extended cursors.
- [ ] Add failing isolated-Postgres tests for history-wide search, literal matching, sorting/ties, missing inputs and tenant isolation; implement Repo.List and optional filename.
- [ ] Add API validation and query echo tests; update OpenAPI and API docs.
### Task 2: UI integration
- [ ] Add query/order API tests including unsupported older backend; implement list query parameters and optional filename/echo types.
- [ ] Add explicit search/clear and chronology controls with cursor/selection reset and no-matches state; verify browser behavior.
### Task 3: Verification and PRs
- [ ] Run backend unit/integration tests against disposable DB, frontend tests/build/browser suite; fresh independent review; create separate backend/UI PRs and link dependency.
