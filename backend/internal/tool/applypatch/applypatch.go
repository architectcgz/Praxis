package applypatch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"praxis/internal/tool"
)

const inputSchema = `{
  "type": "object",
  "properties": {
    "patch": {
      "type": "string",
      "minLength": 1,
      "maxLength": %[1]d,
      "description": "使用 apply_patch 格式描述的文件补丁；maxLength 按字符计，实际 UTF-8 输入不得超过 %[1]d 字节"
    },
    "path": {
      "type": "string",
      "description": "补丁目标根目录，可以是绝对路径或相对于项目根目录的路径；省略时使用项目根目录"
    }
  },
  "required": ["patch"],
  "additionalProperties": false
}`

type patchTool struct {
	maxPatchBytes int
	schema        json.RawMessage
}

func New(maxPatchBytes int) tool.Tool {
	return patchTool{
		maxPatchBytes: maxPatchBytes,
		schema:        json.RawMessage(fmt.Sprintf(inputSchema, maxPatchBytes)),
	}
}

func (t patchTool) Definition() tool.ToolDefinition {
	return tool.ToolDefinition{
		Name: "apply_patch",
		Description: "在已授权的项目目录内应用一个或多个文件补丁。" +
			"patch 必须使用 *** Begin Patch、*** Update File、*** Add File、*** Delete File 和 *** End Patch 格式；" +
			"path 可选，省略时按项目根目录解析。所有目标文件会先完成格式、内容和写入范围校验，" +
			"任一目标无效时不会写入文件。",
		InputSchema: bytes.Clone(t.schema),
	}
}

type arguments struct {
	Patch      string `json:"patch"`
	Path       string `json:"path"`
	operations []operation
}

// NewArguments 提供业务默认补丁根目录，由编排层在此基础上解码显式输入。
func (patchTool) NewArguments() any {
	return &arguments{Path: "."}
}

func (a *arguments) PathArgument() *string { return &a.Path }

// Prepare 接收编排层已解析的根目录，只校验并解析补丁，不读写目标文件。
func (t patchTool) Prepare(input any, environment tool.Environment) (tool.PreparedCall, error) {
	decoded, ok := input.(*arguments)
	if !ok {
		return tool.PreparedCall{}, errors.New("tool argument type does not match the registered arguments")
	}
	args := *decoded
	if args.Patch == "" {
		return tool.PreparedCall{}, errors.New("apply_patch arguments are invalid: patch is required")
	}
	if len(args.Patch) > t.maxPatchBytes {
		return tool.PreparedCall{}, fmt.Errorf("apply_patch arguments are invalid: patch must not exceed %d UTF-8 bytes", t.maxPatchBytes)
	}
	operations, err := parse(args.Patch)
	if err != nil {
		return tool.PreparedCall{}, fmt.Errorf("apply_patch arguments are invalid: %s", err)
	}
	args.operations = operations
	return tool.PreparedCall{
		Arguments: args,
		Call: func(ctx context.Context) (tool.ToolResult, error) {
			return call(ctx, args, environment)
		},
	}, nil
}
