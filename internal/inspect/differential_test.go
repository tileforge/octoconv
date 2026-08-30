package inspect

// D-10. This file runs BOTH text-layer detectors over one corpus and records
// what each says, because the alternative -- reasoning about how pdftotext
// behaves -- is a defect class this project has already paid for.
//
// The framing is D-08 and it decides how the results must be read: the WORKER
// is authoritative and inspect is advisory. The worker runs pdftotext over the
// whole file at job time; inspect runs a bounded scanner inside a request. A
// difference between them is therefore a MEASUREMENT, not automatically a bug,
// and the table separates the two cases rather than summing them:
//
//	agree            -- same verdict
//	go_unknown       -- inspect honestly declined; pdftotext answered.
//	                    A declared limit of this build, not an error.
//	go_wrong_present -- inspect said present, the worker disagrees.
//	                    Dangerous: steers a client away from OCR it needs.
//	go_wrong_absent  -- inspect said absent, the worker found text.
//	                    The worst case: a claim of ABSENCE that is false,
//	                    which D-09 exists to make impossible.
//
// Only the last two fail the test. Failing on go_unknown would make the suite
// unmaintainable the moment a legitimately undecodable fixture is added -- and
// would push a future maintainer to weaken the whole thing rather than the one
// row that deserves it.

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// updateDivergence rewrites testdata/pdf/DIVERGENCE.md from the current run.
// Without it the test compares against the committed table and fails on any
// difference, so a change in either detector shows up as a diff in a
// human-readable artifact rather than as a number quietly moving inside a test.
var updateDivergence = flag.Bool("update-divergence", false, "rewrite testdata/pdf/DIVERGENCE.md from this run")

// requirePdftotextEnv makes a skipped differential run fail instead of pass.
//
// Skipping when pdftotext is absent is the right default -- it keeps `go test`
// hermetic on a machine without poppler. But a skipped run reported as "ok" is
// indistinguishable from a run that proved something, and this is the one suite
// in the phase whose whole purpose is to compare against an external tool. The
// phase gate sets this variable; CI should too, wherever poppler is installed.
const requirePdftotextEnv = "OCTOCONV_REQUIRE_PDFTOTEXT"

// divergenceClass is the four-way classification described in the file header.
type divergenceClass string

const (
	classAgree          divergenceClass = "agree"
	classGoUnknown      divergenceClass = "go_unknown"
	classGoWrongPresent divergenceClass = "go_wrong_present"
	classGoWrongAbsent  divergenceClass = "go_wrong_absent"
)

// divergenceRow is one fixture's line in the table.
type divergenceRow struct {
	Fixture    string
	GoVerdict  string
	GoReason   string
	GoCount    int
	WorkerVal  int
	WorkerSays string
	Class      divergenceClass
}

// pdftotextArgs duplicates internal/convert's ocrTextLayerArgs.
//
// It is a duplicate because internal/inspect may not import internal/convert
// (findings.go's layering note) and the original is unexported. The copy is
// pinned from the other side by convert.TestTextLayerArgvIsStable, which
// asserts the converter's own argv is exactly {path, "-"} -- so this file
// cannot silently start measuring a different invocation than the worker runs.
func pdftotextArgs(path string) []string { return []string{path, "-"} }

// workerTextLayerCount runs pdftotext exactly as the worker does and counts
// non-whitespace bytes exactly as the worker does.
func workerTextLayerCount(t *testing.T, path string) int {
	t.Helper()
	out, err := exec.Command("pdftotext", pdftotextArgs(path)...).Output()
	if err != nil {
		t.Fatalf("pdftotext %s: %v", path, err)
	}
	return countNonWhitespaceBytes(out)
}

// differentialCorpus is every PDF in both fixture directories, discovered by
// WALKING them rather than by listing names here: a fixture added later is
// measured automatically, and cannot be forgotten by whoever adds it.
func differentialCorpus(t *testing.T) []string {
	t.Helper()
	var paths []string
	for _, glob := range []string{
		filepath.Join("testdata", "pdf", "*.pdf"),
		filepath.Join("..", "convert", "testdata", "ocr", "*.pdf"),
	} {
		found, err := filepath.Glob(glob)
		if err != nil {
			t.Fatalf("glob %s: %v", glob, err)
		}
		paths = append(paths, found...)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		t.Fatal("differential corpus is empty -- the fixture directories moved")
	}
	return paths
}

