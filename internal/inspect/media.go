// This file implements the media-family analyzer for Phase 58's synchronous
// inspect path (INSP-01): duration/resolution/declared-codec for mp4/mov/
// m4a (ISOBMFF), duration/declared-codec for mkv/webm (EBML), duration for
// wav (RIFF), and an honest `unknown` for mp3/ogg/avi -- the format this
// plan exists to prove is genuinely not worth attempting in a synchronous,
// bounded, in-process parser (D-01, D-02, 58-RESEARCH.md Q1).
//
// The single most important design decision in this file is the moov
// search strategy: MP4/MOV's `moov` box (which carries `mvhd`'s duration
// and `tkhd`'s resolution) usually lives at the file TAIL, not the head --
// ffmpeg's default output does NOT relocate `moov` to the front unless
// `+faststart` is explicitly requested (Assumption A1, 58-RESEARCH.md).
// This is NOT a "peek the first N KiB" problem the way convert.Dimensions'
// image parsing is -- it genuinely needs a tail-anchored read, which is
// available because the multipart upload backing this analyzer is an
// io.ReaderAt, not a pure stream.
package inspect

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
)

// maxISOBMFFBoxes is the hard ceiling on the number of top-level boxes any
// single walkTopLevelBoxes/firstChildBox call may step through. Mirrors
// convert/dimensions.go's walkBoxes bounds-before-trust discipline, plus an
// explicit iteration cap (58-RESEARCH.md Q4: a hard iteration ceiling, not
// a context deadline, is the load-bearing bound for a CPU-bound loop).
const maxISOBMFFBoxes = 4096

// maxEBMLElements is the equivalent iteration ceiling for the EBML element
// walk (mkv/webm) below -- same rationale as maxISOBMFFBoxes.
const maxEBMLElements = 512

// maxRIFFChunks is the equivalent iteration ceiling for the WAV RIFF chunk
// walk below -- same rationale as maxISOBMFFBoxes.
const maxRIFFChunks = 256

// mp4TailWindow bounds the tail-anchored read used when `moov` is not found
// in the head window. Deliberately far larger than window.go's
// defaultTailWindow (64 KiB): unlike a PDF trailer, an MP4's `moov` box can
// itself run to hundreds of KB on a file with many tracks/edits, and it can
// start anywhere within the last stretch of the file depending on how much
// trailing padding a given encoder appends after the final `mdat`. 2 MiB is
// this media module's own constant, sized for that reality -- clamped by
// the caller's maxBytes work ceiling exactly like every other window in
// this package.
const mp4TailWindow = 2 << 20

// ebmlInfoWindow bounds the head-anchored read for the EBML continuation
// below. avPeekLen (internal/convert/avsniff.go), 4 KiB, is sized only to
// reach the DocType element that identifies mkv vs. webm -- it is NOT sized
// to reach Segment/Info/Duration, which sits further into the file. This
// module's own, larger constant continues past where matchEBML stops.
const ebmlInfoWindow = 256 * 1024

// AnalyzeMedia extracts INSP-01 media characteristics for one already
// format-detected (mp4/mov/mkv/webm/wav/mp3/m4a/ogg/avi) upload. format is
// never re-sniffed here -- detection already happened via convert.Sniff /
// SniffVideo / SniffAudio before this is called.
//
// This analyzer never returns an error: a parser that cannot determine a
// characteristic reports that characteristic as nil plus a `<key>_reason`,
// not a Go error -- D-03's "inspect only ever reports" discipline applies
// to characteristic extraction exactly as it does to findings.
//
// The media family declares no Findings and no Checks of its own: none of
// this package's fourteen closed vocabulary Codes (findings.go) describe a
// media-format-specific risk -- the codes that exist today are container/
// PDF/image concepts. Reducing every Code to not_applicable for a media
// upload is the report ASSEMBLER's job (plan 58-07), which has the full
// picture across every family; a single family analyzer emitting
// not_applicable Checks for codes it has no opinion about would just be
// duplicated boilerplate repeated once per family. This is a deliberate
// omission, not an oversight.
func AnalyzeMedia(format string, r io.ReaderAt, size int64, maxBytes int64) FamilyResult {
	var chars Characteristics
	switch format {
	case "mp4", "mov":
		chars = isobmffCharacteristics(r, size, maxBytes, true)
	case "m4a":
		chars = isobmffCharacteristics(r, size, maxBytes, false)
	case "mkv", "webm":
		chars = ebmlCharacteristics(r, size, maxBytes)
	case "wav":
		chars = wavCharacteristics(r, size, maxBytes)
	case "mp3", "ogg":
		chars = unsupportedAudioDurationCharacteristics()
	case "avi":
		chars = unsupportedVideoCharacteristics()
	default:
		chars = Characteristics{}
	}
	return FamilyResult{Characteristics: chars, Findings: nil, Checks: nil}
}

