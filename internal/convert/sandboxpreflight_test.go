package convert

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequireOutputMissingFileErrors(t *testing.T) {
	dir := t.TempDir()
	if err := requireOutput(filepath.Join(dir, "missing"), nil); err == nil {
		t.Error("expected an error for a file that does not exist")
	}
}

func TestRequireOutputEmptyFileErrors(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "empty")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := requireOutput(p, nil); err == nil {
		t.Error("expected an error for an empty file")
	}
}

func TestRequireOutputWithoutWantErrors(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "out")
	if err := os.WriteFile(p, []byte("something else entirely"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := requireOutput(p, []byte("the-marker")); err == nil {
		t.Error("expected an error when the file does not contain the wanted marker")
	}
}

func TestRequireOutputContainingWantIsNil(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "out")
	if err := os.WriteFile(p, []byte("prefix the-marker suffix"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := requireOutput(p, []byte("the-marker")); err != nil {
		t.Errorf("requireOutput: %v", err)
	}
}

func TestRequireDeniedNilErrorErrors(t *testing.T) {
	dir := t.TempDir()
	if err := requireDenied(nil, filepath.Join(dir, "out"), nil); err == nil {
		t.Error("a nil conversion error means confinement did not deny anything -- expected an error")
	}
}

func TestRequireDeniedOutputCarryingMarkerErrors(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "out")
	if err := os.WriteFile(p, []byte("leaked the-marker anyway"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	convErr := os.ErrPermission // any non-nil error stands in for a real conversion failure
	if err := requireDenied(convErr, p, []byte("the-marker")); err == nil {
		t.Error("expected an error: the output carries the marker from the supposedly-denied file")
	}
}

func TestRequireDeniedNoMarkerNonEmptyOutputErrors(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "out")
	if err := os.WriteFile(p, []byte("some bytes"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	convErr := os.ErrPermission
	if err := requireDenied(convErr, p, nil); err == nil {
		t.Error("expected an error: a denied conversion still produced non-empty output")
	}
}

func TestRequireDeniedNoOutputIsNil(t *testing.T) {
	dir := t.TempDir()
	convErr := os.ErrPermission
	if err := requireDenied(convErr, filepath.Join(dir, "never-written"), []byte("marker")); err != nil {
		t.Errorf("requireDenied: %v, want nil -- a clean denial with no output at all", err)
	}
}

// place writes identical bytes to both the job and the outside directories,
// and the outside directory is never a descendant of the job directory --
// the two must be genuine siblings for a denial to say anything about
// location.
func TestPreflightWorkspacePlaceWritesIdenticalBytesToBothDirs(t *testing.T) {
	w, cleanup, err := newPreflightWorkspace("test")
	if err != nil {
		t.Fatalf("newPreflightWorkspace: %v", err)
	}
	defer cleanup()

	if strings.HasPrefix(w.outsideDir, w.jobDir+string(os.PathSeparator)) || w.outsideDir == w.jobDir {
		t.Fatalf("outsideDir %q is not a sibling of jobDir %q", w.outsideDir, w.jobDir)
	}

	data := []byte("preflight fixture bytes")
	inside, outside, err := w.place("fixture.txt", data)
	if err != nil {
		t.Fatalf("place: %v", err)
	}

	got, err := os.ReadFile(inside)
	if err != nil || string(got) != string(data) {
		t.Errorf("inside fixture = %q, %v; want %q, nil", got, err, data)
	}
	got, err = os.ReadFile(outside)
	if err != nil || string(got) != string(data) {
		t.Errorf("outside fixture = %q, %v; want %q, nil", got, err, data)
	}
}

// requireAbsent is the marker judgment on its own -- html's own negative
// preflight run cannot go through requireDenied's convErr check (chromium
// exits 0 and writes a blank PDF when denied, D-06), so this is what
// HTMLPreflight judges the effectiveness run with.
func TestRequireAbsent(t *testing.T) {
	dir := t.TempDir()

	if err := requireAbsent(filepath.Join(dir, "missing"), []byte("marker")); err != nil {
		t.Errorf("missing file: %v, want nil (a clean denial)", err)
	}

	clean := filepath.Join(dir, "clean")
	if err := os.WriteFile(clean, []byte("nothing of interest here"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := requireAbsent(clean, []byte("the-marker")); err != nil {
		t.Errorf("file without marker: %v, want nil", err)
	}

	leaked := filepath.Join(dir, "leaked")
	if err := os.WriteFile(leaked, []byte("leaked the-marker anyway"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := requireAbsent(leaked, []byte("the-marker")); err == nil {
		t.Error("file carrying the marker: expected an error")
	}

	if err := requireAbsent(filepath.Join(dir, "irrelevant"), nil); err == nil {
		t.Error("nil marker: expected an error -- a denial judged without one proves nothing")
	}
}

// newPreflightWorkspaceOutside places the outside directory under the given
// base rather than always os.TempDir() -- html's own grant includes /tmp, so
// its preflight needs an outside location its ruleset does not also read.
func TestPreflightWorkspaceOutsideBase(t *testing.T) {
	base := t.TempDir()
	w, cleanup, err := newPreflightWorkspaceOutside("testclass", base)
	if err != nil {
		t.Fatalf("newPreflightWorkspaceOutside: %v", err)
	}

	if !strings.HasPrefix(w.outsideDir, base) {
		t.Errorf("outsideDir %q is not under base %q", w.outsideDir, base)
	}
	if strings.HasPrefix(w.jobDir, base) {
		t.Errorf("jobDir %q unexpectedly landed under base %q -- only outsideDir should move", w.jobDir, base)
	}
	if _, err := os.Stat(w.jobDir); err != nil {
		t.Errorf("jobDir does not exist: %v", err)
	}
	if _, err := os.Stat(w.outsideDir); err != nil {
		t.Errorf("outsideDir does not exist: %v", err)
	}

	cleanup()
	if _, err := os.Stat(w.jobDir); !os.IsNotExist(err) {
		t.Error("cleanup did not remove jobDir")
	}
	if _, err := os.Stat(w.outsideDir); !os.IsNotExist(err) {
		t.Error("cleanup did not remove outsideDir")
	}
}

// requireOutsideGrants proves the workspace's outside directory is not
// itself readable under any granted set -- equal to or beneath a grant is an
// error; a sibling that merely shares a string prefix, or an unrelated path,
// is not.
func TestRequireOutsideGrants(t *testing.T) {
	cases := []struct {
		name    string
		outside string
		granted []string
		wantErr bool
	}{
		{"equal to a granted path", "/a/b", []string{"/a/b"}, true},
		{"beneath a granted path", "/a/b/c", []string{"/a/b"}, true},
		{"sibling sharing a mere string prefix", "/a/bc", []string{"/a/b"}, false},
		{"unrelated", "/x/y", []string{"/a/b"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := preflightWorkspace{outsideDir: c.outside}
			err := w.requireOutsideGrants(c.granted)
			if c.wantErr && err == nil {
				t.Error("expected an error")
			}
			if !c.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestPreflightMarkerIsUniqueAndPrefixed(t *testing.T) {
	a, err := preflightMarker()
	if err != nil {
		t.Fatalf("preflightMarker: %v", err)
	}
	b, err := preflightMarker()
	if err != nil {
		t.Fatalf("preflightMarker: %v", err)
	}
	if !strings.HasPrefix(a, "octoconv-preflight-") {
		t.Errorf("marker %q missing the octoconv-preflight- prefix", a)
	}
	if a == b {
		t.Errorf("two markers were identical: %q", a)
	}
}
