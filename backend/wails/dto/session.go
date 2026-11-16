package dto

import (
	"time"
)

type SessionSnapshot struct {
	QueuePaused bool                   `json:"queuePaused"`
	ID          string                 `json:"id"`
	ProjectID   string                 `json:"projectId"`
	Title       string                 `json:"title"`
	CreatedAt   time.Time              `json:"createdAt"`
	UpdatedAt   time.Time              `json:"updatedAt"`
	Agents      []AgentView            `json:"agents"`
	Tasks       []QueuedTask           `json:"tasks"`
	Entries     map[string]HistoryPage `json:"entries"`
}

type SessionSummary struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"projectId"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type CreateSessionRequest struct {
	SessionID string `json:"sessionId" validate:"session_id"`
	ProjectID string `json:"projectId" validate:"project_id"`
	RequestID string `json:"requestId" validate:"id"`
}

type CreateSessionResponse struct {
	SessionID string `json:"sessionId"`
	ProjectID string `json:"projectId"`
}
