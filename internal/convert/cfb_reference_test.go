package convert

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math/rand/v2"
	"testing"
	"unicode/utf16"
)

func referenceClassifyCFB(r io.ReaderAt, size int64) CFBClass {
	if size < cfbHeaderSize {
		return CFBUnknown
	}

	header := make([]byte, cfbHeaderSize)
	if n, _ := r.ReadAt(header, 0); n != cfbHeaderSize {
		return CFBUnknown
	}
	if !bytes.Equal(header[0:8], oleCFBMagic) {
		return CFBUnknown
	}

	sectorShift := binary.LittleEndian.Uint16(header[30:32])
	if sectorShift != 9 && sectorShift != 12 {
		return CFBUnknown
	}
	sectorSize := 1 << sectorShift

	firstDirSector := binary.LittleEndian.Uint32(header[48:52])
	firstDIFATSector := binary.LittleEndian.Uint32(header[68:72])

	if firstDIFATSector != cfbEndOfChain {
		return CFBUnknown
	}

	fat := referenceBuildCFBFAT(r, header, sectorSize, size)
	if fat == nil {
		return CFBUnknown
	}

	names, ok := referenceCFBWalkDirectory(r, firstDirSector, sectorSize, size, fat)
	if !ok {
		return CFBUnknown
	}
	return referenceClassifyCFBNames(names)
}

func referenceBuildCFBFAT(r io.ReaderAt, header []byte, sectorSize int, size int64) map[uint32]uint32 {
	fat := make(map[uint32]uint32)
	entriesPerFATSector := sectorSize / 4

	for i := 0; i < cfbNumHeaderDIFATSlots; i++ {
		off := cfbDIFATOffset + i*4
		fatSector := binary.LittleEndian.Uint32(header[off : off+4])
		if fatSector >= cfbSentinelThreshold {
			continue
		}
		if !referenceCFBSectorInBounds(fatSector, sectorSize, size) {
			return nil
		}

		data := make([]byte, sectorSize)
		if n, _ := r.ReadAt(data, referenceCFBSectorOffset(fatSector, sectorSize)); n != sectorSize {
			return nil
		}

		base := uint32(i) * uint32(entriesPerFATSector)
		for j := 0; j < entriesPerFATSector; j++ {
			fat[base+uint32(j)] = binary.LittleEndian.Uint32(data[j*4 : j*4+4])
		}
	}
	return fat
}

func referenceCFBWalkDirectory(r io.ReaderAt, start uint32, sectorSize int, size int64, fat map[uint32]uint32) (names []string, ok bool) {
	visited := make(map[uint32]bool)
	entriesPerSector := sectorSize / cfbEntrySize
	sector := start
	count := 0

	for sector < cfbSentinelThreshold {
		if visited[sector] {
			return nil, false
		}
		if count >= cfbMaxDirSectors {
			return nil, false
		}
		if !referenceCFBSectorInBounds(sector, sectorSize, size) {
			return nil, false
		}
		visited[sector] = true
		count++

		data := make([]byte, sectorSize)
		if n, _ := r.ReadAt(data, referenceCFBSectorOffset(sector, sectorSize)); n != sectorSize {
			return nil, false
		}

		for e := 0; e < entriesPerSector; e++ {
			off := e * cfbEntrySize
			entry := data[off : off+cfbEntrySize]

			objType := entry[66]
			if objType == 0x00 {
				continue
			}

			nameLen := binary.LittleEndian.Uint16(entry[64:66])
			if nameLen < 2 || nameLen > cfbNameFieldSize || nameLen%2 != 0 {
				continue
			}

			u16 := make([]uint16, nameLen/2)
			for k := range u16 {
				u16[k] = binary.LittleEndian.Uint16(entry[k*2 : k*2+2])
			}
			if len(u16) > 0 && u16[len(u16)-1] == 0 {
				u16 = u16[:len(u16)-1]
			}
			names = append(names, string(utf16.Decode(u16)))
		}

		next, known := fat[sector]
		if !known {
			return nil, false
		}
		sector = next
	}
	return names, true
}

