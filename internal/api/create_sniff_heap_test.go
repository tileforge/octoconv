package api

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
	"unsafe"

	"go.yaml.in/yaml/v3"

	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/jobs"
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
		{"zip.NewReader: EOCD search, 1024 B then 65*1024 B rounded up to 9 pages", "$GOROOT/src/archive/zip/reader.go", 0, "buf = make([]byte, int(bLen))", 1024 + 73728},
		{"zip.NewReader: EOCD comment copy, 65535 B rounded up to 8 pages", "$GOROOT/src/archive/zip/reader.go", 0, "d.comment = string(b[:l])", 65536},
		{"zip.NewReader: bufio.Reader over the directory, 4096 B buffer and the struct", "$GOROOT/src/archive/zip/reader.go", 0, "buf := bufio.NewReader(rs)", 4096 + 80},
		{"zip.NewReader: one File per record and one for the read that ends the walk, 176 B size class", "$GOROOT/src/archive/zip/reader.go", 0, "f := &File{zip: r, zipr: rdr}", records * 176},
		{"readDirectoryHeader: the 46-byte header buffer escapes, 48 B size class", "$GOROOT/src/archive/zip/reader.go", 0, "var buf [directoryHeaderLen]byte", records * 48},
		{"zip.NewReader: r.File grows by append to 4096 pointers, nextslicecap and roundupsize with the malloc header: 16 allocations up to 49152 B", "$GOROOT/src/archive/zip/reader.go", 0, "r.File = append(r.File, f)", zipFileSliceGrowthBytes},
		{"small objects: zip.Reader, directoryEnd, readerAtRecorder and nameCount (docsniff.go), readFailureRecorder (handlers.go)", "$GOROOT/src/archive/zip/reader.go", 0, "zr := new(Reader)", 1024},
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

	readme, err := os.ReadFile("../../docs/API.md")
	if err != nil {
		t.Fatal(err)
	}
	_, semantics, ok := strings.Cut(string(readme), "**Status semantics**")
	if !ok {
		t.Fatal("docs/API.md has no Status semantics section")
	}
	semantics, _, _ = strings.Cut(semantics, "\n**")
	if !strings.Contains(collapse(semantics), want) {
		t.Errorf("docs/API.md: Status semantics does not state %q; it reads %q", want, semantics)
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
	if got := roundAlloc(int64(unsafe.Sizeof(zip.File{})), true); got != 176 {
		t.Errorf("zip.File allocation size class = %d, the per-record term assumes 176", got)
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

const (
	sniffMagicHeapBytes          = 64 << 10
	htmlLowerPrefixBytes         = 1024
	htmlSniffHeapBytes           = (convert.SniffWindowBytes + htmlLowerPrefixBytes + 64<<10 - 1) &^ (64<<10 - 1)
	sniffWindowHeapBytes         = (convert.SniffWindowBytes + 1024 + 64<<10 - 1) &^ (64<<10 - 1)
	cadSplitBytes                = 106496
	cadSniffHeapBytes            = (convert.CADHeadBytes + convert.CADTailBytes + cadSplitBytes + 1024 + 64<<10 - 1) &^ (64<<10 - 1)
	cfbSniffHeapBytes            = (cfbClassifyHeapBytes + 8 + 64<<10 - 1) &^ (64<<10 - 1)
	avPeekLenBytes               = 4 * 1024
	mp3PeekLenBytes              = 512 * 1024
	videoSniffHeapBytes          = (avPeekLenBytes + 1024 + 64<<10 - 1) &^ (64<<10 - 1)
	audioSniffHeapBytes          = (mp3PeekLenBytes + 1024 + 64<<10 - 1) &^ (64<<10 - 1)
	heicBoxTypeBytes             = 4 * (convert.DimensionPeekBytes / 8) * 4
	dimensionsHeapBytes          = (convert.DimensionPeekBytes + heicBoxTypeBytes + 1024 + 64<<10 - 1) &^ (64<<10 - 1)
	contentDispositionMinParam   = 6
	contentDispositionParamBytes = 640
	contentDispositionHeapBytes  = (maxFormNonFileBytes/contentDispositionMinParam*contentDispositionParamBytes + 64<<10 - 1) &^ (64<<10 - 1)
	attachmentNamesHeapBytes     = contentDispositionHeapBytes + 64<<10
	optsHeapBytes                = 64 << 10
	acceptedResponseHeapBytes    = 64 << 10
)

type sniffRow struct {
	name       string
	calls      []string
	site       string
	bound      int64
	derivation string
	measure    func(t *testing.T) int64
}

func readerFile(data []byte) memMultipartFile { return memMultipartFile{bytes.NewReader(data)} }

func paddedFile(prefix []byte, size int) []byte {
	data := make([]byte, size)
	copy(data, prefix)
	return data
}

func worstContentDisposition(budget int) string {
	cd := `form-data; name="file"; filename="a.tex"`
	for i := 0; ; i++ {
		param := fmt.Sprintf("; k%x*0=v", i)
		if len(cd)+len(param) > budget {
			return cd
		}
		cd += param
	}
}

func worstOpts() []byte {
	inner := `"pdf_profile":"pdf/a-2b"`
	return []byte("{" + inner + strings.Repeat(" ", maxOptsBytes-len(inner)-2) + "}")
}

func createSniffInventory() []sniffRow {
	return []sniffRow{
		{"sniff_magic", []string{"convert.Sniff"}, "convert.Sniff(file)", sniffMagicHeapBytes,
			"sniffLen 12 B buffer, bytes.Reader and io.MultiReader (sniff.go:129); under 1 KiB, rounded up to 64 KiB",
			func(t *testing.T) int64 {
				f := readerFile(make([]byte, 1<<20))
				return allocatedBy(func() { _, _, _ = convert.Sniff(f) })
			}},
		{"zip_directory", []string{"sniffCreateContainer"}, "sniffCreateContainer(file, header.Size)", zipSniffHeapBytes,
			"zipSniffTerms (D-27a): inspect bound windows and zip.NewReader on at most 4096 records",
			func(t *testing.T) int64 {
				data := worstZipDirectory(inspectMaxZipDirEntries)
				binary.LittleEndian.PutUint16(data[len(data)-2:], 0xFFFF)
				data = append(data, make([]byte, 0xFFFF)...)
				f := readerFile(data)
				return allocatedBy(func() { _, _, _ = sniffCreateContainer(f, int64(len(data))) })
			}},
		{"html", []string{"convert.LooksLikeHTML"}, "convert.LooksLikeHTML(file, header.Size)", htmlSniffHeapBytes,
			"readSniffPrefix window SniffWindowBytes (markupsniff.go:98) and bytes.ToLower of at most len(\"<!doctype html\") + 1 = 15 runes of it, at most 15 x utf8.UTFMax = 60 B in and 90 B out through bytes.Map: under htmlLowerPrefixBytes 1 KiB with the small objects",
			func(t *testing.T) int64 {
				data := []byte(strings.Repeat("Ⱥ", convert.SniffWindowBytes/2))
				f := readerFile(data)
				return allocatedBy(func() { _ = convert.LooksLikeHTML(f, int64(len(data))) })
			}},
		{"latex_window", []string{"readSniffWindow", "convert.ValidateSniffPrefix", "convert.LooksLikeLaTeXFromPrefix"}, "readSniffWindow(file, header.Size)", sniffWindowHeapBytes,
			"readSniffWindow allocates min(size, SniffWindowBytes) (inspect_handlers.go readSniffWindow) and under 1 KiB of small objects; ValidateSniffPrefix and LooksLikeLaTeXFromPrefix slice it; kept as latexPrefix until the handler returns",
			func(t *testing.T) int64 {
				prefix, _, _ := worstTeXWindow(t)
				data := append(prefix, make([]byte, 1<<20)...)
				for i := convert.SniffWindowBytes; i < len(data); i++ {
					data[i] = ' '
				}
				f := readerFile(data)
				return allocatedBy(func() {
					if w, err := readSniffWindow(f, int64(len(data))); err == nil {
						if p, ok := convert.ValidateSniffPrefix(w, int64(len(data))); ok {
							_ = convert.LooksLikeLaTeXFromPrefix(p)
						}
					}
				})
			}},
		{"text", []string{"convert.LooksLikeText"}, "convert.LooksLikeText(file, header.Size)", sniffWindowHeapBytes,
			"readSniffPrefix window SniffWindowBytes (markupsniff.go:98) and under 1 KiB of small objects; hasDisallowedControlByte reads it in place",
			func(t *testing.T) int64 {
				data := bytes.Repeat([]byte("text "), (2<<20)/5)
				f := readerFile(data)
				return allocatedBy(func() { _ = convert.LooksLikeText(f, int64(len(data))) })
			}},
		{"cad", []string{"convert.ReadCADWindows", "convert.SniffCADWindows"}, "convert.ReadCADWindows(file, header.Size)", cadSniffHeapBytes,
			"CADHeadBytes and CADTailBytes windows (cadsniff.go:68, :52) and bytes.Split of the head in sniffDXF: (CADHeadBytes + 1) x 24 B slice headers = 98328 B, rounded up to 13 pages",
			func(t *testing.T) int64 {
				data := bytes.Repeat([]byte("\n"), 1<<20)
				f := readerFile(data)
				return allocatedBy(func() {
					if head, tail, err := convert.ReadCADWindows(f, int64(len(data))); err == nil {
						_ = convert.SniffCADWindows(head, tail, int64(len(data)), "dxf")
					}
				})
			}},
		{"cfb", []string{"convert.IsOLECFB", "convert.ClassifyCFB"}, "convert.IsOLECFB(file)", cfbSniffHeapBytes,
			"IsOLECFB reads 8 B (olecfb.go:24); ClassifyCFB is cfbClassifyHeapBytes (78-13)",
			func(t *testing.T) int64 {
				data, _ := worstCFB(12)
				f := readerFile(data)
				return allocatedBy(func() {
					if convert.IsOLECFB(f) {
						_ = convert.ClassifyCFB(f, int64(len(data)))
					}
				})
			}},
		{"video", []string{"convert.SniffVideo"}, "convert.SniffVideo(rest)", videoSniffHeapBytes,
			"avPeekLen 4096 B peek (avsniff.go:200) and its io.MultiReader; held by rest until the handler returns",
			func(t *testing.T) int64 {
				_, rest, _ := convert.Sniff(readerFile(make([]byte, 1<<20)))
				return allocatedBy(func() { _, _, _ = convert.SniffVideo(rest) })
			}},
		{"audio", []string{"convert.SniffAudio"}, "convert.SniffAudio(rest)", audioSniffHeapBytes,
			"mp3PeekLen 512 KiB peek (audiosniff.go:99) and its io.MultiReader; held until the handler returns",
			func(t *testing.T) int64 {
				_, rest, _ := convert.Sniff(readerFile(make([]byte, 1<<20)))
				return allocatedBy(func() { _, _, _ = convert.SniffAudio(rest) })
			}},
		{"dimensions", []string{"convert.Dimensions"}, "convert.Dimensions(detected, rest)", dimensionsHeapBytes,
			"DimensionPeekBytes peek (dimensions.go:58) and the 4-byte box type strings walkBoxes makes for heic, at most 4 levels x DimensionPeekBytes/8 boxes",
			nil},
		{"tex_gate", []string{"convert.VisitTeXPreamblePackages"}, "convert.VisitTeXPreamblePackages(latexPrefix", texScanHeapBytes,
			"texScanHeapBytes (78-13): the string(prefix) copy of the window",
			func(t *testing.T) int64 {
				prefix, _, _ := worstTeXWindow(t)
				return allocatedBy(func() {
					convert.VisitTeXPreamblePackages(prefix, func(pkg string) bool { return convert.TeXPackageResolvable(pkg) })
				})
			}},
		{"document_filename", []string{"rawPartFilename"}, "len(rawPartFilename(fhs[0]))", contentDispositionHeapBytes,
			"mime.ParseMediaType of the part's Content-Disposition (inputnames_helper.go rawPartFilename): per RFC 2231 parameter with a fresh base name a map header 48 B and a first group 288 B (mediatype.go:181, :189), the continuation and params map growth, fmt.Sprintf and the stitched value (mediatype.go:208-231), at most 640 B; at most maxFormNonFileBytes / 6 parameters (\";k*0=v\" is the shortest)",
			func(t *testing.T) int64 {
				fh := &multipart.FileHeader{Filename: "a.tex", Header: textproto.MIMEHeader{"Content-Disposition": {worstContentDisposition(maxFormNonFileBytes)}}}
				return allocatedBy(func() { _ = rawPartFilename(fh) })
			}},
		{"attachment_names", []string{"jobs.SafeInputFilename"}, "jobs.SafeInputFilename(rawPartFilename(a))", attachmentNamesHeapBytes,
			"one rawPartFilename per attachment, at most jobs.MaxInputParts - 1, all part headers together within maxFormNonFileBytes; SafeInputFilename and the names map of at most 32 names of 255 B",
			func(t *testing.T) int64 {
				var fhs []*multipart.FileHeader
				budget := maxFormNonFileBytes
				for i := 0; i < jobs.MaxInputParts-1; i++ {
					name := fmt.Sprintf("%03d", i) + strings.Repeat("n", maxInputFilenameBytes-7) + ".bib"
					cd := `form-data; name="file"; filename="` + name + `"`
					if i == 0 {
						cd = worstContentDisposition(budget - (jobs.MaxInputParts-2)*(len(cd)+8))
					}
					fhs = append(fhs, &multipart.FileHeader{Filename: name, Header: textproto.MIMEHeader{"Content-Disposition": {cd}}})
				}
				return allocatedBy(func() {
					names := map[string]bool{"doc.tex": true}
					for _, a := range fhs {
						if name, err := jobs.SafeInputFilename(rawPartFilename(a)); err == nil {
							names[name] = true
						}
					}
				})
			}},
		{"opts", []string{"convert.ParseDocOpts"}, "convert.ParseDocOpts([]byte(rawOpts))", optsHeapBytes,
			"opts at most maxOptsBytes: the []byte copy, the strict decode, json.Marshal of the normalized struct and json.Unmarshal into a map; the largest of the seven engine parsers",
			func(t *testing.T) int64 {
				raw := string(worstOpts())
				var most int64
				for _, parse := range []func([]byte) (any, error){
					func(b []byte) (any, error) { return convert.ParseDocOpts(b) },
					func(b []byte) (any, error) { return convert.ParseHTMLOpts(b) },
					func(b []byte) (any, error) { return convert.ParseAudioOpts(b) },
					func(b []byte) (any, error) { return convert.ParseAVOpts(b) },
					func(b []byte) (any, error) { return convert.ParseOCROpts(b) },
					func(b []byte) (any, error) { return convert.ParsePandocOpts(b) },
					func(b []byte) (any, error) { return convert.ParseCADOpts(b) },
				} {
					a := allocatedBy(func() {
						o, _ := parse([]byte(raw))
						normalized, _ := json.Marshal(o)
						var m map[string]any
						_ = json.Unmarshal(normalized, &m)
					})
					most = max(most, a)
				}
				return most
			}},
		{"accepted_response", []string{"writeJSON"}, "writeJSON(w, http.StatusAccepted, resp)", acceptedResponseHeapBytes,
			"the 202 body: job id, status and the normalized opts map, encoded by json.Encoder (handlers.go writeJSON)",
			func(t *testing.T) int64 {
				var opts map[string]any
				_ = json.Unmarshal([]byte(`{"pdf_profile":"pdf/a-2b"}`), &opts)
				resp := map[string]any{"job_id": "00000000-0000-0000-0000-000000000000", "status": "queued", "opts": opts}
				rec := httptest.NewRecorder()
				return allocatedBy(func() { writeJSON(rec, http.StatusAccepted, resp) })
			}},
	}
}

type dimensionsCase struct {
	format string
	data   []byte
}

func worstDimensionsInputs() []dimensionsCase {
	png := paddedFile([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR\xff\xff\xff\xff\xff\xff\xff\xff"), 1<<20)
	jpg := paddedFile([]byte{0xFF, 0xD8, 0xFF, 0xC0, 0x00, 0x11, 0x08, 0xFF, 0xFF, 0xFF, 0xFF}, 1<<20)
	webp := paddedFile([]byte("RIFF\x00\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00\x00\x00\x00\x00\xff\xff\xff\xff\xff\xff"), 1<<20)
	tiff := paddedFile([]byte{0x49, 0x49, 0x2A, 0x00, 0x08, 0x00, 0x00, 0x00, 0x02, 0x00,
		0x00, 0x01, 0x04, 0x00, 0x01, 0x00, 0x00, 0x00, 0xFF, 0xFF, 0xFF, 0xFF,
		0x01, 0x01, 0x04, 0x00, 0x01, 0x00, 0x00, 0x00, 0xFF, 0xFF, 0xFF, 0xFF}, 1<<20)
	heic := make([]byte, 1<<20)
	box := func(b []byte, size int, typ string) {
		binary.BigEndian.PutUint32(b, uint32(size))
		copy(b[4:8], typ)
	}
	peek := convert.DimensionPeekBytes
	box(heic, peek, "meta")
	box(heic[12:], peek-12, "iprp")
	box(heic[20:], peek-20, "ipco")
	for off := 28; off+8 <= peek-20; off += 8 {
		box(heic[off:], 8, "free")
	}
	box(heic[peek-20:], 20, "ispe")
	binary.BigEndian.PutUint32(heic[peek-8:], 0xFFFFFFFF)
	binary.BigEndian.PutUint32(heic[peek-4:], 0xFFFFFFFF)
	return []dimensionsCase{{"png", png}, {"jpg", jpg}, {"webp", webp}, {"tiff", tiff}, {"heic", heic}}
}

func measuredRows(t *testing.T) map[string]int64 {
	t.Helper()
	got := map[string]int64{}
	for _, row := range createSniffInventory() {
		if row.measure == nil {
			continue
		}
		got[row.name] = row.measure(t)
	}
	for _, c := range worstDimensionsInputs() {
		_, rest, _ := convert.Sniff(readerFile(c.data))
		a := allocatedBy(func() { _, _, _, _ = convert.Dimensions(c.format, rest) })
		got["dimensions"] = max(got["dimensions"], a)
	}
	return got
}

func handlersSourceLine(t *testing.T, site string) int {
	t.Helper()
	raw, err := os.ReadFile("handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, site) {
			return i + 1
		}
	}
	t.Fatalf("handlers.go has no line containing %q", site)
	return 0
}

func TestCreateSniffInventoryRowsStayWithinTheirBounds(t *testing.T) {
	for _, row := range createSniffInventory() {
		if row.measure == nil {
			continue
		}
		t.Run(row.name, func(t *testing.T) {
			got := row.measure(t)
			t.Logf("handlers.go:%d %s: allocated %d B (%.3f MiB), bound %d B (%.3f MiB): %s", handlersSourceLine(t, row.site), row.name, got, mib(got), row.bound, mib(row.bound), row.derivation)
			if got > row.bound {
				t.Errorf("%s allocated %d B on its worst input, more than its bound %d", row.name, got, row.bound)
			}
		})
	}
	for _, c := range worstDimensionsInputs() {
		t.Run("dimensions_"+c.format, func(t *testing.T) {
			_, rest, _ := convert.Sniff(readerFile(c.data))
			var w, h uint32
			var err error
			got := allocatedBy(func() { w, h, _, err = convert.Dimensions(c.format, rest) })
			t.Logf("handlers.go:%d dimensions %s: %dx%d err %v, allocated %d B, bound %d B", handlersSourceLine(t, "convert.Dimensions(detected, rest)"), c.format, w, h, err, got, dimensionsHeapBytes)
			if err != nil || uint64(w)*uint64(h) < 1<<31 {
				t.Errorf("%s: %dx%d err %v, want huge declared dimensions read without error", c.format, w, h, err)
			}
			if got > dimensionsHeapBytes {
				t.Errorf("Dimensions(%s) allocated %d B, more than dimensionsHeapBytes %d", c.format, got, dimensionsHeapBytes)
			}
		})
	}
}

type sniffPath struct {
	name string
	rows []string
	why  string
}

func createSniffPaths() []sniffPath {
	return []sniffPath{
		{"document_filename", []string{"document_filename"}, "the file name bound runs right after the form, before any sniffer"},
		{"zip", []string{"sniff_magic", "zip_directory"}, "rest keeps Sniff's prefix; the zip.Reader is dropped when sniffCreateContainer returns"},
		{"html", []string{"sniff_magic", "html"}, "the window and the lower case of its first runes die with LooksLikeHTML"},
		{"latex_gate", []string{"sniff_magic", "latex_window", "tex_gate"}, "latexPrefix is held from its read to the end; the gate copies it"},
		{"latex_attachments", []string{"sniff_magic", "latex_window", "attachment_names"}, "only tectonic takes attachments, so latexPrefix is held; the gate's copy is dead by then"},
		{"latex_accepted", []string{"sniff_magic", "latex_window", "opts", "accepted_response"}, "latexPrefix held to the 202"},
		{"latex_falls_through", []string{"sniff_magic", "latex_window", "video", "audio"}, "a text-shaped .tex that is not LaTeX keeps latexPrefix while video and audio peek"},
		{"text", []string{"sniff_magic", "text"}, "the window dies with LooksLikeText"},
		{"cad", []string{"sniff_magic", "cad"}, "head and tail die with the CAD branch"},
		{"cfb", []string{"sniff_magic", "cfb"}, "every CFB verdict returns 422"},
		{"av_accepted", []string{"sniff_magic", "video", "audio", "opts", "accepted_response"}, "rest holds both peeks to the 202"},
		{"image_accepted", []string{"sniff_magic", "dimensions", "opts", "accepted_response"}, "images are found by Sniff, so video and audio never run"},
	}
}

func createSniffHeapTerm() (term int64, sums map[string]int64) {
	bounds := map[string]int64{}
	for _, row := range createSniffInventory() {
		bounds[row.name] = row.bound
	}
	sums = map[string]int64{}
	for _, p := range createSniffPaths() {
		for _, r := range p.rows {
			sums[p.name] += bounds[r]
		}
		term = max(term, sums[p.name])
	}
	return term, sums
}

const createSniffHeapBytes = 8257536

func TestCreateSniffHeapTermCoversTheInventory(t *testing.T) {
	rows := map[string]sniffRow{}
	for _, row := range createSniffInventory() {
		rows[row.name] = row
	}
	inPath := map[string]bool{}
	for _, p := range createSniffPaths() {
		for _, r := range p.rows {
			if _, ok := rows[r]; !ok {
				t.Errorf("path %s names row %s, which the inventory does not have", p.name, r)
			}
			inPath[r] = true
		}
	}
	for name := range rows {
		if !inPath[name] {
			t.Errorf("row %s is in no path: its heap is never added to createSniffHeapBytes", name)
		}
	}
	term, sums := createSniffHeapTerm()
	measured := measuredRows(t)
	var most int64
	var mostRow string
	t.Logf("%-18s %-12s %10s %10s  %s", "row", "site", "bound", "measured", "derivation")
	for _, row := range createSniffInventory() {
		t.Logf("%-18s handlers.go:%-4d %10d %10d  %s", row.name, handlersSourceLine(t, row.site), row.bound, measured[row.name], row.derivation)
		if measured[row.name] > most {
			most, mostRow = measured[row.name], row.name
		}
	}
	for _, p := range createSniffPaths() {
		t.Logf("path %-20s %10d B (%.3f MiB) = %s: %s", p.name, sums[p.name], mib(sums[p.name]), strings.Join(p.rows, " + "), p.why)
	}
	t.Logf("createSniffHeapBytes %d B (%.3f MiB): the largest path; the largest single measurement is %s, %d B (%.3f MiB)", createSniffHeapBytes, mib(createSniffHeapBytes), mostRow, most, mib(most))
	if createSniffHeapBytes != term {
		t.Errorf("createSniffHeapBytes = %d, the largest path sums to %d", createSniffHeapBytes, term)
	}
	if createSniffHeapBytes < most {
		t.Errorf("createSniffHeapBytes %d is below the largest measurement %d (%s)", createSniffHeapBytes, most, mostRow)
	}
}

var createSnifferCall = regexp.MustCompile(`^(Sniff|LooksLike|ReadCAD|SniffCAD|IsOLECFB|ClassifyCFB|SniffVideo|SniffAudio|Dimensions|VisitTeXPreamblePackages|ValidateSniffPrefix)`)

func TestCreateSniffInventoryCoversEveryCall(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "handlers.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var body *ast.BlockStmt
	for _, d := range file.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == "handleCreateJob" {
			body = fn.Body
		}
	}
	if body == nil {
		t.Fatal("handlers.go has no handleCreateJob")
	}
	called := map[string]int{}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fun := call.Fun.(type) {
		case *ast.SelectorExpr:
			if pkg, ok := fun.X.(*ast.Ident); ok && pkg.Name == "convert" && createSnifferCall.MatchString(fun.Sel.Name) {
				called["convert."+fun.Sel.Name] = fset.Position(call.Pos()).Line
			}
			if pkg, ok := fun.X.(*ast.Ident); ok && pkg.Name == "jobs" && fun.Sel.Name == "SafeInputFilename" {
				called["jobs.SafeInputFilename"] = fset.Position(call.Pos()).Line
			}
		case *ast.Ident:
			switch fun.Name {
			case "readSniffWindow", "sniffCreateContainer", "rawPartFilename", "writeJSON":
				called[fun.Name] = fset.Position(call.Pos()).Line
			}
		}
		return true
	})
	listed := map[string]string{}
	for _, row := range createSniffInventory() {
		for _, c := range row.calls {
			listed[c] = row.name
		}
	}
	for c, line := range called {
		if _, ok := listed[c]; !ok {
			t.Errorf("handleCreateJob calls %s at handlers.go:%d and no inventory row names it", c, line)
		}
	}
	for c, row := range listed {
		if c == "convert.ParseDocOpts" {
			continue
		}
		if _, ok := called[c]; !ok {
			t.Errorf("row %s names %s, which handleCreateJob does not call", row, c)
		}
	}
	t.Logf("%d sniffer calls in handleCreateJob, every one in a row", len(called))

	for _, c := range []struct{ file, decl string }{
		{"../convert/avsniff.go", "const avPeekLen = 4 * 1024"},
		{"../convert/audiosniff.go", "const mp3PeekLen = 512 * 1024"},
	} {
		raw, err := os.ReadFile(c.file)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), c.decl) {
			t.Errorf("%s no longer declares %q: avPeekLenBytes and mp3PeekLenBytes copy those values", c.file, c.decl)
		}
	}
}
