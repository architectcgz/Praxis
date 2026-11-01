package coordinator

import "errors"

var (
	errCoordinatorClosed    = errors.New("execution coordinator is closed")
	errExecutionInterrupted = errors.New("agent execution interrupted")
)
