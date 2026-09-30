package convert

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// requireMeasuredSet compares a class var with the set SANDBOX-PATHS.md
// records, in order. A mismatch says to re-measure, never to edit the
// expectation: the expectation is the measurement.
func requireMeasuredSet(t *testing.T, name string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s = %q, this test knows %q -- re-measure with scripts/document-sandbox-measure.sh "+
			"and update SANDBOX-PATHS.md rather than editing the expectation", name, got, want)
	}
}

func TestSofficeReadWriteSetIsTheMeasuredOne(t *testing.T) {
	requireMeasuredSet(t, "sofficeReadWrite", sofficeReadWrite, []string{"/tmp"})
}

// The scoped-socket route (70-01): a directory beneath which soffice may
// create a Unix socket and nothing else.
func TestSofficeSocketDirsAreTheMeasuredOnes(t *testing.T) {
	requireMeasuredSet(t, "sofficeSocketDirs", sofficeSocketDirs, []string{"/tmp"})
}

func TestSofficeReadOnlySetIsTheMeasuredOne(t *testing.T) {
	requireMeasuredSet(t, "sofficeReadOnly", sofficeReadOnly, []string{
		"/usr/lib",
		"/usr/bin/dash",
		"/usr/bin/basename",
		"/usr/bin/dirname",
		"/usr/bin/ls",
		"/usr/bin/sed",
		"/etc/libreoffice",
		"/usr/share/fonts",
		"/dev/urandom",
	})
}

func TestSofficeEnginesAreTheMeasuredOnes(t *testing.T) {
	requireMeasuredSet(t, "sofficeEngines", sofficeEngines, []string{"soffice"})
}

func TestVerapdfReadWriteSetIsTheMeasuredOne(t *testing.T) {
	requireMeasuredSet(t, "verapdfReadWrite", verapdfReadWrite, []string{"/dev/null"})
}

func TestVerapdfReadOnlySetIsTheMeasuredOne(t *testing.T) {
	requireMeasuredSet(t, "verapdfReadOnly", verapdfReadOnly, []string{
		"/usr/lib",
		"/usr/bin/dash",
		"/usr/bin/dirname",
		"/usr/bin/which",
		"/opt/verapdf",
		"/etc/java-17-openjdk",
		"/proc",
		"/sys/fs/cgroup",
		"/dev/random",
	})
}

func TestVerapdfEnginesAreTheMeasuredOnes(t *testing.T) {
	requireMeasuredSet(t, "verapdfEngines", verapdfEngines, []string{"verapdf"})
}

// D-10: veraPDF's grant is its own and narrower than soffice's. Its writable
// set is the null device and nothing else (the job directory is implicit),
// and its read-only set is a separate var, never soffice's.
func TestVerapdfGrantIsItsOwn(t *testing.T) {
	for _, p := range verapdfReadWrite {
		if p != "/dev/null" {
			t.Errorf("verapdfReadWrite carries %q: veraPDF may write nowhere but the job directory and /dev/null (D-10)", p)
		}
	}
	if slices.Equal(verapdfReadOnly, sofficeReadOnly) {
		t.Error("verapdfReadOnly equals sofficeReadOnly: the veraPDF ruleset must be built from its own var (D-10)")
	}
	for _, p := range sofficeReadOnly {
		if strings.HasPrefix(p, "/usr/bin/") && slices.Contains(verapdfReadOnly, p) &&
			p != "/usr/bin/dash" && p != "/usr/bin/dirname" {
			t.Errorf("verapdfReadOnly grants soffice's helper binary %q, which veraPDF was measured not to need", p)
		}
	}
	if slices.Contains(verapdfReadOnly, "/usr/share/fonts") || slices.Contains(verapdfReadOnly, "/etc/libreoffice") {
		t.Error("verapdfReadOnly carries a soffice-only path (fonts or the LibreOffice configuration)")
	}
}

