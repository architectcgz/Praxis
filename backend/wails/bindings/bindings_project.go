package bindings

import (
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"path/filepath"
	"praxis/internal/fault"
	"praxis/internal/identity"
	projectservice "praxis/internal/service/project"
	"praxis/wails/dto"
	"praxis/wails/validation"
	"strings"
)

type ProjectBindings struct {
	host Host
}

// SelectProjectPath 打开系统目录选择器并返回选中的项目路径；用户取消时返回空字符串。
func (b *ProjectBindings) SelectProjectPath() (string, error) {
	ctx, _, err := b.host.BindingContext()
	if err != nil {
		return "", err
	}
	path, err := wailsruntime.OpenDirectoryDialog(ctx, wailsruntime.OpenDialogOptions{Title: "选择项目路径"})
	if err != nil {
		return "", publicError(b.host, "ProjectBindings.SelectProjectPath", err)
	}
	return path, nil
}

func (b *ProjectBindings) ListProjects() ([]dto.ProjectSummary, error) {
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return nil, err
	}
	projects, err := services.Projects.ListProjects(ctx, 100)
	if err != nil {
		return nil, publicError(b.host, "ProjectBindings.ListProjects", err)
	}
	result := make([]dto.ProjectSummary, 0, len(projects))
	for _, project := range projects {
		result = append(result, dto.ProjectSummary{
			ID:                 project.ID.String(),
			Name:               project.Name,
			DefaultWorkspaceID: project.DefaultWorkspaceID.String(),
			Path:               project.Path,
			State:              string(project.State),
		})
	}
	return result, nil
}

func (b *ProjectBindings) CreateProject(wire dto.CreateProjectRequest) (dto.CreateProjectResponse, error) {
	if err := validation.ValidateCreateProject(wire); err != nil {
		return dto.CreateProjectResponse{}, err
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.CreateProjectResponse{}, err
	}
	params := projectservice.CreateInput{
		ProjectID:   identity.ProjectID(wire.ProjectID),
		WorkspaceID: identity.WorkspaceID(wire.WorkspaceID),
		Name:        strings.TrimSpace(wire.ProjectName),
		Path:        filepath.Clean(strings.TrimSpace(wire.Path)),
		RequestID:   identity.RequestID(wire.RequestID),
	}
	if params.Name == "" || strings.ContainsAny(params.Name, "\\/:*?\"<>|\x00\r\n") ||
		params.Name == "." || params.Name == ".." || !filepath.IsAbs(params.Path) || strings.ContainsAny(params.Path, "\x00\r\n") {
		err := fault.New(projectservice.ProjectWorkspaceInvalid, "")
		return dto.CreateProjectResponse{}, publicError(b.host, "ProjectBindings.CreateProject.request", err)
	}
	result, err := services.Projects.CreateProject(ctx, params)
	if err != nil {
		return dto.CreateProjectResponse{}, publicError(b.host, "ProjectBindings.CreateProject", err)
	}
	return dto.CreateProjectResponse{
		ProjectID:   result.Project.ID.String(),
		Name:        result.Project.Name,
		WorkspaceID: result.Workspace.ID.String(),
		Path:        result.Workspace.Path,
	}, nil
}
