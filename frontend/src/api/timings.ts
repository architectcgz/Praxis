export type OperationTiming = {
    id: string
    parentId?: string
    sessionId: string
    agentId: string
    executionId: string
    kind: 'agent' | 'provider' | 'tool'
    name: string
    referenceId?: string
    startedAt: string
    finishedAt?: string
    durationMs?: number
    firstResponseMs?: number
    status: 'running' | 'completed' | 'failed' | 'cancelled' | 'interrupted'
    revision: number
}
