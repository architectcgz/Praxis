// Package fs 实现本地项目目录和受限文本读取。
package fs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"praxis/internal/projectfile"
	"unicode/utf8"
)

// Filesystem 使用操作系统文件系统实现受限的项目文件操作。
type Filesystem struct{}

// Ensure 确保绝对路径是目录，并报告目标目录是否由本次调用创建。
func (Filesystem) Ensure(ctx context.Context, absolutePath string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	info, err := os.Stat(absolutePath)
	if err == nil {
		if !info.IsDir() {
			return false, errors.New("project path is not a directory")
		}
		return false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("inspect project directory: %w", err)
	}
	if err := os.MkdirAll(absolutePath, 0o700); err != nil {
		return false, fmt.Errorf("create project directory: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return true, err
	}
	return true, nil
}

// RemoveEmpty 只删除空目录，不递归删除用户内容。
func (Filesystem) RemoveEmpty(ctx context.Context, absolutePath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Remove(absolutePath)
}

// ReadText 在项目根目录内读取有界 UTF-8 普通文件。
func (Filesystem) ReadText(ctx context.Context, projectRoot, relativePath string, maxBytes int64) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if maxBytes <= 0 {
		return "", errors.New("project text limit must be positive")
	}
	root, err := os.OpenRoot(projectRoot)
	if err != nil {
		return "", fmt.Errorf("open project directory: %w", err)
	}
	defer root.Close()
	info, err := root.Stat(relativePath)
	if err != nil {
		return "", fmt.Errorf("inspect project file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", projectfile.ErrNotRegularFile
	}
	if info.Size() > maxBytes {
		return "", projectfile.ErrFileTooLarge
	}
	file, err := root.Open(relativePath)
	if err != nil {
		return "", fmt.Errorf("open project file: %w", err)
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return "", fmt.Errorf("inspect opened project file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", projectfile.ErrNotRegularFile
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return "", fmt.Errorf("read project file: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if int64(len(data)) > maxBytes {
		return "", projectfile.ErrFileTooLarge
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return "", projectfile.ErrInvalidText
	}
	return string(bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))), nil
}
