package workspace

import (
	"context"
	"errors"
)

// TextReader 读取工作区内有界 UTF-8 普通文件；实现通过下列错误报告内容边界。
type TextReader interface {
	ReadText(context.Context, string, string, int64) (string, error)
}

var (
	ErrFileTooLarge   = errors.New("file exceeds the size limit")
	ErrNotRegularFile = errors.New("file is not a regular file")
	ErrInvalidText    = errors.New("file is not valid UTF-8 text")
)
