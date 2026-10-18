//go:build !windows

package jsonl

import (
	"errors"
	"os"
	"path/filepath"
)

func syncDirectory(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(file.Sync(), file.Close())
}

func publishFile(from, to string) error {
	if err := os.Rename(from, to); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(to))
}
