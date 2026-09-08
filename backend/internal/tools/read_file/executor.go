package readfile

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	domainexecution "praxis/internal/domain/execution"
	domainsecurity "praxis/internal/domain/security"
	runtimecontract "praxis/internal/runtime"
)

type output struct {
	Path       string `json:"path"`
	Offset     int    `json:"offset"`
	Limit      int    `json:"limit"`
	Size       int64  `json:"size"`
	Content    string `json:"content"`
	NextOffset *int   `json:"nextOffset,omitempty"`
}

// Execute reads an authorized UTF-8 regular file and returns one bounded page.
func Execute(ctx context.Context, call runtimecontract.AuthorizedToolCall) (runtimecontract.ToolResult, error) {
	if ctx == nil {
		return runtimecontract.ToolResult{}, errors.New("tool execution context is required")
	}
	if err := ctx.Err(); err != nil {
		return runtimecontract.ToolResult{}, err
	}
	if call.Name != domainsecurity.ToolReadFile {
		return runtimecontract.ToolResult{}, errors.New("read_file executor does not support the call")
	}
	call = call.Snapshot()
	var toolArguments arguments
	if err := json.Unmarshal(call.NormalizedArguments, &toolArguments); err != nil {
		return runtimecontract.ToolResult{}, errors.New("normalized read_file arguments are invalid")
	}
	if toolArguments.Path != call.Path || toolArguments.Offset < 0 ||
		toolArguments.Limit < minLimit || toolArguments.Limit > maxLimit ||
		!authorizedRealFile(toolArguments.Path, call.ReadScopes) {
		return runtimecontract.ToolResult{}, errors.New("read_file path is outside an authorized regular file")
	}
	file, err := os.Open(toolArguments.Path)
	if err != nil {
		return runtimecontract.ToolResult{}, errors.New("read_file could not open the file")
	}
	defer file.Close()
	fileInfo, err := file.Stat()
	if err != nil || !fileInfo.Mode().IsRegular() || !authorizedRealFile(toolArguments.Path, call.ReadScopes) {
		return runtimecontract.ToolResult{}, errors.New("read_file path is outside an authorized regular file")
	}
	if err := ctx.Err(); err != nil {
		return runtimecontract.ToolResult{}, err
	}
	fileSize := fileInfo.Size()
	offset := toolArguments.Offset
	if int64(offset) > fileSize {
		offset = int(fileSize)
	}
	if _, err := file.Seek(int64(offset), io.SeekStart); err != nil {
		return runtimecontract.ToolResult{}, errors.New("read_file could not seek to the requested offset")
	}
	available := fileSize - int64(offset)
	// Read enough trailing bytes to identify a character crossing the byte limit.
	readLimit := int64(toolArguments.Limit + utf8.UTFMax - 1)
	if available < readLimit {
		readLimit = available
	}
	raw, err := io.ReadAll(io.LimitReader(file, readLimit))
	if err != nil {
		return runtimecontract.ToolResult{}, errors.New("read_file could not read the file")
	}
	if err := ctx.Err(); err != nil {
		return runtimecontract.ToolResult{}, err
	}
	contentEnd, err := contentEndAtLimit(raw, toolArguments.Limit)
	if err != nil {
		return runtimecontract.ToolResult{}, err
	}
	content := raw[:contentEnd]
	encoded, contentEnd, err := encodeBoundedOutput(toolArguments, offset, fileSize, content)
	if err != nil {
		return runtimecontract.ToolResult{}, err
	}
	return runtimecontract.ToolResult{
		Content: string(encoded), Truncated: int64(offset+contentEnd) < fileSize,
	}, nil
}

// contentEndAtLimit returns a UTF-8 character boundary at or before limit.
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

// encodeBoundedOutput retains the longest UTF-8 prefix whose JSON output fits
// within the inline tool-result limit.
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
		if len(candidate) <= domainexecution.MaxInlineToolResultBytes {
			best, encoded, low = middle, candidate, middle+1
			continue
		}
		high = middle - 1
	}
	if best >= 0 {
		return encoded, ends[best], nil
	}
	empty, err := encodeOutput(toolArguments, offset, fileSize, "")
	if err != nil || len(empty) > domainexecution.MaxInlineToolResultBytes {
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

func authorizedRealFile(path string, scopes []string) bool {
	cleanPath := filepath.Clean(path)
	realPath, err := filepath.EvalSymlinks(cleanPath)
	if err != nil || !samePath(cleanPath, realPath) {
		return false
	}
	info, err := os.Stat(realPath)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	for _, scope := range scopes {
		cleanScope := filepath.Clean(scope)
		if !pathWithin(cleanScope, cleanPath) {
			continue
		}
		realScope, err := filepath.EvalSymlinks(cleanScope)
		if err == nil && samePath(cleanScope, realScope) && pathWithin(realScope, realPath) {
			return true
		}
	}
	return false
}

func samePath(left, right string) bool {
	left, right = filepath.Clean(left), filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
