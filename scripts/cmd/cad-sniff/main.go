package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/apaderin/octoconv/internal/convert"
)

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func emit(group, root, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	size := st.Size()
	head, tail, err := convert.ReadCADWindows(f, size)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	declared := convert.NormalizeFormat(strings.TrimPrefix(filepath.Ext(path), "."))
	res := convert.SniffCADWindows(head, tail, size, declared)
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash %s: %w", path, err)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	name := group + "/" + filepath.ToSlash(rel)
	fmt.Printf("%s\t%s\t%s\t%d\t%s\t%s\n", name, group, hex.EncodeToString(h.Sum(nil)), size,
		dash(res.Format), dash(string(res.Rejection)))
	return nil
}

func main() {
	group := flag.String("group", "", "corpus group name")
	root := flag.String("root", ".", "directory the names are relative to")
	flag.Parse()
	if *group == "" {
		fmt.Fprintln(os.Stderr, "usage: cad-sniff -group G -root DIR FILE...")
		os.Exit(64)
	}
	failed := false
	for _, p := range flag.Args() {
		if err := emit(*group, *root, p); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}
