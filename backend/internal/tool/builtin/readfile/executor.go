package readfile

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"praxis/internal/tool"
	"praxis/internal/utils/pathutil"
	"unicode/utf8"
)

type output struct {
	Path       string `json:"path"`
	Offset     int    `json:"offset"`
	Limit      int    `json:"limit"`
	Size       int64  `json:"size"`
	Content    string `json:"content"`
	NextOffset *int   `json:"nextOffset,omitempty"`
}

// Execute 读取 Workspace 内的 UTF-8 普通文件并返回有界结果。
func Execute(ctx context.Context, call tool.ExecutionCall) (tool.ToolResult, error) {
	if ctx == nil {
		return tool.ToolResult{}, errors.New("tool task context is required")
	}
	if err := ctx.Err(); err != nil {
		return tool.ToolResult{}, err
	}
	if call.Name != tool.ToolReadFile {
		return tool.ToolResult{}, errors.New("read_file executor does not support the call")
	}
	var toolArguments arguments
	if err := json.Unmarshal(call.NormalizedArguments, &toolArguments); err != nil {
		return tool.ToolResult{}, errors.New("normalized read_file arguments are invalid")
	}
	if toolArguments.Path != call.Path || toolArguments.Offset < 0 ||
		toolArguments.Limit < minLimit || toolArguments.Limit > maxLimit ||
		!workspaceFile(toolArguments.Path, call.WorkspacePath) {
		return tool.ToolResult{}, errors.New("read_file path is outside a workspace regular file")
	}
	file, err := os.Open(toolArguments.Path)
	if err != nil {
		return tool.ToolResult{}, errors.New("read_file could not open the file")
	}
	defer file.Close()
	fileInfo, err := file.Stat()
	if err != nil || !fileInfo.Mode().IsRegular() || !workspaceFile(toolArguments.Path, call.WorkspacePath) {
		return tool.ToolResult{}, errors.New("read_file path is outside a workspace regular file")
	}
	if err := ctx.Err(); err != nil {
		return tool.ToolResult{}, err
	}
	fileSize := fileInfo.Size()
	offset := toolArguments.Offset
	if int64(offset) > fileSize {
		offset = int(fileSize)
	}
	if _, err := file.Seek(int64(offset), io.SeekStart); err != nil {
		return tool.ToolResult{}, errors.New("read_file could not seek to the requested offset")
	}
	available := fileSize - int64(offset)
	// 额外读取足够的尾部字节，以识别跨越字节上限的字符。
	readLimit := int64(toolArguments.Limit + utf8.UTFMax - 1)
	if available < readLimit {
		readLimit = available
	}
	raw, err := io.ReadAll(io.LimitReader(file, readLimit))
	if err != nil {
		return tool.ToolResult{}, errors.New("read_file could not read the file")
	}
	if err := ctx.Err(); err != nil {
		return tool.ToolResult{}, err
	}
	contentEnd, err := contentEndAtLimit(raw, toolArguments.Limit)
	if err != nil {
		return tool.ToolResult{}, err
	}
	content := raw[:contentEnd]
	encoded, contentEnd, err := encodeBoundedOutput(toolArguments, offset, fileSize, content)
	if err != nil {
		return tool.ToolResult{}, err
	}
	return tool.NewToolSuccess(
		string(encoded),
		int64(offset+contentEnd) < fileSize,
	), nil
}

// contentEndAtLimit 返回不超过 limit 的 UTF-8 字符边界。
func contentEndAtLimit(content []byte, limit int) (int, error) {
	end := 0
	for index := 0; index < len(content) && index < limit; {
		runeValue, size := utf8.DecodeRune(content[index:])
		if runeValue == utf8.RuneError && size == 1 {
			return 0, errors.New("read_file supports UTF-8 text files only")
		}
		if index+size > limit {
			break
		}
		index += size
		end = index
	}
	return end, nil
}

// encodeBoundedOutput 保留 JSON 输出不超过内联工具结果上限的最长 UTF-8 前缀。
func encodeBoundedOutput(
	toolArguments arguments,
	offset int,
	fileSize int64,
	content []byte,
) ([]byte, int, error) {
	ends := utf8RuneEnds(content)
	low, high, best := 0, len(ends)-1, -1
	var encoded []byte
	for low <= high {
		middle := low + (high-low)/2
		contentEnd := ends[middle]
		candidate, err := encodeOutput(toolArguments, offset, fileSize, string(content[:contentEnd]))
		if err != nil {
			return nil, 0, errors.New("read_file could not encode its result")
		}
		if len(candidate) <= tool.MaxInlineResultBytes {
			best, encoded, low = middle, candidate, middle+1
			continue
		}
		high = middle - 1
	}
	if best >= 0 {
		return encoded, ends[best], nil
	}
	empty, err := encodeOutput(toolArguments, offset, fileSize, "")
	if err != nil || len(empty) > tool.MaxInlineResultBytes {
		return nil, 0, errors.New("read_file result metadata exceeds the size limit")
	}
	return empty, 0, nil
}

func utf8RuneEnds(content []byte) []int {
	ends := make([]int, 1, len(content)+1)
	for index := 0; index < len(content); {
		_, size := utf8.DecodeRune(content[index:])
		index += size
		ends = append(ends, index)
	}
	return ends
}

func encodeOutput(toolArguments arguments, offset int, fileSize int64, content string) ([]byte, error) {
	contentEnd := offset + len([]byte(content))
	toolOutput := output{
		Path: toolArguments.Path, Offset: offset, Limit: toolArguments.Limit,
		Size: fileSize, Content: content,
	}
	if int64(contentEnd) < fileSize {
		next := contentEnd
		toolOutput.NextOffset = &next
	}
	return json.Marshal(toolOutput)
}

// workspaceFile 保留普通文件和符号链接检查，读取范围由 Workspace 唯一确定。
func workspaceFile(path, workspace string) bool {
	if !pathutil.IsAbsoluteNormalized(path) || !pathutil.IsAbsoluteNormalized(workspace) || !pathutil.IsWithin(workspace, path) {
		return false
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil || !pathutil.SamePath(path, realPath) {
		return false
	}
	info, err := os.Stat(realPath)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	realWorkspace, err := filepath.EvalSymlinks(workspace)
	return err == nil && pathutil.SamePath(workspace, realWorkspace) && pathutil.IsWithin(realWorkspace, realPath)
}
