// Package workerproto is the operator-approved worker catalog and the TLS session listener.
package workerproto

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrCapabilityConflict is returned when a second worker is approved for an
// operation, source format, and target format that another worker already owns.
var ErrCapabilityConflict = errors.New("capability format already approved")

// Repo is the Postgres catalog and session store.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo returns a catalog repository for the given pool.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// Approve stores an operator manifest. The stub rejects every call.
func (r *Repo) Approve(ctx context.Context, workerID uuid.UUID, fileBytes []byte) error {
	return errors.New("not implemented")
}
