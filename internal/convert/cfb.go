package convert

import (
	"bytes"
	"encoding/binary"
	"io"
)

// CFBClass is the outcome of classifying an OLE-CFB (Compound File Binary)
// upload by reading ONLY its root-storage directory entry names. It is
// deliberately NOT wired into the Converter registry -- see IsOLECFB
// (olecfb.go) for why: every CFB match is an unconditional rejection, this
// merely refines WHICH rejection message the caller should show.
type CFBClass int

// CFBUnknown is the zero value on purpose (D-04/Pitfall 11): every early
// return, parse error, bounds violation, or cycle/cap trip falls through to
// CFBUnknown by construction rather than requiring an explicit assignment at
// every failure site. This is the fail-closed default -- ClassifyCFB never
// "accepts" a file, it only distinguishes rejection reasons.
const (
	CFBUnknown CFBClass = iota
	CFBEncrypted
	CFBLegacy
)

// MS-CFB structural constants (see the ms_cfb_facts block in
// .planning/phases/22-cfb-classification/22-01-PLAN.md for the spec this
// mirrors). This parser intentionally supports only the 109 header-DIFAT
// entries and never reads stream content, mini-FAT, or the directory
// red-black tree -- see cfbWalkDirectory.
const (
	cfbHeaderSize          = 512
	cfbNumHeaderDIFATSlots = 109
	cfbDIFATOffset         = 76
	cfbEntrySize           = 128
	cfbNameFieldSize       = 64
	cfbMaxDirSectors       = 4096

	// cfbEndOfChain is the FAT/DIFAT terminator sentinel.
	cfbEndOfChain uint32 = 0xFFFFFFFE

	// cfbSentinelThreshold: any sector number >= this value is one of the
	// four reserved FAT sentinels (FATSECT 0xFFFFFFFD, ENDOFCHAIN
	// 0xFFFFFFFE, FREESECT 0xFFFFFFFF, DIFSECT 0xFFFFFFFC) or otherwise
	// invalid -- never a real sector index (ms_cfb_facts).
	cfbSentinelThreshold uint32 = 0xFFFFFFFA
)

// ClassifyCFB reads the CFB header, FAT (header-DIFAT entries only), and
// directory-sector chain of r (size bytes long) and classifies it by the set
// of decoded root-storage entry NAMES -- never stream content. It returns
// CFBEncrypted if an EncryptionInfo/EncryptedPackage stream is present
// (encrypted wins over legacy if both are present, D-03), CFBLegacy if a
// legacy-binary Office marker stream is present (WordDocument, Workbook,
// Book, or "PowerPoint Document"), or CFBUnknown otherwise.
//
// Every declared offset/count/sector index is bounds-checked against size
// before use; every FAT-chain follow is guarded by a visited-set and a hard
// sector cap. Any short read, out-of-range field, unsupported >109-DIFAT-
// sector file, or chain cycle/cap-exceeded returns CFBUnknown -- fail
// closed, never a panic, never an unbounded loop (D-04, T-22-01, T-22-02).
func ClassifyCFB(r io.ReaderAt, size int64) CFBClass {
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

	// This parser only follows the 109 header-DIFAT entries. A file that
	// declares additional DIFAT sectors (roughly >7MB) is outside this
	// bound and is rejected fail-closed rather than walked further -- both
	// target fixtures are ~9KB and never need DIFAT sectors (ms_cfb_facts).
	if firstDIFATSector != cfbEndOfChain {
		return CFBUnknown
	}

	buf := make([]byte, sectorSize)
	fat, ok := buildCFBFAT(r, header, buf, size)
	if !ok {
		return CFBUnknown
	}

	encrypted, legacy, ok := cfbWalkDirectory(r, firstDirSector, buf, size, &fat)
	if !ok {
		return CFBUnknown
	}
	switch {
	case encrypted:
		return CFBEncrypted
	case legacy:
		return CFBLegacy
	default:
		return CFBUnknown
	}
}

type cfbFAT struct {
	next      []uint32
	read      [cfbNumHeaderDIFATSlots]bool
	perSector uint32
}

func (f *cfbFAT) lookup(sector uint32) (uint32, bool) {
	slot := sector / f.perSector
	if slot >= cfbNumHeaderDIFATSlots || !f.read[slot] {
		return 0, false
	}
	return f.next[sector], true
}

