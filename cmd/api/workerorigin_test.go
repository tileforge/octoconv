package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkerLeafAllowsOriginUsesPlatformVerifier(t *testing.T) {
	tests := []struct {
		name    string
		dns     []string
		cn      string
		origin  string
		allowed bool
	}{
		{name: "exact dns", dns: []string{"worker.example.com"}, origin: "worker.example.com", allowed: true},
		{name: "case", dns: []string{"worker.example.com"}, origin: "Worker.Example.COM", allowed: true},
		{name: "wildcard", dns: []string{"*.example.com"}, origin: "worker.example.com", allowed: true},
		{name: "wildcard parent", dns: []string{"*.example.com"}, origin: "example.com", allowed: false},
		{name: "extra label", dns: []string{"*.example.com"}, origin: "a.worker.example.com", allowed: false},
		{name: "other name", dns: []string{"worker.example.com"}, origin: "other.example.com", allowed: false},
		{name: "common name only", cn: "worker.example.com", origin: "worker.example.com", allowed: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := writeServerLeaf(t, tt.dns, tt.cn)
			err := workerLeafAllowsOrigin(path, tt.origin)
			if tt.allowed && err != nil {
				t.Fatalf("origin %s: %v", tt.origin, err)
			}
			if !tt.allowed && err == nil {
				t.Fatalf("origin %s was accepted", tt.origin)
			}
		})
	}
}

func writeServerLeaf(t *testing.T, dns []string, cn string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	if cn == "" {
		cn = "leaf"
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     dns,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	path := filepath.Join(t.TempDir(), "server.crt")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}
