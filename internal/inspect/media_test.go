package inspect

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"testing"
	"time"
)

// ===== fixture builders: ISOBMFF (mp4/mov) =====

func isobmffBox(boxType string, payload []byte) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint32(b[0:4], uint32(8+len(payload)))
	copy(b[4:8], boxType)
	return append(b, payload...)
}

func corruptBoxSize(box []byte, newSize uint32) []byte {
	out := append([]byte{}, box...)
	binary.BigEndian.PutUint32(out[0:4], newSize)
	return out
}

func mvhdBoxV0(timescale, duration uint32) []byte {
	p := make([]byte, 100) // version 0 mvhd only needs ~24 meaningful bytes; pad generously
	binary.BigEndian.PutUint32(p[12:16], timescale)
	binary.BigEndian.PutUint32(p[16:20], duration)
	return isobmffBox("mvhd", p)
}

func mvhdBoxV1(timescale uint32, duration uint64) []byte {
	p := make([]byte, 120)
	p[0] = 1 // version
	binary.BigEndian.PutUint32(p[20:24], timescale)
	binary.BigEndian.PutUint64(p[24:32], duration)
	return isobmffBox("mvhd", p)
}

func tkhdBoxV0(width, height uint32) []byte {
	p := make([]byte, 84)
	wFixed := width << 16
	hFixed := height << 16
	binary.BigEndian.PutUint32(p[76:80], wFixed)
	binary.BigEndian.PutUint32(p[80:84], hFixed)
	return isobmffBox("tkhd", p)
}

func stsdBox(codec string) []byte {
	p := make([]byte, 16)
	binary.BigEndian.PutUint32(p[4:8], 1) // entry_count = 1
	binary.BigEndian.PutUint32(p[8:12], 16)
	copy(p[12:16], codec)
	return isobmffBox("stsd", p)
}

// hdlrBox builds a minimal mdia -> hdlr FullBox declaring handlerType
// ("vide" or "soun") -- ISO/IEC 14496-12 8.4.3: version+flags(4) +
// pre_defined(4) + handler_type(4) + reserved(12) [+ name, omitted here
// since findTrackByHandler never reads past handler_type].
func hdlrBox(handlerType string) []byte {
	p := make([]byte, 24)
	copy(p[8:12], handlerType)
	return isobmffBox("hdlr", p)
}

// trakBox builds a trak whose mdia declares handlerType via hdlr -- real
// MP4/MOV/M4A muxers always write this box, and findTrackByHandler
// (media.go, PR9R4 finding at media.go:275) now selects a track by it
// rather than by position, so every fixture representing a real track must
// carry one.
func trakBox(tkhd, stsdWrapped []byte, handlerType string) []byte {
	hdlr := hdlrBox(handlerType)
	stbl := isobmffBox("stbl", stsdWrapped)
	minf := isobmffBox("minf", stbl)
	mdiaPayload := append(append([]byte{}, hdlr...), minf...)
	mdia := isobmffBox("mdia", mdiaPayload)
	payload := append(append([]byte{}, tkhd...), mdia...)
	return isobmffBox("trak", payload)
}

func moovBox(mvhd, trak []byte) []byte {
	payload := append(append([]byte{}, mvhd...), trak...)
	return isobmffBox("moov", payload)
}

func minimalFtyp() []byte {
	return isobmffBox("ftyp", []byte("isom\x00\x00\x02\x00isomiso2avc1mp41"))
}

// fastStartMP4Fixture places moov right after ftyp -- the head window
// covers it directly. handlerType is the single track's own mdia -> hdlr
// declaration ("vide" for a real video track, "soun" for an audio-only
// track such as m4a) -- see trakBox's own doc comment.
func fastStartMP4Fixture(timescale, duration, width, height uint32, codec, handlerType string) []byte {
	mvhd := mvhdBoxV0(timescale, duration)
	tkhd := tkhdBoxV0(width, height)
	trak := trakBox(tkhd, stsdBox(codec), handlerType)
	moov := moovBox(mvhd, trak)
	mdat := isobmffBox("mdat", make([]byte, 64))
	buf := append(append([]byte{}, minimalFtyp()...), moov...)
	return append(buf, mdat...)
}

// tailMoovMP4Fixture places a large mdat BEFORE moov, forcing a real
// tail-anchored search -- mirrors what a real, non-faststart encoder
// produces (Assumption A1).
func tailMoovMP4Fixture(timescale, duration, width, height uint32, codec, handlerType string, mdatSize int) []byte {
	mvhd := mvhdBoxV0(timescale, duration)
	tkhd := tkhdBoxV0(width, height)
	trak := trakBox(tkhd, stsdBox(codec), handlerType)
	moov := moovBox(mvhd, trak)
	mdat := isobmffBox("mdat", make([]byte, mdatSize))
	buf := append(append([]byte{}, minimalFtyp()...), mdat...)
	return append(buf, moov...)
}

// ===== fixture builders: EBML (mkv/webm) =====

func ebmlIDBytes(id uint32, length int) []byte {
	b := make([]byte, length)
	v := id
	for i := length - 1; i >= 0; i-- {
		b[i] = byte(v & 0xFF)
		v >>= 8
	}
	return b
}

// ebmlEncodeSize4 always encodes as a 4-byte EBML size vint (marker
// 0001xxxx) -- simpler than choosing a minimal length, and every fixture
// value used in this file fits comfortably within 28 bits.
func ebmlEncodeSize4(v uint32) []byte {
	b := make([]byte, 4)
	b[0] = 0x10 | byte((v>>24)&0x0F)
	b[1] = byte(v >> 16)
	b[2] = byte(v >> 8)
	b[3] = byte(v)
	return b
}

func ebmlElem(idBytes, payload []byte) []byte {
	out := append(append([]byte{}, idBytes...), ebmlEncodeSize4(uint32(len(payload)))...)
	return append(out, payload...)
}

func float64Bytes(v float64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, math.Float64bits(v))
	return b
}

