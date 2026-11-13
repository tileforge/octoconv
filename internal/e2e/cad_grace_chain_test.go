package e2e

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tileforge/octoconv/internal/convert"
)

func cadReadFile(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.Split(string(raw), "\n")
}

func cadBlock(t *testing.T, lines []string, path string, keys ...string) []string {
	t.Helper()
	start, indent := 0, 0
	for _, key := range keys {
		head := strings.Repeat(" ", indent) + key + ":"
		found := false
		for i := start; i < len(lines); i++ {
			if strings.TrimRight(lines[i], " ") == head {
				start, found = i+1, true
				break
			}
		}
		if !found {
			t.Fatalf("%s has no %q block", path, strings.Join(keys, "."))
		}
		indent += 2
	}
	end := start
	for end < len(lines) {
		l := lines[end]
		if strings.TrimSpace(l) != "" && len(l)-len(strings.TrimLeft(l, " ")) < indent {
			break
		}
		end++
	}
	return lines[start:end]
}

func cadIntField(t *testing.T, block []string, path string, re *regexp.Regexp) int {
	t.Helper()
	for _, l := range block {
		if m := re.FindStringSubmatch(l); m != nil {
			n, err := strconv.Atoi(m[1])
			if err != nil {
				t.Fatalf("%s: %q: %v", path, l, err)
			}
			return n
		}
	}
	t.Fatalf("%s: no line matches %s", path, re)
	return 0
}

func TestCADGraceChainFollowsTheEngineTimeout(t *testing.T) {
	const (
		composeFile = "../../docker-compose.yml"
		valuesFile  = "../../deploy/chart/octoconv/values.yaml"
		configMap   = "../../deploy/chart/octoconv/templates/configmap.yaml"
	)
	engine := int(convert.CADDefaultEngineTimeout / time.Second)
	if time.Duration(engine)*time.Second != convert.CADDefaultEngineTimeout {
		t.Fatalf("CADDefaultEngineTimeout %s is not whole seconds", convert.CADDefaultEngineTimeout)
	}

	cm := parseConfigMapTimeouts(t)
	if got, ok := cm["CAD_ENGINE_TIMEOUT"]; !ok || got != engine {
		t.Errorf("ConfigMap CAD_ENGINE_TIMEOUT = %d s (present %v), want the Go default %d s", got, ok, engine)
	}

	compose := cadReadFile(t, composeFile)

	values := cadReadFile(t, valuesFile)
	kedaCAD := cadBlock(t, values, valuesFile, "keda", "cad")
	if got := cadIntField(t, kedaCAD, valuesFile, regexp.MustCompile(`^    scaleDownStabilizationSeconds:\s*(\d+)\s*$`)); got < engine {
		t.Errorf("values.yaml keda.cad.scaleDownStabilizationSeconds = %d, want at least the engine timeout %d", got, engine)
	}

	engineRe := regexp.MustCompile(`^\s+CAD_ENGINE_TIMEOUT:\s*"([^"]*)"\s*$`)
	n := 0
	for i, l := range compose {
		m := engineRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		n++
		if want := strconv.Itoa(cm["CAD_ENGINE_TIMEOUT"]) + "s"; m[1] != want {
			t.Errorf("docker-compose.yml:%d CAD_ENGINE_TIMEOUT = %q, want the ConfigMap value %q", i+1, m[1], want)
		}
	}
	if n < 2 {
		t.Fatalf("found %d CAD_ENGINE_TIMEOUT lines in docker-compose.yml; the parser is wrong", n)
	}

	stages := map[string]time.Duration{
		"CAD_GUARD_TIMEOUT":  convert.CADDefaultGuardTimeout,
		"CAD_MESH_TIMEOUT":   convert.CADDefaultMeshTimeout,
		"CAD_RENDER_TIMEOUT": convert.CADDefaultRenderTimeout,
		"CAD_DXF_TIMEOUT":    convert.CADDefaultDXFTimeout,
		"CAD_RSVG_TIMEOUT":   convert.CADDefaultRsvgTimeout,
	}
	for _, src := range []struct {
		path  string
		lines []string
	}{
		{configMap, cadReadFile(t, configMap)},
	} {
		for name, d := range stages {
			want := strconv.Itoa(int(d/time.Second)) + "s"
			re := regexp.MustCompile(`^\s+` + name + `:\s*"([^"]*)"\s*$`)
			var got []string
			for _, l := range src.lines {
				if m := re.FindStringSubmatch(l); m != nil {
					got = append(got, m[1])
				}
			}
			if len(got) != 1 || got[0] != want {
				t.Errorf("%s %s = %v, want exactly one %q (the Go default)", src.path, name, got, want)
			}
		}
	}
}
