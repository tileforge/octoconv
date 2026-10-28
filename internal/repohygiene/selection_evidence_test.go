package repohygiene_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const selectionEvidenceDir = "testdata/v1.16-selection"

func TestSelectionEvidenceCopiesMatchTheirSources(t *testing.T) {
	sources, err := os.ReadFile(filepath.Join(selectionEvidenceDir, "SOURCES.txt"))
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for n, line := range strings.Split(string(sources), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := map[string]string{}
		for _, part := range strings.Split(line, ",") {
			key, value, ok := strings.Cut(part, "=")
			if !ok || key == "" || value == "" {
				t.Fatalf("SOURCES.txt line %d: malformed field %q", n+1, part)
			}
			if _, dup := fields[key]; dup {
				t.Fatalf("SOURCES.txt line %d: duplicate field %q", n+1, key)
			}
			fields[key] = value
		}
		for _, key := range []string{"file", "bytes", "sha256", "origin", "origin_commit"} {
			if fields[key] == "" {
				t.Fatalf("SOURCES.txt line %d: missing %s", n+1, key)
			}
		}
		name := fields["file"]
		if name != filepath.Base(name) || name == "SOURCES.txt" {
			t.Fatalf("SOURCES.txt line %d: file %q is not a copy in %s", n+1, name, selectionEvidenceDir)
		}
		if listed[name] {
			t.Fatalf("SOURCES.txt line %d: %s listed twice", n+1, name)
		}
		listed[name] = true
		data, err := os.ReadFile(filepath.Join(selectionEvidenceDir, name))
		if err != nil {
			t.Fatalf("SOURCES.txt line %d: %v", n+1, err)
		}
		want, err := strconv.Atoi(fields["bytes"])
		if err != nil {
			t.Fatalf("SOURCES.txt line %d: bytes %q: %v", n+1, fields["bytes"], err)
		}
		if len(data) != want {
			t.Fatalf("%s has %d bytes, SOURCES.txt says %d", name, len(data), want)
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != fields["sha256"] {
			t.Fatalf("%s has sha256 %s, SOURCES.txt says %s", name, got, fields["sha256"])
		}
	}
	if len(listed) < 3 {
		t.Fatalf("SOURCES.txt lists %d copies, want at least 3", len(listed))
	}
	entries, err := os.ReadDir(selectionEvidenceDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == "SOURCES.txt" {
			continue
		}
		if !listed[e.Name()] {
			t.Fatalf("%s/%s is not listed in SOURCES.txt", selectionEvidenceDir, e.Name())
		}
	}
}