// Neither engine is granted /usr/bin as a directory: only the named
// binaries it was measured to exec.
func TestNeitherEngineIsGrantedUsrBinWholesale(t *testing.T) {
	for name, set := range map[string][]string{"sofficeReadOnly": sofficeReadOnly, "verapdfReadOnly": verapdfReadOnly} {
		for _, p := range set {
			if p == "/usr/bin" || p == "/usr/bin/" {
				t.Errorf("%s grants %q wholesale", name, p)
			}
		}
	}
}

// The type stays comparable: a nil helper equals the zero value, a confined
// converter does not, and the type is usable as a map key.
func TestLibreOfficeConverterStaysComparable(t *testing.T) {
	var a, b LibreOfficeConverter
	if a != b {
		t.Fatal("two zero converters differ")
	}
	if got := (LibreOfficeConverter{}).WithSandbox(nil); got != a {
		t.Error("WithSandbox(nil) does not equal the zero value")
	}
	confined := LibreOfficeConverter{}.WithSandbox([]string{"/usr/local/bin/document-worker", "__sandbox-exec"})
	if confined == a {
		t.Error("a confined converter equals an unconfined one, so Sandbox is not being compared at all")
	}
	seen := map[LibreOfficeConverter]bool{a: true}
	if !seen[b] {
		t.Error("the zero converter is not its own map key")
	}
}

// ConfinedDocument, on the route 70-02 measured (soffice's grant includes
// /tmp read-write): a confined converter above one task at a time is refused,
// naming the variable and the shared /tmp; at one it wires the sweep; an
// unconfined one gets neither. It never calls Convert.
func TestConfinedDocument(t *testing.T) {
	helper, calls := fakeSandboxHelper(t)

	if _, err := ConfinedDocument(helper, 2, "DOCUMENT_WORKER_CONCURRENCY"); err == nil ||
		!strings.Contains(err.Error(), "DOCUMENT_WORKER_CONCURRENCY=2") ||
		!strings.Contains(err.Error(), "shared /tmp") {
		t.Errorf("ConfinedDocument(helper, 2, ...) err = %v, want it to name DOCUMENT_WORKER_CONCURRENCY=2 and the shared /tmp", err)
	}

	// asynq runs runtime.NumCPU() tasks for a value below 1, so 0 and -1 are
	// not "one at a time" and are refused like any other value above 1.
	for _, n := range []int{0, -1} {
		want := fmt.Sprintf("DOCUMENT_WORKER_CONCURRENCY=%d", n)
		if _, err := ConfinedDocument(helper, n, "DOCUMENT_WORKER_CONCURRENCY"); err == nil ||
			!strings.Contains(err.Error(), want) ||
			!strings.Contains(err.Error(), "shared /tmp") {
			t.Errorf("ConfinedDocument(helper, %d, ...) err = %v, want it to name %s and the shared /tmp", n, err, want)
		}
	}

	c, err := ConfinedDocument(helper, 1, "DOCUMENT_WORKER_CONCURRENCY")
	if err != nil {
		t.Fatalf("ConfinedDocument(helper, 1, ...): %v", err)
	}
	if c.sharedTmp != sharedTmpDir {
		t.Errorf("confined at concurrency 1: sharedTmp = %q, want %q", c.sharedTmp, sharedTmpDir)
	}

	for _, n := range []int{-1, 0, 1, 2, 4} {
		c, err := ConfinedDocument(nil, n, "DOCUMENT_WORKER_CONCURRENCY")
		if err != nil {
			t.Fatalf("ConfinedDocument(nil, %d, ...): %v", n, err)
		}
		if c.sharedTmp != "" {
			t.Errorf("unconfined at %d: sharedTmp = %q, want empty (nothing to sweep, and a sweep would delete a concurrent task's files)", n, c.sharedTmp)
		}
	}

	if got := calls(); len(got) != 0 {
		t.Errorf("ConfinedDocument invoked the helper %d time(s); it must never run an engine", len(got))
	}
}