func referenceClassifyCFBNames(names []string) CFBClass {
	hasEncrypted := false
	hasLegacy := false
	for _, n := range names {
		switch n {
		case "EncryptionInfo", "EncryptedPackage":
			hasEncrypted = true
		case "WordDocument", "Workbook", "Book", "PowerPoint Document":
			hasLegacy = true
		}
	}
	switch {
	case hasEncrypted:
		return CFBEncrypted
	case hasLegacy:
		return CFBLegacy
	default:
		return CFBUnknown
	}
}

func referenceCFBSectorOffset(sector uint32, sectorSize int) int64 {
	return (int64(sector) + 1) * int64(sectorSize)
}

func referenceCFBSectorInBounds(sector uint32, sectorSize int, size int64) bool {
	if sector >= cfbSentinelThreshold {
		return false
	}
	off := referenceCFBSectorOffset(sector, sectorSize)
	return off >= 0 && off+int64(sectorSize) <= size
}

type diffCFBInput struct {
	name string
	data []byte
}

func diffCFBEntry(name string, nameLen int, objType byte) [cfbEntrySize]byte {
	var e [cfbEntrySize]byte
	u16 := utf16.Encode([]rune(name))
	for i, u := range u16 {
		if i*2+2 > cfbNameFieldSize {
			break
		}
		binary.LittleEndian.PutUint16(e[i*2:], u)
	}
	if nameLen < 0 {
		nameLen = (len(u16) + 1) * 2
	}
	binary.LittleEndian.PutUint16(e[64:66], uint16(nameLen))
	e[66] = objType
	return e
}

func diffCFBSetFAT(buf []byte, shift uint16, fatSectors int, sector, next uint32) {
	ss := 1 << shift
	per := ss / 4
	fs := int(sector) / per
	if fs >= fatSectors {
		return
	}
	off := (1+fs)*ss + (int(sector)%per)*4
	binary.LittleEndian.PutUint32(buf[off:], next)
}

func diffCFBDirOffset(shift uint16, fatSectors, d int) int {
	return (1 + fatSectors + d) << shift
}

func diffCFB(shift uint16, fatSectors, dirSectors int, entry func(d, e int) [cfbEntrySize]byte) []byte {
	ss := 1 << shift
	buf := make([]byte, (1+fatSectors+dirSectors)*ss)
	h := buf[:cfbHeaderSize]
	copy(h, []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1})
	binary.LittleEndian.PutUint16(h[30:], shift)
	firstDir := uint32(fatSectors)
	binary.LittleEndian.PutUint32(h[48:], firstDir)
	binary.LittleEndian.PutUint32(h[68:], cfbEndOfChain)
	for i := 0; i < cfbNumHeaderDIFATSlots; i++ {
		v := uint32(0xFFFFFFFF)
		if i < fatSectors {
			v = uint32(i)
		}
		binary.LittleEndian.PutUint32(h[cfbDIFATOffset+i*4:], v)
	}
	for d := 0; d < dirSectors; d++ {
		s := firstDir + uint32(d)
		next := s + 1
		if d == dirSectors-1 {
			next = cfbEndOfChain
		}
		diffCFBSetFAT(buf, shift, fatSectors, s, next)
		off := diffCFBDirOffset(shift, fatSectors, d)
		for e := 0; e < ss/cfbEntrySize; e++ {
			ent := entry(d, e)
			copy(buf[off+e*cfbEntrySize:], ent[:])
		}
	}
	return buf
}

func diffCFBOnly(entries ...[cfbEntrySize]byte) func(d, e int) [cfbEntrySize]byte {
	return func(d, e int) [cfbEntrySize]byte {
		if d == 0 && e < len(entries) {
			return entries[e]
		}
		return [cfbEntrySize]byte{}
	}
}

