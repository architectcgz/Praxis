// Package jsonl 提供不解释业务载荷的逐行日志文件读写。
package jsonl

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func CheckRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("log must be a regular file")
	}
	return nil
}

// Create 建立空日志，不覆盖已有文件；成功返回不保证断电持久化。
func Create(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create log %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("create log %s: %w", path, err)
	}
	return nil
}

// Append 不补建文件、不回滚失败写入、不逐条同步；调用方负责串行追加及失败后的处理。
func Append(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	n, writeErr := file.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	return errors.Join(writeErr, file.Close())
}

// Read 向 apply 传递含换行符的完整记录；未换行的尾部备份后截断，调用方须排除并发读写。
func Read(ctx context.Context, path, backupDir string, apply func([]byte) error) (err error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()
	reader := bufio.NewReader(file)
	var offset int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, readErr := reader.ReadBytes('\n')
		if errors.Is(readErr, io.EOF) {
			if len(line) > 0 {
				if err := truncateTail(file, backupDir, offset, line); err != nil {
					return fmt.Errorf("%s offset %d: %w", path, offset, err)
				}
			}
			return nil
		}
		if readErr != nil {
			return readErr
		}
		if err := apply(line); err != nil {
			return fmt.Errorf("%s offset %d: %w", path, offset, err)
		}
		offset += int64(len(line))
	}
}

// truncateTail 先同步尾行备份及其目录，再截断原日志，避免修复时丢失待检查的原始内容。
func truncateTail(file *os.File, backupDir string, offset int64, tail []byte) error {
	backup, err := os.CreateTemp(backupDir, "incomplete-tail-*")
	if err != nil {
		return err
	}
	_, writeErr := backup.Write(tail)
	err = errors.Join(writeErr, backup.Sync(), backup.Close())
	if err != nil {
		return err
	}
	if err := syncDirectory(backupDir); err != nil {
		return err
	}
	if err := file.Truncate(offset); err != nil {
		return err
	}
	return file.Sync()
}
