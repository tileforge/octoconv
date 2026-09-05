package convert

import (
	"strings"
	"testing"
)

// TestTailBufferKeepsTheEnd is the unit half of the P1 fixed in round 2 of
// PR #16's review: the engine's output was buffered without a bound, and this
// engine's input is a program that can print as fast as it likes.
//
// The tail is what the parsers need -- the missing .sty that aborted the run,
// the biber failure, the final pass's undefined-reference warning all appear at
// the END -- so a buffer that kept the head would be bounded and useless.
func TestTailBufferKeepsTheEnd(t *testing.T) {
	t.Run("keeps the last bytes across many writes", func(t *testing.T) {
		b := &tailBuffer{max: 10}
		for i := 0; i < 100; i++ {
			if n, err := b.Write([]byte("0123456789")); n != 10 || err != nil {
				t.Fatalf("Write = %d, %v; an io.Writer that under-reports breaks exec", n, err)
			}
		}
		got := b.String()
		if !strings.HasSuffix(got, "0123456789") {
			t.Fatalf("tail = %q, want the last write to survive", got)
		}
		if len(b.buf) != 10 {
			t.Fatalf("retained %d bytes with max 10 -- the bound is not holding", len(b.buf))
		}
	})

	t.Run("a single write larger than the bound is also bounded", func(t *testing.T) {
		// The realistic shape: one read from the pipe delivers more than the
		// whole allowance.
		b := &tailBuffer{max: 8}
		b.Write([]byte(strings.Repeat("x", 1000) + "TAIL"))
		if len(b.buf) != 8 {
			t.Fatalf("retained %d bytes from a 1004-byte write with max 8", len(b.buf))
		}
		if !strings.HasSuffix(b.String(), "TAIL") {
			t.Fatalf("tail = %q, want it to end in TAIL", b.String())
		}
	})

	t.Run("truncation is stated, never silent", func(t *testing.T) {
		b := &tailBuffer{max: 4}
		b.Write([]byte("aaaaaaaaaa"))
		got := b.String()
		if !strings.Contains(got, "discarded") {
			t.Fatalf("output = %q: a parser reading this would conclude the engine never printed "+
				"the earlier bytes, which is a claim this buffer cannot make", got)
		}
		if !strings.HasPrefix(got, "[octoconv:") {
			t.Fatalf("the marker must lead, so a tail-reading parser cannot mistake it for engine "+
				"output; got %q", got)
		}
	})

	t.Run("max <= 0 keeps everything", func(t *testing.T) {
		// runCommand's own behaviour, unchanged: veraPDF's whole XML report
		// arrives on stdout and is parsed, so it must not be trimmed.
		b := &tailBuffer{max: 0}
		b.Write([]byte(strings.Repeat("y", 5000)))
		if len(b.Bytes()) != 5000 {
			t.Fatalf("unbounded buffer kept %d of 5000 bytes", len(b.Bytes()))
		}
	})
}

// TestTectonicLogLimitIsAboveTheMeasuredCeiling holds the constant to the
// measurement it came from rather than to a round number someone liked.
func TestTectonicLogLimitIsAboveTheMeasuredCeiling(t *testing.T) {
	// Measured in the shipped image on the largest document the API will
	// accept (2 143 444 B source, 745 pages, 7 passes -- the same run 64-05's
	// timeout comes from).
	const measuredCeilingLog = 1_416_019
	if tectonicLogLimit <= measuredCeilingLog {
		t.Fatalf("tectonicLogLimit = %d is at or below the %d bytes a LEGITIMATE document at the "+
			"input ceiling produces: real diagnostics would be truncated",
			tectonicLogLimit, measuredCeilingLog)
	}
	// And it must stay small against the container the chart gives the worker,
	// or the bound does not bound anything that matters.
	const workerMemoryLimit = 1 << 30
	if tectonicLogLimit > workerMemoryLimit/64 {
		t.Fatalf("tectonicLogLimit = %d is more than 1/64 of the worker's %d-byte container",
			tectonicLogLimit, workerMemoryLimit)
	}
}
