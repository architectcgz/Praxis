package toolexecution

import (
	"praxis/internal/identity"
	"time"
)

type EventKind string

const (
	ApprovalRequired EventKind = "tool.approval_required"
	ApprovalDecided  EventKind = "tool.approval_decided"
	Reviewed         EventKind = "tool.reviewed"
)

// Event 仅保存审批和审核事实；工具完成以消息回执为准。
type Event struct {
	Key
	SessionID    identity.SessionID
	Kind         EventKind
	At           time.Time
	Request      *ApprovalRequest `json:",omitempty"`
	UserDecision *UserDecision    `json:",omitempty"`
	Review       *ReviewRecord    `json:",omitempty"`
}
