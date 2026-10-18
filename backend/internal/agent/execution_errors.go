package agent

import (
	"praxis/internal/fault"
)

const (
	ExecutionBusy          fault.Code = "task.busy"
	ExecutionContract      fault.Code = "task.contract_error"
	ExecutionPolicyBlocked fault.Code = "task.policy_blocked"
	ExecutionApproval      fault.Code = "task.approval_required"
	ExecutionStorage       fault.Code = "task.storage_error"
	ExecutionProvider      fault.Code = "task.provider_error"
	ExecutionTool          fault.Code = "task.tool_error"
	ExecutionResourceLimit fault.Code = "task.resource_limit"
)
