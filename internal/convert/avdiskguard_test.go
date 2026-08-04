package convert

import (
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

// statfsFreeBytes reads dir's real available-to-unprivileged-user free
// space via the same unix.Statfs call EnforceMinFreeDisk itself uses, so
// tests can construct exact-boundary and guaranteed-insufficient cases
// without assuming any specific free-space number for this host.
func statfsFreeBytes(t *testing.T, dir string) uint64 {
	t.Helper()
	var stat unix.Statfs_t
	if err := unix.Statfs(dir, &stat); err != nil {
		t.Fatalf("statfs %q: %v", dir, err)
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize)
}

// TestEnforceMinFreeDisk pins the fail-closed behaviors from the plan's
// <behavior> block: success at/above the threshold, failure below it, the
// exact-boundary edge against the pure decision helper (free == needed is
// NOT an error), one byte below that boundary (IS an error, pinning `<` vs
// `<=`), the zero-free degenerate cases, a statfs error on a bogus path
// (distinguishable from the insufficient-space sentinel), and errors.Is
// matching on the insufficient-space path.
func TestEnforceMinFreeDisk(t *testing.T) {
	dir := t.TempDir()
	free := statfsFreeBytes(t, dir)

	t.Run("sufficient space passes", func(t *testing.T) {
		// A tiny input size relative to any real filesystem's free space --
		// stable on any host, never assumes a specific free-space number.
		if err := EnforceMinFreeDisk(dir, 1, 3.0); err != nil {
			t.Fatalf("EnforceMinFreeDisk(dir, 1, 3.0) = %v, want nil", err)
		}
	})

	t.Run("insufficient space fails closed", func(t *testing.T) {
		if free == 0 {
			t.Skip("host reports zero free bytes; cannot construct a guaranteed-insufficient case")
		}
		// safetyFactor 2.0 against an input sized to the ENTIRE free space
		// guarantees needed (2x free) exceeds free, regardless of how much
		// free space this host happens to report.
		err := EnforceMinFreeDisk(dir, int64(free), 2.0)
		if err == nil {
			t.Fatal("EnforceMinFreeDisk(dir, free, 2.0) = nil, want error")
		}
		if !errors.Is(err, ErrAVInsufficientDiskSpace) {
			t.Errorf("error = %v, want errors.Is(err, ErrAVInsufficientDiskSpace)", err)
		}
	})

	t.Run("exact boundary passes", func(t *testing.T) {
		// Previously this sampled free via its own unix.Statfs call and passed
		// it into EnforceMinFreeDisk, which performs its OWN unix.Statfs
		// moments later and compares that fresh reading against the stale
		// sample. Any concurrent writer -- and this suite runs against a live
		// Postgres + MinIO Docker stack, both writing continuously -- can
		// shrink free space between the two syscalls; observed live at an
		// 8192-byte (2-block) gap, failing about 1 run in 5 under load. This
		// was never an exact boundary, it was two independent syscalls racing
		// each other. Calling the pure decision helper directly with a
		// test-owned constant removes the filesystem, and the race, entirely:
		// needed == inputSizeBytes * safetyFactor == const * 1.0 == const, so
		// free == needed holds by construction, not by two syscalls agreeing.
		const constFree = uint64(1) << 30 // 1 GiB, arbitrary and test-owned
		if err := checkFreeAgainstNeeded(constFree, int64(constFree), 1.0); err != nil {
			t.Fatalf("checkFreeAgainstNeeded at exact boundary (free==needed) = %v, want nil", err)
		}
	})

	t.Run("one byte below boundary fails closed", func(t *testing.T) {
		// Pins the comparison as strict (`free < needed`, never `<=`). Without
		// this case, the exact-boundary case alone cannot distinguish the two:
		// both a `<` and a `<=` guard would pass free==needed.
		const constFree = uint64(1) << 30 // 1 GiB, arbitrary and test-owned
		err := checkFreeAgainstNeeded(constFree-1, int64(constFree), 1.0)
		if err == nil {
			t.Fatal("checkFreeAgainstNeeded(free-1, ...) = nil, want error")
		}
		if !errors.Is(err, ErrAVInsufficientDiskSpace) {
			t.Errorf("error = %v, want errors.Is(err, ErrAVInsufficientDiskSpace)", err)
		}
	})

	t.Run("comfortably above boundary passes", func(t *testing.T) {
		if err := checkFreeAgainstNeeded(1<<30, 1, 1.0); err != nil {
			t.Fatalf("checkFreeAgainstNeeded(1GiB, 1, 1.0) = %v, want nil", err)
		}
	})

	t.Run("zero free with zero needed passes (degenerate equality)", func(t *testing.T) {
		if err := checkFreeAgainstNeeded(0, 0, 1.0); err != nil {
			t.Fatalf("checkFreeAgainstNeeded(0, 0, 1.0) = %v, want nil", err)
		}
	})

	t.Run("zero free with positive needed fails closed", func(t *testing.T) {
		err := checkFreeAgainstNeeded(0, 1, 1.0)
		if !errors.Is(err, ErrAVInsufficientDiskSpace) {
			t.Errorf("checkFreeAgainstNeeded(0, 1, 1.0) = %v, want errors.Is(err, ErrAVInsufficientDiskSpace)", err)
		}
	})

	t.Run("insufficient-space error text carries free, needed, input and factor", func(t *testing.T) {
		err := checkFreeAgainstNeeded(10, 20, 2.0)
		if err == nil {
			t.Fatal("checkFreeAgainstNeeded(10, 20, 2.0) = nil, want error")
		}
		want := "av: insufficient free disk space: free=10 needed=40 input=20 factor=2.00"
		if err.Error() != want {
			t.Errorf("checkFreeAgainstNeeded(10, 20, 2.0).Error() = %q, want %q", err.Error(), want)
		}
	})

	t.Run("statfs error on bogus path is not the insufficient-space sentinel", func(t *testing.T) {
		err := EnforceMinFreeDisk("/nonexistent/bogus/path/for/avdiskguard/test", 1, 1.0)
		if err == nil {
			t.Fatal("EnforceMinFreeDisk on a nonexistent path = nil, want a statfs error")
		}
		if errors.Is(err, ErrAVInsufficientDiskSpace) {
			t.Errorf("statfs error on a bogus path = %v, want a plain wrapped statfs error, NOT ErrAVInsufficientDiskSpace", err)
		}
	})

	t.Run("errors.Is matches sentinel on the insufficient-space path", func(t *testing.T) {
		if free == 0 {
			t.Skip("host reports zero free bytes; cannot construct a guaranteed-insufficient case")
		}
		// Ask for a petabyte rather than free+1. `free` is sampled once at the
		// top of this test, but the host keeps writing between that sample and
		// this call, so free+1 can quietly become satisfiable and the guard
		// correctly returns nil -- a flake that has nothing to do with the code
		// under test. A demand no real filesystem can meet removes the race.
		const unsatisfiable = int64(1) << 50 // 1 PiB
		err := EnforceMinFreeDisk(dir, unsatisfiable, 1.0)
		if !errors.Is(err, ErrAVInsufficientDiskSpace) {
			t.Errorf("errors.Is(err, ErrAVInsufficientDiskSpace) = false for err=%v, want true", err)
		}
	})
}
