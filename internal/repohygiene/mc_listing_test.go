package repohygiene_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const (
	mcGateDefaultScript = "../../scripts/final-live-gate.sh"
	mcJob               = "11111111-2222-3333-4444-555555555555"
	mcErrorTail         = "{\n \"status\": \"error\",\n \"error\": {\n  \"message\": \"Unable to list folder.\",\n  \"cause\": {}\n }\n}\n"
	mcTextTail          = "mc: <ERROR> Unable to list folder.\n"
)

func mcGateScript(t *testing.T) string {
	t.Helper()
	path := os.Getenv("OCTOCONV_FINAL_GATE_SCRIPT")
	if path == "" {
		return mcGateDefaultScript
	}
	if !filepath.IsAbs(path) {
		t.Fatalf("OCTOCONV_FINAL_GATE_SCRIPT must be an absolute path, got %q", path)
	}
	return path
}

func mcFunction(t *testing.T, lines []string, anchor string) string {
	t.Helper()
	start, starts := -1, 0
	for i, line := range lines {
		if line == anchor {
			start = i
			starts++
		}
	}
	if starts != 1 {
		t.Fatalf("anchor %q matches %d lines, want exactly 1", anchor, starts)
	}
	for i := start + 1; i < len(lines); i++ {
		if lines[i] == "}" {
			return strings.Join(lines[start:i+1], "\n")
		}
	}
	t.Fatalf("anchor %q has no closing line }", anchor)
	return ""
}

func mcGateFunctions(t *testing.T, path string) string {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(source), "\n")
	list := mcFunction(t, lines, "cad_bucket_list() {")
	count := mcFunction(t, lines, "cad_prefix_count() {")
	return list + "\n" + count + "\n"
}

func mcObject(key string, size int) string {
	return fmt.Sprintf(`{"status":"success","type":"file","lastModified":"2026-10-17T00:00:00.000Z","size":%d,"key":"%s","etag":"0123456789abcdef0123456789abcdef","url":"http://localhost:9000/octoconv/","versionOrdinal":1,"storageClass":"STANDARD"}`+"\n", size, key)
}

const mcReplayProgram = `exec 3>&2
tmo() { shift; "$@"; }
docker() {
	if [ "$*" != "compose exec -T minio mc ls --recursive --json local/octoconv" ]; then
		printf 'REPLAY_UNEXPECTED %s\n' "$*" >&3
		return 99
	fi
	printf 'REPLAY_CALL\n' >&3
	cat "$REPLAY_OUT"
	cat "$REPLAY_ERR" >&2
	return "$(cat "$REPLAY_RC")"
}
`

const mcReplayTail = `fn=0
cad_bucket_list "$LIST" || fn=$?
printf 'fn_rc=%s\n' "$fn"
printf 'uploads=%s\n' "$(cad_prefix_count "$LIST" "uploads/$JOB/")"
printf 'results=%s\n' "$(cad_prefix_count "$LIST" "results/$JOB/")"
`

func mcPair(t *testing.T, s string) [2]int {
	t.Helper()
	f := strings.Fields(s)
	if len(f) != 2 {
		t.Fatalf("cad_prefix_count printed %q, want two numbers", s)
	}
	var p [2]int
	for i := range f {
		n, err := strconv.Atoi(f[i])
		if err != nil {
			t.Fatalf("cad_prefix_count printed %q: %v", s, err)
		}
		p[i] = n
	}
	return p
}

func mcJudge(t *testing.T, fns string, stdout, stderr []byte, rc int, job string) (int, []string, [2]int, [2]int, string) {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Fatalf("jq is required on PATH: %v", err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "replay.out")
	errf := filepath.Join(dir, "replay.err")
	rcf := filepath.Join(dir, "replay.rc")
	list := filepath.Join(dir, "list")
	for _, f := range []struct {
		path string
		body []byte
	}{{out, stdout}, {errf, stderr}, {rcf, []byte(strconv.Itoa(rc) + "\n")}} {
		if err := os.WriteFile(f.path, f.body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("bash", "-c", mcReplayProgram+fns+mcReplayTail)
	cmd.Env = append(os.Environ(), "REPLAY_OUT="+out, "REPLAY_ERR="+errf, "REPLAY_RC="+rcf, "LIST="+list, "JOB="+job)
	var so, se bytes.Buffer
	cmd.Stdout = &so
	cmd.Stderr = &se
	if err := cmd.Run(); err != nil {
		t.Fatalf("replay program failed: %v; stdout: %s; stderr: %s", err, so.String(), se.String())
	}
	vals := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(so.String()), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("unexpected replay output line %q", line)
		}
		vals[k] = v
	}
	fnRC, err := strconv.Atoi(vals["fn_rc"])
	if err != nil {
		t.Fatalf("fn_rc %q: %v; stdout: %s", vals["fn_rc"], err, so.String())
	}
	var rows []string
	body, err := os.ReadFile(list)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(body), "\n") {
		if line != "" {
			rows = append(rows, line)
		}
	}
	return fnRC, rows, mcPair(t, vals["uploads"]), mcPair(t, vals["results"]), se.String()
}