func diffCFBFilled(lastDir int, last [cfbEntrySize]byte, lastEntry int) func(d, e int) [cfbEntrySize]byte {
	filler := diffCFBEntry(string([]rune{0x4E00, 0x4E00, 0x4E00, 0x4E00, 0x4E00, 0x4E00, 0x4E00, 0x4E00, 0x4E00, 0x4E00, 0x4E00, 0x4E00, 0x4E00, 0x4E00, 0x4E00, 0x4E00, 0x4E00, 0x4E00, 0x4E00, 0x4E00, 0x4E00, 0x4E00, 0x4E00, 0x4E00, 0x4E00, 0x4E00, 0x4E00, 0x4E00, 0x4E00, 0x4E00, 0x4E00}), 64, 1)
	return func(d, e int) [cfbEntrySize]byte {
		if d == lastDir && e == lastEntry {
			return last
		}
		return filler
	}
}

func diffCFBPerSector(names ...string) func(d, e int) [cfbEntrySize]byte {
	return func(d, e int) [cfbEntrySize]byte {
		if e == 1 && d < len(names) && names[d] != "" {
			return diffCFBEntry(names[d], -1, 2)
		}
		if e == 0 && d == 0 {
			return diffCFBEntry("Root Entry", -1, 5)
		}
		return [cfbEntrySize]byte{}
	}
}

