package agentruntime

import "praxis/internal/core/domain"

// validateRunForRuntime prevents a run from crossing the immutable runtime thread boundary.
func validateRunForRuntime(run domain.AgentRun, threadID domain.AgentThreadID) error {
	if run.AgentThreadID != threadID {
		return &RuntimeError{Code: ErrorContract, Message: "run does not belong to runtime thread"}
	}
	return nil
}
