package api

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
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
	"github.com/tileforge/octoconv/internal/convert/pdfref805d49da"
	"github.com/tileforge/octoconv/internal/inspect"
)

const inspectAnalyzerTimeSlack = 250 * time.Millisecond

const liveHeapMinGCCycles = 3

const liveHeapSampleEvery = 100 * time.Microsecond

const (
	liveHeapEnoughGCCycles = 64
	liveHeapSampleFor      = 200 * time.Millisecond
)

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
	began := time.Now()
	for {
		call()
		p.calls++
		_, _, cycles := readLiveHeap(s)
		p.cycles = cycles - baseCycles
		if liveHeapPeakSingleSample || p.calls >= 100000 {
			break
		}
		if p.cycles >= liveHeapMinGCCycles && (p.cycles >= liveHeapEnoughGCCycles || time.Since(began) >= liveHeapSampleFor) {
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
	pdfMaxIndexedObjects      = 65536
	pdfMinScannedObjectSpan   = int64(len("1 0 obj") + 1)
	pdfXrefEntryBytes         = 20
	pdfMaxPagesDicts          = 8192
	pdfMaxDictDepth           = 64
	pdfDictFrameBytes         = 48
	pdfDefaultTailWindow      = 64 << 10
	zlibStateBytes            = 4864 + 2688 + 160 + 32768 + 6912 + 286*256 + 768 + 30*256 + 6912 + 286*256 + 768 + 30*256 + 80 + 8 + 48
	analysisSmallObjectsBytes = 64 << 10
)

func pdfHeadBytes(work int64) int64 { return work }

func pdfTailBytes(work int64) int64 { return min(work/4, pdfDefaultTailWindow) }

func pdfMaxOpaqueRanges(n int64) int64 { return max(n/48, pdfOpaqueRangeFloor) }

func pdfInflateLimit(work int64) int64 { return min(pdfMaxInflatedStreamBytes, work/4) + 1 }

func pdfNextOpaqueRangeCap(c, limit int64) int64 {
	half := limit/2 + limit%2
	next := max(64, 2*c)
	if next > half {
		next = half
		if c >= half {
			next = limit
		}
	}
	return min(next, limit)
}

func pdfOpaqueRangeGrowth(limit, n int64) sliceGrowth {
	var g sliceGrowth
	var c int64
	for c < n {
		next := pdfNextOpaqueRangeCap(c, limit)
		g.peak = max(g.peak, roundAlloc(c*pdfOpaqueRangeBytes, false)+roundAlloc(next*pdfOpaqueRangeBytes, false))
		g.caps = append(g.caps, c)
		c = next
	}
	g.final = roundAlloc(c*pdfOpaqueRangeBytes, false)
	return g
}

func pdfOpaqueRangesOf(n int64) sliceGrowth {
	return pdfOpaqueRangeGrowth(pdfMaxOpaqueRanges(n), pdfMaxOpaqueRanges(n))
}

func pdfOpaqueGrowthPeak(work int64) int64 {
	head := pdfOpaqueRangesOf(pdfHeadBytes(work))
	tail := pdfOpaqueRangesOf(pdfTailBytes(work))
	return max(head.peak, head.final+tail.peak)
}

func pdfOpaqueKept(work int64) int64 {
	return pdfOpaqueRangesOf(pdfHeadBytes(work)).final + pdfOpaqueRangesOf(pdfTailBytes(work)).final
}

func pdfValidatedStreams(work int64) sliceGrowth {
	h := pdfHeadBytes(work)
	n := min(pdfMaxOpaqueRanges(h), pdfMaxTokenScanIterations, h/pdfMinValidatedStreamSpan+1)
	return pdfOpaqueRangeGrowth(pdfMaxOpaqueRanges(h), n)
}

func pdfScannedObjects(work int64) int64 {
	return min(pdfMaxIndexedObjects, pdfHeadBytes(work)/pdfMinScannedObjectSpan)
}

func pdfXrefEntries(work int64) int64 {
	return min(pdfMaxIndexedObjects, pdfHeadBytes(work)/pdfXrefEntryBytes)
}

func pdfMergedObjects(work int64) int64 {
	return min(pdfMaxIndexedObjects, pdfScannedObjects(work)+pdfXrefEntries(work))
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
		{"pdf_opaque_growth", "opaque ranges while they grow: appendOpaqueRange doubles from 64 while the double is at most half of pdfMaxOpaqueRanges(len) = max(len/48, 4096), then takes half, then the limit (D-31, D-29a); 16 B ranges, old and new arrays live at each step, at most 24 B a range of the limit plus rounding; the head's kept array with the tail's growth",
			[]analysisSource{{pdf, "grown := make([]pdfOpaqueRange, len(ranges), nextOpaqueRangeCap(cap(ranges), limit))"}, {pdf, "half := limit/2 + limit%2"}, {pdf, "return max(n/48, pdfOpaqueRangeFloor)"}},
			[]string{"inspect.AnalyzePDF"}, "head and tail, inside scanPDFOpaqueRanges",
			pdfOpaqueGrowthPeak},
		{"pdf_opaque_kept", "opaque ranges kept: headOpaque and tailOpaque at their final capacity, in scope until AnalyzePDF returns",
			[]analysisSource{{pdf, "headOpaque, headOpaqueCeilingHit := scanPDFOpaqueRanges(head, tr)"}},
			[]string{"inspect.AnalyzePDF"}, "the text layer and the page count",
			pdfOpaqueKept},
		{"pdf_validated_streams", "text layer stream ranges: scanValidatedStreamRanges appends through appendOpaqueRange under pdfMaxOpaqueRanges(len) (D-31 F7), at most min(limit, one per 17 B of the head, maxTokenScanIterations); growth by the opaque range schedule, its peak counted through the stream loop",
			[]analysisSource{{pdftext, "ranges, ok = appendOpaqueRange(ranges, pdfOpaqueRange{start: bodyStart, end: bodyEnd}, limit, tr)"}, {pdftext, "limit := pdfMaxOpaqueRanges(len(buf))"}},
			[]string{"inspect.AnalyzePDF"}, "the whole stream loop of scanPDFTextLayer",
			func(w int64) int64 { return pdfValidatedStreams(w).peak }},
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
		{"pdf_scan_objects", "page count, scanPDFImpl: objects map[int]int of at most min(pdfMaxIndexedObjects, head / 8) entries, a header \"1 0 obj\" and a separator per entry (swiss tables of 1024 slots, split ones at least 384 full)",
			[]analysisSource{{pdflex, "res.objects[ints[0].value] = next"}, {dictscan, "scan := scanPDFLimited(b, nil, pdfMaxIndexedObjects, stop)"}},
			[]string{"inspect.AnalyzePDF"}, "the resolved streams and one token",
			func(w int64) int64 { return swissMapBytes(pdfScannedObjects(w)) }},
		{"pdf_scan_streams_and_token", "page count, scanPDFImpl with keepStreams false: no resolved stream list (D-31 F8a); names compared in place by pdfNameEquals and keywords as a slice of the head, no token is built (D-31 F5)",
			[]analysisSource{{pdflex, "if keepStreams {"}, {pdflex, "return scanPDFImpl(b, seed, maxObjects, stop, false)"}, {pdflex, "func pdfNameEquals(b []byte, i int, s string) bool {"}, {pdflex, "return b[start:i], i"}},
			[]string{"inspect.AnalyzePDF"}, "the objects map",
			func(int64) int64 { return 0 }},
		{"pdf_merge_maps", "page count, the xref merge: objects grows to at most pdfMaxIndexedObjects distinct numbers, a new one past the cap answers (nil, true) (D-31 F8b), while the xref map of at most min(pdfMaxIndexedObjects, head / 20 B a table entry) is ranged over; objects at most min(cap, scanned + xref)",
			[]analysisSource{{dictscan, "if _, known := objects[num]; !known && len(objects) >= pdfMaxIndexedObjects {"}, {dictscan, "objects[num] = off"}, {pdflex, "out[num] = hdr"}},
			[]string{"inspect.AnalyzePDF"}, "the kept resolved stream slice",
			func(w int64) int64 { return swissMapBytes(pdfMergedObjects(w)) + swissMapBytes(pdfXrefEntries(w)) }},
		{"pdf_scan_streams_kept", "page count: the scan in scope until PDFTypedDictUints returns holds no stream list (D-31 F8a), only the objects map counted in pdf_merge_maps",
			[]analysisSource{{dictscan, "objects := scan.objects"}, {pdflex, "return scanPDFImpl(b, seed, maxObjects, stop, false)"}},
			[]string{"inspect.AnalyzePDF"}, "the merge",
			func(int64) int64 { return 0 }},
		{"pdf_walk_map", "page count, the dictionary walk: the merged objects map, at most min(pdfMaxIndexedObjects, scanned + xref) entries (D-31 F8b)",
			[]analysisSource{{dictscan, "if off, found := objects[pendingLength]; found {"}},
			[]string{"inspect.AnalyzePDF"}, "the walk tokens",
			func(w int64) int64 { return swissMapBytes(pdfMergedObjects(w)) }},
		{"pdf_walk_tokens", "page count, the dictionary walk: a frame keeps the offset of its key in the head and compares it in place, names and keywords build nothing, the scan kept no stream list (D-31 F5, F8a)",
			[]analysisSource{{dictscan, "stack[n-1].keyOff = start"}, {dictscan, "keyIs := func(f frame, s string) bool { return f.keyOff >= 0 && pdfNameEquals(b, f.keyOff, s) }"}, {dictscan, "word, next := pdfKeywordAt(b, i)"}},
			[]string{"inspect.AnalyzePDF"}, "the merged map",
			func(int64) int64 { return 0 }},
		{"pdf_walk_values", "page count, the dictionary walk: values (8 B) of at most maxPagesDictsScanned matched dictionaries and the frame stack of at most maxPDFDictDepth 48 B frames without pointers (the key is an offset), append growth",
			[]analysisSource{{dictscan, "values = append(values, f.val)"}, {dictscan, "stack = append(stack, frame{expectKey: true, keyOff: -1})"}},
			[]string{"inspect.AnalyzePDF"}, "the merged map",
			func(int64) int64 {
				return appendGrowth(8, pdfMaxPagesDicts+1, false).peak + appendGrowth(pdfDictFrameBytes, pdfMaxDictDepth, false).peak
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

const (
	inspectMaxRelsPartBytes     = 256 << 10
	inspectEBMLInfoWindow       = 256 * 1024
	inspectMP4TailWindow        = 2 << 20
	inspectZipBoundTailWindow   = 73728
	xmlStackNodeBytes           = 64
	xmlMinNestedElementSpan     = int64(len("<a>"))
	xmlAttrBytes                = 48
	xmlMinAttrSpan              = int64(len(` b=""`))
	xmlDecoderFixedBytes        = 4 << 10
	zipDirectoryStringsPerEntry = 3 * 16
)

func readAllPeak(n int64) int64 {
	caps := []int64{512}
	filled := int64(0)
	next := int64(256)
	cur := int64(512)
	for filled+cur < n {
		filled += cur
		cur = roundAlloc(next, false)
		caps = append(caps, cur)
		next += next / 2
	}
	var sum int64
	for _, c := range caps {
		sum += c
	}
	if len(caps) == 1 {
		return sum
	}
	return sum + roundAlloc(n, false)
}

func bufferGrowthPeak(n int64) int64 {
	c := int64(64)
	peak := c
	for c < n {
		next := roundAlloc(2*c, false)
		peak = max(peak, c+next)
		c = next
	}
	return peak
}

func bufferGrowthBreaks(n int64) []int64 {
	out := allocBreaks(n)
	for c := int64(64); c < n; c = roundAlloc(2*c, false) {
		out = append(out, c+1)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func xmlTokenPeak(s int64) int64 {
	if s <= 0 {
		return 0
	}
	return bufferGrowthPeak(s) + roundAlloc(s, false)
}

func xmlPartDecodePeak(n int64) int64 {
	attrs := appendGrowth(xmlAttrBytes, n/xmlMinAttrSpan+1, true)
	attrsAt := func(s int64) int64 { return appendGrowth(xmlAttrBytes, s/xmlMinAttrSpan, true).peak }
	nodesAt := func(s int64) int64 { return xmlStackNodeBytes * (s/xmlMinNestedElementSpan + 1) }
	tokenAndNodes := func(x int64) int64 { return disjointSpans(x, bufferGrowthBreaks(x), xmlTokenPeak, nodesAt) }
	return roundAlloc(n, false) + disjointSpans(n, attrs.breaks(xmlMinAttrSpan), attrsAt, tokenAndNodes) + xmlDecoderFixedBytes
}

func otherAnalysisRows() []analysisRow {
	const handlers, container, media, image = "internal/api/inspect_handlers.go", "internal/inspect/container.go", "internal/inspect/media.go", "internal/inspect/image.go"
	return []analysisRow{
		{"response_after_release", "the inspect response (writeJSON of the report): written after file.Close, dropMultipartForm and release, so it never lives with the form and the analysis slot; not in the term",
			[]analysisSource{{handlers, "dropMultipartForm(r)"}, {handlers, "release()"}, {handlers, "writeJSON(w, http.StatusOK, inspectResponse{Report: report, Operations: operations})"}},
			nil, "nothing of the analysis slot",
			func(int64) int64 { return 0 }},
		{"detect_sniff_window", "detection window: readSniffWindow allocates min(size, SniffWindowBytes); ValidateSniffPrefix, LooksLikeLaTeXFromPrefix, LooksLikeTextFromPrefix and IsOLECFBFromPrefix read it in place (sniffWindowHeapBytes, 78-14); block-scoped, dead before SniffVideo",
			[]analysisSource{{handlers, "window, windowErr := readSniffWindow(file, size)"}, {handlers, "buf := make([]byte, n)"}, {"internal/convert/markupsniff.go", "func ValidateSniffPrefix(raw []byte, size int64) ([]byte, bool) {"}},
			[]string{"readSniffWindow", "convert.ValidateSniffPrefix", "convert.LooksLikeLaTeXFromPrefix", "convert.LooksLikeTextFromPrefix", "convert.IsOLECFBFromPrefix", "convert.IsCADDeclared"}, "the HTML lower-case copy or the CAD windows",
			func(int64) int64 { return sniffWindowHeapBytes }},
		{"detect_html_lower", "LooksLikeHTMLFromPrefix: bytes.ToLower of at most 15 runes of the window, the markers' length plus one (htmlLowerPrefixBytes, 78-18)",
			[]analysisSource{{"internal/convert/htmlsniff.go", "lower := bytes.ToLower(buf[:n])"}},
			[]string{"convert.LooksLikeHTMLFromPrefix"}, "the detection window",
			func(int64) int64 { return htmlLowerPrefixBytes }},
		{"detect_cad", "CAD: ReadCADTail CADTailBytes and SniffCADWindows' bytes.Split of the head (cadSniffHeapBytes, 78-14)",
			[]analysisSource{{handlers, "tail, tailErr = convert.ReadCADTail(file, size)"}, {handlers, "res := convert.SniffCADWindows(head, tail, size, declaredFormat)"}},
			[]string{"convert.ReadCADTail", "convert.SniffCADWindows"}, "the detection window",
			func(int64) int64 { return cadSniffHeapBytes }},
		{"detect_video_peek", "SniffVideo: avPeekLen 4096 B peek and its io.MultiReader (videoSniffHeapBytes, 78-14); rest is dropped",
			[]analysisSource{{handlers, "videoDetected, _, verr := convert.SniffVideo(fresh())"}},
			[]string{"convert.SniffVideo"}, "small objects only",
			func(int64) int64 { return videoSniffHeapBytes }},
		{"detect_audio_peek", "SniffAudio: mp3PeekLen 512 KiB peek and its io.MultiReader (audioSniffHeapBytes, 78-14); rest is dropped",
			[]analysisSource{{handlers, "audioDetected, _, aerr := convert.SniffAudio(fresh())"}},
			[]string{"convert.SniffAudio"}, "small objects only",
			func(int64) int64 { return audioSniffHeapBytes }},
		{"zip_bound_windows", "zipDirectoryExceedsStructuralBound: the EOCD tail window and the directory window inspectMaxZipDirBytes",
			[]analysisSource{{handlers, "if outcome, _, _ := zipDirectoryExceedsStructuralBound(file, size); outcome.exceeds() {"}, {handlers, "buf := make([]byte, tailLen)"}, {handlers, "window := make([]byte, windowLen)"}},
			[]string{"zipDirectoryExceedsStructuralBound"}, "small objects only",
			func(int64) int64 { return inspectZipBoundTailWindow + inspectMaxZipDirBytes }},
		{"zip_reader", "SniffContainerReader: zip.NewReader on at most inspectMaxZipDirEntries records (zipSniffHeapBytes, 78-14 D-27a, which also counts the bound windows); the zip.Reader is kept through AnalyzeContainerWith",
			[]analysisSource{{handlers, "} else if cr, zr, cerr := convert.SniffContainerReader(file, size); cerr == nil && cr.Format != \"\" && !cr.DuplicateRootPart {"}, {"internal/convert/docsniff.go", "zr, err := zip.NewReader(rr, size)"}},
			[]string{"convert.SniffContainerReader", "inspect.AnalyzeContainer", "inspect.AnalyzeContainerWith"}, "the whole container analysis",
			func(int64) int64 { return zipSniffHeapBytes }},
		{"zip_names", "zip directory strings: per record d (name, extra, comment) kept through f.Extra and the Name and Comment copies, within inspectMaxZipDirBytes of directory, three size classes of rounding per record",
			[]analysisSource{{"$GOROOT/src/archive/zip/reader.go", "d := make([]byte, filenameLen+extraLen+commentLen)"}, {"$GOROOT/src/archive/zip/reader.go", "f.Name = string(d[:filenameLen])"}},
			[]string{"convert.SniffContainerReader"}, "the whole container analysis",
			func(int64) int64 {
				return 2*inspectMaxZipDirBytes + (inspectMaxZipDirEntries+1)*zipDirectoryStringsPerEntry
			}},
		{"container_flate", "f.Open of a deflated part: a flate decompressor from archive/zip's flateReaderPool (the decompressor of pdf_zlib without the zlib reader) and the checksum reader",
			[]analysisSource{{container, "rc, operr := f.Open()"}, {"$GOROOT/src/archive/zip/register.go", "fr, ok := flateReaderPool.Get().(io.ReadCloser)"}},
			[]string{"inspect.AnalyzeContainerWith"}, "one part at a time",
			func(int64) int64 { return roundUp64KiB(zlibStateBytes) }},
		{"container_part_read", "one part read: io.ReadAll(io.LimitReader(rc, <= maxRelsPartBytes + 1)), go1.26.4 chunks growing 1.5x and the right-sized copy, all live at the copy",
			[]analysisSource{{container, "data, readErr := io.ReadAll(io.LimitReader(rc, limit))"}, {container, "data, readErr := io.ReadAll(io.LimitReader(rc, partLimit+1))"}, {"$GOROOT/src/io/io.go", "final := append([]byte(nil), make([]byte, finalSize)...)[:0]"}},
			[]string{"inspect.AnalyzeContainerWith"}, "the zip reader and the flate state",
			func(int64) int64 { return readAllPeak(inspectMaxRelsPartBytes + 1) }},
		{"container_part_decode", "one part decoded by encoding/xml Token (F4): the data, plus over disjoint spans of it a 64 B stack node per unclosed element (at least 3 B, no depth limit in Token), one element's Attr slice growth (48 B per at least 5 B) and the bytes.Buffer of the longest token with its string copy; decoder struct and ns map",
			[]analysisSource{{container, "dec := xml.NewDecoder(bytes.NewReader(data))"}, {"$GOROOT/src/encoding/xml/xml.go", "s = new(stack)"}, {"$GOROOT/src/encoding/xml/xml.go", "d.pushElement(t1.Name)"}},
			[]string{"inspect.AnalyzeContainerWith"}, "the zip reader and the flate state",
			func(int64) int64 { return xmlPartDecodePeak(inspectMaxRelsPartBytes + 1) }},
		{"media_isobmff_windows", "ISOBMFF findMoov: head window maxBytes - tailReserve (or size) and tail window tailReserve = min(maxBytes/4, mp4TailWindow), both live; box type strings are 4 B and dropped per box",
			[]analysisSource{{media, "headWin, headOK := readWindow(r, 0, headLen, size)"}, {media, "tailWin, tailOK := readWindow(r, tailOff, tailLen, size)"}},
			[]string{"inspect.AnalyzeMedia"}, "small objects only",
			func(w int64) int64 { return roundAlloc(w, false) + roundAlloc(min(w/4, inspectMP4TailWindow), false) }},
		{"media_ebml_window", "EBML: one window of at most ebmlInfoWindow",
			[]analysisSource{{media, "buf, ok := readWindow(r, 0, winLen, size)"}},
			[]string{"inspect.AnalyzeMedia"}, "small objects only",
			func(w int64) int64 { return roundAlloc(min(w, inspectEBMLInfoWindow), false) }},
		{"media_wav_window", "RIFF/WAV: one window of min(maxBytes, size), maxBytes = work minus the detection window",
			[]analysisSource{{media, "winLen := maxBytes"}},
			[]string{"inspect.AnalyzeMedia"}, "small objects only",
			func(w int64) int64 { return roundAlloc(w, false) }},
		{"image_window", "images: readImageWindow of min(maxBytes, size), maxBytes = work (Sniff consumes nothing); the metadata scans read it in place",
			[]analysisSource{{image, "buf, ok := readWindow(r, 0, winLen, size)"}},
			[]string{"inspect.AnalyzeImage"}, "convert.Dimensions",
			func(w int64) int64 { return roundAlloc(w, false) }},
		{"image_dimensions", "convert.Dimensions over the window: DimensionPeekBytes peek and HEIC box type strings (dimensionsHeapBytes, 78-14)",
			[]analysisSource{{image, "width, height, _, err = convert.Dimensions(format, bytes.NewReader(buf))"}},
			nil, "the image window",
			func(int64) int64 { return dimensionsHeapBytes }},
		{"cfb_classify", "ole_cfb: ClassifyCFB through NewBudgetedReaderAt(file, maxBytes) (cfbClassifyHeapBytes, 78-13)",
			[]analysisSource{{handlers, "budgeted := inspect.NewBudgetedReaderAt(file, maxBytes)"}, {handlers, "class := convert.ClassifyCFB(budgeted, size)"}},
			[]string{"convert.ClassifyCFB", "inspect.NewBudgetedReaderAt"}, "small objects only; the detection window is dead",
			func(int64) int64 { return cfbClassifyHeapBytes }},
	}
}

func inspectAnalysisRows() []analysisRow {
	var rows []analysisRow
	rows = append(rows, commonAnalysisRows()...)
	rows = append(rows, pdfAnalysisRows()...)
	rows = append(rows, otherAnalysisRows()...)
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
		{"detect", "detect_window", []string{"small_objects", "detect_sniff_window"}},
		{"detect", "detect_html", []string{"small_objects", "detect_sniff_window", "detect_html_lower"}},
		{"detect", "detect_cad", []string{"small_objects", "detect_sniff_window", "detect_cad"}},
		{"detect", "detect_video", []string{"small_objects", "detect_video_peek"}},
		{"detect", "detect_audio", []string{"small_objects", "detect_audio_peek"}},
		{"zip", "zip_bound", []string{"small_objects", "zip_bound_windows"}},
		{"container", "container_read", []string{"small_objects", "zip_reader", "zip_names", "container_flate", "container_part_read"}},
		{"container", "container_decode", []string{"small_objects", "zip_reader", "zip_names", "container_flate", "container_part_decode"}},
		{"media", "media_isobmff", []string{"small_objects", "media_isobmff_windows"}},
		{"media", "media_ebml", []string{"small_objects", "media_ebml_window"}},
		{"media", "media_wav", []string{"small_objects", "media_wav_window"}},
		{"image", "image", []string{"small_objects", "image_window", "image_dimensions"}},
		{"cfb", "cfb", []string{"small_objects", "cfb_classify"}},
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

func logAnalysisInventory(t *testing.T, work int64) {
	t.Helper()
	for _, r := range inspectAnalysisRows() {
		at := make([]string, len(r.sources))
		for i, s := range r.sources {
			at[i] = analysisSourceAt(t, s)
		}
		t.Logf("row %-28s %10d B (%6.2f MiB)  %s; live with: %s; %s", r.name, r.bound(work), mib(r.bound(work)), r.what, r.with, strings.Join(at, ", "))
	}
	for _, p := range inspectAnalysisPaths() {
		s := analysisPathBytes(t, p, work)
		t.Logf("path %-9s %-22s %10d B (%6.2f MiB) = %s", p.family, p.name, s, mib(s), strings.Join(p.rows, " + "))
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

func pdfLongKeyword(work int64) []byte {
	head := "%PDF-1.4\n"
	return []byte(head + strings.Repeat("a", int(work)-len(head)))
}

func referencePageCountRoute(gen func() []byte, rest map[string]any) func(a inspectAnalysis) (string, error) {
	var route func(a inspectAnalysis) (string, error)
	return func(a inspectAnalysis) (string, error) {
		if route == nil {
			want := map[string]any{}
			for k, v := range rest {
				want[k] = v
			}
			values, hit := pdfref805d49da.PDFTypedDictUints(gen(), "Pages", "Count", pdfMaxPagesDicts)
			switch {
			case hit:
				want["page_count_reason"] = "work_ceiling_bytes"
			case len(values) > 0:
				want["page_count"] = values[len(values)-1]
			default:
				want["page_count_reason"] = "malformed_input"
			}
			route = characteristicRoute(want)
		}
		return route(a)
	}
}

func pdfRawLiteral(work int64) []byte {
	allowance := work / 4
	open := "%PDF-1.4\n1 0 obj\n<<>>\nstream\n"
	body := "(" + strings.Repeat("a", int(allowance)-len("()Tj\n")) + ")Tj\n"
	pdf := open + body + "endstream\nendobj\n%"
	return []byte(pdf + strings.Repeat("x", int(work)-len(pdf)))
}

func pdfXrefMaps(work int64) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	for n := 1; n <= pdfMaxIndexedObjects; n++ {
		fmt.Fprintf(&b, "%d 0 obj\n", n)
	}
	first := pdfMaxIndexedObjects + 1
	const fixed = 256
	room := int(work) - b.Len() - fixed
	var payload bytes.Buffer
	var offsets []int
	for i := 0; i < pdfMaxIndexedObjects; i++ {
		header := fmt.Sprintf("%d 0 obj\n", first+i)
		if room < len(header)+int(pdfXrefEntryBytes) {
			break
		}
		room -= len(header) + int(pdfXrefEntryBytes)
		offsets = append(offsets, payload.Len())
		payload.WriteString(header)
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
	if rest := int(work) - b.Len(); rest > 1 {
		b.WriteString(strings.Repeat("%\n", rest/2))
	}
	return b.Bytes()
}

type analysisCase struct {
	name     string
	declared string
	want     string
	paths    []string
	gen      func() []byte
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
	flate := func() []byte { d, _ := pdfFlateWorst(work); return d }
	scanText := map[string]any{"text_layer": "absent"}
	if work > pdfMaxTokenScanIterations {
		scanText = map[string]any{"text_layer_reason": "work_ceiling_bytes"}
	}
	with := func(base map[string]any, kv ...any) map[string]any {
		out := map[string]any{}
		for k, v := range base {
			out[k] = v
		}
		for i := 0; i+1 < len(kv); i += 2 {
			out[kv[i].(string)] = kv[i+1]
		}
		return out
	}
	objects := func() []byte { return pdfObjects(work) }
	keyword := func() []byte { return pdfLongKeyword(work) }
	return []analysisCase{
		{"pdf_flate_worst", "pdf", "pdf", []string{"pdf_text_inflate"}, flate, characteristicRoute(map[string]any{"text_layer_reason": "work_ceiling_bytes", "page_count_reason": "malformed_input"})},
		{"pdf_ranges", "pdf", "pdf", []string{"pdf_opaque_scan"}, func() []byte { return pdfRepeated(work, "%PDF-1.4\n", "(%)\n", "") }, characteristicRoute(with(scanText, "page_count_reason", "malformed_input"))},
		{"pdf_dict_names", "pdf", "pdf", []string{"pdf_page_count_walk"}, func() []byte { return pdfDictNames(work) }, characteristicRoute(with(scanText, "page_count_reason", "malformed_input"))},
		{"pdf_objects", "pdf", "pdf", []string{"pdf_page_count_scan"}, objects, referencePageCountRoute(objects, scanText)},
		{"pdf_long_name", "pdf", "pdf", []string{"pdf_page_count_walk"}, func() []byte { return pdfLongName(work) }, characteristicRoute(with(scanText, "page_count_reason", "malformed_input"))},
		{"pdf_validated_streams", "pdf", "pdf", []string{"pdf_text_inflate"}, func() []byte { return pdfRepeated(work, "%PDF-1.4\n", ">>streamendstream", "") }, characteristicRoute(map[string]any{"text_layer_reason": "work_ceiling_bytes", "page_count_reason": "malformed_input"})},
		{"pdf_resolved_streams", "pdf", "pdf", []string{"pdf_page_count_scan"}, func() []byte { return pdfRepeated(work, "%PDF-1.4\n", "<</Length 0>>stream\nendstream\n", "") }, characteristicRoute(map[string]any{"text_layer_reason": "work_ceiling_bytes", "page_count_reason": "malformed_input"})},
		{"pdf_raw_literal", "pdf", "pdf", []string{"pdf_text_operators"}, func() []byte { return pdfRawLiteral(work) }, characteristicRoute(map[string]any{"text_layer": "present"})},
		{"pdf_xref_maps", "pdf", "pdf", []string{"pdf_page_count_scan", "pdf_page_count_merge"}, func() []byte { return pdfXrefMaps(work) }, characteristicRoute(with(scanText, "page_count_reason", "work_ceiling_bytes"))},
		{"pdf_long_keyword", "pdf", "pdf", []string{"pdf_page_count_scan"}, keyword, referencePageCountRoute(keyword, scanText)},
	}
}

type analysisMeasure struct {
	timed   inspectAnalysis
	sampled inspectAnalysis
	peak    livePeak
}

func measureInspectAnalysis(t *testing.T, c analysisCase, data []byte, work int64, duration time.Duration, half bool) (analysisMeasure, string, error) {
	t.Helper()
	f := newHookedFile(data)
	f.half = half
	size := int64(len(data))
	var m analysisMeasure
	m.timed = inspectAnalysisHeap(f, size, c.declared, work, duration)
	if m.timed.exhausted == inspect.ReasonWorkCeilingTime {
		return m, "", fmt.Errorf("the timed run stopped on the time ceiling (%s under MaxDuration %s): the sampled run measures the full path, and a path cut by time is not it", m.timed.elapsed, duration)
	}
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
		m.sampled = inspectAnalysisHeap(f, size, c.declared, work, analysisSampledMaxDuration)
	})
	if m.sampled.detected != m.timed.detected || m.sampled.exhausted != m.timed.exhausted || m.sampled.report.Coverage.Reason != m.timed.report.Coverage.Reason {
		return m, route, fmt.Errorf("the sampled run took another path: detected %q, exhausted %q, coverage %q; the timed run %q, %q, %q", m.sampled.detected, m.sampled.exhausted, m.sampled.report.Coverage.Reason, m.timed.detected, m.timed.exhausted, m.timed.report.Coverage.Reason)
	}
	if sroute, serr := c.route(m.sampled); serr != nil || sroute != route {
		return m, route, fmt.Errorf("the sampled run took another path: %s (%v)", sroute, serr)
	}
	return m, route, nil
}

const analysisChildEnv = "OCTOCONV_INSPECT_ANALYSIS_HEAP_CHILD"

const analysisRaceMaxDuration = time.Hour

const analysisSampledMaxDuration = time.Hour

func analysisChildGODEBUG() string {
	if g := os.Getenv("GODEBUG"); g != "" {
		return g + ",gcstoptheworld=1"
	}
	return "gcstoptheworld=1"
}

func runAnalysisChild(t *testing.T) {
	t.Helper()
	levels := strings.Split(t.Name(), "/")
	for i, l := range levels {
		levels[i] = "^" + regexp.QuoteMeta(l) + "$"
	}
	cmd := exec.Command(os.Args[0], "-test.run="+strings.Join(levels, "/"), "-test.count=1", "-test.v", "-test.timeout=20m")
	cmd.Env = append(os.Environ(), analysisChildEnv+"=1", "GODEBUG="+analysisChildGODEBUG())
	out, err := cmd.CombinedOutput()
	ran, failed, inside := false, false, false
	for _, line := range strings.Split(string(out), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--- PASS: "+t.Name()+" ") {
			ran = true
		}
		if strings.HasPrefix(trimmed, "--- FAIL: "+t.Name()+" ") {
			failed = true
		}
		switch {
		case trimmed == "=== RUN   "+t.Name():
			inside = true
		case strings.HasPrefix(trimmed, "--- "):
			inside = false
		case inside && trimmed != "":
			t.Log("child: " + trimmed)
		}
		if strings.HasPrefix(trimmed, "--- FAIL") || strings.HasPrefix(trimmed, "--- SKIP") || strings.HasPrefix(trimmed, "panic:") {
			t.Log("child: " + trimmed)
		}
	}
	if failed {
		t.Fatalf("the measurement of %s failed in the measurement process (GODEBUG=%s)", t.Name(), analysisChildGODEBUG())
	}
	if !ran {
		t.Fatalf("the measurement process did not pass %s: %v", t.Name(), err)
	}
	if err != nil {
		t.Logf("the measurement process exited with %v outside %s; the parent test reports its own checks", err, t.Name())
	}
}

type analysisRowCheck struct {
	what string
	call func(data []byte)
	rows []string
}

var analysisRowChecks = map[string]analysisRowCheck{
	"pdf_xref_maps": {"convert.PDFTypedDictUints alone: the scan objects map and the xref map live together in the merge", func(data []byte) {
		_, _ = convert.PDFTypedDictUints(data, "Pages", "Count", pdfMaxPagesDicts, nil)
	}, []string{"pdf_merge_maps"}},
}

func checkAnalysisRows(t *testing.T, c analysisCase, data []byte, work int64) {
	t.Helper()
	base, _, _ := strings.Cut(c.name, "@")
	rc, ok := analysisRowChecks[base]
	if !ok {
		return
	}
	byName := analysisRowsByName(t)
	var bound int64
	for _, name := range rc.rows {
		bound += byName[name].bound(work)
	}
	p := liveHeapPeak(t, func() { rc.call(data) })
	t.Logf("%s, rows %s: live peak %d B (%.2f MiB), %d GC cycles over %d calls, rows bound %d B (%.2f MiB)", rc.what, strings.Join(rc.rows, " + "), p.peak, mib(p.peak), p.cycles, p.calls, bound, mib(bound))
	if err := liveHeapPeakValid(p); err != nil {
		t.Fatal(err)
	}
	if p.peak > bound {
		t.Errorf("%s: live peak %d B is above rows %s, %d B", rc.what, p.peak, strings.Join(rc.rows, " + "), bound)
	}
}

func runAnalysisCases(t *testing.T, cases []analysisCase, work int64, duration time.Duration, term int64) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if os.Getenv(analysisChildEnv) == "" {
				runAnalysisChild(t)
				return
			}
			if !strings.Contains(os.Getenv("GODEBUG"), "gcstoptheworld=1") {
				t.Fatalf("the measurement process runs without GODEBUG gcstoptheworld=1: a concurrent mark counts objects allocated during it, live or not")
			}
			var bound int64
			var by string
			for _, name := range c.paths {
				if b := analysisPathBytes(t, analysisPathByName(t, name), work); b > bound {
					bound, by = b, name
				}
			}
			measured := duration
			if raceInstrumentedBuild() {
				measured = analysisRaceMaxDuration
			}
			data := c.gen()
			m, route, err := measureInspectAnalysis(t, c, data, work, measured, false)
			t.Logf("%s: file %d B; live peak %d B (%.2f MiB), HeapAlloc peak %d B (%.2f MiB), %d GC cycles over %d calls; live at ReadAt %.2f MiB over %d reads; %s timed, %s sampled; detected %q, exhausted %q; route %s; paths %s, bound %d B (%.2f MiB) by %s; term %d B (%.2f MiB)",
				c.name, len(data), m.peak.peak, mib(m.peak.peak), m.peak.heapAllocPeak, mib(m.peak.heapAllocPeak), m.peak.cycles, m.peak.calls, mib(m.sampled.liveReads), m.sampled.reads, m.timed.elapsed, m.sampled.elapsed, m.timed.detected, m.timed.exhausted, route, strings.Join(c.paths, ","), bound, mib(bound), by, term, mib(term))
			if err != nil {
				t.Fatal(err)
			}
			if err := liveHeapPeakValid(m.peak); err != nil {
				t.Fatal(err)
			}
			if m.peak.peak > bound {
				t.Errorf("live peak %d B is above its path %s, %d B", m.peak.peak, by, bound)
			}
			if bound > term {
				t.Errorf("path %s, %d B, is above inspectAnalysisHeapBytes(work) %d B", by, bound, term)
			}
			if m.peak.peak > term {
				t.Errorf("live peak %d B is above inspectAnalysisHeapBytes(work) %d B", m.peak.peak, term)
			}
			checkAnalysisRows(t, c, data, work)
			if measured != duration {
				t.Logf("race-instrumented build: both runs under MaxDuration %s, the %s + %s time check runs on the shipped build", measured, duration, inspectAnalyzerTimeSlack)
			} else if m.sampled.elapsed > duration {
				t.Logf("the sampled run (GOGC 1, gcstoptheworld=1) took %s under MaxDuration %s; its time is not the shipped build's, the timed run's %s is", m.sampled.elapsed, analysisSampledMaxDuration, m.timed.elapsed)
			} else if m.timed.elapsed > duration+inspectAnalyzerTimeSlack {
				t.Errorf("the analysis took %s, above MaxDuration %s + analyzerTimeSlack %s", m.timed.elapsed, duration, inspectAnalyzerTimeSlack)
			}
		})
	}
}

func TestInspectAnalysisStaysWithinItsTerm(t *testing.T) {
	work := inspectWorkBytes(t)
	duration := inspectWorkDuration(t)
	checkAnalysisSources(t)
	term := inspectAnalysisHeapBytes(work)
	derived, by := inspectAnalysisHeapTerm(t, work)
	if term != derived {
		t.Errorf("inspectAnalysisHeapBytes(%d) = %d, the inventory's largest path rounds up to %d", work, term, derived)
	}
	logAnalysisInventory(t, work)
	t.Logf("inspectAnalysisHeapBytes(%d) = %d B (%.2f MiB), set by path %s (family %s)", work, term, mib(term), by.name, by.family)
	configs := inspectConfigValues(t, "INSPECT_MAX_WORK_BYTES")
	names := make([]string, 0, len(configs))
	for name := range configs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		w := transportCapInt(t, name, "INSPECT_MAX_WORK_BYTES", configs[name])
		got, p := inspectAnalysisHeapTerm(t, w)
		t.Logf("configuration %s: work %d, inspectAnalysisHeapBytes = %d B (%.2f MiB), set by %s", name, w, got, mib(got), p.name)
	}
	for _, f := range []struct{ name, path string }{{"F4 encoding/xml Token over one rels/ODF/workbook part", "container_decode"}} {
		b := analysisPathBytes(t, analysisPathByName(t, f.path), work)
		t.Logf("%s: path %s %d B (%.2f MiB), margin to the term %d B (%.2f MiB)", f.name, f.path, b, mib(b), term-b, mib(term-b))
	}
	var cases []analysisCase
	cases = append(cases, pdfAnalysisCases(work)...)
	cases = append(cases, otherAnalysisCases(t, work)...)
	runAnalysisCases(t, cases, work, duration, term)
	for _, w := range shippedSmallerWorks(t, configs, work) {
		if os.Getenv(analysisChildEnv) == "" {
			smallerTerm, smallerBy := inspectAnalysisHeapTerm(t, w)
			t.Logf("inventory at work %d: inspectAnalysisHeapBytes(%d) = %d B (%.2f MiB), set by path %s (family %s)", w, w, smallerTerm, mib(smallerTerm), smallerBy.name, smallerBy.family)
			logAnalysisInventory(t, w)
		}
		var smaller []analysisCase
		for _, c := range pdfAnalysisCases(w) {
			c.name += fmt.Sprintf("@%dMiB", w>>20)
			smaller = append(smaller, c)
		}
		runAnalysisCases(t, smaller, w, duration, inspectAnalysisHeapBytes(w))
	}
}

func shippedSmallerWorks(t *testing.T, configs map[string]string, work int64) []int64 {
	t.Helper()
	seen := map[int64]bool{}
	var out []int64
	for name, v := range configs {
		w := transportCapInt(t, name, "INSPECT_MAX_WORK_BYTES", v)
		if w < work && !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] > out[j] })
	if len(out) == 0 {
		t.Fatalf("no shipped configuration sets INSPECT_MAX_WORK_BYTES below %d: the smaller-work subtests would run on nothing", work)
	}
	return out
}

