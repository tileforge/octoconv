---
phase: 71-evaluation-gate
plan: 08
subsystem: infra
tags: [cad, image-size, cold-pull, registry, dind, decision-record, no-wiring, eval-gate]
status: complete

requires:
  - phase: 71-07
    provides: pruned and full images of the pinned OCP build, prune and licence evidence, Landlock candidates
  - phase: 71-06
    provides: PIN_DECISION, memory limit, ceilings, caps, threads, tessellation constants
  - phase: 71-05
    provides: user acceptance of the renderer (approved)
provides:
  - scripts/cad-image-size-measure.sh (uncompressed and per-layer compressed size, cold pull into a fresh dind through registry:2, calculated network time)
  - evidence 71-08-image-size.txt (IMAGE_SIZE, LAYER, REGISTRY, DIND, COLD_PULL, COLD_PULL_DIST, PULL_CALC, FLEET, D15)
  - 71-DECISION.md (machine-readable DECISION summary, decision sections, coverage table, artifact index)
  - evidence 71-08-no-wiring.txt (criterion 5 gate, last line NO-WIRING)
affects: [72, 73, 74, 75, 76]

actuals:
  tokens: 20500
  tasks: 2
  commits: 3
plan_head_before: 5e5844e1c82bdd68a4d2aed638f97fd9fc99d9ae
plan_head_after: 600499226267f1e4990211e627c5d281be66d299

tech-stack:
  added: []
  patterns: [digest-pinned third-party images for the measurement, fresh throwaway daemon per cold pull, machine-readable decision summary cross-checked against evidence lines]

key-files:
  created:
    - scripts/cad-image-size-measure.sh
    - .planning/phases/71-evaluation-gate/71-DECISION.md
    - .planning/phases/71-evaluation-gate/evidence/71-08-image-size.txt
    - .planning/phases/71-evaluation-gate/evidence/71-08-no-wiring.txt
  modified: []

key-decisions:
  - "Image size is recorded, not thresholded (D-15): pruned 1083.0 MiB uncompressed, 299.3 MiB compressed, cold pull median 6.38 s on loopback; the cost goes to phase 76 as an input for keda.cad.minReplicaCount"
  - "71-DECISION.md records the user's choices as given: OCP 7.9.3.1.1 against the formula's 8.0.1.0.0, VTK + OSMesa approved, D-18 outcome record, lock files approved, libssl3 for the one-off f3d image only"
  - "Every DECISION summary value is cross-checked against its evidence line; the evidence is what the later phases read"

requirements-completed: [EVAL-01, EVAL-02, EVAL-03, EVAL-04]
---

# Phase 71 Plan 08: Image size, cold pull, decision record and no-wiring gate Summary

The last measurement of phase 71 (size and cold pull of the pruned and full CAD images through a local `registry:2` into a fresh throwaway `docker:dind`) is recorded, `71-DECISION.md` collects every decision of the phase with an evidence reference per number, and the criterion 5 gate shows that nothing outside the CAD testdata and the measurement Dockerfile changed against `4e595c0`.

## Accomplishments

### Task 1: image size and cold pull (`ba22957`, gate `SIZE-OK`)

- **Sizes** (`evidence/71-08-image-size.txt#IMAGE_SIZE`): pruned 1083.0 MiB uncompressed, 299.3 MiB compressed (313870924 bytes over 6 layers); full 1358.3 MiB and 366.7 MiB. The compressed size is the sum of the six `LAYER` rows from the registry manifest. The full-minus-pruned difference is 275.3 MiB, equal to `delta_image_mib` of `71-07-prune.txt#PRUNE_SIZE`.
- **Cold pull** (`COLD_PULL`, `COLD_PULL_DIST`): pruned 6.38 s median over 3 pulls (min 6.04, max 7.11), full 7.75 s (one pull). Every row has `fetched` equal to `layers` and `already_exists=0`; the label says `medium=loopback` and `measures=download+decompress+extract over loopback not network`.
- **Calculated network time** (`PULL_CALC`, label `рассчитано`): pruned 25.1 s at 100 Mbit/s, 2.5 s at 1 Gbit/s, 0.3 s at 10 Gbit/s; full 30.8 / 3.1 / 0.3 s.
- **Fleet comparison** (`FLEET`): the largest fleet image on this host is `octoconv-document-worker` at 733.0 MiB; the cad image is larger than any of them. `D15,threshold=none` is recorded.
- **Third-party images** are pinned by index digest and recorded: `registry@sha256:a3d8aaa6...`, `docker@sha256:7dcdfc4a...`. dind runs `--privileged` with only an anonymous volume and the throwaway network `octoconv-cad-pull`; after the run no container, network, volume or `localhost:5055` tag is left, port 5055 is free, and the `octoconv-cad-measure:*` tags are untouched.
- A second full run after a comment-only edit of the script (`--repeats 1`, output to the scratch directory) reproduced the sizes exactly and gave 6.44 s and 7.42 s; the committed evidence is the three-repeat run.

