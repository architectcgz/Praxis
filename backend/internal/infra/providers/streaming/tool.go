package streaming

import (
	"bytes"
	"encoding/json"
	"errors"
	"praxis/internal/contracts"

	"praxis/internal/core/model"
)

// ToolAccumulator 用于累积流式响应中的工具调用分片。
type ToolAccumulator struct {
	ID        string
	Name      string
	Arguments string
}

// EmitTool 将累积完成的工具调用转换为统一的模型流事件并发送。
// 缺少工具名时返回协议错误，避免丢弃未完成调用后将响应误判为自然完成；空参数使用空 JSON 对象。
func EmitTool(emit EmitFunc, call *ToolAccumulator) error {
	if call == nil || call.Name == "" {
		return errors.New("provider emitted an incomplete tool call")
	}
	args := json.RawMessage(call.Arguments)
	if len(bytes.TrimSpace(args)) == 0 {
		args = json.RawMessage(`{}`)
	}
	return emit(model.ModelStreamEvent{
		Kind: model.StreamToolCall,
		ToolCall: contracts.ToolCall{
			ID:        call.ID,
			Name:      contracts.ToolName(call.Name),
			Arguments: bytes.Clone(args),
		},
	})
}
