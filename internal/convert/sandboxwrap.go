package convert

import (
	"fmt"
	"os/exec"

	"github.com/apaderin/octoconv/internal/sandbox"
)

// engineSandbox is what a converter needs to run its engine confined: the
// helper argv prefix, and whether a filesystem ruleset is wanted at all.
//
// Embedded in each converter rather than passed per call, so the decision is
// made once at startup where it can refuse to start, not per job where it
// could silently vary.
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
func (s engineSandbox) wrapEngine(name string, spec []string, args []string) (string, []string, error) {
	enginePath, err := exec.LookPath(name)
	if err != nil {
		return "", nil, fmt.Errorf("locate engine %s: %w", name, err)
	}
	if len(s.Helper) == 0 {
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

// spec is a thin alias so converters read as "paths, then spec" rather than
// repeating the SpecWith call with an empty Limits every time.
func spec(rw, ro []string) []string {
	return sandbox.Paths{ReadWrite: rw, ReadOnly: ro}.SpecWith(sandbox.Limits{})
}
