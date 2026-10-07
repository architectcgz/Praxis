package applypatch

import (
	"encoding/json"

	"praxis/internal/contracts"
	toolcontracts "praxis/internal/tools/contracts"
)

const maxPatchBytes = 512 * 1024

var inputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "patch": {
      "type": "string",
      "minLength": 1,
      "maxLength": 524288,
      "description": "使用 apply_patch 格式描述的文件补丁"
    },
    "path": {
      "type": "string",
      "description": "补丁工作区根目录，可以是绝对路径或相对于执行工作区的路径；省略时使用执行工作区"
    }
  },
  "required": ["patch"],
  "additionalProperties": false
}`)

// Definition 返回 apply_patch 的模型可见工具契约。
func Definition() contracts.ToolDefinition {
	return contracts.ToolDefinition{
		Name: toolcontracts.ToolApplyPatch,
		Description: "在已授权的工作区内应用一个或多个文件补丁。" +
			"patch 必须使用 *** Begin Patch、*** Update File、*** Add File、*** Delete File 和 *** End Patch 格式；" +
			"path 可选，省略时按执行工作区解析。所有目标文件会先完成格式、内容和写入范围校验，" +
			"任一目标无效时不会写入文件。",
		InputSchema: append(json.RawMessage(nil), inputSchema...),
	}
}
