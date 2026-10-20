package readfile

import (
	"encoding/json"
	"praxis/internal/core/tool"
	toolargs "praxis/internal/infra/tools/arguments"
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

// Prepare 校验 read_file 参数并提取待授权的资源路径。
func Prepare(
	args json.RawMessage,
	workspacePath string,
) (json.RawMessage, error) {
	return toolargs.PreparePathArguments(args, workspacePath, defaultLimit, minLimit, maxLimit)
}
