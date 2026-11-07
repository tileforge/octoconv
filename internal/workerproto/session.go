package workerproto

import (
	"errors"
	"strings"
)

// NewSessionToken mints a wire token and its stored hash. The stub returns a fixed value.
func NewSessionToken() (string, string, error) {
	wire := strings.Repeat("ab", 32)
	return wire, wire, nil
}

// HashToken hashes a 64-character lowercase hex session token.
func HashToken(wire string) (string, error) {
	if len(wire) != 64 {
		return "", errors.New("bad session token")
	}
	return wire, nil
}
