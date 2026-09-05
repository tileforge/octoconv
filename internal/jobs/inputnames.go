package jobs

import (
	"errors"
	"path"
	"strings"
	"unicode"
)

// Filenames a client sends become FILES ON DISK in the worker, next to the
// document the engine compiles. That is the whole point of multi-file jobs --
// \input{preamble} and \addbibresource{refs.bib} resolve by name -- and it is
// also the reason the names cannot be taken as given.
//
// The engine is already known to read what it is pointed at: PR #19 measured a
// .tex reading /etc/hostname by absolute path under --untrusted. A filename is
// the other direction of the same problem -- if a client can choose where their
// bytes LAND, they can choose what the engine reads, and a traversal writes
// outside the job directory.
//
// So this refuses rather than repairs. A name that needs fixing is a name the
// client can send correctly the first time, and silently rewriting it would
// make the file the engine reads different from the one the document names.

// ErrUnsafeInputFilename is returned for a name that cannot be used as a plain
// file inside the job's own directory.
var ErrUnsafeInputFilename = errors.New("unsafe input filename")

// maxInputFilenameLen bounds the name itself. 255 is the common single-component
// limit on ext4/APFS/overlayfs; a longer name fails at creation time in the
// worker, which is a job failure for something the API can see.
const maxInputFilenameLen = 255

// SafeInputFilename returns name if it can be used verbatim as a file in the
// job's directory, and an error otherwise.
//
// It is deliberately stricter than "no slashes". The rules, each with the thing
// it refuses:
//
//	empty / "." / ".."        a name that is not a file
//	any / or \                traversal, and Windows-style separators that a
//	                          later path join could interpret
//	a leading -               a name that a command line reads as a flag
//	control characters        names that break logs, and NUL, which truncates a
//	                          path at the syscall boundary
//	leading or trailing space or dot   names that some filesystems normalise away,
//	                          so two different uploads become one file
func SafeInputFilename(name string) (string, error) {
	if name == "" {
		return "", ErrUnsafeInputFilename
	}
	// WHAT THIS SEES, and why the caller has to work for it. Go's
	// mime/multipart applies filepath.Base while parsing, so
	// FileHeader.Filename is already repaired: "chapters/preamble.tex" arrives
	// as "preamble.tex".
	//
	// Feeding this function that value made the promise below false in the way
	// that matters -- a request naming "chapters/preamble.tex" was ACCEPTED,
	// and the worker then materialised "preamble.tex" at the root while the
	// document read \input{chapters/preamble} (PR #20 review). So api's caller
	// reads the raw filename out of the part's own Content-Disposition, which
	// the parser does not rewrite, and passes THAT here.
	//
	// The rules below are therefore load-bearing rather than defence in depth:
	// they see what the client sent.
	if strings.ContainsAny(name, `/\`) {
		return "", ErrUnsafeInputFilename
	}
	if name == "." || name == ".." || path.Base(name) != name {
		return "", ErrUnsafeInputFilename
	}
	if len(name) > maxInputFilenameLen {
		return "", ErrUnsafeInputFilename
	}
	if strings.HasPrefix(name, "-") {
		return "", ErrUnsafeInputFilename
	}
	if strings.HasPrefix(name, " ") || strings.HasSuffix(name, " ") ||
		strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") {
		return "", ErrUnsafeInputFilename
	}
	for _, r := range name {
		if r == 0 || unicode.IsControl(r) {
			return "", ErrUnsafeInputFilename
		}
	}
	return name, nil
}

// MaxInputParts bounds how many files one job may carry, document included.
//
// Size does not bound count: a thousand one-byte attachments pass every byte
// ceiling while costing a thousand S3 uploads, a thousand job_inputs rows and a
// thousand worker downloads, all charged as one small job. 32 is past any real
// project and far below the multipart parser's own 1 000-part default.
const MaxInputParts = 32

// WorkerOwnedName reports whether name collides with a file the worker or the
// engine creates in the job's directory.
//
// THE WHOLE "in.*" AND "out.*" NAMESPACE, not two specific names. The worker
// renames the document to in.<source> and writes out.<target>, and the ENGINE
// writes its own product and intermediates beside them from the same stem:
// tectonic turns in.latex into in.pdf. Reserving only the two names the worker
// picks left in.pdf to be silently overwritten by the engine -- measured, with
// an attachment of that name replaced by the compiled PDF (PR #20 review):
//
//	before   ls -> in.latex in.pdf preamble.tex refs.bib
//	         head in.pdf -> %PDF-1.5      (the attachment was gone)
//
// A client's file must not depend on which intermediates an engine happens to
// write, so the stem is reserved rather than the extensions enumerated.
// WorkerOwnedName is exported because BOTH gates need it. The API refuses these
// names before anything is stored; the worker refuses them again before it
// creates a file, because a job row is data and a row that did not come from
// the API is exactly when a worker should stop.
func WorkerOwnedName(name string) bool {
	stem := name
	if i := strings.IndexByte(name, '.'); i >= 0 {
		stem = name[:i]
	}
	return stem == "in" || stem == "out"
}