func uintBytes32(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

// mkvFixtureWithPadding builds a minimal mkv-shaped file: EBML header,
// then an optional Void-like filler element BEFORE Info/Tracks (to push
// Duration outside a small window), then Segment{Info{TimestampScale,
// Duration}, Tracks{TrackEntry{CodecID}}}.
func mkvFixtureWithPadding(paddingLen int) []byte {
	header := ebmlElem(ebmlIDBytes(0x1A45DFA3, 4), []byte{})
	var filler []byte
	if paddingLen > 0 {
		filler = ebmlElem(ebmlIDBytes(0xEC, 1), make([]byte, paddingLen))
	}
	tsElem := ebmlElem(ebmlIDBytes(0x2AD7B1, 3), uintBytes32(1000000))
	durElem := ebmlElem(ebmlIDBytes(0x4489, 2), float64Bytes(12.5))
	infoPayload := append(append([]byte{}, tsElem...), durElem...)
	infoElem := ebmlElem(ebmlIDBytes(0x1549A966, 4), infoPayload)

	codecElem := ebmlElem(ebmlIDBytes(0x86, 1), []byte("V_MPEG4/ISO/AVC"))
	trackEntryElem := ebmlElem(ebmlIDBytes(0xAE, 1), codecElem)
	tracksElem := ebmlElem(ebmlIDBytes(0x1654AE6B, 4), trackEntryElem)

	segmentPayload := append(append([]byte{}, filler...), infoElem...)
	segmentPayload = append(segmentPayload, tracksElem...)
	segmentElem := ebmlElem(ebmlIDBytes(0x18538067, 4), segmentPayload)

	return append(header, segmentElem...)
}

func minimalMKVFixture() []byte {
	return mkvFixtureWithPadding(0)
}

// ===== fixture builders: RIFF/WAV =====

func minimalWAVFixture(sampleRate, channels, bitsPerSample, dataSize uint32) []byte {
	byteRate := sampleRate * channels * bitsPerSample / 8
	blockAlign := channels * bitsPerSample / 8

	fmtChunk := make([]byte, 16)
	binary.LittleEndian.PutUint16(fmtChunk[0:2], 1) // PCM
	binary.LittleEndian.PutUint16(fmtChunk[2:4], uint16(channels))
	binary.LittleEndian.PutUint32(fmtChunk[4:8], sampleRate)
	binary.LittleEndian.PutUint32(fmtChunk[8:12], byteRate)
	binary.LittleEndian.PutUint16(fmtChunk[12:14], uint16(blockAlign))
	binary.LittleEndian.PutUint16(fmtChunk[14:16], uint16(bitsPerSample))

	buf := []byte("RIFF")
	buf = append(buf, 0, 0, 0, 0) // RIFF size, unused by the analyzer
	buf = append(buf, []byte("WAVE")...)
	buf = append(buf, []byte("fmt ")...)
	fmtLen := make([]byte, 4)
	binary.LittleEndian.PutUint32(fmtLen, uint32(len(fmtChunk)))
	buf = append(buf, fmtLen...)
	buf = append(buf, fmtChunk...)
	buf = append(buf, []byte("data")...)
	dataLen := make([]byte, 4)
	binary.LittleEndian.PutUint32(dataLen, dataSize)
	buf = append(buf, dataLen...)
	buf = append(buf, make([]byte, dataSize)...)
	return buf
}

// ===== counting io.ReaderAt wrapper (proves reads stay bounded) =====

type countingReaderAt struct {
	data           []byte
	totalRequested int64
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	c.totalRequested += int64(len(p))
	if off < 0 || off >= int64(len(c.data)) {
		return 0, io.EOF
	}
	n := copy(p, c.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// ===== Task 1 behavior tests =====

func TestAnalyzeMediaFindsMoovAtHead(t *testing.T) {
	data := fastStartMP4Fixture(1000, 5000, 640, 480, "avc1", isobmffHandlerVideo)
	res := AnalyzeMedia("mp4", bytes.NewReader(data), int64(len(data)), int64(len(data)), nil)
	dur, ok := res.Characteristics["duration_seconds"].(float64)
	if !ok {
		t.Fatalf("duration_seconds missing or wrong type: %#v (reason=%v)", res.Characteristics["duration_seconds"], res.Characteristics["duration_seconds_reason"])
	}
	if dur != 5.0 {
		t.Fatalf("duration_seconds = %v, want 5.0", dur)
	}
	codec, _ := res.Characteristics["declared_codec"].(string)
	if codec != "avc1" {
		t.Fatalf("declared_codec = %q, want %q", codec, "avc1")
	}
}

func TestAnalyzeMediaFindsMoovAtTail(t *testing.T) {
	// mdat is deliberately large enough that a small head window cannot
	// possibly reach moov; the tail window (clamped to maxBytes) must.
	data := tailMoovMP4Fixture(1000, 5000, 640, 480, "avc1", isobmffHandlerVideo, 9000)
	const maxBytes = 2048
	res := AnalyzeMedia("mp4", bytes.NewReader(data), int64(len(data)), maxBytes, nil)
	dur, ok := res.Characteristics["duration_seconds"].(float64)
	if !ok {
		t.Fatalf("duration_seconds missing or wrong type: %#v (reason=%v)", res.Characteristics["duration_seconds"], res.Characteristics["duration_seconds_reason"])
	}
	if dur != 5.0 {
		t.Fatalf("duration_seconds = %v, want 5.0", dur)
	}
}

func TestAnalyzeMediaMVHDVersion1SixtyFourBit(t *testing.T) {
	mvhd := mvhdBoxV1(90000, 1080000) // 12 seconds
	tkhd := tkhdBoxV0(1920, 1080)
	trak := trakBox(tkhd, stsdBox("hvc1"), isobmffHandlerVideo)
	moov := moovBox(mvhd, trak)
	data := append(append([]byte{}, minimalFtyp()...), moov...)

	res := AnalyzeMedia("mp4", bytes.NewReader(data), int64(len(data)), int64(len(data)), nil)
	dur, ok := res.Characteristics["duration_seconds"].(float64)
	if !ok || dur != 12.0 {
		t.Fatalf("duration_seconds = %#v, want 12.0", res.Characteristics["duration_seconds"])
	}
}

func TestAnalyzeMediaTkhdDimensionsAreFixedPointDecoded(t *testing.T) {
	data := fastStartMP4Fixture(1000, 5000, 640, 480, "avc1", isobmffHandlerVideo)
	res := AnalyzeMedia("mp4", bytes.NewReader(data), int64(len(data)), int64(len(data)), nil)
	w, ok := res.Characteristics["width"].(uint32)
	if !ok {
		t.Fatalf("width missing or wrong type: %#v", res.Characteristics["width"])
	}
	if w != 640 {
		t.Fatalf("width = %d, want 640 (NOT %d -- 16.16 fixed-point must be decoded, not read as a plain integer)", w, uint32(640)<<16)
	}
	h, ok := res.Characteristics["height"].(uint32)
	if !ok || h != 480 {
		t.Fatalf("height = %#v, want 480", res.Characteristics["height"])
	}
}

func TestAnalyzeMediaMP4OversizedBoxDoesNotPanicOrOverAllocate(t *testing.T) {
	mvhdCorrupt := corruptBoxSize(mvhdBoxV0(1000, 5000), 0x7FFFFFFF)
	trak := trakBox(tkhdBoxV0(640, 480), stsdBox("avc1"), isobmffHandlerVideo)
	moov := moovBox(mvhdCorrupt, trak)
	data := append(append([]byte{}, minimalFtyp()...), moov...)

	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("AnalyzeMedia panicked: %v", p)
		}
	}()
	res := AnalyzeMedia("mp4", bytes.NewReader(data), int64(len(data)), int64(len(data)), nil)
	if res.Characteristics["duration_seconds"] != nil {
		t.Fatalf("duration_seconds = %#v, want nil", res.Characteristics["duration_seconds"])
	}
	reason, _ := res.Characteristics["duration_seconds_reason"].(string)
	if reason != string(ReasonMalformedInput) {
		t.Fatalf("duration_seconds_reason = %q, want %q", reason, ReasonMalformedInput)
	}
}

func TestAnalyzeMediaMoovNotFoundWithinCoverageIsMalformed(t *testing.T) {
	// Head+tail windows fully cover this tiny file; moov is genuinely
	// absent, so the reason must be malformed_input, not a resource
	// ceiling that was never actually hit.
	data := append(append([]byte{}, minimalFtyp()...), isobmffBox("mdat", make([]byte, 16))...)
	res := AnalyzeMedia("mp4", bytes.NewReader(data), int64(len(data)), int64(len(data)), nil)
	reason, _ := res.Characteristics["duration_seconds_reason"].(string)
	if reason != string(ReasonMalformedInput) {
		t.Fatalf("duration_seconds_reason = %q, want %q (windows covered the whole file)", reason, ReasonMalformedInput)
	}
}

// TestAnalyzeMediaMP4HeadReadFailureWithFullWindowIsReadFailedNotWorkCeiling
// is PR9R9's own finding, verbatim (media.go:382): "For an MP4/MOV/M4A
// whose backing multipart file returns a non-EOF error during a full head
// read (size <= maxBytes), readWindow returns headOK=false, but this
// branch reports work_ceiling_bytes. No byte or iteration ceiling caused
// that failure -- the service could not read the upload -- and the same
// repository already maps this scenario to ReasonReadFailed for WAV and
// image analysis." readFailureReaderAt (image_test.go) always fails with a
// non-EOF error, independent of size -- maxBytes == size here selects
// findMoov's own !oversizedFile branch, whose head window is, by
// construction, always sized to cover the file in full (see findMoov's own
// doc comment), isolating this to the read-failure path, never the
// iteration-ceiling one.
func TestAnalyzeMediaMP4HeadReadFailureWithFullWindowIsReadFailedNotWorkCeiling(t *testing.T) {
	r := &readFailureReaderAt{err: errSimulatedDiskFailure}
	const size = 1024
	res := AnalyzeMedia("mp4", r, size, size, nil) // maxBytes == size: !oversizedFile

	for _, key := range []string{"duration_seconds", "width", "height", "declared_codec"} {
		if res.Characteristics[key] != nil {
			t.Errorf("%s = %#v, want nil -- readFailureReaderAt never returns real bytes", key, res.Characteristics[key])
		}
		reason, _ := res.Characteristics[key+"_reason"].(string)
		if reason != string(ReasonReadFailed) {
			t.Errorf("%s_reason = %q, want %q -- a read failure with a full-coverage head window (size<=maxBytes) is neither a fact about the file's structure (malformed_input) nor this scan's own byte ceiling (work_ceiling_bytes); the SAME rule wavCharacteristics and scanImageMetadata already apply to their own identically-shaped branch (PR9R9 finding, media.go:382)", key, reason, ReasonReadFailed)
		}
	}
}

func TestAnalyzeMediaMoovNotFoundBeyondCoverageIsWorkCeiling(t *testing.T) {
	filler := make([]byte, 20000)
	data := append(append([]byte{}, minimalFtyp()...), filler...)
	const maxBytes = 512 // far smaller than len(filler): head+tail cannot cover the middle
	res := AnalyzeMedia("mp4", bytes.NewReader(data), int64(len(data)), maxBytes, nil)
	reason, _ := res.Characteristics["duration_seconds_reason"].(string)
	if reason != string(ReasonWorkCeilingBytes) {
		t.Fatalf("duration_seconds_reason = %q, want %q (windows did not cover the whole file)", reason, ReasonWorkCeilingBytes)
	}
}

// TestAnalyzeMediaISOBMFFChildBoxIterationCeilingIsWorkCeilingBytes is
// media.go's own audit finding (round 2 plan's explicit instruction to
// check every scanner, not only the codes the reviewer named): a moov box
// containing more than maxISOBMFFBoxes sibling boxes BEFORE mvhd/trak
// exhausts firstChildBox's own iteration ceiling -- OUR resource limit, not
// a structural fact about the file -- and must report
// unknown/work_ceiling_bytes for duration/width/height/declared_codec,
// never malformed_input (which never marks Coverage partial).
func TestAnalyzeMediaISOBMFFChildBoxIterationCeilingIsWorkCeilingBytes(t *testing.T) {
	var fillers []byte
	for i := 0; i < maxISOBMFFBoxes+10; i++ {
		fillers = append(fillers, isobmffBox("free", []byte{})...)
	}
	mvhd := mvhdBoxV0(1000, 5000)
	trak := trakBox(tkhdBoxV0(640, 480), stsdBox("avc1"), isobmffHandlerVideo)
	moovPayload := append(append(append([]byte{}, fillers...), mvhd...), trak...)
	moov := isobmffBox("moov", moovPayload)
	mdat := isobmffBox("mdat", make([]byte, 64))
	data := append(append(append([]byte{}, minimalFtyp()...), moov...), mdat...)

	res := AnalyzeMedia("mp4", bytes.NewReader(data), int64(len(data)), int64(len(data)), nil)

	if res.Characteristics["duration_seconds"] != nil {
		t.Fatalf("duration_seconds = %#v, want nil (mvhd sits past firstChildBox's own iteration ceiling)", res.Characteristics["duration_seconds"])
	}
	if reason, _ := res.Characteristics["duration_seconds_reason"].(string); reason != string(ReasonWorkCeilingBytes) {
		t.Fatalf("duration_seconds_reason = %q, want %q -- OUR iteration ceiling, not the file's structure, decided this miss", reason, ReasonWorkCeilingBytes)
	}
	if reason, _ := res.Characteristics["width_reason"].(string); reason != string(ReasonWorkCeilingBytes) {
		t.Fatalf("width_reason = %q, want %q", reason, ReasonWorkCeilingBytes)
	}
	if reason, _ := res.Characteristics["declared_codec_reason"].(string); reason != string(ReasonWorkCeilingBytes) {
		t.Fatalf("declared_codec_reason = %q, want %q", reason, ReasonWorkCeilingBytes)
	}
}

// ===== quick/260811-r4b Task 2: candidate-walk fixes (media.go:239, :275) =====

// TestAnalyzeMediaVideoDimensionsFoundWhenAudioTrackPrecedesVideoTrack is
// the PR9R4 finding at media.go:275, verbatim: "Valid MP4/MOV files may
// place an audio trak before the video trak. This lookup always selects the
// first track, successfully parses its audio tkhd, and reports width and
// height as zero while ignoring the later video track's actual
// dimensions." The audio trak's own tkhd is deliberately 0x0 (conventional
// for an audio-only track) -- if the fix regressed to "the first trak",
// this test would observe width=height=0, not an error.
func TestAnalyzeMediaVideoDimensionsFoundWhenAudioTrackPrecedesVideoTrack(t *testing.T) {
	mvhd := mvhdBoxV0(1000, 5000)
	audioTrak := trakBox(tkhdBoxV0(0, 0), stsdBox("mp4a"), isobmffHandlerAudio)
	videoTrak := trakBox(tkhdBoxV0(640, 480), stsdBox("avc1"), isobmffHandlerVideo)
	moovPayload := append(append(append([]byte{}, mvhd...), audioTrak...), videoTrak...)
	moov := isobmffBox("moov", moovPayload)
	data := append(append([]byte{}, minimalFtyp()...), moov...)
	data = append(data, isobmffBox("mdat", make([]byte, 16))...)

	res := AnalyzeMedia("mp4", bytes.NewReader(data), int64(len(data)), int64(len(data)), nil)

	w, ok := res.Characteristics["width"].(uint32)
	if !ok || w != 640 {
		t.Fatalf("width = %#v, want 640 -- the VIDEO track's real dimensions, not the preceding audio track's 0x0", res.Characteristics["width"])
	}
	h, ok := res.Characteristics["height"].(uint32)
	if !ok || h != 480 {
		t.Fatalf("height = %#v, want 480", res.Characteristics["height"])
	}
	codec, ok := res.Characteristics["declared_codec"].(string)
	if !ok || codec != "avc1" {
		t.Fatalf("declared_codec = %#v, want %q -- the video track's own codec, not the preceding audio track's %q", res.Characteristics["declared_codec"], "avc1", "mp4a")
	}
}

// TestAnalyzeMediaNoVideoTrackYieldsUnknownDimensionsNotZero is the plan's
// own explicit requirement: "отсутствие видеодорожки после ограниченного
// обхода -- unknown, не ноль". A moov with only an audio trak (no track
// anywhere declares handler_type="vide") must report width/height as
// nil/malformed_input, never silently fall back to the audio track's own
// (zero) tkhd dimensions.
func TestAnalyzeMediaNoVideoTrackYieldsUnknownDimensionsNotZero(t *testing.T) {
	mvhd := mvhdBoxV0(1000, 5000)
	audioTrak := trakBox(tkhdBoxV0(0, 0), stsdBox("mp4a"), isobmffHandlerAudio)
	moov := moovBox(mvhd, audioTrak)
	data := append(append([]byte{}, minimalFtyp()...), moov...)
	data = append(data, isobmffBox("mdat", make([]byte, 16))...)

	res := AnalyzeMedia("mp4", bytes.NewReader(data), int64(len(data)), int64(len(data)), nil)

	if res.Characteristics["width"] != nil {
		t.Fatalf("width = %#v, want nil -- no track declares handler_type=vide; must never report 0 as if that were a real measurement", res.Characteristics["width"])
	}
	if reason, _ := res.Characteristics["width_reason"].(string); reason != string(ReasonMalformedInput) {
		t.Fatalf("width_reason = %q, want %q -- the full moov was in hand and walked to completion; a genuinely missing video track is a structural fact, not a resource ceiling", reason, ReasonMalformedInput)
	}
	if res.Characteristics["height"] != nil {
		t.Fatalf("height = %#v, want nil", res.Characteristics["height"])
	}
}

// TestAnalyzeMediaFindsMoovDespiteEarlierIncidentalOccurrenceInPayload is
// the PR9R4 finding at media.go:239, verbatim: "When the tail window
// contains the bytes `moov` inside media payload before the real `moov`
// box, bytes.Index selects that incidental first occurrence. If its
// preceding four bytes do not form a valid box size, the single validation
// attempt fails and the later real box is never considered, so a valid
// file loses all MP4 characteristics." maxBytes is deliberately smaller
// than mdat's own declared box size, so the head window cannot skip past
// mdat as a single top-level box and the search genuinely must fall
// through to the tail-anchored substring search this test exercises.
func TestAnalyzeMediaFindsMoovDespiteEarlierIncidentalOccurrenceInPayload(t *testing.T) {
	fakeOccurrence := append(bytes.Repeat([]byte{'X'}, 4), []byte("moov")...) // preceding 4 bytes ('X','X','X','X') do not form a plausible box-size field
	mdatPayload := append(make([]byte, 6000), fakeOccurrence...)              // the incidental occurrence sits near mdat's END, i.e. inside the tail window
	mdat := isobmffBox("mdat", mdatPayload)

	mvhd := mvhdBoxV0(1000, 5000)
	trak := trakBox(tkhdBoxV0(640, 480), stsdBox("avc1"), isobmffHandlerVideo)
	moov := moovBox(mvhd, trak)

	data := append(append(append([]byte{}, minimalFtyp()...), mdat...), moov...)

	const maxBytes = 4096 // smaller than mdat's own ~6012-byte declared size (head cannot skip it as one box), comfortably larger than fakeOccurrence+moov (both fall inside the tail window)
	res := AnalyzeMedia("mp4", bytes.NewReader(data), int64(len(data)), maxBytes, nil)

	dur, ok := res.Characteristics["duration_seconds"].(float64)
	if !ok || dur != 5.0 {
		t.Fatalf("duration_seconds = %#v, want 5.0 -- an incidental earlier \"moov\" byte sequence inside mdat's own payload must not stop the search from reaching the REAL moov box that follows it", res.Characteristics["duration_seconds"])
	}
	w, ok := res.Characteristics["width"].(uint32)
	if !ok || w != 640 {
		t.Fatalf("width = %#v, want 640", res.Characteristics["width"])
	}
}

// TestAnalyzeMediaSkipsIncidentalEmptyMoovBoxForRealOne is the PR9R5 finding
// at media.go:321, verbatim: "If media payload contains \x00\x00\x00\x08moov
// before the real tail moov, this local walk accepts the incidental bytes
// as a valid empty box and immediately returns its empty payload. The
// analyzer then reports malformed characteristics and never examines the
// genuine later box, even though the MP4 itself is valid." Unlike the round
// 4 fixture above (whose fake occurrence's preceding 4 bytes do NOT form a
// valid box size), fakeEmptyMoov here IS a fully self-consistent, valid
// moov box header (size=8, type="moov", zero-length payload) -- exactly the
// literal byte sequence the finding names -- so round 4's own "parses as a
// box" criterion alone is not enough to reject it.
func TestAnalyzeMediaSkipsIncidentalEmptyMoovBoxForRealOne(t *testing.T) {
	fakeEmptyMoov := isobmffBox("moov", []byte{}) // self-consistent, VALID, but EMPTY moov box header
	mdatPayload := append(make([]byte, 6000), fakeEmptyMoov...)
	mdat := isobmffBox("mdat", mdatPayload)

	mvhd := mvhdBoxV0(1000, 5000)
	trak := trakBox(tkhdBoxV0(640, 480), stsdBox("avc1"), isobmffHandlerVideo)
	moov := moovBox(mvhd, trak)

	data := append(append(append([]byte{}, minimalFtyp()...), mdat...), moov...)

	const maxBytes = 4096 // smaller than mdat's own declared size (head cannot skip it as one box), same shape as the round 4 fixture above
	res := AnalyzeMedia("mp4", bytes.NewReader(data), int64(len(data)), maxBytes, nil)

	dur, ok := res.Characteristics["duration_seconds"].(float64)
	if !ok || dur != 5.0 {
		t.Fatalf("duration_seconds = %#v, want 5.0 -- an incidental, self-consistent but EMPTY \"moov\" box (valid header, no mvhd/trak) must not be accepted as the real moov just because it PARSES; the real moov that follows must still be found", res.Characteristics["duration_seconds"])
	}
	w, ok := res.Characteristics["width"].(uint32)
	if !ok || w != 640 {
		t.Fatalf("width = %#v, want 640", res.Characteristics["width"])
	}
}

// TestAnalyzeMediaRejectsEmptyMvhdTrakDecoyForRealMoov is the PR9R6 finding
// at media.go:382, verbatim: "Fresh evidence after the prior `moov` comment:
// a tail payload containing a locally valid 24-byte decoy (`moov` with empty
// 8-byte `mvhd` and `trak` children) now passes this usability check, so
// `findMoovInTail` still selects it and ignores the genuine later `moov`;
// downstream parsing then reports malformed or missing characteristics for
// an otherwise valid MP4/MOV. Validate that `mvhd` parses and that a
// suitable track has the structure consumed downstream before accepting the
// candidate." Unlike the round 5 fixture above (an empty moov with NO
// children at all), this decoy's mvhd AND trak children are each PRESENT
// (round 5's own bar, cleared) but each carries a ZERO-byte payload:
// parseMVHD cannot decode a duration out of an empty mvhd, and
// findTrackByHandler cannot find mdia/hdlr inside an empty trak -- so round
// 6's consumer-collapsed criterion (isUsableMoovCandidate) must reject this
// candidate on that ground and keep searching for the genuine moov that
// follows, exactly as it already does for round 4 and round 5's own decoys.
func TestAnalyzeMediaRejectsEmptyMvhdTrakDecoyForRealMoov(t *testing.T) {
	decoyMoov := moovBox(isobmffBox("mvhd", []byte{}), isobmffBox("trak", []byte{}))
	if len(decoyMoov) != 24 {
		t.Fatalf("decoy fixture is %d bytes, want 24 -- fixture no longer matches the finding's own worked example", len(decoyMoov))
	}
	mdatPayload := append(make([]byte, 6000), decoyMoov...)
	mdat := isobmffBox("mdat", mdatPayload)

	mvhd := mvhdBoxV0(1000, 5000)
	trak := trakBox(tkhdBoxV0(640, 480), stsdBox("avc1"), isobmffHandlerVideo)
	moov := moovBox(mvhd, trak)

	data := append(append(append([]byte{}, minimalFtyp()...), mdat...), moov...)

	const maxBytes = 4096 // smaller than mdat's own declared size (head cannot skip it as one box), same shape as the round 4/5 fixtures above
	res := AnalyzeMedia("mp4", bytes.NewReader(data), int64(len(data)), maxBytes, nil)

	dur, ok := res.Characteristics["duration_seconds"].(float64)
	if !ok || dur != 5.0 {
		t.Fatalf("duration_seconds = %#v, want 5.0 -- a 24-byte decoy moov with EMPTY mvhd and trak children (present, but carrying zero real fields) must not be accepted just because both children exist; the real moov that follows must still be found", res.Characteristics["duration_seconds"])
	}
	w, ok := res.Characteristics["width"].(uint32)
	if !ok || w != 640 {
		t.Fatalf("width = %#v, want 640", res.Characteristics["width"])
	}
	codec, ok := res.Characteristics["declared_codec"].(string)
	if !ok || codec != "avc1" {
		t.Fatalf("declared_codec = %#v, want %q", res.Characteristics["declared_codec"], "avc1")
	}
}

// TestAnalyzeMediaRejectsFakeMoovPlantedInsideMdatPayloadForGenuineTailMoov
// is the PR9R8 finding at media.go:498, verbatim: "an mdat payload can
// contain a self-consistent fake moov with only a parseable 20-byte mvhd
// carrying attacker-chosen timescale/duration fields before the genuine
// tail moov. This branch accepts it immediately, so the analyzer publishes
// the fake duration, reports the missing dimensions/codec as malformed, and
// never examines the real metadata. Successful consumer parsing does not
// establish that bytes found by substring search are a top-level box;
// validate the candidate's placement in the file's top-level box
// sequence." Unlike every earlier decoy in this file (round 4: doesn't
// parse as a box; round 5: parses but empty; round 6: children present but
// empty), this one clears round 6's own consumer-collapsed bar cleanly --
// its mvhd is real, minimal, and genuinely decodes via parseMVHD. Only a
// placement proof, not a content check, can reject it.
func TestAnalyzeMediaRejectsFakeMoovPlantedInsideMdatPayloadForGenuineTailMoov(t *testing.T) {
	fakeMvhdPayload := make([]byte, 20)                     // the finding's own "20-byte mvhd"
	binary.BigEndian.PutUint32(fakeMvhdPayload[12:16], 1)   // attacker-chosen timescale
	binary.BigEndian.PutUint32(fakeMvhdPayload[16:20], 999) // attacker-chosen duration -> 999.0s if ever published
	fakeMvhd := isobmffBox("mvhd", fakeMvhdPayload)
	if _, ok, _ := parseMVHD(fakeMvhdPayload); !ok {
		t.Fatal("fixture bug: the fake mvhd itself must genuinely parse via parseMVHD, or this fixture no longer matches the finding's own worked example")
	}
	fakeMoov := isobmffBox("moov", fakeMvhd) // self-consistent, and its mvhd genuinely decodes -- clears every prior round's own bar

	mdatPayload := append(make([]byte, 6000), fakeMoov...)
	mdat := isobmffBox("mdat", mdatPayload)

	mvhd := mvhdBoxV0(1000, 5000) // genuine duration = 5.0s
	trak := trakBox(tkhdBoxV0(640, 480), stsdBox("avc1"), isobmffHandlerVideo)
	moov := moovBox(mvhd, trak)

	data := append(append(append([]byte{}, minimalFtyp()...), mdat...), moov...)

	const maxBytes = 4096 // smaller than mdat's own declared size (head cannot skip it as one box), same shape as the round 4/5/6 fixtures above
	res := AnalyzeMedia("mp4", bytes.NewReader(data), int64(len(data)), maxBytes, nil)

	dur, ok := res.Characteristics["duration_seconds"].(float64)
	if !ok || dur != 5.0 {
		t.Fatalf("duration_seconds = %#v, want 5.0 -- a fake moov PLANTED INSIDE mdat's own declared payload, whose mvhd genuinely parses with attacker-chosen fields, must never be accepted just because it successfully extracts a value; only the candidate whose PLACEMENT in the real top-level box chain is proven may be accepted, and that is the genuine moov that follows mdat's own declared end", res.Characteristics["duration_seconds"])
	}
	w, ok := res.Characteristics["width"].(uint32)
	if !ok || w != 640 {
		t.Fatalf("width = %#v, want 640 -- the fake moov's own empty structure (no trak) must not have been accepted in its place", res.Characteristics["width"])
	}
}

// ===== PR9R9: headBoxChainProvenEnd's own fallback paths, previously
// untested and previously falling through to the exact substring scan the
// finding is about (coordinator review of the round-8 fix) =====

// fakeMoovWith999sDuration builds a self-consistent "moov" box wrapping a
// genuinely-parseable 20-byte mvhd carrying attacker-chosen
// timescale=1/duration=999 -- the same worked example as
// TestAnalyzeMediaRejectsFakeMoovPlantedInsideMdatPayloadForGenuineTailMoov,
// factored out so every "does the fallback still let this decoy's fake
// 999.0s through" test below builds the identical decoy.
func fakeMoovWith999sDuration() []byte {
	fakeMvhdPayload := make([]byte, 20)
	binary.BigEndian.PutUint32(fakeMvhdPayload[12:16], 1)   // attacker-chosen timescale
	binary.BigEndian.PutUint32(fakeMvhdPayload[16:20], 999) // attacker-chosen duration -> 999.0s if ever published
	return isobmffBox("moov", isobmffBox("mvhd", fakeMvhdPayload))
}

// TestAnalyzeMediaProvenPlacementSizeZeroDoesNotFallBackToDecoy is the
// coordinator's first named fallback shape: the top-level box right after
// ftyp declares size==0 ("extends to EOF" -- ISOBMFF only permits this for
// a file's FINAL box, never mid-head). headBoxChainProvenEnd refuses to
// treat this as a valid jump point (case 0 in its own switch) and reports
// !ok. Before this fix, !ok fell through to findMoovInTail's substring
// scan, which would find and accept the decoy planted right after this
// box exactly as it did before round 8's own fix.
func TestAnalyzeMediaProvenPlacementSizeZeroDoesNotFallBackToDecoy(t *testing.T) {
	sizeZeroHeader := make([]byte, 8) // size bytes stay zero
	copy(sizeZeroHeader[4:8], "mdat")

	// The decoy is placed near the file's own END (behind 6000 bytes of
	// filler), inside where the TAIL window would land -- not immediately
	// after the malformed header -- so an inversion that restores the old
	// "fall through to findMoovInTail" behavior would actually be ABLE to
	// reach and accept it (a decoy sitting only in the HEAD portion would
	// make the inversion inconclusive: it would stay unreachable by the
	// substring scan regardless of whether the fallback ran at all).
	decoy := fakeMoovWith999sDuration()
	data := append(append([]byte{}, minimalFtyp()...), sizeZeroHeader...)
	data = append(data, make([]byte, 6000)...)
	data = append(data, decoy...)

	const maxBytes = 4096
	if int64(len(data)) <= maxBytes {
		t.Fatal("fixture bug: this test requires size > maxBytes (oversizedFile) to exercise the proof path at all")
	}
	res := AnalyzeMedia("mp4", bytes.NewReader(data), int64(len(data)), maxBytes, nil)

	if res.Characteristics["duration_seconds"] != nil {
		t.Fatalf("duration_seconds = %#v, want nil -- a box declaring size=0 mid-head must never let the fallback scan through to the decoy's 999.0s", res.Characteristics["duration_seconds"])
	}
	if reason, _ := res.Characteristics["duration_seconds_reason"].(string); reason != string(ReasonWorkCeilingBytes) {
		t.Fatalf("duration_seconds_reason = %q, want %q -- placement could not be proven; that is an honest unknown, not grounds to scan for the decoy", reason, ReasonWorkCeilingBytes)
	}
}

// TestAnalyzeMediaProvenPlacementMalformedSizeDoesNotFallBackToDecoy is the
// coordinator's second named fallback shape: the box right after ftyp
// declares a size SMALLER than its own 8-byte header -- structurally
// malformed, and headBoxChainProvenEnd refuses to trust it as a jump
// point.
func TestAnalyzeMediaProvenPlacementMalformedSizeDoesNotFallBackToDecoy(t *testing.T) {
	malformedHeader := make([]byte, 8)
	binary.BigEndian.PutUint32(malformedHeader[0:4], 4) // declared size(4) < header length(8): malformed
	copy(malformedHeader[4:8], "mdat")

	// See TestAnalyzeMediaProvenPlacementSizeZeroDoesNotFallBackToDecoy's own
	// comment for why the decoy sits behind filler, near the file's end,
	// within where the tail window would land.
	decoy := fakeMoovWith999sDuration()
	data := append(append([]byte{}, minimalFtyp()...), malformedHeader...)
	data = append(data, make([]byte, 6000)...)
	data = append(data, decoy...)

	const maxBytes = 4096
	if int64(len(data)) <= maxBytes {
		t.Fatal("fixture bug: this test requires size > maxBytes (oversizedFile) to exercise the proof path at all")
	}
	res := AnalyzeMedia("mp4", bytes.NewReader(data), int64(len(data)), maxBytes, nil)

	if res.Characteristics["duration_seconds"] != nil {
		t.Fatalf("duration_seconds = %#v, want nil -- a box whose declared size is smaller than its own header must never let the fallback scan through to the decoy's 999.0s", res.Characteristics["duration_seconds"])
	}
	if reason, _ := res.Characteristics["duration_seconds_reason"].(string); reason != string(ReasonWorkCeilingBytes) {
		t.Fatalf("duration_seconds_reason = %q, want %q", reason, ReasonWorkCeilingBytes)
	}
}

// TestAnalyzeMediaProvenPlacementTruncated64BitSizeDoesNotFallBackToDecoy is
// the coordinator's third named fallback shape: a box declares the 64-bit
// size marker (size64==1) right at the head window's own edge, so its
// TYPE is fully buffered but the 8-byte real size field that would follow
// is not. headBoxChainProvenEnd refuses to trust a jump point it cannot
// fully read the size of.
func TestAnalyzeMediaProvenPlacementTruncated64BitSizeDoesNotFallBackToDecoy(t *testing.T) {
	const maxBytes = 4096
	tailReserve := int64(maxBytes) / 4
	headLen := int64(maxBytes) - tailReserve // mirrors findMoov's own reservation arithmetic exactly

	ftyp := minimalFtyp()
	marker := make([]byte, 8) // size64 marker (==1) + type; its own real 8-byte size field is what gets truncated
	binary.BigEndian.PutUint32(marker[0:4], 1)
	copy(marker[4:8], "mdat")

	// Pad with one real, fully-buffered "free" box so `marker` starts
	// exactly `headLen` bytes from file offset 0 -- i.e. its own type is
	// the LAST 4 bytes headBoxChainProvenEnd's loop condition (i+8<=len)
	// still admits, and the 8-byte real size field that should follow it
	// is entirely unbuffered.
	padPayloadLen := int(headLen) - len(ftyp) - 8 - 8 // -8 for the pad box's own header, -8 for marker
	if padPayloadLen < 0 {
		t.Fatalf("fixture bug: headLen=%d too small for this layout", headLen)
	}
	pad := isobmffBox("free", make([]byte, padPayloadLen))

	// See TestAnalyzeMediaProvenPlacementSizeZeroDoesNotFallBackToDecoy's own
	// comment for why the decoy sits behind filler, near the file's end,
	// within where the tail window would land, rather than immediately
	// after `marker`.
	decoy := fakeMoovWith999sDuration()
	data := append(append([]byte{}, ftyp...), pad...)
	if int64(len(data)+len(marker)) != headLen {
		t.Fatalf("fixture bug: marker would start at %d, want exactly headLen=%d", len(data), headLen)
	}
	data = append(data, marker...)
	data = append(data, make([]byte, 6000)...)
	data = append(data, decoy...)

	if int64(len(data)) <= maxBytes {
		t.Fatal("fixture bug: this test requires size > maxBytes (oversizedFile) to exercise the proof path at all")
	}
	res := AnalyzeMedia("mp4", bytes.NewReader(data), int64(len(data)), maxBytes, nil)

	if res.Characteristics["duration_seconds"] != nil {
		t.Fatalf("duration_seconds = %#v, want nil -- a 64-bit size field truncated at the head window's own edge must never let the fallback scan through to the decoy's 999.0s", res.Characteristics["duration_seconds"])
	}
	if reason, _ := res.Characteristics["duration_seconds_reason"].(string); reason != string(ReasonWorkCeilingBytes) {
		t.Fatalf("duration_seconds_reason = %q, want %q", reason, ReasonWorkCeilingBytes)
	}
}

// TestAnalyzeMediaProvenPositionNotMoovAndNothingFollowsIsMalformed is the
// coordinator's fourth named case: the proven position IS reachable, but
// the box actually there is not "moov", and nothing else follows it
// either. The comment this round-8 fix originally carried ("fall through
// to the substring scan... rather than concluding absent here") treated
// this as a reason to keep scanning; it no longer does. tailWin's own end
// is the file's true end, so walking cleanly from the proven position to
// there without finding "moov" and without hitting a ceiling is COMPLETE
// proof moov does not exist -- malformed_input, not a scan for a decoy
// that (in this fixture) does not even exist.
func TestAnalyzeMediaProvenPositionNotMoovAndNothingFollowsIsMalformed(t *testing.T) {
	const mdatSize = 6000
	mdat := isobmffBox("mdat", make([]byte, mdatSize))
	trailer := isobmffBox("free", []byte{}) // a REAL top-level box, but never "moov" -- and nothing follows it
	data := append(append([]byte{}, minimalFtyp()...), mdat...)
	data = append(data, trailer...)

	const maxBytes = 4096
	if int64(len(data)) <= maxBytes {
		t.Fatal("fixture bug: this test requires size > maxBytes (oversizedFile) to exercise the proof path at all")
	}
	res := AnalyzeMedia("mp4", bytes.NewReader(data), int64(len(data)), maxBytes, nil)

	if res.Characteristics["duration_seconds"] != nil {
		t.Fatalf("duration_seconds = %#v, want nil -- no moov exists anywhere in this file", res.Characteristics["duration_seconds"])
	}
	if reason, _ := res.Characteristics["duration_seconds_reason"].(string); reason != string(ReasonMalformedInput) {
		t.Fatalf("duration_seconds_reason = %q, want %q -- the ENTIRE top-level chain was proven and walked cleanly (head chain up to mdat's true end, tail chain from there to the file's true end), and moov genuinely does not exist -- a proven fact about the file, not our own limit", reason, ReasonMalformedInput)
	}
}

// TestAnalyzeMediaFitsWithinBudgetRejectsDecoyMoovInMdatPayload is the
// coordinator's explicit follow-up requirement: a file AT OR BELOW
// maxBytes, containing a decoy "moov" (a genuinely parseable mvhd,
// distinctive attacker-chosen duration) inside an mdat payload, with NO
// genuine top-level moov anywhere in the file. Coordinator's own reasoning,
// verbatim: "headLen is clamped to size, so headWin is the entire file...
// firstChildBox... does not find moov... any bytes spelling moov anywhere
// in this file are inside some box's payload, by construction... [the old
// fallback] can accept one and publish its mvhd duration... every
// candidate it can possibly find is a decoy. It cannot rescue a genuine
// top-level box, because [the head walk] already proved there isn't one."
// This must not publish the decoy's duration.
func TestAnalyzeMediaFitsWithinBudgetRejectsDecoyMoovInMdatPayload(t *testing.T) {
	decoy := fakeMoovWith999sDuration()
	mdatPayload := append(make([]byte, 100), decoy...)
	mdat := isobmffBox("mdat", mdatPayload)
	data := append(append([]byte{}, minimalFtyp()...), mdat...)
	// No genuine trailing moov -- this file genuinely has none at the top
	// level; the ONLY "moov"-shaped bytes anywhere are the decoy, buried
	// inside mdat's own declared payload.

	maxBytes := int64(len(data)) // the WHOLE file fits within maxBytes -- !oversizedFile, the branch this test targets
	res := AnalyzeMedia("mp4", bytes.NewReader(data), int64(len(data)), maxBytes, nil)

	if res.Characteristics["duration_seconds"] != nil {
		t.Fatalf("duration_seconds = %#v, want nil -- a decoy moov inside mdat's own payload, with no genuine top-level moov anywhere in the file, must never publish the decoy's attacker-chosen 999.0s", res.Characteristics["duration_seconds"])
	}
	if reason, _ := res.Characteristics["duration_seconds_reason"].(string); reason != string(ReasonMalformedInput) {
		t.Fatalf("duration_seconds_reason = %q, want %q -- the whole file fits within maxBytes, so the head walk alone proves, completely, that no top-level moov exists", reason, ReasonMalformedInput)
	}
}

// TestFindMoovInTailUsabilityCeilingExhaustionIsWorkCeilingBytes originally
// proved that exhausting findMoovInTail's own candidate cap
// (maxMoovTailCandidatesScanned) on candidates that parse as self-
// consistent "moov" headers but are never USABLE (every one of them is the
// same incidental empty box, repeated) reported unknown/work_ceiling_bytes,
// never "not found".
//
// PR9R9 (coordinator follow-up to the round-8 fix, see maxMoovTailCandidatesScanned's
// own doc comment for the full history): findMoovInTail and the substring
// scan it performed are DELETED -- a substring scan can never distinguish
// a genuinely rescued top-level box from a decoy planted inside another
// box's own payload, because neither has proven placement, and accepting
// an unproven candidate is exactly the class this whole file has now
// closed four times over. This fixture's own file fits entirely within
// maxBytes, so findMoov's initial firstChildBox walk already covers the
// WHOLE FILE as a genuine top-level box chain: it examines ftyp and mdat
// (mdat's own honestly-declared size fully covers all 84 embedded decoys
// as ITS payload, never as top-level boxes) and finds no "moov" among
// them. That is now a PROVEN, complete fact about the file's structure
// (malformed_input) -- not an inconclusive candidate-cap exhaustion
// (work_ceiling_bytes). The new mechanism is STRICTLY more precise, never
// weaker: it can tell "we genuinely examined everything and moov is
// absent" apart from "we gave up after N tries", where the deleted
// mechanism could only ever report the latter. tr.Exhausted() is
// correspondingly false -- no ceiling fired, because completing the proof
// never needed one.
func TestFindMoovInTailUsabilityCeilingExhaustionIsWorkCeilingBytes(t *testing.T) {
	const n = maxMoovTailCandidatesScanned + 20
	fakeEmptyMoov := isobmffBox("moov", []byte{})
	var payload []byte
	for i := 0; i < n; i++ {
		payload = append(payload, fakeEmptyMoov...)
	}
	mdat := isobmffBox("mdat", payload)
	data := append(append([]byte{}, minimalFtyp()...), mdat...)

	const maxBytes = 1 << 20 // generous relative to this fixture's own size -- the whole file fits within the head window
	tr := NewTracker(Budget{MaxBytes: int64(len(data)) * 4, MaxDuration: time.Minute})
	res := AnalyzeMedia("mp4", bytes.NewReader(data), int64(len(data)), maxBytes, tr)

	if res.Characteristics["duration_seconds"] != nil {
		t.Fatalf("duration_seconds = %#v, want nil -- every one of these self-consistent but EMPTY moov boxes lives inside mdat's own declared payload, never at the top level", res.Characteristics["duration_seconds"])
	}
	if reason, _ := res.Characteristics["duration_seconds_reason"].(string); reason != string(ReasonMalformedInput) {
		t.Fatalf("duration_seconds_reason = %q, want %q -- a genuine top-level walk of the WHOLE file proved no moov box exists (PR9R9)", reason, ReasonMalformedInput)
	}
	if tr.Exhausted() {
		t.Fatal("Tracker reports exhausted, but no ceiling should have fired -- the top-level walk completed cleanly, well within every bound (only 2 top-level boxes: ftyp, mdat)")
	}
}

// TestFindMoovInTailUsabilityCheckOwnCeilingIsWorkCeilingBytes proves that
// when the ONLY moov candidate reachable in the tail window DOES have a
// real mvhd/trak, but more than maxISOBMFFBoxes filler boxes precede mvhd
// inside it, isUsableMoovCandidate's own lookup cannot reach an answer --
// this must abort with unknown/work_ceiling_bytes, never silently treat the
// inconclusive check as "not usable" and report the file as if it had no
// moov at all (which would misattribute OUR OWN iteration cap to the
// file's structure, D-02a).
func TestFindMoovInTailUsabilityCheckOwnCeilingIsWorkCeilingBytes(t *testing.T) {
	var fillers []byte
	for i := 0; i < maxISOBMFFBoxes+10; i++ {
		fillers = append(fillers, isobmffBox("free", []byte{})...)
	}
	mvhd := mvhdBoxV0(1000, 5000)
	trak := trakBox(tkhdBoxV0(640, 480), stsdBox("avc1"), isobmffHandlerVideo)
	moovPayload := append(append(append([]byte{}, fillers...), mvhd...), trak...)
	moov := isobmffBox("moov", moovPayload)

	const mdatSize = 2 << 20 // large enough that the head window (maxBytes below) cannot skip it as one top-level box
	mdat := isobmffBox("mdat", make([]byte, mdatSize))
	data := append(append(append([]byte{}, minimalFtyp()...), mdat...), moov...)

	const maxBytes = 1 << 20 // smaller than mdat's own declared size, forcing the tail-anchored search; the fixed mp4TailWindow (2 MiB) still comfortably reaches moov near EOF
	tr := NewTracker(Budget{MaxBytes: int64(len(data)) * 4, MaxDuration: time.Minute})
	res := AnalyzeMedia("mp4", bytes.NewReader(data), int64(len(data)), maxBytes, tr)

	if res.Characteristics["duration_seconds"] != nil {
		t.Fatalf("duration_seconds = %#v, want nil -- the only moov candidate reachable in the tail window has a REAL mvhd/trak, but more than maxISOBMFFBoxes filler boxes precede mvhd, so this candidate's own usability check cannot reach an answer", res.Characteristics["duration_seconds"])
	}
	if reason, _ := res.Characteristics["duration_seconds_reason"].(string); reason != string(ReasonWorkCeilingBytes) {
		t.Fatalf("duration_seconds_reason = %q, want %q -- silently treating an inconclusive usability check as \"not usable\" and moving on would misattribute this ceiling to the file's structure", reason, ReasonWorkCeilingBytes)
	}
	if !tr.Exhausted() {
		t.Fatal("Tracker was never marked -- isUsableMoovCandidate's own iteration-ceiling exhaustion must reach the Tracker it was given")
	}
}

// TestFindMoovInTailWorstCaseCandidateValidationStaysBounded originally
// proved round 6's own DoS threat disposition (T-R6C-02): isUsableMoovCandidate's
// per-candidate validation, run up to maxMoovTailCandidatesScanned times
// against an adversarial tail window (64 decoy moov boxes x 20 non-matching
// trak siblings each), still completed in well under a second.
//
// PR9R9 (coordinator follow-up, see maxMoovTailCandidatesScanned's own doc
// comment): findMoovInTail and isUsableMoovCandidate are deleted -- a
// substring scan can never distinguish a rescued genuine box from a
// planted decoy, so it is never consulted for a value. This SAME
// adversarial fixture (unchanged) now proves an even STRONGER timing bound:
// findMoov's initial firstChildBox walk covers the whole file (it fits
// within maxBytes) as a genuine top-level chain and skips ALL 64 decoys x
// 20 trak siblings each in ONE hop, as mdat's own single opaque payload --
// never descending into a single one of them, unlike the deleted
// mechanism's own per-candidate mvhd/trak descent. The walk proves no
// top-level moov exists (malformed_input), and does so essentially
// instantly.
func TestFindMoovInTailWorstCaseCandidateValidationStaysBounded(t *testing.T) {
	nonMatchingTrak := trakBox(tkhdBoxV0(0, 0), stsdBox("mp4a"), isobmffHandlerAudio) // handler_type="soun", never matches wantHandler="vide" below
	const traksPerDecoy = 20                                                          // real, non-ceiling-hitting work per candidate -- comfortably under maxTrakCandidatesScanned (64) so every decoy is rejected on genuine absence, never on its own ceiling
	var decoyPayload []byte
	for i := 0; i < traksPerDecoy; i++ {
		decoyPayload = append(decoyPayload, nonMatchingTrak...)
	}
	decoyPayload = append(isobmffBox("mvhd", []byte{}), decoyPayload...) // present-but-empty mvhd: parses as a child, never as a real duration
	decoyMoov := isobmffBox("moov", decoyPayload)

	const n = maxMoovTailCandidatesScanned // every one of these must be individually rejected, none via its own ceiling
	var payload []byte
	for i := 0; i < n; i++ {
		payload = append(payload, decoyMoov...)
	}
	mdat := isobmffBox("mdat", payload)
	data := append(append([]byte{}, minimalFtyp()...), mdat...)

	const maxBytes = 1 << 20
	tr := NewTracker(Budget{MaxBytes: int64(len(data)) * 4, MaxDuration: time.Minute})

	start := time.Now()
	res := AnalyzeMedia("mp4", bytes.NewReader(data), int64(len(data)), maxBytes, tr)
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Fatalf("AnalyzeMedia took %v against %d adversarial decoys x %d non-matching trak siblings each -- want comfortably under 2s; the top-level walk must skip mdat's ENTIRE declared payload in one hop, never descending into a single decoy", elapsed, n, traksPerDecoy)
	}
	if res.Characteristics["duration_seconds"] != nil {
		t.Fatalf("duration_seconds = %#v, want nil -- every decoy here lives inside mdat's own declared payload, never at the top level", res.Characteristics["duration_seconds"])
	}
	if reason, _ := res.Characteristics["duration_seconds_reason"].(string); reason != string(ReasonMalformedInput) {
		t.Fatalf("duration_seconds_reason = %q, want %q -- a genuine top-level walk of the WHOLE file proved no moov box exists (PR9R9)", reason, ReasonMalformedInput)
	}
	if tr.Exhausted() {
		t.Fatal("Tracker reports exhausted, but no ceiling should have fired -- the top-level walk completed cleanly (only 2 top-level boxes: ftyp, mdat), never touching any of the 64 decoys")
	}
}

