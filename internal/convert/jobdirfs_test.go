package convert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func jobDirOutside(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func jobDirUntouched(t *testing.T, outside, want string) {
	t.Helper()
	got, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("the outside file changed: %q, want %q", got, want)
	}
}

func jobDirNewFile(t *testing.T, path, body string, perm fs.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("%s is %v, want a regular file", path, info.Mode().Type())
	}
	if perm != 0 && info.Mode().Perm() != perm {
		t.Errorf("%s mode = %v, want %v", path, info.Mode().Perm(), perm)
	}
	if body != "" {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != body {
			t.Errorf("%s = %q, want %q", path, got, body)
		}
	}
}

func TestWriteNewFileNeverWritesThroughAPlantedLink(t *testing.T) {
	t.Run("a symlink", func(t *testing.T) {
		outside := jobDirOutside(t, "untouched")
		path := filepath.Join(t.TempDir(), "out")
		if err := os.Symlink(outside, path); err != nil {
			t.Fatal(err)
		}
		if err := writeNewFile(path, []byte("body"), 0o600); err != nil {
			t.Fatalf("writeNewFile: %v", err)
		}
		jobDirUntouched(t, outside, "untouched")
		jobDirNewFile(t, path, "body", 0o600)
	})
	t.Run("a hard link", func(t *testing.T) {
		outside := jobDirOutside(t, "untouched")
		path := filepath.Join(t.TempDir(), "out")
		if err := os.Link(outside, path); err != nil {
			t.Fatal(err)
		}
		if err := writeNewFile(path, []byte("body"), 0o600); err != nil {
			t.Fatalf("writeNewFile: %v", err)
		}
		jobDirUntouched(t, outside, "untouched")
		jobDirNewFile(t, path, "body", 0o600)
	})
	for _, perm := range []fs.FileMode{0o600, 0o644} {
		t.Run(fmt.Sprintf("a stale regular file at %#o", perm), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "out")
			if err := os.WriteFile(path, []byte("stale and longer"), 0o666); err != nil {
				t.Fatal(err)
			}
			if err := writeNewFile(path, []byte("body"), perm); err != nil {
				t.Fatalf("writeNewFile: %v", err)
			}
			jobDirNewFile(t, path, "body", perm)
		})
	}
	t.Run("a non-empty directory", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "out")
		if err := os.MkdirAll(filepath.Join(path, "inner"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := writeNewFile(path, []byte("body"), 0o600); err == nil {
			t.Fatal("writeNewFile over a non-empty directory succeeded")
		}
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() {
			t.Errorf("the directory is gone: %v, %v", info, err)
		}
	})
}

func jobDirRealBinary(t *testing.T, name string) string {
	t.Helper()
	p, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s is not installed: %v", name, err)
	}
	return p
}

type ocrPDFFakes struct {
	bin, jobDir, in, out string
}

