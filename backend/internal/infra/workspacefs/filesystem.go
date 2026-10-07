// Package workspacefs 实现本地 Workspace 目录和文本读取 Port。
package workspacefs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"unicode/utf8"

	"praxis/internal/contracts"
)

// Filesystem 使用操作系统文件系统实现受限的 Workspace 操作。
type Filesystem struct{}

// Ensure 确保绝对路径是目录，并报告目标目录是否由本次调用创建。
func (Filesystem) Ensure(ctx context.Context, absolutePath string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	info, err := os.Stat(absolutePath)
	if err == nil {
		if !info.IsDir() {
			return false, errors.New("workspace path is not a directory")
		}
		return false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("inspect workspace directory: %w", err)
	}
	if err := os.MkdirAll(absolutePath, 0o700); err != nil {
		return false, fmt.Errorf("create workspace directory: %w", err)
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

// ReadText 在 Workspace 根目录内读取有界 UTF-8 普通文件。
func (Filesystem) ReadText(ctx context.Context, workspaceRoot, relativePath string, maxBytes int64) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if maxBytes <= 0 {
		return "", errors.New("workspace text limit must be positive")
	}
	root, err := os.OpenRoot(workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("open workspace: %w", err)
	}
	defer root.Close()
	info, err := root.Stat(relativePath)
	if err != nil {
		return "", fmt.Errorf("inspect workspace file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", contracts.New(contracts.InvalidRequest, "只能预览普通文本文件。")
	}
	if info.Size() > maxBytes {
		return "", contracts.New(contracts.InvalidRequest, "文件超过 1 MiB，无法预览。")
	}
	file, err := root.Open(relativePath)
	if err != nil {
		return "", fmt.Errorf("open workspace file: %w", err)
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return "", fmt.Errorf("inspect opened workspace file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", contracts.New(contracts.InvalidRequest, "只能预览普通文本文件。")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return "", fmt.Errorf("read workspace file: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if int64(len(data)) > maxBytes {
		return "", contracts.New(contracts.InvalidRequest, "文件超过 1 MiB，无法预览。")
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return "", contracts.New(contracts.InvalidRequest, "仅支持 UTF-8 文本预览，不支持二进制文件。")
	}
	return string(bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))), nil
}