// ===== Part 1: ISOBMFF (mp4/mov/m4a) =====

// isobmffCharacteristics locates `moov` (head-then-tail, see findMoov) and
// extracts duration (mvhd), resolution (tkhd, only when includeDims is
// true -- m4a is audio and must not carry width/height keys at all), and
// declared codec (stsd's first sample entry fourCC). Scope boundary
// (58-05-PLAN.md): this reports the CONTAINER-DECLARED codec fourCC
// (avc1/hvc1/mp4a/...), never a profile or level -- profile/level detail
// requires ffprobe and is out of scope (INSP-V2-02), exactly as
// convert.Dimensions reports declared pixel dimensions, never decoded
// pixels.
func isobmffCharacteristics(r io.ReaderAt, size, maxBytes int64, includeDims bool) Characteristics {
	c := Characteristics{}

	moovPayload, reason := findMoov(r, size, maxBytes)
	if moovPayload == nil {
		c["duration_seconds"] = nil
		c["duration_seconds_reason"] = string(reason)
		if includeDims {
			c["width"] = nil
			c["width_reason"] = string(reason)
			c["height"] = nil
			c["height_reason"] = string(reason)
		}
		c["declared_codec"] = nil
		c["declared_codec_reason"] = string(reason)
		return c
	}

	if mvhdPayload, ok := firstChildBox(moovPayload, "mvhd"); ok {
		if durSec, ok, malformed := parseMVHD(mvhdPayload); ok {
			c["duration_seconds"] = durSec
		} else {
			_ = malformed // both failure modes (short box, zero timescale) map to the same reason
			c["duration_seconds"] = nil
			c["duration_seconds_reason"] = string(ReasonMalformedInput)
		}
	} else {
		// mvhd is MANDATORY inside a well-formed moov (ISO/IEC 14496-12) --
		// its absence here is a structural fact about THIS file, not a
		// resource ceiling: the full moov box is already in hand.
		c["duration_seconds"] = nil
		c["duration_seconds_reason"] = string(ReasonMalformedInput)
	}

	if includeDims {
		if w, h, ok := findTrackDimensions(moovPayload); ok {
			c["width"] = w
			c["height"] = h
		} else {
			c["width"] = nil
			c["width_reason"] = string(ReasonMalformedInput)
			c["height"] = nil
			c["height_reason"] = string(ReasonMalformedInput)
		}
	}

	if codec, ok := findDeclaredCodec(moovPayload); ok {
		c["declared_codec"] = codec
	} else {
		c["declared_codec"] = nil
		c["declared_codec_reason"] = string(ReasonMalformedInput)
	}

	return c
}

