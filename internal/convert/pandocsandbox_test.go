package convert

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The measured set, pinned. Every entry here is proven by
// docs/SANDBOX-PATHS.md's "pandoc" section (2026-09-20):
// the release binary is statically linked, and the trace phase found zero
// paths outside the job directory across five format targets.
func TestPandocReadOnlySetIsTheMeasuredOne(t *testing.T) {
	if len(pandocReadOnly) != 0 {
		t.Fatalf("pandocReadOnly has %d paths, this test knows 0 -- re-measure with "+
			"scripts/sandbox-paths-measure.sh rather than editing the expectation", len(pandocReadOnly))
	}
}

func TestPandocEnginesAreTheMeasuredOnes(t *testing.T) {
	want := []string{"pandoc"}
	if len(pandocEngines) != len(want) {
		t.Fatalf("pandocEngines has %d entries, this test knows %d -- re-measure with "+
			"scripts/sandbox-paths-measure.sh rather than editing the expectation",
			len(pandocEngines), len(want))
	}
	for i := range want {
		if pandocEngines[i] != want[i] {
			t.Errorf("pandocEngines[%d] = %q, want %q", i, pandocEngines[i], want[i])
		}
	}
}

// PandocConverter stays comparable: WithSandbox(nil) equals the zero value,
// a confined converter does not, and the type is usable as a map key --
// fails to COMPILE if Sandbox stops being a pointer.
func TestPandocConverterStaysComparable(t *testing.T) {
	var a, b PandocConverter
	if a != b {
		t.Fatal("two zero converters differ")
	}
	if got := (PandocConverter{}).WithSandbox(nil); got != a {
		t.Error("WithSandbox(nil) does not equal the zero value")
	}

	confined := PandocConverter{}.WithSandbox([]string{"/usr/local/bin/pandoc-worker", "__sandbox-exec"})
	if confined == a {
		t.Error("a confined converter equals an unconfined one, so the field is not being compared at all")
	}
	seen := map[PandocConverter]bool{a: true}
	if !seen[b] {
		t.Error("the zero converter is not its own map key")
	}
}

// PandocConverter.Convert through the fake sandbox helper and a fake pandoc
// on PATH: the recorded invocation runs the resolved fake pandoc after --
// and grants rw: on the job directory (filepath.Dir(outPath)). The fake
// pandoc exits 0 writing nothing, so Convert fails at output validation --
// this test asserts on the recorded argv, not on Convert succeeding.
func TestPandocConvertRunsTheEngineThroughTheHelper(t *testing.T) {
	helper, calls := fakeSandboxHelper(t)
	dir := t.TempDir()
	fakeEngine(t, dir, "pandoc", "exit 0\n")
	pathWith(t, dir)
	enginePath, err := exec.LookPath("pandoc")
	if err != nil {
		t.Fatalf("exec.LookPath(pandoc): %v", err)
	}

	jobDir := t.TempDir()
	inPath := filepath.Join(jobDir, "in.md")
	if err := os.WriteFile(inPath, []byte("# hi\n"), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}
	outPath := filepath.Join(jobDir, "out.html")

	c := PandocConverter{}.WithSandbox(helper)
	if err := c.Convert(context.Background(), inPath, outPath, nil); err == nil {
		t.Fatal("expected an error: the fake pandoc wrote no output, so validation must fail")
	}

	invocations := calls()
	if len(invocations) != 1 {
		t.Fatalf("helper invoked %d time(s), want 1: %v", len(invocations), invocations)
	}
	argv := invocations[0]
	sep := indexOf(argv, "--")
	if sep < 0 || sep+1 >= len(argv) || argv[sep+1] != enginePath {
		t.Fatalf("argv %v does not run the resolved pandoc %q after --", argv, enginePath)
	}
	if indexOf(argv[1:sep], "rw:"+jobDir) < 0 {
		t.Errorf("argv %v missing rw:%s", argv, jobDir)
	}
}

// PandocPreflight on a zero (unconfined) converter returns nil without
// creating a preflight workspace at all -- nothing to prove for a
// deployment that has deliberately turned confinement off.
func TestPandocPreflightUnconfinedProvesNothing(t *testing.T) {
	pattern := filepath.Join(os.TempDir(), "pandoc-preflight-job-*")
	before, _ := filepath.Glob(pattern)

	c := PandocConverter{}
	if err := PandocPreflight(context.Background(), c); err != nil {
		t.Errorf("PandocPreflight on an unconfined converter: %v, want nil", err)
	}

	after, _ := filepath.Glob(pattern)
	if len(after) != len(before) {
		t.Errorf("PandocPreflight created a workspace (%v -> %v) though the converter is unconfined",
			before, after)
	}
}
