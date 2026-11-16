import type { RunSummary as RunSnapshot } from '../../bindings/praxis/wails/dto/models'
import type { AgentView as GeneratedAgentView } from '../../bindings/praxis/wails/dto/models'
import type { Entry as GeneratedEntry, EntryBlock as GeneratedEntryBlock } from '../../bindings/praxis/wails/dto/models'
import { getAgentBinding } from './bindings'
import type { Timing } from './timings'


// Go 的展示投影保证 content 为数组，缺失的 timing 字段直接省略，不编码为 null。
export type Entry = Omit<GeneratedEntry, 'content' | 'timing'> & { content: EntryBlock[]; timing?: Timing }
export type EntryBlock = Omit<GeneratedEntryBlock, 'timing'> & { timing?: Timing }
export type { RunSnapshot }
export type HistoryPage = { headEntryId: string; entries: Entry[]; hasMore: boolean; nextCursor: string }

/** 保留生成接口的字段，将枚举暴露为其字面量联合，允许前端表达乐观状态。 */
export type AgentView = Omit<GeneratedAgentView, 'state'> & { state: 'idle' | 'executing' | 'waiting' | 'interrupted' | 'failed' }

export type ModelUsage = {
    inputTokens: number
    outputTokens?: number
    cacheReadInputTokens?: number
    cacheCreationInputTokens?: number
}

export type ModelUsageRecord = {
    sessionId: string
    runId: string
    turnId: string
    requestId: string
    usage: ModelUsage
}

export type { AgentEvent } from './events'
export { subscribeAgentEvents } from './events'

export function listAgentEntries(sessionId: string, agentId: string, headEntryId = '', beforeEntryId = ''): Promise<HistoryPage> {
    return getAgentBinding().ListAgentEntries({ sessionId, agentId, headEntryId, beforeEntryId, limit: 50 }) as Promise<HistoryPage>
}

export async function listSessionUsage(sessionId: string, turnIds: string[]): Promise<ModelUsageRecord[]> {
    return (await getAgentBinding().ListSessionUsage({ sessionId, turnIds }) ?? []) as ModelUsageRecord[]
}
