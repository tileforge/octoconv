# CAD corpus — provenance

These files are **inputs** to the tests of phases 71-75, never their output. Each one exists to make
one specific claim about the CAD helpers observable rather than argued. Replacing a file means
replacing its section below.

Licences allowed for committed files (phase 71, D-04): PD, CC0-1.0, CC-BY-3.0, CC-BY-4.0, MIT,
BSD-2-Clause, BSD-3-Clause, and `generated` for the synthetic output of `scripts/cad-corpus-gen.py`.
CC-BY-SA and NC are not taken. Large files are not committed; their pins go to
`scripts/cad-corpus-manifest.tsv` (plan 71-03).

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
