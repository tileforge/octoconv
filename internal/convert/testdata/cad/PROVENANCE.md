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

<!-- reject-provenance-begin -->
## Rejection fixtures (reject/)
Generated by `python3 scripts/cad-corpus-gen.py --reject` from committed files in `synthetic/` and the stdlib builders of that script, no OCP; committed before any code that judges them; excluded from the three phase-71 corpus walkers so the inventory numbers stay comparable. `stl_binary_ac10_header.stl` and `stl_binary_empty.stl` are acceptance cases kept beside the rejections, for STL-over-magic precedence and for n = 0 accepted at sniff level with the triangle guard in phase 73; `dxf_binary.dxf` is the sentinel followed by zero bytes, not a readable binary DXF, so it exercises the sentinel rule only.
Generation command for every file below: `python3 scripts/cad-corpus-gen.py --reject`

## `reject/step_truncated.step` — 13161 bytes

- Licence: generated
- sha256: daccb1be6ef3dc5b7d0ef34d46e6fa44301133d14cf8b1f700de4e92e33fb352
- Command: see the generation command above

**Proves:** Truncated STEP. The first half of synthetic/asymmetric_l_bracket.step: the ISO-10303-21; magic and HEADER; are present, the END-ISO-10303-21; trailer is not, so a head-only check would call it STEP and the tail window must refuse it.

## `reject/step_missing_header.step` — 15567 bytes

- Licence: generated
- sha256: 24e13f3f4e52efc01f03f680c1a0b8c1296bee0b14ebb339c743caa29d79c522
- Command: see the generation command above

**Proves:** STEP without a header section. synthetic/units_inch.step with its single HEADER; replaced by HEADEX;: the magic and the trailer are present, so only the HEADER; requirement can refuse it.

## `reject/step_trailing_nul.step` — 15568 bytes

- Licence: generated
- sha256: dd1d1f20951dd824d3135c1c3f0472421b0238fcb18ca5a9b6b69c516b4f7deb
- Command: see the generation command above

**Proves:** STEP with a trailing NUL byte. synthetic/units_inch.step plus one 0x00 after the trailer: ASCII whitespace stripping does not remove a NUL, so the trailer is not at the end and the file is refused.

## `reject/step_bom.step` — 15570 bytes

- Licence: generated
- sha256: 380464a93a0ef2b0d94fc692b1d673749d3f6b25b0b577ed621642aa937f14ed
- Command: see the generation command above

**Proves:** STEP behind a UTF-8 byte order mark. EF BB BF followed by synthetic/units_inch.step: the magic is not at the start of the file, so the file stays refused until a differential against the engine decides otherwise.

## `reject/iges_truncated.iges` — 12312 bytes

- Licence: generated
- sha256: 7147865072293abf16febf939ed9223ff8bcf507fbcbcc3f84d7868a31083c20
- Command: see the generation command above

**Proves:** Truncated IGES. synthetic/surfaces_only.iges without its last 81-byte record, the T terminate record: the start, global, directory and parameter sections are intact and the terminate record is not.

## `reject/dxf_truncated.dxf` — 212 bytes

- Licence: generated
- sha256: 113bd89166ed4233587ebb5ea143b8ac1390240fb1558cb5f8ddcb06aeb22b5e
- Command: see the generation command above

**Proves:** Truncated ASCII DXF. The comments_999 pair list without its final (0, EOF) pair: the file ends after ENDSEC, so the EOF group is missing from the tail.

## `reject/dxf_bom.dxf` — 225 bytes

- Licence: generated
- sha256: 32e26185b5e59c51b647322242a75eaf5e06b1d2af68633aa1921ee2f305ab78
- Command: see the generation command above

**Proves:** ASCII DXF behind a UTF-8 byte order mark. EF BB BF followed by the comments_999 file: the first group code is not at the start of the file, so the file stays refused until a differential decides otherwise.

## `reject/dxf_binary.dxf` — 86 bytes

- Licence: generated
- sha256: 2ad2211c4f3a5bad0bed2027e6ef35f894b4e3d8a34a3b810b8b27824cafa06b
- Command: see the generation command above

**Proves:** Binary DXF sentinel. The 22-byte AutoCAD Binary DXF sentinel followed by 64 zero bytes, not a readable binary DXF: it exercises the sentinel rule only.

## `reject/dwg_ac1027.dwg` — 128 bytes

- Licence: generated
- sha256: 470606dee17e8957a1b9504b284318bc0e5c5f02ac030e46d8a15f7a67c66a05
- Command: see the generation command above

**Proves:** DWG version magic. ASCII AC1027 (AutoCAD 2013) followed by 122 zero bytes, 128 bytes in all: a DWG is recognised by its magic and refused for being a binary format the service has no engine for.

## `reject/stl_binary_truncated.stl` — 683 bytes

- Licence: generated
- sha256: 69417d61a2a8f28812e589694bc224e60b421928cc90e907175d85406f6e6469
- Command: see the generation command above

**Proves:** Truncated binary STL. A 12-triangle box with its last byte removed: the size formula 84 + 50 * n is off by one byte short, and the header carries no solid marker.

## `reject/stl_binary_trailing_byte.stl` — 685 bytes

- Licence: generated
- sha256: 50d6f75b826157ada9160be986eb57457d61d6b585c232a1f33bbaedcba168ef
- Command: see the generation command above

**Proves:** Binary STL with a trailing byte. A 12-triangle box plus one 0x00: the size formula is off by one byte long.

## `reject/stl_ascii_truncated.stl` — 1502 bytes

- Licence: generated
- sha256: f1c02fe7bc19eb2f8e0cd4dab175d64ea80612f67be3c8d5ab4485a05738cd3d
- Command: see the generation command above

**Proves:** Truncated ASCII STL. The ascii box cut at the first byte of its 7th facet normal line: it starts with solid and carries six whole facets but no endsolid line.

## `reject/stl_ambiguous.stl` — 684 bytes

- Licence: generated
- sha256: ee6938b82fe1930cc60f18b8c06d0fd5971b1b68cd194bc3272fa581edf2a09d
- Command: see the generation command above

**Proves:** Ambiguous STL. A binary box whose 80-byte header starts with solid ambiguous and whose final 10 bytes are LF endsolid LF: the size formula holds, it starts with solid and its last line is endsolid, so both the ASCII and the binary reading are valid on the markers.

## `reject/stl_binary_ac10_header.stl` — 684 bytes

- Licence: generated
- sha256: 3087e5d0d1e956bcbfe3df19e60f3b1884acf56a9161150bbe4b728ad43b221c
- Command: see the generation command above

**Proves:** Binary STL whose header starts with AC1027. A 12-triangle box whose size formula holds: acceptance case kept beside the rejections, so that the STL size formula takes precedence over the DWG magic.

## `reject/stl_binary_empty.stl` — 84 bytes

- Licence: generated
- sha256: 99ce674a40036f72d61426bc10c46f5e5a3bb455bb32a466ad13608a2f04b3ad
- Command: see the generation command above

**Proves:** Binary STL with no triangles. An 80-byte header plus uint32 0, 84 bytes: acceptance case kept beside the rejections, n = 0 passes the size formula at sniff level and the triangle guard belongs to phase 73.

<!-- reject-provenance-end -->

---

## Phase 75 DXF fixtures (`dxf/`)

- Licence: generated
- Origin: Every file is written by `scripts/cad-corpus-gen.py`; no third-party material.
- Generation command: `scripts/cad-dxf-probe.sh fixtures --write`, which runs `/opt/venv/bin/python -B scripts/cad-corpus-gen.py --dxf-fixtures --out-dir OUT` inside the engine image of `Dockerfile.cad-worker` (ezdxf 1.4.4) and copies the 54 files here. `scripts/cad-dxf-probe.sh fixtures` without `--write` regenerates them and requires every committed file to be byte-identical.
- Determinism: `PYTHONHASHSEED=0` (the generator refuses to run without it, because ezdxf writes the OBJECTS section in set order), `python -B` rather than `-I` so the seed is honoured, `ezdxf.options.write_fixed_meta_data_for_testing = True` (fixed timestamps and GUIDs), ezdxf pinned to 1.4.4. Byte-level edits and the stdlib-written files use fixed literals only.
- Check: `python3 scripts/cad-corpus-gen.py --dxf-fixtures --check-only` needs only the standard library and checks the byte markers of every file; the sha256, size and ezdxf strict-read facts of each file are the `FIXTURE` lines in `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`.
- `text_alpha` and `text_alpha_faint` were added by quick task 261012-o2d; their `FIXTURE` lines, and those of the 41 earlier files regenerated byte-identical beside them, are in `.planning/quick/261012-o2d-codex-pr36-p2-preserve-dxf-text-opacity-/evidence/probe-fixtures.txt` (image id `sha256:3f7941452549cd5ddd758c7aff3afc25fff793bc823738f85d09427e75efe118`).
- `text_fonts` was added by quick task 261012-qds; its `FIXTURE` line, and those of the 43 earlier files regenerated byte-identical beside it, are in `.planning/quick/261012-qds-codex-pr36-round2-oblique-font-style-dxf/evidence/probe-fixtures.txt` (image id `sha256:2c41529bad72b32a82047ed53147f549b054bfdd573ed94c55aec330e9a110de`).
- `alpha_clear_geometry`, `alpha_clear_all`, `alpha_one_visible` and `alpha_one_fill` were added by quick task 261012-u2s; their `FIXTURE` lines, and those of the 44 earlier files regenerated byte-identical beside them, are in `.planning/quick/261012-u2s-codex-pr36-round3-fully-transparent-dxf/evidence/probe-fixtures.txt` (image id `sha256:5958d4199ce7a728aef340e368b6ebce07fbf0f91b15ae060033917ea838d230`).
- `white_geometry`, `white_text`, `white_one_black`, `degenerate_fill`, `faint_only` and `zwsp_text` were added by quick task 261013-0og; their `FIXTURE` lines, and those of the 48 earlier files regenerated byte-identical beside them, are in `.planning/quick/261013-0og-codex-pr36-round4-dxf-emptiness-judged-b/evidence/probe-fixtures.txt` (image id `sha256:10d5b44c9d1cea5e71e6ee088fd544280b94e006128b4f4e7a43f9d301683965`).
- All 54 files sniff as `dxf` with no create-time rejection (`TestDXFFixturesSniffAsDXF`).