// findMoov implements the two-step, explicitly-decided search strategy
// (58-05-PLAN.md): head window first (covers `+faststart` files for free),
// then a tail-anchored window if the head window did not contain `moov`.
// Returns (nil, reason) when moov is found nowhere -- reason distinguishes
// "our own windows didn't cover the whole file" (ReasonWorkCeilingBytes)
// from "the windows covered everything and moov genuinely is not there"
// (ReasonMalformedInput). These are two different facts and must not be
// collapsed into one (D-02a's whole point, applied here to media instead
// of PDF).
func findMoov(r io.ReaderAt, size, maxBytes int64) ([]byte, Reason) {
	if size <= 0 || maxBytes <= 0 {
		return nil, ReasonMalformedInput
	}

	headLen := maxBytes
	if headLen > size {
		headLen = size
	}
	var headActual int64
	headWin, headOK := readWindow(r, 0, headLen, size)
	if headOK {
		headActual = int64(len(headWin))
		if payload, ok := firstChildBox(headWin, "moov"); ok {
			return payload, ""
		}
	}

	// STEP 2: moov not in the head window -- try the tail. The tail window
	// starts mid-file, so the bytes at its own offset 0 are the MIDDLE of
	// whatever box precedes moov (very likely `mdat`), not a box header at
	// all. Walking from the tail window's own offset 0 would misparse that
	// middle-of-box garbage as a bogus box header. Instead: find the
	// literal 4-byte "moov" type field as a substring, step back 4 bytes to
	// what should be that box's own size field, and start the (bounded,
	// bounds-checked) box walk from THERE -- this is the one place this
	// file must not "just walk from offset 0" like every other box walk in
	// this package, and it is easy to lose on a future refactor if this
	// comment is removed.
	tailLen := int64(mp4TailWindow)
	if tailLen > maxBytes {
		tailLen = maxBytes
	}
	if tailLen > size {
		tailLen = size
	}
	tailOff := size - tailLen
	if tailOff < 0 {
		tailOff = 0
	}
	var tailActual int64
	tailWin, tailOK := readWindow(r, tailOff, tailLen, size)
	if tailOK {
		tailActual = int64(len(tailWin))
		if idx := bytes.Index(tailWin, []byte("moov")); idx >= 4 {
			boxStart := idx - 4
			var payload []byte
			found := false
			walkTopLevelBoxes(tailWin[boxStart:], func(t string, p []byte) bool {
				if t == "moov" {
					payload, found = p, true
				}
				return false // only the box located at boxStart is meaningful here
			})
			if found {
				return payload, ""
			}
		}
	}

	if headActual+tailActual >= size {
		// Both windows together covered the entire file (accounting for
		// possible overlap on small files) and moov still never appeared --
		// this is a fact about the FILE, not about our own resource limits.
		return nil, ReasonMalformedInput
	}
	return nil, ReasonWorkCeilingBytes
}

// findTrackDimensions descends moov -> trak (first) -> tkhd and decodes
// its declared width/height.
func findTrackDimensions(moovPayload []byte) (width, height uint32, ok bool) {
	trakPayload, ok := firstChildBox(moovPayload, "trak")
	if !ok {
		return 0, 0, false
	}
	tkhdPayload, ok := firstChildBox(trakPayload, "tkhd")
	if !ok {
		return 0, 0, false
	}
	return parseTKHD(tkhdPayload)
}

// findDeclaredCodec descends moov -> trak (first) -> mdia -> minf -> stbl
// -> stsd and reads the first sample entry's fourCC.
func findDeclaredCodec(moovPayload []byte) (string, bool) {
	trakPayload, ok := firstChildBox(moovPayload, "trak")
	if !ok {
		return "", false
	}
	mdiaPayload, ok := firstChildBox(trakPayload, "mdia")
	if !ok {
		return "", false
	}
	minfPayload, ok := firstChildBox(mdiaPayload, "minf")
	if !ok {
		return "", false
	}
	stblPayload, ok := firstChildBox(minfPayload, "stbl")
	if !ok {
		return "", false
	}
	stsdPayload, ok := firstChildBox(stblPayload, "stsd")
	if !ok {
		return "", false
	}
	return parseSTSDCodec(stsdPayload)
}

// parseMVHD decodes mvhd's timescale+duration (ISO/IEC 14496-12 8.2.2.1),
// handling both version 0 (32-bit fields) and version 1 (64-bit fields).
// timescale == 0 is reported as malformed (division by zero avoided, never
// attempted) rather than as a panic or an infinite/NaN result.
func parseMVHD(b []byte) (durationSeconds float64, ok bool, malformed bool) {
	if len(b) < 1 {
		return 0, false, false
	}
	version := b[0]
	var timescale uint32
	var duration uint64
	switch version {
	case 1:
		// 1(version)+3(flags)+8(creation)+8(modification)+4(track... no,
		// mvhd has no track_ID)+... exact layout: version+flags(4),
		// creation_time(8), modification_time(8), timescale(4), duration(8).
		if len(b) < 32 {
			return 0, false, false
		}
		timescale = binary.BigEndian.Uint32(b[20:24])
		duration = binary.BigEndian.Uint64(b[24:32])
	default:
		// version 0: version+flags(4), creation_time(4), modification_time(4),
		// timescale(4), duration(4).
		if len(b) < 20 {
			return 0, false, false
		}
		timescale = binary.BigEndian.Uint32(b[12:16])
		duration = uint64(binary.BigEndian.Uint32(b[16:20]))
	}
	if timescale == 0 {
		return 0, false, true
	}
	return float64(duration) / float64(timescale), true, false
}

