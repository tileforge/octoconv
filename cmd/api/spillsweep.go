package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const multipartSpillPrefix = "multipart-"

func isMultipartSpillName(name string) bool {
	rest, ok := strings.CutPrefix(name, multipartSpillPrefix)
	if !ok || rest == "" {
		return false
	}
	for i := 0; i < len(rest); i++ {
		if rest[i] < '0' || rest[i] > '9' {
			return false
		}
	}
	return true
}

func sweepMultipartSpill(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("list spill dir: %w", err)
	}
	removed := 0
	var errs []error
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !isMultipartSpillName(entry.Name()) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			errs = append(errs, err)
			continue
		}
		removed++
	}
	return removed, errors.Join(errs...)
}
