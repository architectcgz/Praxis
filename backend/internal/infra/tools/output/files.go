// Package output 保存超过内联限制的完整工具输出，文件由系统临时目录管理。
package output

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
)

// Files 不缓存结果；会话共享同一目录，执行结束不删除仍被历史引用的文件。
type Files struct {
	Root string
}

func (f Files) Directory(sessionID identity.SessionID) string {
	return filepath.Join(f.Root, sessionID.String())
}

// Bound 在 Entry 创建之前转存完整结果，保留工具的错误和副作用状态。
func (f Files) Bound(sessionID identity.SessionID, result tool.ToolResult) (tool.ToolResult, error) {
	if len(result.Payload) <= tool.MaxInlineResultBytes {
		return result, nil
	}
	file, err := Save(f.Directory(sessionID), result.Payload)
	if err != nil {
		return result, err
	}
	encoded, err := json.Marshal(struct {
		OutputFile string `json:"outputFile"`
		Size       int64  `json:"size"`
		Message    string `json:"message"`
	}{
		OutputFile: file.Path,
		Size:       file.Size,
		Message:    "完整工具输出已保存到临时文件，请使用 read_file 按 offset 和 limit 分段读取；临时文件可能被系统清理。",
	})
	if err != nil {
		return result, err
	}
	result.Payload = string(encoded)
	result.Truncated = false
	return result, nil
}

type File struct {
	Path string
	Size int64
}

func Save(directory, content string) (File, error) {
	file, err := create(directory)
	if err != nil {
		return File{}, err
	}
	size, writeErr := io.WriteString(file, content)
	err = errors.Join(writeErr, file.Close())
	if err != nil {
		_ = os.Remove(file.Name())
		return File{}, err
	}
	return File{Path: file.Name(), Size: int64(size)}, nil
}

func create(directory string) (*os.File, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	return os.CreateTemp(directory, "output-*.txt")
}
