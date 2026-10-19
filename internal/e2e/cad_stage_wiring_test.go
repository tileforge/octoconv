package e2e

import (
	"strings"
	"testing"
)

func cadEnvValue(t *testing.T, block []string, path, service, key string) string {
	t.Helper()
	prefix := key + ":"
	for _, l := range block {
		if v, ok := strings.CutPrefix(strings.TrimSpace(l), prefix); ok {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	t.Fatalf("%s: services.%s.environment has no %s", path, service, key)
	return ""
}

func TestCADWorkerSignsProgressWithTheWebhookSecret(t *testing.T) {
	const (
		composeFile    = "../../docker-compose.yml"
		e2eComposeFile = "../../docker-compose.e2e.yml"
		cadDeployment  = "../../deploy/chart/octoconv/templates/deployment-cad-worker.yaml"
	)

	compose := cadReadFile(t, composeFile)
	cadEnv := cadBlock(t, compose, composeFile, "services", "cad-worker", "environment")
	webhookEnv := cadBlock(t, compose, composeFile, "services", "webhook-worker-1", "environment")
	got := cadEnvValue(t, cadEnv, composeFile, "cad-worker", "WEBHOOK_SIGNING_SECRET")
	want := cadEnvValue(t, webhookEnv, composeFile, "webhook-worker-1", "WEBHOOK_SIGNING_SECRET")
	if got == "" {
		t.Fatalf("%s: cad-worker WEBHOOK_SIGNING_SECRET is empty, so its stage webhooks are silently disabled", composeFile)
	}
	if got != want {
		t.Fatalf("%s: cad-worker WEBHOOK_SIGNING_SECRET = %q, webhook-worker-1 has %q: a client could not verify both with one key", composeFile, got, want)
	}

	e2e := cadReadFile(t, e2eComposeFile)
	hosts := cadBlock(t, e2e, e2eComposeFile, "services", "cad-worker", "extra_hosts")
	if !strings.Contains(strings.Join(hosts, "\n"), "host.docker.internal:host-gateway") {
		t.Fatalf("%s: services.cad-worker.extra_hosts = %q, want host.docker.internal:host-gateway", e2eComposeFile, hosts)
	}

	deployment := strings.Join(cadReadFile(t, cadDeployment), "\n")
	if !strings.Contains(deployment, `include "octoconv.commonEnv"`) {
		t.Fatalf("%s does not include octoconv.commonEnv, so the cad-worker pod gets no WEBHOOK_SIGNING_SECRET from octoconv-secret", cadDeployment)
	}
}