// TestFindTrackByHandlerCandidateCeilingIsWorkCeilingBytes proves
// findTrackByHandler's own candidate cap (maxTrakCandidatesScanned,
// T-R4B-02): more trak candidates than the cap allows, none of which
// declare handler_type="vide", must report unknown/work_ceiling_bytes --
// never malformed_input (which would wrongly claim the full moov was
// examined) -- and must mark the Tracker it was given.
func TestFindTrackByHandlerCandidateCeilingIsWorkCeilingBytes(t *testing.T) {
	const n = maxTrakCandidatesScanned + 20 // exceeds the per-search candidate cap; comfortably under maxISOBMFFBoxes so the OUTER box walk's own ceiling is not what fires here
	moovPayload := append([]byte{}, mvhdBoxV0(1000, 5000)...)
	for i := 0; i < n; i++ {
		moovPayload = append(moovPayload, trakBox(tkhdBoxV0(0, 0), stsdBox("mp4a"), isobmffHandlerAudio)...)
	}
	moov := isobmffBox("moov", moovPayload)
	data := append(append([]byte{}, minimalFtyp()...), moov...)
	data = append(data, isobmffBox("mdat", make([]byte, 16))...)

	tr := NewTracker(Budget{MaxBytes: int64(len(data)) * 4, MaxDuration: time.Minute})
	res := AnalyzeMedia("mp4", bytes.NewReader(data), int64(len(data)), int64(len(data)), tr)

	if res.Characteristics["width"] != nil {
		t.Fatalf("width = %#v, want nil -- none of the %d trak candidates examined declares handler_type=vide, and exhausting the candidate cap must not silently fall back to any of them", res.Characteristics["width"], n)
	}
	if reason, _ := res.Characteristics["width_reason"].(string); reason != string(ReasonWorkCeilingBytes) {
		t.Fatalf("width_reason = %q, want %q -- OUR OWN candidate cap, not the file's structure, decided this miss", reason, ReasonWorkCeilingBytes)
	}
	if !tr.Exhausted() {
		t.Fatal("Tracker was never marked -- findTrackByHandler's own candidate-cap exhaustion must reach the Tracker it was given")
	}
}

