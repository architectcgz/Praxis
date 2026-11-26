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
            return '请求格式无效。'
        case API_ERROR_CODES.requestCanceled:
            return '请求已取消。'
        case API_ERROR_CODES.requestTimeout:
            return '请求超时，请稍后重试。'
        case API_ERROR_CODES.startupFailed:
            return '应用启动失败，请检查应用日志，处理后重新启动。'
        case API_ERROR_CODES.configurationInvalid:
            return '配置内容无效，请检查配置文件的格式和字段。'
        case API_ERROR_CODES.configurationReadFailed:
            return '无法读取配置，请检查文件是否存在以及访问权限。'
        case API_ERROR_CODES.projectInvalid:
            return '项目名称、目录或状态无效。'
        case API_ERROR_CODES.modelNotConfigured:
            return '发送前请先配置 Provider 和 Model。'
        case API_ERROR_CODES.validation:
            return '请求参数无效。'
        case API_ERROR_CODES.notFound:
            return '找不到请求的资源。'
        case API_ERROR_CODES.agentExecuting:
            return '代理正在执行中，请等待当前任务完成。'
        case API_ERROR_CODES.requestConflict:
            return '请求冲突，请刷新后重试。'
        case API_ERROR_CODES.runContract:
            return '执行契约无效。'
        case API_ERROR_CODES.runStorage:
            return '执行状态保存失败。'
        case API_ERROR_CODES.runProvider:
            return '模型 Provider 无法完成请求。'
        case API_ERROR_CODES.bindingUnavailable:
            return '请求的桌面能力不可用。'
        case API_ERROR_CODES.internal:
            return 'Praxis 因内部错误无法完成操作。'
        default:
            return code
    }
}
