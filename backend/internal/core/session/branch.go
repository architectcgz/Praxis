package session

import (
	"errors"
	"strings"
	"time"

	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

const (
	ErrBranchNameConflict errorcode.Code = "session.branch_name_conflict"
	ErrInvalidForkPoint   errorcode.Code = "session.invalid_fork_point"
	ErrRootBranch         errorcode.Code = "session.root_branch"
	ErrBranchHasChildren  errorcode.Code = "session.branch_has_children"
)

// BranchCatalog 是 SQLite 中的结构快照，不携带历史正文。
type BranchCatalog struct {
	Branches        []Branch
	RootBranchID    identity.BranchID
	DefaultBranchID identity.BranchID
}

// Branch 的父关系与分叉点属于结构元数据；Head 和修改时间由所属历史派生。
type Branch struct {
	ID             identity.BranchID `json:"id"`
	Name           string            `json:"name"`
	RequestID      string            `json:"request_id"`
	SourceBranchID identity.BranchID `json:"source_branch_id,omitempty"`
	BaseEntryID    string            `json:"base_entry_id"`
	HeadEntryID    string            `json:"head_entry_id"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

func (b Branch) Validate() error {
	if !b.ID.Valid() || b.SourceBranchID != "" && !b.SourceBranchID.Valid() || b.Name == "" || strings.TrimSpace(b.Name) != b.Name {
		return errors.New("invalid branch identity or name")
	}
	if b.CreatedAt.IsZero() || b.UpdatedAt.Before(b.CreatedAt) {
		return errors.New("invalid branch timestamps")
	}
	if b.RequestID == "" || strings.TrimSpace(b.RequestID) != b.RequestID {
		return errors.New("invalid branch creation request")
	}
	if b.IsRoot() && b.BaseEntryID != "" || !b.IsRoot() && b.BaseEntryID == "" {
		return errors.New("invalid branch origin")
	}
	return nil
}

func (b Branch) IsRoot() bool { return b.SourceBranchID == "" }
