import { getCommandBinding } from './bindings'

export type QueuedTaskSnapshot = { id: string; status: string; sequence: number; executionId: string }

export type SendInputRequest = {
    sessionId: string
    agentId: string
    requestId: string
    content: string
    providerId: string
    modelId: string
    reasoningLevel: string
}

export function sendInput(request: SendInputRequest) {
    return getCommandBinding().SendInput(request)
}

export type QueueTaskRequest = {
    taskId: string
    requestId: string
    agentId: string
    prompt: string
    providerId: string
    modelId: string
    reasoningLevel: string
}

/** 预约忙碌期间的后续输入，领取时创建独立 Execution ID。 */
export function queueTask(request: QueueTaskRequest) {
    return getCommandBinding().QueueTask(request)
}
