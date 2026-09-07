package convert

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The worker's classifier keys on the sentinels, and its tests prove it
// handles them. Nothing proved the CONVERTER produces them -- which is the
// same "check and checked thing are different objects" mistake that cost this
// phase four findings. These drive Convert itself.
//
// No tectonic involved: a stand-in engine reproduces the two kernel-level
// outcomes that matter, which is what the classification actually reads.

func cpuTestConverter(t *testing.T, engineBody string, cpuSeconds int) (TectonicConverter, string) {
	t.Helper()
	dir := t.TempDir()

	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// Convert resolves "tectonic" from PATH before invoking the helper.
	if err := os.WriteFile(filepath.Join(bin, "tectonic"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))

	// The stand-in helper ignores the ruleset tokens and runs its own body, so
	// the test needs no kernel support beyond `ulimit -t`.
	helper := filepath.Join(dir, "helper")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\n"+engineBody), 0o755); err != nil {
		t.Fatal(err)
	}

	bundle := filepath.Join(dir, "bundle.ttb")
	if err := os.WriteFile(bundle, []byte("not a real bundle"), 0o600); err != nil {
		t.Fatal(err)
	}
	in := filepath.Join(dir, "in.tex")
	if err := os.WriteFile(in, []byte("\\documentclass{article}\\begin{document}x\\end{document}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return TectonicConverter{
		BundlePath:    bundle,
		SandboxHelper: []string{helper},
		CPUSeconds:    cpuSeconds,
	}, in
}

// TestConvertProducesTheCPUExhaustedSentinel: a child killed by a signal after
// spending its whole budget must be reported as the document costing too much,
// not as a generic engine failure -- the classifier downstream can only act on
// what this produces.
func TestConvertProducesTheCPUExhaustedSentinel(t *testing.T) {
	// Burns CPU under a 1-second RLIMIT_CPU of its own, so the kernel kills it
	// exactly the way ApplyLimits arranges in production.
	c, in := cpuTestConverter(t, "ulimit -t 1\nwhile :; do :; done\n", 1)

	err := c.Convert(context.Background(), in, filepath.Join(filepath.Dir(in), "out.pdf"), nil)
	if err == nil {
		t.Fatal("a child killed for exceeding its CPU limit produced no error")
	}
	if !errors.Is(err, ErrTeXCPUExhausted) {
		t.Fatalf("the CPU refusal is not classified, so the worker sees a generic engine failure "+
			"and the client is told their document could not be typeset rather than that it cost "+
			"too much: %v", err)
	}
	if !strings.Contains(err.Error(), "of CPU") {
		t.Fatalf("the error does not say how much CPU was spent, which is the one number an "+
			"operator needs to tell a bad document from a budget set too low: %v", err)
	}
}

// TestConvertProducesTheStarvedSentinel is the other direction and the one
// that costs a client a working document: the deadline fires while the engine
// has barely run.
func TestConvertProducesTheStarvedSentinel(t *testing.T) {
	// Sleeps rather than working: consumes no CPU at all, so when the deadline
	// fires the budget is untouched.
	c, in := cpuTestConverter(t, "sleep 30\n", 20)

	ctx, cancel := context.WithTimeout(context.Background(), 2*1e9)
	defer cancel()

	err := c.Convert(ctx, in, filepath.Join(filepath.Dir(in), "out.pdf"), nil)
	if err == nil {
		t.Fatal("a deadline that expired produced no error")
	}
	if !errors.Is(err, ErrTeXEngineStarved) {
		t.Fatalf("a deadline that fired while the engine consumed no CPU is not classified as "+
			"starvation, so the worker treats it as a document that failed to typeset -- the "+
			"client is blamed and the job is never retried on a quieter node: %v", err)
	}
	if errors.Is(err, ErrTeXEngineTimeout) {
		t.Fatal("it is ALSO the terminal timeout sentinel; whichever the worker checks first " +
			"decides, and that is not a decision to leave to ordering")
	}
}

// And a document that genuinely worked for its whole deadline keeps the old
// meaning: terminal, because it did not finish and will not next time.
func TestConvertStillTimesOutWhenTheEngineWasBusy(t *testing.T) {
	c, in := cpuTestConverter(t, "while :; do :; done\n", 2)

	ctx, cancel := context.WithTimeout(context.Background(), 3*1e9)
	defer cancel()

	err := c.Convert(ctx, in, filepath.Join(filepath.Dir(in), "out.pdf"), nil)
	if err == nil {
		t.Fatal("no error from a document that ran past its deadline")
	}
	if errors.Is(err, ErrTeXEngineStarved) {
		t.Fatalf("an engine that burned CPU for the whole deadline is reported as STARVED, so a "+
			"genuinely non-terminating document would be retried until the budget is gone: %v", err)
	}
}
