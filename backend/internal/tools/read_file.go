package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"praxis/internal/pathutil"
	"unicode/utf8"
)

const readFileInputSchema = `{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "项目文件或当前会话工具输出文件的路径，可以是绝对路径或相对于项目根目录的路径"
    },
    "offset": {
      "type": "integer",
      "minimum": 0,
      "description": "以 UTF-8 字节计的零基偏移量"
    },
    "limit": {
      "type": "integer",
      "minimum": %d,
      "maximum": %d,
      "description": "读取原文的最大 UTF-8 字节数，省略时为 %d 字节；编码后结果仍受内联上限约束"
    }
  },
  "required": ["path"],
  "additionalProperties": false
}`

type ReadFileLimits struct {
	DefaultReadBytes  int
	MaxReadBytes      int
	InlineResultBytes int
}

type readFileArguments struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitzero"`
	Limit  *int   `json:"limit"`
}

func (a *readFileArguments) PathArgument() *string { return &a.Path }

func NewReadFile(limits ReadFileLimits) Tool {
	return Function[readFileArguments]{
		Name: "read_file",
		Description: "读取项目目录或当前会话工具输出目录内已知 UTF-8 普通文件；不要用它列目录。" +
			"输入 path，可选 offset 和 limit；相对路径按项目根目录解析。" +
			"返回包含文件定位信息和 content 的 JSON 对象；内容未读完时提供 nextOffset。" +
			fmt.Sprintf("单次最多读取 %d 字节原文，编码后的结果最多内联 %d 字节。", limits.MaxReadBytes, limits.InlineResultBytes),
		InputSchema:      json.RawMessage(fmt.Sprintf(readFileInputSchema, utf8.UTFMax, limits.MaxReadBytes, limits.DefaultReadBytes)),
		IsReadOnly:       true,
		DefaultArguments: func() readFileArguments { return readFileArguments{Limit: new(limits.DefaultReadBytes)} },
		PrepareArguments: func(args readFileArguments) (readFileArguments, error) {
			if args.Offset < 0 {
				return args, errors.New("read_file arguments are invalid: offset cannot be negative")
			}
			if args.Limit == nil || *args.Limit < utf8.UTFMax || *args.Limit > limits.MaxReadBytes {
				return args, fmt.Errorf("read_file arguments are invalid: limit must be between %d and %d bytes", utf8.UTFMax, limits.MaxReadBytes)
			}
			return args, nil
		},
		Execute: func(ctx context.Context, args readFileArguments, env Environment) (ToolResult, error) {
			return readFile(ctx, args, env, limits)
		},
	}
}

type readFileOutput struct {
	Path       string `json:"path"`
	Offset     int    `json:"offset"`
	Limit      int    `json:"limit"`
	Size       int64  `json:"size"`
	Content    string `json:"content"`
	NextOffset *int   `json:"nextOffset,omitempty"`
}

// readFile 分段读取项目文件或当前会话的临时输出，不开放其他会话和整个临时目录。
func readFile(ctx context.Context, toolArguments readFileArguments, environment Environment, limits ReadFileLimits) (ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return ToolResult{}, err
	}
	if !readableFile(toolArguments.Path, environment) {
		return ToolResult{}, errors.New("read_file requires a regular file within the project or current session output directory")
	}
	file, err := os.Open(toolArguments.Path)
	if err != nil {
		return ToolResult{}, errors.New("read_file could not open the file")
	}
	defer file.Close()
	fileInfo, err := file.Stat()
	if err != nil || !fileInfo.Mode().IsRegular() {
		return ToolResult{}, errors.New("read_file requires a regular file within the project or current session output directory")
	}
	if err := ctx.Err(); err != nil {
		return ToolResult{}, err
	}
	fileSize := fileInfo.Size()
	offset := toolArguments.Offset
	if int64(offset) > fileSize {
		offset = int(fileSize)
	}
	if _, err := file.Seek(int64(offset), io.SeekStart); err != nil {
		return ToolResult{}, errors.New("read_file could not seek to the requested offset")
	}
	available := fileSize - int64(offset)
	// 额外读取足够的尾部字节，以识别跨越字节上限的字符。
	readLimit := int64(*toolArguments.Limit + utf8.UTFMax - 1)
	if available < readLimit {
		readLimit = available
	}
	raw, err := io.ReadAll(io.LimitReader(file, readLimit))
	if err != nil {
		return ToolResult{}, errors.New("read_file could not read the file")
	}
	if err := ctx.Err(); err != nil {
		return ToolResult{}, err
	}
	contentEnd, err := contentEndAtLimit(raw, *toolArguments.Limit)
	if err != nil {
		return ToolResult{}, err
	}
	content := raw[:contentEnd]
	encoded, contentEnd, err := encodeBoundedOutput(toolArguments, offset, fileSize, content, limits.InlineResultBytes)
	if err != nil {
		return ToolResult{}, err
	}
	return NewToolSuccess(
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
	toolArguments readFileArguments,
	offset int,
	fileSize int64,
	content []byte,
	inlineBytes int,
) ([]byte, int, error) {
	ends := utf8RuneEnds(content)
	low, high, best := 0, len(ends)-1, -1
	var encoded []byte
	for low <= high {
		middle := low + (high-low)/2
		contentEnd := ends[middle]
		candidate, err := encodeReadFileOutput(toolArguments, offset, fileSize, string(content[:contentEnd]))
		if err != nil {
			return nil, 0, errors.New("read_file could not encode its result")
		}
		if len(candidate) <= inlineBytes {
			best, encoded, low = middle, candidate, middle+1
			continue
		}
		high = middle - 1
	}
	if best >= 0 {
		return encoded, ends[best], nil
	}
	empty, err := encodeReadFileOutput(toolArguments, offset, fileSize, "")
	if err != nil || len(empty) > inlineBytes {
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

func encodeReadFileOutput(toolArguments readFileArguments, offset int, fileSize int64, content string) ([]byte, error) {
	contentEnd := offset + len([]byte(content))
	toolOutput := readFileOutput{
		Path:    toolArguments.Path,
		Offset:  offset,
		Limit:   *toolArguments.Limit,
		Size:    fileSize,
		Content: content,
	}
	if int64(contentEnd) < fileSize {
		toolOutput.NextOffset = new(contentEnd)
	}
	return json.Marshal(toolOutput)
}

// projectFile 保留普通文件和符号链接检查，读取范围由项目根目录确定。
func projectFile(path, projectRoot string) bool {
	if projectRoot == "" || !pathutil.IsWithin(projectRoot, path) {
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
	realRoot, err := filepath.EvalSymlinks(projectRoot)
	return err == nil && pathutil.SamePath(projectRoot, realRoot) && pathutil.IsWithin(realRoot, realPath)
}

func readableFile(path string, environment Environment) bool {
	return projectFile(path, environment.ProjectPath) || projectFile(path, environment.OutputDirectory)
}
