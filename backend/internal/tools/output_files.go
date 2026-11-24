package tools

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"praxis/internal/identity"
)

// OutputFiles 不缓存结果；会话共享同一目录，执行结束不删除仍被历史引用的文件。
type OutputFiles struct {
	Root string
}

func (f OutputFiles) Directory(sessionID identity.SessionID) string {
	return filepath.Join(f.Root, sessionID.String())
}

// Bound 在 Entry 创建之前转存完整结果，保留工具的错误和副作用状态。
func (f OutputFiles) Bound(sessionID identity.SessionID, result ToolResult, limit int) (ToolResult, error) {
	if len(result.Payload) <= limit {
		return result, nil
	}
	file, err := saveOutput(f.Directory(sessionID), result.Payload)
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

type outputFile struct {
	Path string
	Size int64
}

func saveOutput(directory, content string) (outputFile, error) {
	file, err := createOutputFile(directory)
	if err != nil {
		return outputFile{}, err
	}
	size, writeErr := io.WriteString(file, content)
	err = errors.Join(writeErr, file.Close())
	if err != nil {
		_ = os.Remove(file.Name())
		return outputFile{}, err
	}
	return outputFile{Path: file.Name(), Size: int64(size)}, nil
}

func createOutputFile(directory string) (*os.File, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	return os.CreateTemp(directory, "output-*.txt")
}
