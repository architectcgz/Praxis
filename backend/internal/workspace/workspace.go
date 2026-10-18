package workspace

import (
	"praxis/internal/identity"
	"time"
)

// WorkspaceKind 标识工作区目录的提供方式。
type WorkspaceKind string

const (
	WorkspaceProjectRoot WorkspaceKind = "project_root"
	WorkspaceWorktree    WorkspaceKind = "worktree"
	WorkspaceTemporary   WorkspaceKind = "temporary"
)

// WorkspaceState 表示工作区是否可用于执行 Task。
type WorkspaceState string

const (
	WorkspaceReady       WorkspaceState = "ready"
	WorkspaceUnavailable WorkspaceState = "unavailable"
	WorkspaceArchived    WorkspaceState = "archived"
)

// Workspace 是供一个或多个 Session 使用的共享文件系统资源。
// Path 和 Revision 是可变的资源事实，ID 是稳定身份。
type Workspace struct {
	ID        identity.WorkspaceID
	ProjectID identity.ProjectID
	Kind      WorkspaceKind
	Path      string
	State     WorkspaceState
	Revision  uint64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewWorkspace 使用入口已校验的身份、类型和路径创建 ready 工作区。
func NewWorkspace(
	id identity.WorkspaceID,
	projectID identity.ProjectID,
	kind WorkspaceKind,
	path string,
	at time.Time,
) Workspace {
	return Workspace{
		ID:        id,
		ProjectID: projectID,
		Kind:      kind,
		Path:      path,
		State:     WorkspaceReady,
		Revision:  1,
		CreatedAt: at.UTC(),
		UpdatedAt: at.UTC(),
	}
}
