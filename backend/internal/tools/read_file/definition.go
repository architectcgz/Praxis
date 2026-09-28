package readfile

import (
	"encoding/json"

	toolcontracts "praxis/internal/tools/contracts"
)

var inputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "普通文件路径，可以是绝对路径或相对于执行工作区的路径"
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
func Definition() toolcontracts.ToolDefinition {
	return toolcontracts.ToolDefinition{
		Name: toolcontracts.ToolReadFile,
		Description: "在需要读取工作区内已知 UTF-8 普通文件内容时调用；不要用它列目录。" +
			"输入 path，可选 offset 和 limit；相对路径按工作区解析。" +
			"返回包含文件定位信息和 content 的 JSON 对象；内容未读完时提供 nextOffset。" +
			"仅支持已授权的普通文件，单次返回最多 50 KiB。",
		InputSchema: append(json.RawMessage(nil), inputSchema...),
	}
}
