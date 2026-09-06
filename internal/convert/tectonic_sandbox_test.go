package convert

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The sandbox spec IS the security claim, so it is asserted directly rather
// than through a running engine -- the same argument tectonicArgs is tested
// under. A kernel is not needed to see that /etc was granted.

// TestSandboxSpecGrantsTheJobDirectoryAndNothingElseWritable is the property
// that matters most: an engine whose input is a PROGRAM must not be able to
// write outside the directory holding its own job.
func TestSandboxSpecGrantsTheJobDirectoryAndNothingElseWritable(t *testing.T) {
	jobDir := t.TempDir()
	bundle := filepath.Join(t.TempDir(), "bundle.ttb")
	if err := os.WriteFile(bundle, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := filepath.Join(t.TempDir(), "tectonic")
	if err := os.WriteFile(engine, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	spec := tectonicSandboxSpec(filepath.Join(jobDir, "in.tex"), jobDir, bundle, engine)

	var rw []string
	for _, tok := range spec {
		if strings.HasPrefix(tok, "rw:") {
			rw = append(rw, strings.TrimPrefix(tok, "rw:"))
		}
	}
	if len(rw) == 0 {
		t.Fatalf("no writable path at all; the engine could not write its PDF.\nspec = %v", spec)
	}
	// /dev/null is the one other writable entry, because a program that cannot
	// open it fails in ways that have nothing to do with typesetting. Anything
	// BEYOND these is a widening nobody asked for, and adding one has to be an
	// edit to this list.
	//
	// os.TempDir() is deliberately NOT here. It was, and that was the defect:
	// Landlock grants are path-beneath and additive, so granting the shared
	// /tmp granted every other job's directory underneath it and made the
	// jobDir entry decorative. The engine's TMPDIR now points AT the job
	// directory instead (codex, PR #21).
	allowed := map[string]bool{jobDir: true, "/dev/null": true}
	for _, p := range rw {
		if !allowed[p] {
			t.Fatalf("spec grants WRITE to %q, which is neither the job directory (%q) nor TMPDIR (%q).\n"+
				"An engine that runs client-authored programs must not be able to write there.\nspec = %v",
				p, jobDir, os.TempDir(), spec)
		}
	}
}

// TestSandboxSpecDoesNotGrantEtc pins the read that started this work.
//
// PR #19 measured \openin=/etc/hostname typesetting the container's hostname
// into a client's PDF. /etc/ssl/certs is the one exception, and it is named
// explicitly so that widening the exception to /etc has to be a deliberate
// edit to this test.
func TestSandboxSpecDoesNotGrantEtc(t *testing.T) {
	jobDir := t.TempDir()
	bundle := filepath.Join(t.TempDir(), "bundle.ttb")
	if err := os.WriteFile(bundle, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := filepath.Join(t.TempDir(), "tectonic")
	if err := os.WriteFile(engine, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, tok := range tectonicSandboxSpec(filepath.Join(jobDir, "in.tex"), jobDir, bundle, engine) {
		path := tok[3:]
		if path == "/etc" || path == "/" || path == "/proc" || path == "/run" {
			t.Fatalf("spec grants %q. Landlock rules are PATH BENEATH: granting %q reaches every "+
				"secret, every mounted credential and /proc/self/environ -- the exact reads this "+
				"sandbox exists to remove.", tok, path)
		}
	}
}

// TestSandboxSpecSkipsPathsThatDoNotExist is the other direction, and it is
// about availability rather than confinement: sandbox.Apply treats a path it
// cannot open as an ERROR, so a spec naming /lib64 on an arm64 image would
// refuse every job on that image.
func TestSandboxSpecSkipsPathsThatDoNotExist(t *testing.T) {
	jobDir := t.TempDir()
	bundle := filepath.Join(t.TempDir(), "bundle.ttb")
	if err := os.WriteFile(bundle, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := filepath.Join(t.TempDir(), "tectonic")
	if err := os.WriteFile(engine, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, tok := range tectonicSandboxSpec(filepath.Join(jobDir, "in.tex"), jobDir, bundle, engine) {
		if _, err := os.Stat(tok[3:]); err != nil {
			t.Fatalf("spec names %q, which does not exist here: %v.\n"+
				"sandbox.Apply refuses a rule it cannot open, so this spec would fail EVERY job "+
				"on a host with this layout rather than confining one.", tok, err)
		}
	}
}

// TestSandboxSpecCarriesTheBundleReadOnly: the bundle must be reachable (the
// engine reads every .sty out of it) and must not be writable (one job must
// not be able to edit what the next job typesets with).
func TestSandboxSpecCarriesTheBundleReadOnly(t *testing.T) {
	jobDir := t.TempDir()
	bundle := filepath.Join(t.TempDir(), "bundle.ttb")
	if err := os.WriteFile(bundle, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := filepath.Join(t.TempDir(), "tectonic")
	if err := os.WriteFile(engine, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	spec := tectonicSandboxSpec(filepath.Join(jobDir, "in.tex"), jobDir, bundle, engine)
	if !slices.Contains(spec, "ro:"+bundle) {
		t.Fatalf("bundle %q is not readable in the spec; the engine would find no packages at all.\nspec = %v", bundle, spec)
	}
	if slices.Contains(spec, "rw:"+bundle) {
		t.Fatalf("bundle %q is WRITABLE in the spec: one client's document could rewrite the "+
			"package set the next client is typeset with.\nspec = %v", bundle, spec)
	}
}

// TestSandboxSetupExitIsNotTheEngineFailureCode holds the contract that keeps
// a broken node from being reported as a broken document.
//
// tectonic exits 1 when a document fails to compile. If the helper shared that
// status, Convert could not tell "the confinement could not be established"
// from "this LaTeX is wrong", and the first would be classified terminal and
// never retried.
func TestSandboxSetupExitIsNotTheEngineFailureCode(t *testing.T) {
	if TectonicSandboxSetupExit == 1 || TectonicSandboxSetupExit == 0 {
		t.Fatalf("TectonicSandboxSetupExit = %d, which collides with the engine's own exit statuses; "+
			"an infrastructure fault would be reported to the client as a document that failed to typeset",
			TectonicSandboxSetupExit)
	}
}

// TestSandboxSpecDoesNotGrantTheSharedTempDir states the regression as its own
// case, because the one above would keep passing if jobDir ever happened to BE
// the shared temporary directory.
func TestSandboxSpecDoesNotGrantTheSharedTempDir(t *testing.T) {
	jobDir := t.TempDir()
	bundle := filepath.Join(t.TempDir(), "bundle.ttb")
	if err := os.WriteFile(bundle, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := filepath.Join(t.TempDir(), "tectonic")
	if err := os.WriteFile(engine, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	shared := strings.TrimSuffix(os.TempDir(), "/")
	for _, tok := range tectonicSandboxSpec(filepath.Join(jobDir, "in.tex"), jobDir, bundle, engine) {
		if strings.TrimSuffix(tok[3:], "/") == shared {
			t.Fatalf("spec grants %q, the SHARED temporary directory. Landlock grants are recursive: "+
				"this reaches every other job's inputs and outputs, and lets a document write files "+
				"outside the workDir the worker cleans up.", tok)
		}
	}
}

// TestSandboxSetupFailureKeepsTheReason drives Convert through a helper that
// fails the way a real node does, and asserts the operator can still tell WHY.
//
// The defect it pins: runCommandLimitedEnv returns only STDOUT in out and
// embeds stderr in the error, while the helper reports on stderr. Formatting
// out here recorded an empty string, so "the sandbox could not be established"
// arrived with no indication whether ruleset creation, a path rule,
// restrict_self or the exec had failed -- on the one error class whose cause is
// a property of the machine and cannot be reproduced from the job (codex, #21).
func TestSandboxSetupFailureKeepsTheReason(t *testing.T) {
	dir := t.TempDir()

	// A stand-in engine, so exec.LookPath resolves on any host: Convert
	// locates it before invoking the helper, and never runs it here.
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "tectonic"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))

	const reason = "landlock: restrict self: operation not permitted"
	helper := filepath.Join(dir, "helper")
	script := "#!/bin/sh\necho '__sandbox-exec: " + reason + "' >&2\nexit " +
		fmt.Sprint(TectonicSandboxSetupExit) + "\n"
	if err := os.WriteFile(helper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	bundle := filepath.Join(dir, "bundle.ttb")
	if err := os.WriteFile(bundle, []byte("not a real bundle, never opened"), 0o600); err != nil {
		t.Fatal(err)
	}
	in := filepath.Join(dir, "in.tex")
	if err := os.WriteFile(in, []byte("\\documentclass{article}\\begin{document}x\\end{document}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	c := TectonicConverter{BundlePath: bundle, SandboxHelper: []string{helper}}
	err := c.Convert(context.Background(), in, filepath.Join(dir, "out.pdf"), nil)

	if err == nil {
		t.Fatal("a helper exiting with the setup status produced no error")
	}
	if !errors.Is(err, ErrTeXSandboxUnavailable) {
		t.Fatalf("error is not classified as a sandbox fault, so the job would be treated as a bad "+
			"document and never retried: %v", err)
	}
	if !strings.Contains(err.Error(), reason) {
		t.Fatalf("the reason the sandbox could not be established is missing from the error.\n"+
			"An operator sees only that it failed, not whether it was the ruleset, a path rule or "+
			"the exec -- and this is the one failure here that cannot be reproduced from the job.\n"+
			"want it to contain: %q\ngot: %v", reason, err)
	}
}

// TestCacheDirValidationCatchesWhatTheSandboxMadeFatal covers a failure mode
// this sandbox INTRODUCED.
//
// Unsandboxed, a tectonic that finds no TECTONIC_CACHE_DIR falls back to
// $HOME/.cache and compiles its own format -- slow, but it works. Under
// Landlock $HOME is not granted, so the same fallback ends as "cannot write
// format file texput.fmt: Permission denied" on every job, measured in the
// shipped image. The message names the sandbox and not the unset variable,
// which is why this is a startup check and not a comment.
func TestCacheDirValidationCatchesWhatTheSandboxMadeFatal(t *testing.T) {
	if err := ValidateTectonicCacheDir(""); err == nil {
		t.Fatal("an unset cache directory was accepted; every job would fail with a permission " +
			"error that points at the sandbox rather than at the missing variable")
	}
	if err := ValidateTectonicCacheDir("/nonexistent/cache/dir"); err == nil {
		t.Fatal("a cache directory that does not exist was accepted")
	}

	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTectonicCacheDir(file); err == nil {
		t.Fatal("a regular file was accepted as the format cache")
	}

	// EXISTING IS NOT ENOUGH. An empty directory used to be accepted here, and
	// codex round 2 named the case: an empty volume mounted over the image's
	// pre-warmed cache. It passes every structural check and then fails every
	// job, because the sandbox grants it read-only and the engine cannot
	// compile the format it is missing. Before confinement that same empty
	// cache populated itself on the first run.
	if err := ValidateTectonicCacheDir(t.TempDir()); err == nil {
		t.Fatal("an EMPTY cache directory was accepted. Read-only, the engine cannot populate it, " +
			"so every job fails with a permission error -- and an empty mounted volume is the " +
			"ordinary way to reach this")
	}

	// ...and a directory that actually holds a format is accepted, including
	// one nested the way tectonic lays its cache out.
	warm := t.TempDir()
	if err := os.MkdirAll(filepath.Join(warm, "formats", "x86"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(warm, "formats", "x86", "latex.fmt.gz"), []byte("f"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTectonicCacheDir(warm); err != nil {
		t.Fatalf("a cache holding a compiled format was refused: %v", err)
	}

	// Classified as a deployment fault, so the client is not told their
	// document is at fault -- the same sentinel the bundle checks use.
	if !errors.Is(ValidateTectonicCacheDir(""), ErrTeXBundleUnusable) {
		t.Fatal("the cache fault is not classified as a deployment fault, so it would reach the " +
			"client as \"the document could not be typeset\"")
	}
}

// TestSandboxSpecGrantsTheResolvedEngine covers an installation this would
// otherwise break.
//
// tectonicSystemReadOnly names /usr, /lib and /bin, which is where this image
// puts tectonic -- but exec.LookPath answers from PATH, and an image that
// installs it in /opt or a Nix store resolves outside all three. The helper
// would apply the ruleset and then take EACCES from execve, so a deployment
// that worked before confinement would stop working, with an error that says
// nothing about PATH (codex round 2).
func TestSandboxSpecGrantsTheResolvedEngine(t *testing.T) {
	jobDir := t.TempDir()
	bundle := filepath.Join(t.TempDir(), "bundle.ttb")
	if err := os.WriteFile(bundle, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Deliberately outside every entry in tectonicSystemReadOnly.
	elsewhere := t.TempDir()
	engine := filepath.Join(elsewhere, "tectonic")
	if err := os.WriteFile(engine, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	spec := tectonicSandboxSpec(filepath.Join(jobDir, "in.tex"), jobDir, bundle, engine)
	if !slices.Contains(spec, "ro:"+engine) {
		t.Fatalf("the resolved engine %q is not in the ruleset, so execve would return EACCES "+
			"after the sandbox is applied -- and the failure would name the sandbox, not PATH.\nspec = %v",
			engine, spec)
	}
	if slices.Contains(spec, "rw:"+engine) {
		t.Fatalf("the engine binary is WRITABLE: a document could rewrite the program that typesets "+
			"the next one.\nspec = %v", spec)
	}
}
