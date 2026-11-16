package timing

import "time"

// Record 是所属 Entry 的只读耗时数据，操作身份由 Entry 提供。
// DurationMS 为 nil 表示尚未结束或无法确定实际耗时。
type Record struct {
	StartedAt       time.Time `json:"startedAt"`
	FinishedAt      time.Time `json:"finishedAt,omitzero"`
	DurationMS      *int64    `json:"durationMs,omitempty"`
	FirstResponseMS *int64    `json:"firstResponseMs,omitempty"`
}
