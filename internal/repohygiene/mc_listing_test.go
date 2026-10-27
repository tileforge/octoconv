package repohygiene_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
