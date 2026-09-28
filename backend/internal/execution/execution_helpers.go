package execution

// ValidExecutionOutcome reports whether an execution outcome is supported.
func ValidExecutionOutcome(outcome ExecutionOutcome) bool { return validExecutionOutcome(outcome) }
