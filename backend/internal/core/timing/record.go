package timing

import (
	"errors"
	"strings"
	"time"
)

type Kind string

type Status string

const (
	Agent    Kind = "agent"
	Provider Kind = "provider"
	Tool     Kind = "tool"

	Running     Status = "running"
	Completed   Status = "completed"
	Failed      Status = "failed"
	Cancelled   Status = "cancelled"
	Interrupted Status = "interrupted"
)

// Operation 只携带关联标识；禁止写入 Prompt、工具参数、响应正文或凭据。
type Operation struct {
	SessionID   string `json:"sessionId"`
	AgentID     string `json:"agentId"`
	ExecutionID string `json:"executionId"`
	Kind        Kind   `json:"kind"`
	Name        string `json:"name"`
	ReferenceID string `json:"referenceId,omitempty"`
}

// Record 是独立的计时事实。DurationMS 为 nil 表示尚未结束或异常退出后无法确定耗时。
type Record struct {
	Operation
	ID              string    `json:"id"`
	ParentID        string    `json:"parentId,omitempty"`
	StartedAt       time.Time `json:"startedAt"`
	FinishedAt      time.Time `json:"finishedAt,omitzero"`
	DurationMS      *int64    `json:"durationMs,omitempty"`
	FirstResponseMS *int64    `json:"firstResponseMs,omitempty"`
	Status          Status    `json:"status"`
	Revision        int       `json:"revision"`
}

// Validate 只读校验存储恢复和写入边界；不修正非法字段，也不推算未知耗时。
func (r Record) Validate() error {
	for _, value := range []string{r.ID, r.SessionID, r.AgentID, r.ExecutionID, r.Name} {
		if value == "" || value != strings.TrimSpace(value) {
			return errors.New("timing identity and name must be canonical and nonempty")
		}
	}
	if r.Kind != Agent && r.Kind != Provider && r.Kind != Tool {
		return errors.New("unknown timing kind")
	}
	if r.StartedAt.IsZero() || r.Revision < 1 || r.ParentID == r.ID {
		return errors.New("invalid timing identity, timestamp or revision")
	}
	if r.DurationMS != nil && *r.DurationMS < 0 || r.FirstResponseMS != nil && *r.FirstResponseMS < 0 {
		return errors.New("negative operation duration")
	}
	if r.FirstResponseMS != nil && (r.Kind != Provider || r.DurationMS != nil && *r.FirstResponseMS > *r.DurationMS) {
		return errors.New("invalid first response duration")
	}
	switch r.Status {
	case Running:
		if r.DurationMS != nil || !r.FinishedAt.IsZero() {
			return errors.New("running timing has terminal fields")
		}
	case Completed, Failed, Cancelled:
		if r.DurationMS == nil || r.FinishedAt.IsZero() {
			return errors.New("finished timing requires duration and timestamp")
		}
	case Interrupted:
		if r.DurationMS != nil || !r.FinishedAt.IsZero() {
			return errors.New("interrupted timing cannot claim an exact end")
		}
	default:
		return errors.New("unknown timing status")
	}
	return nil
}
