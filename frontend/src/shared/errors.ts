import { API_ERROR_CODES, BindingUnavailableError, isApiErrorCode } from '../api'

export function readableError(error: unknown) {
    if (error instanceof BindingUnavailableError) {
        return 'Wails bridge is unavailable. Run the desktop app to connect to the core.'
    }
    if (error instanceof Error && error.message) {
        if (isApiErrorCode(error.message)) {
            return readableApiError(error.message)
        }
        return error.message
    }
    return 'The command could not be completed.'
}

function readableApiError(code: string) {
    switch (code) {
        case API_ERROR_CODES.projectWorkspaceInvalid:
            return 'Project name must not contain a path separator.'
        case API_ERROR_CODES.modelNotConfigured:
            return 'Select a configured Provider and Model before sending.'
        case API_ERROR_CODES.validation:
            return 'Some request values are invalid.'
        case API_ERROR_CODES.notFound:
            return 'The requested resource could not be found.'
        case API_ERROR_CODES.notReady:
            return 'Praxis is still starting. Try again in a moment.'
        case API_ERROR_CODES.bindingUnavailable:
            return 'The requested desktop capability is unavailable.'
        case API_ERROR_CODES.internal:
            return 'Praxis could not complete the operation because of an internal error.'
        default:
            return code
    }
}
