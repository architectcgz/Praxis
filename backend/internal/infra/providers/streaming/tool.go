package streaming

import (
	"praxis/internal/contracts"
	toolcontracts "praxis/internal/tools/contracts"

	"bytes"
	"encoding/json"

	runtimecontract "praxis/internal/agent_runtime"
)

// ToolAccumulator 用于累积流式响应中的工具调用分片。
type ToolAccumulator struct {
	ID        string
	Name      string
	Arguments string
}

// EmitTool 将累积完成的工具调用转换为统一的模型流事件并发送。
// 缺少工具名时跳过，缺少或仅包含空白字符的参数时使用空 JSON 对象。
func EmitTool(emit EmitFunc, call *ToolAccumulator) error {
	if call == nil || call.Name == "" {
		return nil
	}
	args := json.RawMessage(call.Arguments)
	if len(bytes.TrimSpace(args)) == 0 {
		args = json.RawMessage(`{}`)
	}
	return emit(runtimecontract.ModelStreamEvent{
		Kind: runtimecontract.StreamToolCall,
		ToolCall: toolcontracts.ToolCall{
			ID:        call.ID,
			Name:      contracts.ToolName(call.Name),
			Arguments: bytes.Clone(args),
		},
	})
}