// parseTKHD decodes tkhd's declared width/height (ISO/IEC 14496-12
// 8.3.2.1). Both fields are stored as 16.16 FIXED-POINT values -- the
// integer part is the high 16 bits. Reading them as plain 32-bit integers
// (skipping the >>16) would report a value 65536x too large (T-58-28).
func parseTKHD(b []byte) (width, height uint32, ok bool) {
	if len(b) < 1 {
		return 0, 0, false
	}
	version := b[0]
	var off int
	if version == 1 {
		// version+flags(4) + creation(8) + modification(8) + track_ID(4) +
		// reserved(4) + duration(8) = 36
		off = 36
	} else {
		// version+flags(4) + creation(4) + modification(4) + track_ID(4) +
		// reserved(4) + duration(4) = 24
		off = 24
	}
	// + reserved[2](8) + layer/alternate_group/volume/reserved(8) + matrix(36) = 52
	off += 52
	if len(b) < off+8 {
		return 0, 0, false
	}
	wFixed := binary.BigEndian.Uint32(b[off : off+4])
	hFixed := binary.BigEndian.Uint32(b[off+4 : off+8])
	return wFixed >> 16, hFixed >> 16, true
}

// parseSTSDCodec reads the fourCC of stsd's first sample entry -- the
// declared codec (e.g. avc1/hvc1/mp4a). stsd (ISO/IEC 14496-12 8.5.2) is a
// FullBox: version+flags(4) + entry_count(4), followed by sample entries
// each shaped like an ordinary box: size(4) + format-fourCC(4) + ...
func parseSTSDCodec(b []byte) (codec string, ok bool) {
	const entryStart = 8
	if len(b) < entryStart+8 {
		return "", false
	}
	return string(b[entryStart+4 : entryStart+8]), true
}

// walkTopLevelBoxes iterates top-level ISOBMFF boxes in buf, calling
// fn(type, payload) for each; fn returns false to stop early. A direct
// analog of convert/dimensions.go's walkBoxes -- deliberately re-
// implemented rather than imported, because that function is unexported by
// the convert package and this package must not grow a public API on
// convert just to share four lines of box-header parsing. Every declared
// box size is validated against the REMAINING bounded buffer before being
// trusted (T-58-24) -- this function never allocates by a declared size and
// never reads past len(buf).
func walkTopLevelBoxes(buf []byte, fn func(boxType string, payload []byte) bool) {
	i := 0
	for count := 0; i+8 <= len(buf) && count < maxISOBMFFBoxes; count++ {
		size64 := uint64(binary.BigEndian.Uint32(buf[i : i+4]))
		boxType := string(buf[i+4 : i+8])
		headerLen := 8
		switch size64 {
		case 1:
			if i+16 > len(buf) {
				return
			}
			size64 = binary.BigEndian.Uint64(buf[i+8 : i+16])
			headerLen = 16
		case 0:
			return // "extends to EOF": not expected within a bounded window, bail conservatively
		}
		if size64 < uint64(headerLen) || size64 > uint64(len(buf)-i) {
			return // truncated or malformed within the bounded window: fail closed
		}
		size := int(size64)
		if !fn(boxType, buf[i+headerLen:i+size]) {
			return
		}
		i += size
	}
}

// firstChildBox returns the payload of the first top-level box of boxType
// within buf, or (nil, false) if none is found within maxISOBMFFBoxes
// boxes.
func firstChildBox(buf []byte, boxType string) ([]byte, bool) {
	var payload []byte
	found := false
	walkTopLevelBoxes(buf, func(t string, p []byte) bool {
		if t == boxType {
			payload, found = p, true
			return false
		}
		return true
	})
	return payload, found
}

// ===== Part 2: EBML (mkv/webm) =====

// EBML element IDs this analyzer needs, continuing the walk
// convert/avsniff.go's matchEBML stops at (DocType). Widths (1/2/3/4 bytes)
// are the well-known Matroska/EBML values (RFC 8794 for the base grammar;
// the Matroska element IDs themselves are the long-standing de facto
// values every mkv/webm encoder and reader agrees on).
const (
	ebmlIDSegment    = 0x18538067
	ebmlIDInfo       = 0x1549A966
	ebmlIDTimestamp  = 0x2AD7B1 // TimestampScale (formerly TimecodeScale)
	ebmlIDDuration   = 0x4489
	ebmlIDTracks     = 0x1654AE6B
	ebmlIDTrackEntry = 0xAE
	ebmlIDCodecID    = 0x86
)

