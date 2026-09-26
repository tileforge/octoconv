// Package convert (this file): htmlsandbox.go is html's half of the
// engineExec contract -- the measured grant (including the CONF-06 /tmp
// decision), the shared-/tmp guard, the sweep, and the preflight that proves
// confinement by PDF content -- mirroring pandocsandbox.go's shape (a single
// call site), extended with the writable-set and environment pieces html's
// own D-01..D-08 decisions require.
package convert

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// htmlReadWrite is the class's writable set beyond the job directory itself
// (forJobRW's extraRW), MEASURED (SANDBOX-PATHS.md html section, reconfirmed
// 2026-09-25): chromium puts its shared memory in /tmp regardless of TMPDIR
// (measured, does not move), and needs /dev/null. This is the CONF-06 /tmp
// decision's grant-side consequence -- see sharedTmpGuard/ConfinedChromium
// for the concurrency-1 invariant that makes granting it safe at all.
var htmlReadWrite = []string{"/tmp", "/dev/null"}

// htmlReadOnly is what the engine needs to read, beyond its own binary,
// MEASURED (SANDBOX-PATHS.md html section, reconfirmed 2026-09-25). Two
// entries are NOT auto-resolved by forJobRW's engines mechanism (D-08,
// Pitfall 3): the wrapper /usr/bin/chromium-headless-shell is granted
// through htmlEngines (a PATH lookup), but its interpreter /usr/bin/dash is
// never looked up by name -- the wrapper's own bytes are
// "#!/bin/sh -e\n\n/usr/lib/chromium/chromium-headless-shell \"$@\"\n" (61
// bytes, no exec keyword) -- so /usr/bin/dash must be listed literally here.
// The real binary under /usr/lib/chromium is covered by the /usr/lib entry
// below, not by a third literal path.
var htmlReadOnly = []string{
	"/usr/bin/dash",
	"/lib",
	"/usr/lib",
	"/usr/share",
	"/usr/local/share",
	"/etc/fonts",
	"/etc/ld.so.cache",
	"/etc/localtime",
	"/etc/nsswitch.conf",
	"/etc/host.conf",
	"/etc/hosts",
	"/etc/resolv.conf",
	"/proc",
	"/sys/devices",
	"/dev/urandom",
}

// htmlEngines is every binary ChromiumConverter.Convert invokes. One: a
// single chromium-headless-shell subprocess per job, like pandoc's single
// `pandoc` subprocess.
var htmlEngines = []string{"chromium-headless-shell"}

// htmlPreflightOutsideBase is where HTMLPreflight places its "outside"
// fixture. html's own writable AND readable grants both include the system
// temp dir (/tmp, via htmlReadWrite): newPreflightWorkspace's default
// (os.TempDir()) would put the outside file somewhere the ruleset can
// legitimately read, and the preflight would refuse every start for having
// nothing to test. /var/tmp is world-writable in debian:bookworm-slim and
// lies outside every html grant; requireOutsideGrants proves that at run
// time rather than trusting this comment.
const htmlPreflightOutsideBase = "/var/tmp"

// WithSandbox returns c with Sandbox set to a *engineSandbox built from
// helper. A method rather than a constructor, mirroring
// PandocConverter.WithSandbox -- ConfinedChromium (below) is the one that
// also wires the D-03/D-04 concurrency guard and sweep, the same way
// cmd/chromium-worker composes them.
func (c ChromiumConverter) WithSandbox(helper []string) ChromiumConverter {
	c.Sandbox = newEngineSandbox(helper)
	return c
}

// ConfinedChromium builds a ChromiumConverter confined by helper, enforcing
// D-03 (a confined worker whose ruleset grants the shared /tmp must run at
// most one task at a time) BEFORE returning anything usable, and wiring D-04
// (sweep the shared /tmp before every task) when the class is actually
// confined.
//
// The unconfined path (helper empty, the escape hatch) gets neither the
// guard nor the sweep: an unconfined engine can already read/write
// everything in the container, so refusing concurrency here would protect
// nothing, and sweeping a shared /tmp at concurrency above 1 would delete a
// CONCURRENT task's own files out from under it.
func ConfinedChromium(helper []string, concurrency int, concurrencyEnv string) (ChromiumConverter, error) {
	c := ChromiumConverter{}.WithSandbox(helper)
	if err := sharedTmpGuard(htmlReadWrite, c.Sandbox, concurrency, concurrencyEnv); err != nil {
		return ChromiumConverter{}, err
	}
	if c.Sandbox.confines() && slices.Contains(htmlReadWrite, sharedTmpDir) {
		c.sharedTmp = sharedTmpDir
	}
	return c, nil
}

