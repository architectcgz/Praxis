package toolexecution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"praxis/internal/core/security"
	"praxis/internal/core/tool"
	"praxis/internal/fault"
	"praxis/internal/identity"
	"time"
)

// Key 同时定位消息配对、审批事实和进程内取消句柄。
type Key struct {
	ExecutionID identity.AgentExecutionID
	CallID      string
}

func (k Key) String() string { return fmt.Sprintf("%s:%d:%s", k.ExecutionID, len(k.CallID), k.CallID) }

func ArgumentsDigest(name tool.ToolName, args json.RawMessage) string {
	sum := sha256.Sum256(append(append([]byte(name), 0), args...))
	return hex.EncodeToString(sum[:])
}

type EventKind string

const (
	ApprovalRequired EventKind = "tool.approval_required"
	Approved         EventKind = "tool.approved"
	Started          EventKind = "tool.started"
	StopRequested    EventKind = "tool.stop_requested"
	Reviewed         EventKind = "tool.reviewed"
)

type ApprovalSource string

const (
	ApprovalSourceUser ApprovalSource = "user"
	ApprovalSourceYolo ApprovalSource = "yolo"
	ApprovalSourceAuto ApprovalSource = "auto"
)

type ApprovalRecord struct {
	Source          ApprovalSource
	ArgumentsDigest string
	ApprovedAt      time.Time
}

type RiskReview struct {
	TurnID          identity.TurnID
	ArgumentsDigest string
	ProviderID      string
	ModelID         string
	StartedAt       time.Time
	CompletedAt     time.Time
	Assessment      security.RiskAssessment
}

// Event 只保存无法从消息配对推导的控制事实，不包含原始调用或结果正文。
type Event struct {
	Key
	SessionID       identity.SessionID
	Kind            EventKind
	At              time.Time
	ArgumentsDigest string                    `json:",omitempty"`
	Decision        security.ApprovalDecision `json:",omitzero"`
	Source          ApprovalSource            `json:",omitempty"`
	Summary         string                    `json:",omitempty"`
	Review          *RiskReview               `json:",omitempty"`
}

// Facts 是控制事件的只读投影，完成与否必须查询 ToolResult 消息。
type Facts struct {
	RequiredAt      time.Time
	ArgumentsDigest string
	Decision        security.ApprovalDecision
	Summary         string
	Approval        ApprovalRecord
	Review          RiskReview
	StartedAt       time.Time
	StopRequestedAt time.Time
}

func (f Facts) AwaitingApproval() bool {
	return !f.RequiredAt.IsZero() && f.Approval.ApprovedAt.IsZero() && f.StartedAt.IsZero() && f.StopRequestedAt.IsZero()
}

// Apply 校验事件顺序；存储层按日志序号投影，不持久化此结构。
func (f *Facts) Apply(e Event) error {
	switch e.Kind {
	case ApprovalRequired:
		if !f.StartedAt.IsZero() || !f.StopRequestedAt.IsZero() {
			return fault.ErrRequestConflict
		}
		f.RequiredAt, f.ArgumentsDigest, f.Decision, f.Summary = e.At, e.ArgumentsDigest, e.Decision, e.Summary
		f.Approval = ApprovalRecord{}
	case Reviewed:
		if !f.StartedAt.IsZero() || !f.StopRequestedAt.IsZero() || e.Review == nil {
			return fault.ErrRequestConflict
		}
		f.Review = *e.Review
	case Approved:
		if !f.StartedAt.IsZero() || !f.StopRequestedAt.IsZero() {
			return fault.ErrRequestConflict
		}
		if e.Source == ApprovalSourceUser && (!f.AwaitingApproval() || f.ArgumentsDigest != e.ArgumentsDigest) {
			return fault.ErrRequestConflict
		}
		if e.Source == ApprovalSourceUser && e.Decision.Action == security.ApprovalAuto && f.Review.Assessment.Verdict != security.RiskUncertain {
			return fault.ErrRequestConflict
		}
		if e.Source == ApprovalSourceYolo && e.Decision.Action != security.ApprovalYolo {
			return fault.ErrRequestConflict
		}
		if e.Source != ApprovalSourceUser && e.Source != ApprovalSourceAuto && e.Source != ApprovalSourceYolo {
			return fault.ErrRequestConflict
		}
		if e.Decision.Action == security.ApprovalDeny {
			return fault.ErrRequestConflict
		}
		if e.Source == ApprovalSourceAuto && (e.Decision.Action != security.ApprovalAuto || f.Review.ArgumentsDigest != e.ArgumentsDigest || f.Review.Assessment.Verdict != security.RiskSafe) {
			return fault.ErrRequestConflict
		}
		f.ArgumentsDigest, f.Decision, f.Summary = e.ArgumentsDigest, e.Decision, e.Summary
		f.Approval = ApprovalRecord{Source: e.Source, ArgumentsDigest: e.ArgumentsDigest, ApprovedAt: e.At}
	case Started:
		if !f.StartedAt.IsZero() || !f.StopRequestedAt.IsZero() || f.Approval.ApprovedAt.IsZero() || f.Approval.ArgumentsDigest != e.ArgumentsDigest {
			return fault.ErrRequestConflict
		}
		f.StartedAt = e.At
	case StopRequested:
		if !f.StopRequestedAt.IsZero() {
			return fault.ErrRequestConflict
		}
		f.StopRequestedAt = e.At
	default:
		return fault.InvalidValue("tool.event", "unknown control event")
	}
	return nil
}
