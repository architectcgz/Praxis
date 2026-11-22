package applypatch

import (
	"encoding/json"
	"fmt"
	"praxis/internal/core/tool"
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
      "description": "补丁目标根目录，可以是绝对路径或相对于项目根目录的路径；省略时使用项目根目录"
    }
  },
  "required": ["patch"],
  "additionalProperties": false
}`)

// Definition 返回 apply_patch 的模型可见工具契约。
func Definition() tool.ToolDefinition {
	return tool.ToolDefinition{
		Name: "apply_patch",
		Description: "在已授权的项目目录内应用一个或多个文件补丁。" +
			"patch 必须使用 *** Begin Patch、*** Update File、*** Add File、*** Delete File 和 *** End Patch 格式；" +
			"path 可选，省略时按项目根目录解析。所有目标文件会先完成格式、内容和写入范围校验，" +
			"任一目标无效时不会写入文件。",
		InputSchema: append(json.RawMessage(nil), inputSchema...),
	}
}

// Arguments 保存补丁及根目录，路径由调用编排统一解析。
type Arguments struct {
	Patch      string      `json:"patch"`
	Path       string      `json:"path"`
	Operations []Operation `json:"-"`
}

// NewArguments 提供业务默认补丁根目录，由编排层在此基础上解码显式输入。
func NewArguments() Arguments {
	return Arguments{Path: "."}
}

func (a *Arguments) PathArgument() *string { return &a.Path }

// Prepare 接收编排层已解析的根目录，只校验并解析补丁，不读写目标文件。
func Prepare(args Arguments) (Arguments, error) {
	invalidArguments := func(reason string) (Arguments, error) {
		return Arguments{}, fmt.Errorf("apply_patch arguments are invalid: %s", reason)
	}
	if args.Patch == "" {
		return invalidArguments("patch is required")
	}
	if len(args.Patch) > maxPatchBytes {
		return invalidArguments("patch is too large")
	}
	operations, err := Parse(args.Patch)
	if err != nil {
		return invalidArguments(err.Error())
	}
	args.Operations = operations
	return args, nil
}
