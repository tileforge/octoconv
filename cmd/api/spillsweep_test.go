package main

import (
	"bytes"
	"errors"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSweepMultipartSpillRemovesWhatParseMultipartFormSpills(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for _, part := range []struct{ field, name string }{{"file", "a.bin"}, {"file2", "b.bin"}} {
		fw, err := mw.CreateFormFile(part.field, part.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(bytes.Repeat([]byte{'x'}, 64<<10)); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.WriteField("target", "png"); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/jobs", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if err := req.ParseMultipartForm(1 << 10); err != nil {
		t.Fatalf("ParseMultipartForm: %v", err)
	}

	spilled, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(spilled) == 0 {
		t.Fatalf("ParseMultipartForm spilled nothing into TMPDIR")
	}

	removed, err := sweepMultipartSpill(dir)
	if err != nil {
		t.Fatalf("sweepMultipartSpill: %v", err)
	}
	if removed != len(spilled) {
		t.Fatalf("removed %d files, ParseMultipartForm spilled %d", removed, len(spilled))
	}
	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		names := make([]string, 0, len(left))
		for _, e := range left {
			names = append(names, e.Name())
		}
		t.Fatalf("spill files left after the sweep: %v", names)
	}
}

func TestSweepMultipartSpillLeavesEverythingElse(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()

	removedNames := []string{"multipart-1", "multipart-4294967295"}
	keptNames := []string{"multipart-", "multipart-12a", "multipart-notes.txt", "xmultipart-5", "keep.txt"}
	for _, name := range append(append([]string{}, removedNames...), keptNames...) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("spill"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "multipart-77"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "multipart-77", "multipart-88"), []byte("nested"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(outside, "target")
	if err := os.WriteFile(target, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "multipart-99")); err != nil {
		t.Fatal(err)
	}

	removed, err := sweepMultipartSpill(dir)
	if err != nil {
		t.Fatalf("sweepMultipartSpill: %v", err)
	}
	if removed != len(removedNames) {
		t.Fatalf("removed %d files, want %d", removed, len(removedNames))
	}

	for _, name := range removedNames {
		if _, err := os.Lstat(filepath.Join(dir, name)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s: want removed, Lstat err = %v", name, err)
		}
	}
	for _, name := range append(append([]string{}, keptNames...), "multipart-77", filepath.Join("multipart-77", "multipart-88"), "multipart-99") {
		if _, err := os.Lstat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s: want kept, Lstat err = %v", name, err)
		}
	}
	info, err := os.Lstat(filepath.Join(dir, "multipart-99"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		t.Errorf("multipart-99 is no longer a symlink: mode %v", info.Mode())
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("symlink target: %v", err)
	}
	if string(got) != "payload" {
		t.Errorf("symlink target holds %q, want %q", got, "payload")
	}
}

func TestSweepMultipartSpillReportsAMissingDir(t *testing.T) {
	removed, err := sweepMultipartSpill(filepath.Join(t.TempDir(), "absent"))
	if removed != 0 {
		t.Errorf("removed %d files from a missing dir", removed)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("want an error matching fs.ErrNotExist, got %v", err)
	}
}

func TestSweepMultipartSpillReportsEveryFailedRemove(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory write permission")
	}
	dir := t.TempDir()
	for _, name := range []string{"multipart-1", "multipart-2"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("spill"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Error(err)
		}
	})

	removed, err := sweepMultipartSpill(dir)
	if removed != 0 {
		t.Errorf("removed %d files from a read-only dir", removed)
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("want an error matching fs.ErrPermission, got %v", err)
	}
	for _, name := range []string{"multipart-1", "multipart-2"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error does not name %s: %v", name, err)
		}
		if _, statErr := os.Lstat(filepath.Join(dir, name)); statErr != nil {
			t.Errorf("%s: want kept, Lstat err = %v", name, statErr)
		}
	}
}

func TestMainSweepsSpillBeforeAnyListener(t *testing.T) {
	src := readMainSource(t)

	if n := strings.Count(src, "sweepMultipartSpill("); n != 1 {
		t.Fatalf("main.go calls sweepMultipartSpill %d times, want 1", n)
	}
	decl := strings.Index(src, "spillDir := os.TempDir()")
	call := strings.Index(src, "sweepMultipartSpill(spillDir)")
	if decl < 0 || call < 0 {
		t.Fatalf("main.go must declare spillDir := os.TempDir() and call sweepMultipartSpill(spillDir): decl=%d call=%d", decl, call)
	}
	if decl > call {
		t.Fatalf("spillDir is declared after the sweep call")
	}

	connect := strings.Index(src, "db.Connect(ctx)")
	if connect < 0 {
		t.Fatalf("main.go no longer calls db.Connect(ctx)")
	}
	for _, marker := range []string{"db.Connect(ctx)", "api.NewHTTPServer(", ".ListenAndServe()"} {
		at := strings.Index(src, marker)
		if at < 0 {
			t.Fatalf("main.go no longer contains %s", marker)
		}
		if call > at {
			t.Errorf("the sweep runs after %s", marker)
		}
	}

	if call < connect {
		region := src[call:connect]
		if n := strings.Count(region, "log.Printf("); n != 2 {
			t.Errorf("the sweep has %d log.Printf calls before db.Connect, want 2", n)
		}
		if strings.Contains(region, "Fatal") {
			t.Errorf("a sweep failure must not stop startup")
		}
	}
}
