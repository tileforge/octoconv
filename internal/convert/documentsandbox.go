// Package convert (this file): documentsandbox.go is document's half of the
// engineExec contract -- two measured sets (soffice's and veraPDF's, each its
// own ruleset), two engine environments, the shared-/tmp guard and sweep, and
// the preflight that proves BOTH engines work inside the job directory and are
// denied a file outside it -- mirroring htmlsandbox.go (the shared /tmp) and
// probesandbox.go (several engines, one preflight).
package convert

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
)

// sofficeReadWrite is soffice's writable set beyond the job directory,
// MEASURED (SANDBOX-PATHS.md document section, scripts/document-sandbox-measure.sh,
// 70-02). soffice binds its single-instance socket under /tmp whatever TMPDIR
// says, and a socket-only grant on /tmp does not let it convert: it unlinks its
// own socket at exit and that needs a remove right, so /tmp is granted
// read-write too (check C1 proves neither entry is decorative). That makes this
// the CONF-06 shared-/tmp class: sharedTmpGuard holds a confined document
// worker to one task at a time, and Convert sweeps /tmp before every task.
// Re-measure with scripts/document-sandbox-measure.sh before ever changing it;
// do not edit it to fix a failing test.
var sofficeReadWrite = []string{
	"/tmp",
}

// sofficeSocketDirs are the directories beneath which soffice may create a
// Unix-domain socket and nothing else (the sock: token, 70-01's scoped-socket
// route). Together with sofficeReadWrite it is what soffice measured to need.
var sofficeSocketDirs = []string{
	"/tmp",
}

// sofficeReadOnly is what soffice reads beyond its own binary, the job
// directory and the two sets above, narrowed by removal one entry at a time
// (SANDBOX-PATHS.md, 70-02): the wrapper shell and the four helpers it runs,
// the libraries and program directory under /usr/lib, the LibreOffice
// configuration, the fonts, and the entropy device. The engine itself
// (/usr/bin/soffice) is granted through sofficeEngines, and the real binaries
// under /usr/lib/libreoffice by the /usr/lib entry.
var sofficeReadOnly = []string{
	"/usr/lib",
	"/usr/bin/dash",
	"/usr/bin/basename",
	"/usr/bin/dirname",
	"/usr/bin/ls",
	"/usr/bin/sed",
	"/etc/libreoffice",
	"/usr/share/fonts",
	"/dev/urandom",
}

// sofficeEngines is every binary LibreOfficeConverter.Convert hands to the
// ruleset: one soffice subprocess per job.
var sofficeEngines = []string{
	"soffice",
}

// verapdfReadWrite is veraPDF's writable set beyond the job directory: the
// null device and nothing else (D-10, measured). The JVM's temp files go to
// the job directory through -Djava.io.tmpdir, and -XX:-UsePerfData keeps it
// from creating its performance-data directory under /tmp, so the container's
// /tmp gains no entry from a veraPDF run.
var verapdfReadWrite = []string{
	"/dev/null",
}

// verapdfReadOnly is what veraPDF reads beyond its own script, the job
// directory and the null device, narrowed by removal (SANDBOX-PATHS.md, 70-02
// and 70-08): the JVM under /usr/lib, the wrapper shell and its two helpers,
// the veraPDF distribution, the JVM configuration, and the container's own view
// of its limits. That view is three /proc entries and /sys/fs/cgroup; without
// any of them the JVM's Operating System Metrics (-XshowSettings:system) stop
// matching the container (measured by V0 and V0L in
// scripts/document-sandbox-measure.sh):
//   - /proc/cgroups: the JVM's Java-side metrics provider reads it;
//   - /proc/self/cgroup and /proc/self/mountinfo: where the container's cgroup
//     is mounted.
//
// The two /proc/self entries are resolved by the ruleset's helper in ITS OWN
// pid when the ruleset is applied. They cover the JVM because the launcher
// script ends in `exec java` and the helper itself execs the launcher, so all
// three are one pid (measured: 70-08 LAUNCHER line). A launcher that forked
// the JVM would leave these two entries naming a process the JVM is not, and
// V0L would fail. The whole of /proc is not granted: that opened other
// processes' cmdline and status to the engine (70-08 baseline).
//
// /dev/random is kept on a latency measurement: without it the JVM's seeding
// falls back and every run is seconds slower. A separate, NARROWER grant than
// soffice's (D-10): none of soffice's helper binaries, no fonts, no LibreOffice
// configuration.
var verapdfReadOnly = []string{
	"/usr/lib",
	"/usr/bin/dash",
	"/usr/bin/dirname",
	"/usr/bin/which",
	"/opt/verapdf",
	"/etc/java-17-openjdk",
	"/proc/cgroups",
	"/proc/self/cgroup",
	"/proc/self/mountinfo",
	"/sys/fs/cgroup",
	"/dev/random",
}