func buildCFBFAT(r io.ReaderAt, header, buf []byte, size int64) (cfbFAT, bool) {
	sectorSize := len(buf)
	entriesPerFATSector := sectorSize / 4
	fat := cfbFAT{
		next:      make([]uint32, cfbNumHeaderDIFATSlots*entriesPerFATSector),
		perSector: uint32(entriesPerFATSector),
	}

	for i := 0; i < cfbNumHeaderDIFATSlots; i++ {
		off := cfbDIFATOffset + i*4
		fatSector := binary.LittleEndian.Uint32(header[off : off+4])
		if fatSector >= cfbSentinelThreshold {
			// Unused DIFAT slot (FREESECT) or another reserved sentinel:
			// not a real FAT sector, nothing more to read from this slot.
			continue
		}
		if !cfbSectorInBounds(fatSector, sectorSize, size) {
			return cfbFAT{}, false
		}

		if n, _ := r.ReadAt(buf, cfbSectorOffset(fatSector, sectorSize)); n != sectorSize {
			return cfbFAT{}, false
		}

		base := i * entriesPerFATSector
		for j := 0; j < entriesPerFATSector; j++ {
			fat.next[base+j] = binary.LittleEndian.Uint32(buf[j*4 : j*4+4])
		}
		fat.read[i] = true
	}
	return fat, true
}

func cfbWalkDirectory(r io.ReaderAt, start uint32, buf []byte, size int64, fat *cfbFAT) (encrypted, legacy, ok bool) {
	visited := make(map[uint32]struct{})
	sectorSize := len(buf)
	entriesPerSector := sectorSize / cfbEntrySize
	sector := start
	count := 0

	for sector < cfbSentinelThreshold {
		if _, seen := visited[sector]; seen {
			return false, false, false
		}
		if count >= cfbMaxDirSectors {
			return false, false, false
		}
		if !cfbSectorInBounds(sector, sectorSize, size) {
			return false, false, false
		}
		visited[sector] = struct{}{}
		count++

		if n, _ := r.ReadAt(buf, cfbSectorOffset(sector, sectorSize)); n != sectorSize {
			return false, false, false
		}

		for e := 0; e < entriesPerSector; e++ {
			off := e * cfbEntrySize
			entry := buf[off : off+cfbEntrySize]

			objType := entry[66]
			if objType == 0x00 {
				continue // unallocated entry
			}

			nameLen := binary.LittleEndian.Uint16(entry[64:66])
			if nameLen < 2 || nameLen > cfbNameFieldSize || nameLen%2 != 0 {
				// A single malformed entry is not a cycle/DoS -- skip it
				// and keep scanning the rest of this sector's entries.
				continue
			}

			units := int(nameLen / 2)
			if binary.LittleEndian.Uint16(entry[(units-1)*2:]) == 0 {
				units--
			}
			isEncrypted, isLegacy := classifyCFBName(entry[:units*2])
			encrypted = encrypted || isEncrypted
			legacy = legacy || isLegacy
		}

		next, known := fat.lookup(sector)
		if !known {
			// Chain continuation undetermined (sector not covered by any
			// header-DIFAT-referenced FAT range) -- fail closed rather
			// than classify on a possibly-incomplete name set.
			return false, false, false
		}
		sector = next
	}
	return encrypted, legacy, true
}

func classifyCFBName(name []byte) (encrypted, legacy bool) {
	switch {
	case cfbNameIs(name, "EncryptionInfo"), cfbNameIs(name, "EncryptedPackage"):
		return true, false
	case cfbNameIs(name, "WordDocument"), cfbNameIs(name, "Workbook"), cfbNameIs(name, "Book"), cfbNameIs(name, "PowerPoint Document"):
		return false, true
	default:
		return false, false
	}
}

func cfbNameIs(name []byte, want string) bool {
	if len(name) != len(want)*2 {
		return false
	}
	for k := 0; k < len(want); k++ {
		if binary.LittleEndian.Uint16(name[k*2:k*2+2]) != uint16(want[k]) {
			return false
		}
	}
	return true
}

// cfbSectorOffset returns the byte offset of sector number sector. Sector 0
// begins at offset sectorSize (the header always occupies the first 512
// bytes, followed by padding to sectorSize for v4/4096-byte-sector files).
func cfbSectorOffset(sector uint32, sectorSize int) int64 {
	return (int64(sector) + 1) * int64(sectorSize)
}

// cfbSectorInBounds reports whether sector is a real, in-range sector index
// that can be fully read from a file of size bytes.
func cfbSectorInBounds(sector uint32, sectorSize int, size int64) bool {
	if sector >= cfbSentinelThreshold {
		return false
	}
	off := cfbSectorOffset(sector, sectorSize)
	return off >= 0 && off+int64(sectorSize) <= size
}
