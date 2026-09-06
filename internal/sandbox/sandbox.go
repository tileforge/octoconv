// Package sandbox confines a conversion engine's filesystem access to the files
// its job actually needs.
//
// # WHY IT EXISTS
//
// PR #19 measured what --untrusted does and does not do: it refuses \write18,
// and it does NOT confine reads. A .tex containing
//
//	\newread\f \openin\f=/etc/hostname \read\f to \line ... \line
//
// typeset the container's hostname into the PDF the client got back, and the
// same read against /proc/self/environ returned the worker's environment. That
// PR removed what the reads were WORTH by giving the engine an allowlist
// environment, and said plainly that it did not remove the reads. This does.
//
// # WHY LANDLOCK
//
// The todo left behind by #19 costed three options and called a mount namespace
// per job "the only one that makes the guarantee structural" -- while noting it
// needs a capability the chart does not grant. Landlock needs none: an
// unprivileged process restricts ITSELF, the restriction is inherited across
// execve and cannot be lifted. Measured in the shipped image as nobody: ABI
// version 8.
//
// The rules are applied by a process that then execs the engine, because they
// cannot be applied to someone else -- see cmd/tectonic-worker's sandbox-exec
// mode.
package sandbox

import (
	"fmt"
	"strings"
)

// Paths is what a sandboxed engine may touch. Everything else in the container
// -- /etc, /proc, /run, any future mounted secret -- becomes unreachable, which
// is the point.
type Paths struct {
	// ReadWrite is the job's own directory: the input the worker materialised,
	// the output the engine writes, and whatever intermediates it keeps.
	ReadWrite []string
	// ReadOnly is what the engine needs to run at all: its bundle, its
	// precompiled format cache, and the system directories holding the binary
	// and its shared libraries.
	ReadOnly []string
}

// Spec renders p as argv tokens.
//
// # WHY A WIRE FORMAT AT ALL
//
// Landlock restricts the CALLING process, so the rules must be applied by
// whatever then becomes the engine -- across an exec boundary, where the only
// thing that survives is argv. Rendering and parsing live together here
// because they are one format with two ends in two binaries, and a format
// whose halves live apart is a format that drifts.
func (p Paths) Spec() []string {
	spec := make([]string, 0, len(p.ReadWrite)+len(p.ReadOnly))
	for _, path := range p.ReadWrite {
		spec = append(spec, "rw:"+path)
	}
	for _, path := range p.ReadOnly {
		spec = append(spec, "ro:"+path)
	}
	return spec
}

// ParseSpec reads Spec's output back, and returns whatever followed "--" as
// the command to exec.
//
// An unrecognised token is an ERROR rather than something to skip: a dropped
// token is a rule that was meant to be granted and was not, which surfaces
// much later as an engine failing to read a file nobody can explain.
func ParseSpec(argv []string) (Paths, []string, error) {
	var p Paths
	i := 0
	for ; i < len(argv) && argv[i] != "--"; i++ {
		switch {
		case strings.HasPrefix(argv[i], "ro:"):
			p.ReadOnly = append(p.ReadOnly, strings.TrimPrefix(argv[i], "ro:"))
		case strings.HasPrefix(argv[i], "rw:"):
			p.ReadWrite = append(p.ReadWrite, strings.TrimPrefix(argv[i], "rw:"))
		default:
			return p, nil, fmt.Errorf("sandbox: unrecognised argument %q", argv[i])
		}
	}
	if i >= len(argv)-1 {
		return p, nil, fmt.Errorf("sandbox: no command after --")
	}
	return p, argv[i+1:], nil
}
