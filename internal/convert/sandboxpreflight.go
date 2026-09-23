// Package convert (this file): sandboxpreflight.go is the toolkit every
// confined engine class's own <Class>Preflight function is built from --
// LibvipsPreflight and TectonicPreflight each grew their own workspace and
// judgment before this landed; PandocPreflight and the classes after it
// share this one instead.
//
// The shape is the same for every class: convert something REAL through the
// REAL helper and judge the result by CONTENT, never by exit status alone --
// ghostscript was measured to exit 0 and write nothing when denied, which is
// the whole reason a preflight exists at all (LibvipsPreflight's own doc
// comment). This file adds the piece none of the earlier preflights needed
// on their own: a SECOND, sibling workspace outside the job directory, so a
// preflight can prove not just that confinement lets the right thing happen
// (sufficiency) but that it denies the wrong one (effectiveness) -- with the
// identical bytes, under the identical ruleset, so a failure outside is
// attributable only to location.
package convert

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// preflightWorkspace is a preflight's two directories: the job directory the
// ruleset grants, and a SIBLING directory it does not. Both are real
// directories on disk, not a single tree with an "outside" subdirectory --
// the ruleset must have a genuine reason to deny the second one, not merely
// a path that happens to fall outside a granted prefix by construction.
type preflightWorkspace struct {
	jobDir     string
	outsideDir string
}

// newPreflightWorkspace creates the two directories and returns a cleanup
// func that removes both. class names the caller (e.g. "pandoc") so a
// process running several preflights leaves distinguishable temp
// directories on a failed cleanup.
func newPreflightWorkspace(class string) (preflightWorkspace, func(), error) {
	jobDir, err := os.MkdirTemp("", class+"-preflight-job-")
	if err != nil {
		return preflightWorkspace{}, nil, fmt.Errorf("preflight job directory: %w", err)
	}
	outsideDir, err := os.MkdirTemp("", class+"-preflight-outside-")
	if err != nil {
		os.RemoveAll(jobDir)
		return preflightWorkspace{}, nil, fmt.Errorf("preflight outside directory: %w", err)
	}
	w := preflightWorkspace{jobDir: jobDir, outsideDir: outsideDir}
	cleanup := func() {
		os.RemoveAll(jobDir)
		os.RemoveAll(outsideDir)
	}
	return w, cleanup, nil
}

// place writes identical bytes to both directories under name, then reads the
// OUTSIDE copy back through this process's own (unconfined) identity and
// compares it -- a canary this uid cannot even read before any ruleset is
// applied proves nothing when the ruleset later denies it too: the denial
// would say nothing about confinement, only about permissions that already
// existed.
func (w preflightWorkspace) place(name string, data []byte) (inside, outside string, err error) {
	inside = filepath.Join(w.jobDir, name)
	outside = filepath.Join(w.outsideDir, name)
	if err := os.WriteFile(inside, data, 0o600); err != nil {
		return "", "", fmt.Errorf("preflight fixture (inside): %w", err)
	}
	if err := os.WriteFile(outside, data, 0o600); err != nil {
		return "", "", fmt.Errorf("preflight fixture (outside): %w", err)
	}
	got, err := os.ReadFile(outside)
	if err != nil {
		return "", "", fmt.Errorf("preflight canary: the outside fixture cannot be read even before "+
			"any ruleset is applied, so a later denial would prove nothing: %w", err)
	}
	if !bytes.Equal(got, data) {
		return "", "", fmt.Errorf("preflight canary: the outside fixture read back different bytes "+
			"than it was written, before any ruleset was applied")
	}
	return inside, outside, nil
}

// requireOutput is the sufficiency judgment: path must exist, be non-empty,
// and -- when want is non-nil -- contain it. Judging by content, not merely
// existence, is what catches an engine that exits 0 and writes an empty or
// unrelated file when denied (LibvipsPreflight's ghostscript finding).
func requireOutput(path string, want []byte) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("no output at %s: %w", path, err)
	}
	if len(data) == 0 {
		return fmt.Errorf("output at %s is empty", path)
	}
	if want != nil && !bytes.Contains(data, want) {
		return fmt.Errorf("output at %s does not contain the expected marker", path)
	}
	return nil
}

// requireDenied is the effectiveness judgment, the mirror of requireOutput:
// the conversion of a file OUTSIDE the job directory must have failed
// (convErr == nil is itself a failure of the judgment -- confinement is not
// in effect), and outPath must carry no trace of having succeeded partway --
// absent, or present but not carrying marker when one was given.
func requireDenied(convErr error, outPath string, marker []byte) error {
	if convErr == nil {
		return fmt.Errorf("converted a file outside the job directory: confinement is not in effect")
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		return nil // no output at all: the denial is clean
	}
	if marker == nil {
		if len(data) > 0 {
			return fmt.Errorf("the denied conversion still produced %d bytes of output at %s",
				len(data), outPath)
		}
		return nil
	}
	if bytes.Contains(data, marker) {
		return fmt.Errorf("output at %s carries the marker from the denied file: it was converted anyway",
			outPath)
	}
	return nil
}

// preflightMarker is a value a preflight can plant in a fixture and then
// search for in output -- unique per run, so a marker found in an output
// could only have come from THIS preflight's own fixture, never a stale file
// left by an earlier one.
func preflightMarker() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("preflight marker: %w", err)
	}
	return "octoconv-preflight-" + hex.EncodeToString(b), nil
}
