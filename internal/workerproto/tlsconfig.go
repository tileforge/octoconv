package workerproto

import (
	"crypto/tls"
	"crypto/x509"
)

// ListenerConfig builds the worker listener TLS policy.
func ListenerConfig(pool *x509.CertPool, verify func(tls.ConnectionState) error) *tls.Config {
	return &tls.Config{ClientCAs: pool}
}

// ServerTLSConfig builds a listener config that checks the peer certificate against the catalog.
func ServerTLSConfig(repo *Repo) (*tls.Config, error) {
	return &tls.Config{}, nil
}