// ebmlCharacteristics continues matchEBML's walk (which stops once it has
// identified DocType) into Segment -> Info -> Duration/TimestampScale and
// Segment -> Tracks -> TrackEntry -> CodecID, within a single bounded head
// window (ebmlInfoWindow, clamped by maxBytes).
//
// Safe-refusal discipline required by 58-RESEARCH.md Assumption A2: if
// Duration is not found within the window, the result is unknown with
// ReasonWorkCeilingBytes -- the window is NEVER grown adaptively "until it
// is found". Growing a buffer in response to untrusted content not
// containing what you are looking for is exactly the lever an adversarial
// input turns into a resource-exhaustion primitive (T-58-23).
func ebmlCharacteristics(r io.ReaderAt, size, maxBytes int64) Characteristics {
	c := Characteristics{}

	winLen := int64(ebmlInfoWindow)
	if winLen > maxBytes {
		winLen = maxBytes
	}
	if winLen > size {
		winLen = size
	}

	buf, ok := readWindow(r, 0, winLen, size)
	if !ok {
		c["duration_seconds"] = nil
		c["duration_seconds_reason"] = string(ReasonWorkCeilingBytes)
		c["declared_codec"] = nil
		c["declared_codec_reason"] = string(ReasonWorkCeilingBytes)
		return c
	}

	segmentPayload, foundSegment := firstEBMLChild(buf, ebmlIDSegment)
	if !foundSegment {
		c["duration_seconds"] = nil
		c["duration_seconds_reason"] = string(ReasonWorkCeilingBytes)
		c["declared_codec"] = nil
		c["declared_codec_reason"] = string(ReasonWorkCeilingBytes)
		return c
	}

	if durSec, ok := findEBMLDuration(segmentPayload); ok {
		c["duration_seconds"] = durSec
	} else {
		c["duration_seconds"] = nil
		c["duration_seconds_reason"] = string(ReasonWorkCeilingBytes)
	}

	if codec, ok := findEBMLCodecID(segmentPayload); ok {
		c["declared_codec"] = codec
	} else {
		c["declared_codec"] = nil
		c["declared_codec_reason"] = string(ReasonWorkCeilingBytes)
	}

	return c
}

func findEBMLDuration(segment []byte) (float64, bool) {
	infoPayload, ok := firstEBMLChild(segment, ebmlIDInfo)
	if !ok {
		return 0, false
	}
	// TimestampScale defaults to 1,000,000 (nanoseconds per timestamp unit)
	// per the Matroska spec when the element is absent.
	timestampScale := uint64(1000000)
	if tsBytes, ok := firstEBMLChild(infoPayload, ebmlIDTimestamp); ok {
		if v, ok := ebmlReadUint(tsBytes); ok {
			timestampScale = v
		}
	}
	durBytes, ok := firstEBMLChild(infoPayload, ebmlIDDuration)
	if !ok {
		return 0, false
	}
	durRaw, ok := ebmlReadFloat(durBytes)
	if !ok {
		return 0, false
	}
	if timestampScale == 0 {
		return 0, false
	}
	return durRaw * float64(timestampScale) / 1e9, true
}

func findEBMLCodecID(segment []byte) (string, bool) {
	tracksPayload, ok := firstEBMLChild(segment, ebmlIDTracks)
	if !ok {
		return "", false
	}
	trackEntryPayload, ok := firstEBMLChild(tracksPayload, ebmlIDTrackEntry)
	if !ok {
		return "", false
	}
	codecBytes, ok := firstEBMLChild(trackEntryPayload, ebmlIDCodecID)
	if !ok {
		return "", false
	}
	return string(codecBytes), true
}

// firstEBMLChild returns the payload of the first element of id within buf.
func firstEBMLChild(buf []byte, id uint32) ([]byte, bool) {
	var payload []byte
	found := false
	walkEBMLElements(buf, func(gotID uint32, p []byte) bool {
		if gotID == id {
			payload, found = p, true
			return false
		}
		return true
	})
	return payload, found
}

