package entry

import (
	"bytes"
	"encoding/json"
	"praxis/internal/core/tool"
	"praxis/internal/core/toolexecution"
	"praxis/internal/identity"
	"time"
)

// ToolCall 是已提交 Assistant 中的完整调用及执行归属，不另行持久化。
type ToolCall struct {
	toolexecution.Key
	SessionID identity.SessionID
	AgentID   identity.AgentID
	RequestID string
	TurnID    identity.TurnID
	Name      tool.ToolName
	Arguments json.RawMessage
	CreatedAt time.Time
}

// ToolCalls 从已提交的 Assistant 提取调用，正常执行和派生查询共用此身份映射。
func (m ContextEntry) ToolCalls(sessionID identity.SessionID, agentID identity.AgentID) []ToolCall {
	calls := make([]ToolCall, 0)
	for _, block := range m.Content {
		if block.Kind != BlockToolCall {
			continue
		}
		calls = append(calls, ToolCall{
			Key:       toolexecution.Key{ExecutionID: identity.AgentExecutionID(m.ExecutionID), CallID: block.CallID},
			SessionID: sessionID,
			AgentID:   agentID,
			RequestID: m.RequestID,
			TurnID:    identity.TurnID(m.TurnID),
			Name:      tool.ToolName(block.Name),
			Arguments: bytes.Clone(block.Input),
			CreatedAt: m.CreatedAt,
		})
	}
	return calls
}
