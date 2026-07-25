package jobs

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCursorRoundTrip(t *testing.T) {
	c := Cursor{CreatedAt: time.Now(), ID: uuid.New()}

	enc := EncodeCursor(c)
	if enc == "" {
		t.Fatalf("EncodeCursor returned empty string")
	}

	got, err := DecodeCursor(enc)
	if err != nil {
		t.Fatalf("DecodeCursor: %v", err)
	}
	if !got.CreatedAt.Equal(c.CreatedAt) {
		t.Fatalf("CreatedAt = %v, want %v", got.CreatedAt, c.CreatedAt)
	}
	// RFC3339Nano preserves nanosecond precision, so the formatted string
	// representation must match exactly (not just Equal, which treats
	// different-but-simultaneous-instant timezones as equal).
	if got.CreatedAt.Format(time.RFC3339Nano) != c.CreatedAt.Format(time.RFC3339Nano) {
		t.Fatalf("CreatedAt lost precision: got %s, want %s",
			got.CreatedAt.Format(time.RFC3339Nano), c.CreatedAt.Format(time.RFC3339Nano))
	}
	if got.ID != c.ID {
		t.Fatalf("ID = %v, want %v", got.ID, c.ID)
	}
}

func TestCursorDecodeEmpty(t *testing.T) {
	if _, err := DecodeCursor(""); err == nil {
		t.Fatalf("expected error decoding empty cursor, got nil")
	}
}

func TestCursorDecodeNonBase64(t *testing.T) {
	if _, err := DecodeCursor("!!!not-base64!!!"); err == nil {
		t.Fatalf("expected error decoding non-base64 cursor, got nil")
	}
}

func TestCursorDecodeStructurallyWrong(t *testing.T) {
	cases := map[string]string{
		"missing separator":     rawURLEncode(t, "no-separator-here"),
		"unparseable timestamp": rawURLEncode(t, "not-a-time|"+uuid.New().String()),
		"bad uuid":              rawURLEncode(t, time.Now().Format(time.RFC3339Nano)+"|not-a-uuid"),
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeCursor(s); err == nil {
				t.Fatalf("expected error decoding %s, got nil", name)
			}
		})
	}
}

func TestCursorDecodeTampered(t *testing.T) {
	c := Cursor{CreatedAt: time.Now(), ID: uuid.New()}
	enc := EncodeCursor(c)

	// Flip a byte in the middle of the encoded string to simulate tampering.
	b := []byte(enc)
	mid := len(b) / 2
	if b[mid] == 'A' {
		b[mid] = 'B'
	} else {
		b[mid] = 'A'
	}
	tampered := string(b)

	got, err := DecodeCursor(tampered)
	if err == nil {
		// A tampered cursor must never silently decode to a DIFFERENT valid
		// cursor either - it must error, not just "possibly differ".
		if got.CreatedAt.Equal(c.CreatedAt) && got.ID == c.ID {
			t.Fatalf("tampered cursor decoded identically to original — mutation had no effect")
		}
		t.Fatalf("expected error decoding tampered cursor, got %+v", got)
	}
}

// rawURLEncode is a small test helper mirroring the codec's own encoding, so
// TestCursorDecodeStructurallyWrong can construct base64-valid-but-
// structurally-wrong payloads without depending on cursor.go internals.
func rawURLEncode(t *testing.T, payload string) string {
	t.Helper()
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}
