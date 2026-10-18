package streaming

import (
	"bytes"
	"encoding/json"
	"errors"
	modelapi "praxis/internal/core/model"
	"praxis/internal/core/tool"
	"strings"
)

// ToolAccumulator 用于累积流式响应中的工具调用分片。
type ToolAccumulator struct {
	ID        string
	Name      string
	Arguments string
}

// ValidateToolCall 校验完整调用的 canonical 身份和 JSON 对象；不修补缺失参数。
// 流式分片允许暂时无效，只有协议确认调用完整后才执行本校验。
func ValidateToolCall(call *ToolAccumulator) error {
	if call == nil || call.ID == "" || call.Name == "" || call.ID != strings.TrimSpace(call.ID) || call.Name != strings.TrimSpace(call.Name) {
		return errors.New("provider emitted an incomplete or non-canonical tool identity")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(call.Arguments), &object); err != nil || object == nil {
		return errors.New("provider tool arguments must be a complete JSON object")
	}
	return nil
}

// EmitTool 复制并发送已完成的调用；调用方须先用 ValidateToolCall 校验整批工具。
// 此处仅返回 emit 的错误；Schema 和授权由工具执行边界负责。
func EmitTool(emit EmitFunc, call *ToolAccumulator) error {
	args := json.RawMessage(call.Arguments)
	return emit(modelapi.ModelStreamEvent{
		Kind: modelapi.StreamToolCall,
		ToolCall: tool.ToolCall{
			ID:        call.ID,
			Name:      tool.ToolName(call.Name),
			Arguments: bytes.Clone(args),
		},
	})
}
