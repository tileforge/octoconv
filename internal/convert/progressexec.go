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

// runCommandStreaming is a byte-for-byte-independent sibling of runCommand
// (exec.go, NEVER modified by this file, D-01a/PROG-05) that additionally
// streams an ffmpeg -progress feed to onLine, one raw "key=value" line at a
// time, while mirroring runCommand's EXACT Setpgid + syscall.Kill(-pid,
// SIGKILL) process-group-kill discipline.
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
func runCommandStreaming(ctx context.Context, onLine func(string), name string, args ...string) ([]byte, error) {
	tmpDir, err := os.MkdirTemp("", "octoconv-progress-*")
	if err != nil {
		return nil, fmt.Errorf("progress tmpdir: %w", err)
	}
	// Server-owned temp dir, never a client-controlled path (T-41-06);
	// removed unconditionally on every return path.
	defer os.RemoveAll(tmpDir)

	fifoPath := filepath.Join(tmpDir, "progress.fifo")
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

	finalArgs := progressStreamArgs(fifoPath, args)
	cmd := exec.Command(name, finalArgs...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // mirrors runCommand exactly

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		rd.Close()
		return nil, fmt.Errorf("start %s: %w", name, err)
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
		// Negative pid targets the process group led by the child.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done // reap, same as runCommand
		// Guaranteed unblock on the never-opened-FIFO path: push a
		// definitive EOF even if ffmpeg's own writer never appeared.
		if w, werr := os.OpenFile(fifoPath, os.O_WRONLY, 0); werr == nil {
			w.Close()
		}
		close(stop)
		<-scanDone // join before returning -- zero leaked goroutines (PROG-05)
		return nil, fmt.Errorf("%s killed: %w", name, ctx.Err())
	case err := <-done:
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
			return stdout.Bytes(), fmt.Errorf("%s failed: %w: %s", name, err, stderr.String())
		}
		return stdout.Bytes(), nil
	}
}
