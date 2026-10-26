package dto

type CreateProjectRequest struct {
	ProjectID   string `json:"projectId" validate:"project_id"`
	WorkspaceID string `json:"workspaceId" validate:"id"`
	ProjectName string `json:"projectName" validate:"project_name"`
	Path        string `json:"path" validate:"project_path"`
	RequestID   string `json:"requestId" validate:"id"`
}

type CreateProjectResponse struct {
	ProjectID   string `json:"projectId"`
	Name        string `json:"name"`
	WorkspaceID string `json:"workspaceId"`
	Path        string `json:"path"`
}

type ProjectSummary struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	DefaultWorkspaceID string `json:"defaultWorkspaceId"`
	Path               string `json:"path"`
	State              string `json:"state"`
}
