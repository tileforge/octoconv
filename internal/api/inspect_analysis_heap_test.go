package api

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/inspect"
)

const inspectAnalyzerTimeSlack = 250 * time.Millisecond

const liveHeapMinGCCycles = 3

const liveHeapSampleEvery = 100 * time.Microsecond

var liveHeapMetricNames = []string{"/gc/heap/live:bytes", "/memory/classes/heap/objects:bytes", "/gc/cycles/total:gc-cycles"}

func newLiveHeapSamples() []metrics.Sample {
	s := make([]metrics.Sample, len(liveHeapMetricNames))
	for i, name := range liveHeapMetricNames {
		s[i].Name = name
	}
	return s
}

func readLiveHeap(s []metrics.Sample) (live, heapAlloc int64, cycles uint64) {
	metrics.Read(s)
	return int64(s[0].Value.Uint64()), int64(s[1].Value.Uint64()), s[2].Value.Uint64()
}

type hookedFile struct {
	r           *bytes.Reader
	samples     []metrics.Sample
	base        int64
	liveAtReads int64
	reads       int
	half        bool
}

func newHookedFile(data []byte) *hookedFile {
	return &hookedFile{r: bytes.NewReader(data), samples: newLiveHeapSamples()}
}

func (f *hookedFile) Read(p []byte) (int, error) { return f.r.Read(p) }

func (f *hookedFile) ReadAt(p []byte, off int64) (int, error) {
	f.reads++
	if live, _, _ := readLiveHeap(f.samples); live-f.base > f.liveAtReads {
		f.liveAtReads = live - f.base
	}
	if f.half && len(p) > 1 {
		p = p[:len(p)/2]
	}
	return f.r.ReadAt(p, off)
}

func (f *hookedFile) Seek(off int64, whence int) (int64, error) { return f.r.Seek(off, whence) }

func (f *hookedFile) Close() error { return nil }

type inspectAnalysis struct {
	detected  string
	report    inspect.Report
	exhausted inspect.Reason
	elapsed   time.Duration
	reads     int
	liveReads int64
}

func inspectAnalysisHeap(f *hookedFile, size int64, declared string, work int64, duration time.Duration) inspectAnalysis {
	var res inspectAnalysis
	f.reads, f.liveAtReads = 0, 0
	_, _ = f.r.Seek(0, 0)
	start := time.Now()
	tr := inspect.NewTracker(inspect.Budget{MaxBytes: work, MaxDuration: duration})
	var sniffed *convert.ContainerResult
	var sniffedZip *zip.Reader
	res.detected, sniffed, sniffedZip = detectInspectFormat(f, size, declared, tr)
	families := dispatchInspectFamilies(res.detected, sniffed, sniffedZip, f, size, tr)
	families = append(families, extensionMismatchFamilyResult(res.detected, declared))
	res.report = inspect.BuildReport(reportFormat(res.detected), declared, size, tr, families...)
	res.elapsed = time.Since(start)
	res.exhausted = tr.ExhaustedReason()
	res.reads, res.liveReads = f.reads, f.liveAtReads
	return res
}

type livePeak struct {
	peak          int64
	heapAllocPeak int64
	cycles        uint64
	calls         int
}

var liveHeapPeakSingleSample bool

func liveHeapPeakValid(p livePeak) error {
	if p.cycles < liveHeapMinGCCycles {
		return fmt.Errorf("%d GC cycles over %d calls, want at least %d: a peak sampled before the collector marked the call's heap is not a measurement", p.cycles, p.calls, liveHeapMinGCCycles)
	}
	return nil
}

func liveHeapPeak(t *testing.T, call func()) livePeak {
	t.Helper()
	old := debug.SetGCPercent(1)
	defer debug.SetGCPercent(old)
	runtime.GC()
	s := newLiveHeapSamples()
	baseLive, baseAlloc, baseCycles := readLiveHeap(s)
	var stop atomic.Bool
	var topLive, topAlloc atomic.Int64
	topLive.Store(baseLive)
	topAlloc.Store(baseAlloc)
	done := make(chan struct{})
	go func() {
		defer close(done)
		own := newLiveHeapSamples()
		for !stop.Load() {
			live, alloc, _ := readLiveHeap(own)
			if live > topLive.Load() {
				topLive.Store(live)
			}
			if alloc > topAlloc.Load() {
				topAlloc.Store(alloc)
			}
			time.Sleep(liveHeapSampleEvery)
		}
	}()
	var p livePeak
	for {
		call()
		p.calls++
		_, _, cycles := readLiveHeap(s)
		p.cycles = cycles - baseCycles
		if liveHeapPeakSingleSample || p.cycles >= liveHeapMinGCCycles {
			break
		}
		if p.calls >= 1000 {
			break
		}
	}
	stop.Store(true)
	<-done
	live, alloc, _ := readLiveHeap(s)
	if live > topLive.Load() {
		topLive.Store(live)
	}
	if alloc > topAlloc.Load() {
		topAlloc.Store(alloc)
	}
	p.peak = topLive.Load() - baseLive
	p.heapAllocPeak = topAlloc.Load() - baseAlloc
	if liveHeapPeakSingleSample {
		p.peak = 0
		if live, _, _ := readLiveHeap(s); live > baseLive {
			p.peak = live - baseLive
		}
	}
	return p
}

