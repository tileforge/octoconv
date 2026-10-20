package api

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

const (
	chartAPIEphemeralHeadroom int64 = 128 << 20
	apiSpillMountPath               = "/var/tmp/octoconv"
	chartAPIDeployment              = "../../deploy/chart/octoconv/templates/deployment-api.yaml"
)

var spillCapKeys = []string{"MAX_UPLOAD_BYTES", "INSPECT_MAX_UPLOAD_BYTES", "INSPECT_MAX_CONCURRENT"}

type chartAPISpill struct {
	API struct {
		Spill struct {
			MountPath *string `yaml:"mountPath"`
			SizeLimit *string `yaml:"sizeLimit"`
		} `yaml:"spill"`
		Resources struct {
			Requests struct {
				EphemeralStorage *string `yaml:"ephemeralStorage"`
			} `yaml:"requests"`
			Limits struct {
				EphemeralStorage *string `yaml:"ephemeralStorage"`
			} `yaml:"limits"`
		} `yaml:"resources"`
		ExtraEnv map[string]any `yaml:"extraEnv"`
	} `yaml:"api"`
}

func readChartAPISpill(t *testing.T, file string) chartAPISpill {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var doc chartAPISpill
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode %s: %v", file, err)
	}
	return doc
}

var integerBytes = regexp.MustCompile(`^[0-9]+$`)

func chartIntegerBytes(t *testing.T, where, key, raw string) int64 {
	t.Helper()
	if !integerBytes.MatchString(raw) {
		t.Fatalf("%s: %s = %q is not a whole number of bytes", where, key, raw)
	}
	n := transportCapInt(t, where, key, raw)
	if n <= 0 {
		t.Fatalf("%s: %s = %q is not a positive number of bytes", where, key, raw)
	}
	return n
}

func overrideString(base, over *string) string {
	if over != nil {
		return *over
	}
	if base != nil {
		return *base
	}
	return ""
}

func spillCaps(t *testing.T, where string, extraEnvs ...map[string]any) map[string]int64 {
	t.Helper()
	caps := map[string]int64{}
	for _, key := range spillCapKeys {
		caps[key] = chartLiteral(t, key)
	}
	for _, env := range extraEnvs {
		for _, key := range spillCapKeys {
			if v, ok := env[key]; ok {
				caps[key] = transportCapInt(t, where+" api.extraEnv", key, fmt.Sprint(v))
			}
		}
	}
	return caps
}

func spillSizeFor(caps map[string]int64) int64 {
	return int64(createIntakeMaxConcurrent)*caps["MAX_UPLOAD_BYTES"] +
		(int64(inspectIntakeMaxConcurrent)+caps["INSPECT_MAX_CONCURRENT"])*caps["INSPECT_MAX_UPLOAD_BYTES"]
}

