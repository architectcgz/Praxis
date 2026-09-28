package system

import "time"

type Clock interface {
	Now() time.Time
}

type IDGenerator interface {
	New(prefix string) string
}
