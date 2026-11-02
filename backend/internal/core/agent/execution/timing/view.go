package timing

import "time"

// View 是计时查询与通知共享的值；Agent 总耗时来自 Execution，不是操作 Span。
type View struct {
	ID              string
	ParentID        string
	SessionID       string
	AgentID         string
	ExecutionID     string
	Kind            string
	Name            string
	ReferenceID     string
	StartedAt       time.Time
	FinishedAt      time.Time
	DurationMS      *int64
	FirstResponseMS *int64
	Status          string
	Revision        int
}

// View 将操作计时事实投影为查询和实时通知共用的视图；已结束的执行不会保留悬空的运行状态。
func (r Record) View(sessionID, agentID string, executionActive bool) View {
	if r.Status == Running && !executionActive {
		r.Status = Interrupted
	}

	revision := 1
	if r.Status != Running {
		revision = 2
	}
	return View{
		ID:              r.ID,
		ParentID:        r.ParentID,
		SessionID:       sessionID,
		AgentID:         agentID,
		ExecutionID:     r.ExecutionID,
		Kind:            string(r.Kind),
		Name:            r.Name,
		ReferenceID:     r.ReferenceID,
		StartedAt:       r.StartedAt,
		FinishedAt:      r.FinishedAt,
		DurationMS:      r.DurationMS,
		FirstResponseMS: r.FirstResponseMS,
		Status:          string(r.Status),
		Revision:        revision,
	}
}