// DocumentPreflight on a zero (unconfined) converter returns nil without
// creating a preflight workspace at all.
func TestDocumentPreflightUnconfinedProvesNothing(t *testing.T) {
	pattern := filepath.Join(os.TempDir(), "document-preflight-job-*")
	before, _ := filepath.Glob(pattern)

	if err := DocumentPreflight(context.Background(), LibreOfficeConverter{}); err != nil {
		t.Errorf("DocumentPreflight on an unconfined converter: %v, want nil", err)
	}

	after, _ := filepath.Glob(pattern)
	if len(after) != len(before) {
		t.Errorf("DocumentPreflight created a workspace (%v -> %v) though the converter is unconfined", before, after)
	}
}

// A confined converter built WITHOUT ConfinedDocument has not wired the sweep:
// the preflight refuses before creating any workspace.
func TestDocumentPreflightRefusesAConfinedConverterThatDoesNotSweep(t *testing.T) {
	helper, calls := fakeSandboxHelper(t)
	pattern := filepath.Join(os.TempDir(), "document-preflight-job-*")
	before, _ := filepath.Glob(pattern)

	err := DocumentPreflight(context.Background(), LibreOfficeConverter{}.WithSandbox(helper))
	if err == nil || !strings.Contains(err.Error(), "sweep") {
		t.Errorf("DocumentPreflight err = %v, want it to mention the sweep", err)
	}
	after, _ := filepath.Glob(pattern)
	if len(after) != len(before) {
		t.Errorf("DocumentPreflight created a workspace before refusing (%v -> %v)", before, after)
	}
	if got := calls(); len(got) != 0 {
		t.Errorf("DocumentPreflight ran an engine (%d helper call(s)) before refusing", len(got))
	}
}

// CONF-04 adjacency: the preflight's outside directory lies beneath no path
// of either engine's sets -- socket directories included -- and the check that
// proves it is not vacuous (a directory under /tmp is refused, because both
// soffice grants name /tmp).
func TestDocumentPreflightOutsideBaseIsOutsideEveryGrant(t *testing.T) {
	sets := [][]string{sofficeReadWrite, sofficeSocketDirs, sofficeReadOnly, verapdfReadWrite, verapdfReadOnly}

	w, cleanup, err := newPreflightWorkspaceOutside("document", documentPreflightOutsideBase)
	if err != nil {
		t.Skipf("no %s on this host: %v", documentPreflightOutsideBase, err)
	}
	defer cleanup()
	if err := w.requireOutsideGrants(sets...); err != nil {
		t.Errorf("the outside directory under %s is not outside every grant: %v", documentPreflightOutsideBase, err)
	}

	bad, cleanupBad, err := newPreflightWorkspaceOutside("document", "/tmp")
	if err != nil {
		t.Skipf("no /tmp on this host: %v", err)
	}
	defer cleanupBad()
	if err := bad.requireOutsideGrants(sets...); err == nil {
		t.Error("requireOutsideGrants accepted an outside directory under /tmp, which soffice is granted (non-vacuity)")
	}
}

// fakeSoffice plays soffice: it writes <outdir>/in.pdf carrying the PDF/A
// OutputIntent marker, and dumps its environment.
func fakeSofficeScript(envDump string) string {
	return `outdir=""
prev=""
for a in "$@"; do
  if [ "$prev" = "--outdir" ]; then outdir="$a"; fi
  prev="$a"
done
env > '` + envDump + `'
printf '%%PDF-1.7\n/GTS_PDFA1\n' > "$outdir/in.pdf"
`
}

// fakeVeraPDF plays veraPDF: it prints the committed compliant report, and
// dumps its environment.
func fakeVeraPDFScript(t *testing.T, envDump string) string {
	t.Helper()
	report, err := filepath.Abs(filepath.Join("testdata", "verapdf_compliant.mrr.xml"))
	if err != nil {
		t.Fatalf("locate the compliant veraPDF report: %v", err)
	}
	return "env > '" + envDump + "'\ncat '" + report + "'\n"
}

