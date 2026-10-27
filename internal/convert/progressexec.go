package convert

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// progressFIFORetryInterval is the reader goroutine's backoff between
// retries of a transient "no writer yet" EOF (see runCommandStreaming's
// doc comment). Short enough that a real ffmpeg -progress line is never
// delayed noticeably, long enough that the retry loop never busy-spins.
const progressFIFORetryInterval = 5 * time.Millisecond

// progressStreamArgs prepends the "-progress file:<fifoPath>" global option
// ahead of the caller's own argv. Factored out as its own pure function
// (mirrors av.go's argv-builder style, e.g. transcodeToMP4Args) so the
// file-protocol-only transport choice is directly unit-testable without
// spawning any process (T-41-07).
func progressStreamArgs(fifoPath string, args []string) []string {
	return append([]string{"-progress", "file:" + fifoPath}, args...)
}

// runCommandStreaming is a sibling of runCommand (exec.go) that additionally
// streams an ffmpeg -progress feed to onLine, one raw "key=value" line at a
// time, with runCommand's process handling: Setpgid, WaitDelay and
// killGroupAndReap.
//
// Transport: a private named FIFO opened through the already-allowed
// "file:" protocol -- NEVER the fd-based transport ("-progress" pointed at
// an extra file descriptor via cmd.ExtraFiles) sketched in earlier
// research. The pinned hardened octoconv-av-worker:dev ffmpeg build is
// compiled with --enable-protocol='file,crypto' ONLY (Phase 36 D-10's
// CVE-clean pin); pointing "-progress" at that fd-based scheme fails
// "Protocol not found" (exit 8) against that real binary (verified
// in-container -- see 41-02-PLAN.md <transport_correction> and
// 41-RESEARCH.md's TRANSPORT CORRECTION banner). Widening the hardened
// image's protocol allowlist to add that scheme was rejected -- that would
// force a rebuild + CVE re-validation of the pinned build -- so this
// function instead feeds ffmpeg's progress writer through a server-owned
// FIFO on the SAME "file" protocol every other argv builder in this
// package already uses (T-41-07).
//
// The FIFO's own directory is created INSIDE fifoDir (Phase 68 Plan 04,
// T-68-19): fifoDir is the caller's job directory, the ONLY path a confined
// job's ruleset grants write access to -- a directory under the shared
// system temp dir (this function's pre-Phase-68 behavior) would be denied
// to a confined engine under Landlock, and would be visible to other jobs
// sharing that same temp dir even when unconfined. Removed unconditionally
// on every return path.
//
// command is called AFTER the FIFO exists and its read end is already open,
// with fifoPath so the caller can fold "-progress file:<fifoPath>" into the
// engine's OWN argv before any sandbox wrapping happens -- see
// engineExec.stream's doc comment for why that ordering is load-bearing.
// Every error command returns is passed straight back to the caller,
// closing the already-open read end first.
//
// Leak-safety (T-41-04/PROG-05): the read end is opened O_RDONLY|O_NONBLOCK
// BEFORE the child is started, so this function can never hang even if the
// child fails to start or dies before ever touching the FIFO. A FIFO with
// zero writers reports read() as an immediate EOF -- indistinguishable, at
// the syscall level, from "the writer hasn't opened yet" vs. "the writer is
// gone for good" -- so the reader goroutine treats every EOF as transient
// and retries (progressFIFORetryInterval backoff) UNLESS the parent has
// already reaped the child (cmd.Wait() returned) and signaled stop; only
// then does the next EOF end the reader for good. The parent additionally
// pokes the FIFO once after reaping (open the write end, close it
// immediately) so a child that NEVER opened the FIFO at all still resolves
// promptly rather than depending solely on the retry loop's next tick.
// scanDone is joined in BOTH the ctx.Done()/SIGKILL arm and the normal-exit
// arm, before returning -- the same discipline runCommand already applies
// to its own reaper goroutine, extended here to the reader.
func runCommandStreaming(ctx context.Context, onLine func(string), fifoDir, label string, command func(fifoPath string) (name string, args []string, err error)) ([]byte, error) {
	dir, err := os.MkdirTemp(fifoDir, ".progress-")
	if err != nil {
		return nil, fmt.Errorf("progress tmpdir: %w", err)
	}
	// The FIFO's own private directory, always removed on every return path
	// -- never the caller's whole fifoDir, which is the job directory and
	// outlives this one invocation.
	defer os.RemoveAll(dir)

	fifoPath := filepath.Join(dir, "progress.fifo")
	if err := syscall.Mkfifo(fifoPath, 0o600); err != nil {
		return nil, fmt.Errorf("progress fifo: %w", err)
	}

	// Non-blocking read-open: succeeds immediately regardless of whether
	// ffmpeg has opened (or will ever open) its write end -- a blocking
	// open() here would hang forever against a child that never touches
	// the FIFO.
	rd, err := os.OpenFile(fifoPath, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("progress fifo open: %w", err)
	}

	name, args, err := command(fifoPath)
	if err != nil {
		rd.Close()
		return nil, err
	}

	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // mirrors runCommand exactly
	cmd.WaitDelay = engineWaitDelay

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		rd.Close()
		return nil, fmt.Errorf("start %s: %w", label, err)
	}

	// stop tells the reader "the child has been reaped -- the NEXT EOF is
	// final, stop retrying." Closed by the parent, exactly once, in
	// whichever select arm below runs.
	stop := make(chan struct{})
	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)
		defer rd.Close()
		br := bufio.NewReader(rd)
		for {
			line, rerr := br.ReadString('\n')
			if line != "" {
				onLine(strings.TrimRight(line, "\r\n"))
			}
			if rerr == nil {
				continue
			}
			// Both a "zero writers" EOF (ffmpeg hasn't opened its end
			// yet, or momentarily has zero writers) AND an EAGAIN
			// (a writer IS connected but has not written anything
			// since our last read) are transient "no data yet"
			// signals on a non-blocking FIFO read -- neither means
			// end-of-stream. Only stop's closure (the parent has
			// reaped the child) makes the NEXT such signal final.
			if rerr == io.EOF || errors.Is(rerr, syscall.EAGAIN) {
				select {
				case <-stop:
					return // definitive: child reaped, nothing more will ever come
				default:
					time.Sleep(progressFIFORetryInterval)
					continue
				}
			}
			// An unexpected read error (neither of the above) -- give
			// up rather than spin forever on it.
			return
		}
	}()

	// Kill the entire process group on context cancellation/timeout --
	// identical to runCommand.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-ctx.Done():
		killGroupAndReap(cmd.Process.Pid, done) // reap, same as runCommand
		// Guaranteed unblock on the never-opened-FIFO path: push a
		// definitive EOF even if ffmpeg's own writer never appeared.
		if w, werr := os.OpenFile(fifoPath, os.O_WRONLY, 0); werr == nil {
			w.Close()
		}
		close(stop)
		<-scanDone // join before returning -- zero leaked goroutines (PROG-05)
		return nil, fmt.Errorf("%s killed: %w", label, ctx.Err())
	case err := <-done:
		if err != nil {
			killGroupAndReap(cmd.Process.Pid, nil)
		}
		// Same guaranteed-unblock poke on the normal-exit path: ffmpeg
		// may have exited having never opened the FIFO (e.g. an early
		// argument-parse failure) just as validly as on the kill path.
		if w, werr := os.OpenFile(fifoPath, os.O_WRONLY, 0); werr == nil {
			w.Close()
		}
		close(stop)
		<-scanDone // join BEFORE returning, so the caller's terminal read
		// of onLine's side effects never races a still-in-flight line
		if err != nil {
			return stdout.Bytes(), fmt.Errorf("%s failed: %w: %s", label, err, stderr.String())
		}
		return stdout.Bytes(), nil
	}
}

