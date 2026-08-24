package domain

import "strings"

// ExecutionFailureCode is the stable, UI-safe reason for a failed or
// interrupted target execution. Its string representation is persisted in
// SQLite and JSONL, and crosses the Wails binding unchanged.
type ExecutionFailureCode string

const (
	ExecutionFailureProviderUnavailable ExecutionFailureCode = "provider_unavailable"
	ExecutionFailureProvider            ExecutionFailureCode = "execution_provider_error"
	ExecutionFailureTool                ExecutionFailureCode = "execution_tool_error"
	ExecutionFailurePolicyBlocked       ExecutionFailureCode = "execution_policy_blocked"
	ExecutionFailureApprovalRequired    ExecutionFailureCode = "execution_approval_required"
	ExecutionFailureResourceLimit       ExecutionFailureCode = "execution_resource_limit"
	ExecutionFailureStorage             ExecutionFailureCode = "execution_storage_error"
	ExecutionFailureContract            ExecutionFailureCode = "execution_contract_error"
	ExecutionFailureBusy                ExecutionFailureCode = "execution_busy"
	ExecutionFailureClosed              ExecutionFailureCode = "execution_closed"
	ExecutionFailureInterrupted         ExecutionFailureCode = "execution_interrupted"
	ExecutionFailureRuntimeCancelled    ExecutionFailureCode = "runtime_cancelled"
	ExecutionFailureRuntimeFailed       ExecutionFailureCode = "runtime_failed"
	ExecutionFailureRuntimeInvalid      ExecutionFailureCode = "runtime_invalid_outcome"
	ExecutionFailureRecoveryInterrupted ExecutionFailureCode = "recovery_interrupted"
	ExecutionFailureLegacy              ExecutionFailureCode = "legacy_failure"
)

func (c ExecutionFailureCode) Valid() bool {
	switch c {
	case "", ExecutionFailureProviderUnavailable, ExecutionFailureProvider,
		ExecutionFailureTool, ExecutionFailurePolicyBlocked,
		ExecutionFailureApprovalRequired, ExecutionFailureResourceLimit,
		ExecutionFailureStorage, ExecutionFailureContract, ExecutionFailureBusy,
		ExecutionFailureClosed, ExecutionFailureInterrupted,
		ExecutionFailureRuntimeCancelled, ExecutionFailureRuntimeFailed,
		ExecutionFailureRuntimeInvalid, ExecutionFailureRecoveryInterrupted,
		ExecutionFailureLegacy:
		return true
	default:
		return false
	}
}

// ExecutionFailureFromLegacy preserves known codes during conversion and
// replaces pre-enum free-form values with one safe, queryable code.
func ExecutionFailureFromLegacy(value string) ExecutionFailureCode {
	code := ExecutionFailureCode(strings.TrimSpace(value))
	if code == "" {
		return ""
	}
	if code != "" && code.Valid() {
		return code
	}
	return ExecutionFailureLegacy
}
