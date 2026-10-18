# Phase 75 Discussion Log

**Date:** 2026-10-07
**Mode:** `--auto`. In every area the orchestrator picked the recommended option, under the user's v1.16 delegation. No AskUserQuestion was shown.

[auto] Selected all gray areas: Pipeline and stages, Text and output safety, Render dxf→png, Measurements, Bombs and failure classes, Confinement.

- [auto] Pipeline: Q "Where does the DXF bomb pre-pass live?" → "A DXF mode in the existing stdlib cad-guard" (recommended; reuses its measured ruleset)
- [auto] Pipeline: Q "Strict ezdxf read or recover?" → "Strict; recover only with an auditor gate that fails closed" (recommended)
- [auto] Pipeline: Q "XREF/IMAGE/UNDERLAY handling?" → "Never resolved; skipped explicitly and counted; Landlock as the second line" (recommended)
- [auto] Text: Q "Text as outlines (ezdxf default) or real <text>?" → "Real <text>, escaped by an XML serializer" (recommended). Outlines would break SC1 'text not lost' and SC2 'text extractable'.
- [auto] Safety: Q "Trust the ezdxf SVG or validate?" → "Validate in Go with a closed element/attribute allowlist before upload or rsvg" (recommended)
- [auto] PDF: Q "Page size?" → "Content-fit page" (recommended by the phase-71 research)
- [auto] Render: Q "view/quality on dxf?" → "422 opts_not_applicable" (recommended)
- [auto] Render: Q "Non-square framing?" → "Letterbox on the background, fitted with a margin" (recommended by the phase-71 research)
- [auto] Render: Q "Pixel judge?" → "A new drawing mode, calibrated against the 71 pixelcheck by the differential method" (recommended)
- [auto] Measurements: Q "bbox source?" → "Drawn entity set via ezdxf bbox, never $EXTMIN/$EXTMAX" (roadmap SC3)
- [auto] Bombs: Q "Codes?" → "cad_too_complex for caps and cycles, cad_unreadable for non-numeric values; both terminal" (recommended; existing codes)
- [auto] Confinement: Q "Rulesets?" → "Re-measure on the production image by the 74 D-09 method; the fonts negative control must show the text drop" (recommended)

Deferred: layout/page options, SHX fidelity, 3DSOLID rendering, and timeout/KEDA (phase 76).
