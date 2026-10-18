package agent

import (
	"fmt"
	"praxis/internal/identity"
)

// NewTurnID 根据任务内已持久化的请求序号生成标识，不创建独立执行状态。
func NewTurnID(taskID identity.AgentExecutionID, sequence uint64) identity.TurnID {
	return identity.TurnID(fmt.Sprintf("turn:%s:%d", taskID, sequence))
}