func diffCFBCorpus(t *testing.T) []diffCFBInput {
	t.Helper()
	var corpus []diffCFBInput
	add := func(name string, data []byte) {
		corpus = append(corpus, diffCFBInput{name: name, data: data})
	}

	legacy := readFixture(t, "testdata/legacy.doc")
	encrypted := readFixture(t, "testdata/encrypted.docx")
	add("testdata/legacy.doc", legacy)
	add("testdata/encrypted.docx", encrypted)
	for _, n := range []int{0, 7, 8, 200, 511, 512, 513, 1024, len(legacy) / 2, len(legacy) - 1} {
		if n <= len(legacy) {
			add(fmt.Sprintf("legacy.doc[:%d]", n), legacy[:n])
		}
	}
	for _, n := range []int{512, len(encrypted) / 2, len(encrypted) - 1} {
		if n <= len(encrypted) {
			add(fmt.Sprintf("encrypted.docx[:%d]", n), encrypted[:n])
		}
	}

	add("cfbCyclicFATSample", cfbCyclicFATSample())
	add("cfbCorruptedSectorShiftSample", cfbCorruptedSectorShiftSample())
	add("cfbOutOfBoundsDirSample", cfbOutOfBoundsDirSample())
	add("cfbBothMarkersSample", cfbBothMarkersSample())
	add("buildCFBFile WordDocument", buildCFBFile([][cfbEntrySize]byte{cfbEntryBytes("WordDocument", 0x02)}, nil))
	add("buildCFBFile EncryptedPackage", buildCFBFile([][cfbEntrySize]byte{cfbEntryBytes("EncryptedPackage", 0x02)}, nil))
	add("buildCFBFile nothing", buildCFBFile(nil, nil))
	add("not a cfb file", []byte("not a cfb file at all"))

	known := []string{"EncryptionInfo", "EncryptedPackage", "WordDocument", "Workbook", "Book", "PowerPoint Document", "Root Entry", "WordDocumentX", "wordDocument", "Book\x00", "W", ""}
	for _, shift := range []uint16{9, 12} {
		for _, name := range known {
			u := len(utf16.Encode([]rune(name)))
			for _, nameLen := range []int{-1, u * 2, u*2 + 4, u*2 + 1, 0, 1, 2, 63, 64, 66} {
				for _, objType := range []byte{0, 1, 2, 5} {
					add(fmt.Sprintf("shift %d name %q nameLen %d objType %d", shift, name, nameLen, objType),
						diffCFB(shift, 1, 1, diffCFBOnly(diffCFBEntry(name, nameLen, objType))))
				}
			}
		}
		add(fmt.Sprintf("shift %d encrypted and legacy", shift), diffCFB(shift, 1, 1, diffCFBOnly(diffCFBEntry("WordDocument", -1, 2), diffCFBEntry("EncryptionInfo", -1, 2))))
		add(fmt.Sprintf("shift %d legacy and encrypted in different sectors", shift), diffCFB(shift, 1, 3, diffCFBPerSector("Workbook", "", "EncryptedPackage")))
		add(fmt.Sprintf("shift %d no names", shift), diffCFB(shift, 1, 2, diffCFBOnly()))
		add(fmt.Sprintf("shift %d worst names", shift), diffCFB(shift, 2, 3, diffCFBFilled(-1, [cfbEntrySize]byte{}, 0)))
	}

	word := diffCFBEntry("WordDocument", -1, 2)
	add("shift 9 4096 dir sectors", diffCFB(9, 109, cfbMaxDirSectors, diffCFBFilled(cfbMaxDirSectors-1, word, 3)))
	add("shift 9 4097 dir sectors", diffCFB(9, 109, cfbMaxDirSectors+1, diffCFBFilled(cfbMaxDirSectors, word, 3)))
	add("shift 12 4096 dir sectors", diffCFB(12, 109, cfbMaxDirSectors, diffCFBFilled(cfbMaxDirSectors-1, word, 31)))
	add("shift 12 4097 dir sectors", diffCFB(12, 109, cfbMaxDirSectors+1, diffCFBFilled(cfbMaxDirSectors, word, 31)))
	add("shift 12 4096 dir sectors worst names", diffCFB(12, 109, cfbMaxDirSectors, diffCFBFilled(-1, [cfbEntrySize]byte{}, 0)))

	mixed := func() []byte { return diffCFB(9, 1, 4, diffCFBPerSector("EncryptionInfo", "WordDocument", "", "Book")) }

	b := mixed()
	diffCFBSetFAT(b, 9, 1, 4, 1)
	add("cycle back to the first directory sector", b)
	b = mixed()
	diffCFBSetFAT(b, 9, 1, 1, 1)
	add("self cycle", b)
	b = mixed()
	diffCFBSetFAT(b, 9, 1, 2, 100000)
	add("sector past the end of the file", b)
	b = mixed()
	diffCFBSetFAT(b, 9, 1, 1, 0xFFFFFFF9)
	add("sector just below the sentinel threshold", b)
	b = mixed()
	diffCFBSetFAT(b, 9, 1, 1, cfbSentinelThreshold)
	add("chain ends at the sentinel threshold", b)
	b = mixed()
	diffCFBSetFAT(b, 9, 1, 1, 0xFFFFFFFF)
	add("chain ends at FREESECT", b)
	b = mixed()
	diffCFBSetFAT(b, 9, 1, 1, 0)
	add("chain enters the FAT sector", b)

	enc := diffCFB(9, 1, 2, diffCFBPerSector("EncryptionInfo", "WordDocument"))
	diffCFBSetFAT(enc, 9, 1, 1, 5000)
	add("EncryptionInfo in the first sector, chain broken after it", enc)
	enc = diffCFB(12, 1, 2, diffCFBPerSector("EncryptedPackage"))
	diffCFBSetFAT(enc, 12, 1, 1, 77)
	add("EncryptedPackage in the first sector, v4 chain broken after it", enc)

	add("directory sector without a FAT record", diffCFB(9, 1, 130, diffCFBPerSector("EncryptionInfo")))
	add("directory sector at the edge of the FAT", diffCFB(9, 1, 126, diffCFBPerSector("EncryptionInfo")))

	for _, v := range []uint32{0xFFFFFFFC, 0xFFFFFFFD, 0xFFFFFFFE, 0xFFFFFFFF, cfbSentinelThreshold, 0xFFFFFFF9, 1, 2, 99} {
		b = diffCFB(9, 2, 3, diffCFBPerSector("Workbook", "EncryptionInfo"))
		binary.LittleEndian.PutUint32(b[cfbDIFATOffset:], v)
		add(fmt.Sprintf("DIFAT slot 0 = %#x", v), b)
		b = diffCFB(9, 2, 3, diffCFBPerSector("Workbook", "EncryptionInfo"))
		binary.LittleEndian.PutUint32(b[cfbDIFATOffset+4:], v)
		add(fmt.Sprintf("DIFAT slot 1 = %#x", v), b)
		b = diffCFB(9, 2, 3, diffCFBPerSector("Workbook", "EncryptionInfo"))
		binary.LittleEndian.PutUint32(b[cfbDIFATOffset+108*4:], v)
		add(fmt.Sprintf("DIFAT slot 108 = %#x", v), b)
	}

	for _, v := range []uint32{0, 1, 0xFFFFFFFF, 0xFFFFFFFD} {
		b = mixed()
		binary.LittleEndian.PutUint32(b[68:], v)
		add(fmt.Sprintf("first DIFAT sector %#x", v), b)
	}
	for _, v := range []uint16{0, 8, 10, 11, 13, 0xFF} {
		b = mixed()
		binary.LittleEndian.PutUint16(b[30:], v)
		add(fmt.Sprintf("sectorShift %d", v), b)
	}
	for _, v := range []uint32{0, 1, 2, 4, 5, 0xFFFFFFFE, 0xFFFFFFFF, cfbSentinelThreshold, 0xFFFFFFF9} {
		b = mixed()
		binary.LittleEndian.PutUint32(b[48:], v)
		add(fmt.Sprintf("first directory sector %#x", v), b)
	}
	b = mixed()
	b[0] ^= 1
	add("bad magic", b)
	full := mixed()
	for _, n := range []int{511, 512, 1023, 1024, len(full) - 512, len(full) - 1} {
		add(fmt.Sprintf("mixed[:%d]", n), full[:n])
	}
	return corpus
}

