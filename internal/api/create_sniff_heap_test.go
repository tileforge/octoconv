package api

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
	"unsafe"

	"go.yaml.in/yaml/v3"

	"github.com/tileforge/octoconv/internal/convert"
)

const cfbClassifyHeapBytes = 1 << 20

const texScanHeapBytes = convert.SniffWindowBytes + 64<<10

const zipSniffHeapBytes = 23 << 16

type heapTerm struct {
	what   string
	file   string
	line   int
	source string
	bytes  int64
}

const zipFileSliceGrowthBytes = 8 + 16 + 32 + 64 + 128 + 256 + 512 + 1152 + 2304 + 4864 + 8192 + 12288 + 18432 + 24576 + 32768 + 49152

func zipSniffTerms() []heapTerm {
	records := int64(inspectMaxZipDirEntries + 1)
	return []heapTerm{
		{"inspect bound: EOCD tail window, 66560 B rounded up to 9 pages", "internal/api/inspect_handlers.go", 0, "buf := make([]byte, tailLen)", 73728},
		{"inspect bound: directory window inspectMaxZipDirBytes, 23 pages", "internal/api/inspect_handlers.go", 0, "window := make([]byte, windowLen)", inspectMaxZipDirBytes},
		{"zip.NewReader: EOCD search, 1024 B then 65*1024 B rounded up to 9 pages", "$GOROOT/src/archive/zip/reader.go", 576, "buf = make([]byte, int(bLen))", 1024 + 73728},
		{"zip.NewReader: EOCD comment copy, 65535 B rounded up to 8 pages", "$GOROOT/src/archive/zip/reader.go", 605, "d.comment = string(b[:l])", 65536},
		{"zip.NewReader: bufio.Reader over the directory, 4096 B buffer and the struct", "$GOROOT/src/archive/zip/reader.go", 140, "buf := bufio.NewReader(rs)", 4096 + 80},
		{"zip.NewReader: one File per record and one for the read that ends the walk, 176 B size class", "$GOROOT/src/archive/zip/reader.go", 147, "f := &File{zip: r, zipr: rdr}", records * 176},
		{"readDirectoryHeader: the 46-byte header buffer escapes, 48 B size class", "$GOROOT/src/archive/zip/reader.go", 358, "var buf [directoryHeaderLen]byte", records * 48},
		{"zip.NewReader: r.File grows by append to 4096 pointers, nextslicecap and roundupsize with the malloc header: 16 allocations up to 49152 B", "$GOROOT/src/archive/zip/reader.go", 156, "r.File = append(r.File, f)", zipFileSliceGrowthBytes},
		{"small objects: zip.Reader (reader.go:111), directoryEnd (reader.go:592), readerAtRecorder and nameCount (docsniff.go), readFailureRecorder (handlers.go)", "$GOROOT/src/archive/zip/reader.go", 111, "zr := new(Reader)", 1024},
	}
}

func heapTermsSum(terms []heapTerm) int64 {
	var sum int64
	for _, term := range terms {
		sum += term.bytes
	}
	return sum
}

func roundUp64KiB(n int64) int64 {
	return (n + 64<<10 - 1) &^ (64<<10 - 1)
}

func checkHeapTermSources(t *testing.T, terms []heapTerm) {
	t.Helper()
	for _, term := range terms {
		path := strings.Replace(term.file, "$GOROOT", runtime.GOROOT(), 1)
		if !strings.HasPrefix(term.file, "$GOROOT") {
			path = "../../" + term.file
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", term.what, err)
		}
		lines := strings.Split(string(raw), "\n")
		at := term.line
		if at == 0 {
			for i, l := range lines {
				if strings.Contains(l, term.source) {
					at = i + 1
					break
				}
			}
		}
		if at == 0 || at > len(lines) || !strings.Contains(lines[at-1], term.source) {
			t.Errorf("%s: %s:%d does not read %q", term.what, term.file, at, term.source)
			continue
		}
		t.Logf("%8d B  %s:%d  %s", term.bytes, term.file, at, term.what)
	}
}

