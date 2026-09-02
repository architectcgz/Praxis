package session

import (
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
	ID          SessionID
	ProjectID   ProjectID
	WorkspaceID WorkspaceID
	Goal        string
	State       SessionState
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func NewSession(id SessionID, projectID ProjectID, workspaceID WorkspaceID, goal string, at time.Time) (Session, error) {
	session := Session{
		ID: id, ProjectID: projectID, WorkspaceID: workspaceID,
		Goal: strings.TrimSpace(goal), State: SessionActive,
		CreatedAt: at.UTC(), UpdatedAt: at.UTC(),
	}
	if err := session.Validate(); err != nil {
		return Session{}, err
	}
	return session, nil
}

func (s Session) Validate() error {
	if idIsEmpty(string(s.ID)) || idIsEmpty(string(s.ProjectID)) || idIsEmpty(string(s.WorkspaceID)) {
		return invalidValue("session", "required reference is missing")
	}
	if s.Goal == "" {
		return invalidValue("session.goal", "goal is required")
	}
	if s.State != SessionActive && s.State != SessionArchived {
		return invalidValue("session.state", "unknown session state")
	}
	if s.CreatedAt.IsZero() || s.UpdatedAt.IsZero() || s.UpdatedAt.Before(s.CreatedAt) {
		return invalidValue("session.timestamps", "timestamps are invalid")
	}
	return nil
}

func (s *Session) Archive(at time.Time) error {
	if s.State != SessionActive {
		return invalidTransition("session", string(s.State), string(SessionArchived))
	}
	s.State, s.UpdatedAt = SessionArchived, at.UTC()
	return nil
}

func (s Session) Snapshot() Session { return s }