func analysisZip(t *testing.T, entries func(zw *zip.Writer)) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	entries(zw)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func relsDenseDocx(t *testing.T, parts, partBytes int, head, unit, tail string) []byte {
	t.Helper()
	body := head + strings.Repeat(unit, (partBytes-len(head)-len(tail))/len(unit)) + tail
	return analysisZip(t, func(zw *zip.Writer) {
		mustWriteZipEntry(t, zw, "[Content_Types].xml", "<Types/>")
		mustWriteZipEntry(t, zw, "word/document.xml", "root part content")
		for i := 0; i < parts; i++ {
			mustWriteZipEntry(t, zw, fmt.Sprintf("word/_rels/p%02d.xml.rels", i), body)
		}
	})
}

func denseODFContent(t *testing.T, partBytes int) []byte {
	t.Helper()
	return analysisZip(t, func(zw *zip.Writer) {
		mw, err := zw.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := mw.Write([]byte("application/vnd.oasis.opendocument.text")); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"content.xml", "styles.xml"} {
			head, tail := `<office:document-content xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0">`, "</office:document-content>"
			mustWriteZipEntry(t, zw, name, head+strings.Repeat("<a/>", (partBytes-len(head)-len(tail))/4)+tail)
		}
	})
}

func denseWorkbookXLSX(t *testing.T, partBytes int) []byte {
	t.Helper()
	head, tail := `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheets>`, "</sheets></workbook>"
	return analysisZip(t, func(zw *zip.Writer) {
		mustWriteZipEntry(t, zw, "[Content_Types].xml", "<Types/>")
		mustWriteZipEntry(t, zw, "xl/workbook.xml", head+strings.Repeat(`<sheet/>`, (partBytes-len(head)-len(tail))/8)+tail)
	})
}