const worstZipDirectoryRecords = 182000

const (
	worstCFBFATSectors = 109
	worstCFBDirSectors = 4096
)

func worstCFB(sectorShift uint16) (data []byte, lastDirOffset int64) {
	ss := 1 << sectorShift
	buf := make([]byte, (1+worstCFBFATSectors+worstCFBDirSectors)*ss)
	h := buf[:512]
	copy(h, []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1})
	binary.LittleEndian.PutUint16(h[30:], sectorShift)
	firstDir := uint32(worstCFBFATSectors)
	binary.LittleEndian.PutUint32(h[48:], firstDir)
	binary.LittleEndian.PutUint32(h[68:], 0xFFFFFFFE)
	for i := 0; i < 109; i++ {
		v := uint32(0xFFFFFFFF)
		if i < worstCFBFATSectors {
			v = uint32(i)
		}
		binary.LittleEndian.PutUint32(h[76+i*4:], v)
	}
	per := ss / 4
	for d := 0; d < worstCFBDirSectors; d++ {
		s := firstDir + uint32(d)
		next := s + 1
		if d == worstCFBDirSectors-1 {
			next = 0xFFFFFFFE
		}
		binary.LittleEndian.PutUint32(buf[(1+int(s)/per)*ss+(int(s)%per)*4:], next)
		sec := buf[(1+int(s))*ss : (2+int(s))*ss]
		for e := 0; e < ss/128; e++ {
			ent := sec[e*128 : (e+1)*128]
			for k := 0; k < 31; k++ {
				binary.LittleEndian.PutUint16(ent[k*2:], 0x4E00)
			}
			binary.LittleEndian.PutUint16(ent[64:], 64)
			ent[66] = 1
		}
	}
	return buf, int64(1+int(firstDir)+worstCFBDirSectors-1) * int64(ss)
}

func worstZipDirectory(records int) []byte {
	var b bytes.Buffer
	local := make([]byte, 30)
	binary.LittleEndian.PutUint32(local[0:], 0x04034b50)
	b.Write(local)
	dirOff := b.Len()
	rec := make([]byte, zipCentralDirMinRecordLen)
	binary.LittleEndian.PutUint32(rec[0:], 0x02014b50)
	for i := 0; i < records; i++ {
		b.Write(rec)
	}
	dirSize := b.Len() - dirOff
	eocd := make([]byte, zipEOCDFixedLen)
	binary.LittleEndian.PutUint32(eocd[0:], 0x06054b50)
	binary.LittleEndian.PutUint16(eocd[8:], uint16(records))
	binary.LittleEndian.PutUint16(eocd[10:], uint16(records))
	binary.LittleEndian.PutUint32(eocd[12:], uint32(dirSize))
	binary.LittleEndian.PutUint32(eocd[16:], uint32(dirOff))
	b.Write(eocd)
	return b.Bytes()
}

func zipEntriesAtTheBound(t *testing.T) int {
	t.Helper()
	within := func(n int) bool {
		data := manyEntriesDocxFixture(t, n)
		outcome, _, _ := zipDirectoryExceedsStructuralBound(bytes.NewReader(data), int64(len(data)))
		return !outcome.exceeds()
	}
	lo, hi := 50, inspectMaxZipDirEntries+1
	if !within(lo) || within(hi) {
		t.Fatalf("manyEntriesDocxFixture: %d entries must be within inspect's bound and %d past it", lo, hi)
	}
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if within(mid) {
			lo = mid
		} else {
			hi = mid
		}
	}
	return lo
}

