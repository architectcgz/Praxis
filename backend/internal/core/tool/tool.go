// Package tool 定义模型与工具执行共同使用的工具协议、注册目录与结果边界。
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"praxis/internal/identity"
)

type ToolName string

// MaxInlineResultBytes 限制内联结果的 UTF-8 字节数，超出部分不截断，完整结果转存临时文件。
const MaxInlineResultBytes = 50 * 1024

// ToolDefinition 是 Provider 中立的模型可见工具定义。
type ToolDefinition struct {
	Name        ToolName
	Description string
	InputSchema json.RawMessage
}

// CallKey 用执行身份和 Provider 调用 ID 定位同一次工具调用。
type CallKey struct {
	RunID  identity.RunID
	CallID string
}

func (k CallKey) String() string {
	return fmt.Sprintf("%s:%d:%s", k.RunID, len(k.CallID), k.CallID)
}

// ToolResult 是工具执行后交给本次执行实例和模型调用服务的结果。
type ToolResult struct {
	Payload    string
	ErrorClass string
	SideEffect bool
	Truncated  bool
}

// NewToolSuccess 创建成功的工具结果，payload 由具体工具负责生成。
func NewToolSuccess(payload string, truncated bool) ToolResult {
	return ToolResult{
		Payload:   payload,
		Truncated: truncated,
	}
}

// NewToolError 创建统一格式的工具错误结果，仅作为工具结果返回给模型。
func NewToolError(code, message string) ToolResult {
	encoded, _ := json.Marshal(struct {
		Error   string `json:"error"`
		Message string `json:"message,omitempty"`
	}{
		Error:   code,
		Message: message,
	})
	return ToolResult{
		Payload:    string(encoded),
		ErrorClass: code,
	}
}

// Tool 提供输入类型和参数准备能力，资源访问约束由具体工具在副作用边界检查。
type Tool interface {
	// Definition 返回独立副本，InputSchema 不与工具内部共享可变数据。
	Definition() ToolDefinition
	// NewArguments 根据工具业务规则提供默认值，返回独立的非 nil 结构体指针，供编排层解码显式输入。
	NewArguments() any
	// Prepare 接收已解码且路径已规范化的参数，校验业务规则并绑定文件访问范围；不得产生副作用或修改共享实例。
	Prepare(arguments any, environment Environment) (PreparedCall, error)
}

// Environment 是本次工具执行的文件访问范围；临时输出目录只属于当前会话。
type Environment struct {
	ProjectPath     string
	OutputDirectory string
}

// PathArguments 声明需要按项目目录解析的路径字段，具体解析和校验由调用编排负责。
// PathArgument 返回本次参数对象中路径字段的非 nil 指针，不返回共享工具状态。
type PathArguments interface {
	PathArgument() *string
}

// PreparedCall 绑定本次已准备的参数和实际调用，不保存共享工具状态。
// Arguments 必须可被 JSON 序列化，与 Call 捕获的参数一致且在准备后保持只读，仅供审批和 Hook 使用。
// Call 必须非 nil，只能在授权通过后执行，不重新解码或准备参数。
type PreparedCall struct {
	Arguments any
	Call      func(context.Context) (ToolResult, error)
}

// ToolCatalog 提供当前启用的工具定义和实现。
type ToolCatalog interface {
	List() []ToolDefinition
	Get(ToolName) (Tool, bool)
}

// ReadOnlyTool 是工具实现对副作用边界的明确声明，未知扩展默认串行执行。
type ReadOnlyTool interface{ ReadOnly() bool }

func IsReadOnly(catalog ToolCatalog, name ToolName) bool {
	t, ok := catalog.Get(name)
	if !ok {
		return false
	}
	capability, ok := t.(ReadOnlyTool)
	return ok && capability.ReadOnly()
}
