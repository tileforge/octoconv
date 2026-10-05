package convert

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/apaderin/octoconv/internal/measurements"
)

const (
	cadMeasureDraftName = "measure.json"
	cadMeshSTLName      = "mesh.stl"
)

type CADRenderConverter struct{ CADConverter }

func (CADRenderConverter) Operation() string { return OperationRender }

func (CADRenderConverter) Pairs() []Pair {
	return []Pair{{From: "step", To: "png"}, {From: "iges", To: "png"}, {From: "stl", To: "png"}}
}

func CADMeasurementsPath(outPath string) string {
	return outPath + ".measurements.json"
}

func (c CADRenderConverter) Convert(ctx context.Context, inPath, outPath string, opts map[string]any) (err error) {
	parsed, err := CADOptsFromMap(opts)
	if err != nil {
		return fmt.Errorf("cad: %w", err)
	}
	format := NormalizeFormat(filepath.Ext(inPath))
	target := NormalizeFormat(filepath.Ext(outPath))
	if (format != "step" && format != "iges" && format != "stl") || target != "png" {
		return fmt.Errorf("cad: unsupported render %s -> %s", format, target)
	}
	if err := ValidateCADApplicability(EngineCAD, OperationRender, format, target, parsed); err != nil {
		return fmt.Errorf("cad: %w", err)
	}
	o := parsed.Resolved()

	info, err := os.Stat(inPath)
	if err != nil {
		return fmt.Errorf("cad: stat input: %w", err)
	}
	if ceiling := cadMaxBytes(format); info.Size() > ceiling {
		return fmt.Errorf("cad: %w: %s input is %d bytes against a %d-byte ceiling", ErrCADInputTooLarge, format, info.Size(), ceiling)
	}

	jobDir := filepath.Dir(outPath)
	draftPath := filepath.Join(jobDir, cadMeasureDraftName)
	sidecar := CADMeasurementsPath(outPath)
	defer func() {
		if err != nil {
			os.Remove(sidecar)
		}
	}()

	renderInput := inPath
	triangles := 0
	if format == "stl" {
		if _, err := c.statsStage(ctx, jobDir, inPath, draftPath); err != nil {
			return err
		}
	} else {
		if _, err := c.guardStage(ctx, jobDir, format, inPath); err != nil {
			return err
		}
		renderInput = filepath.Join(jobDir, cadMeshSTLName)
		verdict, err := c.meshStage(ctx, jobDir, format, inPath, renderInput, o.Quality, draftPath)
		if err != nil {
			return err
		}
		n, _, err := judgeBinarySTL(renderInput)
		if err != nil {
			return fmt.Errorf("cad: %w", err)
		}
		if err := requireSTLMatchesVerdict(n, verdict); err != nil {
			return err
		}
		triangles = n
	}

	draft, err := parseCADMeasureDraft(draftPath)
	if err != nil {
		return fmt.Errorf("cad: %w: %v", ErrCADMeasurementsInvalid, err)
	}
	if format == "stl" {
		triangles = draft.Triangles
	}
	if triangles > CADMaxRenderTriangles {
		return fmt.Errorf("cad: %w: %d triangles against a cap of %d", ErrCADTooComplex, triangles, CADMaxRenderTriangles)
	}
	if cadZeroExtent(draft.BBox) {
		return fmt.Errorf("cad: %w: the model has no extent", ErrCADEmptyResult)
	}
	doc, err := buildCADMeasurements(draft, o.AssumeUnit)
	if err != nil {
		return fmt.Errorf("cad: %w: %v", ErrCADMeasurementsInvalid, err)
	}

	if err := c.renderStage(ctx, jobDir, renderInput, outPath, o); err != nil {
		return err
	}

	f, err := os.Open(outPath)
	if err != nil {
		return fmt.Errorf("cad: %w: no image at %s: %v", ErrCADRenderRejected, outPath, err)
	}
	_, err = JudgeCADPNG(f, *o.Size, o.Background)
	f.Close()
	if err != nil {
		return err
	}

	body, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("cad: %w: %v", ErrCADMeasurementsInvalid, err)
	}
	if err := os.WriteFile(sidecar, body, 0o600); err != nil {
		return fmt.Errorf("cad: write measurements: %w", err)
	}
	return nil
}

func cadZeroExtent(b measurements.BBox) bool {
	for i := range b.Min {
		if i < len(b.Max) && b.Max[i]-b.Min[i] > 0 {
			return false
		}
	}
	return true
}

func (c CADRenderConverter) renderStage(ctx context.Context, jobDir, inPath, outPath string, o CADOpts) error {
	x, err := c.cadStage(jobDir, cadRenderReadOnly, cadRenderEngines)
	if err != nil {
		return err
	}
	timeout := c.renderTimeout()
	stageCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, cpu, err := x.runBounded(stageCtx, cadOutputLimit, "cad-render",
		"--view", o.View,
		"--size", strconv.Itoa(*o.Size),
		"--background", o.Background,
		inPath, outPath)
	v, err := classifyCADStage(cadStageRender, ctx, stageCtx, timeout, out, cpu, err)
	if err != nil {
		return err
	}
	if v.View != o.View || v.Size != *o.Size || v.Background != o.Background {
		return fmt.Errorf("cad render: %w: the renderer reported view %q size %d background %q, the request was %q %d %q",
			ErrCADRenderRejected, v.View, v.Size, v.Background, o.View, *o.Size, o.Background)
	}
	return nil
}
