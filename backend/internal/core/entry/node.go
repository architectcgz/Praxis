package entry

import (
	"praxis/internal/core/entry/role"
	"praxis/internal/identity"
	"time"
)

// Node 只保存历史结构、预览与上下文选择信息，不持有正文或文件位置。
// 子代理节点用于独立路径读取，不进入主会话历史。
type Node struct {
	ID           string
	AgentID      identity.AgentID
	RunID        string
	TurnID       string
	Role         role.Role
	Summary      bool
	Run          bool
	Visible      bool
	Metadata     bool
	MessageID    string
	HasToolCalls bool
	PreviewText  string
	CreatedAt    time.Time
}

func NewNode(agentID identity.AgentID, value ContextEntry) Node {
	node := Node{
		ID:          value.ID,
		AgentID:     agentID,
		RunID:       value.RunID,
		TurnID:      value.TurnID,
		Role:        value.Role,
		Summary:     value.IsSummary(),
		Run:         value.IsRun(),
		Metadata:    value.IsMetadata(),
		Visible:     !value.IsMetadata() || value.ExecutionResult() != nil,
		MessageID:   value.InputMessageID(),
		PreviewText: value.Preview(),
		CreatedAt:   value.CreatedAt,
	}

	if request, _ := value.RequestFact(); request != nil && (request.Status == RequestFailed || request.Status == RequestInterrupted) {
		node.Visible = true
	}
	for _, block := range value.Content {
		if _, ok := block.(ToolCallBlock); ok {
			node.HasToolCalls = true
			break
		}
	}
	return node
}
