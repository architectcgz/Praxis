package entry

import (
	"praxis/internal/core/session/entry/role"
	"praxis/internal/identity"
	"time"
)

// Node 只保存历史结构、预览与上下文选择信息，不持有正文或文件位置。
// 子代理节点用于独立路径读取，不进入主会话历史树。
type Node struct {
	ID               string
	ParentID         string
	BranchID         identity.BranchID
	AgentID          identity.AgentID
	RunID            string
	TurnID           string
	RetainedEntryIDs []string
	Role             role.Role
	Summary          bool
	Run              bool
	PreviewText      string
	CreatedAt        time.Time
}

func NewNode(branchID identity.BranchID, agentID identity.AgentID, value ContextEntry) Node {
	node := Node{
		ID:          value.ID,
		ParentID:    value.ParentID,
		BranchID:    branchID,
		AgentID:     agentID,
		RunID:       value.RunID,
		TurnID:      value.TurnID,
		Role:        value.Role,
		Summary:     value.IsSummary(),
		Run:         value.IsRun(),
		PreviewText: value.Preview(),
		CreatedAt:   value.CreatedAt,
	}
	if value.Compression != nil {
		node.RetainedEntryIDs = value.Compression.RetainedEntryIDs
	}
	return node
}
