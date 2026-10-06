import { getCommandBinding } from './bindings'
import { API_ERROR_CODES } from './errors'

export type SendInputRequest = {
    sessionId: string
    agentId: string
    requestId: string
    content: string
    providerId: string
    modelId: string
    reasoningLevel: string
}

export type PauseAgentRequest = { commandId: string; agentId: string; targetTurnId: string }
export type CancelTurnRequest = PauseAgentRequest
export type AgentControlResponse = {
    commandId: string
    agentId?: string
    targetTurnId?: string
    kind?: string
    status: string
    existingCommand?: boolean
    cancellationError?: string
}

export function sendInput(request: SendInputRequest) {
    if (!request.providerId.trim() || !request.modelId.trim()) {
        return Promise.reject(new Error(API_ERROR_CODES.modelNotConfigured))
    }
    return getCommandBinding().SendInput(request)
}

export function pauseAgent(request: PauseAgentRequest) {
    return getCommandBinding().PauseAgent(request)
}

/** 取消明确指定的回合；延迟到达的请求不会取消后续回合。 */
export function cancelTurn(request: CancelTurnRequest) {
    return getCommandBinding().CancelTurn(request)
}
