---
schema_version: 1
open_count: 0
waived_count: 0
fixed_count: 2
total_count: 2
last_updated: 2026-09-30T21:38:46.726Z
---

# Broken Windows Ledger

> Cross-phase defect register. With `workflow.windows_enforce` enabled, `/gsd-ship` blocks while `open_count > 0`.
> Waive with `gsd-tools windows waive <id> "<reason>"` (reason required).
> Mark fixed with `gsd-tools windows fixed <id>`.

| id | phase | kind | file | line | description | status | reason | recorded_at | resolved_at |
|----|-------|------|------|------|-------------|--------|--------|-------------|-------------|
| 1 | 69 | unrun-verify | internal/convert/chromium.go |  | 69-03 Task 2: the required chromium.go literal-low-level-exec inversion (proving TestConfinedClassesExecOnlyThroughTheSandbox fails naming chromium.go) could not be run -- the harness's own security-weakening tool classifier denied the edit and a follow-up read; 3/4 required class-closure inversions were completed and reverted, this one is open | fixed |  | 2026-09-26T20:46:31.900Z | 2026-09-29T13:34:18.528Z |
| 2 | 70 | unmet-truth | scripts/sandbox-liveproof.sh |  | 70-05: eight-class gate ends FAILURES PRESENT (40 checks run): html large-document-without-dev-shm fails at its negative control (Chromium 154 vs 153 in phase 69); reproduces on 705f962, not caused by phase 70; see phases/70-document-on-amd64/deferred-items.md | fixed |  | 2026-09-30T16:47:45.781Z | 2026-09-30T21:38:46.726Z |

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
  },
  {
    "id": 2,
    "kind": "unmet-truth",
    "phase": "70",
    "file": "scripts/sandbox-liveproof.sh",
    "line": null,
    "description": "70-05: eight-class gate ends FAILURES PRESENT (40 checks run): html large-document-without-dev-shm fails at its negative control (Chromium 154 vs 153 in phase 69); reproduces on 705f962, not caused by phase 70; see phases/70-document-on-amd64/deferred-items.md",
    "status": "fixed",
    "reason": "",
    "recorded_at": "2026-09-30T16:47:45.781Z",
    "resolved_at": "2026-09-30T21:38:46.726Z",
    "milestone": "v1.15"
  }
]
````