| Group | Files | Built with | Purpose |
|---|---|---|---|
| Text | `text_cases`, `text_illegal_chars` | ezdxf R2018; `text_illegal_chars` is an ezdxf file whose TEXT value is byte-edited to `AB\x01CD\xffEF` | Text kept as SVG text: `&<>"'` and a script tag round trip, rotation, alignment, FIT/ALIGNED, width factor, `%%c %%d %%p`, MTEXT codes, ATTRIB, dimension text, glyphs outside DejaVu; a C0 control and an invalid UTF-8 byte that must not break the XML |
| Layers | `layers_off_frozen`, `all_hidden` | ezdxf R2018 | Off and frozen layers are absent from the drawing; a drawing with only hidden content is empty |
| Transparency | `text_alpha`, `text_alpha_faint` | ezdxf R2018 | Opaque, 50% (alpha 127, opacity 0.498) and 100% transparent TEXT and MTEXT, a 50% and a 100% two-paragraph MTEXT and a 50% MTEXT with an inline `\C1` colour (ezdxf's complex MTEXT path), a TEXT on a 50% layer (ByLayer), a ByBlock TEXT in a 50% INSERT, a 50% LINE, solid HATCH and XLINE inside an opaque frame; a drawing whose only ink is one 50% TEXT beside one 100% TEXT |
| Nothing visible | `alpha_clear_geometry`, `alpha_clear_all`, `alpha_one_visible`, `alpha_one_fill` | ezdxf R2018 | A LINE, CIRCLE, ARC, LWPOLYLINE, solid HATCH, SOLID, POINT and XLINE at 100% transparency, a LINE on a 100% layer (ByLayer) and a ByBlock LINE in a 100% INSERT; `alpha_clear_all` adds a 100% TEXT and MTEXT; `alpha_one_visible` adds one 50% LINE and `alpha_one_fill` one 50% solid HATCH, the drawing's only visible element |
| Nothing on a white page | `white_geometry`, `white_text`, `white_one_black`, `degenerate_fill`, `faint_only`, `zwsp_text` | ezdxf R2018 | Opaque drawings that leave the white page white: a LINE, CIRCLE, ARC, LWPOLYLINE, solid HATCH, SOLID and POINT in true colour white (420 = 16777215) and a LINE in ACI 255; a TEXT and an MTEXT in true colour white; both of those plus one ACI 7 LINE, which draws black (the one that must convert); a SOLID whose four points are collinear and a HATCH whose boundary encloses no area; a LINE, a solid HATCH and a TEXT at transparency alpha 1 of 255 (440 = 33554433, opacity 0.004); a fully transparent LINE that sets the frame and an opaque TEXT of only U+200B |
| Fonts | `text_fonts` | ezdxf R2018 | One TEXT per text style naming each of the six faces of `fonts-dejavu-core` and the two Sans Mono oblique files (absent from the image, so they fall back to `DejaVuSans.ttf`), two styles with extended font data (family DejaVu Sans Mono, italic and italic bold), an MTEXT with an inline `\fDejaVu Sans Mono|b1|i1;` font, a style with oblique angle 15 and width 0.7 and a TEXT with its own oblique angle 15 and width factor 0.7, inside a frame |
| Never resolved | `never_resolved` | ezdxf R2018 | IMAGE naming `/etc/passwd` and `../../outside/secret.png`, PDFUNDERLAY naming `/etc/hostname`, an XREF to `/etc/passwd` and an OLE2FRAME beside visible content |
| Framing | `wrong_extents`, `header_far_extents`, `wide`, `tall`, `far_point`, `single_point`, `xline_ray` | ezdxf R2018; the two extents files have `$EXTMIN`/`$EXTMAX` byte-edited to ±1e9 and ±1e20 | Frame and bbox come from the drawn entities, not the header; extreme aspect ratios; a stray far point; a zero-size drawing; XLINE/RAY with an unnormalised direction (999, 0, 0) |
| Units | `units_0`, `units_1`, `units_2`, `units_3`, `units_4`, `units_5`, `units_6`, `units_13`, `units_21`, `units_missing` | ezdxf R2018 | `$INSUNITS` mapping to the measurements vocabulary, codes outside it, and a file without the variable |
| Block bombs | `cyclic_self`, `cyclic_ab`, `cyclic_unreferenced`, `cyclic_case`, `nest_d16`, `nest_d17`, `minsert_over_inserts`, `entities_over`, `dimension_minsert` | standard library (R12 pairs) | Cycles (self, mutual, unreferenced, `A` inserting `a`), nesting at and one above the derived bound 16, one MINSERT of 16 x 10627 = 170032 cells (one above the 170031 cap), 851 inserts of a 1000-entity block (851000 expanded entities, above 850155), a DIMENSION block holding a 1000 x 1000 MINSERT |
| Numbers | `nonnumeric_abc`, `nonnumeric_comma`, `nonnumeric_nan`, `nonnumeric_inf`, `nonnumeric_underscore`, `coord_1e300`, `coord_1e400` | standard library (R12 pairs) | One LINE x coordinate in ENTITIES is `abc`, `1,5`, `nan`, `inf`, `1_0`, `1e300` or `1e400` |
| Section scope | `nonfinite_header`, `nonfinite_tables`, `nonfinite_objects` | ezdxf R2018, one code-40 value byte-edited | `$LTSCALE` = `nan` in HEADER, a VPORT code 40 = `inf` in TABLES, a LAYOUT (AcDbPlotSettings) code 40 = `1_0` in OBJECTS, with ENTITIES and BLOCKS finite |

## `dxf/all_hidden.dxf` — 16037 bytes

- Licence: generated
- sha256: 459e79e524ca7d73bd7ca11dd3e5a96a4119062a00bebd790736a31454762356
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/alpha_clear_all.dxf` — 17652 bytes

- Licence: generated
- sha256: 0ede455e97af4f644bd28f35b5172eecb82ddc8c15871d771eac1ad4566a841b
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261012-u2s-codex-pr36-round3-fully-transparent-dxf/evidence/probe-fixtures.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/alpha_clear_geometry.dxf` — 17365 bytes

- Licence: generated
- sha256: ba5a57e46da1d38e8ec63c17f35606a72ee3e43a07ed349aafd8b9e9074f5adf
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261012-u2s-codex-pr36-round3-fully-transparent-dxf/evidence/probe-fixtures.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/alpha_one_fill.dxf` — 17930 bytes

- Licence: generated
- sha256: e5fe228e42020556c877a6910beda9c97c7ac94f67422f2efebb1d790e2e9a03
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261012-u2s-codex-pr36-round3-fully-transparent-dxf/evidence/probe-fixtures.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/alpha_one_visible.dxf` — 17776 bytes

- Licence: generated
- sha256: 3428b4e1a909989e8c7f4ee594eefa6813dc45fbecee3fce4a14e74865d57422
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261012-u2s-codex-pr36-round3-fully-transparent-dxf/evidence/probe-fixtures.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/coord_1e300.dxf` — 319 bytes

- Licence: generated
- sha256: a7918997414ccd8e15817091f082223d5c841ed94e53d8b461ee01320a57d5b0
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/coord_1e400.dxf` — 319 bytes

- Licence: generated
- sha256: 649dfb1e21035b2ebb32c4b4a8ccd3c304f9a8628bdf74d4219cd636155578a9
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/cyclic_ab.dxf` — 722 bytes

- Licence: generated
- sha256: f50320aea5ea1558f144886e47b36cbf9686b124285d3f6d3fa948b5232f0654
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/cyclic_case.dxf` — 488 bytes

- Licence: generated
- sha256: 9cafae08f1efe86506963dd759f0ea780945f46681c92cd3b85cfe688f7f928e
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/cyclic_self.dxf` — 570 bytes

- Licence: generated
- sha256: 2adecb7704be0f3b2b0a2284fc2f31cac2f0dc6153e4535fb5b1c47197be1927
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/cyclic_unreferenced.dxf` — 745 bytes

- Licence: generated
- sha256: ec08e16683ec10c2867a2581cbf68889077f421d3348248bf3b753b1a3a511fd
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/degenerate_fill.dxf` — 15665 bytes

- Licence: generated
- sha256: 33353ef67563c8b9b61e4ca0e6ef4c4aaadcff2253be7e4156f9feab4f7b0103
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261013-0og-codex-pr36-round4-dxf-emptiness-judged-b/evidence/probe-fixtures.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/dimension_minsert.dxf` — 1050 bytes

- Licence: generated
- sha256: 04d2d945b5cfb96877d0663a392e9c32e02d008aa7845211949504b399065af7
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/entities_over.dxf` — 133801 bytes

- Licence: generated
- sha256: 3ff090e0276e852976e554e4a91a95b95100fed04af71b60fe2403bc2db1af8f
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/faint_only.dxf` — 15810 bytes

- Licence: generated
- sha256: 2e50614e2f603f56b8db4da3910e45265f12a2d1138949144ad2facc501664de
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261013-0og-codex-pr36-round4-dxf-emptiness-judged-b/evidence/probe-fixtures.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/far_point.dxf` — 15887 bytes

- Licence: generated
- sha256: 592f8629bd8216697d3f27e21f2a41d1788e955973ef440f7e30939e9c879a48
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/header_far_extents.dxf` — 15368 bytes

- Licence: generated
- sha256: c7812721a011bdbd64e7a1ba8376a571abe05fffcc1fd0184db1c1f52a1b6b99
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/layers_off_frozen.dxf` — 16283 bytes

- Licence: generated
- sha256: 662533fc0a8d2e442b7e8150b31d58869d3deda43954799ecccdc93c30845080
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/minsert_over_inserts.dxf` — 784 bytes

- Licence: generated
- sha256: 3a692b00607ce0bf234b7d381e07ba8ecf765f49d115f1c08359844e553ad78c
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/nest_d16.dxf` — 2808 bytes

- Licence: generated
- sha256: 8d6e30b85a216102defee6d8e12d58b05b59ff484830d8861c21f228d9d77a50
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/nest_d17.dxf` — 2968 bytes

- Licence: generated
- sha256: 9d23cd31221c41024bb2115a7a1815762085a9969cccf1eb469ee5dbc85a0a7c
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/never_resolved.dxf` — 18288 bytes

- Licence: generated
- sha256: b41ae3516de03a9421ac8734202f8c937f3dc7138cdc469e5982384efbceeae8
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/nonfinite_header.dxf` — 15365 bytes

- Licence: generated
- sha256: d6c8856888b88703fbf9a45bc3641631072b0f5e2d1f6614de4d6435ceeef331
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/nonfinite_objects.dxf` — 15365 bytes

- Licence: generated
- sha256: 23042031f88c5e2d60264b19dff5b7c1dd2a70f66aa00d6547d5bdaa2c8b3186
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/nonfinite_tables.dxf` — 15362 bytes

- Licence: generated
- sha256: 99f4aecc8f904c83812865ea10a43962ce0eaf281e46fdc82fa0868d2aef35a7
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/nonnumeric_abc.dxf` — 317 bytes

- Licence: generated
- sha256: 748f988889a42216a662a7ee06520e76938aa9f827dff65401beea7f5813e581
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/nonnumeric_comma.dxf` — 317 bytes

- Licence: generated
- sha256: 48ca7e03c97f4e4ab8f21cd433cff4cce377cf0f2e50060f90f7cdc0d9872bd4
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/nonnumeric_inf.dxf` — 317 bytes

- Licence: generated
- sha256: 6514420c01cbb7e24e8357b99a05c9a28098e581781f0c67506d15d1e41543c3
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/nonnumeric_nan.dxf` — 317 bytes

- Licence: generated
- sha256: 1f2f0ee582a2d624a0b4645188f2e725d28c7c135ac3acb599939e2ba102692c
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/nonnumeric_underscore.dxf` — 317 bytes

- Licence: generated
- sha256: a32565e3e3c006ba443371c97585c04a0d449a925a1d30ed44dab80694c1c39c
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/single_point.dxf` — 15342 bytes

- Licence: generated
- sha256: 7395ff64f57d00409d9cbcc9dcd1301b7f9659ee1f5eb33ccb9c99967f1f6ac1
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/tall.dxf` — 15830 bytes

- Licence: generated
- sha256: 57866cf809178b73e22022d80ac3ab64e883e86ef176980af8b374554fb13232
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/text_alpha.dxf` — 18418 bytes

- Licence: generated
- sha256: cef5f5e969d84c72e811ce65100d5986a7f5b54d2dc3920d2671829ca7bc9962
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261012-o2d-codex-pr36-p2-preserve-dxf-text-opacity-/evidence/probe-fixtures.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/text_alpha_faint.dxf` — 15567 bytes

- Licence: generated
- sha256: b4e08323fa71db35dc26e9af614c66c3decaa2866a09b254c74cb506300d6c8d
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261012-o2d-codex-pr36-p2-preserve-dxf-text-opacity-/evidence/probe-fixtures.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/text_cases.dxf` — 58961 bytes

- Licence: generated
- sha256: 7fc6e1c393ebef985f203193cb66d300e28392a6fc79e90ac8b72192a009729f
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/text_fonts.dxf` — 19601 bytes

- Licence: generated
- sha256: 72e1097b8aa6731bb70590bffa220b6b80c238121d5fd087cb3b7519c98fa02e
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261012-qds-codex-pr36-round2-oblique-font-style-dxf/evidence/probe-fixtures.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/text_illegal_chars.dxf` — 15506 bytes

- Licence: generated
- sha256: f65a360e07e9411f661222c28d47bb18b7d9683c37809cc75ba16979cb0468ab
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/units_0.dxf` — 15365 bytes

- Licence: generated
- sha256: 98d4c7faa946c9f5925bc0b323a131a2d241fd68ac4390527669d0c820bb89c8
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/units_1.dxf` — 15365 bytes

- Licence: generated
- sha256: 790d7471e1d249604b4954fd16ea73c84f2eaaae459a6742b70e5ad92c2ca402
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/units_13.dxf` — 15366 bytes

- Licence: generated
- sha256: 2da5bf290d0575366988aa09c445e39cb12e260e8aff03e41100d610cb9aaccc
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/units_2.dxf` — 15365 bytes

- Licence: generated
- sha256: a9761601a43cfb5fa961b694332c7195e967818a16254fe2caf6189ac2348e97
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/units_21.dxf` — 15366 bytes

- Licence: generated
- sha256: 77dfc1e3f9b53250c4d2c47863790e405b6710e32a2c7dda5d86b7fa7525c6db
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/units_3.dxf` — 15365 bytes

- Licence: generated
- sha256: 37cd41cf237c22a503cd2396e2fed04fb58b6d9dddd8bbcd464bbac115d8b7c1
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/units_4.dxf` — 15365 bytes

- Licence: generated
- sha256: d9c11a7d5b54ca6a9356485761a40321e677b4a5bd749c25d49392f94b5e99bf
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/units_5.dxf` — 15365 bytes

- Licence: generated
- sha256: a217b68db08342608a7ad7a0af302734a3f3383c9a4b61aa0a5f16ddee1b679a
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/units_6.dxf` — 15365 bytes

- Licence: generated
- sha256: 47a70280e10cddeb3e9dd6db99ae71c302dd3bfa511c5d9cc047b0753f75a6fa
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/units_missing.dxf` — 15345 bytes

- Licence: generated
- sha256: c805ce7d4ec84be199ad96f1e50c73e530c3962bc4332ef954e0ae3b4e087846
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/white_geometry.dxf` — 16433 bytes

- Licence: generated
- sha256: 3b11df754ed651c1dbe1e8c90fc79117ad143f60a8f2fd679213e92352b212bf
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261013-0og-codex-pr36-round4-dxf-emptiness-judged-b/evidence/probe-fixtures.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/white_one_black.dxf` — 16967 bytes

- Licence: generated
- sha256: 38355868449f58996b9fa5cb9bae669db15060081352e75f9b7f00a76e6a52a6
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261013-0og-codex-pr36-round4-dxf-emptiness-judged-b/evidence/probe-fixtures.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/white_text.dxf` — 15546 bytes

- Licence: generated
- sha256: 0326e47c5016c101f265de93578fd8826ae36698e12b3f8a54a5d4e07aae6a9f
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261013-0og-codex-pr36-round4-dxf-emptiness-judged-b/evidence/probe-fixtures.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/wide.dxf` — 15830 bytes

- Licence: generated
- sha256: 8ce03c03bf4a550620415ac106a266c616178e3bc0773c20d0a04b456d7a050d
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/wrong_extents.dxf` — 15391 bytes

- Licence: generated
- sha256: ba89caaa5c385e6c257e73fb0ede33ea8d6214b946daf128a2ebe5219f620b73
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/xline_ray.dxf` — 15935 bytes

- Licence: generated
- sha256: 7cf660ec3fdbee4bdae132f6dd96e78e62114254277c1af20b793a0ac762d6c4
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt`, the FIXTURE line of this file (same sha256 and bytes)

## `dxf/zwsp_text.dxf` — 15519 bytes

- Licence: generated
- sha256: 0e0df3e5b0752e0a9c32867340ad25c8ff5299c6431b46bc49cf9d2307d5468b
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261013-0og-codex-pr36-round4-dxf-emptiness-judged-b/evidence/probe-fixtures.txt`, the FIXTURE line of this file (same sha256 and bytes)

---

## Phase 75 helper outputs (`dxf-out/`)

- Licence: generated
- Origin: Every file is an output of this repository's `cmd/cad-worker/helper/cad-dxf` (and of `rsvg-convert` run on that output) for a fixture listed above; no third-party material beyond the corpus files they were drawn from (`public/uncommon.dxf`, `public/hatches_2.dxf`, MIT, notices in this document).
- Generation command: `scripts/cad-dxf-probe.sh helper --group all --write-out`. Every run is confined by the candidate Landlock sets of 75-01 under `--network none --cpus 2 --memory 4g --memory-swap 4g --user nobody` with HOME and XDG under the job dir: `cad-dxf --max-drawn-entities 850155 --texts out.texts.json --render-svg out.render.svg --measure out.measure.json in.dxf out.svg`, then `rsvg-convert -f pdf -b white` and the letterboxed PNG argv of D-08 (`[-b white] -w sw -h sh --page-width S --page-height S --left left --top top`), both on the render-only SVG (75-CONTEXT R-9).
- Two SVGs per drawing (R-9): `<name>.svg` is the client SVG with the exact text, the only SVG returned for dxf to svg; `<name>.render.svg` is the same document with every character outside the resolved DejaVu cmap replaced by U+FFFD, and it is the only input `rsvg-convert` gets. Only `text_cases` has such characters (2 substituted).
- Image: `octoconv-cad-worker:engine75-88915fc55680`, image id `sha256:f6bb26df31da648ba52fd97c75f644190d2444b0eca29dc5ac14f5def93b432f`, built from commit `a1cb958` (`Dockerfile.cad-worker --target engine`). The sha256 and size of every file committed by 75-02 are the `OUT` lines in `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, except `frames.tsv`: 82ab86b (quick 261012-o2d) rewrote it, and its `OUT` line is in `.planning/quick/261012-o2d-codex-pr36-p2-preserve-dxf-text-opacity-/evidence/probe-helper.txt`. The files added by later quick tasks are named in the table below, and each file's own section below names its evidence line.
- The PDFs carry a creation date, so a refresh changes their bytes; the SVG, texts.json, drafts and PNGs are deterministic for a given image.

| Files | Source | What they are for |
|---|---|---|
| `text_cases.svg`, `.render.svg`, `.texts.json`, `.pdf` | `dxf/text_cases.dxf` | SVG text and texts.json cross-check, the `&<>"'` round trip, coverage of characters outside DejaVu (U+FFFD in the render-only SVG and in the PDF), a PDF with extractable text |
| `legacy_cp1251.svg`, `.render.svg`, `.texts.json`, `.pdf` | `synthetic/legacy_cp1251.dxf` | One text element `Деталь` and its PDF |
| `uncommon.svg`, `.render.svg`, `.texts.json`, `.pdf` | `public/uncommon.dxf` | 49 texts, dimensions, leaders and multileaders |
| `hatches_2.svg`, `.render.svg`, `.texts.json`, `.pdf` | `public/hatches_2.dxf` | Filled paths with text |
| `wide.svg`, `.render.svg`, `.texts.json`, `.measure.json`; `tall.svg`, `.render.svg`, `.texts.json` | `dxf/wide.dxf`, `dxf/tall.dxf` | Extreme aspect ratios and a dxf draft |
| `text_alpha.svg`, `.render.svg`, `.texts.json`, `.pdf`; `text_alpha_faint.svg`, `.render.svg`, `.texts.json`, `.pdf`, `text_alpha_faint_1280_opaque.png`, `text_alpha_faint_1280_transparent.png` | `dxf/text_alpha.dxf`, `dxf/text_alpha_faint.dxf` | `fill-opacity` on transparent text and `stroke-opacity`/`fill-opacity` on transparent shapes, `invisible` in texts.json, a PDF without the fully transparent texts, and frames whose only ink is half-transparent text. Added by quick task 261012-o2d with `scripts/cad-dxf-probe.sh helper --group semantics --write-out` (image id `sha256:3f7941452549cd5ddd758c7aff3afc25fff793bc823738f85d09427e75efe118`); their `OUT` lines are in `.planning/quick/261012-o2d-codex-pr36-p2-preserve-dxf-text-opacity-/evidence/probe-helper.txt`. That run rewrote the other files of this directory too: every SVG, texts.json, draft and PNG came out byte-identical, and the six earlier PDFs differed only in `/CreationDate`, so the committed bytes (and the 75-02 `OUT` lines) were kept |
| `text_fonts.svg`, `.render.svg`, `.texts.json`, `.pdf` | `dxf/text_fonts.dxf` | The font attributes cad-dxf derives from the font it measured with (family, weight, style, stretch) and a PDF that embeds exactly those faces. Added by quick task 261012-qds with `scripts/cad-dxf-probe.sh helper --group semantics --write-out` (image id `sha256:2c41529bad72b32a82047ed53147f549b054bfdd573ed94c55aec330e9a110de`); their `OUT` lines are in `.planning/quick/261012-qds-codex-pr36-round2-oblique-font-style-dxf/evidence/probe-helper.txt`. That run rewrote the other files of this directory too: every SVG, texts.json, draft and PNG came out byte-identical, and the eight earlier PDFs differed only in `/CreationDate`, so the committed bytes were kept |
| `alpha_one_visible.svg`, `.render.svg`, `.texts.json`, `.pdf` | `dxf/alpha_one_visible.dxf` | A drawing whose only visible element is one 50% line among fully transparent geometry and two fully transparent texts: Visible is 1 in both SVGs, and the PDF holds the background and that line as its only paint. Added by quick task 261012-u2s with `scripts/cad-dxf-probe.sh helper --group semantics --write-out` (image id `sha256:5958d4199ce7a728aef340e368b6ebce07fbf0f91b15ae060033917ea838d230`); their `OUT` lines are in `.planning/quick/261012-u2s-codex-pr36-round3-fully-transparent-dxf/evidence/probe-helper.txt`. That run rewrote the other files of this directory too: every SVG, texts.json, draft and PNG came out byte-identical, and the nine earlier PDFs differed only in `/CreationDate`, so the committed bytes were kept |
| `white_one_black.svg`, `.render.svg`, `.texts.json`, `.pdf` | `dxf/white_one_black.dxf` | A drawing whose only ink on a white page is one black line among true colour white geometry and two white texts: Visible counts every white element, the ink check finds the black line, and the PDF keeps the white texts in its text layer. Added by quick task 261013-0og with `scripts/cad-dxf-probe.sh helper --group semantics --write-out` (image id `sha256:10d5b44c9d1cea5e71e6ee088fd544280b94e006128b4f4e7a43f9d301683965`); their `OUT` lines are in `.planning/quick/261013-0og-codex-pr36-round4-dxf-emptiness-judged-b/evidence/probe-helper.txt`. That run rewrote the other files of this directory too: every SVG, texts.json, draft and PNG came out byte-identical, and the ten earlier PDFs differed only in `/CreationDate`, so the committed bytes were kept |
| `wrong_extents.measure.json`, `units_3.measure.json` | `dxf/wrong_extents.dxf`, `dxf/units_3.dxf` | A draft whose bbox comes from the drawn line, not the header; a draft with an empty unit and raw code `3` |
| `nofont.pdf` | `text_cases.render.svg` drawn by `rsvg-convert` under a set without `/usr/share/fonts` | A PDF with no extractable text (Pitfall 1), which the PDF judge must reject |
| `outline.pdf` | `text_cases.dxf` drawn with ezdxf's stock SVG backend (text as paths) through the probe harness, then `rsvg-convert` | A PDF whose text is only outlines, which the PDF judge must reject |
| `wide_1280_opaque.png`, `wide_1280_transparent.png`, `tall_256_opaque.png` | `wide.render.svg`, `tall.render.svg` | Letterboxed frames for the pixel judge |
| `frames.tsv` | the five PNG runs | `name, size, background, w_mm, h_mm, sw, sh, left, top` of each PNG (tab separated, header row first) |

## `dxf-out/alpha_one_visible.pdf` — 1060 bytes

- Licence: generated
- sha256: 34efcd9ebe492d03b002d3fdf717f59575ffd76e2257959b37ca764551bdaf3c
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261012-u2s-codex-pr36-round3-fully-transparent-dxf/evidence/probe-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/alpha_one_visible.render.svg` — 1894 bytes

- Licence: generated
- sha256: db658252767f654fc0528538feed667ed5caf7048839bdc7f71f72ef4298d71d
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261012-u2s-codex-pr36-round3-fully-transparent-dxf/evidence/probe-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/alpha_one_visible.svg` — 1894 bytes

- Licence: generated
- sha256: db658252767f654fc0528538feed667ed5caf7048839bdc7f71f72ef4298d71d
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261012-u2s-codex-pr36-round3-fully-transparent-dxf/evidence/probe-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/alpha_one_visible.texts.json` — 137 bytes

- Licence: generated
- sha256: b5b21139f3e572208ed605c6df96acbca1be35798d6f07625516878e84903d96
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261012-u2s-codex-pr36-round3-fully-transparent-dxf/evidence/probe-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/frames.tsv` — 375 bytes

- Licence: generated
- sha256: 03f91fb932669204bd973ee9fca00fbf8384dbd9db2eaa3a7c1bc9a88cd6d6bf
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261012-o2d-codex-pr36-p2-preserve-dxf-text-opacity-/evidence/probe-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/hatches_2.pdf` — 15013 bytes

- Licence: generated
- sha256: 53fb9a11bcf0c61ce6fb1381d718b14711f2ff2601713b76ca0c051a165f1538
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/hatches_2.render.svg` — 17337 bytes

- Licence: generated
- sha256: 8dbb251da794792a16747b5a8879bd9b8aeb26a9ae1f8a184af12457645c4694
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/hatches_2.svg` — 17337 bytes

- Licence: generated
- sha256: 8dbb251da794792a16747b5a8879bd9b8aeb26a9ae1f8a184af12457645c4694
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/hatches_2.texts.json` — 241 bytes

- Licence: generated
- sha256: 00019b8c5f1bdc0943ad4705738ccf861d50ebbc44d6ac02d1e31ab02d25a028
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/legacy_cp1251.pdf` — 4946 bytes

- Licence: generated
- sha256: 8533ce1fb9a4005f837659ca5cf401c76413c437ef3ead584ec0086916a7b501
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/legacy_cp1251.render.svg` — 455 bytes

- Licence: generated
- sha256: a34f365aaf1fa4fa2374b0331f343da6c0c7fddea7b5677352ddda1b91b6fa4e
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/legacy_cp1251.svg` — 455 bytes

- Licence: generated
- sha256: a34f365aaf1fa4fa2374b0331f343da6c0c7fddea7b5677352ddda1b91b6fa4e
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/legacy_cp1251.texts.json` — 108 bytes

- Licence: generated
- sha256: 22d7177dcba622634f648865ebf69714d63932200f3cfad39d4c02a1f82d84cb
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/nofont.pdf` — 2298 bytes

- Licence: generated
- sha256: a0ca6b77070c34fddc32e5e70804b6d1af0cbb1d32a4e6dfc10f684a3d17ebce
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/outline.pdf` — 30820 bytes

- Licence: generated
- sha256: 9851dd6a5ecc12f4ff9ab68259f864bd1a237a8b255e506b3d45e8439372883d
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/tall.render.svg` — 753 bytes

- Licence: generated
- sha256: 4f1d9991253f9c75461e091bdc038b871e4e1369110ebe30aff26a313d6fd645
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/tall.svg` — 753 bytes

- Licence: generated
- sha256: 4f1d9991253f9c75461e091bdc038b871e4e1369110ebe30aff26a313d6fd645
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/tall.texts.json` — 44 bytes

- Licence: generated
- sha256: a4da04c3bc7d619e4113f1143268eaf3d7f4d6b3de1e5aa7a62f8d57cef92c9a
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/tall_256_opaque.png` — 838 bytes

- Licence: generated
- sha256: 7b64faf953eed667bd5c51a46b2d5448867b10ebbd3669197e774b5d89dabdef
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/text_alpha.pdf` — 7300 bytes

- Licence: generated
- sha256: 99e6a11bd505eb696f25b96e5964004484ccf242c9189578afdd6b12566a1e23
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261012-o2d-codex-pr36-p2-preserve-dxf-text-opacity-/evidence/probe-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/text_alpha.render.svg` — 3230 bytes

- Licence: generated
- sha256: 28dccb4948824020b06ae051a5200c07bcf133028ff8522336e9b201ba6fb630
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261012-o2d-codex-pr36-p2-preserve-dxf-text-opacity-/evidence/probe-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/text_alpha.svg` — 3230 bytes

- Licence: generated
- sha256: 28dccb4948824020b06ae051a5200c07bcf133028ff8522336e9b201ba6fb630
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261012-o2d-codex-pr36-p2-preserve-dxf-text-opacity-/evidence/probe-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/text_alpha.texts.json` — 651 bytes

- Licence: generated
- sha256: 0725429b98ab81531893614b41e82283e38d69421ea6bff76bf44fa2ef9acac6
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261012-o2d-codex-pr36-p2-preserve-dxf-text-opacity-/evidence/probe-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/text_alpha_faint.pdf` — 5067 bytes

- Licence: generated
- sha256: 5fe1b6219f8b5dd6f13144a0011ad5c56d6dc317f982c6da66cb28c9d64784d4
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261012-o2d-codex-pr36-p2-preserve-dxf-text-opacity-/evidence/probe-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/text_alpha_faint.render.svg` — 639 bytes

- Licence: generated
- sha256: a1f300f87f9738e8080d6018c0bba8e6d8ea5dbb0412ff748632eeca53179549
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261012-o2d-codex-pr36-p2-preserve-dxf-text-opacity-/evidence/probe-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/text_alpha_faint.svg` — 639 bytes

- Licence: generated
- sha256: a1f300f87f9738e8080d6018c0bba8e6d8ea5dbb0412ff748632eeca53179549
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261012-o2d-codex-pr36-p2-preserve-dxf-text-opacity-/evidence/probe-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/text_alpha_faint.texts.json` — 106 bytes

- Licence: generated
- sha256: a5d1910359e59d45d8d3b43a4e619f6197b0ac9662c1ca58c69efef7ef76944c
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261012-o2d-codex-pr36-p2-preserve-dxf-text-opacity-/evidence/probe-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/text_alpha_faint_1280_opaque.png` — 23053 bytes

- Licence: generated
- sha256: 075d73e0b3269f76ba49c922e241834179933bfaf82805f19854af9fa282982e
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261012-o2d-codex-pr36-p2-preserve-dxf-text-opacity-/evidence/probe-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/text_alpha_faint_1280_transparent.png` — 17581 bytes

- Licence: generated
- sha256: abc060fd14b315c0ea65f917a55f77ff5b7bd5083f2ece73d66f0995afd9f899
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261012-o2d-codex-pr36-p2-preserve-dxf-text-opacity-/evidence/probe-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/text_cases.pdf` — 14939 bytes

- Licence: generated
- sha256: 226bc928291420a88a16fc2dd629f01d33696b1031d225a085e93a2c5bae40e1
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/text_cases.render.svg` — 3781 bytes

- Licence: generated
- sha256: 20c3d46812b96fcafa1c634b2314bed056c28f200e1a10594673a9e621c0ee40
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/text_cases.svg` — 3781 bytes

- Licence: generated
- sha256: ce96c27263937945bac512b93d007d4ce1da6c63936047a4403280e6214832ba
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/text_cases.texts.json` — 851 bytes

- Licence: generated
- sha256: e8265853aba7b55458f2c2ad746ee9f038765a5a6441d1c7ed59bdceb1f91e02
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/text_fonts.pdf` — 28794 bytes

- Licence: generated
- sha256: 12e0a43da08c6baf81d626a147b78f133c0e4d7f90d7f0233c69f369010f9054
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261012-qds-codex-pr36-round2-oblique-font-style-dxf/evidence/probe-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/text_fonts.render.svg` — 3116 bytes

- Licence: generated
- sha256: 3eb1977da23f9666faf77ce952dedcbda29abd98696a4a332b96f68966ff360a
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261012-qds-codex-pr36-round2-oblique-font-style-dxf/evidence/probe-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/text_fonts.svg` — 3116 bytes

- Licence: generated
- sha256: 3eb1977da23f9666faf77ce952dedcbda29abd98696a4a332b96f68966ff360a
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261012-qds-codex-pr36-round2-oblique-font-style-dxf/evidence/probe-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/text_fonts.texts.json` — 749 bytes

- Licence: generated
- sha256: db859426bb13a5a57f970102305a852c1b70964dfd80271dd528e39bd26d7d12
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261012-qds-codex-pr36-round2-oblique-font-style-dxf/evidence/probe-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/uncommon.pdf` — 16581 bytes

- Licence: generated
- sha256: a91d6995a213a7921a4cef9c410f190a8cc84d4428dc7fd77df442d328104384
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/uncommon.render.svg` — 24056 bytes

- Licence: generated
- sha256: eb40139afd527928683075ea1f0116da2f293538f4134b610487ea7f07e576ac
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/uncommon.svg` — 24056 bytes

- Licence: generated
- sha256: eb40139afd527928683075ea1f0116da2f293538f4134b610487ea7f07e576ac
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/uncommon.texts.json` — 1874 bytes

- Licence: generated
- sha256: 76fda2e53df983ebcff6e99eec01acd46b960d7e6e9fef4ddcf879b31a19ea2b
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/units_3.measure.json` — 229 bytes

- Licence: generated
- sha256: f36b3243749fb50705f32b553b3123143ddd0ff681877299d757daca31d83b74
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/white_one_black.pdf` — 5387 bytes

- Licence: generated
- sha256: 6a6ee42d2e8c39796d3598479be236ae54842e8cf5921447f0ed74bbf42d1e2e
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261013-0og-codex-pr36-round4-dxf-emptiness-judged-b/evidence/probe-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/white_one_black.render.svg` — 1449 bytes

- Licence: generated
- sha256: 6f8c7e3cb8b41107c56d6338c4d793e7ae4362b6b4c908f367a2f07750b3394e
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261013-0og-codex-pr36-round4-dxf-emptiness-judged-b/evidence/probe-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/white_one_black.svg` — 1449 bytes

- Licence: generated
- sha256: 6f8c7e3cb8b41107c56d6338c4d793e7ae4362b6b4c908f367a2f07750b3394e
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261013-0og-codex-pr36-round4-dxf-emptiness-judged-b/evidence/probe-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/white_one_black.texts.json` — 103 bytes

- Licence: generated
- sha256: 72578c47513be03f3ae37298ab11cda0a267b51017bc152153bbc5cee2b404e8
- Command: see the generation command of this group above
- Evidence: `.planning/quick/261013-0og-codex-pr36-round4-dxf-emptiness-judged-b/evidence/probe-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/wide.measure.json` — 233 bytes

- Licence: generated
- sha256: 5257419be07be0b173a684edc18bc929b0928db684f5dd74148e5ba048103752
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/wide.render.svg` — 753 bytes

- Licence: generated
- sha256: 06bc9fc8c596e39eea0f998f3c14b2b00443831bd3c45cf884b457e48f27a9be
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/wide.svg` — 753 bytes

- Licence: generated
- sha256: 06bc9fc8c596e39eea0f998f3c14b2b00443831bd3c45cf884b457e48f27a9be
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/wide.texts.json` — 44 bytes

- Licence: generated
- sha256: 1e322aef7477a0b5824a435cf211f0d32102c3e37ead065e2300e33d70f34fd3
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/wide_1280_opaque.png` — 7840 bytes

- Licence: generated
- sha256: 61849ffea6337ffd6c5d9fa297ade7bd03e5c5a80e6541db39ef3084b1aba291
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/wide_1280_transparent.png` — 6637 bytes

- Licence: generated
- sha256: 02fecb0e12146469100447f3963b22462937c7ffd33c7c2dff611c219499945e
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## `dxf-out/wrong_extents.measure.json` — 233 bytes

- Licence: generated
- sha256: 49332bcf90c3915a05ff3358f82976178e3cb59bc28254a93444587b8b38de6d
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt`, the OUT line of this file (same sha256 and bytes)

## Phase 75 line-ending fixtures (`dxf-cr/`)

- Licence: generated by this repository; no third-party material.
- Generation command: `scripts/cad-dxf-cr-fixture.sh [--image TAG]`, run in the engine stage of `Dockerfile.cad-worker` (image id `sha256:f5de09d59a4bde8a1ad9f7ac67b978db4611af9c92d47b4b3e498d7d98f07448`, the 75-05 image). The sha256 and size of every file are the `FILE` lines in `.planning/phases/75-dxf-2d-pipeline/evidence/75-07-cr.txt`; two runs gave identical bytes.
- Purpose: the texts cross-check of the DXF converters compares texts.json with the text the Go XML parser reads from the SVG, and an XML parser turns CR and CRLF into LF. These files prove that the comparison normalizes line endings on both sides and nothing else.

| Files | Source | What they are for |
|---|---|---|
| `cr_carriers.dxf` | ezdxf 1.4.4, R2000, `write_fixed_meta_data_for_testing` | Every way a readable DXF can spell a carriage return in text: caret `^M` in TEXT and MTEXT, the `\U+000D` escape in TEXT and MTEXT |
| `cr_carriers.svg`, `.render.svg`, `.texts.json`, `.verdict` | the production `cad-dxf` on `cr_carriers.dxf` | What the helper emits for them: no CR reaches texts.json (TEXT drops `^M`, MTEXT draws it as U+25AF, `\U+000D` stays literal); a raw CR byte in a value makes the file `unreadable/read_error` |
| `cr_injected.svg`, `.render.svg`, `.texts.json`, `.verdict` | `cr_carriers.svg` with CR, CRLF and CR CR written into three texts by Python's ElementTree, the serializer `cad-dxf` uses; covered and the U+FFFD substitution recomputed from the DejaVu Sans cmap | The SVG carries the raw line endings, texts.json keeps them, the parsed SVG text holds LF |

## `dxf-cr/cr_carriers.dxf` — 14415 bytes

- Licence: generated
- sha256: c0f9f98d1e76721dec8fd1c80687698e4f0e60379108eb8eea455e6c7d3abd9f
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-07-cr.txt`, the FILE line of this file (same sha256 and bytes)

## `dxf-cr/cr_carriers.render.svg` — 1241 bytes

- Licence: generated
- sha256: 2d346792167e00a7b11deae8a0d06dde1a2a898f2bce764b533eb81e28fe09e3
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-07-cr.txt`, the FILE line of this file (same sha256 and bytes)

## `dxf-cr/cr_carriers.svg` — 1241 bytes

- Licence: generated
- sha256: 2d346792167e00a7b11deae8a0d06dde1a2a898f2bce764b533eb81e28fe09e3
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-07-cr.txt`, the FILE line of this file (same sha256 and bytes)

## `dxf-cr/cr_carriers.texts.json` — 223 bytes

- Licence: generated
- sha256: d6802300c3261ef25553f52213f1713579045c0d6cd1917d8cf9953166946b12
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-07-cr.txt`, the FILE line of this file (same sha256 and bytes)

## `dxf-cr/cr_carriers.verdict` — 242 bytes

- Licence: generated
- sha256: 7707651a76fe924476bd00c3ea58e3d48f909784d66c00b707e751888c26c4c6
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-07-cr.txt`, the FILE line of this file (same sha256 and bytes)

## `dxf-cr/cr_injected.render.svg` — 1246 bytes

- Licence: generated
- sha256: e5a7133fe1fcaed6d4e540fa6620b36efc372bdf6f34586b2778130a3c998dcd
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-07-cr.txt`, the FILE line of this file (same sha256 and bytes)

## `dxf-cr/cr_injected.svg` — 1236 bytes

- Licence: generated
- sha256: b6ed63cbc2825d736959868082c0f709e39590fa3a8aa2d917082aca0a50f3ee
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-07-cr.txt`, the FILE line of this file (same sha256 and bytes)

## `dxf-cr/cr_injected.texts.json` — 205 bytes

- Licence: generated
- sha256: 09cd13185ad3e14cedba024af1a7eb182f0a27e3052baca28c3e9e93a9a5cdf1
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-07-cr.txt`, the FILE line of this file (same sha256 and bytes)

## `dxf-cr/cr_injected.verdict` — 269 bytes

- Licence: generated
- sha256: feb09217bf396ee05e2aedefd187eeebe1c46be511fa1526989b7bcb00d30b54
- Command: see the generation command of this group above
- Evidence: `.planning/phases/75-dxf-2d-pipeline/evidence/75-07-cr.txt`, the FILE line of this file (same sha256 and bytes)

---

## 3D render pairs on both backgrounds (`render-bg/`)

- Licence: generated
- Origin: Every file is a PNG drawn by this repository's `cmd/cad-worker/helper/cad-render` from a mesh of a corpus file listed above (`public/LinearSlide-MGN15x-Block.step`, CC-BY-3.0, unmodified geometry; the synthetic `degenerate_plate.step` and `degenerate_rod.step`; the derived `stl-ascii` slice of the fetched `nasa-sls-block1.stl`, PD).
- Added by quick task 261014-0lo. Image `octoconv-cad-worker:latest`, image id `sha256:e2d4105a85da1d8d6743f4820b974b889d4383b7c0011fac4fbf7b599cd96482` (the 261013-0og build; `cmd/cad-worker` and `Dockerfile.cad-worker` are unchanged since). Every run: `--network none --cpus 2 --memory 4g --memory-swap 4g --user nobody`.
- Meshes: `cad-mesh --format step --quality Q --measure M IN OUT` (production argv), or `cad-mesh stats --measure M IN OUT` for the ASCII STL. Frames: `cad-render --view V --size S --background opaque|transparent IN.stl OUT.png`, once per background from the same STL. Harness and per-pair numbers: `.planning/quick/261014-0lo-render-judge-transparent-and-opaque-back/evidence/` (`harness/`, `pairs.txt`).
- What they prove: the 3D pixel judge gives the same verdict on the opaque and the transparent render of one mesh, with the same box, margin and dark count, and the transparent part pixels a subset of the opaque ones (the difference is the FXAA fringe, which `cad-render` writes into alpha-0 pixels).

| Pair (`<name>_opaque.png`, `<name>_transparent.png`) | Mesh | View, size | Verdict on both |
|---|---|---|---|
| `nauo4_iso_1280` | `scripts/cad-corpus-gen.py --scaled-from public/LinearSlide-MGN15x-Block.step --kind step-nauo --targets 4` (540850 bytes, sha256 `12fae7aaf151a5c9777f4ebc97b0d73e5d682dfcc151f63adb48cb42a60b7572`), draft quality, 12624 triangles | iso, 1280 | blob at about 4.3% ink: the sparse assembly the transparent route refused before (M2 of 76-02 at n=4) |
| `linearslide_iso_256` | `public/LinearSlide-MGN15x-Block.step`, standard, 5948 triangles | iso, 256 | blob, a dense part |
| `w15_iso_256` | `stl-ascii-nasa-sls-block1-1MiB.stl` (sha256 `77492adbca35ff7f0c583732208324a834170aec9cb76eee7ad4e0b853c18ef0`), stats path, 4396 triangles | iso, 256 | thin, the 71 W15 slice |
| `degenerate_plate_front_1280` | `synthetic/degenerate_plate.step`, standard, 12 triangles | front, 1280 | thin, a plate seen edge on |
| `degenerate_rod_iso_256` | `synthetic/degenerate_rod.step`, standard, 140 triangles | iso, 256 | refused by J4 on both |

## `render-bg/degenerate_plate_front_1280_opaque.png` — 7741 bytes

- Licence: generated
- sha256: 4960d07bd66c01ae26741adeb846be5adc8db1097ebfe4df800deadd7fa1a507
- Command: see the pair table of this group above
- Evidence: no sha256 was recorded when the file was committed in 056a8d2 (quick 261014-0lo); this sha256 is that of the committed file, computed by quick 261018-44q; the file has not changed since 056a8d2.

## `render-bg/degenerate_plate_front_1280_transparent.png` — 9503 bytes

- Licence: generated
- sha256: aecae1feb6e1d92060f04bbebbcee4a5ea60926752b531c4eefd053cc0443509
- Command: see the pair table of this group above
- Evidence: no sha256 was recorded when the file was committed in 056a8d2 (quick 261014-0lo); this sha256 is that of the committed file, computed by quick 261018-44q; the file has not changed since 056a8d2.

## `render-bg/degenerate_rod_iso_256_opaque.png` — 970 bytes

- Licence: generated
- sha256: d2dd961f9b2444cbf670f7ebc677bae9bd940262f911469fd4598e8b2449486f
- Command: see the pair table of this group above
- Evidence: no sha256 was recorded when the file was committed in 056a8d2 (quick 261014-0lo); this sha256 is that of the committed file, computed by quick 261018-44q; the file has not changed since 056a8d2.

## `render-bg/degenerate_rod_iso_256_transparent.png` — 1054 bytes

- Licence: generated
- sha256: d6930aa3e94ddccfec335810666e5cbc1f7d77228a0af1578fddb17630cfc724
- Command: see the pair table of this group above
- Evidence: no sha256 was recorded when the file was committed in 056a8d2 (quick 261014-0lo); this sha256 is that of the committed file, computed by quick 261018-44q; the file has not changed since 056a8d2.

## `render-bg/linearslide_iso_256_opaque.png` — 8764 bytes

- Licence: generated
- sha256: 1761e0f82edc714e5431f45574c90ec3b18142ed15aed07bc0a72c48e54cb173
- Command: see the pair table of this group above
- Evidence: no sha256 was recorded when the file was committed in 056a8d2 (quick 261014-0lo); this sha256 is that of the committed file, computed by quick 261018-44q; the file has not changed since 056a8d2.

## `render-bg/linearslide_iso_256_transparent.png` — 9312 bytes

- Licence: generated
- sha256: e55f8e721566f76c493115250912bf332791e3a3bdf804f2e6a1b7e6bf06cadc
- Command: see the pair table of this group above
- Evidence: no sha256 was recorded when the file was committed in 056a8d2 (quick 261014-0lo); this sha256 is that of the committed file, computed by quick 261018-44q; the file has not changed since 056a8d2.

## `render-bg/nauo4_iso_1280_opaque.png` — 47041 bytes

- Licence: generated
- sha256: 470382d75a317c7e72c7ce280dfb61b3d4516dce6412136fa9168cf3df6f4043
- Command: see the pair table of this group above
- Evidence: no sha256 was recorded when the file was committed in 056a8d2 (quick 261014-0lo); this sha256 is that of the committed file, computed by quick 261018-44q; the file has not changed since 056a8d2.

## `render-bg/nauo4_iso_1280_transparent.png` — 53741 bytes

- Licence: generated
- sha256: a7c7fc0a0bfe867a1122c5a464b8e3dc581bf392fe095d773e8b6278ffdd55b3
- Command: see the pair table of this group above
- Evidence: no sha256 was recorded when the file was committed in 056a8d2 (quick 261014-0lo); this sha256 is that of the committed file, computed by quick 261018-44q; the file has not changed since 056a8d2.

## `render-bg/w15_iso_256_opaque.png` — 1628 bytes

- Licence: generated
- sha256: 38d96d1902f2448f887db865292754ecf8feccf71f2ab6e6c54f3c9bf6746670
- Command: see the pair table of this group above
- Evidence: no sha256 was recorded when the file was committed in 056a8d2 (quick 261014-0lo); this sha256 is that of the committed file, computed by quick 261018-44q; the file has not changed since 056a8d2.

## `render-bg/w15_iso_256_transparent.png` — 1789 bytes

- Licence: generated
- sha256: 5bf66e2cb05e326b4324ee9f568d5e1f77cd258dae4f081ecd71e94db895b265
- Command: see the pair table of this group above
- Evidence: no sha256 was recorded when the file was committed in 056a8d2 (quick 261014-0lo); this sha256 is that of the committed file, computed by quick 261018-44q; the file has not changed since 056a8d2.

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

## Derived (not committed)

Deterministic scalings of real files by `scripts/cad-corpus-gen.py --scaled-from FILE --kind K --targets LIST --scaled-dir DIR`, written to `$OCTOCONV_CAD_CACHE/scaled`, never committed, and honestly synthetic: they exist because no open STEP or STL above about 52 MB was found and no open ASCII STL above 2 MiB (D-05). Two runs produce byte-identical files (the sha256 values below were identical on the repeated runs). The licence of a derived file is the licence of its base.

Every run prints one line per rung, `SCALED,kind=..,target=..,bytes=..,sha=..,check=ok`, and exits non-zero if a check fails. Checks: **step-copies** entity count equals k x base, maximum id equals k x base maximum, header and trailer unchanged, and on the smallest rung of each series OCP reads k x the base solids and k x the base volume (relative 1e-6); **step-nauo** the number of NEXT_ASSEMBLY_USAGE_OCCURRENCE in the text equals N; **stl-tile** size equals 84 + 50 x triangles, the count field agrees and the first tile equals the base; **stl-ascii** the facet count read back equals the facets written and the size is within 400 bytes of the target, first facet checked; **dxf-replicate** entity counts per type equal K x base, handles are unique and ezdxf reads the smallest rung with K x the base modelspace length.

Generator command (shared prefix, the measurement image of the time; the image tag changes whenever `scripts/cad-measure` changes):

```
docker run --rm --network none --cpus 2 --memory 6g --memory-swap 6g -v "$PWD:/src:ro" -v "$OCTOCONV_CAD_CACHE/scaled:/scaled" -v "$OCTOCONV_CAD_CACHE/fetched:/fetched:ro" -w /src octoconv-cad-measure:ocp7.9.3.1.1-full-16d52a0d41e3 /opt/venv/bin/python -I -B scripts/cad-corpus-gen.py --scaled-from BASE --kind KIND --targets LIST --scaled-dir /scaled
```

### step-copies from LinearSlide-MGN15x-Block.step

- Base: `public/LinearSlide-MGN15x-Block.step` (committed)
- Base licence: CC-BY-3.0, attribution to k2-mirror and FreeCAD-library is inherited
- What: Text replication of the DATA section with every `#n` reference renumbered by `i x max id`, header and trailer kept. A small base so that the ladder starts at 1 MiB and the geometry per MiB is a simple single part (5948 triangles per copy).
- Command: the shared prefix above with `--scaled-from internal/convert/testdata/cad/public/LinearSlide-MGN15x-Block.step --kind step-copies --targets 1,2,5,10,20,40,80,150,200`

| Target | Coefficient | Bytes | sha256 |
|---|---|---|---|
| 1MiB | k=2 | 1094537 | 9bea6097ff46f036d3a8c84044e9b7f5166c5d3e07570831be9df13c4853b355 |
| 2MiB | k=4 | 2212957 | 909da73eea4b1cff734bbcf7f52cbddd8d78bf7d2de840b1b6ab82c15371bee4 |
| 5MiB | k=9 | 5011396 | d7568636bb7c72477a6811f479561f13794a0eb023f07ae6ed690aaa5e8eb3b5 |
| 10MiB | k=18 | 10262824 | 3a71f9da1fd0bae74f537209d66b36125c9fdf958abfdcb3ddc2939131b84d5d |
| 20MiB | k=36 | 20765680 | 6530ac5cd90c7dde4b70907fb90a565c34d0485c68b92ae90f0de33c5ba5f63e |
| 40MiB | k=72 | 41771392 | d974832f3ab7579afc53ee41879b51e2b1b63cf61913c4698128728ac3f0be07 |
| 80MiB | k=142 | 83903158 | 18e415e040bef94f4de0ac8e3c93d2d15f835bd58cdac92db6d2e98a2e586dd4 |
| 150MiB | k=263 | 157443812 | 06c54759d5dc2f4bb38564f3677a28bdd5e2be3a44ec682fb83873adf21a4eff |
| 200MiB | k=349 | 209712376 | ec6721113e6376b7738b8653241debec8d178bd2f575151d51419c672dcdf611 |

### step-copies from freecad-mit-e-vent-t-slot.step

- Base: `freecad-mit-e-vent-t-slot.step` (fetched; 9044371 bytes, the largest file of the Mechanical, Medical and Generic categories in the manifest; a SolidWorks 2019 assembly with 102 NAUO, 22 products, 25 solids)
- Base licence: CC-BY-3.0, attribution to berserkdesign (xolotl-hub) and FreeCAD-library is inherited
- What: The same replication on a heavy real base, so the RSS multiplier per MiB comes from real assembly geometry (each copy also carries its 102 NAUO). Targets below two copies of the base (1, 2, 5, 10 MiB) are not generated: the real files of that size are in the manifest.
- Command: the shared prefix above with `--scaled-from /fetched/freecad-mit-e-vent-t-slot.step --kind step-copies --targets 20,40,80,150,200`

| Target | Coefficient | Bytes | sha256 |
|---|---|---|---|
| 20MiB | k=2 | 18279614 | 1b026cafc43d4a05bc413489be08c6c1a0b47d2da6ae0c7bc7cece7637972f7c |
| 40MiB | k=5 | 46055501 | 1a3c924ee500e4e1ec5bf8c6568cc26f228d9e4b2a79be869f5b9f2d02961841 |
| 80MiB | k=9 | 83090017 | e5d6dbbe5bc0658eda63753da20a16d1e3b7da911dd9d95034c57119b3cde06d |
| 150MiB | k=17 | 158260464 | 90f70871e205896bbb792663029ff8395c59de35282e70faabe0f84bfdafe116 |
| 200MiB | k=22 | 205507679 | 06f270b622d4fd2db908978b026b38173dd0b5ff825bf034560e0e354d674ca2 |

### step-nauo from LinearSlide-MGN15x-Block.step

- Base: `public/LinearSlide-MGN15x-Block.step` (committed)
- Base licence: CC-BY-3.0, attribution to k2-mirror and FreeCAD-library is inherited
- What: One prototype read with STEPControl_Reader and placed N times through XCAF (STEPCAFControl_Writer) on a cubic grid with a 200 mm pitch. The geometry is written once, the file stays small while the instance count grows: the bomb vector of Pitfall 8, kept apart from the copies series.
- Command: the shared prefix above with `--scaled-from internal/convert/testdata/cad/public/LinearSlide-MGN15x-Block.step --kind step-nauo --targets 10,100,1000,2500,5000,10000,100000` (2500 and 5000 were added in 71-06 to bracket the memory break between 1000 and 10000 instances)

| Target | Coefficient | Bytes | sha256 |
|---|---|---|---|
| n10 | instances=10 | 544463 | 2944fbbb904fdca56eba23be3c067c35a2f7a0a8f49c02ebaeb717c10f42a6b7 |
| n100 | instances=100 | 599248 | fe4eea9efa59c93cde84d547c6b6f19a9976c6c3e62a9b08be30c14d57aac4b6 |
| n1000 | instances=1000 | 1154281 | fc05817ffcc9eac684af650138fd4c0feaa2d0b8d5e59a1306131ac7b6b78ffd |
| n2500 | instances=2500 | 2087270 | c7a5e41ddc15b9ecf964eb28204ed5284a02b14ab7248ff7a7b9a7f787f05ebe |
| n5000 | instances=5000 | 3642601 | e97dd239cf51440c467225f39dc3eda7ad948e579ac297e90b74b38323400b5a |
| n10000 | instances=10000 | 6757254 | 297e9ba579e472f91a9790b03363ac22ffc7b7cae2c85cd8dfe20419d13c0158 |
| n100000 | instances=100000 | 64740150 | 97ee6e9c88708d1189725a3dc619b2acf4b4ccc4a09383fa9eddc7ac550e647c |

### stl-tile from nasa-sls-block1.stl

- Base: `nasa-sls-block1.stl` (fetched; 13721684 bytes, 274432 triangles)
- Base licence: PD (NASA)
- What: Binary tiles on a grid with a pitch of 1.05 bounding-box extents; size is exactly 84 + 50 x triangles. Targets of 100 and 200 MiB only, below that the real NASA files are in the manifest.
- Command: the shared prefix above with `--scaled-from /fetched/nasa-sls-block1.stl --kind stl-tile --targets 100,200`

| Target | Coefficient | Bytes | sha256 |
|---|---|---|---|
| 100MiB | tiles=8 (2195456 triangles) | 109772884 | bd4a03ed375f191d3f3af81ae852093c8862585aaa8a0f9eca7ba594b728c788 |
| 200MiB | tiles=15 (4116480 triangles) | 205824084 | 2045f91fb675dc99f4dd6dee717bea623afc84bab709726576e4662491454b6c |

### stl-ascii from nasa-sls-block1.stl

- Base: `nasa-sls-block1.stl` (fetched)
- Base licence: PD (NASA)
- What: The first N triangles of the same tiling sequence as the binary ladder, written as ASCII with `%.6e`; the size lands within one facet (at most 400 bytes) below the target. This is the only source of ASCII STL above 2 MiB.
- Command: the shared prefix above with `--scaled-from /fetched/nasa-sls-block1.stl --kind stl-ascii --targets 1,5,10,25,50,100,200`

| Target | Coefficient | Bytes | sha256 |
|---|---|---|---|
| 1MiB | 4396 facets | 1048506 | 77492adbca35ff7f0c583732208324a834170aec9cb76eee7ad4e0b853c18ef0 |
| 5MiB | 21988 facets | 5242783 | f9fb45d9fe49096181cd0fd60152ed8f5aa88173f03ecacef9c27eb86d704184 |
| 10MiB | 43836 facets | 10485616 | ef31bbfafbb92b6f37933497813fc971b041617288913fd0080f3e063a9360d9 |
| 25MiB | 109544 facets | 26214361 | 8e58ee5b3de24a523d3341eb02d382d40bfeb347cb070e78d2aeeca3ff7ea169 |
| 50MiB | 219538 facets | 52428718 | 71295dc849c0bd6dda68c59dfb95d8e906513a33c298607352c4f50c4b3546c5 |
| 100MiB | 440762 facets | 104857386 | a137dabd88d19f138259630b5d517d5bddb0ead66e0a5b9d2619ca6fbe7f5bb5 |
| 200MiB | 883037 facets | 209715040 | d42f47a7e934a6dff57601307035a372e141936b1f41f8d7f189149b3b7f3de7 |

### dxf-replicate from uncommon.dxf

- Base: `public/uncommon.dxf` (committed)
- Base licence: MIT, Copyright (c) 2020 Manfred Moitzi (notice in this document)
- What: The ENTITIES section is repeated N times with x shifted by 1000 x copy index on group codes 10 to 16, handles (group 5) renumbered from $HANDSEED + 1, $HANDSEED updated, and `$TD*` and GUID header variables set to constants. Blocks, tables and objects are not repeated, so every copy of an INSERT or DIMENSION points at the same block.
- Command: the shared prefix above with `--scaled-from internal/convert/testdata/cad/public/uncommon.dxf --kind dxf-replicate --targets 2,5,10,20,50,100,200` (the 100 and 200 MiB rungs were added in 71-06 so that a ladder reaches the 100 MiB bar of D-05)

| Target | Coefficient | Bytes | sha256 |
|---|---|---|---|
| 2MiB | copies=24 (2568 entities) | 2050076 | 48cacc1f3f4d09c9546ec0d7977d7c5e73af3eef053557ea5d2a9a0221d919d8 |
| 5MiB | copies=66 (7062 entities) | 5171642 | 6db807ae96e58e71922ba43497b37ad290b84582b37deda03171e005fc195b7b |
| 10MiB | copies=136 (14552 entities) | 10376517 | 76764005e785ac2a736b61284fb91c3fa6b35378ccc2f96272c998f64738ad09 |
| 20MiB | copies=275 (29425 entities) | 20716171 | cda9dd00f6462a6c95db3108af78a98437695c6b185a59a1f857c93388ec1c6d |
| 50MiB | copies=692 (74044 entities) | 51745292 | 5d14734c8907e813031cfd99ce7242164ee8ae525d4bfd2974a742967489f652 |
| 100MiB | copies=1387 (148409 entities) | 103540757 | 23a0f58a7636cab8b3945aaae5d7a6d9626226dfa4c6c430875f415983dfc3a4 |
| 200MiB | copies=2778 (297246 entities) | 207242589 | 59d3f5f76542910dd335f0840ecdaeb064ec6fafb11fe4cb6ab9f6af43fb1882 |

### dxf-minsert (synthetic, no base content)

- Base: `public/uncommon.dxf` (committed) supplies only the file-name stem; the content is generated
- Licence: generated (no third-party content)
- What: One MINSERT of a five-entity block (circle, two lines, a closed polyline, one text) on a grid of `columns x rows` cells with a 12-unit pitch, written by ezdxf R2018 with fixed metadata. The file stays near 16 KB while the number of expanded inserts grows: the expansion vector of D-10 that no real corpus file has (none of the public DXF files has a MINSERT). Added in 71-06.
- Command: the shared prefix above with `-e PYTHONHASHSEED=0`, `python -B` instead of `python -I -B` (ezdxf writes OBJECTS in set order, so the bytes depend on the hash seed; the generator exits when the seed is not 0) and `--scaled-from internal/convert/testdata/cad/public/uncommon.dxf --kind dxf-minsert --targets 100,1000,10000,100000,1000000,10000000`

| Target | Coefficient | Bytes | sha256 |
|---|---|---|---|
| n100 | cells=100 (10 x 10) | 16269 | d08db976478b21224a30ad44bd2da376402cbf3bedb44e1afc4867b0dc8afc6c |
| n1000 | cells=1000 (25 x 40) | 16269 | 29a84ae6fecdd0cf70d68a8380b519766c162792a0110ef96fae67ada61ea6f1 |
| n10000 | cells=10000 (100 x 100) | 16271 | 4de9da67164d23919be1c7aa4bde9bf91462a77208611572b11a8badf3424808 |
| n100000 | cells=100000 (250 x 400) | 16271 | 5c6d7b4e3189129adede259242b1375805760034ef26bf9255cec682f94d7e6b |
| n1000000 | cells=1000000 (1000 x 1000) | 16273 | b4eb2b8cd7bcea941ea214489f307d12e1c9b654fc06f65a9d333ad32dbd5400 |
| n10000000 | cells=10000000 (3125 x 3200) | 16273 | 936e05beeeb408cb9252a6e34e9e63b6aaba3069b570e5b24aebff039f5a411f |

### iges-from-step from the step-copies-freecad-mit-e-vent-t-slot series

- Base: `step-copies-freecad-mit-e-vent-t-slot-{20,40,80,150}MiB.step` (derived, see above), whose base is `freecad-mit-e-vent-t-slot.step` (fetched)
- Base licence: inherited from the STEP base (see its entry under Fetched)
- What: The STEP file read with STEPControl_Reader and written by IGESControl_Writer in millimetres (faces and surfaces, the writer default, as the real IGES files of the corpus are surface-and-curve files); the two generation timestamps in the global section are replaced by `15H20000101.000000` so that two runs agree byte for byte (checked on a small file). IGES is about six times the size of its STEP source. Added in 71-06; the generator has no other IGES scaling mode.
- Command: the shared prefix above with `-e PYTHONHASHSEED=0` and `--scaled-from /scaled/step-copies-freecad-mit-e-vent-t-slot-NNMiB.step --kind iges-from-step --targets 1`

| Source rung | Bytes | sha256 |
|---|---|---|
| 20MiB | 114920208 | 1d7262cb13b81da4e871570e23a710cb747e006799a3877a8d06db31d11be720 |
| 40MiB | 287323362 | 6e68f397a560816d791241a6f7a169930cdf2526b5b28fecb2c9be2c8050b2d7 |
| 80MiB | 517266243 | 25f6edbba4008781b835cf68089dcadcbe63a0f8db8a3ffd6b66627fc0f38338 |
| 150MiB | 977152005 | e2829781eb6cdafbfebfc19b941530b09acbccdb266f5a8056158a0b8725e5ea |

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
