package bindings

import (
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"praxis/internal/contracts"

	"praxis/wails/dto"
	"praxis/wails/validation"
)

type ProjectBindings struct {
	runtime Runtime
}

// SelectProjectPath 打开系统目录选择器并返回选中的项目路径；用户取消时返回空字符串，桌面能力不可用时返回错误。
func (b *ProjectBindings) SelectProjectPath() (string, error) {
	ctx, _, err := b.runtime.BindingContext()
	if err != nil {
		return "", err
	}
	path, err := runtime.OpenDirectoryDialog(ctx, runtime.OpenDialogOptions{
		Title: "选择项目路径",
	})
	if err != nil {
		return "", publicError(b.runtime, "ProjectBindings.SelectProjectPath", err)
	}
	return path, nil
}

func (b *ProjectBindings) ListProjects() ([]dto.ProjectSummary, error) {
	ctx, service, err := b.runtime.BindingContext()
	if err != nil {
		return nil, err
	}
	projects, err := service.Projects.ListProjects(ctx, 100)
	if err != nil {
		return nil, publicError(b.runtime, "ProjectBindings.ListProjects", err)
	}
	result := make([]dto.ProjectSummary, 0, len(projects))
	for _, project := range projects {
		result = append(result, dto.ProjectSummary{
			ID: project.ID.String(), Name: project.Name,
			DefaultWorkspaceID: project.DefaultWorkspaceID.String(), Path: project.Path,
			State: string(project.State),
		})
	}
	return result, nil
}

func (b *ProjectBindings) CreateProject(request dto.CreateProjectRequest) (dto.CreateProjectResponse, error) {
	if err := validation.ValidateCreateProject(request); err != nil {
		return dto.CreateProjectResponse{}, err
	}
	ctx, service, err := b.runtime.BindingContext()
	if err != nil {
		return dto.CreateProjectResponse{}, err
	}
	result, err := service.Projects.CreateProject(
		ctx,
		contracts.ProjectID(request.ProjectID),
		contracts.WorkspaceID(request.WorkspaceID),
		request.ProjectName,
		request.Path,
		contracts.RequestID(request.RequestID),
	)
	if err != nil {
		return dto.CreateProjectResponse{}, publicError(b.runtime, "ProjectBindings.CreateProject", err)
	}
	return dto.CreateProjectResponse{
		ProjectID: result.Project.ID.String(), Name: result.Project.Name,
		WorkspaceID: result.Workspace.ID.String(), Path: result.Workspace.Path,
	}, nil
}
