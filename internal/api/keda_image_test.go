package api

import (
	"os"
	"strings"
	"testing"
)

func TestImageKEDAQueryUnchanged(t *testing.T) {
	body, err := os.ReadFile("../../deploy/chart/octoconv/templates/scaledobject-image.yaml")
	if err != nil {
		t.Fatal("scaled object")
	}
	const query = `sum(octoconv_queue_depth{queue="image", state=~"pending|active|retry"})`
	if !strings.Contains(string(body), query) {
		t.Fatal("image query")
	}
}
