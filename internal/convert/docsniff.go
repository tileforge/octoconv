package convert

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"math"
)

// ContainerResult is the outcome of a single ZIP-central-directory pass used
// to disambiguate the six OOXML/ODF office formats and simultaneously
// gather the zip-bomb (D-03) and macro-part (D-05) safety signals.
type ContainerResult struct {
	Format            string // "docx"|"xlsx"|"pptx"|"odt"|"ods"|"odp"|"epub"|"" (unrecognized)
	TotalUncompressed uint64
	HasMacro          bool
	DuplicateRootPart bool // fail-closed signal (RESEARCH.md Pitfall 3)
}

// ErrNotAZip is returned when SniffContainer's input cannot be parsed as a
// ZIP central directory at all (as opposed to being a valid-but-unrecognized
// zip, which returns ContainerResult{} with Format=="" and a nil error).
//
// It reports a fact about the INPUT. A failure to read the input at all --
// a non-EOF io.ReaderAt error -- is deliberately NOT reported as this
// sentinel, because "this is not a zip" is a conclusion and an unreadable
// upload gives no grounds for one. Callers that need the distinction test
// with errors.Is; callers that only care whether the sniff succeeded keep
// working unchanged, since both cases are still non-nil errors. This split
// exists because internal/inspect must choose between reporting
// not_applicable and read_failed, and it could not while every failure
// arrived here wearing the same sentinel (codex PR #9, round 6 neighbour
// audit).
var ErrNotAZip = errors.New("not a valid zip container")

// ooxmlRootParts is the closed set of OOXML root-part paths this project
// disambiguates by name-presence, not position (D-01/D-02).
// Source: verified via Go programs against real pandoc/openpyxl-generated
// docx/pptx/xlsx fixtures (RESEARCH.md Pattern 1) -- root-part names are
// position-independent in the ZIP central directory.
var ooxmlRootParts = map[string]string{
	"word/document.xml":    "docx",
	"xl/workbook.xml":      "xlsx",
	"ppt/presentation.xml": "pptx",
}

// odfMimetypes is the closed set of media-type payloads found in the first
// ZIP entry ("mimetype", stored uncompressed) -- the OASIS-mandated ODF
// trio PLUS epub, which independently designates its identity via the
// same first-entry-stored-uncompressed convention (T-47-05c, 47-CONTEXT.md).
// Source: OASIS OpenDocument v1.2 Part 3 §17.4 (mimetype file requirement);
// EPUB Open Container Format 3.x §3.3 ("the first file in the archive MUST
// be ... named mimetype", stored/uncompressed, payload "application/epub+zip").
var odfMimetypes = map[string]string{
	"application/vnd.oasis.opendocument.text":         "odt",
	"application/vnd.oasis.opendocument.spreadsheet":  "ods",
	"application/vnd.oasis.opendocument.presentation": "odp",
	"application/epub+zip":                            "epub",
}

// ooxmlMacroParts is the closed set of literal OOXML macro-storage paths
// (docm/xlsm/pptm and similar macro-enabled variants).
// Source: xlsxwriter documentation -- "a package must contain at most one
// VBA Project part" at word|xl|ppt/vbaProject.bin.
var ooxmlMacroParts = map[string]bool{
	"word/vbaProject.bin": true,
	"xl/vbaProject.bin":   true,
	"ppt/vbaProject.bin":  true,
}

// readerAtRecorder wraps an io.ReaderAt and remembers whether ITS OWN
// underlying ReadAt call ever returned a real (non-EOF) error -- codex PR #9
// round 8's own finding (docsniff.go:93, PR9R8-F4-STRUCTURAL-VS-IO): the
// previous approach classified a zip.NewReader error as "operational" by
// checking whether it matched one of three known sentinels
// (zip.ErrFormat/zip.ErrAlgorithm/zip.ErrChecksum) and defaulting
// everything else to "operational read failure" -- which broke on the very
// next error archive/zip returns that was never enumerated
// (io.ErrUnexpectedEOF for a truncated central-directory header,
// "zip: invalid comment length" for an inconsistent EOCD -- neither
// originates in ReadAt, both are properties of the bytes themselves). The
// only reliable way to know whether a failure originated in the READER
// (this handler's own inability to get bytes at all) rather than in the
// PARSER's interpretation of bytes it successfully received is to be told
// so by the reader itself -- not to infer origin from the shape of
// whatever error the parser built on top of it happens to return. A caller
// that reads failed AFTER zip.NewReader has already returned gets an
// OBSERVED fact, not a guess.
type readerAtRecorder struct {
	r      io.ReaderAt
	failed bool
	err    error
}

// ReadAt delegates to the wrapped reader and records the FIRST real
// (non-EOF) failure it observes -- later calls after a recorded failure
// still delegate (archive/zip may retry at a different offset after a
// short read elsewhere), but do not overwrite the first recorded cause.
func (rr *readerAtRecorder) ReadAt(p []byte, off int64) (int, error) {
	n, err := rr.r.ReadAt(p, off)
	if err != nil && err != io.EOF && !rr.failed {
		rr.failed = true
		rr.err = err
	}
	return n, err
}

