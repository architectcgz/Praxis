package workspace

import (
	"praxis/internal/contracts"
	"praxis/internal/utils/pathutil"

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
	ID        contracts.WorkspaceID
	ProjectID contracts.ProjectID
	Kind      WorkspaceKind
	Path      string
	State     WorkspaceState
	Revision  uint64
	CreatedAt time.Time
	UpdatedAt time.Time
}

func NewWorkspace(
	id contracts.WorkspaceID,
	projectID contracts.ProjectID,
	kind WorkspaceKind,
	path string,
	at time.Time,
) (Workspace, error) {
	workspace := Workspace{
		ID:        id,
		ProjectID: projectID,
		Kind:      kind,
		Path:      path,
		State:     WorkspaceReady,
		Revision:  1,
		CreatedAt: at.UTC(),
		UpdatedAt: at.UTC(),
	}
	if err := workspace.Validate(); err != nil {
		return Workspace{}, err
	}
	return workspace, nil
}

func (w Workspace) Validate() error {
	if contracts.EmptyID(string(w.ID)) || contracts.EmptyID(string(w.ProjectID)) {
		return contracts.InvalidValue("workspace", "required reference is missing")
	}
	if w.Kind != WorkspaceProjectRoot && w.Kind != WorkspaceWorktree && w.Kind != WorkspaceTemporary {
		return contracts.InvalidValue("workspace.kind", "unknown workspace kind")
	}
	if !pathutil.IsAbsoluteNormalized(w.Path) {
		return contracts.InvalidValue("workspace.path", "path must be an absolute normalized path")
	}
	if w.State != WorkspaceReady && w.State != WorkspaceUnavailable && w.State != WorkspaceArchived {
		return contracts.InvalidValue("workspace.state", "unknown workspace state")
	}
	if w.Revision == 0 {
		return contracts.InvalidValue("workspace.revision", "revision must be positive")
	}
	if w.CreatedAt.IsZero() || w.UpdatedAt.IsZero() || w.UpdatedAt.Before(w.CreatedAt) {
		return contracts.InvalidValue("workspace.timestamps", "timestamps are invalid")
	}
	return nil
}