func TestLiveHeapPeakRejectsAPeakWithoutGCCycles(t *testing.T) {
	if err := liveHeapPeakValid(livePeak{peak: 1 << 20, cycles: 0, calls: 1}); err == nil {
		t.Error("a peak with 0 GC cycles was accepted")
	}
	if err := liveHeapPeakValid(livePeak{peak: 1 << 20, cycles: liveHeapMinGCCycles - 1, calls: 1}); err == nil {
		t.Errorf("a peak with %d GC cycles was accepted", liveHeapMinGCCycles-1)
	}
	if err := liveHeapPeakValid(livePeak{peak: 1 << 20, cycles: liveHeapMinGCCycles, calls: 1}); err != nil {
		t.Error(err)
	}
	var held []byte
	p := liveHeapPeak(t, func() { held = make([]byte, 4<<20); runtime.KeepAlive(held) })
	if err := liveHeapPeakValid(p); err != nil {
		t.Fatal(err)
	}
	if p.peak < 4<<20 {
		t.Errorf("liveHeapPeak saw %d B live while a 4 MiB slice was held", p.peak)
	}
}

func inspectConfigValues(t *testing.T, key string) map[string]string {
	t.Helper()
	where := map[string]string{}
	raw, err := os.ReadFile(transportCapChart)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(key) + `: "([^"]+)"$`)
	ms := re.FindAllSubmatch(raw, -1)
	if len(ms) != 1 {
		t.Fatalf("%s: %s is not a literal this test can read (%d lines match)", transportCapChart, key, len(ms))
	}
	where["configmap.yaml"] = string(ms[0][1])
	read := func(name string, env map[string]any) {
		if v, ok := env[key]; ok {
			where[name] = fmt.Sprint(v)
		}
	}
	read("values.yaml api.extraEnv", readChartAPISpill(t, chartValues).API.ExtraEnv)
	overlays, err := filepath.Glob(transportCapChartDir + "values-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(overlays)
	for _, o := range overlays {
		read(filepath.Base(o)+" api.extraEnv", readChartAPISpill(t, o).API.ExtraEnv)
	}
	compose, err := os.ReadFile(transportCapCompose)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Services map[string]struct {
			Environment yaml.Node `yaml:"environment"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(compose, &doc); err != nil {
		t.Fatalf("decode %s: %v", transportCapCompose, err)
	}
	api := doc.Services["api"]
	var env map[string]any
	if err := api.Environment.Decode(&env); err != nil {
		t.Fatalf("%s services.api.environment: %v", transportCapCompose, err)
	}
	if _, ok := env[key]; !ok {
		t.Fatalf("%s services.api.environment does not set %s", transportCapCompose, key)
	}
	read("docker-compose.yml services.api.environment", env)
	return where
}

func inspectWorkBytes(t *testing.T) int64 {
	t.Helper()
	where := inspectConfigValues(t, "INSPECT_MAX_WORK_BYTES")
	var work int64
	for name, v := range where {
		work = max(work, transportCapInt(t, name, "INSPECT_MAX_WORK_BYTES", v))
	}
	t.Logf("INSPECT_MAX_WORK_BYTES by configuration: %v; work = %d", where, work)
	return work
}

func inspectWorkDuration(t *testing.T) time.Duration {
	t.Helper()
	where := inspectConfigValues(t, "INSPECT_MAX_WORK_DURATION")
	var d time.Duration
	for name, v := range where {
		n, err := time.ParseDuration(v)
		if err != nil || n <= 0 {
			t.Fatalf("%s: INSPECT_MAX_WORK_DURATION = %q is not a positive duration", name, v)
		}
		d = max(d, n)
	}
	t.Logf("INSPECT_MAX_WORK_DURATION by configuration: %v; duration = %s", where, d)
	return d
}

func TestInspectAnalyzerTimeSlackMatchesTheInspectPackage(t *testing.T) {
	raw, err := os.ReadFile("../inspect/analyzer_ceiling_test.go")
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("const analyzerTimeSlack = %d * time.Millisecond", inspectAnalyzerTimeSlack/time.Millisecond)
	if !strings.Contains(string(raw), want) {
		t.Errorf("internal/inspect/analyzer_ceiling_test.go does not declare %q: inspectAnalyzerTimeSlack copies the slack 78-15 measured", want)
	}
}

func zlibOf(n int) []byte {
	var z bytes.Buffer
	w, _ := zlib.NewWriterLevel(&z, zlib.BestCompression)
	_, _ = w.Write(make([]byte, n))
	_ = w.Close()
	return z.Bytes()
}

const heapPageBytes = 8192

var goSizeClasses = []int64{0, 8, 16, 24, 32, 48, 64, 80, 96, 112, 128, 144, 160, 176, 192, 208, 224, 240, 256, 288, 320, 352, 384, 416, 448, 480, 512, 576, 640, 704, 768, 896, 1024, 1152, 1280, 1408, 1536, 1792, 2048, 2304, 2688, 3072, 3200, 3456, 4096, 4864, 5376, 6144, 6528, 6784, 6912, 8192, 9472, 9728, 10240, 10880, 12288, 13568, 14336, 16384, 18432, 19072, 20480, 21760, 24576, 27264, 28672, 32768}

func goSizeClassSource() string {
	parts := make([]string, len(goSizeClasses))
	for i, c := range goSizeClasses {
		parts[i] = fmt.Sprint(c)
	}
	return "var SizeClassToSize = [NumSizeClasses]uint16{" + strings.Join(parts, ", ") + "}"
}

func roundAlloc(n int64, scan bool) int64 {
	if n <= 0 {
		return 0
	}
	if scan && n > 512 && n <= 32768-8 {
		n += 8
	}
	if n <= 32768 {
		i := sort.Search(len(goSizeClasses), func(i int) bool { return goSizeClasses[i] >= n })
		return goSizeClasses[i]
	}
	return (n + heapPageBytes - 1) &^ (heapPageBytes - 1)
}

func nextSliceCap(newLen, oldCap int64) int64 {
	doublecap := oldCap + oldCap
	if newLen > doublecap {
		return newLen
	}
	if oldCap < 256 {
		return doublecap
	}
	newcap := oldCap
	for newcap < newLen {
		newcap += (newcap + 3*256) >> 2
	}
	return newcap
}

type sliceGrowth struct {
	caps  []int64
	peak  int64
	final int64
}

func appendGrowth(elem, n int64, scan bool) sliceGrowth {
	var g sliceGrowth
	var c int64
	for c < n {
		mem := roundAlloc(nextSliceCap(c+1, c)*elem, scan)
		g.peak = max(g.peak, roundAlloc(c*elem, scan)+mem)
		g.caps = append(g.caps, c)
		c = mem / elem
	}
	g.final = roundAlloc(c*elem, scan)
	return g
}

func doublingGrowth(elem, floor, limit int64, scan bool) sliceGrowth {
	var g sliceGrowth
	var c int64
	for c < limit {
		next := min(max(floor, 2*c), limit)
		g.peak = max(g.peak, roundAlloc(c*elem, scan)+roundAlloc(next*elem, scan))
		g.caps = append(g.caps, c)
		c = next
	}
	g.final = roundAlloc(c*elem, scan)
	return g
}

func (g sliceGrowth) breaks(per int64) []int64 {
	out := make([]int64, 0, len(g.caps))
	for _, c := range g.caps {
		out = append(out, (c+1)*per)
	}
	return out
}

func disjointSpans(h int64, breaks []int64, a func(span int64) int64, b func(span int64) int64) int64 {
	best := a(0) + b(h)
	for _, s := range breaks {
		if s > h {
			break
		}
		best = max(best, a(s)+b(h-s))
	}
	return best
}

func allocBreaks(h int64) []int64 {
	var out []int64
	for _, c := range goSizeClasses {
		if c > h {
			return out
		}
		out = append(out, c+1)
	}
	for s := int64(32768) + 1; s <= h; s += heapPageBytes {
		out = append(out, s)
	}
	return out
}

func tokenReadPeak(n int64) int64 {
	if n <= 0 {
		return 0
	}
	g := appendGrowth(1, n, false)
	return max(g.peak, g.final+roundAlloc(n, false))
}

func decodedStringsPeak(n int64) int64 {
	g := appendGrowth(1, n, false)
	return disjointSpans(n, g.breaks(1), func(s int64) int64 { return appendGrowth(1, s, false).peak }, func(s int64) int64 { return appendGrowth(1, s, false).final })
}

const (
	swissMapSlotBytes      = 16
	swissMapGroupSlots     = 8
	swissMapTableSlots     = 1024
	swissMapSplitMinFilled = 384
	swissMapTableBytes     = 64
)

func swissMapBytes(entries int64) int64 {
	groups := roundAlloc(swissMapTableSlots/swissMapGroupSlots*(8+swissMapGroupSlots*swissMapSlotBytes), false)
	tables := entries/swissMapSplitMinFilled + 1
	dir := int64(1)
	for dir < 2*tables {
		dir *= 2
	}
	return tables*(groups+swissMapTableBytes) + 2*roundAlloc(dir*8, true) + 2*groups
}

type analysisSource struct {
	file   string
	source string
}

type analysisRow struct {
	name    string
	what    string
	sources []analysisSource
	calls   []string
	with    string
	bound   func(work int64) int64
}

type analysisPath struct {
	family string
	name   string
	rows   []string
}

const (
	pdfMaxInflatedStreamBytes = 1 << 20
	pdfOpaqueRangeBytes       = 16
	pdfOpaqueRangeFloor       = 4096
	pdfMaxTokenScanIterations = 1 << 20
	pdfMinValidatedStreamSpan = int64(len(">>streamendstream"))
	pdfMinResolvedStreamSpan  = int64(len("<</Length 0>>stream\nendstream"))
	pdfMaxIndexedObjects      = 65536
	pdfMaxPagesDicts          = 8192
	pdfMaxDictDepth           = 64
	pdfDictFrameBytes         = 56
	pdfDefaultTailWindow      = 64 << 10
	zlibStateBytes            = 4864 + 2688 + 160 + 32768 + 6912 + 286*256 + 768 + 30*256 + 6912 + 286*256 + 768 + 30*256 + 80 + 8 + 48
	analysisSmallObjectsBytes = 64 << 10
)

func pdfHeadBytes(work int64) int64 { return work }

func pdfTailBytes(work int64) int64 { return min(work/4, pdfDefaultTailWindow) }

func pdfMaxOpaqueRanges(n int64) int64 { return max(n/48, pdfOpaqueRangeFloor) }

func pdfInflateLimit(work int64) int64 { return min(pdfMaxInflatedStreamBytes, work/4) + 1 }

func pdfOpaqueGrowthPeak(work int64) int64 {
	head := doublingGrowth(pdfOpaqueRangeBytes, 64, pdfMaxOpaqueRanges(pdfHeadBytes(work)), false)
	tail := doublingGrowth(pdfOpaqueRangeBytes, 64, pdfMaxOpaqueRanges(pdfTailBytes(work)), false)
	return max(head.peak, head.final+tail.peak)
}

func pdfOpaqueKept(work int64) int64 {
	return doublingGrowth(pdfOpaqueRangeBytes, 64, pdfMaxOpaqueRanges(pdfHeadBytes(work)), false).final +
		doublingGrowth(pdfOpaqueRangeBytes, 64, pdfMaxOpaqueRanges(pdfTailBytes(work)), false).final
}

func pdfResolvedStreams(work int64) sliceGrowth {
	return appendGrowth(16, pdfHeadBytes(work)/pdfMinResolvedStreamSpan+1, false)
}

func pdfWalkTokens(work int64) int64 {
	h := pdfHeadBytes(work)
	streams := pdfResolvedStreams(work)
	keysAndToken := func(x int64) int64 {
		return disjointSpans(x, allocBreaks(x), func(s int64) int64 { return roundAlloc(s, false) }, tokenReadPeak) + pdfMaxDictDepth*heapPageBytes
	}
	streamsKept := func(s int64) int64 {
		return appendGrowth(16, s/pdfMinResolvedStreamSpan, false).final
	}
	return disjointSpans(h, streams.breaks(pdfMinResolvedStreamSpan), streamsKept, keysAndToken)
}

func pdfScanStreamsAndToken(work int64) int64 {
	h := pdfHeadBytes(work)
	streams := pdfResolvedStreams(work)
	growth := func(s int64) int64 { return appendGrowth(16, s/pdfMinResolvedStreamSpan, false).peak }
	return disjointSpans(h, streams.breaks(pdfMinResolvedStreamSpan), growth, tokenReadPeak)
}

func pdfAnalysisRows() []analysisRow {
	const pdf, pdftext, pdflex, dictscan, window = "internal/inspect/pdf.go", "internal/inspect/pdftext.go", "internal/convert/pdflex.go", "internal/convert/pdfdictscan.go", "internal/inspect/window.go"
	return []analysisRow{
		{"pdf_head", "head window: readWindow of headLen = maxBytes (size <= maxBytes) or maxBytes - tailReserve; maxBytes = tr.Remaining() = work, the PDF branch of detectInspectFormat consumes nothing",
			[]analysisSource{{pdf, "head, headOK := readWindow(r, 0, headLen, size)"}, {window, "buf := make([]byte, n)"}},
			[]string{"inspect.AnalyzePDF"}, "the whole of AnalyzePDF",
			func(w int64) int64 { return roundAlloc(pdfHeadBytes(w), false) }},
		{"pdf_tail", "tail window: tailReserve = min(maxBytes/4, defaultTailWindow) when size > maxBytes (head + tail <= work, counted apart)",
			[]analysisSource{{pdf, "tail, tailOK = readWindow(r, tailOff, tailReserve, size)"}},
			[]string{"inspect.AnalyzePDF"}, "the whole of AnalyzePDF",
			func(w int64) int64 { return roundAlloc(pdfTailBytes(w), false) }},
		{"pdf_opaque_growth", "opaque ranges while they grow: appendOpaqueRange doubles from 64 to pdfMaxOpaqueRanges(len) = max(len/48, 4096) ranges of 16 B, old and new arrays live at each step (up to 32 B a range of the limit at the last step, not 24); the head's kept array with the tail's growth",
			[]analysisSource{{pdf, "grown := make([]pdfOpaqueRange, len(ranges), min(max(64, 2*cap(ranges)), limit))"}, {pdf, "return max(n/48, pdfOpaqueRangeFloor)"}},
			[]string{"inspect.AnalyzePDF"}, "head and tail, inside scanPDFOpaqueRanges",
			pdfOpaqueGrowthPeak},
		{"pdf_opaque_kept", "opaque ranges kept: headOpaque and tailOpaque at their final capacity, in scope until AnalyzePDF returns",
			[]analysisSource{{pdf, "headOpaque, headOpaqueCeilingHit := scanPDFOpaqueRanges(head, tr)"}},
			[]string{"inspect.AnalyzePDF"}, "the text layer and the page count",
			pdfOpaqueKept},
		{"pdf_validated_streams", "text layer stream ranges: scanValidatedStreamRanges appends with no cap of its own, one range per at least 17 B of the head (\">>streamendstream\") and at most maxTokenScanIterations; append growth (old and new array) of 16 B elements",
			[]analysisSource{{pdftext, "ranges = append(ranges, pdfOpaqueRange{start: bodyStart, end: bodyEnd})"}},
			[]string{"inspect.AnalyzePDF"}, "the whole stream loop of scanPDFTextLayer",
			func(w int64) int64 {
				return appendGrowth(16, min(pdfMaxTokenScanIterations, pdfHeadBytes(w)/pdfMinValidatedStreamSpan)+1, false).peak
			}},
		{"pdf_inflate_growth", "inflate buffer while it grows: state.inflate doubles from 512 B to limit = min(pdfMaxInflatedStreamBytes, remaining) + 1, remaining <= work/4; old and new live at each step",
			[]analysisSource{{pdftext, "grown := make([]byte, len(out), min(max(512, 2*int64(cap(out))), limit))"}, {pdftext, "limit := min(int64(pdfMaxInflatedStreamBytes), state.remaining)"}},
			[]string{"inspect.AnalyzePDF"}, "the stream ranges, the zlib state",
			func(w int64) int64 { return doublingGrowth(1, 512, pdfInflateLimit(w), false).peak }},
		{"pdf_inflate_kept", "inflate buffer kept: state.out at its final capacity, reused across streams and held by textState until AnalyzePDF returns",
			[]analysisSource{{pdftext, "state.out = out"}},
			[]string{"inspect.AnalyzePDF"}, "the text operator strings and the page count",
			func(w int64) int64 { return doublingGrowth(1, 512, pdfInflateLimit(w), false).final }},
		{"pdf_zlib", "zlib state, one per scan (zlib.Resetter): flate decompressor 4864 B, bits 2688 B, codebits 160 B, history 32 KiB, per Huffman decoder links up to 286 (litlen) and 30 (dist) subtables of 64 uint32 plus their headers, twice for the rebuild of a dynamic block; zlib reader, adler32, bytes.Reader",
			[]analysisSource{{pdftext, "zr, err := zlib.NewReader(bytes.NewReader(body))"}, {"$GOROOT/src/compress/flate/dict_decoder.go", "dd.hist = make([]byte, size)"}, {"$GOROOT/src/compress/flate/inflate.go", "h.links[off] = make([]uint32, numLinks)"}},
			[]string{"inspect.AnalyzePDF"}, "held by textState until AnalyzePDF returns",
			func(int64) int64 { return roundUp64KiB(zlibStateBytes) }},
		{"pdf_text_strings", "text operator strings: readPDFLiteralString and readPDFHexString build the content by append; the outer string lives while operatorFollows decodes the next one, both within one stream's data <= remaining <= work/4 (raw body or inflated output)",
			[]analysisSource{{pdftext, "content, next := readPDFLiteralString(buf, i)"}, {pdftext, "_, next := readPDFLiteralString(buf, i)"}},
			[]string{"inspect.AnalyzePDF"}, "the kept inflate buffer, the stream ranges",
			func(w int64) int64 { return decodedStringsPeak(w / 4) }},
		{"pdf_scan_objects", "page count, scanPDFImpl: objects map[int]int of at most pdfMaxIndexedObjects entries (swiss tables of 1024 slots, split ones at least 384 full)",
			[]analysisSource{{pdflex, "res.objects[ints[0].value] = next"}, {dictscan, "scan := scanPDFLimited(b, nil, pdfMaxIndexedObjects, stop)"}},
			[]string{"inspect.AnalyzePDF"}, "the resolved streams and one token",
			func(int64) int64 { return swissMapBytes(pdfMaxIndexedObjects) }},
		{"pdf_scan_streams_and_token", "page count, scanPDFImpl: resolved stream slice growth (16 B per at least 29 B of the head) and one decoded name (pdfReadName: append growth and the string copy) or keyword over disjoint spans of the head",
			[]analysisSource{{pdflex, "res.streams = append(res.streams, [2]int{payload, payload + n})"}, {pdflex, "return string(out), i"}, {pdflex, "return string(b[start:i]), i"}},
			[]string{"inspect.AnalyzePDF"}, "the objects map",
			pdfScanStreamsAndToken},
		{"pdf_merge_maps", "page count, the xref merge: objects grows to pdfMaxIndexedObjects scanned plus pdfMaxIndexedObjects from the xref (headers the scanner skipped inside stream payloads) while the xref map of up to pdfMaxIndexedObjects is ranged over",
			[]analysisSource{{dictscan, "objects[num] = off"}, {pdflex, "out[num] = hdr"}},
			[]string{"inspect.AnalyzePDF"}, "the kept resolved stream slice",
			func(int64) int64 { return swissMapBytes(2*pdfMaxIndexedObjects) + swissMapBytes(pdfMaxIndexedObjects) }},
		{"pdf_scan_streams_kept", "page count: the resolved stream slice at its final capacity, scan in scope until PDFTypedDictUints returns",
			[]analysisSource{{dictscan, "objects := scan.objects"}},
			[]string{"inspect.AnalyzePDF"}, "the merge",
			func(w int64) int64 { return pdfResolvedStreams(w).final }},
		{"pdf_walk_map", "page count, the dictionary walk: the merged objects map",
			[]analysisSource{{dictscan, "if off, found := objects[pendingLength]; found {"}},
			[]string{"inspect.AnalyzePDF"}, "the walk tokens",
			func(int64) int64 { return swissMapBytes(2 * pdfMaxIndexedObjects) }},
		{"pdf_walk_tokens", "page count, the dictionary walk: up to maxPDFDictDepth key strings (one per frame), one name being decoded and the kept resolved stream slice, over disjoint spans of the head; plus a page of rounding per key",
			[]analysisSource{{dictscan, "stack[n-1].key = name"}, {dictscan, "name, next := pdfReadName(b, i)"}},
			[]string{"inspect.AnalyzePDF"}, "the merged map",
			pdfWalkTokens},
		{"pdf_walk_values", "page count, the dictionary walk: values (8 B) of at most maxPagesDictsScanned matched dictionaries and the frame stack of at most maxPDFDictDepth 56 B frames, append growth",
			[]analysisSource{{dictscan, "values = append(values, f.val)"}, {dictscan, "stack = append(stack, frame{expectKey: true})"}},
			[]string{"inspect.AnalyzePDF"}, "the merged map",
			func(int64) int64 {
				return appendGrowth(8, pdfMaxPagesDicts+1, false).peak + appendGrowth(pdfDictFrameBytes, pdfMaxDictDepth, true).peak
			}},
	}
}

func commonAnalysisRows() []analysisRow {
	return []analysisRow{
		{"small_objects", "small objects of the sequence: Tracker, FamilyResults and their Checks, BuildReport's merged report, Sniff's 12 B buffer and io.MultiReader, the SectionReaders of detectInspectFormat; under 16 KiB, rounded up to 64 KiB",
			[]analysisSource{{"internal/api/inspect_handlers.go", "report := inspect.BuildReport(reportFormat(detected), declaredFormat, header.Size, tr, families...)"}, {"internal/convert/sniff.go", "buf := make([]byte, sniffLen)"}},
			[]string{"convert.Sniff", "extensionMismatchFamilyResult", "inspect.BuildReport"}, "every path",
			func(int64) int64 { return analysisSmallObjectsBytes }},
	}
}

func inspectAnalysisRows() []analysisRow {
	var rows []analysisRow
	rows = append(rows, commonAnalysisRows()...)
	rows = append(rows, pdfAnalysisRows()...)
	return rows
}

func inspectAnalysisPaths() []analysisPath {
	pdfBase := []string{"small_objects", "pdf_head", "pdf_tail"}
	pdfPage := append(append([]string{}, pdfBase...), "pdf_opaque_kept", "pdf_inflate_kept", "pdf_zlib")
	return []analysisPath{
		{"pdf", "pdf_opaque_scan", append(append([]string{}, pdfBase...), "pdf_opaque_growth")},
		{"pdf", "pdf_text_inflate", append(append([]string{}, pdfBase...), "pdf_opaque_kept", "pdf_validated_streams", "pdf_inflate_growth", "pdf_zlib")},
		{"pdf", "pdf_text_operators", append(append([]string{}, pdfBase...), "pdf_opaque_kept", "pdf_validated_streams", "pdf_inflate_kept", "pdf_zlib", "pdf_text_strings")},
		{"pdf", "pdf_page_count_scan", append(append([]string{}, pdfPage...), "pdf_scan_objects", "pdf_scan_streams_and_token")},
		{"pdf", "pdf_page_count_merge", append(append([]string{}, pdfPage...), "pdf_merge_maps", "pdf_scan_streams_kept")},
		{"pdf", "pdf_page_count_walk", append(append([]string{}, pdfPage...), "pdf_walk_map", "pdf_walk_tokens", "pdf_walk_values")},
	}
}

func analysisRowsByName(t *testing.T) map[string]analysisRow {
	t.Helper()
	byName := map[string]analysisRow{}
	for _, r := range inspectAnalysisRows() {
		if _, dup := byName[r.name]; dup {
			t.Fatalf("inventory row %s twice", r.name)
		}
		byName[r.name] = r
	}
	return byName
}

func analysisPathBytes(t *testing.T, p analysisPath, work int64) int64 {
	t.Helper()
	byName := analysisRowsByName(t)
	var sum int64
	for _, name := range p.rows {
		r, ok := byName[name]
		if !ok {
			t.Fatalf("path %s names row %s, which the inventory does not have", p.name, name)
		}
		sum += r.bound(work)
	}
	return sum
}

func analysisPathByName(t *testing.T, name string) analysisPath {
	t.Helper()
	for _, p := range inspectAnalysisPaths() {
		if p.name == name {
			return p
		}
	}
	t.Fatalf("no inventory path %s", name)
	return analysisPath{}
}

func inspectAnalysisHeapTerm(t *testing.T, work int64) (term int64, by analysisPath) {
	t.Helper()
	for _, p := range inspectAnalysisPaths() {
		if s := analysisPathBytes(t, p, work); s > term {
			term, by = s, p
		}
	}
	return roundUp64KiB(term), by
}

func inspectAnalysisHeapBytes(work int64) int64 {
	var term int64
	byName := map[string]analysisRow{}
	for _, r := range inspectAnalysisRows() {
		byName[r.name] = r
	}
	for _, p := range inspectAnalysisPaths() {
		var sum int64
		for _, name := range p.rows {
			sum += byName[name].bound(work)
		}
		term = max(term, sum)
	}
	return roundUp64KiB(term)
}

func checkAnalysisSources(t *testing.T) {
	t.Helper()
	files := map[string][]string{}
	read := func(file string) []string {
		if lines, ok := files[file]; ok {
			return lines
		}
		path := strings.Replace(file, "$GOROOT", runtime.GOROOT(), 1)
		if !strings.HasPrefix(file, "$GOROOT") {
			path = "../../" + file
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		files[file] = strings.Split(string(raw), "\n")
		return files[file]
	}
	for _, r := range inspectAnalysisRows() {
		for _, s := range r.sources {
			at := 0
			for i, l := range read(s.file) {
				if strings.Contains(l, s.source) {
					at = i + 1
					break
				}
			}
			if at == 0 {
				t.Errorf("row %s: %s does not read %q", r.name, s.file, s.source)
			}
		}
	}
	found := false
	for _, l := range read("$GOROOT/src/internal/runtime/gc/sizeclasses.go") {
		if strings.Contains(l, goSizeClassSource()) {
			found = true
		}
	}
	if !found {
		t.Errorf("$GOROOT/src/internal/runtime/gc/sizeclasses.go does not read %q: goSizeClasses copies go1.26.4's size classes", goSizeClassSource())
	}
}

func analysisSourceAt(t *testing.T, s analysisSource) string {
	t.Helper()
	path := strings.Replace(s.file, "$GOROOT", runtime.GOROOT(), 1)
	if !strings.HasPrefix(s.file, "$GOROOT") {
		path = "../../" + s.file
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, l := range strings.Split(string(raw), "\n") {
		if strings.Contains(l, s.source) {
			return fmt.Sprintf("%s:%d", s.file, i+1)
		}
	}
	return s.file + ":?"
}

func logAnalysisInventory(t *testing.T, work int64, family string) {
	t.Helper()
	byName := analysisRowsByName(t)
	used := map[string]bool{}
	for _, p := range inspectAnalysisPaths() {
		if family != "" && p.family != family {
			continue
		}
		for _, name := range p.rows {
			used[name] = true
		}
	}
	names := make([]string, 0, len(used))
	for name := range used {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		r := byName[name]
		at := make([]string, len(r.sources))
		for i, s := range r.sources {
			at[i] = analysisSourceAt(t, s)
		}
		t.Logf("row %-28s %10d B (%6.2f MiB)  %s; live with: %s; %s", name, r.bound(work), mib(r.bound(work)), r.what, r.with, strings.Join(at, ", "))
	}
	for _, p := range inspectAnalysisPaths() {
		if family != "" && p.family != family {
			continue
		}
		s := analysisPathBytes(t, p, work)
		t.Logf("path %-24s %10d B (%6.2f MiB) = %s", p.name, s, mib(s), strings.Join(p.rows, " + "))
	}
}

func pdfFlateWorst(work int64) (data []byte, streams int) {
	stream := zlibOf(pdfMaxInflatedStreamBytes)
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

func pdfRepeated(work int64, head, unit, tail string) []byte {
	var b bytes.Buffer
	b.WriteString(head)
	for int64(b.Len()+len(unit)+len(tail)) <= work {
		b.WriteString(unit)
	}
	b.WriteString(tail)
	return b.Bytes()
}

func pdfDictNames(work int64) []byte {
	return pdfRepeated(work, "%PDF-1.4\n1 0 obj\n<<", "/ab ", ">>\nendobj\n")
}

func pdfObjects(work int64) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	for i := 1; ; i++ {
		obj := fmt.Sprintf("%d 0 obj\n1\nendobj\n", i)
		if int64(b.Len()+len(obj)) > work {
			break
		}
		b.WriteString(obj)
	}
	return b.Bytes()
}

func pdfLongName(work int64) []byte {
	head, tail := "%PDF-1.4\n1 0 obj\n<</", ">>\nendobj\n"
	return []byte(head + strings.Repeat("a", int(work)-len(head)-len(tail)) + tail)
}

func pdfRawLiteral(work int64) []byte {
	allowance := work / 4
	open := "%PDF-1.4\n1 0 obj\n<<>>\nstream\n"
	body := "(" + strings.Repeat("a", int(allowance)-len("()Tj\n")) + ")Tj\n"
	pdf := open + body + "endstream\nendobj\n%"
	return []byte(pdf + strings.Repeat("x", int(work)-len(pdf)))
}

func pdfXrefMapsAndName(work int64) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	for n := 1; n <= pdfMaxIndexedObjects; n++ {
		fmt.Fprintf(&b, "%d 0 obj\n", n)
	}
	var payload bytes.Buffer
	offsets := make([]int, pdfMaxIndexedObjects)
	first := pdfMaxIndexedObjects + 1
	for i := range offsets {
		offsets[i] = payload.Len()
		fmt.Fprintf(&payload, "%d 0 obj\n", first+i)
	}
	header := fmt.Sprintf("<</Length %d>>\nstream\n", payload.Len())
	base := b.Len() + len(header)
	b.WriteString(header)
	b.Write(payload.Bytes())
	b.WriteString("\nendstream\n")
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n%d %d\n", first, len(offsets))
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", base+off)
	}
	fmt.Fprintf(&b, "trailer\n<<>>\nstartxref\n%d\n%%%%EOF\n", xref)
	head, tail := "<</", ">>\n"
	if rest := int(work) - b.Len() - len(head) - len(tail); rest > 0 {
		b.WriteString(head + strings.Repeat("a", rest) + tail)
	}
	return b.Bytes()
}

type analysisCase struct {
	name     string
	declared string
	want     string
	path     string
	data     []byte
	route    func(a inspectAnalysis) (string, error)
}

func characteristicRoute(want map[string]any) func(a inspectAnalysis) (string, error) {
	return func(a inspectAnalysis) (string, error) {
		keys := make([]string, 0, len(want))
		for k := range want {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var got []string
		var bad []string
		for _, k := range keys {
			v, ok := a.report.Characteristics[k]
			got = append(got, fmt.Sprintf("%s=%v", k, v))
			if !ok || fmt.Sprint(v) != fmt.Sprint(want[k]) {
				bad = append(bad, fmt.Sprintf("%s = %v, want %v", k, v, want[k]))
			}
		}
		route := strings.Join(got, " ") + " coverage=" + string(a.report.Coverage.Reason) + fmt.Sprintf(" reads=%d", a.reads)
		if len(bad) > 0 {
			return route, fmt.Errorf("the analyzer took another path: %s", strings.Join(bad, "; "))
		}
		return route, nil
	}
}

func pdfAnalysisCases(work int64) []analysisCase {
	flate, _ := pdfFlateWorst(work)
	return []analysisCase{
		{"pdf_flate_worst", "pdf", "pdf", "pdf_text_inflate", flate, characteristicRoute(map[string]any{"text_layer_reason": "work_ceiling_bytes", "page_count_reason": "malformed_input"})},
		{"pdf_ranges", "pdf", "pdf", "pdf_opaque_scan", pdfRepeated(work, "%PDF-1.4\n", "(%)\n", ""), characteristicRoute(map[string]any{"text_layer_reason": "work_ceiling_bytes", "page_count_reason": "malformed_input"})},
		{"pdf_dict_names", "pdf", "pdf", "pdf_page_count_walk", pdfDictNames(work), characteristicRoute(map[string]any{"text_layer_reason": "work_ceiling_bytes", "page_count_reason": "malformed_input"})},
		{"pdf_objects", "pdf", "pdf", "pdf_page_count_scan", pdfObjects(work), characteristicRoute(map[string]any{"text_layer_reason": "work_ceiling_bytes", "page_count_reason": "work_ceiling_bytes"})},
		{"pdf_long_name", "pdf", "pdf", "pdf_page_count_walk", pdfLongName(work), characteristicRoute(map[string]any{"text_layer_reason": "work_ceiling_bytes", "page_count_reason": "malformed_input"})},
		{"pdf_validated_streams", "pdf", "pdf", "pdf_text_inflate", pdfRepeated(work, "%PDF-1.4\n", ">>streamendstream", ""), characteristicRoute(map[string]any{"text_layer": "absent", "page_count_reason": "malformed_input"})},
		{"pdf_resolved_streams", "pdf", "pdf", "pdf_page_count_scan", pdfRepeated(work, "%PDF-1.4\n", "<</Length 0>>stream\nendstream\n", ""), characteristicRoute(map[string]any{"text_layer_reason": "work_ceiling_bytes", "page_count_reason": "malformed_input"})},
		{"pdf_raw_literal", "pdf", "pdf", "pdf_text_operators", pdfRawLiteral(work), characteristicRoute(map[string]any{"text_layer": "present"})},
		{"pdf_xref_maps", "pdf", "pdf", "pdf_page_count_walk", pdfXrefMapsAndName(work), characteristicRoute(map[string]any{"text_layer_reason": "work_ceiling_bytes", "page_count_reason": "malformed_input"})},
	}
}

type analysisMeasure struct {
	timed   inspectAnalysis
	sampled inspectAnalysis
	peak    livePeak
}

func measureInspectAnalysis(t *testing.T, c analysisCase, work int64, duration time.Duration, half bool) (analysisMeasure, string, error) {
	t.Helper()
	f := newHookedFile(c.data)
	f.half = half
	size := int64(len(c.data))
	var m analysisMeasure
	m.timed = inspectAnalysisHeap(f, size, c.declared, work, duration)
	route, err := c.route(m.timed)
	if m.timed.detected != c.want {
		return m, route, fmt.Errorf("detected %q, want %q", m.timed.detected, c.want)
	}
	if err != nil {
		return m, route, err
	}
	m.peak = liveHeapPeak(t, func() {
		live, _, _ := readLiveHeap(f.samples)
		f.base = live
		m.sampled = inspectAnalysisHeap(f, size, c.declared, work, duration)
	})
	if m.sampled.detected != m.timed.detected || m.sampled.exhausted != m.timed.exhausted || m.sampled.report.Coverage.Reason != m.timed.report.Coverage.Reason {
		return m, route, fmt.Errorf("the sampled run took another path: detected %q, exhausted %q, coverage %q; the timed run %q, %q, %q", m.sampled.detected, m.sampled.exhausted, m.sampled.report.Coverage.Reason, m.timed.detected, m.timed.exhausted, m.timed.report.Coverage.Reason)
	}
	if sroute, serr := c.route(m.sampled); serr != nil || sroute != route {
		return m, route, fmt.Errorf("the sampled run took another path: %s (%v)", sroute, serr)
	}
	return m, route, nil
}

func runAnalysisCases(t *testing.T, cases []analysisCase, work int64, duration time.Duration, term int64) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := analysisPathByName(t, c.path)
			bound := analysisPathBytes(t, path, work)
			m, route, err := measureInspectAnalysis(t, c, work, duration, false)
			t.Logf("%s: file %d B; live peak %d B (%.2f MiB), HeapAlloc peak %d B (%.2f MiB), %d GC cycles over %d calls; live at ReadAt %.2f MiB over %d reads; %s timed, %s sampled; detected %q, exhausted %q; route %s; path %s bound %d B (%.2f MiB); term %d B (%.2f MiB)",
				c.name, len(c.data), m.peak.peak, mib(m.peak.peak), m.peak.heapAllocPeak, mib(m.peak.heapAllocPeak), m.peak.cycles, m.peak.calls, mib(m.sampled.liveReads), m.sampled.reads, m.timed.elapsed, m.sampled.elapsed, m.timed.detected, m.timed.exhausted, route, c.path, bound, mib(bound), term, mib(term))
			if err != nil {
				t.Fatal(err)
			}
			if err := liveHeapPeakValid(m.peak); err != nil {
				t.Fatal(err)
			}
			if m.peak.peak > bound {
				t.Errorf("live peak %d B is above its path %s, %d B", m.peak.peak, c.path, bound)
			}
			if bound > term {
				t.Errorf("path %s, %d B, is above inspectAnalysisHeapBytes(work) %d B", c.path, bound, term)
			}
			if m.timed.elapsed > duration+inspectAnalyzerTimeSlack {
				t.Errorf("the analysis took %s, above MaxDuration %s + analyzerTimeSlack %s", m.timed.elapsed, duration, inspectAnalyzerTimeSlack)
			}
		})
	}
}

func TestInspectAnalysisStaysWithinItsTerm(t *testing.T) {
	work := inspectWorkBytes(t)
	duration := inspectWorkDuration(t)
	checkAnalysisSources(t)
	term, by := inspectAnalysisHeapTerm(t, work)
	if got := inspectAnalysisHeapBytes(work); got != term {
		t.Fatalf("inspectAnalysisHeapBytes(%d) = %d, the inventory sums to %d", work, got, term)
	}
	logAnalysisInventory(t, work, "")
	t.Logf("inspectAnalysisHeapBytes(%d) = %d B (%.2f MiB), set by path %s (family %s)", work, term, mib(term), by.name, by.family)
	var cases []analysisCase
	cases = append(cases, pdfAnalysisCases(work)...)
	runAnalysisCases(t, cases, work, duration, term)
}
