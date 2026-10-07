package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/apaderin/octoconv/internal/convert"
)

func letterbox(svg string, size int, maxBytes int64) (convert.CADLetterbox, error) {
	f, err := os.Open(svg)
	if err != nil {
		return convert.CADLetterbox{}, err
	}
	defer f.Close()
	facts, err := convert.ValidateCADSVG(f, io.Discard, maxBytes)
	if err != nil {
		return convert.CADLetterbox{}, err
	}
	return convert.CADDrawingLetterbox(facts.WidthMM, facts.HeightMM, size)
}

func main() {
	size := flag.Int("size", 0, "requested frame size in pixels")
	background := flag.String("background", "opaque", "opaque or transparent")
	drawing := flag.Bool("drawing", false, "judge DXF drawing frames against the letterbox of --letterbox-svg")
	letterboxSVG := flag.String("letterbox-svg", "", "SVG whose root size sets the drawing letterbox")
	printLetterbox := flag.Bool("print-letterbox", false, "print the letterbox of --svg at --size and exit")
	svg := flag.String("svg", "", "SVG for --print-letterbox")
	maxSVG := flag.Int64("max-svg-bytes", 1<<30, "largest SVG accepted, in bytes")
	flag.Parse()
	if *printLetterbox {
		if *svg == "" || *size == 0 || flag.NArg() != 0 {
			fmt.Fprintln(os.Stderr, "usage: cad-pixel-judge --print-letterbox --svg FILE --size N")
			os.Exit(64)
		}
		box, err := letterbox(*svg, *size, *maxSVG)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cad-pixel-judge: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("LETTERBOX,sw=%d,sh=%d,left=%d,top=%d\n", box.Width, box.Height, box.Left, box.Top)
		return
	}
	if *size == 0 || flag.NArg() == 0 || *drawing != (*letterboxSVG != "") {
		fmt.Fprintln(os.Stderr, "usage: cad-pixel-judge --size N --background opaque|transparent [--drawing --letterbox-svg FILE] PNG...")
		os.Exit(64)
	}
	var box convert.CADLetterbox
	if *drawing {
		var err error
		box, err = letterbox(*letterboxSVG, *size, *maxSVG)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cad-pixel-judge: %v\n", err)
			os.Exit(64)
		}
	}
	failed := false
	for _, path := range flag.Args() {
		f, err := os.Open(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cad-pixel-judge: %v\n", err)
			os.Exit(64)
		}
		var facts convert.CADPixelFacts
		if *drawing {
			facts, err = convert.JudgeCADDrawingPNG(f, *size, *background, box)
		} else {
			facts, err = convert.JudgeCADPNG(f, *size, *background)
		}
		f.Close()
		ok := 1
		if err != nil {
			ok = 0
			failed = true
			fmt.Fprintf(os.Stderr, "cad-pixel-judge: %s: %v\n", path, err)
		}
		alpha := 0
		if facts.HasAlpha {
			alpha = 1
		}
		fmt.Printf("GOJUDGE,png=%s,ok=%d,mode=%s,w=%d,h=%d,count=%d,dark_count=%d,frac=%.4f,dark=%.4f,box=%dx%d,margin_px=%d,alpha=%d,frac_transparent=%.4f",
			path, ok, facts.Mode, facts.Width, facts.Height, facts.Count, facts.DarkCount, facts.Frac, facts.Dark, facts.BoxW, facts.BoxH, facts.Margin, alpha, facts.FracTransparent)
		if *drawing {
			fmt.Printf(",box_x=%d,box_y=%d,frac6=%.6f,lb_left=%d,lb_top=%d,lb_w=%d,lb_h=%d",
				facts.BoxX, facts.BoxY, facts.Frac, box.Left, box.Top, box.Width, box.Height)
		}
		fmt.Println()
	}
	if failed {
		os.Exit(1)
	}
}
