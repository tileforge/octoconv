package inspect

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

const analyzerDifferentialDuration = time.Hour

func zlibOf(data []byte) []byte {
	var z bytes.Buffer
	w, _ := zlib.NewWriterLevel(&z, zlib.BestCompression)
	_, _ = w.Write(data)
	_ = w.Close()
	return z.Bytes()
}

func worstFlatePDF(work int64) (data []byte, streams int) {
	stream := zlibOf(make([]byte, pdfMaxInflatedStreamBytes))
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	tail := "trailer\n<</Root 1 0 R>>\n%%EOF\n"
	for i := 1; ; i++ {
		obj := fmt.Sprintf("%d 0 obj\n<</Length %d>>\nstream\n", i, len(stream))
		if int64(b.Len()+len(obj)+len(stream)+len("\nendstream\nendobj\n")+len(tail)) > work {
			break
		}
		b.WriteString(obj)
		b.Write(stream)
		b.WriteString("\nendstream\nendobj\n")
		streams++
	}
	b.WriteString(tail)
	return b.Bytes(), streams
}

func rangesPDF(n int) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	for b.Len()+4 <= n {
		b.WriteString("(%)\n")
	}
	return b.Bytes()
}

func dictNamesPDF(n int) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n1 0 obj\n<<")
	for b.Len()+4 <= n-8 {
		b.WriteString("/ab ")
	}
	b.WriteString(">>\nendobj\n")
	return b.Bytes()
}

func objectsPDF(n int) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	for i := 1; b.Len()+20 <= n; i++ {
		fmt.Fprintf(&b, "%d 0 obj\n1\nendobj\n", i)
	}
	return b.Bytes()
}

type genStream struct {
	dict string
	body []byte
}

func genPDF(trailer string, streams ...genStream) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n")
	b.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	b.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	b.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n")
	for i, s := range streams {
		fmt.Fprintf(&b, "%d 0 obj\n<< %s /Length %d >>\nstream\n", i+4, s.dict, len(s.body))
		b.Write(s.body)
		b.WriteString("\nendstream\nendobj\n")
	}
	b.WriteString("trailer\n<< /Root 1 0 R " + trailer + ">>\n%%EOF\n")
	return b.Bytes()
}

func repeatTo(unit string, n int) []byte {
	return []byte(strings.Repeat(unit, n/len(unit)+1)[:n])
}

var (
	corpusText    = "BT /F1 12 Tf 72 712 Td (The quick brown fox jumps over the lazy dog) Tj ET\n"
	corpusJunk    = "q 1 0 0 1 0 0 cm 0.5 g Q\n"
	corpusTJ      = "BT [(Kerned) -120 (text) 40 (in an array)] TJ ET\n"
	corpusHex     = "BT <48656c6c6f20776f726c6420616e6420667269656e6473> Tj ET\n"
	corpusQuote   = "BT (line one) ' 1 2 (line two with words) \" ET\n"
	corpusLZWBody = []byte("\x80\x0b\x60\x50\x22\x0c\x0c\x85\x01")
)

func flate(dict string, content []byte) genStream {
	return genStream{dict: "/Filter /FlateDecode " + dict, body: zlibOf(content)}
}

func corruptChecksum(z []byte) []byte {
	out := append([]byte(nil), z...)
	out[len(out)-1] ^= 0xff
	return out
}

type analyzerInput struct {
	name     string
	data     []byte
	maxBytes int64
}

