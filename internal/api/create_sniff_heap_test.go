package api

import (
	"bytes"
	"encoding/binary"
	"io"
	"runtime"
	"strings"
	"testing"

	"github.com/tileforge/octoconv/internal/convert"
)

const cfbClassifyHeapBytes = 1 << 20

const texScanHeapBytes = convert.SniffWindowBytes + 64<<10

const (
	worstCFBFATSectors = 109
	worstCFBDirSectors = 4096
)

func worstCFB(sectorShift uint16) (data []byte, lastDirOffset int64) {
	ss := 1 << sectorShift
	buf := make([]byte, (1+worstCFBFATSectors+worstCFBDirSectors)*ss)
	h := buf[:512]
	copy(h, []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1})
	binary.LittleEndian.PutUint16(h[30:], sectorShift)
	firstDir := uint32(worstCFBFATSectors)
	binary.LittleEndian.PutUint32(h[48:], firstDir)
	binary.LittleEndian.PutUint32(h[68:], 0xFFFFFFFE)
	for i := 0; i < 109; i++ {
		v := uint32(0xFFFFFFFF)
		if i < worstCFBFATSectors {
			v = uint32(i)
		}
		binary.LittleEndian.PutUint32(h[76+i*4:], v)
	}
	per := ss / 4
	for d := 0; d < worstCFBDirSectors; d++ {
		s := firstDir + uint32(d)
		next := s + 1
		if d == worstCFBDirSectors-1 {
			next = 0xFFFFFFFE
		}
		binary.LittleEndian.PutUint32(buf[(1+int(s)/per)*ss+(int(s)%per)*4:], next)
		sec := buf[(1+int(s))*ss : (2+int(s))*ss]
		for e := 0; e < ss/128; e++ {
			ent := sec[e*128 : (e+1)*128]
			for k := 0; k < 31; k++ {
				binary.LittleEndian.PutUint16(ent[k*2:], 0x4E00)
			}
			binary.LittleEndian.PutUint16(ent[64:], 64)
			ent[66] = 1
		}
	}
	return buf, int64(1+int(firstDir)+worstCFBDirSectors-1) * int64(ss)
}

func worstTeXWindow(t *testing.T) (prefix []byte, name string, names int) {
	t.Helper()
	for _, c := range []string{"graphicx", "amsmath", "xcolor", "hyperref"} {
		if convert.TeXPackageResolvable(c) {
			name = c
			break
		}
	}
	if name == "" {
		t.Fatal("none of graphicx, amsmath, xcolor, hyperref is resolvable: the worst window cannot be built from names the gate admits")
	}
	head := `\documentclass{article}\usepackage{`
	n := (convert.SniffWindowBytes - len(head) - len(name) - 1) / (len(name) + 1)
	src := head + strings.Repeat(name+",", n) + name + "}"
	src += strings.Repeat(" ", convert.SniffWindowBytes-len(src))
	return []byte(src), name, n + 1
}

type readAtProbe struct {
	r       io.ReaderAt
	at      int64
	base    uint64
	live    int64
	reached bool
}

func (p *readAtProbe) ReadAt(b []byte, off int64) (int, error) {
	if off == p.at && !p.reached {
		p.live = int64(liveHeap()) - int64(p.base)
		p.reached = true
	}
	return p.r.ReadAt(b, off)
}

func allocatedBy(call func()) int64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	call()
	runtime.ReadMemStats(&after)
	return int64(after.TotalAlloc - before.TotalAlloc)
}

func sniffHeap(t *testing.T, r io.ReaderAt, at int64, call func(io.ReaderAt)) (allocated, liveAtRead int64) {
	t.Helper()
	p := &readAtProbe{r: r, at: at}
	p.base = liveHeap()
	allocated = allocatedBy(func() { call(p) })
	if !p.reached {
		t.Fatalf("the sniffer never read offset %d: the measurement did not reach the point it bounds", at)
	}
	return allocated, p.live
}

func TestCreateSniffersStayWithinTheirTerms(t *testing.T) {
	for _, c := range []struct {
		name  string
		shift uint16
	}{{"cfb_v4_worst", 12}, {"cfb_v3_worst", 9}} {
		t.Run(c.name, func(t *testing.T) {
			data, last := worstCFB(c.shift)
			var class convert.CFBClass
			allocated, live := sniffHeap(t, bytes.NewReader(data), last, func(r io.ReaderAt) {
				class = convert.ClassifyCFB(r, int64(len(data)))
			})
			t.Logf("sector %d B, file %d B (%.2f MiB): class %v, allocated by the call %d B (%.2f MiB), live at the last directory sector read %d B (%.2f MiB), term %d B (%.2f MiB)",
				1<<c.shift, len(data), mib(int64(len(data))), class, allocated, mib(allocated), live, mib(live), cfbClassifyHeapBytes, mib(cfbClassifyHeapBytes))
			if class != convert.CFBUnknown {
				t.Errorf("class = %v, want CFBUnknown: no entry names a known stream", class)
			}
			if allocated > cfbClassifyHeapBytes {
				t.Errorf("ClassifyCFB allocated %d B on the worst %d-byte-sector file, more than cfbClassifyHeapBytes %d", allocated, 1<<c.shift, cfbClassifyHeapBytes)
			}
			if live > cfbClassifyHeapBytes {
				t.Errorf("ClassifyCFB held %d B at the last directory sector read, more than cfbClassifyHeapBytes %d", live, cfbClassifyHeapBytes)
			}
		})
	}

	t.Run("tex_window_worst", func(t *testing.T) {
		prefix, name, want := worstTeXWindow(t)
		var unsupported string
		var visited int
		var complete bool
		allocated := allocatedBy(func() {
			complete = convert.VisitTeXPreamblePackages(prefix, func(pkg string) bool {
				visited++
				if convert.TeXPackageResolvable(pkg) {
					return true
				}
				unsupported = pkg
				return false
			})
		})
		t.Logf("window %d B of \\usepackage{%s,...}: %d names visited of %d, unsupported %q, complete %v, allocated by the gate %d B (%.2f MiB), term %d B (%.2f MiB)",
			len(prefix), name, visited, want, unsupported, complete, allocated, mib(allocated), texScanHeapBytes, mib(texScanHeapBytes))
		if len(prefix) != convert.SniffWindowBytes {
			t.Fatalf("window is %d B, want exactly convert.SniffWindowBytes %d", len(prefix), convert.SniffWindowBytes)
		}
		if unsupported != "" || visited != want || complete {
			t.Errorf("the gate visited %d of %d names, unsupported %q, complete %v: want every name visited, none unsupported, complete false", visited, want, unsupported, complete)
		}
		if allocated > texScanHeapBytes {
			t.Errorf("the TeX package gate allocated %d B on the worst window, more than texScanHeapBytes %d", allocated, texScanHeapBytes)
		}
	})
}