// verapdfEngines is the one binary ValidatePDFA hands to its ruleset.
var verapdfEngines = []string{
	"verapdf",
}

// documentEngineTmpDir is the per-job directory (under the job directory)
// both engines' temp files go to. Measured: TMPDIR moves every soffice scratch
// write, and -Djava.io.tmpdir plus -XX:-UsePerfData every JVM one.
const documentEngineTmpDir = "tmp"

// documentPreflightOutsideBase is where DocumentPreflight places its outside
// fixture. soffice's grants name /tmp (read-write and socket), so
// newPreflightWorkspace's default (os.TempDir()) would put the outside file
// beneath a granted path and the preflight would refuse every start for having
// nothing to test. /var/tmp is world-writable in debian:bookworm-slim and lies
// outside every document grant; requireOutsideGrants proves that at run time
// rather than trusting this comment.
const documentPreflightOutsideBase = "/var/tmp"

// documentPreflightTmpDir is the job-relative temp directory as a path.
func documentPreflightTmpDir(jobDir string) string {
	return filepath.Join(jobDir, documentEngineTmpDir)
}

// requireNoWhitespaceJobDir refuses a job directory whose engine temp path
// contains whitespace. The veraPDF launcher script expands JAVA_OPTS unquoted,
// so verapdfEnv's -Djava.io.tmpdir=<dir> would be torn into separate JVM
// arguments. Quoting inside JAVA_OPTS cannot repair that -- the launcher's
// shell does not re-parse quotes in an expanded variable -- so the directory
// is refused with its cause named. Job directories come from os.MkdirTemp("")
// and so live under TMPDIR; that is where the operator fixes it. soffice fares
// no better in such a path (measured 70-07: a confined start with a whitespace
// TMPDIR produced no conversion within the 60 s preflight timeout), which is
// why the startup preflight applies this check to every confined start, not
// only to PDF/A jobs.
func requireNoWhitespaceJobDir(jobDir string) error {
	tmp := documentPreflightTmpDir(jobDir)
	if strings.ContainsFunc(tmp, unicode.IsSpace) {
		return fmt.Errorf("the job directory %q contains whitespace, and the veraPDF launcher "+
			"word-splits JAVA_OPTS, so -Djava.io.tmpdir would be torn apart; point TMPDIR at a path "+
			"without whitespace", tmp)
	}
	return nil
}

// sofficeEnv is the environment soffice runs with: the shared allowlist
// (PATH, HOME, LANG, LC_ALL, each when set) plus TMPDIR inside the job
// directory. The worker's own environment (DATABASE_URL, the S3 keys, ...) is
// never handed on, and never modified: this builds a list, it does not
// os.Setenv. That empties the engine's own /proc/self/environ; soffice's
// ruleset grants no /proc at all (SANDBOX-PATHS.md "/proc и другие процессы",
// 70-08), so other processes' entries are out of its reach by the grant.
func sofficeEnv(jobDir string) []string {
	return engineEnv(documentPreflightTmpDir(jobDir))
}

// verapdfEnv is the environment veraPDF runs with: sofficeEnv's list plus
// JAVA_OPTS, which the veraPDF launcher script passes to the JVM. A
// document-specific builder, so engineEnv's shared allowlist is not widened
// for a variable only this class needs. As for soffice, the scrub empties the
// engine's own /proc/self/environ. Other processes' entries were measured
// under the old whole-/proc grant (N3, 70-08): environ denied, cmdline and
// status open; the three-entry grant above opens none of them
// (SANDBOX-PATHS.md "/proc и другие процессы").
func verapdfEnv(jobDir string) []string {
	tmp := documentPreflightTmpDir(jobDir)
	return append(sofficeEnv(jobDir), "JAVA_OPTS=-Djava.io.tmpdir="+tmp+" -XX:-UsePerfData")
}

