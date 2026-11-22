package readfile

import (
	"encoding/json"
	"fmt"
	"praxis/internal/core/tool"
	"unicode/utf8"
)

const (
	defaultLimit = 32 * 1024
	minLimit     = utf8.UTFMax
	maxLimit     = tool.MaxInlineResultBytes
)

var inputSchema = json.RawMessage(`{
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
      "minimum": 4,
      "maximum": 51200,
      "description": "返回的最大 UTF-8 内容字节数"
    }
  },
  "required": ["path"],
  "additionalProperties": false
}`)

// Definition 返回 read_file 的模型可见工具契约。
func Definition() tool.ToolDefinition {
	return tool.ToolDefinition{
		Name: "read_file",
		Description: "读取项目目录或当前会话工具输出目录内已知 UTF-8 普通文件；不要用它列目录。" +
			"输入 path，可选 offset 和 limit；相对路径按项目根目录解析。" +
			"返回包含文件定位信息和 content 的 JSON 对象；内容未读完时提供 nextOffset。" +
			"仅支持已授权的普通文件，单次返回最多 50 KiB。",
		InputSchema: append(json.RawMessage(nil), inputSchema...),
	}
}

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
