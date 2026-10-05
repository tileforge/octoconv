package convert

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func sniffBytes(b []byte, declared string) CADResult {
	head, tail, err := ReadCADWindows(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		panic(err)
	}
	return SniffCADWindows(head, tail, int64(len(b)), declared)
}

func readCADFile(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "cad", rel))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func declaredForExt(path string) string {
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	if strings.EqualFold(ext, "igs") {
		return "iges"
	}
	return NormalizeFormat(ext)
}

func binarySTLWith(header string, triangles uint32) []byte {
	b := make([]byte, 84+50*int(triangles))
	copy(b, header)
	binary.LittleEndian.PutUint32(b[80:84], triangles)
	return b
}

func TestSniffCADCorpus(t *testing.T) {
	var seen int
	for _, dir := range []string{"public", "synthetic"} {
		entries, err := os.ReadDir(filepath.Join("testdata", "cad", dir))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.IsDir() || e.Name() == "PROVENANCE.md" {
				continue
			}
			seen++
			rel := filepath.Join(dir, e.Name())
			t.Run(rel, func(t *testing.T) {
				declared := declaredForExt(e.Name())
				got := sniffBytes(readCADFile(t, rel), declared)
				if got.Format != declared || got.Rejection != CADRejectNone {
					t.Fatalf("declared %s: got %+v, want format %s without rejection", declared, got, declared)
				}
			})
		}
	}
	if seen == 0 {
		t.Fatal("corpus walk found no files")
	}
}

func TestSniffCADRejectFixtures(t *testing.T) {
	want := map[string]CADResult{
		"step_truncated.step":          {"step", CADRejectTruncated},
		"step_missing_header.step":     {"step", CADRejectTruncated},
		"step_trailing_nul.step":       {"step", CADRejectTruncated},
		"step_bom.step":                {},
		"iges_truncated.iges":          {"iges", CADRejectTruncated},
		"dxf_truncated.dxf":            {"dxf", CADRejectTruncated},
		"dxf_bom.dxf":                  {},
		"dxf_binary.dxf":               {"dxf", CADRejectBinaryDXF},
		"dwg_ac1027.dwg":               {"dwg", CADRejectDWG},
		"stl_binary_truncated.stl":     {"stl", CADRejectTruncated},
		"stl_binary_trailing_byte.stl": {"stl", CADRejectTruncated},
		"stl_ascii_truncated.stl":      {"stl", CADRejectTruncated},
		"stl_ambiguous.stl":            {"stl", CADRejectAmbiguousSTL},
		"stl_binary_ac10_header.stl":   {"stl", CADRejectNone},
		"stl_binary_empty.stl":         {"stl", CADRejectNone},
	}
	entries, err := os.ReadDir(filepath.Join("testdata", "cad", "reject"))
	if err != nil {
		t.Fatal(err)
	}
	var onDisk []string
	for _, e := range entries {
		onDisk = append(onDisk, e.Name())
	}
	sort.Strings(onDisk)
	var inTable []string
	for name := range want {
		inTable = append(inTable, name)
	}
	sort.Strings(inTable)
	if strings.Join(onDisk, ",") != strings.Join(inTable, ",") {
		t.Fatalf("reject/ listing and expectation table differ:\n disk:  %v\n table: %v", onDisk, inTable)
	}
	for name, exp := range want {
		t.Run(name, func(t *testing.T) {
			got := sniffBytes(readCADFile(t, filepath.Join("reject", name)), declaredForExt(name))
			if got != exp {
				t.Fatalf("got %+v, want %+v", got, exp)
			}
		})
	}
}

