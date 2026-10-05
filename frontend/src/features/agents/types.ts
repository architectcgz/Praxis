import type { AgentEvent, AgentHistoryItem, AgentSnapshot } from '../../api'

/** 单次执行的临时输出，持久化历史回填后移除。 */
export type StreamingOutput = {
    turnId: string
    steps: { step: number; events: AgentEvent[] }[]
    error: string
}

export type StreamingOutputs = Record<string, StreamingOutput>

/** 发送中的用户消息，保留发送时的归属，避免切换视图后串到其他对话。 */
export type PendingUserMessage = {
    requestId: string
    turnId: string
    sessionId: string
    agentId: string
    content: string
    at: string
}

/** Agent 侧栏展示所需的最小 Session 摘要。 */
export type AgentSessionSummary = { title: string; agents: AgentSnapshot[] }

export type AgentsPanelProps = {
    loading: boolean
    session: AgentSessionSummary | null
    histories: Record<string, AgentHistoryItem[]>
    selectedAgentID: string
    onSelectAgent: (id: string) => void
}
