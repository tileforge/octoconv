package workerproto

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tileforge/octoconv/internal/convert"
)

func TestProtocolOutputLimit(t *testing.T) {
	cases := []struct {
		engine string
		target string
		want   int64
	}{
		{engine: convert.EngineImage, target: "jpg", want: protocolDefaultOutputBytes},
		{engine: convert.EngineImage, target: "svg", want: protocolDefaultOutputBytes},
		{engine: convert.EngineCAD, target: "svg", want: convert.CADMaxDXFSVGBytes},
		{engine: convert.EngineCAD, target: "SVG", want: convert.CADMaxDXFSVGBytes},
		{engine: convert.EngineCAD, target: "stl", want: convert.CADMaxSTLBytes},
		{engine: convert.EngineCAD, target: "pdf", want: protocolDefaultOutputBytes},
		{engine: convert.EngineCAD, target: "png", want: protocolDefaultOutputBytes},
		{engine: convert.EngineAV, target: "wav", want: 2 << 30},
		{engine: convert.EngineAV, target: "jpg", want: 2 << 30},
		{engine: convert.EngineAV, target: "mp4", want: 2 << 30},
		{engine: convert.EngineAV, target: "stl", want: protocolDefaultOutputBytes},
		{engine: "", target: "stl", want: protocolDefaultOutputBytes},
	}
	for _, tc := range cases {
		if got := protocolOutputLimit(tc.engine, tc.target); got != tc.want {
			t.Errorf("protocolOutputLimit(%q, %q) = %d, want %d", tc.engine, tc.target, got, tc.want)
		}
	}
	if convert.CADMaxDXFSVGBytes <= protocolDefaultOutputBytes {
		t.Fatalf("SVG ceiling %d is not above the protocol default", int64(convert.CADMaxDXFSVGBytes))
	}
	if convert.CADMaxSTLBytes <= protocolDefaultOutputBytes {
		t.Fatalf("STL ceiling %d is not above the protocol default", int64(convert.CADMaxSTLBytes))
	}
	if convert.CADMaxDXFPDFBytes > protocolDefaultOutputBytes {
		t.Fatalf("PDF ceiling %d exceeds the protocol default", int64(convert.CADMaxDXFPDFBytes))
	}
}

