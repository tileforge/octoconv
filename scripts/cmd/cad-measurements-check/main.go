package main

import (
	"fmt"
	"math"
	"os"
	"sort"

	"github.com/tileforge/octoconv/internal/measurements"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: cad-measurements-check FILE")
		os.Exit(64)
	}
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "cad-measurements-check: %v\n", err)
		os.Exit(64)
	}
	doc, err := measurements.Validate(raw)
	if err != nil {
		fmt.Printf("MEAS,valid=0,error=%q\n", err.Error())
		os.Exit(1)
	}
	extents := make([]float64, len(doc.BBox.Min))
	for i := range extents {
		extents[i] = doc.BBox.Max[i] - doc.BBox.Min[i]
	}
	sort.Float64s(extents)
	ext := ""
	for i, e := range extents {
		if i > 0 {
			ext += ":"
		}
		ext += fmt.Sprintf("%.6g", e)
	}
	reason := "none"
	if doc.VolumeNullReason != "" {
		reason = doc.VolumeNullReason
	} else if doc.AreaNullReason != "" {
		reason = doc.AreaNullReason
	}
	fmt.Printf("MEAS,format=%s,unit=%s,unit_source=%s,extents=%s,volume=%s,area=%s,reason=%s,valid=1\n",
		doc.Format, doc.Unit, doc.UnitSource, ext, quantity(doc.Volume), quantity(doc.Area), reason)
}

func quantity(v *float64) string {
	if v == nil {
		return "null"
	}
	if math.IsNaN(*v) {
		return "nan"
	}
	return fmt.Sprintf("%.6g", *v)
}
