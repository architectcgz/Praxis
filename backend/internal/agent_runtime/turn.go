package agentruntime

import (
	"praxis/internal/contracts"
	turnmodel "praxis/internal/core/turn"

	"context"
)

// TurnParams 描述 loop 一次迭代的归属；Sequence 是 Task 内的迭代序号。
type TurnParams struct {
	TaskID    contracts.TaskID
	SessionID contracts.SessionID
	AgentID   contracts.AgentID
	Sequence  uint64
}

// TurnRecorder 负责 loop 迭代生命周期记录的持久化，实现方必须保证重复调用幂等。
type TurnRecorder interface {
	// RecordStart 按 Task 归属与序号持久化 running 记录；重复调用返回已有记录，失败返回错误。
	RecordStart(context.Context, TurnParams) (turnmodel.Turn, error)
	// RecordEnd 按迭代 ID 持久化结果与失败信息；已结算时不重复写入，失败返回错误。
	RecordEnd(context.Context, contracts.TurnID, turnmodel.TurnOutcome, contracts.TaskFailureCode, string) error
}
