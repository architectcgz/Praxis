package session

import (
	"errors"
	"strings"
	"time"

	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

const (
	ErrIncompleteToolCalls errorcode.Code = "session.incomplete_tool_calls"
	ErrBranchNameConflict  errorcode.Code = "session.branch_name_conflict"
)

// Branch 保存命名路径的位置，不拥有 Entry 或页面身份。
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
	if b.ID == "" || strings.TrimSpace(b.ID.String()) != b.ID.String() || b.Name == "" || strings.TrimSpace(b.Name) != b.Name {
		return errors.New("invalid branch identity or name")
	}
	if b.CreatedAt.IsZero() || b.UpdatedAt.Before(b.CreatedAt) {
		return errors.New("invalid branch timestamps")
	}
	return nil
}
