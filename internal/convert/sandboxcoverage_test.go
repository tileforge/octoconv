// Package convert (this file): sandboxcoverage_test.go is phase 68's closing
// gate -- an AST+type-checked scanner over every non-test file in this
// package that fails the build when an engine exec anywhere in
// internal/convert, now or in a file added later, escapes classification:
// runs a binary other than through engineExec.run/.stream, execs a binary
// whose literal name is missing from its class's engines list, leaves a
// listed engine never exec'd (a stale grant), or lands in a file nobody
// assigned to a class or a later phase (T-68-27). The class is held, not
// the four named instances.
//
// TestEngineCallScannerCatchesALiteralExec is the scanner's own self-test:
// small, self-contained synthetic packages -- never the real production
// files -- prove each violation shape actually fires, and that a correctly
// routed class file does not.
package convert

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------
// Classification of the REAL package's files. See the plan's <action> for
// the shape this mirrors: infrastructure files implement the mechanism and
// may call anything; legacy-confined files (their own pre-Phase-68
// mechanism) may only pass a non-literal, already-wrapped name; pending
// files belong to a class a later phase confines and may still call a
// low-level runner with a literal name; class files must route every
// engine invocation through engineExec.run/.stream with a literal name in
// their own class's engines list; anything else is unclassified and a
// build failure.
// ---------------------------------------------------------------------

// sandboxInfraFiles implement the confinement mechanism itself
// (engineExec.run/.stream, the low-level runners, wrapEngine) and may call
// a low-level runner or exec.Command/exec.CommandContext freely.
var sandboxInfraFiles = map[string]bool{
	"exec.go":         true,
	"progressexec.go": true,
	"sandboxwrap.go":  true,
}

// sandboxLegacyConfinedFiles are engines confined by their OWN mechanism,
// built before engineExec existed: libvips.go (phase 67) wraps through
// engineSandbox.wrapEngine directly, and tectonic.go (PR #21) through its
// own CPU-budget-carrying helper. Both may call a low-level runner ONLY
// with a NON-literal name -- the helper-wrapped/resolved name their own
// wrap step produced, never a bare string.
var sandboxLegacyConfinedFiles = map[string]bool{
	"libvips.go":  true,
	"tectonic.go": true,
}

// sandboxPendingFiles belong to a class a LATER phase confines. A
// literal-name low-level-runner call is allowed ONLY in these files --
// nowhere else in the package -- each commented with the phase that
// removes its entry once that phase confines the class.
var sandboxPendingFiles = map[string]string{
	"libreoffice.go": "phase 70",
	"verapdf.go":     "phase 70",
}

// sandboxClassFileEngines maps a class file to the literal engine names its
// engineExec.run/.stream calls may use. audioduration.go's probe
// (probeDuration, shared by EnforceMaxDuration for audio and avduration.go's
// own callers for av) is restricted to the INTERSECTION of
// avEngines/audioEngines: whatever it calls must be legitimate under BOTH
// classes' rulesets, since forJob's caller decides which measured set
// actually applies at run time (68-RESEARCH.md Pitfall 2).
func sandboxClassFileEngines(base string) (engines []string, isClass bool) {
	switch base {
	case "pandoc.go", "pandocsandbox.go":
		return pandocEngines, true
	case "ocr.go", "ocrpdf.go", "ocrguards.go", "ocrsandbox.go":
		return ocrEngines, true
	case "av.go", "avduration.go", "avsandbox.go":
		return avEngines, true
	case "whisper.go", "audiosandbox.go":
		return audioEngines, true
	case "audioduration.go":
		return sandboxIntersect(avEngines, audioEngines), true
	case "chromium.go", "htmlsandbox.go":
		return htmlEngines, true
	case "sanitizeconverter.go", "sanitize.go", "sanitizepdf.go", "sanitizeverify.go", "probesandbox.go":
		return probeEngines, true
	}
	return nil, false
}

