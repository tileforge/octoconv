package convert

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	killTestReturnBound      = 3 * time.Second
	killTestForkRaceAttempts = 10
)

type killTestRunner struct {
	name string
	run  func(ctx context.Context, dir string, args []string) error
}

func killTestRunners() []killTestRunner {
	return []killTestRunner{
		{
			name: "runCommandCore",
			run: func(ctx context.Context, dir string, args []string) error {
				_, _, err := runCommandCore(ctx, 0, nil, "forker", "/bin/sh", args...)
				return err
			},
		},
		{
			name: "runCommandStreaming",
			run: func(ctx context.Context, dir string, args []string) error {
				_, err := runCommandStreaming(ctx, func(string) {}, dir, "forker", func(string) (string, []string, error) {
					return "/bin/sh", args, nil
				})
				return err
			},
		},
	}
}

func killTestStart(r killTestRunner, ctx context.Context, dir string, args []string) <-chan error {
	ret := make(chan error, 1)
	go func() { ret <- r.run(ctx, dir, args) }()
	return ret
}

func killTestWaitPid(t *testing.T, path string, within time.Duration) int {
	t.Helper()
	for end := time.Now().Add(within); time.Now().Before(end); time.Sleep(time.Millisecond) {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if p, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && p > 0 {
			return p
		}
	}
	return 0
}

func killTestSweep(pgid int) {
	end := time.Now().Add(5 * time.Second)
	lastCheck := time.Now()
	for time.Now().Before(end) {
		if err := syscall.Kill(-pgid, syscall.SIGKILL); errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Since(lastCheck) >= 100*time.Millisecond {
			if len(killTestLiveMembers(pgid)) == 0 {
				return
			}
			lastCheck = time.Now()
		}
		time.Sleep(time.Millisecond)
	}
}

func killTestGroupMembers(pgid int) (live, zombie []string) {
	if err := syscall.Kill(-pgid, 0); errors.Is(err, syscall.ESRCH) {
		return nil, nil
	}
	if _, err := os.Stat("/proc/self/stat"); err == nil {
		paths, _ := filepath.Glob("/proc/[0-9]*/stat")
		for _, p := range paths {
			data, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			s := string(data)
			i := strings.LastIndexByte(s, ')')
			if i < 0 {
				continue
			}
			fields := strings.Fields(s[i+1:])
			if len(fields) < 3 {
				continue
			}
			if g, err := strconv.Atoi(fields[2]); err != nil || g != pgid {
				continue
			}
			entry := fmt.Sprintf("%s state=%s ppid=%s", strings.Fields(s)[0], fields[0], fields[1])
			if fields[0] == "Z" || fields[0] == "X" {
				zombie = append(zombie, entry)
			} else {
				live = append(live, entry)
			}
		}
		return live, zombie
	}
	out, err := exec.Command("/bin/ps", "-A", "-o", "pid=,pgid=,stat=").Output()
	if err != nil {
		return []string{fmt.Sprintf("ps failed: %v", err)}, nil
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		if g, err := strconv.Atoi(fields[1]); err != nil || g != pgid {
			continue
		}
		entry := fmt.Sprintf("%s stat=%s", fields[0], fields[2])
		if strings.HasPrefix(fields[2], "Z") {
			zombie = append(zombie, entry)
		} else {
			live = append(live, entry)
		}
	}
	return live, zombie
}

func killTestLiveMembers(pgid int) []string {
	live, _ := killTestGroupMembers(pgid)
	return live
}

func killTestProbe(pgid int) string {
	start := time.Now()
	_ = syscall.Kill(-pgid, 0)
	took := time.Since(start)
	if took > 500*time.Millisecond {
		return fmt.Sprintf("%s; host kill() stall, see Pitfall 2", took)
	}
	return took.String()
}

