package convert

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// ErrAudioDurationExceeded is returned when the declared duration exceeds
// the configured ceiling -- fail-closed (D-07/VALID-03 precedent), the
// audio analog of ErrDimensionsUnknown/the image pixel-ceiling rejection
// (T-30-02). This phase only needs this Go-level error to exist and be
// unit-testable; mapping it to an HTTP 422 response is a future
// (out-of-scope) API-routing phase's job.
var ErrAudioDurationExceeded = errors.New("declared audio duration exceeds configured maximum")

// maxSaneDurationSeconds is a server-constant plausibility ceiling on the
// ffprobe-reported declared duration, applied in FLOAT space BEFORE the
// float64 -> time.Duration conversion. Go's out-of-range float-to-int
// conversion is implementation-defined (spec, "Conversions"): on amd64 an
// overflowing product saturates to math.MinInt64 (negative), silently
// bypassing a naive `d > max` check -- while arm64 saturates to MaxInt64
// and rejects, so the bypass is invisible on dev machines and live in
// production. 1<<31 seconds (~68 years) is far above any real ceiling and
// safely below float64(math.MaxInt64)/1e9, so the multiplication by
// float64(time.Second) can never overflow int64 for accepted values.
const maxSaneDurationSeconds = 1 << 31

// parseProbedDuration parses ffprobe's duration output and validates it in
// float space before any conversion to time.Duration -- rejecting NaN, +/-Inf,
// negative, and implausibly huge declared durations fail-closed. Split from
// ProbeDuration so the adversarial-input validation is unit-testable without
// ffprobe on PATH and independent of platform conversion behavior.
func parseProbedDuration(raw string) (time.Duration, error) {
	secs, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, fmt.Errorf("ffprobe: unparseable duration %q: %w", raw, err)
	}
	if math.IsNaN(secs) || math.IsInf(secs, 0) || secs < 0 || secs > maxSaneDurationSeconds {
		return 0, fmt.Errorf("ffprobe: implausible duration %v", secs)
	}
	return time.Duration(secs * float64(time.Second)), nil
}

// ProbeDuration runs ffprobe as its own short, bounded, killable subprocess
// (runCommand, exec.go) to read the container's declared duration BEFORE any
// decode/transcribe step runs -- the audio analog of dimensions.go's
// declared-pixel-ceiling check (VALID-03/Phase 7 precedent). ctx should
// carry a SHORT bound distinct from the full engine timeout (ffprobe reading
// container metadata is near-instant even for large files; it must never be
// allowed to run for the full AUDIO_ENGINE_TIMEOUT budget) -- see T-30-03.
// ffprobeDurationArgs builds ffprobe's argv for ProbeDuration, isolated as
// its own function so IN-01's "file:" protocol prefix on the path argv
// element is unit-testable without invoking a real ffprobe subprocess
// (mirrors whisper.go's whisperArgs argv-pinning test style). Also carries
// "-protocol_whitelist file,crypto" (AVE-02/ROADMAP SC5): this probe is
// reused verbatim by Plan 03 as the FIRST ffprobe invocation on untrusted
// VIDEO input in the AV engine's guard stage, so it must be hardened
// identically to the AV engine's new resolution probe (ffprobeStreamArgs,
// avduration.go) -- "every ffmpeg/ffprobe invocation" holds with no
// exception, closing T-34-08b.
func ffprobeDurationArgs(path string) []string {
	// IN-01 (30-REVIEW.md, defense-in-depth): the argv element handed to
	// ffprobe is prefixed with the explicit "file:" protocol specifier so
	// ffprobe can never reinterpret it as a protocol/URL specifier
	// (concat:/http:/pipe:) or a leading-dash option. path itself (used for
	// os.Stat/output-existence checks elsewhere) is left unprefixed --
	// today's callers always pass a server-generated workdir path, so this
	// is a no-op for current behavior; it only matters if a future caller
	// ever threads a client-influenced filename through here.
	return []string{"-v", "error", "-show_entries",
		"format=duration", "-of", "default=noprint_wrappers=1:nokey=1",
		"-protocol_whitelist", "file,crypto", "file:" + path}
}

// probeDuration runs ffprobe through x, the caller's job-scoped engineExec
// (Phase 68 Plan 04) -- unexported because an exported entry point that runs
// ffprobe on client input without a sandbox handle is exactly the shape
// decision 1 closed (T-68-20).
func probeDuration(ctx context.Context, x engineExec, path string) (time.Duration, error) {
	out, err := x.run(ctx, "ffprobe", ffprobeDurationArgs(path)...)
	if err != nil {
		return 0, fmt.Errorf("ffprobe: %w", err)
	}
	return parseProbedDuration(string(out))
}

// enforceMaxDurationOf applies the ceiling to an ALREADY-probed duration, so
// a caller that must probe the same file for other reasons can reuse that
// single probe instead of spawning another ffprobe (WR-05, 34-REVIEW.md).
func enforceMaxDurationOf(d, max time.Duration) error {
	if d > max {
		return fmt.Errorf("%w: declared %v exceeds ceiling %v", ErrAudioDurationExceeded, d, max)
	}
	return nil
}