// helperInvocations splits the fake helper's recorded calls into (tokens
// before "--", command after it), one entry per call.
func helperInvocations(t *testing.T, calls func() [][]string) (tokens [][]string, commands []string) {
	t.Helper()
	for _, argv := range calls() {
		sep := slices.Index(argv, "--")
		if sep < 0 || sep+1 >= len(argv) {
			t.Fatalf("helper call without a -- separator and a command: %v", argv)
		}
		tokens = append(tokens, argv[1:sep])
		commands = append(commands, argv[sep+1])
	}
	return tokens, commands
}

func tokensWith(prefix string, tokens []string) []string {
	var out []string
	for _, tok := range tokens {
		if strings.HasPrefix(tok, prefix) {
			out = append(out, strings.TrimPrefix(tok, prefix))
		}
	}
	return out
}

// runFakeDocumentConvert runs a confined PDF/A Convert against fake soffice
// and veraPDF engines and returns the job directory, the helper's recorded
// calls and the two environment dumps' paths.
func runFakeDocumentConvert(t *testing.T) (jobDir string, calls func() [][]string, sofficeEnvDump, verapdfEnvDump string) {
	t.Helper()
	helper, calls := fakeSandboxHelper(t)
	binDir := t.TempDir()
	dumps := t.TempDir()
	sofficeEnvDump = filepath.Join(dumps, "soffice.env")
	verapdfEnvDump = filepath.Join(dumps, "verapdf.env")
	fakeEngine(t, binDir, "soffice", fakeSofficeScript(sofficeEnvDump))
	fakeEngine(t, binDir, "verapdf", fakeVeraPDFScript(t, verapdfEnvDump))
	pathWith(t, binDir)

	jobDir = t.TempDir()
	inPath := filepath.Join(jobDir, "in.docx")
	if err := os.WriteFile(inPath, []byte("not read by the fake"), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}
	c := LibreOfficeConverter{}.WithSandbox(helper)
	err := c.Convert(context.Background(), inPath, filepath.Join(jobDir, "out.pdf"),
		map[string]any{"pdf_profile": pdfProfileA2b})
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	return jobDir, calls, sofficeEnvDump, verapdfEnvDump
}

// Each engine runs under its OWN ruleset: soffice's carries the job
// directory, its measured writable and socket sets and every read-only entry;
// veraPDF's carries the job directory, /dev/null, its own read-only entries
// and nothing that only soffice needs -- and the two are built separately,
// once each.
func TestLibreOfficeConvertRunsEachEngineUnderItsOwnRuleset(t *testing.T) {
	jobDir, calls, _, _ := runFakeDocumentConvert(t)

	tokens, commands := helperInvocations(t, calls)
	if len(tokens) != 2 {
		t.Fatalf("helper invoked %d time(s), want 2 (soffice, then veraPDF): %v", len(tokens), calls())
	}
	if filepath.Base(commands[0]) != "soffice" || filepath.Base(commands[1]) != "verapdf" {
		t.Fatalf("commands after -- = %v, want soffice then verapdf", commands)
	}

	sRW, sSock, sRO := tokensWith("rw:", tokens[0]), tokensWith("sock:", tokens[0]), tokensWith("ro:", tokens[0])
	wantSRW := append([]string{jobDir}, sofficeReadWrite...)
	if !slices.Equal(sRW, wantSRW) {
		t.Errorf("soffice rw tokens = %v, want %v", sRW, wantSRW)
	}
	if !slices.Equal(sSock, sofficeSocketDirs) {
		t.Errorf("soffice sock tokens = %v, want %v", sSock, sofficeSocketDirs)
	}
	for _, want := range sofficeReadOnly {
		if !slices.Contains(sRO, want) {
			t.Errorf("soffice ro tokens %v miss %q", sRO, want)
		}
	}
	if !slices.Contains(sRO, commands[0]) {
		t.Errorf("soffice ro tokens %v do not grant the resolved engine %q", sRO, commands[0])
	}

	vRW, vSock, vRO := tokensWith("rw:", tokens[1]), tokensWith("sock:", tokens[1]), tokensWith("ro:", tokens[1])
	wantVRW := append([]string{jobDir}, verapdfReadWrite...)
	if !slices.Equal(vRW, wantVRW) {
		t.Errorf("veraPDF rw tokens = %v, want %v (D-10: the job directory and /dev/null only)", vRW, wantVRW)
	}
	if len(vSock) != 0 {
		t.Errorf("veraPDF carries sock tokens %v: only soffice binds a socket", vSock)
	}
	wantVRO := append(slices.Clone(verapdfReadOnly), commands[1])
	if !slices.Equal(vRO, wantVRO) {
		t.Errorf("veraPDF ro tokens = %v, want its own set plus the resolved engine %v", vRO, wantVRO)
	}
	for _, p := range []string{"/usr/bin/basename", "/usr/bin/ls", "/usr/bin/sed", "/usr/share/fonts", "/etc/libreoffice", "/dev/urandom"} {
		if slices.Contains(vRO, p) {
			t.Errorf("veraPDF's ruleset grants the soffice-only path %q", p)
		}
	}
}