// WithSandbox returns c with Sandbox set to a *engineSandbox built from
// helper. A method rather than a constructor, mirroring
// ChromiumConverter.WithSandbox -- ConfinedDocument (below) is the one that
// also wires the concurrency guard and the sweep.
func (c LibreOfficeConverter) WithSandbox(helper []string) LibreOfficeConverter {
	c.Sandbox = newEngineSandbox(helper)
	return c
}

// sofficeExec builds soffice's ruleset for one job: the job directory, the
// measured sets above, the resolved soffice, and an engine environment scrubbed
// of the worker's secrets.
func (c LibreOfficeConverter) sofficeExec(jobDir string) engineExec {
	return c.Sandbox.forJobRWSock(jobDir, sofficeReadWrite, sofficeSocketDirs, sofficeReadOnly, sofficeEngines).
		withEnv(sofficeEnv(jobDir))
}

// verapdfExec builds veraPDF's ruleset for one job: its OWN, narrower set
// (D-10), never soffice's.
func (c LibreOfficeConverter) verapdfExec(jobDir string) engineExec {
	return c.Sandbox.forJobRW(jobDir, verapdfReadWrite, verapdfReadOnly, verapdfEngines).
		withEnv(verapdfEnv(jobDir))
}

// ConfinedDocument builds a LibreOfficeConverter confined by helper, enforcing
// the shared-/tmp guard (a confined worker whose soffice ruleset grants /tmp
// read-write must run at most one task at a time) BEFORE returning anything
// usable, and wiring the sweep of that /tmp for a confined converter.
//
// The unconfined path (helper empty, the escape hatch) gets neither the guard
// nor the sweep: an unconfined engine can already read and write everything in
// the container, so refusing concurrency there would protect nothing, and
// sweeping a shared /tmp at concurrency above 1 would delete a CONCURRENT
// task's files out from under it.
func ConfinedDocument(helper []string, concurrency int, concurrencyEnv string) (LibreOfficeConverter, error) {
	c := LibreOfficeConverter{}.WithSandbox(helper)
	if err := sharedTmpGuard(sofficeReadWrite, c.Sandbox, concurrency, concurrencyEnv); err != nil {
		return LibreOfficeConverter{}, err
	}
	if c.Sandbox.confines() && slices.Contains(sofficeReadWrite, sharedTmpDir) {
		c.sharedTmp = sharedTmpDir
	}
	return c, nil
}

// zipPartContains reports whether the named part of the zip archive at path
// contains marker. The part is read through io.LimitReader (16 MiB), so a
// hostile or corrupt archive cannot make the preflight allocate without bound.
// An unreadable path, a non-zip or a missing part is an error: the caller
// decides what that means (the preflight's negative run reads it as a clean
// denial).
func zipPartContains(path, part string, marker []byte) (bool, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return false, fmt.Errorf("open %s as a zip: %w", path, err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != part {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return false, fmt.Errorf("open part %s of %s: %w", part, path, err)
		}
		defer rc.Close()
		data, err := io.ReadAll(io.LimitReader(rc, 16<<20))
		if err != nil {
			return false, fmt.Errorf("read part %s of %s: %w", part, path, err)
		}
		return bytes.Contains(data, marker), nil
	}
	return false, fmt.Errorf("%s has no part %s", path, part)
}

// documentPreflightDOCX builds the smallest DOCX LibreOffice 7.4 converts with
// the marker intact (70-02 measured the part list): the content types, the
// package relationship to the main part, and the main part with one paragraph
// carrying marker. Built with archive/zip directly -- this is production code,
// not a test helper -- and byte-for-byte deterministic for a given marker.
func documentPreflightDOCX(marker string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	write := func(name, content string) {
		// Writing into an in-memory bytes.Buffer through archive/zip cannot
		// fail short of an out-of-memory condition, the same convention
		// probePreflightDOCX already relies on, so the error is not checked.
		w, _ := zw.Create(name)
		_, _ = io.WriteString(w, content)
	}
	write("[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
		`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`+
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`+
		`<Default Extension="xml" ContentType="application/xml"/>`+
		`<Override PartName="/word/document.xml" ContentType="`+probePreflightDOCXContentType+`"/>`+
		`</Types>`)
	write("_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>`+
		`</Relationships>`)
	write("word/document.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`+
		`<w:body><w:p><w:r><w:t>`+marker+`</w:t></w:r></w:p></w:body></w:document>`)
	_ = zw.Close()
	return buf.Bytes()
}

