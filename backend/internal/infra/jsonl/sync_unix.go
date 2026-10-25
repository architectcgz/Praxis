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
	// 临时文件与目标位于同一目录；硬链接原子发布且拒绝覆盖已有日志。
	if err := os.Link(from, to); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(to))
}
