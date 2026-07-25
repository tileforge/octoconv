package convert

import (
	"context"
	"strconv"
	"strings"
)

// ProgressUpdate is a single, throttle-ready progress observation for one
// job: a clamped percent complete and an optional ETA. ETA is nil until an
// estimate is derivable (D-04, etaEstimator's warm-up window) -- callers
// must treat a nil ETA as "not yet known," never as zero seconds remaining.
type ProgressUpdate struct {
	// Pct is 0-100, clamped and monotonic within a job (D-02a).
	Pct int
	// ETA is the estimated remaining seconds, or nil before the EMA
	// warm-up completes (D-04).
	ETA *int
}

// ProgressSink is implemented by the worker (Plan 03) and passed to
// ProgressConverter.ConvertWithProgress. SourceDuration is called once,
// early, with the probed source duration (feeds PROG-02's persisted
// source_duration_seconds); Update is called for every throttled tick.
// Neither method carries any engine-internal text (stderr, paths) -- only
// integers -- so no diagnostic detail can leak into a client-facing field
// (T-41-03).
type ProgressSink interface {
	// SourceDuration reports the source's probed duration in seconds,
	// called at most once per conversion, before the first Update.
	SourceDuration(seconds float64)
	// Update reports a new progress observation. Implementations are
	// expected to throttle/persist; ProgressConverter implementations
	// must NOT throttle on their own -- that responsibility belongs to
	// the sink (D-03).
	Update(u ProgressUpdate)
}

// ProgressConverter is an OPTIONAL capability a Converter may additionally
// implement (D-01) -- added ALONGSIDE Converter, never as a change to it.
// Converters with no native machine-readable progress signal (libvips,
// LibreOffice, Chromium, whisper-cli's transcribe stage) do not implement
// this; the worker type-asserts for it and falls back to the plain
// Convert(ctx, in, out, opts) path when absent.
type ProgressConverter interface {
	// ConvertWithProgress behaves like Convert but additionally reports
	// progress to sink as the conversion runs.
	ConvertWithProgress(ctx context.Context, inPath, outPath string, opts map[string]any, sink ProgressSink) error
}

// progressTick is one parsed ffmpeg -progress block. Unexported: this is the
// parser's internal currency, not a seam contract (ProgressUpdate is the
// exported shape callers receive).
type progressTick struct {
	// outTimeSeconds is out_time_us (or out_time_ms, an identical
	// microsecond value despite the "_ms" name) divided by 1e6. Zero and
	// hasData=false when the block reported "N/A" (warm-up / a
	// transient mid-stream gap).
	outTimeSeconds float64
	// speed is ffmpeg's speed= field with the trailing "x" stripped.
	// Zero when "N/A" or unparseable -- callers must not treat a zero
	// speed as "stalled at 0x," only as "not yet known."
	speed float64
	// hasData is false when out_time was "N/A" -- "not yet derivable,"
	// never a garbage/zero value passed off as real (Pitfall 2,
	// 41-RESEARCH.md).
	hasData bool
	// done is true when the block's terminator was "progress=end"
	// rather than "progress=continue".
	done bool
}

// progressBlockParser accumulates ffmpeg -progress "key=value" lines into
// blocks, each terminated by a "progress=continue"/"progress=end" line. It
// is a bounded, per-line consumer: a malformed/truncated/non-numeric line
// is guarded before any strconv call, never panics, and never allocates
// unbounded state (T-41-01) -- one map entry per accumulated key, reset on
// every terminator.
type progressBlockParser struct {
	cur map[string]string
}

// newProgressBlockParser returns a parser ready to consume lines via
// feedLine, one ffmpeg -progress stream at a time.
func newProgressBlockParser() *progressBlockParser {
	return &progressBlockParser{cur: make(map[string]string)}
}

// feedLine consumes one raw line from the ffmpeg -progress stream. It
// returns (tick, true) when the line was the block's "progress=" terminator
// (a completed tick is available), else (progressTick{}, false). Blank
// lines and lines with no "=" are ignored rather than erroring -- ffmpeg's
// own output is server-generated/trusted, but a defensive parser never
// panics on an unexpected shape (T-41-01).
func (p *progressBlockParser) feedLine(line string) (progressTick, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return progressTick{}, false
	}
	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return progressTick{}, false
	}
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)

	if key != "progress" {
		p.cur[key] = value
		return progressTick{}, false
	}

	tick := buildProgressTick(p.cur, value)
	p.cur = make(map[string]string) // reset for the next block
	return tick, true
}

// buildProgressTick assembles a completed tick from one block's accumulated
// fields plus the terminator's value ("continue" or "end").
func buildProgressTick(fields map[string]string, terminator string) progressTick {
	tick := progressTick{done: terminator == "end"}

	outStr, ok := fields["out_time_us"]
	if !ok || outStr == "N/A" {
		outStr, ok = fields["out_time_ms"]
	}
	if ok {
		if seconds, ok := parseProgressMicroseconds(outStr); ok {
			tick.outTimeSeconds = seconds
			tick.hasData = true
		}
	}

	if speedStr, ok := fields["speed"]; ok {
		if speed, ok := parseProgressSpeed(speedStr); ok {
			tick.speed = speed
		}
	}

	return tick
}

// parseProgressMicroseconds parses an out_time_us/out_time_ms field value,
// explicitly guarding the literal "N/A" BEFORE any strconv call (Pitfall 2,
// 41-RESEARCH.md) -- ffmpeg emits "N/A" for every numeric field during its
// warm-up block(s), and this codebase's own microsecond wart means the
// division by 1e6 must always follow a successful parse, never precede a
// guard check.
func parseProgressMicroseconds(raw string) (seconds float64, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "N/A" {
		return 0, false
	}
	us, err := parseProgressFloat(raw)
	if err != nil {
		return 0, false
	}
	return us / 1e6, true
}

// parseProgressSpeed parses ffmpeg's speed= field, which carries a trailing
// "x" (e.g. "2.69x", or leading-space-padded "   3x") and reports "N/A"
// during warm-up -- both guarded before strconv, mirroring
// parseProgressMicroseconds's discipline.
func parseProgressSpeed(raw string) (speed float64, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "N/A" {
		return 0, false
	}
	raw = strings.TrimSuffix(raw, "x")
	raw = strings.TrimSpace(raw)
	v, err := parseProgressFloat(raw)
	if err != nil {
		return 0, false
	}
	return v, true
}

// parseProgressFloat is the single strconv.ParseFloat call site for the
// block parser -- every caller guards "N/A"/empty BEFORE reaching here, so
// this never needs its own N/A special case (Pitfall 2, 41-RESEARCH.md).
func parseProgressFloat(raw string) (float64, error) {
	return strconv.ParseFloat(raw, 64)
}
