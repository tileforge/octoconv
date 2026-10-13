---
phase: 75-dxf-2d-pipeline
plan: 03
subsystem: cad-worker guard (DXF)
tags: [cad-guard, dxf, ezdxf, differential, block-graph, bombs, cost]

requires:
  - phase: 75-dxf-2d-pipeline
    provides: 75-01 engine image, DXF fixtures and probe pattern; 75-02 production cad-dxf (--max-drawn-entities, recursion and drawn-entity backstops)
provides:
  - "cad-guard --format dxf --max-expanded-inserts N --max-expanded-entities N --max-nesting N --max-abs-coordinate X SRC (stage guard, exit 0 ok, 2 unreadable, 4 too_complex, 64 usage)"
  - "guard DXF reasons: bad_group_code, non_numeric_value, coordinate_out_of_range, cyclic_block_reference, nesting_too_deep, expanded_inserts_over_cap, expanded_entities_over_cap, cannot_count; counters entities, expanded_inserts, expanded_entities, nesting, blocks (+ line, code on numeric findings), saturating at cap + 1"
  - "scripts/cad-dxf-guard-diff.sh diff|cost [--out FILE]"
  - "scripts/cad-corpus-gen.py --kind dxf-guard-stress --shape chain|fanout|blocks|wide --targets MiB"
  - "evidence/75-03-guard.txt with DERIVED,key=cad_dxf_nesting_bound,value=16 (M=4 over 17 corpus files)"
affects: [75-05, 75-06, 75-09, 76]

actuals:
  tokens: 37500
  tasks: 2
  commits: 7
plan_head_before: a9f6b57d5e45439daf840a6735388e859c13fff9
plan_head_after: 1cab62443f5e029b7d55c346d24cace1c4dfdc9d

tech-stack:
  added: []
  patterns:
    - "Guard emulates ezdxf's dxf_info pass (utf-8 ignore, first 5 known vars, string compare against AC1021, codepage suffix map) to pick the name codec, then keys blocks by decode(surrogateescape).lower()"
    - "Over-approximating block graph: every code-2 value of a reference is an edge, every candidate name of a BLOCK (code 2, owner BLOCK_RECORD) is unioned into one node, *Model_Space and *Paper_Space alias the root"
    - "Handle-referenced blocks (MLEADER content and arrows, LEADER arrows via DIMSTYLE, MLEADERSTYLE) join the graph through BLOCK_RECORD handles; style blocks hang off one transparent shared node"
    - "Differential with expectations and policy declarations frozen (read-only file, cmp) before any G, A or H run"

key-files:
  created:
    - scripts/cad-dxf-guard-diff.sh
    - .planning/phases/75-dxf-2d-pipeline/evidence/75-03-guard.txt
  modified:
    - cmd/cad-worker/helper/cad-guard
    - internal/convert/cadguard_test.go
    - scripts/cad-corpus-gen.py

key-decisions:
  - "75-03: block keys follow ezdxf 1.4.4's strict reader exactly: no \\U+ or \\M+ decoding (only ezdxf.recover decodes them), no stripping, lower() after the document codec"
  - "75-03 (Rule 2, accepted by the orchestrator): the guard follows MLEADER/MULTILEADER (341, 342, 344, 345, 1005), LEADER (DIMSTYLE 341-344, R12 names 5-7) and MLEADERSTYLE (341, 343) block handles; a self-referencing MULTILEADER passes ezdxf's audit and ends the unguarded helper in RecursionError"
  - "75-03: integer-code syntax follows ezdxf's TYPE_TABLE (60-79, 90-99, 160-179, 270-299, 370-389, 400-409, 420-429, 440-459, 1060-1071); codes 80-89 are strings in ezdxf"
  - "75-03: expanded_entities counts the model-space entities themselves (an INSERT counts once) plus mult x N(child), as the plan's formula states"
  - "75-03 (orchestrator decision 2): P3 holds except for inputs declared before the run as over a policy bound; those are DIVERGENCE conservative_count or numeric_policy"
  - "75-03 (orchestrator decision 3): at-cap inputs whose H times out at 30 s are admitted_slow facts; 75-05 measures the stage timeout"
  - "75-03: DERIVED nesting bound 16 = 4 x M, M = 4 (dimension_in_nested_blocks) over the 12 committed corpus files and the five cached replicates"

requirements-completed: [CCONV-02, CCONV-03, RNDR-06]