func killTestRequireGroupGone(t *testing.T, pgid int, within time.Duration, what string) {
	t.Helper()
	end := time.Now().Add(within)
	for {
		live := killTestLiveMembers(pgid)
		if len(live) == 0 {
			return
		}
		if time.Now().After(end) {
			killTestSweep(pgid)
			t.Fatalf("%s: group %d still has live members %s after the runner returned: %v", what, pgid, within, live)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func killTestDrain(t *testing.T, ret <-chan error, name string) {
	t.Helper()
	select {
	case <-ret:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s did not return even after the sweep", name)
	}
}

func TestRunnersKillAChildForkedDuringTheGroupKill(t *testing.T) {
	const script = `f="${1%/*}/never-written"; echo $$ > "$1"; while :; do (read _ < "$f") & kill $!; done`
	for _, r := range killTestRunners() {
		t.Run(r.name, func(t *testing.T) {
			for attempt := 0; attempt < killTestForkRaceAttempts; attempt++ {
				dir := t.TempDir()
				if err := syscall.Mkfifo(filepath.Join(dir, "never-written"), 0o600); err != nil {
					t.Fatalf("mkfifo: %v", err)
				}
				pidFile := filepath.Join(dir, "leader.pid")
				args := []string{"-c", script, "sh", pidFile}

				ctx, cancel := context.WithCancel(context.Background())
				ret := killTestStart(r, ctx, dir, args)
				pgid := killTestWaitPid(t, pidFile, 5*time.Second)
				if pgid == 0 {
					cancel()
					killTestDrain(t, ret, r.name)
					t.Fatalf("attempt %d: the forker never wrote its pid", attempt)
				}
				time.Sleep(20 * time.Millisecond)
				cancelled := time.Now()
				cancel()

				select {
				case err := <-ret:
					took := time.Since(cancelled)
					if !errors.Is(err, context.Canceled) {
						killTestSweep(pgid)
						t.Fatalf("attempt %d: err = %v, want context.Canceled", attempt, err)
					}
					if !strings.HasPrefix(err.Error(), "forker killed: ") {
						killTestSweep(pgid)
						t.Fatalf("attempt %d: err = %q, want the \"forker killed: \" prefix", attempt, err)
					}
					killTestRequireGroupGone(t, pgid, 2*time.Second, fmt.Sprintf("attempt %d", attempt))
					killTestSweep(pgid)
					t.Logf("attempt %d: returned %s after cancel", attempt, took)
				case <-time.After(killTestReturnBound):
					probe := killTestProbe(pgid)
					t.Logf("attempt %d: live members of group %d: %v", attempt, pgid, killTestLiveMembers(pgid))
					killTestSweep(pgid)
					killTestDrain(t, ret, r.name)
					t.Fatalf("attempt %d: %s still blocked %s after cancel: a child forked during the group kill holds its pipes (probe kill took %s)", attempt, r.name, killTestReturnBound, probe)
				}
			}
		})
	}
}

func TestRunnersReturnWhenADescendantOutsideTheGroupHoldsThePipes(t *testing.T) {
	if _, err := exec.LookPath("perl"); err != nil {
		t.Fatalf("perl is required to move a descendant out of the group: %v", err)
	}
	const script = `echo $$ > "$2"; perl -e 'setpgrp(0,0); open(my $f, ">", $ARGV[0]) or die; print $f $$; close $f; exec "sleep", "30"' "$1" & wait`
	for _, r := range killTestRunners() {
		t.Run(r.name, func(t *testing.T) {
			dir := t.TempDir()
			escFile := filepath.Join(dir, "escapee.pid")
			leaderFile := filepath.Join(dir, "leader.pid")
			args := []string{"-c", script, "sh", escFile, leaderFile}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ret := killTestStart(r, ctx, dir, args)
			esc := killTestWaitPid(t, escFile, 5*time.Second)
			pgid := killTestWaitPid(t, leaderFile, 5*time.Second)
			if esc == 0 || pgid == 0 {
				cancel()
				if esc != 0 {
					killTestSweep(esc)
				}
				killTestDrain(t, ret, r.name)
				t.Fatalf("pids not written: escapee %d, leader %d", esc, pgid)
			}
			t.Cleanup(func() { killTestSweep(esc) })
			cancelled := time.Now()
			cancel()

			select {
			case err := <-ret:
				took := time.Since(cancelled)
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("err = %v, want context.Canceled", err)
				}
				if !strings.HasPrefix(err.Error(), "forker killed: ") {
					t.Fatalf("err = %q, want the \"forker killed: \" prefix", err)
				}
				killTestRequireGroupGone(t, pgid, 2*time.Second, "leader group")
				t.Logf("returned %s after cancel", took)
			case <-time.After(killTestReturnBound):
				probe := killTestProbe(pgid)
				killTestSweep(esc)
				killTestDrain(t, ret, r.name)
				t.Fatalf("%s still blocked %s after cancel: a descendant outside the group holds its pipes (probe kill took %s)", r.name, killTestReturnBound, probe)
			}
		})
	}
}

func TestRunnersEndADescendantHoldingThePipesAfterTheLeaderExits(t *testing.T) {
	for _, r := range killTestRunners() {
		t.Run(r.name, func(t *testing.T) {
			for _, code := range []int{0, 3} {
				t.Run(fmt.Sprintf("exit %d", code), func(t *testing.T) {
					dir := t.TempDir()
					pidFile := filepath.Join(dir, "leader.pid")
					args := []string{"-c", fmt.Sprintf(`echo $$ > "$1"; sleep 30 & exit %d`, code), "sh", pidFile}

					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					started := time.Now()
					ret := killTestStart(r, ctx, dir, args)
					pgid := killTestWaitPid(t, pidFile, 5*time.Second)
					if pgid == 0 {
						cancel()
						killTestDrain(t, ret, r.name)
						t.Fatal("the leader never wrote its pid")
					}

					var err error
					select {
					case err = <-ret:
					case <-time.After(killTestReturnBound):
						probe := killTestProbe(pgid)
						killTestSweep(pgid)
						killTestDrain(t, ret, r.name)
						t.Fatalf("leader exited %d while a descendant in its group holds the pipes: %s still blocked %s (probe kill took %s)", code, r.name, killTestReturnBound, probe)
					}
					took := time.Since(started)
					if err == nil {
						killTestSweep(pgid)
						t.Fatal("err = nil, want a failure: the pipes were closed under a descendant still holding them")
					}
					if !strings.HasPrefix(err.Error(), "forker failed: ") {
						killTestSweep(pgid)
						t.Fatalf("err = %q, want the \"forker failed: \" prefix", err)
					}
					if errors.Is(err, context.Canceled) {
						killTestSweep(pgid)
						t.Fatalf("err = %v, want no context.Canceled: nothing cancelled", err)
					}
					if code == 0 && !errors.Is(err, exec.ErrWaitDelay) {
						killTestSweep(pgid)
						t.Fatalf("err = %v, want exec.ErrWaitDelay", err)
					}
					if code == 3 {
						var ee *exec.ExitError
						if !errors.As(err, &ee) || ee.ExitCode() != 3 {
							killTestSweep(pgid)
							t.Fatalf("err = %v, want an *exec.ExitError with code 3", err)
						}
					}
					killTestRequireGroupGone(t, pgid, 2*time.Second, "descendant in the leader's group")
					t.Logf("returned %s after start: %v", took, err)
				})
			}
		})
	}
}
