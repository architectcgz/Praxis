package tool

import "context"

// Tool 提供输入类型和参数准备能力，资源访问约束由具体工具在副作用边界检查。
type Tool interface {
	// Definition 返回独立副本，InputSchema 不与工具内部共享可变数据。
	Definition() ToolDefinition
	// NewArguments 根据工具业务规则提供默认值，返回独立的非 nil 结构体指针，供编排层解码显式输入。
	NewArguments() any
	// Prepare 接收已解码且路径已规范化的参数，校验业务规则并绑定执行工作区；不得产生副作用或修改共享实例。
	Prepare(arguments any, workspacePath string) (PreparedCall, error)
}

// PathArguments 声明需要按工作区解析的路径字段，具体解析和校验由调用编排负责。
// PathArgument 返回本次参数对象中路径字段的非 nil 指针，不返回共享工具状态。
type PathArguments interface {
	PathArgument() *string
}

// PreparedCall 绑定本次已准备的参数、待匹配命令和实际调用，不保存共享工具状态。
// Arguments 必须可被 JSON 序列化，与 Call 捕获的参数一致且在准备后保持只读，仅供审批和 Hook 使用。
// Command 为空表示不匹配命令规则；Call 必须非 nil，只能在授权通过后执行，不重新解码或准备参数。
type PreparedCall struct {
	Arguments any
	Command   string
	Call      func(context.Context) (ToolResult, error)
}

// ToolCatalog 提供当前启用的工具定义和实现。
type ToolCatalog interface {
	List() []ToolDefinition
	Get(ToolName) (Tool, bool)
}