func worstTeXWindow(t *testing.T) (prefix []byte, name string, names int) {
	t.Helper()
	for _, c := range []string{"graphicx", "amsmath", "xcolor", "hyperref"} {
		if convert.TeXPackageResolvable(c) {
			name = c
			break
		}
	}
	if name == "" {
		t.Fatal("none of graphicx, amsmath, xcolor, hyperref is resolvable: the worst window cannot be built from names the gate admits")
	}
	head := `\documentclass{article}\usepackage{`
	n := (convert.SniffWindowBytes - len(head) - len(name) - 1) / (len(name) + 1)
	src := head + strings.Repeat(name+",", n) + name + "}"
	src += strings.Repeat(" ", convert.SniffWindowBytes-len(src))
	return []byte(src), name, n + 1
}

type readAtProbe struct {
	r       io.ReaderAt
	at      int64
	base    uint64
	live    int64
	reached bool
}

func (p *readAtProbe) ReadAt(b []byte, off int64) (int, error) {
	if off == p.at && !p.reached {
		p.live = int64(liveHeap()) - int64(p.base)
		p.reached = true
	}
	return p.r.ReadAt(b, off)
}

func allocatedBy(call func()) int64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	call()
	runtime.ReadMemStats(&after)
	return int64(after.TotalAlloc - before.TotalAlloc)
}

func sniffHeap(t *testing.T, r io.ReaderAt, at int64, call func(io.ReaderAt)) (allocated, liveAtRead int64) {
	t.Helper()
	p := &readAtProbe{r: r, at: at}
	p.base = liveHeap()
	allocated = allocatedBy(func() { call(p) })
	if !p.reached {
		t.Fatalf("the sniffer never read offset %d: the measurement did not reach the point it bounds", at)
	}
	return allocated, p.live
}

