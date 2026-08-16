package convert

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"testing"
)

// mustWriteEntry adds a single deflate-compressed entry with the given
// content to zw, failing the test on any write error.
func mustWriteEntry(t *testing.T, zw *zip.Writer, name, content string) {
	t.Helper()
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
	if err != nil {
		t.Fatalf("CreateHeader(%q): %v", name, err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatalf("Write(%q): %v", name, err)
	}
}

// ooxmlZipFixture builds a zip containing the given OOXML root part, but
// preceded by 8 filler entries (mirroring the real pandoc-produced pptx
// where [Content_Types].xml sits at central-directory index 8, not 0) --
// proving SniffContainer's root-part-presence-by-name check is
// position-independent.
func ooxmlZipFixture(t *testing.T, rootPart string) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	mustWriteEntry(t, zw, "[Content_Types].xml", "<Types/>")
	for i := 0; i < 7; i++ {
		mustWriteEntry(t, zw, fmt.Sprintf("filler/part%d.xml", i), "filler")
	}
	mustWriteEntry(t, zw, rootPart, "root part content")
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	return buf.Bytes()
}

// odfZipFixture builds a zip whose first entry is "mimetype" (stored,
// uncompressed) carrying the given payload, per OASIS ODF v1.2 Part 3
// §17.4.
func odfZipFixture(t *testing.T, mimetype string) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	fw, err := zw.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		t.Fatalf("CreateHeader(mimetype): %v", err)
	}
	if _, err := fw.Write([]byte(mimetype)); err != nil {
		t.Fatalf("Write(mimetype): %v", err)
	}
	mustWriteEntry(t, zw, "META-INF/manifest.xml", "<manifest/>")
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	return buf.Bytes()
}

// odfWithBasicFixture builds an ODF-shaped zip (mimetype-at-index-0, per
// odfZipFixture) plus an additional macro-storage entry -- covers ODF's
// Basic/-prefixed macro detection (a directory-prefix match, distinct from
// OOXML's literal vbaProject.bin path match).
func odfWithBasicFixture(t *testing.T, mimetype, basicEntry string) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	fw, err := zw.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		t.Fatalf("CreateHeader(mimetype): %v", err)
	}
	if _, err := fw.Write([]byte(mimetype)); err != nil {
		t.Fatalf("Write(mimetype): %v", err)
	}
	mustWriteEntry(t, zw, basicEntry, "Sub Foo\nEnd Sub\n")
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	return buf.Bytes()
}

// macroZipFixture builds a minimal, valid-shaped docx (word/document.xml
// root part present) plus the given macro-part entry -- covers OOXML's
// literal vbaProject.bin path match.
func macroZipFixture(t *testing.T, macroPart string) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	mustWriteEntry(t, zw, "word/document.xml", "root part content")
	mustWriteEntry(t, zw, macroPart, "macro content")
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	return buf.Bytes()
}

// duplicateNameZipFixture builds a zip with two entries sharing the exact
// same name -- reused for both the duplicate-root-part and the
// duplicate-macro-part scenarios, since SniffContainer's fail-closed guard
// (RESEARCH.md Pitfall 3) covers both name classes via one shared counter.
func duplicateNameZipFixture(t *testing.T, name string) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	mustWriteEntry(t, zw, name, "first copy")
	mustWriteEntry(t, zw, name, "second copy, different content")
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	return buf.Bytes()
}

// zipBombFixture builds a single deflate-compressed entry whose declared
// UncompressedSize64 equals declaredSize, written from real (highly
// compressible, all-zero) content so archive/zip.Writer computes the size
// itself rather than trusting a caller-spoofed header field.
func zipBombFixture(t *testing.T, declaredSize uint64) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	w, err := zw.CreateHeader(&zip.FileHeader{Name: "word/document.xml", Method: zip.Deflate})
	if err != nil {
		t.Fatalf("CreateHeader: %v", err)
	}
	chunk := make([]byte, 1<<20) // 1 MiB of zeros, reused per write
	var written uint64
	for written < declaredSize {
		n := declaredSize - written
		if n > uint64(len(chunk)) {
			n = uint64(len(chunk))
		}
		if _, err := w.Write(chunk[:n]); err != nil {
			t.Fatalf("Write: %v", err)
		}
		written += n
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	return buf.Bytes()
}

