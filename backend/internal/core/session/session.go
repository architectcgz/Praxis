package session

import (
	"fmt"
	"strings"
	"time"

	"praxis/internal/core/security"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

type SessionState string

const (
	SessionActive   SessionState = "active"
	SessionArchived SessionState = "archived"
)

// Session 是 Project 的持久化协作容器。
type Session struct {
	ID           identity.SessionID
	ProjectID    identity.ProjectID
	WorkspaceID  identity.WorkspaceID
	Title        string
	State        SessionState
	ApprovalMode *security.ApprovalMode `json:",omitempty"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// NewSession 使用入口已校验的身份创建 active 会话，初始标题为空。
func NewSession(id identity.SessionID, projectID identity.ProjectID, workspaceID identity.WorkspaceID, at time.Time) Session {
	return Session{
		ID:          id,
		ProjectID:   projectID,
		WorkspaceID: workspaceID,
		State:       SessionActive,
		CreatedAt:   at.UTC(),
		UpdatedAt:   at.UTC(),
	}
}

// MainAgentID 是主会话入口的稳定身份；运行实例由各次 Execution 分别创建和持有。
func (s Session) MainAgentID() identity.AgentID {
	return identity.AgentID(s.ID)
}

// NameFromInput 从首次成功回答对应的用户输入提取短标题；已有标题不会被后续输入覆盖。
// 截断以 Unicode 字符为单位，避免拆断多字节文本。
func (s *Session) NameFromInput(content string, at time.Time) {
	if s.Title != "" {
		return
	}
	words := []rune(strings.Join(strings.Fields(content), " "))
	if len(words) == 0 {
		return
	}
	if len(words) > 40 {
		words = words[:40]
	}
	s.Title = string(words)
	s.UpdatedAt = at.UTC()
}

// Validate 检查对象自身的完整性，不修改或修复输入。
func (value Session) Validate() error {
	if !value.ID.Valid() || !value.ProjectID.Valid() {
		return fmt.Errorf("session: invalid session or project identity: %w", errorcode.ErrInvalidValue)
	}
	if value.WorkspaceID == "" || value.WorkspaceID.String() != strings.TrimSpace(value.WorkspaceID.String()) {
		return fmt.Errorf("session.workspaceID: required reference must be canonical and nonempty: %w", errorcode.ErrInvalidValue)
	}
	if value.State != SessionActive && value.State != SessionArchived {
		return fmt.Errorf("session.state: unknown session state: %w", errorcode.ErrInvalidValue)
	}
	if value.ApprovalMode != nil && !value.ApprovalMode.ValidDefault() {
		return fmt.Errorf("session.approvalMode: default approval mode must be ask, auto or yolo: %w", errorcode.ErrInvalidValue)
	}
	if value.CreatedAt.IsZero() || value.UpdatedAt.IsZero() || value.UpdatedAt.Before(value.CreatedAt) {
		return fmt.Errorf("session.timestamps: timestamps are invalid: %w", errorcode.ErrInvalidValue)
	}
	return nil
}