// walkEBMLElements iterates EBML elements in buf, calling fn(id, payload)
// for each. Unlike walkTopLevelBoxes, an element whose declared size is
// either the reserved "unknown size" marker (RFC 8794 SS6.5 -- all data bits
// set to 1; Segment is routinely written this way by streaming encoders) OR
// simply larger than what remains of buf is clamped to the rest of the
// already-bounded buffer, rather than being rejected outright: a Segment
// with unknown size is a NORMAL, valid file, and refusing to descend into
// it at all would make ebmlInfoWindow useless for the majority of real
// files. This never grows buf itself -- it only ever narrows how much of
// the buffer already in hand is treated as this element's payload.
func walkEBMLElements(buf []byte, fn func(id uint32, payload []byte) bool) {
	pos := 0
	for count := 0; pos < len(buf) && count < maxEBMLElements; count++ {
		id, idLen, ok := ebmlReadIDVint(buf, pos)
		if !ok {
			return
		}
		pos += idLen
		sizeRaw, sizeLen, ok := ebmlReadSizeVint(buf, pos)
		if !ok {
			return
		}
		pos += sizeLen
		remaining := uint64(len(buf) - pos)
		unknownSize := sizeLen > 0 && sizeRaw == (uint64(1)<<uint(7*sizeLen))-1
		end := len(buf)
		if !unknownSize && sizeRaw <= remaining {
			end = pos + int(sizeRaw)
		}
		payload := buf[pos:end]
		if !fn(id, payload) {
			return
		}
		pos = end
	}
}

// ebmlVintLen returns the byte-length of an EBML variable-length integer
// given its first byte (RFC 8794 SS4) -- a deliberate copy of
// convert/avsniff.go's vintLen (that function is unexported by its
// package; see the doc comment on window.go's readWindow for why this
// package copies rather than imports across the boundary).
func ebmlVintLen(first byte) int {
	for i := 0; i < 8; i++ {
		if first&(0x80>>uint(i)) != 0 {
			return i + 1
		}
	}
	return 0
}

// ebmlReadSizeVint decodes an EBML SIZE vint at buf[pos:], masking off the
// length-marker bits. Fails closed the instant the vint's declared length
// would run past the bounded buffer.
func ebmlReadSizeVint(buf []byte, pos int) (value uint64, length int, ok bool) {
	if pos < 0 || pos >= len(buf) {
		return 0, 0, false
	}
	n := ebmlVintLen(buf[pos])
	if n == 0 || pos+n > len(buf) {
		return 0, 0, false
	}
	value = uint64(buf[pos]) &^ (0xFF << uint(8-n))
	for i := 1; i < n; i++ {
		value = value<<8 | uint64(buf[pos+i])
	}
	return value, n, true
}

// ebmlReadIDVint decodes an EBML ELEMENT ID vint at buf[pos:] WITHOUT
// masking the length-marker bits (they are part of an ID's own value).
// Fails closed past the bounded buffer or past the 4-byte maximum ID width.
func ebmlReadIDVint(buf []byte, pos int) (id uint32, length int, ok bool) {
	if pos < 0 || pos >= len(buf) {
		return 0, 0, false
	}
	n := ebmlVintLen(buf[pos])
	if n == 0 || n > 4 || pos+n > len(buf) {
		return 0, 0, false
	}
	var v uint32
	for i := 0; i < n; i++ {
		v = v<<8 | uint32(buf[pos+i])
	}
	return v, n, true
}

// ebmlReadUint decodes an EBML "uinteger" element value: a big-endian
// unsigned integer of 1-8 bytes (RFC 8794 SS7.2).
func ebmlReadUint(b []byte) (uint64, bool) {
	if len(b) == 0 || len(b) > 8 {
		return 0, false
	}
	var v uint64
	for _, by := range b {
		v = v<<8 | uint64(by)
	}
	return v, true
}

// ebmlReadFloat decodes an EBML "float" element value: big-endian IEEE 754,
// either 4 bytes (float32) or 8 bytes (float64) (RFC 8794 SS7.5).
func ebmlReadFloat(b []byte) (float64, bool) {
	switch len(b) {
	case 4:
		return float64(math.Float32frombits(binary.BigEndian.Uint32(b))), true
	case 8:
		return math.Float64frombits(binary.BigEndian.Uint64(b)), true
	default:
		return 0, false
	}
}

// ===== Part 3: RIFF/WAV =====

