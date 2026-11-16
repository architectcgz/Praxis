import { API_ERROR_CODES, BindingUnavailableError, isApiErrorCode, parseApiError, type ApiErrorCode } from '../api'

export function apiErrorCode(error: unknown): ApiErrorCode | undefined {
    const parsed = parseApiError(error)
    if (!parsed || !isApiErrorCode(parsed.code)) {
        return undefined
    }
    return parsed.code
}

export function readableError(error: unknown) {
    if (error instanceof BindingUnavailableError) {
        return '桌面连接不可用，请在桌面应用中重试。'
    }
    const parsed = parseApiError(error)
    if (parsed) {
        return parsed.message || readableApiError(parsed.code)
    }
    if (error instanceof Error && error.message) {
        return error.message
    }
    if (typeof error === 'string' && error.trim()) {
        return error
    }
    return '操作未完成。'
}

function readableApiError(code: string) {
    switch (code) {
        case API_ERROR_CODES.invalidRequest:
        case API_ERROR_CODES.genericInvalidRequest:
            return '请求格式无效。'
        case API_ERROR_CODES.requestCanceled:
        case API_ERROR_CODES.genericRequestCanceled:
            return '请求已取消。'
        case API_ERROR_CODES.requestTimeout:
        case API_ERROR_CODES.genericRequestTimeout:
            return '请求超时，请稍后重试。'
        case API_ERROR_CODES.projectInvalid:
            return '项目名称、目录或状态无效。'
        case API_ERROR_CODES.modelNotConfigured:
            return '发送前请先配置 Provider 和 Model。'
        case API_ERROR_CODES.validation:
        case API_ERROR_CODES.validationError:
            return '请求参数无效。'
        case API_ERROR_CODES.invalidTransition:
            return '当前状态不允许执行此操作。'
        case API_ERROR_CODES.notFound:
            return '找不到请求的资源。'
        case API_ERROR_CODES.notReady:
            return 'Praxis 仍在启动，请稍后重试。'
        case API_ERROR_CODES.agentExecuting:
            return '代理正在执行中，请等待当前任务完成。'
        case API_ERROR_CODES.agentUnavailable:
            return '代理当前不可用。'
        case API_ERROR_CODES.alreadyEnded:
            return '此执行已经结束。'
        case API_ERROR_CODES.workQueueEmpty:
            return '当前没有可执行的工作。'
        case API_ERROR_CODES.requestNotFound:
            return '找不到对应请求。'
        case API_ERROR_CODES.requestConflict:
            return '请求冲突，请刷新后重试。'
        case API_ERROR_CODES.runContract:
            return '执行契约无效。'
        case API_ERROR_CODES.runPolicyBlocked:
            return '请求被执行策略阻止。'
        case API_ERROR_CODES.runApprovalRequired:
            return '此操作需要批准后才能继续。'
        case API_ERROR_CODES.runStorage:
            return '执行状态保存失败。'
        case API_ERROR_CODES.sessionLogUnavailable:
            return '会话日志写入失败，已停止该会话。请重启应用后再继续，勿重复执行工具。'
        case API_ERROR_CODES.runProvider:
            return '模型 Provider 无法完成请求。'
        case API_ERROR_CODES.runTool:
            return '执行所需工具失败。'
        case API_ERROR_CODES.runResourceLimit:
            return '执行达到资源限制。'
        case API_ERROR_CODES.runBusy:
            return '代理正忙，请稍后重试。'
        case API_ERROR_CODES.runInterrupted:
            return '执行已中断。'
        case API_ERROR_CODES.configuration:
            return '配置无效，请检查后重试。'
        case API_ERROR_CODES.bindingUnavailable:
            return '请求的桌面能力不可用。'
        case API_ERROR_CODES.internal:
        case API_ERROR_CODES.genericInternal:
            return 'Praxis 因内部错误无法完成操作。'
        default:
            return code
    }
}
