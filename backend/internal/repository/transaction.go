package repository

import "context"

// TxRunner 在同一事务中执行持久化操作。
type TxRunner interface {
	InTx(context.Context, func(context.Context) error) error
}
