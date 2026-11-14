package inspect

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"strconv"
	"testing"
	"unsafe"
)

func TestSTLMeshFindings(t *testing.T) {
	cases := []struct {
		name string
		tris [][3]int
		want []Code
	}{
		{"triangle", [][3]int{{0, 1, 2}}, []Code{CodeSTLOpen}},
		{"same_wind", [][3]int{{0, 1, 2}, {0, 1, 3}}, []Code{CodeSTLOpen, CodeSTLUnoriented}},
		{"fan", [][3]int{{0, 1, 2}, {0, 1, 3}, {0, 1, 4}}, []Code{CodeSTLOpen, CodeSTLNonManifold}},
		{"tet", [][3]int{{0, 1, 2}, {0, 2, 3}, {0, 3, 1}, {1, 3, 2}}, nil},
		{"tet_reversed", [][3]int{{0, 2, 1}, {0, 3, 2}, {0, 1, 3}, {1, 2, 3}}, nil},
		{"degenerate", [][3]int{{0, 0, 1}}, nil},
		{"mixed", [][3]int{{0, 1, 2}, {0, 1, 3}, {4, 4, 5}}, []Code{CodeSTLOpen, CodeSTLUnoriented}},
		{"open_touching_deg", [][3]int{{0, 1, 2}, {0, 0, 1}}, []Code{CodeSTLOpen}},
		{"empty", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := asciiSTL(tc.tris)
			res := AnalyzeSTL(bytes.NewReader(buf), int64(len(buf)), int64(len(buf)), nil)
			assertSTLResult(t, res, tc.want)
		})
	}
}

func TestSTLNegativeZeroWelds(t *testing.T) {
	buf := binarySTL([][3][3]float32{
		{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}},
		{{-0, 0, 0}, {1, 0, 0}, {0, 0, 1}},
	})
	res := AnalyzeSTL(bytes.NewReader(buf), int64(len(buf)), int64(len(buf)), nil)
	assertSTLResult(t, res, []Code{CodeSTLOpen, CodeSTLUnoriented})
}

func TestSTLNaNDoesNotWeld(t *testing.T) {
	nan := math.Float32frombits(0x7fc00000)
	buf := binarySTL([][3][3]float32{{{nan, 0, 0}, {nan, 0, 0}, {1, 0, 0}}})
	res := AnalyzeSTL(bytes.NewReader(buf), int64(len(buf)), int64(len(buf)), nil)
	assertSTLResult(t, res, []Code{CodeSTLOpen})
}

func TestSTLCornerSize(t *testing.T) {
	if unsafe.Sizeof(stlCorner{}) != 32 {
		t.Fatalf("stlCorner = %d bytes, want 32", unsafe.Sizeof(stlCorner{}))
	}
}

func TestSTLOverCensusCapIsUnknown(t *testing.T) {
	count := stlCensusTriangles + 1
	buf := make([]byte, 84+50*count)
	binary.LittleEndian.PutUint32(buf[80:84], uint32(count))
	tr := NewTracker(Budget{MaxBytes: int64(len(buf)), MaxDuration: 1<<63 - 1})
	res := AnalyzeSTL(bytes.NewReader(buf), int64(len(buf)), int64(len(buf)), tr)
	if tr.ExhaustedReason() != ReasonWorkCeilingBytes {
		t.Fatalf("exhausted = %q, want %q", tr.ExhaustedReason(), ReasonWorkCeilingBytes)
	}
	for _, code := range stlCodes {
		assertCheckReason(t, res, code, ReasonWorkCeilingBytes)
	}
}

func TestSTLSolidNamedVertexIsOpen(t *testing.T) {
	tris := [][3]int{{0, 1, 2}}
	for _, name := range []string{"vertex", "myvertex"} {
		t.Run(name, func(t *testing.T) {
			buf := asciiSTLNamed(name, tris, "\n")
			res := AnalyzeSTL(bytes.NewReader(buf), int64(len(buf)), int64(len(buf)), nil)
			assertSTLResult(t, res, []Code{CodeSTLOpen})
		})
	}
	buf := asciiSTLNamed("vertex", tris, "\r\n")
	res := AnalyzeSTL(bytes.NewReader(buf), int64(len(buf)), int64(len(buf)), nil)
	assertSTLResult(t, res, []Code{CodeSTLOpen})
}

func TestSTLBinaryTriangleIsOpen(t *testing.T) {
	buf := binarySTL([][3][3]float32{{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}}})
	res := AnalyzeSTL(bytes.NewReader(buf), int64(len(buf)), int64(len(buf)), nil)
	assertSTLResult(t, res, []Code{CodeSTLOpen})
}

