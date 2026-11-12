package workerproto

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

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
		{name: "jpg above the image ceiling", format: "jpg", contentType: "image/jpeg", size: protocolDefaultOutputBytes + 1, want: false},
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
