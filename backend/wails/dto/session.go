package dto

import (
	"time"

	agentservice "praxis/internal/service/agent"
)

type SessionSnapshot struct {
	ID          string                   `json:"id"`
	ProjectID   string                   `json:"projectId"`
	WorkspaceID string                   `json:"workspaceId"`
	Title       string                   `json:"title"`
	CreatedAt   time.Time                `json:"createdAt"`
	UpdatedAt   time.Time                `json:"updatedAt"`
	Agents      []agentservice.AgentView `json:"agents"`
	Tasks       []QueuedTask             `json:"tasks"`
}

type SessionSummary struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"projectId"`
	WorkspaceID string    `json:"workspaceId"`
	Title       string    `json:"title"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type CreateSessionRequest struct {
	SessionID   string `json:"sessionId" validate:"session_id"`
	ProjectID   string `json:"projectId" validate:"project_id"`
	WorkspaceID string `json:"workspaceId" validate:"id"`
	RequestID   string `json:"requestId" validate:"id"`
}

type CreateSessionResponse struct {
	SessionID   string `json:"sessionId"`
	ProjectID   string `json:"projectId"`
	WorkspaceID string `json:"workspaceId"`
}
