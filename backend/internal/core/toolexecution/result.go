package toolexecution

import "praxis/internal/core/tool"

// Result 仅传递本次运行结果，不保存完成状态。
type Result struct {
	ToolResult tool.ToolResult
	Err        error
}
