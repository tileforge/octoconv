package workerproto

import (
	"testing"

	"github.com/tileforge/octoconv/internal/jobs"
)

func TestTectonicSourceRows(t *testing.T) {
	t.Run("ordinal zero leads and an empty extra format comes from the filename", func(t *testing.T) {
		got, ok := tectonicSourceRows([]storedJobInput{
			{Ordinal: 1, Key: "uploads/job/1-refs.bib", Filename: "refs.bib"},
			{Ordinal: 0, Key: "uploads/job/0-paper.tex", Format: "latex", Filename: "paper.tex"},
		})
		if !ok || len(got) != 2 {
			t.Fatalf("ok %v rows %+v", ok, got)
		}
		if got[0].Ordinal != 0 || got[0].Format != "latex" || got[1].Ordinal != 1 || got[1].Format != "bib" {
			t.Fatalf("rows %+v", got)
		}
	})

	t.Run("a gap is allowed and extra order is kept", func(t *testing.T) {
		got, ok := tectonicSourceRows([]storedJobInput{
			{Ordinal: 2, Key: "b", Filename: "refs.bib"},
			{Ordinal: 0, Key: "a", Format: " tex ", Filename: "paper.tex"},
		})
		if !ok || len(got) != 2 || got[0].Ordinal != 0 || got[0].Format != "latex" || got[1].Ordinal != 2 || got[1].Format != "bib" {
			t.Fatalf("ok %v rows %+v", ok, got)
		}
	})

	t.Run("an extensionless extra is bin", func(t *testing.T) {
		got, ok := tectonicSourceRows([]storedJobInput{
			{Ordinal: 0, Key: "a", Format: "latex", Filename: "paper.tex"},
			{Ordinal: 1, Key: "b", Filename: "notes"},
		})
		if !ok || len(got) != 2 || got[1].Format != "bin" {
			t.Fatalf("ok %v rows %+v", ok, got)
		}
	})

	t.Run("a stored tex format on an extra normalizes to latex", func(t *testing.T) {
		got, ok := tectonicSourceRows([]storedJobInput{
			{Ordinal: 0, Key: "a", Format: "latex", Filename: "paper.tex"},
			{Ordinal: 1, Key: "b", Format: "tex", Filename: "refs.bib"},
		})
		if !ok || len(got) != 2 || got[1].Format != "latex" {
			t.Fatalf("ok %v rows %+v", ok, got)
		}
	})

	t.Run("the part ceiling is the job maximum", func(t *testing.T) {
		full := make([]storedJobInput, jobs.MaxInputParts)
		for i := range full {
			full[i] = storedJobInput{Ordinal: i, Key: "k", Format: "latex", Filename: "a.tex"}
		}
		if _, ok := tectonicSourceRows(full); !ok {
			t.Fatal("a full set was refused")
		}
		full = append(full, storedJobInput{Ordinal: jobs.MaxInputParts, Key: "k", Format: "latex", Filename: "a.tex"})
		if _, ok := tectonicSourceRows(full); ok {
			t.Fatal("one past the maximum was claimed")
		}
	})

	refused := []struct {
		name string
		rows []storedJobInput
	}{
		{name: "empty", rows: nil},
		{name: "missing ordinal zero", rows: []storedJobInput{{Ordinal: 1, Key: "b", Format: "bib", Filename: "refs.bib"}}},
		{name: "duplicate ordinal", rows: []storedJobInput{
			{Ordinal: 0, Key: "a", Format: "latex", Filename: "paper.tex"},
			{Ordinal: 0, Key: "b", Format: "latex", Filename: "other.tex"},
		}},
		{name: "empty ordinal zero format", rows: []storedJobInput{{Ordinal: 0, Key: "a", Format: "  ", Filename: "paper.tex"}}},
		{name: "negative ordinal", rows: []storedJobInput{
			{Ordinal: 0, Key: "a", Format: "latex", Filename: "paper.tex"},
			{Ordinal: -1, Key: "b", Format: "bib", Filename: "refs.bib"},
		}},
		{name: "empty key", rows: []storedJobInput{{Ordinal: 0, Key: "", Format: "latex", Filename: "paper.tex"}}},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := tectonicSourceRows(tc.rows); ok {
				t.Fatal("claimed")
			}
		})
	}
}
