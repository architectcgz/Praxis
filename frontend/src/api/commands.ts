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
export type ControlRequest = { requestId: string; agentId: string; kind: 'pause' | 'close' }
export type QueueWorkRequest = { id: string; requestId: string; agentId: string; prompt: string }
export type ControlResponse = { requestId: string; status: string }

export function sendInput(request: SendInputRequest) {
    if (!request.providerId.trim() || !request.modelId.trim()) {
        return Promise.reject(new Error(API_ERROR_CODES.modelNotConfigured))
    }
    return getCommandBinding().SendInput(request)
}

export function requestControl(request: ControlRequest) {
    return getCommandBinding().RequestControl(request)
}
