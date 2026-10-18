// Package tool 定义模型和工具执行共同使用的纯工具值类型。
package tool

type ToolName string

const (
	ToolReadFile   ToolName = "read_file"
	ToolBash       ToolName = "bash"
	ToolApplyPatch ToolName = "apply_patch"
)
