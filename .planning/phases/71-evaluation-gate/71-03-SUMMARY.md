---
phase: 71-evaluation-gate
plan: 03
subsystem: infra
tags: [cad, corpus, provenance, licences, size-ladder, counters, nauo, ezdxf, nist, nasa, freecad-library]
status: complete

requires:
  - phase: 71-02
    provides: measurement image and common.sh, the tracer part, the synthetic fixture generator
provides:
  - 18 committed public files with per-file source, author, licence and sha256 (PROVENANCE.md)
  - scripts/cad-corpus-manifest.tsv, 17 large files pinned by sha256 and licence
  - scripts/cad-corpus-fetch.sh (--check, --dest, archive members, licence gate, rm on mismatch)
  - scaling modes of scripts/cad-corpus-gen.py and a 33-rung deterministic ladder up to 200 MiB and 100000 NAUO
  - scripts/cad-measure/counters.py, stdlib D-10 counters independent of OCP and ezdxf
  - scripts/cad-corpus-inventory.sh and evidence/71-03-corpus-inventory.txt
affects: [71-04, 71-05, 71-06, 71-07]

actuals:
  tokens: 29000
  tasks: 2
  commits: 2
plan_head_before: 31d25d0740fb65437b4a837df609df309ba57933
plan_head_after: b953c551b1a332e2c173a01efc30c10235eb39c0

tech-stack:
  added: []
  patterns: [manifest as the single place for pinned digests, private corpus printed as closed vocabularies only, status line derived from a file count, derived files labelled with base and coefficient]

key-files:
  created:
    - scripts/cad-corpus-manifest.tsv
    - scripts/cad-corpus-fetch.sh
    - scripts/cad-corpus-inventory.sh
    - scripts/cad-measure/counters.py
    - internal/convert/testdata/cad/public/ (18 files)
    - .planning/phases/71-evaluation-gate/evidence/71-03-corpus-inventory.txt
  modified:
    - scripts/cad-corpus-gen.py
    - internal/convert/testdata/cad/PROVENANCE.md

key-decisions:
  - "Commit threshold 2 MiB per file and 15 MiB over testdata/cad (assumption A5): the committed corpus is 6.9 MB on disk, the largest file 1.4 MB"
  - "Two STEP-copies series: a small committed base (ladder from 1 MiB, calibration of the replication by OCP volume) and the heaviest real mechanical file of the manifest, a SolidWorks 2019 assembly (ladder from 17 MiB)"
  - "NIST PMI STEP files are tagged PD on the strength of the NIST disclaimer and US federal authorship; the D2MI archive says public domain in so many words"
  - "In private mode counters.py prints sha256, format, sizes and counters, and string-valued fields (units, code pages) only from closed vocabularies, never raw text"

patterns-established:
  - "A SCALED line is printed only after the rung passed its structural check; a failed rung exits non-zero and writes no SCALED line"
  - "An inventory group whose CNT line count differs from its file count is an error, not a smaller measurement"

requirements-completed: [EVAL-02]
---

# Phase 71 Plan 03: Public corpus, size ladder and D-10 counters Summary

The CAD corpus now has a licence and a sha256 on every file: 18 small public files are committed, 17 large ones are pinned in a manifest that a fetch script verifies, 33 deterministic rungs reach 200 MiB of STEP, binary STL and ASCII STL, 50 MiB of DXF and 100000 NAUO instances, and the D-10 counters for all 87 corpus files plus the private-stream status are in one evidence file. The private directory was empty, so the evidence says `реальный поток не замерен`.

## Accomplishments