func generatedPDFs() []analyzerInput {
	const big = 8 << 20
	junk4000 := repeatTo(corpusJunk, 4000)
	text := []byte(strings.Repeat(corpusText, 10))
	textZ := zlibOf(text)
	var in []analyzerInput
	add := func(name string, data []byte, maxBytes ...int64) {
		for _, m := range maxBytes {
			in = append(in, analyzerInput{name: fmt.Sprintf("%s/max=%d", name, m), data: data, maxBytes: m})
		}
	}
	l := int64(len(text))
	truncatedZ := zlibOf(repeatTo(corpusText, 20000))
	truncatedZ = truncatedZ[:len(truncatedZ)/2]
	add("flate_text", genPDF("", genStream{dict: "/Filter /FlateDecode", body: textZ}), big, 4*l-4, 4*l, 4*l+4, 4*l+3)
	add("flate_junk_then_text", genPDF("", flate("", junk4000), flate("", text)), big, 4*4000-4, 4*4000, 4*(4000+l), 4*(4000+l)-1)
	add("raw_text", genPDF("", genStream{body: []byte("BT (Hello there my good friend) Tj ET")}), big, 64, 4*37, 4*36)
	add("raw_junk_then_text", genPDF("", genStream{body: junk4000}, genStream{body: []byte(corpusText)}), big, 4*4000, 4*3999, 4*(4000+int64(len(corpusText))))
	add("junk_then_lzw", genPDF("", flate("", junk4000), genStream{dict: "/Filter /LZWDecode", body: corpusLZWBody}), big, 4*4000, 4*4001)
	add("junk_then_raw_filter_text", genPDF("", flate("", junk4000), genStream{dict: "/Filter /DCTDecode", body: []byte(corpusText)}, flate("", text)), big, 4*4000)
	add("corrupt_checksum", genPDF("", genStream{dict: "/Filter /FlateDecode", body: corruptChecksum(textZ)}), big, 4*(l/2), 4*l, 4*l-4)
	add("truncated_deflate", genPDF("", genStream{dict: "/Filter /FlateDecode", body: truncatedZ}), big, 4*100, 4*20000)
	add("junk_then_corrupt", genPDF("", flate("", junk4000), genStream{dict: "/Filter /FlateDecode", body: []byte("\x78\x9c\xff\xff\xff\xff\x00\x01")}), big, 4*4000, 4*4001)
	add("junk_then_empty_then_text", genPDF("", flate("", junk4000), flate("", nil), flate("", text)), big, 4*4000, 4*4000+4)
	add("junk_then_empty_then_lzw", genPDF("", flate("", junk4000), flate("", nil), genStream{dict: "/Filter /LZWDecode", body: corpusLZWBody}), big, 4*4000)
	add("skip_markers_then_text", genPDF("",
		flate("/Subtype /Image /Width 10 /Height 10", repeatTo("\x00\x01\x02", 50000)),
		genStream{dict: "/Length1 900", body: repeatTo("\x00'\"", 3000)},
		flate("/Type /EmbeddedFile", []byte(corpusText)),
		flate("", text)), big, 4*l, 4*l-4)
	add("ambiguous_font_then_text", genPDF("", genStream{dict: "/Resources << /Font << /FontFile2 9 0 R >> >>", body: []byte(corpusText)}, flate("", text)), big, 4*l)
	add("hex_tj_quote", genPDF("", flate("", []byte(corpusHex+corpusTJ+corpusQuote))), big, 4*int64(len(corpusHex+corpusTJ+corpusQuote)), 4*30)
	add("over_cap_then_text", genPDF("", flate("", make([]byte, pdfMaxInflatedStreamBytes+10)), flate("", text)), big)
	add("at_cap_then_text", genPDF("", flate("", make([]byte, pdfMaxInflatedStreamBytes)), flate("", text)), big, 4*(pdfMaxInflatedStreamBytes+l), 4*pdfMaxInflatedStreamBytes)
	add("encrypted_text", genPDF("/Encrypt 9 0 R ", flate("", text)), big, 4*l-4)
	add("js_openaction_text", genPDF("/OpenAction << /S /JavaScript /JS (app.alert(1)) >> ", flate("", text), genStream{body: []byte("(/JS inside a literal) /Launch")}), big, 4*l-4, 2048)
	add("objstm_text", genPDF("", genStream{dict: "/Type /ObjStm /N 1 /First 4", body: zlibOf([]byte("4 0 << /JavaScript 1 >>"))}, flate("", text)), big, 4*l)
	add("name_escape", genPDF("/Java#53cript 1 ", flate("", text)), big)
	add("unterminated_literal", append(genPDF("", flate("", text)), []byte("(never closed << /Length 3 >> stream\nabc\nendstream")...), big)
	add("comment_before_stream", []byte("%PDF-1.4\n4 0 obj\n<< /Length 30 >> % note\nstream\n"+corpusText+"\nendstream\nendobj\n"), big, 64)
	add("text_after_many_small_streams", func() []byte {
		var s []genStream
		for i := 0; i < 40; i++ {
			s = append(s, flate("", repeatTo(corpusJunk, 300+i)))
		}
		s = append(s, flate("", text))
		return genPDF("", s...)
	}(), big, 4*12000, 4*20000)
	return in
}

