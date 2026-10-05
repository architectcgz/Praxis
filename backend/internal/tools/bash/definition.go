package bash

import (
	"encoding/json"

	toolcontracts "praxis/internal/tools/contracts"
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
      "description": "脚本工作目录，可以是绝对路径或相对于执行工作区的路径"
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

// Definition 返回 bash 的模型可见工具契约。
func Definition() toolcontracts.ToolDefinition {
	return toolcontracts.ToolDefinition{
		Name: toolcontracts.ToolBash,
		Description: "在已授权的工作区内执行 Bash 命令或多行脚本，返回合并后的 stdout 和 stderr。" +
			"输入 command，可选 path 和 timeout（秒）；相对 path 按工作区解析。" +
			"脚本在宿主机执行；只在用户显式授予命令权限时可用。" +
			"脚本受当前 turn 的取消、超时和输出大小限制；不适合启动长期进程。",
		InputSchema: append(json.RawMessage(nil), inputSchema...),
	}
}
