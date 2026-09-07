package convert

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/apaderin/octoconv/internal/sandbox"
	"testing"
)

// The worker's classifier keys on the sentinels, and its tests prove it
// handles them. Nothing proved the CONVERTER produces them -- which is the
// same "check and checked thing are different objects" mistake that cost this
// phase four findings. These drive Convert itself.
//
// No tectonic involved: a stand-in engine reproduces the two kernel-level
// outcomes that matter, which is what the classification actually reads.

// helperArgv returns the tokens the stand-in helper was actually handed.
func helperArgv(t *testing.T, in string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(filepath.Dir(in), "argv"))
	if err != nil {
		t.Fatalf("the helper recorded no argv, so it was never invoked: %v", err)
	}
	return strings.Fields(string(b))
}

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

	// The stand-in helper RECORDS ITS ARGV before doing anything, then runs
	// its own body -- so the test needs no kernel support beyond `ulimit -t`,
	// and can still assert that the tokens it was supposed to receive arrived.
	//
	// The recording is not decoration. Without it these tests proved only that
	// the CLASSIFICATION works, and an inversion that stopped emitting the
	// cpu: token passed twice: the helper was killing itself with its own
	// ulimit and the spec was never read.
	helper := filepath.Join(dir, "helper")
	argvFile := filepath.Join(dir, "argv")
	body := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argvFile + "\n" + engineBody
	if err := os.WriteFile(helper, []byte(body), 0o755); err != nil {
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
		BundlePath:        bundle,
		SandboxHelper:     []string{helper},
		CPUSeconds:        cpuSeconds,
		ConfineFilesystem: true,
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

// TestStarvationIsAShareNotABudgetComparison pins the fix for codex round 1's
// second finding, and the reason it is a ratio.
//
// The threshold used to compare consumption against the CONFIGURED budget,
// which made the verdict depend on the relationship between two independently
// configurable numbers: with TECTONIC_ENGINE_CPU_SECONDS=600 and the default
// 271s deadline, a fully saturated runaway consumes ~271 CPU-seconds -- under
// half of 600 -- and would have been called starvation and RETRIED,
// multiplying the runaway's cost across every attempt.
//
// A share of one CPU cannot be gamed that way: a document that had a CPU to
// itself is at 1.00 however the two settings are configured.
func TestStarvationIsAShareNotABudgetComparison(t *testing.T) {
	// The shape of the finding: a budget far larger than the deadline, and an
	// engine that saturates its CPU for the whole attempt.
	c, in := cpuTestConverter(t, "while :; do :; done\n", 600)

	ctx, cancel := context.WithTimeout(context.Background(), 2*1e9)
	defer cancel()

	err := c.Convert(ctx, in, filepath.Join(filepath.Dir(in), "out.pdf"), nil)
	if err == nil {
		t.Fatal("no error from a document that ran past its deadline")
	}
	if errors.Is(err, ErrTeXEngineStarved) {
		t.Fatalf("a runaway that had a whole CPU for the entire attempt is classified as STARVED "+
			"because its consumption was small next to a 600s budget. It would be retried, and "+
			"each retry would burn the same CPU again: %v", err)
	}
	if !errors.Is(err, ErrTeXEngineTimeout) {
		t.Fatalf("want the ordinary engine deadline for a document that worked and did not "+
			"finish, got: %v", err)
	}
}

// TestCPUBudgetAppliesWithLandlockOff is codex round 1's first finding, and
// the configuration my other tests never exercised: TECTONIC_SANDBOX_DISABLED
// is a supported deployment, and the CPU budget does not depend on Landlock.
//
// The bug it pins is the quiet kind -- the worker logged an active budget and
// enforced nothing, which is "a bound that is configured and not plumbed is a
// bound that does not exist", a defect this repository has shipped and named
// before.
func TestCPUBudgetAppliesWithLandlockOff(t *testing.T) {
	c, in := cpuTestConverter(t, "ulimit -t 1\nwhile :; do :; done\n", 1)
	c.ConfineFilesystem = false // the sandbox is off; the budget is not

	err := c.Convert(context.Background(), in, filepath.Join(filepath.Dir(in), "out.pdf"), nil)
	if err == nil {
		t.Fatal("no error at all with the filesystem sandbox off")
	}
	if !errors.Is(err, ErrTeXCPUExhausted) {
		t.Fatalf("with the sandbox off the CPU budget is not enforced or not classified, so a "+
			"deployment that disabled Landlock silently loses the bound it configured: %v", err)
	}

	// The classification above can be satisfied by a child that limits itself.
	// This is the half that says the BOUND WAS ASKED FOR: the cpu: token has
	// to reach the helper, and no path token may, because the sandbox is off.
	argv := helperArgv(t, in)
	var sawCPU bool
	for _, tok := range argv {
		if tok == "cpu:1" {
			sawCPU = true
		}
		if strings.HasPrefix(tok, "ro:") || strings.HasPrefix(tok, "rw:") {
			t.Errorf("the helper was handed %q with the filesystem sandbox OFF; a ruleset nobody "+
				"asked for is being applied.\nargv = %v", tok, argv)
		}
	}
	if !sawCPU {
		t.Fatalf("the helper was never handed a cpu: token, so the budget is not being applied at "+
			"all -- the worker would log an active budget and enforce nothing.\nargv = %v", argv)
	}
}

// And the spec proves which half is being asked for: limits with no path
// tokens, because an EMPTY ruleset denies everything rather than nothing.
func TestSpecWithoutConfinementCarriesOnlyTheLimit(t *testing.T) {
	spec := (sandbox.Paths{}).SpecWith(sandbox.Limits{CPUSeconds: 42})
	if len(spec) != 1 || spec[0] != "cpu:42" {
		t.Fatalf("spec = %v, want exactly [cpu:42]. A path token here would be a ruleset nobody "+
			"asked for; no token at all would be a budget that is never applied.", spec)
	}
}

// TestPreflightHonoursTheConfinementFlag is a REGRESSION I introduced fixing
// codex round 1: making the helper carry the CPU budget meant SandboxHelper is
// non-empty even with Landlock disabled, and the preflight then built a
// filesystem ruleset regardless -- so on a host with no Landlock the worker
// exited 78 at startup, in exactly the deployment TECTONIC_SANDBOX_DISABLED
// exists to support.
//
// The test asserts the SPEC, because that is what the helper acts on: a
// preflight that runs is not evidence that it ran the right configuration.
func TestPreflightHonoursTheConfinementFlag(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "tectonic"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))

	helper := filepath.Join(dir, "helper")
	argvFile := filepath.Join(dir, "argv")
	body := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argvFile + "\nexit 0\n"
	if err := os.WriteFile(helper, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(dir, "bundle.ttb")
	if err := os.WriteFile(bundle, []byte("not a real bundle"), 0o600); err != nil {
		t.Fatal(err)
	}

	c := TectonicConverter{
		BundlePath:        bundle,
		SandboxHelper:     []string{helper},
		CPUSeconds:        271,
		ConfineFilesystem: false, // TECTONIC_SANDBOX_DISABLED=true
	}
	// The stand-in helper exits 0 without producing a PDF, so the preflight
	// reports failure -- what is under test is the ARGV it was handed.
	_ = c.TectonicPreflight(context.Background(), c.SandboxHelper)

	raw, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatalf("the preflight never invoked the helper: %v", err)
	}
	argv := strings.Fields(string(raw))

	var sawCPU bool
	for _, tok := range argv {
		if tok == "cpu:271" {
			sawCPU = true
		}
		if strings.HasPrefix(tok, "ro:") || strings.HasPrefix(tok, "rw:") {
			t.Errorf("the preflight builds a filesystem ruleset with confinement DISABLED: %q.\n"+
				"On a host without Landlock the helper exits 78 and the worker refuses to start, "+
				"which is the one deployment the escape hatch exists for.\nargv = %v", tok, argv)
		}
	}
	if !sawCPU {
		t.Errorf("the preflight carries no cpu: token, so it is not exercising the budget the "+
			"jobs will run under.\nargv = %v", argv)
	}
}