func TestCreateSniffersStayWithinTheirTerms(t *testing.T) {
	for _, c := range []struct {
		name  string
		shift uint16
	}{{"cfb_v4_worst", 12}, {"cfb_v3_worst", 9}} {
		t.Run(c.name, func(t *testing.T) {
			data, last := worstCFB(c.shift)
			var class convert.CFBClass
			allocated, live := sniffHeap(t, bytes.NewReader(data), last, func(r io.ReaderAt) {
				class = convert.ClassifyCFB(r, int64(len(data)))
			})
			t.Logf("sector %d B, file %d B (%.2f MiB): class %v, allocated by the call %d B (%.2f MiB), live at the last directory sector read %d B (%.2f MiB), term %d B (%.2f MiB)",
				1<<c.shift, len(data), mib(int64(len(data))), class, allocated, mib(allocated), live, mib(live), cfbClassifyHeapBytes, mib(cfbClassifyHeapBytes))
			if class != convert.CFBUnknown {
				t.Errorf("class = %v, want CFBUnknown: no entry names a known stream", class)
			}
			if allocated > cfbClassifyHeapBytes {
				t.Errorf("ClassifyCFB allocated %d B on the worst %d-byte-sector file, more than cfbClassifyHeapBytes %d", allocated, 1<<c.shift, cfbClassifyHeapBytes)
			}
			if live > cfbClassifyHeapBytes {
				t.Errorf("ClassifyCFB held %d B at the last directory sector read, more than cfbClassifyHeapBytes %d", live, cfbClassifyHeapBytes)
			}
		})
	}

	t.Run("tex_window_worst", func(t *testing.T) {
		prefix, name, want := worstTeXWindow(t)
		var unsupported string
		var visited int
		var complete bool
		allocated := allocatedBy(func() {
			complete = convert.VisitTeXPreamblePackages(prefix, func(pkg string) bool {
				visited++
				if convert.TeXPackageResolvable(pkg) {
					return true
				}
				unsupported = pkg
				return false
			})
		})
		t.Logf("window %d B of \\usepackage{%s,...}: %d names visited of %d, unsupported %q, complete %v, allocated by the gate %d B (%.2f MiB), term %d B (%.2f MiB)",
			len(prefix), name, visited, want, unsupported, complete, allocated, mib(allocated), texScanHeapBytes, mib(texScanHeapBytes))
		if len(prefix) != convert.SniffWindowBytes {
			t.Fatalf("window is %d B, want exactly convert.SniffWindowBytes %d", len(prefix), convert.SniffWindowBytes)
		}
		if unsupported != "" || visited != want || complete {
			t.Errorf("the gate visited %d of %d names, unsupported %q, complete %v: want every name visited, none unsupported, complete false", visited, want, unsupported, complete)
		}
		if allocated > texScanHeapBytes {
			t.Errorf("the TeX package gate allocated %d B on the worst window, more than texScanHeapBytes %d", allocated, texScanHeapBytes)
		}
	})

	t.Run("zip_directory_worst", func(t *testing.T) {
		data := worstZipDirectory(worstZipDirectoryRecords)
		file := memMultipartFile{bytes.NewReader(data)}
		var cr convert.ContainerResult
		var outcome zipBoundOutcome
		var err error
		allocated := allocatedBy(func() {
			cr, outcome, err = sniffCreateContainer(file, int64(len(data)))
		})
		t.Logf("%d minimal central-directory records, file %d B (%.2f MiB): outcome %v, format %q, err %v, allocated by the call %d B (%.2f MiB), term %d B (%.2f MiB)",
			worstZipDirectoryRecords, len(data), mib(int64(len(data))), outcome, cr.Format, err, allocated, mib(allocated), zipSniffHeapBytes, mib(zipSniffHeapBytes))
		if outcome != zipBoundExceedsCeiling {
			t.Errorf("outcome = %v, want zipBoundExceedsCeiling: the directory is past inspect's structural bound", outcome)
		}
		if allocated > zipSniffHeapBytes {
			t.Errorf("sniffCreateContainer allocated %d B on a %d-record directory, more than zipSniffHeapBytes %d", allocated, worstZipDirectoryRecords, zipSniffHeapBytes)
		}
	})

	t.Run("zip_directory_worst_within_bound", func(t *testing.T) {
		data := worstZipDirectory(inspectMaxZipDirEntries)
		binary.LittleEndian.PutUint16(data[len(data)-2:], 0xFFFF)
		data = append(data, make([]byte, 0xFFFF)...)
		file := memMultipartFile{bytes.NewReader(data)}
		var cr convert.ContainerResult
		var outcome zipBoundOutcome
		var err error
		allocated := allocatedBy(func() {
			cr, outcome, err = sniffCreateContainer(file, int64(len(data)))
		})
		t.Logf("%d minimal central-directory records and a 65535-byte EOCD comment, file %d B: outcome %v, format %q, err %v, allocated by the call %d B (%.3f MiB), term %d B (%.4f MiB)",
			inspectMaxZipDirEntries, len(data), outcome, cr.Format, err, allocated, mib(allocated), zipSniffHeapBytes, mib(zipSniffHeapBytes))
		if outcome != zipBoundOK || err != nil {
			t.Errorf("outcome %v, err %v: want zipBoundOK and nil, the directory is inside inspect's bound", outcome, err)
		}
		if allocated > zipSniffHeapBytes {
			t.Errorf("sniffCreateContainer allocated %d B on the worst directory inside inspect's bound, more than zipSniffHeapBytes %d", allocated, zipSniffHeapBytes)
		}
	})

	t.Run("zip_directory_at_the_bound", func(t *testing.T) {
		n := zipEntriesAtTheBound(t)
		data := manyEntriesDocxFixture(t, n)
		file := memMultipartFile{bytes.NewReader(data)}
		var cr convert.ContainerResult
		var outcome zipBoundOutcome
		var err error
		allocated := allocatedBy(func() {
			cr, outcome, err = sniffCreateContainer(file, int64(len(data)))
		})
		t.Logf("manyEntriesDocxFixture(%d), %d entries, file %d B (%.2f MiB): outcome %v, format %q, err %v, allocated by the call %d B (%.2f MiB), term %d B (%.2f MiB)",
			n, n+1, len(data), mib(int64(len(data))), outcome, cr.Format, err, allocated, mib(allocated), zipSniffHeapBytes, mib(zipSniffHeapBytes))
		if outcome != zipBoundOK || err != nil || cr.Format != "docx" {
			t.Errorf("outcome %v, err %v, format %q: want zipBoundOK, nil and docx for the largest archive inspect still reads", outcome, err, cr.Format)
		}
		if allocated > zipSniffHeapBytes {
			t.Errorf("sniffCreateContainer allocated %d B on the largest archive inspect still reads, more than zipSniffHeapBytes %d", allocated, zipSniffHeapBytes)
		}
	})
}