func padTo(data []byte, size int64) []byte {
	if int64(len(data)) >= size {
		return data
	}
	out := make([]byte, size)
	copy(out, data)
	return out
}

func worstTailMoovMP4(work int64) []byte {
	ftyp := isobmffTestBox("ftyp", []byte("isom\x00\x00\x02\x00isomiso2avc1mp41"))
	mvhd := make([]byte, 20)
	binary.BigEndian.PutUint32(mvhd[12:16], 1000)
	binary.BigEndian.PutUint32(mvhd[16:20], 5000)
	moov := isobmffTestBox("moov", isobmffTestBox("mvhd", mvhd))
	var b bytes.Buffer
	b.Write(ftyp)
	for i := 0; i < inspectMaxISOBMFFBoxes-16; i++ {
		b.Write(isobmffTestBox("free", nil))
	}
	size := work + work/8
	mdatLen := size - int64(b.Len()) - int64(len(moov))
	hdr := make([]byte, 8)
	binary.BigEndian.PutUint32(hdr[0:4], uint32(mdatLen))
	copy(hdr[4:8], "mdat")
	b.Write(hdr)
	b.Write(make([]byte, mdatLen-8))
	b.Write(moov)
	return b.Bytes()
}

const (
	inspectMaxISOBMFFBoxes          = 4096
	inspectMaxEBMLElements          = 512
	inspectMaxRIFFChunks            = 256
	inspectMaxMetadataMarkerScanned = 4096
)