// SniffContainer inspects a ZIP-shaped upload's central directory to
// disambiguate the six supported office formats plus epub (D-01, T-47-05c),
// sum declared uncompressed size across every entry for the zip-bomb guard
// (D-02/D-03),
// and detect macro-carrying parts (D-03/D-05) -- all in a single pass over
// the archive/zip central directory (RESEARCH.md Pitfall 4: never re-parse
// the central directory more than once per upload). r must be the original,
// unconsumed upload source (e.g. multipart.File), not a re-stitched
// io.Reader -- the archive/zip package requires positional access.
func SniffContainer(r io.ReaderAt, size int64) (ContainerResult, error) {
	rr := &readerAtRecorder{r: r}
	zr, err := zip.NewReader(rr, size)
	if err != nil {
		// PR9R8-F4-STRUCTURAL-VS-IO: classify by what rr actually OBSERVED
		// about the underlying reader (see readerAtRecorder's own doc
		// comment), never by matching err against a closed list of known
		// archive/zip sentinels. rr.failed is set if, and only if, the
		// wrapped io.ReaderAt itself returned a genuine (non-EOF) error --
		// every OTHER zip.NewReader failure, whatever Go error type it
		// happens to be, describes bytes this function DID successfully
		// read, so it is always ErrNotAZip (see that sentinel's own doc
		// comment), never an operational read failure, regardless of
		// whether this exact error shape has ever been seen before.
		if rr.failed {
			return ContainerResult{}, fmt.Errorf("read zip container: %w", rr.err)
		}
		return ContainerResult{}, fmt.Errorf("%w: %w", ErrNotAZip, err)
	}

	var res ContainerResult
	nameCount := map[string]int{}

	for i, f := range zr.File {
		// Saturating add: UncompressedSize64 comes from the central
		// directory, which is attacker-controlled and never validated
		// against the actual payload. Two entries each declaring ~2^63 would
		// wrap a plain += back to a small total, turning both this sum and
		// every zip-bomb check built on it (handlers.go's
		// maxDocumentUncompressedBytes, enforcePandocInputSize) into a
		// bypass. Saturating at MaxUint64 fails closed instead.
		if f.UncompressedSize64 > math.MaxUint64-res.TotalUncompressed {
			res.TotalUncompressed = math.MaxUint64
		} else {
			res.TotalUncompressed += f.UncompressedSize64
		}

		if ooxmlMacroParts[f.Name] || hasBasicPrefix(f.Name) {
			res.HasMacro = true
			nameCount[f.Name]++
		}
		if fmtName, ok := ooxmlRootParts[f.Name]; ok {
			res.Format = fmtName
			nameCount[f.Name]++
		}

		if i == 0 && f.Name == "mimetype" && f.Method == zip.Store {
			// rerr, on failure, is NOT classified here -- see the rr.failed
			// check below, at this function's own return decision, for why
			// (PR9R9 finding, docsniff.go:120). f.Open()/the returned
			// io.ReadCloser's Read calls decompress through rr, the SAME
			// readerAtRecorder zip.NewReader itself used above, so any
			// non-EOF ReadAt failure this read hits is recorded on rr
			// exactly like every other read this function performs.
			payload, rerr := readBounded(f, 128)
			if rerr == nil {
				if odfFmt, ok := odfMimetypes[string(payload)]; ok {
					res.Format = odfFmt
				}
			}
		}
	}

	for _, c := range nameCount {
		if c > 1 {
			res.DuplicateRootPart = true
		}
	}

	// PR9R9 finding (docsniff.go:120), verbatim: "If zip.NewReader reads the
	// central directory successfully but the later readBounded call for an
	// ODF/EPUB mimetype entry encounters a non-EOF ReadAt failure, this
	// recorder sets rr.failed only after the sole check at lines 121-135 has
	// passed. The error from readBounded is ignored at lines 165-170 and
	// rr.failed is never checked again, so SniffContainer returns a nil
	// error with an empty format and the inspect path reports
	// format_unrecognized rather than read_failed." Checking rr.failed HERE,
	// at the actual return decision -- rather than only once, immediately
	// after zip.NewReader -- closes it: rr.failed is set as a side effect of
	// ANY ReadAt this function performs through rr, and the mimetype read
	// above is the only additional one beyond zip.NewReader's own central-
	// directory read, so this check is scoped tightly to that one path. On
	// an observed I/O failure, this returns the SAME shape (empty result,
	// wrapped error) the zip.NewReader failure branch above already returns
	// for an operational read failure -- detectInspectFormat's own
	// errors.Is(cerr, convert.ErrNotAZip) check already classifies this
	// correctly as read_failed, not malformed_input or format_unrecognized,
	// with no further change needed on that side.
	if rr.failed {
		return ContainerResult{}, fmt.Errorf("read zip container: %w", rr.err)
	}
	return res, nil
}

// hasBasicPrefix reports whether name lives under ODF's "Basic/" macro-
// storage directory (script-lc.xml at the root plus script-lb.xml/module
// files under arbitrary, user-defined library-name subdirectories) -- a
// prefix match, not a literal library-name match, so a renamed library is
// still caught.
func hasBasicPrefix(name string) bool {
	return len(name) >= 6 && name[:6] == "Basic/"
}

// readBounded opens f and reads at most max bytes -- used only for the
// small "mimetype" payload comparison; every other SniffContainer decision
// (format, size, macro) uses central-directory metadata alone with zero
// decompression.
func readBounded(f *zip.File, max int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, max))
}
