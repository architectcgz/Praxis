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

export type SendInputResponse = { executionId: string; existingRequest: boolean; activationError?: string }
export type QueueWorkRequest = { id: string; requestId: string; agentId: string; prompt: string }
export type PauseAgentRequest = { commandId: string; agentId: string }
export type CloseAgentRequest = PauseAgentRequest
export type AgentControlResponse = {
    commandId: string
    agentId?: string
    targetExecutionId?: string
    kind?: string
    status: string
    existingCommand?: boolean
    cancellationError?: string
}
export type PauseAgentResponse = AgentControlResponse
export type CloseAgentResponse = AgentControlResponse

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