func worstEBML(size int64) []byte {
	var seg bytes.Buffer
	for i := 0; i < inspectMaxEBMLElements+8; i++ {
		seg.Write([]byte{0xEC, 0x80})
	}
	var b bytes.Buffer
	b.Write(buildEBMLFixture("matroska"))
	b.Write([]byte{0x18, 0x53, 0x80, 0x67, 0x01, 0, 0, 0, 0, 0, 0, 0})
	binary.BigEndian.PutUint32(b.Bytes()[b.Len()-4:], uint32(seg.Len()))
	b.Write(seg.Bytes())
	return padTo(b.Bytes(), size)
}

func worstRIFF(size int64) []byte {
	var body bytes.Buffer
	body.WriteString("WAVE")
	for i := 0; i < inspectMaxRIFFChunks+8; i++ {
		body.WriteString("junk\x00\x00\x00\x00")
	}
	var b bytes.Buffer
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(size-8))
	b.Write(body.Bytes())
	return padTo(b.Bytes(), size)
}

func worstPNG(size int64) []byte {
	var b bytes.Buffer
	b.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], 0x7FFFFFFF)
	binary.BigEndian.PutUint32(ihdr[4:8], 0x7FFFFFFF)
	ihdr[8], ihdr[9] = 8, 6
	writeChunk := func(typ string, payload []byte) {
		_ = binary.Write(&b, binary.BigEndian, uint32(len(payload)))
		b.WriteString(typ)
		b.Write(payload)
		b.Write([]byte{0, 0, 0, 0})
	}
	writeChunk("IHDR", ihdr)
	for i := 0; i < inspectMaxMetadataMarkerScanned+8; i++ {
		writeChunk("abCd", nil)
	}
	return padTo(b.Bytes(), size)
}

