package repository

import (
	"context"
	timingmodel "praxis/internal/core/timing"
)

// Timing 持久化计时事实，拒绝旧版本覆盖和终态回退。
type Timing interface {
	Save(context.Context, timingmodel.Record) error
	ListAgent(context.Context, string, int) ([]timingmodel.Record, error)
	RecoverInterrupted(context.Context) error
}
