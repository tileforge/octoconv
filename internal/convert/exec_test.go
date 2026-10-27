package convert

import (
	"context"
	"encoding/json"
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

func TestKillTestReturnBoundCoversTheRunnerBounds(t *testing.T) {
	if want := engineWaitDelay + groupKillBudget + time.Second; killTestReturnBound < want {
		t.Errorf("killTestReturnBound = %s, want at least engineWaitDelay + groupKillBudget + 1s = %s", killTestReturnBound, want)
	}
	if groupKillInterval <= 0 || groupKillInterval >= groupKillBudget {
		t.Errorf("groupKillInterval = %s, want > 0 and < groupKillBudget (%s)", groupKillInterval, groupKillBudget)
	}
}

type killTestCall struct{ start, end time.Time }

type killTestFakeKill struct {
	t     *testing.T
	calls []killTestCall
	reply func(i int) error
	block func(i int) time.Duration
}

func (f *killTestFakeKill) kill(pid int, sig syscall.Signal) error {
	if pid != -(1<<30) || sig != syscall.SIGKILL {
		f.t.Errorf("kill(%d, %v), want kill(%d, SIGKILL)", pid, sig, -(1 << 30))
	}
	i := len(f.calls)
	c := killTestCall{start: time.Now()}
	if f.block != nil {
		time.Sleep(f.block(i))
	}
	c.end = time.Now()
	f.calls = append(f.calls, c)
	if f.reply != nil {
		return f.reply(i)
	}
	return nil
}

func killTestRunLoop(t *testing.T, f *killTestFakeKill, done <-chan error) (before time.Time, took time.Duration) {
	t.Helper()
	returned := make(chan struct{})
	before = time.Now()
	go func() {
		defer close(returned)
		killGroupWith(f.kill, 1<<30, done)
	}()
	select {
	case <-returned:
	case <-time.After(5 * groupKillBudget):
		t.Fatalf("killGroupWith did not return within %s", 5*groupKillBudget)
	}
	return before, time.Since(before)
}

func TestKillGroupLoopIssuesAKillAfterASlowOne(t *testing.T) {
	t.Run("slow kill", func(t *testing.T) {
		f := &killTestFakeKill{t: t, block: func(i int) time.Duration {
			if i == 0 {
				return groupKillBudget * 3 / 2
			}
			return 0
		}}
		before, _ := killTestRunLoop(t, f, nil)
		if len(f.calls) != 2 {
			t.Fatalf("kill was called %d times, want 2: after a kill that blocked past the budget, one more kill must follow", len(f.calls))
		}
		if f.calls[1].start.Before(f.calls[0].end) {
			t.Errorf("the second kill started %s before the first returned", f.calls[0].end.Sub(f.calls[1].start))
		}
		if f.calls[1].start.Before(before.Add(groupKillBudget)) {
			t.Errorf("the last kill started %s after the loop began, want after the %s budget", f.calls[1].start.Sub(before), groupKillBudget)
		}
	})
	t.Run("esrch", func(t *testing.T) {
		f := &killTestFakeKill{t: t, reply: func(i int) error {
			if i == 2 {
				return syscall.ESRCH
			}
			return nil
		}}
		_, took := killTestRunLoop(t, f, nil)
		if len(f.calls) != 3 {
			t.Fatalf("kill was called %d times, want 3: the loop ends on the first ESRCH", len(f.calls))
		}
		if took >= groupKillBudget {
			t.Errorf("the loop took %s, want less than the %s budget", took, groupKillBudget)
		}
	})
	t.Run("never gone", func(t *testing.T) {
		f := &killTestFakeKill{t: t}
		before, took := killTestRunLoop(t, f, nil)
		if took < groupKillBudget || took > groupKillBudget+200*time.Millisecond {
			t.Errorf("the loop took %s, want between %s and %s", took, groupKillBudget, groupKillBudget+200*time.Millisecond)
		}
		if n := len(f.calls); n == 0 || f.calls[n-1].start.Before(before.Add(groupKillBudget)) {
			t.Errorf("the last of %d kills did not start after the %s budget", n, groupKillBudget)
		}
	})
	t.Run("done first", func(t *testing.T) {
		done := make(chan error, 1)
		done <- nil
		f := &killTestFakeKill{t: t, reply: func(i int) error {
			if i == 2 {
				return syscall.ESRCH
			}
			return nil
		}}
		_, took := killTestRunLoop(t, f, done)
		if len(f.calls) != 3 {
			t.Fatalf("kill was called %d times, want 3: done must not end the loop, ESRCH must", len(f.calls))
		}
		if len(done) != 0 {
			t.Error("done was never read")
		}
		if took >= groupKillBudget {
			t.Errorf("the loop took %s, want less than the %s budget", took, groupKillBudget)
		}
	})
}

func killTestErrKind(err error) string {
	var ee *exec.ExitError
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, exec.ErrWaitDelay):
		return "waitdelay"
	case errors.As(err, &ee):
		return fmt.Sprintf("exit:%d", ee.ExitCode())
	default:
		return "other"
	}
}

func killTestArgvEnv(t *testing.T, name string) []string {
	t.Helper()
	raw := os.Getenv(name)
	var argv []string
	if err := json.Unmarshal([]byte(raw), &argv); err != nil || len(argv) == 0 {
		t.Fatalf("%s = %q: want a non-empty JSON array of strings (%v)", name, raw, err)
	}
	return argv
}

