package agent

import (
	"context"

	"praxis/internal/identity"
)

// ExecutionStore 只表达 Execution 事实的读取与保存，调度和展示查询由消费者声明。
type ExecutionStore interface {
	Get(context.Context, identity.AgentExecutionID) (Execution, error)
	Save(context.Context, Execution) error
}
