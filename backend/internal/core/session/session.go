package session

import (
	"praxis/internal/identity"
	"strings"
	"time"
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
	Title       string
	State       SessionState
	CreatedAt   time.Time
	UpdatedAt   time.Time
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
