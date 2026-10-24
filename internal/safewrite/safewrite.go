// Copyright 2026, Jamf Software LLC

// Package safewrite writes a file by renaming a fully written temporary file
// over its destination. It imports nothing from this module, so any writer can
// share it.
package safewrite

import (
	"fmt"
	"os"
	"path/filepath"
)

// File writes data to path with mode perm, through a temporary file in the same
// directory renamed into place. The rename replaces the directory entry, so a
// symlink or hard link already at path is replaced rather than written through,
// and a pre-existing file's mode does not survive. A reader never sees a
// partial file.
func File(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".jamf-cli-tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("setting file mode: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("moving file into place: %w", err)
	}
	return nil
}