// sweepSharedTmp removes every entry of dir except the one holding keep
// (the current job's own directory, itself created under /tmp --
// internal/worker/worker.go's os.MkdirTemp("", ...)). Safe ONLY because
// D-01/D-03 hold concurrency to 1 for any class that grants dir: there is no
// concurrent task whose files this could delete out from under it.
//
// Removing a symlink entry removes the symlink itself, never whatever it
// points at (os.RemoveAll never follows a top-level symlink argument to its
// target).
//
// A failure here is a NODE fault -- a permission race, or a file another
// process still holds open -- not a statement about any client's file, so it
// is wrapped in ErrEngineSandboxUnavailable and is retryable, exactly like a
// sandbox that could not be established at all.
func sweepSharedTmp(dir, keep string) error {
	cleanDir := filepath.Clean(dir)
	cleanKeep := filepath.Clean(keep)
	entries, err := os.ReadDir(cleanDir)
	if err != nil {
		return fmt.Errorf("%w: sweep %s before the task: %w", ErrEngineSandboxUnavailable, cleanDir, err)
	}
	for _, e := range entries {
		p := filepath.Join(cleanDir, e.Name())
		if p == cleanKeep || strings.HasPrefix(cleanKeep, p+string(filepath.Separator)) {
			continue
		}
		if err := os.RemoveAll(p); err != nil {
			return fmt.Errorf("%w: sweep %s before the task: %w", ErrEngineSandboxUnavailable, cleanDir, err)
		}
	}
	return nil
}

// HTMLPreflight proves the engine can actually be EXECUTED under the
// ruleset c would build for a job, before any job depends on it -- both
// halves, judged by PDF CONTENT (D-06/D-07), never by chromium's exit
// status: under a ruleset chromium renders a blank 652-byte PDF when a read
// is denied instead of failing, so "output exists" or "conversion returned
// an error" prove nothing in either direction here.
//
// nil when c is unconfined: nothing to prove for a deployment that has
// deliberately turned confinement off.
func HTMLPreflight(ctx context.Context, c ChromiumConverter) error {
	if !c.Sandbox.confines() {
		return nil
	}
	// A confined converter whose ruleset grants the shared /tmp MUST have
	// been built with the sweep wired (ConfinedChromium) -- checked before
	// any workspace exists, because a preflight run without the sweep would
	// itself be racing whatever a prior task left in /tmp.
	if slices.Contains(htmlReadWrite, sharedTmpDir) && c.sharedTmp == "" {
		return fmt.Errorf("a confined html converter must sweep the shared /tmp before every task (CONF-06); " +
			"build it with ConfinedChromium")
	}

	w, cleanup, err := newPreflightWorkspaceOutside("html", htmlPreflightOutsideBase)
	if err != nil {
		return err
	}
	defer cleanup()

	if err := w.requireOutsideGrants(htmlReadWrite, htmlReadOnly); err != nil {
		return err
	}

	marker, err := preflightMarker()
	if err != nil {
		return err
	}
	html := []byte("<html><head><title>" + marker + "</title></head><body><p>" + marker + "</p></body></html>")

	inside, outside, err := w.place("in.html", html)
	if err != nil {
		return err
	}

	// Sufficiency (D-06/D-07): the SAME converter that will run production
	// jobs converts a file INSIDE the job directory, through the real
	// production Convert path, and the PDF it produces carries the marker
	// verbatim, uncompressed, as /Title (marker) in the /Info dictionary
	// (measured, octoconv-chromium-worker:measure69).
	outInside := filepath.Join(w.jobDir, "out.pdf")
	if err := c.Convert(ctx, inside, outInside, nil); err != nil {
		return fmt.Errorf("the engine cannot convert under the sandbox: %w", err)
	}
	if err := requireOutput(outInside, []byte("/Title ("+marker+")")); err != nil {
		return fmt.Errorf("the engine cannot convert under the sandbox: %w", err)
	}

	// Effectiveness: this canNOT go through Convert, because Convert copies
	// its input into the job directory (rendered.html) before the engine
	// ever runs -- Convert(outside, ...) would render the marker from the
	// COPY inside the job directory and prove nothing about confinement.
	// The negative run instead points the ENGINE ITSELF at the outside file,
	// using the identical argv builder and the identical ruleset Convert
	// uses. chromium exits 0 and writes a blank 652-byte PDF when denied
	// (D-06), so its error is deliberately not judged here -- only the
	// output's content is.
	outOutside := filepath.Join(w.jobDir, "neg.pdf")
	_, convErr := c.exec(w.jobDir).run(ctx, "chromium-headless-shell",
		chromiumPrintArgs(w.jobDir, outOutside, "file://"+outside)...)
	_ = convErr
	if err := requireAbsent(outOutside, []byte(marker)); err != nil {
		return fmt.Errorf("the engine read a file outside the job directory: %w", err)
	}
	return nil
}
