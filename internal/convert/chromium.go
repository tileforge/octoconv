package convert

import (
	"context"
	"errors"
)

// ChromiumConverter admits html to pdf. Conversion runs in
// tileforge/octoconv-worker-html. Convert fails closed so this process
// cannot shell out to chromium.
type ChromiumConverter struct{}

// Pairs returns the single html to pdf pair.
func (ChromiumConverter) Pairs() []Pair {
	return []Pair{{From: "html", To: "pdf"}}
}

// Engine returns the html engine class.
func (ChromiumConverter) Engine() string { return EngineHTML }

// Convert refuses the call. The in-repo chromium process has been removed.
func (ChromiumConverter) Convert(context.Context, string, string, map[string]any) error {
	return errors.New("chromium: the in-repo engine is retired")
}
