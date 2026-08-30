package domain

import (
	"strings"
	"time"
)

type SessionState string

const (
	SessionActive   SessionState = "active"
	SessionArchived SessionState = "archived"
)

// Session is the durable top-level collaboration container. It never owns a
// transcript directly; every transcript belongs to one Agent in a group.
type Session struct {
	ID          SessionID
	ProjectID   ProjectID
	WorkspaceID WorkspaceID
	Goal        string
	State       SessionState
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func NewSession(
	id SessionID,
	projectID ProjectID,
	workspaceID WorkspaceID,
	goal string,
	at time.Time,
) (Session, error) {
	session := Session{
		ID:          id,
		ProjectID:   projectID,
		WorkspaceID: workspaceID,
		Goal:        strings.TrimSpace(goal),
		State:       SessionActive,
		CreatedAt:   at.UTC(),
		UpdatedAt:   at.UTC(),
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
	s.State = SessionArchived
	s.UpdatedAt = at.UTC()
	return nil
}

func (s Session) Snapshot() Session { return s }

// AgentGroup describes an explicit membership and concurrency boundary inside
// one Session. PrimaryAgentID is assigned after the group's first Agent exists.
type AgentGroup struct {
	ID             AgentGroupID
	SessionID      SessionID
	PrimaryAgentID AgentID
	MaxConcurrent  int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func NewAgentGroup(id AgentGroupID, sessionID SessionID, maxConcurrent int, at time.Time) (AgentGroup, error) {
	group := AgentGroup{
		ID:            id,
		SessionID:     sessionID,
		MaxConcurrent: maxConcurrent,
		CreatedAt:     at.UTC(),
		UpdatedAt:     at.UTC(),
	}
	if err := group.Validate(); err != nil {
		return AgentGroup{}, err
	}
	return group, nil
}

func (g AgentGroup) Validate() error {
	if idIsEmpty(string(g.ID)) || idIsEmpty(string(g.SessionID)) {
		return invalidValue("agentGroup", "required reference is missing")
	}
	if g.MaxConcurrent < 1 {
		return invalidValue("agentGroup.maxConcurrent", "concurrency limit must be positive")
	}
	if g.CreatedAt.IsZero() || g.UpdatedAt.IsZero() || g.UpdatedAt.Before(g.CreatedAt) {
		return invalidValue("agentGroup.timestamps", "timestamps are invalid")
	}
	return nil
}

func (g *AgentGroup) SetPrimary(agentID AgentID, at time.Time) error {
	if idIsEmpty(string(agentID)) {
		return invalidValue("agentGroup.primaryAgentID", "agent id is required")
	}
	g.PrimaryAgentID = agentID
	g.UpdatedAt = at.UTC()
	return nil
}
