import type {
    AgentView as GeneratedAgentView,
    HistoryMessage as GeneratedHistoryMessage,
    HistoryPage as GeneratedHistoryPage,
    MessageBlock as GeneratedMessageBlock,
    ModelRequestActivity as GeneratedModelRequestActivity,
    ModelRequestResult as GeneratedModelRequestResult,
    ModelUsage as GeneratedModelUsage,
    RunSummary as RunSnapshot,
} from '../../bindings/praxis/internal/dto/models'
import { getAgentBinding } from './bindings'
import type { Timing } from './timings'

export type HistoryMessage = Omit<GeneratedHistoryMessage, 'content' | 'timing'> & { content: MessageBlock[]; timing?: Timing }
export type MessageBlock = Omit<GeneratedMessageBlock, 'timing'> & { timing?: Timing }
export type ModelUsage = { [Key in keyof GeneratedModelUsage]: NonNullable<GeneratedModelUsage[Key]> }
export type ModelRequestResult = Omit<GeneratedModelRequestResult, 'status' | 'usage'> & {
    status: 'completed' | 'failed' | 'interrupted'
    usage?: ModelUsage
}
export type ModelRequestActivity = Omit<GeneratedModelRequestActivity, 'requestKind' | 'timing' | 'result'> & {
    requestKind: 'iteration' | 'summary' | 'risk_review'
    timing?: Timing
    result?: ModelRequestResult
}
export type HistoryPage = Omit<GeneratedHistoryPage, 'messages' | 'requests'> & {
    messages: HistoryMessage[]
    requests: ModelRequestActivity[]
}
export type { RunSnapshot }
export type AgentView = Omit<GeneratedAgentView, 'state'> & { state: 'idle' | 'executing' | 'waiting' | 'interrupted' | 'failed' }

export type { AgentEvent } from './events'
export { subscribeAgentEvents } from './events'

export function listAgentHistory(sessionId: string, agentId: string, before: number | null = null): Promise<HistoryPage> {
    return getAgentBinding().ListAgentHistory({ sessionId, agentId, before, limit: 50 }) as Promise<HistoryPage>
}
