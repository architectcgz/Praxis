package project

import (
	"path/filepath"
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
	ID                 ProjectID
	Name               string
	Path               string
	DefaultWorkspaceID WorkspaceID
	State              ProjectState
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func NewProject(
	id ProjectID,
	name string,
	path string,
	defaultWorkspaceID WorkspaceID,
	at time.Time,
) (Project, error) {
	project := Project{
		ID:                 id,
		Name:               strings.TrimSpace(name),
		Path:               normalizeAbsolutePath(path),
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
	if idIsEmpty(string(p.ID)) {
		return invalidValue("project.id", "id is required")
	}
	if p.Name == "" || strings.ContainsAny(p.Name, "\x00\r\n") {
		return invalidValue("project.name", "name is required and cannot contain control characters")
	}
	if p.Path == "" || p.Path == "." || !filepath.IsAbs(p.Path) || filepath.Clean(p.Path) != p.Path || strings.ContainsAny(p.Path, "\x00\r\n") {
		return invalidValue("project.path", "path must be an absolute normalized path")
	}
	if idIsEmpty(string(p.DefaultWorkspaceID)) {
		return invalidValue("project.defaultWorkspaceID", "default workspace id is required")
	}
	if p.State != ProjectActive && p.State != ProjectArchived {
		return invalidValue("project.state", "unknown project state")
	}
	if p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() || p.UpdatedAt.Before(p.CreatedAt) {
		return invalidValue("project.timestamps", "timestamps are invalid")
	}
	return nil
}

func (p *Project) Archive(at time.Time) error {
	if p.State != ProjectActive {
		return invalidTransition("project", string(p.State), string(ProjectArchived))
	}
	p.State = ProjectArchived
	p.UpdatedAt = at.UTC()
	return nil
}

func (p Project) Snapshot() Project { return p }
