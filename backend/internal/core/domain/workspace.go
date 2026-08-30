package domain

import (
	"path/filepath"
	"strings"
	"time"
)

// WorkspaceKind identifies how a workspace directory is provisioned.
type WorkspaceKind string

const (
	WorkspaceProjectRoot WorkspaceKind = "project_root"
	WorkspaceWorktree    WorkspaceKind = "worktree"
	WorkspaceTemporary   WorkspaceKind = "temporary"
)

// WorkspaceState describes whether an execution directory can be used.
type WorkspaceState string

const (
	WorkspaceReady       WorkspaceState = "ready"
	WorkspaceUnavailable WorkspaceState = "unavailable"
	WorkspaceArchived    WorkspaceState = "archived"
)

// Workspace is the shared filesystem resource used by one or more Sessions.
// Path and Revision are mutable resource facts; ID is its stable identity.
type Workspace struct {
	ID        WorkspaceID
	ProjectID ProjectID
	Kind      WorkspaceKind
	Path      string
	State     WorkspaceState
	Revision  uint64
	CreatedAt time.Time
	UpdatedAt time.Time
}

func NewWorkspace(
	id WorkspaceID,
	projectID ProjectID,
	kind WorkspaceKind,
	path string,
	at time.Time,
) (Workspace, error) {
	workspace := Workspace{
		ID:        id,
		ProjectID: projectID,
		Kind:      kind,
		Path:      normalizeAbsolutePath(path),
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
	if idIsEmpty(string(w.ID)) || idIsEmpty(string(w.ProjectID)) {
		return invalidValue("workspace", "required reference is missing")
	}
	if w.Kind != WorkspaceProjectRoot && w.Kind != WorkspaceWorktree && w.Kind != WorkspaceTemporary {
		return invalidValue("workspace.kind", "unknown workspace kind")
	}
	if w.Path == "." || !filepath.IsAbs(w.Path) || filepath.Clean(w.Path) != w.Path || strings.ContainsAny(w.Path, "\x00\r\n") {
		return invalidValue("workspace.path", "path must be an absolute normalized path")
	}
	if w.State != WorkspaceReady && w.State != WorkspaceUnavailable && w.State != WorkspaceArchived {
		return invalidValue("workspace.state", "unknown workspace state")
	}
	if w.Revision == 0 {
		return invalidValue("workspace.revision", "revision must be positive")
	}
	if w.CreatedAt.IsZero() || w.UpdatedAt.IsZero() || w.UpdatedAt.Before(w.CreatedAt) {
		return invalidValue("workspace.timestamps", "timestamps are invalid")
	}
	return nil
}

// Move changes the execution path and advances the resource revision.
func (w *Workspace) Move(path string, at time.Time) error {
	path = normalizeAbsolutePath(path)
	if path == "." || !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n") {
		return invalidValue("workspace.path", "path must be an absolute path")
	}
	if w.State == WorkspaceArchived {
		return invalidTransition("workspace", string(w.State), "moved")
	}
	if path == w.Path {
		return nil
	}
	w.Path = path
	w.Revision++
	w.State = WorkspaceReady
	w.UpdatedAt = at.UTC()
	return nil
}

func (w *Workspace) MarkUnavailable(at time.Time) error {
	if w.State == WorkspaceArchived {
		return invalidTransition("workspace", string(w.State), string(WorkspaceUnavailable))
	}
	w.State = WorkspaceUnavailable
	w.UpdatedAt = at.UTC()
	return nil
}

func (w *Workspace) Archive(at time.Time) error {
	if w.State == WorkspaceArchived {
		return invalidTransition("workspace", string(w.State), string(WorkspaceArchived))
	}
	w.State = WorkspaceArchived
	w.UpdatedAt = at.UTC()
	return nil
}

func (w Workspace) Snapshot() Workspace { return w }

func normalizeAbsolutePath(path string) string {
	return filepath.Clean(strings.TrimSpace(path))
}