// sandboxClassFileGroups maps a class file to the class(es) whose "found
// engines" union it contributes to, for the per-class equality check below.
// audioduration.go contributes to BOTH av and audio -- its probe serves
// both classes -- everything else contributes to exactly one.
func sandboxClassFileGroups(base string) []string {
	switch base {
	case "pandoc.go", "pandocsandbox.go":
		return []string{"pandoc"}
	case "ocr.go", "ocrpdf.go", "ocrguards.go", "ocrsandbox.go":
		return []string{"ocr"}
	case "av.go", "avduration.go", "avsandbox.go":
		return []string{"av"}
	case "whisper.go", "audiosandbox.go":
		return []string{"audio"}
	case "audioduration.go":
		return []string{"av", "audio"}
	case "chromium.go", "htmlsandbox.go":
		return []string{"html"}
	case "sanitizeconverter.go", "sanitize.go", "sanitizepdf.go", "sanitizeverify.go", "probesandbox.go":
		return []string{"probe"}
	}
	return nil
}

// sandboxGroupEngines is the oracle a class's found-engines union is
// compared against: the SAME package vars the converters themselves build
// their rulesets from (pandocEngines, ocrEngines, avEngines, audioEngines).
func sandboxGroupEngines(group string) []string {
	switch group {
	case "pandoc":
		return pandocEngines
	case "ocr":
		return ocrEngines
	case "av":
		return avEngines
	case "audio":
		return audioEngines
	case "html":
		return htmlEngines
	case "probe":
		return probeEngines
	}
	return nil
}

func sandboxIntersect(a, b []string) []string {
	bs := make(map[string]bool, len(b))
	for _, x := range b {
		bs[x] = true
	}
	var out []string
	for _, x := range a {
		if bs[x] {
			out = append(out, x)
		}
	}
	return out
}

func sandboxContains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------
// The scanner itself -- shared by the real test and its own self-test.
// ---------------------------------------------------------------------

// sandboxLowLevelRunnerNameArg maps a low-level runner function's name to
// the zero-based index, within ITS OWN call's argument list, of the "name"
// (engine binary) argument -- exec.go's own signatures.
var sandboxLowLevelRunnerNameArg = map[string]int{
	"runCommand":              1,
	"runCommandAs":            2,
	"runCommandCore":          4,
	"runCommandLimited":       2,
	"runCommandLimitedEnv":    3,
	"runCommandLimitedEnvCPU": 3,
}

type sandboxViolation struct {
	pos string
	msg string
}

func (v sandboxViolation) String() string { return v.pos + ": " + v.msg }

// sandboxScanConfig parameterizes classification so the same scanner code
// runs over both the real package and the self-test's synthetic ones.
type sandboxScanConfig struct {
	infra        map[string]bool
	legacy       map[string]bool
	pending      map[string]string
	classEngines func(base string) ([]string, bool)
	classGroups  func(base string) []string
	groupEngines func(group string) []string
}

func realSandboxScanConfig() sandboxScanConfig {
	return sandboxScanConfig{
		infra:        sandboxInfraFiles,
		legacy:       sandboxLegacyConfinedFiles,
		pending:      sandboxPendingFiles,
		classEngines: sandboxClassFileEngines,
		classGroups:  sandboxClassFileGroups,
		groupEngines: sandboxGroupEngines,
	}
}

// sandboxScanReport is what one scan produces: every violation found, the
// union of literal engine names actually exec'd per class (for the
// per-class equality check), and how many engineExec calls were found per
// file (for the non-vacuity check).
type sandboxScanReport struct {
	violations   []sandboxViolation
	foundByGroup map[string]map[string]bool
	callsByFile  map[string]int
}