func worstJPEG(size int64) []byte {
	var b bytes.Buffer
	b.Write([]byte{0xFF, 0xD8})
	b.Write([]byte{0xFF, 0xC0, 0x00, 0x11, 0x08, 0xFF, 0xFF, 0xFF, 0xFF, 0x03, 0x01, 0x22, 0x00, 0x02, 0x11, 0x01, 0x03, 0x11, 0x01})
	for i := 0; i < inspectMaxMetadataMarkerScanned+8; i++ {
		b.Write([]byte{0xFF, 0xE2, 0x00, 0x04, 0x00, 0x00})
	}
	return padTo(b.Bytes(), size)
}

func worstGIF(size int64) []byte {
	return padTo([]byte("GIF89a\xff\xff\xff\xff\xf7\x00\x00"), size)
}

func worstWebP(size int64) []byte {
	var body bytes.Buffer
	body.WriteString("WEBP")
	body.WriteString("VP8X")
	_ = binary.Write(&body, binary.LittleEndian, uint32(10))
	body.Write([]byte{0, 0, 0, 0, 0xFF, 0x3F, 0x00, 0xFF, 0x3F, 0x00})
	for i := 0; i < inspectMaxMetadataMarkerScanned+8; i++ {
		body.WriteString("abcd\x00\x00\x00\x00")
	}
	var b bytes.Buffer
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(size-8))
	b.Write(body.Bytes())
	return padTo(b.Bytes(), size)
}

