package convert

import (
	"bytes"
	"fmt"
	"runtime"
	"testing"
)

// Two allocation bounds, both measured before they existed and both on paths
// that run in the API process -- internal/inspect reads up to its 8 MiB budget
// through this tokenizer for every POST /v1/inspect, and the sanitize worker
// reads whole uploads through it.
//
// They are asserted as ALLOCATION, not as wall time: time is a property of the
// machine, and a bound that only holds on a fast laptop is not a bound.

// allocDuring reports bytes allocated while f runs.
func allocDuring(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// TestScanPDFDoesNotGrowOnALongIntegerRun is the one that was NOT in the todo
// and turned out to be the real cost.
//
// The walk keeps "the last two integer tokens", and it kept them by trimming
// with ints = ints[len(ints)-2:] -- which re-slices the SAME backing array, so
// append grows that array for the life of the walk. A cross-reference table is
// a long run of integers and nothing else, so it was the worst case:
//
//	20 971 614 bytes in, 48.0 MB allocated in scanPDF alone (~2.3x the input)
//
// After: 0.0 MB, and the walk itself went from 96ms to 54ms.
func TestScanPDFDoesNotGrowOnALongIntegerRun(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n1 0 obj\n<< /Type /Pages /Count 2 >>\nendobj\n")
	xrefAt := buf.Len()
	buf.WriteString("xref\n0 1048576\n")
	for i := 0; i < 1048575; i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", 9)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size 1048576 >>\nstartxref\n%d\n%%%%EOF\n", xrefAt)
	big := buf.Bytes()

	var got pdfScan
	alloc := allocDuring(func() { got = scanPDF(big) })

	// Generous by design: the point is that allocation does not SCALE with the
	// run, not that it is exactly zero. Before the fix this was 48 MB on this
	// very input.
	const ceiling = 1 << 20
	if alloc > ceiling {
		t.Fatalf("scanPDF allocated %.1f MB walking a %d-byte file that is mostly an xref table; "+
			"the last-two-integers window is growing its backing array again",
			float64(alloc)/(1<<20), len(big))
	}
	if len(got.objects) != 1 {
		t.Fatalf("objects = %d, want 1 -- the walk stopped seeing structure", len(got.objects))
	}
}

// TestTypedDictScanIsBoundedByNestingDepth is the second finding the todo
// recorded, and the reason PDFTypedDictUints could not be restored as it was:
// maxDicts bounds how many dictionaries are visited, and nothing bounded how
// deep they nest, so the frame stack grew with the nesting.
//
//	2 097 169 bytes of "<<" cost 131.4 MB before maxPDFDictDepth existed.
func TestTypedDictScanIsBoundedByNestingDepth(t *testing.T) {
	deep := append([]byte("%PDF-1.4\n1 0 obj\n"), bytes.Repeat([]byte("<<"), 1<<20)...)

	var values []int
	var ceiling bool
	alloc := allocDuring(func() { values, ceiling = PDFTypedDictUints(deep, "Pages", "Count", 1<<20) })

	if !ceiling {
		t.Fatal("a file nesting a million dictionaries did not report a ceiling: a walk that answers " +
			"here is answering from a stack it grew to the file's own depth")
	}
	if values != nil {
		t.Fatalf("values = %v alongside a ceiling; an inflated integer is worse than an honest unknown", values)
	}
	const ceilingBytes = 1 << 20
	if alloc > ceilingBytes {
		t.Fatalf("PDFTypedDictUints allocated %.1f MB on %d bytes of nesting", float64(alloc)/(1<<20), len(deep))
	}
}

// TestRealFilesStillResolveAfterTheBounds is the other half. A bound that
// refuses ordinary documents trades a resource finding for a correctness one,
// which is the worse trade -- so the files this repository pins must still
// parse, with their streams found.
func TestRealFilesStillResolveAfterTheBounds(t *testing.T) {
	for _, name := range []string{
		"testdata/sanitize/threat_all_classes.pdf",
		"testdata/ocr/multipage_text.pdf",
		"testdata/verapdf_noncompliant.pdf",
	} {
		t.Run(name, func(t *testing.T) {
			raw := readFixtureOrSkip(t, name)
			ranges, err := pdfStreamPayloads(raw)
			if err != nil {
				t.Fatalf("a pinned file stopped resolving: %v", err)
			}
			if len(ranges) == 0 {
				t.Fatal("no streams found in a real PDF: the walk is not seeing structure")
			}
		})
	}
}
