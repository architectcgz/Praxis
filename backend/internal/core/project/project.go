package project

import (
	"praxis/internal/contracts"
	"praxis/internal/utils/pathutil"

	"strings"
	"time"
)

// ProjectState is the lifecycle of a long-lived user project.
type ProjectState string

const (
	ProjectActive   ProjectState = "active"
	ProjectArchived ProjectState = "archived"
)

// Project is the durable identity and catalog record for one user project.
// Workspaces and Sessions are linked by identifiers and remain independent
// aggregates.
type Project struct {
	ID                 contracts.ProjectID
	Name               string
	Path               string
	DefaultWorkspaceID contracts.WorkspaceID
	State              ProjectState
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func NewProject(
	id contracts.ProjectID,
	name string,
	path string,
	defaultWorkspaceID contracts.WorkspaceID,
	at time.Time,
) (Project, error) {
	project := Project{
		ID:                 id,
		Name:               name,
		Path:               path,
		DefaultWorkspaceID: defaultWorkspaceID,
		State:              ProjectActive,
		CreatedAt:          at.UTC(),
		UpdatedAt:          at.UTC(),
	}
	if err := project.Validate(); err != nil {
		return Project{}, err
	}
	return project, nil
}

func (p Project) Validate() error {
	if contracts.EmptyID(string(p.ID)) {
		return contracts.InvalidValue("project.id", "id is required")
	}
	if p.Name == "" || strings.ContainsAny(p.Name, "\x00\r\n") {
		return contracts.InvalidValue("project.name", "name is required and cannot contain control characters")
	}
	if !pathutil.IsAbsoluteNormalized(p.Path) {
		return contracts.InvalidValue("project.path", "path must be an absolute normalized path")
	}
	if contracts.EmptyID(string(p.DefaultWorkspaceID)) {
		return contracts.InvalidValue("project.defaultWorkspaceID", "default workspace id is required")
	}
	if p.State != ProjectActive && p.State != ProjectArchived {
		return contracts.InvalidValue("project.state", "unknown project state")
	}
	if p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() || p.UpdatedAt.Before(p.CreatedAt) {
		return contracts.InvalidValue("project.timestamps", "timestamps are invalid")
	}
	return nil
}
