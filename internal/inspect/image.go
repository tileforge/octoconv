// This file implements the image-family analyzer for Phase 58's synchronous
// inspect path: declared pixel dimensions, pixel-bomb shape, and EXIF/XMP
// metadata presence (INSP-01, INSP-02).
package inspect

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"

	"github.com/apaderin/octoconv/internal/convert"
)

// maxMetadataMarkerScanIterations bounds every marker/chunk-walking loop
// below -- the same discipline as window.go's maxTokenScanIterations: a
// hard iteration ceiling, not a context deadline, is the load-bearing
// bound for a CPU-bound loop (58-RESEARCH.md Q4).
const maxMetadataMarkerScanIterations = 4096

// AnalyzeImage extracts INSP-01 image characteristics and the two
// image-applicable INSP-02 findings for one already format-detected image
// upload.
func AnalyzeImage(format string, r io.ReaderAt, size int64, maxBytes int64, pixelBombThreshold uint64, tr *Tracker) FamilyResult {
	c := Characteristics{}

	// Reuse, not reimplement: the function referenced below is the ONLY
	// existing pure-Go characteristic extractor in this project, and it
	// already runs inline in the API path today (handlers.go around line
	// 509, VALID-03). A second implementation of the same declared-
	// dimension logic here would be a guaranteed future divergence between
	// what /v1/inspect reports and what handleCreateJob actually enforces
	// -- the same reasoning behind D-03's single-vocabulary, two-consumer
	// design, just applied to characteristic extraction instead of
	// findings. The `rest` reader that function returns is deliberately
	// discarded here: AnalyzeImage operates on an io.ReaderAt, not a stream
	// that needs to be handed further downstream.
	// PR9R23 (image.go:38), verbatim: "For an image larger than the inspection
	// work allowance, convert.Dimensions reads up to its independent 64 KiB
	// peek here, and scanImageMetadata then reads the full maxBytes allowance
	// again; the dispatcher nevertheless records only maxBytes. This lets every
	// such image exceed the advertised operation-wide byte ceiling."
	//
	// The fix is not to deduct one read from the other: the two windows OVERLAP
	// (both start at offset 0), so deducting would double-charge the same bytes
	// and, for any file smaller than the peek, leave the metadata scan a
	// zero-byte window over bytes already in memory. This function now reads
	// the window ONCE and uses it twice -- one bounded read, one charge, and
	// the dimension parser can no longer help itself to 64 KiB the caller's
	// ceiling never approved. The MP4 head/tail split in media.go stays a
	// split precisely because those two windows are DISJOINT; these are not.
	buf, windowTruncated, readFailed, noBytes := readImageWindow(r, size, maxBytes)

	// The peek convert.Dimensions would have taken for itself, had it been
	// handed the whole file. When the shared window is shorter than that, a
	// parse failure is OUR ceiling rather than a fact about the image -- the
	// PR9R16 misattribution, which naively clamping the peek would have
	// reintroduced (a short peek fails as ErrDimensionsUnknown, and that is
	// otherwise read as malformed_input).
	dimPeek := int64(convert.DimensionPeekBytes)
	if dimPeek > size {
		dimPeek = size
	}
	dimWindowShort := int64(len(buf)) < dimPeek

	var width, height uint32
	err := error(convert.ErrDimensionsUnknown)
	if !readFailed && len(buf) > 0 {
		// bytes.Reader over the window this function already read, instead of a
		// second SectionReader over the file: same parser, same answer for any
		// input it was ever going to answer for, zero additional bytes.
		width, height, _, err = convert.Dimensions(format, bytes.NewReader(buf))
	}
	dimsKnown := err == nil
	// Both outcomes are an honest "we don't know", never a rejection --
	// inspect only ever reports (D-03) -- but they do not say the same thing
	// about WHOSE fault it is, and this branch used to state one of them for
	// both (PR9R16, image.go:39). convert.ErrDimensionsUnknown means the
	// bounded peek read the bytes and found no declared dimensions: a fact
	// about the image. Any other error came from the reader underneath us:
	// a fact about this service, which the metadata scan of the SAME reader
	// already reports as read_failed. Blaming the upload for our own disk
	// is the same misattribution rounds 9 through 15 kept finding, here in
	// the one path that still had it.
	dimsReason := Reason(ReasonMalformedInput)
	if !dimsKnown && !errors.Is(err, convert.ErrDimensionsUnknown) {
		dimsReason = ReasonReadFailed
	}
	switch {
	case !dimsKnown && readFailed:
		// The window read itself failed on bytes this scan meant to examine in
		// full -- our disk, not the image (PR9R6 finding 2, now decided once
		// here for BOTH answers instead of separately inside each).
		dimsReason = ReasonReadFailed
	case !dimsKnown && dimWindowShort && !noBytes:
		// The parser was handed less than the peek it would have taken on its
		// own, so its failure says nothing about the image: our ceiling did.
		dimsReason = ReasonWorkCeilingBytes
		tr.MarkCeilingHit()
	}
	if !dimsKnown {
		c["width"] = nil
		c["width_reason"] = string(dimsReason)
		c["height"] = nil
		c["height_reason"] = string(dimsReason)
		c["pixels"] = nil
		c["pixels_reason"] = string(dimsReason)
	} else {
		c["width"] = width
		c["height"] = height
		c["pixels"] = uint64(width) * uint64(height)
	}
	c["declared_format"] = format

	var findings []Finding
	var checks []Check

	// Pixel-bomb shape: uint64(width) * uint64(height), computed in uint64
	// -- width/height are each uint32, and a hostile pair near max-uint32
	// produces a product that does not fit in 32 bits (the same reason
	// Server.maxImagePixels is itself declared uint64, api.go around line
	// 174). T-58-27.
	switch {
	case !dimsKnown:
		// T-58-26: unknown dimensions are NEVER reported clean. "We don't
		// know the size" is not the same claim as "we know there is no
		// bomb" -- collapsing the two would be exactly the false-safety
		// defect class INSP-03 exists to prevent, and it is the one place
		// in this analyzer where getting it backwards produces a false
		// sense of safety rather than merely an incomplete report.
		checks = append(checks, Check{Code: CodeImagePixelBombShape, Status: StatusUnknown, Reason: dimsReason})
	case uint64(width)*uint64(height) > pixelBombThreshold:
		sev, _ := SeverityOf(CodeImagePixelBombShape)
		findings = append(findings, Finding{Code: CodeImagePixelBombShape, Severity: sev})
	default:
		checks = append(checks, Check{Code: CodeImagePixelBombShape, Status: StatusClean})
	}

	// The SAME window the dimension parser just used -- no second read, so
	// nothing to deduct (PR9R23).
	loc, found, ceilingHit, malformedHit := scanImageMetadataIn(format, buf, windowTruncated, noBytes, tr)
	switch {
	case found:
		sev, _ := SeverityOf(CodeMetadataPresent)
		findings = append(findings, Finding{Code: CodeMetadataPresent, Severity: sev, Location: loc})
	case readFailed:
		// quick/260812-r6c, codex PR9R6 finding 2 (image.go:165): the ONE
		// readWindow call this function makes failed even though the
		// window was sized to cover the file IN FULL (not merely too
		// small) -- a real I/O error, or a truthful empty read, on bytes
		// this scan otherwise intended to examine. Round 5's own sweep of
		// this exact file fixed every WALK exit that conflated "aborted"
		// with "found nothing" and missed this READ exit one line above
		// them: reporting the zero-value here presented a disk/temp-file
		// failure as a successful, complete scan. See scanImageMetadata's
		// own doc comment for the full three-way read-failure/ceiling/
		// degenerate-window split this case is the first of.
		checks = append(checks, Check{Code: CodeMetadataPresent, Status: StatusUnknown, Reason: ReasonReadFailed})
	case ceilingHit:
		// codex PR9 finding 3 / T-SLE-03: a miss inside a window (or an
		// iteration walk) that did NOT cover/complete the whole scan is not
		// evidence the metadata does not exist past what was actually
		// looked at. See scanImageMetadata's own doc comment for every
		// source folded into this single signal.
		checks = append(checks, Check{Code: CodeMetadataPresent, Status: StatusUnknown, Reason: ReasonWorkCeilingBytes})
	case malformedHit:
		// quick/260811-r5p task 4 (class sweep, PR9R5-SWEEP-CLASSES(б)): a
		// scan that aborted on a STRUCTURAL problem -- a declared
		// length/size running past the buffer, a magic-byte header that
		// does not parse, or leftover bytes that never form the format's
		// own natural end -- must never be folded into the same signal a
		// genuinely complete, empty walk produces. See scanImageMetadata's
		// own doc comment for the full ceiling/malformed distinction, which
		// deliberately mirrors container.go's externalRefStatus
		// (PR9R5, container.go:462).
		checks = append(checks, Check{Code: CodeMetadataPresent, Status: StatusUnknown, Reason: ReasonMalformedInput})
	default:
		checks = append(checks, Check{Code: CodeMetadataPresent, Status: StatusClean})
	}

	return FamilyResult{Characteristics: c, Findings: findings, Checks: checks}
}