// stream runs name confined under e's ruleset (or directly, for an
// unconfined engineExec), streaming its -progress feed to onLine via a FIFO
// created inside fifoDir -- the caller's job directory (Phase 68 Plan 04).
//
// The "-progress file:<fifoPath>" option is folded into the ENGINE's own
// argv (progressStreamArgs) BEFORE wrapEngine wraps the whole thing for the
// helper -- never the other way around. Helper mode is recognised solely by
// argv[1] being the exec-mode marker (internal/sandbox/execmode.go,
// RunExecModeIfRequested): wrapping first and prepending "-progress"
// afterward would push it in front of "__sandbox-exec", and the worker
// binary would try to start a second worker process instead of exec'ing
// ffmpeg. T-68-18.
func (e engineExec) stream(ctx context.Context, onLine func(string), fifoDir, name string, args []string) ([]byte, error) {
	// A per-exec environment (html's own replaced-environment shape) is not
	// wired through runCommandStreaming at all -- refusing loudly here means
	// a future streaming class that also needs one fails fast at the call
	// site instead of silently running with the wrong (inherited) environment.
	if e.env != nil {
		return nil, fmt.Errorf("engineExec.stream: a per-exec environment is not supported")
	}
	out, err := runCommandStreaming(ctx, onLine, fifoDir, name, func(fifoPath string) (string, []string, error) {
		return e.sandbox.wrapEngine(name, e.spec, progressStreamArgs(fifoPath, args))
	})
	return out, e.sandboxFault(err)
}
