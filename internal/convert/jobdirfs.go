package convert

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"syscall"
)

var ErrJobFileIrregular = errors.New("not a regular file")

func writeNewFile(path string, body []byte, perm fs.FileMode) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	if _, err := f.Write(body); err != nil {
		f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}

func OpenJobFile(path string) (*os.File, fs.FileInfo, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, syscall.ELOOP) {
		return nil, nil, fmt.Errorf("%w: %s is a symbolic link", ErrJobFileIrregular, path)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("open %s: %w", path, err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, nil, fmt.Errorf("%w: %s is %v", ErrJobFileIrregular, path, info.Mode().Type())
	}
	return f, info, nil
}

func ReadJobFile(path string) ([]byte, error) {
	f, _, err := OpenJobFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	body, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return body, nil
}
