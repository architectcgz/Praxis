package repository

import (
	"context"
	"praxis/internal/core/toolexecution"
)

// ToolExecution 只追加控制事件并按调用键读取投影，不保存执行实体或结果。
type ToolExecution interface {
	Read(context.Context, toolexecution.Key) (toolexecution.Facts, error)
	Append(context.Context, toolexecution.Event) error
}
