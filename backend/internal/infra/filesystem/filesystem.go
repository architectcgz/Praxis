// Package fs 实现本地项目目录准备能力。
package fs

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// Filesystem 使用操作系统文件系统准备项目根目录。
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
