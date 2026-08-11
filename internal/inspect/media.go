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
func AnalyzeMedia(format string, r io.ReaderAt, size int64, maxBytes int64, tr *Tracker) FamilyResult {
	var chars Characteristics
	switch format {
	case "mp4", "mov":
		chars = isobmffCharacteristics(r, size, maxBytes, true, tr)
	case "m4a":
		chars = isobmffCharacteristics(r, size, maxBytes, false, tr)
	case "mkv", "webm":
		chars = ebmlCharacteristics(r, size, maxBytes, tr)
	case "wav":
		chars = wavCharacteristics(r, size, maxBytes, tr)
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
func isobmffCharacteristics(r io.ReaderAt, size, maxBytes int64, includeDims bool, tr *Tracker) Characteristics {
	c := Characteristics{}

	moovPayload, reason := findMoov(r, size, maxBytes, tr)
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

	if mvhdPayload, ok, ceilingHit := firstChildBox(moovPayload, "mvhd", tr); ok {
		if durSec, ok, malformed := parseMVHD(mvhdPayload); ok {
			c["duration_seconds"] = durSec
		} else {
			_ = malformed // both failure modes (short box, zero timescale) map to the same reason
			c["duration_seconds"] = nil
			c["duration_seconds_reason"] = string(ReasonMalformedInput)
		}
	} else if ceilingHit {
		// firstChildBox's own maxISOBMFFBoxes cap -- not the file's
		// structure -- decided this miss (media.go's own audit finding,
		// same class as image.go's iteration-ceiling defect): OUR resource
		// limit, so this must mark the report selective, never agree with
		// "malformed" (which never does).
		c["duration_seconds"] = nil
		c["duration_seconds_reason"] = string(ReasonWorkCeilingBytes)
	} else {
		// mvhd is MANDATORY inside a well-formed moov (ISO/IEC 14496-12) --
		// its absence here (with the cap NOT exhausted) is a structural
		// fact about THIS file, not a resource ceiling: the full moov box
		// is already in hand and was walked to completion.
		c["duration_seconds"] = nil
		c["duration_seconds_reason"] = string(ReasonMalformedInput)
	}

	// wantHandler selects which trak this analyzer cares about, via the
	// SAME mdia -> hdlr -> handler_type ("vide"/"soun") every real
	// MP4/MOV/M4A muxer already writes: mp4/mov (includeDims==true) want
	// the VIDEO track's dimensions and codec; m4a (includeDims==false) is
	// audio-only and wants the AUDIO track's codec. See findTrackByHandler's
	// own doc comment (PR9R4 finding, media.go:275) for why "the first
	// trak" is no longer an acceptable selection for either.
	wantHandler := isobmffHandlerAudio
	if includeDims {
		wantHandler = isobmffHandlerVideo
	}

	if includeDims {
		if w, h, ok, ceilingHit := findTrackDimensions(moovPayload, wantHandler, tr); ok {
			c["width"] = w
			c["height"] = h
		} else {
			reason := ReasonMalformedInput
			if ceilingHit {
				reason = ReasonWorkCeilingBytes
			}
			c["width"] = nil
			c["width_reason"] = string(reason)
			c["height"] = nil
			c["height_reason"] = string(reason)
		}
	}

	if codec, ok, ceilingHit := findDeclaredCodec(moovPayload, wantHandler, tr); ok {
		c["declared_codec"] = codec
	} else {
		reason := ReasonMalformedInput
		if ceilingHit {
			reason = ReasonWorkCeilingBytes
		}
		c["declared_codec"] = nil
		c["declared_codec_reason"] = string(reason)
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
func findMoov(r io.ReaderAt, size, maxBytes int64, tr *Tracker) ([]byte, Reason) {
	if size <= 0 || maxBytes <= 0 {
		return nil, ReasonMalformedInput
	}

	headLen := maxBytes
	if headLen > size {
		headLen = size
	}
	var headActual int64
	var headCeilingHit bool
	headWin, headOK := readWindow(r, 0, headLen, size)
	if headOK {
		headActual = int64(len(headWin))
		if payload, ok, ceilingHit := firstChildBox(headWin, "moov", tr); ok {
			return payload, ""
		} else {
			headCeilingHit = ceilingHit
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
	var tailCeilingHit bool
	tailWin, tailOK := readWindow(r, tailOff, tailLen, size)
	if tailOK {
		tailActual = int64(len(tailWin))
		if payload, found, ceilingHit := findMoovInTail(tailWin, tr); found {
			return payload, ""
		} else {
			tailCeilingHit = ceilingHit
		}
	}

	if headActual+tailActual >= size && !headCeilingHit && !tailCeilingHit {
		// Both windows together covered the entire file (accounting for
		// possible overlap on small files), NEITHER window's own iteration
		// ceiling fired while looking for moov there, and moov still never
		// appeared -- this is a fact about the FILE, not about our own
		// resource limits. If either ceiling fired, its own window DID
		// cover its bytes but an iteration cap -- not the file's structure
		// -- decided the miss, so this must not collapse into
		// ReasonMalformedInput either (media.go's own audit finding).
		return nil, ReasonMalformedInput
	}
	return nil, ReasonWorkCeilingBytes
}

// maxMoovTailCandidatesScanned bounds how many candidate positions of the
// literal 4-byte "moov" substring within the tail window findMoovInTail will
// try as a real, USABLE box before giving up (T-R4B-02).
//
// PR9R4 finding (media.go:239), verbatim: "When the tail window contains
// the bytes `moov` inside media payload before the real `moov` box,
// bytes.Index selects that incidental first occurrence. If its preceding
// four bytes do not form a valid box size, the single validation attempt
// fails and the later real box is never considered, so a valid file loses
// all MP4 characteristics." The fix is the same shape as the trak-selection
// fix below it in this file: the first match is not necessarily the only
// candidate, so the search must walk forward past a failed candidate
// instead of giving up -- and that walk must itself be bounded, or fixing
// this correctness bug would reopen the unbounded-work class round 3 of
// this same review already closed elsewhere in this package.
//
// PR9R5 finding (media.go:321), verbatim: "If media payload contains
// \x00\x00\x00\x08moov before the real tail moov, this local walk accepts
// the incidental bytes as a valid empty box and immediately returns its
// empty payload. The analyzer then reports malformed characteristics and
// never examines the genuine later box, even though the MP4 itself is
// valid." Round 4 raised the acceptance bar from "the literal substring
// moov was found somewhere" to "parses as a self-consistent box" -- but a
// bare, empty box header parses exactly as cleanly as a real one, so the
// winning criterion never actually distinguished a genuine moov from a
// decoy shaped like one. findMoovInTail now raises the bar again, to
// "yields a USABLE moov structure" (isUsableMoovCandidate) -- the SAME
// candidate cap still bounds how many occurrences are tried either way.
const maxMoovTailCandidatesScanned = 64

// findMoovInTail searches tailWin for a REAL, USABLE moov box, trying every
// literal "moov" substring occurrence in order (bounded by
// maxMoovTailCandidatesScanned) rather than only the first. A candidate is
// first validated by attempting to parse it as one self-consistent
// top-level box via walkTopLevelBoxes (its own declared size must fit
// within the bounded buffer AND its type must genuinely be "moov"); a
// candidate that DOES parse that way is validated FURTHER, against
// isUsableMoovCandidate -- only a candidate that yields the actual
// structure this analyzer needs (mvhd, at least one trak) is accepted.
// Any other candidate, parseable or not, causes the search to continue to
// the NEXT occurrence rather than giving up or accepting an unusable box.
func findMoovInTail(tailWin []byte, tr *Tracker) (payload []byte, found bool, ceilingHit bool) {
	searchFrom := 0
	for candidates := 0; candidates < maxMoovTailCandidatesScanned; candidates++ {
		rel := bytes.Index(tailWin[searchFrom:], []byte("moov"))
		if rel < 0 {
			return nil, false, false
		}
		idx := searchFrom + rel
		searchFrom = idx + 1 // resume one byte later so overlapping occurrences are still found
		if idx < 4 {
			continue
		}
		boxStart := idx - 4
		var candidatePayload []byte
		matchedType := false
		walkTopLevelBoxes(tailWin[boxStart:], tr, func(t string, p []byte) bool {
			if t == "moov" {
				candidatePayload, matchedType = p, true
			}
			return false // only the box located at boxStart is meaningful for this candidate
		})
		if !matchedType {
			continue
		}
		usable, usabilityCeilingHit := isUsableMoovCandidate(candidatePayload, tr)
		if usable {
			return candidatePayload, true, false
		}
		if usabilityCeilingHit {
			// This candidate's OWN usability check could not reach an
			// answer within maxISOBMFFBoxes -- OUR OWN resource limit, not
			// proof this candidate is unusable. Silently treating an
			// inconclusive check as "reject and keep going" would risk
			// eventually reporting "not found" for a file whose real moov
			// this scan never actually ruled out -- abort the whole search
			// with a ceiling result instead, exactly like every other
			// ceilingHit branch in this package.
			return nil, false, true
		}
		// This candidate parsed as a self-consistent "moov" box header, but
		// its payload does not yield mvhd+trak -- PR9R5's own incidental
		// "\x00\x00\x00\x08moov" case, and any other empty/truncated/garbage
		// payload shaped the same way. A box header that merely PARSES is
		// not proof it is the file's real moov; keep searching.
	}
	// The candidate cap was exhausted without validating a USABLE moov box --
	// OUR OWN resource limit, not proof the file lacks one.
	tr.MarkCeilingHit()
	return nil, false, true
}

// isUsableMoovCandidate reports whether payload -- already validated as a
// self-consistent top-level box of type "moov" -- actually carries the
// child structure this analyzer exists to read out of it: mvhd (ISO/IEC
// 14496-12's own MANDATORY moov child, carrying duration) and at least one
// trak (also mandatory -- a moov describing zero tracks describes no media
// at all). A bare box header that merely parses (round 4's own criterion)
// is not evidence of either; this is that missing distinction (PR9R5,
// media.go:321). ceilingHit is true if EITHER lookup exhausted
// firstChildBox's own maxISOBMFFBoxes cap before reaching an answer -- the
// caller must treat that as "this scan could not determine usability",
// never as "not usable" (D-02a, applied one level down from every other
// ceilingHit in this file).
func isUsableMoovCandidate(payload []byte, tr *Tracker) (usable bool, ceilingHit bool) {
	_, hasMvhd, c1 := firstChildBox(payload, "mvhd", tr)
	if !hasMvhd {
		return false, c1
	}
	_, hasTrak, c2 := firstChildBox(payload, "trak", tr)
	return hasTrak, c1 || c2
}

// findTrackDimensions descends moov -> trak (first) -> tkhd and decodes
// its declared width/height. ceilingHit is true if ANY step along the
// descent hit firstChildBox's own maxISOBMFFBoxes cap before an answer was
// reached -- the caller must not report a ceilingHit miss the same way it
// reports a genuine structural absence.
//
// PR9R4 finding (media.go:275), verbatim: "Valid MP4/MOV files may place an
// audio trak before their video trak. This lookup always selects the first
// track, successfully parses its audio tkhd, and reports width and height
// as zero while ignoring the later video track's actual dimensions." Zero
// width/height for an audio track is not "the dimensions are zero" -- it is
// "this track was never about dimensions at all" -- so the fix selects the
// VIDEO track by its own mdia -> hdlr -> handler_type (findTrackByHandler),
// never merely the first trak encountered.
func findTrackDimensions(moovPayload []byte, wantHandler string, tr *Tracker) (width, height uint32, ok bool, ceilingHit bool) {
	trakPayload, found, c1 := findTrackByHandler(moovPayload, wantHandler, tr)
	if !found {
		return 0, 0, false, c1
	}
	tkhdPayload, ok2, c2 := firstChildBox(trakPayload, "tkhd", tr)
	if !ok2 {
		return 0, 0, false, c1 || c2
	}
	w, h, ok3 := parseTKHD(tkhdPayload)
	return w, h, ok3, c1 || c2
}

// findDeclaredCodec descends moov -> trak (selected by wantHandler, see
// findTrackByHandler) -> mdia -> minf -> stbl -> stsd and reads the first
// sample entry's fourCC. ceilingHit is the same signal as
// findTrackDimensions's own.
//
// Selecting by handler here, not merely by the same trak
// findTrackDimensions resolves, is deliberate consistency (not a separate
// finding): reporting the video track's real dimensions alongside a
// declared_codec pulled from a DIFFERENT (audio) track purely because it
// happened to come first would be its own quiet mislabeling -- the same
// "first match treated as the only candidate" shape the plan calls out,
// one function over.
func findDeclaredCodec(moovPayload []byte, wantHandler string, tr *Tracker) (codec string, ok bool, ceilingHit bool) {
	trakPayload, found, c1 := findTrackByHandler(moovPayload, wantHandler, tr)
	if !found {
		return "", false, c1
	}
	mdiaPayload, ok2, c2 := firstChildBox(trakPayload, "mdia", tr)
	if !ok2 {
		return "", false, c1 || c2
	}
	minfPayload, ok3, c3 := firstChildBox(mdiaPayload, "minf", tr)
	if !ok3 {
		return "", false, c1 || c2 || c3
	}
	stblPayload, ok4, c4 := firstChildBox(minfPayload, "stbl", tr)
	if !ok4 {
		return "", false, c1 || c2 || c3 || c4
	}
	stsdPayload, ok5, c5 := firstChildBox(stblPayload, "stsd", tr)
	if !ok5 {
		return "", false, c1 || c2 || c3 || c4 || c5
	}
	codec, ok6 := parseSTSDCodec(stsdPayload)
	return codec, ok6, c1 || c2 || c3 || c4 || c5
}

// isobmffHandlerVideo / isobmffHandlerAudio are the two ISO/IEC 14496-12
// handler_type fourCCs (mdia -> hdlr) this analyzer selects a trak by:
// "vide" for the video track (mp4/mov's own dimensions and codec),
// "soun" for the audio track (m4a's own codec).
const (
	isobmffHandlerVideo = "vide"
	isobmffHandlerAudio = "soun"
)

// maxTrakCandidatesScanned bounds how many `trak` boxes findTrackByHandler
// will open (descend into mdia/hdlr) while looking for a specific handler
// type -- separate from, and smaller than, maxISOBMFFBoxes, which only
// bounds the OUTER top-level walk of moov's own direct children (T-R4B-02).
// A moov box could in principle declare far more trak children than any
// real file would ever have; without a candidate-specific cap, an
// adversarial upload with thousands of tiny trak boxes preceding the real
// one could make this search arbitrarily expensive even though
// maxISOBMFFBoxes already bounds the outer walk.
const maxTrakCandidatesScanned = 64

// findTrackByHandler walks moovPayload's own top-level `trak` boxes
// (bounded by maxTrakCandidatesScanned, not merely the first one
// encountered) looking for the first whose mdia -> hdlr declares
// handler_type == wantHandler. ceilingHit is true if either the outer moov
// walk (maxISOBMFFBoxes, via walkTopLevelBoxes) or this function's own
// candidate cap was exhausted before a match was found or ruled out -- in
// that case the caller must report unknown, never silently fall back to
// whichever trak happened to be examined last (that fallback is exactly
// the PR9R4 finding this function exists to close).
func findTrackByHandler(moovPayload []byte, wantHandler string, tr *Tracker) (trakPayload []byte, found bool, ceilingHit bool) {
	candidates := 0
	outerCeilingHit := walkTopLevelBoxes(moovPayload, tr, func(t string, p []byte) bool {
		if found {
			return false
		}
		if t != "trak" {
			return true
		}
		if candidates >= maxTrakCandidatesScanned {
			tr.MarkCeilingHit()
			ceilingHit = true
			return false
		}
		candidates++
		mdiaPayload, ok1, c1 := firstChildBox(p, "mdia", tr)
		if !ok1 {
			ceilingHit = ceilingHit || c1
			return true
		}
		hdlrPayload, ok2, c2 := firstChildBox(mdiaPayload, "hdlr", tr)
		if !ok2 {
			ceilingHit = ceilingHit || c1 || c2
			return true
		}
		if len(hdlrPayload) < 12 {
			return true // malformed hdlr: not a match, keep looking
		}
		if string(hdlrPayload[8:12]) == wantHandler {
			trakPayload, found = p, true
			return false
		}
		return true
	})
	ceilingHit = ceilingHit || outerCeilingHit
	return trakPayload, found, ceilingHit
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
//
// ceilingHit reports whether maxISOBMFFBoxes -- not a malformed/truncated
// box header, not fn returning false, not the end of buf -- is what stopped
// the walk (media.go's own sibling of image.go's iteration-ceiling class,
// found auditing media.go per the round 2 plan's explicit instruction to
// check every scanner, not only the codes the reviewer named): a box this
// caller is looking for (mvhd, trak, stsd, ...) can be preceded by more
// sibling boxes than the cap allows, and treating that exhaustion the same
// as "genuinely absent" would misattribute THIS scan's own resource limit
// to a fact about the file -- exactly the D-02a mistake, one level removed.
func walkTopLevelBoxes(buf []byte, tr *Tracker, fn func(boxType string, payload []byte) bool) (ceilingHit bool) {
	i := 0
	for count := 0; i+8 <= len(buf); count++ {
		if count >= maxISOBMFFBoxes {
			tr.MarkCeilingHit()
			return true
		}
		size64 := uint64(binary.BigEndian.Uint32(buf[i : i+4]))
		boxType := string(buf[i+4 : i+8])
		headerLen := 8
		switch size64 {
		case 1:
			if i+16 > len(buf) {
				return false
			}
			size64 = binary.BigEndian.Uint64(buf[i+8 : i+16])
			headerLen = 16
		case 0:
			return false // "extends to EOF": not expected within a bounded window, bail conservatively
		}
		if size64 < uint64(headerLen) || size64 > uint64(len(buf)-i) {
			return false // truncated or malformed within the bounded window: fail closed
		}
		size := int(size64)
		if !fn(boxType, buf[i+headerLen:i+size]) {
			return false
		}
		i += size
	}
	return false
}

// firstChildBox returns the payload of the first top-level box of boxType
// within buf, whether it was found, and whether maxISOBMFFBoxes was
// exhausted before an answer was reached (see walkTopLevelBoxes's own doc
// comment for why callers must not collapse that into "not found").
func firstChildBox(buf []byte, boxType string, tr *Tracker) (payload []byte, found bool, ceilingHit bool) {
	ceilingHit = walkTopLevelBoxes(buf, tr, func(t string, p []byte) bool {
		if t == boxType {
			payload, found = p, true
			return false
		}
		return true
	})
	return payload, found, ceilingHit
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
// Safe-refusal discipline required by 58-RESEARCH.md Assumption A2: the
// window is NEVER grown adaptively "until it is found" -- growing a buffer
// in response to untrusted content not containing what you are looking for
// is exactly the lever an adversarial input turns into a resource-
// exhaustion primitive (T-58-23).
//
// windowCoversWholeFile (media.go's own audit finding, same round as the
// isobmffCharacteristics fix above) distinguishes the two facts a miss can
// mean here: if the window covers the ENTIRE file AND the internal element
// walk never hit its own maxEBMLElements cap, a miss is a genuine structural
// fact about the file (ReasonMalformedInput) and must NOT make Coverage
// selective; otherwise the miss is THIS scan's own ceiling
// (ReasonWorkCeilingBytes), which must. An earlier version of this function
// reported every miss as ReasonWorkCeilingBytes unconditionally -- safe in
// the sense that it never claimed "clean" or "complete" falsely, but wrong
// in the OTHER direction: a small, fully-buffered, genuinely malformed mkv
// would always mark the whole report selective even though nothing was
// actually left unexamined.
func ebmlCharacteristics(r io.ReaderAt, size, maxBytes int64, tr *Tracker) Characteristics {
	c := Characteristics{}

	winLen := int64(ebmlInfoWindow)
	if winLen > maxBytes {
		winLen = maxBytes
	}
	if winLen > size {
		winLen = size
	}
	windowCoversWholeFile := winLen >= size

	buf, ok := readWindow(r, 0, winLen, size)
	if !ok {
		c["duration_seconds"] = nil
		c["duration_seconds_reason"] = string(ReasonWorkCeilingBytes)
		c["declared_codec"] = nil
		c["declared_codec_reason"] = string(ReasonWorkCeilingBytes)
		return c
	}

	segmentPayload, foundSegment, segCeilingHit := firstEBMLChild(buf, ebmlIDSegment, tr)
	if !foundSegment {
		reason := ebmlMissReason(windowCoversWholeFile, segCeilingHit)
		c["duration_seconds"] = nil
		c["duration_seconds_reason"] = string(reason)
		c["declared_codec"] = nil
		c["declared_codec_reason"] = string(reason)
		return c
	}

	if durSec, ok, ceilingHit := findEBMLDuration(segmentPayload, tr); ok {
		c["duration_seconds"] = durSec
	} else {
		c["duration_seconds"] = nil
		c["duration_seconds_reason"] = string(ebmlMissReason(windowCoversWholeFile, ceilingHit))
	}

	if codec, ok, ceilingHit := findEBMLCodecID(segmentPayload, tr); ok {
		c["declared_codec"] = codec
	} else {
		c["declared_codec"] = nil
		c["declared_codec_reason"] = string(ebmlMissReason(windowCoversWholeFile, ceilingHit))
	}

	return c
}

// ebmlMissReason is ebmlCharacteristics's single verdict rule for a miss:
// OUR OWN ceiling (byte window did not cover the whole file, or an internal
// element walk hit maxEBMLElements) versus a genuine structural fact about
// the file. See ebmlCharacteristics's own doc comment for why these must
// never collapse into one answer.
func ebmlMissReason(windowCoversWholeFile, ceilingHit bool) Reason {
	if !windowCoversWholeFile || ceilingHit {
		return ReasonWorkCeilingBytes
	}
	return ReasonMalformedInput
}

// findEBMLDuration returns Duration (seconds) plus whether ANY step along
// Segment -> Info -> Duration/TimestampScale hit firstEBMLChild's own
// maxEBMLElements cap before an answer was reached.
func findEBMLDuration(segment []byte, tr *Tracker) (durationSeconds float64, ok bool, ceilingHit bool) {
	infoPayload, ok1, c1 := firstEBMLChild(segment, ebmlIDInfo, tr)
	if !ok1 {
		return 0, false, c1
	}
	// TimestampScale defaults to 1,000,000 (nanoseconds per timestamp unit)
	// per the Matroska spec when the element is absent (RFC 8794/Matroska).
	// quick/260811-r3s (media.go:575 finding): that default is a legitimate
	// substitution ONLY when TimestampScale is PROVEN absent -- not when its
	// own search was cut short by maxEBMLElements. An earlier version of
	// this function discarded firstEBMLChild's ceilingHit here ("a default
	// is substituted either way, so whether the cap fired ... changes
	// nothing"), which is exactly backwards: if Info places 512+ filler
	// elements before a NON-default TimestampScale, this lookup exhausts its
	// own cap, the separate Duration lookup below can still succeed, and the
	// function would publish a duration computed against the WRONG
	// (default) scale with complete-looking coverage. tsCeilingHit is
	// tracked and, if Duration was otherwise found, forces a ceiling result
	// instead of a computed value -- the same "unknown, not a guess"
	// discipline D-02a already applies to checks, applied here to a
	// characteristic's own default-value substitution.
	timestampScale := uint64(1000000)
	tsCeilingHit := false
	if tsBytes, tsOK, tsCeil := firstEBMLChild(infoPayload, ebmlIDTimestamp, tr); tsOK {
		if v, ok := ebmlReadUint(tsBytes); ok {
			timestampScale = v
		}
	} else {
		tsCeilingHit = tsCeil
	}
	durBytes, ok2, c2 := firstEBMLChild(infoPayload, ebmlIDDuration, tr)
	if !ok2 {
		return 0, false, c1 || c2
	}
	if tsCeilingHit {
		// Duration WAS found, but the scale needed to convert its raw value
		// to seconds was never proven absent -- only unreached. Publishing
		// durRaw against the unproven default would be a guess dressed as a
		// fact (the same reasoning distinguishing clean from unknown,
		// applied to a characteristic instead of a Check).
		return 0, false, true
	}
	durRaw, ok3 := ebmlReadFloat(durBytes)
	if !ok3 {
		return 0, false, false // Duration WAS found, just not decodable -- a structural fact, not a ceiling
	}
	if timestampScale == 0 {
		return 0, false, false
	}
	return durRaw * float64(timestampScale) / 1e9, true, false
}

// findEBMLCodecID returns the declared codec plus the same ceilingHit signal
// as findEBMLDuration's own.
func findEBMLCodecID(segment []byte, tr *Tracker) (codec string, ok bool, ceilingHit bool) {
	tracksPayload, ok1, c1 := firstEBMLChild(segment, ebmlIDTracks, tr)
	if !ok1 {
		return "", false, c1
	}
	trackEntryPayload, ok2, c2 := firstEBMLChild(tracksPayload, ebmlIDTrackEntry, tr)
	if !ok2 {
		return "", false, c1 || c2
	}
	codecBytes, ok3, c3 := firstEBMLChild(trackEntryPayload, ebmlIDCodecID, tr)
	if !ok3 {
		return "", false, c1 || c2 || c3
	}
	return string(codecBytes), true, false
}

// firstEBMLChild returns the payload of the first element of id within buf,
// whether it was found, and whether maxEBMLElements was exhausted before an
// answer was reached (walkEBMLElements's own ceilingHit).
func firstEBMLChild(buf []byte, id uint32, tr *Tracker) (payload []byte, found bool, ceilingHit bool) {
	ceilingHit = walkEBMLElements(buf, tr, func(gotID uint32, p []byte) bool {
		if gotID == id {
			payload, found = p, true
			return false
		}
		return true
	})
	return payload, found, ceilingHit
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
//
// ceilingHit reports whether maxEBMLElements -- not a malformed vint, not fn
// returning false, not the end of buf -- is what stopped the walk (the EBML
// analog of walkTopLevelBoxes's own ceilingHit, media.go's audit finding).
func walkEBMLElements(buf []byte, tr *Tracker, fn func(id uint32, payload []byte) bool) (ceilingHit bool) {
	pos := 0
	for count := 0; pos < len(buf); count++ {
		if count >= maxEBMLElements {
			tr.MarkCeilingHit()
			return true
		}
		id, idLen, ok := ebmlReadIDVint(buf, pos)
		if !ok {
			return false
		}
		pos += idLen
		sizeRaw, sizeLen, ok := ebmlReadSizeVint(buf, pos)
		if !ok {
			return false
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
			return false
		}
		pos = end
	}
	return false
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
//
// media.go's own audit finding (round 2 plan's explicit instruction to
// check every scanner, not only the codes the reviewer named): this
// function previously reported EVERY miss as ReasonMalformedInput,
// including a miss caused by (a) the byte window not covering the whole
// file, or (b) maxRIFFChunks being exhausted before fmt/data were both
// seen -- both are OUR OWN resource limits, exactly the D-02a class every
// other analyzer in this package already closed. windowTruncated and
// ceilingHit below close it here too.
func wavCharacteristics(r io.ReaderAt, size, maxBytes int64, tr *Tracker) Characteristics {
	c := Characteristics{}

	winLen := maxBytes
	if winLen > size {
		winLen = size
	}
	windowTruncated := winLen < size

	buf, ok := readWindow(r, 0, winLen, size)
	if !ok {
		reason := ReasonMalformedInput
		if windowTruncated {
			reason = ReasonWorkCeilingBytes
		}
		c["duration_seconds"] = nil
		c["duration_seconds_reason"] = string(reason)
		return c
	}
	if len(buf) < 12 || string(buf[0:4]) != "RIFF" || string(buf[8:12]) != "WAVE" {
		reason := ReasonMalformedInput
		if windowTruncated {
			reason = ReasonWorkCeilingBytes
		}
		c["duration_seconds"] = nil
		c["duration_seconds_reason"] = string(reason)
		return c
	}

	var byteRate, dataSize uint32
	foundFmt, foundData := false, false
	pos := 12
	ceilingHit := false
	for count := 0; pos+8 <= len(buf) && !(foundFmt && foundData); count++ {
		if count >= maxRIFFChunks {
			ceilingHit = true
			tr.MarkCeilingHit()
			break
		}
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
		reason := ReasonMalformedInput
		if windowTruncated || ceilingHit {
			reason = ReasonWorkCeilingBytes
		}
		c["duration_seconds"] = nil
		c["duration_seconds_reason"] = string(reason)
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