// TestFindMoovInTailCandidateCeilingIsWorkCeilingBytes originally proved
// findMoovInTail's own candidate cap (maxMoovTailCandidatesScanned,
// T-R4B-02): more incidental "moov" substring occurrences in the tail
// window than the cap allowed, none of which validated as a real box,
// reported unknown/work_ceiling_bytes -- never malformed_input.
//
// PR9R9 (coordinator follow-up, see maxMoovTailCandidatesScanned's own doc
// comment): findMoovInTail is deleted -- a substring scan over these
// incidental "moov" occurrences could never prove any of them was a
// genuine top-level box, so it is never consulted. This fixture's file
// fits within maxBytes, so findMoov's initial firstChildBox walk covers
// the WHOLE FILE: it sees ftyp and ONE mdat box whose own honestly-
// declared size fully covers all of this garbage payload (the 'Y'-padded
// "moov" occurrences are never valid box headers at all, let alone
// top-level ones), and finds no "moov" among the genuine top-level boxes.
// That is now PROVEN (malformed_input), not merely uncertain after a
// candidate cap ran out (work_ceiling_bytes) -- exactly the sharper
// distinction this fixture's own name predates.
func TestFindMoovInTailCandidateCeilingIsWorkCeilingBytes(t *testing.T) {
	const n = maxMoovTailCandidatesScanned + 20
	var payload []byte
	for i := 0; i < n; i++ {
		// Each occurrence's own preceding 4 bytes ('Y','Y','Y','Y') never
		// form a plausible box-size field, so every candidate fails
		// validation and the search must advance to the next one.
		payload = append(payload, bytes.Repeat([]byte{'Y'}, 4)...)
		payload = append(payload, []byte("moov")...)
		payload = append(payload, bytes.Repeat([]byte{'Y'}, 4)...)
	}
	mdat := isobmffBox("mdat", payload)
	data := append(append([]byte{}, minimalFtyp()...), mdat...)

	const maxBytes = 1 << 20 // generous relative to this fixture's own size -- both windows cover the whole tiny file, isolating the candidate cap as the ONLY thing that can produce a ceiling result here
	tr := NewTracker(Budget{MaxBytes: int64(len(data)) * 4, MaxDuration: time.Minute})
	res := AnalyzeMedia("mp4", bytes.NewReader(data), int64(len(data)), maxBytes, tr)

	if res.Characteristics["duration_seconds"] != nil {
		t.Fatalf("duration_seconds = %#v, want nil -- no real moov box exists in this fixture at all", res.Characteristics["duration_seconds"])
	}
	if reason, _ := res.Characteristics["duration_seconds_reason"].(string); reason != string(ReasonMalformedInput) {
		t.Fatalf("duration_seconds_reason = %q, want %q -- a genuine top-level walk of the WHOLE file proved no moov box exists (PR9R9)", reason, ReasonMalformedInput)
	}
	if tr.Exhausted() {
		t.Fatal("Tracker reports exhausted, but no ceiling should have fired -- the top-level walk completed cleanly (only 2 top-level boxes: ftyp, mdat)")
	}
}

