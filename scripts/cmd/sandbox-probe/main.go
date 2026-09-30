// Command sandbox-probe applies a candidate Landlock ruleset and then execs a
// command under it, so a proposed path set can be PROVEN sufficient rather
// than argued to be.
//
// It exists because the obvious way to discover what an engine needs --
// tracing openat -- under-reports in a way the trace itself cannot show. The
// engine's own executable is never opened, it is execve'd, so the first set
// measured for libvips looked complete and every conversion failed with
// "exec: permission denied" the moment a real ruleset was built from it. A
// path set is a hypothesis until a real engine runs a real job under it.
//
// PROBE_RW, PROBE_SOCK and PROBE_RO are colon-separated path lists (PROBE_SOCK
// names the directories where the engine may create a Unix-domain socket and do
// nothing else -- the sock: grant, for an engine whose single-instance IPC binds
// one; leave it unset for every other engine). Before exec, the probe
// reads a file outside every grant and refuses to continue if it succeeds:
// a ruleset that permits everything would otherwise report every engine as
// happily confined.
//
// Not shipped. It lives under scripts/cmd/ beside texpackages for the same
// reason: it is a tool for measuring this repository, not a service binary.
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"syscall"

	"github.com/apaderin/octoconv/internal/sandbox"
)

// defaultCanary is read after the ruleset is established and must be denied.
//
// Overridable through PROBE_CANARY because a fixed path stops being a canary
// the moment an engine legitimately needs the directory holding it: chromium
// reads /etc/fonts, /etc/hosts and /etc/resolv.conf, and a first attempt that
// granted /etc wholesale made /etc/hostname readable -- at which point this
// check cannot tell a confined engine from an unconfined one. It refused to
// continue, which is the behaviour wanted, but the fix is to let the caller
// name a path outside its own set.
const defaultCanary = "/etc/hostname"

func main() {
	canary := os.Getenv("PROBE_CANARY")
	if canary == "" {
		canary = defaultCanary
	}

	// BEFORE the ruleset, and before it for a reason: an error afterwards is
	// only evidence of confinement if the read would have worked without it.
	// A mistyped PROBE_CANARY, a directory, or a file this user cannot read
	// anyway all fail for reasons Landlock had no part in, and taking that for
	// proof makes every result after it unsound (codex, PR #27).
	if _, err := os.ReadFile(canary); err != nil {
		fmt.Fprintf(os.Stderr, "canary %s is not readable before the ruleset is applied (%v), so "+
			"its failure afterwards would prove nothing. Name a regular file this user can read "+
			"and no grant covers.\n", canary, err)
		os.Exit(7)
	}

	if err := sandbox.Apply(sandbox.Paths{
		ReadWrite: splitPaths(os.Getenv("PROBE_RW")),
		ReadOnly:  splitPaths(os.Getenv("PROBE_RO")),
		Socket:    splitPaths(os.Getenv("PROBE_SOCK")),
	}); err != nil {
		fmt.Fprintln(os.Stderr, "apply:", err)
		os.Exit(3)
	}

	// And afterwards it must fail with a PERMISSION denial specifically. Any
	// other errno means something else changed, not that the ruleset bound.
	if _, err := os.ReadFile(canary); err == nil {
		fmt.Fprintf(os.Stderr, "the ruleset confines nothing: %s is still readable. Either the "+
			"grants cover it, in which case name a canary outside them with PROBE_CANARY, or "+
			"the ruleset was not established at all.\n", canary)
		os.Exit(4)
	} else if !errors.Is(err, fs.ErrPermission) {
		fmt.Fprintf(os.Stderr, "canary %s became unreadable for a reason that is not a permission "+
			"denial (%v), so the ruleset is not what stopped it.\n", canary, err)
		os.Exit(8)
	}

	argv := os.Args[1:]
	if len(argv) == 0 {
		fmt.Printf("ruleset established, %s denied, no command given\n", canary)
		return
	}

	bin, err := lookPath(argv[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "lookpath:", err)
		os.Exit(5)
	}
	// Whatever the engine reports after this point is the answer: Landlock is
	// inherited across execve and cannot be lifted.
	if err := syscall.Exec(bin, argv, os.Environ()); err != nil {
		fmt.Fprintln(os.Stderr, "exec:", err)
		os.Exit(6)
	}
}

// splitPaths turns a colon-separated list into a slice, dropping empties so an
// unset variable yields no grants rather than one grant on "".
func splitPaths(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ":") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// lookPath resolves argv[0] against PATH. Done here rather than left to
// syscall.Exec, which takes a path and not a name.
func lookPath(name string) (string, error) {
	if strings.Contains(name, "/") {
		return name, nil
	}
	for _, dir := range strings.Split(os.Getenv("PATH"), ":") {
		p := dir + "/" + name
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s not found in PATH", name)
}
