package worker

import (
	"fmt"
	"testing"

	"github.com/apaderin/octoconv/internal/convert"
)

// Every other tectonic failure is a property of the DOCUMENT and therefore
// terminal: the same source produces the same missing package, the same
// runaway expansion, the same oversized PDF on every attempt. This one is a
// property of the NODE.
//
// Left terminal, one node whose kernel lost Landlock -- a rollback, a
// node-pool upgrade, a securityContext edit -- would consume tex jobs, fail
// each in milliseconds, and tell every client their document is broken. It
// would also be the fastest consumer in the fleet, so it would win the race
// for work.
func TestSandboxUnavailableIsRetryable(t *testing.T) {
	err := fmt.Errorf("tectonic: %w: __sandbox-exec: landlock: create ruleset: invalid argument",
		convert.ErrTeXSandboxUnavailable)

	if isTectonicTerminal(err) {
		t.Fatal("a node that cannot establish the sandbox is treated as a document that cannot be " +
			"typeset: the job is never retried elsewhere, and the client is told their file is at fault")
	}
}

// The classifier below the sentinel check matches on SUBSTRINGS of the error
// text, and that text carries the helper's stderr, which this package does not
// author. A message that happens to contain one of those signatures must not
// flip the verdict.
func TestSandboxUnavailableStaysRetryableWhateverTheHelperPrinted(t *testing.T) {
	for _, tail := range []string{
		"landlock: open /opt/tectonic: permission denied",
		"exec /usr/local/bin/tectonic: emergency stop",
		"unrecoverable error while creating the ruleset",
		"latex error",
	} {
		err := fmt.Errorf("tectonic: %w: %s", convert.ErrTeXSandboxUnavailable, tail)
		if isTectonicTerminal(err) {
			t.Errorf("helper output %q made a node fault terminal; the sentinel must decide, not the text", tail)
		}
	}
}