// TestAnalyzeMediaWAVWindowTruncationIsWorkCeilingBytes is media.go's own
// audit finding: wavCharacteristics previously reported EVERY miss as
// malformed_input, including a miss caused by the byte window not covering
// the whole file (fmt/data chunks placed past maxBytes) -- OUR OWN resource
// limit, exactly the D-02a class every other analyzer in this package
// already closed.
func TestAnalyzeMediaWAVWindowTruncationIsWorkCeilingBytes(t *testing.T) {
	data := minimalWAVFixture(44100, 2, 16, 44100*2*2*3) // 3 seconds, real file
	const maxBytes = 30                                  // covers RIFF/WAVE + the "fmt " tag but cuts the fmt chunk's own body short
	res := AnalyzeMedia("wav", bytes.NewReader(data), int64(len(data)), maxBytes, nil)

	if res.Characteristics["duration_seconds"] != nil {
		t.Fatalf("duration_seconds = %#v, want nil (fmt/data sit past the truncated window)", res.Characteristics["duration_seconds"])
	}
	if reason, _ := res.Characteristics["duration_seconds_reason"].(string); reason != string(ReasonWorkCeilingBytes) {
		t.Fatalf("duration_seconds_reason = %q, want %q -- the byte window, not the file's structure, decided this miss", reason, ReasonWorkCeilingBytes)
	}
}