coverage:
  - id: D1
    description: "cad-guard DXF mode decides cycles, nesting, expanded inserts, expanded entities, bad group codes and section-scoped numerics before ezdxf, stdlib only, no cap literal"
    requirement: CCONV-02
    verification:
      - kind: unit
        ref: "internal/convert/cadguard_test.go TestCADGuardDXF* (10 tests) and TestCADGuardHelperHoldsNoNumbersOfItsOwn"
        status: pass
    human_judgment: false
  - id: D2
    description: "Block keys equal ezdxf's on case, code-page, Unicode, escape and space variants"
    requirement: CCONV-02
    verification:
      - kind: integration
        ref: "scripts/cad-dxf-guard-diff.sh diff (DIFF rows variant__case_*, unicode_escape*, space_*)"
        status: pass
    human_judgment: false
  - id: D3
    description: "No input whose unguarded run is failed, crash or timeout passes the guard (except declared at-cap admitted_slow); R-8 scope outside ENTITIES/BLOCKS ends ok"
    requirement: RNDR-06
    verification:
      - kind: integration
        ref: "evidence/75-03-guard.txt SUMMARY,mode=diff unexplained=0; SCOPE x3 result=ok"
        status: pass
    human_judgment: false
  - id: D4
    description: "Guard cost at 118 MiB within 20 s and the 2842.3 MiB budget inside the image"
    requirement: RNDR-06
    verification:
      - kind: integration
        ref: "evidence/75-03-guard.txt GUARD_COST x10, SUMMARY,mode=cost result=ok"
        status: pass
    human_judgment: false

duration: 5h46m wall (15:40 to 21:26 local, including a disk halt)
completed: 2026-10-07
status: complete
---

# Phase 75 Plan 03: cad-guard DXF mode Summary

**A stdlib cad-guard DXF mode that keys blocks exactly as ezdxf 1.4.4 does. It also follows the handle-referenced MLEADER and LEADER blocks, and it rejects every bomb class before ezdxf loads the file. The differential has 105 inputs, 0 unexplained divergences, a derived nesting bound of 16, and a worst cost at 118 MiB of 14.90 s and 1502 MiB.**

## Performance

- **Duration:** first commit 15:40, last 21:26 local time. This includes a halt on the VM disk floor; see "Disk halt".
- **Tasks:** 2 of 2.
- **Files:** 5 changed, 2324 insertions.

## Accomplishments

### Task 1: cad-guard DXF mode

- **Reading:** the guard reads the file as bytes, splits it with universal newlines (`bytes.splitlines` per 4 MiB chunk, carrying the last pair across chunks) and stops at `0/EOF`, as ezdxf does. Group codes go through a per-chunk cache; a code line that is not `[+-]?[0-9]+` is `bad_group_code` with its line number.
- **Name codec:** a first pass copies ezdxf's `dxf_info`:
  - it decodes as UTF-8 and drops undecodable bytes;
  - it needs `0/SECTION 2/HEADER` first;
  - a point-valued variable consumes the y and an optional z tag;
  - it stops after five of `$ACADVER`, `$DWGCODEPAGE`, `$HANDSEED`, `$INSUNITS` and `$INSBASE`;
  - it compares the version as a string, `>= "AC1021"` means UTF-8, otherwise `toencoding` matches the code-page suffix.
  
  Block names are then `decode(codec, "surrogateescape").lower()`. ASCII names are keyed as lower-cased bytes. A non-ASCII name whose lower-case form is ASCII, such as the Kelvin sign, folds onto the same key.
- **Numeric rules, only inside ENTITIES and BLOCKS:**
  - a real code that does not match the real syntax, or is infinite, is `non_numeric_value`;
  - codes 10-39 beyond `--max-abs-coordinate`, or infinite, are `coordinate_out_of_range`;
  - integer codes must have integer or real syntax.
- **Block graph:**
  - INSERT, DIMENSION, ARC_DIMENSION, LARGE_RADIAL_DIMENSION and ACAD_TABLE add edges by code 2. INSERT multiplicity is `max(cols,1) x max(rows,1)`.
  - A BLOCK with no name takes its owner BLOCK_RECORD's name, as ezdxf's `recover_block_name` does.
  - The rejection order is cycle anywhere (Kahn), then longest chain, expanded inserts, expanded entities.
  - All graph work is iterative and saturates at cap + 1.
- **Arguments:** for step and iges, `--max-entities` and `--max-nauo` are optional at the parser and required by an explicit exit 64. For dxf, the four DXF options are required and validated (exit 64).
- **Tests:** 10 `TestCADGuardDXF*` tests sit beside the unchanged STEP and IGES tests, plus the extended token-based no-numbers test. They cover every behavior bullet of the plan, plus the Kelvin sign, a lower-case `$ACADVER`, `\U+` escapes, a nameless block, `*Model_Space` self-insertion, the handle cases, CR/LF/CRLF chunk boundaries, and garbage after EOF.

