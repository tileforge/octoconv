package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/tileforge/octoconv/internal/convert"
)

func field(s string) string {
	return strings.NewReplacer(",", ";", "\n", " ", "\r", " ").Replace(s)
}

func main() {
	maxBytes := flag.Int64("max-bytes", 1<<30, "largest SVG accepted, in bytes")
	out := flag.String("out", "", "write the re-serialized SVG here")
	flag.Parse()
	if flag.NArg() != 1 || *maxBytes <= 0 {
		fmt.Fprintln(os.Stderr, "usage: cad-svg-check [--max-bytes N] [--out FILE] SVG")
		os.Exit(64)
	}
	path := flag.Arg(0)
	in, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cad-svg-check: %v\n", err)
		os.Exit(64)
	}
	defer in.Close()
	var w io.Writer = io.Discard
	var outFile *os.File
	if *out != "" {
		outFile, err = os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cad-svg-check: %v\n", err)
			os.Exit(64)
		}
		w = outFile
	}
	facts, err := convert.ValidateCADSVG(in, w, *maxBytes)
	if outFile != nil {
		if cerr := outFile.Close(); err == nil && cerr != nil {
			err = cerr
		}
		if err != nil {
			os.Remove(*out)
		}
	}
	if err != nil {
		fmt.Printf("GOSVG,svg=%s,ok=0,width_mm=0,height_mm=0,texts=0,visible=0,reason=%s\n", path, field(err.Error()))
		os.Exit(1)
	}
	fmt.Printf("GOSVG,svg=%s,ok=1,width_mm=%g,height_mm=%g,texts=%d,visible=%d,reason=none\n", path, facts.WidthMM, facts.HeightMM, len(facts.Texts), facts.Visible)
}
