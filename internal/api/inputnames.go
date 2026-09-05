package api

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
	// WHAT THIS ACTUALLY CATCHES, measured rather than assumed. Go's
	// mime/multipart already applies filepath.Base to the filename it parses,
	// so over HTTP:
	//
	//	"../../etc/evil.bib"  arrives as "evil.bib"   -- never reaches here
	//	"/etc/evil.bib"       arrives as "evil.bib"   -- never reaches here
	//	"sub\evil.bib"        arrives unchanged        -- refused here
	//	".."                  arrives unchanged        -- refused here
	//
	// So the POSIX traversal a reader would expect this to refuse is already
	// gone, and a client's "dir/file.tex" DOES silently become "file.tex"
	// before this function sees it. The rules below are still worth having --
	// they catch what the standard library leaves (a Windows separator a later
	// join could read, "..", flag-shaped names, control characters) and they
	// hold for any caller that is not the HTTP parser -- but claiming they stop
	// traversal would be crediting this code with the runtime's work.
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

// maxInputParts bounds how many files one job may carry, document included.
//
// Size does not bound count: a thousand one-byte attachments pass every byte
// ceiling while costing a thousand S3 uploads, a thousand job_inputs rows and a
// thousand worker downloads, all charged as one small job. 32 is past any real
// project and far below the multipart parser's own 1 000-part default.
const maxInputParts = 32

// workerOwnedName reports whether name collides with a file the worker or the
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
func workerOwnedName(name string) bool {
	stem := name
	if i := strings.IndexByte(name, '.'); i >= 0 {
		stem = name[:i]
	}
	return stem == "in" || stem == "out"
}