func diffCFBPerturbBases() [][]byte {
	return [][]byte{
		diffCFB(9, 1, 4, diffCFBPerSector("EncryptionInfo", "WordDocument", "", "Book")),
		diffCFB(9, 1, 3, diffCFBPerSector("", "PowerPoint Document", "Workbook")),
		diffCFB(9, 2, 3, diffCFBPerSector("EncryptedPackage", "", "")),
		diffCFB(12, 1, 2, diffCFBPerSector("Workbook", "EncryptionInfo")),
		diffCFB(12, 1, 2, diffCFBPerSector("", "Book")),
	}
}

func diffCFBPerturb(rng *rand.Rand, base []byte) ([]byte, string) {
	b := bytes.Clone(base)
	shift := binary.LittleEndian.Uint16(b[30:32])
	ss := 1 << shift
	fatSectors := 0
	for i := 0; i < cfbNumHeaderDIFATSlots; i++ {
		if binary.LittleEndian.Uint32(b[cfbDIFATOffset+i*4:]) < cfbSentinelThreshold {
			fatSectors++
		}
	}
	dirStart := (1 + fatSectors) * ss
	word := func() uint32 {
		switch rng.IntN(4) {
		case 0:
			return uint32(rng.IntN(len(b)/ss + 3))
		case 1:
			return []uint32{0xFFFFFFFC, 0xFFFFFFFD, 0xFFFFFFFE, 0xFFFFFFFF, cfbSentinelThreshold, 0xFFFFFFF9}[rng.IntN(6)]
		default:
			return rng.Uint32()
		}
	}
	switch rng.IntN(4) {
	case 0:
		off := rng.IntN(cfbHeaderSize)
		b[off] = byte(rng.IntN(256))
		return b, fmt.Sprintf("header byte %d", off)
	case 1:
		slot := rng.IntN(cfbNumHeaderDIFATSlots)
		if rng.IntN(2) == 0 {
			off := cfbDIFATOffset + slot*4 + rng.IntN(4)
			b[off] = byte(rng.IntN(256))
			return b, fmt.Sprintf("DIFAT byte %d", off)
		}
		binary.LittleEndian.PutUint32(b[cfbDIFATOffset+slot*4:], word())
		return b, fmt.Sprintf("DIFAT slot %d", slot)
	case 2:
		idx := rng.IntN(fatSectors * ss / 4)
		off := ss + idx*4
		if rng.IntN(2) == 0 {
			off += rng.IntN(4)
			b[off] = byte(rng.IntN(256))
			return b, fmt.Sprintf("FAT byte %d", off)
		}
		binary.LittleEndian.PutUint32(b[off:], word())
		return b, fmt.Sprintf("FAT record %d", idx)
	default:
		entry := dirStart + rng.IntN((len(b)-dirStart)/cfbEntrySize)*cfbEntrySize
		switch rng.IntN(4) {
		case 0:
			binary.LittleEndian.PutUint16(b[entry+64:], uint16(rng.IntN(70)))
			return b, fmt.Sprintf("directory nameLen at %d", entry)
		case 1:
			b[entry+66] = byte(rng.IntN(6))
			return b, fmt.Sprintf("directory objType at %d", entry)
		case 2:
			off := entry + rng.IntN(cfbNameFieldSize)
			b[off] = byte(rng.IntN(256))
			return b, fmt.Sprintf("directory name byte %d", off)
		default:
			off := entry + rng.IntN(cfbEntrySize)
			b[off] = byte(rng.IntN(256))
			return b, fmt.Sprintf("directory byte %d", off)
		}
	}
}

