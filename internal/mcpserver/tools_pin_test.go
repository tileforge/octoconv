package mcpserver

import (
	"os"
	"reflect"
	"regexp"
	"sort"
	"testing"
)

func TestMCPRegistersTheFiveTools(t *testing.T) {
	raw, err := os.ReadFile("mcpserver.go")
	if err != nil {
		t.Fatal("mcpserver.go")
	}
	matches := regexp.MustCompile(`(?m)Name:\s+"([^"]+)"`).FindAllStringSubmatch(string(raw), -1)
	got := make([]string, 0, len(matches))
	for _, match := range matches {
		got = append(got, match[1])
	}
	sort.Strings(got)
	want := []string{
		"convert_file",
		"download_result",
		"get_job_status",
		"list_presets",
		"list_supported_formats",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Tool.Name literals = %v", got)
	}
}
