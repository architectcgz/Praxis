// Package project owns Project and initial Workspace write use cases.
package project

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	commandprotocol "praxis/internal/command"
	domainfoundation "praxis/internal/domain/foundation"
	domainproject "praxis/internal/domain/project"
	domainworkspace "praxis/internal/domain/workspace"
	"praxis/internal/persistence"
	"praxis/internal/system"
)

// Readiness controls command admission while startup recovery or shutdown is
// converging durable state.
type Readiness interface {
	Ready() bool
}

// Config contains the ports required to create Project-owned durable state.
type Config struct {
	Transactions persistence.TxRunner
	Projects     persistence.ProjectRepository
	Workspaces   persistence.WorkspaceRepository
	Events       persistence.EventRepository
	Readiness    Readiness
	Clock        system.Clock
	IDs          system.IDGenerator
}

// Service owns Project mutations and their transaction boundaries.
type Service struct {
	tx         persistence.TxRunner
	projects   persistence.ProjectRepository
	workspaces persistence.WorkspaceRepository
	events     persistence.EventRepository
	readiness  Readiness
	clock      system.Clock
	ids        system.IDGenerator
}

// CreateProjectParams contains the durable identity and initial workspace
// data for a new Project.
type CreateProjectParams struct {
	RequestID   domainfoundation.RequestID
	ProjectID   domainfoundation.ProjectID
	WorkspaceID domainfoundation.WorkspaceID
	Name        string
	Path        string
}

// CreateProjectResult returns the Project and its initial Workspace.
type CreateProjectResult struct {
	Project   domainproject.Project
	Workspace domainworkspace.Workspace
}

// NewService creates the Project application service.
func NewService(config Config) (*Service, error) {
	for _, required := range []struct {
		name  string
		value any
	}{
		{name: "transactions", value: config.Transactions},
		{name: "projects", value: config.Projects},
		{name: "workspaces", value: config.Workspaces},
		{name: "events", value: config.Events},
		{name: "readiness", value: config.Readiness},
	} {
		if required.value == nil {
			return nil, fmt.Errorf("project service %s is required", required.name)
		}
	}
	clock := config.Clock
	if clock == nil {
		clock = system.UTCClock{}
	}
	ids := config.IDs
	if ids == nil {
		ids = system.SecureIDGenerator{}
	}
	return &Service{
		tx:         config.Transactions,
		projects:   config.Projects,
		workspaces: config.Workspaces,
		events:     config.Events,
		readiness:  config.Readiness,
		clock:      clock,
		ids:        ids,
	}, nil
}

// CreateProject persists a Project, its initial Workspace, an audit event,
// and its idempotency receipt in one transaction.
func (s *Service) CreateProject(ctx context.Context, params CreateProjectParams) (CreateProjectResult, error) {
	if ctx == nil {
		return CreateProjectResult{}, errors.New("create project context is required")
	}
	if !s.readiness.Ready() {
		return CreateProjectResult{}, commandprotocol.NewError(commandprotocol.ErrorNotReady)
	}
	params.Name = strings.TrimSpace(params.Name)
	params.Path = filepath.Clean(strings.TrimSpace(params.Path))
	if params.ProjectID == "" || params.WorkspaceID == "" {
		return CreateProjectResult{}, commandprotocol.NewError(commandprotocol.ErrorInvalidRequest)
	}
	if params.Name == "" || strings.ContainsAny(params.Name, "\x00\r\n") || !absolutePath(params.Path) {
		return CreateProjectResult{}, commandprotocol.NewError(commandprotocol.ErrorProjectWorkspaceInvalid)
	}
	var result CreateProjectResult
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := s.projects.Get(txCtx, params.ProjectID)
		if err == nil {
			workspace, workspaceErr := s.workspaces.Get(txCtx, params.WorkspaceID)
			if workspaceErr != nil {
				return workspaceErr
			}
			if existing.Name != params.Name || existing.Path != params.Path ||
				existing.DefaultWorkspaceID != params.WorkspaceID ||
				workspace.ProjectID != params.ProjectID || workspace.Path != params.Path {
				return domainfoundation.ErrRequestConflict
			}
			result = CreateProjectResult{Project: existing, Workspace: workspace}
			return nil
		}
		if !errors.Is(err, domainfoundation.ErrNotFound) {
			return err
		}
		if _, err := s.workspaces.Get(txCtx, params.WorkspaceID); err == nil {
			return domainfoundation.ErrRequestConflict
		} else if !errors.Is(err, domainfoundation.ErrNotFound) {
			return err
		}
		at := s.clock.Now().UTC()
		workspace, err := domainworkspace.NewWorkspace(
			params.WorkspaceID,
			params.ProjectID,
			domainworkspace.WorkspaceProjectRoot,
			params.Path,
			at,
		)
		if err != nil {
			return fmt.Errorf("create workspace: %w", err)
		}
		project, err := domainproject.NewProject(params.ProjectID, params.Name, params.Path, workspace.ID, at)
		if err != nil {
			return fmt.Errorf("create project: %w", err)
		}
		if err := s.projects.Save(txCtx, project); err != nil {
			return err
		}
		if err := s.workspaces.Save(txCtx, workspace); err != nil {
			return err
		}
		event := domainfoundation.DomainEvent{
			ID:          domainfoundation.EventID(s.ids.New("event")),
			Type:        domainfoundation.EventProjectCreated,
			OccurredAt:  at,
			ProjectID:   project.ID,
			WorkspaceID: workspace.ID,
		}
		if err := s.events.Append(txCtx, event); err != nil {
			return err
		}
		result = CreateProjectResult{Project: project, Workspace: workspace}
		return nil
	})
	return result, err
}

func absolutePath(path string) bool {
	return path != "" && path != "." && filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsAny(path, "\x00\r\n")
}
