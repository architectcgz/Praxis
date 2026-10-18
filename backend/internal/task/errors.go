package task

import (
	"praxis/internal/fault"
)

const (
	TaskBusy          fault.Code = "task.busy"
	TaskContract      fault.Code = "task.contract_error"
	TaskPolicyBlocked fault.Code = "task.policy_blocked"
	TaskApproval      fault.Code = "task.approval_required"
	TaskStorage       fault.Code = "task.storage_error"
	TaskProvider      fault.Code = "task.provider_error"
	TaskTool          fault.Code = "task.tool_error"
	TaskResourceLimit fault.Code = "task.resource_limit"
)
