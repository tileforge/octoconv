package workerproto

import (
	"testing"

	"github.com/tileforge/octoconv/internal/convert"
)

func TestPandocManifestMatchesPandocPairs(t *testing.T) {
	parsed := mustParseCadManifest(t, "manifest-pandoc-convert.json")
	if parsed.id != "octoconv.convert.pandoc" || parsed.legacyEngine != convert.EnginePandoc || parsed.legacyOp != convert.OperationConvert || parsed.clientOperation != convert.OperationConvert {
		t.Fatalf("capability = %s op %s engine %s client %s", parsed.id, parsed.legacyOp, parsed.legacyEngine, parsed.clientOperation)
	}
	want := pandocManifestOrder()
	if len(parsed.formats) != len(want) {
		t.Fatalf("formats = %d, want %d", len(parsed.formats), len(want))
	}
	advertised := map[convert.Pair]struct{}{}
	for _, pair := range (convert.PandocConverter{}).Pairs() {
		advertised[pair] = struct{}{}
	}
	if len(advertised) != len(want) {
		t.Fatalf("Pairs() = %d, want %d", len(advertised), len(want))
	}
	for i, format := range parsed.formats {
		pair := convert.Pair{From: format.source, To: format.target}
		if pair != want[i] {
			t.Fatalf("pair %d = %s to %s, want %s to %s", i, pair.From, pair.To, want[i].From, want[i].To)
		}
		if _, ok := advertised[pair]; !ok {
			t.Fatalf("manifest pair %s to %s is not in pandocPairTable", pair.From, pair.To)
		}
		engine, ok := convert.Default.EngineForOp(convert.OperationConvert, format.source, format.target)
		if !ok || engine != convert.EnginePandoc {
			t.Fatalf("%s to %s engine %q ok %v", format.source, format.target, engine, ok)
		}
	}
}

func pandocManifestOrder() []convert.Pair {
	table := []struct {
		from string
		to   []string
	}{
		{"md", []string{"html", "docx", "epub", "rst", "org", "latex"}},
		{"rst", []string{"html", "docx", "epub", "md"}},
		{"org", []string{"html", "docx", "epub", "md"}},
		{"latex", []string{"html", "docx", "epub", "md"}},
		{"html", []string{"md", "docx", "epub", "rst", "latex"}},
		{"docx", []string{"md", "html", "rst", "org", "latex", "epub"}},
		{"epub", []string{"md", "html", "docx"}},
	}
	var pairs []convert.Pair
	for _, row := range table {
		for _, to := range row.to {
			pairs = append(pairs, convert.Pair{From: row.from, To: to})
		}
	}
	return pairs
}
