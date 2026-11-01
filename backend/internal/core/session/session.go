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

// SessionInfo 是 Project 的持久化协作容器。
type SessionInfo struct {
	ID           identity.SessionID
	ProjectID    identity.ProjectID
	Title        string
	State        SessionState
	ApprovalMode *security.ApprovalMode `json:",omitempty"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// NewSession 使用入口已校验的身份创建 active 会话，初始标题为空。
func NewSessionInfo(id identity.SessionID, projectID identity.ProjectID, at time.Time) SessionInfo {
	return SessionInfo{
		ID:        id,
		ProjectID: projectID,
		State:     SessionActive,
		CreatedAt: at.UTC(),
		UpdatedAt: at.UTC(),
	}
}

// MainAgentID 是主会话入口的稳定身份；运行实例由各次 Execution 分别创建和持有。
func (s SessionInfo) MainAgentID() identity.AgentID {
	return identity.AgentID(s.ID)
}

// TitleFromInput 合并输入中的空白并提取短标题；空白输入返回空字符串。
// 截断以 Unicode 字符为单位，避免拆断多字节文本。
func TitleFromInput(content string) string {
	words := []rune(strings.Join(strings.Fields(content), " "))
	if len(words) > 40 {
		words = words[:40]
	}
	return string(words)
}

// Validate 检查对象自身的完整性，不修改或修复输入。
func (value SessionInfo) Validate() error {
	if !value.ID.Valid() || !value.ProjectID.Valid() {
		return fmt.Errorf("session: invalid session or project identity: %w", errorcode.ErrInvalidValue)
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
