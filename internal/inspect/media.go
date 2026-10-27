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

	// wantHandler selects which trak this analyzer cares about, via the
	// SAME mdia -> hdlr -> handler_type ("vide"/"soun") every real
	// MP4/MOV/M4A muxer already writes: mp4/mov (includeDims==true) want
	// the VIDEO track's dimensions and codec; m4a (includeDims==false) is
	// audio-only and wants the AUDIO track's codec. See findTrackByHandler's
	// own doc comment (PR9R4 finding, media.go:275) for why "the first
	// trak" is no longer an acceptable selection for either.
	//
	// findMoov no longer takes wantHandler/includeDims at all (PR9R9: it
	// proves placement from the box chain alone, never from what a
	// candidate's content extracts -- see findMoov's own doc comment). Both
	// are resolved here purely for findTrackDimensions/findDeclaredCodec
	// below, once a moov has already been found.
	wantHandler := isobmffHandlerAudio
	if includeDims {
		wantHandler = isobmffHandlerVideo
	}

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
		c["duration_seconds_reason"] = string(tr.ceilingReason())
	} else {
		// mvhd is MANDATORY inside a well-formed moov (ISO/IEC 14496-12) --
		// its absence here (with the cap NOT exhausted) is a structural
		// fact about THIS file, not a resource ceiling: the full moov box
		// is already in hand and was walked to completion.
		c["duration_seconds"] = nil
		c["duration_seconds_reason"] = string(ReasonMalformedInput)
	}

	if includeDims {
		if w, h, ok, ceilingHit := findTrackDimensions(moovPayload, wantHandler, tr); ok {
			c["width"] = w
			c["height"] = h
		} else {
			reason := Reason(ReasonMalformedInput)
			if ceilingHit {
				reason = tr.ceilingReason()
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
		reason := Reason(ReasonMalformedInput)
		if ceilingHit {
			reason = tr.ceilingReason()
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
//
// PR9R8 finding (media.go:498), verbatim: "an mdat payload can contain a
// self-consistent fake moov with only a parseable 20-byte mvhd carrying
// attacker-chosen timescale/duration fields before the genuine tail moov.
// This branch accepts it immediately... Successful consumer parsing does
// not establish that bytes found by substring search are a top-level
// box; validate the candidate's placement in the file's top-level box
// sequence." This is the FOURTH defeat of the same acceptance bar (round
// 4: substring; round 5: parses as a box; round 6: children present;
// round 8: consumer extraction succeeds) -- every one of those criteria
// asked "is what we found any good", never "is what we found actually
// WHERE a top-level box would be". When the file is larger than maxBytes
// (oversizedFile below) and moov is not in the head, this function PROVES
// a tail-anchored position via headBoxChainProvenEnd: walk the REAL
// top-level box chain from file offset 0 -- exactly like a conformant
// parser would, never looking inside any box's own payload -- until a box
// is found whose declared size runs past the head window; THAT box's own
// declared END, computed from its header alone (never its payload), is a
// FACT about the file's structure, not a guess. A box planted inside e.g.
// `mdat`'s own opaque payload can never produce this proof, because
// reaching it would require looking INSIDE the preceding box's payload,
// which a genuine top-level walk never does -- it only ever hops FORWARD
// by a box's own declared size, skipping `mdat`'s entire declared payload
// (fake `moov` included) in one hop.
//
// PR9R9 finding #1 (coordinator review of the round-8 fix, media.go:498):
// "headBoxChainProvenEnd returning !ok is trivially attacker-controlled
// [size==0, a header declaring less than its own length, a 64-bit size
// field truncated at the window edge]. In every one of those, control
// falls straight through to findMoovInTail, the exact substring scan the
// finding is about, and the decoy in mdat wins precisely as it did
// before." Falling through to the substring scan whenever placement could
// not be proven reopened exactly the hole the proof exists to close -- an
// attacker picks the head box shape, so the file shapes that defeat
// headBoxChainProvenEnd's proof are exactly the ones an attacker would
// choose. When oversizedFile is true, the substring scan is therefore
// NEVER invoked anymore: either the proof succeeds and the candidate it
// names is trusted directly, or it does not, and the outcome is an HONEST
// unknown or a PROVEN absence -- never a guess.
//
// PR9R9 finding #2 (a SECOND coordinator review, of finding #1's own
// fix): the fix above only closed the oversizedFile branch. A second look
// caught a call this file's own comment had defended as merely
// "redundant" when the file fits within maxBytes (!oversizedFile) --
// coordinator's own correction, verbatim: "Complete coverage makes the
// substring scan unnecessary -- it does not make it harmless, and those
// are different properties... in the one situation where that call runs,
// every candidate it can possibly find is a decoy. It cannot rescue a
// genuine top-level box, because [the head walk] already proved there
// isn't one." When !oversizedFile, headWin already covers the ENTIRE
// file, and the initial firstChildBox(headWin, "moov", tr) call above
// already performed a complete, genuine top-level walk before this
// function ever reaches the code below -- if that walk did not find
// "moov", no genuine top-level moov exists anywhere in the file, full
// stop. A substring scan run afterward, over the SAME bytes, could
// therefore only ever find and accept a decoy (bytes spelling "moov"
// inside some OTHER box's own payload), publishing a fake value exactly
// like the original finding describes -- just reached through the
// SUCCESS-case regime instead of the fallback one. So the substring scan
// is not consulted here either; see the !oversizedFile branch below for
// the exact reasoning per outcome.
//
// Both PR9R9 fixes together mean the substring scan this whole file used
// to depend on (findMoovInTail, and the acceptance criterion it ran,
// isUsableMoovCandidate) has NO reachable caller left at all and has been
// deleted -- see maxMoovTailCandidatesScanned's own doc comment for what
// replaced it and why the round 4/5/6 tests that once exercised it still
// pass, with updated expectations, against the new mechanism. An
// unnecessary unknown costs a missing characteristic; an unproven accept
// costs a wrong value presented as fact, and D-01/D-03 both forbid the
// latter.
//
// PR9R8 finding (media.go:255), verbatim: "For an MP4/MOV larger than
// maxBytes whose moov is not found in the head, the analyzer first reads
// maxBytes bytes... and then performs this additional tail read of up to
// min(mp4TailWindow, maxBytes). The caller subsequently charges only
// maxBytes to the tracker, so a small configured ceiling can be exceeded
// by nearly 2x." When size > maxBytes (oversizedFile, the exact case
// that finding describes), a SHARE of maxBytes is now reserved for the
// tail window up front and deducted from what the head window may read,
// so the two windows together never spend more than the SAME maxBytes
// ceiling the caller charges once (internal/api/inspect_handlers.go's own
// "charge the full allowance" accounting comment -- out of this round's
// write scope, so this function must not rely on that caller changing).
// When the whole file already fits within maxBytes there is nothing to
// reserve: both windows simply cover the file, exactly as before, and
// their combined read is bounded by size itself, never by maxBytes at
// all -- unchanged from round 0.
//
// PR9R9 finding #3 (double charge, media.go:337, a coordinator review of
// the round-8 fix above): round 8's own closing sentence here used to
// read "every actual byte read is ALSO reported to tr directly via
// consumeActual, so a Tracker configured with its own tight
// Budget.MaxBytes -- not just the maxBytes parameter this function
// receives -- observes the SAME true total, provable independently of
// whatever any caller does with its own return value." That defense
// assumed a caller whose OWN charge could legitimately diverge from tr's
// real remaining budget -- but the ONE production caller,
// dispatchInspectFamilies (internal/api/inspect_handlers.go), captures
// maxBytes := tr.Remaining() ONCE, before dispatch, and unconditionally
// charges tr.Consume(maxBytes) with that SAME captured value afterward,
// regardless of what any family analyzer actually read -- the identical
// convention every OTHER family (PDF, container, image) already relies
// on exclusively; none of them ever call tr.Consume themselves. Given
// that, findMoov's own direct tr.Consume call (via the now-deleted
// consumeActual helper) was never observing a genuinely SEPARATE budget
// -- it was charging the SAME shared Tracker a second time, on top of
// the dispatcher's own full-allowance charge, for every ISOBMFF file
// whose head window read any bytes at all. A file that reads even ONE
// actual byte here pushed the Tracker's total consumed past whatever
// tr.Remaining() was at dispatch time, tripping ReasonWorkCeilingBytes
// unconditionally -- reproduced live, against the real HTTP handler:
// `coverage.complete=false reason="work_ceiling_bytes"` for a small,
// fully-parsed, valid MP4. The window-sizing fix above
// (headLen+tailReserve <= maxBytes) is what actually closes PR9R8's own
// "nearly 2x" finding -- it bounds what THIS function may ever read,
// independent of any accounting -- so the redundant direct charge added
// nothing but the double count. dispatchInspectFamilies remains the SOLE
// owner of tr's byte/time budget for this (and every other) family
// analyzer; findMoov still reports local iteration ceilings via
// tr.MarkCeilingHit() exactly as before (a SEPARATE receptacle from
// Consume's own byte/time accounting -- see budget.go's own doc
// comment), but it never calls tr.Consume itself, matching every other
// family analyzer in this package.
func findMoov(r io.ReaderAt, size, maxBytes int64, tr *Tracker) ([]byte, Reason) {
	if size <= 0 || maxBytes <= 0 {
		return nil, ReasonMalformedInput
	}

	// oversizedFile gates every fix in this function's own doc comment: "the
	// file exceeds maxBytes" is the ONE condition that makes a SEPARATE tail
	// budget reservation necessary (a file that already fits within
	// maxBytes never needs to spend more than size on either window) and
	// the ONE condition that requires a SEPARATE, explicit proof step
	// (headBoxChainProvenEnd) at all -- a head window covering the WHOLE
	// file, below, is already a complete, genuine top-level walk on its
	// own, with no substring scan of any kind ever consulted either way
	// (see this function's own doc comment, PR9R9 finding #2, for why
	// "complete" and "harmless" are not the same property, and why the
	// !oversizedFile branch below still needs its own explicit reasoning
	// rather than silently doing nothing).
	oversizedFile := size > maxBytes
	headLen := maxBytes
	var tailReserve int64
	if oversizedFile {
		tailReserve = maxBytes / 4
		if tailReserve > mp4TailWindow {
			tailReserve = mp4TailWindow
		}
		headLen = maxBytes - tailReserve
	}
	if headLen > size {
		headLen = size
	}
	var headCeilingHit bool
	headWin, headOK := readWindow(r, 0, headLen, size)
	if headOK {
		if payload, ok, ceilingHit := firstChildBox(headWin, "moov", tr); ok {
			return payload, ""
		} else {
			headCeilingHit = ceilingHit
		}
	}

	if !oversizedFile {
		// PR9R9 follow-up (coordinator review of the round-8 fix): when the
		// file fits within maxBytes, headLen == size, so headWin above IS
		// the entire file, and the firstChildBox call just above already
		// walked its REAL top-level box chain in full, looking for "moov"
		// among every genuine top-level box. It did not find one -- or we
		// would already have returned. That means any occurrence of the
		// literal bytes "moov" ANYWHERE else in this file is necessarily
		// INSIDE some other box's own payload (a decoy), never a genuine
		// top-level box: complete coverage makes a substring scan
		// UNNECESSARY, but a coordinator review correctly pointed out that
		// is a different property from HARMLESS -- handing those bytes to
		// findMoovInTail could still find and ACCEPT a decoy (parseable
		// mvhd, real-looking values), publishing a fake duration exactly
		// like the PR9R8 finding describes, just reached through the
		// SUCCESS-case regime instead of the fallback one. So the outcome
		// here is decided by the head walk's own result alone -- no
		// substring scan is ever consulted for a value in this branch:
		//   - !headOK: readWindow's own ReadAt call failed. headLen == size
		//     in this branch by construction (see the clamp above), and
		//     size > 0 / maxBytes > 0 are already guaranteed by this
		//     function's own top-of-function guard, so readWindow's other
		//     two !ok causes (n<=0, off>=size) can never fire here -- the
		//     window WAS sized to cover the file IN FULL. A full-coverage
		//     window that still fails to read is an operational inability
		//     to read the upload at all, never a fact about the file's
		//     structure and never THIS scan's own byte ceiling (raising
		//     maxBytes would not fix it) -- ReasonReadFailed (PR9R9 finding,
		//     media.go:382), the SAME rule wavCharacteristics and
		//     scanImageMetadata already apply to their own identically-
		//     shaped "full window, still failed" branch.
		//   - headCeilingHit (headOK true, but maxISOBMFFBoxes exhausted
		//     before the walk could finish): OUR OWN resource limit, not a
		//     read failure and not a fact about the file -- ReasonWorkCeilingBytes.
		//   - otherwise: the walk covered every top-level box in the file,
		//     either reaching the natural end cleanly or stopping at a
		//     malformed/truncated box along the way (walkTopLevelBoxes
		//     itself does not distinguish those two exits -- both return
		//     found=false, ceilingHit=false) -- and "moov" was not among
		//     them. A malformed/truncated box is itself a structural fact
		//     about this file (ReasonMalformedInput's own definition:
		//     "structure is present but did not parse"), so both exits are
		//     the SAME honest answer: moov is not a genuine top-level box
		//     in this file, full stop -- ReasonMalformedInput. The
		//     alternative -- letting a substring scan try to "rescue" a
		//     moov past a broken chain -- cannot distinguish a genuinely
		//     rescued box from a planted decoy, since neither has proven
		//     placement; that is precisely the trade this analyzer refuses
		//     to make (D-01/D-03).
		if !headOK {
			return nil, ReasonReadFailed
		}
		if headCeilingHit {
			return nil, tr.ceilingReason()
		}
		return nil, ReasonMalformedInput
	}

	// STEP 2 (oversizedFile only): try the tail. The tail window starts
	// mid-file, so the bytes at its own offset 0 are the MIDDLE of
	// whatever box precedes moov (very likely `mdat`), not a box header at
	// all.
	tailLen := tailReserve
	if tailLen > size {
		tailLen = size
	}
	tailOff := size - tailLen
	if tailOff < 0 {
		tailOff = 0
	}
	tailWin, tailOK := readWindow(r, tailOff, tailLen, size)
	if !tailOK {
		// No tail window to prove anything against at all -- our own
		// inability to read, never a fact about the file's structure.
		//
		// Audited against the SAME "full coverage still failed ->
		// ReasonReadFailed" rule findMoov's own head-window branch above
		// now applies (PR9R9 sweep, this function's own doc comment): unlike
		// that branch, tailLen == tailReserve here is ALWAYS strictly less
		// than size in the oversizedFile path this STEP belongs to (size >
		// maxBytes >= tailReserve by construction), so this window can never
		// be sized to cover the file in full -- it is unconditionally
		// truncated, exactly like wavCharacteristics's own windowTruncated
		// branch. WAV's rule keeps ReasonWorkCeilingBytes for EVERY miss on
		// a truncated window, real ReadAt failure or degenerate (n<=0)
		// alike, because a genuinely undersized window cannot be told apart
		// from an operational failure without first proving the window
		// SHOULD have been big enough -- it should not have been, here, by
		// design. No behavior change; this comment records that this site
		// was checked, not skipped.
		return nil, ReasonWorkCeilingBytes
	}

	// PR9R9 fix: PROVE, never scan. See this function's own doc comment for
	// why findMoovInTail (substring scan) must not run at all in this
	// branch.
	nextOff, ok := headBoxChainProvenEnd(headWin, tr)
	if !ok {
		// headBoxChainProvenEnd could not establish ANY proven top-level
		// boundary -- whether because its own maxISOBMFFBoxes ceiling
		// fired, or because the box it was examining was malformed/
		// declared size 0/had a 64-bit size field truncated at the window
		// edge. Every one of these is OUR OWN inability to prove a
		// position, never a fact about the file (an attacker chooses these
		// shapes specifically to defeat the proof, so treating any of them
		// as "genuinely absent" would hand the decoy right back its
		// opening).
		return nil, tr.ceilingReason()
	}
	if nextOff < tailOff {
		// The proven boundary exists but falls in the gap between what the
		// head and tail windows actually cover -- we genuinely do not know
		// what is there.
		return nil, ReasonWorkCeilingBytes
	}
	if nextOff >= size {
		// The proven boundary is AT or PAST the file's actual end: either
		// nothing more can follow within the real file (declaring more
		// would be dishonest), or this box's own declared size already
		// claims more bytes than the file has. Combined with the
		// [0, nextOff) chain headBoxChainProvenEnd already walked
		// box-by-box (and the prior firstChildBox(headWin, "moov", ...)
		// call already ruled "moov" out among all of those), this is
		// COMPLETE, proven coverage of the file's entire top-level
		// structure: moov genuinely does not exist.
		return nil, ReasonMalformedInput
	}
	rel := nextOff - tailOff
	if payload, found, ceilingHit := firstChildBox(tailWin[rel:], "moov", tr); found {
		return payload, ""
	} else if ceilingHit {
		return nil, tr.ceilingReason()
	}
	// tailWin's own end IS the file's true end (tailOff+tailLen == size by
	// construction), so a walk from the PROVEN nextOff all the way to
	// len(tailWin) that neither found "moov" nor hit a ceiling has just
	// walked the ENTIRE remainder of the file as a real top-level chain,
	// cleanly. Combined with the equally proven [0, nextOff) head chain,
	// moov is proven absent from the file, not merely unreached by this
	// scan.
	return nil, ReasonMalformedInput
}

// headBoxChainProvenEnd walks REAL top-level boxes within headWin (which
// begins at absolute file offset 0) using the same size64/headerLen
// header parsing walkTopLevelBoxes already uses, but instead of matching
// a specific box type, it looks for the FIRST box whose declared size
// extends past headWin's own buffered length -- the box walkTopLevelBoxes
// itself cannot examine further, because only its header (never its
// payload) is in hand. That box's own declared END, computed purely from
// its header (its own start position plus its own declared size64), is a
// PROVEN absolute file offset: the file's own structure says so, and this
// function never reads or trusts a single byte of that box's payload to
// reach it.
//
// This is what closes PR9R8's finding (media.go:498): a box planted
// inside another box's own declared payload (e.g. a fake `moov` hidden
// inside `mdat`) can never produce this proof, because reaching it would
// require looking INSIDE the preceding box's payload -- which this walk,
// by construction, never does; it only ever hops forward by a box's own
// declared size, the same discipline walkTopLevelBoxes already applies
// when the whole box (header AND payload) is in hand.
//
// ok is false when: headWin is too short to reach such a box, a box's
// own header is malformed (declared size smaller than its own header),
// a box's declared size extends to EOF (size64==0, never expected within
// a bounded window), or maxISOBMFFBoxes was exhausted first (ceilingHit
// is marked on tr in that case, the same convention walkTopLevelBoxes
// itself already uses).
//
// PR9R9 finding (media.go:498, the round-8 fix's own gap): every one of
// these !ok shapes is TRIVIALLY attacker-controlled -- a crafted file can
// choose exactly which one fires. The original round-8 fix fell back to
// findMoovInTail's substring scan whenever ok was false, which handed the
// decoy this whole mechanism exists to defeat a second, guaranteed way
// in: an attacker simply shapes the box after ftyp to trip one of these
// cases, and the fallback scans straight into their planted `moov`.
// Callers MUST NOT fall back to the substring search on !ok (or on any
// other failure to reach a usable "moov" from the proven position) --
// report an honest unknown (or a proven absence, when full coverage was
// established) instead. See findMoov's own doc comment and its
// oversizedFile branch for the exact reasoning per outcome.
func headBoxChainProvenEnd(headWin []byte, tr *Tracker) (nextOffset int64, ok bool) {
	i := 0
	for count := 0; i+8 <= len(headWin); count++ {
		if count >= maxISOBMFFBoxes || tr.OutOfTime() {
			tr.MarkCeilingHit()
			return 0, false
		}
		size64 := uint64(binary.BigEndian.Uint32(headWin[i : i+4]))
		headerLen := 8
		switch size64 {
		case 1:
			if i+16 > len(headWin) {
				return 0, false
			}
			size64 = binary.BigEndian.Uint64(headWin[i+8 : i+16])
			headerLen = 16
		case 0:
			return 0, false // "extends to EOF": no further box to prove a position from
		}
		if size64 < uint64(headerLen) {
			return 0, false // malformed: never a valid jump point
		}
		if size64 > uint64(len(headWin)-i) {
			// This box's own header IS fully buffered (the loop condition
			// plus the 64-bit branch above both already guarantee that);
			// only its PAYLOAD runs past headWin. Its declared end is a
			// proven absolute file offset -- headWin[0] is file offset 0.
			return int64(i) + int64(size64), true
		}
		i += int(size64)
	}
	return 0, false
}

// maxMoovTailCandidatesScanned is now a FIXTURE-SIZING constant only,
// retained solely because three tests in media_test.go still reference it
// directly by name (TestFindMoovInTailUsabilityCeilingExhaustionIsWorkCeilingBytes,
// TestFindMoovInTailWorstCaseCandidateValidationStaysBounded,
// TestFindMoovInTailCandidateCeilingIsWorkCeilingBytes) -- it no longer
// bounds any live search algorithm; the algorithm it once bounded
// (findMoovInTail, T-R4B-02) has been deleted.
//
// History, for a future reader wondering why the fixtures those tests
// build still exist: rounds 4, 5, and 6 each raised the acceptance bar for
// a tail-anchored "moov" candidate found by literal substring search --
// "substring present" (PR9R4, defeated by an incidental occurrence);
// "parses as a self-consistent box" (PR9R5, defeated by a valid, empty
// box); "mvhd/trak children present" (PR9R6, defeated by children that
// were present but themselves empty) -- and round 8 (PR9R8) defeated the
// fourth bar in turn ("the candidate's own consumer extraction succeeds"):
// a self-consistent fake moov with a genuinely parseable mvhd, planted
// inside mdat's own payload, cleared every one of those bars while never
// being a genuine top-level box at all.
//
// The PR9R9 coordinator follow-up that closed the round-8 finding closed
// the SEARCH MECHANISM itself, not just the acceptance bar it kept
// re-raising: a substring scan can never distinguish a genuinely rescued
// top-level box from a planted decoy, because neither has proven
// placement -- findMoov now decides every outcome from a genuine top-level
// box-chain walk alone (see findMoov's own doc comment), in BOTH the
// oversized-file branch (headBoxChainProvenEnd) and the
// fits-within-budget branch (the initial firstChildBox walk over the
// whole file). findMoovInTail and isUsableMoovCandidate are deleted.
const maxMoovTailCandidatesScanned = 64

// codecFromTrak descends an ALREADY-SELECTED trakPayload -> mdia -> minf ->
// stbl -> stsd and reads the first sample entry's fourCC -- factored out of
// findDeclaredCodec (moov -> trak -> ...) so that descent has exactly one
// implementation (round 6, T-R6C-01). It previously had a second caller,
// isUsableMoovCandidate's own codec-extraction attempt, run as part of
// validating a tail-anchored substring-search candidate; that caller and
// the whole substring-scan mechanism it belonged to were deleted (PR9R9 --
// see findMoov's own doc comment and maxMoovTailCandidatesScanned's).
func codecFromTrak(trakPayload []byte, tr *Tracker) (codec string, ok bool, ceilingHit bool) {
	mdiaPayload, ok1, c1 := firstChildBox(trakPayload, "mdia", tr)
	if !ok1 {
		return "", false, c1
	}
	minfPayload, ok2, c2 := firstChildBox(mdiaPayload, "minf", tr)
	if !ok2 {
		return "", false, c1 || c2
	}
	stblPayload, ok3, c3 := firstChildBox(minfPayload, "stbl", tr)
	if !ok3 {
		return "", false, c1 || c2 || c3
	}
	stsdPayload, ok4, c4 := firstChildBox(stblPayload, "stsd", tr)
	if !ok4 {
		return "", false, c1 || c2 || c3 || c4
	}
	codec, ok5 := parseSTSDCodec(stsdPayload)
	return codec, ok5, c1 || c2 || c3 || c4
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
// sample entry's fourCC, via the shared codecFromTrak descent (round 6,
// T-R6C-01). ceilingHit is the same signal as findTrackDimensions's own.
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
	codec, ok, c2 := codecFromTrak(trakPayload, tr)
	return codec, ok, c1 || c2
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
		if candidates >= maxTrakCandidatesScanned || tr.OutOfTime() {
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
		if count >= maxISOBMFFBoxes || tr.OutOfTime() {
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
//
// PR9R9 sweep (task 2's own instruction to walk EVERY readWindow-failure
// site in this file, not only the two named findings): this function's own
// readWindow call below had the identical class of defect findMoov's head
// window carried before this same round's fix -- ok==false was reported as
// ReasonWorkCeilingBytes UNCONDITIONALLY, even when windowCoversWholeFile
// was true (winLen sized to cover the file in full, readWindow's own
// ReadAt call genuinely failing). That is not this scan's own budget --
// raising maxBytes would not fix an operational read failure -- the SAME
// distinction wavCharacteristics and findMoov's head-window branch both
// apply. Brought to the one rule those two already establish: a failure on
// a full-coverage window is ReasonReadFailed, a failure on a genuinely
// truncated window stays ReasonWorkCeilingBytes, and a degenerate window
// (winLen<=0: size<=0 or maxBytes<=0, no read even attempted) is
// ReasonMalformedInput -- mirroring wavCharacteristics's own three-way
// switch exactly, so a fourth family never has to rediscover this rule from
// scratch in a later round.
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
		// PR9R9 sweep (see this function's own doc comment): the same
		// three-way split wavCharacteristics already applies to its own
		// readWindow failure, brought here.
		reason := ReasonMalformedInput // degenerate window (winLen<=0): no read was even attempted, not an operational failure
		switch {
		case !windowCoversWholeFile:
			reason = ReasonWorkCeilingBytes
		case winLen > 0:
			// windowCoversWholeFile and winLen>0: this window was sized to
			// cover the file IN FULL, so readWindow's failure here is
			// readWindow's own io.ReaderAt.ReadAt call failing on bytes this
			// scan otherwise intended to examine in full -- never this
			// scan's own ceiling and never a fact about the file.
			reason = ReasonReadFailed
		}
		c["duration_seconds"] = nil
		c["duration_seconds_reason"] = string(reason)
		c["declared_codec"] = nil
		c["declared_codec_reason"] = string(reason)
		return c
	}

	segmentPayload, foundSegment, segCeilingHit := firstEBMLChild(buf, ebmlIDSegment, tr)
	if !foundSegment {
		reason := ebmlMissReason(windowCoversWholeFile, segCeilingHit, tr)
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
		c["duration_seconds_reason"] = string(ebmlMissReason(windowCoversWholeFile, ceilingHit, tr))
	}

	if codec, ok, ceilingHit := findEBMLCodecID(segmentPayload, tr); ok {
		c["declared_codec"] = codec
	} else {
		c["declared_codec"] = nil
		c["declared_codec_reason"] = string(ebmlMissReason(windowCoversWholeFile, ceilingHit, tr))
	}

	return c
}

// ebmlMissReason is ebmlCharacteristics's single verdict rule for a miss:
// OUR OWN ceiling (byte window did not cover the whole file, or an internal
// element walk hit maxEBMLElements) versus a genuine structural fact about
// the file. See ebmlCharacteristics's own doc comment for why these must
// never collapse into one answer.
func ebmlMissReason(windowCoversWholeFile, ceilingHit bool, tr *Tracker) Reason {
	if ceilingHit {
		return tr.ceilingReason()
	}
	if !windowCoversWholeFile {
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
		if count >= maxEBMLElements || tr.OutOfTime() {
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
//
// quick/260812-r6c task 2's own neighbour audit (media.go, the same file
// image.go:165's finding named): the readWindow failure branch below had
// the SAME defect that finding named for image.go, one file over -- when
// windowTruncated is false (this window was sized to cover the file IN
// FULL) but the read itself still failed, this reported ReasonMalformedInput
// -- a claim about the FILE's structure -- for an operational read failure
// that examined zero bytes. ReasonReadFailed (status.go) is the honest
// answer for that one branch; windowTruncated still wins when the window
// itself was too small, unchanged from before.
func wavCharacteristics(r io.ReaderAt, size, maxBytes int64, tr *Tracker) Characteristics {
	c := Characteristics{}

	winLen := maxBytes
	if winLen > size {
		winLen = size
	}
	windowTruncated := winLen < size

	buf, ok := readWindow(r, 0, winLen, size)
	if !ok {
		reason := ReasonMalformedInput // degenerate window (size<=0 or maxBytes<=0): no read was even attempted, not an operational failure
		switch {
		case windowTruncated:
			reason = ReasonWorkCeilingBytes
		case winLen > 0:
			// winLen>0 and NOT windowTruncated: sized to cover the file IN
			// FULL, so this failure is readWindow's own io.ReaderAt.ReadAt
			// call failing on bytes this scan intended to examine in full
			// -- never a fact about the file's structure (quick/260812-r6c
			// task 2).
			reason = ReasonReadFailed
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
		if count >= maxRIFFChunks || tr.OutOfTime() {
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
		reason := Reason(ReasonMalformedInput)
		switch {
		case ceilingHit:
			reason = tr.ceilingReason()
		case windowTruncated:
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
