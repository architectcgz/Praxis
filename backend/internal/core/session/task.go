package session

import (
	"praxis/internal/identity"
	"time"
)

const MaxInputBytes = 32 * 1024

// Task 是 Session 的待执行输入，不持有 Agent、请求身份或执行配置。
// Task ID 用于队列查询与取消，其他执行身份由调度准入时分配。
type Task struct {
	ID        identity.TaskID
	SessionID identity.SessionID
	Prompt    string
	Sequence  uint64
	CreatedAt time.Time
}

func NewTask(id identity.TaskID, sessionID identity.SessionID, prompt string, at time.Time) Task {
	return Task{
		ID:        id,
		SessionID: sessionID,
		Prompt:    prompt,
		CreatedAt: at.UTC(),
	}
}
