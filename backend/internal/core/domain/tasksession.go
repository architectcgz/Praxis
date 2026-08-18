package domain

import (
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type TaskSessionState string

const (
	TaskSessionActive   TaskSessionState = "active"
	TaskSessionArchived TaskSessionState = "archived"
)

type TaskSession struct {
	ID           TaskSessionID
	Goal         string
	WorkspaceKey string
	ThreadIDs    []AgentThreadID
	State        TaskSessionState
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func NewTaskSession(id TaskSessionID, goal, workspaceKey string, at time.Time) (TaskSession, error) {
	session := TaskSession{
		ID:           id,
		Goal:         strings.TrimSpace(goal),
		WorkspaceKey: filepath.Clean(strings.TrimSpace(workspaceKey)),
		State:        TaskSessionActive,
		CreatedAt:    at.UTC(),
		UpdatedAt:    at.UTC(),
	}
	if err := session.Validate(); err != nil {
		return TaskSession{}, err
	}
	return session, nil
}

func (s TaskSession) Validate() error {
	if idIsEmpty(string(s.ID)) {
		return invalidValue("taskSession.id", "id is required")
	}
	if s.Goal == "" {
		return invalidValue("taskSession.goal", "goal is required")
	}
	if s.WorkspaceKey == "." || !filepath.IsAbs(s.WorkspaceKey) {
		return invalidValue("taskSession.workspaceKey", "workspace key must be an absolute path")
	}
	if s.State != TaskSessionActive && s.State != TaskSessionArchived {
		return invalidValue("taskSession.state", "unknown task session state")
	}
	if s.CreatedAt.IsZero() || s.UpdatedAt.IsZero() {
		return invalidValue("taskSession.timestamps", "timestamps are required")
	}
	return nil
}

func (s *TaskSession) AddThread(threadID AgentThreadID, at time.Time) error {
	if s.State != TaskSessionActive {
		return invalidTransition("taskSession", string(s.State), "thread_added")
	}
	if idIsEmpty(string(threadID)) {
		return invalidValue("taskSession.threadIDs", "thread id is required")
	}
	if slices.Contains(s.ThreadIDs, threadID) {
		return invalidValue("taskSession.threadIDs", "duplicate thread")
	}
	s.ThreadIDs = append(s.ThreadIDs, threadID)
	s.UpdatedAt = at.UTC()
	return nil
}

func (s *TaskSession) Archive(at time.Time) error {
	if s.State != TaskSessionActive {
		return invalidTransition("taskSession", string(s.State), string(TaskSessionArchived))
	}
	s.State = TaskSessionArchived
	s.UpdatedAt = at.UTC()
	return nil
}

func (s TaskSession) Snapshot() TaskSession {
	copy := s
	copy.ThreadIDs = append([]AgentThreadID(nil), s.ThreadIDs...)
	return copy
}
