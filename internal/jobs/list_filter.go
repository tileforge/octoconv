package jobs

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// NormalizeListFilter validates the query and the closed chronological sort set.
// Status and engine validation remain in the API handler.
func NormalizeListFilter(f ListFilter) (ListFilter, error) {
	f.Query = strings.TrimSpace(f.Query)
	if len(f.Query) > 256 || !utf8.ValidString(f.Query) || strings.ContainsRune(f.Query, 0) {
		return ListFilter{}, fmt.Errorf("query must be valid UTF-8, at most 256 bytes, without NUL")
	}
	if f.Sort == "" {
		f.Sort = "newest"
	}
	if f.Sort != "newest" && f.Sort != "oldest" {
		return ListFilter{}, fmt.Errorf("sort must be newest or oldest")
	}
	return f, nil
}

// Matches binds a pagination position to its search and chronological order.
func (c Cursor) Matches(f ListFilter) bool {
	sort := c.Sort
	if sort == "" {
		sort = "newest"
	}
	return sort == f.Sort && c.Query == f.Query
}
