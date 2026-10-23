package toolexecution

import (
	"praxis/internal/core/security"
	"praxis/internal/identity"
	"time"
)

type EventKind string

const (
	ApprovalRequired EventKind = "tool.approval_required"
	Approved         EventKind = "tool.approved"
	Started          EventKind = "tool.started"
	StopRequested    EventKind = "tool.stop_requested"
	Reviewed         EventKind = "tool.reviewed"
)

// Event 只保存无法从消息配对推导的控制事实，不包含原始调用或结果正文。
type Event struct {
	Key
	SessionID identity.SessionID
	Kind      EventKind
	At        time.Time
	Decision  security.ApprovalDecision `json:",omitzero"`
	Source    ApprovalSource            `json:",omitempty"`
	Summary   string                    `json:",omitempty"`
	Review    *RiskReview               `json:",omitempty"`
}

// Facts 是控制事件的只读投影，完成与否必须查询 ToolResult 消息。
type Facts struct {
	ApprovalFacts
	StartedAt       time.Time
	StopRequestedAt time.Time
}

// Apply 按事件更新控制事实；存储层按日志序号投影，不持久化此结构。
func (f *Facts) Apply(e Event) {
	switch e.Kind {
	case ApprovalRequired:
		f.RequiredAt, f.Decision, f.Summary = e.At, e.Decision, e.Summary
		f.Approval = ApprovalRecord{}
	case Reviewed:
		f.Review = *e.Review
	case Approved:
		f.Decision, f.Summary = e.Decision, e.Summary
		f.Approval = ApprovalRecord{Source: e.Source, ApprovedAt: e.At}
	case Started:
		f.StartedAt = e.At
	case StopRequested:
		f.StopRequestedAt = e.At
	}
}
