package convert

import (
	"bytes"
	"io"
)

// TestCreateSniffInventoryCoversEveryCall walks handleCreateJob, so the direct
// path keeps its own calls and this function repeats them.
func SniffDeclared(r io.ReaderAt, size int64, declared string) (string, error) {
	declared = NormalizeFormat(declared)
	section := io.NewSectionReader(r, 0, size)
	detected, rest, err := Sniff(section)
	if err != nil {
		return "", err
	}
	if detected == "" {
		var prefix [4]byte
		if n, _ := r.ReadAt(prefix[:], 0); n == 4 && bytes.Equal(prefix[:], []byte{'P', 'K', 3, 4}) {
			cr, cerr := SniffContainer(r, size)
			if cerr == nil && cr.Format != "" && !cr.DuplicateRootPart {
				detected = cr.Format
			}
		}
	}
	if detected == "" && declared == "html" && LooksLikeHTML(r, size) {
		detected = "html"
	}
	if detected == "" && declared == "latex" {
		window := make([]byte, SniffWindowBytes)
		if size >= 0 && size < int64(len(window)) {
			window = window[:size]
		}
		n, readErr := r.ReadAt(window, 0)
		if readErr == nil || readErr == io.EOF {
			if prefix, ok := ValidateSniffPrefix(window[:n], size); ok && LooksLikeLaTeXFromPrefix(prefix) {
				detected = "latex"
			}
		}
	}
	if detected == "" && (declared == "md" || declared == "rst" || declared == "org") && LooksLikeText(r, size) {
		detected = declared
	}
	if detected == "" && IsCADDeclared(declared) {
		if head, tail, cerr := ReadCADWindows(r, size); cerr == nil {
			res := SniffCADWindows(head, tail, size, declared)
			if res.Rejection == "" {
				detected = res.Format
			}
		}
	}
	if detected == "" && IsOLECFB(r) {
		_ = ClassifyCFB(r, size)
		return "", nil
	}
	if detected == "" {
		detected, rest, err = SniffVideo(rest)
		if err != nil {
			return "", err
		}
	}
	if detected == "" {
		detected, _, err = SniffAudio(rest)
		if err != nil {
			return "", err
		}
	}
	return detected, nil
}
