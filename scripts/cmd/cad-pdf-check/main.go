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
	svg := flag.String("svg", "", "the validated render-only SVG the PDF was drawn from")
	report := flag.Bool("report", false, "print one REPORT line per text with its rendered size and whether the PDF holds it")
	flag.Parse()
	if *report {
		if flag.NArg() != 1 || *texts == "" || *svg == "" || *maxBytes <= 0 {
			fmt.Fprintln(os.Stderr, "usage: cad-pdf-check --report --texts FILE --svg FILE [--max-bytes N] PDF")
			os.Exit(64)
		}
		rows, residual, err := convert.CADDXFPDFReport(flag.Arg(0), *texts, *svg, *maxBytes)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cad-pdf-check: %v\n", err)
			os.Exit(64)
		}
		present, lost, empty := 0, 0, 0
		for i, r := range rows {
			switch r.Present {
			case 1:
				present++
			case 0:
				lost++
			default:
				empty++
			}
			fmt.Printf("REPORT,i=%d,svg_pt=%.6g,pdf_pt=%.6g,present=%d,text=%s\n", i, r.SVGPt, r.PDFPt, r.Present, field(strings.ReplaceAll(r.Text, "=", ":")))
		}
		fmt.Printf("GOPDFREPORT,texts=%d,present=%d,lost=%d,empty=%d,residual=%d\n", len(rows), present, lost, empty, residual)
		return
	}
	if flag.NArg() != 1 || *maxBytes <= 0 || (*svg != "" && *texts == "") {
		fmt.Fprintln(os.Stderr, "usage: cad-pdf-check [--max-bytes N] [--texts FILE [--svg FILE]] [--print-text] PDF")
		os.Exit(64)
	}
	path := flag.Arg(0)
	var facts convert.CADPDFFacts
	var err error
	if *svg != "" {
		facts, err = convert.JudgeCADDXFPDFFiles(path, *texts, *svg, *maxBytes)
	} else {
		want := ""
		if *texts != "" {
			t, terr := convert.CADDXFExpectedTextFile(*texts, *maxBytes)
			if terr != nil {
				fmt.Fprintf(os.Stderr, "cad-pdf-check: %v\n", terr)
				os.Exit(64)
			}
			want = t
		}
		facts, err = convert.JudgeCADPDF(path, *maxBytes, want)
	}
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
		floor := ""
		if *svg != "" {
			floor = fmt.Sprintf(",below_render_floor=%d,below_floor_runes=%d,floor_pt=%g,invisible_text=%d,invisible_runes=%d",
				facts.BelowRenderFloor, facts.BelowFloorRunes, facts.FloorPt, facts.InvisibleText, facts.InvisibleRunes)
		}
		fmt.Printf("GOPDF,pdf=%s,ok=%d,pages=%d,paint_ops=%d,ink_ops=%d,text_runes=%d,expected_runes=%d%s,reason=%s\n",
			path, ok, facts.Pages, facts.PaintOps, facts.InkOps, facts.TextRunes, facts.ExpectedRunes, floor, reason)
	}
	if err != nil {
		if *printText {
			fmt.Fprintf(os.Stderr, "cad-pdf-check: %v\n", err)
		}
		os.Exit(1)
	}
}
