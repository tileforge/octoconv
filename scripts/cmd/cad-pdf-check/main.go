package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/apaderin/octoconv/internal/convert"
)

func field(s string) string {
	return strings.NewReplacer(",", ";", "\n", " ", "\r", " ").Replace(s)
}

func main() {
	maxBytes := flag.Int64("max-bytes", 1<<30, "largest PDF and texts.json accepted, in bytes")
	texts := flag.String("texts", "", "texts.json whose covered characters the PDF text must contain in order")
	printText := flag.Bool("print-text", false, "print the decoded text instead of the facts line")
	flag.Parse()
	if flag.NArg() != 1 || *maxBytes <= 0 {
		fmt.Fprintln(os.Stderr, "usage: cad-pdf-check [--max-bytes N] [--texts FILE] [--print-text] PDF")
		os.Exit(64)
	}
	path := flag.Arg(0)
	want := ""
	if *texts != "" {
		t, err := convert.CADDXFExpectedTextFile(*texts, *maxBytes)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cad-pdf-check: %v\n", err)
			os.Exit(64)
		}
		want = t
	}
	facts, err := convert.JudgeCADPDF(path, *maxBytes, want)
	if err != nil && !errors.Is(err, convert.ErrCADPDFRejected) {
		fmt.Fprintf(os.Stderr, "cad-pdf-check: %v\n", err)
		os.Exit(64)
	}
	if *printText {
		fmt.Print(facts.Text)
	} else {
		ok, reason := 1, "none"
		if err != nil {
			ok, reason = 0, field(err.Error())
		}
		fmt.Printf("GOPDF,pdf=%s,ok=%d,pages=%d,paint_ops=%d,text_runes=%d,expected_runes=%d,reason=%s\n",
			path, ok, facts.Pages, facts.PaintOps, facts.TextRunes, facts.ExpectedRunes, reason)
	}
	if err != nil {
		if *printText {
			fmt.Fprintf(os.Stderr, "cad-pdf-check: %v\n", err)
		}
		os.Exit(1)
	}
}