- **Committed public files** (`internal/convert/testdata/cad/public/`, 19 with the 71-02 tracer part): FreeCAD-library parts under CC-BY-3.0 (an 18-NAUO V-Slot assembly, a 3-NAUO SC8UU, a 2-NAUO LM8UU, a corner bracket), four NIST STEP files under PD (an NX 8.5 export in inch units, two AP242 files with PMI whose unit is `CONVERSION_BASED_UNIT` in `MILLIMETRE` and lower-case `inch`, one carrying 229 `\X2\` escapes), two NASA binary STL under PD, eight DXF from the ezdxf repository under MIT (hatches, blocks, nested block dimensions, an ACAD_TABLE, a georeferenced file, an R2007 file, an ACIS file). Every one has source URL pinned to a commit or an archive digest, author, licence token, attribution and sha256 in PROVENANCE.md.
- **Manifest and fetcher**: 17 large files (a SolidWorks assembly of 9 MB with 102 NAUO, a 62-NAUO motor of 18 MB, two real FreeCAD IGES files with no solid and no shell, NIST STEP up to 6 MB, NASA STL from 12 to 52 MB). `scripts/cad-corpus-fetch.sh` downloaded all 17 and `--check` passes. Negative runs: a licence outside the allowed list exits 1 before any download or directory is created, a wrong sha256 prints have/want, removes the file and exits 1, a wrong archive digest exits 1, one appended byte in a cache copy makes `--check` exit 1.
- **Size ladder** (`$OCTOCONV_CAD_CACHE/scaled`, 33 files, 1.8 GB, not committed): step-copies from a small real base 1 to 200 MiB (nine rungs) and from the heaviest real part 17 to 196 MiB (five rungs), step-nauo 10 to 100000 instances (a 65 MB file with one prototype and 100000 NAUO), stl-tile 105 and 196 MiB, stl-ascii 1 to 200 MiB (seven rungs), dxf-replicate 2 to 49 MiB (up to 692 copies of the modelspace). All 33 print `check=ok`; the repeated runs gave identical sha256 values for every series. OCP read the smallest rung of each step-copies series with exactly k times the base solids and volume.
- **Counters** (`scripts/cad-measure/counters.py`): STEP entities, NAUO, products, MANIFOLD_SOLID_BREP, unit text, originating system, non-ASCII bytes, `\X2\` escapes; IGES type histogram, solid flag, unit flag and name, newline-free flag; STL kind, triangles, header-starts-solid; DXF version, code page, `$INSUNITS`, INSERT, MINSERT, blocks, DIMENSION, HATCH, entity count, group 999. The first run over the real files already shows what D-10 will need: real assemblies have 2, 3, 18, 62 and 102 NAUO, the real STEP spellings of inch are `CBU:INCH`, `CBU:inch` and `CBU:MILLIMETRE` as well as `SI:MILLI_METRE`, and none of the public DXF files has a MINSERT.
- **Inventory** (`evidence/71-03-corpus-inventory.txt`): HDR (image `ocp7.9.3.1.1-full-16d52a0d41e3`, arm64, kernel 7.0.14-orbstack, host noise 12 containers), `INV` lines (committed 37, fetched 17, scaled 33), 87 CNT lines, and `PRIVATE_CORPUS,files=0,status=реальный поток не замерен`.
- **Private-stream handling proven**: with the variable unset and with an empty directory the line above is printed; with one file named `SECRET-client-x.step` the output is `files=1,status=measured`, one anonymous CNT line, a `PRIVATE_COUNTERS,files=1,counted=1,unreadable=0` line and a `PRIVATE_SIZES` line, and the file name appears nowhere.

## Task Commits

| Task | Name | Commit | Files |
| ---- | ---- | ------ | ----- |
| 1 | Public corpus with licences, manifest and verifying fetcher | b848120 | public/ (18 files), PROVENANCE.md, scripts/cad-corpus-manifest.tsv, scripts/cad-corpus-fetch.sh |
| 2 | Size ladder, D-10 counters and inventory | b953c55 | scripts/cad-corpus-gen.py, scripts/cad-measure/counters.py, scripts/cad-corpus-inventory.sh, PROVENANCE.md, evidence/71-03-corpus-inventory.txt |

`commits: 2` is measured with `git rev-list --count 31d25d0..b953c55` and excludes the SUMMARY commit.

## Gates

- Task 1: `CORPUS-OK`, `PROVENANCE-COMPLETE`, `TAMPER-DETECTED`.
- Task 2: `INVENTORY-OK`, `PRIVACY-OK` (run twice, the second time after the counters were hardened).
- `git diff --name-only 4e595c0 -- internal cmd deploy docker-compose.yml | grep -v '^internal/convert/testdata/cad/'` is empty; `internal/convert/exec.go` untouched; `scripts/cad-corpus-gen.py` still reproduces the committed `synthetic/` directory byte for byte in the image.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] DXF counters lost the header after the first code 2 value**
- **Found during:** Task 2 (`$INSUNITS` printed `-` for every real DXF)
- **Issue:** header variables with group code 2 (`$DIMSTYLE`, `$CLAYER`, ...) were taken for section names, so the parser left HEADER early and missed `$INSUNITS`.
- **Fix:** a code 2 value names a section only right after `0 SECTION`.
- **Files modified:** scripts/cad-measure/counters.py
- **Commit:** b953c55

**2. [Rule 1 - Bug] IGES global-section parameters were shifted by two**
- **Found during:** Task 2 (`unit_name=0.01` for a millimetre file)
- **Issue:** empty default delimiter parameters were dropped from the parameter list.
- **Fix:** the first two parameters are always present, empty when defaulted. Millimetre files now read `unit_flag=2,unit_name=MM`.
- **Commit:** b953c55

**3. [Rule 1 - Bug] STEP-copies sizes were up to 12% above the target**
- **Found during:** Task 2 (200 MiB rung came out at 225 MiB)
- **Issue:** the copy count came from the base length, but renumbered ids are longer.
- **Fix:** copies are added until the real renumbered length is nearest the target; the 200 MiB rungs are now 200.0 and 196.0 MiB.
- **Commit:** b953c55

**4. [Rule 2 - Missing critical] Private mode printed string content**
- **Found during:** Task 2 privacy test, by feeding a file with `CONVERSION_BASED_UNIT('CLIENTNAME',...)`
- **Issue:** unit names, code pages and `$ACADVER` came from the file and were printed verbatim, which the plan's prohibition (no content from the private corpus) does not allow.
- **Fix:** without `--names` those fields are printed only when they match a closed vocabulary, otherwise `other`; the originating system is printed only with `--names`.
- **Commit:** b953c55

**5. [Rule 2 - Missing critical] An inventory group could silently shrink**
- **Issue:** a failed bind mount (the OrbStack flake recorded in 71-02) would give fewer CNT lines than files and still print a clean-looking file.
- **Fix:** the inventory exits 1 when a group's CNT lines differ from its file count or any line is an error. The private group reports `counted` and `unreadable` instead of failing, so unreadable private files are visible rather than hidden.
- **Commit:** b953c55

### Plan assumptions that did not hold

- **Exporter diversity of committed parts.** The plan asked for parts from SolidWorks, Creo, Inventor and others. Every FreeCAD-library part that was sampled (about 120 headers) is FreeCAD-exported, as in 71-02. Non-FreeCAD origin among committed files comes from NIST only: an NX 8.5 export and three files whose exporter NIST removed. A real SolidWorks 2019 assembly exists, but at 9 MB it is manifest-only. PROVENANCE.md says so under "Not found in open form".
- **IGES.** Two of the three FreeCAD-library IGES files are real surface-and-curve files (no entity 186, no 514) but 3 MB and 11 MB, so both are manifest-only and the surfaces-only category is closed by a real fetched file and the synthetic ones. The third (648 bytes) holds one group entity and no geometry and was not taken.
- **Ladder from one base.** With the 9 MB heaviest base, targets of 1 to 10 MiB cannot be two or more copies, and the plan's OCP check on a 1 to 2 MiB rung needs a small base. The generator therefore skips rungs below two copies (printing `SCALED_SKIP`) and the ladder uses two bases; the small-base series carries the OCP calibration, the heavy-base series the real assembly geometry.
- **ASCII STL.** The plan converts the binary rungs to ASCII; the generator writes ASCII rungs from the first N triangles of the same tiling sequence, so geometry matches the binary ladder without a 200 MiB intermediate.

### Other notes

- **Licence points for the user, none blocking.** (1) The NIST PMI page says the CAD models and STEP files "can be used without any restrictions" and asks for acknowledgement; the files are US federal work. The three committed PMI files (`nist_ctc_01`, `nist_ctc_05`, `nist_stc_06`) rely on that, with the statement quoted in PROVENANCE.md. The D2MI archive states public domain under 17 USC 105 explicitly; if the PMI wording is judged too weak, the three files can be dropped without losing a category (D2MI gives the NX inch file). (2) The NASA repository lists non-NASA contributors (Ball Aerospace, DigitalSpace, Conti) and does not attribute files; the files taken are NASA programme models and every NASA file carries that caveat. (3) FreeCAD-library per-part authorship is not verified (A8), as in the plan.
- **DXF exporter is unverified.** Of the eight committed DXF files only the use as an input in the ezdxf repository (read by an example or a test) suggests a foreign origin, for `acad_table_with_blk_ref`, `houses_of_parliament_georeferenced` and `3dsolids`; the headers are anonymised. PROVENANCE.md marks every one "possibly written by ezdxf" (Pitfall 26) rather than claiming a CAD exporter.
- **Image tag changed twice** because `counters.py` lives in `scripts/cad-measure` (`...-5ce4cfcb342a`, then `...-16d52a0d41e3` after the vocabulary change). Six `octoconv-cad-measure` tags now exist locally; none was deleted. The synthetic fixtures' generation command in PROVENANCE.md still names the 71-02 tag, whose image exists, and the 71-03 image reproduces the committed `synthetic/` bytes.
- **Disk.** Free space fell from 17 to 15 GiB (cache 2.1 GB: fetched 263 MB, scaled 1.8 GB). The 5 GiB precondition held throughout.
- **Time and memory of generation.** The whole ladder is generated in about 3 minutes at `--cpus 2 --memory 6g`; the 100000-instance file took 56 seconds including the smaller counts. Nothing was killed, so none of this says where reading these files breaks; that is 71-06.
- **GitHub code search** returned a rate-limit error once; the later runs used the core API only.

## Known Stubs

None. `scripts/cad-corpus-gen.py --scaled-from` has no mode for IGES scaling; no IGES rung was required.

## Threat Flags

None beyond the plan's register. T-71-06 (tampering) is mitigated and proven by the mismatch, wrong-archive and appended-byte runs; T-71-07 (private disclosure) by the file-name test and the closed-vocabulary test; T-71-08 (licences) by the allow-list checks on manifest and PROVENANCE.md before the commit; T-71-09 is accepted, the ladder files are only generated and read in containers with `--cpus` and `--memory`.

## Self-Check: PASSED

- scripts/cad-corpus-manifest.tsv, scripts/cad-corpus-fetch.sh, scripts/cad-corpus-inventory.sh, scripts/cad-measure/counters.py, scripts/cad-corpus-gen.py (scaled modes): present
- internal/convert/testdata/cad/public/ (19 files), PROVENANCE.md with Fetched, Derived (not committed), Coverage and Licence notices: present
- .planning/phases/71-evaluation-gate/evidence/71-03-corpus-inventory.txt: present, 87 CNT lines, `PRIVATE_CORPUS` line
- commits b848120 and b953c55: present on gsd/phase-71-evaluation-gate
- gates printed `CORPUS-OK`, `PROVENANCE-COMPLETE`, `TAMPER-DETECTED`, `INVENTORY-OK`, `PRIVACY-OK`
