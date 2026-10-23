import { getCommandBinding } from './bindings'

export type PauseAgentRequest = { commandId: string; agentId: string; targetExecutionId: string }
export type StopAgentRequest = PauseAgentRequest
export type AgentControlResponse = {
    commandId: string
    agentId?: string
    targetExecutionId?: string
    kind?: string
    status: string
    existingCommand?: boolean
    cancellationError?: string
}

export function pauseAgent(request: PauseAgentRequest) {
    return getCommandBinding().PauseAgent(request)
}

/** 取消明确指定的回合；延迟到达的请求不会取消后续回合。 */
export function stopAgent(request: StopAgentRequest) {
    return getCommandBinding().StopAgent(request)
}

export type ToolControlRequest = { sessionId: string; agentId: string; executionId: string; callId: string }

export function stopToolCall({ sessionId, agentId, executionId, callId }: ToolControlRequest) {
    return getCommandBinding().StopToolCall({ sessionId, agentId, executionId, callId })
}

export type ToolDecisionRequest = ToolControlRequest
/** 待授权记录携带完整冻结参数与审核结论，展示字段不随授权决策提交。 */
export type ToolApproval = ToolDecisionRequest & {
    name: string
    arguments: unknown
    ruleId: string
    action: string
    riskVerdict: string
    riskReason: string
}
export async function listToolApprovals(agentId: string): Promise<ToolApproval[]> {
    const values = await getCommandBinding().ListToolApprovals(agentId)
    return values.map((value) => ({ ...value, arguments: JSON.parse(value.arguments) as unknown }))
}
export function approveToolCall(request: ToolDecisionRequest) {
    return getCommandBinding().ApproveToolCall(toolDecisionRequest(request))
}

export function denyToolCall(request: ToolDecisionRequest) {
    return getCommandBinding().DenyToolCall(toolDecisionRequest(request))
}

/** 审批只提交调用身份，展示字段不参与决策请求。 */
function toolDecisionRequest({ sessionId, agentId, executionId, callId }: ToolDecisionRequest): ToolDecisionRequest {
    return { sessionId, agentId, executionId, callId }
}
