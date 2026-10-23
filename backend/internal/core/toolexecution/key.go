package toolexecution

import (
	"fmt"
	"praxis/internal/identity"
)

// Key 同时定位消息配对、审批事实和进程内取消句柄。
// 同一 Key 的执行参数在准备后冻结，审批与审核结果直接绑定此 Key。
type Key struct {
	ExecutionID identity.AgentExecutionID
	CallID      string
}

func (k Key) String() string { return fmt.Sprintf("%s:%d:%s", k.ExecutionID, len(k.CallID), k.CallID) }
