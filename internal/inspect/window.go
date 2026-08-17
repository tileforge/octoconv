package inspect

import "io"

// defaultTailWindow bounds a tail-anchored read for structures that live at
// the end of a file (e.g. a PDF trailer, an MP4 moov box on a
// non-fast-start file).
const defaultTailWindow = 64 << 10

// BudgetedReaderAt bounds the TOTAL bytes an io.ReaderAt hands out, for a
// reader this package must pass to a parser that takes no allowance of its own.
//
// PR9R23 named this class in pdf.go and image.go; the sweep for it found a
// third, larger instance outside this package: convert.ClassifyCFB walks up to
// cfbMaxDirSectors (4096) directory sectors of up to 4096 bytes each, i.e. as
// much as ~16 MiB, while its inspect caller is charged the operation's
// allowance -- 8 MiB by default. Every other analyzer here sizes its own
// windows and can simply be told a smaller number; ClassifyCFB is shared with
// the conversion path, where no such allowance exists, so it is bounded from
// the OUTSIDE instead of growing a parameter that only one of its two callers
// could ever supply.
//
// Exhaustion is served as a short read plus io.EOF, which every bounded parser
// in this repo already treats as fail-closed (ClassifyCFB answers CFBUnknown).
// Exceeded then lets the caller say WHY the answer is unknown -- our ceiling,
// not the file -- instead of publishing an unclassified verdict as if the whole
// file had been examined.
type BudgetedReaderAt struct {
	r         io.ReaderAt
	remaining int64
	exceeded  bool
}

// NewBudgetedReaderAt wraps r so that no more than limit bytes are ever
// returned across all ReadAt calls. A non-positive limit serves nothing.
func NewBudgetedReaderAt(r io.ReaderAt, limit int64) *BudgetedReaderAt {
	return &BudgetedReaderAt{r: r, remaining: limit}
}

// ReadAt serves at most the remaining budget, and records that the budget ran
// out if a caller asked for more.
func (b *BudgetedReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if b.remaining <= 0 {
		b.exceeded = true
		return 0, io.EOF
	}
	if int64(len(p)) > b.remaining {
		b.exceeded = true
		p = p[:b.remaining]
	}
	n, err := b.r.ReadAt(p, off)
	b.remaining -= int64(n)
	return n, err
}

// Exceeded reports whether any read was cut short (or refused outright)
// because the budget was exhausted.
func (b *BudgetedReaderAt) Exceeded() bool { return b.exceeded }

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
