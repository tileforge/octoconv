package convert

import "strconv"

// InputByteCeiling is the default input ceiling for an engine and format.
// perFormat is true when the ceiling is specific to format. gated is false when that input has no ceiling.
func InputByteCeiling(engine, format string) (limit int64, perFormat, gated bool) {
	format = NormalizeFormat(format)
	if engine == EngineCAD && format == "dwg" {
		return 0, false, false
	}
	if formats, ok := inputFormatByteCeilings[engine]; ok {
		if n, ok := formats[format]; ok {
			return n, true, true
		}
	}
	n, ok := inputEngineByteCeilings[engine]
	return n, false, ok
}

// ByteCeilingMessage is the client text for an input over InputByteCeiling.
func ByteCeilingMessage(format string, limit int64, perFormat bool) string {
	if !perFormat {
		return "file exceeds size limit for this conversion type"
	}
	if limit%(1<<20) == 0 {
		return format + " file exceeds the " + strconv.FormatInt(limit>>20, 10) + " MiB limit for this conversion"
	}
	return format + " file exceeds the " + strconv.FormatInt(limit, 10) + " bytes limit for this conversion"
}

var inputEngineByteCeilings = map[string]int64{
	EngineImage:    100 << 20,
	EngineDocument: 100 << 20,
	EngineHTML:     100 << 20,
	EngineAudio:    100 << 20,
	EngineAV:       2 << 30,
	EngineOCR:      100 << 20,
	EnginePandoc:   100 << 20,
	EngineProbe:    50 << 20,
	EngineTectonic: 2 << 20,
	EngineCAD:      CADMaxIGESBytes,
	EngineArchive:  100 << 20,
}

var inputFormatByteCeilings = map[string]map[string]int64{
	EngineCAD: {
		"step": CADMaxSTEPBytes,
		"iges": CADMaxIGESBytes,
		"stl":  CADMaxSTLBytes,
		"dxf":  CADMaxDXFBytes,
	},
}
