package convert

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
)

// documentFormats are the office document formats the document capability admits.
var documentFormats = []string{"docx", "odt", "xlsx", "ods", "pptx", "odp"}

// crossPairs is the explicit list of the six intra-family pairs. It is not a
// cross-product of documentFormats, which would also admit docx to ods.
var crossPairs = []Pair{
	{From: "docx", To: "odt"},
	{From: "odt", To: "docx"},
	{From: "xlsx", To: "ods"},
	{From: "ods", To: "xlsx"},
	{From: "pptx", To: "odp"},
	{From: "odp", To: "pptx"},
}

// LibreOfficeConverter admits the twelve document pairs. Conversion runs in
// tileforge/octoconv-worker-document. Convert fails closed so this process
// cannot shell out to soffice.
type LibreOfficeConverter struct{}

// Pairs returns one pdf target per office format, then the six cross pairs.
func (LibreOfficeConverter) Pairs() []Pair {
	pairs := make([]Pair, 0, len(documentFormats)+len(crossPairs))
	for _, f := range documentFormats {
		pairs = append(pairs, Pair{From: f, To: "pdf"})
	}
	pairs = append(pairs, crossPairs...)
	return pairs
}

// Engine returns the document engine class.
func (LibreOfficeConverter) Engine() string { return EngineDocument }

// Convert refuses the call. The in-repo LibreOffice process has been removed.
func (LibreOfficeConverter) Convert(context.Context, string, string, map[string]any) error {
	return errors.New("libreoffice: the in-repo engine is retired")
}

// pdfMagic is the leading byte sequence every valid PDF file begins with.
var pdfMagic = []byte("%PDF-")

// validatePDF guards against an engine that exits 0 with an empty or corrupt
// PDF. Chromium reuses it. The output must be non-empty and begin with %PDF-.
func validatePDF(path string) error {
	fi, err := statJobOutput(path)
	if err != nil {
		return fmt.Errorf("libreoffice: stat output: %w", err)
	}
	if fi.Size() == 0 {
		return fmt.Errorf("libreoffice: output is empty")
	}
	// A file shorter than the magic itself can never be a valid PDF; classify
	// it as the terminal missing-magic case up front so the ReadFull below
	// cannot surface it as a transient-looking "unexpected EOF" (D-04).
	if fi.Size() < int64(len(pdfMagic)) {
		return fmt.Errorf("libreoffice: output missing %%PDF- magic bytes")
	}
	f, _, err := OpenJobFile(path)
	if err != nil {
		return jobOutputOpenFailure("libreoffice", err)
	}
	defer f.Close()
	buf := make([]byte, len(pdfMagic))
	if _, err := io.ReadFull(f, buf); err != nil {
		return fmt.Errorf("libreoffice: read output header: %w", err)
	}
	if !bytes.Equal(buf, pdfMagic) {
		return fmt.Errorf("libreoffice: output missing %%PDF- magic bytes")
	}
	return nil
}
