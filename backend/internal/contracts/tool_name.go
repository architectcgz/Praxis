package contracts

type ToolName string

const (
	ToolReadFile   ToolName = "read_file"
	ToolBash       ToolName = "bash"
	ToolApplyPatch ToolName = "apply_patch"
)