func TestCadBucketListAgainstTheStubShape(t *testing.T) {
	upload := "uploads/" + mcJob + "/0-a.step"
	result := "results/" + mcJob + "/0-a.png"
	objects := mcObject(upload, 10) + mcObject(result, 20)
	prefixed := mcObject("octoconv/"+upload, 10) + mcObject("octoconv/"+result, 20)
	cases := []struct {
		name     string
		stdout   string
		rc       int
		wantZero bool
		check    func(t *testing.T, rows []string, uploads, results [2]int)
	}{
		{
			name:     "objects",
			stdout:   objects,
			wantZero: true,
			check: func(t *testing.T, rows []string, uploads, results [2]int) {
				if len(rows) != 2 {
					t.Fatalf("list has %d rows, want 2: %q", len(rows), rows)
				}
				for _, r := range rows {
					if r == "BAD" {
						t.Fatalf("list has a BAD row: %q", rows)
					}
				}
				if uploads != [2]int{1, 10} || results != [2]int{1, 20} {
					t.Fatalf("uploads=%v results=%v, want [1 10] and [1 20]", uploads, results)
				}
			},
		},
		{name: "error_tail", stdout: objects + mcErrorTail},
		{name: "text_tail", stdout: objects + mcTextTail},
		{name: "nonzero_rc", stdout: objects, rc: 1},
		{
			name:     "bucket_prefixed_keys",
			stdout:   prefixed,
			wantZero: true,
			check: func(t *testing.T, rows []string, uploads, results [2]int) {
				if uploads != [2]int{0, 0} || results != [2]int{0, 0} {
					t.Fatalf("uploads=%v results=%v, want [0 0] for both", uploads, results)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fns := mcGateFunctions(t, mcGateScript(t))
			fnRC, rows, uploads, results, replayStderr := mcJudge(t, fns, []byte(tc.stdout), nil, tc.rc, mcJob)
			if strings.Contains(replayStderr, "REPLAY_UNEXPECTED") {
				t.Fatalf("the gate called docker with another command: %s", replayStderr)
			}
			if n := strings.Count(replayStderr, "REPLAY_CALL"); n != 1 {
				t.Fatalf("the gate listed the bucket %d times, want 1", n)
			}
			if tc.wantZero && fnRC != 0 {
				t.Fatalf("cad_bucket_list returned %d, want 0; rows %q", fnRC, rows)
			}
			if !tc.wantZero && fnRC == 0 {
				t.Fatalf("cad_bucket_list returned 0, want non-zero; rows %q", rows)
			}
			if tc.check != nil {
				tc.check(t, rows, uploads, results)
			}
		})
	}
}

const mcLiveDir = "testdata/mc-ls-live"

func mcGateUUID(t *testing.T, path string) *regexp.Regexp {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, line := range strings.Split(string(source), "\n") {
		if v, ok := strings.CutPrefix(line, "UUID_RE='"); ok && strings.HasSuffix(v, "'") {
			found = append(found, strings.TrimSuffix(v, "'"))
		}
	}
	if len(found) != 1 {
		t.Fatalf("UUID_RE is assigned on %d lines of %s, want 1", len(found), path)
	}
	return regexp.MustCompile(found[0])
}

func mcLiveFields(t *testing.T, n int, line string) map[string]string {
	t.Helper()
	fields := map[string]string{}
	for _, part := range strings.Split(line, ",") {
		key, value, ok := strings.Cut(part, "=")
		if !ok || key == "" || value == "" {
			t.Fatalf("SOURCES.txt line %d: malformed field %q", n, part)
		}
		if _, dup := fields[key]; dup {
			t.Fatalf("SOURCES.txt line %d: duplicate field %q", n, key)
		}
		fields[key] = value
	}
	return fields
}

func mcLiveCopies(t *testing.T) (map[string][]byte, string) {
	t.Helper()
	sources, err := os.ReadFile(filepath.Join(mcLiveDir, "SOURCES.txt"))
	if err != nil {
		t.Fatal(err)
	}
	copies := map[string][]byte{}
	job := ""
	for i, line := range strings.Split(string(sources), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := mcLiveFields(t, i+1, line)
		if fields["probe"] != "" {
			if job != "" {
				t.Fatalf("SOURCES.txt line %d: second probe line", i+1)
			}
			job = fields["job"]
			continue
		}
		for _, key := range []string{"file", "bytes", "sha256", "origin", "lines", "origin_commit"} {
			if fields[key] == "" {
				t.Fatalf("SOURCES.txt line %d: missing %s", i+1, key)
			}
		}
		name := fields["file"]
		if name != filepath.Base(name) || name == "SOURCES.txt" {
			t.Fatalf("SOURCES.txt line %d: file %q is not a copy in %s", i+1, name, mcLiveDir)
		}
		if _, dup := copies[name]; dup {
			t.Fatalf("SOURCES.txt line %d: %s listed twice", i+1, name)
		}
		data, err := os.ReadFile(filepath.Join(mcLiveDir, name))
		if err != nil {
			t.Fatalf("SOURCES.txt line %d: %v", i+1, err)
		}
		want, err := strconv.Atoi(fields["bytes"])
		if err != nil {
			t.Fatalf("SOURCES.txt line %d: bytes %q: %v", i+1, fields["bytes"], err)
		}
		if len(data) != want {
			t.Fatalf("%s has %d bytes, SOURCES.txt says %d", name, len(data), want)
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != fields["sha256"] {
			t.Fatalf("%s has sha256 %s, SOURCES.txt says %s", name, got, fields["sha256"])
		}
		copies[name] = data
	}
	entries, err := os.ReadDir(mcLiveDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == "SOURCES.txt" {
			continue
		}
		if _, ok := copies[e.Name()]; !ok {
			t.Fatalf("%s/%s is not listed in SOURCES.txt", mcLiveDir, e.Name())
		}
	}
	for _, name := range []string{"ls.jsonl", "ls.err", "ls.rc", "absent.out", "absent.err", "absent.rc"} {
		if _, ok := copies[name]; !ok {
			t.Fatalf("SOURCES.txt does not list %s", name)
		}
	}
	return copies, job
}

func mcLiveRC(t *testing.T, name string, data []byte) int {
	t.Helper()
	rc, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("%s holds %q: %v", name, data, err)
	}
	return rc
}

func TestCadBucketListReadsTheLiveMcShape(t *testing.T) {
	gate := mcGateScript(t)
	copies, job := mcLiveCopies(t)
	if !mcGateUUID(t, gate).MatchString(job) {
		t.Fatalf("probe job %q does not match the gate's UUID_RE", job)
	}
	t.Run("listing", func(t *testing.T) {
		fns := mcGateFunctions(t, gate)
		stdout := copies["ls.jsonl"]
		fnRC, rows, uploads, results, replayStderr := mcJudge(t, fns, stdout, copies["ls.err"], mcLiveRC(t, "ls.rc", copies["ls.rc"]), job)
		if strings.Contains(replayStderr, "REPLAY_UNEXPECTED") {
			t.Fatalf("the gate called docker with another command: %s", replayStderr)
		}
		if n := strings.Count(replayStderr, "REPLAY_CALL"); n != 1 {
			t.Fatalf("the gate listed the bucket %d times, want 1", n)
		}
		if fnRC != 0 {
			t.Fatalf("cad_bucket_list returned %d on the live listing, want 0; rows %q", fnRC, rows)
		}
		values := 0
		for _, line := range strings.Split(string(stdout), "\n") {
			if line != "" {
				values++
			}
		}
		if len(rows) != values {
			t.Fatalf("list has %d rows for %d live lines", len(rows), values)
		}
		for _, r := range rows {
			if r == "BAD" {
				t.Fatalf("list has a BAD row: %q", rows)
			}
			_, key, ok := strings.Cut(r, "\t")
			if !ok {
				t.Fatalf("list row %q has no tab", r)
			}
			if strings.HasPrefix(key, "octoconv/") {
				t.Fatalf("key %q carries the bucket name", key)
			}
		}
		if uploads != [2]int{1, 15567} || results != [2]int{2, 19787} {
			t.Fatalf("uploads=%v results=%v for job %s, want [1 15567] and [2 19787] from ls.jsonl", uploads, results, job)
		}
	})
	t.Run("absent_bucket", func(t *testing.T) {
		fns := mcGateFunctions(t, gate)
		fnRC, rows, _, _, replayStderr := mcJudge(t, fns, copies["absent.out"], copies["absent.err"], mcLiveRC(t, "absent.rc", copies["absent.rc"]), job)
		if strings.Contains(replayStderr, "REPLAY_UNEXPECTED") {
			t.Fatalf("the gate called docker with another command: %s", replayStderr)
		}
		if fnRC == 0 {
			t.Fatalf("cad_bucket_list returned 0 on the absent bucket, want non-zero; rows %q", rows)
		}
	})
}