### Task 2: differential and cost (evidence at 555978d, tracked dirt 1 = the orchestrator's config.json)

- **Diff: 105 inputs.** 49 generated variants, 41 fixtures, 3 reject files and 12 corpus files. Each runs through G (the guard), A (`ezdxf.readfile` plus audit) and H (the unguarded production cad-dxf under its sandbox-probe ruleset with a 30 s deadline).
- **Results:** 73 agree, 32 named divergences, 0 unexplained:

  | Class | Count | Inputs |
  |---|---|---|
  | numeric_policy | 13 | nan, -inf, `1_0`, 1e300 and above-bound coordinates, which H draws as ok |
  | preempted | 9 | inf and 1e400 end H in `failed/draw_exception`; over-cap MINSERTs end H in a timeout |
  | conservative_count | 4 | nest_d17 and nest_d64, ok in H; MINSERT 170032 is ok in H in 12.1 s |
  | audit_blind | 4 | mleader_cycle, leader_cycle, table_cycle, dim_cycle: no audit 104, H `too_complex/recursion`, G cyclic |
  | admitted_slow | 1 | grid_at_entity_cap |
  | reader_rejects | 1 | dxf_truncated |

- **SCOPE:** nonfinite_header, nonfinite_tables and nonfinite_objects are G ok and H ok, so `result=ok`. R-8's scope leaves no retryable crash.
- **DERIVED:** `cad_dxf_nesting_bound=16`, from 4 x M, M = 4 (dimension_in_nested_blocks), over 17 files.
- **FACT:** H is ok up to depth 64 and first ends `too_complex/recursion` at depth 199. The research said 196-200; nothing between 64 and 199 was probed.
- **Cost:** 10 rows inside the image, `/usr/bin/python3` 3.11.2 under `--cpus 2 --memory 4g --memory-swap 4g --network none --user nobody`:

  | Shape | Verdict | Wall s (rep 1 / rep 2) | Peak MiB | % of budget |
  |---|---|---|---|---|
  | chain | too_complex/nesting_too_deep | 10.48 / 10.24 | 1074 | 37.8 |
  | fanout | too_complex/expanded_inserts_over_cap | 6.44 / 6.28 | 219 | 7.7 |
  | blocks | ok | 6.11 / 5.71 | 847 | 29.8 |
  | wide (extra) | too_complex/expanded_inserts_over_cap | 11.00 / 14.90 | 1502 | 52.8 |
  | replicate 118 MiB | ok | 3.03 / 3.00 | 197 | 6.9 |

  The worst row leaves 1.34x headroom under the 20 s bound. The existing 30 s `CAD_GUARD_TIMEOUT` leaves 2.0x on the worst row.

## Deviations from Plan

### Orchestrator decisions (recorded in the evidence)

**1. [Rule 2, accepted as binding] The guard follows MLEADER, LEADER and style block handles (603bfa7).**
- **Why a name-only graph is not enough:** uncommon.dxf has 3 MULTILEADER and 3 LEADER entities. Their block content and arrows are resolved by BLOCK_RECORD handle, not by a code-2 name.
- **Probe:** a MULTILEADER whose context block handle points at its own block passes ezdxf's audit with no error, and the unguarded helper ends `too_complex/recursion`. The DIFF row is `variant__mleader_cycle`: G `too_complex/cyclic_block_reference`, A `audit:clean`, H `too_complex/recursion` (0.50 s), class `audit_blind`. leader_cycle shows the same through DIMSTYLE.
- **Without the change**, the same handle could carry a million-cell MINSERT that the guard never counts. A Go test covers that case.

**2. P3 with declared exceptions.**
- Read literally, P3 would fail the plan's own fixtures: nest_d17, and nonnumeric_nan, nonnumeric_underscore and coord_1e300 are all ok in H, yet R-8 and D-12 require the guard to reject them.
- The declarations are data in the script: the policy column of the generator manifest and `declared_fixtures`. They are frozen in a read-only file before any run.
- The rule itself is the `RULE,key=p3_declared_exceptions` line in the evidence.

**3. admitted_slow.** grid_at_entity_cap times out H at 30 s and is recorded as `FACT,key=admitted_slow`. 75-05 measures the stage timeout. minsert_at_insert_cap was ok in H in 11.9 s.