func fixturePDFs(t *testing.T) []analyzerInput {
	t.Helper()
	var paths []string
	for _, g := range []string{
		filepath.Join("testdata", "pdf", "*.pdf"),
		filepath.Join("..", "convert", "testdata", "ocr", "*.pdf"),
		filepath.Join("..", "convert", "testdata", "sanitize", "*.pdf"),
		filepath.Join("..", "convert", "testdata", "*.pdf"),
	} {
		m, err := filepath.Glob(g)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, m...)
	}
	sort.Strings(paths)
	if len(paths) < 6 {
		t.Fatalf("PDF fixture corpus has %d files, want the six testdata/pdf files at least", len(paths))
	}
	var in []analyzerInput
	add := func(name string, data []byte) {
		n := int64(len(data))
		for _, m := range []int64{8 << 20, n, n / 2, 4 * n / 10, 1024} {
			if m > 0 {
				in = append(in, analyzerInput{name: fmt.Sprintf("%s/max=%d", name, m), data: data, maxBytes: m})
			}
		}
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		add(filepath.ToSlash(p), data)
	}
	add("api/pdfBytesFixture", []byte("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n"))
	for name, f := range map[string]func() []byte{
		"cleanPDFFixture":            cleanPDFFixture,
		"jsPDFFixture":               jsPDFFixture,
		"objStmPDFFixture":           objStmPDFFixture,
		"jsAndObjStmPDFFixture":      jsAndObjStmPDFFixture,
		"encryptedPDFFixture":        encryptedPDFFixture,
		"hexEscapeJSPDFFixture":      hexEscapeJSPDFFixture,
		"hexEscapeObjStmPDFFixture":  hexEscapeObjStmPDFFixture,
		"jsInsideStreamOnlyFixture":  jsInsideStreamOnlyPDFFixture,
		"jscriptOnlyPDFFixture":      jscriptOnlyPDFFixture,
		"jsInsideLiteralOnlyFixture": jsInsideLiteralStringOnlyPDFFixture,
	} {
		add("pdf_test/"+name, f())
	}
	sort.Slice(in, func(i, j int) bool { return in[i].name < in[j].name })
	return in
}

func stopPDFs() []analyzerInput {
	flateWorst, _ := worstFlatePDF(1 << 20)
	return []analyzerInput{
		{name: "pdf_flate_worst/work=1MiB", data: flateWorst, maxBytes: 1 << 20},
		{name: "pdf_ranges/256KiB", data: rangesPDF(256 << 10), maxBytes: 8 << 20},
		{name: "pdf_dict_names/1MiB", data: dictNamesPDF(1 << 20), maxBytes: 8 << 20},
		{name: "pdf_objects/1MiB", data: objectsPDF(1 << 20), maxBytes: 8 << 20},
	}
}

var mutationTokens = []string{
	"stream", "endstream", "(", ")", "<", ">", "<<", ">>", "%", "\n", "\\", "endobj",
	"/Filter /FlateDecode ", "/Filter /LZWDecode ", "/Subtype /Image ", "/FontFile2 ", "/Length1 ",
	"BT (mutated text here) Tj ET", "<4142434445464748494a4b4c4d4e4f50> Tj", "/JavaScript", "/Encrypt",
	"/Type /Pages /Count 7", "/ObjStm", "#", "\x78\x9c",
}

func mutatedPDFs(bases []analyzerInput, n int) []analyzerInput {
	rng := rand.New(rand.NewPCG(78, 15))
	out := make([]analyzerInput, 0, n)
	for i := 0; i < n; i++ {
		base := bases[rng.IntN(len(bases))]
		data := append([]byte(nil), base.data...)
		for ops := 1 + rng.IntN(4); ops > 0 && len(data) > 0; ops-- {
			at := rng.IntN(len(data))
			switch rng.IntN(6) {
			case 0:
				data[at] ^= byte(1 + rng.IntN(255))
			case 1:
				tok := mutationTokens[rng.IntN(len(mutationTokens))]
				data = append(data[:at], append([]byte(tok), data[at:]...)...)
			case 2:
				end := min(len(data), at+1+rng.IntN(64))
				data = append(data[:at], data[end:]...)
			case 3:
				data = data[:at]
			case 4:
				end := min(len(data), at+1+rng.IntN(256))
				seg := append([]byte(nil), data[at:end]...)
				data = append(data[:end], append(seg, data[end:]...)...)
			case 5:
				data[at] = "()<>%\n\\"[rng.IntN(7)]
			}
		}
		maxBytes := base.maxBytes
		switch rng.IntN(4) {
		case 0:
			maxBytes = int64(len(data))
		case 1:
			maxBytes = 4 * int64(1+rng.IntN(max(1, len(data))))
		}
		if maxBytes <= 0 {
			maxBytes = 1
		}
		label, _, _ := strings.Cut(base.name, "/max=")
		out = append(out, analyzerInput{name: fmt.Sprintf("mutation/%04d/%s/max=%d", i, label, maxBytes), data: data, maxBytes: maxBytes})
	}
	return out
}

func analyzerDifferentialCorpus(t *testing.T) []analyzerInput {
	t.Helper()
	gen := generatedPDFs()
	in := append([]analyzerInput{}, fixturePDFs(t)...)
	in = append(in, gen...)
	in = append(in, stopPDFs()...)
	in = append(in, mutatedPDFs(gen, 2000)...)
	return in
}

