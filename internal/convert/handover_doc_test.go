package convert

import (
	"os"
	"strings"
	"testing"
)

func TestWorkerHandoverSection(t *testing.T) {
	raw, err := os.ReadFile("../../docs/WORKER-HANDOVER.md")
	if err != nil {
		t.Fatal("WORKER-HANDOVER.md")
	}
	body := string(raw)
	for _, bad := range []string{"core_sha", "gh repo create", "internal/"} {
		if strings.Contains(body, bad) {
			t.Fatalf("handover contains %s", bad)
		}
	}
	needles := []string{
		"protocol_version 1",
		"docs/worker-protocol/v1.schema.json",
		"docs/worker-protocol/examples/",
		"docs/worker-protocol/conformance.md",
		"Do not change the core to add a worker.",
		"This repository does not publish a Python worker.",
		"A client certificate is required.",
		"There is no server flag to allow plaintext.",
		"Approve the manifest before HELLO.",
		"expires_at is the minimum of issued_at plus file_link_max_ttl, the attempt deadline, and the job deadline.",
		"It is not capped by lease_expires_at.",
		`sum(octoconv_queue_depth{queue="image", state=~"pending|active|retry"})`,
		"The lease stays 30 seconds.",
		"imagepdf does not get a ScaledObject.",
		"png to pdf stays 422.",
	}
	for _, needle := range needles {
		if n := strings.Count(body, needle); n != 1 {
			t.Fatalf("handover sentence count = %d", n)
		}
	}
	readme, err := os.ReadFile("../../docs/README.md")
	if err != nil {
		t.Fatal("README.md")
	}
	if !strings.Contains(string(readme), "WORKER-HANDOVER.md") {
		t.Fatal("README missing handover link")
	}
}
