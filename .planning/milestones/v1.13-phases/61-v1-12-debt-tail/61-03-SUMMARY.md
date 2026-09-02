---
phase: 61-v1-12-debt-tail
plan: 03
subsystem: docs, convert
tags: [doc-sync, readme, operator-runbook, inversion-proof]
requires:
  - plan: 61-01
    provides: "The command names this section documents"
provides:
  - "Self-consistent quota section; engine list under a two-way doc-sync test"
  - "The quota ↔ webhook-worker coupling, written down before an incident needs it"
affects: []
status: complete
---

# Plan 61-03 — Summary

Commit `91f2f37`. Closes DEBT-10 and the documentation half of DEBT-09.

## Three things the README said, or failed to say

**It contradicted itself.** Line 242 claimed quotas are *"enforced fail-closed
before the upload touches storage"*; twelve lines later the same section
explained the opposite, and that passage is backed by a measurement. The promise
was rewritten to match the explanation rather than the explanation softened: the
guarantee is **no over-admission**, not "nothing is uploaded".

**It hardcoded a count.** *"The seven known engine classes"* — and phase 60 adds
an eighth, which would make that sentence false from a commit that never opens
the README. Replacing seven with eight only moves the problem to the ninth, so
the classes are **enumerated** and held equal to `convert.AllEngines()` by a
two-way doc-sync test.

One-way checking would miss both halves, and both have happened in this
repository: a class in the document but not the code promises something that
does not exist; a class in the code but not the document leaves an operator
unable to set a limit they are entitled to.

**It said nothing about the coupling.** The daily cap counts in-flight jobs; a
job leaves that count only on a terminal transition; stranded jobs are normally
recovered by the reconciler's sweeper — which lives inside `cmd/webhook-worker`
under a fleet-wide advisory lock. **With every webhook-worker down, "clients are
running out of quota" is a symptom of a stopped worker, not of client traffic.**

Written as an operational fact rather than a defect warning, because the
behaviour is correct and is the price of the daily cap being real.

## Inversions, each naming its own cause

| Inversion | Message |
|---|---|
| drop `pandoc` from the list | `missing from the README: [pandoc]` |
| invent `quantum` | `present in the README but not declared in Go: [quantum]` |
| restore "seven known engine" | names the exact phrase found |

The count guard is deliberately **narrow** — a handful of spellings, listed in
the code with an invitation to extend them knowingly. A test that cries wolf on
unrelated prose gets deleted rather than obeyed.
