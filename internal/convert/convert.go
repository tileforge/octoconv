// Package convert defines the Converter abstraction, a registry of supported
// format pairs, and the concrete engine implementations (libvips for images).
package convert

import (
	"context"
	"sort"
	"strings"
)

// Engine-class identifiers (D-01/DEBT-02). This is the SINGLE compile-time
// source of truth for engine-class string values -- referenced by
// Converter.Engine implementations (LibvipsConverter, LibreOfficeConverter,
// ChromiumConverter, and the future AudioConverter), the API routing switch
// (internal/api/handlers.go), the reconciler recovery-routing switch
// (internal/reconciler/reconciler.go), and the queue-name constants
// (internal/queue/queue.go). No other file may hold a raw
// "image"/"document"/"html"/"audio" engine-class literal.
const (
	EngineImage    = "image"
	EngineDocument = "document"
	EngineHTML     = "html"
	EngineAudio    = "audio"
	EngineAV       = "av"
	EngineOCR      = "ocr"
	EnginePandoc   = "pandoc"
)

// AllEngines returns the seven engine classes in a fixed, documented order --
// EngineImage, EngineDocument, EngineHTML, EngineAudio, EngineAV, EngineOCR,
// EnginePandoc. The order is stable because callers (cmd/manage-clients) use
// it to render operator-facing error text; a fresh slice is constructed on
// every call so a caller can never mutate the source of truth through the
// returned slice. Adding an eighth engine class means adding it here as well
// as to the const block above -- TestAllEnginesCoversEveryEngineClass fails
// if the two ever diverge from the registry.
func AllEngines() []string {
	return []string{
		EngineImage,
		EngineDocument,
		EngineHTML,
		EngineAudio,
		EngineAV,
		EngineOCR,
		EnginePandoc,
	}
}

// IsKnownEngine reports whether engine is exactly one of the values returned
// by AllEngines. The comparison is exact and case-sensitive with no
// trimming and no normalization: engine-class strings are stored verbatim in
// jobs.engine and client_limits.engine, and NormalizeFormat exists for file
// formats, not engine classes -- applying it here would silently accept
// "Image" and write a client_limits row that loadQuotaState's equality join
// (`l.engine = $2`) can never match, which is the exact operator-typo defect
// this function exists to reject. Do not "helpfully" add normalization here.
func IsKnownEngine(engine string) bool {
	for _, e := range AllEngines() {
		if engine == e {
			return true
		}
	}
	return false
}

// Pair is an ordered (source, target) format pair, e.g. {"png", "webp"}.
// Formats are always normalized (lowercase, canonical aliases).
type Pair struct {
	From string
	To   string
}

// Converter turns a file in one format into another by shelling out to an
// external engine. inPath and outPath are local filesystem paths; the output
// format is implied by outPath's extension.
type Converter interface {
	// Pairs reports the format pairs this converter can handle.
	Pairs() []Pair
	// Convert reads inPath and writes the converted result to outPath.
	Convert(ctx context.Context, inPath, outPath string, opts map[string]any) error
	// Engine reports the engine class this converter belongs to (e.g.
	// "image", "document") -- the single source of truth for engine-class
	// routing (D-01).
	Engine() string
}

// NormalizeFormat lowercases a format/extension and folds common aliases so the
// registry has a single canonical key per format.
func NormalizeFormat(f string) string {
	f = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(f), "."))
	switch f {
	case "jpeg":
		return "jpg"
	case "tif":
		return "tiff"
	case "htm":
		return "html"
	case "tex":
		// `.tex` is THE conventional LaTeX extension; `.latex` is rare. Without
		// this alias a normal paper.tex normalized to "tex", which matches no
		// registered pair, so every real LaTeX upload 422'd as unsupported and
		// handlers.go's `source == "latex"` content-detection branch was
		// unreachable. Same alias discipline as jpeg->jpg / tif->tiff / htm->html.
		return "latex"
	default:
		return f
	}
}

// Registry maps normalized format pairs to the converter that handles them.
type Registry struct {
	m map[Pair]Converter
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{m: make(map[Pair]Converter)}
}

// Register adds a converter for every pair it advertises. Later registrations
// override earlier ones for the same pair.
func (r *Registry) Register(c Converter) {
	for _, p := range c.Pairs() {
		r.m[Pair{From: NormalizeFormat(p.From), To: NormalizeFormat(p.To)}] = c
	}
}

// Lookup finds the converter for a (from, to) pair, normalizing inputs.
func (r *Registry) Lookup(from, to string) (Converter, bool) {
	c, ok := r.m[Pair{From: NormalizeFormat(from), To: NormalizeFormat(to)}]
	return c, ok
}

// Supports reports whether a (from, to) pair is convertible.
func (r *Registry) Supports(from, to string) bool {
	_, ok := r.Lookup(from, to)
	return ok
}

// EngineFor reports the engine class that handles a (from, to) pair (D-02),
// or ("", false) if the pair is unsupported.
func (r *Registry) EngineFor(from, to string) (string, bool) {
	c, ok := r.Lookup(from, to)
	if !ok {
		return "", false
	}
	return c.Engine(), true
}

// Classes returns every registered (from, to) pair grouped by engine class
// (D-06) -- the single registry-derived source GET /v1/formats walks. Each
// class's pairs are sorted deterministically (by From then To) so repeated
// calls and JSON encoding order stay stable; no engine/pair string literal
// exists anywhere else in the codebase (this is the only walk of r.m).
func (r *Registry) Classes() map[string][]Pair {
	out := make(map[string][]Pair)
	for pair, c := range r.m {
		class := c.Engine()
		out[class] = append(out[class], pair)
	}
	for class := range out {
		pairs := out[class]
		sort.Slice(pairs, func(i, j int) bool {
			if pairs[i].From != pairs[j].From {
				return pairs[i].From < pairs[j].From
			}
			return pairs[i].To < pairs[j].To
		})
		out[class] = pairs
	}
	return out
}

// Default is the process-wide registry populated by converters.go.
var Default = NewRegistry()
