# Roadmap: OctoConv

## Milestones

- ✅ **v1.0–v1.12** — Phases 1–55 (archived; see `.planning/milestones/`)
- ✅ **v1.13 Inspect & Operations** — Phases 56–61 (shipped 2026-09-02)
- 📋 **v1.14** — not yet defined (`/gsd:new-milestone`)

## Phases

<details>
<summary>✅ v1.13 Inspect &amp; Operations (Phases 56–61) — SHIPPED 2026-09-02</summary>

- [x] Phase 56: JobRunner Consolidation & Structured Error Codes (7/7 plans) — 2026-08-08
- [x] Phase 57: Operation Dimension & Output Roles (7/7 plans) — 2026-08-09
- [x] Phase 58: Synchronous Inspection (8/8 plans) — 2026-08-18
- [x] Phase 59: Born-Digital PDF Routing (7/7 plans) — 2026-08-30
- [x] Phase 60: Sanitize — Eighth Engine Class (6/6 plans) — 2026-09-02
- [x] Phase 61: v1.12 Debt Tail (3/3 plans) — 2026-08-30

Full detail: `.planning/milestones/v1.13-ROADMAP.md`
Requirements: `.planning/milestones/v1.13-REQUIREMENTS.md` (28/28 complete)
Summary: `.planning/MILESTONES.md`

</details>

<details>
<summary>✅ v1.0 – v1.12 (Phases 1–55) — archived</summary>

Each milestone's full phase detail and requirements live in
`.planning/milestones/v{X.Y}-ROADMAP.md` and `-REQUIREMENTS.md`.

</details>

## Progress

| Phase | Milestone | Plans | Status | Completed |
|-------|-----------|-------|--------|-----------|
| 56. JobRunner Consolidation & Structured Error Codes | v1.13 | 7/7 | Complete | 2026-08-08 |
| 57. Operation Dimension & Output Roles | v1.13 | 7/7 | Complete | 2026-08-09 |
| 58. Synchronous Inspection | v1.13 | 8/8 | Complete | 2026-08-18 |
| 59. Born-Digital PDF Routing | v1.13 | 7/7 | Complete | 2026-08-30 |
| 60. Sanitize — Eighth Engine Class | v1.13 | 6/6 | Complete | 2026-09-02 |
| 61. v1.12 Debt Tail | v1.13 | 3/3 | Complete | 2026-08-30 |

## Carried into the next milestone

Not blockers for the close; each is recorded in `.planning/todos/pending/`:

- **probe has no page ceiling** — the timeout measurement showed cost is per
  page (~2.0 s/page for scans), so an over-long document fails on timeout
  rather than being refused at admission the way OCR refuses with
  `too_many_pages`.
- **Three P2 findings from PR #12 round 8**, merged unfixed by decision:
  multi-chunk ICC profiles, `endstream` as a substring inside binary stream
  data, and a crafted relationship that removes a package's own main part.
- **API startup crashloops until Postgres is reachable.**
- **Inspect honest-reporting open items.**
- **The reconciler soak test shares a database with parallel packages** and
  intermittently sweeps another package's job.

Dormant seeds (15) live in `.planning/seeds/` — including SEED-022, tectonic as
a ninth engine class for `latex → pdf`.

