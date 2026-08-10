package inspect

import "io"

// defaultTailWindow bounds a tail-anchored read for structures that live at
// the end of a file (e.g. a PDF trailer, an MP4 moov box on a
// non-fast-start file).
const defaultTailWindow = 64 << 10

// maxTokenScanIterations is the hard ceiling on the number of steps any
// token/box/element-walking loop in this package may take. See the
// "why iterations, not context" note on readWindow below -- this constant,
// not a context deadline, is the load-bearing bound.
const maxTokenScanIterations = 1 << 20

// readWindow is the ONE way this package reads untrusted bytes. It is a
// direct copy of the discipline in internal/convert/markupsniff.go's
// readSniffPrefix (readSniffPrefix itself is unexported by that package, so
// this is a deliberate copy, not an import -- there is no other way to
// reuse it across package boundaries).
//
// It reads at most n bytes starting at off, clamped to [0, size): the
// returned slice is allocated at exactly min(n, size-off) bytes and NEVER
// at a size taken from the file's own content -- every existing parser in
// this repo (cfbWalkDirectory, walkBoxes, matchEBML) follows this same
// rule, and a new parser that allocated make([]byte, declaredLen) before
// validating declaredLen against the actual available bytes would
// reintroduce exactly the attacker-controlled-allocation class those
// parsers already close.
//
// io.EOF from r.ReadAt is treated as a normal, expected end of input (a
// partial read is a valid result, not an error) -- r's io.ReaderAt
// short-read contract is honored identically to readSniffPrefix's.
//
// off < 0, n <= 0, or off >= size returns (nil, false) without panicking.
//
// Why the budget is an ITERATION ceiling, not a context.WithTimeout: a
// context deadline cancels an I/O-bound WAIT, but it does NOT interrupt a
// CPU-bound loop that never returns to the scheduler or checks ctx.Err()
// (58-RESEARCH.md Q4). None of this project's existing bounded parsers use
// context for their internal loops -- cfbWalkDirectory bounds itself with
// cfbMaxDirSectors plus a visited-sector set, and this package's own
// token-scanning loops (58-04/58-05) are expected to bound themselves with
// maxTokenScanIterations the same way. context, if used at all in this
// package, is an outer courtesy bound that guarantees a semaphore slot
// eventually releases even if a bug slips through -- it is never the
// primary mechanism.
func readWindow(r io.ReaderAt, off, n, size int64) ([]byte, bool) {
	if off < 0 || n <= 0 || off >= size {
		return nil, false
	}
	remaining := size - off
	if n > remaining {
		n = remaining
	}
	buf := make([]byte, n)
	read, err := r.ReadAt(buf, off)
	if err != nil && err != io.EOF {
		return nil, false
	}
	buf = buf[:read]
	if len(buf) == 0 {
		return nil, false
	}
	return buf, true
}
