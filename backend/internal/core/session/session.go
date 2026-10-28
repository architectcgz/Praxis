package session

import (
	"fmt"
	"regexp"
	"strings"
	"time"

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
	ID          identity.SessionID
	ProjectID   identity.ProjectID
	WorkspaceID identity.WorkspaceID
	// 这里只保存主实例的稳定身份与定义引用，不保存 Agent 对象或运行状态。
	MainAgentID       identity.AgentID
	AgentDefinitionID identity.AgentDefinitionID
	Title             string
	State             SessionState
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// NewSession 使用入口已校验的身份创建 active 会话，初始标题为空。
func NewSession(id identity.SessionID, projectID identity.ProjectID, workspaceID identity.WorkspaceID, agentID identity.AgentID, definitionID identity.AgentDefinitionID, at time.Time) Session {
	return Session{
		ID:                id,
		ProjectID:         projectID,
		WorkspaceID:       workspaceID,
		MainAgentID:       agentID,
		AgentDefinitionID: definitionID,
		State:             SessionActive,
		CreatedAt:         at.UTC(),
		UpdatedAt:         at.UTC(),
	}
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
	for _, id := range []string{
		value.WorkspaceID.String(),
		value.MainAgentID.String(),
	} {
		if id == "" || id != strings.TrimSpace(id) {
			return fmt.Errorf("session: required reference must be canonical and nonempty: %w", errorcode.ErrInvalidValue)
		}
	}
	if !definitionIDPattern.MatchString(value.AgentDefinitionID.String()) {
		return fmt.Errorf("session.agentDefinitionID: invalid agent definition identity: %w", errorcode.ErrInvalidValue)
	}
	if value.State != SessionActive && value.State != SessionArchived {
		return fmt.Errorf("session.state: unknown session state: %w", errorcode.ErrInvalidValue)
	}
	if value.CreatedAt.IsZero() || value.UpdatedAt.IsZero() || value.UpdatedAt.Before(value.CreatedAt) {
		return fmt.Errorf("session.timestamps: timestamps are invalid: %w", errorcode.ErrInvalidValue)
	}
	return nil
}

var definitionIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
