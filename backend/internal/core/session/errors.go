package session

import "errors"

var (
	ErrExecutionPaused  = errors.New("agent execution paused")
	ErrExecutionStopped = errors.New("agent execution stopped")
)
