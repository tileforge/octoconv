// Package convert (this file): ocr.go implements OCRConverter, the OCR-01/03
// converter core (43-CONTEXT.md D-01/D-02/D-04): Tesseract-backed text/hOCR/
// ALTO extraction from images, mirroring AudioConverter's (whisper.go)/
// AVConverter's (av.go) standalone-converter shape -- Pairs()/Engine()/
// Convert(), a guard stage BEFORE the expensive subprocess, argv builders
// factored into their own pure functions. Built and unit-tested directly
// against the real tesseract binary this phase, but deliberately NOT
// wired into the process-wide converter registry (mirrors Phase 30/34's
// own fence) -- registration is Phase 44's job. The streaming per-page PDF
// source path (pdftoppm render -> OCR -> delete, D-01/SC2) is Plan 04's
// addition to this same Convert() dispatch; this plan builds and tests the
// IMAGE path only.
package convert

import (
	"context"
	"errors"
)

// ocrSourceFormats is the closed set of source formats OCRConverter's
// Pairs() advertises: the four images this project already sniffs
// (jpg/png/tiff/webp) plus pdf (scanned/image-only PDF, born-digital
// rejected by ocrguards.go's detectBornDigitalPDF -- Plan 04's job to call
// it). ocrTargetFormats is the D-01 single-target output set -- one output
// per job, mirroring AudioConverter's {txt,srt,vtt,json} shape.
var (
	ocrSourceFormats = []string{"jpg", "png", "tiff", "webp", "pdf"}
	ocrTargetFormats = []string{"txt", "hocr", "alto"}
)

// OCRConverter extracts text (and, per D-01, hOCR/ALTO word/line bounding
// boxes) from images and scanned/image-only PDFs by shelling out to
// `tesseract` -- the sixth engine class (EngineOCR), mirroring
// AudioConverter's (whisper.go) and AVConverter's (av.go) shape: a plain
// struct, Pairs()/Engine()/Convert(), argv builders isolated as their own
// pure functions for unit-testability. It is built and unit-tested this
// phase directly against a real tesseract/poppler binary but deliberately
// NOT wired into the process-wide converter registry (Phase 43 scope
// fence, mirrors Phase 30's/34's own fence) -- registration is Phase 44's
// responsibility.
//
// MaxImagePixels and MaxPages let an operator-configured instance override
// the guard ceilings (ocrguards.go, Plan 02) without editing package
// consts -- a zero value on either field falls back to "no cap configured
// this phase" (enforceImagePixelBound's/enforceMaxPages's own zero-value
// convention), so a bare OCRConverter{} is well-defined, mirroring
// AVConverter's MaxSourceDuration/MaxSourceResolutionHeight
// zero-value-defaulting contract. MaxPages is unused by this plan's IMAGE
// path -- it exists for Plan 04's PDF page-count guard call.
type OCRConverter struct {
	MaxImagePixels uint64
	MaxPages       int
}

// Pairs returns every {source, target} combination of ocrSourceFormats x
// ocrTargetFormats (D-01: single-target output, 15 pairs = 5 sources x 3
// targets) -- source and target sets are disjoint, so no identity-pair
// filter is needed (mirrors AudioConverter.Pairs'/AVConverter.Pairs' shape).
func (OCRConverter) Pairs() []Pair {
	pairs := make([]Pair, 0, len(ocrSourceFormats)*len(ocrTargetFormats))
	for _, from := range ocrSourceFormats {
		for _, to := range ocrTargetFormats {
			pairs = append(pairs, Pair{From: from, To: to})
		}
	}
	return pairs
}

// Engine reports the OCR engine class (D-01).
func (OCRConverter) Engine() string { return EngineOCR }

// Convert is a placeholder satisfying the Converter interface for this
// task's Pairs()/Engine() commit -- Task 2 replaces this body with the
// real tesseract invocation (PSM 3 / OEM 1, D-04).
func (c OCRConverter) Convert(ctx context.Context, inPath, outPath string, opts map[string]any) error {
	return errors.New("ocr: Convert not yet implemented")
}

// var _ Converter ensures OCRConverter's concrete value type satisfies the
// Converter interface at compile time -- mirrors AVConverter{}'s own
// assertion (av.go), pinning a value receiver across every method here.
var _ Converter = OCRConverter{}
