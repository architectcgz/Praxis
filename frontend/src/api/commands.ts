import { getCommandBinding } from './bindings'
import { parseToolCallInput, type ToolCallInput } from './tools'

export type StopAgentRequest = { sessionId: string; branchId: string; agentId: string; runId: string }

/** 中断用户选择的具体 Run，不将目标替换为命令到达时的新执行。 */
export function stopAgent(request: StopAgentRequest) {
    return getCommandBinding().StopAgent(request)
}

export type ToolCancelRequest = { sessionId: string; agentId: string; runId: string; callId: string }

export function stopToolCall({ sessionId, agentId, runId, callId }: ToolCancelRequest) {
    return getCommandBinding().StopToolCall({ sessionId, agentId, runId, callId })
}

export type ToolDecisionRequest = { sessionId: string; agentId: string; runId: string; callId: string }
/** 待授权记录携带完整冻结参数与审核结论，展示字段不随授权决策提交。 */
export type ToolApproval = ToolDecisionRequest & ToolCallInput & {
    ruleId: string
    action: string
    riskVerdict: string
    riskReason: string
}
export async function listToolApprovals(sessionId: string, agentId: string): Promise<ToolApproval[]> {
    const values = await getCommandBinding().ListToolApprovals(sessionId, agentId) ?? []
    return values.map((value) => ({ ...value, ...parseToolCallInput(value.name, value.arguments) }))
}
export function approveToolCall(request: ToolDecisionRequest) {
    return getCommandBinding().ApproveToolCall(toolDecisionRequest(request))
}

export function denyToolCall(request: ToolDecisionRequest) {
    return getCommandBinding().DenyToolCall(toolDecisionRequest(request))
}

/** 审批只提交调用身份，展示字段不参与决策请求。 */
function toolDecisionRequest({ sessionId, agentId, runId, callId }: ToolDecisionRequest): ToolDecisionRequest {
    return { sessionId, agentId, runId, callId }
}
