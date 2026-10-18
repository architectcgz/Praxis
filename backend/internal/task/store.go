package task

import (
	"context"

	"praxis/internal/identity"
)

// Store 只表达 Task 事实的读取与保存，调度和展示查询由消费者声明。
type Store interface {
	Get(context.Context, identity.TaskID) (Task, error)
	Save(context.Context, Task) error
}