// scanImageMetadata looks for the cheapest possible marker/chunk proving an
// EXIF or XMP payload is present, reusing the same marker-walk discipline
// convert's own PNG/JPEG dimension parsers already apply: bounded, fails
// closed, never trusts a declared length past the read window
// (58-RESEARCH.md Q1 "Images" row). It stops at the FIRST match --
// presence is a yes/no fact, not a catalog.
//
// windowTruncated reports whether the bounded window it actually read
// covered the WHOLE file (codex PR9 finding 3 / T-SLE-03): for an image
// larger than maxBytes, a miss inside the head window is NOT evidence the
// metadata does not exist -- valid PNG, WebP, HEIC, and JPEG files can all
// place EXIF/XMP metadata after the default byte ceiling.
//
// Return shape (quick/260811-r5p task 4, PR9R5-SWEEP-CLASSES(б); extended
// quick/260812-r6c task 2 with readFailed): a MISS can come from FOUR
// genuinely different places, kept as four genuinely different named
// results instead of folding any two of them into the same bool -- exactly
// the discipline container.go's externalRefStatus already applies to the
// container family (see imageMetadataStatus's own doc comment below, which
// this function's per-format branches compose with windowTruncated via
// composeMetadataStatus):
//
//   - found: a marker/chunk WAS located -- location names it.
//   - readFailed: THIS scan's own single readWindow call failed even
//     though the window it asked for was sized to cover the file IN FULL
//     (winLen > 0 and NOT windowTruncated) -- a real, non-EOF error from
//     the underlying io.ReaderAt, or a truthful empty read where bytes
//     were expected. This is neither a fact about the file's structure nor
//     about this scan's own byte-window/iteration ceiling: no bytes were
//     ever examined at all (codex PR9R6 finding 2, image.go:165 -- round
//     5's own sweep of this file fixed every WALK exit that conflated
//     "aborted" with "found nothing" and missed this READ exit one line
//     above them).
//   - ceilingHit: THIS operation's own resource limit is why nothing was
//     found -- either this function's own byte window did not cover the
//     whole file (windowTruncated), or a per-format scanner's own
//     maxMetadataMarkerScanIterations cap fired first. Neither is a fact
//     about the file; both are OUR refusal to keep looking, but -- unlike
//     readFailed -- a refusal driven by a PRESET budget, not by an
//     operational inability to read at all.
//   - malformedHit: a per-format scanner read every byte it was given,
//     within every one of ITS OWN local ceilings, and STILL could not reach
//     the format's own natural end (JPEG's EOI; PNG's and WebP's chunk
//     walk consuming the buffer exactly; TIFF's byte-order header check) --
//     a declared length/size running past the buffer, a header that fails
//     its own structural check, or leftover bytes too short to form
//     another chunk/segment header. That is a fact about the FILE, never
//     this scan's own limit, and never an unexamined read either.
//
// Precedence when a scanner's own malformed signal and the byte-window
// ceiling could both apply: ceilingHit always wins (composeMetadataStatus
// below). If the window itself was cut short, this function cannot tell
// whether the REAL file is malformed or was merely never fully read -- the
// honest answer is our own ceiling, not a claim about the file's structure
// (the same precedence container.go's scanExternalReferences applies
// between truncatedAnyPart and malformedAnyPart). readFailed can only ever
// be returned directly from the readWindow call below -- once buf is in
// hand, every per-format scanner operates on bytes that were genuinely
// read, so none of them can produce it.
// readImageWindow performs the ONE bounded read AnalyzeImage is allowed, and
// classifies a failure to perform it. Both the dimension parser and the
// metadata scan work from the buffer it returns, so the operation-wide byte
// ceiling bounds one read rather than being spent once per consumer (PR9R23,
// image.go:38).
//
//   - readFailed: the window was sized to cover the file IN FULL and the read
//     still failed -- a real, non-EOF io.ReaderAt error, or a truthful empty
//     read. Neither a fact about the file nor a preset budget: no bytes were
//     examined at all (PR9R6 finding 2).
//   - noBytes: there was nothing to read (size <= 0), so no read was even
//     attempted -- a degenerate-input fact, not a failure and not a ceiling.
//   - windowTruncated: the window did not cover the whole file, which is the
//     byte-window half of "ceilingHit" (codex PR9 finding 3 / T-SLE-03). A
//     non-positive maxBytes lands here too, and correctly so: refusing to read
//     is our own budget decision, never the file's fault.
func readImageWindow(r io.ReaderAt, size, maxBytes int64) (buf []byte, windowTruncated, readFailed, noBytes bool) {
	winLen := maxBytes
	if winLen > size {
		winLen = size
	}
	windowTruncated = winLen < size
	buf, ok := readWindow(r, 0, winLen, size)
	if ok {
		return buf, windowTruncated, false, false
	}
	switch {
	case windowTruncated:
		return nil, true, false, false
	case winLen <= 0:
		return nil, false, false, true
	default:
		return nil, false, true, false
	}
}