func TestSniffContainer_DOCX(t *testing.T) {
	data := ooxmlZipFixture(t, "word/document.xml")
	res, err := SniffContainer(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("SniffContainer error: %v", err)
	}
	if res.Format != "docx" {
		t.Fatalf("Format = %q, want docx", res.Format)
	}
}

func TestSniffContainer_XLSX(t *testing.T) {
	data := ooxmlZipFixture(t, "xl/workbook.xml")
	res, err := SniffContainer(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("SniffContainer error: %v", err)
	}
	if res.Format != "xlsx" {
		t.Fatalf("Format = %q, want xlsx", res.Format)
	}
}

func TestSniffContainer_PPTX(t *testing.T) {
	data := ooxmlZipFixture(t, "ppt/presentation.xml")
	res, err := SniffContainer(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("SniffContainer error: %v", err)
	}
	if res.Format != "pptx" {
		t.Fatalf("Format = %q, want pptx", res.Format)
	}
}

func TestSniffContainer_ODT(t *testing.T) {
	cases := map[string]string{
		"application/vnd.oasis.opendocument.text":         "odt",
		"application/vnd.oasis.opendocument.spreadsheet":  "ods",
		"application/vnd.oasis.opendocument.presentation": "odp",
	}
	for mimetype, want := range cases {
		data := odfZipFixture(t, mimetype)
		res, err := SniffContainer(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("SniffContainer error for %q: %v", mimetype, err)
		}
		if res.Format != want {
			t.Fatalf("mimetype %q: Format = %q, want %q", mimetype, res.Format, want)
		}
	}
}

func TestSniffContainer_BareZipUnrecognized(t *testing.T) {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	mustWriteEntry(t, zw, "readme.txt", "hello")
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	data := buf.Bytes()

	res, err := SniffContainer(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("SniffContainer error: %v", err)
	}
	if res.Format != "" {
		t.Fatalf("Format = %q, want \"\" for unrecognized zip", res.Format)
	}
}

func TestSniffContainer_ZipBombDeclaredSize(t *testing.T) {
	const declared uint64 = 20 * 1024 * 1024 // 20 MiB
	data := zipBombFixture(t, declared)

	res, err := SniffContainer(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("SniffContainer error: %v", err)
	}
	if res.TotalUncompressed != declared {
		t.Fatalf("TotalUncompressed = %d, want %d", res.TotalUncompressed, declared)
	}
}

func TestSniffContainer_MacroDetectedOOXML(t *testing.T) {
	data := macroZipFixture(t, "word/vbaProject.bin")
	res, err := SniffContainer(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("SniffContainer error: %v", err)
	}
	if !res.HasMacro {
		t.Fatal("HasMacro = false, want true for OOXML vbaProject.bin")
	}
}

func TestSniffContainer_MacroDetectedODF(t *testing.T) {
	data := odfWithBasicFixture(t, "application/vnd.oasis.opendocument.text", "Basic/Standard/script-lb.xml")
	res, err := SniffContainer(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("SniffContainer error: %v", err)
	}
	if !res.HasMacro {
		t.Fatal("HasMacro = false, want true for ODF Basic/ entry")
	}
}

func TestSniffContainer_DuplicateRootPartRejected(t *testing.T) {
	data := duplicateNameZipFixture(t, "word/document.xml")
	res, err := SniffContainer(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("SniffContainer error: %v", err)
	}
	if !res.DuplicateRootPart {
		t.Fatal("DuplicateRootPart = false, want true for duplicated word/document.xml")
	}
}

func TestSniffContainer_DuplicateMacroPartRejected(t *testing.T) {
	data := duplicateNameZipFixture(t, "word/vbaProject.bin")
	res, err := SniffContainer(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("SniffContainer error: %v", err)
	}
	if !res.DuplicateRootPart {
		t.Fatal("DuplicateRootPart = false, want true for duplicated word/vbaProject.bin")
	}
}

func TestSniffContainer_NotAZip(t *testing.T) {
	data := []byte("this is definitely not a zip file, no PK signature here")
	_, err := SniffContainer(bytes.NewReader(data), int64(len(data)))
	if !errors.Is(err, ErrNotAZip) {
		t.Fatalf("err = %v, want ErrNotAZip", err)
	}
}

