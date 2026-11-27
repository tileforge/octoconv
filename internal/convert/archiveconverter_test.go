package convert

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveConverterRegistersFilesToZip(t *testing.T) {
	c := ArchiveConverter{}
	if c.Engine() != EngineArchive {
		t.Fatalf("Engine() = %q, want %q", c.Engine(), EngineArchive)
	}
	if c.Operation() != OperationArchive {
		t.Fatalf("Operation() = %q, want %q", c.Operation(), OperationArchive)
	}
	pairs := c.Pairs()
	if len(pairs) != 1 || pairs[0] != (Pair{From: "files", To: "zip"}) {
		t.Fatalf("Pairs() = %+v, want one files to zip pair", pairs)
	}

	got, ok := Default.LookupOp(OperationArchive, "files", "zip")
	if !ok {
		t.Fatal("LookupOp(archive, files, zip) = false, want the registered converter")
	}
	if got.Engine() != EngineArchive {
		t.Fatalf("registered engine = %q, want %q", got.Engine(), EngineArchive)
	}
	if _, ok := Default.Lookup("files", "zip"); ok {
		t.Fatal("Lookup(files, zip) under convert = true, want false")
	}
	if _, ok := Default.Classes()[EngineArchive]; ok {
		t.Fatal("Classes() includes archive; a non-convert engine must stay out of the engines object")
	}

	dir := t.TempDir()
	in := filepath.Join(dir, "in.bin")
	out := filepath.Join(dir, "out.zip")
	if err := os.WriteFile(in, []byte("bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.Convert(context.Background(), in, out, nil); err == nil {
		t.Fatal("Convert() error = nil, want an error")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("Convert wrote %s: %v", out, err)
	}
}
