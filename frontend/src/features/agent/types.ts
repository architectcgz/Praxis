import type { AgentEvent } from '../../api'

/** 单次执行的临时输出，持久化条目回填后移除。 */
export type StreamingOutput = {
    runId: string
    turns: { turnId: string; events: AgentEvent[] }[]
    error: string
}

export type StreamingOutputs = Record<string, StreamingOutput>
