package project

import (
	"praxis/internal/contracts"
	"praxis/internal/utils/pathutil"

	"strings"
	"time"
)

// ProjectState 表示用户项目的生命周期状态。
type ProjectState string

const (
	ProjectActive   ProjectState = "active"
	ProjectArchived ProjectState = "archived"
)

// Project 保存用户项目的持久化身份和目录记录。
// Workspace 和 Session 通过标识符关联，各自保持为独立聚合。
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
