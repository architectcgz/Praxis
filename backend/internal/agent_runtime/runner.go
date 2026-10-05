package agentruntime

import (
	"context"

	"praxis/internal/contracts"
	turnmodel "praxis/internal/core/turn"
)

// TurnRunner 接收持久化执行快照与 owner 限定的消息存储，返回执行结果和失败分类。
// 实现只负责模型与工具循环，不负责最终结算；取消或执行失败通过 error 返回。
type TurnRunner interface {
	RunWithSession(
		context.Context,
		turnmodel.Turn,
		TurnMessageStore,
	) (turnmodel.TurnOutcome, contracts.TurnFailureCode, error)
}