// failingAfterOffsetReaderAt wraps r, delegating every ReadAt call whose
// offset is below failAt unchanged, but returning failErr for any call at
// or past failAt -- models a genuine i/o failure (e.g. a disk read error on
// a spooled temp file, or a client connection reset mid-read) that begins
// partway through a file, as opposed to a legitimate io.EOF at the file's
// own end.
type failingAfterOffsetReaderAt struct {
	r       io.ReaderAt
	failAt  int64
	failErr error
}

func (f *failingAfterOffsetReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off >= f.failAt {
		return 0, f.failErr
	}
	return f.r.ReadAt(p, off)
}

// TestSniffContainer_ReadFailureIsNotErrNotAZip is PR9R8-F3's own
// docsniff-level proof: a genuine io.ReaderAt failure occurring AFTER the
// 4-byte PK\x03\x04 prefix (which detectInspectFormat, inspect_handlers.go,
// has already read successfully by the time it calls SniffContainer) must
// surface as an error that is NOT ErrNotAZip -- ErrNotAZip is reserved for
// a verdict about the file's own bytes (see that sentinel's own doc
// comment), and a read failure gives no grounds for one.
func TestSniffContainer_ReadFailureIsNotErrNotAZip(t *testing.T) {
	data := ooxmlZipFixture(t, "word/document.xml")
	failErr := errors.New("simulated disk read failure")
	r := &failingAfterOffsetReaderAt{r: bytes.NewReader(data), failAt: 4, failErr: failErr}

	_, err := SniffContainer(r, int64(len(data)))
	if err == nil {
		t.Fatal("SniffContainer err = nil, want a read-failure error")
	}
	if errors.Is(err, ErrNotAZip) {
		t.Fatalf("err = %v classified as ErrNotAZip (structural), want an operational read failure -- a genuine ReadAt failure must never be reported as a property of the file (PR9R8-F3/F4)", err)
	}
	if !errors.Is(err, failErr) {
		t.Fatalf("err = %v does not wrap the original failErr -- the underlying cause must stay diagnosable", err)
	}
}

// writeUint16 / writeUint32 append a little-endian field -- the ZIP central
// directory / EOCD record's own byte order -- to buf.
func writeUint16(buf *bytes.Buffer, v uint16) {
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], v)
	buf.Write(b[:])
}

func writeUint32(buf *bytes.Buffer, v uint32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	buf.Write(b[:])
}

// truncatedCentralDirectoryHeaderFixture builds a minimal, otherwise
// well-formed ZIP whose SOLE central-directory file header declares a
// filename length (200) far longer than the bytes actually present before
// the EOCD record that immediately follows it -- codex PR #9 round 8's own
// reviewer scenario (docsniff.go:93): archive/zip's own directory-header
// reader (readDirectoryHeader, via bufio/io.ReadFull) runs out of
// underlying bytes partway through the filename field and returns
// io.ErrUnexpectedEOF -- a STRUCTURAL fact about this file's own central
// directory (the header's own declared length does not match its own
// content), never a failure of the io.ReaderAt call that delivered every
// byte it was ever asked for (each such read legitimately hits real
// end-of-file, io.EOF, not a fabricated failure).
func truncatedCentralDirectoryHeaderFixture(t *testing.T) []byte {
	t.Helper()
	var header bytes.Buffer
	header.WriteString("PK\x01\x02") // signature
	header.Write(make([]byte, 2))    // version made by
	header.Write(make([]byte, 2))    // version needed
	header.Write(make([]byte, 2))    // general purpose flags
	header.Write(make([]byte, 2))    // compression method
	header.Write(make([]byte, 2))    // mod time
	header.Write(make([]byte, 2))    // mod date
	header.Write(make([]byte, 4))    // crc32
	header.Write(make([]byte, 4))    // compressed size
	header.Write(make([]byte, 4))    // uncompressed size
	writeUint16(&header, 200)        // filename length -- LIES: no 200 bytes follow
	writeUint16(&header, 0)          // extra field length
	writeUint16(&header, 0)          // file comment length
	header.Write(make([]byte, 2))    // disk number start
	header.Write(make([]byte, 2))    // internal file attrs
	header.Write(make([]byte, 4))    // external file attrs
	header.Write(make([]byte, 4))    // relative offset of local header
	if header.Len() != 46 {
		t.Fatalf("central directory header fixed portion = %d bytes, want 46", header.Len())
	}
	header.WriteString("short") // only 5 of the declared 200 filename bytes

	dirBytes := uint32(header.Len())

	var eocd bytes.Buffer
	eocd.WriteString("PK\x05\x06")
	writeUint16(&eocd, 0) // disk number
	writeUint16(&eocd, 0) // disk with the start of the central directory
	writeUint16(&eocd, 1) // entries on this disk
	writeUint16(&eocd, 1) // total entries
	writeUint32(&eocd, dirBytes)
	writeUint32(&eocd, 0) // directory offset -- the (truncated) header starts at 0
	writeUint16(&eocd, 0) // comment length

	out := append([]byte{}, header.Bytes()...)
	return append(out, eocd.Bytes()...)
}

