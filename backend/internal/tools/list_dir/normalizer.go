package listdir

import (
	toolcontracts "praxis/internal/tools/contracts"
	toolshared "praxis/internal/tools/shared"
)

const (
	defaultLimit = 200
	maxLimit     = 1000
)

type arguments struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// Normalize 校验 list_dir 参数并提取待授权的资源路径。
func Normalize(
	call toolcontracts.ToolCall,
) (toolcontracts.NormalizedToolCall, error) {
	return toolshared.NormalizePathArguments(call, toolcontracts.ToolListDir, defaultLimit, 1, maxLimit)
}
