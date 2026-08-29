package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"praxis/internal/contracts"
	"praxis/internal/core/domain"
	"praxis/internal/core/orchestrate"
	"praxis/internal/storage"
)

func (a *App) CreateProject(
	request contracts.CreateProjectRequest,
) (response contracts.CreateProjectResponse, err error) {
	done := a.beginBinding("CreateProject")
	defer func() { done(err) }()
	ctx, service, err := a.bindingContext()
	if err != nil {
		return contracts.CreateProjectResponse{}, err
	}
	creator, ok := service.(projectCreatorService)
	if !ok {
		return contracts.CreateProjectResponse{}, bindingError(contracts.ErrorCodeBindingUnavailable)
	}
	workspaceKey, err := a.createProjectWorkspace(request.ProjectName)
	if err != nil {
		return contracts.CreateProjectResponse{}, err
	}
	result, err := creator.CreateProject(ctx, orchestrate.CreateProjectRequest{
		WorkspaceKey: workspaceKey,
		Goal:         request.Goal,
	})
	if err != nil {
		_ = os.Remove(workspaceKey)
		return contracts.CreateProjectResponse{}, publicBindingError(err)
	}
	return contracts.CreateProjectResponse{
		SessionID:    result.Session.ID.String(),
		AgentID:      result.Agent.ID.String(),
		Goal:         result.Session.Goal,
		WorkspaceKey: result.Session.WorkspaceKey,
	}, nil
}

func (a *App) CreateSession(
	request contracts.CreateSessionRequest,
) (response contracts.CreateSessionResponse, err error) {
	done := a.beginBinding("CreateSession")
	defer func() { done(err) }()
	ctx, service, err := a.bindingContext()
	if err != nil {
		return contracts.CreateSessionResponse{}, err
	}
	creator, ok := service.(sessionCreatorService)
	if !ok {
		return contracts.CreateSessionResponse{}, bindingError(contracts.ErrorCodeBindingUnavailable)
	}
	result, err := creator.CreateSessionForWorkspace(ctx, request.WorkspaceKey, request.Goal)
	if err != nil {
		return contracts.CreateSessionResponse{}, publicBindingError(err)
	}
	return contracts.CreateSessionResponse{
		SessionID:    result.Session.ID.String(),
		AgentID:      result.Agent.ID.String(),
		Goal:         result.Session.Goal,
		WorkspaceKey: result.Session.WorkspaceKey,
	}, nil
}

// createProjectWorkspace reserves a private project directory under the
// current user's Praxis root. The frontend never chooses a filesystem path.
func (a *App) createProjectWorkspace(projectName string) (string, error) {
	projectName = strings.TrimSpace(projectName)
	if !validProjectName(projectName) {
		return "", bindingError(contracts.ErrorCodeProjectWorkspaceInvalid)
	}
	a.mu.RLock()
	root := a.projectRoot
	a.mu.RUnlock()
	if root == "" {
		resolved, err := storage.ResolveDataRoot("")
		if err != nil {
			return "", bindingError(contracts.ErrorCodeInternal)
		}
		root = resolved.Projects
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", bindingError(contracts.ErrorCodeInternal)
	}
	workspaceKey := filepath.Join(root, projectName)
	if err := os.Mkdir(workspaceKey, 0o700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", bindingError(contracts.ErrorCodeWorkspaceConflict)
		}
		return "", bindingError(contracts.ErrorCodeInternal)
	}
	return workspaceKey, nil
}

func validProjectName(name string) bool {
	return name != "" && name != "." && name != ".." &&
		filepath.Clean(name) == name && !filepath.IsAbs(name) &&
		!strings.ContainsAny(name, "/\\\x00\r\n")
}

func (a *App) GetSession(sessionID string) (response contracts.SessionSnapshot, err error) {
	done := a.beginBinding("GetSession")
	defer func() { done(err) }()
	ctx, service, err := a.bindingContext()
	if err != nil {
		return contracts.SessionSnapshot{}, err
	}
	projection, err := service.ProjectSession(ctx, domain.SessionID(sessionID), 100)
	if err != nil {
		return contracts.SessionSnapshot{}, publicBindingError(err)
	}
	result := contracts.SessionSnapshot{
		ID:           projection.Session.ID.String(),
		Goal:         projection.Session.Goal,
		WorkspaceKey: projection.Session.WorkspaceKey,
		CreatedAt:    projection.Session.CreatedAt,
		UpdatedAt:    projection.Session.UpdatedAt,
		Groups:       make([]contracts.GroupSnapshot, 0, len(projection.Groups)),
		Agents:       make([]contracts.AgentSnapshot, 0, len(projection.Agents)),
	}
	for _, group := range projection.Groups {
		result.Groups = append(result.Groups, contracts.GroupSnapshot{
			ID:             group.ID.String(),
			PrimaryAgentID: group.PrimaryAgentID.String(),
			MaxConcurrent:  group.MaxConcurrent,
		})
	}
	for _, agent := range projection.Agents {
		result.Agents = append(result.Agents, contracts.AgentSnapshot{
			ID:               agent.ID.String(),
			SessionID:        agent.SessionID.String(),
			GroupID:          agent.GroupID.String(),
			Profile:          string(agent.Profile),
			State:            string(agent.State),
			CurrentExecution: agent.CurrentExecutionID.String(),
			Executions:       make([]contracts.ExecutionSnapshot, 0),
		})
	}
	return result, nil
}
