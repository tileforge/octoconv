---
created: 2026-10-17
area: cad-live-proofs
severity: low
origin: quick 261017-doa sweep
audit_acknowledged:
  milestone: v1.16
  at: 2026-10-18
---

# Two selection steps read load-contaminated rows

## What

Quick 261017-doa followed every contamination flag in the seven live-proof scripts to its verdict
(`.planning/quick/261017-doa-codex-round-2-contaminated-loadproof-tim/evidence/sweep.txt`). Two
selection steps read rows without their load flag:

1. `scripts/keda-cad-downscale-survival.sh` long_select (line 437) takes the largest
   `attempt_wall_s` of every `INWORKER_ROW ... status=done` row, `load_clean=0` rows included. A
   loaded row can raise a candidate's wall into the band.
2. `scripts/cad-dxf-bounds.sh` `select()` (line 4303) builds BASIS W through `load_all`, which drops
   host-sleep rows only, not the rows the quiet pass marks `load_contaminated_row`.

## Why it was not fixed

- Neither script has a stub harness. The quick task was limited to stubs, with no live run.
- Both errors push one way only:
  - long_select: the live LONG wall must still lie in the band (line 980) and LONG's quiet line still
    gates (line 979). A wrong pick can fail the run but cannot pass it. The only `load_clean=0` row on
    file (76-03-inworker.txt, dxf_pdf L170030 rep 3) is not a render candidate, so the official pick
    M2 is not affected.
  - select(): a loaded wall gives a larger W and a smaller r_upper, so the classification gets stricter.
    derive takes W from clean rows (lines 4699 and 4732).
- One point is still open. Nobody traced whether a select-step classification reaches a published claim
  without derive judging it again.

## Next step

Add a stub harness for one of the two scripts. Then either filter on the flag (`kv("load_clean") != "0"`
in long_select; `_clean` rows in select), or pre-register the current behaviour in a RULE line.