// scanImageMetadataIn is the per-format half of the old scanImageMetadata: it
// classifies bytes ALREADY read (see readImageWindow) and never reads any of
// its own, which is what keeps this family's byte accounting to a single
// charge.
func scanImageMetadataIn(format string, buf []byte, windowTruncated, noBytes bool, tr *Tracker) (location string, found bool, ceilingHit bool, malformedHit bool) {
	if len(buf) == 0 {
		switch {
		case windowTruncated:
			return "", false, true, false
		case noBytes:
			// Preserves this scan's long-standing behavior for a genuinely
			// empty upload rather than mislabeling it as a read failure that
			// never happened.
			return "", false, false, false
		}
	}
	switch format {
	case "jpg", "jpeg":
		return composeMetadataStatus(scanJPEGMetadata(buf, tr), windowTruncated)
	case "png":
		return composeMetadataStatus(scanPNGMetadata(buf, tr), windowTruncated)
	case "tiff":
		return scanTIFFMetadata(buf, windowTruncated)
	case "webp":
		return composeMetadataStatus(scanRIFFChunkMetadata(buf, tr), windowTruncated)
	case "heic":
		loc, ok := scanISOBMFFMetadataTokens(buf)
		return loc, ok, windowTruncated, false
	default:
		return "", false, windowTruncated, false
	}
}