func TestChartAPISpillVolumeMatchesTheIntakeBounds(t *testing.T) {
	tmpl, err := os.ReadFile(chartAPIDeployment)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(tmpl), "\n")
	trimmed := make([]string, len(lines))
	for i, l := range lines {
		trimmed[i] = strings.TrimSpace(l)
	}
	hasLine := func(want string) bool {
		for _, l := range trimmed {
			if l == want {
				return true
			}
		}
		return false
	}
	tmpdirWired := false
	for i, l := range trimmed {
		if l == "- name: TMPDIR" && i+1 < len(trimmed) && trimmed[i+1] == "value: {{ .Values.api.spill.mountPath | quote }}" {
			tmpdirWired = true
		}
	}
	if !tmpdirWired {
		t.Errorf("%s: no env entry TMPDIR with value {{ .Values.api.spill.mountPath | quote }}", chartAPIDeployment)
	}
	for _, want := range []string{
		"mountPath: {{ .Values.api.spill.mountPath | quote }}",
		"sizeLimit: {{ .Values.api.spill.sizeLimit | quote }}",
		"ephemeral-storage: {{ .Values.api.resources.requests.ephemeralStorage | quote }}",
		"ephemeral-storage: {{ .Values.api.resources.limits.ephemeralStorage | quote }}",
	} {
		if !hasLine(want) {
			t.Errorf("%s: no line %q", chartAPIDeployment, want)
		}
	}
	if strings.Contains(string(tmpl), "medium:") {
		t.Errorf("%s: the spill emptyDir must stay on disk, found medium:", chartAPIDeployment)
	}
	cm, err := os.ReadFile(transportCapChart)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cm), "TMPDIR") {
		t.Errorf("%s: TMPDIR in the shared ConfigMap reaches every pod through envFrom", transportCapChart)
	}

	base := readChartAPISpill(t, chartValues)
	overlays, err := filepath.Glob(transportCapChartDir + "values-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(overlays) == 0 {
		t.Fatalf("no values-*.yaml overlay under %s", transportCapChartDir)
	}
	sort.Strings(overlays)
	configs := append([]string{chartValues}, overlays...)

	for _, file := range configs {
		name := filepath.Base(file)
		t.Run(name, func(t *testing.T) {
			var over chartAPISpill
			caps := spillCaps(t, name, base.API.ExtraEnv)
			if file != chartValues {
				over = readChartAPISpill(t, file)
				caps = spillCaps(t, name, base.API.ExtraEnv, over.API.ExtraEnv)
			}
			mountPath := overrideString(base.API.Spill.MountPath, over.API.Spill.MountPath)
			sizeLimit := chartIntegerBytes(t, name, "api.spill.sizeLimit", overrideString(base.API.Spill.SizeLimit, over.API.Spill.SizeLimit))
			request := chartIntegerBytes(t, name, "api.resources.requests.ephemeralStorage", overrideString(base.API.Resources.Requests.EphemeralStorage, over.API.Resources.Requests.EphemeralStorage))
			limit := chartIntegerBytes(t, name, "api.resources.limits.ephemeralStorage", overrideString(base.API.Resources.Limits.EphemeralStorage, over.API.Resources.Limits.EphemeralStorage))
			want := spillSizeFor(caps)
			t.Logf("%s: MAX_UPLOAD_BYTES=%d INSPECT_MAX_UPLOAD_BYTES=%d INSPECT_MAX_CONCURRENT=%d want=%d sizeLimit=%d ephemeral-storage request=%d limit=%d",
				name, caps["MAX_UPLOAD_BYTES"], caps["INSPECT_MAX_UPLOAD_BYTES"], caps["INSPECT_MAX_CONCURRENT"], want, sizeLimit, request, limit)
			if mountPath != apiSpillMountPath {
				t.Errorf("%s: api.spill.mountPath = %q, want %q", name, mountPath, apiSpillMountPath)
			}
			if sizeLimit != want {
				t.Errorf("%s: api.spill.sizeLimit = %d, want %d × %d + (%d + %d) × %d = %d",
					name, sizeLimit, createIntakeMaxConcurrent, caps["MAX_UPLOAD_BYTES"], inspectIntakeMaxConcurrent, caps["INSPECT_MAX_CONCURRENT"], caps["INSPECT_MAX_UPLOAD_BYTES"], want)
			}
			if request != limit {
				t.Errorf("%s: ephemeral-storage request %d != limit %d", name, request, limit)
			}
			if request != sizeLimit+chartAPIEphemeralHeadroom {
				t.Errorf("%s: ephemeral-storage request = %d, want sizeLimit %d + %d = %d", name, request, sizeLimit, chartAPIEphemeralHeadroom, sizeLimit+chartAPIEphemeralHeadroom)
			}
		})
	}
}

const (
	composeAPISpillVolume = "api_spill"
	dockerfileAPI         = "../../Dockerfile.api"
	dockerfileSpillDir    = "RUN install -d -o nobody -g nogroup -m 0700 " + apiSpillMountPath
)