func worstHTMLWindow() []byte {
	head := "<html>"
	return []byte(head + strings.Repeat("Ⱥ", (2*convert.SniffWindowBytes-len(head))/2))
}

func worstLaTeXWindow(t *testing.T) []byte {
	t.Helper()
	prefix, _, _ := worstTeXWindow(t)
	return append(append([]byte{}, prefix...), bytes.Repeat([]byte(" "), convert.SniffWindowBytes)...)
}

func worstMP3(t *testing.T) []byte {
	t.Helper()
	data := audioMP3Fixture(t)
	return append(append([]byte{}, data...), make([]byte, 2*convert.SniffWindowBytes)...)
}

func detectedRoute(keys ...string) func(a inspectAnalysis) (string, error) {
	return func(a inspectAnalysis) (string, error) {
		var got []string
		for _, k := range keys {
			got = append(got, fmt.Sprintf("%s=%v", k, a.report.Characteristics[k]))
		}
		var checks []string
		for _, c := range a.report.Checks {
			checks = append(checks, fmt.Sprintf("%s:%s/%s", c.Code, c.Status, c.Reason))
		}
		sort.Strings(checks)
		return strings.Join(got, " ") + " checks=" + strings.Join(checks, ",") + " coverage=" + string(a.report.Coverage.Reason) + fmt.Sprintf(" reads=%d", a.reads), nil
	}
}