// inconsistentEOCDCommentLengthFixture builds a minimal, otherwise
// well-formed EOCD record (empty central directory: zero entries) whose OWN
// declared comment length (500) exceeds the bytes actually available after
// it -- codex PR #9 round 8's own reviewer scenario. The finding's own
// prose names archive/zip's readDirectoryEnd's errors.New("zip: invalid
// comment length") (reader.go:603) as the resulting error; empirically
// (verified by direct construction against this project's own vendored Go
// 1.26 toolchain, plus a line-by-line read of $GOROOT/src/archive/zip/
// reader.go) that specific check is unreachable in THIS Go version: its own
// signature locator, findSignatureInBlock (reader.go:697-713), already
// performs the mathematically IDENTICAL truncation check (n+22+i>len(b),
// same n, same window) BEFORE readDirectoryEnd ever reaches its own later
// check, so any input that would trip the later check has already made
// findSignatureInBlock itself report "no EOCD signature found" first,
// yielding zip.ErrFormat (reader.go:586) instead. This fixture still
// exercises the SAME reviewer-named defect class (an EOCD whose own
// declared comment length cannot possibly be honest) and still proves the
// property PR9R8-F4 requires -- see
// TestSniffContainer_InconsistentEOCDCommentLengthIsStructuralNotReadFailure's
// own doc comment for what is actually asserted and why.
func inconsistentEOCDCommentLengthFixture(t *testing.T) []byte {
	t.Helper()
	var eocd bytes.Buffer
	eocd.WriteString("PK\x05\x06")
	writeUint16(&eocd, 0)   // disk number
	writeUint16(&eocd, 0)   // disk with the start of the central directory
	writeUint16(&eocd, 0)   // entries on this disk
	writeUint16(&eocd, 0)   // total entries
	writeUint32(&eocd, 0)   // directory size
	writeUint32(&eocd, 0)   // directory offset
	writeUint16(&eocd, 500) // comment length -- LIES: no comment bytes follow
	return eocd.Bytes()
}