// imageMetadataStatus is the shared three-way result scanJPEGMetadata,
// scanPNGMetadata, and scanRIFFChunkMetadata (WebP) each return -- the same
// shape as container.go's externalRefStatus, mirrored here on purpose
// (quick/260811-r5p task 4): at most one of location (non-empty), ceiling,
// or malformed is ever meaningful.
//
//   - location non-empty: the format-specific marker/chunk WAS found.
//   - ceiling: THIS scanner's own local resource limit
//     (maxMetadataMarkerScanIterations) fired before the walk could reach
//     the format's own natural end.
//   - malformed: the walk read every byte in b, within its own ceiling, and
//     STILL never reached the format's own natural end -- see each
//     scanner's own doc comment for what "natural end" means for that
//     format. A declared length/size that runs past b, a header that fails
//     its own magic-byte check, or leftover bytes too short to form another
//     header are all this outcome, never a synonym for "scanned to the end
//     and found nothing".
//   - all three zero-valued: the walk reached the format's own natural end
//     within b and never saw a metadata marker -- a genuine clean, exactly
//     as much a real answer as a non-empty location is.
type imageMetadataStatus struct {
	location  string
	ceiling   bool
	malformed bool
}

// composeMetadataStatus applies the SAME precedence rule to every per-format
// scanner that returns an imageMetadataStatus: a byte-window ceiling (the
// caller did not read enough of the file to cover the whole thing) is
// checked BEFORE a scanner's own structural malformed signal, never after
// -- see scanImageMetadata's own doc comment for why.
func composeMetadataStatus(st imageMetadataStatus, windowTruncated bool) (location string, found bool, ceilingHit bool, malformedHit bool) {
	switch {
	case st.location != "":
		return st.location, true, false, false
	case windowTruncated || st.ceiling:
		return "", false, true, false
	case st.malformed:
		return "", false, false, true
	default:
		return "", false, false, false
	}
}

