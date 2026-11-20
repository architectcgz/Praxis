package tools

import (
	"fmt"
	"math"
	"strings"
)

const BashName = "bash"

const BashDescription = "在已授权的项目目录内执行 Bash 命令或多行脚本，返回合并后的 stdout 和 stderr。" +
	"输入 command，可选 path 和 timeout（秒）；相对 path 按项目根目录解析。" +
	"脚本在宿主机执行；只在用户显式授予命令权限时可用。" +
	"输出超过 50 KiB 时完整写入当前会话的临时文件，返回 outputFile、size 和执行状态，可用 read_file 分段读取。" +
	"脚本受当前执行的取消和超时控制；不适合启动长期进程。"

const BashInputSchema = `{
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
}`

const (
	maxCommandBytes = 128 * 1024
	defaultTimeout  = 120
	maxTimeout      = 600
)

// BashArguments 在解码、参数准备和执行阶段使用同一类型。
type BashArguments struct {
	Command string  `json:"command"`
	Path    string  `json:"path"`
	Timeout float64 `json:"timeout,omitzero"`
}

// DefaultBashArguments 提供业务默认目录和超时，显式超时为零时仍按无效值处理。
func DefaultBashArguments() BashArguments {
	return BashArguments{
		Path:    ".",
		Timeout: defaultTimeout,
	}
}

func (a *BashArguments) PathArgument() *string { return &a.Path }

// PrepareBashArguments 接收编排层已解析的工作目录，只校验命令和超时范围。
func PrepareBashArguments(args BashArguments) (BashArguments, error) {
	invalidArguments := func(reason string) (BashArguments, error) {
		return BashArguments{}, fmt.Errorf("bash arguments are invalid: %s", reason)
	}
	if strings.TrimSpace(args.Command) == "" {
		return invalidArguments("command is required")
	}
	if len(args.Command) > maxCommandBytes {
		return invalidArguments("command is too long")
	}
	if math.IsNaN(args.Timeout) || math.IsInf(args.Timeout, 0) ||
		args.Timeout <= 0 || args.Timeout > maxTimeout {
		return invalidArguments("timeout must be greater than 0 and within the supported range")
	}
	return args, nil
}
