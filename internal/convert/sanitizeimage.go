package convert

// This file (sanitizeimage.go): metadata removal from images (SANI-02).
//
// TOOL CHOICE, AND WHY IT IS NEITHER OF THE TWO THE PLAN OFFERED
//
// Plan 60-03 named two routes: shell out to exiftool (a new binary in the
// image), or re-encode through the stdlib discarding metadata. A third is
// available and strictly better than both, so it is what this does:
//
//	STRUCTURAL STRIP -- walk the container's segment/chunk structure and copy
//	forward everything except the segments that carry metadata, leaving the
//	compressed image data byte-identical.
//
// Against exiftool: no new binary, no new supply-chain surface, and the
// guarantee is by construction rather than by trusting a tool's option
// surface to mean what its documentation says.
//
// Against re-encoding: re-encoding a JPEG decodes and recompresses it, which
// LOSES QUALITY every time, silently and irreversibly. Plan 60-03 was explicit
// that quietly degrading the image is not acceptable and that unavoidable loss
// would have to be reported. Structural stripping makes the question moot --
// the entropy-coded scan data is copied verbatim, so the pixels that come out
// are bit-for-bit the pixels that went in. There is no loss to report because
// there is no loss.
//
// The cost of this route is that it is format-specific: each container needs
// its own walker. That is a real limit and it is why the supported set below
// is closed and small, rather than a claim about images in general.

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
)

// imageThreatClasses is what the image path removes. Metadata only: an image
// container carries no actions, no macros and no attachments, and claiming
// otherwise would be the same false statement pdfThreatClasses exists to
// avoid.
var imageThreatClasses = []ThreatClass{ThreatDocumentMetadata}

// COLOR MANAGEMENT IS KEPT; METADATA IS NOT. THE RULE IS ABOUT WHAT A
// SEGMENT DOES, NOT ABOUT WHICH MARKER CARRIES IT.
//
// The first version of this dropped every APP0-APP15 segment, on the argument
// that an allowlist of "segments we know are safe" would let the next vendor
// extension through. That argument is right about METADATA carriers and wrong
// about two segments that are not metadata at all:
//
//	APP2 with an "ICC_PROFILE\0" payload -- the colour profile. Without it a
//	    wide-gamut image is rendered as if it were sRGB. The pixels are
//	    unchanged; what they MEAN is not.
//	APP14 with an "Adobe" payload -- the colour-transform flag. Without it a
//	    CMYK or YCCK JPEG decodes with inverted or wrong channels, and some
//	    decoders refuse it outright.
//
// Dropping those made this file's own losslessness claim false in the only
// sense a client cares about: the bytes of the scan survived and the picture
// changed. Caught in review (PR #12).
//
// Both are kept by CONTENT, never by marker number: APP2 is also used by
// FlashPix (FPXR), and APP14 by other producers, so a marker-only allowlist
// would readmit exactly the vendor extensions the original argument was
// guarding against. Everything else in APP0-APP15, and every COM segment,
// still goes -- APP1 is EXIF/GPS/XMP, APP13 is IPTC, APP0 is JFIF.
//
// WHAT THIS LEAVES ON THE TABLE, STATED RATHER THAN ELIDED: an ICC profile is
// a structured container with vendor-defined tags, so a preserved profile can
// carry bytes this service did not enumerate. That is a boundary of the
// guarantee, and docs/SANITIZE.md's scope section says so; it is not a claim
// this code quietly makes.
//
// The scan data after SOS is copied verbatim to the end. That is what makes
// this lossless: the compressed image is never decoded.
func stripJPEGMetadata(src []byte) ([]byte, error) {
	if len(src) < 2 || src[0] != 0xFF || src[1] != 0xD8 {
		return nil, fmt.Errorf("not a JPEG (no SOI)")
	}

	var out bytes.Buffer
	out.Write(src[:2]) // SOI

	i := 2
	for i+1 < len(src) {
		if src[i] != 0xFF {
			return nil, fmt.Errorf("malformed JPEG: expected a marker at offset %d", i)
		}
		marker := src[i+1]

		// Standalone markers carry no length field.
		if marker == 0xD8 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			out.Write(src[i : i+2])
			i += 2
			continue
		}
		if marker == 0xD9 { // EOI
			out.Write(src[i : i+2])
			i += 2
			break
		}
		if i+4 > len(src) {
			return nil, fmt.Errorf("truncated JPEG segment header at offset %d", i)
		}
		segLen := int(binary.BigEndian.Uint16(src[i+2 : i+4]))
		if segLen < 2 || i+2+segLen > len(src) {
			return nil, fmt.Errorf("invalid JPEG segment length %d at offset %d", segLen, i)
		}
		segEnd := i + 2 + segLen

		isAPP := marker >= 0xE0 && marker <= 0xEF
		isCOM := marker == 0xFE
		if !isAPP && !isCOM || isKeptJPEGAppSegment(marker, src[i+4:segEnd]) {
			out.Write(src[i:segEnd])
		}

		if marker == 0xDA { // SOS: entropy-coded data runs to the end
			out.Write(src[segEnd:])
			return out.Bytes(), nil
		}
		i = segEnd
	}
	return out.Bytes(), nil
}