// TestAnalyzeMediaWAVReadFailureWithFullWindowIsNotMalformed is
// quick/260812-r6c task 2's own neighbour-audit finding: wavCharacteristics'
// readWindow failure branch had the SAME defect image.go:165's finding
// named, one file over -- when windowTruncated is false (the window was
// sized to cover the file IN FULL) but the underlying read genuinely fails,
// this used to report ReasonMalformedInput, a claim about the FILE's
// structure, for an operational read failure that examined zero bytes.
// readFailureReaderAt (image_test.go) always fails with a non-EOF error,
// independent of size, isolating this to the read-failure path.
func TestAnalyzeMediaWAVReadFailureWithFullWindowIsNotMalformed(t *testing.T) {
	r := &readFailureReaderAt{err: errSimulatedDiskFailure}
	const size = 1024
	res := AnalyzeMedia("wav", r, size, size, nil) // maxBytes == size: window sized to cover the file IN FULL

	if res.Characteristics["duration_seconds"] != nil {
		t.Fatalf("duration_seconds = %#v, want nil -- readFailureReaderAt never returns real bytes", res.Characteristics["duration_seconds"])
	}
	reason, _ := res.Characteristics["duration_seconds_reason"].(string)
	if reason != string(ReasonReadFailed) {
		t.Fatalf("duration_seconds_reason = %q, want %q -- a read failure with a full-coverage window is neither a fact about the file's structure (malformed_input) nor this scan's own byte ceiling (work_ceiling_bytes)", reason, ReasonReadFailed)
	}
}

