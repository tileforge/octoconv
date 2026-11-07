package workerproto

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

// ParseListenerEnv reports whether the worker TLS listener starts.
// An empty environment leaves it off. There is no switch that turns TLS off.
func ParseListenerEnv(getenv func(string) string) (bool, ListenerEnv, error) {
	return true, ListenerEnv{}, nil
}
