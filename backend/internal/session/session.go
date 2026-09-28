package session

import (
	"praxis/internal/contracts"

	"strings"
	"time"
)

type SessionState string

const (
	SessionActive   SessionState = "active"
	SessionArchived SessionState = "archived"
)

// Session is the durable collaboration container for a Project.
type Session struct {
	ID          contracts.SessionID
	ProjectID   contracts.ProjectID
	WorkspaceID contracts.WorkspaceID
	Title       string
	State       SessionState
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func NewSession(id contracts.SessionID, projectID contracts.ProjectID, workspaceID contracts.WorkspaceID, at time.Time) (Session, error) {
	session := Session{
		ID: id, ProjectID: projectID, WorkspaceID: workspaceID,
		State:     SessionActive,
		CreatedAt: at.UTC(), UpdatedAt: at.UTC(),
	}
	if err := session.Validate(); err != nil {
		return Session{}, err
	}
	return session, nil
}

func (s Session) Validate() error {
	if contracts.EmptyID(string(s.ID)) || contracts.EmptyID(string(s.ProjectID)) || contracts.EmptyID(string(s.WorkspaceID)) {
		return contracts.InvalidValue("session", "required reference is missing")
	}
	if s.State != SessionActive && s.State != SessionArchived {
		return contracts.InvalidValue("session.state", "unknown session state")
	}
	if s.CreatedAt.IsZero() || s.UpdatedAt.IsZero() || s.UpdatedAt.Before(s.CreatedAt) {
		return contracts.InvalidValue("session.timestamps", "timestamps are invalid")
	}
	return nil
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

func (s *Session) Archive(at time.Time) error {
	if s.State != SessionActive {
		return contracts.InvalidTransition("session", string(s.State), string(SessionArchived))
	}
	s.State, s.UpdatedAt = SessionArchived, at.UTC()
	return nil
}

func (s Session) Snapshot() Session { return s }
