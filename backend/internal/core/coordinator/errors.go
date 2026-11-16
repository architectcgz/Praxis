package coordinator

import (
	"errors"
	agentrun "praxis/internal/core/agent/run"
)

var (
	errCoordinatorClosed = errors.New("execution coordinator is closed")
	errRunInterrupted    = agentrun.ErrInterrupted
)