// Neither engine gets the worker's secrets; soffice gets TMPDIR inside the
// job directory, veraPDF the same plus the JVM options that keep every JVM
// scratch write there; the worker's own environment is untouched.
func TestDocumentEnginesGetAJobScopedTmpAndNoSecrets(t *testing.T) {
	t.Setenv("DATABASE_URL", "secret-canary")
	t.Setenv("S3_SECRET_KEY", "secret-canary")
	tmpBefore, tmpWasSet := os.LookupEnv("TMPDIR")

	jobDir, _, sofficeDump, verapdfDump := runFakeDocumentConvert(t)
	jobTmp := filepath.Join(jobDir, documentEngineTmpDir)

	read := func(path string) string {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read env dump %s: %v", path, err)
		}
		return string(b)
	}
	soffice, verapdf := read(sofficeDump), read(verapdfDump)

	for name, dump := range map[string]string{"soffice": soffice, "veraPDF": verapdf} {
		for _, secret := range []string{"DATABASE_URL", "S3_SECRET_KEY", "secret-canary"} {
			if strings.Contains(dump, secret) {
				t.Errorf("%s's environment carries %q:\n%s", name, secret, dump)
			}
		}
		if !strings.Contains(dump, "TMPDIR="+jobTmp+"\n") {
			t.Errorf("%s's environment lacks TMPDIR=%s:\n%s", name, jobTmp, dump)
		}
	}
	if strings.Contains(soffice, "JAVA_OPTS") {
		t.Errorf("soffice's environment carries JAVA_OPTS:\n%s", soffice)
	}
	for _, want := range []string{"JAVA_OPTS=", "-Djava.io.tmpdir=" + jobTmp, "-XX:-UsePerfData"} {
		if !strings.Contains(verapdf, want) {
			t.Errorf("veraPDF's environment lacks %q:\n%s", want, verapdf)
		}
	}
	if st, err := os.Stat(jobTmp); err != nil || !st.IsDir() {
		t.Errorf("the job's tmp directory %s was not created: %v", jobTmp, err)
	}

	tmpAfter, tmpIsSet := os.LookupEnv("TMPDIR")
	if tmpBefore != tmpAfter || tmpWasSet != tmpIsSet {
		t.Errorf("the worker's own TMPDIR changed from %q to %q", tmpBefore, tmpAfter)
	}
}