// jpegColourPayloadPrefixes are the payload signatures of the two APP segments
// that carry colour management rather than metadata, keyed by their marker.
// Matched on the payload, never on the marker alone -- see this file's header.
var jpegColourPayloadPrefixes = map[byte][]byte{
	0xE2: []byte("ICC_PROFILE\x00"), // APP2
	0xEE: []byte("Adobe"),           // APP14
}

// isKeptJPEGAppSegment reports whether an APPn segment is one of the two
// colour-management segments this sanitizer preserves.
func isKeptJPEGAppSegment(marker byte, payload []byte) bool {
	prefix, ok := jpegColourPayloadPrefixes[marker]
	if !ok {
		return false
	}
	return bytes.HasPrefix(payload, prefix)
}

// pngMetadataChunks is the closed set of PNG chunk types that carry metadata.
// eXIf is the PNG container for the same EXIF block JPEG puts in APP1; the
// text chunks carry everything from authorship to embedded XMP.
// iCCP, sRGB, gAMA and cHRM are deliberately ABSENT: they are PNG's colour
// management, the same role APP2/APP14 play in JPEG, and dropping them here
// while keeping them there would make the two paths disagree about what
// "metadata" means.
var pngMetadataChunks = map[string]bool{
	"tEXt": true, "iTXt": true, "zTXt": true,
	"eXIf": true, "tIME": true,
}

// stripPNGMetadata drops metadata chunks from a PNG, copying every other chunk
// through byte-for-byte -- IDAT (the compressed image) is never touched.
func stripPNGMetadata(src []byte) ([]byte, error) {
	sig := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}
	if len(src) < len(sig) || !bytes.Equal(src[:len(sig)], sig) {
		return nil, fmt.Errorf("not a PNG (bad signature)")
	}

	var out bytes.Buffer
	out.Write(sig)

	i := len(sig)
	for i+8 <= len(src) {
		length := int(binary.BigEndian.Uint32(src[i : i+4]))
		if length < 0 || i+12+length > len(src) {
			return nil, fmt.Errorf("invalid PNG chunk length %d at offset %d", length, i)
		}
		ctype := string(src[i+4 : i+8])
		end := i + 12 + length // length + type + data + CRC

		if !pngMetadataChunks[ctype] {
			out.Write(src[i:end])
		}
		i = end
		if ctype == "IEND" {
			break
		}
	}
	return out.Bytes(), nil
}

// sanitizeImage strips metadata from inPath into outPath.
//
// The format is decided from the file's own magic bytes, not from the caller's
// claimed format: a client that uploads a PNG named .jpg would otherwise get
// the JPEG walker, which would fail on the signature check and turn a
// mislabelled file into an error instead of a sanitized one.
func sanitizeImage(ctx context.Context, inPath, outPath string) (SanitizeResult, error) {
	src, err := os.ReadFile(inPath)
	if err != nil {
		return SanitizeResult{}, fmt.Errorf("sanitize image: read: %w", err)
	}

	var out []byte
	switch {
	case len(src) >= 2 && src[0] == 0xFF && src[1] == 0xD8:
		out, err = stripJPEGMetadata(src)
	case len(src) >= 8 && bytes.Equal(src[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}):
		out, err = stripPNGMetadata(src)
	default:
		return SanitizeResult{}, fmt.Errorf("sanitize image: unrecognized image container")
	}
	if err != nil {
		return SanitizeResult{}, fmt.Errorf("sanitize image: %w", err)
	}

	if err := os.WriteFile(outPath, out, 0o644); err != nil {
		return SanitizeResult{}, fmt.Errorf("sanitize image: write output: %w", err)
	}

	_ = ctx // pure Go; no external process to cancel

	return SanitizeResult{
		Removed: append([]ThreatClass(nil), imageThreatClasses...),
		// Nothing is lost: the compressed image data is copied verbatim, so
		// there is no quality cost to report. An empty list here is a claim,
		// and TestSanitizeImageIsLossless is what backs it.
		LostCapabilities: nil,
	}, nil
}
