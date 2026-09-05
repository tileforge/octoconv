package convert

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"
)

// tailBuffer is an io.Writer that retains at most max bytes, discarding from
// the FRONT as more arrive. max <= 0 retains everything.
type tailBuffer struct {
	max     int
	buf     []byte
	dropped int64
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if t.max <= 0 {
		t.buf = append(t.buf, p...)
		return n, nil
	}
	if len(p) >= t.max {
		t.dropped += int64(len(t.buf)) + int64(len(p)-t.max)
		t.buf = append(t.buf[:0], p[len(p)-t.max:]...)
		return n, nil
	}
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.max; over > 0 {
		t.dropped += int64(over)
		t.buf = append(t.buf[:0], t.buf[over:]...)
	}
	return n, nil
}

// Bytes returns what was retained. When anything was discarded it says so, at
// the FRONT, where no parser reading the tail can mistake it for engine output
// -- silence about a truncation would turn "the log does not contain X" into a
// claim this buffer is not entitled to make.
func (t *tailBuffer) Bytes() []byte {
	if t.dropped == 0 {
		return t.buf
	}
	return append([]byte(fmt.Sprintf("[octoconv: %d earlier bytes of engine output discarded]\n", t.dropped)), t.buf...)
}

func (t *tailBuffer) String() string { return string(t.Bytes()) }

// runCommand executes an external engine with hardened process handling:
//
//   - the child runs in its own process group (Setpgid), so when ctx is
//     cancelled or its deadline fires we kill the whole group — children such as
//     LibreOffice's soffice.bin do not get orphaned/left hanging;
//   - stdout is captured and returned to the caller (some engines, e.g.
//     veraPDF's `--format xml`, write their entire machine-readable result to
//     stdout rather than a file, D-04/phase 23 -- verified live in
//     scripts/verapdf-measure.sh) and stderr is captured and folded into the
//     returned error for diagnostics.
//
// The caller is expected to pass a ctx carrying the engine timeout. stdout is
// returned even when the process exits non-zero (D-09: some engines use a
// non-zero exit code to report a valid-but-negative result rather than a
// process failure -- e.g. veraPDF exits 1 for a non-compliant-but-valid
// report -- so the caller, not runCommand, decides how to interpret
// exit-code-vs-output).
func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	// 0 == keep everything, which is what every engine but one needs: veraPDF's
	// entire XML report arrives on stdout and is PARSED, so truncating it would
	// corrupt a result rather than trim a diagnostic.
	return runCommandLimited(ctx, 0, name, args...)
}

// runCommandLimited is runCommand with a bound on how much of the child's
// output is retained. The LAST limit bytes are kept, not the first.
//
// # WHY THIS EXISTS
//
// runCommand buffers the child's stdout and stderr in memory with no bound,
// which is fine for an engine fed a document and not fine for one fed a
// PROGRAM. Found in review (PR #16, codex round 2, P1) and then measured, in
// the shipped image at the chart's own 1 GiB:
//
//	235 bytes of .tex  ->  323 565 198 bytes of stdout in 30 seconds
//
// That is 10.8 MB/s from a \message loop, so the 271-second tectonic timeout
// would have to hold ~2.9 GB in a 1 GiB container. The worker is OOM-killed
// long before the deadline can classify anything, the job is left active, and
// the reconciler hands it back to be allocated again.
//
// The tail is the half worth keeping: every parser downstream reads the END of
// the log -- the missing .sty that aborted the run, the biber launch failure,
// and the undefined-reference warning, which is deliberately scoped to the
// final pass. Keeping the head would drop exactly the bytes that name the
// cause.
func runCommandLimited(ctx context.Context, limit int, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout := &tailBuffer{max: limit}
	stderr := &tailBuffer{max: limit}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", name, err)
	}

	// Kill the entire process group on context cancellation/timeout.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-ctx.Done():
		// Negative pid targets the process group led by the child.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done // reap
		return nil, fmt.Errorf("%s killed: %w", name, ctx.Err())
	case err := <-done:
		if err != nil {
			return stdout.Bytes(), fmt.Errorf("%s failed: %w: %s", name, err, stderr.String())
		}
		return stdout.Bytes(), nil
	}
}
