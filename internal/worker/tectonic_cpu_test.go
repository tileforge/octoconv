package worker

import (
	"fmt"
	"testing"

	"github.com/tileforge/octoconv/internal/convert"
)

// The two verdicts that come from the SAME pair of numbers -- how long the
// attempt took, and how much CPU the engine actually consumed -- and that must
// not be confused, because one blames the document and the other blames the
// node.

// TestCPUExhaustedIsTerminal: this is what the document COST, measured by the
// kernel and independent of what else the node was doing. It costs the same on
// the next attempt, so retrying spends the budget again to reach the same
// answer.
func TestCPUExhaustedIsTerminal(t *testing.T) {
	err := fmt.Errorf("tectonic: %w after 271s of CPU: ...", convert.ErrTeXCPUExhausted)
	if !isTectonicTerminal(err) {
		t.Fatal("a document that spent its whole CPU budget is retried; the retry burns the " +
			"budget again and reaches the same verdict, on a worker that could have been " +
			"typesetting something else")
	}
	code, msg := tectonicFailureCode(err)
	if code == "" || msg == "" {
		t.Fatal("no client-facing code/message for a CPU refusal")
	}
	// The remedy is a cheaper document, not a faster one, so the message must
	// not be the time-limit wording.
	if msg == "the document did not finish typesetting within the time limit" {
		t.Fatalf("a CPU refusal is reported with the DEADLINE's wording (%q), which sends the "+
			"client looking for a document that is slow rather than one that is expensive", msg)
	}
}

// TestStarvedIsRetryable is the other direction, and the one that costs a
// client a working document when it is wrong: the deadline fired while the
// engine had barely run. Measured in the shipped image at a 2-CPU quota, the
// same document took 0.96s wall for 0.95s of CPU idle and 3.67s wall for 1.08s
// of CPU under 4x oversubscription -- wall inflated 4x, consumption did not
// move.
func TestStarvedIsRetryable(t *testing.T) {
	err := fmt.Errorf("tectonic: %w: the deadline expired after only 12.3s of CPU against a 271s budget",
		convert.ErrTeXEngineStarved)

	if isTectonicTerminal(err) {
		t.Fatal("a deadline that fired while the engine was starved of CPU is treated as a " +
			"document that failed to typeset: the client is told their file is at fault and the " +
			"job is never retried on a quieter node")
	}
}

// And the two must stay distinguishable from the plain deadline, which keeps
// its old meaning: the engine ran, for the whole budget, and did not finish.
func TestPlainDeadlineIsStillTerminal(t *testing.T) {
	err := fmt.Errorf("tectonic: %w: context deadline exceeded", convert.ErrTeXEngineTimeout)
	if !isTectonicTerminal(err) {
		t.Fatal("the ordinary engine deadline stopped being terminal; a non-terminating document " +
			"would now be retried until the retry budget is gone")
	}
}
