package jobs

import (
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func TestNormalizeListSearch(t *testing.T) {
	cases := []struct {
		name, query, sort string
		valid             bool
	}{
		{name: "defaults", valid: true}, {name: "trim", query: " report ", sort: "oldest", valid: true},
		{name: "literal", query: `%_\`, sort: "newest", valid: true},
		{name: "bad order", sort: "created_at; DROP TABLE jobs", valid: false},
		{name: "too long", query: strings.Repeat("x", 257), valid: false},
		{name: "nul", query: "a\x00b", valid: false},
		{name: "invalid utf8", query: string([]byte{255}), valid: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := NormalizeListFilter(ListFilter{Query: tc.query, Sort: tc.sort})
			if (err == nil) != tc.valid {
				t.Fatalf("filter=%+v err=%v", f, err)
			}
			if tc.valid && f.Query != strings.TrimSpace(tc.query) {
				t.Fatalf("query=%q", f.Query)
			}
			if tc.valid && tc.sort == "" && f.Sort != "newest" {
				t.Fatalf("sort=%q", f.Sort)
			}
		})
	}
}
func TestSearchCursorRoundtripAndContext(t *testing.T) {
	for _, c := range []Cursor{
		{CreatedAt: time.Unix(100, 0).UTC(), ID: uuid.New(), Query: "default order"},
		{CreatedAt: time.Unix(100, 0).UTC(), ID: uuid.New()},
		{CreatedAt: time.Unix(100, 0).UTC(), ID: uuid.New(), Sort: "oldest", Query: "Отчёт|%_"},
		{CreatedAt: time.Unix(100, 0).UTC(), ID: uuid.New(), Sort: "newest", Query: "report"},
	} {
		decoded, err := DecodeCursor(EncodeCursor(c))
		if err != nil || decoded != c {
			t.Fatalf("roundtrip: %+v %v", decoded, err)
		}
		f, _ := NormalizeListFilter(ListFilter{Query: c.Query, Sort: c.Sort})
		if !decoded.Matches(f) {
			t.Fatal("matching context refused")
		}
		f.Query += "different"
		if decoded.Matches(f) {
			t.Fatal("different query accepted")
		}
	}
}
