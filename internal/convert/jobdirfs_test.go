package convert

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func jobDirOutside(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func jobDirUntouched(t *testing.T, outside, want string) {
	t.Helper()
	got, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("the outside file changed: %q, want %q", got, want)
	}
}

func jobDirNewFile(t *testing.T, path, body string, perm fs.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("%s is %v, want a regular file", path, info.Mode().Type())
	}
	if perm != 0 && info.Mode().Perm() != perm {
		t.Errorf("%s mode = %v, want %v", path, info.Mode().Perm(), perm)
	}
	if body != "" {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != body {
			t.Errorf("%s = %q, want %q", path, got, body)
		}
	}
}

func TestWriteNewFileNeverWritesThroughAPlantedLink(t *testing.T) {
	t.Run("a symlink", func(t *testing.T) {
		outside := jobDirOutside(t, "untouched")
		path := filepath.Join(t.TempDir(), "out")
		if err := os.Symlink(outside, path); err != nil {
			t.Fatal(err)
		}
		if err := writeNewFile(path, []byte("body"), 0o600); err != nil {
			t.Fatalf("writeNewFile: %v", err)
		}
		jobDirUntouched(t, outside, "untouched")
		jobDirNewFile(t, path, "body", 0o600)
	})
	t.Run("a hard link", func(t *testing.T) {
		outside := jobDirOutside(t, "untouched")
		path := filepath.Join(t.TempDir(), "out")
		if err := os.Link(outside, path); err != nil {
			t.Fatal(err)
		}
		if err := writeNewFile(path, []byte("body"), 0o600); err != nil {
			t.Fatalf("writeNewFile: %v", err)
		}
		jobDirUntouched(t, outside, "untouched")
		jobDirNewFile(t, path, "body", 0o600)
	})
	for _, perm := range []fs.FileMode{0o600, 0o644} {
		t.Run(fmt.Sprintf("a stale regular file at %#o", perm), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "out")
			if err := os.WriteFile(path, []byte("stale and longer"), 0o666); err != nil {
				t.Fatal(err)
			}
			if err := writeNewFile(path, []byte("body"), perm); err != nil {
				t.Fatalf("writeNewFile: %v", err)
			}
			jobDirNewFile(t, path, "body", perm)
		})
	}
	t.Run("a non-empty directory", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "out")
		if err := os.MkdirAll(filepath.Join(path, "inner"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := writeNewFile(path, []byte("body"), 0o600); err == nil {
			t.Fatal("writeNewFile over a non-empty directory succeeded")
		}
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() {
			t.Errorf("the directory is gone: %v, %v", info, err)
		}
	})
}
