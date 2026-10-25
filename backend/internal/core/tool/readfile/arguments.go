package readfile

import (
	"fmt"
	"praxis/internal/core/tool"
	"unicode/utf8"
)

const (
	defaultLimit = 32 * 1024
	minLimit     = utf8.UTFMax
	maxLimit     = tool.MaxInlineResultBytes
)

// Arguments 在解码、参数准备和执行阶段使用同一类型。
type Arguments struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitzero"`
	Limit  int    `json:"limit,omitzero"`
}

// NewArguments 提供业务默认值；JSON 解码只覆盖显式输入，limit 为零时仍须按无效值处理。
func NewArguments() Arguments {
	return Arguments{Limit: defaultLimit}
}

func (a *Arguments) PathArgument() *string { return &a.Path }

// Prepare 接收编排层已解析的路径，只校验读取范围；实际资源权限在读取边界检查。
func Prepare(args Arguments) (Arguments, error) {
	invalidArguments := func(reason string) (Arguments, error) {
		return Arguments{}, fmt.Errorf("read_file arguments are invalid: %s", reason)
	}
	if args.Offset < 0 {
		return invalidArguments("offset cannot be negative")
	}
	if args.Limit < minLimit || args.Limit > maxLimit {
		return invalidArguments(fmt.Sprintf("limit must be between %d and %d", minLimit, maxLimit))
	}
	return args, nil
}