// A helper exit of 78 means the engine never ran. ValidatePDFA words a run
// with no stdout as a pdf/a validation error, but the sentinel stays reachable
// inside it so isDocumentTerminal can tell a node fault from a bad document.
func TestValidatePDFAKeepsTheSandboxSentinel(t *testing.T) {
	binDir := t.TempDir()
	fakeEngine(t, binDir, "verapdf", "echo unreachable\n")
	pathWith(t, binDir)

	s := &engineSandbox{Helper: failingSandboxHelper(t, 78), Confine: true}
	x := s.forJobRW(t.TempDir(), verapdfReadWrite, verapdfReadOnly, verapdfEngines)

	err := ValidatePDFA(context.Background(), x, "/x.pdf")
	if err == nil {
		t.Fatal("ValidatePDFA succeeded though the helper exited 78")
	}
	if !errors.Is(err, ErrEngineSandboxUnavailable) {
		t.Errorf("err = %v, want it to wrap ErrEngineSandboxUnavailable", err)
	}
	if !strings.Contains(err.Error(), "pdf/a validation error") {
		t.Errorf("err = %v, want the pdf/a validation error text (a terminal signature the classifier must not act on)", err)
	}

	// Control: any other helper exit keeps the text but not the sentinel.
	s2 := &engineSandbox{Helper: failingSandboxHelper(t, 1), Confine: true}
	err = ValidatePDFA(context.Background(), s2.forJobRW(t.TempDir(), verapdfReadWrite, verapdfReadOnly, verapdfEngines), "/x.pdf")
	if err == nil || errors.Is(err, ErrEngineSandboxUnavailable) {
		t.Errorf("exit 1: err = %v, want a plain validation error without the sentinel", err)
	}
}

func TestVerapdfArgsIsTheProductionArgv(t *testing.T) {
	want := []string{"-f", "2b", "--format", "xml", "/x.pdf"}
	if got := verapdfArgs("/x.pdf"); !slices.Equal(got, want) {
		t.Errorf("verapdfArgs = %v, want %v", got, want)
	}
}

func writeTestZip(t *testing.T, path, part, content string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create(part)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestZipPartContains(t *testing.T) {
	dir := t.TempDir()
	marker := []byte("octoconv-preflight-abc")

	with := filepath.Join(dir, "with.odt")
	writeTestZip(t, with, "content.xml", "<x>octoconv-preflight-abc</x>")
	if found, err := zipPartContains(with, "content.xml", marker); err != nil || !found {
		t.Errorf("marker present: found=%v err=%v, want true, nil", found, err)
	}

	without := filepath.Join(dir, "without.odt")
	writeTestZip(t, without, "content.xml", "<x>something else</x>")
	if found, err := zipPartContains(without, "content.xml", marker); err != nil || found {
		t.Errorf("marker absent: found=%v err=%v, want false, nil", found, err)
	}

	// The marker in another part does not count: the part named is the one judged.
	other := filepath.Join(dir, "other.odt")
	writeTestZip(t, other, "meta.xml", "octoconv-preflight-abc")
	if _, err := zipPartContains(other, "content.xml", marker); err == nil {
		t.Error("a zip without the named part gave no error")
	}

	if _, err := zipPartContains(filepath.Join(dir, "missing.odt"), "content.xml", marker); err == nil {
		t.Error("a missing file gave no error")
	}
	notZip := filepath.Join(dir, "plain.odt")
	if err := os.WriteFile(notZip, []byte("this is not a zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := zipPartContains(notZip, "content.xml", marker); err == nil {
		t.Error("a non-zip gave no error")
	}
}

// The preflight fixture is the part list 70-02 measured, carries the marker
// as the paragraph text, and differs per marker.
func TestDocumentPreflightDOCXCarriesTheMarker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "in.docx")
	if err := os.WriteFile(path, documentPreflightDOCX("octoconv-preflight-test"), 0o600); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("the fixture is not a zip: %v", err)
	}
	defer zr.Close()
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	if want := []string{"[Content_Types].xml", "_rels/.rels", "word/document.xml"}; !slices.Equal(names, want) {
		t.Errorf("fixture parts = %v, want %v", names, want)
	}
	if found, err := zipPartContains(path, "word/document.xml", []byte("octoconv-preflight-test")); err != nil || !found {
		t.Errorf("word/document.xml does not carry the marker: found=%v err=%v", found, err)
	}
}