func closedListTextStep(steps []referenceStreamStep) (row string, k int, state pdfTextScanState) {
	for i, s := range steps {
		if !s.decoded {
			continue
		}
		rem := s.before.remaining
		switch {
		case rem == 0:
			row = "V3"
		case s.flate && s.readErr && s.out <= pdfMaxInflatedStreamBytes && int64(s.out) > rem:
			row = "V2"
		case s.flate && int64(s.out) > min(int64(pdfMaxInflatedStreamBytes), rem):
			row = "V1"
		case !s.flate && int64(s.out) > rem:
			row = "V1"
		}
		if row != "" {
			state = s.before
			state.budgetHit = true
			state.covered = false
			return row, i, state
		}
	}
	return "", -1, pdfTextScanState{}
}

func closedListTextRow(steps []referenceStreamStep) (string, pdfTextScanState) {
	row, _, state := closedListTextStep(steps)
	return row, state
}

func analyzerTracker() *Tracker {
	return NewTracker(Budget{MaxDuration: analyzerDifferentialDuration})
}

type pdfOutcome struct {
	res       FamilyResult
	exhausted Reason
}

func newPDFOutcome(in analyzerInput) pdfOutcome {
	tr := analyzerTracker()
	res := AnalyzePDF(bytes.NewReader(in.data), int64(len(in.data)), in.maxBytes, tr)
	return pdfOutcome{res: res, exhausted: tr.ExhaustedReason()}
}

func referencePDFOutcome(in analyzerInput, o referenceOracle) (pdfOutcome, referenceRun) {
	tr := analyzerTracker()
	res, run := referenceAnalyzePDF(bytes.NewReader(in.data), int64(len(in.data)), in.maxBytes, tr, o)
	return pdfOutcome{res: res, exhausted: tr.ExhaustedReason()}, run
}

func textVerdict(o pdfOutcome) string {
	return fmt.Sprintf("%v/%v", o.res.Characteristics["text_layer"], o.res.Characteristics["text_layer_reason"])
}

type closedListRow struct {
	input, row             string
	k                      int
	reference, oracle, got string
}

func TestAnalyzerVerdictsMatchTheReferenceOutsideTheClosedList(t *testing.T) {
	corpus := analyzerDifferentialCorpus(t)
	fired := map[string]int{}
	differed := map[string]int{}
	var rows []closedListRow
	for _, in := range corpus {
		t.Run(in.name, func(t *testing.T) {
			ref, refRun := referencePDFOutcome(in, referenceOracle{})
			want := ref
			row, k, _ := closedListTextStep(refRun.steps)
			got := newPDFOutcome(in)
			if row != "" {
				oracle, oracleRun := referencePDFOutcome(in, referenceOracle{textLayer: closedListTextRow})
				if oracleRun.textRow != row {
					t.Fatalf("oracle run fired %q, reference trace %q", oracleRun.textRow, row)
				}
				want = oracle
				fired[row]++
				if !reflect.DeepEqual(oracle, ref) {
					differed[row]++
				}
				rows = append(rows, closedListRow{input: in.name, row: row, k: k, reference: textVerdict(ref), oracle: textVerdict(oracle), got: textVerdict(got)})
			}
			if !reflect.DeepEqual(got.res, want.res) || got.exhausted != want.exhausted {
				t.Fatalf("closed-list row %q: new outcome differs from the expected one\n got:  %+v exhausted=%q\n want: %+v exhausted=%q\n reference: %+v exhausted=%q",
					row, got.res, got.exhausted, want.res, want.exhausted, ref.res, ref.exhausted)
			}
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].row+rows[i].input < rows[j].row+rows[j].input })
	var b strings.Builder
	fmt.Fprintf(&b, "closed-list rows over %d inputs: fired %v, outcome differs from the reference %v\n", len(corpus), fired, differed)
	fmt.Fprintf(&b, "%-4s %-72s %3s  %-34s %-34s %s\n", "row", "input", "k", "reference", "oracle", "new")
	for _, r := range rows {
		if strings.HasPrefix(r.input, "mutation/") && r.reference == r.oracle {
			continue
		}
		fmt.Fprintf(&b, "%-4s %-72s %3d  %-34s %-34s %s\n", r.row, r.input, r.k, r.reference, r.oracle, r.got)
	}
	t.Log(b.String())
	for _, row := range []string{"V1", "V2", "V3"} {
		if fired[row] == 0 {
			t.Errorf("closed-list row %s never fired on the corpus", row)
		}
		if differed[row] == 0 {
			t.Errorf("closed-list row %s fired %d times and its oracle never differed from the reference", row, fired[row])
		}
	}
}

func allocatedBy(call func()) uint64 {
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	call()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}
