package applypatch

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

// OperationKind 表示补丁声明的文件变更，不包含文件系统状态。
type OperationKind uint8

const (
	Update OperationKind = iota + 1
	Add
	Delete
)

// Operation 暴露资源定位信息，补丁内容和匹配算法由 Core 持有。
type Operation struct {
	Kind    OperationKind
	Path    string
	MoveTo  string
	content []string
	hunks   []hunk
}

type hunk struct {
	oldStart int
	oldLines []string
	newLines []string
}

// Parse 校验补丁语法与相对路径，不读取或修改目标文件。
func Parse(patch string) ([]Operation, error) {
	if !utf8.ValidString(patch) {
		return nil, errors.New("patch must be valid UTF-8")
	}
	lines := strings.Split(strings.ReplaceAll(patch, "\r\n", "\n"), "\n")
	if len(lines) < 2 || lines[0] != "*** Begin Patch" {
		return nil, errors.New("patch must start with *** Begin Patch")
	}
	end := -1
	for index := 1; index < len(lines); index++ {
		if lines[index] == "*** End Patch" {
			end = index
			break
		}
	}
	if end < 0 {
		return nil, errors.New("patch must end with *** End Patch")
	}
	for _, line := range lines[end+1:] {
		if strings.TrimSpace(line) != "" {
			return nil, errors.New("patch contains content after *** End Patch")
		}
	}

	operations := make([]Operation, 0)
	for index := 1; index < end; {
		line := lines[index]
		switch {
		case strings.HasPrefix(line, "*** Update File: "):
			path, err := parsePath(line, "*** Update File: ")
			if err != nil {
				return nil, err
			}
			value, next, err := parseUpdate(lines, index+1, end)
			if err != nil {
				return nil, fmt.Errorf("update file %q: %w", path, err)
			}
			value.Path = path
			operations = append(operations, value)
			index = next
		case strings.HasPrefix(line, "*** Add File: "):
			path, err := parsePath(line, "*** Add File: ")
			if err != nil {
				return nil, err
			}
			content, next, err := parseAdd(lines, index+1, end)
			if err != nil {
				return nil, fmt.Errorf("add file %q: %w", path, err)
			}
			operations = append(operations, Operation{
				Kind:    Add,
				Path:    path,
				content: content,
			})
			index = next
		case strings.HasPrefix(line, "*** Delete File: "):
			path, err := parsePath(line, "*** Delete File: ")
			if err != nil {
				return nil, err
			}
			operations = append(operations, Operation{Kind: Delete, Path: path})
			index++
		default:
			return nil, fmt.Errorf("unexpected patch line %q", line)
		}
	}
	if len(operations) == 0 {
		return nil, errors.New("patch does not contain a file operation")
	}
	return operations, nil
}

func parsePath(line, prefix string) (string, error) {
	path := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if path == "" {
		return "", errors.New("file path is required")
	}
	if strings.ContainsAny(path, "\x00\r\n") {
		return "", errors.New("file path is invalid")
	}
	path = filepath.Clean(filepath.FromSlash(path))
	if path == "." || path == ".." || filepath.IsAbs(path) || filepath.VolumeName(path) != "" ||
		strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("file path %q must stay inside the patch root", path)
	}
	return path, nil
}

func parseAdd(lines []string, index, end int) ([]string, int, error) {
	content := make([]string, 0)
	for index < end && !isOperationHeader(lines[index]) {
		line := lines[index]
		if line == "" || line[0] != '+' {
			return nil, index, errors.New("added file content must prefix every line with +")
		}
		content = append(content, line[1:])
		index++
	}
	return content, index, nil
}

func parseUpdate(lines []string, index, end int) (Operation, int, error) {
	value := Operation{Kind: Update}
	for index < end && !isOperationHeader(lines[index]) {
		line := lines[index]
		switch {
		case strings.HasPrefix(line, "*** Move to: "):
			if value.MoveTo != "" {
				return Operation{}, index, errors.New("move target is specified more than once")
			}
			path, err := parsePath(line, "*** Move to: ")
			if err != nil {
				return Operation{}, index, err
			}
			value.MoveTo = path
			index++
		case line == "*** End of File":
			index++
		case strings.HasPrefix(line, "@@"):
			valueHunk, next, err := parseHunk(lines, index, end)
			if err != nil {
				return Operation{}, index, err
			}
			value.hunks = append(value.hunks, valueHunk)
			index = next
		default:
			return Operation{}, index, fmt.Errorf("unexpected update line %q", line)
		}
	}
	if len(value.hunks) == 0 {
		return Operation{}, index, errors.New("updated file requires at least one hunk")
	}
	return value, index, nil
}

func parseHunk(lines []string, index, end int) (hunk, int, error) {
	oldStart, err := parseHunkStart(lines[index])
	if err != nil {
		return hunk{}, index, err
	}
	value := hunk{oldStart: oldStart}
	index++
	for index < end && !strings.HasPrefix(lines[index], "@@") && !isOperationHeader(lines[index]) &&
		!strings.HasPrefix(lines[index], "*** Move to: ") && lines[index] != "*** End of File" {
		line := lines[index]
		if line == "" {
			return hunk{}, index, errors.New("hunk lines require a prefix")
		}
		switch line[0] {
		case ' ':
			value.oldLines = append(value.oldLines, line[1:])
			value.newLines = append(value.newLines, line[1:])
		case '-':
			value.oldLines = append(value.oldLines, line[1:])
		case '+':
			value.newLines = append(value.newLines, line[1:])
		case '\\':
			if line != `\ No newline at end of file` {
				return hunk{}, index, fmt.Errorf("unsupported hunk marker %q", line)
			}
		default:
			return hunk{}, index, fmt.Errorf("invalid hunk line %q", line)
		}
		index++
	}
	if len(value.oldLines) == 0 && len(value.newLines) == 0 {
		return hunk{}, index, errors.New("hunk is empty")
	}
	return value, index, nil
}

func parseHunkStart(line string) (int, error) {
	if line == "@@" {
		return 0, nil
	}
	rest := strings.TrimPrefix(line, "@@")
	rest, _, _ = strings.Cut(rest, "@@")
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return 0, nil
	}
	if !strings.HasPrefix(fields[0], "-") {
		return 0, errors.New("hunk header is missing the old file range")
	}
	oldRange := strings.TrimPrefix(fields[0], "-")
	oldRange, _, _ = strings.Cut(oldRange, ",")
	start, err := strconv.Atoi(oldRange)
	if err != nil || start < 0 {
		return 0, errors.New("hunk header has an invalid old file range")
	}
	return start, nil
}

func isOperationHeader(line string) bool {
	return strings.HasPrefix(line, "*** Update File: ") ||
		strings.HasPrefix(line, "*** Add File: ") ||
		strings.HasPrefix(line, "*** Delete File: ")
}
