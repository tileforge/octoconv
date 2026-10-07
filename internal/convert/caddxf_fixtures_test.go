package convert

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var dxfFixtureExpectedRejections = map[string]CADRejection{}

func TestDXFFixturesSniffAsDXF(t *testing.T) {
	dir := filepath.Join("testdata", "cad", "dxf")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".dxf") {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		t.Fatalf("no .dxf fixtures in %s", dir)
	}
	sort.Strings(names)
	seen := map[string]bool{}
	for _, name := range names {
		seen[name] = true
		t.Run(name, func(t *testing.T) {
			f, err := os.Open(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			st, err := f.Stat()
			if err != nil {
				t.Fatal(err)
			}
			head, tail, err := ReadCADWindows(f, st.Size())
			if err != nil {
				t.Fatal(err)
			}
			res := SniffCADWindows(head, tail, st.Size(), "dxf")
			if res.Format != "dxf" {
				t.Fatalf("sniffed format %q, want dxf", res.Format)
			}
			if want, ok := dxfFixtureExpectedRejections[name]; ok {
				if res.Rejection != want {
					t.Fatalf("rejection %q, want %q", res.Rejection, want)
				}
				return
			}
			if res.Rejection != "" {
				t.Fatalf("rejected at create time as %q", res.Rejection)
			}
		})
	}
	for name := range dxfFixtureExpectedRejections {
		if !seen[name] {
			t.Errorf("expected-rejection entry %s names no fixture", name)
		}
	}
}
