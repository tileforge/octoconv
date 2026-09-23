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
