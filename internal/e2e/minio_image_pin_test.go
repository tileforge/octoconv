package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The MinIO image reference, held to docker-compose.yml and the Helm chart.
//
// Docker Hub stopped serving the official MinIO images, so `docker compose up`,
// `scripts/ci-local.sh e2e` and a fresh chart install died on pull on any
// machine without a hand-made local alias. One chainguard image carries both
// minio and mc, so compose and the chart use that single image for the server
// and the createbucket job; the chart's mc containers (createbucket and
// wait-for-bucket) take minio.mcImage when an operator sets it and this image
// otherwise.
//
// The reference is pinned by digest because Chainguard's free tier publishes
// only :latest, which floats. Compose and the chart must carry the SAME string:
// if they drift, dev and Kubernetes silently run different MinIO builds.
//
// The set of references is DERIVED from the files, never listed here, and a scan
// that finds nothing fails -- a check over zero references is not a pass.
//
// It lives in internal/e2e with the other compose-wiring checks and does not
// call e2eSetup, so it runs in the offline tier too.

const (
	minioChartDir       = "../../deploy/chart/octoconv"
	minioTemplateLine   = "image: {{ .Values.minio.image | quote }}"
	minioMCTemplateLine = "image: {{ .Values.minio.mcImage | default .Values.minio.image | quote }}"
)

var (
	minioImageLineRe = regexp.MustCompile(`^image:\s*(.+)$`)
	minioPinnedRe    = regexp.MustCompile(`^cgr\.dev/chainguard/minio@sha256:[0-9a-f]{64}$`)
	mcImageKeyRe     = regexp.MustCompile(`^mcImage:\s*(.*)$`)
	mcDigestRe       = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// minioRef is one MinIO image reference found in a file.
type minioRef struct {
	file  string
	line  int
	value string
}

func (r minioRef) String() string { return fmt.Sprintf("%s:%d", r.file, r.line) }

// minioImageRefProblem returns why ref is not an acceptable MinIO image
// reference, or "" if it is the digest-pinned chainguard form.
func minioImageRefProblem(ref string) string {
	switch {
	case minioPinnedRe.MatchString(ref):
		return ""
	case !strings.HasPrefix(ref, "cgr.dev/chainguard/minio"):
		return "not the cgr.dev/chainguard/minio repository"
	case !strings.Contains(ref, "@sha256:"):
		return "not pinned by digest (a tag floats)"
	case strings.Contains(strings.SplitN(ref, "@", 2)[0], ":"):
		return "carries a tag in front of the digest (`:tag@sha256:` reads as the tag for pull policy; use repository@sha256:...)"
	default:
		return "digest is not 64 lowercase hex characters"
	}
}

func mcImageRefProblem(ref string) string {
	if ref == "" {
		return ""
	}
	name, digest, found := strings.Cut(ref, "@")
	switch {
	case !found:
		return "not pinned by digest (a tag floats)"
	case name == "":
		return "no image name in front of the digest"
	case strings.Contains(name[strings.LastIndex(name, "/")+1:], ":"):
		return "carries a tag in front of the digest (use repository@sha256:...)"
	case !mcDigestRe.MatchString(digest):
		return "digest is not sha256: followed by 64 lowercase hex characters"
	}
	return ""
}

// minioImageRefs returns every non-comment `image:` value containing "minio"
// in path, quotes and trailing comments stripped.
func minioImageRefs(t *testing.T, path string) []minioRef {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var refs []minioRef
	for i, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "#") {
			continue
		}
		m := minioImageLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		v := strings.TrimSpace(m[1])
		if j := strings.Index(v, " #"); j >= 0 {
			v = strings.TrimSpace(v[:j])
		}
		v = strings.Trim(v, `"'`)
		if !strings.Contains(v, "minio") {
			continue
		}
		refs = append(refs, minioRef{file: filepath.Base(path), line: i + 1, value: v})
	}
	return refs
}

