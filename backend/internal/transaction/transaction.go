package transaction

import (
	"context"
)

// Runner 在单一持久化后端内执行原子事务。
type Runner interface {
	InTx(context.Context, func(context.Context) error) error
}
