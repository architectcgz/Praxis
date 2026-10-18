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

export type PauseAgentRequest = { commandId: string; agentId: string; targetExecutionId: string }
export type StopAgentRequest = PauseAgentRequest
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
    targetExecutionId?: string
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
export function stopAgent(request: StopAgentRequest) {
    return getCommandBinding().StopAgent(request)
}

/** 预约忙碌期间的后续输入，领取时创建独立 Execution ID。 */
export function queueTask(request: QueueTaskRequest) {
    return getCommandBinding().QueueTask(request)
}

export type ToolDecisionRequest = { sessionId: string; agentId: string; executionId: string; callId: string; argumentsDigest: string }
/** 待授权记录携带完整冻结参数与审核结论，展示字段不随授权决策提交。 */
export type ToolApproval = ToolDecisionRequest & {
    name: string
    normalizedArguments: unknown
    path: string
    ruleId: string
    action: string
    riskVerdict: string
    riskReason: string
}
export async function listToolApprovals(agentId: string): Promise<ToolApproval[]> {
    const values = await getCommandBinding().ListToolApprovals(agentId)
    return values.map((value) => ({ ...value, normalizedArguments: JSON.parse(value.normalizedArguments) as unknown }))
}
export function approveToolCall(request: ToolDecisionRequest) {
    return getCommandBinding().ApproveToolCall(toolDecisionRequest(request))
}

export function denyToolCall(request: ToolDecisionRequest) {
    return getCommandBinding().DenyToolCall(toolDecisionRequest(request))
}

/** 审批只提交调用身份与参数摘要，展示字段不参与决策请求。 */
function toolDecisionRequest({ sessionId, agentId, executionId, callId, argumentsDigest }: ToolDecisionRequest): ToolDecisionRequest {
    return { sessionId, agentId, executionId, callId, argumentsDigest }
}