func TestClassifyCFBMatchesTheReference(t *testing.T) {
	verdicts := map[CFBClass]int{}
	corpus := diffCFBCorpus(t)
	for _, in := range corpus {
		want := referenceClassifyCFB(bytes.NewReader(in.data), int64(len(in.data)))
		got := ClassifyCFB(bytes.NewReader(in.data), int64(len(in.data)))
		verdicts[want]++
		if got != want {
			t.Errorf("%s (%d bytes): ClassifyCFB = %v, the 805d49da implementation = %v", in.name, len(in.data), got, want)
		}
	}
	t.Logf("corpus: %d inputs, reference verdicts unknown %d, encrypted %d, legacy %d", len(corpus), verdicts[CFBUnknown], verdicts[CFBEncrypted], verdicts[CFBLegacy])

	const perturbations = 2000
	rng := rand.New(rand.NewPCG(78, 13))
	bases := diffCFBPerturbBases()
	perturbed := map[CFBClass]int{}
	for i := 0; i < perturbations; i++ {
		b, what := diffCFBPerturb(rng, bases[rng.IntN(len(bases))])
		want := referenceClassifyCFB(bytes.NewReader(b), int64(len(b)))
		got := ClassifyCFB(bytes.NewReader(b), int64(len(b)))
		perturbed[want]++
		if got != want {
			t.Errorf("perturbation %d (%s): ClassifyCFB = %v, the 805d49da implementation = %v", i, what, got, want)
		}
	}
	t.Logf("perturbations: %d, reference verdicts unknown %d, encrypted %d, legacy %d", perturbations, perturbed[CFBUnknown], perturbed[CFBEncrypted], perturbed[CFBLegacy])
	for _, c := range []CFBClass{CFBUnknown, CFBEncrypted, CFBLegacy} {
		if verdicts[c] == 0 || perturbed[c] == 0 {
			t.Errorf("verdict %v never produced by the reference (corpus %d, perturbations %d): the differential does not cover it", c, verdicts[c], perturbed[c])
		}
	}
}
