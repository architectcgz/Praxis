package bash

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"praxis/internal/tool"
	"strings"
)

const (
	maxCommandBytes = 128 * 1024
	defaultTimeout  = 120
	maxTimeout      = 600
)

var inputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "command": {
      "type": "string",
      "minLength": 1,
      "maxLength": 131072,
      "description": "要执行的 Bash 命令或多行脚本"
    },
    "path": {
      "type": "string",
      "description": "脚本工作目录，可以是绝对路径或相对于项目根目录的路径"
    },
    "timeout": {
      "type": "number",
      "exclusiveMinimum": 0,
      "maximum": 600,
      "description": "超时时间，单位为秒；省略时为 120 秒，最长 600 秒"
    }
  },
  "required": ["command"],
  "additionalProperties": false
}`)

type bashTool struct{}

func New() tool.Tool { return bashTool{} }

func (bashTool) Definition() tool.ToolDefinition {
	return tool.ToolDefinition{
		Name: "bash",
		Description: "在已授权的项目目录内执行 Bash 命令或多行脚本，返回合并后的 stdout 和 stderr。" +
			"输入 command，可选 path 和 timeout（秒）；相对 path 按项目根目录解析。" +
			"脚本在宿主机执行；只在用户显式授予命令权限时可用。" +
			"输出超过 50 KiB 时完整写入当前会话的临时文件，返回 outputFile、size 和执行状态，可用 read_file 分段读取。" +
			"脚本受当前执行的取消和超时控制；不适合启动长期进程。",
		InputSchema: bytes.Clone(inputSchema),
	}
}

type arguments struct {
	Command string  `json:"command"`
	Path    string  `json:"path"`
	Timeout float64 `json:"timeout,omitzero"`
}

// NewArguments 提供业务默认目录和超时；显式超时为零不会回退到默认值。
func (bashTool) NewArguments() any {
	return &arguments{
		Path:    ".",
		Timeout: defaultTimeout,
	}
}

func (a *arguments) PathArgument() *string { return &a.Path }

// Prepare 接收编排层已解析的工作目录，只校验命令和超时范围。
func (bashTool) Prepare(input any, environment tool.Environment) (tool.PreparedCall, error) {
	decoded, ok := input.(*arguments)
	if !ok {
		return tool.PreparedCall{}, errors.New("tool argument type does not match the registered arguments")
	}
	args := *decoded
	if strings.TrimSpace(args.Command) == "" {
		return tool.PreparedCall{}, errors.New("bash arguments are invalid: command is required")
	}
	if len(args.Command) > maxCommandBytes {
		return tool.PreparedCall{}, errors.New("bash arguments are invalid: command is too long")
	}
	if math.IsNaN(args.Timeout) || math.IsInf(args.Timeout, 0) ||
		args.Timeout <= 0 || args.Timeout > maxTimeout {
		return tool.PreparedCall{}, errors.New("bash arguments are invalid: timeout must be greater than 0 and within the supported range")
	}
	return tool.PreparedCall{
		Arguments: args,
		Call: func(ctx context.Context) (tool.ToolResult, error) {
			return call(ctx, args, environment)
		},
	}, nil
}
