package inspect

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"testing"
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

func trakBox(tkhd, stsdWrapped []byte) []byte {
	stbl := isobmffBox("stbl", stsdWrapped)
	minf := isobmffBox("minf", stbl)
	mdia := isobmffBox("mdia", minf)
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
// covers it directly.
func fastStartMP4Fixture(timescale, duration, width, height uint32, codec string) []byte {
	mvhd := mvhdBoxV0(timescale, duration)
	tkhd := tkhdBoxV0(width, height)
	trak := trakBox(tkhd, stsdBox(codec))
	moov := moovBox(mvhd, trak)
	mdat := isobmffBox("mdat", make([]byte, 64))
	buf := append(append([]byte{}, minimalFtyp()...), moov...)
	return append(buf, mdat...)
}

// tailMoovMP4Fixture places a large mdat BEFORE moov, forcing a real
// tail-anchored search -- mirrors what a real, non-faststart encoder
// produces (Assumption A1).
func tailMoovMP4Fixture(timescale, duration, width, height uint32, codec string, mdatSize int) []byte {
	mvhd := mvhdBoxV0(timescale, duration)
	tkhd := tkhdBoxV0(width, height)
	trak := trakBox(tkhd, stsdBox(codec))
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
	data := fastStartMP4Fixture(1000, 5000, 640, 480, "avc1")
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
	data := tailMoovMP4Fixture(1000, 5000, 640, 480, "avc1", 9000)
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
	trak := trakBox(tkhd, stsdBox("hvc1"))
	moov := moovBox(mvhd, trak)
	data := append(append([]byte{}, minimalFtyp()...), moov...)

	res := AnalyzeMedia("mp4", bytes.NewReader(data), int64(len(data)), int64(len(data)), nil)
	dur, ok := res.Characteristics["duration_seconds"].(float64)
	if !ok || dur != 12.0 {
		t.Fatalf("duration_seconds = %#v, want 12.0", res.Characteristics["duration_seconds"])
	}
}

func TestAnalyzeMediaTkhdDimensionsAreFixedPointDecoded(t *testing.T) {
	data := fastStartMP4Fixture(1000, 5000, 640, 480, "avc1")
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
	trak := trakBox(tkhdBoxV0(640, 480), stsdBox("avc1"))
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
	trak := trakBox(tkhdBoxV0(640, 480), stsdBox("avc1"))
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
		"m4a": fastStartMP4Fixture(1000, 5000, 0, 0, "mp4a"),
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
	data := fastStartMP4Fixture(1000, 5000, 0, 0, "mp4a")
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
// bounded by maxBytes and mp4TailWindow, not by the actual file size.
func TestAnalyzeMediaNeverReadsBeyondBudget(t *testing.T) {
	data := tailMoovMP4Fixture(1000, 5000, 640, 480, "avc1", 9000)
	cr := &countingReaderAt{data: data}
	const maxBytes = 2048
	_ = AnalyzeMedia("mp4", cr, int64(len(data)), maxBytes, nil)

	limit := int64(maxBytes) + mp4TailWindow
	if cr.totalRequested > limit {
		t.Fatalf("read %d bytes, want <= %d (maxBytes + mp4TailWindow)", cr.totalRequested, limit)
	}
}

func TestAnalyzeMediaNeverPanics(t *testing.T) {
	oversizedBoxData := func() []byte {
		mvhdCorrupt := corruptBoxSize(mvhdBoxV0(1000, 5000), 0x7FFFFFFF)
		trak := trakBox(tkhdBoxV0(640, 480), stsdBox("avc1"))
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
	data := fastStartMP4Fixture(1000, 5000, 640, 480, "avc1")
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
