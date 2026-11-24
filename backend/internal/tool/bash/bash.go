package bash

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"praxis/internal/tool"
	"strings"
	"time"
)

const inputSchema = `{
  "type": "object",
  "properties": {
    "command": {
      "type": "string",
      "minLength": 1,
      "maxLength": %[1]d,
      "description": "要执行的 Bash 命令或多行脚本；maxLength 按字符计，实际 UTF-8 输入不得超过 %[1]d 字节"
    },
    "path": {
      "type": "string",
      "description": "脚本工作目录，可以是绝对路径或相对于项目根目录的路径"
    },
    "timeout": {
      "type": "number",
      "exclusiveMinimum": 0,
      "maximum": %[2]d,
      "description": "超时时间，单位为秒；省略时为 %[3]d 秒，最长 %[2]d 秒"
    }
  },
  "required": ["command"],
  "additionalProperties": false
}`

type Limits struct {
	DefaultTimeoutSeconds int
	MaxTimeoutSeconds     int
	MaxCommandBytes       int
	ExitWait              time.Duration
	InlineResultBytes     int
}

type bashTool struct {
	shell      string
	limits     Limits
	definition tool.ToolDefinition
}

func New(shell string, limits Limits) tool.Tool {
	definition := tool.ToolDefinition{
		Name: "bash",
		Description: "在已授权的项目目录内执行 Bash 命令或多行脚本，返回合并后的 stdout 和 stderr。" +
			"输入 command，可选 path 和 timeout（秒）；相对 path 按项目根目录解析。" +
			"脚本在宿主机执行；只在用户显式授予命令权限时可用。" +
			fmt.Sprintf("输出或编码结果超过 %d 字节时，将完整正文写入当前会话的临时文件，返回 outputFile、size 和执行状态，可用 read_file 分段读取。", limits.InlineResultBytes) +
			"脚本受当前执行的取消和超时控制；不适合启动长期进程。",
		InputSchema: json.RawMessage(fmt.Sprintf(inputSchema, limits.MaxCommandBytes, limits.MaxTimeoutSeconds, limits.DefaultTimeoutSeconds)),
	}
	return &bashTool{
		shell:      shell,
		limits:     limits,
		definition: definition,
	}
}

func (t *bashTool) Definition() tool.ToolDefinition {
	definition := t.definition
	definition.InputSchema = bytes.Clone(definition.InputSchema)
	return definition
}

type arguments struct {
	Command string   `json:"command"`
	Path    string   `json:"path"`
	Timeout *float64 `json:"timeout"`
}

// NewArguments 提供业务默认目录和超时；显式超时为零不会回退到默认值。
func (t *bashTool) NewArguments() any {
	return &arguments{
		Path:    ".",
		Timeout: new(float64(t.limits.DefaultTimeoutSeconds)),
	}
}

func (a *arguments) PathArgument() *string { return &a.Path }

// Prepare 接收编排层已解析的工作目录，只校验命令和超时范围。
func (t *bashTool) Prepare(input any, environment tool.Environment) (tool.PreparedCall, error) {
	decoded, ok := input.(*arguments)
	if !ok {
		return tool.PreparedCall{}, errors.New("tool argument type does not match the registered arguments")
	}
	args := *decoded
	if strings.TrimSpace(args.Command) == "" {
		return tool.PreparedCall{}, errors.New("bash arguments are invalid: command is required")
	}
	if len(args.Command) > t.limits.MaxCommandBytes {
		return tool.PreparedCall{}, fmt.Errorf("bash arguments are invalid: command must not exceed %d UTF-8 bytes", t.limits.MaxCommandBytes)
	}
	if args.Timeout == nil || math.IsNaN(*args.Timeout) || math.IsInf(*args.Timeout, 0) ||
		*args.Timeout <= 0 || *args.Timeout > float64(t.limits.MaxTimeoutSeconds) {
		return tool.PreparedCall{}, fmt.Errorf("bash arguments are invalid: timeout must be greater than 0 and at most %d seconds", t.limits.MaxTimeoutSeconds)
	}
	return tool.PreparedCall{
		Arguments: args,
		Call: func(ctx context.Context) (tool.ToolResult, error) {
			return t.call(ctx, args, environment)
		},
	}, nil
}
