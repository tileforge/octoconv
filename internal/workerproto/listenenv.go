package workerproto

import (
	"errors"
	"fmt"
	"strings"
)

// ListenerEnv is the worker TLS listener configuration read from the process environment.
type ListenerEnv struct {
	Addr        string
	Cert        string
	Key         string
	ClientCA    string
	Origin      string
	ManifestDir string
	FileAccess  string
}

var listenerEnvKeys = []string{
	"WORKER_ADDR",
	"WORKER_TLS_CERT",
	"WORKER_TLS_KEY",
	"WORKER_CLIENT_CA",
	"WORKER_ORIGIN",
	"WORKER_MANIFEST_DIR",
	"WORKER_FILE_ACCESS",
}

// ParseListenerEnv reports whether the worker TLS listener starts.
// An empty environment leaves it off. There is no switch that turns TLS off.
func ParseListenerEnv(getenv func(string) string) (bool, ListenerEnv, error) {
	if getenv == nil {
		return false, ListenerEnv{}, errors.New("worker listener: nil getenv")
	}
	values := make(map[string]string, len(listenerEnvKeys))
	set := make([]string, 0)
	missing := make([]string, 0)
	for _, key := range listenerEnvKeys {
		value := getenv(key)
		values[key] = value
		if value == "" {
			missing = append(missing, key)
			continue
		}
		set = append(set, key)
	}
	if len(set) == 0 {
		return false, ListenerEnv{}, nil
	}
	if len(missing) > 0 {
		return false, ListenerEnv{}, fmt.Errorf("worker listener: set %s; missing %s", strings.Join(set, ", "), strings.Join(missing, ", "))
	}
	env := ListenerEnv{
		Addr:        values["WORKER_ADDR"],
		Cert:        values["WORKER_TLS_CERT"],
		Key:         values["WORKER_TLS_KEY"],
		ClientCA:    values["WORKER_CLIENT_CA"],
		Origin:      values["WORKER_ORIGIN"],
		ManifestDir: values["WORKER_MANIFEST_DIR"],
		FileAccess:  values["WORKER_FILE_ACCESS"],
	}
	if env.FileAccess != "direct" {
		return false, ListenerEnv{}, errors.New("worker listener: WORKER_FILE_ACCESS must be direct")
	}
	return true, env, nil
}
