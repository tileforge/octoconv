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

// workerOwnedName reports whether name collides with a file the worker creates
// for itself in the job's directory.
//
// The worker renames the document: ordinal 0 becomes in.<source format> and the
// engine writes out.<target format> beside it. Those two names are therefore
// not the client's to use -- an attachment called in.latex would overwrite the
// document that passed every check, and the engine would compile the part that
// passed none.
func workerOwnedName(name, sourceFormat, targetFormat string) bool {
	return name == "in."+sourceFormat || name == "out."+targetFormat
}
