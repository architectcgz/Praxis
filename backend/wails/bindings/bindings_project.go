package bindings

import (
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"praxis/internal/contracts"
	"praxis/internal/request"
	"praxis/wails/dto"
	"praxis/wails/validation"
)

type ProjectBindings struct {
	runtime Runtime
}

// SelectProjectPath 打开系统目录选择器并返回选中的项目路径；用户取消时返回空字符串。
func (b *ProjectBindings) SelectProjectPath() (string, error) {
	ctx, _, err := b.runtime.BindingContext()
	if err != nil {
		return "", err
	}
	path, err := runtime.OpenDirectoryDialog(ctx, runtime.OpenDialogOptions{Title: "选择项目路径"})
	if err != nil {
		return "", publicError(b.runtime, "ProjectBindings.SelectProjectPath", err)
	}
	return path, nil
}

func (b *ProjectBindings) ListProjects() ([]dto.ProjectSummary, error) {
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return nil, err
	}
	projects, err := services.Projects.ListProjects(ctx, 100)
	if err != nil {
		return nil, publicError(b.runtime, "ProjectBindings.ListProjects", err)
	}
	result := make([]dto.ProjectSummary, 0, len(projects))
	for _, project := range projects {
		result = append(result, dto.ProjectSummary{
			ID:                 project.ID.String(),
			Name:               project.Name,
			DefaultWorkspaceID: project.DefaultWorkspaceID.String(),
			Path:               project.Path,
			State:              project.State,
		})
	}
	return result, nil
}

func (b *ProjectBindings) CreateProject(wire dto.CreateProjectRequest) (dto.CreateProjectResponse, error) {
	if err := validation.ValidateCreateProject(wire); err != nil {
		return dto.CreateProjectResponse{}, err
	}
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return dto.CreateProjectResponse{}, err
	}
	canonical, err := request.NewCreateProject(request.CreateProject{
		ProjectID:   contracts.ProjectID(wire.ProjectID),
		WorkspaceID: contracts.WorkspaceID(wire.WorkspaceID),
		Name:        wire.ProjectName,
		Path:        wire.Path,
		RequestID:   contracts.RequestID(wire.RequestID),
	})
	if err != nil {
		return dto.CreateProjectResponse{}, publicError(b.runtime, "ProjectBindings.CreateProject.request", err)
	}
	result, err := services.Projects.CreateProject(ctx, canonical)
	if err != nil {
		return dto.CreateProjectResponse{}, publicError(b.runtime, "ProjectBindings.CreateProject", err)
	}
	return dto.CreateProjectResponse{
		ProjectID:   result.ProjectID.String(),
		Name:        result.Name,
		WorkspaceID: result.WorkspaceID.String(),
		Path:        result.Path,
	}, nil
}
