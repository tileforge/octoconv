package convert_test

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/apaderin/octoconv/internal/convert"
)

const (
	errorCodesPath   = "../../docs/ERROR-CODES.md"
	measurementsPath = "../../docs/MEASUREMENTS.md"
)

func readDoc(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func docRow(t *testing.T, doc, path, prefix string) string {
	t.Helper()
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	t.Fatalf("%s has no line starting with %q", path, prefix)
	return ""
}

func TestREADMECADRowNamesEveryCADPair(t *testing.T) {
	row := docRow(t, readDoc(t, readmePath), readmePath, "| `cad` |")
	cells := strings.Split(row, "|")
	if len(cells) < 4 {
		t.Fatalf("the cad row has %d cells: %q", len(cells), row)
	}
	converts := cells[2]
	classes := convert.Default.OperationClasses()
	formats := map[string]bool{}
	for _, op := range []string{convert.OperationConvert, convert.OperationRender} {
		pairs := classes[op][convert.EngineCAD]
		if len(pairs) == 0 {
			t.Fatalf("the registry has no %s pair for the cad engine", op)
		}
		if op != convert.OperationConvert && !strings.Contains(converts, "`"+op+"`") {
			t.Errorf("the README cad row does not name the %s operation: %q", op, converts)
		}
		for _, p := range pairs {
			formats[p.From], formats[p.To] = true, true
			for _, f := range []string{p.From, p.To} {
				if !strings.Contains(converts, "`"+f+"`") {
					t.Errorf("the README cad row does not name %s of the %s pair %s -> %s: %q", f, op, p.From, p.To, converts)
				}
			}
		}
	}
	for _, m := range regexp.MustCompile("`([a-z]+)`").FindAllStringSubmatch(converts, -1) {
		tok := m[1]
		if tok == convert.OperationRender {
			continue
		}
		if !formats[convert.NormalizeFormat(tok)] {
			t.Errorf("the README cad row names %s, which no cad pair uses", tok)
		}
	}
}

func TestErrorCodesDocStatesTheCADCeilingsOfTheCode(t *testing.T) {
	doc := readDoc(t, errorCodesPath)
	payload := docRow(t, doc, errorCodesPath, "| `payload_too_large` |")
	input := docRow(t, doc, errorCodesPath, "| `input_too_large` |")
	paragraph := docRow(t, doc, errorCodesPath, "CAD content checks run only")
	for _, c := range []struct {
		name  string
		bytes int64
	}{
		{"STEP", convert.CADMaxSTEPBytes},
		{"IGES", convert.CADMaxIGESBytes},
		{"STL", convert.CADMaxSTLBytes},
		{"DXF", convert.CADMaxDXFBytes},
	} {
		if c.bytes%(1<<20) != 0 {
			t.Fatalf("the %s ceiling %d is not whole MiB; the documents write it in MiB", c.name, c.bytes)
		}
		mib := c.bytes >> 20
		for _, w := range []struct{ where, text, want string }{
			{"payload_too_large row", payload, fmt.Sprintf("%d MiB %s", mib, c.name)},
			{"input_too_large row", input, fmt.Sprintf("%d MiB for %s", mib, c.name)},
			{"CAD paragraph", paragraph, fmt.Sprintf("%d MiB %s", mib, c.name)},
		} {
			if !strings.Contains(w.text, w.want) {
				t.Errorf("docs/ERROR-CODES.md %s does not state %q", w.where, w.want)
			}
		}
	}
	if want := fmt.Sprintf("%d bytes for a DXF", int64(convert.CADMaxDXFBytes)); !strings.Contains(paragraph, want) {
		t.Errorf("docs/ERROR-CODES.md does not state the DXF inspect max_bytes %q", want)
	}
	dxf := docRow(t, doc, errorCodesPath, "A DXF job fails with")
	for _, want := range []string{
		fmt.Sprintf("more than %d INSERT/MINSERT instances", convert.CADMaxDXFExpandedInserts),
		fmt.Sprintf("or %d entities", convert.CADMaxDXFExpandedEntities),
		fmt.Sprintf("deeper than %d levels", convert.CADMaxDXFNesting),
		fmt.Sprintf("an SVG above %d MiB", int64(convert.CADMaxDXFSVGBytes)>>20),
		fmt.Sprintf("`CAD_DXF_TIMEOUT`, %d s by default", int(convert.CADDefaultDXFTimeout.Seconds())),
		fmt.Sprintf("`CAD_RSVG_TIMEOUT`, %d s by default", int(convert.CADDefaultRsvgTimeout.Seconds())),
	} {
		if !strings.Contains(dxf, want) {
			t.Errorf("docs/ERROR-CODES.md's DXF paragraph does not state %q", want)
		}
	}
}

func TestMeasurementsDocStatesThePDFTextFloorOfTheCode(t *testing.T) {
	want := strconv.FormatFloat(convert.CADDXFPDFTextFloorPt, 'g', -1, 64) + " pt"
	if !strings.Contains(readDoc(t, measurementsPath), want) {
		t.Errorf("docs/MEASUREMENTS.md does not state the PDF text floor %q", want)
	}
}

func TestCADDocsStateThatANothingVisibleDrawingIsEmptyOnEveryRoute(t *testing.T) {
	errorCodes := readDoc(t, errorCodesPath)
	measurementsDoc := readDoc(t, measurementsPath)
	dxf := docRow(t, errorCodes, errorCodesPath, "A DXF job fails with")
	row := docRow(t, errorCodes, errorCodesPath, "| `cad_empty_result` |")
	var transparency string
	for _, p := range strings.Split(measurementsDoc, "\n\n") {
		if strings.HasPrefix(p, "**Transparency.**") {
			transparency = strings.Join(strings.Fields(p), " ")
		}
	}
	if transparency == "" {
		t.Fatalf("%s has no Transparency paragraph", measurementsPath)
	}
	for _, c := range []struct{ where, text string }{
		{"ERROR-CODES DXF paragraph", dxf},
		{"ERROR-CODES cad_empty_result row", row},
		{"MEASUREMENTS Transparency paragraph", transparency},
	} {
		if !strings.Contains(c.text, "fully transparent") {
			t.Errorf("%s does not name a fully transparent drawing", c.where)
		}
		for _, stale := range []string{"SVG still converts", "SVG converts", "its SVG converts"} {
			if strings.Contains(c.text, stale) {
				t.Errorf("%s still says %q", c.where, stale)
			}
		}
	}
	for _, c := range []struct{ where, text string }{
		{"ERROR-CODES DXF paragraph", dxf},
		{"MEASUREMENTS Transparency paragraph", transparency},
	} {
		if !strings.Contains(c.text, "SVG, PDF and PNG") || !strings.Contains(c.text, "`cad_empty_result`") {
			t.Errorf("%s does not state that SVG, PDF and PNG of a drawing with nothing visible are `cad_empty_result`", c.where)
		}
	}
}
