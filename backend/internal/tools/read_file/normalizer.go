package readfile

import (
	toolmodel "praxis/internal/tool_invocation"
	"unicode/utf8"

	toolcontracts "praxis/internal/tools/contracts"
	toolshared "praxis/internal/tools/shared"
)

const (
	defaultLimit = 32 * 1024
	minLimit     = utf8.UTFMax
	maxLimit     = toolmodel.MaxInlineToolResultBytes
)

type arguments struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// Normalize 校验 read_file 参数并提取待授权的资源路径。
func Normalize(
	call toolcontracts.ToolCall,
) (toolcontracts.NormalizedToolCall, error) {
	return toolshared.NormalizePathArguments(call, toolcontracts.ToolReadFile, defaultLimit, minLimit, maxLimit)
}