// TestAnalyzeMediaWAVIterationCeilingIsWorkCeilingBytes isolates the OTHER
// half: a byte window that covers the WHOLE file, but whose chunk walk
// exhausts maxRIFFChunks on filler chunks before ever reaching fmt/data.
func TestAnalyzeMediaWAVIterationCeilingIsWorkCeilingBytes(t *testing.T) {
	data := []byte("RIFF\x00\x00\x00\x00WAVE")
	for i := 0; i < maxRIFFChunks+10; i++ {
		data = append(data, riffChunk("JUNK", []byte{1, 2})...)
	}
	fmtChunk := make([]byte, 16)
	binary.LittleEndian.PutUint16(fmtChunk[0:2], 1)
	binary.LittleEndian.PutUint16(fmtChunk[2:4], 2)
	binary.LittleEndian.PutUint32(fmtChunk[4:8], 44100)
	binary.LittleEndian.PutUint32(fmtChunk[8:12], 44100*2*2)
	binary.LittleEndian.PutUint16(fmtChunk[12:14], 4)
	binary.LittleEndian.PutUint16(fmtChunk[14:16], 16)
	data = append(data, riffChunk("fmt ", fmtChunk)...)
	data = append(data, riffChunk("data", make([]byte, 100))...)

	// maxBytes covers the ENTIRE file -- isolating this test to the
	// iteration-ceiling path, not the byte-window path.
	res := AnalyzeMedia("wav", bytes.NewReader(data), int64(len(data)), int64(len(data)), nil)

	if res.Characteristics["duration_seconds"] != nil {
		t.Fatalf("duration_seconds = %#v, want nil (fmt/data sit past maxRIFFChunks filler chunks)", res.Characteristics["duration_seconds"])
	}
	if reason, _ := res.Characteristics["duration_seconds_reason"].(string); reason != string(ReasonWorkCeilingBytes) {
		t.Fatalf("duration_seconds_reason = %q, want %q -- the chunk-count ceiling, not the file's structure, decided this miss", reason, ReasonWorkCeilingBytes)
	}
}

// TestAnalyzeMediaEBMLSmallMalformedFileIsMalformedNotWorkCeiling is
// media.go's own audit finding: ebmlCharacteristics previously reported
// EVERY miss as work_ceiling_bytes unconditionally, even for a tiny,
// entirely-buffered file with no Info element at all -- collapsing a
// genuine structural fact ("this file has no Duration, and this scan
// examined the whole thing") into a claim that THIS SERVICE's own budget
// was responsible, which would falsely mark the report selective for a
// file this scan actually finished inspecting.
func TestAnalyzeMediaEBMLSmallMalformedFileIsMalformedNotWorkCeiling(t *testing.T) {
	header := ebmlElem(ebmlIDBytes(0x1A45DFA3, 4), []byte{})
	codecElem := ebmlElem(ebmlIDBytes(0x86, 1), []byte("V_MPEG4/ISO/AVC"))
	trackEntryElem := ebmlElem(ebmlIDBytes(0xAE, 1), codecElem)
	tracksElem := ebmlElem(ebmlIDBytes(0x1654AE6B, 4), trackEntryElem)
	// Deliberately NO Info element anywhere -- Duration is genuinely absent,
	// not merely unreached.
	segmentElem := ebmlElem(ebmlIDBytes(0x18538067, 4), tracksElem)
	data := append(header, segmentElem...)

	res := AnalyzeMedia("mkv", bytes.NewReader(data), int64(len(data)), int64(len(data)), nil)

	if res.Characteristics["duration_seconds"] != nil {
		t.Fatalf("duration_seconds = %#v, want nil", res.Characteristics["duration_seconds"])
	}
	if reason, _ := res.Characteristics["duration_seconds_reason"].(string); reason != string(ReasonMalformedInput) {
		t.Fatalf("duration_seconds_reason = %q, want %q -- this tiny file was read and walked in full, with no ceiling of any kind hit; the absence of Info/Duration is a fact about the FILE", reason, ReasonMalformedInput)
	}
	// declared_codec DOES exist (Tracks/TrackEntry/CodecID are present) --
	// proving this is a genuinely mixed file, not one where everything
	// happens to be absent.
	codec, _ := res.Characteristics["declared_codec"].(string)
	if codec != "V_MPEG4/ISO/AVC" {
		t.Fatalf("declared_codec = %q, want %q -- sanity check that this fixture is not simply empty/malformed everywhere", codec, "V_MPEG4/ISO/AVC")
	}
}

func TestAnalyzeMediaMKVDurationWithinHeadWindow(t *testing.T) {
	data := minimalMKVFixture()
	res := AnalyzeMedia("mkv", bytes.NewReader(data), int64(len(data)), int64(len(data)), nil)
	dur, ok := res.Characteristics["duration_seconds"].(float64)
	if !ok {
		t.Fatalf("duration_seconds missing or wrong type: %#v (reason=%v)", res.Characteristics["duration_seconds"], res.Characteristics["duration_seconds_reason"])
	}
	want := 12.5 * 1000000 / 1e9
	if dur != want {
		t.Fatalf("duration_seconds = %v, want %v", dur, want)
	}
	codec, _ := res.Characteristics["declared_codec"].(string)
	if codec != "V_MPEG4/ISO/AVC" {
		t.Fatalf("declared_codec = %q, want %q", codec, "V_MPEG4/ISO/AVC")
	}
}

// TestAnalyzeMediaEBMLReadFailureWithFullWindowIsReadFailedNotWorkCeiling is
// PR9R9 task 2's own sweep (not a named finding by itself, but the SAME
// defect class media.go:382 named for MP4, found by walking every
// readWindow-failure site in this file per that task's own instruction):
// ebmlCharacteristics's readWindow call had the identical shape --
// windowCoversWholeFile true (size <= maxBytes, and small enough to fit
// inside ebmlInfoWindow too) but the underlying read still fails -- and,
// before this fix, unconditionally reported work_ceiling_bytes regardless.
// readFailureReaderAt (image_test.go) isolates this to the read-failure
// path.
func TestAnalyzeMediaEBMLReadFailureWithFullWindowIsReadFailedNotWorkCeiling(t *testing.T) {
	r := &readFailureReaderAt{err: errSimulatedDiskFailure}
	const size = 1024                              // well within ebmlInfoWindow (256 KiB)
	res := AnalyzeMedia("mkv", r, size, size, nil) // maxBytes == size: windowCoversWholeFile

	for _, key := range []string{"duration_seconds", "declared_codec"} {
		if res.Characteristics[key] != nil {
			t.Errorf("%s = %#v, want nil -- readFailureReaderAt never returns real bytes", key, res.Characteristics[key])
		}
		reason, _ := res.Characteristics[key+"_reason"].(string)
		if reason != string(ReasonReadFailed) {
			t.Errorf("%s_reason = %q, want %q -- a read failure with a full-coverage window is neither a fact about the file's structure (malformed_input) nor this scan's own byte ceiling (work_ceiling_bytes); the SAME rule wavCharacteristics and findMoov's own head-window branch already apply (PR9R9 sweep, ebmlCharacteristics's own doc comment)", key, reason, ReasonReadFailed)
		}
	}
}

func TestAnalyzeMediaEBMLWindowDoesNotGrow(t *testing.T) {
	data := mkvFixtureWithPadding(8192)
	cr := &countingReaderAt{data: data}
	const smallMaxBytes = 2048
	res := AnalyzeMedia("mkv", cr, int64(len(data)), smallMaxBytes, nil)

	if res.Characteristics["duration_seconds"] != nil {
		t.Fatalf("duration_seconds = %#v, want nil (Duration sits outside the bounded window)", res.Characteristics["duration_seconds"])
	}
	reason, _ := res.Characteristics["duration_seconds_reason"].(string)
	if reason != string(ReasonWorkCeilingBytes) {
		t.Fatalf("duration_seconds_reason = %q, want %q", reason, ReasonWorkCeilingBytes)
	}
	if cr.totalRequested > smallMaxBytes {
		t.Fatalf("read %d bytes, want <= %d (window must never grow)", cr.totalRequested, smallMaxBytes)
	}
}

func TestAnalyzeMediaWAVDuration(t *testing.T) {
	sampleRate, channels, bits := uint32(44100), uint32(2), uint32(16)
	byteRate := sampleRate * channels * bits / 8
	dataSize := byteRate * 3 // 3 seconds
	data := minimalWAVFixture(sampleRate, channels, bits, dataSize)

	res := AnalyzeMedia("wav", bytes.NewReader(data), int64(len(data)), int64(len(data)), nil)
	dur, ok := res.Characteristics["duration_seconds"].(float64)
	if !ok || dur != 3.0 {
		t.Fatalf("duration_seconds = %#v, want 3.0", res.Characteristics["duration_seconds"])
	}
}

func TestAnalyzeMediaMP3AndOGGReportUnknownDuration(t *testing.T) {
	for _, format := range []string{"mp3", "ogg"} {
		t.Run(format, func(t *testing.T) {
			data := []byte{0, 1, 2, 3}
			res := AnalyzeMedia(format, bytes.NewReader(data), int64(len(data)), int64(len(data)), nil)
			if res.Characteristics["duration_seconds"] != nil {
				t.Fatalf("duration_seconds = %#v, want nil", res.Characteristics["duration_seconds"])
			}
			reason, _ := res.Characteristics["duration_seconds_reason"].(string)
			if reason != string(ReasonParserUnavailable) {
				t.Fatalf("duration_seconds_reason = %q, want %q", reason, ReasonParserUnavailable)
			}
		})
	}
}

func TestAnalyzeMediaAudioHasNoResolutionKeys(t *testing.T) {
	fixtures := map[string][]byte{
		"mp3": {0, 1, 2, 3},
		"ogg": {0, 1, 2, 3},
		"wav": minimalWAVFixture(44100, 2, 16, 44100*2*2),
		"m4a": fastStartMP4Fixture(1000, 5000, 0, 0, "mp4a", isobmffHandlerAudio),
	}
	for format, data := range fixtures {
		t.Run(format, func(t *testing.T) {
			res := AnalyzeMedia(format, bytes.NewReader(data), int64(len(data)), int64(len(data)), nil)
			if _, ok := res.Characteristics["width"]; ok {
				t.Fatalf("%s Characteristics contains width key, want absent", format)
			}
			if _, ok := res.Characteristics["height"]; ok {
				t.Fatalf("%s Characteristics contains height key, want absent", format)
			}
		})
	}
}

func TestAnalyzeMediaM4ADuration(t *testing.T) {
	data := fastStartMP4Fixture(1000, 5000, 0, 0, "mp4a", isobmffHandlerAudio)
	res := AnalyzeMedia("m4a", bytes.NewReader(data), int64(len(data)), int64(len(data)), nil)
	dur, ok := res.Characteristics["duration_seconds"].(float64)
	if !ok || dur != 5.0 {
		t.Fatalf("duration_seconds = %#v, want 5.0", res.Characteristics["duration_seconds"])
	}
	codec, _ := res.Characteristics["declared_codec"].(string)
	if codec != "mp4a" {
		t.Fatalf("declared_codec = %q, want %q", codec, "mp4a")
	}
}

