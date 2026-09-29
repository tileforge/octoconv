---
schema_version: 1
open_count: 0
waived_count: 0
fixed_count: 1
total_count: 1
last_updated: 2026-09-29T13:34:18.528Z
---

# Broken Windows Ledger

> Cross-phase defect register. With `workflow.windows_enforce` enabled, `/gsd-ship` blocks while `open_count > 0`.
> Waive with `gsd-tools windows waive <id> "<reason>"` (reason required).
> Mark fixed with `gsd-tools windows fixed <id>`.

| id | phase | kind | file | line | description | status | reason | recorded_at | resolved_at |
|----|-------|------|------|------|-------------|--------|--------|-------------|-------------|
| 1 | 69 | unrun-verify | internal/convert/chromium.go |  | 69-03 Task 2: the required chromium.go literal-low-level-exec inversion (proving TestConfinedClassesExecOnlyThroughTheSandbox fails naming chromium.go) could not be run -- the harness's own security-weakening tool classifier denied the edit and a follow-up read; 3/4 required class-closure inversions were completed and reverted, this one is open | fixed |  | 2026-09-26T20:46:31.900Z | 2026-09-29T13:34:18.528Z |

````json
[
  {
    "id": 1,
    "kind": "unrun-verify",
    "phase": "69",
    "file": "internal/convert/chromium.go",
    "line": null,
    "description": "69-03 Task 2: the required chromium.go literal-low-level-exec inversion (proving TestConfinedClassesExecOnlyThroughTheSandbox fails naming chromium.go) could not be run -- the harness's own security-weakening tool classifier denied the edit and a follow-up read; 3/4 required class-closure inversions were completed and reverted, this one is open",
    "status": "fixed",
    "reason": "",
    "recorded_at": "2026-09-26T20:46:31.900Z",
    "resolved_at": "2026-09-29T13:34:18.528Z",
    "milestone": "v1.15"
  }
]
````
