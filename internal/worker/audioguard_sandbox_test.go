package worker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/apaderin/octoconv/internal/convert"
	"github.com/apaderin/octoconv/internal/jobs"
)

// fakeAudioGuardSandboxHelper writes an executable bash script that plays
// the role of the sandbox helper (`<worker> __sandbox-exec ...`) without
// ever applying Landlock: it records its own argv, then execs whatever
// follows the first "--" -- a small local copy of
// internal/convert/sandboxexec_test.go's own fakeSandboxHelper. package
// convert's test helpers are unexported and not visible from this package,
// so this is copied rather than shared.
//
// calls() returns every invocation recorded so far, parsed back from the
// argv log, oldest first; nil if the helper was never invoked.
func fakeAudioGuardSandboxHelper(t *testing.T) (helper []string, calls func() [][]string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-sandbox-helper.sh")
	callsPath := script + ".calls"
	const body = `#!/bin/bash
script="$0"
for a in "$@"; do printf '%s\n' "$a" >> "${script}.calls"; done
printf -- '--end--\n' >> "${script}.calls"

args=("$@")
sep=-1
for i in "${!args[@]}"; do
  if [ "${args[$i]}" = "--" ]; then sep=$i; break; fi
done
if [ "$sep" -lt 0 ]; then
  echo "fake-sandbox-helper: no -- separator in argv" >&2
  exit 1
fi
rest=("${args[@]:$((sep+1))}")
exec "${rest[@]}"
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("fakeAudioGuardSandboxHelper: %v", err)
	}

	calls = func() [][]string {
		data, err := os.ReadFile(callsPath)
		if err != nil {
			return nil
		}
		var invocations [][]string
		var cur []string
		for _, line := range strings.Split(string(data), "\n") {
			switch line {
			case "--end--":
				invocations = append(invocations, cur)
				cur = nil
			case "":
				// A blank line is the trailing newline of the file, not an
				// argument.
			default:
				cur = append(cur, line)
			}
		}
		return invocations
	}
	return []string{script, "__sandbox-exec"}, calls
}

// fakeAudioGuardEngine writes an executable sh script named name in dir -- a
// local copy of internal/convert/sandboxexec_test.go's own fakeEngine.
func fakeAudioGuardEngine(t *testing.T, dir, name, script string) {
	t.Helper()
	p := filepath.Join(dir, name)
	body := "#!/bin/sh\n" + script
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatalf("fakeAudioGuardEngine: %v", err)
	}
}

// audioGuardPathWith prepends dir to PATH for the duration of the test, so
// exec.LookPath (forJob's own resolution) finds a fake engine placed there.
func audioGuardPathWith(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestAudioGuardRunsFFprobeThroughTheWorkersHelper proves a helper given to
// NewHandler reaches the audio guard's ffprobe call -- the end-to-end
// threading from Handler.audioSandboxHelper through NewRunner's
// audioPreConvertGuard installation to enforceAudioGuardBeforeConvert's
// convert.EnforceMaxDuration call -- with a nil-helper control proving the
// test observes the HELPER, not merely that the guard ran.
func TestAudioGuardRunsFFprobeThroughTheWorkersHelper(t *testing.T) {
	t.Run("confined: helper receives the probe, strictly before Convert", func(t *testing.T) {
		helper, calls := fakeAudioGuardSandboxHelper(t)
		dir := t.TempDir()
		fakeAudioGuardEngine(t, dir, "ffprobe", "printf '1.5\\n'\n")
		audioGuardPathWith(t, dir)

		h := &Handler{audioMaxDuration: time.Hour, audioSandboxHelper: helper}
		NewRunner(h, AudioDescriptor)

		jobDir := t.TempDir()
		inPath := filepath.Join(jobDir, "in.wav")
		if err := os.WriteFile(inPath, []byte("x"), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}

		var probedBeforeConvert bool
		convertCalls := 0
		convertFn := func() error {
			convertCalls++
			probedBeforeConvert = len(calls()) == 1
			return nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := h.preConvertGuard(ctx, &jobs.Job{Engine: convert.EngineAudio}, inPath, convertFn); err != nil {
			t.Fatalf("preConvertGuard = %v, want nil", err)
		}
		if convertCalls != 1 {
			t.Fatalf("convertFn called %d times, want exactly 1", convertCalls)
		}
		if !probedBeforeConvert {
			t.Fatal("convertFn ran before the helper was invoked -- the guard did not run before Convert")
		}

		invocations := calls()
		if len(invocations) != 1 {
			t.Fatalf("helper invoked %d time(s), want 1: %v", len(invocations), invocations)
		}
		argv := invocations[0]
		if len(argv) == 0 || argv[0] != "__sandbox-exec" {
			t.Fatalf("argv[0] = %v, want __sandbox-exec first", argv)
		}
		sep := -1
		for i, tok := range argv {
			if tok == "--" {
				sep = i
				break
			}
		}
		if sep < 0 {
			t.Fatalf("no -- separator in %v", argv)
		}
		var rw []string
		for _, tok := range argv[:sep] {
			if strings.HasPrefix(tok, "rw:") {
				rw = append(rw, strings.TrimPrefix(tok, "rw:"))
			}
		}
		if len(rw) != 1 || rw[0] != jobDir {
			t.Errorf("rw tokens = %v, want exactly [%s]", rw, jobDir)
		}
		if sep+1 >= len(argv) || filepath.Base(argv[sep+1]) != "ffprobe" {
			t.Errorf("argv after -- = %v, want the resolved ffprobe path first", argv[sep+1:])
		}
	})

	// Control: with a nil audioSandboxHelper, the SAME script exists on disk
	// (its .calls tracking file is created), but is never named as the
	// Handler's helper -- proving the prior subtest's PASS is evidence of
	// the helper actually being invoked, not of some other side effect.
	t.Run("control: nil helper records no invocation", func(t *testing.T) {
		dir := t.TempDir()
		fakeAudioGuardEngine(t, dir, "ffprobe", "printf '1.5\\n'\n")
		audioGuardPathWith(t, dir)
		_, calls := fakeAudioGuardSandboxHelper(t)

		h := &Handler{audioMaxDuration: time.Hour, audioSandboxHelper: nil}
		NewRunner(h, AudioDescriptor)

		jobDir := t.TempDir()
		inPath := filepath.Join(jobDir, "in.wav")
		if err := os.WriteFile(inPath, []byte("x"), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		convertCalls := 0
		if err := h.preConvertGuard(ctx, &jobs.Job{Engine: convert.EngineAudio}, inPath, func() error { convertCalls++; return nil }); err != nil {
			t.Fatalf("preConvertGuard = %v, want nil", err)
		}
		if convertCalls != 1 {
			t.Fatalf("convertFn called %d times, want exactly 1", convertCalls)
		}
		if invocations := calls(); invocations != nil {
			t.Errorf("helper invoked %v times though audioSandboxHelper is nil -- the test is observing something other than the helper", invocations)
		}
	})

	t.Run("over ceiling rejects and never calls convertFn", func(t *testing.T) {
		helper, _ := fakeAudioGuardSandboxHelper(t)
		dir := t.TempDir()
		fakeAudioGuardEngine(t, dir, "ffprobe", "printf '7200\\n'\n")
		audioGuardPathWith(t, dir)

		h := &Handler{audioMaxDuration: time.Hour, audioSandboxHelper: helper}
		NewRunner(h, AudioDescriptor)

		jobDir := t.TempDir()
		inPath := filepath.Join(jobDir, "in.wav")
		if err := os.WriteFile(inPath, []byte("x"), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		convertCalls := 0
		err := h.preConvertGuard(ctx, &jobs.Job{Engine: convert.EngineAudio}, inPath, func() error { convertCalls++; return nil })
		if !errors.Is(err, convert.ErrAudioDurationExceeded) {
			t.Fatalf("preConvertGuard error = %v, want errors.Is ErrAudioDurationExceeded", err)
		}
		if convertCalls != 0 {
			t.Fatalf("convertFn called %d times, want 0", convertCalls)
		}
	})

	t.Run("non-audio job never probes", func(t *testing.T) {
		helper, calls := fakeAudioGuardSandboxHelper(t)
		dir := t.TempDir()
		fakeAudioGuardEngine(t, dir, "ffprobe", "printf '7200\\n'\n") // would reject if it ever ran
		audioGuardPathWith(t, dir)

		h := &Handler{audioMaxDuration: time.Hour, audioSandboxHelper: helper}
		NewRunner(h, AudioDescriptor)

		jobDir := t.TempDir()
		inPath := filepath.Join(jobDir, "in.png")
		if err := os.WriteFile(inPath, []byte("x"), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		convertCalls := 0
		if err := h.preConvertGuard(ctx, &jobs.Job{Engine: convert.EngineImage}, inPath, func() error { convertCalls++; return nil }); err != nil {
			t.Fatalf("preConvertGuard = %v, want nil for a non-audio job", err)
		}
		if convertCalls != 1 {
			t.Fatalf("convertFn called %d times, want exactly 1", convertCalls)
		}
		if invocations := calls(); invocations != nil {
			t.Errorf("helper invoked %v times for a non-audio job, want zero", invocations)
		}
	})
}
