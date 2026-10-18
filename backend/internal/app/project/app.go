// Package project 提供项目应用入口和公开结果投影。
package project

import (
	"context"

	projectcontracts "praxis/internal/app/project/contracts"
	"praxis/internal/core/identity"
	"praxis/internal/request"
	businessproject "praxis/internal/service/project"
)

// App 适配项目业务，不拥有持久化状态。
type App struct {
	projects *businessproject.Service
}

// New 连接项目业务；依赖由组合根统一装配。
func New(projects *businessproject.Service) *App {
	return &App{projects: projects}
}

// ListProjects 返回项目目录；负数分页返回输入错误，零值使用应用默认值。
func (a *App) ListProjects(ctx context.Context, limit int) ([]projectcontracts.ProjectSummary, error) {
	limit, err := request.ListLimit(limit)
	if err != nil {
		return nil, err
	}
	projects, err := a.projects.ListProjects(ctx, limit)
	if err != nil {
		return nil, err
	}
	result := make([]projectcontracts.ProjectSummary, 0, len(projects))
	for _, project := range projects {
		result = append(result, projectcontracts.ProjectSummary{
			ID:                 project.ID.String(),
			Name:               project.Name,
			DefaultWorkspaceID: project.DefaultWorkspaceID.String(),
			Path:               project.Path,
			State:              string(project.State),
		})
	}
	return result, nil
}

// CreateProject 接收 canonical 请求，返回创建后的公开身份和工作区路径。
func (a *App) CreateProject(ctx context.Context, canonical request.CreateProject) (projectcontracts.CreateProjectResult, error) {
	created, err := a.projects.CreateProject(ctx, businessproject.CreateProjectParams{
		RequestID:   identity.RequestID(canonical.RequestID),
		ProjectID:   identity.ProjectID(canonical.ProjectID),
		WorkspaceID: identity.WorkspaceID(canonical.WorkspaceID),
		Name:        canonical.Name,
		Path:        canonical.Path,
	})
	if err != nil {
		return projectcontracts.CreateProjectResult{}, err
	}
	return projectcontracts.CreateProjectResult{
		ProjectID:   created.Project.ID.String(),
		Name:        created.Project.Name,
		WorkspaceID: created.Workspace.ID.String(),
		Path:        created.Workspace.Path,
	}, nil
}

// GetProjectApprovalMode 返回项目选择和业务层解析的有效授权模式。
func (a *App) GetProjectApprovalMode(ctx context.Context, projectID string) (projectcontracts.ProjectApprovalMode, error) {
	value, err := a.projects.GetApprovalMode(ctx, identity.ProjectID(projectID))
	return publicApprovalMode(value), err
}

// SetProjectApprovalMode 保存 canonical 授权选择，失败时不返回成功状态。
func (a *App) SetProjectApprovalMode(ctx context.Context, canonical request.SetProjectApprovalMode) (projectcontracts.ProjectApprovalMode, error) {
	value, err := a.projects.SetApprovalMode(ctx, identity.ProjectID(canonical.ProjectID), canonical.Mode)
	return publicApprovalMode(value), err
}

func publicApprovalMode(value businessproject.ApprovalSettings) projectcontracts.ProjectApprovalMode {
	result := projectcontracts.ProjectApprovalMode{
		ProjectID:     value.ProjectID.String(),
		GlobalMode:    string(value.GlobalMode),
		EffectiveMode: string(value.EffectiveMode),
	}
	if value.ProjectMode != nil {
		mode := string(*value.ProjectMode)
		result.ProjectMode = &mode
	}
	return result
}
