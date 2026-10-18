package agent

import (
	"praxis/internal/fault"
)

const (
	ExecutingCode   fault.Code = "agent.executing"
	UnavailableCode fault.Code = "agent.unavailable"
)

var (
	ErrAgentExecuting   = fault.New(ExecutingCode, "")
	ErrAgentUnavailable = fault.New(UnavailableCode, "")
)
