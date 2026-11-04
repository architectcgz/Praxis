package coordinator

import "errors"

var (
	errCoordinatorClosed = errors.New("execution coordinator is closed")
	errRunInterrupted    = errors.New("agent execution interrupted")
)
