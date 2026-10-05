package convert

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

type goldenRow struct {
	name, group, sha, sniffFormat, sniffRejection, reader, readerAccepts, readerDetail, policy string
	bytes                                                                                      int64
}

func (r goldenRow) sniffAccepts() bool {
	return r.sniffFormat != "-" && r.sniffRejection == "-"
}

func loadGolden(t *testing.T) []goldenRow {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "cad-sniff-golden.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) < 2 {
		t.Fatal("golden has no data rows")
	}
	var rows []goldenRow
	seen := map[string]bool{}
	for i, line := range lines[1:] {
		f := strings.Split(line, "\t")
		if len(f) != 10 {
			t.Fatalf("golden row %d has %d columns, want 10", i+2, len(f))
		}
		n, err := strconv.ParseInt(f[3], 10, 64)
		if err != nil {
			t.Fatalf("golden row %d: bytes %q: %v", i+2, f[3], err)
		}
		for j, v := range f {
			if v == "" {
				t.Fatalf("golden row %d: column %d is empty", i+2, j+1)
			}
		}
		if seen[f[0]] {
			t.Fatalf("golden lists %s twice", f[0])
		}
		seen[f[0]] = true
		rows = append(rows, goldenRow{
			name: f[0], group: f[1], sha: f[2], bytes: n, sniffFormat: f[4], sniffRejection: f[5],
			reader: f[6], readerAccepts: f[7], readerDetail: f[8], policy: f[9],
		})
	}
	return rows
}

func sha256File(t *testing.T, path string) (string, int64) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), int64(len(b))
}

func recomputeVerdict(t *testing.T, path string) (string, string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	head, tail, err := ReadCADWindows(f, st.Size())
	if err != nil {
		t.Fatal(err)
	}
	declared := NormalizeFormat(strings.TrimPrefix(filepath.Ext(path), "."))
	res := SniffCADWindows(head, tail, st.Size(), declared)
	format, rejection := res.Format, string(res.Rejection)
	if format == "" {
		format = "-"
	}
	if rejection == "" {
		rejection = "-"
	}
	return format, rejection
}

func checkGoldenRow(t *testing.T, r goldenRow, path string) {
	t.Helper()
	sha, size := sha256File(t, path)
	if sha != r.sha {
		t.Fatalf("%s: sha256 %s differs from golden %s", r.name, sha, r.sha)
	}
	if size != r.bytes {
		t.Fatalf("%s: %d bytes, golden says %d", r.name, size, r.bytes)
	}
	format, rejection := recomputeVerdict(t, path)
	if format != r.sniffFormat || rejection != r.sniffRejection {
		t.Fatalf("%s: sniffer says format=%s rejection=%s, golden says format=%s rejection=%s",
			r.name, format, rejection, r.sniffFormat, r.sniffRejection)
	}
}

func checkGoldenPolicy(t *testing.T, r goldenRow) {
	t.Helper()
	if r.policy == "violation" {
		t.Fatalf("%s: policy is violation", r.name)
	}
	accepts := r.sniffAccepts()
	real := r.group == "public" || r.group == "fetched"
	switch {
	case !accepts && r.readerAccepts != "0":
		if real {
			t.Fatalf("%s: D-09 violation: reader %s accepts (%s) while the sniffer does not (format=%s rejection=%s)",
				r.name, r.reader, r.readerDetail, r.sniffFormat, r.sniffRejection)
		}
		if r.group != "reject" || !strings.HasPrefix(r.policy, "policy:") {
			t.Fatalf("%s: reader accepts and the sniffer does not, policy is %q, want policy:<category> on a reject row", r.name, r.policy)
		}
	case accepts && r.readerAccepts == "1", !accepts && r.readerAccepts == "0":
		if r.policy != "agree" {
			t.Fatalf("%s: verdicts agree but policy is %q", r.name, r.policy)
		}
	case accepts && r.readerAccepts == "0":
		if r.policy != "later-stage" {
			t.Fatalf("%s: sniffer accepts and reader rejects but policy is %q", r.name, r.policy)
		}
	default:
		t.Fatalf("%s: unclassifiable row accepts=%v reader_accepts=%s", r.name, accepts, r.readerAccepts)
	}
}

func TestSniffCADGolden(t *testing.T) {
	rows := loadGolden(t)
	byName := map[string]goldenRow{}
	for _, r := range rows {
		byName[r.name] = r
	}

	committed := 0
	for _, group := range []string{"public", "synthetic", "reject"} {
		entries, err := os.ReadDir(filepath.Join("testdata", "cad", group))
		if err != nil {
			t.Fatal(err)
		}
		var files []string
		for _, e := range entries {
			if e.IsDir() || strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			files = append(files, group+"/"+e.Name())
		}
		sort.Strings(files)
		have := map[string]bool{}
		for _, name := range files {
			have[name] = true
			if _, ok := byName[name]; !ok {
				t.Errorf("%s is committed but missing from the golden", name)
			}
		}
		for _, r := range rows {
			if r.group == group && !have[r.name] {
				t.Errorf("golden lists %s which is not committed", r.name)
			}
		}
		for _, name := range files {
			r, ok := byName[name]
			if !ok {
				continue
			}
			committed++
			t.Run(name, func(t *testing.T) {
				checkGoldenRow(t, r, filepath.Join("testdata", "cad", filepath.FromSlash(name)))
				checkGoldenPolicy(t, r)
			})
		}
	}
	if committed == 0 {
		t.Fatal("no committed corpus file was checked")
	}

	cache := os.Getenv("OCTOCONV_CAD_CACHE")
	if cache == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		cache = filepath.Join(home, ".cache", "octoconv", "cad-corpus")
	}
	for _, r := range rows {
		switch r.group {
		case "public", "synthetic", "reject":
			continue
		case "fetched":
		default:
			t.Errorf("%s: unknown group %q", r.name, r.group)
			continue
		}
		r := r
		t.Run(r.name, func(t *testing.T) {
			checkGoldenPolicy(t, r)
			path := filepath.Join(cache, "fetched", strings.TrimPrefix(r.name, "fetched/"))
			if _, err := os.Stat(path); err != nil {
				t.Skipf("fetched file absent under %s", cache)
			}
			if sha, _ := sha256File(t, path); sha != r.sha {
				t.Skipf("fetched file hash differs from the golden")
			}
			checkGoldenRow(t, r, path)
		})
	}
}