### Task 2: decision record and criterion 5 gate (`6004992`, gates `DECISION-OK` and `NO-WIRING`)

- `71-DECISION.md` (Russian) opens with 26 `DECISION,key=...,value=...,evidence=...` lines, then: what is decided and what stays open, core (EVAL-01), renderer (D-11..D-14, D-16 with a proof per failure condition), DXF 2D path, memory and ceilings (D-07 formula with substituted numbers for each format), caps (D-10), threads and tessellation, image (D-15, D-17), licences (EVAL-04, D-18), Landlock candidates, vocabulary drafts for phase 72, corpus, thresholds and the A1-A10 log (each closed by a measurement or left an assumption), not measured, a coverage table (GOAL, EVAL-01..04, D-01..D-18, research items), criterion 5, artifact index.
- The user's decisions are recorded as given: OCP **7.9.3.1.1** (the formula printed 8.0.1.0.0, `PIN_FORMULA`, user override in `PIN_DECISION`), renderer VTK 9.7.1 + OSMesa `approved_2026-10-04`, D-18 `record`, lock files approved, `libssl3` only in the one-off f3d image.
- The first verify gate cross-checks pin, memory limit, every `ceiling_*_mib` against its `CEILING` line, the D-18 outcome against `71-01-SUMMARY.md`, the pruned size against `IMAGE_SIZE`, that each cited evidence file exists and is tracked, all D-01..D-18 and EVAL-01..04, and the private-corpus phrase. I additionally checked the caps, threads, quality constants, compressed size, cold pull distribution and several quoted numbers by pattern against the evidence (all matched).
- Criterion 5 (`evidence/71-08-no-wiring.txt`): base `4e595c0` resolves, 39 files in scope before the exclusion (38 under `internal/convert/testdata/cad/`, plus `Dockerfile.cad-measure`), none after it; existing `scripts/` files unchanged; the six guard tests print `--- PASS` on go1.26.4 and no `--- FAIL`. Reverse check: an untracked file in `internal/` and `internal/convert/testdata/cad_x.txt` were each listed by the gate (probe files created and removed again).
- **Open items carried into the record:** lawyer review (`нужен OK юриста`, non-blocking; FreeImage, LibRaw, jbigkit GPL-2.0, libgomp), render time guard for phase 76 (stacked duplicates 61.57 s against 1.18 s), private corpus not measured (`реальный поток не замерен`), Landlock sets depend on `LP_NUM_THREADS=2` and `occt_parallel=off`, pod limit 4 GiB deviates from the fleet 1Gi, amd64 not measured.

## Task Commits

| Task | Name | Commit | Files |
| ---- | ---- | ------ | ----- |
| 1 | Image size and cold pull | ba22957 | scripts/cad-image-size-measure.sh, evidence/71-08-image-size.txt |
| 1 | Keep only the external-constraint comment | 9dfd657 | scripts/cad-image-size-measure.sh |
| 2 | Decision record and no-wiring gate | 6004992 | 71-DECISION.md, evidence/71-08-no-wiring.txt |

`commits: 3` is measured with `git rev-list --count 5e5844e..6004992` from the plan ledger and excludes the SUMMARY commit. All three are SSH-signed (`G`).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] A cold pull after `rmi` and `prune` inside one daemon is not guaranteed cold**
- **Found during:** Task 1 design (the daemon uses the containerd snapshotter: image removal does not remove content blobs at once, so a repeat could reuse them without printing `Already exists`).
- **Fix:** a fresh dind container (new anonymous volume) for every pull instead of `docker rmi` plus `docker image prune -af` in one daemon. The check "no layer already present" stays (`dind_empty` before the pull, exit 1 on any `Already exists`). `DIND` records `fresh_daemon_per_pull=1`.
- **Files modified:** scripts/cad-image-size-measure.sh
- **Commit:** ba22957

