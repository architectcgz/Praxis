package readfile

import (
	"encoding/json"
	"praxis/internal/core/tool"
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