type zipVerdictInput struct {
	name string
	data []byte
}

func zipVerdictCorpus(t *testing.T) []zipVerdictInput {
	t.Helper()
	var in []zipVerdictInput
	for _, c := range zipDirectoryDifferentialCorpus(t) {
		in = append(in, zipVerdictInput{c.name, c.data})
	}
	n := zipEntriesAtTheBound(t)
	const nameLen = 2000
	recordLen := uint32(zipCentralDirMinRecordLen + nameLen)
	corruptSignature := docxFixture(t)
	idx := bytes.Index(corruptSignature, []byte{'P', 'K', 0x01, 0x02})
	if idx < 0 {
		t.Fatal("docxFixture has no central-directory record")
	}
	corruptSignature[idx+2], corruptSignature[idx+3] = 0x09, 0x09
	overshoot := docxFixture(t)
	binary.LittleEndian.PutUint16(overshoot[idx+30:idx+32], 0xFFF0)
	return append(in,
		zipVerdictInput{"many_entries_50", manyEntriesDocxFixture(t, 50)},
		zipVerdictInput{"many_entries_over_the_entry_ceiling", manyEntriesDocxFixture(t, inspectMaxZipDirEntries+1)},
		zipVerdictInput{"many_entries_at_the_bound", manyEntriesDocxFixture(t, n)},
		zipVerdictInput{"many_entries_one_past_the_bound", manyEntriesDocxFixture(t, n+1)},
		zipVerdictInput{"real_directory_window_exhaustion", realZipCentralDirectoryFixture(100, nameLen, 100, 100*recordLen)},
		zipVerdictInput{"real_directory_lying_dirbytes", realZipCentralDirectoryFixture(65537, 0, 1, zipCentralDirMinRecordLen)},
		zipVerdictInput{"corrupt_directory_signature", corruptSignature},
		zipVerdictInput{"record_overshoots_the_directory", overshoot},
		zipVerdictInput{"worst_directory", worstZipDirectory(worstZipDirectoryRecords)},
	)
}

func postCreateZip(t *testing.T, data []byte) (rawResult, *fakeRepo, *fakeStorage) {
	t.Helper()
	repo, store := &fakeRepo{}, &fakeStorage{}
	_, ts := newIntakeServerWith(t, repo, store, createIntakeConfig(64<<20), 30*time.Second, 32<<10)
	body, ct := multipartBody(t, "big.docx", "pdf", data)
	return rawPost(t, ts, "/v1/jobs", nil, ct, int64(body.Len()), body), repo, store
}

func createZipVerdict(t *testing.T, res rawResult, repo *fakeRepo) string {
	t.Helper()
	const mismatch = "declared format docx does not match detected content "
	switch {
	case res.status == http.StatusAccepted && repo.created != nil:
		return "format " + repo.created.SourceFormat
	case res.status != http.StatusUnprocessableEntity || res.payload["code"] != "invalid_request":
	case res.payload["error"] == createZipDirectoryTooLargeMessage:
		return "directory too large"
	case res.payload["error"] == "unrecognized file content for big.docx":
		return "not a recognized container"
	case strings.HasPrefix(res.payload["error"], mismatch):
		return "format " + strings.TrimPrefix(res.payload["error"], mismatch)
	}
	t.Fatalf("create answered %d %v: not a verdict about the archive", res.status, res.payload)
	return ""
}

