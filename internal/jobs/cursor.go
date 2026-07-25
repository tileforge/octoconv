package jobs

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Cursor is the decoded keyset pagination position: the (created_at, id)
// tuple Repo.List orders and tie-breaks by (D-09). Callers never construct
// this directly from client input — it only ever comes from DecodeCursor, or
// is built server-side from the last row of a page.
type Cursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// cursorSep separates the two fields in the cursor's decoded payload.
// CreatedAt is formatted as RFC3339Nano, which never contains "|", so a
// simple split is unambiguous.
const cursorSep = "|"

// EncodeCursor produces the opaque, base64-encoded pagination token for c.
// The encoding is an implementation detail (D-03, "Claude's Discretion"): the
// only external contract is that DecodeCursor(EncodeCursor(c)) == c.
func EncodeCursor(c Cursor) string {
	payload := c.CreatedAt.Format(time.RFC3339Nano) + cursorSep + c.ID.String()
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// DecodeCursor parses an opaque cursor token produced by EncodeCursor. Any
// structural problem — empty input, invalid base64, missing separator, an
// unparseable timestamp, or an invalid uuid — returns a wrapped error rather
// than panicking or silently returning a wrong Cursor (T-40-02).
func DecodeCursor(s string) (Cursor, error) {
	if s == "" {
		return Cursor{}, fmt.Errorf("decode cursor: empty cursor")
	}

	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, fmt.Errorf("decode cursor: %w", err)
	}

	parts := strings.SplitN(string(raw), cursorSep, 2)
	if len(parts) != 2 {
		return Cursor{}, fmt.Errorf("decode cursor: malformed payload (missing separator)")
	}

	createdAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return Cursor{}, fmt.Errorf("decode cursor: parse created_at: %w", err)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return Cursor{}, fmt.Errorf("decode cursor: parse id: %w", err)
	}

	return Cursor{CreatedAt: createdAt, ID: id}, nil
}
