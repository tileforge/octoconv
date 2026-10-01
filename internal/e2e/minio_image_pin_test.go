package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// The MinIO image reference, held to docker-compose.yml and the Helm chart.
//
// Docker Hub stopped serving the official MinIO images, so `docker compose up`,
// `scripts/ci-local.sh e2e` and a fresh chart install died on pull on any
// machine without a hand-made local alias. One chainguard image carries both
// minio and mc, so compose and the chart use that single image for the server
// and the createbucket job; the chart's mc containers (createbucket and
// wait-for-bucket) take minio.mcImage, whose default is this same pinned image,
// and fall back to minio.image only when that key is missing or empty.
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

	minioOwnershipTemplateLine = "image: {{ $ownership.image | default .Values.minio.image | quote }}"
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

func overrideImageRefProblem(ref string) string {
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

func valuesScalarAt(data []byte, path ...string) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	n := &doc
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return nil, nil
		}
		n = n.Content[0]
	}
	for _, key := range path {
		if n.Kind == yaml.AliasNode {
			n = n.Alias
		}
		if n.Kind != yaml.MappingNode {
			return nil, nil
		}
		var next *yaml.Node
		for j := 0; j+1 < len(n.Content); j += 2 {
			if n.Content[j].Value == key {
				next = n.Content[j+1]
				break
			}
		}
		if next == nil {
			return nil, nil
		}
		n = next
	}
	if n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	if n.Kind != yaml.ScalarNode {
		return nil, fmt.Errorf("%s is not a scalar", strings.Join(path, "."))
	}
	return n, nil
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
	ownershipKeys := 0
	for _, f := range valuesFiles {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		node, err := valuesScalarAt(b, "minio", "dataOwnership", "image")
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		skip := 0
		if node != nil {
			ownershipKeys++
			skip = node.Line
			if p := overrideImageRefProblem(node.Value); p != "" {
				t.Errorf("%s:%d: minio.dataOwnership.image %q: %s", filepath.Base(f), node.Line, node.Value, p)
			}
		}
		for _, r := range minioImageRefs(t, f) {
			if r.line != skip {
				valuesRefs = append(valuesRefs, r)
			}
		}
	}
	if ownershipKeys < 1 {
		t.Fatalf("found %d minio.dataOwnership.image keys in %s/values*.yaml, want at least 1: the scan matched nothing, which is a failure, not a pass", ownershipKeys, minioChartDir)
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
	// form, the ownership init form or the mc form. The set of files is derived by walking the directory.
	serverLines, ownershipLines, mcLines := 0, 0, 0
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
			case minioOwnershipTemplateLine:
				ownershipLines++
			case minioMCTemplateLine:
				mcLines++
			default:
				t.Errorf("%s:%d: %q, want exactly %q, %q or %q", filepath.Base(f), i+1, line, minioTemplateLine, minioOwnershipTemplateLine, minioMCTemplateLine)
			}
		}
	}
	// the minio container in minio.yaml.
	if serverLines < 1 {
		t.Errorf("found %d server image lines in chart templates, want at least 1", serverLines)
	}
	if ownershipLines < 1 {
		t.Errorf("found %d ownership init image lines in chart templates, want at least 1", ownershipLines)
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
			if p := overrideImageRefProblem(v); p != "" {
				t.Errorf("%s:%d: mcImage %q: %s", filepath.Base(f), i+1, v, p)
			}
		}
	}
	if mcKeys < 1 {
		t.Errorf("found %d mcImage keys in %s/values*.yaml, want at least 1: the scan matched nothing, which is a failure, not a pass", mcKeys, minioChartDir)
	}

	vb, err := os.ReadFile(minioChartDir + "/values.yaml")
	if err != nil {
		t.Fatal(err)
	}
	imgNode, err := valuesScalarAt(vb, "minio", "image")
	if err != nil {
		t.Fatal(err)
	}
	mcNode, err := valuesScalarAt(vb, "minio", "mcImage")
	if err != nil {
		t.Fatal(err)
	}
	if imgNode == nil || mcNode == nil {
		t.Fatalf("values.yaml must define both minio.image and minio.mcImage (image=%v, mcImage=%v)", imgNode != nil, mcNode != nil)
	}
	if strings.TrimSpace(mcNode.Value) == "" {
		t.Errorf("values.yaml:%d: minio.mcImage is empty, want the same pinned image as minio.image (values.yaml:%d); a pin bump must update both keys", mcNode.Line, imgNode.Line)
	} else {
		_, imgDigest, _ := strings.Cut(imgNode.Value, "@")
		_, mcDigest, _ := strings.Cut(mcNode.Value, "@")
		if imgDigest == "" || imgDigest != mcDigest {
			t.Errorf("values.yaml:%d minio.mcImage digest %q differs from values.yaml:%d minio.image digest %q; a pin bump must update both keys", mcNode.Line, mcDigest, imgNode.Line, imgDigest)
		}
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

func TestOverrideImageRefRejectsUnpinnedForms(t *testing.T) {
	const hex = "fa23f6a6f62645654530ff94aa077d1cc0d0e44c8f1cce02ab039873612edc72"
	cases := []struct {
		name string
		ref  string
		ok   bool
	}{
		{"empty falls back to minio.image", "", true},
		{"pinned minio image", "cgr.dev/chainguard/minio@sha256:" + hex, true},
		{"registry with a port", "registry.example:5000/mirror/mc@sha256:" + hex, true},
		{"mirror with minio in the name", "registry.example.invalid/mirror/minio@sha256:" + hex, true},
		{"mc latest", "minio/mc:latest", false},
		{"tag in front of the digest", "registry.example:5000/mirror/mc:RELEASE.2025-08-13T08-35-41Z@sha256:" + hex, false},
		{"short digest", "registry.example/mirror/mc@sha256:fa23f6a6", false},
		{"uppercase digest", "registry.example/mirror/mc@sha256:" + strings.ToUpper(hex), false},
		{"no name in front of the digest", "@sha256:" + hex, false},
		{"registry with a port, no digest", "registry.example:5000/mirror/mc", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := overrideImageRefProblem(c.ref)
			if c.ok && got != "" {
				t.Errorf("%q rejected: %s", c.ref, got)
			}
			if !c.ok && got == "" {
				t.Errorf("%q accepted, want rejected", c.ref)
			}
		})
	}
}