func inspectZipVerdict(t *testing.T, srv *Server, data []byte) string {
	t.Helper()
	status, body := doInspect(t, srv, "big.docx", data)
	if status != http.StatusOK {
		t.Fatalf("inspect answered %d: %s", status, body)
	}
	var resp inspectResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode inspect body: %v", err)
	}
	switch resp.Format {
	case inspectZipTooLargeFormat:
		return "directory too large"
	case inspectZipReadFailedFormat, inspectZipMalformedFormat, "":
		return "not a recognized container"
	}
	return "format " + resp.Format
}

func TestCreateRefusesAnOversizedZipDirectory(t *testing.T) {
	data := worstZipDirectory(worstZipDirectoryRecords)
	res, repo, store := postCreateZip(t, data)
	t.Logf("big.docx with %d minimal central-directory records (%d B): status %d %v", worstZipDirectoryRecords, len(data), res.status, res.payload)
	if res.status != http.StatusUnprocessableEntity || res.payload["code"] != "invalid_request" || res.payload["error"] != createZipDirectoryTooLargeMessage {
		t.Errorf("status %d payload %v, want 422 invalid_request %q", res.status, res.payload, createZipDirectoryTooLargeMessage)
	}
	if store.uploaded {
		t.Error("the archive reached storage")
	}
	if repo.created != nil {
		t.Errorf("a job was created: %+v", repo.created)
	}
}

func TestCreateAndInspectAgreeOnZipDirectories(t *testing.T) {
	inspectSrv, _ := newIntakeServer(t, createIntakeConfig(64<<20), 30*time.Second, 32<<10)
	for _, in := range zipVerdictCorpus(t) {
		t.Run(in.name, func(t *testing.T) {
			res, repo, _ := postCreateZip(t, in.data)
			created := createZipVerdict(t, res, repo)
			inspected := inspectZipVerdict(t, inspectSrv, in.data)
			t.Logf("%d B: create %q (status %d), inspect %q", len(in.data), created, res.status, inspected)
			if created != inspected {
				t.Errorf("create says %q, inspect says %q: one file, two verdicts", created, inspected)
			}
		})
	}
}

func TestZipDirectoryRefusalIsStatedInTheContracts(t *testing.T) {
	want := createZipDirectoryTooLargeMessage
	collapse := func(s string) string { return strings.Join(strings.Fields(s), " ") }

	codes, err := os.ReadFile("../../docs/ERROR-CODES.md")
	if err != nil {
		t.Fatal(err)
	}
	var row string
	for _, line := range strings.Split(string(codes), "\n") {
		if strings.HasPrefix(line, "| `invalid_request` |") {
			row = line
		}
	}
	if row == "" {
		t.Fatal("docs/ERROR-CODES.md has no invalid_request row")
	}
	if !strings.Contains(row, want) {
		t.Errorf("docs/ERROR-CODES.md: the invalid_request row does not state %q; it reads %q", want, row)
	}
	if !strings.Contains(row, "422") {
		t.Errorf("docs/ERROR-CODES.md: the invalid_request row does not name 422, the status create refuses the archive with; it reads %q", row)
	}

	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode %s: %v", specPath, err)
	}
	if d := openAPIString(t, doc, "paths", "/v1/jobs", "post", "responses", "422", "description"); !strings.Contains(collapse(d), want) {
		t.Errorf("%s: the 422 of POST /v1/jobs does not state %q; it reads %q", specPath, want, d)
	}

	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	_, semantics, ok := strings.Cut(string(readme), "**Status semantics**")
	if !ok {
		t.Fatal("README.md has no Status semantics section")
	}
	semantics, _, _ = strings.Cut(semantics, "\n**")
	if !strings.Contains(collapse(semantics), want) {
		t.Errorf("README.md: Status semantics does not state %q; it reads %q", want, semantics)
	}
}

