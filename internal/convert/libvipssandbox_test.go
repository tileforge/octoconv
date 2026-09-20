package convert

import (
	"path/filepath"
	"strings"
	"testing"
)

// The spec is the contract between the converter and the helper, and it is
// assembled from strings. These tests pin the two things a reader cannot see
// by looking at either side alone: that the engine's own binary is granted,
// and that nothing but the job's directory is writable.

func TestLibvipsSpecGrantsTheEngineItsOwnBinary(t *testing.T) {
	got := libvipsSpec("/job/in.png", "/job/out.webp", "/usr/bin/vips")

	if indexOf(got, "ro:/usr/bin/vips") < 0 {
		t.Errorf("the ruleset does not grant the engine its own executable, so every conversion "+
			"would fail with exec: permission denied before reading anything: %v", got)
	}
}

func TestLibvipsSpecWritesOnlyTheJobDirectory(t *testing.T) {
	got := libvipsSpec("/job/in.png", "/job/out.webp", "/usr/bin/vips")

	var writable []string
	for _, tok := range got {
		if strings.HasPrefix(tok, "rw:") {
			writable = append(writable, strings.TrimPrefix(tok, "rw:"))
		}
	}
	if len(writable) != 1 || writable[0] != "/job" {
		t.Errorf("writable paths are %v, want exactly [/job] -- a second writable path is a way "+
			"out of the job's own directory", writable)
	}
}

// An input somewhere other than the output directory has to be granted too,
// and read-only. The worker materialises both in one directory today, so this
// is the branch nothing else exercises.
func TestLibvipsSpecGrantsASeparateInputDirectoryReadOnly(t *testing.T) {
	got := libvipsSpec("/uploads/in.png", "/job/out.webp", "/usr/bin/vips")

	if indexOf(got, "ro:/uploads") < 0 {
		t.Errorf("an input outside the output directory is not granted, so the engine could not "+
			"read it: %v", got)
	}
	if indexOf(got, "rw:/uploads") >= 0 {
		t.Errorf("the input directory is granted WRITABLE, which the engine never needs: %v", got)
	}
}

// The measured set, pinned. Every path here was observed during a real
// conversion and then proven necessary by running five format pairs under a
// ruleset built from this list (.planning/research/SANDBOX-PATHS.md). Removing
// one without re-measuring is how a worker starts failing every job.
func TestLibvipsReadOnlySetIsTheMeasuredOne(t *testing.T) {
	want := []string{
		"/lib",
		"/usr/lib",
		"/etc/ld.so.cache",
		"/proc/self/auxv",
		"/proc/self/status",
		"/proc/filesystems",
		"/proc/cpuinfo",
		"/sys/devices/system/cpu/online",
		"/sys/devices/system/cpu/possible",
	}
	if len(libvipsReadOnly) != len(want) {
		t.Fatalf("the measured set has %d paths, this test knows %d -- re-measure with "+
			"scripts/sandbox-paths-measure.sh rather than editing the expectation",
			len(libvipsReadOnly), len(want))
	}
	for i := range want {
		if libvipsReadOnly[i] != want[i] {
			t.Errorf("path %d is %q, want %q", i, libvipsReadOnly[i], want[i])
		}
	}
	for _, p := range libvipsReadOnly {
		if !filepath.IsAbs(p) {
			t.Errorf("%q is not absolute; the helper grants paths verbatim", p)
		}
	}
}

// wrapEngine is what turns a spec into an argv, and the order matters: the
// helper reads its own flags, then the spec, then "--", then the engine.
func TestWrapEnginePutsTheSpecBeforeTheSeparator(t *testing.T) {
	s := engineSandbox{Helper: []string{"/usr/local/bin/worker", "__sandbox-exec"}, Confine: true}

	name, args, err := s.wrapEngine("sh", []string{"rw:/job", "ro:/lib"}, []string{"-c", "true"})
	if err != nil {
		t.Fatalf("wrapEngine: %v", err)
	}
	if name != "/usr/local/bin/worker" {
		t.Errorf("command is %q, want the helper", name)
	}
	sep := indexOf(args, "--")
	if sep < 0 {
		t.Fatalf("no -- separator in %v", args)
	}
	for _, tok := range []string{"rw:/job", "ro:/lib"} {
		if i := indexOf(args, tok); i < 0 || i > sep {
			t.Errorf("%q is at %d and the separator at %d; the helper would read it as an "+
				"engine argument", tok, i, sep)
		}
	}
	if i := indexOf(args, "-c"); i < sep {
		t.Errorf("the engine's own arguments appear before --, where the helper would try to "+
			"parse them as spec tokens: %v", args)
	}
}

// With no helper the engine is executed directly, and the spec must not leak
// into its argv -- a converter built by a test has no sandbox, and vips would
// be handed "rw:/job" as a filename.
func TestWrapEngineWithoutAHelperPassesArgumentsThrough(t *testing.T) {
	var s engineSandbox

	name, args, err := s.wrapEngine("sh", []string{"rw:/job"}, []string{"-c", "true"})
	if err != nil {
		t.Fatalf("wrapEngine: %v", err)
	}
	if !strings.HasSuffix(name, "/sh") {
		t.Errorf("command is %q, want a resolved path to sh", name)
	}
	if len(args) != 2 || args[0] != "-c" {
		t.Errorf("args are %v, want the engine's own arguments untouched", args)
	}
}

func indexOf(ss []string, want string) int {
	for i, s := range ss {
		if s == want {
			return i
		}
	}
	return -1
}
