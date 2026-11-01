package entry

import (
	"bytes"
	"encoding/json"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
)

// Key 复用工具契约的调用身份。
type Key = tool.CallKey

// ToolCall 是工具调用及执行归属的内存表示，调用信息随 Assistant Entry 持久化。
type ToolCall struct {
	Key
	SessionID identity.SessionID
	AgentID   identity.AgentID
	RequestID string
	TurnID    identity.TurnID
	Name      tool.ToolName
	Arguments json.RawMessage
}

// ToolCalls 从 Assistant Entry 取得调用，供执行查询与启动恢复使用。
func (m ContextEntry) ToolCalls(sessionID identity.SessionID, agentID identity.AgentID) []ToolCall {
	calls := make([]ToolCall, 0)
	for _, block := range m.Content {
		if block.Kind != BlockToolCall {
			continue
		}
		calls = append(calls, ToolCall{
			Key:       Key{ExecutionID: identity.AgentExecutionID(m.ExecutionID), CallID: block.CallID},
			SessionID: sessionID,
			AgentID:   agentID,
			RequestID: m.RequestID,
			TurnID:    identity.TurnID(m.TurnID),
			Name:      tool.ToolName(block.Name),
			Arguments: bytes.Clone(block.Input),
		})
	}
	return calls
}

// ResultEntry 为正常结果和中断回执使用相同的确定性身份及调用归属，不分配历史序号或时间。
func (c ToolCall) ResultEntry(result tool.ToolResult) ContextEntry {
	if len(result.Payload) > tool.MaxInlineResultBytes {
		result = tool.NewToolError("tool_execution_failed", "Tool result exceeds the inline size limit.")
	}
	if result.Payload == "" {
		result.Payload = "(empty tool result)"
	}
	return ContextEntry{
		ID:          "tool-result:" + c.ExecutionID.String() + ":" + c.CallID,
		RequestID:   c.RequestID,
		ExecutionID: c.ExecutionID.String(),
		TurnID:      c.TurnID.String(),
		Role:        role.Tool,
		AuthorKind:  AuthorTool,
		AuthorID:    string(c.Name),
		Content: []Block{{
			Kind:    BlockToolResult,
			CallID:  c.CallID,
			Name:    string(c.Name),
			Text:    result.Payload,
			IsError: result.ErrorClass != "",
		}},
	}
}