// scanJPEGMetadata walks JPEG marker segments (the same shape convert's
// jpegDimensions walks) looking for an APP1 segment carrying either an
// Exif payload or an Adobe XMP payload. Unlike jpegDimensions, this does
// NOT stop at the first SOF -- it must see every marker up to SOS/EOI,
// because APP1 can appear before or after other APPn segments.
//
// Every exit below is classified against JPEG's own natural end -- EOI
// (0xD9), the ONE marker that legitimately ends the marker-segment stream
// this walk understands. SOS (0xDA) was treated as a second natural end
// until PR9R22 (image.go:355) pointed out that it ends only the current
// header walk: decoders resume marker processing after each scan, so a
// progressive JPEG's later scans, and any APP1 segment between a scan and
// EOI, sat entirely unexamined behind a `clean` verdict. The walk now steps
// over each scan's entropy-coded data (skipEntropyCodedData) and keeps going
// (quick/260811-r5p task 4,
// PR9R5-SWEEP-CLASSES(б), applying the exact rule task 2 already applied to
// container.go's XML parsing: only a walk that reaches the FORMAT'S OWN
// natural end may report clean):
//
//   - No valid SOI at all, a marker-length field running past b, or a
//     declared segment length running past b are all a STRUCTURAL problem
//     with the bytes this scan was given, not a "scanned everything, found
//     nothing" miss -- each now reports malformed, never the same
//     zero-value a genuine EOI-terminated miss reports.
//   - Running out of buffer without ever hitting EOI (the loop condition
//     failing, with no earlier explicit return) is the SAME class: a real,
//     complete JPEG ends with EOI, so falling off the end of b without
//     seeing it means this walk never reached the format's own end -- also
//     malformed. Entropy-coded data that runs to the end of the buffer with
//     no marker behind it is the same verdict, reached one level down in
//     skipEntropyCodedData.
//   - ceilingHit (maxMetadataMarkerScanIterations firing before EOI) is
//     kept as its OWN, separate outcome from malformed for exactly the
//     reason container.go's ceiling/malformed split exists: a fully-
//     buffered JPEG can place 4096+ harmless marker segments before an
//     APP1 Exif/XMP segment, and that is THIS scan's own resource limit,
//     never a fact about the file (codex PR9 round 2, image.go:209).
//
// skipEntropyCodedData advances past one JPEG scan's entropy-coded data,
// starting at from (the first byte after the SOS header), and returns the
// offset of the next real marker's 0xFF byte.
//
// Inside a scan a 0xFF byte is always followed by 0x00 (a stuffed byte,
// escaping a literal 0xFF in the compressed stream), by RSTn (0xD0-0xD7, a
// restart marker belonging to the scan), or by 0xFF as fill; anything else
// begins a real marker and ends the scan. ok=false means the buffer ran out
// with no such marker, i.e. this scan is the last thing in b and EOI was
// never reached.
//
// Skipping with IndexByte rather than one byte per outer-loop iteration is
// what keeps the marker-segment ceiling meaningful: charging one iteration per
// compressed byte would turn every ordinary photograph into
// work_ceiling_bytes. The offset only ever moves forward, so the whole scan
// stays a single linear pass over b.
func skipEntropyCodedData(b []byte, from int) (next int, ok bool) {
	for j := from; j >= 0 && j < len(b); {
		k := bytes.IndexByte(b[j:], 0xFF)
		if k < 0 {
			return 0, false
		}
		j += k
		if j+1 >= len(b) {
			return 0, false
		}
		switch nxt := b[j+1]; {
		case nxt == 0x00, nxt >= 0xD0 && nxt <= 0xD7:
			j += 2 // stuffed byte, or a restart marker inside the scan
		case nxt == 0xFF:
			j++ // fill byte before the next marker
		default:
			return j, true
		}
	}
	return 0, false
}

func scanJPEGMetadata(b []byte, tr *Tracker) imageMetadataStatus {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		// Not even a parseable SOI marker within the bytes this scan
		// received: a structural problem with the file, never this scan's
		// own iteration ceiling (see doc comment above).
		return imageMetadataStatus{malformed: true}
	}
	i := 2
	for iter := 0; i+1 < len(b); iter++ {
		if iter >= maxMetadataMarkerScanIterations {
			tr.MarkCeilingHit()
			return imageMetadataStatus{ceiling: true}
		}
		if b[i] != 0xFF {
			i++ // resync on stray non-marker byte
			continue
		}
		marker := b[i+1]
		if marker == 0xFF {
			i++ // fill byte
			continue
		}
		if marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) || marker == 0xD8 {
			i += 2 // standalone markers: TEM, RSTn, stray SOI -- no length field
			continue
		}
		if marker == 0xD9 {
			// EOI: JPEG's own natural end, genuinely clean if no metadata
			// marker was seen before this point.
			return imageMetadataStatus{}
		}
		if i+4 > len(b) {
			return imageMetadataStatus{malformed: true} // buffer ends mid marker-length field: not a natural end
		}
		segLen := int(b[i+2])<<8 | int(b[i+3])
		if segLen < 2 || i+2+segLen > len(b) {
			return imageMetadataStatus{malformed: true} // declared segment length runs past the buffer: not a natural end
		}
		if marker == 0xE1 { // APP1
			payload := b[i+4 : i+2+segLen]
			if bytes.HasPrefix(payload, []byte("Exif\x00\x00")) {
				return imageMetadataStatus{location: "JPEG APP1 Exif"}
			}
			if bytes.Contains(payload, []byte("http://ns.adobe.com/xap/1.0/")) {
				return imageMetadataStatus{location: "JPEG APP1 XMP"}
			}
		}
		if marker == 0xDA { // SOS
			// PR9R22 (image.go:355), verbatim: "For a multi-scan JPEG, or any
			// JPEG with an APP1 segment between an entropy-coded scan and EOI,
			// returning at the first SOS reports metadata_present: clean
			// without examining the later APP1 Exif/XMP marker. JPEG decoders
			// resume marker processing after each scan, so SOS ends only the
			// current header walk, not the file's possible metadata."
			//
			// Correct, and the doc comment above was wrong in the same words:
			// SOS is not one of JPEG's natural ends. EOI is the only one. So
			// the walk now steps OVER the entropy-coded data instead of
			// stopping at it, and keeps looking for markers -- progressive
			// JPEGs have several scans, and an APP1 segment may legally sit
			// between any two of them or between the last scan and EOI.
			//
			// Inside entropy-coded data a 0xFF byte is always followed by
			// 0x00 (a stuffed byte, not a marker), by RSTn (0xD0-0xD7, a
			// restart marker inside the scan), or by 0xFF as fill; anything
			// else is a real marker and ends the scan. Bytes are skipped with
			// IndexByte rather than one at a time by the outer loop's resync
			// branch, which matters for the CEILING as much as for speed: the
			// ceiling counts marker segments, and charging it one iteration
			// per compressed byte would turn every ordinary photo into
			// work_ceiling_bytes. j only ever moves forward, so the whole
			// function stays a single linear pass over b.
			next, ok := skipEntropyCodedData(b, i+2+segLen)
			if !ok {
				// Entropy-coded data ran to the end of the buffer with no
				// marker after it, so this walk never reached EOI -- the same
				// "never reached the format's own natural end" verdict every
				// other structural exit here returns. A window that merely
				// stopped short is composeMetadataStatus's ceiling, which is
				// checked ahead of malformed.
				return imageMetadataStatus{malformed: true}
			}
			i = next
			continue
		}
		i += 2 + segLen
	}
	// Ran out of buffer without ever reaching EOI: never JPEG's own
	// natural end, so this miss cannot be reported clean (see doc comment
	// above).
	return imageMetadataStatus{malformed: true}
}

