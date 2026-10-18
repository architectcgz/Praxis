package contracts

import "time"

// TaskInfo 是任务的安全展示数据，不暴露执行配置和内部错误。
type TaskInfo struct {
	ID             string
	Status         string
	Outcome        string
	FailureCode    string
	FailureMessage string
	CreatedAt      time.Time
	StartedAt      time.Time
	EndedAt        time.Time
}
