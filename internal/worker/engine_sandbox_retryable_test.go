package worker

import (
	"errors"
	"fmt"
	"testing"

	"github.com/apaderin/octoconv/internal/convert"
)

// TestEngineSandboxUnavailableIsRetryableForEveryConfinedClass proves the
// sentinel decides BEFORE any class's own terminal-prefix match: a helper
// that could not establish the ruleset is a NODE fault, and every classifier
// here must treat it as retryable even though the wrapped text also carries
// that class's own terminal shape ("pandoc: pandoc:", "ocr: tesseract:",
// ErrAVTranscodeFailed, "audio: ffmpeg:").
func TestEngineSandboxUnavailableIsRetryableForEveryConfinedClass(t *testing.T) {
	cases := []struct {
		name       string
		isTerminal func(error) bool
		err        error
	}{
		{"pandoc", isPandocTerminal, fmt.Errorf("pandoc: pandoc: %w", convert.ErrEngineSandboxUnavailable)},
		{"ocr", isOCRTerminal, fmt.Errorf("ocr: tesseract: %w", convert.ErrEngineSandboxUnavailable)},
		{"av", isAVTerminal, fmt.Errorf("av: transcode: %w: %w", convert.ErrAVTranscodeFailed, convert.ErrEngineSandboxUnavailable)},
		{"audio", isAudioTerminal, fmt.Errorf("audio: ffmpeg: %w", convert.ErrEngineSandboxUnavailable)},
		// "stat output" is itself a chromium terminal signature
		// (terminalChromiumSignatures) -- wrapping the sentinel around it is
		// the non-vacuous case: the sentinel must win even when the wrapped
		// text ALSO carries html's own terminal shape.
		{"html", isHTMLTerminal, fmt.Errorf("chromium: stat output: %w", convert.ErrEngineSandboxUnavailable)},
		// The wrapped text ALSO carries ErrSanitizeMalformedInput, which
		// classifyEngineFailure's OWN doc comment says is terminal on its
		// own -- the non-vacuous case: the sentinel must win even when the
		// wrapped text carries probe's own terminal shape too.
		{"probe", isProbeTerminal, fmt.Errorf("%w: ps2write pass: %w",
			convert.ErrSanitizeMalformedInput, convert.ErrEngineSandboxUnavailable)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.isTerminal(c.err) {
				t.Errorf("%s classified a sandbox-unavailable fault as terminal even though the text "+
					"also carries this class's own terminal shape: %v", c.name, c.err)
			}
		})
	}
}

// TestEngineSandboxControlShapesAreTerminal is the non-vacuity control: the
// SAME shapes, wrapping a plain engine failure instead of the sentinel, must
// still classify terminal -- proving the previous test passes BECAUSE of the
// sentinel check, and not because these shapes happen to be terminal-blind.
func TestEngineSandboxControlShapesAreTerminal(t *testing.T) {
	plain := errors.New("exit status 1")
	cases := []struct {
		name       string
		isTerminal func(error) bool
		err        error
	}{
		{"pandoc", isPandocTerminal, fmt.Errorf("pandoc: pandoc: %w", plain)},
		{"ocr", isOCRTerminal, fmt.Errorf("ocr: tesseract: %w", plain)},
		{"av", isAVTerminal, fmt.Errorf("av: transcode: %w: %w", convert.ErrAVTranscodeFailed, plain)},
		{"audio", isAudioTerminal, fmt.Errorf("audio: ffmpeg: %w", plain)},
		{"html", isHTMLTerminal, fmt.Errorf("chromium: stat output: %w", plain)},
		{"probe", isProbeTerminal, fmt.Errorf("%w: ps2write pass: %w", convert.ErrSanitizeMalformedInput, plain)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !c.isTerminal(c.err) {
				t.Errorf("%s: the shape alone (without the sandbox sentinel) must be terminal, or the "+
					"retryable test above is vacuous: %v", c.name, c.err)
			}
		})
	}
}
