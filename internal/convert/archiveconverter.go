package convert

import (
	"context"
	"errors"
)

type ArchiveConverter struct{}

func (ArchiveConverter) Engine() string { return EngineArchive }

func (ArchiveConverter) Operation() string { return OperationArchive }

func (ArchiveConverter) Pairs() []Pair {
	return []Pair{{From: "files", To: "zip"}}
}

func (ArchiveConverter) Convert(context.Context, string, string, map[string]any) error {
	return errors.New("archive: packing is not an in-process converter")
}