func TestComposeAPISpillIsOnANamedVolume(t *testing.T) {
	raw, err := os.ReadFile(transportCapCompose)
	if err != nil {
		t.Fatal(err)
	}
	var compose struct {
		Services map[string]map[string]yaml.Node `yaml:"services"`
		Volumes  map[string]yaml.Node            `yaml:"volumes"`
	}
	if err := yaml.Unmarshal(raw, &compose); err != nil {
		t.Fatalf("decode %s: %v", transportCapCompose, err)
	}
	api, ok := compose.Services["api"]
	if !ok {
		t.Fatalf("%s has no api service", transportCapCompose)
	}
	var env map[string]any
	envNode := api["environment"]
	if err := envNode.Decode(&env); err != nil {
		t.Fatalf("%s services.api.environment is not a map this test can read: %v", transportCapCompose, err)
	}
	chartMount := overrideString(readChartAPISpill(t, chartValues).API.Spill.MountPath, nil)
	tmpdir, ok := env["TMPDIR"]
	if !ok {
		t.Errorf("%s services.api.environment has no TMPDIR", transportCapCompose)
	} else if got := fmt.Sprint(tmpdir); got != apiSpillMountPath || got != chartMount {
		t.Errorf("%s services.api.environment.TMPDIR = %q, want %q (chart api.spill.mountPath %q)", transportCapCompose, got, apiSpillMountPath, chartMount)
	}
	wantVolume := composeAPISpillVolume + ":" + apiSpillMountPath
	volumesNode, ok := api["volumes"]
	if !ok {
		t.Errorf("%s services.api has no volumes, want %q", transportCapCompose, wantVolume)
	} else {
		var volumes []string
		if err := volumesNode.Decode(&volumes); err != nil {
			t.Fatalf("%s services.api.volumes is not a list of strings: %v", transportCapCompose, err)
		}
		if !slices.Contains(volumes, wantVolume) {
			t.Errorf("%s services.api.volumes = %q, want an entry %q", transportCapCompose, volumes, wantVolume)
		}
	}
	if _, ok := compose.Volumes[composeAPISpillVolume]; !ok {
		t.Errorf("%s top-level volumes has no %s", transportCapCompose, composeAPISpillVolume)
	}
	for _, key := range []string{"tmpfs", "storage_opt"} {
		if _, ok := api[key]; ok {
			t.Errorf("%s services.api has %s: D-08 keeps the compose spill on a named volume without a hard size", transportCapCompose, key)
		}
	}

	df, err := os.ReadFile(dockerfileAPI)
	if err != nil {
		t.Fatal(err)
	}
	lastFrom, install, user := -1, -1, -1
	for i, l := range strings.Split(string(df), "\n") {
		l = strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(l, "FROM "):
			lastFrom = i
		case l == dockerfileSpillDir:
			install = i
		case l == "USER nobody":
			user = i
		}
	}
	if install < 0 || user < 0 || !(lastFrom < install && install < user) {
		t.Errorf("%s: want %q in the runtime stage above USER nobody (last FROM line %d, install line %d, USER nobody line %d)",
			dockerfileAPI, dockerfileSpillDir, lastFrom+1, install+1, user+1)
	}

	caps := map[string]int64{}
	for _, key := range spillCapKeys {
		v, ok := env[key]
		if !ok {
			t.Fatalf("%s services.api.environment has no %s", transportCapCompose, key)
		}
		caps[key] = transportCapInt(t, transportCapCompose+" services.api.environment", key, fmt.Sprint(v))
	}
	t.Logf("docker-compose.yml api: spill on %s without a hard size (D-08): %d × %d + (%d + %d) × %d = %d bytes",
		composeAPISpillVolume, createIntakeMaxConcurrent, caps["MAX_UPLOAD_BYTES"], inspectIntakeMaxConcurrent, caps["INSPECT_MAX_CONCURRENT"], caps["INSPECT_MAX_UPLOAD_BYTES"], spillSizeFor(caps))
}
