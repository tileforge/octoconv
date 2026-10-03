# CAD corpus — provenance

These files are **inputs** to the tests of phases 71-75, never their output. Each one exists to make
one specific claim about the CAD helpers observable rather than argued. Replacing a file means
replacing its section below.

Licences allowed for committed files (phase 71, D-04): PD, CC0-1.0, CC-BY-3.0, CC-BY-4.0, MIT,
BSD-2-Clause, BSD-3-Clause, and `generated` for the synthetic output of `scripts/cad-corpus-gen.py`.
CC-BY-SA and NC are not taken. Large files are not committed; their pins (URL, sha256, licence) are in
`scripts/cad-corpus-manifest.tsv` and `scripts/cad-corpus-fetch.sh` downloads them into a cache outside the repository.

Commit threshold (not set by D-03, a planner choice, assumption A5): at most 2 MiB per file and 15 MiB over
`internal/convert/testdata/cad`. Everything larger is manifest-only.

---

## `public/LinearSlide-MGN15x-Block.step` — 535327 bytes

- Source: https://raw.githubusercontent.com/FreeCAD/FreeCAD-library/544a254e090eaf7bfbb6a9b69e249dc0d3d29d67/Mechanical%20Parts/Bearings/linear_bearings/LinearSlide-MGN15x-Block.step
- Author: k2-mirror (git author of the commit that added the file, 3b75a91ce115841a4770fae72ef4e54ccac2a8fa, 2024-08-27), via the FreeCAD-library contributors
- Licence: CC-BY-3.0
- Attribution: "LinearSlide-MGN15x-Block.step", FreeCAD-library (https://github.com/FreeCAD/FreeCAD-library), licensed under CC-BY 3.0 (http://creativecommons.org/licenses/by/3.0/); the repository README states that all parts in it are CC-BY 3.0, per-part authorship is not verified by the repository
- sha256: 26a68f070dea1515d1ce9ef80b54bcec865d87bc4cebb6898ff0d936d4216d45
- Exporter: `Open CASCADE STEP processor 7.6`, originating system `FreeCAD` (FILE_NAME), schema `AUTOMOTIVE_DESIGN`

**Used for:** the phase 71 tracer slice, a real asymmetric mechanical part (one solid, four counterbored
holes, MILLI_METRE units) through the measurement image, OCP, VTK/OSMesa, the pixel judge and Landlock.
Measured at tracer time: 1 solid, volume 17567.98 mm^3, bbox diagonal 72.69 mm, 5948 triangles at the
standard quality.

**If replaced:** the replacement must be a real mechanical STEP of 100 KiB to 2 MiB under an allowed
licence, not symmetric, with the tracer evidence (`evidence/71-02-tracer.txt`) re-run on it.

---

---

## `public/V-Slot-20x20-100mm.step` — 183394 bytes

- Source: https://raw.githubusercontent.com/FreeCAD/FreeCAD-library/544a254e090eaf7bfbb6a9b69e249dc0d3d29d67/Mechanical%20Parts/Profiles%20EN/OpenBuilds/V-Slot-20x20-100mm.step
- Author: Charles Fox (charles-fox), git author of the commit that added the file, a305030944e02017dbb9b5de495b49022d8eafd5, 2026-09-29
- Licence: CC-BY-3.0
- Attribution: "V-Slot-20x20-100mm.step", FreeCAD-library (https://github.com/FreeCAD/FreeCAD-library), by Charles Fox, licensed under CC-BY 3.0 (http://creativecommons.org/licenses/by/3.0/); not modified. The repository README states that all parts in it are CC-BY 3.0; per-part authorship is not verified by the repository (assumption A8).
- sha256: 9c98f6a68872f4ed5f1cc2b514cf3e70ded179b4c1c90a4761adcef3903e874f
- Origin: `Open CASCADE STEP processor 7.8`, originating system `FreeCAD`, MILLI_METRE

**Used for:** A real mechanical assembly: 18 NEXT_ASSEMBLY_USAGE_OCCURRENCE, 8 products, 2 MANIFOLD_SOLID_BREP, 4255 entities (counters.py). The committed real STEP assembly with NAUO of 2 or more that the corpus must carry.

**If replaced:** A real mechanical STEP assembly with at least two assembly usage occurrences under an allowed licence.

---

## `public/SC8UU.step` — 175950 bytes

- Source: https://raw.githubusercontent.com/FreeCAD/FreeCAD-library/544a254e090eaf7bfbb6a9b69e249dc0d3d29d67/Mechanical%20Parts/Mountings/SC8UU/SC8UU.step
- Author: hasecilu (hasecilu), git author of the commit that added the file, b3ee3aec4c213e53e2af2c8ebf936b94e17d89c2, 2022-01-04
- Licence: CC-BY-3.0
- Attribution: "SC8UU.step", FreeCAD-library (https://github.com/FreeCAD/FreeCAD-library), by hasecilu, licensed under CC-BY 3.0 (http://creativecommons.org/licenses/by/3.0/); not modified. The repository README states that all parts in it are CC-BY 3.0; per-part authorship is not verified by the repository (assumption A8).
- sha256: 5d436ba0ef1777f9b7b7381fbcc5983304b56408f0aecc22a8b6c0ae139e9123
- Origin: `Open CASCADE STEP processor 7.5`, originating system `FreeCAD`, MILLI_METRE

**Used for:** A second real assembly: 3 NEXT_ASSEMBLY_USAGE_OCCURRENCE, 4 products, 3 MANIFOLD_SOLID_BREP, 3589 entities.

**If replaced:** A real STEP assembly with three or more components.

---

## `public/LM8UU.step` — 42778 bytes

- Source: https://raw.githubusercontent.com/FreeCAD/FreeCAD-library/544a254e090eaf7bfbb6a9b69e249dc0d3d29d67/Mechanical%20Parts/Mountings/LM8UU/LM8UU.step
- Author: hasecilu (hasecilu), git author of the commit that added the file, b3ee3aec4c213e53e2af2c8ebf936b94e17d89c2, 2022-01-04
- Licence: CC-BY-3.0
- Attribution: "LM8UU.step", FreeCAD-library (https://github.com/FreeCAD/FreeCAD-library), by hasecilu, licensed under CC-BY 3.0 (http://creativecommons.org/licenses/by/3.0/); not modified. The repository README states that all parts in it are CC-BY 3.0; per-part authorship is not verified by the repository (assumption A8).
- sha256: 1e2fb3226e6df8be982b76e1101e0a8bd2bf2db09a29ea3842beca4c3d29a1a0
- Origin: `Open CASCADE STEP processor 7.5`, originating system `FreeCAD`, MILLI_METRE

**Used for:** The smallest real assembly (42778 bytes): 2 NEXT_ASSEMBLY_USAGE_OCCURRENCE, 3 products, 2 MANIFOLD_SOLID_BREP, 915 entities; the boundary case of NAUO equal to 2.

**If replaced:** A real STEP assembly with exactly two components.

---

## `public/2020_corner_bracket-Corner.step` — 132515 bytes

- Source: https://raw.githubusercontent.com/FreeCAD/FreeCAD-library/544a254e090eaf7bfbb6a9b69e249dc0d3d29d67/Mechanical%20Parts/Mountings/Corner/2020_corner_bracket-Corner.step
- Author: hasecilu (hasecilu), git author of the commit that added the file, b342740f9b9ecc63dab5855bab3b9fdf3e3e3488, 2021-12-13
- Licence: CC-BY-3.0
- Attribution: "2020_corner_bracket-Corner.step", FreeCAD-library (https://github.com/FreeCAD/FreeCAD-library), by hasecilu, licensed under CC-BY 3.0 (http://creativecommons.org/licenses/by/3.0/); not modified. The repository README states that all parts in it are CC-BY 3.0; per-part authorship is not verified by the repository (assumption A8).
- sha256: 25e3fbc171f13e1a96258ab084b2d8134a737ebd34098a312e297344481af348
- Origin: `Open CASCADE STEP processor 7.5`, originating system `FreeCAD`, MILLI_METRE

**Used for:** A real single mechanical part with holes (1 MANIFOLD_SOLID_BREP, 2734 entities, no NAUO): the single-part mesh and render path.

**If replaced:** A real single-solid mechanical STEP of 100 KiB to 2 MiB.

---

## `public/nist-d2mi-827-9999-906.stp` — 100202 bytes

- Source: https://www.nist.gov/system/files/documents/el/msid/infotest/NIST-D2MI-Models.zip, member `NIST-D2MI-Models/827-9999-906.stp` (archive sha256 f20e36fb68633129dfedadf209ba4836bb0e42e18f12e7a2c28140ecdfb26cf3)
- Author: NIST (US federal government employees), D2MI project
- Licence: PD
- Attribution: National Institute of Standards and Technology, Design to Manufacturing and Inspection (D2MI) models; README.txt of the archive: the files "were developed at the National Institute of Standards and Technology by employees of the Federal Government in the course of their official duties. Pursuant to Title 17 Section 105 of the United States Code the results are not subject to copyright protection and are in the public domain."
- sha256: 1327cf9c4668b1d5bf15553ba4bba803abe93d1aa55d716eafedfa3daf6ac443
- Origin: `SIEMENS PLM Software NX 8.5` through `ST-DEVELOPER v15`; unit CONVERSION_BASED_UNIT INCH; one MANIFOLD_SOLID_BREP, 2432 entities

**Used for:** A real inch-native STEP from a commercial CAD (NX), the origin the FreeCAD parts cannot give: units are written as CONVERSION_BASED_UNIT, not SI_UNIT.

**If replaced:** A real STEP from a non-FreeCAD exporter with inch units.

---

## `public/nist_ctc_01_asme1_ap242-e1.stp` — 396445 bytes

- Source: https://www.nist.gov/system/files/documents/noindex/2024/06/19/NIST-PMI-STEP-Files.zip, member `NIST-PMI-STEP-Files/nist_ctc_01_asme1_ap242-e1.stp` (archive sha256 8fa78429e6d8d9b0d7681d223b6aa9ec98c3772185c55b1a0e3679b21c181911)
- Author: NIST (US federal government employees), MBE PMI Validation and Conformance Testing project
- Licence: PD
- Attribution: National Institute of Standards and Technology, CTC 01 test case, STEP AP242 edition 1 with semantic PMI; NIST disclaimer on the download page: "The test cases, CAD models, and STEP files can be used without any restrictions"; NIST asks for acknowledgement.
- sha256: 85a5752da05f53c456ca3a9e038c90358e1d5a3141d1f0d6e5f0970f2356e821
- Origin: exporter information removed by NIST (README of the archive); unit CONVERSION_BASED_UNIT MILLIMETRE (a named unit, not SI_UNIT); 1 MANIFOLD_SOLID_BREP, 4350 entities

**Used for:** A real AP242 part with PMI whose millimetre unit is written as CONVERSION_BASED_UNIT('MILLIMETRE'), the unit spelling a naive SI_UNIT reader misses.

**If replaced:** A real AP242 STEP with PMI and a named conversion-based millimetre unit.

---

## `public/nist_ctc_05_asme1_ap242-e1.stp` — 878030 bytes

- Source: https://www.nist.gov/system/files/documents/noindex/2024/06/19/NIST-PMI-STEP-Files.zip, member `NIST-PMI-STEP-Files/nist_ctc_05_asme1_ap242-e1.stp` (archive sha256 8fa78429e6d8d9b0d7681d223b6aa9ec98c3772185c55b1a0e3679b21c181911)
- Author: NIST (US federal government employees), MBE PMI Validation and Conformance Testing project
- Licence: PD
- Attribution: National Institute of Standards and Technology, CTC 05 test case, STEP AP242 edition 1 with semantic PMI; NIST disclaimer on the download page: "The test cases, CAD models, and STEP files can be used without any restrictions"; NIST asks for acknowledgement.
- sha256: 59bbc09a34621c03106e4c1b2a5bc909fdb67463117c16c4965ee7a6fe5c1521
- Origin: exporter information removed by NIST; unit CONVERSION_BASED_UNIT written in lower case ('inch'); 2 products, 1 MANIFOLD_SOLID_BREP, 13394 entities

**Used for:** A real inch file whose unit name is the lower-case 'inch': a unit-name matcher that compares case-sensitively against INCH misses it.

**If replaced:** A real STEP whose inch unit name is not upper case.

---

## `public/nist_stc_06_asme1_ap242-e3.stp` — 1006401 bytes

- Source: https://www.nist.gov/system/files/documents/noindex/2024/06/19/NIST-PMI-STEP-Files.zip, member `NIST-PMI-STEP-Files/nist_stc_06_asme1_ap242-e3.stp` (archive sha256 8fa78429e6d8d9b0d7681d223b6aa9ec98c3772185c55b1a0e3679b21c181911)
- Author: NIST (US federal government employees), MBE PMI Validation and Conformance Testing project
- Licence: PD
- Attribution: National Institute of Standards and Technology, STC 06 test case, STEP AP242 edition 3; NIST disclaimer on the download page: "The test cases, CAD models, and STEP files can be used without any restrictions"; NIST asks for acknowledgement.
- sha256: 71777c28da76da0e8a667e4cbe792d5f72c09b5c56440c9744d3d50ca96ecc8d
- Origin: exporter information removed by NIST; SI_UNIT milli metre; 229 \X2\ escapes (non-ASCII text through STEP escapes, the file itself is ASCII); 1 MANIFOLD_SOLID_BREP, 11780 entities

**Used for:** The real counterpart of the `\X2\` escape: product text carried as escapes in an ASCII file, as opposed to the raw 8-bit bytes of the synthetic cp1251 and Latin-1 fixtures.

**If replaced:** A real STEP with `\X2\` escapes.

---

## `public/nasa-jwst-all-pieces-without-supports.stl` — 1404884 bytes

- Source: https://raw.githubusercontent.com/nasa/NASA-3D-Resources/11ebb4ee043715aefbba6aeec8a61746fad67fa7/3D%20Printing/James%20Webb%20Space%20Telescope/All%20pieces%20%28without%20supports%29.stl
- Author: NASA (git author arc-webservices, commit 53d7c4ad798aa25c1c5fd7e851a79d2045de7ff2, 2024-12-24); the per-file contributor is not recorded by the repository
- Licence: PD
- Attribution: NASA 3D Resources, James Webb Space Telescope 3D printing model (https://github.com/nasa/NASA-3D-Resources); the README states the assets are free and without copyright and points to the NASA Images and Media Usage Guidelines; the README lists non-NASA contributors (Ball Aerospace, DigitalSpace, Conti), per-file attribution is not given
- sha256: 6326c7db217d750f3a33395797fa9c16b6e090f589e56cf3928b32852859e1c4
- Origin: binary STL, 28096 triangles, the 80 byte header is `COLOR=...` and does not begin with `solid`

**Used for:** A real binary STL of an assembly-like model at about 1.4 MiB; the committed binary STL for the mesh and render path.

**If replaced:** A real binary STL of 100 KiB to 2 MiB under an allowed licence.

---

## `public/nasa-hubble-all-pieces.stl` — 429584 bytes

- Source: https://raw.githubusercontent.com/nasa/NASA-3D-Resources/11ebb4ee043715aefbba6aeec8a61746fad67fa7/3D%20Printing/Hubble%20Space%20Telescope/All%20pieces.stl
- Author: NASA (git author arc-webservices, commit 53d7c4ad798aa25c1c5fd7e851a79d2045de7ff2, 2024-12-24); the per-file contributor is not recorded by the repository
- Licence: PD
- Attribution: NASA 3D Resources, Hubble Space Telescope 3D printing model (https://github.com/nasa/NASA-3D-Resources); the README states the assets are free and without copyright; per-file attribution is not given
- sha256: 413bc4d12451c9637165e7d6080a3d0c08ac7d787c80065094498565b009620c
- Origin: binary STL, 8590 triangles, header `COLOR=...`

**Used for:** A second small real binary STL with a different triangle count.

**If replaced:** A real binary STL of 100 KiB to 2 MiB.

---

## `public/hatches_1.dxf` — 130334 bytes

- Source: https://raw.githubusercontent.com/mozman/ezdxf/9fb787cc2cff2b566e4ef9cdea8888c7eb24ca8b/examples_dxf/hatches_1.dxf
- Author: mozman, last commit on the path c8b18226f8656cb2469b3003c6d845859dd97c11 (2020-06-28, a restructure; the history before the move was not followed)
- Licence: MIT
- Attribution: ezdxf, Copyright (c) 2020 Manfred Moitzi, MIT licence (https://github.com/mozman/ezdxf/blob/9fb787cc2cff2b566e4ef9cdea8888c7eb24ca8b/LICENSE); the notice is reproduced at the end of this document; not modified
- sha256: 2dc2930b4f7d5fb16698437e4f6972986e580a81b2b5d36a780b4311f85fb326
- Origin: AC1032, ANSI_1252, $INSUNITS 1; the exporter is not recorded in the file (header fields are anonymised) and no evidence was found that it came from a CAD exporter: treat it as possibly written by ezdxf itself (Pitfall 26, fixtures of one generator)

**Used for:** 10 HATCH entities, 32 modelspace entities, 3 blocks (counters.py).

**If replaced:** A real DXF with hatches from a CAD exporter.

---

## `public/hatches_2.dxf` — 208793 bytes

- Source: https://raw.githubusercontent.com/mozman/ezdxf/9fb787cc2cff2b566e4ef9cdea8888c7eb24ca8b/examples_dxf/hatches_2.dxf
- Author: mozman, last commit on the path c8b18226f8656cb2469b3003c6d845859dd97c11 (2020-06-28, a restructure; the history before the move was not followed)
- Licence: MIT
- Attribution: ezdxf, Copyright (c) 2020 Manfred Moitzi, MIT licence (https://github.com/mozman/ezdxf/blob/9fb787cc2cff2b566e4ef9cdea8888c7eb24ca8b/LICENSE); the notice is reproduced at the end of this document; not modified
- sha256: a50f02abbbafb0fedef5140e3f99691012bcffa192ecb298470fe8c5b2b9aad6
- Origin: AC1032, ANSI_1252, $INSUNITS 1; the exporter is not recorded in the file (header fields are anonymised) and no evidence was found that it came from a CAD exporter: treat it as possibly written by ezdxf itself (Pitfall 26, fixtures of one generator)

**Used for:** 2 HATCH entities, 11 modelspace entities, 3 blocks.

**If replaced:** A real DXF with hatches from a CAD exporter.

---

## `public/uncommon.dxf` — 343110 bytes

- Source: https://raw.githubusercontent.com/mozman/ezdxf/9fb787cc2cff2b566e4ef9cdea8888c7eb24ca8b/examples_dxf/uncommon.dxf
- Author: mozman, last commit on the path c8b18226f8656cb2469b3003c6d845859dd97c11 (2020-06-28, a restructure; the history before the move was not followed)
- Licence: MIT
- Attribution: ezdxf, Copyright (c) 2020 Manfred Moitzi, MIT licence (https://github.com/mozman/ezdxf/blob/9fb787cc2cff2b566e4ef9cdea8888c7eb24ca8b/LICENSE); the notice is reproduced at the end of this document; not modified
- sha256: 5efdd671e3b6d8c45bd0688b1d6479d523f2290f0b390f1584348bf46e34c965
- Origin: AC1032, ANSI_1252, $INSUNITS 1; the exporter is not recorded in the file (header fields are anonymised) and no evidence was found that it came from a CAD exporter: treat it as possibly written by ezdxf itself (Pitfall 26, fixtures of one generator)

**Used for:** 13 DIMENSION, 1 INSERT, 33 blocks, 85 modelspace entities: the dimension and block file, and the base of the DXF replication ladder.

**If replaced:** A real DXF with dimensions and blocks from a CAD exporter.

---

## `public/dimension_in_nested_blocks.dxf` — 91234 bytes

- Source: https://raw.githubusercontent.com/mozman/ezdxf/9fb787cc2cff2b566e4ef9cdea8888c7eb24ca8b/examples_dxf/dimension_in_nested_blocks.dxf
- Author: mozman, commit f790e635cd071bba300640d60f19fea7c27d0377 (2021-06-03)
- Licence: MIT
- Attribution: ezdxf, Copyright (c) 2020 Manfred Moitzi, MIT licence (https://github.com/mozman/ezdxf/blob/9fb787cc2cff2b566e4ef9cdea8888c7eb24ca8b/LICENSE); the notice is reproduced at the end of this document; not modified
- sha256: 03b50bf9a29513147d2b66f6ec1a4377494ced38437082c979639dfa42d613ea
- Origin: AC1027, ANSI_1252, $INSUNITS 6; the exporter is not recorded in the file (header fields are anonymised) and no evidence was found that it came from a CAD exporter: treat it as possibly written by ezdxf itself (Pitfall 26, fixtures of one generator)

**Used for:** 1 DIMENSION, 7 INSERT, 9 blocks: dimensions and inserts that the file name says are nested in blocks; the block-nesting depth is not counted by counters.py.

**If replaced:** A real DXF with nested block references and dimensions.

---

## `public/acad_table_with_blk_ref.dxf` — 156603 bytes

- Source: https://raw.githubusercontent.com/mozman/ezdxf/9fb787cc2cff2b566e4ef9cdea8888c7eb24ca8b/examples_dxf/acad_table_with_blk_ref.dxf
- Author: mozman, commit 5a4f25d62f8e5e3c4eef3da772fb1f8e01fdfb2e (2021-11-28)
- Licence: MIT
- Attribution: ezdxf, Copyright (c) 2020 Manfred Moitzi, MIT licence (https://github.com/mozman/ezdxf/blob/9fb787cc2cff2b566e4ef9cdea8888c7eb24ca8b/LICENSE); the notice is reproduced at the end of this document; not modified
- sha256: 7c42eac2e0c21b386273294b7937845ddda3da58f85c30fe130db229570f8015
- Origin: AC1032, ANSI_1252, $INSUNITS 6; an ACAD_TABLE entity with an associated anonymous block; the ezdxf repository reads it as an input in examples/entities/read_acad_table.py and integration_tests/test_acad_table.py, the exporter is still not recorded in the file

**Used for:** 1 INSERT, 6 blocks, 1 modelspace entity (the table).

**If replaced:** A real DXF with an ACAD_TABLE.

---

## `public/houses_of_parliament_georeferenced.dxf` — 228897 bytes

- Source: https://raw.githubusercontent.com/mozman/ezdxf/9fb787cc2cff2b566e4ef9cdea8888c7eb24ca8b/tests/test_01_dxf_entities/houses_of_parliament_georeferenced.dxf
- Author: mbway, commit 823593a57d235c5127ded6aec51b3a228fcc3e8d (2020-08-24)
- Licence: MIT
- Attribution: ezdxf, Copyright (c) 2020 Manfred Moitzi, MIT licence (https://github.com/mozman/ezdxf/blob/9fb787cc2cff2b566e4ef9cdea8888c7eb24ca8b/LICENSE); the notice is reproduced at the end of this document; not modified
- sha256: a892e62134665452eb6bc9dee23ff8f8c68977e866ca67594acd0b2bfd47b109
- Origin: AC1032, ANSI_1252, $INSUNITS 1; GEODATA object; the repository reads it as an input in tests/test_01_dxf_entities/test_135_geo_data.py; the exporter is not recorded in the file

**Used for:** 1 modelspace entity, 3 blocks: a DXF dominated by one large entity.

**If replaced:** A real georeferenced DXF.

---

## `public/columns_R2007.dxf` — 195808 bytes

- Source: https://raw.githubusercontent.com/mozman/ezdxf/9fb787cc2cff2b566e4ef9cdea8888c7eb24ca8b/examples_dxf/columns_R2007.dxf
- Author: mozman, commit 92cbf4636bfb273834e30d416b7c35ba28785460 (2021-04-24)
- Licence: MIT
- Attribution: ezdxf, Copyright (c) 2020 Manfred Moitzi, MIT licence (https://github.com/mozman/ezdxf/blob/9fb787cc2cff2b566e4ef9cdea8888c7eb24ca8b/LICENSE); the notice is reproduced at the end of this document; not modified
- sha256: 31d4d411982821a3db75854f90108e15f867ecdd619d526fb3c9bd761b92095c
- Origin: AC1021 (R2007), ANSI_1252, $INSUNITS 4; the exporter is not recorded in the file (header fields are anonymised) and no evidence was found that it came from a CAD exporter: treat it as possibly written by ezdxf itself (Pitfall 26, fixtures of one generator)

**Used for:** 69 modelspace entities, 6 blocks; the R2007 dialect of the format (multiple-column MTEXT).

**If replaced:** A real R2007 DXF.

---

## `public/3dsolids.dxf` — 161075 bytes

- Source: https://raw.githubusercontent.com/mozman/ezdxf/9fb787cc2cff2b566e4ef9cdea8888c7eb24ca8b/examples/acistools/3dsolids.dxf
- Author: mozman, commit 90cd95b4dedc977676a8223dc8ca5ceab2087b29 (2022-05-13)
- Licence: MIT
- Attribution: ezdxf, Copyright (c) 2020 Manfred Moitzi, MIT licence (https://github.com/mozman/ezdxf/blob/9fb787cc2cff2b566e4ef9cdea8888c7eb24ca8b/LICENSE); the notice is reproduced at the end of this document; not modified
- sha256: 8193d9917354a9165d500098aa31209218f0b3d6bf3b24632b89003295806c4d
- Origin: AC1024 (R2010), ANSI_1252, $INSUNITS 4; 4 ACIS 3DSOLID entities; the ezdxf repository reads it as an input in examples/acistools/mesh_from_body.py; the exporter is not recorded in the file

**Used for:** 4 modelspace entities, 4 blocks: ACIS solid bodies that a 2D DXF path must skip or reject rather than render.

**If replaced:** A real DXF with 3DSOLID entities.

---

## Synthetic fixtures (`synthetic/`)
Generated by `scripts/cad-corpus-gen.py` before any code that judges them; the generator is deterministic (no timestamps, no randomness, no host paths) and its `--check-only` mode needs only the standard library. Two runs in the measurement image produce a byte-identical directory equal to the committed one. Generation reads every fixture back with OCP and ezdxf where it can, because a marker grep cannot see a malformed file.
Generation command for every file below: `docker run --rm --network none -v "$PWD:/src:ro" -v OUT:/out -w /src octoconv-cad-measure:ocp7.9.3.1.1-full-0e0672165074 /opt/venv/bin/python -I -B scripts/cad-corpus-gen.py --out-dir /out  (OCP 7.9.3.1.1, Open CASCADE 7.9)`

## `synthetic/assembly_nauo4.step` — 18594 bytes

- Licence: generated
- sha256: 19253fb38b999d469c96ef892310c7d6e25e2b357c50b54739a2991d6b1ba537
- Command: see the generation command above

**Proves:** Assembly. One product (a 10 mm cube) instanced four times through NEXT_ASSEMBLY_USAGE_OCCURRENCE (XCAF, STEPCAFControl_Writer), translated 0, 20, 40, 60 mm along X.

**Expected (measured at generation):** Exactly 4 NEXT_ASSEMBLY_USAGE_OCCURRENCE entities; OCP reads 4 solids, volume 4000 mm^3, bbox 70 x 10 x 10. Instancing counts (D-10) are compared with copies of geometry.

**If replaced:** A real assembly with repeated instances of one product.

## `synthetic/asymmetric_l_bracket.step` — 26322 bytes

- Licence: generated
- sha256: bee97d7aa4790a09b4de06f062a7f5741c390bdbb1ca626f7a0ee3ed3c26da7a
- Command: see the generation command above

**Proves:** The designated asymmetric part for the criterion that three views differ. An L profile (60 x 40, legs 10 thick) extruded 20 mm with a 6 mm hole through the vertical leg at y = 28, z = 7.

**Expected (measured at generation):** 1 solid, volume 17717.2567 mm^3, bbox 60 x 40 x 20, centroid 8.594 mm away from the bbox centre.

**If replaced:** Any real asymmetric solid; the three-view criterion applies only to designated asymmetric parts.

## `synthetic/blocks_minsert.dxf` — 1126 bytes

- Licence: generated
- sha256: 14d75a902c5a7c2ef62ff9e2d5a4d8b95755bc7b39e34cf0fc2b85c621f4c1ce
- Command: see the generation command above

**Proves:** 2D entities. R12 file with a BOX block, an anonymous dimension block *D1, one INSERT, one MINSERT (INSERT with 3 columns and 3 rows, spacing 15) and one DIMENSION.

**Expected (measured at generation):** ezdxf sees INSERT column and row counts [(1, 1), (3, 3)] and 1 DIMENSION.

**If replaced:** A real DXF with blocks, MINSERT and dimensions from a CAD exporter.

## `synthetic/comments_999.dxf` — 222 bytes

- Licence: generated
- sha256: a93b21ad0802ab9359f0e2261dc152ede0873c9c9e1bf378b1e2997d70333b98
- Command: see the generation command above

**Proves:** Sniffer trap for phase 72. The file starts with two group 999 comment pairs before the first SECTION, so a sniffer that expects 0/SECTION on the first line fails.

**Expected (measured at generation):** ezdxf reads 1 entity (a LINE from (0,0) to (10,5)).

**If replaced:** A real DXF whose first pairs are group 999 comments.

## `synthetic/degenerate_plate.step` — 15442 bytes

- Licence: generated
- sha256: 8895810c3f1b91259434846c902f429209863f4a22da05edcf6e0ab92c0a24bb
- Command: see the generation command above

**Proves:** Degenerate geometry. A 100 x 100 x 0.1 mm plate.

**Expected (measured at generation):** 1 solid, volume 1000 mm^3, bbox 100 x 100 x 0.1; the renderer must not produce a NaN camera or an empty frame.

**If replaced:** A real thin plate under 1 mm in one axis.

## `synthetic/degenerate_rod.step` — 5668 bytes

- Licence: generated
- sha256: d5e9e0acd954d40cf9359e3e88a60ab9d9cb19a870bfa1e56bfd244ddb3e27bf
- Command: see the generation command above

**Proves:** Degenerate geometry. A cylinder of radius 0.5 and height 200 mm.

**Expected (measured at generation):** 1 solid, volume 157.0796 mm^3 (pi x 0.25 x 200), bbox 1 x 1 x 200.

**If replaced:** A real rod with an aspect ratio of at least 200.

## `synthetic/iges_no_newlines.iges` — 12240 bytes

- Licence: generated
- sha256: 1c228e991340fd51b3a3748add266fb64e3d4324db2728efec5693347e43fadd
- Command: see the generation command above

**Proves:** Sniffer trap for phase 72. The content of surfaces_only.iges without any line break: 153 records of 80 columns back to back, column 73 of every record is S, G, D, P or T.

**Expected (measured at generation):** Length 12240 = 153 x 80 with no line terminator; byte-equal to surfaces_only.iges with its line breaks removed.

**If replaced:** A real IGES written without line breaks.

## `synthetic/legacy_cp1251.dxf` — 227 bytes

- Licence: generated
- sha256: c3d66b5d8993e42f0f60852f0ad6ccab98780cfbcd297003f65b7a5aa2047b87
- Command: see the generation command above

**Proves:** Encoding origin for DXF. R12 file with $DWGCODEPAGE ANSI_1251 and a TEXT value in raw cp1251 bytes.

**Expected (measured at generation):** ezdxf decodes the TEXT as 'Деталь' through $DWGCODEPAGE.

**If replaced:** A real R12 or R2000 DXF with ANSI_1251 and Cyrillic text.

## `synthetic/legacy_cp1251.step` — 15329 bytes

- Licence: generated
- sha256: b7d0724eefb56042f6e125b0a63183e721d5de5c67af24fc11deb58c6b841108
- Command: see the generation command above

**Proves:** Encoding origin. PRODUCT name and description hold raw cp1251 bytes ("Деталь", "Деталь болт М5") with no \X2\ or \X\ escape.

**Expected (measured at generation):** 1 solid, 3 x 2 x 1 box, volume 6 mm^3. The raw bytes are 0xC4E5F2E0EBFC in the product name; they are not valid UTF-8.

**If replaced:** A real legacy 8-bit STEP from a Cyrillic CAD with the same property (raw cp1251, no escapes).

## `synthetic/legacy_latin1.dxf` — 229 bytes

- Licence: generated
- sha256: 5d6a8fc066a9e8c28ee049d73fd785588305e93f0d7dd12e40935ea78725a15e
- Command: see the generation command above

**Proves:** Encoding origin for DXF. R12 file with $DWGCODEPAGE ANSI_1252 and a TEXT value in raw Latin-1 bytes.

**Expected (measured at generation):** ezdxf decodes the TEXT as 'Bügel Ø5' through $DWGCODEPAGE.

**If replaced:** A real DXF with ANSI_1252 and accented text.

## `synthetic/legacy_latin1.step` — 15336 bytes

- Licence: generated
- sha256: f893dcd2c7afb8476e10ac7af406437308864a7c678991289d33955ece4897c2
- Command: see the generation command above

**Proves:** Encoding origin. PRODUCT name and description hold raw Latin-1 bytes ("Bügel Ø5", "Halterung Ø5 für Ø8") with no \X2\ or \X\ escape.

**Expected (measured at generation):** 1 solid, 3 x 2 x 1 box, volume 6 mm^3. The name bytes are 42 FC 67 65 6C 20 D8 35; they are not valid UTF-8.

**If replaced:** A real legacy 8-bit STEP from a Western European CAD with the same property.

## `synthetic/open_shell.step` — 13604 bytes

- Licence: generated
- sha256: 6136fcac4deee013df97c54696a52efaa6a4d9f860982208e9d1d55d0626ba06
- Command: see the generation command above

**Proves:** Null-volume reason. The five faces of a 2 x 3 x 1 box (one face missing) as an OPEN_SHELL in a SHELL_BASED_SURFACE_MODEL; there is no MANIFOLD_SOLID_BREP and no CLOSED_SHELL.

**Expected (measured at generation):** OCP reads 0 solids (volume is null, reason open shell or no solids), bbox 2 x 3 x 1. A MANIFOLD_SOLID_BREP pointing at a CLOSED_SHELL with a face removed was also tried by hand: OCP reads 0 solids for it as well, so it is not a distinct case.

**If replaced:** A real STEP with an open shell and no solid.

## `synthetic/reversed_solid.step` — 15377 bytes

- Licence: generated
- sha256: b924c973c5df19b94ff9822998319f0fe325b0042500d2209d1988da58b9fa05
- Command: see the generation command above

**Proves:** Orientation. A 2 x 3 x 1 box written from the reversed solid: the six ADVANCED_FACE same_sense flags are the complement of the plain box (.T. .F. .T. .F. .T. .F. against .F. .T. .F. .T. .F. .T.).

**Expected (measured at generation):** OCP's STEP reader orients the solid back: 1 solid, volume +6 mm^3, not negative. Measured by flipping ADVANCED_FACE flags, FACE_BOUND flags and both on a plain box: all three read as +6. The helper must therefore not report a negative volume for this file.

**If replaced:** A real STEP exported with inverted face orientation.

## `synthetic/single_face.step` — 4527 bytes

- Licence: generated
- sha256: fcfc54200e32913ec7f6bdfe05c811a479c7891db929d319b6089d40276647f8
- Command: see the generation command above

**Proves:** Degenerate geometry and null-volume reason. One planar face 100 x 100 mm and no solid; the file holds an OPEN_SHELL of one ADVANCED_FACE inside a SHELL_BASED_SURFACE_MODEL.

**Expected (measured at generation):** OCP reads 0 solids (volume is null, reason no_solids), bbox 100 x 100 x 0.

**If replaced:** A real STEP holding a face and no solid.

## `synthetic/stl_ascii_box.stl` — 3005 bytes

- Licence: generated
- sha256: bae23d8e1edb895c48747466491e3e756b2498a4e422ce73c3ff28cf69772950
- Command: see the generation command above

**Proves:** The same 10 x 20 x 30 box as ASCII STL (solid, facet normal, outer loop, vertex, endsolid).

**Expected (measured at generation):** 12 facets, signed volume 6000 mm^3, same geometry as stl_binary_solid_header.stl.

**If replaced:** A real ASCII STL; the binary and ASCII multipliers are compared in the memory curve.

## `synthetic/stl_binary_solid_header.stl` — 684 bytes

- Licence: generated
- sha256: ade18261538a244c5320bf83524d49667048dd5c063dd298bb81064f670e7605
- Command: see the generation command above

**Proves:** Sniffer trap for phase 72. Binary STL of a 10 x 20 x 30 box (12 triangles) whose 80 byte header begins with the word solid, so a sniffer that treats a leading solid as ASCII misclassifies it. Size 84 + 50 x 12 = 684.

**Expected (measured at generation):** 12 triangles, signed volume 6000 mm^3 (outward winding).

**If replaced:** A real binary STL whose header begins with solid.

## `synthetic/surfaces_only.iges` — 12393 bytes

- Licence: generated
- sha256: 8e4bce60557e9ac2881f9763797ced089824c3d477fc75bd7d420961826bcc9f
- Command: see the generation command above

**Proves:** Geometry class. The six faces of a 2 x 3 x 1 box written as trimmed surfaces without shell or solid entities. Directory-section entity types: 102, 108, 110, 142, 144, 402; neither 186 (manifold solid) nor 514 (shell) is present. Date parameters 18 and 25 of the global section are fixed to 20200101.000000.

**Expected (measured at generation):** IGES contains surfaces only; a volume cannot be computed from it.

**If replaced:** A real surfaces-only IGES (no 186, no 514).

## `synthetic/units_inch.step` — 15567 bytes

- Licence: generated
- sha256: 7ddac77097685f6702bda25d2d1ed5169d5b7d033e1154e79cced5ddfdeb6120
- Command: see the generation command above

**Proves:** Unit origin. A 2 x 3 x 1 box whose LENGTH_UNIT is written by the generator as CONVERSION_BASED_UNIT('INCH') through LENGTH_MEASURE_WITH_UNIT(25.4) of SI_UNIT(.MILLI.,.METRE.), referenced by GLOBAL_UNIT_ASSIGNED_CONTEXT. Setting write.step.unit=IN in OCP wrote a millimetre header, so the entities are written by hand (71-RESEARCH V15). The generator parses the DATA section to confirm the context points at the inch unit.

**Expected (measured at generation):** OCP reads the length unit as INCH and scales to millimetres: 1 solid, volume 98322.384 mm^3 (6 in^3), bbox 50.8 x 76.2 x 25.4 mm. STEPControl_Reader.FileUnits is not trusted alone; the unit text in the file is INCH.

**If replaced:** A real STEP from an inch-native exporter with the same unit entities, the same two expectations (text INCH, mm-scaled geometry).

---

## Fetched (manifest authoritative)

These files are not committed. `scripts/cad-corpus-manifest.tsv` holds the URL, archive digest, member, sha256, byte count and licence of each; the sha256 values are deliberately not repeated here, so that the manifest is the only place that has to agree with the bytes. `scripts/cad-corpus-fetch.sh` downloads them into `$OCTOCONV_CAD_CACHE/fetched` and refuses a file whose sha256 differs.

Checked and not taken: FreeCAD-library `Electronics Parts/Cameras/Wide-170-Angle-Raspi-Camera.iges` (648 bytes, a single group entity, no geometry) and every board model (Raspberry Pi, RAMPS), whose upstream licences differ.

### `freecad-mit-e-vent-t-slot.step` — 9044371 bytes

- Source: FreeCAD-library
- Author: berserkdesign (xolotl-hub)
- Licence: CC-BY-3.0
- Attribution: FreeCAD-library (https://github.com/FreeCAD/FreeCAD-library), by berserkdesign (xolotl-hub), licensed under CC-BY 3.0 (http://creativecommons.org/licenses/by/3.0/); not modified; per-part authorship is not verified by the repository (assumption A8)
- Measured by counters.py: STEP, 89066 entities, 102 NEXT_ASSEMBLY_USAGE_OCCURRENCE, 22 products, 25 MANIFOLD_SOLID_BREP, unit SI:MILLI_METRE, originating system SolidWorks_2019

### `freecad-emergency-button-disk.step` — 15019946 bytes

- Source: FreeCAD-library
- Author: AngelLM
- Licence: CC-BY-3.0
- Attribution: FreeCAD-library (https://github.com/FreeCAD/FreeCAD-library), by AngelLM, licensed under CC-BY 3.0 (http://creativecommons.org/licenses/by/3.0/); not modified; per-part authorship is not verified by the repository (assumption A8)
- Measured by counters.py: STEP, 206873 entities, 1 NEXT_ASSEMBLY_USAGE_OCCURRENCE, 2 products, 10 MANIFOLD_SOLID_BREP, unit SI:MILLI_METRE, originating system FreeCAD

### `freecad-faulhaber-2342l.step` — 18231708 bytes

- Source: FreeCAD-library
- Author: hasecilu
- Licence: CC-BY-3.0
- Attribution: FreeCAD-library (https://github.com/FreeCAD/FreeCAD-library), by hasecilu, licensed under CC-BY 3.0 (http://creativecommons.org/licenses/by/3.0/); not modified; per-part authorship is not verified by the repository (assumption A8)
- Measured by counters.py: STEP, 253030 entities, 62 NEXT_ASSEMBLY_USAGE_OCCURRENCE, 46 products, 34 MANIFOLD_SOLID_BREP, unit SI:MILLI_METRE, originating system FreeCAD

### `freecad-sprocket-ansi-z114.step` — 8158613 bytes

- Source: FreeCAD-library
- Author: Simone Bignetti (pysimone)
- Licence: CC-BY-3.0
- Attribution: FreeCAD-library (https://github.com/FreeCAD/FreeCAD-library), by Simone Bignetti (pysimone), licensed under CC-BY 3.0 (http://creativecommons.org/licenses/by/3.0/); not modified; per-part authorship is not verified by the repository (assumption A8)
- Measured by counters.py: STEP, 121765 entities, 0 NEXT_ASSEMBLY_USAGE_OCCURRENCE, 1 products, 1 MANIFOLD_SOLID_BREP, unit SI:MILLI_METRE, originating system FreeCAD

### `freecad-mk8-hotend.iges` — 3058317 bytes

- Source: FreeCAD-library
- Author: Daniel Stauffer
- Licence: CC-BY-3.0
- Attribution: FreeCAD-library (https://github.com/FreeCAD/FreeCAD-library), by Daniel Stauffer, licensed under CC-BY 3.0 (http://creativecommons.org/licenses/by/3.0/); not modified; per-part authorship is not verified by the repository (assumption A8)
- Measured by counters.py: IGES, entity types 100:480;102:852;108:523;110:3386;120:114;124:592;126:211;128:28;142:814;144:665;402:17, has_solid=0 (no 186 manifold solid and no 514 shell), unit MM

### `freecad-festo-valve-5-2.igs` — 11321208 bytes

- Source: FreeCAD-library
- Author: berserkdesign (xolotl-hub)
- Licence: CC-BY-3.0
- Attribution: FreeCAD-library (https://github.com/FreeCAD/FreeCAD-library), by berserkdesign (xolotl-hub), licensed under CC-BY 3.0 (http://creativecommons.org/licenses/by/3.0/); not modified; per-part authorship is not verified by the repository (assumption A8)
- Measured by counters.py: IGES, entity types 100:2026;102:3576;110:5922;120:618;124:1619;126:9205;128:967;142:1788;144:1585;308:12;314:579;408:24, has_solid=0 (no 186 manifold solid and no 514 shell), unit MM

### `nist-d2mi-827-9999-904-rev-c.stp` — 3725845 bytes

- Source: NIST D2MI models, archive member `NIST-D2MI-Models/827-9999-904 rev c.stp`
- Author: NIST (US federal government employees)
- Licence: PD
- Attribution: National Institute of Standards and Technology; the files are US federal government work, the page and README state that they can be used without restrictions and ask for acknowledgement
- Measured by counters.py: STEP, 45764 entities, 0 NEXT_ASSEMBLY_USAGE_OCCURRENCE, 1 products, 1 MANIFOLD_SOLID_BREP, unit CBU:INCH, originating system 3D_EvolutionDEV_4.1_by_CT_CoreTechnologie

### `nist-ftc-09-ap242-e1.stp` — 6109836 bytes

- Source: NIST MBE PMI STEP files, archive member `NIST-PMI-STEP-Files/nist_ftc_09_asme1_ap242-e1.stp`
- Author: NIST (US federal government employees)
- Licence: PD
- Attribution: National Institute of Standards and Technology; the files are US federal government work, the page and README state that they can be used without restrictions and ask for acknowledgement
- Measured by counters.py: STEP, 11662 entities, 0 NEXT_ASSEMBLY_USAGE_OCCURRENCE, 4 products, 1 MANIFOLD_SOLID_BREP, unit CBU:MILLIMETRE, originating system -

### `nist-stc-09-ap242-e3.stp` — 5287678 bytes

- Source: NIST MBE PMI STEP files, archive member `NIST-PMI-STEP-Files/nist_stc_09_asme1_ap242-e3.stp`
- Author: NIST (US federal government employees)
- Licence: PD
- Attribution: National Institute of Standards and Technology; the files are US federal government work, the page and README state that they can be used without restrictions and ask for acknowledgement
- Measured by counters.py: STEP, 10393 entities, 0 NEXT_ASSEMBLY_USAGE_OCCURRENCE, 1 products, 1 MANIFOLD_SOLID_BREP, unit CBU:INCH, originating system -

### `nist-stc-10-ap242-e2.stp` — 4875056 bytes

- Source: NIST MBE PMI STEP files, archive member `NIST-PMI-STEP-Files/nist_stc_10_asme1_ap242-e2.stp`
- Author: NIST (US federal government employees)
- Licence: PD
- Attribution: National Institute of Standards and Technology; the files are US federal government work, the page and README state that they can be used without restrictions and ask for acknowledgement
- Measured by counters.py: STEP, 12073 entities, 0 NEXT_ASSEMBLY_USAGE_OCCURRENCE, 1 products, 1 MANIFOLD_SOLID_BREP, unit SI:MILLI_METRE, originating system -

### `nasa-sls-block1.stl` — 13721684 bytes

- Source: NASA-3D-Resources
- Author: NASA (committed by arc-webservices; per-file contributor not recorded by the repository)
- Licence: PD
- Attribution: NASA 3D Resources (https://github.com/nasa/NASA-3D-Resources), free and without copyright per its README; per-file attribution is not given and the README lists non-NASA contributors (Ball Aerospace, DigitalSpace, Conti)
- Measured by counters.py: binary STL, 274432 triangles

### `nasa-curiosity-2-components.stl` — 12157084 bytes

- Source: NASA-3D-Resources
- Author: NASA (committed by arc-webservices; per-file contributor not recorded by the repository)
- Licence: PD
- Attribution: NASA 3D Resources (https://github.com/nasa/NASA-3D-Resources), free and without copyright per its README; per-file attribution is not given and the README lists non-NASA contributors (Ball Aerospace, DigitalSpace, Conti)
- Measured by counters.py: binary STL, 243140 triangles

### `nasa-curiosity-4-wheels.stl` — 17407284 bytes

- Source: NASA-3D-Resources
- Author: NASA (committed by arc-webservices; per-file contributor not recorded by the repository)
- Licence: PD
- Attribution: NASA 3D Resources (https://github.com/nasa/NASA-3D-Resources), free and without copyright per its README; per-file attribution is not given and the README lists non-NASA contributors (Ball Aerospace, DigitalSpace, Conti)
- Measured by counters.py: binary STL, 348144 triangles

### `nasa-dart.stl` — 22319934 bytes

- Source: NASA-3D-Resources
- Author: NASA (committed by arc-webservices; per-file contributor not recorded by the repository)
- Licence: PD
- Attribution: NASA 3D Resources (https://github.com/nasa/NASA-3D-Resources), free and without copyright per its README; per-file attribution is not given and the README lists non-NASA contributors (Ball Aerospace, DigitalSpace, Conti)
- Measured by counters.py: binary STL, 446397 triangles

### `nasa-roman-space-telescope.stl` — 27238784 bytes

- Source: NASA-3D-Resources
- Author: NASA (committed by arc-webservices; per-file contributor not recorded by the repository)
- Licence: PD
- Attribution: NASA 3D Resources (https://github.com/nasa/NASA-3D-Resources), free and without copyright per its README; per-file attribution is not given and the README lists non-NASA contributors (Ball Aerospace, DigitalSpace, Conti)
- Measured by counters.py: binary STL, 544774 triangles

### `nasa-vesta-b.stl` — 40366434 bytes

- Source: NASA-3D-Resources
- Author: NASA (committed by arc-webservices; per-file contributor not recorded by the repository)
- Licence: PD
- Attribution: NASA 3D Resources (https://github.com/nasa/NASA-3D-Resources), free and without copyright per its README; per-file attribution is not given and the README lists non-NASA contributors (Ball Aerospace, DigitalSpace, Conti)
- Measured by counters.py: binary STL, 807327 triangles

### `nasa-pillars-of-creation-full.stl` — 52041984 bytes

- Source: NASA-3D-Resources
- Author: NASA (committed by arc-webservices; per-file contributor not recorded by the repository)
- Licence: PD
- Attribution: NASA 3D Resources (https://github.com/nasa/NASA-3D-Resources), free and without copyright per its README; per-file attribution is not given and the README lists non-NASA contributors (Ball Aerospace, DigitalSpace, Conti)
- Measured by counters.py: binary STL, 1040838 triangles

---

## Coverage

Each EVAL-02 category with the files that close it. Kinds: **real** is a file written by someone else's software and found in an open source, **synthetic** is the output of `scripts/cad-corpus-gen.py` without `--scaled-from`, **derived** is a deterministic scaling of a real file (see Derived).

| Category | Files | Kind |
|---|---|---|
| STEP assembly with NEXT_ASSEMBLY_USAGE_OCCURRENCE of 2 or more | committed `public/V-Slot-20x20-100mm.step` (18), `public/SC8UU.step` (3), `public/LM8UU.step` (2); fetched `freecad-mit-e-vent-t-slot.step` (102, SolidWorks 2019 originating system), `freecad-faulhaber-2342l.step` (62); `synthetic/assembly_nauo4.step` (4); the NAUO instancing ladder | real, synthetic, derived |
| IGES with surfaces and curves only (no entity 186, no 514) | fetched `freecad-mk8-hotend.iges`, `freecad-festo-valve-5-2.igs`; `synthetic/surfaces_only.iges`, `synthetic/iges_no_newlines.iges` | real (both exported by FreeCAD), synthetic |
| Large binary STL | fetched `nasa-sls-block1.stl` (13.7 MB), `nasa-curiosity-2-components.stl`, `nasa-curiosity-4-wheels.stl`, `nasa-dart.stl`, `nasa-roman-space-telescope.stl` (27.2 MB), `nasa-vesta-b.stl` (40.4 MB), `nasa-pillars-of-creation-full.stl` (52.0 MB); committed small `public/nasa-jwst-all-pieces-without-supports.stl`, `public/nasa-hubble-all-pieces.stl`; the tiled ladder to 100 and 200 MiB | real, derived |
| Large ASCII STL | no open ASCII STL above 2 MiB was found; the ladder to 200 MiB is derived from a real binary STL; the committed `synthetic/stl_ascii_box.stl` is a 12-facet box | derived, synthetic |
| DXF with HATCH | `public/hatches_1.dxf` (10), `public/hatches_2.dxf` (2) | real source, exporter unverified |
| DXF with INSERT and blocks | `public/uncommon.dxf`, `public/dimension_in_nested_blocks.dxf`, `public/acad_table_with_blk_ref.dxf`; MINSERT only in `synthetic/blocks_minsert.dxf` (counters.py shows minsert=0 in every public DXF) | real source, synthetic |
| DXF with DIMENSION | `public/uncommon.dxf` (13), `public/dimension_in_nested_blocks.dxf` (1); `synthetic/blocks_minsert.dxf` (1) | real source, synthetic |
| Legacy 8-bit cp1251 | `synthetic/legacy_cp1251.step`, `synthetic/legacy_cp1251.dxf` | synthetic |
| Legacy 8-bit Latin-1 | `synthetic/legacy_latin1.step`, `synthetic/legacy_latin1.dxf` | synthetic |

Also real: `public/nist_stc_06_asme1_ap242-e3.stp` carries 229 `\X2\` escapes, `public/nist-d2mi-827-9999-906.stp` is an NX export in inch units, `public/nist_ctc_05_asme1_ap242-e1.stp` writes the inch unit in lower case.

## Not found in open form

- Real STEP or DXF with raw cp1251 or Latin-1 text (Cyrillic or Western European CAD): the legacy fixtures are synthetic.
- A real IGES that is surfaces only and was not exported by FreeCAD: both real surface files in the corpus come from FreeCAD-library.
- A real DXF with dimensions from a CAD exporter under an allowed licence: the DXF files come from the ezdxf repository, and for most of them the exporter is not recorded.
- A real DXF with MINSERT.
- Any open STEP or STL above about 52 MB; everything larger is derived and says so.
- A spread of exporters among the committed small parts: every committed FreeCAD-library part is FreeCAD-exported; the NX export (D2MI) and the exporter-stripped NIST files are the only other origins among committed files, and SolidWorks 2019 appears only in a fetched assembly.

## Licence notices

ezdxf (the DXF files above), MIT licence:

> MIT License
>
> Copyright (c) 2020 Manfred Moitzi
>
> Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:
>
> The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.
>
> THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

FreeCAD-library parts (CC-BY-3.0): credit is given per file above; licence text at http://creativecommons.org/licenses/by/3.0/legalcode; no modifications were made.
