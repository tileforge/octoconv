package convert

import (
	"os"
	"strings"
	"testing"
)

func TestProtocolRolloutSection(t *testing.T) {
	raw, err := os.ReadFile("../../docs/OPERATIONS.md")
	if err != nil {
		t.Fatal("OPERATIONS.md")
	}
	body := string(raw)
	needles := []string{
		"## Protocol image rollout",
		"There is no down migration.",
		"Do not drop a column and do not edit a migration that schema_migrations has recorded.",
		"The first protocol worker is the image worker.",
		"The legacy image worker stays up.",
		"Delete the image rows in worker_capability_formats before DrainWorker.",
		"Keep the worker_capabilities row until no queued protocol image job remains.",
		"A catalog miss enqueues the new image job.",
		"DrainWorker stops new pulls.",
		"A lease expiry with retry budget left returns that job to queued.",
		"Call DrainWorker only after the queued protocol image jobs and the active protocol image jobs are both gone.",
		"Do not change jobs.dispatch_owner.",
		"A done job keeps its job_outputs row.",
		"document, html, audio, av, ocr, pandoc, probe, and cad stay on asynq.",
		"png to pdf stays 422.",
		"imagepdf does not get a ScaledObject.",
		`sum(octoconv_queue_depth{queue="image", state=~"pending|active|retry"})`,
	}
	for _, needle := range needles {
		if n := strings.Count(body, needle); n != 1 {
			t.Fatalf("rollout sentence count = %d", n)
		}
	}
}