### Auto-fixed issues

**4. [Rule 1, plan text] Integer code set.** The plan listed 60-99. ezdxf types 80-89 as strings, so the guard follows ezdxf's TYPE_TABLE.

**5. [Rule 1, my test] Expanded-entity expectation.** The plan's formula counts model-space entities themselves, so a grid at the insert cap has 170031 + 1 expanded entities. The test was corrected in 97c10f6.

**6. [Rule 3] No-numbers test.**
- The literal 16 occurs in the existing STEP code: `CHUNK = 16 << 20` and the regex quantifier `{0,16}`.
- The test now tokenizes the helper and rejects numeric literals equal to any cap. It also keeps the substring check for the unique caps.
- `CHUNK` is now `1 << 24`, the same value. STEP and IGES verdicts are unchanged and all their tests pass.

**7. [Rule 2] Speed (6bda940).**
- The first version took 13-17 s on the host for fanout and wide. Since then:
  - pairs are processed per chunk;
  - ASCII names are keyed as bytes;
  - consecutive identical edges are merged;
  - edges are sorted once;
  - leaf children are kept out of the Kahn pass.
- I added a fourth adversarial shape, `wide` (INSERTs naming distinct undefined blocks, 6.2M edges), to the cost rows; it is the worst row.

**8. [Rule 3, generator] table_cycle.** ezdxf's `move_to_layout` left the ACAD_TABLE's owner handle stale, so audit removed the table and H drew nothing. The generator now sets the owner, and H ends `too_complex/recursion`.

### Disk halt

- The first run halted at the start of Task 2: the shared VM had 4.7-5.2 GiB free, below the 6 GiB floor.
- **Inside the rules:** I removed my own tag, ran `docker builder prune -f`, and removed four untagged octoconv compose images (project=octoconv, api and cad-worker, no containers).
- **Fix:** the user freed disk and the run resumed at 10.7 GiB. It ended at 8.7 GiB free.
- **Cleanup:** a stray `cmd/cad-worker/helper/__pycache__`, left by my host profiling harness, broke one image build. It was removed and never committed.

## Known limits (not stubs)

- **Over-approximation:**
  - every code-2 value of a reference is an edge;
  - every candidate BLOCK name is merged;
  - paper space is merged into the root;
  - every annotation reaches every style block through the shared node.

  This can reject a malformed file that ezdxf would draw. None of the 105 inputs needed it beyond the declared policies.
- **Proxy graphics:** blocks reached only through proxy graphics are not modelled. cad-dxf skips ACAD_PROXY_ENTITY. Behind that, the drawn-entity cap and the RecursionError backstop from 75-02 still apply.

## Known Stubs

None.

## Threat Flags

None. The guard stays stdlib-only: no ezdxf, numpy or OCP import, and no cap literal. These paths have no diff against main:
- `internal/convert/exec.go`
- `internal/api`, `internal/worker`
- `Dockerfile.cad-worker`

No dxf pair is registered (`TestDefaultRegistersOnlyAllowlistedCADPairs` passes).

## Verification

- Task 1 gate: `ast.parse(feature_version=(3,11))` passes, and `go test -run TestCADGuard -v` gives 23 PASS, 0 FAIL, 0 SKIP, with 11 `TestCADGuardDXF*` tests passing. The helper has no comment lines; the test diff adds no `//` lines.
- Task 2 gate:
  - `bash -n` passes;
  - both modes exit 0 at 555978d;
  - there are no STOP lines;
  - SUMMARY diff shows `unexplained=0 result=ok` and SUMMARY cost shows `result=ok`;
  - the DERIVED line is at 16;
  - 3 SCOPE rows are ok and none failed;
  - there are 10 GUARD_COST rows.
- The DIFF rows include all five case variants, unicode_escape, both space variants, dim_cycle, table_cycle and mleader_cycle. Every DIVERGENCE row has class, G, A and H.
- `GOTOOLCHAIN=go1.26.4 go build ./... && go vet ./...` is clean, and `go test ./... -count=1` is green.
- No `engine75` image tag remains. VM free disk is 8.7 GiB.
- I did not append to the broken-windows ledger: no `gsd_run` resolver was in this shell, and the ledger is optional.

## Self-Check: PASSED

- All five key files exist.
- Commits c08865e, 97c10f6, 603bfa7, 6bda940, 8bab555, 555978d and 1cab624 are ancestors of HEAD and SSH-signed (G).
- STATE.md, ROADMAP.md and config.json have no diff in a9f6b57..HEAD.
