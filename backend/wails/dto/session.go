package dto

import (
	"time"

	sessionmodel "praxis/internal/core/session"
	agentservice "praxis/internal/service/agent"
)

type SessionSnapshot struct {
	BindingID string                   `json:"bindingId"`
	Branch    sessionmodel.Branch      `json:"branch"`
	ID        string                   `json:"id"`
	ProjectID string                   `json:"projectId"`
	Title     string                   `json:"title"`
	CreatedAt time.Time                `json:"createdAt"`
	UpdatedAt time.Time                `json:"updatedAt"`
	Agents    []agentservice.AgentView `json:"agents"`
	Tasks     []QueuedTask             `json:"tasks"`
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
