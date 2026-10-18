package agent

import (
	"praxis/internal/fault"
)

const (
	ExecutionBusy          fault.Code = "execution.busy"
	ExecutionContract      fault.Code = "execution.contract_error"
	ExecutionPolicyBlocked fault.Code = "execution.policy_blocked"
	ExecutionApproval      fault.Code = "execution.approval_required"
	ExecutionStorage       fault.Code = "execution.storage_error"
	ExecutionProvider      fault.Code = "execution.provider_error"
	ExecutionTool          fault.Code = "execution.tool_error"
	ExecutionResourceLimit fault.Code = "execution.resource_limit"
)