func otherAnalysisCases(t *testing.T, work int64) []analysisCase {
	t.Helper()
	detect := []string{"detect_window", "detect_video", "detect_audio"}
	container := []string{"container_read", "container_decode"}
	return []analysisCase{
		{"container_docx_at_the_bound", "docx", "docx", container, func() []byte { return manyEntriesDocxFixture(t, zipEntriesAtTheBound(t)) }, detectedRoute("container_format")},
		{"rels_dense_empty", "docx", "docx", container, func() []byte {
			return relsDenseDocx(t, 64, inspectMaxRelsPartBytes-1, "<Relationships>", "<a/>", "</Relationships>")
		}, detectedRoute("container_format")},
		{"rels_dense_attr", "docx", "docx", container, func() []byte {
			return relsDenseDocx(t, 64, inspectMaxRelsPartBytes-1, "<Relationships>", `<a b="c"/>`, "</Relationships>")
		}, detectedRoute("container_format")},
		{"rels_dense_attrs", "docx", "docx", container, func() []byte {
			return relsDenseDocx(t, 64, inspectMaxRelsPartBytes-1, "<Relationships>", `<a b="" c="" d="" e="" f="" g="" h="" i=""/>`, "</Relationships>")
		}, detectedRoute("container_format")},
		{"rels_dense_ns", "docx", "docx", container, func() []byte {
			return relsDenseDocx(t, 64, inspectMaxRelsPartBytes-1, "<Relationships>", `<a xmlns:x="y"/>`, "</Relationships>")
		}, detectedRoute("container_format")},
		{"rels_nested", "docx", "docx", container, func() []byte { return relsDenseDocx(t, 64, inspectMaxRelsPartBytes-1, "", "<a>", "") }, detectedRoute("container_format")},
		{"rels_wide_attrs", "docx", "docx", container, func() []byte { return relsDenseDocx(t, 64, inspectMaxRelsPartBytes-1, "<a", ` b=""`, "/>") }, detectedRoute("container_format")},
		{"container_odf_dense", "odt", "odt", container, func() []byte { return denseODFContent(t, inspectMaxRelsPartBytes-1) }, detectedRoute("container_format")},
		{"container_xlsx_workbook_dense", "xlsx", "xlsx", container, func() []byte { return denseWorkbookXLSX(t, inspectMaxRelsPartBytes) }, detectedRoute("container_format", "sheet_count", "sheet_count_reason")},
		{"media_isobmff_boxes", "mp4", "mp4", []string{"media_isobmff"}, func() []byte { return worstTailMoovMP4(work) }, detectedRoute("duration_seconds", "duration_seconds_reason")},
		{"media_ebml", "mkv", "mkv", append([]string{"media_ebml"}, detect...), func() []byte { return worstEBML(2 * int64(convert.SniffWindowBytes)) }, detectedRoute("duration_seconds", "duration_seconds_reason")},
		{"media_riff", "wav", "wav", append([]string{"media_wav"}, detect...), func() []byte { return worstRIFF(work) }, detectedRoute("duration_seconds", "duration_seconds_reason")},
		{"media_mp3", "mp3", "mp3", detect, func() []byte { return worstMP3(t) }, detectedRoute("duration_seconds", "duration_seconds_reason")},
		{"image_png", "png", "png", []string{"image"}, func() []byte { return worstPNG(work + 1) }, detectedRoute("pixels", "pixels_reason")},
		{"image_jpeg_markers", "jpg", "jpg", []string{"image"}, func() []byte { return worstJPEG(work + 1) }, detectedRoute("pixels", "pixels_reason")},
		{"image_gif", "gif", "", detect, func() []byte { return worstGIF(work + 1) }, detectedRoute()},
		{"image_webp", "webp", "webp", []string{"image"}, func() []byte { return worstWebP(work + 1) }, detectedRoute("pixels", "pixels_reason")},
		{"text_html_window", "html", "html", []string{"detect_html"}, func() []byte { return worstHTMLWindow() }, detectedRoute()},
		{"text_latex_window", "latex", "latex", []string{"detect_window"}, func() []byte { return worstLaTeXWindow(t) }, detectedRoute()},
		{"text_markdown_window", "md", "md", []string{"detect_window"}, func() []byte { return bytes.Repeat([]byte("text "), 2*convert.SniffWindowBytes/5) }, detectedRoute()},
		{"cad_dxf_window", "dxf", "", append([]string{"detect_cad"}, detect...), func() []byte { return bytes.Repeat([]byte("\n"), 2*convert.SniffWindowBytes) }, detectedRoute()},
		{"cfb_v4_worst", "doc", inspectOLECFBFormat, []string{"detect_window", "cfb"}, func() []byte { return func() []byte { d, _ := worstCFB(12); return d }() }, detectedRoute()},
		{"zip_directory_too_large", "docx", inspectZipTooLargeFormat, []string{"zip_bound"}, func() []byte { return worstZipDirectory(worstZipDirectoryRecords) }, detectedRoute()},
	}
}

