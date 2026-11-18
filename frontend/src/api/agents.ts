import type {
    HistoryMessage as GeneratedHistoryMessage,
    HistoryPage as GeneratedHistoryPage,
    MessageBlock,
    ModelRequestActivity as GeneratedModelRequestActivity,
    ModelRequestResult as GeneratedModelRequestResult,
    ModelUsage as GeneratedModelUsage,
} from '../../bindings/praxis/wails/bindings'
import { getAgentBinding } from './bindings'

export type { MessageBlock }
export type HistoryMessage = Omit<GeneratedHistoryMessage, 'content'> & { content: MessageBlock[] }
export type ModelUsage = { [Key in keyof GeneratedModelUsage]: NonNullable<GeneratedModelUsage[Key]> }
export type ModelRequestResult = Omit<GeneratedModelRequestResult, 'status' | 'usage'> & {
    status: 'completed' | 'failed' | 'interrupted'
    usage?: ModelUsage
}
export type ModelRequestActivity = Omit<GeneratedModelRequestActivity, 'requestKind' | 'result'> & {
    requestKind: 'iteration' | 'summary' | 'risk_review'
    result?: ModelRequestResult
}
export type HistoryPage = Omit<GeneratedHistoryPage, 'messages' | 'requests'> & {
    messages: HistoryMessage[]
    requests: ModelRequestActivity[]
}
export type RunOutcome = 'completed' | 'yielded' | 'failed' | 'interrupted'

export type AgentView = {
    ID: string
    SessionID: string
    Kind: 'main' | 'subagent'
    createdAt?: string
    name: string
    state: 'unknown' | 'idle' | 'executing' | 'waiting' | 'interrupted' | 'failed'
    runId?: string
    runStartedAt?: string
    runEndedAt?: string
    runOutcome?: RunOutcome
    runFailure?: string
    runIterations?: number
    runToolCalls?: number
}

export function activeRunId(agent?: AgentView | null) {
    return agent?.runEndedAt ? undefined : agent?.runId
}

export type { AgentEvent } from './events'
export { subscribeAgentEvents } from './events'

export function listAgentHistory(sessionId: string, agentId: string, before: number | null = null): Promise<HistoryPage> {
    return getAgentBinding().ListAgentHistory({ sessionId, agentId, before, limit: 50 }) as Promise<HistoryPage>
}