// DocumentPreflight proves BOTH document engines actually run under the
// rulesets c builds for a job, and are denied a file outside the job
// directory, before any job depends on it -- judged by CONTENT in every
// direction (CONF-04, D-11), never by an engine's exit status:
//
//   - soffice was measured to exit 0 and write nothing when denied (silent),
//     while a conversion that merely exits 0 might also have written something
//     unrelated;
//   - veraPDF exits 7 for a non-compliant verdict and for a denial alike, so its
//     status says nothing either way.
//
// Sufficiency comes first and is what makes the negatives evidence: the SAME
// argv under the SAME ruleset succeeded on the inside copy before the outside
// copy is judged, so a failure outside is attributable to location alone.
//
// nil when c is unconfined: nothing to prove for a deployment that has
// deliberately turned confinement off.
func DocumentPreflight(ctx context.Context, c LibreOfficeConverter) error {
	if !c.Sandbox.confines() {
		return nil
	}
	// A confined converter whose soffice ruleset grants the shared /tmp MUST
	// have been built with the sweep wired (ConfinedDocument): checked before
	// any workspace exists, because a preflight run without the sweep would
	// itself be racing whatever a prior task left in /tmp.
	if slices.Contains(sofficeReadWrite, sharedTmpDir) && c.sharedTmp == "" {
		return fmt.Errorf("a confined document converter must sweep the shared /tmp before every task (CONF-06); " +
			"build it with ConfinedDocument")
	}

	w, cleanup, err := newPreflightWorkspaceOutside("document", documentPreflightOutsideBase)
	if err != nil {
		return err
	}
	defer cleanup()

	// Before any engine runs: a TMPDIR the engines cannot use is named here
	// rather than surfacing as a soffice timeout (IN-02).
	if err := requireNoWhitespaceJobDir(w.jobDir); err != nil {
		return err
	}

	if err := w.requireOutsideGrants(sofficeReadWrite, sofficeSocketDirs, sofficeReadOnly,
		verapdfReadWrite, verapdfReadOnly); err != nil {
		return err
	}

	marker, err := preflightMarker()
	if err != nil {
		return err
	}
	inside, outside, err := w.place("in.docx", documentPreflightDOCX(marker))
	if err != nil {
		return err
	}

	if err := documentPreflightSufficiency(ctx, c, w, inside, marker); err != nil {
		return err
	}
	return documentPreflightEffectiveness(ctx, c, w, outside, marker)
}

// documentPreflightSufficiency proves both engines succeed INSIDE the job
// directory, through the production paths. soffice's checks come before
// veraPDF's.
func documentPreflightSufficiency(ctx context.Context, c LibreOfficeConverter, w preflightWorkspace, inside, marker string) error {
	// (a) soffice: the production Convert of the DOCX to ODT, judged by the
	// marker in the produced content.xml.
	outODT := filepath.Join(w.jobDir, "out.odt")
	if err := c.Convert(ctx, inside, outODT, nil); err != nil {
		return fmt.Errorf("the engines cannot convert under the sandbox: %w", err)
	}
	found, err := zipPartContains(outODT, "content.xml", []byte(marker))
	if err != nil {
		return fmt.Errorf("the engines cannot convert under the sandbox: %w", err)
	}
	if !found {
		return fmt.Errorf("the engines cannot convert under the sandbox: " +
			"content.xml of the converted document does not carry the marker")
	}

	// (b) soffice and veraPDF together: the full production PDF/A path (soffice,
	// the /GTS_PDFA pre-filter, ValidatePDFA under the veraPDF ruleset).
	outPDF := filepath.Join(w.jobDir, "out.pdf")
	if err := c.Convert(ctx, inside, outPDF, map[string]any{"pdf_profile": pdfProfileA2b}); err != nil {
		return fmt.Errorf("the engines cannot convert under the sandbox: %w", err)
	}

	// (c) veraPDF alone, directly, on the inside copy: the verdict must be
	// compliant. The job's tmp directory exists already (Convert made it), but a
	// preflight that reaches here must not depend on that.
	if err := os.MkdirAll(documentPreflightTmpDir(w.jobDir), 0o700); err != nil {
		return fmt.Errorf("the engines cannot convert under the sandbox: %w", err)
	}
	report, _ := c.verapdfExec(w.jobDir).run(ctx, "verapdf", verapdfArgs(outPDF)...)
	compliant, _, err := parseVeraPDFReport(report)
	if err != nil {
		return fmt.Errorf("the engines cannot convert under the sandbox: veraPDF gave no verdict on the inside copy: %w", err)
	}
	if !compliant {
		return fmt.Errorf("the engines cannot convert under the sandbox: veraPDF called the inside PDF/A non-compliant")
	}
	return nil
}

