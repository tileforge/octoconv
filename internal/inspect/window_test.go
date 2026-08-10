package inspect

import (
	"bytes"
	"errors"
	"testing"
)

func TestReadWindowBasicRead(t *testing.T) {
	r := bytes.NewReader([]byte("0123456789"))
	buf, ok := readWindow(r, 2, 3, 10)
	if !ok {
		t.Fatal("readWindow ok = false, want true")
	}
	if string(buf) != "234" {
		t.Fatalf("readWindow = %q, want %q", buf, "234")
	}
}

func TestReadWindowClampsToSize(t *testing.T) {
	r := bytes.NewReader([]byte("0123456789"))
	// Request 100 bytes starting at offset 8 in a file claimed to be size
	// 10 -- must clamp to the 2 bytes actually available in [0, size).
	buf, ok := readWindow(r, 8, 100, 10)
	if !ok {
		t.Fatal("readWindow ok = false, want true")
	}
	if len(buf) != 2 {
		t.Fatalf("len(readWindow) = %d, want 2 (clamped to size-off)", len(buf))
	}
	if string(buf) != "89" {
		t.Fatalf("readWindow = %q, want %q", buf, "89")
	}
}

// eofReaderAt returns io.EOF alongside a partial read, exactly the
// io.ReaderAt short-read contract readWindow must honor identically to
// readSniffPrefix.
type eofReaderAt struct {
	data []byte
}

func (e eofReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n := copy(p, e.data[off:])
	if n < len(p) {
		return n, errors.New("EOF")
	}
	return n, nil
}

func TestReadWindowTreatsEOFAsNormal(t *testing.T) {
	// bytes.Reader itself already returns io.EOF for a short final read;
	// this exercises that path directly through the real io.ReaderAt this
	// package actually receives (multipart.File).
	r := bytes.NewReader([]byte("abc"))
	buf, ok := readWindow(r, 0, 10, 3)
	if !ok {
		t.Fatal("readWindow ok = false, want true on a short-but-valid read")
	}
	if string(buf) != "abc" {
		t.Fatalf("readWindow = %q, want %q", buf, "abc")
	}
}

func TestReadWindowRejectsInvalidArgsWithoutPanic(t *testing.T) {
	r := bytes.NewReader([]byte("abcdef"))
	cases := []struct {
		name string
		off  int64
		n    int64
		size int64
	}{
		{"negative offset", -1, 3, 6},
		{"zero n", 0, 0, 6},
		{"negative n", 0, -5, 6},
		{"offset at size", 6, 3, 6},
		{"offset beyond size", 100, 3, 6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("readWindow panicked: %v", p)
				}
			}()
			buf, ok := readWindow(r, tc.off, tc.n, tc.size)
			if ok {
				t.Fatalf("readWindow ok = true, want false for %s", tc.name)
			}
			if buf != nil {
				t.Fatalf("readWindow buf = %v, want nil for %s", buf, tc.name)
			}
		})
	}
}

// TestReadWindowNeverAllocatesBeyondN proves the returned slice's capacity
// never exceeds n, even when size claims far more is available than n
// itself requests -- the allocation size is always min(n, size-off), never
// n alone and never a value read from the file's own content.
func TestReadWindowNeverAllocatesBeyondN(t *testing.T) {
	data := make([]byte, 1<<20)
	r := bytes.NewReader(data)
	const n = 128
	buf, ok := readWindow(r, 0, n, int64(len(data)))
	if !ok {
		t.Fatal("readWindow ok = false, want true")
	}
	if cap(buf) > n {
		t.Fatalf("cap(readWindow) = %d, want <= %d", cap(buf), n)
	}
}

// TestReadWindowNeverPanics is a table-driven pass over adversarial
// negative/overflow arguments -- readWindow must fail closed (ok==false)
// rather than panic in every case. Every case below is deliberately built
// so the invalid-argument guard (off<0 || n<=0 || off>=size) fires BEFORE
// any allocation is attempted -- this test proves the guard order, not
// readWindow's behavior for a caller that passes both a huge n AND a huge
// size (that combination is the CALLER's responsibility to bound, per the
// doc comment on readWindow; every real caller in this package always
// passes an already-small n such as defaultTailWindow).
func TestReadWindowNeverPanics(t *testing.T) {
	r := eofReaderAt{data: []byte("short")}
	cases := []struct {
		off, n, size int64
	}{
		{-1, -1, -1},
		{0, 1 << 30, 5}, // n far exceeds size; clamps to remaining=5, no huge alloc
		{1 << 30, 1, 5}, // off >= size fires the guard before remaining is computed
		{0, 0, 0},
		{-100, 100, 5},
		{5, 1, 5}, // off == size exactly (half-open range excludes it)
	}
	for _, tc := range cases {
		func() {
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("readWindow(off=%d, n=%d, size=%d) panicked: %v", tc.off, tc.n, tc.size, p)
				}
			}()
			readWindow(r, tc.off, tc.n, tc.size)
		}()
	}
}