func TestValuesScalarAtFollowsYAMLPaths(t *testing.T) {
	cases := []struct {
		name     string
		doc      string
		path     []string
		wantNil  bool
		wantVal  string
		wantLine int
	}{
		{"block style", "minio:\n  image: a\n  dataOwnership:\n    image: \"mirror/minio@sha256:x\"\n", []string{"minio", "dataOwnership", "image"}, false, "mirror/minio@sha256:x", 4},
		{"flow style", "minio: {dataOwnership: {image: x}}\n", []string{"minio", "dataOwnership", "image"}, false, "x", 1},
		{"image under minio is not dataOwnership.image", "minio:\n  image: a\n", []string{"minio", "dataOwnership", "image"}, true, "", 0},
		{"image under prometheus is not dataOwnership.image", "prometheus:\n  image: p\nminio:\n  dataOwnership:\n    uid: 1\n", []string{"minio", "dataOwnership", "image"}, true, "", 0},
		{"missing top-level key", "other: 1\n", []string{"minio", "dataOwnership", "image"}, true, "", 0},
		{"commented key is ignored", "minio:\n  dataOwnership:\n    # image: x\n    uid: 1\n", []string{"minio", "dataOwnership", "image"}, true, "", 0},
		{"empty value is found", "minio:\n  dataOwnership:\n    image: \"\"\n", []string{"minio", "dataOwnership", "image"}, false, "", 3},
		{"empty document", "", []string{"minio", "dataOwnership", "image"}, true, "", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n, err := valuesScalarAt([]byte(c.doc), c.path...)
			if err != nil {
				t.Fatal(err)
			}
			if c.wantNil {
				if n != nil {
					t.Fatalf("found %q at line %d, want nothing", n.Value, n.Line)
				}
				return
			}
			if n == nil {
				t.Fatal("found nothing, want a scalar")
			}
			if n.Value != c.wantVal || n.Line != c.wantLine {
				t.Errorf("got %q at line %d, want %q at line %d", n.Value, n.Line, c.wantVal, c.wantLine)
			}
		})
	}
	if _, err := valuesScalarAt([]byte("minio:\n  dataOwnership:\n    image: [a]\n"), "minio", "dataOwnership", "image"); err == nil {
		t.Error("a sequence at the path was accepted as a scalar")
	}
}
