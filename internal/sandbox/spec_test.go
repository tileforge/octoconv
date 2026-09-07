package sandbox

import (
	"slices"
	"testing"
)

// The two ends of this format live in different binaries -- convert renders it,
// cmd/tectonic-worker's sandbox-exec mode reads it -- and nothing but a
// round-trip holds them equal.

func TestSpecRoundTrips(t *testing.T) {
	want := Paths{
		ReadWrite: []string{"/tmp/job-1", "/tmp"},
		ReadOnly:  []string{"/opt/tectonic/octoconv-bundle.ttb", "/usr", "/var/cache/tectonic"},
	}
	cmd := []string{"/usr/local/bin/tectonic", "-X", "compile", "--untrusted", "/tmp/job-1/in.tex"}

	got, gotCmd, err := ParseSpec(append(append(want.Spec(), "--"), cmd...))
	if err != nil {
		t.Fatalf("ParseSpec on Spec's own output: %v", err)
	}
	if !slices.Equal(got.ReadWrite, want.ReadWrite) {
		t.Errorf("ReadWrite = %v, want %v", got.ReadWrite, want.ReadWrite)
	}
	if !slices.Equal(got.ReadOnly, want.ReadOnly) {
		t.Errorf("ReadOnly = %v, want %v", got.ReadOnly, want.ReadOnly)
	}
	if !slices.Equal(gotCmd, cmd) {
		t.Errorf("command = %v, want %v", gotCmd, cmd)
	}
}

// A path is never granted MORE than it was rendered with. The two prefixes are
// three characters apart and the parse is a prefix match, which is exactly the
// shape that silently promotes read-only to writable if it is ever loosened.
func TestParseSpecDoesNotPromoteReadOnlyToWritable(t *testing.T) {
	p, _, err := ParseSpec([]string{"ro:/opt/tectonic", "--", "/bin/true"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.ReadWrite) != 0 {
		t.Fatalf("a ro: token produced ReadWrite = %v", p.ReadWrite)
	}
}

// An unrecognised token is refused, not skipped. Skipping drops a rule, and a
// dropped rule is a directory the engine cannot read -- which arrives as an
// engine error nobody can trace back to here.
func TestParseSpecRefusesUnknownTokens(t *testing.T) {
	if _, _, err := ParseSpec([]string{"rw:/tmp", "x:/etc", "--", "/bin/true"}); err == nil {
		t.Fatal("ParseSpec accepted an unrecognised token; the rule it named was silently dropped")
	}
}

// No command means there is nothing to confine, and execing nothing would
// leave the caller to decide what to do next. It must not be able to conclude
// "sandbox applied".
func TestParseSpecRequiresACommand(t *testing.T) {
	for _, argv := range [][]string{{}, {"rw:/tmp"}, {"rw:/tmp", "--"}} {
		if _, _, err := ParseSpec(argv); err == nil {
			t.Fatalf("ParseSpec(%v) returned no error but has no command to exec", argv)
		}
	}
}
