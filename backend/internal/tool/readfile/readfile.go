package readfile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"praxis/internal/tool"
	"unicode/utf8"
)

const inputSchema = `{
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

type Limits struct {
	DefaultReadBytes  int
	MaxReadBytes      int
	InlineResultBytes int
}

type readFileTool struct {
	limits     Limits
	definition tool.ToolDefinition
}

func New(limits Limits) tool.Tool {
	definition := tool.ToolDefinition{
		Name: "read_file",
		Description: "读取项目目录或当前会话工具输出目录内已知 UTF-8 普通文件；不要用它列目录。" +
			"输入 path，可选 offset 和 limit；相对路径按项目根目录解析。" +
			"返回包含文件定位信息和 content 的 JSON 对象；内容未读完时提供 nextOffset。" +
			fmt.Sprintf("单次最多读取 %d 字节原文，编码后的结果最多内联 %d 字节。", limits.MaxReadBytes, limits.InlineResultBytes),
		InputSchema: json.RawMessage(fmt.Sprintf(inputSchema, utf8.UTFMax, limits.MaxReadBytes, limits.DefaultReadBytes)),
	}
	return &readFileTool{
		limits:     limits,
		definition: definition,
	}
}

func (*readFileTool) ReadOnly() bool { return true }

func (t *readFileTool) Definition() tool.ToolDefinition {
	definition := t.definition
	definition.InputSchema = bytes.Clone(definition.InputSchema)
	return definition
}

type arguments struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitzero"`
	Limit  *int   `json:"limit"`
}

// NewArguments 提供业务默认值；JSON 解码只覆盖显式输入，limit 为零时仍须按无效值处理。
func (t *readFileTool) NewArguments() any {
	return &arguments{Limit: new(t.limits.DefaultReadBytes)}
}

func (a *arguments) PathArgument() *string { return &a.Path }

// Prepare 接收编排层已解析的路径，只校验读取范围；实际资源权限在读取边界检查。
func (t *readFileTool) Prepare(input any, environment tool.Environment) (tool.PreparedCall, error) {
	decoded, ok := input.(*arguments)
	if !ok {
		return tool.PreparedCall{}, errors.New("tool argument type does not match the registered arguments")
	}
	args := *decoded
	if args.Offset < 0 {
		return tool.PreparedCall{}, errors.New("read_file arguments are invalid: offset cannot be negative")
	}
	if args.Limit == nil || *args.Limit < utf8.UTFMax || *args.Limit > t.limits.MaxReadBytes {
		return tool.PreparedCall{}, fmt.Errorf("read_file arguments are invalid: limit must be between %d and %d bytes", utf8.UTFMax, t.limits.MaxReadBytes)
	}
	return tool.PreparedCall{
		Arguments: args,
		Call: func(ctx context.Context) (tool.ToolResult, error) {
			return t.call(ctx, args, environment)
		},
	}, nil
}
