import { getCommandBinding } from './bindings'

export type SendInputRequest = {
    sessionId: string
    agentId: string
    requestId: string
    content: string
    providerId: string
    modelId: string
    reasoning: string
    sandboxMode: string
    approvalMode: string
    revision: string
}

export type SendInputResponse = { executionId: string; existingRequest: boolean; activationError?: string }
export type ControlRequest = { id: string; agentId: string; kind: 'pause' | 'close' }
export type ControlResponse = { requestId: string; status: string }

export function sendInput(request: SendInputRequest) {
    return getCommandBinding().SendInput(request)
}

export function requestControl(request: ControlRequest) {
    return getCommandBinding().RequestControl(request)
}
