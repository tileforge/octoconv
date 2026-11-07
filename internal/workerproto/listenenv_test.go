package workerproto

import (
	"strings"
	"testing"
)

func listenerGetenv(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

func directListenerValues() map[string]string {
	return map[string]string{
		"WORKER_ADDR":         ":8443",
		"WORKER_TLS_CERT":     "/etc/octoconv/worker-tls/server.crt",
		"WORKER_TLS_KEY":      "/etc/octoconv/worker-tls/server.key",
		"WORKER_CLIENT_CA":    "/etc/octoconv/worker-tls/ca.pem",
		"WORKER_ORIGIN":       "worker.octoconv.local",
		"WORKER_MANIFEST_DIR": "/etc/octoconv/worker-manifests",
		"WORKER_FILE_ACCESS":  "direct",
	}
}

func TestListenerEnvUnsetIsOff(t *testing.T) {
	enabled, env, err := ParseListenerEnv(func(string) string { return "" })
	if err != nil {
		t.Fatalf("unset env: %v", err)
	}
	if enabled {
		t.Fatal("unset env enabled the listener")
	}
	if env != (ListenerEnv{}) {
		t.Fatalf("unset env = %+v, want zero", env)
	}

	enabled, _, err = ParseListenerEnv(func(key string) string {
		if key == "WORKER_TLS_DISABLED" {
			return "true"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("disable flag: %v", err)
	}
	if enabled {
		t.Fatal("WORKER_TLS_DISABLED enabled the listener")
	}
}

func TestListenerEnvPartialFails(t *testing.T) {
	names := []string{
		"WORKER_ADDR",
		"WORKER_TLS_CERT",
		"WORKER_TLS_KEY",
		"WORKER_CLIENT_CA",
		"WORKER_ORIGIN",
		"WORKER_MANIFEST_DIR",
		"WORKER_FILE_ACCESS",
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			enabled, _, err := ParseListenerEnv(listenerGetenv(map[string]string{name: "set"}))
			if err == nil {
				t.Fatal("partial env returned nil error")
			}
			if enabled {
				t.Fatal("partial env enabled the listener")
			}
			if !strings.Contains(err.Error(), name) {
				t.Fatalf("error %q does not name %s", err, name)
			}
		})
	}

	full := directListenerValues()
	for _, name := range names {
		t.Run("missing "+name, func(t *testing.T) {
			values := directListenerValues()
			delete(values, name)
			enabled, _, err := ParseListenerEnv(listenerGetenv(values))
			if err == nil {
				t.Fatal("incomplete env returned nil error")
			}
			if enabled {
				t.Fatal("incomplete env enabled the listener")
			}
			if !strings.Contains(err.Error(), name) {
				t.Fatalf("error %q does not name %s", err, name)
			}
		})
	}
	if len(full) != 7 {
		t.Fatalf("direct set has %d values, want 7", len(full))
	}
}

func TestListenerEnvGatewayFails(t *testing.T) {
	for _, access := range []string{"gateway", "Gateway", "direct ", " direct", "direct # comment", "off", "false"} {
		t.Run(access, func(t *testing.T) {
			values := directListenerValues()
			values["WORKER_FILE_ACCESS"] = access
			enabled, _, err := ParseListenerEnv(listenerGetenv(values))
			if err == nil {
				t.Fatalf("file_access %q returned nil error", access)
			}
			if enabled {
				t.Fatalf("file_access %q enabled the listener", access)
			}
		})
	}
}

func TestListenerEnvDirectSucceeds(t *testing.T) {
	values := directListenerValues()
	enabled, got, err := ParseListenerEnv(listenerGetenv(values))
	if err != nil {
		t.Fatalf("direct: %v", err)
	}
	if !enabled {
		t.Fatal("direct did not enable the listener")
	}
	want := ListenerEnv{
		Addr:        values["WORKER_ADDR"],
		Cert:        values["WORKER_TLS_CERT"],
		Key:         values["WORKER_TLS_KEY"],
		ClientCA:    values["WORKER_CLIENT_CA"],
		Origin:      values["WORKER_ORIGIN"],
		ManifestDir: values["WORKER_MANIFEST_DIR"],
		FileAccess:  values["WORKER_FILE_ACCESS"],
	}
	if got != want {
		t.Fatalf("direct env = %+v, want %+v", got, want)
	}

	t.Run("does not trim", func(t *testing.T) {
		untrimmed := directListenerValues()
		untrimmed["WORKER_ORIGIN"] = "worker.octoconv.local # name"
		enabled, env, err := ParseListenerEnv(listenerGetenv(untrimmed))
		if err != nil || !enabled {
			t.Fatalf("enabled=%v err=%v", enabled, err)
		}
		if env.Origin != "worker.octoconv.local # name" {
			t.Fatalf("origin = %q, want the untrimmed value", env.Origin)
		}
	})
}
