// This file implements the image-family analyzer for Phase 58's synchronous
// inspect path: declared pixel dimensions, pixel-bomb shape, and EXIF/XMP
// metadata presence (INSP-01, INSP-02).
package inspect

import (
	"bytes"
	"encoding/binary"
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
func AnalyzeImage(format string, r io.ReaderAt, size int64, maxBytes int64, pixelBombThreshold uint64) FamilyResult {
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
	width, height, _, err := convert.Dimensions(format, io.NewSectionReader(r, 0, size))
	dimsKnown := err == nil
	if !dimsKnown {
		// convert.ErrDimensionsUnknown (bounded peek did not find declared
		// dimensions) and any other error from the underlying reader are
		// both an honest "we don't know", never a rejection -- inspect only
		// ever reports (D-03).
		c["width"] = nil
		c["width_reason"] = string(ReasonMalformedInput)
		c["height"] = nil
		c["height_reason"] = string(ReasonMalformedInput)
		c["pixels"] = nil
		c["pixels_reason"] = string(ReasonMalformedInput)
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
		checks = append(checks, Check{Code: CodeImagePixelBombShape, Status: StatusUnknown, Reason: ReasonMalformedInput})
	case uint64(width)*uint64(height) > pixelBombThreshold:
		sev, _ := SeverityOf(CodeImagePixelBombShape)
		findings = append(findings, Finding{Code: CodeImagePixelBombShape, Severity: sev})
	default:
		checks = append(checks, Check{Code: CodeImagePixelBombShape, Status: StatusClean})
	}

	if loc, found := scanImageMetadata(format, r, size, maxBytes); found {
		sev, _ := SeverityOf(CodeMetadataPresent)
		findings = append(findings, Finding{Code: CodeMetadataPresent, Severity: sev, Location: loc})
	} else {
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
func scanImageMetadata(format string, r io.ReaderAt, size, maxBytes int64) (location string, found bool) {
	winLen := maxBytes
	if winLen > size {
		winLen = size
	}
	buf, ok := readWindow(r, 0, winLen, size)
	if !ok {
		return "", false
	}
	switch format {
	case "jpg", "jpeg":
		return scanJPEGMetadata(buf)
	case "png":
		return scanPNGMetadata(buf)
	case "tiff":
		// A TIFF file carries an IFD by construction -- its presence is
		// almost always true and is NOT a false positive (58-05-PLAN.md
		// action text, Part 3): report it whenever the bounded window
		// contains a parseable TIFF byte-order header, without re-deriving
		// the same IFD walk the dimension parser already validated.
		if len(buf) >= 8 && (bytes.Equal(buf[0:2], []byte{0x49, 0x49}) || bytes.Equal(buf[0:2], []byte{0x4D, 0x4D})) {
			return "TIFF IFD0", true
		}
		return "", false
	case "webp":
		return scanRIFFChunkMetadata(buf)
	case "heic":
		return scanISOBMFFMetadataTokens(buf)
	default:
		return "", false
	}
}

// scanJPEGMetadata walks JPEG marker segments (the same shape convert's
// jpegDimensions walks) looking for an APP1 segment carrying either an
// Exif payload or an Adobe XMP payload. Unlike jpegDimensions, this does
// NOT stop at the first SOF -- it must see every marker up to SOS/EOI,
// because APP1 can appear before or after other APPn segments.
func scanJPEGMetadata(b []byte) (string, bool) {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return "", false
	}
	i := 2
	for iter := 0; i+1 < len(b) && iter < maxMetadataMarkerScanIterations; iter++ {
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
		if marker == 0xD9 || marker == 0xDA {
			return "", false // EOI or SOS reached: no metadata marker seen
		}
		if i+4 > len(b) {
			return "", false
		}
		segLen := int(b[i+2])<<8 | int(b[i+3])
		if segLen < 2 || i+2+segLen > len(b) {
			return "", false
		}
		if marker == 0xE1 { // APP1
			payload := b[i+4 : i+2+segLen]
			if bytes.HasPrefix(payload, []byte("Exif\x00\x00")) {
				return "JPEG APP1 Exif", true
			}
			if bytes.Contains(payload, []byte("http://ns.adobe.com/xap/1.0/")) {
				return "JPEG APP1 XMP", true
			}
		}
		i += 2 + segLen
	}
	return "", false
}

// scanPNGMetadata walks PNG chunks after the 8-byte signature looking for
// an eXIf chunk or an iTXt chunk carrying the Adobe XMP keyword.
func scanPNGMetadata(b []byte) (string, bool) {
	if len(b) < 8 {
		return "", false
	}
	pos := 8
	for iter := 0; pos+8 <= len(b) && iter < maxMetadataMarkerScanIterations; iter++ {
		length := int(binary.BigEndian.Uint32(b[pos : pos+4]))
		chunkType := string(b[pos+4 : pos+8])
		dataStart := pos + 8
		if length < 0 || uint64(dataStart)+uint64(length)+4 > uint64(len(b)) {
			return "", false // declared chunk length runs past the bounded window: fail closed
		}
		switch chunkType {
		case "eXIf":
			return "PNG eXIf chunk", true
		case "iTXt":
			if bytes.Contains(b[dataStart:dataStart+length], []byte("XML:com.adobe.xmp")) {
				return "PNG iTXt XMP", true
			}
		}
		pos = dataStart + length + 4 // +4 for the trailing CRC
	}
	return "", false
}

// scanRIFFChunkMetadata walks WebP's RIFF chunks (after the 12-byte
// RIFF/WEBP header) looking for an EXIF or XMP chunk.
func scanRIFFChunkMetadata(b []byte) (string, bool) {
	if len(b) < 12 {
		return "", false
	}
	pos := 12
	for iter := 0; pos+8 <= len(b) && iter < maxMetadataMarkerScanIterations; iter++ {
		fourCC := string(b[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(b[pos+4 : pos+8]))
		dataStart := pos + 8
		if size < 0 || uint64(dataStart)+uint64(size) > uint64(len(b)) {
			return "", false // declared chunk size runs past the bounded window: fail closed
		}
		switch fourCC {
		case "EXIF":
			return "WebP EXIF chunk", true
		case "XMP ":
			return "WebP XMP chunk", true
		}
		advance := size
		if advance%2 == 1 {
			advance++ // RIFF chunks are word-aligned; odd sizes get one pad byte
		}
		pos = dataStart + advance
	}
	return "", false
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
func scanISOBMFFMetadataTokens(b []byte) (string, bool) {
	if bytes.Contains(b, []byte("Exif")) {
		return "HEIC Exif item", true
	}
	if bytes.Contains(b, []byte("XMP ")) {
		return "HEIC XMP item", true
	}
	return "", false
}
