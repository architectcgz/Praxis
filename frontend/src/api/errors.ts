export const API_ERROR_CODES = {
    invalidRequest: 'invalid_request',
    validation: 'validation_error',
    invalidTransition: 'invalid_transition',
    notReady: 'orchestration_not_ready',
    bindingUnavailable: 'binding_unavailable',
    notFound: 'not_found',
    agentExecuting: 'agent_executing',
    agentUnavailable: 'agent_unavailable',
    requestNotFound: 'request_not_found',
    requestConflict: 'request_conflict',
    workspaceConflict: 'workspace_conflict',
    alreadySettled: 'already_settled',
    workQueueEmpty: 'work_queue_empty',
    workItemActive: 'work_item_active',
    alreadyDelivered: 'already_delivered',
    projectWorkspaceInvalid: 'project_workspace_invalid',
    modelNotConfigured: 'model_not_configured',
    requestCanceled: 'request_canceled',
    requestTimeout: 'request_timeout',
    executionContract: 'execution_contract_error',
    executionPolicyBlocked: 'execution_policy_blocked',
    executionApprovalRequired: 'execution_approval_required',
    executionStorage: 'execution_storage_error',
    executionProvider: 'execution_provider_error',
    executionTool: 'execution_tool_error',
    executionResourceLimit: 'execution_resource_limit',
    executionBusy: 'execution_busy',
    executionClosed: 'execution_closed',
    executionInterrupted: 'execution_interrupted',
    internal: 'internal_error',
} as const

export type ApiErrorCode = typeof API_ERROR_CODES[keyof typeof API_ERROR_CODES]

export function isApiErrorCode(value: string): value is ApiErrorCode {
    return Object.values(API_ERROR_CODES).includes(value as ApiErrorCode)
}
