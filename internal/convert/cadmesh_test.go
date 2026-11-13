package convert

import (
	"context"
	"errors"
	"testing"
)

func TestCADIncompleteVerdictIsAnUnreadableFile(t *testing.T) {
	f := newCADFixture(t, "step", cadOKGuard(), cadSay("mesh", "incomplete", "mesh_status", 2))
	err := (CADConverter{}).Convert(context.Background(), f.in, f.out, nil)
	if !errors.Is(err, ErrCADUnreadable) || errors.Is(err, ErrCADEmptyResult) {
		t.Fatalf("err = %v, want ErrCADUnreadable alone", err)
	}
}
