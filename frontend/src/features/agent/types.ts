import type { AgentEvent, Entry, AgentView } from '../../api'

/** 单次执行的临时输出，持久化条目回填后移除。 */
export type StreamingOutput = {
    runId: string
    turns: { turnId: string; events: AgentEvent[] }[]
    error: string
}

export type StreamingOutputs = Record<string, StreamingOutput>

/** 发送中的用户消息，保留发送时的归属，避免切换视图后串到其他对话。 */
export type PendingUserMessage = {
    requestId: string
    runId: string
    sessionId: string
    agentId: string
    content: string
    at: string
}

/** Agent 侧栏展示所需的最小 Session 摘要。 */
export type AgentSessionSummary = { title: string; agents: AgentView[] }

export type AgentsPanelProps = {
    loading: boolean
    session: AgentSessionSummary | null
    agentEntries: Record<string, Entry[]>
    selectedAgentID: string
    onSelectAgent: (id: string) => void
}
