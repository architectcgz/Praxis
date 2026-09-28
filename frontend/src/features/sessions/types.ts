import type { AgentEvent } from '../../api'

export type StreamingOutput = {
    executionId: string
    turns: { turn: number; events: AgentEvent[] }[]
    error: string
}

export type PendingUserMessage = {
    requestId: string
    executionId: string
    sessionId: string
    agentId: string
    content: string
    at: string
}