func TestRunnerReturnLatencyLive(t *testing.T) {
	if os.Getenv("OCTOCONV_RUNNER_LATENCY_LIVE") != "1" {
		t.Skip("set OCTOCONV_RUNNER_LATENCY_LIVE=1 and OCTOCONV_RUNNER_LATENCY_{KILL,ERROR,OK}_ARGV to measure runner return latency")
	}
	var killAfter time.Duration
	if v := os.Getenv("OCTOCONV_RUNNER_LATENCY_KILL_AFTER"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			t.Fatalf("OCTOCONV_RUNNER_LATENCY_KILL_AFTER = %q: %v", v, err)
		}
		killAfter = d
	}
	killArgv := killTestArgvEnv(t, "OCTOCONV_RUNNER_LATENCY_KILL_ARGV")
	okArgv := killTestArgvEnv(t, "OCTOCONV_RUNNER_LATENCY_OK_ARGV")
	var errArgv []string
	errNone := ""
	if raw := os.Getenv("OCTOCONV_RUNNER_LATENCY_ERROR_ARGV"); strings.HasPrefix(raw, "none:") {
		errNone = strings.TrimPrefix(raw, "none:")
	} else {
		errArgv = killTestArgvEnv(t, "OCTOCONV_RUNNER_LATENCY_ERROR_ARGV")
	}
	bound := engineWaitDelay + groupKillBudget + 500*time.Millisecond

	for _, c := range []struct {
		name string
		argv []string
	}{{"kill", killArgv}, {"error", errArgv}, {"ok", okArgv}} {
		t.Run(c.name, func(t *testing.T) {
			if c.name == "error" && errNone != "" {
				t.Logf("RUNNERLAT case=error none reason=%s", errNone)
				return
			}
			pidFile := filepath.Join(t.TempDir(), "leader.pid")
			args := append([]string{"-c", `echo $$ > "$0"; exec "$@"`, pidFile}, c.argv...)

			var ctx context.Context
			var cancel context.CancelFunc
			if c.name == "kill" {
				ctx, cancel = context.WithCancel(context.Background())
			} else {
				ctx, cancel = context.WithTimeout(context.Background(), 10*time.Minute)
			}
			defer cancel()

			ret := make(chan error, 1)
			launched := time.Now()
			go func() {
				_, _, err := runCommandCore(ctx, 0, nil, "engine", "/bin/sh", args...)
				ret <- err
			}()
			leader := killTestWaitPid(t, pidFile, 30*time.Second)
			if leader == 0 {
				cancel()
				killTestDrain(t, ret, "runCommandCore")
				t.Fatal("the wrapper never wrote the leader's pid")
			}
			exited := make(chan time.Time, 1)
			go func(lastAlive time.Time) {
				for end := time.Now().Add(15 * time.Minute); time.Now().Before(end); time.Sleep(time.Millisecond) {
					probed := time.Now()
					if err := syscall.Kill(leader, 0); errors.Is(err, syscall.ESRCH) {
						exited <- lastAlive
						return
					}
					lastAlive = probed
				}
			}(launched)

			late, aliveAtCancel := "na", "na"
			var cancelled time.Time
			if c.name == "kill" {
				time.Sleep(killAfter)
				aliveAtCancel = strconv.FormatBool(syscall.Kill(leader, 0) == nil)
				cancelled = time.Now()
				cancel()
			}
			err := <-ret
			returned := time.Now()
			if c.name == "kill" {
				late = strconv.FormatInt(returned.Sub(cancelled).Milliseconds(), 10)
			}
			afterExit := int64(-1)
			select {
			case at := <-exited:
				afterExit = returned.Sub(at).Milliseconds()
			case <-time.After(2 * time.Second):
			}

			var live, zombies []string
			for end := time.Now().Add(2 * time.Second); ; time.Sleep(5 * time.Millisecond) {
				live, zombies = killTestGroupMembers(leader)
				if len(live) == 0 || time.Now().After(end) {
					break
				}
			}
			waitDelay := 0
			if errors.Is(err, exec.ErrWaitDelay) {
				waitDelay = 1
			}
			t.Logf("RUNNERLAT case=%s after_leader_exit_ms=%d late_ms=%s leader_alive_at_cancel=%s err=%s waitdelay=%d live_after=%d zombies_after=%d",
				c.name, afterExit, late, aliveAtCancel, killTestErrKind(err), waitDelay, len(live), len(zombies))
			if len(live) > 0 {
				killTestSweep(leader)
				t.Fatalf("group %d still has live members 2s after the runner returned: %v", leader, live)
			}

			switch c.name {
			case "kill":
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("err = %v, want context.Canceled", err)
				}
				if d := returned.Sub(cancelled); d > bound {
					t.Fatalf("returned %s after cancel, want at most %s", d, bound)
				}
			case "error":
				if err == nil {
					t.Fatal("err = nil, want the engine to fail")
				}
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("err = %v, want an engine failure, not a cancellation", err)
				}
				if afterExit < 0 || time.Duration(afterExit)*time.Millisecond > bound {
					t.Fatalf("returned %d ms after the leader exited, want at most %s", afterExit, bound)
				}
			case "ok":
				if err != nil {
					t.Fatalf("err = %v (ErrWaitDelay: %t), want nil: the success path regressed", err, errors.Is(err, exec.ErrWaitDelay))
				}
			}
		})
	}
}