func TestSniffCADMutations(t *testing.T) {
	step := readCADFile(t, "public/LM8UU.step")
	dxf := readCADFile(t, "public/hatches_1.dxf")
	iges := readCADFile(t, "synthetic/surfaces_only.iges")
	binSTL := readCADFile(t, "public/nasa-hubble-all-pieces.stl")
	asciiSTL := readCADFile(t, "synthetic/stl_ascii_box.stl")

	type sample struct {
		name     string
		declared string
		data     []byte
	}
	all := []sample{
		{"step", "step", step},
		{"dxf", "dxf", dxf},
		{"iges", "iges", iges},
		{"stl_binary", "stl", binSTL},
		{"stl_ascii", "stl", asciiSTL},
	}
	text := []sample{all[0], all[1], all[2], all[4]}

	for _, s := range all {
		for _, cut := range []int{17, 81, len(s.data) / 2, 5000} {
			if cut >= len(s.data)-84 {
				continue
			}
			name := s.name + "/cut"
			t.Run(name, func(t *testing.T) {
				got := sniffBytes(s.data[:len(s.data)-cut], s.declared)
				if got.Format != s.declared || got.Rejection != CADRejectTruncated {
					t.Fatalf("cut %d: got %+v", cut, got)
				}
			})
		}
	}

	accepted := func(t *testing.T, label string, b []byte, declared string) {
		t.Helper()
		got := sniffBytes(b, declared)
		if got.Format != declared || got.Rejection != CADRejectNone {
			t.Fatalf("%s: got %+v, want accepted %s", label, got, declared)
		}
	}
	rejected := func(t *testing.T, label string, b []byte, declared string) {
		t.Helper()
		got := sniffBytes(b, declared)
		if got.Format != declared || got.Rejection != CADRejectTruncated {
			t.Fatalf("%s: got %+v, want truncated %s", label, got, declared)
		}
	}

	t.Run("final newline only", func(t *testing.T) {
		accepted(t, "step", bytes.TrimRight(step, "\r\n"), "step")
		accepted(t, "dxf", bytes.TrimRight(dxf, "\r\n"), "dxf")
	})

	t.Run("iges line endings", func(t *testing.T) {
		crlf := bytes.ReplaceAll(iges, []byte("\n"), []byte("\r\n"))
		accepted(t, "crlf", crlf, "iges")
		accepted(t, "crlf without final newline", bytes.TrimRight(crlf, "\r\n"), "iges")
		accepted(t, "lf without final newline", bytes.TrimRight(iges, "\n"), "iges")
	})

	t.Run("trailing whitespace", func(t *testing.T) {
		for _, s := range text {
			accepted(t, s.name, append(append([]byte{}, s.data...), []byte("  \r\n \t\n")...), s.declared)
		}
	})

	t.Run("trailing nul", func(t *testing.T) {
		for _, s := range text {
			for _, n := range []int{1, 512} {
				rejected(t, s.name, append(append([]byte{}, s.data...), make([]byte, n)...), s.declared)
			}
		}
	})

	t.Run("trailing nul on the endsolid line", func(t *testing.T) {
		for _, n := range []int{1, 512} {
			b := append(append([]byte{}, bytes.TrimRight(asciiSTL, "\n")...), make([]byte, n)...)
			rejected(t, "ascii stl without final newline", b, "stl")
		}
	})

	t.Run("binary stl count plus one", func(t *testing.T) {
		b := append([]byte{}, binSTL...)
		binary.LittleEndian.PutUint32(b[80:84], binary.LittleEndian.Uint32(b[80:84])+1)
		rejected(t, "count+1", b, "stl")
	})

	t.Run("ascii stl endsolid forms", func(t *testing.T) {
		nameless := bytes.Replace(asciiSTL, []byte("endsolid box"), []byte("endsolid"), 1)
		if bytes.Equal(nameless, asciiSTL) {
			t.Fatal("fixture no longer ends with a named endsolid")
		}
		accepted(t, "nameless", nameless, "stl")
		accepted(t, "upper", bytes.ToUpper(asciiSTL), "stl")
	})
}

func TestSniffCADGarbage(t *testing.T) {
	garbage := bytes.Repeat([]byte("A"), 1000)
	for _, declared := range []string{"dxf", "step", "iges"} {
		if got := sniffBytes(garbage, declared); got != (CADResult{}) {
			t.Fatalf("garbage declared %s: got %+v, want no verdict", declared, got)
		}
	}
	if got := sniffBytes(garbage, "stl"); got != (CADResult{"stl", CADRejectTruncated}) {
		t.Fatalf("garbage declared stl: got %+v", got)
	}
	if got := sniffBytes(bytes.Repeat([]byte("A"), 83), "stl"); got != (CADResult{}) {
		t.Fatalf("83 bytes declared stl: got %+v, want no verdict", got)
	}
}

