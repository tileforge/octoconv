package convert

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	provenanceFileLimit  = 2 << 20
	provenanceTotalLimit = 15 << 20
)

var (
	provenanceHeading = regexp.MustCompile("^## `([^`]+)` — ([0-9]+) bytes$")
	provenanceHex     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	provenanceLicence = regexp.MustCompile(`^(PD|CC0-1\.0|CC-BY-3\.0|CC-BY-4\.0|MIT|BSD-2-Clause|BSD-3-Clause|generated)( |$)`)
)

type provenanceSection struct {
	line     int
	size     int64
	sha256   []string
	licences []string
}

func parseProvenance(doc string) (map[string][]provenanceSection, []string) {
	sections := map[string][]provenanceSection{}
	var problems []string
	var current *provenanceSection
	var currentRel string
	flush := func() {
		if current != nil {
			sections[currentRel] = append(sections[currentRel], *current)
		}
		current = nil
	}
	for i, line := range strings.Split(doc, "\n") {
		n := i + 1
		if strings.HasPrefix(line, "- Licence: ") && !provenanceLicence.MatchString(strings.TrimPrefix(line, "- Licence: ")) {
			problems = append(problems, fmt.Sprintf("PROVENANCE.md:%d: licence line outside the D-04 allow-list: %q", n, line))
		}
		if strings.HasPrefix(line, "## ") {
			flush()
			m := provenanceHeading.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			size, err := strconv.ParseInt(m[2], 10, 64)
			if err != nil {
				problems = append(problems, fmt.Sprintf("PROVENANCE.md:%d: byte count %q: %v", n, m[2], err))
				continue
			}
			currentRel = m[1]
			current = &provenanceSection{line: n, size: size}
			continue
		}
		if current == nil {
			continue
		}
		if v, ok := strings.CutPrefix(line, "- sha256: "); ok {
			current.sha256 = append(current.sha256, v)
		}
		if v, ok := strings.CutPrefix(line, "- Licence: "); ok {
			current.licences = append(current.licences, v)
		}
	}
	flush()
	return sections, problems
}

func TestCADProvenanceCoversEveryCommittedFile(t *testing.T) {
	root := filepath.Join("testdata", "cad")
	doc, err := os.ReadFile(filepath.Join(root, "PROVENANCE.md"))
	if err != nil {
		t.Fatal(err)
	}
	sections, problems := parseProvenance(string(doc))

	files := map[string]bool{}
	var total int64
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		total += info.Size()
		if info.Size() > provenanceFileLimit {
			problems = append(problems, fmt.Sprintf("%s: %d bytes is above the 2 MiB commit threshold", rel, info.Size()))
		}
		if rel == "PROVENANCE.md" {
			return nil
		}
		files[rel] = true
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		problems = append(problems, checkProvenanceFile(rel, int64(len(b)), hex.EncodeToString(sum[:]), sections[rel])...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("testdata/cad walk found no file")
	}
	if total > provenanceTotalLimit {
		problems = append(problems, fmt.Sprintf("testdata/cad: %d bytes in total is above the 15 MiB commit threshold", total))
	}
	for rel, list := range sections {
		if !files[rel] {
			problems = append(problems, fmt.Sprintf("%s: section at PROVENANCE.md:%d names a file that is not committed", rel, list[0].line))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		t.Errorf("%d provenance problems:\n%s", len(problems), strings.Join(problems, "\n"))
	}
}

func checkProvenanceFile(rel string, size int64, sum string, list []provenanceSection) []string {
	switch len(list) {
	case 0:
		return []string{rel + ": no per-file section"}
	case 1:
	default:
		lines := make([]string, len(list))
		for i, s := range list {
			lines[i] = strconv.Itoa(s.line)
		}
		return []string{fmt.Sprintf("%s: %d per-file sections (PROVENANCE.md lines %s)", rel, len(list), strings.Join(lines, ", "))}
	}
	s := list[0]
	var problems []string
	where := fmt.Sprintf("%s (PROVENANCE.md:%d)", rel, s.line)
	if s.size != size {
		problems = append(problems, fmt.Sprintf("%s: heading says %d bytes, the file has %d", where, s.size, size))
	}
	switch {
	case len(s.sha256) != 1:
		problems = append(problems, fmt.Sprintf("%s: %d sha256 lines, want exactly one", where, len(s.sha256)))
	case !provenanceHex.MatchString(s.sha256[0]):
		problems = append(problems, fmt.Sprintf("%s: sha256 %q is not 64 lowercase hex digits", where, s.sha256[0]))
	case s.sha256[0] != sum:
		problems = append(problems, fmt.Sprintf("%s: sha256 %s, the file has %s", where, s.sha256[0], sum))
	}
	switch {
	case len(s.licences) != 1:
		problems = append(problems, fmt.Sprintf("%s: %d licence lines, want exactly one", where, len(s.licences)))
	case !provenanceLicence.MatchString(s.licences[0]):
		problems = append(problems, fmt.Sprintf("%s: licence %q is outside the D-04 allow-list", where, s.licences[0]))
	}
	return problems
}
