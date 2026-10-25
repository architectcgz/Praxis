package applypatch

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// Apply 计算新增或修改后的内容，不访问文件系统；上下文不匹配时返回错误且不改变原始内容。
func (o Operation) Apply(content []byte) ([]byte, error) {
	switch o.Kind {
	case Add:
		if len(o.content) == 0 {
			return nil, nil
		}
		return []byte(strings.Join(o.content, "\n") + "\n"), nil
	case Update:
		return applyHunks(content, o.hunks)
	default:
		return nil, errors.New("file operation does not produce content")
	}
}

func applyHunks(content []byte, hunks []hunk) ([]byte, error) {
	if !utf8.Valid(content) {
		return nil, errors.New("apply_patch supports UTF-8 text files only")
	}
	newline := "\n"
	if strings.Contains(string(content), "\r\n") {
		newline = "\r\n"
	}
	normalized := strings.ReplaceAll(string(content), "\r\n", "\n")
	endsWithNewline := strings.HasSuffix(normalized, "\n")
	if endsWithNewline {
		normalized = strings.TrimSuffix(normalized, "\n")
	}
	lines := strings.Split(normalized, "\n")
	if normalized == "" && !endsWithNewline {
		lines = nil
	}
	cursor := 0
	for _, value := range hunks {
		index := findHunk(lines, value, cursor)
		if index < 0 {
			return nil, errors.New("hunk context does not match the file")
		}
		updated := make([]string, 0, len(lines)-len(value.oldLines)+len(value.newLines))
		updated = append(updated, lines[:index]...)
		updated = append(updated, value.newLines...)
		updated = append(updated, lines[index+len(value.oldLines):]...)
		lines = updated
		cursor = index + len(value.newLines)
	}
	result := strings.Join(lines, "\n")
	if endsWithNewline && len(lines) > 0 {
		result += "\n"
	}
	if newline != "\n" {
		result = strings.ReplaceAll(result, "\n", newline)
	}
	return []byte(result), nil
}

// findHunk 优先使用声明行号，否则从已应用位置向后匹配上下文，避免重复命中已处理片段。
func findHunk(lines []string, value hunk, cursor int) int {
	if len(value.oldLines) == 0 {
		if value.oldStart > 0 {
			return min(value.oldStart-1, len(lines))
		}
		return min(cursor, len(lines))
	}
	preferred := cursor
	if value.oldStart > 0 {
		preferred = value.oldStart - 1
	}
	if hunkMatches(lines, preferred, value.oldLines) {
		return preferred
	}
	for index := max(0, cursor); index+len(value.oldLines) <= len(lines); index++ {
		if hunkMatches(lines, index, value.oldLines) {
			return index
		}
	}
	return -1
}

func hunkMatches(lines []string, index int, expected []string) bool {
	if index < 0 || index+len(expected) > len(lines) {
		return false
	}
	for offset, line := range expected {
		if lines[index+offset] != line {
			return false
		}
	}
	return true
}
