// Package contracts 定义会话应用入口的公开数据。
package contracts

import (
	"time"

	agentcontracts "praxis/internal/app/agent/contracts"
)

// SessionSummary 是会话目录中的稳定展示数据。
type SessionSummary struct {
	ID          string
	ProjectID   string
	WorkspaceID string
	Title       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// SessionDetail 返回会话及其 Agent 摘要。
type SessionDetail struct {
	Session SessionSummary
	Agents  []agentcontracts.AgentDetail
}

// CreateSessionResult 返回已创建会话的关联身份。
type CreateSessionResult struct {
	SessionID    string
	ProjectID    string
	WorkspaceID  string
	AgentID      string
	DefinitionID string
}