func TestMinioImagePinnedByDigest(t *testing.T) {
	composeFiles, err := filepath.Glob("../../docker-compose*.yml")
	if err != nil {
		t.Fatal(err)
	}
	valuesFiles, err := filepath.Glob(minioChartDir + "/values*.yaml")
	if err != nil {
		t.Fatal(err)
	}

	var composeRefs, valuesRefs []minioRef
	for _, f := range composeFiles {
		composeRefs = append(composeRefs, minioImageRefs(t, f)...)
	}
	for _, f := range valuesFiles {
		valuesRefs = append(valuesRefs, minioImageRefs(t, f)...)
	}
	// compose has minio and createbucket; the chart has the single minio.image.
	if len(composeRefs) < 2 {
		t.Fatalf("found %d MinIO image references in docker-compose*.yml, want at least 2 (minio, createbucket): the scan matched nothing, which is a failure, not a pass", len(composeRefs))
	}
	if len(valuesRefs) < 1 {
		t.Fatalf("found %d MinIO image references in %s/values*.yaml, want at least 1 (minio.image): the scan matched nothing, which is a failure, not a pass", len(valuesRefs), minioChartDir)
	}

	all := append(append([]minioRef{}, composeRefs...), valuesRefs...)
	for _, r := range all {
		if p := minioImageRefProblem(r.value); p != "" {
			t.Errorf("%s: %q: %s", r, r.value, p)
		}
	}
	for _, r := range all[1:] {
		if r.value != all[0].value {
			t.Errorf("%s: %q differs from %s: %q -- compose and chart must carry the same image string", r, r.value, all[0], all[0].value)
		}
	}

	// Templates: every image: that reads .Values.minio.* is exactly the server
	// form or the mc form. The set of files is derived by walking the directory.
	serverLines, mcLines := 0, 0
	tplFiles, err := filepath.Glob(minioChartDir + "/templates/*")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range tplFiles {
		if !strings.HasSuffix(f, ".yaml") && !strings.HasSuffix(f, ".tpl") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, raw := range strings.Split(string(b), "\n") {
			line := strings.TrimSpace(raw)
			if !strings.HasPrefix(line, "image:") || !strings.Contains(line, ".Values.minio.") {
				continue
			}
			switch line {
			case minioTemplateLine:
				serverLines++
			case minioMCTemplateLine:
				mcLines++
			default:
				t.Errorf("%s:%d: %q, want exactly %q or %q", filepath.Base(f), i+1, line, minioTemplateLine, minioMCTemplateLine)
			}
		}
	}
	// minio and migrate-data-ownership in minio.yaml.
	if serverLines < 2 {
		t.Errorf("found %d server image lines in chart templates, want at least 2", serverLines)
	}
	// wait-for-minio and createbucket in job-createbucket.yaml, wait-for-bucket in _helpers.tpl.
	if mcLines < 3 {
		t.Errorf("found %d mc image lines in chart templates, want at least 3", mcLines)
	}

	mcKeys := 0
	for _, f := range valuesFiles {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, raw := range strings.Split(string(b), "\n") {
			line := strings.TrimSpace(raw)
			if strings.HasPrefix(line, "#") {
				continue
			}
			m := mcImageKeyRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			mcKeys++
			v := strings.TrimSpace(m[1])
			if j := strings.Index(v, " #"); j >= 0 {
				v = strings.TrimSpace(v[:j])
			}
			v = strings.Trim(v, `"'`)
			if p := mcImageRefProblem(v); p != "" {
				t.Errorf("%s:%d: mcImage %q: %s", filepath.Base(f), i+1, v, p)
			}
		}
	}
	if mcKeys < 1 {
		t.Errorf("found %d mcImage keys in %s/values*.yaml, want at least 1: the scan matched nothing, which is a failure, not a pass", mcKeys, minioChartDir)
	}
}

func TestMinioImageRefRejectsUnpinnedForms(t *testing.T) {
	const digest = "sha256:fa23f6a6f62645654530ff94aa077d1cc0d0e44c8f1cce02ab039873612edc72"
	cases := []struct {
		name string
		ref  string
		ok   bool
	}{
		{"pinned index digest", "cgr.dev/chainguard/minio@" + digest, true},
		{"old official minio release tag", "minio/minio:RELEASE.2025-09-07T16-13-09Z", false},
		{"old mc latest", "minio/mc:latest", false},
		{"chainguard tag only", "cgr.dev/chainguard/minio:latest", false},
		{"chainguard latest@digest (D-02)", "cgr.dev/chainguard/minio:latest@" + digest, false},
		{"chainguard short digest", "cgr.dev/chainguard/minio@sha256:fa23f6a6", false},
		{"same digest, other repository", "cgr.dev/chainguard/mc@" + digest, false},
		{"same digest, docker hub repository", "minio/minio@" + digest, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := minioImageRefProblem(c.ref)
			if c.ok && got != "" {
				t.Errorf("%q rejected: %s", c.ref, got)
			}
			if !c.ok && got == "" {
				t.Errorf("%q accepted, want rejected", c.ref)
			}
		})
	}
}

func TestMcImageRefRejectsUnpinnedForms(t *testing.T) {
	const hex = "fa23f6a6f62645654530ff94aa077d1cc0d0e44c8f1cce02ab039873612edc72"
	cases := []struct {
		name string
		ref  string
		ok   bool
	}{
		{"empty falls back to minio.image", "", true},
		{"pinned minio image", "cgr.dev/chainguard/minio@sha256:" + hex, true},
		{"registry with a port", "registry.example:5000/mirror/mc@sha256:" + hex, true},
		{"mc latest", "minio/mc:latest", false},
		{"tag in front of the digest", "registry.example:5000/mirror/mc:RELEASE.2025-08-13T08-35-41Z@sha256:" + hex, false},
		{"short digest", "registry.example/mirror/mc@sha256:fa23f6a6", false},
		{"uppercase digest", "registry.example/mirror/mc@sha256:" + strings.ToUpper(hex), false},
		{"no name in front of the digest", "@sha256:" + hex, false},
		{"registry with a port, no digest", "registry.example:5000/mirror/mc", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := mcImageRefProblem(c.ref)
			if c.ok && got != "" {
				t.Errorf("%q rejected: %s", c.ref, got)
			}
			if !c.ok && got == "" {
				t.Errorf("%q accepted, want rejected", c.ref)
			}
		})
	}
}
