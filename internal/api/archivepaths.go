package api

import (
	"errors"
	"mime/multipart"
	"path"
	"strings"
	"unicode"

	"github.com/tileforge/octoconv/internal/convert"
	"github.com/tileforge/octoconv/internal/jobs"
)

var errInvalidArchiveMember = errors.New("invalid archive member")

type archiveMember struct {
	Name   string
	Base   string
	Format string
}

func validateArchiveMembers(files []*multipart.FileHeader, paths []string, pathsPresent bool) ([]archiveMember, error) {
	if len(files) == 0 {
		return nil, errInvalidArchiveMember
	}
	if pathsPresent && len(paths) != len(files) {
		return nil, errInvalidArchiveMember
	}
	members := make([]archiveMember, 0, len(files))
	seen := make(map[string]struct{}, len(files))
	for i, file := range files {
		base, err := archiveBaseName(file)
		if err != nil {
			return nil, err
		}
		name := base
		if pathsPresent {
			name, err = archiveMemberName(paths[i], base)
			if err != nil {
				return nil, err
			}
		}
		if _, dup := seen[name]; dup {
			return nil, errInvalidArchiveMember
		}
		seen[name] = struct{}{}
		members = append(members, archiveMember{
			Name:   name,
			Base:   base,
			Format: archiveInputFormat(base),
		})
	}
	return members, nil
}

func archiveBaseName(file *multipart.FileHeader) (string, error) {
	base, err := jobs.SafeInputFilename(path.Base(rawPartFilename(file)))
	if err != nil {
		return "", errInvalidArchiveMember
	}
	if jobs.WorkerOwnedName(base) {
		return "", errInvalidArchiveMember
	}
	return base, nil
}

func archiveMemberName(raw, base string) (string, error) {
	if raw == "" {
		return base, nil
	}
	if strings.ContainsRune(raw, '\\') || strings.ContainsRune(raw, 0) {
		return "", errInvalidArchiveMember
	}
	if strings.HasPrefix(raw, "/") || strings.HasSuffix(raw, "/") || hasDrivePrefix(raw) {
		return "", errInvalidArchiveMember
	}
	for _, r := range raw {
		if unicode.IsControl(r) {
			return "", errInvalidArchiveMember
		}
	}
	segments := strings.Split(raw, "/")
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." || hasDrivePrefix(segment) {
			return "", errInvalidArchiveMember
		}
	}
	if segments[len(segments)-1] != base {
		return "", errInvalidArchiveMember
	}
	return raw, nil
}

func archiveInputFormat(base string) string {
	format := convert.NormalizeFormat(strings.TrimPrefix(path.Ext(base), "."))
	if format == "" {
		return "bin"
	}
	return format
}

func hasDrivePrefix(value string) bool {
	return len(value) >= 2 && value[1] == ':' && isASCIILetter(value[0])
}

func isASCIILetter(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}
