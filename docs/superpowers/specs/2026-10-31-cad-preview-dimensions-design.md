# CAD preview dimensions

Render STEP, IGES and STL previews with three overall dimensions and a `Size: X × Y × Z unit`
heading, following the user's reference. The user's “yes to both” is interpreted as an explicit
`dimensions` boolean option with a default of `true`; `false` preserves the existing plain PNG.
This intentionally changes the appearance of requests that omit the option. Existing request
shapes and the measurements JSON schema remain valid. DXF and conversion operations reject the option.

Use the existing measurements document for values, axis order and units. Never assume millimetres
for an STL: show `units unspecified` unless `assume_unit` supplies a unit, and identify assumed units.
These are axis-aligned bounding-box dimensions, not hole diameters, feature dimensions or tolerances.
Orthographic views show dimension lines only for visible axes; all three lengths remain in the heading.

## Approaches

The user accepted both an explicit option and enabling dimensions by default. Implementation choices:

- Draw annotations inside VTK: requires changing its sandboxed rendering path and separating annotations
  from the existing pixel judge.
- Compose annotations in Go after the existing pixel judge: reuse the validated measurements and existing
  `golang.org/x/image` dependency; leave the CAD helper and its container dependencies unchanged. Chosen.

Fit the validated model PNG into an inset square, project its measured mesh bounds with the same camera
as VTK, and draw extension lines, arrows and labels outside the projected box. Preserve requested size and
opaque/transparent background. Suppress collapsed dimension lines. Keep labels inside the frame at all
supported sizes. An annotation must never make an empty model pass the pixel judge.

Verify strict option parsing, API persistence of false, applicability, values and unit provenance,
camera projection, extreme aspect ratios and tiny dimensions, transparent borders, rejection of blank
images, and byte preservation with dimensions disabled. Inspect real CAD renders visually.
