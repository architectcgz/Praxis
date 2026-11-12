import type { AgentEvent, Entry, AgentView } from '../../api'

/** 单次执行的临时输出，持久化条目回填后移除。 */
export type StreamingOutput = {
    runId: string
    turns: { turnId: string; events: AgentEvent[] }[]
    error: string
}

export type StreamingOutputs = Record<string, StreamingOutput>

/** Agent 侧栏展示所需的最小 Session 摘要。 */
export type AgentSessionSummary = { title: string; agents: AgentView[] }

export type AgentsPanelProps = {
    loading: boolean
    session: AgentSessionSummary | null
    agentEntries: Record<string, Entry[]>
    selectedAgentID: string
    onSelectAgent: (id: string) => void
}
