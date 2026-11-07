package workerproto

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const workerURNPrefix = "urn:octoconv:worker:"

// ListenerConfig returns the worker listener TLS policy.
// CipherSuites is left unset because that Go field does not select TLS 1.3 suites.
func ListenerConfig(pool *x509.CertPool, verify func(tls.ConnectionState) error) *tls.Config {
	if pool == nil {
		pool = x509.NewCertPool()
	}
	cfg := &tls.Config{
		MinVersion:             tls.VersionTLS13,
		MaxVersion:             tls.VersionTLS13,
		ClientAuth:             tls.RequireAndVerifyClientCert,
		ClientCAs:              pool,
		SessionTicketsDisabled: true,
		CurvePreferences:       []tls.CurveID{tls.X25519, tls.CurveP256},
		Renegotiation:          tls.RenegotiateNever,
	}
	if verify != nil {
		cfg.VerifyConnection = verify
	}
	return cfg
}

// ServerTLSConfig returns a TLS 1.3 config that accepts only an operator-approved worker certificate.
func ServerTLSConfig(repo *Repo) (*tls.Config, error) {
	if repo == nil {
		return nil, errors.New("worker tls: nil repo")
	}
	return ListenerConfig(x509.NewCertPool(), verifyWorkerPeer(repo)), nil
}

func verifyWorkerPeer(repo *Repo) func(tls.ConnectionState) error {
	return func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) == 0 {
			return errors.New("worker tls: no peer certificate")
		}
		leaf := cs.PeerCertificates[0]
		if !allowedLeafSignature(leaf.SignatureAlgorithm) {
			return errors.New("worker tls: leaf signature")
		}
		workerID, err := workerIDFromLeaf(leaf)
		if err != nil {
			return err
		}
		serial, err := certSerial(leaf)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		approved, err := repo.identityExists(ctx, workerID)
		if err != nil {
			return fmt.Errorf("worker tls: identity: %w", err)
		}
		if !approved {
			return errors.New("worker tls: worker is not approved")
		}
		denied, err := repo.serialDenied(ctx, issuerSHA256(leaf), serial)
		if err != nil {
			return fmt.Errorf("worker tls: denylist: %w", err)
		}
		if denied {
			return errors.New("worker tls: certificate denied")
		}
		return nil
	}
}

func allowedLeafSignature(alg x509.SignatureAlgorithm) bool {
	switch alg {
	case x509.SHA256WithRSA, x509.SHA256WithRSAPSS, x509.ECDSAWithSHA256:
		return true
	default:
		return false
	}
}

func workerIDFromLeaf(leaf *x509.Certificate) (uuid.UUID, error) {
	if leaf == nil || len(leaf.URIs) != 1 || leaf.URIs[0] == nil {
		return uuid.Nil, errors.New("worker tls: uri san")
	}
	raw := leaf.URIs[0].String()
	if !strings.HasPrefix(raw, workerURNPrefix) {
		return uuid.Nil, errors.New("worker tls: uri san")
	}
	suffix := raw[len(workerURNPrefix):]
	id, err := uuid.Parse(suffix)
	if err != nil || id.String() != suffix {
		return uuid.Nil, errors.New("worker tls: uri san")
	}
	return id, nil
}

func certSerial(leaf *x509.Certificate) (string, error) {
	if leaf == nil || leaf.SerialNumber == nil {
		return "", errors.New("worker tls: missing serial")
	}
	return leaf.SerialNumber.String(), nil
}

func issuerSHA256(leaf *x509.Certificate) string {
	sum := sha256.Sum256(leaf.RawIssuer)
	return hex.EncodeToString(sum[:])
}
