package jobs

import (
	"encoding/base64"
	"fmt"
	"hash/crc32"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Cursor is the decoded keyset pagination position: the (created_at, id)
// tuple Repo.List orders and tie-breaks by (D-09). Callers never construct
// this directly from client input — it only ever comes from DecodeCursor, or
// is built server-side from the last row of a page.
type Cursor struct {
	Sort      string
	Query     string
	CreatedAt time.Time
	ID        uuid.UUID
}

// cursorSep separates the fields in the cursor's decoded payload.
// CreatedAt is formatted as RFC3339Nano and the checksum is lowercase hex —
// neither ever contains "|", so a bounded split is unambiguous.
const cursorSep = "|"

// EncodeCursor produces the opaque, base64-encoded pagination token for c.
// The encoding is an implementation detail (D-03, "Claude's Discretion"): the
// external contract is that DecodeCursor(EncodeCursor(c)) == c, and that any
// corrupted/tampered token is rejected. A CRC32 checksum prefixes the payload
// so a single flipped character is detected deterministically rather than
// silently decoding to a different-but-valid (created_at, id). This is a
// corruption guard, not an authentication boundary — the real ownership
// backstop is the unconditional client_id scope in Repo.List's WHERE, so a
// keyed MAC (and its secret management) would be disproportionate here.
func EncodeCursor(c Cursor) string {
	payload := c.CreatedAt.Format(time.RFC3339Nano) + cursorSep + c.ID.String()
	if c.Sort != "" || c.Query != "" {
		payload += cursorSep + c.Sort + cursorSep + base64.RawURLEncoding.EncodeToString([]byte(c.Query))
	}
	sum := crc32.ChecksumIEEE([]byte(payload))
	full := fmt.Sprintf("%08x", sum) + cursorSep + payload
	return base64.RawURLEncoding.EncodeToString([]byte(full))
}

// DecodeCursor parses an opaque cursor token produced by EncodeCursor. Any
// structural problem — empty input, invalid base64, missing separator, a
// checksum mismatch (corruption/tampering), an unparseable timestamp, or an
// invalid uuid — returns a wrapped error rather than panicking or silently
// returning a wrong Cursor (T-40-02).
func DecodeCursor(s string) (Cursor, error) {
	if s == "" || len(s) > 2048 {
		return Cursor{}, fmt.Errorf("decode cursor: empty cursor")
	}

	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, fmt.Errorf("decode cursor: %w", err)
	}

	// Split off the checksum prefix first, then validate it against the
	// remaining payload before trusting any field.
	sumAndPayload := strings.SplitN(string(raw), cursorSep, 2)
	if len(sumAndPayload) != 2 {
		return Cursor{}, fmt.Errorf("decode cursor: malformed payload (missing checksum)")
	}
	payload := sumAndPayload[1]
	want := fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(payload)))
	if sumAndPayload[0] != want {
		return Cursor{}, fmt.Errorf("decode cursor: checksum mismatch (corrupted or tampered cursor)")
	}

	parts := strings.SplitN(payload, cursorSep, 4)
	if len(parts) != 2 && len(parts) != 4 {
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

	c := Cursor{CreatedAt: createdAt, ID: id}
	if len(parts) == 4 {
		query, err := base64.RawURLEncoding.DecodeString(parts[3])
		if err != nil {
			return Cursor{}, fmt.Errorf("decode cursor query: %w", err)
		}
		f, err := NormalizeListFilter(ListFilter{Query: string(query), Sort: parts[2]})
		if err != nil || f.Query != string(query) || (parts[2] != "" && f.Sort != parts[2]) {
			return Cursor{}, fmt.Errorf("decode cursor: invalid query context")
		}
		c.Query = f.Query
		c.Sort = parts[2]
	}
	return c, nil
}
