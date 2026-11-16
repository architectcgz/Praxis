package entry

import (
	"praxis/internal/identity"
	"time"
)

// TurnRecord 表达一轮 ReAct 迭代，请求尝试及审核不单独创建轮次。
type TurnRecord struct {
	ID        identity.TurnID
	RunID     identity.RunID
	StartedAt time.Time
	EndedAt   time.Time
}