// TestAnalyzeMediaNeverReadsBeyondBudget proves the mp4 tail search is
// bounded by maxBytes, not by maxBytes+mp4TailWindow.
//
// PR9R8 finding (media.go:255), verbatim: "the analyzer first reads
// maxBytes bytes... and then performs this additional tail read of up to
// min(mp4TailWindow, maxBytes). The caller subsequently charges only
// maxBytes to the tracker, so a small configured ceiling can be exceeded
// by nearly 2x." This test's own bound was ORIGINALLY maxBytes+
// mp4TailWindow -- documenting, not disproving, the bug the finding
// names. Tightening it to maxBytes alone is the fix's own acceptance
// test, not a round 4/5/6 regression: this test predates round 4 (it is
// part of the original 58-05 plan baseline, never named by any of the
// three earlier findings) and is not in this round's list of tests that
// must stay green without edits.
func TestAnalyzeMediaNeverReadsBeyondBudget(t *testing.T) {
	data := tailMoovMP4Fixture(1000, 5000, 640, 480, "avc1", isobmffHandlerVideo, 9000)
	cr := &countingReaderAt{data: data}
	const maxBytes = 2048
	_ = AnalyzeMedia("mp4", cr, int64(len(data)), maxBytes, nil)

	if cr.totalRequested > maxBytes {
		t.Fatalf("read %d bytes, want <= %d (maxBytes alone -- PR9R8 finding, media.go:255: the tail read must no longer add a SEPARATE mp4TailWindow-or-maxBytes-sized read on top of the head read)", cr.totalRequested, int64(maxBytes))
	}
}

// TestAnalyzeMediaTailSearchNeverExceedsSharedTrackerBudget is the same
// PR9R8 finding (media.go:255) proven THROUGH THE TRACKER itself, not
// merely a counting io.ReaderAt: a Tracker configured with MaxBytes equal
// to the SAME maxBytes passed to AnalyzeMedia must never observe itself
// exhausted by this single family's own head+tail reads, and consumeActual
// must never push Remaining() negative -- both would happen under the
// pre-fix "head reads maxBytes, tail reads ANOTHER maxBytes" behavior,
// since two Consume(maxBytes)-sized reports against a maxBytes-sized
// budget exhausts it on the second call (budget.go's own Consume: first-
// reason-wins, byte ceiling checked strictly).
func TestAnalyzeMediaTailSearchNeverExceedsSharedTrackerBudget(t *testing.T) {
	data := tailMoovMP4Fixture(1000, 5000, 640, 480, "avc1", isobmffHandlerVideo, 9000)
	const maxBytes = 2048
	tr := NewTracker(Budget{MaxBytes: maxBytes, MaxDuration: time.Minute})

	res := AnalyzeMedia("mp4", bytes.NewReader(data), int64(len(data)), maxBytes, tr)

	dur, ok := res.Characteristics["duration_seconds"].(float64)
	if !ok || dur != 5.0 {
		t.Fatalf("duration_seconds = %#v, want 5.0 -- the tail-anchored real moov must still be found under the reserved-budget windows", res.Characteristics["duration_seconds"])
	}
	if tr.Exhausted() {
		t.Fatalf("Tracker(MaxBytes=%d) reports exhausted after analyzing a single mp4 whose own head+tail reads were sized to fit that SAME budget -- charging nearly 2x maxBytes across the two windows is exactly the PR9R8 finding (media.go:255)", int64(maxBytes))
	}
	if remaining := tr.Remaining(); remaining < 0 {
		t.Fatalf("Remaining() = %d, want >= 0 -- consumeActual reported more than the issued MaxBytes budget", remaining)
	}
}

func TestAnalyzeMediaNeverPanics(t *testing.T) {
	oversizedBoxData := func() []byte {
		mvhdCorrupt := corruptBoxSize(mvhdBoxV0(1000, 5000), 0x7FFFFFFF)
		trak := trakBox(tkhdBoxV0(640, 480), stsdBox("avc1"), isobmffHandlerVideo)
		moov := moovBox(mvhdCorrupt, trak)
		return append(append([]byte{}, minimalFtyp()...), moov...)
	}()

	cases := []struct {
		name     string
		format   string
		data     []byte
		size     int64
		maxBytes int64
	}{
		{"empty mp4", "mp4", []byte{}, 0, 4096},
		{"truncated mp4 box", "mp4", []byte{0, 0, 0, 8, 'm', 'o', 'o', 'v'}, 8, 4096},
		{"mp4 size claims larger than actual", "mp4", []byte{1, 2, 3, 4}, 1 << 20, 4096},
		{"mp4 box declares size larger than remaining buffer", "mp4", oversizedBoxData, int64(len(oversizedBoxData)), int64(len(oversizedBoxData))},
		{"empty mkv", "mkv", []byte{}, 0, 4096},
		{"truncated mkv header", "mkv", []byte{0x1A, 0x45}, 2, 4096},
		{"mkv size claims larger than actual", "mkv", []byte{0x1A, 0x45, 0xDF, 0xA3}, 1 << 20, 4096},
		{"empty wav", "wav", []byte{}, 0, 4096},
		{"truncated wav", "wav", []byte("RIFF"), 4, 4096},
		{"wav size claims larger than actual", "wav", []byte("RIFF\x00\x00\x00\x00WAVE"), 1 << 20, 4096},
		{"garbage mp3", "mp3", []byte{0xDE, 0xAD, 0xBE, 0xEF}, 4, 4096},
		{"garbage ogg", "ogg", []byte{0xDE, 0xAD, 0xBE, 0xEF}, 4, 4096},
		{"undesigned avi", "avi", []byte{0, 1, 2, 3}, 4, 4096},
		{"zero size", "mp4", []byte{1, 2, 3, 4}, 0, 4096},
		{"zero maxBytes", "mp4", []byte{1, 2, 3, 4}, 4, 0},
		{"unrecognized format", "xyz", []byte{1, 2, 3, 4}, 4, 4096},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("AnalyzeMedia panicked: %v", p)
				}
			}()
			_ = AnalyzeMedia(tc.format, bytes.NewReader(tc.data), tc.size, tc.maxBytes, nil)
		})
	}
}

// TestAnalyzeMediaDeclaresNoFindingsOrChecks proves the media family's
// documented decision (AnalyzeMedia's doc comment) to leave every code's
// Check/Finding to the report assembler.
func TestAnalyzeMediaDeclaresNoFindingsOrChecks(t *testing.T) {
	data := fastStartMP4Fixture(1000, 5000, 640, 480, "avc1", isobmffHandlerVideo)
	res := AnalyzeMedia("mp4", bytes.NewReader(data), int64(len(data)), int64(len(data)), nil)
	if len(res.Findings) != 0 {
		t.Fatalf("Findings = %+v, want empty", res.Findings)
	}
	if len(res.Checks) != 0 {
		t.Fatalf("Checks = %+v, want empty", res.Checks)
	}
}

// --- quick/260811-r3s Task 2: media.go:575 finding ---

// TestFindEBMLDurationTimestampScaleCeilingHitYieldsUnknownNotDefaultScale
// is the media.go:575 finding, verbatim: "If Info contains Duration before
// 512 filler elements and a non-default TimestampScale after them, this
// lookup exhausts maxEBMLElements but discards ceilingHit; the separate
// Duration lookup succeeds, and the analyzer computes and publishes a
// duration using the default scale. The default is valid only when the
// element is known to be absent, not when its search stopped early, so this
// path must return an unknown ceiling result instead of an incorrect
// duration."
//
// Info's payload here is: Duration (element #1, found immediately) + 600
// filler Void elements (well past maxEBMLElements=512) + a non-default
// TimestampScale (element #602, never reached).
func TestFindEBMLDurationTimestampScaleCeilingHitYieldsUnknownNotDefaultScale(t *testing.T) {
	durElem := ebmlElem(ebmlIDBytes(0x4489, 2), float64Bytes(12.5)) // Duration: found first, before any filler
	var filler []byte
	for i := 0; i < 600; i++ {
		filler = append(filler, ebmlElem(ebmlIDBytes(0xEC, 1), []byte{})...) // Void, well past maxEBMLElements
	}
	tsElem := ebmlElem(ebmlIDBytes(0x2AD7B1, 3), uintBytes32(500000)) // non-default TimestampScale, placed AFTER the filler
	infoPayload := append(append(append([]byte{}, durElem...), filler...), tsElem...)
	infoElem := ebmlElem(ebmlIDBytes(0x1549A966, 4), infoPayload)

	dur, ok, ceilingHit := findEBMLDuration(infoElem, nil)
	if ok {
		t.Fatalf("findEBMLDuration returned ok=true, dur=%v -- must not compute a duration against an unproven default TimestampScale (its own search was cut short, not proven absent)", dur)
	}
	if !ceilingHit {
		t.Fatal("ceilingHit = false, want true -- TimestampScale's own lookup exhausted maxEBMLElements before an answer was reached")
	}
}

// TestAnalyzeMediaEBMLTimestampScaleCeilingHitYieldsUnknownDuration is the
// same finding at the AnalyzeMedia integration level: duration_seconds must
// come back nil/work_ceiling_bytes, never a value computed against the
// default (unproven) TimestampScale.
func TestAnalyzeMediaEBMLTimestampScaleCeilingHitYieldsUnknownDuration(t *testing.T) {
	header := ebmlElem(ebmlIDBytes(0x1A45DFA3, 4), []byte{})

	durElem := ebmlElem(ebmlIDBytes(0x4489, 2), float64Bytes(12.5))
	var filler []byte
	for i := 0; i < 600; i++ {
		filler = append(filler, ebmlElem(ebmlIDBytes(0xEC, 1), []byte{})...)
	}
	tsElem := ebmlElem(ebmlIDBytes(0x2AD7B1, 3), uintBytes32(500000))
	infoPayload := append(append(append([]byte{}, durElem...), filler...), tsElem...)
	infoElem := ebmlElem(ebmlIDBytes(0x1549A966, 4), infoPayload)

	codecElem := ebmlElem(ebmlIDBytes(0x86, 1), []byte("V_MPEG4/ISO/AVC"))
	trackEntryElem := ebmlElem(ebmlIDBytes(0xAE, 1), codecElem)
	tracksElem := ebmlElem(ebmlIDBytes(0x1654AE6B, 4), trackEntryElem)

	segmentPayload := append(append([]byte{}, infoElem...), tracksElem...)
	segmentElem := ebmlElem(ebmlIDBytes(0x18538067, 4), segmentPayload)
	data := append(header, segmentElem...)

	res := AnalyzeMedia("mkv", bytes.NewReader(data), int64(len(data)), int64(len(data)), nil)
	if v := res.Characteristics["duration_seconds"]; v != nil {
		t.Fatalf("duration_seconds = %#v, want nil -- TimestampScale's search was cut short, not proven absent, so no value may be published", v)
	}
	reason, _ := res.Characteristics["duration_seconds_reason"].(string)
	if reason != string(ReasonWorkCeilingBytes) {
		t.Fatalf("duration_seconds_reason = %q, want %q", reason, ReasonWorkCeilingBytes)
	}
}