// scanPNGMetadata walks PNG chunks after the 8-byte signature looking for
// an eXIf chunk or an iTXt chunk carrying the Adobe XMP keyword.
//
// PNG has no single terminal marker byte the way JPEG's EOI is (an IEND
// chunk is conventional, but this walk does not require seeing one by
// name): the natural end this walk recognizes is the chunk walk consuming
// the buffer EXACTLY, with zero leftover bytes (quick/260811-r5p task 4,
// PR9R5-SWEEP-CLASSES(б), same rule task 2 applied to container.go's XML
// parsing). Every other exit is a structural problem, not a clean miss:
//
//   - len(b) < 8: not even the PNG signature fits.
//   - A declared chunk length running past b (dataStart+length+4 >
//     len(b)): the same "declared size past the window" shape
//     scanExternalReferences already closes for zip parts.
//   - The loop exits with pos < len(b) but too few bytes remain for another
//     8-byte chunk header: leftover bytes this walk never accounted for,
//     never a proof that no chunk follows.
//
// Reaching pos == len(b) with the loop condition simply false is the ONE
// exit that means the whole buffer was legitimately consumed, chunk by
// chunk, with nothing left unaccounted for -- exactly as much a genuine
// answer as finding eXIf/iTXt is.
//
// ceilingHit reports whether maxMetadataMarkerScanIterations -- not the
// buffer end, not a malformed chunk -- is what stopped the walk (codex PR9
// round 2, image.go:209): kept as its own outcome, separate from malformed,
// for the same reason scanJPEGMetadata's own ceiling is separate.
func scanPNGMetadata(b []byte, tr *Tracker) imageMetadataStatus {
	if len(b) < 8 {
		return imageMetadataStatus{malformed: true}
	}
	pos := 8
	// This walk has exactly one clean exit, and it is inside the loop: the
	// IEND case. Consuming the buffer to a tidy chunk boundary is NOT the
	// format's end (PR9R16, image.go:426) -- an IHDR-only file lands there
	// too -- and neither is merely HAVING seen an IEND somewhere (PR9R17,
	// image.go:439): IEND terminates, so anything after it is not a PNG the
	// spec describes. Every exit below the loop is therefore a miss without
	// a proven end.
	for iter := 0; pos+8 <= len(b); iter++ {
		if iter >= maxMetadataMarkerScanIterations {
			tr.MarkCeilingHit()
			return imageMetadataStatus{ceiling: true}
		}
		length := int(binary.BigEndian.Uint32(b[pos : pos+4]))
		chunkType := string(b[pos+4 : pos+8])
		dataStart := pos + 8
		if length < 0 || uint64(dataStart)+uint64(length)+4 > uint64(len(b)) {
			return imageMetadataStatus{malformed: true} // declared chunk length runs past the buffer: not a natural end
		}
		switch chunkType {
		case "eXIf":
			return imageMetadataStatus{location: "PNG eXIf chunk"}
		case "iTXt":
			if bytes.Contains(b[dataStart:dataStart+length], []byte("XML:com.adobe.xmp")) {
				return imageMetadataStatus{location: "PNG iTXt XMP"}
			}
		case "IEND":
			// PNG's IEND carries no data (PR9R18, image.go:447). A nonzero
			// declared length here is not a terminator with a payload -- no
			// such thing exists in the format -- so advancing over that
			// payload and calling the result clean accepts arbitrary bytes
			// as if they had been accounted for.
			if length != 0 {
				return imageMetadataStatus{malformed: true}
			}
			// IEND does not merely occur, it TERMINATES (PR9R17,
			// image.go:439). Recording it and walking on -- which is what
			// the previous fix did -- still reported clean for a file with
			// chunks after IEND, which is not a PNG the spec describes.
			// Stop here and decide on what follows, rather than letting the
			// loop's own exit conditions decide.
			pos = dataStart + length + 4
			if pos == len(b) {
				return imageMetadataStatus{}
			}
			// Anything at all after IEND makes this structurally invalid,
			// and the walk stopped before reading it -- so no claim about
			// metadata in those bytes is available either way.
			return imageMetadataStatus{malformed: true}
		}
		pos = dataStart + length + 4 // +4 for the trailing CRC
	}
	// Reaching here means the loop ended without ever reaching IEND (that
	// case returns in both of its outcomes), so the file stopped mid-stream
	// -- whether at a tidy chunk boundary or on a partial header -- and
	// nothing was proven absent from the chunks it never carried.
	// Leftover bytes remain that do not form a full chunk header: this walk
	// did not reach the buffer's end cleanly, so a miss here is not proof
	// metadata is absent from whatever those leftover/unaccounted bytes (or
	// anything the real file has past them) might carry.
	return imageMetadataStatus{malformed: true}
}