func TestSniffCADBinarySTLHeaderPrecedence(t *testing.T) {
	igesRecord := strings.Repeat(" ", 72) + "S      1"
	headers := map[string]string{
		"dwg signature":  "AC1027 bracket",
		"step magic":     "ISO-10303-21;",
		"dxf section":    "  0\nSECTION\n",
		"iges s record":  igesRecord,
		"binary dxf":     "AutoCAD Binary DXF\r\n\x1a\x00",
		"all upper dwg":  "AC1015",
		"leading blanks": "  ISO-10303-21;",
	}
	for name, header := range headers {
		t.Run(name, func(t *testing.T) {
			got := sniffBytes(binarySTLWith(header, 12), "stl")
			if got != (CADResult{"stl", CADRejectNone}) {
				t.Fatalf("got %+v, want stl accepted", got)
			}
		})
	}
	if got := sniffBytes(binarySTLWith("AC1027 bracket", 12), "step"); got != (CADResult{"dwg", CADRejectDWG}) {
		t.Fatalf("AC1027 header declared step: got %+v, want dwg", got)
	}
}

func TestSniffCADDWG(t *testing.T) {
	for _, sig := range []string{"AC1002", "AC1015", "AC1032"} {
		b := append([]byte(sig), make([]byte, 122)...)
		if got := sniffBytes(b, "dwg"); got != (CADResult{"dwg", CADRejectDWG}) {
			t.Fatalf("%s: got %+v", sig, got)
		}
	}
	b := append([]byte("AC1.40"), make([]byte, 122)...)
	if got := sniffBytes(b, "dwg"); got != (CADResult{}) {
		t.Fatalf("AC1.40: got %+v, want no verdict", got)
	}
}

type recordingReaderAt struct {
	r    io.ReaderAt
	reqs []struct{ off, n int64 }
}

func (r *recordingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	r.reqs = append(r.reqs, struct{ off, n int64 }{off, int64(len(p))})
	return r.r.ReadAt(p, off)
}

func TestSniffCADBoundedReads(t *testing.T) {
	const size = 10 << 20
	rec := &recordingReaderAt{r: bytes.NewReader(make([]byte, size))}
	head, tail, err := ReadCADWindows(rec, size)
	if err != nil {
		t.Fatal(err)
	}
	if len(head) != CADHeadBytes || len(tail) != CADTailBytes {
		t.Fatalf("window sizes: head %d tail %d", len(head), len(tail))
	}
	var total int64
	offsets := map[int64]bool{}
	for _, q := range rec.reqs {
		total += q.n
		offsets[q.off] = true
	}
	if total > CADHeadBytes+CADTailBytes {
		t.Fatalf("requested %d bytes, want at most %d", total, CADHeadBytes+CADTailBytes)
	}
	if !offsets[0] || !offsets[size-CADTailBytes] || len(offsets) != 2 {
		t.Fatalf("offsets %v, want exactly 0 and %d", offsets, size-CADTailBytes)
	}

	small := bytes.Repeat([]byte("x"), 100)
	head, tail, err = ReadCADWindows(bytes.NewReader(small), 100)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(head, small) || !bytes.Equal(tail, small) {
		t.Fatalf("100-byte file: head %d tail %d, want both the whole file", len(head), len(tail))
	}
}

func TestSniffCADAllRejections(t *testing.T) {
	got := AllCADRejections()
	want := []CADRejection{CADRejectTruncated, CADRejectAmbiguousSTL, CADRejectBinaryDXF, CADRejectDWG}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] || got[i] == CADRejectNone {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	got[0] = CADRejectNone
	if AllCADRejections()[0] != CADRejectTruncated {
		t.Fatal("AllCADRejections returned shared storage")
	}
}
