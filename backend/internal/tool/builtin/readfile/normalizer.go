package readfile

import (
	"praxis/internal/tool"
	toolargs "praxis/internal/tool/arguments"
	"unicode/utf8"
)

const (
	defaultLimit = 32 * 1024
	minLimit     = utf8.UTFMax
	maxLimit     = tool.MaxInlineResultBytes
)

type arguments struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// Normalize 校验 read_file 参数并提取待授权的资源路径。
func Normalize(
	call tool.ToolCall,
	workspacePath string,
) (tool.NormalizedToolCall, error) {
	return toolargs.NormalizePathArguments(call, workspacePath, tool.ToolReadFile, defaultLimit, minLimit, maxLimit)
}