**2. [Rule 2 - Missing critical functionality] `--entrypoint dockerd` instead of the image's entrypoint**
- **Issue:** with `DOCKER_TLS_CERTDIR` empty the stock entrypoint opens `tcp://0.0.0.0:2375` without TLS; the plan asks for the smallest surface for a privileged container.
- **Fix:** dockerd is started directly with `--host=unix:///var/run/docker.sock` only; verified before the script was written that pulls work (daemon starts, storage driver overlayfs).
- **Commit:** ba22957

**3. [Rule 1 - Bug] Plan said the nofreeimage variant is compared if it exists**
- It does not exist (D-18 outcome is `record`, no `venv-nofreeimage` stage), so the script compares pruned and full only; the check is by image presence, not by flag. After the PR 32 review the presence check and the `nofreeimage` mapping in `common.sh` were removed altogether (the variant advertised a stage that was never built).

### Other notes

- **Comment cleanup.** The first version of the script carried a design header; per the global rule (comments only for external constraints) it was removed in `9dfd657`, leaving one comment (dockerd treats non-localhost registries as TLS-only unless listed). The recorded evidence comes from the earlier text of the script; the later edit changes comments only, and a re-run reproduced the sizes.
- **`f3d` pixel count.** `71-05-SUMMARY.md` says the VTK pixel judge failed 14 of 15 f3d images; the `F3DPIX` evidence lines show 13 `ok=0` and 2 `ok=1`. `71-DECISION.md` uses the evidence (13 of 15).
- **Plan line `GOAL` row** of the coverage table is taken from the ROADMAP phase goal; the plan did not define a separate label.
- **Local images left in place:** `registry` and `docker` pulled by digest for this plan stay in the local image cache (about 25 MB and 357 MB); nothing else was deleted. Free disk went from 12 to 12 GiB (`df -g`).
- **Private-corpus statement.** Evidence has no names or paths of private files (`files=0`); the record repeats only the status line.

## Auth Gates

None. All three commits were signed on the first attempt.

## Known Stubs

None.

## Threat Flags

None beyond the plan's register. T-71-23: dind is pinned by digest, mounts no host directory (anonymous volume only), is attached only to `octoconv-cad-pull`, publishes no port, and is removed with its volume in the EXIT trap; the gate shows no leftover container. T-71-24: the DECISION summary is machine-checked against evidence and every cited evidence file is tracked. T-71-25: the gate and guard tests are green on go1.26.4 and were proven in both directions. T-71-26: the record takes only numbers, sha256 and the `PRIVATE_CORPUS` status. T-71-SC: both third-party images are pinned by index digest and recorded in the evidence; no pip, npm or cargo package was installed.

## Addendum: image tag after the PR 32 keep-list fix

- The final image is now `octoconv-cad-measure:ocp7.9.3.1.1-pruned-dc621b94ee5d` (image id `08117b26d4d0`); `evidence/71-08-image-size.txt` was measured on the previous tag `...-pruned-0d09b7ba39ad` (image id `79f5f6f41f85`), which was kept.
- The size is unchanged: 1135589363 against 1135587708 bytes (+1655 bytes, the `vtk_keeplist.py` source), both 1083.0 MiB, 6 layers each; the hash of `/opt/venv` and `/opt/cad-measure` (without `vtk_keeplist` and the helper pyc files) is equal and the five iso renders are byte-identical (`evidence/71-07-prune-arch.txt`). Compressed size and cold pull were not re-measured, so those numbers are the previous tag's; the registry and `docker:dind` run was skipped as the content is identical.
- amd64 image size and cold pull remain not measured; an emulated amd64 build now passes the prune (`evidence/71-07-prune-amd64.txt`), which is a build proof only.

## Self-Check: PASSED

- Files present: scripts/cad-image-size-measure.sh, 71-DECISION.md, evidence/71-08-image-size.txt, evidence/71-08-no-wiring.txt.
- Commits ba22957, 9dfd657 and 6004992 on gsd/phase-71-evaluation-gate; ledger count 3.
- Gates: `SIZE-OK`, `DECISION-OK`, `NO-WIRING`; STATE.md and ROADMAP.md untouched; `internal/convert/exec.go` untouched.