// sandboxScan type-checks files (one package, built from fset) and walks
// every call expression, classifying each low-level-runner call,
// exec.Command/exec.CommandContext call, and engineExec.run/.stream call
// according to cfg. Type-checking (go/types + go/importer) is what lets the
// walk tell an engineExec.run call apart from an unrelated method also
// named run or stream, rather than exempting every "*.run(" text match.
func sandboxScan(t testing.TB, fset *token.FileSet, files []*ast.File, cfg sandboxScanConfig) sandboxScanReport {
	t.Helper()

	conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil)}
	info := &types.Info{
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Uses:       make(map[*ast.Ident]types.Object),
	}
	if _, err := conf.Check("octoconv/sandboxscan", fset, files, info); err != nil {
		t.Fatalf("type-check the package for the sandbox coverage scan: %v", err)
	}

	report := sandboxScanReport{
		foundByGroup: make(map[string]map[string]bool),
		callsByFile:  make(map[string]int),
	}

	addFound := func(group, engine string) {
		if report.foundByGroup[group] == nil {
			report.foundByGroup[group] = make(map[string]bool)
		}
		report.foundByGroup[group][engine] = true
	}
	violate := func(pos token.Pos, format string, args ...any) {
		p := fset.Position(pos)
		report.violations = append(report.violations, sandboxViolation{
			pos: fmt.Sprintf("%s:%d", filepath.Base(p.Filename), p.Line),
			msg: fmt.Sprintf(format, args...),
		})
	}

	for _, f := range files {
		base := filepath.Base(fset.Position(f.Pos()).Filename)

		isInfra := cfg.infra[base]
		isLegacy := cfg.legacy[base]
		_, isPending := cfg.pending[base]
		classEngines, isClass := (func() ([]string, bool) {
			if cfg.classEngines == nil {
				return nil, false
			}
			return cfg.classEngines(base)
		})()
		classGroups := func() []string {
			if cfg.classGroups == nil {
				return nil
			}
			return cfg.classGroups(base)
		}()

		reportLowLevelCall := func(fnName string, nameArgIdx int, call *ast.CallExpr) {
			switch {
			case isInfra:
				// The mechanism itself.
			case isPending:
				// Literal (or any) low-level-runner call is allowed only here.
			case isLegacy:
				if nameArgIdx < 0 {
					violate(call.Pos(), "%s calls %s -- legacy-confined files may only call a low-level runner with a non-literal (helper-resolved) name", base, fnName)
					return
				}
				if lit, isLit := sandboxLowLevelCallLiteral(nameArgIdx, call); isLit {
					violate(call.Pos(), "%s calls %s(%q) with a LITERAL engine name -- legacy-confined files must pass the helper-wrapped name, never a bare string", base, fnName, lit)
				}
			case isClass:
				violate(call.Pos(), "%s calls %s directly -- class files must route every engine invocation through engineExec.run/.stream, not a low-level runner", base, fnName)
			default:
				violate(call.Pos(), "%s calls %s -- classify it: confine it (route through engineExec) or assign it to a phase (add it to the pending-class allowlist)", base, fnName)
			}
		}

		reportEngineExecCall := func(method string, call *ast.CallExpr) {
			if !isClass {
				violate(call.Pos(), "%s calls engineExec.%s but is not registered in sandboxClassFileEngines/sandboxClassFileGroups -- classify it", base, method)
				return
			}
			nameArgIdx := 1 // run(ctx, name, args...)
			if method == "stream" {
				nameArgIdx = 3 // stream(ctx, onLine, fifoDir, name, args)
			}
			if nameArgIdx >= len(call.Args) {
				violate(call.Pos(), "%s calls engineExec.%s with too few arguments to find the name", base, method)
				return
			}
			bl, ok := call.Args[nameArgIdx].(*ast.BasicLit)
			if !ok || bl.Kind != token.STRING {
				violate(call.Pos(), "%s calls engineExec.%s with a NON-LITERAL name -- the coverage scanner must see what is exec'd", base, method)
				return
			}
			lit, err := strconv.Unquote(bl.Value)
			if err != nil {
				violate(call.Pos(), "%s calls engineExec.%s with an unparseable name literal", base, method)
				return
			}
			if !sandboxContains(classEngines, lit) {
				violate(call.Pos(), "%s calls engineExec.%s(%q) -- %q is not in this class's engines list", base, method, lit, lit)
				return
			}
			report.callsByFile[base]++
			for _, g := range classGroups {
				addFound(g, lit)
			}
		}

		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			if id, ok := call.Fun.(*ast.Ident); ok {
				if idx, isRunner := sandboxLowLevelRunnerNameArg[id.Name]; isRunner {
					if obj := info.Uses[id]; obj != nil {
						if _, isFunc := obj.(*types.Func); isFunc {
							reportLowLevelCall(id.Name, idx, call)
						}
					}
				} else if id.Name == "runCommandStreaming" {
					if obj := info.Uses[id]; obj != nil {
						if _, isFunc := obj.(*types.Func); isFunc {
							reportLowLevelCall(id.Name, -1, call)
						}
					}
				}
				return true
			}

			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}

			if isPkgQualifiedExecCommand(info, sel) {
				idx := 0
				if sel.Sel.Name == "CommandContext" {
					idx = 1
				}
				reportLowLevelCall("exec."+sel.Sel.Name, idx, call)
				return true
			}

			if recvName, ok := sandboxSelectionReceiverName(info, sel); ok && recvName == "engineExec" &&
				(sel.Sel.Name == "run" || sel.Sel.Name == "stream") {
				reportEngineExecCall(sel.Sel.Name, call)
			}
			return true
		})
	}

	return report
}

