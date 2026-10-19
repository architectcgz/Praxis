package repository

import (
	"context"
)

// Transaction 在同一持久化后端内执行原子事务，不暴露具体事务对象。
type Transaction interface {
	InTx(context.Context, func(context.Context) error) error
}
