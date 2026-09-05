package api

import (
	"mime"
	"mime/multipart"
)

// rawPartFilename returns the filename the CLIENT sent, not the one the
// standard library kept.
//
// Go's mime/multipart applies filepath.Base while parsing, so
// "chapters/preamble.tex" reaches FileHeader.Filename as "preamble.tex". That
// silent repair is why an accepted request could still fail in the worker: a
// document reading \input{chapters/preamble} was queued, and the attachment was
// materialised at the directory root under a different name than the document
// asks for (PR #20 review).
//
// This package's rule is that names are refused, never repaired -- so the rule
// has to see what was sent. The raw value survives in the part's own
// Content-Disposition header, which the parser does not rewrite.
//
// Falls back to the parsed name when the header is absent or unparseable:
// refusing a request because its own headers cannot be re-read would be
// answering a parser question with a client error.
func rawPartFilename(fh *multipart.FileHeader) string {
	if cd := fh.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if name := params["filename"]; name != "" {
				return name
			}
		}
	}
	return fh.Filename
}
