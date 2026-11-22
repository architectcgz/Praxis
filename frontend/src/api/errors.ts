// wails 层错误码：入参校验与传输/宿主。
// 业务错误码由后端所属包定义，Wails 透传稳定码，前端按码提供文案。
export const API_ERROR_CODES = {
    // wails 层
    invalidRequest: 'invalid_request',
    bindingUnavailable: 'binding_unavailable',
    requestCanceled: 'request_canceled',
    requestTimeout: 'request_timeout',

    // 通用业务错误
    validation: 'generic.invalid_value',
    notFound: 'generic.not_found',
    internal: 'generic.internal_error',
    requestConflict: 'request.conflict',
    runStorage: 'execution.storage_error',
    runProvider: 'execution.provider_error',
    runContract: 'execution.contract_error',

    // 业务领域
    projectInvalid: 'project.invalid',
    agentExecuting: 'agent.executing',
    modelNotConfigured: 'model.not_configured',
} as const

export type ApiErrorCode = typeof API_ERROR_CODES[keyof typeof API_ERROR_CODES]

export interface ApiErrorEnvelope {
    code: string
    message: string
}

export function isApiErrorCode(value: string): value is ApiErrorCode {
    return Object.values(API_ERROR_CODES).includes(value as ApiErrorCode)
}

// 解析后端绑定错误信封 {"code","message"}。
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
            return undefined
        }
    }
    return undefined
}