// scanRIFFChunkMetadata walks WebP's RIFF chunks (after the 12-byte
// RIFF/WEBP header) looking for an EXIF or XMP chunk.
//
// The same shape as scanPNGMetadata above -- codex PR9 round 2's own
// phrasing ("The JPEG and WebP walkers have the same shape") already
// applied to the ceiling outcome; quick/260811-r5p task 4
// (PR9R5-SWEEP-CLASSES(б)) applies it to the malformed/natural-end split
// too, which the original review rounds never named for this scanner. RIFF
// has no format-level terminal marker either, so the natural end this walk
// recognizes is identical to PNG's: the chunk walk consuming the buffer
// EXACTLY. A declared chunk size running past b, or leftover bytes too
// short for another 8-byte fourCC+size header, are both a structural
// problem, never a clean miss.
func scanRIFFChunkMetadata(b []byte, tr *Tracker) imageMetadataStatus {
	if len(b) < 12 {
		return imageMetadataStatus{malformed: true}
	}
	// PR9R21 (image.go:519), verbatim: "For a fully buffered WebP whose RIFF
	// size field declares more bytes than the file actually contains, a
	// complete present chunk such as VP8X can leave pos == len(b) and this
	// branch reports metadata_present: clean."
	//
	// The per-chunk guard below checks each chunk against len(b) and never
	// checked the CONTAINER's own declared length, so "the walk consumed the
	// buffer exactly" was read as RIFF's natural end even when the RIFF
	// header said the file should have gone on. b[4:8] is the byte count
	// following those first 8 bytes, so a conformant file has
	// declaredEnd == len(b) exactly, and both inequalities are structural:
	//
	//   - declaredEnd > len(b): the container claims bytes the file does not
	//     have -- truncation, the reported case;
	//   - declaredEnd < len(b): bytes sit beyond the container's own end, and
	//     the walk would go on reading them as chunks, so an EXIF chunk
	//     OUTSIDE the RIFF container could be reported as this file's
	//     metadata. Rejecting it mirrors the rule this same file already
	//     applies to PNG, where nothing may follow IEND.
	//
	// A truncated read WINDOW is not this branch's concern: composeMetadataStatus
	// checks windowTruncated BEFORE malformed, so a partial window over a
	// large file stays a ceiling (our budget) and never becomes a verdict
	// about the file. uint64 throughout because the declared size is
	// attacker-controlled and 0xFFFFFFFF+8 overflows an int32.
	//
	// convert's WebP dimension parser is deliberately left alone: it reads
	// the VP8X/VP8/VP8L header, and for a truncated file those bytes are
	// present and their answer is true, so the report carrying real
	// dimensions beside a malformed metadata verdict is honest. Changing that
	// parser would also change the conversion path, which this review round
	// has no evidence about.
	if uint64(binary.LittleEndian.Uint32(b[4:8]))+8 != uint64(len(b)) {
		return imageMetadataStatus{malformed: true}
	}
	pos := 12
	for iter := 0; pos+8 <= len(b); iter++ {
		if iter >= maxMetadataMarkerScanIterations {
			tr.MarkCeilingHit()
			return imageMetadataStatus{ceiling: true}
		}
		fourCC := string(b[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(b[pos+4 : pos+8]))
		dataStart := pos + 8
		if size < 0 || uint64(dataStart)+uint64(size) > uint64(len(b)) {
			return imageMetadataStatus{malformed: true} // declared chunk size runs past the buffer: not a natural end
		}
		switch fourCC {
		case "EXIF":
			return imageMetadataStatus{location: "WebP EXIF chunk"}
		case "XMP ":
			return imageMetadataStatus{location: "WebP XMP chunk"}
		}
		advance := size
		if advance%2 == 1 {
			advance++ // RIFF chunks are word-aligned; odd sizes get one pad byte
		}
		pos = dataStart + advance
	}
	if pos == len(b) {
		// The chunk walk consumed the buffer exactly: RIFF's own natural
		// end for this walk (see doc comment).
		return imageMetadataStatus{}
	}
	// Leftover bytes remain that do not form a full chunk header: not a
	// natural end, so a miss here is not proof metadata is absent.
	return imageMetadataStatus{malformed: true}
}

// scanTIFFMetadata reports whether buf begins with a valid TIFF byte-order
// header ("II" or "MM") within at least 8 bytes -- a TIFF file carries an
// IFD by construction (58-05-PLAN.md action text, Part 3), so a valid
// header alone is enough to report metadata FOUND without re-deriving the
// same IFD walk the dimension parser already validated.
//
// A MISS was, before this task, folded into exactly the defect class this
// file closes elsewhere (quick/260811-r5p task 4, PR9R5-SWEEP-CLASSES(б)):
// the original inline branch this function replaces returned "not found"
// identically whether the window was merely too short to cover 8 bytes, OR
// the window covered the WHOLE file and those bytes simply are not a valid
// TIFF header at all. Those are different facts:
//
//   - windowTruncated true: this scan's own byte ceiling is why the header
//     could not be validated -- OUR limit, not the file's fault.
//   - windowTruncated false and the header still does not match: the
//     format claim (format=="tiff", from upstream sniffing) and the bytes
//     this scan actually read disagree within a COMPLETE read of the file
//     -- a structural fact about the file, the same class as
//     scanJPEGMetadata's own magic-byte check, and it must never be
//     reported the same way an honestly short window is.
func scanTIFFMetadata(buf []byte, windowTruncated bool) (location string, found bool, ceilingHit bool, malformedHit bool) {
	if len(buf) >= 8 && (bytes.Equal(buf[0:2], []byte{0x49, 0x49}) || bytes.Equal(buf[0:2], []byte{0x4D, 0x4D})) {
		return "TIFF IFD0", true, false, false
	}
	if windowTruncated {
		return "", false, true, false
	}
	return "", false, false, true
}

// scanISOBMFFMetadataTokens looks for the literal EXIF/XMP box-type tokens
// anywhere in the bounded HEIC window. HEIC stores EXIF/XMP as items
// inside meta/iinf/iloc, whose full item-id-to-offset resolution is
// exactly the complexity convert's heicDimensions already declines to
// parse in full (it takes the max ispe across items rather than resolving
// the primary item, for the same reason). A literal token scan is the
// cheap, bounded approximation used here: it can false-negative on an
// encoder that never emits these bytes contiguously, but it never claims a
// structural walk it did not actually do.
//
// quick/260811-r5p task 4 (class sweep, PR9R5-SWEEP-CLASSES(б)) explicitly
// weighed whether this function belongs to the SAME defect class as
// scanJPEGMetadata/scanPNGMetadata/scanRIFFChunkMetadata (an abort-on-
// structural-problem exit reporting exactly what a genuinely clean miss
// reports) -- and the answer, stated plainly rather than left implicit, is
// NO: this function performs no structural walk at all to abort FROM. It is
// bytes.Contains over the entire window, unconditionally, every single
// time -- there is no declared length to run past, no magic-byte header to
// fail, no chunk boundary that could leave leftover bytes unaccounted for.
// Its only source of incompleteness is the byte WINDOW itself, already
// reported as ceilingHit via windowTruncated at the call site identically
// to every other format here, plus the approximation's own inherent
// false-negative risk on a non-contiguous encoding -- a property of the
// APPROXIMATION CHOSEN, not of any exit path this function takes.
//
// Giving that false-negative risk its own unknown/malformed_input verdict
// would mean EVERY HEIC upload with no metadata reports unknown instead of
// clean -- a much larger behavior change than replacing a mis-classified
// abort with an honest one, is not what any of the five container/image
// review rounds asked for, and was already surfaced as a known,
// deliberately-unchanged limitation rather than silent omission (see
// 260811-r5p-SUMMARY-task4-sweep.md, "Раскрытые ограничения" item 1). A
// miss here therefore keeps reporting clean whenever the byte window was
// not truncated -- on purpose, not by omission.
func scanISOBMFFMetadataTokens(b []byte) (string, bool) {
	if bytes.Contains(b, []byte("Exif")) {
		return "HEIC Exif item", true
	}
	if bytes.Contains(b, []byte("XMP ")) {
		return "HEIC XMP item", true
	}
	return "", false
}
