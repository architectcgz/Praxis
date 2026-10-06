package repository

import (
	"praxis/internal/contracts"
	turnmodel "praxis/internal/core/turn"

	"context"
)

// TurnRepository 负责 loop 迭代的持久化和未结算迭代查询。
type TurnRepository interface {
	Get(ctx context.Context, id contracts.TurnID) (turnmodel.Turn, error)
	Save(ctx context.Context, turn turnmodel.Turn) error
	ListOpen(ctx context.Context) ([]turnmodel.Turn, error)
}
