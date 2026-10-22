package inspect

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"math"
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
		{name: "pdf_ranges_then_js/256KiB", data: append(rangesPDF(256<<10), "\n1 0 obj\n<< /OpenAction << /S /JavaScript >> >>\nendobj\n"...), maxBytes: 8 << 20},
		{name: "pdf_comments/256KiB", data: append([]byte("%PDF-1.4\n"), repeatTo("%\n", 256<<10)...), maxBytes: 8 << 20},
		{name: "pdf_ranges_in_tail/max=1MiB", data: append(append(genPDF("", flate("", []byte(corpusText))), repeatTo(corpusJunk, 2<<20)...), rangesPDF(64<<10)...), maxBytes: 1 << 20},
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

func closedListOpaqueRow(run referenceRun) bool {
	return run.headRanges > pdfMaxOpaqueRanges(run.headLen) || (run.tailLen > 0 && run.tailRanges > pdfMaxOpaqueRanges(run.tailLen))
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
			textRow, k, _ := closedListTextStep(refRun.steps)
			opaqueRow := closedListOpaqueRow(refRun)
			got := newPDFOutcome(in)
			var fire []string
			if textRow != "" {
				fire = append(fire, textRow)
			}
			if opaqueRow {
				fire = append(fire, "V4")
			}
			if len(fire) > 0 {
				o := referenceOracle{opaqueCap: opaqueRow}
				if textRow != "" {
					o.textLayer = closedListTextRow
				}
				oracle, oracleRun := referencePDFOutcome(in, o)
				if oracleRun.textRow != textRow {
					t.Fatalf("oracle run fired %q, reference trace %q", oracleRun.textRow, textRow)
				}
				want = oracle
				for _, row := range fire {
					fired[row]++
					if !reflect.DeepEqual(oracle, ref) {
						differed[row]++
					}
					rows = append(rows, closedListRow{input: in.name, row: row, k: k, reference: textVerdict(ref), oracle: textVerdict(oracle), got: textVerdict(got)})
					if row == "V4" {
						rows[len(rows)-1].k = -1
						rows[len(rows)-1].reference = checksVerdict(ref)
						rows[len(rows)-1].oracle = checksVerdict(oracle)
						rows[len(rows)-1].got = checksVerdict(got)
					}
				}
				if opaqueRow && textRow == "" && !reflect.DeepEqual(oracle.res.Characteristics, ref.res.Characteristics) {
					t.Fatalf("V4 oracle changed characteristics that do not use opaque ranges:\n oracle:    %v\n reference: %v", oracle.res.Characteristics, ref.res.Characteristics)
				}
			}
			if !reflect.DeepEqual(got.res, want.res) || got.exhausted != want.exhausted {
				t.Fatalf("closed-list rows %v: new outcome differs from the expected one\n got:  %+v exhausted=%q\n want: %+v exhausted=%q\n reference: %+v exhausted=%q",
					fire, got.res, got.exhausted, want.res, want.exhausted, ref.res, ref.exhausted)
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
	for _, row := range []string{"V1", "V2", "V3", "V4"} {
		if fired[row] == 0 {
			t.Errorf("closed-list row %s never fired on the corpus", row)
		}
		if differed[row] == 0 {
			t.Errorf("closed-list row %s fired %d times and its oracle never differed from the reference", row, fired[row])
		}
	}
}

func checksVerdict(o pdfOutcome) string {
	unknown := 0
	for _, c := range o.res.Checks {
		if c.Status == StatusUnknown {
			unknown++
		}
	}
	return fmt.Sprintf("checks unknown %d/%d, findings %d", unknown, len(o.res.Checks), len(o.res.Findings))
}

func allocatedBy(call func()) uint64 {
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	call()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

func TestPDFInflateStaysUnderTheAllowance(t *testing.T) {
	const work = int64(8 << 20)
	data, streams := worstFlatePDF(work)
	limit := uint64(work + 2*(work/4+1) + 4<<20)
	var res FamilyResult
	var elapsed time.Duration
	allocated := allocatedBy(func() {
		tr := NewTracker(Budget{MaxBytes: work})
		start := time.Now()
		res = AnalyzePDF(bytes.NewReader(data), int64(len(data)), work, tr)
		elapsed = time.Since(start)
	})
	t.Logf("pdf_flate_worst: %d streams of %d inflated bytes, %d B file, work %d: allocated %d B (%.2f MiB, limit %d B = %.2f MiB), %s (MaxDuration %s), text_layer %v/%v",
		streams, pdfMaxInflatedStreamBytes, len(data), work, allocated, float64(allocated)/(1<<20), limit, float64(limit)/(1<<20), elapsed, defaultMaxDuration,
		res.Characteristics["text_layer"], res.Characteristics["text_layer_reason"])
	if res.Characteristics["text_layer"] != nil || res.Characteristics["text_layer_reason"] != string(ReasonWorkCeilingBytes) {
		t.Errorf("text_layer = %v/%v, want <nil>/%s as the reference reports", res.Characteristics["text_layer"], res.Characteristics["text_layer_reason"], ReasonWorkCeilingBytes)
	}
	if allocated > limit {
		t.Errorf("AnalyzePDF allocated %d B on pdf_flate_worst, want at most work + 2 x (inflateAllowance + 1) + 4 MiB = %d B", allocated, limit)
	}
	if elapsed > defaultMaxDuration {
		t.Errorf("AnalyzePDF took %s on pdf_flate_worst, want at most MaxDuration %s", elapsed, defaultMaxDuration)
	}
}

func literalRanges(buf []byte, tr *Tracker) ([]pdfOpaqueRange, bool) {
	return scanPDFLiteralAndHexStringRanges(buf, nil, math.MaxInt, tr)
}

func sameRanges(a, b []pdfOpaqueRange) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

func bestOf(runs int, call func()) time.Duration {
	best := time.Duration(1<<63 - 1)
	for i := 0; i < runs; i++ {
		start := time.Now()
		call()
		best = min(best, time.Since(start))
	}
	return best
}

func TestPDFLiteralScanIsLinear(t *testing.T) {
	for _, n := range []int{1 << 10, 64 << 10, 256 << 10} {
		head := rangesPDF(n)
		got, gotHit := literalRanges(head, nil)
		want, wantHit := referenceScanPDFLiteralAndHexStringRanges(head, nil)
		if !sameRanges(got, want) || gotHit != wantHit {
			t.Errorf("pdf_ranges head %d B: %d ranges ceilingHit=%v, reference %d ranges ceilingHit=%v", n, len(got), gotHit, len(want), wantHit)
		}
	}
	corpus := analyzerDifferentialCorpus(t)
	mismatched := 0
	for _, in := range corpus {
		got, gotHit := literalRanges(in.data, nil)
		want, wantHit := referenceScanPDFLiteralAndHexStringRanges(in.data, nil)
		if !sameRanges(got, want) || gotHit != wantHit {
			mismatched++
			t.Errorf("%s: literal/hex ranges differ from the reference (%d vs %d, ceilingHit %v vs %v)", in.name, len(got), len(want), gotHit, wantHit)
		}
	}
	t.Logf("literal/hex ranges equal to the reference on %d of %d corpus inputs", len(corpus)-mismatched, len(corpus))

	times := map[int]time.Duration{}
	for _, n := range []int{256 << 10, 1 << 20, 4 << 20} {
		head := rangesPDF(n)
		times[n] = bestOf(3, func() { literalRanges(head, nil) })
		t.Logf("pdf_ranges head %d B: %s (best of 3)", n, times[n])
	}
	ratio := float64(times[4<<20]) / float64(max(times[1<<20], time.Microsecond))
	t.Logf("time(4 MiB) / time(1 MiB) = %.2f (linear 4, quadratic 16)", ratio)
	if ratio > 8 {
		t.Errorf("time(4 MiB) / time(1 MiB) = %.2f, want at most 8: the literal/hex scan grows faster than linearly", ratio)
	}
	head := rangesPDF(8 << 20)
	elapsed := bestOf(1, func() { literalRanges(head, nil) })
	t.Logf("pdf_ranges head %d B: %s", len(head), elapsed)
	if elapsed > time.Second {
		t.Errorf("scanPDFLiteralAndHexStringRanges took %s on the 8 MiB pdf_ranges head, want at most 1s", elapsed)
	}
}

func liveHeap() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func TestPDFOpaqueRangesStayUnderTheirCap(t *testing.T) {
	head := rangesPDF(8 << 20)
	base := liveHeap()
	start := time.Now()
	ranges, ceilingHit := scanPDFOpaqueRanges(head, nil)
	elapsed := time.Since(start)
	live := int64(liveHeap()) - int64(base)
	runtime.KeepAlive(ranges)
	runtime.KeepAlive(head)
	limit := pdfMaxOpaqueRanges(len(head))
	bound := int64(max(len(head)/2, 24*pdfOpaqueRangeFloor))
	t.Logf("pdf_ranges head %d B: %d merged ranges (cap %d, %d B of backing array), ceilingHit=%v, live heap over base %d B (%.2f MiB), bound max(len/2, 24 x %d) = %d B, %s",
		len(head), len(ranges), limit, 16*cap(ranges), ceilingHit, live, float64(live)/(1<<20), pdfOpaqueRangeFloor, bound, elapsed)
	if !ceilingHit {
		t.Errorf("scanPDFOpaqueRanges on pdf_ranges did not report its ceiling")
	}
	if live > bound {
		t.Errorf("live heap after scanPDFOpaqueRanges = %d B, want at most %d B", live, bound)
	}
	if backing := int64(24 * cap(ranges)); backing > bound {
		t.Errorf("the merged ranges keep a backing array of %d ranges; with the old array alive while it grew that is %d B, want at most %d B", cap(ranges), backing, bound)
	}
}

func realisticPDFHeads() []analyzerInput {
	rng := rand.New(rand.NewPCG(78, 152))
	var in []analyzerInput
	for _, head := range []int{1 << 20, 2 << 20, 4 << 20, 8 << 20} {
		for _, streamSize := range []int{64 << 10, 256 << 10, 1 << 20} {
			if streamSize > head/2 {
				continue
			}
			var b bytes.Buffer
			b.WriteString("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n")
			body := make([]byte, streamSize)
			for i := 1; b.Len()+streamSize+200 < head; i++ {
				for j := 0; j+8 <= len(body); j += 8 {
					v := rng.Uint64()
					for k := 0; k < 8; k++ {
						body[j+k] = byte(v >> (8 * k))
					}
				}
				fmt.Fprintf(&b, "%d 0 obj\n<< /Length %d /Filter /FlateDecode >>\nstream\n", i, len(body))
				b.Write(body)
				b.WriteString("\nendstream\nendobj\n")
			}
			in = append(in, analyzerInput{name: fmt.Sprintf("generated head %d MiB, random-byte streams of %d KiB", head>>20, streamSize>>10), data: b.Bytes()})
		}
	}
	return in
}

func TestPDFOpaqueRangeCapLeavesRealFilesAlone(t *testing.T) {
	var in []analyzerInput
	var paths []string
	for _, g := range []string{
		filepath.Join("testdata", "pdf", "*.pdf"),
		filepath.Join("..", "convert", "testdata", "*.pdf"),
		filepath.Join("..", "convert", "testdata", "*", "*.pdf"),
		filepath.Join("..", "convert", "testdata", "*", "*", "*.pdf"),
		filepath.Join("..", "e2e", "testdata", "*.pdf"),
	} {
		m, err := filepath.Glob(g)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, m...)
	}
	sort.Strings(paths)
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		in = append(in, analyzerInput{name: filepath.ToSlash(p), data: data})
	}
	in = append(in, analyzerInput{name: "api pdfBytesFixture", data: []byte("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n")})
	for _, f := range []struct {
		name string
		data []byte
	}{
		{"pdf_test cleanPDFFixture", cleanPDFFixture()},
		{"pdf_test jsPDFFixture", jsPDFFixture()},
		{"pdf_test objStmPDFFixture", objStmPDFFixture()},
		{"pdf_test encryptedPDFFixture", encryptedPDFFixture()},
		{"pdf_test jsInsideLiteralStringOnlyPDFFixture", jsInsideLiteralStringOnlyPDFFixture()},
	} {
		in = append(in, analyzerInput{name: f.name, data: f.data})
	}
	in = append(in, realisticPDFHeads()...)
	if len(in) < 20 {
		t.Fatalf("real PDF corpus has %d inputs, want the repository PDFs, the api fixture and the generated heads", len(in))
	}

	t.Logf("pdfMaxOpaqueRanges(n) = max(n/48, %d) (D-29a): a pdfOpaqueRange is %d B; ranges at the cap grown by doubling capped at the cap hold the old and new arrays at once, (1/2 + 1) x 16 = 24 B per range, so 24 x n/48 = n/2 above the floor and 24 x %d = %d B below it", pdfOpaqueRangeFloor, 16, pdfOpaqueRangeFloor, 24*pdfOpaqueRangeFloor)
	for _, n := range []int{0, 15, 4096, 48 * pdfOpaqueRangeFloor, 1 << 20, 8 << 20} {
		limit := pdfMaxOpaqueRanges(n)
		if 24*limit > max(n/2, 24*pdfOpaqueRangeFloor) {
			t.Errorf("pdfMaxOpaqueRanges(%d) = %d: 24 x cap = %d B exceeds max(n/2, 24 x %d)", n, limit, 24*limit, pdfOpaqueRangeFloor)
		}
	}
	worst := 0.0
	var b strings.Builder
	fmt.Fprintf(&b, "%-62s %9s %7s %8s %7s\n", "input", "n (B)", "ranges", "cap", "ratio")
	for _, c := range in {
		s, _ := referenceScanPDFStreamRanges(c.data, nil)
		cm, _ := referenceScanPDFCommentRanges(c.data, nil)
		l, _ := referenceScanPDFLiteralAndHexStringRanges(c.data, nil)
		total := len(s) + len(cm) + len(l)
		limit := pdfMaxOpaqueRanges(len(c.data))
		ratio := float64(total) / float64(limit)
		worst = max(worst, ratio)
		fmt.Fprintf(&b, "%-62s %9d %7d %8d %7.4f\n", c.name, len(c.data), total, limit, ratio)
		if ratio > 0.25 {
			t.Errorf("%s: %d opaque ranges against a cap of %d (ratio %.3f), want at most a quarter of the cap", c.name, total, limit, ratio)
		}
	}
	fmt.Fprintf(&b, "worst ratio %.4f (limit 0.25) over %d inputs", worst, len(in))
	t.Log(b.String())
}
