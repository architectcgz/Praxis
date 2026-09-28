package listdir

import (
	"encoding/json"

	toolcontracts "praxis/internal/tools/contracts"
)

var inputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "目录路径，可以是绝对路径或相对于执行工作区的路径"
    },
    "offset": {
      "type": "integer",
      "minimum": 0,
      "description": "以目录项计的零基偏移量"
    },
    "limit": {
      "type": "integer",
      "minimum": 1,
      "maximum": 1000,
      "description": "返回的最大目录项数量"
    }
  },
  "required": ["path"],
  "additionalProperties": false
}`)

// Definition 返回 list_dir 的模型可见工具契约。
func Definition() toolcontracts.ToolDefinition {
	return toolcontracts.ToolDefinition{
		Name: toolcontracts.ToolListDir,
		Description: "在需要查看目录的直接子项或确认路径结构时调用；不要递归列目录。" +
			"输入 path，可选 offset 和 limit；相对路径按工作区解析。" +
			"返回按名称排序、包含 name 和 type 的 entries JSON 数组；内容未列完时提供 nextOffset。" +
			"仅支持已授权的目录，单次返回最多 50 KiB。",
		InputSchema: append(json.RawMessage(nil), inputSchema...),
	}
}
