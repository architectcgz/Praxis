package project

import (
	"praxis/internal/core/identity"
	"praxis/internal/core/security"
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
	ID                 identity.ProjectID
	Name               string
	Path               string
	DefaultWorkspaceID identity.WorkspaceID
	State              ProjectState
	ApprovalMode       *security.ApprovalMode `json:",omitempty"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// NewProject 使用入口已校验的身份、名称和路径创建项目，初始状态为 active。
func NewProject(
	id identity.ProjectID,
	name string,
	path string,
	defaultWorkspaceID identity.WorkspaceID,
	at time.Time,
) Project {
	return Project{
		ID:                 id,
		Name:               name,
		Path:               path,
		DefaultWorkspaceID: defaultWorkspaceID,
		State:              ProjectActive,
		CreatedAt:          at.UTC(),
		UpdatedAt:          at.UTC(),
	}
}
