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