func newOCRPDFFakes(t *testing.T) ocrPDFFakes {
	t.Helper()
	f := ocrPDFFakes{bin: t.TempDir(), jobDir: t.TempDir()}
	f.in = filepath.Join(f.jobDir, "in.pdf")
	f.out = filepath.Join(f.jobDir, "out.txt")
	if err := os.WriteFile(f.in, []byte("%PDF-1.4 fake"), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeEngine(t, f.bin, "pdfinfo", "printf 'Pages:          1\\nPage    1 size:  612 x 792 pts\\n'\n")
	pathWith(t, f.bin)
	return f
}

func (f ocrPDFFakes) set(t *testing.T, pdftotext, pdftoppm, tesseract string) {
	t.Helper()
	fakeEngine(t, f.bin, "pdftotext", pdftotext)
	fakeEngine(t, f.bin, "pdftoppm", pdftoppm)
	fakeEngine(t, f.bin, "tesseract", tesseract)
}

func ocrPDFPagePNG() string {
	return "eval \"B=\\${$#}\"\nprintf 'png' > \"$B.png\"\n"
}

func ocrPDFPageText(pre, text string) string {
	return pre + fmt.Sprintf("printf '%%s' %s > \"$2.txt\"\n", cadDXFQuote(text))
}

func TestOCRPDFWritesTheOutputAsANewFile(t *testing.T) {
	t.Run("the text-layer route", func(t *testing.T) {
		f := newOCRPDFFakes(t)
		outside := jobDirOutside(t, "untouched")
		text := "a usable text layer well over sixteen characters"
		plant := fmt.Sprintf("ln -s %s %s\n", cadDXFQuote(outside), cadDXFQuote(f.out))
		f.set(t, plant+fmt.Sprintf("printf '%%s' %s\n", cadDXFQuote(text)), "exit 7\n", "exit 7\n")
		if err := (OCRConverter{}).Convert(context.Background(), f.in, f.out, nil); err != nil {
			t.Fatalf("Convert: %v", err)
		}
		jobDirUntouched(t, outside, "untouched")
		jobDirNewFile(t, f.out, text, 0)
	})
	t.Run("the page route", func(t *testing.T) {
		f := newOCRPDFFakes(t)
		outside := jobDirOutside(t, "untouched")
		text := "recognised page text"
		plant := fmt.Sprintf("ln -s %s %s\n", cadDXFQuote(outside), cadDXFQuote(f.out))
		f.set(t, "printf '   \\n'\n", ocrPDFPagePNG(), ocrPDFPageText(plant, text))
		if err := (OCRConverter{}).Convert(context.Background(), f.in, f.out, nil); err != nil {
			t.Fatalf("Convert: %v", err)
		}
		jobDirUntouched(t, outside, "untouched")
		jobDirNewFile(t, f.out, text, 0)
	})
}

func sanitizeFileWrapper(realFile, plant string) string {
	return plant + fmt.Sprintf("exec %s \"$@\"\n", cadDXFQuote(realFile))
}

func TestSanitizeWritesAsNewFilesAfterItsStages(t *testing.T) {
	t.Run("the pdf redaction", func(t *testing.T) {
		realGS := jobDirRealBinary(t, "gs")
		bin := t.TempDir()
		outside := filepath.Join(t.TempDir(), "outside.pdf")
		snapshot := filepath.Join(t.TempDir(), "snapshot.pdf")
		fakeEngine(t, bin, "gs", fmt.Sprintf(`%s "$@" || exit $?
case "$*" in
*-sDEVICE=pdfwrite*)
  for a; do case "$a" in -sOutputFile=*) O=${a#-sOutputFile=} ;; esac; done
  mv "$O" %s && cp %s %s && ln -s %s "$O" || exit 9
  ;;
esac
`, cadDXFQuote(realGS), cadDXFQuote(outside), cadDXFQuote(outside), cadDXFQuote(snapshot), cadDXFQuote(outside)))
		pathWith(t, bin)
		out := filepath.Join(t.TempDir(), "out.pdf")
		if _, err := sanitizePDF(context.Background(), engineExec{}, filepath.Join(sanitizeCorpusDir, "threat_all_classes.pdf"), out); err != nil {
			t.Fatalf("sanitizePDF: %v", err)
		}
		want, err := os.ReadFile(snapshot)
		if err != nil {
			t.Fatalf("the gs wrapper never planted: %v", err)
		}
		if got, _ := os.ReadFile(outside); !bytes.Equal(got, want) {
			t.Error("the worker wrote the redacted PDF through a planted link")
		}
		jobDirNewFile(t, out, "", 0)
	})
	t.Run("the verify parts", func(t *testing.T) {
		realFile := jobDirRealBinary(t, "file")
		jobDirRealBinary(t, "unzip")
		bin := t.TempDir()
		outside := jobDirOutside(t, "untouched")
		fakeEngine(t, bin, "file", sanitizeFileWrapper(realFile, fmt.Sprintf(`eval "L=\${$#}"
D=$(dirname "$L")
i=0
while [ $i -lt 100 ]; do
  p=$(printf '%%s/part-%%03d' "$D" $i)
  [ -e "$p" ] || [ -L "$p" ] || ln -s %s "$p"
  i=$((i+1))
done
`, cadDXFQuote(outside))))
		pathWith(t, bin)
		out := filepath.Join(t.TempDir(), "clean.docx")
		_, err := sanitizeFile(context.Background(), engineExec{}, "docm", sanitizeDocmFixture(t), out)
		jobDirUntouched(t, outside, "untouched")
		if err != nil {
			t.Fatalf("sanitizeFile: %v", err)
		}
	})
	t.Run("the report", func(t *testing.T) {
		realFile := jobDirRealBinary(t, "file")
		jobDirRealBinary(t, "unzip")
		jobDir := t.TempDir()
		in := filepath.Join(jobDir, "in.docm")
		raw, err := os.ReadFile(sanitizeDocmFixture(t))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(in, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(jobDir, "out.docx")
		report := SanitizeReportPath(out)
		bin := t.TempDir()
		outside := jobDirOutside(t, "untouched")
		fakeEngine(t, bin, "file", sanitizeFileWrapper(realFile, fmt.Sprintf("[ -L %s ] || ln -s %s %s\n", cadDXFQuote(report), cadDXFQuote(outside), cadDXFQuote(report))))
		pathWith(t, bin)
		if err := (SanitizeConverter{}).Convert(context.Background(), in, out, nil); err != nil {
			t.Fatalf("Convert: %v", err)
		}
		jobDirUntouched(t, outside, "untouched")
		jobDirNewFile(t, report, "", 0)
		body, err := os.ReadFile(report)
		if err != nil {
			t.Fatal(err)
		}
		var rep SanitizeReport
		if err := json.Unmarshal(body, &rep); err != nil {
			t.Fatalf("the report does not parse: %v", err)
		}
		if rep.Format != "docm" || rep.GuaranteeScope == "" || len(rep.Removed) == 0 {
			t.Errorf("report = %+v, want a docm report with removed classes and a scope", rep)
		}
	})
}
