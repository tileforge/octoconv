// Package repohygiene holds checks about the repository itself rather than
// about any one package's behaviour. It has no non-test files on purpose:
// there is nothing here to import, only things to keep true.
//
// This file makes .gitignore's list of command binaries a CONTRACT rather than
// a list somebody remembers to extend, the same way
// internal/convert/readme_sync_test.go does for the engine classes and
// internal/api/openapi_paths_sync_test.go for the routes.
//
// It exists because `go build -o api ./cmd/api` writes its binary to the
// repository root and .gitignore knew one such name -- /tectonic-worker, added
// after one incident. Every other command was free to repeat it, and two had:
// a 39 MB api and a 14 MB manage-clients were sitting untracked, one `git add
// -A` away from the history forever. Fixing the instance and not the class is
// what made that possible, so the list is now held equal to cmd/ from both
// sides.
package repohygiene_test

import (
	"os"
	"sort"
	"strings"
	"testing"
)

const (
	gitignorePath = "../../.gitignore"
	cmdPath       = "../../cmd"

	// blockBegin/blockEnd delimit the machine-checked list. Markers rather
	// than a position, so prose can be rewritten around it and the next
	// editor is told the lines are under test before changing them.
	blockBegin = "# cmd-binaries-begin"
	blockEnd   = "# cmd-binaries-end"
)

// ignoredCommandBinaries returns the names listed between the markers.
func ignoredCommandBinaries(t *testing.T) []string {
	t.Helper()

	b, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("read %s: %v", gitignorePath, err)
	}
	doc := string(b)

	start := strings.Index(doc, blockBegin)
	if start < 0 {
		t.Fatalf("%s no longer contains %q -- the list below it is what this test holds equal "+
			"to cmd/; if the block moved, move the markers with it rather than deleting the check",
			gitignorePath, blockBegin)
	}
	end := strings.Index(doc[start:], blockEnd)
	if end < 0 {
		t.Fatalf("the cmd-binaries block in %s is unterminated (%q missing)", gitignorePath, blockEnd)
	}

	var names []string
	for _, line := range strings.Split(doc[start:start+end], "\n")[1:] {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, "/") {
			t.Errorf("%q inside the cmd-binaries block is not anchored with a leading slash, so "+
				"it would also match that name in any subdirectory -- /api must not silence "+
				"cmd/api or internal/api", line)
			continue
		}
		names = append(names, strings.TrimPrefix(line, "/"))
	}
	return names
}

// commandDirectories returns the names under cmd/.
func commandDirectories(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(cmdPath)
	if err != nil {
		t.Fatalf("read %s: %v", cmdPath, err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		t.Fatalf("found no directories under %s -- the scan, not the repository, is probably "+
			"what broke", cmdPath)
	}
	return names
}

// TestGitignoreCoversEveryCommandBinary compares the two sets in BOTH
// directions.
//
// Each direction fails differently and both matter. A command missing from
// .gitignore is a binary that can be committed by accident -- tens of
// megabytes that a repository never gets back. A name in .gitignore with no
// command behind it is worse than clutter: it is a pattern silently hiding
// whatever file someday takes that name.
func TestGitignoreCoversEveryCommandBinary(t *testing.T) {
	ignored := ignoredCommandBinaries(t)
	commands := commandDirectories(t)

	ignoredSet := make(map[string]bool, len(ignored))
	for _, n := range ignored {
		ignoredSet[n] = true
	}
	commandSet := make(map[string]bool, len(commands))
	for _, n := range commands {
		commandSet[n] = true
	}

	var unignored, orphaned []string
	for _, n := range commands {
		if !ignoredSet[n] {
			unignored = append(unignored, n)
		}
	}
	for _, n := range ignored {
		if !commandSet[n] {
			orphaned = append(orphaned, n)
		}
	}
	sort.Strings(unignored)
	sort.Strings(orphaned)

	if len(unignored) > 0 {
		t.Errorf("cmd/%v can be built into the repository root and .gitignore does not cover "+
			"them, so `git add -A` would commit the binary.\nAdd /<name> inside the "+
			"cmd-binaries block.", unignored)
	}
	if len(orphaned) > 0 {
		t.Errorf(".gitignore's cmd-binaries block lists %v, which cmd/ does not contain. A "+
			"pattern with nothing behind it hides whatever file takes that name next.\n"+
			"Remove it, or restore the command it was written for.", orphaned)
	}
}