func TestSTLOverBudgetIsUnknown(t *testing.T) {
	buf := asciiSTL([][3]int{{0, 1, 2}})
	tr := NewTracker(Budget{MaxBytes: 1 << 20, MaxDuration: 1<<63 - 1})
	res := AnalyzeSTL(bytes.NewReader(buf), int64(len(buf)), int64(len(buf))-1, tr)
	if tr.ExhaustedReason() != ReasonWorkCeilingBytes {
		t.Fatalf("exhausted = %q, want %q", tr.ExhaustedReason(), ReasonWorkCeilingBytes)
	}
	if len(res.Findings) != 0 {
		t.Fatalf("findings = %+v, want none", res.Findings)
	}
	for _, code := range stlCodes {
		assertCheckReason(t, res, code, ReasonWorkCeilingBytes)
	}
}

func TestSTLReadFailureIsReadFailed(t *testing.T) {
	res := AnalyzeSTL(stlErrReader{}, 16, 16, nil)
	if len(res.Findings) != 0 {
		t.Fatalf("findings = %+v, want none", res.Findings)
	}
	for _, code := range stlCodes {
		assertCheckReason(t, res, code, ReasonReadFailed)
	}
}

func TestSTLGarbageIsMalformed(t *testing.T) {
	buf := bytes.Repeat([]byte("not a mesh"), 8)
	res := AnalyzeSTL(bytes.NewReader(buf), int64(len(buf)), int64(len(buf)), nil)
	for _, code := range stlCodes {
		assertCheckReason(t, res, code, ReasonMalformedInput)
	}
}

func TestSTLCodesAreNotApplicableToSTEP(t *testing.T) {
	report := BuildReport("step", "step", 20, nil)
	for _, code := range stlCodes {
		var found bool
		for _, check := range report.Checks {
			if check.Code != code {
				continue
			}
			found = true
			if check.Status != StatusNotApplicable || check.Reason != ReasonFormatNotApplicable {
				t.Fatalf("%s = %+v, want not_applicable", code, check)
			}
		}
		if !found {
			t.Fatalf("%s missing from checks", code)
		}
	}
	for _, finding := range report.Findings {
		for _, code := range stlCodes {
			if finding.Code == code {
				t.Fatalf("STEP report carries %s", code)
			}
		}
	}
}

func TestSTLFindingsAreNotJobRejections(t *testing.T) {
	for token, binding := range RejectBindings {
		for _, code := range stlCodes {
			if binding.FindingCode == code {
				t.Errorf("RejectBindings[%q] binds %s", token, code)
			}
		}
	}
}

func assertSTLResult(t *testing.T, res FamilyResult, want []Code) {
	t.Helper()
	if len(res.Findings) != len(want) {
		t.Fatalf("findings = %+v, want %v", res.Findings, want)
	}
	for i, code := range want {
		if res.Findings[i].Code != code {
			t.Fatalf("findings[%d] = %s, want %s", i, res.Findings[i].Code, code)
		}
		if res.Findings[i].Severity != SeverityInfo {
			t.Fatalf("%s severity = %s, want info", code, res.Findings[i].Severity)
		}
	}
	present := map[Code]bool{}
	for _, code := range want {
		present[code] = true
	}
	for _, code := range stlCodes {
		if present[code] {
			continue
		}
		assertCheckStatus(t, res, code, StatusClean)
	}
}

func asciiSTL(tris [][3]int) []byte {
	return asciiSTLNamed("t", tris, "\n")
}

func asciiSTLNamed(name string, tris [][3]int, nl string) []byte {
	var b bytes.Buffer
	b.WriteString("solid " + name + nl)
	for _, tri := range tris {
		b.WriteString("facet normal 0 0 0" + nl + " outer loop" + nl)
		for _, v := range tri {
			b.WriteString("  vertex ")
			b.WriteString(strconv.Itoa(v))
			b.WriteString(" 0 0" + nl)
		}
		b.WriteString(" endloop" + nl + "endfacet" + nl)
	}
	b.WriteString("endsolid " + name + nl)
	for stlBinarySize(b.Bytes()) {
		b.WriteString(nl)
	}
	return b.Bytes()
}

func binarySTL(tris [][3][3]float32) []byte {
	buf := make([]byte, 84+50*len(tris))
	binary.LittleEndian.PutUint32(buf[80:84], uint32(len(tris)))
	for i, tri := range tris {
		rec := buf[84+50*i:]
		for v := 0; v < 3; v++ {
			for axis := 0; axis < 3; axis++ {
				off := 12 + 4*(3*v+axis)
				binary.LittleEndian.PutUint32(rec[off:], math.Float32bits(tri[v][axis]))
			}
		}
	}
	return buf
}

func stlBinarySize(buf []byte) bool {
	if len(buf) < 84 {
		return false
	}
	count := binary.LittleEndian.Uint32(buf[80:84])
	return int64(len(buf)) == 84+50*int64(count)
}

type stlErrReader struct{}

func (stlErrReader) ReadAt([]byte, int64) (int, error) {
	return 0, errors.New("read failed")
}
