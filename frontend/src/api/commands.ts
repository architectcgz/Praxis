import { getCommandBinding } from './bindings'

export type StopAgentRequest = { agentId: string }

/** 停止 Agent 当前执行；后端负责在内部定位并中断对应的 Execution。 */
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
export async function listToolApprovals(sessionId: string, branchId: string, agentId: string): Promise<ToolApproval[]> {
    const values = await getCommandBinding().ListToolApprovals(sessionId, branchId, agentId) ?? []
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
