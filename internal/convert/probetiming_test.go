package convert

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestProbeTimingHarness is the measurement driver behind
// scripts/probe-timeout-measure.sh. It is a TEST rather than a command on
// purpose: what has to be timed is the exact path the worker takes, and a
// separate binary would have to re-assemble that path and could drift from
// it, which is precisely how a timeout ends up measuring something other than
// what runs in production.
//
// It calls SanitizeConverter.Convert, not SanitizeFile, for the same reason.
// Convert is what the worker's Runner invokes, and it does one thing more:
// it builds and writes the report. The first version of this harness called
// SanitizeFile and reported report_bytes=0 on every row -- the number was
// visibly wrong, which is the only reason it was noticed.
//
// It does nothing unless OCTOCONV_PROBE_CORPUS names a directory, so a normal
// `go test ./...` is unaffected.
//
// Output is one CSV line per file on stdout, for the script to aggregate:
//
//	file,format,in_bytes,out_bytes,report_bytes,seconds,status
//
// A file that fails to sanitize or verify is reported with its status and
// makes the whole run fail. A timeout measured on a corpus that partly does
// not sanitize is measuring the wrong thing.
func TestProbeTimingHarness(t *testing.T) {
	dir := os.Getenv("OCTOCONV_PROBE_CORPUS")
	if dir == "" {
		t.Skip("OCTOCONV_PROBE_CORPUS unset; this harness runs only under scripts/probe-timeout-measure.sh")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read corpus dir %s: %v", dir, err)
	}

	work := t.TempDir()
	measured, skipped, failed := 0, 0, 0

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		format := NormalizeFormat(strings.TrimPrefix(filepath.Ext(name), "."))
		if !SanitizeSupports(format) {
			skipped++
			fmt.Printf("SKIP,%s,%s,unsupported format\n", name, format)
			continue
		}

		in := filepath.Join(dir, name)
		fi, err := os.Stat(in)
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		out := filepath.Join(work, "out-"+name)

		// The worker names its downloaded input "in.<source_format>", and
		// SanitizeConverter derives the format from that name. Copy to the
		// same shape rather than passing the corpus filename through, so the
		// harness exercises the derivation too.
		staged := filepath.Join(work, "in."+format)
		blob, err := os.ReadFile(in)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if err := os.WriteFile(staged, blob, 0o600); err != nil {
			t.Fatalf("stage %s: %v", name, err)
		}

		start := time.Now()
		err = SanitizeConverter{}.Convert(context.Background(), staged, out, nil)
		elapsed := time.Since(start)

		if err != nil {
			failed++
			status := "error"
			switch {
			case errors.Is(err, ErrSanitizeResidue):
				status = "residue"
			case errors.Is(err, ErrSanitizeUnverifiable):
				status = "unverifiable"
			}
			fmt.Printf("ROW,%s,%s,%d,0,0,%.3f,%s\n", name, format, fi.Size(), elapsed.Seconds(), status)
			t.Errorf("%s: %v", name, err)
			continue
		}

		outSize := int64(0)
		if ofi, err := os.Stat(out); err == nil {
			outSize = ofi.Size()
		}
		reportSize := int64(0)
		if rfi, err := os.Stat(SanitizeReportPath(out)); err == nil {
			reportSize = rfi.Size()
		}

		measured++
		fmt.Printf("ROW,%s,%s,%d,%d,%d,%.3f,ok\n",
			name, format, fi.Size(), outSize, reportSize, elapsed.Seconds())
	}

	fmt.Printf("SUMMARY,measured=%d,skipped=%d,failed=%d\n", measured, skipped, failed)
	if measured == 0 {
		t.Fatalf("no file in %s was sanitizable -- a timeout measured on an empty corpus "+
			"measures nothing", dir)
	}
}
