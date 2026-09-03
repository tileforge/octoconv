---
id: SEED-023
status: dormant
planted: 2026-09-03
planted_during: Phase 63 review (v1.14, PR #15 in its sixth review round) — raised by the operator
trigger_when: v1.15 scoping, or any milestone that proposes a human-facing surface on OctoConv
scope: Large
---

# SEED-023: An operator console, and the auth question that gates it

The operator asked whether the next milestone should carry a UI item. It should
— but as an **operator console**, not a client portal, and only after one
constraint is deliberately revisited.

## Why a client portal is the wrong shape

OctoConv's clients are internal SERVICES, not people. PROJECT.md is explicit:
"только внутренние сервисы компании — публичная многоарендность и биллинг не
требуются". A portal whose caller is a Go process has no user.

## Why an operator console is not empty

The operational surface already exists and is reachable today only by curl and
two CLIs (`cmd/manage-clients`, `cmd/manage-presets`):

* `GET /v1/system/usage` — per-client usage and limits (OPSV-01/OPSV-02)
* `GET /v1/jobs` — filtered, cursor-paginated, carrying `error_code` and
  `error_message` from a closed, documented vocabulary
* job events — the per-job audit trail workers and the reconciler write
* `/v1/presets` REST — create, list, deactivate
* quotas and per-client limits — settable today only through `manage-clients`

Queue and worker inspection is explicitly OUT: asynqmon already ships in
compose and does it better. A console that duplicates it is a worse version of
something already running.

## The blocker, and it is a real one

Every one of those endpoints authenticates with an API key resolved from the
`clients` table. A human UI needs sessions or SSO, and PROJECT.md's constraints
say "не вводить отдельный внешний auth-провайдер".

**That has to be revisited before the item is scoped, not during it.** A phase
that discovers it mid-flight stalls on a decision that was never a UI decision.
Options include at least: an operator-only static credential, a reverse proxy
terminating SSO in front of the console, or lifting the constraint for HUMAN
access while keeping it for service access.

## Recommended first scope

**Read-only.** Jobs, job events, usage, limits, presets — views, no mutations.
Retry, cancel and preset editing roughly double the surface (CSRF, idempotency,
per-operator permissions), and most of the value is in seeing a failed job's
`error_code` without composing a curl command.

The workflow tooling already exists (`/gsd:ui-phase`, `/gsd:sketch`,
`/gsd:ui-review`), so the process cost is low; the product cost is the auth
decision above.

## Not now

v1.14 is not closed — phase 63 is in review and phase 64 (tectonic) has not
started. This belongs in v1.15 scoping.
