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

export type PauseAgentRequest = { commandId: string; agentId: string }
export type CloseAgentRequest = PauseAgentRequest
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

export function closeAgent(request: CloseAgentRequest) {
    return getCommandBinding().CloseAgent(request)
}
