//go:build linux

package convert

import (
	"errors"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// startDevShmObserver watches dir for entries CREATED in it (IN_CREATE and
// IN_MOVED_TO) from now until the returned drain function is called, and
// drain returns their names. It runs in the test process, OUTSIDE any
// Landlock ruleset, so what it reports is what the engine did to the real
// directory, not what the engine says it did (70-06-PLAN.md Task 1.2).
//
// An IN_Q_OVERFLOW means events were lost, so it counts as activity: an
// observer that silently dropped creates would report the clean result this
// check exists to refuse.
//
// Start it before the engine run and drain it after the engine has exited;
// the kernel queues the events in between. drain closes the watch and is
// safe to call once.
func startDevShmObserver(t *testing.T, dir string) (drain func() []string) {
	t.Helper()
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		t.Fatalf("inotify_init1: %v", err)
	}
	if _, err := unix.InotifyAddWatch(fd, dir, unix.IN_CREATE|unix.IN_MOVED_TO); err != nil {
		_ = unix.Close(fd)
		t.Fatalf("inotify_add_watch %s: %v", dir, err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = unix.Close(fd)
		}
	})

	return func() []string {
		t.Helper()
		var names []string
		buf := make([]byte, 64*1024)
		for {
			n, err := unix.Read(fd, buf)
			if err != nil {
				if errors.Is(err, unix.EINTR) {
					continue
				}
				if !errors.Is(err, unix.EAGAIN) {
					t.Fatalf("read inotify events on %s: %v", dir, err)
				}
				break
			}
			if n == 0 {
				break
			}
			for off := 0; off+unix.SizeofInotifyEvent <= n; {
				ev := (*unix.InotifyEvent)(unsafe.Pointer(&buf[off]))
				nameLen := int(ev.Len)
				if ev.Mask&unix.IN_Q_OVERFLOW != 0 {
					names = append(names, "<IN_Q_OVERFLOW>")
				} else if nameLen > 0 {
					raw := buf[off+unix.SizeofInotifyEvent : off+unix.SizeofInotifyEvent+nameLen]
					for i, c := range raw {
						if c == 0 {
							raw = raw[:i]
							break
						}
					}
					names = append(names, string(raw))
				}
				off += unix.SizeofInotifyEvent + nameLen
			}
		}
		closed = true
		_ = unix.Close(fd)
		return names
	}
}