// documentPreflightEffectiveness proves both engines are DENIED the identical
// bytes placed OUTSIDE the job directory, under the SAME rulesets.
func documentPreflightEffectiveness(ctx context.Context, c LibreOfficeConverter, w preflightWorkspace, outside, marker string) error {
	// (d) soffice. Convert is a valid negative here because it hands inPath to
	// soffice without copying it into the job directory (unlike chromium's), so
	// the engine itself is pointed at the outside file through the production
	// path. Its error is deliberately NOT judged (70-02 measured the denial as
	// silent: exit 0, no output file -- which the production converter turns
	// into an error, but a denial that wrote something would not be): only
	// content decides. A missing or unreadable output, or one that is not a zip,
	// is a clean denial.
	negODT := filepath.Join(w.jobDir, "neg.odt")
	_ = c.Convert(ctx, outside, negODT, nil)
	if found, err := zipPartContains(negODT, "content.xml", []byte(marker)); err == nil && found {
		return fmt.Errorf("an engine read a file outside the job directory: "+
			"soffice converted %s and its content.xml carries the marker", outside)
	}

	// (e) veraPDF, on the bytes of the PDF/A the sufficiency run produced,
	// placed outside every grant. A verdict of EITHER kind about that file
	// means veraPDF read it; a denial produces a report with no verdict at all
	// (measured: no isCompliant attribute). Its exit status is not judged.
	pdfa, err := os.ReadFile(filepath.Join(w.jobDir, "out.pdf"))
	if err != nil {
		return fmt.Errorf("preflight: read back the PDF/A fixture: %w", err)
	}
	_, outsidePDF, err := w.place("pdfa.pdf", pdfa)
	if err != nil {
		return err
	}
	report, _ := c.verapdfExec(w.jobDir).run(ctx, "verapdf", verapdfArgs(outsidePDF)...)
	if veraPDFHasVerdict(report) {
		return fmt.Errorf("an engine read a file outside the job directory: " +
			"veraPDF gave a verdict about a PDF/A outside every grant")
	}
	return nil
}

// veraPDFHasVerdict reports whether a veraPDF machine-readable report carries
// a compliance verdict of either kind -- an isCompliant attribute on a
// validation report. A denied read produces a report with an exception message
// and no such attribute (measured, 70-02 N2), and a report that is not XML at
// all carries none either. Deliberately independent of parseVeraPDFReport:
// that function answers "is this a trustworthy verdict", this one answers "did
// veraPDF get as far as judging the file", and the preflight's negative run
// needs the second.
func veraPDFHasVerdict(raw []byte) bool {
	var report struct {
		XMLName xml.Name `xml:"report"`
		Jobs    struct {
			Job []struct {
				ValidationReport struct {
					IsCompliant *string `xml:"isCompliant,attr"`
				} `xml:"validationReport"`
			} `xml:"job"`
		} `xml:"jobs"`
	}
	if err := xml.Unmarshal(raw, &report); err != nil {
		return false
	}
	for _, j := range report.Jobs.Job {
		if j.ValidationReport.IsCompliant != nil {
			return true
		}
	}
	return false
}
