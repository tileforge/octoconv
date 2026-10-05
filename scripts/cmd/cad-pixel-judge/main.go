package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/apaderin/octoconv/internal/convert"
)

func main() {
	size := flag.Int("size", 0, "requested frame size in pixels")
	background := flag.String("background", "opaque", "opaque or transparent")
	flag.Parse()
	if *size == 0 || flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: cad-pixel-judge --size N --background opaque|transparent PNG...")
		os.Exit(64)
	}
	failed := false
	for _, path := range flag.Args() {
		f, err := os.Open(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cad-pixel-judge: %v\n", err)
			os.Exit(64)
		}
		facts, err := convert.JudgeCADPNG(f, *size, *background)
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
		fmt.Printf("GOJUDGE,png=%s,ok=%d,mode=%s,w=%d,h=%d,count=%d,dark_count=%d,frac=%.4f,dark=%.4f,box=%dx%d,margin_px=%d,alpha=%d,frac_transparent=%.4f\n",
			path, ok, facts.Mode, facts.Width, facts.Height, facts.Count, facts.DarkCount, facts.Frac, facts.Dark, facts.BoxW, facts.BoxH, facts.Margin, alpha, facts.FracTransparent)
	}
	if failed {
		os.Exit(1)
	}
}
