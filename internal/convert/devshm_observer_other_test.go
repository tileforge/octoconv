//go:build !linux

package convert

import "testing"

// startDevShmObserver is the non-Linux stand-in for the inotify observer in
// devshm_observer_linux_test.go, so the package's tests compile on a
// developer's machine. The only caller, TestChromiumLargeDocumentNeedsNoDevShmLive,
// skips off the built chromium-worker image before it reaches this.
func startDevShmObserver(t *testing.T, dir string) (drain func() []string) {
	t.Helper()
	t.Fatalf("the /dev/shm observer needs inotify and runs only on linux (watching %s)", dir)
	return nil
}