// wavCharacteristics reads the RIFF/WAVE header and walks its chunks
// (bounded by maxRIFFChunks) looking for `fmt ` (byte rate) and `data`
// (declared size); duration_seconds = data size / byte rate. This is the
// one audio format in this analyzer with a cheap, bounded, exact duration
// -- unlike mp3/ogg below, WAV's RIFF header carries everything needed
// directly, with no frame-walk or tail-read required.
func wavCharacteristics(r io.ReaderAt, size, maxBytes int64) Characteristics {
	c := Characteristics{}

	winLen := maxBytes
	if winLen > size {
		winLen = size
	}
	buf, ok := readWindow(r, 0, winLen, size)
	if !ok || len(buf) < 12 || string(buf[0:4]) != "RIFF" || string(buf[8:12]) != "WAVE" {
		c["duration_seconds"] = nil
		c["duration_seconds_reason"] = string(ReasonMalformedInput)
		return c
	}

	var byteRate, dataSize uint32
	foundFmt, foundData := false, false
	pos := 12
	for count := 0; pos+8 <= len(buf) && count < maxRIFFChunks && !(foundFmt && foundData); count++ {
		chunkID := string(buf[pos : pos+4])
		chunkSize := binary.LittleEndian.Uint32(buf[pos+4 : pos+8])
		chunkStart := pos + 8
		if uint64(chunkStart)+uint64(chunkSize) > uint64(len(buf)) {
			break // declared chunk size runs past the bounded window: fail closed
		}
		chunkData := buf[chunkStart : chunkStart+int(chunkSize)]
		switch chunkID {
		case "fmt ":
			if len(chunkData) >= 16 {
				byteRate = binary.LittleEndian.Uint32(chunkData[8:12])
				foundFmt = true
			}
		case "data":
			dataSize = chunkSize
			foundData = true
		}
		advance := int(chunkSize)
		if advance%2 == 1 {
			advance++ // RIFF chunks are word-aligned; odd sizes get one pad byte
		}
		pos = chunkStart + advance
	}

	if !foundFmt || !foundData || byteRate == 0 {
		c["duration_seconds"] = nil
		c["duration_seconds_reason"] = string(ReasonMalformedInput)
		return c
	}

	c["duration_seconds"] = float64(dataSize) / float64(byteRate)
	return c
}

// ===== Part 4: honest refusal for mp3/ogg (and avi, undesigned this phase) =====

// unsupportedAudioDurationCharacteristics reports duration_seconds as
// unknown for mp3/ogg -- a DECISION (D-01), not a gap left for later.
//
// MP3: exact duration for a genuinely variable-bitrate stream without a
// Xing/VBRI header requires summing every frame -- an unbounded, whole-file
// walk with no cheap bounded shortcut. A file WITH a Xing/VBRI header could
// be read cheaply, but detecting which case applies and branching parser
// behavior on it is exactly the kind of format-coverage-over-boundedness
// trade this phase declines to make (D-02).
//
// OGG: duration requires reading the granule position of the LAST page in
// the stream -- a tail read, structurally identical in spirit to MP4's
// moov search above, except Vorbis and Opus disagree on how a granule
// position converts to seconds, adding a second axis of complexity this
// phase's byte budget does not justify.
//
// Reporting unknown here IS the correct, complete answer for this phase
// (D-01) -- not laziness, and not a defect a future reviewer should "fix"
// by reaching for ffprobe.
func unsupportedAudioDurationCharacteristics() Characteristics {
	return Characteristics{
		"duration_seconds":        nil,
		"duration_seconds_reason": string(ReasonParserUnavailable),
	}
}

// unsupportedVideoCharacteristics reports every video-applicable key as
// unknown for avi -- no AVI RIFF-chunk parser was researched or written in
// this phase; ParserUnavailable is the honest, correct label for that gap
// (D-01), not a stand-in for a value we simply forgot to compute.
func unsupportedVideoCharacteristics() Characteristics {
	return Characteristics{
		"duration_seconds":        nil,
		"duration_seconds_reason": string(ReasonParserUnavailable),
		"width":                   nil,
		"width_reason":            string(ReasonParserUnavailable),
		"height":                  nil,
		"height_reason":           string(ReasonParserUnavailable),
		"declared_codec":          nil,
		"declared_codec_reason":   string(ReasonParserUnavailable),
	}
}