var inspectAnalysisScannedFuncs = []string{"detectInspectFormat", "dispatchInspectFamilies"}

func inspectAnalysisCalls(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "inspect_handlers.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	var calls []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		scanned := false
		for _, name := range inspectAnalysisScannedFuncs {
			if fn.Name.Name == name {
				scanned = true
			}
		}
		if !scanned {
			continue
		}
		found[fn.Name.Name] = true
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := ""
			switch fun := call.Fun.(type) {
			case *ast.SelectorExpr:
				if id, ok := fun.X.(*ast.Ident); ok {
					switch {
					case id.Name == "convert":
						name = "convert." + fun.Sel.Name
					case id.Name == "inspect" && (strings.HasPrefix(fun.Sel.Name, "Analyze") || fun.Sel.Name == "NewBudgetedReaderAt"):
						name = "inspect." + fun.Sel.Name
					}
				}
			case *ast.Ident:
				if fun.Name == "readSniffWindow" || fun.Name == "zipDirectoryExceedsStructuralBound" {
					name = fun.Name
				}
			}
			if name != "" {
				calls = append(calls, fmt.Sprintf("%s in %s at %s", name, fn.Name.Name, fset.Position(call.Pos())))
			}
			return true
		})
	}
	for _, name := range inspectAnalysisScannedFuncs {
		if !found[name] {
			t.Fatalf("inspect_handlers.go no longer declares %s: the scan would pass on nothing", name)
		}
	}
	return calls
}

func TestInspectAnalysisInventoryCoversEveryCall(t *testing.T) {
	covered := map[string]string{}
	for _, r := range inspectAnalysisRows() {
		for _, c := range r.calls {
			covered[c] = r.name
		}
	}
	calls := inspectAnalysisCalls(t)
	if len(calls) < 20 {
		t.Fatalf("the scan found %d calls in %v, want at least 20: it no longer reads what the handler calls", len(calls), inspectAnalysisScannedFuncs)
	}
	for _, c := range calls {
		name := strings.SplitN(c, " ", 2)[0]
		row, ok := covered[name]
		if !ok {
			t.Errorf("%s: no inventory row names it", c)
			continue
		}
		t.Logf("%s: row %s", c, row)
	}
}