func TestZipSniffHeapTermIsDerived(t *testing.T) {
	terms := zipSniffTerms()
	checkHeapTermSources(t, terms)
	sum := heapTermsSum(terms)
	t.Logf("sum %d B (%.3f MiB), rounded up to 64 KiB %d B, zipSniffHeapBytes %d B (%.4f MiB)", sum, mib(sum), roundUp64KiB(sum), zipSniffHeapBytes, mib(zipSniffHeapBytes))
	if roundUp64KiB(sum) != zipSniffHeapBytes {
		t.Errorf("zipSniffHeapBytes = %d, the terms sum to %d and round up to %d", zipSniffHeapBytes, sum, roundUp64KiB(sum))
	}
	if got := int64(unsafe.Sizeof(zip.File{})); got != 176 {
		t.Errorf("unsafe.Sizeof(zip.File{}) = %d, the per-record term assumes 176", got)
	}
	var files []*zip.File
	growth := allocatedBy(func() {
		for i := 0; i < inspectMaxZipDirEntries; i++ {
			files = append(files, nil)
		}
	})
	runtime.KeepAlive(files)
	t.Logf("append of %d *zip.File from nil: %d B allocated, term %d B", inspectMaxZipDirEntries, growth, zipFileSliceGrowthBytes)
	if growth > zipFileSliceGrowthBytes {
		t.Errorf("growing []*zip.File to %d entries allocated %d B, more than zipFileSliceGrowthBytes %d", inspectMaxZipDirEntries, growth, zipFileSliceGrowthBytes)
	}
}

type flakyReadAtFile struct {
	*bytes.Reader
	failOn map[int]bool
	reads  int
}

func (f *flakyReadAtFile) ReadAt(p []byte, off int64) (int, error) {
	f.reads++
	if f.failOn[f.reads] {
		return 0, errors.New("injected read failure")
	}
	return f.Reader.ReadAt(p, off)
}

func (f *flakyReadAtFile) Close() error { return nil }

func TestSniffCreateContainerNeverReportsOKForUnexaminedInput(t *testing.T) {
	docx := docxFixture(t)
	noEOCD := append([]byte{'P', 'K', 3, 4}, make([]byte, 100)...)
	for _, c := range []struct {
		name    string
		data    []byte
		failOn  map[int]bool
		want    zipBoundOutcome
		format  string
		maxRead int
	}{
		{"valid_docx", docx, nil, zipBoundOK, "docx", 0},
		{"bare_zip", bareZipFixture(t), nil, zipBoundOK, "", 0},
		{"tail_read_fails_once", docx, map[int]bool{1: true}, zipBoundReadFailed, "", 1},
		{"directory_read_fails_after_the_bound", docx, map[int]bool{3: true}, zipBoundReadFailed, "", 0},
		{"shorter_than_an_eocd", []byte{'P', 'K', 3, 4}, nil, zipBoundMalformed, "", 0},
		{"no_eocd", noEOCD, nil, zipBoundMalformed, "", 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			file := &flakyReadAtFile{Reader: bytes.NewReader(c.data), failOn: c.failOn}
			cr, outcome, err := sniffCreateContainer(file, int64(len(c.data)))
			t.Logf("%d B: outcome %v, format %q, err %v, reads %d", len(c.data), outcome, cr.Format, err, file.reads)
			if outcome != c.want || cr.Format != c.format {
				t.Errorf("outcome %v format %q, want %v %q", outcome, cr.Format, c.want, c.format)
			}
			if outcome == zipBoundOK && err != nil {
				t.Errorf("zipBoundOK with error %v", err)
			}
			if c.maxRead > 0 && file.reads > c.maxRead {
				t.Errorf("%d reads after the bound could not read the tail: the archive was parsed without the bound having examined it", file.reads)
			}
		})
	}
}