func TestDifferentialTextLayerDetection(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		if os.Getenv(requirePdftotextEnv) != "" {
			t.Fatalf("pdftotext is not on PATH and %s is set -- the differential run is the point of this suite, and "+
				"a skip reported as a pass is how a gate stops meaning anything", requirePdftotextEnv)
		}
		t.Skipf("pdftotext not installed, skipping the differential run (set %s=1 to make this a failure)", requirePdftotextEnv)
	}

	var rows []divergenceRow
	for _, path := range differentialCorpus(t) {
		data := readFixture(t, path)

		tr := NewTracker(Budget{})
		res := AnalyzePDF(bytes.NewReader(data), int64(len(data)), defaultMaxBytesForTest(), tr)
		goVerdict, _ := res.Characteristics["text_layer"].(string)
		goReason, _ := res.Characteristics["text_layer_reason"].(string)

		// The count comes from the scan directly: the report only carries the
		// verdict, and 59-05 showed a verdict is too coarse to protect a count.
		state := scanPDFTextLayer(data, defaultMaxBytesForTest()/4, nil)

		workerCount := workerTextLayerCount(t, path)
		workerUsable := workerCount > pdfUsableTextLayerThreshold
		workerSays := TextLayerAbsent
		if workerUsable {
			workerSays = TextLayerPresent
		}

		row := divergenceRow{
			Fixture:    filepath.Base(path),
			GoVerdict:  goVerdict,
			GoReason:   goReason,
			GoCount:    state.nonWhitespace,
			WorkerVal:  workerCount,
			WorkerSays: workerSays,
		}
		switch {
		case goVerdict == "":
			row.GoVerdict = "unknown"
			row.Class = classGoUnknown
		case goVerdict == TextLayerPresent && !workerUsable:
			row.Class = classGoWrongPresent
		case goVerdict == TextLayerAbsent && workerUsable:
			row.Class = classGoWrongAbsent
		default:
			row.Class = classAgree
		}
		rows = append(rows, row)
	}

	for _, row := range rows {
		switch row.Class {
		case classGoWrongAbsent:
			t.Errorf("%s: inspect reported ABSENT while pdftotext extracted %d non-whitespace characters -- a false "+
				"claim of absence is the one outcome D-09 forbids outright; a client trusting it skips OCR it needs",
				row.Fixture, row.WorkerVal)
		case classGoWrongPresent:
			t.Errorf("%s: inspect reported PRESENT (count %d) while pdftotext found only %d, below the shared "+
				"threshold %d -- this steers a client away from the OCR that would actually serve them",
				row.Fixture, row.GoCount, row.WorkerVal, pdfUsableTextLayerThreshold)
		}
	}

	writeDivergenceTable(t, rows)
}

// writeDivergenceTable compares the run against the committed DIVERGENCE.md, or
// rewrites it under -update-divergence.
func writeDivergenceTable(t *testing.T, rows []divergenceRow) {
	t.Helper()
	const path = "testdata/pdf/DIVERGENCE.md"

	var b strings.Builder
	b.WriteString(divergenceHeader)
	b.WriteString("| Fixture | inspect | count | pdftotext | count | Class |\n")
	b.WriteString("|---|---|---:|---|---:|---|\n")
	for _, r := range rows {
		verdict := r.GoVerdict
		if r.GoReason != "" {
			verdict = fmt.Sprintf("%s (`%s`)", r.GoVerdict, r.GoReason)
		}
		fmt.Fprintf(&b, "| `%s` | %s | %d | %s | %d | `%s` |\n",
			r.Fixture, verdict, r.GoCount, r.WorkerSays, r.WorkerVal, r.Class)
	}
	got := b.String()

	if *updateDivergence {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		t.Logf("wrote %s (-update-divergence)", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v -- the divergence table is a committed artifact, not a generated side effect", path, err)
	}
	if string(want) != got {
		t.Fatalf("%s is out of date. Regenerate with:\n\n    go test ./internal/inspect/ -run TestDifferential -update-divergence\n\n"+
			"and review the diff: a moved number means one of the two detectors changed behavior.\n\ngot:\n%s", path, got)
	}
}

const divergenceHeader = `# Text-layer detection: measured divergence

Generated by ` + "`TestDifferentialTextLayerDetection`" + ` (internal/inspect/differential_test.go).
Regenerate with ` + "`go test ./internal/inspect/ -run TestDifferential -update-divergence`" + `.

**How to read this.** The worker is authoritative and inspect is advisory (D-08).
The worker runs ` + "`pdftotext`" + ` over the whole file at job time; inspect runs a bounded
scanner inside a request, over a byte window, without decoding every filter. A difference
between them is a measurement, not automatically a defect:

- ` + "`agree`" + ` — same verdict.
- ` + "`go_unknown`" + ` — inspect honestly declined to answer where pdftotext could. A declared
  limit of this build, and the intended behaviour when a stream cannot be decoded.
- ` + "`go_wrong_present`" + ` — inspect claimed a usable text layer the worker does not see.
  Steers a client away from OCR they need. Fails the test.
- ` + "`go_wrong_absent`" + ` — inspect claimed NO text layer while the worker found one. The one
  outcome D-09 forbids outright. Fails the test.

**Counts can differ while verdicts agree**, and that is the most interesting column here:
inspect counts the bytes inside text-operator string arguments, while pdftotext applies the
font's encoding and any ` + "`/ToUnicode`" + ` map and yields characters. For a multi-byte
encoding the two are simply different quantities.

`
