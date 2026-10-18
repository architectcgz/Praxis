package identity

import (
	"fmt"
)

// NewTurnID 根据执行内已持久化的请求序号生成标识，不创建独立执行状态。
func NewTurnID(executionID AgentExecutionID, sequence uint64) TurnID {
	return TurnID(fmt.Sprintf("turn:%s:%d", executionID, sequence))
}
