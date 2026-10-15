import { getCommandBinding } from './bindings'

export type SendInputRequest = {
    sessionId: string
    agentId: string
    requestId: string
    content: string
    providerId: string
    modelId: string
    reasoningLevel: string
}

export type PauseAgentRequest = { commandId: string; agentId: string; targetTaskId: string }
export type CancelTaskRequest = PauseAgentRequest
export type QueueTaskRequest = {
    taskId: string
    requestId: string
    agentId: string
    prompt: string
    providerId: string
    modelId: string
    reasoningLevel: string
}
export type AgentControlResponse = {
    commandId: string
    agentId?: string
    targetTaskId?: string
    kind?: string
    status: string
    existingCommand?: boolean
    cancellationError?: string
}

export function sendInput(request: SendInputRequest) {
    return getCommandBinding().SendInput(request)
}

export function pauseAgent(request: PauseAgentRequest) {
    return getCommandBinding().PauseAgent(request)
}

/** 取消明确指定的回合；延迟到达的请求不会取消后续回合。 */
export function cancelTask(request: CancelTaskRequest) {
    return getCommandBinding().CancelTask(request)
}

/** 预约忙碌期间的后续输入，后续执行沿用返回的 Task ID。 */
export function queueTask(request: QueueTaskRequest) {
    return getCommandBinding().QueueTask(request)
}

export type ToolDecisionRequest = { sessionId: string; agentId: string; taskId: string; callId: string; argumentsDigest: string }
export type ToolApproval = ToolDecisionRequest & { name: string; command: string; path: string; timeout: number; ruleId: string }
export async function listToolApprovals(agentId: string): Promise<ToolApproval[]> {
    return getCommandBinding().ListToolApprovals(agentId)
}
export function approveToolCall(request: ToolDecisionRequest) { return getCommandBinding().ApproveToolCall(request) }
export function denyToolCall(request: ToolDecisionRequest) { return getCommandBinding().DenyToolCall(request) }
