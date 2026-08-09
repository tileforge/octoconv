package api

import (
	"net/http"

	"github.com/apaderin/octoconv/internal/convert"
)

// engineFormats is the JSON shape for a single engine class in the
// GET /v1/formats response: its supported (source, target) pairs, each
// rendered as a two-element [from, to] array.
type engineFormats struct {
	Pairs [][2]string `json:"pairs"`
}

// handleListFormats returns the registry-derived capability map (D-06):
// for each engine class, the (source, target) pairs convert.Default
// actually supports -- reshaped from convert.Default.Classes(), never a
// hardcoded pair list that can drift from the real registry.
func (s *Server) handleListFormats(w http.ResponseWriter, r *http.Request) {
	classes := convert.Default.Classes()

	// This loop is UNCHANGED from before Phase 57 -- not one of its lines
	// moved -- because it is exactly what makes the "engines" key byte-
	// identical to the pre-57 golden fixture (proven by
	// TestPre57GoldenListFormats and TestListFormats_EnginesKeyUnchanged).
	engines := make(map[string]engineFormats, len(classes))
	for class, pairs := range classes {
		rendered := make([][2]string, 0, len(pairs))
		for _, p := range pairs {
			rendered = append(rendered, [2]string{p.From, p.To})
		}
		engines[class] = engineFormats{Pairs: rendered}
	}

	// New {operation -> engine -> pairs} tree (57-04, D-10). Added
	// unconditionally, INCLUDING convert -- not "guarded on non-empty" --
	// because both the client and the two MCP entry points that plan 57-07
	// teaches about the operation field need somewhere to discover the full
	// operation list, rather than guess it.
	operationClasses := convert.Default.OperationClasses()
	operations := make(map[string]map[string]engineFormats, len(operationClasses))
	for operation, byEngine := range operationClasses {
		engines2 := make(map[string]engineFormats, len(byEngine))
		for class, pairs := range byEngine {
			rendered := make([][2]string, 0, len(pairs))
			for _, p := range pairs {
				rendered = append(rendered, [2]string{p.From, p.To})
			}
			engines2[class] = engineFormats{Pairs: rendered}
		}
		operations[operation] = engines2
	}

	// operations is a second, purely additive key in the SAME map literal
	// passed to writeJSON. Key order in this literal has no effect on the
	// wire: json.Encoder sorts map[string]any keys lexicographically, and
	// engines sorts before operations, so engines stays first in the
	// output -- this is exactly the property TestListFormats_EnginesKeyUnchanged's
	// prefix assertion relies on, not an accident of literal order.
	writeJSON(w, http.StatusOK, map[string]any{"engines": engines, "operations": operations})
}
