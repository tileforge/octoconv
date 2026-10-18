// Package convert (this file): pandocsandbox.go is pandoc's half of the
// engineExec contract -- the measured grant, and the preflight that proves
// it -- mirroring libvips.go's own ConfinedLibvips/LibvipsPreflight pair, but
// built on the shared engineExec/preflight toolkit (sandboxwrap.go,
// sandboxpreflight.go) rather than a per-class copy of either.
package convert

import (
	"context"
	"fmt"
	"path/filepath"
)

// pandocReadOnly is what the engine needs to read, beyond its own binary,
// MEASURED rather than reasoned -- docs/SANDBOX-PATHS.md's
// "pandoc" section, 2026-09-20.
//
// Empty. The release binary is statically linked (ldd: "not a dynamic
// executable"), and the trace phase found zero paths outside the job
// directory across five format targets. The binary itself is granted
// separately, through pandocEngines -- forJob resolves it and adds it to the
// ruleset, the same way every other class's own executable is granted.
var pandocReadOnly = []string{}

// pandocEngines is every binary PandocConverter.Convert invokes. One: the
// pair table always dispatches to a single `pandoc` subprocess, unlike OCR's
// tesseract+pdftoppm+pdfinfo+pdftotext or AV's ffmpeg+ffprobe.
var pandocEngines = []string{"pandoc"}

// WithSandbox returns c with Sandbox set to a *engineSandbox built from
// helper. A method rather than a ConfinedPandoc-style constructor
// (ConfinedLibvips's own shape) because PandocConverter carries MaxInputBytes
// too, and this lets cmd/pandoc-worker keep configuring it in one place:
// buildPandocConverter(...).WithSandbox(helper).
func (c PandocConverter) WithSandbox(helper []string) PandocConverter {
	c.Sandbox = newEngineSandbox(helper)
	return c
}

// PandocPreflight proves the engine can actually be EXECUTED under the
// ruleset c would build for a job, before any job depends on it -- both
// halves: that a conversion INSIDE the job directory succeeds (sufficiency)
// and that the identical bytes OUTSIDE it are denied (effectiveness),
// judged by output content in both directions (CONF-04), never by exit
// status alone.
//
// nil when c is unconfined: nothing to prove for a deployment that has
// deliberately turned confinement off.
func PandocPreflight(ctx context.Context, c PandocConverter) error {
	if !c.Sandbox.confines() {
		return nil
	}

	w, cleanup, err := newPreflightWorkspace("pandoc")
	if err != nil {
		return err
	}
	defer cleanup()

	marker, err := preflightMarker()
	if err != nil {
		return err
	}
	markdown := []byte("# OctoConv sandbox preflight\n\n" + marker + "\n")

	inside, outside, err := w.place("in.md", markdown)
	if err != nil {
		return err
	}

	// Sufficiency: the SAME converter value that will run production jobs
	// converts a file INSIDE the job directory, and the output carries the
	// marker.
	outInside := filepath.Join(w.jobDir, "out.html")
	if err := c.Convert(ctx, inside, outInside, nil); err != nil {
		return fmt.Errorf("the engine cannot convert under the sandbox: %w", err)
	}
	if err := requireOutput(outInside, []byte(marker)); err != nil {
		return fmt.Errorf("the engine cannot convert under the sandbox: %w", err)
	}

	// Effectiveness: the IDENTICAL bytes, converted under the IDENTICAL
	// ruleset, from OUTSIDE the job directory. The positive run above already
	// proved the engine and the marker work; a failure here is attributable
	// only to location.
	outOutside := filepath.Join(w.jobDir, "neg.html")
	convErr := c.Convert(ctx, outside, outOutside, nil)
	if err := requireDenied(convErr, outOutside, []byte(marker)); err != nil {
		return fmt.Errorf("the engine read a file outside the job directory: %w", err)
	}
	return nil
}