func TestArtifactListedHonorsCADCeilings(t *testing.T) {
	sha := strings.Repeat("ab", 32)
	listed := func(format, contentType string, size int64) bool {
		return artifactListed(completeArtifact{
			Role:        "primary",
			Ordinal:     "0",
			Format:      format,
			ContentType: contentType,
			SHA256:      sha,
			ByteSize:    json.Number(strconv.FormatInt(size, 10)),
		})
	}
	cases := []struct {
		name        string
		format      string
		contentType string
		size        int64
		want        bool
	}{
		{name: "jpg at the image ceiling", format: "jpg", contentType: "image/jpeg", size: protocolDefaultOutputBytes, want: true},
		{name: "jpg between the image ceiling and the av ceiling", format: "jpg", contentType: "image/jpeg", size: protocolDefaultOutputBytes + 1, want: true},
		{name: "jpg above the av ceiling", format: "jpg", contentType: "image/jpeg", size: (2 << 30) + 1, want: false},
		{name: "wav above the image ceiling", format: "wav", contentType: "audio/wav", size: protocolDefaultOutputBytes + 1, want: true},
		{name: "wav above the av ceiling", format: "wav", contentType: "audio/wav", size: (2 << 30) + 1, want: false},
		{name: "svg at the dxf ceiling", format: "svg", contentType: "image/svg+xml", size: convert.CADMaxDXFSVGBytes, want: true},
		{name: "svg above the dxf ceiling", format: "svg", contentType: "image/svg+xml", size: convert.CADMaxDXFSVGBytes + 1, want: false},
		{name: "stl at the stl ceiling", format: "stl", contentType: "model/stl", size: convert.CADMaxSTLBytes, want: true},
		{name: "stl above the stl ceiling", format: "stl", contentType: "model/stl", size: convert.CADMaxSTLBytes + 1, want: false},
		{name: "pdf at 100 MiB", format: "pdf", contentType: "application/pdf", size: protocolDefaultOutputBytes, want: true},
		{name: "pdf above 100 MiB", format: "pdf", contentType: "application/pdf", size: protocolDefaultOutputBytes + 1, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := listed(tc.format, tc.contentType, tc.size); got != tc.want {
				t.Fatalf("artifactListed = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestCADJobStagePublishesOnlyPublicWords(t *testing.T) {
	guard, mesh := "guard", "mesh"
	one, two, three := 1, 2, 3
	zero := 0
	other := "preprocess"
	cases := []struct {
		name  string
		stage *string
		index *int
		count *int
		ok    bool
	}{
		{name: "guard", stage: &guard, index: &one, count: &three, ok: true},
		{name: "mesh", stage: &mesh, index: &two, count: &three, ok: true},
		{name: "unknown word", stage: &other, index: &one, count: &three, ok: false},
		{name: "index zero", stage: &guard, index: &zero, count: &three, ok: false},
		{name: "index past count", stage: &guard, index: &three, count: &two, ok: false},
		{name: "missing index", stage: &guard, count: &three, ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, ok := cadJobStage(tc.stage, tc.index, tc.count)
			if ok != tc.ok {
				t.Fatalf("cadJobStage ok = %t, want %t", ok, tc.ok)
			}
		})
	}
	for _, word := range convert.CADStageVocabulary() {
		stage := word
		if _, got, n, ok := cadJobStage(&stage, &one, &three); !ok || got != 1 || n != 3 {
			t.Fatalf("cadJobStage(%q) = %d/%d ok %t", word, got, n, ok)
		}
	}
}

func TestProtocolAttemptResetClearsStage(t *testing.T) {
	paths := []string{"../jobs/repo.go", "reconcile.go", "session.go", "schedule.go"}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		from := 0
		for {
			at := strings.Index(text[from:], "eta_seconds = NULL")
			if at < 0 {
				break
			}
			at += from
			end := at + len("eta_seconds = NULL") + 160
			if end > len(text) {
				end = len(text)
			}
			window := text[at:end]
			for _, part := range []string{"stage = NULL", "stage_index = NULL", "stage_count = NULL"} {
				if !strings.Contains(window, part) {
					t.Fatalf("%s near %d clears eta without %s", path, at, part)
				}
			}
			from = at + len("eta_seconds = NULL")
		}
	}
}

func TestActivateProtocolCountsTheAttempt(t *testing.T) {
	body, err := os.ReadFile("../jobs/repo.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	start := strings.Index(text, "func (r *Repo) ActivateProtocol")
	if start < 0 {
		t.Fatal("ActivateProtocol missing")
	}
	fn := text[start:]
	if end := strings.Index(fn, "\nfunc "); end > 0 {
		fn = fn[:end]
	}
	inc := strings.Index(fn, "attempts = attempts + 1")
	eta := strings.Index(fn, "eta_seconds = NULL")
	if inc < 0 || eta < 0 || inc > eta {
		t.Fatal("ActivateProtocol does not increment jobs.attempts before clearing eta")
	}
}

func TestFileLinkMaxTTLCoversDefaultCADBudget(t *testing.T) {
	want := int(convert.CADDefaultEngineTimeout / time.Second)
	if FileLinkMaxTTLSeconds != want {
		t.Fatalf("FileLinkMaxTTLSeconds = %d, want %d", FileLinkMaxTTLSeconds, want)
	}
	if FileLinkMaxTTLSeconds >= 15*60 {
		t.Fatalf("FileLinkMaxTTLSeconds = %d, must stay under 15 minutes", FileLinkMaxTTLSeconds)
	}
	if FileLinkMaxTTLSeconds <= int(convert.CADDefaultMeshTimeout/time.Second) {
		t.Fatalf("FileLinkMaxTTLSeconds = %d does not cover mesh timeout %s", FileLinkMaxTTLSeconds, convert.CADDefaultMeshTimeout)
	}
	n := strconv.Itoa(FileLinkMaxTTLSeconds)
	doc, err := os.ReadFile("../../docs/WORKER-PROTOCOL.md")
	if err != nil {
		t.Fatal(err)
	}
	body := string(doc)
	for _, needle := range []string{
		"| `file_link_max_ttl_seconds` | " + n + " |",
		"advertised as `file_link_max_ttl_seconds` " + n,
		"| `file_link_max_ttl` | " + n + " seconds, advertised as `file_link_max_ttl_seconds` |",
		"Do not use 15 minutes.",
		"Issued `expires_at` is the minimum of `issued_at` plus this cap, `attempt_deadline`, and `job_deadline`.",
		"a cad job that opts into progress webhooks",
		"Protocol progress copies a CAD stage onto the job",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("WORKER-PROTOCOL.md missing %q", needle)
		}
	}
	fixture, err := os.ReadFile("../../docs/worker-protocol/examples/positive/hello-response.json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(fixture), `"file_link_max_ttl_seconds": `+n) {
		t.Fatalf("hello fixture does not advertise %s", n)
	}
	interval := fileLinkIntervalSQL()
	if !strings.Contains(interval, n) || strings.Contains(interval, "300") {
		t.Fatalf("link interval SQL = %q", interval)
	}
}
