package bindings

import (
	"praxis/internal/identity"
	"praxis/internal/pathutil"
	projectservice "praxis/internal/service/project"
	"praxis/wails/validation"
	"strings"
)

type ProjectBindings struct {
	listLimit int
	host      Host
}

type CreateProjectRequest struct {
	ProjectID   string `json:"projectId" validate:"project_id"`
	ProjectName string `json:"projectName" validate:"project_name"`
	Path        string `json:"path" validate:"project_path"`
}

type CreateProjectResponse struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Path      string `json:"path"`
}

type ProjectSummary struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Path  string `json:"path"`
	State string `json:"state"`
}

// SelectProjectPath 打开系统目录选择器并返回选中的项目路径；用户取消时返回空字符串。
func (b *ProjectBindings) SelectProjectPath() (string, error) {
	_, _, err := b.host.BindingContext()
	if err != nil {
		return "", err
	}
	path, err := b.host.SelectDirectory("选择项目路径")
	if err != nil {
		return "", publicError(b.host, "ProjectBindings.SelectProjectPath", err)
	}
	return path, nil
}

func (b *ProjectBindings) ListProjects() ([]ProjectSummary, error) {
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return nil, err
	}
	projects, err := services.Projects.ListProjects(ctx, b.listLimit)
	if err != nil {
		return nil, publicError(b.host, "ProjectBindings.ListProjects", err)
	}
	result := make([]ProjectSummary, 0, len(projects))
	for _, project := range projects {
		result = append(result, ProjectSummary{
			ID:    project.ID.String(),
			Name:  project.Name,
			Path:  project.Path,
			State: string(project.State),
		})
	}
	return result, nil
}

func (b *ProjectBindings) CreateProject(wire CreateProjectRequest) (CreateProjectResponse, error) {
	wire.ProjectName = strings.TrimSpace(wire.ProjectName)
	wire.Path = pathutil.NormalizeAbsolutePath(wire.Path)
	if err := validation.Struct(wire); err != nil {
		return CreateProjectResponse{}, publicInputError(b.host, "ProjectBindings.CreateProject.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return CreateProjectResponse{}, err
	}
	params := projectservice.CreateInput{
		ProjectID: identity.ProjectID(wire.ProjectID),
		Name:      wire.ProjectName,
		Path:      wire.Path,
	}
	result, err := services.Projects.CreateProject(ctx, params)
	if err != nil {
		return CreateProjectResponse{}, publicError(b.host, "ProjectBindings.CreateProject", err)
	}
	return CreateProjectResponse{
		ProjectID: result.ID.String(),
		Name:      result.Name,
		Path:      result.Path,
	}, nil
}
