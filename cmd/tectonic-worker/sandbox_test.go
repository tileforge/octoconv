package main

import (
	"testing"

	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/sandbox"
)

// The helper's exit status and the classifier that reads it are one contract
// spanning two packages. This is the cmd-side half.
func TestSetupExitMatchesTheClassifier(t *testing.T) {
	if sandbox.SetupExit != convert.TectonicSandboxSetupExit {
		t.Fatalf("the helper exits %d and Convert classifies %d: a node that cannot establish the "+
			"sandbox would be reported to the client as a document that failed to typeset",
			sandbox.SetupExit, convert.TectonicSandboxSetupExit)
	}
}

// The converter must actually receive the helper. A sandbox that is resolved,
// logged, and not plumbed is a sandbox that does not exist -- this repository
// has shipped that exact defect once, which is why buildTectonicConverter is a
// function at all.
func TestSandboxHelperReachesTheConverter(t *testing.T) {
	helper := []string{"/usr/local/bin/tectonic-worker", sandbox.ExecMode}
	c := buildTectonicConverter("/opt/tectonic/b.ttb", convert.DefaultTectonicMaxOutputBytes, helper, 0, true)
	if len(c.SandboxHelper) != len(helper) {
		t.Fatalf("SandboxHelper = %v, want %v: the engine would run unconfined", c.SandboxHelper, helper)
	}
}
