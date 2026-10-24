package coordinator

import "errors"

var (
	errCoordinatorClosed = errors.New("execution coordinator is closed")
	errExecutionPaused   = errors.New("agent execution paused")
	errExecutionStopped  = errors.New("agent execution stopped")
)
