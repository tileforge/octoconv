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
		"protocol_version 2",
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
		"tileforge/octoconv-worker-<engine>",
		"tileforge/octoconv-worker-cad",
		"<ENGINE>_PROTOCOL_CORE_URL",
		"<ENGINE>_PROTOCOL_CLIENT_CERT",
		"<ENGINE>_PROTOCOL_CLIENT_KEY",
		"<ENGINE>_PROTOCOL_CLIENT_CA",
		"<ENGINE>_PROTOCOL_WORKER_ID",
		"<ENGINE>_PROTOCOL_MANIFEST_PATH",
		"CAD_PROTOCOL_",
		"no `DATABASE_URL`",
		"no `REDIS_ADDR`",
		"manifest-cad-convert.json",
		"manifest-cad-render.json",
		"octoconv.convert.cad",
		"octoconv.render.cad",
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