// A denied veraPDF read yields a report with an exception and no isCompliant
// attribute; a verdict of either kind carries one. Not a parse of the report
// as a trustworthy verdict -- only whether veraPDF got as far as judging.
func TestVeraPDFHasVerdict(t *testing.T) {
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	if !veraPDFHasVerdict(read("verapdf_compliant.mrr.xml")) {
		t.Error("a compliant report has no verdict")
	}
	if !veraPDFHasVerdict(read("verapdf_noncompliant.mrr.xml")) {
		t.Error("a non-compliant report has no verdict")
	}
	denied := []byte(`<report><jobs><job><validationReport jobEndStatus="failed to parse">` +
		`<exceptionMessage>Exception: Couldn't parse stream caused by exception: /outside/in.pdf (Permission denied)</exceptionMessage>` +
		`</validationReport></job></jobs><batchSummary totalJobs="1" failedToParse="0"></batchSummary></report>`)
	if veraPDFHasVerdict(denied) {
		t.Error("a report with an exception and no isCompliant attribute counted as a verdict")
	}
	for _, junk := range [][]byte{nil, []byte(""), []byte("Permission denied"), []byte("<report><jobs></jobs></report>")} {
		if veraPDFHasVerdict(junk) {
			t.Errorf("%q counted as a verdict", junk)
		}
	}
}

// The sweep runs first: a stale entry left in the shared /tmp is gone before
// soffice starts, the job's own directory survives, and (with the sandbox
// fake in place) both engines still ran.
func TestLibreOfficeConvertSweepsTheSharedTmpFirst(t *testing.T) {
	helper, _ := fakeSandboxHelper(t)
	binDir := t.TempDir()
	dumps := t.TempDir()
	// The fake soffice fails loudly when the stale entry is still there.
	shared := t.TempDir()
	stale := filepath.Join(shared, "OSL_PIPE_stale")
	if err := os.WriteFile(stale, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeEngine(t, binDir, "soffice", `if [ -e '`+stale+`' ]; then echo "stale entry survived the sweep" >&2; exit 3; fi
`+fakeSofficeScript(filepath.Join(dumps, "soffice.env")))
	fakeEngine(t, binDir, "verapdf", fakeVeraPDFScript(t, filepath.Join(dumps, "verapdf.env")))
	pathWith(t, binDir)

	jobDir := filepath.Join(shared, "job")
	if err := os.Mkdir(jobDir, 0o700); err != nil {
		t.Fatal(err)
	}
	inPath := filepath.Join(jobDir, "in.docx")
	if err := os.WriteFile(inPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := LibreOfficeConverter{}.WithSandbox(helper)
	c.sharedTmp = shared
	if err := c.Convert(context.Background(), inPath, filepath.Join(jobDir, "out.pdf"),
		map[string]any{"pdf_profile": pdfProfileA2b}); err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("the stale entry survived the sweep: %v", err)
	}
	if _, err := os.Stat(filepath.Join(jobDir, "out.pdf")); err != nil {
		t.Errorf("the job's own output is gone: %v", err)
	}
}

// A sweep that cannot run is a node fault: retryable, never blamed on the file.
func TestLibreOfficeConvertSweepFaultIsRetryable(t *testing.T) {
	helper, calls := fakeSandboxHelper(t)
	jobDir := t.TempDir()
	c := LibreOfficeConverter{}.WithSandbox(helper)
	c.sharedTmp = filepath.Join(t.TempDir(), "does-not-exist")
	err := c.Convert(context.Background(), filepath.Join(jobDir, "in.docx"), filepath.Join(jobDir, "out.pdf"), nil)
	if !errors.Is(err, ErrEngineSandboxUnavailable) {
		t.Errorf("err = %v, want it to wrap ErrEngineSandboxUnavailable", err)
	}
	if got := calls(); len(got) != 0 {
		t.Errorf("an engine ran (%d helper call(s)) after the sweep failed", len(got))
	}
}

// Every documented failure text stays coupled to the classifier: a plain
// validation error is not swallowed by the new plumbing.
func TestDocumentValidationErrorTextIsUnchanged(t *testing.T) {
	err := validateReport([]byte("not xml"))
	if err == nil || !strings.Contains(err.Error(), "pdf/a validation error") {
		t.Errorf("validateReport(garbage) = %v, want the pdf/a validation error text", err)
	}
	if fmt.Sprint(verapdfArgs("p")) != "[-f 2b --format xml p]" {
		t.Errorf("verapdf argv drifted: %v", verapdfArgs("p"))
	}
}
