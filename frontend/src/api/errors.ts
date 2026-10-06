// wails 层错误码：入参校验与传输/宿主。
// 业务错误码来自 internal/contracts，由 wails 层原样透传，前端直接识别。
export const API_ERROR_CODES = {
    // wails 层
    invalidRequest: 'invalid_request',
    bindingUnavailable: 'binding_unavailable',
    requestCanceled: 'request_canceled',
    requestTimeout: 'request_timeout',
    internal: 'internal_error',

    // 业务层（internal/contracts）
    validation: 'generic.invalid_value',
    invalidTransition: 'generic.invalid_transition',
    notFound: 'generic.not_found',
    notReady: 'orchestration.not_ready',
    projectWorkspaceInvalid: 'project.workspace_invalid',
    workspaceConflict: 'project.lease_conflict',
    revisionConflict: 'project.revision_conflict',
    agentExecuting: 'agent.executing',
    agentUnavailable: 'agent.unavailable',
    alreadyEnded: 'agent.already_ended',
    workQueueEmpty: 'work.queue_empty',
    requestNotFound: 'request.not_found',
    requestConflict: 'request.conflict',
    modelNotConfigured: 'model.not_configured',
    taskContract: 'task.contract_error',
    taskPolicyBlocked: 'task.policy_blocked',
    taskApprovalRequired: 'task.approval_required',
    taskStorage: 'task.storage_error',
    taskProvider: 'task.provider_error',
    taskTool: 'task.tool_error',
    taskResourceLimit: 'task.resource_limit',
    taskBusy: 'task.busy',
    taskInterrupted: 'task.interrupted',
} as const

export type ApiErrorCode = typeof API_ERROR_CODES[keyof typeof API_ERROR_CODES]

export interface ApiErrorEnvelope {
    code: string
    message: string
}

export function isApiErrorCode(value: string): value is ApiErrorCode {
    return Object.values(API_ERROR_CODES).includes(value as ApiErrorCode)
}

// 解析绑定错误：
//   - 后端 wire 为 JSON {"code","message"}；
//   - 兼容前端合成的裸码字符串（如 commands.ts 抛出的 modelNotConfigured）。
export function parseApiError(error: unknown): ApiErrorEnvelope | undefined {
    const message = error instanceof Error ? error.message : typeof error === 'string' ? error : undefined
    if (message === undefined) {
        return undefined
    }
    const trimmed = message.trim()
    const start = trimmed.indexOf('{')
    const end = trimmed.lastIndexOf('}')
    if (start >= 0 && end > start) {
        try {
            const parsed = JSON.parse(trimmed.slice(start, end + 1)) as { code?: unknown; message?: unknown }
            if (typeof parsed.code === 'string') {
                return {
                    code: parsed.code,
                    message: typeof parsed.message === 'string' ? parsed.message : '',
                }
            }
        } catch {
            // 非法 JSON 时回退到裸码解析
        }
    }
    const [rawCode, ...rest] = trimmed.split(':')
    const code = rawCode.trim()
    if (isApiErrorCode(code)) {
        return { code, message: rest.join(':').trim() }
    }
    return undefined
}