// sandboxLowLevelCallLiteral reports whether call's argument at nameArgIdx
// is a string literal, and its value.
func sandboxLowLevelCallLiteral(nameArgIdx int, call *ast.CallExpr) (lit string, isLiteral bool) {
	if nameArgIdx < 0 || nameArgIdx >= len(call.Args) {
		return "", false
	}
	bl, ok := call.Args[nameArgIdx].(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", false
	}
	v, err := strconv.Unquote(bl.Value)
	if err != nil {
		return "", false
	}
	return v, true
}

// isPkgQualifiedExecCommand reports whether sel is os/exec's Command or
// CommandContext, resolved through type info rather than by matching the
// literal identifier "exec" -- an import alias would defeat a text match.
func isPkgQualifiedExecCommand(info *types.Info, sel *ast.SelectorExpr) bool {
	if sel.Sel.Name != "Command" && sel.Sel.Name != "CommandContext" {
		return false
	}
	obj := info.Uses[sel.Sel]
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	return obj.Pkg().Path() == "os/exec"
}

// sandboxSelectionReceiverName resolves sel's method-call receiver type
// NAME (e.g. "engineExec"), through go/types rather than a syntactic guess
// -- this is what tells an engineExec.run call apart from an unrelated
// method also named run or stream, if one is ever added to a class file.
func sandboxSelectionReceiverName(info *types.Info, sel *ast.SelectorExpr) (string, bool) {
	selection, ok := info.Selections[sel]
	if !ok || selection == nil || selection.Kind() != types.MethodVal {
		return "", false
	}
	t := selection.Recv()
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return "", false
	}
	return named.Obj().Name(), true
}

// sandboxParseRealPackage parses every non-_test.go file in the CURRENT
// directory -- go test's working directory is the package's own source
// directory, so "." is internal/convert.
func sandboxParseRealPackage(t *testing.T) (*token.FileSet, []*ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}
	var files []*ast.File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		t.Fatal("found zero non-test .go files -- this test must run from internal/convert")
	}
	return fset, files
}

// ---------------------------------------------------------------------
// TestConfinedClassesExecOnlyThroughTheSandbox -- the real gate.
// ---------------------------------------------------------------------

func TestConfinedClassesExecOnlyThroughTheSandbox(t *testing.T) {
	fset, files := sandboxParseRealPackage(t)
	cfg := realSandboxScanConfig()
	report := sandboxScan(t, fset, files, cfg)

	for _, v := range report.violations {
		t.Errorf("%s", v.String())
	}

	// Non-vacuity: the scan actually visited (and found engine calls in)
	// the four files 68-RESEARCH.md names as the confirming evidence for
	// each class's own converter.
	for _, base := range []string{"pandoc.go", "ocrguards.go", "av.go", "whisper.go", "chromium.go", "sanitizepdf.go", "sanitizeverify.go"} {
		if report.callsByFile[base] == 0 {
			t.Errorf("found zero engineExec calls in %s -- the scanner is not actually parsing this file (non-vacuity check)", base)
		}
	}

	for _, group := range []string{"pandoc", "ocr", "av", "audio", "html", "probe"} {
		want := sandboxGroupEngines(group)
		if len(want) == 0 {
			t.Fatalf("class %s: sandboxGroupEngines returned nothing -- the group is misconfigured", group)
		}
		got := report.foundByGroup[group]
		wantSet := make(map[string]bool, len(want))
		for _, w := range want {
			wantSet[w] = true
			if !got[w] {
				t.Errorf("class %s: %q is listed in its engines var but never exec'd anywhere -- stale grant", group, w)
			}
		}
		for g := range got {
			if !wantSet[g] {
				t.Errorf("class %s: %q is exec'd somewhere but not in its engines var -- ungranted use", group, g)
			}
		}
	}
}

// ---------------------------------------------------------------------
// TestEngineCallScannerCatchesALiteralExec -- the scanner's own self-test.
// Synthetic, self-contained mini-packages, never the real production
// files: each shape below must yield a violation, and a correctly-routed
// class file must yield none.
// ---------------------------------------------------------------------

// sandboxSyntheticInfra stands in for sandboxwrap.go/progressexec.go: it
// declares a minimal engineExec type with run/stream methods, and a
// minimal runCommand, so each scenario below type-checks on its own
// without needing the real package's full import graph.
const sandboxSyntheticInfra = `package synth

import "context"

type engineExec struct{}

func (e engineExec) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return nil, nil
}

func (e engineExec) stream(ctx context.Context, onLine func(string), fifoDir, name string, args []string) ([]byte, error) {
	return nil, nil
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return nil, nil
}
`

