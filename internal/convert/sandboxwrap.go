package convert

import (
	"context"
	"errors"
	"fmt"
	"os/exec"

	"github.com/apaderin/octoconv/internal/sandbox"
)

// engineSandbox is what a converter needs to run its engine confined: the
// helper argv prefix, and whether a filesystem ruleset is wanted at all.
//
// Held by POINTER in converters, and nil-safe in every method below, so that
// adding it does not make an exported converter non-comparable. LibvipsConverter
// was an empty struct and could be compared with ==; a slice field would have
// taken that away silently, and the property is easier to keep than to restore
// once something depends on it (codex, PR #28). A nil *engineSandbox means "run
// the engine directly", which is the zero value every test wants.
type engineSandbox struct {
	// Helper is the argv prefix from sandbox.ResolveHelper. Empty means the
	// engine is executed directly.
	Helper []string
	// Confine asks for the filesystem ruleset. False with a non-empty Helper
	// is meaningful for an engine that also carries a resource limit -- see
	// TectonicConverter -- and means "run through the helper, restrict
	// nothing", because an EMPTY ruleset denies everything rather than nothing.
	Confine bool
}

// wrapEngine turns (engine name, args) into the command actually executed.
//
// It resolves the engine through PATH here rather than leaving it to the
// helper, so a missing binary is reported as a missing binary instead of as a
// sandbox that failed to establish -- two failures an operator would otherwise
// have to tell apart from one exit status.
func (s *engineSandbox) wrapEngine(name string, spec []string, args []string) (string, []string, error) {
	enginePath, err := exec.LookPath(name)
	if err != nil {
		return "", nil, fmt.Errorf("locate engine %s: %w", name, err)
	}
	if s == nil || len(s.Helper) == 0 {
		return enginePath, args, nil
	}
	wrapped := append(append(append(
		append([]string{}, s.Helper[1:]...),
		spec...),
		"--", enginePath), args...)
	return s.Helper[0], wrapped, nil
}

// enginePathFor is wrapEngine's lookup alone, for a spec builder that needs
// the resolved binary before the argv is assembled -- every ruleset has to
// grant the engine its own executable, which no trace reports reliably.
func enginePathFor(name string) (string, error) {
	p, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("locate engine %s: %w", name, err)
	}
	return p, nil
}

// confines reports whether a filesystem ruleset is wanted. Nil-safe for the
// same reason wrapEngine is: a converter built without a sandbox is the
// ordinary case in tests.
func (s *engineSandbox) confines() bool { return s != nil && s.Confine }

// spec is a thin alias so converters read as "paths, then spec" rather than
// repeating the SpecWith call with an empty Limits every time.
func spec(rw, ro []string) []string {
	return sandbox.Paths{ReadWrite: rw, ReadOnly: ro}.SpecWith(sandbox.Limits{})
}

// ErrEngineSandboxUnavailable is that status, classified.
//
// The helper exited sandbox.SetupExit, so the engine never ran at all -- a
// property of THIS NODE (a Landlock ruleset it could not apply), never of the
// client's file. Mirrors ErrTeXSandboxUnavailable (tectonic.go); a plain
// converter that reads/writes files and never runs a program-as-input has no
// CPU-exhaustion or engine-starved cousin to it, so this is the only sentinel
// engineExec needs.
var ErrEngineSandboxUnavailable = errors.New("engine sandbox could not be established")

// newEngineSandbox builds the sandbox an engineExec wraps around a job.
//
// nil for an empty helper, so a converter built with one stays equal to the
// zero value -- ConfinedLibvips(nil) and LibvipsConverter{} must compare
// equal, and a non-nil *engineSandbox with Confine:false would not.
func newEngineSandbox(helper []string) *engineSandbox {
	if len(helper) == 0 {
		return nil
	}
	return &engineSandbox{Helper: helper, Confine: true}
}

// engineExec is one job's ruleset, built once and shared by every engine
// invocation that job makes.
//
// Splitting "build the ruleset" (forJob) from "run one command under it"
// (run) is what lets a class with several call sites -- OCR's tesseract AND
// pdfinfo AND pdftoppm AND pdftotext -- share one spec instead of rebuilding
// it, argument by argument, at each site.
type engineExec struct {
	sandbox *engineSandbox
	spec    []string
}

// forJob builds one job's engineExec: readOnly is the class's own measured
// set (e.g. pandocReadOnly), engines is every binary name that job's engine
// may invoke -- each resolved through PATH and added to the ruleset, because
// none of them appear in a filesystem trace (they are execve'd, not opened).
//
// When s does not confine (nil, or Confine: false), the returned engineExec
// carries no spec at all: wrapEngine then runs the engine directly for a nil
// sandbox, and passes an EMPTY spec through the helper for one with
// Confine: false -- "run through the helper, restrict nothing" (tectonic's
// own CPU-budget-without-filesystem-ruleset shape).
//
// The job directory is the ONLY writable path, and it is deliberately the
// ONLY path granted at all beyond the class's static set and its engines --
// unlike libvipsSpec, the input's own directory is never granted separately.
// The worker materialises every input inside the job directory before
// Convert ever runs, so a file anywhere else has no legitimate reason to be
// read, and the preflight's negative run depends on exactly that being
// denied.
func (s *engineSandbox) forJob(jobDir string, readOnly, engines []string) engineExec {
	if !s.confines() {
		return engineExec{sandbox: s}
	}
	ro := append([]string{}, readOnly...)
	for _, name := range engines {
		// A name that does not resolve is skipped, not an error here: it
		// cannot be exec'd either way, and the call site that actually needs
		// it reports "locate engine" through wrapEngine when it tries.
		if p, err := exec.LookPath(name); err == nil {
			ro = append(ro, p)
		}
	}
	return engineExec{sandbox: s, spec: spec([]string{jobDir}, ro)}
}

// run wraps and executes one engine invocation under e's ruleset, naming the
// ENGINE (not the helper) in every error runCommandAs produces, and turning a
// helper-exited-78 failure into the retryable ErrEngineSandboxUnavailable
// rather than an ordinary engine failure.
func (e engineExec) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmdName, cmdArgs, err := e.sandbox.wrapEngine(name, e.spec, args)
	if err != nil {
		return nil, err
	}
	out, err := runCommandAs(ctx, name, cmdName, cmdArgs...)
	return out, e.sandboxFault(err)
}

// sandboxFault turns a helper exit of sandbox.SetupExit into
// ErrEngineSandboxUnavailable, keeping the original *exec.ExitError reachable
// through errors.As/errors.Unwrap. Any other error -- including a genuine
// engine failure that merely happens to exit some other code -- is returned
// unchanged.
func (e engineExec) sandboxFault(err error) error {
	if err == nil || e.sandbox == nil || len(e.sandbox.Helper) == 0 {
		return err
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == sandbox.SetupExit {
		return fmt.Errorf("%w: %w", ErrEngineSandboxUnavailable, err)
	}
	return err
}
