// Package contracts 定义计时查询和通知的公开快照。
package contracts

import "time"

// OperationTiming 保留未知耗时，不能将未上报值补零。
type OperationTiming struct {
	SessionID       string
	AgentID         string
	TaskID          string
	Kind            string
	Name            string
	ReferenceID     string
	ID              string
	ParentID        string
	StartedAt       time.Time
	FinishedAt      time.Time
	DurationMS      *int64
	FirstResponseMS *int64
	Status          string
	Revision        int
}
