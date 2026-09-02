package app

import (
	"context"

	"praxis/internal/contracts"
	domainfoundation "praxis/internal/core/domain/foundation"
)

type ProjectBindings struct {
	runtime *bindingRuntime
	service serviceRef[ProjectService]
}

func (b *ProjectBindings) ListProjects() (response []contracts.ProjectSummary, err error) {
	done := b.runtime.begin("ListProjects")
	defer func() { done(err) }()
	ctx, service, err := b.bindingContext()
	if err != nil {
		return nil, err
	}
	projects, err := service.ListProjects(ctx, 100)
	if err != nil {
		return nil, publicBindingError(err)
	}
	result := make([]contracts.ProjectSummary, 0, len(projects))
	for _, project := range projects {
		result = append(result, contracts.ProjectSummary{
			ID: project.ID.String(), Name: project.Name,
			DefaultWorkspaceID: project.DefaultWorkspaceID.String(), Path: project.Path,
			State: string(project.State),
		})
	}
	return result, nil
}

func (b *ProjectBindings) CreateProject(
	request contracts.CreateProjectRequest,
) (response contracts.CreateProjectResponse, err error) {
	done := b.runtime.begin("CreateProject")
	defer func() { done(err) }()
	ctx, service, err := b.bindingContext()
	if err != nil {
		return contracts.CreateProjectResponse{}, err
	}
	result, err := service.CreateProject(ctx, request.ProjectName, request.Path, domainfoundation.RequestID(request.RequestID))
	if err != nil {
		return contracts.CreateProjectResponse{}, publicBindingError(err)
	}
	return contracts.CreateProjectResponse{
		ProjectID: result.Project.ID.String(), Name: result.Project.Name,
		WorkspaceID: result.Workspace.ID.String(), Path: result.Workspace.Path,
	}, nil
}

func (b *ProjectBindings) bindingContext() (context.Context, ProjectService, error) {
	ctx, err := b.runtime.context()
	if err != nil {
		return nil, nil, err
	}
	service := b.service.get()
	if service == nil {
		return nil, nil, bindingError(contracts.ErrorCodeBindingUnavailable)
	}
	return ctx, service, nil
}