const sandboxSyntheticLowLevelInClass = `package synth

import "context"

func doIt(ctx context.Context) {
	runCommand(ctx, "somebinary")
}
`

const sandboxSyntheticUngranted = `package synth

import "context"

func doIt(ctx context.Context, x engineExec) {
	x.run(ctx, "notallowed")
}
`

const sandboxSyntheticNonLiteral = `package synth

import "context"

func doIt(ctx context.Context, x engineExec, name string) {
	x.run(ctx, name)
}
`

const sandboxSyntheticUnclassifiedExecCommand = `package synth

import "os/exec"

func doIt() {
	exec.Command("somebinary")
}
`

const sandboxSyntheticCorrect = `package synth

import "context"

func doIt(ctx context.Context, x engineExec) {
	x.run(ctx, "allowedengine")
}
`

// sandboxRunSyntheticScenario type-checks the synthetic infra file plus one
// target file, classified as a class file (engines=classEngines) iff
// classFiles[targetName], and returns whatever violations the scanner found.
func sandboxRunSyntheticScenario(t *testing.T, targetName, targetSrc string, classFiles map[string]bool, classEngines []string) []sandboxViolation {
	t.Helper()
	fset := token.NewFileSet()
	infra, err := parser.ParseFile(fset, "zzinfra.go", sandboxSyntheticInfra, 0)
	if err != nil {
		t.Fatalf("parse synthetic infra: %v", err)
	}
	target, err := parser.ParseFile(fset, targetName, targetSrc, 0)
	if err != nil {
		t.Fatalf("parse synthetic target %s: %v", targetName, err)
	}

	cfg := sandboxScanConfig{
		infra: map[string]bool{"zzinfra.go": true},
		classEngines: func(base string) ([]string, bool) {
			if classFiles[base] {
				return classEngines, true
			}
			return nil, false
		},
		classGroups: func(base string) []string {
			if classFiles[base] {
				return []string{"synthclass"}
			}
			return nil
		},
		groupEngines: func(group string) []string {
			if group == "synthclass" {
				return classEngines
			}
			return nil
		},
	}
	report := sandboxScan(t, fset, []*ast.File{infra, target}, cfg)
	return report.violations
}

func TestEngineCallScannerCatchesALiteralExec(t *testing.T) {
	t.Run("low-level runner call in a class file", func(t *testing.T) {
		v := sandboxRunSyntheticScenario(t, "classfile.go", sandboxSyntheticLowLevelInClass,
			map[string]bool{"classfile.go": true}, []string{"allowedengine"})
		if len(v) == 0 {
			t.Error("want a violation for a class file calling a low-level runner directly, got none")
		}
	})

	t.Run("engine not in the class's engines list", func(t *testing.T) {
		v := sandboxRunSyntheticScenario(t, "classfile.go", sandboxSyntheticUngranted,
			map[string]bool{"classfile.go": true}, []string{"allowedengine"})
		if len(v) == 0 {
			t.Error("want a violation for exec'ing an engine outside the class's engines list, got none")
		}
	})

	t.Run("non-literal name passed to run", func(t *testing.T) {
		v := sandboxRunSyntheticScenario(t, "classfile.go", sandboxSyntheticNonLiteral,
			map[string]bool{"classfile.go": true}, []string{"allowedengine"})
		if len(v) == 0 {
			t.Error("want a violation for a non-literal name passed to engineExec.run, got none")
		}
	})

	t.Run("unclassified file execs directly", func(t *testing.T) {
		v := sandboxRunSyntheticScenario(t, "unclassified.go", sandboxSyntheticUnclassifiedExecCommand,
			map[string]bool{}, nil)
		if len(v) == 0 {
			t.Error("want a violation for an unclassified file calling exec.Command, got none")
		}
	})

	t.Run("a correct class file yields no violation", func(t *testing.T) {
		v := sandboxRunSyntheticScenario(t, "classfile.go", sandboxSyntheticCorrect,
			map[string]bool{"classfile.go": true}, []string{"allowedengine"})
		if len(v) != 0 {
			t.Errorf("want zero violations for a correctly-routed class file, got %v", v)
		}
	})
}
