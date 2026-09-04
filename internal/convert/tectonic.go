package convert

// This file (tectonic.go): the ninth engine class -- real typesetting,
// `latex` -> `pdf`, through tectonic.
//
// WHAT MAKES THIS ENGINE DIFFERENT FROM THE OTHER EIGHT
//
// Every other converter here reads a file and writes a file. This one runs a
// TeX interpreter over a document that is a PROGRAM: LaTeX has macros, control
// flow, file input and -- unless stopped -- the ability to run shell commands
// and to fetch what it does not have. Two of this phase's requirements exist
// because of that, and both are properties of the argv below plus the image
// around it, not of anything this Go code computes:
//
//	TEX-02  no network. The bundle is baked into the image and passed with -b.
//	        Without it tectonic reaches for relay.fullyjustified.net and the
//	        document decides what gets downloaded.
//	TEX-04  no shell escape. --untrusted makes \write18 and \input{|cmd}
//	        un-enableable REGARDLESS of any other flag on the same command
//	        line -- a structural guarantee rather than a default that a later
//	        argument can undo.
//
// WHAT IS NOT HERE, AND WHY THE ABSENCE IS THE GUARANTEE
//
// No biber. Tectonic launches biber as an EXTERNAL program when a document
// produces a .bcf, taking its argv from a .run.xml that the document's own
// packages write. Whether --untrusted gates that path was never established
// (64-RESEARCH.md records the reading as UNVERIFIED), and with no binary in the
// image the question does not arise. biblatex runs on backend=bibtex, which
// tectonic executes inside its own process. See 64-CONTEXT.md D-07.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TectonicBundleEnv names the environment variable cmd/tectonic-worker reads
// to locate the baked-in bundle. Declared here, beside the converter that
// consumes it, rather than in the cmd -- so the default below and the reader
// cannot drift the way PROBE_ENGINE_TIMEOUT once did between two processes.
const TectonicBundleEnv = "TECTONIC_BUNDLE_PATH"

// DefaultTectonicBundlePath is where Dockerfile.tectonic-worker puts the
// bundle. A path rather than a URL, and that is the whole of TEX-02: a bundle
// tectonic can open locally is a bundle it never fetches.
const DefaultTectonicBundlePath = "/opt/tectonic/octoconv-bundle.ttb"

// TectonicConverter is the tex engine's registry entry.
//
// BundlePath is empty in the zero value converters.go registers at init;
// cmd/tectonic-worker replaces it with the configured one at startup, the same
// shape cmd/ocr-worker and cmd/probe-worker use for their own ceilings. A zero
// value therefore cannot typeset, which is deliberate: the failure is "this
// process was not configured", not "the document fetched something".
type TectonicConverter struct {
	BundlePath string
}

// Engine reports the ninth engine class.
func (TectonicConverter) Engine() string { return EngineTectonic }

// Pairs is the single pair this engine serves.
//
// latex -> pdf only. pandoc already serves latex -> {html, docx, epub, md} and
// registering any of those here would panic at init: Registry.Register refuses
// a triple collision since Phase 57 D-09, which is what makes "two engines, one
// source format" safe to have at all.
func (TectonicConverter) Pairs() []Pair {
	return []Pair{{From: "latex", To: "pdf"}}
}

// tectonicArgs builds the argument vector, as its own function for the reason
// ps2writeArgs and ocrPageCountArgs are: the argv IS the security-relevant
// part, and a test can assert on it directly instead of reaching into a running
// process.
//
// Every flag here is either load-bearing or deliberately absent:
//
//	-X compile     the V2 CLI's one-shot compile.
//	--untrusted    TEX-04, and the whole of it.
//	-b <bundle>    TEX-02. Without it tectonic uses its default network bundle.
//	-o <dir>       output beside the input rather than in the process's cwd.
//	-p             print the engine's chatter. This is where a diagnosable
//	               failure message comes from, and 64-03's package-name
//	               extraction reads it -- removing it silently breaks a later
//	               requirement rather than this one.
//
// NOT passed, each for a stated reason:
//
//	-r/--reruns    tectonic reruns until convergence on its own. Pinning a
//	               count would replace "as many passes as this document needs"
//	               with a number, which is exactly what 64-05's measurement
//	               exists to characterise.
//	-k             nothing wants the .aux/.bcf litter.
//	--synctex      an editor feature; a server has no use for it.
//	-Z shell-escape  never, under any circumstances. --untrusted already makes
//	               it inert, and the two together mean a future edit that drops
//	               --untrusted still has to add this one deliberately.
func tectonicArgs(inPath, outDir, bundlePath string) []string {
	return []string{
		"-X", "compile", inPath,
		"--untrusted",
		"-b", bundlePath,
		"-o", outDir,
		"-p",
	}
}

// Convert typesets inPath into outPath.
//
// opts is accepted and ignored for now: this engine takes none, and the
// parameter stays because the Converter interface has it. Unlike sanitize, a
// populated map is NOT an error here -- 64-03 decides what tex opts mean, and
// refusing them now would make that a breaking change rather than an addition.
func (c TectonicConverter) Convert(ctx context.Context, inPath, outPath string, _ map[string]any) error {
	if c.BundlePath == "" {
		// Not a document failure. A converter with no bundle would fall back
		// to tectonic's default, which is a network fetch -- so this refuses
		// rather than silently violating TEX-02.
		return fmt.Errorf("tectonic: no bundle configured (%s unset); refusing to run, because "+
			"without -b tectonic fetches over the network", TectonicBundleEnv)
	}
	if _, err := os.Stat(c.BundlePath); err != nil {
		return fmt.Errorf("tectonic: bundle %q is not readable: %w", c.BundlePath, err)
	}

	outDir := filepath.Dir(outPath)
	out, err := runCommand(ctx, "tectonic", tectonicArgs(inPath, outDir, c.BundlePath)...)
	if err != nil {
		// The engine's own chatter is the only thing that says WHY, and it is
		// what 64-03 will parse for an unresolvable package name. Carried on
		// the error rather than dropped.
		return fmt.Errorf("tectonic: %w: %s", err, tectonicTail(out))
	}

	// tectonic writes <input basename>.pdf into -o, which is not necessarily
	// the name the worker asked for. Renaming rather than passing outPath
	// directly: -o takes a DIRECTORY, and pretending otherwise would put the
	// PDF somewhere the caller does not look.
	produced := filepath.Join(outDir, strings.TrimSuffix(filepath.Base(inPath), filepath.Ext(inPath))+".pdf")
	if produced != outPath {
		if err := os.Rename(produced, outPath); err != nil {
			return fmt.Errorf("tectonic: engine reported success but produced no %q: %w", produced, err)
		}
	}

	// Exit 0 with no output is a green job handing the client nothing --
	// sanitizePDF's own intermediate check exists because gs does exactly that
	// on some inputs, and a TeX engine that typesets an empty document is the
	// same trap.
	info, err := os.Stat(outPath)
	if err != nil {
		return fmt.Errorf("tectonic: no output at %q: %w", outPath, err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("tectonic: engine exited 0 and produced an empty PDF")
	}
	return nil
}

// tectonicTail returns the last few lines of the engine's output, which is
// where its error lines are. The whole log is unbounded and attacker-influenced;
// an error message is not the place to put it.
func tectonicTail(out []byte) string {
	const keep = 12
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) > keep {
		lines = lines[len(lines)-keep:]
	}
	return strings.Join(lines, " | ")
}
