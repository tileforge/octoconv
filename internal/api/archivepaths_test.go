package api

import (
	"errors"
	"mime"
	"mime/multipart"
	"net/textproto"
	"path"
	"testing"
)

func archivePart(filename string) *multipart.FileHeader {
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{
		"name":     "file",
		"filename": filename,
	}))
	return &multipart.FileHeader{Filename: path.Base(filename), Header: header}
}

func TestValidateArchiveMembers(t *testing.T) {
	t.Parallel()

	members, err := validateArchiveMembers(
		[]*multipart.FileHeader{archivePart("other/a.txt"), archivePart("b.txt")},
		[]string{"docs/a.txt", ""},
		true,
	)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(members) != 2 || members[0].Name != "docs/a.txt" || members[0].Base != "a.txt" || members[0].Format != "txt" {
		t.Fatalf("first member %+v", members)
	}
	if members[1].Name != "b.txt" || members[1].Base != "b.txt" || members[1].Format != "txt" {
		t.Fatalf("second member %+v", members[1])
	}

	rooted, err := validateArchiveMembers(
		[]*multipart.FileHeader{archivePart("nested/a.txt"), archivePart("README")},
		nil,
		false,
	)
	if err != nil {
		t.Fatalf("zero paths: %v", err)
	}
	if rooted[0].Name != "a.txt" || rooted[1].Name != "README" || rooted[1].Format != "bin" {
		t.Fatalf("root members %+v", rooted)
	}

	one, err := validateArchiveMembers([]*multipart.FileHeader{archivePart("notes.txt")}, nil, false)
	if err != nil || one[0].Name != "notes.txt" {
		t.Fatalf("one file zero paths: %+v %v", one, err)
	}

	dotted, err := validateArchiveMembers(
		[]*multipart.FileHeader{archivePart("a.txt")},
		[]string{".github/workflows/a.txt"},
		true,
	)
	if err != nil || dotted[0].Name != ".github/workflows/a.txt" || dotted[0].Base != "a.txt" {
		t.Fatalf("leading dot segment: %+v %v", dotted, err)
	}

	distinct, err := validateArchiveMembers(
		[]*multipart.FileHeader{archivePart("A.txt"), archivePart("a.txt")},
		nil,
		false,
	)
	if err != nil || distinct[0].Name != "A.txt" || distinct[1].Name != "a.txt" {
		t.Fatalf("case-distinct names: %+v %v", distinct, err)
	}
}

func TestValidateArchiveMembersRefuses(t *testing.T) {
	t.Parallel()
	files := []*multipart.FileHeader{archivePart("a.txt"), archivePart("b.txt")}
	cases := []struct {
		name    string
		files   []*multipart.FileHeader
		paths   []string
		present bool
	}{
		{name: "last segment mismatch", files: files, paths: []string{"docs/b.txt", "b.txt"}, present: true},
		{name: "short path list", files: files, paths: []string{"a.txt"}, present: true},
		{name: "long path list", files: files, paths: []string{"a.txt", "b.txt", "c.txt"}, present: true},
		{name: "empty present list", files: files, paths: []string{}, present: true},
		{
			name:    "duplicate member",
			files:   []*multipart.FileHeader{archivePart("a.txt"), archivePart("a.txt")},
			paths:   []string{"docs/a.txt", "docs/a.txt"},
			present: true,
		},
		{
			name:    "duplicate root",
			files:   []*multipart.FileHeader{archivePart("a.txt"), archivePart("nested/a.txt")},
			present: false,
		},
		{name: "dotdot", files: files, paths: []string{"docs/../a.txt", "b.txt"}, present: true},
		{name: "dot segment", files: files, paths: []string{"docs/./a.txt", "b.txt"}, present: true},
		{name: "empty segment", files: files, paths: []string{"docs//a.txt", "b.txt"}, present: true},
		{name: "absolute", files: files, paths: []string{"/docs/a.txt", "b.txt"}, present: true},
		{name: "trailing slash", files: files, paths: []string{"docs/a.txt/", "b.txt"}, present: true},
		{name: "backslash", files: files, paths: []string{`docs\a.txt`, "b.txt"}, present: true},
		{name: "drive prefix", files: files, paths: []string{"C:/a.txt", "b.txt"}, present: true},
		{
			name:    "drive relative",
			files:   []*multipart.FileHeader{archivePart("a.txt")},
			paths:   []string{"C:a.txt"},
			present: true,
		},
		{
			name:    "nul",
			files:   []*multipart.FileHeader{archivePart("a.txt")},
			paths:   []string{"docs/a.txt\x00"},
			present: true,
		},
		{
			name:    "control",
			files:   []*multipart.FileHeader{archivePart("a.txt")},
			paths:   []string{"docs/\na.txt"},
			present: true,
		},
		{name: "reserved ordinal zero", files: []*multipart.FileHeader{archivePart("in.txt")}, present: false},
		{
			name:    "reserved later file",
			files:   []*multipart.FileHeader{archivePart("a.txt"), archivePart("out.zip")},
			present: false,
		},
		{name: "leading dash", files: []*multipart.FileHeader{archivePart("-a.txt")}, present: false},
		{name: "leading dot base", files: []*multipart.FileHeader{archivePart(".gitignore")}, present: false},
		{name: "drive base", files: []*multipart.FileHeader{archivePart("C:a.txt")}, present: false},
		{
			name:    "empty path drive base",
			files:   []*multipart.FileHeader{archivePart("C:a.txt")},
			paths:   []string{""},
			present: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := validateArchiveMembers(tc.files, tc.paths, tc.present)
			if !errors.Is(err, errInvalidArchiveMember) {
				t.Fatalf("err = %v, want invalid archive member; members=%+v", err, got)
			}
		})
	}
}
