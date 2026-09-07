package main

import (
	"runtime"
	"strings"
	"testing"

	"github.com/apaderin/octoconv/internal/convert"
	"github.com/apaderin/octoconv/internal/sandbox"
)

// TestSandboxIsOnUnlessSomebodyWroteItDown is the whole startup contract.
//
// The failure this guards against is not a crash, it is a QUIET one: a worker
// that could not confine the engine, started anyway, and typeset client
// documents with read access to every file in its container. So the only two
// permitted outcomes are "confined" and "refused to start" -- unless the
// operator set the escape hatch, which is the third and has to be written
// down.
func TestSandboxIsOnUnlessSomebodyWroteItDown(t *testing.T) {
	helper, err := resolveSandboxHelper()

	switch {
	case err != nil:
		// Refused. Only legitimate where Landlock is genuinely absent, and the
		// message has to say what to do about it.
		if sandbox.Available() {
			t.Fatalf("landlock is available and resolveSandboxHelper still failed: %v", err)
		}
		if !strings.Contains(err.Error(), sandboxDisabledEnv) {
			t.Fatalf("the refusal does not name %s, so an operator on a kernel without landlock "+
				"has no documented way forward: %v", sandboxDisabledEnv, err)
		}
	case len(helper) == 0:
		t.Fatalf("resolveSandboxHelper returned no helper and no error: the engine would run "+
			"unconfined, silently, with %s unset", sandboxDisabledEnv)
	default:
		if !sandbox.Available() {
			t.Fatalf("a helper was returned on a host without landlock (%s): the sandbox cannot "+
				"be established and every job would fail at exec time instead of at startup", runtime.GOOS)
		}
		if helper[len(helper)-1] != sandboxExecMode {
			t.Fatalf("helper = %v, whose last argument is not %q: the binary would start as a "+
				"WORKER rather than as the sandbox helper, and consume the queue twice",
				helper, sandboxExecMode)
		}
	}
}

// The escape hatch is exact. "1", "yes" and "TRUE" must NOT disable the
// sandbox: a half-recognised value would be read as "off" by the operator and
// "on" by the code, or the reverse, and the reverse is the dangerous one.
func TestEscapeHatchIsExactlyTrue(t *testing.T) {
	for _, v := range []string{"1", "yes", "TRUE", "True", "on", ""} {
		t.Setenv(sandboxDisabledEnv, v)
		helper, err := resolveSandboxHelper()
		if err == nil && len(helper) == 0 {
			t.Fatalf("%s=%q disabled the sandbox; only the exact string \"true\" may",
				sandboxDisabledEnv, v)
		}
	}
	t.Setenv(sandboxDisabledEnv, "true")
	helper, err := resolveSandboxHelper()
	if err != nil || len(helper) != 0 {
		t.Fatalf("%s=true did not disable the sandbox (helper=%v err=%v); the documented "+
			"escape hatch does not work", sandboxDisabledEnv, helper, err)
	}
}

// The helper's exit status and the classifier that reads it are one contract
// spanning two packages. This is the cmd-side half.
func TestSetupExitMatchesTheClassifier(t *testing.T) {
	if sandboxSetupExit != convert.TectonicSandboxSetupExit {
		t.Fatalf("the helper exits %d and Convert classifies %d: a node that cannot establish the "+
			"sandbox would be reported to the client as a document that failed to typeset",
			sandboxSetupExit, convert.TectonicSandboxSetupExit)
	}
}

// The converter must actually receive the helper. A sandbox that is resolved,
// logged, and not plumbed is a sandbox that does not exist -- this repository
// has shipped that exact defect once, which is why buildTectonicConverter is a
// function at all.
func TestSandboxHelperReachesTheConverter(t *testing.T) {
	helper := []string{"/usr/local/bin/tectonic-worker", sandboxExecMode}
	c := buildTectonicConverter("/opt/tectonic/b.ttb", convert.DefaultTectonicMaxOutputBytes, helper)
	if len(c.SandboxHelper) != len(helper) {
		t.Fatalf("SandboxHelper = %v, want %v: the engine would run unconfined", c.SandboxHelper, helper)
	}
}