// TestSniffContainer_TruncatedCentralDirectoryHeaderIsStructuralNotReadFailure
// is PR9R8-F4-STRUCTURAL-VS-IO's own central proof, half one: io.ErrUnexpectedEOF
// -- never checked against any of the three sentinels
// (zip.ErrFormat/zip.ErrAlgorithm/zip.ErrChecksum) the earlier version of
// SniffContainer allowlisted -- must still classify as ErrNotAZip
// (structural), because docsniff.go's own readerAtRecorder observed every
// ReadAt call it made succeed.
func TestSniffContainer_TruncatedCentralDirectoryHeaderIsStructuralNotReadFailure(t *testing.T) {
	data := truncatedCentralDirectoryHeaderFixture(t)

	_, err := SniffContainer(bytes.NewReader(data), int64(len(data)))
	if err == nil {
		t.Fatal("SniffContainer err = nil, want an error for a truncated central-directory header")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err = %v does not wrap io.ErrUnexpectedEOF -- test fixture assumption broken (archive/zip's own behavior may have changed)", err)
	}
	if !errors.Is(err, ErrNotAZip) {
		t.Fatalf("err = %v classified as an operational read failure, want ErrNotAZip (structural) -- io.ErrUnexpectedEOF here comes from a genuine io.EOF this test's own bytes.Reader legitimately returned at the real end of the fixture, never a fabricated ReadAt failure (PR9R8-F4)", err)
	}
}

// TestSniffContainer_InconsistentEOCDCommentLengthIsStructuralNotReadFailure
// is PR9R8-F4-STRUCTURAL-VS-IO's own central proof, half two -- and, in its
// own way, an even sharper one than a specific named error string could be:
// it proves the classification does not depend on WHICH structural error
// archive/zip happens to surface for an EOCD whose own declared comment
// length cannot possibly be honest, only on whether docsniff.go's own
// readerAtRecorder ever observed a real read failure (see
// inconsistentEOCDCommentLengthFixture's own doc comment for why the
// specific error this Go version actually returns, zip.ErrFormat, differs
// from the finding's own quoted prose, and why that gap does not weaken
// this proof: zip.ErrFormat was among the three ORIGINAL sentinels the
// earlier version of SniffContainer already handled, so this input alone
// would not, by itself, prove the new mechanism generalizes -- see the
// SIBLING test above (io.ErrUnexpectedEOF, never on that original list) for
// the case that does. This test's own job is narrower and still real: an
// EOCD that lies about its own comment length must never be reported as a
// read failure, regardless of which exact Go error value carries that
// verdict.
func TestSniffContainer_InconsistentEOCDCommentLengthIsStructuralNotReadFailure(t *testing.T) {
	data := inconsistentEOCDCommentLengthFixture(t)

	_, err := SniffContainer(bytes.NewReader(data), int64(len(data)))
	if err == nil {
		t.Fatal("SniffContainer err = nil, want an error for an EOCD with an inconsistent comment length")
	}
	if !errors.Is(err, ErrNotAZip) {
		t.Fatalf("err = %v classified as an operational read failure, want ErrNotAZip (structural) -- an EOCD lying about its own comment length must never be reported as a read failure (PR9R8-F4)", err)
	}
}

// TestReaderAtRecorder_ObservesOnlyRealReadFailures is PR9R8-F4's own most
// direct proof, exercising docsniff.go's own readerAtRecorder in isolation
// from archive/zip entirely -- the reviewer's own suggested landmark for
// this fix ("a wrapper over io.ReaderAt, remembering its own failure; then
// 'was there a read failure' is a fact, not a guess"). A genuine (non-EOF)
// ReadAt error marks failed and preserves the original error for
// diagnosis; a legitimate io.EOF at the real end of input -- exactly what
// every ReadAt call inside a successful (or purely structurally-broken)
// parse eventually hits -- never does, no matter how many times it occurs.
func TestReaderAtRecorder_ObservesOnlyRealReadFailures(t *testing.T) {
	t.Run("EOF is not a failure", func(t *testing.T) {
		rr := &readerAtRecorder{r: bytes.NewReader([]byte("hi"))}
		buf := make([]byte, 10)
		if _, err := rr.ReadAt(buf, 0); !errors.Is(err, io.EOF) {
			t.Fatalf("ReadAt err = %v, want io.EOF (bytes.Reader's own short-read behavior)", err)
		}
		if rr.failed {
			t.Fatal("failed = true after a legitimate io.EOF, want false")
		}
	})

	t.Run("a real error is recorded and preserved", func(t *testing.T) {
		want := errors.New("simulated disk failure")
		rr := &readerAtRecorder{r: &failingAfterOffsetReaderAt{r: bytes.NewReader([]byte("hi")), failAt: 0, failErr: want}}
		buf := make([]byte, 2)
		if _, err := rr.ReadAt(buf, 0); !errors.Is(err, want) {
			t.Fatalf("ReadAt err = %v, want it to wrap %v", err, want)
		}
		if !rr.failed {
			t.Fatal("failed = false after a genuine ReadAt error, want true")
		}
		if !errors.Is(rr.err, want) {
			t.Fatalf("rr.err = %v, want it to wrap %v", rr.err, want)
		}
	})

	t.Run("the FIRST failure is sticky, not overwritten by a later success", func(t *testing.T) {
		first := errors.New("first failure")
		rr := &readerAtRecorder{r: bytes.NewReader([]byte("hello world"))}
		// Record a real failure directly (bypassing r) to simulate the
		// first ReadAt call failing...
		rr.failed = true
		rr.err = first
		// ...then a LATER call that legitimately succeeds must not erase
		// the first recorded cause -- the caller only checks failed/err
		// AFTER the parser has already given up, at which point the FIRST
		// failure is the one that actually explains why.
		buf := make([]byte, 5)
		if _, err := rr.ReadAt(buf, 0); err != nil {
			t.Fatalf("ReadAt err = %v, want nil for a genuinely successful later read", err)
		}
		if !errors.Is(rr.err, first) {
			t.Fatalf("rr.err = %v, want the FIRST recorded failure (%v) to survive a later successful read", rr.err, first)
		}
	})
}
