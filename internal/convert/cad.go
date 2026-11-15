package convert

import (
	"context"
	"errors"
	"time"
)

const (
	CADMaxSTEPBytes         = 63 << 20
	CADMaxIGESBytes         = 120 << 20
	CADMaxSTEPEntities      = 754762
	CADMaxNAUO              = 1518
	CADMaxRenderTriangles   = 3835382
	CADMaxSTLBytes          = 190840832
	CADDefaultGuardTimeout  = 21 * time.Second
	CADDefaultMeshTimeout   = 324 * time.Second
	CADDefaultRenderTimeout = 521 * time.Second
	CADIOAllowance          = 20 * time.Second
	CADDefaultEngineTimeout = max(
		CADDefaultGuardTimeout+CADDefaultMeshTimeout+CADDefaultRenderTimeout+CADIOAllowance,
		CADDefaultGuardTimeout+CADDefaultDXFTimeout+2*CADDefaultRsvgTimeout+CADIOAllowance,
		CADDefaultGuardTimeout+CADDefaultDWGTimeout+CADDefaultDXFTimeout+2*CADDefaultRsvgTimeout+CADIOAllowance)

	CADMaxDXFBytes            = 118 << 20
	CADMaxDXFExpandedInserts  = 170031
	CADMaxDXFExpandedEntities = 850155
	CADMaxDXFNesting          = 16
	CADMaxDXFSVGBytes         = 134 << 20
	CADMaxDXFPDFBytes         = 19 << 20
	CADDefaultDXFTimeout      = 186 * time.Second
	CADDefaultDWGTimeout      = 186 * time.Second
	CADDefaultRsvgTimeout     = 217 * time.Second

	CADDXFPDFTextFloorPt = 0.00469174
	CADDXFInkSize        = CADMaxRenderSize
	CADInkChannelCutoff  = cadBackgroundCutoff

	cadRenderMargin     = 0.06
	cadMarginMin        = 0.03
	cadBackgroundCutoff = 250
	cadPDFInflateFactor = 64

	cadStageGuard  = "guard"
	cadStageMesh   = "mesh"
	cadStageRender = "render"
	cadStageDXF    = "dxf"
	cadStageDWG    = "dwg"
	cadStageRsvg   = "rsvg"
	cadStageInk    = "ink"
	cadStageJudge  = "judge"
)

var (
	ErrCADInputTooLarge  = errors.New("cad: the input exceeds this deployment's size ceiling")
	ErrCADTooComplex     = errors.New("cad: the model is too complex to convert")
	ErrCADUnreadable     = errors.New("cad: the file could not be read as CAD data")
	ErrCADEmptyResult    = errors.New("cad: the conversion produced no geometry")
	ErrCADMeshTimeout    = errors.New("cad: the mesh stage did not finish within the time limit")
	ErrCADEngineKilled   = errors.New("cad: the engine was killed before it finished")
	ErrCADRenderRejected = errors.New("cad: the rendered image failed the content check")

	ErrCADRenderTimeout       = errors.New("cad: the render stage did not finish within the time limit")
	ErrCADRenderFailed        = errors.New("cad: the render stage failed")
	ErrCADMeasurementsInvalid = errors.New("cad: the measurements could not be produced")

	ErrCADDXFTimeout      = errors.New("cad: the dxf stage did not finish within the time limit")
	ErrCADDWGTimeout      = errors.New("cad: the dwg took too long to read; simplify the drawing or export fewer details")
	ErrCADRsvgTimeout     = errors.New("cad: the rsvg stage did not finish within the time limit")
	ErrCADDrawingFailed   = errors.New("cad: the drawing stage failed")
	ErrCADNothingDrawable = errors.New("cad: the drawing has nothing drawable")

	ErrCADMeasure     = errors.New("cad: the measurements draft cannot become a measurements document")
	ErrCADPDFRejected = errors.New("cad: the drawing's PDF failed the content check")
	ErrCADSVGRejected = errors.New("cad: the drawing's SVG failed the safety check")
)

func CADMeasurementsPath(outPath string) string {
	return outPath + ".measurements.json"
}

func cadMaxBytes(format string) int64 {
	switch format {
	case "iges":
		return CADMaxIGESBytes
	case "stl":
		return CADMaxSTLBytes
	case "dxf":
		return CADMaxDXFBytes
	}
	return CADMaxSTEPBytes
}

type CADConverter struct{}

func (CADConverter) Pairs() []Pair {
	return []Pair{{From: "step", To: "stl"}, {From: "iges", To: "stl"}}
}

func (CADConverter) Engine() string { return EngineCAD }

func (CADConverter) Convert(context.Context, string, string, map[string]any) error {
	return errors.New("cad: unsupported conversion")
}

type CADRenderConverter struct{ CADConverter }

func (CADRenderConverter) Operation() string { return OperationRender }

func (CADRenderConverter) Pairs() []Pair {
	return []Pair{{From: "step", To: "png"}, {From: "iges", To: "png"}, {From: "stl", To: "png"}}
}

type CADDXFConverter struct{ CADConverter }

func (CADDXFConverter) Pairs() []Pair {
	return []Pair{
		{From: "dxf", To: "svg"}, {From: "dxf", To: "pdf"},
		{From: "dwg", To: "svg"}, {From: "dwg", To: "pdf"}, {From: "dwg", To: "png"},
	}
}

type CADDXFRenderConverter struct{ CADConverter }

func (CADDXFRenderConverter) Operation() string { return OperationRender }

func (CADDXFRenderConverter) Pairs() []Pair {
	return []Pair{{From: "dxf", To: "png"}, {From: "dwg", To: "png"}}
}
