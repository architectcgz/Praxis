package system

import (
	"context"
	"time"
)

type Clock interface {
	Now() time.Time
}

type IDGenerator interface {
	New(prefix string) string
}

type Lifecycle interface {
	Ready(ctx context.Context) error
	Shutdown(ctx context.Context) error
}
